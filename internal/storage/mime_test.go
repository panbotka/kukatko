package storage

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// pngHeader is a minimal PNG signature plus IHDR start, enough for content
// sniffing to recognise image/png.
var pngHeader = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R'}

// jpegHeader is the JPEG SOI + JFIF marker prefix, recognised as image/jpeg.
var jpegHeader = []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'}

func TestDetectMIME(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header []byte
		file   string
		want   string
	}{
		{"png by content", pngHeader, "whatever.bin", "image/png"},
		{"jpeg by content", jpegHeader, "photo.dat", "image/jpeg"},
		{"gif by content", []byte("GIF89a......."), "x", "image/gif"},
		{"heic by extension", []byte{0, 0, 0, 0, 'f', 't', 'y', 'p'}, "IMG_0001.HEIC", "image/heic"},
		{"raw dng by extension", []byte{0x01, 0x02, 0x03, 0x04}, "shot.dng", "image/x-adobe-dng"},
		{"mov by extension", []byte{0, 0, 0, 0, 'm', 'o', 'o', 'v'}, "clip.mov", "video/quicktime"},
		{"unknown stays octet-stream", []byte{0x00, 0x01, 0x02, 0x03}, "mystery.xyz", octetStream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := detectMIME(tt.header, tt.file); got != tt.want {
				t.Errorf("detectMIME(%q) = %q, want %q", tt.file, got, tt.want)
			}
		})
	}
}

func TestFSStore_detectsMIME(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	when := time.Date(2024, time.May, 17, 0, 0, 0, 0, time.UTC)

	stored, err := fs.Store(t.Context(), bytes.NewReader(pngHeader), when, "pic.png")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if stored.MIME != "image/png" {
		t.Errorf("MIME = %q, want image/png", stored.MIME)
	}
}

func TestFSStore_mimeByExtensionWhenContentOpaque(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	when := time.Date(2024, time.May, 17, 0, 0, 0, 0, time.UTC)
	// An opaque HEIC-like body: content sniffing yields octet-stream, so the
	// .heic extension must drive detection.
	body := strings.Repeat("\x00\x01ftypheic", 8)

	stored, err := fs.Store(t.Context(), strings.NewReader(body), when, "IMG.heic")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if stored.MIME != "image/heic" {
		t.Errorf("MIME = %q, want image/heic", stored.MIME)
	}
}
