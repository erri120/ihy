// Replay-driven tests: decode every recorded payload through the
// typed client methods, offline. Recording happens via
// TestRecordReplays (IHY_MB_RECORD=1, live MusicBrainz, 1 req/s).
package musicbrainz

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const replaysDir = "testdata/replays"

// TestReplayLookupsDecode replays every recorded URL through the
// matching typed lookup and asserts the payload decodes with the
// fields the resolver relies on.
func TestReplayLookupsDecode(t *testing.T) {
	rt, err := NewReplayTransport(replaysDir)
	if err != nil {
		t.Skipf("no replay manifest yet: %v", err)
	}
	client := NewClient(DefaultBaseURL, UserAgent("0.0.1", "test"), rt)
	ctx := context.Background()

	for url := range rt.manifest {
		// URL form: <base>/ws/2/<entity>/<mbid>?inc=...
		rest, ok := strings.CutPrefix(url, client.base+"/ws/2/")
		if !ok {
			t.Errorf("unexpected manifest URL: %s", url)
			continue
		}
		parts := strings.SplitN(rest, "?", 2)
		entityPath := strings.SplitN(parts[0], "/", 2)
		if len(entityPath) != 2 {
			t.Errorf("unparsable manifest URL: %s", url)
			continue
		}
		entity, mbid := entityPath[0], MBID(entityPath[1])

		switch entity {
		case EntityArtist:
			a, err := client.Artist(ctx, mbid)
			if err != nil {
				t.Errorf("artist %s: %v", mbid, err)
				continue
			}
			if a.Name == "" {
				t.Errorf("artist %s: missing name", mbid)
			}
		case EntityReleaseGroup:
			rg, err := client.ReleaseGroup(ctx, mbid)
			if err != nil {
				t.Errorf("release-group %s: %v", mbid, err)
				continue
			}
			if rg.Title == "" || rg.PrimaryType == "" || len(rg.ArtistCredit) == 0 {
				t.Errorf("release-group %s: missing title/type/credits", mbid)
			}
		case EntityRelease:
			r, err := client.Release(ctx, mbid)
			if err != nil {
				t.Errorf("release %s: %v", mbid, err)
				continue
			}
			if r.Title == "" || r.ReleaseGroup.MBID == "" {
				t.Errorf("release %s: missing title or release-group", mbid)
			}
		case EntityRecording:
			r, err := client.Recording(ctx, mbid)
			if err != nil {
				t.Errorf("recording %s: %v", mbid, err)
				continue
			}
			if r.Title == "" || len(r.ArtistCredit) == 0 {
				t.Errorf("recording %s: missing title or credits", mbid)
			}
		case EntityWork:
			w, err := client.Work(ctx, mbid)
			if err != nil {
				t.Errorf("work %s: %v", mbid, err)
				continue
			}
			if w.Title == "" {
				t.Errorf("work %s: missing title", mbid)
			}
		default:
			t.Errorf("unknown entity type in manifest: %s", entity)
		}
	}
	if t.Failed() {
		return
	}
	// Trimmed fields must never appear in replays: the CC BY-NC-SA
	// licensed categories stay out of the repo.
	entries, err := filepath.Glob(filepath.Join(replaysDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{`"tags"`, `"genres"`, `"rating"`, `"annotations"`, `"annotation"`}
	for _, f := range entries {
		if strings.HasSuffix(f, manifestFile) {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range banned {
			if strings.Contains(string(raw), b+":") {
				t.Errorf("%s contains license-restricted field %s", filepath.Base(f), b)
			}
		}
	}
}

// TestTrimFields checks the trimming logic directly.
func TestTrimFields(t *testing.T) {
	raw := []byte(`{"name":"X","tags":[{"name":"metal"}],"genres":[],"rating":{"value":5},
		"relations":[{"artist":{"name":"Y","tags":[{"name":"jazz"}]}}]}`)
	trimmed, err := trimFields(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := string(trimmed)
	for _, banned := range []string{"tags", "genres", "rating"} {
		if strings.Contains(s, `"`+banned+`"`) {
			t.Errorf("trim missed %s: %s", banned, s)
		}
	}
	if !strings.Contains(s, `"name": "X"`) || !strings.Contains(s, `"name": "Y"`) {
		t.Errorf("trim dropped relevant data: %s", s)
	}
}
