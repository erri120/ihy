// Replay infrastructure: live MusicBrainz responses are recorded
// once, trimmed of license-restricted and irrelevant fields, stored
// in-repo, and replayed offline in tests.
//
// License (musicbrainz.org/doc/Development/JSON_Data_Dumps): most
// MusicBrainz data is CC0, but annotations, genres, ratings and user
// tags are CC BY-NC-SA 3.0. Trimming removes those fields (and other
// unused ones) so replays only carry CC0 data. See NOTICE.md in the
// replays directory.
package musicbrainz

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// trimmedFields are removed from recorded payloads: the CC BY-NC-SA
// licensed categories (tags, genres, ratings, annotations) plus
// fields the resolver never consumes (search scores, area blobs).
var trimmedFields = map[string]bool{
	"tags": true, "genres": true, "rating": true,
	"annotations": true, "annotation": true,
	"score": true, "area": true, "begin-area": true,
}

func trimFields(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	trimmed := trimValue(v)
	out, err := json.MarshalIndent(trimmed, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func trimValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k := range t {
			if trimmedFields[k] {
				delete(t, k)
			}
		}
		for k, e := range t {
			t[k] = trimValue(e)
		}
		return t
	case []any:
		for i, e := range t {
			t[i] = trimValue(e)
		}
		return t
	default:
		return v
	}
}

// replayKey is the replay manifest key for a request URL.
func replayKey(method, url string) (string, error) {
	if method != http.MethodGet {
		return "", fmt.Errorf("replays only support GET, got %s", method)
	}
	h := sha1.Sum([]byte(url))
	return fmt.Sprintf("%x", h), nil
}

// Manifest maps request URLs to replay file names.
type Manifest map[string]string

const manifestFile = "manifest.json"

// RecordingTransport fetches live, trims, and stores responses.
type RecordingTransport struct {
	dir  string
	rt   http.RoundTripper
	seen Manifest
}

// Compile-time interface assertions, per project convention.
var (
	_ http.RoundTripper = (*RecordingTransport)(nil)
	_ http.RoundTripper = (*ReplayTransport)(nil)
)

// NewRecordingTransport stores trimmed responses under dir.
func NewRecordingTransport(dir string, rt http.RoundTripper) (*RecordingTransport, error) {
	if rt == nil {
		rt = http.DefaultTransport
	}
	t := &RecordingTransport{dir: dir, rt: rt, seen: Manifest{}}
	if err := t.loadManifest(); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *RecordingTransport) loadManifest() error {
	raw, err := os.ReadFile(filepath.Join(t.dir, manifestFile))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, &t.seen)
}

func (t *RecordingTransport) saveManifest() error {
	out, err := json.MarshalIndent(t.seen, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(t.dir, manifestFile), append(out, '\n'), 0o644)
}

func (t *RecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.rt.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp, nil // only record clean 200 responses
	}
	name, err := replayKey(req.Method, req.URL.String())
	if err != nil {
		return resp, err
	}
	raw, err := readBody(resp)
	if err != nil {
		return resp, err
	}
	trimmed, err := trimFields(raw)
	if err != nil {
		return resp, err
	}
	if err := os.WriteFile(filepath.Join(t.dir, name+".json"), trimmed, 0o644); err != nil {
		return resp, err
	}
	t.seen[req.URL.String()] = name + ".json"
	if err := t.saveManifest(); err != nil {
		return resp, err
	}
	return resp, nil
}

// ReplayTransport serves previously recorded responses offline. A
// manifest miss is a test failure with instructions.
type ReplayTransport struct {
	dir      string
	manifest Manifest
}

// NewReplayTransport loads the replay manifest from dir.
func NewReplayTransport(dir string) (*ReplayTransport, error) {
	t := &ReplayTransport{dir: dir, manifest: Manifest{}}
	raw, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return nil, fmt.Errorf("read replay manifest: %w", err)
	}
	if err := json.Unmarshal(raw, &t.manifest); err != nil {
		return nil, fmt.Errorf("parse replay manifest: %w", err)
	}
	return t, nil
}

func (t *ReplayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return nil, fmt.Errorf("replays only support GET, got %s", req.Method)
	}
	name, ok := t.manifest[req.URL.String()]
	if !ok {
		return nil, fmt.Errorf(
			"replay miss: %s not recorded; set IHY_MB_RECORD=1 (live MusicBrainz) and rerun to record it",
			req.URL)
	}
	raw, err := os.ReadFile(filepath.Join(t.dir, name))
	if err != nil {
		return nil, fmt.Errorf("read replay %s: %w", name, err)
	}
	return responseWithBody(req, http.StatusOK, raw), nil
}

// copyBody and responseWithBody are small helpers shared by both
// transports to handle response bodies without losing them to the
// recorder.
