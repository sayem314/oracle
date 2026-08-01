.PHONY: help dev run-api run-web build test lint fmt

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

dev: ## Run the Go API and SvelteKit dev server together
	@$(MAKE) -j2 run-api run-web

run-api: ## Run the Go API (cmd/oracle)
	go run ./apps/api/cmd/oracle

run-web: ## Run the SvelteKit dev server
	bun run dev

build: ## Build the API binary into bin/ and the frontend
	go build -o bin/oracle ./apps/api/cmd/oracle
	bun run build

test: ## Run Go tests and the frontend type-check
	go test ./apps/api/...
	bun run check

lint: ## Lint Go (golangci-lint), check Prettier, and type-check the frontend
	golangci-lint run ./apps/api/...
	bun run lint
	bun run check

fmt: ## Format Go and frontend code
	gofmt -w apps/api
	bun run format
