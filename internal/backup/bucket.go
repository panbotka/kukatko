package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors for the bucket-backed originals source. They exist so a
// misconfigured backup fails loudly at wiring time instead of quietly copying an
// empty set of originals and reporting success.
var (
	// ErrNoSourceStore indicates NewBucketOriginals was handed no primary store,
	// which is a wiring bug rather than a configuration one.
	ErrNoSourceStore = errors.New("backup: primary object store is required")
	// ErrNoSourceBucket indicates the primary bucket name is missing, so there is
	// nothing to copy the originals out of.
	ErrNoSourceBucket = errors.New("backup: primary originals bucket not configured")
)

// BucketOriginals is an OriginalSource backed by the primary object store: the
// bucket the live library reads and writes. It lists that bucket to enumerate
// the originals, and transfers each one by asking the *backup* bucket's service
// to copy it across server-side. The payload therefore never travels through
// this process — which is the whole point on a host whose disk could not hold
// the library in the first place.
//
// The two buckets are configured independently (endpoint, region, bucket,
// credentials), so the backup may live in another account or with another
// provider. The server-side copy is issued against the backup endpoint with the
// primary bucket named as the copy source, so those credentials must be able to
// read the primary bucket.
type BucketOriginals struct {
	source ObjectStore
	bucket string
}

// compile-time assertion that BucketOriginals satisfies OriginalSource.
var _ OriginalSource = (*BucketOriginals)(nil)

// NewBucketOriginals returns a BucketOriginals that lists bucket through source.
// It returns ErrNoSourceStore when source is nil and ErrNoSourceBucket when
// bucket is empty, so an unconfigured primary can never be mistaken for an empty
// library.
func NewBucketOriginals(source ObjectStore, bucket string) (*BucketOriginals, error) {
	if source == nil {
		return nil, ErrNoSourceStore
	}
	if bucket == "" {
		return nil, ErrNoSourceBucket
	}
	return &BucketOriginals{source: source, bucket: bucket}, nil
}

// Bucket returns the name of the primary bucket the originals are read from.
func (b *BucketOriginals) Bucket() string { return b.bucket }

// List enumerates every original in the primary bucket. Database dumps and
// in-progress uploads are skipped, so pointing the backup at the primary bucket
// by mistake cannot turn dumps into "originals"; everything else in the bucket
// is an original, since derived artefacts are cached elsewhere.
func (b *BucketOriginals) List(ctx context.Context) ([]LocalOriginal, error) {
	objects, err := b.source.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("backup: listing originals in %s: %w", b.bucket, err)
	}
	originals := make([]LocalOriginal, 0, len(objects))
	for _, obj := range objects {
		if skipKey(obj.Key) {
			continue
		}
		originals = append(originals, LocalOriginal{Key: obj.Key, Size: obj.Size})
	}
	return originals, nil
}

// CopyTo has dst copy the original across from the primary bucket server-side,
// under the same key. Nothing is read or written by this process.
func (b *BucketOriginals) CopyTo(ctx context.Context, dst ObjectStore, original LocalOriginal) error {
	if err := dst.CopyFrom(ctx, b.bucket, original.Key, original.Key); err != nil {
		return fmt.Errorf("backup: copying %s from %s: %w", original.Key, b.bucket, err)
	}
	return nil
}

// skipKey reports whether an object key in the primary bucket is not an original:
// a database dump under the dump prefix, or a partial upload under the temporary
// prefix.
func skipKey(key string) bool {
	return strings.HasPrefix(key, dumpPrefix) || strings.HasPrefix(key, tmpDirName+"/")
}
