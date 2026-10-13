package rekey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/panbotka/kukatko/internal/storage"
)

// verify reports whether the original's new key holds the bytes the catalogue
// promises for photo, returning ErrVerifyFailed when it does not.
//
// The size must equal photos.file_size. Then, on a store that reports entity
// tags, a copy whose ETag equals the source's content ETag (the MD5 of a
// single-part object) holds the source's bytes, which is answered by two
// metadata requests. Everywhere else — a multipart source, a source already
// gone, a filesystem — the copy is read back and its SHA-256 compared with
// photos.file_hash.
func (s *Service) verify(ctx context.Context, photo PhotoFiles, original Move) error {
	size, err := s.sizeOf(ctx, original.To)
	if err != nil {
		return err
	}
	if size != photo.FileSize {
		return fmt.Errorf("%w: %s holds %d bytes, the catalogue says %d",
			ErrVerifyFailed, original.To, size, photo.FileSize)
	}
	if s.etagsMatch(ctx, original) {
		return nil
	}
	digest, err := s.digest(ctx, original.To)
	if err != nil {
		return err
	}
	if photo.FileHash == "" || !strings.EqualFold(digest, photo.FileHash) {
		return fmt.Errorf("%w: %s digests to %s, the catalogue says %q",
			ErrVerifyFailed, original.To, digest, photo.FileHash)
	}
	return nil
}

// sizeOf returns the size of the object at key.
func (s *Service) sizeOf(ctx context.Context, key string) (int64, error) {
	if stater, ok := s.store.(storage.ETagStater); ok {
		stat, err := stater.StatETag(ctx, key)
		if err != nil {
			return 0, fmt.Errorf("rekey: stat %s: %w", key, err)
		}
		return stat.Size, nil
	}
	info, err := s.store.Stat(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("rekey: stat %s: %w", key, err)
	}
	return info.Size(), nil
}

// etagsMatch reports whether the store's entity tags prove the copy holds the
// source's bytes: both are content ETags and they are equal. Any doubt — no
// tags, a multipart tag, a source that is gone — is false, which sends verify
// to read the copy back.
func (s *Service) etagsMatch(ctx context.Context, original Move) bool {
	stater, ok := s.store.(storage.ETagStater)
	if !ok {
		return false
	}
	src, err := stater.StatETag(ctx, original.From)
	if err != nil || !storage.IsContentETag(src.ETag) {
		return false
	}
	dst, err := stater.StatETag(ctx, original.To)
	if err != nil {
		return false
	}
	return src.Size == dst.Size && strings.EqualFold(src.ETag, dst.ETag)
}

// digest streams the object at key and returns its hex SHA-256.
func (s *Service) digest(ctx context.Context, key string) (string, error) {
	reader, err := s.store.Open(ctx, key)
	if err != nil {
		return "", fmt.Errorf("rekey: opening %s: %w", key, err)
	}
	defer func() { _ = reader.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, reader); err != nil {
		return "", fmt.Errorf("rekey: reading %s: %w", key, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
