// Prune half of the resolver: the graph is enrichment of the files,
// not a MusicBrainz mirror. Entities exist because a file references
// them; when the last file goes, the entity and its dependents go.
package resolver

import (
	"context"
	"fmt"
)

// pruned counts what one prune pass removed.
type pruned struct {
	Tracks      int
	Works       int
	Labels      int
	Releases    int
	Recordings  int
	Groups      int
	Artists     int
	Credits     int
	Aliases     int
	RecWorks    int
	ReleaseLbls int
}

// prune deletes every graph entity no longer referenced by any file,
// plus dependents (credits, aliases, placement and link rows) whose
// subject died. Release rows are file-backed by definition (they were
// only created from file tags), so "release without files" cannot
// survive a resolve pass.
//
// One transaction: this is one logical unit, all statements are fast
// set-based deletes against local tables.
func (st *run) prune(ctx context.Context) (pruned, error) {
	var out pruned
	tx, err := st.r.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	exec := func(query string, args ...any) (int, error) {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		return int(n), err
	}

	// Placement and link rows whose subject lost its files. A track
	// exists for a release+recording placement; if either side has no
	// files left, the placement is meaningless.
	if out.Tracks, err = exec(`
		DELETE FROM track t
		WHERE NOT EXISTS (SELECT 1 FROM file f WHERE f.release_mbid = t.release_mbid)
		   OR NOT EXISTS (SELECT 1 FROM file f WHERE f.recording_mbid = t.recording_mbid)`); err != nil {
		return out, fmt.Errorf("prune tracks: %w", err)
	}
	// Work links die with their recording; works backed only by a
	// file's MUSICBRAINZ_WORKID tag survive (their recording may
	// legitimately carry no performance relation).
	if out.RecWorks, err = exec(`
		DELETE FROM recording_work rw
		WHERE NOT EXISTS (SELECT 1 FROM file f WHERE f.recording_mbid = rw.recording_mbid)`); err != nil {
		return out, fmt.Errorf("prune recording_work: %w", err)
	}
	if out.ReleaseLbls, err = exec(`
		DELETE FROM release_label rl
		WHERE NOT EXISTS (SELECT 1 FROM file f WHERE f.release_mbid = rl.release_mbid)`); err != nil {
		return out, fmt.Errorf("prune release_label: %w", err)
	}
	if out.Labels, err = exec(`
		DELETE FROM label l
		WHERE NOT EXISTS (SELECT 1 FROM release_label rl WHERE rl.label_mbid = l.mbid)`); err != nil {
		return out, fmt.Errorf("prune labels: %w", err)
	}

	// The entity rows themselves, children before parents.
	if out.Releases, err = exec(`
		DELETE FROM release r
		WHERE NOT EXISTS (SELECT 1 FROM file f WHERE f.release_mbid = r.mbid)`); err != nil {
		return out, fmt.Errorf("prune releases: %w", err)
	}
	if out.Recordings, err = exec(`
		DELETE FROM recording rec
		WHERE NOT EXISTS (SELECT 1 FROM file f WHERE f.recording_mbid = rec.mbid)`); err != nil {
		return out, fmt.Errorf("prune recordings: %w", err)
	}
	if out.Groups, err = exec(`
		DELETE FROM release_group rg
		WHERE NOT EXISTS (SELECT 1 FROM release r WHERE r.release_group_mbid = rg.mbid)`); err != nil {
		return out, fmt.Errorf("prune release-groups: %w", err)
	}
	if out.Works, err = exec(`
		DELETE FROM work w
		WHERE NOT EXISTS (SELECT 1 FROM recording_work rw WHERE rw.work_mbid = w.mbid)
		  AND NOT EXISTS (SELECT 1 FROM file f WHERE f.tags->'MUSICBRAINZ_WORKID'->>0 = w.mbid::text)`); err != nil {
		return out, fmt.Errorf("prune works: %w", err)
	}

	// Polymorphic dependents: credit and alias key their subject by
	// (type, mbid) without a FK, so they are cleaned after the subject
	// rows are gone. Artists die last, by having no credits left (the
	// credit FK to artist enforces the same rule from below).
	if out.Credits, err = exec(`
		DELETE FROM credit c
		WHERE (c.subject_type = 'artist'         AND NOT EXISTS (SELECT 1 FROM artist a        WHERE a.mbid = c.subject_mbid::uuid))
		   OR (c.subject_type = 'release_group'  AND NOT EXISTS (SELECT 1 FROM release_group g WHERE g.mbid = c.subject_mbid::uuid))
		   OR (c.subject_type = 'release'        AND NOT EXISTS (SELECT 1 FROM release r       WHERE r.mbid = c.subject_mbid::uuid))
		   OR (c.subject_type = 'recording'      AND NOT EXISTS (SELECT 1 FROM recording rec   WHERE rec.mbid = c.subject_mbid::uuid))
		   OR (c.subject_type = 'work'           AND NOT EXISTS (SELECT 1 FROM work w          WHERE w.mbid = c.subject_mbid::uuid))`); err != nil {
		return out, fmt.Errorf("prune credits: %w", err)
	}
	if out.Aliases, err = exec(`
		DELETE FROM alias a
		WHERE (a.entity_type = 'artist'          AND NOT EXISTS (SELECT 1 FROM artist a2       WHERE a2.mbid = a.entity_mbid::uuid))
		   OR (a.entity_type = 'release_group'   AND NOT EXISTS (SELECT 1 FROM release_group g WHERE g.mbid = a.entity_mbid::uuid))
		   OR (a.entity_type = 'release'         AND NOT EXISTS (SELECT 1 FROM release r       WHERE r.mbid = a.entity_mbid::uuid))
		   OR (a.entity_type = 'recording'       AND NOT EXISTS (SELECT 1 FROM recording rec   WHERE rec.mbid = a.entity_mbid::uuid))
		   OR (a.entity_type = 'work'            AND NOT EXISTS (SELECT 1 FROM work w          WHERE w.mbid = a.entity_mbid::uuid))`); err != nil {
		return out, fmt.Errorf("prune aliases: %w", err)
	}
	if out.Artists, err = exec(`
		DELETE FROM artist a
		WHERE NOT EXISTS (SELECT 1 FROM credit c WHERE c.artist_mbid = a.mbid)`); err != nil {
		return out, fmt.Errorf("prune artists: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
