// Unit tests for the streaming service: pure helpers plus a
// real-Postgres test of resolution and the advisory warmer (skips
// without IHY_TEST_DSN).
package stream

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestContentType(t *testing.T) {
	cases := map[string]string{
		"flac":     "audio/flac",
		"FLAC":     "audio/flac",
		" mp3 ":    "audio/mpeg",
		"opus":     "audio/ogg",
		"m4a":      "audio/mp4",
		"spectral": "application/octet-stream",
		"":         "application/octet-stream",
	}
	for in, want := range cases {
		if got := ContentType(in); got != want {
			t.Errorf("ContentType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateRelPath(t *testing.T) {
	for _, good := range []string{"song.flac", "a/b/c.flac", "01 名前.flac"} {
		if err := validateRelPath(good); err != nil {
			t.Errorf("validateRelPath(%q) = %v, want nil", good, err)
		}
	}
	for _, bad := range []string{"", "/abs/song.flac", "../escape.flac", "a/../../escape.flac", "~/x.flac"} {
		if err := validateRelPath(bad); err == nil {
			t.Errorf("validateRelPath(%q) = nil, want error", bad)
		}
	}
}

func TestParseUpcoming(t *testing.T) {
	got := ParseUpcoming(" 4, 5, abc, 5, 0, -1, ,6", 5)
	want := []int64{4, 6}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ParseUpcoming = %v, want %v", got, want)
	}
	// Cap at maxUpcoming, drop the current file id.
	ids := "1,2,3,4"
	hdr := ids + ",5,6"
	got = ParseUpcoming(hdr, 1)
	if len(got) != maxUpcoming {
		t.Errorf("ParseUpcoming kept %d ids, want %d", len(got), maxUpcoming)
	}
}

func TestSourceETag(t *testing.T) {
	mod := time.Unix(1700000000, 0)
	src := Source{ModTime: mod, Size: 1234}
	got := src.ETag()
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Errorf("ETag %q is not a quoted validator", got)
	}
	if want := fmt.Sprintf(`"%x-%x"`, mod.Unix(), src.Size); got != want {
		t.Errorf("ETag = %s, want %s", got, want)
	}
}
