package announcement

import (
	"errors"
	"testing"
)

// TestNormalizeLevel checks defaulting, pass-through and rejection of levels.
func TestNormalizeLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "empty defaults to info", input: "", want: LevelInfo, wantErr: nil},
		{name: "info passes through", input: LevelInfo, want: LevelInfo, wantErr: nil},
		{name: "warning passes through", input: LevelWarning, want: LevelWarning, wantErr: nil},
		{name: "unknown is rejected", input: "danger", want: "", wantErr: ErrInvalidLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeLevel(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeLevel(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("normalizeLevel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
