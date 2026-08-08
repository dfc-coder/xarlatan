#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fail() { printf 'v0.6 contract: %s\n' "$1" >&2; exit 1; }
require_text() {
  local file="$1" pattern="$2"
  grep -Fq -- "$pattern" "$ROOT/$file" || fail "$file missing: $pattern"
}
reject_text() {
  local file="$1" pattern="$2"
  if grep -Fq -- "$pattern" "$ROOT/$file"; then fail "$file must not contain: $pattern"; fi
}

require_text Makefile 'VERSION ?= v0.6.0-beta.1'
require_text Makefile 'install: build'
require_text config.yaml 'mode: "voice_gateway"'
require_text config.yaml 'agent_alias: "xarlatan"'
require_text config.yaml 'stt-venv/bin/python'
require_text config.yaml 'tts-venv/bin/python'
require_text config.yaml 'voices-v1.0.bin'
require_text scripts/install.sh 'for artifact in assistant calibrate; do'
reject_text scripts/install.sh 'for artifact in assistant calibrate llama-server; do'
require_text scripts/setup_voice_runtime.sh 'STT_VENV=' 
require_text scripts/setup_voice_runtime.sh 'TTS_VENV='
require_text scripts/setup_voice_runtime.sh 'openvino-genai==${OPENVINO_GENAI_VERSION}'
require_text scripts/setup_voice_runtime.sh 'onnxruntime-openvino==${ORT_OPENVINO_VERSION}'
require_text scripts/openvino_voice_worker.py 'ASRPipeline'
require_text scripts/openvino_voice_worker.py 'OpenVINOExecutionProvider'
require_text scripts/openvino_voice_worker.py 'Kokoro.from_session'
require_text packaging/systemd/xarlatan.service 'ReadWritePaths=/var/lib/xarlatan %h/.zeroclaw'
require_text scripts/beta_v06_acceptance.sh 'zeroclaw response stream started'
require_text scripts/beta_v06_acceptance.sh 'same arecord PID survives turns and interruption'
require_text scripts/beta_v06_acceptance.sh 'Final result: %s'
reject_text scripts/beta_v06_acceptance.sh 'pkill'
reject_text scripts/beta_v06_acceptance.sh 'killall'

printf 'v0.6 release contract: PASS\n'
