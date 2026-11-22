// The design catalog is development tooling: registered only in dev
// mode and served without auth (like the static assets).
package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDesignIsDevOnly(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{Dev: false}).Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/design", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /ui/design outside dev mode: got %d, want 404", rec.Code)
	}
}

func TestDesignSkipsAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{Dev: true}).Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/design", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/design in dev mode without a session: got %d, want 200", rec.Code)
	}
}

func TestDevRefreshDevOnly(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{Dev: false}).Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/dev/refresh", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /ui/dev/refresh outside dev mode: got %d, want 404", rec.Code)
	}
}
