package hls

import (
	"errors"
	"strings"
	"testing"
)

// testHash is a SHA256-shaped file hash, the shape every catalogued file has.
const testHash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"

// TestKey verifies the object key is assembled from the three parts in the order
// the layout contract fixes, for both kinds of object a rendition holds.
func TestKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		hash      string
		rendition string
		file      string
		want      string
	}{
		{
			name:      "init segment",
			hash:      testHash,
			rendition: Rendition1080p,
			file:      InitName,
			want:      "hls/" + testHash + "/1080p/init.mp4",
		},
		{
			name:      "first media segment",
			hash:      testHash,
			rendition: Rendition1080p,
			file:      "00000.m4s",
			want:      "hls/" + testHash + "/1080p/00000.m4s",
		},
		{
			name:      "another rendition",
			hash:      "aabbccddeeff",
			rendition: "720p",
			file:      "01234.m4s",
			want:      "hls/aabbccddeeff/720p/01234.m4s",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Key(tt.hash, tt.rendition, tt.file)
			if err != nil {
				t.Fatalf("Key(%q, %q, %q): %v", tt.hash, tt.rendition, tt.file, err)
			}
			if got != tt.want {
				t.Errorf("Key(%q, %q, %q) = %q, want %q", tt.hash, tt.rendition, tt.file, got, tt.want)
			}
		})
	}
}

// TestKey_rejects verifies every part of the key is validated, so a key is never
// built from input this package does not recognise.
func TestKey_rejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		hash      string
		rendition string
		file      string
		wantErr   error
	}{
		{name: "empty hash", hash: "", rendition: "1080p", file: InitName, wantErr: ErrInvalidHash},
		{name: "hash too short", hash: "aabb", rendition: "1080p", file: InitName, wantErr: ErrInvalidHash},
		{
			name: "hash too long", hash: strings.Repeat("a", maxHashLen+1),
			rendition: "1080p", file: InitName, wantErr: ErrInvalidHash,
		},
		{name: "hash not hex", hash: "zzzzzzzz", rendition: "1080p", file: InitName, wantErr: ErrInvalidHash},
		{name: "hash uppercase", hash: "AABBCCDDEEFF", rendition: "1080p", file: InitName, wantErr: ErrInvalidHash},
		{name: "hash with traversal", hash: "../../etc", rendition: "1080p", file: InitName, wantErr: ErrInvalidHash},
		{name: "empty rendition", hash: testHash, rendition: "", file: InitName, wantErr: ErrInvalidRendition},
		{name: "uppercase rendition", hash: testHash, rendition: "1080P", file: InitName, wantErr: ErrInvalidRendition},
		{name: "rendition traversal", hash: testHash, rendition: "..", file: InitName, wantErr: ErrInvalidRendition},
		{name: "rendition with slash", hash: testHash, rendition: "a/b", file: InitName, wantErr: ErrInvalidRendition},
		{name: "rendition with dot", hash: testHash, rendition: "1080.p", file: InitName, wantErr: ErrInvalidRendition},
		{
			name: "rendition too long", hash: testHash,
			rendition: strings.Repeat("a", maxRenditionLen+1), file: InitName, wantErr: ErrInvalidRendition,
		},
		{name: "bad file name", hash: testHash, rendition: "1080p", file: "playlist.m3u8", wantErr: ErrInvalidName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Key(tt.hash, tt.rendition, tt.file)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Key(%q, %q, %q) error = %v, want %v", tt.hash, tt.rendition, tt.file, err, tt.wantErr)
			}
			if got != "" {
				t.Errorf("Key(%q, %q, %q) = %q, want no key on error", tt.hash, tt.rendition, tt.file, got)
			}
		})
	}
}

// TestPrefixFor verifies the per-video prefix ends in a slash, which is what
// confines a prefix listing to that video's own objects.
func TestPrefixFor(t *testing.T) {
	t.Parallel()

	got, err := PrefixFor(testHash)
	if err != nil {
		t.Fatalf("PrefixFor(%q): %v", testHash, err)
	}
	want := "hls/" + testHash + "/"
	if got != want {
		t.Errorf("PrefixFor(%q) = %q, want %q", testHash, got, want)
	}
	key, err := Key(testHash, Rendition1080p, InitName)
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if !strings.HasPrefix(key, got) {
		t.Errorf("Key = %q, want it under the prefix %q", key, got)
	}
}

// TestPrefixFor_rejectsBadHash verifies a malformed hash yields no prefix, so a
// caller can never list or delete under a prefix it did not mean.
func TestPrefixFor_rejectsBadHash(t *testing.T) {
	t.Parallel()

	for _, hash := range []string{"", "xy", "../secret", "AABBCC", strings.Repeat("a", maxHashLen+1)} {
		got, err := PrefixFor(hash)
		if !errors.Is(err, ErrInvalidHash) {
			t.Errorf("PrefixFor(%q) error = %v, want ErrInvalidHash", hash, err)
		}
		if got != "" {
			t.Errorf("PrefixFor(%q) = %q, want no prefix on error", hash, got)
		}
	}
}

// TestValidateName verifies exactly two shapes of name are accepted — the
// initialisation segment and a five-digit media segment — and that everything a
// client could otherwise smuggle through a path segment is rejected.
func TestValidateName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		ok   bool
	}{
		{name: "init segment", file: "init.mp4", ok: true},
		{name: "first segment", file: "00000.m4s", ok: true},
		{name: "last five-digit segment", file: "99999.m4s", ok: true},
		{name: "empty", file: "", ok: false},
		{name: "four digits", file: "0000.m4s", ok: false},
		{name: "six digits", file: "000000.m4s", ok: false},
		{name: "unpadded", file: "7.m4s", ok: false},
		{name: "no extension", file: "00000", ok: false},
		{name: "wrong extension", file: "00000.ts", ok: false},
		{name: "double extension", file: "00000.m4s.exe", ok: false},
		{name: "playlist is never stored", file: "index.m3u8", ok: false},
		{name: "uppercase extension", file: "00000.M4S", ok: false},
		{name: "uppercase init", file: "INIT.MP4", ok: false},
		{name: "parent traversal", file: "..", ok: false},
		{name: "traversal to another prefix", file: "../../thumb/aa.jpg", ok: false},
		{name: "absolute path", file: "/etc/passwd", ok: false},
		{name: "nested path", file: "sub/00000.m4s", ok: false},
		{name: "leading space", file: " 00000.m4s", ok: false},
		{name: "trailing newline", file: "00000.m4s\n", ok: false},
		{name: "hidden dot file", file: ".init.mp4", ok: false},
		{name: "init with suffix", file: "init.mp4.bak", ok: false},
		{name: "non-ascii digits", file: "٠٠٠٠٠.m4s", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateName(tt.file)
			if tt.ok && err != nil {
				t.Errorf("ValidateName(%q) = %v, want nil", tt.file, err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalidName) {
				t.Errorf("ValidateName(%q) = %v, want ErrInvalidName", tt.file, err)
			}
		})
	}
}

// TestValidateRendition verifies the rendition names the layout allows and the
// ones it refuses, since a rendition is a directory component of the key.
func TestValidateRendition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rendition string
		ok        bool
	}{
		{name: "the first rendition", rendition: Rendition1080p, ok: true},
		{name: "another height", rendition: "720p", ok: true},
		{name: "letters only", rendition: "source", ok: true},
		{name: "empty", rendition: "", ok: false},
		{name: "uppercase", rendition: "1080P", ok: false},
		{name: "with dash", rendition: "1080-p", ok: false},
		{name: "with underscore", rendition: "1080_p", ok: false},
		{name: "with slash", rendition: "1080p/x", ok: false},
		{name: "traversal", rendition: "..", ok: false},
		{name: "too long", rendition: strings.Repeat("a", maxRenditionLen+1), ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRendition(tt.rendition)
			if tt.ok && err != nil {
				t.Errorf("ValidateRendition(%q) = %v, want nil", tt.rendition, err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalidRendition) {
				t.Errorf("ValidateRendition(%q) = %v, want ErrInvalidRendition", tt.rendition, err)
			}
		})
	}
}

// TestMIMEFor verifies the two media types an HLS object is stored with. They
// are decided by name because a fragmented-MP4 segment's leading box is not
// something content sniffing recognises as video.
func TestMIMEFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{InitName, InitMIME},
		{"00000.m4s", SegmentMIME},
		{"00007.m4s", SegmentMIME},
	}
	for _, tt := range tests {
		if got := MIMEFor(tt.name); got != tt.want {
			t.Errorf("MIMEFor(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
