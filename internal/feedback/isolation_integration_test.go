//go:build integration

package feedback_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestFaceOpinions_neverCrossProjects records face rejections and confirmations
// from inside B about A's face, about B's face and A's subject, and without a
// project: every one is refused (ErrTargetNotFound / ErrNoProject), writes no
// row and no audit entry, while the same opinion about B's own face and subject
// is recorded.
func TestFaceOpinions_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := feedback.NewStore(db.Pool())
	ctx := t.Context()
	entry := audit.Entry{Action: audit.ActionFaceReject, TargetType: "faces"}

	refused := []struct {
		name    string
		project string
		photo   string
		subject string
		want    error
	}{
		{"A's face", f.B.ProjectUID, f.A.PhotoUID, f.B.SubjectUID, feedback.ErrTargetNotFound},
		{"A's subject", f.B.ProjectUID, f.B.PhotoUID, f.A.SubjectUID, feedback.ErrTargetNotFound},
		{"no project", "", f.B.PhotoUID, f.B.SubjectUID, feedback.ErrNoProject},
	}
	for _, c := range refused {
		rej := feedback.FaceRejectionKey{PhotoUID: c.photo, SubjectUID: c.subject}
		conf := feedback.FaceConfirmationKey{PhotoUID: c.photo, SubjectUID: c.subject}
		if err := store.RejectFace(ctx, c.project, rej, entry); !errors.Is(err, c.want) {
			t.Errorf("RejectFace(%s) = %v, want %v", c.name, err, c.want)
		}
		if err := store.UnrejectFace(ctx, c.project, rej, entry); !errors.Is(err, c.want) {
			t.Errorf("UnrejectFace(%s) = %v, want %v", c.name, err, c.want)
		}
		if err := store.ConfirmFace(ctx, c.project, conf, entry); !errors.Is(err, c.want) {
			t.Errorf("ConfirmFace(%s) = %v, want %v", c.name, err, c.want)
		}
		if err := store.UnconfirmFace(ctx, c.project, conf, entry); !errors.Is(err, c.want) {
			t.Errorf("UnconfirmFace(%s) = %v, want %v", c.name, err, c.want)
		}
	}
	var rows, audited int
	if err := db.Pool().QueryRow(ctx,
		"SELECT (SELECT count(*) FROM face_rejections) + (SELECT count(*) FROM face_confirmations), "+
			"(SELECT count(*) FROM audit_log WHERE action = $1)", audit.ActionFaceReject).Scan(&rows, &audited); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 0 || audited != 0 {
		t.Errorf("refused opinions left %d rows and %d audit entries", rows, audited)
	}

	own := feedback.FaceRejectionKey{PhotoUID: f.B.PhotoUID, SubjectUID: f.B.SubjectUID}
	if err := store.RejectFace(ctx, f.B.ProjectUID, own, entry); err != nil {
		t.Fatalf("RejectFace on B's own face: %v", err)
	}
	if ok, err := store.IsFaceRejected(ctx, own); err != nil || !ok {
		t.Errorf("B's own rejection recorded = %v, %v; want true", ok, err)
	}
}

// TestLabelRejections_neverCrossProjects rejects and un-rejects labels from
// inside B about A's photo, about A's label and without a project: every one is
// refused (ErrTargetNotFound / ErrNoProject) and writes no row and no audit
// entry, while B's own rejection is recorded and stamped with B's project.
func TestLabelRejections_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := feedback.NewStore(db.Pool())
	ctx := t.Context()
	entry := audit.Entry{Action: audit.ActionLabelReject, TargetType: "labels"}

	refused := []struct {
		name    string
		project string
		key     feedback.LabelRejectionKey
		want    error
	}{
		{"A's photo", f.B.ProjectUID, feedback.LabelRejectionKey{PhotoUID: f.A.PhotoUID, LabelUID: f.B.LabelUID},
			feedback.ErrTargetNotFound},
		{"A's label", f.B.ProjectUID, feedback.LabelRejectionKey{PhotoUID: f.B.PhotoUID, LabelUID: f.A.LabelUID},
			feedback.ErrTargetNotFound},
		{"no project", "", feedback.LabelRejectionKey{PhotoUID: f.B.PhotoUID, LabelUID: f.B.LabelUID},
			feedback.ErrNoProject},
	}
	for _, c := range refused {
		if err := store.RejectLabel(ctx, c.project, c.key, entry); !errors.Is(err, c.want) {
			t.Errorf("RejectLabel(%s) = %v, want %v", c.name, err, c.want)
		}
		if err := store.UnrejectLabel(ctx, c.project, c.key, entry); !errors.Is(err, c.want) {
			t.Errorf("UnrejectLabel(%s) = %v, want %v", c.name, err, c.want)
		}
	}
	assertNoOpinions(t, db.Pool(), "label_rejections", audit.ActionLabelReject)

	own := feedback.LabelRejectionKey{PhotoUID: f.B.PhotoUID, LabelUID: f.B.LabelUID}
	if err := store.RejectLabel(ctx, f.B.ProjectUID, own, entry); err != nil {
		t.Fatalf("RejectLabel on B's own label: %v", err)
	}
	if ok, err := store.IsLabelRejected(ctx, own); err != nil || !ok {
		t.Errorf("B's own rejection recorded = %v, %v; want true", ok, err)
	}
	assertAuditProject(t, db.Pool(), audit.ActionLabelReject, f.B.ProjectUID)
}

// TestDuplicatePairs_neverCrossProjects dismisses, un-dismisses, confirms and
// un-confirms near-duplicate pairs from inside B: a pair holding one of A's
// photos, a pair entirely of A's photos and a call without a project are all
// refused with nothing written. B's own pair is recorded and stamped with B's
// project.
func TestDuplicatePairs_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	store := feedback.NewStore(pool)
	ctx := t.Context()
	otherA := insertPhoto(t, pool, f.A.ProjectUID)
	otherB := insertPhoto(t, pool, f.B.ProjectUID)
	entry := audit.Entry{Action: audit.ActionDuplicateDismiss, TargetType: "photos"}

	refused := []struct {
		name    string
		project string
		photo   string
		other   string
		want    error
	}{
		{"mixed pair", f.B.ProjectUID, f.B.PhotoUID, f.A.PhotoUID, feedback.ErrTargetNotFound},
		{"A's pair", f.B.ProjectUID, f.A.PhotoUID, otherA, feedback.ErrTargetNotFound},
		{"no project", "", f.B.PhotoUID, otherB, feedback.ErrNoProject},
	}
	for _, c := range refused {
		dismissal := feedback.DuplicateDismissalKey{PhotoUID: c.photo, OtherUID: c.other}
		confirmation := feedback.DuplicateConfirmationKey{PhotoUID: c.photo, OtherUID: c.other}
		if err := store.DismissDuplicate(ctx, c.project, dismissal, entry); !errors.Is(err, c.want) {
			t.Errorf("DismissDuplicate(%s) = %v, want %v", c.name, err, c.want)
		}
		if err := store.UndismissDuplicate(ctx, c.project, dismissal, entry); !errors.Is(err, c.want) {
			t.Errorf("UndismissDuplicate(%s) = %v, want %v", c.name, err, c.want)
		}
		if err := store.ConfirmDuplicate(ctx, c.project, confirmation, entry); !errors.Is(err, c.want) {
			t.Errorf("ConfirmDuplicate(%s) = %v, want %v", c.name, err, c.want)
		}
		if err := store.UnconfirmDuplicate(ctx, c.project, confirmation, entry); !errors.Is(err, c.want) {
			t.Errorf("UnconfirmDuplicate(%s) = %v, want %v", c.name, err, c.want)
		}
	}
	assertNoOpinions(t, pool, "duplicate_dismissals", audit.ActionDuplicateDismiss)
	assertNoOpinions(t, pool, "duplicate_confirmations", audit.ActionDuplicateDismiss)

	own := feedback.DuplicateDismissalKey{PhotoUID: f.B.PhotoUID, OtherUID: otherB}
	if err := store.DismissDuplicate(ctx, f.B.ProjectUID, own, entry); err != nil {
		t.Fatalf("DismissDuplicate on B's own pair: %v", err)
	}
	if ok, err := store.IsDuplicateDismissed(ctx, own); err != nil || !ok {
		t.Errorf("B's own dismissal recorded = %v, %v; want true", ok, err)
	}
	assertAuditProject(t, pool, audit.ActionDuplicateDismiss, f.B.ProjectUID)
}

// insertPhoto inserts one more 640x480 photo into the project and returns its
// uid.
func insertPhoto(t *testing.T, pool *pgxpool.Pool, projectUID string) string {
	t.Helper()
	uid := projecttest.MissingUID("ph")
	sum := sha256.Sum256([]byte(uid))
	if _, err := pool.Exec(t.Context(), `INSERT INTO photos (uid, project_id, file_hash, file_path, file_name,
		file_size, file_mime, file_width, file_height, taken_at)
		VALUES ($1, $2, $3, $4, $5, 1024, 'image/jpeg', 640, 480, '2026-06-02T12:00:00Z')`,
		uid, projectUID, hex.EncodeToString(sum[:]), "2026/10/"+uid+".jpg", uid+".jpg"); err != nil {
		t.Fatalf("inserting a photo into %s: %v", projectUID, err)
	}
	return uid
}

// assertNoOpinions fails t unless table is empty and the audit log holds no
// entry of action.
func assertNoOpinions(t *testing.T, pool *pgxpool.Pool, table, action string) {
	t.Helper()
	var rows, audited int
	if err := pool.QueryRow(t.Context(),
		"SELECT (SELECT count(*) FROM "+table+"), (SELECT count(*) FROM audit_log WHERE action = $1)",
		action).Scan(&rows, &audited); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	if rows != 0 || audited != 0 {
		t.Errorf("refused opinions left %d rows in %s and %d audit entries", rows, table, audited)
	}
}

// assertAuditProject fails t unless every audit entry of action is stamped with
// projectUID (and there is at least one).
func assertAuditProject(t *testing.T, pool *pgxpool.Pool, action, projectUID string) {
	t.Helper()
	var total, inProject int
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*), count(*) FILTER (WHERE project_id = $2) FROM audit_log WHERE action = $1",
		action, projectUID).Scan(&total, &inProject); err != nil {
		t.Fatalf("reading the audit entries of %s: %v", action, err)
	}
	if total == 0 || total != inProject {
		t.Errorf("audit entries of %s: %d, %d of them in project %s; want all", action, total, inProject, projectUID)
	}
}

// TestDuplicateMarkerDismissals_neverCrossProjects dismisses and un-dismisses
// repeated-marker groups from inside B naming A's photo, A's subject and no
// project: every one is refused (ErrTargetNotFound / ErrNoProject) and writes
// no row and no audit entry. An existing dismissal of A's survives B's attempt
// to take it back, and B's own dismissal is recorded and stamped with B's
// project.
func TestDuplicateMarkerDismissals_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	store := feedback.NewStore(pool)
	ctx := t.Context()
	dismiss := audit.Entry{Action: audit.ActionDuplicateMarkerDismiss, TargetType: "subjects"}
	undismiss := audit.Entry{Action: audit.ActionDuplicateMarkerUndismiss, TargetType: "subjects"}

	refused := []struct {
		name    string
		project string
		key     feedback.DuplicateMarkerDismissalKey
		want    error
	}{
		{"A's group", f.B.ProjectUID,
			feedback.DuplicateMarkerDismissalKey{PhotoUID: f.A.PhotoUID, SubjectUID: f.A.SubjectUID},
			feedback.ErrTargetNotFound},
		{"A's photo", f.B.ProjectUID,
			feedback.DuplicateMarkerDismissalKey{PhotoUID: f.A.PhotoUID, SubjectUID: f.B.SubjectUID},
			feedback.ErrTargetNotFound},
		{"A's subject", f.B.ProjectUID,
			feedback.DuplicateMarkerDismissalKey{PhotoUID: f.B.PhotoUID, SubjectUID: f.A.SubjectUID},
			feedback.ErrTargetNotFound},
		{"no project", "",
			feedback.DuplicateMarkerDismissalKey{PhotoUID: f.B.PhotoUID, SubjectUID: f.B.SubjectUID},
			feedback.ErrNoProject},
	}
	for _, c := range refused {
		if err := store.DismissDuplicateMarkers(ctx, c.project, c.key, dismiss); !errors.Is(err, c.want) {
			t.Errorf("DismissDuplicateMarkers(%s) = %v, want %v", c.name, err, c.want)
		}
	}
	assertNoOpinions(t, pool, "duplicate_marker_dismissals", audit.ActionDuplicateMarkerDismiss)

	aGroup := feedback.DuplicateMarkerDismissalKey{PhotoUID: f.A.PhotoUID, SubjectUID: f.A.SubjectUID}
	if err := store.DismissDuplicateMarkers(ctx, f.A.ProjectUID, aGroup, dismiss); err != nil {
		t.Fatalf("DismissDuplicateMarkers on A's own group: %v", err)
	}
	for _, c := range refused {
		if err := store.UndismissDuplicateMarkers(ctx, c.project, c.key, undismiss); !errors.Is(err, c.want) {
			t.Errorf("UndismissDuplicateMarkers(%s) = %v, want %v", c.name, err, c.want)
		}
	}
	if ok, err := store.IsDuplicateMarkersDismissed(ctx, aGroup); err != nil || !ok {
		t.Errorf("A's dismissal after B's take-back attempts = %v, %v; want still dismissed", ok, err)
	}
	var undone int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE action = $1",
		audit.ActionDuplicateMarkerUndismiss).Scan(&undone); err != nil {
		t.Fatalf("counting take-back audit entries: %v", err)
	}
	if undone != 0 {
		t.Errorf("refused take-backs left %d audit entries", undone)
	}

	own := feedback.DuplicateMarkerDismissalKey{PhotoUID: f.B.PhotoUID, SubjectUID: f.B.SubjectUID}
	if err := store.DismissDuplicateMarkers(ctx, f.B.ProjectUID, own, dismiss); err != nil {
		t.Fatalf("DismissDuplicateMarkers on B's own group: %v", err)
	}
	var stamped int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE action = $1 AND project_id = $2",
		audit.ActionDuplicateMarkerDismiss, f.B.ProjectUID).Scan(&stamped); err != nil {
		t.Fatalf("reading B's audit entries: %v", err)
	}
	if stamped != 1 {
		t.Errorf("B's dismissal audit entries stamped with B = %d, want 1", stamped)
	}
}

// TestExclusionLists_scopedToProject records one near-duplicate dismissal, one
// confirmation and one repeated-marker dismissal in each project and asserts
// each project's bulk lists return its own row and never the other project's.
func TestExclusionLists_scopedToProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	store := feedback.NewStore(pool)
	ctx := t.Context()
	entry := audit.Entry{Action: audit.ActionDuplicateDismiss, TargetType: "photos"}

	want := map[projecttest.Side]string{}
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tn := f.Tenant(side)
		other := insertPhoto(t, pool, tn.ProjectUID)
		want[side] = tn.PhotoUID
		if err := store.DismissDuplicate(ctx, tn.ProjectUID,
			feedback.DuplicateDismissalKey{PhotoUID: tn.PhotoUID, OtherUID: other}, entry); err != nil {
			t.Fatalf("DismissDuplicate in %s: %v", side, err)
		}
		if err := store.ConfirmDuplicate(ctx, tn.ProjectUID,
			feedback.DuplicateConfirmationKey{PhotoUID: tn.PhotoUID, OtherUID: other}, entry); err != nil {
			t.Fatalf("ConfirmDuplicate in %s: %v", side, err)
		}
		if err := store.DismissDuplicateMarkers(ctx, tn.ProjectUID,
			feedback.DuplicateMarkerDismissalKey{PhotoUID: tn.PhotoUID, SubjectUID: tn.SubjectUID}, entry); err != nil {
			t.Fatalf("DismissDuplicateMarkers in %s: %v", side, err)
		}
	}

	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		projectUID := f.Tenant(side).ProjectUID
		dismissed, err := store.DismissedDuplicatePairs(ctx, projectUID)
		if err != nil {
			t.Fatalf("DismissedDuplicatePairs(%s): %v", side, err)
		}
		confirmed, err := store.ConfirmedDuplicatePairs(ctx, projectUID)
		if err != nil {
			t.Fatalf("ConfirmedDuplicatePairs(%s): %v", side, err)
		}
		groups, err := store.DismissedDuplicateMarkerGroups(ctx, projectUID)
		if err != nil {
			t.Fatalf("DismissedDuplicateMarkerGroups(%s): %v", side, err)
		}
		if len(dismissed) != 1 || !pairNames(dismissed[0].PhotoUID, dismissed[0].OtherUID, want[side]) {
			t.Errorf("%s's dismissed pairs = %+v, want only its own pair with %s", side, dismissed, want[side])
		}
		if len(confirmed) != 1 || !pairNames(confirmed[0].PhotoUID, confirmed[0].OtherUID, want[side]) {
			t.Errorf("%s's confirmed pairs = %+v, want only its own pair with %s", side, confirmed, want[side])
		}
		if len(groups) != 1 || groups[0].PhotoUID != want[side] {
			t.Errorf("%s's dismissed marker groups = %+v, want only its own group on %s", side, groups, want[side])
		}
	}
}

// pairNames reports whether the pair (a, b) names uid.
func pairNames(a, b, uid string) bool {
	return a == uid || b == uid
}
