package core

import (
	"context"
	"log"
	"time"
)

type LeaseSweeper struct {
	Queue *Queue
}

func NewLeaseSweeper(q *Queue) *LeaseSweeper {
	return &LeaseSweeper{Queue: q}
}

func (l *LeaseSweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(l.Queue.Config().SweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := l.Queue.store.ReclaimStaleLeases()
			if err != nil {
				log.Printf("lease sweeper error: %v", err)
			} else if count > 0 {
				log.Printf("lease sweeper reclaimed %d stale jobs", count)
			}
		}
	}
}
