# Validación del baseline WI-00

Fecha de congelación: 2026-08-05.

## Entorno utilizado

- Linux amd64.
- Go 1.23.2 ejecutando un módulo declarado para Go 1.21.
- Sin acceso DNS saliente desde el entorno de validación.

## Inventario

- 2 comandos: `cmd/assistant`, `cmd/calibrate`.
- 10 paquetes bajo `internal/`.
- 19 archivos `*_test.go`.
- module path normalizado a `github.com/dfc-coder/xarlatan`.

## Resultados reproducidos

| Comando | Resultado |
|---|---|
| `gofmt -l .` | Correcto: sin archivos pendientes después de normalizar el snapshot |
| `go test -count=1 -cover ./internal/vad` | Correcto — 90.1% |
| `go test -count=1 -cover ./internal/orchestrator` | Correcto — 86.3% |
| `go test -count=1 -cover ./internal/console` | Correcto — 100.0% |
| `go test -count=1 -cover ./internal/tools` | Correcto — 5.1% |
| `go test -count=1 ./...` | Bloqueado antes de compilar por falta de acceso DNS para descargar `gopkg.in/yaml.v3` y `sherpa-onnx-go` |
| `go vet ./...` | Mismo bloqueo de dependencias externas |
| `go build ./cmd/assistant ./cmd/calibrate` | Mismo bloqueo de dependencias externas |

El bloqueo externo no se registra como suite verde. La CI agregada en este work item ejecuta los gates completos en GitHub Actions, donde las dependencias pueden descargarse.

## Riesgos conocidos preservados

WI-00 importa y congela el comportamiento; no corrige los defectos siguientes:

- configuración permisiva y valores cero ambiguos;
- herramientas de filesystem sin confinamiento robusto;
- `web_fetch` vulnerable a SSRF;
- runtime agente duplicado y limitado a una ronda de tools;
- lifecycle incompleto de `llama-server`;
- memoria no acotada realmente;
- `compose.yml` ausente;
- instalación systemd sin usuario dedicado;
- inyección de versión incompatible con `const buildVersion`;
- `--reset` sin efecto persistente.

Estos riesgos están asignados a WI-01 hasta WI-08 y no deben resolverse dentro del PR de baseline.
