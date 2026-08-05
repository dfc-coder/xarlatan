# Xarlatan — asistente de voz local

Asistente de voz local escrito en **Go** para Linux x86_64. El pipeline principal usa ALSA, sherpa-onnx y llama-server. Las herramientas web son opcionales, por lo que el modo con herramientas no es estrictamente offline.

> **Estado del repositorio:** este commit congela el baseline WI-00 para iniciar el refactor SDD/TDD. Conserva deliberadamente riesgos conocidos. No ejecutes `sudo make install` ni habilites herramientas mutables hasta completar los work items de seguridad y operación.

```
Micrófono → [sherpa-onnx STT] → Texto → [llama-server] → Respuesta → [sherpa-onnx TTS] → Altavoz
                  STT                     LLM                       TTS
```

---

## Arquitectura

| Componente | Tecnología | Rol |
|---|---|---|
| **STT** | [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) · Go/CGo | Transcripción de voz a texto |
| **LLM** | [llama-server](https://github.com/ggerganov/llama.cpp) · HTTP | Generación de respuesta |
| **TTS** | [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) · Go/CGo | Síntesis de voz |
| **Audio** | ALSA (`arecord` / `aplay`) | Captura y reproducción |
| **VAD** | Energía RMS (Go puro) | Detección de voz / silencio |
| **Orquestador** | Go 1.21 | Pipeline, config, señales |

### Decisiones de diseño

- **llama-server via HTTP** — evita CGo para llama.cpp; el binario del servidor se compila una sola vez y Go lo gestiona como subproceso.  
- **sherpa-onnx vía Go/CGo** — una sola integración para STT y TTS, sin subprocesos extra.  
- **VAD en Go puro** — sin dependencias extra; umbral de energía RMS configurable.

---

## Requisitos del sistema

```bash
# Herramientas de compilación
sudo apt install -y build-essential cmake git curl

# Go 1.21+
# https://go.dev/dl/

# ALSA (audio)
sudo apt install -y alsa-utils libasound2-dev

# Añadir usuario al grupo audio
sudo usermod -aG audio $USER
```

---

## Build rápido

```bash
# 1. Clonar y entrar
git clone https://github.com/dfc-coder/xarlatan.git && cd xarlatan

# 2. Compilar todo (llama-server, binario Go)
make all

# 3. Descargar modelos por defecto
#    sherpa STT base · TTS es_ES davefx-medium · Gemma 3 270M Q4_K_M
make models

# 4. Ejecutar
./bin/assistant
```

### Con GPU (CUDA)

```bash
GGML_CUDA=1 make all
# En config.yaml: llm.n_gpu_layers: 35   (ajustar según VRAM)
```

---

## Instalación permanente

```bash
# Instala en /usr/local/bin + servicio systemd
sudo make install

# Habilitar como servicio
sudo systemctl enable --now assistant

# Ver logs
journalctl -u assistant -f
```

---

## Estructura del proyecto

```
assistant/
├── cmd/
│   └── assistant/
│       └── main.go          # Punto de entrada, pipeline principal
├── internal/
│   ├── audio/
│   │   ├── capture.go       # Grabación ALSA (arecord)
│   │   └── playback.go      # Reproducción ALSA (aplay)
│   ├── vad/
│   │   └── vad.go           # VAD energía RMS (máquina de estados)
│   ├── stt/
│   │   └── stt.go           # Wrapper sherpa-onnx STT
│   ├── llm/
│   │   └── llama.go         # Cliente HTTP llama-server + historial
│   ├── tts/
│   │   └── tts.go           # Wrapper sherpa-onnx TTS
│   └── config/
│       └── config.go        # Carga YAML + defaults
├── scripts/
│   ├── download_models.sh   # Descarga modelos de HuggingFace
│   └── install.sh           # Instalador del sistema
├── Makefile                 # Build, deps, install, compose
├── Dockerfile               # Entorno de desarrollo en contenedor
├── config.yaml              # Configuración por defecto
└── go.mod
```

### Contenedor local

Los targets `dev-*` del Makefile todavía hacen referencia a `compose.yml`, pero ese archivo no estaba incluido en el snapshot. Se conservan únicamente para congelar el baseline; su reparación o eliminación pertenece a WI-08.

---

## Configuración

Edita `config.yaml` (o `/etc/assistant/config.yaml` si instalado):

```yaml
audio:
  silence_threshold: 0.015   # Bajar = más sensible al ruido
  silence_duration_ms: 1500  # Espera antes de cortar grabación

stt:
  encoder: "models/stt/sherpa-onnx-whisper-base/base-encoder.onnx"
  decoder: "models/stt/sherpa-onnx-whisper-base/base-decoder.onnx"
  tokens: "models/stt/sherpa-onnx-whisper-base/base-tokens.txt"
  language: "auto"           # o "es", "en", etc.

llm:
  model: "models/llm/gemma-3-270m-it-Q4_K_M.gguf"
  n_gpu_layers: 0            # >0 para GPU
  threads: 4
  system_prompt: "Eres un asistente de voz…"

tts:
  model: "models/tts/vits-piper-es_ES-davefx-medium/es_ES-davefx-medium.onnx"
  tokens: "models/tts/vits-piper-es_ES-davefx-medium/tokens.txt"
  data_dir: "models/tts/vits-piper-es_ES-davefx-medium/espeak-ng-data"
  length_scale: 1.0          # Velocidad: <1 más rápido
```

---

## Modelos recomendados

### sherpa-onnx STT

| Modelo | Tamaño | Velocidad | Calidad |
|---|---|---|---|
| `tiny` | 75 MB | ⚡⚡⚡ | Básica |
| `base` | 142 MB | ⚡⚡ | Buena ✓ |
| `small` | 466 MB | ⚡ | Muy buena |
| `medium` | 1.5 GB | ~ | Excelente |

### LLM (GGUF)

| Modelo | Tamaño | RAM | Notas |
|---|---|---|---|
| Gemma 3 270M Q4_K_M | ~0.2 GB | 1 GB | Ligero |
| Qwen2.5-1.5B Q4_K_M | ~1 GB | 2 GB | Más capacidad |
| Qwen2.5-3B Q4_K_M | ~2 GB | 3 GB | Buen equilibrio ✓ |
| Llama-3.2-3B Q4_K_M | ~2 GB | 3 GB | Alternativa English |
| Mistral-7B Q4_K_M | ~4.1 GB | 6 GB | Alta calidad |

### sherpa-onnx TTS (voces españolas)

```bash
# Cambiar voces/modelos al descargar:
STT_MODEL=base                make models
TTS_MODEL=es_ES-davefx-medium make models    # Español España
TTS_MODEL=es_MX-claude-high   make models    # Español México
```

---

## Validación del baseline

```bash
gofmt -l .
go vet ./...
go test -count=1 ./...
go test -coverprofile=coverage.out ./...
go build ./cmd/assistant ./cmd/calibrate
```

La suite completa requiere descargar `gopkg.in/yaml.v3` y `sherpa-onnx-go`. Los resultados reproducidos se registran en [`docs/baseline/VALIDATION.md`](docs/baseline/VALIDATION.md).

---

## Flags de línea de comandos

```text
./bin/assistant [flags]

  -config string    Ruta al config.yaml (default: "config.yaml")
  -log string       Nivel de log: debug|info|warn|error (default: "info")
  -no-tools         Deshabilitar todas las herramientas
  -reset            Borrar historial de conversación al iniciar
  -version          Mostrar versión y salir
```

---

## Latencias típicas (CPU, i7-10700)

| Componente | Modelo | Latencia |
|---|---|---|
| STT | sherpa-onnx ASR base, 5s audio | ~400 ms |
| LLM | Gemma 3 270M Q4 | ~800 ms |
| TTS | sherpa-onnx es_ES davefx | ~150 ms |
| **Total** | | **~1.5 s** |

---

## Variables de entorno

| Variable | Descripción |
|---|---|
| `ASSISTANT_CONFIG` | Ruta al config.yaml (sobreescribe `-config`) |
| `GGML_CUDA` | Activa soporte CUDA en el Makefile |

---

## Licencia

MIT
