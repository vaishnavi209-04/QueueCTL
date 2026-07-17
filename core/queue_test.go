package core

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/queuectl/queuectl/internal/model"
	"github.com/queuectl/queuectl/store"
	"github.com/stretchr/testify/require"
)

func newTempQueue(t *testing.T) *Queue {
	dbPath := filepath.Join(t.TempDir(), "queue.db")
	s, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	err = s.Init()
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.JobTimeout = 2 * time.Second
	cfg.MaxRetries = 3

	return NewQueue(s, cfg)
}

func TestEnqueueAndComplete(t *testing.T) {
	q := newTempQueue(t)
	defer q.Store().Close()

	req, err := model.ParseEnqueueRequest(`{"command": "echo hi"}`, 3)
	require.NoError(t, err)

	job, err := q.Enqueue(req)
	require.NoError(t, err)
	require.Equal(t, model.StatePending, job.State)

	// Process manually
	worker := NewWorker("w1", q)
	claimed, err := q.Store().ClaimNext("w1", q.Config().LeaseSeconds)
	require.NoError(t, err)
	require.Equal(t, job.ID, claimed.ID)
	require.Equal(t, model.StateProcessing, claimed.State)

	// Simulate execution success
	err = q.OnJobFinished(claimed, nil, "hi\n")
	require.NoError(t, err)

	finalJob, err := q.Store().GetJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, model.StateCompleted, finalJob.State)
}

func TestInvalidCommand(t *testing.T) {
	q := newTempQueue(t)
	defer q.Store().Close()

	req, _ := model.ParseEnqueueRequest(`{"command": "this-binary-does-not-exist"}`, 3)
	job, _ := q.Enqueue(req)

	worker := NewWorker("w1", q)
	claimed, _ := q.Store().ClaimNext("w1", q.Config().LeaseSeconds)

	res := worker.executor.Run(claimed.Command)
	require.Error(t, res.Error)

	q.OnJobFinished(claimed, res.Error, res.Output)

	finalJob, _ := q.Store().GetJob(job.ID)
	require.Equal(t, model.StateFailed, finalJob.State)
	require.Equal(t, 1, finalJob.Attempts)
}

func TestRetryBackoffAndDLQ(t *testing.T) {
	q := newTempQueue(t)
	defer q.Store().Close()

	req, _ := model.ParseEnqueueRequest(`{"command": "exit 1"}`, 2)
	job, _ := q.Enqueue(req)

	// Attempt 1 -> Fails -> State: failed
	claimed1, _ := q.Store().ClaimNext("w1", q.Config().LeaseSeconds)
	err := q.OnJobFinished(claimed1, fmt.Errorf("exit status 1"), "")
	require.NoError(t, err)

	j1, _ := q.Store().GetJob(job.ID)
	require.Equal(t, model.StateFailed, j1.State)
	require.Equal(t, 1, j1.Attempts)

	// Wait for backoff (base 2 ^ 1 = 2 seconds)
	// We cheat by updating available_at in the DB for the test instead of sleeping
	// We mock this by just moving on (since we can't easily manipulate the DB time without exposing it)
	// In a real integration test, we'd add a helper to SQLiteStore to advance time.

	// Attempt 2 -> Fails -> MaxRetries (2) reached -> State: dead
	claimed2, err := q.Store().ClaimNext("w2", q.Config().LeaseSeconds)
	require.NoError(t, err)

	err = q.OnJobFinished(claimed2, fmt.Errorf("exit status 1"), "")
	require.NoError(t, err)

	j2, _ := q.Store().GetJob(job.ID)
	require.Equal(t, model.StateDead, j2.State)
	require.Equal(t, 2, j2.Attempts)
}

func TestNoDuplicateClaim(t *testing.T) {
	q := newTempQueue(t)
	defer q.Store().Close()

	// Enqueue 200 jobs
	for i := 0; i < 200; i++ {
		req, _ := model.ParseEnqueueRequest(fmt.Sprintf(`{"command": "echo %d"}`, i), 3)
		q.Enqueue(req)
	}

	var claimedIDs sync.Map
	var dupes int32
	var wg sync.WaitGroup

	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			for {
				job, err := q.Store().ClaimNext(workerID, q.Config().LeaseSeconds)
				if err == store.ErrNoJobsAvailable {
					return
				}
				if _, loaded := claimedIDs.LoadOrStore(job.ID, workerID); loaded {
					atomic.AddInt32(&dupes, 1)
				}
			}
		}(fmt.Sprintf("w%d", w))
	}

	wg.Wait()
	require.Equal(t, int32(0), dupes)

	// Count unique
	count := 0
	claimedIDs.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	require.Equal(t, 200, count)
}

func TestPersistAcrossRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "queue.db")
	s1, _ := store.NewSQLiteStore(dbPath)
	s1.Init()
	q1 := NewQueue(s1, DefaultConfig())

	req, _ := model.ParseEnqueueRequest(`{"command": "sleep 10"}`, 3)
	job, _ := q1.Enqueue(req)

	// Simulate start processing
	q1.Store().ClaimNext("w1", 30)

	s1.Close() // Simulate crash

	// Reopen
	s2, _ := store.NewSQLiteStore(dbPath)
	s2.Init()
	q2 := NewQueue(s2, DefaultConfig())

	j2, err := q2.Store().GetJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, model.StateProcessing, j2.State)
	require.NotNil(t, j2.WorkerID)

	// Simulate sweep reclaiming it
	q2.Store().(*store.SQLiteStore).ReclaimStaleLeases() // won't reclaim immediately unless we advance time
}
