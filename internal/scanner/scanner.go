// Package scanner walks read-only library roots and records the files
// it finds together with their raw tags.
//
// The library is immutable input: the scanner only reads (WalkDir does
// lstat-only traversal, tags are read non-destructively). The raw tag
// map is stored as evidence; linking files into the graph is the
// resolver's job, not the scanner's.
package scanner

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.senan.xyz/taglib"
)

// TagReader reads the raw tag map of an audio file. Production uses
// the TagLib WASM build (pure Go, no cgo); tests can inject fakes.
type TagReader func(path string) (map[string][]string, error)

// DefaultTagReader reads tags via go-taglib, preserving multi-valued
// tags as multiple entries per key.
func DefaultTagReader(path string) (map[string][]string, error) {
	return taglib.ReadTags(path)
}

var audioExts = map[string]bool{
	".flac": true, ".mp3": true, ".ogg": true, ".opus": true,
	".m4a": true, ".m4b": true, ".wav": true, ".wma": true,
	".aiff": true, ".aif": true,
}

// Result summarizes one scan pass over a library root.
type Result struct {
	Added     int
	Updated   int
	Removed   int
	Unchanged int
	TagErrors int
}

// existingFile is the scanner-owned slice of a file row.
type existingFile struct {
	mtime time.Time
	size  int64
}

// Scan walks root, reads tags for new or changed audio files, and
// upserts file rows. It never writes to the library.
func Scan(ctx context.Context, db *sql.DB, root string, readTags TagReader) (Result, error) {
	var res Result

	libID, err := upsertLibrary(ctx, db, root)
	if err != nil {
		return res, fmt.Errorf("register library root: %w", err)
	}

	existing, err := loadExisting(ctx, db, libID)
	if err != nil {
		return res, fmt.Errorf("load existing files: %w", err)
	}

	type record struct {
		rel    string
		mtime  time.Time
		size   int64
		format string
	}
	var records []record
	seen := map[string]bool{}

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", p, err)
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !audioExts[ext] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			res.TagErrors++
			log.Printf("scanner: stat %s: %v", p, err)
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("rel path for %s: %w", p, err)
		}
		records = append(records, record{
			rel:    rel,
			mtime:  info.ModTime().Truncate(time.Microsecond),
			size:   info.Size(),
			format: strings.TrimPrefix(ext, "."),
		})
		seen[rel] = true
		return nil
	})
	if err != nil {
		return res, err
	}

	for _, r := range records {
		if e, ok := existing[r.rel]; ok && e.mtime.Equal(r.mtime) && e.size == r.size {
			res.Unchanged++
			continue
		}
		tags, err := readTags(filepath.Join(root, r.rel))
		if err != nil {
			res.TagErrors++
			log.Printf("scanner: read tags %s: %v", r.rel, err)
			continue
		}
		if err := upsertFile(ctx, db, libID, r.rel, r.mtime, r.size, r.format, tags); err != nil {
			return res, fmt.Errorf("upsert %s: %w", r.rel, err)
		}
		if _, ok := existing[r.rel]; ok {
			res.Updated++
		} else {
			res.Added++
		}
	}

	for rel := range existing {
		if seen[rel] {
			continue
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM file WHERE library_id = $1 AND path = $2`, libID, rel); err != nil {
			return res, fmt.Errorf("delete %s: %w", rel, err)
		}
		res.Removed++
	}
	return res, nil
}

// RunAll scans every given library root sequentially in the calling
// goroutine. Intended to be started in the background right after
// boot so startup never blocks on a large library. Later triggers
// (schedule, web UI) enqueue the same work as River jobs.
func RunAll(ctx context.Context, db *sql.DB, roots []string, readTags TagReader) {
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			log.Printf("scanner: library root %s unavailable: %v", root, err)
			continue
		}
		res, err := Scan(ctx, db, root, readTags)
		if err != nil {
			log.Printf("scanner: scan %s failed: %v", root, err)
			continue
		}
		log.Printf("scanner: %s: %d added, %d updated, %d removed, %d unchanged, %d tag errors",
			root, res.Added, res.Updated, res.Removed, res.Unchanged, res.TagErrors)
	}
}

func upsertLibrary(ctx context.Context, db *sql.DB, root string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO library (root_path) VALUES ($1)
		ON CONFLICT (root_path) DO UPDATE SET active = true
		RETURNING id`, root).Scan(&id)
	return id, err
}

func loadExisting(ctx context.Context, db *sql.DB, libID int64) (map[string]existingFile, error) {
	rows, err := db.QueryContext(ctx, `SELECT path, mtime, size_bytes FROM file WHERE library_id = $1`, libID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]existingFile{}
	for rows.Next() {
		var path string
		var e existingFile
		if err := rows.Scan(&path, &e.mtime, &e.size); err != nil {
			return nil, err
		}
		out[path] = e
	}
	return out, rows.Err()
}

// upsertFile only touches scanner-owned columns; release_mbid and
// recording_mbid belong to the resolver and are left alone on re-scan.
func upsertFile(ctx context.Context, db *sql.DB, libID int64, rel string, mtime time.Time, size int64, format string, tags map[string][]string) error {
	raw, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("encode tags: %w", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO file (library_id, path, mtime, size_bytes, format, tags)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (path) DO UPDATE SET
			library_id = EXCLUDED.library_id,
			mtime      = EXCLUDED.mtime,
			size_bytes = EXCLUDED.size_bytes,
			format     = EXCLUDED.format,
			tags       = EXCLUDED.tags`,
		libID, rel, mtime, size, format, raw)
	return err
}
