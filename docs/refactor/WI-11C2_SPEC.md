# WI-11C2 — Bounded partial/final STT events

## Objetivo

Dar al pipeline una señal STT parcial antes del endpoint sin reemplazar ni reinterpretar el backend Whisper offline actual. `v0.5.0-beta.1` usa un único preview decode acotado; el decode final sigue siendo la única transcripción autoritativa.

## Contrato honesto del backend

Whisper se usa mediante `sherpa.OfflineRecognizer`; no produce tokens parciales nativos. Por eso WI-11C2 no lo presenta como un recognizer online. La implementación beta toma una snapshot de audio mientras el usuario todavía habla y la decodifica una sola vez como preview.

Un backend transducer/Zipformer online puede reemplazar este mecanismo en el futuro detrás del mismo contrato `TranscriptEvent`.

## Arquitectura

```text
ContinuousRecorder
      |
      | speech >= ~1 s
      +--> PartialAudio snapshot (capacity 1, once/utterance)
      |          |
      |          v
      |    partial STT worker
      |          |
      |          v
      |    TranscriptEvent{partial}
      |
      | Silero endpoint
      v
 final utterance
      |
 final STT
      |
 TranscriptEvent{final}
      |
 wake gate -> AgentRuntime
```

## Data plane

`TranscriptEvent` contiene texto, por lo que no usa `application.Observer`, que permanece metadata-only.

```text
TranscriptEvent:
- TurnID
- Kind: partial | final
- Text
```

La transcripción parcial puede mostrarse en UI/console, pero no entra en memoria ni en el AgentRuntime. La final sí continúa por el flujo normal.

## Snapshot parcial

- máximo una snapshot por utterance;
- se produce cuando el buffer activo alcanza ~1.0 s;
- sólo se produce si existe un `Next()` activo esperando ese utterance;
- channel capacity = 1 y drop-oldest;
- al comenzar un nuevo `Next`, se limpian snapshots anteriores;
- al entrar en playback/barge mode, se limpian previews normales;
- audio nunca se persiste.

Utterances cortos pueden no producir parcial.

## Concurrencia

El preview y el final comparten el mismo `stt.Transcriber`, ya serializado en WI-11C1. Si el endpoint llega mientras el preview sigue decodificando, el contexto del preview se cancela; Whisper offline puede completar el decode nativo en curso, pero su resultado no se publica después de la cancelación. El final sigue siendo autoridad.

## Métrica

`Trace.FirstSTTPartial` contiene el tiempo desde inicio del turno hasta el primer preview válido. Si no hubo preview, permanece en cero.

## Invariantes

- partial nunca llama Responder;
- partial nunca pasa por wake gate;
- partial nunca actualiza memoria;
- final se publica una vez y mantiene el TurnID;
- parciales cancelados/tardíos no pasan al siguiente turno;
- no hay polling de Whisper por chunk;
- no se agrega otro modelo ni dependencia de licencia para esta beta.

## Tests

- preview llega antes del final en un utterance largo controlado;
- responder permanece en cero mientras sólo existe partial;
- final llama responder una sola vez;
- partial/final comparten TurnID;
- `FirstSTTPartial > 0` cuando hubo preview;
- utterance sin preview mantiene `FirstSTTPartial == 0`;
- snapshot queue es capacidad 1 y conserva la más nueva;
- snapshot sólo se publica con `Next` activo;
- repeat y race permanecen verdes.

## No incluido

- ASR token-by-token;
- múltiples previews;
- cambio de Whisper a un modelo online;
- consumo de partial por LLM/memoria.

Estas restricciones mantienen el costo y la semántica acotados para la prueba física de `v0.5.0-beta.1`.
