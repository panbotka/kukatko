//go:build integration

package auth

import (
	"context"
	"net/http"

	"github.com/panbotka/kukatko/internal/project"
)

// ContextWithProject returns ctx carrying user as the authenticated caller
// inside project p with role — exactly what the Require* guards leave behind
// for a request they admitted. It exists for the integration tests of packages
// that mount their handlers without the auth stack, now that a project-scoped
// store refuses to run without a project. Like password_cost_integration.go it
// is compiled only under the integration tag, so no production binary can
// forge a principal with it.
func ContextWithProject(ctx context.Context, user User, p *project.Project, role project.Role) context.Context {
	return withPrincipal(ctx, principal{user: user, project: p, projectRole: role})
}

// AdmitInProject returns a guard that admits every request as user inside
// project p with role (see ContextWithProject): the stand-in for RequireAuth in
// an integration test that is about the handler, not about authorisation.
func AdmitInProject(user User, p *project.Project, role project.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ContextWithProject(r.Context(), user, p, role)))
		})
	}
}
