package main

import (
	"github.com/panbotka/kukatko/internal/albumfeaturedjob"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/organize"
)

// buildAlbumFeaturedScheduler assembles the organize store's feature observer:
// it queues one `album_featured_notify` job inside the transaction that puts
// an album on the homepage, only for a fresh feature.
func buildAlbumFeaturedScheduler() *albumfeaturedjob.Scheduler {
	return albumfeaturedjob.NewScheduler(nil)
}

// buildAlbumFeaturedService assembles the `album_featured_notify` job handler
// that announces a newly featured album to every account that wants the
// album_featured kind, through internal/notifydeliver. It is always built and
// registered, so a queued announcement never waits forever for a claimant; a
// channel switched off instance-wide then simply delivers nothing.
func buildAlbumFeaturedService(cfg *config.Config, db *database.DB) *albumfeaturedjob.Service {
	return albumfeaturedjob.New(albumfeaturedjob.Config{
		DB:      db.Pool(),
		Albums:  organize.NewStore(db.Pool()),
		Deliver: buildDeliverer(cfg, db),
	})
}
