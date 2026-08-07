# WI-11C1 — Wake-qualified physical barge-in

## Objetivo

Permitir que el usuario corte una respuesta hablada usando la misma captura continua de WI-10D, sin convertir el eco del propio TTS en una interrupción falsa.

## Política de `v0.5.0-beta.1`

La beta prioriza **cero self-trigger** sobre barge-in arbitrario. Durante `speaking`, una interrupción sólo se confirma si pasan dos gates:

1. gate acústico: Silero + energía relativa persistente por encima de un baseline adaptativo;
2. gate textual: STT confirma wake word + verbo explícito de stop (`para`, `detente`, `cancela`, `basta`, `silencio`, `cállate`).

Ejemplos válidos:

```text
Xarlatan, para
Xarlatan, detente
Xarlatan, cancela
Xarlatan, basta
```

Una frase de eco como `estoy aquí para ayudarte` puede incluso generar un candidato acústico, pero no puede cancelar playback porque no satisface el wake gate.

## Arquitectura

```text
single persistent arecord
          |
          +---- normal mode ----> Silero ----> utterance queue
          |
          +---- speaking mode --> adaptive RMS + Silero
                                      |
                               bounded candidate
                                      |
                               BargeInController
                                      |
                              shared serialized STT
                                      |
                              ExplicitStopPolicy
                                      |
                         InterruptEvent(TurnID, latency)
                                      |
                                 Coordinator
                                      |
                         cancel active turn by cause
                         /           |            \
                       LLM          TTS       Playback.Stop
```

Audio de candidatos pertenece al data plane privado. `InterruptEvent` sólo lleva metadata (`TurnID`, razón y latencia).

## Gate acústico

Durante speaking:

- se calientan 3 chunks (~240 ms) para estimar el nivel de eco/ambiente;
- threshold = `max(0.025 RMS, baseline * 1.65)`;
- Silero debe reportar voz;
- se requieren 2 chunks consecutivos por encima del threshold;
- se conservan 2 chunks de pre-roll;
- candidato máximo: 2 s;
- candidate queue: capacidad 2, drop-oldest;
- al terminar speaking se descartan candidatos pendientes.

Este no es AEC. Es un filtro conservador previo a la confirmación textual.

## STT compartido

El recognizer Whisper existente sigue siendo la fuente de verdad. `stt.Transcriber` serializa el acceso a sherpa con mutex para impedir decodes concurrentes entre el STT normal y la validación de barge-in.

El candidato nunca modifica memoria ni llega a AgentRuntime.

## Interrupción

`BargeInController` implementa `application.InterruptSource`:

1. recibe `BargeCandidate` privado;
2. transcribe;
3. aplica `ExplicitStopPolicy`;
4. si confirma, publica un único `InterruptEvent` para el `TurnID` activo;
5. Coordinator cancela el contexto del turno;
6. LLM/TTS observan cancelación y playback ejecuta `Stop`;
7. trace termina `interrupted`;
8. el loop vuelve a escuchar.

Eventos/candidatos viejos no deben poder afectar el siguiente TurnID.

## Métrica

`Trace.InterruptLatency` mide desde el comienzo estimado del candidato acústico hasta la confirmación textual que dispara la cancelación. Es metadata-only.

## Tests

- echo-level input no genera candidato acústico;
- energía persistente + Silero genera candidato;
- candidate queue permanece acotada y se limpia al reanudar;
- transcript sin wake no interrumpe;
- wake sin stop no interrumpe;
- `Xarlatan, para` confirma;
- controller publica exactamente un evento para el TurnID activo;
- cancelación cierra subscription;
- trace conserva `InterruptLatency`;
- STT compartido está serializado;
- repetición y race permanecen verdes.

## No incluido

- AEC full-duplex genérico;
- interrupción wake-free por cualquier voz;
- reutilizar el mismo utterance como siguiente comando;
- STT parcial.

El siguiente slice WI-11C2 agrega parciales/finales; una versión posterior podrá relajar la policy física si las mediciones del hardware permiten AEC/reference cancellation fiable.
