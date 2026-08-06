# WI-02 — Sandbox de filesystem

Tracking: issue #5.

## DEFINE `/spec`

### Problema observable

El helper actual solo limpia y concatena paths. Un path aparentemente interno puede atravesar un symlink hacia un archivo o directorio externo. Además, `""` y `"."` resuelven a la raíz del sandbox, por lo que una operación recursiva puede borrar todo el root. Las escrituras truncan el destino directamente y las lecturas/escrituras no tienen límites globales de policy.

### Requisitos

- **SEC-FS-001:** `..`, paths absolutos y normalización no pueden escapar del root.
- **SEC-FS-002:** un symlink de archivo o directorio que apunte fuera debe rechazarse antes de abrir, listar, modificar o borrar.
- **SEC-FS-003:** la creación de un archivo o directorio no puede atravesar un parent symlink externo.
- **SEC-FS-004:** la raíz del sandbox nunca puede borrarse.
- **SEC-FS-005:** paths vacíos o equivalentes a raíz deben rechazarse para mutaciones.
- **SEC-FS-006:** una escritura de reemplazo debe ser atómica mediante temporal y rename dentro del mismo directorio.
- **SEC-FS-007:** lectura y escritura deben respetar límites configurados antes de reservar o consumir payloads excesivos.
- **SEC-FS-008:** las mutaciones siguen requiriendo `ToolPolicy`; el sandbox no reemplaza autorización.

### Alcance

- resolver y conservar el root absoluto/canónico al construir herramientas;
- validar containment después de resolver symlinks;
- validar el parent existente para destinos nuevos;
- separar resolución para lectura, creación y mutación destructiva;
- proteger root, vacío y `.`;
- escribir mediante archivo temporal, sync, close y rename;
- introducir límites por tool/config;
- devolver errores tipados o identificables para policy, path y tamaño.

### No alcance

- hardening HTTP/SSRF: WI-03;
- confirmaciones humanas para acciones destructivas: WI-13;
- permisos de systemd y paths del servicio: WI-08;
- soporte multiplataforma completo. Linux continúa siendo la plataforma primaria.

### Criterios de aceptación

1. Ninguna tool puede seguir un symlink interno hacia afuera.
2. Un destino nuevo bajo un parent symlink externo es rechazado.
3. `fs_delete` rechaza `""`, `"."`, la ruta absoluta del root y equivalentes normalizados.
4. Traversal mediante `..` o path absoluto externo es rechazado.
5. Reemplazar un archivo no expone contenido parcial si la escritura falla.
6. Lecturas y escrituras mayores al límite fallan antes de procesar el contenido completo.
7. El modo read-only continúa rechazando `fs_write`, `fs_delete` y `fs_mkdir`.
8. La suite completa, vet y build permanecen verdes al finalizar.

## PLAN `/plan`

### Slice A — Boundary resolver y protección de raíz

Tests RED:

- `TestSafePathRejectsSymlinkEscape`;
- `TestSafePathRejectsParentSymlinkForNewFile`;
- `TestDeleteRejectsSandboxRoot`;
- `TestDeleteRejectsEmptyPath`.

Cambio mínimo posterior:

- introducir un `Sandbox` construido desde un root absoluto existente;
- resolver symlinks para destinos existentes;
- resolver el parent existente para destinos nuevos;
- comprobar containment con `filepath.Rel`, no con prefijos de strings;
- rechazar root y paths vacíos para mutaciones.

### Slice B — Cobertura de todas las operaciones

Tests RED previstos:

- read/stat/list sobre symlink externo;
- write/mkdir/delete mediante parent symlink;
- path absoluto externo;
- traversal normalizado;
- symlink interno que permanece dentro del root;
- errores de symlink roto y parent inexistente.

Cambio mínimo posterior:

- todas las tools usan el mismo resolver;
- operaciones read y mutate declaran su intención;
- no quedan llamadas directas a `safePath` legacy.

### Slice C — Escritura atómica

Tests RED previstos:

- `TestWriteIsAtomic`;
- fallo antes de rename conserva el destino anterior;
- el temporal se elimina después de error;
- append mantiene semántica explícita o se implementa mediante reemplazo seguro.

Cambio mínimo posterior:

- temporal en el mismo directorio;
- permisos definidos;
- `Sync`, `Close`, `Rename` y cleanup determinista.

### Slice D — Límites y policy

Tests RED previstos:

- `TestReadRejectsOversizedFile`;
- `TestWriteRejectsOversizedPayload`;
- límites inválidos rechazados en configuración;
- `TestReadOnlyPolicyRejectsWriteDeleteAndMkdir` como regresión.

Cambio mínimo posterior:

- `max_read_bytes` y `max_write_bytes` validados;
- chequeo por metadata y lectores limitados;
- rechazo antes de crear buffers/payloads excesivos.

## Riesgos y decisiones

- `EvalSymlinks` por sí solo no funciona para destinos que aún no existen; se debe resolver el parent existente.
- comprobar prefijos de strings es incorrecto para roots como `/tmp/root` y `/tmp/root-other`; se usará `filepath.Rel`.
- no se intentará resolver ataques TOCTOU de un actor local malicioso con privilegios equivalentes mediante una abstracción puramente path-based. El servicio hardened de WI-08 reducirá ese riesgo; el resolver debe evitar los escapes reproducibles del modelo y configuración.
- no se habilita filesystem por defecto durante WI-02.

## Rollback

Revertir los commits de WI-02. No hay migraciones persistentes. El rollback restaura el filesystem deshabilitado por defecto de WI-01.