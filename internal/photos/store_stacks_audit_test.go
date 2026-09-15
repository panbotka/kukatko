package photos

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
)

// TestStackPlanNormalize checks the two ways a plan can fail to describe a stack
// and that a valid one keeps its members deduped in first-seen order.
func TestStackPlanNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		plan    StackPlan
		want    []string
		wantErr error
	}{
		{
			name: "duplicates collapse but order holds",
			plan: StackPlan{PrimaryUID: "b", MemberUIDs: []string{"c", "b", "c"}},
			want: []string{"c", "b"},
		},
		{
			name:    "one distinct member is not a stack",
			plan:    StackPlan{PrimaryUID: "a", MemberUIDs: []string{"a", "a"}},
			wantErr: ErrStackTooSmall,
		},
		{
			name:    "the primary must be a member",
			plan:    StackPlan{PrimaryUID: "z", MemberUIDs: []string{"a", "b"}},
			wantErr: ErrPhotoNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.plan.normalize()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalize error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if len(got.MemberUIDs) != len(tt.want) {
				t.Fatalf("members = %v, want %v", got.MemberUIDs, tt.want)
			}
			for i, uid := range tt.want {
				if got.MemberUIDs[i] != uid {
					t.Errorf("members[%d] = %q, want %q", i, got.MemberUIDs[i], uid)
				}
			}
		})
	}
}

// TestStackEntry checks the entry handed down by a caller is completed rather
// than mutated: the facts the transaction learnt are merged in, the target is
// defaulted, and the caller's own map is left untouched — the copy is what keeps
// a detail stamped inside a transaction closure from being lost.
func TestStackEntry(t *testing.T) {
	t.Parallel()

	callerDetails := map[string]any{"via": "review"}
	entry := audit.Entry{Action: audit.ActionStackUngroup, Details: callerDetails}

	got := stackEntry(entry, "pht1", map[string]any{"stack_uid": "stk1"})

	if got.TargetUID != "pht1" {
		t.Errorf("target = %q, want the defaulted photo uid", got.TargetUID)
	}
	if got.Details["via"] != "review" || got.Details["stack_uid"] != "stk1" {
		t.Errorf("details = %v, want both the caller's and the store's", got.Details)
	}
	if len(callerDetails) != 1 {
		t.Errorf("caller details = %v, want them untouched", callerDetails)
	}
}

// TestStackEntry_keepsAnExplicitTarget checks a caller that already named a target
// keeps it: defaulting must never overwrite a decision.
func TestStackEntry_keepsAnExplicitTarget(t *testing.T) {
	t.Parallel()

	got := stackEntry(audit.Entry{TargetUID: "chosen"}, "pht1", nil)
	if got.TargetUID != "chosen" {
		t.Errorf("target = %q, want the caller's own", got.TargetUID)
	}
}
