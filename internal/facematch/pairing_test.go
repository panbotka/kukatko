package facematch

import (
	"context"
	"maps"
	"testing"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// The production fixture this whole feature exists for: photo
// ph9a09ujh1p4qfalm4eigm4h7a, where faces 6 and 7 both claimed marker
// mt8kfg7wih6tp99t and "Tomáš Kozák" was therefore rendered twice. Face 7 overlaps
// the marker by ~0.80 and face 6 by ~0.16 — both clear the 0.1 threshold, which is
// exactly why per-face matching handed the marker to each of them in turn.
const (
	tomasMarker   = "mt8kfg7wih6tp99t"
	unnamedMarker = "mt8kfg7wbumuzse5"
)

// tomasPhoto returns the two detected faces and two markers of the production
// fixture, with the faces' cached links already pointing at the duplicated marker
// the way the database holds them.
func tomasPhoto(subjectUID string) ([]vectors.Face, []people.Marker) {
	marker := tomasMarker
	faces := []vectors.Face{
		{
			PhotoUID: "ph9a09ujh1p4qfalm4eigm4h7a", FaceIndex: 6, DetScore: 0.663,
			BBox: [4]float64{0.3489, 0.2873, 0.0464, 0.1067}, MarkerUID: &marker, SubjectUID: &subjectUID,
		},
		{
			PhotoUID: "ph9a09ujh1p4qfalm4eigm4h7a", FaceIndex: 7, DetScore: 0.588,
			BBox: [4]float64{0.2965, 0.3217, 0.0755, 0.1227}, MarkerUID: &marker, SubjectUID: &subjectUID,
		},
	}
	markers := []people.Marker{
		{
			UID: tomasMarker, PhotoUID: "ph9a09ujh1p4qfalm4eigm4h7a", Type: people.MarkerFace,
			X: 0.2889, Y: 0.3241, W: 0.0917, H: 0.1222, Reviewed: true, SubjectUID: &subjectUID,
		},
		{
			UID: unnamedMarker, PhotoUID: "ph9a09ujh1p4qfalm4eigm4h7a", Type: people.MarkerFace,
			X: 0.4019, Y: 0.3121, W: 0.0430, H: 0.0783,
		},
	}
	return faces, markers
}

// TestMatchMarkers_oneMarkerOneFace checks two faces competing for a single marker
// leave it with the closer one only — the production duplicate.
func TestMatchMarkers_oneMarkerOneFace(t *testing.T) {
	t.Parallel()
	faces, markers := tomasPhoto("su_tomas")

	matched := matchMarkers(faces, markers, DefaultIoUThreshold)

	if got, ok := matched[7]; !ok || got.MarkerUID != tomasMarker {
		t.Errorf("face 7 matched %+v, want %s (the higher IoU)", got, tomasMarker)
	}
	if got, ok := matched[6]; ok {
		t.Errorf("face 6 matched %+v, want no marker (face 7 claimed it)", got)
	}
	if got := matched.claims()[tomasMarker]; got != 7 {
		t.Errorf("marker claimed by face %d, want 7", got)
	}
}

// TestMatchMarkers_twoFacesTwoMarkers checks the pairing is exclusive rather than
// per-face greedy: both faces overlap marker mkBig most, so a per-face "best
// marker" search sends both there and leaves mkSmall unmatched. The exclusive
// pairing must give each face its own marker.
func TestMatchMarkers_twoFacesTwoMarkers(t *testing.T) {
	t.Parallel()
	// mkBig straddles both faces and is each one's closest marker; mkSmall only
	// reaches the second face, and less well than mkBig does.
	faces := []vectors.Face{
		{FaceIndex: 0, BBox: [4]float64{0.10, 0.10, 0.20, 0.20}},
		{FaceIndex: 1, BBox: [4]float64{0.28, 0.10, 0.20, 0.20}},
	}
	markers := []people.Marker{
		{UID: "mkBig", Type: people.MarkerFace, X: 0.15, Y: 0.10, W: 0.25, H: 0.20},
		{UID: "mkSmall", Type: people.MarkerFace, X: 0.40, Y: 0.10, W: 0.20, H: 0.20},
	}
	// Sanity: a per-face best-marker search really would send both faces to mkBig.
	if IoU(faces[1].BBox, markerBox(markers[0])) <= IoU(faces[1].BBox, markerBox(markers[1])) {
		t.Fatalf("fixture is not the greedy trap: face 1 already prefers mkSmall")
	}

	matched := matchMarkers(faces, markers, DefaultIoUThreshold)

	if got := matched[0].MarkerUID; got != "mkBig" {
		t.Errorf("face 0 matched %q, want mkBig", got)
	}
	if got := matched[1].MarkerUID; got != "mkSmall" {
		t.Errorf("face 1 matched %q, want mkSmall (mkBig is taken)", got)
	}
}

// TestMatchMarkers_stable checks repeated matching over the same input yields the
// same pairing. An unstable choice would have consecutive reads of one photo
// rewrite the cached links back and forth.
func TestMatchMarkers_stable(t *testing.T) {
	t.Parallel()
	// Two faces and two markers with identical geometry, so every pair scores the
	// same IoU and only the tie-break decides.
	box := [4]float64{0.2, 0.2, 0.3, 0.3}
	faces := []vectors.Face{{FaceIndex: 3, BBox: box}, {FaceIndex: 1, BBox: box}}
	markers := []people.Marker{
		{UID: "mkZ", Type: people.MarkerFace, X: box[0], Y: box[1], W: box[2], H: box[3]},
		{UID: "mkA", Type: people.MarkerFace, X: box[0], Y: box[1], W: box[2], H: box[3]},
	}

	first := matchMarkers(faces, markers, DefaultIoUThreshold)
	for i := range 5 {
		again := matchMarkers(faces, markers, DefaultIoUThreshold)
		if !maps.Equal(first, again) {
			t.Fatalf("run %d matched %v, want the same pairing as %v", i, again, first)
		}
	}
	// The tie-break is documented: lowest face index first, then lowest marker uid.
	if got := first[1].MarkerUID; got != "mkA" {
		t.Errorf("face 1 matched %q, want mkA (lowest index takes the lowest uid)", got)
	}
	if got := first[3].MarkerUID; got != "mkZ" {
		t.Errorf("face 3 matched %q, want mkZ", got)
	}
}

// TestMatchMarkers_skipsUnusableMarkers checks markers that are not face regions,
// invalid ones, and overlaps below the threshold never enter the pairing.
func TestMatchMarkers_skipsUnusableMarkers(t *testing.T) {
	t.Parallel()
	box := [4]float64{0.2, 0.2, 0.3, 0.3}
	faces := []vectors.Face{{FaceIndex: 0, BBox: box}}
	tests := []struct {
		name   string
		marker people.Marker
	}{
		{"label marker", people.Marker{UID: "mk1", Type: people.MarkerLabel, X: 0.2, Y: 0.2, W: 0.3, H: 0.3}},
		{"invalid marker", people.Marker{UID: "mk1", Type: people.MarkerFace, X: 0.2, Y: 0.2, W: 0.3, H: 0.3, Invalid: true}},
		{"below threshold", people.Marker{UID: "mk1", Type: people.MarkerFace, X: 0.45, Y: 0.45, W: 0.3, H: 0.3}},
		{"no overlap", people.Marker{UID: "mk1", Type: people.MarkerFace, X: 0.8, Y: 0.8, W: 0.1, H: 0.1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if matched := matchMarkers(faces, []people.Marker{tt.marker}, DefaultIoUThreshold); len(matched) != 0 {
				t.Errorf("matched %v, want no pairing", matched)
			}
		})
	}
}

// TestSurplusFaces_unclaimedMarkerKeepsOneLink checks a marker no face overlaps
// any more still ends up on exactly one face, so the repair converges instead of
// reporting the same duplicate forever.
func TestSurplusFaces_unclaimedMarkerKeepsOneLink(t *testing.T) {
	t.Parallel()
	stale := "mkStale"
	faces := []vectors.Face{
		{FaceIndex: 2, BBox: [4]float64{0.1, 0.1, 0.1, 0.1}, MarkerUID: &stale},
		{FaceIndex: 5, BBox: [4]float64{0.6, 0.6, 0.1, 0.1}, MarkerUID: &stale},
	}
	// The marker sits nowhere near either face, so nobody claims it.
	markers := []people.Marker{{UID: stale, Type: people.MarkerFace, X: 0.9, Y: 0.9, W: 0.05, H: 0.05}}

	surplus := surplusFaces(faces, matchMarkers(faces, markers, DefaultIoUThreshold))

	if surplus[2] {
		t.Error("face 2 flagged surplus, want the lowest index to keep the link")
	}
	if !surplus[5] {
		t.Error("face 5 not flagged surplus, want its duplicate link cleared")
	}
}

// TestSurplusFaces_singleLinkKept checks a face holding the only link on its
// marker is never flagged, whatever the geometry — the repair must not touch a
// marker that is not duplicated.
func TestSurplusFaces_singleLinkKept(t *testing.T) {
	t.Parallel()
	own := "mkOwn"
	faces := []vectors.Face{
		{FaceIndex: 0, BBox: [4]float64{0.1, 0.1, 0.1, 0.1}, MarkerUID: &own},
		{FaceIndex: 1, BBox: [4]float64{0.6, 0.6, 0.1, 0.1}},
	}
	markers := []people.Marker{{UID: own, Type: people.MarkerFace, X: 0.9, Y: 0.9, W: 0.05, H: 0.05}}

	if surplus := surplusFaces(faces, matchMarkers(faces, markers, DefaultIoUThreshold)); len(surplus) != 0 {
		t.Errorf("surplus = %v, want none (no marker is duplicated)", surplus)
	}
}

// TestPhotoFaces_exclusiveMarkerPerFace runs the production fixture through the
// whole read path: the marker's person is reported once, the loser becomes an
// ordinary unnamed face offered for assignment, and its stale cached link is
// cleared rather than left for the rest of the app to read.
func TestPhotoFaces_exclusiveMarkerPerFace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	subjUID := "su_tomas"
	faces, markers := tomasPhoto(subjUID)
	ff := &fakeFaces{list: faces}
	pe := &fakePeople{
		markers:       markers,
		subjectsByUID: map[string]people.Subject{subjUID: {UID: subjUID, Name: "Tomáš Kozák"}},
	}
	svc := newService(&fakePhotos{}, ff, pe)

	resp, err := svc.PhotoFaces(ctx, "ph9a09ujh1p4qfalm4eigm4h7a")
	if err != nil {
		t.Fatalf("PhotoFaces: %v", err)
	}

	named := 0
	for _, view := range resp.Faces {
		if view.SubjectUID == subjUID {
			named++
		}
	}
	if named != 1 {
		t.Errorf("%d views name Tomáš Kozák, want exactly 1 (%+v)", named, resp.Faces)
	}
	byIndex := make(map[int]FaceView, len(resp.Faces))
	for _, view := range resp.Faces {
		byIndex[view.FaceIndex] = view
	}
	if got := byIndex[7]; got.MarkerUID != tomasMarker || got.Action != ActionAlreadyDone {
		t.Errorf("face 7 = %+v, want already_done on %s", got, tomasMarker)
	}
	if got := byIndex[6]; got.MarkerUID != "" || got.SubjectUID != "" || got.Action != ActionCreateMarker {
		t.Errorf("face 6 = %+v, want an unnamed face offered a new marker", got)
	}
	// The unnamed marker matched no face, so it is still rendered for the editor.
	if _, ok := byIndex[-1]; !ok {
		t.Errorf("unmatched marker %s not rendered: %+v", unnamedMarker, resp.Faces)
	}
	// Face 6's stale link must be cleared, not merely ignored.
	if ff.lastFaceIdx != 6 || ff.lastMarker != "" || ff.lastSubject != "" {
		t.Errorf("last cache write = face %d marker %q subject %q, want face 6 cleared",
			ff.lastFaceIdx, ff.lastMarker, ff.lastSubject)
	}
}

// TestClearSurplusLinks checks the maintenance entry point clears exactly the
// surplus link and leaves the winning face alone.
func TestClearSurplusLinks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	faces, markers := tomasPhoto("su_tomas")
	ff := &fakeFaces{list: faces}
	svc := newService(&fakePhotos{}, ff, &fakePeople{markers: markers})

	cleared, err := svc.ClearSurplusLinks(ctx, "ph9a09ujh1p4qfalm4eigm4h7a")
	if err != nil {
		t.Fatalf("ClearSurplusLinks: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1", cleared)
	}
	if ff.updates != 1 || ff.lastFaceIdx != 6 || ff.lastMarker != "" || ff.lastSubject != "" {
		t.Errorf("writes=%d last = face %d marker %q subject %q, want only face 6 cleared",
			ff.updates, ff.lastFaceIdx, ff.lastMarker, ff.lastSubject)
	}

	// Re-running over the repaired rows is a no-op.
	faces[0].MarkerUID, faces[0].SubjectUID = nil, nil
	ff.updates = 0
	if cleared, err = svc.ClearSurplusLinks(ctx, "ph9a09ujh1p4qfalm4eigm4h7a"); err != nil {
		t.Fatalf("ClearSurplusLinks re-run: %v", err)
	}
	if cleared != 0 || ff.updates != 0 {
		t.Errorf("re-run cleared %d rows with %d writes, want 0/0", cleared, ff.updates)
	}
}
