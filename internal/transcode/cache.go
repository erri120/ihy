// Cache and warming half of the transcoder.
//
// Cache entry naming (all under IHY_TRANSCODE_DIR):
//
//	<key>.<container>        played: a real request served it
//	<key>.spec.<container>   speculative: warmed for the queue, never played
//	<key>.part               in-progress encode, renamed on success
//
// The key hashes source path + mtime + profile, so a rescan
// invalidates naturally. File mtimes are the only clocks: touched on
// every served hit (LRU), creation time for spec entries (TTL), which
// keeps the cache stateless across restarts. Sweeps run after
// finished encodes.
package transcode

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// WarmItem is one cacheable encode: a source file and the profile to
// apply.
type WarmItem struct {
	SrcPath string
	Mtime   time.Time
	Profile Profile
}

// Key derives the cache entry name. The source mtime participates so
// rescans invalidate naturally.
func (it WarmItem) Key() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s|%d",
		it.SrcPath, it.Mtime.UnixNano(), it.Profile.Container, it.Profile.Codec, it.Profile.BitrateKbps)))
	return fmt.Sprintf("%x", sum)
}

// inflight is one running encode; concurrent requests and warm jobs
// for the same key coalesce on it. Fields are written before done is
// closed and read after, so the channel provides the synchronization.
type inflight struct {
	done chan struct{}
	spec bool
	err  error
}

// Service owns the transcode cache directory.
type Service struct {
	dir      string
	enc      Encoder
	maxBytes int64
	specTTL  time.Duration
	ttl      time.Duration
	now      func() time.Time // injectable for tests

	mu       sync.Mutex
	inflight map[string]*inflight

	// warm worker state. Warm replaces the queue wholesale: a fresh
	// queue head supersedes pending work the new list dropped, and a
	// running encode whose key is no longer wanted is cancelled.
	warmMu     sync.Mutex
	wake       chan struct{}
	warmQueue  []WarmItem
	currentKey string
	warmCancel context.CancelFunc
	warmStop   chan struct{}
	warmOnce   sync.Once
}

// New creates the cache directory and starts the warm worker. Zero
// TTLs fall back to the documented defaults.
func New(dir string, enc Encoder, maxBytes int64, specTTL, ttl time.Duration) (*Service, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("transcode cache dir %s: %w", dir, err)
	}
	if specTTL <= 0 {
		specTTL = 24 * time.Hour
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	s := &Service{
		dir:      dir,
		enc:      enc,
		maxBytes: maxBytes,
		specTTL:  specTTL,
		ttl:      ttl,
		now:      time.Now,
		inflight: map[string]*inflight{},
		wake:     make(chan struct{}, 1),
		warmStop: make(chan struct{}),
	}
	go s.warmLoop()
	return s, nil
}

// Close stops the warm worker and cancels a running warm encode.
func (s *Service) Close() {
	s.warmOnce.Do(func() { close(s.warmStop) })
	s.warmMu.Lock()
	if s.warmCancel != nil {
		s.warmCancel()
	}
	s.warmMu.Unlock()
}

// names returns the played, spec and part file names for a key.
func names(key, container string) (played, spec, part string) {
	return key + "." + container, key + ".spec." + container, key + ".part"
}

// locate returns the existing played or spec path for the key.
func (s *Service) locate(key, container string) (playedPath, specPath string) {
	played, spec, _ := names(key, container)
	if st, err := os.Stat(filepath.Join(s.dir, played)); err == nil && st.Mode().IsRegular() {
		return filepath.Join(s.dir, played), ""
	}
	if st, err := os.Stat(filepath.Join(s.dir, spec)); err == nil && st.Mode().IsRegular() {
		return "", filepath.Join(s.dir, spec)
	}
	return "", ""
}

// Get returns the cached transcoded output for a real request,
// encoding it synchronously if absent. Concurrent requests for the
// same key coalesce; a speculative encode in flight for the same key
// is awaited and promoted to played. A superseded warm encode is
// retried as a played encode.
func (s *Service) Get(ctx context.Context, srcPath string, mtime time.Time, p Profile) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		it := WarmItem{SrcPath: srcPath, Mtime: mtime, Profile: p}
		key := it.Key()

		if played, spec := s.locate(key, p.Container); played != "" || spec != "" {
			if played != "" {
				s.touch(played)
				return played, nil
			}
			playedName, _, _ := names(key, p.Container)
			playedPath := filepath.Join(s.dir, playedName)
			if err := os.Rename(spec, playedPath); err == nil {
				s.touch(playedPath)
				return playedPath, nil
			}
			// Rename raced with a sweep: encode afresh.
		}

		j, mine := s.claim(key)
		if !mine {
			select {
			case <-j.done:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			if j.err == nil {
				if played, spec := s.locate(key, p.Container); played != "" {
					return played, nil
				} else if spec != "" {
					playedName, _, _ := names(key, p.Container)
					playedPath := filepath.Join(s.dir, playedName)
					if err := os.Rename(spec, playedPath); err == nil {
						s.touch(playedPath)
						return playedPath, nil
					}
				}
				continue // vanished; retry
			}
			// A speculative warm encode that was superseded or hit a
			// transient failure: the real request retries as played.
			if j.spec {
				continue
			}
			return "", j.err
		}
		return s.runEncode(ctx, key, it, false, j)
	}
	return "", fmt.Errorf("transcode: could not complete encode for %s", srcPath)
}

// claim registers (or joins) the inflight job for the key. mine=true
// means the caller owns the encode.
func (s *Service) claim(key string) (*inflight, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.inflight[key]; ok {
		return j, false
	}
	j := &inflight{done: make(chan struct{})}
	s.inflight[key] = j
	return j, true
}

// runEncode performs the encode for an owned job: temp file, atomic
// rename on success, sweep after. spec marks a warm-only output. The
// context is the caller's: a request disconnect or a superseded warm
// job kills ffmpeg mid-flight.
func (s *Service) runEncode(ctx context.Context, key string, it WarmItem, spec bool, j *inflight) (string, error) {
	defer func() {
		s.mu.Lock()
		delete(s.inflight, key)
		close(j.done)
		s.mu.Unlock()
	}()
	j.spec = spec

	played, specName, part := names(key, it.Profile.Container)
	out := filepath.Join(s.dir, played)
	if spec {
		out = filepath.Join(s.dir, specName)
	}

	err := s.enc.Encode(ctx, it.SrcPath, it.Profile, filepath.Join(s.dir, part))
	if err == nil {
		err = os.Rename(filepath.Join(s.dir, part), out)
	} else {
		os.Remove(filepath.Join(s.dir, part))
	}
	j.err = err
	if err == nil {
		s.sweep()
		return out, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	return "", err
}

// maxWarmItems caps the warm queue; clients only need a few seconds of
// lookahead.
const maxWarmItems = 5

// Warm replaces the pending warm queue with items (advisory; the
// server may drop, cap or dedupe it) and starts speculative encodes
// for them, oldest queue position first. Items already cached are
// skipped.
func (s *Service) Warm(items []WarmItem) {
	s.warmMu.Lock()
	defer s.warmMu.Unlock()

	kept := make([]WarmItem, 0, len(items))
	wanted := map[string]bool{}
	for _, it := range items {
		played, _ := s.locate(it.Key(), it.Profile.Container)
		if played != "" {
			continue // already cacheable
		}
		k := it.Key()
		if !wanted[k] {
			wanted[k] = true
			kept = append(kept, it)
		}
	}
	if len(kept) > maxWarmItems {
		kept = kept[:maxWarmItems]
	}
	s.warmQueue = kept
	if s.warmCancel != nil && !wanted[s.currentKey] {
		s.warmCancel() // superseded
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) warmLoop() {
	for {
		s.warmMu.Lock()
		if len(s.warmQueue) == 0 {
			s.warmMu.Unlock()
			select {
			case <-s.warmStop:
				return
			case <-s.wake:
			}
			continue
		}
		it := s.warmQueue[0]
		s.warmQueue = s.warmQueue[1:]
		ctx, cancel := context.WithCancel(context.Background())
		s.warmCancel = cancel
		s.currentKey = it.Key()
		s.warmMu.Unlock()

		key := it.Key()
		if j, mine := s.claim(key); mine {
			// Speculative encode; the request path may join this job.
			s.runEncode(ctx, key, it, true, j)
		} else {
			<-j.done // a real request is already on it
		}
		cancel()

		s.warmMu.Lock()
		s.warmCancel = nil
		s.currentKey = ""
		s.warmMu.Unlock()
	}
}

// touch refreshes the entry's LRU clock.
func (s *Service) touch(path string) {
	now := s.now()
	os.Chtimes(path, now, now)
}

// sweep enforces the TTLs and the byte cap. For played entries mtime
// is the last-access time (touch), for spec entries the creation
// time; both clocks come from the filesystem, so the cache is
// stateless across restarts. Stale keys (rescan churn) age out via
// the same TTL.
func (s *Service) sweep() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	now := s.now()
	var files []os.FileInfo
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, info)
		total += info.Size()
	}
	for _, info := range files {
		name := info.Name()
		age := now.Sub(info.ModTime())
		switch {
		case strings.HasSuffix(name, ".part"):
			if age > s.ttl {
				os.Remove(filepath.Join(s.dir, name)) // crashed encode
				total -= info.Size()
			}
		case strings.Contains(name, ".spec."):
			if age > s.specTTL {
				os.Remove(filepath.Join(s.dir, name))
				total -= info.Size()
			}
		default:
			if age > s.ttl {
				os.Remove(filepath.Join(s.dir, name))
				total -= info.Size()
			}
		}
	}
	if s.maxBytes <= 0 || total <= s.maxBytes {
		return
	}
	// LRU: oldest mtime first; parts are skipped (they belong to a
	// running encode).
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime().Before(files[j].ModTime()) })
	for _, info := range files {
		if total <= s.maxBytes {
			return
		}
		if strings.HasSuffix(info.Name(), ".part") {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, info.Name())); err == nil {
			total -= info.Size()
		}
	}
}
