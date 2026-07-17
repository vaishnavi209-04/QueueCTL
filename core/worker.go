package core

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/queuectl/queuectl/internal/model"
	"github.com/queuectl/queuectl/store"
)

type Worker struct {
	ID       string
	queue    *Queue
	executor *Executor
}

func NewWorker(id string, q *Queue) *Worker {
	return &Worker{
		ID:       id,
		queue:    q,
		executor: NewExecutor(q.Config().JobTimeout),
	}
}

func (w *Worker) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Printf("Worker %s started", w.ID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Worker %s stopping", w.ID)
			return
		default:
			job, err := w.queue.store.ClaimNext(w.ID, w.queue.Config().LeaseSeconds)
			if err != nil {
				if errors.Is(err, store.ErrNoJobsAvailable) {
					// Sleep before polling again
					select {
					case <-ctx.Done():
						return
					case <-time.After(1 * time.Second):
						continue
					}
				}
				log.Printf("Worker %s error claiming job: %v", w.ID, err)
				time.Sleep(1 * time.Second)
				continue
			}

			w.processJob(ctx, job)
		}
	}
}

func (w *Worker) processJob(ctx context.Context, job *model.Job) {
	log.Printf("Worker %s processing job %s", w.ID, job.ID)

	// Start heartbeat
	heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
	defer cancelHeartbeat()

	go w.heartbeat(heartbeatCtx, job.ID)

	// Execute
	res := w.executor.Run(job.Command)

	// Stop heartbeat
	cancelHeartbeat()

	// Handle result
	if err := w.queue.OnJobFinished(job, res.Error, res.Output); err != nil {
		log.Printf("Worker %s error finalizing job %s: %v", w.ID, job.ID, err)
	} else {
		if res.Error == nil {
			log.Printf("Worker %s completed job %s", w.ID, job.ID)
		} else {
			log.Printf("Worker %s failed job %s: %v", w.ID, job.ID, res.Error)
		}
	}
}

func (w *Worker) heartbeat(ctx context.Context, jobID string) {
	ticker := time.NewTicker(w.queue.Config().HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.queue.store.RenewLease(jobID, w.ID, w.queue.Config().LeaseSeconds); err != nil {
				log.Printf("Worker %s failed to renew lease for job %s: %v", w.ID, jobID, err)
				return // Stop heartbeating if we fail (e.g. lease already lost)
			}
		}
	}
}
