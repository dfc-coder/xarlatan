# WI-10B — Persistent audio playback foundation

## Status

Implementation slice for Phase 2 after WI-10A.

## Goal

Replace one-shot playback-per-response with a reusable ALSA playback process that stays alive across compatible audio writes and exposes explicit lifecycle primitives required by later streaming and barge-in work.

## Invariants

1. `Playback` owns at most one active playback process.
2. Compatible PCM writes reuse the active process.
3. A sample-rate or channel change stops the old process before creating a new one.
4. `Play` preserves the current half-duplex contract: write response audio, then run the microphone rearm guard.
5. `Write` does not run the guard and is the primitive for later TTS streaming.
6. `Stop` immediately invalidates the active process and permits a lazy restart.
7. `Close` is idempotent and permanently prevents new sessions.
8. Cancellation during a blocked write stops the process and returns the context error.
9. Process state is synchronized and must remain race-free.
10. This slice does not add Silero VAD, wake word, LLM streaming or TTS chunking.

## Runtime shape

```text
Coordinator playback worker
          |
          v
       Playback
          |
          +--> Play = Write + post-playback guard
          |
          +--> Write -----------------------+
          |                                 |
          |                      persistent aplay process
          |                                 |
          +--> Stop  ------------------> kill / discard
          |
          +--> Close -----------------> terminal release
```

The `aplay` process is lazy: the first `Write` starts it. Later compatible writes reuse it. Format changes or explicit `Stop` replace it.

## Compatibility

`application.Player` remains unchanged:

```go
type Player interface {
    Play(context.Context, audio.Buffer) error
}
```

This keeps WI-10B isolated from the Coordinator contract. Streaming will consume the additional concrete `Write`/`Stop` lifecycle only in its own slice.

## RED/GREEN tests

- two compatible `Play` calls start one playback process;
- changing PCM format restarts exactly once;
- cancellation during write stops the process;
- `Stop` permits a fresh lazy session;
- repeated `Close` is safe;
- writes after `Close` fail;
- microphone rearm runs only after successful audio write;
- production composition closes playback during shutdown.

## Gate

```bash
go test -count=1 ./...
go test -count=20 ./internal/audio ./internal/application
go test -race -count=1 ./internal/audio ./internal/application
```

## Follow-up

WI-10C introduces Silero VAD and explicit speech lifecycle events. Streaming LLM -> TTS follows after playback and VAD cancellation semantics are stable.
