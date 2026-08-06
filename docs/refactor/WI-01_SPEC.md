# WI-01 — Configuración estricta y ToolPolicy

Tracking: issue #4.

## DEFINE `/spec`

### Problema

El baseline aceptaba campos YAML desconocidos, utilizaba `0` como indicador de ausencia y no validaba la configuración antes de construir dependencias. El registro y el executor tampoco aplicaban una policy central, por lo que la configuración no podía garantizar defaults de solo lectura.

### Requisitos

- **CFG-001:** un campo YAML desconocido produce error con el nombre del campo.
- **CFG-002:** un cero configurado explícitamente se preserva; un campo omitido recibe su default.
- **CFG-003:** defaults y validación ocurren antes de construir dependencias runtime.
- **CFG-004:** solo audio mono está soportado hasta implementar mezcla explícita.
- **POL-001:** las tools mutables están denegadas por defecto.
- **POL-002:** una tool no permitida no puede registrarse ni ejecutarse.
- **POL-003:** habilitar filesystem requiere un root absoluto, existente, de tipo directorio y distinto de la raíz del volumen.

### Alcance

- decoder YAML estricto con `KnownFields(true)`;
- detección de presencia de campos para distinguir omisión y cero;
- `Config.Validate()` para estructura, rangos, providers y recursos runtime;
- esquema `tools.filesystem` explícito;
- `ToolPolicy` allowlist aplicada en registry y executor;
- filesystem deshabilitado por defecto y mutaciones opt-in;
- validación en `main` antes de STT, LLM, TTS y tools.

### No alcance

- resolver symlinks o confinamiento efectivo de rutas: WI-02;
- bloquear SSRF: WI-03;
- cambiar el loop del agente: WI-04;
- modificar systemd o instalación: WI-08.

### Criterios de aceptación

1. Un typo YAML falla antes de construir audio, LLM o tools.
2. `temperature: 0` permanece en cero y una temperatura omitida recibe `0.7`.
3. Puertos fuera de `1..65535` producen error.
4. `audio.channels != 1` produce error.
5. Los recursos STT/LLM/TTS inexistentes se rechazan antes de iniciar el pipeline.
6. Filesystem deshabilitado no expone tools de archivos.
7. Filesystem habilitado expone solo lectura salvo opt-in explícito de mutaciones.
8. Una denegación de policy es distinguible de “tool inexistente”.

## PLAN `/plan`

### Slice A — Decoder estricto y valores explícitos

- `TestLoadRejectsUnknownYAMLField`.
- `TestLoadPreservesExplicitZeroTemperature`.
- `TestLoadAppliesDefaultTemperatureWhenOmitted`.

### Slice B — Defaults y validación

- `TestValidateRejectsInvalidLLMPort`.
- `TestValidateRejectsNonMonoAudio`.
- `TestValidateRequiresAbsoluteFSRootWhenFilesystemEnabled`.
- `TestValidateRejectsMissingRuntimeFiles`.
- `TestValidateAcceptsOperationalConfig`.
- `TestValidateRejectsUnknownWebProvider`.

### Slice C — ToolPolicy e integración

- `TestDefaultToolPolicyDeniesMutation`.
- `TestRegistryContainsOnlyAllowedTools`.
- `TestExecutorRejectsDeniedTool`.
- `TestExecutorKeepsUnknownDistinctFromDenied`.
- `TestBuildRegistryDefaultExposesOnlyWebTools`.
- `TestBuildRegistryReadOnlyFilesystem`.
- `TestBuildRegistryAllowsMutationsOnlyWhenExplicit`.

## BUILD `/build`

- `Config.Load` usa decoder estricto y rechaza documentos YAML múltiples.
- Los defaults consultan presencia en el árbol YAML antes de reemplazar valores numéricos.
- `Config.Validate` valida rangos, providers, paths y ejecutabilidad.
- `ToolPolicy` es una allowlist inmutable.
- Registry aplica policy al registrar y recuerda nombres denegados.
- Executor diferencia policy denial de tool desconocida.
- `main` valida primero y construye el registry mediante policy derivada de config.

## Rollback

Revertir el merge de WI-01. No hay migración de datos; el único cambio incompatible es el esquema YAML de filesystem.
