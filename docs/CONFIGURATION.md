# Configuración

Xarlatan carga YAML en modo estricto. Los campos desconocidos, documentos múltiples y valores fuera de rango detienen el proceso antes de construir STT, LLM, TTS, audio o tools.

`ASSISTANT_CONFIG` tiene prioridad sobre el flag `-config`.

## Filesystem tools

Filesystem está deshabilitado por defecto:

```yaml
tools:
  filesystem:
    enabled: false
    root: ""
    allow_mutations: false
```

Para habilitar solo lectura:

```yaml
tools:
  filesystem:
    enabled: true
    root: "/home/user/xarlatan-data"
    allow_mutations: false
```

Esto expone únicamente:

- `fs_read`
- `fs_list`
- `fs_stat`

Las mutaciones requieren opt-in explícito:

```yaml
tools:
  filesystem:
    enabled: true
    root: "/home/user/xarlatan-data"
    allow_mutations: true
```

Esto agrega `fs_write`, `fs_delete` y `fs_mkdir`.

**Limitación:** WI-01 controla autorización y configuración, pero el confinamiento contra symlinks se implementará en WI-02. Mantener filesystem deshabilitado fuera de pruebas controladas hasta completar ese work item.

## Web search

```yaml
tools:
  web_search:
    provider: "duckduckgo"
    api_key: ""
    base_url: ""
```

Providers:

- `duckduckgo`: no requiere clave.
- `brave`: requiere `api_key`.
- `searxng`: requiere una `base_url` HTTP(S) válida.

## Validación runtime

Antes de iniciar, se comprueba:

- audio mono y parámetros positivos;
- host, puerto y parámetros LLM;
- provider de búsqueda y sus campos requeridos;
- root de filesystem absoluto, existente, directorio y distinto de `/`;
- existencia de modelos y tokens STT/TTS/LLM;
- ejecutabilidad de `llama-server`;
- existencia de `tts.data_dir`.

## Cambio incompatible desde WI-00

`tools.fs_root` ya no es válido. El loader lo rechaza como campo desconocido. Debe migrarse al bloque `tools.filesystem` mostrado arriba.
