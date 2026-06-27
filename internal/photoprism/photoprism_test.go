package photoprism

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestClient builds an HTTPClient pointed at base with tiny backoff delays so
// retry tests run fast.
func newTestClient(t *testing.T, base string) *HTTPClient {
	t.Helper()
	c, err := New(Config{
		BaseURL:        base,
		Token:          "test-token",
		Timeout:        2 * time.Second,
		MaxRetries:     5,
		RetryBaseDelay: time.Millisecond,
		RetryMaxDelay:  5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// writeJSON is a test helper that writes a JSON body with the right content type.
func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

// TestNew_validation checks base-URL validation and default application.
func TestNew_validation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		baseURL string
		wantErr error
	}{
		{name: "valid https", baseURL: "https://photos.example", wantErr: nil},
		{name: "trailing slash trimmed", baseURL: "https://photos.example/", wantErr: nil},
		{name: "missing host", baseURL: "https://", wantErr: ErrInvalidURL},
		{name: "bad scheme", baseURL: "ftp://photos.example", wantErr: ErrInvalidURL},
		{name: "not a url", baseURL: "://nope", wantErr: ErrInvalidURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := New(Config{BaseURL: tt.baseURL, Token: "t"})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New(%q) err = %v, want %v", tt.baseURL, err, tt.wantErr)
			}
			if tt.wantErr == nil && c.timeout != DefaultTimeout {
				t.Errorf("default timeout = %v, want %v", c.timeout, DefaultTimeout)
			}
		})
	}
}

// TestListPhotos_incrementalQuery verifies the incremental query is built with
// the clamped count, offset, merged=true, order=updated, and the updated: filter.
func TestListPhotos_incrementalQuery(t *testing.T) {
	t.Parallel()
	var gotQuery url.Values
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, `[]`)
	}))
	defer srv.Close()

	since := time.Date(2023, 1, 2, 3, 4, 5, 0, time.UTC)
	c := newTestClient(t, srv.URL)
	if _, err := c.ListPhotos(context.Background(), PhotoListParams{
		Count:        5000, // exceeds MaxCount, must clamp
		Offset:       2000,
		UpdatedSince: since,
	}); err != nil {
		t.Fatalf("ListPhotos: %v", err)
	}

	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	checks := map[string]string{
		"count":  "1000",
		"offset": "2000",
		"merged": "true",
		"order":  "updated",
		"q":      `updated:"2023-01-02T03:04:05Z"`,
	}
	for key, want := range checks {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("query %q = %q, want %q", key, got, want)
		}
	}
}

// TestListPhotos_scopedQuery verifies AlbumUID sets the s= album filter and that
// a raw Query overrides the UpdatedSince watermark filter — the two scoping modes
// used to map album and label membership during import.
func TestListPhotos_scopedQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		params PhotoListParams
		wantS  string
		wantQ  string
	}{
		{
			name:   "album scope sets s",
			params: PhotoListParams{AlbumUID: "as6sg6bxpogaaba1"},
			wantS:  "as6sg6bxpogaaba1",
			wantQ:  "",
		},
		{
			name:   "raw query overrides watermark",
			params: PhotoListParams{Query: `label:"cat"`, UpdatedSince: time.Now()},
			wantS:  "",
			wantQ:  `label:"cat"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				writeJSON(w, `[]`)
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL)
			if _, err := c.ListPhotos(context.Background(), tt.params); err != nil {
				t.Fatalf("ListPhotos: %v", err)
			}
			if got := gotQuery.Get("s"); got != tt.wantS {
				t.Errorf("query s = %q, want %q", got, tt.wantS)
			}
			if got := gotQuery.Get("q"); got != tt.wantQ {
				t.Errorf("query q = %q, want %q", got, tt.wantQ)
			}
		})
	}
}

// TestListPhotos_paging verifies the caller can page by advancing the offset and
// that each request reflects the requested offset.
func TestListPhotos_paging(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	offsets := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		offsets = append(offsets, r.URL.Query().Get("offset"))
		mu.Unlock()
		if r.URL.Query().Get("offset") == "0" {
			writeJSON(w, `[{"UID":"p1"},{"UID":"p2"}]`)
			return
		}
		writeJSON(w, `[{"UID":"p3"}]`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	page1, err := c.ListPhotos(context.Background(), PhotoListParams{Count: 2, Offset: 0})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	page2, err := c.ListPhotos(context.Background(), PhotoListParams{Count: 2, Offset: 2})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page1) != 2 || len(page2) != 1 {
		t.Fatalf("page sizes = %d,%d want 2,1", len(page1), len(page2))
	}
	if page1[0].UID != "p1" || page2[0].UID != "p3" {
		t.Errorf("unexpected UIDs: %q %q", page1[0].UID, page2[0].UID)
	}
	if offsets[0] != "0" || offsets[1] != "2" {
		t.Errorf("offsets = %v, want [0 2]", offsets)
	}
}

// TestListPhotos_fieldParsing verifies photo, file and marker fields decode,
// including Files[].Hash (SHA1) and the embedded Markers[].
func TestListPhotos_fieldParsing(t *testing.T) {
	t.Parallel()
	const body = `[{
		"UID":"pqabc","Type":"image","Title":"Beach","Description":"Sunset",
		"TakenAt":"2023-07-01T10:00:00Z","Lat":50.1,"Lng":14.4,"Altitude":300,
		"Width":4000,"Height":3000,"OriginalName":"IMG_1.jpg",
		"CameraModel":"X100","LensModel":"23mm","Iso":200,"FNumber":2.8,
		"UpdatedAt":"2023-07-02T08:00:00Z",
		"Files":[
			{"UID":"f1","Hash":"da39a3ee5e6b4b0d3255bfef95601890afd80709","Primary":true,
			 "Mime":"image/jpeg","Markers":[
				{"UID":"m1","Type":"face","Name":"Alice","SubjUID":"su1",
				 "X":0.1,"Y":0.2,"W":0.3,"H":0.4,"Score":90}]},
			{"UID":"f2","Hash":"otherhash","Primary":false,"Mime":"image/heic"}
		]}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, body)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	photos, err := c.ListPhotos(context.Background(), PhotoListParams{})
	if err != nil {
		t.Fatalf("ListPhotos: %v", err)
	}
	if len(photos) != 1 {
		t.Fatalf("got %d photos, want 1", len(photos))
	}
	p := photos[0]
	if p.UID != "pqabc" || p.Title != "Beach" || p.Lat != 50.1 || p.Altitude != 300 {
		t.Errorf("photo scalar fields wrong: %+v", p)
	}
	if !p.TakenAt.Equal(time.Date(2023, 7, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("TakenAt = %v", p.TakenAt)
	}
	primary, ok := p.PrimaryFile()
	if !ok || primary.Hash != "da39a3ee5e6b4b0d3255bfef95601890afd80709" {
		t.Errorf("PrimaryFile = %+v ok=%v", primary, ok)
	}
	if len(primary.Markers) != 1 {
		t.Fatalf("got %d markers, want 1", len(primary.Markers))
	}
	m := primary.Markers[0]
	if m.Type != "face" || m.Name != "Alice" || m.SubjUID != "su1" || m.X != 0.1 || m.Score != 90 {
		t.Errorf("marker fields wrong: %+v", m)
	}
}

// TestListEndpoints_parse exercises ListAlbums, ListLabels and ListSubjects
// against their respective endpoints.
func TestListEndpoints_parse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/albums":
			writeJSON(w, `[{"UID":"al1","Title":"Trip","Type":"album"}]`)
		case "/api/v1/labels":
			writeJSON(w, `[{"UID":"lb1","Name":"Dog","Priority":5}]`)
		case "/api/v1/subjects":
			writeJSON(w, `[{"UID":"su1","Name":"Alice","Type":"person"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	ctx := context.Background()
	albums, err := c.ListAlbums(ctx, ListParams{})
	if err != nil || len(albums) != 1 || albums[0].Title != "Trip" {
		t.Fatalf("ListAlbums = %v, %v", albums, err)
	}
	labels, err := c.ListLabels(ctx, ListParams{Count: 50})
	if err != nil || len(labels) != 1 || labels[0].Priority != 5 {
		t.Fatalf("ListLabels = %v, %v", labels, err)
	}
	subjects, err := c.ListSubjects(ctx, ListParams{})
	if err != nil || len(subjects) != 1 || subjects[0].Name != "Alice" {
		t.Fatalf("ListSubjects = %v, %v", subjects, err)
	}
}

// TestDownloadOriginal_streamsBytesWithToken verifies a download obtains a
// session token, sends it as ?t=, and streams the original bytes.
func TestDownloadOriginal_streamsBytesWithToken(t *testing.T) {
	t.Parallel()
	const want = "ORIGINAL-IMAGE-BYTES"
	var sessionCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session":
			sessionCalls++
			writeJSON(w, `{"config":{"downloadToken":"dtok","previewToken":"ptok"}}`)
		case r.URL.Path == "/api/v1/dl/abc123":
			if r.URL.Query().Get("t") != "dtok" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, want)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	dl, err := c.DownloadOriginal(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("DownloadOriginal: %v", err)
	}
	defer func() { _ = dl.Body.Close() }()
	got, err := io.ReadAll(dl.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if dl.ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q, want image/jpeg", dl.ContentType)
	}
	if sessionCalls != 1 {
		t.Errorf("session calls = %d, want 1", sessionCalls)
	}
}

// TestDownloadOriginal_emptyHash rejects a blank file hash before any request.
func TestDownloadOriginal_emptyHash(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, "https://photos.example")
	_, err := c.DownloadOriginal(context.Background(), "   ")
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
}

// TestDownloadOriginal_refreshesTokenFromHeader verifies a rotated token
// advertised via X-Download-Token is used on the next download without a new
// session call.
func TestDownloadOriginal_refreshesTokenFromHeader(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	valid := "tok1"
	sessionCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/session" {
			sessionCalls++
			writeJSON(w, `{"config":{"downloadToken":"`+valid+`"}}`)
			return
		}
		if r.URL.Query().Get("t") != valid {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		// Rotate the token and advertise it on the response.
		valid = "tok2"
		w.Header().Set("X-Download-Token", valid)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, "bytes")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	for i := range 2 {
		dl, err := c.DownloadOriginal(context.Background(), "h")
		if err != nil {
			t.Fatalf("download %d: %v", i, err)
		}
		_, _ = io.ReadAll(dl.Body)
		_ = dl.Body.Close()
	}
	if sessionCalls != 1 {
		t.Errorf("session calls = %d, want 1 (header rotation should avoid a refresh)", sessionCalls)
	}
}

// TestDownloadOriginal_refreshesSessionOnUnauthorized verifies that a stale
// cached token triggers a session refresh and a single retry.
func TestDownloadOriginal_refreshesSessionOnUnauthorized(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	issued := 0
	valid := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/session" {
			issued++
			valid = "tok" + strings.Repeat("x", issued)
			writeJSON(w, `{"config":{"downloadToken":"`+valid+`"}}`)
			return
		}
		if r.URL.Query().Get("t") != valid {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Rotate server-side without telling the client, so the next call is stale.
		valid = "rotated"
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	// First download establishes a session and succeeds.
	dl, err := c.DownloadOriginal(context.Background(), "h")
	if err != nil {
		t.Fatalf("first download: %v", err)
	}
	_, _ = io.ReadAll(dl.Body)
	_ = dl.Body.Close()
	// Second download's cached token is now stale -> 401 -> refresh -> retry.
	dl2, err := c.DownloadOriginal(context.Background(), "h")
	if err != nil {
		t.Fatalf("second download: %v", err)
	}
	_, _ = io.ReadAll(dl2.Body)
	_ = dl2.Body.Close()
	if issued != 2 {
		t.Errorf("session refreshes = %d, want 2", issued)
	}
}

// TestListPhotos_retriesOn429 verifies the client backs off and retries on 429,
// honouring Retry-After, and ultimately succeeds.
func TestListPhotos_retriesOn429(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeJSON(w, `[{"UID":"p1"}]`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	photos, err := c.ListPhotos(context.Background(), PhotoListParams{})
	if err != nil {
		t.Fatalf("ListPhotos: %v", err)
	}
	if len(photos) != 1 {
		t.Errorf("got %d photos, want 1", len(photos))
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

// TestListPhotos_rateLimitExhausted verifies that an unrelenting 429 returns
// ErrRateLimited after the retry budget is spent.
func TestListPhotos_rateLimitExhausted(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c, err := New(Config{
		BaseURL:        srv.URL,
		Token:          "t",
		MaxRetries:     2,
		RetryBaseDelay: time.Millisecond,
		RetryMaxDelay:  2 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.ListPhotos(context.Background(), PhotoListParams{}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

// TestGetJSON_errorMapping checks status and content-type classification on a
// JSON endpoint.
func TestGetJSON_errorMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr error
	}{
		{
			name: "401 unauthorized",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			wantErr: ErrUnauthorized,
		},
		{
			name: "404 not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: ErrNotFound,
		},
		{
			name: "503 unavailable",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
			wantErr: ErrUnavailable,
		},
		{
			name: "500 upstream",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: ErrUpstream,
		},
		{
			name: "200 but HTML not JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, "<html></html>")
			},
			wantErr: ErrBadResponse,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			c := newTestClient(t, srv.URL)
			_, err := c.ListPhotos(context.Background(), PhotoListParams{})
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestSend_transportFailure maps a connection failure to ErrUnavailable.
func TestSend_transportFailure(t *testing.T) {
	t.Parallel()
	// A reserved TEST-NET-1 address with a closed port yields a transport error.
	c := newTestClient(t, "http://192.0.2.1:1")
	c.maxRetries = 0
	c.timeout = 200 * time.Millisecond
	_, err := c.ListPhotos(context.Background(), PhotoListParams{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
