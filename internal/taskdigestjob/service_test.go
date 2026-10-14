package taskdigestjob

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notifystanding"
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

// fakeSource is a scripted Source: the digests it hands out, keyed by
// "<project>/<user>", and the stamps it received under the same keys.
type fakeSource struct {
	digests  map[string]phototask.Digest
	err      error
	stampErr error
	stamps   map[string]time.Time
}

// key is how fakeSource files a (project, person) pair.
func key(projectUID, userUID string) string { return projectUID + "/" + userUID }

// with returns a source holding ds, each filed under its own pair.
func with(ds ...phototask.Digest) *fakeSource {
	f := &fakeSource{digests: map[string]phototask.Digest{}}
	for _, d := range ds {
		f.digests[key(d.ProjectUID, d.UserUID)] = d
	}
	return f
}

// DigestFor returns the scripted digest of the pair, nil when there is none.
func (f *fakeSource) DigestFor(_ context.Context, projectUID, userUID string) (*phototask.Digest, error) {
	if f.err != nil {
		return nil, f.err
	}
	d, ok := f.digests[key(projectUID, userUID)]
	if !ok {
		return nil, nil //nolint:nilnil // the Source contract: nil digest = nothing due
	}
	return &d, nil
}

// MarkDigested records the stamp, or fails when scripted to.
func (f *fakeSource) MarkDigested(_ context.Context, projectUID, uid string, at time.Time) error {
	if f.stampErr != nil {
		return f.stampErr
	}
	if f.stamps == nil {
		f.stamps = map[string]time.Time{}
	}
	f.stamps[key(projectUID, uid)] = at
	return nil
}

// queue records the `mail_send` jobs the real mailjob.Enqueuer scheduled, so a
// test can push them through the real handler and the fake mailer.
type queue struct {
	jobs []jobs.Job
	err  error
	exec []jobs.Execer
	// standing is what deliver's mail handler asks about every addressed mail.
	standing standingRecorder
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

// standingRecorder is a mailjob.Standing that lets everybody through and
// records whom it was asked about.
type standingRecorder struct {
	asked []notifystanding.Addressee
}

// InStanding records a and answers yes.
func (s *standingRecorder) InStanding(_ context.Context, a notifystanding.Addressee) (bool, error) {
	s.asked = append(s.asked, a)
	return true, nil
}

// deliver runs every recorded job through the real mail_send handler into a
// fake mailer and returns what it would have sent. Every digest mail is
// addressed, so the handler asks q.standing about each one.
func (q *queue) deliver(t *testing.T) []mailer.Message {
	t.Helper()
	fake := mailer.NewFake()
	svc := mailjob.NewService(mailjob.ServiceConfig{Sender: fake, Standing: &q.standing})
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

// digest builds one person's digest of project (slug = its uid, name
// "Knihovna <uid>") over n tasks, listing at most listed.
func digest(projectUID, uid, email string, n, listed int) phototask.Digest {
	d := phototask.Digest{
		ProjectUID: projectUID, ProjectSlug: projectUID, ProjectName: "Knihovna " + projectUID,
		UserUID: uid, Email: email, DisplayName: "Osoba " + uid, Total: n,
	}
	for i := range listed {
		d.Tasks = append(d.Tasks, phototask.DigestTask{
			UID: uid + "-t" + string(rune('a'+i)), Title: "Otázka " + string(rune('A'+i)),
			State: phototask.StateQuestion,
		})
	}
	return d
}

// run is Run that fails the test on an error.
func run(t *testing.T, svc *Service, projectUID, userUID string) Outcome {
	t.Helper()
	outcome, err := svc.Run(context.Background(), projectUID, userUID)
	if err != nil {
		t.Fatalf("Run(%s, %s): %v", projectUID, userUID, err)
	}
	return outcome
}

// TestRun_onePairOneMail verifies the happy path end to end: the pair the job
// names gets exactly one mail — rendered through the real mail_send handler
// into the fake mailer — with the subject counting their tasks, the project's
// name heading the list and every link inside the project (/p/<slug>/) on the
// public base URL, and that membership is stamped with the fixed clock after
// the mail was scheduled.
func TestRun_onePairOneMail(t *testing.T) {
	t.Parallel()

	source := with(digest("v", "us-a", "a@example.com", 2, 2), digest("v", "us-b", "b@example.com", 25, 20))
	svc, q := newService(t, source, true)

	if got := run(t, svc, "v", "us-a"); got != OutcomeSent {
		t.Errorf("outcome = %q, want sent", got)
	}
	if got := run(t, svc, "v", "us-b"); got != OutcomeSent {
		t.Errorf("outcome = %q, want sent", got)
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
	// Each mail is addressed to its person and project, so the mail handler
	// re-checks their standing when it sends.
	wantAsked := []notifystanding.Addressee{
		{UserUID: "us-a", ProjectUID: "v", Kind: DigestKind}, {UserUID: "us-b", ProjectUID: "v", Kind: DigestKind},
	}
	if !slices.Equal(q.standing.asked, wantAsked) {
		t.Errorf("standing asked about %+v, want %+v", q.standing.asked, wantAsked)
	}
	if sent[0].To != "a@example.com" || sent[0].Subject != "Kukátko · Knihovna v: Čekají na tebe 2 úkoly" {
		t.Errorf("first mail = %q %q, want a's two tasks, the subject naming the project", sent[0].To, sent[0].Subject)
	}
	if !strings.Contains(sent[0].Body, "Projekt Knihovna v\n\n- Otázka A (čeká na odpověď)\n"+
		"  https://kukatko.example.com/p/v/tasks/us-a-ta\n") {
		t.Errorf("first body = %q, want the project heading and the task line with its link", sent[0].Body)
	}
	if !strings.Contains(sent[0].Body, "https://kukatko.example.com/p/v/tasks?waiting=1\n") {
		t.Errorf("first body = %q, want the link to the project's waiting queue", sent[0].Body)
	}
	if !strings.Contains(sent[1].Body, "…a dalších 5 úkolů.\n") {
		t.Errorf("second body = %q, want the count of the unlisted rest", sent[1].Body)
	}
	for _, uid := range []string{"us-a", "us-b"} {
		if got, ok := source.stamps[key("v", uid)]; !ok || !got.Equal(pinned) {
			t.Errorf("stamp for %s = %v (%v), want the fixed clock %v", uid, got, ok, pinned)
		}
	}
}

// TestRun_twoProjectsTwoMails verifies a person in two projects with something
// due in each gets two jobs' worth of mail: one per project, each headed with
// its project and linking into it, each stamping only its own membership.
func TestRun_twoProjectsTwoMails(t *testing.T) {
	t.Parallel()

	source := with(digest("pa", "us-a", "a@example.com", 1, 1), digest("pb", "us-a", "a@example.com", 1, 1))
	svc, q := newService(t, source, true)
	for _, projectUID := range []string{"pa", "pb"} {
		payload, _ := json.Marshal(jobs.TaskDigestPayload{ProjectUID: projectUID, UserUID: "us-a"})
		if err := svc.Handle(context.Background(), jobs.Job{ID: 1, Type: jobs.TypeTaskDigest, Payload: payload}); err != nil {
			t.Fatalf("Handle(%s): %v", projectUID, err)
		}
	}
	sent := q.deliver(t)
	if len(sent) != 2 {
		t.Fatalf("delivered %d mails, want one per project", len(sent))
	}
	for i, projectUID := range []string{"pa", "pb"} {
		if !strings.Contains(sent[i].Body, "Projekt Knihovna "+projectUID+"\n") ||
			!strings.Contains(sent[i].Body, "/p/"+projectUID+"/tasks/") {
			t.Errorf("mail %d = %q, want %s's heading and links", i, sent[i].Body, projectUID)
		}
		if _, ok := source.stamps[key(projectUID, "us-a")]; !ok {
			t.Errorf("membership of %s not stamped", projectUID)
		}
	}
}

// TestRun_nothingNewSendsNothing verifies a pair with nothing due — the store's
// answer when no waiting task moved past the stamp — and a legacy job naming no
// pair schedule no mail and stamp nobody.
func TestRun_nothingNewSendsNothing(t *testing.T) {
	t.Parallel()

	source := with(digest("v", "us-a", "a@example.com", 1, 1))
	svc, q := newService(t, source, true)
	for _, pair := range [][2]string{{"v", "us-other"}, {"other", "us-a"}, {"", ""}} {
		if got := run(t, svc, pair[0], pair[1]); got != OutcomeNothing {
			t.Errorf("Run(%v) = %q, want nothing", pair, got)
		}
	}
	if err := svc.Handle(context.Background(), jobs.Job{ID: 3, Type: jobs.TypeTaskDigest}); err != nil {
		t.Errorf("Handle(legacy job) = %v, want nil", err)
	}
	if len(q.jobs) != 0 || len(source.stamps) != 0 {
		t.Errorf("%d jobs, %d stamps; want nothing at all", len(q.jobs), len(source.stamps))
	}
}

// TestRun_disabledMailSendsAndStampsNothing verifies that with mail switched
// off the run is a no-op: nothing is scheduled, and — the part that matters —
// nobody is stamped, so nothing is silenced for the day mail is switched on.
func TestRun_disabledMailSendsAndStampsNothing(t *testing.T) {
	t.Parallel()

	source := with(digest("v", "us-a", "a@example.com", 1, 1))
	svc, q := newService(t, source, false)
	if got := run(t, svc, "v", "us-a"); got != OutcomeMailOff {
		t.Errorf("outcome = %q, want mail_off", got)
	}
	if len(q.jobs) != 0 || len(source.stamps) != 0 {
		t.Errorf("%d jobs, %d stamps; want nothing at all", len(q.jobs), len(source.stamps))
	}
}

// TestRun_skipsWhoCannotBeMailed verifies a placeholder or missing address is
// skipped without stamping — they hear the news once the address is fixed —
// and without an error the queue would retry.
func TestRun_skipsWhoCannotBeMailed(t *testing.T) {
	t.Parallel()

	source := with(digest("v", "us-placeholder", "import-1@kukatko.invalid", 1, 1), digest("v", "us-blank", "", 1, 1))
	svc, q := newService(t, source, true)
	for _, uid := range []string{"us-placeholder", "us-blank"} {
		if got := run(t, svc, "v", uid); got != OutcomeSkipped {
			t.Errorf("Run(%s) = %q, want skipped", uid, got)
		}
	}
	if len(q.jobs) != 0 || len(source.stamps) != 0 {
		t.Errorf("%d jobs, %d stamps; want nothing for addresses that cannot be mailed", len(q.jobs), len(source.stamps))
	}
}

// TestRun_reportsFailures verifies a source that cannot be read, a queue that
// refuses and a stamp that fails each come back as an error the queue retries,
// and that a refused mail stamps nobody.
func TestRun_reportsFailures(t *testing.T) {
	t.Parallel()

	t.Run("source unreadable", func(t *testing.T) {
		t.Parallel()
		broken := errors.New("connection refused")
		svc, q := newService(t, &fakeSource{err: broken}, true)
		if _, err := svc.Run(context.Background(), "v", "us-a"); !errors.Is(err, broken) {
			t.Errorf("Run = %v, want it to wrap %v", err, broken)
		}
		if len(q.jobs) != 0 {
			t.Errorf("%d jobs scheduled, want none", len(q.jobs))
		}
	})

	t.Run("queue refuses", func(t *testing.T) {
		t.Parallel()
		source := with(digest("v", "us-a", "a@example.com", 1, 1))
		svc, q := newService(t, source, true)
		q.err = errors.New("queue is full")
		if _, err := svc.Run(context.Background(), "v", "us-a"); !errors.Is(err, q.err) {
			t.Errorf("Run = %v, want it to wrap %v", err, q.err)
		}
		if len(source.stamps) != 0 {
			t.Errorf("%d stamps, want nobody stamped", len(source.stamps))
		}
	})

	t.Run("stamp fails", func(t *testing.T) {
		t.Parallel()
		source := with(digest("v", "us-a", "a@example.com", 1, 1))
		source.stampErr = errors.New("project_members locked")
		svc, q := newService(t, source, true)
		// The mail was scheduled before the stamp failed: that is the safe order
		// (a repeat beats a silence).
		if _, err := svc.Run(context.Background(), "v", "us-a"); !errors.Is(err, source.stampErr) || len(q.jobs) != 1 {
			t.Errorf("Run = %v with %d jobs, want the stamp error after the mail was scheduled", err, len(q.jobs))
		}
	})

	t.Run("payload unreadable", func(t *testing.T) {
		t.Parallel()
		svc, _ := newService(t, with(), true)
		if err := svc.Handle(context.Background(), jobs.Job{ID: 9, Payload: []byte("{")}); err == nil {
			t.Error("Handle(broken payload) = nil, want an error")
		}
	})
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

// TestURLs verifies the links lead into the project (/p/<slug>/) on the public
// base with a tolerated trailing slash, and degrade to site-relative paths
// without one.
func TestURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		base      string
		wantTask  string
		wantQueue string
	}{
		{"https://kukatko.example.com", "https://kukatko.example.com/p/v/tasks/tk-1", "https://kukatko.example.com/p/v/tasks?waiting=1"},
		{"https://kukatko.example.com/ ", "https://kukatko.example.com/p/v/tasks/tk-1", "https://kukatko.example.com/p/v/tasks?waiting=1"},
		{"", "/p/v/tasks/tk-1", "/p/v/tasks?waiting=1"},
	}
	for _, tt := range tests {
		if got := TaskURL(tt.base, "v", "tk-1"); got != tt.wantTask {
			t.Errorf("TaskURL(%q) = %q, want %q", tt.base, got, tt.wantTask)
		}
		if got := QueueURL(tt.base, "v"); got != tt.wantQueue {
			t.Errorf("QueueURL(%q) = %q, want %q", tt.base, got, tt.wantQueue)
		}
	}
}
