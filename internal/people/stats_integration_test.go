//go:build integration

package people_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// makeDatedPhoto inserts a photo captured at takenAt and returns its uid.
func makeDatedPhoto(t *testing.T, store *photos.Store, hash string, takenAt time.Time) string {
	t.Helper()
	created, err := store.Create(context.Background(), photos.Photo{
		FileHash:   hash,
		FilePath:   "2024/01/" + hash + ".jpg",
		FileName:   hash + ".jpg",
		FileWidth:  4000,
		FileHeight: 3000,
		TakenAt:    &takenAt,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// SubjectStats is read on the review game's answer path, so it has to agree with
// the people index and the subject's gallery about what counts as one of a
// person's photos: non-invalid markers on visible photos, counted per photo
// rather than per marker.

func TestSubjectStats_countsVisiblePhotosAndTheirYears(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	anna := makeSubject(t, store, "Anna")
	old := makeDatedPhoto(t, photoStore, "hash-1961", time.Date(1961, 4, 12, 9, 0, 0, 0, time.UTC))
	mid := makeDatedPhoto(t, photoStore, "hash-1990", time.Date(1990, 7, 1, 9, 0, 0, 0, time.UTC))
	undated := makePhoto(t, photoStore, "hash-undated")

	// Two markers on one photo: the count is per photo, not per face.
	box := [4]float64{0.1, 0.1, 0.2, 0.2}
	mkFace(t, store, old, anna.UID, box, 90, false)
	mkFace(t, store, old, anna.UID, box, 80, false)
	mkFace(t, store, mid, anna.UID, box, 90, false)
	mkFace(t, store, undated, anna.UID, box, 90, false)
	// An invalid marker is not one of the person's faces, here as everywhere else.
	mkFace(t, store, mid, anna.UID, box, 90, true)

	stats, err := store.SubjectStats(ctx, anna.UID)
	if err != nil {
		t.Fatalf("SubjectStats: %v", err)
	}
	if stats.UID != anna.UID || stats.Name != "Anna" {
		t.Errorf("stats = %+v, want the subject's own uid and name", stats)
	}
	if stats.PhotoCount != 3 {
		t.Errorf("photo count = %d, want 3 distinct photos (one carries two markers)",
			stats.PhotoCount)
	}
	if stats.OldestYear != 1961 || stats.NewestYear != 1990 {
		t.Errorf("years = %d–%d, want 1961–1990 (the undated photo has no year to span)",
			stats.OldestYear, stats.NewestYear)
	}
}

func TestSubjectStats_aPersonWithNoPhotosAndAMissingOne(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := context.Background()

	empty := makeSubject(t, store, "Nikdo")
	stats, err := store.SubjectStats(ctx, empty.UID)
	if err != nil {
		t.Fatalf("SubjectStats: %v", err)
	}
	if stats.PhotoCount != 0 || stats.OldestYear != 0 || stats.NewestYear != 0 {
		t.Errorf("stats = %+v, want zeroes for a subject with no photos", stats)
	}
	if _, err := store.SubjectStats(ctx, "nosuchsubject"); !errors.Is(err, people.ErrSubjectNotFound) {
		t.Errorf("SubjectStats of an unknown uid = %v, want ErrSubjectNotFound", err)
	}
}
