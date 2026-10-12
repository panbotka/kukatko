//go:build integration

package feedback_test

import (
	"errors"
	"testing"

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
