# WI-07 — Evidencia

## Objetivo

Extraer el pipeline de voz desde `cmd/assistant` hacia una aplicación testeable, cancelable y observable, manteniendo un único camino para AgentRuntime y memoria y dejando contratos estables para WI-10/WI-11.

## DEFINE

- Issue: #10.
- Especificación: `docs/refactor/WI-07_SPEC.md`.
- Uso: `docs/refactor/WI-07_USAGE.md`.
- Requisitos: AUD-001..AUD-006 y APP-001..APP-006.

## RED

Los intentos que fallaron únicamente por formato o sintaxis de fixtures se descartan como evidencia TDD.

RED contractual válido:

- commit: `7b1db04ee2840e7a7388ee6f7b52a71013016238`;
- CI: `31114968393`;
- baseline inventory: `31114971122`;
- formato: success;
- primer fallo: `undefined: pcmChunkBytes` en `internal/audio/wi07_test.go` y contratos nuevos de `internal/conversation`/`internal/application` inexistentes.

## GREEN funcional

La primera suite que alcanzó tests completos sobre `19fc516b71c6948cf3b7e42ddd41b466fc9737f5` confirmó:

- `gofmt`: success;
- `go vet ./...`: success;
- `internal/application`: 93.7 %;
- `internal/conversation`: 84.5 %;
- `internal/memory`: 86.4 %;
- `internal/orchestrator`: 86.0 %;
- `internal/vad`: 90.1 %.

El único fallo fue una regresión de un fixture histórico con sample rate artificial de 10 Hz. Se corrigió el cálculo de chunk mediante redondeo superior para garantizar al menos una muestra por chunk, manteniendo 2560 bytes para PCM mono S16_LE a 16 kHz.

## Implementación

### Audio

- `audio.Buffer` desacopla dominio de sherpa, WAV y subprocessos;
- chunk size usa sample rate, mono y S16_LE;
- EOF parcial procesa solo frames completos;
- captura y playback reciben `context.Context`;
- `arecord` y `aplay` usan comandos cancelables;
- VAD, pre-roll y silencio final se conservan.

### Conversación

- `conversation.Session` ejecuta `memory.Prepare -> AgentRuntime.Run -> memory.Update`;
- llama una vez al agente;
- no confirma ni devuelve historial parcial cuando agente o memoria fallan;
- devuelve copias defensivas, incluidos argumentos JSON de tool calls.

### Aplicación

- `Application.RunTurn` coordina las cinco etapas en orden;
- `Application.Run` recupera fallos de etapa y termina ante cancelación/configuración inválida;
- síntesis y playback son límites separados;
- estados: idle, listening, transcribing, thinking, synthesizing, speaking y stopping;
- observer y trace no reciben contenido conversacional;
- `View` queda separado para presentación explícita.

### Composition root

`cmd/assistant/main.go` construye dependencias, lifecycle y `Application`; no ejecuta etapas de voz, memoria ni agente directamente.

## Gates requeridos

```bash
gofmt -l .
go vet ./...
go test -count=1 -coverprofile=coverage.out ./...
go test -count=20 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
go test -race -count=1 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
go build ./cmd/assistant ./cmd/calibrate
```

Cobertura mínima:

- memory >= 80 %;
- application >= 80 %;
- conversation >= 80 %.

## Riesgo residual

- sherpa offline comprueba cancelación antes y después de CGo, pero no puede interrumpir un decode/generate ya iniciado;
- captura continúa iniciando `arecord` por turno hasta WI-10;
- no existe wake word, streaming ni barge-in hasta WI-10/WI-11;
- el smoke definitivo de dispositivos pertenece a WI-09.

## Rollback

Revertir el squash merge de WI-07. No existen migraciones ni persistencia nueva. El rollback restaura el loop anterior en `main.go` y bloquea WI-10/WI-11.

## Verificación final

Pendiente de completar con head final, runs, artefacto y digest después de review.