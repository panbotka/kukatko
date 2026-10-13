//go:build integration

package system_test

import (
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/system"
	"github.com/panbotka/kukatko/internal/vectors"
)

// seedProjectExtras gives the project of tenant rows the projecttest fixture
// does not seed, so its counts and charts differ from the other tenant's in
// every series: a second photo (a video with a camera, taken in 2019, with a
// nameless marker), an embedding, a detected face and a geocoded place on the
// tenant's seeded photo. embeddings and faces name the project explicitly —
// their column defaults to the bootstrap project.
func seedProjectExtras(t *testing.T, db *database.DB, tenant *projecttest.Tenant) {
	t.Helper()
	const extra = "pbxextra"
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO photos (uid, project_id, file_hash, file_path, file_name, file_size, media_type,
			camera_make, camera_model, taken_at)
			VALUES ($1, $2, 'hash-pbxextra', 'b/pbxextra.mp4', 'pbxextra.mp4', 4096, 'video',
			'Nikon', 'Z6', '2019-05-01T12:00:00Z')`, []any{extra, tenant.ProjectUID}},
		{`INSERT INTO markers (uid, photo_uid) VALUES ('mkpbxextra', $1)`, []any{extra}},
		{`INSERT INTO embeddings (photo_uid, embedding, project_id) VALUES ($1, $2, $3)`,
			[]any{tenant.PhotoUID, vectors.ToHalfVec(make([]float32, vectors.ImageDim)), tenant.ProjectUID}},
		{`INSERT INTO faces (photo_uid, face_index, embedding, bbox, project_id) VALUES ($1, 0, $2, $3, $4)`,
			[]any{tenant.PhotoUID, vectors.ToHalfVec(make([]float32, vectors.FaceDim)),
				[]float64{0.1, 0.1, 0.2, 0.2}, tenant.ProjectUID}},
		{`INSERT INTO photo_places (photo_uid, country, lat, lng) VALUES ($1, 'Česko', 50.08, 14.44)`,
			[]any{tenant.PhotoUID}},
	}
	for _, s := range statements {
		if _, err := db.Pool().Exec(t.Context(), s.sql, s.args...); err != nil {
			t.Fatalf("seeding the extras of %s: %v", tenant.Slug, err)
		}
	}
}

// TestLibraryStats_neverCrossProjects seeds the two-tenant fixture, gives B a
// second photo plus an embedding, a face and a place, and proves each
// project's counts are its own: A's are exactly the fixture's one photo, its
// person, marker, album and label — none of B's rows — and B's are its own,
// read through the same service so the per-project memoisation is exercised
// too. An empty project is refused, never counted as the whole instance.
func TestLibraryStats_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	seedProjectExtras(t, db, &f.B)
	store := system.NewStore(db.Pool())
	svc := system.New(system.Config{Library: store})

	common := system.Library{
		Subjects: 1, SubjectsPerson: 1, MarkersAssigned: 1, Albums: 1, AlbumsManual: 1, Labels: 1,
	}
	wantA := common
	wantA.Photos, wantA.Images, wantA.PhotosLive, wantA.PhotosListed = 1, 1, 1, 1
	wantA.PhotosWithoutEmbedding, wantA.PhotosWithoutFaces, wantA.Markers = 1, 1, 1
	wantB := common
	wantB.Photos, wantB.Videos, wantB.Images, wantB.PhotosLive, wantB.PhotosListed = 2, 1, 1, 2, 2
	wantB.PhotosWithEmbedding, wantB.PhotosWithFaces, wantB.PhotosGeocoded = 1, 1, 1
	wantB.PhotosWithoutEmbedding, wantB.PhotosWithoutFaces = 1, 1
	wantB.Embeddings, wantB.Faces, wantB.FacesUnassigned = 1, 1, 1
	wantB.Markers, wantB.MarkersUnassigned = 2, 1

	// B is asked first and A after it, so a memoisation shared across projects
	// would hand A B's snapshot.
	for _, tc := range []struct {
		side projecttest.Side
		want system.Library
	}{{projecttest.SideB, wantB}, {projecttest.SideA, wantA}} {
		got, err := svc.LibraryStats(t.Context(), f.Tenant(tc.side).ProjectUID)
		if err != nil {
			t.Fatalf("LibraryStats(%s): %v", tc.side, err)
		}
		if got != tc.want {
			t.Errorf("LibraryStats(%s) = %+v, want %+v", tc.side, got, tc.want)
		}
	}
	if _, err := store.CountLibrary(t.Context(), ""); !errors.Is(err, system.ErrNoProject) {
		t.Errorf("CountLibrary without a project = %v, want ErrNoProject", err)
	}
}

// TestLibraryCharts_neverCrossProjects proves every chart series counts its
// own project only: B's 2019 video, its camera and its bytes appear in B's
// charts and nowhere in A's, whose series hold the fixture's one camera-less
// 2026 photo and nothing else.
func TestLibraryCharts_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	seedProjectExtras(t, db, &f.B)
	store := system.NewStore(db.Pool())
	svc := system.New(system.Config{Charts: store})

	for _, tc := range []struct {
		side    projecttest.Side
		photos  int
		cameras int
		videos  int
		bytes   int64
	}{
		{side: projecttest.SideB, photos: 2, cameras: 1, videos: 1, bytes: 1024 + 4096},
		{side: projecttest.SideA, photos: 1, cameras: 0, videos: 0, bytes: 1024},
	} {
		charts, err := svc.LibraryCharts(t.Context(), f.Tenant(tc.side).ProjectUID)
		if err != nil {
			t.Fatalf("LibraryCharts(%s): %v", tc.side, err)
		}
		if got := sumPhotosByYear(charts); got != tc.photos {
			t.Errorf("%s photos by year add up to %d, want %d", tc.side, got, tc.photos)
		}
		if len(charts.TopCameras) != tc.cameras {
			t.Errorf("%s top cameras = %+v, want %d", tc.side, charts.TopCameras, tc.cameras)
		}
		if got := videoBucket(charts); got != tc.videos {
			t.Errorf("%s video bucket = %d photos, want %d", tc.side, got, tc.videos)
		}
		if n := len(charts.StorageByYear); n == 0 || charts.StorageByYear[n-1].CumulativeBytes != tc.bytes {
			t.Errorf("%s storage by year = %+v, want %d bytes in total", tc.side, charts.StorageByYear, tc.bytes)
		}
	}
	if _, err := store.AggregateCharts(t.Context(), "", time.Now()); !errors.Is(err, system.ErrNoProject) {
		t.Errorf("AggregateCharts without a project = %v, want ErrNoProject", err)
	}
}

// sumPhotosByYear adds up the capture-year histogram.
func sumPhotosByYear(charts system.Charts) int {
	total := 0
	for _, year := range charts.PhotosByYear {
		total += year.Photos
	}
	return total
}

// videoBucket returns how many photos the storage-by-media breakdown puts in
// the video bucket.
func videoBucket(charts system.Charts) int {
	for _, bucket := range charts.StorageByMedia {
		if bucket.Media == "video" {
			return bucket.Photos
		}
	}
	return 0
}
