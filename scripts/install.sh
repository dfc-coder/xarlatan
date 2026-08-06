#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

DESTDIR="${DESTDIR:-}"
PREFIX="${PREFIX:-/usr/local}"
SYSCONFDIR="${SYSCONFDIR:-/etc}"
LOCALSTATEDIR="${LOCALSTATEDIR:-/var/lib}"
BACKUPDIR="${BACKUPDIR:-/var/backups/xarlatan}"
SYSTEMD_USER_DIR="${SYSTEMD_USER_DIR:-/etc/systemd/user}"
SYSTEMD_SYSTEM_DIR="${SYSTEMD_SYSTEM_DIR:-/etc/systemd/system}"
LDSO_CONF_DIR="${LDSO_CONF_DIR:-/etc/ld.so.conf.d}"
TARGET_USER="${XARLATAN_TARGET_USER:-${SUDO_USER:-}}"
TARGET_GROUP="${XARLATAN_TARGET_GROUP:-}"
TARGET_UID=""
SKIP_SYSTEMD="${SKIP_SYSTEMD:-0}"

path_in_root() { printf '%s%s' "$DESTDIR" "$1"; }
BIN_DIR="$(path_in_root "$PREFIX/bin")"
LIB_DIR="$(path_in_root "$PREFIX/lib/xarlatan")"
CONFIG_DIR="$(path_in_root "$SYSCONFDIR/xarlatan")"
DATA_DIR="$(path_in_root "$LOCALSTATEDIR/xarlatan")"
USER_SERVICE_FILE="$(path_in_root "$SYSTEMD_USER_DIR/xarlatan.service")"
LEGACY_SERVICE_FILE="$(path_in_root "$SYSTEMD_SYSTEM_DIR/xarlatan.service")"
LDSO_CONF_FILE="$(path_in_root "$LDSO_CONF_DIR/xarlatan.conf")"
ROLLBACK_DIR="$(path_in_root "$BACKUPDIR")"
MANIFEST="$ROLLBACK_DIR/manifest"

log() { printf '%s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

require_root() {
  if [[ -z "$DESTDIR" && "$(id -u)" -ne 0 ]]; then
    die "run as root or set DESTDIR for a staged installation"
  fi
}

resolve_target_identity() {
  if [[ -n "$DESTDIR" ]]; then
    TARGET_USER="${TARGET_USER:-xarlatan-test}"
    TARGET_GROUP="${TARGET_GROUP:-$TARGET_USER}"
    return 0
  fi
  if [[ -z "$TARGET_USER" || "$TARGET_USER" == root ]]; then
    die "run through sudo from the desktop user or set XARLATAN_TARGET_USER"
  fi
  id "$TARGET_USER" >/dev/null 2>&1 || die "target desktop user does not exist: $TARGET_USER"
  TARGET_GROUP="${TARGET_GROUP:-$(id -gn "$TARGET_USER")}"
  TARGET_UID="$(id -u "$TARGET_USER")"
}

require_artifacts() {
  local artifact
  for artifact in assistant calibrate llama-server; do
    [[ -x "$ROOT/bin/$artifact" ]] || die "missing executable bin/$artifact; run 'make all' first"
  done
  [[ -f "$ROOT/packaging/config.yaml" ]] || die "missing packaging/config.yaml"
  [[ -f "$ROOT/packaging/systemd/xarlatan.service" ]] || die "missing systemd user unit"
  [[ -f "$ROOT/scripts/collect_runtime_libs.sh" ]] || die "missing runtime library collector"
}

backup_one() {
  local logical="$1"
  local target
  target="$(path_in_root "$logical")"
  if [[ -e "$target" || -L "$target" ]]; then
    mkdir -p "$ROLLBACK_DIR/files$(dirname "$logical")"
    cp -a "$target" "$ROLLBACK_DIR/files$logical"
    printf 'restore|%s\n' "$logical" >> "$MANIFEST"
  else
    printf 'remove|%s\n' "$logical" >> "$MANIFEST"
  fi
}

prepare_rollback() {
  rm -rf "$ROLLBACK_DIR"
  mkdir -p "$ROLLBACK_DIR/files"
  chmod 0700 "$ROLLBACK_DIR"
  : > "$MANIFEST"
  backup_one "$PREFIX/bin/xarlatan"
  backup_one "$PREFIX/bin/xarlatan-calibrate"
  backup_one "$PREFIX/bin/llama-server"
  backup_one "$PREFIX/lib/xarlatan"
  backup_one "$LDSO_CONF_DIR/xarlatan.conf"
  backup_one "$SYSTEMD_USER_DIR/xarlatan.service"
  backup_one "$SYSTEMD_SYSTEM_DIR/xarlatan.service"
}

stop_legacy_service() {
  [[ "$SKIP_SYSTEMD" == 1 || -n "$DESTDIR" ]] && return 0
  if command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now xarlatan.service >/dev/null 2>&1 || true
  fi
}

install_runtime_libraries() {
  local runtime_stage
  runtime_stage="$(mktemp -d)"

  if [[ -d "$ROOT/lib" ]] && compgen -G "$ROOT/lib/*" >/dev/null; then
    while IFS= read -r -d '' library; do
      install -m755 "$library" "$runtime_stage/$(basename "$library")"
    done < <(find "$ROOT/lib" -maxdepth 1 -type f -print0)
  elif ! bash "$ROOT/scripts/collect_runtime_libs.sh" "$ROOT/bin/assistant" "$runtime_stage"; then
    rm -rf "$runtime_stage"
    die "could not collect runtime libraries"
  fi

  rm -rf "$LIB_DIR"
  mkdir -p "$LIB_DIR"
  while IFS= read -r -d '' library; do
    install -m755 "$library" "$LIB_DIR/$(basename "$library")"
  done < <(find "$runtime_stage" -maxdepth 1 -type f -print0)
  rm -rf "$runtime_stage"

  install -d -m755 "$(dirname "$LDSO_CONF_FILE")"
  printf '%s\n' "$PREFIX/lib/xarlatan" > "$LDSO_CONF_FILE"
  chmod 0644 "$LDSO_CONF_FILE"
}

install_artifacts() {
  install -Dm755 "$ROOT/bin/assistant" "$BIN_DIR/xarlatan"
  install -Dm755 "$ROOT/bin/calibrate" "$BIN_DIR/xarlatan-calibrate"
  install -Dm755 "$ROOT/bin/llama-server" "$BIN_DIR/llama-server"
  install -Dm644 "$ROOT/packaging/systemd/xarlatan.service" "$USER_SERVICE_FILE"
  rm -f "$LEGACY_SERVICE_FILE"

  mkdir -p "$CONFIG_DIR" "$DATA_DIR/models"
  if [[ ! -e "$CONFIG_DIR/config.yaml" ]]; then
    install -m640 "$ROOT/packaging/config.yaml" "$CONFIG_DIR/config.yaml"
  fi
  if [[ -d "$ROOT/models" ]]; then
    cp -a "$ROOT/models/." "$DATA_DIR/models/"
  fi
}

set_permissions() {
  chmod 0640 "$CONFIG_DIR/config.yaml"
  chmod 0750 "$DATA_DIR" "$DATA_DIR/models"
  find "$DATA_DIR/models" -type d -exec chmod 0750 {} +
  find "$DATA_DIR/models" -type f -exec chmod 0640 {} +
  chmod 0755 "$LIB_DIR"
  find "$LIB_DIR" -type f -exec chmod 0755 {} +

  [[ -n "$DESTDIR" ]] && return 0

  chown root:"$TARGET_GROUP" "$CONFIG_DIR/config.yaml"
  chown -R "$TARGET_USER":"$TARGET_GROUP" "$DATA_DIR"
  chown -R root:root "$LIB_DIR" "$LDSO_CONF_FILE" "$USER_SERVICE_FILE" "$ROLLBACK_DIR"
  chmod 0700 "$ROLLBACK_DIR"
}

refresh_dynamic_linker() {
  [[ -n "$DESTDIR" ]] && return 0
  command -v ldconfig >/dev/null 2>&1 || die "ldconfig is required"
  ldconfig

  local linkage
  linkage="$(env -u LD_LIBRARY_PATH ldd "$BIN_DIR/xarlatan" 2>&1 || true)"
  if grep -q 'not found' <<<"$linkage"; then
    printf '%s\n' "$linkage" >&2
    die "installed xarlatan still has unresolved runtime libraries"
  fi
}

reload_user_systemd() {
  [[ "$SKIP_SYSTEMD" == 1 || -n "$DESTDIR" ]] && return 0
  command -v systemctl >/dev/null 2>&1 || return 0
  systemctl daemon-reload

  if [[ -d "/run/user/$TARGET_UID" ]] && command -v runuser >/dev/null 2>&1; then
    runuser -u "$TARGET_USER" -- env XDG_RUNTIME_DIR="/run/user/$TARGET_UID" \
      systemctl --user daemon-reload || log "WARNING: run 'systemctl --user daemon-reload' as $TARGET_USER"
  else
    log "WARNING: user systemd session not active; run 'systemctl --user daemon-reload' as $TARGET_USER"
  fi
}

require_root
resolve_target_identity
require_artifacts
mkdir -p "$DATA_DIR"
prepare_rollback
stop_legacy_service
install_runtime_libraries
install_artifacts
set_permissions
refresh_dynamic_linker
reload_user_systemd

log "Xarlatan installation complete for desktop user: $TARGET_USER"
log "Configuration: $SYSCONFDIR/xarlatan/config.yaml"
log "Enable explicitly as $TARGET_USER: systemctl --user enable --now xarlatan"
log "Logs: journalctl --user -u xarlatan -f"
log "Rollback: sudo make rollback"
