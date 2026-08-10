# Xarlatan — Local-first low-latency voice gateway for AI agents

**Bring your agent. Xarlatan gives it ears and a voice.**

Xarlatan is a Linux voice runtime written in Go. It turns a text-oriented AI
agent into a realtime voice experience without putting an LLM, agent memory or
tools inside the voice engine.

```text
microphone
   |
   v
Silero VAD -> wake / endpointing -> streaming STT
                                      |
                                      v
                                  ACP v1
                                      |
                         +------------+------------+
                         |                         |
                    ZeroClaw                  NullClaw
                         |                         |
                         +------------+------------+
                                      |
                              user-facing text
                                      |
                                      v
                           sentence buffer -> Kokoro
                                                |
                                                v
                                             speaker
```

Xarlatan owns **audio, turn-taking and latency**. The external agent owns
**LLM selection, memory, tools, skills, permissions and agent behavior**.

## Why Xarlatan

Most agent runtimes already know how to receive text and produce text. Requiring
every agent to implement microphone capture, VAD, STT, neural TTS, playback and
barge-in duplicates a difficult realtime pipeline.

Xarlatan provides that pipeline once:

- persistent microphone capture;
- Silero VAD, wake gating and endpointing;
- persistent Whisper/OpenVINO STT with GPU preference and CPU fallback;
- partial preview plus authoritative final transcripts;
- ACP v1 over stdio for an external agent runtime;
- streamed `agent_message_chunk` text only — thoughts and raw tool data are
  never voice-eligible;
- sentence-level Kokoro TTS and persistent playback;
- wake-qualified barge-in, cancellation and stale-turn rejection;
- monotonic latency instrumentation from end-of-speech to first audible PCM.

In `voice_gateway` mode Xarlatan has **no conversational LLM**.

## Agent interoperability

v0.7.0 uses a generic ACP v1 subprocess adapter. The agent is selected only by
configuration.

### ZeroClaw

```yaml
agent:
  mode: "voice_gateway"
  acp:
    binary: "/usr/local/bin/zeroclaw"
    args: ["acp"]
    cwd: "/var/lib/xarlatan/workspace"
```

### NullClaw

```yaml
agent:
  mode: "voice_gateway"
  acp:
    binary: "/usr/local/bin/nullclaw"
    args: ["acp"]
    cwd: "/var/lib/xarlatan/workspace"
```

The ACP adapter uses portable text content blocks, requires an absolute session
workspace and supports both response shapes used by the initial runtimes:
streamed chunks plus optional terminal content, or streamed chunks with only a
terminal stop reason.

Permissions are never auto-approved. Confirmed interruption stops local audio
immediately and sends best-effort ACP cancellation; late deltas from the old
turn are discarded even if the agent runtime cannot cancel its compute
immediately.

## Build

Requirements:

- Linux x86_64;
- Go 1.21+;
- ALSA development/runtime packages;
- `bash`, `curl`, `python3`, `espeak-ng` for voice setup.

On Debian/Ubuntu:

```bash
sudo apt install -y build-essential git curl alsa-utils libasound2-dev \
  python3 python3-venv espeak-ng
```

Build the v0.7 voice gateway:

```bash
git clone https://github.com/dfc-coder/xarlatan.git
cd xarlatan
make dev-setup
make build
./bin/assistant -version
```

Expected:

```text
assistant v0.7.0
```

`make build` does **not** build a local LLM server. `make all` remains only for
the historical `legacy_native` rollback path and also builds `llama-server`.

## Install

Install Xarlatan itself:

```bash
sudo make install
```

Prepare the local voice inference runtime:

```bash
sudo bash ./scripts/setup_voice_runtime.sh
```

That script installs/configures **voice inference only**. It does not install or
configure ZeroClaw, NullClaw or another agent runtime.

Install and configure the ACP agent separately, then update:

```text
/etc/xarlatan/config.yaml
```

The default installed workspace is:

```text
/var/lib/xarlatan/workspace
```

Run in foreground:

```bash
/usr/local/bin/xarlatan --config /etc/xarlatan/config.yaml --no-tools
```

Or as the installed systemd user service:

```bash
systemctl --user daemon-reload
systemctl --user enable --now xarlatan
journalctl --user -u xarlatan -f
```

## Realtime lifecycle

Every active voice turn has its own cancellation context/TurnID.

```text
speech end
  -> final STT
  -> ACP session/prompt
  -> first user-facing text delta
  -> sentence boundary
  -> Kokoro first PCM
  -> playback
```

On confirmed barge-in:

```text
stop playback
  -> cancel/drop TTS
  -> cancel local turn
  -> send session/cancel best-effort
  -> reject stale ACP deltas
  -> recover for the next turn
```

## Latency instrumentation

Xarlatan records bounded metadata only:

```text
T0  end of speech
T1  final STT
T2  first ACP user-facing delta
T3  first synthesized PCM
T4  playback start
```

Derived measurements include STT latency, agent TTFT, TTS first-chunk latency,
playback latency, EOS-to-first-audio and interruption latency. Metrics do not
store transcript text, raw tool payloads, credentials or PCM.

## Validation

```bash
make format-check
go vet ./...
go test -count=1 ./...
go test -count=20 ./internal/acp ./internal/inference ./internal/application
go test -race -count=1 ./internal/acp ./internal/inference ./internal/application
bash scripts/tests/v07_contract_test.sh
```

Physical acceptance on the target Linux machine:

```bash
EXPECTED_VERSION=v0.7.0 \
bash ./scripts/beta_v07_acceptance.sh /etc/xarlatan/config.yaml
```

The release is physically accepted only when the report ends exactly:

```text
Final result: PASS
```

## Architecture notes

- **Events are the control plane; streams are the data plane.** High-frequency
  PCM does not travel through a generic event bus.
- Coordinator owns lifecycle; workers own inference work.
- STT/TTS workers are persistent to avoid model startup per turn.
- ACP is a wire boundary, not an Xarlatan-owned agent abstraction.
- `legacy_native` remains available as a rollback path during the migration,
  but it is not the v0.7 product architecture.

The v0.7 design and release evidence live under `docs/refactor/`.

## Rollback

The last installation snapshot is kept under `/var/backups/xarlatan`:

```bash
sudo make rollback
```

## License

MIT
