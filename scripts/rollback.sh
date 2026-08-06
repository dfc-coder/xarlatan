#!/usr/bin/env bash
set -euo pipefail

DESTDIR="${DESTDIR:-}"
PREFIX="${PREFIX:-/usr/local}"
LOCALSTATEDIR="${LOCALSTATEDIR:-/var/lib}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
SKIP_SYSTEMD="${SKIP_SYSTEMD:-0}"

path_in_root() { printf '%s%s' "$DESTDIR" "$1"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
ROLLBACK_DIR="$(path_in_root "$LOCALSTATEDIR/xarlatan/rollback")"
MANIFEST="$ROLLBACK_DIR/manifest"

if [[ -z "$DESTDIR" && "$(id -u)" -ne 0 ]]; then
  die "run as root or set DESTDIR"
fi
[[ -f "$MANIFEST" ]] || die "no rollback snapshot found"

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

if [[ "$SKIP_SYSTEMD" != "1" && -z "$DESTDIR" && -x "$(command -v systemctl || true)" ]]; then
  systemctl daemon-reload
fi

rm -rf "$ROLLBACK_DIR"
printf 'Last Xarlatan installation rolled back.\n'
