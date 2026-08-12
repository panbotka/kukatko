package main

import (
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/capabilitiesapi"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/reachability"
	"github.com/panbotka/kukatko/internal/version"
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
//
// It probes the host that answers /embed/text, which is what the flag is about:
// with embedding.text_url pointing at an always-on text instance, semantic search
// keeps working while the GPU box sleeps, and probing the box would wrongly grey
// the mode out. This is the one health probe that follows the text URL — the one
// internal/wake reads must stay on the box, or the box would never be woken.
func buildReachabilityChecker(cfg *config.Config) (*reachability.Checker, error) {
	textURL := textEmbeddingURL(cfg)
	if textURL == "" {
		return reachability.New(reachability.Config{}), nil
	}
	clientCfg := embeddingClientConfig(cfg)
	clientCfg.BaseURL = textURL
	client, err := embedding.New(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("capabilities: building embedding health client: %w", err)
	}
	return reachability.New(reachability.Config{Health: client, Enabled: true}), nil
}

// buildCapabilitiesAPI mounts GET /capabilities, an all-authenticated view of the
// instance feature flags (currently semantic search) backed by the cached
// reachability checker, plus the build metadata of this binary — the one source
// of truth for the version the UI prints, read from the server that runs it.
func buildCapabilitiesAPI(checker *reachability.Checker, authAPI *auth.API) *capabilitiesapi.API {
	return capabilitiesapi.NewAPI(capabilitiesapi.Config{
		Embeddings:  checker,
		Build:       version.Get(),
		RequireAuth: authAPI.RequireAuth,
	})
}
