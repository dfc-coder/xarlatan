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

yaml_nested_value() {
  local section="$1"
  local subsection="$2"
  local key="$3"
  awk -v section="$section" -v subsection="$subsection" -v key="$key" '
    $0 ~ "^" section ":[[:space:]]*$" { in_section=1; next }
    in_section && $0 ~ "^[^[:space:]#]" { exit }
    in_section && $0 ~ "^[[:space:]]{2}" subsection ":[[:space:]]*$" { in_subsection=1; next }
    in_subsection && $0 ~ "^[[:space:]]{2}[^[:space:]#]" { exit }
    in_subsection && $0 ~ "^[[:space:]]{4}" key ":[[:space:]]*" {
      line=$0
      sub("^[[:space:]]{4}" key ":[[:space:]]*", "", line)
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

infer_vad_model_path() {
  local stt_encoder_path="$1"
  local model_root
  if [[ -n "${XARLATAN_VAD_MODEL:-}" ]]; then
    if [[ "$XARLATAN_VAD_MODEL" = /* ]]; then
      printf '%s' "$XARLATAN_VAD_MODEL"
    else
      resolve_config_path "$XARLATAN_VAD_MODEL"
    fi
    return
  fi
  model_root="$(dirname "$(dirname "$(dirname "$stt_encoder_path")")")"
  printf '%s/vad/silero_vad.onnx' "$model_root"
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

if [[ "$(uname -s)" == Linux ]]; then
  pass "Linux host"
else
  fail "Linux is required"
fi
if [[ "$(uname -m)" == x86_64 ]]; then
  pass "x86_64 architecture"
else
  warn "untested architecture: $(uname -m)"
fi

for command in arecord aplay curl sha256sum tar timeout ldd; do
  check_command "$command"
done

if [[ -f "$CONFIG" ]]; then
  pass "configuration: $CONFIG"
else
  fail "configuration not found: $CONFIG"
fi
if [[ -x "$XARLATAN_BIN" ]]; then
  pass "assistant binary: $XARLATAN_BIN"
else
  fail "assistant binary not executable: $XARLATAN_BIN"
fi
AGENT_MODE="legacy_native"
if [[ -f "$CONFIG" ]]; then
  AGENT_MODE="$(yaml_value agent mode)"
  AGENT_MODE="${AGENT_MODE:-legacy_native}"
fi
if [[ "$AGENT_MODE" == "legacy_native" ]]; then
  if [[ -x "$LLAMA_SERVER_BIN" ]]; then
    pass "llama-server binary: $LLAMA_SERVER_BIN"
  else
    fail "llama-server not executable: $LLAMA_SERVER_BIN"
  fi
else
  pass "voice_gateway does not require llama-server"
fi

if [[ -x "$XARLATAN_BIN" ]]; then
  actual_version="$(env -u LD_LIBRARY_PATH "$XARLATAN_BIN" -version 2>/dev/null || true)"
  if [[ "$actual_version" == "assistant $EXPECTED_VERSION" ]]; then
    pass "version without LD_LIBRARY_PATH: $actual_version"
  else
    fail "version/linker mismatch: got '${actual_version:-empty}', want 'assistant $EXPECTED_VERSION'"
  fi

  linkage="$(env -u LD_LIBRARY_PATH ldd "$XARLATAN_BIN" 2>&1 || true)"
  if grep -q 'not found' <<<"$linkage"; then
    fail "assistant has unresolved runtime libraries"
  else
    pass "assistant runtime libraries resolved without shell environment"
  fi
fi

if [[ -f "$CONFIG" ]]; then
  if [[ "$AGENT_MODE" == "voice_gateway" ]]; then
    acp_binary="$(yaml_nested_value agent acp binary)"
    acp_cwd="$(yaml_nested_value agent acp cwd)"
    worker_script="$(yaml_nested_value voice worker script)"
    vad_model="$(yaml_nested_value voice vad model)"
    stt_python="$(yaml_nested_value voice stt python)"
    stt_model_dir="$(yaml_nested_value voice stt model_dir)"
    tts_python="$(yaml_nested_value voice tts python)"
    tts_model_dir="$(yaml_nested_value voice tts model_dir)"
    tts_voice_file="$(yaml_nested_value voice tts voice_file)"

    if [[ -n "$acp_binary" && -x "$acp_binary" ]]; then
      pass "ACP executable: $acp_binary"
    else
      fail "ACP executable missing or not executable: ${acp_binary:-unset}"
    fi
    if [[ -n "$acp_cwd" && "$acp_cwd" == /* && -d "$acp_cwd" ]]; then
      pass "ACP workspace: $acp_cwd"
    else
      fail "ACP workspace must be an existing absolute directory: ${acp_cwd:-unset}"
    fi
    check_file "voice worker" "$worker_script"
    check_file "Silero VAD model" "$vad_model"
    if [[ -x "$stt_python" ]]; then pass "STT Python: $stt_python"; else fail "STT Python not executable: $stt_python"; fi
    if [[ -d "$stt_model_dir" ]]; then pass "Whisper model directory: $stt_model_dir"; else fail "Whisper model directory missing: $stt_model_dir"; fi
    if [[ -x "$tts_python" ]]; then pass "TTS Python: $tts_python"; else fail "TTS Python not executable: $tts_python"; fi
    if [[ -d "$tts_model_dir" ]]; then pass "Kokoro model directory: $tts_model_dir"; else fail "Kokoro model directory missing: $tts_model_dir"; fi
    check_file "Kokoro voice file" "$tts_voice_file"
  else
      stt_encoder="$(yaml_value stt encoder)"
      stt_decoder="$(yaml_value stt decoder)"
      stt_tokens="$(yaml_value stt tokens)"
      llm_model="$(yaml_value llm model)"
      tts_model="$(yaml_value tts model)"
      tts_tokens="$(yaml_value tts tokens)"
      tts_data="$(yaml_value tts data_dir)"
      llm_host="$(yaml_value llm host)"

      stt_encoder_path="$(resolve_config_path "$stt_encoder")"
      check_file "STT encoder" "$stt_encoder_path"
      check_file "STT decoder" "$(resolve_config_path "$stt_decoder")"
      check_file "STT tokens" "$(resolve_config_path "$stt_tokens")"
      check_file "Silero VAD model" "$(infer_vad_model_path "$stt_encoder_path")"
      check_file "LLM model" "$(resolve_config_path "$llm_model")"
      check_file "TTS model" "$(resolve_config_path "$tts_model")"
      check_file "TTS tokens" "$(resolve_config_path "$tts_tokens")"
      tts_data_path="$(resolve_config_path "$tts_data")"
      if [[ -d "$tts_data_path" ]]; then
        pass "TTS data: $tts_data_path"
      else
        fail "TTS data missing: $tts_data_path"
      fi

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

PIPEWIRE_RUNTIME="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
if [[ -S "$PIPEWIRE_RUNTIME/pipewire-0" ]]; then
  pass "PipeWire user session available: $PIPEWIRE_RUNTIME/pipewire-0"
else
  warn "PipeWire user session socket not found: $PIPEWIRE_RUNTIME/pipewire-0"
fi

if command -v systemctl >/dev/null 2>&1 && systemctl --user cat xarlatan.service >/dev/null 2>&1; then
  pass "systemd user service installed"
else
  warn "systemd user service not installed or user manager unavailable"
fi

printf '\nPreflight summary: %d failure(s), %d warning(s)\n' "$FAILURES" "$WARNINGS"
[[ "$FAILURES" -eq 0 ]]
