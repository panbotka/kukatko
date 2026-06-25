package version

import "testing"

// TestGet_returnsCurrentVars verifies that Get reflects the package-level
// Version and Commit variables.
func TestGet_returnsCurrentVars(t *testing.T) {
	t.Parallel()

	got := Get()
	if got.Version != Version {
		t.Errorf("Get().Version = %q, want %q", got.Version, Version)
	}
	if got.Commit != Commit {
		t.Errorf("Get().Commit = %q, want %q", got.Commit, Commit)
	}
}

// TestInfo_String checks the human-readable formatting of build information.
func TestInfo_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info Info
		want string
	}{
		{
			name: "dev build",
			info: Info{Version: "dev", Commit: "none"},
			want: "dev (none)",
		},
		{
			name: "released build",
			info: Info{Version: "1.2.3", Commit: "abc1234"},
			want: "1.2.3 (abc1234)",
		},
		{
			name: "empty fields",
			info: Info{},
			want: " ()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.info.String(); got != tt.want {
				t.Errorf("Info.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
