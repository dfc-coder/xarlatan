# Xarlatan v0.7.0 — Agent-agnostic ACP Voice Gateway

v0.7.0 separates the voice engine from the agent runtime.

```text
microphone -> VAD / wake / endpointing -> Whisper STT -> ACP v1
                                                       |
                                      +----------------+----------------+
                                      |                                 |
                                  ZeroClaw                          NullClaw
                                      |                                 |
                                      +----------------+----------------+
                                                       |
                                            user-facing text stream
                                                       |
                                      Kokoro <- sentence buffer <-+
                                         |
                                      speaker
```

Xarlatan has no conversational LLM in `voice_gateway` mode. Install and
configure the external ACP runtime separately.

## 1. Build

```bash
cd ~/Documents/projects/xarlatan
git switch main
git pull --ff-only
make dev-setup
make clean build
./bin/assistant -version
```

Expected:

```text
assistant v0.7.0
```

`make build` must not create `bin/llama-server`.

## 2. Choose and configure an ACP runtime

Install either runtime using its own supported installation process and verify
that its ACP command is available.

### ZeroClaw

```bash
zeroclaw acp --help
```

Configure Xarlatan:

```yaml
agent:
  mode: "voice_gateway"
  acp:
    binary: "/usr/local/bin/zeroclaw"
    args: ["acp"]
    cwd: "/var/lib/xarlatan/workspace"
```

ZeroClaw owns its providers, credentials, memory, tools and agent configuration.

### NullClaw

```bash
nullclaw acp --help
```

Configure Xarlatan:

```yaml
agent:
  mode: "voice_gateway"
  acp:
    binary: "/usr/local/bin/nullclaw"
    args: ["acp"]
    cwd: "/var/lib/xarlatan/workspace"
```

NullClaw owns its providers, credentials, memory, tools and agent configuration.
The ACP `cwd` is required and must be absolute.

Changing between the two runtimes requires only changing `binary`/`args`; the
Xarlatan voice pipeline is unchanged.

## 3. Install Xarlatan

```bash
sudo bash ./scripts/install.sh
systemctl --user daemon-reload
```

The installer creates `/var/lib/xarlatan/workspace` for the ACP session.

Verify:

```bash
env -u LD_LIBRARY_PATH /usr/local/bin/xarlatan -version
```

Expected:

```text
assistant v0.7.0
```

## 4. Prepare voice inference

```bash
sudo bash ./scripts/setup_voice_runtime.sh
```

This installs only the Xarlatan voice inference stack:

- OpenVINO Whisper STT;
- Kokoro ONNX TTS;
- isolated STT/TTS Python environments;
- the persistent voice worker.

It does **not** install, copy or configure ZeroClaw or NullClaw.

The setup output reports available OpenVINO devices and Kokoro ONNX providers.
Intel GPU/OpenVINO is preferred where available; CPU fallback is valid.

## 5. Validate configuration before service startup

Confirm the selected ACP executable and workspace:

```bash
grep -A5 '^agent:' /etc/xarlatan/config.yaml
ls -ld /var/lib/xarlatan/workspace
```

Start in foreground first:

```bash
env -u LD_LIBRARY_PATH /usr/local/bin/xarlatan \
  --config /etc/xarlatan/config.yaml \
  --no-tools \
  --wake=true \
  --wake-word xarlatan \
  --wake-aliases 'charlatan,charlatán' \
  --barge-in=true
```

Expected startup markers include voice STT/TTS readiness and, when the ACP
server advertises metadata, `ACP agent ready`.

## 6. Physical demo

Use these interactions in order.

### Wake rejection

Say a sentence without the wake phrase. Xarlatan must remain silent.

### Streaming response

Say:

```text
Charlatán, explícame en varias frases qué puedes hacer.
```

The final transcript must reach the configured ACP runtime. Only
`agent_message_chunk` text is voice-eligible. Plans, thoughts, raw tool data and
permission payloads must not be spoken.

For an agent that streams user-facing text, Kokoro should begin speaking before
the whole agent turn completes.

### Barge-in

Ask for a long answer. While Xarlatan is speaking say:

```text
Charlatán, para.
```

Playback must stop promptly. Xarlatan locally cancels the active voice turn,
sends `session/cancel` best-effort and rejects every late delta from the old
turn.

Current NullClaw stdio execution may not process remote cancellation until its
synchronous agent invocation returns. This does not permit stale speech to
resume; local cancellation remains authoritative. A next prompt can remain
blocked until that ACP invocation has returned.

### Recovery

After the cancelled agent turn is no longer busy, say:

```text
Charlatán, dime hola.
```

The next turn must complete normally.

### Self-trigger

Remain silent after Xarlatan finishes speaking. Its own TTS must not create a
new turn.

## 7. Automated repository gates

```bash
make format-check
go vet ./...
go test -count=1 ./...
go test -count=20 ./internal/acp ./internal/inference ./internal/application
go test -race -count=1 ./internal/acp ./internal/inference ./internal/application
bash scripts/tests/v07_contract_test.sh
```

The generic `internal/acp` statement coverage floor is 80%.

## 8. Physical acceptance

Stop the user service before running the foreground acceptance harness:

```bash
systemctl --user stop xarlatan || true
```

Then:

```bash
EXPECTED_VERSION=v0.7.0 \
bash ./scripts/beta_v07_acceptance.sh /etc/xarlatan/config.yaml
```

The release is physically accepted only when the generated report ends exactly:

```text
Final result: PASS
```

Issue #68 stays open until that report exists.

## 9. Service mode

After foreground acceptance:

```bash
systemctl --user enable --now xarlatan
systemctl --user status xarlatan --no-pager
journalctl --user -u xarlatan -f
```

The systemd user unit is hardened but allows the two initially supported agent
runtimes to persist under `~/.zeroclaw` or `~/.nullclaw` when present.

## 10. Rollback

Code rollback point for the previous gateway is the v0.6.0-beta.1 merge commit:

```text
3e208eb2691c7e3f6253d6ca0b9eaaa97b54ac0b
```

Installation rollback:

```bash
sudo make rollback
```

v0.7 does not delete or rewrite external agent configuration or credentials.
The historical `legacy_native` mode remains untouched during this delivery.
