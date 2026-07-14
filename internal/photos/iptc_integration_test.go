//go:build integration

package photos_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover the IPTC/XMP credit fields and the
// file-technical columns added by 0027_photos_iptc_metadata: the insert/read
// round trip, and which of them Store.UpdateMetadata may write.

// iptcPhoto builds a Photo with every column of 0027 populated with a distinct,
// recognisable value, so a column silently missing from the insert or the scan
// order shows up as a mismatch rather than as a plausible-looking zero.
func iptcPhoto(hash string) photos.Photo {
	return photos.Photo{
		FileHash:     hash,
		FilePath:     "2023/06/" + hash + ".jpg",
		FileName:     hash + ".jpg",
		FileMime:     "image/jpeg",
		Subject:      "Summer holiday at the lake",
		Keywords:     "lake,summer,holiday",
		Artist:       "Jan Novák",
		Copyright:    "© 2023 Jan Novák",
		License:      "CC BY-NC 4.0",
		Software:     "Adobe Lightroom 12.4",
		Scan:         true,
		ColorProfile: "Apple Wide Color Sharing Profile",
		ImageCodec:   "heic",
		CameraSerial: "SN-12345678",
		OriginalName: "IMG_0042.HEIC",
		Projection:   "equirectangular",
	}
}

// assertIPTC compares every column of 0027 against the values iptcPhoto seeded.
func assertIPTC(t *testing.T, got photos.Photo) {
	t.Helper()
	fields := []struct {
		name string
		got  string
		want string
	}{
		{"subject", got.Subject, "Summer holiday at the lake"},
		{"keywords", got.Keywords, "lake,summer,holiday"},
		{"artist", got.Artist, "Jan Novák"},
		{"copyright", got.Copyright, "© 2023 Jan Novák"},
		{"license", got.License, "CC BY-NC 4.0"},
		{"software", got.Software, "Adobe Lightroom 12.4"},
		{"color_profile", got.ColorProfile, "Apple Wide Color Sharing Profile"},
		{"image_codec", got.ImageCodec, "heic"},
		{"camera_serial", got.CameraSerial, "SN-12345678"},
		{"original_name", got.OriginalName, "IMG_0042.HEIC"},
		{"projection", got.Projection, "equirectangular"},
	}
	for _, f := range fields {
		if f.got != f.want {
			t.Errorf("%s = %q, want %q", f.name, f.got, f.want)
		}
	}
	if !got.Scan {
		t.Error("scan = false, want true")
	}
}

// TestIPTCColumns_roundTrip verifies every column added by 0027 survives an
// insert and is read back by every scan path (the INSERT … RETURNING, the
// single-photo read and the batch read), which is what catches a column list that
// drifted out of step with scanPhoto's argument order.
func TestIPTCColumns_roundTrip(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, iptcPhoto("iptc-1"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	assertIPTC(t, created)

	got, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	assertIPTC(t, got)

	listed, err := store.ListByUIDs(ctx, []string{created.UID})
	if err != nil {
		t.Fatalf("ListByUIDs: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListByUIDs returned %d photos, want 1", len(listed))
	}
	assertIPTC(t, listed[0])
}

// TestIPTCColumns_defaults verifies an existing-style row that knows none of the
// new metadata takes the column defaults rather than failing to insert: every new
// column is NOT NULL with a zero-value default, which is exactly the state every
// row already in the catalogue is in after the migration.
func TestIPTCColumns_defaults(t *testing.T) {
	store, _ := newStore(t)

	created, err := store.Create(t.Context(), photos.Photo{
		FileHash: "iptc-empty", FilePath: "2023/06/e.jpg", FileName: "e.jpg", FileMime: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Subject != "" || created.Keywords != "" || created.Artist != "" ||
		created.Copyright != "" || created.License != "" || created.Software != "" ||
		created.ColorProfile != "" || created.ImageCodec != "" || created.CameraSerial != "" ||
		created.OriginalName != "" || created.Projection != "" || created.Scan {
		t.Errorf("a photo with no IPTC metadata did not take the column defaults: %+v", created)
	}
}

// TestUpdateMetadata_editableIPTCFields verifies UpdateMetadata writes the six
// user-editable fields and leaves the machine-derived ones (software,
// color_profile, image_codec, camera_serial, original_name, projection) exactly as
// the ingest wrote them — they describe the file, and no edit may rewrite it.
func TestUpdateMetadata_editableIPTCFields(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, iptcPhoto("iptc-2"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := store.UpdateMetadata(ctx, created.UID, photos.MetadataUpdate{
		Title:     "Lake",
		Subject:   "Autumn at the lake",
		Keywords:  "lake,autumn",
		Artist:    "Petra Nová",
		Copyright: "© 2024 Petra Nová",
		License:   "All rights reserved",
		Scan:      false,
	})
	if err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	if updated.Subject != "Autumn at the lake" || updated.Keywords != "lake,autumn" ||
		updated.Artist != "Petra Nová" || updated.Copyright != "© 2024 Petra Nová" ||
		updated.License != "All rights reserved" || updated.Scan {
		t.Errorf("editable IPTC fields not written: %+v", updated)
	}
	if updated.Software != "Adobe Lightroom 12.4" || updated.ColorProfile != "Apple Wide Color Sharing Profile" ||
		updated.ImageCodec != "heic" || updated.CameraSerial != "SN-12345678" ||
		updated.OriginalName != "IMG_0042.HEIC" || updated.Projection != "equirectangular" {
		t.Errorf("an edit rewrote the machine-derived file metadata: %+v", updated)
	}

	// The persisted row, not just the RETURNING clause, carries the edit.
	got, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.Subject != "Autumn at the lake" || got.Scan {
		t.Errorf("persisted row does not carry the edit: %+v", got)
	}
}
