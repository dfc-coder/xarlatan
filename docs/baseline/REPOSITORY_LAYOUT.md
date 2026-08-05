# Clasificación del repositorio

Este documento congela qué pertenece al baseline de WI-00.

## Fuente versionada

- `cmd/`: binarios `assistant` y `calibrate`.
- `internal/`: audio, VAD, STT, LLM, TTS, herramientas, memoria y orquestación.
- `scripts/`: descarga de modelos e instalación.
- `config.yaml`: configuración de ejemplo del snapshot.
- `Dockerfile`, `Makefile`, `go.mod`, `go.sum`.
- pruebas `*_test.go` existentes.
- documentación SDD/TDD en `docs/refactor/` y OpenSpec.

## Dependencias de terceros

- Dependencias Go declaradas en `go.mod` y verificadas por `go.sum`.
- `llama.cpp` no se versiona: `make llama` lo clona en `vendor/` usando `LLAMA_REF`.
- Los modelos STT, TTS y LLM no se versionan: `make models` los descarga en `models/`.

## Artefactos no versionados

- `bin/`, `build/`, `dist/`: binarios y resultados de build.
- `vendor/`: checkout/build local de llama.cpp.
- `models/`: pesos y archivos de modelos.
- `coverage.out`, perfiles, logs, audio temporal y caches.
- `.pi/`: estado local generado por tooling.

## Archivos ausentes del snapshot

`Makefile` y `README.md` hacían referencia a `compose.yml`, pero el archivo no estaba incluido. WI-00 conserva los targets para no mezclar un cambio operativo; la documentación los marca como no disponibles. Su resolución pertenece a WI-08.
