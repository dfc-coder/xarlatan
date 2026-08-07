# WI-10C — Silero VAD speech lifecycle

## Status

Phase 2 implementation slice after the event-driven Coordinator and persistent playback foundations.

## Goal

Use sherpa-onnx's Silero VAD as the production voice-activity detector while keeping audio capture independent from the native VAD implementation. The detector must expose stable speech-start/speech-end semantics for later barge-in and streaming work.

## Design

```text
ALSA 80 ms chunks
       |
       v
    Recorder
       |
       v
 vad.Detector interface
       |
       +--> SileroDetector -> sherpa-onnx VoiceActivityDetector
       |
       +--> legacy energy VAD (calibration/tests only)
```

`internal/audio` owns capture and pre-roll. `internal/vad` owns voice segmentation. The composition root selects Silero for production.

## Runtime model

The existing sherpa-onnx Go dependency already exposes Silero VAD and the native ONNX runtime used by STT/TTS. No second ONNX runtime is introduced.

The pinned model is installed at:

```text
models/vad/silero_vad.onnx
/var/lib/xarlatan/models/vad/silero_vad.onnx
```

The path is inferred from the existing STT model root; `XARLATAN_VAD_MODEL` is available for non-standard layouts.

## Invariants

1. Production voice capture uses Silero VAD at 16 kHz.
2. `Recorder` depends only on `vad.Detector`, never sherpa types.
3. Speech lifecycle events contain metadata only.
4. Exactly one `EventSpeechStarted` and one `EventSpeechEnded` are emitted for a completed segment.
5. Once speech begins, trailing chunks remain part of the utterance until Silero queues a completed segment.
6. Existing bounded pre-roll remains the only pre-speech audio retained.
7. Empty sample chunks never reach sherpa's native `AcceptWaveform` call.
8. `Reset` clears queued/native segment state between turns.
9. `Close` releases the native VAD exactly once.
10. Missing VAD model or unsupported sample rate fails before voice runtime startup.
11. Model download is atomic and checksum-pinned.
12. This slice does not add wake word, continuous capture, barge-in or LLM/TTS streaming.

## Compatibility

The existing RMS/EMA detector remains available because `cmd/calibrate` uses its noise-floor statistics and existing unit tests exercise its state machine. It implements the same `Detector` interface but is not selected by `cmd/assistant` production composition.

Existing YAML does not gain overlapping VAD tuning fields. Silero derives:

- sample rate and maximum speech duration from `audio`;
- minimum silence from `audio.silence_duration_ms`;
- fixed model defaults appropriate to the bundled 16 kHz Silero model.

## Tests

RED/GREEN coverage includes:

- production composition requires Silero;
- standard and custom VAD model path resolution;
- missing model and unsupported sample rate rejection;
- one speech-start and one speech-end event per segment;
- completed-segment edge case cannot omit speech-start;
- reset and idempotent close;
- empty chunks are ignored safely;
- Recorder accepts a fake detector and preserves bounded pre-roll;
- preflight requires the VAD model;
- WI-09 release contracts remain intact.

## Gates

```bash
go test -count=1 ./...
go test -count=20 ./internal/vad ./internal/audio ./internal/application
go test -race -count=1 ./internal/vad ./internal/audio ./internal/application
```

CI also retains formatting, vet, focused coverage, build/version, release-contract and systemd gates.

## Rollback

Revert the WI-10C commit. `NewRecorder` and the legacy energy detector remain valid, so rollback does not require a data migration. The additional `models/vad` file is inert if the code is reverted.

## Follow-up

The next slice may consume `EventSpeechStarted` during playback to implement the cancellation path required for barge-in, followed by streaming LLM -> TTS over the persistent playback channel.
