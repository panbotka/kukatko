//go:build integration

package review_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/review"
)

// The breather pick is one query with three jobs: only show photos somebody
// liked, only show photos the library would show anyway, and show at most one
// per era so a session's cards do not all come from one wedding.

// seedPhoto inserts a photo taken in the given year and returns its uid; hidden
// keeps it out of the library the way the photo's own page does.
func seedPhoto(t *testing.T, store *photos.Store, hash string, year int, hidden bool) string {
	t.Helper()
	taken := time.Date(year, 6, 1, 12, 0, 0, 0, time.UTC)
	created, err := store.Create(context.Background(), photos.Photo{
		FileHash:          hash,
		FilePath:          "2024/01/" + hash + ".jpg",
		FileName:          hash + ".jpg",
		TakenAt:           &taken,
		HiddenFromLibrary: hidden,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// seedRating writes one user's star rating for a photo.
func seedRating(t *testing.T, pool *pgxpool.Pool, userUID, photoUID string, rating int) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO user_ratings (user_uid, photo_uid, rating) VALUES ($1, $2, $3)`,
		userUID, photoUID, rating)
	if err != nil {
		t.Fatalf("seedRating(%s): %v", photoUID, err)
	}
}

// seedFavorite marks a photo as one user's favourite.
func seedFavorite(t *testing.T, pool *pgxpool.Pool, userUID, photoUID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO user_favorites (user_uid, photo_uid) VALUES ($1, $2)`, userUID, photoUID)
	if err != nil {
		t.Fatalf("seedFavorite(%s): %v", photoUID, err)
	}
}

func TestPickBreathers_oneLikedPhotoPerEraNewestFirst(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	photoStore := photos.NewStore(pool)
	seedUser(t, pool, "anna", "anna", "Anna")
	seedUser(t, pool, "bara", "bara", "Bára")

	// Two liked photos in the 2010s: the higher-rated one represents the era.
	good2015 := seedPhoto(t, photoStore, "hash-2015-good", 2015, false)
	ok2017 := seedPhoto(t, photoStore, "hash-2017-ok", 2017, false)
	fave1998 := seedPhoto(t, photoStore, "hash-1998", 1998, false)
	dull2016 := seedPhoto(t, photoStore, "hash-2016-dull", 2016, false)
	hidden2019 := seedPhoto(t, photoStore, "hash-2019-hidden", 2019, true)
	seedRating(t, pool, "anna", good2015, 5)
	seedRating(t, pool, "anna", ok2017, 4)
	seedRating(t, pool, "anna", dull2016, 3) // below the floor
	seedRating(t, pool, "anna", hidden2019, 5)
	seedFavorite(t, pool, "anna", fave1998)
	// Another user's opinions must not leak into anna's cards.
	seedRating(t, pool, "bara", dull2016, 5)

	store := review.NewBreatherStore(pool)
	picks, err := store.PickBreathers(context.Background(), "anna", 8)
	if err != nil {
		t.Fatalf("PickBreathers: %v", err)
	}
	if len(picks) != 2 {
		t.Fatalf("picks = %+v, want one per era (the 2010s and the 1990s)", picks)
	}
	if picks[0].PhotoUID != good2015 || picks[0].Rating != 5 || picks[0].Favorite {
		t.Errorf("first pick = %+v, want the best-rated photo of the newest era (%s)",
			picks[0], good2015)
	}
	if picks[1].PhotoUID != fave1998 || !picks[1].Favorite {
		t.Errorf("second pick = %+v, want the favourited 1998 photo (%s)", picks[1], fave1998)
	}
}

func TestPickBreathers_nothingLikedYieldsNoPicks(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	seedUser(t, pool, "anna", "anna", "Anna")
	seedPhoto(t, photos.NewStore(pool), "hash-plain", 2020, false)

	picks, err := review.NewBreatherStore(pool).PickBreathers(context.Background(), "anna", 8)
	if err != nil {
		t.Fatalf("PickBreathers: %v", err)
	}
	if len(picks) != 0 {
		t.Errorf("picks = %+v, want none — a library nobody has curated has no breathers", picks)
	}
}
