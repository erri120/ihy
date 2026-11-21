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
	"os"
	"strings"
)

// Config is the bootstrap configuration of ihy.
type Config struct {
	// DSN is the Postgres connection string (IHY_DSN, required).
	DSN string
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
}

// FromEnv builds Config from the process environment.
func FromEnv() (Config, error) {
	var c Config

	c.DSN = os.Getenv("IHY_DSN")
	if c.DSN == "" {
		return c, fmt.Errorf("IHY_DSN is required (e.g. postgres://ihy:ihy@db:5432/ihy?sslmode=disable)")
	}

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
	return c, nil
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
