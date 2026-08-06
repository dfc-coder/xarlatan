# WI-09 — Evidencia de release candidate

## Estado

**RC integrado / aceptación física fallida antes del pipeline / hotfix en curso.**

REL-003 no se marca aprobado hasta adjuntar un reporte `PASS` generado por `scripts/beta_acceptance.sh` en `dakota-fedora`.

## DEFINE

- Issue: #12.
- RC inicial: PR #31, merge `54734affb99a48f2c3b7c3d32ce64740849d753d`.
- Hotfix de primera ejecución: PR #33.
- Especificación: `docs/refactor/WI-09_SPEC.md`.
- Runbook: `docs/BETA_RUNBOOK.md`.
- Changelog: `CHANGELOG.md`.
- Requisitos: REL-001..REL-008.

## Contratos

- `scripts/tests/wi09_contract_test.sh` verifica assets, sintaxis, workflow, target de release, modelo público con checksum, política de privacidad, dispositivo de audio coherente y preflight con fakes.
- `scripts/preflight.sh` valida host, comandos, versión, binarios, modelos, loopback, filesystem deshabilitado y enumeración ALSA.
- `scripts/beta_acceptance.sh` exige captura, playback, servicio y pipeline real y emite un reporte sin contenido conversacional.
- `scripts/package_release.sh` crea un tar ordenado, normalizado y verificable mediante SHA-256.
- `scripts/download_models.sh` descarga el LLM a un temporal, valida SHA-256 y lo mueve de forma atómica.

## Gates automatizados

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

`systemd-analyze --threshold` usa porcentaje. El valor `50` corresponde a 5,0/10.

## Validación aislada previa

- instalación en `DESTDIR`, reinstalación, rollback, uninstall y purge: PASS;
- `systemd-analyze verify`: PASS;
- exposición systemd: 4,2/10, clasificación `OK`;
- preflight con fakes: PASS;
- paquete repetido con `SOURCE_DATE_EPOCH=0`: mismo SHA-256;
- no se encontró un issue P0 independiente abierto.

GitHub Actions no registró runs automáticos para los commits escritos mediante el conector. No se declara un run verde inexistente.

## Primera ejecución física — 2026-08-06

Host observado: `dakota-fedora`.

Resultados:

- build de `assistant v0.4.0-beta.1`: completado;
- STT: descargado;
- TTS: descargado;
- LLM: **FAIL**, HTTP 401 desde `google/gemma-3-270m-it-GGUF`;
- instalación nativa: completada;
- lanzamiento de aceptación: **FAIL**, `Permission denied` al ejecutar `./scripts/beta_acceptance.sh`;
- pipeline de voz: no iniciado;
- reporte de aceptación: no generado.

La prueba no valida ni invalida todavía ALSA, STT, llama-server, TTS o systemd en ejecución. Falló antes de alcanzar esos gates.

## Hotfix de primera ejecución

1. Se reemplaza el repositorio restringido por `Qwen/Qwen2.5-0.5B-Instruct-GGUF`.
2. El archivo por defecto pasa a ser `qwen2.5-0.5b-instruct-q4_k_m.gguf`.
3. Se valida SHA-256 `74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db`.
4. Los archivos vacíos, parciales o corruptos no se consideran instalados.
5. `make models` deja de usar directorios como targets artificiales.
6. El runbook usa `bash ./scripts/...` para no depender del bit ejecutable del checkout.
7. La configuración local y la instalada apuntan al nuevo modelo.

## Repetición física requerida

```bash
cd ~/Documents/projects/xarlatan
git switch main
git pull --ff-only
rm -f models/llm/gemma-3-270m-it-Q4_K_M.gguf
make models
sudo make install
sudo sed -i \
  's#gemma-3-270m-it-Q4_K_M.gguf#qwen2.5-0.5b-instruct-q4_k_m.gguf#' \
  /etc/xarlatan/config.yaml
EXPECTED_VERSION=v0.4.0-beta.1 \
AUDIO_DEVICE=default \
bash ./scripts/beta_acceptance.sh /etc/xarlatan/config.yaml
```

## Evidencia requerida para cierre

```text
beta-acceptance-<timestamp>.md
Host: dakota-fedora
Final result: PASS
```

## Riesgos residuales

- el audio del servicio puede diferir del audio foreground por PipeWire/ALSA;
- la beta usa captura por turno y no implementa wake word;
- Qwen2.5 0.5B prioriza un smoke ligero sobre calidad;
- la aceptación interactiva no puede ejecutarse honestamente desde GitHub Actions.

## Rollback

```bash
sudo systemctl disable --now xarlatan 2>/dev/null || true
sudo make rollback
sudo systemctl daemon-reload
```
