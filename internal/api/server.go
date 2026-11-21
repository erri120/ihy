// The API server: implements the generated OpenAPI interface on top
// of Postgres, with per-user display names.
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"ihy/internal/api/gen"
	"ihy/internal/auth"
	"ihy/internal/display"
)

// Compile-time interface assertion, per project convention.
var _ gen.ServerInterface = (*Server)(nil)

// Server implements the generated API on top of the database.
type Server struct {
	db   *sql.DB
	auth *Auth
}

func NewServer(db *sql.DB) *Server {
	return &Server{db: db, auth: NewAuth(db)}
}

// ptr and uuidOf adapt strings to the generated optional/UUID types.
func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(n int) *int { return &n }

func uuidOf(mbid string) openapi_types.UUID {
	var u openapi_types.UUID
	_ = u.UnmarshalText([]byte(mbid))
	return u
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(gen.Error{Error: msg})
}

func writeOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// prefsFor, aliasesFor and the name-resolution rules live in
// internal/display, shared with the web UI.
func (s *Server) prefsFor(ctx context.Context, userID int64) (map[string]display.Pref, error) {
	return display.PrefsFor(ctx, s.db, userID)
}

func (s *Server) aliasesFor(ctx context.Context, entityType, mbid string) ([]display.Alias, error) {
	return display.Aliases(ctx, s.db, entityType, mbid)
}

func displayName(p display.Pref, canonical string, aliases []display.Alias) string {
	return display.Name(p, canonical, aliases)
}

// artistDisplayName applies the name-history policy for the artist's
// own display name. "current" keeps the canonical name (or its
// locale-preferring alias); "at_release" is only meaningful per album,
// so the artist page itself shows the current name.
func artistDisplayName(p display.Pref, canonical string, aliases []display.Alias) string {
	return displayName(p, canonical, aliases)
}

func (s *Server) userFromCtx(ctx context.Context) (auth.User, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return u, fmt.Errorf("authentication required")
	}
	return u, nil
}

// --- auth handlers: delegate to the hand-rolled implementations ---

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	s.auth.Login(w, r)
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	s.auth.Logout(w, r)
}

func (s *Server) ListTokens(w http.ResponseWriter, r *http.Request) {
	s.auth.ListTokens(w, r)
}

func (s *Server) CreateToken(w http.ResponseWriter, r *http.Request) {
	s.auth.CreateToken(w, r)
}

func (s *Server) RevokeToken(w http.ResponseWriter, r *http.Request, id int64) {
	s.auth.RevokeToken(w, r, id)
}

// --- display prefs ---

func (s *Server) GetDisplayPrefs(w http.ResponseWriter, r *http.Request) {
	user, err := s.userFromCtx(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	prefs, err := s.prefsFor(r.Context(), user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []gen.DisplayPref{}
	for entityType, p := range prefs {
		out = append(out, gen.DisplayPref{
			EntityType:    gen.DisplayPrefEntityType(entityType),
			LocaleChain:   p.LocaleChain,
			HistoryPolicy: gen.DisplayPrefHistoryPolicy(p.HistoryPolicy),
		})
	}
	writeOK(w, out)
}

func (s *Server) PutDisplayPrefs(w http.ResponseWriter, r *http.Request) {
	user, err := s.userFromCtx(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	var in []gen.DisplayPref
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	valid := map[string]bool{"artist": true, "release_group": true, "release": true, "recording": true, "work": true}
	for _, p := range in {
		if !valid[string(p.EntityType)] || p.HistoryPolicy != "current" && p.HistoryPolicy != "at_release" {
			writeErr(w, http.StatusBadRequest, "invalid entity_type or history_policy")
			return
		}
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM display_pref WHERE user_id = $1`, user.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, p := range in {
		if _, err := s.db.ExecContext(r.Context(), `
			INSERT INTO display_pref (user_id, entity_type, locale_chain, history_policy)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id, entity_type) DO UPDATE SET
				locale_chain = EXCLUDED.locale_chain, history_policy = EXCLUDED.history_policy`,
			user.ID, string(p.EntityType), p.LocaleChain, string(p.HistoryPolicy)); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.GetDisplayPrefs(w, r)
}

// --- search ---

func (s *Server) Search(w http.ResponseWriter, r *http.Request, params gen.SearchParams) {
	user, err := s.userFromCtx(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	prefs, err := s.prefsFor(r.Context(), user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := strings.TrimSpace(params.Q)
	if q == "" {
		writeErr(w, http.StatusBadRequest, "q must not be empty")
		return
	}

	hits := []gen.SearchHit{}
	addHit := func(hitType, mbid, canonical string, aliases []display.Alias) {
		name := displayName(prefs[hitType], canonical, aliases)
		hits = append(hits, gen.SearchHit{
			Type:          gen.SearchHitType(hitType),
			Mbid:          uuidOf(mbid),
			Name:          name,
			CanonicalName: ptr(canonical),
		})
	}

	// Recordings: full-text over title, aliases and work names.
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT mbid, title FROM recording, websearch_to_tsquery('simple', $1) query
		WHERE search_vector @@ query ORDER BY ts_rank_cd(search_vector, query) DESC LIMIT 20`, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var mbid, title string
		if err := rows.Scan(&mbid, &title); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		aliases, _ := s.aliasesFor(r.Context(), "recording", mbid)
		addHit("recording", mbid, title, aliases)
	}
	rows.Close()

	// Artists, release-groups, releases: name or alias match.
	for entityType, table := range map[string]string{
		"artist": "artist", "release_group": "release_group", "release": "release",
	} {
		nameCol := map[string]string{"artist": "name", "release_group": "title", "release": "title"}[entityType]
		rows, err := s.db.QueryContext(r.Context(), fmt.Sprintf(`
			SELECT t.mbid, t.%s FROM %s t
			WHERE t.%s ILIKE '%%' || $1 || '%%'
			   OR EXISTS (SELECT 1 FROM alias a WHERE a.entity_type = $2 AND a.entity_mbid = t.mbid
			              AND a.Name ILIKE '%%' || $1 || '%%')
			LIMIT 10`, nameCol, table, nameCol), q, entityType)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		for rows.Next() {
			var mbid, name string
			if err := rows.Scan(&mbid, &name); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			aliases, _ := s.aliasesFor(r.Context(), entityType, mbid)
			addHit(entityType, mbid, name, aliases)
		}
		rows.Close()
	}

	writeOK(w, hits)
}

// --- artist page ---

func (s *Server) GetArtist(w http.ResponseWriter, r *http.Request, mbid openapi_types.UUID) {
	user, err := s.userFromCtx(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	prefs, err := s.prefsFor(r.Context(), user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	artistPref := prefs["artist"]

	var name, sortName, disamb, typ, country, begin, end string
	err = s.db.QueryRowContext(r.Context(), `
		SELECT name, sort_name, coalesce(disambiguation, ''), coalesce(type, ''),
		       coalesce(country, ''), coalesce(begin_date, ''), coalesce(end_date, '')
		FROM artist WHERE mbid = $1`, mbid).
		Scan(&name, &sortName, &disamb, &typ, &country, &begin, &end)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "unknown artist")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	aliases, err := s.aliasesFor(r.Context(), "artist", mbid.String())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Primary albums: release-group level credits.
	type album struct {
		mbid, title, date string
		featuredTracks    int
	}
	var albums []album
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT rg.mbid, rg.title, coalesce(rg.first_release_date, '')
		FROM credit c JOIN release_group rg ON rg.mbid = c.subject_mbid
		WHERE c.subject_type = 'release_group' AND c.artist_mbid = $1 AND c.role = 'artist-credit'
		ORDER BY rg.first_release_date`, mbid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var a album
		if err := rows.Scan(&a.mbid, &a.title, &a.date); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		albums = append(albums, a)
	}
	rows.Close()
	primary := map[string]bool{}
	for _, a := range albums {
		primary[a.mbid] = true
	}

	// Featured albums: the artist appears only on tracks.
	rows, err = s.db.QueryContext(r.Context(), `
		SELECT rg.mbid, rg.title, coalesce(rg.first_release_date, ''), count(DISTINCT f.recording_mbid)
		FROM credit c
		JOIN file f ON f.recording_mbid = c.subject_mbid
		JOIN release rel ON rel.mbid = f.release_mbid
		JOIN release_group rg ON rg.mbid = rel.release_group_mbid
		WHERE c.subject_type = 'recording' AND c.artist_mbid = $1
		GROUP BY rg.mbid, rg.title, rg.first_release_date`, mbid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var a album
		if err := rows.Scan(&a.mbid, &a.title, &a.date, &a.featuredTracks); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !primary[a.mbid] {
			albums = append(albums, a)
		} else {
			// The artist has both an RG credit and track credits on
			// their own album; that is primary, not featured.
			for i := range albums {
				if albums[i].mbid == a.mbid {
					albums[i].featuredTracks = 0
				}
			}
		}
	}
	rows.Close()

	out := gen.Artist{
		Mbid:          mbid,
		Name:          artistDisplayName(artistPref, name, aliases),
		CanonicalName: ptr(name),
		SortName:      ptr(sortName),
		Type:          ptr(typ),
		Country:       ptr(country),
		Begin:         ptr(begin),
		End:           ptr(end),
		Aliases:       ptrToGenAliases(aliases),
		Albums:        []gen.AlbumAppearance{},
	}
	for _, a := range albums {
		rgAliases, _ := s.aliasesFor(r.Context(), "release_group", a.mbid)
		name := displayName(prefs["release_group"], a.title, rgAliases)
		rel := "featured"
		if primary[a.mbid] {
			rel = "primary"
		}
		appearance := gen.AlbumAppearance{
			Mbid:             uuidOf(a.mbid),
			Name:             name,
			CanonicalName:    ptr(a.title),
			FirstReleaseDate: ptr(a.date),
			Relationship:     gen.AlbumAppearanceRelationship(rel),
		}
		if rel == "featured" {
			appearance.FeaturedTracks = intPtr(a.featuredTracks)
		}
		// The at_release policy: show the name the artist used when
		// the album came out, if a dated alias covers it.
		if artistPref.HistoryPolicy == "at_release" && a.date != "" {
			if historical := display.NameAtRelease(aliases, a.date); historical != "" {
				appearance.Name = historical
			}
		}
		out.Albums = append(out.Albums, appearance)
	}
	writeOK(w, out)
}

// --- release-group page ---

func (s *Server) GetReleaseGroup(w http.ResponseWriter, r *http.Request, mbid openapi_types.UUID) {
	user, err := s.userFromCtx(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	prefs, err := s.prefsFor(r.Context(), user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var title, primaryType, firstRelease string
	var secondary []string
	err = s.db.QueryRowContext(r.Context(), `
		SELECT title, primary_type, coalesce(secondary_types, '{}'), coalesce(first_release_date, '')
		FROM release_group WHERE mbid = $1`, mbid).Scan(&title, &primaryType, &secondary, &firstRelease)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "unknown release-group")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	aliases, err := s.aliasesFor(r.Context(), "release_group", mbid.String())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	primaryArtists := []gen.NamedRef{}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT a.mbid, a.Name FROM credit c JOIN artist a ON a.mbid = c.artist_mbid
		WHERE c.subject_type = 'release_group' AND c.subject_mbid = $1 AND c.role = 'artist-credit'
		ORDER BY c.position`, mbid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var m, n string
		if err := rows.Scan(&m, &n); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		aliases, _ := s.aliasesFor(r.Context(), "artist", m)
		primaryArtists = append(primaryArtists, gen.NamedRef{
			Mbid: uuidOf(m), Name: displayName(prefs["artist"], n, aliases), CanonicalName: ptr(n),
		})
	}
	rows.Close()

	releases := []gen.NamedRef{}
	rows, err = s.db.QueryContext(r.Context(), `
		SELECT mbid, title FROM release WHERE release_group_mbid = $1 ORDER BY date`, mbid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var m, n string
		if err := rows.Scan(&m, &n); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		aliases, _ := s.aliasesFor(r.Context(), "release", m)
		releases = append(releases, gen.NamedRef{
			Mbid: uuidOf(m), Name: displayName(prefs["release"], n, aliases), CanonicalName: ptr(n),
		})
	}
	rows.Close()

	writeOK(w, gen.ReleaseGroupDetail{
		Mbid:             mbid,
		Name:             displayName(prefs["release_group"], title, aliases),
		CanonicalName:    ptr(title),
		PrimaryType:      ptr(primaryType),
		SecondaryTypes:   &secondary,
		FirstReleaseDate: ptr(firstRelease),
		PrimaryArtists:   primaryArtists,
		Releases:         releases,
	})
}

// --- recording page ---

func (s *Server) GetRecording(w http.ResponseWriter, r *http.Request, mbid openapi_types.UUID) {
	user, err := s.userFromCtx(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	prefs, err := s.prefsFor(r.Context(), user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var title string
	var lengthMS int
	var disamb string
	err = s.db.QueryRowContext(r.Context(), `
		SELECT title, coalesce(length_ms, 0), coalesce(disambiguation, '')
		FROM recording WHERE mbid = $1`, mbid).Scan(&title, &lengthMS, &disamb)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "unknown recording")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	aliases, err := s.aliasesFor(r.Context(), "recording", mbid.String())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	credits := []gen.Credit{}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT c.artist_mbid, a.Name, c.role, c.details, c.position, c.joinphrase
		FROM credit c JOIN artist a ON a.mbid = c.artist_mbid
		WHERE c.subject_type = 'recording' AND c.subject_mbid = $1
		ORDER BY c.role, c.position`, mbid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var am, an, role, details, join string
		var pos int
		if err := rows.Scan(&am, &an, &role, &details, &pos, &join); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		artistAliases, _ := s.aliasesFor(r.Context(), "artist", am)
		credits = append(credits, gen.Credit{
			ArtistMbid: uuidOf(am),
			ArtistName: displayName(prefs["artist"], an, artistAliases),
			Role:       role,
			Details:    ptr(details),
			Position:   pos,
			Joinphrase: ptr(join),
		})
	}
	rows.Close()

	var work *struct {
		CanonicalName *string            `json:"canonical_name,omitempty"`
		Composers     *[]string          `json:"composers,omitempty"`
		Mbid          openapi_types.UUID `json:"mbid"`
		Name          string             `json:"name"`
	}
	var workMbid, workTitle string
	err = s.db.QueryRowContext(r.Context(), `
		SELECT w.mbid, w.title FROM recording_work rw JOIN work w ON w.mbid = rw.work_mbid
		WHERE rw.recording_mbid = $1`, mbid).Scan(&workMbid, &workTitle)
	if err == nil {
		workAliases, _ := s.aliasesFor(r.Context(), "work", workMbid)
		composers := []string{}
		rows, err := s.db.QueryContext(r.Context(), `
			SELECT a.Name FROM credit c JOIN artist a ON a.mbid = c.artist_mbid
			WHERE c.subject_type = 'work' AND c.subject_mbid = $1 AND c.role = 'composer'
			ORDER BY c.position`, workMbid)
		if err == nil {
			for rows.Next() {
				var n string
				if err := rows.Scan(&n); err == nil {
					composers = append(composers, n)
				}
			}
			rows.Close()
		}
		if len(composers) == 0 {
			composers = nil
		}
		work = &struct {
			CanonicalName *string            `json:"canonical_name,omitempty"`
			Composers     *[]string          `json:"composers,omitempty"`
			Mbid          openapi_types.UUID `json:"mbid"`
			Name          string             `json:"name"`
		}{
			Mbid:          uuidOf(workMbid),
			Name:          displayName(prefs["work"], workTitle, workAliases),
			CanonicalName: ptr(workTitle),
			Composers:     &composers,
		}
	} else if err != sql.ErrNoRows {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := gen.RecordingDetail{
		Mbid:          mbid,
		Name:          displayName(prefs["recording"], title, aliases),
		CanonicalName: ptr(title),
		LengthMs:      intPtr(lengthMS),
		Aliases:       ptrToGenAliases(aliases),
		Credits:       credits,
		Work:          work,
	}
	writeOK(w, out)
}

func ptrToGenAliases(aliases []display.Alias) *[]gen.Alias {
	out := []gen.Alias{}
	for _, a := range aliases {
		out = append(out, gen.Alias{
			Name:    a.Name,
			Locale:  ptr(a.Locale),
			Primary: &a.Primary,
			Begin:   ptr(a.Begin),
			End:     ptr(a.End),
		})
	}
	return &out
}

func orOmit(s string) string {
	if s == "" {
		return ""
	}
	return s
}
