#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fail() { printf 'WI-09 contract failed: %s\n' "$*" >&2; exit 1; }
assert_file() { [[ -f "$1" ]] || fail "missing file: $1"; }
assert_contains() { grep -Fq -- "$2" "$1" || fail "$1 missing: $2"; }

for file in \
  "$ROOT/scripts/preflight.sh" \
  "$ROOT/scripts/beta_acceptance.sh" \
  "$ROOT/scripts/package_release.sh" \
  "$ROOT/docs/BETA_RUNBOOK.md" \
  "$ROOT/docs/refactor/WI-09_SPEC.md" \
  "$ROOT/docs/refactor/WI-09_EVIDENCE.md" \
  "$ROOT/CHANGELOG.md"; do
  assert_file "$file"
done

bash -n "$ROOT/scripts/preflight.sh" "$ROOT/scripts/beta_acceptance.sh" "$ROOT/scripts/package_release.sh"
assert_contains "$ROOT/Makefile" 'release-candidate:'
assert_contains "$ROOT/.github/workflows/release-candidate.yml" 'v*-beta.*'
assert_contains "$ROOT/docs/BETA_RUNBOOK.md" 'dakota-fedora'
assert_contains "$ROOT/scripts/beta_acceptance.sh" 'VOICE_PIPELINE'
assert_contains "$ROOT/scripts/beta_acceptance.sh" 'REQUIRE_SERVICE="${REQUIRE_SERVICE:-1}"'
assert_contains "$ROOT/scripts/beta_acceptance.sh" 'The report contains no transcript'

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/models/stt" "$TMP/models/llm" "$TMP/models/tts/espeak-ng-data"
for file in encoder.onnx decoder.onnx tokens.txt; do printf x > "$TMP/models/stt/$file"; done
printf x > "$TMP/models/llm/model.gguf"
printf x > "$TMP/models/tts/model.onnx"
printf x > "$TMP/models/tts/tokens.txt"

cat > "$TMP/config.yaml" <<CONFIG
stt:
  encoder: "$TMP/models/stt/encoder.onnx"
  decoder: "$TMP/models/stt/decoder.onnx"
  tokens: "$TMP/models/stt/tokens.txt"
llm:
  model: "$TMP/models/llm/model.gguf"
  host: "127.0.0.1"
tts:
  model: "$TMP/models/tts/model.onnx"
  tokens: "$TMP/models/tts/tokens.txt"
  data_dir: "$TMP/models/tts/espeak-ng-data"
tools:
  filesystem:
    enabled: false
CONFIG

cat > "$TMP/bin/assistant" <<'SH'
#!/usr/bin/env bash
[[ "${1:-}" == -version ]] && { echo 'assistant v0.4.0-beta.1'; exit 0; }
exit 0
SH
cat > "$TMP/bin/llama-server" <<'SH'
#!/usr/bin/env bash
exit 0
SH
for command in arecord aplay; do
  cat > "$TMP/bin/$command" <<'SH'
#!/usr/bin/env bash
exit 0
SH
done
chmod +x "$TMP/bin/"*

PATH="$TMP/bin:$PATH" EXPECTED_VERSION=v0.4.0-beta.1 \
  XARLATAN_BIN="$TMP/bin/assistant" LLAMA_SERVER_BIN="$TMP/bin/llama-server" \
  "$ROOT/scripts/preflight.sh" "$TMP/config.yaml" >/dev/null

printf 'WI-09 contracts: ok\n'
