package config

import (
	"os"
	"testing"
	"time"
)

func setenv(t *testing.T, k, v string) {
	t.Helper()
	if v == "" {
		t.Setenv(k, "")
		os.Unsetenv(k)
		return
	}
	t.Setenv(k, v)
}

func TestFromEnvRequiredDBSettings(t *testing.T) {
	setenv(t, "DB_HOST", "")
	setenv(t, "DB_DATABASE", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("missing DB_HOST/DB_DATABASE must be an error")
	}
	setenv(t, "DB_HOST", "db")
	if _, err := FromEnv(); err == nil {
		t.Fatal("missing DB_DATABASE must be an error")
	}
}

// Password characters that need URL escaping must come out intact.
func TestDSN(t *testing.T) {
	setenv(t, "DB_HOST", "db")
	setenv(t, "DB_DATABASE", "ihy")
	setenv(t, "DB_USER", "ihy")
	setenv(t, "DB_PASSWORD", "s3cret/x")
	setenv(t, "DB_PORT", "5433")
	setenv(t, "DB_SSLMODE", "")

	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	want := "postgres://ihy:s3cret%2Fx@db:5433/ihy?sslmode=disable"
	if got := c.DSN(); got != want {
		t.Errorf("DSN = %q, want %q", got, want)
	}
}

// The official MusicBrainz server is paced at 1 req/s and that is
// not user-adjustable: IHY_MB_RATE_LIMIT only applies to mirrors.
func TestFromEnvMBRateLimit(t *testing.T) {
	setenv(t, "DB_HOST", "db")
	setenv(t, "DB_DATABASE", "ihy")

	// Default: official endpoint, 1 req/s.
	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if c.MBBaseURL != "https://musicbrainz.org" || c.MBRateLimit != 1.0 {
		t.Errorf("defaults = %q %.1f, want official 1.0", c.MBBaseURL, c.MBRateLimit)
	}

	// Rate limit without a mirror: configuration error.
	setenv(t, "IHY_MB_RATE_LIMIT", "5")
	if _, err := FromEnv(); err == nil {
		t.Fatal("IHY_MB_RATE_LIMIT with the official endpoint must be an error")
	}
	setenv(t, "IHY_MB_RATE_LIMIT", "")

	// Spelling variants of the official host count as official.
	setenv(t, "IHY_MB_BASE_URL", "https://www.musicbrainz.org/")
	setenv(t, "IHY_MB_RATE_LIMIT", "5")
	if _, err := FromEnv(); err == nil {
		t.Fatal("www.musicbrainz.org is still the official server")
	}
	setenv(t, "IHY_MB_RATE_LIMIT", "")

	// A mirror may set its own rate.
	setenv(t, "IHY_MB_BASE_URL", "http://mb.local:5000")
	setenv(t, "IHY_MB_RATE_LIMIT", "10")
	c, err = FromEnv()
	if err != nil {
		t.Fatalf("FromEnv mirror: %v", err)
	}
	if c.MBBaseURL != "http://mb.local:5000" || c.MBRateLimit != 10.0 {
		t.Errorf("mirror config = %q %.1f, want mirror 10.0", c.MBBaseURL, c.MBRateLimit)
	}

	// Garbage rate limits are rejected on mirrors too.
	setenv(t, "IHY_MB_RATE_LIMIT", "0")
	if _, err := FromEnv(); err == nil {
		t.Fatal("non-positive rate limit must be an error")
	}
}

func TestFromEnvDefaultsAndLibraries(t *testing.T) {
	setenv(t, "DB_HOST", "db")
	setenv(t, "DB_DATABASE", "ihy")
	setenv(t, "IHY_ADDR", "")
	setenv(t, "IHY_LIBRARIES", " /music/flac , /music/ogg ,,.local/")
	setenv(t, "IHY_ARTWORK_DIR", "")
	setenv(t, "IHY_TRANSCODE_DIR", "")

	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if c.Addr != ":7731" {
		t.Errorf("Addr = %q, want default :7731", c.Addr)
	}
	if c.ArtworkDir != "./data/artwork" {
		t.Errorf("ArtworkDir = %q, want default", c.ArtworkDir)
	}
	if c.TranscodeDir != "./data/transcode" {
		t.Errorf("TranscodeDir = %q, want default", c.TranscodeDir)
	}
	if len(c.Libraries) != 3 || c.Libraries[0] != "/music/flac" || c.Libraries[1] != "/music/ogg" || c.Libraries[2] != ".local/" {
		t.Errorf("Libraries = %q, want trimmed three-element list", c.Libraries)
	}
	if c.FFmpegPath != "ffmpeg" {
		t.Errorf("FFmpegPath = %q, want default", c.FFmpegPath)
	}
	if c.TranscodeMaxBytes != 0 {
		t.Errorf("TranscodeMaxBytes = %d, want 0 (uncapped)", c.TranscodeMaxBytes)
	}
	if c.TranscodeSpeculativeTTL != 24*time.Hour {
		t.Errorf("TranscodeSpeculativeTTL = %v, want default 24h", c.TranscodeSpeculativeTTL)
	}
}

func TestFromEnvTranscodeSettings(t *testing.T) {
	setenv(t, "DB_HOST", "db")
	setenv(t, "DB_DATABASE", "ihy")
	setenv(t, "IHY_FFMPEG_PATH", "/opt/ffmpeg/bin/ffmpeg")
	setenv(t, "IHY_TRANSCODE_MAX_BYTES", "2147483648")
	setenv(t, "IHY_TRANSCODE_SPECULATIVE_TTL", "2h")

	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if c.FFmpegPath != "/opt/ffmpeg/bin/ffmpeg" {
		t.Errorf("FFmpegPath = %q", c.FFmpegPath)
	}
	if c.TranscodeMaxBytes != 1<<31 {
		t.Errorf("TranscodeMaxBytes = %d", c.TranscodeMaxBytes)
	}
	if c.TranscodeSpeculativeTTL != 2*time.Hour {
		t.Errorf("TranscodeSpeculativeTTL = %v", c.TranscodeSpeculativeTTL)
	}

	for _, bad := range []struct{ key, value string }{
		{"IHY_TRANSCODE_MAX_BYTES", "-1"},
		{"IHY_TRANSCODE_MAX_BYTES", "abc"},
		{"IHY_TRANSCODE_SPECULATIVE_TTL", "-5m"},
		{"IHY_TRANSCODE_SPECULATIVE_TTL", "soon"},
	} {
		setenv(t, bad.key, bad.value)
		if _, err := FromEnv(); err == nil {
			t.Errorf("%s=%q accepted", bad.key, bad.value)
		}
		setenv(t, bad.key, "")
	}
}
