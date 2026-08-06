# WI-09 — Evidencia de release candidate

## Estado

**RC y hotfixes preparados / repetición de aceptación física pendiente.**

REL-003 no se marca aprobado hasta adjuntar un reporte `PASS` generado por `scripts/beta_acceptance.sh` en `dakota-fedora`.

## DEFINE

- Issue: #12.
- RC inicial: PR #31, merge `54734affb99a48f2c3b7c3d32ce64740849d753d`.
- Hotfix de descarga: PR #33, merge `d0ae37212bf9e01c11476a54bcdf0da42d081c8f`.
- Hotfix de versión/permisos: PR #34.
- Especificación: `docs/refactor/WI-09_SPEC.md`.
- Runbook: `docs/BETA_RUNBOOK.md`.
- Changelog: `CHANGELOG.md`.
- Requisitos: REL-001..REL-009.

## Contratos

- `scripts/tests/wi09_contract_test.sh` verifica assets, sintaxis, modelo público con checksum, versión normalizada, rebuild forzado, permisos instalados, dispositivo de audio coherente y preflight con fakes.
- `scripts/preflight.sh` valida host, comandos, versión, binarios, modelos, loopback, filesystem deshabilitado y enumeración ALSA.
- `scripts/beta_acceptance.sh` exige captura, playback, servicio y pipeline real y emite un reporte sin contenido conversacional.
- `scripts/package_release.sh` crea un tar ordenado, normalizado y verificable mediante SHA-256.
- `scripts/download_models.sh` descarga el LLM a un temporal, valida SHA-256 y lo mueve de forma atómica.
- `scripts/install.sh` normaliza directorios de modelos a `0750` y archivos a `0640`.

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

## Validación aislada acumulada

- instalación en `DESTDIR`, reinstalación, rollback, uninstall y purge: PASS;
- `systemd-analyze verify`: PASS;
- exposición systemd: 4,2/10, clasificación `OK`;
- preflight con fakes: PASS;
- paquete repetido con `SOURCE_DATE_EPOCH=0`: mismo SHA-256;
- descarga LLM fake con checksum correcto: PASS;
- archivo corrupto reemplazado: PASS;
- modelo personalizado sin checksum rechazado: PASS;
- package contiene descargador y runbook: PASS;
- `VERSION=v0.4.0-beta.1` genera `assistant v0.4.0-beta.1`: PASS;
- segundo `make build` reconstruye los binarios existentes: PASS;
- modelo fuente `0600` se instala como archivo `0640` bajo directorios `0750`: PASS;
- no se encontró un issue P0 independiente abierto.

GitHub Actions no registró runs automáticos para los commits escritos mediante el conector. No se declara un run verde inexistente.

## Primera ejecución física — 2026-08-06

- build beta: completado;
- STT/TTS: descargados;
- LLM Gemma: **FAIL**, HTTP 401;
- instalación: completada;
- acceptance: **FAIL**, script sin modo ejecutable;
- pipeline: no iniciado.

## Segunda ejecución física — 2026-08-06

- STT: presente;
- TTS: presente;
- Qwen2.5 0.5B: descargado y validado;
- instalación nativa: completada;
- versión instalada: **FAIL**, `assistant vv0.4.0-beta.1`;
- lectura directa como `dakota` de `/etc/xarlatan/config.yaml`: denegada;
- lectura directa como `dakota` del modelo instalado: denegada;
- pipeline: no iniciado por decisión de gate.

La denegación al usuario interactivo no implica por sí sola un defecto: configuración y modelos instalados deben ser legibles por `xarlatan`, no públicos. El defecto real era que el runbook usaba la configuración restringida para el foreground y que la instalación no normalizaba modos heredados.

## Hotfix de versión y permisos

1. `BUILD_VERSION` elimina un único prefijo `v` antes de inyectar la versión.
2. `make build` fuerza la reconstrucción de `assistant` y `calibrate`.
3. La instalación aplica `0750` a directorios de modelos y `0640` a archivos.
4. La configuración permanece `root:xarlatan 0640`.
5. El foreground usa `./config.yaml` y modelos locales legibles por `dakota`.
6. El smoke systemd usa la configuración/modelos instalados como usuario `xarlatan`.
7. Las verificaciones instaladas se ejecutan mediante `sudo` o `sudo -u xarlatan`.

## Repetición física requerida

```bash
cd ~/Documents/projects/xarlatan
git switch main
git pull --ff-only

make build VERSION=v0.4.0-beta.1
sudo bash ./scripts/install.sh

/usr/local/bin/xarlatan -version
sudo -u xarlatan test -r /etc/xarlatan/config.yaml
sudo -u xarlatan test -r \
  /var/lib/xarlatan/models/llm/qwen2.5-0.5b-instruct-q4_k_m.gguf

EXPECTED_VERSION=v0.4.0-beta.1 \
AUDIO_DEVICE=default \
bash ./scripts/beta_acceptance.sh ./config.yaml
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
