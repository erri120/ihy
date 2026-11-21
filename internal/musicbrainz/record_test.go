// Recording driver: walks the fixture library, collects the MBIDs the
// resolver will need, and records trimmed replays from live
// MusicBrainz at the documented 1 req/s.
//
// Usage: make record-replays  (env IHY_MB_RECORD=1 IHY_FIXTURE_DIR=...)
//
// VA (the "Various Artists" pseudo-artist) is deliberately never
// recorded: the resolver skips it entirely.
package musicbrainz

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"ihy/internal/scanner"
)

const vaArtistMBID = MBID("89ad4ac3-39f7-470e-963a-56509c546377")

// wantedFromTags collects MBIDs per entity type from one file's tags.
// Role tags without IDs (e.g. LYRICIST values) are ignored: the MB
// lookup supplies those relationships.
func wantedFromTags(tags map[string][]string) map[string][]MBID {
	out := map[string][]MBID{}
	add := func(key, entity string) {
		for _, raw := range tags[key] {
			for _, id := range strings.Split(raw, ";") {
				id = strings.TrimSpace(id)
				if id == "" || MBID(id) == vaArtistMBID {
					continue
				}
				if !contains(out[entity], MBID(id)) {
					out[entity] = append(out[entity], MBID(id))
				}
			}
		}
	}
	add("MUSICBRAINZ_ARTISTID", EntityArtist)
	add("MUSICBRAINZ_ALBUMARTISTID", EntityArtist)
	add("MUSICBRAINZ_COMPOSERID", EntityArtist)
	add("MUSICBRAINZ_TRACKID", EntityRecording)
	add("MUSICBRAINZ_WORKID", EntityWork)
	add("MUSICBRAINZ_ALBUMID", EntityRelease)
	add("MUSICBRAINZ_RELEASEGROUPID", EntityReleaseGroup)
	return out
}

func contains(list []MBID, v MBID) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}

// lookupURL builds the WS/2 lookup URL for an entity, mirroring the
// client so the recording driver can skip already-recorded requests.
func lookupURL(entityType string, mbid MBID) string {
	inc := map[string]string{
		EntityArtist:       incArtist,
		EntityReleaseGroup: incRG,
		EntityRelease:      incRelease,
		EntityRecording:    incRecording,
		EntityWork:         incWork,
	}[entityType]
	return fmt.Sprintf("%s/ws/2/%s/%s?inc=%s&fmt=json", DefaultBaseURL, entityType, mbid, inc)
}

func TestRecordReplays(t *testing.T) {
	if os.Getenv("IHY_MB_RECORD") != "1" {
		t.Skip("set IHY_MB_RECORD=1 to record replays from live MusicBrainz")
	}
	fixtureDir := os.Getenv("IHY_FIXTURE_DIR")
	if fixtureDir == "" {
		t.Fatal("IHY_FIXTURE_DIR not set")
	}

	wanted := map[string][]MBID{}
	err := filepath.WalkDir(fixtureDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".flac", ".mp3", ".ogg", ".opus", ".m4a":
		default:
			return nil
		}
		tags, err := scanner.DefaultTagReader(p)
		if err != nil {
			return fmt.Errorf("read tags %s: %w", p, err)
		}
		for entity, ids := range wantedFromTags(tags) {
			for _, id := range ids {
				if !contains(wanted[entity], id) {
					wanted[entity] = append(wanted[entity], id)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	rec, err := NewRecordingTransport(replaysDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := NewReplayTransport(replaysDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var transport http.RoundTripper = rec
	if replay != nil {
		// Replay-first: recorded URLs are served offline (but still
		// traversed for their relations); misses are fetched live and
		// recorded.
		transport = NewReplayFirstTransport(replay, rec)
	}
	client := NewClient(DefaultBaseURL, UserAgent("0.0.1", DefaultContactURL), transport)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// IHY_MB_LIMIT caps the number of NEW (live) lookups per run:
	// verify a small batch before committing to the full pass.
	limit := 0
	if raw := os.Getenv("IHY_MB_LIMIT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}

	// The queue helpers mirror the resolver's traversal exactly:
	// fetching a recording or work reveals artists that exist only
	// inside payloads (performers tagged without IDs); VA is skipped.
	queueRelArtists := func(rels []Relation) {
		for _, rel := range rels {
			if rel.TargetType != "artist" || rel.Artist == nil || rel.Artist.MBID == "" || rel.Artist.MBID == vaArtistMBID {
				continue
			}
			if !contains(wanted[EntityArtist], rel.Artist.MBID) {
				wanted[EntityArtist] = append(wanted[EntityArtist], rel.Artist.MBID)
			}
		}
	}
	queueWorkTargets := func(rels []Relation) {
		for _, rel := range rels {
			if rel.TargetType != "work" || rel.Work == nil || rel.Work.MBID == "" {
				continue
			}
			if !contains(wanted[EntityWork], rel.Work.MBID) {
				wanted[EntityWork] = append(wanted[EntityWork], rel.Work.MBID)
			}
		}
	}
	queueArtistCredits := func(entries []ArtistCreditEntry) {
		for _, e := range entries {
			if e.Artist.MBID == "" || e.Artist.MBID == vaArtistMBID {
				continue
			}
			if !contains(wanted[EntityArtist], e.Artist.MBID) {
				wanted[EntityArtist] = append(wanted[EntityArtist], e.Artist.MBID)
			}
		}
	}
	fetchOne := func(entity string, id MBID) error {
		switch entity {
		case EntityArtist:
			_, err := client.Artist(ctx, id)
			return err
		case EntityReleaseGroup:
			rg, err := client.ReleaseGroup(ctx, id)
			if err != nil {
				return err
			}
			queueArtistCredits(rg.ArtistCredit)
			return nil
		case EntityRelease:
			rel, err := client.Release(ctx, id)
			if err != nil {
				return err
			}
			queueArtistCredits(rel.ArtistCredit)
			return nil
		case EntityRecording:
			rec, err := client.Recording(ctx, id)
			if err != nil {
				return err
			}
			queueRelArtists(rec.Relations)
			queueWorkTargets(rec.Relations)
			return nil
		case EntityWork:
			w, err := client.Work(ctx, id)
			if err != nil {
				return err
			}
			queueRelArtists(w.Relations)
			return nil
		}
		return fmt.Errorf("unknown entity type %s", entity)
	}

	var entityOrder = []string{EntityRecording, EntityRelease, EntityReleaseGroup, EntityWork, EntityArtist}
	newLookups := 0
	for _, entity := range entityOrder {
		ids := wanted[entity]
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			url := lookupURL(entity, id)
			_, recorded := rec.seen[url]
			if !recorded {
				newLookups++
				if limit > 0 && newLookups > limit {
					t.Logf("limit %d reached after %d new lookups", limit, newLookups-1)
					return
				}
			}
			err := fetchOne(entity, id)
			if err == ErrNotFound {
				t.Logf("entity %s %s not found (stale MBID?), skipping", entity, id)
				continue
			}
			if err != nil {
				t.Errorf("lookup %s %s: %v", entity, id, err)
			}
		}
	}
	t.Logf("manifest now holds %d recorded URLs", len(rec.seen))
}
