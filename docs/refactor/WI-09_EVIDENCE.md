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

- `scripts/tests/wi09_contract_test.sh` verifica assets, sintaxis, workflow, target de release, política de privacidad, dispositivo de audio coherente y preflight con fakes.
- `scripts/preflight.sh` valida host, comandos, versión, binarios, modelos, loopback, filesystem deshabilitado y enumeración ALSA.
- `scripts/beta_acceptance.sh` exige captura, playback, servicio y pipeline real y emite un reporte sin contenido conversacional.
- `scripts/package_release.sh` crea un tar ordenado, normalizado y verificable mediante SHA-256.

## Gates automatizados

```bash
gofmt -l .
go vet ./...
go test -count=1 -coverprofile=coverage.out ./...
go test -count=20 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
go test -race -count=1 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
shellcheck scripts/install.sh scripts/uninstall.sh scripts/rollback.sh scripts/preflight.sh scripts/beta_acceptance.sh scripts/package_release.sh scripts/tests/wi08_contract_test.sh scripts/tests/wi09_contract_test.sh
bash scripts/tests/wi08_contract_test.sh
bash scripts/tests/wi09_contract_test.sh
make release-candidate VERSION=v0.4.0-beta.1
sha256sum -c dist/SHA256SUMS
systemd-analyze verify packaging/systemd/xarlatan.service
systemd-analyze security --offline=yes --threshold=50 packaging/systemd/xarlatan.service
```

`systemd-analyze --threshold` usa porcentaje. El valor `50` corresponde a 5,0/10; `--threshold=5` era incorrecto y fue corregido durante la validación de WI-09.

## Validación ejecutada en entorno aislado

- `bash -n` de scripts operativos y de release: PASS;
- instalación en `DESTDIR`: PASS;
- segunda instalación sin drift de artefactos: PASS;
- rollback de actualización: PASS;
- uninstall preservando configuración: PASS;
- purge de configuración y datos: PASS;
- `systemd-analyze verify`: PASS;
- exposición systemd: 4,2/10, clasificación `OK`;
- gate equivalente `--threshold=50`: PASS;
- preflight con binarios, modelos, ALSA y configuración fake: PASS;
- creación repetida del paquete con `SOURCE_DATE_EPOCH=0`: mismo SHA-256;
- auditoría de issues abiertos: solo epic, WI-09 y work items de Fase 2; no aparece un issue P0 separado;
- PR #31 no cambia archivos Go: solo release, scripts, docs, Makefile y workflows.

La validación aislada no sustituye la suite Go sobre un checkout completo ni el hardware real. GitHub Actions no registró runs automáticos para los commits escritos por el conector, por lo que no se declara un run verde inexistente.

## Defectos encontrados y corregidos

1. CI construía `VERSION=0.4.0-ci` pero esperaba `assistant v0.4.0-ci`; se normalizó a `VERSION=v0.4.0-ci`.
2. La aceptación podía terminar en `PASS` con systemd ausente; ahora el servicio instalado es obligatorio por defecto.
3. El gate `systemd-analyze --threshold=5` interpretaba 5%, no 5,0/10; se cambió a `--threshold=50`.
4. La captura manual podía usar un dispositivo distinto al configurado para la aplicación; ahora esa discrepancia se rechaza.
5. ShellCheck estaba expandido a scripts legacy fuera del slice; ahora se limita explícitamente a los scripts WI-08/WI-09.
6. El workflow de release no repetía race, repetición ni systemd; ahora es autocontenido.

## Auditoría P0

No se acepta el RC si existe un issue abierto que describa:

- escape de filesystem sandbox;
- bypass SSRF;
- servicio ejecutado como root;
- pérdida o sobrescritura de configuración/modelos;
- proceso administrado huérfano;
- tool mutable habilitada por defecto.

La búsqueda de issues abiertos no encontró un issue P0 independiente con esas condiciones. Los work items abiertos pertenecen a WI-09 y Fase 2.

## Aceptación física

Después de construir e instalar:

```bash
EXPECTED_VERSION=v0.4.0-beta.1 \
AUDIO_DEVICE=default \
./scripts/beta_acceptance.sh /etc/xarlatan/config.yaml
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
