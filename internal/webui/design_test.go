// Design page routing: unauthenticated requests redirect to login,
// like every other UI page; the dev refresh stream is only mounted
// in dev mode.
package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDesignRequiresLogin(t *testing.T) {
	for _, dev := range []bool{false, true} {
		h := (&Server{Dev: dev}).Mux()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/design", nil))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("GET /ui/design without a session: got %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/login?next=%2Fui%2Fdesign" {
			t.Fatalf("redirect target: got %q, want /login?next=%%2Fui%%2Fdesign", loc)
		}
	}
}

func TestDevRefreshDevOnly(t *testing.T) {
	h := (&Server{Dev: false}).Mux()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/dev/refresh", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /ui/dev/refresh outside dev mode: got %d, want 404", rec.Code)
	}
}
