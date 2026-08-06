#!/usr/bin/env bash
set -euo pipefail

CONFIG="${1:-${XARLATAN_CONFIG:-}}"
EXPECTED_VERSION="${EXPECTED_VERSION:-v0.4.0-beta.1}"
XARLATAN_BIN="${XARLATAN_BIN:-}"
LLAMA_SERVER_BIN="${LLAMA_SERVER_BIN:-}"
FAILURES=0
WARNINGS=0

pass() { printf 'PASS  %s\n' "$*"; }
warn() { printf 'WARN  %s\n' "$*"; WARNINGS=$((WARNINGS + 1)); }
fail() { printf 'FAIL  %s\n' "$*" >&2; FAILURES=$((FAILURES + 1)); }

resolve_default_paths() {
  if [[ -z "$CONFIG" ]]; then
    if [[ -f /etc/xarlatan/config.yaml ]]; then
      CONFIG=/etc/xarlatan/config.yaml
    else
      CONFIG=config.yaml
    fi
  fi
  if [[ -z "$XARLATAN_BIN" ]]; then
    if [[ -x /usr/local/bin/xarlatan ]]; then
      XARLATAN_BIN=/usr/local/bin/xarlatan
    else
      XARLATAN_BIN=./bin/assistant
    fi
  fi
  if [[ -z "$LLAMA_SERVER_BIN" ]]; then
    if [[ -x /usr/local/bin/llama-server ]]; then
      LLAMA_SERVER_BIN=/usr/local/bin/llama-server
    else
      LLAMA_SERVER_BIN=./bin/llama-server
    fi
  fi
}

yaml_value() {
  local section="$1"
  local key="$2"
  awk -v section="$section" -v key="$key" '
    $0 ~ "^" section ":[[:space:]]*$" { inside=1; next }
    inside && $0 ~ "^[^[:space:]#]" { exit }
    inside && $0 ~ "^[[:space:]]+" key ":[[:space:]]*" {
      line=$0
      sub("^[[:space:]]+" key ":[[:space:]]*", "", line)
      sub(/[[:space:]]+#.*/, "", line)
      gsub(/^"|"$/, "", line)
      gsub(/^\047|\047$/, "", line)
      print line
      exit
    }
  ' "$CONFIG"
}

resolve_config_path() {
  local value="$1"
  if [[ "$value" = /* ]]; then
    printf '%s' "$value"
  else
    printf '%s/%s' "$(cd "$(dirname "$CONFIG")" && pwd)" "$value"
  fi
}

check_command() {
  if command -v "$1" >/dev/null 2>&1; then
    pass "command available: $1"
  else
    fail "missing command: $1"
  fi
}

check_file() {
  local label="$1"
  local path="$2"
  if [[ -s "$path" ]]; then
    pass "$label: $path"
  else
    fail "$label missing or empty: $path"
  fi
}

resolve_default_paths

[[ "$(uname -s)" == Linux ]] && pass "Linux host" || fail "Linux is required"
[[ "$(uname -m)" == x86_64 ]] && pass "x86_64 architecture" || warn "untested architecture: $(uname -m)"

for command in arecord aplay curl sha256sum tar timeout; do
  check_command "$command"
done

[[ -f "$CONFIG" ]] && pass "configuration: $CONFIG" || fail "configuration not found: $CONFIG"
[[ -x "$XARLATAN_BIN" ]] && pass "assistant binary: $XARLATAN_BIN" || fail "assistant binary not executable: $XARLATAN_BIN"
[[ -x "$LLAMA_SERVER_BIN" ]] && pass "llama-server binary: $LLAMA_SERVER_BIN" || fail "llama-server not executable: $LLAMA_SERVER_BIN"

if [[ -x "$XARLATAN_BIN" ]]; then
  actual_version="$($XARLATAN_BIN -version 2>/dev/null || true)"
  if [[ "$actual_version" == "assistant $EXPECTED_VERSION" ]]; then
    pass "version: $actual_version"
  else
    fail "version mismatch: got '${actual_version:-empty}', want 'assistant $EXPECTED_VERSION'"
  fi
fi

if [[ -f "$CONFIG" ]]; then
  stt_encoder="$(yaml_value stt encoder)"
  stt_decoder="$(yaml_value stt decoder)"
  stt_tokens="$(yaml_value stt tokens)"
  llm_model="$(yaml_value llm model)"
  tts_model="$(yaml_value tts model)"
  tts_tokens="$(yaml_value tts tokens)"
  tts_data="$(yaml_value tts data_dir)"
  llm_host="$(yaml_value llm host)"

  check_file "STT encoder" "$(resolve_config_path "$stt_encoder")"
  check_file "STT decoder" "$(resolve_config_path "$stt_decoder")"
  check_file "STT tokens" "$(resolve_config_path "$stt_tokens")"
  check_file "LLM model" "$(resolve_config_path "$llm_model")"
  check_file "TTS model" "$(resolve_config_path "$tts_model")"
  check_file "TTS tokens" "$(resolve_config_path "$tts_tokens")"
  [[ -d "$(resolve_config_path "$tts_data")" ]] && pass "TTS data: $(resolve_config_path "$tts_data")" || fail "TTS data missing: $(resolve_config_path "$tts_data")"

  if [[ "$llm_host" == 127.0.0.1 || "$llm_host" == localhost ]]; then
    pass "LLM binds to loopback: $llm_host"
  else
    fail "LLM host must be loopback for beta: ${llm_host:-unset}"
  fi

  if awk '/^[[:space:]]+filesystem:/{f=1;next} f && /^[[:space:]]{4}enabled:[[:space:]]+false/{ok=1;exit} f && /^[[:space:]]{2}[^[:space:]]/{exit} END{exit !ok}' "$CONFIG"; then
    pass "filesystem tools disabled"
  else
    fail "filesystem tools must remain disabled for beta"
  fi
fi

if arecord -L >/dev/null 2>&1; then
  pass "ALSA capture devices enumerated"
else
  fail "ALSA capture enumeration failed"
fi
if aplay -L >/dev/null 2>&1; then
  pass "ALSA playback devices enumerated"
else
  fail "ALSA playback enumeration failed"
fi

if id -nG "$(id -un)" | tr ' ' '\n' | grep -qx audio; then
  pass "current user belongs to audio group"
else
  warn "current user is not in audio group; PipeWire may still provide foreground audio"
fi

printf '\nPreflight summary: %d failure(s), %d warning(s)\n' "$FAILURES" "$WARNINGS"
[[ "$FAILURES" -eq 0 ]]
