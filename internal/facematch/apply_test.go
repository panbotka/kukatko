package facematch

import (
	"testing"

	"github.com/panbotka/kukatko/internal/people"
)

// TestAssignDetails_via confirms the audit details carry the via marker only
// when the request sets it, so review-originated confirmations are tagged
// details.via = "review" while ordinary assignments stay untagged.
func TestAssignDetails_via(t *testing.T) {
	t.Parallel()

	faceIdx := 3
	subject := people.Subject{UID: "subj1", Name: "Alice"}
	tests := []struct {
		name    string
		req     AssignRequest
		wantVia any
	}{
		{
			name:    "review request tags via",
			req:     AssignRequest{Action: ActionCreateMarker, PhotoUID: "p1", FaceIndex: &faceIdx, Via: "review"},
			wantVia: "review",
		},
		{
			name:    "ordinary request leaves via unset",
			req:     AssignRequest{Action: ActionAssignPerson, PhotoUID: "p1", MarkerUID: "m1"},
			wantVia: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			details := assignDetails(tt.req, subject)
			if got := details["via"]; got != tt.wantVia {
				t.Errorf("details[via] = %v, want %v", got, tt.wantVia)
			}
			if details["subject_uid"] != subject.UID {
				t.Errorf("details[subject_uid] = %v, want %s", details["subject_uid"], subject.UID)
			}
		})
	}
}
