# Xarlatan v0.4.0-beta.1 — primera prueba en Fedora

Este runbook está preparado para el host `dakota-fedora` y el checkout histórico `~/Documents/projects/assistant`.

## 1. Actualizar el checkout

```bash
cd ~/Documents/projects/assistant
git fetch --all --tags --prune
git switch main
git pull --ff-only
```

Antes de probar la beta, confirma que `git status --short` no contiene cambios personales sin guardar.

## 2. Dependencias Fedora

```bash
sudo dnf install -y \
  gcc gcc-c++ make cmake git curl tar bzip2 \
  golang alsa-utils alsa-lib-devel ShellCheck
```

Comprueba el audio visible en la sesión:

```bash
arecord -L
aplay -L
```

Para la primera prueba usa foreground. Un servicio de sistema no comparte automáticamente todos los dispositivos PipeWire de la sesión de escritorio.

## 3. Construir el candidato

```bash
make clean-all
make all VERSION=v0.4.0-beta.1
./bin/assistant -version
```

La última línea debe devolver exactamente:

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

Descarga o conserva los modelos existentes:

```bash
make models
```

No borres modelos locales funcionales para repetir una descarga innecesaria. El preflight valida archivos y paths antes de iniciar.

## 5. Preflight desde el checkout

```bash
EXPECTED_VERSION=v0.4.0-beta.1 \
XARLATAN_BIN=./bin/assistant \
LLAMA_SERVER_BIN=./bin/llama-server \
./scripts/preflight.sh ./config.yaml
```

No continúes si aparece un `FAIL`. Los `WARN` del grupo `audio` pueden ser compatibles con PipeWire en foreground, pero deben verificarse mediante la captura real del paso siguiente.

## 6. Primera aceptación en foreground

```bash
EXPECTED_VERSION=v0.4.0-beta.1 \
XARLATAN_BIN=./bin/assistant \
LLAMA_SERVER_BIN=./bin/llama-server \
AUDIO_DEVICE=default \
./scripts/beta_acceptance.sh ./config.yaml
```

El script realiza:

1. preflight;
2. grabación ALSA de cuatro segundos;
3. reproducción y confirmación humana;
4. smoke del servicio si ya está instalado;
5. ventana de 45 segundos para formular una pregunta y confirmar respuesta hablada;
6. creación de `beta-acceptance-<timestamp>.md`.

El reporte no incluye transcripción, prompt, respuesta ni secretos. Un resultado `PASS` es la evidencia que cierra REL-003 y WI-09.

## 7. Pregunta de prueba recomendada

Usa una pregunta corta y determinista:

```text
¿Qué día viene después del lunes?
```

La aceptación requiere oír una respuesta coherente. No evalúa todavía wake word, escucha continua, streaming ni barge-in; esas funciones pertenecen a Fase 2.

## 8. Instalar después del foreground PASS

```bash
sudo make install
sudo systemctl daemon-reload
```

Revisa antes de habilitar:

```bash
sudo systemd-analyze verify /etc/systemd/system/xarlatan.service
sudo systemd-analyze security --offline=yes /etc/systemd/system/xarlatan.service
sudo -u xarlatan test -r /etc/xarlatan/config.yaml
```

## 9. Probar el servicio

```bash
sudo systemctl start xarlatan
sudo systemctl status xarlatan --no-pager
sudo journalctl -u xarlatan -n 80 --no-pager
sudo systemctl stop xarlatan
```

No habilites el arranque automático hasta que capture y reproduzca correctamente con el dispositivo ALSA configurado:

```bash
sudo systemctl enable --now xarlatan
```

## 10. Diagnóstico

### `arecord: audio open error`

```bash
arecord -L
pactl info 2>/dev/null || true
```

Prueba foreground con `AUDIO_DEVICE=default`. Para systemd puede ser necesario configurar un dispositivo ALSA explícito, por ejemplo uno obtenido de `arecord -l`; no adivines `hw:X,Y`.

### El LLM no inicia

```bash
/usr/local/bin/llama-server --version
ls -lh /var/lib/xarlatan/models/llm/
sudo journalctl -u xarlatan -n 100 --no-pager
```

Confirma que `llm.model`, `llm.server_binary`, host y puerto de `/etc/xarlatan/config.yaml` coinciden con los archivos instalados.

### STT o TTS falla

```bash
ls -lh models/stt/sherpa-onnx-whisper-base/
ls -lh models/tts/vits-piper-es_ES-davefx-medium/
```

Ejecuta nuevamente `scripts/preflight.sh`. No habilites el servicio con un preflight rojo.

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

Verifica:

```bash
/usr/local/bin/xarlatan -version
sudo systemctl status xarlatan --no-pager || true
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
- salida de `./bin/assistant -version`;
- SHA del commit probado: `git rev-parse HEAD`;
- resultado de `systemctl status` sin contenido conversacional.
