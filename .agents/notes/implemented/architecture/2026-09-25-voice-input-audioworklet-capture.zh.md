# Agent Note: 语音输入改用 AudioWorklet 采集

Status: implemented

[English](2026-09-25-voice-input-audioworklet-capture.md) | 中文

## Problem

[桌面 WebView 权限修复](2026-09-25-desktop-webview-media-permission.zh.md)落地后，Linux 桌面版的语音输入已经拿到了可用的麦克风：权限信号触发、设备枚举返回一个音频输入、实时波形随说话起伏。但识别结果始终是「未识别到语音」。

问题出在采集阶段——它什么都没产出。`MediaRecorder` 接受了 `start()`、报告 `state === 'recording'`、协商出 `mimeType === 'audio/mp4; codecs=mp4a'`，随后一个 `dataavailable` 数据都没有：无论给 timeslice 还是显式调用 `requestData()`，而且不触发 `onerror`。最终 blob 为零字节，`Recording.stop()` 把它映射为 `RecordingError('empty')`，客户端渲染成"转写为空"的提示。波形看不出这一点：`AnalyserNode` 与 `MediaRecorder` 各自独立读取同一条流，所以波形正常只能证明流里有信号，说明不了录制器的情况。

容器里的 WebKitGTK 录音只宣告 `audio/mp4`，而 `MediaRecorder.isTypeSupported()` 报告它受支持。该判定描述的是理论支持，并不能预测实际是否可用，因此无法拿它当守卫。

## Decision

用 `AudioWorklet` 采集原始 PCM，把 `MediaRecorder` 移出采集路径。

`Recording.start()` 从 `data:` URL 加载一个小型内联处理器，把麦克风源经它接到输出端，并累积投递上来的渲染量子。输入的 `process()` 只有在它的输出属于渲染图时才会执行，所以 worklet 必须接到输出端才能采集；它不写 `outputs`，因此输出是静音。处理器把每个量子转发到主线程，而不是在 worklet 里编码——这样既不会留第二份录音副本，也不会让传输量无界增长。只读通道 0，与 Host 接受的单声道输出一致。

`Recording.stop()` 拼接各量子，再用 `OfflineAudioContext` 重采样到 16 kHz，复用产品原本对解码出的录制器 blob 使用的那条路径。`encodeWave()` 与面向 Host 的 WAV 格式均未改动。

worklet 路径没有 `recorder.onerror` 的对应物，因此目前 `start(onError)` 存在的那个设备中断信号改由各采集轨道的 `ended` 事件承担。没有它的话，录音中途消失的麦克风会让界面一直停在录音状态直到时长定时器触发，用户看到的是"转写为空"而不是设备中断。

## Alternatives considered

**在 worklet 里编码并投递压缩块。** 更接近 `MediaRecorder` 原本做的事，但这里没有收益却要把编码器搬进 worklet：Host 消费的是 16 kHz PCM WAV，压缩后的数据马上又会被解码回来。

**在 `AudioWorklet` 不可用时退回 `ScriptProcessorNode`。** `ScriptProcessorNode` 在这个容器里同样可用、接线也更简单，但它已被废弃，而且改动前已经验证过首选方案可用。为一个当前没有消费者的场景维护两条采集路径，等于把要维护的状态翻倍。

**保留 `MediaRecorder`，把空录音当作权限类故障上报。** 实测原因是采集阶段的缺陷，不是授权问题；按权限故障上报会把用户引到错误的设置项。

**把 `isTypeSupported('audio/mp4')` 当成编码器可用的证明。** 它在整个故障期间都返回 true。它是能力查询，不是存活检查；把守卫建立在这上面会让缺陷继续存在。

## Consequences

采集不再依赖容器的编码器链，在 `MediaRecorder` 什么都不产出的环境里也能工作。录音按设备采样率经 `OfflineAudioContext` 重采样，本容器报告为 44.1 kHz。

`start()` 现在要求 `AudioWorkletNode`，所以 `unavailable` 覆盖的是没有它的浏览器，而不再是没有 `MediaRecorder` 的浏览器；客户端把两者视为同一个用户可见状态。

验证需要真实音频与真实解码器，因为单元测试把两端都 mock 掉了：实测证据来自一个一次性的 Wails 探针，它在打包环境里跑同一条 worklet 加重采样的管线、保存产出的 WAV，再送进随产品交付的 SenseVoice 模型，模型转写出了所说的话。同一个探针环境此前从 `MediaRecorder` 拿到的是零字节。
