package exif

import "time"

// MinCaptureYear is the earliest year a photograph's capture date may claim:
// 1826, the year of the oldest surviving photograph. Anything reaching further
// back is an identifier that merely looks like a date.
const MinCaptureYear = 1826

// CaptureYearLookahead is how far past the current year a capture date may
// reach. One year forgives a camera whose clock is set wrong; it does not
// forgive an asset id.
const CaptureYearLookahead = 1

// PlausibleCaptureYear reports whether year could be the year a photograph was
// taken: no earlier than the birth of photography and no later than a year from
// now.
//
// It is the one place the rule lives. Two paths ask it and must never disagree:
// the file-name date heuristic below, which drops a guess outside the range
// before it is ever written, and the library integrity scan, which finds the
// rows that were dated before the guard existed (the long digit runs Facebook
// and WhatsApp name their downloads with — 90090310_638783213372240_… reads as
// 9009-03-10 — are exactly what both are looking at). A date the camera itself
// wrote is an assertion and is kept however odd it looks; this bounds the
// guesses.
func PlausibleCaptureYear(year int) bool {
	minYear, maxYear := CaptureYearBounds()
	return year >= minYear && year <= maxYear
}

// CaptureYearBounds returns the inclusive range of years a capture date may
// claim, as of now. It exists so a caller that has to express the same rule as a
// query — the maintenance scan, whose predicate runs in SQL — takes the bounds
// from here instead of restating them.
func CaptureYearBounds() (minYear, maxYear int) {
	return MinCaptureYear, time.Now().UTC().Year() + CaptureYearLookahead
}
