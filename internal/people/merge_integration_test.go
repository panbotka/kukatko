//go:build integration

package people_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover the merge of one subject into another:
// what moves, what the two sides' conflicting opinions resolve to, and that
// nothing is left pointing at the subject the merge deleted.

// mergeEnv bundles the stores a merge test asserts against.
type mergeEnv struct {
	people   *people.Store
	photos   *photos.Store
	vectors  *vectors.Store
	feedback *feedback.Store
	audit    *audit.Store
	db       *database.DB
}

// newMergeEnv returns the stores a merge test needs over a freshly truncated
// integration database.
func newMergeEnv(t *testing.T) mergeEnv {
	t.Helper()
	ps, photoStore, vs, db := newStores(t)
	return mergeEnv{
		people:   ps,
		photos:   photoStore,
		vectors:  vs,
		feedback: feedback.NewStore(db.Pool()),
		audit:    audit.NewStore(db.Pool()),
		db:       db,
	}
}

// makeSubject creates a named subject and returns it.
func makeSubject(t *testing.T, store *people.Store, name string) people.Subject {
	t.Helper()
	subj, err := store.CreateSubject(context.Background(), people.Subject{Name: name})
	if err != nil {
		t.Fatalf("CreateSubject %s: %v", name, err)
	}
	return subj
}

// markPhoto creates a face marker on photoUID assigned to subjectUID and returns
// its uid.
func markPhoto(t *testing.T, store *people.Store, photoUID, subjectUID string) string {
	t.Helper()
	m, err := store.CreateMarker(context.Background(), people.Marker{
		PhotoUID:   photoUID,
		SubjectUID: &subjectUID,
		Type:       people.MarkerFace,
		X:          0.1, Y: 0.1, W: 0.2, H: 0.2,
	})
	if err != nil {
		t.Fatalf("CreateMarker on %s: %v", photoUID, err)
	}
	return m.UID
}

// cacheFace stores face slot 0 of photoUID pointing at markerUID and the subject,
// mirroring what the detector plus an assignment leave behind. It is the
// denormalised cache the merge has to move by hand (faces has no foreign key to
// subjects).
func cacheFace(t *testing.T, store *vectors.Store, photoUID, markerUID string, subj people.Subject) {
	t.Helper()
	if err := store.SaveFaces(context.Background(), photoUID, []vectors.Face{{
		FaceIndex:   0,
		Vector:      faceVec(),
		BBox:        [4]float64{0.1, 0.1, 0.2, 0.2},
		MarkerUID:   &markerUID,
		SubjectUID:  &subj.UID,
		SubjectName: subj.Name,
	}}); err != nil {
		t.Fatalf("SaveFaces on %s: %v", photoUID, err)
	}
}

// countBySubject returns how many rows of table reference subjectUID, which is
// how the tests prove the merge left no orphan behind.
func countBySubject(t *testing.T, env mergeEnv, table, subjectUID string) int {
	t.Helper()
	var n int
	// table is a test-local constant, never user input.
	err := env.db.Pool().QueryRow(context.Background(),
		"SELECT count(*) FROM "+table+" WHERE subject_uid = $1", subjectUID).Scan(&n)
	if err != nil {
		t.Fatalf("counting %s of %s: %v", table, subjectUID, err)
	}
	return n
}

// markerSubjects returns the subject uid of every marker on photoUID, in a
// deterministic order, so a test can say exactly who a photo is tagged with.
func markerSubjects(t *testing.T, env mergeEnv, photoUID string) []string {
	t.Helper()
	markers, err := env.people.ListMarkersByPhoto(context.Background(), photoUID)
	if err != nil {
		t.Fatalf("ListMarkersByPhoto %s: %v", photoUID, err)
	}
	out := make([]string, 0, len(markers))
	for _, m := range markers {
		if m.SubjectUID != nil {
			out = append(out, *m.SubjectUID)
		}
	}
	return out
}

// mergeEntry builds the audit entry a merge handler would hand the store.
func mergeEntry(actorUID, sourceUID, keeperUID string) audit.Entry {
	return actorEntry(actorUID, audit.ActionSubjectMerge, "subjects", keeperUID, map[string]any{
		"source_uid": sourceUID,
		"keeper_uid": keeperUID,
	})
}

// TestMergeSubjects_movesEverything merges a duplicate person into a keeper and
// checks that every kind of row the source carried ends up on the keeper, that
// the keeper's empty fields are filled from it, and that nothing is left
// referencing the deleted subject.
func TestMergeSubjects_movesEverything(t *testing.T) {
	env := newMergeEnv(t)
	ctx := t.Context()
	actor := makeUser(t, env.db, "usr_merge", "merger")

	p1 := makePhoto(t, env.photos, "hash-1")
	p2 := makePhoto(t, env.photos, "hash-2")

	source := makeSubject(t, env.people, "Anna N.")
	keeper := makeSubject(t, env.people, "Anna Nováková")
	// The source is the richer record: it is favorited, has notes and a cover, all
	// of which the bare keeper should inherit.
	if _, err := env.people.UpdateSubject(ctx, source.UID, people.SubjectUpdate{
		Name: source.Name, Type: people.SubjectPerson, Favorite: true,
		Notes: "sister", CoverPhotoUID: &p1,
	}); err != nil {
		t.Fatalf("preparing source subject: %v", err)
	}
	source, _ = env.people.GetSubjectByUID(ctx, source.UID)

	sourceMarker := markPhoto(t, env.people, p1, source.UID)
	cacheFace(t, env.vectors, p1, sourceMarker, source)
	markPhoto(t, env.people, p2, keeper.UID)

	// One opinion of each kind, all recorded for the source.
	if err := env.feedback.ConfirmFace(ctx,
		feedback.FaceConfirmationKey{PhotoUID: p1, FaceIndex: 0, SubjectUID: source.UID},
		actorEntry(actor, audit.ActionFaceConfirm, "subjects", source.UID, nil)); err != nil {
		t.Fatalf("ConfirmFace: %v", err)
	}
	if err := env.feedback.RejectFace(ctx,
		feedback.FaceRejectionKey{PhotoUID: p2, FaceIndex: 1, SubjectUID: source.UID},
		actorEntry(actor, audit.ActionFaceReject, "subjects", source.UID, nil)); err != nil {
		t.Fatalf("RejectFace: %v", err)
	}
	if err := env.feedback.DismissDuplicateMarkers(ctx,
		feedback.DuplicateMarkerDismissalKey{PhotoUID: p1, SubjectUID: source.UID},
		actorEntry(actor, audit.ActionDuplicateMarkerDismiss, "subjects", source.UID, nil)); err != nil {
		t.Fatalf("DismissDuplicateMarkers: %v", err)
	}

	res, err := env.people.MergeSubjectsAudited(ctx, source.UID, keeper.UID,
		mergeEntry(actor, source.UID, keeper.UID))
	if err != nil {
		t.Fatalf("MergeSubjectsAudited: %v", err)
	}
	if res.MarkersMoved != 1 || res.FacesMoved != 1 || res.ConfirmationsMoved != 1 ||
		res.RejectionsMoved != 1 || res.DismissalsMoved != 1 || res.SharedPhotos != 0 {
		t.Errorf("merge result = %+v, want one of each moved and no shared photo", res)
	}

	if _, err := env.people.GetSubjectByUID(ctx, source.UID); !errors.Is(err, people.ErrSubjectNotFound) {
		t.Errorf("source subject after merge: err = %v, want ErrSubjectNotFound", err)
	}

	// The marker and the face cache now name the keeper, by uid and by name.
	if got := markerSubjects(t, env, p1); len(got) != 1 || got[0] != keeper.UID {
		t.Errorf("markers on %s = %v, want [%s]", p1, got, keeper.UID)
	}
	cachedUID, cachedName := faceCache(t, env.vectors, p1)
	if cachedUID == nil || *cachedUID != keeper.UID || cachedName != keeper.Name {
		t.Errorf("face cache = (%v, %q), want (%s, %q)", cachedUID, cachedName, keeper.UID, keeper.Name)
	}

	// Every opinion is readable through the keeper.
	confirmed, err := env.feedback.IsFaceConfirmed(ctx,
		feedback.FaceConfirmationKey{PhotoUID: p1, FaceIndex: 0, SubjectUID: keeper.UID})
	if err != nil || !confirmed {
		t.Errorf("IsFaceConfirmed(keeper) = %v, %v, want true", confirmed, err)
	}
	rejections, err := env.feedback.FaceRejectionsForSubject(ctx, keeper.UID)
	if err != nil {
		t.Fatalf("FaceRejectionsForSubject: %v", err)
	}
	if len(rejections) != 1 || rejections[0].PhotoUID != p2 || rejections[0].FaceIndex != 1 {
		t.Errorf("keeper rejections = %+v, want the source's %s#1", rejections, p2)
	}
	dismissed, err := env.feedback.IsDuplicateMarkersDismissed(ctx,
		feedback.DuplicateMarkerDismissalKey{PhotoUID: p1, SubjectUID: keeper.UID})
	if err != nil || !dismissed {
		t.Errorf("IsDuplicateMarkersDismissed(keeper) = %v, %v, want true", dismissed, err)
	}

	// The keeper inherited the fields it had none of; nothing points at the source.
	merged, err := env.people.GetSubjectByUID(ctx, keeper.UID)
	if err != nil {
		t.Fatalf("GetSubjectByUID(keeper): %v", err)
	}
	if !merged.Favorite || merged.Notes != "sister" ||
		merged.CoverPhotoUID == nil || *merged.CoverPhotoUID != p1 {
		t.Errorf("keeper after merge = %+v, want the source's favorite, notes and cover", merged)
	}
	for _, table := range []string{
		"markers", "faces", "face_rejections", "face_confirmations", "duplicate_marker_dismissals",
	} {
		if n := countBySubject(t, env, table, source.UID); n != 0 {
			t.Errorf("%s rows still referencing the merged-away subject = %d, want 0", table, n)
		}
	}

	rec := requireOneAudit(t, ctx, env.audit, audit.ActionSubjectMerge, actor, keeper.UID)
	if rec.Details["source_uid"] != source.UID {
		t.Errorf("audit details = %+v, want source_uid %s", rec.Details, source.UID)
	}
}

// TestMergeSubjects_samePhotoKeepsBothMarkers checks the same-photo conflict: when
// both people were marked on one photo, the merge keeps both markers rather than
// discarding either, and reports the photo so the repeated-marker review can pick
// it up. The dismissal the source carried for that photo is deliberately NOT
// moved — the group there is new, and nobody has judged it yet.
func TestMergeSubjects_samePhotoKeepsBothMarkers(t *testing.T) {
	env := newMergeEnv(t)
	ctx := t.Context()
	actor := makeUser(t, env.db, "usr_same", "same")

	shared := makePhoto(t, env.photos, "hash-shared")
	source := makeSubject(t, env.people, "Petr")
	keeper := makeSubject(t, env.people, "Petr K.")
	markPhoto(t, env.people, shared, source.UID)
	markPhoto(t, env.people, shared, keeper.UID)
	if err := env.feedback.DismissDuplicateMarkers(ctx,
		feedback.DuplicateMarkerDismissalKey{PhotoUID: shared, SubjectUID: source.UID},
		actorEntry(actor, audit.ActionDuplicateMarkerDismiss, "subjects", source.UID, nil)); err != nil {
		t.Fatalf("DismissDuplicateMarkers: %v", err)
	}

	res, err := env.people.MergeSubjectsAudited(ctx, source.UID, keeper.UID,
		mergeEntry(actor, source.UID, keeper.UID))
	if err != nil {
		t.Fatalf("MergeSubjectsAudited: %v", err)
	}
	if res.SharedPhotos != 1 || res.DismissalsMoved != 0 {
		t.Errorf("merge result = %+v, want 1 shared photo and no dismissal moved", res)
	}

	subjects := markerSubjects(t, env, shared)
	if len(subjects) != 2 || subjects[0] != keeper.UID || subjects[1] != keeper.UID {
		t.Errorf("markers on the shared photo = %v, want both naming %s", subjects, keeper.UID)
	}
	dismissed, err := env.feedback.IsDuplicateMarkersDismissed(ctx,
		feedback.DuplicateMarkerDismissalKey{PhotoUID: shared, SubjectUID: keeper.UID})
	if err != nil {
		t.Fatalf("IsDuplicateMarkersDismissed: %v", err)
	}
	if dismissed {
		t.Error("the repeated-marker group the merge created is already dismissed, want it reviewable")
	}
}

// TestMergeSubjects_conflictingFeedback checks the precedence rule: wherever the
// two sides disagree about one face, the positive record wins and the rejection
// is dropped — whichever side each came from.
func TestMergeSubjects_conflictingFeedback(t *testing.T) {
	env := newMergeEnv(t)
	ctx := t.Context()
	actor := makeUser(t, env.db, "usr_conflict", "conflict")

	assigned := makePhoto(t, env.photos, "hash-assigned")
	confirmed := makePhoto(t, env.photos, "hash-confirmed")
	source := makeSubject(t, env.people, "Jana")
	keeper := makeSubject(t, env.people, "Jana S.")

	// The source is assigned to a face the keeper says is not them.
	marker := markPhoto(t, env.people, assigned, source.UID)
	cacheFace(t, env.vectors, assigned, marker, source)
	if err := env.feedback.RejectFace(ctx,
		feedback.FaceRejectionKey{PhotoUID: assigned, FaceIndex: 0, SubjectUID: keeper.UID},
		actorEntry(actor, audit.ActionFaceReject, "subjects", keeper.UID, nil)); err != nil {
		t.Fatalf("RejectFace(keeper): %v", err)
	}
	// And the source says a face the keeper has confirmed is not them.
	if err := env.feedback.ConfirmFace(ctx,
		feedback.FaceConfirmationKey{PhotoUID: confirmed, FaceIndex: 0, SubjectUID: keeper.UID},
		actorEntry(actor, audit.ActionFaceConfirm, "subjects", keeper.UID, nil)); err != nil {
		t.Fatalf("ConfirmFace(keeper): %v", err)
	}
	if err := env.feedback.RejectFace(ctx,
		feedback.FaceRejectionKey{PhotoUID: confirmed, FaceIndex: 0, SubjectUID: source.UID},
		actorEntry(actor, audit.ActionFaceReject, "subjects", source.UID, nil)); err != nil {
		t.Fatalf("RejectFace(source): %v", err)
	}

	res, err := env.people.MergeSubjectsAudited(ctx, source.UID, keeper.UID,
		mergeEntry(actor, source.UID, keeper.UID))
	if err != nil {
		t.Fatalf("MergeSubjectsAudited: %v", err)
	}
	if res.RejectionsDropped != 2 || res.RejectionsMoved != 0 {
		t.Errorf("merge result = %+v, want both contradicted rejections dropped and none moved", res)
	}

	rejections, err := env.feedback.FaceRejectionsForSubject(ctx, keeper.UID)
	if err != nil {
		t.Fatalf("FaceRejectionsForSubject: %v", err)
	}
	if len(rejections) != 0 {
		t.Errorf("keeper rejections after merge = %+v, want none — both contradict an assignment", rejections)
	}
	// The positive records both survive.
	stillConfirmed, err := env.feedback.IsFaceConfirmed(ctx,
		feedback.FaceConfirmationKey{PhotoUID: confirmed, FaceIndex: 0, SubjectUID: keeper.UID})
	if err != nil || !stillConfirmed {
		t.Errorf("IsFaceConfirmed = %v, %v, want the keeper's confirmation kept", stillConfirmed, err)
	}
	if got := markerSubjects(t, env, assigned); len(got) != 1 || got[0] != keeper.UID {
		t.Errorf("markers on the assigned photo = %v, want [%s]", got, keeper.UID)
	}
}

// TestMergeSubjects_lifeYearsTravelAsAPair checks the merge's rule for the
// birth/death years: they fill a keeper that carries neither, and a keeper that
// already knows one of them keeps its own pair untouched. Filling them
// separately could pair one person's birth with another's death — which the
// death >= birth CHECK would reject, taking the whole merge with it.
func TestMergeSubjects_lifeYearsTravelAsAPair(t *testing.T) {
	env := newMergeEnv(t)
	ctx := t.Context()
	actor := makeUser(t, env.db, "usr_life", "life")

	dated := func(name string, birth, death *int) people.Subject {
		t.Helper()
		subj, err := env.people.CreateSubject(ctx, people.Subject{
			Name: name, BirthYear: birth, DeathYear: death,
		})
		if err != nil {
			t.Fatalf("CreateSubject %s: %v", name, err)
		}
		return subj
	}

	// The keeper knows nothing, the source knows both years: the pair moves.
	source := dated("Marie (dup)", new(1923), new(1998))
	keeper := dated("Marie", nil, nil)
	if _, err := env.people.MergeSubjectsAudited(ctx, source.UID, keeper.UID,
		mergeEntry(actor, source.UID, keeper.UID)); err != nil {
		t.Fatalf("MergeSubjectsAudited: %v", err)
	}
	filled, err := env.people.GetSubjectByUID(ctx, keeper.UID)
	if err != nil {
		t.Fatalf("GetSubjectByUID: %v", err)
	}
	if filled.BirthYear == nil || *filled.BirthYear != 1923 ||
		filled.DeathYear == nil || *filled.DeathYear != 1998 {
		t.Errorf("keeper years after the merge = %v/%v, want 1923/1998",
			filled.BirthYear, filled.DeathYear)
	}

	// The keeper knows when it was born; the source's death year must not be
	// grafted onto it, as it would claim a death six years before that birth.
	source2 := dated("Josef (dup)", new(1910), new(1929))
	keeper2 := dated("Josef", new(1935), nil)
	if _, err := env.people.MergeSubjectsAudited(ctx, source2.UID, keeper2.UID,
		mergeEntry(actor, source2.UID, keeper2.UID)); err != nil {
		t.Fatalf("MergeSubjectsAudited into a dated keeper: %v", err)
	}
	kept, err := env.people.GetSubjectByUID(ctx, keeper2.UID)
	if err != nil {
		t.Fatalf("GetSubjectByUID: %v", err)
	}
	if kept.BirthYear == nil || *kept.BirthYear != 1935 || kept.DeathYear != nil {
		t.Errorf("dated keeper's years after the merge = %v/%v, want 1935/nil",
			kept.BirthYear, kept.DeathYear)
	}
}

// TestMergeSubjects_rejected checks the two refusals — merging a subject into
// itself and naming a subject that does not exist — and that a refused merge
// changes nothing and writes no audit row.
func TestMergeSubjects_rejected(t *testing.T) {
	env := newMergeEnv(t)
	ctx := t.Context()
	actor := makeUser(t, env.db, "usr_bad", "bad")

	photo := makePhoto(t, env.photos, "hash-bad")
	source := makeSubject(t, env.people, "Eva")
	markPhoto(t, env.people, photo, source.UID)

	if _, err := env.people.MergeSubjectsAudited(ctx, source.UID, source.UID,
		mergeEntry(actor, source.UID, source.UID)); !errors.Is(err, people.ErrMergeIntoSelf) {
		t.Errorf("merging into itself: err = %v, want ErrMergeIntoSelf", err)
	}
	if _, err := env.people.MergeSubjectsAudited(ctx, source.UID, "su_missing",
		mergeEntry(actor, source.UID, "su_missing")); !errors.Is(err, people.ErrSubjectNotFound) {
		t.Errorf("merging into a missing subject: err = %v, want ErrSubjectNotFound", err)
	}

	if _, err := env.people.GetSubjectByUID(ctx, source.UID); err != nil {
		t.Errorf("subject after a refused merge: err = %v, want it untouched", err)
	}
	if n := countBySubject(t, env, "markers", source.UID); n != 1 {
		t.Errorf("markers after a refused merge = %d, want 1", n)
	}
	requireNoAudit(t, ctx, env.audit, audit.ActionSubjectMerge)
}
