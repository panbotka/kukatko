package reset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// keyKind classifies a key in the store by the prefix that owns it. Only the
// three owned kinds are ever deleted; kindForeign is what keeps a wipe inside its
// own namespace.
type keyKind int

const (
	// kindForeign is a key under none of Kukátko's prefixes. Something else put
	// it in the store, so the reset counts it and leaves it alone.
	kindForeign keyKind = iota
	// kindOriginal is an original media file at YYYY/MM/<name>.
	kindOriginal
	// kindThumbnail is a cached thumbnail under the thumb/ prefix.
	kindThumbnail
	// kindSidecar is a metadata sidecar under the sidecars/ prefix.
	kindSidecar
)

// originalKeyPattern matches the layout internal/storage gives every original:
// the YYYY/MM directory derived from the capture time, then a filename. It is
// anchored at the start and requires a name after the month, so neither a bare
// directory marker nor a deeper foreign tree that merely begins with digits is
// mistaken for an original.
var originalKeyPattern = regexp.MustCompile(`^[0-9]{4}/[0-9]{2}/[^/]+$`)

// classifyKey reports which of Kukátko's prefixes owns key, or kindForeign when
// none does.
//
// The bucket root is the namespace — there is no per-application prefix in front
// of YYYY/MM — so "what may this command delete" cannot be answered by a single
// prefix test. It is answered here, by the three layouts this application
// actually writes, and everything else is somebody else's object.
func classifyKey(key string) keyKind {
	clean := strings.TrimPrefix(strings.TrimSpace(key), "/")
	switch {
	case clean == "":
		return kindForeign
	case strings.HasPrefix(clean, thumb.CacheSubdir+"/"):
		return kindThumbnail
	case strings.HasPrefix(clean, sidecarexport.Prefix+"/"):
		return kindSidecar
	case originalKeyPattern.MatchString(clean):
		return kindOriginal
	default:
		return kindForeign
	}
}

// PrefixCounts is a per-prefix object count: the shape of the store as this
// command sees it, one number per thing it owns.
type PrefixCounts struct {
	// Originals counts keys under a YYYY/MM directory.
	Originals int `json:"originals"`
	// Thumbnails counts keys under the thumb/ prefix.
	Thumbnails int `json:"thumbnails"`
	// Sidecars counts keys under the sidecars/ prefix.
	Sidecars int `json:"sidecars"`
}

// Total returns the sum of the three prefixes.
func (p PrefixCounts) Total() int {
	return p.Originals + p.Thumbnails + p.Sidecars
}

// with returns the counts with the counter of kind's prefix incremented. A
// foreign key is not counted here — it is counted separately, because it is never
// deleted.
func (p PrefixCounts) with(kind keyKind) PrefixCounts {
	switch kind {
	case kindOriginal:
		p.Originals++
	case kindThumbnail:
		p.Thumbnails++
	case kindSidecar:
		p.Sidecars++
	case kindForeign:
	}
	return p
}

// catalogueFiles is the identity of every file the catalogue references: the
// storage key of each original, and the content hash each set of thumbnails is
// keyed by. Both are needed because the two layouts are addressed differently —
// originals and sidecars by path, thumbnails by hash.
type catalogueFiles struct {
	paths  []string
	hashes []string
}

// objectKeys expands the catalogued files into every store key they own: each
// original, the sidecar beside it, and one thumbnail per registered size.
//
// The expansion is deliberately blind to what the store actually holds. Probing
// first would cost one request per candidate on an object store — for a library
// of twenty thousand photos that is a hundred and sixty thousand extra requests
// to learn what a delete of a missing key already tells us for free.
func (c catalogueFiles) objectKeys() ([]string, PrefixCounts) {
	sizes := thumb.SizeNames()
	keys := make([]string, 0, len(c.paths)*2+len(c.hashes)*len(sizes))
	var counts PrefixCounts

	for _, filePath := range c.paths {
		keys = append(keys, filePath)
		counts.Originals++
		sidecar, err := sidecarexport.KeyFor(filePath)
		if err != nil {
			// A row with no usable path has no sidecar either; the original above
			// is still attempted, and the store reports it as invalid.
			continue
		}
		keys = append(keys, sidecar)
		counts.Sidecars++
	}
	for _, hash := range c.hashes {
		for _, size := range sizes {
			key, err := thumb.RelPath(hash, size)
			if err != nil {
				// A malformed hash addresses no cached thumbnail; nothing to delete.
				continue
			}
			keys = append(keys, key)
			counts.Thumbnails++
		}
	}
	return keys, counts
}

// StorageResult reports what the wipe did to the store.
type StorageResult struct {
	// Deleted is the number of objects actually removed.
	Deleted int `json:"deleted"`
	// Missing is the number of keys that held nothing. On the catalogue-driven
	// path this is the normal case for a thumbnail size that was never generated,
	// not a problem.
	Missing int `json:"missing"`
	// Skipped is the number of keys the store rejected as unusable (a corrupt
	// catalogue row). They address no object, so nothing was lost by skipping.
	Skipped int `json:"skipped"`
	// Foreign is the number of keys found outside every owned prefix and left
	// untouched. Only a sweep can see them.
	Foreign int `json:"foreign"`
	// Failed is the number of objects that could not be deleted.
	Failed int `json:"failed"`
	// Failures holds a bounded sample of the failures, for the operator to act on.
	Failures []string `json:"failures,omitempty"`
	// ThumbCacheCleared is the number of file hashes whose locally cached
	// thumbnails were removed, and ThumbCacheSwept reports that the whole local
	// thumbnail cache directory was removed instead.
	ThumbCacheCleared int  `json:"thumb_cache_cleared"`
	ThumbCacheSwept   bool `json:"thumb_cache_swept"`
}

// Touched reports whether the wipe got as far as doing anything to the store. It
// is false for a run that a guard stopped first, which is what lets a caller keep
// an all-zero storage summary out of an abort message.
func (r StorageResult) Touched() bool {
	return r.Deleted+r.Missing+r.Skipped+r.Foreign+r.Failed+r.ThumbCacheCleared > 0 || r.ThumbCacheSwept
}

// failureSampleLimit caps how many per-object failures are kept. The count is
// exact regardless; a wipe of a broken bucket must not print a hundred thousand
// identical lines.
const failureSampleLimit = 20

// collector accumulates a StorageResult from concurrent deletions.
type collector struct {
	mu     sync.Mutex
	result StorageResult
}

// record folds the outcome of deleting one key into the result: a nil error
// counts as deleted, a missing object as missing, an unusable key as skipped, and
// anything else as a failure with a bounded sample kept.
func (c *collector) record(key string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case err == nil:
		c.result.Deleted++
	case errors.Is(err, os.ErrNotExist):
		c.result.Missing++
	case errors.Is(err, storage.ErrInvalidPath):
		c.result.Skipped++
	default:
		c.result.Failed++
		if len(c.result.Failures) < failureSampleLimit {
			c.result.Failures = append(c.result.Failures, key+": "+err.Error())
		}
	}
}

// deleteKeys deletes every key with bounded concurrency and returns what
// happened to each. A per-object failure is collected rather than fatal: the
// caller decides what an incomplete store wipe means, and it decides not to
// truncate the catalogue that still names those objects.
//
// It returns an error only when the context is cancelled, which is the one
// failure that means "stop", not "note it and carry on".
func deleteKeys(ctx context.Context, store ObjectStore, keys []string, concurrency int) (StorageResult, error) {
	var acc collector
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(concurrency)
	for _, key := range keys {
		group.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				return fmt.Errorf("reset: object deletion cancelled: %w", err)
			}
			acc.record(key, store.Delete(groupCtx, key))
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return acc.result, fmt.Errorf("reset: deleting objects: %w", err)
	}
	return acc.result, nil
}

// sweepKeys lists everything the store holds and returns the owned keys, their
// per-prefix counts and how many foreign keys were seen and left alone.
//
// This is the only path that can remove an object the catalogue never mentioned,
// which is why it is opt-in: it is the difference between "delete what the
// library referenced" and "empty the prefixes the library owns". It still never
// deletes indiscriminately — every key is classified, and an unowned one is
// counted, not touched.
func sweepKeys(ctx context.Context, lister storage.KeyLister) ([]string, PrefixCounts, int, error) {
	var (
		keys    []string
		counts  PrefixCounts
		foreign int
	)
	err := lister.Keys(ctx, func(key string) error {
		kind := classifyKey(key)
		if kind == kindForeign {
			foreign++
			return nil
		}
		counts = counts.with(kind)
		keys = append(keys, key)
		return nil
	})
	if err != nil {
		return nil, PrefixCounts{}, 0, fmt.Errorf("reset: listing the store: %w", err)
	}
	return keys, counts, foreign, nil
}
