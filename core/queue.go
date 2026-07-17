package core

import (
	"github.com/queuectl/queuectl/internal/model"
	"github.com/queuectl/queuectl/store"
)

type Queue struct {
	store store.Store
	cfg   Config
}

func NewQueue(s store.Store, cfg Config) *Queue {
	return &Queue{
		store: s,
		cfg:   cfg,
	}
}

func (q *Queue) Config() Config {
	return q.cfg
}

func (q *Queue) Store() store.Store {
	return q.store
}

func (q *Queue) Enqueue(req *model.EnqueueRequest) (*model.Job, error) {
	job := &model.Job{
		ID:          req.ID,
		Command:     req.Command,
		MaxRetries:  *req.MaxRetries,
		Priority:    req.Priority,
		RunAt:       req.RunAt,
		AvailableAt: req.RunAt, // available immediately at run_at
	}

	if err := q.store.Enqueue(job); err != nil {
		return nil, err
	}

	// Refresh to get exact dates from db
	return q.store.GetJob(job.ID)
}

func (q *Queue) OnJobFinished(job *model.Job, exitErr error, output string) error {
	if exitErr == nil {
		return q.store.Complete(job.ID)
	}

	errMsg := exitErr.Error()
	if output != "" {
		errMsg = errMsg + ": " + output
	}

	if job.Attempts >= job.MaxRetries {
		return q.store.MarkDead(job.ID, errMsg)
	}

	delay := CalculateBackoff(q.cfg.BackoffBase, job.Attempts)
	return q.store.MarkFailed(job.ID, errMsg, delay)
}
