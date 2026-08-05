# ─────────────────────────────────────────────────────────────────────────────
#  Makefile — Voice Assistant
#  Builds: llama.cpp (llama-server) · Go binary
# ─────────────────────────────────────────────────────────────────────────────

SHELL := /bin/bash
.DEFAULT_GOAL := build

# ── Directories ───────────────────────────────────────────────────────────────
REPO_ROOT   := $(shell pwd)
VENDOR_DIR  := $(REPO_ROOT)/vendor
BUILD_DIR   := $(REPO_ROOT)/build
BIN_DIR     := $(REPO_ROOT)/bin
MODEL_DIR   := $(REPO_ROOT)/models

LLAMA_DIR   := $(VENDOR_DIR)/llama.cpp
CONTAINER_COMPOSE ?= podman compose
COMPOSE_FILE := compose.yml

# ── Versions / URLs ───────────────────────────────────────────────────────────
# Upstream llama.cpp now publishes release tags; use the current one here.
LLAMA_REF    := b8660

# ── Build flags ───────────────────────────────────────────────────────────────
NPROC        := $(shell nproc)
CMAKE_COMMON := -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF
GOFLAGS      ?= -mod=mod

# GPU support: set GGML_CUDA=1 to enable CUDA layers
ifdef GGML_CUDA
  CMAKE_LLAMA_EXTRA := -DGGML_CUDA=ON
  CGO_LLAMA_EXTRA   := -DGGML_USE_CUDA
endif

# ── Targets ───────────────────────────────────────────────────────────────────

.PHONY: all build deps llama models install clean help sync

all: deps build ## Build everything from scratch

## ── Ensure builds ──────────────────────────────────────────────────────────────

sync: ## Check and build missing dependencies
	@echo "── Checking builds..."
	@[ -f $(BIN_DIR)/llama-server ] || $(MAKE) llama
	@[ -f $(BIN_DIR)/assistant ] || $(MAKE) build
	@echo "✓ All builds present"

## ── Dependency builds ────────────────────────────────────────────────────────

deps: llama ## Build third-party dependencies

llama: $(BIN_DIR)/llama-server ## Build llama-server binary

$(BIN_DIR)/llama-server:
	@echo "── Syncing llama.cpp $(LLAMA_REF)…"
	@mkdir -p $(VENDOR_DIR) $(BIN_DIR)
	@if [ ! -d $(LLAMA_DIR)/.git ]; then \
		git clone https://github.com/ggerganov/llama.cpp $(LLAMA_DIR); \
	fi
	@cd $(LLAMA_DIR) && \
		git fetch --all --tags --prune && \
		git checkout $(LLAMA_REF)
	@echo "── Cleaning previous llama.cpp build…"
	@rm -rf $(LLAMA_DIR)/build
	@echo "── Building llama-server…"
	@cmake -S $(LLAMA_DIR) -B $(LLAMA_DIR)/build \
	    $(CMAKE_COMMON) \
	    -DLLAMA_BUILD_SERVER=ON \
	    -DLLAMA_BUILD_TESTS=OFF \
	    $(CMAKE_LLAMA_EXTRA)
	@cmake --build $(LLAMA_DIR)/build --config Release --target llama-server -j$(NPROC)
	@cp $(LLAMA_DIR)/build/bin/llama-server $(BIN_DIR)/llama-server
	@echo "✓ llama-server built → bin/llama-server"

clean-llama:
	@rm -rf $(LLAMA_DIR)/build $(BIN_DIR)/llama-server
	@echo "✓ llama build cleaned"

reset-llama:
	@rm -rf $(LLAMA_DIR) $(BIN_DIR)/llama-server
	@echo "✓ llama source + binary removed"

## ── Go binary ────────────────────────────────────────────────────────────────

build: ## Compile the Go assistant binary
	@echo "── Compiling assistant…"
	@mkdir -p $(BIN_DIR)
	@go build \
	    $(GOFLAGS) \
	    -ldflags="-s -w -X main.buildVersion=$$(git describe --tags --always 2>/dev/null || echo dev)" \
	    -o $(BIN_DIR)/assistant \
	    ./cmd/assistant
	@echo "✓ assistant built → bin/assistant"

## ── Model downloads ──────────────────────────────────────────────────────────

models: models/stt models/tts models/llm ## Download default models (see scripts/download_models.sh)

models/stt models/tts models/llm:
	@bash scripts/download_models.sh

## ── Installation ─────────────────────────────────────────────────────────────

install: build ## Install to /usr/local (requires sudo)
	@bash scripts/install.sh

## ── Housekeeping ─────────────────────────────────────────────────────────────

clean: ## Remove build artifacts
	@rm -rf $(BUILD_DIR) $(BIN_DIR)
	@echo "✓ cleaned"

clean-all: clean ## Remove everything including vendor sources
	@rm -rf $(VENDOR_DIR)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	    awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n",$$1,$$2}'

## ── Container dev environment ───────────────────────────────────────────────

dev-up: ## Start the containerized dev environment (detached)
	$(CONTAINER_COMPOSE) -f $(COMPOSE_FILE) up -d --build
	@echo ""
	@echo "  Service:    assistant-dev"
	@echo "  Shell:       make dev-shell"

dev-down: ## Stop and remove the container environment
	$(CONTAINER_COMPOSE) -f $(COMPOSE_FILE) down

dev-shell: ## Open a bash shell in the running container
	$(CONTAINER_COMPOSE) -f $(COMPOSE_FILE) exec assistant-dev bash

dev-rebuild: ## Rebuild the image from scratch (no cache)
	$(CONTAINER_COMPOSE) -f $(COMPOSE_FILE) build --no-cache
	$(MAKE) dev-up

dev-logs: ## Tail container logs
	$(CONTAINER_COMPOSE) -f $(COMPOSE_FILE) logs -f
