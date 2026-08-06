# oracle

> **Work in progress.** This project is in draft and under active development.
> Don't run it yet — things will break and interfaces will change. Check back later.

Autonomous personal assistant. Monorepo with a Go backend and a SvelteKit frontend.

## Layout

```
apps/
  api/   Go backend (module: github.com/sayem314/oracle/apps/api)
  web/   SvelteKit frontend (bun workspace)
```

## Prerequisites

- Go 1.26+
- bun 1.3+

## Getting started

```bash
bun install          # install frontend deps (links the workspace)

# backend
go run ./apps/api/cmd/oracle

# frontend
bun run dev          # SvelteKit dev server on :5173
```

## Commands

| Command         | Description                    |
| --------------- | ------------------------------ |
| `bun run dev`   | Run the SvelteKit dev server   |
| `bun run build` | Build the frontend             |
| `bun run check` | Type-check the frontend        |
| `go build ./apps/api/...`              | Build all Go packages |
| `go build -o bin/oracle ./apps/api/cmd/oracle` | Build the API binary into `bin/` |
| `go test ./apps/api/...`               | Run Go tests          |

## Backup & restore (Docker)

All state lives in the SQLite file on the `oracle-data` volume. Snapshots are
consistent even while the API is running (SQLite online backup, WAL-safe):

```bash
make backup                                # snapshots into backups/, keeps 14
make backup KEEP=30                        # keep more snapshots
make backup-restore FILE=backups/oracle-20260806-120000.db
```

The API briefly stops during a restore. Backups contain plaintext LLM API keys,
so treat the `backups/` directory like a secret store.
