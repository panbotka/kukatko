//go:build integration

package review_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/review"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These are the tenant-isolation tests of the review game. Both seeded projects
// get the same scene with IDENTICAL vectors — the seeded person tagged on the
// seeded photo, plus a second photo with one unnamed face sitting in the
// uncertainty band — so the other project's face is always exactly as good a
// question as the project's own.

// plantBandCandidates gives each tenant the scene described above and returns
// the uid of the photo carrying the unnamed face, keyed by project uid.
func plantBandCandidates(t *testing.T, pool *pgxpool.Pool, f *projecttest.Fixture) map[string]string {
	t.Helper()
	faces := vectors.NewStore(pool)
	out := make(map[string]string, 2)
	for _, tn := range []projecttest.Tenant{f.A, f.B} {
		subject, marker := tn.SubjectUID, tn.MarkerUID
		if err := faces.SaveFaces(t.Context(), tn.PhotoUID, []vectors.Face{{
			FaceIndex: 0, Vector: vec(map[int]float32{0: 1}), BBox: [4]float64{0.25, 0.25, 0.2, 0.2},
			DetScore: 0.95, SubjectUID: &subject, MarkerUID: &marker,
		}}); err != nil {
			t.Fatalf("SaveFaces exemplar %s: %v", tn.Slug, err)
		}
		uid := projecttest.MissingUID("ph")
		sum := sha256.Sum256([]byte(uid))
		if _, err := pool.Exec(t.Context(), `INSERT INTO photos (uid, project_id, file_hash, file_path,
			file_name, file_size, file_mime, file_width, file_height, taken_at)
			VALUES ($1, $2, $3, $4, $5, 1024, 'image/jpeg', 1000, 800, '2026-06-02T12:00:00Z')`,
			uid, tn.ProjectUID, hex.EncodeToString(sum[:]), "2026/10/"+uid+".jpg", uid+".jpg"); err != nil {
			t.Fatalf("inserting a photo into %s: %v", tn.Slug, err)
		}
		if err := faces.SaveFaces(t.Context(), uid, []vectors.Face{{
			FaceIndex: 0, Vector: vec(map[int]float32{0: 0.6, 2: 0.8}), BBox: reviewableBox, DetScore: 0.9,
		}}); err != nil {
			t.Fatalf("SaveFaces candidate %s: %v", tn.Slug, err)
		}
		out[tn.ProjectUID] = uid
	}
	return out
}

// isolationService composes the review game over the real stores of pool,
// asking about faces and outliers only, with scope as the proof of an answer's
// targets (the real review.NewScopeStore unless a test replaces it).
func isolationService(pool *pgxpool.Pool, scope review.Scope) *review.Service {
	faces := vectors.NewStore(pool)
	peopleStore := people.NewStore(pool)
	photoStore := photos.NewStore(pool)
	feedbackStore := feedback.NewStore(pool)
	organizeStore := organize.NewStore(pool)
	cand := candidates.New(candidates.Config{
		Faces: faces, People: peopleStore, Feedback: feedbackStore, Photos: photoStore,
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 1000, MinFacePx: 32, Concurrency: 2, MinFaceRel: 0.02,
	})
	return review.New(review.Config{
		Sweeper: sweep.New(sweep.Config{Subjects: peopleStore, Finder: cand, Concurrency: 2}),
		Expander: expand.New(expand.Config{
			Vectors: faces, Organize: organizeStore, Feedback: feedbackStore, Photos: photoStore,
			Media: mediaurl.NewBuilder(nil),
		}),
		Organize: organizeStore,
		Faces:    faces,
		Feedback: feedbackStore,
		Assigner: facematch.New(facematch.Config{Photos: photoStore, Faces: faces, People: peopleStore}),
		Scope:    scope,
		Outliers: outliers.New(outliers.Config{Faces: faces, People: peopleStore, Feedback: feedbackStore}),
		Subjects: peopleStore,
		Photos:   photoStore,
		KindShares: map[review.Kind]float64{
			review.KindFace: 1, review.KindOutlier: 1,
		},
		BandMin: 0.45, BandMax: 0.75,
	})
}

// TestQueue_neverCrossesProjects builds the same player's queue in each project
// and asserts every question is about the project's own photos and people —
// A's queue never holds B's face, nor B's queue A's — and that each project's
// band candidate is asked about in its own queue.
func TestQueue_neverCrossesProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	candidatesOf := plantBandCandidates(t, pool, f)
	svc := isolationService(pool, review.NewScopeStore(pool))

	queue := func(projectUID string) ([]string, error) {
		res, err := svc.Queue(t.Context(), projectUID, f.A.UserUID, review.SourcePeople, 100)
		var out []string
		for _, q := range res.Questions {
			out = append(out, q.Photo.UID)
			if q.Subject != nil {
				out = append(out, q.Subject.UID)
			}
		}
		return out, err
	}
	f.AssertStoreScoped(t, queue)
	for _, tn := range []projecttest.Tenant{f.A, f.B} {
		res, err := svc.Queue(t.Context(), tn.ProjectUID, f.A.UserUID, review.SourcePeople, 100)
		if err != nil {
			t.Fatalf("Queue in %s: %v", tn.Slug, err)
		}
		own := map[string]bool{tn.PhotoUID: true, candidatesOf[tn.ProjectUID]: true}
		asked := false
		for _, q := range res.Questions {
			if !own[q.Photo.UID] || q.Photo.ProjectID != tn.ProjectUID {
				t.Errorf("queue in %s asks about photo %s of project %s", tn.Slug, q.Photo.UID, q.Photo.ProjectID)
			}
			if q.Subject != nil && q.Subject.UID != tn.SubjectUID {
				t.Errorf("queue in %s asks about person %s", tn.Slug, q.Subject.UID)
			}
			asked = asked || q.Photo.UID == candidatesOf[tn.ProjectUID]
		}
		if !asked {
			t.Errorf("queue in %s = %+v, want its own band candidate asked about", tn.Slug, res.Questions)
		}
	}
	if _, err := svc.Queue(t.Context(), "", f.A.UserUID, review.SourcePeople, 100); !errors.Is(err, review.ErrNoProject) {
		t.Errorf("Queue without a project = %v, want review.ErrNoProject", err)
	}
}

// everythingInProject is a Scope that vouches for every uid in every project —
// the up-front proof gone wrong. With it nothing stands between an answer and
// the face read but the read's own project predicate.
type everythingInProject struct{}

// all answers every uid as belonging to the project.
func all(uids []string) map[string]bool {
	out := make(map[string]bool, len(uids))
	for _, uid := range uids {
		out[uid] = true
	}
	return out
}

// PhotosInProject vouches for every photo.
func (everythingInProject) PhotosInProject(_ context.Context, _ string, uids []string) (map[string]bool, error) {
	return all(uids), nil
}

// SubjectsInProject vouches for every subject.
func (everythingInProject) SubjectsInProject(_ context.Context, _ string, uids []string) (map[string]bool, error) {
	return all(uids), nil
}

// LabelsInProject vouches for every label.
func (everythingInProject) LabelsInProject(_ context.Context, _ string, uids []string) (map[string]bool, error) {
	return all(uids), nil
}

// TestAnswer_foreignFaceReadIsScopedEvenWithoutTheProof has B's player answer
// about A's real faces — an unnamed one and one already carrying A's person —
// with the question-level proof replaced by one that vouches for anything. The
// face is still read inside B, so every answer is exactly what a question about
// a photo that exists nowhere gets — "gone" — where an unscoped read would have
// told B that face 2 exists and already belongs to A's person ("assigned"). A's
// faces keep their identities and no marker is drawn on A's photo: the review
// game's face read no longer depends on the order its caller checks things in.
func TestAnswer_foreignFaceReadIsScopedEvenWithoutTheProof(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	if err := vectors.NewStore(pool).SaveFaces(t.Context(), f.A.PhotoUID, []vectors.Face{
		{FaceIndex: 1, Vector: vec(map[int]float32{3: 1}), BBox: reviewableBox, DetScore: 0.9},
		{
			FaceIndex: 2, Vector: vec(map[int]float32{4: 1}), BBox: [4]float64{0.6, 0.1, 0.2, 0.2}, DetScore: 0.9,
			SubjectUID: &f.A.SubjectUID, MarkerUID: &f.A.MarkerUID,
		},
	}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}
	svc := isolationService(pool, everythingInProject{})
	missing := projecttest.MissingUID("ph")

	for _, tt := range []struct {
		name    string
		kind    string
		index   int
		subject string
		answer  review.Answer
	}{
		{name: "yes about A's unnamed face", kind: "face", index: 1, subject: f.B.SubjectUID, answer: review.AnswerYes},
		{name: "yes guessing A's named face", kind: "face", index: 2, subject: f.A.SubjectUID, answer: review.AnswerYes},
		{name: "outlier no about A's named face", kind: "outlier", index: 2, subject: f.A.SubjectUID, answer: review.AnswerNo},
	} {
		foreign, err := svc.Answer(t.Context(), f.B.ProjectUID, f.B.UserUID,
			fmt.Sprintf("%s:%s:%d:%s", tt.kind, f.A.PhotoUID, tt.index, tt.subject), tt.answer, reviewMeta)
		if err != nil {
			t.Fatalf("%s from B: %v", tt.name, err)
		}
		nowhere, err := svc.Answer(t.Context(), f.B.ProjectUID, f.B.UserUID,
			fmt.Sprintf("%s:%s:%d:%s", tt.kind, missing, tt.index, tt.subject), tt.answer, reviewMeta)
		if err != nil {
			t.Fatalf("%s about a missing photo: %v", tt.name, err)
		}
		if foreign.Result != "gone" || foreign.Result != nowhere.Result {
			t.Errorf("%s from B = %q, a missing photo = %q; want both \"gone\"", tt.name, foreign.Result, nowhere.Result)
		}
	}

	var markers, unnamedAssigned, namedKept int
	if err := pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM markers WHERE photo_uid = $1 AND uid <> $2),
		(SELECT count(*) FROM faces WHERE photo_uid = $1 AND face_index = 1 AND subject_uid IS NOT NULL),
		(SELECT count(*) FROM faces WHERE photo_uid = $1 AND face_index = 2 AND subject_uid = $3)`,
		f.A.PhotoUID, f.A.MarkerUID, f.A.SubjectUID).Scan(&markers, &unnamedAssigned, &namedKept); err != nil {
		t.Fatalf("counting A's markers and faces: %v", err)
	}
	if markers != 0 || unnamedAssigned != 0 || namedKept != 1 {
		t.Errorf("after B's answers A's photo has %d new markers, unnamed face assigned %d, named face kept %d; "+
			"want 0, 0, 1", markers, unnamedAssigned, namedKept)
	}
	var subjectOfMarker *string
	if err := pool.QueryRow(t.Context(), "SELECT subject_uid FROM markers WHERE uid = $1",
		f.A.MarkerUID).Scan(&subjectOfMarker); err != nil {
		t.Fatalf("reading A's marker: %v", err)
	}
	if subjectOfMarker == nil || *subjectOfMarker != f.A.SubjectUID {
		t.Errorf("A's marker names %v after B's answers, want A's person %s still", subjectOfMarker, f.A.SubjectUID)
	}
}
