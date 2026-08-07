#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_PATH="${1:-${ROOT_DIR}/config.yaml}"
EXPECTED_VERSION="${EXPECTED_VERSION:-v0.5.0-beta.1}"
AUDIO_DEVICE="${AUDIO_DEVICE:-default}"
ASSISTANT_BIN="${ASSISTANT_BIN:-/usr/local/bin/xarlatan}"
SERVICE_NAME="${SERVICE_NAME:-xarlatan}"
REPORT_PATH="${ROOT_DIR}/beta-acceptance-$(date -u +%Y%m%dT%H%M%SZ).md"
RESULTS_FILE="$(mktemp)"
INTERACTION_LOG="$(mktemp)"
FAILURES=0
FOREGROUND_PID=""
CAPTURE_PID_INITIAL=""

cleanup() {
  if [[ -n "${FOREGROUND_PID}" ]] && kill -0 "${FOREGROUND_PID}" 2>/dev/null; then
    kill -INT "${FOREGROUND_PID}" 2>/dev/null || true
    for _ in {1..20}; do
      kill -0 "${FOREGROUND_PID}" 2>/dev/null || break
      sleep 0.1
    done
    kill -TERM "${FOREGROUND_PID}" 2>/dev/null || true
  fi
  rm -f "${RESULTS_FILE}" "${INTERACTION_LOG}"
}
trap cleanup EXIT

record_pass() {
  printf 'PASS  %s\n' "$1" | tee -a "${RESULTS_FILE}"
}

record_fail() {
  printf 'FAIL  %s\n' "$1" | tee -a "${RESULTS_FILE}"
  FAILURES=$((FAILURES + 1))
}

ask_yes() {
  local prompt="$1"
  local answer=""
  read -r -p "${prompt} [y/N] " answer || true
  [[ "${answer}" =~ ^[Yy]$ ]]
}

wait_for_process() {
  local pid="$1"
  local attempts="${2:-50}"
  for ((i=0; i<attempts; i++)); do
    if kill -0 "${pid}" 2>/dev/null; then
      return 0
    fi
    sleep 0.1
  done
  return 1
}

single_arecord_child() {
  local parent_pid="$1"
  mapfile -t _capture_pids < <(pgrep -P "${parent_pid}" -x arecord 2>/dev/null || true)
  [[ "${#_capture_pids[@]}" -eq 1 ]] || return 1
  printf '%s\n' "${_capture_pids[0]}"
}

wait_for_single_arecord() {
  local parent_pid="$1"
  local attempts="${2:-200}"
  local pid=""
  for ((i=0; i<attempts; i++)); do
    if pid="$(single_arecord_child "${parent_pid}")"; then
      printf '%s\n' "${pid}"
      return 0
    fi
    if ! kill -0 "${parent_pid}" 2>/dev/null; then
      return 1
    fi
    sleep 0.1
  done
  return 1
}

printf 'Running v0.5 continuous-voice beta preflight...\n'
if "${ROOT_DIR}/scripts/preflight.sh" "${CONFIG_PATH}"; then
  record_pass "historical beta preflight"
else
  record_fail "historical beta preflight"
fi

if [[ ! -x "${ASSISTANT_BIN}" ]]; then
  record_fail "assistant binary executable: ${ASSISTANT_BIN}"
else
  record_pass "assistant binary executable: ${ASSISTANT_BIN}"
fi

ACTUAL_VERSION="$(env -u LD_LIBRARY_PATH "${ASSISTANT_BIN}" -version 2>/dev/null || true)"
if [[ "${ACTUAL_VERSION}" == "assistant ${EXPECTED_VERSION}" ]]; then
  record_pass "installed version without LD_LIBRARY_PATH: ${ACTUAL_VERSION}"
else
  record_fail "installed version mismatch: got '${ACTUAL_VERSION}', expected 'assistant ${EXPECTED_VERSION}'"
fi

LDD_OUTPUT="$(env -u LD_LIBRARY_PATH ldd "${ASSISTANT_BIN}" 2>&1 || true)"
if [[ -z "${LDD_OUTPUT}" ]] || grep -q 'not found' <<<"${LDD_OUTPUT}" || grep -qiE 'No such file|not a dynamic executable' <<<"${LDD_OUTPUT}"; then
  record_fail "assistant runtime libraries resolved"
else
  record_pass "assistant runtime libraries resolved without shell environment"
fi

printf '\nChecking installed systemd user lifecycle and persistent capture...\n'
systemctl --user stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
if systemctl --user start "${SERVICE_NAME}"; then
  if [[ "$(systemctl --user is-active "${SERVICE_NAME}" 2>/dev/null || true)" == "active" ]]; then
    record_pass "systemd user service starts"
  else
    record_fail "systemd user service active after start"
  fi

  SERVICE_PID="$(systemctl --user show "${SERVICE_NAME}" -p MainPID --value 2>/dev/null || true)"
  if [[ "${SERVICE_PID}" =~ ^[1-9][0-9]*$ ]] && kill -0 "${SERVICE_PID}" 2>/dev/null; then
    record_pass "systemd MainPID available: ${SERVICE_PID}"
    if SERVICE_CAPTURE_PID="$(wait_for_single_arecord "${SERVICE_PID}" 200)"; then
      record_pass "systemd service owns exactly one steady-state arecord child: ${SERVICE_CAPTURE_PID}"
    else
      record_fail "systemd service owns exactly one steady-state arecord child"
    fi
    mapfile -t SERVICE_CHILDREN < <(pgrep -P "${SERVICE_PID}" 2>/dev/null || true)
  else
    record_fail "systemd MainPID available"
    SERVICE_CHILDREN=()
  fi
else
  record_fail "systemd user service starts"
  SERVICE_CHILDREN=()
fi

systemctl --user stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
sleep 2
if [[ "$(systemctl --user is-active "${SERVICE_NAME}" 2>/dev/null || true)" == "inactive" ]]; then
  record_pass "systemd user service stops"
else
  record_fail "systemd user service stops"
fi

LEAKED_CHILD=0
for child in "${SERVICE_CHILDREN[@]:-}"; do
  if [[ -n "${child}" ]] && kill -0 "${child}" 2>/dev/null; then
    LEAKED_CHILD=1
  fi
done
if [[ "${LEAKED_CHILD}" -eq 0 ]]; then
  record_pass "service stop leaves no previously-owned child process"
else
  record_fail "service stop leaves no previously-owned child process"
fi

printf '\nStarting interactive continuous-voice test.\n'
printf 'Use the microphone normally. Do not type commands into Xarlatan.\n'
printf 'The wake phrase for this test is: Xarlatan.\n\n'

env -u LD_LIBRARY_PATH "${ASSISTANT_BIN}" \
  --config "${CONFIG_PATH}" \
  --no-tools \
  --wake=true \
  --wake-word xarlatan \
  --wake-aliases 'charlatan,charlatán' \
  --barge-in=true \
  > >(tee "${INTERACTION_LOG}") 2>&1 &
FOREGROUND_PID=$!

if wait_for_process "${FOREGROUND_PID}" 50; then
  record_pass "foreground continuous assistant starts"
else
  record_fail "foreground continuous assistant starts"
fi

if CAPTURE_PID_INITIAL="$(wait_for_single_arecord "${FOREGROUND_PID}" 200)"; then
  record_pass "foreground owns exactly one arecord child: ${CAPTURE_PID_INITIAL}"
else
  record_fail "foreground owns exactly one arecord child"
fi

printf '\nSTEP 1 — Wake rejection\n'
printf 'Say a normal sentence WITHOUT saying Xarlatan, for example: "qué lindo día hace hoy".\n'
read -r -p 'Wait a few seconds, then press Enter to continue. ' _ || true
if ask_yes 'Did Xarlatan remain silent and ignore that non-wake speech?'; then
  record_pass "ordinary speech without wake is ignored"
else
  record_fail "ordinary speech without wake is ignored"
fi

printf '\nSTEP 2 — Long wake-qualified turn + partial STT\n'
printf 'Say a sentence longer than one second, for example:\n'
printf '  "Xarlatan, explícame en varias frases qué puedes hacer como asistente local"\n'
read -r -p 'Wait until Xarlatan begins answering, then press Enter. ' _ || true
if ask_yes 'Did Xarlatan accept the wake-qualified command and begin answering?'; then
  record_pass "wake-qualified command produces a response"
else
  record_fail "wake-qualified command produces a response"
fi

if grep -Fq '[partial]' "${INTERACTION_LOG}"; then
  record_pass "partial STT preview observed before final handling"
else
  record_fail "partial STT preview observed before final handling"
fi

printf '\nSTEP 3 — Physical barge-in\n'
printf 'While Xarlatan is still speaking a sufficiently long answer, say clearly:\n'
printf '  "Xarlatan, para"\n'
read -r -p 'After trying the interruption, press Enter. ' _ || true
if ask_yes 'Did the active spoken answer stop promptly after "Xarlatan, para"?'; then
  record_pass "wake-qualified physical barge-in stops active playback"
else
  record_fail "wake-qualified physical barge-in stops active playback"
fi

printf '\nSTEP 4 — Recovery after interruption\n'
printf 'Say: "Xarlatan, dime hola".\n'
read -r -p 'Wait for the response, then press Enter. ' _ || true
if ask_yes 'Did the next wake-qualified turn work normally after interruption?'; then
  record_pass "new turn works after interruption"
else
  record_fail "new turn works after interruption"
fi

sleep 1
if [[ -n "${CAPTURE_PID_INITIAL}" ]]; then
  if CAPTURE_PID_FINAL="$(single_arecord_child "${FOREGROUND_PID}")" && [[ "${CAPTURE_PID_FINAL}" == "${CAPTURE_PID_INITIAL}" ]]; then
    record_pass "same arecord PID survives multiple turns and barge-in: ${CAPTURE_PID_FINAL}"
  else
    record_fail "same arecord PID survives multiple turns and barge-in"
  fi
fi

printf '\nSTEP 5 — Self-trigger regression\n'
printf 'Remain silent for several seconds after the final response.\n'
read -r -p 'When the room is quiet and Xarlatan has settled, press Enter. ' _ || true
if ask_yes 'Did Xarlatan remain silent without responding to its own TTS/echo?'; then
  record_pass "no spontaneous post-playback self-trigger"
else
  record_fail "no spontaneous post-playback self-trigger"
fi

kill -INT "${FOREGROUND_PID}" 2>/dev/null || true
for _ in {1..40}; do
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

FINAL_RESULT="PASS"
if [[ "${FAILURES}" -ne 0 ]]; then
  FINAL_RESULT="FAIL"
fi

{
  printf '# Xarlatan v0.5 beta acceptance\n\n'
  printf -- '- Timestamp (UTC): `%s`\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf -- '- Host: `%s`\n' "$(hostname)"
  printf -- '- Expected version: `%s`\n' "${EXPECTED_VERSION}"
  printf -- '- Audio device: `%s`\n' "${AUDIO_DEVICE}"
  printf -- '- Config: `%s`\n\n' "${CONFIG_PATH}"
  printf '## Gates\n\n```text\n'
  cat "${RESULTS_FILE}"
  printf '```\n\n'
  printf '## Interaction evidence\n\n'
  printf -- '- Partial marker count: `%s`\n' "$(grep -Fc '[partial]' "${INTERACTION_LOG}" || true)"
  printf -- '- Initial continuous arecord PID: `%s`\n' "${CAPTURE_PID_INITIAL:-not-observed}"
  printf '\nFinal result: %s\n' "${FINAL_RESULT}"
} > "${REPORT_PATH}"

printf '\nAcceptance report: %s\n' "${REPORT_PATH}"
if [[ "${FINAL_RESULT}" != "PASS" ]]; then
  exit 1
fi
