# Makefile for the 'tsk' project

# --- Project Settings ---
BINARY_NAME := tsk
# The main package is in the project root.
MAIN_PKG := . # <<< FIX: Changed from './cmd/tsk' to '.'
# Extract version from cmd/root.go where Cobra typically stores it
VERSION ?= $(shell grep -m1 'Version\s*=' cmd/root.go | cut -d'"' -f2)
VERSION_TAG := $(shell echo $(VERSION) | sed 's/\./-/g')

# --- Paths ---
BUILD_DIR := build
ASSET_DIR := internal/data/assets
INSTALL_DIR ?= /usr/local/bin

# Source files for asset generation
JSONL_SRC := glosses.jsonl
TSV_SRC := example-sentences.tsv

# Generated asset files (target)
GOB_FILE := $(ASSET_DIR)/glosses.gob
WORDS_FILE := $(ASSET_DIR)/words.txt
DB_FILE := $(ASSET_DIR)/example-sentences.sqlite

# --- Tools ---
MAKEGLOSS_CMD := go run ./cmd/makegloss
DB_BUILDER_SCRIPT := ./build-example-sentences-db.sh
# Define platforms for cross-compilation
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/386

# Phony targets don't represent files
.PHONY: all build assets clean install

# --- Build Rules ---

# Default target: build everything
all: build

# Generate all assets. This is a phony target that depends on the actual file targets.
assets: $(GOB_FILE) $(WORDS_FILE) $(DB_FILE)
	@echo "✅ All assets are up to date."

# The main 'build' rule depends on 'assets' to ensure they are generated first.
build: assets
	@echo "-> Building binaries for $(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	@for platform in $(PLATFORMS); do \
		OS=$$(echo $$platform | cut -d'/' -f1); \
		ARCH=$$(echo $$platform | cut -d'/' -f2); \
		OUTPUT=$(BUILD_DIR)/$(BINARY_NAME)_$${OS}_$${ARCH}_$(VERSION_TAG); \
		if [ "$$OS" = "windows" ]; then \
			OUTPUT=$${OUTPUT}.exe; \
		fi; \
		echo "   - $$OS/$$ARCH -> $$OUTPUT"; \
		GOOS=$$OS GOARCH=$$ARCH CGO_ENABLED=0 go build -ldflags="-s -w" -o $$OUTPUT $(MAIN_PKG); \
	done

# --- Asset Generation Rules ---

# Rule to generate the glosses.gob file
$(GOB_FILE): $(JSONL_SRC) cmd/makegloss/main.go
	@echo "-> Generating $(GOB_FILE)..."
	@$(MAKEGLOSS_CMD) -in $(JSONL_SRC) -out $(GOB_FILE)

# Rule to generate words.txt and place it in the assets directory
$(WORDS_FILE): $(JSONL_SRC)
	@echo "-> Generating $(WORDS_FILE)..."
	@jq -r '.word' $(JSONL_SRC) | sort -u > $(WORDS_FILE)

# Rule to build the SQLite DB and place it in the assets directory
# Note: This assumes your build script can be pointed to an output file.
# If build-example-sentences-db.sh has a hardcoded path, you may need to edit it.
$(DB_FILE): $(TSV_SRC) $(DB_BUILDER_SCRIPT)
	@echo "-> Generating $(DB_FILE)..."
	@# We move the generated file to the correct assets directory.
	@./$(DB_BUILDER_SCRIPT)
	@mv $(shell basename $(DB_FILE)) $(DB_FILE)

# --- Utility Rules ---

install: build
	@echo "-> Installing $(BINARY_NAME) to $(INSTALL_DIR)..."
	@cp $(BUILD_DIR)/$(BINARY_NAME)_$(shell go env GOOS)_$(shell go env GOARCH)_$(VERSION_TAG) $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "✅ Installation complete."

clean:
	@echo "-> Cleaning generated files..."
	@rm -rf $(BUILD_DIR)
	@rm -f $(GOB_FILE) $(WORDS_FILE) $(DB_FILE)
	@# Also remove old files from the root, just in case
	@rm -f words.txt example-sentences.sqlite glosses.gob
