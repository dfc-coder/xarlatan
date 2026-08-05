# Xarlatan

Asistente de voz local en Go para Linux, basado en ALSA, sherpa-onnx y llama.cpp.

> Estado actual: el repositorio remoto fue inicializado para alojar el plan. El snapshot de código analizado todavía debe importarse y congelarse como baseline antes de iniciar cualquier refactor funcional.

## Modelo de entrega

```text
  DEFINE          PLAN           BUILD          VERIFY         REVIEW          SHIP
 ┌──────┐      ┌──────┐      ┌──────┐      ┌──────┐      ┌──────┐      ┌──────┐
 │ Idea │ ───▶ │ Spec │ ───▶ │ Code │ ───▶ │ Test │ ───▶ │  QA  │ ───▶ │  Go  │
 │Refine│      │  PRD │      │ Impl │      │Debug │      │ Gate │      │ Live │
 └──────┘      └──────┘      └──────┘      └──────┘      └──────┘      └──────┘
  /spec          /plan          /build        /test         /review       /ship
```

Cada cambio debe entrar con un requisito identificable y salir con evidencia verificable:

```text
Requirement ID -> Spec -> Failing test -> Minimal implementation -> Refactor
               -> Verification evidence -> Review gate -> Release evidence
```

## Documentos operativos

- [Plan maestro de refactorización](docs/refactor/PLAN.md)
- [Backlog y secuencia de PRs](docs/refactor/WORK_ITEMS.md)
- [Matriz de trazabilidad](docs/refactor/TRACEABILITY.md)
- [Gates de calidad](docs/refactor/QUALITY_GATES.md)
- [Reglas para agentes y colaboradores](AGENTS.md)

## Principios

1. No se reescribe el proyecto: se corrige mediante slices verticales pequeños.
2. Ningún cambio funcional comienza sin especificación y criterio de aceptación.
3. Todo bug confirmado se reproduce primero con una prueba roja.
4. Las herramientas mutables permanecen deshabilitadas hasta completar el hardening.
5. Los tests del orquestador deben representar el runtime real, no una arquitectura paralela.
6. La seguridad del filesystem, red y servicio systemd es requisito funcional, no documentación opcional.
7. Cada PR debe ser reversible de forma independiente.

## Secuencia prevista

| Orden | Slice | Resultado |
|---:|---|---|
| 00 | Baseline | Código importado, reproducible y medido |
| 01 | Configuración y policy | Config estricta y herramientas seguras por defecto |
| 02 | Filesystem sandbox | Sin escapes, borrado de raíz ni mutaciones implícitas |
| 03 | Web security | Cliente HTTP protegido contra SSRF y respuestas ilimitadas |
| 04 | Runtime agente | Un único orquestador con múltiples tool rounds acotados |
| 05 | Lifecycle LLM | Inicio, health, cancelación y cierre deterministas |
| 06 | Memoria | Contexto realmente acotado y semántica segura |
| 07 | Audio/STT/TTS | Contratos testeables y captura mono validada |
| 08 | Instalación | Servicio sin privilegios, build e instalación consistentes |
| 09 | Release candidate | E2E, smoke tests, runbook y rollback validados |

## Definition of Done global

Un slice solo puede marcarse terminado cuando:

- sus criterios de aceptación están cubiertos por tests automatizados;
- `go test`, `go vet`, formato, build y gates específicos pasan;
- no reduce la seguridad ni habilita permisos más amplios por defecto;
- actualiza especificación, trazabilidad y documentación operativa;
- incluye procedimiento de rollback;
- fue revisado contra [QUALITY_GATES.md](docs/refactor/QUALITY_GATES.md).
