package psfeeds

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient points a real HTTPClient at srv with tiny backoff so a 429 retry
// test does not sleep for real seconds.
func newTestClient(t *testing.T, baseURL string) *HTTPClient {
	t.Helper()
	client, err := New(Config{
		BaseURL:    baseURL,
		Token:      "psat_test",
		RetryDelay: time.Millisecond,
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestNew_invalidURL(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, url string }{
		{"empty", ""},
		{"no scheme", "sorter.example"},
		{"ftp scheme", "ftp://sorter.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(Config{BaseURL: tt.url}); !errors.Is(err, ErrInvalidURL) {
				t.Errorf("New(%q) error = %v, want ErrInvalidURL", tt.url, err)
			}
		})
	}
}

func TestListEmbeddings_decodesAndSendsAuth(t *testing.T) {
	t.Parallel()
	var gotAuth, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		next := "p1"
		_ = json.NewEncoder(w).Encode(EmbeddingsPage{
			Embeddings: []Embedding{{PhotoUID: "p1", Model: "ViT-L-14", Pretrained: "laion", Dim: 3, Vector: []float32{0.1, 0.2, 0.3}}},
			Total:      42,
			NextAfter:  &next,
		})
	}))
	defer srv.Close()

	page, err := newTestClient(t, srv.URL).ListEmbeddings(context.Background(), 100, "p0")
	if err != nil {
		t.Fatalf("ListEmbeddings: %v", err)
	}
	if gotAuth != "Bearer psat_test" {
		t.Errorf("Authorization = %q, want Bearer psat_test", gotAuth)
	}
	if gotPath != "/api/v1/embeddings" {
		t.Errorf("path = %q, want /api/v1/embeddings", gotPath)
	}
	if gotQuery != "after=p0&limit=100" {
		t.Errorf("query = %q, want after=p0&limit=100", gotQuery)
	}
	if len(page.Embeddings) != 1 || page.Embeddings[0].PhotoUID != "p1" || len(page.Embeddings[0].Vector) != 3 {
		t.Errorf("page.Embeddings = %+v", page.Embeddings)
	}
	if page.Total != 42 || page.NextAfter == nil || *page.NextAfter != "p1" {
		t.Errorf("Total=%d NextAfter=%v", page.Total, page.NextAfter)
	}
}

func TestListFaces_decodesInt64Cursor(t *testing.T) {
	t.Parallel()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		next := int64(7)
		_ = json.NewEncoder(w).Encode(FacesPage{
			Faces: []Face{{
				ID: 7, PhotoUID: "p1", FaceIndex: 0, Model: "buffalo_l",
				BBox: []float64{10, 20, 30, 40}, DetScore: 0.9, MarkerUID: "m1",
				SubjectUID: "s1", SubjectName: "Alice", PhotoWidth: 100, PhotoHeight: 200,
				Orientation: 1, Vector: []float32{0.1, 0.2},
			}},
			Total:     5,
			NextAfter: &next,
		})
	}))
	defer srv.Close()

	page, err := newTestClient(t, srv.URL).ListFaces(context.Background(), 200, 3)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if gotQuery != "after=3&limit=200" {
		t.Errorf("query = %q, want after=3&limit=200", gotQuery)
	}
	if len(page.Faces) != 1 || page.Faces[0].ID != 7 || page.Faces[0].SubjectName != "Alice" {
		t.Errorf("page.Faces = %+v", page.Faces)
	}
	if page.NextAfter == nil || *page.NextAfter != 7 {
		t.Errorf("NextAfter = %v, want 7", page.NextAfter)
	}
}

func TestListFaces_omitsCursorAtStart(t *testing.T) {
	t.Parallel()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FacesPage{})
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL).ListFaces(context.Background(), 0, 0); err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty (no limit, no after)", gotQuery)
	}
}

func TestStats_decodes(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Stats{TotalPhotos: 20310, PhotosWithEmbeddings: 20092, TotalFaces: 112806})
	}))
	defer srv.Close()

	stats, err := newTestClient(t, srv.URL).Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalPhotos != 20310 || stats.PhotosWithEmbeddings != 20092 || stats.TotalFaces != 112806 {
		t.Errorf("stats = %+v", stats)
	}
}

func TestGet_retriesRateLimit(t *testing.T) {
	t.Parallel()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(EmbeddingsPage{Total: 1})
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL).ListEmbeddings(context.Background(), 10, ""); err != nil {
		t.Fatalf("ListEmbeddings after retry: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (two 429s then success)", calls)
	}
}

func TestGet_classifiesErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      int
		contentType string
		wantErr     error
	}{
		{"unauthorized", http.StatusUnauthorized, "application/json", ErrUnauthorized},
		{"forbidden", http.StatusForbidden, "application/json", ErrUnauthorized},
		{"not found", http.StatusNotFound, "application/json", ErrNotFound},
		{"server error", http.StatusInternalServerError, "application/json", ErrUpstream},
		{"unavailable", http.StatusServiceUnavailable, "application/json", ErrUnavailable},
		{"non-json 200", http.StatusOK, "text/html", ErrBadResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("<html>nope</html>"))
			}))
			defer srv.Close()

			_, err := newTestClient(t, srv.URL).Stats(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Stats error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
