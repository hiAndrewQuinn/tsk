# Makefile

# Define the target platforms
PLATFORMS = linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/386
OUTPUT_DIR = build
MAIN = tsk.go

# Extract version from tsk.go (e.g. "v0.0.1") and replace dots with dashes
VERSION := $(shell grep -m1 'const version' tsk.go | cut -d'"' -f2 | sed 's/\./-/g')

# Installation directory (default: /usr/local/bin)
INSTALL_DIR ?= /usr/local/bin

.PHONY: all clean install

all: words.txt $(OUTPUT_DIR) build-all

# Generate words.txt from glosses.jsonl before building
words.txt: glosses.jsonl
	jq '.word' glosses.jsonl | sort -u > words.txt

$(OUTPUT_DIR):
	mkdir -p $(OUTPUT_DIR)

build-all:
	@for platform in $(PLATFORMS); do \
		OS=$$(echo $$platform | cut -d'/' -f1); \
		ARCH=$$(echo $$platform | cut -d'/' -f2); \
		OUTPUT=$(OUTPUT_DIR)/tsk_$${OS}_$${ARCH}_$(VERSION); \
		if [ "$$OS" = "windows" ]; then \
			OUTPUT=$${OUTPUT}.exe; \
		fi; \
		echo "Building for $$OS/$$ARCH -> $$OUTPUT"; \
		GOOS=$$OS GOARCH=$$ARCH CGO_ENABLED=0 \
			go build -a -installsuffix cgo -ldflags '-extldflags "-static"' -o $$OUTPUT $(MAIN); \
	done

install: all
	@if [ "$(shell go env GOOS)" = "windows" ]; then \
		echo "Install not supported on Windows"; \
		exit 1; \
	fi
	@echo "Installing tsk to $(INSTALL_DIR)"; \
	cp $(OUTPUT_DIR)/tsk_$(shell go env GOOS)_$(shell go env GOARCH)_$(VERSION) $(INSTALL_DIR)/tsk

clean:
	rm -rf $(OUTPUT_DIR)
