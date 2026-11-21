// End-to-end API test: scan + resolve the fixtures offline, then
// exercise the read API with display preferences.
package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.senan.xyz/taglib"

	"ihy/internal/api/gen"
	"ihy/internal/auth"
	"ihy/internal/db"
	"ihy/internal/dbtest"
	"ihy/internal/musicbrainz"
	"ihy/internal/resolver"
	"ihy/internal/scanner"
)

func setupAPI(t *testing.T) *httptest.Server {
	t.Helper()
	fixtureDir := filepath.Join("..", "..", "examples")
	if _, err := os.Stat(fixtureDir); err != nil {
		t.Skipf("fixtures unavailable: %v", err)
	}
	dsn := dbtest.Server(t)
	if _, err := db.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	if created, err := auth.EnsureAdminUser(context.Background(), conn, "admin", "hunter2!"); err != nil || !created {
		t.Fatalf("bootstrap admin: created=%v err=%v", created, err)
	}

	if _, err := scanner.Scan(context.Background(), conn, fixtureDir, taglib.ReadTags); err != nil {
		t.Fatalf("scan: %v", err)
	}
	rt, err := musicbrainz.NewReplayTransport(filepath.Join("..", "musicbrainz", "testdata", "replays"))
	if err != nil {
		t.Fatalf("replays: %v", err)
	}
	client := musicbrainz.NewClient(musicbrainz.DefaultBaseURL, "test", rt)
	if _, err := resolver.New(conn, client).Resolve(context.Background()); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	server := NewServer(conn)
	mux := http.NewServeMux()
	mux.Handle("/", NewAuth(conn).Middleware(gen.Handler(server)))
	return httptest.NewServer(mux)
}

func TestDisplayNamesAndBrowse(t *testing.T) {
	srv := setupAPI(t)

	// Login.
	resp, err := http.Post(srv.URL+"/auth/login", "application/json",
		bytes.NewReader([]byte(`{"username":"admin","password":"hunter2!"}`)))
	if err != nil {
		t.Fatal(err)
	}
	cookie := resp.Cookies()[0]
	resp.Body.Close()

	get := func(path string) (int, map[string]any) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.AddCookie(cookie)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		raw, _ := io.ReadAll(r.Body)
		var out map[string]any
		json.Unmarshal(raw, &out)
		if r.StatusCode != 200 {
			t.Fatalf("GET %s: %d %s", path, r.StatusCode, raw)
		}
		return r.StatusCode, out
	}
	putJSON := func(path string, body any) int {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("PUT", srv.URL+path, bytes.NewReader(b))
		req.AddCookie(cookie)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}

	// Search by English alias: the canonical title is Japanese.
	req, _ := http.NewRequest("GET", srv.URL+"/search?q=Peaceful+Sleep", nil)
	req.AddCookie(cookie)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("search 'Peaceful Sleep': %d %s", r.StatusCode, raw)
	}
	var hits []map[string]any
	if err := json.Unmarshal(raw, &hits); err != nil {
		t.Fatalf("search decode: %v (%s)", err, raw)
	}
	if len(hits) == 0 {
		t.Fatal("search 'Peaceful Sleep': 0 hits")
	}
	for _, h := range hits {
		if h["type"] == "recording" && h["canonical_name"] == "穏ヤカナ眠リ" {
			break
		}
	}

	// Display prefs: prefer English artist names.
	putJSON("/me/display-prefs", []map[string]any{{
		"entity_type":    "artist",
		"locale_chain":   []string{"en"},
		"history_policy": "current",
	}})

	// The artist page displays the English primary alias while the
	// canonical name stays.
	code, body := get("/artists/d82df34a-a1ff-45fa-ac50-c0b7454b201e")
	if code != 200 {
		t.Fatalf("artist page: %d %v", code, body)
	}
	if body["name"] != "Keiichi Okabe" {
		t.Errorf("display name = %v, want Keiichi Okabe", body["name"])
	}
	if body["canonical_name"] != "岡部啓一" {
		t.Errorf("canonical name = %v, want 岡部啓一", body["canonical_name"])
	}

	// MMXX: Electric Callboy primary, with the renamed-artist display.
	code, body = get("/artists/cf075492-d880-4afc-b87b-d6b03e33dacc")
	if code != 200 {
		t.Fatalf("artist page: %d %v", code, body)
	}
	// With locale chain en, the artist display name is the English
	// primary alias; the canonical stays.
	if body["name"] != "Electric Callboy" {
		t.Errorf("artist display = %v, want Electric Callboy", body["name"])
	}
	if body["canonical_name"] != "Electric Callboy" {
		t.Errorf("canonical = %v, want Electric Callboy", body["canonical_name"])
	}
	albums := body["albums"].([]any)
	sawPrimary := false
	for _, a := range albums {
		m := a.(map[string]any)
		if m["canonical_name"] == "MMXX" && m["relationship"] == "primary" {
			sawPrimary = true
			if m["name"] != "MMXX" {
				t.Errorf("album display = %v, want MMXX", m["name"])
			}
		}
	}
	if !sawPrimary {
		t.Error("MMXX not listed as primary album for Electric Callboy")
	}

	// 257ers: MMXX featured with a track count.
	code, body = get("/artists/ae667c89-4be1-4100-bb44-39bcfbe9cc28")
	if code != 200 {
		t.Fatalf("artist page: %d %v", code, body)
	}
	albums = body["albums"].([]any)
	featuredMMXX := false
	for _, a := range albums {
		m := a.(map[string]any)
		if m["canonical_name"] == "MMXX" && m["relationship"] == "featured" {
			featuredMMXX = true
			if n, ok := m["featured_tracks"].(float64); !ok || n < 1 {
				t.Errorf("featured track count missing: %v", m["featured_tracks"])
			}
		}
	}
	if !featuredMMXX {
		t.Error("MMXX not featured for 257ers")
	}

	// at_release policy: MMXX (2021) displays "Eskimo Callboy".
	putJSON("/me/display-prefs", []map[string]any{{
		"entity_type":    "artist",
		"locale_chain":   []string{"en"},
		"history_policy": "at_release",
	}})
	code, body = get("/artists/cf075492-d880-4afc-b87b-d6b03e33dacc")
	if code != 200 {
		t.Fatalf("artist page: %d", code)
	}
	albums = body["albums"].([]any)
	found := false
	for _, a := range albums {
		m := a.(map[string]any)
		if m["canonical_name"] == "MMXX" && m["relationship"] == "primary" && m["name"] == "Eskimo Callboy" {
			found = true
		}
	}
	if !found {
		t.Error("at_release policy did not display 'Eskimo Callboy' for MMXX")
	}
}
