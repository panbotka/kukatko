package maintenance

import (
	"context"

	"github.com/panbotka/kukatko/internal/storekeys"
)

// StoreKind names the kind of store an instance keeps its originals in, so a
// report can say where its inventory came from instead of always claiming
// "disk". The two kinds are the two storage backends.
type StoreKind string

const (
	// StoreDisk is the local filesystem backend (storage.backend: fs) — the
	// originals root on this host.
	StoreDisk StoreKind = "disk"
	// StoreObject is the object-store backend (storage.backend: r2) — the
	// bucket, which on such an instance is the only place originals exist. The
	// originals root is then empty, so counting it would report zero files for a
	// full library.
	StoreObject StoreKind = "object"
)

// StoreEntry is one object of a project's tree as the store scan sees it: its
// key, what kind of thing it is, and the project whose prefix holds it — "" for
// the root layout, which belongs to whichever project still uses it.
type StoreEntry struct {
	// Key is the object's slash-separated key relative to the store root.
	Key string
	// Kind is what the object is: an original, a sidecar, the genealogy export, a
	// thumbnail or a streaming segment.
	Kind storekeys.Kind
	// ProjectUID is the uid the key's projects/<uid>/ prefix names, or "" for a
	// key in the root layout. The uid comes from the key alone: whether such a
	// project exists is the scan's question, not the listing's.
	ProjectUID string
}

// StoreScanner enumerates the objects the instance's store actually holds,
// whichever backend that is. It backs the orphan half of the scan (in the store,
// no catalogue row), the orphan-project and legacy-layout findings, the
// orphan-import repair, and the inventory totals the report prints.
type StoreScanner interface {
	// Kind names the store being enumerated, so the report can say which one the
	// numbers came from.
	Kind() StoreKind
	// ListObjects calls yield once per object a project's tree owns — every
	// original, sidecar, genealogy export, thumbnail and streaming segment, in
	// either layout — classified with its project. Dumps, half-written uploads
	// and foreign objects are not the library's and are never yielded. It stops
	// at the first error from yield and returns it unwrapped, so a caller can end
	// the walk with a sentinel of its own.
	//
	// It streams: the whole key set is never materialised, so a bucket holding a
	// hundred thousand objects is walked in bounded memory, and once.
	ListObjects(ctx context.Context, yield func(StoreEntry) error) error
}

// KeyLister is the subset of a storage backend the store scan needs: a streamed
// listing of every key the store holds. It is satisfied by storage.KeyLister,
// which both backends implement — *storage.FS by walking the originals root,
// *storage.R2 by listing the bucket.
type KeyLister interface {
	// Keys calls yield once for every object the store holds, passing its
	// slash-separated key relative to the store root.
	Keys(ctx context.Context, yield func(key string) error) error
}

// projectObject classifies key into the StoreEntry the scan reasons about, and
// reports false for anything that is not a project's: a dump, a half-written
// upload, or a key under none of Kukátko's layouts.
//
// The test is positive — "does this belong to a project's tree?" — rather than a
// list of prefixes to skip, and for the same reason internal/reset classifies
// keys that way: the bucket root is the namespace, so thumb/ and sidecars/ are
// not the only things that can sit beside the originals. An object some other
// tool put in a shared bucket must not be counted as an orphan, because an orphan
// is a file the repair offers to ingest into the library. The classification
// itself is internal/storekeys', shared with the backup, the storage migration
// and the wipe, so a prefix added to the store is not something this scan can be
// left behind on.
//
// The switch has no default clause on purpose: a new kind of object in the store
// must say here whether a project owns it.
func projectObject(key string) (StoreEntry, bool) {
	kind, projectUID := storekeys.ClassifyWithProject(key)
	switch kind {
	case storekeys.KindOriginal, storekeys.KindSidecar, storekeys.KindFamilies,
		storekeys.KindThumbnail, storekeys.KindHLS:
		return StoreEntry{Key: key, Kind: kind, ProjectUID: projectUID}, true
	case storekeys.KindDump, storekeys.KindPartial, storekeys.KindForeign:
		return StoreEntry{}, false
	}
	return StoreEntry{}, false // unreachable: every kind is decided above.
}

// StoreOriginals is the StoreScanner over a storage backend: it streams every key
// the store holds and keeps the ones a project's tree owns. One implementation
// serves both backends, which is what makes the scan see the same library
// whichever one an instance runs.
type StoreOriginals struct {
	kind   StoreKind
	lister KeyLister
}

// compile-time assertion that *StoreOriginals satisfies StoreScanner.
var _ StoreScanner = (*StoreOriginals)(nil)

// NewStoreOriginals returns a scanner that lists originals through lister and
// reports kind as the store it enumerated. Pass StoreDisk with the filesystem
// backend and StoreObject with the object store, so the report names the store
// the numbers actually came from.
func NewStoreOriginals(kind StoreKind, lister KeyLister) *StoreOriginals {
	return &StoreOriginals{kind: kind, lister: lister}
}

// Kind returns the store kind this scanner was built for. See StoreScanner.
func (s *StoreOriginals) Kind() StoreKind {
	return s.kind
}

// ListObjects streams the store's keys and yields those a project's tree owns,
// classified, skipping dumps, half-written uploads and foreign objects. See
// StoreScanner.
func (s *StoreOriginals) ListObjects(ctx context.Context, yield func(StoreEntry) error) error {
	//nolint:wrapcheck // both errors travel back verbatim: the caller's own yield
	// error must stay recognisable to it, and the backend's listing error is
	// wrapped by the scan that asked for the listing.
	return s.lister.Keys(ctx, func(key string) error {
		obj, ok := projectObject(key)
		if !ok {
			return nil
		}
		return yield(obj)
	})
}
