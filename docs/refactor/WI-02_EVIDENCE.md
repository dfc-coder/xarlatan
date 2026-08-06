# WI-02 — Evidencia de sandbox de filesystem

Tracking: issue #5, PR #24.

## DEFINE

La especificación `WI-02_SPEC.md` define SEC-FS-001..008: containment léxico, resolución de symlinks, validación de parents para destinos nuevos, protección de raíz, escrituras atómicas, límites de payload y preservación de `ToolPolicy`.

## PLAN

El trabajo se ejecutó en cuatro slices:

1. resolver canónico y protección de raíz;
2. adopción del resolver por las seis tools;
3. escritura y append mediante reemplazo atómico;
4. límites configurables y regresión de policy read-only.

## BUILD — RED

El primer estado RED reprodujo cuatro vulnerabilidades existentes:

- `TestSafePathRejectsSymlinkEscape`: un symlink interno podía apuntar a un archivo externo;
- `TestSafePathRejectsParentSymlinkForNewFile`: un destino nuevo podía atravesar un parent symlink externo;
- `TestDeleteRejectsSandboxRoot`: `path: "."` podía eliminar la raíz;
- `TestDeleteRejectsEmptyPath`: `path: ""` podía eliminar la raíz.

La matriz se amplió antes del GREEN para cubrir traversal, paths absolutos, todas las tools, symlinks internos válidos, límites, atomicidad y policy.

## BUILD — GREEN

### Boundary resolver

- `FilesystemSandbox` conserva un root absoluto y canónico.
- `filepath.Rel` verifica containment léxico.
- `EvalSymlinks` verifica containment efectivo para destinos existentes.
- Los destinos nuevos resuelven el parent existente más cercano.
- Mutaciones rechazan vacío, `.`, raíz y symlinks finales.
- Symlinks que resuelven dentro del sandbox permanecen disponibles para operaciones de lectura.
- Los errores de boundary y policy tienen códigos identificables.

### Operaciones

`fs_read`, `fs_list`, `fs_stat`, `fs_write`, `fs_delete` y `fs_mkdir` comparten la misma instancia del sandbox construida por el runtime.

### Atomicidad

- temporal en el mismo directorio;
- permisos definidos;
- `Write`, `Sync`, `Close` y `Rename`;
- cleanup del temporal en errores;
- sync del directorio cuando está disponible;
- append implementado como reemplazo atómico del contenido completo.

### Límites

La configuración incorpora:

```yaml
tools:
  filesystem:
    max_read_bytes: 1048576
    max_write_bytes: 1048576
```

Ambos valores tienen default de 1 MiB y deben ser positivos cuando filesystem está habilitado.

## VERIFY

Head funcional verificado: `bfb74b806c3f710b1a848b61b89d44468bf12848`.

- `baseline inventory`: success;
- `gofmt -l .`: success;
- `go vet ./...`: success;
- `go test -count=1 -coverprofile=coverage.out ./...`: success;
- `go build ./cmd/assistant ./cmd/calibrate`: success;
- artefacto `coverage`: publicado, digest `sha256:5dc9ed9be827df2e820029c2bf441069758446e7767c92ec4a079de376c65fdf`.

## Matriz de regresión

- traversal mediante `..`;
- path absoluto externo;
- symlink de archivo externo;
- symlink interno que permanece dentro del root;
- parent symlink para destino nuevo;
- read/stat/list/write/mkdir/delete mediante escape;
- mutación de raíz mediante vacío o `.`;
- archivo de lectura mayor al límite;
- payload de escritura mayor al límite;
- conservación del destino anterior cuando falla el rename;
- eliminación del temporal después de error;
- mutaciones rechazadas por policy read-only.

## REVIEW

- filesystem continúa deshabilitado por defecto;
- `allow_mutations` continúa siendo un opt-in independiente;
- el sandbox no introduce una ruta alternativa alrededor de `ToolPolicy`;
- no se agregaron dependencias externas;
- el cambio permanece dentro de config, tools, composition root, tests, documentación y diagnóstico de CI.

## Riesgo residual

Una abstracción basada en paths no elimina por sí sola ataques TOCTOU de un actor local concurrente con permisos equivalentes que modifique el árbol durante una operación. WI-08 debe reducir ese riesgo mediante usuario dedicado, permisos mínimos y aislamiento de systemd.

## Rollback

Revertir el squash merge de PR #24. No existen migraciones persistentes. El rollback vuelve al estado de WI-01, donde filesystem permanece deshabilitado por defecto.
