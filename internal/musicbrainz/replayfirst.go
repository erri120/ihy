// Replay-first transport: serve recorded URLs offline, record and
// store misses live. Lets the recording driver re-traverse recorded
// entities (relation recursion) without touching the API for them.
package musicbrainz

import "net/http"

// ReplayFirstTransport composes a replay transport (read-only view of
// the manifest) with a recording transport (live + store). A request
// present in the manifest is served from the replay; anything else is
// fetched live and recorded.
type ReplayFirstTransport struct {
	replay *ReplayTransport
	record *RecordingTransport
}

var _ http.RoundTripper = (*ReplayFirstTransport)(nil)

func NewReplayFirstTransport(replay *ReplayTransport, record *RecordingTransport) *ReplayFirstTransport {
	return &ReplayFirstTransport{replay: replay, record: record}
}

func (t *ReplayFirstTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet {
		// Manifest keys are host-independent request URIs.
		if _, ok := t.replay.manifest[req.URL.RequestURI()]; ok {
			return t.replay.RoundTrip(req)
		}
	}
	return t.record.RoundTrip(req)
}
