# WI-04 — Evidencia de AgentRuntime único

Tracking: issue #7, PR #26.

## DEFINE

`WI-04_SPEC.md` define AGT-001..009, invariantes, contrato de `AgentRuntime`, errores terminales, errores recuperables de tools, alcance y rollback.

El problema inicial era una arquitectura duplicada:

- `cmd/assistant/main.go` contenía el loop LLM/tools real y solo permitía una ronda;
- `internal/orchestrator` contenía un grafo de nodos heurísticos y respuestas placeholder que no participaba del runtime productivo.

## PLAN

El trabajo se ejecutó en cuatro slices:

1. contratos RED para respuesta directa, múltiples rondas, límites, orden, errores, cancelación y trace;
2. `AgentRuntime` y resultados tipados de `tools.Executor`;
3. delegación desde `cmd/assistant`, límite CLI y eliminación del grafo placeholder;
4. repetición, race, suite completa, observabilidad, documentación y review.

## BUILD — RED

Head RED: `765d292c14d878591dde0e2742dcc2ccd4a1c90a`.

`internal/orchestrator/runtime_test.go` fue agregado antes de producción y referenciaba deliberadamente contratos inexistentes:

- `AgentRuntime`;
- `RuntimeConfig`;
- `Request` y `Result`;
- `RoundTrace` y `RoundObserver`;
- errores y stop reasons tipados;
- resultados detallados del executor.

El RED estableció que el código anterior no podía satisfacer múltiples rondas, límite, IDs, cancelación ni una traza por llamada al modelo.

## BUILD — GREEN

### Runtime único

`internal/orchestrator.AgentRuntime` es el único dueño del loop:

1. llama al generador con el historial y el input inicial;
2. termina inmediatamente ante una respuesta sin tools;
3. detecta llamadas en el último mensaje assistant;
4. valida el budget de rondas antes de ejecutar;
5. ejecuta tools secuencialmente;
6. agrega mensajes `role=tool` conservando `tool_call_id`;
7. vuelve al modelo con input vacío;
8. repite hasta respuesta directa, error terminal, cancelación o límite.

`cmd/assistant/main.go` construye el runtime y realiza exactamente una llamada a `agent.Run` por turno. Ya no inspecciona `ToolCalls`, no llama directamente a `llmClient.Generate` y no ejecuta tools.

### Límite

El flag `-max-tool-rounds` tiene default `4` y se rechaza si es menor o igual a cero antes de construir subsistemas pesados.

El budget cuenta solo rondas que ejecutaron tools. Si el modelo solicita otra ronda después del límite, el runtime devuelve `round_limit` y no ejecuta esas llamadas.

### Tool execution tipada

`tools.Executor.RunAllDetailed` conserva orden y devuelve `ExecutionRecord` con:

- llamada original;
- mensaje protocolar;
- log breve;
- duración;
- código estable.

Códigos:

- `unknown_tool`;
- `denied_tool`;
- `tool_error`;
- `cancelled`.

Errores desconocidos, denegados o recuperables regresan al modelo como mensajes `tool`; no abortan automáticamente el turno.

### Errores terminales

`RuntimeError` distingue:

- `invalid_runtime`;
- `model_error`;
- `round_limit`;
- `cancelled`;
- `deadline_exceeded`.

Cancelación y deadline prevalecen sobre un error secundario retornado por modelo o tool.

### Observabilidad

Cada llamada al modelo genera un `RoundTrace` con:

- número de round;
- duración del modelo y total;
- tools ordenadas con call ID, nombre, resultado, error code y duración;
- stop reason;
- error terminal, cuando corresponde.

`SlogObserver` no registra contenido de mensajes ni argumentos. `MetricsObserver` cuenta rounds, tool rounds, calls, fallos y stop reasons y devuelve snapshots defensivos.

### Limpieza

Se eliminaron:

- generic node orchestrator;
- `State` y context projections;
- router y planner heurísticos;
- response composer placeholder;
- tool/finalizer/memory nodes;
- tests asociados a esa arquitectura desconectada.

El inventario CI dejó de contar archivos de test de forma artificial y pasó a exigir contratos semánticos críticos, incluido `internal/orchestrator/runtime_test.go`.

## Matriz AGT

| Requisito | Evidencia principal |
|---|---|
| AGT-001 | `TestApplicationUsesOrchestratorOnly` |
| AGT-002 | `TestAgentReturnsDirectReply` |
| AGT-003 | `TestAgentExecutesOneToolRound`, `TestAgentExecutesMultipleToolRounds` |
| AGT-004 | `TestAgentStopsAtConfiguredRoundLimit`, validación del constructor y flag |
| AGT-005 | `TestAgentPreservesToolCallOrdering` |
| AGT-006 | `TestAgentReturnsUnknownToolError` |
| AGT-007 | `TestAgentContinuesAfterRecoverableToolError`, `TestAgentReturnsDeniedToolErrorToModel` |
| AGT-008 | `TestAgentCancelsDuringToolExecution`, `TestAgentHonorsDeadlineDuringModelGeneration` |
| AGT-009 | `TestAgentEmitsTraceForEveryRound`, tests de `MetricsObserver` |

Contratos adicionales:

- historial de entrada inmutable;
- configuración inválida rechazada;
- error del modelo envuelto y trazado;
- snapshots de métricas defensivos.

## VERIFY

Head funcional verificado: `42d053c45bd0a84769c8ecfbf77e00fb742010e8`.

Workflow CI: run `31072324754`.

- baseline inventory: success;
- `gofmt -l .`: success;
- `go vet ./...`: success;
- `go test -count=1 -coverprofile=coverage.out ./...`: success;
- `go test -count=20 ./internal/orchestrator ./internal/tools`: success;
- `go test -race -count=1 ./internal/orchestrator ./internal/tools`: success;
- `go build ./cmd/assistant ./cmd/calibrate`: success;
- artefacto `coverage`: `sha256:888c692480c055896678301c6487ee55913b6604a8f83e0801319eb55d8c6c25`.

## REVIEW

- no existe otro loop LLM/tools en `cmd/assistant`;
- el runtime no ejecuta la ronda que excede el límite;
- las tools permanecen secuenciales y determinísticas;
- los IDs se conservan en mensajes y traces;
- los errores recuperables vuelven al modelo;
- cancelación evita iniciar otra tool o ronda;
- no se agregaron dependencias externas;
- no se modificó el lifecycle de llama-server, memoria ni pipeline de voz fuera del punto de integración definido;
- no permanecen fuentes o tests del grafo placeholder.

## Riesgo residual

- memoria y compactación continúan coordinadas desde `main.go` hasta WI-06;
- audio, STT y TTS continúan en el loop de aplicación hasta WI-07;
- el runtime depende del `context.Context` recibido y todavía no impone un timeout propio por turno;
- no existe aún budget de tokens o costo;
- las tools largas deben respetar cancelación cooperativa.

## Rollback

Revertir el squash merge de PR #26. El rollback restaura el loop de una sola ronda en `cmd/assistant` y el grafo placeholder anterior. No existe migración de datos. El flag `-max-tool-rounds` desaparece al volver a la versión previa.
