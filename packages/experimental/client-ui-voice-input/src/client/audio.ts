/** Browser-owned microphone capture and native Web Audio resampling. */

/** Capture failure whose message is localized by the caller. */
export class RecordingError extends Error {
  constructor(readonly kind: 'unavailable' | 'permission' | 'empty' | 'cancelled' | 'interrupted' | 'deviceMissing') { super(kind); this.name = 'RecordingError' }
}

/**
 * Encode mono floating-point samples as the canonical PCM16 WAV accepted by the Host.
 * @param samples - native-resampled 16 kHz mono samples.
 * @returns complete little-endian WAV bytes.
 */
export function encodeWave(samples: Float32Array): Uint8Array<ArrayBuffer> {
  const bytes = new Uint8Array(44 + samples.length * 2)
  const view = new DataView(bytes.buffer)
  const text = (at: number, value: string): void => { for (let i = 0; i < value.length; i++) bytes[at + i] = value.charCodeAt(i) }
  text(0, 'RIFF'); view.setUint32(4, bytes.length - 8, true); text(8, 'WAVE'); text(12, 'fmt ')
  view.setUint32(16, 16, true); view.setUint16(20, 1, true); view.setUint16(22, 1, true)
  view.setUint32(24, 16000, true); view.setUint32(28, 32000, true)
  view.setUint16(32, 2, true); view.setUint16(34, 16, true); text(36, 'data')
  view.setUint32(40, samples.length * 2, true)
  for (const [i, sample] of samples.entries()) {
    const value = Math.max(-1, Math.min(1, sample))
    view.setInt16(44 + i * 2, Math.round(value * (value < 0 ? 32768 : 32767)), true)
  }
  return bytes
}

/**
 * Encode the binary recording for the existing JSON Remote carrier.
 * @param bytes - complete recording.
 * @returns base64 with no data URL prefix.
 */
export function audioBase64(bytes: Uint8Array): string {
  let text = ''
  for (let i = 0; i < bytes.length; i += 8192) text += String.fromCharCode(...bytes.subarray(i, i + 8192))
  return btoa(text)
}

/**
 * Worklet processor source for raw PCM accumulation.
 *
 * Why not MediaRecorder: WebKitGTK's MediaRecorder accepts `start()` and reports
 * `state === 'recording'`, but emits no `dataavailable` payload at all — the container only
 * advertises `audio/mp4`, and its encoder chain produces nothing. The recording therefore
 * arrives empty and the transcript is blank. An AudioWorklet sidesteps the encoder entirely
 * by receiving the already-decoded float samples, which WebKitGTK delivers reliably.
 *
 * The processor forwards each render quantum to the main thread instead of encoding in the
 * worklet: accumulating there would keep two copies of the recording and make the transfer
 * size unbounded. Only channel 0 is read, matching the mono 16 kHz output the Host accepts.
 */
const PCM_WORKLET_SOURCE = `class DshPcmCollector extends AudioWorkletProcessor {
  process(inputs) {
    const channel = inputs[0] && inputs[0][0]
    if (channel && channel.length) this.port.postMessage(channel.slice())
    return true
  }
}
registerProcessor('dsh-pcm-collector', DshPcmCollector)`

/**
 * Flatten worklet quanta into one contiguous buffer.
 * @param parts - capture-rate samples in arrival order.
 * @param total - the exact combined length, which the caller already tracks.
 * @returns one buffer holding every sample once.
 */
function concatenate(parts: Float32Array[], total: number): Float32Array<ArrayBuffer> {
  const joined = new Float32Array(total)
  let offset = 0
  for (const part of parts) { joined.set(part, offset); offset += part.length }
  return joined
}

/** One microphone acquisition, including a permission prompt that may settle after cancellation. */
export class Recording {
  private stream: MediaStream | undefined
  private context: AudioContext | undefined
  private analyser: AnalyserNode | undefined
  private worklet: AudioWorkletNode | undefined
  private samples = new Float32Array(256)
  /** Capture-rate samples received from the worklet, in arrival order. */
  private captured: Float32Array[] = []
  private capturedLength = 0
  private readonly lifetime = new AbortController()
  private disposal: Promise<void> | undefined

  constructor(private readonly onDispose: () => void, private readonly deviceId = '') {}

  /**
   * Acquire the selected microphone without recording or playing audio.
   * @param onError - receives device interruptions before resource release completes.
   * @returns after the level analyser starts; cancelled grants release their tracks immediately.
   */
  async preview(onError?: (error: RecordingError) => void): Promise<void> {
    const devices = (navigator as Partial<Navigator>).mediaDevices
    if (!devices) throw new RecordingError('unavailable')
    let stream: MediaStream
    try {
      stream = await devices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true,
        ...(this.deviceId === '' ? {} : { deviceId: { exact: this.deviceId } }) }, video: false })
    } catch (error) {
      if (error instanceof DOMException) {
        if (error.name === 'NotAllowedError') throw new RecordingError('permission')
        if (error.name === 'NotFoundError' || error.name === 'OverconstrainedError') throw new RecordingError('deviceMissing')
      }
      throw error
    }
    if (this.lifetime.signal.aborted) { stream.getTracks().forEach((track) => { track.stop() }); throw new RecordingError('cancelled') }
    this.stream = stream
    try {
      this.context = new AudioContext()
      const source = this.context.createMediaStreamSource(stream)
      // The analyser keeps driving Waveform; it reads the same source independently of the
      // worklet, so the live level stays available even if capture collection fails.
      this.analyser = this.context.createAnalyser()
      this.analyser.fftSize = this.samples.length
      source.connect(this.analyser)
      // 采集轨道 ended 是设备被拔掉/被系统回收的异步信号：没有它用户会停在"录音中"
      // 直到超时，看到的是"未识别到语音"而不是设备中断。AbortSignal 让释放时自动摘除。
      for (const track of stream.getTracks()) track.addEventListener('ended', () => { this.interrupted(onError) }, { signal: this.lifetime.signal })
    } catch (error) { await this.dispose(); throw error }
  }

  /**
   * Acquire the selected microphone and record audio for transcription.
   * @param onError - receives failures during capture, before asynchronous resource release finishes.
   * @returns after capture starts; a cancelled permission grant immediately releases its tracks.
   */
  async start(onError?: (error: RecordingError) => void): Promise<void> {
    if (typeof AudioWorkletNode === 'undefined') throw new RecordingError('unavailable')
    await this.preview(onError)
    if (this.lifetime.signal.aborted) throw new RecordingError('cancelled')
    try {
      const context = this.context
      if (!context || this.stream === undefined) throw new RecordingError('unavailable')
      // 采集用 AudioWorklet 读取已解码的浮点样本，绕开 WebKitGTK 不产出数据的 MediaRecorder
      // 编码链（见 PCM_WORKLET_SOURCE 的说明）。
      await context.audioWorklet.addModule(`data:application/javascript,${encodeURIComponent(PCM_WORKLET_SOURCE)}`)
      this.worklet = new AudioWorkletNode(context, 'dsh-pcm-collector')
      this.worklet.port.onmessage = (event: MessageEvent<Float32Array>) => {
        if (this.lifetime.signal.aborted) return
        const quantum = event.data
        this.captured.push(quantum)
        this.capturedLength += quantum.length
      }
      // An input's process() only runs while its output is part of the rendering graph, so the
      // worklet must reach the destination. Its output is silence: it never writes to outputs.
      const source = context.createMediaStreamSource(this.stream)
      source.connect(this.worklet)
      this.worklet.connect(context.destination)
    } catch (error) { await this.dispose(); throw error }
  }

  private interrupted(onError: ((error: RecordingError) => void) | undefined): void {
    if (this.lifetime.signal.aborted) return
    void this.dispose().catch(() => undefined)
    try { onError?.(new RecordingError('interrupted')) } catch (error) {
      console.error('Speech recording error handler failed', error)
    }
  }

  /**
   * Read the live microphone signal.
   * @returns the measured RMS level, or zero outside capture.
   */
  amplitude(): number {
    if (!this.analyser) return 0
    this.analyser.getFloatTimeDomainData(this.samples)
    let sum = 0
    for (const sample of this.samples) sum += sample * sample
    return Math.sqrt(sum / this.samples.length)
  }

  /**
   * Identify the input actually acquired by this operation.
   * @returns the audio track's name and device group, or undefined outside capture.
   */
  deviceInfo(): Pick<MediaDeviceInfo, 'label' | 'groupId'> | undefined {
    const track = this.stream?.getAudioTracks()[0]
    return track ? { label: track.label, groupId: track.getSettings().groupId ?? '' } : undefined
  }

  /**
   * Finish capture and resample the recording.
   * @param maxDurationSeconds - truncate timer overshoot to the Host limit.
   * @returns one recording in the Host's 16 kHz PCM16 WAV format.
   */
  async stop(maxDurationSeconds: number): Promise<Uint8Array<ArrayBuffer>> {
    const context = this.context
    if (!context || this.capturedLength === 0) { await this.dispose(); throw new RecordingError('empty') }
    try {
      this.stream?.getTracks().forEach((track) => { track.stop() })
      this.lifetime.signal.throwIfAborted()
      const collected = concatenate(this.captured, this.capturedLength)
      // Resample with the same native path the analyzer already proved out in this WebKit:
      // capture runs at the device rate (44100 here), while the Host requires 16000.
      const frames = Math.max(1, Math.floor(Math.min(collected.length / context.sampleRate, maxDurationSeconds) * 16000))
      const offline = new OfflineAudioContext(1, frames, 16000)
      const buffer = offline.createBuffer(1, collected.length, context.sampleRate)
      buffer.copyToChannel(collected, 0)
      const source = offline.createBufferSource()
      source.buffer = buffer
      source.connect(offline.destination)
      source.start()
      const resampled = await offline.startRendering()
      this.lifetime.signal.throwIfAborted()
      return encodeWave(resampled.getChannelData(0))
    } finally { await this.dispose() }
  }

  /**
   * Release this recording and invalidate pending permission grants.
   * @returns the shared release promise, including any AudioContext close failure.
   */
  dispose(): Promise<void> {
    if (!this.disposal) {
      const closing = Promise.withResolvers<void>()
      this.disposal = closing.promise
      void this.release().then(closing.resolve, closing.reject)
    }
    return this.disposal
  }

  private async release(): Promise<void> {
    this.lifetime.abort(new RecordingError('cancelled'))
    if (this.worklet) {
      // 断开而不是继续投递：release 之后到达的消息没有消费者，且会让 captured 持续增长。
      this.worklet.port.onmessage = null
      try { this.worklet.disconnect() } catch { /* 已断开或上下文已关闭，重复断开无需处理 */ }
    }
    this.stream?.getTracks().forEach((track) => { track.stop() })
    this.stream = undefined
    const context = this.context
    this.context = undefined
    this.analyser = undefined
    this.worklet = undefined
    this.captured = []
    this.capturedLength = 0
    try { if (context && context.state !== 'closed') await context.close() }
    finally { this.onDispose() }
  }
}
