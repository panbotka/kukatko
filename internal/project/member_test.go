package project

import (
	"errors"
	"testing"
)

// TestParseRole verifies the four project roles parse and everything else —
// the instance-only maintainer included — is ErrInvalidRole.
func TestParseRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    Role
		wantErr error
	}{
		{in: "viewer", want: RoleViewer},
		{in: "curator", want: RoleCurator},
		{in: "editor", want: RoleEditor},
		{in: "admin", want: RoleAdmin},
		{in: "maintainer", wantErr: ErrInvalidRole},
		{in: "Admin", wantErr: ErrInvalidRole},
		{in: "", wantErr: ErrInvalidRole},
		{in: " viewer", wantErr: ErrInvalidRole},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRole(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseRole(%q) error = %v, want %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseRole(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestRole_ladder verifies every predicate on every rung of the strict ladder,
// plus an unknown role that satisfies none of them.
func TestRole_ladder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role                              Role
		valid, canCurate, canWrite, admin bool
	}{
		{role: RoleViewer, valid: true},
		{role: RoleCurator, valid: true, canCurate: true},
		{role: RoleEditor, valid: true, canCurate: true, canWrite: true},
		{role: RoleAdmin, valid: true, canCurate: true, canWrite: true, admin: true},
		{role: Role("maintainer")},
		{role: Role("")},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			t.Parallel()
			if got := tt.role.Valid(); got != tt.valid {
				t.Errorf("%q.Valid() = %v, want %v", tt.role, got, tt.valid)
			}
			if got := tt.role.CanCurate(); got != tt.canCurate {
				t.Errorf("%q.CanCurate() = %v, want %v", tt.role, got, tt.canCurate)
			}
			if got := tt.role.CanWrite(); got != tt.canWrite {
				t.Errorf("%q.CanWrite() = %v, want %v", tt.role, got, tt.canWrite)
			}
			if got := tt.role.IsAdmin(); got != tt.admin {
				t.Errorf("%q.IsAdmin() = %v, want %v", tt.role, got, tt.admin)
			}
		})
	}
}

// TestSameSubject verifies optional subject uids compare by value, nil only
// equal to nil.
func TestSameSubject(t *testing.T) {
	t.Parallel()

	a, a2, b := "sbj_a", "sbj_a", "sbj_b"
	tests := []struct {
		name string
		x, y *string
		want bool
	}{
		{name: "both nil", want: true},
		{name: "nil vs set", y: &a},
		{name: "set vs nil", x: &a},
		{name: "equal values", x: &a, y: &a2, want: true},
		{name: "different values", x: &a, y: &b},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sameSubject(tt.x, tt.y); got != tt.want {
				t.Errorf("sameSubject = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestWrapMemberErr verifies sentinels pass through unchanged and anything else
// is wrapped with context but still unwraps to the original.
func TestWrapMemberErr(t *testing.T) {
	t.Parallel()

	for _, sentinel := range []error{ErrNotFound, ErrNotMember, ErrAlreadyMember, ErrLastAdmin} {
		if got := wrapMemberErr("op", "pj", "us", sentinel); !errors.Is(got, sentinel) {
			t.Errorf("wrapMemberErr(%v) = %v, want the sentinel", sentinel, got)
		}
	}
	other := errors.New("boom")
	got := wrapMemberErr("adding", "pj1", "us1", other)
	if !errors.Is(got, other) || got.Error() != "project: adding member us1 of pj1: boom" {
		t.Errorf("wrapMemberErr(other) = %q", got)
	}
}
