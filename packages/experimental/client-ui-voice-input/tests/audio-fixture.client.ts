/** Controlled browser audio devices with real Recording ownership and conversion. */
import { vi } from 'vitest'
import { Recording } from '../src/client/audio.ts'

/**
 * Install per-test audio devices; callers restore globals and release acquired recordings.
 *
 * 采集路径是 AudioWorklet（见 src/client/audio.ts 顶部注释），因此这里模拟的是
 * AudioContext.audioWorklet.addModule + AudioWorkletNode 的消息投递，而不是 MediaRecorder。
 * `emit` 用来在采集进行中向页面投递一个渲染量子，语义等于真实 worklet 的每次 process。
 *
 * @param options - device failures, an empty final recording, or a device interruption.
 * @returns the recording, device controls, and resource observations.
 */
export function captureFixture(options: { empty?: boolean; recorderError?: boolean; constructError?: boolean } = {}) {
  const trackStop = vi.fn(), close = vi.fn(async () => {}), disposed = vi.fn()
  const rendering = vi.fn(async () => ({ getChannelData: () => new Float32Array([0.5, -0.5]) }))
  // 采集期中断：真实场景是采集轨道触发 ended（麦克风被拔掉/被系统回收）。
  const trackListeners: (() => void)[] = []
  const track = {
    stop: trackStop,
    addEventListener: (_type: string, listener: () => void) => { trackListeners.push(listener) },
  }
  let deliver: ((quantum: Float32Array) => void) | undefined
  class Worklet {
    port: { onmessage: ((event: MessageEvent<Float32Array>) => void) | null } = { onmessage: null }
    constructor() {
      if (options.constructError) throw new Error('recorder unavailable')
      deliver = (quantum) => {
        if (!options.empty) this.port.onmessage?.({ data: quantum } as MessageEvent<Float32Array>)
        if (options.recorderError) trackListeners.forEach((listener) => { listener() })
      }
    }
    connect() {}
    disconnect() {}
  }
  const offline = vi.fn(function Offline(_channels: number, _frames: number, _rate: number) {
    // createBuffer 承载采集到的 PCM：新路径把 worklet 累积的样本拷进离线缓冲再重采样，
    // 少了它就只会在 stop() 里抛 TypeError，把失败原因伪装成采集问题。
    return {
      destination: {},
      createBuffer: (_channels: number, length: number, _rate: number) => ({
        length, copyToChannel: vi.fn(), getChannelData: () => new Float32Array(length),
      }),
      createBufferSource: () => ({ buffer: null, connect() {}, start() {} }),
      startRendering: rendering,
    }
  })
  vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: async () => ({ getTracks: () => [track], getAudioTracks: () => [track] }) } })
  vi.stubGlobal('AudioWorkletNode', Worklet)
  vi.stubGlobal('AudioContext', function Audio() {
    return { state: 'running', sampleRate: 16000, close,
      createMediaStreamSource: () => ({ connect: vi.fn() }),
      createAnalyser: () => ({ fftSize: 256, getFloatTimeDomainData: (buffer: Float32Array) => { buffer.fill(0.25) } }),
      audioWorklet: { addModule: async () => {} },
      createBuffer: (_channels: number, length: number) => ({ copyToChannel: vi.fn(), length }),
    }
  })
  vi.stubGlobal('OfflineAudioContext', offline)
  return { recording: new Recording(disposed), trackStop, close, disposed, rendering, offline,
    // 投递一个采集量子；未调用时录音为空，stop() 应报 empty。
    emit: (quantum = new Float32Array([0.25, -0.25])) => { deliver?.(quantum) },
    // 模拟采集期设备中断：真实场景由采集轨道的 ended 事件承担（origin 见 audio.ts）。
    failRecorder: () => { trackListeners.forEach((listener) => { listener() }) } }
}
