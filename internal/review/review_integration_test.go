//go:build integration

package review_test

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/review"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against
// KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they do not run in parallel.

// reviewHarness bundles real stores and the composed services over a freshly
// truncated database, mirroring cmd/kukatko's wiring.
type reviewHarness struct {
	db       *database.DB
	photos   *photos.Store
	people   *people.Store
	vectors  *vectors.Store
	organize *organize.Store
	feedback *feedback.Store
}

// newReviewHarness returns a harness over a truncated integration database.
func newReviewHarness(t *testing.T) *reviewHarness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return &reviewHarness{
		db:       db,
		photos:   photos.NewStore(db.Pool()),
		people:   people.NewStore(db.Pool()),
		vectors:  vectors.NewStore(db.Pool()),
		organize: organize.NewStore(db.Pool()),
		feedback: feedback.NewStore(db.Pool()),
	}
}

// service composes a fresh review service over the harness stores — a fresh
// instance means a cold cache and empty sessions, like a server restart.
func (h *reviewHarness) service() *review.Service {
	candSvc := candidates.New(candidates.Config{
		Faces: h.vectors, People: h.people, Feedback: h.feedback, Photos: h.photos,
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 1000, MinFacePx: 32, Concurrency: 2, MinFaceRel: 0.02,
	})
	sweepSvc := sweep.New(sweep.Config{Subjects: h.people, Finder: candSvc, Concurrency: 2})
	expandSvc := expand.New(expand.Config{
		Vectors: h.vectors, Organize: h.organize, Feedback: h.feedback, Photos: h.photos,
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 200, Concurrency: 2,
	})
	matchSvc := facematch.New(facematch.Config{Photos: h.photos, Faces: h.vectors, People: h.people})
	return review.New(review.Config{
		Sweeper:  sweepSvc,
		Expander: expandSvc,
		Organize: h.organize,
		Faces:    h.vectors,
		Feedback: h.feedback,
		Assigner: matchSvc,
		BandMin:  0.45, BandMax: 0.75,
	})
}

// vec builds a FaceDim face vector from index→value overrides.
func vec(set map[int]float32) []float32 {
	v := make([]float32, vectors.FaceDim)
	for i, x := range set {
		v[i] = x
	}
	return v
}

// imgVec builds an ImageDim image embedding from index→value overrides.
func imgVec(set map[int]float32) []float32 {
	v := make([]float32, vectors.ImageDim)
	for i, x := range set {
		v[i] = x
	}
	return v
}

// reviewableBox clears both face-size floors on a 1000x800 photo.
var reviewableBox = [4]float64{0.3, 0.3, 0.3, 0.3}

// photo inserts a 1000x800 photo and returns its uid.
func (h *reviewHarness) photo(t *testing.T, hash string) string {
	t.Helper()
	created, err := h.photos.Create(context.Background(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileWidth: 1000, FileHeight: 800, FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// face writes one face onto a fresh photo and returns the photo uid.
func (h *reviewHarness) face(t *testing.T, hash string, face vectors.Face) string {
	t.Helper()
	photoUID := h.photo(t, hash)
	face.PhotoWidth, face.PhotoHeight, face.Orientation = 1000, 800, 1
	if face.BBox == ([4]float64{}) {
		face.BBox = reviewableBox
	}
	if err := h.vectors.SaveFaces(context.Background(), photoUID, []vectors.Face{face}); err != nil {
		t.Fatalf("SaveFaces(%s): %v", hash, err)
	}
	return photoUID
}

// namedSubject creates a subject with one exemplar face AND an assigned marker,
// so the sweep (which scans subjects with markers) picks it up.
func (h *reviewHarness) namedSubject(t *testing.T, name, hash string, exemplar []float32) people.Subject {
	t.Helper()
	subj, err := h.people.CreateSubject(context.Background(), people.Subject{Name: name})
	if err != nil {
		t.Fatalf("CreateSubject(%s): %v", name, err)
	}
	photoUID := h.face(t, hash, vectors.Face{
		FaceIndex: 0, Vector: exemplar, DetScore: 0.95, SubjectUID: &subj.UID,
	})
	if _, err := h.people.CreateMarker(context.Background(), people.Marker{
		PhotoUID: photoUID, SubjectUID: &subj.UID, Type: people.MarkerFace,
		X: 0.3, Y: 0.3, W: 0.3, H: 0.3,
	}); err != nil {
		t.Fatalf("CreateMarker(%s): %v", name, err)
	}
	return subj
}

// embedded inserts a photo with an image embedding and returns the uid.
func (h *reviewHarness) embedded(t *testing.T, hash string, v []float32) string {
	t.Helper()
	uid := h.photo(t, hash)
	if _, err := h.vectors.SaveEmbedding(context.Background(), vectors.Embedding{
		PhotoUID: uid, Vector: v, Model: "clip", Pretrained: "test",
	}); err != nil {
		t.Fatalf("SaveEmbedding(%s): %v", hash, err)
	}
	return uid
}

// label creates a label with the given member photos and returns it.
func (h *reviewHarness) label(t *testing.T, name string, members ...string) organize.Label {
	t.Helper()
	created, err := h.organize.CreateLabel(context.Background(), organize.Label{Name: name})
	if err != nil {
		t.Fatalf("CreateLabel(%s): %v", name, err)
	}
	for _, member := range members {
		if err := h.organize.AttachLabel(
			context.Background(), member, created.UID, organize.SourceManual, 0,
		); err != nil {
			t.Fatalf("AttachLabel(%s): %v", member, err)
		}
	}
	return created
}

// queueIDs fetches one big batch and returns the question ids in order.
func queueIDs(t *testing.T, svc *review.Service) []string {
	t.Helper()
	res, err := svc.Queue(context.Background(), "tester", 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	ids := make([]string, 0, len(res.Questions))
	for _, q := range res.Questions {
		ids = append(ids, q.ID)
	}
	return ids
}

// bandFace is 0.4 cosine-distance from e0 → confidence 0.60, inside the band.
func bandFace() []float32 { return vec(map[int]float32{0: 0.6, 1: 0.8}) }

func TestReviewQueue_faceBandAndExclusionsDB(t *testing.T) {
	h := newReviewHarness(t)
	alice := h.namedSubject(t, "Alice", "alice-src", vec(map[int]float32{0: 1}))
	bob, err := h.people.CreateSubject(context.Background(), people.Subject{Name: "Bob"})
	if err != nil {
		t.Fatalf("CreateSubject(Bob): %v", err)
	}
	bandPhoto := h.face(t, "band", vectors.Face{FaceIndex: 0, Vector: bandFace(), DetScore: 0.9})
	// Too certain (confidence 0.90 ≥ 0.75): belongs on /recognition, not the game.
	h.face(t, "certain", vectors.Face{
		FaceIndex: 0, Vector: vec(map[int]float32{0: 0.9, 1: 0.43589}), DetScore: 0.9,
	})
	// Below the band (confidence 0.30): outside the search threshold entirely.
	h.face(t, "noise", vectors.Face{
		FaceIndex: 0, Vector: vec(map[int]float32{0: 0.3, 1: 0.9539}), DetScore: 0.9,
	})
	// Already assigned to another person: the unassigned-only search skips it.
	h.face(t, "taken", vectors.Face{
		FaceIndex: 0, Vector: vec(map[int]float32{0: 0.99, 1: 0.1411}), DetScore: 0.9,
		SubjectUID: &bob.UID,
	})
	// Too small to be a fair question (15px face).
	h.face(t, "tiny", vectors.Face{
		FaceIndex: 0, Vector: vec(map[int]float32{0: 0.65, 2: 0.7599}), DetScore: 0.9,
		BBox: [4]float64{0.01, 0.01, 0.015, 0.015},
	})
	// In band but already rejected for Alice: never asked again.
	rejectedPhoto := h.face(t, "rejected", vectors.Face{
		FaceIndex: 0, Vector: vec(map[int]float32{0: 0.6, 2: 0.8}), DetScore: 0.9,
	})
	rejKey := feedback.FaceRejectionKey{PhotoUID: rejectedPhoto, FaceIndex: 0, SubjectUID: alice.UID}
	if err := h.feedback.RejectFace(context.Background(), rejKey, audit.Entry{Action: "face.reject"}); err != nil {
		t.Fatalf("RejectFace: %v", err)
	}

	res, err := h.service().Queue(context.Background(), "tester", 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 1 {
		t.Fatalf("questions = %+v, want exactly the one band candidate", res.Questions)
	}
	q := res.Questions[0]
	if q.Kind != review.KindFace || q.Photo.UID != bandPhoto || q.Subject.UID != alice.UID {
		t.Errorf("question = %+v, want the band face for Alice on %s", q, bandPhoto)
	}
	if q.Confidence < 0.55 || q.Confidence > 0.65 {
		t.Errorf("confidence = %f, want ≈0.60", q.Confidence)
	}
	if q.BBox == nil || q.BBox.Pixel == ([4]int{}) || q.BBox.Relative != reviewableBox {
		t.Errorf("bbox = %+v, want pixel and relative boxes for %v", q.BBox, reviewableBox)
	}
	if q.Action != string(candidates.ActionCreateMarker) {
		t.Errorf("action = %q, want create_marker (no marker exists yet)", q.Action)
	}
}

func TestReviewAnswer_faceYesAssignsAndConvergesDB(t *testing.T) {
	h := newReviewHarness(t)
	alice := h.namedSubject(t, "Alice", "alice-src", vec(map[int]float32{0: 1}))
	bandPhoto := h.face(t, "band", vectors.Face{FaceIndex: 0, Vector: bandFace(), DetScore: 0.9})

	svc := h.service()
	res, err := svc.Queue(context.Background(), "tester", 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 1 {
		t.Fatalf("questions = %d, want 1", len(res.Questions))
	}
	questionID := res.Questions[0].ID

	answer, err := svc.Answer(context.Background(), "tester", questionID, review.AnswerYes, audit.Meta{ActorUID: ""})
	if err != nil {
		t.Fatalf("Answer yes: %v", err)
	}
	if answer.Result != "assigned" || answer.Answered != 1 || answer.Remaining != 0 {
		t.Fatalf("answer = %+v, want assigned 1/0", answer)
	}
	markers, err := h.people.ListMarkersByPhoto(context.Background(), bandPhoto)
	if err != nil {
		t.Fatalf("ListMarkersByPhoto: %v", err)
	}
	if len(markers) != 1 || markers[0].SubjectUID == nil || *markers[0].SubjectUID != alice.UID {
		t.Fatalf("markers = %+v, want one marker assigned to Alice", markers)
	}

	// Answering the same question again must not double-assign or error.
	again, err := svc.Answer(context.Background(), "tester", questionID, review.AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("repeat Answer: %v", err)
	}
	if again.Result != "already_answered" || again.Answered != 1 {
		t.Errorf("repeat answer = %+v, want already_answered with count still 1", again)
	}
	// Even a fresh service (cold session — like answering after a restart)
	// must not create a second marker: the face row already carries Alice.
	fresh, err := h.service().Answer(context.Background(), "tester", questionID, review.AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("cold repeat Answer: %v", err)
	}
	if fresh.Result != "assigned" {
		t.Errorf("cold repeat = %+v, want assigned (idempotent short-circuit)", fresh)
	}
	markers, err = h.people.ListMarkersByPhoto(context.Background(), bandPhoto)
	if err != nil {
		t.Fatalf("ListMarkersByPhoto: %v", err)
	}
	if len(markers) != 1 {
		t.Fatalf("markers after replay = %d, want still 1", len(markers))
	}

	// The assigned face is gone from a rebuilt queue (unassigned-only search).
	if ids := queueIDs(t, h.service()); len(ids) != 0 {
		t.Errorf("queue after assign = %v, want empty", ids)
	}
}

// TestReviewAnswer_faceYesTagsAuditViaReview proves the closed attribution gap:
// confirming a face in the review game writes a face.assign audit row tagged
// details.via = "review", while an ordinary assignment through the same state
// machine stays untagged — so the leaderboard counts only review decisions.
func TestReviewAnswer_faceYesTagsAuditViaReview(t *testing.T) {
	h := newReviewHarness(t)
	h.namedSubject(t, "Alice", "alice-src", vec(map[int]float32{0: 1}))
	h.face(t, "band", vectors.Face{FaceIndex: 0, Vector: bandFace(), DetScore: 0.9})

	svc := h.service()
	res, err := svc.Queue(context.Background(), "tester", 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 1 {
		t.Fatalf("questions = %d, want 1", len(res.Questions))
	}
	if _, err := svc.Answer(context.Background(), "tester", res.Questions[0].ID, review.AnswerYes, audit.Meta{}); err != nil {
		t.Fatalf("Answer yes: %v", err)
	}

	auditStore := audit.NewStore(h.db.Pool())
	assigns, err := auditStore.List(context.Background(), audit.Filter{Action: audit.ActionFaceAssign, Limit: 50})
	if err != nil {
		t.Fatalf("List face.assign: %v", err)
	}
	if len(assigns) != 1 {
		t.Fatalf("face.assign rows = %d, want 1 (only the review confirmation)", len(assigns))
	}
	if assigns[0].Details["via"] != "review" {
		t.Errorf("review face.assign details[via] = %v, want review", assigns[0].Details["via"])
	}

	// An ordinary assignment through the same facematch state machine must NOT
	// carry the via marker.
	matchSvc := facematch.New(facematch.Config{Photos: h.photos, Faces: h.vectors, People: h.people})
	bob, err := h.people.CreateSubject(context.Background(), people.Subject{Name: "Bob"})
	if err != nil {
		t.Fatalf("CreateSubject(Bob): %v", err)
	}
	plainPhoto := h.face(t, "plain", vectors.Face{FaceIndex: 0, Vector: vec(map[int]float32{3: 1}), DetScore: 0.9})
	faceIdx, box := 0, reviewableBox
	if _, err := matchSvc.Apply(context.Background(), facematch.AssignRequest{
		PhotoUID: plainPhoto, Action: facematch.ActionCreateMarker,
		SubjectUID: bob.UID, FaceIndex: &faceIdx, BBox: &box,
	}, audit.Meta{}); err != nil {
		t.Fatalf("plain Apply: %v", err)
	}

	assigns, err = auditStore.List(context.Background(), audit.Filter{Action: audit.ActionFaceAssign, Limit: 50})
	if err != nil {
		t.Fatalf("List face.assign again: %v", err)
	}
	if len(assigns) != 2 {
		t.Fatalf("face.assign rows = %d, want 2", len(assigns))
	}
	// List is newest-first, so the plain (non-review) assignment is assigns[0].
	if via, tagged := assigns[0].Details["via"]; tagged {
		t.Errorf("non-review face.assign details[via] = %v, want absent", via)
	}
}

func TestReviewAnswer_faceNoRejectsDB(t *testing.T) {
	h := newReviewHarness(t)
	alice := h.namedSubject(t, "Alice", "alice-src", vec(map[int]float32{0: 1}))
	bandPhoto := h.face(t, "band", vectors.Face{FaceIndex: 0, Vector: bandFace(), DetScore: 0.9})

	svc := h.service()
	ids := queueIDs(t, svc)
	if len(ids) != 1 {
		t.Fatalf("queue = %v, want one question", ids)
	}
	res, err := svc.Answer(context.Background(), "tester", ids[0], review.AnswerNo, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer no: %v", err)
	}
	if res.Result != "rejected" {
		t.Fatalf("result = %q, want rejected", res.Result)
	}
	rejected, err := h.feedback.IsFaceRejected(context.Background(), feedback.FaceRejectionKey{
		PhotoUID: bandPhoto, FaceIndex: 0, SubjectUID: alice.UID,
	})
	if err != nil || !rejected {
		t.Fatalf("IsFaceRejected = %v, %v; want true", rejected, err)
	}
	// The rejection is durable: even a fresh service never asks again.
	if ids := queueIDs(t, h.service()); len(ids) != 0 {
		t.Errorf("queue after no = %v, want empty", ids)
	}
}

func TestReviewAnswer_labelFlowDB(t *testing.T) {
	h := newReviewHarness(t)
	member := h.embedded(t, "member", imgVec(map[int]float32{0: 1}))
	label := h.label(t, "Ostatky", member)
	bandPhoto := h.embedded(t, "band", imgVec(map[int]float32{0: 0.6, 1: 0.8}))
	// Too certain (similarity 0.95 ≥ 0.75): bulk-confirm territory.
	h.embedded(t, "certain", imgVec(map[int]float32{0: 0.95, 1: 0.3122}))

	svc := h.service()
	res, err := svc.Queue(context.Background(), "tester", 100)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 1 {
		t.Fatalf("questions = %+v, want exactly the band candidate", res.Questions)
	}
	q := res.Questions[0]
	if q.Kind != review.KindLabel || q.Photo.UID != bandPhoto || q.Label.UID != label.UID {
		t.Fatalf("question = %+v, want the band photo for label %s", q, label.UID)
	}

	answer, err := svc.Answer(context.Background(), "tester", q.ID, review.AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer yes: %v", err)
	}
	if answer.Result != "labeled" {
		t.Fatalf("result = %q, want labeled", answer.Result)
	}
	labels, err := h.organize.LabelsForPhoto(context.Background(), bandPhoto)
	if err != nil {
		t.Fatalf("LabelsForPhoto: %v", err)
	}
	if len(labels) != 1 || labels[0].UID != label.UID {
		t.Fatalf("labels = %+v, want [%s]", labels, label.UID)
	}
	// Now a member, the photo is excluded from the next rebuilt queue.
	if ids := queueIDs(t, h.service()); len(ids) != 0 {
		t.Errorf("queue after labeling = %v, want empty", ids)
	}
}

func TestReviewAnswer_labelNoRejectsDB(t *testing.T) {
	h := newReviewHarness(t)
	member := h.embedded(t, "member", imgVec(map[int]float32{0: 1}))
	label := h.label(t, "Ostatky", member)
	bandPhoto := h.embedded(t, "band", imgVec(map[int]float32{0: 0.6, 1: 0.8}))

	svc := h.service()
	ids := queueIDs(t, svc)
	if len(ids) != 1 {
		t.Fatalf("queue = %v, want one question", ids)
	}
	if _, err := svc.Answer(context.Background(), "tester", ids[0], review.AnswerNo, audit.Meta{}); err != nil {
		t.Fatalf("Answer no: %v", err)
	}
	rejected, err := h.feedback.IsLabelRejected(context.Background(), feedback.LabelRejectionKey{
		PhotoUID: bandPhoto, LabelUID: label.UID,
	})
	if err != nil || !rejected {
		t.Fatalf("IsLabelRejected = %v, %v; want true", rejected, err)
	}
	if ids := queueIDs(t, h.service()); len(ids) != 0 {
		t.Errorf("queue after no = %v, want empty", ids)
	}
}

func TestReviewAnswer_skipDoesNotLeadNextBatchDB(t *testing.T) {
	h := newReviewHarness(t)
	h.namedSubject(t, "Alice", "alice-src", vec(map[int]float32{0: 1}))
	h.face(t, "band1", vectors.Face{FaceIndex: 0, Vector: bandFace(), DetScore: 0.9})
	h.face(t, "band2", vectors.Face{
		FaceIndex: 0, Vector: vec(map[int]float32{0: 0.58, 1: 0.8146}), DetScore: 0.9,
	})

	svc := h.service()
	first, err := svc.Queue(context.Background(), "tester", 1)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	skipped := first.Questions[0].ID
	res, err := svc.Answer(context.Background(), "tester", skipped, review.AnswerSkip, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer skip: %v", err)
	}
	if res.Result != "skipped" || res.Answered != 0 {
		t.Fatalf("skip = %+v, want skipped and uncounted", res)
	}
	next, err := svc.Queue(context.Background(), "tester", 10)
	if err != nil {
		t.Fatalf("Queue after skip: %v", err)
	}
	if len(next.Questions) != 1 || next.Questions[0].ID == skipped {
		t.Fatalf("next batch = %+v, want just the other question", next.Questions)
	}
	// A skip is not a rejection: nothing durable was written.
	refs, err := h.feedback.FaceRejectionsForSubject(context.Background(), "any")
	if err != nil || len(refs) != 0 {
		t.Errorf("rejections after skip = %v, %v; want none", refs, err)
	}
}

func TestReviewQueue_emptyLibraryDB(t *testing.T) {
	h := newReviewHarness(t)
	res, err := h.service().Queue(context.Background(), "tester", 0)
	if err != nil {
		t.Fatalf("Queue on empty library: %v", err)
	}
	if len(res.Questions) != 0 || res.Reason != review.ReasonNoSources {
		t.Fatalf("result = %+v, want empty queue with reason %q", res, review.ReasonNoSources)
	}
}

func TestReviewQueue_deterministicDB(t *testing.T) {
	h := newReviewHarness(t)
	h.namedSubject(t, "Alice", "alice-src", vec(map[int]float32{0: 1}))
	for i, x := range []float32{0.60, 0.55, 0.65, 0.70} {
		h.face(t, fmt.Sprintf("face%d", i), vectors.Face{
			FaceIndex: 0, Vector: vec(map[int]float32{0: x, 1: sqrt32(1 - x*x)}), DetScore: 0.9,
		})
	}
	member := h.embedded(t, "member", imgVec(map[int]float32{0: 1}))
	h.label(t, "Ostatky", member)
	for i, x := range []float32{0.5, 0.62, 0.71} {
		h.embedded(t, fmt.Sprintf("cand%d", i), imgVec(map[int]float32{0: x, 1: sqrt32(1 - x*x)}))
	}
	first := queueIDs(t, h.service())
	second := queueIDs(t, h.service())
	if len(first) != 7 {
		t.Fatalf("questions = %d (%v), want 7", len(first), first)
	}
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Fatalf("queue not deterministic:\n first = %v\nsecond = %v", first, second)
	}
}

// sqrt32 returns the float32 square root, for building unit vectors.
func sqrt32(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}
