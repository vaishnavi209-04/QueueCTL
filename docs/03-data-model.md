# 03 — Data Model

## 3.1 Storage engine

**SQLite, WAL mode, single file at `~/.queuectl/queue.db`** (overridable via
`--db-path` / config). See ADR-002 for why SQLite over a raw JSON file or an
embedded KV store.

Pragmas set on every connection open:

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
```

`busy_timeout` matters more than it looks: it's what makes concurrent
processes hitting the same file *wait and retry* instead of erroring out
under write contention.

## 3.2 `jobs` table

```sql
CREATE TABLE jobs (
    id            TEXT PRIMARY KEY,           -- UUID, client-supplied or generated
    command       TEXT NOT NULL,
    state         TEXT NOT NULL CHECK (state IN
                     ('pending','processing','completed','failed','dead')),
    attempts      INTEGER NOT NULL DEFAULT 0,
    max_retries   INTEGER NOT NULL,
    priority      INTEGER NOT NULL DEFAULT 0,  -- bonus: priority queue
    run_at        TEXT NOT NULL,               -- bonus: delayed jobs; = created_at if immediate
    available_at  TEXT NOT NULL,               -- when eligible to be claimed next (backoff lands here)
    worker_id     TEXT,                        -- who currently holds the lease (NULL unless processing)
    lease_expires TEXT,                        -- processing jobs must renew or be reclaimed
    last_error    TEXT,                        -- captured stderr/exit info from most recent failure
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX idx_jobs_claimable ON jobs (state, available_at, priority);
CREATE INDEX idx_jobs_state     ON jobs (state);
```

Design notes:

- **No separate `dlq` table.** `state='dead'` *is* the DLQ. `dlq list` is
  `SELECT * FROM jobs WHERE state='dead'`. This removes an entire class of
  bug (job present in both tables, or in neither, after a crash mid-move).
- **`lease_expires`, not just `worker_id`.** A worker can die without
  releasing its claim. The sweep in §05 reclaims any `processing` row whose
  lease has expired — this is what makes persistence-across-crash actually
  safe, not just persistence-across-clean-restart.
- **Timestamps as TEXT (ISO-8601), not INTEGER.** Matches the brief's sample
  job JSON (`"created_at": "2025-11-04T10:30:00Z"`) and keeps `jobs` rows
  human-readable when inspected with the `sqlite3` CLI during grading.

## 3.3 `config` table

```sql
CREATE TABLE config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

Seeded with defaults on first run (`max_retries=3`, `backoff_base=2`,
`job_timeout_seconds=300`, `lease_seconds=30`). `config set` upserts here;
`core/config.go` reads this table and merges over the layered order in
`02-architecture.md` §2.6.

## 3.4 State machine

```
        enqueue
           │
           ▼
      ┌─────────┐   claimed by worker    ┌────────────┐
      │ pending │ ───────────────────▶  │ processing │
      └─────────┘                        └─────┬──────┘
           ▲                                    │
           │ retry (backoff elapsed,             │ exit 0
           │ attempts < max_retries)             ▼
      ┌─────────┐  exit ≠0 / timeout      ┌────────────┐
      │ failed  │ ◀───────────────────────┤ completed  │
      └────┬────┘                          └────────────┘
           │ attempts >= max_retries
           ▼
      ┌─────────┐   dlq retry
      │  dead   │ ───────────────▶ back to pending (attempts reset)
      └─────────┘
```

Legal transitions only — enforced in `store/sqlite.go`, not left to callers
to get right:

| From | To | Trigger |
|---|---|---|
| — | `pending` | `enqueue` |
| `pending` | `processing` | atomic claim (§05) |
| `failed` | `processing` | atomic claim, once `available_at` has elapsed |
| `processing` | `completed` | command exit 0 |
| `processing` | `failed` | command exit ≠0, or timeout, and `attempts < max_retries` |
| `processing` | `dead` | command exit ≠0, or timeout, and `attempts >= max_retries` |
| `processing` (stale lease) | `pending` | reclaim sweep, no attempts increment (worker died, job didn't really run) |
| `dead` | `pending` | `dlq retry` |

Any other transition is a programming error and the store layer returns an
error rather than silently applying it — this is intentionally strict so
tests catch it immediately.

## 3.5 Sample row lifecycle (for the demo recording)

```
enqueue → id=job1 state=pending attempts=0
claim   → state=processing worker_id=w1 lease_expires=+30s
fail    → state=failed attempts=1 available_at=+2s   (backoff_base=2, 2^1)
claim   → state=processing worker_id=w2 lease_expires=+30s
fail    → state=failed attempts=2 available_at=+4s   (2^2)
claim   → state=processing worker_id=w1
fail    → state=dead attempts=3        (max_retries=3 exhausted)
dlq retry job1 → state=pending attempts=0
```
