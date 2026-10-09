package imgconvert

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"golang.org/x/sync/semaphore"
)

// ErrOverBudget is returned (alongside ErrImageTooLarge, so every caller that
// already degrades on an oversized source degrades on this too) when a single
// decode would need more memory than the whole process-wide DecodeBudget: it
// could never be admitted, so it is refused instead of waiting forever.
var ErrOverBudget = errors.New("imgconvert: decode exceeds the process-wide memory budget")

// DecodeBudget bounds the bytes of decoded bitmaps — and the scratch derived
// from them — that in-process image work may hold at once, across every caller
// sharing it. The pixel cap (EnforcePixelBound) bounds one image by its pixel
// count; this bounds the sum, in bytes, so the bound counts bit depth (a 16-bit
// PNG decodes to twice the bytes of an 8-bit one of the same size) and N
// concurrent uploads cannot each take a cap's worth of memory.
//
// A caller reserves its estimated cost before decoding and releases it when the
// bitmap is no longer referenced; releasing a large reservation runs a garbage
// collection first, so the next admitted decode does not stack on the garbage
// of the previous one. A reservation waits while the budget is
// spent, and is refused outright (ErrOverBudget) when it exceeds the whole
// budget. A nil *DecodeBudget is valid and unbounded: Reserve admits everything
// immediately.
type DecodeBudget struct {
	sem      *semaphore.Weighted
	capacity int64
}

// NewDecodeBudget returns a budget of capacity bytes, or nil — the unbounded
// budget — when capacity is not positive.
func NewDecodeBudget(capacity int64) *DecodeBudget {
	if capacity <= 0 {
		return nil
	}
	return &DecodeBudget{sem: semaphore.NewWeighted(capacity), capacity: capacity}
}

// Capacity returns the budget in bytes, or 0 for the unbounded (nil) budget.
func (b *DecodeBudget) Capacity() int64 {
	if b == nil {
		return 0
	}
	return b.capacity
}

// Reserve blocks until cost bytes of the budget are free and takes them,
// returning the func that gives them back (safe to call more than once). A
// non-positive cost and a nil budget reserve nothing. It returns an error
// wrapping ErrImageTooLarge and ErrOverBudget when cost exceeds the whole
// budget, and ctx's error when ctx ends while waiting.
func (b *DecodeBudget) Reserve(ctx context.Context, cost int64) (func(), error) {
	if b == nil || cost <= 0 {
		return func() {}, nil
	}
	if cost > b.capacity {
		return func() {}, fmt.Errorf("%w: %w: needs %d bytes, budget is %d",
			ErrImageTooLarge, ErrOverBudget, cost, b.capacity)
	}
	if err := b.sem.Acquire(ctx, cost); err != nil {
		return func() {}, fmt.Errorf("imgconvert: waiting for decode budget: %w", err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if cost >= collectOnRelease {
				// The budget counts bytes the caller has stopped referencing, but
				// Go frees them only at the next collection — which, with the
				// default GOGC, may come after the heap has doubled. A waiter
				// admitted now would then allocate on top of the garbage, and
				// two "sequential" 800 MB decodes would still peak at 1.6 GB.
				runtime.GC()
			}
			b.sem.Release(cost)
		})
	}, nil
}

// collectOnRelease is the reservation size from which releasing it forces a
// garbage collection first, so the bytes it stood for are really free before the
// next decode is admitted. Below it, the garbage is small next to the budget
// and the collection would cost more than it saves.
const collectOnRelease = 64 << 20

// PeekConfig reads the header of the image at path — dimensions and colour
// model, never the pixel data — with the registered pure-Go decoders. ok is
// false when the header cannot be parsed, which callers treat as "not judged
// here" and leave to their own decode to report.
func PeekConfig(path string) (cfg image.Config, ok bool, err error) {
	f, err := os.Open(path) //nolint:gosec // G304: path is the storage/imgconvert file the caller is about to decode.
	if err != nil {
		return image.Config{}, false, fmt.Errorf("imgconvert: open %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }()
	cfg, _, err = image.DecodeConfig(f)
	if err != nil {
		return image.Config{}, false, nil
	}
	return cfg, true, nil
}

// DecodedBytes estimates the memory image.Decode allocates for the bitmap of an
// image with header cfg: width×height×BytesPerPixel(cfg.ColorModel).
func DecodedBytes(cfg image.Config) int64 {
	return int64(cfg.Width) * int64(cfg.Height) * BytesPerPixel(cfg.ColorModel)
}

// RGBABytes is the size of a w×h *image.RGBA — what Orient, an edit or a
// resize allocates for a full copy of the picture.
func RGBABytes(w, h int) int64 {
	return int64(w) * int64(h) * rgbaBytesPerPixel
}

// rgbaBytesPerPixel is the size of one *image.RGBA / *image.NRGBA pixel.
const rgbaBytesPerPixel = 4

// BytesPerPixel returns the bytes one pixel of the bitmap the standard decoders
// produce for model takes — which is where the bit depth enters: a 16-bit PNG
// header names a 64-bit model (8 bytes) where an 8-bit one names a 32-bit model
// (4). JPEG's YCbCr is charged as 4:4:4 (3 bytes), its largest subsampling. An
// unknown model is charged 8 bytes, the widest any standard decoder produces, so
// an estimate is never low.
func BytesPerPixel(model color.Model) int64 {
	if _, ok := model.(color.Palette); ok {
		return 1
	}
	switch model {
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.Gray16Model, color.Alpha16Model:
		return 2 // 16 bits per pixel.
	case color.YCbCrModel:
		return 3 // Y + Cb + Cr at 4:4:4.
	case color.RGBAModel, color.NRGBAModel, color.CMYKModel, color.NYCbCrAModel:
		return rgbaBytesPerPixel
	default:
		return 8 // RGBA64/NRGBA64 and anything unknown: 4 channels × 16 bits.
	}
}
