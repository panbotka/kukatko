//go:build integration

package photos_test

import (
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestListBBox verifies the map's "photos in this area" filter: inclusive edges,
// photos without a location never match, and a box across the antimeridian
// covers both sides of it. Count must agree with List, since the panel's header
// and the viewer's paging read the same filter.
func TestListBBox(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	place := func(hash string, lat, lng *float64) string {
		t.Helper()
		p := samplePhoto(hash)
		p.Lat, p.Lng = lat, lng
		created, err := store.Create(ctx, p)
		if err != nil {
			t.Fatalf("Create %s: %v", hash, err)
		}
		return created.UID
	}
	brno := place("bb-brno", new(49.19), new(16.61))
	edge := place("bb-edge", new(49.5), new(17.0))
	prague := place("bb-prague", new(50.08), new(14.42))
	fiji := place("bb-fiji", new(-17.7), new(178.0))
	samoa := place("bb-samoa", new(-13.8), new(-172.1))
	place("bb-nowhere", nil, nil)

	tests := []struct {
		name string
		box  photos.BBox
		want []string
	}{
		{name: "moravia, edge inclusive", box: photos.BBox{South: 49, West: 16, North: 49.5, East: 17}, want: []string{brno, edge}},
		{name: "bohemia", box: photos.BBox{South: 50, West: 14, North: 50.2, East: 14.5}, want: []string{prague}},
		{name: "across the antimeridian", box: photos.BBox{South: -20, West: 170, North: -10, East: -170}, want: []string{fiji, samoa}},
		{name: "empty sea", box: photos.BBox{South: 0, West: 0, North: 1, East: 1}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := photos.ListParams{BBox: &tt.box}
			list, err := store.List(ctx, params)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			got := make([]string, 0, len(list))
			for i := range list {
				got = append(got, list[i].UID)
			}
			slices.Sort(got)
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("List(bbox %+v) = %v, want %v", tt.box, got, want)
			}
			count, err := store.Count(ctx, params)
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if count != len(want) {
				t.Errorf("Count(bbox %+v) = %d, want %d", tt.box, count, len(want))
			}
		})
	}
}
