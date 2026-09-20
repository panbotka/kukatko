package taskdigestjob

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/phototask"
)

// pinned is the fixed clock every run in these tests stamps with.
var pinned = time.Date(2026, time.September, 20, 7, 0, 0, 0, time.UTC)

// fakeExec stands in for the pool the mail jobs are inserted on. It is never
// queried — the fake scheduler below does not touch the database — but the
// service must hand exactly this executor to the mail scheduler.
type fakeExec struct{}

// QueryRow is never called in these tests; it exists to satisfy jobs.Execer.
func (fakeExec) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("fakeExec.QueryRow must not be called")
}

// fakeSource is a scripted Source: the digests it hands out and the stamps it
// received.
type fakeSource struct {
	digests  []phototask.Digest
	err      error
	stampErr error
	stamps   map[string]time.Time
}

// Digests returns the scripted digests or error.
func (f *fakeSource) Digests(context.Context) ([]phototask.Digest, error) {
	return f.digests, f.err
}

// MarkDigested records the stamp, or fails when scripted to.
func (f *fakeSource) MarkDigested(_ context.Context, uid string, at time.Time) error {
	if f.stampErr != nil {
		return f.stampErr
	}
	if f.stamps == nil {
		f.stamps = map[string]time.Time{}
	}
	f.stamps[uid] = at
	return nil
}

// queue records the `mail_send` jobs the real mailjob.Enqueuer scheduled, so a
// test can push them through the real handler and the fake mailer.
type queue struct {
	jobs []jobs.Job
	err  error
	exec []jobs.Execer
}

// schedule is the mailjob.Scheduler the enqueuer is built with.
func (q *queue) schedule(
	_ context.Context, exec jobs.Execer, jobType string, raw json.RawMessage, _ jobs.EnqueueOptions,
) (jobs.Job, error) {
	if q.err != nil {
		return jobs.Job{}, q.err
	}
	q.exec = append(q.exec, exec)
	job := jobs.Job{ID: int64(len(q.jobs) + 1), Type: jobType, Payload: raw}
	q.jobs = append(q.jobs, job)
	return job, nil
}

// deliver runs every recorded job through the real mail_send handler into a
// fake mailer and returns what it would have sent.
func (q *queue) deliver(t *testing.T) []mailer.Message {
	t.Helper()
	fake := mailer.NewFake()
	svc := mailjob.NewService(mailjob.ServiceConfig{Sender: fake})
	for _, job := range q.jobs {
		if err := svc.Handle(context.Background(), job); err != nil {
			t.Fatalf("delivering job %d: %v", job.ID, err)
		}
	}
	return fake.Sent()
}

// newService wires a Service over the fakes with mail on, returning the queue
// the mails land on.
func newService(t *testing.T, source *fakeSource, enabled bool) (*Service, *queue) {
	t.Helper()
	q := &queue{}
	mail := mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: enabled, Schedule: q.schedule})
	svc := New(Config{
		Source: source, Mail: mail, Exec: fakeExec{},
		BaseURL: "https://kukatko.example.com/", Clock: func() time.Time { return pinned },
	})
	return svc, q
}

// digest builds one person's digest over n tasks, listing at most listed.
func digest(uid, email string, n, listed int) phototask.Digest {
	d := phototask.Digest{UserUID: uid, Email: email, DisplayName: "Osoba " + uid, Total: n}
	for i := range listed {
		d.Tasks = append(d.Tasks, phototask.DigestTask{
			UID: uid + "-t" + string(rune('a'+i)), Title: "Otázka " + string(rune('A'+i)),
			State: phototask.StateQuestion,
		})
	}
	return d
}

// TestRun_oneMailPerPerson verifies the happy path end to end: every person
// the source says is due gets exactly one mail — rendered through the real
// mail_send handler into the fake mailer — with the subject counting their
// tasks and the links built on the public base URL, and is stamped with the
// fixed clock after the mail was scheduled.
func TestRun_oneMailPerPerson(t *testing.T) {
	t.Parallel()

	source := &fakeSource{digests: []phototask.Digest{
		digest("us-a", "a@example.com", 2, 2),
		digest("us-b", "b@example.com", 25, 20),
	}}
	svc, q := newService(t, source, true)

	res, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res != (Result{Sent: 2}) {
		t.Errorf("Result = %+v, want 2 sent", res)
	}
	for i, exec := range q.exec {
		if _, ok := exec.(fakeExec); !ok {
			t.Errorf("job %d was scheduled on %T, want the service's executor", i, exec)
		}
	}

	sent := q.deliver(t)
	if len(sent) != 2 {
		t.Fatalf("delivered %d mails, want 2", len(sent))
	}
	if sent[0].To != "a@example.com" || sent[0].Subject != "Čekají na tebe 2 úkoly" {
		t.Errorf("first mail = %q %q, want a's two tasks", sent[0].To, sent[0].Subject)
	}
	if !strings.Contains(sent[0].Body, "- Otázka A (čeká na odpověď)\n  https://kukatko.example.com/tasks/us-a-ta\n") {
		t.Errorf("first body = %q, want the task line with its absolute link", sent[0].Body)
	}
	if !strings.Contains(sent[0].Body, "https://kukatko.example.com/tasks?waiting=1\n") {
		t.Errorf("first body = %q, want the link to the waiting queue", sent[0].Body)
	}
	if sent[1].To != "b@example.com" || sent[1].Subject != "Čeká na tebe 25 úkolů" {
		t.Errorf("second mail = %q %q, want b's 25 tasks", sent[1].To, sent[1].Subject)
	}
	if !strings.Contains(sent[1].Body, "…a dalších 5 úkolů.\n") {
		t.Errorf("second body = %q, want the count of the unlisted rest", sent[1].Body)
	}

	for _, uid := range []string{"us-a", "us-b"} {
		if got, ok := source.stamps[uid]; !ok || !got.Equal(pinned) {
			t.Errorf("stamp for %s = %v (%v), want the fixed clock %v", uid, got, ok, pinned)
		}
	}
}

// TestRun_nothingNewSendsNothing verifies a source with nobody due — the store's
// answer when no waiting task moved past anybody's stamp — schedules no mail
// and stamps nobody.
func TestRun_nothingNewSendsNothing(t *testing.T) {
	t.Parallel()

	source := &fakeSource{}
	svc, q := newService(t, source, true)

	res, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res != (Result{}) || len(q.jobs) != 0 || len(source.stamps) != 0 {
		t.Errorf("Result = %+v, %d jobs, %d stamps; want nothing at all", res, len(q.jobs), len(source.stamps))
	}
}

// TestRun_disabledMailSendsAndStampsNothing verifies that with mail switched
// off the run is a no-op: nothing is scheduled, and — the part that matters —
// nobody is stamped, so nothing is silenced for the day mail is switched on.
func TestRun_disabledMailSendsAndStampsNothing(t *testing.T) {
	t.Parallel()

	source := &fakeSource{digests: []phototask.Digest{digest("us-a", "a@example.com", 1, 1)}}
	svc, q := newService(t, source, false)

	res, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res != (Result{}) || len(q.jobs) != 0 || len(source.stamps) != 0 {
		t.Errorf("Result = %+v, %d jobs, %d stamps; want nothing at all", res, len(q.jobs), len(source.stamps))
	}
}

// TestRun_skipsWhoCannotBeMailed verifies a placeholder or missing address is
// skipped without stamping — they hear the news once the address is fixed — and
// without failing the run for everybody else.
func TestRun_skipsWhoCannotBeMailed(t *testing.T) {
	t.Parallel()

	source := &fakeSource{digests: []phototask.Digest{
		digest("us-placeholder", "import-1@kukatko.invalid", 1, 1),
		digest("us-blank", "", 1, 1),
		digest("us-real", "real@example.com", 1, 1),
	}}
	svc, q := newService(t, source, true)

	res, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res != (Result{Sent: 1, Skipped: 2}) {
		t.Errorf("Result = %+v, want 1 sent and 2 skipped", res)
	}
	sent := q.deliver(t)
	if len(sent) != 1 || sent[0].To != "real@example.com" {
		t.Errorf("delivered %v, want only the real address", sent)
	}
	if _, ok := source.stamps["us-placeholder"]; ok {
		t.Error("the placeholder account was stamped; want it left for the day its address is fixed")
	}
	if _, ok := source.stamps["us-real"]; !ok {
		t.Error("the real account was not stamped")
	}
}

// TestRun_reportsFailures verifies a source that cannot be read fails the run
// outright, while a queue or stamp failure for one person is counted, leaves
// that person unstamped, does not stop the others, and comes back as
// ErrPartialRun so the queue retries.
func TestRun_reportsFailures(t *testing.T) {
	t.Parallel()

	t.Run("source unreadable", func(t *testing.T) {
		t.Parallel()
		broken := errors.New("connection refused")
		svc, q := newService(t, &fakeSource{err: broken}, true)
		if _, err := svc.Run(context.Background()); !errors.Is(err, broken) {
			t.Errorf("Run = %v, want it to wrap %v", err, broken)
		}
		if len(q.jobs) != 0 {
			t.Errorf("%d jobs scheduled, want none", len(q.jobs))
		}
	})

	t.Run("queue refuses", func(t *testing.T) {
		t.Parallel()
		source := &fakeSource{digests: []phototask.Digest{digest("us-a", "a@example.com", 1, 1)}}
		svc, q := newService(t, source, true)
		q.err = errors.New("queue is full")
		res, err := svc.Run(context.Background())
		if !errors.Is(err, ErrPartialRun) {
			t.Errorf("Run = %v, want ErrPartialRun", err)
		}
		if res != (Result{Failed: 1}) || len(source.stamps) != 0 {
			t.Errorf("Result = %+v with %d stamps, want 1 failed and nobody stamped", res, len(source.stamps))
		}
	})

	t.Run("stamp fails", func(t *testing.T) {
		t.Parallel()
		source := &fakeSource{
			digests:  []phototask.Digest{digest("us-a", "a@example.com", 1, 1)},
			stampErr: errors.New("users table locked"),
		}
		svc, q := newService(t, source, true)
		res, err := svc.Run(context.Background())
		if !errors.Is(err, ErrPartialRun) {
			t.Errorf("Run = %v, want ErrPartialRun", err)
		}
		// The mail was scheduled before the stamp failed: that is the safe order
		// (a repeat beats a silence), and the result says so.
		if res != (Result{Failed: 1}) || len(q.jobs) != 1 {
			t.Errorf("Result = %+v with %d jobs, want 1 failed after the mail was scheduled", res, len(q.jobs))
		}
	})
}

// TestHandle_runsAndReportsTheError verifies the worker entry point runs the
// digest and passes a partial run's error back to the queue.
func TestHandle_runsAndReportsTheError(t *testing.T) {
	t.Parallel()

	source := &fakeSource{digests: []phototask.Digest{digest("us-a", "a@example.com", 1, 1)}}
	svc, q := newService(t, source, true)
	if err := svc.Handle(context.Background(), jobs.Job{ID: 7, Type: jobs.TypeTaskDigest}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(q.jobs) != 1 {
		t.Errorf("%d jobs scheduled, want 1", len(q.jobs))
	}

	q.err = errors.New("queue is full")
	source.stamps = nil
	if err := svc.Handle(context.Background(), jobs.Job{ID: 8}); !errors.Is(err, ErrPartialRun) {
		t.Errorf("Handle with a broken queue = %v, want ErrPartialRun", err)
	}
}

// TestNew_requiresItsCollaborators verifies a missing Source, Mail or Exec is
// a startup panic rather than a run-time failure.
func TestNew_requiresItsCollaborators(t *testing.T) {
	t.Parallel()

	mail := mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: true, Schedule: (&queue{}).schedule})
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "no source", cfg: Config{Mail: mail, Exec: fakeExec{}}},
		{name: "no mail", cfg: Config{Source: &fakeSource{}, Exec: fakeExec{}}},
		{name: "no exec", cfg: Config{Source: &fakeSource{}, Mail: mail}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("New did not panic")
				}
			}()
			New(tt.cfg)
		})
	}
}

// TestURLs verifies the links are built on the public base with a tolerated
// trailing slash, and degrade to site-relative paths without one.
func TestURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		base      string
		wantTask  string
		wantQueue string
	}{
		{"https://kukatko.example.com", "https://kukatko.example.com/tasks/tk-1", "https://kukatko.example.com/tasks?waiting=1"},
		{"https://kukatko.example.com/ ", "https://kukatko.example.com/tasks/tk-1", "https://kukatko.example.com/tasks?waiting=1"},
		{"", "/tasks/tk-1", "/tasks?waiting=1"},
	}
	for _, tt := range tests {
		if got := TaskURL(tt.base, "tk-1"); got != tt.wantTask {
			t.Errorf("TaskURL(%q) = %q, want %q", tt.base, got, tt.wantTask)
		}
		if got := QueueURL(tt.base); got != tt.wantQueue {
			t.Errorf("QueueURL(%q) = %q, want %q", tt.base, got, tt.wantQueue)
		}
	}
}
