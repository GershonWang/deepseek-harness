/** Browser recording bytes conform to the Host's fixed PCM format. */
import { afterEach, expect, it, vi } from 'vitest'
import { audioBase64, encodeWave, Recording, RecordingError } from '../src/client/audio.ts'
import { captureFixture } from './audio-fixture.client.ts'

afterEach(() => { vi.unstubAllGlobals() })

it('encodes bounded PCM samples and base64 across chunk boundaries', () => {
  const bytes = encodeWave(new Float32Array([-2, -0.5, 0, 0.5, 2]))
  const view = new DataView(bytes.buffer)
  expect(new TextDecoder().decode(bytes.slice(0, 4))).toBe('RIFF')
  expect(view.getUint32(24, true)).toBe(16000)
  expect(view.getUint32(40, true)).toBe(10)
  expect([0, 1, 2, 3, 4].map(i => view.getInt16(44 + i * 2, true))).toEqual([-32768, -16384, 0, 16384, 32767])
  const large = encodeWave(new Float32Array(17000))
  expect(Buffer.from(audioBase64(large), 'base64')).toEqual(Buffer.from(large))
})

it('reports unavailable recording and denied permission', async () => {
  vi.stubGlobal('navigator', {})
  const recording = new Recording(() => {})
  await expect(recording.start()).rejects.toMatchObject({ kind: 'unavailable' })
  vi.stubGlobal('AudioWorkletNode', function WorkletStub() {})
  vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: async () => { throw new DOMException('denied', 'NotAllowedError') } } })
  await expect(recording.start()).rejects.toMatchObject({ kind: 'permission' })
  vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: async () => { throw new Error('device lost') } } })
  await expect(recording.start()).rejects.toThrow('device lost')
  expect(new RecordingError('empty').message).toBe('empty')
})

it('releases a microphone granted after cancellation', async () => {
  const permission = Promise.withResolvers<{ getTracks: () => { stop: () => void }[]; getAudioTracks: () => never[] }>()
  const stop = vi.fn(), dispose = vi.fn()
  vi.stubGlobal('AudioWorkletNode', function WorkletStub() {})
  vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: () => permission.promise } })
  const recording = new Recording(dispose)
  const acquiring = recording.start()
  await recording.dispose()
  permission.resolve({ getTracks: () => [{ stop }], getAudioTracks: () => [] })
  await expect(acquiring).rejects.toMatchObject({ kind: 'cancelled' })
  expect(stop).toHaveBeenCalledOnce()
  expect(dispose).toHaveBeenCalledOnce()
})


it('encodes accumulated PCM, bounds timer overshoot and closes audio resources', async () => {
  const b = captureFixture()
  expect(b.recording.amplitude()).toBe(0)
  await b.recording.start()
  expect(b.recording.amplitude()).toBe(0.25)
  // 两个量子：拼接后长度 4 帧 @16000Hz → 上限 1 秒不截断，输出 4 帧 16 kHz WAV。
  b.emit(new Float32Array([0.5, -0.5]))
  b.emit(new Float32Array([0.5, -0.5]))
  const result = await b.recording.stop(1)
  expect(b.offline).toHaveBeenCalledWith(1, 4, 16000)
  expect(result).toEqual(encodeWave(new Float32Array([0.5, -0.5])))
  expect(b.trackStop).toHaveBeenCalled()
  expect(b.close).toHaveBeenCalledOnce()
  await b.recording.dispose()
  expect(b.close).toHaveBeenCalledOnce()
})

it.each([{ empty: true }, { recorderError: true }, { constructError: true }])('releases failed capture resources: %j', async (options) => {
  const b = captureFixture(options)
  if (options.constructError) await expect(b.recording.start()).rejects.toThrow('recorder unavailable')
  else { await b.recording.start(); b.emit(); await expect(b.recording.stop(120)).rejects.toMatchObject({ kind: 'empty' }) }
  expect(b.trackStop).toHaveBeenCalled()
  expect(b.close).toHaveBeenCalledOnce()
})

it('discards resampling results that finish after cancellation', async () => {
  const b = captureFixture(), rendered = Promise.withResolvers<{ getChannelData: () => Float32Array<ArrayBuffer> }>()
  b.rendering.mockReturnValueOnce(rendered.promise)
  await b.recording.start()
  b.emit()
  const stopping = b.recording.stop(120), rejected = expect(stopping).rejects.toMatchObject({ kind: 'cancelled' })
  await vi.waitFor(() => { expect(b.rendering).toHaveBeenCalledOnce() })
  await b.recording.dispose()
  rendered.resolve({ getChannelData: () => new Float32Array([0.5, -0.5]) })
  await rejected
  await expect(b.recording.stop(120)).rejects.toMatchObject({ kind: 'empty' })
})

it('releases active capture on a spontaneous recorder error even when AudioContext.close rejects', async () => {
  const b = captureFixture()
  await b.recording.start()
  b.close.mockRejectedValueOnce(new Error('audio device disconnected'))
  b.failRecorder()
  await vi.waitFor(() => { expect(b.disposed).toHaveBeenCalledOnce() })
  expect(b.trackStop).toHaveBeenCalled()
  await expect(b.recording.dispose()).rejects.toThrow('audio device disconnected')
})

it.each([false, true])('shares pending AudioContext closure across repeated release (failure=%s)', async (failed) => {
  const b = captureFixture(), closing = Promise.withResolvers<undefined>()
  b.close.mockReturnValueOnce(closing.promise)
  await b.recording.start()
  const first = b.recording.dispose(), second = b.recording.dispose()
  const results = Promise.allSettled([first, second])
  try {
    expect(first).toBe(second)
    expect(b.trackStop).toHaveBeenCalledOnce()
    expect(b.close).toHaveBeenCalledOnce()
    expect(b.disposed).not.toHaveBeenCalled()
  } finally {
    if (failed) closing.reject(new Error('audio close failed'))
    else closing.resolve(undefined)
    await results
  }
  expect(b.disposed).toHaveBeenCalledOnce()
  expect(b.recording.dispose()).toBe(first)
  expect((await results).map(result => result.status)).toEqual(failed ? ['rejected', 'rejected'] : ['fulfilled', 'fulfilled'])
})

it('reports a device interruption once while resource closure is still pending', async () => {
  const b = captureFixture(), closing = Promise.withResolvers<undefined>(), failure = vi.fn()
  b.close.mockReturnValueOnce(closing.promise)
  await b.recording.start(failure)
  try {
    b.failRecorder(); b.failRecorder()
    expect(failure).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ kind: 'interrupted' }))
    expect(b.trackStop).toHaveBeenCalledOnce()
    expect(b.disposed).not.toHaveBeenCalled()
  } finally { closing.resolve(undefined); await b.recording.dispose() }
  expect(b.disposed).toHaveBeenCalledOnce()
})

it('contains a failing interruption callback and still releases the microphone', async () => {
  const b = captureFixture(), error = new Error('callback failed'), report = vi.spyOn(console, 'error').mockImplementation(() => {})
  try {
    await b.recording.start(() => { throw error })
    b.failRecorder()
    await b.recording.dispose()
    expect(b.trackStop).toHaveBeenCalledOnce()
    expect(b.disposed).toHaveBeenCalledOnce()
    expect(report).toHaveBeenCalledWith('Speech recording error handler failed', error)
  } finally { report.mockRestore(); await b.recording.dispose() }
})
