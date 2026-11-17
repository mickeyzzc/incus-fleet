SHELL := /bin/bash

GO ?= go
BUN ?= bun
GOLANGCI_LINT ?= golangci-lint
CONFIG ?= config.local.yaml
MODULE ?= github.com/mickeyzzc/incus-fleet

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/mickeyzzc/incus-fleet/internal/version.Version=$(VERSION) \
	-X github.com/mickeyzzc/incus-fleet/internal/version.Commit=$(COMMIT) \
	-X github.com/mickeyzzc/incus-fleet/internal/version.BuildDate=$(BUILD_DATE)

GO_PACKAGES := ./cmd/... ./internal/... ./web
# main() calls os.Exit and web only embeds assets, so the coverage gate applies
# to the internal packages that hold product behavior.
COVERPKG := $(shell $(GO) list ./internal/... 2>/dev/null | grep -v -E '/internal/(testutil|agentpb)' | paste -sd, -)
GO_FILES := $(shell find cmd internal web -name '*.go' -not -path '*/node_modules/*')

.PHONY: help deps frontend backend build run test test-go test-web coverage coverage-go coverage-web \
	lint lint-go lint-web fmt fmt-check vet tidy check release clean proto agent-test agent-lint agent-build

help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps: ## Install Go modules and frontend dependencies
	$(GO) mod download
	cd web && $(BUN) install --frozen-lockfile

frontend: ## Build the Vue frontend into web/dist
	cd web && $(BUN) run build

backend: ## Build the Go binaries (requires web/dist)
	mkdir -p bin
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/incus-fleet ./cmd/incus-fleet
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/incus-fleet-helper ./cmd/incus-fleet-helper

build: frontend backend ## Build the frontend and backend

run: ## Run locally with CONFIG (default: config.local.yaml)
	$(GO) run ./cmd/incus-fleet -config $(CONFIG)

test: test-go test-web ## Run backend and frontend tests

test-go: ## Run Go tests
	$(GO) test $(GO_PACKAGES)

test-web: ## Run frontend tests
	cd web && $(BUN) run test

agent-test: ## Run Rust agent tests
	cargo test --manifest-path agent/Cargo.toml

agent-lint: ## Run Rust formatting and clippy
	cargo fmt --manifest-path agent/Cargo.toml -- --check
	cargo clippy --manifest-path agent/Cargo.toml -- -D warnings

agent-build: ## Build the Rust agent in release mode
	cargo build --manifest-path agent/Cargo.toml --release

proto: ## Regenerate Go protobuf bindings
	protoc -I api --go_out=. --go_opt=module=$(MODULE) api/agent/v1/agent.proto

coverage: coverage-go coverage-web ## Run both coverage suites

coverage-go: ## Run Go coverage and enforce the 90% gate
	$(GO) test -coverpkg='$(COVERPKG)' -coverprofile=coverage.out $(GO_PACKAGES)
	@total=$$($(GO) tool cover -func=coverage.out | tail -1 | grep -oE '[0-9]+\.[0-9]+'); \
	echo "Go coverage: $$total%"; \
	awk -v c="$$total" 'BEGIN { if (c + 0 < 90) { print "error: Go coverage below 90%"; exit 1 } }'

coverage-web: ## Run frontend coverage with configured thresholds
	cd web && $(BUN) run coverage

lint: lint-go lint-web ## Lint backend and frontend

lint-go: ## Run go vet and golangci-lint
	$(GO) vet $(GO_PACKAGES)
	$(GOLANGCI_LINT) run

lint-web: ## Run ESLint
	cd web && $(BUN) run lint

fmt: ## Format Go sources
	gofmt -w $(GO_FILES)

fmt-check: ## Fail if Go sources are not formatted
	@unformatted="$$(gofmt -l $(GO_FILES))"; \
	if [ -n "$$unformatted" ]; then echo "gofmt required for:"; echo "$$unformatted"; exit 1; fi

vet: ## Run go vet
	$(GO) vet $(GO_PACKAGES)

tidy: ## Tidy Go modules
	$(GO) mod tidy

check: fmt-check vet lint test ## Full local verification

release: frontend ## Cross-compile Linux binaries into dist/
	mkdir -p dist
	GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/incus-fleet-linux-amd64 ./cmd/incus-fleet
	GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/incus-fleet-linux-arm64 ./cmd/incus-fleet
	GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/incus-fleet-helper-linux-amd64 ./cmd/incus-fleet-helper
	GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/incus-fleet-helper-linux-arm64 ./cmd/incus-fleet-helper

clean: ## Remove build outputs but keep web/dist/.gitkeep
	rm -rf bin dist coverage.out coverage.html
	find web/dist -mindepth 1 ! -name .gitkeep -delete
