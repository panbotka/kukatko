package exif

import (
	"testing"
	"time"
)

// TestParseFilenameDate_patterns covers the supported filename naming
// conventions plus the rejection of names without a parseable date and of names
// whose digits do not form a valid calendar date.
func TestParseFilenameDate_patterns(t *testing.T) {
	t.Parallel()

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
