package userpic

import (
	"context"
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// Origin names which link of the chain produced a picture. It is reported to the
// account page — which has to say "this is the face of the person you are linked
// to" rather than show a picture with no explanation — and to nobody else: the
// endpoint that serves the picture itself deliberately does not say.
type Origin string

const (
	// OriginUpload is a picture the user uploaded.
	OriginUpload Origin = "upload"
	// OriginPhoto is a photograph of the library the user pointed at.
	OriginPhoto Origin = "photo"
	// OriginSubject is the avatar of the person the account is linked to.
	OriginSubject Origin = "subject"
	// OriginNone is no picture at all — the client draws the coloured initial.
	OriginNone Origin = "none"
)

// Pictures is the storage this service reads and writes. It is an interface so
// the chain can be tested without a database; it is satisfied by *Store.
type Pictures interface {
	// Get returns the stored picture, or ErrNoPicture.
	Get(ctx context.Context, userUID string) (Picture, error)
	// SetUpload stores already-normalized JPEG bytes as the account's picture.
	SetUpload(ctx context.Context, userUID string, image []byte) error
	// SetPhoto points the account's picture at a library photo.
	SetPhoto(ctx context.Context, userUID, photoUID string) error
	// Clear removes the stored picture; clearing nothing is not an error.
	Clear(ctx context.Context, userUID string) error
}

// Users resolves an account to the person of the library it says it is. It is
// satisfied by *auth.Store.
type Users interface {
	// GetUserByUID returns the account, or auth.ErrUserNotFound.
	GetUserByUID(ctx context.Context, uid string) (auth.User, error)
}

// Subjects resolves a subject to the picture that stands for it — the same
// lookup the subject avatar endpoint makes, so an inherited face is exactly the
// face the people index draws. It is satisfied by *people.Store.
type Subjects interface {
	// SubjectAvatar returns the subject's cover photo or best face, or
	// people.ErrSubjectNotFound / people.ErrNoAvatar.
	SubjectAvatar(ctx context.Context, uid string) (people.AvatarSource, error)
}

// Photos reads the record behind a picked photo: whether it still exists, and
// whether it may be shown on a profile. It is satisfied by *photos.Store.
type Photos interface {
	// GetByUID returns one photo or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
}

// Service resolves and changes a user's profile picture. It is the whole of the
// chain described in the package doc; the HTTP layer above it only maps outcomes
// to status codes.
type Service struct {
	pictures Pictures
	users    Users
	subjects Subjects
	photos   Photos
}

// Config bundles the dependencies of NewService.
type Config struct {
	// Pictures is where an uploaded or picked picture is stored.
	Pictures Pictures
	// Users resolves the account's linked subject, the chain's third source.
	Users Users
	// Subjects turns that subject into a photo and a face box.
	Subjects Subjects
	// Photos decides whether a picked photo is still showable.
	Photos Photos
}

// NewService returns a Service from cfg.
func NewService(cfg Config) *Service {
	return &Service{
		pictures: cfg.Pictures,
		users:    cfg.Users,
		subjects: cfg.Subjects,
		photos:   cfg.Photos,
	}
}

// Resolve walks the chain for one account and returns the first answer that
// exists, together with the Origin that produced it. It returns ErrNoPicture
// when no link of the chain answers — an ordinary outcome, and the one the HTTP
// layer turns into the 404 that tells a client to draw the initial.
//
// Every link is re-checked on every call rather than trusted from when it was
// set: a picked photo is looked up and re-judged, so one that has since been
// archived, purged, or flagged private or hidden simply stops being an answer
// and the account falls through to the face below it. That is the difference
// between a chain and a stored decision — the stored decision would serve a
// photo its owner has since hidden.
func (s *Service) Resolve(ctx context.Context, userUID string) (Source, Origin, error) {
	stored, err := s.pictures.Get(ctx, userUID)
	switch {
	case err == nil:
		source, ok, storedErr := s.resolveStored(ctx, stored)
		if storedErr != nil {
			return Source{}, OriginNone, storedErr
		}
		if ok {
			return source, originOf(stored.Kind), nil
		}
	case errors.Is(err, ErrNoPicture):
		// No stored answer; the linked subject below is the common case.
	default:
		return Source{}, OriginNone, fmt.Errorf("userpic: reading stored picture of %s: %w", userUID, err)
	}
	return s.resolveSubject(ctx, userUID)
}

// resolveStored turns a stored row into a servable source, reporting false when
// the row exists but cannot be served — a picked photo that has gone or been
// flagged since. A false is a fall-through, not a failure: the caller moves on
// to the next link of the chain.
func (s *Service) resolveStored(ctx context.Context, stored Picture) (Source, bool, error) {
	if stored.Kind == KindUpload {
		return Source{Upload: stored.Image}, true, nil
	}
	photo, err := s.photos.GetByUID(ctx, stored.PhotoUID)
	if errors.Is(err, photos.ErrPhotoNotFound) {
		return Source{}, false, nil
	}
	if err != nil {
		return Source{}, false, fmt.Errorf("userpic: reading picked photo %s: %w", stored.PhotoUID, err)
	}
	if !Showable(photo) {
		return Source{}, false, nil
	}
	return Source{PhotoUID: photo.UID}, true, nil
}

// resolveSubject returns the avatar of the person the account says it is — the
// chain's default and the reason most accounts need to configure nothing at all.
// An account with no link, a link to a person who has been deleted, or a person
// with no picture anywhere all yield ErrNoPicture.
func (s *Service) resolveSubject(ctx context.Context, userUID string) (Source, Origin, error) {
	user, err := s.users.GetUserByUID(ctx, userUID)
	if errors.Is(err, auth.ErrUserNotFound) {
		return Source{}, OriginNone, ErrNoPicture
	}
	if err != nil {
		return Source{}, OriginNone, fmt.Errorf("userpic: reading account %s: %w", userUID, err)
	}
	if user.SubjectUID == nil || *user.SubjectUID == "" {
		return Source{}, OriginNone, ErrNoPicture
	}
	avatar, err := s.subjects.SubjectAvatar(ctx, *user.SubjectUID)
	if errors.Is(err, people.ErrSubjectNotFound) || errors.Is(err, people.ErrNoAvatar) {
		return Source{}, OriginNone, ErrNoPicture
	}
	if err != nil {
		return Source{}, OriginNone, fmt.Errorf("userpic: reading subject avatar %s: %w", *user.SubjectUID, err)
	}
	return Source{PhotoUID: avatar.PhotoUID, Face: convertBox(avatar.Face)}, OriginSubject, nil
}

// State is what the account page is told about the picture it is editing: which
// source is in force, and — for a picked photo — which photo it is, so the page
// can show the very thumbnail the user chose.
type State struct {
	// Origin names the link of the chain currently answering, OriginNone when
	// the account falls all the way through to the coloured initial.
	Origin Origin `json:"origin"`
	// PhotoUID is the photo the picture is cut from, for OriginPhoto and
	// OriginSubject; empty otherwise.
	PhotoUID string `json:"photo_uid,omitempty"`
	// SubjectUID is the person the account is linked to, whenever it is linked —
	// even when an upload or a pick is currently overriding that face, so the
	// page can say what clearing the picture would fall back to.
	SubjectUID string `json:"subject_uid,omitempty"`
}

// Describe reports which source currently answers for the account, for the
// account page. It is the same walk as Resolve without the bytes: a page that
// only has to label the picture it is about to show should not also make the
// server read an uploaded JPEG out of the database.
func (s *Service) Describe(ctx context.Context, userUID string) (State, error) {
	state := State{Origin: OriginNone}
	user, err := s.users.GetUserByUID(ctx, userUID)
	if err != nil && !errors.Is(err, auth.ErrUserNotFound) {
		return State{}, fmt.Errorf("userpic: reading account %s: %w", userUID, err)
	}
	if err == nil && user.SubjectUID != nil {
		state.SubjectUID = *user.SubjectUID
	}

	source, origin, err := s.Resolve(ctx, userUID)
	if errors.Is(err, ErrNoPicture) {
		return state, nil
	}
	if err != nil {
		return State{}, err
	}
	state.Origin = origin
	state.PhotoUID = source.PhotoUID
	return state, nil
}

// SetUpload normalizes the submitted bytes (see Normalize) and stores the result
// as the account's picture, replacing whatever it had. The submitted original is
// not kept. It returns ErrUnsupportedFormat or ErrTooLarge for bytes that cannot
// become a picture.
func (s *Service) SetUpload(ctx context.Context, userUID string, data []byte) error {
	normalized, err := Normalize(data)
	if err != nil {
		return err
	}
	if err := s.pictures.SetUpload(ctx, userUID, normalized); err != nil {
		return fmt.Errorf("userpic: storing the uploaded picture of %s: %w", userUID, err)
	}
	return nil
}

// SetPhoto points the account's picture at the library photo named by photoUID,
// replacing whatever it had. It returns ErrPhotoNotFound for a photo that does
// not exist (or has been archived, which is the same thing to somebody picking
// one), and ErrPhotoNotAllowed for one flagged private or hidden from the
// library.
//
// The refusal is the point of the method. A profile picture is shown to every
// reader of every thread the account writes in, so letting a private photo
// become one would be the plainest way there is to sidestep the flag.
func (s *Service) SetPhoto(ctx context.Context, userUID, photoUID string) error {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if errors.Is(err, photos.ErrPhotoNotFound) {
		return ErrPhotoNotFound
	}
	if err != nil {
		return fmt.Errorf("userpic: reading picked photo %s: %w", photoUID, err)
	}
	if photo.Private || photo.HiddenFromLibrary {
		return ErrPhotoNotAllowed
	}
	if photo.ArchivedAt != nil {
		return ErrPhotoNotFound
	}
	if err := s.pictures.SetPhoto(ctx, userUID, photo.UID); err != nil {
		return fmt.Errorf("userpic: storing the picked picture of %s: %w", userUID, err)
	}
	return nil
}

// Clear removes the account's stored picture. It does not necessarily return the
// account to the coloured initial: a linked account falls back to that person's
// face, which is the chain's default and not a leftover.
func (s *Service) Clear(ctx context.Context, userUID string) error {
	if err := s.pictures.Clear(ctx, userUID); err != nil {
		return fmt.Errorf("userpic: clearing the picture of %s: %w", userUID, err)
	}
	return nil
}

// Showable reports whether a photo may stand as somebody's profile picture: it
// must not be on its way out of the library, and it must not be one of the two
// kinds of "keep this out of the way" the catalogue records.
//
// It is exported because the same judgement is made twice — once when a photo is
// picked, and again every time the picture is served — and the two must agree,
// or a photo hidden after the fact would go on being shown.
func Showable(photo photos.Photo) bool {
	return photo.ArchivedAt == nil && !photo.Private && !photo.HiddenFromLibrary
}

// originOf maps a stored row's kind to the Origin that names it.
func originOf(kind Kind) Origin {
	if kind == KindUpload {
		return OriginUpload
	}
	return OriginPhoto
}

// convertBox converts the people store's face box into this package's, passing
// nil through — which is how a hand-picked cover photo says "show me whole".
func convertBox(box *people.Box) *Box {
	if box == nil {
		return nil
	}
	return &Box{X: box.X, Y: box.Y, W: box.W, H: box.H}
}
