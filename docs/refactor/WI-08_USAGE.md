# WI-08 — Uso operativo

## Preparación

Construir como usuario normal:

```bash
make all
make models
```

Validar los artefactos:

```bash
./bin/assistant -version
test -x ./bin/calibrate
test -x ./bin/llama-server
```

## Instalación

```bash
sudo make install
```

La instalación no habilita ni inicia el servicio. Conserva la configuración existente y crea un snapshot del estado anterior en `/var/backups/xarlatan`.

Revisar:

```bash
sudoedit /etc/xarlatan/config.yaml
sudo systemd-analyze verify /etc/systemd/system/xarlatan.service
sudo systemd-analyze security --offline=yes /etc/systemd/system/xarlatan.service
```

Confirmar que los modelos configurados existen:

```bash
sudo -u xarlatan test -r /var/lib/xarlatan/models/stt/sherpa-onnx-whisper-base/base-encoder.onnx
sudo -u xarlatan test -r /var/lib/xarlatan/models/tts/vits-piper-es_ES-davefx-medium/es_ES-davefx-medium.onnx
sudo -u xarlatan test -r /var/lib/xarlatan/models/llm/gemma-3-270m-it-Q4_K_M.gguf
```

## Audio

Un servicio del sistema no hereda la sesión PipeWire del usuario. Enumerar dispositivos ALSA:

```bash
arecord -L
aplay -L
```

Probar el dispositivo elegido antes de iniciar Xarlatan:

```bash
arecord -D plughw:CARD=<card>,DEV=<dev> -f S16_LE -r 16000 -c 1 -d 3 /tmp/xarlatan-mic.wav
aplay -D plughw:CARD=<card>,DEV=<dev> /tmp/xarlatan-mic.wav
```

Configurar el mismo valor en `audio.device`.

## Inicio

```bash
sudo systemctl enable --now xarlatan
systemctl status xarlatan --no-pager
journalctl -u xarlatan -f
```

La unidad arranca con `--no-tools`. Para habilitar tools hay que editar explícitamente la unidad y revisar antes `ToolPolicy`, filesystem sandbox y configuración de red.

## Diagnóstico

```bash
sudo -u xarlatan /usr/local/bin/xarlatan -version
sudo -u xarlatan test -r /etc/xarlatan/config.yaml
sudo journalctl -u xarlatan -b --no-pager
sudo systemctl show xarlatan -p User -p Group -p ReadWritePaths -p NoNewPrivileges
```

Errores frecuentes:

- `config validation`: revisar paths absolutos y permisos;
- `llm server start/readiness`: verificar binario, modelo, puerto y memoria;
- `stt` o `tts`: verificar modelos sherpa y archivos de tokens/data;
- `arecord`/`aplay`: usar un dispositivo ALSA accesible al grupo `audio`;
- reinicios repetidos: detener el servicio y ejecutar foreground para aislar la etapa.

## Foreground de recuperación

```bash
sudo systemctl stop xarlatan
sudo -u xarlatan /usr/local/bin/xarlatan \
  -config /etc/xarlatan/config.yaml \
  -no-tools \
  -log debug
```

## Rollback

Restaurar binarios y unidad previos:

```bash
sudo make rollback
sudo systemctl daemon-reload
sudo systemctl restart xarlatan
```

La configuración y los modelos no se modifican durante rollback.

## Desinstalación

Preservar configuración y modelos:

```bash
sudo make uninstall
```

Eliminar todo:

```bash
sudo PURGE=1 make uninstall
```
