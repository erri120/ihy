// Package db owns the schema migrations. Migration files are embedded
// so the binary can bring a fresh database up on its own.
package db

import (
	"embed"
	"fmt"
	"net/url"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// pgx5URL converts a postgres:// or postgresql:// DSN to the pgx5://
// scheme registered by golang-migrate's pgx/v5 driver.
func pgx5URL(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse DSN: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("unsupported DSN scheme %q (want postgres://)", u.Scheme)
	}
	u.Scheme = "pgx5"
	return u.String(), nil
}

// New opens a migrate handle for the given DSN. Callers that need
// fine-grained control (tests stepping down) use this directly.
func New(dsn string) (*migrate.Migrate, error) {
	d, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	target, err := pgx5URL(dsn)
	if err != nil {
		return nil, err
	}
	m, err := migrate.NewWithSourceInstance("iofs", d, target)
	if err != nil {
		return nil, fmt.Errorf("open migrate: %w", err)
	}
	return m, nil
}

// Migrate applies all pending migrations. Applying on an up-to-date
// database is a no-op.
func Migrate(dsn string) error {
	m, err := New(dsn)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Up(); err != nil && !strings.Contains(err.Error(), "no change") {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
