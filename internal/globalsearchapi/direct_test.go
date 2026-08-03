package globalsearchapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// The well-formed uids the direct-hit tests paste into the search box, one per
// prefix. Only their shape matters here — the fake resolves them from maps.
const (
	photoUID  = "ph7lpul2io09bcg2rvp2rljsr6"
	albumUID  = "al7lpul2io09bcg2rvp2rljsr6"
	labelUID  = "lb7lpul2io09bcg2rvp2rljsr6"
	personUID = "su7lpul2io09bcg2rvp2rljsr6"
	stackUID  = "st7lpul2io09bcg2rvp2rljsr6"
	markerUID = "mk7lpul2io09bcg2rvp2rljsr6"
	ppUID     = "pt8suk5b57jgshdz"
)

// newDirectFake returns a fake wired with one row of every kind: the photo the
// uid, marker, stack and PhotoPrism lookups all lead to, plus an album, a label
// and a person.
func newDirectFake() *fakeSearcher {
	photo := photos.Photo{UID: photoUID, Title: "Beach sunset", FileName: "IMG_0001.jpg"}
	member := photos.Photo{UID: "phstackmember0000000000000", FileName: "raw.dng"}
	cover := "phcover00000000000000000000"
	return &fakeSearcher{
		byUID:     map[string]photos.Photo{photoUID: photo, member.UID: member},
		byPPUID:   map[string]photos.Photo{ppUID: photo},
		byPPAlias: map[string]photos.Photo{},
		// The real store orders the primary first; so does this.
		stacks:       map[string][]photos.Photo{stackUID: {photo, member}},
		albumsByUID:  map[string]organize.Album{albumUID: {UID: albumUID, Title: "Léto 2024", CoverPhotoUID: &cover}},
		labelsByUID:  map[string]organize.Label{labelUID: {UID: labelUID, Name: "sunset"}},
		subjectsByID: map[string]people.Subject{personUID: {UID: personUID, Name: "Alice"}},
		markersByID:  map[string]people.Marker{markerUID: {UID: markerUID, PhotoUID: photoUID}},
	}
}

// getDirect issues a global search for q and decodes the envelope.
func getDirect(t *testing.T, f *fakeSearcher, q string) response {
	t.Helper()
	srv := newTestServer(f, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q="+q)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("q=%s status = %d, want 200", q, resp.StatusCode)
	}
	var body response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// TestDirect_everyPrefix verifies each uid prefix resolves to the right entity,
// and that the ids standing for something else are routed: a marker to the photo
// it sits on, a stack to its primary, a PhotoPrism uid to the catalogue photo.
func TestDirect_everyPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		uid        string
		wantKind   string
		wantTarget string
		wantUID    string
		wantTitle  string
	}{
		{"photo", photoUID, "photo", "photo", photoUID, "Beach sunset"},
		{"album", albumUID, "album", "album", albumUID, "Léto 2024"},
		{"label", labelUID, "label", "label", labelUID, "sunset"},
		{"person", personUID, "person", "person", personUID, "Alice"},
		{"marker routes to its photo", markerUID, "marker", "photo", photoUID, "Beach sunset"},
		{"stack routes to its primary", stackUID, "stack", "photo", photoUID, "Beach sunset"},
		{"photoprism uid", ppUID, "photoprism", "photo", photoUID, "Beach sunset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := getDirect(t, newDirectFake(), tt.uid)
			if body.Direct == nil {
				t.Fatalf("q=%s: no direct hit", tt.uid)
			}
			d := body.Direct
			if !d.Found {
				t.Fatalf("q=%s: found = false, want true", tt.uid)
			}
			if d.UID != tt.uid || d.Kind != tt.wantKind {
				t.Fatalf("q=%s: uid/kind = %s/%s, want %s/%s", tt.uid, d.UID, d.Kind, tt.uid, tt.wantKind)
			}
			if d.TargetKind != tt.wantTarget || d.TargetUID != tt.wantUID {
				t.Fatalf("q=%s: target = %s/%s, want %s/%s",
					tt.uid, d.TargetKind, d.TargetUID, tt.wantTarget, tt.wantUID)
			}
			if d.Title != tt.wantTitle {
				t.Fatalf("q=%s: title = %q, want %q", tt.uid, d.Title, tt.wantTitle)
			}
		})
	}
}

// TestDirect_photoprismAlias verifies a source uid whose bytes were catalogued
// under another uid resolves through the alias table rather than being reported
// missing.
func TestDirect_photoprismAlias(t *testing.T) {
	t.Parallel()
	f := newDirectFake()
	const aliased = "pt9zzzz5b57jgshd"
	f.byPPAlias[aliased] = photos.Photo{UID: photoUID, Title: "Beach sunset"}

	body := getDirect(t, f, aliased)
	if body.Direct == nil || !body.Direct.Found || body.Direct.TargetUID != photoUID {
		t.Fatalf("direct = %+v, want the aliased photo %s", body.Direct, photoUID)
	}
}

// TestDirect_notFound verifies a well-formed id that names nothing says so
// instead of falling through to an empty free-text result.
func TestDirect_notFound(t *testing.T) {
	t.Parallel()
	body := getDirect(t, newDirectFake(), "ph000000000000000000000000")
	if body.Direct == nil {
		t.Fatal("no direct hit for a well-formed uid")
	}
	if body.Direct.Found {
		t.Fatalf("found = true for an unknown uid: %+v", body.Direct)
	}
	if body.Direct.Kind != "photo" || body.Direct.TargetUID != "" {
		t.Fatalf("direct = %+v, want kind photo and no target", body.Direct)
	}
}

// TestDirect_replacesFanout verifies the uid branch does not also run the
// four-way fuzzy search: the groups come back empty (not nil) and no search
// method was called.
func TestDirect_replacesFanout(t *testing.T) {
	t.Parallel()
	f := newDirectFake()
	f.albums = []organize.AlbumCount{{Album: organize.Album{UID: "alx", Title: "x"}}}

	body := getDirect(t, f, photoUID)
	if f.searched != 0 {
		t.Fatalf("fuzzy search ran %d times for a uid query, want 0", f.searched)
	}
	if len(body.Albums) != 0 || len(body.Labels) != 0 || len(body.People) != 0 || len(body.Photos) != 0 {
		t.Fatalf("groups are not empty: %+v", body)
	}
}

// TestDirect_encodesEmptyGroupsAsArrays verifies the uid branch keeps the
// envelope's arrays non-nil, so a client never has to guard against null.
func TestDirect_encodesEmptyGroupsAsArrays(t *testing.T) {
	t.Parallel()
	srv := newTestServer(newDirectFake(), 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q="+photoUID)
	defer func() { _ = resp.Body.Close() }()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, group := range []string{"albums", "labels", "people", "photos"} {
		if string(raw[group]) != "[]" {
			t.Fatalf("%s = %s, want []", group, raw[group])
		}
	}
}

// TestDirect_reportsPhotoState verifies a photo reached by its id says what
// state it is in, so an archived or hidden hit is not silently confusing.
func TestDirect_reportsPhotoState(t *testing.T) {
	t.Parallel()
	f := newDirectFake()
	archived := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)
	stack := "st0000000000000000000000000"
	f.byUID[photoUID] = photos.Photo{
		UID: photoUID, Title: "Old attic", ArchivedAt: &archived,
		HiddenFromLibrary: true, Private: true, StackUID: &stack, StackPrimary: false,
	}

	body := getDirect(t, f, photoUID)
	if body.Direct == nil || !body.Direct.Found {
		t.Fatalf("direct = %+v, want a found hit", body.Direct)
	}
	want := []string{stateArchived, stateHidden, statePrivate, stateStackMember}
	if len(body.Direct.States) != len(want) {
		t.Fatalf("states = %v, want %v", body.Direct.States, want)
	}
	for i, state := range want {
		if body.Direct.States[i] != state {
			t.Fatalf("states = %v, want %v", body.Direct.States, want)
		}
	}
}

// TestDirect_noStatesForOrdinaryPhoto verifies a photo in the ordinary library
// view reports no states at all, so the client shows no needless badge.
func TestDirect_noStatesForOrdinaryPhoto(t *testing.T) {
	t.Parallel()
	body := getDirect(t, newDirectFake(), photoUID)
	if body.Direct == nil || len(body.Direct.States) != 0 {
		t.Fatalf("states = %+v, want none", body.Direct)
	}
}

// TestDirect_uidNextToText verifies an id pasted with a word beside it — the way
// it arrives out of a log line — is still recognised as a lookup.
func TestDirect_uidNextToText(t *testing.T) {
	t.Parallel()
	body := getDirect(t, newDirectFake(), "photo%20"+photoUID)
	if body.Direct == nil || body.Direct.TargetUID != photoUID {
		t.Fatalf("direct = %+v, want the photo %s", body.Direct, photoUID)
	}
}

// TestDirect_absentForPlainText verifies an ordinary query keeps the fuzzy
// behaviour and carries no direct hit at all.
func TestDirect_absentForPlainText(t *testing.T) {
	t.Parallel()
	f := newDirectFake()
	body := getDirect(t, f, "dovolena")
	if body.Direct != nil {
		t.Fatalf("direct = %+v, want none for plain text", body.Direct)
	}
	if f.searched == 0 {
		t.Fatal("fuzzy search did not run for plain text")
	}
}

// TestDirect_storeError verifies a store failure in the uid branch is a 500
// rather than a silent miss — "not found" must mean the id is unknown, not that
// the database was unreachable.
func TestDirect_storeError(t *testing.T) {
	t.Parallel()
	f := newDirectFake()
	f.err = errStore
	srv := newTestServer(f, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q="+albumUID)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}
