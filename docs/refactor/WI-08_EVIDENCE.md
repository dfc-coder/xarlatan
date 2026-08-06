# WI-08 — Evidencia

## Objetivo

Corregir build, instalación y servicio systemd para obtener una instalación coherente, no root, idempotente y reversible.

## DEFINE

- Issue: #11.
- Especificación: `docs/refactor/WI-08_SPEC.md`.
- Uso: `docs/refactor/WI-08_USAGE.md`.
- Requisitos: OPS-001..OPS-012.

## RED

El contrato inicial se agregó en `scripts/tests/wi08_contract_test.sh` antes de crear:

- `packaging/config.yaml`;
- `packaging/systemd/xarlatan.service`;
- `scripts/uninstall.sh`;
- `scripts/rollback.sh`;
- los targets y variables de build requeridos.

El fallo contractual esperado era la ausencia de esos archivos y de los contratos `all: deps build`, `install: all`, versión inyectable y servicio no root.

## Implementación

### Build

- `make all` depende de `llama-server`, `assistant` y `calibrate`;
- `make install` depende de `all`;
- la versión usa `var buildVersion = "dev"` y `-X main.buildVersion=$(VERSION)`;
- se eliminaron los targets compose que apuntaban a un archivo inexistente;
- `--reset` se eliminó mientras la memoria sea volátil.

### Instalación

- paths instalados bajo `/usr/local`, `/etc/xarlatan` y `/var/lib/xarlatan`;
- configuración existente no se sobrescribe;
- modelos presentes se copian sin descargar durante install;
- usuario/grupo `xarlatan` sin login;
- la instalación no habilita ni inicia el servicio;
- segunda instalación mantiene el mismo estado;
- uninstall preserva config/modelos y `PURGE=1` elimina todo.

### Rollback

- snapshot en `/var/backups/xarlatan`;
- ownership root-only y modo 0700;
- el servicio no puede escribir el snapshot;
- rollback de actualización restaura binarios/unidad previos;
- rollback de primera instalación elimina binarios/unidad y preserva config/modelos.

### systemd

- `User=xarlatan`, `Group=xarlatan`, `SupplementaryGroups=audio`;
- capabilities vacías y `NoNewPrivileges=true`;
- `ProtectSystem=strict`, `ProtectHome=true`, protecciones de kernel/control groups;
- `ReadWritePaths=/var/lib/xarlatan`;
- `DevicePolicy=closed` y `DeviceAllow=char-alsa rw`;
- tools deshabilitadas en `ExecStart`;
- logs en journald.

## Verificación local del contrato operativo

Ejecutado sobre un árbol descartable reconstruido con los archivos del PR:

```text
bash -n: success
instalación DESTDIR: success
segunda instalación sin drift: success
rollback de actualización: success
uninstall preservando config: success
purge: success
rollback de primera instalación: success
systemd-analyze verify: success con binario de prueba instalado
systemd-analyze security --offline=yes: exposición 4.2, clasificación OK
```

## Gates CI

```bash
gofmt -l .
go vet ./...
go test -count=1 -coverprofile=coverage.out ./...
go test -count=20 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
go test -race -count=1 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
make clean build VERSION=0.4.0-ci
shellcheck scripts/install.sh scripts/uninstall.sh scripts/rollback.sh scripts/tests/wi08_contract_test.sh
bash scripts/tests/wi08_contract_test.sh
systemd-analyze verify packaging/systemd/xarlatan.service
systemd-analyze security --offline=yes --threshold=5 packaging/systemd/xarlatan.service
```

Los IDs finales de CI, artifact y digest se completan después del último review.

## Riesgo residual

- el servicio del sistema no hereda la sesión PipeWire del usuario; el dispositivo ALSA debe probarse y configurarse explícitamente;
- el instalador no descarga modelos;
- el smoke con hardware/modelos reales pertenece a WI-09.

## Rollback

Ejecutar `sudo make rollback`. La configuración y los modelos se preservan.