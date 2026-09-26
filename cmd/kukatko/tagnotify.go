package main

import (
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/pushjob"
	"github.com/panbotka/kukatko/internal/tagnotifyjob"
)

// buildTagNotifyRecorder assembles the tagging half of the "you were tagged in
// N photos" notification: the people store's tag observer that records which
// photos an account was tagged on and opens its push.tags.window-long window.
// It records nothing while push is off, since nothing would be delivered.
func buildTagNotifyRecorder(cfg *config.Config, db *database.DB) *tagnotifyjob.Recorder {
	return tagnotifyjob.NewRecorder(tagNotifyRecorderConfig(cfg, notification.NewStore(db.Pool())))
}

// tagNotifyRecorderConfig maps the configuration onto the recorder's: push on
// or off, and the window length.
func tagNotifyRecorderConfig(cfg *config.Config, prefs tagnotifyjob.PreferenceReader) tagnotifyjob.RecorderConfig {
	return tagnotifyjob.RecorderConfig{
		Enabled:     cfg.Push.Enabled,
		Window:      cfg.Push.Tags.Window,
		Preferences: prefs,
	}
}

// buildTagNotifyService assembles the `tag_notify` job handler that closes a
// window: one notification for every photo tagged inside it, delivered through
// push_send. Like the push service it is built, and registered, even with push
// off, so a window left open when push was switched off closes instead of
// waiting forever for a claimant; its delivery then queues nothing.
func buildTagNotifyService(cfg *config.Config, db *database.DB) *tagnotifyjob.Service {
	return tagnotifyjob.New(tagnotifyjob.Config{
		DB:            db.Pool(),
		Notifications: notification.NewStore(db.Pool()),
		Push:          pushjob.NewEnqueuer(pushjob.EnqueuerConfig{Enabled: cfg.Push.Enabled}),
	})
}
