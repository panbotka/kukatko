package namelessjob

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/people"
)

// fakeStore is a stub Store recording the audited calls the handlers made.
type fakeStore struct {
	subjects    []people.NamelessSubject
	listErr     error
	snapshots   map[string]people.SubjectSnapshot
	snapshotErr error
	detachErr   error
	restoreErr  error
	detached    []string
	restored    []people.SubjectSnapshot
	entries     []audit.Entry
}

func (f *fakeStore) ListNamelessSubjects(context.Context) ([]people.NamelessSubject, error) {
	return f.subjects, f.listErr
}

func (f *fakeStore) SnapshotSubject(_ context.Context, uid string) (people.SubjectSnapshot, error) {
	if f.snapshotErr != nil {
		return people.SubjectSnapshot{}, f.snapshotErr
	}
	snap, ok := f.snapshots[uid]
	if !ok {
		return people.SubjectSnapshot{}, people.ErrSubjectNotFound
	}
	return snap, nil
}

func (f *fakeStore) DetachSubject(
	_ context.Context, uid string, entry audit.Entry,
) (people.SubjectSnapshot, error) {
	if f.detachErr != nil {
		return people.SubjectSnapshot{}, f.detachErr
	}
	f.detached = append(f.detached, uid)
	f.entries = append(f.entries, entry)
	return f.snapshots[uid], nil
}

func (f *fakeStore) RestoreSubject(
	_ context.Context, snap people.SubjectSnapshot, entry audit.Entry,
) (people.Subject, error) {
	if f.restoreErr != nil {
		return people.Subject{}, f.restoreErr
	}
	f.restored = append(f.restored, snap)
	f.entries = append(f.entries, entry)
	return snap.Subject, nil
}

// fakeQueue is a stub Queue capturing what was enqueued.
type fakeQueue struct {
	enqueued []jobs.Job
	err      error
}

func (q *fakeQueue) Enqueue(
	_ context.Context, jobType string, payload json.RawMessage, _ jobs.EnqueueOptions,
) (jobs.Job, error) {
	if q.err != nil {
		return jobs.Job{}, q.err
	}
	job := jobs.Job{ID: int64(len(q.enqueued) + 1), Type: jobType, Payload: payload}
	q.enqueued = append(q.enqueued, job)
	return job, nil
}

// fixture returns a service over one nameless catch-all subject owning two
// markers and two cached faces, plus its stubs.
func fixture() (*Service, *fakeStore, *fakeQueue) {
	subj := people.Subject{
		UID: "sunuikf1e9jdpjog5qgomsvgrb", Slug: "subject", Name: "", Type: people.SubjectPerson,
		CreatedAt: time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
	}
	snap := people.SubjectSnapshot{
		Subject:    subj,
		MarkerUIDs: []string{"mrk1", "mrk2"},
		Faces:      []people.FaceRef{{PhotoUID: "pho1"}, {PhotoUID: "pho2", FaceIndex: 1}},
	}
	store := &fakeStore{
		subjects:  []people.NamelessSubject{{Subject: subj, MarkerCount: 2, FaceCount: 2}},
		snapshots: map[string]people.SubjectSnapshot{subj.UID: snap},
	}
	queue := &fakeQueue{}
	return New(store, queue, nil), store, queue
}

// TestSnapshotReadsWithoutChanging verifies the snapshot the operator downloads
// is the full undo — subject, markers and faces — and that taking it detaches
// nothing.
func TestSnapshotReadsWithoutChanging(t *testing.T) {
	t.Parallel()
	svc, store, queue := fixture()

	undo, err := svc.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(undo.Subjects) != 1 {
		t.Fatalf("undo covers %d subject(s), want 1", len(undo.Subjects))
	}
	if got := undo.Subjects[0]; len(got.MarkerUIDs) != 2 || len(got.Faces) != 2 {
		t.Errorf("snapshot = %d markers / %d faces, want 2 / 2", len(got.MarkerUIDs), len(got.Faces))
	}
	if len(store.detached) != 0 || len(queue.enqueued) != 0 {
		t.Errorf("Snapshot changed something: detached %v, enqueued %d", store.detached, len(queue.enqueued))
	}
}

// TestSnapshotSkipsVanishedSubject verifies a subject that disappears between the
// listing and its snapshot is skipped rather than failing the whole read — a
// concurrent CLI run getting there first is a success.
func TestSnapshotSkipsVanishedSubject(t *testing.T) {
	t.Parallel()
	svc, store, _ := fixture()
	store.snapshots = map[string]people.SubjectSnapshot{}

	undo, err := svc.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(undo.Subjects) != 0 {
		t.Errorf("undo covers %d subject(s), want none", len(undo.Subjects))
	}
}

// TestEnqueueDetachCarriesTheUndoCounts verifies the scheduled job names the
// subject and the counts the delivered undo file recorded.
func TestEnqueueDetachCarriesTheUndoCounts(t *testing.T) {
	t.Parallel()
	svc, _, queue := fixture()
	undo, err := svc.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	queued, err := svc.EnqueueDetach(t.Context(), undo, audit.Meta{ActorUID: "usr1", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("EnqueueDetach: %v", err)
	}
	if queued != 1 || len(queue.enqueued) != 1 {
		t.Fatalf("queued %d job(s) (%d recorded), want 1", queued, len(queue.enqueued))
	}
	job := queue.enqueued[0]
	if job.Type != jobs.TypeNamelessDetach {
		t.Errorf("job type = %q, want %q", job.Type, jobs.TypeNamelessDetach)
	}
	var p detachPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if p.SubjectUID != undo.Subjects[0].Subject.UID || p.MarkerCount != 2 || p.FaceCount != 2 {
		t.Errorf("payload = %+v, want the subject with 2 markers / 2 faces", p)
	}
	if p.Actor.UID != "usr1" || p.Actor.IP != "10.0.0.1" {
		t.Errorf("payload actor = %+v, want the scheduling maintainer", p.Actor)
	}
}

// TestEnqueueRestoreRejectsUidlessSnapshot verifies an undo file whose snapshot
// names no subject is refused rather than queued to fail later in the worker.
func TestEnqueueRestoreRejectsUidlessSnapshot(t *testing.T) {
	t.Parallel()
	svc, _, queue := fixture()

	if _, err := svc.EnqueueRestore(t.Context(),
		Undo{Subjects: []people.SubjectSnapshot{{}}}, audit.Meta{}); err == nil {
		t.Fatal("EnqueueRestore accepted a snapshot with no subject uid")
	}
	if len(queue.enqueued) != 0 {
		t.Errorf("enqueued %d job(s) for an unusable snapshot, want none", len(queue.enqueued))
	}
}

// TestHandleDetachDetachesAndAudits verifies the job reuses the store's audited
// detach and stamps the scheduling maintainer onto the audit entry.
func TestHandleDetachDetachesAndAudits(t *testing.T) {
	t.Parallel()
	svc, store, queue := fixture()
	undo, err := svc.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := svc.EnqueueDetach(t.Context(), undo, audit.Meta{ActorUID: "usr1"}); err != nil {
		t.Fatalf("EnqueueDetach: %v", err)
	}

	if err := svc.HandleDetach(t.Context(), queue.enqueued[0]); err != nil {
		t.Fatalf("HandleDetach: %v", err)
	}
	if len(store.detached) != 1 || store.detached[0] != undo.Subjects[0].Subject.UID {
		t.Fatalf("detached %v, want the nameless subject", store.detached)
	}
	entry := store.entries[0]
	if entry.Action != audit.ActionSubjectDelete || entry.TargetType != "subject" {
		t.Errorf("audit entry = %s/%s, want subject.delete on a subject", entry.Action, entry.TargetType)
	}
	if entry.ActorUID != "usr1" {
		t.Errorf("audit actor = %q, want usr1", entry.ActorUID)
	}
	if entry.Details["markers"] != 2 || entry.Details["reason"] != detachReason {
		t.Errorf("audit details = %v, want the marker count and the reason", entry.Details)
	}
}

// TestHandleDetachIsIdempotent verifies a subject that is already gone completes
// the job instead of failing it, so a retry or a double-click is harmless.
func TestHandleDetachIsIdempotent(t *testing.T) {
	t.Parallel()
	svc, store, _ := fixture()
	store.detachErr = people.ErrSubjectNotFound

	payload, err := json.Marshal(detachPayload{SubjectUID: "sunuikf1e9jdpjog5qgomsvgrb"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := svc.HandleDetach(t.Context(), jobs.Job{ID: 1, Payload: payload}); err != nil {
		t.Errorf("HandleDetach on an already detached subject = %v, want nil", err)
	}
}

// TestHandleDetachRejectsUidlessPayload verifies a payload naming no subject
// fails the job rather than detaching something arbitrary.
func TestHandleDetachRejectsUidlessPayload(t *testing.T) {
	t.Parallel()
	svc, store, _ := fixture()

	if err := svc.HandleDetach(t.Context(), jobs.Job{ID: 1, Payload: []byte(`{}`)}); err == nil {
		t.Fatal("HandleDetach accepted a payload with no subject uid")
	}
	if len(store.detached) != 0 {
		t.Errorf("detached %v, want nothing", store.detached)
	}
}

// TestHandleRestoreReplaysTheSnapshot verifies the undo job hands the whole
// snapshot back to the store and audits the re-creation.
func TestHandleRestoreReplaysTheSnapshot(t *testing.T) {
	t.Parallel()
	svc, store, queue := fixture()
	undo, err := svc.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := svc.EnqueueRestore(t.Context(), undo, audit.Meta{ActorUID: "usr1"}); err != nil {
		t.Fatalf("EnqueueRestore: %v", err)
	}
	if queue.enqueued[0].Type != jobs.TypeNamelessRestore {
		t.Fatalf("job type = %q, want %q", queue.enqueued[0].Type, jobs.TypeNamelessRestore)
	}

	if err := svc.HandleRestore(t.Context(), queue.enqueued[0]); err != nil {
		t.Fatalf("HandleRestore: %v", err)
	}
	if len(store.restored) != 1 {
		t.Fatalf("restored %d subject(s), want 1", len(store.restored))
	}
	got := store.restored[0]
	if got.Subject.UID != undo.Subjects[0].Subject.UID || len(got.MarkerUIDs) != 2 || len(got.Faces) != 2 {
		t.Errorf("restored %+v, want the snapshot with its 2 markers / 2 faces", got)
	}
	if entry := store.entries[0]; entry.Action != audit.ActionSubjectCreate || entry.ActorUID != "usr1" {
		t.Errorf("audit entry = %s by %q, want subject.create by usr1", entry.Action, entry.ActorUID)
	}
}

// TestEnqueueDetachReportsQueueFailure verifies a queue error stops the
// scheduling and is reported rather than swallowed.
func TestEnqueueDetachReportsQueueFailure(t *testing.T) {
	t.Parallel()
	svc, _, queue := fixture()
	queue.err = errors.New("queue down")
	undo, err := svc.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	queued, err := svc.EnqueueDetach(t.Context(), undo, audit.Meta{})
	if err == nil {
		t.Fatal("EnqueueDetach hid the queue failure")
	}
	if queued != 0 {
		t.Errorf("queued = %d, want 0", queued)
	}
}
