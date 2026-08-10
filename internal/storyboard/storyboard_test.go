package storyboard

import (
	"errors"
	"strings"
	"testing"
)

// TestPlan_layout covers the grid the planner lays out for clips of very
// different lengths: the one-row floor for something short, a grown grid in the
// middle, and the hard ceiling for a clip far longer than the grid can sample at
// the target interval.
func TestPlan_layout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		durationMs   int
		wantRows     int
		wantCount    int
		wantInterval int
	}{
		{name: "a two-second clip still fills one row", durationMs: 2000, wantRows: 1, wantCount: 10, wantInterval: 200},
		{name: "twenty seconds is one row at two seconds", durationMs: 20000, wantRows: 1, wantCount: 10, wantInterval: 2000},
		{name: "a minute grows to three rows", durationMs: 60000, wantRows: 3, wantCount: 30, wantInterval: 2000},
		{name: "an hour is capped at the full grid", durationMs: 3600000, wantRows: 10, wantCount: 100, wantInterval: 36000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, err := Plan(tt.durationMs, 1920, 1080)
			if err != nil {
				t.Fatalf("Plan(%d) error = %v, want nil", tt.durationMs, err)
			}
			if spec.Rows != tt.wantRows {
				t.Errorf("Plan(%d).Rows = %d, want %d", tt.durationMs, spec.Rows, tt.wantRows)
			}
			if spec.Count != tt.wantCount {
				t.Errorf("Plan(%d).Count = %d, want %d", tt.durationMs, spec.Count, tt.wantCount)
			}
			if spec.IntervalMs != tt.wantInterval {
				t.Errorf("Plan(%d).IntervalMs = %d, want %d", tt.durationMs, spec.IntervalMs, tt.wantInterval)
			}
			if spec.Columns*spec.Rows != spec.Count {
				t.Errorf("Plan(%d) grid %dx%d does not hold %d frames",
					tt.durationMs, spec.Columns, spec.Rows, spec.Count)
			}
		})
	}
}

// TestPlan_noDuration verifies a clip whose length is unknown is refused rather
// than laid out against a guess: it is the "this video will never have a
// storyboard" signal the service turns into an unavailable status.
func TestPlan_noDuration(t *testing.T) {
	t.Parallel()

	for _, duration := range []int{0, -1} {
		if _, err := Plan(duration, 1920, 1080); !errors.Is(err, ErrNoDuration) {
			t.Errorf("Plan(%d) error = %v, want ErrNoDuration", duration, err)
		}
	}
}

// TestPlan_tileSize checks the tile geometry: the aspect ratio follows the
// source, both sides stay even (ffmpeg's scaler rejects odd dimensions for common
// pixel formats), a source narrower than the tile is never upscaled, and unknown
// dimensions fall back to 16:9 instead of failing.
func TestPlan_tileSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		width      int
		height     int
		wantWidth  int
		wantHeight int
	}{
		{name: "16:9 source", width: 1920, height: 1080, wantWidth: 160, wantHeight: 90},
		{name: "portrait source", width: 1080, height: 1920, wantWidth: 160, wantHeight: 284},
		{name: "tiny source is not upscaled", width: 64, height: 48, wantWidth: 64, wantHeight: 48},
		{name: "unknown dimensions fall back to 16:9", width: 0, height: 0, wantWidth: 160, wantHeight: 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, err := Plan(10000, tt.width, tt.height)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if spec.TileWidth != tt.wantWidth || spec.TileHeight != tt.wantHeight {
				t.Errorf("tile = %dx%d, want %dx%d",
					spec.TileWidth, spec.TileHeight, tt.wantWidth, tt.wantHeight)
			}
			if spec.TileWidth%2 != 0 || spec.TileHeight%2 != 0 {
				t.Errorf("tile %dx%d has an odd side; ffmpeg's scaler needs even ones",
					spec.TileWidth, spec.TileHeight)
			}
		})
	}
}

// TestSpec_TileIndex verifies the position→tile mapping the client mirrors:
// the first tile covers the start, each interval steps one tile, and a position
// at or past the end clamps to the last tile rather than reading off the grid.
func TestSpec_TileIndex(t *testing.T) {
	t.Parallel()

	spec := Spec{Columns: 10, Rows: 1, Count: 10, IntervalMs: 1000}
	tests := []struct {
		positionMs int
		want       int
	}{
		{positionMs: 0, want: 0},
		{positionMs: 999, want: 0},
		{positionMs: 1000, want: 1},
		{positionMs: 9500, want: 9},
		{positionMs: 60000, want: 9},
		{positionMs: -5, want: 0},
	}
	for _, tt := range tests {
		if got := spec.TileIndex(tt.positionMs); got != tt.want {
			t.Errorf("TileIndex(%d) = %d, want %d", tt.positionMs, got, tt.want)
		}
	}
}

// TestSpec_TileIndex_degenerate verifies an empty spec (the zero value a pending
// storyboard carries) answers 0 instead of dividing by zero.
func TestSpec_TileIndex_degenerate(t *testing.T) {
	t.Parallel()

	if got := (Spec{}).TileIndex(5000); got != 0 {
		t.Errorf("zero Spec TileIndex = %d, want 0", got)
	}
}

// TestRelPath verifies the sharded cache key: it lives under the package's own
// prefix, shards on the first three byte-pairs of the hash and names the file
// after the whole hash.
func TestRelPath(t *testing.T) {
	t.Parallel()

	const hash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	got, err := RelPath(hash)
	if err != nil {
		t.Fatalf("RelPath: %v", err)
	}
	want := "storyboard/ab/cd/ef/" + hash + "_sb.jpg"
	if got != want {
		t.Errorf("RelPath = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, CacheSubdir+"/") {
		t.Errorf("RelPath %q escapes the %q prefix", got, CacheSubdir)
	}
}

// TestRelPath_invalidHash verifies a hash that could let a caller escape the cache
// root — empty, too short, non-hex, or carrying path separators — is refused.
func TestRelPath_invalidHash(t *testing.T) {
	t.Parallel()

	for _, hash := range []string{"", "ab", "abcde", "../../etc/passwd", "ABCDEF01", "zzzzzz"} {
		if _, err := RelPath(hash); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("RelPath(%q) error = %v, want ErrInvalidHash", hash, err)
		}
	}
}
