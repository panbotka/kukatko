// Package taskdigestjob sends every person one e-mail a day per project listing
// the open tasks of that project whose move is theirs.
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
// last digest of a project was scheduled (on the membership), and a person
// whose waiting tasks have not moved past that stamp receives nothing. The
// store decides who is due (phototask.Store.Digests); this package renders the
// decision into mail.
//
// The work is per (project, person): the Scheduler enqueues one `task_digest`
// job for every pair with something due, its payload naming both
// (jobs.TaskDigestPayload), and the job sends that one mail — headed with the
// project's name, its links under /p/<slug>/ — and stamps that one membership.
// A person in two projects gets two mails, each about one library.
package taskdigestjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/project"
)

// Source is what the digest reads and stamps. It is satisfied by
// *phototask.Store.
type Source interface {
	// DigestFor returns one person's digest of one project, nil when nothing
	// new waits on them there.
	DigestFor(ctx context.Context, projectUID, userUID string) (*phototask.Digest, error)
	// MarkDigested records when a person's digest of a project was scheduled.
	MarkDigested(ctx context.Context, projectUID, uid string, at time.Time) error
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

// Outcome says what one job did for its pair.
type Outcome string

const (
	// OutcomeSent is a digest scheduled and its membership stamped.
	OutcomeSent Outcome = "sent"
	// OutcomeNothing is a pair with nothing new to hear (or a legacy job naming
	// no pair): no mail, no stamp.
	OutcomeNothing Outcome = "nothing"
	// OutcomeSkipped is a recipient the mailer would refuse (a placeholder in
	// the reserved .invalid domain, or no address at all), left unstamped so
	// they hear the news once their address is fixed.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeMailOff is a run on an instance whose mail is switched off.
	OutcomeMailOff Outcome = "mail_off"
)

// Handle is the worker.HandlerFunc for `task_digest` jobs. The payload names
// the pair (jobs.TaskDigestPayload); a job queued before the payload existed
// names none and does nothing — the next daily run covers everybody. A failure
// to read, schedule or stamp is an ordinary error and the queue retries it.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var payload jobs.TaskDigestPayload
	if len(job.Payload) > 0 {
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("taskdigestjob: decoding payload of job %d: %w", job.ID, err)
		}
	}
	outcome, err := s.Run(ctx, payload.ProjectUID, payload.UserUID)
	s.log.InfoContext(ctx, "task digest run",
		slog.Int64("job_id", job.ID), slog.String("project_uid", payload.ProjectUID),
		slog.String("user_uid", payload.UserUID), slog.String("outcome", string(outcome)))
	return err
}

// Run sends userUID's digest of projectUID once: when the source says it is
// due, it schedules the mail and stamps the membership, in that order — a stamp
// without a mail would silence somebody, a mail without a stamp only repeats
// itself. A recipient the mailer would refuse is skipped and left unstamped;
// the skip is not an error. With mail switched off nothing is scheduled and
// nobody is stamped. An empty projectUID or userUID (a legacy job) does
// nothing.
func (s *Service) Run(ctx context.Context, projectUID, userUID string) (Outcome, error) {
	if !s.mail.Enabled() {
		s.log.InfoContext(ctx, "task digest not sent: mail is disabled")
		return OutcomeMailOff, nil
	}
	if projectUID == "" || userUID == "" {
		return OutcomeNothing, nil
	}
	d, err := s.source.DigestFor(ctx, projectUID, userUID)
	if err != nil {
		return "", fmt.Errorf("taskdigestjob: reading the digest of %s in %s: %w", userUID, projectUID, err)
	}
	if d == nil {
		return OutcomeNothing, nil
	}
	switch err := s.send(ctx, *d); {
	case err == nil:
		return OutcomeSent, nil
	case errors.Is(err, mailer.ErrInvalidAddress), errors.Is(err, mailer.ErrPlaceholderAddress):
		s.log.DebugContext(ctx, "task digest skipped: recipient cannot be mailed",
			slog.String("user_uid", d.UserUID), slog.String("error", err.Error()))
		return OutcomeSkipped, nil
	default:
		return "", fmt.Errorf("taskdigestjob: digest of %s in %s: %w", userUID, projectUID, err)
	}
}

// send schedules one person's digest and stamps their membership. It returns
// the mailer's typed error, unwrapped by nothing, for an address that cannot be
// mailed, so Run can tell a skip from a failure.
func (s *Service) send(ctx context.Context, d phototask.Digest) error {
	if err := mailer.ValidateAddress(d.Email); err != nil {
		return err //nolint:wrapcheck // the typed error is the whole point: Run classifies it
	}
	mail := mailjob.TasksWaitingDigest(d.Email, s.dataFor(d))
	if err := s.mail.Enqueue(ctx, s.exec, mail); err != nil {
		return fmt.Errorf("scheduling the mail: %w", err)
	}
	if err := s.source.MarkDigested(ctx, d.ProjectUID, d.UserUID, s.clock()); err != nil {
		return fmt.Errorf("stamping the membership: %w", err)
	}
	return nil
}

// dataFor turns a digest into what the template needs: the person, the
// project's name, the count, the listed tasks with their links, and the link to
// the whole queue — every link inside the project (/p/<slug>/…).
func (s *Service) dataFor(d phototask.Digest) mailer.TasksWaitingDigestData {
	tasks := make([]mailer.DigestTask, 0, len(d.Tasks))
	for _, t := range d.Tasks {
		tasks = append(tasks, mailer.DigestTask{
			Title: t.Title, State: string(t.State), URL: TaskURL(s.baseURL, d.ProjectSlug, t.UID),
		})
	}
	return mailer.TasksWaitingDigestData{
		DisplayName: d.DisplayName,
		ProjectName: d.ProjectName,
		Total:       d.Total,
		Tasks:       tasks,
		QueueURL:    QueueURL(s.baseURL, d.ProjectSlug),
	}
}

// The two pages the digest links to.
const (
	tasksPath   = "/tasks"
	waitingPath = tasksPath + "?waiting=1"
)

// TaskURL is the absolute address of a task's page inside the project slug
// (/p/<slug>/tasks/<uid>) under base, or the site-relative path when base is
// empty. A trailing slash on base is tolerated.
func TaskURL(base, slug, uid string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/") + project.AppPath(slug, tasksPath+"/"+uid)
}

// QueueURL is the address of the project's queue filtered to what waits on the
// reader, under base (site-relative when base is empty).
func QueueURL(base, slug string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/") + project.AppPath(slug, waitingPath)
}
