package embedding

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// readMultipartOCR parses an /ocr/image request and returns the bytes of the
// "file" part together with every scalar form field that came with it.
func readMultipartOCR(t *testing.T, r *http.Request) (data []byte, fields map[string]string) {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		t.Fatalf("unexpected content type %q: %v", r.Header.Get("Content-Type"), err)
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	fields = map[string]string{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read part body: %v", err)
		}
		if part.FormName() == "file" {
			data = body
			continue
		}
		fields[part.FormName()] = string(body)
	}
	return data, fields
}

func TestImageOCR_success(t *testing.T) {
	t.Parallel()
	const body = `{"text":"VESELICE\nPout 2026","blocks_count":2,` +
		`"blocks":[{"text":"VESELICE","bbox":[33,42,426,121],"confidence":0.99},` +
		`{"text":"Pout 2026","bbox":[33,148,460,230],"confidence":0.98}],` +
		`"lang":"latin","model":"PP-OCRv5_mobile","min_confidence":0.5}`

	var gotPath string
	var gotFile []byte
	var gotFields map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotFile, gotFields = readMultipartOCR(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	result, err := newTestClient(t, srv.URL).ImageOCR(context.Background(), strings.NewReader("image-bytes"), 0.8)
	if err != nil {
		t.Fatalf("ImageOCR: %v", err)
	}
	if gotPath != "/ocr/image" {
		t.Errorf("path = %q, want /ocr/image", gotPath)
	}
	if string(gotFile) != "image-bytes" {
		t.Errorf("file part = %q, want image-bytes", gotFile)
	}
	if gotFields["min_confidence"] != "0.8" {
		t.Errorf("min_confidence field = %q, want 0.8", gotFields["min_confidence"])
	}
	if result.Text != "VESELICE\nPout 2026" {
		t.Errorf("Text = %q", result.Text)
	}
	if result.Model != "PP-OCRv5_mobile" || result.Lang != "latin" {
		t.Errorf("Model/Lang = %q/%q", result.Model, result.Lang)
	}
	if len(result.Blocks) != 2 {
		t.Fatalf("Blocks = %d, want 2", len(result.Blocks))
	}
	if result.Blocks[0].Text != "VESELICE" || result.Blocks[0].BBox != [4]float64{33, 42, 426, 121} {
		t.Errorf("block 0 = %+v", result.Blocks[0])
	}
	if result.Blocks[1].Confidence != 0.98 {
		t.Errorf("block 1 confidence = %v, want 0.98", result.Blocks[1].Confidence)
	}
}

func TestImageOCR_omitsMinConfidenceWhenNonPositive(t *testing.T) {
	t.Parallel()
	var gotFields map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, gotFields = readMultipartOCR(t, r)
		_, _ = io.WriteString(w, `{"text":"","blocks_count":0,"blocks":[],"lang":"latin","model":"m"}`)
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL).ImageOCR(context.Background(), strings.NewReader("x"), 0); err != nil {
		t.Fatalf("ImageOCR: %v", err)
	}
	if _, ok := gotFields["min_confidence"]; ok {
		t.Errorf("min_confidence sent = %q, want it omitted so the service default applies",
			gotFields["min_confidence"])
	}
}

// A photo with no text is a normal 200 with an empty result, not an error. It is
// the common case in a family archive, and the caller must be able to record it
// as "we looked and there was nothing".
func TestImageOCR_emptyResult(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w,
			`{"text":"","blocks_count":0,"blocks":[],"lang":"latin","model":"PP-OCRv5_mobile"}`)
	}))
	defer srv.Close()

	result, err := newTestClient(t, srv.URL).ImageOCR(context.Background(), strings.NewReader("x"), 0.5)
	if err != nil {
		t.Fatalf("ImageOCR: %v", err)
	}
	if result.Text != "" || len(result.Blocks) != 0 {
		t.Errorf("result = %+v, want empty", result)
	}
	if result.Model != "PP-OCRv5_mobile" {
		t.Errorf("Model = %q, want the tag even on an empty reading", result.Model)
	}
}

// An offline box (the normal state here) must surface as the retryable
// ErrUnavailable so the job is requeued rather than dead-lettered.
func TestImageOCR_sidecarDown(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL).ImageOCR(context.Background(), strings.NewReader("x"), 0.5)
	if !errors.Is(err, ErrUnavailable) || !IsUnavailable(err) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestImageOCR_unreachable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening any more

	_, err := newTestClient(t, url).ImageOCR(context.Background(), strings.NewReader("x"), 0.5)
	if !IsUnavailable(err) {
		t.Fatalf("err = %v, want unavailable", err)
	}
}

func TestImageOCR_badResponses(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		status int
		body   string
	}{
		"unparseable body": {http.StatusOK, "not json"},
		"bad status":       {http.StatusBadRequest, `{"detail":"unsupported media type"}`},
		"malformed bbox":   {http.StatusOK, `{"text":"x","blocks":[{"text":"x","bbox":[1,2]}],"model":"m"}`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			_, err := newTestClient(t, srv.URL).ImageOCR(context.Background(), strings.NewReader("x"), 0.5)
			if !errors.Is(err, ErrBadResponse) {
				t.Fatalf("err = %v, want ErrBadResponse", err)
			}
		})
	}
}
