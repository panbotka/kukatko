package ppimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/video"
)

// hashBytes returns the hex SHA256 of b, mirroring the storage layer so a fake
// store's hashes match the importer's staged hashes.
func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// errAlbumTypeRequired mirrors the real album endpoint rejecting a listing that
// names no album type (it answers 400 "Permission denied").
var errAlbumTypeRequired = errors.New("photoprism: album listing requires a type")

// fakeClient is an in-memory PhotoPrismClient. It pages the incremental photo
// listing (filtered by UpdatedSince), serves scoped listings keyed by the album
// uid and by the verbatim q= expression, and streams stored originals keyed by
// their SHA1 file hash.
type fakeClient struct {
	photos      []photoprism.Photo
	albums      []photoprism.Album
	labels      []photoprism.Label
	albumPhotos map[string][]photoprism.Photo
	// queryPhotos answers a scoped listing by its exact q= expression (e.g.
	// `label:"sdh"`, `year:1985`), so a test that keys it also pins the expression
	// the importer sends to the source.
	queryPhotos map[string][]photoprism.Photo
	files       map[string][]byte
	downloadErr map[string]error
	listErr     error

	mu        sync.Mutex
	downloads []string
}

// ListPhotos returns one page of the photos the params select.
func (c *fakeClient) ListPhotos(_ context.Context, p photoprism.PhotoListParams) ([]photoprism.Photo, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return page(c.selectPhotos(p), p.Offset, p.Count), nil
}

// selectPhotos resolves which photos a listing selects: the album's and the
// query's photos intersected when both filters are set (the source applies s= and
// q= together), either one alone, or the incrementally filtered catalogue when
// neither is.
func (c *fakeClient) selectPhotos(p photoprism.PhotoListParams) []photoprism.Photo {
	switch {
	case p.AlbumUID != "" && p.Query != "":
		return intersectPhotos(c.albumPhotos[p.AlbumUID], c.queryPhotos[p.Query])
	case p.AlbumUID != "":
		return c.albumPhotos[p.AlbumUID]
	case p.Query != "":
		return c.queryPhotos[p.Query]
	default:
		return filterUpdated(c.photos, p.UpdatedSince)
	}
}

// intersectPhotos returns the photos present in both lists, in the order of a.
func intersectPhotos(a, b []photoprism.Photo) []photoprism.Photo {
	inB := make(map[string]struct{}, len(b))
	for i := range b {
		inB[b[i].UID] = struct{}{}
	}
	out := make([]photoprism.Photo, 0, len(a))
	for i := range a {
		if _, ok := inB[a[i].UID]; ok {
			out = append(out, a[i])
		}
	}
	return out
}

// ListAlbums returns one page of the albums of the requested type. It mirrors the
// real endpoint, which serves exactly one album type per request and rejects a
// listing that names none — the fake refuses that outright so a client that
// forgets the type cannot pass the tests while failing against a real PhotoPrism.
func (c *fakeClient) ListAlbums(_ context.Context, p photoprism.ListParams) ([]photoprism.Album, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	if p.Type == "" {
		return nil, errAlbumTypeRequired
	}
	ofType := make([]photoprism.Album, 0, len(c.albums))
	for _, a := range c.albums {
		if a.Type == p.Type {
			ofType = append(ofType, a)
		}
	}
	return pageAlbums(ofType, p.Offset, p.Count), nil
}

// ListLabels returns one page of labels.
func (c *fakeClient) ListLabels(_ context.Context, p photoprism.ListParams) ([]photoprism.Label, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return pageLabels(c.labels, p.Offset, p.Count), nil
}

// DownloadOriginal streams the stored original for a SHA1 file hash, recording
// the request and honouring any configured per-hash error.
func (c *fakeClient) DownloadOriginal(_ context.Context, fileHash string) (*photoprism.Download, error) {
	c.mu.Lock()
	c.downloads = append(c.downloads, fileHash)
	c.mu.Unlock()
	if err, ok := c.downloadErr[fileHash]; ok {
		return nil, err
	}
	data, ok := c.files[fileHash]
	if !ok {
		return nil, photoprism.ErrNotFound
	}
	return &photoprism.Download{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentType:   "image/jpeg",
		ContentLength: int64(len(data)),
	}, nil
}

// downloadCount reports how many originals were requested.
func (c *fakeClient) downloadCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.downloads)
}

// filterUpdated returns the photos updated at or after since (inclusive), or all
// when since is zero.
func filterUpdated(in []photoprism.Photo, since time.Time) []photoprism.Photo {
	if since.IsZero() {
		return in
	}
	out := make([]photoprism.Photo, 0, len(in))
	for _, p := range in {
		if !p.UpdatedAt.Before(since) {
			out = append(out, p)
		}
	}
	return out
}

// page slices a photo list into [offset, offset+count).
func page(in []photoprism.Photo, offset, count int) []photoprism.Photo {
	lo, hi := bounds(len(in), offset, count)
	return in[lo:hi]
}

// pageAlbums slices an album list into [offset, offset+count).
func pageAlbums(in []photoprism.Album, offset, count int) []photoprism.Album {
	lo, hi := bounds(len(in), offset, count)
	return in[lo:hi]
}

// pageLabels slices a label list into [offset, offset+count).
func pageLabels(in []photoprism.Label, offset, count int) []photoprism.Label {
	lo, hi := bounds(len(in), offset, count)
	return in[lo:hi]
}

// bounds clamps an [offset, offset+count) window to a slice of length n.
func bounds(n, offset, count int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > n {
		offset = n
	}
	hi := offset + count
	if count <= 0 || hi > n {
		hi = n
	}
	return offset, hi
}

// fakeRunStore is an in-memory RunStore that tracks the lifecycle of runs and the
// resume watermark.
type fakeRunStore struct {
	runs        []*importer.Run
	startErr    error
	completeErr error
	nextID      int64
}

// Start opens a new running run.
func (s *fakeRunStore) Start(_ context.Context, source importer.Source) (importer.Run, error) {
	if s.startErr != nil {
		return importer.Run{}, s.startErr
	}
	s.nextID++
	run := &importer.Run{ID: s.nextID, Source: source, Status: importer.StatusRunning, StartedAt: time.Now()}
	s.runs = append(s.runs, run)
	return *run, nil
}

// UpdateCounts replaces the counts of the run identified by id.
func (s *fakeRunStore) UpdateCounts(_ context.Context, id int64, counts importer.Counts) error {
	run := s.find(id)
	if run == nil {
		return importer.ErrRunNotFound
	}
	run.Counts = counts
	return nil
}

// Complete closes a run as done with the watermark and counts.
func (s *fakeRunStore) Complete(_ context.Context, id int64, watermark *time.Time, counts importer.Counts) error {
	if s.completeErr != nil {
		return s.completeErr
	}
	run := s.find(id)
	if run == nil {
		return importer.ErrRunNotFound
	}
	run.Status = importer.StatusDone
	run.HighWatermark = watermark
	run.Counts = counts
	return nil
}

// Fail closes a run as failed.
func (s *fakeRunStore) Fail(_ context.Context, id int64, lastErr string, counts importer.Counts) error {
	run := s.find(id)
	if run == nil {
		return importer.ErrRunNotFound
	}
	run.Status = importer.StatusFailed
	run.LastError = lastErr
	run.Counts = counts
	return nil
}

// LatestWatermark returns the watermark of the most recent done run.
func (s *fakeRunStore) LatestWatermark(_ context.Context, _ importer.Source) (time.Time, bool, error) {
	for i := len(s.runs) - 1; i >= 0; i-- {
		run := s.runs[i]
		if run.Status == importer.StatusDone && run.HighWatermark != nil {
			return *run.HighWatermark, true, nil
		}
	}
	return time.Time{}, false, nil
}

// find returns the run with id, or nil.
func (s *fakeRunStore) find(id int64) *importer.Run {
	for _, run := range s.runs {
		if run.ID == id {
			return run
		}
	}
	return nil
}

// last returns the most recently started run.
func (s *fakeRunStore) last() *importer.Run {
	if len(s.runs) == 0 {
		return nil
	}
	return s.runs[len(s.runs)-1]
}

// fakePhotoStore is an in-memory PhotoStore enforcing the file_hash uniqueness the
// real store guarantees.
type fakePhotoStore struct {
	byUID   map[string]photos.Photo
	byPPUID map[string]string
	byHash  map[string]string
	files   map[string][]photos.PhotoFile
	seq     int
}

// newFakePhotoStore returns an empty fakePhotoStore.
func newFakePhotoStore() *fakePhotoStore {
	return &fakePhotoStore{
		byUID:   map[string]photos.Photo{},
		byPPUID: map[string]string{},
		byHash:  map[string]string{},
		files:   map[string][]photos.PhotoFile{},
	}
}

// Create inserts p, rejecting a duplicate file hash with ErrFileHashTaken.
func (s *fakePhotoStore) Create(_ context.Context, p photos.Photo) (photos.Photo, error) {
	if _, ok := s.byHash[p.FileHash]; ok {
		return photos.Photo{}, photos.ErrFileHashTaken
	}
	s.seq++
	p.UID = fmt.Sprintf("ph%08d", s.seq)
	p.CreatedAt, p.UpdatedAt = time.Now(), time.Now()
	s.byUID[p.UID] = p
	s.byHash[p.FileHash] = p.UID
	if p.PhotoprismUID != nil {
		s.byPPUID[*p.PhotoprismUID] = p.UID
	}
	return p, nil
}

// CreateFile records the file row against its photo so tests can assert the
// primary original and any live-photo sidecar were linked.
func (s *fakePhotoStore) CreateFile(_ context.Context, f photos.PhotoFile) (photos.PhotoFile, error) {
	s.files[f.PhotoUID] = append(s.files[f.PhotoUID], f)
	return f, nil
}

// GetByFileHash returns the photo with the given content hash.
func (s *fakePhotoStore) GetByFileHash(_ context.Context, hash string) (photos.Photo, error) {
	uid, ok := s.byHash[hash]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return s.byUID[uid], nil
}

// GetByPhotoprismUID returns the photo imported from the given PhotoPrism UID.
func (s *fakePhotoStore) GetByPhotoprismUID(_ context.Context, ppUID string) (photos.Photo, error) {
	uid, ok := s.byPPUID[ppUID]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return s.byUID[uid], nil
}

// UpdateMetadata applies m to the photo and returns it.
func (s *fakePhotoStore) UpdateMetadata(_ context.Context, uid string, m photos.MetadataUpdate) (photos.Photo, error) {
	p, ok := s.byUID[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	p.Title, p.Description, p.Notes = m.Title, m.Description, m.Notes
	p.TakenAt, p.TakenAtSource = m.TakenAt, m.TakenAtSource
	p.Lat, p.Lng, p.Altitude, p.Private = m.Lat, m.Lng, m.Altitude, m.Private
	s.byUID[uid] = p
	return p, nil
}

// SetPhotoprismRef stamps the external IDs onto the photo and returns it.
func (s *fakePhotoStore) SetPhotoprismRef(_ context.Context, uid, ppUID, ppFileHash string) (photos.Photo, error) {
	p, ok := s.byUID[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	p.PhotoprismUID, p.PhotoprismFileHash = &ppUID, &ppFileHash
	s.byUID[uid] = p
	s.byPPUID[ppUID] = uid
	return p, nil
}

// Delete removes the photo and its indexes.
func (s *fakePhotoStore) Delete(_ context.Context, uid string) error {
	p, ok := s.byUID[uid]
	if !ok {
		return photos.ErrPhotoNotFound
	}
	delete(s.byHash, p.FileHash)
	if p.PhotoprismUID != nil {
		delete(s.byPPUID, *p.PhotoprismUID)
	}
	delete(s.byUID, uid)
	return nil
}

// fakeStorage hashes and records published originals, mirroring the storage
// layer's SHA256 content hashing so the importer's dedup stays consistent.
type fakeStorage struct {
	stored map[string][]byte
}

// newFakeStorage returns an empty fakeStorage.
func newFakeStorage() *fakeStorage {
	return &fakeStorage{stored: map[string][]byte{}}
}

// Store reads src, hashes it, and returns a stored-file descriptor.
func (s *fakeStorage) Store(
	_ context.Context, src io.Reader, _ time.Time, name string,
) (storage.StoredFile, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return storage.StoredFile{}, err
	}
	hash := hashBytes(data)
	rel := "2023/01/" + name
	s.stored[rel] = data
	return storage.StoredFile{Hash: hash, RelPath: rel, Size: int64(len(data)), MIME: "image/jpeg"}, nil
}

// Delete removes a stored original.
func (s *fakeStorage) Delete(_ context.Context, relPath string) error {
	delete(s.stored, relPath)
	return nil
}

// fakeThumbs is a no-op Thumbnailer.
type fakeThumbs struct {
	err error
}

// GenerateAll reports success (or the configured error).
func (t *fakeThumbs) GenerateAll(_ context.Context, _ photos.Photo) (map[string]string, error) {
	return map[string]string{}, t.err
}

// fakeAlbumStore records albums and membership in memory.
type fakeAlbumStore struct {
	albums  []organize.AlbumSummary
	members map[string][]string
	seq     int
}

// newFakeAlbumStore returns an empty fakeAlbumStore.
func newFakeAlbumStore() *fakeAlbumStore {
	return &fakeAlbumStore{members: map[string][]string{}}
}

// ListAlbums returns the recorded albums.
func (s *fakeAlbumStore) ListAlbums(_ context.Context) ([]organize.AlbumSummary, error) {
	return s.albums, nil
}

// CreateAlbum appends a new album with a generated UID.
func (s *fakeAlbumStore) CreateAlbum(_ context.Context, a organize.Album) (organize.Album, error) {
	s.seq++
	a.UID = fmt.Sprintf("al%08d", s.seq)
	s.albums = append(s.albums, organize.AlbumSummary{AlbumCount: organize.AlbumCount{Album: a}})
	return a, nil
}

// AddPhoto records album membership idempotently.
func (s *fakeAlbumStore) AddPhoto(_ context.Context, albumUID, photoUID string) error {
	if slices.Contains(s.members[albumUID], photoUID) {
		return nil
	}
	s.members[albumUID] = append(s.members[albumUID], photoUID)
	return nil
}

// fakeLabelStore records labels and attachments in memory.
type fakeLabelStore struct {
	labels   []organize.LabelCount
	attached map[string][]string
	seq      int
}

// newFakeLabelStore returns an empty fakeLabelStore.
func newFakeLabelStore() *fakeLabelStore {
	return &fakeLabelStore{attached: map[string][]string{}}
}

// ListLabels returns the recorded labels.
func (s *fakeLabelStore) ListLabels(_ context.Context) ([]organize.LabelCount, error) {
	return s.labels, nil
}

// CreateLabel appends a new label with a generated UID.
func (s *fakeLabelStore) CreateLabel(_ context.Context, l organize.Label) (organize.Label, error) {
	s.seq++
	l.UID = fmt.Sprintf("lb%08d", s.seq)
	s.labels = append(s.labels, organize.LabelCount{Label: l})
	return l, nil
}

// AttachLabel records a label attachment idempotently.
func (s *fakeLabelStore) AttachLabel(
	_ context.Context, photoUID, labelUID string, _ organize.LabelSource, _ int,
) error {
	if slices.Contains(s.attached[labelUID], photoUID) {
		return nil
	}
	s.attached[labelUID] = append(s.attached[labelUID], photoUID)
	return nil
}

// fakePeopleStore records subjects and markers in memory.
type fakePeopleStore struct {
	bySlug  map[string]people.Subject
	markers []people.Marker
	seq     int
}

// newFakePeopleStore returns an empty fakePeopleStore.
func newFakePeopleStore() *fakePeopleStore {
	return &fakePeopleStore{bySlug: map[string]people.Subject{}}
}

// GetSubjectBySlug returns the subject with the given slug.
func (s *fakePeopleStore) GetSubjectBySlug(_ context.Context, slug string) (people.Subject, error) {
	subj, ok := s.bySlug[slug]
	if !ok {
		return people.Subject{}, people.ErrSubjectNotFound
	}
	return subj, nil
}

// CreateSubject appends a new subject with a generated UID and slug.
func (s *fakePeopleStore) CreateSubject(_ context.Context, subj people.Subject) (people.Subject, error) {
	s.seq++
	subj.UID = fmt.Sprintf("su%08d", s.seq)
	subj.Slug = people.Slugify(subj.Name)
	s.bySlug[subj.Slug] = subj
	return subj, nil
}

// CreateMarker records a marker.
func (s *fakePeopleStore) CreateMarker(_ context.Context, m people.Marker) (people.Marker, error) {
	s.seq++
	m.UID = fmt.Sprintf("mk%08d", s.seq)
	s.markers = append(s.markers, m)
	return m, nil
}

// fakeEnqueuer records the photo UIDs jobs were scheduled for.
type fakeEnqueuer struct {
	embeds []string
	faces  []string
}

// EnqueueImageEmbed records uid.
func (e *fakeEnqueuer) EnqueueImageEmbed(_ context.Context, uid string) error {
	e.embeds = append(e.embeds, uid)
	return nil
}

// EnqueueFaceDetect records uid.
func (e *fakeEnqueuer) EnqueueFaceDetect(_ context.Context, uid string) error {
	e.faces = append(e.faces, uid)
	return nil
}

// fakeProber is an in-memory VideoProber returning canned metadata for any path,
// recording the paths it probed so tests can assert which staged file (the video
// original or a live photo's motion clip) was probed.
type fakeProber struct {
	meta   video.Metadata
	err    error
	mu     sync.Mutex
	probed []string
	calls  int
}

// Probe records the call and returns the canned metadata (or error).
func (p *fakeProber) Probe(_ context.Context, filePath string) (video.Metadata, error) {
	p.mu.Lock()
	p.calls++
	p.probed = append(p.probed, filePath)
	p.mu.Unlock()
	if p.err != nil {
		return video.Metadata{}, p.err
	}
	return p.meta, nil
}

// probeCalls reports how many times Probe was invoked.
func (p *fakeProber) probeCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// errDownload is a sentinel used to simulate a failed download in tests.
var errDownload = errors.New("download boom")
