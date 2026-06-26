package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/photoapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// buildPhotoAPI assembles the photo browse/curation subsystem: the on-disk
// original store and thumbnailer (for media serving), the photo repository, and
// the HTTP API. Read endpoints reuse the auth subsystem's RequireAuth guard,
// metadata and archive endpoints its RequireWrite guard, and media endpoints its
// RequireAuthOrDownloadToken guard (cookie or download token) — all supplied via
// authAPI so the photoapi package stays decoupled from auth's wiring.
func buildPhotoAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) (*photoapi.API, error) {
	store, err := storage.NewFS(cfg.Storage.OriginalsPath)
	if err != nil {
		return nil, fmt.Errorf("initialising originals storage: %w", err)
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath)
	photoStore := photos.NewStore(db.Pool())

	return photoapi.NewAPI(photoapi.Config{
		Store:           photoStore,
		Storage:         store,
		Thumbnailer:     thumbnailer,
		RequireAuth:     authAPI.RequireAuth,
		RequireWrite:    authAPI.RequireWrite,
		RequireDownload: authAPI.RequireAuthOrDownloadToken,
	}), nil
}
