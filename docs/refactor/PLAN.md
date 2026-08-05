# Plan maestro de refactorización SDD/TDD

## 1. Objetivo

Dejar Xarlatan operativo, seguro, reproducible y mantenible sin reemplazar su arquitectura completa. El refactor se ejecutará mediante slices verticales pequeños, cada uno trazado desde un requisito hasta evidencia de release.

El resultado esperado es un asistente local que:

- capture audio mono y detecte voz de forma estable;
- transcriba, consulte al LLM y sintetice respuesta;
- ejecute herramientas mediante una policy explícita;
- mantenga memoria acotada;
- gestione llama-server de forma determinista;
- se instale como servicio sin privilegios;
- pueda compilarse, probarse, instalarse y revertirse con comandos documentados.

## 2. Estado de partida confirmado

El snapshot analizado contiene:

- Go 1.21;
- `cmd/assistant` y `cmd/calibrate`;
- paquetes `audio`, `config`, `console`, `llm`, `memory`, `orchestrator`, `stt`, `tools`, `tts` y `vad`;
- 19 archivos de pruebas;
- integración con ALSA, sherpa-onnx y llama-server;
- un orquestador probado pero no utilizado por el runtime principal;
- herramientas de filesystem y web habilitables sin policy suficiente;
- instalación systemd que actualmente puede ejecutar el proceso con privilegios excesivos;
- referencias a `compose.yml` sin que el archivo exista en el snapshot.

El repositorio remoto no contiene todavía ese snapshot. La primera entrega debe importarlo sin mezclar correcciones funcionales.

## 3. Alcance

### Incluido

- baseline reproducible;
- configuración estricta y validada;
- hardening de filesystem y red;
- unificación del runtime agente;
- lifecycle de llama-server;
- memoria acotada;
- contratos testeables para audio, STT y TTS;
- packaging, systemd, CI, E2E y runbook;
- documentación y trazabilidad.

### Fuera de alcance

- interfaz gráfica;
- soporte multiplataforma completo;
- wake word o biometría de voz;
- ejecución distribuida;
- plugins remotos;
- reentrenamiento de modelos;
- reemplazo de sherpa-onnx o llama.cpp sin evidencia de bloqueo.

## 4. Invariantes de diseño

1. Linux y ALSA continúan siendo la plataforma primaria.
2. Go sigue siendo el lenguaje principal.
3. llama-server continúa separado por HTTP.
4. STT y TTS se mantienen detrás de interfaces, aunque la implementación siga usando sherpa-onnx.
5. Ninguna herramienta mutable queda habilitada por defecto.
6. El runtime usa un solo flujo de orquestación.
7. Cada operación larga acepta `context.Context`.
8. Ningún paquete de dominio depende de `cmd/assistant`.
9. Los errores normales se devuelven; no se usa `panic` como control de flujo.
10. El refactor no avanza si el slice anterior no supera su gate.

## 5. Arquitectura objetivo

```text
cmd/assistant
    |
    v
Application
    |-- Recorder ---------> VAD
    |-- Transcriber ------> sherpa-onnx
    |-- AgentRuntime
    |      |-- LLMClient
    |      |-- ToolPolicy
    |      |-- ToolExecutor
    |      `-- ConversationMemory
    |-- Speaker ----------> sherpa-onnx / ALSA
    `-- Observability

LLMServerManager ---------> llama-server process or external endpoint
```

### Contratos principales

```go
type Recorder interface {
    RecordUntilSilence(ctx context.Context) ([]float32, error)
}

type Transcriber interface {
    Transcribe(ctx context.Context, samples []float32) (string, error)
}

type Agent interface {
    Reply(ctx context.Context, input string) (Reply, error)
}

type Speaker interface {
    Speak(ctx context.Context, text string) error
}
```

Las interfaces se introducen solo donde permiten testear lifecycle, errores y composición. No se crearán abstracciones para helpers puros que ya son fáciles de probar.

## 6. Flujo SDD

### DEFINE `/spec`

Salida obligatoria por slice:

- problema observable;
- requisito funcional o no funcional con ID;
- alcance y no alcance;
- criterios de aceptación verificables;
- escenarios de error;
- riesgos y restricciones.

No se acepta como especificación una descripción como “mejorar seguridad”. Debe expresarse como comportamiento:

> SEC-FS-002: una ruta dentro del sandbox que atraviese un enlace simbólico hacia afuera debe ser rechazada antes de abrir, crear, modificar o borrar el destino.

### PLAN `/plan`

Salida obligatoria:

- diseño mínimo;
- archivos y contratos afectados;
- decisiones y trade-offs;
- pruebas que deben fallar primero;
- estrategia de migración;
- rollback;
- dependencias con otros slices.

### BUILD `/build`

Ciclo estricto:

1. RED: escribir una prueba que reproduzca el requisito o defecto.
2. GREEN: aplicar el cambio mínimo para hacerla pasar.
3. REFACTOR: limpiar sin modificar comportamiento.
4. Ejecutar la suite focalizada después de cada paso.
5. Ejecutar gates completos antes de abrir PR.

### VERIFY `/test`

Capas:

- unitarias para lógica pura;
- integración para HTTP, filesystem, subprocessos y composición;
- security regression para rutas, red y permisos;
- E2E con fakes para el pipeline completo;
- smoke manual para ALSA y modelos reales.

### REVIEW `/review`

El review debe comprobar:

- trazabilidad requisito-prueba-código;
- ausencia de paths alternativos al runtime;
- defaults seguros;
- manejo de cancelación y cleanup;
- observabilidad sin exponer secretos;
- rollback practicable.

### SHIP `/ship`

La entrega requiere:

- versión identificable;
- artefactos reproducibles;
- instalación validada;
- servicio hardened;
- smoke tests ejecutados;
- changelog y runbook;
- rollback probado.

## 7. Slices de implementación

### PR 00 — Baseline reproducible

**Propósito:** importar el snapshot sin corregir comportamiento.

Entregables:

- código completo versionado;
- módulo renombrado a `github.com/dfc-coder/xarlatan` si corresponde;
- inventario de dependencias;
- baseline de tests y cobertura;
- CI mínima para formato, vet, test y build;
- lista explícita de fallos conocidos;
- `LICENSE` y `.gitignore` coherentes.

Tests/evidencia:

- `go test ./...`;
- `go test -cover ./...`;
- `go vet ./...`;
- `go build ./cmd/assistant ./cmd/calibrate`.

Gate: no se mezcla ninguna corrección funcional con la importación.

### PR 01 — Configuración estricta y ToolPolicy

**Problemas:** campos YAML desconocidos se ignoran, valores cero válidos se pisan y las herramientas no tienen una policy central.

Diseño:

- decoder YAML con `KnownFields(true)`;
- `Config.Validate()` separado de defaults;
- representación explícita para valores opcionales cuyo cero es válido;
- `ToolPolicy` con categorías `read`, `network` y `mutate`;
- herramientas mutables deshabilitadas por defecto;
- `fs_root` obligatorio cuando se habilita filesystem.

Pruebas rojas iniciales:

- typo YAML produce error;
- temperatura `0` se conserva;
- puerto inválido se rechaza;
- channels distinto de `1` se rechaza;
- filesystem habilitado sin root falla;
- una herramienta no permitida no se registra ni ejecuta.

Gate: el runtime no puede arrancar con configuración insegura o ambigua.

### PR 02 — Sandbox de filesystem

**Problemas:** escapes mediante symlink, borrado de raíz, paths vacíos y mutaciones amplias.

Diseño:

- resolver raíz absoluta una vez;
- canonicalización segura del parent para paths existentes y nuevos;
- verificación posterior a symlinks;
- rechazo de path vacío, `.`, raíz y destinos fuera del root;
- operaciones mutables separadas;
- escritura atómica;
- límites de tamaño para lectura y escritura;
- errores tipados.

Pruebas rojas iniciales:

- symlink interno hacia archivo externo;
- symlink interno hacia directorio externo;
- creación a través de parent symlink;
- borrado recursivo de raíz;
- path vacío;
- `..` normalizado;
- archivo mayor al límite;
- escritura parcial no deja destino corrupto.

Gate: suite de regresión de seguridad obligatoria y cobertura alta en `internal/tools`.

### PR 03 — Seguridad HTTP y SSRF

**Problemas:** `web_fetch` permite loopback, redes privadas, metadata, redirects inseguros y cuerpos ilimitados.

Diseño:

- cliente HTTP dedicado;
- solo `http` y `https`;
- resolución DNS con validación de todas las IPs;
- bloqueo de loopback, private, link-local, multicast y unspecified;
- revalidación de cada redirect;
- límites de redirects, timeout y body;
- content type permitido/configurable;
- User-Agent definido;
- errores diferenciados entre policy, red y respuesta.

Pruebas rojas iniciales:

- `127.0.0.1`, `::1`, RFC1918 y link-local rechazados;
- hostname que resuelve a IP privada rechazado;
- redirect público a privado rechazado;
- respuesta mayor al límite truncada o rechazada;
- scheme no permitido rechazado;
- timeout cancela correctamente.

Gate: ningún request se emite antes de validar el destino efectivo.

### PR 04 — Runtime agente único

**Problemas:** `main.go` implementa un loop propio y `internal/orchestrator` representa otra arquitectura; solo se soporta una ronda de herramientas.

Decisión:

- conservar el nombre `internal/orchestrator`;
- convertirlo en el runtime real;
- eliminar planners/composers placeholder que no participen del flujo real;
- mover la coordinación fuera de `main.go`;
- limitar tool rounds mediante configuración;
- preservar mensajes assistant/tool en orden;
- devolver resultado, trace y error tipado.

Pruebas rojas iniciales:

- respuesta sin tools;
- una ronda de tool;
- dos rondas encadenadas;
- límite de iteraciones;
- herramienta desconocida;
- tool error recuperable;
- cancelación durante tool;
- respuesta vacía final;
- historial conserva IDs y orden.

Gate: no existe un segundo camino de orquestación en `cmd/assistant`.

### PR 05 — Lifecycle de llama-server

**Problemas:** cleanup incompleto, health polling con bodies abiertos, errores ignorados y proceso no observado.

Diseño:

- separar `LLMClient` de `ServerManager`;
- soportar modo managed y external;
- `Start`, `WaitReady`, `Wait` y `Stop` explícitos;
- proceso inyectable para tests;
- health timeout configurable;
- cierre de body en cada intento;
- kill escalonado SIGTERM/SIGKILL;
- detección de salida prematura;
- construcción segura de requests.

Pruebas rojas iniciales:

- binary inexistente;
- health nunca ready;
- proceso termina antes del ready;
- cancelación durante startup;
- stop idempotente;
- external mode no inicia proceso;
- URL inválida devuelve error, no panic;
- body del health se cierra.

Gate: ningún subprocesso queda huérfano en tests de cancelación.

### PR 06 — Memoria acotada

**Problemas:** el “resumen” concatena texto indefinidamente y eleva contenido no confiable a mensaje system.

Decisión inicial KISS:

- eliminar la falsa compactación;
- conservar una ventana acotada por presupuesto estimado de tokens/caracteres;
- preservar pares assistant/tool completos;
- no convertir contenido conversacional en `system`;
- exponer una interfaz opcional de summarizer para una fase posterior;
- reemplazar el resumen anterior, nunca concatenarlo indefinidamente.

Pruebas rojas iniciales:

- presupuesto máximo respetado;
- system prompt único e inmutable;
- tool calls no quedan huérfanos;
- mensajes vacíos no consumen ventana;
- contenido hostil no aparece como system;
- summary replacement no crece sin límite.

Gate: el tamaño del prompt permanece acotado para conversaciones prolongadas.

### PR 07 — Audio, STT y TTS testeables

**Problemas:** configuración multicanal no soportada, subprocessos y dependencias concretas dificultan pruebas del pipeline.

Diseño:

- validar captura mono;
- hacer explícita la conversión de bytes a samples;
- interfaces solo en el borde de aplicación;
- separar playback ALSA de síntesis;
- propagar contexto a transcripción y síntesis;
- normalizar errores y métricas de latencia;
- mantener tests de VAD y pre-roll.

Pruebas rojas iniciales:

- channels != 1 rechazado;
- chunk size mono correcto;
- EOF parcial manejado;
- cancelación detiene recorder/playback;
- fallo STT no invoca agente;
- fallo de agente no invoca TTS;
- fallo TTS no rompe el siguiente ciclo.

Gate: E2E con fakes cubre el ciclo escuchar-procesar-responder.

### PR 08 — Build, instalación y servicio

**Problemas:** `make install` no garantiza llama-server, falta compose, versión no se inyecta y systemd carece de hardening.

Diseño:

- `buildVersion` como variable inyectable;
- `install` depende de todos los artefactos;
- `compose.yml` real o eliminación de targets no soportados;
- usuario y grupo dedicados;
- directorios con ownership mínimo;
- systemd con `NoNewPrivileges`, `ProtectSystem`, `ProtectHome`, `PrivateTmp` y paths explícitos;
- tools deshabilitadas en config instalada salvo opt-in;
- `--reset` implementado o eliminado;
- uninstall y rollback documentados.

Pruebas/evidencia:

- test de scripts con shellcheck y entorno temporal;
- validación de unit file;
- instalación en contenedor/VM;
- versión del binario coincide con commit/tag;
- servicio no corre como root;
- filesystem del servicio no puede escribir fuera del path permitido.

Gate: instalación limpia y rollback ejecutados en entorno descartable.

### PR 09 — Release candidate

Entregables:

- E2E automatizado con fakes;
- smoke test con hardware/modelos reales;
- matriz de compatibilidad;
- runbook de incidentes;
- guía de configuración segura;
- changelog;
- tag release candidate;
- rollback probado.

Gate final: todos los requisitos P0/P1 cerrados, ninguna excepción de seguridad abierta y evidencia de operación sostenida.

## 8. Estrategia de branches y PRs

- una rama por slice: `refactor/00-baseline`, `refactor/01-config-policy`, etc.;
- PRs pequeños y ordenados;
- squash merge para cada slice;
- ningún PR depende de cambios no publicados;
- feature flags solo cuando reduzcan riesgo de migración;
- PRs de seguridad no deben mezclarse con mejoras cosméticas.

## 9. Métricas

No se utilizará cobertura global como objetivo aislado. Se medirán:

- requisitos con prueba asociada: 100 % para P0/P1;
- regresiones de seguridad reproducidas: 100 %;
- paquetes modificados con cobertura suficiente para sus ramas críticas;
- subprocessos huérfanos después de tests: 0;
- tool rounds mayores al límite: 0;
- escrituras fuera del sandbox: 0;
- requests a redes bloqueadas: 0;
- fallos de CI en main: 0.

## 10. Criterio de finalización

El refactor se considera operativo cuando una instalación nueva puede:

1. compilar todos los artefactos;
2. validar configuración antes de arrancar;
3. iniciar llama-server o conectarse a uno externo;
4. capturar audio mono;
5. transcribir una entrada;
6. completar múltiples rondas de herramientas dentro de policy;
7. responder por TTS;
8. mantener contexto acotado;
9. detenerse sin procesos huérfanos;
10. ejecutarse como servicio sin privilegios;
11. superar los gates automáticos y el smoke test documentado.
