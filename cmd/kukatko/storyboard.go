package main

import (
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storyboard"
	"github.com/panbotka/kukatko/internal/storyboardjob"
)

// buildStoryboardService assembles the video scrub-preview subsystem: the sprite
// generator over the shared originals store and the local derived-media cache,
// plus the service that reads a photo's storyboard status and schedules its lazy
// generation. One instance backs both the photo API's two storyboard routes and
// the worker's `storyboard` handler, so the reader and the renderer always agree
// on the cache layout.
//
// It needs no configuration of its own: the sprite is derived media under
// storage.cache_path, and a host without ffmpeg simply reports every storyboard as
// unavailable.
func buildStoryboardService(
	cfg *config.Config, db *database.DB, store storage.Storage, enqueuer *jobs.Enqueuer,
) *storyboardjob.Service {
	return storyboardjob.New(storyboardjob.Config{
		Photos:    photos.NewStore(db.Pool()),
		Generator: storyboard.New(store, cfg.Storage.CachePath),
		Enqueuer:  enqueuer,
	})
}
