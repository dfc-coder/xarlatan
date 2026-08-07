SHELL := /bin/bash
.DEFAULT_GOAL := build

REPO_ROOT := $(shell pwd)
VENDOR_DIR := $(REPO_ROOT)/vendor
BIN_DIR := $(REPO_ROOT)/bin
RUNTIME_LIB_DIR := $(REPO_ROOT)/lib
LLAMA_DIR := $(VENDOR_DIR)/llama.cpp

LLAMA_REF := b8660
NPROC := $(shell nproc)
CMAKE_COMMON := -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF
GOFLAGS ?= -mod=mod
VERSION ?= v0.5.0-beta.1
BUILD_VERSION := $(patsubst v%,%,$(VERSION))

ifdef GGML_CUDA
  CMAKE_LLAMA_EXTRA := -DGGML_CUDA=ON
endif

.PHONY: all build runtime-libs deps llama models install uninstall rollback release-candidate preflight beta-acceptance clean clean-all clean-llama reset-llama help FORCE

all: deps build ## Build llama-server, Go commands and native runtime libraries

deps: llama ## Build third-party runtime dependencies

llama: $(BIN_DIR)/llama-server ## Build llama-server

$(BIN_DIR)/llama-server:
	@echo "── Syncing llama.cpp $(LLAMA_REF)…"
	@mkdir -p $(VENDOR_DIR) $(BIN_DIR)
	@if [ ! -d $(LLAMA_DIR)/.git ]; then \
		git clone https://github.com/ggerganov/llama.cpp $(LLAMA_DIR); \
	fi
	@cd $(LLAMA_DIR) && git fetch --all --tags --prune && git checkout $(LLAMA_REF)
	@rm -rf $(LLAMA_DIR)/build
	@cmake -S $(LLAMA_DIR) -B $(LLAMA_DIR)/build \
		$(CMAKE_COMMON) \
		-DLLAMA_BUILD_SERVER=ON \
		-DLLAMA_BUILD_TESTS=OFF \
		$(CMAKE_LLAMA_EXTRA)
	@cmake --build $(LLAMA_DIR)/build --config Release --target llama-server -j$(NPROC)
	@cp $(LLAMA_DIR)/build/bin/llama-server $(BIN_DIR)/llama-server
	@echo "✓ llama-server built → bin/llama-server"

build: $(BIN_DIR)/assistant $(BIN_DIR)/calibrate runtime-libs ## Build Go commands and stage native runtime libraries

$(BIN_DIR)/assistant: FORCE
	@mkdir -p $(BIN_DIR)
	@go build $(GOFLAGS) \
		-ldflags="-s -w -X main.buildVersion=$(BUILD_VERSION)" \
		-o $(BIN_DIR)/assistant ./cmd/assistant
	@echo "✓ assistant v$(BUILD_VERSION) built → bin/assistant"

$(BIN_DIR)/calibrate: FORCE
	@mkdir -p $(BIN_DIR)
	@go build $(GOFLAGS) -ldflags="-s -w" -o $(BIN_DIR)/calibrate ./cmd/calibrate
	@echo "✓ calibrate built → bin/calibrate"

runtime-libs: $(BIN_DIR)/assistant FORCE ## Stage non-system CGo libraries before sudo installation
	@module_dir="$$(go list $(GOFLAGS) -m -f '{{.Dir}}' github.com/k2-fsa/sherpa-onnx-go-linux)"; \
	rm -rf $(RUNTIME_LIB_DIR); \
	mkdir -p $(RUNTIME_LIB_DIR); \
	bash scripts/collect_runtime_libs.sh $(BIN_DIR)/assistant $(RUNTIME_LIB_DIR) "$$module_dir"; \
	test -s $(RUNTIME_LIB_DIR)/libsherpa-onnx-c-api.so
	@echo "✓ native runtime staged → lib/"

FORCE:

models: ## Download and validate default runtime models
	@bash scripts/download_models.sh

install: all ## Install binaries, config and user service; does not enable service
	@bash scripts/install.sh

uninstall: ## Remove binaries, libraries and service, preserving config/models
	@bash scripts/uninstall.sh

rollback: ## Restore the state before the last install
	@bash scripts/rollback.sh

release-candidate: all ## Build deterministic beta archive and SHA-256
	@bash scripts/package_release.sh "$(VERSION)"

preflight: build ## Validate local beta prerequisites and models
	@EXPECTED_VERSION="v$(BUILD_VERSION)" XARLATAN_BIN="$(BIN_DIR)/assistant" LLAMA_SERVER_BIN="$(BIN_DIR)/llama-server" bash scripts/preflight.sh config.yaml

beta-acceptance: all ## Run interactive physical beta acceptance
	@EXPECTED_VERSION="v$(BUILD_VERSION)" XARLATAN_BIN="$(BIN_DIR)/assistant" LLAMA_SERVER_BIN="$(BIN_DIR)/llama-server" bash scripts/beta_acceptance.sh config.yaml

clean-llama:
	@rm -rf $(LLAMA_DIR)/build $(BIN_DIR)/llama-server

reset-llama:
	@rm -rf $(LLAMA_DIR) $(BIN_DIR)/llama-server

clean: ## Remove generated binaries, native libraries and release archives
	@rm -rf $(BIN_DIR) $(RUNTIME_LIB_DIR) $(REPO_ROOT)/dist

clean-all: clean ## Remove generated binaries and vendored llama.cpp
	@rm -rf $(VENDOR_DIR)

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n",$$1,$$2}'
