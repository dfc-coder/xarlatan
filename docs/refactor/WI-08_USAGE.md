# WI-08 — Uso operativo

## Preparación

Construir y descargar modelos como usuario normal:

```bash
make all VERSION=v0.4.0-beta.1
make models
./bin/assistant -version
```

## Instalación

```bash
sudo make install VERSION=v0.4.0-beta.1
systemctl --user daemon-reload
```

El instalador obtiene el usuario objetivo desde `SUDO_USER`, conserva la configuración existente y crea un snapshot en `/var/backups/xarlatan`.

Instala:

```text
/usr/local/bin/xarlatan
/usr/local/bin/xarlatan-calibrate
/usr/local/bin/llama-server
/usr/local/lib/xarlatan/
/etc/ld.so.conf.d/xarlatan.conf
/etc/xarlatan/config.yaml
/etc/systemd/user/xarlatan.service
/var/lib/xarlatan/models/
```

## Verificación

```bash
env -u LD_LIBRARY_PATH /usr/local/bin/xarlatan -version
env -u LD_LIBRARY_PATH ldd /usr/local/bin/xarlatan

test -r /etc/xarlatan/config.yaml
test -r /var/lib/xarlatan/models/stt/sherpa-onnx-whisper-base/base-encoder.onnx
test -r /var/lib/xarlatan/models/tts/vits-piper-es_ES-davefx-medium/es_ES-davefx-medium.onnx
test -r /var/lib/xarlatan/models/llm/qwen2.5-0.5b-instruct-q4_k_m.gguf

systemd-analyze verify /etc/systemd/user/xarlatan.service
```

`ldd` no debe mostrar ninguna dependencia `not found`.

## Audio

Xarlatan utiliza la sesión PipeWire del usuario de escritorio:

```bash
test -S "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/pipewire-0"
arecord -D default -f S16_LE -r 16000 -c 1 -d 3 /tmp/xarlatan-mic.wav
aplay -D default /tmp/xarlatan-mic.wav
rm -f /tmp/xarlatan-mic.wav
```

No debe ejecutarse como el usuario de sistema legacy `xarlatan`; ese contexto no puede acceder al dispositivo `default` de PipeWire.

## Inicio

```bash
systemctl --user enable --now xarlatan
systemctl --user status xarlatan --no-pager
journalctl --user -u xarlatan -f
```

La unidad se inicia al abrir la sesión del usuario y arranca con `--no-tools`.

## Diagnóstico

```bash
systemctl --user restart xarlatan
systemctl --user status xarlatan --no-pager -l
journalctl --user -u xarlatan -b --no-pager
systemctl --user show xarlatan -p ReadWritePaths -p NoNewPrivileges
```

Errores frecuentes:

- `shared libraries ... not found`: ejecutar `sudo ldconfig` y revisar `/usr/local/lib/xarlatan`;
- `Host is down`: comprobar que se usa `systemctl --user` y que PipeWire está activo;
- `config validation`: revisar paths y permisos;
- `llm server start/readiness`: verificar modelo, puerto y memoria;
- `stt` o `tts`: verificar modelos y archivos de tokens/data.

## Foreground de recuperación

```bash
systemctl --user stop xarlatan
/usr/local/bin/xarlatan \
  -config /etc/xarlatan/config.yaml \
  -no-tools \
  -log debug
```

## Rollback

```bash
systemctl --user disable --now xarlatan 2>/dev/null || true
sudo make rollback
systemctl --user daemon-reload
```

## Desinstalación

Preservar configuración y modelos:

```bash
sudo make uninstall
```

Eliminar todo:

```bash
sudo PURGE=1 make uninstall
```
