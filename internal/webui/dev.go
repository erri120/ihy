// Design page and UI dev-mode helpers: the Dendera component
// catalog and its SSE hot reload.
package webui

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"time"
)

// Design shows the Dendera component catalog (/ui/design).
func (s *Server) Design(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	render(w, http.StatusOK, DesignPage(t))
}

// devRefresh streams server-sent events whenever a static asset
// changes on disk (dev mode only). The design page swaps stylesheet
// URLs on the event; a dropped connection (dev server restart) makes
// it reload the page. Deletions are not detected: editing, not
// pruning, is the dev loop.
func (s *Server) devRefresh(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if s.DevStatic == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	modified := map[string]time.Time{}
	tick := time.NewTicker(600 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if scanStatic(s.DevStatic, modified) {
				fmt.Fprintf(w, "event: change\ndata: %d\n\n", time.Now().UnixMilli())
				fl.Flush()
			}
		}
	}
}

// scanStatic reports whether any file under root changed since the
// last call. The first call only seeds the baseline.
func scanStatic(root string, modified map[string]time.Time) bool {
	seed := len(modified) == 0
	changed := false
	fs.WalkDir(os.DirFS(root), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint: nil return skips unreadable entries
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		mod := info.ModTime()
		if prev, ok := modified[path]; ok && !prev.Equal(mod) {
			changed = true
		}
		modified[path] = mod
		return nil
	})
	if seed {
		return false
	}
	return changed
}
