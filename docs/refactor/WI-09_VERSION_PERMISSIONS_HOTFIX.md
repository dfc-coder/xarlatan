# WI-09 hotfix — versionado, rebuild y permisos de aceptación

## Evidencia observada en dakota-fedora

Después de integrar el hotfix de descarga de modelos:

- `make models` completó STT, TTS y Qwen2.5 0.5B;
- `sudo make install VERSION=v0.4.0-beta.1` completó;
- `/usr/local/bin/xarlatan -version` devolvió `assistant vv0.4.0-beta.1`;
- el usuario interactivo no pudo leer `/etc/xarlatan/config.yaml` ni `/var/lib/xarlatan/models/...`.

## Diagnóstico

1. `main.go` antepone `v` al valor recibido en `buildVersion`; cuando Make recibe una versión ya normalizada con `v`, la duplica.
2. Los targets de binarios no tienen dependencias de fuente ni un prerequisito forzado; `make install` puede reutilizar binarios anteriores.
3. La configuración instalada debe permanecer restringida a `root:xarlatan`; no corresponde hacerla pública solo para ejecutar el gate.
4. La aceptación foreground debe usar `config.yaml` y modelos del checkout, mientras el smoke systemd valida la instalación real bajo el usuario `xarlatan`.
5. La instalación debe normalizar permisos de modelos para que el usuario del servicio pueda leerlos aunque el archivo fuente se haya creado con modo `0600`.

## Corrección

- normalizar la versión inyectada para admitir `0.4.0-beta.1` y `v0.4.0-beta.1` sin duplicar prefijo;
- añadir un contrato ejecutable de versión y rebuild;
- forzar rebuild de los binarios Go cuando se ejecuta `make build` o `make install`;
- instalar directorios de modelos con modo `0750` y archivos con modo `0640`, propiedad `xarlatan:xarlatan`;
- validar explícitamente que el usuario `xarlatan` puede leer configuración y modelos;
- ejecutar el foreground de aceptación con `./config.yaml`, conservando el smoke de systemd sobre `/etc/xarlatan/config.yaml`.

## Estado

WI-09 permanece abierto hasta obtener el reporte físico `PASS`.
