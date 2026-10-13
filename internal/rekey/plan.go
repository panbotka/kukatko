package rekey

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storekeys"
	"github.com/panbotka/kukatko/internal/thumb"
)

// mapTagPrefix names the media-playlist tag carrying the initialisation
// segment's URI, and uriAttrPrefix how that tag introduces it.
const (
	mapTagPrefix  = "#EXT-X-MAP:"
	uriAttrPrefix = `URI="`
)

// Plan lists every object photo occupies in the store and where each moves to
// under newPrefix: the original, every thumbnail size the thumbnailer knows, the
// initialisation and media segments of every recorded streaming rendition, and
// the metadata sidecar. The original comes first. The new key of each is
// storekeys.Join(newPrefix, rest) where rest is the old key with its own prefix
// stripped, so the tree under the new prefix is the old tree verbatim.
//
// It is pure: it asks the store nothing, so it lists keys whether or not an
// object exists there — a thumbnail size never rendered, a sidecar never
// written. The caller copies what exists and skips the rest. A malformed
// newPrefix plans nothing, and a malformed file hash plans no derived object
// keyed by it.
//
// It walks storekeys.Kinds() rather than calling the planners in a row, so a new
// kind of object in the store fails the exhaustive linter here until this
// package says what moving a project does with it.
func Plan(photo PhotoFiles, newPrefix string) []Move {
	if storekeys.ValidatePrefix(newPrefix) != nil || photo.FilePath == "" {
		return nil
	}
	var keys []planned
	for _, kind := range storekeys.Kinds() {
		keys = append(keys, planKind(kind, photo)...)
	}
	moves := make([]Move, 0, len(keys))
	for _, key := range keys {
		_, rest := storekeys.Strip(key.key)
		moves = append(moves, Move{From: key.key, To: storekeys.Join(newPrefix, rest), Kind: key.kind})
	}
	return moves
}

// planned is one current key and its kind, before the new key is derived.
type planned struct {
	key  string
	kind storekeys.Kind
}

// planKind returns the keys of one kind that photo occupies.
//
// The switch has no default clause on purpose (see Plan). The project-wide
// genealogy export belongs to no photo and is moved by Finish; dumps, partial
// uploads and foreign objects are nobody's photo.
func planKind(kind storekeys.Kind, photo PhotoFiles) []planned {
	switch kind {
	case storekeys.KindOriginal:
		return []planned{{key: photo.FilePath, kind: storekeys.KindOriginal}}
	case storekeys.KindSidecar:
		key, err := sidecarexport.KeyFor(photo.FilePath)
		if err != nil {
			return nil
		}
		return []planned{{key: key, kind: storekeys.KindSidecar}}
	case storekeys.KindThumbnail:
		return planThumbs(photo)
	case storekeys.KindHLS:
		return planHLS(photo)
	case storekeys.KindFamilies, storekeys.KindDump, storekeys.KindPartial, storekeys.KindForeign:
		return nil
	}
	return nil
}

// planThumbs returns the key of every registered thumbnail size of photo, under
// the prefix of its own original.
func planThumbs(photo PhotoFiles) []planned {
	prefix := storekeys.PrefixOf(photo.FilePath)
	sizes := thumb.SizeNames()
	out := make([]planned, 0, len(sizes))
	for _, size := range sizes {
		key, err := thumb.RelPath(prefix, photo.FileHash, size)
		if err != nil {
			return nil
		}
		out = append(out, planned{key: key, kind: storekeys.KindThumbnail})
	}
	return out
}

// planHLS returns the keys of every object of every recorded rendition of
// photo: the names its media playlist lists, together with init.mp4 and the
// segment names the recorded count implies — a union, so neither a playlist
// that spells a name differently nor a count that is off strands an object.
func planHLS(photo PhotoFiles) []planned {
	prefix := storekeys.PrefixOf(photo.FilePath)
	var out []planned
	for _, rendition := range photo.Renditions {
		for _, name := range renditionNames(rendition) {
			key, err := hls.Key(prefix, photo.FileHash, rendition.Name, name)
			if err != nil {
				continue
			}
			out = append(out, planned{key: key, kind: storekeys.KindHLS})
		}
	}
	return out
}

// renditionNames returns, sorted and without duplicates, the object names of
// one rendition: init.mp4, 00000.m4s up to the recorded segment count, and
// every valid name the media playlist lists.
func renditionNames(rendition Rendition) []string {
	names := make([]string, 0, 1+max(rendition.SegmentCount, 0))
	names = append(names, hls.InitName)
	for index := range rendition.SegmentCount {
		names = append(names, fmt.Sprintf("%05d.m4s", index))
	}
	names = append(names, playlistNames(rendition.Playlist)...)
	slices.Sort(names)
	return slices.Compact(names)
}

// playlistNames returns the object names a media playlist lists: the URI of its
// EXT-X-MAP tag and every segment line, each reduced to its base name. A name
// the layout cannot hold is dropped.
func playlistNames(playlist string) []string {
	var names []string
	for line := range strings.Lines(playlist) {
		line = strings.TrimSpace(line)
		var uri string
		switch {
		case strings.HasPrefix(line, mapTagPrefix):
			uri = mapURI(line)
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		default:
			uri = line
		}
		if name := path.Base(uri); hls.ValidateName(name) == nil {
			names = append(names, name)
		}
	}
	return names
}

// mapURI returns the quoted URI attribute of an EXT-X-MAP tag, or "".
func mapURI(line string) string {
	_, after, found := strings.Cut(line, uriAttrPrefix)
	if !found {
		return ""
	}
	uri, _, found := strings.Cut(after, `"`)
	if !found {
		return ""
	}
	return uri
}
