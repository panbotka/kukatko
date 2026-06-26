package exif

import "strings"

// dmsToDecimal converts a degrees/minutes/seconds coordinate to signed-magnitude
// decimal degrees (the sign is applied separately by applyHemisphere). All three
// components are taken as positive magnitudes: 39°54'56" becomes 39.915556.
func dmsToDecimal(degrees, minutes, seconds float64) float64 {
	return degrees + minutes/60 + seconds/3600
}

// applyHemisphere returns the coordinate magnitude signed according to the EXIF
// hemisphere reference: "S" (south) and "W" (west) yield a negative value,
// "N"/"E" (and an empty or unrecognised ref) keep it positive. The reference is
// matched case-insensitively and accepts both the single-letter form ("S") and
// exiftool's spelled-out form ("South").
func applyHemisphere(magnitude float64, ref string) float64 {
	switch strings.ToUpper(strings.TrimSpace(ref)) {
	case "S", "SOUTH", "W", "WEST":
		return -magnitude
	default:
		return magnitude
	}
}
