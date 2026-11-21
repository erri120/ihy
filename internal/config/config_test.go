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

func TestFromEnvRequiredDSN(t *testing.T) {
	setenv(t, "IHY_DSN", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("missing IHY_DSN must be an error")
	}
}

func TestFromEnvDefaultsAndLibraries(t *testing.T) {
	setenv(t, "IHY_DSN", "postgres://ihy:ihy@db:5432/ihy?sslmode=disable")
	setenv(t, "IHY_ADDR", "")
	setenv(t, "IHY_LIBRARIES", " /music/flac , /music/ogg ,,")
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
	if len(c.Libraries) != 2 || c.Libraries[0] != "/music/flac" || c.Libraries[1] != "/music/ogg" {
		t.Errorf("Libraries = %q, want trimmed two-element list", c.Libraries)
	}
}
