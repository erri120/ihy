// Package jobs owns the durable background job queue (River on
// Postgres). Today's only queue is "exports" for offline export
// workers; scan, enrichment and artwork workers join the same queue
// infrastructure later per docs/architecture.md.
//
// River's schema is upstream-maintained and applied by rivermigrate at
// boot, the same tier as golang-migrate for our 0001: never hand-edit
// its tables in our migrations.
package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// exportsQueue is the single job queue; one worker at a time, shared
// with the interactive transcode path through the global ffmpeg
// concurrency cap.
const exportsQueue = "exports"

// Runner owns the River client for the process.
type Runner struct {
	River *river.Client[pgx.Tx]
}

// NewRunner applies the River schema migrations and builds the client
// on the given pool. The client is not started; call Start after the
// caller is ready for workers to pick up jobs (this lets tests enqueue
// against a stopped client to prove restart semantics).
func NewRunner(ctx context.Context, pool *pgxpool.Pool, workers *river.Workers) (*Runner, error) {
	mg, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return nil, fmt.Errorf("jobs: river migrate init: %w", err)
	}
	if _, err := mg.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("jobs: river migrate: %w", err)
	}

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			exportsQueue: {MaxWorkers: 1},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("jobs: river client: %w", err)
	}
	return &Runner{River: client}, nil
}

// Start begins running workers. The context governs the client's
// background goroutines.
func (r *Runner) Start(ctx context.Context) error {
	return r.River.Start(ctx)
}

// Stop halts the client, waiting for in-flight jobs to finish.
func (r *Runner) Stop() {
	// A fixed grace period: an export encode is killed (via its job
	// context) when it expires, so shutdown never hangs on a
	// hours-long job.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r.River.Stop(ctx)
}
