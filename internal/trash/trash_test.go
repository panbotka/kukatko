package trash

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakePhotoStore is an in-memory PhotoStore for the purge tests. It tracks which
// rows were deleted (and the audit entry each deletion carried) and can be made
// to fail a specific row's deletion.
type fakePhotoStore struct {
	photos    map[string]photos.Photo
	files     map[string][]photos.PhotoFile
	deleted   map[string]bool
	auditedAs map[string]audit.Entry
	deleteErr map[string]error
	listErr   error
}

// newFakePhotoStore returns an empty store ready to be seeded.
func newFakePhotoStore() *fakePhotoStore {
	return &fakePhotoStore{
		photos:    map[string]photos.Photo{},
		files:     map[string][]photos.PhotoFile{},
		deleted:   map[string]bool{},
		auditedAs: map[string]audit.Entry{},
		deleteErr: map[string]error{},
	}
}

// seed adds a photo with the given archived time (nil = live) and one original
// file whose path and hash derive from the uid.
func (f *fakePhotoStore) seed(uid string, archivedAt *time.Time) {
	f.photos[uid] = photos.Photo{UID: uid, FileHash: uid + "-hash", FilePath: uid + ".jpg", ArchivedAt: archivedAt}
	f.files[uid] = []photos.PhotoFile{{PhotoUID: uid, FilePath: uid + ".jpg", FileHash: uid + "-hash"}}
}

// GetByUID returns the seeded photo or photos.ErrPhotoNotFound.
func (f *fakePhotoStore) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	p, ok := f.photos[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return p, nil
}

// ListFiles returns the seeded files for uid.
func (f *fakePhotoStore) ListFiles(_ context.Context, uid string) ([]photos.PhotoFile, error) {
	return f.files[uid], nil
}

// DeleteAudited removes the seeded photo and records the audit entry it carried,
// honouring a configured per-uid error. A configured error leaves the row (and
// records no audit entry), mirroring the real store's atomic rollback.
func (f *fakePhotoStore) DeleteAudited(_ context.Context, uid string, entry audit.Entry) error {
	if err := f.deleteErr[uid]; err != nil {
		return err
	}
	if _, ok := f.photos[uid]; !ok {
		return photos.ErrPhotoNotFound
	}
	delete(f.photos, uid)
	f.deleted[uid] = true
	f.auditedAs[uid] = entry
	return nil
}

// ListArchivedUIDs returns archived UIDs oldest-first, applying the before cutoff
// and limit/offset over a deterministic snapshot.
func (f *fakePhotoStore) ListArchivedUIDs(
	_ context.Context, before *time.Time, limit, offset int,
) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	type entry struct {
		uid string
		at  time.Time
	}
	var entries []entry
	for uid, p := range f.photos {
		if p.ArchivedAt == nil {
			continue
		}
		if before != nil && p.ArchivedAt.After(*before) {
			continue
		}
		entries = append(entries, entry{uid: uid, at: *p.ArchivedAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].at.Equal(entries[j].at) {
			return entries[i].uid < entries[j].uid
		}
		return entries[i].at.Before(entries[j].at)
	})
	uids := make([]string, 0, len(entries))
	for _, e := range entries {
		uids = append(uids, e.uid)
	}
	if offset >= len(uids) {
		return []string{}, nil
	}
	end := min(offset+limit, len(uids))
	return uids[offset:end], nil
}

// fakeStorage records deleted relative paths and can fail a specific one.
type fakeStorage struct {
	deleted map[string]bool
	failOn  map[string]error
}

// newFakeStorage returns an empty fake storage.
func newFakeStorage() *fakeStorage {
	return &fakeStorage{deleted: map[string]bool{}, failOn: map[string]error{}}
}

// Delete records relPath as deleted unless configured to fail.
func (f *fakeStorage) Delete(_ context.Context, relPath string) error {
	if err := f.failOn[relPath]; err != nil {
		return err
	}
	f.deleted[relPath] = true
	return nil
}

// fakeThumb records removed hashes and can fail a specific one.
type fakeThumb struct {
	removed map[string]bool
	failOn  map[string]error
}

// newFakeThumb returns an empty fake thumbnailer.
func newFakeThumb() *fakeThumb {
	return &fakeThumb{removed: map[string]bool{}, failOn: map[string]error{}}
}

// Remove records hash as removed unless configured to fail.
func (f *fakeThumb) Remove(hash string) error {
	if err := f.failOn[hash]; err != nil {
		return err
	}
	f.removed[hash] = true
	return nil
}

// fakeRemote records removed keys.
type fakeRemote struct {
	removed map[string]bool
}

// Remove records key as removed.
func (f *fakeRemote) Remove(_ context.Context, key string) error {
	f.removed[key] = true
	return nil
}

// newService wires a Service over the supplied fakes with a 1-day retention and
// a small batch so the batching loop is exercised.
func newService(t *testing.T, store *fakePhotoStore, fs *fakeStorage, th *fakeThumb, remote RemoteRemover) *Service {
	t.Helper()
	return New(Config{
		Photos:        store,
		Storage:       fs,
		Thumbnailer:   th,
		Remote:        remote,
		RetentionDays: 1,
		BatchSize:     2,
	})
}

func TestPurgePhoto_states(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)

	tests := []struct {
		name    string
		uid     string
		archive *time.Time
		seed    bool
		wantErr error
	}{
		{name: "missing photo", uid: "ph_missing", seed: false, wantErr: photos.ErrPhotoNotFound},
		{name: "live photo", uid: "ph_live", seed: true, archive: nil, wantErr: ErrNotArchived},
		{name: "archived photo", uid: "ph_arch", seed: true, archive: &old, wantErr: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := newFakePhotoStore()
			if tt.seed {
				store.seed(tt.uid, tt.archive)
			}
			fs, th := newFakeStorage(), newFakeThumb()
			svc := newService(t, store, fs, th, nil)

			err := svc.PurgePhoto(context.Background(), tt.uid, audit.Meta{ActorUID: "usr_admin"})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("PurgePhoto error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil {
				if !store.deleted[tt.uid] {
					t.Errorf("row %s was not deleted", tt.uid)
				}
				if !fs.deleted[tt.uid+".jpg"] {
					t.Errorf("original %s.jpg was not deleted", tt.uid)
				}
				if !th.removed[tt.uid+"-hash"] {
					t.Errorf("thumbnails for %s-hash were not removed", tt.uid)
				}
				got := store.auditedAs[tt.uid]
				if got.Action != audit.ActionPhotoPurge || got.ActorUID != "usr_admin" {
					t.Errorf("purge audit entry = %+v, want action %q actor usr_admin", got, audit.ActionPhotoPurge)
				}
				if got.Details["source"] != sourceManual {
					t.Errorf("purge audit source = %v, want %q", got.Details["source"], sourceManual)
				}
			}
		})
	}
}

func TestPurgePhoto_removesRemoteObject(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)
	store := newFakePhotoStore()
	store.seed("ph_a", &old)
	fs, th := newFakeStorage(), newFakeThumb()
	remote := &fakeRemote{removed: map[string]bool{}}
	svc := newService(t, store, fs, th, remote)

	if err := svc.PurgePhoto(context.Background(), "ph_a", audit.Meta{}); err != nil {
		t.Fatalf("PurgePhoto: %v", err)
	}
	if !remote.removed["ph_a.jpg"] {
		t.Errorf("remote object ph_a.jpg was not removed")
	}
}

func TestEmptyTrash_purgesAllArchived(t *testing.T) {
	t.Parallel()
	recent := time.Now().Add(-time.Hour)
	old := time.Now().Add(-72 * time.Hour)
	store := newFakePhotoStore()
	store.seed("ph_live", nil)
	store.seed("ph_recent", &recent) // recent but still archived → emptied
	store.seed("ph_old", &old)
	fs, th := newFakeStorage(), newFakeThumb()
	svc := newService(t, store, fs, th, nil)

	res, err := svc.EmptyTrash(context.Background(), audit.Meta{})
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if res.Purged != 2 || res.Failed != 0 {
		t.Fatalf("EmptyTrash result = %+v, want {Purged:2 Failed:0}", res)
	}
	if store.deleted["ph_live"] {
		t.Errorf("live photo must not be purged by EmptyTrash")
	}
	if !store.deleted["ph_recent"] || !store.deleted["ph_old"] {
		t.Errorf("archived photos were not all purged: %v", store.deleted)
	}
}

func TestPurgeExpired_respectsRetention(t *testing.T) {
	t.Parallel()
	recent := time.Now().Add(-time.Hour)   // within 1-day retention → kept
	old := time.Now().Add(-72 * time.Hour) // beyond retention → purged
	store := newFakePhotoStore()
	store.seed("ph_recent", &recent)
	store.seed("ph_old", &old)
	fs, th := newFakeStorage(), newFakeThumb()
	svc := newService(t, store, fs, th, nil)

	res, err := svc.PurgeExpired(context.Background())
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if res.Purged != 1 || res.Failed != 0 {
		t.Fatalf("PurgeExpired result = %+v, want {Purged:1 Failed:0}", res)
	}
	if store.deleted["ph_recent"] {
		t.Errorf("photo within retention was purged")
	}
	if !store.deleted["ph_old"] {
		t.Errorf("expired photo was not purged")
	}
}

func TestPurgeExpired_disabledWhenRetentionNonPositive(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-72 * time.Hour)
	store := newFakePhotoStore()
	store.seed("ph_old", &old)
	fs, th := newFakeStorage(), newFakeThumb()
	svc := New(Config{Photos: store, Storage: fs, Thumbnailer: th, RetentionDays: 0})

	res, err := svc.PurgeExpired(context.Background())
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if res != (Result{}) {
		t.Fatalf("PurgeExpired with retention 0 = %+v, want zero", res)
	}
	if store.deleted["ph_old"] {
		t.Errorf("scheduled purge must be disabled when retention <= 0")
	}
}

func TestPurgeArchived_keepsRowWhenArtifactDeleteFails(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-72 * time.Hour)
	store := newFakePhotoStore()
	store.seed("ph_ok", &old)
	store.seed("ph_bad", &old)
	fs, th := newFakeStorage(), newFakeThumb()
	fs.failOn["ph_bad.jpg"] = errors.New("disk on fire")
	svc := newService(t, store, fs, th, nil)

	res, err := svc.EmptyTrash(context.Background(), audit.Meta{})
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if res.Purged != 1 || res.Failed != 1 {
		t.Fatalf("result = %+v, want {Purged:1 Failed:1}", res)
	}
	if !store.deleted["ph_ok"] {
		t.Errorf("healthy photo was not purged")
	}
	if store.deleted["ph_bad"] {
		t.Errorf("photo with failed artifact deletion must keep its row for retry")
	}
}

func TestNew_panicsOnMissingCollaborators(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Errorf("New did not panic on nil collaborators")
		}
	}()
	New(Config{})
}
