# Decision Log

Lightweight ADRs. Latest first.

## 2026-08-03: Limen auth, first-user gate, and session middleware

- Limen (`github.com/thecodearcher/limen`, better-auth style) supplies credential-password auth, sessions, and rate limiting. It is net/http based, so it mounts at `/auth/*` through Fiber's official `middleware/adaptor`, and the `/api/v1` session middleware converts requests back with `adaptor.ConvertRequest` to call `auth.GetSession`. Exactly the shape the Fiber ADR anticipated.
- Limen sits behind our own `auth.Auth` interface (`Handler`, `UserID`, `HasUsers`). Handlers and middleware depend on the interface, not the library, per project convention.
- The SQL adapter (`adapters/sql`, sqlx under the hood) wraps the same `*sql.DB` as our store, so auth and chat share one SQLite pool as planned.
- Limen's default tables (`users`, `sessions`) collide with our chat `sessions` table, so its schema is renamed to `auth_users`, `auth_sessions`, `auth_verifications` via Limen's schema config. `accounts` (OAuth-only) and `rate_limits` (limiter defaults to the in-memory cache store) are not created until needed.
- Migrations stay goose-managed and hand-written for SQLite: Limen's CLI generates migrations for postgres/mysql only. Limen writes `time.Time` through modernc's default `t.String()` encoding, and modernc parses `DATETIME` columns back to `time.Time` on read, so the round trip works without DSN changes; time columns are declared `DATETIME` for that reason.
- Sessions are cookie-based with bearer tokens enabled (`WithBearerEnabled`): the web app uses cookies now, and mobile/API clients can use `Authorization: Bearer` later without server changes. Email verification is disabled; there is no mail infrastructure on a self-hosted box.
- Sign-up is open until the first user exists, then permanently locked (403). A gate in front of `/auth/*` checks the user count on sign-up requests, and a Limen additional-fields callback stamps `role` at creation: the users table is empty exactly when the first account is created, so it becomes `admin`. Known edge: two concurrent first sign-ups could both see an empty table; harmless on a single-user box. Role is stamped now but consumed by nothing yet; permission work lands with Step 9.
- The sessions table `user_id` changed from TEXT to INTEGER by editing migration 00001 in place, matching Limen's int64 user IDs; safe because the app is pre-launch with no deployed databases. Chat session creation now carries the authenticated user, and accessing a session owned by someone else returns 404 (no existence leak).
- Limen's `BaseURL` stays at its localhost default: it only matters for building email/OAuth URLs, both unused. Revisit if either lands.
- Config gains `auth_secret` (ORACLE_AUTH_SECRET), required, exactly 32 bytes, validated at load like other config.

## 2026-08-03: SSE chat endpoint, pre-stream validation and named events

- `POST /api/v1/chat` streams over Fiber v3's built-in `sse` middleware, which owns the transport (headers, framing, flushing, 15s heartbeat comments, disconnect detection). Application events stay ours: named events `start`, `delta`, `done`, `error`, JSON payloads. Named events are self-describing for the Step 7 SvelteKit client, which consumes them via fetch + ReadableStream anyway (EventSource is GET-only, so no loss).
- Two-phase handler: a wrapper validates the body and resolves the session *before* the stream opens, so bad requests get proper HTTP 4xx JSON (`message is required`, `session not found`) instead of a 200 carrying an error event. The SSE `error` event is reserved for failures mid-stream, matching OpenAI's streaming behavior. The wrapper hands off through `c.Locals`.
- The user message is persisted before streaming; the assistant message only after a complete stream. A failed run leaves the user's turn visible and no partial assistant text behind.
- Sessions are created implicitly when `session_id` is omitted. `user_id` is an empty placeholder until LimenAuth lands in Step 6 and supplies real identity.
- Fiber's default error handler renders text/plain, so `server.New` installs a minimal JSON error handler (`{"error": "..."}`); API-first means errors are JSON too.
- History is the last 1000 messages for now. Token-aware truncation is a later concern.

## 2026-08-03: Single protocol, OpenAI-compatible only

- Supersedes the dual protocol decision below. The Anthropic-compatible implementation and `anthropics/anthropic-sdk-go` are removed; `llm.Provider` now ships OpenAI-compatible (`openai/openai-go`) plus the deterministic mock.
- The OpenAI protocol is the de facto lingua franca: OpenAI, OpenRouter, LiteLLM, Together, Groq, Mistral, DeepSeek, vLLM, and Ollama all speak it natively, so one protocol plus a configurable base URL reaches everything, including local inference, which matters for a self-hosted assistant. Anthropic models stay reachable through those gateways in OpenAI format.
- A second protocol adapter doubled code, tests, and SDK quirks without unlocking any provider a gateway cannot already serve. Not worth the burden.
- Multi-provider support gets revisited only if a need appears that gateways cannot cover, for example native Anthropic prompt caching or cache control once tool calling lands.
- Everything else from the superseded entry stands: streaming-first iterator, our own domain types, `System` as a first-class request field, mock default with fail-fast config validation, hermetic `httptest` SSE tests.

## 2026-08-03: LLM provider layer, dual protocol and streaming first

- `llm.Provider` ships with three implementations: OpenAI-compatible (`openai/openai-go` v3), Anthropic-compatible (`anthropics/anthropic-sdk-go`), and a deterministic mock. The two real protocols cover the market: hundreds of providers (OpenRouter, LiteLLM, vLLM, Ollama, gateways) expose one of these two APIs, so a configurable base URL on each unlocks them without per-provider code.
- The interface is streaming first (`Next/Current/Err/Close` iterator), because SSE chat is the primary consumer one step away. Non-streaming callers accumulate chunks. Empty protocol events are skipped in the adapters, so every `Chunk` carries a delta or a finish reason.
- Domain types (`Message`, `Request`, `Chunk`) are ours; SDK types never cross the interface. `System` is a first-class request field: Anthropic takes it as a parameter, OpenAI as a message, and papering over that later would leak.
- Mock is the default provider: zero-config `make dev`, deterministic tests. Real providers require `llm_api_key` and `llm_model` at config validation, fail fast at boot.
- Anthropic requires `max_tokens`; defaulted to 4096 for now, surfaced on the request when tuning matters.
- Hermetic tests: both real providers are tested against `httptest` servers speaking raw SSE. Anthropic's typed stream dispatches on the `event:` line, not just the JSON `type`, so fakes must emit both (matches production responses).

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

- Per-user LLM credentials: provider, API key, base URL, and model move from env config to per-user settings in the database (auth has landed), and chat requests can carry a model override so users switch models mid-conversation, the way modern agent and IDE tools do. Whether env config is removed or kept as an admin-supplied default is decided then.
- API-first: OpenAPI spec. Codegen approach evaluated per step.
