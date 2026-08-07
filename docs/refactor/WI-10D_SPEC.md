# WI-10D — Continuous capture, bounded pre-roll and wake gate

## Objetivo

Mantener un único proceso de captura durante la vida del asistente y convertir su stream PCM en utterances acotadas mediante Silero VAD, sin almacenar audio continuo. Cada utterance se transcribe una sola vez y sólo abre un turno si pasa el wake gate.

## Arquitectura

```text
persistent arecord
      |
      v
ContinuousRecorder
      |
      +-- suppressed while assistant speaks (PCM discarded)
      |
      v
Silero VAD
      |
320 ms bounded pre-roll
      |
4-slot bounded utterance queue
      |
VoiceInput.Next()
      |
STT exactly once
      |
WakeDetector
      |
Coordinator: wake_detected -> thinking -> ...
```

## Captura

`ContinuousRecorder` conserva el contrato `VoiceInput.Next`, por lo que Coordinator y workers no conocen ALSA ni el lifecycle del proceso. El primer `Next` inicia `arecord`; llamadas posteriores reutilizan el mismo proceso. Cancelar un contexto de turno sólo cancela la espera en `Next`, no la fuente global.

Si downstream se atrasa, la cola mantiene como máximo cuatro utterances y descarta el más antiguo antes de aceptar audio nuevo. `DroppedUtterances` expone sólo el contador, nunca contenido.

`Close` detiene el proceso y libera Silero exactamente una vez.

## Pre-roll y privacidad

El único audio previo a voz retenido es un ring de cuatro chunks de 80 ms (320 ms). No existe persistencia en disco ni buffer continuo sin límite. Al publicar una utterance se copia sólo ese segmento y el resto del PCM se descarta.

## Wake gate

El wake gate funciona sobre la transcripción final para evitar un segundo decode STT.

Default CLI:

```text
-wake=true
-wake-word=xarlatan
-wake-aliases=charlatan,charlatán
```

`internal/wake.Detector` está separado del Coordinator y puede reemplazarse posteriormente por un detector dedicado sin cambiar lifecycle. `PhraseDetector`:

- compara palabras completas, no substrings;
- normaliza mayúsculas y diacríticos españoles para matching;
- acepta wake en los primeros tres tokens o como dirección al final;
- rechaza menciones ambientales en medio de una frase larga;
- elimina el wake word antes de enviar el command al AgentRuntime.

Un transcript rechazado termina como `noop` con outcome `wake_ignored`. Un match emite el estado metadata-only `wake_detected`.

## Half-duplex durante WI-10D

La captura ALSA permanece abierta durante TTS, pero `captureGateObserver` marca `ContinuousRecorder` como suppressed durante `speaking`. El read loop sigue drenando PCM y lo descarta antes de VAD.

El antiguo microphone-rearm guard abría un segundo `arecord`; en continuous mode se reemplaza por un settling delay de 350 ms mientras la captura sigue suppressed. Esto mantiene la protección anti-eco del beta anterior y respeta el invariant de una única fuente de captura.

WI-11C sustituirá esta supresión completa durante playback por una política explícita de barge-in/echo discrimination.

## Recuperación

Si el proceso de captura termina inesperadamente, la fuente publica un error acotado y vuelve a estado no-running. El siguiente `Next` puede iniciar una nueva fuente. No se crean procesos por turno en operación normal.

## Tests

- dos utterances consecutivas usan un único factory start;
- cancelar `Next` no detiene la fuente;
- cola de utterances mantiene capacidad fija y descarta oldest;
- `Close` es idempotente;
- límites inválidos fallan antes de iniciar proceso;
- wake aliases y diacríticos funcionan;
- substrings/menciones ambientales no activan;
- transcript sin wake no llega al responder;
- match produce `wake_detected` y command sin prefijo;
- observer suprime sólo durante speaking;
- producción requiere ContinuousRecorder, continuous playback guard y wake detector.

## Gates

```bash
go test -count=1 ./...
go test -count=20 ./internal/audio ./internal/application ./internal/wake ./internal/vad/...
go test -race -count=1 ./internal/audio ./internal/application ./internal/wake ./internal/vad/...
```

Se conservan además todos los gates de release, systemd y WI-09.

## No incluido

- escuchar Silero durante playback;
- AEC o comparación de echo reference;
- barge-in físico;
- STT parcial;
- conversation follow-up window sin wake word.

Estos puntos pertenecen a WI-11C / cierre de WI-10 y WI-11 antes de `v0.5.0-beta.1`.
