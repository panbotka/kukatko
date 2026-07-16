// Package geoestimate infers a photo's location from photos taken near it in
// time. A camera without a GPS receiver, a scan or a stripped export leaves a
// photo with no coordinates, but it was very often taken the same day, in the
// same place, as photos that do have them. Reusing their location fills the map
// and makes the places hierarchy useful, and costs nothing but a query.
//
// The package is built around one rule: a wrong location is worse than no
// location. A bad guess does not merely look wrong on the detail page — it
// silently poisons the map, the places hierarchy and any near: search built on
// them, and it does so while looking exactly as trustworthy as a measured
// coordinate. So the estimator refuses far more readily than it guesses: if the
// day's photos span Prague and Vienna there is no honest answer and it produces
// nothing. Everything it does write is marked "estimate" (see
// photos.Photo.LocationSource) and can be accepted or thrown away by the user.
package geoestimate

import "math"

// earthRadiusMeters is the mean radius of the Earth, the sphere the haversine
// distance is computed on. Over the few kilometres this package cares about, a
// sphere and the real ellipsoid differ by metres — far below the precision a
// "same day, same place" inference could ever claim.
const earthRadiusMeters = 6371000.0

// Point is a position in degrees.
type Point struct {
	Lat float64
	Lng float64
}

// Estimate returns the location shared by neighbours and true when they are
// geographically coherent — every one of them lies within radiusMeters of their
// centroid — and the zero Point and false when they are not, or when there are
// no neighbours at all.
//
// Coherence is deliberately the crudest test that answers the question "are
// these points close together": the centroid, and the distance to the furthest
// point from it. A single outlier is enough to refuse the whole set, which is
// the intended failure mode — the cost of refusing is an empty field the user
// can fill in, and the cost of guessing wrong is a lie the user has no reason to
// doubt. Nothing here clusters, votes or discards outliers, because every one of
// those turns "these photos agree" into "most of these photos agree", which is a
// different and far weaker claim than the UI would be making on its behalf.
//
// A set straddling the ±180° antimeridian gets a meaningless centroid out in the
// middle of the Pacific, every point falls outside the radius, and the set is
// reported incoherent. That is a wrong reason reaching the right answer, and it
// is left alone: producing nothing is already the safe outcome, and the only
// libraries affected are ones whose photos genuinely straddle the date line
// within a few hours.
func Estimate(neighbours []Point, radiusMeters float64) (Point, bool) {
	if len(neighbours) == 0 {
		return Point{}, false
	}
	centre := centroid(neighbours)
	for _, n := range neighbours {
		if DistanceMeters(centre, n) > radiusMeters {
			return Point{}, false
		}
	}
	return centre, true
}

// centroid returns the arithmetic mean of the points, which callers must not
// pass an empty slice (Estimate guards it). It is a plain mean in degrees rather
// than a spherical average: over a coherent set — a few kilometres across, far
// from the poles and the antimeridian — the two agree to well within a metre,
// and any set where they would not is one Estimate is about to reject anyway.
func centroid(points []Point) Point {
	var sumLat, sumLng float64
	for _, p := range points {
		sumLat += p.Lat
		sumLng += p.Lng
	}
	n := float64(len(points))
	return Point{Lat: sumLat / n, Lng: sumLng / n}
}

// DistanceMeters returns the great-circle distance between a and b in metres,
// using the haversine formula.
func DistanceMeters(a, b Point) float64 {
	lat1 := radians(a.Lat)
	lat2 := radians(b.Lat)
	dLat := lat2 - lat1
	dLng := radians(b.Lng - a.Lng)

	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusMeters * math.Asin(math.Min(1, math.Sqrt(h)))
}

// radians converts degrees to radians.
func radians(deg float64) float64 {
	return deg * math.Pi / 180
}
