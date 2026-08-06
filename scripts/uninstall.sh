#!/usr/bin/env bash
set -euo pipefail

DESTDIR="${DESTDIR:-}"
PREFIX="${PREFIX:-/usr/local}"
SYSCONFDIR="${SYSCONFDIR:-/etc}"
LOCALSTATEDIR="${LOCALSTATEDIR:-/var/lib}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
XARLATAN_USER="${XARLATAN_USER:-xarlatan}"
XARLATAN_GROUP="${XARLATAN_GROUP:-xarlatan}"
SKIP_SYSTEMD="${SKIP_SYSTEMD:-0}"
PURGE="${PURGE:-0}"

path_in_root() { printf '%s%s' "$DESTDIR" "$1"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

if [[ -z "$DESTDIR" && "$(id -u)" -ne 0 ]]; then
  die "run as root or set DESTDIR"
fi

if [[ "$SKIP_SYSTEMD" != "1" && -z "$DESTDIR" && -x "$(command -v systemctl || true)" ]]; then
  systemctl disable --now xarlatan.service 2>/dev/null || true
fi

rm -f \
  "$(path_in_root "$PREFIX/bin/xarlatan")" \
  "$(path_in_root "$PREFIX/bin/xarlatan-calibrate")" \
  "$(path_in_root "$PREFIX/bin/llama-server")" \
  "$(path_in_root "$SYSTEMD_DIR/xarlatan.service")"

if [[ "$PURGE" == "1" ]]; then
  rm -rf \
    "$(path_in_root "$SYSCONFDIR/xarlatan")" \
    "$(path_in_root "$LOCALSTATEDIR/xarlatan")"
  if [[ -z "$DESTDIR" ]]; then
    id "$XARLATAN_USER" >/dev/null 2>&1 && userdel "$XARLATAN_USER" || true
    getent group "$XARLATAN_GROUP" >/dev/null && groupdel "$XARLATAN_GROUP" || true
  fi
fi

if [[ "$SKIP_SYSTEMD" != "1" && -z "$DESTDIR" && -x "$(command -v systemctl || true)" ]]; then
  systemctl daemon-reload
fi

printf 'Xarlatan uninstalled%s.\n' "$([[ "$PURGE" == "1" ]] && printf ' with purge' || true)"
