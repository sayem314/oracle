# AGENTS.md

Guidance for AI coding agents (and humans) working in this repository.

## Project

oracle is an autonomous personal assistant, built step by step. Self-hosted monorepo with a Go API and a SvelteKit frontend. Multi-user and API-first (web now, mobile later).

## Commands

Everything runs from the repo root:

| Command            | What it does                                  |
| ------------------ | --------------------------------------------- |
| `make help`        | Show all commands                             |
| `make dev`         | Run the Go API and SvelteKit dev server       |
| `make test`        | Go tests + svelte-check                       |
| `make lint`        | golangci-lint + prettier check + svelte-check |
| `make fmt`         | gofmt + prettier write                        |
| `make sqlc`        | Regenerate type-safe query code from SQL      |
| `make docker-up`   | Build and run the full stack via Docker       |
| `make docker-down` | Stop the Docker Compose stack                 |

Go runs via module paths from the root, for example `go test ./apps/api/...`. The frontend is a bun workspace at `apps/web`.

After touching Go modules, run `go mod tidy`.

## Layout

```
apps/api/            Go API (module github.com/sayem314/oracle/apps/api)
  cmd/oracle/        entrypoint, wiring only
  db/queries/        SQL queries (sqlc input)
  internal/auth/     Auth interface over Limen (credential-password auth, sessions)
  internal/chat/     Run engine (model->tool->model loop) shared by handlers and scheduler
  internal/config/   koanf config loading + validation
  internal/llm/      LLM Provider interface, OpenAI-compatible implementation, mock
  internal/scheduler/ Cron poll loop that runs jobs headlessly
  internal/server/   HTTP app, routes, handlers (one file per domain)
  internal/store/    Store interface over sqlc-generated db package
  internal/tool/     Tool Executor interface, Registry, built-in tools
  migrations/        goose SQL migrations, embedded and applied at startup
apps/web/            SvelteKit frontend (bun workspace)
docs/                decision log
```

Go style: `cmd/` per binary, private code under `internal/` grouped by feature. No `pkg/` until something needs importing elsewhere.

## Conventions

- Tests mirror sources (`foo.go` gets `foo_test.go`) and use black-box packages (`server_test`).
- testify: `require` for preconditions (errors, nil checks), `assert` for independent value checks. No `testify/suite` (use `t.Run`), no `testify/mock` (hand-written fakes). Enforced by `testifylint`.
- golangci-lint v2 (`.golangci.yml`) must report zero issues. gofmt is enforced.
- Deferred cleanup: a `Close()` error at teardown is deliberately ignored. Write a plain `defer x.Close() //nolint:errcheck` — no `defer func() { _ = ... }()` wrappers, and do not add errcheck exclusions to `.golangci.yml`.
- Code comments: default to none. A one-line `//` comment is allowed when the "why" is genuinely non-obvious (precedence, subtle edge cases, workarounds). Never comment self-evident code, and no block explanations unless asked.
- Config via koanf. Precedence: defaults, then `.env`, then `ORACLE_*` env vars, then plain `PORT`. Copy `.env.example` to `.env` (git-ignored).
- Logging via the global `github.com/rs/zerolog/log` package, configured once in `main`. Colored console on a TTY, JSON otherwise. Import and use `log.Info()` directly, no plumbing.
- Storage: SQLite via sqlc + goose + `modernc.org/sqlite` (pure Go). Write queries in `apps/api/db/queries/*.sql`, then `make sqlc`. Schema lives in `apps/api/migrations/` (goose Up/Down files, embedded, applied at startup). New migration: `make migration name=foo`. Handlers depend on the `store.Store` interface, never on the generated package directly.
- LLM access via the `llm.Provider` interface (streaming iterator: `Next/Current/Err/Close`). Two implementations: OpenAI-compatible (`openai-go`) and a deterministic mock (the default). Single protocol by choice: `llm_base_url` covers any OpenAI-compatible gateway (OpenRouter, LiteLLM, Ollama, ...), and Anthropic models are reachable through them. Handlers depend on the interface; tests use the mock or `httptest` SSE servers.
- Auth via Limen behind the `auth.Auth` interface: mounted at `/auth/*` through `middleware/adaptor`, session middleware guards `/api/*`, Limen's tables live in the same SQLite DB as `auth_*` (goose-managed, hand-written for SQLite). Sign-up locks after the first user, who is stamped `admin`.
- Small steps: only build what the current step needs. Revisit structure when it hurts.

## Roadmap

- [x] Step 1: Fiber v3 server + `/health`
- [x] Step 2: koanf config + zerolog logging
- [x] Step 3: SQLite via sqlc + goose (Session/Message schemas, embedded migrations, store)
- [x] Step 4: LLM `Provider` interface + OpenAI-compatible implementation + mock
- [x] Step 5: SSE `POST /api/v1/chat` wired to Provider
- [x] Step 6: Limen at `/auth/*` + session middleware on `/api/*`
- [x] Step 7: SvelteKit login + streaming chat UI
- [x] Step 8: Tool-calling foundation
- [x] Step 9: Permission ruleset (allow/deny/ask)
- [x] Step 10a: Scheduler (cron jobs, headless runs)
- [x] Step 10b: Multi-user hardening (admin-managed users, session APIs)
- [x] Step 10c: Deploy (Docker, adapter-node)
- [x] Step 11: Per-user LLM settings (provider/key/model in DB, per-request model override)
- [x] Step 12: Global admin-managed provider profiles (named gateways, per-profile model lists, per-user default preference, chat picker)

Rationale for these choices lives in `docs/decisions.md`.
