//go:build integration

package jobs_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
)

// projectPayload builds a library-wide job payload naming a project, plus any
// extra fields.
func projectPayload(t *testing.T, projectUID string, extra map[string]string) json.RawMessage {
	t.Helper()
	fields := map[string]string{"project_uid": projectUID}
	for k, v := range extra {
		fields[k] = v
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshaling payload: %v", err)
	}
	return raw
}

// TestEnqueueDedup_faceClusterIsPerProjectWhileQueued verifies migration
// 0105's face_cluster index and the per-project pending count: one queued pass
// per project, another project's pass is not a duplicate, a running pass does
// not block its successor, and a legacy payload naming no project never
// collides with anything.
func TestEnqueueDedup_faceClusterIsPerProjectWhileQueued(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypeFaceCluster, projectPayload(t, "pj_a", nil),
		jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("first face_cluster for A: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeFaceCluster, projectPayload(t, "pj_a", nil),
		jobs.EnqueueOptions{}); !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("second queued face_cluster for A = %v, want ErrDuplicate", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeFaceCluster, projectPayload(t, "pj_b", nil),
		jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("face_cluster for B = %v, want success", err)
	}
	for range 2 {
		if _, err := store.Enqueue(ctx, jobs.TypeFaceCluster, json.RawMessage(`{"recluster":true}`),
			jobs.EnqueueOptions{}); err != nil {
			t.Fatalf("legacy face_cluster with no project = %v, want success", err)
		}
	}

	for project, want := range map[string]int{"pj_a": 1, "pj_b": 1, "pj_c": 0, "": 0} {
		got, err := store.CountPendingForProject(ctx, project, jobs.TypeFaceCluster)
		if err != nil {
			t.Fatalf("CountPendingForProject(%q): %v", project, err)
		}
		if got != want {
			t.Errorf("pending face_cluster for %q = %d, want %d", project, got, want)
		}
	}

	claimed, err := store.Claim(ctx, "w1", jobs.TypeFaceCluster)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	var running struct {
		ProjectUID string `json:"project_uid"`
	}
	if err := json.Unmarshal(claimed.Payload, &running); err != nil {
		t.Fatalf("decoding the claimed payload: %v", err)
	}
	if running.ProjectUID != "" {
		if _, err := store.Enqueue(ctx, jobs.TypeFaceCluster, projectPayload(t, running.ProjectUID, nil),
			jobs.EnqueueOptions{}); err != nil {
			t.Fatalf("successor of the running pass = %v, want success", err)
		}
	}
}

// TestEnqueueDedup_namelessIsPerProjectAndSubject verifies migration 0105's
// nameless index: the same subject's detach is queued once per project, while
// another subject, another project or the other job type is not a duplicate.
func TestEnqueueDedup_namelessIsPerProjectAndSubject(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()
	enqueue := func(jobType, project, subject string) error {
		_, err := store.Enqueue(ctx, jobType, projectPayload(t, project, map[string]string{"subject_uid": subject}),
			jobs.EnqueueOptions{})
		return err
	}
	if err := enqueue(jobs.TypeNamelessDetach, "pj_a", "su_1"); err != nil {
		t.Fatalf("first detach: %v", err)
	}
	if err := enqueue(jobs.TypeNamelessDetach, "pj_a", "su_1"); !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("second queued detach of the same subject = %v, want ErrDuplicate", err)
	}
	for _, other := range [][3]string{
		{jobs.TypeNamelessDetach, "pj_a", "su_2"},
		{jobs.TypeNamelessDetach, "pj_b", "su_1"},
		{jobs.TypeNamelessRestore, "pj_a", "su_1"},
	} {
		if err := enqueue(other[0], other[1], other[2]); err != nil {
			t.Errorf("enqueue %v = %v, want success", other, err)
		}
	}
}
