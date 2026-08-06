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

Comprueba la sesión de audio del usuario:

```bash
arecord -L
aplay -L
test -S "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/pipewire-0"
```

## 3. Construir el candidato

En una instalación nueva:

```bash
make clean-all
make all VERSION=v0.4.0-beta.1
```

Después de un hotfix que solo cambia Go, Makefile o instalación:

```bash
make build VERSION=v0.4.0-beta.1
```

Verifica:

```bash
./bin/assistant -version
```

La salida exacta debe ser:

```text
assistant v0.4.0-beta.1
```

## 4. Descargar y validar modelos

```bash
make models
```

La configuración beta utiliza:

```text
models/stt/sherpa-onnx-whisper-base/
models/tts/vits-piper-es_ES-davefx-medium/
models/llm/qwen2.5-0.5b-instruct-q4_k_m.gguf
```

El LLM se valida mediante SHA-256 antes de moverse a su ruta final.

## 5. Instalar sin habilitar el servicio

```bash
sudo bash ./scripts/install.sh
systemctl --user daemon-reload
```

La instalación:

- copia los binarios a `/usr/local/bin`;
- copia las dependencias CGo no pertenecientes al sistema a `/usr/local/lib/xarlatan`;
- registra esa ruta en `/etc/ld.so.conf.d/xarlatan.conf`;
- instala la unidad en `/etc/systemd/user/xarlatan.service`;
- elimina la unidad legacy `/etc/systemd/system/xarlatan.service`;
- conserva configuración y modelos en `/etc/xarlatan` y `/var/lib/xarlatan`;
- asigna acceso al usuario de escritorio que ejecutó `sudo`.

Verifica que el binario ya no dependa del entorno del shell:

```bash
env -u LD_LIBRARY_PATH /usr/local/bin/xarlatan -version
env -u LD_LIBRARY_PATH ldd /usr/local/bin/xarlatan | grep 'not found' && exit 1 || true
```

Verifica acceso y ownership:

```bash
test -r /etc/xarlatan/config.yaml && echo 'config readable'
test -r /var/lib/xarlatan/models/llm/qwen2.5-0.5b-instruct-q4_k_m.gguf \
  && echo 'LLM readable'

sudo stat -c '%U:%G %a %n' \
  /etc/xarlatan/config.yaml \
  /var/lib/xarlatan/models/llm/qwen2.5-0.5b-instruct-q4_k_m.gguf \
  /etc/systemd/user/xarlatan.service \
  /etc/ld.so.conf.d/xarlatan.conf
```

En `dakota-fedora`, la configuración debe pertenecer a `root:dakota` y los modelos a `dakota:dakota`, ambos sin acceso público.

## 6. Preflight instalado

```bash
EXPECTED_VERSION=v0.4.0-beta.1 \
XARLATAN_BIN=/usr/local/bin/xarlatan \
LLAMA_SERVER_BIN=/usr/local/bin/llama-server \
bash ./scripts/preflight.sh ./config.yaml
```

No continúes si aparece un `FAIL`. El preflight comprueba explícitamente el binario sin `LD_LIBRARY_PATH` y la sesión PipeWire del usuario.

## 7. Ejecutar la aceptación completa

```bash
EXPECTED_VERSION=v0.4.0-beta.1 \
AUDIO_DEVICE=default \
bash ./scripts/beta_acceptance.sh ./config.yaml
```

El script exige:

1. preflight local;
2. captura ALSA real;
3. reproducción confirmada;
4. arranque y parada mediante `systemctl --user`;
5. interacción `voz -> STT -> LLM -> TTS -> audio`;
6. ausencia de turnos espontáneos después del playback;
7. reporte final `PASS`.

Formula exactamente una pregunta y luego permanece en silencio:

```text
¿Cuánto es tres por dos?
```

## 8. Habilitar después del PASS

```bash
systemctl --user enable --now xarlatan
systemctl --user status xarlatan --no-pager
journalctl --user -u xarlatan -n 80 --no-pager
```

La unidad se inicia al comenzar la sesión gráfica del usuario. No necesita un contenedor ni un usuario de sistema separado.

## 9. Diagnóstico

### Dependencias dinámicas

```bash
env -u LD_LIBRARY_PATH ldd /usr/local/bin/xarlatan
sudo ls -lh /usr/local/lib/xarlatan
cat /etc/ld.so.conf.d/xarlatan.conf
sudo ldconfig
```

No debe aparecer ninguna biblioteca como `not found`.

### Audio o PipeWire

```bash
systemctl --user status pipewire wireplumber --no-pager
test -S "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/pipewire-0"
arecord -D default -f S16_LE -r 16000 -c 1 -d 1 /tmp/xarlatan-audio.wav
aplay -D default /tmp/xarlatan-audio.wav
rm -f /tmp/xarlatan-audio.wav
```

No pruebes `default` como el usuario de sistema legacy `xarlatan`; ese usuario no pertenece a la sesión PipeWire.

### Servicio

```bash
systemctl --user daemon-reload
systemctl --user restart xarlatan
systemctl --user status xarlatan --no-pager -l
journalctl --user -u xarlatan -b -n 150 --no-pager
```

### LLM, STT o TTS

```bash
sudo ls -lh /var/lib/xarlatan/models/llm/
sudo ls -lh /var/lib/xarlatan/models/stt/sherpa-onnx-whisper-base/
sudo ls -lh /var/lib/xarlatan/models/tts/vits-piper-es_ES-davefx-medium/
bash ./scripts/preflight.sh ./config.yaml
```

## 10. Rollback y desinstalación

```bash
systemctl --user disable --now xarlatan 2>/dev/null || true
sudo make rollback
systemctl --user daemon-reload
```

Para remover binarios, librerías y unidad preservando configuración y modelos:

```bash
sudo make uninstall
```

Para borrar también configuración, modelos y snapshot:

```bash
sudo PURGE=1 make uninstall
```

## 11. Evidencia

Conserva:

- `beta-acceptance-<timestamp>.md`;
- `env -u LD_LIBRARY_PATH /usr/local/bin/xarlatan -version`;
- `env -u LD_LIBRARY_PATH ldd /usr/local/bin/xarlatan`;
- `git rev-parse HEAD`;
- `systemctl --user status xarlatan` sin contenido conversacional.
