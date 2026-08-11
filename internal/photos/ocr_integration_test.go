//go:build integration

package photos_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They exercise migration 0058 end to end: the OCR
// columns, the rebuilt fts expression that folds ocr_text in at weight D, and the
// `text:` filter compiled by internal/photos.

// ocrPhoto builds a still Photo with a distinct file hash and the given title,
// leaving everything else at its zero value.
func ocrPhoto(hash, title string) photos.Photo {
	return photos.Photo{
		FileHash:  hash,
		FilePath:  "2024/06/" + hash + ".jpg",
		FileName:  hash + ".jpg",
		FileMime:  "image/jpeg",
		MediaType: photos.MediaImage,
		Title:     title,
	}
}

// listUIDsByQuery parses input through the query language, maps it onto
// ListParams the way the API layer does, and returns the resulting uids in the
// order the store yields them.
func listUIDsByQuery(t *testing.T, store *photos.Store, input string) []string {
	t.Helper()
	parsed := query.Parse(input)
	list, err := store.List(t.Context(), photos.ListParams{
		Search:       parsed.PlainText(),
		SearchNot:    parsed.NotTerms(),
		QueryFilters: parsed.Filters,
	})
	if err != nil {
		t.Fatalf("List(%q): %v", input, err)
	}
	uids := make([]string, len(list))
	for i, p := range list {
		uids[i] = p.UID
	}
	return uids
}

// TestOCR_foundByFreeTextAndRankedBelowTitle is the whole point of the feature:
// a photo whose only match is a sign in the picture turns up in a plain search,
// but a photo that genuinely carries the word in its title still comes first.
func TestOCR_foundByFreeTextAndRankedBelowTitle(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	titled, err := store.Create(ctx, ocrPhoto("ocr-title", "Veselice 2026"))
	if err != nil {
		t.Fatalf("create titled: %v", err)
	}
	signed, err := store.Create(ctx, ocrPhoto("ocr-sign", "Nedělní procházka"))
	if err != nil {
		t.Fatalf("create signed: %v", err)
	}
	unrelated, err := store.Create(ctx, ocrPhoto("ocr-none", "Kočka na zdi"))
	if err != nil {
		t.Fatalf("create unrelated: %v", err)
	}

	// Only the second photo has the word painted on a sign inside the frame.
	if err := store.SaveOCR(ctx, signed.UID, photos.OCR{
		Text: "VESELICE\nObecní úřad", Model: "PP-OCRv5_mobile",
	}); err != nil {
		t.Fatalf("SaveOCR(signed): %v", err)
	}
	// The third was looked at and had nothing to read — that must not make it
	// match, and must not leave it pending either.
	if err := store.SaveOCR(ctx, unrelated.UID, photos.OCR{Model: "PP-OCRv5_mobile"}); err != nil {
		t.Fatalf("SaveOCR(unrelated): %v", err)
	}

	got := searchUIDs(t, store, photos.ListParams{FullText: "veselice"})
	if len(got) != 2 {
		t.Fatalf("Search(veselice) = %v, want both the titled and the signed photo", got)
	}
	if got[0] != titled.UID {
		t.Errorf("first result = %s, want the titled photo %s — OCR text must rank below a real title",
			got[0], titled.UID)
	}
	if got[1] != signed.UID {
		t.Errorf("second result = %s, want the signed photo %s", got[1], signed.UID)
	}
}

// TestOCR_textFilterMatchesOnlyRecognisedText verifies `text:` reads ocr_text and
// nothing else: it finds the photo with the sign and ignores the one whose title
// happens to say the same thing.
func TestOCR_textFilterMatchesOnlyRecognisedText(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if _, err := store.Create(ctx, ocrPhoto("f-title", "Veselice 2026")); err != nil {
		t.Fatalf("create titled: %v", err)
	}
	signed, err := store.Create(ctx, ocrPhoto("f-sign", "Nedělní procházka"))
	if err != nil {
		t.Fatalf("create signed: %v", err)
	}
	if _, err := store.Create(ctx, ocrPhoto("f-none", "Kočka na zdi")); err != nil {
		t.Fatalf("create unrelated: %v", err)
	}
	if err := store.SaveOCR(ctx, signed.UID, photos.OCR{
		Text: "VESELICE\nObecní úřad", Model: "PP-OCRv5_mobile",
	}); err != nil {
		t.Fatalf("SaveOCR: %v", err)
	}

	got := listUIDsByQuery(t, store, "text:veselice")
	if len(got) != 1 || got[0] != signed.UID {
		t.Fatalf("text:veselice = %v, want only the signed photo [%s]", got, signed.UID)
	}
	if got := listUIDsByQuery(t, store, "text:kocka"); len(got) != 0 {
		t.Errorf("text:kocka = %v, want nothing — a photo with no recognised text must not match", got)
	}
}

// TestOCR_textFilterIgnoresAccents is why `text:` folds diacritics where its
// sibling text filters do not: the latin recogniser routinely returns a Czech
// word stripped of them ("Pouť" on a real sign comes back as "Pout"), so a
// correctly spelled query must still find it — and the reverse must hold too.
func TestOCR_textFilterIgnoresAccents(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	stripped, err := store.Create(ctx, ocrPhoto("a-stripped", "Léto"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	accented, err := store.Create(ctx, ocrPhoto("a-accented", "Zima"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.SaveOCR(ctx, stripped.UID, photos.OCR{Text: "Pout 2026", Model: "m"}); err != nil {
		t.Fatalf("SaveOCR(stripped): %v", err)
	}
	if err := store.SaveOCR(ctx, accented.UID, photos.OCR{Text: "Pouť 2026", Model: "m"}); err != nil {
		t.Fatalf("SaveOCR(accented): %v", err)
	}

	for _, q := range []string{"text:pouť", "text:pout"} {
		got := listUIDsByQuery(t, store, q)
		if len(got) != 2 {
			t.Errorf("%s = %v, want both the accented and the stripped reading", q, got)
		}
	}
}

// TestOCR_saveAndRead covers the storage contract: the reading round-trips, an
// empty one is a stored value rather than a missing row, a second reading
// replaces the first, and an unknown photo is an error.
func TestOCR_saveAndRead(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	photo, err := store.Create(ctx, ocrPhoto("rt-1", "Cedule"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := store.GetOCR(ctx, photo.UID); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Fatalf("GetOCR before any run = %v, want ErrPhotoNotFound", err)
	}
	if err := store.SaveOCR(ctx, photo.UID, photos.OCR{Text: "PRVNÍ", Model: "v1"}); err != nil {
		t.Fatalf("SaveOCR: %v", err)
	}
	got, err := store.GetOCR(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetOCR: %v", err)
	}
	if got.Text != "PRVNÍ" || got.Model != "v1" {
		t.Errorf("GetOCR = %+v, want PRVNÍ/v1", got)
	}

	// A forced re-run with a better model replaces the earlier reading.
	if err := store.SaveOCR(ctx, photo.UID, photos.OCR{Text: "DRUHÉ", Model: "v2"}); err != nil {
		t.Fatalf("SaveOCR again: %v", err)
	}
	if got, _ = store.GetOCR(ctx, photo.UID); got.Text != "DRUHÉ" || got.Model != "v2" {
		t.Errorf("GetOCR after re-run = %+v, want DRUHÉ/v2", got)
	}

	// An empty reading is stored, not skipped.
	if err := store.SaveOCR(ctx, photo.UID, photos.OCR{Model: "v2"}); err != nil {
		t.Fatalf("SaveOCR empty: %v", err)
	}
	if got, err = store.GetOCR(ctx, photo.UID); err != nil || got.Text != "" {
		t.Errorf("GetOCR after empty reading = %+v, %v; want an empty stored text", got, err)
	}

	if err := store.SaveOCR(ctx, "nope", photos.OCR{}); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("SaveOCR(unknown) = %v, want ErrPhotoNotFound", err)
	}
}

// TestOCR_backfillCandidates verifies the backfill converges: a photo is a
// candidate until it has been looked at — whatever the recogniser found — while
// videos and archived photos are never candidates at all.
func TestOCR_backfillCandidates(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	pending, err := store.Create(ctx, ocrPhoto("bf-pending", "A"))
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	empty, err := store.Create(ctx, ocrPhoto("bf-empty", "B"))
	if err != nil {
		t.Fatalf("create empty: %v", err)
	}
	clip := ocrPhoto("bf-video", "C")
	clip.MediaType = photos.MediaVideo
	clip.FileMime = "video/mp4"
	if _, err := store.Create(ctx, clip); err != nil {
		t.Fatalf("create video: %v", err)
	}
	archived, err := store.Create(ctx, ocrPhoto("bf-archived", "D"))
	if err != nil {
		t.Fatalf("create archived: %v", err)
	}
	if _, err := store.Archive(ctx, archived.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	uids, err := store.ListPhotosMissingOCR(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingOCR: %v", err)
	}
	if len(uids) != 2 {
		t.Fatalf("pending = %v, want exactly the two live stills", uids)
	}

	// Looking at a photo and finding nothing must take it out of the queue.
	if err := store.SaveOCR(ctx, empty.UID, photos.OCR{Model: "m"}); err != nil {
		t.Fatalf("SaveOCR(empty): %v", err)
	}
	uids, err = store.ListPhotosMissingOCR(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingOCR: %v", err)
	}
	if len(uids) != 1 || uids[0] != pending.UID {
		t.Fatalf("pending after an empty reading = %v, want only [%s]", uids, pending.UID)
	}

	// The forced full run re-reads every live still, recognised or not — and
	// still never a video or an archived photo.
	all, err := store.ListActiveImageUIDs(ctx)
	if err != nil {
		t.Fatalf("ListActiveImageUIDs: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListActiveImageUIDs = %v, want the two live stills", all)
	}
}
