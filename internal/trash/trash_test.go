package trash

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storekeys"
	"github.com/panbotka/kukatko/internal/thumb"
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

// testProject is the project the unit tests' photos live in; otherProject is a
// second one, for the isolation cases.
const (
	testProject  = "pj_test"
	otherProject = "pj_other"
)

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

// seed adds a photo of testProject with the given archived time (nil = live)
// and one original file whose path and hash derive from the uid.
func (f *fakePhotoStore) seed(uid string, archivedAt *time.Time) {
	f.seedIn(testProject, uid, archivedAt)
}

// seedIn is seed for a photo of projectUID.
func (f *fakePhotoStore) seedIn(projectUID, uid string, archivedAt *time.Time) {
	f.photos[uid] = photos.Photo{
		UID: uid, ProjectID: projectUID, FileHash: uid + "-hash", FilePath: uid + ".jpg", ArchivedAt: archivedAt,
	}
	f.files[uid] = []photos.PhotoFile{{PhotoUID: uid, FilePath: uid + ".jpg", FileHash: uid + "-hash"}}
}

// inProject returns the seeded photo when it belongs to projectUID, like the
// real store's project-scoped reads.
func (f *fakePhotoStore) inProject(projectUID, uid string) (photos.Photo, bool) {
	p, ok := f.photos[uid]
	if !ok || p.ProjectID != projectUID {
		return photos.Photo{}, false
	}
	return p, true
}

// GetByUIDInProject returns the project's seeded photo or
// photos.ErrPhotoNotFound.
func (f *fakePhotoStore) GetByUIDInProject(_ context.Context, projectUID, uid string) (photos.Photo, error) {
	p, ok := f.inProject(projectUID, uid)
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return p, nil
}

// ListFiles returns the seeded files for the project's uid.
func (f *fakePhotoStore) ListFiles(_ context.Context, projectUID, uid string) ([]photos.PhotoFile, error) {
	if _, ok := f.inProject(projectUID, uid); !ok {
		return []photos.PhotoFile{}, nil
	}
	return f.files[uid], nil
}

// DeleteAudited removes the project's seeded photo and records the audit entry
// it carried, honouring a configured per-uid error. A configured error leaves
// the row (and records no audit entry), mirroring the real store's atomic
// rollback.
func (f *fakePhotoStore) DeleteAudited(_ context.Context, projectUID, uid string, entry audit.Entry) error {
	if err := f.deleteErr[uid]; err != nil {
		return err
	}
	if _, ok := f.inProject(projectUID, uid); !ok {
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
	_ context.Context, projectUID string, before *time.Time, limit, offset int,
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
		if p.ArchivedAt == nil || p.ProjectID != projectUID {
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
func (f *fakeThumb) Remove(prefix, hash string) error {
	if err := f.failOn[hash]; err != nil {
		return err
	}
	f.removed[prefix+hash] = true
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
func newService(t *testing.T, store *fakePhotoStore, fs FileStorage, th *fakeThumb, remote RemoteRemover) *Service {
	t.Helper()
	return New(Config{
		Photos:        store,
		Projects:      fakeProjects{active(testProject)},
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

			err := svc.PurgePhoto(context.Background(), testProject, tt.uid, audit.Meta{ActorUID: "usr_admin"})
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

	if err := svc.PurgePhoto(context.Background(), testProject, "ph_a", audit.Meta{}); err != nil {
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

	res, err := svc.EmptyTrash(context.Background(), testProject, audit.Meta{})
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

func TestPurgeOlderThan_removesOnlyOlder(t *testing.T) {
	t.Parallel()
	recent := time.Now().Add(-time.Hour)   // 1h old → newer than a 1-day cutoff → kept
	old := time.Now().Add(-72 * time.Hour) // 3 days old → older than a 1-day cutoff → purged
	store := newFakePhotoStore()
	store.seed("ph_live", nil)
	store.seed("ph_recent", &recent)
	store.seed("ph_old", &old)
	fs, th := newFakeStorage(), newFakeThumb()
	svc := newService(t, store, fs, th, nil)

	res, err := svc.PurgeOlderThan(context.Background(), testProject, 1, audit.Meta{ActorUID: "usr_admin"})
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if res.Purged != 1 || res.Failed != 0 {
		t.Fatalf("result = %+v, want {Purged:1 Failed:0}", res)
	}
	if store.deleted["ph_live"] || store.deleted["ph_recent"] {
		t.Errorf("only the older archived photo should be purged: %v", store.deleted)
	}
	if !store.deleted["ph_old"] {
		t.Errorf("photo older than the cutoff was not purged")
	}
	if got := store.auditedAs["ph_old"]; got.ActorUID != "usr_admin" || got.Details["source"] != sourcePurgeOlder {
		t.Errorf("purge audit entry = %+v, want actor usr_admin source %q", got, sourcePurgeOlder)
	}
}

func TestPurgeOlderThan_zeroDaysEmptiesAll(t *testing.T) {
	t.Parallel()
	recent := time.Now().Add(-time.Hour)
	old := time.Now().Add(-72 * time.Hour)
	store := newFakePhotoStore()
	store.seed("ph_live", nil)
	store.seed("ph_recent", &recent)
	store.seed("ph_old", &old)
	fs, th := newFakeStorage(), newFakeThumb()
	svc := newService(t, store, fs, th, nil)

	// days == 0 means "everything currently in trash", equivalent to EmptyTrash.
	res, err := svc.PurgeOlderThan(context.Background(), testProject, 0, audit.Meta{})
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if res.Purged != 2 || res.Failed != 0 {
		t.Fatalf("result = %+v, want {Purged:2 Failed:0}", res)
	}
	if store.deleted["ph_live"] {
		t.Errorf("live photo must not be purged")
	}
	if !store.deleted["ph_recent"] || !store.deleted["ph_old"] {
		t.Errorf("every archived photo should be purged with days=0: %v", store.deleted)
	}
}

func TestPurgeOlderThan_negativeDaysClampedToZero(t *testing.T) {
	t.Parallel()
	recent := time.Now().Add(-time.Hour)
	store := newFakePhotoStore()
	store.seed("ph_recent", &recent)
	fs, th := newFakeStorage(), newFakeThumb()
	svc := newService(t, store, fs, th, nil)

	// A negative age is clamped to 0 (the cutoff never lands in the future), so it
	// behaves like days=0 rather than purging photos archived "after now".
	res, err := svc.PurgeOlderThan(context.Background(), testProject, -5, audit.Meta{})
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if res.Purged != 1 || res.Failed != 0 {
		t.Fatalf("result = %+v, want {Purged:1 Failed:0}", res)
	}
	if !store.deleted["ph_recent"] {
		t.Errorf("negative days should clamp to 0 and purge all archived: %v", store.deleted)
	}
}

func TestPurgeExpired_disabledWhenRetentionNonPositive(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-72 * time.Hour)
	store := newFakePhotoStore()
	store.seed("ph_old", &old)
	fs, th := newFakeStorage(), newFakeThumb()
	svc := New(Config{
		Photos: store, Projects: fakeProjects{active(testProject)}, Storage: fs, Thumbnailer: th, RetentionDays: 0,
	})

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

	res, err := svc.EmptyTrash(context.Background(), testProject, audit.Meta{})
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

// fakeListingStorage is a fakeStorage that can also list its keys by prefix,
// which is the shape of a real backend and the only shape from which the purge
// can find a video's HLS segments.
type fakeListingStorage struct {
	*fakeStorage
	// keys is everything the store holds, in the order a listing yields it.
	keys []string
	// listErr, when non-nil, is what every listing fails with.
	listErr error
}

// newFakeListingStorage returns a store holding keys.
func newFakeListingStorage(keys ...string) *fakeListingStorage {
	return &fakeListingStorage{fakeStorage: newFakeStorage(), keys: keys}
}

// KeysWithPrefix yields every held key starting with prefix, matched literally
// exactly as the real backends match it.
func (f *fakeListingStorage) KeysWithPrefix(_ context.Context, prefix string, yield func(key string) error) error {
	if f.listErr != nil {
		return f.listErr
	}
	for _, key := range f.keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if err := yield(key); err != nil {
			return err
		}
	}
	return nil
}

// compile-time assertion that the fake is the interface the purge asserts for.
var _ storage.PrefixLister = (*fakeListingStorage)(nil)

// seedHashed adds an archived photo whose file hash is a real hex digest, which
// is what the HLS layout requires to address a video's segments.
func (f *fakePhotoStore) seedHashed(uid, hash string, archivedAt *time.Time) {
	f.photos[uid] = photos.Photo{
		UID: uid, ProjectID: testProject, FileHash: hash, FilePath: uid + ".mp4", ArchivedAt: archivedAt,
	}
	f.files[uid] = []photos.PhotoFile{{PhotoUID: uid, FilePath: uid + ".mp4", FileHash: hash}}
}

// TestPurgePhoto_removesHLSObjects verifies a purged video's streaming segments
// go with it — every rendition, init segment and media segment — while another
// video's segments and everything outside the prefix are left alone.
func TestPurgePhoto_removesHLSObjects(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)
	const (
		purged = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
		kept   = "ffeeddccbbaa00112233445566778899aabbccddeeff00112233445566778899"
	)
	store := newFakePhotoStore()
	store.seedHashed("ph_video", purged, &old)
	fs := newFakeListingStorage(
		"hls/"+purged+"/1080p/init.mp4",
		"hls/"+purged+"/1080p/00000.m4s",
		"hls/"+purged+"/1080p/00001.m4s",
		"hls/"+purged+"/720p/init.mp4",
		"hls/"+kept+"/1080p/init.mp4",
		"thumb/ff/ee/dd/"+kept+"_tile_500.jpg",
	)
	// The purge asserts its storage for the listing interface, so the fake it is
	// wired with is the listing one.
	svc := newService(t, store, fs, newFakeThumb(), nil)

	if err := svc.PurgePhoto(context.Background(), testProject, "ph_video", audit.Meta{}); err != nil {
		t.Fatalf("PurgePhoto: %v", err)
	}
	for _, key := range []string{
		"hls/" + purged + "/1080p/init.mp4",
		"hls/" + purged + "/1080p/00000.m4s",
		"hls/" + purged + "/1080p/00001.m4s",
		"hls/" + purged + "/720p/init.mp4",
	} {
		if !fs.deleted[key] {
			t.Errorf("HLS object %s was not deleted", key)
		}
	}
	if fs.deleted["hls/"+kept+"/1080p/init.mp4"] {
		t.Error("another video's HLS objects were deleted")
	}
	if fs.deleted["thumb/ff/ee/dd/"+kept+"_tile_500.jpg"] {
		t.Error("another video's thumbnail object was deleted")
	}
}

// TestPurgePhoto_hlsListingFailureIsReported verifies a store that refuses to
// list fails the purge rather than deleting the row while its segments remain:
// a failed listing is the store refusing work it can do, unlike a backend that
// cannot list at all.
func TestPurgePhoto_hlsListingFailureIsReported(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)
	const hash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	store := newFakePhotoStore()
	store.seedHashed("ph_video", hash, &old)
	fs := newFakeListingStorage("hls/" + hash + "/1080p/init.mp4")
	fs.listErr = errors.New("bucket unreachable")
	svc := newService(t, store, fs, newFakeThumb(), nil)

	if err := svc.PurgePhoto(context.Background(), testProject, "ph_video", audit.Meta{}); err == nil {
		t.Fatal("PurgePhoto succeeded, want the listing failure reported")
	}
	if store.deleted["ph_video"] {
		t.Error("the row was deleted although its HLS objects could not be found")
	}
}

// TestPurgePhoto_hlsSkippedWhenUnaddressable verifies the two ways a purge has no
// segments to look for — a backend that cannot list by prefix, and a file hash
// the HLS layout cannot address — leave the purge itself intact.
func TestPurgePhoto_hlsSkippedWhenUnaddressable(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)

	t.Run("backend cannot list by prefix", func(t *testing.T) {
		t.Parallel()
		store := newFakePhotoStore()
		store.seedHashed("ph_video", "aabbccddeeff", &old)
		svc := newService(t, store, newFakeStorage(), newFakeThumb(), nil)
		if err := svc.PurgePhoto(context.Background(), testProject, "ph_video", audit.Meta{}); err != nil {
			t.Fatalf("PurgePhoto: %v", err)
		}
		if !store.deleted["ph_video"] {
			t.Error("the row was not purged")
		}
	})

	t.Run("hash the layout cannot address", func(t *testing.T) {
		t.Parallel()
		store := newFakePhotoStore()
		store.seed("ph_odd", &old) // seed gives a non-hex "ph_odd-hash"
		fs := newFakeListingStorage("hls/aabbccddeeff/1080p/init.mp4")
		svc := newService(t, store, fs, newFakeThumb(), nil)
		if err := svc.PurgePhoto(context.Background(), testProject, "ph_odd", audit.Meta{}); err != nil {
			t.Fatalf("PurgePhoto: %v", err)
		}
		if !store.deleted["ph_odd"] {
			t.Error("the row was not purged")
		}
		if fs.deleted["hls/aabbccddeeff/1080p/init.mp4"] {
			t.Error("an unrelated video's HLS objects were deleted")
		}
	})
}

// fakeProjects is a ProjectLister over a fixed project list.
type fakeProjects []project.Project

// List returns the fixed projects.
func (f fakeProjects) List(context.Context) ([]project.Project, error) {
	return f, nil
}

// active returns an active project with the given uid.
func active(uid string) project.Project {
	return project.Project{UID: uid, Active: true}
}

// TestPurgePhoto_foreignPhotoIsNotFound checks a photo of another project is
// answered exactly like a missing one and is left in its own trash untouched.
func TestPurgePhoto_foreignPhotoIsNotFound(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)
	store := newFakePhotoStore()
	store.seedIn(otherProject, "ph_theirs", &old)
	fs, th := newFakeStorage(), newFakeThumb()
	svc := newService(t, store, fs, th, nil)

	err := svc.PurgePhoto(context.Background(), testProject, "ph_theirs", audit.Meta{})
	if !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Fatalf("PurgePhoto of a foreign photo = %v, want ErrPhotoNotFound", err)
	}
	if store.deleted["ph_theirs"] || fs.deleted["ph_theirs.jpg"] || len(th.removed) != 0 {
		t.Errorf("a refused purge touched the other project's photo or its files")
	}
}

// TestEmptyTrash_onlyTheProject empties one project's trash and leaves the
// other project's archived photo — older, and listed in no other way — alone.
func TestEmptyTrash_onlyTheProject(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)
	store := newFakePhotoStore()
	store.seed("ph_mine", &old)
	store.seedIn(otherProject, "ph_theirs", &old)
	svc := newService(t, store, newFakeStorage(), newFakeThumb(), nil)

	res, err := svc.EmptyTrash(context.Background(), testProject, audit.Meta{})
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if res.Purged != 1 || !store.deleted["ph_mine"] || store.deleted["ph_theirs"] {
		t.Errorf("EmptyTrash = %+v, deleted %v; want only ph_mine purged", res, store.deleted)
	}
}

// TestPurgeExpired_sweepsEveryActiveProject checks the scheduled retention purge
// visits each active project's trash in turn and skips a deactivated project,
// whose data is kept as it is.
func TestPurgeExpired_sweepsEveryActiveProject(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-72 * time.Hour)
	store := newFakePhotoStore()
	store.seedIn(testProject, "ph_a", &old)
	store.seedIn(otherProject, "ph_b", &old)
	store.seedIn("pj_closed", "ph_closed", &old)
	svc := New(Config{
		Photos: store,
		Projects: fakeProjects{
			active(testProject), active(otherProject), {UID: "pj_closed", Active: false},
		},
		Storage: newFakeStorage(), Thumbnailer: newFakeThumb(), RetentionDays: 1,
	})

	res, err := svc.PurgeExpired(context.Background())
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if res.Purged != 2 || !store.deleted["ph_a"] || !store.deleted["ph_b"] {
		t.Errorf("PurgeExpired = %+v, deleted %v; want ph_a and ph_b", res, store.deleted)
	}
	if store.deleted["ph_closed"] {
		t.Errorf("the retention purge swept a deactivated project")
	}
}

// TestPurgePhoto_keysComeFromTheRow seeds a photo whose original lives under a
// project's storage prefix and asserts the purge deletes exactly the key its
// file row names — and the sidecar derived from that key — rather than any key
// recomputed from the project or the hash, so a photo is cleaned up where it
// actually lives, whichever layout it was stored in.
func TestPurgePhoto_keysComeFromTheRow(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)
	const key = "projects/pj_test/2026/10/ph_pref.jpg"
	store := newFakePhotoStore()
	store.photos["ph_pref"] = photos.Photo{
		UID: "ph_pref", ProjectID: testProject, FileHash: "ph_pref-hash", FilePath: key, ArchivedAt: &old,
	}
	store.files["ph_pref"] = []photos.PhotoFile{{PhotoUID: "ph_pref", FilePath: key, FileHash: "ph_pref-hash"}}
	fs := newFakeStorage()
	svc := newService(t, store, fs, newFakeThumb(), nil)

	if err := svc.PurgePhoto(context.Background(), testProject, "ph_pref", audit.Meta{}); err != nil {
		t.Fatalf("PurgePhoto: %v", err)
	}
	sidecar, err := sidecarexport.KeyFor(key)
	if err != nil {
		t.Fatalf("KeyFor: %v", err)
	}
	for _, want := range []string{key, sidecar} {
		if !fs.deleted[want] {
			t.Errorf("purge did not delete %s; deleted %v", want, fs.deleted)
		}
	}
}

// fakeSprites records the (prefix, hash) of every storyboard sprite removed.
type fakeSprites struct {
	removed []string
}

// Remove records prefix+hash as removed.
func (f *fakeSprites) Remove(prefix, hash string) error {
	f.removed = append(f.removed, prefix+hash)
	return nil
}

// TestPurgePhoto_followsTheOriginalsLayout verifies a purge of a photo whose
// original lives under a project prefix finds every derived object under that
// same prefix — the thumbnails, the storyboard and the HLS segments — and leaves
// the root-layout objects of identical bytes (one hash) alone.
func TestPurgePhoto_followsTheOriginalsLayout(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)
	const (
		prefix = "projects/pj0123456789abcdefghijklmn/"
		hash   = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
		path   = prefix + "2024/05/clip.mp4"
	)
	store := newFakePhotoStore()
	store.photos["ph_video"] = photos.Photo{
		UID: "ph_video", ProjectID: testProject, FileHash: hash, FilePath: path, ArchivedAt: &old,
	}
	store.files["ph_video"] = []photos.PhotoFile{{PhotoUID: "ph_video", FilePath: path, FileHash: hash}}
	fs := newFakeListingStorage(
		prefix+"hls/"+hash+"/1080p/init.mp4",
		prefix+"hls/"+hash+"/1080p/00000.m4s",
		"hls/"+hash+"/1080p/init.mp4",
	)
	th, sprites := newFakeThumb(), &fakeSprites{}
	svc := New(Config{
		Photos: store, Projects: fakeProjects{active(testProject)}, Storage: fs,
		Thumbnailer: th, Storyboards: sprites, RetentionDays: 1,
	})

	if err := svc.PurgePhoto(context.Background(), testProject, "ph_video", audit.Meta{}); err != nil {
		t.Fatalf("PurgePhoto: %v", err)
	}
	for _, key := range []string{
		path,
		prefix + "sidecars/2024/05/clip.mp4.yml",
		prefix + "hls/" + hash + "/1080p/init.mp4",
		prefix + "hls/" + hash + "/1080p/00000.m4s",
	} {
		if !fs.deleted[key] {
			t.Errorf("%s was not deleted", key)
		}
	}
	if fs.deleted["hls/"+hash+"/1080p/init.mp4"] {
		t.Error("the root-layout segments of the same hash were deleted")
	}
	if !th.removed[prefix+hash] || th.removed[hash] {
		t.Errorf("thumbnails removed = %v, want only %q", th.removed, prefix+hash)
	}
	if len(sprites.removed) != 1 || sprites.removed[0] != prefix+hash {
		t.Errorf("storyboards removed = %v, want [%s]", sprites.removed, prefix+hash)
	}
}

// fakeMoveLock stands in for the storage move's photo lock: it refuses the
// photos in busy with storekeys.ErrMoving and otherwise runs the purge, recording
// which photo it ran and whether the purge's deletions happened under it.
type fakeMoveLock struct {
	busy  map[string]bool
	store *fakePhotoStore
	held  []string
}

// Purge runs purge unless the photo is busy, and checks the row went inside it.
func (f *fakeMoveLock) Purge(_ context.Context, projectUID, photoUID string, purge func() error) error {
	if f.busy[photoUID] {
		return fmt.Errorf("rekey: %w: %s", storekeys.ErrMoving, photoUID)
	}
	if err := purge(); err != nil {
		return err
	}
	if f.store.deleted[photoUID] {
		f.held = append(f.held, projectUID+"|"+photoUID)
	}
	return nil
}

// TestPurge_underTheMoveLock verifies every purge — the manual one, empty-trash
// and the scheduled retention — deletes under the storage move's photo lock,
// and that a photo the move holds is left in the trash, its row and original
// untouched: the manual purge reports storekeys.ErrMoving, a batch counts it as
// failed and goes on with the rest.
func TestPurge_underTheMoveLock(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-72 * time.Hour)
	store := newFakePhotoStore()
	store.seed("ph_free", &old)
	store.seed("ph_moving", &old)
	fs, th := newFakeStorage(), newFakeThumb()
	lock := &fakeMoveLock{busy: map[string]bool{"ph_moving": true}, store: store}
	svc := New(Config{
		Photos: store, Projects: fakeProjects{active(testProject)}, Storage: fs, Thumbnailer: th,
		Moves: lock, RetentionDays: 1,
	})

	if err := svc.PurgePhoto(t.Context(), testProject, "ph_moving", audit.Meta{}); !errors.Is(err, storekeys.ErrMoving) {
		t.Fatalf("PurgePhoto of a photo the move holds = %v, want storekeys.ErrMoving", err)
	}
	res, err := svc.PurgeExpired(t.Context())
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if res.Purged != 1 || res.Failed != 1 {
		t.Errorf("PurgeExpired = %+v, want one purged and the held one failed", res)
	}
	if store.deleted["ph_moving"] || fs.deleted["ph_moving.jpg"] {
		t.Errorf("the photo the move holds was purged (row %v, original %v)",
			store.deleted["ph_moving"], fs.deleted["ph_moving.jpg"])
	}
	if want := []string{testProject + "|ph_free"}; !slices.Equal(lock.held, want) {
		t.Errorf("purged under the lock: %v, want %v", lock.held, want)
	}
}

// purgedHash and keptHash are two valid content hashes: the purged file's and
// another file's sharing nothing with it.
const (
	purgedHash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	keptHash   = "ffeeddccbbaa00112233445566778899aabbccddeeff00112233445566778899"
)

// thumbKey returns the store key of hash's thumbnail of the named size under
// prefix, failing the test on a layout error.
func thumbKey(t *testing.T, prefix, hash, size string) string {
	t.Helper()
	key, err := thumb.RelPath(prefix, hash, size)
	if err != nil {
		t.Fatalf("thumb.RelPath: %v", err)
	}
	return key
}

// seedStoredAt adds an archived photo of the test project whose only file is
// stored at filePath with the given content hash.
func (f *fakePhotoStore) seedStoredAt(uid, filePath, hash string, archivedAt *time.Time) {
	f.photos[uid] = photos.Photo{
		UID: uid, ProjectID: testProject, FileHash: hash, FilePath: filePath, ArchivedAt: archivedAt,
	}
	f.files[uid] = []photos.PhotoFile{{PhotoUID: uid, FilePath: filePath, FileHash: hash}}
}

// TestPurgePhoto_removesStoredThumbnails verifies a purge deletes the thumbnail
// objects the store holds for the purged file — every size the listing finds, a
// size no longer registered included — and not only the local cache, while
// another file's thumbnail objects stay.
func TestPurgePhoto_removesStoredThumbnails(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)
	store := newFakePhotoStore()
	store.seedStoredAt("ph_pic", "2023/01/pic.jpg", purgedHash, &old)
	sizes := thumb.SizeNames()
	purged := []string{
		thumbKey(t, "", purgedHash, sizes[0]),
		thumbKey(t, "", purgedHash, sizes[1]),
		"thumb/aa/bb/cc/" + purgedHash + "_retired_size.jpg",
	}
	kept := thumbKey(t, "", keptHash, sizes[0])
	fs := newFakeListingStorage(append(slices.Clone(purged), kept)...)
	svc := newService(t, store, fs, newFakeThumb(), nil)

	if err := svc.PurgePhoto(context.Background(), testProject, "ph_pic", audit.Meta{}); err != nil {
		t.Fatalf("PurgePhoto: %v", err)
	}
	for _, key := range purged {
		if !fs.deleted[key] {
			t.Errorf("thumbnail object %s survives the purge", key)
		}
	}
	if fs.deleted[kept] {
		t.Errorf("another file's thumbnail object %s was deleted", kept)
	}
}

// TestPurgePhoto_storedThumbnailsWithoutListing verifies a store that cannot
// list by prefix still loses every registered size's thumbnail object.
func TestPurgePhoto_storedThumbnailsWithoutListing(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)
	store := newFakePhotoStore()
	store.seedStoredAt("ph_pic", "2023/01/pic.jpg", purgedHash, &old)
	fs := newFakeStorage()
	svc := newService(t, store, fs, newFakeThumb(), nil)

	if err := svc.PurgePhoto(context.Background(), testProject, "ph_pic", audit.Meta{}); err != nil {
		t.Fatalf("PurgePhoto: %v", err)
	}
	for _, size := range thumb.SizeNames() {
		if key := thumbKey(t, "", purgedHash, size); !fs.deleted[key] {
			t.Errorf("thumbnail object %s was not deleted", key)
		}
	}
}

// TestPurgePhoto_movedPhotoRootLeftovers verifies a photo a storage move has
// committed under its project's prefix loses its derived objects under both
// layouts — the root ones a failed derived copy left behind included — once no
// project is left in the root layout, and only its prefixed ones while another
// project still uses the root (a root key named after its hash may be that
// project's).
func TestPurgePhoto_movedPhotoRootLeftovers(t *testing.T) {
	t.Parallel()
	old := time.Now().Add(-48 * time.Hour)
	// The fake project's uid is not one a prefix is built from; any valid
	// project prefix stands for its storage prefix.
	prefix := project.StoragePrefixFor(project.BootstrapUID)
	size := thumb.SizeNames()[0]
	prefixed := []string{thumbKey(t, prefix, purgedHash, size), prefix + "hls/" + purgedHash + "/1080p/init.mp4"}
	root := []string{thumbKey(t, "", purgedHash, size), "hls/" + purgedHash + "/1080p/init.mp4"}

	for _, tc := range []struct {
		name     string
		projects fakeProjects
		rootGone bool
	}{
		{"root vacated", fakeProjects{{UID: testProject, Active: true, StoragePrefix: prefix}}, true},
		{"root still in use", fakeProjects{
			{UID: testProject, Active: true, StoragePrefix: prefix}, active(otherProject),
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakePhotoStore()
			store.seedStoredAt("ph_moved", prefix+"2023/01/moved.mp4", purgedHash, &old)
			fs := newFakeListingStorage(append(slices.Clone(prefixed), root...)...)
			th := newFakeThumb()
			svc := New(Config{Photos: store, Projects: tc.projects, Storage: fs, Thumbnailer: th})

			if err := svc.PurgePhoto(context.Background(), testProject, "ph_moved", audit.Meta{}); err != nil {
				t.Fatalf("PurgePhoto: %v", err)
			}
			for _, key := range prefixed {
				if !fs.deleted[key] {
					t.Errorf("prefixed object %s survives the purge", key)
				}
			}
			for _, key := range root {
				if fs.deleted[key] != tc.rootGone {
					t.Errorf("root object %s deleted = %v, want %v", key, fs.deleted[key], tc.rootGone)
				}
			}
			if th.removed[purgedHash] != tc.rootGone {
				t.Errorf("root thumbnail cache removed = %v, want %v", th.removed[purgedHash], tc.rootGone)
			}
		})
	}
}
