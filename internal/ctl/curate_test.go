package ctl

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// TestClient_ListFavorites verifies the favorites page reuses the /photos envelope
// and honours the catalogue filters.
func TestClient_ListFavorites(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery url.Values
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		w.Write([]byte(`{"photos":[{"uid":"pht01","is_favorite":true}],
			"total":1,"limit":100,"offset":0,"next_offset":null}`))
	})

	raw, err := client.ListFavorites(t.Context(), ListOptions{Limit: 10, Year: 2024})
	if err != nil {
		t.Fatalf("ListFavorites returned %v", err)
	}
	if gotPath != "/api/v1/favorites" {
		t.Errorf("path = %q, want /api/v1/favorites", gotPath)
	}
	if gotQuery.Get("limit") != "10" || gotQuery.Get("taken_after") == "" {
		t.Errorf("query = %v, want the paging and the year range", gotQuery)
	}
	page, err := DecodePhotoPage(raw)
	if err != nil || len(page.Photos) != 1 {
		t.Errorf("DecodePhotoPage = %+v, %v, want the favorites page", page, err)
	}
}

// TestClient_ListFavorites_dropsFavorite pins the one filter GET /favorites does
// not need: the endpoint scopes itself to the caller, so forwarding favorite=true
// would only restate what the path already says.
func TestClient_ListFavorites_dropsFavorite(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"photos":[],"total":0,"limit":100,"offset":0,"next_offset":null}`))
	})

	if _, err := client.ListFavorites(t.Context(), ListOptions{Favorite: true, Album: "alb1"}); err != nil {
		t.Fatalf("ListFavorites returned %v", err)
	}
	if gotQuery.Has("favorite") {
		t.Errorf("query = %v, want no favorite parameter", gotQuery)
	}
	if gotQuery.Get("album") != "alb1" {
		t.Errorf("album = %q, want the filters the endpoint does honour to survive", gotQuery.Get("album"))
	}
}

// TestClient_Favorite verifies both toggles hit the same path with the verb the
// API distinguishes them by, and that a 204 is a success with no body.
func TestClient_Favorite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		call       func(*Client) error
		wantMethod string
	}{
		{
			name:       "add",
			wantMethod: http.MethodPut,
			call:       func(c *Client) error { return c.AddFavorite(t.Context(), "pht 01") },
		},
		{
			name:       "remove",
			wantMethod: http.MethodDelete,
			call:       func(c *Client) error { return c.RemoveFavorite(t.Context(), "pht 01") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			})

			if err := tt.call(client); err != nil {
				t.Fatalf("favorite toggle returned %v", err)
			}
			if gotMethod != tt.wantMethod || gotPath != "/api/v1/photos/pht 01/favorite" {
				t.Errorf("request = %s %s, want %s on the escaped favorite path",
					gotMethod, gotPath, tt.wantMethod)
			}
		})
	}
}

// TestClient_Favorite_notFound verifies a missing photo surfaces the server's own
// 404 message rather than a bare failure.
func TestClient_Favorite_notFound(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"photo not found"}`))
	})

	var status *StatusError
	err := client.AddFavorite(t.Context(), "pht99")
	if !errors.As(err, &status) || status.Status != http.StatusNotFound {
		t.Fatalf("AddFavorite error = %v, want a 404 *StatusError", err)
	}
	if status.Message != "photo not found" {
		t.Errorf("message = %q, want the server's own text", status.Message)
	}
}

// TestClient_SetRating verifies the stars and the flag travel as an optional pair,
// so setting one leaves the other untouched server-side.
func TestClient_SetRating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rating   *int
		flag     *string
		wantKeys []string
		noKeys   []string
	}{
		{name: "stars only", rating: new(4), wantKeys: []string{"rating"}, noKeys: []string{"flag"}},
		{name: "flag only", flag: new(FlagPick), wantKeys: []string{"flag"}, noKeys: []string{"rating"}},
		{name: "eye flag only", flag: new(FlagEye), wantKeys: []string{"flag"}, noKeys: []string{"rating"}},
		{name: "both", rating: new(0), flag: new(FlagReject), wantKeys: []string{"rating", "flag"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			var gotBody map[string]any
			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				json.NewDecoder(r.Body).Decode(&gotBody)
				w.WriteHeader(http.StatusNoContent)
			})

			if err := client.SetRating(t.Context(), "pht01", tt.rating, tt.flag); err != nil {
				t.Fatalf("SetRating returned %v", err)
			}
			if gotMethod != http.MethodPut || gotPath != "/api/v1/photos/pht01/rating" {
				t.Errorf("request = %s %s, want PUT on the rating path", gotMethod, gotPath)
			}
			for _, key := range tt.wantKeys {
				if _, set := gotBody[key]; !set {
					t.Errorf("body = %v, want %q to be sent", gotBody, key)
				}
			}
			for _, key := range tt.noKeys {
				if _, set := gotBody[key]; set {
					t.Errorf("body = %v, want %q omitted so the server leaves it alone", gotBody, key)
				}
			}
		})
	}
}

// TestClient_SetRating_invalid verifies an empty change, an out-of-range star
// value and an unknown flag are all caught before a round trip.
func TestClient_SetRating_invalid(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite invalid input")
	})
	if err := client.SetRating(t.Context(), "pht01", nil, nil); !errors.Is(err, ErrEmptyRating) {
		t.Errorf("empty rating error = %v, want ErrEmptyRating", err)
	}
	if err := client.SetRating(t.Context(), "pht01", new(6), nil); !errors.Is(err, ErrInvalidRating) {
		t.Errorf("out-of-range rating error = %v, want ErrInvalidRating", err)
	}
	if err := client.SetRating(t.Context(), "pht01", new(-1), nil); !errors.Is(err, ErrInvalidRating) {
		t.Errorf("negative rating error = %v, want ErrInvalidRating", err)
	}
	if err := client.SetRating(t.Context(), "pht01", nil, new("maybe")); !errors.Is(err, ErrInvalidFlag) {
		t.Errorf("unknown flag error = %v, want ErrInvalidFlag", err)
	}
	if err := client.SetRating(t.Context(), "", new(3), nil); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank uid error = %v, want ErrEmptyUID", err)
	}
}

// TestClient_ClearRating verifies the clear call is a bodiless DELETE on the
// rating path.
func TestClient_ClearRating(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var gotLength int64
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotLength = r.Method, r.URL.Path, r.ContentLength
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.ClearRating(t.Context(), "pht01"); err != nil {
		t.Fatalf("ClearRating returned %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/photos/pht01/rating" {
		t.Errorf("request = %s %s, want DELETE on the rating path", gotMethod, gotPath)
	}
	if gotLength > 0 {
		t.Errorf("content length = %d, want no body", gotLength)
	}
}
