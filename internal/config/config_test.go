package config

import (
	"os"
	"testing"
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
}
