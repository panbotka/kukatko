package sidecar

import (
	"path/filepath"
	"slices"
	"testing"
)

// TestMatchNamingVariants walks the Takeout filename minefield: the sidecar name
// has changed across export versions, is capped in length (so it arrives cut
// short), and moves the copy index of a duplicate from one side of the extension
// to the other. Every one of these must find its photo — a sidecar that does not
// is a capture date lost.
func TestMatchNamingVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		media   string
		sidecar string
	}{
		{"original form", "IMG_1234.jpg", "IMG_1234.jpg.json"},
		{"current form", "IMG_1234.jpg", "IMG_1234.jpg.supplemental-metadata.json"},
		{"supplemental cut short", "IMG_1234.jpg", "IMG_1234.jpg.supplemental-me.json"},
		{"supplemental cut to nothing", "IMG_1234.jpg", "IMG_1234.jpg.s.json"},
		{"name cut into the extension", "IMG_1234.jpg", "IMG_1234.jp.json"},
		{"name cut before the extension", "IMG_1234.jpg", "IMG_1234.json"},
		{"copy index moves across the extension", "IMG_1234(1).jpg", "IMG_1234.jpg(1).json"},
		{"copy index with the supplemental suffix", "IMG_1234(2).jpg", "IMG_1234.jpg.supplemental-metadata(2).json"},
		{"copy index on the media side only", "IMG_1234(3).jpg", "IMG_1234(3).jpg.json"},
		{"case-insensitive extension", "IMG_1234.JPG", "IMG_1234.jpg.json"},
		{"apple xmp beside the file", "IMG_1234.HEIC", "IMG_1234.HEIC.xmp"},
		{"apple xmp named after the stem", "IMG_1234.HEIC", "IMG_1234.xmp"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			media := filepath.Join("Takeout", tc.media)
			side := filepath.Join("Takeout", tc.sidecar)

			got := Match([]string{media}, []string{side})
			if got.Pairs[media] != side {
				t.Errorf("Pairs[%s] = %q, want %q", tc.media, got.Pairs[media], side)
			}
			if len(got.Orphans) != 0 {
				t.Errorf("Orphans = %v, want none", got.Orphans)
			}
			if len(got.Missing) != 0 {
				t.Errorf("Missing = %v, want none", got.Missing)
			}
		})
	}
}

// TestMatchReportsWhatDidNotPair is the whole reason the matcher reports rather
// than guesses: a sidecar whose photo was not exported, and a photo whose sidecar
// was not, are both quiet ways to lose a decade of dates.
func TestMatchReportsWhatDidNotPair(t *testing.T) {
	t.Parallel()

	media := []string{"T/IMG_1.jpg", "T/IMG_2.jpg"}
	sidecars := []string{"T/IMG_1.jpg.json", "T/IMG_gone.jpg.json"}

	got := Match(media, sidecars)

	if len(got.Pairs) != 1 || got.Pairs["T/IMG_1.jpg"] != "T/IMG_1.jpg.json" {
		t.Errorf("Pairs = %v, want only IMG_1", got.Pairs)
	}
	if !slices.Equal(got.Orphans, []string{"T/IMG_gone.jpg.json"}) {
		t.Errorf("Orphans = %v, want the sidecar of the photo that was not exported", got.Orphans)
	}
	if !slices.Equal(got.Missing, []string{"T/IMG_2.jpg"}) {
		t.Errorf("Missing = %v, want the photo whose sidecar was not exported", got.Missing)
	}
}

// TestMatchQuietInAnOrdinaryFolder keeps the report honest: a folder of camera
// files with no sidecars at all is not a folder of photos "missing" their
// sidecars, and must say nothing.
func TestMatchQuietInAnOrdinaryFolder(t *testing.T) {
	t.Parallel()

	got := Match([]string{"cam/DSC_0001.jpg", "cam/DSC_0002.jpg"}, nil)

	if len(got.Pairs) != 0 || len(got.Orphans) != 0 || len(got.Missing) != 0 {
		t.Errorf("Match(no sidecars) = %+v, want an empty report", got)
	}
}

// TestMatchAmbiguousTruncationMatchesNothing guards the one case where a guess
// would be worse than a miss: a truncated stem that fits two photos says nothing
// about which one it meant, and attaching one photo's history to another is not a
// recoverable mistake.
func TestMatchAmbiguousTruncationMatchesNothing(t *testing.T) {
	t.Parallel()

	media := []string{"T/IMG_12345.jpg", "T/IMG_12346.jpg"}
	got := Match(media, []string{"T/IMG_1234.json"})

	if len(got.Pairs) != 0 {
		t.Errorf("Pairs = %v, want none: the stem fits both photos", got.Pairs)
	}
	if !slices.Equal(got.Orphans, []string{"T/IMG_1234.json"}) {
		t.Errorf("Orphans = %v, want the ambiguous sidecar", got.Orphans)
	}
}

// TestMatchExactBeatsTruncated makes sure a truncated stem cannot steal a photo
// that owns its sidecar outright.
func TestMatchExactBeatsTruncated(t *testing.T) {
	t.Parallel()

	media := []string{"T/IMG_1234.jpg", "T/IMG_1234_edited.jpg"}
	sidecars := []string{"T/IMG_1234.jpg.json", "T/IMG_1234_edited.jpg.json"}

	got := Match(media, sidecars)

	if got.Pairs["T/IMG_1234.jpg"] != "T/IMG_1234.jpg.json" {
		t.Errorf("Pairs[IMG_1234.jpg] = %q", got.Pairs["T/IMG_1234.jpg"])
	}
	if got.Pairs["T/IMG_1234_edited.jpg"] != "T/IMG_1234_edited.jpg.json" {
		t.Errorf("Pairs[IMG_1234_edited.jpg] = %q", got.Pairs["T/IMG_1234_edited.jpg"])
	}
	if len(got.Orphans) != 0 || len(got.Missing) != 0 {
		t.Errorf("Orphans = %v, Missing = %v, want none", got.Orphans, got.Missing)
	}
}

// TestMatchTakeoutAlbumMetadataIgnored keeps Takeout's own bookkeeping out of the
// report: `metadata.json` describes an album, and Kukátko imports no albums from
// an export (they are full of auto-generated junk from the phone). It is neither
// a sidecar nor an orphan — it simply is not media metadata.
func TestMatchTakeoutAlbumMetadataIgnored(t *testing.T) {
	t.Parallel()

	got := Match([]string{"T/IMG_1.jpg"}, []string{"T/metadata.json", "T/IMG_1.jpg.json"})

	if len(got.Orphans) != 0 {
		t.Errorf("Orphans = %v, want none: an album's metadata.json is not a media sidecar", got.Orphans)
	}
	if got.Pairs["T/IMG_1.jpg"] != "T/IMG_1.jpg.json" {
		t.Errorf("Pairs = %v", got.Pairs)
	}
	if IsSidecar("T/metadata.json") {
		t.Error("IsSidecar(metadata.json) = true, want false")
	}
}

// TestMatchPerDirectory keeps the pairing local: an export writes the sidecar
// beside its media, and a same-named photo one folder over is a different photo.
func TestMatchPerDirectory(t *testing.T) {
	t.Parallel()

	media := []string{filepath.Join("a", "IMG_1.jpg"), filepath.Join("b", "IMG_1.jpg")}
	sidecars := []string{filepath.Join("a", "IMG_1.jpg.json")}

	got := Match(media, sidecars)

	if got.Pairs[media[0]] != sidecars[0] {
		t.Errorf("Pairs[a/IMG_1.jpg] = %q, want the sidecar in a/", got.Pairs[media[0]])
	}
	if _, paired := got.Pairs[media[1]]; paired {
		t.Errorf("b/IMG_1.jpg took a sidecar from another directory: %v", got.Pairs)
	}
	if len(got.Missing) != 0 {
		t.Errorf("Missing = %v, want none: b/ holds no sidecars at all", got.Missing)
	}
}

// TestMatchLivePhotoPrefersTheStill breaks the Apple tie: an `IMG_1234.xmp` beside
// both halves of a Live Photo describes the still, not the video.
func TestMatchLivePhotoPrefersTheStill(t *testing.T) {
	t.Parallel()

	media := []string{"T/IMG_1234.HEIC", "T/IMG_1234.MOV"}
	got := Match(media, []string{"T/IMG_1234.xmp"})

	if got.Pairs["T/IMG_1234.HEIC"] != "T/IMG_1234.xmp" {
		t.Errorf("Pairs = %v, want the XMP on the still image", got.Pairs)
	}
	if !slices.Equal(got.Missing, []string{"T/IMG_1234.MOV"}) {
		t.Errorf("Missing = %v, want the video half", got.Missing)
	}
}

// TestMatchJSONBeatsXMP settles a photo that has both: the Takeout JSON is the
// richer sidecar, and a media file takes only one.
func TestMatchJSONBeatsXMP(t *testing.T) {
	t.Parallel()

	got := Match([]string{"T/IMG_1.jpg"}, []string{"T/IMG_1.jpg.xmp", "T/IMG_1.jpg.json"})

	if got.Pairs["T/IMG_1.jpg"] != "T/IMG_1.jpg.json" {
		t.Errorf("Pairs = %v, want the JSON", got.Pairs)
	}
	if len(got.Orphans) != 0 {
		t.Errorf("Orphans = %v, want none: the XMP did match a photo, it was only redundant", got.Orphans)
	}
}

// TestIsSidecar covers what the walk offers the matcher: the two formats it
// reads, and the two Apple writes that are not metadata at all.
func TestIsSidecar(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"IMG_1.jpg.json": true,
		"IMG_1.JSON":     true,
		"IMG_1.xmp":      true,
		"IMG_1.AAE":      false,
		"IMG_1.thm":      false,
		"IMG_1.jpg":      false,
		"metadata.json":  false,
	}
	for name, want := range tests {
		if got := IsSidecar(name); got != want {
			t.Errorf("IsSidecar(%q) = %v, want %v", name, got, want)
		}
	}
}
