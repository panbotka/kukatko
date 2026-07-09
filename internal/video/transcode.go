package video

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// webFriendlyCodecs is the set of video codecs that play natively in modern
// browsers' HTML5 <video> element, so they are streamed as-is rather than
// transcoded. Everything else (notably HEVC/H.265, MPEG-4 ASP, ProRes, …) is a
// transcode candidate when on-the-fly transcoding is enabled.
var webFriendlyCodecs = map[string]struct{}{
	"h264":   {},
	"avc":    {},
	"avc1":   {},
	"vp8":    {},
	"vp9":    {},
	"av1":    {},
	"theora": {},
}

// IsWebFriendlyCodec reports whether codec names a video codec that plays
// natively in browsers. The comparison is case-insensitive. An empty codec is
// treated as unknown (not web-friendly) so the caller does not transcode a video
// whose codec could not be probed.
func IsWebFriendlyCodec(codec string) bool {
	if codec == "" {
		return false
	}
	_, ok := webFriendlyCodecs[strings.ToLower(strings.TrimSpace(codec))]
	return ok
}

// TranscodeArgs builds the ffmpeg argument list that remuxes/transcodes the
// video at src into a fragmented H.264/AAC MP4 written to stdout (pipe:1). src is
// either a local path or an http(s) URL: ffmpeg opens both, so a video living in
// a remote bucket is transcoded straight from its signed URL rather than
// downloaded first. The fragmented flags (frag_keyframe+empty_moov) make the MP4
// streamable to a pipe without seeking back to write the moov atom, so the output
// can be copied straight to an HTTP response. Audio is mapped optionally (0:a?)
// so silent clips still transcode. It is a standalone function so the command
// construction can be unit-tested without executing ffmpeg.
func TranscodeArgs(src string) []string {
	return []string{
		"-nostdin",
		"-i", src,
		"-map", "0:v:0",
		"-map", "0:a?",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "23",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4",
		"pipe:1",
	}
}

// TranscodeStream is a running ffmpeg transcode whose H.264/MP4 output is read
// from Read. The caller MUST Close it to terminate ffmpeg and reap the process,
// even if Read returned an error or io.EOF.
type TranscodeStream struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	cancel context.CancelFunc
}

// Read pulls the next chunk of transcoded MP4 bytes from ffmpeg's stdout.
func (s *TranscodeStream) Read(p []byte) (int, error) {
	return s.stdout.Read(p) //nolint:wrapcheck // must pass io.EOF through verbatim.
}

// Close terminates the ffmpeg process (cancelling its context, which sends a
// kill signal) and waits for it to exit so no zombie is left behind. The wait
// error is intentionally discarded because terminating ffmpeg mid-stream is the
// normal shutdown path and surfaces as a non-nil (killed) status.
func (s *TranscodeStream) Close() error {
	s.cancel()
	_ = s.stdout.Close()
	_ = s.cmd.Wait()
	return nil
}

// Transcode starts an ffmpeg process that transcodes the video at src — a local
// path or an http(s) URL — to a streamable H.264/MP4 and returns a
// TranscodeStream the caller reads and then closes. The transcode runs for as
// long as the returned stream is read; closing it (or cancelling ctx) terminates
// ffmpeg. It returns an error wrapping ErrFFmpegMissing when ffmpeg is not
// installed, so the caller can fall back to serving the original.
func Transcode(ctx context.Context, src string) (*TranscodeStream, error) {
	if _, err := exec.LookPath(ffmpegBinary); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFFmpegMissing, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	// #nosec G204 -- src is either the stored original materialized by the storage
	// layer and confined to its root, or a URL that layer signed; the remaining
	// arguments are constant flags.
	cmd := exec.CommandContext(cctx, ffmpegBinary, TranscodeArgs(src)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("video: ffmpeg stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("video: starting ffmpeg transcode: %w", err)
	}
	return &TranscodeStream{cmd: cmd, stdout: stdout, cancel: cancel}, nil
}
