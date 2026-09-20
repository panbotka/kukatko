package main

import (
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/taskdigestjob"
)

// taskDigestOn reports whether the daily tasks digest runs on this instance:
// it has to be asked for, and it rides on mail, so with mail off there is
// nothing for it to send through. Both the handler and the scheduler read this
// one answer, so a job is never enqueued that nothing will claim.
func taskDigestOn(cfg *config.Config) bool {
	return cfg.Tasks.Digest.Enabled && cfg.Mail.Enabled
}

// buildTaskDigestServiceOrNil assembles the `task_digest` job handler — the
// once-a-day run that mails every person the open tasks whose move is theirs —
// or nil when the digest is off. Its mails go through the same queue enqueuer
// the account flows use, on the pool: a digest is not the side effect of any
// one mutation, so there is no transaction to join. The links inside it are
// built on mail.base_url, exactly as the password-reset link is.
func buildTaskDigestServiceOrNil(cfg *config.Config, db *database.DB) *taskdigestjob.Service {
	if !taskDigestOn(cfg) {
		return nil
	}
	return taskdigestjob.New(taskdigestjob.Config{
		Source:  phototask.NewStore(db.Pool()),
		Mail:    mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: cfg.Mail.Enabled}),
		Exec:    db.Pool(),
		BaseURL: cfg.Mail.BaseURL,
	})
}

// buildTaskDigestScheduler returns the daily scheduler that enqueues the
// `task_digest` job at tasks.digest.hour UTC — inert (its Run returns at once)
// when the digest is off, so the serve command can always start it.
func buildTaskDigestScheduler(cfg *config.Config, db *database.DB) *taskdigestjob.Scheduler {
	return taskdigestjob.NewScheduler(taskdigestjob.SchedulerConfig{
		Enabled:  taskDigestOn(cfg),
		Enqueuer: jobs.NewEnqueuer(jobs.NewStore(db.Pool())),
		Hour:     cfg.Tasks.Digest.Hour,
	})
}
