# WI-06 — Memoria acotada y segura

## Estado

- Issue: #9
- Parent: #2
- Depends on: WI-04 (#7)
- Base: `ee8d1bc6558d530405fd77e15d2e92ab66400b9a`
- Requirements: MEM-001..MEM-007

## Problema

La implementación actual de `internal/memory` no mantiene un presupuesto real de prompt:

1. recorta por cantidad fija de pares, sin considerar tamaño;
2. resume mediante concatenación literal de mensajes descartados;
3. concatena cada resumen nuevo al anterior, por lo que el estado vuelve a crecer sin límite;
4. inserta el resumen derivado de conversación como un segundo mensaje `system`;
5. corta por posiciones, por lo que no modela explícitamente turnos con llamadas y resultados de tools;
6. no puede demostrar que una conversación prolongada conserve un tamaño estable.

## Objetivo

Reemplazar la compactación posicional por un componente de memoria que construya un historial acotado, conserve turnos completos y mantenga una única frontera de confianza: solo el prompt configurado puede usar el rol `system`.

## Alcance

### Incluido

- presupuesto determinista medido en bytes del JSON de mensajes;
- configuración estricta de `max_history_bytes` y `max_summary_bytes`;
- `Manager` propietario del summary y de la ventana reciente;
- agrupación y eviction por turnos completos iniciados por `user`;
- preservación atómica de exchanges `assistant.tool_calls` + mensajes `tool`;
- rechazo de mensajes `system` dentro de contenido conversacional;
- summary opcional mediante interfaz pequeña `Summarizer`;
- implementación extractiva local, sin otra inferencia LLM;
- summary incorporado como contenido no confiable con rol `user`;
- replacement del summary anterior, nunca concatenación acumulativa;
- copia defensiva de mensajes y argumentos de tools;
- trazas de tamaño, turnos descartados y uso de summary;
- integración del composition root.

### No incluido

- memoria persistente entre procesos;
- embeddings, vector store o retrieval semántico;
- otra llamada LLM para resumir;
- tokenización específica de cada modelo;
- modificación del loop de tools del `AgentRuntime`;
- streaming o pipeline de voz.

## Modelo

```text
trusted system prompt
        +
optional untrusted summary (role=user)
        +
recent complete turns
        =
bounded history
```

Un turno comienza con un mensaje `user` y termina inmediatamente antes del siguiente mensaje `user`. Dentro del turno pueden existir múltiples secuencias:

```text
user
assistant(tool_calls)
tool
tool
assistant(tool_calls)
tool
assistant(final)
```

La memoria conserva o elimina el turno completo. Nunca corta entre una llamada y su resultado.

## Contratos

### Config

```go
type Config struct {
    MaxHistoryBytes int
    MaxSummaryBytes int
}
```

- ambos valores deben ser mayores que cero;
- `MaxSummaryBytes` debe ser menor que `MaxHistoryBytes`;
- el system prompt por sí solo debe caber dentro del budget.

### Summarizer

```go
type Summarizer interface {
    Summarize(ctx context.Context, previous string, dropped []llm.Message, maxBytes int) (string, error)
}
```

El resultado reemplaza completamente al summary anterior. El manager aplica el límite nuevamente aunque una implementación defectuosa lo exceda.

### Manager

```go
type Manager struct { ... }

func New(config Config, systemPrompt string, summarizer Summarizer) (*Manager, error)
func (m *Manager) History() []llm.Message
func (m *Manager) Update(ctx context.Context, history []llm.Message) (Snapshot, error)
func (m *Manager) Reset()
```

`History` devuelve una copia. `Update` solo muta estado después de validar y construir un snapshot que cabe en el budget.

## Requisitos

### MEM-001 — Budget máximo

Cada valor devuelto por `History` y `Snapshot.History` mide como máximo `MaxHistoryBytes`. La medición usa la serialización JSON de `[]llm.Message`, incluyendo roles, contenido, tool calls, argumentos y tool call IDs.

### MEM-002 — Único system prompt

El historial contiene exactamente un mensaje `system`: el prompt entregado a `New`. Ningún mensaje de entrada puede reemplazarlo ni agregar otro.

### MEM-003 — Tool exchange completo

Si se conserva un turno con llamadas de tools, se conservan conjuntamente el mensaje assistant, todos sus resultados tool y las continuaciones assistant del mismo turno. La eviction elimina el turno completo.

### MEM-004 — Sin promoción de conversación

Summary y contenido conversacional usan roles no privilegiados. Un `system` encontrado después del prompt confiable produce error tipado `invalid_history`.

### MEM-005 — Eviction por turno completo

Cuando el budget se excede, se descartan turnos completos desde el más antiguo. Nunca se conserva un sufijo parcial de un turno.

### MEM-006 — Summary reemplazable

Cada compactación que descarta turnos puede producir un summary nuevo. Ese valor reemplaza al anterior. El summary no se concatena indefinidamente y queda limitado por `MaxSummaryBytes`.

### MEM-007 — Conversación prolongada estable

Después de cientos de actualizaciones, el historial continúa bajo el mismo budget y el número de mensajes no crece sin límite.

## Errores

```text
invalid_config
invalid_history
budget_exceeded
summarizer_error
cancelled
```

Los errores incluyen causa sin registrar contenido completo de conversación.

## Algoritmo

1. Validar que el primer mensaje sea el system prompt confiable.
2. Remover del input el summary interno previo, identificado por un marcador privado y rol `user`.
3. Rechazar cualquier otro mensaje `system`.
4. Validar roles y relaciones de tool calls.
5. Agrupar mensajes en turnos iniciados por `user`.
6. Construir candidato con summary previo y todos los turnos.
7. Mientras exceda el budget, remover el turno completo más antiguo.
8. Si hubo eviction, invocar el summarizer una vez con el summary anterior y todos los mensajes descartados.
9. Reemplazar el summary y volver a ajustar al budget.
10. Si el turno más reciente por sí solo no cabe, descartarlo completo y resumirlo cuando exista summarizer.
11. Publicar el nuevo estado de forma atómica.

## Tests RED

- `TestMemoryNeverExceedsBudget`
- `TestMemoryKeepsSingleSystemPrompt`
- `TestMemoryPreservesToolExchange`
- `TestMemoryDoesNotPromoteConversationToSystem`
- `TestMemoryDropsOldestCompleteTurn`
- `TestSummaryReplacesPreviousSummary`
- `TestLongConversationRemainsBounded`
- `TestMemoryRejectsInvalidToolExchange`
- `TestMemoryReturnsDefensiveCopies`
- `TestMemoryHonorsCancellation`
- config defaults y validación estricta;
- composition root usa `memory.Manager` y elimina `MergeSummary`.

## Criterios de aceptación

- MEM-001..007 ligados a tests exactos;
- ningún historial emitido excede el budget;
- exactamente un rol `system` en cada historial;
- no hay llamadas de tool huérfanas ni resultados separados;
- no existe concatenación indefinida de summaries;
- suite focalizada, repetición, race, suite completa y build verdes;
- `internal/memory` alcanza al menos 80 % de cobertura de statements;
- configuración y trazabilidad actualizadas.

## Rollback

Revertir el PR restaura `Compact`, `Compose` y `MergeSummary`. El cambio de YAML agrega un bloque opcional con defaults; al revertir, ese bloque debe eliminarse porque el loader estricto lo rechazaría.

## Trade-offs

- El budget se expresa en bytes JSON, no tokens exactos. Es determinista, independiente del modelo y conservador, pero no representa exactamente el tokenizer del GGUF.
- La implementación extractiva puede perder matices. El objetivo de WI-06 es acotación y seguridad; un summarizer semántico puede agregarse detrás de la interfaz sin cambiar el manager.
- Un turno individual mayor que el budget no se fragmenta: se descarta completo y, cuando es posible, se representa mediante summary acotado.