package psfeedsimport

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/psfeeds"
	"github.com/panbotka/kukatko/internal/vectors"
)

// fakeFeeds serves in-memory embeddings and faces with real keyset pagination, so
// the importer's paging loop is exercised without a real photo-sorter.
type fakeFeeds struct {
	embeddings []psfeeds.Embedding
	faces      []psfeeds.Face
	embErr     error
	faceErr    error
}

// ListEmbeddings returns one keyset page of embeddings ordered by photo_uid.
func (f *fakeFeeds) ListEmbeddings(_ context.Context, limit int, after string) (psfeeds.EmbeddingsPage, error) {
	if f.embErr != nil {
		return psfeeds.EmbeddingsPage{}, f.embErr
	}
	items := append([]psfeeds.Embedding(nil), f.embeddings...)
	sort.Slice(items, func(i, j int) bool { return items[i].PhotoUID < items[j].PhotoUID })
	var page []psfeeds.Embedding
	for _, it := range items {
		if it.PhotoUID > after && len(page) < limit {
			page = append(page, it)
		}
	}
	out := psfeeds.EmbeddingsPage{Embeddings: page, Total: len(items)}
	if len(page) == limit && len(page) > 0 {
		last := page[len(page)-1].PhotoUID
		out.NextAfter = &last
	}
	return out, nil
}

// ListFaces returns one keyset page of faces ordered by id.
func (f *fakeFeeds) ListFaces(_ context.Context, limit int, after int64) (psfeeds.FacesPage, error) {
	if f.faceErr != nil {
		return psfeeds.FacesPage{}, f.faceErr
	}
	items := append([]psfeeds.Face(nil), f.faces...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	var page []psfeeds.Face
	for _, it := range items {
		if it.ID > after && len(page) < limit {
			page = append(page, it)
		}
	}
	out := psfeeds.FacesPage{Faces: page, Total: len(items)}
	if len(page) == limit && len(page) > 0 {
		last := page[len(page)-1].ID
		out.NextAfter = &last
	}
	return out, nil
}

// fakePhotos resolves photos by PhotoPrism UID from an in-memory map.
type fakePhotos struct {
	byPPUID map[string]photos.Photo
}

func (f *fakePhotos) GetByPhotoprismUID(_ context.Context, ppUID string) (photos.Photo, error) {
	if p, ok := f.byPPUID[ppUID]; ok {
		return p, nil
	}
	return photos.Photo{}, photos.ErrPhotoNotFound
}

// savedFaces records one RecordFaceDetection call.
type savedFaces struct {
	faces []vectors.Face
	model string
}

// fakeVectors records the embeddings and faces the importer writes.
type fakeVectors struct {
	embeddings map[string]vectors.Embedding
	faces      map[string]savedFaces
	embErr     error
	faceErr    error
}

func newFakeVectors() *fakeVectors {
	return &fakeVectors{embeddings: map[string]vectors.Embedding{}, faces: map[string]savedFaces{}}
}

func (f *fakeVectors) SaveEmbedding(_ context.Context, emb vectors.Embedding) (vectors.Embedding, error) {
	if f.embErr != nil {
		return vectors.Embedding{}, f.embErr
	}
	f.embeddings[emb.PhotoUID] = emb
	return emb, nil
}

func (f *fakeVectors) RecordFaceDetection(_ context.Context, photoUID string, faces []vectors.Face, model string) error {
	if f.faceErr != nil {
		return f.faceErr
	}
	f.faces[photoUID] = savedFaces{faces: append([]vectors.Face(nil), faces...), model: model}
	return nil
}

// fakePeople is an in-memory subjects+markers store: subjects keyed by slug,
// markers by uid, matching people.Store's find-or-create-by-slug and
// preserve-uid behaviour closely enough to test idempotency.
type fakePeople struct {
	subjectsBySlug map[string]people.Subject
	markers        map[string]people.Marker
	nextSubject    int
	createSubjects int
	createMarkers  int
}

func newFakePeople() *fakePeople {
	return &fakePeople{subjectsBySlug: map[string]people.Subject{}, markers: map[string]people.Marker{}}
}

func (f *fakePeople) GetSubjectBySlug(_ context.Context, slug string) (people.Subject, error) {
	if s, ok := f.subjectsBySlug[slug]; ok {
		return s, nil
	}
	return people.Subject{}, people.ErrSubjectNotFound
}

func (f *fakePeople) CreateSubject(_ context.Context, subj people.Subject) (people.Subject, error) {
	f.nextSubject++
	f.createSubjects++
	subj.UID = fmt.Sprintf("su%08d", f.nextSubject)
	subj.Slug = people.Slugify(subj.Name)
	if subj.Type == "" {
		subj.Type = people.SubjectPerson
	}
	f.subjectsBySlug[subj.Slug] = subj
	return subj, nil
}

func (f *fakePeople) GetMarkerByUID(_ context.Context, uid string) (people.Marker, error) {
	if m, ok := f.markers[uid]; ok {
		return m, nil
	}
	return people.Marker{}, people.ErrMarkerNotFound
}

func (f *fakePeople) CreateMarker(_ context.Context, m people.Marker) (people.Marker, error) {
	f.createMarkers++
	f.markers[m.UID] = m
	return m, nil
}

// fakeRuns records the import-run lifecycle so tests can assert the final status.
// Like the real importer.Store it closes a run 'partial' rather than 'done' when
// the run has any recorded failure.
type fakeRuns struct {
	started      int
	completed    int
	failed       int
	lastCounts   importer.Counts
	lastError    string
	lastWatermk  *time.Time
	lastSource   importer.Source
	updateCounts int
	failures     []importer.Failure
	lastStatus   importer.Status
}

func (f *fakeRuns) Start(_ context.Context, source importer.Source) (importer.Run, error) {
	f.started++
	f.lastSource = source
	return importer.Run{ID: 1, Source: source, Status: importer.StatusRunning}, nil
}

func (f *fakeRuns) UpdateCounts(_ context.Context, _ int64, counts importer.Counts) error {
	f.updateCounts++
	f.lastCounts = counts
	return nil
}

func (f *fakeRuns) Complete(_ context.Context, id int64, watermark *time.Time, counts importer.Counts) error {
	f.completed++
	f.lastCounts = counts
	f.lastWatermk = watermark
	f.lastStatus = importer.StatusDone
	if f.unresolvedFailures(id) > 0 {
		f.lastStatus = importer.StatusPartial
	}
	return nil
}

func (f *fakeRuns) Fail(_ context.Context, _ int64, lastErr string, counts importer.Counts) error {
	f.failed++
	f.lastError = lastErr
	f.lastCounts = counts
	f.lastStatus = importer.StatusFailed
	return nil
}

// RecordFailures appends the run's per-item failures.
func (f *fakeRuns) RecordFailures(_ context.Context, failures []importer.Failure) error {
	f.failures = append(f.failures, failures...)
	return nil
}

// unresolvedFailures counts the outstanding failures recorded for run id.
func (f *fakeRuns) unresolvedFailures(id int64) int {
	n := 0
	for _, fl := range f.failures {
		if fl.RunID == id && fl.ResolvedAt == nil {
			n++
		}
	}
	return n
}
