package model

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

type JobState string

const (
	StatePending    JobState = "pending"
	StateProcessing JobState = "processing"
	StateCompleted  JobState = "completed"
	StateFailed     JobState = "failed"
	StateDead       JobState = "dead"
)

type Job struct {
	ID           string     `json:"id"`
	Command      string     `json:"command"`
	State        JobState   `json:"state"`
	Attempts     int        `json:"attempts"`
	MaxRetries   int        `json:"max_retries"`
	Priority     int        `json:"priority"`
	RunAt        time.Time  `json:"run_at"`
	AvailableAt  time.Time  `json:"available_at"`
	WorkerID     *string    `json:"worker_id,omitempty"`
	LeaseExpires *time.Time `json:"lease_expires,omitempty"`
	LastError    *string    `json:"last_error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type EnqueueRequest struct {
	ID         string    `json:"id,omitempty"`
	Command    string    `json:"command"`
	MaxRetries *int      `json:"max_retries,omitempty"`
	Priority   int       `json:"priority,omitempty"`
	RunAt      time.Time `json:"run_at,omitempty"`
}

func (r *EnqueueRequest) ValidateAndSetDefaults(defaultMaxRetries int) error {
	if r.Command == "" {
		return errors.New("command is required")
	}
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	if r.MaxRetries == nil {
		r.MaxRetries = &defaultMaxRetries
	}
	if r.RunAt.IsZero() {
		r.RunAt = time.Now()
	}
	return nil
}

func ParseEnqueueRequest(data string, defaultMaxRetries int) (*EnqueueRequest, error) {
	var req EnqueueRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return nil, err
	}
	if err := req.ValidateAndSetDefaults(defaultMaxRetries); err != nil {
		return nil, err
	}
	return &req, nil
}
