package auth

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the auth and admin user-management endpoints onto r,
// which the caller has already scoped under the API base path (for example
// /api/v1). The layout is:
//
//	POST   /auth/login            public
//	POST   /auth/register         public, rate-limited per address
//	POST   /auth/logout           public (idempotent)
//	GET    /auth/password-reset/{token}   public, rate-limited per address
//	POST   /auth/password-reset/{token}   public, rate-limited per address
//	GET    /auth/me               RequireSignedIn
//	POST   /auth/password         RequireSignedIn
//	POST   /auth/welcome-seen     RequireSignedIn
//	POST   /auth/passkeys/register/begin   RequireSignedIn
//	POST   /auth/passkeys/register/finish  RequireSignedIn
//	POST   /auth/passkeys/login/begin      public, rate-limited per address
//	POST   /auth/passkeys/login/finish     public, rate-limited per address
//	GET    /auth/passkeys         RequireSignedIn
//	DELETE /auth/passkeys/{id}    RequireSignedIn
//	POST   /auth/tokens           RequireSignedIn
//	GET    /auth/tokens           RequireSignedIn
//	PATCH  /auth/tokens/{id}      RequireSignedIn (admin-only in the handler)
//	DELETE /auth/tokens/{id}      RequireSignedIn
//	GET    /admin/users           RequireMaintainer
//	POST   /admin/users           RequireMaintainer
//	PATCH  /admin/users/{uid}     RequireMaintainer
//	PUT    /admin/users/{uid}/username   RequireMaintainer
//	POST   /admin/users/{uid}/disable    RequireMaintainer
//	POST   /admin/users/{uid}/password   RequireMaintainer
//	POST   /admin/users/{uid}/password-reset RequireMaintainer
//
// and, in a project (under /api/v1/p/{project}):
//
//	GET    /users                 RequireAuth (uid + name of the project's members)
//	PUT    /auth/subject          RequireAuth (the caller's "this is me" in the project)
//
// The server mounts the two halves apart: RegisterInstanceRoutes once under
// /api/v1, RegisterProjectRoutes with the project-scoped API under
// /api/v1/p/{project}. RegisterRoutes mounts both on one router, for a test
// that tells no project prefix apart.
func (a *API) RegisterRoutes(r chi.Router) {
	a.RegisterInstanceRoutes(r)
	a.RegisterProjectRoutes(r)
}

// RegisterInstanceRoutes mounts the instance-wide half of RegisterRoutes: every
// /auth/* route and the /admin/users account administration. They belong to
// an account, not to a project, so they are mounted once, without the project
// prefix.
func (a *API) RegisterInstanceRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Post("/login", a.handleLogin)
		// A write an anonymous caller may perform (the password-reset pair
		// below is the other), so it carries the per-address budget as
		// middleware rather than trusting the handler to remember. It is mounted even when no registration is wired: the
		// handler then answers the closed-door 403, which is the truth,
		// and a client never has to tell a missing route from a closed door.
		r.With(a.registerLimit.Middleware).Post("/register", a.handleRegister)
		r.Post("/logout", a.handleLogout)
		// The two halves of a password reset: both are reached by somebody who
		// is by definition locked out, so neither can require a session, and
		// both therefore carry the per-address budget as middleware. The token
		// in the path is the whole credential — nothing in the body names an
		// account — so there is nothing else to attribute a caller to.
		r.Route("/password-reset", func(r chi.Router) {
			r.Use(a.passwordResetLimit.Middleware)
			r.Get("/{token}", a.handlePasswordResetStatus)
			r.Post("/{token}", a.handlePasswordResetConsume)
		})
		r.With(a.RequireSignedIn).Get("/me", a.handleMe)
		r.With(a.RequireSignedIn).Post("/password", a.handlePassword)
		// Self-service and self-scoped: the account written to is the session's,
		// and the welcome is shown to everybody who signs in, so everybody who
		// signs in must be able to say they have read it.
		r.With(a.RequireSignedIn).Post("/welcome-seen", a.handleWelcomeSeen)
		// The two public halves of a passkey sign-in sit beside the two
		// authenticated halves of a passkey registration, because they are one
		// feature and a client discovers them together. Their rate limiting is
		// inside the handlers rather than middleware here: unlike registration
		// and password reset, the budget is charged against a key the handler
		// composes (see passkeyLoginLimitKey) and cleared again on success,
		// exactly as the password login does it.
		r.Route("/passkeys", func(r chi.Router) {
			r.Post("/login/begin", a.handleBeginPasskeyLogin)
			r.Post("/login/finish", a.handleFinishPasskeyLogin)
			r.With(a.RequireSignedIn).Post("/register/begin", a.handleBeginPasskeyRegistration)
			r.With(a.RequireSignedIn).Post("/register/finish", a.handleFinishPasskeyRegistration)
			r.With(a.RequireSignedIn).Get("/", a.handleListPasskeys)
			r.With(a.RequireSignedIn).Delete("/{id}", a.handleDeletePasskey)
		})
		r.Route("/tokens", func(r chi.Router) {
			r.Use(a.RequireSignedIn)
			r.Post("/", a.handleCreateAPIToken)
			r.Get("/", a.handleListAPITokens)
			// The rate-limit exemption is an admin's decision, but the role check
			// lives in the service rather than in a RequireAdmin here: minting a
			// token needs the very same check *conditionally* (only when the body
			// asks for the exemption), so keeping both in one place is what stops
			// the two paths from drifting apart.
			r.Patch("/{id}", a.handleUpdateAPIToken)
			r.Delete("/{id}", a.handleRevokeAPIToken)
		})
	})

	// The account record is instance-wide — one account, every project it
	// belongs to — so administering it is the system administrator's: a project
	// admin manages roles and approval of their own members through
	// /p/{project}/members (internal/memberapi).
	r.Route("/admin/users", func(r chi.Router) {
		r.Use(a.RequireMaintainer)
		r.Get("/", a.handleListUsers)
		r.Post("/", a.handleCreateUser)
		r.Patch("/{uid}", a.handleUpdateUser)
		r.Put("/{uid}/username", a.handleRenameUser)
		r.Post("/{uid}/disable", a.handleDisableUser)
		r.Post("/{uid}/password", a.handleResetPassword)
		r.Post("/{uid}/password-reset", a.handleIssuePasswordReset)
	})
}

// RegisterProjectRoutes mounts the project-scoped half of RegisterRoutes:
// GET /users, the people of the project for every signed-in role — uid and
// name only — and PUT /auth/subject, the caller's own "this is me" among the
// project's people. GET /users sits beside /users/{uid}/avatar
// (internal/userpicapi), which is the picture for the same uid, and
// deliberately apart from /admin/users, which is the administrative record of
// an account.
func (a *API) RegisterProjectRoutes(r chi.Router) {
	r.With(a.RequireAuth).Get("/users", a.handleDirectory)
	// Self-service and self-scoped: the membership it changes is the session
	// account's in the request's project, so it needs no role beyond being a
	// member — a viewer is as much a person in the photographs as an admin is.
	r.With(a.RequireAuth).Put("/auth/subject", a.handleSubject)
}
