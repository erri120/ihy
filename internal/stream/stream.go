// Package stream serves physical audio files and warms upcoming ones
// into the OS page cache.
//
// The warmer is advisory by design: the kernel page cache is the only
// cache (shared, evictable under memory pressure, no process memory),
// and a lost or slow hint degrades to the plain cold read, never to a
// broken request. Clients must not depend on warming happening.
package stream

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// maxUpcoming caps the advisory X-Ihy-Upcoming list; clients only need
// a few seconds of lookahead, and the cap bounds per-request work.
const maxUpcoming = 5

// Warmer prefetches files into the page cache.
type Warmer interface {
	// Warm is fire-and-forget and advisory: it may drop requests
	// under load and never reports errors to the caller.
	Warm(fileIDs []int64)
}

// Compile-time interface assertion, per project convention.
var _ Warmer = (*Service)(nil)

// Service streams audio files from the library roots on top of the
// database's file rows.
type Service struct {
	db *sql.DB
	// warm warms one absolute path; swappable for tests. Called
	// only with validated paths from the database.
	warm func(absPath string)
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db, warm: fadviseWarm}
}

// ErrUnknownFile says the file id has no row in the database.
var ErrUnknownFile = errors.New("unknown file")

// Source describes one resolvable file.
type Source struct {
	AbsPath string
	Size    int64
	ModTime time.Time
	Format  string
}

// Resolve joins the file row with its library root and validates the
// stored path. The scanner only writes library-relative paths, but the
// streamer re-checks: it is the one handler that turns a DB value into
// a filesystem access.
func (s *Service) Resolve(fileID int64) (Source, error) {
	var root, rel, format string
	var size int64
	var modTime time.Time
	err := s.db.QueryRow(`
		SELECT l.root_path, f.path, f.size_bytes, f.mtime, f.format
		FROM file f JOIN library l ON l.id = f.library_id
		WHERE f.id = $1`, fileID).
		Scan(&root, &rel, &size, &modTime, &format)
	if errors.Is(err, sql.ErrNoRows) {
		return Source{}, fmt.Errorf("stream: file %d: %w", fileID, ErrUnknownFile)
	}
	if err != nil {
		return Source{}, fmt.Errorf("stream: file %d: %w", fileID, err)
	}
	if err := validateRelPath(rel); err != nil {
		return Source{}, fmt.Errorf("stream: file %d: %w", fileID, err)
	}
	return Source{
		AbsPath: filepath.Join(root, rel),
		Size:    size,
		ModTime: modTime,
		Format:  format,
	}, nil
}

// Warm schedules the advisory page-cache warming in the background so
// the caller (an HTTP handler) never blocks on NFS reads. It caps,
// dedupes and silently skips unresolvable ids.
func (s *Service) Warm(fileIDs []int64) {
	go func() { s.warmSync(fileIDs) }()
}

// warmSync warms synchronously; Warm wraps it in one goroutine. Tests
// call it directly for determinism.
func (s *Service) warmSync(fileIDs []int64) {
	seen := make(map[int64]bool, len(fileIDs))
	for _, id := range fileIDs {
		if len(seen) >= maxUpcoming {
			return
		}
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		src, err := s.Resolve(id)
		if err != nil {
			continue // advisory: skip quietly
		}
		s.warm(src.AbsPath)
	}
}

// validateRelPath rejects absolute paths and anything escaping the
// library root.
func validateRelPath(rel string) error {
	if rel == "" {
		return errors.New("empty path")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "~") {
		return fmt.Errorf("not library-relative: %q", rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("escapes library root: %q", rel)
	}
	if clean != rel {
		return fmt.Errorf("not a clean relative path: %q", rel)
	}
	return nil
}

// ContentType maps the scanner's raw format values to MIME types. The
// scanner stores the container format verbatim; unknown values fall
// back to octet-stream rather than a guessed type.
func ContentType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "flac":
		return "audio/flac"
	case "mp3":
		return "audio/mpeg"
	case "ogg", "oga":
		return "audio/ogg"
	case "opus":
		return "audio/ogg"
	case "m4a", "mp4", "alac":
		return "audio/mp4"
	case "aac":
		return "audio/aac"
	case "wav", "wave":
		return "audio/wav"
	case "aiff", "aif":
		return "audio/aiff"
	case "ape":
		return "audio/ape"
	case "wv":
		return "audio/wavpack"
	case "wma":
		return "audio/x-ms-wma"
	default:
		return "application/octet-stream"
	}
}

// ParseUpcoming parses the advisory X-Ihy-Upcoming header value:
// comma-separated file ids. Malformed entries are skipped, the current
// file excluded, duplicates dropped, and the result capped at
// maxUpcoming.
func ParseUpcoming(header string, current int64) []int64 {
	var out []int64
	seen := make(map[int64]bool)
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var id int64
		if _, err := fmt.Sscanf(part, "%d", &id); err != nil {
			continue
		}
		if id <= 0 || id == current || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= maxUpcoming {
			break
		}
	}
	return out
}

// ETag builds a strong validator from the DB row, so it survives
// stat churn the same way the serving path does.
func (src Source) ETag() string {
	return fmt.Sprintf(`"%x-%x"`, src.ModTime.Unix(), src.Size)
}
