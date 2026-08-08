# v0.6.0-beta.1 — ZeroClaw Voice Gateway

Issue: #66  
Parent: #60

## 1. Purpose

`v0.6.0-beta.1` changes Xarlatan's product boundary without discarding the
validated v0.5 voice runtime.

```text
ZeroClaw = agent / memory / tools / skills / sessions / approvals
Xarlatan = mic / VAD / wake / endpointing / STT / TTS / playback / barge-in / anti-echo
```

Xarlatan is the local voice gateway. ZeroClaw is the agent plane. Xarlatan
must not implement a second agent stack in the new runtime mode.

The existing native agent path remains temporarily available only as a legacy
rollback/benchmark path while the new beta is validated physically.

## 2. Delivery workflow

This beta uses one ephemeral branch and one pull request:

```text
REQUIREMENT -> DEVELOPMENT -> TEST -> SHIP -> DELETE BRANCH
```

No sub-feature branch is created for this release. TDD evidence is preserved
as commits inside `delivery/v0.6.0-beta.1`.

## 3. Control plane and data plane

The v0.5 rule remains authoritative:

```text
Events = control plane
Streams = data plane
```

High-frequency PCM is never placed on the generic event stream. Audio stays in
dedicated typed buffers/streams. Text deltas travel on the response stream.
Lifecycle, state, health and cancellation are metadata events.

Every turn has a local `TurnID`. A remote notification whose local turn is no
longer active is discarded before it can reach TTS or playback.

## 4. Voice ingress

```text
persistent microphone
        |
        v
Silero VAD (CPU)
        |
        +--> speech_started
        |
        v
endpointing / bounded utterance
        |
        v
STT
  preferred: OpenVINO ASR / Intel GPU
  fallback:  CPU
        |
        +--> partial_transcript
        +--> final_transcript (authoritative)
        |
        v
wake gate
        |
        v
ZeroClaw transport
```

The existing v0.5 invariants remain:

- one persistent capture process;
- bounded pre-roll;
- partial STT is preview-only;
- final STT alone enters wake/session/agent handling;
- wake phrase defaults to `xarlatan` with `charlatan,charlatán` aliases;
- capture remains open while playback is active, with echo-safe gating.

## 5. ZeroClaw transport

The initial transport is ACP v1 over stdio by launching `zeroclaw acp`.
ACP is the wire between the voice plane and agent plane; it is not an
`AgentBackend` abstraction owned by Xarlatan.

Required handshake/lifecycle:

1. launch child;
2. `initialize` and require protocol version 1;
3. validate server capabilities needed by the beta;
4. `session/new` once per Xarlatan runtime conversation;
5. `session/prompt` for every authoritative final transcript;
6. stream eligible `session/update` notifications;
7. `session/cancel` when the active local turn is interrupted;
8. reap the child on shutdown.

### 5.1 Speech eligibility

Only:

```text
session/update.update.sessionUpdate == "agent_message_chunk"
```

is eligible to become spoken response text.

The following are never sent to TTS:

- `agent_thought_chunk`;
- `tool_call`;
- `tool_call_update` raw input/output;
- credentials, secrets or diagnostic payloads.

The final `session/prompt` result is used to validate final reconstruction and
turn completion, not to replay text already synthesized from deltas.

### 5.2 Permissions

ZeroClaw may send `session/request_permission` as an outbound JSON-RPC request.
The v0.6 gateway has no implicit voice approval policy.

Therefore:

- never auto-approve;
- prefer an advertised reject option;
- otherwise return a cancelled outcome;
- never speak raw tool arguments while rejecting.

Interactive voice approval is explicitly deferred beyond this beta.

## 6. Voice egress

```text
ZeroClaw agent_message_chunk
        |
        v
response text stream
        |
        v
sentence buffer
        |
        v
persistent Kokoro worker
        |
        v
PCM 24 kHz mono
        |
        v
persistent playback
```

Kokoro is mandatory for v0.6. The default Spanish voice is `ef_dora` until the
physical voice comparison selects another Spanish Kokoro voice.

The model and speaker embedding are loaded once and reused across turns.
A new model process must not be created for every phrase.

For Spanish in the OpenVINO GenAI Kokoro path, `espeak-ng` is an explicit
runtime prerequisite for grapheme-to-phoneme conversion.

## 7. Intel Iris Xe acceleration

### 7.1 VAD and wake

Silero VAD, wake matching, endpointing and audio plumbing remain on CPU. Their
workloads are small and latency-sensitive; GPU transfer is not justified.

### 7.2 STT

Preferred engine:

```text
OpenVINO GenAI ASRPipeline
Whisper model
Device = GPU / GPU.0
```

Fallback is CPU. Device selection and fallback reason are observable in health
and metadata; failure to expose the Intel GPU must not crash the voice service.

Input remains normalized mono 16 kHz float32.

### 7.3 Kokoro

Kokoro uses OpenVINO GenAI `Text2SpeechPipeline` when available. GPU is a
performance preference, not a release checkbox.

At startup/acceptance the implementation benchmarks or observes real first-PCM
latency. Iris Xe remains selected only when it improves the voice latency path.
Otherwise Kokoro stays on CPU with the same voice.

### 7.4 Contention and barge-in

Voice input has priority over speech generation:

```text
confirmed interruption
 -> stop playback immediately
 -> cancel/drop pending Kokoro work
 -> send ZeroClaw session/cancel
 -> make STT the priority consumer
 -> reject late response deltas
```

A TTS inference may never make `Charlatán, para` less deterministic.

## 8. Runtime modes

Configuration exposes two modes during migration:

```yaml
agent:
  mode: voice_gateway   # target/default after physical PASS
  zeroclaw:
    binary: /usr/local/bin/zeroclaw
    agent_alias: xarlatan
```

and a temporary:

```yaml
agent:
  mode: legacy_native
```

In `voice_gateway` mode Xarlatan must not construct or start:

- `llama-server`;
- local bounded conversation memory;
- local ToolRegistry/Executor;
- local AgentRuntime.

## 9. Worker boundary

OpenVINO integration is kept outside the Coordinator. The Go voice runtime owns
lifecycle/cancellation and talks to long-lived inference workers through a
bounded binary-framed stdio protocol:

```text
fixed/header metadata frame
raw binary audio payload
```

PCM is not base64-wrapped into generic JSON events. This preserves the data
plane rule and avoids unnecessary copies/expansion.

The worker protocol has explicit request IDs, operation type, payload length,
result metadata and errors. A crashed worker produces a recoverable health
transition and bounded restart; no infinite respawn loop is allowed.

## 10. Metrics

The beta records monotonic metadata for:

```text
T0 = end of speech
T1 = authoritative final transcript
T2 = first ZeroClaw agent_message_chunk
T3 = first Kokoro PCM
T4 = first audible PCM
```

Derived metrics:

- STT latency: `T1-T0`;
- agent TTFT: `T2-T1`;
- TTS first chunk: `T3-T2`;
- playback latency: `T4-T3`;
- primary KPI: `T4-T0` (EOS -> first audible PCM);
- interrupt KPI: confirmed interruption -> playback stopped.

Metrics never contain transcript, response or tool content.

## 11. TDD contract

Tests are written before implementation for the new boundaries.

### ACP

- initialize -> session/new -> session/prompt;
- streamed message chunks stay ordered;
- final reconstruction matches the ACP final response;
- thought/tool updates do not reach the voice stream;
- permission request is safely rejected/cancelled;
- local cancellation emits `session/cancel`;
- late delta after cancellation is dropped;
- concurrent prompts on one session are rejected locally;
- malformed JSON-RPC is recoverable;
- child EOF/crash is observable and reaped;
- Close is idempotent.

### Inference worker

- binary framing round-trip;
- sample-rate/channel validation;
- TTS returns Kokoro PCM metadata;
- cancellation invalidates stale response IDs;
- GPU preference falls back to CPU with an explicit reason;
- health distinguishes ready/degraded/unavailable.

### Voice gateway

All v0.5 tests remain green, plus:

- ZeroClaw mode does not initialize the native LLM/agent stack;
- only message deltas reach sentence segmentation;
- interruption stops playback and cancels the remote turn;
- next turn succeeds;
- no self-trigger regression.

## 12. Automated quality gates

Before ship:

```bash
go test -count=1 ./...
go test -count=20 ./internal/application ./internal/audio ./internal/stt ./internal/wake ./internal/barge ./internal/zeroclaw ./internal/inference
go test -race -count=1 ./internal/application ./internal/audio ./internal/stt ./internal/wake ./internal/barge ./internal/zeroclaw ./internal/inference
```

Existing focused coverage floors are preserved. New ZeroClaw/inference packages
receive explicit coverage floors before the PR becomes ready.

## 13. Physical acceptance

On `dakota-fedora` the v0.6 acceptance report must prove:

1. installed version is `v0.6.0-beta.1`;
2. exactly one steady-state capture process;
3. Intel GPU visibility, or explicit documented CPU fallback;
4. wake reject/accept;
5. partial/final STT;
6. final transcript enters a real ZeroClaw session;
7. ZeroClaw streams response text before turn completion;
8. Kokoro Spanish voice is clear;
9. first audio starts before a long ZeroClaw response is complete;
10. `Charlatán, para` stops playback and cancels the remote turn;
11. the next turn works;
12. silence/TTS does not self-trigger;
13. shutdown leaves no owned capture, ZeroClaw or inference-worker processes.

The report must end exactly:

```text
Final result: PASS
```

The release issue remains open until that physical PASS exists.
