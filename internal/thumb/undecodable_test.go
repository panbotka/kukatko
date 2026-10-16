package thumb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/panbotka/kukatko/internal/imgconvert"
)

// TestUndecodable_classifies proves which failures are marked as a state of the
// original (ErrUndecodable) and which stay server faults.
func TestUndecodable_classifies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil stays nil", err: nil, want: false},
		{name: "unsupported format", err: fmt.Errorf("prep: %w", imgconvert.ErrUnsupportedFormat), want: true},
		{name: "RAW without preview", err: fmt.Errorf("prep: %w", imgconvert.ErrNoEmbeddedPreview), want: true},
		{name: "over the pixel cap", err: fmt.Errorf("gate: %w", imgconvert.ErrImageTooLarge), want: true},
		{name: "converter refused the file", err: fmt.Errorf("heif: %w", &exec.ExitError{}), want: true},
		{name: "converter not installed", err: fmt.Errorf("prep: %w", imgconvert.ErrConverterMissing), want: false},
		{name: "stat failure", err: fmt.Errorf("prep: %w", os.ErrNotExist), want: false},
		{name: "anything else", err: errors.New("disk on fire"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := undecodable(context.Background(), tt.err)
			if !errors.Is(got, tt.err) {
				t.Errorf("undecodable(%v) = %v, lost the original error", tt.err, got)
			}
			if errors.Is(got, ErrUndecodable) != tt.want {
				t.Errorf("undecodable(%v) marked = %v, want %v", tt.err, errors.Is(got, ErrUndecodable), tt.want)
			}
		})
	}
}

// TestUndecodable_cancelledContextIsNotTheFiles proves a cancelled request is
// never blamed on the original, even when the error it produced looks like one
// (a killed converter exits non-zero, a cancelled RAW extraction finds no preview).
func TestUndecodable_cancelledContextIsNotTheFiles(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, err := range []error{
		fmt.Errorf("prep: %w", imgconvert.ErrNoEmbeddedPreview),
		fmt.Errorf("heif: %w", &exec.ExitError{}),
	} {
		if errors.Is(undecodable(ctx, err), ErrUndecodable) {
			t.Errorf("undecodable(cancelled, %v) marked the original", err)
		}
	}
}

// TestDecodeAndOrient_undecodableOriginals proves the decode marks the originals
// that can never become a picture, and only those: a truncated JPEG and bytes
// that are no image are ErrUndecodable, a file that is not there is not.
func TestDecodeAndOrient_undecodableOriginals(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for x := range 64 {
		img.Set(x, x%48, color.RGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encoding fixture: %v", err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return path
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "truncated JPEG", path: write("truncated.jpg", buf.Bytes()[:buf.Len()/2]), want: true},
		{name: "not an image", path: write("garbage.jpg", []byte("plain text, no pixels")), want: true},
		{name: "unsupported extension", path: write("notes.xyz", []byte("plain text")), want: true},
		{name: "missing file", path: filepath.Join(dir, "absent.jpg"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, release, err := decodeAndOrient(context.Background(), tt.path, 1, 0, nil, nil)
			release()
			if err == nil {
				t.Fatal("decodeAndOrient succeeded, want an error")
			}
			if errors.Is(err, ErrUndecodable) != tt.want {
				t.Errorf("decodeAndOrient error %v: marked undecodable = %v, want %v",
					err, errors.Is(err, ErrUndecodable), tt.want)
			}
		})
	}

	t.Run("a sound JPEG decodes", func(t *testing.T) {
		t.Parallel()
		got, release, err := decodeAndOrient(context.Background(), write("ok.jpg", buf.Bytes()), 1, 0, nil, nil)
		defer release()
		if err != nil {
			t.Fatalf("decodeAndOrient: %v", err)
		}
		if got.Bounds().Dx() != 64 {
			t.Errorf("decoded width = %d, want 64", got.Bounds().Dx())
		}
	})
}
