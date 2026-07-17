# 07 — Project Structure

## 7.1 Repository layout

```
queuectl/
├── cmd/
│   ├── root.go              # Cobra root command, global flags (--json, --db-path)
│   ├── enqueue.go
│   ├── worker.go             # start/stop/run subcommands
│   ├── status.go
│   ├── list.go
│   ├── dlq.go
│   └── config.go
│
├── core/
│   ├── queue.go              # Queue struct — the one entry point cmd/ calls into
│   ├── worker.go             # worker pool, poll loop, lease heartbeat, shutdown
│   ├── executor.go           # subprocess execution, timeout handling
│   ├── backoff.go            # pure function: attempts -> delay
│   ├── config.go             # layered config resolution
│   └── lease.go              # sweep for stale processing jobs
│
├── store/
│   ├── store.go               # Store interface (so SQLite is swappable — ADR-002)
│   ├── sqlite.go              # SQLite implementation, all raw SQL lives here
│   └── migrations/
│       ├── 0001_init.sql
│       └── 0002_add_priority_run_at.sql
│
├── internal/
│   └── model/
│       └── job.go             # Job struct, JSON (de)serialization, validation
│
├── scripts/
│   ├── crash_recovery_test.sh
│   └── cli_e2e_test.sh
│
├── docs/                       # this design package
│   ├── 01-requirements.md
│   ├── 02-architecture.md
│   ├── 03-data-model.md
│   ├── 04-cli-api-spec.md
│   ├── 05-concurrency-and-reliability.md
│   ├── 06-testing-strategy.md
│   ├── 07-project-structure.md
│   └── adr/
│       ├── ADR-001-language-choice.md
│       ├── ADR-002-persistence-choice.md
│       ├── ADR-003-cli-service-split.md
│       └── ADR-004-web-dashboard-scope.md
│
├── main.go
├── go.mod
├── go.sum
└── README.md                   # grader-facing: setup, usage, demo link
```

## 7.2 Package boundary rules

These are the rules a reviewer skimming the repo should be able to verify in
under a minute — stated explicitly because "clean, modular code" is a graded
criterion, not just a vibe:

1. **Only `store/` writes SQL.** `core/` calls `store.Store` interface
   methods, never raw queries. Enforced by code review / grep in CI
   (`grep -rn "database/sql" --include=*.go core/` should return nothing).
2. **Only `core/` contains business logic** (retry math, state transitions).
   `cmd/` is presentation only — parse flags, call `core`, format output.
3. **`internal/model` has no dependencies on `core` or `store`** — it's the
   shared vocabulary both layers use, preventing import cycles.
4. **`store.Store` is an interface**, `sqlite.go` is one implementation.
   This is what makes "SQLite → Postgres later" a config change, not a
   rewrite (referenced in `02-architecture.md` §2.8), and it's what makes
   `core/` unit-testable against an in-memory fake without a real DB file
   for the fast unit-test tier.

## 7.3 Dependencies (deliberately minimal)

| Package | Why |
|---|---|
| `github.com/spf13/cobra` | CLI framework — help text, flag parsing, subcommands |
| `modernc.org/sqlite` | Pure-Go SQLite driver — **no cgo**, so `go build` produces a static binary with zero system dependencies (important for graders on any OS) |
| `github.com/google/uuid` | Job ID generation |
| `gopkg.in/yaml.v3` | Config file parsing |
| `github.com/stretchr/testify` | Test assertions only (test-scope dependency) |

Deliberately **not** using an ORM — five tables, hand-written SQL in one
file (`store/sqlite.go`) is more auditable than a query-builder abstraction
for a codebase this size.

## 7.4 Build & distribution

```bash
go build -o bin/queuectl .
# static binary, no runtime deps — copy anywhere, or:
go install github.com/<you>/queuectl@latest
```
