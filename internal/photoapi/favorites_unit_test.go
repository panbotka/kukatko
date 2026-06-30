package photoapi

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestFavoriteRequested checks the favorite=true filter parsing: absent or false
// yields no scope, true requests it, and a non-boolean value is an error.
func TestFavoriteRequested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		query   string
		want    bool
		wantErr bool
	}{
		{name: "absent", query: "", want: false},
		{name: "true", query: "favorite=true", want: true},
		{name: "false", query: "favorite=false", want: false},
		{name: "one is true", query: "favorite=1", want: true},
		{name: "invalid", query: "favorite=yes-please", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("ParseQuery(%q): %v", tt.query, err)
			}
			got, err := favoriteRequested(q)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("favoriteRequested(%q) error = nil, want error", tt.query)
				}
				return
			}
			if err != nil {
				t.Fatalf("favoriteRequested(%q) unexpected error: %v", tt.query, err)
			}
			if got != tt.want {
				t.Errorf("favoriteRequested(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

// fakeFavorites is a controllable FavoriteStore for annotation unit tests.
type fakeFavorites struct {
	favored map[string]bool
	err     error
	gotUser string
	gotUIDs []string
}

// AddFavorite is unused by the annotation tests and always succeeds.
func (f *fakeFavorites) AddFavorite(_ context.Context, _, _ string) error { return nil }

// RemoveFavorite is unused by the annotation tests and always succeeds.
func (f *fakeFavorites) RemoveFavorite(_ context.Context, _, _ string) error { return nil }

// FavoritedAmong records its arguments and returns the configured set or error.
func (f *fakeFavorites) FavoritedAmong(
	_ context.Context, userUID string, photoUIDs []string,
) (map[string]bool, error) {
	f.gotUser = userUID
	f.gotUIDs = photoUIDs
	if f.err != nil {
		return nil, f.err
	}
	return f.favored, nil
}

// TestAnnotateFavorites verifies the is-favorite annotation: the flag is set per
// photo from the store's set, an empty list skips the query, and a nil store
// leaves every flag false.
func TestAnnotateFavorites(t *testing.T) {
	t.Parallel()

	list := []photos.Photo{{UID: "ph_1"}, {UID: "ph_2"}, {UID: "ph_3"}}

	t.Run("flags the favorited photos", func(t *testing.T) {
		t.Parallel()
		fake := &fakeFavorites{favored: map[string]bool{"ph_1": true, "ph_3": true}}
		api := &API{favorites: fake}
		views, err := api.annotate(context.Background(), "us_1", list)
		if err != nil {
			t.Fatalf("annotate: %v", err)
		}
		want := map[string]bool{"ph_1": true, "ph_2": false, "ph_3": true}
		for _, v := range views {
			if v.IsFavorite != want[v.UID] {
				t.Errorf("%s is_favorite = %v, want %v", v.UID, v.IsFavorite, want[v.UID])
			}
		}
		if fake.gotUser != "us_1" || len(fake.gotUIDs) != 3 {
			t.Errorf("FavoritedAmong got user=%q uids=%v", fake.gotUser, fake.gotUIDs)
		}
	})

	t.Run("empty list skips the query", func(t *testing.T) {
		t.Parallel()
		fake := &fakeFavorites{err: errors.New("must not be called")}
		api := &API{favorites: fake}
		views, err := api.annotate(context.Background(), "us_1", nil)
		if err != nil || len(views) != 0 {
			t.Fatalf("annotate(empty) = %v, %v", views, err)
		}
	})

	t.Run("nil store leaves flags false", func(t *testing.T) {
		t.Parallel()
		api := &API{}
		views, err := api.annotate(context.Background(), "us_1", list)
		if err != nil {
			t.Fatalf("annotate: %v", err)
		}
		for _, v := range views {
			if v.IsFavorite {
				t.Errorf("%s is_favorite = true with nil store, want false", v.UID)
			}
		}
	})

	t.Run("propagates the store error", func(t *testing.T) {
		t.Parallel()
		api := &API{favorites: &fakeFavorites{err: errors.New("boom")}}
		if _, err := api.annotate(context.Background(), "us_1", list); err == nil {
			t.Fatal("annotate error = nil, want store error")
		}
	})
}
