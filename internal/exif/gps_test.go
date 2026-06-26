package exif

import (
	"math"
	"testing"
)

// TestDmsToDecimal_conversion checks degrees/minutes/seconds fold into the
// expected decimal degrees, including the integer and zero cases.
func TestDmsToDecimal_conversion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		deg, min, sec float64
		want          float64
	}{
		{name: "whole degrees", deg: 39, min: 0, sec: 0, want: 39},
		{name: "minutes and seconds", deg: 39, min: 54, sec: 56, want: 39.91555555555556},
		{name: "fractional seconds", deg: 116, min: 23, sec: 27, want: 116.39083333333333},
		{name: "zero", deg: 0, min: 0, sec: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := dmsToDecimal(tt.deg, tt.min, tt.sec)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("dmsToDecimal(%v,%v,%v) = %v, want %v", tt.deg, tt.min, tt.sec, got, tt.want)
			}
		})
	}
}

// TestApplyHemisphere_sign verifies that the southern and western hemisphere
// references (in both abbreviated and spelled-out forms) negate the magnitude
// while northern/eastern and unknown references leave it positive.
func TestApplyHemisphere_sign(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mag  float64
		ref  string
		want float64
	}{
		{name: "north positive", mag: 39.9, ref: "N", want: 39.9},
		{name: "east positive", mag: 116.4, ref: "E", want: 116.4},
		{name: "south negative", mag: 33.8, ref: "S", want: -33.8},
		{name: "west negative", mag: 70.6, ref: "W", want: -70.6},
		{name: "spelled south", mag: 12.5, ref: "South", want: -12.5},
		{name: "spelled west lowercase", mag: 12.5, ref: "west", want: -12.5},
		{name: "padded ref", mag: 5, ref: "  S  ", want: -5},
		{name: "empty ref keeps sign", mag: 5, ref: "", want: 5},
		{name: "unknown ref keeps sign", mag: 5, ref: "?", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := applyHemisphere(tt.mag, tt.ref); got != tt.want {
				t.Errorf("applyHemisphere(%v, %q) = %v, want %v", tt.mag, tt.ref, got, tt.want)
			}
		})
	}
}
