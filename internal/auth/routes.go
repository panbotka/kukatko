package auth

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the auth and admin user-management endpoints onto r,
// which the caller has already scoped under the API base path (for example
// /api/v1). The layout is:
//
//	POST   /auth/login            public
//	POST   /auth/logout           public (idempotent)
//	GET    /auth/me               RequireAuth
//	POST   /auth/password         RequireAuth
//	PUT    /auth/subject          RequireAuth
//	POST   /auth/welcome-seen     RequireAuth
//	POST   /auth/tokens           RequireAuth
//	GET    /auth/tokens           RequireAuth
//	DELETE /auth/tokens/{id}      RequireAuth
//	GET    /admin/users           RequireAdmin
//	POST   /admin/users           RequireAdmin
//	PATCH  /admin/users/{uid}     RequireAdmin
//	POST   /admin/users/{uid}/disable    RequireAdmin
//	POST   /admin/users/{uid}/password   RequireAdmin
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Post("/login", a.handleLogin)
		r.Post("/logout", a.handleLogout)
		r.With(a.RequireAuth).Get("/me", a.handleMe)
		r.With(a.RequireAuth).Post("/password", a.handlePassword)
		// Self-service and self-scoped: the account it changes is the session's,
		// so it needs no role beyond being signed in — a viewer is as much a
		// person in the photographs as a maintainer is.
		r.With(a.RequireAuth).Put("/subject", a.handleSubject)
		// Self-service for the same reason, and needs no role for a second one:
		// the welcome is shown to everybody who signs in, so everybody who signs
		// in must be able to say they have read it.
		r.With(a.RequireAuth).Post("/welcome-seen", a.handleWelcomeSeen)
		r.Route("/tokens", func(r chi.Router) {
			r.Use(a.RequireAuth)
			r.Post("/", a.handleCreateAPIToken)
			r.Get("/", a.handleListAPITokens)
			r.Delete("/{id}", a.handleRevokeAPIToken)
		})
	})

	r.Route("/admin/users", func(r chi.Router) {
		r.Use(a.RequireAdmin)
		r.Get("/", a.handleListUsers)
		r.Post("/", a.handleCreateUser)
		r.Patch("/{uid}", a.handleUpdateUser)
		r.Post("/{uid}/disable", a.handleDisableUser)
		r.Post("/{uid}/password", a.handleResetPassword)
	})
}
