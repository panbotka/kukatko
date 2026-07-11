package photoapi

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
)

// fakeUserResolver is a controllable UserResolver for resolveUploader unit tests:
// it returns the configured user for a matching UID and auth.ErrUserNotFound
// otherwise, recording the UID it was asked for.
type fakeUserResolver struct {
	byUID  map[string]auth.User
	err    error
	gotUID string
}

// GetUserByUID records uid and returns the configured user, the configured error,
// or auth.ErrUserNotFound when no user matches.
func (f *fakeUserResolver) GetUserByUID(_ context.Context, uid string) (auth.User, error) {
	f.gotUID = uid
	if f.err != nil {
		return auth.User{}, f.err
	}
	user, ok := f.byUID[uid]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return user, nil
}

// TestResolveUploader checks that a photo's uploader UID resolves to a compact
// reference with a human-readable name (display name, or username when the
// display name is empty), and that every unresolvable case — no uploader, no
// resolver, an unknown user, or a lookup error — omits the uploader (nil) so the
// detail response falls back to a neutral placeholder rather than failing.
func TestResolveUploader(t *testing.T) {
	t.Parallel()

	users := map[string]auth.User{
		"us_named": {UID: "us_named", Username: "alice", DisplayName: "Alice Example"},
		"us_bare":  {UID: "us_bare", Username: "bob", DisplayName: ""},
	}

	tests := []struct {
		name       string
		resolver   UserResolver
		uploadedBy *string
		want       *uploaderRef
	}{
		{
			name:       "resolves the display name",
			resolver:   &fakeUserResolver{byUID: users},
			uploadedBy: new("us_named"),
			want:       &uploaderRef{UID: "us_named", Name: "Alice Example"},
		},
		{
			name:       "falls back to username when display name is empty",
			resolver:   &fakeUserResolver{byUID: users},
			uploadedBy: new("us_bare"),
			want:       &uploaderRef{UID: "us_bare", Name: "bob"},
		},
		{
			name:       "nil uploader yields no reference",
			resolver:   &fakeUserResolver{byUID: users},
			uploadedBy: nil,
			want:       nil,
		},
		{
			name:       "empty uploader yields no reference",
			resolver:   &fakeUserResolver{byUID: users},
			uploadedBy: new(""),
			want:       nil,
		},
		{
			name:       "nil resolver yields no reference",
			resolver:   nil,
			uploadedBy: new("us_named"),
			want:       nil,
		},
		{
			name:       "unknown user yields no reference",
			resolver:   &fakeUserResolver{byUID: users},
			uploadedBy: new("us_ghost"),
			want:       nil,
		},
		{
			name:       "lookup error yields no reference",
			resolver:   &fakeUserResolver{err: errors.New("db down")},
			uploadedBy: new("us_named"),
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &API{users: tt.resolver}
			got := api.resolveUploader(context.Background(), tt.uploadedBy)
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("resolveUploader = %+v, want nil", got)
			case tt.want != nil && got == nil:
				t.Fatalf("resolveUploader = nil, want %+v", tt.want)
			case tt.want != nil && (got.UID != tt.want.UID || got.Name != tt.want.Name):
				t.Errorf("resolveUploader = %+v, want %+v", got, tt.want)
			}
		})
	}
}
