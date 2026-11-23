// Package transcode converts audio files on request into a
// server-managed cache of transcoded outputs, keyed by source
// mtime + profile so rescans invalidate naturally.
//
// Outputs are plain files served with http.ServeContent (Range, HEAD,
// If-Modified-Since all work), not ffmpeg stdout pipes: the first
// request of a key waits for the encode, later requests are cache
// hits with full seek support, which is what gapless clients need.
package transcode

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Profile is one server-provided transcode profile (the
// transcode_profile row). Container and codec are verbatim
// ffmpeg-style names, deliberately not a closed enum.
type Profile struct {
	ID          int64
	Name        string
	Container   string
	Codec       string
	BitrateKbps int
}

// Encoder is the injected encode step; tests use a fake, production
// the FFmpeg implementation.
type Encoder interface {
	// Encode writes the transcoded output to outPath. Errors are
	// wrapped with the profile name and the ffmpeg output tail.
	Encode(ctx context.Context, srcPath string, p Profile, outPath string) error
}

// Compile-time interface assertion, per project convention.
var _ Encoder = (*FFmpeg)(nil)

// FFmpeg shells out to the ffmpeg binary (IHY_FFMPEG_PATH).
type FFmpeg struct {
	Bin string
}

// Check verifies the binary exists and runs; called at startup so a
// missing ffmpeg is a boot failure, not a per-request one.
func (f FFmpeg) Check(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, f.Bin, "-version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg %q unusable: %v: %s", f.Bin, err, firstLine(out))
	}
	return nil
}

// Encode runs one transcode. Context cancellation (client disconnect,
// superseded warm job) kills the process.
func (f FFmpeg) Encode(ctx context.Context, srcPath string, p Profile, outPath string) error {
	cmd := exec.CommandContext(ctx, f.Bin,
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", srcPath,
		"-map", "0:a:0",
		"-c:a", p.Codec,
		"-b:a", strconv.Itoa(p.BitrateKbps)+"k",
		"-f", p.Container,
		outPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg %s->%s/%s %dk: %v: %s",
			srcPath, p.Container, p.Codec, p.BitrateKbps, err, firstLine(out))
	}
	return nil
}

func firstLine(out []byte) string {
	s := strings.TrimSpace(string(out))
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	return s
}
