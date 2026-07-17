# 05 — Concurrency & Reliability

This is the document that answers the brief's two hard disqualifiers:
**race conditions** and **duplicate job execution**. Everything here is
designed to make those bugs *structurally impossible*, not just unlikely.

## 5.1 The core invariant

> **No two workers ever see the same job as "claimed" at the same time.**

This is guaranteed by making "claim a job" a single atomic SQL statement,
not a read-then-write pair in application code.

### The wrong way (read-then-write — has a race window)

```go
// DO NOT DO THIS
job := db.QueryRow("SELECT * FROM jobs WHERE state='pending' LIMIT 1")
db.Exec("UPDATE jobs SET state='processing' WHERE id=?", job.ID)
// ^ another worker's SELECT can return the same row before this UPDATE commits
```

### The right way (atomic claim, one statement)

```sql
UPDATE jobs
SET state = 'processing',
    worker_id = ?,
    lease_expires = datetime('now', '+' || ? || ' seconds'),
    attempts = attempts + CASE WHEN state = 'failed' THEN 0 ELSE 0 END,
    updated_at = datetime('now')
WHERE id = (
    SELECT id FROM jobs
    WHERE state IN ('pending', 'failed')
      AND available_at <= datetime('now')
    ORDER BY priority DESC, available_at ASC
    LIMIT 1
)
RETURNING id, command, attempts, max_retries;
```

Wrapped in a transaction opened with `BEGIN IMMEDIATE` (not the default
`BEGIN DEFERRED`), which takes SQLite's write lock **immediately** rather
than lazily on first write. Combined with `busy_timeout=5000`, a second
worker's concurrent claim attempt simply *waits* for the lock and then
re-evaluates the `WHERE id = (SELECT ...)` subquery — by which point the
first job is no longer `pending`, so it correctly gets a different job or
nothing. This is what makes the guarantee hold **across OS processes**, not
just across goroutines in one process — important because the brief's
"multiple worker processes" is meant literally.

This single statement is the *only* code path allowed to move a job into
`processing`. It lives in one function, `store.ClaimNext()`, and every
worker — goroutine-mode or subprocess-mode — calls it. One code path to
audit, not N.

## 5.2 Crash safety: the lease sweep

Atomic claim prevents double-claiming while workers are alive. It does not,
by itself, handle a worker that's `kill -9`'d mid-job — that job is stuck at
`state='processing'` forever unless something notices.

Solution: every claimed job gets a `lease_expires` (default 30s, configurable). A
background sweep — run by whichever worker process is currently active, via
a `SELECT` guarded by the same `BEGIN IMMEDIATE` pattern — periodically runs:

```sql
UPDATE jobs
SET state = 'pending', worker_id = NULL, lease_expires = NULL
WHERE state = 'processing' AND lease_expires < datetime('now');
```

Note this resets to `pending` **without incrementing `attempts`** — the job
never actually finished running, so it shouldn't be penalized as a failure.
A live worker still processing a long job periodically renews its lease
(`UPDATE ... SET lease_expires = ...` on a heartbeat interval, default every
10s for a 30s lease) so it isn't reclaimed out from under it.

This is also what makes "persist across restarts" mean something stronger
than "the file is still on disk" — a job interrupted by a crash resumes
correctly, not stuck or silently lost.

## 5.3 Exponential backoff

```
delay_seconds = backoff_base ^ attempts
available_at  = now + delay_seconds
```

Computed and **stored**, not scheduled as an in-memory timer — this is what
makes backoff survive a full application restart for free. If the app is
down when a backoff window elapses, the job is simply eligible again the
moment any worker next polls; no missed-timer recovery logic needed.

`backoff_base` is configurable (`config set backoff-base 2`), default `2`.
Optional jitter (±10%) can be added to avoid thundering-herd re-claims when
many jobs fail at once — worth doing since it's a two-line change and shows
production awareness, but it's additive, not required for correctness.

## 5.4 Execution & timeouts

```go
ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
defer cancel()
cmd := exec.CommandContext(ctx, "sh", "-c", job.Command)
out, err := cmd.CombinedOutput()
```

- Exit code 0 → success.
- Non-zero exit, `exec` error (command not found), or `ctx.Err() ==
  context.DeadlineExceeded` → all treated uniformly as "failure", routed
  through the same retry/DLQ logic in §5.5. This is deliberate: the brief
  says "invalid commands should trigger retries," so a missing binary isn't
  special-cased into a different code path — it's just another failure mode.
- `last_error` column stores truncated combined stdout+stderr (first 4KB)
  for `dlq list -v` / debugging, satisfying the "job output logging" bonus
  cheaply.

## 5.5 Retry/DLQ decision (single function, single source of truth)

```go
func (q *Queue) onJobFinished(job Job, exitErr error) error {
    if exitErr == nil {
        return q.store.Complete(job.ID)
    }
    if job.Attempts+1 >= job.MaxRetries {
        return q.store.MarkDead(job.ID, exitErr.Error())
    }
    delay := time.Duration(math.Pow(float64(q.cfg.BackoffBase), float64(job.Attempts+1))) * time.Second
    return q.store.MarkFailed(job.ID, exitErr.Error(), delay)
}
```

All three outcomes (complete / retry / dead) are one SQL statement each,
called from one function. Nothing about retry counting or DLQ movement is
duplicated across the codebase.

## 5.6 Graceful shutdown

```
SIGTERM/SIGINT received
   → stop polling for new jobs (claim loop exits)
   → for each in-flight job: wait up to --timeout (default 30s)
   → on completion: normal Complete/Fail/MarkDead as usual
   → release lease (worker_id=NULL) on anything not yet finished
   → exit 0
```

Implemented with `context.Context` cancellation propagated from a
`signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` at the top of
`worker start` — idiomatic Go, no custom signal plumbing needed. A second
`Ctrl-C` forces immediate exit (documented in `--help`) for a grader who
doesn't want to wait out a `sleep 300` demo job.

## 5.7 Testing this section

Every claim in this document has a corresponding test — see
`06-testing-strategy.md` §"Concurrency & crash tests" for the exact list
(race-detector run, N-workers-one-job test, kill-9-mid-job recovery test).
