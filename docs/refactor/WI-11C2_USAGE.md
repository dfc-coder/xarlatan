# WI-11C2 — Uso y diagnóstico

## Salida esperada

Para una frase suficientemente larga, la consola puede mostrar primero un preview:

```text
[partial] Xarlatan explícame cómo funciona...
```

Ese texto es informativo. No activa wake, no modifica memoria y no llega al AgentRuntime.

Después del endpoint de Silero aparece el turno final por el flujo normal. Sólo esa transcripción final puede pasar el wake gate y producir respuesta.

## Frases cortas

Una frase menor al umbral de preview puede no mostrar `[partial]`. Esto es correcto: el final sigue siendo obligatorio y autoritativo.

## Diagnóstico

- si aparece `[partial]` pero no hay final, revisar endpoint/captura;
- si el agente responde antes del final, es un bug de autoridad STT;
- si un preview de un turno anterior aparece en el siguiente, es un bug de TurnID/cancelación;
- si el preview aumenta demasiado la latencia final, medir el costo del Whisper offline antes de considerar un backend ASR online.

## Privacidad

La snapshot parcial es RAM-only, capacidad 1, una por utterance y no se persiste.
