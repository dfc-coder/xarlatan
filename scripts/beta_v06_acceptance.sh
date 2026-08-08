#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_PATH="${1:-/etc/xarlatan/config.yaml}"
EXPECTED_VERSION="${EXPECTED_VERSION:-v0.6.0-beta.1}"
ASSISTANT_BIN="${ASSISTANT_BIN:-/usr/local/bin/xarlatan}"
SERVICE_NAME="${SERVICE_NAME:-xarlatan}"
STT_PYTHON="${STT_PYTHON:-/opt/xarlatan/voice/stt-venv/bin/python}"
TTS_PYTHON="${TTS_PYTHON:-/opt/xarlatan/voice/tts-venv/bin/python}"
REPORT_PATH="${ROOT_DIR}/beta-acceptance-$(date -u +%Y%m%dT%H%M%SZ).md"
RESULTS_FILE="$(mktemp)"
INTERACTION_LOG="$(mktemp)"
FAILURES=0
FOREGROUND_PID=""
CAPTURE_PID_INITIAL=""

cleanup() {
  if [[ -n "${FOREGROUND_PID}" ]] && kill -0 "${FOREGROUND_PID}" 2>/dev/null; then
    kill -INT "${FOREGROUND_PID}" 2>/dev/null || true
    for _ in {1..40}; do
      kill -0 "${FOREGROUND_PID}" 2>/dev/null || break
      sleep 0.1
    done
    kill -TERM "${FOREGROUND_PID}" 2>/dev/null || true
  fi
  rm -f "${RESULTS_FILE}" "${INTERACTION_LOG}"
}
trap cleanup EXIT

record_pass() { printf 'PASS  %s\n' "$1" | tee -a "${RESULTS_FILE}"; }
record_fail() { printf 'FAIL  %s\n' "$1" | tee -a "${RESULTS_FILE}"; FAILURES=$((FAILURES + 1)); }

ask_yes() {
  local answer=""
  read -r -p "$1 [y/N] " answer || true
  [[ "${answer}" =~ ^[Yy]$ ]]
}

wait_for_single_arecord() {
  local parent="$1"
  local pid=""
  for _ in {1..240}; do
    mapfile -t captures < <(pgrep -P "${parent}" -x arecord 2>/dev/null || true)
    if [[ "${#captures[@]}" -eq 1 ]]; then
      pid="${captures[0]}"
      printf '%s\n' "${pid}"
      return 0
    fi
    kill -0 "${parent}" 2>/dev/null || return 1
    sleep 0.1
  done
  return 1
}

wait_for_log() {
  local pattern="$1"
  local attempts="${2:-300}"
  for ((i=0; i<attempts; i++)); do
    grep -Fq "${pattern}" "${INTERACTION_LOG}" && return 0
    [[ -z "${FOREGROUND_PID}" ]] || kill -0 "${FOREGROUND_PID}" 2>/dev/null || return 1
    sleep 0.1
  done
  return 1
}

owned_descendants() {
  local root="$1"
  local frontier=("${root}")
  local next=()
  local current child
  while [[ "${#frontier[@]}" -gt 0 ]]; do
    next=()
    for current in "${frontier[@]}"; do
      while read -r child; do
        [[ -n "${child}" ]] || continue
        printf '%s\n' "${child}"
        next+=("${child}")
      done < <(pgrep -P "${current}" 2>/dev/null || true)
    done
    frontier=("${next[@]}")
  done
}

printf 'Running Xarlatan v0.6 ZeroClaw Voice Gateway acceptance...\n\n'

if [[ -x "${ASSISTANT_BIN}" ]]; then record_pass "assistant executable"; else record_fail "assistant executable"; fi
ACTUAL_VERSION="$(env -u LD_LIBRARY_PATH "${ASSISTANT_BIN}" -version 2>/dev/null || true)"
if [[ "${ACTUAL_VERSION}" == "assistant ${EXPECTED_VERSION}" ]]; then
  record_pass "installed version: ${ACTUAL_VERSION}"
else
  record_fail "installed version mismatch: ${ACTUAL_VERSION}"
fi

for required in \
  /usr/local/bin/zeroclaw \
  /usr/local/lib/xarlatan/openvino_voice_worker.py \
  "${STT_PYTHON}" \
  "${TTS_PYTHON}" \
  /var/lib/xarlatan/models/openvino/whisper-base \
  /var/lib/xarlatan/models/openvino/kokoro/kokoro-v1.0.onnx \
  /var/lib/xarlatan/models/openvino/kokoro/voices-v1.0.bin \
  /var/lib/xarlatan/models/vad/silero_vad.onnx; do
  if [[ -e "${required}" ]]; then record_pass "runtime asset: ${required}"; else record_fail "runtime asset: ${required}"; fi
done

if [[ -r "${HOME}/.zeroclaw/config.toml" ]]; then
  record_pass "ZeroClaw user configuration readable"
else
  record_fail "ZeroClaw user configuration readable (${HOME}/.zeroclaw/config.toml)"
fi

OPENVINO_DEVICES="$(${STT_PYTHON} - <<'PY' 2>/dev/null || true
import openvino as ov
print(",".join(ov.Core().available_devices))
PY
)"
if [[ -n "${OPENVINO_DEVICES}" ]]; then
  record_pass "OpenVINO devices: ${OPENVINO_DEVICES}"
else
  record_fail "OpenVINO device discovery"
fi
if grep -q 'GPU' <<<"${OPENVINO_DEVICES}"; then
  record_pass "Intel GPU visible to OpenVINO"
else
  record_pass "Intel GPU unavailable; explicit CPU fallback will be accepted"
fi

ORT_PROVIDERS="$(${TTS_PYTHON} - <<'PY' 2>/dev/null || true
import onnxruntime as ort
print(",".join(ort.get_available_providers()))
PY
)"
if [[ -n "${ORT_PROVIDERS}" ]]; then
  record_pass "Kokoro ONNX providers: ${ORT_PROVIDERS}"
else
  record_fail "Kokoro ONNX provider discovery"
fi

printf '\nChecking systemd user lifecycle...\n'
systemctl --user stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
if systemctl --user start "${SERVICE_NAME}"; then
  sleep 3
  if [[ "$(systemctl --user is-active "${SERVICE_NAME}" 2>/dev/null || true)" == "active" ]]; then
    record_pass "systemd user service active"
    SERVICE_PID="$(systemctl --user show "${SERVICE_NAME}" -p MainPID --value)"
    if SERVICE_CAPTURE="$(wait_for_single_arecord "${SERVICE_PID}")"; then
      record_pass "systemd owns one persistent arecord: ${SERVICE_CAPTURE}"
    else
      record_fail "systemd owns one persistent arecord"
    fi
    mapfile -t SERVICE_DESCENDANTS < <(owned_descendants "${SERVICE_PID}")
  else
    record_fail "systemd user service active"
    SERVICE_DESCENDANTS=()
  fi
else
  record_fail "systemd user service starts"
  SERVICE_DESCENDANTS=()
fi
systemctl --user stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
sleep 2
LEAK=0
for pid in "${SERVICE_DESCENDANTS[@]:-}"; do
  [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null && LEAK=1
 done
if [[ "${LEAK}" -eq 0 ]]; then record_pass "systemd stop reaps owned children"; else record_fail "systemd stop reaps owned children"; fi

printf '\nStarting foreground physical voice test...\n'
env -u LD_LIBRARY_PATH "${ASSISTANT_BIN}" \
  --config "${CONFIG_PATH}" \
  --no-tools \
  --wake=true \
  --wake-word xarlatan \
  --wake-aliases 'charlatan,charlatán' \
  --barge-in=true \
  > >(tee "${INTERACTION_LOG}") 2>&1 &
FOREGROUND_PID=$!

if CAPTURE_PID_INITIAL="$(wait_for_single_arecord "${FOREGROUND_PID}")"; then
  record_pass "foreground owns one persistent arecord: ${CAPTURE_PID_INITIAL}"
else
  record_fail "foreground owns one persistent arecord"
fi
if wait_for_log "voice STT ready" 600; then record_pass "OpenVINO STT worker ready"; else record_fail "OpenVINO STT worker ready"; fi
if wait_for_log "voice TTS ready" 600; then record_pass "Kokoro worker ready"; else record_fail "Kokoro worker ready"; fi

printf '\nSTEP 1 — Wake rejection\n'
printf 'Say a normal sentence WITHOUT "Xarlatan".\n'
read -r -p 'Wait a few seconds and press Enter. ' _ || true
if ask_yes 'Did Xarlatan remain silent?'; then record_pass "non-wake speech ignored"; else record_fail "non-wake speech ignored"; fi

printf '\nSTEP 2 — ZeroClaw streaming + Kokoro\n'
printf 'Say: "Xarlatan, explícame en varias frases qué puedes hacer y menciona tu memoria y herramientas".\n'
read -r -p 'When speech begins, press Enter. ' _ || true
if grep -Fq '[partial]' "${INTERACTION_LOG}"; then record_pass "partial STT observed"; else record_fail "partial STT observed"; fi
if grep -Fq 'zeroclaw response stream started' "${INTERACTION_LOG}"; then record_pass "real ZeroClaw streaming delta observed"; else record_fail "real ZeroClaw streaming delta observed"; fi
if ask_yes 'Did the answer use the Kokoro Spanish voice clearly?'; then record_pass "Kokoro Spanish voice audible"; else record_fail "Kokoro Spanish voice audible"; fi
if ask_yes 'Did audio begin before the long answer had completely finished generating?'; then record_pass "streaming text reaches TTS before remote completion"; else record_fail "streaming text reaches TTS before remote completion"; fi

printf '\nSTEP 3 — Physical barge-in\n'
printf 'Ask another question that produces a long answer. While it is speaking say clearly: "Xarlatan, para".\n'
read -r -p 'After the interruption attempt, press Enter. ' _ || true
if ask_yes 'Did playback stop promptly?'; then record_pass "barge-in stops playback"; else record_fail "barge-in stops playback"; fi

printf '\nSTEP 4 — Recovery\n'
printf 'Say: "Xarlatan, dime hola".\n'
read -r -p 'Wait for the response and press Enter. ' _ || true
if ask_yes 'Did the next turn work normally?'; then record_pass "turn recovers after interruption"; else record_fail "turn recovers after interruption"; fi

if [[ -n "${CAPTURE_PID_INITIAL}" ]]; then
  mapfile -t captures < <(pgrep -P "${FOREGROUND_PID}" -x arecord 2>/dev/null || true)
  if [[ "${#captures[@]}" -eq 1 && "${captures[0]}" == "${CAPTURE_PID_INITIAL}" ]]; then
    record_pass "same arecord PID survives turns and interruption"
  else
    record_fail "same arecord PID survives turns and interruption"
  fi
fi

printf '\nSTEP 5 — Self-trigger regression\n'
printf 'Remain silent after the final response for several seconds.\n'
read -r -p 'Then press Enter. ' _ || true
if ask_yes 'Did Xarlatan remain silent without reacting to its own TTS?'; then record_pass "no TTS self-trigger"; else record_fail "no TTS self-trigger"; fi

mapfile -t FOREGROUND_DESCENDANTS < <(owned_descendants "${FOREGROUND_PID}")
kill -INT "${FOREGROUND_PID}" 2>/dev/null || true
for _ in {1..60}; do
  kill -0 "${FOREGROUND_PID}" 2>/dev/null || break
  sleep 0.1
 done
if kill -0 "${FOREGROUND_PID}" 2>/dev/null; then
  kill -TERM "${FOREGROUND_PID}" 2>/dev/null || true
  record_fail "foreground assistant stops cleanly"
else
  record_pass "foreground assistant stops cleanly"
fi
FOREGROUND_PID=""
sleep 1
LEAK=0
for pid in "${FOREGROUND_DESCENDANTS[@]:-}"; do
  [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null && LEAK=1
 done
if [[ "${LEAK}" -eq 0 ]]; then record_pass "shutdown leaves no owned ZeroClaw/audio/inference child"; else record_fail "shutdown leaves no owned child"; fi

FINAL_RESULT=PASS
[[ "${FAILURES}" -eq 0 ]] || FINAL_RESULT=FAIL
{
  printf '# Xarlatan v0.6 ZeroClaw Voice Gateway acceptance\n\n'
  printf -- '- Timestamp UTC: `%s`\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf -- '- Host: `%s`\n' "$(hostname)"
  printf -- '- Expected version: `%s`\n' "${EXPECTED_VERSION}"
  printf -- '- OpenVINO devices: `%s`\n' "${OPENVINO_DEVICES:-none}"
  printf -- '- Kokoro providers: `%s`\n\n' "${ORT_PROVIDERS:-none}"
  printf '## Gates\n\n```text\n'
  cat "${RESULTS_FILE}"
  printf '```\n\n'
  printf '## Runtime markers\n\n'
  printf -- '- Partial markers: `%s`\n' "$(grep -Fc '[partial]' "${INTERACTION_LOG}" || true)"
  printf -- '- ZeroClaw stream markers: `%s`\n' "$(grep -Fc 'zeroclaw response stream started' "${INTERACTION_LOG}" || true)"
  printf -- '- Initial arecord PID: `%s`\n\n' "${CAPTURE_PID_INITIAL:-not-observed}"
  printf 'Final result: %s\n' "${FINAL_RESULT}"
} > "${REPORT_PATH}"

printf '\nAcceptance report: %s\n' "${REPORT_PATH}"
[[ "${FINAL_RESULT}" == PASS ]]
