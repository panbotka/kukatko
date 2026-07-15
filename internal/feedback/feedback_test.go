package feedback_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/feedback"
)

// TestEmptyKeyValidation checks that every store entry point rejects an incomplete
// key with ErrEmptyKey before touching the database — so a nil pool is never
// dereferenced. This runs without a database.
func TestEmptyKeyValidation(t *testing.T) {
	t.Parallel()

	store := feedback.NewStore(nil)
	ctx := context.Background()
	entry := audit.Entry{Action: audit.ActionFaceReject}

	faceKey := feedback.FaceRejectionKey{FaceIndex: 0} // no photo/subject UID
	labelKey := feedback.LabelRejectionKey{}           // no photo/label UID

	checks := []struct {
		name string
		err  error
	}{
		{"RejectFace", store.RejectFace(ctx, faceKey, entry)},
		{"UnrejectFace", store.UnrejectFace(ctx, faceKey, entry)},
		{"RejectLabel", store.RejectLabel(ctx, labelKey, entry)},
		{"UnrejectLabel", store.UnrejectLabel(ctx, labelKey, entry)},
	}
	for _, c := range checks {
		if !errors.Is(c.err, feedback.ErrEmptyKey) {
			t.Errorf("%s empty key = %v, want ErrEmptyKey", c.name, c.err)
		}
	}

	if _, err := store.IsFaceRejected(ctx, faceKey); !errors.Is(err, feedback.ErrEmptyKey) {
		t.Errorf("IsFaceRejected empty key = %v, want ErrEmptyKey", err)
	}
	if _, err := store.IsLabelRejected(ctx, labelKey); !errors.Is(err, feedback.ErrEmptyKey) {
		t.Errorf("IsLabelRejected empty key = %v, want ErrEmptyKey", err)
	}
	if _, err := store.FaceRejectionsForSubject(ctx, ""); !errors.Is(err, feedback.ErrEmptyKey) {
		t.Errorf("FaceRejectionsForSubject empty = %v, want ErrEmptyKey", err)
	}
	if _, err := store.LabelRejectionsForLabel(ctx, ""); !errors.Is(err, feedback.ErrEmptyKey) {
		t.Errorf("LabelRejectionsForLabel empty = %v, want ErrEmptyKey", err)
	}
}
