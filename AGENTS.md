# AGENTS.md

Guía operativa para agentes y colaboradores de Xarlatan.

## Contexto

Xarlatan es un asistente de voz local en Go para Linux. El pipeline actual es:

```text
ALSA capture -> VAD -> STT -> LLM/tools -> memory -> TTS -> ALSA playback
```

El refactor se ejecuta con SDD y TDD estricto. El plan vinculante está en `docs/refactor/` y el baseline reproducido en `docs/baseline/`.

## Regla principal

No escribir código funcional antes de:

1. identificar el Requirement ID;
2. leer su work item;
3. confirmar criterios de aceptación;
4. escribir una prueba que falle por la razón correcta.

## Flujo obligatorio

- `/spec`: problema, IDs, alcance, aceptación, errores y riesgos.
- `/plan`: diseño mínimo, archivos, tests RED, trade-offs, migración y rollback.
- `/build`: RED -> GREEN -> REFACTOR sin cambios ajenos al slice.
- `/test`: suite focalizada, suite completa y gates de `docs/refactor/QUALITY_GATES.md`.
- `/review`: trazabilidad, evidencia, riesgos residuales y rollback.
- `/ship`: instalación, smoke y rollback validados.

## Reglas de código

- Go 1.21 hasta que un spec apruebe el upgrade.
- Usar `gofmt`.
- Errores envueltos con `%w`; no usar `panic` para fallos operativos.
- Propagar `context.Context` a red, subprocessos y operaciones largas.
- No crear goroutines sin ownership, cancelación y test de shutdown.
- No introducir interfaces salvo en bordes sustituibles en tests.
- Mantener `cmd/assistant` como composition root.
- El objetivo de WI-04 es un único runtime en `internal/orchestrator`.
- No registrar tools fuera de `ToolPolicy` ni habilitar mutaciones por defecto.
- No promover contenido de usuario/tool a role `system`.
- No registrar secretos, audio, prompts completos ni contenido de archivos por defecto.

## Reglas de seguridad objetivo

- `fs_root` vacío nunca debe significar acceso irrestricto.
- Validar paths después de resolver symlinks.
- La raíz del sandbox no puede borrarse.
- Bloquear loopback, private y link-local en requests web y redirects.
- Los servicios instalados no deben ejecutarse como root.
- Todo subprocesso debe detenerse con cancelación o shutdown.

Estas reglas describen el estado objetivo. El baseline importado todavía contiene riesgos documentados en `docs/baseline/VALIDATION.md`.

## Build y pruebas

Hay 19 archivos de pruebas en el baseline.

```bash
gofmt -l .
go vet ./...
go test -count=1 ./...
go test -coverprofile=coverage.out ./...
go build ./cmd/assistant ./cmd/calibrate
```

Prueba focalizada:

```bash
go test -count=1 ./internal/<package> -run '^TestName$'
```

Lifecycle o concurrencia:

```bash
go test -race -count=1 ./internal/<package>
go test -count=50 ./internal/<package>
```

Targets disponibles en `Makefile`: `build`, `deps`, `all`, `models`, `llama`, `clean` y `clean-all`. Los targets `dev-*` no son operativos hasta incorporar o eliminar la dependencia de `compose.yml` en WI-08.

## PRs

- Una rama y un slice por PR.
- Usar `.github/pull_request_template.md`.
- Incluir evidencia RED y GREEN.
- Actualizar `docs/refactor/TRACEABILITY.md`.
- Declarar desviaciones, riesgos y rollback.

## Prohibiciones

- Reescritura completa.
- Activar mutaciones para facilitar pruebas.
- Deshabilitar tests para hacer pasar CI.
- Ocultar fallos con retries indefinidos o sleeps arbitrarios.
- Mantener dos orquestadores activos después de WI-04.
- Introducir dependencias sin justificar necesidad, licencia y superficie de seguridad.
- Marcar un requisito completo sin evidencia.
