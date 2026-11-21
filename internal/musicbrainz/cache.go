// Package musicbrainz adds a DB-backed caching transport so repeated
// lookups never re-hit the API: good citizenship and fast rescans.
package musicbrainz

import (
	"crypto/sha1"
	"database/sql"
	"fmt"
	"net/http"
)

// CachingTransport consults the mb_cache table for GET requests and
// stores fresh 200 responses. It composes over any inner transport
// (live or replay) and keeps the URL-hash keying the schema defines.
type CachingTransport struct {
	db *sql.DB
	rt http.RoundTripper
}

var _ http.RoundTripper = (*CachingTransport)(nil)

func NewCachingTransport(db *sql.DB, rt http.RoundTripper) *CachingTransport {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &CachingTransport{db: db, rt: rt}
}

func cacheKey(url string) []byte {
	h := sha1.Sum([]byte(url))
	return h[:]
}

func (t *CachingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return nil, fmt.Errorf("cache transport supports GET only, got %s", req.Method)
	}
	key := cacheKey(req.URL.String())

	var cached []byte
	err := t.db.QueryRowContext(req.Context(),
		`SELECT body FROM mb_cache WHERE url_hash = $1`, key).Scan(&cached)
	switch {
	case err == nil:
		return responseWithBody(req, http.StatusOK, cached), nil
	case err != sql.ErrNoRows:
		return nil, fmt.Errorf("read mb_cache: %w", err)
	}

	resp, err := t.rt.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp, nil // don't cache failures
	}
	raw, err := readBody(resp)
	if err != nil {
		return resp, err
	}
	_, err = t.db.ExecContext(req.Context(), `
		INSERT INTO mb_cache (url_hash, url, body, fetched_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (url_hash) DO UPDATE SET body = EXCLUDED.body, fetched_at = now()`,
		key, req.URL.String(), raw)
	if err != nil {
		return resp, fmt.Errorf("write mb_cache: %w", err)
	}
	return resp, nil
}
