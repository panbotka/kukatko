package auth

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestPendingParam covers the user listing's approval filter: absent means no
// filter, the two booleans mean the two halves of the roster, and anything else
// is refused rather than silently listing everybody.
func TestPendingParam(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    *bool
		wantErr bool
	}{
		{name: "absent", query: ""},
		{name: "empty", query: "?pending="},
		{name: "true", query: "?pending=true", want: new(true)},
		{name: "one", query: "?pending=1", want: new(true)},
		{name: "false", query: "?pending=false", want: new(false)},
		{name: "zero", query: "?pending=0", want: new(false)},
		{name: "nonsense", query: "?pending=maybe", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
				"/admin/users"+tt.query, nil)
			got, err := pendingParam(r)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("pendingParam(%q) = %v, want an error", tt.query, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("pendingParam(%q): %v", tt.query, err)
			}
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("pendingParam(%q) = %v, want no filter", tt.query, *got)
			case tt.want != nil && got == nil:
				t.Errorf("pendingParam(%q) = no filter, want %v", tt.query, *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("pendingParam(%q) = %v, want %v", tt.query, *got, *tt.want)
			}
		})
	}
}

// TestUpdateUserRequest_auditDetails pins that the user.update audit entry
// names only the permission-carrying fields the patch really sets: a name-only
// edit records neither a role nor a disabled flag it never sent.
func TestUpdateUserRequest_auditDetails(t *testing.T) {
	tests := []struct {
		name string
		req  updateUserRequest
		want map[string]any
	}{
		{name: "name only", req: updateUserRequest{DisplayName: new("Jan")}, want: map[string]any{}},
		{
			name: "role and flag",
			req:  updateUserRequest{InstanceRole: new(RoleMaintainer), Disabled: new(false)},
			want: map[string]any{"instance_role": "maintainer", "disabled": false},
		},
		{name: "unblock", req: updateUserRequest{Disabled: new(false)}, want: map[string]any{"disabled": false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.auditDetails(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("auditDetails() = %v, want %v", got, tt.want)
			}
		})
	}
}
