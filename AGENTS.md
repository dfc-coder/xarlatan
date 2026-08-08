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

## Regla de formato Go — hard gate

`gofmt` no es un gate de descubrimiento tardío. Es una precondición para crear
un commit válido.

- Todo archivo `.go` creado o modificado debe pasar por `gofmt -w` antes del
  commit.
- Antes de avanzar de DEVELOPMENT a TEST debe pasar `make format-check`.
- El hook versionado `.githooks/pre-commit` formatea automáticamente todos los
  `.go` staged y vuelve a agregarlos al commit.
- Un RED de TDD **no cuenta como RED válido** si CI falla primero por formato,
  sintaxis de fixture, typo o infraestructura. El primer fallo debe ser el
  contrato funcional esperado.
- Un agente que escriba Go mediante API/connector debe producir contenido ya
  compatible con `gofmt`; no debe usar CI como formatter.
- Nunca se baja, elimina o salta el gate de formato para hacer avanzar una
  entrega.

Preparación de un checkout nuevo:

```bash
make dev-setup
```

Formateo manual y verificación:

```bash
make fmt
make format-check
```

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
make format-check
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

## Git delivery

Para una entrega/beta normal:

```text
REQUIREMENT -> DEVELOPMENT -> TEST -> SHIP -> DELETE BRANCH
```

- Una única rama efímera por entrega y un único PR.
- No crear ramas por cada sub-WI de la misma entrega.
- Los commits internos pueden representar SPEC, RED, GREEN, REFACTOR y TEST.
- Una rama adicional sólo se justifica para un hotfix independiente posterior.
- Después del merge, eliminar la rama de entrega.

## PRs

- Usar `.github/pull_request_template.md`.
- Incluir evidencia RED y GREEN.
- Actualizar `docs/refactor/TRACEABILITY.md` cuando corresponda.
- Declarar desviaciones, riesgos y rollback.
- No marcar ready mientras `make format-check` no sea verde.

## Prohibiciones

- Reescritura completa.
- Activar mutaciones para facilitar pruebas.
- Deshabilitar tests para hacer pasar CI.
- Ocultar fallos con retries indefinidos o sleeps arbitrarios.
- Mantener dos orquestadores activos después de WI-04.
- Introducir dependencias sin justificar necesidad, licencia y superficie de seguridad.
- Marcar un requisito completo sin evidencia.
- Considerar válido un RED cuya primera falla sea `gofmt`, sintaxis o fixture.
