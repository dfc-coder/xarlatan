#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "setup_voice_runtime.sh must run as root (use sudo)" >&2
  exit 1
fi

DESKTOP_USER="${SUDO_USER:-${XARLATAN_DESKTOP_USER:-}}"
if [[ -z "${DESKTOP_USER}" || "${DESKTOP_USER}" == "root" ]]; then
  echo "unable to determine desktop user; run with sudo from the desktop account" >&2
  exit 1
fi

VOICE_ROOT="${XARLATAN_VOICE_ROOT:-/opt/xarlatan/voice}"
VENV="${VOICE_ROOT}/venv"
MODEL_ROOT="${XARLATAN_MODEL_ROOT:-/var/lib/xarlatan/models/openvino}"
STT_DIR="${MODEL_ROOT}/whisper-base"
KOKORO_DIR="${MODEL_ROOT}/kokoro"
CACHE_ROOT="${XARLATAN_CACHE_ROOT:-/var/lib/xarlatan/cache/openvino}"
WORKER_DST="/usr/local/lib/xarlatan/openvino_voice_worker.py"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

OPENVINO_GENAI_VERSION="2026.2.1.0"
OPTIMUM_INTEL_VERSION="2.0.0"
KOKORO_ONNX_VERSION="0.5.0"

for command_name in python3 curl espeak-ng; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "missing prerequisite: ${command_name}" >&2
    exit 1
  fi
done

if ! python3 -m venv --help >/dev/null 2>&1; then
  echo "python3 venv support is required (Fedora: sudo dnf install python3)" >&2
  exit 1
fi

install -d -m 0755 "${VOICE_ROOT}" "${STT_DIR}" "${KOKORO_DIR}" "${CACHE_ROOT}/stt" "${CACHE_ROOT}/tts" /usr/local/lib/xarlatan
python3 -m venv "${VENV}"
"${VENV}/bin/python" -m pip install --upgrade pip
"${VENV}/bin/python" -m pip install \
  "openvino-genai==${OPENVINO_GENAI_VERSION}" \
  "optimum-intel==${OPTIMUM_INTEL_VERSION}" \
  "kokoro-onnx==${KOKORO_ONNX_VERSION}"

install -m 0755 "${REPO_ROOT}/scripts/openvino_voice_worker.py" "${WORKER_DST}"

if [[ ! -s "${STT_DIR}/openvino_encoder_model.xml" && ! -s "${STT_DIR}/openvino_model.xml" ]]; then
  rm -rf "${STT_DIR:?}"/*
  "${VENV}/bin/optimum-cli" export openvino \
    --trust-remote-code \
    --model openai/whisper-base \
    "${STT_DIR}"
fi

KOKORO_MODEL="${KOKORO_DIR}/kokoro-v1.0.onnx"
KOKORO_VOICES="${KOKORO_DIR}/voices-v1.0.bin"
if [[ ! -s "${KOKORO_MODEL}" ]]; then
  tmp="${KOKORO_MODEL}.part"
  rm -f "${tmp}"
  curl --fail --location --retry 3 \
    "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/kokoro-v1.0.onnx" \
    --output "${tmp}"
  test -s "${tmp}"
  mv "${tmp}" "${KOKORO_MODEL}"
fi
if [[ ! -s "${KOKORO_VOICES}" ]]; then
  tmp="${KOKORO_VOICES}.part"
  rm -f "${tmp}"
  curl --fail --location --retry 3 \
    "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/voices-v1.0.bin" \
    --output "${tmp}"
  test -s "${tmp}"
  mv "${tmp}" "${KOKORO_VOICES}"
fi

# Models and venv are immutable during normal assistant operation; only caches
# and ZeroClaw's own workspace/state need write access at runtime.
chmod -R a+rX "${VOICE_ROOT}" "${MODEL_ROOT}"
chown -R root:root "${VOICE_ROOT}" "${MODEL_ROOT}"
chown -R "${DESKTOP_USER}:${DESKTOP_USER}" "${CACHE_ROOT}"

"${VENV}/bin/python" - <<'PY'
import openvino as ov
core = ov.Core()
print("OpenVINO devices:", ", ".join(core.available_devices) or "none")
PY

"${VENV}/bin/python" - <<'PY'
from kokoro_onnx import Kokoro
print("Kokoro runtime: ready")
PY

if command -v zeroclaw >/dev/null 2>&1; then
  ZC_BIN="$(command -v zeroclaw)"
elif sudo -u "${DESKTOP_USER}" bash -lc 'command -v zeroclaw' >/dev/null 2>&1; then
  ZC_BIN="$(sudo -u "${DESKTOP_USER}" bash -lc 'command -v zeroclaw')"
else
  echo "ZeroClaw is not installed. Install and run 'zeroclaw quickstart' as ${DESKTOP_USER} before acceptance." >&2
  exit 1
fi
install -m 0755 "${ZC_BIN}" /usr/local/bin/zeroclaw

if ! sudo -u "${DESKTOP_USER}" test -r "/home/${DESKTOP_USER}/.zeroclaw/config.toml"; then
  echo "ZeroClaw config missing for ${DESKTOP_USER}; run: zeroclaw quickstart" >&2
  exit 1
fi

printf 'Voice runtime ready:\n'
printf '  Python:   %s\n' "${VENV}/bin/python"
printf '  Whisper: %s\n' "${STT_DIR}"
printf '  Kokoro:  %s\n' "${KOKORO_DIR}"
printf '  ZeroClaw: /usr/local/bin/zeroclaw\n'
