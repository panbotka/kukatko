package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// makeVec returns a slice of n incrementing float32 values for assertions.
func makeVec(n int) []float32 {
	v := make([]float32, n)
	for i := range v {
		v[i] = float32(i) + 0.5
	}
	return v
}

// readMultipartFile parses a multipart request and returns the bytes of the
// "file" field along with its declared content type.
func readMultipartFile(t *testing.T, r *http.Request) (data []byte, contentType string) {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		t.Fatalf("unexpected content type %q: %v", r.Header.Get("Content-Type"), err)
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		t.Fatalf("read part: %v", err)
	}
	if part.FormName() != "file" {
		t.Fatalf("form name = %q, want file", part.FormName())
	}
	body, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("read part body: %v", err)
	}
	return body, part.Header.Get("Content-Type")
}

// newTestClient builds an HTTPClient pointed at srv with fast timeouts.
func newTestClient(t *testing.T, baseURL string) *HTTPClient {
	t.Helper()
	c, err := New(Config{
		BaseURL:        baseURL,
		ImageDim:       4,
		FaceDim:        3,
		RequestTimeout: 2 * time.Second,
		HealthTimeout:  500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNew_validation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		baseURL string
		wantErr error
	}{
		{"valid http", "http://box:8000", nil},
		{"valid https trailing slash", "https://box:8000/", nil},
		{"missing scheme", "box:8000", ErrInvalidURL},
		{"ftp scheme", "ftp://box", ErrInvalidURL},
		{"empty", "", ErrInvalidURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := New(Config{BaseURL: tt.baseURL})
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("New(%q) err = %v, want %v", tt.baseURL, err, tt.wantErr)
			}
		})
	}
}

func TestNew_defaults(t *testing.T) {
	t.Parallel()
	c, err := New(Config{BaseURL: "http://box:8000"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.imageDim != DefaultImageDim || c.faceDim != DefaultFaceDim {
		t.Errorf("dims = %d/%d, want %d/%d", c.imageDim, c.faceDim, DefaultImageDim, DefaultFaceDim)
	}
	if c.requestTimeout != DefaultRequestTimeout || c.healthTimeout != DefaultHealthTimeout {
		t.Errorf("timeouts = %v/%v", c.requestTimeout, c.healthTimeout)
	}
	if c.healthPath != DefaultHealthPath {
		t.Errorf("healthPath = %q, want %q", c.healthPath, DefaultHealthPath)
	}
}

func TestImageEmbedding_success(t *testing.T) {
	t.Parallel()
	want := makeVec(4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != endpointImage || r.Method != http.MethodPost {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		data, ct := readMultipartFile(t, r)
		if string(data) != "JPEGDATA" {
			t.Errorf("file data = %q", data)
		}
		if ct == "" {
			t.Errorf("missing part content type")
		}
		_ = json.NewEncoder(w).Encode(embeddingResponse{
			Dim: 4, Embedding: want, Model: "clip", Pretrained: "ViT-L-14",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	got, model, pretrained, err := c.ImageEmbedding(context.Background(), strings.NewReader("JPEGDATA"))
	if err != nil {
		t.Fatalf("ImageEmbedding: %v", err)
	}
	if len(got) != 4 || model != "clip" || pretrained != "ViT-L-14" {
		t.Errorf("got %v model=%q pretrained=%q", got, model, pretrained)
	}
}

func TestTextEmbedding_success(t *testing.T) {
	t.Parallel()
	want := makeVec(4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != endpointText {
			t.Errorf("path = %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Text != "a cat" {
			t.Errorf("text = %q", body.Text)
		}
		_ = json.NewEncoder(w).Encode(embeddingResponse{Dim: 4, Embedding: want, Model: "clip"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	got, _, _, err := c.TextEmbedding(context.Background(), "a cat")
	if err != nil {
		t.Fatalf("TextEmbedding: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("len = %d, want 4", len(got))
	}
}

func TestFaceEmbeddings_success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != endpointFace {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = readMultipartFile(t, r)
		_ = json.NewEncoder(w).Encode(faceEnvelope{
			FacesCount: 1,
			Model:      "arcface",
			Faces: []faceItem{{
				FaceIndex: 0, Dim: 3, Embedding: makeVec(3),
				BBox: []float64{10, 20, 110, 220}, DetScore: 0.97,
			}},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	faces, model, err := c.FaceEmbeddings(context.Background(), strings.NewReader("img"))
	if err != nil {
		t.Fatalf("FaceEmbeddings: %v", err)
	}
	if model != "arcface" || len(faces) != 1 {
		t.Fatalf("model=%q faces=%d", model, len(faces))
	}
	f := faces[0]
	if f.Index != 0 || len(f.Embedding) != 3 || f.DetScore != 0.97 {
		t.Errorf("face = %+v", f)
	}
	if f.BBox != [4]float64{10, 20, 110, 220} {
		t.Errorf("bbox = %v", f.BBox)
	}
}

func TestEmbedding_dimMismatch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(embeddingResponse{Dim: 2, Embedding: makeVec(2)})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, _, _, err := c.ImageEmbedding(context.Background(), strings.NewReader("x"))
	if !errors.Is(err, ErrDimMismatch) {
		t.Errorf("err = %v, want ErrDimMismatch", err)
	}
}

func TestFaceEmbeddings_dimMismatch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(faceEnvelope{
			Faces: []faceItem{{FaceIndex: 0, Embedding: makeVec(99), BBox: []float64{0, 0, 1, 1}}},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, _, err := c.FaceEmbeddings(context.Background(), strings.NewReader("x"))
	if !errors.Is(err, ErrDimMismatch) {
		t.Errorf("err = %v, want ErrDimMismatch", err)
	}
}

func TestFaceEmbeddings_badBBox(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(faceEnvelope{
			Faces: []faceItem{{FaceIndex: 0, Embedding: makeVec(3), BBox: []float64{0, 0, 1}}},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, _, err := c.FaceEmbeddings(context.Background(), strings.NewReader("x"))
	if !errors.Is(err, ErrBadResponse) {
		t.Errorf("err = %v, want ErrBadResponse", err)
	}
}

func TestEmbedding_emptyVector(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(embeddingResponse{Dim: 0, Embedding: []float32{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, _, _, err := c.ImageEmbedding(context.Background(), strings.NewReader("x"))
	if !errors.Is(err, ErrBadResponse) {
		t.Errorf("err = %v, want ErrBadResponse", err)
	}
}

func TestEmbedding_malformedJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, _, _, err := c.TextEmbedding(context.Background(), "x")
	if !errors.Is(err, ErrBadResponse) {
		t.Errorf("err = %v, want ErrBadResponse", err)
	}
}

func TestDo_statusClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		wantErr error
	}{
		{"bad gateway", http.StatusBadGateway, ErrUnavailable},
		{"service unavailable", http.StatusServiceUnavailable, ErrUnavailable},
		{"gateway timeout", http.StatusGatewayTimeout, ErrUnavailable},
		{"bad request", http.StatusBadRequest, ErrBadResponse},
		{"internal error", http.StatusInternalServerError, ErrBadResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("boom"))
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL)
			_, _, _, err := c.ImageEmbedding(context.Background(), strings.NewReader("x"))
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("status %d: err = %v, want %v", tt.status, err, tt.wantErr)
			}
			if errors.Is(err, ErrUnavailable) != IsUnavailable(err) {
				t.Errorf("IsUnavailable disagrees with errors.Is for %v", err)
			}
		})
	}
}

func TestImageEmbedding_offline(t *testing.T) {
	t.Parallel()
	// Point at a closed server to force a transport (connection refused) error.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := newTestClient(t, url)
	_, _, _, err := c.ImageEmbedding(context.Background(), strings.NewReader("x"))
	if !IsUnavailable(err) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestImageEmbedding_timeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_ = json.NewEncoder(w).Encode(embeddingResponse{Dim: 4, Embedding: makeVec(4)})
	}))
	defer srv.Close()
	defer close(release)

	c, err := New(Config{BaseURL: srv.URL, ImageDim: 4, RequestTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, _, err = c.ImageEmbedding(context.Background(), strings.NewReader("x"))
	if !IsUnavailable(err) {
		t.Errorf("err = %v, want ErrUnavailable (timeout)", err)
	}
}

func TestImageEmbedding_contextCanceled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(embeddingResponse{Dim: 4, Embedding: makeVec(4)})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := c.ImageEmbedding(ctx, strings.NewReader("x"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if IsUnavailable(err) {
		t.Errorf("canceled context should not classify as unavailable: %v", err)
	}
}

func TestHealthy(t *testing.T) {
	t.Parallel()
	t.Run("reachable", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != DefaultHealthPath {
				t.Errorf("path = %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		if !newTestClient(t, srv.URL).Healthy(context.Background()) {
			t.Error("Healthy = false, want true")
		}
	})

	t.Run("reachable even on 404", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		if !newTestClient(t, srv.URL).Healthy(context.Background()) {
			t.Error("Healthy = false, want true (any HTTP response = reachable)")
		}
	})

	t.Run("offline", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()
		if newTestClient(t, url).Healthy(context.Background()) {
			t.Error("Healthy = true, want false")
		}
	})
}

// staticClient is a trivial Client implementation proving the interface is
// fakeable without any network.
type staticClient struct{}

func (staticClient) ImageEmbedding(context.Context, io.Reader) ([]float32, string, string, error) {
	return makeVec(768), "fake", "fake", nil
}
func (staticClient) TextEmbedding(context.Context, string) ([]float32, string, string, error) {
	return makeVec(768), "fake", "fake", nil
}
func (staticClient) FaceEmbeddings(context.Context, io.Reader) ([]Face, string, error) {
	return nil, "fake", nil
}
func (staticClient) Healthy(context.Context) bool { return true }

func TestClient_fakeable(t *testing.T) {
	t.Parallel()
	var c Client = staticClient{}
	if !c.Healthy(context.Background()) {
		t.Error("fake Healthy = false")
	}
	v, _, _, err := c.ImageEmbedding(context.Background(), strings.NewReader("x"))
	if err != nil || len(v) != 768 {
		t.Errorf("fake ImageEmbedding = %d, %v", len(v), err)
	}
}
