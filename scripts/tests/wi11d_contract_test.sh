#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

bash -n scripts/beta_acceptance.sh
bash -n scripts/beta_v05_acceptance.sh

if ! grep -Fq 'VERSION ?= v0.5.0-beta.1' Makefile && ! grep -Fq 'VERSION ?= v0.6.0-beta.1' Makefile && ! grep -Fq 'VERSION ?= v0.7.0' Makefile; then
  echo 'expected a supported beta default version' >&2
  exit 1
fi
grep -Fq 'beta_v05_acceptance.sh' scripts/beta_acceptance.sh
grep -Fq 'EXPECTED_VERSION' scripts/beta_acceptance.sh

grep -Fq -- '--wake=true' scripts/beta_v05_acceptance.sh
grep -Fq -- '--barge-in=true' scripts/beta_v05_acceptance.sh
grep -Fq '[partial]' scripts/beta_v05_acceptance.sh
grep -Fq 'same arecord PID survives multiple turns and barge-in' scripts/beta_v05_acceptance.sh
grep -Fq 'ordinary speech without wake is ignored' scripts/beta_v05_acceptance.sh
grep -Fq 'wake-qualified physical barge-in stops active playback' scripts/beta_v05_acceptance.sh
grep -Fq 'no spontaneous post-playback self-trigger' scripts/beta_v05_acceptance.sh
grep -Fq 'Final result: %s' scripts/beta_v05_acceptance.sh

if grep -Eq 'pkill|killall' scripts/beta_v05_acceptance.sh; then
  echo 'v0.5 acceptance must only stop owned PIDs' >&2
  exit 1
fi

echo 'WI-11D release contract: PASS'
