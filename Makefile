# Makefile

# Define the target platforms
PLATFORMS = linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/386
OUTPUT_DIR = build
MAIN = tsk.go

.PHONY: all clean

all: $(OUTPUT_DIR) build-all

$(OUTPUT_DIR):
	mkdir -p $(OUTPUT_DIR)

build-all:
	@for platform in $(PLATFORMS); do \
		OS=$$(echo $$platform | cut -d'/' -f1); \
		ARCH=$$(echo $$platform | cut -d'/' -f2); \
		OUTPUT=$(OUTPUT_DIR)/tsk_$${OS}_$${ARCH}; \
		if [ "$$OS" = "windows" ]; then \
			OUTPUT=$${OUTPUT}.exe; \
		fi; \
		echo "Building for $$OS/$$ARCH -> $$OUTPUT"; \
		GOOS=$$OS GOARCH=$$ARCH CGO_ENABLED=0 \
			go build -a -installsuffix cgo -ldflags '-extldflags "-static"' -o $$OUTPUT $(MAIN); \
	done

clean:
	rm -rf $(OUTPUT_DIR)

