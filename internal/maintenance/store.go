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

// StoreScanner enumerates the originals the instance's store actually holds,
// whichever backend that is. It backs the orphan half of the scan (in the store,
// no catalogue row), the orphan-import repair, and the inventory total the report
// prints.
type StoreScanner interface {
	// Kind names the store being enumerated, so the report can say which one the
	// numbers came from.
	Kind() StoreKind
	// ListOriginals calls yield once per original the store holds, passing its
	// storage key. It stops at the first error from yield and returns it
	// unwrapped, so a caller can end the walk with a sentinel of its own.
	//
	// It streams: the whole key set is never materialised, so a bucket holding a
	// hundred thousand objects is walked in bounded memory.
	ListOriginals(ctx context.Context, yield func(key string) error) error
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

// isOriginalKey reports whether key names an original media file rather than one
// of the other things the store holds.
//
// The test is positive — "does this look like an original?" — rather than a list
// of prefixes to skip, and for the same reason internal/reset classifies keys
// that way: the bucket root is the namespace, so thumb/ and sidecars/ are not the
// only things that can sit beside the originals. An object some other tool put in
// a shared bucket must not be counted as an orphan, because an orphan is a file
// the repair offers to ingest into the library. The classification itself is
// internal/storekeys', shared with the backup, the storage migration and the
// wipe, so a prefix added to the store is not something this scan can be left
// behind on.
//
// Excluding the sidecars matters on both backends: a sidecar describes a photo,
// it is not one, and no catalogue row will ever point at it. Counting them would
// report one orphan per photo forever and never let the report be clean again.
// The same now goes for a video's streaming segments.
func isOriginalKey(key string) bool {
	return storekeys.Classify(key) == storekeys.KindOriginal
}

// StoreOriginals is the StoreScanner over a storage backend: it streams every key
// the store holds and keeps the ones that are originals. One implementation
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

// ListOriginals streams the store's keys and yields those that are originals,
// skipping thumbnails, metadata sidecars and anything else the store holds. See
// StoreScanner.
func (s *StoreOriginals) ListOriginals(ctx context.Context, yield func(key string) error) error {
	//nolint:wrapcheck // both errors travel back verbatim: the caller's own yield
	// error must stay recognisable to it, and the backend's listing error is
	// wrapped by the scan that asked for the listing.
	return s.lister.Keys(ctx, func(key string) error {
		if !isOriginalKey(key) {
			return nil
		}
		return yield(key)
	})
}
