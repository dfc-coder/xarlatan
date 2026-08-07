#!/usr/bin/env bash
# scripts/download_models.sh — downloads default VAD, ASR, TTS and llama models.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$SCRIPT_DIR/.."
MODEL_DIR="$ROOT/models"

mkdir -p "$MODEL_DIR"/{vad,stt,tts,llm}

green() { printf '\033[32m%s\033[0m\n' "$*"; }
cyan()  { printf '\033[36m%s\033[0m\n' "$*"; }
die()   { printf '\033[31mERROR: %s\033[0m\n' "$*" >&2; exit 1; }

download_atomic() {
    local url="$1"
    local destination="$2"
    local expected_sha256="$3"
    local tmp_file
    local actual_sha256
    tmp_file="$(mktemp "${destination}.part.XXXXXX")"

    if ! curl -fL --retry 3 --retry-delay 2 --progress-bar "$url" -o "$tmp_file"; then
        rm -f "$tmp_file"
        die "download failed: $url"
    fi
    if [[ ! -s "$tmp_file" ]]; then
        rm -f "$tmp_file"
        die "downloaded file is empty: $url"
    fi

    actual_sha256="$(sha256sum "$tmp_file" | awk '{print $1}')"
    if [[ "$actual_sha256" != "$expected_sha256" ]]; then
        rm -f "$tmp_file"
        die "checksum mismatch: $(basename "$destination")"
    fi

    mv -f "$tmp_file" "$destination"
}

# ── Silero VAD model ─────────────────────────────────────────────────────────
# k2-fsa's 16 kHz export, used directly by sherpa-onnx VoiceActivityDetector.
VAD_URL="https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/silero_vad.onnx"
VAD_SHA256="9e2449e1087496d8d4caba907f23e0bd3f78d91fa552479bb9c23ac09cbb1fd6"
VAD_PATH="$MODEL_DIR/vad/silero_vad.onnx"

vad_actual_sha256=""
if [[ -s "$VAD_PATH" ]]; then
    vad_actual_sha256="$(sha256sum "$VAD_PATH" | awk '{print $1}')"
fi
if [[ "$vad_actual_sha256" == "$VAD_SHA256" ]]; then
    green "✓ Silero VAD model already present and valid"
else
    rm -f "$VAD_PATH"
    cyan "Downloading Silero VAD"
    download_atomic "$VAD_URL" "$VAD_PATH" "$VAD_SHA256"
    green "✓ VAD: models/vad/silero_vad.onnx"
fi

# ── Sherpa STT model ──────────────────────────────────────────────────────────
STT_MODEL="${STT_MODEL:-base}"  # tiny | base
STT_ASSET="sherpa-onnx-whisper-${STT_MODEL}.tar.bz2"
STT_URL="https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/${STT_ASSET}"
STT_DIR="$MODEL_DIR/stt/sherpa-onnx-whisper-${STT_MODEL}"

if [[ ! -s "$STT_DIR/${STT_MODEL}-encoder.onnx" ]]; then
    cyan "Downloading STT model: $STT_MODEL"
    tmp_file="$(mktemp)"
    if ! curl -fL --retry 3 --retry-delay 2 --progress-bar "$STT_URL" -o "$tmp_file"; then
        rm -f "$tmp_file"
        die "STT download failed"
    fi
    if ! tar -xjf "$tmp_file" -C "$MODEL_DIR/stt"; then
        rm -f "$tmp_file"
        die "STT extraction failed"
    fi
    rm -f "$tmp_file"
    [[ -s "$STT_DIR/${STT_MODEL}-encoder.onnx" ]] || die "STT encoder missing after extraction"
    [[ -s "$STT_DIR/${STT_MODEL}-decoder.onnx" ]] || die "STT decoder missing after extraction"
    [[ -s "$STT_DIR/${STT_MODEL}-tokens.txt" ]] || die "STT tokens missing after extraction"
    green "✓ STT: models/stt/sherpa-onnx-whisper-${STT_MODEL}/${STT_MODEL}-encoder.onnx"
else
    green "✓ STT model already present"
fi

# ── Sherpa TTS model ──────────────────────────────────────────────────────────
TTS_MODEL="${TTS_MODEL:-es_ES-davefx-medium}"
TTS_ASSET="vits-piper-${TTS_MODEL}.tar.bz2"
TTS_URL="https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/${TTS_ASSET}"
TTS_DIR="$MODEL_DIR/tts/vits-piper-${TTS_MODEL}"

if [[ ! -s "$TTS_DIR/${TTS_MODEL}.onnx" ]]; then
    cyan "Downloading TTS model: $TTS_MODEL"
    tmp_file="$(mktemp)"
    if ! curl -fL --retry 3 --retry-delay 2 --progress-bar "$TTS_URL" -o "$tmp_file"; then
        rm -f "$tmp_file"
        die "TTS download failed"
    fi
    if ! tar -xjf "$tmp_file" -C "$MODEL_DIR/tts"; then
        rm -f "$tmp_file"
        die "TTS extraction failed"
    fi
    rm -f "$tmp_file"
    [[ -s "$TTS_DIR/${TTS_MODEL}.onnx" ]] || die "TTS model missing after extraction"
    [[ -s "$TTS_DIR/tokens.txt" ]] || die "TTS tokens missing after extraction"
    [[ -d "$TTS_DIR/espeak-ng-data" ]] || die "TTS espeak-ng-data missing after extraction"
    green "✓ TTS: models/tts/vits-piper-${TTS_MODEL}/${TTS_MODEL}.onnx"
else
    green "✓ TTS model already present"
fi

# ── LLM model ─────────────────────────────────────────────────────────────────
DEFAULT_LLM_REPO="Qwen/Qwen2.5-0.5B-Instruct-GGUF"
DEFAULT_LLM_FILE="qwen2.5-0.5b-instruct-q4_k_m.gguf"
DEFAULT_LLM_SHA256="74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db"
LLM_REPO="${LLM_REPO:-$DEFAULT_LLM_REPO}"
LLM_FILE="${LLM_FILE:-$DEFAULT_LLM_FILE}"
LLM_URL="https://huggingface.co/${LLM_REPO}/resolve/main/${LLM_FILE}"
LLM_PATH="$MODEL_DIR/llm/$LLM_FILE"

if [[ "$LLM_REPO" == "$DEFAULT_LLM_REPO" && "$LLM_FILE" == "$DEFAULT_LLM_FILE" ]]; then
    LLM_SHA256="${LLM_SHA256:-$DEFAULT_LLM_SHA256}"
else
    LLM_SHA256="${LLM_SHA256:-}"
    [[ -n "$LLM_SHA256" ]] || die "custom LLM requires LLM_SHA256"
fi

actual_sha256=""
if [[ -s "$LLM_PATH" ]]; then
    actual_sha256="$(sha256sum "$LLM_PATH" | awk '{print $1}')"
fi

if [[ "$actual_sha256" == "$LLM_SHA256" ]]; then
    green "✓ LLM model already present and valid"
else
    rm -f "$LLM_PATH"
    cyan "Downloading LLM: $LLM_FILE (this may take a while…)"
    download_atomic "$LLM_URL" "$LLM_PATH" "$LLM_SHA256"
    green "✓ LLM: models/llm/$LLM_FILE"
fi

printf '\n'
green "All models ready."
cat <<EOF

  Runtime model layout:
  ─────────────────────
  vad:
    model: "models/vad/silero_vad.onnx"

  stt:
    encoder: "models/stt/sherpa-onnx-whisper-${STT_MODEL}/${STT_MODEL}-encoder.onnx"
    decoder: "models/stt/sherpa-onnx-whisper-${STT_MODEL}/${STT_MODEL}-decoder.onnx"
    tokens: "models/stt/sherpa-onnx-whisper-${STT_MODEL}/${STT_MODEL}-tokens.txt"

  tts:
    model: "models/tts/vits-piper-${TTS_MODEL}/${TTS_MODEL}.onnx"
    tokens: "models/tts/vits-piper-${TTS_MODEL}/tokens.txt"
    data_dir: "models/tts/vits-piper-${TTS_MODEL}/espeak-ng-data"

  llm:
    model: "models/llm/$LLM_FILE"
EOF
