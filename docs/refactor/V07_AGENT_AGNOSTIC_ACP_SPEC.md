# v0.7.0 — Agent-agnostic ACP Voice Gateway

Issue: #68
Parent: #60

## 1. Purpose

`v0.7.0` removes the ZeroClaw-specific dependency from Xarlatan's voice-gateway
composition. Xarlatan remains responsible only for the voice plane; the agent
runtime is selected through a generic ACP v1 subprocess boundary.

```text
Xarlatan = microphone / VAD / wake / endpointing / STT / TTS / playback /
           barge-in / latency
Agent    = LLM / memory / tools / skills / approvals / agent session
```

The first two supported ACP runtimes are ZeroClaw and NullClaw. The core must
not know which one is running.

## 2. Delivery workflow

One delivery branch and one pull request:

```text
REQUIREMENT -> DEVELOPMENT -> TEST -> REVIEW -> SHIP -> DELETE BRANCH
```

Branch: `delivery/v0.7.0`.

No functional code is written before the tests described in section 11 are
introduced and observed RED for the intended contract. A RED caused first by
formatting, syntax, fixture setup or infrastructure is invalid.

## 3. Product boundary

```text
                       AGENT PLANE

             ZeroClaw              NullClaw
                 \                    /
                  \      ACP v1      /
                   +----------------+
                            |
                            v
                    XARLATAN VOICE
                         GATEWAY
                    /             \
                  mic             speaker
```

Xarlatan does not own an LLM, agent memory, tools, skills or provider routing in
`voice_gateway` mode.

`legacy_native` remains unchanged as the existing rollback path during this
release. It is not part of the v0.7 product direction.

## 4. Generic ACP runtime

The ZeroClaw-specific package/composition is replaced by `internal/acp`.

The generic runtime owns exactly one configured child process:

```text
<binary> <args...>
```

Examples:

```text
/usr/local/bin/zeroclaw acp
/usr/local/bin/nullclaw acp
```

Required lifecycle:

1. validate executable, args and absolute workspace `cwd`;
2. spawn the configured command;
3. perform JSON-RPC 2.0 `initialize` with protocol version 1;
4. capture optional `agentInfo` metadata;
5. create one ACP session with `session/new` and absolute `cwd`;
6. submit each authoritative final transcript using `session/prompt`;
7. stream user-facing `agent_message_chunk` text only;
8. on cancellation, stop local voice work immediately, send best-effort
   `session/cancel`, and reject every late delta;
9. close stdin and reap the child on shutdown;
10. use a bounded kill fallback only if graceful shutdown exceeds the timeout.

ACP remains the wire protocol. It does not become an agent-runtime abstraction
inside Xarlatan.

## 5. ACP compatibility profile

### 5.1 Session creation

For cross-runtime compatibility, Xarlatan always sends:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "session/new",
  "params": {
    "cwd": "/absolute/workspace"
  }
}
```

NullClaw requires an absolute `cwd`. ZeroClaw accepts it and uses it as the
per-session workspace/security boundary.

ZeroClaw-specific `agentAlias` is not part of the generic core contract. Agent
selection must be performed by the external runtime's own configuration or by
configured command-line arguments when supported.

### 5.2 Prompt shape

Xarlatan sends the standard content-block form instead of a provider-specific
string shortcut:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "session/prompt",
  "params": {
    "sessionId": "s-1",
    "prompt": [
      {"type": "text", "text": "¿Qué reuniones tengo mañana?"}
    ]
  }
}
```

This shape is accepted by the target runtimes and avoids relying on a
ZeroClaw-only compatibility shortcut.

### 5.3 Speech eligibility

Only this update is voice-eligible:

```text
session/update
  update.sessionUpdate == "agent_message_chunk"
  update.content.type   == "text"
```

Never send to TTS:

- `agent_thought_chunk`;
- `plan`;
- `tool_call`;
- `tool_call_update`;
- raw tool input/output;
- permission payloads;
- credentials or diagnostic content.

### 5.4 Final-response reconstruction

Xarlatan accumulates the same user-facing `agent_message_chunk` text that it
streams toward sentence segmentation.

At terminal `session/prompt`:

- if terminal `content` is absent, the accumulated streamed text is the final
  reply (NullClaw profile);
- if terminal `content` exists, it is accepted as the authoritative buffered
  compatibility result (ZeroClaw profile);
- terminal content is never replayed through TTS after streamed deltas were
  already spoken.

### 5.5 Permissions

`session/request_permission` is never auto-approved in v0.7. A reject option is
selected when advertised; otherwise the request is cancelled. Raw tool data is
never spoken while rejecting.

### 5.6 Cancellation

Local cancellation is authoritative:

```text
confirmed barge-in
 -> stop playback
 -> cancel/drop TTS
 -> cancel local turn context
 -> send ACP session/cancel best-effort
 -> drop all late agent deltas
 -> keep the local voice path stopped and consistent
```

ZeroClaw can process its cancellation extension during an active turn.
NullClaw's current stdio implementation performs a synchronous agent invocation,
so remote compute cancellation may not be processed until that invocation
returns. Xarlatan must therefore never wait for remote cancellation to stop
local playback/TTS. While that synchronous invocation is still active the ACP
session remains busy, so a new prompt may have to wait until the runtime returns.

## 6. Configuration

Target schema:

```yaml
agent:
  mode: "voice_gateway"
  acp:
    binary: "/usr/local/bin/zeroclaw"
    args: ["acp"]
    cwd: "/var/lib/xarlatan/workspace"
```

NullClaw switch:

```yaml
agent:
  mode: "voice_gateway"
  acp:
    binary: "/usr/local/bin/nullclaw"
    args: ["acp"]
    cwd: "/var/lib/xarlatan/workspace"
```

Validation in `voice_gateway` mode:

- `agent.acp.binary` must be executable;
- `agent.acp.args` may be empty but each entry must be non-empty after trim;
- `agent.acp.cwd` is required;
- `agent.acp.cwd` must be absolute and an existing directory;
- legacy local LLM/tools settings are not validated in this mode.

The packaged configuration contains no `agent.zeroclaw` field.

## 7. Voice-runtime independence

`scripts/setup_voice_runtime.sh` owns only voice inference setup:

```text
OpenVINO Whisper
Kokoro / ONNX Runtime
voice worker assets
```

It must not:

- install or copy ZeroClaw;
- require a ZeroClaw config;
- install or copy NullClaw;
- require a NullClaw config.

Agent installation, credentials, model provider and agent configuration remain
external responsibilities.

## 8. Existing voice invariants

The v0.6 voice plane remains authoritative:

```text
persistent capture
 -> Silero VAD
 -> endpointing
 -> persistent STT worker
 -> partial preview + authoritative final transcript
 -> wake gate
 -> ACP
 -> message deltas
 -> sentence buffer
 -> persistent Kokoro worker
 -> persistent playback
```

No regression is allowed in:

- bounded pre-roll;
- wake aliases (`xarlatan`, `charlatan`, `charlatán`);
- partial/final authority;
- TurnID/context stale-work rejection;
- wake-qualified barge-in;
- anti-self-trigger behavior;
- child ownership and reaping;
- content-free latency tracing.

## 9. Metrics

The v0.6 monotonic boundaries remain:

```text
T0 = EOS
T1 = final STT
T2 = first ACP user-facing delta
T3 = first synthesized PCM
T4 = playback start
```

Derived latency remains:

- `STTLatency`;
- `AgentTTFT`;
- `TTSFirstChunkLatency`;
- `PlaybackLatency`;
- `EOSToFirstAudio`;
- `InterruptLatency`.

No metric/log may contain full transcript, raw tool data, credentials or PCM.
Agent readiness may log bounded `agentInfo` name/title/version metadata.

## 10. Failure behavior

- unsupported ACP version: startup error, child reaped;
- malformed JSON-RPC: turn/startup error with context, no secret payload dump;
- missing session ID: startup error;
- child EOF/crash: recoverable runtime error, child state reaped;
- concurrent prompt on one session: local busy error;
- permission request: reject/cancel, never auto-approve;
- local turn cancellation: immediate local stop regardless of remote support;
- shutdown: idempotent and bounded.

No retries are unbounded.

## 11. TDD contract

### RED A — generic ACP wire contract

Tests introduced before implementation must prove the current v0.6 behavior is
insufficient:

1. initialization exposes optional `agentInfo`;
2. `session/new` always sends absolute `cwd`;
3. prompt is an ACP text content-block array, not a plain string;
4. NullClaw-style terminal result without `content` reconstructs the streamed
   reply;
5. ZeroClaw-style terminal result with `content` remains compatible;
6. plan/thought/tool updates are filtered from voice deltas;
7. permission requests are rejected/cancelled;
8. cancelled turns reject late deltas.

### RED B — generic subprocess composition

1. configured `binary + args` is launched exactly;
2. no `zeroclaw` literal is needed by the generic runtime;
3. child crash is surfaced;
4. Close reaps the child and is idempotent.

### RED C — generic configuration

1. voice gateway validates `agent.acp`;
2. missing binary/cwd fails;
3. relative cwd fails;
4. blank arg fails;
5. composition imports/uses generic ACP only;
6. example/packaged config has no ZeroClaw-specific agent schema.

### Regression

All existing v0.5/v0.6 voice tests remain green after implementation.

## 12. Automated gates

Before merge:

```bash
make format-check
go vet ./...
go test -count=1 ./...
go test -count=20 ./internal/acp ./internal/inference ./internal/application
go test -race -count=1 ./internal/acp ./internal/inference ./internal/application
python3 -m py_compile scripts/openvino_voice_worker.py
shellcheck <release/runtime scripts>
make clean build
```

Expected version:

```text
assistant v0.7.0
```

`make clean build` must not create `bin/llama-server`.

Coverage target:

```text
internal/acp >= 80%
```

Historical focused coverage floors remain preserved.

## 13. RDD — release/demo deliverable

The v0.7 repository is a portfolio artifact as well as a release candidate.
The README must make the product understandable in under two minutes.

Required headline:

```text
Xarlatan — Local-first low-latency voice gateway for AI agents
```

Required positioning:

```text
Bring your agent. Xarlatan gives it ears and a voice.
```

The README/runbook demonstrate:

- microphone -> STT -> ACP -> agent -> streamed text -> Kokoro -> speaker;
- ZeroClaw and NullClaw as configuration-only alternatives;
- barge-in/cancellation;
- latency boundaries;
- Linux/local-first deployment;
- explicit statement that Xarlatan has no conversational LLM in voice-gateway
  mode.

Release artifacts:

- `CHANGELOG.md` v0.7.0 entry;
- `docs/BETA_V07_RUNBOOK.md` (release-candidate runbook despite stable target
  version);
- v0.7 release contract;
- v0.7 physical acceptance harness;
- traceability/evidence with RED/GREEN/CI references, risk and rollback.

## 14. Physical acceptance

Automated gates may merge the code. Issue #68 stays OPEN until the generated
physical Linux report ends exactly:

```text
Final result: PASS
```

The report must prove at least:

1. installed version `v0.7.0`;
2. one persistent capture process;
3. voice workers ready with explicit device/fallback metadata;
4. wake reject/accept and authoritative final STT;
5. one real configured ACP runtime receives the transcript;
6. user-facing ACP output reaches Kokoro and is audible;
7. a long response begins speaking before the agent turn finishes when the
   runtime actually streams;
8. `Charlatán, para` stops local playback promptly;
9. next turn recovers after the configured ACP runtime is no longer busy;
10. no TTS self-trigger;
11. shutdown leaves no owned audio/inference/ACP child;
12. switching ZeroClaw <-> NullClaw requires config only when both are installed.

Automated protocol conformance must cover both ZeroClaw-like and NullClaw-like
server shapes even if only one real runtime is present during physical smoke.

## 15. Rollback

- `v0.6.0-beta.1` merge commit
  `3e208eb2691c7e3f6253d6ca0b9eaaa97b54ac0b` is the code rollback point;
- no migration removes or rewrites `~/.zeroclaw` or `~/.nullclaw`;
- `legacy_native` is unchanged;
- release installation rollback remains `sudo make rollback`.

Issue #66 remains independent and must not be closed by this release.