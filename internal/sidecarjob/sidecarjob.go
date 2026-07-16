// Package sidecarjob is the queue handler that keeps every photo's metadata
// sidecar current, plus the library-wide backfill that writes the ones that are
// missing.
//
// It is the scheduled half of internal/sidecarexport: that package knows the
// format and how to put it in storage, this one knows when. A `sidecar` job is
// enqueued by every mutation that changes a photo's metadata or curation; the
// handler re-reads the photo and rewrites its file. Doing it here rather than in
// the request is what keeps a 500-photo bulk edit from turning into 500
// synchronous writes to an object store the user is waiting on.
//
// The handler is idempotent and stateless: it reads the photo as it is now and
// writes the file as it should be now. Running it twice writes the same bytes
// twice; running it late writes the current state, not the state that triggered
// it. That is why a lost or coalesced job costs nothing and why the queue's
// per-photo dedup is a safe debounce rather than a dropped update.
//
// Do not confuse it with the "sidecar" of internal/embedjob and internal/facejob,
// which is the machine-learning service on the box. This package writes files.
package sidecarjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/sidecarexport"
)

// ErrMissingPhotoUID indicates a sidecar job whose payload carries no photo_uid.
// It is a malformed job rather than a transient failure, so it is returned as an
// error and dead-letters rather than retrying forever.
var ErrMissingPhotoUID = errors.New("sidecarjob: job payload has no photo_uid")

// ErrBackfillUnavailable indicates a backfill requested on a service built
// without a photo lister or an enqueuer.
var ErrBackfillUnavailable = errors.New("sidecarjob: backfill not available")

// PhotoStore reads the catalogue row a sidecar describes and records that the
// file was written. It is satisfied by photos.Store.
type PhotoStore interface {
	// GetByUID returns the photo, or ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
	// GetEdit returns the photo's non-destructive edit, or ErrEditNotFound when it
	// has none.
	GetEdit(ctx context.Context, photoUID string) (photos.Edit, error)
	// MarkSidecarWritten stamps the export marker after the file has landed.
	MarkSidecarWritten(ctx context.Context, uid string) error
}

// Organizer reads the curation that lives in the organize store.
type Organizer interface {
	// AlbumsForPhoto returns the photo's album memberships.
	AlbumsForPhoto(ctx context.Context, photoUID string) ([]organize.Album, error)
	// PhotoLabelsForPhoto returns the photo's labels with their provenance.
	PhotoLabelsForPhoto(ctx context.Context, photoUID string) ([]organize.PhotoLabel, error)
	// FavoritesForPhoto returns every user who favorited the photo.
	FavoritesForPhoto(ctx context.Context, photoUID string) ([]organize.UserFavorite, error)
	// RatingsForPhoto returns every user's rating of the photo.
	RatingsForPhoto(ctx context.Context, photoUID string) ([]organize.UserRating, error)
}

// PeopleStore reads the photo's face markers with the names assigned to them.
type PeopleStore interface {
	// ListMarkersWithSubjects returns the photo's markers with subject names.
	ListMarkersWithSubjects(ctx context.Context, photoUID string) ([]people.MarkerSubject, error)
}

// PlaceStore reads the photo's cached reverse-geocoded place.
type PlaceStore interface {
	// GetPlace returns the cached place, or places.ErrPlaceNotFound.
	GetPlace(ctx context.Context, photoUID string) (places.Place, error)
}

// UserStore resolves a user UID to a username, so the sidecar records who
// uploaded a photo by a name rather than by an identifier a rebuild will not
// recognise. It is satisfied by auth.Store.
type UserStore interface {
	// GetUserByUID returns the user, or an error when there is none.
	GetUserByUID(ctx context.Context, uid string) (auth.User, error)
}

// SidecarWriter renders and stores a document. It is satisfied by
// sidecarexport.Writer.
type SidecarWriter interface {
	// Write stores doc as the sidecar of the original at fileKey and returns the
	// key written.
	Write(ctx context.Context, fileKey string, doc sidecarexport.Document) (string, error)
	// Delete removes the sidecar of the original at fileKey. An absent sidecar is
	// not an error.
	Delete(ctx context.Context, fileKey string) error
}

// PhotoLister lists the photos a backfill should schedule.
type PhotoLister interface {
	// ListPhotosMissingSidecar returns the uids whose sidecar is missing or stale.
	ListPhotosMissingSidecar(ctx context.Context, limit int) ([]string, error)
	// ListActiveUIDs returns every non-archived photo, for a forced full re-run.
	ListActiveUIDs(ctx context.Context) ([]string, error)
}

// Enqueuer schedules sidecar jobs, used by the backfill.
type Enqueuer interface {
	// EnqueueSidecar schedules a sidecar write for photoUID.
	EnqueueSidecar(ctx context.Context, photoUID string) error
}

// Config bundles the dependencies of New. Photos, Organize, People and Writer are
// required. Places, Users, Lister and Enqueuer are optional: without Places the
// cached place is omitted, without Users the uploader is recorded by UID only,
// and without Lister or Enqueuer the backfill answers ErrBackfillUnavailable.
type Config struct {
	// Photos reads the catalogue row and stamps the export marker.
	Photos PhotoStore
	// Organize reads albums, labels, favorites and ratings.
	Organize Organizer
	// People reads face markers and their subjects.
	People PeopleStore
	// Places reads the cached reverse-geocoded place.
	Places PlaceStore
	// Users resolves the uploader's username.
	Users UserStore
	// Writer renders and stores the document.
	Writer SidecarWriter
	// Lister lists the photos the backfill schedules.
	Lister PhotoLister
	// Enqueuer schedules the jobs the backfill creates.
	Enqueuer Enqueuer
	// Logger receives the skips. Defaults to slog.Default().
	Logger *slog.Logger
}

// Service writes photos' metadata sidecars.
type Service struct {
	photos   PhotoStore
	organize Organizer
	people   PeopleStore
	places   PlaceStore
	users    UserStore
	writer   SidecarWriter
	lister   PhotoLister
	enqueuer Enqueuer
	log      *slog.Logger
}

// New returns a Service from cfg. It panics when a required dependency is
// missing, which is a wiring bug and should fail at startup rather than on the
// first job.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Organize == nil || cfg.People == nil || cfg.Writer == nil {
		panic("sidecarjob: Photos, Organize, People and Writer are required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		photos:   cfg.Photos,
		organize: cfg.Organize,
		people:   cfg.People,
		places:   cfg.Places,
		users:    cfg.Users,
		writer:   cfg.Writer,
		lister:   cfg.Lister,
		enqueuer: cfg.Enqueuer,
		log:      log,
	}
}

// jobPayload is the JSON shape of a sidecar job's payload.
type jobPayload struct {
	PhotoUID string `json:"photo_uid"`
}

// Handle runs one sidecar job: it decodes the payload and rewrites the photo's
// sidecar. It returns ErrMissingPhotoUID for a malformed payload.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var p jobPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("sidecarjob: decoding payload: %w", err)
	}
	if p.PhotoUID == "" {
		return ErrMissingPhotoUID
	}
	return s.Export(ctx, p.PhotoUID)
}

// Export writes the current sidecar of the photo identified by photoUID and
// stamps its export marker.
//
// A photo that no longer exists is a logged skip rather than a failure: the row
// is gone, re-reading it will never succeed, and failing the job would only
// dead-letter it and make a library-wide backfill look broken. The photo was
// almost certainly purged between the enqueue and the run, which is a race the
// queue is expected to lose gracefully.
func (s *Service) Export(ctx context.Context, photoUID string) error {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		if errors.Is(err, photos.ErrPhotoNotFound) {
			s.log.WarnContext(ctx, "sidecar export skipped: photo is gone",
				slog.String("photo_uid", photoUID))
			return nil
		}
		return fmt.Errorf("sidecarjob: loading photo %s: %w", photoUID, err)
	}
	in, err := s.gather(ctx, photo)
	if err != nil {
		return err
	}
	if _, err := s.writer.Write(ctx, photo.FilePath, sidecarexport.Build(in)); err != nil {
		return fmt.Errorf("sidecarjob: writing sidecar for %s: %w", photoUID, err)
	}
	if err := s.photos.MarkSidecarWritten(ctx, photoUID); err != nil {
		// The file is already in storage, which is what matters; only the marker
		// failed. Leaving it unstamped makes the backfill rewrite it later, which is
		// harmless — the alternative, failing the job, would rewrite it anyway.
		return fmt.Errorf("sidecarjob: stamping sidecar marker for %s: %w", photoUID, err)
	}
	return nil
}

// Remove deletes the sidecar of the photo whose original is stored at fileKey. It
// is called when a photo is purged: a sidecar that outlives its photo is not a
// harmless leftover but a rebuild that resurrects something the user deleted.
func (s *Service) Remove(ctx context.Context, fileKey string) error {
	if err := s.writer.Delete(ctx, fileKey); err != nil {
		return fmt.Errorf("sidecarjob: removing sidecar for %s: %w", fileKey, err)
	}
	return nil
}

// gather collects the photo's curation into a sidecarexport.Input. Each
// collaborator is consulted in turn; the optional ones degrade to an omitted
// group rather than an error.
func (s *Service) gather(ctx context.Context, photo photos.Photo) (sidecarexport.Input, error) {
	in := sidecarexport.Input{Photo: photo}
	var err error
	if in.Albums, err = s.organize.AlbumsForPhoto(ctx, photo.UID); err != nil {
		return sidecarexport.Input{}, fmt.Errorf("sidecarjob: reading albums of %s: %w", photo.UID, err)
	}
	if in.Labels, err = s.organize.PhotoLabelsForPhoto(ctx, photo.UID); err != nil {
		return sidecarexport.Input{}, fmt.Errorf("sidecarjob: reading labels of %s: %w", photo.UID, err)
	}
	if in.Favorites, err = s.organize.FavoritesForPhoto(ctx, photo.UID); err != nil {
		return sidecarexport.Input{}, fmt.Errorf("sidecarjob: reading favorites of %s: %w", photo.UID, err)
	}
	if in.Ratings, err = s.organize.RatingsForPhoto(ctx, photo.UID); err != nil {
		return sidecarexport.Input{}, fmt.Errorf("sidecarjob: reading ratings of %s: %w", photo.UID, err)
	}
	if in.People, err = s.people.ListMarkersWithSubjects(ctx, photo.UID); err != nil {
		return sidecarexport.Input{}, fmt.Errorf("sidecarjob: reading markers of %s: %w", photo.UID, err)
	}
	if in.Edit, err = s.editOf(ctx, photo.UID); err != nil {
		return sidecarexport.Input{}, err
	}
	if in.Place, err = s.placeOf(ctx, photo.UID); err != nil {
		return sidecarexport.Input{}, err
	}
	in.UploadedBy = s.uploaderOf(ctx, photo)
	return in, nil
}

// editOf reads the photo's non-destructive edit, returning nil when it has none.
func (s *Service) editOf(ctx context.Context, photoUID string) (*photos.Edit, error) {
	edit, err := s.photos.GetEdit(ctx, photoUID)
	if err != nil {
		if errors.Is(err, photos.ErrEditNotFound) {
			return nil, nil //nolint:nilnil // no edit is the common case, not an error
		}
		return nil, fmt.Errorf("sidecarjob: reading edit of %s: %w", photoUID, err)
	}
	return &edit, nil
}

// placeOf reads the photo's cached place, returning nil when it has never been
// geocoded or when no place store is wired.
func (s *Service) placeOf(ctx context.Context, photoUID string) (*places.Place, error) {
	if s.places == nil {
		return nil, nil //nolint:nilnil // an unwired place cache omits the group
	}
	place, err := s.places.GetPlace(ctx, photoUID)
	if err != nil {
		if errors.Is(err, places.ErrPlaceNotFound) {
			return nil, nil //nolint:nilnil // never geocoded is the common case
		}
		return nil, fmt.Errorf("sidecarjob: reading place of %s: %w", photoUID, err)
	}
	return &place, nil
}

// uploaderOf resolves the uploader's username, best-effort: an unknown or deleted
// account yields an empty string rather than failing the export. Who uploaded a
// photo is a nice-to-have in the file; the curation is not, and one must not cost
// the other.
func (s *Service) uploaderOf(ctx context.Context, photo photos.Photo) string {
	if s.users == nil || photo.UploadedBy == nil {
		return ""
	}
	user, err := s.users.GetUserByUID(ctx, *photo.UploadedBy)
	if err != nil {
		s.log.DebugContext(ctx, "sidecar export: uploader not resolved",
			slog.String("photo_uid", photo.UID), slog.String("error", err.Error()))
		return ""
	}
	return user.Username
}

// BackfillSidecars enqueues a sidecar job for every photo whose sidecar is
// missing or stale and returns how many were scheduled. When all is true it
// schedules every non-archived photo instead — a forced full re-run, which is
// what recovers curation that changed without touching the photo row (an album
// membership, a label) and so does not look stale.
//
// It is idempotent and resumable, and neither takes any bookkeeping of its own:
// the queue's per-photo dedup makes a re-enqueue a no-op, and the pending
// predicate is self-clearing, so a run over a drained library schedules nothing
// and a run interrupted halfway picks up the rest.
func (s *Service) BackfillSidecars(ctx context.Context, all bool) (int, error) {
	if s.lister == nil || s.enqueuer == nil {
		return 0, ErrBackfillUnavailable
	}
	uids, err := s.backfillCandidates(ctx, all)
	if err != nil {
		return 0, err
	}
	enqueued := 0
	for _, uid := range uids {
		if err := s.enqueuer.EnqueueSidecar(ctx, uid); err != nil {
			return enqueued, fmt.Errorf("sidecarjob: enqueuing sidecar for %s: %w", uid, err)
		}
		enqueued++
	}
	return enqueued, nil
}

// backfillCandidates returns the uids a backfill should schedule: every
// non-archived photo when all is set, otherwise those whose sidecar is missing or
// stale.
func (s *Service) backfillCandidates(ctx context.Context, all bool) ([]string, error) {
	if all {
		uids, err := s.lister.ListActiveUIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("sidecarjob: listing active photos: %w", err)
		}
		return uids, nil
	}
	uids, err := s.lister.ListPhotosMissingSidecar(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("sidecarjob: listing photos missing a sidecar: %w", err)
	}
	return uids, nil
}
