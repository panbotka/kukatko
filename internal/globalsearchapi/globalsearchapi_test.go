package globalsearchapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/thumb"
)

// fakeSearcher is an in-memory implementation of every store interface the
// handler needs. It records the query and limit it was asked for and returns
// canned rows or a canned error. The uid lookups answer from small maps, so the
// direct-hit branch can be driven without a database; a uid absent from a map is
// a miss, which is what the handler must report as "no such id".
type fakeSearcher struct {
	gotQuery string
	gotLimit int
	albums   []organize.AlbumCount
	labels   []organize.LabelCount
	subjects []people.Subject
	photos   []photos.Photo
	err      error

	// searched counts the fuzzy fan-out calls, so a test can assert the uid
	// branch replaced them instead of adding a fifth query.
	searched int
	// albumCovers, labelCovers and subjectCovers are the canned batch cover
	// lookups; coverCalls counts them, so a test can assert a whole group is
	// resolved in one call rather than one per hit.
	albumCovers   map[string]organize.Cover
	labelCovers   map[string]organize.Cover
	subjectCovers map[string]people.Cover
	coverCalls    int
	// coverUIDs records the uids each cover lookup was asked for, keyed by group.
	coverUIDs map[string][]string
	// byUID and friends back the direct lookups.
	byUID        map[string]photos.Photo
	byPPUID      map[string]photos.Photo
	byPPAlias    map[string]photos.Photo
	stacks       map[string][]photos.Photo
	albumsByUID  map[string]organize.Album
	labelsByUID  map[string]organize.Label
	subjectsByID map[string]people.Subject
	markersByID  map[string]people.Marker
}

// SearchAlbums records the query/limit and returns the canned albums or error.
func (f *fakeSearcher) SearchAlbums(_ context.Context, q string, limit int) ([]organize.AlbumCount, error) {
	f.gotQuery, f.gotLimit = q, limit
	f.searched++
	return f.albums, f.err
}

// GetAlbumByUID answers from albumsByUID, reporting a miss as the store's
// not-found sentinel.
func (f *fakeSearcher) GetAlbumByUID(_ context.Context, uid string) (organize.Album, error) {
	if f.err != nil {
		return organize.Album{}, f.err
	}
	album, ok := f.albumsByUID[uid]
	if !ok {
		return organize.Album{}, organize.ErrAlbumNotFound
	}
	return album, nil
}

// GetLabelByUID answers from labelsByUID.
func (f *fakeSearcher) GetLabelByUID(_ context.Context, uid string) (organize.Label, error) {
	label, ok := f.labelsByUID[uid]
	if !ok {
		return organize.Label{}, organize.ErrLabelNotFound
	}
	return label, nil
}

// GetSubjectByUID answers from subjectsByID.
func (f *fakeSearcher) GetSubjectByUID(_ context.Context, uid string) (people.Subject, error) {
	subject, ok := f.subjectsByID[uid]
	if !ok {
		return people.Subject{}, people.ErrSubjectNotFound
	}
	return subject, nil
}

// GetMarkerByUID answers from markersByID.
func (f *fakeSearcher) GetMarkerByUID(_ context.Context, uid string) (people.Marker, error) {
	marker, ok := f.markersByID[uid]
	if !ok {
		return people.Marker{}, people.ErrMarkerNotFound
	}
	return marker, nil
}

// GetByUID answers from byUID.
func (f *fakeSearcher) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	return lookupPhoto(f.byUID, uid)
}

// GetByPhotoprismUID answers from byPPUID.
func (f *fakeSearcher) GetByPhotoprismUID(_ context.Context, uid string) (photos.Photo, error) {
	return lookupPhoto(f.byPPUID, uid)
}

// GetByPhotoprismAlias answers from byPPAlias.
func (f *fakeSearcher) GetByPhotoprismAlias(_ context.Context, uid string) (photos.Photo, error) {
	return lookupPhoto(f.byPPAlias, uid)
}

// ListStackMembers answers from stacks, primary first as the real store does.
func (f *fakeSearcher) ListStackMembers(_ context.Context, stackUID string) ([]photos.Photo, error) {
	return f.stacks[stackUID], nil
}

// lookupPhoto returns the photo stored under uid, or the store's not-found
// sentinel.
func lookupPhoto(m map[string]photos.Photo, uid string) (photos.Photo, error) {
	photo, ok := m[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return photo, nil
}

// AlbumCovers returns the canned album covers, recording the batch it was asked
// for.
func (f *fakeSearcher) AlbumCovers(_ context.Context, uids []string) (map[string]organize.Cover, error) {
	f.recordCovers("albums", uids)
	return f.albumCovers, f.err
}

// LabelCovers returns the canned label covers, recording the batch it was asked for.
func (f *fakeSearcher) LabelCovers(_ context.Context, uids []string) (map[string]organize.Cover, error) {
	f.recordCovers("labels", uids)
	return f.labelCovers, f.err
}

// SubjectCovers returns the canned subject covers, recording the batch it was
// asked for.
func (f *fakeSearcher) SubjectCovers(_ context.Context, uids []string) (map[string]people.Cover, error) {
	f.recordCovers("people", uids)
	return f.subjectCovers, f.err
}

// recordCovers notes one cover lookup: which group, which uids, and one more
// call against the total.
func (f *fakeSearcher) recordCovers(group string, uids []string) {
	f.coverCalls++
	if f.coverUIDs == nil {
		f.coverUIDs = map[string][]string{}
	}
	f.coverUIDs[group] = uids
}

// SearchLabels returns the canned labels or error.
func (f *fakeSearcher) SearchLabels(_ context.Context, _ string, _ int) ([]organize.LabelCount, error) {
	return f.labels, f.err
}

// SearchSubjects returns the canned subjects or error.
func (f *fakeSearcher) SearchSubjects(_ context.Context, _ string, _ int) ([]people.Subject, error) {
	return f.subjects, f.err
}

// Search returns the canned photos or error, ignoring the list params beyond
// what the handler set on them.
func (f *fakeSearcher) Search(_ context.Context, _ photos.ListParams) ([]photos.Photo, error) {
	return f.photos, f.err
}

// passthrough is an auth guard stand-in that admits every request.
func passthrough(next http.Handler) http.Handler { return next }

// errStore is the canned store failure the error paths are driven with.
var errStore = errors.New("boom")

// newTestServer mounts the global-search API backed by f under /api/v1 with the
// given per-group limit (0 uses the package default).
func newTestServer(f *fakeSearcher, limit int) *httptest.Server {
	api := NewAPI(Config{
		Organizer: f, People: f, Photos: f, Limit: limit, RequireAuth: passthrough,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return httptest.NewServer(r)
}

// getGlobal issues a context-aware GET against the global-search endpoint.
func getGlobal(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest %s: %v", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// TestHandleGlobal_grouped verifies the endpoint returns every entity group under
// the grouped envelope and echoes the query.
func TestHandleGlobal_grouped(t *testing.T) {
	t.Parallel()
	cover := "ph-cover"
	f := &fakeSearcher{
		albums:      []organize.AlbumCount{{Album: organize.Album{UID: "al1", Title: "Dovolená"}, PhotoCount: 3}},
		labels:      []organize.LabelCount{{Label: organize.Label{UID: "lb1", Name: "sunset"}, PhotoCount: 7}},
		subjects:    []people.Subject{{UID: "su1", Name: "Tomáš"}},
		photos:      []photos.Photo{{UID: "ph1"}},
		albumCovers: map[string]organize.Cover{"al1": {PhotoUID: cover, FileHash: "hash"}},
	}
	srv := newTestServer(f, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q=dov")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Query != "dov" {
		t.Fatalf("query = %q, want dov", body.Query)
	}
	if len(body.Albums) != 1 || body.Albums[0].UID != "al1" || body.Albums[0].PhotoCount != 3 {
		t.Fatalf("albums = %+v, want one al1/3", body.Albums)
	}
	if body.Albums[0].Cover == nil || *body.Albums[0].Cover != cover {
		t.Fatalf("album cover = %v, want %q", body.Albums[0].Cover, cover)
	}
	if len(body.Labels) != 1 || body.Labels[0].Name != "sunset" || body.Labels[0].PhotoCount != 7 {
		t.Fatalf("labels = %+v, want one sunset/7", body.Labels)
	}
	if len(body.People) != 1 || body.People[0].Name != "Tomáš" {
		t.Fatalf("people = %+v, want one Tomáš", body.People)
	}
	if len(body.Photos) != 1 || body.Photos[0].UID != "ph1" {
		t.Fatalf("photos = %+v, want one ph1", body.Photos)
	}
}

// TestHandleGlobal_stampsEntityCovers verifies every entity group carries the
// photo standing for each hit and the address to draw it from, that a hit with
// no cover carries neither (so the client falls back to its glyph), and that a
// whole group is resolved in one batch call rather than one per row.
func TestHandleGlobal_stampsEntityCovers(t *testing.T) {
	t.Parallel()
	f := &fakeSearcher{
		albums: []organize.AlbumCount{
			{Album: organize.Album{UID: "al1", Title: "Beach"}, PhotoCount: 3},
			{Album: organize.Album{UID: "al2", Title: "Empty"}},
		},
		labels: []organize.LabelCount{
			{Label: organize.Label{UID: "lb1", Name: "sunset"}, PhotoCount: 7},
			{Label: organize.Label{UID: "lb2", Name: "unused"}},
		},
		subjects: []people.Subject{{UID: "su1", Name: "Tomáš"}, {UID: "su2", Name: "Nobody"}},
		albumCovers: map[string]organize.Cover{
			"al1": {PhotoUID: "ph-al", FileHash: "ha"},
		},
		labelCovers: map[string]organize.Cover{
			"lb1": {PhotoUID: "ph-lb", FileHash: "hb"},
		},
		subjectCovers: map[string]people.Cover{
			"su1": {PhotoUID: "ph-su", FileHash: "hc"},
		},
	}
	srv := newTestServer(f, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q=beach")
	defer func() { _ = resp.Body.Close() }()
	var body response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Every kind gets the same pair: the cover uid and a thumbnail address, the
	// latter minted at the medallion size rather than the grid size.
	assertCover(t, "album", body.Albums[0].Cover, body.Albums[0].ThumbURL, "ph-al")
	assertCover(t, "label", body.Labels[0].Cover, body.Labels[0].ThumbURL, "ph-lb")
	assertCover(t, "person", body.People[0].Cover, body.People[0].ThumbURL, "ph-su")

	// …and a hit the lookup had nothing for carries neither half of it.
	assertNoCover(t, "album", body.Albums[1].Cover, body.Albums[1].ThumbURL)
	assertNoCover(t, "label", body.Labels[1].Cover, body.Labels[1].ThumbURL)
	assertNoCover(t, "person", body.People[1].Cover, body.People[1].ThumbURL)

	// One lookup per group, not one per hit: six rows, three calls.
	if f.coverCalls != 3 {
		t.Errorf("cover lookups = %d, want 3 (one per group)", f.coverCalls)
	}
	for group, want := range map[string][]string{
		"albums": {"al1", "al2"}, "labels": {"lb1", "lb2"}, "people": {"su1", "su2"},
	} {
		if !slices.Equal(f.coverUIDs[group], want) {
			t.Errorf("%s cover lookup asked for %v, want the whole batch %v", group, f.coverUIDs[group], want)
		}
	}
}

// assertCover checks a hit carries the expected cover uid and a thumbnail
// address at the medallion size.
func assertCover(t *testing.T, kind string, cover *string, thumbURL, wantUID string) {
	t.Helper()
	if cover == nil || *cover != wantUID {
		t.Errorf("%s cover = %v, want %q", kind, cover, wantUID)
	}
	if !strings.Contains(thumbURL, thumb.AvatarSize) {
		t.Errorf("%s thumb_url = %q, want the %s medallion", kind, thumbURL, thumb.AvatarSize)
	}
}

// assertNoCover checks a hit with nothing to show carries neither half of the
// cover pair, so the client draws its own glyph instead of a broken image.
func assertNoCover(t *testing.T, kind string, cover *string, thumbURL string) {
	t.Helper()
	if cover != nil {
		t.Errorf("%s without a cover has cover = %q, want none", kind, *cover)
	}
	if thumbURL != "" {
		t.Errorf("%s without a cover has thumb_url = %q, want none", kind, thumbURL)
	}
}

// TestHandleGlobal_emptyGroupAsksForNoCovers verifies a group that matched
// nothing is asked for no uids, which is what lets the store answer it without
// touching the database.
func TestHandleGlobal_emptyGroupAsksForNoCovers(t *testing.T) {
	t.Parallel()
	f := &fakeSearcher{}
	srv := newTestServer(f, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q=nothing")
	defer func() { _ = resp.Body.Close() }()
	for group, uids := range f.coverUIDs {
		if len(uids) != 0 {
			t.Errorf("%s cover lookup asked for %v on an empty group", group, uids)
		}
	}
}

// TestHandleGlobal_trimsAndPassesLimit verifies the query is trimmed and the
// configured per-group limit reaches the stores.
func TestHandleGlobal_trimsAndPassesLimit(t *testing.T) {
	t.Parallel()
	f := &fakeSearcher{}
	srv := newTestServer(f, 5)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q=%20%20dovolena%20%20")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if f.gotQuery != "dovolena" {
		t.Fatalf("store saw query %q, want trimmed dovolena", f.gotQuery)
	}
	if f.gotLimit != 5 {
		t.Fatalf("store saw limit %d, want 5", f.gotLimit)
	}
}

// TestHandleGlobal_emptyQuery verifies a blank or whitespace-only q is 400 and no
// store call is made.
func TestHandleGlobal_emptyQuery(t *testing.T) {
	t.Parallel()
	for _, q := range []string{"", "%20%20"} {
		f := &fakeSearcher{}
		srv := newTestServer(f, 0)
		resp := getGlobal(t, srv.URL+"/api/v1/search/global?q="+q)
		if resp.StatusCode != http.StatusBadRequest {
			_ = resp.Body.Close()
			srv.Close()
			t.Fatalf("q=%q status = %d, want 400", q, resp.StatusCode)
		}
		if f.gotQuery != "" {
			_ = resp.Body.Close()
			srv.Close()
			t.Fatalf("store was queried for blank q")
		}
		_ = resp.Body.Close()
		srv.Close()
	}
}

// TestHandleGlobal_storeError verifies a store failure becomes a 500.
func TestHandleGlobal_storeError(t *testing.T) {
	t.Parallel()
	f := &fakeSearcher{err: errors.New("boom")}
	srv := newTestServer(f, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q=dovolena")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

// TestNewAPI_defaultLimit verifies a non-positive configured limit falls back to
// the package default.
func TestNewAPI_defaultLimit(t *testing.T) {
	t.Parallel()
	f := &fakeSearcher{}
	srv := newTestServer(f, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/global?q=x")
	defer func() { _ = resp.Body.Close() }()
	if f.gotLimit != defaultGroupLimit {
		t.Fatalf("store saw limit %d, want default %d", f.gotLimit, defaultGroupLimit)
	}
}
