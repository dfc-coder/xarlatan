# WI-07 — Uso operativo

## Ejecución

El comando público no cambia:

```bash
./bin/assistant -config config.yaml -no-tools -log debug
```

`cmd/assistant` construye una única `application.Application` y delega el loop completo mediante `Run(ctx)`.

## Flujo de un turno

```text
idle
  -> listening
  -> transcribing
  -> thinking
  -> synthesizing
  -> speaking
  -> idle
```

- audio vacío o ruido corto: no-op;
- transcript vacío: no-op;
- fallo recuperable de captura, STT, agente, síntesis o playback: se registra el código y comienza otro turno;
- cancelación o deadline: se detiene la etapa activa y termina el loop;
- configuración o dependencias inválidas: error terminal.

## Límites conservados

Los límites de WI-04 y WI-06 continúan vigentes:

```text
-max-tool-rounds=4
-max-history-bytes=12288
-max-summary-bytes=2048
```

WI-07 no agrega configuración de wake word, streaming ni captura continua.

## Componentes

### `internal/application.Application`

Coordina etapas de voz mediante interfaces pequeñas:

- `VoiceInput`;
- `Transcriber`;
- `Responder`;
- `Synthesizer`;
- `Player`;
- `Observer`;
- `View`.

### `internal/conversation.Session`

Coordina exactamente:

```text
memory.Prepare -> AgentRuntime.Run -> memory.Update
```

Solo devuelve historial después de que `memory.Update` confirma el turno.

### Presentación y observabilidad

- `View` recibe transcript y respuesta para mostrarlos al usuario.
- `Observer` recibe únicamente `turn_id` y estados.
- `Trace` contiene duraciones, conteos y códigos; no contiene audio, texto, prompts ni argumentos de tools.

## Cancelación

`Ctrl+C` cancela el contexto raíz. La cancelación se propaga a:

- `arecord` mediante `exec.CommandContext`;
- STT antes y después del decode offline;
- `conversation.Session` y `AgentRuntime`;
- TTS antes y después de la generación offline;
- `aplay` mediante `exec.CommandContext`.

Sherpa offline no puede garantizar preemption dentro de una llamada CGo ya iniciada. Este límite queda documentado hasta WI-11.

## Base para Fase 2

- WI-10 reemplaza la implementación de `VoiceInput` por captura continua, ring buffer y wake word.
- WI-11 extiende STT/TTS/playback para chunks y barge-in.
- Model routing y memoria persistente permanecen detrás de `conversation.Session`.
- Eventos del observer pueden adaptarse más adelante al event bus sin acoplar `Application`.

## Rollback

Revertir el squash merge de WI-07. No existen migraciones de datos ni estado persistente nuevo. El rollback restaura el loop de voz en `cmd/assistant` y bloquea WI-10/WI-11 hasta recuperar contratos testeables.