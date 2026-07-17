package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/queuectl/queuectl/internal/model"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// Ensure the directory exists
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory: %w", err)
		}
	}

	// Busy timeout is critical for concurrent access in WAL mode.
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", filepath.ToSlash(dbPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Init() error {
	// Minimal schema initialization to get up and running if not exists
	schema := `
	CREATE TABLE IF NOT EXISTS jobs (
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
	
	CREATE INDEX IF NOT EXISTS idx_jobs_claimable ON jobs (state, available_at, priority);
	CREATE INDEX IF NOT EXISTS idx_jobs_state     ON jobs (state);
	
	CREATE TABLE IF NOT EXISTS config (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Enqueue(job *model.Job) error {
	query := `
		INSERT INTO jobs (
			id, command, state, attempts, max_retries, priority, 
			run_at, available_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now().UTC().Format(time.RFC3339)
	job.CreatedAt, _ = time.Parse(time.RFC3339, now)
	job.UpdatedAt = job.CreatedAt
	job.State = model.StatePending

	_, err := s.db.Exec(
		query, job.ID, job.Command, job.State, job.Attempts, job.MaxRetries,
		job.Priority, job.RunAt.UTC().Format(time.RFC3339),
		job.AvailableAt.UTC().Format(time.RFC3339),
		now, now,
	)
	return err
}

func (s *SQLiteStore) ClaimNext(workerID string, leaseSeconds int) (*model.Job, error) {
	// BEGIN IMMEDIATE to take the write lock before executing the read
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Execute BEGIN IMMEDIATE explicitly since standard sql package starts transactions as DEFERRED
	if _, err := tx.Exec("ROLLBACK; BEGIN IMMEDIATE;"); err != nil {
		return nil, err
	}

	query := `
		UPDATE jobs
		SET state = 'processing',
			worker_id = ?,
			lease_expires = strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '+' || ? || ' seconds'),
			attempts = attempts + 1,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = (
			SELECT id FROM jobs
			WHERE state IN ('pending', 'failed')
			  AND available_at <= strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			ORDER BY priority DESC, available_at ASC
			LIMIT 1
		)
		RETURNING id, command, state, attempts, max_retries, priority, run_at, available_at, worker_id, lease_expires, created_at, updated_at;
	`

	var job model.Job
	var runAt, availableAt, leaseExpires, createdAt, updatedAt string

	err = tx.QueryRow(query, workerID, leaseSeconds).Scan(
		&job.ID, &job.Command, &job.State, &job.Attempts, &job.MaxRetries,
		&job.Priority, &runAt, &availableAt, &job.WorkerID, &leaseExpires,
		&createdAt, &updatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoJobsAvailable
		}
		return nil, err
	}

	job.RunAt, _ = time.Parse(time.RFC3339, runAt)
	job.AvailableAt, _ = time.Parse(time.RFC3339, availableAt)
	if leaseExpires != "" {
		t, _ := time.Parse(time.RFC3339, leaseExpires)
		job.LeaseExpires = &t
	}
	job.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	job.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &job, nil
}

func (s *SQLiteStore) Complete(jobID string) error {
	res, err := s.db.Exec(`
		UPDATE jobs 
		SET state = 'completed', updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') 
		WHERE id = ? AND state = 'processing'
	`, jobID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrInvalidStateTransition
	}
	return nil
}

func (s *SQLiteStore) MarkFailed(jobID string, exitErr string, delay time.Duration) error {
	// Truncate exit error to ~4KB
	if len(exitErr) > 4000 {
		exitErr = exitErr[:4000]
	}

	res, err := s.db.Exec(`
		UPDATE jobs 
		SET state = 'failed',
		    available_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '+' || ? || ' seconds'),
		    last_error = ?,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ? AND state = 'processing'
	`, int(delay.Seconds()), exitErr, jobID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrInvalidStateTransition
	}
	return nil
}

func (s *SQLiteStore) MarkDead(jobID string, exitErr string) error {
	if len(exitErr) > 4000 {
		exitErr = exitErr[:4000]
	}

	res, err := s.db.Exec(`
		UPDATE jobs 
		SET state = 'dead',
		    last_error = ?,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ? AND state = 'processing'
	`, exitErr, jobID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrInvalidStateTransition
	}
	return nil
}

func (s *SQLiteStore) GetJob(jobID string) (*model.Job, error) {
	query := `
		SELECT id, command, state, attempts, max_retries, priority, run_at, available_at, worker_id, lease_expires, last_error, created_at, updated_at 
		FROM jobs WHERE id = ?
	`
	var job model.Job
	var runAt, availableAt, workerID, leaseExpires, lastError, createdAt, updatedAt sql.NullString

	err := s.db.QueryRow(query, jobID).Scan(
		&job.ID, &job.Command, &job.State, &job.Attempts, &job.MaxRetries, &job.Priority,
		&runAt, &availableAt, &workerID, &leaseExpires, &lastError, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}

	job.RunAt, _ = time.Parse(time.RFC3339, runAt.String)
	job.AvailableAt, _ = time.Parse(time.RFC3339, availableAt.String)
	if workerID.Valid {
		job.WorkerID = &workerID.String
	}
	if leaseExpires.Valid && leaseExpires.String != "" {
		t, _ := time.Parse(time.RFC3339, leaseExpires.String)
		job.LeaseExpires = &t
	}
	if lastError.Valid {
		job.LastError = &lastError.String
	}
	job.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	job.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)

	return &job, nil
}

func (s *SQLiteStore) ListJobs(state *model.JobState, limit int) ([]*model.Job, error) {
	query := `
		SELECT id, command, state, attempts, max_retries, priority, run_at, available_at, worker_id, lease_expires, last_error, created_at, updated_at 
		FROM jobs
	`
	var args []interface{}

	if state != nil {
		query += " WHERE state = ?"
		args = append(args, *state)
	}

	query += " ORDER BY updated_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*model.Job
	for rows.Next() {
		var job model.Job
		var runAt, availableAt, workerID, leaseExpires, lastError, createdAt, updatedAt sql.NullString

		err := rows.Scan(
			&job.ID, &job.Command, &job.State, &job.Attempts, &job.MaxRetries, &job.Priority,
			&runAt, &availableAt, &workerID, &leaseExpires, &lastError, &createdAt, &updatedAt,
		)
		if err != nil {
			return nil, err
		}

		job.RunAt, _ = time.Parse(time.RFC3339, runAt.String)
		job.AvailableAt, _ = time.Parse(time.RFC3339, availableAt.String)
		if workerID.Valid {
			job.WorkerID = &workerID.String
		}
		if leaseExpires.Valid && leaseExpires.String != "" {
			t, _ := time.Parse(time.RFC3339, leaseExpires.String) // sqlite strftime format
			job.LeaseExpires = &t
		}
		if lastError.Valid {
			job.LastError = &lastError.String
		}
		job.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
		job.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)

		jobs = append(jobs, &job)
	}
	return jobs, nil
}

func (s *SQLiteStore) GetStats() (map[string]int, error) {
	rows, err := s.db.Query("SELECT state, COUNT(*) FROM jobs GROUP BY state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := map[string]int{
		"pending":    0,
		"processing": 0,
		"completed":  0,
		"failed":     0,
		"dead":       0,
	}

	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, err
		}
		stats[state] = count
	}
	return stats, nil
}

func (s *SQLiteStore) ReclaimStaleLeases() (int, error) {
	// BEGIN IMMEDIATE to avoid deadlocks
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("ROLLBACK; BEGIN IMMEDIATE;"); err != nil {
		return 0, err
	}

	res, err := tx.Exec(`
		UPDATE jobs
		SET state = 'pending', worker_id = NULL, lease_expires = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE state = 'processing' AND lease_expires < strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
	`)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	rows, _ := res.RowsAffected()
	return int(rows), nil
}

func (s *SQLiteStore) RenewLease(jobID string, workerID string, leaseSeconds int) error {
	res, err := s.db.Exec(`
		UPDATE jobs
		SET lease_expires = strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '+' || ? || ' seconds')
		WHERE id = ? AND worker_id = ? AND state = 'processing'
	`, leaseSeconds, jobID, workerID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrInvalidStateTransition // or lost lease
	}
	return nil
}

func (s *SQLiteStore) RetryDead(jobID string) error {
	res, err := s.db.Exec(`
		UPDATE jobs
		SET state = 'pending', attempts = 0, available_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ? AND state = 'dead'
	`, jobID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrInvalidStateTransition
	}
	return nil
}

func (s *SQLiteStore) RetryAllDead() (int, error) {
	res, err := s.db.Exec(`
		UPDATE jobs
		SET state = 'pending', attempts = 0, available_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE state = 'dead'
	`)
	if err != nil {
		return 0, err
	}
	rows, _ := res.RowsAffected()
	return int(rows), nil
}

func (s *SQLiteStore) GetConfig(key string) (string, error) {
	var val string
	err := s.db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&val)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return val, nil
}

func (s *SQLiteStore) SetConfig(key string, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value
	`, key, value)
	return err
}

func (s *SQLiteStore) GetAllConfig() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM config")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cfg := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		cfg[k] = v
	}
	return cfg, nil
}
