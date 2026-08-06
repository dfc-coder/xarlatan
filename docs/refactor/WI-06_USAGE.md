# WI-06 — Operación de memoria acotada

Xarlatan mantiene la memoria de la conversación en RAM durante la ejecución. No persiste conversaciones entre reinicios.

## Presupuestos

Los límites operativos se exponen mediante CLI:

```bash
./bin/assistant \
  -max-history-bytes=12288 \
  -max-summary-bytes=2048
```

Defaults:

- `max-history-bytes`: `12288`;
- `max-summary-bytes`: `2048`.

Ambos deben ser positivos y el límite de summary debe ser menor que el límite total.

El budget se mide sobre el JSON exacto de `[]llm.Message`. Incluye roles, contenido, llamadas de tools, argumentos y `tool_call_id`. Es una medida determinista e independiente del modelo; no equivale exactamente al conteo de tokens del GGUF.

## Frontera de confianza

Cada historial contiene exactamente un mensaje `system`: el prompt configurado en `llm.system_prompt`.

El summary derivado de conversación se incorpora como un mensaje `user` con marcador interno. Aunque contenga instrucciones, nunca obtiene privilegios de sistema.

```text
system: prompt confiable
user: [memory-summary] contexto previo no confiable
user: turno reciente
assistant: respuesta reciente
```

## Turnos y tools

Un turno comienza con `user` y termina antes del siguiente `user`. La eviction elimina turnos completos.

```text
user
assistant(tool_calls)
tool
tool
assistant(final)
```

El manager rechaza:

- mensajes `system` adicionales;
- resultados tool sin llamada correspondiente;
- llamadas sin todos sus resultados;
- IDs de llamada vacíos o duplicados dentro de una ronda;
- continuaciones assistant antes de recibir todos los resultados.

## Resumen

`memory.ExtractiveSummarizer` es local y determinista. No ejecuta otra inferencia LLM.

Cuando se descartan turnos:

1. recibe el summary anterior y todos los mensajes descartados;
2. genera un valor acotado;
3. el nuevo valor reemplaza por completo al summary anterior;
4. el manager vuelve a aplicar el budget antes de publicar el estado.

No existe concatenación indefinida de summaries.

## Fallos

Los errores estables son:

- `invalid_config`;
- `invalid_history`;
- `budget_exceeded`;
- `summarizer_error`;
- `cancelled`.

Un fallo de invariantes durante el loop de voz es terminal para esa ejecución. Xarlatan no continúa enviando al modelo una historia que no pudo validar.

## Ajuste práctico

El default de 12 KiB es conservador para el modelo inicial. Para modelos con contexto mayor puede ampliarse, manteniendo un margen para schemas de tools, mensaje del usuario y salida:

```bash
./bin/assistant -max-history-bytes=32768 -max-summary-bytes=4096
```

No conviene igualar el budget de memoria al contexto completo anunciado por el modelo. El request también contiene definición de tools y tokens de generación.