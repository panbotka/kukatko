//go:build integration

package family_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// isolationWorld is two projects side by side, each with the same small family:
// the seeded subject (the child) and a parent recorded through the store.
type isolationWorld struct {
	fx     *projecttest.Fixture
	family *family.Store
	people *people.Store
	// parentA and parentB are the parent created in each project.
	parentA, parentB string
	// famA is the family A's relation landed in.
	famA family.Family
}

// newIsolationWorld seeds the two tenants and records "parent of the seeded
// subject" in each of them through the ordinary write path.
func newIsolationWorld(t *testing.T) *isolationWorld {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	w := &isolationWorld{
		fx:     projecttest.Seed(t, db.Pool()),
		family: family.NewStore(db.Pool()),
		people: people.NewStore(db.Pool()),
	}
	w.parentA, w.famA = w.addParent(t, &w.fx.A, "Bohumil")
	w.parentB, _ = w.addParent(t, &w.fx.B, "Bohuslav")
	return w
}

// addParent creates a parent in the tenant's project and records them as the
// parent of the tenant's seeded subject, returning the parent and the family.
func (w *isolationWorld) addParent(t *testing.T, tn *projecttest.Tenant, name string) (string, family.Family) {
	t.Helper()
	ctx := context.Background()
	parent, err := w.people.CreateSubject(ctx, tn.ProjectUID, people.Subject{Name: name + " " + tn.Token})
	if err != nil {
		t.Fatalf("creating parent in %s: %v", tn.Slug, err)
	}
	fam, err := w.family.AddParentAudited(ctx, tn.ProjectUID, tn.SubjectUID, parent.UID, "", w.entry(tn))
	if err != nil {
		t.Fatalf("recording the parent in %s: %v", tn.Slug, err)
	}
	return parent.UID, fam
}

// entry is an audit entry acted by the tenant's member.
func (w *isolationWorld) entry(tn *projecttest.Tenant) audit.Entry {
	return audit.Entry{ActorUID: tn.UserUID, Action: audit.ActionSubjectRelationAdd, TargetType: "subjects"}
}

// TestIsolation_readsStayInTheirProject asserts the relations, the tree and the
// export of A never name anybody of B and the other way round, and that a
// subject or family of the other project is answered like a missing one.
func TestIsolation_readsStayInTheirProject(t *testing.T) {
	w := newIsolationWorld(t)
	ctx := context.Background()

	w.fx.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		tn := w.tenant(projectUID)
		tree, err := w.family.Tree(ctx, projectUID, tn.SubjectUID)
		if err != nil {
			return nil, err
		}
		uids := []string{}
		for _, m := range tree.Members {
			uids = append(uids, m.UID)
		}
		for _, f := range tree.Families {
			uids = append(uids, f.UID)
		}
		return uids, nil
	})
	w.fx.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		rel, err := w.family.Relations(ctx, projectUID, w.tenant(projectUID).SubjectUID)
		if err != nil {
			return nil, err
		}
		uids := []string{}
		for _, p := range rel.Parents {
			uids = append(uids, p.UID, p.FamilyUID)
		}
		return uids, nil
	})
	w.fx.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		exp, err := w.family.Export(ctx, projectUID)
		if err != nil {
			return nil, err
		}
		uids := []string{}
		for _, f := range exp.Families {
			uids = append(uids, f.UID)
		}
		for _, c := range exp.Children {
			uids = append(uids, c.FamilyUID, c.ChildUID)
		}
		for _, s := range exp.Subjects {
			uids = append(uids, s.UID)
		}
		return uids, nil
	})

	b := w.fx.B.ProjectUID
	if _, err := w.family.Relations(ctx, b, w.fx.A.SubjectUID); !errors.Is(err, family.ErrSubjectNotFound) {
		t.Errorf("Relations of A's subject inside B = %v, want ErrSubjectNotFound", err)
	}
	if _, err := w.family.Tree(ctx, b, w.fx.A.SubjectUID); !errors.Is(err, family.ErrSubjectNotFound) {
		t.Errorf("Tree of A's subject inside B = %v, want ErrSubjectNotFound", err)
	}
	if _, err := w.family.GetFamily(ctx, b, w.famA.UID); !errors.Is(err, family.ErrFamilyNotFound) {
		t.Errorf("GetFamily of A's family inside B = %v, want ErrFamilyNotFound", err)
	}
}

// TestIsolation_writesRefuseTheOtherProject asserts no write ties two projects
// together and none reaches into the other project's rows: each answers the
// way a missing subject, family or relation is answered, and changes nothing.
func TestIsolation_writesRefuseTheOtherProject(t *testing.T) {
	w := newIsolationWorld(t)
	ctx := context.Background()
	a, b := &w.fx.A, &w.fx.B

	if _, err := w.family.AddParentAudited(ctx, b.ProjectUID, b.SubjectUID, a.SubjectUID, "", w.entry(b)); !errors.Is(
		err, family.ErrSubjectNotFound) {
		t.Errorf("AddParent naming A's subject from B = %v, want ErrSubjectNotFound", err)
	}
	if _, err := w.family.AddChildAudited(ctx, b.ProjectUID, a.SubjectUID, b.SubjectUID, "", w.entry(b)); !errors.Is(
		err, family.ErrSubjectNotFound) {
		t.Errorf("AddChild on A's subject from B = %v, want ErrSubjectNotFound", err)
	}
	if _, err := w.family.AddPartnerAudited(ctx, b.ProjectUID, w.parentB, w.parentA, w.entry(b)); !errors.Is(
		err, family.ErrSubjectNotFound) {
		t.Errorf("AddPartner with A's parent from B = %v, want ErrSubjectNotFound", err)
	}
	for _, role := range []family.Role{family.RoleParent, family.RoleChild, family.RolePartner, family.RoleSibling} {
		_, err := w.family.AddRelationAudited(ctx, b.ProjectUID, b.SubjectUID,
			family.AddRelation{Role: role, SubjectUID: a.SubjectUID}, w.entry(b))
		if !errors.Is(err, family.ErrSubjectNotFound) {
			t.Errorf("AddRelation(%s) naming A's subject from B = %v, want ErrSubjectNotFound", role, err)
		}
	}
	if _, err := w.family.UpdateFamilyAudited(ctx, b.ProjectUID, w.famA.UID,
		family.Update{Kind: family.KindMarriage}, w.entry(b)); !errors.Is(err, family.ErrFamilyNotFound) {
		t.Errorf("UpdateFamily of A's family from B = %v, want ErrFamilyNotFound", err)
	}
	if err := w.family.RemoveRelationAudited(ctx, b.ProjectUID, a.SubjectUID, w.parentA, w.entry(b)); !errors.Is(
		err, family.ErrRelationNotFound) {
		t.Errorf("RemoveRelation of A's pair from B = %v, want ErrRelationNotFound", err)
	}

	rel, err := w.family.Relations(ctx, a.ProjectUID, a.SubjectUID)
	if err != nil {
		t.Fatalf("Relations in A: %v", err)
	}
	if len(rel.Parents) != 1 || rel.Parents[0].UID != w.parentA {
		t.Errorf("A's parents after B's attempts = %+v, want only %s", rel.Parents, w.parentA)
	}
	if fam, err := w.family.GetFamily(ctx, a.ProjectUID, w.famA.UID); err != nil || fam.Kind == family.KindMarriage {
		t.Errorf("A's family after B's edit = %+v, %v; want it untouched", fam, err)
	}
}

// TestIsolation_newFamiliesAndSubjectsLandInTheProject asserts what a write
// creates belongs to the request's project: a sibling group (a family with no
// partner to take the project from) and a subject created inline.
func TestIsolation_newFamiliesAndSubjectsLandInTheProject(t *testing.T) {
	w := newIsolationWorld(t)
	ctx := context.Background()
	b := &w.fx.B

	res, err := w.family.AddRelationAudited(ctx, b.ProjectUID, w.parentB, family.AddRelation{
		Role: family.RoleSibling, New: &family.NewPerson{Name: "Sourozenec " + b.Token},
	}, w.entry(b))
	if err != nil {
		t.Fatalf("AddRelation(sibling, new) in B: %v", err)
	}
	if !res.Created {
		t.Fatalf("the sibling was not created inline: %+v", res)
	}
	if _, err := w.people.GetSubjectByUID(ctx, b.ProjectUID, res.Relative.UID); err != nil {
		t.Errorf("the inline subject is not in B: %v", err)
	}
	if _, err := w.family.GetFamily(ctx, b.ProjectUID, res.Family.UID); err != nil {
		t.Errorf("the new sibling group is not in B: %v", err)
	}
	if _, err := w.family.GetFamily(ctx, w.fx.A.ProjectUID, res.Family.UID); !errors.Is(err, family.ErrFamilyNotFound) {
		t.Errorf("B's sibling group read inside A = %v, want ErrFamilyNotFound", err)
	}
}

// TestIsolation_noProjectIsRefused pins the convention: an empty project is a
// programming error, never "the whole instance".
func TestIsolation_noProjectIsRefused(t *testing.T) {
	w := newIsolationWorld(t)
	ctx := context.Background()

	checks := map[string]error{}
	_, checks["Relations"] = w.family.Relations(ctx, "", w.fx.A.SubjectUID)
	_, checks["Tree"] = w.family.Tree(ctx, "", w.fx.A.SubjectUID)
	_, checks["GetFamily"] = w.family.GetFamily(ctx, "", w.famA.UID)
	_, checks["Export"] = w.family.Export(ctx, "")
	_, checks["AddParent"] = w.family.AddParentAudited(ctx, "", w.fx.A.SubjectUID, w.parentA, "", w.entry(&w.fx.A))
	_, checks["AddPartner"] = w.family.AddPartnerAudited(ctx, "", w.fx.A.SubjectUID, w.parentA, w.entry(&w.fx.A))
	_, checks["AddRelation"] = w.family.AddRelationAudited(ctx, "", w.fx.A.SubjectUID,
		family.AddRelation{Role: family.RoleParent, SubjectUID: w.parentA}, w.entry(&w.fx.A))
	_, checks["UpdateFamily"] = w.family.UpdateFamilyAudited(ctx, "", w.famA.UID, family.Update{}, w.entry(&w.fx.A))
	checks["RemoveRelation"] = w.family.RemoveRelationAudited(ctx, "", w.fx.A.SubjectUID, w.parentA, w.entry(&w.fx.A))
	for name, err := range checks {
		if !errors.Is(err, family.ErrNoProject) {
			t.Errorf("%s without a project = %v, want ErrNoProject", name, err)
		}
	}
}

// tenant returns the seeded tenant of projectUID.
func (w *isolationWorld) tenant(projectUID string) *projecttest.Tenant {
	if projectUID == w.fx.B.ProjectUID {
		return &w.fx.B
	}
	return &w.fx.A
}
