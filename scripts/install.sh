#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

DESTDIR="${DESTDIR:-}"
PREFIX="${PREFIX:-/usr/local}"
SYSCONFDIR="${SYSCONFDIR:-/etc}"
LOCALSTATEDIR="${LOCALSTATEDIR:-/var/lib}"
BACKUPDIR="${BACKUPDIR:-/var/backups/xarlatan}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
XARLATAN_USER="${XARLATAN_USER:-xarlatan}"
XARLATAN_GROUP="${XARLATAN_GROUP:-xarlatan}"
SKIP_USER="${SKIP_USER:-0}"
SKIP_SYSTEMD="${SKIP_SYSTEMD:-0}"

path_in_root() { printf '%s%s' "$DESTDIR" "$1"; }
BIN_DIR="$(path_in_root "$PREFIX/bin")"
CONFIG_DIR="$(path_in_root "$SYSCONFDIR/xarlatan")"
DATA_DIR="$(path_in_root "$LOCALSTATEDIR/xarlatan")"
SERVICE_FILE="$(path_in_root "$SYSTEMD_DIR/xarlatan.service")"
ROLLBACK_DIR="$(path_in_root "$BACKUPDIR")"
MANIFEST="$ROLLBACK_DIR/manifest"

log() { printf '%s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

require_root() {
  if [[ -z "$DESTDIR" && "$(id -u)" -ne 0 ]]; then
    die "run as root or set DESTDIR for a staged installation"
  fi
}

require_artifacts() {
  local artifact
  for artifact in assistant calibrate llama-server; do
    [[ -x "$ROOT/bin/$artifact" ]] || die "missing executable bin/$artifact; run 'make all' first"
  done
  [[ -f "$ROOT/packaging/config.yaml" ]] || die "missing packaging/config.yaml"
  [[ -f "$ROOT/packaging/systemd/xarlatan.service" ]] || die "missing systemd unit"
}

create_service_account() {
  [[ "$SKIP_USER" == "1" || -n "$DESTDIR" ]] && return 0
  if ! getent group "$XARLATAN_GROUP" >/dev/null; then
    groupadd --system "$XARLATAN_GROUP"
  fi
  if ! id "$XARLATAN_USER" >/dev/null 2>&1; then
    useradd --system --gid "$XARLATAN_GROUP" --home-dir "$LOCALSTATEDIR/xarlatan" --no-create-home --shell /usr/sbin/nologin "$XARLATAN_USER"
  fi
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
  backup_one "$SYSTEMD_DIR/xarlatan.service"
}

install_artifacts() {
  install -Dm755 "$ROOT/bin/assistant" "$BIN_DIR/xarlatan"
  install -Dm755 "$ROOT/bin/calibrate" "$BIN_DIR/xarlatan-calibrate"
  install -Dm755 "$ROOT/bin/llama-server" "$BIN_DIR/llama-server"
  install -Dm644 "$ROOT/packaging/systemd/xarlatan.service" "$SERVICE_FILE"

  mkdir -p "$CONFIG_DIR" "$DATA_DIR/models"
  if [[ ! -e "$CONFIG_DIR/config.yaml" ]]; then
    install -m640 "$ROOT/packaging/config.yaml" "$CONFIG_DIR/config.yaml"
  fi
  if [[ -d "$ROOT/models" ]]; then
    cp -a "$ROOT/models/." "$DATA_DIR/models/"
  fi
}

set_permissions() {
  [[ "$SKIP_USER" == "1" || -n "$DESTDIR" ]] && return 0
  chown root:"$XARLATAN_GROUP" "$CONFIG_DIR/config.yaml"
  chmod 0640 "$CONFIG_DIR/config.yaml"
  chown -R "$XARLATAN_USER":"$XARLATAN_GROUP" "$DATA_DIR"
  chmod 0750 "$DATA_DIR" "$DATA_DIR/models"
  chown -R root:root "$ROLLBACK_DIR"
  chmod 0700 "$ROLLBACK_DIR"
}

reload_systemd() {
  [[ "$SKIP_SYSTEMD" == "1" || -n "$DESTDIR" ]] && return 0
  if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
  fi
}

require_root
require_artifacts
create_service_account
mkdir -p "$DATA_DIR"
prepare_rollback
install_artifacts
set_permissions
reload_systemd

log "Xarlatan installation complete."
log "Configuration: $SYSCONFDIR/xarlatan/config.yaml"
log "Enable explicitly: systemctl enable --now xarlatan"
log "Logs: journalctl -u xarlatan -f"
log "Rollback: make rollback"
