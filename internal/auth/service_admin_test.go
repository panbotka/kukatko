package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateNote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		note    string
		wantErr error
	}{
		{
			name:    "empty note is allowed",
			note:    "",
			wantErr: nil,
		},
		{
			name:    "short note is allowed",
			note:    "Jana from accounting; account kept for the 2026 audit.",
			wantErr: nil,
		},
		{
			name:    "note at the limit is allowed",
			note:    strings.Repeat("a", MaxNoteLen),
			wantErr: nil,
		},
		{
			name:    "note one rune over the limit is rejected",
			note:    strings.Repeat("a", MaxNoteLen+1),
			wantErr: ErrNoteTooLong,
		},
		{
			// Each 'ř' is two bytes, so this note is 2*MaxNoteLen bytes long. It
			// must still pass: the limit counts runes, not bytes.
			name:    "multi-byte note at the rune limit is allowed",
			note:    strings.Repeat("ř", MaxNoteLen),
			wantErr: nil,
		},
		{
			name:    "multi-byte note over the rune limit is rejected",
			note:    strings.Repeat("ř", MaxNoteLen+1),
			wantErr: ErrNoteTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateNote(tt.note)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("validateNote(%d runes) error = %v, want %v",
					len([]rune(tt.note)), err, tt.wantErr)
			}
		})
	}
}
