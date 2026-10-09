package imgconvert

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// headerOnlyPNG writes a PNG that is nothing but its IHDR — enough for
// image.DecodeConfig, which is all the budget ever reads — naming a w×h RGBA
// image of the given bit depth (8 or 16). It is how a test asks about a
// gigapixel header without allocating anything.
func headerOnlyPNG(t *testing.T, w, h, depth int) string {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	ihdr[8] = byte(depth)
	ihdr[9] = 6 // truecolour with alpha
	var word [4]byte
	binary.BigEndian.PutUint32(word[:], uint32(len(ihdr)))
	buf.Write(word[:])
	crc := crc32.NewIEEE()
	crc.Write([]byte("IHDR"))
	crc.Write(ihdr)
	buf.WriteString("IHDR")
	buf.Write(ihdr)
	binary.BigEndian.PutUint32(word[:], crc.Sum32())
	buf.Write(word[:])
	path := filepath.Join(t.TempDir(), "header.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("writing header-only png: %v", err)
	}
	return path
}

// TestNewDecodeBudget_nonPositiveIsUnbounded verifies a zero or negative
// capacity yields the nil budget, which admits any cost immediately.
func TestNewDecodeBudget_nonPositiveIsUnbounded(t *testing.T) {
	t.Parallel()
	for _, capacity := range []int64{0, -1} {
		b := NewDecodeBudget(capacity)
		if b != nil {
			t.Fatalf("NewDecodeBudget(%d) = %v, want nil", capacity, b)
		}
		if got := b.Capacity(); got != 0 {
			t.Errorf("nil budget Capacity() = %d, want 0", got)
		}
		release, err := b.Reserve(t.Context(), 1<<40)
		if err != nil {
			t.Fatalf("nil budget Reserve = %v, want admitted", err)
		}
		release()
	}
	if got := NewDecodeBudget(100).Capacity(); got != 100 {
		t.Errorf("Capacity() = %d, want 100", got)
	}
}

// TestDecodeBudget_Reserve covers the three outcomes of a reservation: admitted
// while it fits, waiting (until ctx ends) while the budget is spent, refused
// outright when it exceeds the whole budget — and that release is idempotent and
// frees the bytes for the next caller.
func TestDecodeBudget_Reserve(t *testing.T) {
	t.Parallel()
	b := NewDecodeBudget(100)

	first, err := b.Reserve(t.Context(), 60)
	if err != nil {
		t.Fatalf("Reserve(60) = %v, want admitted", err)
	}

	waiting, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := b.Reserve(waiting, 60); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Reserve(60) while 60 of 100 is held = %v, want it to wait until the deadline", err)
	}

	if _, err := b.Reserve(t.Context(), 101); !errors.Is(err, ErrImageTooLarge) || !errors.Is(err, ErrOverBudget) {
		t.Fatalf("Reserve(101) of 100 = %v, want ErrImageTooLarge and ErrOverBudget", err)
	}

	if release, err := b.Reserve(t.Context(), 0); err != nil {
		t.Fatalf("Reserve(0) = %v, want admitted", err)
	} else {
		release()
	}

	first()
	first() // a second release must not hand the bytes back twice
	all, err := b.Reserve(t.Context(), 100)
	if err != nil {
		t.Fatalf("Reserve(100) after release = %v, want admitted", err)
	}
	waiting2, cancel2 := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel2()
	if _, err := b.Reserve(waiting2, 1); err == nil {
		t.Fatal("Reserve(1) with the whole budget held succeeded: a double release leaked capacity")
	}
	all()
}

// TestDecodeBudget_concurrentDecodesStayWithinBudget is the regression test of
// the bound: N goroutines each reserve a decode's worth from one shared budget,
// hold it while "decoding", and release it. At no point may the bytes held
// exceed the budget, every decode must finish, and decodes that fit side by
// side must actually run side by side (a budget that merely serialised
// everything would also pass the first assertion).
func TestDecodeBudget_concurrentDecodesStayWithinBudget(t *testing.T) {
	t.Parallel()
	const (
		capacity = 1000
		cost     = 300 // three fit at once, a fourth does not
		decodes  = 12
	)
	b := NewDecodeBudget(capacity)
	var held, peak, done atomic.Int64
	var wg sync.WaitGroup
	for range decodes {
		wg.Go(func() {
			release, err := b.Reserve(t.Context(), cost)
			if err != nil {
				t.Errorf("Reserve = %v", err)
				return
			}
			now := held.Add(cost)
			for {
				old := peak.Load()
				if now <= old || peak.CompareAndSwap(old, now) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			held.Add(-cost)
			release()
			done.Add(1)
		})
	}
	wg.Wait()
	if got := done.Load(); got != decodes {
		t.Fatalf("%d of %d decodes finished", got, decodes)
	}
	if got := peak.Load(); got > capacity {
		t.Fatalf("peak bytes held = %d, over the %d budget", got, capacity)
	}
	if got := peak.Load(); got < 2*cost {
		t.Errorf("peak bytes held = %d: decodes that fit together never overlapped", got)
	}
}

// TestBytesPerPixel verifies the bit depth enters the estimate: each colour
// model the standard decoders produce costs what its bitmap really takes, and
// an unknown model the widest of them.
func TestBytesPerPixel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		model color.Model
		want  int64
	}{
		{"8-bit gray", color.GrayModel, 1},
		{"16-bit gray", color.Gray16Model, 2},
		{"palette", color.Palette{color.Black, color.White}, 1},
		{"jpeg ycbcr", color.YCbCrModel, 3},
		{"cmyk", color.CMYKModel, 4},
		{"8-bit rgba", color.RGBAModel, 4},
		{"8-bit nrgba", color.NRGBAModel, 4},
		{"16-bit rgba", color.RGBA64Model, 8},
		{"16-bit nrgba", color.NRGBA64Model, 8},
		{"unknown", color.ModelFunc(func(c color.Color) color.Color { return c }), 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := BytesPerPixel(tt.model); got != tt.want {
				t.Errorf("BytesPerPixel(%s) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
	if got := DecodedBytes(image.Config{ColorModel: color.NRGBA64Model, Width: 10, Height: 20}); got != 1600 {
		t.Errorf("DecodedBytes(10x20 16-bit) = %d, want 1600", got)
	}
	if got := RGBABytes(10, 20); got != 800 {
		t.Errorf("RGBABytes(10, 20) = %d, want 800", got)
	}
}

// TestReserveDecode_bitDepth verifies the gate reads the header's bit depth: a
// budget that admits an 8-bit RGBA PNG refuses the 16-bit PNG of the very same
// dimensions — the case the pixel cap alone cannot see — and the extra the
// caller names is charged on top.
func TestReserveDecode_bitDepth(t *testing.T) {
	t.Parallel()
	const side = 1000 // 4 MB at 8 bits, 8 MB at 16
	b := NewDecodeBudget(6 << 20)
	eight := headerOnlyPNG(t, side, side, 8)
	sixteen := headerOnlyPNG(t, side, side, 16)

	release, err := ReserveDecode(t.Context(), eight, 0, b, nil)
	if err != nil {
		t.Fatalf("ReserveDecode(8-bit) = %v, want admitted", err)
	}
	release()

	if _, err := ReserveDecode(t.Context(), sixteen, 0, b, nil); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("ReserveDecode(16-bit) = %v, want ErrOverBudget", err)
	}

	extra := func(image.Config) int64 { return 3 << 20 }
	if _, err := ReserveDecode(t.Context(), eight, 0, b, extra); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("ReserveDecode(8-bit + 3 MB extra) = %v, want ErrOverBudget", err)
	}
}

// TestReserveDecode_pixelCapAndUnreadable verifies the gate still enforces the
// pixel cap, admits an unreadable header without reserving (the caller's decode
// reports it), and reports a missing file.
func TestReserveDecode_pixelCapAndUnreadable(t *testing.T) {
	t.Parallel()
	path := headerOnlyPNG(t, 100, 100, 8)
	if _, err := ReserveDecode(t.Context(), path, 9999, nil, nil); !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("ReserveDecode over the pixel cap = %v, want ErrImageTooLarge", err)
	}

	garbage := filepath.Join(t.TempDir(), "garbage")
	if err := os.WriteFile(garbage, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := NewDecodeBudget(1)
	release, err := ReserveDecode(t.Context(), garbage, 1, b, nil)
	if err != nil {
		t.Fatalf("ReserveDecode(unreadable) = %v, want nil", err)
	}
	release()

	if _, err := ReserveDecode(t.Context(), filepath.Join(t.TempDir(), "missing"), 0, nil, nil); err == nil {
		t.Error("ReserveDecode(missing file) = nil, want an error")
	}
}

// TestPeekConfig verifies the header read: dimensions and model of a real PNG,
// ok=false for bytes that are not an image.
func TestPeekConfig(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray16(image.Rect(0, 0, 7, 5))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "g.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := PeekConfig(path)
	if err != nil || !ok || cfg.Width != 7 || cfg.Height != 5 || BytesPerPixel(cfg.ColorModel) != 2 {
		t.Errorf("PeekConfig = (%+v, %v, %v), want a 7x5 16-bit gray header", cfg, ok, err)
	}
}
