package sidecarjob

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/sidecarexport"
)

// fakePhotos is a PhotoStore over an in-memory photo.
type fakePhotos struct {
	photo    photos.Photo
	getErr   error
	edit     *photos.Edit
	markErr  error
	marked   []string
	getCalls int
}

func (f *fakePhotos) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	f.getCalls++
	if f.getErr != nil {
		return photos.Photo{}, f.getErr
	}
	if f.photo.UID != uid {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return f.photo, nil
}

func (f *fakePhotos) GetEdit(_ context.Context, _ string) (photos.Edit, error) {
	if f.edit == nil {
		return photos.Edit{}, photos.ErrEditNotFound
	}
	return *f.edit, nil
}

func (f *fakePhotos) MarkSidecarWritten(_ context.Context, uid string) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.marked = append(f.marked, uid)
	return nil
}

// fakeOrganize is an Organizer returning fixed curation.
type fakeOrganize struct {
	albums    []organize.Album
	labels    []organize.PhotoLabel
	favorites []organize.UserFavorite
	ratings   []organize.UserRating
	err       error
}

func (f fakeOrganize) AlbumsForPhoto(context.Context, string) ([]organize.Album, error) {
	return f.albums, f.err
}

func (f fakeOrganize) PhotoLabelsForPhoto(context.Context, string) ([]organize.PhotoLabel, error) {
	return f.labels, f.err
}

func (f fakeOrganize) FavoritesForPhoto(context.Context, string) ([]organize.UserFavorite, error) {
	return f.favorites, f.err
}

func (f fakeOrganize) RatingsForPhoto(context.Context, string) ([]organize.UserRating, error) {
	return f.ratings, f.err
}

// fakePeople is a PeopleStore returning fixed markers.
type fakePeople struct {
	markers []people.MarkerSubject
	err     error
}

func (f fakePeople) ListMarkersWithSubjects(context.Context, string) ([]people.MarkerSubject, error) {
	return f.markers, f.err
}

// fakePlaces is a PlaceStore returning a fixed place.
type fakePlaces struct {
	place *places.Place
	err   error
}

func (f fakePlaces) GetPlace(context.Context, string) (places.Place, error) {
	if f.err != nil {
		return places.Place{}, f.err
	}
	if f.place == nil {
		return places.Place{}, places.ErrPlaceNotFound
	}
	return *f.place, nil
}

// fakeUsers is a UserStore resolving one user.
type fakeUsers struct {
	user auth.User
	err  error
}

func (f fakeUsers) GetUserByUID(context.Context, string) (auth.User, error) {
	return f.user, f.err
}

// fakeWriter records the documents it is asked to write.
type fakeWriter struct {
	written  map[string]sidecarexport.Document
	deleted  []string
	writeErr error
	delErr   error
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{written: map[string]sidecarexport.Document{}}
}

func (f *fakeWriter) Write(
	_ context.Context, fileKey string, doc sidecarexport.Document,
) (string, error) {
	if f.writeErr != nil {
		return "", f.writeErr
	}
	f.written[fileKey] = doc
	return "sidecars/" + fileKey + ".yml", nil
}

func (f *fakeWriter) Delete(_ context.Context, fileKey string) error {
	if f.delErr != nil {
		return f.delErr
	}
	f.deleted = append(f.deleted, fileKey)
	return nil
}

// fakeLister is a PhotoLister returning fixed uid lists.
type fakeLister struct {
	pending []string
	active  []string
	err     error
}

func (f fakeLister) ListPhotosMissingSidecar(context.Context, int) ([]string, error) {
	return f.pending, f.err
}

func (f fakeLister) ListActiveUIDs(context.Context) ([]string, error) {
	return f.active, f.err
}

// fakeEnqueuer records the uids it was asked to schedule.
type fakeEnqueuer struct {
	uids []string
	err  error
}

func (f *fakeEnqueuer) EnqueueSidecar(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	f.uids = append(f.uids, uid)
	return nil
}

// quietLogger returns a logger that discards output, so a test's expected skip
// does not spam the run.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestService builds a Service over the given fakes, defaulting the ones the
// test does not care about.
func newTestService(t *testing.T, cfg Config) *Service {
	t.Helper()

	if cfg.Photos == nil {
		cfg.Photos = &fakePhotos{photo: photos.Photo{UID: "pht1", FilePath: "2024/05/a.jpg"}}
	}
	if cfg.Organize == nil {
		cfg.Organize = fakeOrganize{}
	}
	if cfg.People == nil {
		cfg.People = fakePeople{}
	}
	if cfg.Writer == nil {
		cfg.Writer = newFakeWriter()
	}
	if cfg.Logger == nil {
		cfg.Logger = quietLogger()
	}
	return New(cfg)
}

// TestNew_requiresCollaborators asserts a missing required dependency fails at
// startup rather than as a nil dereference on the first job.
func TestNew_requiresCollaborators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{"no photos", Config{Organize: fakeOrganize{}, People: fakePeople{}, Writer: newFakeWriter()}},
		{"no organize", Config{Photos: &fakePhotos{}, People: fakePeople{}, Writer: newFakeWriter()}},
		{"no people", Config{Photos: &fakePhotos{}, Organize: fakeOrganize{}, Writer: newFakeWriter()}},
		{"no writer", Config{Photos: &fakePhotos{}, Organize: fakeOrganize{}, People: fakePeople{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("New did not panic on a missing required dependency")
				}
			}()
			New(tt.cfg)
		})
	}
}

// TestHandle_writesSidecarAndStampsMarker is the handler's happy path: it
// serialises the photo and only then records that the file exists.
func TestHandle_writesSidecarAndStampsMarker(t *testing.T) {
	t.Parallel()

	photoStore := &fakePhotos{photo: photos.Photo{UID: "pht1", FilePath: "2024/05/a.jpg", Title: "Svatba"}}
	writer := newFakeWriter()
	svc := newTestService(t, Config{Photos: photoStore, Writer: writer})

	if err := svc.Handle(context.Background(), sidecarJob("pht1")); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	doc, ok := writer.written["2024/05/a.jpg"]
	if !ok {
		t.Fatalf("nothing written; writer holds %v", writer.written)
	}
	if doc.Descriptive.Title != "Svatba" {
		t.Errorf("written title = %q, want Svatba", doc.Descriptive.Title)
	}
	if len(photoStore.marked) != 1 || photoStore.marked[0] != "pht1" {
		t.Errorf("marked = %v, want [pht1]", photoStore.marked)
	}
}

// TestHandle_missingPhotoUID rejects a malformed payload so it dead-letters
// rather than retrying forever.
func TestHandle_missingPhotoUID(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, Config{})
	err := svc.Handle(context.Background(), jobs.Job{Type: jobs.TypeSidecar, Payload: []byte(`{}`)})
	if !errors.Is(err, ErrMissingPhotoUID) {
		t.Errorf("Handle error = %v, want ErrMissingPhotoUID", err)
	}
}

// TestHandle_malformedPayload likewise errors rather than panicking.
func TestHandle_malformedPayload(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, Config{})
	err := svc.Handle(context.Background(), jobs.Job{Type: jobs.TypeSidecar, Payload: []byte(`not json`)})
	if err == nil {
		t.Error("Handle returned nil for a malformed payload, want an error")
	}
}

// TestExport_missingPhotoIsSkipped asserts a photo purged between the enqueue and
// the run is a skip, not a failure. Failing would dead-letter the job and make a
// library-wide backfill look broken over a race the queue is meant to lose
// gracefully.
func TestExport_missingPhotoIsSkipped(t *testing.T) {
	t.Parallel()

	writer := newFakeWriter()
	svc := newTestService(t, Config{
		Photos: &fakePhotos{photo: photos.Photo{UID: "other"}},
		Writer: writer,
	})
	if err := svc.Export(context.Background(), "gone"); err != nil {
		t.Errorf("Export of a purged photo returned %v, want nil", err)
	}
	if len(writer.written) != 0 {
		t.Errorf("wrote %v for a photo that does not exist", writer.written)
	}
}

// TestExport_writeFailureDoesNotStampMarker is the ordering that makes the
// backfill trustworthy: if the file did not land, the photo must stay pending.
// Stamping first would mark a photo as exported whose sidecar does not exist, and
// nothing would ever write it.
func TestExport_writeFailureDoesNotStampMarker(t *testing.T) {
	t.Parallel()

	photoStore := &fakePhotos{photo: photos.Photo{UID: "pht1", FilePath: "2024/05/a.jpg"}}
	writer := newFakeWriter()
	writer.writeErr = errors.New("bucket unreachable")
	svc := newTestService(t, Config{Photos: photoStore, Writer: writer})

	if err := svc.Export(context.Background(), "pht1"); err == nil {
		t.Fatal("Export returned nil despite a failed write")
	}
	if len(photoStore.marked) != 0 {
		t.Errorf("marked = %v, want none — a photo whose sidecar failed must stay pending",
			photoStore.marked)
	}
}

// TestExport_isRepeatable asserts running the handler twice writes the same
// document. It is what makes the queue's dedup a safe debounce: a coalesced job
// loses nothing, because the job writes the current truth rather than a delta.
func TestExport_isRepeatable(t *testing.T) {
	t.Parallel()

	writer := newFakeWriter()
	svc := newTestService(t, Config{
		Photos: &fakePhotos{photo: photos.Photo{UID: "pht1", FilePath: "2024/05/a.jpg", Title: "x"}},
		Writer: writer,
	})
	for range 3 {
		if err := svc.Export(context.Background(), "pht1"); err != nil {
			t.Fatalf("Export returned error: %v", err)
		}
	}
	if len(writer.written) != 1 {
		t.Errorf("wrote %d distinct keys, want 1", len(writer.written))
	}
}

// TestExport_gathersCuration asserts every collaborator's curation reaches the
// document — the point of the whole feature.
func TestExport_gathersCuration(t *testing.T) {
	t.Parallel()

	writer := newFakeWriter()
	uploader := "usr1"
	svc := newTestService(t, Config{
		Photos: &fakePhotos{
			photo: photos.Photo{UID: "pht1", FilePath: "2024/05/a.jpg", UploadedBy: &uploader},
			edit:  &photos.Edit{PhotoUID: "pht1", Rotation: 90},
		},
		Organize: fakeOrganize{
			albums:    []organize.Album{{UID: "alb1", Title: "Svatba"}},
			labels:    []organize.PhotoLabel{{Label: organize.Label{UID: "lbl1", Name: "Portrét"}}},
			favorites: []organize.UserFavorite{{UserUID: "usr1", Username: "pan.botka"}},
			ratings:   []organize.UserRating{{UserUID: "usr1", Username: "pan.botka", Rating: 5}},
		},
		People: fakePeople{markers: []people.MarkerSubject{{
			Marker: people.Marker{UID: "mrk1", W: 0.1, H: 0.1}, SubjectName: "Jana",
		}}},
		Places: fakePlaces{place: &places.Place{City: "Praha"}},
		Users:  fakeUsers{user: auth.User{UID: "usr1", Username: "pan.botka"}},
		Writer: writer,
	})

	if err := svc.Export(context.Background(), "pht1"); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	doc := writer.written["2024/05/a.jpg"]
	if len(doc.Curation.Albums) != 1 || len(doc.Curation.Labels) != 1 || len(doc.Curation.People) != 1 {
		t.Errorf("curation incomplete: %+v", doc.Curation)
	}
	if len(doc.Curation.Favorites) != 1 || len(doc.Curation.Ratings) != 1 {
		t.Errorf("per-user curation incomplete: %+v", doc.Curation)
	}
	if doc.Spatial == nil || doc.Spatial.Place == nil || doc.Spatial.Place.City != "Praha" {
		t.Errorf("place missing: %+v", doc.Spatial)
	}
	if doc.Edit == nil || doc.Edit.Rotation != 90 {
		t.Errorf("edit missing: %+v", doc.Edit)
	}
	if doc.Identity.UploadedBy != "pan.botka" {
		t.Errorf("UploadedBy = %q, want pan.botka", doc.Identity.UploadedBy)
	}
}

// TestExport_curationReadFailureIsAnError asserts a failure to read curation
// fails the job rather than writing a sidecar that quietly omits it. A file that
// claims a photo has no albums, because the album query errored, is worse than no
// file: it looks authoritative.
func TestExport_curationReadFailureIsAnError(t *testing.T) {
	t.Parallel()

	writer := newFakeWriter()
	svc := newTestService(t, Config{
		Photos:   &fakePhotos{photo: photos.Photo{UID: "pht1", FilePath: "2024/05/a.jpg"}},
		Organize: fakeOrganize{err: errors.New("db down")},
		Writer:   writer,
	})
	if err := svc.Export(context.Background(), "pht1"); err == nil {
		t.Fatal("Export returned nil despite a failed curation read")
	}
	if len(writer.written) != 0 {
		t.Errorf("wrote %v despite a failed curation read", writer.written)
	}
}

// TestExport_uploaderFailureIsNotFatal asserts an unresolvable uploader costs the
// uploader's name, not the sidecar. Who uploaded a photo is a nice-to-have; the
// curation is not, and one must not cost the other.
func TestExport_uploaderFailureIsNotFatal(t *testing.T) {
	t.Parallel()

	writer := newFakeWriter()
	uploader := "deleted-user"
	svc := newTestService(t, Config{
		Photos: &fakePhotos{photo: photos.Photo{UID: "pht1", FilePath: "2024/05/a.jpg", UploadedBy: &uploader}},
		Users:  fakeUsers{err: errors.New("no such user")},
		Writer: writer,
	})
	if err := svc.Export(context.Background(), "pht1"); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if doc, ok := writer.written["2024/05/a.jpg"]; !ok || doc.Identity.UploadedBy != "" {
		t.Errorf("want the sidecar written with an empty uploader, got %+v (present=%v)", doc, ok)
	}
}

// TestExport_worksWithoutOptionalCollaborators asserts the optional stores degrade
// to an omitted group rather than a crash.
func TestExport_worksWithoutOptionalCollaborators(t *testing.T) {
	t.Parallel()

	writer := newFakeWriter()
	svc := newTestService(t, Config{
		Photos: &fakePhotos{photo: photos.Photo{UID: "pht1", FilePath: "2024/05/a.jpg"}},
		Writer: writer,
	})
	if err := svc.Export(context.Background(), "pht1"); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if _, ok := writer.written["2024/05/a.jpg"]; !ok {
		t.Error("no sidecar written without the optional stores")
	}
}

// TestRemove_deletesSidecar covers the purge path: the sidecar goes with the
// photo, because one left behind is what a rebuild reads to resurrect a photo the
// user deleted.
func TestRemove_deletesSidecar(t *testing.T) {
	t.Parallel()

	writer := newFakeWriter()
	svc := newTestService(t, Config{Writer: writer})
	if err := svc.Remove(context.Background(), "2024/05/a.jpg"); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if len(writer.deleted) != 1 || writer.deleted[0] != "2024/05/a.jpg" {
		t.Errorf("deleted = %v, want [2024/05/a.jpg]", writer.deleted)
	}
}

// TestBackfillSidecars_enqueuesPending schedules the photos whose sidecar is
// missing or stale.
func TestBackfillSidecars_enqueuesPending(t *testing.T) {
	t.Parallel()

	enq := &fakeEnqueuer{}
	svc := newTestService(t, Config{
		Lister:   fakeLister{pending: []string{"a", "b", "c"}, active: []string{"a", "b", "c", "d"}},
		Enqueuer: enq,
	})
	got, err := svc.BackfillSidecars(context.Background(), false)
	if err != nil {
		t.Fatalf("BackfillSidecars returned error: %v", err)
	}
	if got != 3 {
		t.Errorf("enqueued = %d, want 3", got)
	}
	if len(enq.uids) != 3 {
		t.Errorf("scheduled %v, want the three pending photos", enq.uids)
	}
}

// TestBackfillSidecars_allForcesFullRun asserts ?all=true schedules every
// non-archived photo. It is what recovers curation that changed without touching
// the photo row — an album membership, a label — and so does not look stale.
func TestBackfillSidecars_allForcesFullRun(t *testing.T) {
	t.Parallel()

	enq := &fakeEnqueuer{}
	svc := newTestService(t, Config{
		Lister:   fakeLister{pending: []string{"a"}, active: []string{"a", "b", "c", "d"}},
		Enqueuer: enq,
	})
	got, err := svc.BackfillSidecars(context.Background(), true)
	if err != nil {
		t.Fatalf("BackfillSidecars returned error: %v", err)
	}
	if got != 4 {
		t.Errorf("enqueued = %d, want 4 (every active photo)", got)
	}
}

// TestBackfillSidecars_drainedLibraryIsIdempotent asserts a run over a library
// whose sidecars are all current schedules nothing. This is what makes the
// backfill safe to run from cron or before every risky operation.
func TestBackfillSidecars_drainedLibraryIsIdempotent(t *testing.T) {
	t.Parallel()

	enq := &fakeEnqueuer{}
	svc := newTestService(t, Config{Lister: fakeLister{pending: nil}, Enqueuer: enq})
	got, err := svc.BackfillSidecars(context.Background(), false)
	if err != nil {
		t.Fatalf("BackfillSidecars returned error: %v", err)
	}
	if got != 0 || len(enq.uids) != 0 {
		t.Errorf("enqueued %d (%v) over a drained library, want 0", got, enq.uids)
	}
}

// TestBackfillSidecars_unavailableWithoutWiring asserts a service built without a
// lister or enqueuer reports the backfill unavailable rather than silently
// reporting success.
func TestBackfillSidecars_unavailableWithoutWiring(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, Config{})
	if _, err := svc.BackfillSidecars(context.Background(), false); !errors.Is(err, ErrBackfillUnavailable) {
		t.Errorf("BackfillSidecars error = %v, want ErrBackfillUnavailable", err)
	}
}

// TestBackfillSidecars_reportsPartialProgressOnFailure asserts a mid-run enqueue
// failure returns how many were scheduled, so an operator knows the run was
// partial rather than assuming nothing happened.
func TestBackfillSidecars_reportsPartialProgressOnFailure(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, Config{
		Lister:   fakeLister{pending: []string{"a", "b"}},
		Enqueuer: &fakeEnqueuer{err: errors.New("queue down")},
	})
	got, err := svc.BackfillSidecars(context.Background(), false)
	if err == nil {
		t.Fatal("BackfillSidecars returned nil despite a queue failure")
	}
	if got != 0 {
		t.Errorf("enqueued = %d, want 0 (the first enqueue failed)", got)
	}
}

// sidecarJob builds a sidecar job carrying photoUID, as the enqueuer would.
func sidecarJob(photoUID string) jobs.Job {
	payload, err := json.Marshal(map[string]string{"photo_uid": photoUID})
	if err != nil {
		panic(err)
	}
	return jobs.Job{Type: jobs.TypeSidecar, Payload: payload}
}
