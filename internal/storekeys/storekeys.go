// Package storekeys says what lives in Kukátko's object store and which layout
// owns it.
//
// The store has no per-application prefix: an original sits at YYYY/MM/<name>,
// and the derived artefacts sit beside it under prefixes of their own — thumb/
// for the thumbnail cache, sidecars/ for the metadata sidecars, hls/ for a
// video's streaming segments, families.yaml for the genealogy export — with the
// database dumps (db/) and the in-progress uploads (.tmp/) using the same key
// space again. So "what is this object?" cannot be answered by a single prefix
// test, and every operation that reasons about the store as a whole has to
// answer it somehow.
//
// This package is where it is answered once. The layouts themselves are built
// where they belong (internal/thumb, internal/sidecarexport, internal/hls,
// internal/familyexport, internal/storage); what lives here is the name of each
// layout's top-level directory, the mapping from a key to the kind of thing it
// is, and the project prefix grammar every builder shares. The whole-store
// operations — the backup, the storage migration, the library wipe — classify
// identically instead of each carrying its own list of prefixes to remember.
// The package imports nothing of Kukátko's own, so every builder can use it.
//
// # Two layouts
//
// Every project's objects live under the project's storage prefix: "" — the
// root layout — for the bootstrap project until its storage move, and
// projects/<uid>/ for every other project. The same per-project tree sits under
// either, so the original 2024/05/x.jpg of a created project is
// projects/<uid>/2024/05/x.jpg, its sidecar projects/<uid>/sidecars/2024/05/x.jpg.yml
// and its thumbnails projects/<uid>/thumb/aa/bb/cc/<hash>_<size>.jpg.
//
// The rule every builder follows: the prefix of a photo's derived objects is the
// prefix of its own original — PrefixOf(photos.file_path), never the project
// row — so a project that is half-way through its storage move is still
// consistent per photo, and no read ever has to fall back or guess. Only the
// project-wide families.yaml, which belongs to no photo, takes the project's
// current storage_prefix.
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
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Top-level directories (and the one top-level object) of a project's tree.
// They hang off the project's storage prefix: "" for the root layout,
// projects/<uid>/ otherwise. Each is re-exported by the package that builds the
// layout (thumb.CacheSubdir, sidecarexport.Prefix, hls.Prefix, familyexport.Key),
// and declared here so the classification and the builders cannot drift apart.
const (
	// ThumbnailDir holds the thumbnail cache, built by internal/thumb.
	ThumbnailDir = "thumb"
	// SidecarDir holds the metadata sidecars, built by internal/sidecarexport.
	SidecarDir = "sidecars"
	// HLSDir holds the streaming renditions, built by internal/hls.
	HLSDir = "hls"
	// FamiliesKey is the genealogy export, written by internal/familyexport.
	FamiliesKey = "families.yaml"
)

// Prefixes of the key spaces this package recognises but that no other package
// exports as a constant. They belong to the store as a whole, never to a
// project, so they exist only at the bucket root.
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
	// ProjectsDir is the directory every created project's storage prefix lives
	// under: projects/<uid>/. It mirrors project.StoragePrefixRoot.
	ProjectsDir = "projects/"
)

// ErrInvalidPrefix is returned by ValidatePrefix (and by every key builder that
// takes a storage prefix) for a prefix that is neither empty nor exactly
// projects/<uid>/ with a well-formed project uid.
var ErrInvalidPrefix = errors.New("storekeys: invalid storage prefix")

// ErrMoving indicates a storage move is moving one photo's objects from one
// layout to the other right now, so nothing may be published for the photo or
// deleted of it until the move is through with it. It is transient: the move
// holds a photo for the time it takes to copy its objects. Defined here, beside
// the layouts, so that the publishers of derived objects (internal/thumb among
// them, which the move itself imports) can recognise it without importing the
// move.
var ErrMoving = errors.New("storekeys: a storage move is moving this photo right now")

// projectPrefixPattern matches a project storage prefix at the start of a key:
// projects/, a project uid (pj and 24 symbols of the lowercase base32 alphabet
// every uid uses), and the closing slash. The uid shape is checked so a foreign
// tree that merely happens to be called projects/ is never taken for a project.
var projectPrefixPattern = regexp.MustCompile(`^projects/(pj[0-9a-v]{24})/`)

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
	// KindFamilies is a project's genealogy export — the single families.yaml
	// at the root of the project's tree, written by internal/familyexport. Like a
	// sidecar it is disaster-recovery data and not regenerable from anything else
	// in the store, and unlike one it belongs to no photo: it is one object for the
	// whole project, which is why it is a kind of its own rather than a sidecar living
	// somewhere odd.
	KindFamilies
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
	case KindFamilies:
		return "families"
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
	KindOriginal, KindSidecar, KindFamilies, KindThumbnail, KindHLS, KindDump, KindPartial, KindForeign,
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

// originalKeyPattern matches the layout internal/storage gives every original
// inside a project's tree: the YYYY/MM directory derived from the capture time,
// then a filename. It is anchored at both ends and requires a name after the
// month, so neither a bare directory marker nor a deeper foreign tree that merely
// begins with digits is mistaken for an original. Classify applies it to the key
// with its project prefix stripped, so it accepts projects/<uid>/YYYY/MM/<name>
// as well.
var originalKeyPattern = regexp.MustCompile(`^[0-9]{4}/[0-9]{2}/[^/]+$`)

// PrefixOf returns the storage prefix of the layout originalKey belongs to:
// projects/<uid>/ when the key starts with a well-formed project prefix, ""
// (the root layout) otherwise. It is how every builder of a derived key learns
// where a photo's derived objects live — from the photo's own file_path, never
// from its project row.
//
//	storekeys.PrefixOf("projects/pj…/2024/05/x.jpg") // "projects/pj…/"
//	storekeys.PrefixOf("2024/05/x.jpg")              // ""
func PrefixOf(originalKey string) string {
	prefix, _ := Strip(originalKey)
	return prefix
}

// Strip splits key into its project storage prefix and the rest of the key
// inside that project's tree. A key under no well-formed project prefix is in
// the root layout: its prefix is "" and rest is key itself. A leading slash and
// surrounding whitespace are tolerated and dropped. Join(Strip(key)) gives the
// cleaned key back.
func Strip(key string) (prefix, rest string) {
	clean := strings.TrimPrefix(strings.TrimSpace(key), "/")
	match := projectPrefixPattern.FindString(clean)
	return match, clean[len(match):]
}

// Join puts rest under the storage prefix prefix: the inverse of Strip. It is a
// plain concatenation, because a prefix is either "" or ends in a slash; a
// caller holding an unchecked prefix validates it first (ValidatePrefix).
func Join(prefix, rest string) string {
	return prefix + rest
}

// ValidatePrefix reports whether prefix is a storage prefix a key builder may
// put a derived key under: "" (the root layout) or exactly projects/<uid>/ with a
// well-formed project uid. It returns ErrInvalidPrefix otherwise, so a malformed
// prefix can never become a path that climbs out of the cache directory or into
// another project's tree.
func ValidatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if match := projectPrefixPattern.FindString(prefix); match != prefix {
		return fmt.Errorf("%w: %q", ErrInvalidPrefix, prefix)
	}
	return nil
}

// ProjectUID returns the project uid a storage prefix names: the uid of
// projects/<uid>/, or "" for the root layout and for anything that is not a
// well-formed prefix.
func ProjectUID(prefix string) string {
	match := projectPrefixPattern.FindStringSubmatch(prefix)
	if len(match) < 2 || match[0] != prefix {
		return ""
	}
	return match[1]
}

// Classify reports which layout owns key, or KindForeign when none does. It is
// ClassifyWithProject without the project uid.
func Classify(key string) Kind {
	kind, _ := ClassifyWithProject(key)
	return kind
}

// ClassifyWithProject reports which layout owns key, or KindForeign when none
// does, and the uid of the project whose tree holds it: "" for the root layout.
// The key is the slash-separated path relative to the store root; a leading
// slash and surrounding whitespace are tolerated, an empty key is foreign.
//
// Both layouts are accepted: a key under projects/<uid>/ is classified by the
// rest of its key, exactly as the same key at the root would be. The store-wide
// kinds — dumps and partial uploads — exist only at the root, so a db/ or .tmp/
// inside a project's tree is foreign, and so is a project prefix with nothing
// after it. The uid is reported only for a kind a project owns; a foreign key
// carries none.
//
// The test is by prefix and never by the store's answer, so it costs nothing and
// works the same for a key that is about to be written as for one that was
// listed. The one exception is the genealogy export, which is a single object
// rather than a layout and is therefore matched by its exact key: it owns no
// prefix, so a foreign file whose name merely begins the same way stays foreign.
func ClassifyWithProject(key string) (Kind, string) {
	prefix, rest := Strip(key)
	kind := classifyRest(rest)
	if prefix == "" {
		return kind, ""
	}
	switch kind {
	case KindOriginal, KindSidecar, KindFamilies, KindThumbnail, KindHLS:
		return kind, ProjectUID(prefix)
	case KindDump, KindPartial, KindForeign:
		return KindForeign, ""
	}
	return KindForeign, ""
}

// classifyRest classifies a key with its project prefix already stripped, by the
// root layout's rules.
func classifyRest(clean string) Kind {
	switch {
	case clean == "":
		return KindForeign
	case strings.HasPrefix(clean, ThumbnailDir+"/"):
		return KindThumbnail
	case strings.HasPrefix(clean, SidecarDir+"/"):
		return KindSidecar
	case clean == FamiliesKey:
		return KindFamilies
	case strings.HasPrefix(clean, HLSDir+"/"):
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
