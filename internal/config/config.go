// Package config loads bootstrap settings from environment variables.
//
// Docker-first decision (docs/scope.md Runtime decisions): no config
// files. Env vars carry bootstrap settings; runtime settings (library
// roots, users, display preferences) live in the database and are
// managed through the web UI. IHY_LIBRARIES is an optional bootstrap
// seed: comma-separated library roots inserted into the library table
// on first start.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config is the bootstrap configuration of ihy.
type Config struct {
	// Postgres connection settings (DB_HOST, DB_DATABASE required;
	// DB_USER, DB_PASSWORD, DB_PORT, DB_SSLMODE optional).
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string
	// Addr is the HTTP listen address (IHY_ADDR, default ":7731").
	Addr string
	// Libraries optionally seeds library roots (IHY_LIBRARIES,
	// comma-separated; library roots are read-only mounts).
	Libraries []string
	// ArtworkDir stores derived artwork thumbnails
	// (IHY_ARTWORK_DIR, default "./data/artwork").
	ArtworkDir string
	// TranscodeDir is the disposable transcode output mount
	// (IHY_TRANSCODE_DIR, default "./data/transcode").
	TranscodeDir string
	// AdminPassword bootstraps the single "admin" account on first
	// boot when no user exists (IHY_ADMIN_PASSWORD). Required for the
	// first run; ignored afterwards.
	AdminPassword string
}

// DSN builds the Postgres connection string from the DB_* settings.
func (c Config) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		Host:   c.DBHost + ":" + c.DBPort,
		Path:   "/" + c.DBName,
	}
	if c.DBUser != "" || c.DBPassword != "" {
		u.User = url.UserPassword(c.DBUser, c.DBPassword)
	}
	u.RawQuery = "sslmode=" + c.DBSSLMode
	return u.String()
}

// FromEnv builds Config from the process environment.
func FromEnv() (Config, error) {
	var c Config

	c.DBHost = os.Getenv("DB_HOST")
	c.DBName = os.Getenv("DB_DATABASE")
	if c.DBHost == "" || c.DBName == "" {
		return c, fmt.Errorf("DB_HOST and DB_DATABASE are required")
	}

	c.DBUser = os.Getenv("DB_USER")
	c.DBPassword = os.Getenv("DB_PASSWORD")
	c.DBPort = envDefault("DB_PORT", "5432")
	c.DBSSLMode = envDefault("DB_SSLMODE", "disable")

	c.Addr = os.Getenv("IHY_ADDR")
	if c.Addr == "" {
		c.Addr = ":7731"
	}

	if libs := os.Getenv("IHY_LIBRARIES"); libs != "" {
		for _, root := range strings.Split(libs, ",") {
			root = strings.TrimSpace(root)
			if root != "" {
				c.Libraries = append(c.Libraries, root)
			}
		}
	}

	c.ArtworkDir = envDefault("IHY_ARTWORK_DIR", "./data/artwork")
	c.TranscodeDir = envDefault("IHY_TRANSCODE_DIR", "./data/transcode")
	c.AdminPassword = os.Getenv("IHY_ADMIN_PASSWORD")
	return c, nil
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
