// Package dbtest provisions throwaway databases for real-Postgres
// tests. Skips the calling test when IHY_TEST_DSN is unset (an admin
// DSN to any database on the server; a fresh database is created and
// dropped per call).
//
//	make test-db
package dbtest

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Server creates a uniquely named database and returns its DSN.
// Cleanup drops the database when the test finishes.
func Server(t *testing.T) string {
	t.Helper()
	admin := os.Getenv("IHY_TEST_DSN")
	if admin == "" {
		t.Skip("IHY_TEST_DSN not set; skipping real-PG test (make test-db)")
	}

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("parse IHY_TEST_DSN: %v", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		t.Fatalf("unsupported DSN scheme %q", u.Scheme)
	}

	adminDSN := swapPath(u, "postgres")
	conn, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer conn.Close()

	name := fmt.Sprintf("ihy_test_%d", time.Now().UnixNano()%1_000_000)
	if _, err := conn.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		c, err := sql.Open("pgx", adminDSN)
		if err != nil {
			t.Logf("cleanup: open: %v", err)
			return
		}
		defer c.Close()
		if _, err := c.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)"); err != nil {
			t.Logf("cleanup: drop: %v", err)
		}
	})
	return swapPath(u, name)
}

func swapPath(u *url.URL, dbname string) string {
	c := *u
	c.Path = "/" + dbname
	return c.String()
}
