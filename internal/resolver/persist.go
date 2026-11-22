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
			st.markDone()
			continue
		}
		if err != nil {
			return fmt.Errorf("recording %s: %w", m, err)
		}
		st.markDone()
		recordings = append(recordings, rec)
		st.wantedArtists(rec.Relations)
		st.wantedWorks(rec.Relations)
	}
	for _, m := range snapshot(st.wanted["work"]) {
		w, err := st.r.fetcher.Work(ctx, m)
		if notFound(err) {
			st.res.NotFound++
			st.markDone()
			continue
		}
		if err != nil {
			return fmt.Errorf("work %s: %w", m, err)
		}
		st.markDone()
		works = append(works, w)
		st.wantedArtists(w.Relations)
	}
	for _, m := range snapshot(st.wanted["artist"]) {
		a, err := st.r.fetcher.Artist(ctx, m)
		if notFound(err) {
			st.res.NotFound++
			st.markDone()
			continue
		}
		if err != nil {
			return fmt.Errorf("artist %s: %w", m, err)
		}
		st.markDone()
		if err := st.r.upsertArtist(ctx, a); err != nil {
			return err
		}
		st.res.Artists++
		n, err := st.r.putAliases(ctx, "artist", a.MBID, artistAliases(a))
		if err != nil {
			return err
		}
		st.res.Aliases += n
	}
	for _, m := range snapshot(st.wanted["release-group"]) {
		rg, err := st.r.fetcher.ReleaseGroup(ctx, m)
		if notFound(err) {
			st.res.NotFound++
			st.markDone()
			continue
		}
		if err != nil {
			return fmt.Errorf("release-group %s: %w", m, err)
		}
		st.markDone()
		if err := st.r.upsertReleaseGroup(ctx, rg); err != nil {
			return err
		}
		st.res.Groups++
		n, err := st.r.putCredits(ctx, "release_group", rg.MBID, creditRows("artist-credit", rg.ArtistCredit))
		if err != nil {
			return err
		}
		st.res.Credits += n
		n, err = st.r.putAliases(ctx, "release_group", rg.MBID, mbAliases(rg.Aliases))
		if err != nil {
			return err
		}
		st.res.Aliases += n
	}
	for _, m := range snapshot(st.wanted["release"]) {
		rel, err := st.r.fetcher.Release(ctx, m)
		if notFound(err) {
			st.res.NotFound++
			st.markDone()
			continue
		}
		if err != nil {
			return fmt.Errorf("release %s: %w", m, err)
		}
		st.markDone()
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
		n, err = st.r.putAliases(ctx, "work", w.MBID, mbAliases(w.Aliases))
		if err != nil {
			return err
		}
		st.res.Aliases += n
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
		n, err = st.r.putAliases(ctx, "recording", rec.MBID, mbAliases(rec.Aliases))
		if err != nil {
			return err
		}
		st.res.Aliases += n
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

// putAliases writes alias rows in one statement and returns how many
// were stored. Empty strings become NULL, matching the per-row
// behavior; the unique index keys on coalesce(locale, ”) so
// NULL-locale aliases dedupe like any other.
func (r *Resolver) putAliases(ctx context.Context, entityType string, mbid musicbrainz.MBID, aliases []musicbrainz.Alias) (int, error) {
	rows := make([]musicbrainz.Alias, 0, len(aliases))
	for _, al := range aliases {
		if al.Name != "" {
			rows = append(rows, al)
		}
	}
	if len(rows) == 0 {
		return 0, nil
	}
	names := make([]string, len(rows))
	sortNames := make([]string, len(rows))
	locales := make([]string, len(rows))
	types := make([]string, len(rows))
	primaries := make([]bool, len(rows))
	begins := make([]string, len(rows))
	ends := make([]string, len(rows))
	for i, al := range rows {
		names[i] = al.Name
		sortNames[i] = al.SortName
		locales[i] = al.Locale
		types[i] = al.Type
		primaries[i] = al.Primary
		begins[i] = al.Begin
		ends[i] = al.End
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO alias (entity_type, entity_mbid, name, sort_name, locale, type, is_primary, begin_date, end_date)
		SELECT $1, $2::uuid, c.name, nullif(c.sort_name, ''), nullif(c.locale, ''),
		       nullif(c.type, ''), c.is_primary, nullif(c.begin_date, ''), nullif(c.end_date, '')
		FROM unnest($3::text[], $4::text[], $5::text[], $6::text[], $7::bool[], $8::text[], $9::text[])
		     AS c(name, sort_name, locale, type, is_primary, begin_date, end_date)
		ON CONFLICT (entity_type, entity_mbid, name, coalesce(locale, '')) DO UPDATE SET
			sort_name = EXCLUDED.sort_name, type = EXCLUDED.type,
			is_primary = EXCLUDED.is_primary,
			begin_date = EXCLUDED.begin_date, end_date = EXCLUDED.end_date`,
		entityType, mbid, names, sortNames, locales, types, primaries, begins, ends)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
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

// putRecordingCredits stores the recording's credits and links its
// performance works in two statements. The work join drops stale work
// MBIDs whose lookup failed.
func (r *Resolver) putRecordingCredits(ctx context.Context, rec musicbrainz.Recording) (int, error) {
	n, err := r.putCredits(ctx, "recording", rec.MBID,
		append(creditRows("artist-credit", rec.ArtistCredit), relCreditRows(rec.Relations)...))
	if err != nil {
		return n, err
	}
	var workIDs []string
	for _, rel := range rec.Relations {
		if rel.Type == "performance" && rel.Work != nil && rel.Work.MBID != "" {
			workIDs = append(workIDs, string(rel.Work.MBID))
		}
	}
	if len(workIDs) > 0 {
		_, err = r.db.ExecContext(ctx, `
			INSERT INTO recording_work (recording_mbid, work_mbid)
			SELECT $1, w.mbid FROM unnest($2::text[]) AS m(mbid)
			JOIN work w ON w.mbid = m.mbid::uuid
			ON CONFLICT DO NOTHING`, rec.MBID, workIDs)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// putWorkCredits stores the work's artist relationships (composer,
// lyricist and similar writing credits).
func (r *Resolver) putWorkCredits(ctx context.Context, w musicbrainz.Work) (int, error) {
	return r.putCredits(ctx, "work", w.MBID, relCreditRows(w.Relations))
}

// putCredits writes credit rows in one statement. Artists missing
// from the artist table (stale MBIDs whose lookup failed) are
// filtered server-side, preserving the per-row behavior.
func (r *Resolver) putCredits(ctx context.Context, subjectType string, subject musicbrainz.MBID, rows []creditInput) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	artists := make([]string, len(rows))
	roles := make([]string, len(rows))
	details := make([]string, len(rows))
	positions := make([]int32, len(rows))
	displayNames := make([]string, len(rows))
	joins := make([]string, len(rows))
	for i, cr := range rows {
		artists[i] = string(cr.artist)
		roles[i] = cr.role
		details[i] = cr.details
		positions[i] = int32(cr.position)
		displayNames[i] = cr.displayName
		joins[i] = cr.joinPhrase
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO credit (subject_type, subject_mbid, artist_mbid, role, details, position, display_name, joinphrase)
		SELECT $1, $2::uuid, c.artist_mbid::uuid, c.role, c.details, c.position, c.display_name, c.joinphrase
		FROM unnest($3::text[], $4::text[], $5::text[], $6::int[], $7::text[], $8::text[])
		     AS c(artist_mbid, role, details, position, display_name, joinphrase)
		WHERE c.artist_mbid ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
		  AND EXISTS (SELECT 1 FROM artist a WHERE a.mbid = c.artist_mbid::uuid)
		ON CONFLICT (subject_type, subject_mbid, artist_mbid, role, position) DO UPDATE SET
			details = EXCLUDED.details, display_name = EXCLUDED.display_name,
			joinphrase = EXCLUDED.joinphrase`,
		subjectType, subject, artists, roles, details, positions, displayNames, joins)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// linkFiles sets file.release_mbid / file.recording_mbid from tags
// in one statement: standalone-marker files keep NULL release, and
// missing tag MBIDs become NULL exactly as the per-row version did.
func (st *run) linkFiles(ctx context.Context, files []fileTags) error {
	if len(files) == 0 {
		return nil
	}
	ids := make([]int64, len(files))
	releases := make([]string, len(files))
	recordings := make([]string, len(files))
	for i, f := range files {
		ids[i] = f.id
		standalone := first(f.tags, "ALBUM") == standaloneMarker
		if !standalone {
			releases[i] = first(f.tags, "MUSICBRAINZ_ALBUMID")
		}
		recordings[i] = first(f.tags, "MUSICBRAINZ_TRACKID")
	}
	if _, err := st.r.db.ExecContext(ctx, `
		UPDATE file SET
			release_mbid = nullif(u.release, '')::uuid,
			recording_mbid = nullif(u.recording, '')::uuid
		FROM unnest($1::bigint[], $2::text[], $3::text[]) AS u(id, release, recording)
		WHERE file.id = u.id`, ids, releases, recordings); err != nil {
		return fmt.Errorf("link files: %w", err)
	}
	return nil
}

// persistConflicts rebuilds the conflict table for the run in one
// pass: tag values are compared against canonical MusicBrainz names
// set-based, per kind (MB wins; tag values are display choices, not
// deletions). The MBID regex guard keeps malformed tag UUIDs from
// aborting the cast the way the per-row queries skipped them.
func (st *run) persistConflicts(ctx context.Context) error {
	if _, err := st.r.db.ExecContext(ctx, `DELETE FROM conflict`); err != nil {
		return err
	}
	const mbidPattern = `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	const vaMBID = "89ad4ac3-39f7-470e-963a-56509c546377"
	// kind, tag tag key resolution, MB table+column, tag MBID key.
	kinds := []struct {
		kind    string
		tagExpr string // SQL for the tag value
		mbidKey string // file tag key holding the MB ID
		table   string
		column  string
	}{
		{"recording-title", "f.tags->'TITLE'->>0", "MUSICBRAINZ_TRACKID", "recording", "title"},
		{"artist-name", "coalesce(nullif(f.tags->'ARTIST'->>0,''), f.tags->'ALBUMARTISTS'->>0)", "MUSICBRAINZ_ARTISTID", "artist", "name"},
		{"artist-name", "coalesce(nullif(f.tags->'ARTIST'->>0,''), f.tags->'ALBUMARTISTS'->>0)", "MUSICBRAINZ_ALBUMARTISTID", "artist", "name"},
		{"release-title", "f.tags->'ALBUM'->>0", "MUSICBRAINZ_ALBUMID", "release", "title"},
		{"release-group-title", "f.tags->'ALBUM'->>0", "MUSICBRAINZ_RELEASEGROUPID", "release_group", "title"},
	}
	for _, k := range kinds {
		res, err := st.r.db.ExecContext(ctx, `
			INSERT INTO conflict (file_id, kind, tag_value, mb_value)
			SELECT DISTINCT f.id, $1, nullif(tag.tag_value, ''), mb.`+k.column+`
			FROM file f
			JOIN `+k.table+` mb ON mb.mbid = CASE
				WHEN f.tags->'`+k.mbidKey+`'->>0 ~* $2 THEN (f.tags->'`+k.mbidKey+`'->>0)::uuid
			END
			CROSS JOIN LATERAL (SELECT `+k.tagExpr+` AS tag_value) tag
			WHERE f.tags->'`+k.mbidKey+`'->>0 ~* $2
			  AND f.tags->'`+k.mbidKey+`'->>0 <> $3
			  AND tag.tag_value IS NOT NULL
			  AND tag.tag_value IS DISTINCT FROM mb.`+k.column+`
			ON CONFLICT (file_id, kind, tag_value, mb_value) DO NOTHING`,
			k.kind, mbidPattern, vaMBID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		st.res.Conflicts += int(n)
	}
	return nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
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
