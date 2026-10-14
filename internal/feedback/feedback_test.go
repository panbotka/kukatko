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
	const projectUID = "pr_test" // never reached: the key check comes first

	faceKey := feedback.FaceRejectionKey{FaceIndex: 0}       // no photo/subject UID
	labelKey := feedback.LabelRejectionKey{}                 // no photo/label UID
	confirmKey := feedback.FaceConfirmationKey{FaceIndex: 0} // no photo/subject UID
	markerKey := feedback.DuplicateMarkerDismissalKey{}      // no photo/subject UID

	checks := []struct {
		name string
		err  error
	}{
		{"RejectFace", store.RejectFace(ctx, projectUID, faceKey, entry)},
		{"UnrejectFace", store.UnrejectFace(ctx, projectUID, faceKey, entry)},
		{"RejectLabel", store.RejectLabel(ctx, projectUID, labelKey, entry)},
		{"UnrejectLabel", store.UnrejectLabel(ctx, projectUID, labelKey, entry)},
		{"ConfirmFace", store.ConfirmFace(ctx, projectUID, confirmKey, entry)},
		{"UnconfirmFace", store.UnconfirmFace(ctx, projectUID, confirmKey, entry)},
		{"DismissDuplicateMarkers", store.DismissDuplicateMarkers(ctx, projectUID, markerKey, entry)},
		{"UndismissDuplicateMarkers", store.UndismissDuplicateMarkers(ctx, projectUID, markerKey, entry)},
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
	if _, err := store.IsFaceConfirmed(ctx, confirmKey); !errors.Is(err, feedback.ErrEmptyKey) {
		t.Errorf("IsFaceConfirmed empty key = %v, want ErrEmptyKey", err)
	}
	if _, err := store.FaceRejectionsForSubject(ctx, ""); !errors.Is(err, feedback.ErrEmptyKey) {
		t.Errorf("FaceRejectionsForSubject empty = %v, want ErrEmptyKey", err)
	}
	if _, err := store.FaceConfirmationsForSubject(ctx, ""); !errors.Is(err, feedback.ErrEmptyKey) {
		t.Errorf("FaceConfirmationsForSubject empty = %v, want ErrEmptyKey", err)
	}
	if _, err := store.LabelRejectionsForLabel(ctx, ""); !errors.Is(err, feedback.ErrEmptyKey) {
		t.Errorf("LabelRejectionsForLabel empty = %v, want ErrEmptyKey", err)
	}
	if _, err := store.IsDuplicateMarkersDismissed(ctx, markerKey); !errors.Is(err, feedback.ErrEmptyKey) {
		t.Errorf("IsDuplicateMarkersDismissed empty key = %v, want ErrEmptyKey", err)
	}
}

// TestExclusionListsNeedAProject checks that the three bulk exclusion lists a
// per-project scan reads refuse an empty project with ErrNoProject before
// touching the database — there is no "every project's" list to fall back to.
func TestExclusionListsNeedAProject(t *testing.T) {
	t.Parallel()

	store := feedback.NewStore(nil)
	ctx := context.Background()
	if _, err := store.DismissedDuplicatePairs(ctx, ""); !errors.Is(err, feedback.ErrNoProject) {
		t.Errorf("DismissedDuplicatePairs without a project = %v, want ErrNoProject", err)
	}
	if _, err := store.ConfirmedDuplicatePairs(ctx, ""); !errors.Is(err, feedback.ErrNoProject) {
		t.Errorf("ConfirmedDuplicatePairs without a project = %v, want ErrNoProject", err)
	}
	if _, err := store.DismissedDuplicateMarkerGroups(ctx, ""); !errors.Is(err, feedback.ErrNoProject) {
		t.Errorf("DismissedDuplicateMarkerGroups without a project = %v, want ErrNoProject", err)
	}
	key := feedback.DuplicateMarkerDismissalKey{PhotoUID: "ph1", SubjectUID: "su1"}
	entry := audit.Entry{Action: audit.ActionDuplicateMarkerDismiss}
	if err := store.DismissDuplicateMarkers(ctx, "", key, entry); !errors.Is(err, feedback.ErrNoProject) {
		t.Errorf("DismissDuplicateMarkers without a project = %v, want ErrNoProject", err)
	}
	if err := store.UndismissDuplicateMarkers(ctx, "", key, entry); !errors.Is(err, feedback.ErrNoProject) {
		t.Errorf("UndismissDuplicateMarkers without a project = %v, want ErrNoProject", err)
	}
}
