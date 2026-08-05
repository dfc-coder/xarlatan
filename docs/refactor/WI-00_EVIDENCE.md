# Evidencia WI-00 — baseline importado

Tracking: issue #3.

## Alcance entregado

| Scope | Estado | Evidencia |
|---|---|---|
| Snapshot importado | Implementado | Fuente, tests, scripts y configuración versionados en `agent/wi-00-baseline` |
| Module path | Implementado | `github.com/dfc-coder/xarlatan` en `go.mod` e imports internos |
| Tests existentes | Congelados | 19 archivos `*_test.go`; resultados reproducidos en `docs/baseline/VALIDATION.md` |
| AUD-005 | Baseline congelado | Suites existentes de VAD, audio y pre-roll preservadas sin refactor funcional |
| Clasificación de artefactos | Implementado | `docs/baseline/REPOSITORY_LAYOUT.md` y `.gitignore` |
| CI inicial | Implementado | `.github/workflows/ci.yml` ejecuta formato, vet, tests, cobertura y build |
| Riesgos conocidos | Documentados | `docs/baseline/VALIDATION.md`; asignados a WI-01 hasta WI-08 |

## Gates locales

- `gofmt -l cmd internal`: sin salida.
- Inventario: 19 archivos `*_test.go`.
- Referencias al módulo anterior: ninguna.
- Suites puras reproducidas:
  - `internal/vad`: 90.1%.
  - `internal/orchestrator`: 86.3%.
  - `internal/console`: 100.0%.
  - `internal/tools`: 5.1%.

La suite completa, `go vet` y el build quedaron bloqueados localmente porque el entorno no tenía DNS para descargar `gopkg.in/yaml.v3` y `sherpa-onnx-go`. El workflow de CI debe confirmar esos gates con acceso a dependencias.

## Desviaciones

- `compose.yml` no estaba presente en el snapshot. Los targets `dev-*` se conservaron y se marcaron como no operativos; su resolución pertenece a WI-08.
- No se corrigieron vulnerabilidades ni defectos funcionales para evitar mezclar el baseline con WI-01 y siguientes.

## Rollback

Revertir el commit de WI-00 o eliminar la rama antes de fusionarla. No hay migraciones de datos ni cambios persistentes de runtime.
