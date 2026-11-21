package musicbrainz

import (
	"bytes"
	"io"
	"net/http"
)

// bodyBuffer is a readCloser over an in-memory body.
func bodyBuffer(b []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(b))
}

// readBody drains resp.Body and rebuilds the response with a fresh
// body so the caller still receives the full payload.
func readBody(resp *http.Response) ([]byte, error) {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body = bodyBuffer(raw)
	return raw, nil
}

// responseWithBody builds an in-memory HTTP response.
func responseWithBody(req *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		Request:    req,
		StatusCode: status,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       bodyBuffer(body),
	}
}
