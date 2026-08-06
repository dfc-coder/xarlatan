# WI-09 hotfix — autoactivación por eco

## Contexto

La aceptación física de `v0.4.0-beta.1` demostró que captura, STT, LLM, TTS y playback funcionan en foreground. También demostró un defecto bloqueante: después de hablar, Xarlatan reabría el micrófono inmediatamente y procesaba ruido o audio residual como una consulta nueva.

Evidencia observada:

```text
Hablando
Escuchando
Procesando STT
👤 (Sombre)
...
Hablando
Escuchando
Procesando STT
👤 [Música]
```

Issue: #35. Bloquea #12 y el RPM #32.

## Causa

`Application.Run` repite turnos correctamente, pero `Playback.Play` retornaba apenas terminaba `aplay`. El siguiente turno abría `arecord` sin margen para drenar el dispositivo ni comprobar que el ambiente hubiera vuelto al silencio.

Whisper también puede producir marcadores no verbales como `[Música]`; antes se aceptaban como input conversacional.

## Corrección beta

### Half-duplex posterior al TTS

`audio.Playback` incorpora un guard obligatorio:

1. reproduce la respuesta completa;
2. mantiene el micrófono desarmado durante 700 ms;
3. abre captura descartable;
4. exige siete chunks de 80 ms —560 ms— por debajo del umbral RMS `0.012`;
5. recién entonces retorna y permite el siguiente `listening`.

El guard respeta `context.Context`. Si el proceso se cancela durante cooldown o rearme, no inicia otro turno.

### Filtro STT

Se convierten en transcript vacío únicamente respuestas totalmente encerradas en `[]` o `()` y de hasta 64 runes, por ejemplo:

```text
[Música]
[Music]
(Sombre)
(ruido)
```

Frases normales como `pon música`, `hola (otra vez)` o `usa [corchetes] aquí` permanecen intactas. Un transcript vacío ya es tratado por la aplicación como `noop`, por lo que no invoca al agente ni actualiza memoria.

## Tests

- el rearme requiere silencio consecutivo y reinicia la ventana si reaparece energía;
- el cooldown es cancelable;
- el guard ocurre después de playback;
- un error de playback no ejecuta el guard;
- markers no verbales se descartan;
- texto conversacional y whitespace normal se preservan.

## Acceptance físico

La prueba debe formular una sola consulta y luego permanecer en silencio durante el resto de la ventana de 45 segundos.

El reporte exige un gate adicional:

```text
no spontaneous post-playback turns | PASS
```

No se acepta WI-09 si aparecen nuevos `Procesando STT`, transcripts o respuestas sin una consulta humana.

## Límites

Este hotfix es half-duplex y no equivale a Acoustic Echo Cancellation. AEC real, wake word, captura persistente, streaming y barge-in continúan fuera de alcance hasta WI-10/WI-11.
