package sidecar

import (
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/panbotka/kukatko/internal/video"
)

// The sidecar extensions this package reads. `.aae` (Apple's edit description)
// and `.thm` (a video's thumbnail) are sidecars too, but neither is metadata.
const (
	extJSON = ".json"
	extXMP  = ".xmp"
)

// supplementalSuffix is the name Google gives its current sidecars:
// `IMG_1234.jpg.supplemental-metadata.json`. Google caps the whole sidecar file
// name, so on a long media name this suffix arrives cut short at an arbitrary
// point ("…supplemental-me.json"), which is why it is matched as a prefix.
const supplementalSuffix = "supplemental-metadata"

// minPrefixStem is the shortest sidecar stem allowed to claim a media file by
// truncation. Google truncates long names, not short ones, so a two-character
// stem matching half the folder is a bug, not an export.
const minPrefixStem = 4

// exportMetadataNames are Takeout's own bookkeeping files. They are not media
// sidecars: `metadata.json` describes an *album*, which Kukátko deliberately
// does not import (the export is full of auto-generated albums from the phone —
// album membership comes from `--album` instead). They are ignored outright, so
// they are never reported as sidecars that matched nothing.
var exportMetadataNames = map[string]struct{}{
	"metadata":                     {},
	"album metadata":               {},
	"print-subscriptions":          {},
	"shared_album_comments":        {},
	"user-generated-memory-titles": {},
}

// indexRe matches the copy index Google appends to a duplicate file name, e.g.
// the "(1)" of `IMG_1234(1).jpg`.
var indexRe = regexp.MustCompile(`^(.*)\((\d+)\)$`)

// Matches is the result of pairing the sidecars in a folder with its media
// files. Everything that did not pair is reported rather than dropped: a silent
// mismatch is how someone loses a decade of capture dates.
type Matches struct {
	// Pairs maps a media file path to the sidecar that describes it. A media file
	// takes at most one sidecar; when both a Takeout JSON and an XMP describe it,
	// the JSON wins (it is the richer of the two).
	Pairs map[string]string
	// Orphans are sidecar paths that describe no media file in their directory,
	// sorted. Either the media was not exported, or the naming defeated the
	// matcher — both are worth saying out loud.
	Orphans []string
	// Missing are media paths with no sidecar, sorted, reported only for
	// directories that hold sidecars at all: in an export folder a photo without
	// its JSON is a photo about to lose its date, while in an ordinary folder of
	// camera files it is simply the normal case and not worth a word.
	Missing []string
}

// IsSidecar reports whether path names a sidecar this package can read.
func IsSidecar(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case extJSON:
		return !isExportMetadata(path)
	case extXMP:
		return true
	default:
		return false
	}
}

// Match pairs sidecar files with the media files they describe. Matching is
// per-directory — an export writes the sidecar next to its media — and survives
// every Takeout naming variant seen in the wild:
//
//	IMG_1234.jpg.json                              the original form
//	IMG_1234.jpg.supplemental-metadata.json        the current form
//	IMG_1234.jpg.supplemental-me.json              …cut short by the name cap
//	IMG_1234.jp.json                               a long name cut into the extension
//	IMG_1234.jpg(1).json  ↔  IMG_1234(1).jpg       the copy index moves
//	IMG_1234.jpg.xmp / IMG_1234.xmp                Apple's standalone XMP
//
// Exact matches are resolved first, so a truncated stem can never steal a media
// file that owns its sidecar outright.
func Match(media, sidecars []string) Matches {
	out := Matches{Pairs: make(map[string]string)}
	byDir := groupByDir(media, sidecars)
	for _, dir := range slices.Sorted(maps.Keys(byDir)) {
		matchDir(byDir[dir], &out)
	}
	slices.Sort(out.Orphans)
	slices.Sort(out.Missing)
	return out
}

// dirFiles is the media and the sidecars of one directory.
type dirFiles struct {
	media    []*mediaFile
	sidecars []string
}

// mediaFile is one media file as the matcher sees it: its canonical name (the
// copy index lifted out, lower-cased) and whether a sidecar has claimed it.
type mediaFile struct {
	path string
	// name is the canonical, lower-cased file name, e.g. "img_1234.jpg".
	name string
	// stem is name without its extension, e.g. "img_1234" — what an Apple XMP
	// sidecar is named after.
	stem string
	// index is the copy index Google appends to duplicate names, 0 when absent.
	index int
	// video reports whether this is a video, used to break the tie when an XMP
	// could describe either half of a Live Photo pair.
	video bool
	// sidecar is the sidecar that claimed this file, "" while unclaimed.
	sidecar string
}

// groupByDir buckets media and sidecar paths by their directory, skipping files
// that are not sidecars this package reads (Apple's `.aae` edit descriptions and
// Takeout's own album `metadata.json` among them).
func groupByDir(media, sidecars []string) map[string]*dirFiles {
	byDir := make(map[string]*dirFiles)
	dir := func(path string) *dirFiles {
		key := filepath.Dir(path)
		if _, ok := byDir[key]; !ok {
			byDir[key] = &dirFiles{}
		}
		return byDir[key]
	}
	for _, path := range media {
		files := dir(path)
		files.media = append(files.media, newMediaFile(path))
	}
	for _, path := range sidecars {
		if !IsSidecar(path) {
			continue
		}
		files := dir(path)
		files.sidecars = append(files.sidecars, path)
	}
	return byDir
}

// newMediaFile canonicalises one media path for matching.
func newMediaFile(path string) *mediaFile {
	name := filepath.Base(path)
	base, index := splitIndex(name)
	canonical := strings.ToLower(base)
	return &mediaFile{
		path:  path,
		name:  canonical,
		stem:  strings.TrimSuffix(canonical, strings.ToLower(filepath.Ext(canonical))),
		index: index,
		video: video.IsVideoPath(name),
	}
}

// matchDir pairs the sidecars of one directory with its media files and records
// what did not pair. Takeout JSON sidecars are offered first so that, when a
// media file has both, the JSON — the richer sidecar — wins.
func matchDir(files *dirFiles, out *Matches) {
	ordered := slices.Clone(files.sidecars)
	slices.SortFunc(ordered, byJSONFirst)

	for _, path := range ordered {
		target := claim(files.media, path)
		switch {
		case target == nil:
			out.Orphans = append(out.Orphans, path)
		case target.sidecar == "":
			target.sidecar = path
			out.Pairs[target.path] = path
		}
		// A media file that already has a sidecar keeps it: the second sidecar
		// matched a real file, so it is not an orphan, it is merely redundant.
	}
	if len(files.sidecars) == 0 {
		return
	}
	for _, file := range files.media {
		if file.sidecar == "" {
			out.Missing = append(out.Missing, file.path)
		}
	}
}

// byJSONFirst orders Takeout JSON sidecars ahead of XMP ones, and orders
// otherwise by path so a run is deterministic.
func byJSONFirst(a, b string) int {
	rank := func(path string) int {
		if strings.EqualFold(filepath.Ext(path), extJSON) {
			return 0
		}
		return 1
	}
	if diff := rank(a) - rank(b); diff != 0 {
		return diff
	}
	return strings.Compare(a, b)
}

// claim returns the media file a sidecar describes, or nil when none does. Exact
// name matches are tried first, then the truncated-name fallback — the latter
// only when it is unambiguous, because a stem that fits two media files says
// nothing about which one it meant.
func claim(media []*mediaFile, path string) *mediaFile {
	keys := sidecarKeys(path)
	for _, key := range keys {
		if file := exactMatch(media, key); file != nil {
			return file
		}
	}
	for _, key := range keys {
		if file := prefixMatch(media, key); file != nil {
			return file
		}
	}
	return nil
}

// sidecarKey is what a sidecar file name says about the media file it describes:
// the media's name (or, for an XMP, possibly only its extension-less stem) plus
// the copy index they share.
type sidecarKey struct {
	// name is the canonical, lower-cased media name the sidecar points at.
	name string
	// index is the copy index carried by either side of the pair.
	index int
	// stemOnly marks a key that names the media without its extension, which is
	// how Apple writes `IMG_1234.xmp` beside `IMG_1234.HEIC`.
	stemOnly bool
}

// sidecarKeys derives, in priority order, the media names a sidecar file name
// could refer to. A Takeout name yields two readings — with and without the
// (possibly truncated) `supplemental-metadata` suffix — because the suffix is
// only recognisable by its prefix and a media name could, in principle, end the
// same way.
func sidecarKeys(path string) []sidecarKey {
	name := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(name))
	stem, index := splitTrailingIndex(strings.TrimSuffix(name, filepath.Ext(name)))
	if ext == extXMP {
		return xmpKeys(stem, index)
	}

	keys := make([]sidecarKey, 0, 2)
	if trimmed, ok := trimSupplemental(stem); ok {
		keys = append(keys, mediaNameKey(trimmed, index))
	}
	return append(keys, mediaNameKey(stem, index))
}

// xmpKeys derives the media names an XMP sidecar could describe: `IMG_1234.jpg`
// for `IMG_1234.jpg.xmp`, and any `IMG_1234.*` for `IMG_1234.xmp`.
func xmpKeys(stem string, index int) []sidecarKey {
	full := mediaNameKey(stem, index)
	bare := full
	bare.stemOnly = true
	bare.name = strings.TrimSuffix(full.name, strings.ToLower(filepath.Ext(full.name)))
	return []sidecarKey{full, bare}
}

// mediaNameKey canonicalises a sidecar stem into the media name it points at,
// lifting out a copy index the stem carries itself (`IMG_1234(1).jpg.json`) and
// keeping the one already found at the end of the sidecar name
// (`IMG_1234.jpg(1).json`).
func mediaNameKey(stem string, index int) sidecarKey {
	base, embedded := splitIndex(stem)
	if index == 0 {
		index = embedded
	}
	return sidecarKey{name: strings.ToLower(base), index: index}
}

// exactMatch returns the unclaimed media file whose canonical name the key
// names. When the key names only a stem (an Apple `IMG_1234.xmp` beside both
// `IMG_1234.HEIC` and `IMG_1234.MOV`), the still image wins: an XMP describes
// the photo, not the Live Photo's video half.
func exactMatch(media []*mediaFile, key sidecarKey) *mediaFile {
	return pick(media, key, func(file *mediaFile) bool {
		if key.stemOnly {
			return file.stem == key.name
		}
		return file.name == key.name
	})
}

// prefixMatch returns the one media file whose name starts with the key's —
// Google's answer to a long file name is to cut the sidecar's name short, so
// `IMG_1234.jp.json` belongs to `IMG_1234.jpg`. An ambiguous prefix (two media
// files start with it) matches nothing: a guess here silently attaches one
// photo's history to another.
func prefixMatch(media []*mediaFile, key sidecarKey) *mediaFile {
	if len(key.name) < minPrefixStem {
		return nil
	}
	return pick(media, key, func(file *mediaFile) bool {
		return strings.HasPrefix(file.name, key.name)
	})
}

// pick returns the single media file with the key's copy index that satisfies
// the predicate, preferring a still image over a video when several qualify (an
// XMP beside both halves of a Live Photo describes the photo). It returns nil
// when the choice is ambiguous. A file another sidecar already claimed is still
// a match — the caller keeps the first claim and treats the second sidecar as
// redundant rather than orphaned.
func pick(media []*mediaFile, key sidecarKey, match func(*mediaFile) bool) *mediaFile {
	var found []*mediaFile
	for _, file := range media {
		if file.index == key.index && match(file) {
			found = append(found, file)
		}
	}
	switch len(found) {
	case 0:
		return nil
	case 1:
		return found[0]
	}
	return preferStill(found)
}

// preferStill breaks a tie between several candidate media files in favour of
// the single still image among them, and gives up when even that is ambiguous.
func preferStill(found []*mediaFile) *mediaFile {
	var still *mediaFile
	for _, file := range found {
		if file.video {
			continue
		}
		if still != nil {
			return nil
		}
		still = file
	}
	return still
}

// splitIndex lifts a copy index out of a file name: `IMG_1234(1).jpg` is
// `IMG_1234.jpg` with index 1. A name without one keeps its shape and index 0.
func splitIndex(name string) (string, int) {
	ext := filepath.Ext(name)
	base, index := splitTrailingIndex(strings.TrimSuffix(name, ext))
	return base + ext, index
}

// splitTrailingIndex strips a trailing `(N)` from a string, returning the string
// and the index (0 when there is none).
func splitTrailingIndex(s string) (string, int) {
	match := indexRe.FindStringSubmatch(s)
	if match == nil {
		return s, 0
	}
	index, err := strconv.Atoi(match[2])
	if err != nil {
		return s, 0
	}
	return match[1], index
}

// trimSupplemental removes Google's `.supplemental-metadata` suffix, in whatever
// truncated form the file-name cap left of it, and reports whether it did.
func trimSupplemental(stem string) (string, bool) {
	dot := strings.LastIndex(stem, ".")
	if dot < 0 {
		return stem, false
	}
	suffix := strings.ToLower(stem[dot+1:])
	if suffix == "" || !strings.HasPrefix(supplementalSuffix, suffix) {
		return stem, false
	}
	return stem[:dot], true
}

// isExportMetadata reports whether a JSON file is one of Takeout's own
// bookkeeping documents (an album description, a subscription list) rather than
// a media sidecar.
func isExportMetadata(path string) bool {
	name := filepath.Base(path)
	stem, _ := splitTrailingIndex(strings.TrimSuffix(name, filepath.Ext(name)))
	_, ok := exportMetadataNames[strings.ToLower(strings.TrimSpace(stem))]
	return ok
}
