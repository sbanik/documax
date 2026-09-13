GO ?= go
BINARY ?= documax
ARGS ?=

.PHONY: help build test test-race coverage test-devtools devtools validate pack pack-minimized unpack minimize expand fix

help:
	@echo "Documax development commands"
	@echo ""
	@echo "Pass any CLI flags and positional arguments through ARGS."
	@echo ""
	@echo "  make build"
	@echo "      Build ./$(BINARY) from ./cmd/documax."
	@echo "  make test"
	@echo "      Run all tests."
	@echo "  make test-race"
	@echo "      Run all tests with Go's race detector."
	@echo "  make coverage"
	@echo "      Report coverage for the application packages."
	@echo "  make pack ARGS='./my-project --format xml'"
	@echo "      Pass ARGS to: documax pack"
	@echo "  make pack-minimized ARGS='./my-project --output project.min.txt'"
	@echo "      Pass ARGS to: documax pack --minimized"
	@echo "  make unpack ARGS='project.min.txt --dir ./restored --subpath d/e'"
	@echo "      Pass ARGS to: documax unpack"
	@echo "  make validate ARGS='project.txt'"
	@echo "      Pass ARGS to: documax validate"
	@echo "  make fix ARGS='project.txt --in-place'"
	@echo "      Pass ARGS to: documax fix"
	@echo "  make minimize ARGS='project.txt --output project.min.txt'"
	@echo "      Pass ARGS to: documax minimize"
	@echo "  make expand ARGS='project.min.txt --output project.txt'"
	@echo "      Pass ARGS to: documax expand"
	@echo "  make devtools ARGS='--dir ./my-project'"
	@echo "      Pass ARGS to: documax-dev validate-pack-unpack"

build:
	@echo "Building ./$(BINARY) from ./cmd/documax..."
	@$(GO) build -o ./$(BINARY) ./cmd/documax
	@echo "Build complete: ./$(BINARY)"

test:
	@echo "Running Documax tests..."
	@$(GO) test ./...
	@echo "Tests passed."

test-race:
	@echo "Running Documax tests with the race detector..."
	@$(GO) test -race ./...
	@echo "Race-detector tests passed."

coverage:
	@echo "Reporting Documax application-package coverage..."
	@$(GO) test -cover ./internal/core ./internal/devtools ./internal/documax

test-devtools:
	@echo "Running Documax and developer-tool tests..."
	@$(GO) test -v ./...
	@echo "Developer-tool tests passed."

devtools:
	@echo "Running documax-dev validate-pack-unpack $(ARGS)..."
	@$(GO) run ./cmd/documax-dev validate-pack-unpack $(ARGS)
	@echo "Development pack/unpack validation passed."

validate:
	@echo "Running documax validate $(ARGS)..."
	@$(GO) run ./cmd/documax validate $(ARGS)
	@echo "Validation passed."

pack:
	@echo "Running documax pack $(ARGS)..."
	@$(GO) run ./cmd/documax pack $(ARGS)
	@echo "Pack complete."

pack-minimized:
	@echo "Running documax pack --minimized $(ARGS)..."
	@$(GO) run ./cmd/documax pack --minimized $(ARGS)
	@echo "Minimized pack complete."

unpack:
	@echo "Running documax unpack $(ARGS)..."
	@$(GO) run ./cmd/documax unpack $(ARGS)
	@echo "Unpack complete."

minimize:
	@echo "Running documax minimize $(ARGS)..."
	@$(GO) run ./cmd/documax minimize $(ARGS)
	@echo "Minimize complete."

expand:
	@echo "Running documax expand $(ARGS)..."
	@$(GO) run ./cmd/documax expand $(ARGS)
	@echo "Expand complete."

fix:
	@echo "Running documax fix $(ARGS)..."
	@$(GO) run ./cmd/documax fix $(ARGS)
	@echo "Fix complete."
