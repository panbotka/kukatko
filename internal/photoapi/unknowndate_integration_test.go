//go:build integration

package photoapi_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// TestPatchClearsTakenAtAndPreservesIt drives the "the date is unknown"
// declaration over HTTP: an explicit taken_at null clears the date, stamps the
// provenance, and puts the wrong date the photo carried away so the declaration
// can be undone — and stating a real date afterwards discards what was put away.
func TestPatchClearsTakenAtAndPreservesIt(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	scannedIn := time.Date(2011, 3, 8, 10, 15, 0, 0, time.UTC)
	photo := env.seedPhoto(t, photos.Photo{
		Title:            "Svatba",
		TakenAt:          &scannedIn,
		TakenAtSource:    "exif",
		TakenAtEstimated: true,
		TakenAtNote:      "podle babičky svatba",
	}, "scan.jpg", 5, 5, 200)
	url := base + "/api/v1/photos/" + photo.UID

	t.Run("clearing preserves the outgoing date", func(t *testing.T) {
		detail, _ := decodeDetail(t, editor, http.MethodPatch, url, []byte(`{"taken_at":null}`))
		if detail.TakenAt != nil {
			t.Errorf("taken_at = %s, want null", detail.TakenAt)
		}
		if detail.TakenAtSource != photos.TakenAtSourceUnknown {
			t.Errorf("taken_at_source = %q, want %q", detail.TakenAtSource, photos.TakenAtSourceUnknown)
		}
		if detail.TakenAtBeforeUnknown == nil || !detail.TakenAtBeforeUnknown.Equal(scannedIn) {
			t.Fatalf("taken_at_before_unknown = %v, want %s", detail.TakenAtBeforeUnknown, scannedIn)
		}
		// The dating note is orthogonal: "unknown, but grandma says it was a wedding"
		// is exactly the state this feature exists to allow.
		if !detail.TakenAtEstimated || detail.TakenAtNote != "podle babičky svatba" {
			t.Errorf("clearing disturbed the estimate pair: estimated=%v note=%q",
				detail.TakenAtEstimated, detail.TakenAtNote)
		}
	})

	t.Run("an unrelated edit leaves the preserved date alone", func(t *testing.T) {
		detail, _ := decodeDetail(t, editor, http.MethodPatch, url, []byte(`{"title":"Svatba 1974"}`))
		if detail.TakenAtBeforeUnknown == nil || !detail.TakenAtBeforeUnknown.Equal(scannedIn) {
			t.Fatalf("taken_at_before_unknown = %v, want %s", detail.TakenAtBeforeUnknown, scannedIn)
		}
	})

	t.Run("clearing again keeps what is already preserved", func(t *testing.T) {
		detail, _ := decodeDetail(t, editor, http.MethodPatch, url, []byte(`{"taken_at":null}`))
		if detail.TakenAtBeforeUnknown == nil || !detail.TakenAtBeforeUnknown.Equal(scannedIn) {
			t.Fatalf("taken_at_before_unknown = %v, want %s", detail.TakenAtBeforeUnknown, scannedIn)
		}
	})

	t.Run("stating a real date discards it", func(t *testing.T) {
		detail, _ := decodeDetail(t, editor, http.MethodPatch, url, []byte(`{"taken_at":"1974-06-14T00:00:00Z"}`))
		if detail.TakenAt == nil {
			t.Fatal("taken_at = null, want the stated date")
		}
		if detail.TakenAtBeforeUnknown != nil {
			t.Errorf("taken_at_before_unknown = %s, want none once a date is stated",
				detail.TakenAtBeforeUnknown)
		}
	})
}

// TestDatedFilterFindsUndatedPhotos proves the worklist `dated:no` covers both
// kinds of undated photo — the one whose date was declared unknown and the one
// that never had a date — and that a photo drops out of it the moment a date is
// stated.
func TestDatedFilterFindsUndatedPhotos(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	scannedIn := time.Date(2011, 3, 8, 10, 15, 0, 0, time.UTC)
	cleared := env.seedPhoto(t, photos.Photo{
		Title: "Scan", TakenAt: &scannedIn, TakenAtSource: "exif",
	}, "scan.jpg", 5, 5, 200)
	never := env.seedPhoto(t, photos.Photo{
		Title: "Print", TakenAtSource: photos.TakenAtSourceUnknown,
	}, "print.jpg", 5, 200, 5)
	dated := env.seedPhoto(t, photos.Photo{
		Title: "Holiday", TakenAt: &scannedIn, TakenAtSource: "exif",
	}, "holiday.jpg", 200, 5, 5)

	clearedURL := base + "/api/v1/photos/" + cleared.UID
	decodeDetail(t, editor, http.MethodPatch, clearedURL, []byte(`{"taken_at":null}`))

	undated := getList(t, editor, base, "q=dated%3Ano")
	if got := uids(undated.Photos); len(got) != 2 ||
		!slices.Contains(got, cleared.UID) || !slices.Contains(got, never.UID) {
		t.Fatalf("dated:no = %v, want the cleared (%s) and the never-dated (%s)",
			got, cleared.UID, never.UID)
	}

	withDate := getList(t, editor, base, "q=dated%3Ayes")
	if got := uids(withDate.Photos); len(got) != 1 || got[0] != dated.UID {
		t.Fatalf("dated:yes = %v, want [%s]", got, dated.UID)
	}

	// Stating a date takes the photo off the worklist.
	decodeDetail(t, editor, http.MethodPatch, clearedURL, []byte(`{"taken_at":"1974-06-14T00:00:00Z"}`))
	after := getList(t, editor, base, "q=dated%3Ano")
	if got := uids(after.Photos); len(got) != 1 || got[0] != never.UID {
		t.Fatalf("dated:no after re-dating = %v, want only [%s]", got, never.UID)
	}
}
