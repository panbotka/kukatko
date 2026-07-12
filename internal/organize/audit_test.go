package organize

import (
	"errors"
	"testing"
)

// TestPrepareAlbumInsert checks type defaulting/validation, UID generation and
// slug derivation shared by the standalone and audited album creates.
func TestPrepareAlbumInsert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       Album
		wantType AlbumType
		wantSlug string
		wantErr  error
	}{
		{
			name:     "empty type defaults to manual and derives slug",
			in:       Album{Title: "Léto u Řeky"},
			wantType: AlbumManual,
			wantSlug: "leto-u-reky",
		},
		{
			name:     "explicit type is preserved",
			in:       Album{Title: "Trip", Type: AlbumFolder},
			wantType: AlbumFolder,
			wantSlug: "trip",
		},
		{
			name:    "invalid type is rejected",
			in:      Album{Title: "Trip", Type: AlbumType("mixtape")},
			wantErr: ErrInvalidType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, slug, err := prepareAlbumInsert(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.Type != tt.wantType {
				t.Errorf("type = %q, want %q", got.Type, tt.wantType)
			}
			if slug != tt.wantSlug {
				t.Errorf("slug = %q, want %q", slug, tt.wantSlug)
			}
			if got.UID == "" {
				t.Error("UID was not generated")
			}
		})
	}
}

// TestPrepareAlbumInsert_keepsGivenUID checks a caller-supplied UID is preserved
// rather than regenerated.
func TestPrepareAlbumInsert_keepsGivenUID(t *testing.T) {
	t.Parallel()
	got, _, err := prepareAlbumInsert(Album{Title: "Trip", UID: "al_fixed"})
	if err != nil {
		t.Fatalf("prepareAlbumInsert: %v", err)
	}
	if got.UID != "al_fixed" {
		t.Errorf("UID = %q, want al_fixed", got.UID)
	}
}

// TestPrepareAlbumUpdate checks type defaulting/validation and slug derivation for
// the album update paths.
func TestPrepareAlbumUpdate(t *testing.T) {
	t.Parallel()

	got, slug, err := prepareAlbumUpdate(AlbumUpdate{Title: "New Title"})
	if err != nil {
		t.Fatalf("prepareAlbumUpdate: %v", err)
	}
	if got.Type != AlbumManual || slug != "new-title" {
		t.Errorf("got type=%q slug=%q, want manual/new-title", got.Type, slug)
	}
	if _, _, err := prepareAlbumUpdate(AlbumUpdate{Title: "X", Type: AlbumType("bogus")}); !errors.Is(err, ErrInvalidType) {
		t.Errorf("err = %v, want ErrInvalidType", err)
	}
}

// TestPrepareLabelInsert checks UID generation and slug derivation for label
// creates.
func TestPrepareLabelInsert(t *testing.T) {
	t.Parallel()

	got, slug, err := prepareLabelInsert(Label{Name: "Beach Day"})
	if err != nil {
		t.Fatalf("prepareLabelInsert: %v", err)
	}
	if slug != "beach-day" {
		t.Errorf("slug = %q, want beach-day", slug)
	}
	if got.UID == "" {
		t.Error("UID was not generated")
	}
}

// TestNormalizeLabelSource checks defaulting an empty source to manual and
// rejecting an unknown one.
func TestNormalizeLabelSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      LabelSource
		want    LabelSource
		wantErr error
	}{
		{name: "empty defaults to manual", in: "", want: SourceManual},
		{name: "ai is kept", in: SourceAI, want: SourceAI},
		{name: "unknown is rejected", in: LabelSource("telepathy"), wantErr: ErrInvalidSource},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeLabelSource(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && got != tt.want {
				t.Errorf("source = %q, want %q", got, tt.want)
			}
		})
	}
}
