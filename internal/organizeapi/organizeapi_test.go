package organizeapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/organizeapi"
)

// fakeAlbums is an in-memory AlbumStore for handler tests. The err fields force a
// specific error from the matching method; the recorded fields capture inputs.
type fakeAlbums struct {
	list      []organize.AlbumCount
	album     organize.Album
	created   organize.Album
	updated   organize.Album
	photoUIDs []string

	listErr    error
	getErr     error
	createErr  error
	updateErr  error
	deleteErr  error
	addErr     error
	removeErr  error
	reorderErr error
	photosErr  error

	lastCreate   organize.Album
	lastUpdate   organize.AlbumUpdate
	deletedUID   string
	addedOrders  []int
	addedUIDs    []string
	removedUIDs  []string
	reorderedTo  []string
	getCallCount int
}

func (f *fakeAlbums) ListAlbums(context.Context) ([]organize.AlbumCount, error) {
	return f.list, f.listErr
}

func (f *fakeAlbums) CreateAlbum(_ context.Context, a organize.Album) (organize.Album, error) {
	f.lastCreate = a
	return f.created, f.createErr
}

func (f *fakeAlbums) GetAlbumByUID(context.Context, string) (organize.Album, error) {
	f.getCallCount++
	return f.album, f.getErr
}

func (f *fakeAlbums) UpdateAlbum(_ context.Context, _ string, upd organize.AlbumUpdate) (organize.Album, error) {
	f.lastUpdate = upd
	return f.updated, f.updateErr
}

func (f *fakeAlbums) DeleteAlbum(_ context.Context, uid string) error {
	f.deletedUID = uid
	return f.deleteErr
}

func (f *fakeAlbums) AddPhoto(_ context.Context, _, photoUID string, sortOrder int) error {
	f.addedUIDs = append(f.addedUIDs, photoUID)
	f.addedOrders = append(f.addedOrders, sortOrder)
	return f.addErr
}

func (f *fakeAlbums) RemovePhoto(_ context.Context, _, photoUID string) error {
	f.removedUIDs = append(f.removedUIDs, photoUID)
	return f.removeErr
}

func (f *fakeAlbums) ReorderPhotos(_ context.Context, _ string, ordered []string) error {
	f.reorderedTo = ordered
	return f.reorderErr
}

func (f *fakeAlbums) ListPhotoUIDs(context.Context, string) ([]string, error) {
	return f.photoUIDs, f.photosErr
}

// fakeLabels is an in-memory LabelStore for handler tests.
type fakeLabels struct {
	list    []organize.LabelCount
	label   organize.Label
	created organize.Label
	updated organize.Label

	listErr   error
	getErr    error
	createErr error
	updateErr error
	deleteErr error
	attachErr error
	detachErr error

	lastCreate   organize.Label
	lastUpdate   organize.LabelUpdate
	deletedUID   string
	attachedTo   string
	attachSource organize.LabelSource
	attachUncert int
	detachedFrom string
}

func (f *fakeLabels) ListLabels(context.Context) ([]organize.LabelCount, error) {
	return f.list, f.listErr
}

func (f *fakeLabels) CreateLabel(_ context.Context, l organize.Label) (organize.Label, error) {
	f.lastCreate = l
	return f.created, f.createErr
}

func (f *fakeLabels) GetLabelByUID(context.Context, string) (organize.Label, error) {
	return f.label, f.getErr
}

func (f *fakeLabels) UpdateLabel(_ context.Context, _ string, upd organize.LabelUpdate) (organize.Label, error) {
	f.lastUpdate = upd
	return f.updated, f.updateErr
}

func (f *fakeLabels) DeleteLabel(_ context.Context, uid string) error {
	f.deletedUID = uid
	return f.deleteErr
}

func (f *fakeLabels) AttachLabel(
	_ context.Context, photoUID, _ string, source organize.LabelSource, uncertainty int,
) error {
	f.attachedTo = photoUID
	f.attachSource = source
	f.attachUncert = uncertainty
	return f.attachErr
}

func (f *fakeLabels) DetachLabel(_ context.Context, photoUID, _ string) error {
	f.detachedFrom = photoUID
	return f.detachErr
}

// passThrough is a no-op guard so handler behaviour is tested without auth.
func passThrough(next http.Handler) http.Handler { return next }

// newServer mounts an API backed by the given stores behind pass-through guards.
func newServer(albums organizeapi.AlbumStore, labels organizeapi.LabelStore) http.Handler {
	api := organizeapi.NewAPI(organizeapi.Config{
		Albums:       albums,
		Labels:       labels,
		RequireAuth:  passThrough,
		RequireWrite: passThrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

// do issues a request against the mounted API and returns the recorder.
func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// --- Albums -----------------------------------------------------------------

// TestAlbumList_ok returns the albums with their counts.
func TestAlbumList_ok(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{list: []organize.AlbumCount{
		{Album: organize.Album{UID: "al_a", Title: "Trip"}, PhotoCount: 4},
	}}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodGet, "/albums", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Albums []organize.AlbumCount `json:"albums"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Albums) != 1 || got.Albums[0].PhotoCount != 4 {
		t.Errorf("body mismatch: %+v", got.Albums)
	}
}

// TestAlbumCreate_ok creates an album and forwards the type.
func TestAlbumCreate_ok(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{created: organize.Album{UID: "al_new", Title: "Trip", Slug: "trip"}}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodPost, "/albums",
		`{"title":"Trip","type":"folder","private":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if albums.lastCreate.Title != "Trip" || albums.lastCreate.Type != organize.AlbumFolder ||
		!albums.lastCreate.Private {
		t.Errorf("create input mismatch: %+v", albums.lastCreate)
	}
}

// TestAlbumCreate_emptyTitle rejects a body with no title.
func TestAlbumCreate_emptyTitle(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeAlbums{}, &fakeLabels{}), http.MethodPost, "/albums", `{"title":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestAlbumCreate_unknownField rejects an unexpected JSON field.
func TestAlbumCreate_unknownField(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeAlbums{}, &fakeLabels{}), http.MethodPost, "/albums",
		`{"title":"Trip","bogus":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestAlbumCreate_invalidType maps the type sentinel to 400.
func TestAlbumCreate_invalidType(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{createErr: organize.ErrInvalidType}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodPost, "/albums",
		`{"title":"Trip","type":"mixtape"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestAlbumGet_notFound maps the album sentinel to 404.
func TestAlbumGet_notFound(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{getErr: organize.ErrAlbumNotFound}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodGet, "/albums/al_x", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestAlbumUpdate_preservesType keeps the existing structural type even though
// the body cannot set it.
func TestAlbumUpdate_preservesType(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{
		album:   organize.Album{UID: "al_a", Title: "Trip", Type: organize.AlbumMoment},
		updated: organize.Album{UID: "al_a", Title: "Trip II"},
	}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodPatch, "/albums/al_a",
		`{"title":"Trip II","order_by":"oldest"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if albums.lastUpdate.Type != organize.AlbumMoment || albums.lastUpdate.OrderBy != "oldest" {
		t.Errorf("update input mismatch: %+v", albums.lastUpdate)
	}
}

// TestAlbumUpdate_notFound maps a missing album to 404 before decoding.
func TestAlbumUpdate_notFound(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{getErr: organize.ErrAlbumNotFound}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodPatch, "/albums/al_x", `{"title":"X"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestAlbumDelete_ok answers 204 and records the deleted UID.
func TestAlbumDelete_ok(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodDelete, "/albums/al_a", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if albums.deletedUID != "al_a" {
		t.Errorf("deleted uid = %q, want al_a", albums.deletedUID)
	}
}

// TestAlbumAddPhotos_appendsAfterExisting positions new photos after the ones
// already in the album and echoes the refreshed order.
func TestAlbumAddPhotos_appendsAfterExisting(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{photoUIDs: []string{"p1", "p2"}}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodPost, "/albums/al_a/photos",
		`{"photo_uids":["p3","p4"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(albums.addedOrders) != 2 || albums.addedOrders[0] != 2 || albums.addedOrders[1] != 3 {
		t.Errorf("sort orders = %v, want [2 3]", albums.addedOrders)
	}
	if albums.addedUIDs[0] != "p3" || albums.addedUIDs[1] != "p4" {
		t.Errorf("added uids = %v, want [p3 p4]", albums.addedUIDs)
	}
}

// TestAlbumAddPhotos_emptyList rejects a body with no photo UIDs.
func TestAlbumAddPhotos_emptyList(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeAlbums{}, &fakeLabels{}), http.MethodPost, "/albums/al_a/photos",
		`{"photo_uids":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestAlbumAddPhotos_albumNotFound maps a missing album to 404.
func TestAlbumAddPhotos_albumNotFound(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{getErr: organize.ErrAlbumNotFound}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodPost, "/albums/al_x/photos",
		`{"photo_uids":["p1"]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestAlbumAddPhotos_photoNotFound maps a missing photo to 404.
func TestAlbumAddPhotos_photoNotFound(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{addErr: organize.ErrPhotoNotFound}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodPost, "/albums/al_a/photos",
		`{"photo_uids":["p1"]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestAlbumRemovePhotos_ok removes the requested photos and echoes the order.
func TestAlbumRemovePhotos_ok(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{photoUIDs: []string{"p2"}}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodDelete, "/albums/al_a/photos",
		`{"photo_uids":["p1"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(albums.removedUIDs) != 1 || albums.removedUIDs[0] != "p1" {
		t.Errorf("removed uids = %v, want [p1]", albums.removedUIDs)
	}
}

// TestAlbumReorder_ok forwards the new order and echoes the refreshed order.
func TestAlbumReorder_ok(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{photoUIDs: []string{"p3", "p1", "p2"}}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodPatch, "/albums/al_a/order",
		`{"photo_uids":["p3","p1","p2"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Join(albums.reorderedTo, ",") != "p3,p1,p2" {
		t.Errorf("reordered to %v, want [p3 p1 p2]", albums.reorderedTo)
	}
	var got struct {
		PhotoUIDs []string `json:"photo_uids"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if strings.Join(got.PhotoUIDs, ",") != "p3,p1,p2" {
		t.Errorf("response order = %v, want [p3 p1 p2]", got.PhotoUIDs)
	}
}

// TestAlbumReorder_notFound maps a missing album to 404.
func TestAlbumReorder_notFound(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{reorderErr: organize.ErrAlbumNotFound}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodPatch, "/albums/al_x/order",
		`{"photo_uids":["p1"]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// --- Labels -----------------------------------------------------------------

// TestLabelList_ok returns the labels with their counts.
func TestLabelList_ok(t *testing.T) {
	t.Parallel()
	labels := &fakeLabels{list: []organize.LabelCount{
		{Label: organize.Label{UID: "lb_a", Name: "Beach"}, PhotoCount: 7},
	}}
	rec := do(t, newServer(&fakeAlbums{}, labels), http.MethodGet, "/labels", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Labels []organize.LabelCount `json:"labels"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0].PhotoCount != 7 {
		t.Errorf("body mismatch: %+v", got.Labels)
	}
}

// TestLabelCreate_ok creates a label and forwards the priority.
func TestLabelCreate_ok(t *testing.T) {
	t.Parallel()
	labels := &fakeLabels{created: organize.Label{UID: "lb_new", Name: "Beach", Slug: "beach"}}
	rec := do(t, newServer(&fakeAlbums{}, labels), http.MethodPost, "/labels",
		`{"name":"Beach","priority":5}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if labels.lastCreate.Name != "Beach" || labels.lastCreate.Priority != 5 {
		t.Errorf("create input mismatch: %+v", labels.lastCreate)
	}
}

// TestLabelCreate_emptyName rejects a body with no name.
func TestLabelCreate_emptyName(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeAlbums{}, &fakeLabels{}), http.MethodPost, "/labels", `{"name":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestLabelUpdate_ok forwards the editable fields.
func TestLabelUpdate_ok(t *testing.T) {
	t.Parallel()
	labels := &fakeLabels{updated: organize.Label{UID: "lb_a", Name: "Sea"}}
	rec := do(t, newServer(&fakeAlbums{}, labels), http.MethodPatch, "/labels/lb_a",
		`{"name":"Sea","priority":2}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if labels.lastUpdate.Name != "Sea" || labels.lastUpdate.Priority != 2 {
		t.Errorf("update input mismatch: %+v", labels.lastUpdate)
	}
}

// TestLabelDelete_ok answers 204 and records the deleted UID.
func TestLabelDelete_ok(t *testing.T) {
	t.Parallel()
	labels := &fakeLabels{}
	rec := do(t, newServer(&fakeAlbums{}, labels), http.MethodDelete, "/labels/lb_a", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if labels.deletedUID != "lb_a" {
		t.Errorf("deleted uid = %q, want lb_a", labels.deletedUID)
	}
}

// TestLabelAttach_ok attaches the label to the photo with its source/uncertainty.
func TestLabelAttach_ok(t *testing.T) {
	t.Parallel()
	labels := &fakeLabels{}
	rec := do(t, newServer(&fakeAlbums{}, labels), http.MethodPost, "/labels/lb_a/photos",
		`{"photo_uid":"ph_1","source":"ai","uncertainty":20}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if labels.attachedTo != "ph_1" || labels.attachSource != organize.SourceAI || labels.attachUncert != 20 {
		t.Errorf("attach input mismatch: to=%q source=%q uncert=%d",
			labels.attachedTo, labels.attachSource, labels.attachUncert)
	}
}

// TestLabelAttach_missingPhotoUID rejects a body with no photo UID.
func TestLabelAttach_missingPhotoUID(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeAlbums{}, &fakeLabels{}), http.MethodPost, "/labels/lb_a/photos",
		`{"source":"manual"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestLabelAttach_invalidSource maps the source sentinel to 400.
func TestLabelAttach_invalidSource(t *testing.T) {
	t.Parallel()
	labels := &fakeLabels{attachErr: organize.ErrInvalidSource}
	rec := do(t, newServer(&fakeAlbums{}, labels), http.MethodPost, "/labels/lb_a/photos",
		`{"photo_uid":"ph_1","source":"telepathy"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestLabelAttach_labelNotFound maps a missing label to 404.
func TestLabelAttach_labelNotFound(t *testing.T) {
	t.Parallel()
	labels := &fakeLabels{attachErr: organize.ErrLabelNotFound}
	rec := do(t, newServer(&fakeAlbums{}, labels), http.MethodPost, "/labels/lb_x/photos",
		`{"photo_uid":"ph_1"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestLabelDetach_ok detaches the label from the photo.
func TestLabelDetach_ok(t *testing.T) {
	t.Parallel()
	labels := &fakeLabels{}
	rec := do(t, newServer(&fakeAlbums{}, labels), http.MethodDelete, "/labels/lb_a/photos",
		`{"photo_uid":"ph_1"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if labels.detachedFrom != "ph_1" {
		t.Errorf("detached from %q, want ph_1", labels.detachedFrom)
	}
}

// TestLabelDetach_labelNotFound maps a missing label to 404 before detaching.
func TestLabelDetach_labelNotFound(t *testing.T) {
	t.Parallel()
	labels := &fakeLabels{getErr: organize.ErrLabelNotFound}
	rec := do(t, newServer(&fakeAlbums{}, labels), http.MethodDelete, "/labels/lb_x/photos",
		`{"photo_uid":"ph_1"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
