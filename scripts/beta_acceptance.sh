#!/usr/bin/env bash
set -euo pipefail

EXPECTED_VERSION="${EXPECTED_VERSION:-v0.4.0-beta.1}"
CONFIG="${1:-${XARLATAN_CONFIG:-}}"
AUDIO_DEVICE="${AUDIO_DEVICE:-default}"
VOICE_WINDOW_SECONDS="${VOICE_WINDOW_SECONDS:-45}"
NON_INTERACTIVE="${NON_INTERACTIVE:-0}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPORT="${REPORT:-$PWD/beta-acceptance-$(date -u +%Y%m%dT%H%M%SZ).md}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [[ -z "$CONFIG" ]]; then
  [[ -f /etc/xarlatan/config.yaml ]] && CONFIG=/etc/xarlatan/config.yaml || CONFIG="$ROOT/config.yaml"
fi
if [[ -x /usr/local/bin/xarlatan ]]; then
  XARLATAN_BIN="${XARLATAN_BIN:-/usr/local/bin/xarlatan}"
  LLAMA_SERVER_BIN="${LLAMA_SERVER_BIN:-/usr/local/bin/llama-server}"
else
  XARLATAN_BIN="${XARLATAN_BIN:-$ROOT/bin/assistant}"
  LLAMA_SERVER_BIN="${LLAMA_SERVER_BIN:-$ROOT/bin/llama-server}"
fi

STATUS=PASS
AUDIO_CAPTURE=FAIL
AUDIO_PLAYBACK=PENDING
SERVICE_SMOKE=SKIPPED
VOICE_PIPELINE=PENDING

mark_fail() { STATUS=FAIL; }
confirm() {
  local prompt="$1"
  if [[ "$NON_INTERACTIVE" == 1 ]]; then
    return 1
  fi
  read -r -p "$prompt [y/N] " answer
  [[ "$answer" =~ ^[Yy]$ ]]
}

printf 'Running beta preflight...\n'
if EXPECTED_VERSION="$EXPECTED_VERSION" XARLATAN_BIN="$XARLATAN_BIN" LLAMA_SERVER_BIN="$LLAMA_SERVER_BIN" "$ROOT/scripts/preflight.sh" "$CONFIG" | tee "$TMP/preflight.log"; then
  PREFLIGHT=PASS
else
  PREFLIGHT=FAIL
  mark_fail
fi

printf '\nRecording four seconds from ALSA device %s. Speak clearly.\n' "$AUDIO_DEVICE"
if arecord -D "$AUDIO_DEVICE" -f S16_LE -r 16000 -c 1 -d 4 "$TMP/capture.wav"; then
  AUDIO_CAPTURE=PASS
else
  mark_fail
fi

if [[ "$AUDIO_CAPTURE" == PASS ]] && aplay -D "$AUDIO_DEVICE" "$TMP/capture.wav"; then
  if confirm "Did you hear your recorded voice clearly?"; then
    AUDIO_PLAYBACK=PASS
  else
    AUDIO_PLAYBACK=FAIL
    mark_fail
  fi
else
  AUDIO_PLAYBACK=FAIL
  mark_fail
fi

if command -v systemctl >/dev/null 2>&1 && [[ -f /etc/systemd/system/xarlatan.service ]]; then
  SUDO=()
  [[ "$(id -u)" -eq 0 ]] || SUDO=(sudo)
  "${SUDO[@]}" systemctl daemon-reload
  if "${SUDO[@]}" systemctl start xarlatan.service; then
    sleep 5
    if "${SUDO[@]}" systemctl is-active --quiet xarlatan.service; then
      SERVICE_SMOKE=PASS
    else
      SERVICE_SMOKE=FAIL
      mark_fail
    fi
    "${SUDO[@]}" systemctl stop xarlatan.service || true
  else
    SERVICE_SMOKE=FAIL
    mark_fail
  fi
fi

printf '\nForeground voice pipeline test. Say a short question after the assistant starts.\n'
printf 'The process will stop automatically after %s seconds.\n' "$VOICE_WINDOW_SECONDS"
set +e
timeout --signal=INT --kill-after=5s "${VOICE_WINDOW_SECONDS}s" "$XARLATAN_BIN" -config "$CONFIG" -log info -no-tools 2>&1 | tee "$TMP/foreground.log"
foreground_rc=${PIPESTATUS[0]}
set -e
if [[ "$foreground_rc" -eq 0 || "$foreground_rc" -eq 124 || "$foreground_rc" -eq 130 ]]; then
  if confirm "Did Xarlatan transcribe, answer and speak at least one response?"; then
    VOICE_PIPELINE=PASS
  else
    VOICE_PIPELINE=FAIL
    mark_fail
  fi
else
  VOICE_PIPELINE=FAIL
  mark_fail
fi

cat > "$REPORT" <<REPORT_EOF
# Xarlatan beta acceptance

- Timestamp UTC: $(date -u +%Y-%m-%dT%H:%M:%SZ)
- Host: $(hostname)
- Kernel: $(uname -srmo)
- Expected version: $EXPECTED_VERSION
- Binary: $XARLATAN_BIN
- Config: $CONFIG
- Audio device: $AUDIO_DEVICE

| Gate | Result |
|---|---|
| Preflight | $PREFLIGHT |
| ALSA capture | $AUDIO_CAPTURE |
| ALSA playback confirmed | $AUDIO_PLAYBACK |
| systemd startup/stop | $SERVICE_SMOKE |
| voice -> STT -> LLM -> TTS -> playback | $VOICE_PIPELINE |

## Final result

**$STATUS**

The report contains no transcript, prompt, model response or secret.
REPORT_EOF

printf '\nAcceptance report: %s\n' "$REPORT"
if [[ "$NON_INTERACTIVE" == 1 && ( "$AUDIO_PLAYBACK" == PENDING || "$VOICE_PIPELINE" == PENDING ) ]]; then
  exit 2
fi
[[ "$STATUS" == PASS ]]
