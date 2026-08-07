# WI-11A — Streaming de respuestas directas LLM -> TTS -> playback

## Estado

Primer slice de baja latencia de WI-11 después de WI-10A (Coordinator), WI-10B (playback persistente) y WI-10C (Silero VAD).

## Objetivo

Empezar a sintetizar y reproducir una respuesta directa antes de que `llama-server` termine de generarla, sin crear un segundo loop de agente ni reproducir contenido especulativo de rounds que pueden terminar en tool calls.

## Arquitectura

```text
llama-server SSE
      |
      v
 llm.Client
      | ContentDelta
      v
 AgentRuntime (único loop)
      |
      v
 conversation.Session
      | canal privado de deltas, TurnID
      v
 Coordinator
      |
      v
 sentenceBuffer
      |
      +--> TTS worker --> Playback.Write
      +--> TTS worker --> Playback.Write
      +--> ...
      |
 final response
      v
 Playback.Finish -> drain audible -> microphone guard
```

Los eventos observables continúan siendo metadata-only. Texto incremental, PCM y resultados de etapa pertenecen al data plane interno.

## Regla de seguridad para tools

El streaming de contenido sólo está habilitado cuando el modelo no recibe definiciones de tools.

Si `AgentRuntime` tiene tools expuestas:

1. se conserva el mismo loop LLM/tools existente;
2. no se publica ningún `ContentDelta`;
3. el Coordinator recibe únicamente el resultado final ya resuelto;
4. TTS/playback usan el camino bufferizado anterior.

Esta restricción evita hablar texto de planificación que posteriormente pueda convertirse en una tool call. Un slice posterior podrá separar planning y final-answer si existe una necesidad demostrada.

## Contratos

### LLM

`Generate` conserva su API. `GenerateStream` es una extensión opcional que reconstruye exactamente el mismo reply final y publica deltas no vacíos durante el parseo SSE.

Un error del consumidor de deltas aborta el stream y se propaga.

### AgentRuntime

`Run` y `RunStream` comparten la misma implementación interna. No existe un segundo loop de tools.

Si hay cualquier tool expuesta, `RunStream` suprime el callback del turno completo.

### Conversation Session

`RespondStream` conserva la transacción:

```text
Memory.Prepare -> AgentRuntime -> Memory.Update -> Result
```

Los deltas pueden salir antes, pero el `Result` final sólo se publica después de un `Memory.Update` exitoso. Un fallo del agente no compromete memoria parcial.

### Segmentación

Los deltas SSE no se envían token por token a TTS. `sentenceBuffer`:

- corta en `.`, `!`, `?` o newline;
- conserva UTF-8;
- fuerza un corte acotado si una respuesta sin puntuación supera 180 runes;
- hace flush del fragmento final cuando termina el modelo.

### Playback

Una respuesta streaming usa una sola sesión `aplay` compatible:

```text
Write(chunk 1)
Write(chunk 2)
...
Finish()
```

`Finish` cierra stdin, espera que `aplay` drene el PCM audible y sólo después ejecuta el microphone rearm guard. `Stop` aborta inmediatamente y queda reservado para cancelación/barge-in.

`Play(buffer)` se redefine como `Write(buffer) + Finish()` y conserva el contrato bufferizado existente.

## Estados

El Coordinator conserva una secuencia estable aunque existan múltiples chunks:

```text
idle
listening
transcribing
thinking
synthesizing   # primera frase
speaking       # primer chunk de playback
idle           # después de Finish
```

No se emiten alternancias `synthesizing/speaking` por cada frase.

## Métricas

El trace mantiene sólo metadata y agrega:

- `FirstResponseDelta`: tiempo desde inicio del turno hasta el primer delta seguro;
- `FirstAudio`: tiempo desde inicio del turno hasta el comienzo del primer playback streaming.

No se registran tokens, transcript, audio ni respuesta en estas métricas.

## Cancelación y TurnID

- cada delta lleva `TurnID` en el canal privado;
- deltas de otro turno se ignoran;
- el callback respeta el contexto del turno;
- un error posterior a audio parcial ejecuta `Playback.Stop` antes de abandonar el turno;
- un `Finish` cancelado también detiene el proceso de playback;
- los workers siguen siendo los únicos que ejecutan responder, TTS y playback.

## Fallback

El camino bufferizado sigue siendo obligatorio cuando:

- el responder no soporta streaming;
- el player no soporta `Write/Finish/Stop`;
- el modelo tiene tools expuestas y por ello no publica deltas;
- no se recibió ningún delta seguro.

El fallback debe producir la misma respuesta, historia, estados y semántica que antes de WI-11A.

## Tests RED/GREEN

- SSE publica deltas en orden y reconstruye el mismo reply final;
- callback fallido cancela el consumo SSE;
- tools expuestas suprimen deltas tanto en Client como en AgentRuntime;
- AgentRuntime streaming comparte el loop existente;
- Session transmite deltas pero sólo retorna historia comprometida;
- sentence buffer reconstruye español/UTF-8 y limita frases sin puntuación;
- dos frases producen dos TTS/Write y un único Finish;
- el primer Write ocurre antes de que termine el responder en el test controlado;
- ausencia de deltas usa `Synthesize + Play` bufferizado;
- `Finish` drena antes del guard;
- cancelación durante Write/Drain ejecuta Stop;
- suite repetida y race-test permanecen verdes.

## Gates

```bash
go test -count=1 ./...
go test -count=20 ./internal/llm ./internal/orchestrator ./internal/conversation ./internal/application ./internal/audio
go test -race -count=1 ./internal/llm ./internal/orchestrator ./internal/conversation ./internal/application ./internal/audio
```

Además permanecen los gates previos de coverage, WI-08/WI-09, build/version y systemd.

## No incluido

- STT parcial;
- wake word;
- captura continua;
- detección de voz durante playback;
- transición de barge-in `speaking -> interrupted -> listening`;
- streaming de rounds con tools.

Esos elementos completan WI-11 en slices posteriores.
