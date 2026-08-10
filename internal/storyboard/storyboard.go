// Package storyboard produces and caches the scrub-preview sprite of a video:
// one JPEG holding a grid of evenly spaced frames, which the player shows next
// to the cursor while the timeline is hovered or dragged.
//
// The sprite is derived media, regenerable from the original and never part of
// the catalogue. It lives in the configured cache root in the same SHA256-sharded
// layout the thumbnailer uses, under its own prefix
//
//	storyboard/<aa>/<bb>/<cc>/<hash>_sb.jpg
//
// where aa/bb/cc are the first three byte-pairs of the original's hex file hash.
// Unlike a thumbnail it is deliberately **cache-only**: it is never uploaded to
// the object store, so it adds no prefix to the bucket and nothing to the
// wipe/orphan-sweep contract, and a pruned cache costs one background job to
// rebuild. The player fetches it through the application's own sprite route,
// which is one ~100 kB request per video actually watched.
//
// Generation shells out to ffmpeg once per video (an `fps` + `tile` filter
// chain), so it is proportional to the clip's length and belongs in the job
// queue, never in a request. Everything here degrades rather than fails: a clip
// with no known duration, or a host with no ffmpeg, simply has no storyboard and
// the player shows no preview.
package storyboard

import (
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
)

// CacheSubdir is the top-level directory under the local cache root that holds
// every generated storyboard sprite. It is exported so the operations that
// reason about whole directories rather than single files (a library wipe) can
// name the one this package owns instead of hardcoding the string.
const CacheSubdir = "storyboard"

// Sentinel errors returned by this package so callers (the job handler, the HTTP
// layer, tests) can branch with errors.Is.
var (
	// ErrNotGenerated indicates the sprite has not been produced yet. It is the
	// ordinary "not there yet" answer a caller turns into a queued job and a
	// preview-less player, not a failure.
	ErrNotGenerated = errors.New("storyboard: sprite not generated yet")
	// ErrNoDuration indicates the video's length is unknown, so no frame schedule
	// can be planned. Such a clip never gets a storyboard.
	ErrNoDuration = errors.New("storyboard: video duration unknown")
	// ErrInvalidHash indicates a file hash that is empty or not a hex string of at
	// least the three byte-pairs needed to shard the cache tree.
	ErrInvalidHash = errors.New("storyboard: invalid file hash")
	// ErrGenerateFailed indicates ffmpeg ran but produced no usable sprite.
	ErrGenerateFailed = errors.New("storyboard: sprite generation failed")
)

const (
	// columns is the sprite's fixed width in tiles. A row of ten 160 px frames is
	// 1600 px, which every browser decodes without tiling limits and which keeps
	// the CSS background-position arithmetic in the client trivial.
	columns = 10
	// maxRows caps the sprite at columns×maxRows = 100 frames, so even a
	// feature-length clip yields one bounded ~1600×900 JPEG.
	maxRows = 10
	// targetIntervalMs is the frame spacing the planner aims for before the grid
	// bounds round it: two seconds is fine enough that a scrub preview tracks the
	// cursor, coarse enough that a five-minute clip still fits the grid.
	targetIntervalMs = 2000
	// maxTileWidth is the widest a single frame is rendered. The preview is shown
	// at roughly this size, and a wider tile would only cost bytes.
	maxTileWidth = 160
	// fallbackTileHeight is the tile height used when the source dimensions are
	// unknown — a 16:9 frame, by far the most common video shape.
	fallbackTileHeight = 90
	// shardLen is the number of leading hex characters consumed by each of the
	// three cache-tree shard levels (aa/bb/cc), matching the thumbnail cache.
	shardLen = 2
	// minHashLen is the shortest hash accepted: enough hex to form all three shard
	// levels.
	minHashLen = shardLen * 3
)

// Spec is the layout of one video's storyboard sprite: how the frames are
// arranged in the JPEG and how a playback position maps onto them. It is a pure
// function of the clip's duration and dimensions (see Plan), so the client can be
// handed it alongside the sprite and needs nothing else to place a preview.
type Spec struct {
	// Columns and Rows are the sprite's grid, filled row-major from the top left.
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
	// Count is how many frames the sprite holds; always Columns×Rows.
	Count int `json:"count"`
	// TileWidth and TileHeight are one frame's pixel size inside the sprite.
	TileWidth  int `json:"tile_width"`
	TileHeight int `json:"tile_height"`
	// IntervalMs is the playback time one tile covers: tile i shows the frame at
	// i×IntervalMs, so a client maps a position t to min(t/IntervalMs, Count-1).
	IntervalMs int `json:"interval_ms"`
}

// Plan computes the sprite layout for a clip durationMs long whose frames are
// width×height pixels. The grid is always full (Count = Columns×Rows) so the
// sprite has no ragged last row, and the interval is the duration divided by that
// count, which spreads the frames evenly across the whole clip.
//
// Unknown source dimensions (either non-positive) fall back to a 16:9 tile rather
// than failing — the tile is a preview, and a slightly wrong aspect ratio is
// better than no preview. A non-positive duration returns ErrNoDuration: without
// a length there is no schedule to lay out.
func Plan(durationMs, width, height int) (Spec, error) {
	if durationMs <= 0 {
		return Spec{}, fmt.Errorf("%w: %d ms", ErrNoDuration, durationMs)
	}
	rows := planRows(durationMs)
	count := rows * columns
	interval := max(durationMs/count, 1)
	tileWidth, tileHeight := planTile(width, height)
	return Spec{
		Columns:    columns,
		Rows:       rows,
		Count:      count,
		TileWidth:  tileWidth,
		TileHeight: tileHeight,
		IntervalMs: interval,
	}, nil
}

// planRows returns how many grid rows a clip of durationMs deserves: enough for
// roughly one frame every targetIntervalMs, clamped to at least one row and at
// most maxRows so the sprite stays bounded for a very long clip.
func planRows(durationMs int) int {
	frames := int(math.Round(float64(durationMs) / float64(targetIntervalMs)))
	rows := (frames + columns - 1) / columns
	return min(max(rows, 1), maxRows)
}

// planTile returns the pixel size of one frame in the sprite for a source of
// width×height. It preserves the source aspect ratio, never upscales beyond the
// source width, and rounds both sides to even numbers because ffmpeg's scaler
// rejects odd dimensions for several pixel formats. Unknown dimensions yield the
// 16:9 fallback.
func planTile(width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		return maxTileWidth, fallbackTileHeight
	}
	tileWidth := even(min(width, maxTileWidth))
	tileHeight := even(int(math.Round(float64(tileWidth) * float64(height) / float64(width))))
	return tileWidth, tileHeight
}

// even rounds n down to the nearest even number, with a floor of 2 so a degenerate
// dimension never becomes zero.
func even(n int) int {
	if n < 2 {
		return 2
	}
	return n - n%2
}

// TileIndex returns the sprite tile that shows the frame at positionMs, clamped
// into the grid. It is the same mapping the client performs and exists so the
// contract is stated (and tested) once, in Go.
func (s Spec) TileIndex(positionMs int) int {
	if s.IntervalMs <= 0 || s.Count <= 0 {
		return 0
	}
	return min(max(positionMs/s.IntervalMs, 0), s.Count-1)
}

// RelPath returns the slash-separated cache path of the storyboard sprite for the
// given file hash — storyboard/<aa>/<bb>/<cc>/<hash>_sb.jpg — whether or not it
// exists yet. It returns ErrInvalidHash for a hash that is empty, non-hex or too
// short to shard.
func RelPath(hash string) (string, error) {
	if err := validateHash(hash); err != nil {
		return "", err
	}
	return path.Join(
		CacheSubdir,
		hash[0:shardLen],
		hash[shardLen:shardLen*2],
		hash[shardLen*2:shardLen*3],
		hash+"_sb.jpg",
	), nil
}

// validateHash reports whether hash is a lowercase hex digest long enough to
// shard the cache tree, returning ErrInvalidHash when it is not. Rejecting it
// here is what keeps a caller-supplied hash from escaping the cache root.
func validateHash(hash string) error {
	if len(hash) < minHashLen {
		return fmt.Errorf("%w: %q", ErrInvalidHash, hash)
	}
	if strings.TrimLeft(hash, "0123456789abcdef") != "" {
		return fmt.Errorf("%w: %q is not lowercase hex", ErrInvalidHash, hash)
	}
	return nil
}
