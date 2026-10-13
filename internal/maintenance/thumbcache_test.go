package maintenance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeResolver maps a (prefix, hash, size) triple to a path under dir, or
// returns err.
type fakeResolver struct {
	dir string
	err error
}

func (r fakeResolver) Path(prefix, hash, size string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return filepath.Join(r.dir, filepath.FromSlash(prefix), hash+"_"+size+".jpg"), nil
}

// TestThumbCacheHasThumbnail verifies presence reflects whether the cache file
// exists, with a missing file reported as absent (not an error).
func TestThumbCacheHasThumbnail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := NewThumbCache(fakeResolver{dir: dir})

	present := "h1_" + representativeThumbSize + ".jpg"
	if err := os.WriteFile(filepath.Join(dir, present), []byte("jpeg"), 0o600); err != nil {
		t.Fatalf("seeding cache file: %v", err)
	}

	if ok, err := cache.HasThumbnail("2024/05/h1.jpg", "h1"); err != nil || !ok {
		t.Errorf("HasThumbnail(h1) = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := cache.HasThumbnail("2024/05/missing.jpg", "missing"); err != nil || ok {
		t.Errorf("HasThumbnail(missing) = (%v, %v), want (false, nil)", ok, err)
	}
	// The same hash under a project prefix is a different cache entry: the
	// prefix comes from the original's own key.
	prefixed := "projects/pj0123456789abcdefghijklmn/2024/05/h1.jpg"
	if ok, err := cache.HasThumbnail(prefixed, "h1"); err != nil || ok {
		t.Errorf("HasThumbnail(prefixed h1) = (%v, %v), want (false, nil)", ok, err)
	}
}

// TestThumbCacheResolverError verifies a resolver error is surfaced.
func TestThumbCacheResolverError(t *testing.T) {
	t.Parallel()
	cache := NewThumbCache(fakeResolver{err: errors.New("bad hash")})
	if _, err := cache.HasThumbnail("2024/05/x.jpg", "x"); err == nil {
		t.Error("HasThumbnail with a failing resolver should error")
	}
}
