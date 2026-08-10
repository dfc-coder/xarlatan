#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fail() { printf 'v0.7 contract: %s\n' "$1" >&2; exit 1; }
require_text() {
  local file="$1" pattern="$2"
  grep -Fq -- "$pattern" "$ROOT/$file" || fail "$file missing: $pattern"
}
reject_text() {
  local file="$1" pattern="$2"
  if grep -Fq -- "$pattern" "$ROOT/$file"; then fail "$file must not contain: $pattern"; fi
}
require_file() { [[ -f "$ROOT/$1" ]] || fail "missing file: $1"; }
reject_path() { [[ ! -e "$ROOT/$1" ]] || fail "obsolete path present: $1"; }

require_text Makefile 'VERSION ?= v0.7.0'
require_text Makefile 'install: build'
require_text Makefile 'beta_v07_acceptance.sh'
require_text config.yaml 'mode: "voice_gateway"'
require_text config.yaml 'acp:'
require_text config.yaml 'args: ["acp"]'
require_text config.yaml 'cwd: "/var/lib/xarlatan/workspace"'
reject_text config.yaml 'agent_alias:'
reject_text packaging/config.yaml 'agent_alias:'
reject_text packaging/config.yaml '  zeroclaw:'

require_file internal/acp/client.go
require_file internal/acp/client_test.go
require_file internal/acp/runtime.go
require_file internal/acp/runtime_test.go
require_file internal/acp/responder.go
require_file internal/acp/responder_test.go
reject_path internal/zeroclaw

require_text cmd/assistant/voice_gateway_runtime.go 'acp.StartRuntime('
require_text cmd/assistant/voice_gateway_runtime.go 'cfg.Agent.ACP.Binary'
reject_text cmd/assistant/voice_gateway_runtime.go 'internal/zeroclaw'
reject_text cmd/assistant/voice_gateway_runtime.go 'zeroclaw.StartRuntime('
require_text internal/acp/client.go '"prompt": []any{'
require_text internal/acp/client.go 'kind != "agent_message_chunk"'
require_text internal/acp/client.go 'reply = streamed.String()'
require_text internal/acp/client.go '"method":  "session/cancel"'

require_text scripts/setup_voice_runtime.sh 'STT_VENV='
require_text scripts/setup_voice_runtime.sh 'TTS_VENV='
require_text scripts/setup_voice_runtime.sh 'openvino-genai==${OPENVINO_GENAI_VERSION}'
require_text scripts/setup_voice_runtime.sh 'onnxruntime-openvino==${ORT_OPENVINO_VERSION}'
reject_text scripts/setup_voice_runtime.sh 'command -v zeroclaw'
reject_text scripts/setup_voice_runtime.sh 'command -v nullclaw'
reject_text scripts/setup_voice_runtime.sh '.zeroclaw/config.toml'
reject_text scripts/setup_voice_runtime.sh '.nullclaw'

require_text packaging/systemd/xarlatan.service 'Description=Xarlatan agent-agnostic voice gateway'
require_text packaging/systemd/xarlatan.service 'ReadWritePaths=/var/lib/xarlatan -%h/.zeroclaw -%h/.nullclaw'
require_text scripts/beta_v07_acceptance.sh 'ACP response stream started'
require_text scripts/beta_v07_acceptance.sh 'same arecord PID survives turns and interruption'
require_text scripts/beta_v07_acceptance.sh 'Final result: %s'
reject_text scripts/beta_v07_acceptance.sh 'pkill'
reject_text scripts/beta_v07_acceptance.sh 'killall'

require_text README.md 'Local-first low-latency voice gateway for AI agents'
require_text README.md 'Bring your agent. Xarlatan gives it ears and a voice.'
require_text README.md 'NullClaw'
require_text README.md 'ZeroClaw'
require_text docs/BETA_V07_RUNBOOK.md 'Issue #68 stays open'
require_text CHANGELOG.md '## v0.7.0'
require_file docs/refactor/V07_AGENT_AGNOSTIC_ACP_SPEC.md

printf 'v0.7 release contract: PASS\n'
