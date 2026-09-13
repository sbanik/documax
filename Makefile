GO ?= go
BINARY ?= documax
DIR ?=
OUTPUT ?= documax-output.txt
INPUT ?= $(OUTPUT)
UNPACK_DIR ?= ./restored
FORMAT ?= bracket
SUBPATH ?=

.PHONY: help build test test-devtools validate pack pack-minimized unpack devtools

help:
	@echo "Documax development commands"
	@echo ""
	@echo "  make build"
	@echo "      Build ./$(BINARY) from ./cmd/documax."
	@echo "  make test"
	@echo "      Run the normal test suite."
	@echo "  make devtools [DIR=/path/to/project]"
	@echo "      Run devtools tests, or validate a pack/unpack round-trip when DIR is set."
	@echo "  make validate INPUT=archive.txt"
	@echo "      Validate a bracket or XML Documax document."
	@echo "  make pack DIR=/path/to/project OUTPUT=project.txt [FORMAT=xml]"
	@echo "      Pack a directory as an expanded document."
	@echo "  make pack-minimized DIR=/path/to/project OUTPUT=project.min.txt [FORMAT=xml]"
	@echo "      Pack a directory directly as a GZ+B64 minimized document."
	@echo "  make unpack INPUT=project.min.txt UNPACK_DIR=./restored [SUBPATH=d/e]"
	@echo "      Unpack an expanded or minimized document."

build:
	@echo "Building ./$(BINARY) from ./cmd/documax..."
	@$(GO) build -o ./$(BINARY) ./cmd/documax
	@echo "Build complete: ./$(BINARY)"

test:
	@echo "Running normal Documax tests..."
	@$(GO) test ./...
	@echo "Normal tests passed."

devtools:
	@if [ -n "$(DIR)" ]; then \
		echo "Running development pack/unpack validation for $(DIR)..."; \
		$(GO) run ./cmd/documax-dev validate-pack-unpack --dir "$(DIR)"; \
	else \
		echo "Running Documax and developer-tool tests..."; \
		$(GO) test -v ./...; \
	fi
	@echo "Development task passed."

test-devtools:
	@$(MAKE) devtools

validate: build
	@echo "Validating $(INPUT)..."
	@./$(BINARY) validate "$(INPUT)"
	@echo "Validation passed: $(INPUT)"

pack: build
	@echo "Packing $(DIR) as an expanded $(FORMAT) document..."
	@./$(BINARY) pack --dir "$(DIR)" --output "$(OUTPUT)" --format "$(FORMAT)"
	@echo "Pack complete: $(OUTPUT)"

pack-minimized: build
	@echo "Packing $(DIR) directly as a minimized $(FORMAT) document..."
	@./$(BINARY) pack --dir "$(DIR)" --output "$(OUTPUT)" --format "$(FORMAT)" --minimize
	@echo "Minimized pack complete: $(OUTPUT)"

unpack: build
	@echo "Unpacking $(INPUT) into $(UNPACK_DIR)..."
	@if [ -n "$(SUBPATH)" ]; then \
		./$(BINARY) unpack "$(INPUT)" --dir "$(UNPACK_DIR)" --subpath "$(SUBPATH)"; \
	else \
		./$(BINARY) unpack "$(INPUT)" --dir "$(UNPACK_DIR)"; \
	fi
	@echo "Unpack complete: $(UNPACK_DIR)"
	
