package bulk

import (
	"testing"
)

// coordAt builds a coordinate for a photo standing at lat/lng.
func coordAt(lat, lng float64) coordinate {
	return coordinate{lat: &lat, lng: &lng}
}

// TestOperations_forPhoto covers the per-photo narrowing of a location
// operation: which photos a fill-the-gaps set touches, and which of them the
// batch actually moves — the answer that decides whether a metered reverse
// geocode is scheduled.
func TestOperations_forPhoto(t *testing.T) {
	t.Parallel()

	half := 50.1
	tests := []struct {
		name        string
		ops         Operations
		current     coordinate
		wantSet     bool
		wantMoved   bool
		wantOtherOp bool
	}{
		{
			name:      "overwrite places a photo that already has a location",
			ops:       Operations{Location: &Location{Lat: 49.2, Lng: 16.6}},
			current:   coordAt(50.1, 14.4),
			wantSet:   true,
			wantMoved: true,
		},
		{
			name:      "overwrite of the very same coordinate moves nothing",
			ops:       Operations{Location: &Location{Lat: 50.1, Lng: 14.4}},
			current:   coordAt(50.1, 14.4),
			wantSet:   true,
			wantMoved: false,
		},
		{
			name:      "only_missing fills an empty photo",
			ops:       Operations{Location: &Location{Lat: 49.2, Lng: 16.6, OnlyMissing: true}},
			current:   coordinate{},
			wantSet:   true,
			wantMoved: true,
		},
		{
			name:      "only_missing leaves a placed photo alone",
			ops:       Operations{Location: &Location{Lat: 49.2, Lng: 16.6, OnlyMissing: true}},
			current:   coordAt(50.1, 14.4),
			wantSet:   false,
			wantMoved: false,
		},
		{
			name:      "only_missing completes half a coordinate",
			ops:       Operations{Location: &Location{Lat: 49.2, Lng: 16.6, OnlyMissing: true}},
			current:   coordinate{lat: &half},
			wantSet:   true,
			wantMoved: true,
		},
		{
			name:        "only_missing keeps the rest of the batch for a placed photo",
			ops:         Operations{Location: &Location{Lat: 49.2, Lng: 16.6, OnlyMissing: true}, Archive: new(true)},
			current:     coordAt(50.1, 14.4),
			wantSet:     false,
			wantMoved:   false,
			wantOtherOp: true,
		},
		{
			name:      "clearing a placed photo takes it off the map",
			ops:       Operations{ClearLocation: true},
			current:   coordAt(50.1, 14.4),
			wantMoved: true,
		},
		{
			name:      "clearing a photo that was never placed changes nothing",
			ops:       Operations{ClearLocation: true},
			current:   coordinate{},
			wantMoved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, moved := tt.ops.forPhoto(tt.current)
			if (got.Location != nil) != tt.wantSet {
				t.Errorf("forPhoto() location = %v, want set=%v", got.Location, tt.wantSet)
			}
			if moved != tt.wantMoved {
				t.Errorf("forPhoto() moved = %v, want %v", moved, tt.wantMoved)
			}
			if (got.Archive != nil) != tt.wantOtherOp {
				t.Errorf("forPhoto() archive = %v, want present=%v", got.Archive, tt.wantOtherOp)
			}
		})
	}
}

// TestOperations_forPhoto_leavesCallerUntouched asserts the narrowing is per
// photo: dropping the location for one photo that already has one must not
// disarm the operation for the rest of the batch.
func TestOperations_forPhoto_leavesCallerUntouched(t *testing.T) {
	t.Parallel()

	ops := Operations{Location: &Location{Lat: 49.2, Lng: 16.6, OnlyMissing: true}}
	if narrowed, _ := ops.forPhoto(coordAt(50.1, 14.4)); narrowed.Location != nil {
		t.Fatalf("narrowed location = %v, want nil", narrowed.Location)
	}
	if ops.Location == nil {
		t.Error("forPhoto() cleared the caller's own operation set")
	}
}

// TestCoordinate_placed covers what counts as a photo that already has a
// location: both components. Half a coordinate cannot be drawn on a map, so a
// fill-the-gaps operation is free to complete it.
func TestCoordinate_placed(t *testing.T) {
	t.Parallel()

	lat := 50.1
	lng := 14.4
	tests := []struct {
		name string
		c    coordinate
		want bool
	}{
		{"both components", coordinate{lat: &lat, lng: &lng}, true},
		{"latitude only", coordinate{lat: &lat}, false},
		{"longitude only", coordinate{lng: &lng}, false},
		{"neither", coordinate{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.c.placed(); got != tt.want {
				t.Errorf("placed() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOperations_Summary_onlyMissing asserts the audit entry records which of the
// two set-location edits was made: the same coordinate with and without
// only_missing describes two different changes to the same batch.
func TestOperations_Summary_onlyMissing(t *testing.T) {
	t.Parallel()

	overwrite := Operations{Location: &Location{Lat: 49.2, Lng: 16.6}}.Summary()
	location, ok := overwrite["location"].(map[string]any)
	if !ok {
		t.Fatalf("summary location = %T, want map", overwrite["location"])
	}
	if _, marked := location["only_missing"]; marked {
		t.Errorf("overwrite summary claims only_missing: %v", location)
	}

	fill := Operations{Location: &Location{Lat: 49.2, Lng: 16.6, OnlyMissing: true}}.Summary()
	filled, ok := fill["location"].(map[string]any)
	if !ok {
		t.Fatalf("summary location = %T, want map", fill["location"])
	}
	if filled["only_missing"] != true {
		t.Errorf("fill summary = %v, want only_missing true", filled)
	}
}
