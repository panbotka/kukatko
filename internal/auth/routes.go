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
