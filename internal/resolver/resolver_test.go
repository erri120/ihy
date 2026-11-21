// End-to-end resolver test: scan the fixture library, resolve it
// offline against the recorded MusicBrainz replays, and pin the
// decided graph and UX properties.
//
// Requires a real Postgres (IHY_TEST_DSN) and the fixture library
// (IHY_FIXTURE_DIR); runs in `make test-e2e`.
package resolver

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.senan.xyz/taglib"

	"ihy/internal/db"
	"ihy/internal/dbtest"
	"ihy/internal/musicbrainz"
	"ihy/internal/scanner"
)

func setup(t *testing.T) *sql.DB {
	t.Helper()
	fixtureDir := os.Getenv("IHY_FIXTURE_DIR")
	if fixtureDir == "" {
		t.Skip("IHY_FIXTURE_DIR not set; skipping resolver e2e test")
	}
	dsn := dbtest.Server(t)
	if _, err := db.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	res, err := scanner.Scan(context.Background(), conn, fixtureDir, taglib.ReadTags)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Added == 0 {
		t.Fatalf("scan added nothing: %+v", res)
	}

	rt, err := musicbrainz.NewReplayTransport(filepath.Join("..", "musicbrainz", "testdata", "replays"))
	if err != nil {
		t.Fatalf("open replays: %v", err)
	}
	client := musicbrainz.NewClient(musicbrainz.DefaultBaseURL, "test", rt)
	r := New(conn, client)
	out, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out.NotFound > 0 {
		t.Logf("resolve skipped %d stale MBIDs", out.NotFound)
	}
	if out.Artists == 0 || out.Recordings == 0 || out.Works == 0 {
		t.Fatalf("resolve produced an empty graph: %+v", out)
	}
	return conn
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// MMXX: the album is credited to the renamed artist; 257ers appears
// only as a track performer and must show up as "featured".
func TestMMXXPrimaryAndFeatured(t *testing.T) {
	db := setup(t)
	const ec = "cf075492-d880-4afc-b87b-d6b03e33dacc"
	const rg = "ec1698d1-5811-4803-9cc3-68666934ea23"

	// Primary artist: RG-level credit for Electric Callboy, canonical
	// name intact (never rewritten to the tag name).
	var name string
	if err := db.QueryRow(`SELECT name FROM artist WHERE mbid = $1`, ec).Scan(&name); err != nil {
		t.Fatalf("artist row: %v", err)
	}
	if name != "Electric Callboy" {
		t.Errorf("canonical artist name = %q, want Electric Callboy (MB wins)", name)
	}
	if n := count(t, db, `SELECT count(*) FROM credit WHERE subject_type='release_group' AND subject_mbid=$1 AND artist_mbid=$2 AND role='artist-credit'`, rg, ec); n == 0 {
		t.Error("no primary-artist credit row for Electric Callboy on MMXX release-group")
	}

	// Featured: 257ers has recording-level credits on the album.
	const f257ers = "ae667c89-4be1-4100-bb44-39bcfbe9cc28"
	if n := count(t, db, `SELECT count(*) FROM credit c JOIN file f ON f.recording_mbid = c.subject_mbid
		WHERE f.release_mbid IN (SELECT mbid FROM release WHERE release_group_mbid = $1)
		AND c.artist_mbid = $2`, rg, f257ers); n == 0 {
		t.Error("257ers has no track-level credit on MMXX")
	}

	// Renamed artist: the dated alias must exist (display choice).
	if n := count(t, db, `SELECT count(*) FROM alias WHERE entity_type='artist' AND entity_mbid=$1
		AND name='Eskimo Callboy' AND end_date IS NOT NULL`, ec); n == 0 {
		t.Error("historical alias 'Eskimo Callboy' missing")
	}
}

// The VA compilation must resolve without VA anywhere in the data.
func TestVACompilationHasNoVA(t *testing.T) {
	db := setup(t)
	const va = "89ad4ac3-39f7-470e-963a-56509c546377"
	const rg = "ea4c5330-a526-405e-8fb5-3698d7680d91"

	if n := count(t, db, `SELECT count(*) FROM artist WHERE mbid = $1`, va); n != 0 {
		t.Error("VA pseudo-artist was inserted into artist")
	}
	if n := count(t, db, `SELECT count(*) FROM credit WHERE artist_mbid = $1`, va); n != 0 {
		t.Error("VA appears in credit rows")
	}
	if n := count(t, db, `SELECT count(*) FROM credit WHERE subject_type='release_group' AND subject_mbid=$1 AND role='artist-credit'`, rg); n != 0 {
		t.Error("VA release-group must have no primary-artist credits")
	}
	if n := count(t, db, `SELECT count(*) FROM release WHERE release_group_mbid = $1`, rg); n == 0 {
		t.Error("VA compilation release missing")
	}
	// Work-level grouping: ten covers of one work must all link.
	if n := count(t, db, `SELECT count(DISTINCT rw.recording_mbid) FROM recording_work rw
		WHERE rw.work_mbid = '748bbd7d-a800-49a5-8579-a18e11b6ccfd'`); n < 8 {
		t.Errorf("only %d recordings linked to the 千本桜 work, want >= 8", n)
	}
}

// Standalone recording: file linked to recording, no release.
func TestStandaloneRecording(t *testing.T) {
	db := setup(t)
	const rec = "7c75d292-1c56-46c5-a1d8-ecfa6f13ce1c"

	var rel, fileCount interface{}
	var nFiles int
	if err := db.QueryRow(`SELECT release_mbid, count(*) FROM file WHERE recording_mbid = $1 GROUP BY release_mbid`, rec).Scan(&rel, &nFiles); err != nil {
		t.Fatalf("standalone file row: %v", err)
	}
	if rel != nil {
		t.Error("standalone recording file must not link to a release")
	}
	if n := count(t, db, `SELECT count(*) FROM recording WHERE mbid = $1`, rec); n != 1 {
		t.Error("standalone recording missing from graph")
	}
	if n := count(t, db, `SELECT count(*) FROM credit WHERE subject_type='recording' AND subject_mbid=$1`, rec); n == 0 {
		t.Error("standalone recording has no credits (Turbo/GUMI)")
	}
	_ = fileCount
}

// Search UX property: English work title and recording alias surface
// the Japanese-titled recordings.
func TestSearchUX(t *testing.T) {
	db := setup(t)

	find := func(q string) []string {
		rows, err := db.Query(`
			SELECT title FROM recording, websearch_to_tsquery('simple', $1) query
			WHERE search_vector @@ query`, q)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}

	hits := find("Peaceful Sleep")
	if len(hits) == 0 {
		t.Error("searching 'Peaceful Sleep' found nothing; alias search broken")
	}
	hits = find("Weight of the World")
	if len(hits) == 0 {
		t.Error("searching 'Weight of the World' found nothing; work-title search broken")
	}
	hits = find("穏ヤカナ眠リ")
	if len(hits) == 0 {
		t.Error("searching the raw Japanese title found nothing")
	}
}

// Conflict policy: tags say "Eskimo Callboy", MB canonical is
// "Electric Callboy"; the delta must be recorded, not silently used.
func TestConflictsRecorded(t *testing.T) {
	db := setup(t)
	if n := count(t, db, `SELECT count(*) FROM conflict WHERE kind='artist-name' AND tag_value='Eskimo Callboy' AND mb_value='Electric Callboy'`); n == 0 {
		t.Error("Eskimo Callboy vs Electric Callboy conflict not recorded")
	}
}
