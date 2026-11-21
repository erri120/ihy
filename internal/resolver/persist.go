// Fetch and persistence half of the resolver: entities, credits,
// aliases, conflicts, file links and search vectors.
package resolver

import (
	"context"
	"fmt"
	"strings"

	"ihy/internal/musicbrainz"
)

// fetch pulls every wanted entity and persists it, in dependency
// order: recordings and works first (their relationships reveal
// additional artists that only exist inside payloads, e.g. performers
// tagged without IDs), then artists, then release-groups and
// releases, then all credits. A stale MBID (ErrNotFound) is skipped
// and counted; anything else aborts the run.
func (st *run) fetch(ctx context.Context) error {
	var recordings []musicbrainz.Recording
	var works []musicbrainz.Work

	for _, m := range snapshot(st.wanted["recording"]) {
		rec, err := st.r.fetcher.Recording(ctx, m)
		if notFound(err) {
			st.res.NotFound++
			continue
		}
		if err != nil {
			return fmt.Errorf("recording %s: %w", m, err)
		}
		recordings = append(recordings, rec)
		st.wantedArtists(rec.Relations)
		st.wantedWorks(rec.Relations)
	}
	for _, m := range snapshot(st.wanted["work"]) {
		w, err := st.r.fetcher.Work(ctx, m)
		if notFound(err) {
			st.res.NotFound++
			continue
		}
		if err != nil {
			return fmt.Errorf("work %s: %w", m, err)
		}
		works = append(works, w)
		st.wantedArtists(w.Relations)
	}
	for _, m := range snapshot(st.wanted["artist"]) {
		a, err := st.r.fetcher.Artist(ctx, m)
		if notFound(err) {
			st.res.NotFound++
			continue
		}
		if err != nil {
			return fmt.Errorf("artist %s: %w", m, err)
		}
		if err := st.r.upsertArtist(ctx, a); err != nil {
			return err
		}
		st.res.Artists++
		st.res.Aliases += st.r.putAliases(ctx, "artist", a.MBID, artistAliases(a))
	}
	for _, m := range snapshot(st.wanted["release-group"]) {
		rg, err := st.r.fetcher.ReleaseGroup(ctx, m)
		if notFound(err) {
			st.res.NotFound++
			continue
		}
		if err != nil {
			return fmt.Errorf("release-group %s: %w", m, err)
		}
		if err := st.r.upsertReleaseGroup(ctx, rg); err != nil {
			return err
		}
		st.res.Groups++
		n, err := st.r.putCredits(ctx, "release_group", rg.MBID, creditRows("artist-credit", rg.ArtistCredit))
		if err != nil {
			return err
		}
		st.res.Credits += n
		st.res.Aliases += st.r.putAliases(ctx, "release_group", rg.MBID, mbAliases(rg.Aliases))
	}
	for _, m := range snapshot(st.wanted["release"]) {
		rel, err := st.r.fetcher.Release(ctx, m)
		if notFound(err) {
			st.res.NotFound++
			continue
		}
		if err != nil {
			return fmt.Errorf("release %s: %w", m, err)
		}
		if err := st.r.upsertRelease(ctx, rel); err != nil {
			return err
		}
		st.res.Releases++
		n, err := st.r.putCredits(ctx, "release", rel.MBID, creditRows("artist-credit", rel.ArtistCredit))
		if err != nil {
			return err
		}
		st.res.Credits += n
	}
	// Works must be persisted before recordings: the recording->work
	// links reference the work rows.
	for _, w := range works {
		if err := st.r.upsertWork(ctx, w); err != nil {
			return err
		}
		st.res.Works++
		n, err := st.r.putWorkCredits(ctx, w)
		if err != nil {
			return err
		}
		st.res.Credits += n
		st.res.Aliases += st.r.putAliases(ctx, "work", w.MBID, mbAliases(w.Aliases))
	}
	for _, rec := range recordings {
		if err := st.r.upsertRecording(ctx, rec); err != nil {
			return err
		}
		st.res.Recordings++
		n, err := st.r.putRecordingCredits(ctx, rec)
		if err != nil {
			return err
		}
		st.res.Credits += n
		st.res.Aliases += st.r.putAliases(ctx, "recording", rec.MBID, mbAliases(rec.Aliases))
	}
	return nil
}

// snapshot returns a copy of the wanted list so fetch loops can grow
// it safely.
func snapshot(list []musicbrainz.MBID) []musicbrainz.MBID {
	out := make([]musicbrainz.MBID, len(list))
	copy(out, list)
	return out
}

// wantedArtists adds relationship artists to the fetch set; VA is
// skipped everywhere.
func (st *run) wantedArtists(rels []musicbrainz.Relation) {
	for _, rel := range rels {
		if rel.TargetType != "artist" || rel.Artist == nil || rel.Artist.MBID == "" {
			continue
		}
		if rel.Artist.MBID == vaArtistMBID {
			continue
		}
		if !containsMBID(st.wanted["artist"], rel.Artist.MBID) {
			st.wanted["artist"] = append(st.wanted["artist"], rel.Artist.MBID)
		}
	}
}

// wantedWorks adds work targets of performance relationships to the
// fetch set (a recording's work may not be in the tags, but is needed
// for work-level grouping and search).
func (st *run) wantedWorks(rels []musicbrainz.Relation) {
	for _, rel := range rels {
		if rel.TargetType != "work" || rel.Work == nil || rel.Work.MBID == "" {
			continue
		}
		if !containsMBID(st.wanted["work"], rel.Work.MBID) {
			st.wanted["work"] = append(st.wanted["work"], rel.Work.MBID)
		}
	}
}

func notFound(err error) bool {
	return err == musicbrainz.ErrNotFound
}

// upsertArtist writes the artist row.
func (r *Resolver) upsertArtist(ctx context.Context, a musicbrainz.Artist) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO artist (mbid, name, sort_name, disambiguation, type, country, begin_date, end_date, ended)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (mbid) DO UPDATE SET
			name = EXCLUDED.name, sort_name = EXCLUDED.sort_name,
			disambiguation = EXCLUDED.disambiguation, type = EXCLUDED.type,
			country = EXCLUDED.country, begin_date = EXCLUDED.begin_date,
			end_date = EXCLUDED.end_date, ended = EXCLUDED.ended`,
		a.MBID, a.Name, a.SortName, a.Disambiguation, nullString(a.Type),
		nullString(a.Country), nullString(a.LifeSpan.Begin),
		nullString(a.LifeSpan.End), a.LifeSpan.Ended)
	return err
}

func (r *Resolver) upsertReleaseGroup(ctx context.Context, rg musicbrainz.ReleaseGroup) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO release_group (mbid, title, primary_type, secondary_types, first_release_date)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (mbid) DO UPDATE SET
			title = EXCLUDED.title, primary_type = EXCLUDED.primary_type,
			secondary_types = EXCLUDED.secondary_types,
			first_release_date = EXCLUDED.first_release_date`,
		rg.MBID, rg.Title, rg.PrimaryType, rg.SecondaryTypes, nullString(rg.FirstReleaseDate))
	return err
}

func (r *Resolver) upsertRelease(ctx context.Context, rel musicbrainz.Release) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO release (mbid, release_group_mbid, title, date, country, barcode)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (mbid) DO UPDATE SET
			release_group_mbid = EXCLUDED.release_group_mbid, title = EXCLUDED.title,
			date = EXCLUDED.date, country = EXCLUDED.country, barcode = EXCLUDED.barcode`,
		rel.MBID, rel.ReleaseGroup.MBID, rel.Title, nullString(rel.Date),
		nullString(rel.Country), nullString(rel.Barcode))
	if err != nil {
		return err
	}
	for _, li := range rel.LabelInfo {
		if li.Label == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO label (mbid, name) VALUES ($1, $2)
			ON CONFLICT (mbid) DO UPDATE SET name = EXCLUDED.name`,
			li.Label.MBID, li.Label.Name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO release_label (release_mbid, label_mbid, catalog_number)
			VALUES ($1, $2, $3)
			ON CONFLICT (release_mbid, label_mbid, catalog_number) DO NOTHING`,
			rel.MBID, li.Label.MBID, nullString(li.CatalogNumber)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Resolver) upsertRecording(ctx context.Context, rec musicbrainz.Recording) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO recording (mbid, title, length_ms, disambiguation, first_release_date)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (mbid) DO UPDATE SET
			title = EXCLUDED.title, length_ms = EXCLUDED.length_ms,
			disambiguation = EXCLUDED.disambiguation,
			first_release_date = EXCLUDED.first_release_date`,
		rec.MBID, rec.Title, rec.LengthMS, rec.Disambiguation,
		nullString(rec.FirstReleaseDate))
	return err
}

func (r *Resolver) upsertWork(ctx context.Context, w musicbrainz.Work) error {
	var iswc any
	if len(w.ISWCs) > 0 {
		iswc = w.ISWCs[0]
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO work (mbid, title, type, iswc)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (mbid) DO UPDATE SET
			title = EXCLUDED.title, type = EXCLUDED.type, iswc = EXCLUDED.iswc`,
		w.MBID, w.Title, nullString(w.Type), iswc)
	return err
}

// putAliases writes alias rows and returns how many were stored.
func (r *Resolver) putAliases(ctx context.Context, entityType string, mbid musicbrainz.MBID, aliases []musicbrainz.Alias) int {
	n := 0
	for _, al := range aliases {
		if al.Name == "" {
			continue
		}
		_, err := r.db.ExecContext(ctx, `
			INSERT INTO alias (entity_type, entity_mbid, name, sort_name, locale, type, is_primary, begin_date, end_date)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (entity_type, entity_mbid, name, locale) DO UPDATE SET
				sort_name = EXCLUDED.sort_name, type = EXCLUDED.type,
				is_primary = EXCLUDED.is_primary,
				begin_date = EXCLUDED.begin_date, end_date = EXCLUDED.end_date`,
			entityType, mbid, al.Name, nullString(al.SortName), nullString(al.Locale),
			nullString(al.Type), al.Primary, nullString(al.Begin), nullString(al.End))
		if err == nil {
			n++
		}
	}
	return n
}

func artistAliases(a musicbrainz.Artist) []musicbrainz.Alias { return a.Aliases }
func mbAliases(src []musicbrainz.Alias) []musicbrainz.Alias  { return src }

// creditRows converts an ordered artist-credit into credit rows with
// role "artist-credit".
func creditRows(role string, entries []musicbrainz.ArtistCreditEntry) []creditInput {
	var out []creditInput
	for i, e := range entries {
		if e.Artist.MBID == "" || e.Artist.MBID == vaArtistMBID {
			continue
		}
		out = append(out, creditInput{
			artist: e.Artist.MBID, role: role, position: i,
			displayName: e.Name, joinPhrase: e.JoinPhrase,
		})
	}
	return out
}

type creditInput struct {
	artist      musicbrainz.MBID
	role        string
	position    int
	displayName string
	joinPhrase  string
	details     string
}

// relCreditRows converts recording/work artist relationships into
// credit rows: role is the raw MusicBrainz relationship type,
// details carries the attributes (e.g. "viola", "lead vocals").
func relCreditRows(rels []musicbrainz.Relation) []creditInput {
	var out []creditInput
	positions := map[string]int{}
	for _, rel := range rels {
		if rel.TargetType != "artist" || rel.Artist == nil {
			continue
		}
		if rel.Artist.MBID == vaArtistMBID {
			continue
		}
		role := rel.Type
		pos := positions[role]
		positions[role]++
		out = append(out, creditInput{
			artist: rel.Artist.MBID, role: role, position: pos,
			displayName: rel.Artist.Name,
			details:     strings.Join(rel.Attributes, ", "),
		})
	}
	return out
}

// putCredits writes credit rows; also links work relations when the
// payload is a recording (recording -> work performance links).
func (r *Resolver) putRecordingCredits(ctx context.Context, rec musicbrainz.Recording) (int, error) {
	n, err := r.putCredits(ctx, "recording", rec.MBID,
		append(creditRows("artist-credit", rec.ArtistCredit), relCreditRows(rec.Relations)...))
	if err != nil {
		return n, err
	}
	for _, rel := range rec.Relations {
		if rel.Type == "performance" && rel.Work != nil && rel.Work.MBID != "" {
			// Skip stale work MBIDs whose lookup failed.
			var exists bool
			if err := r.db.QueryRowContext(ctx,
				`SELECT EXISTS (SELECT 1 FROM work WHERE mbid = $1)`, rel.Work.MBID).Scan(&exists); err != nil {
				return n, err
			}
			if !exists {
				continue
			}
			if _, err := r.db.ExecContext(ctx, `
				INSERT INTO recording_work (recording_mbid, work_mbid)
				VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				rec.MBID, rel.Work.MBID); err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

// putWorkCredits stores the work's artist relationships (composer,
// lyricist and similar writing credits).
func (r *Resolver) putWorkCredits(ctx context.Context, w musicbrainz.Work) (int, error) {
	return r.putCredits(ctx, "work", w.MBID, relCreditRows(w.Relations))
}

func (r *Resolver) putCredits(ctx context.Context, subjectType string, subject musicbrainz.MBID, rows []creditInput) (int, error) {
	n := 0
	for _, cr := range rows {
		// A relationship can target a stale MBID whose artist lookup
		// failed; a credit without its artist row would violate the
		// FK, so drop it (the missing artist is counted elsewhere).
		var exists bool
		err := r.db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM artist WHERE mbid = $1)`, cr.artist).Scan(&exists)
		if err != nil {
			return n, err
		}
		if !exists {
			continue
		}
		_, err = r.db.ExecContext(ctx, `
			INSERT INTO credit (subject_type, subject_mbid, artist_mbid, role, details, position, display_name, joinphrase)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (subject_type, subject_mbid, artist_mbid, role, position) DO UPDATE SET
				details = EXCLUDED.details, display_name = EXCLUDED.display_name,
				joinphrase = EXCLUDED.joinphrase`,
			subjectType, subject, cr.artist, cr.role, cr.details, cr.position,
			cr.displayName, cr.joinPhrase)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// linkFiles sets file.release_mbid / file.recording_mbid from tags.
func (st *run) linkFiles(ctx context.Context, files []fileTags) error {
	for _, f := range files {
		standalone := first(f.tags, "ALBUM") == standaloneMarker
		var release any
		if !standalone {
			if v := first(f.tags, "MUSICBRAINZ_ALBUMID"); v != "" {
				release = musicbrainz.MBID(v)
			}
		}
		var recording any
		if v := first(f.tags, "MUSICBRAINZ_TRACKID"); v != "" {
			recording = musicbrainz.MBID(v)
		}
		if _, err := st.r.db.ExecContext(ctx, `
			UPDATE file SET release_mbid = $2, recording_mbid = $3 WHERE id = $1`,
			f.id, release, recording); err != nil {
			return fmt.Errorf("link file %d: %w", f.id, err)
		}
		st.recordConflicts(f)
	}
	return nil
}

// recordConflicts compares tag values against canonical MusicBrainz
// names for MBID-anchored entities (MB wins; tag value is recorded as
// a display choice, not deleted).
func (st *run) recordConflicts(f fileTags) {
	if v := first(f.tags, "MUSICBRAINZ_TRACKID"); v != "" {
		var mbTitle string
		err := st.r.db.QueryRow(`SELECT title FROM recording WHERE mbid = $1`,
			musicbrainz.MBID(v)).Scan(&mbTitle)
		if err == nil {
			if tag := first(f.tags, "TITLE"); tag != "" && tag != mbTitle {
				st.add(f.id, "recording-title", tag, mbTitle)
			}
		}
	}
	for key, kind := range map[string]string{
		"MUSICBRAINZ_ARTISTID":       "artist-name",
		"MUSICBRAINZ_ALBUMARTISTID":  "artist-name",
		"MUSICBRAINZ_ALBUMID":        "release-title",
		"MUSICBRAINZ_RELEASEGROUPID": "release-group-title",
	} {
		v := first(f.tags, key)
		if v == "" || v == string(vaArtistMBID) {
			continue
		}
		var table, column string
		switch kind {
		case "artist-name":
			table, column = "artist", "name"
		case "release-title":
			table, column = "release", "title"
		default:
			table, column = "release_group", "title"
		}
		var mbName string
		err := st.r.db.QueryRow(fmt.Sprintf(`SELECT %s FROM %s WHERE mbid = $1`, column, table),
			musicbrainz.MBID(v)).Scan(&mbName)
		if err == nil {
			tagValue := first(f.tags, tagKeyFor(kind, f.tags))
			if tagValue != "" && tagValue != mbName {
				st.add(f.id, kind, tagValue, mbName)
			}
		}
	}
}

// tagKeyFor picks the tag key holding the human name for a conflict
// kind (ARTIST/ALBUMARTIST/ALBUM). The tag value is compared against
// the canonical MB name of the MBID it was written with.
func tagKeyFor(kind string, tags map[string][]string) string {
	switch kind {
	case "artist-name":
		if _, ok := tags["ARTIST"]; ok {
			return "ARTIST"
		}
		return "ALBUMARTISTS"
	case "release-title":
		return "ALBUM"
	default:
		return "ALBUM"
	}
}

func (st *run) add(fileID int64, kind, tagValue, mbValue string) {
	for _, c := range st.confRows {
		if c.fileID == fileID && c.kind == kind && c.tagValue == tagValue && c.mbValue == mbValue {
			return
		}
	}
	st.confRows = append(st.confRows, conflictRow{fileID, kind, tagValue, mbValue})
}

// persistConflicts rebuilds the conflict table for the run.
func (st *run) persistConflicts(ctx context.Context) error {
	if _, err := st.r.db.ExecContext(ctx, `DELETE FROM conflict`); err != nil {
		return err
	}
	for _, c := range st.confRows {
		if _, err := st.r.db.ExecContext(ctx, `
			INSERT INTO conflict (file_id, kind, tag_value, mb_value)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (file_id, kind, tag_value, mb_value) DO NOTHING`,
			c.fileID, c.kind, c.tagValue, c.mbValue); err != nil {
			return err
		}
		st.res.Conflicts++
	}
	return nil
}

// buildSearchVectors writes the weighted search document per
// recording: title (A), recording aliases (B), linked work titles
// (C), work aliases (D).
func (st *run) buildSearchVectors(ctx context.Context) error {
	_, err := st.r.db.ExecContext(ctx, `
		UPDATE recording SET search_vector =
			setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
			setweight(to_tsvector('simple', coalesce((
				SELECT string_agg(a.name, ' ') FROM alias a
				WHERE a.entity_type = 'recording' AND a.entity_mbid = recording.mbid
			), '')), 'B') ||
			setweight(to_tsvector('simple', coalesce((
				SELECT string_agg(w.title, ' ') FROM recording_work rw
				JOIN work w ON w.mbid = rw.work_mbid
				WHERE rw.recording_mbid = recording.mbid
			), '')), 'C') ||
			setweight(to_tsvector('simple', coalesce((
				SELECT string_agg(a.name, ' ') FROM recording_work rw
				JOIN work w ON w.mbid = rw.work_mbid
				JOIN alias a ON a.entity_type = 'work' AND a.entity_mbid = w.mbid
				WHERE rw.recording_mbid = recording.mbid
			), '')), 'D')`)
	return err
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
