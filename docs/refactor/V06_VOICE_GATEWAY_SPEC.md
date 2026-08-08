# v0.6.0-beta.1 — ZeroClaw Voice Gateway

Issue: #66  
Parent: #60

## 1. Purpose

`v0.6.0-beta.1` changes Xarlatan's product boundary without discarding the
validated v0.5 voice runtime:

```text
ZeroClaw = agent / memory / tools / skills / sessions / approvals
Xarlatan = mic / VAD / wake / endpointing / STT / TTS / playback / barge-in / anti-echo
```

Xarlatan is the local voice gateway. ZeroClaw is the agent plane. The previous
native agent remains temporarily available only as `legacy_native` for rollback
and comparison during the physical beta cycle.

## 2. Delivery workflow

This beta uses one ephemeral branch and one pull request:

```text
REQUIREMENT -> DEVELOPMENT -> TEST -> SHIP -> DELETE BRANCH
```

No sub-feature branches are created. RED/GREEN evidence is preserved as commits
and CI runs on `delivery/v0.6.0-beta.1`.

A RED is invalid if formatting, syntax, fixture setup or infrastructure fails
before the intended contract. `make format-check` is therefore a prerequisite
to every valid TDD transition.

## 3. Control plane and data plane

The v0.5 invariant remains authoritative:

```text
Events = control plane
Streams = data plane
```

High-frequency PCM is never placed on the generic event stream. Audio uses
bounded typed buffers and raw binary worker payloads. Text deltas use the
response stream. Lifecycle, health, state and cancellation remain metadata.

Every local turn has a `TurnID`. Late work from a cancelled/stale turn is
rejected before it can reach synthesis or playback.

## 4. Voice ingress

```text
persistent microphone
        |
        v
Silero VAD (CPU)
        |
        v
endpointing / bounded utterance
        |
        v
persistent Whisper worker
  OpenVINO GenAI ASRPipeline
  preferred: Intel GPU
  fallback: CPU
        |
        +--> partial transcript preview
        +--> authoritative final transcript
        |
        v
wake gate
        |
        v
ZeroClaw ACP
```

The v0.5 invariants remain:

- one persistent capture process;
- bounded pre-roll;
- partial STT is preview-only;
- final STT alone enters wake/session handling;
- primary wake phrase `xarlatan` with `charlatan,charlatán` aliases;
- capture remains open while playback is active with conservative echo gating.

Silero, wake matching, endpointing and audio plumbing remain CPU workloads.

## 5. ZeroClaw transport

The initial transport is ACP v1 over stdio by launching:

```text
zeroclaw acp
```

ACP is only the wire between the voice plane and agent plane. Xarlatan does not
own a second agent abstraction in `voice_gateway` mode.

Required lifecycle:

1. launch exactly one child;
2. `initialize` and require protocol version 1;
3. `session/new` once per runtime conversation;
4. `session/prompt` for every authoritative final transcript;
5. stream eligible `session/update` notifications;
6. `session/cancel` when the active local turn is interrupted/cancelled;
7. close stdin and reap the child on shutdown;
8. bounded kill fallback only if graceful shutdown exceeds its timeout.

### 5.1 Speech eligibility

Only:

```text
session/update.update.sessionUpdate == "agent_message_chunk"
```

is eligible for spoken output.

Never send these to TTS:

- `agent_thought_chunk`;
- `tool_call`;
- `tool_call_update` raw input/output;
- credentials/secrets/diagnostic payloads.

The terminal `session/prompt` result validates final reconstruction and turn
completion; it must not replay text already synthesized from deltas.

### 5.2 Permissions

`session/request_permission` is never auto-approved in v0.6.

- select a reject option when one is advertised;
- otherwise return a cancelled outcome;
- never speak raw tool arguments while rejecting.

Interactive voice approvals are deferred beyond this beta.

## 6. Voice egress

```text
ZeroClaw agent_message_chunk
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

Kokoro is mandatory for v0.6. The default Spanish voice is `ef_dora`.

The stable runtime path is:

```text
kokoro-onnx
    +
ONNX Runtime
    +
OpenVINO Execution Provider (preferred on Intel GPU)
```

If `OpenVINOExecutionProvider` or the requested Intel GPU cannot load Kokoro,
Xarlatan falls back to `CPUExecutionProvider` while keeping the same Kokoro
voice. GPU use is a latency optimization, not a release checkbox.

The Kokoro model/session is loaded once in a persistent worker and reused across
phrases/turns. A new Python/model process must not be created per sentence.

Spanish G2P requires `espeak-ng` in the physical runtime.

## 7. Intel Iris Xe acceleration

### 7.1 STT

Preferred:

```text
OpenVINO GenAI ASRPipeline
Whisper base
Device = GPU
```

Fallback is CPU and the resolved device plus fallback reason are emitted as
metadata-only health information.

Input remains normalized mono float32 at 16 kHz.

### 7.2 Kokoro

Preferred:

```text
kokoro-onnx
ONNX Runtime OpenVINO EP
Device = GPU
PERFORMANCE_HINT = LATENCY
NUM_STREAMS = 1
```

Fallback:

```text
kokoro-onnx
CPUExecutionProvider
```

STT and TTS use isolated Python virtual environments because OpenVINO GenAI and
ONNX Runtime OpenVINO follow independent native dependency trains.

### 7.3 Barge-in priority

Voice input remains authoritative:

```text
confirmed interruption
 -> stop playback
 -> cancel/drop current TTS work
 -> send ZeroClaw session/cancel
 -> reject late deltas by TurnID/context
 -> next voice turn may restart a cancelled worker cleanly
```

A blocked inference worker is killed/reaped on cancellation and restarted only
when the next operation requires it.

## 8. Runtime modes

Target/default beta mode:

```yaml
agent:
  mode: voice_gateway
```

Temporary rollback mode:

```yaml
agent:
  mode: legacy_native
```

In `voice_gateway` mode Xarlatan must not construct/start:

- `llama-server`;
- local conversation memory;
- local ToolRegistry/Executor;
- local AgentRuntime.

`make build` therefore builds the gateway without `llama-server`. `make all`
retains the legacy dependency only for rollback comparison.

## 9. Voice worker protocol

OpenVINO/Kokoro integration remains outside Coordinator. Go owns
lifecycle/cancellation and talks to persistent workers over stdio:

```text
JSON metadata header + newline
raw float32 payload bytes
```

Metadata contains request ID, operation, payload length, audio format,
requested/resolved device and bounded errors. PCM is never base64-encoded into
JSON.

Required behavior:

- request IDs detect stale/mismatched responses;
- STT rejects non-mono/non-16-kHz input;
- worker process is persistent across normal turns;
- cancellation reaps a blocked worker;
- next request can restart it;
- shutdown is idempotent and leaves no owned worker child.

## 10. Latency metadata

For a streaming turn, the trace exposes monotonic metadata boundaries:

```text
T0 = end of endpointed capture / EOS boundary
T1 = authoritative final transcript
T2 = first ZeroClaw content delta
T3 = first synthesized PCM / speaking boundary
T4 = playback-start boundary
```

Derived fields:

- `STTLatency = T1-T0`;
- `AgentTTFT = T2-T1`;
- `TTSFirstChunkLatency = T3-T2`;
- `PlaybackLatency = T4-T3`;
- `EOSToFirstAudio = T4-T0`;
- confirmed interrupt latency remains independent.

The software trace can observe playback submission/start, not the physical
speaker cone. The physical acceptance therefore separately verifies that audio
is actually audible and starts before a long ZeroClaw response is complete.

No latency record contains transcript, response, tool data or PCM.

## 11. TDD contract

### ACP

- initialize -> session/new -> session/prompt;
- streamed message chunks preserve order;
- final reconstruction matches the terminal result;
- thought/tool updates do not enter the voice stream;
- permission requests are safely rejected/cancelled;
- local cancellation emits `session/cancel`;
- late delta after cancellation is dropped;
- concurrent prompts on one session are rejected locally;
- malformed JSON-RPC/process crash/EOF are recoverable errors;
- child is reaped;
- Close is idempotent.

### Inference worker

- binary framing round-trip;
- raw float32 PCM data plane;
- sample-rate/channel validation;
- TTS returns Kokoro PCM metadata;
- cancellation invalidates/reaps blocked work;
- persistent process survives normal repeated requests;
- next request restarts after cancellation;
- GPU preference -> explicit CPU fallback;
- health reports requested/resolved devices.

### Voice gateway

All v0.5 tests remain green, plus:

- ZeroClaw mode does not initialize the native LLM/agent stack;
- gateway STT/TTS do not instantiate legacy sherpa Whisper/VITS;
- only agent message deltas reach sentence segmentation;
- interruption stops playback and cancels the remote turn;
- next turn succeeds;
- no self-trigger regression;
- latency metadata is positive, ordered and content-free.

## 12. Automated quality gates

Before ship:

```bash
make format-check
go vet ./...
go test -count=1 ./...
go test -count=20 ... ./internal/inference ./internal/zeroclaw
go test -race -count=1 ... ./internal/inference ./internal/zeroclaw
python3 -m py_compile scripts/openvino_voice_worker.py
shellcheck ...
```

Historical focused coverage floors remain unchanged. New floors:

```text
internal/inference >= 70%
internal/zeroclaw  >= 70%
```

The default build must report exactly:

```text
assistant v0.6.0-beta.1
```

and `make clean build` must not create `bin/llama-server`.

## 13. Physical acceptance

On `dakota-fedora` the v0.6 report must prove:

1. installed version `v0.6.0-beta.1`;
2. exactly one steady-state capture process;
3. OpenVINO device discovery with Intel GPU or explicit CPU fallback;
4. wake reject/accept;
5. partial/final STT;
6. final transcript reaches a real ZeroClaw session;
7. ZeroClaw streams response text before turn completion;
8. Kokoro Spanish voice is audible and clear;
9. first audible response begins before a long ZeroClaw response completes;
10. `Charlatán, para` stops playback and cancels the remote turn;
11. next turn works;
12. silence/TTS does not self-trigger;
13. shutdown leaves no owned capture, ZeroClaw or inference-worker children.

The report must end exactly:

```text
Final result: PASS
```

The release issue remains open until that physical PASS exists.
