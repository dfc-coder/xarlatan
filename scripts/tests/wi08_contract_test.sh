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
assert_file "$ROOT/scripts/collect_runtime_libs.sh"
assert_file "$ROOT/scripts/install.sh"
assert_file "$ROOT/scripts/uninstall.sh"
assert_file "$ROOT/scripts/rollback.sh"

bash -n \
  "$ROOT/scripts/collect_runtime_libs.sh" \
  "$ROOT/scripts/install.sh" \
  "$ROOT/scripts/uninstall.sh" \
  "$ROOT/scripts/rollback.sh"

assert_contains "$ROOT/Makefile" 'all: deps build'
if grep -Eq 'VERSION \?= v0\.(6\.0-beta\.1|7\.0)' "$ROOT/Makefile"; then
  assert_contains "$ROOT/Makefile" 'install: build'
else
  assert_contains "$ROOT/Makefile" 'install: all'
fi
assert_contains "$ROOT/Makefile" '$(BIN_DIR)/calibrate'
assert_not_contains "$ROOT/Makefile" 'compose.yml'
assert_not_contains "$ROOT/Makefile" 'dev-up:'

assert_contains "$ROOT/cmd/assistant/main.go" 'var buildVersion = "dev"'
assert_not_contains "$ROOT/cmd/assistant/main.go" 'flag.Bool("reset"'
assert_not_contains "$ROOT/packaging/systemd/xarlatan.service" 'User='
assert_not_contains "$ROOT/packaging/systemd/xarlatan.service" 'Group='
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'After=pipewire.service wireplumber.service'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'WantedBy=default.target'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'NoNewPrivileges=true'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'ProtectSystem=strict'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" 'ReadWritePaths=/var/lib/xarlatan'
assert_not_contains "$ROOT/packaging/systemd/xarlatan.service" 'DevicePolicy='
assert_not_contains "$ROOT/packaging/systemd/xarlatan.service" '/var/backups/xarlatan'
assert_contains "$ROOT/packaging/systemd/xarlatan.service" '--no-tools'
assert_contains "$ROOT/packaging/config.yaml" 'enabled: false'
assert_contains "$ROOT/packaging/config.yaml" '/var/lib/xarlatan/models/'
assert_contains "$ROOT/scripts/install.sh" 'SYSTEMD_USER_DIR="${SYSTEMD_USER_DIR:-/etc/systemd/user}"'
assert_contains "$ROOT/scripts/install.sh" 'LDSO_CONF_DIR="${LDSO_CONF_DIR:-/etc/ld.so.conf.d}"'
assert_contains "$ROOT/scripts/install.sh" 'collect_runtime_libs.sh'
assert_contains "$ROOT/scripts/install.sh" 'systemctl --user daemon-reload'
assert_contains "$ROOT/scripts/uninstall.sh" 'systemctl --user'

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$ROOT/bin"
for binary in assistant calibrate llama-server; do
  if [[ ! -x "$ROOT/bin/$binary" ]]; then
    printf '#!/usr/bin/env sh\nexit 0\n' > "$ROOT/bin/$binary"
    chmod 0755 "$ROOT/bin/$binary"
  fi
done

DESTDIR="$TMP/root" SKIP_SYSTEMD=1 XARLATAN_TARGET_USER=tester bash "$ROOT/scripts/install.sh"
[[ -x "$TMP/root/usr/local/bin/xarlatan" ]] || fail 'xarlatan binary not installed'
[[ -x "$TMP/root/usr/local/bin/xarlatan-calibrate" ]] || fail 'calibrate binary not installed'
[[ -x "$TMP/root/usr/local/bin/llama-server" ]] || fail 'optional legacy llama-server not installed when present'
[[ -f "$TMP/root/etc/xarlatan/config.yaml" ]] || fail 'config not installed'
[[ -f "$TMP/root/etc/systemd/user/xarlatan.service" ]] || fail 'user unit not installed'
[[ ! -e "$TMP/root/etc/systemd/system/xarlatan.service" ]] || fail 'legacy system unit installed'
[[ -f "$TMP/root/etc/ld.so.conf.d/xarlatan.conf" ]] || fail 'dynamic linker config not installed'
[[ -d "$TMP/root/usr/local/lib/xarlatan" ]] || fail 'runtime library directory not installed'
[[ -f "$TMP/root/var/backups/xarlatan/manifest" ]] || fail 'rollback manifest not installed outside service data'

first_state="$(staged_hashes "$TMP/root")"
DESTDIR="$TMP/root" SKIP_SYSTEMD=1 XARLATAN_TARGET_USER=tester bash "$ROOT/scripts/install.sh"
second_state="$(staged_hashes "$TMP/root")"
[[ "$second_state" == "$first_state" ]] || fail 'second install changed installed state'

DESTDIR="$TMP/root" SKIP_SYSTEMD=1 bash "$ROOT/scripts/rollback.sh"
[[ -x "$TMP/root/usr/local/bin/xarlatan" ]] || fail 'rollback did not restore previous binary'
[[ -f "$TMP/root/etc/systemd/user/xarlatan.service" ]] || fail 'rollback did not restore user unit'

DESTDIR="$TMP/root" SKIP_SYSTEMD=1 bash "$ROOT/scripts/uninstall.sh"
[[ ! -e "$TMP/root/usr/local/bin/xarlatan" ]] || fail 'uninstall left xarlatan binary'
[[ ! -e "$TMP/root/usr/local/lib/xarlatan" ]] || fail 'uninstall left runtime libraries'
[[ ! -e "$TMP/root/etc/systemd/user/xarlatan.service" ]] || fail 'uninstall left user unit'
[[ -f "$TMP/root/etc/xarlatan/config.yaml" ]] || fail 'uninstall removed config without PURGE=1'

DESTDIR="$TMP/root" SKIP_SYSTEMD=1 PURGE=1 bash "$ROOT/scripts/uninstall.sh"
[[ ! -e "$TMP/root/etc/xarlatan/config.yaml" ]] || fail 'purge left config'
[[ ! -e "$TMP/root/var/backups/xarlatan" ]] || fail 'purge left rollback backup'

DESTDIR="$TMP/fresh" SKIP_SYSTEMD=1 XARLATAN_TARGET_USER=tester bash "$ROOT/scripts/install.sh"
DESTDIR="$TMP/fresh" SKIP_SYSTEMD=1 bash "$ROOT/scripts/rollback.sh"
[[ ! -e "$TMP/fresh/usr/local/bin/xarlatan" ]] || fail 'first-install rollback left binary'
[[ ! -e "$TMP/fresh/usr/local/lib/xarlatan" ]] || fail 'first-install rollback left runtime libraries'
[[ ! -e "$TMP/fresh/etc/systemd/user/xarlatan.service" ]] || fail 'first-install rollback left user unit'
[[ -f "$TMP/fresh/etc/xarlatan/config.yaml" ]] || fail 'rollback removed operator config'

printf 'WI-08 installation contracts: ok\n'
