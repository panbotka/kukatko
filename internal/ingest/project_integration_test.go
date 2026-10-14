//go:build integration

package ingest_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storekeys"
	"github.com/panbotka/kukatko/internal/thumb"
)

// TestIngest_dedupAndKeysArePerProject verifies the dedup key is (project,
// file_hash) and the object key carries the project's storage prefix: the same
// bytes uploaded into the bootstrap project A (empty prefix) and a created
// project B ("projects/<uid>/") become two photo rows with two different keys,
// both originals on disk; a re-upload into A is A's duplicate; and A's listing
// does not show B's photo.
func TestIngest_dedupAndKeysArePerProject(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: false})
	ctx := t.Context()
	projects := project.NewStore(env.db.Pool())
	projectA, err := projects.Get(ctx, project.BootstrapUID)
	if err != nil {
		t.Fatalf("Get bootstrap: %v", err)
	}
	projectB, err := projects.Create(ctx, audit.Meta{}, project.CreateParams{Slug: "ingest-b", Name: "Ingest B"})
	if err != nil {
		t.Fatalf("Create project B: %v", err)
	}
	data := jpegBytes(t, 30, 160, 90, 90)

	inA := env.svc.Ingest(ctx, projectA, bytes.NewReader(data), "same.jpg", env.uploader)
	inB := env.svc.Ingest(ctx, projectB, bytes.NewReader(data), "same.jpg", env.uploader)
	if inA.Outcome != ingest.OutcomeCreated || inB.Outcome != ingest.OutcomeCreated {
		t.Fatalf("outcomes A=%+v B=%+v, want both created", inA, inB)
	}
	if inA.PhotoUID == inB.PhotoUID {
		t.Fatalf("both projects got photo %s, want two rows", inA.PhotoUID)
	}
	again := env.svc.Ingest(ctx, projectA, bytes.NewReader(data), "again.jpg", env.uploader)
	if again.Outcome != ingest.OutcomeDuplicate || again.PhotoUID != inA.PhotoUID {
		t.Fatalf("re-upload into A = %+v, want A's duplicate %s", again, inA.PhotoUID)
	}

	photoA, err := env.store.GetByUIDInProject(ctx, projectA.UID, inA.PhotoUID)
	if err != nil {
		t.Fatalf("A's photo in A: %v", err)
	}
	photoB, err := env.store.GetByUIDInProject(ctx, projectB.UID, inB.PhotoUID)
	if err != nil {
		t.Fatalf("B's photo in B: %v", err)
	}
	assertSeparateKeys(t, env.root, photoA, photoB, projectB.StoragePrefix)
	assertSeparateDerivedKeys(t, env.thumbs, photoA, photoB)

	if _, err := env.store.GetByUIDInProject(ctx, projectA.UID, inB.PhotoUID); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Fatalf("B's photo read in A = %v, want ErrPhotoNotFound", err)
	}
	listedA, err := env.store.ListActiveUIDs(ctx, projectA.UID)
	if err != nil {
		t.Fatalf("ListActiveUIDs(A): %v", err)
	}
	if !slices.Equal(listedA, []string{inA.PhotoUID}) {
		t.Fatalf("A's listing = %v, want only %s", listedA, inA.PhotoUID)
	}
}

// assertSeparateKeys checks two catalogued copies of the same bytes have the
// same hash but different object keys — the bootstrap one in the root layout,
// the created project's under prefixB — and that both originals exist on disk.
func assertSeparateKeys(t *testing.T, root string, photoA, photoB photos.Photo, prefixB string) {
	t.Helper()
	if photoA.FileHash != photoB.FileHash {
		t.Fatalf("hashes differ (%s vs %s) for the same bytes", photoA.FileHash, photoB.FileHash)
	}
	if photoA.FilePath == photoB.FilePath {
		t.Fatalf("both projects share the key %q", photoA.FilePath)
	}
	if strings.HasPrefix(photoA.FilePath, project.StoragePrefixRoot) {
		t.Errorf("A's key %q is under a project prefix, want the root layout", photoA.FilePath)
	}
	if !strings.HasPrefix(photoB.FilePath, prefixB) {
		t.Errorf("B's key %q is not under B's prefix %q", photoB.FilePath, prefixB)
	}
	for _, key := range []string{photoA.FilePath, photoB.FilePath} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(key))); err != nil {
			t.Errorf("original %s: %v", key, err)
		}
	}
}

// assertSeparateDerivedKeys checks the derived objects of two catalogued copies
// of the same bytes (one hash) never share a key either: every builder takes the
// prefix of the photo's own original, so the thumbnails, the sidecar and the
// streaming segments of the two copies live in their own projects' trees — and
// the thumbnails ingest generated for each are both in the cache.
func assertSeparateDerivedKeys(t *testing.T, thumbs *thumb.Thumbnailer, photoA, photoB photos.Photo) {
	t.Helper()
	prefixA, prefixB := storekeys.PrefixOf(photoA.FilePath), storekeys.PrefixOf(photoB.FilePath)
	if prefixA != "" || prefixB == "" {
		t.Fatalf("prefixes A=%q B=%q, want the root layout for A and a project prefix for B", prefixA, prefixB)
	}
	thumbA, errA := thumb.RelPath(prefixA, photoA.FileHash, thumb.GridSize)
	thumbB, errB := thumb.RelPath(prefixB, photoB.FileHash, thumb.GridSize)
	sidecarA, errC := sidecarexport.KeyFor(photoA.FilePath)
	sidecarB, errD := sidecarexport.KeyFor(photoB.FilePath)
	hlsA, errE := hls.PrefixFor(prefixA, photoA.FileHash)
	hlsB, errF := hls.PrefixFor(prefixB, photoB.FileHash)
	if err := errors.Join(errA, errB, errC, errD, errE, errF); err != nil {
		t.Fatalf("building derived keys: %v", err)
	}
	for _, pair := range [][2]string{{thumbA, thumbB}, {sidecarA, sidecarB}, {hlsA, hlsB}} {
		if pair[0] == pair[1] {
			t.Errorf("both projects share the derived key %q", pair[0])
		}
		if !strings.HasPrefix(pair[1], prefixB) {
			t.Errorf("B's derived key %q is not under B's prefix %q", pair[1], prefixB)
		}
	}
	for _, photo := range []photos.Photo{photoA, photoB} {
		reader, err := thumbs.OpenCached(storekeys.PrefixOf(photo.FilePath), photo.FileHash, "tile_224")
		if err != nil {
			t.Errorf("thumbnail of %s under its own prefix: %v", photo.FilePath, err)
			continue
		}
		_ = reader.Close()
	}
}

// TestIngest_nearDuplicateWarningStaysInItsProject uploads a picture into A and
// then, into B, the same picture at another JPEG quality (a near-duplicate of
// A's) and a byte-identical copy of A's file. Neither upload into B may name A's
// photo — the warning carries the matched uid, and B's uploader must not learn
// it: the first finds nothing in an empty B, the byte-identical copy becomes
// B's own photo (not A's duplicate) and is warned about B's earlier upload
// only. A near-duplicate uploaded into A is still warned about A's photo.
func TestIngest_nearDuplicateWarningStaysInItsProject(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: true, PhashMaxDiff: 12})
	ctx := t.Context()
	projects := project.NewStore(env.db.Pool())
	projectA, err := projects.Get(ctx, project.BootstrapUID)
	if err != nil {
		t.Fatalf("Get bootstrap: %v", err)
	}
	projectB, err := projects.Create(ctx, audit.Meta{}, project.CreateParams{Slug: "near-b", Name: "Near B"})
	if err != nil {
		t.Fatalf("Create project B: %v", err)
	}
	original := jpegBytes(t, 120, 120, 120, 95)

	inA := env.svc.Ingest(ctx, projectA, bytes.NewReader(original), "hi.jpg", env.uploader)
	if inA.Outcome != ingest.OutcomeCreated {
		t.Fatalf("upload into A = %+v, want created", inA)
	}
	nearInB := env.svc.Ingest(ctx, projectB, bytes.NewReader(jpegBytes(t, 120, 120, 120, 40)), "lo.jpg", env.uploader)
	if nearInB.Outcome != ingest.OutcomeCreated || hasWarning(nearInB.Warnings, "near_duplicate") {
		t.Fatalf("near-duplicate into an empty B = %+v, want created with no near_duplicate warning", nearInB)
	}
	sameInB := env.svc.Ingest(ctx, projectB, bytes.NewReader(original), "hi.jpg", env.uploader)
	if sameInB.Outcome != ingest.OutcomeCreated || sameInB.PhotoUID == inA.PhotoUID {
		t.Fatalf("byte-identical copy into B = %+v, want a photo of B's own, not A's %s", sameInB, inA.PhotoUID)
	}
	assertWarnedAbout(t, "byte-identical copy into B", sameInB, nearInB.PhotoUID)
	for _, res := range []ingest.FileResult{nearInB, sameInB} {
		for _, w := range res.Warnings {
			if w.PhotoUID == inA.PhotoUID || strings.Contains(w.Message, inA.PhotoUID) {
				t.Errorf("upload into B warned about A's photo: %+v", w)
			}
		}
	}

	nearInA := env.svc.Ingest(ctx, projectA, bytes.NewReader(jpegBytes(t, 120, 120, 120, 60)), "mid.jpg", env.uploader)
	if nearInA.Outcome != ingest.OutcomeCreated {
		t.Fatalf("near-duplicate into A = %+v, want created", nearInA)
	}
	assertWarnedAbout(t, "near-duplicate into A", nearInA, inA.PhotoUID)
}

// assertWarnedAbout checks res carries a near_duplicate warning naming wantUID.
func assertWarnedAbout(t *testing.T, what string, res ingest.FileResult, wantUID string) {
	t.Helper()
	for _, w := range res.Warnings {
		if w.Code == "near_duplicate" && w.PhotoUID == wantUID {
			return
		}
	}
	t.Errorf("%s warnings = %+v, want a near_duplicate naming %s", what, res.Warnings, wantUID)
}
