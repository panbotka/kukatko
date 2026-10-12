package mapsapi

import (
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestToFeature_thumbFollowsTheAPIBase proves a feature's thumbnail leads back
// through the prefix the feed was asked through: the project's when it was
// addressed by slug, the bare /api/v1 through the transition alias.
func TestToFeature_thumbFollowsTheAPIBase(t *testing.T) {
	t.Parallel()
	lat, lng := 49.1, 16.6
	photo := photos.Photo{UID: "ph_a", Lat: &lat, Lng: &lng}
	for base, want := range map[string]string{
		"/api/v1":     "/api/v1/photos/ph_a/thumb/" + geoThumbSize,
		"/api/v1/p/v": "/api/v1/p/v/photos/ph_a/thumb/" + geoThumbSize,
	} {
		f, ok := toFeature(base, &photo)
		if !ok {
			t.Fatalf("toFeature(%q) dropped a located photo", base)
		}
		if f.Properties.Thumb != want {
			t.Errorf("toFeature(%q).Thumb = %q, want %q", base, f.Properties.Thumb, want)
		}
	}
}
