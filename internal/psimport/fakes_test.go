package psimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/photosorter"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/vectors"
)

// fakeSource is an in-memory photo-sorter database for the orchestration tests.
type fakeSource struct {
	photos    []photosorter.Photo
	subjects  []photosorter.Subject
	albums    []photosorter.Album
	labels    []photosorter.Label
	embed     map[string]photosorter.Embedding
	faces     map[string][]photosorter.Face
	processed map[string]int
	phash     map[string]photosorter.Phash
	edit      map[string]photosorter.Edit
	markers   map[string][]photosorter.Marker
	albumMem  map[string][]photosorter.AlbumPhoto
	labelMem  map[string][]photosorter.PhotoLabel
}

// newFakeSource returns an empty fakeSource with initialised maps.
func newFakeSource() *fakeSource {
	return &fakeSource{
		embed: map[string]photosorter.Embedding{}, faces: map[string][]photosorter.Face{},
		processed: map[string]int{}, phash: map[string]photosorter.Phash{},
		edit: map[string]photosorter.Edit{}, markers: map[string][]photosorter.Marker{},
		albumMem: map[string][]photosorter.AlbumPhoto{}, labelMem: map[string][]photosorter.PhotoLabel{},
	}
}

func (s *fakeSource) ListPhotos(_ context.Context, p photosorter.PhotoListParams) ([]photosorter.Photo, error) {
	if p.Offset > 0 {
		return nil, nil
	}
	var out []photosorter.Photo
	for _, ph := range s.photos {
		if ph.UpdatedAt.After(p.UpdatedSince) {
			out = append(out, ph)
		}
	}
	return out, nil
}

func (s *fakeSource) ListSubjects(_ context.Context, p photosorter.ListParams) ([]photosorter.Subject, error) {
	if p.Offset > 0 {
		return nil, nil
	}
	return s.subjects, nil
}

func (s *fakeSource) ListAlbums(_ context.Context, p photosorter.ListParams) ([]photosorter.Album, error) {
	if p.Offset > 0 {
		return nil, nil
	}
	return s.albums, nil
}

func (s *fakeSource) ListLabels(_ context.Context, p photosorter.ListParams) ([]photosorter.Label, error) {
	if p.Offset > 0 {
		return nil, nil
	}
	return s.labels, nil
}

func (s *fakeSource) Embedding(_ context.Context, uid string) (photosorter.Embedding, bool, error) {
	e, ok := s.embed[uid]
	return e, ok, nil
}

func (s *fakeSource) Faces(_ context.Context, uid string) ([]photosorter.Face, error) {
	return s.faces[uid], nil
}

func (s *fakeSource) FacesProcessed(_ context.Context, uid string) (int, bool, error) {
	n, ok := s.processed[uid]
	return n, ok, nil
}

func (s *fakeSource) Phash(_ context.Context, uid string) (photosorter.Phash, bool, error) {
	p, ok := s.phash[uid]
	return p, ok, nil
}

func (s *fakeSource) Edit(_ context.Context, uid string) (photosorter.Edit, bool, error) {
	e, ok := s.edit[uid]
	return e, ok, nil
}

func (s *fakeSource) Markers(_ context.Context, uid string) ([]photosorter.Marker, error) {
	return s.markers[uid], nil
}

func (s *fakeSource) AlbumMemberships(_ context.Context, uid string) ([]photosorter.AlbumPhoto, error) {
	return s.albumMem[uid], nil
}

func (s *fakeSource) LabelMemberships(_ context.Context, uid string) ([]photosorter.PhotoLabel, error) {
	return s.labelMem[uid], nil
}

// fakePhotos is an in-memory photo catalogue keyed by uid, file_hash and
// photosorter_uid.
type fakePhotos struct {
	byUID   map[string]photos.Photo
	byHash  map[string]string // hash -> uid
	byPS    map[string]string // ps uid -> uid
	nextID  int
	creates int
}

func newFakePhotos() *fakePhotos {
	return &fakePhotos{byUID: map[string]photos.Photo{}, byHash: map[string]string{}, byPS: map[string]string{}}
}

func (f *fakePhotos) GetByPhotosorterUID(_ context.Context, psUID string) (photos.Photo, error) {
	if uid, ok := f.byPS[psUID]; ok {
		return f.byUID[uid], nil
	}
	return photos.Photo{}, photos.ErrPhotoNotFound
}

func (f *fakePhotos) GetByFileHash(_ context.Context, hash string) (photos.Photo, error) {
	if uid, ok := f.byHash[hash]; ok {
		return f.byUID[uid], nil
	}
	return photos.Photo{}, photos.ErrPhotoNotFound
}

func (f *fakePhotos) SetPhotosorterRef(_ context.Context, uid, psUID string) (photos.Photo, error) {
	p, ok := f.byUID[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	p.PhotosorterUID = &psUID
	f.byUID[uid] = p
	f.byPS[psUID] = uid
	return p, nil
}

func (f *fakePhotos) Create(_ context.Context, p photos.Photo) (photos.Photo, error) {
	if _, ok := f.byHash[p.FileHash]; ok {
		return photos.Photo{}, photos.ErrFileHashTaken
	}
	f.nextID++
	f.creates++
	p.UID = fmt.Sprintf("ph_%d", f.nextID)
	f.byUID[p.UID] = p
	f.byHash[p.FileHash] = p.UID
	if p.PhotosorterUID != nil {
		f.byPS[*p.PhotosorterUID] = p.UID
	}
	return p, nil
}

func (f *fakePhotos) CreateFile(_ context.Context, file photos.PhotoFile) (photos.PhotoFile, error) {
	file.ID = 1
	return file, nil
}

func (f *fakePhotos) SetPhash(context.Context, photos.Phash) error { return nil }
func (f *fakePhotos) SetEdit(context.Context, photos.Edit) error   { return nil }

func (f *fakePhotos) Delete(_ context.Context, uid string) error {
	delete(f.byUID, uid)
	return nil
}

// fakeVectors records the embeddings and faces saved.
type fakeVectors struct {
	embeddings map[string]vectors.Embedding
	faces      map[string][]vectors.Face
}

func newFakeVectors() *fakeVectors {
	return &fakeVectors{embeddings: map[string]vectors.Embedding{}, faces: map[string][]vectors.Face{}}
}

func (f *fakeVectors) SaveEmbedding(_ context.Context, e vectors.Embedding) (vectors.Embedding, error) {
	f.embeddings[e.PhotoUID] = e
	return e, nil
}

func (f *fakeVectors) RecordFaceDetection(_ context.Context, uid string, faces []vectors.Face, _ string) error {
	f.faces[uid] = faces
	return nil
}

// fakePeople is an in-memory subject/marker store keyed by slug and uid.
type fakePeople struct {
	bySlug  map[string]people.Subject
	markers map[string]people.Marker
	nextSub int
}

func newFakePeople() *fakePeople {
	return &fakePeople{bySlug: map[string]people.Subject{}, markers: map[string]people.Marker{}}
}

func (f *fakePeople) GetSubjectBySlug(_ context.Context, slug string) (people.Subject, error) {
	if s, ok := f.bySlug[slug]; ok {
		return s, nil
	}
	return people.Subject{}, people.ErrSubjectNotFound
}

func (f *fakePeople) CreateSubject(_ context.Context, subj people.Subject) (people.Subject, error) {
	f.nextSub++
	subj.UID = fmt.Sprintf("su_%d", f.nextSub)
	subj.Slug = people.Slugify(subj.Name)
	f.bySlug[subj.Slug] = subj
	return subj, nil
}

func (f *fakePeople) GetMarkerByUID(_ context.Context, uid string) (people.Marker, error) {
	if m, ok := f.markers[uid]; ok {
		return m, nil
	}
	return people.Marker{}, people.ErrMarkerNotFound
}

func (f *fakePeople) CreateMarker(_ context.Context, m people.Marker) (people.Marker, error) {
	if m.UID == "" {
		m.UID = fmt.Sprintf("mk_%d", len(f.markers)+1)
	}
	f.markers[m.UID] = m
	return m, nil
}

// fakeAlbums is an in-memory album store recording memberships.
type fakeAlbums struct {
	bySlug  map[string]organize.Album
	members map[string][]string // album uid -> photo uids
	nextID  int
}

func newFakeAlbums() *fakeAlbums {
	return &fakeAlbums{bySlug: map[string]organize.Album{}, members: map[string][]string{}}
}

func (f *fakeAlbums) ListAlbums(context.Context) ([]organize.AlbumSummary, error) {
	out := make([]organize.AlbumSummary, 0, len(f.bySlug))
	for _, a := range f.bySlug {
		out = append(out, organize.AlbumSummary{AlbumCount: organize.AlbumCount{Album: a}})
	}
	return out, nil
}

func (f *fakeAlbums) CreateAlbum(_ context.Context, a organize.Album) (organize.Album, error) {
	f.nextID++
	a.UID = fmt.Sprintf("al_%d", f.nextID)
	a.Slug = strings.ToLower(a.Title)
	f.bySlug[a.Slug] = a
	return a, nil
}

func (f *fakeAlbums) AddPhoto(_ context.Context, albumUID, photoUID string) error {
	f.members[albumUID] = append(f.members[albumUID], photoUID)
	return nil
}

// fakeLabels is an in-memory label store recording attachments.
type fakeLabels struct {
	byName   map[string]organize.Label
	attached map[string][]string // label uid -> photo uids
	nextID   int
}

func newFakeLabels() *fakeLabels {
	return &fakeLabels{byName: map[string]organize.Label{}, attached: map[string][]string{}}
}

func (f *fakeLabels) ListLabels(context.Context) ([]organize.LabelCount, error) {
	out := make([]organize.LabelCount, 0, len(f.byName))
	for _, l := range f.byName {
		out = append(out, organize.LabelCount{Label: l})
	}
	return out, nil
}

func (f *fakeLabels) CreateLabel(_ context.Context, l organize.Label) (organize.Label, error) {
	f.nextID++
	l.UID = fmt.Sprintf("lb_%d", f.nextID)
	f.byName[l.Name] = l
	return l, nil
}

func (f *fakeLabels) AttachLabel(_ context.Context, photoUID, labelUID string, _ organize.LabelSource, _ int) error {
	f.attached[labelUID] = append(f.attached[labelUID], photoUID)
	return nil
}

// fakeRuns is an in-memory import-run store.
type fakeRuns struct {
	nextID    int64
	completed map[int64]*time.Time
	failed    map[int64]string
	counts    map[int64]importer.Counts
}

func newFakeRuns() *fakeRuns {
	return &fakeRuns{completed: map[int64]*time.Time{}, failed: map[int64]string{}, counts: map[int64]importer.Counts{}}
}

func (f *fakeRuns) Start(_ context.Context, _ importer.Source) (importer.Run, error) {
	f.nextID++
	return importer.Run{ID: f.nextID}, nil
}

func (f *fakeRuns) UpdateCounts(_ context.Context, id int64, c importer.Counts) error {
	f.counts[id] = c
	return nil
}

func (f *fakeRuns) Complete(_ context.Context, id int64, w *time.Time, c importer.Counts) error {
	f.completed[id] = w
	f.counts[id] = c
	return nil
}

func (f *fakeRuns) Fail(_ context.Context, id int64, msg string, c importer.Counts) error {
	f.failed[id] = msg
	f.counts[id] = c
	return nil
}

func (f *fakeRuns) LatestWatermark(context.Context, importer.Source) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

// fakeStorage stores into an in-memory map; the content hash is derived from the
// original name so identical names dedup deterministically.
type fakeStorage struct {
	stored map[string]bool
}

func newFakeStorage() *fakeStorage { return &fakeStorage{stored: map[string]bool{}} }

func (f *fakeStorage) Store(
	_ context.Context, src io.Reader, _ time.Time, name string,
) (storage.StoredFile, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return storage.StoredFile{}, err
	}
	return storage.StoredFile{
		Hash:    "hash-" + name,
		RelPath: "2024/01/" + name,
		Size:    int64(len(data)),
		MIME:    "image/jpeg",
	}, nil
}

func (f *fakeStorage) Delete(context.Context, string) error { return nil }

// fakeThumbs is a no-op thumbnailer.
type fakeThumbs struct{}

func (fakeThumbs) GenerateAll(context.Context, photos.Photo) (map[string]string, error) {
	return map[string]string{}, nil
}

// fakeEnqueuer records the photo uids enqueued for embedding and face detection.
type fakeEnqueuer struct {
	embeds []string
	faces  []string
}

func (f *fakeEnqueuer) EnqueueImageEmbed(_ context.Context, uid string) error {
	f.embeds = append(f.embeds, uid)
	return nil
}

func (f *fakeEnqueuer) EnqueueFaceDetect(_ context.Context, uid string) error {
	f.faces = append(f.faces, uid)
	return nil
}

// errOpen is returned by a failing OpenOriginal to exercise the per-photo failure
// path.
var errOpen = errors.New("open failed")

// harness bundles the fakes and the service under test for the orchestration
// tests.
type harness struct {
	src    *fakeSource
	photos *fakePhotos
	vec    *fakeVectors
	people *fakePeople
	albums *fakeAlbums
	labels *fakeLabels
	runs   *fakeRuns
	enq    *fakeEnqueuer
	svc    *Service
}

// newHarness wires the fakes into a Service. files maps an on-disk path to its
// bytes so the create path can copy originals without touching the filesystem; a
// missing path yields errOpen.
func newHarness(src *fakeSource, files map[string][]byte) *harness {
	h := &harness{
		src: src, photos: newFakePhotos(), vec: newFakeVectors(), people: newFakePeople(),
		albums: newFakeAlbums(), labels: newFakeLabels(), runs: newFakeRuns(), enq: &fakeEnqueuer{},
	}
	h.svc = New(Config{
		Source: src, Runs: h.runs, Photos: h.photos, Vectors: h.vec, People: h.people,
		Albums: h.albums, Labels: h.labels, Storage: newFakeStorage(), Thumbnailer: fakeThumbs{},
		Enqueuer: h.enq,
		OpenOriginal: func(path string) (io.ReadCloser, error) {
			data, ok := files[path]
			if !ok {
				return nil, errOpen
			}
			return io.NopCloser(strings.NewReader(string(data))), nil
		},
	})
	return h
}
