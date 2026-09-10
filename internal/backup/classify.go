package backup

import (
	"github.com/panbotka/kukatko/internal/storekeys"
)

// backedUp reports whether an object of kind belongs in the backup.
//
// The rule is what a restore needs and nothing more: an original cannot be
// reproduced from anything else, and a metadata sidecar is the catalogue's
// disaster-recovery copy, so both travel. Everything Kukátko can rebuild from an
// original — the thumbnail cache and a video's streaming segments — does not: it
// would be paid for twice, in storage and in transfer, to save an operator a
// background job they can run at any time. Dumps and half-written uploads are
// not library content at all.
//
// A foreign object is backed up. The store's root is a shared namespace, so an
// object under none of Kukátko's layouts is something whose value this code
// cannot judge, and the safe judgement is to keep a copy of it.
//
// The switch has no default clause on purpose: a new kind of object in the store
// must be classified here before this package compiles cleanly again.
func backedUp(kind storekeys.Kind) bool {
	switch kind {
	case storekeys.KindOriginal, storekeys.KindSidecar, storekeys.KindForeign:
		return true
	case storekeys.KindThumbnail, storekeys.KindHLS, storekeys.KindDump, storekeys.KindPartial:
		return false
	}
	return false // unreachable: every kind is decided above.
}

// backedUpKey reports whether the object at key belongs in the backup, given
// only its key. It is how both sources — the bucket listing and the walk of the
// originals directory — reach the same verdict about the same layout.
func backedUpKey(key string) bool {
	return backedUp(storekeys.Classify(key))
}

// skippedDir reports whether a directory of the originals walk holds nothing the
// backup wants, so the walk can turn away at its root rather than statting every
// file inside it. Only a directory that *is* one of the derived prefixes
// qualifies: a directory classified as foreign is descended into, since that is
// what every YYYY and YYYY/MM directory on the way to an original looks like.
func skippedDir(relDir string) bool {
	kind := storekeys.Classify(relDir + "/")
	return kind != storekeys.KindForeign && !backedUp(kind)
}
