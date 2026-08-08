#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-${VERSION:-}}"
[[ -n "$VERSION" ]] || { printf 'usage: %s VERSION\n' "$0" >&2; exit 2; }
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist"
NAME="xarlatan-${VERSION}-linux-amd64"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

for artifact in assistant calibrate; do
  [[ -x "$ROOT/bin/$artifact" ]] || { printf 'missing bin/%s; run make build VERSION=%s\n' "$artifact" "$VERSION" >&2; exit 1; }
done
[[ -s "$ROOT/lib/libsherpa-onnx-c-api.so" ]] || {
  printf 'missing staged native runtime; run make build VERSION=%s\n' "$VERSION" >&2
  exit 1
}

mkdir -p \
  "$STAGE/$NAME/bin" \
  "$STAGE/$NAME/lib" \
  "$STAGE/$NAME/packaging/systemd" \
  "$STAGE/$NAME/scripts"
install -m755 "$ROOT/bin/assistant" "$STAGE/$NAME/bin/assistant"
install -m755 "$ROOT/bin/calibrate" "$STAGE/$NAME/bin/calibrate"
if [[ -x "$ROOT/bin/llama-server" ]]; then
  install -m755 "$ROOT/bin/llama-server" "$STAGE/$NAME/bin/llama-server"
fi
while IFS= read -r -d '' library; do
  install -m755 "$library" "$STAGE/$NAME/lib/$(basename "$library")"
done < <(find "$ROOT/lib" -maxdepth 1 -type f -print0)
install -m644 "$ROOT/packaging/config.yaml" "$STAGE/$NAME/packaging/config.yaml"
install -m644 "$ROOT/packaging/systemd/xarlatan.service" "$STAGE/$NAME/packaging/systemd/xarlatan.service"
for script in \
  install.sh uninstall.sh rollback.sh download_models.sh preflight.sh \
  beta_acceptance.sh beta_v05_acceptance.sh beta_v06_acceptance.sh \
  collect_runtime_libs.sh setup_voice_runtime.sh openvino_voice_worker.py; do
  install -m755 "$ROOT/scripts/$script" "$STAGE/$NAME/scripts/$script"
done
install -m644 "$ROOT/README.md" "$STAGE/$NAME/README.md"
install -m644 "$ROOT/docs/BETA_RUNBOOK.md" "$STAGE/$NAME/BETA_RUNBOOK.md"
if [[ -f "$ROOT/docs/BETA_V06_RUNBOOK.md" ]]; then
  install -m644 "$ROOT/docs/BETA_V06_RUNBOOK.md" "$STAGE/$NAME/BETA_V06_RUNBOOK.md"
fi
install -m644 "$ROOT/CHANGELOG.md" "$STAGE/$NAME/CHANGELOG.md"
install -m644 "$ROOT/LICENSE" "$STAGE/$NAME/LICENSE"

SUMS_TMP="$STAGE/internal-SHA256SUMS"
(
  cd "$STAGE/$NAME"
  find . -type f -print0 | sort -z | xargs -0 sha256sum > "$SUMS_TMP"
  mv "$SUMS_TMP" SHA256SUMS
)

mkdir -p "$DIST"
ARCHIVE="$DIST/$NAME.tar.gz"
EPOCH="${SOURCE_DATE_EPOCH:-0}"
tar --sort=name --mtime="@$EPOCH" --owner=0 --group=0 --numeric-owner -C "$STAGE" -czf "$ARCHIVE" "$NAME"
(
  cd "$DIST"
  sha256sum "$(basename "$ARCHIVE")" > SHA256SUMS
  sha256sum -c SHA256SUMS
)
printf 'Release candidate: %s\n' "$ARCHIVE"
