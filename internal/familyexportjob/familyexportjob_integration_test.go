//go:build integration

package familyexportjob_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyexport"
	"github.com/panbotka/kukatko/internal/familyexportjob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
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
	pool   *pgxpool.Pool
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
	fsStore, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS returned error: %v", err)
	}
	familyStore := family.NewStore(db.Pool())
	fixture := &treeFixture{
		svc: familyexportjob.New(familyexportjob.Config{
			Families: familyStore,
			Projects: project.NewStore(db.Pool()),
			Writer:   familyexport.NewWriter(fsStore),
		}),
		family: familyStore,
		people: people.NewStore(db.Pool()),
		pool:   db.Pool(),
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
		Role:         auth.RoleUser,
	}.InProject(project.BootstrapUID, project.RoleViewer)); err != nil {
		t.Fatalf("creating the acting user: %v", err)
	}
	return uid
}

// subject inserts a person and returns their uid.
func (f *treeFixture) subject(t *testing.T, name string, birth *int) string {
	t.Helper()
	subj, err := f.people.CreateSubject(context.Background(), project.BootstrapUID, people.Subject{Name: name, BirthYear: birth})
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
	if _, err := f.family.AddParentAudited(
		context.Background(), project.BootstrapUID, childUID, parentUID, "", entry,
	); err != nil {
		t.Fatalf("adding parent %s of %s: %v", parentUID, childUID, err)
	}
}

// read parses the bootstrap project's export from the storage root, failing
// when there is none.
func (f *treeFixture) read(t *testing.T) familyexport.Document {
	t.Helper()
	return f.readKey(t, familyexport.Key)
}

// readKey parses the export stored at key, failing when there is none.
func (f *treeFixture) readKey(t *testing.T, key string) familyexport.Document {
	t.Helper()
	doc, err := familyexport.Unmarshal(f.bytesAt(t, key))
	if err != nil {
		t.Fatalf("parsing %s: %v", key, err)
	}
	return doc
}

// bytesAt returns the raw bytes stored at key, failing when there are none.
func (f *treeFixture) bytesAt(t *testing.T, key string) []byte {
	t.Helper()
	data, err := os.ReadFile(f.path(key))
	if err != nil {
		t.Fatalf("reading %s: %v", key, err)
	}
	return data
}

// path is where key lives on the filesystem backend.
func (f *treeFixture) path(key string) string {
	return filepath.Join(f.root, filepath.FromSlash(key))
}

// export writes the bootstrap project's file, failing the test on an error.
func (f *treeFixture) export(t *testing.T) {
	t.Helper()
	key, err := f.svc.Export(t.Context(), project.BootstrapUID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if key != familyexport.Key {
		t.Errorf("the bootstrap project's export landed at %q, want the root %q", key, familyexport.Key)
	}
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

	fixture.export(t)
	doc := fixture.read(t)

	if doc.Version != familyexport.Version {
		t.Errorf("version = %d, want %d", doc.Version, familyexport.Version)
	}
	if err := familyexport.CheckProject(doc, project.BootstrapUID); err != nil {
		t.Errorf("the file does not prove it is the bootstrap project's: %v", err)
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

	fixture.export(t)
	first := fixture.read(t)
	fixture.export(t)
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
	if err := fixture.family.RemoveRelationAudited(t.Context(), project.BootstrapUID, child, parent, entry); err != nil {
		t.Fatalf("removing the relation: %v", err)
	}
	fixture.export(t)
	after := fixture.read(t)
	if len(after.Families) != 0 || len(after.Subjects) != 0 {
		t.Errorf("export after the removal = %+v, want an empty genealogy", after)
	}
}

// TestExport_oneFilePerProject seeds a family in each of two projects and runs
// the queue's jobs: each project's file lands at its own key with its own
// project block and only its own people, and a job for A leaves B's file alone.
func TestExport_oneFilePerProject(t *testing.T) {
	fixture := newTreeFixture(t)
	fx := projecttest.Seed(t, fixture.pool)
	for _, tn := range []*projecttest.Tenant{&fx.A, &fx.B} {
		parent, err := fixture.people.CreateSubject(t.Context(), tn.ProjectUID, people.Subject{Name: "Rodič " + tn.Token})
		if err != nil {
			t.Fatalf("creating the parent in %s: %v", tn.Slug, err)
		}
		entry := audit.Entry{ActorUID: tn.UserUID, Action: audit.ActionSubjectRelationAdd, TargetType: "subjects"}
		if _, err := fixture.family.AddParentAudited(
			t.Context(), tn.ProjectUID, tn.SubjectUID, parent.UID, "", entry,
		); err != nil {
			t.Fatalf("recording the parent in %s: %v", tn.Slug, err)
		}
	}
	keyB := fx.B.StoragePrefix + familyexport.Key
	if fx.B.StoragePrefix == "" || keyB == familyexport.Key {
		t.Fatalf("B's storage prefix = %q, want a prefix of its own", fx.B.StoragePrefix)
	}

	fixture.handle(t, fx.A.ProjectUID)
	if _, err := os.Stat(fixture.path(keyB)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a job for A wrote B's file too (stat = %v)", err)
	}
	fixture.handle(t, fx.B.ProjectUID)

	for _, tc := range []struct {
		tn   *projecttest.Tenant
		key  string
		peer *projecttest.Tenant
	}{
		{tn: &fx.A, key: familyexport.Key, peer: &fx.B},
		{tn: &fx.B, key: keyB, peer: &fx.A},
	} {
		doc := fixture.readKey(t, tc.key)
		if doc.Project != (familyexport.Project{UID: tc.tn.ProjectUID, Slug: tc.tn.Slug}) {
			t.Errorf("%s: project block = %+v, want %s/%s", tc.key, doc.Project, tc.tn.ProjectUID, tc.tn.Slug)
		}
		if err := familyexport.CheckProject(doc, tc.peer.ProjectUID); !errors.Is(err, familyexport.ErrProjectMismatch) {
			t.Errorf("%s read into the other project = %v, want ErrProjectMismatch", tc.key, err)
		}
		if len(doc.Families) != 1 || len(doc.Subjects) != 2 {
			t.Errorf("%s = %+v, want the project's one family and two people", tc.key, doc)
		}
		for _, subj := range doc.Subjects {
			if subj.UID == tc.peer.SubjectUID {
				t.Errorf("%s describes the other project's subject %s", tc.key, subj.UID)
			}
		}
	}

	before := fixture.bytesAt(t, keyB)
	extra := fixture.subject(t, "Další "+fx.A.Token, nil)
	fixture.addParent(t, extra, fx.A.SubjectUID)
	fixture.handle(t, fx.A.ProjectUID)
	if got := fixture.readKey(t, familyexport.Key); len(got.Families) != 2 {
		t.Errorf("A's file after A's change = %d families, want 2", len(got.Families))
	}
	if !bytes.Equal(fixture.bytesAt(t, keyB), before) {
		t.Error("a job for A rewrote B's file")
	}
}

// handle runs one family_export job for the project, the way the worker would.
func (f *treeFixture) handle(t *testing.T, projectUID string) {
	t.Helper()
	payload, err := json.Marshal(jobs.FamilyExportPayload{ProjectUID: projectUID})
	if err != nil {
		t.Fatalf("encoding the payload: %v", err)
	}
	if err := f.svc.Handle(t.Context(), jobs.Job{Type: jobs.TypeFamilyExport, Payload: payload}); err != nil {
		t.Fatalf("Handle(%s): %v", projectUID, err)
	}
}
