package review

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/vectors"
)

// TestAnswer_foreignTargetIsNotFound answers questions naming a photo, subject
// or label of another project and expects ErrQuestionNotFound with nothing
// written anywhere — not the catalogue, not the feedback, not the skip memory.
func TestAnswer_foreignTargetIsNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      string
		answer  Answer
		foreign string
	}{
		{name: "face yes, foreign photo", id: faceQuestionID("photoB", 0, "subj1"), answer: AnswerYes, foreign: "photoB"},
		{name: "face no, foreign subject", id: faceQuestionID("photo1", 0, "subjB"), answer: AnswerNo, foreign: "subjB"},
		{name: "face skip, foreign subject", id: faceQuestionID("photo1", 0, "subjB"), answer: AnswerSkip, foreign: "subjB"},
		{name: "label yes, foreign label", id: labelQuestionID("photo1", "labB"), answer: AnswerYes, foreign: "labB"},
		{name: "duplicate yes, foreign pair half", id: duplicateQuestionID("photo1", "photoB"), answer: AnswerYes,
			foreign: "photoB"},
		{name: "outlier no, foreign subject", id: outlierQuestionID("photo1", 0, "subjB"), answer: AnswerNo,
			foreign: "subjB"},
		{name: "place yes, foreign photo", id: placeQuestionID("photoB"), answer: AnswerYes, foreign: "photoB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, func(f *fixture) {
				f.scope.foreign = map[string]bool{tt.foreign: true}
				f.skips = &fakeSkips{}
				f.faces.faces[vectors.FaceKey{PhotoUID: "photo1", FaceIndex: 0}] = vectors.Face{
					PhotoUID: "photo1", FaceIndex: 0, MarkerUID: new("mk1"),
				}
			})
			_, err := f.svc.Answer(context.Background(), testProject, "user", tt.id, tt.answer, audit.Meta{})
			if !errors.Is(err, ErrQuestionNotFound) {
				t.Fatalf("Answer = %v, want ErrQuestionNotFound", err)
			}
			if n := len(f.assigner.reqs) + len(f.organize.attached) + len(f.feedback.faceRejects) +
				len(f.feedback.faceConfirms) + len(f.feedback.dupConfirms) + len(f.feedback.labelRejects) +
				len(f.skips.recorded); n != 0 {
				t.Errorf("%d writes for a foreign question, want none", n)
			}
		})
	}
}

// TestAnswer_missingTargetIsTheSameNotFound proves a uid that exists nowhere is
// answered exactly like one of another project.
func TestAnswer_missingTargetIsTheSameNotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) { f.scope.foreign = map[string]bool{"nowhere": true} })
	_, err := f.svc.Answer(context.Background(), testProject, "user",
		faceQuestionID("nowhere", 0, "subj1"), AnswerYes, audit.Meta{})
	if !errors.Is(err, ErrQuestionNotFound) {
		t.Errorf("Answer = %v, want ErrQuestionNotFound", err)
	}
}

// TestAnswer_appliesInTheProject hands the caller's project to the write path.
func TestAnswer_appliesInTheProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.faces.faces[vectors.FaceKey{PhotoUID: "photo1", FaceIndex: 0}] = vectors.Face{PhotoUID: "photo1"}
	})
	if _, err := f.svc.Answer(context.Background(), testProject, "user",
		faceQuestionID("photo1", 0, "subj1"), AnswerYes, audit.Meta{}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(f.assigner.projects) != 1 || f.assigner.projects[0] != testProject {
		t.Errorf("assigned in %v, want [%s]", f.assigner.projects, testProject)
	}
}

// TestService_noProject refuses a queue or an answer with no project rather
// than serving the whole instance.
func TestService_noProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	if _, err := f.svc.Queue(context.Background(), "", "user", SourceBoth, 0); !errors.Is(err, ErrNoProject) {
		t.Errorf("Queue = %v, want ErrNoProject", err)
	}
	if _, err := f.svc.Answer(context.Background(), "", "user",
		faceQuestionID("photo1", 0, "subj1"), AnswerYes, audit.Meta{}); !errors.Is(err, ErrNoProject) {
		t.Errorf("Answer = %v, want ErrNoProject", err)
	}
}

// TestSession_perProject keeps one user's games in two projects apart.
func TestSession_perProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	a, b := f.svc.session("pj_a", "user"), f.svc.session("pj_b", "user")
	if a == b {
		t.Fatal("one session for two projects")
	}
	if a.project != "pj_a" || b.project != "pj_b" {
		t.Errorf("session projects = %q/%q, want pj_a/pj_b", a.project, b.project)
	}
	if again := f.svc.session("pj_a", "user"); again != a {
		t.Error("the same (project, user) got a fresh session")
	}
}

// TestCursors_perProject advances one project's rotation without moving
// another's.
func TestCursors_perProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	f.svc.advance("pj_a", faceCursor, 7)
	if got := f.svc.offset("pj_a", faceCursor); got != 7 {
		t.Errorf("pj_a face cursor = %d, want 7", got)
	}
	if got := f.svc.offset("pj_b", faceCursor); got != 0 {
		t.Errorf("pj_b face cursor = %d, want 0: another project's rebuild moved it", got)
	}
	if got := f.svc.offset("pj_a", labelCursor); got != 0 {
		t.Errorf("pj_a label cursor = %d, want 0: the face rotation moved it", got)
	}
}

// TestLabelPlan_dropsForeignLabels keeps another project's label out of the
// rotation and out of the total.
func TestLabelPlan_dropsForeignLabels(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.organize.labels = []organize.LabelCount{labelCount("labA", 3), labelCount("labB", 5)}
		f.scope.foreign = map[string]bool{"labB": true}
	})
	labels, total, err := f.svc.labelPlan(context.Background(), testProject)
	if err != nil {
		t.Fatalf("labelPlan: %v", err)
	}
	if total != 1 || len(labels) != 1 || labels[0].UID != "labA" {
		t.Errorf("plan = %v (total %d), want only labA", labels, total)
	}
}
