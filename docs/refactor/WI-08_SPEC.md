# WI-08 — Build, instalación y systemd

## Objetivo

Entregar una instalación Linux reproducible, idempotente, no root y reversible para Xarlatan, sin targets inexistentes ni configuración insegura.

## Requisitos

- OPS-001: `make all` produce `assistant`, `calibrate` y `llama-server`.
- OPS-002: `make install` depende de todos los artefactos requeridos.
- OPS-003: no existen targets compose sin archivo compose real.
- OPS-004: la versión se inyecta en una variable build-time y `-version` la muestra.
- OPS-005: el servicio usa usuario y grupo dedicados `xarlatan`.
- OPS-006: la unidad aplica hardening systemd verificable.
- OPS-007: solo `/var/lib/xarlatan` es escribible para el servicio.
- OPS-008: la configuración instalada usa paths absolutos, filesystem deshabilitado y sin tools por defecto.
- OPS-009: instalar dos veces produce el mismo estado.
- OPS-010: existe uninstall y rollback del último despliegue.
- OPS-011: logs van a journald y no contienen secretos por configuración.
- OPS-012: se elimina `--reset` mientras la memoria no sea persistente.

## Layout instalado

```text
/usr/local/bin/xarlatan
/usr/local/bin/xarlatan-calibrate
/usr/local/bin/llama-server
/etc/xarlatan/config.yaml
/etc/systemd/system/xarlatan.service
/var/lib/xarlatan/models
/var/backups/xarlatan
```

## Política de instalación

- El instalador requiere root salvo cuando se usa `DESTDIR` para tests.
- Crea usuario/grupo de sistema sin shell ni home.
- No sobrescribe una configuración existente.
- Antes de reemplazar binarios o unidad guarda un snapshot único en `/var/backups/xarlatan`, fuera de los paths escribibles por el servicio.
- La instalación no habilita ni inicia el servicio automáticamente.
- `uninstall` preserva config y modelos salvo `PURGE=1`.

## Servicio

- `User=xarlatan`, `Group=xarlatan`, `SupplementaryGroups=audio`.
- `NoNewPrivileges=true`, capabilities vacías, `ProtectSystem=strict`, `ProtectHome=true`.
- `ReadWritePaths=/var/lib/xarlatan`; `/var/backups/xarlatan` permanece root-only.
- `PrivateTmp=true`, protección de kernel/control groups y umask restrictiva.
- Ejecuta con `--no-tools` por defecto; el operador puede habilitarlas explícitamente después de revisar policy/config.

## Tests RED

- `scripts/tests/wi08_contract_test.sh` valida Makefile, versionado, unidad, config, idempotencia y rollback en `DESTDIR`.
- CI ejecuta shellcheck, test contractual y `systemd-analyze verify`.

## Gates

```bash
make clean build VERSION=0.4.0-rc.test
./bin/assistant -version
shellcheck scripts/install.sh scripts/uninstall.sh scripts/rollback.sh scripts/tests/wi08_contract_test.sh
bash scripts/tests/wi08_contract_test.sh
systemd-analyze verify packaging/systemd/xarlatan.service
```

## Fuera de alcance

- descarga automática de modelos durante install;
- habilitar o iniciar el servicio sin acción explícita;
- smoke de hardware/modelos reales, que pertenece a WI-09.

## Rollback

`scripts/rollback.sh` restaura el snapshot root-only creado por la última instalación. En una primera instalación elimina los binarios y la unidad que no existían antes. Config y modelos del operador se preservan.