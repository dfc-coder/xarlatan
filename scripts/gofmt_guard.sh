#!/usr/bin/env bash
set -euo pipefail

mode="${1:-check}"

tracked_go_files() {
  git ls-files -z -- '*.go'
}

case "$mode" in
  fix)
    mapfile -d '' files < <(tracked_go_files)
    if ((${#files[@]} == 0)); then
      exit 0
    fi
    gofmt -w -- "${files[@]}"
    ;;

  staged)
    mapfile -d '' files < <(git diff --cached --name-only -z --diff-filter=ACMR -- '*.go')
    if ((${#files[@]} == 0)); then
      exit 0
    fi
    gofmt -w -- "${files[@]}"
    git add -- "${files[@]}"
    ;;

  check)
    mapfile -d '' files < <(tracked_go_files)
    if ((${#files[@]} == 0)); then
      exit 0
    fi
    unformatted="$(gofmt -l -- "${files[@]}")"
    if [[ -n "$unformatted" ]]; then
      printf 'Go files are not gofmt-clean:\n%s\n' "$unformatted" >&2
      printf 'Run: make fmt\n' >&2
      exit 1
    fi
    ;;

  *)
    printf 'usage: %s {check|fix|staged}\n' "$0" >&2
    exit 2
    ;;
esac
