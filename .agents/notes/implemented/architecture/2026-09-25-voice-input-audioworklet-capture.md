# Agent Note: AudioWorklet capture instead of MediaRecorder

Status: implemented

English | [中文](2026-09-25-voice-input-audioworklet-capture.zh.md)

## Problem

After the [desktop WebView permission fixes](2026-09-25-desktop-webview-media-permission.md) landed, voice input in the Linux desktop build reached a working microphone: the permission signal fired, enumeration returned one audio input, and the live waveform responded to speech. Recognition still always answered "未识别到语音".

The capture stage was producing nothing. `MediaRecorder` accepted `start()`, reported `state === 'recording'`, negotiated `mimeType === 'audio/mp4; codecs=mp4a'`, and then never emitted a single `dataavailable` payload — neither on a timeslice nor on an explicit `requestData()`, and without firing `onerror`. The resulting blob was zero bytes, which `Recording.stop()` maps to `RecordingError('empty')`, and the client renders that as the empty-transcript message. The waveform could not reveal this: `AnalyserNode` and `MediaRecorder` read the same stream independently, so a healthy waveform proves the stream carries signal and says nothing about the recorder.

The container's WebKitGTK advertises only `audio/mp4` for recording, and `MediaRecorder.isTypeSupported()` reports it as supported. That predicate describes theoretical support and did not predict actual function, so it could not be used as a guard.

## Decision

Collect raw PCM in an `AudioWorklet` and drop `MediaRecorder` from the capture path.

`Recording.start()` adds a small inline processor from a `data:` URL, connects the microphone source through it to the destination, and accumulates the posted render quanta. An input's `process()` only runs while its output is part of the rendering graph, so the worklet reaching the destination is required for capture; its output is silence because it never writes to `outputs`. The processor forwards each quantum to the main thread rather than encoding in the worklet, which avoids keeping a second copy of the recording and an unbounded transfer size. Only channel 0 is read, matching the mono output the Host accepts.

`Recording.stop()` concatenates the quanta and resamples them with `OfflineAudioContext` to 16 kHz, reusing the path the product already used for the decoded-recorder blob. `encodeWave()` and the Host-facing WAV format are unchanged.

The worklet path has no analogue of `recorder.onerror`, so the device-interruption signal `start(onError)` exists for now comes from each capture track's `ended` event. Without it a microphone that disappears mid-recording leaves the UI in the recording state until the duration timer fires, and the user sees the empty-transcript message instead of an interruption.

## Alternatives considered

**Encode in the worklet and post compressed chunks.** Closer to what `MediaRecorder` was doing, but it moves an encoder into the worklet for no benefit here: the Host consumes 16 kHz PCM WAV, so compression would be decoded again immediately.

**Fall back to `ScriptProcessorNode` when `AudioWorklet` is unavailable.** `ScriptProcessorNode` also worked in this container and is simpler to wire, but it is deprecated, and the alternative was verified working before the change. Carrying two capture paths would double the state to maintain for a case with no current consumer.

**Keep `MediaRecorder` and treat an empty recording as a permission-style failure.** The measured cause is a capture-stage defect, not a grant problem; reporting it as a permissions failure would send users to the wrong setting.

**Assume `isTypeSupported('audio/mp4')` proves the encoder works.** It returned true throughout the failure. It is a capability query, not a liveness check, and building the guard on it would have kept the defect.

## Consequences

Capture no longer depends on the container's encoder chain and works where `MediaRecorder` emits nothing. The recording is resampled through `OfflineAudioContext` at the device rate, which this container reports as 44.1 kHz.

`start()` now requires `AudioWorkletNode`, so the `unavailable` error covers a browser without it instead of one without `MediaRecorder`; the client treats both as the same user-visible state.

Verification needs real audio and a real decoder, because the unit tests mock both ends: the recorded evidence came from a throwaway Wails probe that ran the same worklet-plus-resample pipeline against the packaged environment, saved the resulting WAV, and fed it through the shipped SenseVoice model, which transcribed the spoken words. The same probe environment had produced zero bytes from `MediaRecorder`.
