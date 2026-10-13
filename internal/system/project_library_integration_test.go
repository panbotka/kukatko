//go:build integration

package system_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/system"
)

// TestLibraryByProject_countsEachProjectApart seeds the shared fixture into the
// bootstrap project and a little of everything into a second one, and proves
// each project is counted on its own — the bootstrap's numbers are exactly the
// fixture's (the instance-wide LibraryStats of it), the second project's are
// its own — while a deactivated project is left out.
func TestLibraryByProject_countsEachProjectApart(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedLibrary(t, db)
	store := system.NewStore(db.Pool())
	svc := system.New(system.Config{Library: store, ProjectLibrary: store})
	whole, err := svc.LibraryStats(t.Context())
	if err != nil {
		t.Fatalf("LibraryStats: %v", err)
	}

	projects := project.NewStore(db.Pool())
	b, err := projects.Create(t.Context(), audit.Meta{}, project.CreateParams{Slug: "b", Name: "B"})
	if err != nil {
		t.Fatalf("creating project b: %v", err)
	}
	gone, err := projects.Create(t.Context(), audit.Meta{}, project.CreateParams{Slug: "gone", Name: "Gone"})
	if err != nil {
		t.Fatalf("creating project gone: %v", err)
	}
	if _, err := db.Pool().Exec(t.Context(), `UPDATE projects SET active = false WHERE uid = $1`, gone.UID); err != nil {
		t.Fatalf("deactivating gone: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO photos (uid, file_hash, file_path, file_name, media_type, project_id)
		 VALUES ('pb1', 'hash-pb1', 'b/pb1.mp4', 'pb1.mp4', 'video', $1)`,
		`INSERT INTO labels (uid, name, slug, project_id) VALUES ('lb-b', 'Jen B', 'jen-b', $1)`,
	} {
		if _, err := db.Pool().Exec(t.Context(), stmt, b.UID); err != nil {
			t.Fatalf("seeding project b: %v", err)
		}
	}

	got, err := svc.LibraryByProject(t.Context())
	if err != nil {
		t.Fatalf("LibraryByProject: %v", err)
	}
	if len(got) != 2 || got[0].Slug != "b" || got[1].Slug != project.BootstrapSlug {
		t.Fatalf("projects = %+v, want b and v (the deactivated one left out)", got)
	}
	bootstrap := got[1].Library
	for name, pair := range map[string][2]int{
		"photos":           {bootstrap.Photos, whole.Photos},
		"images":           {bootstrap.Images, whole.Images},
		"archived":         {bootstrap.PhotosArchived, whole.PhotosArchived},
		"with embedding":   {bootstrap.PhotosWithEmbedding, whole.PhotosWithEmbedding},
		"without faces":    {bootstrap.PhotosWithoutFaces, whole.PhotosWithoutFaces},
		"geocoded":         {bootstrap.PhotosGeocoded, whole.PhotosGeocoded},
		"pending geocode":  {bootstrap.PhotosPendingGeocode, whole.PhotosPendingGeocode},
		"faces":            {bootstrap.Faces, whole.Faces},
		"markers assigned": {bootstrap.MarkersAssigned, whole.MarkersAssigned},
		"unassigned":       {bootstrap.MarkersUnassigned, whole.MarkersUnassigned},
		"subjects":         {bootstrap.Subjects, whole.Subjects},
		"albums":           {bootstrap.Albums, whole.Albums},
		"labels":           {bootstrap.Labels, whole.Labels},
	} {
		if pair[0] != pair[1] {
			t.Errorf("bootstrap %s = %d, want the fixture's %d", name, pair[0], pair[1])
		}
	}
	other := got[0]
	if other.Library.Photos != 1 || other.Library.Videos != 1 || other.Library.Labels != 1 ||
		other.Library.Faces != 0 || other.VideosWithoutStreaming != 1 {
		t.Errorf("project b = %+v, want its one video (unstreamed) and its one label", other)
	}
}
