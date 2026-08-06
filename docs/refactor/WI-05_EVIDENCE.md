# WI-05 — Evidencia de lifecycle de llama-server

## Estado

- Issue: #8
- Epic: #2
- Pull request: #27
- Base `main`: `7127583a7dca295208dad1c0f4c000cf96a52769`
- RED head: `654d8b471648e6dbf6097b5a52e3e0713e8e96ec`
- Functional GREEN head: `a2375d9138036f6ef206e0eefc09dcfd255bae06`
- Reviewed GREEN head: `12193f19d6af77e869a6342f7034ffa3ce3a1fab`
- Requirements: LLM-001..LLM-009

## DEFINE

El baseline mezclaba en `internal/llm.Client` dos responsabilidades:

1. ownership del subprocesso `llama-server`;
2. requests OpenAI-compatible y parsing SSE.

El constructor iniciaba el proceso implícitamente, esperaba readiness mediante polling fijo de 60 segundos, no detectaba una salida prematura y el cierre usaba `Kill` directamente. La construcción del request ignoraba errores de `json.Marshal` y `http.NewRequestWithContext`. El health polling tampoco cerraba bodies en todas las ramas.

La especificación de alcance, contratos, errores, invariantes y rollback se encuentra en `docs/refactor/WI-05_SPEC.md`.

## PLAN

1. Definir tests RED para proceso, health probe, external mode, cancelación, shutdown y requests inválidos.
2. Introducir un `ServerManager` con `ProcessFactory` y `HealthProbe` inyectables.
3. Convertir `Client` en un adapter HTTP sin lifecycle de procesos.
4. Integrar modo y timeouts en configuración estricta.
5. Trasladar ownership al composition root sin permitir que `os.Exit` omita defers.
6. Ejecutar suite completa, repetición, race, build, review y trazabilidad.

## RED

El head `654d8b471648e6dbf6097b5a52e3e0713e8e96ec` agregó contratos que referenciaban deliberadamente APIs todavía inexistentes.

- Baseline inventory: run `31075544059`, verde.
- CI: run `31075543999`, fallo esperado en Vet.
- Job: `92532647678`.
- Primer error contractual: `internal/llm/server_manager_test.go:67:10: undefined: Process`.

El fallo fue por ausencia del boundary de proceso requerido, no por una expectativa funcional existente ni por un error de formato.

## GREEN

### Separación de responsabilidades

`llm.Client` ahora:

- recibe `ClientConfig` y una base URL validada;
- realiza exclusivamente requests de inferencia;
- conserva el contrato `orchestrator.Generator`;
- no expone `Start`, `WaitReady`, `Wait`, `Stop` ni `Close`;
- no contiene `exec.Cmd` ni ownership de subprocessos;
- propaga errores de marshal y construcción de requests;
- limita el body leído para errores HTTP;
- mantiene el parsing SSE y tool calling existente.

`llm.ServerManager` ahora es el único owner del proceso managed y expone:

- `Start(context.Context)`;
- `WaitReady(context.Context)`;
- `Wait()`;
- `Stop(context.Context)`.

### Proceso administrado

El manager:

- crea el proceso mediante `ProcessFactory`;
- ejecuta `Process.Wait` exactamente una vez en una goroutine propia;
- almacena el resultado para readiness, `Wait` y shutdown;
- detecta salida antes de readiness;
- limpia y espera el proceso ante timeout o cancelación de startup;
- envía `os.Interrupt`, espera de forma acotada y usa `Kill` solo como fallback;
- espera nuevamente después de `Kill` para recolectar el subprocesso;
- hace `Stop` idempotente y seguro ante llamadas concurrentes mediante `sync.Once`.

### Modo external

En `external`:

- `Start` no invoca `ProcessFactory`;
- `WaitReady` comprueba el endpoint configurado;
- `Stop` no envía señales ni mata procesos;
- configuración no exige binary ni modelo locales.

### Health probe

`HTTPHealthProbe`:

- construye requests con contexto;
- trata únicamente HTTP 200 como ready;
- drena de forma acotada y cierra todos los bodies recibidos;
- propaga errores de URL, transporte, lectura y cierre.

### Errores tipados

Códigos implementados:

- `invalid_config`;
- `start_failed`;
- `early_exit`;
- `startup_timeout`;
- `cancelled`;
- `shutdown_failed`.

`LifecycleError` preserva causas y `IsLifecycleErrorCode` funciona con errores envueltos.

### Composition root

`cmd/assistant/main.go` ahora:

1. valida configuración;
2. crea el contexto de señales;
3. construye e inicia `ServerManager`;
4. espera readiness;
5. construye por separado `llm.Client`;
6. construye `AgentRuntime`;
7. ejecuta el loop de voz;
8. detiene el servidor mediante defer.

El trabajo principal vive en `run() error`. Solo existe un `os.Exit(1)`, después de que `run` retorna y todos sus defers fueron ejecutados.

## Configuración

Campos agregados al bloque `llm`:

```yaml
mode: managed
startup_timeout_ms: 60000
shutdown_timeout_ms: 5000
health_interval_ms: 250
```

- `mode`: `managed` o `external`;
- timeouts e intervalo deben ser positivos;
- un cero explícito se rechaza y no se sustituye por default;
- configuraciones anteriores mantienen comportamiento managed por defaults;
- managed valida modelo regular y binary ejecutable;
- external valida endpoint y parámetros, sin exigir artefactos locales.

## Tests exactos

### Lifecycle

- `TestServerManagerFailsForMissingBinary`
- `TestServerManagerDetectsEarlyExit`
- `TestServerManagerTimesOutWhenHealthNeverReady`
- `TestServerManagerStopsOnContextCancellation`
- `TestServerManagerStopIsIdempotent`
- `TestServerManagerExternalModeDoesNotSpawn`
- `TestServerManagerSignalsThenKillsAfterTimeout`
- `TestServerManagerLeavesNoOrphanAfterStartupCancellation`

### Cliente y health

- `TestClientRejectsInvalidBaseURL`
- `TestClientReturnsMarshalOrRequestErrors`
- `TestClientHasNoProcessLifecycle`
- `TestHealthProbeClosesResponseBody`
- `TestHealthProbeReturnsRequestAndTransportErrors`
- `TestGenerate_UsesSingleLLMCallPerTurn`

### Configuración y composición

- `TestLoadAppliesLLMLifecycleDefaults`
- `TestValidateLLMExternalModeDoesNotRequireLocalArtifacts`
- `TestValidateLLMManagedModeRequiresLocalArtifacts`
- `TestValidateLLMRejectsInvalidLifecycleSettings`
- `TestApplicationOwnsLLMServerLifecycle`
- `TestApplicationUsesOrchestratorOnly`

## Matriz LLM-001..009

| ID | Resultado | Evidencia automatizada |
|---|---|---|
| LLM-001 | Implementado | `TestClientHasNoProcessLifecycle`, `TestApplicationOwnsLLMServerLifecycle` |
| LLM-002 | Implementado | `TestServerManagerExternalModeDoesNotSpawn`, `TestValidateLLMExternalModeDoesNotRequireLocalArtifacts` |
| LLM-003 | Implementado | `TestServerManagerDetectsEarlyExit` |
| LLM-004 | Implementado | `TestServerManagerTimesOutWhenHealthNeverReady`, `TestServerManagerStopsOnContextCancellation` |
| LLM-005 | Implementado | `TestServerManagerStopIsIdempotent` |
| LLM-006 | Implementado | `TestHealthProbeClosesResponseBody` |
| LLM-007 | Implementado | `TestClientRejectsInvalidBaseURL`, `TestClientReturnsMarshalOrRequestErrors`, `TestHealthProbeReturnsRequestAndTransportErrors` |
| LLM-008 | Implementado | `TestServerManagerSignalsThenKillsAfterTimeout` |
| LLM-009 | Implementado | `TestServerManagerLeavesNoOrphanAfterStartupCancellation` |

## VERIFY

### Functional GREEN

Head: `a2375d9138036f6ef206e0eefc09dcfd255bae06`

- Baseline inventory: run `31076896295`, verde.
- CI: run `31076896178`, verde.
- Job: `92536782669`.
- Coverage artifact ID: `8957875055`.
- Digest: `sha256:55cf77c6cbcb62f95774f5bb53d85db0adbec34d3d4c6d42415c0fa75c5377f5`.

Gates:

- `gofmt -l .`;
- `go vet ./...`;
- `go test -count=1 -coverprofile=coverage.out ./...`;
- `go test -count=20 ./internal/orchestrator ./internal/tools ./internal/llm`;
- `go test -race -count=1 ./internal/orchestrator ./internal/tools ./internal/llm`;
- `go build ./cmd/assistant ./cmd/calibrate`;
- coverage upload.

### Reviewed GREEN

Head: `12193f19d6af77e869a6342f7034ffa3ce3a1fab`

- Baseline inventory: run `31077181021`, verde.
- CI: run `31077180792`, verde.
- Job: `92537664768`.
- Coverage artifact ID: `8957984809`.
- Digest: `sha256:43dbf2dcda23a3539507d8a5c78d75cfa2e0b80cf58ef2684d197754780b408a`.

Todos los gates anteriores volvieron a pasar después de las correcciones de review y de la actualización de trazabilidad.

## REVIEW

### Regresión de fixtures

La primera ejecución GREEN completa, run `31076717256`, detectó que fixtures construidos directamente en tests no declaraban el nuevo modo ni timeouts. Se corrigieron para usar `external` cuando la prueba no depende de artefactos locales. No se debilitó el loader estricto ni se aceptaron estados ambiguos.

### Carrera readiness/exit

Se detectó que un health probe podía devolver éxito mientras la goroutine de `Wait` ya había cambiado el estado. La transición a ready ahora se confirma bajo lock; cualquier estado distinto de `started` devuelve `early_exit`.

### Carrera signal/exit

Se detectó que un error de `Signal` podía competir con una salida natural del proceso y provocar un `Kill` prematuro. El manager ahora concede siempre la espera acotada después del intento de señal antes de escalar.

### Resultado de review

- sin loops LLM duplicados;
- sin ownership de proceso en `Client`;
- sin `Wait` duplicado;
- sin shutdown de procesos external;
- sin `os.Exit` dentro del scope que posee recursos;
- sin contenido de prompts, respuestas o tool arguments en logs nuevos;
- sin comentarios de review abiertos en PR #27.

## Riesgo residual

- Linux es la plataforma primaria para la señal `os.Interrupt`; hardening y supervisión systemd pertenecen a WI-08.
- `/health` valida disponibilidad del servidor, no calidad semántica del modelo.
- En modo external, la recuperación y el shutdown del proceso dependen del supervisor externo.
- Un proceso que el sistema operativo no pueda terminar después de señal y kill produce `shutdown_failed`.
- No se implementan todavía streaming incremental, router de modelos, cambio de GGUF ni lifecycle de STT/TTS.

## Rollback

Revertir el squash merge de WI-05 restaura el constructor que inicia `llama-server` implícitamente.

- no existen migraciones de datos;
- para volver a una configuración anterior pueden retirarse `mode` y los timeouts, ya que los defaults reproducen managed;
- el rollback restaura polling fijo y kill directo, por lo que debe considerarse una regresión operativa consciente.
