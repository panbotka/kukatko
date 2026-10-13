package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/server"
)

// projectAPI is the route registration of every project-scoped API group —
// everything that reads or writes catalogue data. Each group is built once
// (one service, one rate limiter, one cache) and mounted twice by
// projectRoutes: under /api/v1/p/{project} and, for the transition, unprefixed
// under /api/v1. Instance-wide routes (/auth/*, /users/{uid}/avatar,
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
	places        func(chi.Router)
	maps          func(chi.Router)
	upload        func(chi.Router) // POST /upload
	uploadLinks   func(chi.Router) // the /upload-links management half only
	notifications func(chi.Router) // the /notifications inbox half only
	capabilities  func(chi.Router)
	settings      func(chi.Router)
	announcement  func(chi.Router)
	users         func(chi.Router) // GET /users, the name directory
	members       func(chi.Router) // /members, the project admin's member management
	invites       func(chi.Router) // /invites, the project admin's invitation links
	audit         func(chi.Router) // /audit, the project's own audit trail
	imports       func(chi.Router)
	process       func(chi.Router)
	mcp           func(chi.Router)
}

// mountProjectAPI registers every project-scoped group of api on r, the router
// the caller has scoped to one project (or to the transition alias). Every
// field of api must be set: a nil one is a wiring bug and panics at start-up,
// which make dev catches.
func mountProjectAPI(r chi.Router, api *projectAPI) {
	for _, register := range []func(chi.Router){
		api.photos, api.globalSearch, api.organize, api.people, api.family, api.avatars,
		api.clusters, api.review, api.sweep, api.candidates, api.outliers, api.duplicates,
		api.dupMarkers, api.expand, api.bulk, api.tasks, api.feedback, api.savedSearches,
		api.searchHistory, api.whatsNew, api.places, api.maps, api.upload, api.uploadLinks,
		api.notifications, api.capabilities, api.settings, api.announcement, api.users,
		api.members, api.invites, api.audit, api.imports, api.process, api.mcp,
	} {
		register(r)
	}
}

// projectRoutes returns the server option mounting api twice inside /api/v1:
// under /p/{project}, where auth.WithProject names the project by its slug
// (and records the prefix, so the links a handler builds lead back through
// it), and unprefixed, as the transition alias. The alias needs no middleware
// of its own: the auth.WithProject every /api/v1 route already passes (see
// requestOptions) finds no {project} parameter and resolves the bootstrap
// project `v`. Under /p/{project} that outer resolution is redone by the inner
// one, which wins.
//
// The alias leaves out two groups. The name directory GET /users: its
// unprefixed path belongs to the system administrator's instance-wide account
// list (internal/projectapi), so the directory is served only under
// /p/{project} — where the frontend asks for it, and ctl in its context's
// project. The project's audit trail GET /audit(/mine): its unprefixed path is
// the maintainer's instance listing (and an account's own actions everywhere).
// The member management /members and
// the invitation links /invites, which are new with projects and have no old
// client to keep working. And the settings and the announcement, which are a
// project's own and are read only through its prefix — the unprefixed
// GET /settings/public is the instance-level route of the sign-in screen
// (settingsapi.RegisterInstanceRoutes).
func projectRoutes(store *project.Store, api *projectAPI) server.Option {
	alias := aliasAPI(api)
	return server.WithAPI(func(r chi.Router) {
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(store))
			mountProjectAPI(r, api)
		})
		// transition alias — removed in the alias-removal step
		mountProjectAPI(r, alias)
	})
}

// aliasAPI is the copy of api the unprefixed transition alias mounts: every
// group but the name directory, whose unprefixed GET /users is the instance
// account list, and the member management, invitation links, settings and
// announcement, which live only under a project, and the project's audit trail,
// whose unprefixed /audit is the maintainer's instance listing (see
// projectRoutes).
func aliasAPI(api *projectAPI) *projectAPI {
	alias := *api
	alias.audit = func(chi.Router) {}
	alias.users = func(chi.Router) {}
	alias.members = func(chi.Router) {}
	alias.invites = func(chi.Router) {}
	alias.settings = func(chi.Router) {}
	alias.announcement = func(chi.Router) {}
	return &alias
}
