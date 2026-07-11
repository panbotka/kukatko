package photoapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakeRatings is a controllable RatingStore for annotation unit tests.
type fakeRatings struct {
	rated   map[string]organize.PhotoRating
	err     error
	gotUser string
	gotUIDs []string
}

// SetRating is unused by the annotation tests and always succeeds.
func (f *fakeRatings) SetRating(_ context.Context, _, _ string, _ int) error { return nil }

// SetFlag is unused by the annotation tests and always succeeds.
func (f *fakeRatings) SetFlag(_ context.Context, _, _, _ string) error { return nil }

// ClearRating is unused by the annotation tests and always succeeds.
func (f *fakeRatings) ClearRating(_ context.Context, _, _ string) error { return nil }

// RatingsAmong records its arguments and returns the configured map or error.
func (f *fakeRatings) RatingsAmong(
	_ context.Context, userUID string, photoUIDs []string,
) (map[string]organize.PhotoRating, error) {
	f.gotUser = userUID
	f.gotUIDs = photoUIDs
	if f.err != nil {
		return nil, f.err
	}
	return f.rated, nil
}

// TestAnnotateRatings verifies the rating annotation: rated photos take their
// store rating/flag, never-rated photos default to rating 0 / flag "none", a nil
// store leaves the defaults, and a store error propagates.
func TestAnnotateRatings(t *testing.T) {
	t.Parallel()

	list := []photos.Photo{{UID: "ph_1"}, {UID: "ph_2"}, {UID: "ph_3"}}

	t.Run("annotates rated photos and defaults the rest", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRatings{rated: map[string]organize.PhotoRating{
			"ph_1": {Rating: 4, Flag: "none"},
			"ph_3": {Rating: 0, Flag: "pick"},
		}}
		api := &API{ratings: fake}
		views, err := api.annotate(context.Background(), "us_1", list)
		if err != nil {
			t.Fatalf("annotate: %v", err)
		}
		want := map[string]organize.PhotoRating{
			"ph_1": {Rating: 4, Flag: "none"},
			"ph_2": {Rating: 0, Flag: "none"},
			"ph_3": {Rating: 0, Flag: "pick"},
		}
		for _, v := range views {
			got := organize.PhotoRating{Rating: v.Rating, Flag: v.Flag}
			if got != want[v.UID] {
				t.Errorf("%s rating = %+v, want %+v", v.UID, got, want[v.UID])
			}
		}
		if fake.gotUser != "us_1" || len(fake.gotUIDs) != 3 {
			t.Errorf("RatingsAmong got user=%q uids=%v", fake.gotUser, fake.gotUIDs)
		}
	})

	t.Run("nil store defaults to rating 0 / flag none", func(t *testing.T) {
		t.Parallel()
		api := &API{}
		views, err := api.annotate(context.Background(), "us_1", list)
		if err != nil {
			t.Fatalf("annotate: %v", err)
		}
		for _, v := range views {
			if v.Rating != 0 || v.Flag != "none" {
				t.Errorf("%s rating = {%d %q} with nil store, want {0 none}", v.UID, v.Rating, v.Flag)
			}
		}
	})

	t.Run("propagates the store error", func(t *testing.T) {
		t.Parallel()
		api := &API{ratings: &fakeRatings{err: errors.New("boom")}}
		if _, err := api.annotate(context.Background(), "us_1", list); err == nil {
			t.Fatal("annotate error = nil, want store error")
		}
	})
}

// TestRatingRequest_validate verifies the body validation: at least one of
// rating/flag is required, and supplied values must be in range.
func TestRatingRequest_validate(t *testing.T) {
	t.Parallel()

	rating := func(n int) *int { return &n }
	flag := func(s string) *string { return &s }

	tests := []struct {
		name    string
		body    ratingRequest
		wantErr bool
	}{
		{name: "neither set", body: ratingRequest{}, wantErr: true},
		{name: "rating only", body: ratingRequest{Rating: rating(3)}},
		{name: "flag only", body: ratingRequest{Flag: flag("pick")}},
		{name: "both", body: ratingRequest{Rating: rating(5), Flag: flag("reject")}},
		{name: "flag none allowed", body: ratingRequest{Flag: flag("none")}},
		{name: "rating zero allowed", body: ratingRequest{Rating: rating(0)}},
		{name: "rating too high", body: ratingRequest{Rating: rating(6)}, wantErr: true},
		{name: "rating negative", body: ratingRequest{Rating: rating(-1)}, wantErr: true},
		{name: "unknown flag", body: ratingRequest{Flag: flag("star")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.body.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate(%+v) error = %v, wantErr %v", tt.body, err, tt.wantErr)
			}
		})
	}
}

// TestHandleRating_noBackend verifies both rating endpoints answer 503 when no
// ratings backend is wired, before any auth or body parsing.
func TestHandleRating_noBackend(t *testing.T) {
	t.Parallel()

	api := &API{} // no ratings store

	put := httptest.NewRequestWithContext(
		t.Context(), http.MethodPut, "/api/v1/photos/ph_1/rating", strings.NewReader(`{"rating":3}`))
	putRec := httptest.NewRecorder()
	api.handleSetRating(putRec, put)
	if putRec.Code != http.StatusServiceUnavailable {
		t.Errorf("PUT status = %d, want 503", putRec.Code)
	}

	del := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/photos/ph_1/rating", nil)
	delRec := httptest.NewRecorder()
	api.handleClearRating(delRec, del)
	if delRec.Code != http.StatusServiceUnavailable {
		t.Errorf("DELETE status = %d, want 503", delRec.Code)
	}
}

// TestIsValidFlag verifies the recognised flag set.
func TestIsValidFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flag string
		want bool
	}{
		{"none", true},
		{"pick", true},
		{"reject", true},
		{"eye", true},
		{"", false},
		{"star", false},
	}
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			t.Parallel()
			if got := isValidFlag(tt.flag); got != tt.want {
				t.Errorf("isValidFlag(%q) = %v, want %v", tt.flag, got, tt.want)
			}
		})
	}
}
