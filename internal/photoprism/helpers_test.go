package photoprism

import (
	"testing"
	"time"
)

// TestClampCount checks page-size clamping into [1, MaxCount].
func TestClampCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want int
	}{
		{in: 0, want: MaxCount},
		{in: -5, want: MaxCount},
		{in: 1, want: 1},
		{in: 500, want: 500},
		{in: MaxCount, want: MaxCount},
		{in: MaxCount + 1, want: MaxCount},
	}
	for _, tt := range tests {
		if got := clampCount(tt.in); got != tt.want {
			t.Errorf("clampCount(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestParseRetryAfter checks integer-second parsing and rejection of junk.
func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want time.Duration
	}{
		{in: "", want: 0},
		{in: "5", want: 5 * time.Second},
		{in: " 12 ", want: 12 * time.Second},
		{in: "-3", want: 0},
		{in: "abc", want: 0},
		{in: "Wed, 21 Oct 2025 07:28:00 GMT", want: 0},
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.in); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestListParamsQuery checks the simple list query builder.
func TestListParamsQuery(t *testing.T) {
	t.Parallel()
	q := ListParams{Count: 2000, Offset: -1}.query()
	if q.Get("count") != "1000" {
		t.Errorf("count = %q, want 1000", q.Get("count"))
	}
	if q.Get("offset") != "0" {
		t.Errorf("offset = %q, want 0", q.Get("offset"))
	}
}

// TestPhotoListParamsQuery_defaults checks defaults when no order or filter set.
func TestPhotoListParamsQuery_defaults(t *testing.T) {
	t.Parallel()
	q := PhotoListParams{}.query()
	if q.Get("order") != "updated" {
		t.Errorf("order = %q, want updated", q.Get("order"))
	}
	if q.Get("merged") != "true" {
		t.Errorf("merged = %q, want true", q.Get("merged"))
	}
	if _, ok := q["q"]; ok {
		t.Errorf("q should be absent without UpdatedSince, got %q", q.Get("q"))
	}
}

// TestPrimaryFile_none returns false when no file is primary.
func TestPrimaryFile_none(t *testing.T) {
	t.Parallel()
	p := Photo{Files: []File{{UID: "f1", Primary: false}}}
	if _, ok := p.PrimaryFile(); ok {
		t.Error("PrimaryFile() ok = true, want false")
	}
	empty := Photo{}
	if _, ok := empty.PrimaryFile(); ok {
		t.Error("empty PrimaryFile() ok = true, want false")
	}
}

// TestFile_IsVideo verifies a file is recognised as video by the Video flag or a
// video/* MIME type, and not otherwise.
func TestFile_IsVideo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file File
		want bool
	}{
		{name: "video flag", file: File{Video: true}, want: true},
		{name: "video mime", file: File{Mime: "video/mp4"}, want: true},
		{name: "video mime cased", file: File{Mime: "Video/Quicktime"}, want: true},
		{name: "still jpeg", file: File{Mime: "image/jpeg"}, want: false},
		{name: "empty", file: File{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.file.IsVideo(); got != tt.want {
				t.Errorf("IsVideo() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPhoto_VideoFile verifies the first video file is selected, or false when the
// photo is a plain still.
func TestPhoto_VideoFile(t *testing.T) {
	t.Parallel()
	live := Photo{Files: []File{
		{UID: "still", Primary: true, Mime: "image/jpeg"},
		{UID: "motion", Mime: "video/mp4"},
	}}
	got, ok := live.VideoFile()
	if !ok || got.UID != "motion" {
		t.Errorf("VideoFile() = %+v ok=%v, want motion", got, ok)
	}
	still := Photo{Files: []File{{UID: "still", Primary: true, Mime: "image/jpeg"}}}
	if _, ok := still.VideoFile(); ok {
		t.Error("VideoFile() ok = true for a still photo, want false")
	}
}

// TestPhoto_StillFile verifies the still image is selected for a live photo even
// when the video file is marked primary, and false for a video-only photo.
func TestPhoto_StillFile(t *testing.T) {
	t.Parallel()
	live := Photo{Files: []File{
		{UID: "motion", Primary: true, Mime: "video/mp4"},
		{UID: "still", Mime: "image/jpeg"},
	}}
	got, ok := live.StillFile()
	if !ok || got.UID != "still" {
		t.Errorf("StillFile() = %+v ok=%v, want still", got, ok)
	}
	videoOnly := Photo{Files: []File{{UID: "motion", Primary: true, Mime: "video/mp4"}}}
	if _, ok := videoOnly.StillFile(); ok {
		t.Error("StillFile() ok = true for a video-only photo, want false")
	}
}

// TestOrHelpers checks the default-fallback helpers.
func TestOrHelpers(t *testing.T) {
	t.Parallel()
	if got := orDuration(0, DefaultTimeout); got != DefaultTimeout {
		t.Errorf("orDuration(0) = %v", got)
	}
	if got := orDuration(time.Second, DefaultTimeout); got != time.Second {
		t.Errorf("orDuration(1s) = %v", got)
	}
	if got := orInt(0, 4); got != 4 {
		t.Errorf("orInt(0) = %d", got)
	}
	if got := orInt(7, 4); got != 7 {
		t.Errorf("orInt(7) = %d", got)
	}
}
