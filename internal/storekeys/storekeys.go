// Package storekeys says what lives in Kukátko's object store and which layout
// owns it.
//
// The store has no per-application prefix: an original sits at YYYY/MM/<name> in
// the bucket root, and the derived artefacts sit beside it under prefixes of
// their own — thumb/ for the thumbnail cache, sidecars/ for the metadata
// sidecars, hls/ for a video's streaming segments — with the database dumps
// (db/) and the in-progress uploads (.tmp/) using the same key space again. So
// "what is this object?" cannot be answered by a single prefix test, and every
// operation that reasons about the store as a whole has to answer it somehow.
//
// This package is where it is answered once. The layouts themselves stay where
// they are built (internal/thumb, internal/sidecarexport, internal/hls,
// internal/storage); what lives here is only the mapping from a key to the kind
// of thing it is, so the whole-store operations — the backup, the storage
// migration, the library wipe — classify identically instead of each carrying
// its own list of prefixes to remember.
//
// # Adding a prefix
//
// A new kind of object in the store means a new Kind here. That is deliberate
// and it is the point of the package: every consumer switches over Kind without
// a default clause, so a new member does not compile-and-quietly-ignore — it
// fails the exhaustive linter in each operation until that operation says, in
// its own terms, what it does with the new kind. Before this package existed,
// the streaming segments were added to the store and two operations went on not
// knowing about them for four commits.
package storekeys

import (
	"regexp"
	"strings"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/thumb"
)

// Prefixes of the key spaces this package recognises but that no other package
// exports as a constant.
const (
	// DumpPrefix is where a backup's database dumps live. They belong to the
	// backup bucket rather than to the library's own store, but the two are
	// classified together because an operator can point one at the other, and a
	// dump must never then be mistaken for an original.
	DumpPrefix = "db/"
	// PartialPrefix is the in-progress upload directory of the filesystem
	// backend, whose contents are half-written files that no operation may treat
	// as finished objects. It mirrors internal/storage's own unexported constant.
	PartialPrefix = ".tmp/"
)

// Kind is what a key in the store holds.
type Kind int

const (
	// KindForeign is a key under none of Kukátko's layouts. Something else put it
	// in the store; every operation here treats it conservatively — it is never
	// deleted, and it is backed up rather than assumed to be regenerable.
	KindForeign Kind = iota
	// KindOriginal is an original media file at YYYY/MM/<name>. It is the only
	// kind that cannot be reproduced from anything else in the store.
	KindOriginal
	// KindSidecar is a metadata sidecar under sidecars/: the catalogue's
	// disaster-recovery copy, written by internal/sidecarexport.
	KindSidecar
	// KindThumbnail is a cached thumbnail under thumb/, regenerable from the
	// original by the thumbnail job.
	KindThumbnail
	// KindHLS is a video's HLS initialisation or media segment under hls/,
	// regenerable from the original by the streaming encode job.
	KindHLS
	// KindDump is a database dump under db/.
	KindDump
	// KindPartial is a half-written upload under .tmp/.
	KindPartial
)

// String returns the kind's name, for log lines and test failures.
func (k Kind) String() string {
	switch k {
	case KindOriginal:
		return "original"
	case KindSidecar:
		return "sidecar"
	case KindThumbnail:
		return "thumbnail"
	case KindHLS:
		return "hls"
	case KindDump:
		return "dump"
	case KindPartial:
		return "partial"
	case KindForeign:
		return "foreign"
	}
	return "foreign"
}

// kinds is every Kind, in the order a report or a plan walks them. A new member
// of the enum belongs here too; the package's tests fail otherwise.
var kinds = []Kind{
	KindOriginal, KindSidecar, KindThumbnail, KindHLS, KindDump, KindPartial, KindForeign,
}

// Kinds returns every kind there is, originals first and foreign last. The
// returned slice is a fresh copy, so a caller may sort or filter it.
//
// It exists so an operation can be tested against the whole enum — "for every
// kind of object the store can hold, what does this do?" — rather than against
// the prefixes its author happened to think of.
func Kinds() []Kind {
	out := make([]Kind, len(kinds))
	copy(out, kinds)
	return out
}

// originalKeyPattern matches the layout internal/storage gives every original:
// the YYYY/MM directory derived from the capture time, then a filename. It is
// anchored at both ends and requires a name after the month, so neither a bare
// directory marker nor a deeper foreign tree that merely begins with digits is
// mistaken for an original.
var originalKeyPattern = regexp.MustCompile(`^[0-9]{4}/[0-9]{2}/[^/]+$`)

// Classify reports which layout owns key, or KindForeign when none does. The key
// is the slash-separated path relative to the store root; a leading slash and
// surrounding whitespace are tolerated, an empty key is foreign.
//
// The test is by prefix and never by the store's answer, so it costs nothing and
// works the same for a key that is about to be written as for one that was
// listed.
func Classify(key string) Kind {
	clean := strings.TrimPrefix(strings.TrimSpace(key), "/")
	switch {
	case clean == "":
		return KindForeign
	case strings.HasPrefix(clean, thumb.CacheSubdir+"/"):
		return KindThumbnail
	case strings.HasPrefix(clean, sidecarexport.Prefix+"/"):
		return KindSidecar
	case strings.HasPrefix(clean, hls.Prefix+"/"):
		return KindHLS
	case strings.HasPrefix(clean, DumpPrefix):
		return KindDump
	case strings.HasPrefix(clean, PartialPrefix):
		return KindPartial
	case originalKeyPattern.MatchString(clean):
		return KindOriginal
	default:
		return KindForeign
	}
}
