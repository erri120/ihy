// Real-Postgres tests for the River foundation: schema migration at
// boot, transactional enqueue against a stopped client, and restart
// semantics (a queued job is executed by a later client). Skips
// without IHY_TEST_DSN.
package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"ihy/internal/db"
	"ihy/internal/dbtest"
)

// noopArgs is a test-only job kind registered in test workers. It
// defaults to the exports queue, the pattern the real job kinds follow.
type noopArgs struct{}

func (noopArgs) Kind() string { return "test_noop" }

func (noopArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: exportsQueue}
}

// noopWorker signals on ran when it executes. Compile-time interface
// assertion, per project convention. WorkerDefaults supplies the
// Middleware/NextRetry/Timeout defaults so only Work is overridden.
type noopWorker struct {
	river.WorkerDefaults[noopArgs]
	ran chan struct{}
}

var _ river.Worker[noopArgs] = (*noopWorker)(nil)

func (w *noopWorker) Work(ctx context.Context, job *river.Job[noopArgs]) error {
	select {
	case <-w.ran:
	default:
		close(w.ran)
	}
	return nil
}

func newTestPool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := dbtest.Server(t)
	if _, err := db.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, dsn
}

func TestRiverSchemaMigrated(t *testing.T) {
	pool, _ := newTestPool(t)
	runner, err := NewRunner(context.Background(), pool, river.NewWorkers())
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer runner.Stop()

	// river_migration holds River's own schema version history.
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM river_migration`).Scan(&n); err != nil {
		t.Fatalf("river_migration table: %v", err)
	}
	if n == 0 {
		t.Fatal("river schema not migrated: river_migration is empty")
	}
}

func TestEnqueueAndRun(t *testing.T) {
	pool, _ := newTestPool(t)
	w := &noopWorker{ran: make(chan struct{})}
	workers := river.NewWorkers()
	river.AddWorker(workers, w)
	runner, err := NewRunner(context.Background(), pool, workers)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer runner.Stop()
	if err := runner.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := runner.River.Insert(context.Background(), noopArgs{}, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}
	select {
	case <-w.ran:
	case <-time.After(5 * time.Second):
		t.Fatal("noop job never executed")
	}
}

func TestQueuedJobSurvivesRestart(t *testing.T) {
	pool, _ := newTestPool(t)
	w := &noopWorker{ran: make(chan struct{})}
	workers := river.NewWorkers()
	river.AddWorker(workers, w)

	// First client enqueues while stopped; the job must land in
	// river_job and wait.
	first, err := NewRunner(context.Background(), pool, workers)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if _, err := first.River.Insert(context.Background(), noopArgs{}, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var state string
	if err := pool.QueryRow(context.Background(), `SELECT state FROM river_job LIMIT 1`).Scan(&state); err != nil {
		t.Fatalf("river_job row: %v", err)
	}
	if state != "available" {
		t.Fatalf("job state = %q, want available (queued, not yet run)", state)
	}

	// A later client picks it up: the restart-semantics guarantee the
	// export worker relies on.
	second, err := NewRunner(context.Background(), pool, workers)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer second.Stop()
	if err := second.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-w.ran:
	case <-time.After(5 * time.Second):
		t.Fatal("queued job was not picked up by the later client")
	}
	// first was never started; stopping it is a no-op.
	first.Stop()
}
