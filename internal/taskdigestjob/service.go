// Package taskdigestjob sends every person one e-mail a day listing the open
// tasks whose move is theirs.
//
// An agent opens questions and review batches in the task queue for a human,
// but nothing reached that human outside the app: the queue's "waiting on me"
// is a filter you have to come and look at. The `task_digest` job turns it into
// a message, on the same rails as every other mail — it schedules `mail_send`
// jobs through internal/mailjob rather than talking to a server itself, so a
// mail host that is briefly away delays the digest instead of losing it.
//
// Two rules keep it from becoming noise. It goes out once a day, at a
// configured hour (the Scheduler here enqueues the job; the worker runs it).
// And it is sent only when something changed: the store stamps when a person's
// last digest was scheduled, and a person whose waiting tasks have not moved
// past that stamp receives nothing. The store decides who is due
// (phototask.Store.Digests); this package renders the decision into mail.
package taskdigestjob

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/phototask"
)

// ErrPartialRun indicates a run scheduled or stamped some digests but not all:
// at least one enqueue or stamp failed. The people it reached are stamped and
// will not hear the same thing twice, so the queue may retry the job for the
// rest.
var ErrPartialRun = errors.New("taskdigestjob: some digests were not scheduled")

// Source is what the digest reads and stamps. It is satisfied by
// *phototask.Store.
type Source interface {
	// Digests returns one entry per person who has something new to hear.
	Digests(ctx context.Context) ([]phototask.Digest, error)
	// MarkDigested records when a person's digest was scheduled.
	MarkDigested(ctx context.Context, uid string, at time.Time) error
}

// MailScheduler schedules the message. It is satisfied by *mailjob.Enqueuer.
type MailScheduler interface {
	// Enabled reports whether this instance sends mail at all.
	Enabled() bool
	// Enqueue schedules m using exec, a pool or an open transaction.
	Enqueue(ctx context.Context, exec jobs.Execer, m mailjob.Mail) error
}

// Config bundles the handler's collaborators. Source, Mail and Exec are
// required.
type Config struct {
	// Source reads who is due and stamps who was written to.
	Source Source
	// Mail schedules the messages.
	Mail MailScheduler
	// Exec is what the mail jobs are inserted on — the pool, since a digest is
	// not the side effect of any one mutation.
	Exec jobs.Execer
	// BaseURL is this instance's public URL, the base of every link in the
	// mail (mail.base_url). Empty falls back to site-relative links, which is
	// what an instance without a configured public URL can honestly say.
	BaseURL string
	// Clock reads the current time, for the stamp; nil uses the wall clock.
	Clock func() time.Time
	// Logger records what was scheduled and what was skipped; nil uses
	// slog.Default().
	Logger *slog.Logger
}

// Service is the worker handler for `task_digest` jobs.
type Service struct {
	source  Source
	mail    MailScheduler
	exec    jobs.Execer
	baseURL string
	clock   func() time.Time
	log     *slog.Logger
}

// New builds a Service from cfg. It panics on a missing Source, Mail or Exec:
// a handler without them is a wiring bug that should surface at startup rather
// than fail every run.
func New(cfg Config) *Service {
	if cfg.Source == nil || cfg.Mail == nil || cfg.Exec == nil {
		panic("taskdigestjob: New requires Source, Mail and Exec")
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		source: cfg.Source, mail: cfg.Mail, exec: cfg.Exec,
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		clock:   clock, log: log,
	}
}

// Result says what one run did: how many digests were scheduled, how many
// people were skipped over an address that cannot be mailed, and how many
// failed to schedule or stamp.
type Result struct {
	Sent    int
	Skipped int
	Failed  int
}

// Handle is the worker.HandlerFunc for `task_digest` jobs. The payload is
// empty — one run covers everybody — so it is not read. A failed run is an
// ordinary error and the queue retries it; the people it did reach are stamped,
// so a retry writes to the rest only.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	res, err := s.Run(ctx)
	s.log.InfoContext(ctx, "task digest run",
		slog.Int64("job_id", job.ID), slog.Int("sent", res.Sent),
		slog.Int("skipped", res.Skipped), slog.Int("failed", res.Failed))
	return err
}

// Run sends the digest once: for every person the source says is due, it
// schedules one mail and stamps the account, in that order — a stamp without a
// mail would silence somebody, a mail without a stamp only repeats itself. A
// recipient the mailer would refuse (a placeholder in the reserved .invalid
// domain, or no address at all) is skipped and left unstamped, so they hear
// the news once their address is fixed; the skip never fails the run. With
// mail switched off nothing is scheduled and nobody is stamped.
//
// A failure to schedule or stamp one person is logged and counted, and the
// run goes on to the next; it then returns ErrPartialRun so the queue retries
// for those it missed.
func (s *Service) Run(ctx context.Context) (Result, error) {
	if !s.mail.Enabled() {
		s.log.InfoContext(ctx, "task digest not sent: mail is disabled")
		return Result{}, nil
	}
	digests, err := s.source.Digests(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("taskdigestjob: reading who is due: %w", err)
	}
	var res Result
	for _, d := range digests {
		switch err := s.send(ctx, d); {
		case err == nil:
			res.Sent++
		case errors.Is(err, mailer.ErrInvalidAddress), errors.Is(err, mailer.ErrPlaceholderAddress):
			res.Skipped++
			s.log.DebugContext(ctx, "task digest skipped: recipient cannot be mailed",
				slog.String("user_uid", d.UserUID), slog.String("error", err.Error()))
		default:
			res.Failed++
			s.log.WarnContext(ctx, "task digest not scheduled",
				slog.String("user_uid", d.UserUID), slog.String("error", err.Error()))
		}
	}
	if res.Failed > 0 {
		return res, fmt.Errorf("%w: %d of %d", ErrPartialRun, res.Failed, len(digests))
	}
	return res, nil
}

// send schedules one person's digest and stamps their account. It returns the
// mailer's typed error, unwrapped by nothing, for an address that cannot be
// mailed, so Run can tell a skip from a failure.
func (s *Service) send(ctx context.Context, d phototask.Digest) error {
	if err := mailer.ValidateAddress(d.Email); err != nil {
		return err //nolint:wrapcheck // the typed error is the whole point: Run classifies it
	}
	mail := mailjob.TasksWaitingDigest(d.Email, s.dataFor(d))
	if err := s.mail.Enqueue(ctx, s.exec, mail); err != nil {
		return fmt.Errorf("scheduling the mail: %w", err)
	}
	if err := s.source.MarkDigested(ctx, d.UserUID, s.clock()); err != nil {
		return fmt.Errorf("stamping the account: %w", err)
	}
	return nil
}

// dataFor turns a digest into what the template needs: the person, the count,
// the listed tasks with their links, and the link to the whole queue.
func (s *Service) dataFor(d phototask.Digest) mailer.TasksWaitingDigestData {
	tasks := make([]mailer.DigestTask, 0, len(d.Tasks))
	for _, t := range d.Tasks {
		tasks = append(tasks, mailer.DigestTask{
			Title: t.Title, State: string(t.State), URL: TaskURL(s.baseURL, t.UID),
		})
	}
	return mailer.TasksWaitingDigestData{
		DisplayName: d.DisplayName,
		Total:       d.Total,
		Tasks:       tasks,
		QueueURL:    QueueURL(s.baseURL),
	}
}

// The two pages the digest links to.
const (
	tasksPath   = "/tasks"
	waitingPath = tasksPath + "?waiting=1"
)

// TaskURL is the absolute address of a task's page under base, or the
// site-relative path when base is empty. A trailing slash on base is
// tolerated.
func TaskURL(base, uid string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/") + tasksPath + "/" + uid
}

// QueueURL is the address of the queue filtered to what waits on the reader,
// under base (site-relative when base is empty).
func QueueURL(base string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/") + waitingPath
}
