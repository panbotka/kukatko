package main

import (
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/capabilitiesapi"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/reachability"
)

// capabilitiesCheckInterval is how often the embeddings-reachability loop probes
// the sidecar to refresh the cached flag GET /capabilities reports. Like the
// auto-wake loop it is a plain constant, not a config key: the flag is purely
// presentational (it only shows or hides the semantic-search hint), so a 60s
// granularity for the box appearing on- or offline is plenty.
const capabilitiesCheckInterval = time.Minute

// buildReachabilityChecker constructs the background embeddings-reachability
// checker. When no embedding URL is configured the checker is inert and always
// reports unreachable (no client is built, so semantic search is never
// advertised); otherwise it reuses the same lightweight embedding client
// construction as the other services for its cheap Healthy probe. A configuration
// error surfaces only for a malformed URL.
func buildReachabilityChecker(cfg *config.Config) (*reachability.Checker, error) {
	if cfg.Embedding.URL == "" {
		return reachability.New(reachability.Config{}), nil
	}
	client, err := embedding.New(embedding.Config{
		BaseURL:  cfg.Embedding.URL,
		ImageDim: cfg.Embedding.ImageDim,
		FaceDim:  cfg.Embedding.FaceDim,
	})
	if err != nil {
		return nil, fmt.Errorf("capabilities: building embedding health client: %w", err)
	}
	return reachability.New(reachability.Config{Health: client, Enabled: true}), nil
}

// buildCapabilitiesAPI mounts GET /capabilities, an all-authenticated view of the
// instance feature flags (currently semantic search) backed by the cached
// reachability checker.
func buildCapabilitiesAPI(checker *reachability.Checker, authAPI *auth.API) *capabilitiesapi.API {
	return capabilitiesapi.NewAPI(capabilitiesapi.Config{
		Embeddings:  checker,
		RequireAuth: authAPI.RequireAuth,
	})
}
