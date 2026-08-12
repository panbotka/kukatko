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
	"slices"
	"strings"
	"sync"
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
	if c.textTimeout != DefaultTextTimeout {
		t.Errorf("textTimeout = %v, want %v", c.textTimeout, DefaultTextTimeout)
	}
	if c.healthPath != DefaultHealthPath {
		t.Errorf("healthPath = %q, want %q", c.healthPath, DefaultHealthPath)
	}
}

func TestNew_boundedDialer(t *testing.T) {
	t.Parallel()
	c, err := New(Config{BaseURL: "http://box:8000"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The stock http.DefaultTransport waits 30 s on a dial the offline box never
	// answers — long enough to make an interactive search look broken — so the
	// client must carry a transport of its own rather than sharing that one.
	transport, ok := c.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.client.Transport)
	}
	if transport == http.DefaultTransport {
		t.Error("client shares http.DefaultTransport, so its 30 s dial timeout applies")
	}
	if c.client.Timeout != 0 {
		t.Errorf("client.Timeout = %v, want 0 (deadlines come from the context)", c.client.Timeout)
	}
}

func TestNew_customHTTPClientWins(t *testing.T) {
	t.Parallel()
	own := &http.Client{}
	c, err := New(Config{BaseURL: "http://box:8000", HTTPClient: own, DialTimeout: time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// An injected client owns its transport; we must not rebuild it underneath.
	if c.client != own {
		t.Error("New replaced the injected HTTPClient")
	}
}

func TestTextEmbedding_timeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_ = json.NewEncoder(w).Encode(embeddingResponse{Dim: 4, Embedding: makeVec(4)})
	}))
	defer srv.Close()
	defer close(release)

	// A generous RequestTimeout must not apply here: the query embedding serves an
	// interactive search and is bounded by the much shorter TextTimeout, so the
	// search can fall back to full-text instead of blocking on a stalled box.
	c, err := New(Config{
		BaseURL:        srv.URL,
		ImageDim:       4,
		RequestTimeout: time.Minute,
		TextTimeout:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	start := time.Now()
	_, _, _, err = c.TextEmbedding(context.Background(), "beach")
	if !IsUnavailable(err) {
		t.Errorf("err = %v, want ErrUnavailable (timeout)", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("TextEmbedding waited %v, want the text timeout to cut it short", elapsed)
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

// routeRecorder is a stand-in sidecar instance that records the paths it was
// asked for and answers every route the client knows with a well-formed body, so
// a test can tell which of two instances a call actually reached.
type routeRecorder struct {
	url string

	mu    sync.Mutex
	paths []string
}

// newRouteRecorder starts a recording sidecar and stops it when the test ends.
func newRouteRecorder(t *testing.T) *routeRecorder {
	t.Helper()
	rec := &routeRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.paths = append(rec.paths, r.URL.Path)
		rec.mu.Unlock()
		switch r.URL.Path {
		case endpointFace:
			_ = json.NewEncoder(w).Encode(faceEnvelope{Model: "arcface"})
		case endpointOCR:
			_ = json.NewEncoder(w).Encode(ocrEnvelope{Model: "ocr"})
		default:
			_ = json.NewEncoder(w).Encode(embeddingResponse{Dim: 4, Embedding: makeVec(4), Model: "siglip"})
		}
	}))
	t.Cleanup(srv.Close)
	rec.url = srv.URL
	return rec
}

// seen returns the paths recorded so far, in order.
func (r *routeRecorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.paths)
}

// callEveryEndpoint exercises each route of the client in a fixed order, failing
// the test on any error so a routing assertion never reads an empty recorder.
func callEveryEndpoint(t *testing.T, c *HTTPClient) {
	t.Helper()
	ctx := context.Background()
	if _, _, _, err := c.TextEmbedding(ctx, "a cat"); err != nil {
		t.Fatalf("TextEmbedding: %v", err)
	}
	if _, _, _, err := c.ImageEmbedding(ctx, strings.NewReader("JPEGDATA")); err != nil {
		t.Fatalf("ImageEmbedding: %v", err)
	}
	if _, _, err := c.FaceEmbeddings(ctx, strings.NewReader("JPEGDATA")); err != nil {
		t.Fatalf("FaceEmbeddings: %v", err)
	}
	if _, err := c.ImageOCR(ctx, strings.NewReader("JPEGDATA"), 0); err != nil {
		t.Fatalf("ImageOCR: %v", err)
	}
	if !c.Healthy(ctx) {
		t.Fatal("Healthy = false, want true")
	}
}

// TestClient_textBaseURLRoutesOnlyText pins the split a second, always-on text
// instance depends on: the search query goes there, everything else stays on the
// box. The health probe is the load-bearing half — internal/wake reads it to
// decide whether to send a magic packet, so a green light from the text instance
// would leave the box asleep and the embedding queue stuck behind it forever.
func TestClient_textBaseURLRoutesOnlyText(t *testing.T) {
	t.Parallel()
	box := newRouteRecorder(t)
	text := newRouteRecorder(t)
	c, err := New(Config{
		BaseURL:        box.url,
		TextBaseURL:    text.url,
		ImageDim:       4,
		FaceDim:        3,
		RequestTimeout: 2 * time.Second,
		HealthTimeout:  500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	callEveryEndpoint(t, c)

	wantText := []string{endpointText}
	if got := text.seen(); !slices.Equal(got, wantText) {
		t.Errorf("text instance saw %v, want %v", got, wantText)
	}
	wantBox := []string{endpointImage, endpointFace, endpointOCR, DefaultHealthPath}
	if got := box.seen(); !slices.Equal(got, wantBox) {
		t.Errorf("box saw %v, want %v", got, wantBox)
	}
}

// TestClient_textFallsBackToBaseURL covers the state every deployment starts in:
// no text URL configured means one host answers everything, exactly as before.
func TestClient_textFallsBackToBaseURL(t *testing.T) {
	t.Parallel()
	box := newRouteRecorder(t)
	c := newTestClient(t, box.url)
	if c.textURL != c.baseURL {
		t.Errorf("textURL = %v, want the base URL %v", c.textURL, c.baseURL)
	}

	callEveryEndpoint(t, c)

	want := []string{endpointText, endpointImage, endpointFace, endpointOCR, DefaultHealthPath}
	if got := box.seen(); !slices.Equal(got, want) {
		t.Errorf("box saw %v, want %v", got, want)
	}
}

func TestNew_textBaseURLValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		textURL  string
		wantHost string
		wantErr  error
	}{
		{"empty falls back to the base host", "", "box:8000", nil},
		{"valid http", "http://embeddings-text:8000", "embeddings-text:8000", nil},
		{"valid https trailing slash", "https://text.example/", "text.example", nil},
		{"missing scheme", "embeddings-text:8000", "", ErrInvalidURL},
		{"missing host", "http://", "", ErrInvalidURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := New(Config{BaseURL: "http://box:8000", TextBaseURL: tt.textURL})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New(text=%q) err = %v, want %v", tt.textURL, err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got := c.textURL.Host; got != tt.wantHost {
				t.Errorf("text host = %q, want %q", got, tt.wantHost)
			}
			if c.baseURL.Host != "box:8000" {
				t.Errorf("base host = %q, want box:8000 (text URL must not move it)", c.baseURL.Host)
			}
		})
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

// healthBody is the sidecar's GET /health envelope as the tests serve it: the
// image-tower block the client parses plus a sibling key it must ignore.
const healthBody = `{"status":"ok",` +
	`"clip":{"model":"ViT-SO400M-14-SigLIP2-378","pretrained":"webli","dim":1152,"precision":"fp16"},` +
	`"face":{"model":"buffalo_l","dim":512}}`

// serveHealth starts a test server answering the health path with body under
// status, and returns a client pointed at it.
func serveHealth(t *testing.T, status int, body string) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != DefaultHealthPath {
			t.Errorf("path = %s, want %s", r.URL.Path, DefaultHealthPath)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return newTestClient(t, srv.URL)
}

func TestHealth_reportsImageTower(t *testing.T) {
	t.Parallel()
	got, err := serveHealth(t, http.StatusOK, healthBody).Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	want := SidecarHealth{
		Model: "ViT-SO400M-14-SigLIP2-378", Pretrained: "webli", Dim: 1152, Precision: "fp16",
	}
	if got != want {
		t.Errorf("Health = %+v, want %+v", got, want)
	}
}

// TestHealth_missingClipBlock covers an older sidecar that publishes no image
// dimension: the call succeeds and Dim stays zero, so the caller reads "unknown"
// rather than "mismatch" and does not warn about a model it cannot see.
func TestHealth_missingClipBlock(t *testing.T) {
	t.Parallel()
	got, err := serveHealth(t, http.StatusOK, `{"status":"ok"}`).Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got.Dim != 0 || got.Model != "" {
		t.Errorf("Health = %+v, want the zero SidecarHealth", got)
	}
}

func TestHealth_errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"malformed json", http.StatusOK, "not json", ErrBadResponse},
		{"gateway status", http.StatusServiceUnavailable, "", ErrUnavailable},
		{"unexpected status", http.StatusNotFound, "", ErrBadResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := serveHealth(t, tt.status, tt.body).Health(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Health error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestHealth_offline pins the classification the startup check leans on: an
// unreachable box — the normal state here — is ErrUnavailable, not a mismatch.
func TestHealth_offline(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if _, err := newTestClient(t, url).Health(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Health error = %v, want ErrUnavailable", err)
	}
}

// staticClient is a trivial Client implementation proving the interface is
// fakeable without any network.
type staticClient struct{}

func (staticClient) ImageEmbedding(context.Context, io.Reader) ([]float32, string, string, error) {
	return makeVec(DefaultImageDim), "fake", "fake", nil
}
func (staticClient) TextEmbedding(context.Context, string) ([]float32, string, string, error) {
	return makeVec(DefaultImageDim), "fake", "fake", nil
}
func (staticClient) FaceEmbeddings(context.Context, io.Reader) ([]Face, string, error) {
	return nil, "fake", nil
}
func (staticClient) ImageOCR(context.Context, io.Reader, float64) (OCRResult, error) {
	return OCRResult{Model: "fake"}, nil
}
func (staticClient) Healthy(context.Context) bool { return true }

func TestClient_fakeable(t *testing.T) {
	t.Parallel()
	var c Client = staticClient{}
	if !c.Healthy(context.Background()) {
		t.Error("fake Healthy = false")
	}
	v, _, _, err := c.ImageEmbedding(context.Background(), strings.NewReader("x"))
	if err != nil || len(v) != DefaultImageDim {
		t.Errorf("fake ImageEmbedding = %d, %v", len(v), err)
	}
}
