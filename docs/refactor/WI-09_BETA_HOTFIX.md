# WI-09 beta hotfix — primera ejecución en dakota-fedora

## Evidencia observada

La primera prueba física encontró dos bloqueos antes de ejecutar el pipeline:

1. `make models` descargó STT y TTS, pero el LLM configurado desde `google/gemma-3-270m-it-GGUF` devolvió HTTP 401.
2. `./scripts/beta_acceptance.sh` devolvió `Permission denied` porque el archivo no tenía bit ejecutable en el checkout.

La instalación nativa terminó porque `make install` construye e instala binarios/configuración, pero deliberadamente no descarga modelos. El preflight debe impedir la aceptación mientras falte el LLM.

## Corrección

- usar un GGUF público y no autenticado: `Qwen/Qwen2.5-0.5B-Instruct-GGUF`;
- usar `qwen2.5-0.5b-instruct-q4_k_m.gguf` con SHA-256 conocido;
- descargar el LLM a un archivo temporal, validar checksum y moverlo de forma atómica;
- rechazar archivos vacíos o parciales;
- convertir `make models` en un único target real;
- ejecutar los scripts desde documentación mediante `bash`, incluso cuando el checkout pierda el bit ejecutable;
- actualizar la configuración beta al nuevo nombre de modelo.

## Estado

WI-09 continúa abierto. La prueba física debe repetirse después de integrar este hotfix y actualizar `/etc/xarlatan/config.yaml`.
