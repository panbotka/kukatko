package exif

import (
	"fmt"
	"testing"
	"time"
)

// TestParseFilenameDate_patterns covers the supported filename naming
// conventions plus the rejection of names without a parseable date, of names
// whose digits do not form a valid calendar date, and of names whose year is
// implausible for a photograph.
func TestParseFilenameDate_patterns(t *testing.T) {
	t.Parallel()

	nextYear := time.Now().UTC().Year() + filenameYearLookahead

	tests := []struct {
		name string
		path string
		want time.Time
		ok   bool
	}{
		{
			name: "android IMG with time",
			path: "/photos/IMG_20230115_143052.jpg",
			want: time.Date(2023, 1, 15, 14, 30, 52, 0, time.UTC),
			ok:   true,
		},
		{
			name: "video dash separator",
			path: "VID_20230115-143052.mp4",
			want: time.Date(2023, 1, 15, 14, 30, 52, 0, time.UTC),
			ok:   true,
		},
		{
			name: "dashed date with dotted time",
			path: "Screenshot 2023-01-15 14.30.52.png",
			want: time.Date(2023, 1, 15, 14, 30, 52, 0, time.UTC),
			ok:   true,
		},
		{
			name: "compact date only",
			path: "20230115.jpg",
			want: time.Date(2023, 1, 15, 0, 0, 0, 0, time.UTC),
			ok:   true,
		},
		{
			name: "dashed date only",
			path: "2023-01-15.heic",
			want: time.Date(2023, 1, 15, 0, 0, 0, 0, time.UTC),
			ok:   true,
		},
		{
			name: "no date in name",
			path: "vacation-photo.jpg",
			ok:   false,
		},
		{
			name: "invalid month rejected",
			path: "20231315_120000.jpg",
			ok:   false,
		},
		{
			name: "invalid day rejected",
			path: "2023-02-31.jpg",
			ok:   false,
		},
		{
			// A Facebook download: the leading digits are an asset id that
			// happens to read as 9009-03-10.
			name: "facebook asset id rejected",
			path: "90090310_638783213372240_7483353598378639360_n.jpg",
			ok:   false,
		},
		{
			name: "year before photography rejected",
			path: fmt.Sprintf("IMG_%d0115_143052.jpg", minFilenameYear-1),
			ok:   false,
		},
		{
			name: "first year of photography accepted",
			path: fmt.Sprintf("IMG_%d0115_143052.jpg", minFilenameYear),
			want: time.Date(minFilenameYear, 1, 15, 14, 30, 52, 0, time.UTC),
			ok:   true,
		},
		{
			// A camera whose clock runs ahead still gets the benefit of the doubt.
			name: "next year accepted",
			path: fmt.Sprintf("IMG_%d0115_143052.jpg", nextYear),
			want: time.Date(nextYear, 1, 15, 14, 30, 52, 0, time.UTC),
			ok:   true,
		},
		{
			name: "two years ahead rejected",
			path: fmt.Sprintf("IMG_%d0115_143052.jpg", nextYear+1),
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseFilenameDate(tt.path)
			if ok != tt.ok {
				t.Fatalf("parseFilenameDate(%q) ok = %v, want %v", tt.path, ok, tt.ok)
			}
			if ok && !got.Equal(tt.want) {
				t.Errorf("parseFilenameDate(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestFilenameTakenAt verifies the exported wrapper returns a pointer to the
// parsed time on a match and (nil, false) when the name carries no date.
func TestFilenameTakenAt(t *testing.T) {
	t.Parallel()

	got, ok := FilenameTakenAt("VID_20230115_143052.mp4")
	if !ok || got == nil {
		t.Fatalf("FilenameTakenAt(video) = %v, %v; want a time and true", got, ok)
	}
	if want := time.Date(2023, 1, 15, 14, 30, 52, 0, time.UTC); !got.Equal(want) {
		t.Errorf("FilenameTakenAt = %v, want %v", got, want)
	}
	if got, ok := FilenameTakenAt("clip.mp4"); ok || got != nil {
		t.Errorf("FilenameTakenAt(no date) = %v, %v; want nil, false", got, ok)
	}
}

// TestPlausibleFilenameYear covers the bounds of the year a file name may claim:
// the birth of photography below, one year ahead of now above.
func TestPlausibleFilenameYear(t *testing.T) {
	t.Parallel()

	thisYear := time.Now().UTC().Year()

	tests := []struct {
		name string
		year int
		want bool
	}{
		{name: "before photography", year: minFilenameYear - 1, want: false},
		{name: "first year of photography", year: minFilenameYear, want: true},
		{name: "this year", year: thisYear, want: true},
		{name: "next year", year: thisYear + filenameYearLookahead, want: true},
		{name: "two years ahead", year: thisYear + filenameYearLookahead + 1, want: false},
		{name: "facebook asset id", year: 9009, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := plausibleFilenameYear(tt.year); got != tt.want {
				t.Errorf("plausibleFilenameYear(%d) = %v, want %v", tt.year, got, tt.want)
			}
		})
	}
}
