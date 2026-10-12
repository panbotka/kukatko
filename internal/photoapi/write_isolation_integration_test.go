//go:build integration

package photoapi_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/thumb"
)

// These are the HTTP-level tenant-isolation tests of the photo write side and
// of the media bytes: a member of B, writing through B's prefix at A's photo,
// gets exactly the answer a photo that does not exist gets, and A's photo is
// left exactly as it was.

// foreignWrite is one write route aimed at a photo: the method, the path
// suffix after "photos/<uid>" and the JSON body (nil for none).
type foreignWrite struct {
	name   string
	method string
	suffix string
	body   any
}

// foreignWrites lists every per-photo write the isolation test fires at A's
// photo. The bodies are valid, so the only reason to refuse is the project.
func foreignWrites() []foreignWrite {
	rating := 4
	return []foreignWrite{
		{"edit metadata", http.MethodPatch, "", map[string]any{"title": "overwritten from B"}},
		{"rate", http.MethodPut, "/rating", map[string]any{"rating": rating}},
		{"clear rating", http.MethodDelete, "/rating", nil},
		{"favourite", http.MethodPut, "/favorite", nil},
		{"unfavourite", http.MethodDelete, "/favorite", nil},
		{"archive", http.MethodPost, "/archive", nil},
		{"restore", http.MethodPost, "/unarchive", nil},
		{"hide", http.MethodPost, "/hide", nil},
		{"unhide", http.MethodPost, "/unhide", nil},
		{"photo edit", http.MethodPut, "/edit", map[string]any{"rotation": 90}},
		{"comment", http.MethodPost, "/comments", map[string]any{"body": "written from B"}},
	}
}

// TestPhotoWrites_foreignPhotoIsMissingAndUntouched fires every per-photo write
// at A's photo as B's member and asserts each is answered like a write at a
// photo that does not exist, then that A's photo changed in no way: its row,
// its edit, its comments, its ratings and its favourites are all as seeded.
func TestPhotoWrites_foreignPhotoIsMissingAndUntouched(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	client := f.ClientFor(t, e.server, projecttest.SideB)
	ctx := t.Context()

	before, err := e.store.GetByUIDInProject(ctx, f.A.ProjectUID, f.A.PhotoUID)
	if err != nil {
		t.Fatalf("loading A's photo: %v", err)
	}

	for _, w := range foreignWrites() {
		t.Run(w.name, func(t *testing.T) {
			foreign := f.Send(t, client, w.method, f.PathFor(projecttest.SideB, "photos/"+f.A.PhotoUID+w.suffix), w.body)
			if foreign.StatusCode != http.StatusNotFound {
				t.Errorf("%s %s on A's photo = %d, want 404", w.method, w.suffix, foreign.StatusCode)
			}
			missing := f.Send(t, client, w.method,
				f.PathFor(projecttest.SideB, "photos/"+projecttest.MissingUID("ph")+w.suffix), w.body)
			projecttest.AssertSameShape(t, foreign, missing)
		})
	}

	assertPhotoUntouched(t, e, before)
	assertNoSatellites(t, e, f.A.PhotoUID)
}

// TestPhotoWrites_ownPhotoStillWritable proves the gate does not refuse the
// caller's own project: the same member edits and favourites B's photo.
func TestPhotoWrites_ownPhotoStillWritable(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	client := f.ClientFor(t, e.server, projecttest.SideB)

	for _, w := range []foreignWrite{
		{"edit metadata", http.MethodPatch, "", map[string]any{"title": "B's own"}},
		{"favourite", http.MethodPut, "/favorite", nil},
		{"comment", http.MethodPost, "/comments", map[string]any{"body": "B on B"}},
	} {
		resp := f.Send(t, client, w.method, f.PathFor(projecttest.SideB, "photos/"+f.B.PhotoUID+w.suffix), w.body)
		_ = resp.Body.Close()
		if resp.StatusCode >= http.StatusBadRequest {
			t.Errorf("%s on B's own photo = %d, want success", w.name, resp.StatusCode)
		}
	}
}

// TestPhotoCommentWrites_foreignThreadIsMissing covers editing and deleting a
// comment through A's photo: the comment exists (A's member wrote it), but B's
// member addresses it through a photo of another project, which must answer as
// a missing photo and leave the comment in place.
func TestPhotoCommentWrites_foreignThreadIsMissing(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	ctx := t.Context()
	created, err := e.comments.Create(ctx, comments.PhotoSubject(f.A.PhotoUID), f.B.UserUID, "B wrote this on A",
		audit.Entry{ActorUID: f.B.UserUID, Action: audit.ActionCommentCreate,
			TargetType: "photos", TargetUID: f.A.PhotoUID})
	if err != nil {
		t.Fatalf("seeding a comment on A's photo: %v", err)
	}
	client := f.ClientFor(t, e.server, projecttest.SideB)

	for _, w := range []foreignWrite{
		{"edit comment", http.MethodPatch, "/comments/" + created.UID, map[string]any{"body": "rewritten"}},
		{"delete comment", http.MethodDelete, "/comments/" + created.UID, nil},
	} {
		t.Run(w.name, func(t *testing.T) {
			foreign := f.Send(t, client, w.method, f.PathFor(projecttest.SideB, "photos/"+f.A.PhotoUID+w.suffix), w.body)
			missing := f.Send(t, client, w.method,
				f.PathFor(projecttest.SideB, "photos/"+projecttest.MissingUID("ph")+w.suffix), w.body)
			if foreign.StatusCode != http.StatusNotFound {
				t.Errorf("%s through A's photo = %d, want 404", w.name, foreign.StatusCode)
			}
			projecttest.AssertSameShape(t, foreign, missing)
		})
	}

	// A deleted comment is no longer found, so a successful read is half the proof.
	got, err := e.comments.Get(ctx, created.UID)
	if err != nil {
		t.Fatalf("reading the comment back (deleted through B's prefix?): %v", err)
	}
	if got.Body != "B wrote this on A" || got.EditedAt != nil {
		t.Errorf("A's comment changed through B's prefix: %+v", got)
	}
}

// TestPhotoMedia_foreignPhotoServesNoBytes covers the media routes — a
// thumbnail, the original, the face crop, the video, the streaming playlist and
// the storyboard — each of which must look the photo up inside the project
// before serving a byte or minting a signed URL: A's photo answers exactly like
// a missing one.
func TestPhotoMedia_foreignPhotoServesNoBytes(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	client := f.ClientFor(t, e.server, projecttest.SideB)

	for _, suffix := range []string{
		"/thumb/" + thumb.GridSize, "/download", "/download?original=true", "/face?box=0.25,0.25,0.2,0.2",
		"/video", "/hls/master.m3u8", "/storyboard", "/storyboard/sprite",
	} {
		t.Run(suffix, func(t *testing.T) {
			foreign := f.PathFor(projecttest.SideB, "photos/"+f.A.PhotoUID+suffix)
			f.AssertInvisible(t, client, foreign)
			projecttest.AssertSameShape(t, f.Get(t, client, foreign),
				f.Get(t, client, f.PathFor(projecttest.SideB, "photos/"+projecttest.MissingUID("ph")+suffix)))
		})
	}
}

// assertPhotoUntouched fails t when the photo's row differs from before in any
// field a write could have changed, updated_at included.
func assertPhotoUntouched(t *testing.T, e *env, before photos.Photo) {
	t.Helper()
	after, err := e.store.GetByUIDInProject(t.Context(), before.ProjectID, before.UID)
	if err != nil {
		t.Fatalf("reloading the photo: %v", err)
	}
	if after.Title != before.Title || after.ArchivedAt != nil || after.HiddenFromLibrary != before.HiddenFromLibrary ||
		!after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("the other project's photo changed: before %+v, after %+v", before, after)
	}
}

// assertNoSatellites fails t when the photo gained an edit, a comment, a rating
// or a favourite.
func assertNoSatellites(t *testing.T, e *env, photoUID string) {
	t.Helper()
	ctx := t.Context()
	if _, err := e.store.GetEdit(ctx, photoUID); !errors.Is(err, photos.ErrEditNotFound) {
		t.Errorf("the other project's photo gained an edit (err %v)", err)
	}
	thread, err := e.comments.List(ctx, comments.PhotoSubject(photoUID))
	if err != nil {
		t.Fatalf("listing comments: %v", err)
	}
	if len(thread) != 0 {
		t.Errorf("the other project's photo gained %d comment(s)", len(thread))
	}
	for _, table := range []string{"user_ratings", "user_favorites"} {
		var n int
		if err := e.db.Pool().QueryRow(ctx,
			"SELECT count(*) FROM "+table+" WHERE photo_uid = $1", photoUID).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("the other project's photo gained %d row(s) in %s", n, table)
		}
	}
}
