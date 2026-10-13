package invite

import (
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/project"
)

// TestInvite_StateAt covers every state and the precedence between them:
// revoked beats expired, both beat used up.
func TestInvite_StateAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	one := 1
	tests := []struct {
		name string
		inv  Invite
		want State
	}{
		{"live", Invite{ExpiresAt: future}, StateActive},
		{"live under its cap", Invite{ExpiresAt: future, MaxUses: &one}, StateActive},
		{"expiry is exclusive", Invite{ExpiresAt: now}, StateExpired},
		{"expired", Invite{ExpiresAt: past}, StateExpired},
		{"revoked", Invite{ExpiresAt: future, RevokedAt: &past}, StateRevoked},
		{"revoked beats expired", Invite{ExpiresAt: past, RevokedAt: &past}, StateRevoked},
		{"used up", Invite{ExpiresAt: future, MaxUses: &one, Uses: 1}, StateUsedUp},
		{"expired beats used up", Invite{ExpiresAt: past, MaxUses: &one, Uses: 1}, StateExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.inv.StateAt(now); got != tt.want {
				t.Errorf("StateAt = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestInvite_openAt verifies an invite is open only while not revoked, not
// expired and its project active — the use cap is not part of it.
func TestInvite_openAt(t *testing.T) {
	t.Parallel()

	now := time.Now()
	past := now.Add(-time.Hour)
	one := 1
	base := Invite{ExpiresAt: now.Add(time.Hour), projectActive: true}
	tests := []struct {
		name string
		edit func(*Invite)
		want bool
	}{
		{"live", func(*Invite) {}, true},
		{"used up stays open", func(i *Invite) { i.MaxUses, i.Uses = &one, 1 }, true},
		{"revoked", func(i *Invite) { i.RevokedAt = &past }, false},
		{"expired", func(i *Invite) { i.ExpiresAt = past }, false},
		{"deactivated project", func(i *Invite) { i.projectActive = false }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inv := base
			tt.edit(&inv)
			if got := inv.openAt(now); got != tt.want {
				t.Errorf("openAt = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNewInvite_Validate covers each refusal and the accepted boundaries.
func TestNewInvite_Validate(t *testing.T) {
	t.Parallel()

	zero, one := 0, 1
	tests := []struct {
		name string
		in   NewInvite
		want error
	}{
		{"defaults", NewInvite{Role: project.RoleViewer}, nil},
		{"admin with a cap", NewInvite{Role: project.RoleAdmin, Days: MaxDays, MaxUses: &one}, nil},
		{"one day", NewInvite{Role: project.RoleCurator, Days: 1}, nil},
		{"no role", NewInvite{}, ErrInvalidRole},
		{"maintainer is no project role", NewInvite{Role: "maintainer"}, ErrInvalidRole},
		{"too long", NewInvite{Role: project.RoleEditor, Days: MaxDays + 1}, ErrInvalidDays},
		{"negative days", NewInvite{Role: project.RoleEditor, Days: -1}, ErrInvalidDays},
		{"zero cap", NewInvite{Role: project.RoleEditor, MaxUses: &zero}, ErrInvalidMaxUses},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.in.Validate(); !errors.Is(err, tt.want) {
				t.Errorf("Validate = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestNewInvite_days verifies no validity is DefaultDays and any other is kept.
func TestNewInvite_days(t *testing.T) {
	t.Parallel()

	if got := (NewInvite{}).days(); got != DefaultDays {
		t.Errorf("days() of none = %d, want %d", got, DefaultDays)
	}
	if got := (NewInvite{Days: 30}).days(); got != 30 {
		t.Errorf("days() of 30 = %d, want 30", got)
	}
}

// TestRegistrationRole verifies registration caps an invite at editor and keeps
// every lesser role.
func TestRegistrationRole(t *testing.T) {
	t.Parallel()

	for role, want := range map[project.Role]project.Role{
		project.RoleViewer: project.RoleViewer, project.RoleCurator: project.RoleCurator,
		project.RoleEditor: project.RoleEditor, project.RoleAdmin: project.RoleEditor,
	} {
		if got := RegistrationRole(role); got != want {
			t.Errorf("RegistrationRole(%s) = %s, want %s", role, got, want)
		}
	}
}

// TestHigher verifies the ladder viewer < curator < editor < admin, with an
// unknown role below all of them and the first argument winning a tie.
func TestHigher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b, want project.Role
	}{
		{project.RoleViewer, project.RoleCurator, project.RoleCurator},
		{project.RoleCurator, project.RoleEditor, project.RoleEditor},
		{project.RoleAdmin, project.RoleEditor, project.RoleAdmin},
		{project.RoleEditor, project.RoleEditor, project.RoleEditor},
		{"bogus", project.RoleViewer, project.RoleViewer},
	}
	for _, tt := range tests {
		if got := higher(tt.a, tt.b); got != tt.want {
			t.Errorf("higher(%s, %s) = %s, want %s", tt.a, tt.b, got, tt.want)
		}
	}
}
