#!/usr/bin/env bash
set -euo pipefail

# Release-specific physical acceptance remains isolated so historical beta
# contracts stay reproducible.
if [[ "${EXPECTED_VERSION:-}" == "v0.7.0" ]]; then
  exec bash "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/beta_v07_acceptance.sh" "$@"
fi

# Keep the historical WI-09 acceptance intact. The continuous-voice beta uses
# its dedicated protocol only when v0.5 is explicitly requested.
if [[ "${EXPECTED_VERSION:-}" == "v0.5.0-beta.1" ]]; then
  exec bash "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/beta_v05_acceptance.sh" "$@"
fi

EXPECTED_VERSION="${EXPECTED_VERSION:-v0.4.0-beta.1}"
CONFIG="${1:-${XARLATAN_CONFIG:-}}"
AUDIO_DEVICE="${AUDIO_DEVICE:-}"
VOICE_WINDOW_SECONDS="${VOICE_WINDOW_SECONDS:-45}"
NON_INTERACTIVE="${NON_INTERACTIVE:-0}"
REQUIRE_SERVICE="${REQUIRE_SERVICE:-1}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPORT="${REPORT:-$PWD/beta-acceptance-$(date -u +%Y%m%dT%H%M%SZ).md}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [[ -z "$CONFIG" ]]; then
  [[ -f /etc/xarlatan/config.yaml ]] && CONFIG=/etc/xarlatan/config.yaml || CONFIG="$ROOT/config.yaml"
fi

config_audio_device() {
  awk '
    /^audio:[[:space:]]*$/ { inside=1; next }
    inside && /^[^[:space:]#]/ { exit }
    inside && /^[[:space:]]+device:[[:space:]]*/ {
      line=$0
      sub(/^[[:space:]]+device:[[:space:]]*/, "", line)
      sub(/[[:space:]]+#.*/, "", line)
      gsub(/^"|"$/, "", line)
      gsub(/^\047|\047$/, "", line)
      print line
      exit
    }
  ' "$CONFIG"
}

CONFIG_AUDIO_DEVICE="$(config_audio_device)"
CONFIG_AUDIO_DEVICE="${CONFIG_AUDIO_DEVICE:-default}"
if [[ -z "$AUDIO_DEVICE" ]]; then
  AUDIO_DEVICE="$CONFIG_AUDIO_DEVICE"
elif [[ "$AUDIO_DEVICE" != "$CONFIG_AUDIO_DEVICE" ]]; then
  printf 'ERROR: AUDIO_DEVICE=%s differs from audio.device=%s in %s\n' \
    "$AUDIO_DEVICE" "$CONFIG_AUDIO_DEVICE" "$CONFIG" >&2
  printf 'Update the config first so manual capture and Xarlatan test the same device.\n' >&2
  exit 2
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
SERVICE_SMOKE=PENDING
VOICE_PIPELINE=PENDING
NO_SPONTANEOUS_TURNS=PENDING

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

printf '\nRecording four seconds from configured ALSA device %s. Speak clearly.\n' "$AUDIO_DEVICE"
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

if command -v systemctl >/dev/null 2>&1 && systemctl --user cat xarlatan.service >/dev/null 2>&1; then
  systemctl --user daemon-reload
  if systemctl --user start xarlatan.service; then
    sleep 5
    if systemctl --user is-active --quiet xarlatan.service; then
      SERVICE_SMOKE=PASS
    else
      SERVICE_SMOKE=FAIL
      mark_fail
    fi
    systemctl --user status xarlatan.service --no-pager > "$TMP/service-status.log" 2>&1 || true
    systemctl --user stop xarlatan.service || true
  else
    SERVICE_SMOKE=FAIL
    mark_fail
  fi
else
  if [[ "$REQUIRE_SERVICE" == 1 ]]; then
    SERVICE_SMOKE=FAIL
    mark_fail
  else
    SERVICE_SMOKE=SKIPPED
  fi
fi

printf '\nForeground voice pipeline test. Ask exactly one short question, then remain silent.\n'
printf 'The process will stop automatically after %s seconds.\n' "$VOICE_WINDOW_SECONDS"
set +e
timeout --signal=INT --kill-after=5s "${VOICE_WINDOW_SECONDS}s" "$XARLATAN_BIN" -config "$CONFIG" -log info -no-tools 2>&1 | tee "$TMP/foreground.log"
foreground_rc=${PIPESTATUS[0]}
set -e
if [[ "$foreground_rc" -eq 0 || "$foreground_rc" -eq 124 || "$foreground_rc" -eq 130 ]]; then
  if confirm "Did Xarlatan transcribe, answer and speak exactly one requested response?"; then
    VOICE_PIPELINE=PASS
  else
    VOICE_PIPELINE=FAIL
    mark_fail
  fi
  if confirm "After that response, did Xarlatan remain silent without spontaneous turns?"; then
    NO_SPONTANEOUS_TURNS=PASS
  else
    NO_SPONTANEOUS_TURNS=FAIL
    mark_fail
  fi
else
  VOICE_PIPELINE=FAIL
  NO_SPONTANEOUS_TURNS=FAIL
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
| systemd user startup/stop | $SERVICE_SMOKE |
| voice -> STT -> LLM -> TTS -> playback | $VOICE_PIPELINE |
| no spontaneous post-playback turns | $NO_SPONTANEOUS_TURNS |

## Final result

**$STATUS**

The report contains no transcript, prompt, model response or secret.
REPORT_EOF

printf '\nAcceptance report: %s\n' "$REPORT"
if [[ "$NON_INTERACTIVE" == 1 ]]; then
  exit 2
fi
[[ "$STATUS" == PASS ]]
