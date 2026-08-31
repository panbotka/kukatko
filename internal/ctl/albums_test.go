package ctl

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

// albumsBody is a realistic bare {"albums": […]} envelope. It carries no paging
// fields at all — unlike the /photos envelope — which is exactly why albums need
// their own decoder.
const albumsBody = `{"albums":[
	{"uid":"alb01","slug":"trip","title":"Trip","type":"album","private":false,"photo_count":12,
	 "created_at":"2024-05-01T10:22:33Z","updated_at":"2024-05-02T10:22:33Z"},
	{"uid":"alb02","slug":"private-moments","title":"Moments","type":"moment","private":true,"photo_count":3}
]}`

// TestClient_ListAlbums verifies the bare album envelope reaches the wire path the
// API serves and decodes with its own decoder, photo counts included.
func TestClient_ListAlbums(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Write([]byte(albumsBody))
	})

	raw, err := client.ListAlbums(t.Context())
	if err != nil {
		t.Fatalf("ListAlbums returned %v", err)
	}
	if gotPath != "/api/v1/albums" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/v1/albums", gotMethod, gotPath)
	}
	albums, err := DecodeAlbums(raw)
	if err != nil {
		t.Fatalf("DecodeAlbums returned %v", err)
	}
	if len(albums) != 2 {
		t.Fatalf("albums = %+v, want two rows", albums)
	}
	if albums[0].UID != "alb01" || albums[0].Title != "Trip" || albums[0].PhotoCount != 12 {
		t.Errorf("first album = %+v, want the decoded Trip album", albums[0])
	}
	if !albums[1].Private || albums[1].Type != AlbumMoment {
		t.Errorf("second album = %+v, want a private moment album", albums[1])
	}
}

// TestClient_ListAlbums_empty verifies an empty library decodes to no albums
// rather than an error.
func TestClient_ListAlbums_empty(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"albums":[]}`))
	})
	raw, err := client.ListAlbums(t.Context())
	if err != nil {
		t.Fatalf("ListAlbums returned %v", err)
	}
	albums, err := DecodeAlbums(raw)
	if err != nil || len(albums) != 0 {
		t.Errorf("DecodeAlbums = %+v, %v, want an empty list", albums, err)
	}
}

// TestClient_GetAlbum verifies the uid is escaped into the path and one album
// decodes. The detail endpoint carries no photo_count, which the renderer relies
// on by omitting the column entirely.
func TestClient_GetAlbum(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"uid":"alb01","slug":"trip","title":"Trip","description":"Summer",
			"type":"album","private":true,"cover_photo_uid":"pht09"}`))
	})

	raw, err := client.GetAlbum(t.Context(), "alb 01")
	if err != nil {
		t.Fatalf("GetAlbum returned %v", err)
	}
	if gotPath != "/api/v1/albums/alb 01" {
		t.Errorf("path = %q, want the escaped uid", gotPath)
	}
	album, err := DecodeAlbum(raw)
	if err != nil {
		t.Fatalf("DecodeAlbum returned %v", err)
	}
	if album.UID != "alb01" || album.Description != "Summer" || !album.Private {
		t.Errorf("album = %+v, want the decoded album", album)
	}
	if album.CoverPhotoUID == nil || *album.CoverPhotoUID != "pht09" {
		t.Errorf("cover = %v, want pht09", album.CoverPhotoUID)
	}
	if album.PhotoCount != 0 {
		t.Errorf("photo_count = %d, want zero: the detail endpoint does not send one", album.PhotoCount)
	}
}

// TestClient_GetAlbum_emptyUID verifies a blank uid never reaches the network,
// where it would read as a list request.
func TestClient_GetAlbum_emptyUID(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with a blank uid")
	})
	if _, err := client.GetAlbum(t.Context(), "  "); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("GetAlbum(\"  \") error = %v, want ErrEmptyUID", err)
	}
}

// TestClient_CreateAlbum verifies the body carries the title and only the options
// actually set, and that the created album decodes back.
func TestClient_CreateAlbum(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"uid":"alb07","slug":"trip","title":"Trip","type":"album"}`))
	})

	raw, err := client.CreateAlbum(t.Context(), AlbumInput{Title: "Trip", Private: true})
	if err != nil {
		t.Fatalf("CreateAlbum returned %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotBody["title"] != "Trip" || gotBody["private"] != true {
		t.Errorf("body = %v, want the title and the private flag", gotBody)
	}
	if _, set := gotBody["type"]; set {
		t.Errorf("body = %v, want no type so the server applies its own default", gotBody)
	}
	album, err := DecodeAlbum(raw)
	if err != nil || album.UID != "alb07" {
		t.Errorf("DecodeAlbum = %+v, %v, want the created album", album, err)
	}
}

// TestClient_CreateAlbum_invalid verifies a blank title and an unknown type are
// caught client-side, before a round trip is spent on a guaranteed 400.
func TestClient_CreateAlbum_invalid(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite invalid input")
	})
	if _, err := client.CreateAlbum(t.Context(), AlbumInput{Title: "  "}); !errors.Is(err, ErrEmptyTitle) {
		t.Errorf("blank title error = %v, want ErrEmptyTitle", err)
	}
	_, err := client.CreateAlbum(t.Context(), AlbumInput{Title: "Trip", Type: "scrapbook"})
	if !errors.Is(err, ErrInvalidAlbumType) {
		t.Errorf("unknown type error = %v, want ErrInvalidAlbumType", err)
	}
}

// TestClient_AlbumMembership verifies both membership calls hit the same path with
// the verb the API distinguishes them by, send the uid list once, and decode the
// refreshed order the server echoes back.
func TestClient_AlbumMembership(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		call       func(*Client) (json.RawMessage, error)
		wantMethod string
	}{
		{
			name:       "add",
			wantMethod: http.MethodPost,
			call: func(c *Client) (json.RawMessage, error) {
				return c.AddAlbumPhotos(t.Context(), "alb01", []string{"pht01", "pht02"})
			},
		},
		{
			name:       "remove",
			wantMethod: http.MethodDelete,
			call: func(c *Client) (json.RawMessage, error) {
				return c.RemoveAlbumPhotos(t.Context(), "alb01", []string{"pht01", "pht02"})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var requests int
			var gotMethod, gotPath string
			var gotBody struct {
				PhotoUIDs []string `json:"photo_uids"`
			}
			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				requests++
				gotMethod, gotPath = r.Method, r.URL.Path
				json.NewDecoder(r.Body).Decode(&gotBody)
				w.Write([]byte(`{"photo_uids":["pht01","pht02","pht03"]}`))
			})

			raw, err := tt.call(client)
			if err != nil {
				t.Fatalf("membership call returned %v", err)
			}
			if requests != 1 {
				t.Errorf("the client made %d requests, want one for the whole list", requests)
			}
			if gotMethod != tt.wantMethod || gotPath != "/api/v1/albums/alb01/photos" {
				t.Errorf("request = %s %s, want %s on the membership path", gotMethod, gotPath, tt.wantMethod)
			}
			if len(gotBody.PhotoUIDs) != 2 || gotBody.PhotoUIDs[0] != "pht01" {
				t.Errorf("body photo_uids = %v, want both uids in one body", gotBody.PhotoUIDs)
			}
			uids, err := DecodePhotoUIDs(raw)
			if err != nil || len(uids) != 3 {
				t.Errorf("DecodePhotoUIDs = %v, %v, want the refreshed order", uids, err)
			}
		})
	}
}

// TestClient_AlbumMembership_invalid verifies a blank album uid and an empty photo
// list are both rejected before a request is made.
func TestClient_AlbumMembership_invalid(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite invalid input")
	})
	if _, err := client.AddAlbumPhotos(t.Context(), "", []string{"pht01"}); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank album uid error = %v, want ErrEmptyUID", err)
	}
	if _, err := client.RemoveAlbumPhotos(t.Context(), "alb01", nil); !errors.Is(err, ErrNoPhotoUIDs) {
		t.Errorf("empty photo list error = %v, want ErrNoPhotoUIDs", err)
	}
}

// TestDecodeAlbums_invalid verifies malformed JSON surfaces as an error rather
// than a silently empty list.
func TestDecodeAlbums_invalid(t *testing.T) {
	t.Parallel()

	if _, err := DecodeAlbums([]byte(`{"albums":`)); err == nil {
		t.Error("DecodeAlbums of malformed JSON returned no error")
	}
	if _, err := DecodeAlbum([]byte(`not json`)); err == nil {
		t.Error("DecodeAlbum of malformed JSON returned no error")
	}
	if _, err := DecodePhotoUIDs([]byte(`[`)); err == nil {
		t.Error("DecodePhotoUIDs of malformed JSON returned no error")
	}
}

// TestClient_UpdateAlbum verifies the update reads the album first and sends the
// whole record back, so the fields the caller did not name survive an endpoint
// that rewrites everything it is given.
func TestClient_UpdateAlbum(t *testing.T) {
	t.Parallel()

	var methods []string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"uid":"alb01","title":"Trip","description":"Summer",
				"type":"moment","private":true,"cover_photo_uid":"pht09"}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"uid":"alb01","title":"Journey","description":"Summer"}`))
	})

	title := "Journey"
	raw, err := client.UpdateAlbum(t.Context(), "alb01", AlbumPatch{Title: &title})
	if err != nil {
		t.Fatalf("UpdateAlbum returned %v", err)
	}
	if len(methods) != 2 || methods[0] != http.MethodGet || methods[1] != http.MethodPatch {
		t.Fatalf("requests = %v, want a GET then a PATCH", methods)
	}
	if gotBody["title"] != "Journey" {
		t.Errorf("body title = %v, want the new one", gotBody["title"])
	}
	if gotBody["description"] != "Summer" || gotBody["private"] != true ||
		gotBody["cover_photo_uid"] != "pht09" {
		t.Errorf("body = %v, want the untouched fields carried across", gotBody)
	}
	if _, sent := gotBody["type"]; sent {
		t.Errorf("body = %v, want no type: it is not user-editable and the server preserves it", gotBody)
	}
	album, err := DecodeAlbum(raw)
	if err != nil || album.Title != "Journey" {
		t.Errorf("DecodeAlbum = %+v, %v, want the refreshed album", album, err)
	}
}

// TestClient_UpdateAlbum_clearCover verifies a blank --cover travels as an
// explicit null, which is the only way to say "no cover" to a whole-record write.
func TestClient_UpdateAlbum_clearCover(t *testing.T) {
	t.Parallel()

	var gotBody map[string]json.RawMessage
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"uid":"alb01","title":"Trip","cover_photo_uid":"pht09"}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"uid":"alb01","title":"Trip"}`))
	})

	cover := ""
	if _, err := client.UpdateAlbum(t.Context(), "alb01", AlbumPatch{Cover: &cover}); err != nil {
		t.Fatalf("UpdateAlbum returned %v", err)
	}
	if string(gotBody["cover_photo_uid"]) != "null" {
		t.Errorf("cover_photo_uid = %s, want an explicit null", gotBody["cover_photo_uid"])
	}
}

// TestClient_UpdateAlbum_invalid verifies an empty patch and a patch that would
// blank the title are both caught before a request is spent.
func TestClient_UpdateAlbum_invalid(t *testing.T) {
	t.Parallel()

	blank := "   "
	tests := []struct {
		name  string
		patch AlbumPatch
		want  error
		serve bool
	}{
		{name: "nothing to change", patch: AlbumPatch{}, want: ErrNoAlbumEdits},
		{name: "blank title", patch: AlbumPatch{Title: &blank}, want: ErrEmptyTitle, serve: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				if !tt.serve || r.Method != http.MethodGet {
					t.Errorf("the server was contacted with %s despite invalid input", r.Method)
					return
				}
				w.Write([]byte(`{"uid":"alb01","title":"Trip"}`))
			})
			if _, err := client.UpdateAlbum(t.Context(), "alb01", tt.patch); !errors.Is(err, tt.want) {
				t.Errorf("UpdateAlbum error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestClient_DeleteAlbum verifies the verb and the path, and that a blank uid is
// refused before it reads as a list request.
func TestClient_DeleteAlbum(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.DeleteAlbum(t.Context(), "alb01"); err != nil {
		t.Fatalf("DeleteAlbum returned %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/albums/alb01" {
		t.Errorf("request = %s %s, want DELETE on the album path", gotMethod, gotPath)
	}
	if err := client.DeleteAlbum(t.Context(), " "); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank uid error = %v, want ErrEmptyUID", err)
	}
}

// TestClient_DescribeAlbum verifies a destructive command can name what it is
// about to remove.
func TestClient_DescribeAlbum(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"uid":"alb01","title":"Trip"}`))
	})
	who, err := client.DescribeAlbum(t.Context(), "alb01")
	if err != nil || who != "Trip (alb01)" {
		t.Errorf("DescribeAlbum = %q, %v, want the named album", who, err)
	}
}
