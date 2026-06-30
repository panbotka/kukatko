//go:build integration

package photos_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// makeRatingUser inserts a viewer account so rating rows have a valid user FK.
func makeRatingUser(t *testing.T, store *auth.Store, uid string) string {
	t.Helper()
	if err := store.CreateUser(t.Context(), auth.User{
		UID:          uid,
		Username:     uid,
		PasswordHash: "x",
		Role:         auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", uid, err)
	}
	return uid
}

// TestList_ratingFiltersAndSort verifies the per-user MinRating and Flag filters
// and the rating sort on the shared List/Count path, including that a photo with
// no rating row counts as rating 0 / flag "none".
func TestList_ratingFiltersAndSort(t *testing.T) {
	store, db := newStore(t)
	org := organize.NewStore(db.Pool())
	users := auth.NewStore(db.Pool())
	ctx := t.Context()

	alice := makeRatingUser(t, users, "rat_alice")
	bob := makeRatingUser(t, users, "rat_bob")

	high := mustCreate(t, store, photos.Photo{
		FileHash: "r-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg", Title: "high",
	})
	low := mustCreate(t, store, photos.Photo{
		FileHash: "r-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg", Title: "low",
	})
	// unrated has no rating row, so it counts as rating 0 / flag none.
	unrated := mustCreate(t, store, photos.Photo{
		FileHash: "r-3", FilePath: "p/3.jpg", FileName: "3.jpg", FileMime: "image/jpeg", Title: "unrated",
	})

	if err := org.SetRating(ctx, alice, high.UID, 5); err != nil {
		t.Fatalf("SetRating high: %v", err)
	}
	if err := org.SetFlag(ctx, alice, high.UID, "pick"); err != nil {
		t.Fatalf("SetFlag high: %v", err)
	}
	if err := org.SetRating(ctx, alice, low.UID, 2); err != nil {
		t.Fatalf("SetRating low: %v", err)
	}
	if err := org.SetFlag(ctx, alice, low.UID, "reject"); err != nil {
		t.Fatalf("SetFlag low: %v", err)
	}

	t.Run("min rating keeps photos at or above the threshold", func(t *testing.T) {
		three := 3
		params := photos.ListParams{RatedBy: &alice, MinRating: &three}
		list, err := store.List(ctx, params)
		if err != nil {
			t.Fatalf("List(min rating): %v", err)
		}
		set := uidSet(list)
		if len(set) != 1 || !set[high.UID] {
			t.Fatalf("min rating 3 = %v, want only high", set)
		}
		total, err := store.Count(ctx, params)
		if err != nil || total != 1 {
			t.Fatalf("Count(min rating) = %d, %v, want 1", total, err)
		}
	})

	t.Run("min rating is per-user", func(t *testing.T) {
		one := 1
		// Bob has rated nothing, so even rating >= 1 matches nothing for him.
		list, err := store.List(ctx, photos.ListParams{RatedBy: &bob, MinRating: &one})
		if err != nil {
			t.Fatalf("List(bob min rating): %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("bob min rating 1 = %v, want none (isolation)", uidSet(list))
		}
	})

	t.Run("flag filter keeps only the flagged photos", func(t *testing.T) {
		pick := "pick"
		list, err := store.List(ctx, photos.ListParams{RatedBy: &alice, Flag: &pick})
		if err != nil {
			t.Fatalf("List(flag pick): %v", err)
		}
		set := uidSet(list)
		if len(set) != 1 || !set[high.UID] {
			t.Fatalf("flag pick = %v, want only high", set)
		}

		reject := "reject"
		list, err = store.List(ctx, photos.ListParams{RatedBy: &alice, Flag: &reject})
		if err != nil {
			t.Fatalf("List(flag reject): %v", err)
		}
		set = uidSet(list)
		if len(set) != 1 || !set[low.UID] {
			t.Fatalf("flag reject = %v, want only low", set)
		}
	})

	t.Run("rating sort orders by the user's rating with unrated last", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{
			RatedBy: &alice, Sort: photos.SortByRating, Order: photos.OrderDesc,
		})
		if err != nil {
			t.Fatalf("List(rating sort desc): %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("rating sort returned %d photos, want 3", len(list))
		}
		// high (5), low (2), then unrated (NULL, NULLS LAST).
		if list[0].UID != high.UID || list[1].UID != low.UID || list[2].UID != unrated.UID {
			t.Fatalf("rating sort order = [%s %s %s], want [high low unrated]",
				list[0].UID, list[1].UID, list[2].UID)
		}
	})

	t.Run("rating sort ascending keeps unrated last", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{
			RatedBy: &alice, Sort: photos.SortByRating, Order: photos.OrderAsc,
		})
		if err != nil {
			t.Fatalf("List(rating sort asc): %v", err)
		}
		// low (2), high (5), then unrated (NULL, NULLS LAST).
		if list[0].UID != low.UID || list[1].UID != high.UID || list[2].UID != unrated.UID {
			t.Fatalf("rating sort asc order = [%s %s %s], want [low high unrated]",
				list[0].UID, list[1].UID, list[2].UID)
		}
	})
}
