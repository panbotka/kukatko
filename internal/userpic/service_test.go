package userpic_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/userpic"
)

// fakePictures is an in-memory stand-in for the user_pictures table, so the
// chain can be exercised without a database.
type fakePictures struct {
	stored map[string]userpic.Picture
}

func newFakePictures() *fakePictures {
	return &fakePictures{stored: map[string]userpic.Picture{}}
}

func (f *fakePictures) Get(_ context.Context, userUID string) (userpic.Picture, error) {
	pic, ok := f.stored[userUID]
	if !ok {
		return userpic.Picture{}, userpic.ErrNoPicture
	}
	return pic, nil
}

func (f *fakePictures) SetUpload(_ context.Context, userUID string, image []byte) error {
	f.stored[userUID] = userpic.Picture{Kind: userpic.KindUpload, Image: image, UpdatedAt: time.Now()}
	return nil
}

func (f *fakePictures) SetPhoto(_ context.Context, userUID, photoUID string) error {
	f.stored[userUID] = userpic.Picture{Kind: userpic.KindPhoto, PhotoUID: photoUID, UpdatedAt: time.Now()}
	return nil
}

func (f *fakePictures) Clear(_ context.Context, userUID string) error {
	delete(f.stored, userUID)
	return nil
}

// fakeUsers answers only the question the chain asks of an account: which person
// of the library it says it is.
type fakeUsers struct {
	subjectUID *string
	missing    bool
}

func (f fakeUsers) GetUserByUID(_ context.Context, uid string) (auth.User, error) {
	if f.missing {
		return auth.User{}, auth.ErrUserNotFound
	}
	return auth.User{UID: uid, Username: "someone", SubjectUID: f.subjectUID}, nil
}

// fakeSubjects hands back a fixed avatar source, or the "nothing to show"
// outcome the people store returns for a person nobody photographed.
type fakeSubjects struct {
	source people.AvatarSource
	err    error
}

func (f fakeSubjects) SubjectAvatar(_ context.Context, _ string) (people.AvatarSource, error) {
	if f.err != nil {
		return people.AvatarSource{}, f.err
	}
	return f.source, nil
}

// fakePhotos is a tiny catalogue keyed by uid.
type fakePhotos struct {
	byUID map[string]photos.Photo
}

func (f fakePhotos) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	photo, ok := f.byUID[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return photo, nil
}

// newService assembles a Service over the fakes, with a linked subject whose
// face lives on "subject-photo" unless a case says otherwise.
func newService(pictures userpic.Pictures, users userpic.Users, subjects userpic.Subjects,
	catalogue map[string]photos.Photo,
) *userpic.Service {
	return userpic.NewService(userpic.Config{
		Pictures: pictures,
		Users:    users,
		Subjects: subjects,
		Photos:   fakePhotos{byUID: catalogue},
	})
}

// linkedTo returns the fakeUsers for an account linked to subjectUID.
func linkedTo(subjectUID string) fakeUsers {
	return fakeUsers{subjectUID: &subjectUID}
}

// visiblePhoto is an ordinary catalogued photo that may become a picture.
func visiblePhoto(uid string) photos.Photo {
	return photos.Photo{UID: uid, FileHash: "abcdef0123456789", FileWidth: 4000, FileHeight: 3000}
}

func TestResolve_anUploadWinsOverEverythingBelowIt(t *testing.T) {
	t.Parallel()

	pictures := newFakePictures()
	if err := pictures.SetUpload(t.Context(), "u1", []byte("jpeg-bytes")); err != nil {
		t.Fatalf("seeding the upload: %v", err)
	}
	svc := newService(pictures, linkedTo("s1"),
		fakeSubjects{source: people.AvatarSource{PhotoUID: "subject-photo"}},
		map[string]photos.Photo{"subject-photo": visiblePhoto("subject-photo")})

	source, origin, err := svc.Resolve(t.Context(), "u1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if origin != userpic.OriginUpload {
		t.Errorf("origin = %q, want %q", origin, userpic.OriginUpload)
	}
	if string(source.Upload) != "jpeg-bytes" {
		t.Errorf("upload = %q, want the stored bytes", source.Upload)
	}
	if source.PhotoUID != "" {
		t.Errorf("an upload also named photo %q", source.PhotoUID)
	}
}

func TestResolve_aPickedPhotoWinsOverTheLinkedSubject(t *testing.T) {
	t.Parallel()

	pictures := newFakePictures()
	if err := pictures.SetPhoto(t.Context(), "u1", "picked"); err != nil {
		t.Fatalf("seeding the pick: %v", err)
	}
	svc := newService(pictures, linkedTo("s1"),
		fakeSubjects{source: people.AvatarSource{PhotoUID: "subject-photo"}},
		map[string]photos.Photo{
			"picked":        visiblePhoto("picked"),
			"subject-photo": visiblePhoto("subject-photo"),
		})

	source, origin, err := svc.Resolve(t.Context(), "u1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if origin != userpic.OriginPhoto {
		t.Errorf("origin = %q, want %q", origin, userpic.OriginPhoto)
	}
	if source.PhotoUID != "picked" {
		t.Errorf("photo = %q, want the picked one", source.PhotoUID)
	}
	if source.Face != nil {
		t.Errorf("a picked photo is shown whole, got face box %+v", source.Face)
	}
}

func TestResolve_theLinkedSubjectIsTheDefault(t *testing.T) {
	t.Parallel()

	box := people.Box{X: 0.6, Y: 0.2, W: 0.1, H: 0.15}
	svc := newService(newFakePictures(), linkedTo("s1"),
		fakeSubjects{source: people.AvatarSource{PhotoUID: "subject-photo", Face: &box}},
		map[string]photos.Photo{"subject-photo": visiblePhoto("subject-photo")})

	source, origin, err := svc.Resolve(t.Context(), "u1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if origin != userpic.OriginSubject {
		t.Errorf("origin = %q, want %q", origin, userpic.OriginSubject)
	}
	if source.Face == nil || source.Face.X != box.X {
		t.Errorf("face = %+v, want the subject's box %+v", source.Face, box)
	}
}

func TestResolve_noSourceAtAllIsErrNoPicture(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		users    fakeUsers
		subjects fakeSubjects
	}{
		"the account names nobody":   {users: fakeUsers{}, subjects: fakeSubjects{}},
		"the linked person is gone":  {users: linkedTo("s1"), subjects: fakeSubjects{err: people.ErrSubjectNotFound}},
		"nobody photographed them":   {users: linkedTo("s1"), subjects: fakeSubjects{err: people.ErrNoAvatar}},
		"the account itself is gone": {users: fakeUsers{missing: true}, subjects: fakeSubjects{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := newService(newFakePictures(), tc.users, tc.subjects, nil)
			if _, _, err := svc.Resolve(t.Context(), "u1"); !errors.Is(err, userpic.ErrNoPicture) {
				t.Errorf("error = %v, want ErrNoPicture", err)
			}
		})
	}
}

func TestResolve_aPickedPhotoThatStoppedBeingShowableFallsThrough(t *testing.T) {
	t.Parallel()

	archived := time.Now()
	gone := visiblePhoto("picked")
	gone.ArchivedAt = &archived
	private := visiblePhoto("picked")
	private.Private = true
	hidden := visiblePhoto("picked")
	hidden.HiddenFromLibrary = true

	cases := map[string]map[string]photos.Photo{
		"purged":   {"subject-photo": visiblePhoto("subject-photo")},
		"archived": {"picked": gone, "subject-photo": visiblePhoto("subject-photo")},
		"private":  {"picked": private, "subject-photo": visiblePhoto("subject-photo")},
		"hidden":   {"picked": hidden, "subject-photo": visiblePhoto("subject-photo")},
	}
	for name, catalogue := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pictures := newFakePictures()
			if err := pictures.SetPhoto(t.Context(), "u1", "picked"); err != nil {
				t.Fatalf("seeding the pick: %v", err)
			}
			svc := newService(pictures, linkedTo("s1"),
				fakeSubjects{source: people.AvatarSource{PhotoUID: "subject-photo"}}, catalogue)

			source, origin, err := svc.Resolve(t.Context(), "u1")
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if origin != userpic.OriginSubject || source.PhotoUID != "subject-photo" {
				t.Errorf("resolved to %q/%q, want the linked subject's face", origin, source.PhotoUID)
			}
		})
	}
}

func TestResolve_anUnusablePickWithNothingBelowItIs404Material(t *testing.T) {
	t.Parallel()

	pictures := newFakePictures()
	if err := pictures.SetPhoto(t.Context(), "u1", "picked"); err != nil {
		t.Fatalf("seeding the pick: %v", err)
	}
	svc := newService(pictures, fakeUsers{}, fakeSubjects{}, nil)

	if _, _, err := svc.Resolve(t.Context(), "u1"); !errors.Is(err, userpic.ErrNoPicture) {
		t.Errorf("error = %v, want ErrNoPicture", err)
	}
}

func TestSetPhoto_refusesAPrivateOrHiddenPhoto(t *testing.T) {
	t.Parallel()

	private := visiblePhoto("p1")
	private.Private = true
	hidden := visiblePhoto("p2")
	hidden.HiddenFromLibrary = true
	catalogue := map[string]photos.Photo{"p1": private, "p2": hidden}

	for _, uid := range []string{"p1", "p2"} {
		pictures := newFakePictures()
		svc := newService(pictures, fakeUsers{}, fakeSubjects{}, catalogue)
		if err := svc.SetPhoto(t.Context(), "u1", uid); !errors.Is(err, userpic.ErrPhotoNotAllowed) {
			t.Errorf("SetPhoto(%s) error = %v, want ErrPhotoNotAllowed", uid, err)
		}
		if _, stored := pictures.stored["u1"]; stored {
			t.Errorf("SetPhoto(%s) stored a picture despite refusing it", uid)
		}
	}
}

func TestSetPhoto_refusesAPhotoThatIsNotThere(t *testing.T) {
	t.Parallel()

	archived := time.Now()
	binned := visiblePhoto("p1")
	binned.ArchivedAt = &archived
	svc := newService(newFakePictures(), fakeUsers{}, fakeSubjects{},
		map[string]photos.Photo{"p1": binned})

	if err := svc.SetPhoto(t.Context(), "u1", "p1"); !errors.Is(err, userpic.ErrPhotoNotFound) {
		t.Errorf("an archived photo: error = %v, want ErrPhotoNotFound", err)
	}
	if err := svc.SetPhoto(t.Context(), "u1", "nowhere"); !errors.Is(err, userpic.ErrPhotoNotFound) {
		t.Errorf("an unknown photo: error = %v, want ErrPhotoNotFound", err)
	}
}

func TestSetUpload_storesTheReEncodedSquareAndNotTheSubmittedBytes(t *testing.T) {
	t.Parallel()

	pictures := newFakePictures()
	svc := newService(pictures, fakeUsers{}, fakeSubjects{}, nil)
	submitted := encodeJPEG(t, gradient(1200, 800))

	if err := svc.SetUpload(t.Context(), "u1", submitted); err != nil {
		t.Fatalf("SetUpload: %v", err)
	}
	stored := pictures.stored["u1"]
	if stored.Kind != userpic.KindUpload {
		t.Errorf("kind = %q, want %q", stored.Kind, userpic.KindUpload)
	}
	if bytesEqual(stored.Image, submitted) {
		t.Error("the submitted original was stored verbatim; it must be re-encoded")
	}
	if bounds := decode(t, stored.Image).Bounds(); bounds.Dx() != bounds.Dy() {
		t.Errorf("stored picture is %v, want a square", bounds)
	}
}

// bytesEqual reports whether two byte slices hold the same bytes.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDescribe_namesTheSourceAndTheFallbackBelowIt(t *testing.T) {
	t.Parallel()

	pictures := newFakePictures()
	if err := pictures.SetUpload(t.Context(), "u1", []byte("bytes")); err != nil {
		t.Fatalf("seeding the upload: %v", err)
	}
	svc := newService(pictures, linkedTo("s1"),
		fakeSubjects{source: people.AvatarSource{PhotoUID: "subject-photo"}},
		map[string]photos.Photo{"subject-photo": visiblePhoto("subject-photo")})

	state, err := svc.Describe(t.Context(), "u1")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if state.Origin != userpic.OriginUpload {
		t.Errorf("origin = %q, want %q", state.Origin, userpic.OriginUpload)
	}
	// The link is reported even while an upload overrides it, so the account page
	// can say what clearing the picture would fall back to.
	if state.SubjectUID != "s1" {
		t.Errorf("subject = %q, want s1", state.SubjectUID)
	}
	if state.PhotoUID != "" {
		t.Errorf("an upload named photo %q", state.PhotoUID)
	}
}

func TestDescribe_anAccountWithNothingIsOriginNone(t *testing.T) {
	t.Parallel()

	svc := newService(newFakePictures(), fakeUsers{}, fakeSubjects{}, nil)
	state, err := svc.Describe(t.Context(), "u1")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if state.Origin != userpic.OriginNone {
		t.Errorf("origin = %q, want %q", state.Origin, userpic.OriginNone)
	}
}

func TestClear_isIdempotentAndLeavesTheChainBelowIt(t *testing.T) {
	t.Parallel()

	pictures := newFakePictures()
	if err := pictures.SetUpload(t.Context(), "u1", []byte("bytes")); err != nil {
		t.Fatalf("seeding the upload: %v", err)
	}
	svc := newService(pictures, linkedTo("s1"),
		fakeSubjects{source: people.AvatarSource{PhotoUID: "subject-photo"}},
		map[string]photos.Photo{"subject-photo": visiblePhoto("subject-photo")})

	for range 2 {
		if err := svc.Clear(t.Context(), "u1"); err != nil {
			t.Fatalf("Clear: %v", err)
		}
	}
	_, origin, err := svc.Resolve(t.Context(), "u1")
	if err != nil {
		t.Fatalf("Resolve after Clear: %v", err)
	}
	if origin != userpic.OriginSubject {
		t.Errorf("origin = %q, want the linked subject's face below the cleared picture", origin)
	}
}
