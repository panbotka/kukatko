package maintenance

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/storekeys"
)

// Project uids of the fixtures: the bootstrap project, a created one and one
// whose row does not exist.
const (
	uidV    = project.BootstrapUID
	uidB    = "pj00000000000000000000000b"
	uidGone = "pj0000000000000000000000go"
)

// prefixOf returns the storage prefix of project uid's own tree.
func prefixOf(uid string) string { return storekeys.ProjectsDir + uid + "/" }

// fakeProjects is a ProjectLister over a fixed list, or one that fails.
type fakeProjects struct {
	list []project.Project
	err  error
}

func (f fakeProjects) List(context.Context) ([]project.Project, error) { return f.list, f.err }

// projectsBeforeMove is an instance with the bootstrap project still in the root
// layout and one created project in its own tree.
func projectsBeforeMove() fakeProjects {
	return fakeProjects{list: []project.Project{
		{UID: uidV, Slug: "v", Name: "Veselice"},
		{UID: uidB, Slug: "b", Name: "Projekt B", StoragePrefix: prefixOf(uidB)},
	}}
}

// projectsAfterMove is the same instance once the bootstrap project's storage
// prefix has been switched to its own tree.
func projectsAfterMove() fakeProjects {
	return fakeProjects{list: []project.Project{
		{UID: uidV, Slug: "v", Name: "Veselice", StoragePrefix: prefixOf(uidV)},
		{UID: uidB, Slug: "b", Name: "Projekt B", StoragePrefix: prefixOf(uidB)},
	}}
}

// projectScenario builds a Service over the given projects, catalogue and store.
// Every catalogued file is a primary original whose presence is what the store
// holds, so a catalogued key missing from the store is a missing original.
func projectScenario(
	projects ProjectLister, files []photos.CataloguedFile, storeKeys []string, importer *fakeImporter,
) *Service {
	present := map[string]bool{}
	for _, key := range storeKeys {
		present[key] = true
	}
	primary := make([]photos.PrimaryFile, 0, len(files))
	paths := make([]string, 0, len(files))
	owners := map[string]string{}
	for i, f := range files {
		primary = append(primary, photos.PrimaryFile{
			PhotoUID: "ph" + string(rune('a'+i)), ProjectUID: f.ProjectUID, FilePath: f.FilePath, FileHash: f.FilePath,
		})
		paths = append(paths, f.FilePath)
		owners[f.FilePath] = f.ProjectUID
	}
	cfg := Config{
		Photos:    &fakePhotos{count: len(files), primary: primary, filePaths: paths, fileProjects: owners},
		Vectors:   &fakeVectors{},
		Originals: fakeOriginals{present: present},
		Store:     fakeStore{keys: storeKeys},
		Projects:  projects,
		Thumbs:    fakeThumbs{have: map[string]bool{}},
		Enqueuer:  &fakeEnqueuer{},
		Embed:     &fakeBackfiller{},
		Faces:     &fakeFaceBackfiller{},
		FaceCache: &fakeFaceCache{},
	}
	if importer != nil {
		cfg.Importer = importer
	}
	return New(cfg)
}

// projectByUID returns the report entry of uid, failing the test when absent.
func projectByUID(t *testing.T, report Report, uid string) ProjectReport {
	t.Helper()
	for _, p := range report.Projects {
		if p.UID == uid {
			return p
		}
	}
	t.Fatalf("report has no entry for project %s: %+v", uid, report.Projects)
	return ProjectReport{}
}

// TestScan_mixedLayoutsReportPerProject scans a store holding the bootstrap
// project's root layout, a created project's tree and the tree of a project that
// no longer exists, and checks every finding lands with its own project — and
// that the orphan project's objects are neither somebody's orphans nor clean.
func TestScan_mixedLayoutsReportPerProject(t *testing.T) {
	t.Parallel()

	files := []photos.CataloguedFile{
		{ProjectUID: uidV, FilePath: "2024/05/a.jpg"},
		{ProjectUID: uidB, FilePath: prefixOf(uidB) + "2024/05/b.jpg"},
		{ProjectUID: uidB, FilePath: prefixOf(uidB) + "2024/05/gone.jpg"},
	}
	store := []string{
		"2024/05/a.jpg",
		"2024/05/stray.jpg",
		"thumb/aa/bb/cc/aabbcc_tile_224.jpg",
		"families.yaml",
		prefixOf(uidB) + "2024/05/b.jpg",
		prefixOf(uidB) + "2024/06/b-orphan.jpg",
		prefixOf(uidB) + "families.yaml",
		prefixOf(uidGone) + "2024/05/x.jpg",
		prefixOf(uidGone) + "thumb/aa/bb/cc/aabbcc_tile_224.jpg",
	}
	report, err := projectScenario(projectsBeforeMove(), files, store, nil).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if got := report.OrphanFiles.Samples; !slices.Equal(got, []string{"2024/05/stray.jpg", prefixOf(uidB) + "2024/06/b-orphan.jpg"}) {
		t.Errorf("instance orphans = %v", got)
	}
	wantGone := []string{prefixOf(uidGone) + "2024/05/x.jpg", prefixOf(uidGone) + "thumb/aa/bb/cc/aabbcc_tile_224.jpg"}
	if report.OrphanProject.Count != 2 || !slices.Equal(report.OrphanProject.Samples, wantGone) {
		t.Errorf("orphan_project = %+v, want %v", report.OrphanProject, wantGone)
	}
	if report.LegacyLayout.Count != 0 {
		t.Errorf("legacy_layout = %+v, want none while the bootstrap project uses the root layout", report.LegacyLayout)
	}
	if report.Store.Originals != 5 {
		t.Errorf("store originals = %d, want 5 (every original in any layout)", report.Store.Originals)
	}
	if report.Clean() {
		t.Error("a scan with an orphan project must not be clean")
	}

	v := projectByUID(t, report, uidV)
	want := ProjectReport{
		UID: uidV, Slug: "v", Name: "Veselice", Photos: 1, FilesInDB: 1, Originals: 2,
		MissingOriginals:  Finding{Count: 0, Samples: []string{}},
		OrphanFiles:       Finding{Count: 1, Samples: []string{"2024/05/stray.jpg"}},
		MissingThumbnails: Finding{Count: 1, Samples: []string{"pha"}},
	}
	if !reflect.DeepEqual(v, want) {
		t.Errorf("project v = %+v, want %+v", v, want)
	}
	b := projectByUID(t, report, uidB)
	want = ProjectReport{
		UID: uidB, Slug: "b", Name: "Projekt B", StoragePrefix: prefixOf(uidB), Photos: 2, FilesInDB: 2, Originals: 2,
		MissingOriginals:  Finding{Count: 1, Samples: []string{"phc"}},
		OrphanFiles:       Finding{Count: 1, Samples: []string{prefixOf(uidB) + "2024/06/b-orphan.jpg"}},
		MissingThumbnails: Finding{Count: 2, Samples: []string{"phb", "phc"}},
	}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("project b = %+v, want %+v", b, want)
	}
	if len(report.Projects) != 2 {
		t.Errorf("projects = %d entries, want 2 (the orphan project is no project)", len(report.Projects))
	}
}

// TestScan_legacyLayoutAfterTheMove covers the bootstrap project half-way
// through and after its storage move. A root original its catalogue still
// points at is its own, not legacy; an uncatalogued root original is legacy, not
// an orphan to import; and a root thumbnail is in use for as long as any
// catalogued file lives at the root, and legacy once none does.
func TestScan_legacyLayoutAfterTheMove(t *testing.T) {
	t.Parallel()

	rootThumb := "thumb/aa/bb/cc/aabbcc_tile_224.jpg"
	tests := map[string]struct {
		files      []photos.CataloguedFile
		store      []string
		wantLegacy []string
		wantVOrigs int
	}{
		"half-way through the move": {
			files: []photos.CataloguedFile{
				{ProjectUID: uidV, FilePath: prefixOf(uidV) + "2024/05/moved.jpg"},
				{ProjectUID: uidV, FilePath: "2024/05/unmoved.jpg"},
			},
			store: []string{
				prefixOf(uidV) + "2024/05/moved.jpg", "2024/05/unmoved.jpg", "2024/05/leftover.jpg", rootThumb,
			},
			wantLegacy: []string{"2024/05/leftover.jpg"},
			wantVOrigs: 2,
		},
		"after the move": {
			files: []photos.CataloguedFile{
				{ProjectUID: uidV, FilePath: prefixOf(uidV) + "2024/05/moved.jpg"},
			},
			store: []string{
				prefixOf(uidV) + "2024/05/moved.jpg", "2024/05/leftover.jpg", rootThumb, "families.yaml",
				"sidecars/2024/05/leftover.jpg.yml",
			},
			wantLegacy: []string{"2024/05/leftover.jpg", "families.yaml", "sidecars/2024/05/leftover.jpg.yml", rootThumb},
			wantVOrigs: 1,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			report, err := projectScenario(projectsAfterMove(), tt.files, tt.store, nil).Scan(context.Background())
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if !slices.Equal(report.LegacyLayout.Samples, tt.wantLegacy) || report.LegacyLayout.Count != len(tt.wantLegacy) {
				t.Errorf("legacy_layout = %+v, want %v", report.LegacyLayout, tt.wantLegacy)
			}
			if report.OrphanFiles.Count != 0 {
				t.Errorf("orphans = %+v, want none: a legacy original is not an orphan to import", report.OrphanFiles)
			}
			if got := projectByUID(t, report, uidV).Originals; got != tt.wantVOrigs {
				t.Errorf("project v originals = %d, want %d", got, tt.wantVOrigs)
			}
			if report.Clean() {
				t.Error("a scan with legacy objects must not be clean")
			}
		})
	}
}

// TestRepairOrphans_importsOnlyProjectOrphans verifies the orphan import offers
// the importer the orphans of real projects' trees and nothing under an orphan
// project or in the legacy layout — no project would receive those.
func TestRepairOrphans_importsOnlyProjectOrphans(t *testing.T) {
	t.Parallel()

	imp := &fakeImporter{outcomes: map[string]ImportOutcome{}}
	files := []photos.CataloguedFile{{ProjectUID: uidV, FilePath: prefixOf(uidV) + "2024/05/a.jpg"}}
	store := []string{
		prefixOf(uidV) + "2024/05/a.jpg",
		prefixOf(uidV) + "2024/05/orphan.jpg",
		prefixOf(uidB) + "2024/05/orphan-b.jpg",
		prefixOf(uidGone) + "2024/05/x.jpg",
		"2024/05/legacy.jpg",
	}
	svc := projectScenario(projectsAfterMove(), files, store, imp)
	res, err := svc.Repair(context.Background(), RepairOptions{ImportOrphans: true}, audit.Meta{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	// In key order, which is the order the scan reports them in.
	want := []string{prefixOf(uidB) + "2024/05/orphan-b.jpg", prefixOf(uidV) + "2024/05/orphan.jpg"}
	if !slices.Equal(imp.imported, want) {
		t.Errorf("imported = %v, want %v", imp.imported, want)
	}
	if res.OrphansImported != 2 {
		t.Errorf("OrphansImported = %d, want 2", res.OrphansImported)
	}
}

// TestScan_withoutProjectListerIsTheBootstrapAlone verifies a Service wired
// without a ProjectLister reports one project — the bootstrap project in the
// root layout — and treats the root layout as its own.
func TestScan_withoutProjectListerIsTheBootstrapAlone(t *testing.T) {
	t.Parallel()

	files := []photos.CataloguedFile{{ProjectUID: uidV, FilePath: "2024/05/a.jpg"}}
	report, err := projectScenario(nil, files, []string{"2024/05/a.jpg"}, nil).Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(report.Projects) != 1 || report.Projects[0].UID != uidV || report.Projects[0].Originals != 1 {
		t.Errorf("projects = %+v, want the bootstrap project holding its one original", report.Projects)
	}
	if report.LegacyLayout.Count != 0 || report.OrphanFiles.Count != 0 {
		t.Errorf("legacy/orphans = %+v/%+v, want none", report.LegacyLayout, report.OrphanFiles)
	}
}

// TestScan_projectListingFails verifies a failure to list the projects aborts
// the scan: without them nothing can be attributed.
func TestScan_projectListingFails(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection refused")
	svc := projectScenario(fakeProjects{err: boom}, nil, nil, nil)
	if _, err := svc.Scan(context.Background()); !errors.Is(err, boom) {
		t.Errorf("Scan error = %v, want it to wrap %v", err, boom)
	}
}

// TestReconciler_attribute pins the attribution rule object by object.
func TestReconciler_attribute(t *testing.T) {
	t.Parallel()

	before := newProjectLedger(projectsBeforeMove().list, defaultSampleLimit)
	after := newProjectLedger(projectsAfterMove().list, defaultSampleLimit)
	rootFile := []photos.CataloguedFile{{ProjectUID: uidV, FilePath: "2024/05/root.jpg"}}
	tests := []struct {
		name      string
		ledger    *projectLedger
		files     []photos.CataloguedFile
		key       string
		wantOwner string
		want      attribution
	}{
		{"root original, root in use", before, nil, "2024/05/x.jpg", uidV, attrOwned},
		{"root thumbnail, root in use", before, nil, "thumb/aa/bb/cc/x_tile_224.jpg", uidV, attrOwned},
		{"created project's original", before, nil, prefixOf(uidB) + "2024/05/x.jpg", uidB, attrOwned},
		{"unknown project's sidecar", before, nil, prefixOf(uidGone) + "sidecars/2024/05/x.jpg.yml", "", attrOrphanProject},
		{"moved: catalogued root original", after, rootFile, "2024/05/root.jpg", uidV, attrOwned},
		{"moved: uncatalogued root original", after, rootFile, "2024/05/other.jpg", "", attrLegacy},
		{"moved: root thumbnail while a file is at the root", after, rootFile, "thumb/aa/bb/cc/x_tile_224.jpg", "", attrInUse},
		{"moved: root thumbnail once none is", after, nil, "thumb/aa/bb/cc/x_tile_224.jpg", "", attrLegacy},
		{"moved: own tree", after, nil, prefixOf(uidV) + "families.yaml", uidV, attrOwned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obj, ok := projectObject(tt.key)
			if !ok {
				t.Fatalf("projectObject(%q) is nobody's", tt.key)
			}
			owner, got := newReconciler(tt.ledger, tt.files).attribute(obj)
			if owner != tt.wantOwner || got != tt.want {
				t.Errorf("attribute(%q) = (%q, %v), want (%q, %v)", tt.key, owner, got, tt.wantOwner, tt.want)
			}
		})
	}
}
