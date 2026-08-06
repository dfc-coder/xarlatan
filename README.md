# Xarlatan — asistente de voz local

Xarlatan es un asistente de voz local para Linux x86_64 escrito en Go. Usa ALSA para audio, sherpa-onnx para STT/TTS y `llama-server` para inferencia LLM.

```text
micrófono → STT → conversation.Session → AgentRuntime → TTS → altavoz
```

## Requisitos

- Go 1.21 o superior.
- `build-essential`, `cmake`, `git` y `curl`.
- `alsa-utils` y `libasound2-dev`.
- Linux con systemd para la instalación como servicio.

En Debian/Ubuntu:

```bash
sudo apt install -y build-essential cmake git curl alsa-utils libasound2-dev
```

## Build

```bash
git clone https://github.com/dfc-coder/xarlatan.git
cd xarlatan
make all
```

`make all` construye:

- `bin/assistant`;
- `bin/calibrate`;
- `bin/llama-server`.

La versión puede fijarse de forma reproducible:

```bash
make clean build VERSION=0.4.0-beta.1
./bin/assistant -version
```

## Modelos

```bash
make models
```

Los modelos locales quedan en `models/`. La instalación copia los modelos presentes a `/var/lib/xarlatan/models`.

## Ejecución en foreground

```bash
./bin/assistant -config config.yaml -no-tools
```

Para calibrar el umbral del micrófono:

```bash
./bin/calibrate
```

## Instalación del sistema

Construye como usuario normal y luego instala como root:

```bash
make all
sudo make install
```

La instalación crea:

```text
/usr/local/bin/xarlatan
/usr/local/bin/xarlatan-calibrate
/usr/local/bin/llama-server
/etc/xarlatan/config.yaml
/etc/systemd/system/xarlatan.service
/var/lib/xarlatan/models
/var/backups/xarlatan
```

El servicio:

- usa el usuario y grupo dedicados `xarlatan`;
- no se habilita ni inicia automáticamente;
- arranca con `--no-tools`;
- solo puede escribir en `/var/lib/xarlatan`;
- registra salida en journald.

Antes de habilitarlo, revisa `/etc/xarlatan/config.yaml`, confirma que los modelos existen y configura un dispositivo ALSA accesible desde un servicio del sistema. En escritorios con PipeWire, `default` puede depender de la sesión del usuario; suele ser necesario usar un dispositivo ALSA explícito como `plughw:CARD=...,DEV=0`.

```bash
sudo systemctl enable --now xarlatan
systemctl status xarlatan
journalctl -u xarlatan -f
```

## Rollback y desinstalación

La última instalación conserva un snapshot root-only en `/var/backups/xarlatan`.

```bash
sudo make rollback
```

Desinstalar preservando configuración y modelos:

```bash
sudo make uninstall
```

Eliminar también configuración, modelos, backup y usuario de servicio:

```bash
sudo PURGE=1 make uninstall
```

## Configuración segura

La configuración instalada usa paths absolutos. El filesystem está deshabilitado y el servicio deshabilita todas las tools por defecto. Para habilitar tools hay que modificar explícitamente la unidad y revisar `ToolPolicy` y el sandbox.

## Flags principales

```text
-config string
-log string
-no-tools
-max-tool-rounds int
-max-history-bytes int
-max-summary-bytes int
-version
```

`--reset` fue eliminado mientras la memoria sea exclusivamente volátil; no se expone una operación sin efecto observable.

## Validación

```bash
gofmt -l .
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./internal/orchestrator ./internal/tools ./internal/llm ./internal/memory ./internal/audio ./internal/conversation ./internal/application
shellcheck scripts/install.sh scripts/uninstall.sh scripts/rollback.sh scripts/tests/wi08_contract_test.sh
bash scripts/tests/wi08_contract_test.sh
sudo systemd-analyze verify packaging/systemd/xarlatan.service
```

## Licencia

MIT
