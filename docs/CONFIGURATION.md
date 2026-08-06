# Configuración

Xarlatan carga YAML en modo estricto. Los campos desconocidos, documentos múltiples y valores fuera de rango detienen el proceso antes de construir STT, LLM, TTS, audio o tools.

`ASSISTANT_CONFIG` tiene prioridad sobre el flag `-config`.

## AgentRuntime

El loop LLM/tools se ejecuta exclusivamente mediante `internal/orchestrator.AgentRuntime`.

El máximo de rondas que pueden ejecutar tools durante un turno se configura mediante CLI:

```bash
assistant -max-tool-rounds=4
```

El default es `4`. El valor debe ser mayor que cero y se valida antes de construir STT, LLM, TTS o tools.

Una ronda de tool comprende:

1. respuesta del modelo con una o más llamadas;
2. ejecución secuencial de esas llamadas en el orden recibido;
3. incorporación de mensajes `role=tool` conservando cada `tool_call_id`;
4. nueva llamada al modelo.

Una respuesta directa no consume el budget. Cuando el modelo solicita una ronda adicional después de alcanzar el límite, el runtime devuelve `round_limit` y no ejecuta esas nuevas llamadas.

## Lifecycle de llama-server

El cliente de inferencia y el lifecycle del servidor son componentes separados. `llm.Client` solo realiza requests HTTP; `llm.ServerManager` posee el proceso local cuando corresponde.

### Modo managed

Es el modo por defecto. Xarlatan inicia `llama-server`, espera `/health`, detecta una salida prematura y lo detiene durante shutdown.

```yaml
llm:
  mode: "managed"
  server_binary: "./bin/llama-server"
  model: "models/llm/model.gguf"
  host: "127.0.0.1"
  port: 8080
  context_size: 4096
  n_gpu_layers: 0
  threads: 4
  startup_timeout_ms: 60000
  shutdown_timeout_ms: 5000
  health_interval_ms: 250
  temperature: 0.7
  top_p: 0.9
  max_tokens: 512
```

En este modo, `server_binary` debe ser ejecutable y `model` debe ser un archivo regular existente.

El cierre es escalonado:

1. señal de interrupción;
2. espera hasta `shutdown_timeout_ms`;
3. `Kill` como fallback;
4. espera final acotada para recolectar el subprocesso.

### Modo external

Xarlatan se conecta a un servidor administrado por systemd, contenedor u otro supervisor. No inicia, señala ni mata subprocessos.

```yaml
llm:
  mode: "external"
  server_binary: ""
  model: ""
  host: "127.0.0.1"
  port: 8080
  context_size: 4096
  n_gpu_layers: 0
  threads: 4
  startup_timeout_ms: 15000
  shutdown_timeout_ms: 5000
  health_interval_ms: 250
  temperature: 0.7
  top_p: 0.9
  max_tokens: 512
```

`server_binary` y `model` locales no son requeridos en modo external. El endpoint `/health` sí debe alcanzar estado ready antes de iniciar el loop de voz.

Todos los timeouts e intervalos deben ser mayores que cero. Un cero escrito explícitamente no se reemplaza por el default: la configuración se rechaza.

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
- `searxng`: requiere una `base_url` HTTP(S) válida y públicamente enrutable.

### Política HTTP/SSRF

`web_search` y `web_fetch` comparten un único `SafeHTTPClient`. La policy se aplica al URL inicial, a cada resolución DNS y a cada redirect efectivo.

Se rechazan antes de conectar:

- schemes distintos de `http` y `https`;
- URLs relativas, sin host o con credenciales embebidas;
- `localhost`, loopback IPv4/IPv6 y direcciones unspecified;
- RFC1918, IPv6 ULA y CGNAT;
- link-local, multicast y endpoints conocidos de metadata;
- hostnames cuyo DNS devuelva una IP bloqueada, incluso si también devuelve IPs públicas.

El dial se realiza contra una IP ya validada, evitando una segunda resolución insegura. Los redirects están limitados a cinco saltos y eliminan headers sensibles cuando cambia host o puerto.

Límites de producción:

- timeout total: 15 segundos;
- body máximo duro: 512 KiB;
- redirects máximos: 5;
- content types permitidos: `text/*`, JSON, XML, XHTML, JavaScript textual y variantes `+json`/`+xml`.

`web_fetch.max_bytes` puede solicitar un límite menor. Cuando la respuesta lo excede, devuelve el prefijo acotado con `[truncated]`. Los providers de búsqueda rechazan una respuesta estructurada truncada para no intentar parsear JSON incompleto.

No existen excepciones implícitas para servicios privados. Un SearXNG local o de red interna requiere un adapter confiable separado con policy explícita; no debe conectarse mediante las tools web genéricas.

## Validación runtime

Antes de iniciar, se comprueba:

- límite de tool rounds positivo;
- audio mono y parámetros positivos;
- `llm.mode`, host, puerto, parámetros de generación y timeouts;
- modelo y ejecutable de `llama-server` solo cuando `llm.mode` es `managed`;
- provider de búsqueda y sus campos requeridos;
- root de filesystem absoluto, existente, directorio y distinto de `/`;
- límites positivos de lectura y escritura cuando filesystem está habilitado;
- existencia de modelos y tokens STT/TTS;
- existencia de `tts.data_dir`.

## Cambios incompatibles

Desde WI-00, `tools.fs_root` ya no es válido. El loader lo rechaza como campo desconocido. Debe migrarse al bloque `tools.filesystem` mostrado arriba.

Desde WI-05, los campos desconocidos del bloque `llm` continúan rechazándose. Las configuraciones anteriores siguen funcionando mediante defaults `mode: managed`, `startup_timeout_ms: 60000`, `shutdown_timeout_ms: 5000` y `health_interval_ms: 250`.
