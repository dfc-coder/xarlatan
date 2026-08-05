# AGENTS.md

Guía operativa para agentes y colaboradores de Xarlatan.

## Contexto

Xarlatan es un asistente de voz local en Go para Linux. El pipeline previsto es:

```text
ALSA capture -> VAD -> STT -> AgentRuntime -> LLM/tools -> memory -> TTS -> ALSA playback
```

El refactor se ejecuta con SDD y TDD estricto. El plan vinculante está en `docs/refactor/`.

## Regla principal

No escribir código funcional antes de:

1. identificar el Requirement ID;
2. leer su work item;
3. confirmar criterios de aceptación;
4. escribir una prueba que falle por la razón correcta.

## Flujo obligatorio

### `/spec`

Definir:

- problema;
- Requirement IDs;
- alcance/no alcance;
- criterios de aceptación;
- errores y cancelación;
- riesgos.

### `/plan`

Definir:

- archivos y contratos afectados;
- diseño mínimo;
- tests RED;
- trade-offs;
- migración y rollback.

### `/build`

Aplicar RED -> GREEN -> REFACTOR. No mezclar mejoras no relacionadas.

### `/test`

Ejecutar suite focalizada, suite completa y gates aplicables de `QUALITY_GATES.md`.

### `/review`

Adjuntar trazabilidad, evidencia de tests, riesgos y rollback.

### `/ship`

Solo después de instalación, smoke y rollback validados.

## Reglas de código

- Go 1.21 hasta que un spec apruebe el upgrade.
- Usar `gofmt`; adoptar `gofumpt` solo de forma consistente en CI.
- Errores envueltos con `%w`.
- No usar `panic` para fallos operativos.
- Propagar `context.Context` a red, subprocessos y operaciones largas.
- No crear goroutines sin ownership, cancelación y test de shutdown.
- No introducir interfaces salvo en bordes que necesiten sustitución en tests.
- Mantener `cmd/assistant` como composition root; no alojar lógica del agente allí.
- Mantener un único runtime en `internal/orchestrator`.
- No registrar herramientas fuera de `ToolPolicy`.
- No habilitar herramientas mutables por defecto.
- No elevar contenido de usuario/tool a role `system`.
- No loguear secretos, audio, prompts completos ni contenido de archivos por defecto.

## Reglas de seguridad

- `fs_root` vacío nunca significa acceso irrestricto.
- Paths se validan después de resolver symlinks.
- La raíz del sandbox no puede borrarse.
- Requests web no pueden acceder a loopback, private o link-local.
- Redirects se revalidan.
- Servicios instalados no se ejecutan como root.
- Todo subprocesso debe detenerse con cancelación o shutdown.

## Tests

Comandos base:

```bash
gofmt -l .
go vet ./...
go test -count=1 ./...
go test -coverprofile=coverage.out ./...
go build ./cmd/assistant ./cmd/calibrate
```

Para una prueba focalizada:

```bash
go test -count=1 ./internal/<package> -run '^TestName$'
```

Para lifecycle o concurrencia:

```bash
go test -race -count=1 ./internal/<package>
go test -count=50 ./internal/<package>
```

## PRs

- Una rama por work item.
- Un PR no mezcla slices.
- El cuerpo usa `.github/pull_request_template.md`.
- Incluir comando y salida esperada del RED.
- Incluir comandos y resultado del GREEN.
- Actualizar `TRACEABILITY.md`.
- Declarar cualquier desviación del diseño.
- Declarar rollback.

## Prohibiciones durante el refactor

- Reescritura completa.
- Activar mutaciones para “probar más fácil”.
- Deshabilitar tests existentes para hacer pasar CI.
- Ocultar fallos con retries indefinidos o sleeps arbitrarios.
- Mantener dos orquestadores activos.
- Introducir dependencias sin justificar necesidad, licencia y superficie de seguridad.
- Marcar un requisito completo sin evidencia.
