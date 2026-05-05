# Makefile for make-it-transparent.
#
# Run `make` with no arguments to see all available targets.
# Override variables on the command line, e.g. `make docker-run PORT=9000`.

.DEFAULT_GOAL := help

# ---- Configuration --------------------------------------------------------
APP    := make-it-transparent
IMAGE  := $(APP)
TAG    ?= local
PORT   ?= 8080
GO     ?= go
DOCKER ?= docker

# ---- Help -----------------------------------------------------------------

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "; printf "Targets:\n"} \
	      /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2} \
	      /^# ---- / {gsub(/# ---- | -+/, ""); printf "\n\033[1m%s\033[0m\n", $$0}' \
	      $(MAKEFILE_LIST)

# ---- Run from source ------------------------------------------------------

.PHONY: run
run: ## Run from source (`go run .`) on http://localhost:$(PORT)
	$(GO) run . -port=:$(PORT)

.PHONY: build
build: ## Build a static binary at ./$(APP)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $(APP) .

.PHONY: gen-og
gen-og: ## Regenerate web/og.png
	$(GO) run ./cmd/gen-og

# ---- Quality gates --------------------------------------------------------

.PHONY: test
test: ## Run tests with race detector + coverage summary
	$(GO) test -race -covermode=atomic -coverprofile=coverage.out ./...
	@$(GO) tool cover -func=coverage.out | tail -3

.PHONY: bench
bench: ## Run benchmarks (no tests)
	$(GO) test -bench=. -benchmem -run=^$$ ./...

.PHONY: fuzz
fuzz: ## Run FuzzParseHex for 30 seconds
	$(GO) test -fuzz=FuzzParseHex -fuzztime=30s ./internal/transparent

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## golangci-lint (requires the binary on PATH)
	@command -v golangci-lint >/dev/null || { echo "install golangci-lint first: https://golangci-lint.run/"; exit 1; }
	golangci-lint run --timeout=5m

.PHONY: fmt
fmt: ## gofmt -w the tree
	gofmt -w .

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy

.PHONY: ci
ci: vet test ## Mirror what CI runs (vet + race tests)

# ---- Run from Docker ------------------------------------------------------

.PHONY: docker-build
docker-build: ## Build the image as $(IMAGE):$(TAG)
	$(DOCKER) build -t $(IMAGE):$(TAG) .

.PHONY: docker-run
docker-run: docker-build ## Build then run on http://localhost:$(PORT)
	-$(DOCKER) rm -f $(APP) >/dev/null 2>&1
	$(DOCKER) run -d --name $(APP) -p $(PORT):8080 $(IMAGE):$(TAG)
	@echo "→ http://localhost:$(PORT)  (logs: make docker-logs · stop: make docker-stop)"

.PHONY: docker-logs
docker-logs: ## Tail logs from the running container
	$(DOCKER) logs -f $(APP)

.PHONY: docker-stop
docker-stop: ## Stop and remove the running container
	-$(DOCKER) rm -f $(APP) >/dev/null 2>&1

.PHONY: docker-clean
docker-clean: docker-stop ## Stop container and remove the local image
	-$(DOCKER) rmi $(IMAGE):$(TAG) >/dev/null 2>&1

.PHONY: docker-buildx
docker-buildx: ## Multi-arch build (linux/amd64 + linux/arm64). Requires buildx.
	$(DOCKER) buildx build --platform linux/amd64,linux/arm64 -t $(IMAGE):$(TAG) .

# ---- Compose --------------------------------------------------------------

.PHONY: compose-up
compose-up: ## docker compose up -d --build (uses local Dockerfile)
	$(DOCKER) compose up -d --build

.PHONY: compose-down
compose-down: ## docker compose down
	$(DOCKER) compose down

.PHONY: compose-logs
compose-logs: ## Tail compose logs
	$(DOCKER) compose logs -f

# ---- Cleanup --------------------------------------------------------------

.PHONY: clean
clean: ## Remove build artefacts (binary, coverage)
	rm -f $(APP) coverage.out
