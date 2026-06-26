package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildPhotoAPI assembles the photo browse/curation subsystem: the on-disk
// original store and thumbnailer (for media serving), the photo repository, and
// the HTTP API. Read endpoints reuse the auth subsystem's RequireAuth guard,
// metadata and archive endpoints its RequireWrite guard, and media endpoints its
// RequireAuthOrDownloadToken guard (cookie or download token) — all supplied via
// authAPI so the photoapi package stays decoupled from auth's wiring. similar is
// the shared vector store backing the similar-photos endpoint and the semantic
// half of search; embedder is the sidecar client that embeds query text for
// semantic and hybrid search. The face-matching service (face↔marker IoU matching,
// assignment and suggestions) is assembled over the shared pool and injected so the
// /photos/{uid}/faces endpoints work.
func buildPhotoAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API,
	similar photoapi.SimilarSearcher, embedder photoapi.TextEmbedder,
) (*photoapi.API, error) {
	store, err := storage.NewFS(cfg.Storage.OriginalsPath)
	if err != nil {
		return nil, fmt.Errorf("initialising originals storage: %w", err)
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath)
	photoStore := photos.NewStore(db.Pool())
	faceSvc := facematch.New(facematch.Config{
		Photos:                photoStore,
		Faces:                 vectors.NewStore(db.Pool()),
		People:                people.NewStore(db.Pool()),
		IoUThreshold:          cfg.Faces.IoUThreshold,
		SuggestionLimit:       cfg.Faces.SuggestionLimit,
		SuggestionMaxDistance: cfg.Faces.SuggestionMaxDistance,
		MinFaceSize:           cfg.Faces.MinFaceSize,
	})

	return photoapi.NewAPI(photoapi.Config{
		Store:           photoStore,
		Storage:         store,
		Thumbnailer:     thumbnailer,
		Similar:         similar,
		Embedder:        embedder,
		Faces:           faceSvc,
		RequireAuth:     authAPI.RequireAuth,
		RequireWrite:    authAPI.RequireWrite,
		RequireDownload: authAPI.RequireAuthOrDownloadToken,
	}), nil
}
