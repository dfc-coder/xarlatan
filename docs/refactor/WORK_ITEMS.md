# Backlog de refactorización

Este backlog define el orden de implementación. Cada item se ejecuta como un PR independiente y debe actualizar la matriz de trazabilidad.

## Reglas de ejecución

- No comenzar un item si sus dependencias no están en `main`.
- El primer commit funcional de cada PR debe contener tests rojos.
- Separar commits `test`, `feat/fix` y `refactor` cuando ayude al review.
- No habilitar herramientas mutables antes de cerrar WI-01 y WI-02.
- No modificar el runtime principal antes de congelar el baseline.
- Los defectos descubiertos fuera de alcance se registran; no se incorporan silenciosamente.

## WI-00 — Importar y congelar baseline

**Prioridad:** P0  
**Dependencias:** ninguna

### Entregables

- Importar el snapshot completo.
- Cambiar el módulo `github.com/local/assistant` por `github.com/dfc-coder/xarlatan`.
- Confirmar qué archivos son fuente, generados, modelos y vendor.
- Agregar `LICENSE`.
- Resolver o documentar `compose.yml` faltante sin alterar todavía el runtime.
- Crear workflow CI inicial.
- Registrar baseline de cobertura por paquete.
- Corregir `AGENTS.md` para reconocer los tests existentes.

### Evidencia

```bash
gofmt -l .
go vet ./...
go test -count=1 ./...
go test -coverprofile=coverage.out ./...
go build ./cmd/assistant ./cmd/calibrate
```

### Criterio de salida

El código analizado existe en GitHub, compila en el entorno soportado o sus bloqueos externos están documentados con reproducción exacta.

---

## WI-01 — Config estricta y policy de herramientas

**Prioridad:** P0  
**Dependencias:** WI-00

### Requisitos

- CFG-001, CFG-002, CFG-003, CFG-004.
- POL-001, POL-002, POL-003.

### Tests RED

- `TestLoadRejectsUnknownYAMLField`.
- `TestLoadPreservesExplicitZeroTemperature`.
- `TestValidateRejectsInvalidLLMPort`.
- `TestValidateRejectsNonMonoAudio`.
- `TestValidateRequiresFSRootWhenFilesystemEnabled`.
- `TestToolPolicyDeniesMutationByDefault`.
- `TestRegistryContainsOnlyAllowedTools`.

### Cambio mínimo

- `Config.ApplyDefaults()` y `Config.Validate()` separados.
- Decoder YAML estricto.
- Campos opcionales explícitos cuando cero sea válido.
- `ToolPolicy` inyectada al registro y executor.
- Config instalada en modo read-only/no-tools por defecto.

### Criterio de salida

No existe una combinación de configuración por defecto que otorgue escritura o borrado al modelo.

---

## WI-02 — Confinamiento de filesystem

**Prioridad:** P0  
**Dependencias:** WI-01

### Requisitos

- SEC-FS-001 a SEC-FS-008.

### Tests RED

- `TestSafePathRejectsSymlinkEscape`.
- `TestSafePathRejectsParentSymlinkForNewFile`.
- `TestDeleteRejectsSandboxRoot`.
- `TestDeleteRejectsEmptyPath`.
- `TestWriteIsAtomic`.
- `TestReadRejectsOversizedFile`.
- `TestWriteRejectsOversizedPayload`.
- `TestReadOnlyPolicyRejectsWriteDeleteAndMkdir`.

### Cambio mínimo

- Resolver root absoluto al construir la tool.
- Canonicalizar destino o parent existente.
- Verificar containment después de resolver symlinks.
- Bloquear raíz, vacío y operaciones fuera de policy.
- Escritura temporal + rename.
- Límites configurables.

### Criterio de salida

La suite demuestra que ninguna operación expuesta puede leer o mutar fuera del root configurado.

---

## WI-03 — HTTP seguro y SSRF

**Prioridad:** P0  
**Dependencias:** WI-01

### Requisitos

- SEC-NET-001 a SEC-NET-008.

### Tests RED

- `TestFetchRejectsLoopback`.
- `TestFetchRejectsPrivateIPv4`.
- `TestFetchRejectsPrivateIPv6`.
- `TestFetchRejectsLinkLocalMetadata`.
- `TestFetchRejectsDNSResolvingToPrivateIP`.
- `TestFetchRejectsRedirectToPrivateIP`.
- `TestFetchRejectsUnsupportedScheme`.
- `TestFetchEnforcesBodyLimit`.
- `TestFetchHonorsContextCancellation`.

### Cambio mínimo

- `SafeHTTPClient` con resolver y dial controlados.
- Revalidación de redirects.
- Timeouts y body limit.
- Content types definidos.

### Criterio de salida

Las herramientas web no pueden utilizarse como proxy hacia servicios locales o privados.

---

## WI-04 — Unificar AgentRuntime

**Prioridad:** P1  
**Dependencias:** WI-01, WI-02, WI-03

### Requisitos

- AGT-001 a AGT-009.

### Tests RED

- `TestAgentReturnsDirectReply`.
- `TestAgentExecutesOneToolRound`.
- `TestAgentExecutesMultipleToolRounds`.
- `TestAgentStopsAtConfiguredRoundLimit`.
- `TestAgentPreservesToolCallOrdering`.
- `TestAgentReturnsUnknownToolError`.
- `TestAgentContinuesAfterRecoverableToolError`.
- `TestAgentCancelsDuringToolExecution`.
- `TestApplicationUsesOrchestratorOnly`.

### Cambio mínimo

- Convertir `internal/orchestrator` en `AgentRuntime`.
- Mover el loop fuera de `main.go`.
- Eliminar planners/composers no conectados.
- Generar `Trace` por round.

### Criterio de salida

Existe un solo camino para LLM + tools y soporta múltiples rondas acotadas.

---

## WI-05 — Lifecycle del LLM

**Prioridad:** P1  
**Dependencias:** WI-00

### Requisitos

- LLM-001 a LLM-009.

### Tests RED

- `TestServerManagerFailsForMissingBinary`.
- `TestServerManagerDetectsEarlyExit`.
- `TestServerManagerStopsOnContextCancellation`.
- `TestServerManagerStopIsIdempotent`.
- `TestServerManagerExternalModeDoesNotSpawn`.
- `TestHealthProbeClosesResponseBody`.
- `TestClientRejectsInvalidBaseURL`.
- `TestClientReturnsMarshalOrRequestErrors`.

### Cambio mínimo

- Separar manager de cliente.
- Interfaces pequeñas para proceso y health probe.
- Cleanup determinista.
- Timeouts configurables.

### Criterio de salida

Los tests no dejan procesos ni conexiones pendientes y el cliente no puede producir panic por configuración inválida.

---

## WI-06 — Memoria acotada y segura

**Prioridad:** P1  
**Dependencias:** WI-04

### Requisitos

- MEM-001 a MEM-007.

### Tests RED

- `TestMemoryNeverExceedsBudget`.
- `TestMemoryKeepsSingleSystemPrompt`.
- `TestMemoryPreservesToolExchange`.
- `TestMemoryDoesNotPromoteConversationToSystem`.
- `TestMemoryDropsOldestCompleteTurn`.
- `TestSummaryReplacesPreviousSummary`.
- `TestLongConversationRemainsBounded`.

### Cambio mínimo

- Ventana por budget.
- Turnos completos.
- Summary opcional y reemplazable.
- Eliminar concatenación indefinida.

### Criterio de salida

Una conversación prolongada mantiene tamaño estable y roles válidos.

---

## WI-07 — Pipeline testeable de voz

**Prioridad:** P1  
**Dependencias:** WI-04, WI-05, WI-06

### Requisitos

- AUD-001 a AUD-006.
- APP-001 a APP-006.

### Tests RED

- `TestRecorderCalculatesMonoChunkSize`.
- `TestRecorderStopsOnCancellation`.
- `TestApplicationSkipsAgentWhenSTTFails`.
- `TestApplicationSkipsSpeakerWhenAgentFails`.
- `TestApplicationRecoversAfterTTSError`.
- `TestApplicationCompletesVoiceTurn`.

### Cambio mínimo

- `Application.Run` o `RunTurn` con dependencias inyectadas.
- Interfaces solo en bordes.
- Separar síntesis de playback.
- Métricas por etapa.

### Criterio de salida

El pipeline completo se valida sin micrófono, modelos ni speaker reales.

---

## WI-08 — Build, systemd y operación

**Prioridad:** P0 para seguridad de instalación; se ejecuta después de estabilizar contratos  
**Dependencias:** WI-01, WI-05, WI-07

### Requisitos

- OPS-001 a OPS-012.

### Tests/evidencia

- `make all` desde checkout limpio.
- `make install DESTDIR=<tmp>` o harness equivalente.
- `shellcheck scripts/*.sh`.
- `systemd-analyze verify` sobre unit file.
- comprobación de `User=` no root.
- comprobación de permisos y paths writeable.
- `assistant --version` coincide con build metadata.
- instalación y uninstall en contenedor/VM.

### Cambio mínimo

- Build reproducible.
- Compose coherente o targets removidos.
- Usuario dedicado.
- Unit file hardened.
- Rollback/uninstall.

### Criterio de salida

Una instalación limpia inicia y detiene el asistente sin privilegios y sin acceso de escritura fuera de los paths declarados.

---

## WI-09 — Release candidate

**Prioridad:** P1  
**Dependencias:** WI-00 a WI-08

### Requisitos

- REL-001 a REL-008.

### Evidencia

- suite completa verde;
- E2E con fakes;
- smoke test hardware/modelos;
- operación continua definida por el plan de QA;
- runbook y rollback;
- changelog;
- release candidate versionada.

### Criterio de salida

El release cumple la Definition of Done global y no mantiene excepciones P0 abiertas.

## Orden y paralelización

```text
WI-00
  |-- WI-01 --+-- WI-02 --+
  |           |            |
  |           +-- WI-03 ---+--> WI-04 --> WI-06 --> WI-07 --> WI-08 --> WI-09
  |
  +---------------- WI-05 ------------------^
```

WI-02, WI-03 y WI-05 pueden desarrollarse en paralelo después de sus dependencias, pero deben integrarse antes de WI-07.
