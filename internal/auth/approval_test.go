package auth

import (
	"testing"
	"time"
)

// TestPrepareNewUser_approvesOnCreation checks that an account built here is
// approved from the start and has not seen the welcome. Both facts matter to the
// admin API: an administrator making an account *is* the approval, and a brand
// new account must still be shown the welcome once.
func TestPrepareNewUser_approvesOnCreation(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, time.August, 23, 10, 30, 0, 0, time.UTC)
	svc := NewService(nil, SessionPolicy{}).WithClock(func() time.Time { return created })

	user, err := svc.prepareNewUser(CreateUserInput{
		Username: "alice",
		Password: "correct horse battery",
		Email:    "alice@example.com",
		Role:     RoleViewer,
	})
	if err != nil {
		t.Fatalf("prepareNewUser: %v", err)
	}
	if user.ApprovedAt == nil {
		t.Fatal("ApprovedAt = nil, want the creation time — an admin-made account is approved")
	}
	if !user.ApprovedAt.Equal(created) {
		t.Errorf("ApprovedAt = %v, want %v", *user.ApprovedAt, created)
	}
	if user.WelcomeSeenAt != nil {
		t.Errorf("WelcomeSeenAt = %v, want nil — a new account has seen nothing", *user.WelcomeSeenAt)
	}
}

// TestInsertUserArgs covers the argument list shared by the plain and audited
// insert paths: the approval stamp is carried through as given (including the
// nil that means "waiting for an administrator"), and an empty subject link is
// normalised to nil so it reaches the foreign key as "no link".
func TestInsertUserArgs(t *testing.T) {
	t.Parallel()

	approved := time.Date(2026, time.August, 23, 10, 30, 0, 0, time.UTC)
	empty := ""
	subject := "su_1"

	tests := []struct {
		name        string
		user        User
		wantApprove *time.Time
		wantSubject *string
	}{
		{
			name:        "approved account carries its stamp",
			user:        User{UID: "us_1", ApprovedAt: &approved},
			wantApprove: &approved,
			wantSubject: nil,
		},
		{
			name:        "unapproved account carries a nil stamp",
			user:        User{UID: "us_2"},
			wantApprove: nil,
			wantSubject: nil,
		},
		{
			name:        "empty subject link is normalised away",
			user:        User{UID: "us_3", SubjectUID: &empty},
			wantApprove: nil,
			wantSubject: nil,
		},
		{
			name:        "real subject link is passed through",
			user:        User{UID: "us_4", SubjectUID: &subject},
			wantApprove: nil,
			wantSubject: &subject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := insertUserArgs(tt.user)
			const wantLen = 10
			if len(args) != wantLen {
				t.Fatalf("insertUserArgs returned %d args, want %d (insertUserQuery placeholders)",
					len(args), wantLen)
			}
			if got := args[0]; got != any(tt.user.UID) {
				t.Errorf("args[0] = %v, want %v", got, tt.user.UID)
			}
			gotSubject, _ := args[8].(*string)
			switch {
			case tt.wantSubject == nil && gotSubject != nil:
				t.Errorf("subject arg = %q, want nil", *gotSubject)
			case tt.wantSubject != nil && gotSubject == nil:
				t.Errorf("subject arg = nil, want %q", *tt.wantSubject)
			case tt.wantSubject != nil && *gotSubject != *tt.wantSubject:
				t.Errorf("subject arg = %q, want %q", *gotSubject, *tt.wantSubject)
			}
			gotApprove, _ := args[9].(*time.Time)
			switch {
			case tt.wantApprove == nil && gotApprove != nil:
				t.Errorf("approved_at arg = %v, want nil", *gotApprove)
			case tt.wantApprove != nil && gotApprove == nil:
				t.Errorf("approved_at arg = nil, want %v", *tt.wantApprove)
			case tt.wantApprove != nil && !gotApprove.Equal(*tt.wantApprove):
				t.Errorf("approved_at arg = %v, want %v", *gotApprove, *tt.wantApprove)
			}
		})
	}
}
