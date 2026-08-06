# Matriz de trazabilidad

La matriz vincula requisitos, work items, tests y evidencia de release. Los nombres de tests son contratos previstos; deben ajustarse al código final sin perder el escenario cubierto.

## Configuración y policy

| ID | Requisito | Work item | Evidencia mínima |
|---|---|---|---|
| CFG-001 | Campos YAML desconocidos deben producir error | WI-01 | `TestLoadRejectsUnknownYAMLField` |
| CFG-002 | Un valor cero válido configurado explícitamente debe preservarse | WI-01 | `TestLoadPreservesExplicitZeroTemperature` |
| CFG-003 | La configuración debe validarse antes de crear dependencias runtime | WI-01 | tests de `Config.Validate` + startup test |
| CFG-004 | Solo captura mono está soportada hasta implementar mezcla explícita | WI-01/WI-07 | `TestValidateRejectsNonMonoAudio`, `TestRecorderCalculatesMonoChunkSize` |
| POL-001 | Las herramientas mutables están deshabilitadas por defecto | WI-01 | `TestToolPolicyDeniesMutationByDefault` |
| POL-002 | Una tool no permitida no puede registrarse ni ejecutarse | WI-01 | registry/executor tests |
| POL-003 | Filesystem requiere root absoluto y válido | WI-01 | config validation tests |

## Filesystem

| ID | Requisito | Work item | Evidencia mínima |
|---|---|---|---|
| SEC-FS-001 | Ninguna ruta puede escapar mediante `..` o normalización | WI-02 | traversal tests |
| SEC-FS-002 | Ninguna ruta puede escapar mediante symlink de archivo | WI-02 | `TestSafePathRejectsSymlinkEscape` |
| SEC-FS-003 | Ninguna creación puede atravesar un parent symlink externo | WI-02 | parent symlink test |
| SEC-FS-004 | La raíz del sandbox nunca puede borrarse | WI-02 | `TestDeleteRejectsSandboxRoot` |
| SEC-FS-005 | Paths vacíos o equivalentes a raíz deben rechazarse para mutaciones | WI-02 | empty/root tests |
| SEC-FS-006 | Escrituras deben ser atómicas | WI-02 | `TestWriteIsAtomic` |
| SEC-FS-007 | Lecturas y escrituras tienen límites configurables | WI-02 | oversized payload tests |
| SEC-FS-008 | Toda mutación requiere policy explícita | WI-01/WI-02 | read-only policy tests |

## Red

| ID | Requisito | Work item | Evidencia mínima |
|---|---|---|---|
| SEC-NET-001 | Solo se aceptan schemes HTTP y HTTPS | WI-03 | unsupported scheme test |
| SEC-NET-002 | Loopback IPv4 e IPv6 debe bloquearse | WI-03 | loopback tests |
| SEC-NET-003 | Redes privadas deben bloquearse | WI-03 | RFC1918/IPv6 private tests |
| SEC-NET-004 | Link-local y metadata deben bloquearse | WI-03 | metadata tests |
| SEC-NET-005 | DNS debe validarse antes de conectar | WI-03 | resolver test |
| SEC-NET-006 | Cada redirect debe revalidarse | WI-03 | redirect-to-private test |
| SEC-NET-007 | Requests y bodies deben tener límites | WI-03 | timeout/body tests |
| SEC-NET-008 | Errores de policy, red y HTTP deben diferenciarse | WI-03 | error classification tests |

## Agente y orquestación

WI-04 está implementado y su evidencia consolidada se encuentra en `docs/refactor/WI-04_EVIDENCE.md`.

| ID | Requisito | Work item | Evidencia automatizada exacta |
|---|---|---|---|
| AGT-001 | Existe un único runtime de orquestación | WI-04 | `TestMainConstructsSingleRuntimeSessionAndApplication` |
| AGT-002 | Respuestas directas no ejecutan tools | WI-04 | `TestAgentReturnsDirectReply` |
| AGT-003 | Se soportan múltiples tool rounds | WI-04 | `TestAgentExecutesOneToolRound`, `TestAgentExecutesMultipleToolRounds` |
| AGT-004 | Tool rounds tienen límite configurable | WI-04 | `TestAgentStopsAtConfiguredRoundLimit`, `TestNewAgentRuntimeRejectsInvalidConfig` |
| AGT-005 | Mensajes assistant/tool conservan orden e IDs | WI-04 | `TestAgentPreservesToolCallOrdering` |
| AGT-006 | Tool desconocida produce error tipado | WI-04 | `TestAgentReturnsUnknownToolError` |
| AGT-007 | Error recuperable de tool puede volver al LLM | WI-04 | `TestAgentContinuesAfterRecoverableToolError`, `TestAgentReturnsDeniedToolErrorToModel` |
| AGT-008 | Cancelación interrumpe el round activo | WI-04 | `TestAgentCancelsDuringToolExecution`, `TestAgentHonorsDeadlineDuringModelGeneration` |
| AGT-009 | Cada round genera trace observable | WI-04 | `TestAgentEmitsTraceForEveryRound`, `TestMetricsObserverCountsRoundsToolsFailuresAndStops` |

## LLM

WI-05 está implementado y su evidencia consolidada se encuentra en `docs/refactor/WI-05_EVIDENCE.md`.

| ID | Requisito | Work item | Evidencia automatizada exacta |
|---|---|---|---|
| LLM-001 | Cliente y server manager tienen lifecycle separado | WI-05 | `TestClientHasNoProcessLifecycle`, `TestApplicationOwnsLLMServerLifecycle` |
| LLM-002 | Modo external no inicia subprocesso | WI-05 | `TestServerManagerExternalModeDoesNotSpawn`, `TestValidateLLMExternalModeDoesNotRequireLocalArtifacts` |
| LLM-003 | Salida prematura se detecta | WI-05 | `TestServerManagerDetectsEarlyExit` |
| LLM-004 | Startup respeta timeout y contexto | WI-05 | `TestServerManagerTimesOutWhenHealthNeverReady`, `TestServerManagerStopsOnContextCancellation` |
| LLM-005 | Stop es idempotente | WI-05 | `TestServerManagerStopIsIdempotent` |
| LLM-006 | Health probe cierra cada body | WI-05 | `TestHealthProbeClosesResponseBody` |
| LLM-007 | Requests inválidos devuelven error y nunca panic | WI-05 | `TestClientRejectsInvalidBaseURL`, `TestClientReturnsMarshalOrRequestErrors`, `TestHealthProbeReturnsRequestAndTransportErrors` |
| LLM-008 | Proceso recibe cierre escalonado | WI-05 | `TestServerManagerSignalsThenKillsAfterTimeout` |
| LLM-009 | No quedan subprocessos huérfanos | WI-05 | `TestServerManagerLeavesNoOrphanAfterStartupCancellation` |

## Memoria

WI-06 está implementado y su evidencia consolidada se encuentra en `docs/refactor/WI-06_EVIDENCE.md`.

| ID | Requisito | Work item | Evidencia automatizada exacta |
|---|---|---|---|
| MEM-001 | El prompt respeta un budget máximo | WI-06 | `TestMemoryNeverExceedsBudget`, `TestMemoryPrepareBoundsNextPrompt`, `TestMemoryPrepareRejectsInputLargerThanBudget` |
| MEM-002 | Existe un único system prompt confiable | WI-06 | `TestMemoryKeepsSingleSystemPrompt` |
| MEM-003 | Exchanges de tool se mantienen completos | WI-06 | `TestMemoryPreservesToolExchange`, `TestMemoryRejectsMalformedHistories` |
| MEM-004 | Conversación no se promueve a role system | WI-06 | `TestMemoryDoesNotPromoteConversationToSystem` |
| MEM-005 | Se descartan turnos completos más antiguos | WI-06 | `TestMemoryDropsOldestCompleteTurn`, `TestMemoryPrepareBoundsNextPrompt` |
| MEM-006 | Un resumen nuevo reemplaza al anterior | WI-06 | `TestSummaryReplacesPreviousSummary`, `TestMemoryTruncatesOversizedUTF8Summary` |
| MEM-007 | Conversación prolongada no produce crecimiento indefinido | WI-06 | `TestLongConversationRemainsBounded` |

## Audio y aplicación

WI-07 está implementado por PR #29 y su evidencia consolidada se encuentra en `docs/refactor/WI-07_EVIDENCE.md`.

| ID | Requisito | Work item | Evidencia automatizada exacta |
|---|---|---|---|
| AUD-001 | El cálculo de chunks corresponde a PCM mono S16_LE | WI-07 | `TestRecorderCalculatesMonoChunkSize` |
| AUD-002 | Captura y playback respetan cancelación | WI-07 | `TestRecorderStopsOnCancellation`, `TestPlayerStopsOnCancellation` |
| AUD-003 | EOF parcial procesa únicamente frames completos | WI-07 | `TestRecorderProcessesPartialEOF` |
| AUD-004 | Síntesis y playback son etapas y errores diferenciados | WI-07 | `TestApplicationSkipsPlaybackWhenSynthesisFails`, `TestApplicationRunRecoversAfterPlaybackError` |
| AUD-005 | VAD, pre-roll y silencio final mantienen comportamiento baseline | WI-00/WI-07 | `TestRecorderProcessChunk_PrependsPreRollOnSpeechStart`, `TestRecorderProcessChunk_IncludesTrailingSilenceBeforeFinish` |
| AUD-006 | Traces y métricas no incluyen audio ni contenido conversacional | WI-07 | `TestApplicationTraceDoesNotContainConversationContent` |
| APP-001 | Error o transcript vacío de STT no invoca agente | WI-07 | `TestApplicationSkipsAgentWhenSTTFails`, `TestApplicationSkipsAgentWhenTranscriptIsEmpty` |
| APP-002 | Error del agente no invoca síntesis ni playback | WI-07 | `TestApplicationSkipsSynthesisWhenAgentFails` |
| APP-003 | Errores de síntesis y playback son recuperables para el loop | WI-07 | `TestApplicationRunRecoversAfterSynthesisError`, `TestApplicationRunRecoversAfterPlaybackError` |
| APP-004 | Un turno exitoso recorre todas las etapas una vez y en orden | WI-07 | `TestApplicationCompletesVoiceTurn`, `TestApplicationEmitsOrderedStates` |
| APP-005 | Cancelación detiene la etapa activa y shutdown respeta ownership inverso | WI-07 | `TestApplicationStopsDuringEveryStageOnCancellation`, `TestMainClosesOwnedResourcesInReverseAcquisitionOrder` |
| APP-006 | `main.go` solo compone dependencias, señales y lifecycle | WI-07 | `TestMainIsCompositionRootOnly`, `TestMainConstructsSingleRuntimeSessionAndApplication` |

## Operación y release

| ID | Requisito | Work item | Evidencia mínima |
|---|---|---|---|
| OPS-001 | `make all` construye todos los binarios requeridos | WI-08 | clean build evidence |
| OPS-002 | `make install` depende de los artefactos completos | WI-08 | Makefile test/review |
| OPS-003 | Targets compose coinciden con archivos reales | WI-08 | compose config validation |
| OPS-004 | La versión se inyecta en una variable build-time | WI-08 | version assertion |
| OPS-005 | El servicio usa usuario dedicado no root | WI-08 | unit inspection/runtime check |
| OPS-006 | El servicio aplica hardening systemd | WI-08 | `systemd-analyze verify/security` |
| OPS-007 | Solo paths declarados son writeable | WI-08 | sandbox service test |
| OPS-008 | Config instalada usa defaults seguros | WI-08 | installed config assertions |
| OPS-009 | Instalación es idempotente | WI-08 | repeated install test |
| OPS-010 | Existe uninstall o rollback documentado | WI-08 | rollback execution |
| OPS-011 | Logs respetan configuración y no exponen secretos | WI-08 | log tests/review |
| OPS-012 | `--reset` tiene efecto real o se elimina | WI-08 | CLI test |
| REL-001 | Suite completa verde sobre commit release | WI-09 | CI link |
| REL-002 | E2E fake verde | WI-09 | test report |
| REL-003 | Smoke de audio/modelos documentado y ejecutado | WI-09 | QA record |
| REL-004 | Runbook cubre startup, fallos y recuperación | WI-09 | runbook review |
| REL-005 | Rollback fue probado | WI-09 | rollback record |
| REL-006 | No existen excepciones P0 abiertas | WI-09 | issue audit |
| REL-007 | Changelog identifica breaking config changes | WI-09 | changelog |
| REL-008 | Release candidate es reproducible desde tag | WI-09 | clean checkout build |

## Regla de actualización

Cada PR debe:

1. marcar los IDs que implementa;
2. enlazar sus tests exactos;
3. registrar desviaciones del diseño;
4. actualizar evidencia de gate;
5. no marcar un requisito como completo basándose solo en revisión manual cuando puede automatizarse.
