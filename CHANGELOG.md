# Changelog

## v0.4.0-beta.1 — 2026-08-06

Primera beta candidata después del refactor operativo WI-00..WI-09.

### Seguridad

- configuración YAML estricta y validada antes de construir dependencias;
- filesystem deshabilitado por defecto y confinado a un sandbox cuando se habilita;
- HTTP compartido con bloqueo SSRF de loopback, redes privadas, link-local, metadata y redirects inseguros;
- servicio systemd no root, sin capabilities, con filesystem de solo lectura salvo `/var/lib/xarlatan`;
- dispositivo del servicio limitado a ALSA;
- herramientas deshabilitadas en el servicio beta mediante `--no-tools`.

### Arquitectura

- `AgentRuntime` es el único loop LLM/tools;
- lifecycle de `llama-server` separado del cliente HTTP;
- memoria acotada por bytes JSON y turnos completos;
- aplicación de voz testeable con captura, STT, conversación, síntesis y playback separados;
- cancelación y errores diferenciados por etapa.

### Operación

- build versionable;
- `make all` construye `assistant`, `calibrate` y `llama-server`;
- instalación idempotente;
- configuración instalada en `/etc/xarlatan`;
- datos/modelos en `/var/lib/xarlatan`;
- rollback root-only en `/var/backups/xarlatan`;
- uninstall y purge explícitos;
- paquete determinista con SHA-256;
- preflight y aceptación física con reporte.

### Cambios incompatibles

- el binario instalado se llama `/usr/local/bin/xarlatan`, no `/usr/local/bin/assistant`;
- el servicio se llama `xarlatan.service`, no `assistant.service`;
- la configuración instalada se encuentra en `/etc/xarlatan/config.yaml`;
- los modelos instalados se encuentran bajo `/var/lib/xarlatan/models`;
- `--reset` fue eliminado porque la memoria sigue siendo volátil;
- los targets `dev-up`, `dev-down`, `dev-shell`, `dev-rebuild` y `dev-logs` fueron eliminados porque no existía `compose.yml`;
- el servicio inicia con `--no-tools`; las tools deben habilitarse deliberadamente fuera del gate beta;
- los paths de configuración de producción son absolutos.

### Límites conocidos

- no hay wake word ni captura continua;
- se inicia `arecord` por turno;
- STT/TTS no son streaming;
- no hay barge-in;
- la memoria no persiste entre reinicios;
- sherpa-onnx offline no puede interrumpir una llamada CGo ya iniciada;
- el servicio de sistema puede requerir un dispositivo ALSA explícito en equipos donde `default` depende de la sesión PipeWire del usuario;
- el modelo Gemma 3 270M se utiliza como smoke ligero, no como modelo de máxima calidad.

### Aceptación

El tag o paquete constituye un release candidate. La beta queda aceptada en una máquina únicamente después de un reporte `PASS` generado por `scripts/beta_acceptance.sh`.
