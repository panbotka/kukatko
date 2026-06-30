//go:build integration

package organize_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/organize"
)

// TestRatingsUpsertDeleteAndIsolation exercises the per-user rating/flag upsert,
// the all-defaults pruning, GetRating and isolation between users.
func TestRatingsUpsertDeleteAndIsolation(t *testing.T) {
	store, photoStore, userStore, _ := newStores(t)
	ctx := t.Context()
	alice := makeUser(t, userStore, "urat_alice", "ralice")
	bob := makeUser(t, userStore, "urat_bob", "rbob")
	p1 := makePhoto(t, photoStore, "rat1")
	p2 := makePhoto(t, photoStore, "rat2")

	// A never-rated photo reads back as the zero value.
	if got, err := store.GetRating(ctx, alice, p1); err != nil || got.Rating != 0 || got.Flag != "none" {
		t.Fatalf("GetRating unrated = %+v, %v, want {0 none}", got, err)
	}

	// Setting a rating creates the row; the flag keeps its default.
	if err := store.SetRating(ctx, alice, p1, 4); err != nil {
		t.Fatalf("SetRating: %v", err)
	}
	if got, _ := store.GetRating(ctx, alice, p1); got.Rating != 4 || got.Flag != "none" {
		t.Fatalf("GetRating after SetRating = %+v, want {4 none}", got)
	}

	// Setting a flag preserves the existing rating (independent columns).
	if err := store.SetFlag(ctx, alice, p1, "pick"); err != nil {
		t.Fatalf("SetFlag: %v", err)
	}
	if got, _ := store.GetRating(ctx, alice, p1); got.Rating != 4 || got.Flag != "pick" {
		t.Fatalf("GetRating after SetFlag = %+v, want {4 pick}", got)
	}

	// Re-setting the same rating is idempotent.
	if err := store.SetRating(ctx, alice, p1, 4); err != nil {
		t.Fatalf("SetRating idempotent: %v", err)
	}

	// Clearing both back to defaults deletes the row.
	if err := store.SetRating(ctx, alice, p1, 0); err != nil {
		t.Fatalf("SetRating 0: %v", err)
	}
	if got, _ := store.GetRating(ctx, alice, p1); got.Rating != 0 || got.Flag != "pick" {
		t.Fatalf("rating 0 should keep the pick flag, got %+v", got)
	}
	if err := store.SetFlag(ctx, alice, p1, "none"); err != nil {
		t.Fatalf("SetFlag none: %v", err)
	}
	if got, _ := store.GetRating(ctx, alice, p1); got.Rating != 0 || got.Flag != "none" {
		t.Fatalf("GetRating after clearing = %+v, want {0 none}", got)
	}

	// Ratings are per-user: bob's view of p2 is independent of alice's.
	if err := store.SetRating(ctx, alice, p2, 5); err != nil {
		t.Fatalf("SetRating alice p2: %v", err)
	}
	if err := store.SetFlag(ctx, bob, p2, "reject"); err != nil {
		t.Fatalf("SetFlag bob p2: %v", err)
	}
	if got, _ := store.GetRating(ctx, alice, p2); got.Rating != 5 || got.Flag != "none" {
		t.Errorf("alice p2 = %+v, want {5 none}", got)
	}
	if got, _ := store.GetRating(ctx, bob, p2); got.Rating != 0 || got.Flag != "reject" {
		t.Errorf("bob p2 = %+v, want {0 reject} (isolation)", got)
	}
}

// TestRatingsAmong verifies the page-annotation helper resolves only rated photos
// and stays per-user.
func TestRatingsAmong(t *testing.T) {
	store, photoStore, userStore, _ := newStores(t)
	ctx := t.Context()
	alice := makeUser(t, userStore, "ramong_alice", "amalice")
	bob := makeUser(t, userStore, "ramong_bob", "ambob")
	p1 := makePhoto(t, photoStore, "ramong1")
	p2 := makePhoto(t, photoStore, "ramong2")
	p3 := makePhoto(t, photoStore, "ramong3")

	if err := store.SetRating(ctx, alice, p1, 3); err != nil {
		t.Fatalf("SetRating p1: %v", err)
	}
	if err := store.SetFlag(ctx, alice, p2, "pick"); err != nil {
		t.Fatalf("SetFlag p2: %v", err)
	}
	if err := store.SetRating(ctx, bob, p1, 1); err != nil {
		t.Fatalf("SetRating bob p1: %v", err)
	}

	aliceSet, err := store.RatingsAmong(ctx, alice, []string{p1, p2, p3})
	if err != nil {
		t.Fatalf("RatingsAmong: %v", err)
	}
	if len(aliceSet) != 2 {
		t.Fatalf("alice RatingsAmong = %v, want only p1 and p2", aliceSet)
	}
	if aliceSet[p1] != (organize.PhotoRating{Rating: 3, Flag: "none"}) {
		t.Errorf("alice p1 = %+v, want {3 none}", aliceSet[p1])
	}
	if aliceSet[p2] != (organize.PhotoRating{Rating: 0, Flag: "pick"}) {
		t.Errorf("alice p2 = %+v, want {0 pick}", aliceSet[p2])
	}
	if _, ok := aliceSet[p3]; ok {
		t.Errorf("p3 should be absent (never rated), got %+v", aliceSet[p3])
	}

	bobSet, _ := store.RatingsAmong(ctx, bob, []string{p1, p2})
	if len(bobSet) != 1 || bobSet[p1].Rating != 1 {
		t.Errorf("bob RatingsAmong = %v, want only p1 rating 1 (isolation)", bobSet)
	}
	if empty, _ := store.RatingsAmong(ctx, alice, nil); len(empty) != 0 {
		t.Errorf("RatingsAmong(nil) = %v, want empty", empty)
	}
}

// TestClearRating verifies the rating clear drops both columns, is idempotent on
// an unrated or missing photo, and stays per-user.
func TestClearRating(t *testing.T) {
	store, photoStore, userStore, _ := newStores(t)
	ctx := t.Context()
	alice := makeUser(t, userStore, "rclear_alice", "rcalice")
	bob := makeUser(t, userStore, "rclear_bob", "rcbob")
	photoUID := makePhoto(t, photoStore, "rclear")

	if err := store.SetRating(ctx, alice, photoUID, 4); err != nil {
		t.Fatalf("SetRating: %v", err)
	}
	if err := store.SetFlag(ctx, alice, photoUID, "pick"); err != nil {
		t.Fatalf("SetFlag: %v", err)
	}
	if err := store.SetFlag(ctx, bob, photoUID, "reject"); err != nil {
		t.Fatalf("SetFlag bob: %v", err)
	}

	if err := store.ClearRating(ctx, alice, photoUID); err != nil {
		t.Fatalf("ClearRating: %v", err)
	}
	if got, _ := store.GetRating(ctx, alice, photoUID); got.Rating != 0 || got.Flag != "none" {
		t.Errorf("alice after clear = %+v, want {0 none}", got)
	}
	// Bob's rating is untouched (per-user).
	if got, _ := store.GetRating(ctx, bob, photoUID); got.Rating != 0 || got.Flag != "reject" {
		t.Errorf("bob after alice clear = %+v, want {0 reject} (isolation)", got)
	}
	// Clearing again, and clearing an unrated/missing photo, are no-op successes.
	if err := store.ClearRating(ctx, alice, photoUID); err != nil {
		t.Errorf("ClearRating idempotent: %v", err)
	}
	if err := store.ClearRating(ctx, alice, "phmissing"); err != nil {
		t.Errorf("ClearRating missing photo = %v, want nil (idempotent)", err)
	}
}

// TestRatingsValidationAndMissing checks input validation and the not-found
// mappings for rating writes.
func TestRatingsValidationAndMissing(t *testing.T) {
	store, photoStore, userStore, _ := newStores(t)
	ctx := t.Context()
	user := makeUser(t, userStore, "rmiss_u", "rmiss")
	photoUID := makePhoto(t, photoStore, "rmiss")

	if err := store.SetRating(ctx, user, photoUID, 6); !errors.Is(err, organize.ErrInvalidRating) {
		t.Errorf("SetRating(6) = %v, want ErrInvalidRating", err)
	}
	if err := store.SetRating(ctx, user, photoUID, -1); !errors.Is(err, organize.ErrInvalidRating) {
		t.Errorf("SetRating(-1) = %v, want ErrInvalidRating", err)
	}
	if err := store.SetFlag(ctx, user, photoUID, "star"); !errors.Is(err, organize.ErrInvalidFlag) {
		t.Errorf("SetFlag(star) = %v, want ErrInvalidFlag", err)
	}

	if err := store.SetRating(ctx, "usermissing", photoUID, 3); !errors.Is(err, organize.ErrUserNotFound) {
		t.Errorf("SetRating missing user = %v, want ErrUserNotFound", err)
	}
	if err := store.SetFlag(ctx, user, "phmissing", "pick"); !errors.Is(err, organize.ErrPhotoNotFound) {
		t.Errorf("SetFlag missing photo = %v, want ErrPhotoNotFound", err)
	}
}
