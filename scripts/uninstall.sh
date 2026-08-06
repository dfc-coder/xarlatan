#!/usr/bin/env bash
set -euo pipefail

DESTDIR="${DESTDIR:-}"
PREFIX="${PREFIX:-/usr/local}"
SYSCONFDIR="${SYSCONFDIR:-/etc}"
LOCALSTATEDIR="${LOCALSTATEDIR:-/var/lib}"
BACKUPDIR="${BACKUPDIR:-/var/backups/xarlatan}"
SYSTEMD_USER_DIR="${SYSTEMD_USER_DIR:-/etc/systemd/user}"
SYSTEMD_SYSTEM_DIR="${SYSTEMD_SYSTEM_DIR:-/etc/systemd/system}"
LDSO_CONF_DIR="${LDSO_CONF_DIR:-/etc/ld.so.conf.d}"
TARGET_USER="${XARLATAN_TARGET_USER:-${SUDO_USER:-}}"
TARGET_UID=""
SKIP_SYSTEMD="${SKIP_SYSTEMD:-0}"
PURGE="${PURGE:-0}"

path_in_root() { printf '%s%s' "$DESTDIR" "$1"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

if [[ -z "$DESTDIR" && "$(id -u)" -ne 0 ]]; then
  die "run as root or set DESTDIR"
fi

if [[ -z "$DESTDIR" && -n "$TARGET_USER" && "$TARGET_USER" != root ]] && id "$TARGET_USER" >/dev/null 2>&1; then
  TARGET_UID="$(id -u "$TARGET_USER")"
fi

user_systemctl() {
  [[ "$SKIP_SYSTEMD" == 1 || -n "$DESTDIR" || -z "$TARGET_UID" ]] && return 0
  [[ -d "/run/user/$TARGET_UID" ]] || return 0
  command -v runuser >/dev/null 2>&1 || return 0
  runuser -u "$TARGET_USER" -- env XDG_RUNTIME_DIR="/run/user/$TARGET_UID" \
    systemctl --user "$@" >/dev/null 2>&1 || true
}

user_systemctl disable --now xarlatan.service

if [[ "$SKIP_SYSTEMD" != 1 && -z "$DESTDIR" ]] && command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now xarlatan.service >/dev/null 2>&1 || true
fi

rm -rf \
  "$(path_in_root "$PREFIX/bin/xarlatan")" \
  "$(path_in_root "$PREFIX/bin/xarlatan-calibrate")" \
  "$(path_in_root "$PREFIX/bin/llama-server")" \
  "$(path_in_root "$PREFIX/lib/xarlatan")"
rm -f \
  "$(path_in_root "$LDSO_CONF_DIR/xarlatan.conf")" \
  "$(path_in_root "$SYSTEMD_USER_DIR/xarlatan.service")" \
  "$(path_in_root "$SYSTEMD_SYSTEM_DIR/xarlatan.service")"

if [[ -z "$DESTDIR" ]] && command -v ldconfig >/dev/null 2>&1; then
  ldconfig
fi

if [[ "$PURGE" == 1 ]]; then
  rm -rf \
    "$(path_in_root "$SYSCONFDIR/xarlatan")" \
    "$(path_in_root "$LOCALSTATEDIR/xarlatan")" \
    "$(path_in_root "$BACKUPDIR")"
  if [[ -z "$DESTDIR" ]]; then
    if id xarlatan >/dev/null 2>&1; then
      userdel xarlatan || true
    fi
    if getent group xarlatan >/dev/null; then
      groupdel xarlatan || true
    fi
  fi
fi

if [[ "$SKIP_SYSTEMD" != 1 && -z "$DESTDIR" ]] && command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
  user_systemctl daemon-reload
fi

if [[ "$PURGE" == 1 ]]; then
  printf 'Xarlatan uninstalled with purge.\n'
else
  printf 'Xarlatan uninstalled; config and models preserved.\n'
fi
