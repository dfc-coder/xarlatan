#!/usr/bin/env bash
set -euo pipefail

BINARY="${1:-}"
DESTINATION="${2:-}"

[[ -n "$BINARY" && -n "$DESTINATION" ]] || {
  printf 'usage: %s BINARY DESTINATION\n' "$0" >&2
  exit 2
}
[[ -x "$BINARY" ]] || {
  printf 'runtime binary is not executable: %s\n' "$BINARY" >&2
  exit 1
}

mkdir -p "$DESTINATION"

ldd_output="$(ldd "$BINARY" 2>&1 || true)"
if grep -qiE 'not a dynamic executable|statically linked' <<<"$ldd_output"; then
  exit 0
fi
if grep -q 'not found' <<<"$ldd_output"; then
  printf '%s\n' "$ldd_output" >&2
  printf 'unresolved runtime libraries for %s\n' "$BINARY" >&2
  exit 1
fi

copied=0
while read -r soname arrow resolved _rest; do
  [[ "$arrow" == '=>' && "$resolved" == /* ]] || continue
  case "$resolved" in
    /lib/*|/lib64/*|/usr/lib/*|/usr/lib64/*)
      continue
      ;;
  esac
  install -m755 "$resolved" "$DESTINATION/$soname"
  copied=$((copied + 1))
done <<<"$ldd_output"

printf 'Collected %d non-system runtime librar%s from %s\n' \
  "$copied" "$([[ "$copied" -eq 1 ]] && printf y || printf ies)" "$BINARY"
