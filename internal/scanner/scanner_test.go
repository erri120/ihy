package scanner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.senan.xyz/taglib"

	"ihy/internal/db"
	"ihy/internal/dbtest"
)

// fakeTags pretends to be a tag reader for a few known files.
func fakeTags(path string) (map[string][]string, error) {
	switch filepath.Base(path) {
	case "a.flac":
		return map[string][]string{"ARTISTS": {"Eskimo Callboy"}, "TITLE": {"Hypa Hypa"}}, nil
	case "b.mp3":
		return map[string][]string{"ARTISTS": {"Turbo", "GUMI"}}, nil
	default:
		return nil, errors.New("unreadable tags")
	}
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := dbtest.Server(t)
	if _, err := db.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func touch(t *testing.T, path string) {
	t.Helper()
	now := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func fileRow(t *testing.T, db *sql.DB, path string) (format string, size int64, tags map[string][]string) {
	t.Helper()
	var raw []byte
	err := db.QueryRow(`SELECT format, size_bytes, tags FROM file WHERE path = $1`, path).Scan(&format, &size, &raw)
	if err != nil {
		t.Fatalf("file row %q: %v", path, err)
	}
	if err := json.Unmarshal(raw, &tags); err != nil {
		t.Fatalf("decode tags: %v", err)
	}
	return format, size, tags
}

func TestScanWalksReadsAndSyncs(t *testing.T) {
	conn := openDB(t)
	root := t.TempDir()

	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.flac", "aaa")
	write("nested/b.mp3", "bbb")
	write("notes.txt", "not audio")

	ctx := context.Background()
	res, err := Scan(ctx, conn, root, fakeTags)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Added != 2 || res.Updated != 0 || res.Unchanged != 0 || res.TagErrors != 0 {
		t.Fatalf("first scan: %+v, want added=2", res)
	}
	format, size, tags := fileRow(t, conn, "a.flac")
	if format != "flac" || size != 3 {
		t.Errorf("a.flac row wrong: format=%q size=%d", format, size)
	}
	if got := tags["ARTISTS"]; len(got) != 1 || got[0] != "Eskimo Callboy" {
		t.Errorf("a.flac tags = %v", got)
	}
	_, _, bTags := fileRow(t, conn, "nested/b.mp3")
	if got := bTags["ARTISTS"]; len(got) != 2 || got[1] != "GUMI" {
		t.Errorf("multi-valued tag lost: %v", got)
	}

	// Second scan: nothing changed.
	res, err = Scan(ctx, conn, root, fakeTags)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if res.Unchanged != 2 || res.Updated != 0 || res.Added != 0 {
		t.Fatalf("second scan: %+v, want unchanged=2", res)
	}

	// Change b.mp3 (mtime moves), remove a.flac.
	touch(t, filepath.Join(root, "nested/b.mp3"))
	os.Remove(filepath.Join(root, "a.flac"))
	res, err = Scan(ctx, conn, root, fakeTags)
	if err != nil {
		t.Fatalf("third scan: %v", err)
	}
	if res.Updated != 1 || res.Removed != 1 || res.Unchanged != 0 {
		t.Fatalf("third scan: %+v, want updated=1 removed=1", res)
	}
	var count int
	if err := conn.QueryRow(`SELECT count(*) FROM file`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("file rows after third scan: %d, want 1", count)
	}
}

// The real TagLib reader must preserve multi-valued tags and unicode
// text from the user's fixture library (IHY_FIXTURE_DIR).
func TestDefaultTagReaderOnFixtures(t *testing.T) {
	fixtureDir := os.Getenv("IHY_FIXTURE_DIR")
	if fixtureDir == "" {
		t.Skip("IHY_FIXTURE_DIR not set; skipping fixture test")
	}

	multiValue, nonASCII := 0, 0
	err := filepath.WalkDir(fixtureDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(p), ".flac") {
			return nil
		}
		tags, err := taglib.ReadTags(p)
		if err != nil {
			return err
		}
		if len(tags["ARTISTS"]) > 1 {
			multiValue++
		}
		for _, v := range tags["ARTIST"] {
			for _, r := range v {
				if r > 0x2E80 { // CJK range: proves unicode survives the WASM round-trip
					nonASCII++
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixtures: %v", err)
	}
	if multiValue < 2 {
		t.Errorf("multi-valued ARTISTS found in %d files, want >= 2; multi-value support is broken", multiValue)
	}
	if nonASCII == 0 {
		t.Error("no unicode artist names found; the fixtures contain Japanese names")
	}
}
