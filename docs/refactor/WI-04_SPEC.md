# WI-04 — AgentRuntime único

Tracking: issue #7.

## DEFINE `/spec`

### Problema observable

`cmd/assistant/main.go` contiene el loop real LLM/tools, limitado a una sola ronda de herramientas. En paralelo, `internal/orchestrator` expone un grafo de nodos placeholder (`Router`, `Planner`, `ResponseComposer`, `MemoryUpdate`) que no ejecuta el flujo de producción ni usa el modelo real. Esto crea dos arquitecturas incompatibles, impide múltiples rondas y deja orden, cancelación y trazas sin un dueño único.

### Requisitos

- **AGT-001:** existe un solo runtime responsable de coordinar LLM y tools.
- **AGT-002:** una respuesta directa termina sin ejecutar tools.
- **AGT-003:** el runtime soporta múltiples rondas LLM → tools → LLM.
- **AGT-004:** la cantidad de rondas de tools tiene un límite configurable y estricto.
- **AGT-005:** mensajes assistant/tool conservan orden y `tool_call_id`.
- **AGT-006:** una tool desconocida produce un código de error estable y un mensaje de role `tool`.
- **AGT-007:** errores recuperables de tools vuelven al LLM para que pueda corregir o responder.
- **AGT-008:** la cancelación interrumpe la generación o ejecución activa y no inicia otra ronda.
- **AGT-009:** cada llamada al modelo produce un `RoundTrace` observable.

### Alcance

- introducir `AgentRuntime` en `internal/orchestrator`;
- definir una interfaz mínima para el generador LLM existente;
- ejecutar tools mediante `tools.Executor` y conservar mensajes OpenAI compatibles;
- agregar resultados tipados de ejecución de tools sin romper `RunAll`;
- mover todo el loop agente fuera de `cmd/assistant/main.go`;
- exponer `-max-tool-rounds` con default seguro y validación antes de construir el runtime;
- reemplazar observabilidad por trazas de rounds y tools;
- eliminar nodos y contextos placeholder no conectados.

### No alcance

- lifecycle de `llama-server` (WI-05);
- compactación y budget de memoria (WI-06);
- extraer el loop completo audio/STT/TTS a `Application` (WI-07);
- retries automáticos de red o tools;
- ejecución paralela de tools;
- planificación separada o model router;
- ampliar el esquema YAML estricto solo para este límite operativo.

### Invariantes

1. Solo `AgentRuntime.Run` llama al generador LLM durante un turno.
2. `main.go` no inspecciona `ToolCalls`, no ejecuta tools y no repite llamadas al LLM.
3. Las tools se ejecutan secuencialmente en el orden solicitado.
4. Cada resultado conserva exactamente el ID de su llamada.
5. Una tool fallida produce un mensaje `tool` y no aborta por sí sola el turno.
6. Al alcanzar el límite, no se ejecutan las nuevas llamadas solicitadas.
7. `context.Canceled` y `context.DeadlineExceeded` prevalecen sobre errores secundarios.
8. El historial de entrada no se modifica in-place.
9. Cada round registra número, duración, calls, resultados y stop reason.

### Contrato propuesto

```go
type AgentRuntime struct { /* model, registry, executor, observer, limits */ }

type Request struct {
    Input   string
    History []llm.Message
}

type Result struct {
    Reply   string
    History []llm.Message
    Trace   Trace
}

func (r *AgentRuntime) Run(ctx context.Context, request Request) (Result, error)
```

`MaxToolRounds` cuenta rondas que efectivamente ejecutan tools. Una respuesta directa puede finalizar en el primer round del modelo sin consumir ese budget. El binario usa `-max-tool-rounds=4` por defecto y rechaza valores menores o iguales a cero antes de construir STT, LLM, TTS o tools.

### Errores

`RuntimeError` debe distinguir al menos:

- `invalid_runtime`;
- `model_error`;
- `round_limit`;
- `cancelled`;
- `deadline_exceeded`.

Los errores de una tool se representan con `tools.ExecutionErrorCode`:

- `unknown_tool`;
- `denied_tool`;
- `tool_error`;
- `cancelled`.

### Criterios de aceptación

- respuesta directa: una llamada LLM, cero tools;
- una y múltiples rondas producen historial válido y reply final;
- al exceder el límite se devuelve `round_limit` y no se ejecuta la ronda excedente;
- múltiples calls conservan orden e IDs en mensajes y trace;
- tool desconocida y tool con error regresan al LLM como resultados recuperables;
- cancelación durante tool execution termina el runtime;
- observer recibe un evento por cada round;
- `cmd/assistant` delega el turno completo al runtime;
- no permanecen planners/composers placeholder;
- suite completa, repetición, race, vet y build quedan verdes.

## PLAN `/plan`

### Slice A — Contrato y RED

- tests de respuesta directa, una/múltiples rondas y límite;
- tests de orden, IDs, unknown tool y error recuperable;
- tests de cancelación y trace;
- architecture test para impedir un segundo loop en `main`.

### Slice B — Runtime y executor tipado

- `AgentRuntime` con modelo inyectable;
- loop acotado;
- conversión de `ToolMessage` a `llm.Message`;
- `Executor.RunAllDetailed` y códigos estables;
- observer por round.

### Slice C — Composition root y limpieza

- configurar `-max-tool-rounds` con default 4;
- usar runtime desde `main.go`;
- eliminar nodos/contextos placeholder y sus tests;
- conservar compactación de memoria fuera del runtime hasta WI-06.

### Slice D — Verificación y ship

- `go test -count=20 ./internal/orchestrator ./internal/tools`;
- `go test -race -count=1 ./internal/orchestrator ./internal/tools`;
- suite completa, vet y builds;
- revisión de diff, trazabilidad, evidencia y rollback.

## Riesgo residual

El runtime queda unificado, pero memoria y voice application continúan coordinadas desde `main.go` hasta WI-06/WI-07. El límite de rounds evita loops ilimitados, pero no introduce budget de tokens ni timeout propio adicional al `context.Context`.

## Rollback

Revertir el squash merge de WI-04 restaura el loop de una sola ronda en `main.go` y los nodos placeholder. No requiere migración de datos ni cambios en `config.yaml`; el flag `-max-tool-rounds` deja de existir al volver a la versión anterior.
