// Web UI handlers: form login, search, browse. Auth reuses the same
// session cookie as the JSON API, with a redirect-based middleware
// instead of 401 JSON responses.
package webui

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
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
	// Dev enables UI development mode: the design page hot-reloads
	// static assets over SSE and full-reloads on server restarts.
	Dev bool
	// DevStatic serves static assets from this directory on disk
	// instead of the embedded copies (dev only).
	DevStatic string
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

// Kind names the entity the card counts ("Album"); counts always name
// their entity (DESIGN.md §2).
func (a AlbumVM) Kind(t *T) string {
	return t.Msg("KindAlbum")
}

type ArtistVM struct {
	Mbid, Name, Canonical, Meta string
	// Type is the MB artist type (Person | Group | Character).
	Type string
	// Original is the canonical name when it differs from the display
	// name (the second, muted line in the page head).
	Original string
	// FormerName renders as "formerly X (until Y)" (rename aliases
	// with an end date; groups mainly).
	FormerName string
	// Origin is the type/area/life-span fact line for the about cell.
	Origin string
	// Play/queue actions land with the player milestone; the actions
	// render disabled until then.
	Albums      []AlbumVM
	Recordings  []RecordingVM
	Works       []WorkVM
	Credits     []CreditVM
	Aliases     []display.Alias
	CreditTotal int
	Tabs        []TabVM
}

// RecordingVM is one recording row (distinct audio, DESIGN.md §2).
type RecordingVM struct {
	Mbid, Title, Length string
}

// WorkVM is one composition row (works tab).
type WorkVM struct {
	Mbid, Title, ISWC string
}

// CreditVM aggregates one relationship role (raw MB type, verbatim).
type CreditVM struct {
	Role  string
	Count int
}

// TabVM is one artist page tab (label + full and fragment URLs).
type TabVM struct {
	Name, Label, URL, Fragment string
	Active                     bool
}

func tabURL(mbid, tab string) string {
	return "/ui/artists/" + url.PathEscape(mbid) + "?tab=" + tab
}

func tabFragment(mbid, tab string) string {
	return "/ui/artists/" + url.PathEscape(mbid) + "/tab/" + tab
}

func recordingDetailFragment(mbid, recordingMbid string) string {
	return "/ui/artists/" + url.PathEscape(mbid) + "/recordings/" + url.PathEscape(recordingMbid) + "/detail"
}

func itoa(n int) string { return strconv.Itoa(n) }

// limited caps a slice for overview sections (most played lists).
func limited[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// playURL is the play intent target for one recording.
func playURL(recordingMbid string) string {
	return "/ui/play?recording=" + url.PathEscape(recordingMbid)
}

// playAlbumURL is the play intent target for a release group (album
// cards): the same accepted-intent contract as rows.
func playAlbumURL(rgMbid string) string {
	return "/ui/play?release-group=" + url.PathEscape(rgMbid)
}

// artistTabs builds the tab list with the active state.
func artistTabs(mbid, active string, t *T) []TabVM {
	names := []struct{ name, key string }{
		{"overview", "TabOverview"},
		{"albums", "Albums"},
		{"recordings", "TabRecordings"},
		{"works", "TabWorks"},
		{"credits", "TabCredits"},
	}
	out := make([]TabVM, 0, len(names))
	for _, n := range names {
		out = append(out, TabVM{
			Name:     n.name,
			Label:    t.Msg(n.key),
			URL:      tabURL(mbid, n.name),
			Fragment: tabFragment(mbid, n.name),
			Active:   n.name == active,
		})
	}
	return out
}

// aliasSummary renders the about cell: "岡部啓一 (ja, primary) · ..." up
// to limit entries, then "+N more".
func aliasSummary(aliases []display.Alias, limit int) string {
	if len(aliases) == 0 {
		return ""
	}
	parts := []string{}
	for i, a := range aliases {
		if i >= limit {
			break
		}
		s := a.Name
		if a.Locale != "" {
			s += " (" + a.Locale
			if a.Primary {
				s += ", primary"
			}
			s += ")"
		}
		parts = append(parts, s)
	}
	if rest := len(aliases) - limit; rest > 0 {
		parts = append(parts, fmt.Sprintf("+%d more", rest))
	}
	return strings.Join(parts, " · ")
}

func artistTypeLabel(t *T, typ string) string {
	switch typ {
	case "Person":
		return t.Msg("ArtistTypePerson")
	case "Group":
		return t.Msg("ArtistTypeGroup")
	case "Character":
		return t.Msg("ArtistTypeCharacter")
	}
	return t.Msg("KindArtist")
}

// librarySummary is the about cell: counts always name their entity.
func librarySummary(t *T, vm ArtistVM) string {
	parts := []string{t.Plural("StatsRecordings", len(vm.Recordings)), t.Plural("StatsAlbums", len(vm.Albums))}
	if len(vm.Works) > 0 {
		parts = append(parts, t.Plural("StatsWorks", len(vm.Works)))
	}
	return strings.Join(parts, " · ")
}

// Register mounts the web UI routes onto the main mux (specific
// patterns; the API keeps its catch-all).
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.auth(s.Home))
	mux.HandleFunc("GET /login", s.LoginPage)
	mux.HandleFunc("POST /login", s.Login)
	mux.HandleFunc("POST /logout", s.Logout)
	mux.HandleFunc("POST /ui/play", s.auth(s.PlayRecording))
	mux.HandleFunc("GET /ui/search", s.auth(s.SearchPage))
	mux.HandleFunc("GET /ui/search/results", s.auth(s.SearchResultsFragment))
	mux.HandleFunc("GET /ui/artists/{mbid}", s.auth(s.Artist))
	mux.HandleFunc("GET /ui/artists/{mbid}/tab/{tab}", s.auth(s.ArtistTab))
	mux.HandleFunc("GET /ui/artists/{mbid}/recordings/{recording}/detail", s.auth(s.ArtistRecordingDetail))
	mux.HandleFunc("GET /ui/release-groups/{mbid}", s.auth(s.ReleaseGroup))
	mux.HandleFunc("GET /ui/recordings/{mbid}", s.auth(s.Recording))
	if s.Dev {
		// The design catalog and its hot-reload stream are development
		// tooling: they exist only in dev mode and skip auth.
		mux.HandleFunc("GET /ui/design", s.Design)
		mux.HandleFunc("GET /ui/dev/refresh", s.devRefresh)
	}
	mux.Handle("GET /ui/static/", s.auth(s.staticHandler().ServeHTTP))
}

// Mux builds the web UI as a standalone handler (tests).
func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

// staticHandler serves static assets: embedded copies in production,
// from disk in dev mode (no-store, so hot reload always revalidates).
func (s *Server) staticHandler() http.Handler {
	if s.DevStatic != "" {
		fsys := os.DirFS(s.DevStatic)
		return http.StripPrefix("/ui/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			http.FileServerFS(fsys).ServeHTTP(w, r)
		}))
	}
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

// translator builds the per-request translator. Dev mode rides on T
// so every page's layout can hook up hot reload.
func (s *Server) translator(r *http.Request) *T {
	t := NewTranslator(r.Header.Get("Accept-Language"))
	t.Dev = s.Dev
	return t
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
	render(w, http.StatusOK, LoginPage(s.translator(r), msg))
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
	t := s.translator(r)
	render(w, http.StatusOK, HomePage(t, stats))
}

// SearchPage renders the search page. With ?q= the results render
// server-side so deep links work; live typing fetches the fragment.
func (s *Server) SearchPage(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	user, ok := userFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	results, err := s.resultVMs(r.Context(), user, q)
	if err != nil {
		s.renderErr(w, err)
		return
	}
	render(w, http.StatusOK, SearchPage(t, q, results))
}

// SearchResultsFragment serves the live results list for the debounced
// search input (GET /ui/search/results).
func (s *Server) SearchResultsFragment(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	user, ok := userFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	results, err := s.resultVMs(r.Context(), user, q)
	if err != nil {
		s.renderErr(w, err)
		return
	}
	render(w, http.StatusOK, SearchResults(t, results))
}

// resultVMs resolves a query into display-name-resolved view models.
// An empty query returns nothing without touching the database.
func (s *Server) resultVMs(ctx context.Context, user auth.User, q string) ([]SearchResultVM, error) {
	if q == "" {
		return nil, nil
	}
	prefs, err := display.PrefsFor(ctx, s.db, user.ID)
	if err != nil {
		return nil, err
	}
	hits, err := s.search(q)
	if err != nil {
		return nil, err
	}
	out := make([]SearchResultVM, 0, len(hits))
	for _, h := range hits {
		aliases, _ := display.Aliases(ctx, s.db, h.Type, h.Mbid)
		out = append(out, SearchResultVM{
			Type:      h.Type,
			Mbid:      h.Mbid,
			Name:      display.Name(prefs[h.Type], h.Name, aliases),
			Canonical: h.Name,
		})
	}
	return out, nil
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

// Artist renders the artist page (tab from the ?tab= query, so tab
// state survives a full page load and deep links).
func (s *Server) Artist(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	user, ok := userFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	mbid := r.PathValue("mbid")
	vm, err := s.artistVM(r.Context(), mbid, user, t)
	if err != nil {
		s.renderErr(w, err)
		return
	}
	if vm == nil {
		http.NotFound(w, r)
		return
	}
	render(w, http.StatusOK, ArtistPage(t, *vm, tabName(r.URL.Query().Get("tab"))))
}

// ArtistTab serves one tab's panel fragment for htmx swaps.
func (s *Server) ArtistTab(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	user, ok := userFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	vm, err := s.artistVM(r.Context(), r.PathValue("mbid"), user, t)
	if err != nil {
		s.renderErr(w, err)
		return
	}
	if vm == nil {
		http.NotFound(w, r)
		return
	}
	render(w, http.StatusOK, ArtistPanel(t, *vm, tabName(r.PathValue("tab"))))
}

// ArtistRecordingDetail serves the expandable detail beneath one
// recording row: this artist's credit roles on the recording (raw MB
// types verbatim) plus the composition line.
func (s *Server) ArtistRecordingDetail(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	if _, ok := userFrom(r.Context()); !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	mbid := r.PathValue("mbid")
	recordingMbid := r.PathValue("recording")

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT role, count(DISTINCT subject_mbid)
		FROM credit
		WHERE subject_type = 'recording' AND subject_mbid = $2
		  AND artist_mbid = $1 AND role <> 'artist-credit'
		GROUP BY role ORDER BY count DESC, role`, mbid, recordingMbid)
	if err != nil {
		s.renderErr(w, err)
		return
	}
	defer rows.Close()
	var roles []CreditVM
	for rows.Next() {
		var c CreditVM
		if err := rows.Scan(&c.Role, &c.Count); err != nil {
			s.renderErr(w, err)
			return
		}
		roles = append(roles, c)
	}

	var work *WorkVM
	var title, iswc string
	err = s.db.QueryRowContext(r.Context(), `
		SELECT w.title, coalesce(w.iswc, '')
		FROM recording_work rw JOIN work w ON w.mbid = rw.work_mbid
		WHERE rw.recording_mbid = $1`, recordingMbid).
		Scan(&title, &iswc)
	if err != nil && err != sql.ErrNoRows {
		s.renderErr(w, err)
		return
	}
	if err == nil {
		work = &WorkVM{Mbid: recordingMbid, Title: title, ISWC: iswc}
	}
	render(w, http.StatusOK, ArtistRecordingDetail(t, roles, work))
}

// PlayRecording accepts a play intent (track row clicks, album card
// plays). Playback lands with the player milestone; until then the
// intent is accepted and intentionally does nothing, keeping the
// interaction contract (click = play) wired.
func (s *Server) PlayRecording(w http.ResponseWriter, r *http.Request) {
	if rg := r.FormValue("release-group"); rg != "" {
		log.Printf("play intent: release-group %s (player milestone pending)", rg)
	} else {
		log.Printf("play intent: recording %s (player milestone pending)", r.FormValue("recording"))
	}
	w.WriteHeader(http.StatusAccepted)
}

// creditRoleNames maps credit rows to their raw role names (the
// detail surface renders the raw upstream types verbatim).
func creditRoleNames(roles []CreditVM) []string {
	out := make([]string, 0, len(roles))
	for _, c := range roles {
		out = append(out, c.Role)
	}
	return out
}

func workTitle(work *WorkVM) string {
	if work == nil {
		return ""
	}
	return work.Title
}

// tabName normalizes the ?tab= value; unknown tabs fall back to the
// overview.
func tabName(s string) string {
	switch s {
	case "albums", "recordings", "works", "credits":
		return s
	}
	return "overview"
}

func (s *Server) renderErr(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// artistVM loads the full artist page view model. A nil VM with a nil
// error means no such artist.
func (s *Server) artistVM(ctx context.Context, mbid string, user auth.User, t *T) (*ArtistVM, error) {
	prefs, err := display.PrefsFor(ctx, s.db, user.ID)
	if err != nil {
		return nil, err
	}
	p := prefs["artist"]

	var canonical, typ, country, begin, end string
	err = s.db.QueryRowContext(ctx, `
		SELECT name, coalesce(type, ''), coalesce(country, ''),
		       coalesce(begin_date, ''), coalesce(end_date, '')
		FROM artist WHERE mbid = $1`, mbid).
		Scan(&canonical, &typ, &country, &begin, &end)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	aliases, err := display.Aliases(ctx, s.db, "artist", mbid)
	if err != nil {
		return nil, err
	}

	// Life-span wording is type-driven (DESIGN.md: artist header
	// variants): person born, group formed, character created.
	var lifeVerb string
	switch typ {
	case "Person":
		lifeVerb = "Born"
	case "Group":
		lifeVerb = "Formed"
	case "Character":
		lifeVerb = "Created"
	}
	life := begin
	if life != "" && lifeVerb != "" {
		life = t.Msg(lifeVerb) + " " + life
	}

	vm := &ArtistVM{
		Mbid:      mbid,
		Name:      display.Name(p, canonical, aliases),
		Canonical: canonical,
		Type:      typ,
		Aliases:   aliases,
		Origin:    strings.Join(nonEmpty(country, life), " · "),
	}
	if vm.Name != canonical {
		vm.Original = canonical
	}
	for _, a := range aliases {
		if a.End != "" {
			vm.FormerName = t.Msg("Formerly") + " " + a.Name + " (" + t.Msg("Until") + " " + a.End + ")"
			break
		}
	}

	// Recording count for the meta line: recordings with at least one
	// file in the library.
	var recordingCount int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(DISTINCT c.subject_mbid)
		FROM credit c
		WHERE c.subject_type = 'recording' AND c.artist_mbid = $1 AND c.role = 'artist-credit'
		  AND EXISTS (SELECT 1 FROM file f WHERE f.recording_mbid = c.subject_mbid)`, mbid).
		Scan(&recordingCount); err != nil {
		return nil, err
	}
	meta := strings.Join(nonEmpty(artistTypeLabel(t, typ), country, life), " · ")
	if recordingCount > 0 {
		meta = strings.Join(nonEmpty(meta, t.Plural("StatsRecordings", recordingCount)), " · ")
	}
	vm.Meta = meta
	vm.Tabs = artistTabs(mbid, "", t)

	// Albums: primary release-group credits, then featured-on (the
	// artist appears only on tracks of that release group).
	type album struct {
		mbid, title, date string
	}
	primary := map[string]bool{}
	var albums []album
	rows, err := s.db.QueryContext(ctx, `
		SELECT rg.mbid, rg.title, coalesce(rg.first_release_date, '')
		FROM credit c JOIN release_group rg ON rg.mbid = c.subject_mbid
		WHERE c.subject_type = 'release_group' AND c.artist_mbid = $1 AND c.role = 'artist-credit'
		ORDER BY rg.first_release_date DESC`, mbid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a album
		if err := rows.Scan(&a.mbid, &a.title, &a.date); err != nil {
			return nil, err
		}
		albums = append(albums, a)
		primary[a.mbid] = true
	}
	rows.Close()

	featuredCount := map[string]int{}
	rows, err = s.db.QueryContext(ctx, `
		SELECT rg.mbid, rg.title, coalesce(rg.first_release_date, ''), count(DISTINCT f.recording_mbid)
		FROM credit c
		JOIN file f ON f.recording_mbid = c.subject_mbid
		JOIN release rel ON rel.mbid = f.release_mbid
		JOIN release_group rg ON rg.mbid = rel.release_group_mbid
		WHERE c.subject_type = 'recording' AND c.artist_mbid = $1
		GROUP BY rg.mbid, rg.title, rg.first_release_date`, mbid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a album
		var featured int
		if err := rows.Scan(&a.mbid, &a.title, &a.date, &featured); err != nil {
			return nil, err
		}
		featuredCount[a.mbid] = featured
		if !primary[a.mbid] {
			albums = append(albums, a)
		}
	}
	rows.Close()

	for _, a := range albums {
		rgAliases, _ := display.Aliases(ctx, s.db, "release_group", a.mbid)
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

	// Recordings: distinct audio the artist is credited on and that
	// has files in the library.
	rows, err = s.db.QueryContext(ctx, `
		SELECT r.mbid, r.title, r.length_ms
		FROM recording r
		WHERE EXISTS (SELECT 1 FROM file f WHERE f.recording_mbid = r.mbid)
		  AND EXISTS (SELECT 1 FROM credit c
		              WHERE c.subject_type = 'recording' AND c.subject_mbid = r.mbid
		                AND c.artist_mbid = $1 AND c.role = 'artist-credit')
		ORDER BY coalesce(r.first_release_date, '') DESC, r.title`, mbid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rec RecordingVM
		var ms sql.NullInt64
		if err := rows.Scan(&rec.Mbid, &rec.Title, &ms); err != nil {
			return nil, err
		}
		rec.Length = formatLength(ms)
		vm.Recordings = append(vm.Recordings, rec)
	}
	rows.Close()

	// Works: compositions reached through composer/writer/arranger
	// relationships (works have no artist of their own).
	rows, err = s.db.QueryContext(ctx, `
		SELECT w.mbid, w.title, coalesce(w.iswc, '')
		FROM work w
		WHERE EXISTS (SELECT 1 FROM credit c
		              WHERE c.subject_type = 'work' AND c.subject_mbid = w.mbid
		                AND c.artist_mbid = $1)
		ORDER BY w.title`, mbid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var w WorkVM
		if err := rows.Scan(&w.Mbid, &w.Title, &w.ISWC); err != nil {
			return nil, err
		}
		vm.Works = append(vm.Works, w)
	}
	rows.Close()

	// Credits: role totals, raw relationship types verbatim.
	rows, err = s.db.QueryContext(ctx, `
		SELECT role, count(DISTINCT subject_type || subject_mbid::text)
		FROM credit WHERE artist_mbid = $1
		GROUP BY role ORDER BY count DESC, role`, mbid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c CreditVM
		if err := rows.Scan(&c.Role, &c.Count); err != nil {
			return nil, err
		}
		vm.Credits = append(vm.Credits, c)
		vm.CreditTotal += c.Count
	}
	return vm, nil
}

// formatLength renders milliseconds as m:ss; unknown lengths render
// empty (the row just omits the duration).
func formatLength(ms sql.NullInt64) string {
	if !ms.Valid || ms.Int64 <= 0 {
		return ""
	}
	total := ms.Int64 / 1000
	return fmt.Sprintf("%d:%02d", total/60, total%60)
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
	t := s.translator(r)
	render(w, http.StatusOK, ComingSoonPage(t, t.Msg("KindAlbum")))
}

func (s *Server) Recording(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	render(w, http.StatusOK, ComingSoonPage(t, t.Msg("KindTrack")))
}
