# Instalador RPM nativo — alcance posterior a WI-09

## Regla de inicio

No comenzar el empaquetado RPM hasta que #12 — WI-09 tenga un reporte físico `PASS` en `dakota-fedora`. El RPM debe empaquetar un runtime validado; no debe utilizarse para compensar fallos de audio, modelos, configuración o systemd.

## Experiencia objetivo

```bash
sudo dnf install ./xarlatan-0.4.0-0.1.beta1.x86_64.rpm
sudo xarlatan-setup
sudo systemctl enable --now xarlatan
```

No se utilizarán contenedores para ejecutar el asistente.

## Contenido del RPM base

- `/usr/bin/xarlatan`;
- `/usr/bin/xarlatan-calibrate`;
- `/usr/libexec/xarlatan/llama-server`;
- `/usr/lib/systemd/system/xarlatan.service`;
- `/etc/xarlatan/config.yaml` como configuración preservable en upgrades;
- usuario/grupo de sistema mediante mecanismos nativos de Fedora;
- directorios de estado bajo `/var/lib/xarlatan`;
- documentación, licencia, changelog y comandos de diagnóstico.

## Modelos

Los modelos STT, TTS y LLM no se incrustan inicialmente en el RPM base por tamaño, actualización y licencias. `xarlatan-setup` debe:

- descargar únicamente los modelos seleccionados;
- verificar checksums;
- escribirlos en `/var/lib/xarlatan/models`;
- poder reanudarse sin destruir modelos existentes;
- no habilitar herramientas mutables.

## Comportamiento de instalación

- no iniciar ni habilitar el servicio automáticamente;
- ejecutar `daemon-reload` mediante macros RPM apropiadas;
- preservar una configuración modificada durante upgrade;
- no borrar modelos ni configuración al remover el paquete base;
- soportar upgrade y downgrade documentados;
- mantener el servicio no root y el hardening validado en WI-08.

## Gates

- build reproducible con `rpmbuild` y validación en entorno Fedora limpio;
- `rpmlint` sin errores de bloqueo;
- instalación, upgrade, downgrade y erase;
- configuración local preservada;
- servicio no root;
- `systemd-analyze verify/security`;
- aceptación beta ejecutada nuevamente desde la instalación RPM;
- checksum y artefacto RPM publicados.
