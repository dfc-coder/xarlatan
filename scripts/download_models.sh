#!/usr/bin/env bash
# scripts/download_models.sh — downloads default models for sherpa-onnx ASR, TTS and llama.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$SCRIPT_DIR/.."
MODEL_DIR="$ROOT/models"

mkdir -p "$MODEL_DIR"/{stt,tts,llm}

# ── Colour helpers ────────────────────────────────────────────────────────────
green() { echo -e "\033[32m$*\033[0m"; }
cyan()  { echo -e "\033[36m$*\033[0m"; }
die()   { echo -e "\033[31mERROR: $*\033[0m" >&2; exit 1; }

# ── Sherpa STT model ──────────────────────────────────────────────────────────
STT_MODEL="${STT_MODEL:-base}"  # tiny | base
STT_ASSET="sherpa-onnx-whisper-${STT_MODEL}.tar.bz2"
STT_URL="https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/${STT_ASSET}"
STT_DIR="$MODEL_DIR/stt/sherpa-onnx-whisper-${STT_MODEL}"

if [ ! -f "$STT_DIR/${STT_MODEL}-encoder.onnx" ]; then
    cyan "Downloading STT model: $STT_MODEL"
    tmp_file="$(mktemp)"
    curl -fL --progress-bar "$STT_URL" -o "$tmp_file"
    tar -xjf "$tmp_file" -C "$MODEL_DIR/stt"
    rm -f "$tmp_file"
    green "✓ STT: models/stt/sherpa-onnx-whisper-${STT_MODEL}/${STT_MODEL}-encoder.onnx"
else
    echo "✓ STT model already present"
fi

# ── Sherpa TTS model ──────────────────────────────────────────────────────────
# Default: Spanish (es_ES) — davefx medium quality voice
TTS_MODEL="${TTS_MODEL:-es_ES-davefx-medium}"
TTS_ASSET="vits-piper-${TTS_MODEL}.tar.bz2"
TTS_URL="https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/${TTS_ASSET}"
TTS_DIR="$MODEL_DIR/tts/vits-piper-${TTS_MODEL}"

if [ ! -f "$TTS_DIR/${TTS_MODEL}.onnx" ]; then
    cyan "Downloading TTS model: $TTS_MODEL"
    tmp_file="$(mktemp)"
    curl -fL --progress-bar "$TTS_URL" -o "$tmp_file"
    tar -xjf "$tmp_file" -C "$MODEL_DIR/tts"
    rm -f "$tmp_file"
    green "✓ TTS: models/tts/vits-piper-${TTS_MODEL}/${TTS_MODEL}.onnx"
else
    echo "✓ TTS model already present"
fi

# ── LLM model ─────────────────────────────────────────────────────────────────
# Default: Gemma 3 270M Q4_K_M — lightweight starter model
LLM_REPO="${LLM_REPO:-google/gemma-3-270m-it-GGUF}"
LLM_FILE="${LLM_FILE:-gemma-3-270m-it-Q4_K_M.gguf}"
LLM_URL="https://huggingface.co/${LLM_REPO}/resolve/main/${LLM_FILE}"

if [ ! -f "$MODEL_DIR/llm/$LLM_FILE" ]; then
    cyan "Downloading LLM: $LLM_FILE  (this may take a while…)"
    curl -fL --progress-bar "$LLM_URL" -o "$MODEL_DIR/llm/$LLM_FILE"
    green "✓ LLM: models/llm/$LLM_FILE"
else
    echo "✓ LLM model already present"
fi

echo ""
green "All models ready. Update config.yaml if you chose different models."
cat <<EOF

  Suggested config.yaml entries:
  ──────────────────────────────
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
