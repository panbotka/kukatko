//go:build integration

package hlsjob_test

import (
	"encoding/json"
	"testing"

	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/jobs"
)

// recordedIndex keys a listing by <photo_uid>/<rendition> so a test can assert
// what is in it without depending on the slice's order twice over.
func recordedIndex(recorded []hlsjob.Recorded) map[string]string {
	index := make(map[string]string, len(recorded))
	for _, rec := range recorded {
		index[rec.PhotoUID+"/"+rec.Rendition] = rec.FileHash
	}
	return index
}

// TestStore_listRecordedCarriesTheFileHash verifies the listing pairs every row
// with the hash naming its prefix in the store — without which nothing can ask
// the store whether the row's objects are still there — and orders it stably.
func TestStore_listRecordedCarriesTheFileHash(t *testing.T) {
	store, photo := storeHarness(t)
	ctx := t.Context()

	for _, name := range []string{"720p", "1080p"} {
		if _, err := store.Save(ctx, row(photo.UID, name, 1920, 1080)); err != nil {
			t.Fatalf("Save(%s): %v", name, err)
		}
	}

	recorded, err := store.ListRecorded(ctx)
	if err != nil {
		t.Fatalf("ListRecorded: %v", err)
	}
	if len(recorded) != 2 {
		t.Fatalf("ListRecorded returned %d rows, want 2", len(recorded))
	}
	index := recordedIndex(recorded)
	for _, name := range []string{"720p", "1080p"} {
		if got := index[photo.UID+"/"+name]; got != photo.FileHash {
			t.Errorf("ListRecorded hash for %s = %q, want %q", name, got, photo.FileHash)
		}
	}
	if recorded[0].Rendition != "1080p" {
		t.Errorf("ListRecorded order = %+v, want the renditions sorted by name", recorded)
	}
}

// TestStore_listRecordedEmptyLibrary verifies a library that has never been
// encoded yields an empty slice rather than an error: not encoded is a state.
func TestStore_listRecordedEmptyLibrary(t *testing.T) {
	store, _ := storeHarness(t)

	recorded, err := store.ListRecorded(t.Context())
	if err != nil {
		t.Fatalf("ListRecorded: %v", err)
	}
	if len(recorded) != 0 {
		t.Errorf("ListRecorded = %+v, want none", recorded)
	}
}

// TestStore_deleteWithdrawsThePromise verifies Delete removes exactly the named
// rendition, reports whether there was one, and is a no-op the second time — the
// shape a repair that may run twice needs.
func TestStore_deleteWithdrawsThePromise(t *testing.T) {
	store, photo := storeHarness(t)
	ctx := t.Context()

	for _, name := range []string{"720p", "1080p"} {
		if _, err := store.Save(ctx, row(photo.UID, name, 1920, 1080)); err != nil {
			t.Fatalf("Save(%s): %v", name, err)
		}
	}

	removed, err := store.Delete(ctx, photo.UID, "720p")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !removed {
		t.Error("Delete reported no row for a rendition that was recorded")
	}
	again, err := store.Delete(ctx, photo.UID, "720p")
	if err != nil {
		t.Fatalf("Delete (second): %v", err)
	}
	if again {
		t.Error("Delete reported a row the second time; the first one removed it")
	}
	kept, err := store.Has(ctx, photo.UID, "1080p")
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !kept {
		t.Error("Delete removed a rendition it was not asked about")
	}
}

// TestStore_encodingHashesFollowsTheQueue verifies the queue's side of the
// answer: a video whose encode is still queued is reported — its objects may be
// appearing in the store right now — and one whose job has finished is not.
func TestStore_encodingHashesFollowsTheQueue(t *testing.T) {
	db, store, photo := storeHarnessDB(t)
	ctx := t.Context()
	queue := jobs.NewStore(db.Pool())

	payload, err := json.Marshal(map[string]string{"photo_uid": photo.UID})
	if err != nil {
		t.Fatalf("marshalling the payload: %v", err)
	}
	if _, err := queue.Enqueue(ctx, jobs.TypeHLSTranscode, payload, jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	hashes, err := store.EncodingHashes(ctx)
	if err != nil {
		t.Fatalf("EncodingHashes: %v", err)
	}
	if len(hashes) != 1 || hashes[0] != photo.FileHash {
		t.Fatalf("EncodingHashes = %v, want [%s] for a queued encode", hashes, photo.FileHash)
	}

	claimed, err := queue.Claim(ctx, "worker-1", jobs.TypeHLSTranscode)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := queue.Complete(ctx, claimed.ID, "worker-1"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	done, err := store.EncodingHashes(ctx)
	if err != nil {
		t.Fatalf("EncodingHashes after completion: %v", err)
	}
	if len(done) != 0 {
		t.Errorf("EncodingHashes = %v after the job finished, want none", done)
	}
}

// TestStore_encodingHashesIgnoresOtherJobTypes verifies only the streaming
// encode counts: another job over the same video writes no segments, so it must
// not excuse an object from being an orphan.
func TestStore_encodingHashesIgnoresOtherJobTypes(t *testing.T) {
	db, store, photo := storeHarnessDB(t)
	ctx := t.Context()

	payload, err := json.Marshal(map[string]string{"photo_uid": photo.UID})
	if err != nil {
		t.Fatalf("marshalling the payload: %v", err)
	}
	queue := jobs.NewStore(db.Pool())
	if _, err := queue.Enqueue(ctx, jobs.TypeThumbnail, payload, jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	hashes, err := store.EncodingHashes(ctx)
	if err != nil {
		t.Fatalf("EncodingHashes: %v", err)
	}
	if len(hashes) != 0 {
		t.Errorf("EncodingHashes = %v for a thumbnail job, want none", hashes)
	}
}
