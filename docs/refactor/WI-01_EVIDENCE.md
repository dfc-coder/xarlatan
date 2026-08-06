# Evidencia WI-01 — Configuración estricta y ToolPolicy

Tracking: issue #4 · PR #14.

## Trazabilidad

| Requirement | Implementación | Tests |
|---|---|---|
| CFG-001 | `yaml.Decoder.KnownFields(true)` | `TestLoadRejectsUnknownYAMLField` |
| CFG-002 | defaults condicionados por presencia en `yaml.Node` | `TestLoadPreservesExplicitZeroTemperature`, `TestLoadAppliesDefaultTemperatureWhenOmitted` |
| CFG-003 | `Config.Validate()` invocado antes de dependencias | tests de validación + revisión de `cmd/assistant/main.go` |
| CFG-004 | validación `channels == 1` | `TestValidateRejectsNonMonoAudio` |
| POL-001 | `DefaultToolPolicy` y config segura | `TestDefaultToolPolicyDeniesMutation`, tests de `buildRegistry` |
| POL-002 | policy aplicada en registry y executor | `TestRegistryContainsOnlyAllowedTools`, `TestExecutorRejectsDeniedTool` |
| POL-003 | validación de root absoluto/existente/directorio/no raíz | `TestValidateRequiresAbsoluteFSRootWhenFilesystemEnabled`, `TestValidateAcceptsOperationalConfig` |

## RED

La primera ejecución falló exclusivamente porque:

- `llm.temperatur` era ignorado;
- `temperature: 0` terminaba convertido en `0.7`.

## GREEN

El cierre requiere evidencia automática de:

```bash
gofmt -l .
go vet ./...
go test -count=1 -coverprofile=coverage.out ./...
go build ./cmd/assistant ./cmd/calibrate
```

También debe pasar `baseline inventory`, que comprueba la presencia de las suites de configuración, policy e integración.

## Cambios incompatibles

El esquema anterior:

```yaml
tools:
  fs_root: ""
```

se reemplaza por:

```yaml
tools:
  filesystem:
    enabled: false
    root: ""
    allow_mutations: false
```

No existe migración automática para evitar interpretar una configuración ambigua como permiso de filesystem.

## Riesgos residuales

WI-01 controla exposición y autorización, pero no corrige la resolución física de rutas. Hasta completar WI-02, filesystem debe permanecer deshabilitado fuera de pruebas controladas.

## Rollback

Revertir el merge de WI-01 y restaurar el `config.yaml` anterior. No hay migraciones persistentes ni cambios de datos.
