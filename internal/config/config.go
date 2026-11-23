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
	"strconv"
	"strings"
	"time"
)

// musicbrainzDefaultBaseURL is the official MusicBrainz server.
const musicbrainzDefaultBaseURL = "https://musicbrainz.org"

// isOfficialMB reports whether the base URL points at the official
// MusicBrainz server (any scheme/host spelling of musicbrainz.org).
func isOfficialMB(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimSuffix(host, ":443")
	host = strings.TrimPrefix(host, "www.")
	return host == "musicbrainz.org"
}

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
	// FFmpegPath is the ffmpeg binary used for transcoding
	// (IHY_FFMPEG_PATH, default "ffmpeg" from PATH). The server
	// refuses to start when the binary is missing or broken.
	FFmpegPath string
	// TranscodeMaxBytes caps the transcode cache (IHY_TRANSCODE_MAX_BYTES,
	// oldest entries evicted first; 0 or unset = unlimited, relying
	// on the mount's own size, e.g. a tmpfs in the deployment).
	TranscodeMaxBytes int64
	// TranscodeSpeculativeTTL is how long transcodes warmed for the
	// queue but never played are kept
	// (IHY_TRANSCODE_SPECULATIVE_TTL, default 24h).
	TranscodeSpeculativeTTL time.Duration
	// AdminPassword bootstraps the single "admin" account on first
	// boot when no user exists (IHY_ADMIN_PASSWORD). Required for the
	// first run; ignored afterwards.
	AdminPassword string
	// MBBaseURL is the MusicBrainz WS/2 root (IHY_MB_BASE_URL,
	// default the official server; set to a mirror's base URL).
	MBBaseURL string
	// MBRateLimit is the pacing in requests per second
	// (IHY_MB_RATE_LIMIT, default 1 = the official limit). Only
	// raise it for a mirror you control.
	MBRateLimit float64
	// UIDev enables web UI development mode: static assets serve
	// from disk and the design page hot-reloads over SSE
	// (IHY_UI_DEV=1).
	UIDev bool
	// UIStaticDir is the directory UI static assets serve from in
	// dev mode, instead of the embedded copies
	// (IHY_UI_STATIC_DIR, set by make dev-ui).
	UIStaticDir string
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

	// MusicBrainz endpoint and pacing. IHY_MB_BASE_URL defaults to
	// the official server. IHY_MB_RATE_LIMIT (requests per second)
	// only applies to a mirror the user controls: setting it while
	// pointing at the official server is a configuration error, since
	// the official 1 req/s rule is not negotiable.
	c.MBBaseURL = envDefault("IHY_MB_BASE_URL", musicbrainzDefaultBaseURL)
	if _, err := url.Parse(c.MBBaseURL); err != nil {
		return c, fmt.Errorf("IHY_MB_BASE_URL: %v", err)
	}
	if v := os.Getenv("IHY_MB_RATE_LIMIT"); v != "" {
		rate, err := strconv.ParseFloat(v, 64)
		if err != nil || rate <= 0 {
			return c, fmt.Errorf("IHY_MB_RATE_LIMIT must be a positive number, got %q", v)
		}
		if isOfficialMB(c.MBBaseURL) {
			return c, fmt.Errorf(
				"IHY_MB_RATE_LIMIT only applies to a self-hosted mirror; the official musicbrainz.org is paced at 1 req/s (set IHY_MB_BASE_URL to a mirror you control, or unset IHY_MB_RATE_LIMIT)")
		}
		c.MBRateLimit = rate
	} else {
		c.MBRateLimit = 1.0
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
	c.FFmpegPath = envDefault("IHY_FFMPEG_PATH", "ffmpeg")
	c.TranscodeMaxBytes = 0
	if v := os.Getenv("IHY_TRANSCODE_MAX_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return c, fmt.Errorf("IHY_TRANSCODE_MAX_BYTES must be a non-negative integer, got %q", v)
		}
		c.TranscodeMaxBytes = n
	}
	c.TranscodeSpeculativeTTL = 24 * time.Hour
	if v := os.Getenv("IHY_TRANSCODE_SPECULATIVE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return c, fmt.Errorf("IHY_TRANSCODE_SPECULATIVE_TTL must be a positive duration, got %q", v)
		}
		c.TranscodeSpeculativeTTL = d
	}
	c.AdminPassword = os.Getenv("IHY_ADMIN_PASSWORD")

	c.UIDev = os.Getenv("IHY_UI_DEV") == "1"
	c.UIStaticDir = os.Getenv("IHY_UI_STATIC_DIR")
	return c, nil
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
