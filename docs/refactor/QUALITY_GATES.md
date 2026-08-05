# Gates de calidad

Los gates son acumulativos. Un PR no puede pasar a la etapa siguiente si falla un gate aplicable.

## Gate 0 — Spec ready

Antes de escribir código:

- requisito con ID;
- problema reproducible;
- criterios de aceptación observables;
- escenarios positivos, negativos y de cancelación;
- alcance y no alcance;
- trade-offs documentados;
- tests RED enumerados;
- rollback definido.

**Rechazo automático:** PR funcional sin requisito o sin prueba roja demostrable.

## Gate 1 — RED válido

La prueba inicial debe:

- fallar por la razón esperada;
- no depender de hardware real salvo smoke test explícito;
- ser determinista;
- no dormir tiempos arbitrarios cuando puede sincronizarse;
- aislar red, filesystem y subprocessos;
- tener nombre que describa comportamiento.

Evidencia requerida en el PR:

```text
command: go test ... -run '^Test...$' -count=1
expected failure: <mensaje o assertion>
```

## Gate 2 — GREEN focalizado

Antes de refactorizar:

- pasan los tests nuevos;
- pasan los tests del paquete;
- la implementación es mínima;
- no se amplía el alcance;
- no se deshabilita una comprobación existente.

Comandos:

```bash
go test -count=1 ./internal/<package>
go test -count=20 ./internal/<package>   # para concurrencia, timing o lifecycle
```

## Gate 3 — Refactor seguro

Después del cambio estructural:

```bash
gofmt -w .
gofmt -l .
go vet ./...
go test -count=1 ./...
go build ./cmd/assistant ./cmd/calibrate
```

Cuando `gofumpt` esté adoptado por CI:

```bash
gofumpt -w .
gofumpt -l .
```

No se acepta código duplicado entre `cmd/assistant` e `internal/orchestrator`.

## Gate 4 — Seguridad

Aplica a configuración, tools, red, filesystem, subprocessos e instalación.

### Filesystem

Debe demostrar:

- bloqueo de traversal;
- bloqueo de symlink escape;
- bloqueo de root deletion;
- policy read-only por defecto;
- límites de tamaño;
- escritura atómica.

### Red

Debe demostrar:

- schemes restringidos;
- bloqueo de loopback/private/link-local;
- validación DNS;
- revalidación de redirects;
- timeout y body limit.

### Subprocessos

Debe demostrar:

- cancelación;
- detección de salida prematura;
- cierre idempotente;
- ausencia de procesos huérfanos.

### Instalación

Debe demostrar:

- usuario no root;
- permisos mínimos;
- paths writeable declarados;
- hardening systemd;
- defaults de tools seguros.

## Gate 5 — Race, repetición y fugas

Para paquetes puros o testeables sin hardware:

```bash
go test -race -count=1 \
  ./internal/audio \
  ./internal/config \
  ./internal/console \
  ./internal/llm \
  ./internal/memory \
  ./internal/orchestrator \
  ./internal/tools \
  ./internal/vad
```

Para tests sensibles a lifecycle:

```bash
go test -count=50 ./internal/llm ./internal/orchestrator ./internal/tools
```

No se permite ignorar flakes. Deben corregirse o aislarse con causa documentada.

## Gate 6 — Cobertura útil

No se exige un porcentaje global artificial. Se aplican estas reglas:

- todo requisito P0/P1 tiene al menos una prueba automatizada;
- todo bug corregido conserva una prueba de regresión;
- paquetes de seguridad modificados deben cubrir ramas críticas;
- `internal/tools`, `internal/config`, `internal/llm`, `internal/memory` e `internal/orchestrator` deben alcanzar al menos 80 % de cobertura de statements al cerrar su slice, salvo exclusión justificada;
- la cobertura de `vad` y `audio` no puede caer respecto del baseline;
- código generado, glue CGo y ramas dependientes de hardware pueden excluirse solo con justificación.

Comando:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## Gate 7 — Integración

Antes de merge:

- suite completa verde;
- build de ambos comandos;
- fixtures y fakes no filtran estado;
- el runtime usa contratos reales;
- configuración de ejemplo valida;
- cambios de schema están documentados;
- migración desde config anterior está probada o documentada.

## Gate 8 — Review QA

Checklist del reviewer:

- [ ] Requisitos y tests enlazados.
- [ ] Se observó RED antes de GREEN.
- [ ] No hay paths alternativos o código muerto nuevo.
- [ ] Contexto y cancelación se propagan.
- [ ] Errores incluyen contexto sin secretos.
- [ ] Defaults son seguros.
- [ ] Logs no contienen audio, prompts completos, API keys ni contenido de archivos por defecto.
- [ ] El rollback es viable.
- [ ] Documentación y trazabilidad están actualizadas.

Para cambios P0 se requiere una revisión centrada en seguridad además de la revisión funcional.

## Gate 9 — Ship

Antes de release candidate:

```bash
make clean-all
make all
go test -count=1 ./...
go vet ./...
```

Además:

- instalación en entorno descartable;
- `systemd-analyze verify`;
- servicio ejecutado como usuario dedicado;
- smoke test de micrófono, STT, LLM, TTS y shutdown;
- operación continua según plan QA;
- rollback ejecutado;
- changelog y runbook publicados;
- tag apunta al commit validado.

## Matriz por etapa

| Etapa | Gate de entrada | Gate de salida |
|---|---|---|
| DEFINE `/spec` | problema identificado | Gate 0 |
| PLAN `/plan` | Gate 0 | diseño, tests y rollback aprobados |
| BUILD `/build` | plan aprobado | Gates 1, 2 y 3 |
| VERIFY `/test` | implementación completa | Gates 4, 5, 6 y 7 |
| REVIEW `/review` | evidencia adjunta | Gate 8 |
| SHIP `/ship` | PRs integrados | Gate 9 |

## Excepciones

Una excepción debe incluir:

- gate omitido;
- motivo técnico;
- riesgo introducido;
- mitigación temporal;
- owner;
- fecha de expiración;
- issue de seguimiento.

No se permiten excepciones para escapes de sandbox, SSRF, ejecución como root, pérdida de datos o procesos huérfanos en release.
