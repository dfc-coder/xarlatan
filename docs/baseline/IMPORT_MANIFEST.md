# Manifiesto de importación WI-00

## Fuente

Snapshot analizado desde `assistant.zip`.

## Exclusiones intencionales

- `.git/` del snapshot.
- `.pi/` y otros estados locales generados.
- `models/`, `vendor/`, `bin/`, `build/` y artefactos derivados.
- audio, logs, cobertura y archivos temporales.

## Inclusiones agregadas durante WI-00

- `LICENSE` MIT.
- `.github/workflows/ci.yml`.
- documentación de baseline y evidencia.
- normalización del module path a `github.com/dfc-coder/xarlatan`.
- formateo `gofmt` sin modificación deliberada de comportamiento.

## Integridad funcional

WI-00 no modifica la arquitectura ni corrige los riesgos funcionales ya identificados. Los defectos se mantienen reproducibles para los slices TDD posteriores.
