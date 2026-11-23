// The design catalog is development tooling: registered only in dev
// mode and served without auth (like the static assets).
package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/text/language"
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

// The hot reload hookup lives in the shared layout so every page
// gets it, not just the design catalog. In dev, one guarded
// EventSource; outside dev, none.
func TestLayoutDevHotReload(t *testing.T) {
	devT := &T{Lang: language.English, Dev: true}
	dev := renderComponent(t, Layout(devT, "Test", ""))
	if !strings.Contains(dev, "EventSource('/ui/dev/refresh')") || !strings.Contains(dev, "__ihyHotReload") {
		t.Error("dev layout must carry the guarded hot reload hookup")
	}
	prod := renderComponent(t, Layout(&T{Lang: language.English}, "Test", ""))
	if strings.Contains(prod, "EventSource") {
		t.Error("non-dev layout must not carry the hot reload hookup")
	}
}

// The shell composes the nav outside the content swap region and pins
// boosted navigation to #content, so chrome persists across pages.
// Shell-less pages (login, catalog) render no nav at all.
func TestLayoutShell(t *testing.T) {
	tt := NewTranslator("en")
	shelled := renderComponent(t, Layout(tt, "Test", "library"))
	for _, want := range []string{`class="with-shell"`, `hx-boost="true"`, `id="shell-nav"`, `hx-preserve="true"`} {
		if !strings.Contains(shelled, want) {
			t.Errorf("shelled layout missing %s", want)
		}
	}
	bare := renderComponent(t, Layout(tt, "Test", ""))
	if strings.Contains(bare, "shell-nav") || strings.Contains(bare, "with-shell") {
		t.Error("shell-less layout (login, catalog) must not render the nav")
	}
}
