package external

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sync"
	"time"
)

// Worker runs the crawler in the background. Pull-driven: it loops, asks
// the store for the next due vault, runs the crawler. Manual Trigger calls
// wake it up immediately instead of waiting for the idle tick.
type Worker struct {
	Crawler *Crawler

	// IdleSleep is the wait between checks when the queue is empty.
	IdleSleep time.Duration

	wakeCh chan struct{}
	wg     sync.WaitGroup
}

func NewWorker(c *Crawler) *Worker {
	return &Worker{
		Crawler:   c,
		IdleSleep: 60 * time.Second,
		wakeCh:    make(chan struct{}, 1),
	}
}

// Start kicks off the worker loop. Returns immediately. Shut down by
// cancelling ctx; Wait() blocks until the loop exits.
func (w *Worker) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.run(ctx)
}

// Trigger nudges the worker so it processes the next pending vault now.
// Safe to call from any goroutine; non-blocking.
func (w *Worker) Trigger(vaultID int64) {
	select {
	case w.wakeCh <- struct{}{}:
	default:
		// already armed
	}
}

func (w *Worker) Wait() { w.wg.Wait() }

func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()
	log.Printf("[external] worker started (idle sleep %s)", w.IdleSleep)
	for {
		// Process all currently-due vaults before sleeping.
		for {
			err := w.Crawler.CrawlOne(ctx)
			if err == nil {
				continue
			}
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if ctx.Err() != nil {
				log.Printf("[external] worker stopping: %v", ctx.Err())
				return
			}
			log.Printf("[external] crawl error: %v", err)
			break
		}
		// Idle wait.
		select {
		case <-ctx.Done():
			log.Printf("[external] worker stopping: %v", ctx.Err())
			return
		case <-w.wakeCh:
			// triggered — loop again
		case <-time.After(w.IdleSleep):
			// periodic re-check
		}
	}
}
