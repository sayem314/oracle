# Decision Log

Lightweight ADRs. Latest first.

## 2026-08-03: Storage layer, sqlc + goose + modernc.org/sqlite

- Chose sqlc (SQL-first codegen) over ent and bob after an activity audit on 2026-08-03. sqlc and bob both commit daily (last commits 2026-08-02), jet monthly (2026-06-20), ent only in bursts (2026-05-31, v0.14.6 in March, low-maintenance mode), squirrel (2024-02-27) and goqu (2023-12-14) are dormant. sqlc is company-backed and rewrote its SQLite parser two days before this decision.
- Oracle's storage queries are fixed-shape CRUD over sessions and messages, so bob's dynamic builder would go unused. sqlc ships zero runtime dependency, and raw SQL is the interface, so there is no escape hatch to outgrow.
- sqlc marks SQLite as beta in its support matrix. Accepted, since the schema is simple and the parser just got heavy investment. Revisit if it bites.
- goose v3 applies migrations from an embedded FS at startup: single binary, no migration CLI in production. Migration files carry Up and Down, and sqlc reads the same directory for its schema (verified the two coexist).
- `modernc.org/sqlite` over `mattn/go-sqlite3`: pure Go, no cgo, self-hosted deploys stay a single binary.
- Timestamps are `DATETIME` columns defaulting to `CURRENT_TIMESTAMP`. modernc returns RFC3339 strings that `database/sql` converts to `time.Time` natively, verified with a throwaway program before committing to the schema.
- `store.Store` is an interface over the sqlc-generated package. Handlers and later LimenAuth wiring depend on the interface, so tests can use hand-written fakes per the testify convention.

## 2026-08-02: Global zerolog logger with TTY-aware output

- Use the `github.com/rs/zerolog/log` global, configured once in `main`. No logger plumbing through handlers.
- `ConsoleWriter` (colored) when stdout is a TTY, JSON otherwise. Detected with `golang.org/x/term`.
- zerolog docs confirm `ConsoleWriter` never auto-detects terminals, so the check is ours to make.
- `x/term` chosen over `mattn/go-isatty`: Go team maintained, one call.

## 2026-08-02: koanf-native .env parsing, no godotenv

- koanf's `env` provider reads process env only. The `.env` file is handled by `os.ReadFile` + koanf's `dotenv` parser, with keys transformed (`ORACLE_LOG_LEVEL` becomes `log_level`).
- Precedence falls out of `Load()` order: defaults, `.env`, `ORACLE_*` env vars, plain `PORT` fallback. Pinned by tests.
- Validation is explicit: `strconv.Atoi` for port, `zerolog.ParseLevel` for level. No mapstructure `DecoderConfig` needed (koanf accessors weak-decode internally). `go-playground/validator` gets adopted when config grows.

## 2026-08-02: Env prefix ORACLE_

- The process environment is a flat shared namespace. The prefix avoids collisions and makes oracle's variables identifiable in `.env`, compose files, and manifests.
- Plain `PORT` is honored as a PaaS-compatibility fallback. `ORACLE_PORT` wins when both are set.

## 2026-08-02: Test conventions

- Official testify best practice: `require` when a failure would make subsequent code unsafe, `assert` for independent checks so all failures report together.
- No `testify/suite` (use `t.Run` subtests), no `testify/mock` (hand-written fakes against our own interfaces).
- `testifylint` enabled in golangci-lint to keep usage consistent.

## 2026-08-02: Route and handler organization

- Handlers live one file per domain inside `internal/server`, registered inline in `New()`. Splitting into a routes package or `internal/handler` is deferred until the inline list hurts.

## 2026-08-02: Fiber v3 as the HTTP server

- Chosen router/framework. API routes will mount under `/api/v1`. LimenAuth (net/http based) will be mounted later through Fiber's official `middleware/adaptor`.

## 2026-08-02: Monorepo scaffold

- `apps/api` (one Go module per app, `go.work` at root) + `apps/web` (SvelteKit, bun workspace).
- A Makefile unifies Go and frontend commands. Lint stack: golangci-lint v2 + gofmt, prettier + svelte-check.
- Editor configs committed for Zed and VS Code.

## Planned (not built yet)

- LimenAuth shares the same SQLite `*sql.DB`.
- LimenAuth for auth (credential-password + JWT), mounted at `/auth/*`, guarding `/api/*` via `adaptor.ConvertRequest`. Kept behind our own `auth` interface due to project youth.
- LLM access via `openai/openai-go` + `anthropics/anthropic-sdk-go` behind a custom `Provider` interface, with a mock implementation for tests.
- API-first: OpenAPI spec. Codegen approach evaluated per step.
