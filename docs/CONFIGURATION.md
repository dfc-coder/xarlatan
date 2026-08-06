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
    max_read_bytes: 1048576
    max_write_bytes: 1048576
```

Para habilitar solo lectura:

```yaml
tools:
  filesystem:
    enabled: true
    root: "/home/user/xarlatan-data"
    allow_mutations: false
    max_read_bytes: 1048576
    max_write_bytes: 1048576
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
    max_read_bytes: 1048576
    max_write_bytes: 1048576
```

Esto agrega `fs_write`, `fs_delete` y `fs_mkdir`.

### Garantías del sandbox

- El root se resuelve a una ruta absoluta y canónica una vez al construir el registry.
- Se rechazan traversal, paths absolutos externos y escapes posteriores a resolver symlinks.
- Los destinos nuevos validan el parent existente antes de crear directorios o archivos.
- Las mutaciones rechazan path vacío, `.`, la raíz y symlinks finales.
- Las escrituras y append usan temporal, `Sync`, `Close` y `Rename` en el mismo directorio.
- `max_read_bytes` rechaza archivos mayores al límite antes de devolver contenido.
- `max_write_bytes` limita tanto el payload como el tamaño resultante de un append.
- El sandbox no reemplaza `ToolPolicy`: las mutaciones continúan deshabilitadas salvo opt-in.

Linux es la plataforma primaria. El resolver evita escapes reproducibles por paths y symlinks, pero no pretende defender contra un actor local concurrente con permisos equivalentes que modifique el árbol durante una operación; el aislamiento del servicio se completa en WI-08.

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
- límites positivos de lectura y escritura cuando filesystem está habilitado;
- existencia de modelos y tokens STT/TTS/LLM;
- ejecutabilidad de `llama-server`;
- existencia de `tts.data_dir`.

## Cambio incompatible desde WI-00

`tools.fs_root` ya no es válido. El loader lo rechaza como campo desconocido. Debe migrarse al bloque `tools.filesystem` mostrado arriba.
