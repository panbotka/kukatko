package familyexportjob

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyexport"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/project"
)

// fakeFamilies answers with a fixed genealogy per project, or with a failure.
type fakeFamilies struct {
	export    family.Export
	byProject map[string]family.Export
	err       error
	reads     []string
}

// Export returns the project's fixed genealogy (or the shared one) and records
// which project was read.
func (f *fakeFamilies) Export(_ context.Context, projectUID string) (family.Export, error) {
	f.reads = append(f.reads, projectUID)
	if f.err != nil {
		return family.Export{}, f.err
	}
	if exp, ok := f.byProject[projectUID]; ok {
		return exp, nil
	}
	return f.export, nil
}

// fakeProjects is an in-memory ProjectStore.
type fakeProjects struct {
	list []project.Project
	err  error
}

// Get returns the project with uid, or project.ErrNotFound.
func (f *fakeProjects) Get(_ context.Context, uid string) (*project.Project, error) {
	for i := range f.list {
		if f.list[i].UID == uid {
			return &f.list[i], nil
		}
	}
	return nil, project.ErrNotFound
}

// List returns every project, or the canned failure.
func (f *fakeProjects) List(context.Context) ([]project.Project, error) {
	return f.list, f.err
}

// bootstrapAndB returns the bootstrap project (root layout) and a created one
// under its own prefix.
func bootstrapAndB() *fakeProjects {
	return &fakeProjects{list: []project.Project{
		{UID: project.BootstrapUID, Slug: "v"},
		{UID: "pjb", Slug: "b", StoragePrefix: "projects/pjb/"},
	}}
}

// written is one document a fakeWriter was handed, with the prefix it went under.
type written struct {
	prefix string
	doc    familyexport.Document
}

// fakeWriter records the documents it was handed.
type fakeWriter struct {
	docs []written
	err  error
}

// Write records doc and returns the export's key under prefix.
func (f *fakeWriter) Write(_ context.Context, prefix string, doc familyexport.Document) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.docs = append(f.docs, written{prefix: prefix, doc: doc})
	return familyexport.KeyFor(prefix), nil
}

// sampleExport returns a one-family genealogy.
func sampleExport() family.Export {
	return family.Export{
		Families: []family.Family{{UID: "fam1", PartnerA: new("sbj1"), Kind: family.KindMarriage}},
		Children: []family.ChildMembership{{FamilyUID: "fam1", ChildUID: "sbj2", Kind: family.ChildBirth}},
		Subjects: []family.ExportSubject{
			{UID: "sbj1", Slug: "bohumil", Name: "Bohumil", Type: "person"},
			{UID: "sbj2", Slug: "jan", Name: "Jan", Type: "person"},
		},
	}
}

// newService builds a Service over the fakes.
func newService(families *fakeFamilies, projects *fakeProjects, writer *fakeWriter) *Service {
	return New(Config{Families: families, Projects: projects, Writer: writer})
}

// TestHandle_writesTheNamedProject is the handler's whole job: read the tree of
// the project the payload names, as it stands, and write it under that
// project's prefix with that project's block — and nobody else's.
func TestHandle_writesTheNamedProject(t *testing.T) {
	t.Parallel()

	families := &fakeFamilies{export: sampleExport()}
	writer := &fakeWriter{}
	job := jobs.Job{Type: jobs.TypeFamilyExport, Payload: []byte(`{"project_uid":"pjb"}`)}

	if err := newService(families, bootstrapAndB(), writer).Handle(t.Context(), job); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(families.reads) != 1 || families.reads[0] != "pjb" {
		t.Errorf("read the genealogy of %v, want only pjb", families.reads)
	}
	if len(writer.docs) != 1 {
		t.Fatalf("wrote %d document(s), want 1", len(writer.docs))
	}
	got := writer.docs[0]
	if got.prefix != "projects/pjb/" {
		t.Errorf("written under %q, want the project's prefix", got.prefix)
	}
	if got.doc.Project != (familyexport.Project{UID: "pjb", Slug: "b"}) {
		t.Errorf("document project = %+v, want pjb/b", got.doc.Project)
	}
	if got.doc.Version != familyexport.Version {
		t.Errorf("document version = %d, want %d", got.doc.Version, familyexport.Version)
	}
	if len(got.doc.Families) != 1 || len(got.doc.Subjects) != 2 || len(got.doc.Families[0].Children) != 1 {
		t.Errorf("document = %+v, want one family with one child and two subjects", got.doc)
	}
}

// TestHandle_legacyPayloadIsTheBootstrapProject pins how a job queued before the
// export was per project is read: no project_uid (or no payload at all) is the
// bootstrap project, written at the root where its file always was.
func TestHandle_legacyPayloadIsTheBootstrapProject(t *testing.T) {
	t.Parallel()

	for _, payload := range []string{"", `{}`, `{"photo_uid":"pht1","nonsense":true}`} {
		families := &fakeFamilies{export: sampleExport()}
		writer := &fakeWriter{}
		job := jobs.Job{Type: jobs.TypeFamilyExport, Payload: []byte(payload)}
		if err := newService(families, bootstrapAndB(), writer).Handle(t.Context(), job); err != nil {
			t.Fatalf("Handle(%q) returned error: %v", payload, err)
		}
		if len(writer.docs) != 1 || writer.docs[0].prefix != "" ||
			writer.docs[0].doc.Project.UID != project.BootstrapUID {
			t.Errorf("Handle(%q) wrote %+v, want one bootstrap document at the root", payload, writer.docs)
		}
	}
}

// TestHandle_rejectsAMalformedPayload verifies a payload that is not JSON fails
// the job rather than being guessed at.
func TestHandle_rejectsAMalformedPayload(t *testing.T) {
	t.Parallel()

	writer := &fakeWriter{}
	job := jobs.Job{Type: jobs.TypeFamilyExport, Payload: []byte(`{not json`)}
	if err := newService(&fakeFamilies{}, bootstrapAndB(), writer).Handle(t.Context(), job); err == nil {
		t.Error("Handle accepted a malformed payload, want an error")
	}
	if len(writer.docs) != 0 {
		t.Errorf("wrote %d document(s), want none", len(writer.docs))
	}
}

// TestHandle_skipsAGoneProject verifies a job for a project that no longer exists
// completes without writing: its families went with it.
func TestHandle_skipsAGoneProject(t *testing.T) {
	t.Parallel()

	writer := &fakeWriter{}
	job := jobs.Job{Type: jobs.TypeFamilyExport, Payload: []byte(`{"project_uid":"pjgone"}`)}
	if err := newService(&fakeFamilies{}, bootstrapAndB(), writer).Handle(t.Context(), job); err != nil {
		t.Fatalf("Handle for a gone project = %v, want nil", err)
	}
	if len(writer.docs) != 0 {
		t.Errorf("wrote %d document(s) for a gone project, want none", len(writer.docs))
	}
}

// TestExportAll_writesEveryProjectToItsOwnKey verifies the CLI path: every
// project gets its own document, with its own genealogy and block, at its own key.
func TestExportAll_writesEveryProjectToItsOwnKey(t *testing.T) {
	t.Parallel()

	families := &fakeFamilies{byProject: map[string]family.Export{"pjb": sampleExport()}}
	writer := &fakeWriter{}
	keys, err := newService(families, bootstrapAndB(), writer).ExportAll(t.Context())
	if err != nil {
		t.Fatalf("ExportAll returned error: %v", err)
	}
	want := []string{familyexport.Key, "projects/pjb/" + familyexport.Key}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	if len(writer.docs) != 2 {
		t.Fatalf("wrote %d document(s), want 2", len(writer.docs))
	}
	if writer.docs[0].doc.Project.UID != project.BootstrapUID || len(writer.docs[0].doc.Families) != 0 {
		t.Errorf("first document = %+v, want the bootstrap project's empty tree", writer.docs[0].doc)
	}
	if writer.docs[1].doc.Project.UID != "pjb" || len(writer.docs[1].doc.Families) != 1 {
		t.Errorf("second document = %+v, want pjb's one family", writer.docs[1].doc)
	}
}

// TestExportAll_reportsAFailedListing verifies a project list that cannot be read
// fails the command rather than reporting nothing written as success.
func TestExportAll_reportsAFailedListing(t *testing.T) {
	t.Parallel()

	broken := errors.New("connection refused")
	_, err := newService(&fakeFamilies{}, &fakeProjects{err: broken}, &fakeWriter{}).ExportAll(t.Context())
	if !errors.Is(err, broken) {
		t.Errorf("ExportAll error = %v, want it to wrap %v", err, broken)
	}
}

// TestExport_writesAnEmptyGenealogy verifies a project whose last relation was
// removed is written as an empty document rather than skipped. Skipping would
// leave yesterday's tree on disk, and a rebuild would restore relations the user
// deleted.
func TestExport_writesAnEmptyGenealogy(t *testing.T) {
	t.Parallel()

	writer := &fakeWriter{}
	if _, err := newService(&fakeFamilies{}, bootstrapAndB(), writer).Export(t.Context(), "pjb"); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if len(writer.docs) != 1 {
		t.Fatalf("wrote %d document(s), want 1", len(writer.docs))
	}
	if len(writer.docs[0].doc.Families) != 0 {
		t.Errorf("document = %+v, want no families", writer.docs[0].doc)
	}
}

// TestExport_unknownProjectIsNotFound verifies a project that does not exist is
// reported as such, which is what lets Handle skip it.
func TestExport_unknownProjectIsNotFound(t *testing.T) {
	t.Parallel()

	_, err := newService(&fakeFamilies{}, bootstrapAndB(), &fakeWriter{}).Export(t.Context(), "pjgone")
	if !errors.Is(err, project.ErrNotFound) {
		t.Errorf("Export error = %v, want project.ErrNotFound", err)
	}
}

// TestExport_reportsAFailedRead verifies a database that cannot be read fails the
// job — so it is retried — rather than writing an empty file over a good one.
func TestExport_reportsAFailedRead(t *testing.T) {
	t.Parallel()

	broken := errors.New("connection refused")
	writer := &fakeWriter{}

	_, err := newService(&fakeFamilies{err: broken}, bootstrapAndB(), writer).Export(t.Context(), "pjb")
	if !errors.Is(err, broken) {
		t.Fatalf("Export error = %v, want it to wrap %v", err, broken)
	}
	if len(writer.docs) != 0 {
		t.Errorf("wrote %d document(s) after a failed read, want none", len(writer.docs))
	}
}

// TestExport_reportsAFailedWrite verifies a store that refuses the write fails
// the job, so the queue retries it instead of reporting a tree that is not there.
func TestExport_reportsAFailedWrite(t *testing.T) {
	t.Parallel()

	refused := errors.New("bucket unreachable")
	_, err := newService(&fakeFamilies{export: sampleExport()}, bootstrapAndB(),
		&fakeWriter{err: refused}).Export(t.Context(), "pjb")
	if !errors.Is(err, refused) {
		t.Fatalf("Export error = %v, want it to wrap %v", err, refused)
	}
	if !strings.Contains(err.Error(), "family tree") {
		t.Errorf("Export error = %q, want it to say what failed", err)
	}
}

// TestNew_requiresItsCollaborators pins each wiring bug as a startup panic rather
// than a nil dereference on the first job.
func TestNew_requiresItsCollaborators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "no families", cfg: Config{Projects: &fakeProjects{}, Writer: &fakeWriter{}}},
		{name: "no projects", cfg: Config{Families: &fakeFamilies{}, Writer: &fakeWriter{}}},
		{name: "no writer", cfg: Config{Families: &fakeFamilies{}, Projects: &fakeProjects{}}},
		{name: "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("New did not panic on a missing dependency")
				}
			}()
			New(tt.cfg)
		})
	}
}
