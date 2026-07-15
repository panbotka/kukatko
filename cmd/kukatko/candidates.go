package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/candidatesapi"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildCandidatesAPI assembles the "find a person among untagged photos" search over
// the shared pool: the candidates service (which votes over each of a subject's
// exemplars' unassigned-face neighbours) behind its editor/admin endpoint. The media
// store stamps the candidate photos' URLs; the write guard is supplied via authAPI
// so the candidatesapi package stays decoupled from auth's wiring.
func buildCandidatesAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage,
) *candidatesapi.API {
	return candidatesapi.NewAPI(candidatesapi.Config{
		Service:      buildCandidatesService(cfg, db, mediaStore),
		RequireWrite: authAPI.RequireWrite,
	})
}

// buildCandidatesService assembles the read-only untagged-face candidate search over
// the shared pool: the four stores plus the media builder, tuned from cfg.Candidates
// (with the relative face-size floor reused from cfg.Faces). The recognition sweep
// reuses the same service as its per-subject finder, so building it here keeps the
// two call sites from drifting.
func buildCandidatesService(
	cfg *config.Config, db *database.DB, mediaStore storage.Storage,
) *candidates.Service {
	return candidates.New(candidates.Config{
		Faces:       vectors.NewStore(db.Pool()),
		People:      people.NewStore(db.Pool()),
		Feedback:    feedback.NewStore(db.Pool()),
		Photos:      photos.NewStore(db.Pool()),
		Media:       mediaurl.NewBuilder(mediaStore),
		MaxDistance: cfg.Candidates.MaxDistance,
		SearchLimit: cfg.Candidates.SearchLimit,
		MinFacePx:   cfg.Candidates.MinFacePx,
		Concurrency: cfg.Candidates.Concurrency,
		MinFaceRel:  cfg.Faces.MinFaceSize,
	})
}
