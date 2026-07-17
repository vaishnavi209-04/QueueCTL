package store

import (
	"errors"
	"time"

	"github.com/queuectl/queuectl/internal/model"
)

var (
	ErrNoJobsAvailable        = errors.New("no jobs available")
	ErrJobNotFound            = errors.New("job not found")
	ErrInvalidStateTransition = errors.New("invalid state transition")
)

type Store interface {
	Init() error
	Close() error

	// Job operations
	Enqueue(job *model.Job) error
	ClaimNext(workerID string, leaseSeconds int) (*model.Job, error)
	Complete(jobID string) error
	MarkFailed(jobID string, exitErr string, delay time.Duration) error
	MarkDead(jobID string, exitErr string) error

	// Read operations
	GetJob(jobID string) (*model.Job, error)
	ListJobs(state *model.JobState, limit int) ([]*model.Job, error)
	GetStats() (map[string]int, error)

	// Maintenance operations
	ReclaimStaleLeases() (int, error)
	RenewLease(jobID string, workerID string, leaseSeconds int) error
	RetryDead(jobID string) error
	RetryAllDead() (int, error)

	// Config operations
	GetConfig(key string) (string, error)
	SetConfig(key string, value string) error
	GetAllConfig() (map[string]string, error)
}
