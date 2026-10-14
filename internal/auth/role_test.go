package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/project"
)

// TestRole_Valid verifies the instance roles: user and maintainer are valid,
// every retired ladder value (and garbage) is not.
func TestRole_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role Role
		want bool
	}{
		{RoleUser, true},
		{RoleMaintainer, true},
		{Role("admin"), false},
		{Role("editor"), false},
		{Role("viewer"), false},
		{Role("ai"), false},
		{Role(""), false},
	}
	for _, tt := range tests {
		if got := tt.role.Valid(); got != tt.want {
			t.Errorf("Role(%q).Valid() = %v, want %v", tt.role, got, tt.want)
		}
	}
}

// TestRole_Predicates verifies the instance operations belong to a maintainer
// alone.
func TestRole_Predicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role Role
		want bool
	}{
		{RoleMaintainer, true},
		{RoleUser, false},
		{Role(""), false},
	}
	for _, tt := range tests {
		if got := tt.role.CanMaintain(); got != tt.want {
			t.Errorf("Role(%q).CanMaintain() = %v, want %v", tt.role, got, tt.want)
		}
		if got := tt.role.CanImport(); got != tt.want {
			t.Errorf("Role(%q).CanImport() = %v, want %v", tt.role, got, tt.want)
		}
	}
}

// TestAccountRole_Split verifies the transitional five-rung vocabulary maps
// onto the instance role and the project role, and folds back unchanged.
func TestAccountRole_Split(t *testing.T) {
	t.Parallel()

	tests := []struct {
		account      AccountRole
		wantValid    bool
		wantInstance Role
		wantProject  project.Role
	}{
		{AccountMaintainer, true, RoleMaintainer, project.RoleAdmin},
		{AccountAdmin, true, RoleUser, project.RoleAdmin},
		{AccountEditor, true, RoleUser, project.RoleEditor},
		{AccountCurator, true, RoleUser, project.RoleCurator},
		{AccountViewer, true, RoleUser, project.RoleViewer},
		{AccountRole("ai"), false, RoleUser, project.Role("ai")},
		{AccountRole(""), false, RoleUser, project.Role("")},
	}
	for _, tt := range tests {
		if got := tt.account.Valid(); got != tt.wantValid {
			t.Errorf("AccountRole(%q).Valid() = %v, want %v", tt.account, got, tt.wantValid)
		}
		instance, inProject := tt.account.Split()
		if instance != tt.wantInstance || inProject != tt.wantProject {
			t.Errorf("AccountRole(%q).Split() = %q, %q, want %q, %q",
				tt.account, instance, inProject, tt.wantInstance, tt.wantProject)
		}
	}
}

// TestAuthorize verifies the guards read the project role for the four
// project-level requirements and the instance role for the two instance-level
// ones, and that no project role (or an invalid one) satisfies nothing
// project-level.
func TestAuthorize(t *testing.T) {
	t.Parallel()

	reqs := []requirement{requireAuth, requireCurate, requireWrite, requireAdmin, requireMaintain, requireImport}
	tests := []struct {
		instance  Role
		inProject project.Role
		want      []bool // in reqs order
	}{
		{RoleUser, project.RoleViewer, []bool{true, false, false, false, false, false}},
		{RoleUser, project.RoleCurator, []bool{true, true, false, false, false, false}},
		{RoleUser, project.RoleEditor, []bool{true, true, true, false, false, false}},
		{RoleUser, project.RoleAdmin, []bool{true, true, true, true, false, false}},
		{RoleUser, "", []bool{false, false, false, false, false, false}},
		{RoleUser, project.Role("maintainer"), []bool{false, false, false, false, false, false}},
		{RoleMaintainer, project.RoleAdmin, []bool{true, true, true, true, true, true}},
		{RoleMaintainer, "", []bool{false, false, false, false, true, true}},
		{Role("admin"), project.RoleViewer, []bool{true, false, false, false, false, false}},
	}
	for _, tt := range tests {
		for i, req := range reqs {
			if got := authorize(tt.instance, tt.inProject, req); got != tt.want[i] {
				t.Errorf("authorize(%q, %q, %d) = %v, want %v", tt.instance, tt.inProject, req, got, tt.want[i])
			}
		}
	}
	if authorize(RoleMaintainer, project.RoleAdmin, requirement(99)) {
		t.Error("an unknown requirement must never be satisfied")
	}
}

// TestRequirement_instanceLevel verifies only the maintainer and import
// requirements are decided by the instance role.
func TestRequirement_instanceLevel(t *testing.T) {
	t.Parallel()

	for req, want := range map[requirement]bool{
		requireAuth: false, requireCurate: false, requireWrite: false, requireAdmin: false,
		requireMaintain: true, requireImport: true,
	} {
		if got := req.instanceLevel(); got != want {
			t.Errorf("requirement(%d).instanceLevel() = %v, want %v", req, got, want)
		}
	}
}

// TestAuthorizeUserManagement verifies the maintainer boundary on user-management
// actions: only a maintainer may grant the maintainer role or touch an account
// that already holds it, while every other action is allowed.
func TestAuthorizeUserManagement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		actor   Role
		current Role
		newRole Role
		wantErr error
	}{
		{"user creates user", RoleUser, "", RoleUser, nil},
		{"user changes a user", RoleUser, RoleUser, RoleUser, nil},
		{"user creates maintainer", RoleUser, "", RoleMaintainer, ErrMaintainerRequired},
		{"user promotes a user to maintainer", RoleUser, RoleUser, RoleMaintainer, ErrMaintainerRequired},
		{"user modifies maintainer", RoleUser, RoleMaintainer, RoleMaintainer, ErrMaintainerRequired},
		{"user disables maintainer (no role change)", RoleUser, RoleMaintainer, "", ErrMaintainerRequired},
		{"maintainer creates maintainer", RoleMaintainer, "", RoleMaintainer, nil},
		{"maintainer promotes a user", RoleMaintainer, RoleUser, RoleMaintainer, nil},
		{"maintainer modifies maintainer", RoleMaintainer, RoleMaintainer, RoleMaintainer, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := authorizeUserManagement(tt.actor, tt.current, tt.newRole); !errors.Is(got, tt.wantErr) {
				t.Errorf("authorizeUserManagement(%q, %q, %q) = %v, want %v",
					tt.actor, tt.current, tt.newRole, got, tt.wantErr)
			}
		})
	}
}

// TestLastProjectThrottle_due verifies the last-project write happens once per
// interval per account, at once on a project switch, and that stale entries
// are dropped.
func TestLastProjectThrottle_due(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	var th lastProjectThrottle
	steps := []struct {
		name    string
		user    string
		project string
		at      time.Time
		want    bool
	}{
		{"first sight", "us1", "pj1", start, true},
		{"again within the minute", "us1", "pj1", start.Add(30 * time.Second), false},
		{"another account", "us2", "pj1", start.Add(30 * time.Second), true},
		{"a project switch", "us1", "pj2", start.Add(40 * time.Second), true},
		{"after the interval", "us1", "pj2", start.Add(40*time.Second + lastProjectTouchInterval), true},
	}
	for _, s := range steps {
		if got := th.due(s.user, s.project, s.at); got != s.want {
			t.Errorf("%s: due = %v, want %v", s.name, got, s.want)
		}
	}
	if _, ok := th.seen["us2"]; ok {
		t.Error("an entry older than the interval should have been dropped")
	}
}
