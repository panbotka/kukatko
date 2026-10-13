package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/server"
)

// projectAPI is the route registration of every project-scoped API group —
// everything that reads or writes catalogue data. Each group is built once
// (one service, one rate limiter, one cache) and mounted once by projectRoutes,
// under /api/v1/p/{project}; there is no unprefixed copy, so a project-scoped
// path without the prefix is an unknown API path (404). Instance-wide routes (/auth/*, /users/{uid}/avatar,
// /auth/picture, /push/*, /jobs, /system, /backup, /maintenance, /restore,
// /audit, the public /u/{code}) are not here: they are mounted once, with
// server.WithAPI.
//
// A new project-scoped group gets a field here and a line in mountProjectAPI;
// TestMountProjectAPI_mountsEveryGroup fails when the second is forgotten.
type projectAPI struct {
	photos        func(chi.Router) // photoapi, incl. its trash and stacks routes
	globalSearch  func(chi.Router)
	organize      func(chi.Router)
	people        func(chi.Router)
	family        func(chi.Router)
	avatars       func(chi.Router) // GET /subjects/{uid}/avatar
	userPictures  func(chi.Router) // GET /users/{uid}/avatar, GET /auth/picture: the per-project reads
	maintenance   func(chi.Router) // /maintenance/nameless-subjects*, the project's nameless repair
	clusters      func(chi.Router)
	review        func(chi.Router)
	sweep         func(chi.Router)
	candidates    func(chi.Router)
	outliers      func(chi.Router)
	duplicates    func(chi.Router)
	dupMarkers    func(chi.Router)
	expand        func(chi.Router)
	bulk          func(chi.Router)
	tasks         func(chi.Router)
	feedback      func(chi.Router)
	savedSearches func(chi.Router)
	searchHistory func(chi.Router)
	whatsNew      func(chi.Router)
	stats         func(chi.Router) // GET /stats, /stats/charts: the library statistics
	places        func(chi.Router)
	maps          func(chi.Router)
	upload        func(chi.Router) // POST /upload
	uploadLinks   func(chi.Router) // the /upload-links management half only
	notifications func(chi.Router) // the /notifications inbox half only
	capabilities  func(chi.Router)
	settings      func(chi.Router)
	announcement  func(chi.Router)
	users         func(chi.Router) // GET /users, the name directory, and PUT /auth/subject
	members       func(chi.Router) // /members, the project admin's member management
	invites       func(chi.Router) // /invites, the project admin's invitation links
	audit         func(chi.Router) // /audit, the project's own audit trail
	imports       func(chi.Router)
	process       func(chi.Router)
	mcp           func(chi.Router)
}

// mountProjectAPI registers every project-scoped group of api on r, the router
// the caller has scoped to one project. Every
// field of api must be set: a nil one is a wiring bug and panics at start-up,
// which make dev catches.
func mountProjectAPI(r chi.Router, api *projectAPI) {
	for _, register := range []func(chi.Router){
		api.photos, api.globalSearch, api.organize, api.people, api.family, api.avatars, api.userPictures,
		api.maintenance,
		api.clusters, api.review, api.sweep, api.candidates, api.outliers, api.duplicates,
		api.dupMarkers, api.expand, api.bulk, api.tasks, api.feedback, api.savedSearches,
		api.searchHistory, api.whatsNew, api.stats, api.places, api.maps, api.upload, api.uploadLinks,
		api.notifications, api.capabilities, api.settings, api.announcement, api.users,
		api.members, api.invites, api.audit, api.imports, api.process, api.mcp,
	} {
		register(r)
	}
}

// projectRoutes returns the server option mounting api inside /api/v1 under
// /p/{project}, where auth.WithProject names the project by its slug (and
// records the prefix, so the links a handler builds lead back through it)
// before any Require* guard decides whether the caller may enter it.
func projectRoutes(store *project.Store, api *projectAPI) server.Option {
	return server.WithAPI(func(r chi.Router) {
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(store))
			mountProjectAPI(r, api)
		})
	})
}
