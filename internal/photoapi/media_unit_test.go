package photoapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// recordingReader yields zeroFill bytes of data and records the largest buffer
// io.Copy asked it to fill, so a test can prove the response body was streamed in
// fixed-size chunks rather than read into one big buffer.
type recordingReader struct {
	remaining int
	maxRead   int
}

// Read fills p with zero bytes up to the remaining length, recording the largest
// p it was handed.
func (rr *recordingReader) Read(p []byte) (int, error) {
	if len(p) > rr.maxRead {
		rr.maxRead = len(p)
	}
	if rr.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), rr.remaining)
	for i := range n {
		p[i] = 0
	}
	rr.remaining -= n
	return n, nil
}

// TestStreamMedia_streamsInChunks proves streamMedia copies the body in bounded
// chunks: even for a payload far larger than io.Copy's buffer, the source is
// never asked for the whole file in a single Read.
func TestStreamMedia_streamsInChunks(t *testing.T) {
	t.Parallel()

	const payload = 4 << 20 // 4 MiB, many times io.Copy's 32 KiB buffer
	reader := &recordingReader{remaining: payload}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/photos/x/download", nil)

	streamMedia(rec, req, reader, `"etag"`, int64(payload))

	if rec.Body.Len() != payload {
		t.Fatalf("streamed %d bytes, want %d", rec.Body.Len(), payload)
	}
	if reader.maxRead == 0 || reader.maxRead >= payload {
		t.Errorf("max single read = %d; want a small chunk well below %d (proof of streaming)", reader.maxRead, payload)
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(payload) {
		t.Errorf("Content-Length = %q, want %d", got, payload)
	}
}

// TestStreamMedia_notModified verifies a matching If-None-Match yields 304 with
// no body.
func TestStreamMedia_notModified(t *testing.T) {
	t.Parallel()

	reader := &recordingReader{remaining: 1024}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/photos/x/download", nil)
	req.Header.Set("If-None-Match", `"abc"`)

	streamMedia(rec, req, reader, `"abc"`, 1024)

	if rec.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 response carried a %d-byte body", rec.Body.Len())
	}
}

// TestStreamMedia_omitsContentLengthForUnknownSize verifies no Content-Length is
// advertised when the size is not known up front (size <= 0).
func TestStreamMedia_omitsContentLengthForUnknownSize(t *testing.T) {
	t.Parallel()

	reader := &recordingReader{remaining: 10}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/photos/x/thumb/tile_100", nil)

	streamMedia(rec, req, reader, `"e"`, 0)

	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length = %q, want empty for unknown size", got)
	}
}

// TestContentDisposition verifies the attachment header is built and sanitised.
func TestContentDisposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain name", in: "beach.jpg", want: `attachment; filename="beach.jpg"`},
		{name: "empty falls back", in: "", want: `attachment; filename="download"`},
		{name: "strips quotes and control", in: "a\"b\n.jpg", want: `attachment; filename="ab.jpg"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := contentDisposition(tt.in); got != tt.want {
				t.Errorf("contentDisposition(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestContentDisposition_noNewline is a guard that the header value is a single
// line (no header injection through the filename).
func TestContentDisposition_noNewline(t *testing.T) {
	t.Parallel()
	if got := contentDisposition("x\r\nSet-Cookie: y"); strings.ContainsAny(got, "\r\n") {
		t.Errorf("contentDisposition leaked a newline: %q", got)
	}
}
