package organize

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestAlbumTypeValid checks the recognised set of album types.
func TestAlbumTypeValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typ  AlbumType
		want bool
	}{
		{typ: AlbumManual, want: true},
		{typ: AlbumFolder, want: true},
		{typ: AlbumMoment, want: true},
		{typ: AlbumState, want: true},
		{typ: AlbumMonth, want: true},
		{typ: "", want: false},
		{typ: "playlist", want: false},
	}
	for _, tt := range tests {
		if got := tt.typ.valid(); got != tt.want {
			t.Errorf("AlbumType(%q).valid() = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

// TestLabelSourceValid checks the recognised set of label sources.
func TestLabelSourceValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  LabelSource
		want bool
	}{
		{src: SourceManual, want: true},
		{src: SourceAI, want: true},
		{src: SourceImport, want: true},
		{src: "", want: false},
		{src: "guess", want: false},
	}
	for _, tt := range tests {
		if got := tt.src.valid(); got != tt.want {
			t.Errorf("LabelSource(%q).valid() = %v, want %v", tt.src, got, tt.want)
		}
	}
}

// TestTranslateMembershipFK checks the album_photos foreign-key error mapping.
func TestTranslateMembershipFK(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		constraint string
		want       error
	}{
		{name: "album fk", constraint: "album_photos_album_uid_fkey", want: ErrAlbumNotFound},
		{name: "photo fk", constraint: "album_photos_photo_uid_fkey", want: ErrPhotoNotFound},
	}
	for _, tt := range tests {
		err := translateMembershipFK(fkError(tt.constraint))
		if !errors.Is(err, tt.want) {
			t.Errorf("translateMembershipFK(%q) = %v, want %v", tt.constraint, err, tt.want)
		}
	}
}

// TestTranslateAttachFK checks the photo_labels foreign-key error mapping.
func TestTranslateAttachFK(t *testing.T) {
	t.Parallel()

	if err := translateAttachFK(fkError("photo_labels_label_uid_fkey")); !errors.Is(err, ErrLabelNotFound) {
		t.Errorf("label fk = %v, want ErrLabelNotFound", err)
	}
	if err := translateAttachFK(fkError("photo_labels_photo_uid_fkey")); !errors.Is(err, ErrPhotoNotFound) {
		t.Errorf("photo fk = %v, want ErrPhotoNotFound", err)
	}
}

// TestTranslateFavoriteFK checks the user_favorites foreign-key error mapping.
func TestTranslateFavoriteFK(t *testing.T) {
	t.Parallel()

	if err := translateFavoriteFK(fkError("user_favorites_user_uid_fkey")); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("user fk = %v, want ErrUserNotFound", err)
	}
	if err := translateFavoriteFK(fkError("user_favorites_photo_uid_fkey")); !errors.Is(err, ErrPhotoNotFound) {
		t.Errorf("photo fk = %v, want ErrPhotoNotFound", err)
	}
}

// fkError builds a foreign-key-violation PgError naming the given constraint, as
// the database would surface it.
func fkError(constraint string) error {
	return &pgconn.PgError{Code: foreignKeyViolation, ConstraintName: constraint}
}
