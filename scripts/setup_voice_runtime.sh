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
DESKTOP_HOME="$(getent passwd "${DESKTOP_USER}" | cut -d: -f6)"
if [[ -z "${DESKTOP_HOME}" || ! -d "${DESKTOP_HOME}" ]]; then
  echo "unable to determine home directory for ${DESKTOP_USER}" >&2
  exit 1
fi

VOICE_ROOT="${XARLATAN_VOICE_ROOT:-/opt/xarlatan/voice}"
STT_VENV="${VOICE_ROOT}/stt-venv"
TTS_VENV="${VOICE_ROOT}/tts-venv"
MODEL_ROOT="${XARLATAN_MODEL_ROOT:-/var/lib/xarlatan/models/openvino}"
STT_DIR="${MODEL_ROOT}/whisper-base"
KOKORO_DIR="${MODEL_ROOT}/kokoro"
CACHE_ROOT="${XARLATAN_CACHE_ROOT:-/var/lib/xarlatan/cache/openvino}"
WORKER_DST="/usr/local/lib/xarlatan/openvino_voice_worker.py"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

OPENVINO_GENAI_VERSION="2026.2.1.0"
OPTIMUM_INTEL_VERSION="2.0.0"
KOKORO_ONNX_VERSION="0.5.0"
ORT_OPENVINO_VERSION="1.24.1"

for command_name in python3 curl espeak-ng getent; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "missing prerequisite: ${command_name}" >&2
    exit 1
  fi
done

if ! python3 -m venv --help >/dev/null 2>&1; then
  echo "python3 venv support is required" >&2
  exit 1
fi

install -d -m 0755 \
  "${VOICE_ROOT}" \
  "${STT_DIR}" \
  "${KOKORO_DIR}" \
  "${CACHE_ROOT}/stt" \
  "${CACHE_ROOT}/tts" \
  /usr/local/lib/xarlatan

# Keep the OpenVINO GenAI and ONNX Runtime OpenVINO stacks isolated. Their
# release trains are independent and should not overwrite each other's native
# OpenVINO libraries inside one Python environment.
python3 -m venv "${STT_VENV}"
"${STT_VENV}/bin/python" -m pip install --upgrade pip
"${STT_VENV}/bin/python" -m pip install \
  "openvino-genai==${OPENVINO_GENAI_VERSION}" \
  "optimum-intel==${OPTIMUM_INTEL_VERSION}"

python3 -m venv "${TTS_VENV}"
"${TTS_VENV}/bin/python" -m pip install --upgrade pip
"${TTS_VENV}/bin/python" -m pip install "kokoro-onnx==${KOKORO_ONNX_VERSION}"
"${TTS_VENV}/bin/python" -m pip uninstall -y onnxruntime onnxruntime-gpu >/dev/null 2>&1 || true
"${TTS_VENV}/bin/python" -m pip install "onnxruntime-openvino==${ORT_OPENVINO_VERSION}"

install -m 0755 "${REPO_ROOT}/scripts/openvino_voice_worker.py" "${WORKER_DST}"

if [[ ! -s "${STT_DIR}/openvino_encoder_model.xml" && ! -s "${STT_DIR}/openvino_model.xml" ]]; then
  rm -rf "${STT_DIR:?}"/*
  "${STT_VENV}/bin/optimum-cli" export openvino \
    --trust-remote-code \
    --model openai/whisper-base \
    "${STT_DIR}"
fi

KOKORO_MODEL="${KOKORO_DIR}/kokoro-v1.0.onnx"
KOKORO_VOICES="${KOKORO_DIR}/voices-v1.0.bin"
download_atomic() {
  local url="$1"
  local destination="$2"
  local tmp="${destination}.part.$$"
  rm -f "${tmp}"
  if ! curl --fail --location --retry 3 --retry-delay 2 "${url}" --output "${tmp}"; then
    rm -f "${tmp}"
    return 1
  fi
  if [[ ! -s "${tmp}" ]]; then
    rm -f "${tmp}"
    return 1
  fi
  mv -f "${tmp}" "${destination}"
}
if [[ ! -s "${KOKORO_MODEL}" ]]; then
  download_atomic \
    "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/kokoro-v1.0.onnx" \
    "${KOKORO_MODEL}"
fi
if [[ ! -s "${KOKORO_VOICES}" ]]; then
  download_atomic \
    "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/voices-v1.0.bin" \
    "${KOKORO_VOICES}"
fi

chmod -R a+rX "${VOICE_ROOT}" "${MODEL_ROOT}"
chown -R root:root "${VOICE_ROOT}" "${MODEL_ROOT}"
chown -R "${DESKTOP_USER}:${DESKTOP_USER}" "${CACHE_ROOT}"

"${STT_VENV}/bin/python" - <<'PY'
import openvino as ov
core = ov.Core()
devices = list(core.available_devices)
print("OpenVINO STT devices:", ", ".join(devices) or "none")
print("Intel GPU visible:", any(device.startswith("GPU") for device in devices))
PY

"${TTS_VENV}/bin/python" - <<'PY'
import onnxruntime as ort
from kokoro_onnx import Kokoro  # noqa: F401
providers = ort.get_available_providers()
print("Kokoro providers:", ", ".join(providers))
print("Kokoro OpenVINO EP visible:", "OpenVINOExecutionProvider" in providers)
PY

printf 'Voice runtime ready:\n'
printf '  STT Python: %s\n' "${STT_VENV}/bin/python"
printf '  TTS Python: %s\n' "${TTS_VENV}/bin/python"
printf '  Whisper:    %s\n' "${STT_DIR}"
printf '  Kokoro:     %s\n' "${KOKORO_DIR}"
printf '  Worker:     %s\n' "${WORKER_DST}"
