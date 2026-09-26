package pushjob

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/push"
)

// fakeExec stands in for the pool or transaction an enqueue runs on. It is never
// queried — the fake lister and scheduler below do not touch a database — but
// the Enqueuer must hand exactly the executor it was given to both, which is
// what makes "enqueue inside the caller's transaction" work.
type fakeExec struct{ name string }

// QueryRow is never called in these tests; it exists to satisfy jobs.Execer.
func (fakeExec) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("fakeExec.QueryRow must not be called")
}

// Query is never called in these tests; it exists to satisfy push.Querier.
func (fakeExec) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("fakeExec.Query must not be called")
}

// scheduled is one recorded call to the fake scheduler.
type scheduled struct {
	exec    jobs.Execer
	jobType string
	payload payload
	opts    jobs.EnqueueOptions
}

// recorder is a fake Scheduler and Lister: it hands out a fixed list of device
// ids, records what it was asked to enqueue and can fail either step on demand.
type recorder struct {
	ids       []string
	listErr   error
	listedOn  push.Querier
	listedFor string
	calls     []scheduled
	err       error
}

// list is the Lister the Enqueuer under test is built with.
func (r *recorder) list(_ context.Context, q push.Querier, userUID string) ([]string, error) {
	r.listedOn, r.listedFor = q, userUID
	return r.ids, r.listErr
}

// schedule is the Scheduler the Enqueuer under test is built with.
func (r *recorder) schedule(
	_ context.Context, exec jobs.Execer, jobType string, raw json.RawMessage, opts jobs.EnqueueOptions,
) (jobs.Job, error) {
	if r.err != nil {
		return jobs.Job{}, r.err
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return jobs.Job{}, err
	}
	r.calls = append(r.calls, scheduled{exec: exec, jobType: jobType, payload: p, opts: opts})
	return jobs.Job{ID: int64(len(r.calls)), Type: jobType}, nil
}

// newEnqueuer returns an Enqueuer over rec.
func newEnqueuer(t *testing.T, enabled bool, rec *recorder) *Enqueuer {
	t.Helper()
	return NewEnqueuer(EnqueuerConfig{Enabled: enabled, Schedule: rec.schedule, List: rec.list})
}

// note is a valid notification for the tests.
var note = push.Notification{Title: "Nová odpověď", Body: "Jana odpověděla", URL: "/tasks/t1", Kind: "task", Tag: "t1"}

// TestEnqueue_oneJobPerDevice verifies the fan-out: every device of the account
// gets its own job naming it and carrying the notification, all on the
// executor the caller passed, with the package's attempt budget.
func TestEnqueue_oneJobPerDevice(t *testing.T) {
	t.Parallel()

	rec := &recorder{ids: []string{"psphone", "pslaptop"}}
	enq := newEnqueuer(t, true, rec)
	tx := fakeExec{name: "the caller's transaction"}

	queued, err := enq.Enqueue(context.Background(), tx, "u1", note)
	if err != nil || queued != 2 {
		t.Fatalf("Enqueue = %d, %v; want 2, nil", queued, err)
	}
	if rec.listedOn != push.Querier(tx) || rec.listedFor != "u1" {
		t.Errorf("listed on %v for %q, want the caller's executor for u1", rec.listedOn, rec.listedFor)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("scheduled %d jobs, want 2", len(rec.calls))
	}
	for i, want := range []string{"psphone", "pslaptop"} {
		call := rec.calls[i]
		if call.jobType != jobs.TypePushSend || call.exec != jobs.Execer(tx) || call.opts.MaxAttempts != MaxAttempts {
			t.Errorf("call %d = %+v, want a push_send on the caller's executor with MaxAttempts", i, call)
		}
		if call.payload.SubscriptionID != want || call.payload.Notification != note {
			t.Errorf("payload %d = %+v, want %s with the notification", i, call.payload, want)
		}
	}
}

// TestEnqueue_refusalsWithoutError verifies the two silent refusals: push off
// (nothing is even read) and an account with no device.
func TestEnqueue_refusalsWithoutError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		enabled bool
		ids     []string
	}{
		{name: "push disabled", enabled: false, ids: []string{"psphone"}},
		{name: "no subscriptions", enabled: true, ids: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{ids: tt.ids}
			enq := newEnqueuer(t, tt.enabled, rec)
			if enq.Enabled() != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", enq.Enabled(), tt.enabled)
			}
			queued, err := enq.Enqueue(context.Background(), fakeExec{}, "u1", note)
			if err != nil || queued != 0 || len(rec.calls) != 0 {
				t.Fatalf("Enqueue = %d, %v with %d jobs; want nothing and no error", queued, err, len(rec.calls))
			}
		})
	}
}

// TestEnqueue_refusalsWithError verifies caller bugs and storage failures are
// reported rather than queued.
func TestEnqueue_refusalsWithError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	tests := []struct {
		name    string
		user    string
		n       push.Notification
		rec     *recorder
		wantErr error
	}{
		{name: "no account", user: " ", n: note, rec: &recorder{}, wantErr: ErrMissingUser},
		{
			name: "no title", user: "u1", n: push.Notification{URL: "/"},
			rec: &recorder{ids: []string{"ps1"}}, wantErr: push.ErrInvalidNotification,
		},
		{
			name: "off-site link", user: "u1", n: push.Notification{Title: "x", URL: "https://evil.example/"},
			rec: &recorder{ids: []string{"ps1"}}, wantErr: push.ErrInvalidNotification,
		},
		{
			name: "oversized", user: "u1", n: push.Notification{Title: "x", Body: strings.Repeat("a", push.MaxPayloadSize)},
			rec: &recorder{ids: []string{"ps1"}}, wantErr: push.ErrPayloadTooLarge,
		},
		{name: "listing fails", user: "u1", n: note, rec: &recorder{listErr: boom}, wantErr: boom},
		{name: "scheduling fails", user: "u1", n: note, rec: &recorder{ids: []string{"ps1"}, err: boom}, wantErr: boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			enq := newEnqueuer(t, true, tt.rec)
			queued, err := enq.Enqueue(context.Background(), fakeExec{}, tt.user, tt.n)
			if !errors.Is(err, tt.wantErr) || queued != 0 {
				t.Fatalf("Enqueue = %d, %v; want 0, %v", queued, err, tt.wantErr)
			}
			if len(tt.rec.calls) != 0 {
				t.Errorf("scheduled %d jobs, want 0", len(tt.rec.calls))
			}
		})
	}
}

// TestNewEnqueuer_defaults verifies the production collaborators are wired when
// none are given.
func TestNewEnqueuer_defaults(t *testing.T) {
	t.Parallel()

	enq := NewEnqueuer(EnqueuerConfig{Enabled: true})
	if enq.schedule == nil || enq.list == nil || enq.log == nil {
		t.Fatalf("NewEnqueuer left a collaborator nil: %+v", enq)
	}
}
