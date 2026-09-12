//go:build integration

package familyexportjob_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyexport"
	"github.com/panbotka/kukatko/internal/familyexportjob"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/storage"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They exercise the export end to end — real
// stores, real relations, a real filesystem backend — because that is the only
// way to answer the question the file exists for: with the database gone, is the
// tree still on the disk?
//
// They share one database and truncate between cases, so they do not run in
// parallel.

// treeFixture is one case's world: the stores, the storage root and the service.
type treeFixture struct {
	svc    *familyexportjob.Service
	family *family.Store
	people *people.Store
	actor  string
	root   string
}

// newTreeFixture builds a Service over a freshly truncated database and a real
// filesystem storage backend rooted in a temp dir.
func newTreeFixture(t *testing.T) *treeFixture {
	t.Helper()

	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	root := t.TempDir()
	fs, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS returned error: %v", err)
	}
	familyStore := family.NewStore(db.Pool())
	fixture := &treeFixture{
		svc: familyexportjob.New(familyexportjob.Config{
			Families: familyStore,
			Writer:   familyexport.NewWriter(fs),
		}),
		family: familyStore,
		people: people.NewStore(db.Pool()),
		root:   root,
	}
	fixture.actor = fixture.makeUser(t, db)
	return fixture
}

// makeUser inserts the account the audited relation writes point at
// (audit_log.actor_uid is a foreign key to users) and returns its uid.
func (f *treeFixture) makeUser(t *testing.T, db *database.DB) string {
	t.Helper()
	const uid = "usfamexport00000000000001"
	if err := auth.NewStore(db.Pool()).CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     "genealogist",
		Email:        "genealogist@example.test",
		PasswordHash: "x",
		Role:         auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating the acting user: %v", err)
	}
	return uid
}

// subject inserts a person and returns their uid.
func (f *treeFixture) subject(t *testing.T, name string, birth *int) string {
	t.Helper()
	subj, err := f.people.CreateSubject(context.Background(), people.Subject{Name: name, BirthYear: birth})
	if err != nil {
		t.Fatalf("creating subject %s: %v", name, err)
	}
	return subj.UID
}

// addParent records parentUID as a parent of childUID.
func (f *treeFixture) addParent(t *testing.T, childUID, parentUID string) {
	t.Helper()
	entry := audit.Entry{
		ActorUID: f.actor, Action: audit.ActionSubjectRelationAdd, TargetType: "subjects",
	}
	if _, err := f.family.AddParentAudited(context.Background(), childUID, parentUID, "", entry); err != nil {
		t.Fatalf("adding parent %s of %s: %v", parentUID, childUID, err)
	}
}

// read parses the export from the storage root, failing when there is none.
func (f *treeFixture) read(t *testing.T) familyexport.Document {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(familyexport.Key)))
	if err != nil {
		t.Fatalf("reading %s: %v", familyexport.Key, err)
	}
	doc, err := familyexport.Unmarshal(data)
	if err != nil {
		t.Fatalf("parsing %s: %v", familyexport.Key, err)
	}
	return doc
}

// TestExport_theTreeSurvivesTheDatabase is the whole point of the file: record a
// genealogy through the ordinary write path, then read the storage as if the
// database were gone and rebuild who is whose parent from it alone.
func TestExport_theTreeSurvivesTheDatabase(t *testing.T) {
	fixture := newTreeFixture(t)
	bohumil := fixture.subject(t, "Bohumil Nečas st.", new(1901))
	marie := fixture.subject(t, "Marie Nečasová", nil)
	// A great-grandchild nobody ever photographed is an ordinary node here, and the
	// reason a photo's sidecar could never hold this: they appear on no photo.
	jan := fixture.subject(t, "Jan Nečas", nil)
	fixture.addParent(t, jan, bohumil)
	fixture.addParent(t, jan, marie)

	if err := fixture.svc.Export(t.Context()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	doc := fixture.read(t)

	if doc.Version != familyexport.Version {
		t.Errorf("version = %d, want %d", doc.Version, familyexport.Version)
	}
	if len(doc.Families) != 1 {
		t.Fatalf("families = %d, want 1: %+v", len(doc.Families), doc.Families)
	}
	fam := doc.Families[0]
	if len(fam.Partners) != 2 {
		t.Errorf("partners = %v, want both parents", fam.Partners)
	}
	if len(fam.Children) != 1 || fam.Children[0].SubjectUID != jan {
		t.Errorf("children = %+v, want Jan alone", fam.Children)
	}

	names := make(map[string]string, len(doc.Subjects))
	for _, subj := range doc.Subjects {
		names[subj.UID] = subj.Name
	}
	if len(names) != 3 {
		t.Errorf("subjects = %d, want the 3 in the family", len(names))
	}
	for _, uid := range append(append([]string{}, fam.Partners...), jan) {
		if names[uid] == "" {
			t.Errorf("the file names %s in a family but describes no such subject", uid)
		}
	}
	if names[bohumil] != "Bohumil Nečas st." {
		t.Errorf("Bohumil's name = %q, want it verbatim", names[bohumil])
	}
}

// TestExport_isIdempotentAndFollowsARemoval verifies re-running writes the same
// bytes, and that removing the relation is reflected in the file rather than
// leaving yesterday's tree standing.
func TestExport_isIdempotentAndFollowsARemoval(t *testing.T) {
	fixture := newTreeFixture(t)
	parent := fixture.subject(t, "Marie Nečasová", nil)
	child := fixture.subject(t, "Jan Nečas", nil)
	fixture.addParent(t, child, parent)

	if err := fixture.svc.Export(t.Context()); err != nil {
		t.Fatalf("first Export: %v", err)
	}
	first := fixture.read(t)
	if err := fixture.svc.Export(t.Context()); err != nil {
		t.Fatalf("second Export: %v", err)
	}
	second := fixture.read(t)
	// Everything but the generation stamp is identical: the handler is idempotent,
	// so a coalesced or repeated job costs nothing and changes nothing.
	if !reflect.DeepEqual(first.Families, second.Families) {
		t.Errorf("re-run families = %+v, want %+v", second.Families, first.Families)
	}
	if !reflect.DeepEqual(first.Subjects, second.Subjects) {
		t.Errorf("re-run subjects = %+v, want %+v", second.Subjects, first.Subjects)
	}

	entry := audit.Entry{
		ActorUID: fixture.actor, Action: audit.ActionSubjectRelationRemove, TargetType: "subjects",
	}
	if err := fixture.family.RemoveRelationAudited(t.Context(), child, parent, entry); err != nil {
		t.Fatalf("removing the relation: %v", err)
	}
	if err := fixture.svc.Export(t.Context()); err != nil {
		t.Fatalf("Export after the removal: %v", err)
	}
	after := fixture.read(t)
	if len(after.Families) != 0 || len(after.Subjects) != 0 {
		t.Errorf("export after the removal = %+v, want an empty genealogy", after)
	}
}
