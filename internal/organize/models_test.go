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

// TestTranslateUserPhotoFK checks the (user_uid, photo_uid) foreign-key error
// mapping shared by user_favorites and user_ratings writes.
func TestTranslateUserPhotoFK(t *testing.T) {
	t.Parallel()

	if err := translateUserPhotoFK(fkError("user_favorites_user_uid_fkey"), "x"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("favorites user fk = %v, want ErrUserNotFound", err)
	}
	if err := translateUserPhotoFK(fkError("user_favorites_photo_uid_fkey"), "x"); !errors.Is(err, ErrPhotoNotFound) {
		t.Errorf("favorites photo fk = %v, want ErrPhotoNotFound", err)
	}
	if err := translateUserPhotoFK(fkError("user_ratings_user_uid_fkey"), "x"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("ratings user fk = %v, want ErrUserNotFound", err)
	}
	if err := translateUserPhotoFK(fkError("user_ratings_photo_uid_fkey"), "x"); !errors.Is(err, ErrPhotoNotFound) {
		t.Errorf("ratings photo fk = %v, want ErrPhotoNotFound", err)
	}
}

// TestRatingFlagValid checks the recognised set of rating flags.
func TestRatingFlagValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flag RatingFlag
		want bool
	}{
		{flag: FlagNone, want: true},
		{flag: FlagPick, want: true},
		{flag: FlagReject, want: true},
		{flag: FlagEye, want: true},
		{flag: "", want: false},
		{flag: "star", want: false},
	}
	for _, tt := range tests {
		if got := tt.flag.valid(); got != tt.want {
			t.Errorf("RatingFlag(%q).valid() = %v, want %v", tt.flag, got, tt.want)
		}
	}
}

// fkError builds a foreign-key-violation PgError naming the given constraint, as
// the database would surface it.
func fkError(constraint string) error {
	return &pgconn.PgError{Code: foreignKeyViolation, ConstraintName: constraint}
}
