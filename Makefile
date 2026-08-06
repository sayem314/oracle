.PHONY: help dev run-api run-web build test lint fmt openapi-lint sqlc migration docker-build docker-up docker-down docker-logs backup backup-restore

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

openapi-lint: ## Validate docs/openapi.yaml with Redocly
	bunx @redocly/cli lint docs/openapi.yaml

sqlc: ## Regenerate type-safe query code from SQL (requires sqlc CLI)
	sqlc -f apps/api/sqlc.yaml generate

migration: ## Create a migration (usage: make migration name=create_foo)
	go run github.com/pressly/goose/v3/cmd/goose -dir apps/api/migrations create $(name) sql

docker-build: ## Build the Docker images (api + web)
	docker compose build

docker-up: ## Build and run the full stack with Docker Compose
	docker compose up --build -d

docker-down: ## Stop the Docker Compose stack
	docker compose down

docker-logs: ## Tail logs from the Docker Compose stack
	docker compose logs -f

backup: ## Snapshot the SQLite volume into backups/ (KEEP=n keeps n snapshots)
	./scripts/backup.sh backup

backup-restore: ## Restore a snapshot: make backup-restore FILE=backups/oracle-20260806-120000.db
	./scripts/backup.sh restore "$(FILE)"
