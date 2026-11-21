// Package resolver links scanned files into the library graph.
//
// Input: file rows carrying MusicBrainz tags (raw tag evidence from
// the scanner). It fetches the referenced entities, persists the
// graph, and records tag-versus-MusicBrainz conflicts for the UI.
// MusicBrainz wins in the graph; tags remain evidence.
//
// Enforced decisions (docs/scope.md, docs/data-model.md):
//   - The "Various Artists" pseudo-artist never enters the data.
//   - The "[standalone recordings]" tag pseudo-album is a marker, not
//     an album; standalone files get no release link.
//   - Credit roles are raw MusicBrainz relationship types, verbatim;
//     the entity's own ordered credit is stored with role
//     "artist-credit".
package resolver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"ihy/internal/musicbrainz"
)

// Fetcher is the subset of the MusicBrainz client the resolver uses
// (same interface, asserted for compile-time checks per convention).
type Fetcher = musicbrainz.Fetcher

// standaloneMarker is Picard's pseudo-album for recordings that are
// on no release at all.
const standaloneMarker = "[standalone recordings]"

// vaArtistMBID is MusicBrainz's "Various Artists" pseudo-artist.
const vaArtistMBID = musicbrainz.MBID("89ad4ac3-39f7-470e-963a-56509c546377")

// fileTags is one scanned file and its raw tags.
type fileTags struct {
	id   int64
	tags map[string][]string
}

// Result summarizes one resolve pass.
type Result struct {
	Files      int
	Artists    int
	Groups     int
	Releases   int
	Recordings int
	Works      int
	Credits    int
	Aliases    int
	Conflicts  int
	NotFound   int // stale or missing MBIDs, skipped
}

// Resolver links files into the graph.
type Resolver struct {
	db      *sql.DB
	fetcher Fetcher
}

func New(db *sql.DB, fetcher Fetcher) *Resolver {
	return &Resolver{db: db, fetcher: fetcher}
}

// resolve run state: wanted entities and fetched data.
type run struct {
	r        *Resolver
	res      Result
	wanted   map[string][]musicbrainz.MBID
	confRows []conflictRow
}

type conflictRow struct {
	fileID   int64
	kind     string
	tagValue string
	mbValue  string
}

// Resolve runs one full pass. Idempotent: every write is an upsert.
func (r *Resolver) Resolve(ctx context.Context) (Result, error) {
	st := &run{r: r, wanted: map[string][]musicbrainz.MBID{}}

	files, err := r.loadFiles(ctx)
	if err != nil {
		return st.res, err
	}
	st.res.Files = len(files)
	st.collect(files)

	if err := st.fetch(ctx); err != nil {
		return st.res, err
	}
	if err := st.linkFiles(ctx, files); err != nil {
		return st.res, err
	}
	if err := st.persistConflicts(ctx); err != nil {
		return st.res, err
	}
	if err := st.buildSearchVectors(ctx); err != nil {
		return st.res, err
	}
	return st.res, nil
}

// loadFiles reads every file row with its raw tags.
func (r *Resolver) loadFiles(ctx context.Context) ([]fileTags, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, tags FROM file WHERE tags != '{}'::jsonb`)
	if err != nil {
		return nil, fmt.Errorf("load files: %w", err)
	}
	defer rows.Close()
	var out []fileTags
	for rows.Next() {
		var f fileTags
		var raw []byte
		if err := rows.Scan(&f.id, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &f.tags); err != nil {
			return nil, fmt.Errorf("decode tags: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// collect gathers the MBIDs to fetch. VA is excluded everywhere; the
// standalone marker suppresses release-level linking.
func (st *run) collect(files []fileTags) {
	add := func(entity string, ids ...string) {
		for _, id := range ids {
			m := musicbrainz.MBID(strings.TrimSpace(id))
			if m == "" || m == vaArtistMBID {
				continue
			}
			if !containsMBID(st.wanted[entity], m) {
				st.wanted[entity] = append(st.wanted[entity], m)
			}
		}
	}
	for _, f := range files {
		standalone := first(f.tags, "ALBUM") == standaloneMarker
		add("recording", first(f.tags, "MUSICBRAINZ_TRACKID"))
		if !standalone {
			add("release", first(f.tags, "MUSICBRAINZ_ALBUMID"))
			add("release-group", first(f.tags, "MUSICBRAINZ_RELEASEGROUPID"))
		}
		add("artist",
			first(f.tags, "MUSICBRAINZ_ALBUMARTISTID"),
			first(f.tags, "MUSICBRAINZ_ARTISTID"),
			first(f.tags, "MUSICBRAINZ_COMPOSERID"))
		add("work", first(f.tags, "MUSICBRAINZ_WORKID"))
	}
}

func first(tags map[string][]string, key string) string {
	if vals, ok := tags[key]; ok && len(vals) > 0 {
		return strings.TrimSpace(vals[0])
	}
	return ""
}

func containsMBID(list []musicbrainz.MBID, m musicbrainz.MBID) bool {
	for _, e := range list {
		if e == m {
			return true
		}
	}
	return false
}
