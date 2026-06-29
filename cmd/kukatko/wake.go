package main

import (
	"context"
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/wake"
)

// wakeCheckInterval is how often the auto-wake loop polls the queue depth and,
// when work is waiting and the box is offline, attempts a Wake-on-LAN. The
// cooldown (not this interval) bounds how often packets are actually sent.
const wakeCheckInterval = time.Minute

// embedJobCounter adapts the job store to wake.QueueDepth, counting pending
// image_embed and face_detect jobs — the work that needs the embeddings box.
type embedJobCounter struct {
	store *jobs.Store
}

// PendingEmbeddingJobs returns how many image_embed/face_detect jobs are queued
// or running.
func (c embedJobCounter) PendingEmbeddingJobs(ctx context.Context) (int, error) {
	n, err := c.store.CountPending(ctx, jobs.TypeImageEmbed, jobs.TypeFaceDetect)
	if err != nil {
		return 0, fmt.Errorf("wake: counting pending embedding jobs: %w", err)
	}
	return n, nil
}

// buildWakeService constructs the optional Wake-on-LAN auto-wake service from
// configuration. It uses the job store for queue depth and a lightweight
// embeddings client for sidecar health probing. It returns an inert service when
// wake is disabled; a configuration error surfaces here (config validation
// already checks the MAC, so this typically only fails on a bad interface name).
func buildWakeService(cfg *config.Config, db *database.DB) (*wake.Service, error) {
	w := cfg.Embedding.Wake
	client, err := embedding.New(embedding.Config{
		BaseURL:  cfg.Embedding.URL,
		ImageDim: cfg.Embedding.ImageDim,
		FaceDim:  cfg.Embedding.FaceDim,
	})
	if err != nil {
		return nil, fmt.Errorf("wake: building embedding health client: %w", err)
	}
	svc, err := wake.New(wake.Config{
		Enabled:       w.Enabled,
		MAC:           w.MAC,
		BroadcastAddr: w.BroadcastAddr,
		Interface:     w.Interface,
		MinQueue:      w.MinQueue,
		Cooldown:      w.Cooldown,
		Queue:         embedJobCounter{store: jobs.NewStore(db.Pool())},
		Health:        client,
	})
	if err != nil {
		return nil, fmt.Errorf("wake: building service: %w", err)
	}
	return svc, nil
}
