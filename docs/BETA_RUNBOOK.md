# Xarlatan v0.4.0-beta.1 — primera prueba en Fedora

Este runbook está preparado para `dakota-fedora` y el checkout `~/Documents/projects/xarlatan`.

## 1. Actualizar el checkout

```bash
cd ~/Documents/projects/xarlatan
git fetch --all --tags --prune
git switch main
git pull --ff-only
git status --short
```

No continúes si el último comando muestra cambios personales sin guardar.

## 2. Dependencias Fedora

```bash
sudo dnf install -y \
  gcc gcc-c++ make cmake git curl tar bzip2 \
  golang alsa-utils alsa-lib-devel ShellCheck
```

Comprueba los dispositivos visibles:

```bash
arecord -L
aplay -L
```

## 3. Construir el candidato

```bash
make clean-all
make all VERSION=v0.4.0-beta.1
./bin/assistant -version
```

La salida debe ser:

```text
assistant v0.4.0-beta.1
```

## 4. Descargar y validar modelos

La configuración beta utiliza:

```text
models/stt/sherpa-onnx-whisper-base/
models/tts/vits-piper-es_ES-davefx-medium/
models/llm/qwen2.5-0.5b-instruct-q4_k_m.gguf
```

Ejecuta:

```bash
make models
```

El LLM se descarga desde el repositorio público `Qwen/Qwen2.5-0.5B-Instruct-GGUF`, se valida mediante SHA-256 y solo después se mueve a su ruta final. Un archivo vacío, parcial o con checksum incorrecto no se acepta.

### Recuperación desde la primera ejecución fallida

La versión inicial intentaba descargar un Gemma restringido y podía dejar un archivo incompleto. Después de actualizar el checkout:

```bash
rm -f models/llm/gemma-3-270m-it-Q4_K_M.gguf
make models
```

## 5. Preflight desde el checkout

```bash
EXPECTED_VERSION=v0.4.0-beta.1 \
XARLATAN_BIN=./bin/assistant \
LLAMA_SERVER_BIN=./bin/llama-server \
bash ./scripts/preflight.sh ./config.yaml
```

No continúes si aparece un `FAIL`.

## 6. Instalar sin habilitar el servicio

```bash
sudo make install
sudo systemctl daemon-reload
```

La instalación preserva una configuración existente. Si `/etc/xarlatan/config.yaml` fue creado antes del hotfix, actualiza únicamente el modelo por defecto:

```bash
sudo sed -i \
  's#gemma-3-270m-it-Q4_K_M.gguf#qwen2.5-0.5b-instruct-q4_k_m.gguf#' \
  /etc/xarlatan/config.yaml
```

Verifica:

```bash
/usr/local/bin/xarlatan -version
grep 'model:' /etc/xarlatan/config.yaml
sudo systemd-analyze verify /etc/systemd/system/xarlatan.service
sudo systemd-analyze security --offline=yes /etc/systemd/system/xarlatan.service
sudo -u xarlatan test -r /etc/xarlatan/config.yaml
```

## 7. Ejecutar la aceptación completa

```bash
EXPECTED_VERSION=v0.4.0-beta.1 \
AUDIO_DEVICE=default \
bash ./scripts/beta_acceptance.sh /etc/xarlatan/config.yaml
```

Se usa `bash` deliberadamente para que el gate no dependa del bit ejecutable conservado por el método de checkout.

El script exige para un `PASS`:

1. preflight con binarios, configuración y modelos instalados;
2. captura ALSA real;
3. reproducción confirmada;
4. arranque y parada de `xarlatan.service`;
5. interacción completa `voz -> STT -> LLM -> TTS -> audio`;
6. reporte `beta-acceptance-<timestamp>.md`.

## 8. Pregunta de prueba

```text
¿Qué día viene después del lunes?
```

Debes oír una respuesta coherente. Esta beta todavía no incluye wake word, captura continua, streaming ni barge-in.

## 9. Habilitar después del PASS

```bash
sudo systemctl enable --now xarlatan
sudo systemctl status xarlatan --no-pager
sudo journalctl -u xarlatan -n 80 --no-pager
```

## 10. Diagnóstico

### Error de audio

```bash
arecord -L
arecord -l
aplay -l
pactl info 2>/dev/null || true
```

Para systemd puede ser necesario configurar un dispositivo ALSA explícito obtenido de `arecord -l`. No adivines `hw:X,Y`.

### El LLM no inicia

```bash
ls -lh /var/lib/xarlatan/models/llm/
grep -A4 '^llm:' /etc/xarlatan/config.yaml
/usr/local/bin/llama-server --version
sudo journalctl -u xarlatan -n 100 --no-pager
```

### STT o TTS falla

```bash
ls -lh /var/lib/xarlatan/models/stt/sherpa-onnx-whisper-base/
ls -lh /var/lib/xarlatan/models/tts/vits-piper-es_ES-davefx-medium/
bash ./scripts/preflight.sh /etc/xarlatan/config.yaml
```

### Proceso huérfano

```bash
pgrep -a llama-server || true
sudo systemctl stop xarlatan
pgrep -a llama-server || true
```

## 11. Rollback

```bash
sudo systemctl disable --now xarlatan 2>/dev/null || true
sudo make rollback
sudo systemctl daemon-reload
```

Para remover binarios preservando configuración y modelos:

```bash
sudo make uninstall
```

Para borrar también configuración, modelos y snapshot:

```bash
sudo PURGE=1 make uninstall
```

## 12. Evidencia

Conserva:

- `beta-acceptance-<timestamp>.md`;
- `/usr/local/bin/xarlatan -version`;
- `git rev-parse HEAD`;
- `systemctl status` sin contenido conversacional.
