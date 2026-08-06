# WI-05 — Lifecycle determinista de llama-server

## Estado

- Issue: #8
- Parent: #2
- Depends on: WI-00 (#3)
- Requirements: LLM-001..LLM-009

## Problema

`internal/llm.Client` mezcla dos responsabilidades distintas:

1. gestionar el subprocesso `llama-server`;
2. enviar requests OpenAI-compatible y parsear respuestas SSE.

El constructor inicia el proceso, bloquea hasta 60 segundos con polling fijo y no observa una salida prematura. El cierre usa `Kill` directamente, no ofrece espera escalonada y no es un lifecycle explícito. Una cancelación durante startup puede devolver sin garantizar cleanup. El cliente también construye requests ignorando errores de serialización y de URL.

## Objetivo

Separar el cliente de inferencia del lifecycle del servidor y garantizar startup, readiness, cancelación y shutdown deterministas, sin procesos ni response bodies pendientes.

## Alcance

### Incluido

- `ServerManager` con `Start`, `WaitReady`, `Wait` y `Stop` explícitos;
- modo `managed`, que inicia y posee un subprocesso;
- modo `external`, que nunca inicia ni detiene subprocessos;
- proceso y health probe inyectables para tests;
- detección de salida antes de readiness;
- timeout y cancelación durante startup;
- shutdown escalonado: señal, espera acotada y kill como fallback;
- `Stop` idempotente y seguro ante concurrencia;
- cliente HTTP construido por separado y sin ownership del proceso;
- validación de base URL y propagación de errores de marshal/request;
- cierre de todos los health response bodies;
- integración del composition root con orden de shutdown definido;
- configuración estricta y documentación de modo/timeouts.

### No incluido

- cambio de modelo GGUF;
- streaming incremental nuevo;
- router de modelos;
- lifecycle de STT/TTS;
- supervisor externo/systemd, que corresponde a WI-08;
- reintentos de generación LLM.

## Requisitos

### LLM-001 — Separación de lifecycle

El cliente HTTP no inicia, espera ni detiene procesos. `ServerManager` es el único owner del subprocesso administrado.

### LLM-002 — Modo external

En modo `external`, `Start` no invoca la fábrica de procesos y `Stop` no envía señales. `WaitReady` puede comprobar el endpoint configurado.

### LLM-003 — Salida prematura

Si el proceso termina antes de que health esté ready, `WaitReady` devuelve un error tipado `early_exit` con la causa disponible.

### LLM-004 — Timeout y contexto

`WaitReady` respeta el contexto del caller y un timeout de startup configurado. En modo managed, todo fallo de readiness detiene y espera el proceso antes de retornar.

### LLM-005 — Stop idempotente

Múltiples llamadas, incluidas llamadas concurrentes, ejecutan una única secuencia de shutdown y devuelven un resultado estable.

### LLM-006 — Health sin fugas

Cada respuesta HTTP de health se drena de forma acotada y se cierra tanto para éxito como para status no-ready.

### LLM-007 — Cliente sin panic

Base URLs, requests o payloads inválidos producen errores. El cliente nunca usa `panic` ni ignora errores operativos de construcción.

### LLM-008 — Cierre escalonado

Para un proceso vivo administrado, `Stop` envía primero una señal de terminación, espera hasta el timeout y usa `Kill` solo si no terminó.

### LLM-009 — Sin subprocessos huérfanos

Tras cancelación de startup o shutdown, `Wait` termina y el proceso administrado no continúa ejecutándose.

## Diseño mínimo

### Cliente

```go
type ClientConfig struct {
    BaseURL    string
    HTTPClient HTTPDoer
    Temperature float64
    TopP       float64
    MaxTokens  int
}

func NewClient(ClientConfig) (*Client, error)
```

`Client` implementa el contrato `orchestrator.Generator` existente. No expone `Close` y no conoce `exec.Cmd`.

### Server manager

```go
type ServerManager interface {
    Start(context.Context) error
    WaitReady(context.Context) error
    Wait() error
    Stop(context.Context) error
}
```

La implementación concreta recibe:

- `ServerConfig`;
- `ProcessFactory`;
- `HealthProbe`;
- reloj/ticker únicamente si la prueba lo necesita.

El proceso se espera una sola vez en una goroutine propiedad del manager. El resultado queda almacenado y es consumido por `Wait`, `WaitReady` y `Stop` sin ejecutar `Wait` dos veces.

### Estados

```text
new -> started -> ready -> stopping -> stopped
             \-> exited
external: new -> ready-checkable -> stopped(no-op)
```

Los métodos inválidos retornan error tipado, no panic.

## Errores tipados

Códigos mínimos:

- `invalid_config`;
- `start_failed`;
- `early_exit`;
- `startup_timeout`;
- `cancelled`;
- `shutdown_failed`.

Los errores preservan la causa mediante `%w`.

## Configuración

Se agregan campos estrictos:

```yaml
llm:
  mode: managed                 # managed | external
  startup_timeout_ms: 60000
  shutdown_timeout_ms: 5000
  health_interval_ms: 250
```

- `managed` requiere `server_binary` ejecutable y `model` regular;
- `external` no requiere binary/model locales, pero sí host y port válidos;
- todos los timeouts/intervalos deben ser positivos.

## Tests RED

- `TestServerManagerFailsForMissingBinary`;
- `TestServerManagerDetectsEarlyExit`;
- `TestServerManagerTimesOutWhenHealthNeverReady`;
- `TestServerManagerStopsOnContextCancellation`;
- `TestServerManagerStopIsIdempotent`;
- `TestServerManagerExternalModeDoesNotSpawn`;
- `TestServerManagerSignalsThenKillsAfterTimeout`;
- `TestHealthProbeClosesResponseBody`;
- `TestClientRejectsInvalidBaseURL`;
- `TestClientReturnsMarshalOrRequestErrors`;
- `TestServerManagerLeavesNoOrphanAfterStartupCancellation`;
- composition-root test que impide volver a `llm.New` con spawn implícito.

## Invariantes

- `exec.Cmd.Wait` se invoca exactamente una vez;
- el manager nunca mata un proceso external;
- un retorno fallido de `WaitReady` en modo managed implica proceso detenido y esperado;
- ningún health response body queda abierto;
- ningún método bloquea indefinidamente sin contexto o timeout;
- `Client.Generate` no modifica el historial recibido;
- no se registra contenido de prompts, respuestas o argumentos de tools.

## Trade-offs

- El manager permanece específico de un proceso local, pero el cliente queda agnóstico respecto del ownership del servidor.
- El shutdown usa señal portable de interrupción antes de kill; el hardening específico de systemd queda para WI-08.
- No se agrega una dependencia externa de supervisión.

## Riesgo residual

- Un proceso que ignore tanto la señal como `Kill` es responsabilidad del sistema operativo y se reporta como `shutdown_failed`.
- La salud semántica del modelo no se valida más allá del endpoint `/health`.
- El modo external depende de un supervisor ajeno a Xarlatan.

## Rollback

Revertir el squash merge de WI-05 restaura el constructor que inicia `llama-server` implícitamente. No existen migraciones de datos. La configuración debe retirar `mode` y timeouts nuevos o volver a los defaults anteriores.
