package photos

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrInvalidBBox is returned by ParseBBox for a value that is not four
// in-range numbers in south,west,north,east order.
var ErrInvalidBBox = errors.New("bbox must be south,west,north,east in degrees")

// BBox is a geographic bounding box in degrees. West may lie east of East: such
// a box crosses the antimeridian and covers the two longitude ranges on either
// side of it.
type BBox struct {
	South float64
	West  float64
	North float64
	East  float64
}

// ParseBBox reads the "south,west,north,east" form the map writes into a URL
// (the same order Leaflet's LatLngBounds.toBBoxString would give, with the axes
// swapped to latitude first, as everything else in this API is). Latitudes must
// lie in [-90, 90] with south not above north, longitudes in [-180, 180].
// It returns ErrInvalidBBox for anything else.
func ParseBBox(raw string) (BBox, error) {
	edges, err := parseEdges(raw)
	if err != nil {
		return BBox{}, err
	}
	box := BBox{South: edges[0], West: edges[1], North: edges[2], East: edges[3]}
	if !validLat(box.South) || !validLat(box.North) || box.South > box.North {
		return BBox{}, fmt.Errorf("%w: latitudes out of range", ErrInvalidBBox)
	}
	if !validLng(box.West) || !validLng(box.East) {
		return BBox{}, fmt.Errorf("%w: longitudes out of range", ErrInvalidBBox)
	}
	return box, nil
}

// parseEdges splits raw into exactly four finite numbers, returning
// ErrInvalidBBox when there are not four or one of them is not a number.
func parseEdges(raw string) ([4]float64, error) {
	var edges [4]float64
	parts := strings.Split(raw, ",")
	if len(parts) != len(edges) {
		return edges, ErrInvalidBBox
	}
	for i, part := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return edges, fmt.Errorf("%w: %q is not a number", ErrInvalidBBox, part)
		}
		edges[i] = v
	}
	return edges, nil
}

// validLat reports whether v is a latitude in [-90, 90].
func validLat(v float64) bool { return v >= -90 && v <= 90 }

// validLng reports whether v is a longitude in [-180, 180].
func validLng(v float64) bool { return v >= -180 && v <= 180 }
