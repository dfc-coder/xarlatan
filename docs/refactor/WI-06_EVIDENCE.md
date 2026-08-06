# WI-06 — Evidencia de memoria acotada

## Estado

- Issue: #9
- Epic: #2
- Pull request: #28
- Base `main`: `ee8d1bc6558d530405fd77e15d2e92ab66400b9a`
- RED válido: `0daae2238c0cbc957764f5ad51c417f0870e03bd`
- Functional GREEN: `1bae89c4982231552eccfb3f87f9a5c8ac64a5a0`
- Reviewed GREEN: `4e45b965f8d7cb956e2caa10a0ac74b2b256963e`
- Requirements: MEM-001..MEM-007

## DEFINE

El baseline limitaba memoria por cantidad fija de pares y no por tamaño real. Los mensajes descartados se concatenaban literalmente en un resumen, ese resumen se acumulaba con el anterior y se reincorporaba como un segundo mensaje `system`.

Ese diseño permitía crecimiento indefinido, elevaba conversación no confiable a un rol privilegiado y no garantizaba que exchanges de tools se conservaran de forma atómica.

La especificación de alcance, contratos, errores, invariantes y rollback se encuentra en `docs/refactor/WI-06_SPEC.md`.

## PLAN

1. Definir contratos RED para budget, roles, tools, eviction y conversación prolongada.
2. Introducir `memory.Manager` con `Summarizer` inyectable.
3. Medir el JSON exacto de `[]llm.Message`.
4. Mantener exactamente un system prompt confiable.
5. Conservar o descartar turnos completos.
6. Integrar compactación antes y después de cada inferencia.
7. Ejecutar cobertura, repetición, race, suite completa, build y review.

## RED

Los primeros intentos de RED contenían un archivo sin formato y fueron descartados del branch mediante reset. No se usan como evidencia TDD.

El RED válido es `0daae2238c0cbc957764f5ad51c417f0870e03bd`:

- Baseline inventory: run `31079169163`, verde.
- CI: run `31079169187`, fallo esperado.
- Job: `92543799426`.
- Formatting: verde.
- Primer error contractual en Vet: `internal/memory/memory_test.go:15:12: undefined: Config`.

El fallo demuestra que los contratos referenciaban las nuevas APIs antes de su implementación.

## GREEN

### Manager

`internal/memory.Manager` es propietario de:

- system prompt confiable;
- summary acotado;
- ventana de turnos recientes;
- configuración de presupuesto;
- validación del protocolo de tools.

APIs principales:

```go
func New(config Config, systemPrompt string, summarizer Summarizer) (*Manager, error)
func (m *Manager) History() []llm.Message
func (m *Manager) Prepare(ctx context.Context, userText string) (Snapshot, error)
func (m *Manager) Update(ctx context.Context, history []llm.Message) (Snapshot, error)
func (m *Manager) Reset()
```

### Budget

El tamaño se calcula mediante la serialización JSON exacta de `[]llm.Message`. Incluye:

- role;
- content;
- tool calls;
- argumentos JSON;
- `tool_call_id`.

`Prepare` garantiza antes de inferencia que:

```text
historial emitido + mensaje actual <= max-history-bytes
```

`Update` garantiza que el estado almacenado después del turno también permanezca dentro del mismo límite.

### Frontera de confianza

Solo el primer mensaje puede tener role `system` y debe coincidir exactamente con el prompt configurado.

El summary se incorpora como:

```text
role=user
content=[memory-summary]\n...
```

La memoria rechaza cualquier otro mensaje `system`.

### Turnos y tools

La eviction se realiza por turnos completos iniciados por `user`. El manager valida:

- IDs de tool call no vacíos;
- resultados tool con llamada correspondiente;
- ausencia de continuaciones assistant antes de recibir todos los resultados;
- ausencia de tool calls incompletas al terminar el turno;
- roles soportados.

Las copias incluyen duplicación defensiva de `json.RawMessage` para evitar aliasing de argumentos.

### Summary

`ExtractiveSummarizer` es local y determinista. No realiza otra inferencia LLM.

Un summary nuevo reemplaza completamente al anterior y vuelve a truncarse por bytes UTF-8 válidos antes de publicar el estado.

### Composition root

`cmd/assistant` agrega:

```text
-max-history-bytes=12288
-max-summary-bytes=2048
```

El loop ejecuta:

```text
STT
→ memory.Prepare(current input)
→ AgentRuntime
→ memory.Update(completed history)
→ TTS
```

Los logs de memoria contienen tamaños y contadores, no contenido conversacional.

## VERIFY

### Functional GREEN

Head: `1bae89c4982231552eccfb3f87f9a5c8ac64a5a0`.

- CI: run `31079856900`, verde.
- Baseline inventory: run `31079856908`, verde.
- Job: `92545969283`.
- Memory coverage: `90.5 %`.
- Coverage artifact: `8959019204`.
- Digest: `sha256:a2f42c0d4b486c4a8c8c8f0a1f9f223af21d48798327f2892837d038b98e6191`.

### Reviewed GREEN

Head: `4e45b965f8d7cb956e2caa10a0ac74b2b256963e`.

- CI: run `31080327665`, verde.
- Baseline inventory: run `31080328107`, verde.
- Job: `92547444775`.
- Memory coverage: `86.4 %`.
- Coverage artifact: `8959206907`.
- Digest: `sha256:435caae82b23ae61a7e66037271d00c8e9aebcb702a59d46b0c41de8e5177298`.

La cobertura bajó al agregar `Prepare`, pero permanece sobre el gate obligatorio de `80 %`.

Gates ejecutados:

- `gofmt -l .`;
- `go vet ./...`;
- `go test -count=1 -coverprofile=coverage.out ./...`;
- coverage focalizada de `internal/memory` >= 80 %;
- `go test -count=20 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory`;
- `go test -race -count=1 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory`;
- `go build ./cmd/assistant ./cmd/calibrate`;
- coverage artifact upload.

## REVIEW

### RED inválido descartado

Un primer commit de tests fallaba en formatting. Se rebobinó la rama hasta la especificación y se volvió a crear un RED formateado que falló únicamente por APIs inexistentes. Solo ese segundo RED se registra como evidencia.

### Cobertura insuficiente

La primera suite funcional completa pasó, pero el gate focalizado detectó `69.4 %` de cobertura. No se redujo el umbral. Se agregaron pruebas de:

- configuración inválida;
- reset y nil safety;
- resumen extractivo;
- cancelación;
- UTF-8 truncado;
- historias y protocolos de tools malformados.

La cobertura subió a `90.5 %` antes del refactor final.

### Mensaje actual fuera del budget

El primer diseño acotaba el historial almacenado, pero `llm.Client` agregaba el mensaje actual después. Por lo tanto, el request efectivo podía superar el presupuesto.

Se agregó `Manager.Prepare`, que mide y compacta el historial junto con el mensaje prospectivo antes de `AgentRuntime.Run`.

### Compactación prematura

La primera versión de `Prepare` reservaba siempre todo `MaxSummaryBytes`, lo que podía eliminar turnos aunque el prompt real ya cupiera.

Se agregó un fast path: si historial real + input cabe, no se descarta ningún turno ni se llama al summarizer.

### Resultado

- no existen segundo system prompt ni promoción de conversación;
- no existen tool results huérfanos en historia aceptada;
- no existe concatenación indefinida de summaries;
- el request efectivo se mide antes de inferencia;
- estado y snapshots son copias defensivas;
- cancelación o error de summary no publican estado parcial;
- no se agregó otra llamada LLM.

## Tests exactos

### Budget y estabilidad

- `TestMemoryNeverExceedsBudget`
- `TestMemoryPrepareBoundsNextPrompt`
- `TestMemoryPrepareDoesNotCompactWithinBudget`
- `TestMemoryPrepareRejectsInputLargerThanBudget`
- `TestLongConversationRemainsBounded`

### Trust y roles

- `TestMemoryKeepsSingleSystemPrompt`
- `TestMemoryDoesNotPromoteConversationToSystem`
- `TestMemoryRejectsMalformedHistories`

### Turnos y tools

- `TestMemoryPreservesToolExchange`
- `TestMemoryDropsOldestCompleteTurn`
- `TestMemoryRejectsInvalidToolExchange`

### Summary y atomicidad

- `TestSummaryReplacesPreviousSummary`
- `TestMemoryTruncatesOversizedUTF8Summary`
- `TestMemoryHonorsCancellation`
- `TestMemoryPrepareHonorsCancellation`
- `TestMemoryWrapsSummarizerFailure`
- `TestMemoryReturnsDefensiveCopies`

### Integración

- `TestApplicationUsesBoundedMemoryOnly`
- `TestApplicationUsesOrchestratorOnly`

## Matriz MEM-001..007

| ID | Resultado | Evidencia automatizada |
|---|---|---|
| MEM-001 | Implementado | `TestMemoryNeverExceedsBudget`, `TestMemoryPrepareBoundsNextPrompt`, `TestMemoryPrepareRejectsInputLargerThanBudget` |
| MEM-002 | Implementado | `TestMemoryKeepsSingleSystemPrompt` |
| MEM-003 | Implementado | `TestMemoryPreservesToolExchange`, `TestMemoryRejectsMalformedHistories` |
| MEM-004 | Implementado | `TestMemoryDoesNotPromoteConversationToSystem` |
| MEM-005 | Implementado | `TestMemoryDropsOldestCompleteTurn`, `TestMemoryPrepareBoundsNextPrompt` |
| MEM-006 | Implementado | `TestSummaryReplacesPreviousSummary`, `TestMemoryTruncatesOversizedUTF8Summary` |
| MEM-007 | Implementado | `TestLongConversationRemainsBounded` |

## Riesgo residual

- Bytes JSON no equivalen exactamente a tokens del tokenizer del modelo.
- El summary extractivo puede perder matices semánticos.
- La memoria continúa siendo volátil y se pierde al cerrar el proceso.
- Los límites se exponen mediante flags; WI-08 puede incorporarlos a la configuración instalada.
- Tool schemas y tokens de salida no forman parte del budget de memoria, por lo que debe conservarse margen operativo respecto del contexto total del modelo.

## Rollback

Revertir el PR:

1. restaura `Compact`, `Compose` y `MergeSummary`;
2. elimina `Manager`, `Prepare` y `ExtractiveSummarizer`;
3. elimina los flags de budget;
4. restaura el composition root anterior.

No existe migración de archivos ni datos persistentes.
