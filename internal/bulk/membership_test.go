package bulk

import (
	"errors"
	"testing"
)

// TestMembershipSummary_rejectsBatchesBeforeTheDatabase verifies the summary
// refuses the same batches Apply does — empty and oversized — without touching
// the pool, so a nil pool is enough to prove no query ran.
func TestMembershipSummary_rejectsBatchesBeforeTheDatabase(t *testing.T) {
	t.Parallel()

	svc := NewService(nil, 2)
	tests := []struct {
		name    string
		uids    []string
		wantErr error
	}{
		{name: "no photos", uids: nil, wantErr: ErrNoPhotos},
		{name: "too large", uids: []string{"ph1", "ph2", "ph3"}, wantErr: ErrBatchTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.MembershipSummary(t.Context(), "pr-test", tt.uids)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestService_refusesWithoutAProject verifies every entry point refuses a call
// without a project before touching the pool (a nil pool proves no query ran):
// an empty project is a programming error, never "any project".
func TestService_refusesWithoutAProject(t *testing.T) {
	t.Parallel()

	svc := NewService(nil, 10)
	uids := []string{"ph1"}
	if _, err := svc.Apply(t.Context(), "", "us1", uids, Operations{Hide: new(true)}); !errors.Is(err, ErrNoProject) {
		t.Errorf("Apply err = %v, want ErrNoProject", err)
	}
	if _, err := svc.LocationSummary(t.Context(), "", uids); !errors.Is(err, ErrNoProject) {
		t.Errorf("LocationSummary err = %v, want ErrNoProject", err)
	}
	if _, err := svc.MembershipSummary(t.Context(), "", uids); !errors.Is(err, ErrNoProject) {
		t.Errorf("MembershipSummary err = %v, want ErrNoProject", err)
	}
}
