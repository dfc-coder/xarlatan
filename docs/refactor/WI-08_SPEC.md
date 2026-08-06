# WI-08 — Build, instalación y systemd

## Objetivo

Entregar una instalación Linux reproducible, idempotente, no root en runtime y reversible para Xarlatan, compatible con la sesión PipeWire del escritorio.

## Requisitos

- OPS-001: `make all` produce `assistant`, `calibrate` y `llama-server`.
- OPS-002: `make install` depende de todos los artefactos requeridos.
- OPS-003: no existen targets compose sin archivo compose real.
- OPS-004: la versión se inyecta en una variable build-time y `-version` la muestra.
- OPS-005: el asistente se ejecuta como servicio systemd de usuario, con la identidad del usuario de escritorio y sin root.
- OPS-006: la unidad aplica hardening systemd verificable.
- OPS-007: solo `/var/lib/xarlatan` es escribible para el servicio.
- OPS-008: la configuración instalada usa paths absolutos, filesystem deshabilitado y sin tools por defecto.
- OPS-009: instalar dos veces produce el mismo estado.
- OPS-010: existe uninstall y rollback del último despliegue.
- OPS-011: logs van al journal de usuario y no contienen secretos por configuración.
- OPS-012: se elimina `--reset` mientras la memoria no sea persistente.
- OPS-013: todas las dependencias dinámicas no pertenecientes al sistema se instalan en una ruta estable y el binario funciona sin `LD_LIBRARY_PATH` heredado.

## Layout instalado

```text
/usr/local/bin/xarlatan
/usr/local/bin/xarlatan-calibrate
/usr/local/bin/llama-server
/usr/local/lib/xarlatan/
/etc/ld.so.conf.d/xarlatan.conf
/etc/xarlatan/config.yaml
/etc/systemd/user/xarlatan.service
/var/lib/xarlatan/models
/var/backups/xarlatan
```

## Política de instalación

- El instalador requiere root salvo cuando se usa `DESTDIR` para tests.
- El usuario objetivo se obtiene de `SUDO_USER` o `XARLATAN_TARGET_USER`.
- El runtime no crea ni usa un usuario de sistema separado: `default` de ALSA pertenece a la sesión PipeWire del usuario de escritorio.
- La configuración queda `root:<grupo del usuario> 0640`; los modelos pertenecen al usuario objetivo con modos restrictivos.
- Las librerías CGo externas se copian a `/usr/local/lib/xarlatan` y se registran mediante `ldconfig`.
- No se sobrescribe una configuración existente.
- Antes de reemplazar binarios, librerías o unidades se guarda un snapshot único en `/var/backups/xarlatan`.
- La instalación no habilita ni inicia el servicio automáticamente.
- `uninstall` preserva config y modelos salvo `PURGE=1`.
- La unidad legacy `/etc/systemd/system/xarlatan.service` se deshabilita y elimina durante la migración.

## Servicio

- Unidad systemd de usuario instalada globalmente en `/etc/systemd/user/xarlatan.service`.
- Se inicia con `systemctl --user`, dentro de la misma sesión que PipeWire y WirePlumber.
- `NoNewPrivileges=true`, capabilities vacías, `ProtectSystem=strict` y `ProtectHome=read-only`.
- `ReadWritePaths=/var/lib/xarlatan`; `/var/backups/xarlatan` permanece root-only.
- `PrivateTmp=true`, protecciones de kernel/control groups y umask restrictiva.
- Ejecuta con `--no-tools` por defecto.

## Tests RED

- `scripts/tests/wi08_contract_test.sh` valida Makefile, unidad de usuario, librerías, config, idempotencia, uninstall y rollback en `DESTDIR`.
- CI ejecuta ShellCheck, contratos y `systemd-analyze verify`.
- El preflight ejecuta versión y `ldd` sin `LD_LIBRARY_PATH`.

## Gates

```bash
make clean build VERSION=v0.4.0-rc.test
./bin/assistant -version
shellcheck scripts/*.sh scripts/tests/wi08_contract_test.sh
bash scripts/tests/wi08_contract_test.sh
systemd-analyze verify packaging/systemd/xarlatan.service
```

## Fuera de alcance

- habilitar o iniciar el servicio sin acción explícita;
- cancelación acústica de eco real;
- wake word y streaming;
- smoke de hardware/modelos reales, que pertenece a WI-09.

## Rollback

`scripts/rollback.sh` restaura el snapshot root-only creado por la última instalación, actualiza el cache del linker y recarga las unidades systemd. Config y modelos del operador se preservan.