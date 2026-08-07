# Changelog

## v0.5.0-beta.1

- Event-driven Coordinator with persistent workers and TurnID cancellation.
- Persistent playback and sentence-level LLM-to-TTS streaming.
- Silero VAD with single continuous microphone capture and bounded pre-roll.
- Transcript wake gate and bounded partial/final STT preview semantics.
- Wake-qualified physical barge-in with conservative echo candidate filtering.
- Dedicated physical acceptance for continuous capture, wake, partial STT, interruption recovery and self-trigger regression.

## v0.4.0-beta.1 — 2026-08-06

Primera beta candidata después del refactor operativo WI-00..WI-09.

### Seguridad

- configuración YAML estricta y validada antes de construir dependencias;
- filesystem deshabilitado por defecto y confinado a un sandbox cuando se habilita;
- HTTP compartido con bloqueo SSRF de loopback, redes privadas, link-local, metadata y redirects inseguros;
- servicio systemd de usuario, sin root ni capabilities, con filesystem de solo lectura salvo `/var/lib/xarlatan`;
- herramientas deshabilitadas en el servicio beta mediante `--no-tools`;
- el LLM beta se descarga a un archivo temporal y se valida mediante SHA-256;
- configuración instalada `0640` bajo `root:<grupo del usuario objetivo>`;
- modelos instalados `0640` y directorios `0750` bajo el usuario de escritorio objetivo.

### Arquitectura

- `AgentRuntime` es el único loop LLM/tools;
- lifecycle de `llama-server` separado del cliente HTTP;
- memoria acotada por bytes JSON y turnos completos;
- aplicación de voz testeable con captura, STT, conversación, síntesis y playback separados;
- guard half-duplex posterior al playback y filtro de markers no verbales de STT;
- cancelación y errores diferenciados por etapa.

### Operación

- build versionable;
- `make all` construye `assistant`, `calibrate` y `llama-server`;
- `make build` fuerza la reconstrucción de los binarios Go;
- versiones con o sin prefijo `v` producen una única salida `assistant v<versión>`;
- instalación idempotente;
- configuración en `/etc/xarlatan` y modelos en `/var/lib/xarlatan`;
- dependencias CGo externas instaladas en `/usr/local/lib/xarlatan` y registradas con `ldconfig`;
- unidad instalada en `/etc/systemd/user/xarlatan.service` para usar la sesión PipeWire del escritorio;
- migración automática desde la unidad legacy `/etc/systemd/system/xarlatan.service`;
- rollback root-only en `/var/backups/xarlatan`;
- uninstall y purge explícitos;
- paquete determinista con SHA-256 y librerías nativas incluidas;
- preflight y aceptación física con reporte;
- `make models` ejecuta un único ciclo de descarga y validación.

### Hotfixes de primera ejecución

- se reemplazó el repositorio restringido de Gemma por `Qwen/Qwen2.5-0.5B-Instruct-GGUF`;
- los archivos vacíos, parciales o con checksum incorrecto se rechazan;
- se corrigió `assistant vv0.4.0-beta.1` normalizando la versión;
- se normalizaron permisos de configuración y modelos;
- se evitó la autoactivación por eco mediante cooldown, silencio estable y filtrado de `[Música]`/markers equivalentes;
- se corrigió el fallo `libsherpa-onnx-c-api.so: cannot open shared object file` empaquetando el runtime dinámico;
- se reemplazó el servicio de sistema por un servicio systemd de usuario porque ALSA `default` depende de PipeWire y devolvía `Host is down` para el usuario de sistema aislado.

### Cambios incompatibles

- el binario instalado se llama `/usr/local/bin/xarlatan`;
- la configuración instalada se encuentra en `/etc/xarlatan/config.yaml`;
- los modelos instalados se encuentran bajo `/var/lib/xarlatan/models`;
- el servicio se administra con `systemctl --user`, no con `sudo systemctl`;
- la unidad se encuentra en `/etc/systemd/user/xarlatan.service`;
- `--reset` fue eliminado porque la memoria sigue siendo volátil;
- los targets Compose inexistentes fueron eliminados;
- el servicio inicia con `--no-tools`;
- los paths de configuración de producción son absolutos.

### Límites conocidos

- no hay wake word ni captura continua;
- se inicia `arecord` por turno;
- STT/TTS no son streaming;
- no hay cancelación acústica de eco real ni barge-in;
- la memoria no persiste entre reinicios;
- sherpa-onnx offline no puede interrumpir una llamada CGo ya iniciada;
- Qwen2.5 0.5B se utiliza como smoke ligero, no como modelo de máxima calidad.

### Aceptación

El tag o paquete constituye un release candidate. La beta queda aceptada únicamente después de un reporte `PASS` generado por `scripts/beta_acceptance.sh`, incluyendo el gate `systemd user startup/stop`.
