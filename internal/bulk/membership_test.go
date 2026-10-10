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
			_, err := svc.MembershipSummary(t.Context(), tt.uids)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
