package review

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

func TestAnswer_yesFaceCreatesMarker(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.faces.faces[vectors.FaceKey{PhotoUID: "photo1", FaceIndex: 0}] = vectors.Face{
			PhotoUID: "photo1", FaceIndex: 0, BBox: [4]float64{0.1, 0.2, 0.3, 0.4},
		}
	})
	id := faceQuestionID("photo1", 0, "subj1")
	res, err := f.svc.Answer(context.Background(), "user", id, AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if res.Result != resultAssigned || res.Answered != 1 {
		t.Errorf("result = %+v, want assigned with answered 1", res)
	}
	if len(f.assigner.reqs) != 1 {
		t.Fatalf("assigner calls = %d, want 1", len(f.assigner.reqs))
	}
	req := f.assigner.reqs[0]
	if req.Action != facematch.ActionCreateMarker || req.BBox == nil ||
		*req.BBox != [4]float64{0.1, 0.2, 0.3, 0.4} || req.SubjectUID != "subj1" {
		t.Errorf("assign request = %+v, want create_marker with the face's bbox", req)
	}
}

func TestAnswer_yesFaceTagsViaReview(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.faces.faces[vectors.FaceKey{PhotoUID: "photo1", FaceIndex: 0}] = vectors.Face{
			PhotoUID: "photo1", FaceIndex: 0, BBox: [4]float64{0.1, 0.2, 0.3, 0.4},
		}
	})
	id := faceQuestionID("photo1", 0, "subj1")
	if _, err := f.svc.Answer(context.Background(), "user", id, AnswerYes, audit.Meta{}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(f.assigner.reqs) != 1 {
		t.Fatalf("assigner calls = %d, want 1", len(f.assigner.reqs))
	}
	// The review path must tag the assignment so its face.assign audit row is
	// distinguishable from an ordinary recognition assignment.
	if got := f.assigner.reqs[0].Via; got != "review" {
		t.Errorf("assign request Via = %q, want review", got)
	}
}

func TestAnswer_yesFaceAssignsExistingMarker(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.faces.faces[vectors.FaceKey{PhotoUID: "photo1", FaceIndex: 1}] = vectors.Face{
			PhotoUID: "photo1", FaceIndex: 1, MarkerUID: new("marker1"),
		}
	})
	id := faceQuestionID("photo1", 1, "subj1")
	res, err := f.svc.Answer(context.Background(), "user", id, AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if res.Result != resultAssigned {
		t.Errorf("result = %q, want assigned", res.Result)
	}
	req := f.assigner.reqs[0]
	if req.Action != facematch.ActionAssignPerson || req.MarkerUID != "marker1" {
		t.Errorf("assign request = %+v, want assign_person on marker1", req)
	}
}

func TestAnswer_yesFaceAlreadyAssignedShortCircuits(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.faces.faces[vectors.FaceKey{PhotoUID: "photo1", FaceIndex: 0}] = vectors.Face{
			PhotoUID: "photo1", FaceIndex: 0, SubjectUID: new("subj1"),
		}
	})
	id := faceQuestionID("photo1", 0, "subj1")
	res, err := f.svc.Answer(context.Background(), "user", id, AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if res.Result != resultAssigned || len(f.assigner.reqs) != 0 {
		t.Errorf("result = %q with %d assigner calls, want assigned with 0 calls",
			res.Result, len(f.assigner.reqs))
	}
}

func TestAnswer_yesLabelAttaches(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id := labelQuestionID("photo1", "lab1")
	res, err := f.svc.Answer(context.Background(), "user", id, AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if res.Result != resultLabeled {
		t.Errorf("result = %q, want labeled", res.Result)
	}
	if len(f.organize.attached) != 1 || f.organize.attached[0] != "photo1/lab1" {
		t.Errorf("attached = %v, want [photo1/lab1]", f.organize.attached)
	}
}

func TestAnswer_noRecordsRejections(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.faces.faces[vectors.FaceKey{PhotoUID: "photo1", FaceIndex: 0}] = vectors.Face{}
	})
	ctx := context.Background()
	if _, err := f.svc.Answer(ctx, "user", faceQuestionID("photo1", 0, "subj1"), AnswerNo, audit.Meta{}); err != nil {
		t.Fatalf("Answer face no: %v", err)
	}
	if _, err := f.svc.Answer(ctx, "user", labelQuestionID("photo2", "lab1"), AnswerNo, audit.Meta{}); err != nil {
		t.Fatalf("Answer label no: %v", err)
	}
	if len(f.feedback.faceRejects) != 1 || f.feedback.faceRejects[0].SubjectUID != "subj1" {
		t.Errorf("face rejections = %+v, want one for subj1", f.feedback.faceRejects)
	}
	if len(f.feedback.labelRejects) != 1 || f.feedback.labelRejects[0].LabelUID != "lab1" {
		t.Errorf("label rejections = %+v, want one for lab1", f.feedback.labelRejects)
	}
	if len(f.assigner.reqs) != 0 || len(f.organize.attached) != 0 {
		t.Error("no answers must not assign or attach anything")
	}
}

func TestAnswer_skipShelvesWithoutWrites(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.sweeper.events = []sweep.Event{personEvent("subj1", 0.4, 0.41), summaryEvent(1)}
	})
	ctx := context.Background()
	first, err := f.svc.Queue(ctx, "user", 1)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	skippedID := first.Questions[0].ID
	res, err := f.svc.Answer(ctx, "user", skippedID, AnswerSkip, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer skip: %v", err)
	}
	if res.Result != resultSkipped || res.Answered != 0 || res.Remaining != 1 {
		t.Errorf("skip result = %+v, want skipped with answered 0 remaining 1", res)
	}
	if len(f.feedback.faceRejects) != 0 {
		t.Error("skip must not record a rejection")
	}
	next, err := f.svc.Queue(ctx, "user", 10)
	if err != nil {
		t.Fatalf("Queue after skip: %v", err)
	}
	for _, q := range next.Questions {
		if q.ID == skippedID {
			t.Fatal("skipped question reappeared in the next batch")
		}
	}
	// The skip also survives a rebuild within the session.
	*f.now = f.now.Add(2 * DefaultCacheTTL)
	rebuilt, err := f.svc.Queue(ctx, "user", 10)
	if err != nil {
		t.Fatalf("Queue after rebuild: %v", err)
	}
	for _, q := range rebuilt.Questions {
		if q.ID == skippedID {
			t.Fatal("skipped question reappeared after a rebuild")
		}
	}
}

func TestAnswer_idempotent(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.faces.faces[vectors.FaceKey{PhotoUID: "photo1", FaceIndex: 0}] = vectors.Face{
			PhotoUID: "photo1", FaceIndex: 0, BBox: [4]float64{0.1, 0.1, 0.2, 0.2},
		}
	})
	ctx := context.Background()
	id := faceQuestionID("photo1", 0, "subj1")
	if _, err := f.svc.Answer(ctx, "user", id, AnswerYes, audit.Meta{}); err != nil {
		t.Fatalf("first Answer: %v", err)
	}
	res, err := f.svc.Answer(ctx, "user", id, AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("second Answer: %v", err)
	}
	if res.Result != resultAlreadyAnswered || res.Answered != 1 {
		t.Errorf("second answer = %+v, want already_answered with answered still 1", res)
	}
	if len(f.assigner.reqs) != 1 {
		t.Errorf("assigner calls = %d, want 1 (no double assign)", len(f.assigner.reqs))
	}
}

func TestAnswer_goneTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*fixture)
		id     string
		answer Answer
	}{
		{
			name:   "face vanished before yes",
			mutate: nil, // fakeFaces map empty → no face row
			id:     faceQuestionID("photo1", 0, "subj1"),
			answer: AnswerYes,
		},
		{
			name: "subject vanished during assign",
			mutate: func(f *fixture) {
				f.faces.faces[vectors.FaceKey{PhotoUID: "photo1", FaceIndex: 0}] = vectors.Face{}
				f.assigner.err = people.ErrSubjectNotFound
			},
			id:     faceQuestionID("photo1", 0, "subj1"),
			answer: AnswerYes,
		},
		{
			name:   "label vanished before yes",
			mutate: func(f *fixture) { f.organize.attachErr = organize.ErrLabelNotFound },
			id:     labelQuestionID("photo1", "lab1"),
			answer: AnswerYes,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, tt.mutate)
			res, err := f.svc.Answer(context.Background(), "user", tt.id, tt.answer, audit.Meta{})
			if err != nil {
				t.Fatalf("Answer: %v (gone must not be an error)", err)
			}
			if res.Result != resultGone || res.Answered != 0 {
				t.Errorf("result = %+v, want gone and uncounted", res)
			}
		})
	}
}

func TestAnswer_invalidInput(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	ctx := context.Background()
	if _, err := f.svc.Answer(ctx, "user", "bogus-id", AnswerYes, audit.Meta{}); !errors.Is(err, ErrInvalidQuestion) {
		t.Errorf("bogus id error = %v, want ErrInvalidQuestion", err)
	}
	id := labelQuestionID("photo1", "lab1")
	if _, err := f.svc.Answer(ctx, "user", id, Answer("maybe"), audit.Meta{}); !errors.Is(err, ErrInvalidAnswer) {
		t.Errorf("bogus answer error = %v, want ErrInvalidAnswer", err)
	}
}

func TestAnswer_updatesQueueCounters(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.sweeper.events = []sweep.Event{personEvent("subj1", 0.4, 0.41), summaryEvent(1)}
		f.faces.faces[vectors.FaceKey{PhotoUID: "photo-subj1-0", FaceIndex: 0}] = vectors.Face{}
		f.faces.faces[vectors.FaceKey{PhotoUID: "photo-subj1-1", FaceIndex: 0}] = vectors.Face{}
	})
	ctx := context.Background()
	first, err := f.svc.Queue(ctx, "user", 10)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if first.Remaining != 2 {
		t.Fatalf("remaining = %d, want 2", first.Remaining)
	}
	answered := first.Questions[0].ID
	res, err := f.svc.Answer(ctx, "user", answered, AnswerYes, audit.Meta{})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if res.Answered != 1 || res.Remaining != 1 {
		t.Errorf("after answer: %+v, want answered 1 remaining 1", res)
	}
	next, err := f.svc.Queue(ctx, "user", 10)
	if err != nil {
		t.Fatalf("Queue after answer: %v", err)
	}
	if next.Answered != 1 || next.Remaining != 1 || len(next.Questions) != 1 {
		t.Errorf("next batch = answered %d remaining %d len %d, want 1/1/1",
			next.Answered, next.Remaining, len(next.Questions))
	}
	if next.Questions[0].ID == answered {
		t.Error("answered question reappeared in the next batch")
	}
}
