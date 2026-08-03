package photoprism

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// configPayload is a trimmed GET /api/v1/config body: the count object the client
// keeps, next to the fields it must ignore (the client config is large and
// carries tokens).
const configPayload = `{
	"downloadToken": "secret-download",
	"previewToken": "secret-preview",
	"settings": {"ui": {"theme": "default"}},
	"count": {
		"all": 20677, "photos": 20670, "media": 6, "videos": 6, "live": 0,
		"animated": 0, "audio": 0, "documents": 1, "hidden": 0, "archived": 96,
		"private": 0, "review": 583, "files": 20870
	}
}`

// TestCounts_readsLibraryTotals checks the counts are read off the client config
// and that the rest of that payload is discarded.
func TestCounts_readsLibraryTotals(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(w, configPayload)
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv.URL).Counts(context.Background())
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if gotPath != "/api/v1/config" {
		t.Errorf("path = %q, want /api/v1/config", gotPath)
	}
	want := LibraryCounts{
		All: 20677, Photos: 20670, Media: 6, Videos: 6, Documents: 1,
		Archived: 96, Review: 583, Files: 20870,
	}
	if got != want {
		t.Errorf("Counts() = %+v, want %+v", got, want)
	}
}

// TestCounts_missingCountObject checks a config payload without a count object
// decodes to zeroes rather than failing: a zero total simply means the reconciler
// has no independent check to make, which is safer than aborting the pass.
func TestCounts_missingCountObject(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, `{"siteTitle": "PhotoPrism"}`)
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv.URL).Counts(context.Background())
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if got != (LibraryCounts{}) {
		t.Errorf("Counts() = %+v, want zero value", got)
	}
}

// TestCounts_errors checks upstream failures are classified like every other JSON
// read: an unauthorized status and a non-JSON body each map to their sentinel.
func TestCounts_errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{
			name: "unauthorized",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			want: ErrUnauthorized,
		},
		{
			name: "html body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<html>login</html>"))
			},
			want: ErrBadResponse,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			_, err := newTestClient(t, srv.URL).Counts(context.Background())
			if !errors.Is(err, tt.want) {
				t.Fatalf("Counts error = %v, want %v", err, tt.want)
			}
		})
	}
}
