package mapy_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/mapy"
)

const testKey = "super-secret-key"

// newFakeMapy starts an httptest server impersonating the mapy.com REST API,
// invoking handler for every request, and returns a client pointed at it.
func newFakeMapy(t *testing.T, handler http.HandlerFunc) (*mapy.HTTPClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := mapy.New(mapy.Config{BaseURL: srv.URL, APIKey: testKey})
	if err != nil {
		t.Fatalf("mapy.New: %v", err)
	}
	return client, srv
}

// TestNew_invalidURL verifies New rejects a non-HTTP base URL.
func TestNew_invalidURL(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, url string }{
		{"bad scheme", "ftp://example.com"},
		{"no host", "http://"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := mapy.New(mapy.Config{BaseURL: tt.url, APIKey: testKey}); !errors.Is(err, mapy.ErrInvalidURL) {
				t.Fatalf("New(%q) error = %v, want ErrInvalidURL", tt.url, err)
			}
		})
	}
}

// TestTile_forwardsKeyAndStreams checks the tile request carries the API key in
// the header (and not the URL), targets the expected path, and streams the bytes
// back unchanged.
func TestTile_forwardsKeyAndStreams(t *testing.T) {
	t.Parallel()
	const payload = "PNGDATA-not-really"
	var gotPath, gotKey, gotQuery string
	client, _ := newFakeMapy(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotQuery = r.URL.Path, r.Header.Get("X-Mapy-Api-Key"), r.URL.RawQuery
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, payload)
	})

	res, err := client.Tile(context.Background(), mapy.TileParams{Mapset: "basic", Z: 3, X: 4, Y: 5})
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	body, _ := io.ReadAll(res.Body)
	if string(body) != payload {
		t.Errorf("tile body = %q, want %q", body, payload)
	}
	if res.ContentType != "image/png" {
		t.Errorf("content type = %q, want image/png", res.ContentType)
	}
	if gotKey != testKey {
		t.Errorf("upstream X-Mapy-Api-Key = %q, want %q", gotKey, testKey)
	}
	if gotQuery != "" {
		t.Errorf("tile query = %q, want empty (key must not be in URL)", gotQuery)
	}
	if want := "/v1/maptiles/basic/256/3/4/5"; gotPath != want {
		t.Errorf("tile path = %q, want %q", gotPath, want)
	}
}

// TestTile_retina checks the @2x suffix is applied only for mapsets that support
// retina and ignored otherwise.
func TestTile_retina(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		mapset   string
		wantSize string
	}{
		{"basic supports retina", "basic", "256@2x"},
		{"outdoor supports retina", "outdoor", "256@2x"},
		{"aerial falls back", "aerial", "256"},
		{"winter falls back", "winter", "256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotPath string
			client, _ := newFakeMapy(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "image/png")
				_, _ = io.WriteString(w, "x")
			})
			res, err := client.Tile(context.Background(),
				mapy.TileParams{Mapset: tt.mapset, Z: 1, X: 2, Y: 3, Retina: true})
			if err != nil {
				t.Fatalf("Tile: %v", err)
			}
			_ = res.Body.Close()
			if !strings.Contains(gotPath, "/"+tt.wantSize+"/") {
				t.Errorf("tile path = %q, want size segment %q", gotPath, tt.wantSize)
			}
		})
	}
}

// TestTile_invalidMapset checks an off-allow-list mapset is rejected before any
// upstream call.
func TestTile_invalidMapset(t *testing.T) {
	t.Parallel()
	called := false
	client, _ := newFakeMapy(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	_, err := client.Tile(context.Background(), mapy.TileParams{Mapset: "evil", Z: 1, X: 1, Y: 1})
	if !errors.Is(err, mapy.ErrInvalidMapset) {
		t.Fatalf("Tile error = %v, want ErrInvalidMapset", err)
	}
	if called {
		t.Error("upstream was called for an invalid mapset")
	}
}

// TestTile_statusClassification checks each upstream status maps to its sentinel
// and that no error mentions the API key.
func TestTile_statusClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code int
		want error
	}{
		{"unauthorized", http.StatusUnauthorized, mapy.ErrUnauthorized},
		{"forbidden", http.StatusForbidden, mapy.ErrUnauthorized},
		{"not found", http.StatusNotFound, mapy.ErrNotFound},
		{"rate limited", http.StatusTooManyRequests, mapy.ErrRateLimited},
		{"bad gateway", http.StatusBadGateway, mapy.ErrUnavailable},
		{"teapot", http.StatusTeapot, mapy.ErrUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, _ := newFakeMapy(t, func(w http.ResponseWriter, _ *http.Request) {
				// Echo the key in the body the way a careless upstream might; it must
				// not survive into the classified error.
				w.WriteHeader(tt.code)
				_, _ = io.WriteString(w, "rejected key "+testKey)
			})
			_, err := client.Tile(context.Background(), mapy.TileParams{Mapset: "basic", Z: 1, X: 1, Y: 1})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Tile error = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Errorf("error leaks API key: %v", err)
			}
		})
	}
}

// TestReverseGeocode_simplifies checks the rgeocode request carries lon/lat/lang
// plus the key header and that the response is reduced to the best match.
func TestReverseGeocode_simplifies(t *testing.T) {
	t.Parallel()
	var rawQuery, gotKey string
	client, _ := newFakeMapy(t, func(w http.ResponseWriter, r *http.Request) {
		rawQuery, gotKey = r.URL.RawQuery, r.Header.Get("X-Mapy-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[
			{"name":"Staré Město","location":"Praha, Hlavní město Praha, Česko",
			 "regionalStructure":[{"name":"Staré Město","type":"regional.municipality_part"},
			                      {"name":"Praha","type":"regional.municipality"}]},
			{"name":"second"}
		]}`)
	})

	res, err := client.ReverseGeocode(context.Background(), 50.08, 14.42)
	if err != nil {
		t.Fatalf("ReverseGeocode: %v", err)
	}
	if res.Name != "Staré Město" {
		t.Errorf("name = %q, want Staré Město", res.Name)
	}
	if res.Location != "Praha, Hlavní město Praha, Česko" {
		t.Errorf("location = %q", res.Location)
	}
	if len(res.RegionalStructure) != 2 || res.RegionalStructure[0].Name != "Staré Město" {
		t.Errorf("regional structure = %+v", res.RegionalStructure)
	}
	if gotKey != testKey {
		t.Errorf("rgeocode key header = %q, want %q", gotKey, testKey)
	}
	for _, want := range []string{"lat=50.08", "lon=14.42", "lang=cs"} {
		if !strings.Contains(rawQuery, want) {
			t.Errorf("rgeocode query %q missing %q", rawQuery, want)
		}
	}
}

// TestReverseGeocode_noMatch checks an empty item list maps to ErrNotFound.
func TestReverseGeocode_noMatch(t *testing.T) {
	t.Parallel()
	client, _ := newFakeMapy(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"items":[]}`)
	})
	_, err := client.ReverseGeocode(context.Background(), 0, 0)
	if !errors.Is(err, mapy.ErrNotFound) {
		t.Fatalf("ReverseGeocode error = %v, want ErrNotFound", err)
	}
}

// TestUserAgent_sentOnEveryRequest checks the configured User-Agent reaches
// mapy.com on both the tile and the rgeocode path, and that an empty
// Config.UserAgent leaves Go's default header untouched.
func TestUserAgent_sentOnEveryRequest(t *testing.T) {
	t.Parallel()
	const configuredUA = "Kukatko/1.0 (test-token)"

	// call drives one client method against the fake upstream; the returned error
	// is irrelevant here, only the header the upstream saw is.
	calls := map[string]func(*mapy.HTTPClient) error{
		"tile": func(c *mapy.HTTPClient) error {
			res, err := c.Tile(context.Background(), mapy.TileParams{Mapset: "basic", Z: 1, X: 2, Y: 3})
			if err == nil {
				_ = res.Body.Close()
			}
			return err
		},
		"rgeocode": func(c *mapy.HTTPClient) error {
			_, err := c.ReverseGeocode(context.Background(), 50.08, 14.42)
			return err
		},
	}

	for _, tt := range []struct {
		name      string
		userAgent string
		configure bool
	}{
		{name: "configured user agent is sent", userAgent: configuredUA, configure: true},
		{name: "empty user agent falls back to the Go default", userAgent: "", configure: false},
	} {
		for op, call := range calls {
			t.Run(tt.name+"/"+op, func(t *testing.T) {
				t.Parallel()
				var gotUA string
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotUA = r.Header.Get("User-Agent")
					_, _ = io.WriteString(w, `{"items":[{"name":"x"}]}`)
				}))
				t.Cleanup(srv.Close)
				client, err := mapy.New(mapy.Config{BaseURL: srv.URL, APIKey: testKey, UserAgent: tt.userAgent})
				if err != nil {
					t.Fatalf("mapy.New: %v", err)
				}
				if err := call(client); err != nil {
					t.Fatalf("%s: %v", op, err)
				}
				if tt.configure {
					if gotUA != configuredUA {
						t.Errorf("%s User-Agent = %q, want %q", op, gotUA, configuredUA)
					}
					return
				}
				if !strings.HasPrefix(gotUA, "Go-http-client/") {
					t.Errorf("%s User-Agent = %q, want Go's default (no explicit header)", op, gotUA)
				}
			})
		}
	}
}

// TestReverseGeocode_statusClassification checks upstream errors are classified
// and never leak the key.
func TestReverseGeocode_statusClassification(t *testing.T) {
	t.Parallel()
	client, _ := newFakeMapy(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "over quota for key "+testKey)
	})
	_, err := client.ReverseGeocode(context.Background(), 1, 2)
	if !errors.Is(err, mapy.ErrRateLimited) {
		t.Fatalf("ReverseGeocode error = %v, want ErrRateLimited", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("error leaks API key: %v", err)
	}
}

// TestClampGeocodeLimit verifies the suggestion count is bounded, with a
// non-positive limit meaning "the default" rather than "none".
func TestClampGeocodeLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero means default", 0, mapy.DefaultGeocodeLimit},
		{"negative means default", -3, mapy.DefaultGeocodeLimit},
		{"in range is kept", 3, 3},
		{"at the cap is kept", mapy.MaxGeocodeLimit, mapy.MaxGeocodeLimit},
		{"above the cap is clamped", 500, mapy.MaxGeocodeLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mapy.ClampGeocodeLimit(tt.in); got != tt.want {
				t.Errorf("ClampGeocodeLimit(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestGeocode_mapsItemsAndRequest checks a place search sends the query, the
// language and a clamped limit upstream with the key in the header, and maps the
// mapy.com items onto Place — including the lon/lat swap, which is the one place
// a silent bug would drop pins in the wrong hemisphere.
func TestGeocode_mapsItemsAndRequest(t *testing.T) {
	t.Parallel()
	const payload = `{"items":[
		{"name":"Veselí nad Moravou","label":"Město","type":"regional.municipality",
		 "location":"Česko","position":{"lon":17.37649,"lat":48.95363},
		 "bbox":[17.3,48.9,17.4,48.9],"zip":"69801"}],"locality":[]}`
	var gotPath, gotKey string
	var gotQuery map[string][]string
	client, _ := newFakeMapy(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotQuery = r.URL.Path, r.Header.Get("X-Mapy-Api-Key"), r.URL.Query()
		_, _ = io.WriteString(w, payload)
	})

	places, err := client.Geocode(context.Background(), "  Veselí nad Moravou  ", 500)
	if err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	if len(places) != 1 {
		t.Fatalf("places = %d, want 1", len(places))
	}
	want := mapy.Place{
		Name: "Veselí nad Moravou", Label: "Město", Type: "regional.municipality",
		Location: "Česko", Lat: 48.95363, Lng: 17.37649,
	}
	if places[0] != want {
		t.Errorf("place = %+v, want %+v", places[0], want)
	}
	if gotPath != "/v1/geocode" {
		t.Errorf("path = %q, want /v1/geocode", gotPath)
	}
	if gotKey != testKey {
		t.Errorf("key header = %q, want %q", gotKey, testKey)
	}
	if got := gotQuery["query"][0]; got != "Veselí nad Moravou" {
		t.Errorf("query = %q, want the trimmed, accented name", got)
	}
	if got := gotQuery["lang"][0]; got != mapy.DefaultLang {
		t.Errorf("lang = %q, want %q", got, mapy.DefaultLang)
	}
	if got := gotQuery["limit"][0]; got != "15" {
		t.Errorf("limit = %q, want 15 (clamped from 500)", got)
	}
	if _, ok := gotQuery["apikey"]; ok {
		t.Error("query carries the API key; it must stay in the header")
	}
}

// TestGeocode_emptyQuery checks a blank name never reaches mapy.com: an empty
// typeahead must not cost a credit.
func TestGeocode_emptyQuery(t *testing.T) {
	t.Parallel()
	var calls int
	client, _ := newFakeMapy(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"items":[]}`)
	})
	for _, query := range []string{"", "   ", "\t\n"} {
		if _, err := client.Geocode(context.Background(), query, 5); !errors.Is(err, mapy.ErrEmptyQuery) {
			t.Errorf("Geocode(%q) error = %v, want ErrEmptyQuery", query, err)
		}
	}
	if calls != 0 {
		t.Errorf("upstream calls = %d, want 0", calls)
	}
}

// TestGeocode_noItems checks a name mapy.com matches nothing for is an empty
// slice and no error — the normal answer to a half-typed name.
func TestGeocode_noItems(t *testing.T) {
	t.Parallel()
	client, _ := newFakeMapy(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"items":[],"locality":[]}`)
	})
	places, err := client.Geocode(context.Background(), "qqqq", 5)
	if err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	if len(places) != 0 {
		t.Errorf("places = %v, want none", places)
	}
}

// TestGeocode_upstreamErrors checks each upstream status becomes its sentinel, so
// the HTTP layer can classify a failure without reading a body that might echo
// the key.
func TestGeocode_upstreamErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		wantErr error
	}{
		{"rejected key", http.StatusForbidden, mapy.ErrUnauthorized},
		{"expired key", http.StatusUnauthorized, mapy.ErrUnauthorized},
		{"unplaceable name", http.StatusNotFound, mapy.ErrNotFound},
		{"out of credits", http.StatusTooManyRequests, mapy.ErrRateLimited},
		{"provider down", http.StatusServiceUnavailable, mapy.ErrUnavailable},
		{"unexpected failure", http.StatusInternalServerError, mapy.ErrUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, _ := newFakeMapy(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, "key "+testKey+" refused")
			})
			_, err := client.Geocode(context.Background(), "Brno", 5)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Geocode error = %v, want %v", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Errorf("error leaks the API key: %v", err)
			}
		})
	}
}

// TestGeocode_badBody checks an unparseable answer is an upstream error rather
// than a panic or a silent empty list.
func TestGeocode_badBody(t *testing.T) {
	t.Parallel()
	client, _ := newFakeMapy(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "not json at all")
	})
	if _, err := client.Geocode(context.Background(), "Brno", 5); !errors.Is(err, mapy.ErrUpstream) {
		t.Fatalf("Geocode error = %v, want ErrUpstream", err)
	}
}
