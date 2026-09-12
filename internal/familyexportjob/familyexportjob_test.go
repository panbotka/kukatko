package familyexportjob

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyexport"
	"github.com/panbotka/kukatko/internal/jobs"
)

// fakeFamilies answers with a fixed genealogy, or with a failure.
type fakeFamilies struct {
	export family.Export
	err    error
	reads  int
}

// Export returns the fixed genealogy and counts the read.
func (f *fakeFamilies) Export(context.Context) (family.Export, error) {
	f.reads++
	if f.err != nil {
		return family.Export{}, f.err
	}
	return f.export, nil
}

// fakeWriter records the documents it was handed.
type fakeWriter struct {
	docs []familyexport.Document
	err  error
}

// Write records doc and returns the export's key.
func (f *fakeWriter) Write(_ context.Context, doc familyexport.Document) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.docs = append(f.docs, doc)
	return familyexport.Key, nil
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

// TestHandle_writesTheCurrentGenealogy is the handler's whole job: read the tree
// as it stands, write it.
func TestHandle_writesTheCurrentGenealogy(t *testing.T) {
	t.Parallel()

	families := &fakeFamilies{export: sampleExport()}
	writer := &fakeWriter{}

	if err := New(Config{Families: families, Writer: writer}).Handle(t.Context(), jobs.Job{}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(writer.docs) != 1 {
		t.Fatalf("wrote %d document(s), want 1", len(writer.docs))
	}
	doc := writer.docs[0]
	if doc.Version != familyexport.Version {
		t.Errorf("document version = %d, want %d", doc.Version, familyexport.Version)
	}
	if len(doc.Families) != 1 || len(doc.Subjects) != 2 {
		t.Errorf("document = %+v, want one family and two subjects", doc)
	}
	if len(doc.Families[0].Children) != 1 {
		t.Errorf("family children = %+v, want the one membership", doc.Families[0].Children)
	}
}

// TestHandle_ignoresThePayload pins that this job needs no payload: there is one
// genealogy, so a job about it names nothing, and a payload left over from
// anywhere must not make it fail.
func TestHandle_ignoresThePayload(t *testing.T) {
	t.Parallel()

	writer := &fakeWriter{}
	svc := New(Config{Families: &fakeFamilies{export: sampleExport()}, Writer: writer})

	job := jobs.Job{Type: jobs.TypeFamilyExport, Payload: []byte(`{"photo_uid":"pht1","nonsense":true}`)}
	if err := svc.Handle(t.Context(), job); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(writer.docs) != 1 {
		t.Errorf("wrote %d document(s), want 1", len(writer.docs))
	}
}

// TestExport_writesAnEmptyGenealogy verifies a library whose last relation was
// removed is written as an empty document rather than skipped. Skipping would
// leave yesterday's tree on disk, and a rebuild would restore relations the user
// deleted.
func TestExport_writesAnEmptyGenealogy(t *testing.T) {
	t.Parallel()

	writer := &fakeWriter{}
	if err := New(Config{Families: &fakeFamilies{}, Writer: writer}).Export(t.Context()); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if len(writer.docs) != 1 {
		t.Fatalf("wrote %d document(s), want 1", len(writer.docs))
	}
	if len(writer.docs[0].Families) != 0 {
		t.Errorf("document = %+v, want no families", writer.docs[0])
	}
}

// TestExport_reportsAFailedRead verifies a database that cannot be read fails the
// job — so it is retried — rather than writing an empty file over a good one.
func TestExport_reportsAFailedRead(t *testing.T) {
	t.Parallel()

	broken := errors.New("connection refused")
	writer := &fakeWriter{}

	err := New(Config{Families: &fakeFamilies{err: broken}, Writer: writer}).Export(t.Context())
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
	err := New(Config{
		Families: &fakeFamilies{export: sampleExport()},
		Writer:   &fakeWriter{err: refused},
	}).Export(t.Context())
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
		{name: "no families", cfg: Config{Writer: &fakeWriter{}}},
		{name: "no writer", cfg: Config{Families: &fakeFamilies{}}},
		{name: "neither"},
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
