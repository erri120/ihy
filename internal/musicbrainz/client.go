// Package musicbrainz provides a thin client for the MusicBrainz
// WS/2 JSON API with rate limiting, retries, and a replay transport
// for tests: live responses are recorded once into trimmed replay
// files in-repo and tests run offline against them.
package musicbrainz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Entity types used by the resolver.
const (
	EntityArtist       = "artist"
	EntityReleaseGroup = "release-group"
	EntityRelease      = "release"
	EntityRecording    = "recording"
	EntityWork         = "work"
)

// Fetcher is the metadata interface the resolver consumes.
type Fetcher interface {
	Artist(ctx context.Context, mbid MBID) (Artist, error)
	ReleaseGroup(ctx context.Context, mbid MBID) (ReleaseGroup, error)
	Release(ctx context.Context, mbid MBID) (Release, error)
	Recording(ctx context.Context, mbid MBID) (Recording, error)
	Work(ctx context.Context, mbid MBID) (Work, error)
}

// DefaultBaseURL is the public MusicBrainz server; override with
// MB_BASE_URL for a mirror.
const DefaultBaseURL = "https://musicbrainz.org"

// Client talks to MusicBrainz WS/2, enforcing the documented 1 req/s
// limit and a proper User-Agent, retrying transient 5xx responses.
type Client struct {
	base    string
	ua      string
	http    *http.Client
	lastReq time.Time
}

// NewClient builds a client. transport may be nil (default transport)
// or a replay transport for tests.
func NewClient(baseURL, userAgent string, transport http.RoundTripper) *Client {
	return &Client{
		base: strings.TrimSuffix(baseURL, "/"),
		ua:   userAgent,
		http: &http.Client{Transport: transport},
	}
}

// UserAgent builds the User-Agent MusicBrainz expects: app name and
// version plus a contact address.
func UserAgent(version, contact string) string {
	return fmt.Sprintf("ihy/%s (contact: %s)", version, contact)
}

// ErrNotFound is returned for 404 responses (unknown or stale MBIDs).
var ErrNotFound = fmt.Errorf("musicbrainz: entity not found")

// get fetches a path (starting with /ws/2/) and returns the raw body.
// Rate limiting is skipped for replay transports: tests run offline
// against recorded files and must not stall per request.
func (c *Client) get(ctx context.Context, path string) (json.RawMessage, error) {
	if _, isReplay := c.http.Transport.(*ReplayTransport); !isReplay {
		// 1 req/s: sleep out the remainder since the previous request.
		if wait := time.Until(c.lastReq.Add(time.Second)); wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	c.lastReq = time.Now()

	url := c.base + path
	var body json.RawMessage
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * time.Second):
			}
		}
		body, err = c.attempt(ctx, url)
		if err == nil || err == ErrNotFound {
			return body, err
		}
	}
	return nil, err
}

func (c *Client) attempt(ctx context.Context, url string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusOK:
		return raw, nil
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("musicbrainz: %s %s (transient)", resp.Status, url)
	default:
		return nil, fmt.Errorf("musicbrainz: %s %s", resp.Status, url)
	}
}

// inc paths per entity type; the inc set stays deliberately free of
// tags/genres/ratings/annotations, which MusicBrainz licenses under
// CC BY-NC-SA and which we never consume.
const (
	incArtist    = "aliases"
	incRG        = "artist-credits+aliases"
	incRelease   = "artist-credits+labels+release-groups"
	incRecording = "artist-credits+work-rels+artist-rels+aliases"
	incWork      = "aliases+artist-rels" // composer/lyricist live on the work
)

// Artist looks up an artist entity.
func (c *Client) Artist(ctx context.Context, mbid MBID) (Artist, error) {
	var out Artist
	return out, lookup(ctx, c, EntityArtist, mbid, incArtist, &out)
}

// ReleaseGroup looks up a release-group entity.
func (c *Client) ReleaseGroup(ctx context.Context, mbid MBID) (ReleaseGroup, error) {
	var out ReleaseGroup
	return out, lookup(ctx, c, EntityReleaseGroup, mbid, incRG, &out)
}

// Release looks up a release entity.
func (c *Client) Release(ctx context.Context, mbid MBID) (Release, error) {
	var out Release
	return out, lookup(ctx, c, EntityRelease, mbid, incRelease, &out)
}

// Recording looks up a recording entity.
func (c *Client) Recording(ctx context.Context, mbid MBID) (Recording, error) {
	var out Recording
	return out, lookup(ctx, c, EntityRecording, mbid, incRecording, &out)
}

// Work looks up a work entity.
func (c *Client) Work(ctx context.Context, mbid MBID) (Work, error) {
	var out Work
	return out, lookup(ctx, c, EntityWork, mbid, incWork, &out)
}

// lookup performs an entity lookup after validating the MBID format:
// malformed IDs would only waste a request and come back as a 400.
func lookup(ctx context.Context, c *Client, entityType string, mbid MBID, inc string, out any) error {
	if !validMBID(mbid) {
		return fmt.Errorf("musicbrainz: invalid MBID %q", mbid)
	}
	path := fmt.Sprintf("/ws/2/%s/%s?inc=%s&fmt=json", entityType, mbid, inc)
	raw, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// validMBID checks the UUID form MusicBrainz lookups require.
func validMBID(mbid MBID) bool {
	s := string(mbid)
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}
