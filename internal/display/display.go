// Package display resolves per-user display names from the alias
// table. Shared by the JSON API and the web UI: the graph stores
// canonical names only, and this package computes what a given user
// sees (locale chain + name-history policy).
package display

import (
	"context"
	"database/sql"
)

// Pref is a user's display preference for one entity type.
type Pref struct {
	LocaleChain   []string
	HistoryPolicy string
}

// Alias is a stored alias for display resolution.
type Alias struct {
	Name    string
	Locale  string
	Primary bool
	Begin   string
	End     string
	Type    string
}

// PrefsFor loads a user's display preferences keyed by entity type.
func PrefsFor(ctx context.Context, db *sql.DB, userID int64) (map[string]Pref, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT entity_type, locale_chain, history_policy FROM display_pref WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Pref{}
	for rows.Next() {
		var entityType string
		var p Pref
		if err := rows.Scan(&entityType, &p.LocaleChain, &p.HistoryPolicy); err != nil {
			return nil, err
		}
		out[entityType] = p
	}
	return out, rows.Err()
}

// Aliases loads the aliases of one entity.
func Aliases(ctx context.Context, db *sql.DB, entityType, mbid string) ([]Alias, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name, coalesce(locale, ''), is_primary, coalesce(begin_date, ''),
			coalesce(end_date, ''), coalesce(type, '')
		FROM alias WHERE entity_type = $1 AND entity_mbid = $2`, entityType, mbid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alias
	for rows.Next() {
		var a Alias
		if err := rows.Scan(&a.Name, &a.Locale, &a.Primary, &a.Begin, &a.End, &a.Type); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Name resolves a display name: the primary alias for the first
// locale in the user's chain that has one, else the canonical name.
// The at_release policy is applied by callers that have a date anchor;
// everywhere else names are "current".
func Name(p Pref, canonical string, aliases []Alias) string {
	for _, locale := range p.LocaleChain {
		for _, a := range aliases {
			if a.Primary && a.Locale == locale {
				return a.Name
			}
		}
	}
	return canonical
}

// NameAtRelease implements the "at_release" policy: the alias whose
// date range covers the release date.
func NameAtRelease(aliases []Alias, releaseDate string) string {
	if releaseDate == "" {
		return ""
	}
	for _, a := range aliases {
		if a.Type != "" && a.Type != "Artist name" {
			continue
		}
		if a.Begin != "" && a.Begin > releaseDate[:min(len(a.Begin), len(releaseDate))] {
			continue
		}
		if a.End != "" && a.End < releaseDate[:min(len(a.End), len(releaseDate))] {
			continue
		}
		// Alias active at that date; prefer one with a date range.
		return a.Name
	}
	return ""
}
