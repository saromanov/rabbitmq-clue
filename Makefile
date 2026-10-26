BINARY := rabbitmq-clue
VERSION ?= dev
LDFLAGS := -s -w -X github.com/saromanov/rabbitmq-clue/internal/version.version=$(VERSION)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: web-build ## Build the binary with the embedded UI
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

.PHONY: run
run: ## Run locally (configure RMQ_CLUE_* env vars first)
	go run ./cmd/$(BINARY)

.PHONY: test
test: ## Unit tests with race detector
	go test -race ./...

.PHONY: test-integration
test-integration: ## Integration tests against a real broker
	RMQ_CLUE_TEST_AMQP_URL=$${RMQ_CLUE_TEST_AMQP_URL:-amqp://guest:guest@127.0.0.1:5672/%2F} \
		go test -race ./internal/replay/ ./internal/schema/ -run Integration

.PHONY: lint
lint: ## golangci-lint v2
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Format Go sources
	gofmt -w ./cmd ./internal

.PHONY: tidy
tidy: ## Verify go.mod/go.sum are tidy
	go mod tidy
	git diff --exit-code go.mod go.sum

.PHONY: vulncheck
vulncheck: ## Scan dependencies for known vulnerabilities
	go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

.PHONY: web-install
web-install: ## Install SPA dependencies
	cd web && npm ci

.PHONY: web-dev
web-dev: ## Run the SPA dev server with HMR (proxies /api to :8080)
	cd web && npm run dev

.PHONY: web-build
web-build: ## Build the SPA into internal/web/dist
	cd web && npm run build

.PHONY: docker-build
docker-build: ## Build the Docker image
	docker build -t rabbitmq-clue:local .

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin