#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-${VERSION:-}}"
[[ -n "$VERSION" ]] || { printf 'usage: %s VERSION\n' "$0" >&2; exit 2; }
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist"
NAME="xarlatan-${VERSION}-linux-amd64"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

for artifact in assistant calibrate llama-server; do
  [[ -x "$ROOT/bin/$artifact" ]] || { printf 'missing bin/%s; run make all VERSION=%s\n' "$artifact" "$VERSION" >&2; exit 1; }
done

mkdir -p "$STAGE/$NAME/bin" "$STAGE/$NAME/packaging/systemd" "$STAGE/$NAME/scripts"
install -m755 "$ROOT/bin/assistant" "$STAGE/$NAME/bin/assistant"
install -m755 "$ROOT/bin/calibrate" "$STAGE/$NAME/bin/calibrate"
install -m755 "$ROOT/bin/llama-server" "$STAGE/$NAME/bin/llama-server"
install -m644 "$ROOT/packaging/config.yaml" "$STAGE/$NAME/packaging/config.yaml"
install -m644 "$ROOT/packaging/systemd/xarlatan.service" "$STAGE/$NAME/packaging/systemd/xarlatan.service"
for script in install.sh uninstall.sh rollback.sh download_models.sh preflight.sh beta_acceptance.sh; do
  install -m755 "$ROOT/scripts/$script" "$STAGE/$NAME/scripts/$script"
done
install -m644 "$ROOT/README.md" "$STAGE/$NAME/README.md"
install -m644 "$ROOT/docs/BETA_RUNBOOK.md" "$STAGE/$NAME/BETA_RUNBOOK.md"
install -m644 "$ROOT/CHANGELOG.md" "$STAGE/$NAME/CHANGELOG.md"
install -m644 "$ROOT/LICENSE" "$STAGE/$NAME/LICENSE"

(
  cd "$STAGE/$NAME"
  find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
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
