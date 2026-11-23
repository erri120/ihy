// Real-Postgres test for Resolve and the warmer. Skips without
// IHY_TEST_DSN (make test-e2e).
package stream

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"ihy/internal/db"
	"ihy/internal/dbtest"
)

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dsn := dbtest.Server(t)
	if _, err := db.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	root := t.TempDir()
	var libID int64
	if err := conn.QueryRow(`INSERT INTO library (root_path) VALUES ($1) RETURNING id`, root).Scan(&libID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var fileID int64
	if err := conn.QueryRow(`
		INSERT INTO file (library_id, path, mtime, size_bytes, format)
		VALUES ($1, $2, now(), 42, 'flac') RETURNING id`, libID, "sub/song.flac").Scan(&fileID); err != nil {
		t.Fatalf("insert file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "song.flac"), make([]byte, 42), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewService(conn), root
}

func TestResolveJoinsLibraryRoot(t *testing.T) {
	svc, root := newTestService(t)
	var fileID int64
	if err := svc.db.QueryRow(`SELECT id FROM file LIMIT 1`).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	src, err := svc.Resolve(fileID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.AbsPath != filepath.Join(root, "sub", "song.flac") {
		t.Errorf("AbsPath = %q", src.AbsPath)
	}
	if src.Size != 42 || src.Format != "flac" || src.ModTime.IsZero() {
		t.Errorf("source = %+v", src)
	}
	if _, err := os.Stat(src.AbsPath); err != nil {
		t.Errorf("resolved file does not exist: %v", err)
	}
}

func TestResolveUnknownFile(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Resolve(9999); err == nil {
		t.Fatal("Resolve(9999) = nil error, want unknown")
	}
}

func TestWarmSyncDedupesAndCaps(t *testing.T) {
	svc, root := newTestService(t)
	var warmed []string
	svc.warm = func(p string) { warmed = append(warmed, p) }

	// One row exists; unknown and duplicate ids are dropped.
	var onlyID int64
	if err := svc.db.QueryRow(`SELECT id FROM file LIMIT 1`).Scan(&onlyID); err != nil {
		t.Fatal(err)
	}
	svc.warmSync([]int64{onlyID, onlyID, 9999})
	if len(warmed) != 1 {
		t.Fatalf("warmed %d paths, want 1 (dupes/unknown dropped)", len(warmed))
	}
	want := filepath.Join(root, "sub", "song.flac")
	if warmed[0] != want {
		t.Errorf("warmed path = %q, want %q", warmed[0], want)
	}

	// The list is capped at maxUpcoming distinct files.
	var ids []int64
	for i := 0; i < 6; i++ {
		var id int64
		if err := svc.db.QueryRow(`
			INSERT INTO file (library_id, path, mtime, size_bytes, format)
			VALUES (1, $1, now(), 1, 'flac') RETURNING id`,
			fmt.Sprintf("t%d.flac", i)).Scan(&id); err != nil {
			t.Fatalf("insert extra file: %v", err)
		}
		ids = append(ids, id)
	}
	warmed = nil
	svc.warmSync(append(ids, 9999, 0, -3))
	if len(warmed) != maxUpcoming {
		t.Errorf("warmed %d paths, want %d (capped)", len(warmed), maxUpcoming)
	}
}

func TestWarmRunsInBackground(t *testing.T) {
	svc, _ := newTestService(t)
	done := make(chan string, 1)
	svc.warm = func(p string) { done <- p }
	svc.Warm([]int64{1})
	select {
	case got := <-done:
		_ = got
	case <-time.After(2 * time.Second):
		t.Fatal("Warm did not invoke the warm hook")
	}
}
