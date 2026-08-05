# WI-01 — Configuración estricta y ToolPolicy

Tracking: issue #4.

## DEFINE `/spec`

### Problema

El baseline acepta campos YAML desconocidos, utiliza `0` como indicador de ausencia y no valida la configuración antes de construir dependencias. Además, el registro y el executor de tools no aplican una policy central, por lo que la configuración no puede garantizar defaults de solo lectura.

### Requisitos

- **CFG-001:** un campo YAML desconocido debe producir error con contexto suficiente para localizarlo.
- **CFG-002:** valores cero válidos configurados explícitamente deben preservarse; la ausencia debe distinguirse del cero.
- **CFG-003:** la configuración debe aplicar defaults y validarse antes de construir dependencias runtime.
- **CFG-004:** solo audio mono está soportado mientras no exista mezcla explícita.
- **POL-001:** las tools mutables están denegadas por defecto.
- **POL-002:** una tool no permitida no puede registrarse ni ejecutarse.
- **POL-003:** habilitar filesystem requiere un root absoluto y válido.

### Alcance

- separar `ApplyDefaults` de `Validate`;
- usar decoder YAML con `KnownFields(true)`;
- representar ausencia de valores donde cero sea válido;
- introducir una `ToolPolicy` inyectada en registry y executor;
- mantener tools mutables deshabilitadas por defecto;
- validar configuración antes de registrar tools.

### No alcance

- resolver symlinks o confinamiento de filesystem: WI-02;
- bloquear SSRF: WI-03;
- cambiar el loop del agente: WI-04;
- modificar systemd o instalación: WI-08.

### Criterios de aceptación

1. Un typo YAML falla antes de construir audio, LLM o tools.
2. `temperature: 0` permanece en cero, mientras una temperatura omitida recibe el default.
3. Puertos fuera de `1..65535` producen error.
4. `audio.channels != 1` produce error.
5. Filesystem habilitado sin root absoluto produce error.
6. `fs_write`, `fs_delete` y `fs_mkdir` no se registran ni ejecutan con policy default.
7. Una denegación de policy produce un resultado distinguible de “tool inexistente”.

## PLAN `/plan`

### Slice A — Decoder estricto y valores explícitos

Tests RED:

- `TestLoadRejectsUnknownYAMLField`.
- `TestLoadPreservesExplicitZeroTemperature`.

Cambio mínimo posterior:

- reemplazar `yaml.Unmarshal` por `yaml.Decoder` + `KnownFields(true)`;
- separar el modelo YAML de la configuración runtime o usar campos opcionales para distinguir ausencia de cero.

### Slice B — Defaults y validación

Tests RED previstos:

- `TestLoadAppliesDefaultTemperatureWhenOmitted`.
- `TestValidateRejectsInvalidLLMPort`.
- `TestValidateRejectsNonMonoAudio`.
- `TestValidateRequiresAbsoluteFSRootWhenFilesystemEnabled`.

### Slice C — ToolPolicy

Tests RED previstos:

- `TestToolPolicyDeniesMutationByDefault`.
- `TestRegistryContainsOnlyAllowedTools`.
- `TestExecutorRejectsDeniedTool`.

### Rollback

Revertir los commits de WI-01. No hay migración de datos; los cambios afectan parsing, validación y registro de tools.
