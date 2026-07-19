package photoapi

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
)

// TestMetadataChanges_recordsOnlyChangedFields verifies the photo edit diff
// records old→new for a field whose value changed, omits the fields that did not,
// and stamps the result under the audit "changes" key.
func TestMetadataChanges_recordsOnlyChangedFields(t *testing.T) {
	t.Parallel()

	before := photos.Photo{Title: "stary popisek", Description: "beze změny", Scan: false}
	after := photos.MetadataUpdate{Title: "novy popisek", Description: "beze změny", Scan: false}

	diff := metadataChanges(before, after).Map()
	change, ok := diff["title"].(audit.Change)
	if !ok {
		t.Fatalf("title change type = %T, want audit.Change", diff["title"])
	}
	if change.Old != "stary popisek" || change.New != "novy popisek" {
		t.Errorf("title change = %+v, want stary→novy", change)
	}
	if _, ok := diff["description"]; ok {
		t.Errorf("unchanged description present in diff: %v", diff)
	}
	if _, ok := diff["scan"]; ok {
		t.Errorf("unchanged scan present in diff: %v", diff)
	}
}

// TestMetadataChanges_pointerAndBoolFields verifies the diff handles the pointer
// (taken_at, lat) and boolean (scan) fields: a cleared coordinate records a nil
// new value, a moved date records both instants, and a flipped flag is recorded.
func TestMetadataChanges_pointerAndBoolFields(t *testing.T) {
	t.Parallel()

	oldTaken := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)
	newTaken := time.Date(2021, 6, 2, 0, 0, 0, 0, time.UTC)
	before := photos.Photo{TakenAt: &oldTaken, Lat: new(50.0), Lng: new(14.0), Scan: false}
	after := photos.MetadataUpdate{TakenAt: &newTaken, Lat: nil, Lng: new(14.0), Scan: true}

	diff := metadataChanges(before, after).Map()
	if len(diff) != 3 {
		t.Fatalf("diff has %d entries, want 3 (taken_at, lat, scan): %v", len(diff), diff)
	}
	// A cleared coordinate is a typed nil *float64 in the interface (not untyped
	// nil), which json.Marshal renders as null; assert on the pointer itself.
	if lat := diff["lat"].(audit.Change); lat.New.(*float64) != nil {
		t.Errorf("cleared lat new = %v, want a nil *float64", lat.New)
	}
	if scan := diff["scan"].(audit.Change); scan.Old != false || scan.New != true {
		t.Errorf("scan change = %+v, want false→true", scan)
	}
	if _, ok := diff["lng"]; ok {
		t.Errorf("unchanged lng present in diff: %v", diff)
	}
}

// TestMetadataChanges_noopStampsNothing verifies an edit that changes no field
// leaves the details map without a "changes" key.
func TestMetadataChanges_noopStampsNothing(t *testing.T) {
	t.Parallel()

	before := photos.Photo{Title: "same", Notes: "same"}
	after := photos.MetadataUpdate{Title: "same", Notes: "same"}

	details := map[string]any{"fields": []string{"title"}}
	metadataChanges(before, after).StampInto(details)
	if _, ok := details[audit.ChangesKey]; ok {
		t.Errorf("no-op edit stamped a changes key: %v", details)
	}
}
