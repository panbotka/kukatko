package stacks

import (
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

func TestPickPrimary(t *testing.T) {
	t.Parallel()
	raw := photos.StackCandidate{UID: "raw", FileName: "IMG.CR2", FileWidth: 6000, FileHeight: 4000, FileSize: 30_000_000}
	jpeg := photos.StackCandidate{UID: "jpg", FileName: "IMG.jpg", FileWidth: 6000, FileHeight: 4000, FileSize: 8_000_000}
	small := photos.StackCandidate{UID: "sml", FileName: "IMG_small.jpg", FileWidth: 1024, FileHeight: 768, FileSize: 500_000}
	large := photos.StackCandidate{UID: "lrg", FileName: "IMG_large.jpg", FileWidth: 4000, FileHeight: 3000, FileSize: 4_000_000}
	still := photos.StackCandidate{UID: "still", FileName: "IMG.heic", MediaType: string(photos.MediaImage), FileWidth: 4032, FileHeight: 3024}
	clip := photos.StackCandidate{UID: "clip", FileName: "IMG.mov", MediaType: string(photos.MediaVideo), FileWidth: 1920, FileHeight: 1080}

	tests := []struct {
		name    string
		members []photos.StackCandidate
		want    string
	}{
		{name: "jpeg beats raw of equal resolution", members: []photos.StackCandidate{raw, jpeg}, want: "jpg"},
		{name: "raw order does not matter", members: []photos.StackCandidate{jpeg, raw}, want: "jpg"},
		{name: "higher resolution wins among rendered", members: []photos.StackCandidate{small, large}, want: "lrg"},
		{name: "still beats video for a live pairing", members: []photos.StackCandidate{clip, still}, want: "still"},
		{name: "empty selection yields no primary", members: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := PickPrimary(tt.members); got != tt.want {
				t.Errorf("PickPrimary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPickPrimary_deterministicTieBreak(t *testing.T) {
	t.Parallel()
	// Two identical rendered images differing only in uid: the smaller uid wins,
	// and it wins regardless of input order.
	a := photos.StackCandidate{UID: "aaa", FileName: "a.jpg", FileWidth: 100, FileHeight: 100}
	b := photos.StackCandidate{UID: "bbb", FileName: "b.jpg", FileWidth: 100, FileHeight: 100}
	if got := PickPrimary([]photos.StackCandidate{a, b}); got != "aaa" {
		t.Errorf("PickPrimary({a,b}) = %q, want aaa", got)
	}
	if got := PickPrimary([]photos.StackCandidate{b, a}); got != "aaa" {
		t.Errorf("PickPrimary({b,a}) = %q, want aaa", got)
	}
}
