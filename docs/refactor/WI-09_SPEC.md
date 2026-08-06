# WI-09 — Release candidate operativo y primera beta

## Objetivo

Cerrar la Fase 1 con un release candidate reproducible y una aceptación física ejecutable en la máquina Fedora objetivo, sin confundir validación automatizada con pruebas reales de micrófono, altavoz y modelos.

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

## Estados de aceptación

### RC automatizado

Se alcanza cuando CI o una ejecución equivalente confirma:

```text
gofmt + vet + suite + coverage + repeat + race
build/version + shellcheck + install contracts
systemd verify/security + release package/checksum
```

### Beta aceptada en la máquina

Se alcanza únicamente cuando `scripts/beta_acceptance.sh` genera un reporte `PASS` que confirma:

1. preflight y modelos;
2. captura ALSA real;
3. reproducción ALSA real confirmada por el operador;
4. arranque y parada del servicio sin root ni crash inmediato;
5. interacción foreground completa confirmada por el operador: voz -> STT -> LLM -> TTS -> audio;
6. rollback disponible.

El repositorio puede entregar el RC y habilitar la primera prueba, pero no puede afirmar REL-003 sin ejecutar ese reporte en el hardware objetivo.

## Máquina objetivo inicial

- host: `dakota-fedora`;
- Linux x86_64 / Fedora;
- checkout conocido: `~/Documents/projects/assistant`;
- ejecución histórica: `./bin/assistant -config config.yaml -log debug`;
- primer smoke recomendado en foreground antes de habilitar systemd;
- LLM inicial: Gemma 3 270M Q4_K_M, CPU-first;
- herramientas deshabilitadas durante aceptación.

## Entregables

- `scripts/preflight.sh`;
- `scripts/beta_acceptance.sh`;
- `scripts/package_release.sh`;
- `scripts/tests/wi09_contract_test.sh`;
- `docs/BETA_RUNBOOK.md`;
- `CHANGELOG.md`;
- `docs/refactor/WI-09_EVIDENCE.md`;
- workflow de release candidate;
- target `make release-candidate`.

## Gates

```bash
gofmt -l .
go vet ./...
go test -count=1 -coverprofile=coverage.out ./...
go test -count=20 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
go test -race -count=1 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
shellcheck scripts/*.sh scripts/tests/*.sh
bash scripts/tests/wi08_contract_test.sh
bash scripts/tests/wi09_contract_test.sh
make release-candidate VERSION=v0.4.0-beta.1
sha256sum -c dist/SHA256SUMS
systemd-analyze verify packaging/systemd/xarlatan.service
systemd-analyze security --offline=yes --threshold=5 packaging/systemd/xarlatan.service
```

## Política de cierre

WI-09 se integra para entregar el beta candidate y el harness físico. El issue #12 solo se cierra como completado después de adjuntar un reporte `PASS` de `scripts/beta_acceptance.sh` ejecutado en `dakota-fedora`. Hasta entonces el estado correcto es **RC listo / aceptación física pendiente**.

## Rollback

- Antes de habilitar el servicio: ejecutar desde el checkout anterior o eliminar el paquete candidato.
- Después de instalar: `sudo make rollback`.
- Si el servicio no inicia: `sudo systemctl disable --now xarlatan`, revisar `journalctl -u xarlatan`, corregir dispositivo/config o ejecutar rollback.
