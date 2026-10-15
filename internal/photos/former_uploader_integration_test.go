//go:build integration

package photos_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/query"
)

// TestUploaderBuckets_formerMemberIsNameless credits B's photo to A's member —
// an account that never belonged to B (an upload the public link credited
// before it checked membership) — and A's photo to the same account: B's
// uploader facet keeps the bucket and its uid but neither of the account's
// names, and flags it former, while A, where the account is a member, names
// it. Removing B's own member from B makes them a former member the same way.
func TestUploaderBuckets_formerMemberIsNameless(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	credit := func(photoUID, userUID string) {
		t.Helper()
		if _, err := db.Pool().Exec(ctx, `UPDATE photos SET uploaded_by = $2 WHERE uid = $1`, photoUID, userUID); err != nil {
			t.Fatalf("crediting %s: %v", photoUID, err)
		}
	}
	credit(f.A.PhotoUID, f.A.UserUID)
	credit(f.B.PhotoUID, f.A.UserUID)

	only := func(projectUID string) photos.UploaderBucket {
		t.Helper()
		buckets, err := store.UploaderBuckets(ctx, projectUID, photos.ListParams{})
		if err != nil || len(buckets) != 1 {
			t.Fatalf("UploaderBuckets(%s) = %+v, %v; want one bucket", projectUID, buckets, err)
		}
		return buckets[0]
	}
	if got := only(f.A.ProjectUID); got.Former || got.Username != f.A.Username || got.UID != f.A.UserUID {
		t.Errorf("A's own member in A = %+v, want named", got)
	}
	if got := only(f.B.ProjectUID); !got.Former || got.Username != "" || got.DisplayName != "" ||
		got.UID != f.A.UserUID || got.Count != 1 {
		t.Errorf("A's member in B = %+v, want a nameless former bucket keeping the uid", got)
	}

	credit(f.B.PhotoUID, f.B.UserUID)
	if got := only(f.B.ProjectUID); got.Former || got.Username != f.B.Username {
		t.Errorf("B's member in B = %+v, want named", got)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM project_members WHERE project_uid = $1 AND user_uid = $2`,
		f.B.ProjectUID, f.B.UserUID); err != nil {
		t.Fatalf("removing B's member: %v", err)
	}
	if got := only(f.B.ProjectUID); !got.Former || got.Username != "" || got.DisplayName != "" {
		t.Errorf("B's removed member in B = %+v, want a nameless former bucket", got)
	}
}

// TestUploaderFilter_formerMemberMatchesByUIDOnly verifies `uploader:` in a
// project matches a former member's photos by the uid the facet hands out, but
// not by the account's live name, which the project no longer reads.
func TestUploaderFilter_formerMemberMatchesByUIDOnly(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	if _, err := db.Pool().Exec(ctx, `UPDATE photos SET uploaded_by = $2 WHERE uid = ANY($1)`,
		[]string{f.A.PhotoUID, f.B.PhotoUID}, f.A.UserUID); err != nil {
		t.Fatalf("crediting: %v", err)
	}
	search := func(projectUID, input string) []string {
		t.Helper()
		list, err := store.List(ctx, projectUID, photos.ListParams{QueryFilters: query.Parse(input).Filters})
		if err != nil {
			t.Fatalf("List(%s, %q): %v", projectUID, input, err)
		}
		return photoUIDs(list)
	}
	if got := search(f.A.ProjectUID, "uploader:"+f.A.Username); len(got) != 1 {
		t.Errorf("A by its member's name = %v, want A's photo", got)
	}
	if got := search(f.B.ProjectUID, "uploader:"+f.A.Username); len(got) != 0 {
		t.Errorf("B by a non-member's name = %v, want nothing", got)
	}
	if got := search(f.B.ProjectUID, "uploader:"+f.A.UserUID); len(got) != 1 || got[0] != f.B.PhotoUID {
		t.Errorf("B by a non-member's uid = %v, want B's photo", got)
	}
}
