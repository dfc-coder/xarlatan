# WI-11B — Deterministic interruption path

## Objetivo

Crear la ruta de lifecycle necesaria para barge-in antes de conectar el micrófono físico durante playback.

## Principio

Una interrupción del usuario no equivale a apagar el servicio.

```text
speech_started
     |
     v
InterruptSource
     |
     v
Coordinator(active TurnID)
     |
     +-- cancel active turn with cause=interrupted
     +-- stop LLM/TTS through context cancellation
     +-- Playback.Stop when audio already started
     +-- reject late deltas/results by TurnID
     v
interrupted
     |
     v
next turn
```

`cancelled` queda reservado para cancelación externa/global del servicio. `interrupted` es recuperable y sólo termina el turno activo.

## Contrato InterruptSource

La fuente recibe el `TurnID` activo al suscribirse y publica únicamente metadata:

- `TurnID`;
- razón opcional (`speech_started`, `stop`, etc.).

No transporta PCM, transcript ni texto de conversación. Eventos con otro `TurnID` se descartan.

## Cancelación

El Coordinator usa `context.WithCancelCause`. Cuando llega una interrupción válida:

- cause = `turn interrupted`;
- el responder/LLM observa `ctx.Done()`;
- TTS observa el mismo contexto;
- si ya hubo audio streaming, `Playback.Stop()` se ejecuta exactamente una vez;
- el trace termina en `StateInterrupted` con outcome `interrupted` y `ErrorInterrupted`;
- `Coordinator.Run` continúa al siguiente turno.

Una cancelación del contexto padre conserva `StateStopping` / `ErrorCancelled` y termina el loop como antes.

## No incluido

- segundo micrófono durante playback;
- AEC;
- discriminación usuario/eco;
- captura continua;
- wake word;
- STT parcial.

Esas capacidades conectarán este contrato en los siguientes slices.

## RED/GREEN

- interrupción durante LLM streaming cancela el turno;
- interrupción después del primer audio ejecuta Stop exactamente una vez;
- interrupción antes del audio no llama Stop;
- evento de TurnID obsoleto no afecta el turno actual;
- eventos duplicados son idempotentes;
- sin InterruptSource el comportamiento existente permanece igual;
- repetición y `-race` no dejan goroutines bloqueadas.

## Gate

```bash
go test -count=1 ./...
go test -count=20 ./internal/application ./internal/audio ./internal/llm ./internal/orchestrator ./internal/conversation
go test -race -count=1 ./internal/application ./internal/audio ./internal/llm ./internal/orchestrator ./internal/conversation
```
