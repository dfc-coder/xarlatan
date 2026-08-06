#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fail() { printf 'WI-08 contract failed: %s\n' "$*" >&2; exit 1; }
assert_file() { [[ -f "$1" ]] || fail "missing file: $1"; }
assert_contains() { grep -Fq -- "$2" "$1" || fail "$1 missing: $2"; }
assert_not_contains() { ! grep -Fq -- "$2" "$1" || fail "$1 unexpectedly contains: $2"; }
staged_hashes() {
  find "$1" -type f ! -path '*/var/backups/xarlatan/*' -print0 | sort -z | xargs -0 sha256sum
}

assert_file "$ROOT/packaging/config.yaml"
assert_file "$ROOT/packaging/systemd/xarlatan.service"
assert_file "$ROOT/scripts/install.sh"
assert_file "$ROOT/scripts/uninstall.sh"
assert_file "$ROOT/scripts/rollback.sh"

assert_contains "$ROOT/Makefile" 'all: deps build'
assert_contains "$ROOT/Makefile" 'install: all'
assert_contains "$ROOT/Makefile" '$(BIN_DIR)/calibrate'
assert_not_contains "$ROOT/Makefile" 'compose.yml'
assert_not_contains "$ROOT/Makefile" 'dev-up:'

assert_contains "$ROOT/cmd/assistant/main.go" 'var buildVersion = "dev"'
assert_not_contains "$ROOT/cmd/assistant/main.go" 'flag.Bool("reset"'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'User=xarlatan'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'Group=xarlatan'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'NoNewPrivileges=true'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'ProtectSystem=strict'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'ReadWritePaths=/var/lib/xarlatan'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'DevicePolicy=closed'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'DeviceAllow=char-alsa rw'
assert_not_contains "$ROOT/packaging/systemd/xarlatan.service" '/var/backups/xarlatan'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" '--no-tools'
assert_contains "$ROOT/packaging/config.yaml" 'enabled: false'
assert_contains "$ROOT/packaging/config.yaml" '/var/lib/xarlatan/models/'
assert_contains "$ROOT/scripts/install.sh" 'BACKUPDIR="${BACKUPDIR:-/var/backups/xarlatan}"'

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$ROOT/bin"
for binary in assistant calibrate llama-server; do
  if [[ ! -x "$ROOT/bin/$binary" ]]; then
    printf '#!/usr/bin/env sh\nexit 0\n' > "$ROOT/bin/$binary"
    chmod 0755 "$ROOT/bin/$binary"
  fi
done

DESTDIR="$TMP/root" SKIP_USER=1 SKIP_SYSTEMD=1 bash "$ROOT/scripts/install.sh"
[[ -x "$TMP/root/usr/local/bin/xarlatan" ]] || fail 'xarlatan binary not installed'
[[ -x "$TMP/root/usr/local/bin/xarlatan-calibrate" ]] || fail 'calibrate binary not installed'
[[ -x "$TMP/root/usr/local/bin/llama-server" ]] || fail 'llama-server not installed'
[[ -f "$TMP/root/etc/xarlatan/config.yaml" ]] || fail 'config not installed'
[[ -f "$TMP/root/etc/systemd/system/xarlatan.service" ]] || fail 'unit not installed'
[[ -f "$TMP/root/var/backups/xarlatan/manifest" ]] || fail 'rollback manifest not installed outside service data'

first_state="$(staged_hashes "$TMP/root")"
DESTDIR="$TMP/root" SKIP_USER=1 SKIP_SYSTEMD=1 bash "$ROOT/scripts/install.sh"
second_state="$(staged_hashes "$TMP/root")"
[[ "$second_state" == "$first_state" ]] || fail 'second install changed installed state'

DESTDIR="$TMP/root" SKIP_SYSTEMD=1 bash "$ROOT/scripts/rollback.sh"
[[ -x "$TMP/root/usr/local/bin/xarlatan" ]] || fail 'rollback did not restore previous binary'

DESTDIR="$TMP/root" SKIP_SYSTEMD=1 bash "$ROOT/scripts/uninstall.sh"
[[ ! -e "$TMP/root/usr/local/bin/xarlatan" ]] || fail 'uninstall left xarlatan binary'
[[ -f "$TMP/root/etc/xarlatan/config.yaml" ]] || fail 'uninstall removed config without PURGE=1'

DESTDIR="$TMP/root" SKIP_SYSTEMD=1 PURGE=1 bash "$ROOT/scripts/uninstall.sh"
[[ ! -e "$TMP/root/etc/xarlatan/config.yaml" ]] || fail 'purge left config'
[[ ! -e "$TMP/root/var/backups/xarlatan" ]] || fail 'purge left rollback backup'

DESTDIR="$TMP/fresh" SKIP_USER=1 SKIP_SYSTEMD=1 bash "$ROOT/scripts/install.sh"
DESTDIR="$TMP/fresh" SKIP_SYSTEMD=1 bash "$ROOT/scripts/rollback.sh"
[[ ! -e "$TMP/fresh/usr/local/bin/xarlatan" ]] || fail 'first-install rollback left binary'
[[ -f "$TMP/fresh/etc/xarlatan/config.yaml" ]] || fail 'rollback removed operator config'

printf 'WI-08 installation contracts: ok\n'
