GO      ?= go
BIN     := bin
PKGS    := ./...

.PHONY: all build test race lint fmt vet cover run clean help

all: fmt vet test build ## Format, vet, test and build

build: ## Build all binaries into bin/
	@mkdir -p $(BIN)
	$(GO) build -o $(BIN)/ $(PKGS)

test: ## Run tests
	$(GO) test $(PKGS)

race: ## Run tests with the race detector
	$(GO) test -race $(PKGS)

vet: ## Run go vet
	$(GO) vet $(PKGS)

fmt: ## Format all Go source
	$(GO) fmt $(PKGS)

lint: ## Run golangci-lint (install: https://golangci-lint.run/welcome/install/)
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found; see https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	}
	golangci-lint run

cover: ## Run tests with coverage and write coverage.html
	$(GO) test -coverprofile=coverage.out $(PKGS)
	$(GO) tool cover -html=coverage.out -o coverage.html
	@$(GO) tool cover -func=coverage.out | tail -1

run: ## Measure the default corridor (USDC -> NGNC) against live mainnet
	$(GO) run ./cmd/ladder

clean: ## Remove build and coverage artifacts
	rm -rf $(BIN) coverage.out coverage.html

help: ## List available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'
