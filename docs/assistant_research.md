# Assistant Research: Hermes vs OpenClaw

Comparative study of two large open-source personal-assistant monorepos, read
as design input for oracle. Both are MIT-licensed. Neither uses Go (Hermes is
Python, OpenClaw is TypeScript), so this is a **concept/IP adoption exercise**,
not vendoring.

Sources: `NousResearch/hermes-agent` (v0.20) and `openclaw/openclaw` (2026.7.2),
cloned shallow to a scratch dir for inspection. Numbers are git-tracked `wc`
counts (blank + comment lines included), filtered consistently.

## Scale (side by side)

| Metric | Hermes (Python) | OpenClaw (TypeScript) |
|---|---|---|
| All tracked LOC | 2.75M / 8,530 files | 9.68M / 31,076 files |
| Primary language | Python 1.49M / 3,846 files | TS 7.16M / 24,873 files |
| Production LOC (no tests) | ~843K / 1,171 files | ~3.15M / 15,071 files |
| Test LOC | ~645K / 2,680 files | ~4.01M / 9,796 files |
| Test : Prod ratio | 0.76 | 1.27 |
| Runtime area (loop+gateway+tools) | ~725K / 892 files | ~3.03M / 14,600 files |
| Go | 0 (only a C `fts5_cjk` SQLite ext) | 0 in runtime (29 files = docs-i18n tool) |
| Frontend | ~457K TS/TSX (web, TUI, desktop) | in the ~3M (Lit + native apps) |

Hermes concentrates logic in big monoliths around 720 LOC/file on average.
`gateway/run.py` is 27K lines and `cli.py` is 18K. OpenClaw fragments into many
small modules (~209 LOC/file) with enormous individual test files (up to 9K+
lines). Opposite maintainability bets. **Hermes is leaner per feature. OpenClaw
is engineered to scale.**

## What each is, in one line

- **Hermes.** A single self-improving agent with a built-in learning loop that
  authors skills from experience, curates memory, and nudges itself. It runs in a
  gateway that multiplexes ~25 messaging platforms plus a TUI/desktop shell.
- **OpenClaw.** A single-operator, gateway-centric product platform. One
  long-running Gateway process owns all state (SQLite) and serves stateless
  WebSocket-RPC clients (CLI, Control UI, native apps). Channels/providers are
  declarative plugins. Memory has baked-in untrusted-input discipline.

## Design ideas worth taking into oracle

Ranked by leverage-to-effort. Oracle's own loop/permission/persistence choices
(chat engine, `Ruleset`, sqlc+goose) already validate the shape of several of
these. The gaps are what oracle lacks.

### 1. Tool-result "taint" + memory provenance (highest leverage)
OpenClaw tags tool results that came from the network (`resultContentSource =
"network"`) and treats any turn they feed as untrusted. Memory writes carry
provenance (`owner/agent/untrusted/system`) stored in **SQLite columns, not
prose**, so the model cannot forge it, and untrusted content is barred from
curated memory. Hermes likewise threat-scans memory at write/load time and
keeps a frozen snapshot injected into the system prompt.
- Relevance: oracle is multi-user (Step 10b) and runs a permission ruleset
  (Step 9). No memory or provenance layer yet. When memory arrives, build the
  provenance column in from the start.
- Cost: cheap now. Expensive to retrofit later.

### 2. Separate the loop into reusable core + product harness
OpenClaw's core is a generic `agentLoop()` (`packages/agent-core/agent-loop.ts`)
returning an event stream. The product "harness" underneath it adds session
locking, compaction, restart recovery, tool-result truncation, and
critical-tool-loop detection. Hermes instead folds everything into one giant
`AIAgent` + `conversation_loop.py` (7.3K lines). The loop itself is, in both,
the same model→tool→model shape oracle's `chat.Run` already implements (both
cap iterations, both round with a tool-round limit).
- Relevance: oracle should grow toward a cleanly separated core loop + product
  harness rather than a widening `chat.go`. Also borrow **critical-tool-loop
  detection** (works) and **tool-result truncation** (cap tool output fed back
  to the model). Both are cheap, real safeguards oracle lacks.
- Cost: low. Aligns with the existing small-package convention.

### 3. Approvals / operator-scopes pattern
OpenClaw gates every Gateway RPC behind fine-grained `operator.read/write/
approvals/admin` scopes and layer device + capability pairing. Approvals exist
for risky tools ("exec-approvals"). Hermes has an approval-isolation mode in
its ACP adapter.
- Relevance: validates oracle's Step 9 ask/deny tool permissions. The gap is
  **scope discipline on the API itself**. Worth reviewing
  `requireSession`/`requireAdmin` grouping as endpoints multiply.
- Cost: low.

### 4. Skills as progressive-disclosure markdown
Both expose skills as `SKILL.md` with YAML frontmatter and inject only a name +
short description index into the system prompt, loading the full body on demand
against the directory. Hermes can author skills from task experience
(`learn_prompt.py`, a Curator). OpenClaw ships a skill catalog + workshop.
- Relevance: cheap, high value once oracle has a prompt-defined agent. The
  format is stable and interoperable (agentskills.io standard actually
  originated around Hermes).
- Cost: low. Defer until oracle's tool set justifies it.

### 5. Outbound channel idempotency ledger
OpenClaw routes channel messages through `conversation_deliveries` with a
`message_hash` and reply correlation, so a retried send cannot duplicate.
- Relevance: only if/when oracle adds messaging channels (Telegram/WhatsApp).
  Currently API-first (web, mobile later), so **defer**.

### 6. Storage doctrine: one DB, heavy use of SQLite
Both converge on SQLite + FTS5 for cross-session search and plain files for
curated memory. OpenClaw splits shared-state vs per-agent DBs with schema in
`*.sql`. Hermes uses one `state.db` with FTS5 (and a C extension for CJK).
- Relevance: oracle is already SQLite via sqlc+goose. Same philosophy. The
  next natural addition is **FTS5 for cross-session recall**, mirroring
  Hermes' `session_search`.

## What oracle should *not* copy

- The file-count / big-monorepo sprawl. Both projects are 2-3 orders of
  magnitude larger than oracle (~17K tracked LOC). Oracle stays small by design
  ("Small steps: only build what the current step needs").
- OpenClaw's 4M-line test harness and per-subsystem×channel×provider QA
  taxonomy. Oracle's existing mirror-source, black-box package tests and ~1.3
  test:prod ratio are the right weight for its size.
- Either project's single-language commitment as a reason to change oracle's
  Go+SvelteKit choice. Neither uses Go. The stack decision stands.

## Bottom line

- **LOC efficiency:** Hermes wins (compact, 0.76 test ratio). **Scale
  efficiency:** OpenClaw wins (modular, declarative, heavily tested). Runtime
  speed is equal. Both are I/O-bound on LLM latency, so LOC says nothing about
  speed here.
- The highest-value, lowest-effort borrows for oracle are **#1
  provenance/taint** (when memory arrives), **#2 loop/harness separation + tool
  truncation + critical-loop guard**, and **#3 scope discipline on endpoints**.
  Skills (**#4**) is a natural follow-on.

## Snapshots

Raw line counts referenced above:

```
HERMES (python):  all 1,486,968 / 3,846 files; prod ~843,459; tests ~644,696
OPENCLAW (ts):    all 7,161,535 / 24,873 files; prod ~3,145,578; tests ~4,014,670
```

Update the numbers here only by re-running the inspection (shallow clones to a
scratch dir. No tool in this repo automates it).
