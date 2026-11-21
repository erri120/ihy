// Real-Postgres tests for the migration runner. They need a throwaway
// server: set IHY_TEST_DSN to an ADMIN DSN (any existing database on
// the server is fine; the test creates and drops its own database).
//
//	make test-db   (starts one via docker compose, runs tests, tears down)
//
// Without IHY_TEST_DSN the tests skip, so plain `go test ./...` stays
// green on machines without Postgres.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// testDatabase provisions a uniquely named database on the test server
// and returns (dsn, cleanup).
func testDatabase(t *testing.T) (string, func()) {
	t.Helper()
	admin := os.Getenv("IHY_TEST_DSN")
	if admin == "" {
		t.Skip("IHY_TEST_DSN not set; skipping real-PG test (make test-db)")
	}

	u, err := parseDSN(admin)
	if err != nil {
		t.Fatalf("parse IHY_TEST_DSN: %v", err)
	}

	adminDB := swapPath(u, "postgres")
	conn, err := sql.Open("pgx", adminDB)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer conn.Close()

	name := fmt.Sprintf("ihy_migrate_%d", time.Now().UnixNano()%1_000_000)
	if _, err := conn.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	testDSN := swapPath(u, name)
	cleanup := func() {
		c, err := sql.Open("pgx", adminDB)
		if err != nil {
			t.Logf("cleanup: open: %v", err)
			return
		}
		defer c.Close()
		if _, err := c.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)"); err != nil {
			t.Logf("cleanup: drop: %v", err)
		}
	}
	return testDSN, cleanup
}

func TestMigrationsApplyOnEmptyDatabase(t *testing.T) {
	dsn, cleanup := testDatabase(t)
	defer cleanup()

	if err := Migrate(dsn); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := Migrate(dsn); err != nil {
		t.Fatalf("second Migrate (must be a no-op): %v", err)
	}

	version, dirty, err := schemaVersion(dsn)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if dirty {
		t.Fatal("schema_migrations marked dirty")
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}

	want := []string{
		"artist", "release_group", "release", "label", "release_label",
		"recording", "work", "recording_work", "credit", "alias",
		"library", "file", "track", "user", "session", "api_token",
		"display_pref", "event", "mb_cache",
	}
	for _, table := range want {
		if !tableExists(dsn, table) {
			t.Errorf("table %q missing after migration", table)
		}
	}
}

func TestMigrationsDownAndUp(t *testing.T) {
	dsn, cleanup := testDatabase(t)
	defer cleanup()

	if err := Migrate(dsn); err != nil {
		t.Fatalf("up: %v", err)
	}

	m, err := New(dsn)
	if err != nil {
		t.Fatalf("open migrate handle: %v", err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	for _, table := range []string{"artist", "credit", "alias", "event"} {
		if tableExists(dsn, table) {
			t.Errorf("table %q still exists after down", table)
		}
	}

	if err := m.Up(); err != nil {
		t.Fatalf("up again after down: %v", err)
	}
	if !tableExists(dsn, "artist") {
		t.Error("table artist missing after down+up cycle")
	}
	m.Close()
}

func TestConstraintShape(t *testing.T) {
	dsn, cleanup := testDatabase(t)
	defer cleanup()

	if err := Migrate(dsn); err != nil {
		t.Fatalf("up: %v", err)
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	// The credit primary key encodes the ordered-rows principle: same
	// artist can hold several roles, same role several artists, order
	// comes from position.
	if _, err := conn.Exec(`
		INSERT INTO artist (mbid, name, sort_name) VALUES
		  ('00000000-0000-0000-0000-000000000001', 'A', 'A'),
		  ('00000000-0000-0000-0000-000000000002', 'B', 'B');
		INSERT INTO credit (subject_type, subject_mbid, artist_mbid, role, position, display_name)
		VALUES ('recording', '00000000-0000-0000-0000-000000000009',
		        '00000000-0000-0000-0000-000000000001', 'performer', 0, 'A'),
		       ('recording', '00000000-0000-0000-0000-000000000009',
		        '00000000-0000-0000-0000-000000000002', 'performer', 1, 'B feat. '),
		       ('recording', '00000000-0000-0000-0000-000000000009',
		        '00000000-0000-0000-0000-000000000001', 'composer', 0, 'A');
	`); err != nil {
		t.Fatalf("insert multi-role credits: %v", err)
	}

	// Duplicate (subject, artist, role, position) must be rejected.
	if _, err := conn.Exec(`
		INSERT INTO credit (subject_type, subject_mbid, artist_mbid, role, position, display_name)
		VALUES ('recording', '00000000-0000-0000-0000-000000000009',
		        '00000000-0000-0000-0000-000000000001', 'performer', 0, 'A again');
	`); err == nil {
		t.Error("duplicate credit row was accepted; primary key not enforced")
	}
}

// helpers

func schemaVersion(dsn string) (version int, dirty bool, err error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, false, err
	}
	defer conn.Close()
	err = conn.QueryRow("SELECT COALESCE(MAX(version), 0), COALESCE(BOOL_OR(dirty), false) FROM schema_migrations").Scan(&version, &dirty)
	return version, dirty, err
}

func tableExists(dsn, table string) bool {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return false
	}
	defer conn.Close()
	var exists bool
	err = conn.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)`, table).Scan(&exists)
	return err == nil && exists
}
