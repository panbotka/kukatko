package maintenance

import (
	"errors"
	"fmt"
	"os"
)

// thumbPathResolver is the subset of *thumb.Thumbnailer that ThumbCache needs:
// resolving a (hash, size) pair to its absolute cache path. Keeping it as an
// interface lets ThumbCache be unit-tested without a real thumbnailer.
type thumbPathResolver interface {
	// Path returns the absolute cache path of the thumbnail for hash and size,
	// whether or not it exists.
	Path(hash, size string) (string, error)
}

// ThumbCache is a ThumbChecker backed by the on-disk thumbnail cache: it resolves
// the representative size's cache path for a file hash and stats it. It satisfies
// ThumbChecker.
type ThumbCache struct {
	resolver thumbPathResolver
	size     string
}

// compile-time assertion that *ThumbCache satisfies ThumbChecker.
var _ ThumbChecker = (*ThumbCache)(nil)

// NewThumbCache returns a ThumbCache that probes the representative thumbnail
// size via resolver (satisfied by *thumb.Thumbnailer).
func NewThumbCache(resolver thumbPathResolver) *ThumbCache {
	return &ThumbCache{resolver: resolver, size: representativeThumbSize}
}

// HasThumbnail reports whether the representative thumbnail for fileHash exists in
// the cache. A malformed hash that the resolver rejects is reported as an error;
// a missing cache file is simply absent, not an error.
func (c *ThumbCache) HasThumbnail(fileHash string) (bool, error) {
	abs, err := c.resolver.Path(fileHash, c.size)
	if err != nil {
		return false, fmt.Errorf("maintenance: resolving thumbnail path for %s: %w", fileHash, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("maintenance: statting thumbnail %s: %w", abs, err)
	}
	return info.Mode().IsRegular(), nil
}
