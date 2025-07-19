# Makefile for the 'tsk' project

# --- Project Settings ---
BINARY_NAME := tsk
# The main package is in the project root.
MAIN_PKG := .
# Extract version from cmd/root.go where Cobra typically stores it
VERSION ?= $(shell grep -m1 'Version\s*=' cmd/root.go | cut -d'"' -f2)
VERSION_TAG := $(shell echo $(VERSION) | sed 's/\./-/g')

# --- Paths ---
BUILD_DIR := build
ASSET_DIR := internal/data/assets
INSTALL_DIR ?= /usr/local/bin

# Source files for asset generation
JSONL_SRC := data/glosses.jsonl
TSV_SRC := data/example-sentences.tsv

# Generated asset files (target)
GOB_FILE := $(ASSET_DIR)/glosses.gob
WORDS_FILE := $(ASSET_DIR)/words.txt
DB_FILE := $(ASSET_DIR)/example-sentences.sqlite

# --- Tools ---
MAKEGLOSS_CMD := go run ./cmd/makegloss
DB_BUILDER_CMD := go run ./cmd/builddb
MAKEWORDS_CMD := go run ./cmd/makewords # <--- ADD THIS LINE
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
		echo "     - $$OS/$$ARCH -> $$OUTPUT"; \
		GOOS=$$OS GOARCH=$$ARCH CGO_ENABLED=0 go build -ldflags="-s -w" -o $$OUTPUT $(MAIN_PKG); \
	done

# --- Asset Generation Rules ---

# Rule to generate the glosses.gob file
$(GOB_FILE): $(JSONL_SRC) cmd/makegloss/main.go
	@echo "-> Generating $(GOB_FILE)..."
	@$(MAKEGLOSS_CMD) -in $(JSONL_SRC) -out $(GOB_FILE)

# Rule to generate words.txt using the new 'makewords' Go program
$(WORDS_FILE): $(JSONL_SRC) cmd/makewords/main.go # <--- MODIFIED
	@echo "-> Generating $(WORDS_FILE)..."
	@$(MAKEWORDS_CMD) -in $(JSONL_SRC) -out $(WORDS_FILE) # <--- MODIFIED

# Rule to build the SQLite DB using the 'builddb' Go program
$(DB_FILE): $(TSV_SRC) cmd/builddb/main.go
	@echo "-> Generating $(DB_FILE)..."
	@$(DB_BUILDER_CMD) -in $(TSV_SRC) -out $(DB_FILE)

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
