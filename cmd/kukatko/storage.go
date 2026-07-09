package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/storage"
)

// newStorage builds the configured originals backend: the local filesystem by
// default, or Cloudflare R2 when storage.backend is "r2". Every command that
// touches originals goes through here, so a single config key moves the whole
// process off the local disk.
//
// config.Validate has already rejected an unknown backend and an R2 backend with
// a missing key, so an error here means the backend could not be reached or its
// directories could not be created.
func newStorage(cfg *config.Config) (storage.Storage, error) {
	switch cfg.Storage.Backend {
	case config.StorageBackendR2:
		store, err := storage.NewR2(storage.R2Options{
			Endpoint:                 cfg.Storage.R2.Endpoint,
			Region:                   cfg.Storage.R2.Region,
			Bucket:                   cfg.Storage.R2.Bucket,
			AccessKey:                cfg.Storage.R2.AccessKey,
			SecretKey:                cfg.Storage.R2.SecretKey,
			MediaBaseURL:             cfg.Storage.R2.MediaBaseURL,
			URLSigningSecret:         cfg.Storage.R2.URLSigningSecret,
			URLSigningSecretPrevious: cfg.Storage.R2.URLSigningSecretPrevious,
			URLTTL:                   cfg.Storage.R2.URLTTL,
			TempPath:                 cfg.Storage.TempPath,
		})
		if err != nil {
			return nil, fmt.Errorf("initialising R2 storage: %w", err)
		}
		return store, nil
	default:
		store, err := storage.NewFS(cfg.Storage.OriginalsPath)
		if err != nil {
			return nil, fmt.Errorf("initialising originals storage: %w", err)
		}
		return store, nil
	}
}
