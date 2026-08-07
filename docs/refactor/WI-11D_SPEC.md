# WI-11D — v0.5.0-beta.1 continuous voice acceptance

## Objetivo

Convertir las foundations de Fase 2 en una beta instalable y físicamente verificable en `dakota-fedora`.

## Release candidate

```text
v0.5.0-beta.1
```

Incluye:

- Coordinator event-driven y workers persistentes;
- una única captura ALSA continua;
- Silero VAD y pre-roll acotado;
- wake gate por transcript;
- STT partial/final con final autoritativo;
- streaming LLM -> frases -> TTS -> playback;
- playback persistente/cancelable;
- interrupción determinista TurnID-scoped;
- barge-in físico conservador wake-qualified;
- métricas de primer partial, primer delta, primer audio e interrupción;
- systemd user service.

## Política física de barge-in

Esta beta no afirma AEC full-duplex genérico. Durante playback, la única captura continua usa Silero + gate RMS adaptativo para producir candidatos; sólo wake + stop explícito confirma interrupción.

```text
Xarlatan, para
Xarlatan, detente
Xarlatan, cancela
Xarlatan, basta
```

El criterio de la beta es evitar self-trigger antes que aceptar cualquier voz arbitraria durante playback.

## Acceptance físico

El harness `scripts/beta_v05_acceptance.sh` prueba:

1. preflight histórico;
2. versión y runtime libs sin `LD_LIBRARY_PATH`;
3. systemd user start/stop;
4. exactamente un child `arecord` estable;
5. habla sin wake ignorada;
6. turno wake-qualified largo;
7. presencia de `[partial]`;
8. barge-in `Xarlatan, para`;
9. turno posterior a la interrupción;
10. mismo PID de captura antes/después;
11. no self-trigger por TTS/eco;
12. cleanup de procesos owned.

Los hechos perceptuales siguen requiriendo confirmación del operador.

## Resultado

Se genera `beta-acceptance-<timestamp>.md` y el work item sólo puede cerrarse si termina exactamente:

```text
Final result: PASS
```

## Gate CI previo

```bash
go test -count=1 ./...
go test -count=20 ./internal/application ./internal/audio ./internal/stt ./internal/wake ./internal/barge
go test -race -count=1 ./internal/application ./internal/audio ./internal/stt ./internal/wake ./internal/barge
bash scripts/tests/wi08_contract_test.sh
bash scripts/tests/wi09_contract_test.sh
bash scripts/tests/wi11d_contract_test.sh
```

## No incluido

- AEC genérico;
- barge-in wake-free;
- model router;
- memoria persistente;
- scheduler/integraciones/multimodalidad.
