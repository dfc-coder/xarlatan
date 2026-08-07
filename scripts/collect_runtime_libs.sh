#!/usr/bin/env bash
set -euo pipefail

BINARY="${1:-}"
DESTINATION="${2:-}"
shift $(( $# >= 2 ? 2 : $# ))
SEARCH_ROOTS=("$@")

[[ -n "$BINARY" && -n "$DESTINATION" ]] || {
  printf 'usage: %s BINARY DESTINATION [SEARCH_ROOT ...]\n' "$0" >&2
  exit 2
}
[[ -x "$BINARY" ]] || {
  printf 'runtime binary is not executable: %s\n' "$BINARY" >&2
  exit 1
}

mkdir -p "$DESTINATION"

is_system_library() {
  case "$1" in
    /lib/*|/lib64/*|/usr/lib/*|/usr/lib64/*)
      return 0
      ;;
  esac
  return 1
}

find_library() {
  local soname="$1"
  local root candidate
  for root in "${SEARCH_ROOTS[@]}"; do
    [[ -d "$root" ]] || continue
    candidate="$(find "$root" \( -type f -o -type l \) -name "$soname" -print -quit 2>/dev/null || true)"
    if [[ -n "$candidate" ]]; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  return 1
}

copy_library() {
  local soname="$1"
  local source="$2"
  [[ -n "$soname" && -n "$source" ]] || return 1
  [[ -e "$source" || -L "$source" ]] || return 1
  install -m755 -D -L "$source" "$DESTINATION/$soname"
}

collect_from_ldd() {
  local target="$1"
  local output line soname arrow resolved rest source
  local progress=0

  output="$(LD_LIBRARY_PATH="$DESTINATION${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" ldd "$target" 2>&1 || true)"
  if grep -qiE 'not a dynamic executable|statically linked' <<<"$output"; then
    return 0
  fi

  while IFS= read -r line; do
    read -r soname arrow resolved rest <<<"$line"
    if [[ "$arrow" == '=>' && "$resolved" == /* ]]; then
      if ! is_system_library "$resolved" && [[ ! -e "$DESTINATION/$soname" ]]; then
        copy_library "$soname" "$resolved"
        progress=1
      fi
      continue
    fi
    if [[ "$arrow" == '=>' && "$resolved" == 'not' && "$rest" == found* ]]; then
      if [[ -e "$DESTINATION/$soname" ]]; then
        continue
      fi
      source="$(find_library "$soname" || true)"
      if [[ -z "$source" ]]; then
        printf 'unresolved runtime library %s for %s\n' "$soname" "$target" >&2
        return 1
      fi
      copy_library "$soname" "$source"
      progress=1
    fi
  done <<<"$output"

  [[ "$progress" -eq 0 ]] || return 10
  return 0
}

for _round in 1 2 3 4 5; do
  progress=0
  set +e
  collect_from_ldd "$BINARY"
  rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || {
    [[ "$rc" -eq 10 ]] && progress=1 || exit "$rc"
  }

  while IFS= read -r -d '' library; do
    set +e
    collect_from_ldd "$library"
    rc=$?
    set -e
    [[ "$rc" -eq 0 ]] || {
      [[ "$rc" -eq 10 ]] && progress=1 || exit "$rc"
    }
  done < <(find "$DESTINATION" -maxdepth 1 -type f -print0)

  [[ "$progress" -eq 1 ]] || break
done

linkage="$(LD_LIBRARY_PATH="$DESTINATION" ldd "$BINARY" 2>&1 || true)"
if grep -q 'not found' <<<"$linkage"; then
  printf '%s\n' "$linkage" >&2
  printf 'runtime dependency closure is incomplete\n' >&2
  exit 1
fi

copied="$(find "$DESTINATION" -maxdepth 1 -type f | wc -l)"
if [[ "$copied" -eq 1 ]]; then
  printf 'Collected 1 non-system runtime library from %s\n' "$BINARY"
else
  printf 'Collected %d non-system runtime libraries from %s\n' "$copied" "$BINARY"
fi
