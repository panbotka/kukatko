// Package hls owns the object layout of Kukátko's HTTP Live Streaming renditions:
// where a transcoded video's segments live in the object store, and what may be
// named inside that namespace.
//
// A video's segments live under
//
//	hls/<file_hash>/<rendition>/
//
// holding the fragmented-MP4 initialisation segment init.mp4 and the media
// segments themselves, numbered from zero and zero-padded to five digits —
// 00000.m4s, 00001.m4s, … Renditions are named in lowercase (the first is
// 1080p), so one hash's prefix holds one directory per quality level.
//
// Playlists are deliberately absent from that list: the .m3u8 a player fetches is
// generated per request from what the catalogue knows, never stored. A stored
// playlist would be a second source of truth that goes stale the moment a
// rendition is added, removed or re-encoded, and it would have to be rewritten
// whenever the URLs it points at change.
//
// The package also holds the two pure halves of streaming that surround those
// keys: the encoding plan — the renditions a video is encoded into and the ffmpeg
// argument list for one of them (EncodeArgs) — and the rendering of the playlists
// a player is served (RewriteMedia, BuildMaster). Both are ordinary functions over
// their arguments.
//
// What the package never does is act. It builds keys, validates names, describes
// an encode and renders text; it does not run ffmpeg, talk to the store or touch
// the database. That is what lets every layer that touches HLS — the encoder, the
// purge, the library wipe and the HTTP endpoints — agree on the same strings
// without depending on each other, and it is why the whole plan can be tested on a
// machine with no ffmpeg installed.
package hls

import (
	"errors"
	"fmt"
	"regexp"
)

// Prefix is the top-level key prefix under which every HLS object lives. It is
// exported so the operations that reason about whole prefixes rather than single
// keys — the library wipe and its orphan sweep — can name the one this package
// owns instead of hardcoding the string.
const Prefix = "hls"

// InitName is the fragmented-MP4 initialisation segment every rendition starts
// with. A player fetches it once per rendition, before any media segment.
const InitName = "init.mp4"

// Rendition1080p is the first (and, for now, only) rendition name. It is part of
// the layout contract rather than an encoder detail: the name appears verbatim in
// object keys, so it is fixed here where the keys are built.
const Rendition1080p = "1080p"

// maxRenditionLen bounds a rendition name. Real names are short ("1080p", "720p"),
// and a bound keeps a caller-supplied path segment from growing an object key
// without limit even while it satisfies the character rule.
const maxRenditionLen = 16

const (
	// minHashLen is the shortest file hash accepted. It matches what the thumbnail
	// cache requires, so the same catalogue hash addresses both layouts.
	minHashLen = 6
	// maxHashLen is the longest file hash accepted: a SHA256 hex digest, which is
	// what the catalogue stores.
	maxHashLen = 64
)

// Sentinel errors returned by this package so callers (the encoder, the purge,
// the HTTP layer, tests) can branch with errors.Is.
var (
	// ErrInvalidHash indicates a file hash that is empty, over-long or not a
	// lowercase hex string.
	ErrInvalidHash = errors.New("hls: invalid file hash")
	// ErrInvalidRendition indicates a rendition name that is not a short,
	// lowercase alphanumeric token.
	ErrInvalidRendition = errors.New("hls: invalid rendition name")
	// ErrInvalidName indicates a file name that is neither the initialisation
	// segment nor a five-digit media segment.
	ErrInvalidName = errors.New("hls: invalid segment name")
)

// segmentNamePattern matches a media segment name exactly: five decimal digits
// and the fragment extension, nothing before or after. The padding is fixed
// rather than merely "some digits" because it is what makes the keys sort in
// playback order, and because a lax rule here is a lax rule at the one place a
// request's path segment becomes an object key.
var segmentNamePattern = regexp.MustCompile(`^[0-9]{5}\.m4s$`)

// renditionPattern matches a rendition name: lowercase letters and digits only.
// No dot and no slash can pass it, so a rendition can never climb out of its
// video's prefix or masquerade as a file.
var renditionPattern = regexp.MustCompile(`^[a-z0-9]+$`)

// PrefixFor returns the key prefix hls/<file_hash>/ holding every rendition of
// the video with the given file hash, or ErrInvalidHash for a malformed one.
//
// The trailing slash is part of the answer: it confines a prefix listing to this
// video's objects, where a bare hls/<hash> would also match the objects of any
// hash that happens to start with it.
func PrefixFor(fileHash string) (string, error) {
	if err := validateHash(fileHash); err != nil {
		return "", err
	}
	return Prefix + "/" + fileHash + "/", nil
}

// Key returns the object key hls/<file_hash>/<rendition>/<name> for one HLS
// object. It validates all three parts and returns ErrInvalidHash,
// ErrInvalidRendition or ErrInvalidName rather than a key built from anything it
// does not recognise.
//
//	key, err := hls.Key(photo.FileHash, hls.Rendition1080p, "00007.m4s")
//	// key == "hls/<hash>/1080p/00007.m4s"
//
// It is the only way an HLS object key is spelled. Two of its three parts reach
// it straight from a request path when a player asks for a segment, so the
// validation is not a courtesy to the caller — it is what stands between that
// path and the store.
func Key(fileHash, rendition, name string) (string, error) {
	prefix, err := PrefixFor(fileHash)
	if err != nil {
		return "", err
	}
	if err := ValidateRendition(rendition); err != nil {
		return "", err
	}
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return prefix + rendition + "/" + name, nil
}

// ValidateName reports whether name is one this layout can hold: either the
// initialisation segment init.mp4, or exactly five digits followed by .m4s. It
// returns ErrInvalidName for everything else — an empty string, a traversal
// segment, wrong padding, a stray extension, a playlist (which is never stored).
func ValidateName(name string) error {
	if name == InitName || segmentNamePattern.MatchString(name) {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInvalidName, name)
}

// ValidateRendition reports whether rendition is a well-formed rendition name —
// one to maxRenditionLen lowercase letters and digits — returning
// ErrInvalidRendition otherwise.
func ValidateRendition(rendition string) error {
	if len(rendition) > maxRenditionLen || !renditionPattern.MatchString(rendition) {
		return fmt.Errorf("%w: %q", ErrInvalidRendition, rendition)
	}
	return nil
}

// validateHash reports whether fileHash is a lowercase hex string of a plausible
// length for a content digest, returning ErrInvalidHash otherwise.
func validateHash(fileHash string) error {
	if len(fileHash) < minHashLen || len(fileHash) > maxHashLen {
		return fmt.Errorf("%w: %q has length %d", ErrInvalidHash, fileHash, len(fileHash))
	}
	for _, r := range fileHash {
		if !isHexDigit(r) {
			return fmt.Errorf("%w: %q is not hex", ErrInvalidHash, fileHash)
		}
	}
	return nil
}

// isHexDigit reports whether r is a lowercase hexadecimal digit.
func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}
