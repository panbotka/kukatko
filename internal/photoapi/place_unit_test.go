package photoapi

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/places"
)

// fakePlaceResolver is a controllable PlaceResolver: it returns a fixed place (or
// error) and counts how often it was asked, so a test can assert the detail
// endpoint reads the cache exactly once and never more.
type fakePlaceResolver struct {
	place places.Place
	err   error
	calls int
}

// GetPlace records the call and returns the fake's configured answer.
func (f *fakePlaceResolver) GetPlace(_ context.Context, _ string) (places.Place, error) {
	f.calls++
	return f.place, f.err
}

// TestResolvePlace covers the four ways the detail response ends up without a
// place — no resolver, no cached row, a lookup failure and the "processed, no
// place" marker — and the one way it carries one.
func TestResolvePlace(t *testing.T) {
	full := places.Place{
		PhotoUID: "ph_1", Country: "Česko", Region: "Jihomoravský kraj",
		City: "Brno", PlaceName: "Špilberk",
	}

	tests := []struct {
		name     string
		resolver *fakePlaceResolver
		want     *placeRef
	}{
		{
			name:     "cached place",
			resolver: &fakePlaceResolver{place: full},
			want: &placeRef{
				Country: "Česko", Region: "Jihomoravský kraj",
				City: "Brno", PlaceName: "Špilberk",
			},
		},
		{
			name:     "not geocoded yet",
			resolver: &fakePlaceResolver{err: places.ErrPlaceNotFound},
			want:     nil,
		},
		{
			name:     "lookup failure never fails the detail",
			resolver: &fakePlaceResolver{err: errors.New("db is down")},
			want:     nil,
		},
		{
			// The places job writes an all-empty row for a photo it cannot geocode;
			// it means "processed", not "here is a place".
			name:     "processed marker row",
			resolver: &fakePlaceResolver{place: places.Place{PhotoUID: "ph_1"}},
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &API{places: tc.resolver}
			got := api.resolvePlace(t.Context(), "ph_1")
			assertPlaceRef(t, got, tc.want)
			if tc.resolver.calls != 1 {
				t.Errorf("resolver calls = %d, want exactly 1", tc.resolver.calls)
			}
		})
	}

	t.Run("no resolver wired", func(t *testing.T) {
		api := &API{}
		if got := api.resolvePlace(t.Context(), "ph_1"); got != nil {
			t.Errorf("resolvePlace() = %+v, want nil without a resolver", got)
		}
	})
}

// assertPlaceRef fails the test when got does not match want, comparing nil-ness
// first so a nil pointer never gets dereferenced.
func assertPlaceRef(t *testing.T, got, want *placeRef) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("resolvePlace() = %+v, want nil", got)
		}
		return
	}
	if got == nil {
		t.Fatalf("resolvePlace() = nil, want %+v", want)
	}
	if *got != *want {
		t.Errorf("resolvePlace() = %+v, want %+v", got, want)
	}
}
