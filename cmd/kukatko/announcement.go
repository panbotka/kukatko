package main

import (
	"github.com/panbotka/kukatko/internal/announcement"
	"github.com/panbotka/kukatko/internal/announcementapi"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
)

// buildAnnouncementAPI assembles the instance-wide announcement HTTP API over the
// shared pool: any signed-in user reads the current banner, and a maintainer
// publishes or clears it. The read guard and the maintainer guard are supplied via
// authAPI so announcementapi stays decoupled from auth's wiring, and publish/clear
// are audited in the same transaction as the change by the store.
func buildAnnouncementAPI(db *database.DB, authAPI *auth.API) *announcementapi.API {
	return announcementapi.NewAPI(announcementapi.Config{
		Store:             announcement.NewStore(db.Pool()),
		RequireAuth:       authAPI.RequireAuth,
		RequireMaintainer: authAPI.RequireMaintainer,
	})
}
