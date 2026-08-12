//go:build integration

package review_test

// The three checks over work the machine already did, driven end to end over a
// real database: the place check on an estimated location, the duplicate check
// on a near-duplicate pair, and the outlier check on a face assigned to the
// wrong person.
//
// Each test asserts the *whole* effect of one answer, not just the intended one:
// the write that should have happened, and that the neighbouring write paths
// were left alone. A duplicate "yes" that quietly archived a photo, or an outlier
// "no" that deleted a marker instead of detaching it, would pass a test that only
// checked the thing it was looking for — and both are exactly the failures this
// feature must never have, because the game is played fast and undo is one step
// deep.

import (
	"context"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/geoestimate"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/review"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// checksService composes a review service with all five question kinds wired,
// mirroring cmd/kukatko's buildReviewAPI. The face and label sides are wired too
// (with nothing seeded for them in these tests) so the merge is exercised as it
// actually runs rather than in a one-kind special case.
func (h *reviewHarness) checksService() *review.Service {
	candSvc := candidates.New(candidates.Config{
		Faces: h.vectors, People: h.people, Feedback: h.feedback, Photos: h.photos,
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 1000, MinFacePx: 32, Concurrency: 2, MinFaceRel: 0.02,
	})
	expandSvc := expand.New(expand.Config{
		Vectors: h.vectors, Organize: h.organize, Feedback: h.feedback, Photos: h.photos,
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 200, Concurrency: 2,
	})
	return review.New(review.Config{
		Sweeper:  sweep.New(sweep.Config{Subjects: h.people, Finder: candSvc, Concurrency: 2}),
		Expander: expandSvc,
		Organize: h.organize,
		Faces:    h.vectors,
		Feedback: h.feedback,
		Assigner: facematch.New(facematch.Config{Photos: h.photos, Faces: h.vectors, People: h.people}),
		Places: geoestimate.NewReviewer(geoestimate.ReviewConfig{
			Catalogue: h.photos, Places: places.NewStore(h.db.Pool()),
		}),
		Duplicates: duplicates.New(duplicates.Config{
			Photos: h.photos, Phashes: h.photos, Embeddings: h.vectors, Feedback: h.feedback,
			PhashMaxDiff: 8, EmbeddingMaxDist: 0.1,
		}),
		Outliers: outliers.New(outliers.Config{
			Faces: h.vectors, People: h.people, Feedback: h.feedback,
		}),
		Subjects:   h.people,
		Photos:     h.photos,
		KindShares: allKinds(),
		BandMin:    0.45, BandMax: 0.75,
	})
}

// estimated inserts a photo carrying an estimated location plus its cached
// place, i.e. exactly the state the geo-estimate backfill leaves behind.
func (h *reviewHarness) estimated(t *testing.T, hash, city string, lat, lng float64) string {
	t.Helper()
	ctx := context.Background()
	created, err := h.photos.Create(ctx, photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileWidth: 1000, FileHeight: 800, FileOrientation: 1,
		Lat: &lat, Lng: &lng, LocationSource: photos.LocationSourceEstimate,
	})
	if err != nil {
		t.Fatalf("creating estimated photo %s: %v", hash, err)
	}
	if _, err := places.NewStore(h.db.Pool()).SavePlace(ctx, places.Place{
		PhotoUID: created.UID, Country: "cz", City: city, PlaceName: "",
		Lat: &lat, Lng: &lng,
	}); err != nil {
		t.Fatalf("SavePlace(%s): %v", hash, err)
	}
	return created.UID
}

// questionOf returns the first queued question of the given kind, failing when
// the queue holds none.
func questionOf(t *testing.T, svc *review.Service, user string, kind review.Kind) review.Question {
	t.Helper()
	res, err := svc.Queue(context.Background(), user, review.SourceBoth, 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	for _, q := range res.Questions {
		if q.Kind == kind {
			return q
		}
	}
	t.Fatalf("no %s question in a queue of %d (reason %q)", kind, len(res.Questions), res.Reason)
	return review.Question{}
}

// reviewMeta is the audit metadata an answer is stamped with.
var reviewMeta = audit.Meta{ActorUID: ""}

func TestReviewPlaceCheck_acceptKeepsTheLocationAndClearsTheEstimateDB(t *testing.T) {
	h := newReviewHarness(t)
	uid := h.estimated(t, "guessed", "Brno", 49.2, 16.6)
	svc := h.checksService()

	question := questionOf(t, svc, "tester", review.KindPlace)
	if question.Place == nil || question.Place.Name != "Brno" {
		t.Fatalf("place question = %+v, want it to name Brno", question.Place)
	}
	if question.Photo.UID != uid {
		t.Fatalf("place question photo = %q, want %q", question.Photo.UID, uid)
	}

	res, err := svc.Answer(context.Background(), "tester", question.ID, review.AnswerYes, reviewMeta)
	if err != nil {
		t.Fatalf("Answer(yes): %v", err)
	}
	if res.Result != "confirmed" {
		t.Errorf("result = %q, want confirmed", res.Result)
	}

	after, err := h.photos.GetByUID(context.Background(), uid)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if after.Lat == nil || after.Lng == nil {
		t.Fatalf("coordinates = %v/%v, want them kept", after.Lat, after.Lng)
	}
	if *after.Lat != 49.2 || *after.Lng != 16.6 {
		t.Errorf("coordinates = %v/%v, want them unchanged", *after.Lat, *after.Lng)
	}
	if after.LocationSource != photos.LocationSourceManual {
		t.Errorf("location_source = %q, want %q", after.LocationSource, photos.LocationSourceManual)
	}
	// The write path is a full-record replace, so the neighbouring metadata is
	// the thing most at risk from it.
	if after.ArchivedAt != nil || after.HiddenFromLibrary {
		t.Errorf("photo archived=%v hidden=%v, want neither touched", after.ArchivedAt, after.HiddenFromLibrary)
	}
	// The question is settled: the photo is no longer an estimate, so it drops
	// out of the pending set instead of being asked about again.
	if pending := countEstimates(t, h); pending != 0 {
		t.Errorf("pending estimates = %d, want 0", pending)
	}
}

func TestReviewPlaceCheck_rejectClearsTheLocationAndLeavesTheTombstoneDB(t *testing.T) {
	h := newReviewHarness(t)
	uid := h.estimated(t, "wrong", "Brno", 49.2, 16.6)
	svc := h.checksService()

	question := questionOf(t, svc, "tester", review.KindPlace)
	res, err := svc.Answer(context.Background(), "tester", question.ID, review.AnswerNo, reviewMeta)
	if err != nil {
		t.Fatalf("Answer(no): %v", err)
	}
	if res.Result != "cleared" {
		t.Errorf("result = %q, want cleared", res.Result)
	}

	after, err := h.photos.GetByUID(context.Background(), uid)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if after.Lat != nil || after.Lng != nil {
		t.Errorf("coordinates = %v/%v, want them cleared", after.Lat, after.Lng)
	}
	// 'manual' with no coordinates is the tombstone: without it the nightly
	// backfill would hand the very same guess straight back.
	if after.LocationSource != photos.LocationSourceManual {
		t.Errorf("location_source = %q, want the %q tombstone",
			after.LocationSource, photos.LocationSourceManual)
	}
	candidates, err := h.photos.ListLocationCandidates(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListLocationCandidates: %v", err)
	}
	for _, c := range candidates {
		if c.UID == uid {
			t.Errorf("photo %s is a backfill candidate again, want the tombstone to hold it out", uid)
		}
	}
}

// countEstimates reports how many photos still carry an unresolved estimated
// location.
func countEstimates(t *testing.T, h *reviewHarness) int {
	t.Helper()
	total, err := h.photos.CountEstimatedLocations(context.Background())
	if err != nil {
		t.Fatalf("CountEstimatedLocations: %v", err)
	}
	return total
}

// duplicatePair inserts two photos with identical perceptual hashes and near
// identical embeddings, i.e. a pair the detector links on both signals.
func (h *reviewHarness) duplicatePair(t *testing.T) (string, string) {
	t.Helper()
	ctx := context.Background()
	left := h.embedded(t, "dup-left", imgVec(map[int]float32{0: 1}))
	right := h.embedded(t, "dup-right", imgVec(map[int]float32{0: 1}))
	for _, uid := range []string{left, right} {
		if err := h.photos.SetPhash(ctx, photos.Phash{PhotoUID: uid, Phash: 42, Dhash: 42}); err != nil {
			t.Fatalf("SetPhash(%s): %v", uid, err)
		}
	}
	return left, right
}

func TestReviewDuplicateCheck_yesConfirmsAndNeverMergesDB(t *testing.T) {
	h := newReviewHarness(t)
	left, right := h.duplicatePair(t)
	svc := h.checksService()

	question := questionOf(t, svc, "tester", review.KindDuplicate)
	if question.Other == nil {
		t.Fatalf("duplicate question carries no second photo: %+v", question)
	}
	if question.Photo.UID == question.Other.UID {
		t.Fatalf("duplicate question shows the same photo twice (%s)", question.Photo.UID)
	}

	res, err := svc.Answer(context.Background(), "tester", question.ID, review.AnswerYes, reviewMeta)
	if err != nil {
		t.Fatalf("Answer(yes): %v", err)
	}
	if res.Result != "confirmed" {
		t.Errorf("result = %q, want confirmed", res.Result)
	}

	ctx := context.Background()
	confirmed, err := h.feedback.IsDuplicateConfirmed(ctx,
		feedback.DuplicateConfirmationKey{PhotoUID: left, OtherUID: right})
	if err != nil {
		t.Fatalf("IsDuplicateConfirmed: %v", err)
	}
	if !confirmed {
		t.Errorf("pair not recorded as confirmed")
	}
	// The one thing the game must never do: neither photo may be archived or
	// deleted by an answer. Merging stays an explicit act on the duplicates page.
	for _, uid := range []string{left, right} {
		photo, getErr := h.photos.GetByUID(ctx, uid)
		if getErr != nil {
			t.Fatalf("GetByUID(%s): %v — the answer must not delete a photo", uid, getErr)
		}
		if photo.ArchivedAt != nil {
			t.Errorf("photo %s archived by a duplicate confirmation", uid)
		}
	}
	// And it is not a dismissal: the pair still links, it is simply judged.
	dismissed, err := h.feedback.IsDuplicateDismissed(ctx,
		feedback.DuplicateDismissalKey{PhotoUID: left, OtherUID: right})
	if err != nil {
		t.Fatalf("IsDuplicateDismissed: %v", err)
	}
	if dismissed {
		t.Errorf("pair also dismissed, want the confirmation alone")
	}
}

func TestReviewDuplicateCheck_noDismissesAndTheGroupStopsComingBackDB(t *testing.T) {
	h := newReviewHarness(t)
	left, right := h.duplicatePair(t)
	svc := h.checksService()

	question := questionOf(t, svc, "tester", review.KindDuplicate)
	res, err := svc.Answer(context.Background(), "tester", question.ID, review.AnswerNo, reviewMeta)
	if err != nil {
		t.Fatalf("Answer(no): %v", err)
	}
	if res.Result != "rejected" {
		t.Errorf("result = %q, want rejected", res.Result)
	}

	ctx := context.Background()
	dismissed, err := h.feedback.IsDuplicateDismissed(ctx,
		feedback.DuplicateDismissalKey{PhotoUID: left, OtherUID: right})
	if err != nil {
		t.Fatalf("IsDuplicateDismissed: %v", err)
	}
	if !dismissed {
		t.Errorf("pair not recorded as dismissed")
	}
	confirmed, err := h.feedback.IsDuplicateConfirmed(ctx,
		feedback.DuplicateConfirmationKey{PhotoUID: left, OtherUID: right})
	if err != nil {
		t.Fatalf("IsDuplicateConfirmed: %v", err)
	}
	if confirmed {
		t.Errorf("pair also confirmed, want the dismissal alone")
	}
	// The point of persisting the "no": a fresh service (cold cache, empty
	// session) must not offer the pair again.
	fresh, err := h.checksService().Queue(ctx, "other", review.SourceBoth, 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	for _, q := range fresh.Questions {
		if q.Kind == review.KindDuplicate {
			t.Errorf("dismissed pair offered again as %s", q.ID)
		}
	}
}

// misassigned creates a subject with three tight exemplar faces plus one face
// far from them, all assigned to that subject through markers — the shape the
// outlier ranking exists to find.
func (h *reviewHarness) misassigned(t *testing.T) (people.Subject, string) {
	t.Helper()
	ctx := context.Background()
	subj, err := h.people.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	tight := []([]float32){
		vec(map[int]float32{0: 1}),
		vec(map[int]float32{0: 0.99, 1: 0.1411}),
		vec(map[int]float32{0: 0.99, 2: 0.1411}),
	}
	for i, v := range tight {
		h.assignedFace(t, subj, "alice-ok-"+string(rune('a'+i)), v)
	}
	// Orthogonal to the centroid: cosine distance ~1, well past the 0.5 floor.
	odd := h.assignedFace(t, subj, "alice-odd", vec(map[int]float32{5: 1}))
	return subj, odd
}

// assignedFace writes one face on a fresh photo, creates its marker for subj and
// links the two, exactly as the assign state machine would.
func (h *reviewHarness) assignedFace(t *testing.T, subj people.Subject, hash string, v []float32) string {
	t.Helper()
	ctx := context.Background()
	photoUID := h.face(t, hash, vectors.Face{
		FaceIndex: 0, Vector: v, DetScore: 0.95, SubjectUID: &subj.UID,
	})
	marker, err := h.people.CreateMarker(ctx, people.Marker{
		PhotoUID: photoUID, SubjectUID: &subj.UID, Type: people.MarkerFace,
		X: 0.3, Y: 0.3, W: 0.3, H: 0.3, Reviewed: true,
	})
	if err != nil {
		t.Fatalf("CreateMarker(%s): %v", hash, err)
	}
	if err := h.vectors.UpdateFaceMarker(ctx, photoUID, 0, marker.UID, subj.UID, subj.Name); err != nil {
		t.Fatalf("UpdateFaceMarker(%s): %v", hash, err)
	}
	return photoUID
}

func TestReviewOutlierCheck_noDetachesThePersonAndKeepsTheMarkerDB(t *testing.T) {
	h := newReviewHarness(t)
	subj, odd := h.misassigned(t)
	svc := h.checksService()

	question := questionOf(t, svc, "tester", review.KindOutlier)
	if question.Photo.UID != odd {
		t.Fatalf("outlier question photo = %q, want the odd one out %q", question.Photo.UID, odd)
	}
	if question.Subject == nil || question.Subject.UID != subj.UID {
		t.Fatalf("outlier question subject = %+v, want %s", question.Subject, subj.UID)
	}
	if question.BBox == nil {
		t.Fatalf("outlier question carries no face box, so the crop cannot be drawn")
	}

	res, err := svc.Answer(context.Background(), "tester", question.ID, review.AnswerNo, reviewMeta)
	if err != nil {
		t.Fatalf("Answer(no): %v", err)
	}
	if res.Result != "detached" {
		t.Errorf("result = %q, want detached", res.Result)
	}

	ctx := context.Background()
	faces, err := h.vectors.FacesByKeys(ctx, []vectors.FaceKey{{PhotoUID: odd, FaceIndex: 0}})
	if err != nil {
		t.Fatalf("FacesByKeys: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("faces = %d, want the face itself kept", len(faces))
	}
	if faces[0].SubjectUID != nil {
		t.Errorf("face still assigned to %q, want the person detached", *faces[0].SubjectUID)
	}
	// Detaching is not deleting: the region survives, so the face can be
	// reassigned (and the answer undone) without re-detecting anything.
	if faces[0].MarkerUID == nil || *faces[0].MarkerUID == "" {
		t.Errorf("marker link dropped, want the marker kept")
	}
	markers, err := h.people.ListMarkersByPhoto(ctx, odd)
	if err != nil {
		t.Fatalf("ListMarkersByPhoto: %v", err)
	}
	if len(markers) != 1 {
		t.Fatalf("markers = %d, want the marker kept", len(markers))
	}
	if markers[0].SubjectUID != nil {
		t.Errorf("marker still carries subject %q, want it cleared", *markers[0].SubjectUID)
	}
	// The other three assignments are untouched: an answer is about one face.
	remaining, err := h.vectors.ListFacesBySubject(ctx, subj.UID)
	if err != nil {
		t.Fatalf("ListFacesBySubject: %v", err)
	}
	if len(remaining) != 3 {
		t.Errorf("faces still assigned = %d, want the other 3 left alone", len(remaining))
	}
}

func TestReviewOutlierCheck_yesConfirmsAndStopsSurfacingDB(t *testing.T) {
	h := newReviewHarness(t)
	subj, odd := h.misassigned(t)
	svc := h.checksService()

	question := questionOf(t, svc, "tester", review.KindOutlier)
	res, err := svc.Answer(context.Background(), "tester", question.ID, review.AnswerYes, reviewMeta)
	if err != nil {
		t.Fatalf("Answer(yes): %v", err)
	}
	if res.Result != "confirmed" {
		t.Errorf("result = %q, want confirmed", res.Result)
	}

	ctx := context.Background()
	confirmed, err := h.feedback.IsFaceConfirmed(ctx, feedback.FaceConfirmationKey{
		PhotoUID: odd, FaceIndex: 0, SubjectUID: subj.UID,
	})
	if err != nil {
		t.Fatalf("IsFaceConfirmed: %v", err)
	}
	if !confirmed {
		t.Errorf("face not recorded as confirmed")
	}
	// A confirmation changes nothing about the assignment itself.
	faces, err := h.vectors.FacesByKeys(ctx, []vectors.FaceKey{{PhotoUID: odd, FaceIndex: 0}})
	if err != nil {
		t.Fatalf("FacesByKeys: %v", err)
	}
	if len(faces) != 1 || faces[0].SubjectUID == nil || *faces[0].SubjectUID != subj.UID {
		t.Errorf("face assignment changed by a confirmation: %+v", faces)
	}
	// And the /outliers page agrees: the confirmed face is excluded there too,
	// because both views read the same feedback set.
	ranked, err := outliers.New(outliers.Config{
		Faces: h.vectors, People: h.people, Feedback: h.feedback,
	}).Outliers(ctx, subj.UID, outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	for _, face := range ranked.Faces {
		if face.PhotoUID == odd {
			t.Errorf("confirmed face still offered as an outlier")
		}
	}
	// The game does not offer it again either, from a cold service.
	fresh, err := h.checksService().Queue(ctx, "other", review.SourceBoth, 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	for _, q := range fresh.Questions {
		if q.Kind == review.KindOutlier && q.Photo.UID == odd {
			t.Errorf("confirmed face offered again as %s", q.ID)
		}
	}
}

func TestReviewQueue_mixesEveryKindWithoutOneDominatingDB(t *testing.T) {
	h := newReviewHarness(t)
	// Two of each new kind's material, plus a face candidate, so the merge has
	// several lists to spread through each other.
	h.estimated(t, "guess-a", "Brno", 49.2, 16.6)
	h.estimated(t, "guess-b", "Praha", 50.1, 14.4)
	h.duplicatePair(t)
	h.misassigned(t)
	h.namedSubject(t, "Bob", "bob-src", vec(map[int]float32{3: 1}))
	h.face(t, "bob-band", vectors.Face{
		FaceIndex: 0, Vector: vec(map[int]float32{3: 0.6, 4: 0.8}), DetScore: 0.9,
	})

	res, err := h.checksService().Queue(context.Background(), "tester", review.SourceBoth, 20)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	counts := map[review.Kind]int{}
	for _, q := range res.Questions {
		counts[q.Kind]++
	}
	for _, kind := range []review.Kind{review.KindPlace, review.KindDuplicate, review.KindOutlier} {
		if counts[kind] == 0 {
			t.Errorf("no %s question in the batch: %v", kind, counts)
		}
	}
	// No kind may own the batch. With four kinds supplying material the merge
	// gives each about a quarter; the assertion is deliberately loose (a kind may
	// legitimately have more material than the others) but a kind holding more
	// than half of a mixed batch means the merge stopped mixing.
	for kind, n := range counts {
		if n*2 > len(res.Questions) {
			t.Errorf("%s holds %d of %d questions, want no kind past half the batch",
				kind, n, len(res.Questions))
		}
	}
}

func TestReviewQueue_peopleSourceSkipsTheChecksDB(t *testing.T) {
	h := newReviewHarness(t)
	h.estimated(t, "guess", "Brno", 49.2, 16.6)
	h.duplicatePair(t)
	h.misassigned(t)

	// The three checks ride with SourceBoth alone: restricting the game to people
	// is a promise about what it will ask, and a place question would break it.
	res, err := h.checksService().Queue(context.Background(), "tester", review.SourcePeople, 20)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	for _, q := range res.Questions {
		switch q.Kind {
		case review.KindPlace, review.KindDuplicate, review.KindOutlier:
			t.Errorf("people-only queue served a %s question", q.Kind)
		case review.KindFace, review.KindLabel:
		}
	}
}

func TestReviewAnswer_placeVerdictIsAuditedAsAReviewDecisionDB(t *testing.T) {
	h := newReviewHarness(t)
	h.estimated(t, "guessed", "Brno", 49.2, 16.6)
	svc := h.checksService()
	question := questionOf(t, svc, "tester", review.KindPlace)

	if _, err := svc.Answer(
		context.Background(), "tester", question.ID, review.AnswerYes, reviewMeta,
	); err != nil {
		t.Fatalf("Answer(yes): %v", err)
	}

	entries, err := audit.NewStore(h.db.Pool()).List(context.Background(), audit.Filter{
		Action: audit.ActionLocationConfirm, Limit: 10,
	})
	if err != nil {
		t.Fatalf("audit List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("audit rows = %d, want exactly one location.confirm", len(entries))
	}
	if entries[0].Details["via"] != audit.ViaReview {
		t.Errorf("audit details.via = %v, want %q — the leaderboard filters on it",
			entries[0].Details["via"], audit.ViaReview)
	}
	if entries[0].CreatedAt.IsZero() || time.Since(entries[0].CreatedAt) > time.Hour {
		t.Errorf("audit created_at = %v, want it stamped now", entries[0].CreatedAt)
	}
}
