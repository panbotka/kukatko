package comments

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
)

// TestSubjectValid verifies a subject is accepted only when it names a known
// kind and carries a uid — the two halves that make a thread addressable.
func TestSubjectValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		subj Subject
		want bool
	}{
		{name: "photo with uid", subj: PhotoSubject("ph1"), want: true},
		{name: "task with uid", subj: TaskSubject("tk1"), want: true},
		{name: "photo without uid", subj: Subject{Kind: SubjectPhoto}, want: false},
		{name: "unknown kind", subj: Subject{Kind: "album", UID: "al1"}, want: false},
		{name: "zero value", subj: Subject{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.subj.Valid(); got != tt.want {
				t.Errorf("Subject%+v.Valid() = %v, want %v", tt.subj, got, tt.want)
			}
		})
	}
}

// TestSubjectConstructors verifies the two helpers build the subject they name.
func TestSubjectConstructors(t *testing.T) {
	t.Parallel()

	if got := PhotoSubject("ph1"); got != (Subject{Kind: SubjectPhoto, UID: "ph1"}) {
		t.Errorf("PhotoSubject(ph1) = %+v", got)
	}
	if got := TaskSubject("tk1"); got != (Subject{Kind: SubjectTask, UID: "tk1"}) {
		t.Errorf("TaskSubject(tk1) = %+v", got)
	}
}

// TestCommentSubject verifies a stored comment reports the one subject it hangs
// off, whichever of the two uid columns carried it.
func TestCommentSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		c    Comment
		want Subject
	}{
		{name: "photo comment", c: Comment{PhotoUID: "ph1"}, want: PhotoSubject("ph1")},
		{name: "task comment", c: Comment{TaskUID: "tk1"}, want: TaskSubject("tk1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.c.Subject(); got != tt.want {
				t.Errorf("Comment%+v.Subject() = %+v, want %+v", tt.c, got, tt.want)
			}
		})
	}
}

// TestStatementsFor verifies each known kind is served by statements written
// against its own column, and that an unknown one is refused rather than guessed.
func TestStatementsFor(t *testing.T) {
	t.Parallel()

	photo, err := statementsFor(PhotoSubject("ph1"))
	if err != nil {
		t.Fatalf("statementsFor(photo): %v", err)
	}
	task, err := statementsFor(TaskSubject("tk1"))
	if err != nil {
		t.Fatalf("statementsFor(task): %v", err)
	}
	for _, tt := range []struct {
		name string
		sql  string
		want string
		deny string
	}{
		{name: "photo list", sql: photo.list, want: "c.photo_uid = $1", deny: "c.task_uid = $1"},
		{name: "task list", sql: task.list, want: "c.task_uid = $1", deny: "c.photo_uid = $1"},
		{name: "photo counts", sql: photo.countsAmong, want: "photo_uid = ANY($1)", deny: "task_uid = ANY"},
		{name: "task counts", sql: task.countsAmong, want: "task_uid = ANY($1)", deny: "photo_uid = ANY"},
		{name: "photo create", sql: photo.create, want: "(uid, photo_uid, author_uid, body)", deny: ""},
		{name: "task create", sql: task.create, want: "(uid, task_uid, author_uid, body)", deny: ""},
	} {
		if !strings.Contains(tt.sql, tt.want) {
			t.Errorf("%s statement does not contain %q:\n%s", tt.name, tt.want, tt.sql)
		}
		if tt.deny != "" && strings.Contains(tt.sql, tt.deny) {
			t.Errorf("%s statement unexpectedly contains %q", tt.name, tt.deny)
		}
	}

	for _, subj := range []Subject{{}, {Kind: "album", UID: "al1"}, {Kind: SubjectPhoto}} {
		if _, err := statementsFor(subj); !errors.Is(err, ErrInvalidSubject) {
			t.Errorf("statementsFor(%+v) error = %v, want ErrInvalidSubject", subj, err)
		}
	}
}

// TestStoreRefusesInvalidSubject verifies every subject-taking entry point
// rejects an unknown subject before it reaches the database — the store here has
// no pool at all, so a query would panic rather than merely fail.
func TestStoreRefusesInvalidSubject(t *testing.T) {
	t.Parallel()

	store := &Store{}
	bad := Subject{Kind: "album", UID: "al1"}
	ctx := context.Background()

	if _, err := store.List(ctx, bad); !errors.Is(err, ErrInvalidSubject) {
		t.Errorf("List(bad) error = %v, want ErrInvalidSubject", err)
	}
	if _, err := store.CountsAmong(ctx, "album", []string{"al1"}); !errors.Is(err, ErrInvalidSubject) {
		t.Errorf("CountsAmong(bad) error = %v, want ErrInvalidSubject", err)
	}
	if _, err := store.Create(ctx, bad, "us1", "hello", audit.Entry{}); !errors.Is(err, ErrInvalidSubject) {
		t.Errorf("Create(bad) error = %v, want ErrInvalidSubject", err)
	}
	// An empty input short-circuits before the kind is ever looked at, which is
	// what lets a caller pass an empty page without special-casing it.
	if got, err := store.CountsAmong(ctx, "album", nil); err != nil || len(got) != 0 {
		t.Errorf("CountsAmong(bad, nil) = %v, %v, want an empty map and no error", got, err)
	}
}
