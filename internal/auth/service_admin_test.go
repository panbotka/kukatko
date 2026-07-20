package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateUsername(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		wantErr  error
	}{
		{
			name:     "ordinary username is allowed",
			username: "alice",
			wantErr:  nil,
		},
		{
			name:     "username at the limit is allowed",
			username: strings.Repeat("a", MaxUsernameLen),
			wantErr:  nil,
		},
		{
			name:     "username one rune over the limit is rejected",
			username: strings.Repeat("a", MaxUsernameLen+1),
			wantErr:  ErrUsernameTooLong,
		},
		{
			// The DoS payload: a megabyte-scale username would otherwise become a
			// permanent rate-limiter key.
			name:     "megabyte username is rejected",
			username: strings.Repeat("a", 1<<20),
			wantErr:  ErrUsernameTooLong,
		},
		{
			// Each 'ř' is two bytes; the limit counts runes, not bytes.
			name:     "multi-byte username at the rune limit is allowed",
			username: strings.Repeat("ř", MaxUsernameLen),
			wantErr:  nil,
		},
		{
			name:     "multi-byte username over the rune limit is rejected",
			username: strings.Repeat("ř", MaxUsernameLen+1),
			wantErr:  ErrUsernameTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateUsername(tt.username)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("validateUsername(%d runes) error = %v, want %v",
					len([]rune(tt.username)), err, tt.wantErr)
			}
		})
	}
}

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
