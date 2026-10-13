package userpic_test

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
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

// projectA and projectB are the two projects the fakes know.
const (
	projectA = "pj-a"
	projectB = "pj-b"
)

// fakeMembers is the memberships the chain consults: per project, per account,
// the person the account says it is there and whether it is approved.
type fakeMembers struct {
	byProject map[string]map[string]*project.Member
	err       error
}

func (f fakeMembers) Membership(_ context.Context, projectUID, userUID string) (*project.Member, error) {
	if f.err != nil {
		return nil, f.err
	}
	m, ok := f.byProject[projectUID][userUID]
	if !ok {
		return nil, project.ErrNotMember
	}
	return m, nil
}

// with returns f plus an approved membership of userUID in projectUID linked to
// subjectUID ("" = linked to nobody).
func (f fakeMembers) with(projectUID, userUID, subjectUID string) fakeMembers {
	approved := time.Now()
	m := &project.Member{ProjectUID: projectUID, UserUID: userUID, ApprovedAt: &approved}
	if subjectUID != "" {
		m.SubjectUID = &subjectUID
	}
	return f.withMember(m)
}

// withMember returns f plus m.
func (f fakeMembers) withMember(m *project.Member) fakeMembers {
	out := fakeMembers{byProject: map[string]map[string]*project.Member{}, err: f.err}
	for p, members := range f.byProject {
		out.byProject[p] = maps.Clone(members)
	}
	if out.byProject[m.ProjectUID] == nil {
		out.byProject[m.ProjectUID] = map[string]*project.Member{}
	}
	out.byProject[m.ProjectUID][m.UserUID] = m
	return out
}

// fakeSubjects hands back a fixed avatar source per subject, or the "nothing to
// show" outcome the people store returns for a person nobody photographed. A
// subject absent from bySubject falls back to source/err.
type fakeSubjects struct {
	source    people.AvatarSource
	err       error
	bySubject map[string]people.AvatarSource
}

func (f fakeSubjects) SubjectAvatar(_ context.Context, _, uid string) (people.AvatarSource, error) {
	if src, ok := f.bySubject[uid]; ok {
		return src, nil
	}
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
func newService(pictures userpic.Pictures, members userpic.Members, subjects userpic.Subjects,
	catalogue map[string]photos.Photo,
) *userpic.Service {
	return userpic.NewService(userpic.Config{
		Pictures: pictures,
		Members:  members,
		Subjects: subjects,
		Photos:   fakePhotos{byUID: catalogue},
	})
}

// linkedTo returns the memberships of an account u1 linked to subjectUID in
// projectA, where the viewer asks from.
func linkedTo(subjectUID string) fakeMembers {
	return fakeMembers{}.with(projectA, "u1", subjectUID).with(projectA, "viewer", "")
}

// inA is a viewer asking from projectA as its approved member.
var inA = userpic.Viewer{UserUID: "viewer", ProjectUID: projectA}

// self is u1 asking about its own picture from projectA.
var self = userpic.Viewer{UserUID: "u1", ProjectUID: projectA}

// visiblePhoto is an ordinary catalogued photo of projectA that may become a
// picture.
func visiblePhoto(uid string) photos.Photo {
	return photos.Photo{UID: uid, FileHash: "abcdef0123456789", FileWidth: 4000, FileHeight: 3000, ProjectID: projectA}
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

	source, origin, err := svc.Resolve(t.Context(), inA, "u1")
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

	source, origin, err := svc.Resolve(t.Context(), inA, "u1")
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

	source, origin, err := svc.Resolve(t.Context(), inA, "u1")
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
		members  fakeMembers
		subjects fakeSubjects
	}{
		"the account names nobody":   {members: linkedTo(""), subjects: fakeSubjects{}},
		"the linked person is gone":  {members: linkedTo("s1"), subjects: fakeSubjects{err: people.ErrSubjectNotFound}},
		"nobody photographed them":   {members: linkedTo("s1"), subjects: fakeSubjects{err: people.ErrNoAvatar}},
		"the account itself is gone": {members: fakeMembers{}, subjects: fakeSubjects{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := newService(newFakePictures(), tc.members, tc.subjects, nil)
			if _, _, err := svc.Resolve(t.Context(), inA, "u1"); !errors.Is(err, userpic.ErrNoPicture) {
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

			source, origin, err := svc.Resolve(t.Context(), inA, "u1")
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
	svc := newService(pictures, linkedTo(""), fakeSubjects{}, nil)

	if _, _, err := svc.Resolve(t.Context(), inA, "u1"); !errors.Is(err, userpic.ErrNoPicture) {
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
		svc := newService(pictures, linkedTo(""), fakeSubjects{}, catalogue)
		if err := svc.SetPhoto(t.Context(), self, uid); !errors.Is(err, userpic.ErrPhotoNotAllowed) {
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
	svc := newService(newFakePictures(), linkedTo(""), fakeSubjects{},
		map[string]photos.Photo{"p1": binned})

	if err := svc.SetPhoto(t.Context(), self, "p1"); !errors.Is(err, userpic.ErrPhotoNotFound) {
		t.Errorf("an archived photo: error = %v, want ErrPhotoNotFound", err)
	}
	if err := svc.SetPhoto(t.Context(), self, "nowhere"); !errors.Is(err, userpic.ErrPhotoNotFound) {
		t.Errorf("an unknown photo: error = %v, want ErrPhotoNotFound", err)
	}
}

func TestSetUpload_storesTheReEncodedSquareAndNotTheSubmittedBytes(t *testing.T) {
	t.Parallel()

	pictures := newFakePictures()
	svc := newService(pictures, linkedTo(""), fakeSubjects{}, nil)
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

// TestSetUpload_reservesFromTheDecodeBudget verifies the upload's decode
// reserves from the shared decode budget: a picture that alone exceeds it is
// ErrTooLarge and stores nothing, and one that fits is stored and gives every
// byte back.
func TestSetUpload_reservesFromTheDecodeBudget(t *testing.T) {
	t.Parallel()

	submitted := encodeJPEG(t, gradient(1200, 800))
	pictures := newFakePictures()
	tiny := userpic.NewService(userpic.Config{Pictures: pictures, DecodeBudget: imgconvert.NewDecodeBudget(1024)})
	if err := tiny.SetUpload(t.Context(), "u1", submitted); !errors.Is(err, userpic.ErrTooLarge) {
		t.Fatalf("SetUpload under a 1 KiB budget = %v, want ErrTooLarge", err)
	}
	if _, ok := pictures.stored["u1"]; ok {
		t.Error("a refused upload was stored")
	}

	budget := imgconvert.NewDecodeBudget(64 << 20)
	svc := userpic.NewService(userpic.Config{Pictures: pictures, DecodeBudget: budget})
	if err := svc.SetUpload(t.Context(), "u1", submitted); err != nil {
		t.Fatalf("SetUpload under a 64 MiB budget = %v", err)
	}
	release, err := budget.Reserve(t.Context(), budget.Capacity())
	if err != nil {
		t.Fatalf("the budget is not whole again after SetUpload: %v", err)
	}
	release()
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

	state, err := svc.Describe(t.Context(), self)
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

	svc := newService(newFakePictures(), linkedTo(""), fakeSubjects{}, nil)
	state, err := svc.Describe(t.Context(), self)
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
	_, origin, err := svc.Resolve(t.Context(), inA, "u1")
	if err != nil {
		t.Fatalf("Resolve after Clear: %v", err)
	}
	if origin != userpic.OriginSubject {
		t.Errorf("origin = %q, want the linked subject's face below the cleared picture", origin)
	}
}

// TestResolve_theInheritedFaceIsTheViewersProjects verifies the chain's third
// step reads the pictured account's membership of the viewer's project: the
// same account wears its project-A person's face to a viewer in A and its
// project-B person's face to a viewer in B, and a request with no project skips
// the step altogether.
func TestResolve_theInheritedFaceIsTheViewersProjects(t *testing.T) {
	t.Parallel()

	members := fakeMembers{}.
		with(projectA, "u1", "sA").with(projectB, "u1", "sB").
		with(projectA, "viewer", "").with(projectB, "viewer", "")
	subjects := fakeSubjects{bySubject: map[string]people.AvatarSource{
		"sA": {PhotoUID: "face-a"},
		"sB": {PhotoUID: "face-b"},
	}}
	svc := newService(newFakePictures(), members, subjects, nil)

	for _, tc := range []struct {
		project string
		want    string
	}{{projectA, "face-a"}, {projectB, "face-b"}} {
		source, origin, err := svc.Resolve(t.Context(), userpic.Viewer{UserUID: "viewer", ProjectUID: tc.project}, "u1")
		if err != nil {
			t.Fatalf("Resolve from %s: %v", tc.project, err)
		}
		if origin != userpic.OriginSubject || source.PhotoUID != tc.want {
			t.Errorf("from %s resolved to %q/%q, want the subject face %q", tc.project, origin, source.PhotoUID, tc.want)
		}
	}

	if _, _, err := svc.Resolve(t.Context(), userpic.Viewer{UserUID: "viewer"}, "u1"); !errors.Is(err, userpic.ErrNoPicture) {
		t.Errorf("without a project: error = %v, want ErrNoPicture (the face step is skipped)", err)
	}

	// Linked only in B: a viewer in A sees no face — a person of another
	// project is nobody here.
	onlyB := newService(newFakePictures(), fakeMembers{}.with(projectB, "u1", "sB").with(projectA, "u1", ""),
		subjects, nil)
	if _, _, err := onlyB.Resolve(t.Context(), inA, "u1"); !errors.Is(err, userpic.ErrNoPicture) {
		t.Errorf("linked only in B, asked from A: error = %v, want ErrNoPicture", err)
	}
}

// TestResolve_aPickedPhotoIsShownOnlyToWhoMaySeeItsProject verifies a picked
// photo of project B answers only for a viewer who may see B — an approved
// member or a maintainer — and that everybody else falls through to the next
// step, exactly as a private photo does.
func TestResolve_aPickedPhotoIsShownOnlyToWhoMaySeeItsProject(t *testing.T) {
	t.Parallel()

	picked := visiblePhoto("picked")
	picked.ProjectID = projectB
	pending := &project.Member{ProjectUID: projectB, UserUID: "waiting"}
	members := fakeMembers{}.
		with(projectA, "u1", "s1").with(projectB, "u1", "").
		with(projectA, "onlyA", "").
		with(projectA, "both", "").with(projectB, "both", "").
		with(projectA, "waiting", "").withMember(pending)
	pictures := newFakePictures()
	if err := pictures.SetPhoto(t.Context(), "u1", "picked"); err != nil {
		t.Fatalf("seeding the pick: %v", err)
	}
	svc := newService(pictures, members,
		fakeSubjects{source: people.AvatarSource{PhotoUID: "subject-photo"}},
		map[string]photos.Photo{"picked": picked})

	for _, tc := range []struct {
		name   string
		viewer userpic.Viewer
		want   userpic.Origin
	}{
		{"a member of A only", userpic.Viewer{UserUID: "onlyA", ProjectUID: projectA}, userpic.OriginSubject},
		{"a member of both", userpic.Viewer{UserUID: "both", ProjectUID: projectA}, userpic.OriginPhoto},
		{"a pending member of B", userpic.Viewer{UserUID: "waiting", ProjectUID: projectA}, userpic.OriginSubject},
		{"a maintainer", userpic.Viewer{UserUID: "root", Maintainer: true, ProjectUID: projectA}, userpic.OriginPhoto},
		{"nobody", userpic.Viewer{ProjectUID: projectA}, userpic.OriginSubject},
	} {
		_, origin, err := svc.Resolve(t.Context(), tc.viewer, "u1")
		if err != nil {
			t.Fatalf("%s: Resolve: %v", tc.name, err)
		}
		if origin != tc.want {
			t.Errorf("%s: origin = %q, want %q", tc.name, origin, tc.want)
		}
	}
}

// TestSetPhoto_refusesAPhotoOfAProjectTheCallerIsNotIn verifies picking a
// photo checks the caller may see the photo's project: a member of A only gets
// ErrPhotoNotFound for a photo of B and nothing is stored, while a member of B
// and a maintainer may pick it.
func TestSetPhoto_refusesAPhotoOfAProjectTheCallerIsNotIn(t *testing.T) {
	t.Parallel()

	inB := visiblePhoto("pb")
	inB.ProjectID = projectB
	catalogue := map[string]photos.Photo{"pb": inB}
	members := fakeMembers{}.with(projectA, "u1", "").with(projectB, "u2", "")

	pictures := newFakePictures()
	svc := newService(pictures, members, fakeSubjects{}, catalogue)
	if err := svc.SetPhoto(t.Context(), self, "pb"); !errors.Is(err, userpic.ErrPhotoNotFound) {
		t.Errorf("a member of A only: error = %v, want ErrPhotoNotFound", err)
	}
	if _, stored := pictures.stored["u1"]; stored {
		t.Error("a refused pick was stored")
	}
	for _, viewer := range []userpic.Viewer{
		{UserUID: "u2", ProjectUID: projectB},
		{UserUID: "root", Maintainer: true},
	} {
		if err := svc.SetPhoto(t.Context(), viewer, "pb"); err != nil {
			t.Errorf("SetPhoto as %+v: %v, want it stored", viewer, err)
		}
		if got := pictures.stored[viewer.UserUID].PhotoUID; got != "pb" {
			t.Errorf("stored pick of %s = %q, want pb", viewer.UserUID, got)
		}
	}
}

// TestResolve_aMembershipReadFailureIsAnError verifies a broken membership
// lookup is reported rather than mistaken for "no picture".
func TestResolve_aMembershipReadFailureIsAnError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	svc := newService(newFakePictures(), fakeMembers{err: boom}, fakeSubjects{}, nil)
	if _, _, err := svc.Resolve(t.Context(), inA, "u1"); !errors.Is(err, boom) {
		t.Errorf("error = %v, want the membership failure", err)
	}
	if _, err := svc.Describe(t.Context(), self); !errors.Is(err, boom) {
		t.Errorf("Describe error = %v, want the membership failure", err)
	}
}
