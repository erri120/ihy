// End-to-end transcode tests: profile CRUD with the admin gate, and
// the transcode endpoint over a fake encoder (plus a real-ffmpeg
// smoke test that skips when ffmpeg is absent).
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
	"strings"
	"sync/atomic"
	"testing"

	"ihy/internal/auth"
	"ihy/internal/transcode"
)

// fakeAPIEncoder counts encodes and writes marker content.
type fakeAPIEncoder struct{ calls atomic.Int64 }

func (f *fakeAPIEncoder) Encode(ctx context.Context, srcPath string, p transcode.Profile, outPath string) error {
	f.calls.Add(1)
	return os.WriteFile(outPath, []byte("encoded:"+p.Name), 0o644)
}

// firstStreamURL finds one playable stream URL through the public API.
func firstStreamURL(t *testing.T, srv *httptest.Server, cookie *http.Cookie) string {
	t.Helper()
	_, raw := streamGet(t, srv, cookie, "/search?q=Hypa+Hypa", nil)
	var hits []map[string]any
	if err := json.Unmarshal(raw, &hits); err != nil {
		t.Fatalf("search decode: %v (%s)", err, raw)
	}
	for _, h := range hits {
		if h["type"] != "recording" {
			continue
		}
		_, raw := streamGet(t, srv, cookie, "/recordings/"+h["mbid"].(string), nil)
		var det struct {
			Streams []struct {
				Url string `json:"url"`
			} `json:"streams"`
		}
		if json.Unmarshal(raw, &det) == nil && len(det.Streams) > 0 {
			return det.Streams[0].Url
		}
	}
	t.Fatal("no stream url found")
	return ""
}

func setupTranscodeAPI(t *testing.T) (*httptest.Server, *sql.DB, *fakeAPIEncoder) {
	t.Helper()
	enc := &fakeAPIEncoder{}
	srv, conn := setupAPIServer(t, Options{TranscodeDir: t.TempDir(), Encoder: enc})
	return srv, conn, enc
}

func login(t *testing.T, srv *httptest.Server, username, password string) *http.Cookie {
	t.Helper()
	resp, err := http.Post(srv.URL+"/auth/login", "application/json",
		bytes.NewReader([]byte(`{"username":"`+username+`","password":"`+password+`"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login %s: %d %s", username, resp.StatusCode, body)
	}
	return resp.Cookies()[0]
}

func doJSON(t *testing.T, srv *httptest.Server, cookie *http.Cookie, method, path string, body any) (int, []byte) {
	t.Helper()
	var raw io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, raw)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	out, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return r.StatusCode, out
}

func TestTranscodeProfileCRUD(t *testing.T) {
	srv, _, _ := setupTranscodeAPI(t)
	cookie := login(t, srv, "admin", "hunter2!")

	// The two seeded presets are there.
	code, body := doJSON(t, srv, cookie, "GET", "/transcode-profiles", nil)
	if code != 200 {
		t.Fatalf("list: %d %s", code, body)
	}
	var profiles []map[string]any
	if err := json.Unmarshal(body, &profiles); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(profiles) != 2 {
		t.Fatalf("seeded profiles = %v, want opus-96 and opus-128", profiles)
	}
	for _, p := range profiles {
		if p["container"] != "ogg" || p["codec"] != "libopus" {
			t.Errorf("preset %+v is not ogg/libopus", p)
		}
	}

	// Create, duplicate, invalid.
	code, body = doJSON(t, srv, cookie, "POST", "/transcode-profiles",
		map[string]any{"name": "opus-48", "container": "ogg", "codec": "libopus", "bitrate_kbps": 48})
	if code != 201 {
		t.Fatalf("create: %d %s", code, body)
	}
	code, body = doJSON(t, srv, cookie, "POST", "/transcode-profiles",
		map[string]any{"name": "opus-48", "container": "ogg", "codec": "libopus", "bitrate_kbps": 48})
	if code != 409 {
		t.Errorf("duplicate create: %d %s, want 409", code, body)
	}
	code, body = doJSON(t, srv, cookie, "POST", "/transcode-profiles",
		map[string]any{"name": "", "container": "ogg", "codec": "libopus", "bitrate_kbps": 48})
	if code != 400 {
		t.Errorf("empty name create: %d %s, want 400", code, body)
	}
	code, body = doJSON(t, srv, cookie, "POST", "/transcode-profiles",
		map[string]any{"name": "neg", "container": "ogg", "codec": "libopus", "bitrate_kbps": -5})
	if code != 400 {
		t.Errorf("negative bitrate create: %d %s, want 400", code, body)
	}

	// Update and delete.
	code, body = doJSON(t, srv, cookie, "PUT", "/transcode-profiles/3",
		map[string]any{"name": "opus-48", "container": "ogg", "codec": "libopus", "bitrate_kbps": 64})
	if code != 200 {
		t.Fatalf("update: %d %s", code, body)
	}
	code, body = doJSON(t, srv, cookie, "PUT", "/transcode-profiles/9999",
		map[string]any{"name": "x", "container": "ogg", "codec": "libopus", "bitrate_kbps": 64})
	if code != 404 {
		t.Errorf("update unknown: %d %s, want 404", code, body)
	}
	code, body = doJSON(t, srv, cookie, "DELETE", "/transcode-profiles/3", nil)
	if code != 200 {
		t.Fatalf("delete: %d %s", code, body)
	}
	code, body = doJSON(t, srv, cookie, "GET", "/transcode-profiles", nil)
	if code != 200 || strings.Count(string(body), `"name"`) != 2 {
		t.Errorf("list after delete = %d %s", code, body)
	}
}

func TestTranscodeProfileAdminGate(t *testing.T) {
	srv, conn, _ := setupTranscodeAPI(t)
	cookie := login(t, srv, "admin", "hunter2!")

	// A second, non-admin user.
	hash, err := auth.HashPassword("listener-pass")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO "user" (name, password_hash) VALUES ('listener', $1)`, hash); err != nil {
		t.Fatal(err)
	}
	userCookie := login(t, srv, "listener", "listener-pass")

	// Reading is for everyone; writing is admin-only.
	code, _ := doJSON(t, srv, userCookie, "GET", "/transcode-profiles", nil)
	if code != 200 {
		t.Errorf("non-admin list: %d, want 200", code)
	}
	code, body := doJSON(t, srv, userCookie, "POST", "/transcode-profiles",
		map[string]any{"name": "x", "container": "ogg", "codec": "libopus", "bitrate_kbps": 96})
	if code != 403 {
		t.Errorf("non-admin create: %d %s, want 403", code, body)
	}
	code, body = doJSON(t, srv, userCookie, "DELETE", "/transcode-profiles/1", nil)
	if code != 403 {
		t.Errorf("non-admin delete: %d %s, want 403", code, body)
	}
	// The admin still can.
	code, _ = doJSON(t, srv, cookie, "DELETE", "/transcode-profiles/1", nil)
	if code != 200 {
		t.Errorf("admin delete: %d, want 200", code)
	}
}

func TestTranscodeEndpoint(t *testing.T) {
	srv, _, enc := setupTranscodeAPI(t)
	cookie := login(t, srv, "admin", "hunter2!")
	url := firstStreamURL(t, srv, cookie)

	// First request encodes synchronously.
	code, body := doJSON(t, srv, cookie, "GET", url+"/transcode?profile=opus-128&upcoming=abc", nil)
	if code != 200 {
		t.Fatalf("transcode: %d %s", code, body)
	}
	if string(body) != "encoded:opus-128" {
		t.Errorf("transcode body = %q", body)
	}
	if calls := enc.calls.Load(); calls != 1 {
		t.Errorf("encoder calls = %d, want 1", calls)
	}

	// Second request is a cache hit.
	_, body2 := doJSON(t, srv, cookie, "GET", url+"/transcode?profile=opus-128", nil)
	if string(body2) != "encoded:opus-128" {
		t.Errorf("cache hit body = %q", body2)
	}
	if calls := enc.calls.Load(); calls != 1 {
		t.Errorf("encoder calls after cache hit = %d, want 1", calls)
	}

	// Unknown profile and unknown file.
	code, body = doJSON(t, srv, cookie, "GET", url+"/transcode?profile=nope", nil)
	if code != 404 || !strings.Contains(string(body), `"error"`) {
		t.Errorf("unknown profile: %d %s", code, body)
	}
	code, body = doJSON(t, srv, cookie, "GET", "/stream/files/999999/transcode?profile=opus-128", nil)
	if code != 404 {
		t.Errorf("unknown file: %d %s", code, body)
	}

	// Profiles deleted mid-flight: deleting the profile leaves the
	// cache entries orphaned until TTL; requests fail with 404.
	code, _ = doJSON(t, srv, cookie, "DELETE", "/transcode-profiles/2", nil)
	if code != 200 {
		t.Fatalf("delete opus-128: %d", code)
	}
	code, body = doJSON(t, srv, cookie, "GET", url+"/transcode?profile=opus-128", nil)
	if code != 404 {
		t.Errorf("deleted profile: %d %s, want 404", code, body)
	}
}

// TestTranscodeWithRealFFmpeg exercises the actual encoder over a
// fixture FLAC; skips when no usable ffmpeg is on the machine.
func TestTranscodeWithRealFFmpeg(t *testing.T) {
	if err := (transcode.FFmpeg{Bin: "ffmpeg"}).Check(context.Background()); err != nil {
		t.Skipf("ffmpeg unavailable: %v", err)
	}
	srv, _ := setupAPIServer(t, Options{TranscodeDir: t.TempDir(), FFmpegPath: "ffmpeg"})
	cookie := login(t, srv, "admin", "hunter2!")
	url := firstStreamURL(t, srv, cookie)

	code, body := doJSON(t, srv, cookie, "GET", url+"/transcode?profile=opus-128", nil)
	if code != 200 {
		t.Fatalf("real transcode: %d (%s)", code, body[:min(len(body), 200)])
	}
	if len(body) == 0 || string(body[:4]) != "OggS" {
		t.Fatalf("output is not an ogg stream (%d bytes)", len(body))
	}
	// The cache hit serves the same bytes.
	_, body2 := doJSON(t, srv, cookie, "GET", url+"/transcode?profile=opus-128", nil)
	if string(body2) != string(body) {
		t.Error("cache hit returned different bytes")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
