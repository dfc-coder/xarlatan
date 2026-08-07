# WI-10A — Event-driven voice coordinator

## Status

Implementation slice for Phase 2. Depends on the physical WI-09 acceptance gate before merge to `main`.

## Goal

Replace the production voice loop composition with a `Coordinator` that owns turn lifecycle and drives long-lived independent workers for capture, STT, conversation, synthesis and playback.

## Invariants

1. The `Coordinator` is the only owner of global turn state transitions.
2. Workers execute stage work and return results; workers never advance application state themselves.
3. One shared `Responder` is used by the voice runtime, preserving the single `conversation.Session` / `AgentRuntime` path.
4. Every turn has a monotonic `TurnID` and a cancelable context.
5. A result from another `TurnID` is stale and cannot advance the active turn.
6. Public observer events remain metadata-only: no raw audio, transcript or assistant reply.
7. Stage payloads remain on internal data channels. This slice does not introduce a generic payload event bus.
8. Recoverable stage errors return the coordinator to the next listening turn.
9. Parent cancellation stops workers and the active turn.

## Runtime shape

```text
                         Coordinator
                             |
          +------------------+------------------+
          |                  |                  |
      lifecycle           TurnID            cancellation
          |
          v
  +---------------+   +-------------+   +---------------+
  | CaptureWorker |-->| STT Worker  |-->| ResponseWorker|
  +---------------+   +-------------+   +-------+-------+
                                               |
                                               v
                                      +----------------+
                                      | SynthesisWorker|
                                      +-------+--------+
                                              |
                                              v
                                      +----------------+
                                      | PlaybackWorker |
                                      +----------------+
```

Workers are started once for `Coordinator.Run` and reused across turns. `Coordinator.RunTurn` starts an isolated worker set for deterministic unit tests and one-shot callers.

## Non-goals

- Silero VAD;
- continuous audio capture;
- wake word;
- LLM token streaming;
- TTS streaming;
- persistent ALSA playback;
- tool-registry redesign;
- critical-action state machines;
- health manager.

Those are follow-up slices built on this coordinator contract.

## Tests

- complete turn ordering through worker boundaries;
- worker reuse across consecutive turns;
- monotonic `TurnID`;
- stale result rejection;
- cancellation propagation;
- recoverable error recovery;
- production composition root uses `NewCoordinator` and contains no voice-stage logic.

## Gate

Before merge:

```bash
go test -count=1 ./...
go test -count=20 ./internal/application
go test -race -count=1 ./internal/application
```

The PR remains blocked by WI-09 physical acceptance until issue #12 records `Final result: PASS`.
