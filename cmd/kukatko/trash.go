package main

import (
	"time"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/trash"
)

// trashPurgeInterval is how often the scheduled retention purge runs. Retention
// is configured in whole days, so checking a few times a day is ample while
// keeping the trash from lingering far past its expiry.
const trashPurgeInterval = 6 * time.Hour

// buildTrashService assembles the purge service over the originals store, the
// thumbnailer and the photo repository. It performs the permanent deletion of
// soft-deleted photos for both the scheduled retention purge and the manual
// admin/editor controls (purge one, empty trash).
//
// No remote object store is wired yet: S3 backup is a separate subsystem with no
// uploader defining the object layout, so trash.Config.Remote is left nil and
// remote backup objects are not purged until that subsystem lands.
func buildTrashService(cfg *config.Config, db *database.DB) (*trash.Service, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	return trash.New(trash.Config{
		Photos:        photos.NewStore(db.Pool()),
		Storage:       store,
		Thumbnailer:   thumb.New(store, cfg.Storage.CachePath),
		RetentionDays: cfg.Trash.RetentionDays,
	}), nil
}
