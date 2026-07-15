package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/feedbackapi"
)

// buildFeedbackAPI assembles the persisted-rejection HTTP API over the shared pool:
// rejecting (and un-rejecting) a face↔subject or photo↔label guess. Every endpoint
// mutates and is guarded by the write guard supplied via authAPI, so feedbackapi
// stays decoupled from auth's wiring; each write is audited in the same transaction
// as the rejection.
func buildFeedbackAPI(db *database.DB, authAPI *auth.API) *feedbackapi.API {
	store := feedback.NewStore(db.Pool())
	return feedbackapi.NewAPI(feedbackapi.Config{
		Store:        store,
		RequireWrite: authAPI.RequireWrite,
	})
}
