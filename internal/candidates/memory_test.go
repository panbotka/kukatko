package candidates

// The allocation-ceiling tests. On 2026-08-02 a single GET /api/v1/review/queue
// grew the server process to 10.9 GB anon-rss and the host OOM killer took the
// whole box down with it (global_oom — photoprism, mariadb and the embeddings
// sidecar were collateral). The queue rebuild's only guard was a 15 s deadline,
// which bounds time and not memory: inside those 15 s the per-subject candidate
// search ran one kNN per exemplar (16 532 of them for one subject) and then
// hydrated every survivor into a full photos.Photo — EXIF blob included — three
// times over on the way to the caller.
//
// So the regression these tests guard is allocation, and it is structural: the
// bytes one search allocates must not follow either the subject's exemplar count
// or the number of unnamed faces in the library. A wall-clock or RSS assertion
// would be flaky on a shared machine; bytes allocated is exact and reproducible.

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// exifBlobBytes is how much raw EXIF JSON a catalogued photo carries. Production
// rows sit in the low kilobytes; it matters because photos.Photo is copied into
// every Candidate, so hydrating an unbounded survivor set multiplies it.
const exifBlobBytes = 2048

// scaleFaces is a FaceStore over a synthetic library: a subject with exemplars
// tagged faces, and a pool of unnamed faces the exemplars' kNNs return windows
// of. It allocates its result slices per call, exactly as the pgx-backed store
// does, so the measured allocation reflects the real fan-out.
type scaleFaces struct {
	exemplars int
	pool      int
	// spread is how far apart consecutive exemplars' result windows sit in the
	// pool. Zero means every exemplar returns the same head window, which holds
	// the number of distinct candidates fixed while the exemplar count varies;
	// a positive spread walks the windows across the pool so the distinct
	// candidate count follows the library instead.
	spread int
}

// SampleFacesBySubject returns at most limit of the subject's tagged faces, one
// per source photo, each carrying a full FaceDim embedding whose first element
// encodes the exemplar's index so the fake kNN can tell them apart. It samples
// with an even stride, and — crucially for what these tests measure — only
// materialises the sample, exactly as the SQL behind the real store does.
func (s *scaleFaces) SampleFacesBySubject(
	_ context.Context, _ string, limit int,
) (vectors.SubjectFaces, error) {
	n := s.exemplars
	step := 1
	if limit > 0 && n > limit {
		step, n = n/limit, limit
	}
	out := vectors.SubjectFaces{
		Faces: make([]vectors.Face, 0, n), Total: s.exemplars, Photos: s.exemplars,
	}
	for i := range n {
		vec := make([]float32, vectors.FaceDim)
		vec[0] = float32(i * step)
		out.Faces = append(out.Faces, vectors.Face{
			PhotoUID: fmt.Sprintf("src%06d", i*step), FaceIndex: 0, Vector: vec, DetScore: 0.9,
		})
	}
	return out, nil
}

// FindSimilarUnassignedFaceCandidates returns this exemplar's window of the
// unnamed-face pool, each face at a stable distance inside the allowed range.
func (s *scaleFaces) FindSimilarUnassignedFaceCandidates(
	_ context.Context, vec []float32, limit int, maxDistance float64, _ []vectors.FaceKey,
) ([]vectors.FaceCandidate, error) {
	n := min(limit, s.pool)
	start := 0
	if s.spread > 0 {
		start = (int(vec[0]) * s.spread) % s.pool
	}
	out := make([]vectors.FaceCandidate, 0, n)
	for i := range n {
		at := (start + i) % s.pool
		out = append(out, vectors.FaceCandidate{
			PhotoUID:  fmt.Sprintf("cand%06d", at),
			FaceIndex: 0,
			Distance:  maxDistance * float64(at%100) / 100,
			BBox:      [4]float64{0.3, 0.3, 0.3, 0.3},
		})
	}
	return out, nil
}

// FacesByKeys returns an embedded face per requested key.
func (s *scaleFaces) FacesByKeys(_ context.Context, keys []vectors.FaceKey) ([]vectors.Face, error) {
	out := make([]vectors.Face, 0, len(keys))
	for _, key := range keys {
		out = append(out, vectors.Face{
			PhotoUID: key.PhotoUID, FaceIndex: key.FaceIndex, Vector: make([]float32, vectors.FaceDim),
		})
	}
	return out, nil
}

// scalePeople is a PeopleStore that knows one subject and no markers.
type scalePeople struct{ exemplars int }

func (s *scalePeople) GetSubjectByUID(_ context.Context, uid string) (people.Subject, error) {
	return people.Subject{UID: uid, Name: uid}, nil
}

func (s *scalePeople) GetMarkerByUID(context.Context, string) (people.Marker, error) {
	return people.Marker{}, people.ErrMarkerNotFound
}

func (s *scalePeople) ListPhotoUIDsBySubject(context.Context, string) ([]string, error) {
	out := make([]string, 0, s.exemplars)
	for i := range s.exemplars {
		out = append(out, fmt.Sprintf("src%06d", i))
	}
	return out, nil
}

// scalePhotos hydrates candidate uids into full catalogue rows, EXIF blob and
// all — the per-candidate cost the real store pays.
type scalePhotos struct{ exif []byte }

func (s *scalePhotos) ListByUIDs(_ context.Context, uids []string) ([]photos.Photo, error) {
	out := make([]photos.Photo, 0, len(uids))
	for _, uid := range uids {
		blob := make([]byte, len(s.exif))
		copy(blob, s.exif)
		out = append(out, photos.Photo{
			UID: uid, FileHash: uid + "-hash", FilePath: "2024/01/" + uid + ".jpg",
			FileName: uid + ".jpg", FileWidth: 4000, FileHeight: 3000, FileOrientation: 1,
			Exif: blob,
		})
	}
	return out, nil
}

// scaleService wires a Service over the synthetic library at the production
// tunables, so the bound under test is the shipped one.
func scaleService(exemplars, pool, spread int) *Service {
	exif := make([]byte, exifBlobBytes)
	for i := range exif {
		exif[i] = 'x'
	}
	return New(Config{
		Faces:    &scaleFaces{exemplars: exemplars, pool: pool, spread: spread},
		People:   &scalePeople{exemplars: exemplars},
		Feedback: &fakeFeedback{},
		Photos:   &scalePhotos{exif: exif},
		Media:    mediaurl.NewBuilder(nil),
		// The production defaults, spelled out so a config change cannot quietly
		// widen what this test measures.
		MaxDistance: DefaultMaxDistance, SearchLimit: 500, MinFacePx: DefaultMinFacePx,
		Concurrency: DefaultConcurrency, MinFaceRel: 0.02,
	})
}

// allocatedBytes reports how many bytes the process allocated while fn ran. It
// counts every goroutine's allocation, which is what we want: the search fans
// out over a worker pool. The test calling it must not be parallel.
func allocatedBytes(tb testing.TB, fn func()) uint64 {
	tb.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.GC()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// findAllocation measures one Find over a synthetic library of the given shape.
func findAllocation(tb testing.TB, exemplars, pool, spread int) uint64 {
	tb.Helper()
	svc := scaleService(exemplars, pool, spread)
	var res Result
	bytes := allocatedBytes(tb, func() {
		var err error
		res, err = svc.Find(context.Background(), "su_scale", Request{})
		if err != nil {
			tb.Fatalf("Find(exemplars=%d, pool=%d): %v", exemplars, pool, err)
		}
	})
	if len(res.Candidates) == 0 {
		tb.Fatalf("Find(exemplars=%d, pool=%d) returned no candidates; the fake library is wrong",
			exemplars, pool)
	}
	return bytes
}

// searchCeilingBytes is the ceiling one candidate search may allocate, whatever
// the library looks like. The structural worst case is MaxExemplars kNN queries
// of vectors' 500-row maximum each, merged into one voted set — a constant a few
// tens of megabytes wide — so this is generous enough not to police the last
// megabyte and two orders of magnitude below the 10.9 GB that killed the box.
const searchCeilingBytes = 96 << 20 // 96 MiB

// TestFind_allocationDoesNotScaleWithExemplarCount checks the per-subject search
// costs the same whether a person is tagged on a thousand photos or twenty
// thousand. The catch-all-subject bug left one "person" holding 16 532 exemplars,
// and the search ran one kNN — and merged one result set — per exemplar, so the
// bytes followed the tagging.
func TestFind_allocationDoesNotScaleWithExemplarCount(t *testing.T) {
	// Both sizes are above MaxExemplars, so a bounded search does identical work
	// for them; every exemplar returns the same window, holding the number of
	// distinct candidates fixed so only the exemplar count varies.
	const pool = 2000
	small := findAllocation(t, 2*DefaultMaxExemplars, pool, 0)
	large := findAllocation(t, 40*DefaultMaxExemplars, pool, 0)
	t.Logf("allocated: %d exemplars %d B, %d exemplars %d B",
		2*DefaultMaxExemplars, small, 40*DefaultMaxExemplars, large)

	if large > searchCeilingBytes {
		t.Errorf("a subject with %d exemplars allocated %d B, want at most %d B — the search "+
			"must be bounded by the exemplar cap, not by how heavily the person is tagged",
			40*DefaultMaxExemplars, large, searchCeilingBytes)
	}
	// 20x the exemplars, both past the cap: the work is the same, so the bytes
	// must be too. Only the subject's own row count may still show through.
	if ratio := float64(large) / float64(small); ratio > 1.5 {
		t.Errorf("20x the exemplars allocated %.2fx the memory (%d B vs %d B), want at most 1.5x",
			ratio, large, small)
	}
}

// TestFind_allocationDoesNotScaleWithLibrarySize checks the search stays inside
// its ceiling when a subject matches a huge number of unnamed faces. This is the
// second axis: hydrating every survivor into a full photos.Photo (EXIF blob
// included) and copying it into a Candidate is what turned a wide match into
// hundreds of megabytes, and truncating afterwards bounded the answer but not
// the work.
func TestFind_allocationDoesNotScaleWithLibrarySize(t *testing.T) {
	// The exemplar count is held fixed; the windows walk the pool so the number
	// of distinct candidates — and only that — grows with the library.
	const exemplars = 500
	small := findAllocation(t, exemplars, 500, 1)
	large := findAllocation(t, exemplars, 40000, 40000/exemplars)
	t.Logf("allocated: 500 unnamed faces %d B, 40000 unnamed faces %d B", small, large)

	if large > searchCeilingBytes {
		t.Errorf("a subject matching 40000 unnamed faces allocated %d B, want at most %d B — "+
			"one request must not scale with the library",
			large, searchCeilingBytes)
	}
	// 80x the matches. The voted set still has to be seen to be ranked, so some
	// growth is honest; hydrating them all is not.
	if ratio := float64(large) / float64(small); ratio > 2 {
		t.Errorf("80x the unnamed faces allocated %.2fx the memory (%d B vs %d B), want at most 2x",
			ratio, large, small)
	}
}
