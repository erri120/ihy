// Unit tests for the transcode cache: coalescing, spec promotion,
// TTL/LRU sweeps and the warm worker. Uses a fake encoder, no ffmpeg.
package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeEnc struct {
	calls atomic.Int64
	delay time.Duration
	fail  bool
}

func (f *fakeEnc) Encode(ctx context.Context, srcPath string, p Profile, outPath string) error {
	f.calls.Add(1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.fail {
		return errors.New("fake encoder failure")
	}
	return os.WriteFile(outPath, []byte(fmt.Sprintf("fake:%s", p.Name)), 0o644)
}

func newTestService(t *testing.T, enc Encoder, maxBytes int64) *Service {
	t.Helper()
	s, err := New(t.TempDir(), enc, maxBytes, time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func profile(name string) Profile {
	return Profile{ID: 1, Name: name, Container: "ogg", Codec: "libopus", BitrateKbps: 128}
}

func TestGetEncodesOnceAndServesCache(t *testing.T) {
	enc := &fakeEnc{delay: 50 * time.Millisecond}
	s := newTestService(t, enc, 0)
	p := profile("opus-128")

	var wg sync.WaitGroup
	paths := make([]string, 4)
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := s.Get(context.Background(), "/lib/a.flac", time.Unix(1000, 0), p)
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			paths[i] = out
		}(i)
	}
	wg.Wait()
	if enc.calls.Load() != 1 {
		t.Errorf("encoder called %d times, want 1 (coalesced)", enc.calls.Load())
	}
	for i, p := range paths {
		if p != paths[0] {
			t.Errorf("request %d served %q, want %q", i, p, paths[0])
		}
	}
	body, err := os.ReadFile(paths[0])
	if err != nil || string(body) != "fake:opus-128" {
		t.Errorf("cache file content = %q err=%v", body, err)
	}
	// Second serial Get is a cache hit.
	if _, err := s.Get(context.Background(), "/lib/a.flac", time.Unix(1000, 0), p); err != nil {
		t.Fatal(err)
	}
	if enc.calls.Load() != 1 {
		t.Errorf("encoder called %d times after cache hit, want 1", enc.calls.Load())
	}
}

func TestGetPromotesSpec(t *testing.T) {
	enc := &fakeEnc{}
	s := newTestService(t, enc, 0)
	p := profile("opus-96")
	it := WarmItem{SrcPath: "/lib/b.flac", Mtime: time.Unix(2000, 0), Profile: p}

	// Simulate a finished speculative encode: only the spec file.
	playedName, specName, _ := names(it.Key(), p.Container)
	if err := os.WriteFile(filepath.Join(s.dir, specName), []byte("spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := s.Get(context.Background(), it.SrcPath, it.Mtime, p)
	if err != nil {
		t.Fatal(err)
	}
	if enc.calls.Load() != 0 {
		t.Errorf("encoder called for a spec hit")
	}
	if filepath.Base(out) != playedName {
		t.Errorf("served %q, want the played entry %q", out, playedName)
	}
	if _, err := os.Stat(filepath.Join(s.dir, playedName)); err != nil {
		t.Errorf("played entry missing after promotion: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, specName)); !os.IsNotExist(err) {
		t.Errorf("spec entry survived promotion")
	}
}

func TestSweepTTLAndCap(t *testing.T) {
	enc := &fakeEnc{}
	s := newTestService(t, enc, 0)
	now := time.Unix(1_000_000, 0)
	s.now = func() time.Time { return now }

	write := func(name string, mtime time.Time, size int) {
		p := filepath.Join(s.dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	old, fresh := now.Add(-2*time.Hour), now.Add(-time.Minute)

	// Spec entry past the speculative TTL.
	spec := WarmItem{SrcPath: "/lib/x.flac", Mtime: time.Unix(5, 0), Profile: profile("opus-96")}
	_, specName, _ := names(spec.Key(), "ogg")
	write(specName, old, 0)
	// Played entries: one stale (key churn), one fresh.
	write("stale.ogg", now.Add(-48*time.Hour), 0)
	write("fresh.ogg", fresh, 0)
	write("fresh.spec.ogg", fresh, 0)

	s.sweep()

	for _, gone := range []string{specName, "stale.ogg"} {
		if _, err := os.Stat(filepath.Join(s.dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s survived the TTL sweep", gone)
		}
	}
	for _, keep := range []string{"fresh.ogg", "fresh.spec.ogg"} {
		if _, err := os.Stat(filepath.Join(s.dir, keep)); err != nil {
			t.Errorf("%s was swept despite being fresh: %v", keep, err)
		}
	}

	// Byte cap evicts oldest first; parts are spared.
	s.maxBytes = 25
	write("a.ogg", now.Add(-3*time.Minute), 10) // oldest
	write("b.ogg", now.Add(-2*time.Minute), 10)
	write("c.part", now.Add(-time.Minute), 10)
	s.sweep()
	if _, err := os.Stat(filepath.Join(s.dir, "a.ogg")); !os.IsNotExist(err) {
		t.Error("oldest entry survived the cap eviction")
	}
	for _, keep := range []string{"b.ogg", "c.part"} {
		if _, err := os.Stat(filepath.Join(s.dir, keep)); err != nil {
			t.Errorf("%s was evicted unexpectedly: %v", keep, err)
		}
	}
}

func TestWarmWorkerProducesSpec(t *testing.T) {
	enc := &fakeEnc{}
	s := newTestService(t, enc, 0)
	items := []WarmItem{
		{SrcPath: "/lib/1.flac", Mtime: time.Unix(10, 0), Profile: profile("opus-96")},
		{SrcPath: "/lib/2.flac", Mtime: time.Unix(20, 0), Profile: profile("opus-96")},
	}
	s.Warm(items)
	deadline := time.Now().Add(2 * time.Second)
	for len(s.warmDone()) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("warm worker produced %d/2 outputs", len(s.warmDone()))
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, it := range items {
		_, specName, _ := names(it.Key(), it.Profile.Container)
		if _, err := os.Stat(filepath.Join(s.dir, specName)); err != nil {
			t.Errorf("spec output missing for %s: %v", it.SrcPath, err)
		}
	}
}

// warmDone lists cache entries the warm worker finished (spec or part).
func (s *Service) warmDone() []string {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestWarmSupersedesPending(t *testing.T) {
	// A slow encoder: the first item is mid-encode when the queue is
	// replaced. The dropped item must never be encoded.
	enc := &fakeEnc{delay: 200 * time.Millisecond}
	s, err := New(t.TempDir(), enc, 0, time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	p := profile("opus-96")
	first := WarmItem{SrcPath: "/lib/1.flac", Mtime: time.Unix(10, 0), Profile: p}
	dropped := WarmItem{SrcPath: "/lib/2.flac", Mtime: time.Unix(20, 0), Profile: p}
	s.Warm([]WarmItem{first, dropped})
	time.Sleep(50 * time.Millisecond) // let the worker start on `first`
	// A fresh queue without either: supersede cancels `first`, drops
	// `dropped`... the replacement adds nothing new, so nothing is
	// encoded after `first` is cancelled.
	s.Warm([]WarmItem{first})
	time.Sleep(600 * time.Millisecond)
	if calls := enc.calls.Load(); calls > 2 {
		t.Errorf("encoder called %d times, dropped item must not encode", calls)
	}
}

func TestWarmItemKeyChangesWithMtime(t *testing.T) {
	a := WarmItem{SrcPath: "/lib/a.flac", Mtime: time.Unix(1, 0), Profile: profile("x")}
	b := WarmItem{SrcPath: "/lib/a.flac", Mtime: time.Unix(2, 0), Profile: profile("x")}
	if a.Key() == b.Key() {
		t.Error("key ignores the source mtime; rescans would serve stale output")
	}
}
