package photoapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/photos"
)

// commentRef is the compact comment embedded in a task-scoped listing row: what
// a reviewer needs to read on one line — who said what, and when — without the
// thread's edit stamp or subject columns.
type commentRef struct {
	UID        string    `json:"uid"`
	Body       string    `json:"body"`
	AuthorUID  string    `json:"author_uid"`
	AuthorName string    `json:"author_name"`
	CreatedAt  time.Time `json:"created_at"`
}

// lastCommentField is the tri-state `last_comment` of a listing row: absent
// from the JSON (the zero value, outside a task scope), an explicit null (a
// task-scoped row nobody has commented on) or the comment itself. The three
// states are what lets a client tell "not asked" from "asked, and there is
// none" — a plain pointer with omitempty would collapse the first two.
type lastCommentField struct {
	// present is true once a task-scoped listing has resolved the field, whether
	// or not a comment was found.
	present bool
	comment *commentRef
}

// IsZero reports whether the field was never resolved, which is what makes the
// `omitzero` tag drop it from every non-task listing.
func (f lastCommentField) IsZero() bool { return !f.present }

// MarshalJSON renders the resolved field: null without a comment, the compact
// comment otherwise.
func (f lastCommentField) MarshalJSON() ([]byte, error) {
	if f.comment == nil {
		return []byte("null"), nil
	}
	return json.Marshal(f.comment)
}

// resolvedLastComment builds the field for a row whose thread has been read:
// nil means "no live comment", which still marks the field present.
func resolvedLastComment(c *comments.Comment) lastCommentField {
	if c == nil {
		return lastCommentField{present: true}
	}
	return lastCommentField{present: true, comment: &commentRef{
		UID: c.UID, Body: c.Body, AuthorUID: c.AuthorUID, AuthorName: c.AuthorName, CreatedAt: c.CreatedAt,
	}}
}

// taskScoped reports whether params narrow the listing to a task's frozen group
// — the one listing whose rows carry their newest comment.
func taskScoped(params photos.ListParams) bool {
	return len(params.TaskUIDs) > 0
}

// annotateLastComments stamps every view's newest live comment from one grouped
// query over the page, marking the field present on every row so a photo
// without a comment reads as null rather than vanishing. It is called only for
// a task-scoped listing (see taskScoped): the review ledger reads it per row,
// and no other page does. Without a comments backend every row is null, since
// there is no thread to read.
func (a *API) annotateLastComments(ctx context.Context, views []photoView) error {
	for i := range views {
		views[i].LastComment = resolvedLastComment(nil)
	}
	if a.comments == nil || len(views) == 0 {
		return nil
	}
	uids := make([]string, len(views))
	for i := range views {
		uids[i] = views[i].UID
	}
	latest, err := a.comments.LatestAmong(ctx, comments.SubjectPhoto, uids)
	if err != nil {
		return fmt.Errorf("photoapi: resolving last comments: %w", err)
	}
	for i := range views {
		if c, ok := latest[views[i].UID]; ok {
			views[i].LastComment = resolvedLastComment(&c)
		}
	}
	return nil
}
