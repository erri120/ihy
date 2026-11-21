// LiveTransport is the rate-limited boundary to the MusicBrainz API:
// every transport chain that can hit the network goes through it, so
// the documented 1 req/s rule is enforced exactly where requests
// leave the process. Offline transports (replay) sit in front of it
// and are never paced.
package musicbrainz

import (
	"net/http"
	"time"
)

type LiveTransport struct {
	rt      http.RoundTripper
	lastReq time.Time
}

var _ http.RoundTripper = (*LiveTransport)(nil)

// NewLiveTransport wraps an inner transport with 1 req/s pacing.
func NewLiveTransport(rt http.RoundTripper) *LiveTransport {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &LiveTransport{rt: rt}
}

func (t *LiveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 1 req/s: sleep out the remainder since the previous request.
	if wait := time.Until(t.lastReq.Add(time.Second)); wait > 0 {
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
	t.lastReq = time.Now()
	return t.rt.RoundTrip(req)
}
