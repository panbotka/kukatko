// Package avatar renders and caches the small square picture that stands for a
// subject on the people index: a padded crop of the person's face, or the
// centre of the cover photo somebody chose for them.
//
// It exists because the browser used to do this job. A subject tile is a ~150 px
// square, but a face box is a few per cent of its photo, so cropping one out
// client-side means downloading a whole-frame preview measured in megapixels to
// paint a thumbnail — measured on the real library, 125 Mpx of image for 72
// tiles. Cutting the crop here turns each tile into a ~320 px JPEG: measured on
// real 24 Mpx originals, ~15 kB against the ~190 kB of the `fit_1280` the browser
// used to fetch to cut the same face — the same picture, an order of magnitude
// less traffic.
//
// The rendition is derived media, regenerable from the original and never part
// of the catalogue. It lives in the configured cache root in the same
// SHA256-sharded layout the thumbnailer uses, under its own prefix
//
//	avatar/<aa>/<bb>/<cc>/<hash>_<variant>_<side>.jpg
//
// where aa/bb/cc are the first three byte-pairs of the photo's hex file hash and
// variant identifies the crop (the face box, or the whole frame). Like the video
// storyboard and unlike a thumbnail it is deliberately **cache-only**: it is
// never uploaded to the object store, so it adds no prefix to the bucket and
// nothing to the wipe/orphan-sweep contract, and a pruned cache costs one cheap
// re-render.
//
// The source is never the original. A crop is cut from the smallest registered
// `fit_*` thumbnail that still puts enough pixels across it (see sourceSize), so
// rendering is a JPEG decode of at most a 1920 px preview — no HEIC/RAW
// shell-out, no 24 Mpx bitmap in a request.
package avatar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/draw"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/thumb"
)

// CacheSubdir is the top-level directory under the local cache root that holds
// every rendered avatar. It is exported so the operations that reason about
// whole directories rather than single files (a library wipe) can name the one
// this package owns instead of hardcoding the string.
const CacheSubdir = "avatar"

// Side is the rendered avatar's edge in pixels. The people index draws its tiles
// at roughly 150 CSS px, so 320 covers a 2× display with a little headroom and
// costs some 15 kB — against the hundreds of kilobytes a whole-frame preview cost
// to crop the same face in the browser.
const Side = 320

// quality is the JPEG encoder quality. It matches the tile sizes in the
// thumbnail registry: at this edge length the difference from 90 is invisible and
// the bytes are the whole point of the exercise.
const quality = 85

// facePadding is how much context the crop keeps around the detector's box, as a
// fraction of the box on each side. A crop tight to the box is a nose and two
// eyes with the chin cut off — recognisable to a machine, not to a person. It is
// the same 0.3 the client used when it still cut these crops itself, so the
// pictures did not change when the work moved to the server.
const facePadding = 0.3

// shardLen is the number of hex characters per cache directory level: three
// levels of one byte each, exactly as the thumbnail cache shards.
const shardLen = 2

// minHashLen is the shortest file hash that can be sharded into the cache tree.
const minHashLen = shardLen * 3

// faceSourceSizes are the thumbnails a face crop may be cut from, ascending.
// They must all be `fit_*` sizes: those keep the whole frame, which is what a
// normalised box is measured against — a `tile_*` size is a centre-cropped
// square, so the frame it shows is not the frame the box was normalised to and
// the crop would land beside the face.
//
// The ladder stops at 1920 on purpose. Past it the pixels behind a small face
// are mostly not there anyway, and a page of tiles must not make the server pull
// a 3840 px preview per person.
var faceSourceSizes = []string{"fit_720", "fit_1280", "fit_1920"}

// Sentinel errors returned by this package so callers (the HTTP layer, tests)
// can branch with errors.Is.
var (
	// ErrInvalidHash indicates a file hash that is empty or not a hex string of at
	// least the three byte-pairs needed to shard the cache tree.
	ErrInvalidHash = errors.New("avatar: invalid file hash")
	// ErrRenderFailed indicates the source thumbnail could not be turned into an
	// avatar — it decoded to nothing usable, or the crop was degenerate.
	ErrRenderFailed = errors.New("avatar: rendering failed")
)

// Box is a face's normalised [x, y, w, h] rectangle in a photo's display space,
// each value in 0..1. It is the geometry a marker carries, named for the one
// thing this package does with it.
type Box struct {
	X float64
	Y float64
	W float64
	H float64
}

// Source opens a photo's thumbnail at a registered size, generating it when it
// exists nowhere yet. It is satisfied by *thumb.Thumbnailer; tests use a fake,
// which is why the renderer depends on this behaviour rather than on the
// thumbnailer itself.
type Source interface {
	// OpenOrGenerate returns a reader over the photo's thumbnail at size. The
	// caller owns the returned reader and must close it.
	OpenOrGenerate(ctx context.Context, photo photos.Photo, size string) (io.ReadCloser, error)
}

// Renderer cuts subject avatars out of cached thumbnails and keeps them in the
// local derived-media cache. It is safe for concurrent use: two requests for the
// same avatar render it twice and each write is atomic, which costs a duplicated
// decode at worst and never a torn file.
type Renderer struct {
	source   Source
	cacheDir string
}

// New returns a Renderer that reads its source thumbnails through source and
// writes rendered avatars under cacheDir (the configured storage.cache_path,
// the same root the thumbnail cache uses).
func New(source Source, cacheDir string) *Renderer {
	return &Renderer{source: source, cacheDir: cacheDir}
}

// Open returns a reader over the subject avatar cut from photo, plus the ETag
// that identifies it. A nil face means the photo is a hand-picked cover and is
// shown whole (centre-cropped square); a non-nil face is padded, squared and cut
// out of the frame. The caller owns the reader and must close it.
//
// A cached rendition is served straight from disk; otherwise the avatar is
// rendered, written to the cache and returned from memory. It returns
// ErrInvalidHash for a photo whose file hash cannot address the cache, and a
// wrapped ErrRenderFailed when the source thumbnail yields no usable crop.
func (r *Renderer) Open(
	ctx context.Context, photo photos.Photo, face *Box,
) (io.ReadCloser, string, error) {
	variant := variantKey(face)
	rel, err := RelPath(photo.FileHash, variant)
	if err != nil {
		return nil, "", err
	}
	etag := strconv.Quote(photo.FileHash + "-" + variant + "-" + strconv.Itoa(Side))
	abs := filepath.Join(r.cacheDir, filepath.FromSlash(rel))
	if file, err := os.Open(abs); err == nil { //nolint:gosec // G304: abs is built from a validated hex hash.
		return file, etag, nil
	}

	data, err := r.render(ctx, photo, face)
	if err != nil {
		return nil, "", err
	}
	if err := writeFileAtomic(abs, data); err != nil {
		return nil, "", err
	}
	return io.NopCloser(bytes.NewReader(data)), etag, nil
}

// render decodes the source thumbnail, cuts the avatar out of it and encodes the
// result as a JPEG. It never upscales: a crop with fewer pixels than Side is
// rendered at its own size, since inventing pixels here would only cost bytes.
func (r *Renderer) render(ctx context.Context, photo photos.Photo, face *Box) ([]byte, error) {
	size := sourceSize(photo, face)
	reader, err := r.source.OpenOrGenerate(ctx, photo, size)
	if err != nil {
		return nil, fmt.Errorf("avatar: opening %s of %s: %w", size, photo.UID, err)
	}
	defer func() { _ = reader.Close() }()

	img, err := jpeg.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("avatar: decoding %s of %s: %w", size, photo.UID, err)
	}
	rect := cropRect(img.Bounds(), face)
	if rect.Dx() <= 0 || rect.Dy() <= 0 {
		return nil, fmt.Errorf("%w: empty crop of %s", ErrRenderFailed, photo.UID)
	}
	side := min(Side, rect.Dx())
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, rect, draw.Src, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("avatar: encoding %s: %w", photo.UID, err)
	}
	return buf.Bytes(), nil
}

// RelPath returns the slash-separated cache path of the avatar for the given file
// hash and crop variant — avatar/<aa>/<bb>/<cc>/<hash>_<variant>_<side>.jpg —
// whether or not it exists yet. It returns ErrInvalidHash for a hash that is
// empty, non-hex or too short to shard.
func RelPath(hash, variant string) (string, error) {
	if err := validateHash(hash); err != nil {
		return "", err
	}
	name := hash + "_" + variant + "_" + strconv.Itoa(Side) + ".jpg"
	return path.Join(CacheSubdir, hash[0:shardLen], hash[shardLen:shardLen*2],
		hash[shardLen*2:shardLen*3], name), nil
}

// validateHash reports whether hash is a lowercase hex digest long enough to
// shard the cache tree, returning ErrInvalidHash when it is not. Rejecting it
// here is what keeps a stored hash from escaping the cache root.
func validateHash(hash string) error {
	if len(hash) < minHashLen {
		return fmt.Errorf("%w: %q", ErrInvalidHash, hash)
	}
	if strings.TrimLeft(hash, "0123456789abcdef") != "" {
		return fmt.Errorf("%w: %q is not lowercase hex", ErrInvalidHash, hash)
	}
	return nil
}

// variantKey names the crop within a photo, so that one photo standing for two
// people yields two cache entries and changing whose face a subject shows
// invalidates neither the other's nor its own by accident. A cover photo is
// shown whole and has a single fixed key; a face is keyed by a digest of its
// box, rounded to the fourth decimal — finer than a marker is ever nudged, and
// coarse enough that floating-point noise cannot fork the cache.
func variantKey(face *Box) string {
	if face == nil {
		return "cover"
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%.4f,%.4f,%.4f,%.4f", face.X, face.Y, face.W, face.H))
	return "f" + hex.EncodeToString(sum[:6])
}

// cropRect returns the pixel rectangle to cut out of a decoded source image: the
// largest centred square for a cover photo, or the face box padded for context
// and squared for a face.
func cropRect(bounds image.Rectangle, face *Box) image.Rectangle {
	if face == nil {
		return centreSquare(bounds)
	}
	return faceSquare(bounds, *face)
}

// centreSquare returns the largest square centred in bounds — the same crop the
// square thumbnail sizes take, so a hand-picked cover shows exactly what it
// showed before the crop moved to the server.
func centreSquare(bounds image.Rectangle) image.Rectangle {
	side := min(bounds.Dx(), bounds.Dy())
	x0 := bounds.Min.X + (bounds.Dx()-side)/2
	y0 := bounds.Min.Y + (bounds.Dy()-side)/2
	return image.Rect(x0, y0, x0+side, y0+side)
}

// faceSquare returns the square pixel rectangle a face is shown in: the box grown
// by facePadding on each side (clamped to the frame), then squared on its longer
// pixel edge and slid back inside the frame rather than clipped, so the crop
// keeps both its size and its shape.
//
// It works in pixels throughout, because normalised units are not comparable
// across the two axes: the same normalised width is more pixels on a wide frame
// than on a tall one.
func faceSquare(bounds image.Rectangle, face Box) image.Rectangle {
	width, height := float64(bounds.Dx()), float64(bounds.Dy())
	left := clampUnit(face.X-face.W*facePadding) * width
	top := clampUnit(face.Y-face.H*facePadding) * height
	right := clampUnit(face.X+face.W*(1+facePadding)) * width
	bottom := clampUnit(face.Y+face.H*(1+facePadding)) * height

	side := min(max(right-left, bottom-top), width, height)
	centreX, centreY := (left+right)/2, (top+bottom)/2
	x0 := min(max(centreX-side/2, 0), width-side)
	y0 := min(max(centreY-side/2, 0), height-side)
	rect := image.Rect(
		bounds.Min.X+int(x0), bounds.Min.Y+int(y0),
		bounds.Min.X+int(x0+side), bounds.Min.Y+int(y0+side),
	)
	return rect.Intersect(bounds)
}

// clampUnit clamps a fraction to the closed unit interval, so a box the detector
// pushed past the edge of its frame never addresses pixels that do not exist.
func clampUnit(v float64) float64 {
	return min(max(v, 0), 1)
}

// sourceSize picks the thumbnail the avatar is cut from: the square grid size for
// a cover photo (already a centred square, so nothing but a resize is needed),
// and for a face the smallest `fit_*` rung that still puts Side pixels across the
// padded crop.
//
// The rung matters because a face is a small window onto a whole frame that has
// to be fetched and decoded entire. Choosing per face is what keeps a person
// filling half their photo from costing what a person in a crowd costs; the
// ladder's ceiling caps the latter, whose pixels are not there at any rung.
func sourceSize(photo photos.Photo, face *Box) string {
	if face == nil {
		return thumb.GridSize
	}
	width, height := displayFrame(photo)
	longSide := float64(max(width, height))
	cropPx := max(face.W*(1+2*facePadding)*float64(width), face.H*(1+2*facePadding)*float64(height))
	if longSide <= 0 || cropPx <= 0 {
		return faceSourceSizes[0]
	}
	for _, size := range faceSourceSizes {
		rung, err := strconv.Atoi(strings.TrimPrefix(size, "fit_"))
		if err != nil {
			continue
		}
		// `fit_N` bounds the frame's longest side and never upscales, so the crop's
		// width there is its width in the original times min(1, N / longest).
		if cropPx*min(1, float64(rung)/longSide) >= Side {
			return size
		}
	}
	return faceSourceSizes[len(faceSourceSizes)-1]
}

// displayFrame returns the photo's frame as it is displayed, swapping the stored
// width and height for the EXIF orientations that turn the picture a quarter
// turn. Marker boxes are normalised against this frame, so the crop maths must
// use it too.
func displayFrame(photo photos.Photo) (width, height int) {
	switch photo.FileOrientation {
	case 5, 6, 7, 8:
		return photo.FileHeight, photo.FileWidth
	default:
		return photo.FileWidth, photo.FileHeight
	}
}

// writeFileAtomic writes data to absPath through a temporary file in the same
// directory and renames it into place, creating the directory tree as needed. A
// reader therefore sees either no avatar or a complete one, never half of a
// JPEG another request is still writing.
func writeFileAtomic(absPath string, data []byte) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("avatar: creating cache directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".avatar-*")
	if err != nil {
		return fmt.Errorf("avatar: creating temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("avatar: writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("avatar: closing %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("avatar: setting mode on %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, absPath); err != nil {
		return fmt.Errorf("avatar: renaming %s into place: %w", tmpName, err)
	}
	return nil
}
