package stacks

import (
	"path/filepath"
	"strings"

	"github.com/panbotka/kukatko/internal/photos"
)

// rawExtensions are the camera RAW file extensions (without the dot). A RAW is
// demoted below a rendered JPEG/HEIC when picking a stack's primary because it is
// not the file a user wants to look at.
var rawExtensions = map[string]bool{
	"cr2": true, "cr3": true, "crw": true, "nef": true, "nrw": true, "arw": true,
	"sr2": true, "srf": true, "dng": true, "raf": true, "rw2": true, "orf": true,
	"pef": true, "srw": true, "x3f": true, "dcr": true, "kdc": true, "mrw": true,
	"3fr": true, "mef": true, "iiq": true, "rwl": true, "erf": true, "mos": true,
	"fff": true, "raw": true,
}

// PickPrimary returns the uid of the member a user is most likely to want to look
// at, making the rule explicit rather than emergent: a still is preferred over a
// video (so a Live Photo shows the photo, not the clip), a rendered image over a
// RAW, then the higher resolution, then the larger file, with the uid breaking
// ties so the choice is deterministic. It returns "" only for an empty slice.
func PickPrimary(members []photos.StackCandidate) string {
	if len(members) == 0 {
		return ""
	}
	best := members[0]
	for _, m := range members[1:] {
		if betterPrimary(best, m) {
			best = m
		}
	}
	return best.UID
}

// betterPrimary reports whether candidate b is a strictly better primary than a,
// comparing the preference dimensions in priority order.
func betterPrimary(a, b photos.StackCandidate) bool {
	if s := boolPref(isStill(a), isStill(b)); s != 0 {
		return s > 0
	}
	if s := boolPref(isRendered(a), isRendered(b)); s != 0 {
		return s > 0
	}
	if ra, rb := resolution(a), resolution(b); ra != rb {
		return rb > ra
	}
	if a.FileSize != b.FileSize {
		return b.FileSize > a.FileSize
	}
	// A total order for reproducibility: the lexicographically smaller uid wins.
	return b.UID < a.UID
}

// boolPref returns +1 when only b has the preferred trait, -1 when only a does,
// and 0 when they agree.
func boolPref(a, b bool) int {
	switch {
	case b && !a:
		return 1
	case a && !b:
		return -1
	default:
		return 0
	}
}

// isStill reports whether the candidate is a still image rather than a video, so
// the still of a Live Photo pairing is preferred over its motion clip.
func isStill(c photos.StackCandidate) bool {
	return c.MediaType != string(photos.MediaVideo)
}

// isRendered reports whether the candidate is a rendered image (JPEG/HEIC/…)
// rather than a camera RAW, judged by its file extension.
func isRendered(c photos.StackCandidate) bool {
	name := c.FileName
	if name == "" {
		name = c.OriginalName
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	return !rawExtensions[ext]
}

// resolution returns the candidate's pixel count, the tie-breaker after media
// kind, guarding against overflow by using 64-bit arithmetic.
func resolution(c photos.StackCandidate) int64 {
	return int64(c.FileWidth) * int64(c.FileHeight)
}
