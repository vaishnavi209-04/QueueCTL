CREATE TABLE jobs (
    id            TEXT PRIMARY KEY,
    command       TEXT NOT NULL,
    state         TEXT NOT NULL CHECK (state IN ('pending','processing','completed','failed','dead')),
    attempts      INTEGER NOT NULL DEFAULT 0,
    max_retries   INTEGER NOT NULL,
    priority      INTEGER NOT NULL DEFAULT 0,
    run_at        TEXT NOT NULL,
    available_at  TEXT NOT NULL,
    worker_id     TEXT,
    lease_expires TEXT,
    last_error    TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX idx_jobs_claimable ON jobs (state, available_at, priority);
CREATE INDEX idx_jobs_state     ON jobs (state);

CREATE TABLE config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
