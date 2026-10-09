//go:build integration

package uploadlink_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// legacyLink creates a link and strips its readable code, leaving the row in the
// shape every link created before migration 0091 has: a hash and nothing else.
func (f fixture) legacyLink(t *testing.T) (uploadlink.Link, string) {
	t.Helper()
	link, code := f.createLink(t)
	if _, err := f.db.Pool().Exec(context.Background(),
		"UPDATE upload_links SET code = NULL WHERE uid = $1", link.UID); err != nil {
		t.Fatalf("stripping the code: %v", err)
	}
	return link, code
}

// linkRow is the part of a link row RestoreCode and RotateCode may (or must not)
// touch, read straight from the table.
type linkRow struct {
	hash    string
	code    *string
	expires time.Time
	revoked *time.Time
}

// readRow reads uid's code columns, expiry and revocation.
func (f fixture) readRow(t *testing.T, uid string) linkRow {
	t.Helper()
	var row linkRow
	if err := f.db.Pool().QueryRow(context.Background(),
		"SELECT code_hash, code, expires_at, revoked_at FROM upload_links WHERE uid = $1", uid).
		Scan(&row.hash, &row.code, &row.expires, &row.revoked); err != nil {
		t.Fatalf("reading link row: %v", err)
	}
	return row
}

// restoreEntry is the audit entry a manager's restore carries.
func (f fixture) restoreEntry() audit.Entry {
	return audit.Entry{ActorUID: f.curator, Action: audit.ActionUploadLinkRestoreCode}
}

// TestLegacyLink_resolvesWithoutCode verifies a hash-only row keeps resolving by
// its code and reads as a link whose code is unknown.
func TestLegacyLink_resolvesWithoutCode(t *testing.T) {
	f := newFixture(t)
	link, code := f.legacyLink(t)

	got, err := f.store.ByCode(context.Background(), code)
	if err != nil || got.UID != link.UID {
		t.Fatalf("ByCode(legacy) = %+v, %v, want the link", got, err)
	}
	if got.Code != "" {
		t.Errorf("legacy Code = %q, want empty", got.Code)
	}
}

// TestRestoreCode_matching verifies the original code is stored, the link keeps
// resolving by that same code, nothing else changes, and one audit entry is
// written — a second restore is a no-op without another.
func TestRestoreCode_matching(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, code := f.legacyLink(t)
	before := f.readRow(t, link.UID)

	restored, err := f.store.RestoreCode(ctx, link.UID, code, f.restoreEntry())
	if err != nil {
		t.Fatalf("RestoreCode: %v", err)
	}
	if restored.Code != code {
		t.Errorf("restored Code = %q, want %q", restored.Code, code)
	}
	after := f.readRow(t, link.UID)
	if after.hash != before.hash || !after.expires.Equal(before.expires) || after.revoked != nil {
		t.Errorf("row changed beyond the code: before %+v, after %+v", before, after)
	}
	if byCode, err := f.store.ByCode(ctx, code); err != nil || byCode.UID != link.UID || byCode.Code != code {
		t.Errorf("ByCode after restore = %+v, %v, want the same link with its code", byCode, err)
	}
	if _, err := f.store.RestoreCode(ctx, link.UID, code, f.restoreEntry()); err != nil {
		t.Fatalf("second RestoreCode: %v", err)
	}
	if got := f.auditCount(t, audit.ActionUploadLinkRestoreCode, link.UID); got != 1 {
		t.Errorf("restore audit entries = %d, want 1", got)
	}
}

// TestRestoreCode_mismatch verifies any other code — well-formed or not — is
// refused with ErrCodeMismatch and leaves the row and the trail untouched.
func TestRestoreCode_mismatch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, code := f.legacyLink(t)
	before := f.readRow(t, link.UID)

	for _, wrong := range []string{"Zz9Zz9Zz", "short", code + "x", ""} {
		if _, err := f.store.RestoreCode(ctx, link.UID, wrong, f.restoreEntry()); !errors.Is(err,
			uploadlink.ErrCodeMismatch) {
			t.Errorf("RestoreCode(%q) error = %v, want ErrCodeMismatch", wrong, err)
		}
	}
	after := f.readRow(t, link.UID)
	if after.code != nil || after.hash != before.hash {
		t.Errorf("row changed after mismatches: %+v", after)
	}
	if got := f.auditCount(t, audit.ActionUploadLinkRestoreCode, link.UID); got != 0 {
		t.Errorf("restore audit entries = %d, want 0", got)
	}
}

// TestRotateCode verifies a fresh code replaces the old one: the old code no
// longer resolves, the new one does and is readable, and an audit entry is
// written.
func TestRotateCode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, oldCode := f.legacyLink(t)
	before := f.readRow(t, link.UID)

	rotated, err := f.store.RotateCode(ctx, link.UID,
		audit.Entry{ActorUID: f.curator, Action: audit.ActionUploadLinkRotateCode})
	if err != nil {
		t.Fatalf("RotateCode: %v", err)
	}
	if !uploadlink.ValidCode(rotated.Code) || rotated.Code == oldCode {
		t.Fatalf("rotated Code = %q, want a fresh valid code", rotated.Code)
	}
	if _, err := f.store.ByCode(ctx, oldCode); !errors.Is(err, uploadlink.ErrNotFound) {
		t.Errorf("ByCode(old) error = %v, want ErrNotFound", err)
	}
	if got, err := f.store.ByCode(ctx, rotated.Code); err != nil || got.UID != link.UID {
		t.Errorf("ByCode(new) = %+v, %v, want the link", got, err)
	}
	after := f.readRow(t, link.UID)
	if after.hash != uploadlink.HashSecret(rotated.Code) || !after.expires.Equal(before.expires) {
		t.Errorf("row after rotate = %+v, want the new hash and the old expiry", after)
	}
	if got := f.auditCount(t, audit.ActionUploadLinkRotateCode, link.UID); got != 1 {
		t.Errorf("rotate audit entries = %d, want 1", got)
	}
}

// TestRecode_revokedAndExpired verifies a revoked link refuses both actions with
// ErrRevoked (as Extend does) and an expired one accepts them.
func TestRecode_revokedAndExpired(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	revoked, code := f.legacyLink(t)
	if _, err := f.store.Revoke(ctx, revoked.UID,
		audit.Entry{ActorUID: f.curator, Action: audit.ActionUploadLinkRevoke}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := f.store.RestoreCode(ctx, revoked.UID, code, f.restoreEntry()); !errors.Is(err,
		uploadlink.ErrRevoked) {
		t.Errorf("RestoreCode(revoked) error = %v, want ErrRevoked", err)
	}
	if _, err := f.store.RotateCode(ctx, revoked.UID, f.restoreEntry()); !errors.Is(err, uploadlink.ErrRevoked) {
		t.Errorf("RotateCode(revoked) error = %v, want ErrRevoked", err)
	}
	if row := f.readRow(t, revoked.UID); row.code != nil || row.hash != uploadlink.HashSecret(code) {
		t.Errorf("revoked row changed: %+v", row)
	}

	expired, code := f.legacyLink(t)
	if _, err := f.db.Pool().Exec(ctx, "UPDATE upload_links SET expires_at = now() - interval '1 day' WHERE uid = $1",
		expired.UID); err != nil {
		t.Fatalf("expiring: %v", err)
	}
	if got, err := f.store.RestoreCode(ctx, expired.UID, code, f.restoreEntry()); err != nil || got.Code != code {
		t.Errorf("RestoreCode(expired) = %q, %v, want the code stored", got.Code, err)
	}
	if _, err := f.store.RotateCode(ctx, "ul_missing", f.restoreEntry()); !errors.Is(err, uploadlink.ErrNotFound) {
		t.Errorf("RotateCode(unknown) error = %v, want ErrNotFound", err)
	}
}
