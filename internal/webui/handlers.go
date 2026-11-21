// Web UI handlers: form login, search, browse. Auth reuses the same
// session cookie as the JSON API, with a redirect-based middleware
// instead of 401 JSON responses.
package webui

import (
	"context"
	"database/sql"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/a-h/templ"

	"ihy/internal/auth"
	"ihy/internal/display"
)

// Same cookie the JSON API issues, so one login covers both.
const sessionCookie = "ihy_session"

// Server holds the web UI handlers.
type Server struct {
	db *sql.DB
}

func NewServer(db *sql.DB) *Server { return &Server{db: db} }

// view models passed to templ components.

type StatsVM struct {
	Files, Artists, ReleaseGroups, Recordings int
}

type SearchResultVM struct {
	Type      string
	Mbid      string
	Name      string
	Canonical string
}

func (r SearchResultVM) KindID() string {
	return kindID(r.Type)
}

func (r SearchResultVM) Href() string {
	switch r.Type {
	case "artist":
		return "/ui/artists/" + url.PathEscape(r.Mbid)
	case "release_group":
		return "/ui/release-groups/" + url.PathEscape(r.Mbid)
	case "release":
		return "/ui/release-groups/" + url.PathEscape(r.Mbid)
	case "recording":
		return "/ui/recordings/" + url.PathEscape(r.Mbid)
	default:
		return ""
	}
}

type AlbumVM struct {
	Mbid, Name, FirstReleaseDate, Relationship string
	FeaturedTracks                             int
}

func (a AlbumVM) Href() string {
	return "/ui/release-groups/" + url.PathEscape(a.Mbid)
}

type ArtistVM struct {
	Name, Canonical, Meta string
	Albums                []AlbumVM
}

// Register mounts the web UI routes onto the main mux (specific
// patterns; the API keeps its catch-all).
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.auth(s.Home))
	mux.HandleFunc("GET /login", s.LoginPage)
	mux.HandleFunc("POST /login", s.Login)
	mux.HandleFunc("POST /logout", s.Logout)
	mux.HandleFunc("GET /ui/search", s.auth(s.Search))
	mux.HandleFunc("GET /ui/artists/{mbid}", s.auth(s.Artist))
	mux.HandleFunc("GET /ui/release-groups/{mbid}", s.auth(s.ReleaseGroup))
	mux.HandleFunc("GET /ui/recordings/{mbid}", s.auth(s.Recording))
	mux.Handle("GET /ui/static/", s.auth(staticHandler().ServeHTTP))
}

// Mux builds the web UI as a standalone handler (tests).
func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

// staticHandler serves embedded static assets.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/ui/static/", http.FileServer(http.FS(sub)))
}

// auth redirects unauthenticated browsers to the login page. Public
// paths (login, static assets) pass through.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || r.URL.Path == "/logout" || strings.HasPrefix(r.URL.Path, "/ui/static/") {
			next(w, r)
			return
		}
		if c, err := r.Cookie(sessionCookie); err == nil {
			if u, err := auth.UserFromSession(r.Context(), s.db, c.Value); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), userKey, u))
				next(w, r)
				return
			}
		}
		target := "/login"
		if r.Method == http.MethodGet && r.URL.Path != "/" {
			target += "?next=" + url.QueryEscape(r.URL.RequestURI())
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

type ctxKey int

const userKey ctxKey = 1

func userFrom(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(userKey).(auth.User)
	return u, ok
}

func render(w http.ResponseWriter, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	c.Render(context.Background(), w)
}

// LoginPage shows the login form.
func (s *Server) LoginPage(w http.ResponseWriter, r *http.Request) {
	msg := ""
	if r.URL.Query().Get("error") != "" {
		msg = "Wrong username or password."
	}
	render(w, http.StatusOK, LoginPage(NewTranslator(r.Header.Get("Accept-Language")), msg))
}

// Login handles the login form. Failures redirect back to the form
// with an error flag (htmx swaps only 2xx responses).
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")
	raw, expires, err := auth.Login(r.Context(), s.db, username, password)
	if err != nil {
		http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
	next := r.URL.Query().Get("next")
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// Logout clears the session cookie and returns to the login page.
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// Home shows library stats and the search box.
func (s *Server) Home(w http.ResponseWriter, r *http.Request) {
	if _, ok := userFrom(r.Context()); !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	stats := StatsVM{}
	for _, q := range []struct {
		dst *int
		sql string
	}{
		{&stats.Files, `SELECT count(*) FROM file`},
		{&stats.Artists, `SELECT count(*) FROM artist`},
		{&stats.ReleaseGroups, `SELECT count(*) FROM release_group`},
		{&stats.Recordings, `SELECT count(*) FROM recording`},
	} {
		if err := s.db.QueryRowContext(r.Context(), q.sql).Scan(q.dst); err != nil {
			http.Error(w, "stats: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	t := NewTranslator(r.Header.Get("Accept-Language"))
	render(w, http.StatusOK, HomePage(t, stats))
}

// Search serves the live results fragment.
func (s *Server) Search(w http.ResponseWriter, r *http.Request) {
	t := NewTranslator(r.Header.Get("Accept-Language"))
	user, ok := userFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		render(w, http.StatusOK, SearchResults(t, nil))
		return
	}
	prefs, err := display.PrefsFor(r.Context(), s.db, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hits, err := s.search(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]SearchResultVM, 0, len(hits))
	for _, h := range hits {
		aliases, _ := display.Aliases(r.Context(), s.db, h.Type, h.Mbid)
		out = append(out, SearchResultVM{
			Type:      h.Type,
			Mbid:      h.Mbid,
			Name:      display.Name(prefs[h.Type], h.Name, aliases),
			Canonical: h.Name,
		})
	}
	render(w, http.StatusOK, SearchResults(t, out))
}

type hit struct {
	Type, Mbid, Name string
}

// search mirrors the API's search: full-text over the recording
// search document, then name/alias matches for the other entities.
func (s *Server) search(q string) ([]hit, error) {
	var out []hit
	rows, err := s.db.Query(
		`SELECT mbid, title FROM recording, websearch_to_tsquery('simple', $1) query
		WHERE search_vector @@ query ORDER BY ts_rank_cd(search_vector, query) DESC LIMIT 20`, q)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var mbid, title string
		if err := rows.Scan(&mbid, &title); err != nil {
			return nil, err
		}
		out = append(out, hit{"recording", mbid, title})
	}
	rows.Close()
	for entityType, table := range map[string]string{
		"artist": "artist", "release_group": "release_group", "release": "release",
	} {
		nameCol := map[string]string{"artist": "name", "release_group": "title", "release": "title"}[entityType]
		rows, err := s.db.Query(
			`SELECT t.mbid, t.`+nameCol+` FROM `+table+` t
			WHERE t.`+nameCol+` ILIKE '%' || $1 || '%'
			   OR EXISTS (SELECT 1 FROM alias a WHERE a.entity_type = $2 AND a.entity_mbid = t.mbid
			              AND a.name ILIKE '%' || $1 || '%')
			LIMIT 10`, q, entityType)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var mbid, name string
			if err := rows.Scan(&mbid, &name); err != nil {
				return nil, err
			}
			out = append(out, hit{entityType, mbid, name})
		}
		rows.Close()
	}
	return out, nil
}

// Artist renders an artist page with albums.
func (s *Server) Artist(w http.ResponseWriter, r *http.Request) {
	t := NewTranslator(r.Header.Get("Accept-Language"))
	user, ok := userFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	mbid := r.PathValue("mbid")
	prefs, err := display.PrefsFor(r.Context(), s.db, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p := prefs["artist"]

	var canonical, typ, country, begin, end string
	err = s.db.QueryRowContext(r.Context(), `
		SELECT name, coalesce(type, ''), coalesce(country, ''),
		       coalesce(begin_date, ''), coalesce(end_date, '')
		FROM artist WHERE mbid = $1`, mbid).
		Scan(&canonical, &typ, &country, &begin, &end)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	aliases, err := display.Aliases(r.Context(), s.db, "artist", mbid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	meta := strings.Join(nonEmpty(typ, country, begin, end), " · ")
	vm := ArtistVM{
		Name:      display.Name(p, canonical, aliases),
		Canonical: canonical,
		Meta:      meta,
	}

	// Primary albums: release-group level credits.
	type album struct {
		mbid, title, date string
	}
	primary := map[string]bool{}
	var albums []album
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT rg.mbid, rg.title, coalesce(rg.first_release_date, '')
		FROM credit c JOIN release_group rg ON rg.mbid = c.subject_mbid
		WHERE c.subject_type = 'release_group' AND c.artist_mbid = $1 AND c.role = 'artist-credit'
		ORDER BY rg.first_release_date`, mbid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var a album
		if err := rows.Scan(&a.mbid, &a.title, &a.date); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		albums = append(albums, a)
		primary[a.mbid] = true
	}
	rows.Close()

	// Featured albums: the artist appears only on tracks.
	featuredCount := map[string]int{}
	rows, err = s.db.QueryContext(r.Context(), `
		SELECT rg.mbid, rg.title, coalesce(rg.first_release_date, ''), count(DISTINCT f.recording_mbid)
		FROM credit c
		JOIN file f ON f.recording_mbid = c.subject_mbid
		JOIN release rel ON rel.mbid = f.release_mbid
		JOIN release_group rg ON rg.mbid = rel.release_group_mbid
		WHERE c.subject_type = 'recording' AND c.artist_mbid = $1
		GROUP BY rg.mbid, rg.title, rg.first_release_date`, mbid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var a album
		var featured int
		if err := rows.Scan(&a.mbid, &a.title, &a.date, &featured); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		featuredCount[a.mbid] = featured
		if !primary[a.mbid] {
			albums = append(albums, a)
		}
	}
	rows.Close()

	for _, a := range albums {
		rgAliases, _ := display.Aliases(r.Context(), s.db, "release_group", a.mbid)
		avm := AlbumVM{
			Mbid:             a.mbid,
			Name:             display.Name(prefs["release_group"], a.title, rgAliases),
			FirstReleaseDate: a.date,
			Relationship:     "featured",
			FeaturedTracks:   featuredCount[a.mbid],
		}
		if primary[a.mbid] {
			avm.Relationship = "primary"
		}
		if p.HistoryPolicy == "at_release" && a.date != "" {
			if historical := display.NameAtRelease(aliases, a.date); historical != "" {
				avm.Name = historical
			}
		}
		vm.Albums = append(vm.Albums, avm)
	}
	render(w, http.StatusOK, ArtistPage(t, vm))
}

// nonEmpty filters out blank strings.
func nonEmpty(ss ...string) []string {
	out := []string{}
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ReleaseGroup and Recording are stubs for the player milestone.
func (s *Server) ReleaseGroup(w http.ResponseWriter, r *http.Request) {
	t := NewTranslator(r.Header.Get("Accept-Language"))
	render(w, http.StatusOK, ComingSoonPage(t, t.Msg("KindAlbum")))
}

func (s *Server) Recording(w http.ResponseWriter, r *http.Request) {
	t := NewTranslator(r.Header.Get("Accept-Language"))
	render(w, http.StatusOK, ComingSoonPage(t, t.Msg("KindTrack")))
}
