#!/usr/bin/env bash
# scripts/install.sh — installs the assistant system-wide (requires sudo).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$SCRIPT_DIR/.."

PREFIX="${PREFIX:-/usr/local}"
INSTALL_BIN="$PREFIX/bin"
INSTALL_DATA="/opt/assistant"
SERVICE_DIR="/etc/systemd/system"
SERVICE_FILE="$SERVICE_DIR/assistant.service"
CONFIG_DIR="/etc/assistant"

green() { echo -e "\033[32m$*\033[0m"; }
cyan()  { echo -e "\033[36m$*\033[0m"; }
die()   { echo -e "\033[31mERROR: $*\033[0m" >&2; exit 1; }

require_root() {
    [ "$(id -u)" -eq 0 ] || die "This script must be run as root (or with sudo)."
}

check_deps() {
    for dep in arecord aplay cmake git curl go; do
        command -v "$dep" &>/dev/null || die "Missing dependency: $dep"
    done
}

install_binaries() {
    cyan "Installing binaries to $INSTALL_BIN…"
    install -Dm755 "$ROOT/bin/assistant"    "$INSTALL_BIN/assistant"
    install -Dm755 "$ROOT/bin/llama-server" "$INSTALL_BIN/llama-server"

    green "✓ binaries installed"
}

install_models() {
    cyan "Installing models to $INSTALL_DATA/models…"
    mkdir -p "$INSTALL_DATA/models"
    cp -r "$ROOT/models/"* "$INSTALL_DATA/models/" 2>/dev/null || \
        echo "  (no models found — run 'make models' first)"
}

install_config() {
    mkdir -p "$CONFIG_DIR"
    if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
        cyan "Installing default config to $CONFIG_DIR/config.yaml…"
        # Patch paths to absolute install locations
        sed \
            -e "s|models/stt/|$INSTALL_DATA/models/stt/|g" \
            -e "s|models/tts/|$INSTALL_DATA/models/tts/|g" \
            -e "s|models/llm/|$INSTALL_DATA/models/llm/|g" \
            -e "s|./bin/llama-server|$INSTALL_BIN/llama-server|g" \
            "$ROOT/config.yaml" > "$CONFIG_DIR/config.yaml"
        green "✓ config installed — edit $CONFIG_DIR/config.yaml to customise"
    else
        echo "  (config exists — skipping)"
    fi
}

install_service() {
    if ! command -v systemctl &>/dev/null; then
        echo "  (systemd not found — skipping service installation)"
        return
    fi
    cyan "Installing systemd user service…"
    cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Voice Assistant (llama.cpp + sherpa-onnx)
After=network.target sound.target

[Service]
Type=simple
ExecStart=$INSTALL_BIN/assistant --config $CONFIG_DIR/config.yaml
Restart=on-failure
RestartSec=3s
Environment=ASSISTANT_CONFIG=$CONFIG_DIR/config.yaml
Environment=ASSISTANT_HOME=$INSTALL_DATA

# Allow access to audio devices
SupplementaryGroups=audio

[Install]
WantedBy=default.target
EOF
    systemctl daemon-reload
    green "✓ systemd service installed at $SERVICE_FILE"
    echo "  Enable:  sudo systemctl enable --now assistant"
    echo "  Logs:    journalctl -u assistant -f"
}

# ── Main ──────────────────────────────────────────────────────────────────────
require_root
check_deps

mkdir -p "$INSTALL_DATA"

install_binaries
install_models
install_config
install_service

echo ""
green "Installation complete!"
echo ""
echo "  Run once (foreground):  assistant --config $CONFIG_DIR/config.yaml"
echo "  Or enable as a service: sudo systemctl enable --now assistant"
