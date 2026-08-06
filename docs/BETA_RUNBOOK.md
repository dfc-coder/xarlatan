# Xarlatan v0.4.0-beta.1 — primera prueba en Fedora

Este runbook está preparado para el host `dakota-fedora` y el checkout histórico `~/Documents/projects/assistant`.

## 1. Actualizar el checkout

```bash
cd ~/Documents/projects/assistant
git fetch --all --tags --prune
git switch main
git pull --ff-only
git status --short
```

No continúes si el último comando muestra cambios personales que todavía no guardaste.

## 2. Dependencias Fedora

```bash
sudo dnf install -y \
  gcc gcc-c++ make cmake git curl tar bzip2 \
  golang alsa-utils alsa-lib-devel ShellCheck
```

Comprueba los dispositivos visibles en la sesión:

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

La salida debe ser exactamente:

```text
assistant v0.4.0-beta.1
```

## 4. Modelos

La configuración beta espera:

```text
models/stt/sherpa-onnx-whisper-base/
models/tts/vits-piper-es_ES-davefx-medium/
models/llm/gemma-3-270m-it-Q4_K_M.gguf
```

Descarga los que falten:

```bash
make models
```

No borres modelos locales funcionales para repetir una descarga innecesaria.

## 5. Preflight desde el checkout

```bash
EXPECTED_VERSION=v0.4.0-beta.1 \
XARLATAN_BIN=./bin/assistant \
LLAMA_SERVER_BIN=./bin/llama-server \
./scripts/preflight.sh ./config.yaml
```

No continúes si aparece un `FAIL`. Un `WARN` indicando que el usuario no pertenece al grupo `audio` puede ser compatible con PipeWire en foreground, pero la captura real debe confirmarlo.

## 6. Instalar sin habilitar el arranque automático

```bash
sudo make install
sudo systemctl daemon-reload
```

Verifica la instalación:

```bash
/usr/local/bin/xarlatan -version
sudo systemd-analyze verify /etc/systemd/system/xarlatan.service
sudo systemd-analyze security --offline=yes /etc/systemd/system/xarlatan.service
sudo -u xarlatan test -r /etc/xarlatan/config.yaml
```

La instalación no habilita el servicio automáticamente.

## 7. Ejecutar la aceptación completa

Desde el checkout:

```bash
EXPECTED_VERSION=v0.4.0-beta.1 \
AUDIO_DEVICE=default \
./scripts/beta_acceptance.sh /etc/xarlatan/config.yaml
```

El script exige para un `PASS` final:

1. preflight con binarios y configuración instalados;
2. captura ALSA real de cuatro segundos;
3. reproducción confirmada por vos;
4. arranque, estabilidad inicial y parada de `xarlatan.service`;
5. una interacción foreground completa `voz -> STT -> LLM -> TTS -> audio`;
6. creación de `beta-acceptance-<timestamp>.md`.

El reporte no contiene transcripción, prompt, respuesta ni secretos. Un resultado `PASS` es la evidencia que completa REL-003 y permite cerrar WI-09.

## 8. Pregunta de prueba

Cuando se abra la ventana foreground, usa una pregunta corta:

```text
¿Qué día viene después del lunes?
```

Debes oír una respuesta coherente. Esta beta no evalúa wake word, escucha continua, streaming ni barge-in.

## 9. Habilitar el servicio solo después del PASS

```bash
sudo systemctl enable --now xarlatan
sudo systemctl status xarlatan --no-pager
```

Revisa logs operativos:

```bash
sudo journalctl -u xarlatan -n 80 --no-pager
```

## 10. Diagnóstico

### `arecord: audio open error`

```bash
arecord -L
arecord -l
pactl info 2>/dev/null || true
```

Prueba el foreground con `AUDIO_DEVICE=default`. Para systemd puede ser necesario configurar un dispositivo ALSA explícito obtenido de `arecord -l`; no adivines `hw:X,Y`.

### El servicio falla pero foreground funciona

Esto normalmente indica una diferencia entre ALSA y la sesión PipeWire del usuario.

```bash
sudo journalctl -u xarlatan -n 100 --no-pager
sudo systemctl cat xarlatan
arecord -l
aplay -l
```

Mantén el servicio deshabilitado y usa foreground hasta fijar `audio.device` con un dispositivo accesible al usuario `xarlatan`.

### El LLM no inicia

```bash
/usr/local/bin/llama-server --version
ls -lh /var/lib/xarlatan/models/llm/
sudo journalctl -u xarlatan -n 100 --no-pager
```

Confirma `llm.model`, `llm.server_binary`, host y puerto en `/etc/xarlatan/config.yaml`.

### STT o TTS falla

```bash
ls -lh /var/lib/xarlatan/models/stt/sherpa-onnx-whisper-base/
ls -lh /var/lib/xarlatan/models/tts/vits-piper-es_ES-davefx-medium/
./scripts/preflight.sh /etc/xarlatan/config.yaml
```

### Proceso huérfano

```bash
pgrep -a llama-server || true
sudo systemctl stop xarlatan
pgrep -a llama-server || true
```

Después de detener Xarlatan no debe quedar un `llama-server` administrado por la aplicación.

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

## 12. Evidencia a conservar

Conserva únicamente:

- `beta-acceptance-<timestamp>.md`;
- salida de `/usr/local/bin/xarlatan -version`;
- SHA del commit probado: `git rev-parse HEAD`;
- resultado de `systemctl status` sin contenido conversacional.
