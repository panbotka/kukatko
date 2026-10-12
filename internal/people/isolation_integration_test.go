//go:build integration

package people_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// These are the store-level tenant-isolation tests of people and markers: with
// one photo, subject and face marker seeded in each of two projects, every read
// answered inside one project finds that project's rows only, and every write
// aimed at the other project's rows is answered like a write at rows that do not
// exist and changes nothing.

// TestPeopleReads_scopedByProject runs the list and lookup reads for each
// project and asserts they return the project's own rows and never the other's.
func TestPeopleReads_scopedByProject(t *testing.T) {
	store, _, _, db := newStores(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	t.Run("subjects list", func(t *testing.T) {
		f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
			list, err := store.ListSubjects(ctx, projectUID)
			uids := make([]string, 0, len(list))
			for i := range list {
				uids = append(uids, list[i].UID)
			}
			return uids, err
		})
	})
	t.Run("search", func(t *testing.T) {
		for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
			own, other := f.Tenant(side), f.Tenant(side.Other())
			found, err := store.SearchSubjects(ctx, own.ProjectUID, other.Token, 10)
			if err != nil {
				t.Fatalf("SearchSubjects: %v", err)
			}
			if len(found) != 0 {
				t.Errorf("searching %s for %s's name found %v", side, side.Other(), found)
			}
		}
	})
	t.Run("markers by photo", func(t *testing.T) {
		own, err := store.ListMarkersByPhoto(ctx, f.A.ProjectUID, f.A.PhotoUID)
		if err != nil || len(own) != 1 || own[0].UID != f.A.MarkerUID {
			t.Fatalf("A's own markers = %v, %v; want [%s]", own, err, f.A.MarkerUID)
		}
		foreign, err := store.ListMarkersByPhoto(ctx, f.B.ProjectUID, f.A.PhotoUID)
		if err != nil {
			t.Fatalf("ListMarkersByPhoto: %v", err)
		}
		if len(foreign) != 0 {
			t.Errorf("A's photo read inside B listed %v, want none", foreign)
		}
	})
	t.Run("single-row lookups", func(t *testing.T) {
		if _, err := store.GetSubjectByUID(ctx, f.B.ProjectUID, f.A.SubjectUID); !errors.Is(err, people.ErrSubjectNotFound) {
			t.Errorf("GetSubjectByUID(A's subject) inside B = %v, want ErrSubjectNotFound", err)
		}
		if _, err := store.GetMarkerByUID(ctx, f.B.ProjectUID, f.A.MarkerUID); !errors.Is(err, people.ErrMarkerNotFound) {
			t.Errorf("GetMarkerByUID(A's marker) inside B = %v, want ErrMarkerNotFound", err)
		}
		if _, err := store.SubjectAvatar(ctx, f.B.ProjectUID, f.A.SubjectUID); !errors.Is(err, people.ErrSubjectNotFound) {
			t.Errorf("SubjectAvatar(A's subject) inside B = %v, want ErrSubjectNotFound", err)
		}
		covers, err := store.SubjectCovers(ctx, f.B.ProjectUID, []string{f.A.SubjectUID})
		if err != nil || len(covers) != 0 {
			t.Errorf("SubjectCovers(A's subject) inside B = %v, %v; want none", covers, err)
		}
		gallery, err := store.ListPhotoUIDsBySubject(ctx, f.B.ProjectUID, f.A.SubjectUID)
		if err != nil || len(gallery) != 0 {
			t.Errorf("A's subject's gallery inside B = %v, %v; want none", gallery, err)
		}
	})
	t.Run("no project", func(t *testing.T) {
		if _, err := store.ListSubjects(ctx, ""); !errors.Is(err, people.ErrNoProject) {
			t.Errorf("ListSubjects without a project = %v, want ErrNoProject", err)
		}
		if _, err := store.GetSubjectByUID(ctx, "", f.A.SubjectUID); !errors.Is(err, people.ErrNoProject) {
			t.Errorf("GetSubjectByUID without a project = %v, want ErrNoProject", err)
		}
	})
}

// TestSubjectSlug_uniquePerProject creates a subject with the same name in both
// projects: each keeps the plain slug, because uniqueness is per project.
func TestSubjectSlug_uniquePerProject(t *testing.T) {
	store, _, _, db := newStores(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	a, err := store.CreateSubject(ctx, f.A.ProjectUID, people.Subject{Name: "Jan Novák"})
	if err != nil {
		t.Fatalf("creating in A: %v", err)
	}
	b, err := store.CreateSubject(ctx, f.B.ProjectUID, people.Subject{Name: "Jan Novák"})
	if err != nil {
		t.Fatalf("creating in B: %v", err)
	}
	if a.Slug != "jan-novak" || b.Slug != "jan-novak" {
		t.Errorf("slugs = %q / %q, want jan-novak in both projects", a.Slug, b.Slug)
	}
	got, err := store.GetSubjectBySlug(ctx, f.B.ProjectUID, "jan-novak")
	if err != nil || got.UID != b.UID {
		t.Errorf("GetSubjectBySlug inside B = %s, %v; want B's %s", got.UID, err, b.UID)
	}
}

// TestPeopleWrites_foreignRowsAreMissingAndUntouched aims every marker and
// subject write at A's rows from inside B and asserts each is refused as a
// missing row, then that A's marker and subject are exactly as seeded.
func TestPeopleWrites_foreignRowsAreMissingAndUntouched(t *testing.T) {
	store, _, _, db := newStores(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	inB := f.B.ProjectUID
	entry := audit.Entry{ActorUID: f.B.UserUID, Action: audit.ActionFaceAssign, TargetType: "markers"}

	writes := []struct {
		name string
		want error
		run  func() error
	}{
		{"create marker on A's photo", people.ErrPhotoNotFound, func() error {
			_, err := store.CreateMarker(ctx, inB, people.Marker{PhotoUID: f.A.PhotoUID, W: 0.1, H: 0.1})
			return err
		}},
		{"create audited marker on A's photo", people.ErrPhotoNotFound, func() error {
			_, err := store.CreateMarkerAudited(ctx, inB,
				people.Marker{PhotoUID: f.A.PhotoUID, SubjectUID: &f.B.SubjectUID, W: 0.1, H: 0.1}, entry)
			return err
		}},
		{"assign A's marker to B's subject", people.ErrMarkerNotFound, func() error {
			_, err := store.AssignSubjectAudited(ctx, inB, f.A.MarkerUID, f.B.SubjectUID, entry)
			return err
		}},
		{"assign B's marker to A's subject", people.ErrSubjectNotFound, func() error {
			_, err := store.AssignSubject(ctx, inB, f.B.MarkerUID, f.A.SubjectUID)
			return err
		}},
		{"unassign A's marker", people.ErrMarkerNotFound, func() error {
			_, err := store.UnassignSubjectAudited(ctx, inB, f.A.MarkerUID, entry)
			return err
		}},
		{"invalidate A's marker", people.ErrMarkerNotFound, func() error {
			_, err := store.SetMarkerInvalidAudited(ctx, inB, f.A.MarkerUID, true, entry)
			return err
		}},
		{"review A's marker", people.ErrMarkerNotFound, func() error {
			_, err := store.SetMarkerReviewed(ctx, inB, f.A.MarkerUID, true)
			return err
		}},
		{"delete A's marker", people.ErrMarkerNotFound, func() error {
			return store.DeleteMarker(ctx, inB, f.A.MarkerUID)
		}},
		{"attach B's subject to A's photo", people.ErrPhotoNotFound, func() error {
			_, err := store.AttachSubjectToPhoto(ctx, inB, f.A.PhotoUID, f.B.SubjectUID, entry)
			return err
		}},
		{"rename A's subject", people.ErrSubjectNotFound, func() error {
			_, err := store.UpdateSubjectAudited(ctx, inB, f.A.SubjectUID, people.SubjectUpdate{Name: "Přejmenován"}, entry)
			return err
		}},
		{"merge A's subject into B's", people.ErrSubjectNotFound, func() error {
			_, err := store.MergeSubjectsAudited(ctx, inB, f.A.SubjectUID, f.B.SubjectUID, entry)
			return err
		}},
		{"delete A's subject", people.ErrSubjectNotFound, func() error {
			return store.DeleteSubjectAudited(ctx, inB, f.A.SubjectUID, entry)
		}},
	}
	for _, w := range writes {
		if err := w.run(); !errors.Is(err, w.want) {
			t.Errorf("%s inside B = %v, want %v", w.name, err, w.want)
		}
	}

	marker, err := store.GetMarkerByUID(ctx, f.A.ProjectUID, f.A.MarkerUID)
	if err != nil {
		t.Fatalf("A's marker is gone: %v", err)
	}
	if marker.SubjectUID == nil || *marker.SubjectUID != f.A.SubjectUID || marker.Invalid || marker.Reviewed {
		t.Errorf("A's marker changed: %+v", marker)
	}
	subject, err := store.GetSubjectByUID(ctx, f.A.ProjectUID, f.A.SubjectUID)
	if err != nil {
		t.Fatalf("A's subject is gone: %v", err)
	}
	if subject.Name != "Osoba "+f.A.Token {
		t.Errorf("A's subject was renamed to %q", subject.Name)
	}
	onPhoto, err := store.ListMarkersByPhoto(ctx, f.A.ProjectUID, f.A.PhotoUID)
	if err != nil || len(onPhoto) != 1 {
		t.Errorf("A's photo carries %v (%v), want only its seeded marker", onPhoto, err)
	}
	var audited int
	if err := db.Pool().QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE actor_uid = $1", f.B.UserUID).
		Scan(&audited); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if audited != 0 {
		t.Errorf("the refused writes left %d audit rows", audited)
	}
}

// TestTagObserver_hearsTheProject asserts the tag observer is told which project
// a tagging happened in.
func TestTagObserver_hearsTheProject(t *testing.T) {
	plain, _, _, db := newStores(t)
	f := projecttest.Seed(t, db.Pool())
	observer := &recordingObserver{}
	store := plain.WithTagObserver(observer)

	subject, err := store.CreateSubject(t.Context(), f.B.ProjectUID, people.Subject{Name: "Druhý"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	if _, err := store.AssignSubject(t.Context(), f.B.ProjectUID, f.B.MarkerUID, subject.UID); err != nil {
		t.Fatalf("AssignSubject: %v", err)
	}
	events := observer.take()
	if len(events) != 2 {
		t.Fatalf("observer heard %+v, want the untagging of the seeded subject and the new tagging", events)
	}
	for _, e := range events {
		if e.tag.ProjectUID != f.B.ProjectUID {
			t.Errorf("%s heard in project %q, want %s", e.kind, e.tag.ProjectUID, f.B.ProjectUID)
		}
	}
}
