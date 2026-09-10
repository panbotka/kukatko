//go:build integration

package dupmerge_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/dupmerge"
	"github.com/panbotka/kukatko/internal/photos"
)

// seedMedia creates a minimal photo of the given media type and returns its UID.
func seedMedia(t *testing.T, store *photos.Store, hash string, kind photos.MediaType) string {
	t.Helper()
	taken := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	created, err := store.Create(t.Context(), photos.Photo{
		FileHash:   hash,
		FilePath:   "2023/06/" + hash + ".mp4",
		FileName:   hash + ".mp4",
		FileSize:   4321,
		FileMime:   "video/mp4",
		FileWidth:  1920,
		FileHeight: 1080,
		MediaType:  kind,
		TakenAt:    &taken,
		Exif:       json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create %s: %v", hash, err)
	}
	return created.UID
}

// TestMerge_refusesToArchiveAVideoForAStill is the guard on the resolve path.
// Detection no longer offers a group mixing a clip with a photograph, but the
// merge endpoint takes the membership from its caller, so a hand-written request
// can still ask for one. It must fail loudly and change nothing rather than
// archive two minutes of footage in favour of one frame of it.
func TestMerge_refusesToArchiveAVideoForAStill(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := t.Context()

	store := photos.NewStore(db.Pool())
	svc := dupmerge.NewService(db.Pool())

	still := seedMedia(t, store, "mk01", photos.MediaImage)
	clip := seedMedia(t, store, "mk02", photos.MediaVideo)

	in := dupmerge.Input{KeeperUID: still, MemberUIDs: []string{still, clip}}
	if _, err := svc.Preview(ctx, in); !errors.Is(err, dupmerge.ErrCrossKindGroup) {
		t.Errorf("Preview error = %v, want ErrCrossKindGroup", err)
	}
	if _, err := svc.Merge(ctx, in); !errors.Is(err, dupmerge.ErrCrossKindGroup) {
		t.Fatalf("Merge error = %v, want ErrCrossKindGroup", err)
	}

	survivor, err := store.GetByUID(ctx, clip)
	if err != nil {
		t.Fatalf("GetByUID(clip): %v", err)
	}
	if survivor.ArchivedAt != nil {
		t.Error("the refused merge archived the clip anyway")
	}
}

// TestMerge_refusesToArchiveAStillForAVideo covers the mirror case: the boundary
// is what makes the pair wrong, not which side of it the keeper is on.
func TestMerge_refusesToArchiveAStillForAVideo(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := t.Context()

	store := photos.NewStore(db.Pool())
	svc := dupmerge.NewService(db.Pool())

	clip := seedMedia(t, store, "mk03", photos.MediaVideo)
	still := seedMedia(t, store, "mk04", photos.MediaImage)

	_, err := svc.Merge(ctx, dupmerge.Input{KeeperUID: clip, MemberUIDs: []string{clip, still}})
	if !errors.Is(err, dupmerge.ErrCrossKindGroup) {
		t.Fatalf("Merge error = %v, want ErrCrossKindGroup", err)
	}
}

// TestMerge_twoVideosStillMerge keeps the case the guard is not about: two clips
// may legitimately be duplicates, and resolving that group must still work.
func TestMerge_twoVideosStillMerge(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := t.Context()

	store := photos.NewStore(db.Pool())
	svc := dupmerge.NewService(db.Pool())

	keeper := seedMedia(t, store, "mk05", photos.MediaVideo)
	copyUID := seedMedia(t, store, "mk06", photos.MediaVideo)

	res, err := svc.Merge(ctx, dupmerge.Input{KeeperUID: keeper, MemberUIDs: []string{keeper, copyUID}})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if res.Archived != 1 {
		t.Fatalf("result = %+v, want one archived copy", res)
	}
}
