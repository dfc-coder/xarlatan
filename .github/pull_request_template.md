## Slice

- Work item: WI-__
- Requirement IDs:
- Spec/plan:

## DEFINE `/spec`

- Problema observable:
- Alcance:
- No alcance:
- Criterios de aceptación:

## PLAN `/plan`

- Diseño aplicado:
- Trade-offs:
- Migración:
- Rollback:

## BUILD `/build`

### RED

```text
command:
expected failure:
actual failure:
```

### GREEN

```text
command:
result:
```

### REFACTOR

- Cambios estructurales sin cambio de comportamiento:

## VERIFY `/test`

- [ ] `gofmt -l .`
- [ ] `go vet ./...`
- [ ] `go test -count=1 ./...`
- [ ] `go build ./cmd/assistant ./cmd/calibrate`
- [ ] race/repetition cuando aplica
- [ ] security regression cuando aplica
- [ ] integración/E2E cuando aplica
- [ ] cobertura revisada

Evidencia:

```text
<commands and relevant output>
```

## REVIEW `/review`

- [ ] `TRACEABILITY.md` actualizado.
- [ ] No se habilitan permisos más amplios por defecto.
- [ ] Contexto y cancelación se propagan.
- [ ] No se introducen paths alternativos al runtime.
- [ ] Logs no exponen secretos o contenido sensible.
- [ ] Documentación y config de ejemplo actualizadas.
- [ ] Desviaciones del plan declaradas.

## SHIP `/ship`

- Riesgo de despliegue:
- Procedimiento de rollback:
- Evidencia de smoke/install, cuando aplica:

## Notas para reviewers

Áreas que requieren atención específica:
