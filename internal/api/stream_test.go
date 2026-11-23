// End-to-end streaming test: discovery via the recording page, then
// full, ranged, conditional and error paths of GET /stream/files/{id}.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func streamLogin(t *testing.T, srv *httptest.Server) *http.Cookie {
	t.Helper()
	resp, err := http.Post(srv.URL+"/auth/login", "application/json",
		bytes.NewReader([]byte(`{"username":"admin","password":"hunter2!"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	return resp.Cookies()[0]
}

func streamGet(t *testing.T, srv *httptest.Server, cookie *http.Cookie, path string, header map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	req.AddCookie(cookie)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return r, raw
}

func TestStreamFileAndDiscovery(t *testing.T) {
	srv := setupAPI(t)
	cookie := streamLogin(t, srv)

	// Discovery: search a recording, then read its playable sources.
	_, raw := streamGet(t, srv, cookie, "/search?q=Hypa+Hypa", nil)
	var hits []map[string]any
	if err := json.Unmarshal(raw, &hits); err != nil {
		t.Fatalf("search decode: %v (%s)", err, raw)
	}
	var recMBID string
	for _, h := range hits {
		if h["type"] == "recording" {
			recMBID = h["mbid"].(string)
			break
		}
	}
	if recMBID == "" {
		t.Fatalf("no recording hit for 'Hypa Hypa': %s", raw)
	}

	_, raw = streamGet(t, srv, cookie, "/recordings/"+recMBID, nil)
	if !strings.Contains(string(raw), `"streams"`) {
		t.Fatalf("recording detail has no streams: %s", raw)
	}
	var detail struct {
		Streams []struct {
			Url       string `json:"url"`
			Format    string `json:"format"`
			SizeBytes int64  `json:"size_bytes"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("recording decode: %v (%s)", err, raw)
	}
	if len(detail.Streams) == 0 {
		t.Fatal("recording detail: empty streams")
	}
	src := detail.Streams[0]
	if src.Format != "flac" || src.SizeBytes <= 0 {
		t.Errorf("first stream source = %+v", src)
	}
	if !strings.HasPrefix(src.Url, "/stream/files/") {
		t.Fatalf("stream url = %q", src.Url)
	}
	if _, err := strconv.ParseInt(strings.TrimPrefix(src.Url, "/stream/files/"), 10, 64); err != nil {
		t.Fatalf("stream url %q: %v", src.Url, err)
	}

	// Full GET: metadata and body length must match the DB row.
	r, body := streamGet(t, srv, cookie, src.Url, nil)
	if r.StatusCode != 200 {
		t.Fatalf("stream GET: %d %s", r.StatusCode, body)
	}
	if ct := r.Header.Get("Content-Type"); ct != "audio/flac" {
		t.Errorf("Content-Type = %q, want audio/flac", ct)
	}
	if r.Header.Get("Accept-Ranges") != "bytes" {
		t.Error("missing Accept-Ranges: bytes")
	}
	if cl := r.Header.Get("Content-Length"); cl != strconv.FormatInt(src.SizeBytes, 10) {
		t.Errorf("Content-Length = %q, want %d", cl, src.SizeBytes)
	}
	if int64(len(body)) != src.SizeBytes {
		t.Errorf("body = %d bytes, want %d", len(body), src.SizeBytes)
	}
	etag := r.Header.Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	// Range request: a 4-byte prefix.
	r, body = streamGet(t, srv, cookie, src.Url, map[string]string{"Range": "bytes=0-3"})
	if r.StatusCode != 206 || len(body) != 4 {
		t.Fatalf("range GET: %d, %d bytes", r.StatusCode, len(body))
	}
	if cr := r.Header.Get("Content-Range"); !strings.HasPrefix(cr, fmt.Sprintf("bytes 0-3/%d", src.SizeBytes)) {
		t.Errorf("Content-Range = %q", cr)
	}

	// Conditional GET with the ETag: 304, empty body.
	r, body = streamGet(t, srv, cookie, src.Url, map[string]string{"If-None-Match": etag})
	if r.StatusCode != 304 || len(body) != 0 {
		t.Errorf("If-None-Match: %d, %d bytes", r.StatusCode, len(body))
	}

	// Unknown file id: 404 with a JSON error body.
	r, body = streamGet(t, srv, cookie, "/stream/files/999999", nil)
	if r.StatusCode != 404 || !strings.Contains(string(body), `"error"`) {
		t.Errorf("unknown file: %d %s", r.StatusCode, body)
	}

	// Unauthenticated: the auth middleware rejects before the handler.
	req, _ := http.NewRequest("GET", srv.URL+src.Url, nil)
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rsp.Body.Close()
	if rsp.StatusCode != 401 {
		t.Errorf("unauthenticated stream: %d, want 401", rsp.StatusCode)
	}
}

func TestStreamWarmHeaderIsAdvisory(t *testing.T) {
	// Garbage in the X-Ihy-Upcoming header must not disturb serving.
	srv := setupAPI(t)
	cookie := streamLogin(t, srv)
	_, raw := streamGet(t, srv, cookie, "/search?q=Hypa+Hypa", nil)
	var hits []map[string]any
	if err := json.Unmarshal(raw, &hits); err != nil {
		t.Fatalf("search decode: %v (%s)", err, raw)
	}
	var url string
	for _, h := range hits {
		if h["type"] == "recording" {
			_, d := streamGet(t, srv, cookie, "/recordings/"+h["mbid"].(string), nil)
			var det struct {
				Streams []struct {
					Url string `json:"url"`
				} `json:"streams"`
			}
			if json.Unmarshal(d, &det) == nil && len(det.Streams) > 0 {
				url = det.Streams[0].Url
				break
			}
		}
	}
	if url == "" {
		t.Fatal("no stream url found")
	}
	r, body := streamGet(t, srv, cookie, url, map[string]string{"X-Ihy-Upcoming": "abc, 999999, 999999"})
	if r.StatusCode != 200 {
		t.Fatalf("stream GET with advisory garbage: %d %s", r.StatusCode, body)
	}
}
