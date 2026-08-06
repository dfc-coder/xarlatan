# WI-09 — Release candidate operativo y primera beta

## Objetivo

Cerrar la Fase 1 con un release candidate reproducible y una aceptación física ejecutable en Fedora, sin confundir validación automatizada con pruebas reales de micrófono, altavoz y modelos.

## Versión

La primera beta candidata usa `v0.4.0-beta.1`.

## Requisitos

- **REL-001:** suite completa, gates focalizados, repetición y race verdes sobre el commit candidato.
- **REL-002:** E2E del turno con fakes permanece verde y sin hardware/red/modelos reales.
- **REL-003:** existe un smoke reproducible de ALSA, sherpa-onnx, llama-server y pipeline completo; solo se marca aprobado con un reporte generado en la máquina objetivo.
- **REL-004:** el runbook cubre preflight, build, instalación, primera ejecución, servicio, fallos, recuperación y diagnóstico.
- **REL-005:** instalación, actualización, rollback, uninstall y purge permanecen verificables.
- **REL-006:** no existen issues P0 abiertos ni excepciones a sandbox, SSRF, servicio no root, pérdida de datos o procesos huérfanos.
- **REL-007:** el changelog enumera cambios incompatibles y límites conocidos.
- **REL-008:** el paquete de release se genera de forma determinista desde una versión/tag y publica SHA-256.
- **REL-009:** los modelos por defecto se descargan desde fuentes públicas o autenticadas explícitamente, a temporales y con checksum antes de instalarse.

## Estados de aceptación

### RC automatizado

```text
gofmt + vet + suite + coverage + repeat + race
build/version + shellcheck + install contracts
systemd verify/security + release package/checksum
model source + atomic download + checksum contract
```

### Beta aceptada en la máquina

Se alcanza únicamente cuando `bash scripts/beta_acceptance.sh` genera un reporte `PASS` que confirma:

1. preflight y modelos;
2. captura ALSA real;
3. reproducción ALSA real confirmada;
4. arranque y parada del servicio sin root ni crash inmediato;
5. interacción completa `voz -> STT -> LLM -> TTS -> audio`;
6. rollback disponible.

## Máquina objetivo inicial

- host: `dakota-fedora`;
- Linux x86_64 / Fedora;
- checkout: `~/Documents/projects/xarlatan`;
- primer smoke en foreground antes de habilitar systemd;
- LLM inicial: Qwen2.5 0.5B Instruct Q4_K_M, CPU-first;
- repositorio: `Qwen/Qwen2.5-0.5B-Instruct-GGUF`;
- SHA-256 fijado en `scripts/download_models.sh`;
- herramientas deshabilitadas durante aceptación.

## Entregables

- `scripts/download_models.sh` con descarga atómica y checksum;
- `scripts/preflight.sh`;
- `scripts/beta_acceptance.sh`;
- `scripts/package_release.sh`;
- `scripts/tests/wi09_contract_test.sh`;
- `docs/BETA_RUNBOOK.md`;
- `CHANGELOG.md`;
- `docs/refactor/WI-09_EVIDENCE.md`;
- workflow de release candidate;
- targets `make models`, `make release-candidate`, `make preflight` y `make beta-acceptance`.

## Gates

```bash
gofmt -l .
go vet ./...
go test -count=1 -coverprofile=coverage.out ./...
go test -count=20 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
go test -race -count=1 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
shellcheck scripts/install.sh scripts/uninstall.sh scripts/rollback.sh scripts/download_models.sh scripts/preflight.sh scripts/beta_acceptance.sh scripts/package_release.sh scripts/tests/wi08_contract_test.sh scripts/tests/wi09_contract_test.sh
bash scripts/tests/wi08_contract_test.sh
bash scripts/tests/wi09_contract_test.sh
make release-candidate VERSION=v0.4.0-beta.1
sha256sum -c dist/SHA256SUMS
systemd-analyze verify packaging/systemd/xarlatan.service
systemd-analyze security --offline=yes --threshold=50 packaging/systemd/xarlatan.service
```

`systemd-analyze --threshold` usa porcentaje; `50` representa una exposición máxima de 5,0/10.

## Política de cierre

El issue #12 solo se cierra después de adjuntar un reporte `PASS` ejecutado en `dakota-fedora`. Un fallo de descarga, permisos, audio, modelo o servicio mantiene WI-09 abierto y genera un hotfix reproducible.

## Rollback

```bash
sudo systemctl disable --now xarlatan 2>/dev/null || true
sudo make rollback
sudo systemctl daemon-reload
```
