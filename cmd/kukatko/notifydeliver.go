package main

import (
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/notifypurgejob"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/pushjob"
)

// buildDeliverer assembles the one delivery path every notification producer
// uses: the account's channel preferences and the record (internal/notification),
// the projects whose slug every deep link is put under (internal/project),
// the push queue (on only with push.enabled) and the mail queue (on only with
// mail.enabled), with the mail's links built on mail.base_url.
func buildDeliverer(cfg *config.Config, db *database.DB) *notifydeliver.Deliverer {
	return notifydeliver.New(notifydeliver.Config{
		Notifications: notification.NewStore(db.Pool()),
		Projects:      project.NewStore(db.Pool()),
		Push:          pushjob.NewEnqueuer(pushjob.EnqueuerConfig{Enabled: cfg.Push.Enabled}),
		Mail:          mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: cfg.Mail.Enabled}),
		BaseURL:       cfg.Mail.BaseURL,
	})
}

// buildNotifyPurgeService assembles the `notification_purge` job handler,
// which deletes the notifications past the default retention (read: 30 days,
// unread: 90 days).
func buildNotifyPurgeService(db *database.DB) *notifypurgejob.Service {
	return notifypurgejob.New(notifypurgejob.Config{Notifications: notification.NewStore(db.Pool())})
}

// buildNotifyPurgeScheduler returns the scheduler that enqueues the purge of
// every active project at startup and once a day after.
func buildNotifyPurgeScheduler(db *database.DB) *notifypurgejob.Scheduler {
	return notifypurgejob.NewScheduler(jobs.NewEnqueuer(jobs.NewStore(db.Pool())),
		project.NewStore(db.Pool()), notifypurgejob.Interval, nil)
}
