package photoapi

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
)

// fakeUserResolver is a controllable UserResolver for the uploader-resolution
// unit tests: it maps a user UID to the account returned for it. An unknown UID
// yields auth.ErrUserNotFound, mirroring the real auth store's contract.
type fakeUserResolver map[string]auth.User

// GetUserByUID returns the user stored under uid, or auth.ErrUserNotFound when no
// entry exists (a deleted or never-created uploader).
func (f fakeUserResolver) GetUserByUID(_ context.Context, uid string) (auth.User, error) {
	user, ok := f[uid]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return user, nil
}

// errUserResolver is a UserResolver that always fails with a non-not-found error,
// exercising the branch where uploader resolution must surface a real failure.
type errUserResolver struct{}

// GetUserByUID always returns a transport-style error unrelated to a missing user.
func (errUserResolver) GetUserByUID(_ context.Context, _ string) (auth.User, error) {
	return auth.User{}, errors.New("database unavailable")
}

// TestResolveUploader covers the uploader-resolution helper: a resolved display
// name, the username fallback when the display name is empty, and the neutral
// (nil) fallbacks for a missing uploader, an unwired resolver and a deleted user,
// versus a real error for a genuine resolver failure.
func TestResolveUploader(t *testing.T) {
	t.Parallel()

	users := fakeUserResolver{
		"us_named":   {UID: "us_named", Username: "alice", DisplayName: "Alice Example"},
		"us_unnamed": {UID: "us_unnamed", Username: "bob"},
	}

	tests := []struct {
		name       string
		resolver   UserResolver
		uploadedBy *string
		want       *uploaderRef
		wantErr    bool
	}{
		{
			name:       "resolves display name",
			resolver:   users,
			uploadedBy: new("us_named"),
			want:       &uploaderRef{UID: "us_named", Name: "Alice Example"},
		},
		{
			name:       "falls back to username",
			resolver:   users,
			uploadedBy: new("us_unnamed"),
			want:       &uploaderRef{UID: "us_unnamed", Name: "bob"},
		},
		{
			name:       "nil uploader yields no reference",
			resolver:   users,
			uploadedBy: nil,
			want:       nil,
		},
		{
			name:       "empty uploader yields no reference",
			resolver:   users,
			uploadedBy: new(""),
			want:       nil,
		},
		{
			name:       "unwired resolver yields no reference",
			resolver:   nil,
			uploadedBy: new("us_named"),
			want:       nil,
		},
		{
			name:       "deleted uploader degrades to no reference",
			resolver:   users,
			uploadedBy: new("us_gone"),
			want:       nil,
		},
		{
			name:       "resolver failure surfaces an error",
			resolver:   errUserResolver{},
			uploadedBy: new("us_named"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &API{users: tt.resolver}
			got, err := api.resolveUploader(context.Background(), tt.uploadedBy)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveUploader() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveUploader() unexpected error = %v", err)
			}
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("resolveUploader() = %+v, want nil", got)
			case tt.want != nil && got == nil:
				t.Errorf("resolveUploader() = nil, want %+v", tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("resolveUploader() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
