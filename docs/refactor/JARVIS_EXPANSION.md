# Fase 2 — Expansión hacia un asistente tipo Jarvis

## Propósito

Esta fase comienza después de completar WI-09 y obtener un release base seguro, reproducible y reversible. No reemplaza el programa WI-00..WI-09: lo extiende desde un asistente de voz local reactivo hacia un asistente continuo, interrumpible, proactivo, persistente y multimodal.

La arquitectura continúa siendo un monolito modular en Go. Los procesos externos se reservan para inferencia y adaptadores que realmente requieran aislamiento.

## Invariantes

1. WI-00..WI-09 deben permanecer verdes antes de iniciar esta fase.
2. Captura continua no significa almacenamiento continuo: el audio crudo se descarta salvo consentimiento y necesidad explícita.
3. Toda operación larga acepta `context.Context` y puede cancelarse.
4. Las acciones sensibles requieren confirmación según policy.
5. Skills, modelos e integraciones no pueden eludir `ToolPolicy`.
6. La proactividad se basa en eventos y reglas explícitas, no en loops LLM sin límite.
7. La memoria persistente distingue hechos confirmados, episodios, tareas y preferencias.
8. El modo local continúa siendo el default; proveedores remotos son opt-in.

## Arquitectura objetivo de Fase 2

```text
Audio continuo -> Wake word/VAD -> Speech streaming -> AgentRuntime
       ^                                           |
       |                                           v
Barge-in <- Playback/TTS streaming <- ModelRouter/Skills
                                                    |
                 +----------------+-----------------+----------------+
                 |                |                                  |
              Memory          ToolPolicy                         Event Bus
                 |                |                                  |
              SQLite       Desktop/Calendar/IoT                   Scheduler
                                                                     |
                                                               Notifications
```

## WI-10 — Captura continua y wake word

**Prioridad:** P1  
**Dependencias:** WI-07, WI-08, WI-09

### Objetivo

Mantener una fuente de audio estable con ring buffer, wake word y estados explícitos sin reiniciar `arecord` por turno.

### Entregables

- `AudioSource` continuo y cancelable;
- ring buffer con pre-roll configurable;
- detector de wake word detrás de interfaz;
- estados `idle`, `wake_detected`, `listening` y `processing`;
- métricas de falsos positivos, falsos negativos y latencia;
- política de privacidad para descarte de audio.

### Gate

Una prueba prolongada no filtra goroutines, descriptores ni memoria, y el audio anterior al wake word solo se conserva dentro del buffer acotado.

---

## WI-11 — STT/TTS streaming y barge-in

**Prioridad:** P1  
**Dependencias:** WI-10

### Objetivo

Reducir latencia percibida y permitir que el usuario interrumpa al asistente mientras habla.

### Entregables

- eventos STT parciales y finales;
- salida TTS por chunks/frases;
- playback cancelable;
- detección de voz durante playback;
- transición `speaking -> interrupted -> listening`;
- métricas `stt_first_partial_ms`, `tts_first_audio_ms` e `interrupt_latency_ms`.

### Gate

El usuario puede interrumpir una respuesta y comenzar un nuevo turno sin audio residual ni procesos huérfanos.

---

## WI-12 — Model router

**Prioridad:** P1  
**Dependencias:** WI-04, WI-05, WI-09

### Objetivo

Seleccionar el modelo mínimo suficiente según tarea, latencia, privacidad y disponibilidad.

### Entregables

- contratos para chat, clasificación, embeddings y visión;
- rutas `fast`, `reasoning`, `embedding` y `vision`;
- fallback explícito y acotado;
- modo local por defecto;
- presupuesto de tokens, tiempo y memoria por request;
- trazas de selección sin secretos.

### Gate

La selección es determinista para reglas conocidas, observable y no reintenta indefinidamente entre modelos.

---

## WI-13 — Skills y confirmaciones

**Prioridad:** P0 para acciones sensibles  
**Dependencias:** WI-04, WI-12

### Objetivo

Separar operaciones atómicas de workflows de negocio y exigir confirmación proporcional al riesgo.

### Entregables

- interfaz `Skill` y registry;
- skills deterministas que coordinan tools;
- clasificación `read`, `write_reversible`, `write_sensitive`, `destructive`, `prohibited`;
- contratos de preview, confirmación, expiración y cancelación;
- idempotency keys para acciones repetibles;
- auditoría de decisión y ejecución.

### Gate

Ninguna acción sensible o destructiva se ejecuta sin confirmación válida y específica para sus argumentos efectivos.

---

## WI-14 — Memoria persistente

**Prioridad:** P1  
**Dependencias:** WI-06, WI-09

### Objetivo

Recordar información útil sin convertir toda conversación en memoria permanente.

### Entregables

- almacenamiento SQLite;
- working memory, conversation window, facts, episodic memory, user profile y task state;
- schema versionado y migraciones reversibles;
- confirmación y provenance para hechos personales;
- TTL y borrado selectivo;
- búsqueda léxica inicial; embeddings solo con evidencia de necesidad.

### Gate

El usuario puede inspeccionar, corregir y borrar memoria; ningún contenido sensible se persiste implícitamente sin una regla documentada.

---

## WI-15 — Event bus y scheduler

**Prioridad:** P1  
**Dependencias:** WI-13, WI-14

### Objetivo

Habilitar tareas pendientes y comportamiento proactivo sin polling LLM permanente.

### Entregables

- bus de eventos tipado;
- scheduler persistente;
- retries con backoff y dead-letter state;
- deduplicación e idempotencia;
- suscripciones explícitas por skill;
- límites de frecuencia y quiet hours;
- recuperación después de reinicio.

### Gate

Un reinicio no duplica acciones y un evento no puede producir una cascada ilimitada.

---

## WI-16 — Integraciones de escritorio, calendario e IoT

**Prioridad:** P1  
**Dependencias:** WI-13, WI-15

### Objetivo

Conectar capacidades útiles mediante adaptadores con permisos mínimos.

### Entregables

- adaptadores separados para desktop, calendario e IoT;
- discovery explícito de capacidades;
- scopes mínimos y credenciales fuera de logs/config versionada;
- modo simulación para escritura;
- health y reconexión;
- contratos de compensación cuando una acción reversible falla parcialmente.

### Gate

Cada integración puede deshabilitarse de forma independiente y ninguna obtiene permisos superiores a los necesarios para sus skills habilitadas.

---

## WI-17 — Multimodalidad

**Prioridad:** P2  
**Dependencias:** WI-12, WI-13, WI-14

### Objetivo

Incorporar imagen, pantalla y documentos como entradas controladas por sesión.

### Entregables

- contratos `ImageInput`, `ScreenCapture` y `DocumentInput`;
- consentimiento visible para cámara y pantalla;
- límites de resolución, tamaño y frecuencia;
- redacción de secretos antes de persistencia o envío remoto;
- router hacia VLM local/remoto según policy;
- provenance de observaciones multimodales.

### Gate

Cámara y pantalla permanecen apagadas por defecto; cada captura es observable, cancelable y atribuible a una solicitud o evento autorizado.

## Dependencias

```text
WI-09
  |-- WI-10 --> WI-11
  |-- WI-12 --> WI-13 --> WI-15 --> WI-16
  |       |         |        ^
  |       |         +--> WI-14
  |       +---------------------> WI-17
  +------------------> WI-14
```

WI-10/WI-11 pueden avanzar en paralelo con WI-12/WI-14 después de WI-09. WI-13 debe preceder cualquier integración con escritura real. WI-15 debe preceder proactividad y tareas persistentes.

## Definition of Done de Fase 2

- conversación continua con wake word y barge-in;
- latencias por etapa observables;
- routing de modelos acotado y local-first;
- acciones sensibles con preview y confirmación;
- memoria persistente inspeccionable y borrable;
- scheduler recuperable e idempotente;
- integraciones con scopes mínimos;
- entradas multimodales opt-in;
- tests unitarios, integración, seguridad, soak y rollback por work item.
