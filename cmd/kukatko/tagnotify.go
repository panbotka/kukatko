package main

import (
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/tagnotifyjob"
)

// buildTagNotifyRecorder assembles the tagging half of the "you were tagged"
// notification: the people store's tag observer that records which photos an
// account was tagged on and opens its notifications.tags.window-long window.
// It records nothing for an account that would receive the kind through no
// channel — every channel off, or the ones it chose switched off instance-wide.
func buildTagNotifyRecorder(cfg *config.Config, db *database.DB) *tagnotifyjob.Recorder {
	return tagnotifyjob.NewRecorder(tagNotifyRecorderConfig(cfg, buildDeliverer(cfg, db)))
}

// tagNotifyRecorderConfig maps the configuration onto the recorder's: the
// window length, and the audience that decides who is recorded at all.
func tagNotifyRecorderConfig(cfg *config.Config, audience tagnotifyjob.Audience) tagnotifyjob.RecorderConfig {
	return tagnotifyjob.RecorderConfig{
		Window:   cfg.Notifications.Tags.Window,
		Audience: audience,
	}
}

// buildTagNotifyService assembles the `tag_notify` job handler that closes a
// window: one notification per album the photos tagged inside it are in (plus
// one for the rest), delivered to the account's channels through
// internal/notifydeliver. It is always built and
// registered, so a window always closes instead of waiting forever for a
// claimant; a channel switched off instance-wide then simply delivers nothing.
func buildTagNotifyService(cfg *config.Config, db *database.DB) *tagnotifyjob.Service {
	return tagnotifyjob.New(tagnotifyjob.Config{
		DB:      db.Pool(),
		Deliver: buildDeliverer(cfg, db),
	})
}
