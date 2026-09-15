package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRateLimitExempt verifies which requests skip a rate limiter: only one
// authenticated by an API token carrying the unlimited flag. An unauthenticated
// request and a session-cookie principal are never exempt — not even an admin's,
// which is the whole point of hanging the flag on the credential rather than on
// the person.
func TestRateLimitExempt(t *testing.T) {
	t.Parallel()

	admin := User{UID: "us_admin", Role: RoleAdmin}
	viewer := User{UID: "us_viewer", Role: RoleViewer}

	tests := []struct {
		name      string
		principal *principal
		want      bool
	}{
		{name: "no principal at all", principal: nil, want: false},
		{
			name:      "admin session cookie",
			principal: &principal{user: admin, session: Session{Token: "sess"}},
			want:      false,
		},
		{
			name:      "plain api token",
			principal: &principal{user: admin},
			want:      false,
		},
		{
			name:      "unlimited api token of a viewer",
			principal: &principal{user: viewer, unlimited: true},
			want:      true,
		},
		{
			name:      "unlimited api token of an admin",
			principal: &principal{user: admin, unlimited: true},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/upload", nil)
			if tt.principal != nil {
				req = req.WithContext(withPrincipal(req.Context(), *tt.principal))
			}
			if got := RateLimitExempt(req); got != tt.want {
				t.Errorf("RateLimitExempt() = %v, want %v", got, tt.want)
			}
		})
	}
}
