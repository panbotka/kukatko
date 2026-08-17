//go:build integration

package jobs_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover UnfinishedForPhoto, the single round
// trip behind the per-photo processing report.

// TestUnfinishedForPhoto_scopesToThePhotoAndDropsDoneJobs checks the two filters:
// another photo's work never leaks in, and completed work is left to its own
// evidence rather than reported from the queue.
func TestUnfinishedForPhoto_scopesToThePhotoAndDropsDoneJobs(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	mine, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p1"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeThumbnail, photoPayload(t, "p2"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("Enqueue for the other photo: %v", err)
	}
	// A completed job of a third type must not appear.
	done, err := store.Enqueue(ctx, jobs.TypeMetadata, photoPayload(t, "p1"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("Enqueue metadata: %v", err)
	}
	if _, err := store.Claim(ctx, "w1", jobs.TypeMetadata); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := store.Complete(ctx, done.ID, "w1"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	list, err := store.UnfinishedForPhoto(ctx, "p1")
	if err != nil {
		t.Fatalf("UnfinishedForPhoto: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d jobs, want 1: %+v", len(list), list)
	}
	if list[0].ID != mine.ID || list[0].Type != jobs.TypeImageEmbed {
		t.Errorf("got job %+v, want the queued image_embed %d", list[0], mine.ID)
	}
}

// TestUnfinishedForPhoto_keepsTheNewestPerType checks a job re-enqueued after an
// earlier one was dead-lettered speaks for its type, rather than the corpse
// behind it.
func TestUnfinishedForPhoto_keepsTheNewestPerType(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	first, err := store.Enqueue(ctx, jobs.TypeOCR, photoPayload(t, "p1"), jobs.EnqueueOptions{
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := store.Claim(ctx, "w1", jobs.TypeOCR); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := store.Fail(ctx, first.ID, "w1", errors.New("sidecar refused")); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	// The dead job frees the dedup key, so a fresh one can be scheduled.
	second, err := store.Enqueue(ctx, jobs.TypeOCR, photoPayload(t, "p1"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("re-enqueue: %v", err)
	}
	makeRunnable(t, db, second.ID)

	list, err := store.UnfinishedForPhoto(ctx, "p1")
	if err != nil {
		t.Fatalf("UnfinishedForPhoto: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d jobs, want 1 per type: %+v", len(list), list)
	}
	if list[0].ID != second.ID || list[0].State != jobs.StateQueued {
		t.Errorf("got job %+v, want the fresh queued %d", list[0], second.ID)
	}
}

// TestUnfinishedForPhoto_reportsEveryUnfinishedState checks the query keeps the
// rows the report reads its states from: queued, running and dead.
func TestUnfinishedForPhoto_reportsEveryUnfinishedState(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypeThumbnail, photoPayload(t, "p1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("Enqueue thumbnail: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("Enqueue image_embed: %v", err)
	}
	if _, err := store.Claim(ctx, "w1", jobs.TypeImageEmbed); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	dead, err := store.Enqueue(ctx, jobs.TypeFaceDetect, photoPayload(t, "p1"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("Enqueue face_detect: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		"UPDATE jobs SET state = 'dead', last_error = 'box offline' WHERE id = $1", dead.ID); err != nil {
		t.Fatalf("dead-lettering: %v", err)
	}

	list, err := store.UnfinishedForPhoto(ctx, "p1")
	if err != nil {
		t.Fatalf("UnfinishedForPhoto: %v", err)
	}
	byType := map[string]jobs.Job{}
	for _, job := range list {
		byType[job.Type] = job
	}
	if got := byType[jobs.TypeThumbnail].State; got != jobs.StateQueued {
		t.Errorf("thumbnail state = %q, want queued", got)
	}
	if got := byType[jobs.TypeImageEmbed].State; got != jobs.StateRunning {
		t.Errorf("image_embed state = %q, want running", got)
	}
	face := byType[jobs.TypeFaceDetect]
	if face.State != jobs.StateDead || face.LastError != "box offline" {
		t.Errorf("face_detect = %+v, want dead carrying its error", face)
	}
}

// TestUnfinishedForPhoto_unknownPhotoIsEmpty checks the absence of work is an
// empty list, not an error — a photo nothing is scheduled for is the normal case.
func TestUnfinishedForPhoto_unknownPhotoIsEmpty(t *testing.T) {
	store, _ := newStore(t)

	list, err := store.UnfinishedForPhoto(t.Context(), "nobody")
	if err != nil {
		t.Fatalf("UnfinishedForPhoto: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("got %d jobs, want none", len(list))
	}
}
