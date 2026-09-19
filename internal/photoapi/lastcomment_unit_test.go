package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/photos"
)

// TestLastCommentField_marshal verifies the three states of the field: absent
// outside a task scope, null for a task-scoped row without a comment, and the
// compact comment otherwise.
func TestLastCommentField_marshal(t *testing.T) {
	t.Parallel()

	stamp := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		field lastCommentField
		want  string
	}{
		{name: "unresolved is omitted", field: lastCommentField{}, want: `"last_comment"`},
		{name: "resolved without a comment is null", field: resolvedLastComment(nil), want: `"last_comment":null`},
		{
			name: "resolved with a comment is the compact comment",
			field: resolvedLastComment(&comments.Comment{
				UID: "cm1", Body: "moved to 1974", AuthorUID: "us1", AuthorName: "Bot", CreatedAt: stamp,
				PhotoUID: "ph1", EditedAt: &stamp,
			}),
			want: `"last_comment":{"uid":"cm1","body":"moved to 1974","author_uid":"us1",` +
				`"author_name":"Bot","created_at":"2026-09-20T10:00:00Z"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			view := photoView{Photo: photos.Photo{UID: "ph1"}, LastComment: tt.field}
			encoded, err := json.Marshal(view)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got := string(encoded)
			if tt.field.IsZero() {
				if strings.Contains(got, tt.want) {
					t.Errorf("an unresolved field was written: %s", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("Marshal = %s, want it to contain %s", got, tt.want)
			}
		})
	}
}

// TestAnnotateLastComments verifies every row of a task-scoped page is marked
// resolved, the commented ones carry their newest comment, the lookup is one
// bulk call over the page, and a missing comments backend degrades to null.
func TestAnnotateLastComments(t *testing.T) {
	t.Parallel()

	views := func() []photoView {
		return []photoView{{Photo: photos.Photo{UID: "ph1"}}, {Photo: photos.Photo{UID: "ph2"}}}
	}
	t.Run("stamps the newest comment per row", func(t *testing.T) {
		t.Parallel()
		fake := &fakeComments{latest: map[string]comments.Comment{"ph2": {UID: "cm9", Body: "done"}}}
		api := &API{comments: fake}
		got := views()
		if err := api.annotateLastComments(context.Background(), got); err != nil {
			t.Fatalf("annotateLastComments: %v", err)
		}
		if strings.Join(fake.gotUIDs, ",") != "ph1,ph2" {
			t.Errorf("asked for %v, want the whole page in one call", fake.gotUIDs)
		}
		if got[0].LastComment.IsZero() || got[0].LastComment.comment != nil {
			t.Errorf("ph1 = %+v, want resolved and null", got[0].LastComment)
		}
		if c := got[1].LastComment.comment; c == nil || c.UID != "cm9" || c.Body != "done" {
			t.Errorf("ph2 = %+v, want comment cm9", got[1].LastComment)
		}
	})
	t.Run("no comments backend resolves every row to null", func(t *testing.T) {
		t.Parallel()
		api := &API{}
		got := views()
		if err := api.annotateLastComments(context.Background(), got); err != nil {
			t.Fatalf("annotateLastComments: %v", err)
		}
		for _, v := range got {
			if v.LastComment.IsZero() || v.LastComment.comment != nil {
				t.Errorf("%s = %+v, want resolved and null", v.UID, v.LastComment)
			}
		}
	})
	t.Run("a failing lookup is reported", func(t *testing.T) {
		t.Parallel()
		api := &API{comments: &fakeComments{err: errors.New("boom")}}
		if err := api.annotateLastComments(context.Background(), views()); err == nil {
			t.Error("annotateLastComments = nil error, want the lookup's failure")
		}
	})
	t.Run("an empty page asks nothing", func(t *testing.T) {
		t.Parallel()
		fake := &fakeComments{}
		api := &API{comments: fake}
		if err := api.annotateLastComments(context.Background(), nil); err != nil {
			t.Fatalf("annotateLastComments: %v", err)
		}
		if fake.gotUIDs != nil {
			t.Errorf("asked for %v on an empty page", fake.gotUIDs)
		}
	})
}

// TestTaskScoped verifies only a task scope switches the last-comment column on.
func TestTaskScoped(t *testing.T) {
	t.Parallel()

	if taskScoped(photos.ListParams{AlbumUIDs: []string{"al1"}}) {
		t.Error("an album scope counts as a task scope")
	}
	if !taskScoped(photos.ListParams{TaskUIDs: []string{"tk1"}}) {
		t.Error("a task scope is not recognised")
	}
}
