//go:build integration

package uploadlink_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// fixture is a truncated database with one curator, one album, one label and
// one photo, which is what most cases need.
type fixture struct {
	db      *database.DB
	store   *uploadlink.Store
	photos  *photos.Store
	org     *organize.Store
	curator string
	album   organize.Album
	label   organize.Label
	photo   string
}

// newFixture truncates the integration database and seeds the fixture.
func newFixture(t *testing.T) fixture {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := context.Background()
	f := fixture{
		db: db, store: uploadlink.NewStore(db.Pool()), photos: photos.NewStore(db.Pool()),
		org: organize.NewStore(db.Pool()), curator: "us_ul_curator",
	}
	if err := auth.NewStore(db.Pool()).CreateUser(ctx, auth.User{
		UID: f.curator, Username: "curator", Email: "curator@example.test",
		PasswordHash: "x", Role: auth.RoleCurator,
	}); err != nil {
		t.Fatalf("creating curator: %v", err)
	}
	var err error
	if f.album, err = f.org.CreateAlbum(ctx, organize.Album{Title: "Pouť 2026"}); err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if f.label, err = f.org.CreateLabel(ctx, organize.Label{Name: "pouť"}); err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	f.photo = f.makePhoto(t, "ulhash1")
	return f
}

// makePhoto inserts a minimal photo with the given file hash and returns its uid.
func (f fixture) makePhoto(t *testing.T, hash string) string {
	t.Helper()
	created, err := f.photos.Create(context.Background(), photos.Photo{
		FileHash: hash, FilePath: "2026/06/" + hash + ".jpg", FileName: hash + ".jpg",
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// createLink creates a link to the fixture's album and label, valid for a day.
func (f fixture) createLink(t *testing.T) (uploadlink.Link, string) {
	t.Helper()
	link, code, err := f.store.Create(context.Background(), uploadlink.NewLink{
		Title: "Pouť 2026 – fotky od vás", Note: "Díky!", CreatedBy: f.curator,
		ExpiresAt: time.Now().Add(24 * time.Hour),
		AlbumUIDs: []string{f.album.UID}, LabelUIDs: []string{f.label.UID, f.label.UID},
	}, audit.Entry{ActorUID: f.curator, Action: audit.ActionUploadLinkCreate})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return link, code
}

// auditCount returns how many entries with action target uid.
func (f fixture) auditCount(t *testing.T, action, targetUID string) int {
	t.Helper()
	var n int
	if err := f.db.Pool().QueryRow(context.Background(),
		"SELECT count(*) FROM audit_log WHERE action = $1 AND target_uid = $2", action, targetUID).Scan(&n); err != nil {
		t.Fatalf("counting audit entries: %v", err)
	}
	return n
}

// TestCreate_storesCodeAndResolvesByCode verifies a created link carries its
// targets and creator, is found by its code (and only by it) through the hash,
// and keeps its code readable so it round-trips through Get.
func TestCreate_storesCodeAndResolvesByCode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, code := f.createLink(t)

	if !uploadlink.ValidCode(code) {
		t.Fatalf("code %q is not a valid code", code)
	}
	if len(link.Albums) != 1 || link.Albums[0].Name != "Pouť 2026" ||
		len(link.Labels) != 1 || link.Labels[0].Name != "pouť" {
		t.Fatalf("targets = %+v / %+v", link.Albums, link.Labels)
	}
	if link.CreatedBy == nil || *link.CreatedBy != f.curator || link.CreatorName != "curator" {
		t.Errorf("creator = %v %q", link.CreatedBy, link.CreatorName)
	}
	var stored string
	if err := f.db.Pool().QueryRow(ctx, "SELECT code_hash FROM upload_links WHERE uid = $1",
		link.UID).Scan(&stored); err != nil {
		t.Fatalf("reading code_hash: %v", err)
	}
	if stored == code || stored != uploadlink.HashSecret(code) {
		t.Errorf("code_hash = %q, want the hash of the code and never the code", stored)
	}
	if link.Code != code {
		t.Errorf("created link Code = %q, want %q", link.Code, code)
	}
	if got, err := f.store.Get(ctx, link.UID); err != nil || got.Code != code {
		t.Errorf("Get Code = %q, %v, want the readable code %q", got.Code, err, code)
	}
	byCode, err := f.store.ByCode(ctx, code)
	if err != nil || byCode.UID != link.UID {
		t.Fatalf("ByCode = %+v, %v", byCode, err)
	}
	if _, err := f.store.ByCode(ctx, "Zz9Zz9Zz"); !errors.Is(err, uploadlink.ErrNotFound) {
		t.Errorf("ByCode(unknown) error = %v, want ErrNotFound", err)
	}
	if got := f.auditCount(t, audit.ActionUploadLinkCreate, link.UID); got != 1 {
		t.Errorf("create audit entries = %d, want 1", got)
	}
}

// TestCreate_unknownTargetRollsBack verifies a missing album refuses the link
// and leaves neither a link nor an audit entry behind.
func TestCreate_unknownTargetRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, _, err := f.store.Create(ctx, uploadlink.NewLink{
		CreatedBy: f.curator, ExpiresAt: time.Now().Add(time.Hour), AlbumUIDs: []string{"al_missing"},
	}, audit.Entry{ActorUID: f.curator, Action: audit.ActionUploadLinkCreate})
	if !errors.Is(err, uploadlink.ErrTargetNotFound) {
		t.Fatalf("Create error = %v, want ErrTargetNotFound", err)
	}
	links, err := f.store.List(ctx, "")
	if err != nil || len(links) != 0 {
		t.Errorf("List = %v, %v; want no links", links, err)
	}
}

// TestList_scopesToCreator verifies a creator sees only their links and the
// empty creator sees all.
func TestList_scopesToCreator(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.createLink(t)
	mine, err := f.store.List(ctx, f.curator)
	if err != nil || len(mine) != 1 {
		t.Fatalf("List(curator) = %d links, %v", len(mine), err)
	}
	others, err := f.store.List(ctx, "us_somebody_else")
	if err != nil || len(others) != 0 {
		t.Errorf("List(other) = %d links, %v; want 0", len(others), err)
	}
	all, err := f.store.List(ctx, "")
	if err != nil || len(all) != 1 {
		t.Errorf("List(all) = %d links, %v; want 1", len(all), err)
	}
}

// TestExtendAndRevoke verifies extending moves the expiry, revoking is final
// and idempotent, and each change is audited once.
func TestExtendAndRevoke(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, _ := f.createLink(t)
	entry := audit.Entry{ActorUID: f.curator}

	later := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	entry.Action = audit.ActionUploadLinkExtend
	extended, err := f.store.Extend(ctx, link.UID, later, entry)
	if err != nil || !extended.ExpiresAt.Equal(later) {
		t.Fatalf("Extend = %v, %v; want expiry %v", extended.ExpiresAt, err, later)
	}
	entry.Action = audit.ActionUploadLinkRevoke
	revoked, err := f.store.Revoke(ctx, link.UID, entry)
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("Revoke = %+v, %v", revoked, err)
	}
	if _, err := f.store.Revoke(ctx, link.UID, entry); err != nil {
		t.Fatalf("second Revoke: %v", err)
	}
	if got := f.auditCount(t, audit.ActionUploadLinkRevoke, link.UID); got != 1 {
		t.Errorf("revoke audit entries = %d, want 1 (the second revoke changes nothing)", got)
	}
	entry.Action = audit.ActionUploadLinkExtend
	if _, err := f.store.Extend(ctx, link.UID, later, entry); !errors.Is(err, uploadlink.ErrRevoked) {
		t.Errorf("Extend(revoked) error = %v, want ErrRevoked", err)
	}
	if _, err := f.store.Extend(ctx, "ul_missing", later, entry); !errors.Is(err, uploadlink.ErrNotFound) {
		t.Errorf("Extend(missing) error = %v, want ErrNotFound", err)
	}
}

// TestRecordUpload_filesIntoTargets verifies an upload puts the photo into the
// link's album and label, stamps the link's last use (the count is the
// reservation's), records its provenance and audits it.
func TestRecordUpload_filesIntoTargets(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, _ := f.createLink(t)

	if err := f.store.ReserveUpload(ctx, link.UID, 0, time.Now()); err != nil {
		t.Fatalf("ReserveUpload: %v", err)
	}
	err := f.store.RecordUpload(ctx, uploadlink.Upload{
		LinkUID: link.UID, PhotoUID: f.photo, Created: true, UploaderName: "Jana", SessionHash: "sess",
	}, audit.Entry{Action: audit.ActionUploadLinkUpload})
	if err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}
	albums, err := f.org.AlbumsForPhoto(ctx, f.photo)
	if err != nil || len(albums) != 1 || albums[0].UID != f.album.UID {
		t.Errorf("AlbumsForPhoto = %+v, %v", albums, err)
	}
	labels, err := f.org.PhotoLabelsForPhoto(ctx, f.photo)
	if err != nil || len(labels) != 1 {
		t.Errorf("PhotoLabelsForPhoto = %+v, %v", labels, err)
	}
	got, err := f.store.Get(ctx, link.UID)
	if err != nil || got.UploadCount != 1 || got.LastUsedAt == nil {
		t.Errorf("counters = %d / %v, %v", got.UploadCount, got.LastUsedAt, err)
	}
	prov, err := f.store.Provenance(ctx, f.photo)
	if err != nil || prov == nil || prov.LinkUID != link.UID || prov.UploaderName != "Jana" ||
		prov.LinkTitle != link.Title || prov.AccountUID != nil || prov.AccountName != "" {
		t.Errorf("Provenance = %+v, %v", prov, err)
	}
	if n := f.auditCount(t, audit.ActionUploadLinkUpload, f.photo); n != 1 {
		t.Errorf("upload audit entries = %d, want 1", n)
	}
	var title, name string
	if err := f.db.Pool().QueryRow(ctx,
		"SELECT details->>'link_title', details->>'uploader_name' FROM audit_log WHERE action = $1 AND target_uid = $2",
		audit.ActionUploadLinkUpload, f.photo).Scan(&title, &name); err != nil || title != link.Title || name != "Jana" {
		t.Errorf("audit details title/name = %q/%q, %v; want %q/Jana", title, name, err, link.Title)
	}
	if err := f.store.RecordUpload(ctx, uploadlink.Upload{LinkUID: "ul_missing", PhotoUID: f.photo},
		audit.Entry{Action: audit.ActionUploadLinkUpload}); !errors.Is(err, uploadlink.ErrNotFound) {
		t.Errorf("RecordUpload(missing link) error = %v, want ErrNotFound", err)
	}
}

// TestReserveUpload_capAndLiveness verifies a reservation takes a slot only
// while the link is live and under its cap (0 = none), names why it refused, and
// that a released slot can be taken again.
func TestReserveUpload_capAndLiveness(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, _ := f.createLink(t)
	now := time.Now()
	count := func() int {
		t.Helper()
		got, err := f.store.Get(ctx, link.UID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		return got.UploadCount
	}

	for i := range 2 {
		if err := f.store.ReserveUpload(ctx, link.UID, 2, now); err != nil {
			t.Fatalf("reservation %d: %v", i, err)
		}
	}
	if err := f.store.ReserveUpload(ctx, link.UID, 2, now); !errors.Is(err, uploadlink.ErrFull) {
		t.Errorf("reservation past the cap error = %v, want ErrFull", err)
	}
	if err := f.store.ReleaseUpload(ctx, link.UID); err != nil {
		t.Fatalf("ReleaseUpload: %v", err)
	}
	if err := f.store.ReserveUpload(ctx, link.UID, 2, now); err != nil {
		t.Errorf("reservation after a release: %v", err)
	}
	if err := f.store.ReserveUpload(ctx, link.UID, 0, now); err != nil || count() != 3 {
		t.Errorf("uncapped reservation: %v, count %d; want 3", err, count())
	}
	if err := f.store.ReserveUpload(ctx, link.UID, 0, link.ExpiresAt); !errors.Is(err, uploadlink.ErrExpired) {
		t.Errorf("reservation at the expiry error = %v, want ErrExpired", err)
	}
	if err := f.store.ReserveUpload(ctx, "ul_missing", 0, now); !errors.Is(err, uploadlink.ErrNotFound) {
		t.Errorf("reservation on a missing link error = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Revoke(ctx, link.UID, audit.Entry{ActorUID: f.curator, Action: audit.ActionUploadLinkRevoke}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := f.store.ReserveUpload(ctx, link.UID, 0, now); !errors.Is(err, uploadlink.ErrRevoked) {
		t.Errorf("reservation on a revoked link error = %v, want ErrRevoked", err)
	}
	if got := count(); got != 3 {
		t.Errorf("count after refusals = %d, want 3", got)
	}
	for range 5 {
		if err := f.store.ReleaseUpload(ctx, link.UID); err != nil {
			t.Fatalf("ReleaseUpload: %v", err)
		}
	}
	if got := count(); got != 0 {
		t.Errorf("count after over-releasing = %d, want it floored at 0", got)
	}
}

// TestProvenance_duplicateIsNotProvenance verifies a duplicate merely filed by a
// link is not reported as having come from it.
func TestProvenance_duplicateIsNotProvenance(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, _ := f.createLink(t)
	if err := f.store.RecordUpload(ctx, uploadlink.Upload{LinkUID: link.UID, PhotoUID: f.photo},
		audit.Entry{Action: audit.ActionUploadLinkUpload}); err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}
	prov, err := f.store.Provenance(ctx, f.photo)
	if err != nil || prov != nil {
		t.Errorf("Provenance = %+v, %v; want nil for a duplicate", prov, err)
	}
}

// TestProvenance_account verifies the provenance names the account an upload is
// attributed to — a signed-in uploader, or the one that claimed the anonymous
// session — and reports none for an unclaimed anonymous upload.
func TestProvenance_account(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, _ := f.createLink(t)
	anon := f.makePhoto(t, "ulhash5")
	for _, up := range []uploadlink.Upload{
		{LinkUID: link.UID, PhotoUID: f.photo, Created: true, UploadedBy: f.curator},
		{LinkUID: link.UID, PhotoUID: anon, Created: true, SessionHash: "anon"},
	} {
		if err := f.store.RecordUpload(ctx, up, audit.Entry{Action: audit.ActionUploadLinkUpload}); err != nil {
			t.Fatalf("RecordUpload: %v", err)
		}
	}
	prov, err := f.store.Provenance(ctx, f.photo)
	if err != nil || prov == nil || prov.AccountUID == nil || *prov.AccountUID != f.curator ||
		prov.AccountName != "curator" || prov.UploaderName != "" {
		t.Errorf("Provenance(signed in) = %+v, %v", prov, err)
	}
	prov, err = f.store.Provenance(ctx, anon)
	if err != nil || prov == nil || prov.AccountUID != nil || prov.AccountName != "" {
		t.Errorf("Provenance(anonymous) = %+v, %v", prov, err)
	}
	attribute(t, f.db, "anon", f.curator)
	prov, err = f.store.Provenance(ctx, anon)
	if err != nil || prov == nil || prov.AccountUID == nil || *prov.AccountUID != f.curator {
		t.Errorf("Provenance(claimed) = %+v, %v", prov, err)
	}
}

// TestAttributeSessionTx verifies a registration claims exactly the photos its
// anonymous session created — not duplicates, not another session's, not a
// photo somebody already owns — and only once.
func TestAttributeSessionTx(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, _ := f.createLink(t)
	other := f.makePhoto(t, "ulhash2")
	dup := f.makePhoto(t, "ulhash3")
	foreign := f.makePhoto(t, "ulhash4")
	for _, up := range []uploadlink.Upload{
		{LinkUID: link.UID, PhotoUID: f.photo, Created: true, SessionHash: "mine"},
		{LinkUID: link.UID, PhotoUID: dup, Created: false, SessionHash: "mine"},
		{LinkUID: link.UID, PhotoUID: foreign, Created: true, SessionHash: "theirs"},
		{LinkUID: link.UID, PhotoUID: other, Created: true, UploadedBy: f.curator},
	} {
		if err := f.store.RecordUpload(ctx, up, audit.Entry{Action: audit.ActionUploadLinkUpload}); err != nil {
			t.Fatalf("RecordUpload: %v", err)
		}
	}
	claimed := attribute(t, f.db, "mine", f.curator)
	if len(claimed) != 1 || claimed[0] != f.photo {
		t.Fatalf("claimed = %v, want only %s", claimed, f.photo)
	}
	photo, err := f.photos.GetByUID(ctx, f.photo)
	if err != nil || photo.UploadedBy == nil || *photo.UploadedBy != f.curator {
		t.Errorf("uploaded_by = %v, %v", photo.UploadedBy, err)
	}
	if again := attribute(t, f.db, "mine", f.curator); len(again) != 0 {
		t.Errorf("second claim = %v, want nothing", again)
	}
	if none := attribute(t, f.db, "", f.curator); len(none) != 0 {
		t.Errorf("empty session claimed %v", none)
	}
}

// attribute runs AttributeSessionTx in its own committed transaction.
func attribute(t *testing.T, db *database.DB, sessionHash, userUID string) []string {
	t.Helper()
	ctx := context.Background()
	var uids []string
	err := pgx.BeginFunc(ctx, db.Pool(), func(tx pgx.Tx) error {
		var err error
		uids, err = uploadlink.AttributeSessionTx(ctx, tx, sessionHash, userUID)
		return err
	})
	if err != nil {
		t.Fatalf("AttributeSessionTx: %v", err)
	}
	return uids
}
