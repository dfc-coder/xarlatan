#!/usr/bin/env bash
set -euo pipefail

DESTDIR="${DESTDIR:-}"
BACKUPDIR="${BACKUPDIR:-/var/backups/xarlatan}"
TARGET_USER="${XARLATAN_TARGET_USER:-${SUDO_USER:-}}"
TARGET_UID=""
SKIP_SYSTEMD="${SKIP_SYSTEMD:-0}"

path_in_root() { printf '%s%s' "$DESTDIR" "$1"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
ROLLBACK_DIR="$(path_in_root "$BACKUPDIR")"
MANIFEST="$ROLLBACK_DIR/manifest"

if [[ -z "$DESTDIR" && "$(id -u)" -ne 0 ]]; then
  die "run as root or set DESTDIR"
fi
[[ -f "$MANIFEST" ]] || die "no rollback snapshot found"

if [[ -z "$DESTDIR" && -n "$TARGET_USER" && "$TARGET_USER" != root ]] && id "$TARGET_USER" >/dev/null 2>&1; then
  TARGET_UID="$(id -u "$TARGET_USER")"
fi

while IFS='|' read -r action logical; do
  [[ -n "$action" && -n "$logical" ]] || continue
  target="$(path_in_root "$logical")"
  case "$action" in
    restore)
      source_file="$ROLLBACK_DIR/files$logical"
      [[ -e "$source_file" || -L "$source_file" ]] || die "rollback source missing: $logical"
      mkdir -p "$(dirname "$target")"
      rm -rf "$target"
      cp -a "$source_file" "$target"
      ;;
    remove)
      rm -rf "$target"
      ;;
    *)
      die "unknown rollback action: $action"
      ;;
  esac
done < "$MANIFEST"

if [[ -z "$DESTDIR" ]] && command -v ldconfig >/dev/null 2>&1; then
  ldconfig
fi

if [[ "$SKIP_SYSTEMD" != 1 && -z "$DESTDIR" ]] && command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
  if [[ -n "$TARGET_UID" && -d "/run/user/$TARGET_UID" ]] && command -v runuser >/dev/null 2>&1; then
    runuser -u "$TARGET_USER" -- env XDG_RUNTIME_DIR="/run/user/$TARGET_UID" \
      systemctl --user daemon-reload || true
  fi
fi

rm -rf "$ROLLBACK_DIR"
printf 'Last Xarlatan installation rolled back.\n'
