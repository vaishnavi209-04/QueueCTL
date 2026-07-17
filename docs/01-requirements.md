# 01 — Requirements & Traceability

## 1.1 Purpose

Translate the assignment brief into unambiguous, testable requirements, and
map every requirement to where it's satisfied in the design. If a reviewer
asks "did you handle X", this table is the answer.

## 1.2 Functional requirements

| ID | Requirement | Source | Satisfied by |
|----|-------------|--------|---------------|
| FR-1 | Enqueue a job via `queuectl enqueue '<json>'` | Brief §CLI | `04-cli-api-spec.md` §Enqueue |
| FR-2 | Start N worker processes via `worker start --count N` | Brief §CLI | `02-architecture.md` §Worker pool |
| FR-3 | Stop workers gracefully, finishing in-flight jobs | Brief §Worker Mgmt | `05-concurrency-and-reliability.md` §Graceful shutdown |
| FR-4 | `status` shows counts by state + active worker count | Brief §CLI | `04-cli-api-spec.md` §Status |
| FR-5 | `list --state X` filters jobs by state | Brief §CLI | `04-cli-api-spec.md` §List |
| FR-6 | `dlq list` / `dlq retry <id>` | Brief §DLQ | `04-cli-api-spec.md` §DLQ |
| FR-7 | `config set <key> <value>` for max-retries, backoff-base, etc. | Brief §Config | `03-data-model.md` §Config table |
| FR-8 | Execute job `command` as a subprocess; exit 0 = success | Brief §Execution | `05-concurrency-and-reliability.md` §Execution |
| FR-9 | Automatic retry with exponential backoff on failure | Brief §Retry | `05-concurrency-and-reliability.md` §Backoff |
| FR-10 | Move to DLQ after `max_retries` exhausted | Brief §Retry | `03-data-model.md` §State machine |
| FR-11 | Jobs persist across process restart | Brief §Persistence | `03-data-model.md` §Storage engine |
| FR-12 | Multiple workers run in parallel with no duplicate execution | Brief §Worker Mgmt | `05-concurrency-and-reliability.md` §Atomic claim |

## 1.3 Non-functional requirements

| ID | Requirement | Rationale | Design response |
|----|-------------|-----------|------------------|
| NFR-1 | No race conditions under concurrent workers | Explicit disqualifier in brief | Single atomic SQL claim (ADR-003), no in-app locks to forget |
| NFR-2 | Survive `kill -9` mid-job without corrupting queue state | "Persist across restarts" implies crash safety, not just clean shutdown | WAL-mode SQLite + `processing` jobs are re-claimable via a stale-lease sweep (see reliability doc) |
| NFR-3 | Zero hardcoded config | Explicit disqualifier | Layered config: defaults → file → CLI flags |
| NFR-4 | CLI usable without reading source | Evaluation: "Documentation" 10%, "clean CLI" deliverable | Cobra auto-generated `--help`, consistent verb-noun grammar |
| NFR-5 | Understandable/modular for a reviewer skimming in <15 min | Evaluation: "Code Quality" 20% | Package-per-concern layout, see `07-project-structure.md` |
| NFR-6 | Single-binary, no external services to install | Ease of grading | Embedded SQLite (`modernc.org/sqlite`, pure Go, no cgo) |

## 1.4 Explicit non-goals (scope control)

Stating these up front avoids scope creep and shows deliberate prioritization
to a reviewer:

- **Not distributed.** One queue = one SQLite file on one machine. Workers
  are goroutines/OS processes on that machine, not across a network. Brief
  doesn't ask for distribution; adding it would dilute effort on core
  requirements.
- **Not a generic task scheduler.** `run_at` scheduling is a listed *bonus*,
  implemented last, after all must-haves are solid.
- **No auth/multi-tenancy.** Single-user local tool.

## 1.5 Bonus features — priority order

Given limited time, implement in this order (highest reviewer-visible value
first, per the brief's weighting):

1. Job output logging (cheap, high visibility when demoing)
2. Job timeout handling (closes an obvious robustness gap)
3. Metrics/execution stats (`status` already has the data; extending is cheap)
4. Scheduled/delayed jobs (`run_at`)
5. Minimal web dashboard (nice-to-have, not required — see ADR-004)
6. Priority queues (highest implementation cost relative to grading weight)

## 1.6 Evaluation-criteria alignment

| Criterion | Weight | How this design targets it |
|---|---|---|
| Functionality | 40% | Every FR above has a named CLI command and a test in `06-testing-strategy.md` |
| Code Quality | 20% | Layered architecture (`02`), explicit package boundaries (`07`) |
| Robustness | 20% | Dedicated concurrency doc (`05`), crash-recovery sweep, race detector in CI |
| Documentation | 10% | This package + generated `--help` + top-level `README.md` for graders |
| Testing | 10% | Unit + integration + a scripted "chaos" test that kills workers mid-job |
