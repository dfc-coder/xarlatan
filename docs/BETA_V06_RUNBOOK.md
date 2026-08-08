# Xarlatan v0.6.0-beta.1 — ZeroClaw Voice Gateway

This beta makes ZeroClaw the agent plane and Xarlatan the local voice gateway.

```text
microphone -> Silero CPU -> OpenVINO Whisper -> ZeroClaw ACP
ZeroClaw text deltas -> sentence buffer -> Kokoro -> persistent playback
```

## 1. Update and build

```bash
cd ~/Documents/projects/xarlatan
git switch main
git pull --ff-only
make clean build
./bin/assistant -version
```

Expected:

```text
assistant v0.6.0-beta.1
```

`make build` intentionally does not build `llama-server`. The legacy native
agent can still be built explicitly with `make all` for rollback comparison.

## 2. Configure ZeroClaw

Install ZeroClaw using its supported installation method, then configure it as
the desktop user:

```bash
zeroclaw quickstart
```

Xarlatan expects a dispatchable ZeroClaw agent alias named `xarlatan` by
default. The ZeroClaw configuration remains in the user's own
`~/.zeroclaw/config.toml`; Xarlatan does not copy credentials into its config.

## 3. Install Xarlatan

```bash
sudo bash ./scripts/install.sh
systemctl --user daemon-reload
```

This installs the Go gateway and Silero native runtime. `llama-server` is not a
required v0.6 artifact.

## 4. Prepare OpenVINO Whisper + Kokoro

System prerequisites include `python3`, Python venv support, `curl`, and
`espeak-ng`.

```bash
sudo bash ./scripts/setup_voice_runtime.sh
```

The setup creates isolated environments:

```text
/opt/xarlatan/voice/stt-venv
  OpenVINO GenAI + Whisper

/opt/xarlatan/voice/tts-venv
  kokoro-onnx + ONNX Runtime OpenVINO EP
```

It also prepares:

```text
/var/lib/xarlatan/models/openvino/whisper-base
/var/lib/xarlatan/models/openvino/kokoro/kokoro-v1.0.onnx
/var/lib/xarlatan/models/openvino/kokoro/voices-v1.0.bin
```

GPU discovery is printed explicitly. If the Intel GPU is unavailable, the beta
must report the CPU fallback rather than failing silently.

The shared Silero model must already exist at:

```text
/var/lib/xarlatan/models/vad/silero_vad.onnx
```

A v0.5 installation normally already has this model. Otherwise run `make
models` before installing.

## 5. Verify installed state

```bash
env -u LD_LIBRARY_PATH /usr/local/bin/xarlatan -version
/usr/local/bin/zeroclaw --help >/dev/null
/opt/xarlatan/voice/stt-venv/bin/python - <<'PY'
import openvino as ov
print(ov.Core().available_devices)
PY
/opt/xarlatan/voice/tts-venv/bin/python - <<'PY'
import onnxruntime as ort
print(ort.get_available_providers())
PY
```

Expected Xarlatan version:

```text
assistant v0.6.0-beta.1
```

## 6. Physical acceptance

```bash
cd ~/Documents/projects/xarlatan
EXPECTED_VERSION=v0.6.0-beta.1 \
  bash ./scripts/beta_v06_acceptance.sh /etc/xarlatan/config.yaml
```

The protocol checks:

- systemd user lifecycle;
- one persistent `arecord`;
- OpenVINO device discovery and explicit fallback;
- wake rejection/acceptance;
- partial/final STT;
- real ZeroClaw streaming;
- Kokoro Spanish voice;
- streaming response before remote completion;
- physical `Charlatán, para` interruption;
- next-turn recovery;
- same capture PID;
- no self-trigger;
- reaping of owned ZeroClaw/audio/inference children.

Release acceptance requires the generated report to end exactly:

```text
Final result: PASS
```

## 7. Logs

```bash
journalctl --user -u xarlatan -f
```

Metadata-only readiness markers include the resolved STT/TTS devices and the
first ZeroClaw response-stream event. Transcript/tool contents are not added to
those diagnostics.

## 8. Rollback

Stop the service first:

```bash
systemctl --user stop xarlatan
sudo make rollback
systemctl --user daemon-reload
```

The v0.5 native path remains available during the physical beta cycle. Do not
close release issue #66 until the v0.6 physical report ends `Final result:
PASS`.
