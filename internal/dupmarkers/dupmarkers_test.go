package dupmarkers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/dupmarkers"
	"github.com/panbotka/kukatko/internal/feedback"
)

// row builds a valid face marker row of the named subject on the named photo. The
// x coordinate doubles as the box's identity in the assertions, since the markers
// of a group come back ordered by it.
func row(markerUID, photoUID, subjectUID, subjectName string, x float64) dupmarkers.MarkerRow {
	return dupmarkers.MarkerRow{
		MarkerUID:   markerUID,
		PhotoUID:    photoUID,
		SubjectUID:  subjectUID,
		SubjectName: subjectName,
		Type:        dupmarkers.MarkerTypeFace,
		X:           x,
		Y:           0.2,
		W:           0.1,
		H:           0.1,
		Width:       4000,
		Height:      3000,
	}
}

// fakeMarkers is a MarkerSource returning canned rows or an error.
type fakeMarkers struct {
	rows []dupmarkers.MarkerRow
	err  error
}

// ListRepeatedMarkers implements dupmarkers.MarkerSource.
func (f fakeMarkers) ListRepeatedMarkers(context.Context) ([]dupmarkers.MarkerRow, error) {
	return f.rows, f.err
}

// fakeDismissals is a DismissalSource returning canned keys or an error.
type fakeDismissals struct {
	keys []feedback.DuplicateMarkerDismissalKey
	err  error
}

// DismissedDuplicateMarkerGroups implements dupmarkers.DismissalSource.
func (f fakeDismissals) DismissedDuplicateMarkerGroups(
	context.Context,
) ([]feedback.DuplicateMarkerDismissalKey, error) {
	return f.keys, f.err
}

// markerUIDs lists a group's marker uids in the order they were returned.
func markerUIDs(g dupmarkers.Group) []string {
	out := make([]string, 0, len(g.Markers))
	for _, m := range g.Markers {
		out = append(out, m.UID)
	}
	return out
}

func TestGroupMarkers_groupsBySubjectWithinOnePhoto(t *testing.T) {
	t.Parallel()

	// One photo: Marie three times (the mistake) and Jan once (the normal case).
	rows := []dupmarkers.MarkerRow{
		row("m1", "p1", "s-marie", "Marie", 0.59),
		row("m2", "p1", "s-marie", "Marie", 0.70),
		row("m3", "p1", "s-marie", "Marie", 0.79),
		row("m4", "p1", "s-jan", "Jan", 0.10),
	}

	groups := dupmarkers.GroupMarkers(rows, nil)

	if len(groups) != 1 {
		t.Fatalf("GroupMarkers() returned %d groups, want 1", len(groups))
	}
	got := groups[0]
	if got.SubjectUID != "s-marie" || got.PhotoUID != "p1" {
		t.Errorf("group = (%s, %s), want (p1, s-marie)", got.PhotoUID, got.SubjectUID)
	}
	if len(got.Markers) != 3 {
		t.Fatalf("group has %d markers, want 3", len(got.Markers))
	}
	// Left to right, so the numbering drawn over the preview reads in order.
	want := []string{"m1", "m2", "m3"}
	for i, uid := range markerUIDs(got) {
		if uid != want[i] {
			t.Errorf("marker[%d] = %s, want %s", i, uid, want[i])
		}
	}
	if got.SubjectName != "Marie" || got.Width != 4000 || got.Height != 3000 {
		t.Errorf("group carries %+v, want Marie on a 4000x3000 frame", got)
	}
}

func TestGroupMarkers_invalidMarkerDoesNotCount(t *testing.T) {
	t.Parallel()

	// Marie twice, but one of the two boxes was flagged as holding no face: the
	// group falls to one marker and stops being a finding at all.
	invalid := row("m2", "p1", "s-marie", "Marie", 0.70)
	invalid.Invalid = true
	rows := []dupmarkers.MarkerRow{
		row("m1", "p1", "s-marie", "Marie", 0.59),
		invalid,
	}

	if groups := dupmarkers.GroupMarkers(rows, nil); len(groups) != 0 {
		t.Fatalf("GroupMarkers() returned %d groups, want 0 (the group fell to one marker)", len(groups))
	}
}

func TestGroupMarkers_invalidMarkerLeavesLargerGroupStanding(t *testing.T) {
	t.Parallel()

	// The same rule from the other side: three markers minus one invalid is still
	// two, which is still a mistake.
	invalid := row("m3", "p1", "s-marie", "Marie", 0.79)
	invalid.Invalid = true
	rows := []dupmarkers.MarkerRow{
		row("m1", "p1", "s-marie", "Marie", 0.59),
		row("m2", "p1", "s-marie", "Marie", 0.70),
		invalid,
	}

	groups := dupmarkers.GroupMarkers(rows, nil)
	if len(groups) != 1 {
		t.Fatalf("GroupMarkers() returned %d groups, want 1", len(groups))
	}
	if got := markerUIDs(groups[0]); len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Errorf("markers = %v, want [m1 m2]", got)
	}
}

func TestGroupMarkers_ignoresLabelMarkersAndTheNamelessSubject(t *testing.T) {
	t.Parallel()

	label1 := row("l1", "p1", "s-marie", "Marie", 0.10)
	label1.Type = "label"
	label2 := row("l2", "p1", "s-marie", "Marie", 0.20)
	label2.Type = "label"
	nameless1 := row("n1", "p2", "s-nameless", "", 0.10)
	nameless2 := row("n2", "p2", "s-nameless", "", 0.20)

	rows := []dupmarkers.MarkerRow{label1, label2, nameless1, nameless2}

	if groups := dupmarkers.GroupMarkers(rows, nil); len(groups) != 0 {
		t.Fatalf("GroupMarkers() returned %d groups, want 0", len(groups))
	}
}

func TestGroupMarkers_separatePhotosAreSeparateGroups(t *testing.T) {
	t.Parallel()

	rows := []dupmarkers.MarkerRow{
		row("a1", "p1", "s-marie", "Marie", 0.10),
		row("a2", "p1", "s-marie", "Marie", 0.20),
		row("b1", "p2", "s-marie", "Marie", 0.10),
		row("b2", "p2", "s-marie", "Marie", 0.20),
	}

	groups := dupmarkers.GroupMarkers(rows, nil)
	if len(groups) != 2 {
		t.Fatalf("GroupMarkers() returned %d groups, want 2", len(groups))
	}
	if groups[0].PhotoUID == groups[1].PhotoUID {
		t.Errorf("both groups are on %s, want one per photo", groups[0].PhotoUID)
	}
}

func TestGroupMarkers_ordersWorstFirstThenByName(t *testing.T) {
	t.Parallel()

	rows := []dupmarkers.MarkerRow{
		// Zdena: two markers.
		row("z1", "p1", "s-zdena", "Zdena", 0.10),
		row("z2", "p1", "s-zdena", "Zdena", 0.20),
		// Marie: three markers — the worse finding, so it must lead.
		row("m1", "p2", "s-marie", "Marie", 0.10),
		row("m2", "p2", "s-marie", "Marie", 0.20),
		row("m3", "p2", "s-marie", "Marie", 0.30),
		// Adam: two markers, so he sorts with Zdena and ahead of her by name.
		row("a1", "p3", "s-adam", "Adam", 0.10),
		row("a2", "p3", "s-adam", "Adam", 0.20),
	}

	groups := dupmarkers.GroupMarkers(rows, nil)

	want := []string{"Marie", "Adam", "Zdena"}
	if len(groups) != len(want) {
		t.Fatalf("GroupMarkers() returned %d groups, want %d", len(groups), len(want))
	}
	for i, name := range want {
		if groups[i].SubjectName != name {
			t.Errorf("group[%d] = %s, want %s", i, groups[i].SubjectName, name)
		}
	}
}

func TestGroupMarkers_dropsDismissedGroups(t *testing.T) {
	t.Parallel()

	rows := []dupmarkers.MarkerRow{
		row("a1", "p1", "s-marie", "Marie", 0.10),
		row("a2", "p1", "s-marie", "Marie", 0.20),
		row("b1", "p2", "s-marie", "Marie", 0.10),
		row("b2", "p2", "s-marie", "Marie", 0.20),
	}
	dismissed := []feedback.DuplicateMarkerDismissalKey{{PhotoUID: "p1", SubjectUID: "s-marie"}}

	groups := dupmarkers.GroupMarkers(rows, dismissed)

	if len(groups) != 1 {
		t.Fatalf("GroupMarkers() returned %d groups, want 1", len(groups))
	}
	if groups[0].PhotoUID != "p2" {
		t.Errorf("surviving group is on %s, want p2 (p1 was dismissed)", groups[0].PhotoUID)
	}
}

func TestGroupMarkers_emptyInput(t *testing.T) {
	t.Parallel()

	groups := dupmarkers.GroupMarkers(nil, nil)
	if groups == nil {
		t.Fatal("GroupMarkers(nil, nil) = nil, want an empty non-nil slice")
	}
	if len(groups) != 0 {
		t.Errorf("GroupMarkers(nil, nil) returned %d groups, want 0", len(groups))
	}
}

func TestFindGroups_paginates(t *testing.T) {
	t.Parallel()

	rows := []dupmarkers.MarkerRow{
		row("a1", "p1", "s-adam", "Adam", 0.10),
		row("a2", "p1", "s-adam", "Adam", 0.20),
		row("b1", "p2", "s-bara", "Bara", 0.10),
		row("b2", "p2", "s-bara", "Bara", 0.20),
		row("c1", "p3", "s-cyril", "Cyril", 0.10),
		row("c2", "p3", "s-cyril", "Cyril", 0.20),
	}
	svc := dupmarkers.New(dupmarkers.Config{
		Markers:    fakeMarkers{rows: rows},
		Dismissals: fakeDismissals{},
	})

	first, err := svc.FindGroups(context.Background(), 2, 0)
	if err != nil {
		t.Fatalf("FindGroups() error = %v", err)
	}
	if first.Total != 3 || len(first.Groups) != 2 {
		t.Fatalf("first page: total=%d groups=%d, want 3 and 2", first.Total, len(first.Groups))
	}
	if first.NextOffset == nil || *first.NextOffset != 2 {
		t.Fatalf("first page next_offset = %v, want 2", first.NextOffset)
	}

	last, err := svc.FindGroups(context.Background(), 2, *first.NextOffset)
	if err != nil {
		t.Fatalf("FindGroups() error = %v", err)
	}
	if len(last.Groups) != 1 || last.Groups[0].SubjectName != "Cyril" {
		t.Fatalf("last page = %+v, want the single Cyril group", last.Groups)
	}
	if last.NextOffset != nil {
		t.Errorf("last page next_offset = %v, want nil", *last.NextOffset)
	}
}

func TestFindGroups_offsetPastTheEndIsAnEmptyPage(t *testing.T) {
	t.Parallel()

	svc := dupmarkers.New(dupmarkers.Config{
		Markers: fakeMarkers{rows: []dupmarkers.MarkerRow{
			row("a1", "p1", "s-adam", "Adam", 0.10),
			row("a2", "p1", "s-adam", "Adam", 0.20),
		}},
		Dismissals: fakeDismissals{},
	})

	res, err := svc.FindGroups(context.Background(), 10, 99)
	if err != nil {
		t.Fatalf("FindGroups() error = %v", err)
	}
	if res.Total != 1 {
		t.Errorf("total = %d, want 1", res.Total)
	}
	if len(res.Groups) != 0 || res.Groups == nil {
		t.Errorf("groups = %v, want an empty non-nil slice", res.Groups)
	}
}

func TestFindGroups_clampsPaging(t *testing.T) {
	t.Parallel()

	svc := dupmarkers.New(dupmarkers.Config{
		Markers:    fakeMarkers{},
		Dismissals: fakeDismissals{},
	})

	res, err := svc.FindGroups(context.Background(), 0, -5)
	if err != nil {
		t.Fatalf("FindGroups() error = %v", err)
	}
	if res.Limit <= 0 {
		t.Errorf("limit = %d, want the default page size", res.Limit)
	}
	if res.Offset != 0 {
		t.Errorf("offset = %d, want 0 for a negative request", res.Offset)
	}

	capped, err := svc.FindGroups(context.Background(), 100000, 0)
	if err != nil {
		t.Fatalf("FindGroups() error = %v", err)
	}
	if capped.Limit >= 100000 {
		t.Errorf("limit = %d, want it capped", capped.Limit)
	}
}

func TestFindGroups_propagatesSourceErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	markerErr := dupmarkers.New(dupmarkers.Config{
		Markers:    fakeMarkers{err: boom},
		Dismissals: fakeDismissals{},
	})
	if _, err := markerErr.FindGroups(context.Background(), 0, 0); !errors.Is(err, boom) {
		t.Errorf("marker source error = %v, want it wrapped", err)
	}

	dismissalErr := dupmarkers.New(dupmarkers.Config{
		Markers:    fakeMarkers{},
		Dismissals: fakeDismissals{err: boom},
	})
	if _, err := dismissalErr.FindGroups(context.Background(), 0, 0); !errors.Is(err, boom) {
		t.Errorf("dismissal source error = %v, want it wrapped", err)
	}
}
