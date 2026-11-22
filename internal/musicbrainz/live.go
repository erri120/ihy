// LiveTransport is the rate-limited boundary to the MusicBrainz API:
// every transport chain that can hit the network goes through it, so
// the documented pacing rule is enforced exactly where requests leave
// the process. Offline transports (replay) sit in front of it and are
// never paced.
package musicbrainz

import (
	"net/http"
	"time"
)

// DefaultRateInterval is the documented musicbrainz.org pacing: one
// request per second, on average, per IP. Self-hosted mirrors may
// allow more; configure via IHY_MB_RATE_LIMIT.
const DefaultRateInterval = time.Second

type LiveTransport struct {
	rt       http.RoundTripper
	interval time.Duration
	lastReq  time.Time
}

var _ http.RoundTripper = (*LiveTransport)(nil)

// NewLiveTransport wraps an inner transport with rate limiting. An
// interval of zero or less means one request per second.
func NewLiveTransport(rt http.RoundTripper, interval time.Duration) *LiveTransport {
	if rt == nil {
		rt = http.DefaultTransport
	}
	if interval <= 0 {
		interval = DefaultRateInterval
	}
	return &LiveTransport{rt: rt, interval: interval}
}

func (t *LiveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Pace by sleeping out the remainder since the previous request.
	if wait := time.Until(t.lastReq.Add(t.interval)); wait > 0 {
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
	t.lastReq = time.Now()
	return t.rt.RoundTrip(req)
}
