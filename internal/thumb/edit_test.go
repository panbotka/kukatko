package thumb

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// fakeEdits is an EditResolver returning a preset edit (or error) for any photo,
// recording how many times it was asked.
type fakeEdits struct {
	edit  photos.Edit
	err   error
	calls int
}

// GetEdit records the call and returns the preset edit or error.
func (f *fakeEdits) GetEdit(_ context.Context, _ string) (photos.Edit, error) {
	f.calls++
	if f.err != nil {
		return photos.Edit{}, f.err
	}
	return f.edit, nil
}

// newEditingThumbnailer builds a Thumbnailer wired with edits over an isolated
// originals store and cache root under t.TempDir().
func newEditingThumbnailer(t *testing.T, edits EditResolver) (*Thumbnailer, *storage.FS) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	return New(store, filepath.Join(root, "cache"), WithEdits(edits)), store
}

// TestGenerate_appliesRotationEdit is the point of the whole feature: a photo the
// user turned a quarter turn in the editor is turned in its thumbnails too, so the
// library grid stops showing it on its side. A 1200×900 source rotated 90° is
// 900×1200, whose fit_720 is 540×720 — the transpose of the unedited 720×540.
func TestGenerate_appliesRotationEdit(t *testing.T) {
	t.Parallel()
	edits := &fakeEdits{edit: photos.Edit{Rotation: 90}}
	th, store := newEditingThumbnailer(t, edits)
	photo := storeJPEG(t, store, 1200, 900, 0)
	photo.UID = "ph-rot"

	got, err := th.Generate(context.Background(), photo, "fit_720")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := jpegBounds(t, got["fit_720"])
	if w != 540 || h != 720 {
		t.Errorf("fit_720 of a 1200x900 source rotated 90° = %dx%d, want 540x720", w, h)
	}
	if edits.calls != 1 {
		t.Errorf("resolver calls = %d, want exactly 1 (one lookup per generation)", edits.calls)
	}
}

// TestGenerate_neutralEditRendersOriginal confirms the stored-but-neutral edit
// (what a reset leaves behind) changes nothing: the thumbnail is the plain
// original's, so resetting an edit really does restore the original rendering
// rather than merely stopping at some intermediate one.
func TestGenerate_neutralEditRendersOriginal(t *testing.T) {
	t.Parallel()
	th, store := newEditingThumbnailer(t, &fakeEdits{edit: photos.Edit{PhotoUID: "ph-neutral"}})
	photo := storeJPEG(t, store, 1200, 900, 0)
	photo.UID = "ph-neutral"

	got, err := th.Generate(context.Background(), photo, "fit_720")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := jpegBounds(t, got["fit_720"])
	if w != 720 || h != 540 {
		t.Errorf("fit_720 with a neutral edit = %dx%d, want the original's 720x540", w, h)
	}
}

// TestGenerate_editNotFoundRendersOriginal confirms the ordinary case — a photo
// nobody has edited — is not an error but the plain rendering.
func TestGenerate_editNotFoundRendersOriginal(t *testing.T) {
	t.Parallel()
	th, store := newEditingThumbnailer(t, &fakeEdits{err: photos.ErrEditNotFound})
	photo := storeJPEG(t, store, 1200, 900, 0)
	photo.UID = "ph-unedited"

	got, err := th.Generate(context.Background(), photo, "fit_720")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if w, h := jpegBounds(t, got["fit_720"]); w != 720 || h != 540 {
		t.Errorf("fit_720 of an unedited photo = %dx%d, want 720x540", w, h)
	}
}

// TestGenerate_editLookupFailureFails confirms a resolver failure that is not
// "no edit stored" fails the generation instead of silently caching the unedited
// rendering under the key an edited one would take — a wrong thumbnail nothing
// would ever notice.
func TestGenerate_editLookupFailureFails(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	th, store := newEditingThumbnailer(t, &fakeEdits{err: boom})
	photo := storeJPEG(t, store, 320, 240, 0)
	photo.UID = "ph-broken"

	if _, err := th.Generate(context.Background(), photo, "fit_720"); !errors.Is(err, boom) {
		t.Fatalf("Generate error = %v, want the resolver's error", err)
	}
	if _, err := th.OpenCached(photo.FileHash, "fit_720"); !errors.Is(err, ErrNotCached) {
		t.Errorf("OpenCached error = %v, want ErrNotCached (nothing may be written)", err)
	}
}

// TestGenerate_withoutResolverIgnoresEdits confirms the resolver is optional: a
// thumbnailer built without WithEdits renders originals verbatim and never asks
// about an edit, which is what the paths that only remove or preview thumbnails
// rely on.
func TestGenerate_withoutResolverIgnoresEdits(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	photo := storeJPEG(t, store, 1200, 900, 0)
	photo.UID = "ph-plain"

	got, err := th.Generate(context.Background(), photo, "fit_720")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if w, h := jpegBounds(t, got["fit_720"]); w != 720 || h != 540 {
		t.Errorf("fit_720 without an edit resolver = %dx%d, want 720x540", w, h)
	}
}

// TestWithEdits_nilIgnored confirms a nil resolver leaves the thumbnailer
// edit-unaware rather than installing a nil interface that would panic on use.
func TestWithEdits_nilIgnored(t *testing.T) {
	t.Parallel()
	th, _ := newThumbnailer(t)
	WithEdits(nil)(th)
	if th.edits != nil {
		t.Error("WithEdits(nil) installed a resolver, want the option ignored")
	}
}

// TestResolveEdit_noUID confirms a photo with no uid (a synthetic Photo built by
// another package's test, or a row that never reached the catalogue) is treated as
// unedited rather than sent to the resolver with an empty key.
func TestResolveEdit_noUID(t *testing.T) {
	t.Parallel()
	edits := &fakeEdits{edit: photos.Edit{Rotation: 180}}
	th, _ := newEditingThumbnailer(t, edits)

	edit, err := th.resolveEdit(context.Background(), "")
	if err != nil {
		t.Fatalf("resolveEdit: %v", err)
	}
	if edit.Rotation != 0 {
		t.Errorf("resolveEdit(\"\") = %+v, want the neutral edit", edit)
	}
	if edits.calls != 0 {
		t.Errorf("resolver calls = %d, want 0", edits.calls)
	}
}
