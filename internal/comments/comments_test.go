package comments

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/panbotka/kukatko/internal/audit"
)

// TestNormalizeBody verifies the shared body validation: surrounding whitespace
// is trimmed, a blank body is rejected, and the length limit counts characters
// rather than bytes (so a Czech comment gets the same allowance as an English one).
func TestNormalizeBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr error
	}{
		{name: "plain body", body: "Kdo je ten kluk vlevo?", want: "Kdo je ten kluk vlevo?"},
		{name: "surrounding whitespace trimmed", body: "  hello \n", want: "hello"},
		{name: "inner whitespace kept", body: "two\n\nlines", want: "two\n\nlines"},
		{name: "empty", body: "", wantErr: ErrEmptyBody},
		{name: "whitespace only", body: " \t\n ", wantErr: ErrEmptyBody},
		{name: "at the limit", body: strings.Repeat("a", MaxBodyLen), want: strings.Repeat("a", MaxBodyLen)},
		{
			name: "multibyte at the limit counts characters, not bytes",
			body: strings.Repeat("ě", MaxBodyLen),
			want: strings.Repeat("ě", MaxBodyLen),
		},
		{name: "one over the limit", body: strings.Repeat("a", MaxBodyLen+1), wantErr: ErrBodyTooLong},
		{
			name:    "trimmed length is what counts",
			body:    "  " + strings.Repeat("a", MaxBodyLen) + "  ",
			want:    strings.Repeat("a", MaxBodyLen),
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeBody(tt.body)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeBody(%q) error = %v, want %v", tt.body, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("normalizeBody(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

// TestNewCommentUID verifies generated UIDs carry the comment prefix, fit the
// VARCHAR(32) column, use only the base32 alphabet and do not repeat.
func TestNewCommentUID(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 100)
	for range 100 {
		uid, err := newCommentUID()
		if err != nil {
			t.Fatalf("newCommentUID: %v", err)
		}
		if !strings.HasPrefix(uid, commentUIDPrefix) {
			t.Fatalf("uid %q does not start with %q", uid, commentUIDPrefix)
		}
		if len(uid) != len(commentUIDPrefix)+uidSuffixLen || len(uid) > uidMaxLen {
			t.Fatalf("uid %q has length %d, want %d (max %d)",
				uid, len(uid), len(commentUIDPrefix)+uidSuffixLen, uidMaxLen)
		}
		if strings.ContainsFunc(uid[len(commentUIDPrefix):], func(r rune) bool {
			return !strings.ContainsRune(uidAlphabet, r)
		}) {
			t.Fatalf("uid %q has characters outside the alphabet", uid)
		}
		if seen[uid] {
			t.Fatalf("uid %q generated twice", uid)
		}
		seen[uid] = true
	}
}

// TestEntryWithComment verifies the audit entry is stamped with the comment UID
// without mutating the caller's details map — the caller may reuse it.
func TestEntryWithComment(t *testing.T) {
	t.Parallel()

	t.Run("adds the uid to a copy of the details", func(t *testing.T) {
		t.Parallel()
		original := map[string]any{"via": "test"}
		entry := audit.Entry{Action: audit.ActionCommentCreate, Details: original}

		got := entryWithComment(entry, "cm_1")
		if got.Details["comment_uid"] != "cm_1" {
			t.Errorf("details[comment_uid] = %v, want cm_1", got.Details["comment_uid"])
		}
		if got.Details["via"] != "test" {
			t.Errorf("details[via] = %v, want test (existing keys kept)", got.Details["via"])
		}
		if _, ok := original["comment_uid"]; ok {
			t.Error("the caller's details map was mutated")
		}
	})

	t.Run("nil details still yields the uid", func(t *testing.T) {
		t.Parallel()
		got := entryWithComment(audit.Entry{Action: audit.ActionCommentDelete}, "cm_2")
		if got.Details["comment_uid"] != "cm_2" {
			t.Errorf("details[comment_uid] = %v, want cm_2", got.Details["comment_uid"])
		}
	})
}

// fkError builds a PostgreSQL foreign-key violation naming the given constraint.
func fkError(constraint string) error {
	return &pgconn.PgError{Code: foreignKeyViolation, ConstraintName: constraint}
}

// TestTranslateMutation verifies the error classification: no row changed means
// the comment is gone, a photo_uid foreign-key violation means the photo is, and
// anything else keeps its cause with the operation for context.
func TestTranslateMutation(t *testing.T) {
	t.Parallel()

	other := errors.New("boom")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "no rows means the comment is gone", err: pgx.ErrNoRows, want: ErrNotFound},
		{
			name: "photo foreign key means the photo is gone",
			err:  fkError("photo_comments_photo_uid_fkey"),
			want: ErrPhotoNotFound,
		},
		{
			name: "another foreign key is not reported as a missing photo",
			err:  fkError("photo_comments_author_uid_fkey"),
			want: nil,
		},
		{name: "anything else is wrapped", err: other, want: other},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := translateMutation(tt.err, "testing")
			if tt.want != nil && !errors.Is(got, tt.want) {
				t.Fatalf("translateMutation(%v) = %v, want %v", tt.err, got, tt.want)
			}
			if tt.want == nil {
				if errors.Is(got, ErrPhotoNotFound) || errors.Is(got, ErrNotFound) {
					t.Fatalf("translateMutation(%v) = %v, want a wrapped error", tt.err, got)
				}
				if !strings.Contains(got.Error(), "testing") {
					t.Errorf("wrapped error %q does not mention the operation", got)
				}
			}
		})
	}
}

// TestNullableUID verifies an empty UID becomes SQL NULL and a real one is passed
// through unchanged.
func TestNullableUID(t *testing.T) {
	t.Parallel()

	if got := nullableUID(""); got != nil {
		t.Errorf("nullableUID(\"\") = %v, want nil", got)
	}
	if got := nullableUID("us_1"); got != "us_1" {
		t.Errorf("nullableUID(us_1) = %v, want us_1", got)
	}
}
