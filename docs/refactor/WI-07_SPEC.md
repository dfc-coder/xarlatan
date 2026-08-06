# WI-07 — Aplicación de voz testeable y contratos para Fase 2

## Estado

Propuesto para implementación en #10.

## Objetivo

Extraer el pipeline de voz de `cmd/assistant/main.go` hacia una aplicación testeable con fakes, cancelación y errores tipados por etapa. `AgentRuntime` continúa siendo el único loop LLM/tools. El resultado debe servir como base estable para captura continua, wake word, streaming y barge-in sin implementar todavía esas capacidades.

## Requisitos

### AUD-001..AUD-006

- **AUD-001:** el tamaño de chunk PCM S16_LE se calcula con sample rate y canales; el flujo operativo requiere mono.
- **AUD-002:** captura y playback reciben `context.Context`, detienen el subprocesso activo y lo esperan.
- **AUD-003:** EOF parcial procesa únicamente frames PCM completos y nunca reutiliza bytes de una lectura anterior.
- **AUD-004:** síntesis y playback son etapas y errores diferentes.
- **AUD-005:** VAD, pre-roll y silencio final conservan el comportamiento del baseline.
- **AUD-006:** traces y métricas contienen tamaños, estados, duraciones y códigos, nunca audio ni texto conversacional.

### APP-001..APP-006

- **APP-001:** STT fallido o vacío no invoca conversación/agente.
- **APP-002:** conversación/agente fallido no invoca síntesis ni playback.
- **APP-003:** síntesis o playback fallidos son recuperables para el loop.
- **APP-004:** un turno exitoso ejecuta capture, STT, session, synthesis y playback exactamente una vez y en orden.
- **APP-005:** cancelación detiene la etapa activa, no inicia otra y prevalece sobre errores secundarios.
- **APP-006:** `main.go` queda limitado a configuración, construcción, señales, ejecución y shutdown.

## Invariantes

1. `internal/orchestrator.AgentRuntime` es el único propietario de rondas LLM/tools.
2. `conversation.Session` llama exactamente una vez al agente por turno.
3. `application.Application` no conoce `llm.Client`, `memory.Manager`, sherpa-onnx, tools ni `exec.Cmd`.
4. Todo borde potencialmente bloqueante recibe `context.Context`.
5. Ninguna etapa posterior comienza cuando la anterior falla.
6. Cancelación y deadline son terminales para `Application.Run`.
7. Fallos recuperables vuelven al estado de escucha.
8. No se introduce event bus global, scheduler, persistencia ni goroutines permanentes.
9. Tests de aplicación no usan ALSA, modelos, red ni procesos reales.

## Arquitectura

```text
cmd/assistant
    |
    v
application.Application
    +--> VoiceInput.Next(ctx) ----------> audio.Buffer
    +--> Transcriber.Transcribe(ctx, b) -> text
    +--> conversation.Session.Respond(ctx, text)
    |       +--> memory.Prepare
    |       +--> AgentRuntime.Run
    |       +--> memory.Update
    +--> Synthesizer.Synthesize(ctx, reply) -> audio.Buffer
    +--> Player.Play(ctx, buffer)
    +--> Observer / View
```

## Contratos

```go
type VoiceInput interface {
    Next(context.Context) (audio.Buffer, error)
}

type Transcriber interface {
    Transcribe(context.Context, audio.Buffer) (string, error)
}

type Responder interface {
    Respond(context.Context, string) (conversation.Result, error)
}

type Synthesizer interface {
    Synthesize(context.Context, string) (audio.Buffer, error)
}

type Player interface {
    Play(context.Context, audio.Buffer) error
}
```

Las interfaces se definen en el paquete consumidor. `audio.Buffer` es un valor de dominio con muestras float32, sample rate y canales; no expone sherpa, WAV temporal ni subprocessos.

## Conversación

`internal/conversation.Session` encapsula solamente:

```text
memory.Prepare -> AgentRuntime.Run -> memory.Update
```

No contiene routing, tools, retries ni otro loop. Un error del agente no ejecuta `memory.Update` ni expone historial parcial como estado confirmado.

## Estados

```text
idle -> listening -> transcribing -> thinking -> synthesizing -> speaking -> idle
```

`stopping` representa cancelación/shutdown. WI-10 puede insertar `wake_detected`; WI-11 puede insertar `interrupted` sin reemplazar el contrato de observación.

## Errores

Códigos estables:

- `capture_failed`
- `transcription_failed`
- `agent_failed`
- `synthesis_failed`
- `playback_failed`
- `cancelled`
- `deadline_exceeded`
- `invalid_application`

Sin audio o transcript vacío son no-op, no errores terminales. Errores de capture/STT/agent/synthesis/playback son recuperables salvo cancelación/deadline. Configuración o dependencia inválida es terminal.

## Observabilidad

Cada turno produce eventos/traza con:

- `turn_id` monotónico;
- estado y etapa;
- duración por etapa y total;
- cantidad de muestras;
- longitud de transcript y respuesta;
- outcome y error code.

No contiene muestras, transcript, respuesta, prompts, tool arguments ni tool results. La salida visible de transcript/respuesta usa un `View` separado.

## Alcance

- `internal/application`;
- `internal/conversation`;
- `audio.Buffer`;
- captura y playback cancelables detrás de factories pequeñas;
- STT con contexto;
- separación síntesis/playback;
- observer y view;
- `main.go` como composition root;
- tests unitarios, E2E fake, repetición y race;
- evidencia, trazabilidad y CI.

## Fuera de alcance

- wake word y captura continua;
- ring buffer continuo;
- STT/TTS streaming y barge-in;
- event bus/scheduler;
- memoria persistente;
- model router;
- skills/confirmaciones;
- systemd e instalación;
- smoke de hardware definitivo.

## Tests RED obligatorios

### Audio

- `TestRecorderCalculatesMonoChunkSize`
- `TestRecorderStopsOnCancellation`
- `TestRecorderProcessesPartialEOF`
- `TestRecorderPreservesPreRollAndTrailingSilence`
- `TestPlayerStopsOnCancellation`

### Conversación

- `TestSessionCallsAgentRuntimeOnce`
- `TestSessionDoesNotCommitMemoryWhenAgentFails`
- `TestSessionDoesNotReturnUncommittedHistory`

### Aplicación

- `TestApplicationSkipsAgentWhenSTTFails`
- `TestApplicationSkipsAgentWhenTranscriptIsEmpty`
- `TestApplicationSkipsSynthesisWhenAgentFails`
- `TestApplicationSkipsPlaybackWhenSynthesisFails`
- `TestApplicationRecoversAfterSynthesisError`
- `TestApplicationRecoversAfterPlaybackError`
- `TestApplicationCompletesVoiceTurn`
- `TestApplicationStopsDuringEachStageOnCancellation`
- `TestApplicationEmitsOrderedStates`
- `TestApplicationTraceDoesNotContainConversationContent`
- `TestMainIsCompositionRootOnly`

Los tests de cancelación usan channels/fakes, no sleeps arbitrarios.

## Gates

```bash
gofmt -l .
go vet ./...
go test -count=1 -coverprofile=coverage.out ./...
go test -count=20 ./internal/audio ./internal/application ./internal/conversation
go test -race -count=1 ./internal/audio ./internal/application ./internal/conversation
go build ./cmd/assistant ./cmd/calibrate
```

Además:

- `internal/application` y `internal/conversation` >= 80 % statements;
- cobertura de audio/VAD no menor que el baseline anterior;
- ningún test de aplicación usa ALSA, sherpa, llama-server o red;
- `main.go` no contiene el loop por etapas;
- suites WI-04, WI-05 y WI-06 permanecen verdes.

## Base para Fase 2

- WI-10 reemplaza `VoiceInput` por captura continua + wake word.
- WI-11 extiende transcripción/síntesis/playback para chunks y agrega `interrupted`.
- WI-12 permanece detrás de `AgentRuntime`/Session.
- WI-14 permanece detrás de Session/memoria.
- WI-15 puede adaptar eventos del observer a un event bus sin acoplar Application.
- WI-17 reutiliza Session desde entradas no vocales.

## Rollback

Revertir el squash merge de WI-07. No existen migraciones de datos. WI-10 y WI-11 quedan bloqueados después del rollback.