# WI-09 — Evidencia de release candidate

## Estado

**RC automatizable listo / aceptación física pendiente.**

La preparación de `v0.4.0-beta.1` puede validarse en CI y en un entorno descartable. REL-003 no se marca aprobado hasta adjuntar un reporte `PASS` generado por `scripts/beta_acceptance.sh` en `dakota-fedora`.

## DEFINE

- Issue: #12.
- PR: #31.
- Especificación: `docs/refactor/WI-09_SPEC.md`.
- Runbook: `docs/BETA_RUNBOOK.md`.
- Changelog: `CHANGELOG.md`.
- Requisitos: REL-001..REL-008.

## Contratos

- `scripts/tests/wi09_contract_test.sh` verifica assets, sintaxis, workflow, target de release, política de privacidad y preflight con fakes.
- `scripts/preflight.sh` valida host, comandos, versión, binarios, modelos, loopback, filesystem deshabilitado y enumeración ALSA.
- `scripts/beta_acceptance.sh` prueba captura, playback, servicio y pipeline real y emite un reporte sin contenido conversacional.
- `scripts/package_release.sh` crea un tar ordenado, normalizado y verificable mediante SHA-256.

## Gates automatizados previstos

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

## Evidencia disponible antes de CI

- los nuevos scripts pasan `bash -n`;
- los nuevos scripts pasan ShellCheck en un entorno aislado;
- los nombres de paquetes Fedora `alsa-utils`, `alsa-lib-devel` y `ShellCheck` fueron verificados contra el catálogo de Fedora;
- WI-08 ya demostró instalación repetida, rollback, uninstall, purge y exposición systemd 4.2/10;
- no se registraron runs de Actions para PR #30; esa limitación quedó documentada y no se reutiliza como evidencia verde.

## Auditoría P0

No se acepta el RC si existe un issue abierto que describa:

- escape de filesystem sandbox;
- bypass SSRF;
- servicio ejecutado como root;
- pérdida o sobrescritura de configuración/modelos;
- proceso administrado huérfano;
- tool mutable habilitada por defecto.

La auditoría final se registra en el PR antes del merge.

## Aceptación física

Comando objetivo:

```bash
EXPECTED_VERSION=v0.4.0-beta.1 \
XARLATAN_BIN=./bin/assistant \
LLAMA_SERVER_BIN=./bin/llama-server \
AUDIO_DEVICE=default \
./scripts/beta_acceptance.sh ./config.yaml
```

Evidencia requerida:

```text
beta-acceptance-<timestamp>.md
Final result: PASS
Host: dakota-fedora
```

## Riesgos residuales

- el audio del servicio puede diferir del audio foreground por PipeWire/ALSA;
- el primer beta usa captura por turno y no implementa wake word;
- Gemma 3 270M prioriza un smoke ligero sobre calidad;
- la aceptación interactiva no puede ejecutarse honestamente desde GitHub Actions.

## Rollback

```bash
sudo systemctl disable --now xarlatan 2>/dev/null || true
sudo make rollback
sudo systemctl daemon-reload
```
