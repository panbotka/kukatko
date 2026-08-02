//go:build integration

package storage

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// thumbnailSizes is the eight derived sizes one photo has in the bucket, named
// here only so the measurement below asks the question at its real width.
var thumbnailSizes = []string{
	"grid", "tile_224", "tile_500", "fit_720", "fit_1280", "fit_1920", "fit_2560", "fit_3840",
}

// TestR2KeysWithPrefix_measureAgainstHeadPerKey records what the thumbnailer's
// "which of this photo's sizes are already published?" question costs in each of
// the two shapes it could take: one prefix listing, or one Head per size. It
// asserts only correctness — that the listing finds exactly the photo's own keys
// — and logs the timings, because a latency threshold against a shared endpoint
// is a flake rather than a fact. Run it with -v to see the numbers on the
// endpoint you care about; the ones behind the shape chosen in internal/thumb are
// written up in docs/PERF.md.
func TestR2KeysWithPrefix_measureAgainstHeadPerKey(t *testing.T) {
	store, _ := newTestR2(t)

	const prefix = "thumb/aa/bb/cc/aabbccdd_"
	keys := make([]string, 0, len(thumbnailSizes))
	for _, size := range thumbnailSizes {
		key := prefix + size + ".jpg"
		body := jpegBytes(key)
		file := StoredFile{Hash: hashOf(body), RelPath: key, Size: int64(len(body)), MIME: "image/jpeg"}
		if err := store.Put(t.Context(), bytes.NewReader(body), file); err != nil {
			t.Fatalf("Put(%s): %v", key, err)
		}
		keys = append(keys, key)
	}

	start := time.Now()
	var listed int
	if err := store.KeysWithPrefix(t.Context(), prefix, func(string) error {
		listed++
		return nil
	}); err != nil {
		t.Fatalf("KeysWithPrefix: %v", err)
	}
	listTime := time.Since(start)
	if listed != len(keys) {
		t.Fatalf("listing found %d key(s), want %d", listed, len(keys))
	}

	start = time.Now()
	for _, key := range keys {
		if _, err := store.Head(t.Context(), key); err != nil {
			t.Fatalf("Head(%s): %v", key, err)
		}
	}
	headTime := time.Since(start)

	t.Logf("endpoint %s: one prefix listing of %d keys took %v; %d Heads took %v",
		os.Getenv(envTestS3Endpoint), len(keys), listTime, len(keys), headTime)
}
