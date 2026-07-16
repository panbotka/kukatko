package mcpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
)

// errNoCaller means a tool handler ran without an identified caller. It can only
// happen if the endpoint is mounted without withCaller in front of it, so it is a
// wiring bug rather than a user error — but the tools fail closed on it anyway,
// because the alternative is a mutation attributed to nobody.
var errNoCaller = errors.New("kukatko: no authenticated caller on this request")

// errReadOnly is what a caller without write permission gets from a write tool.
// It names the roles that would work, because the agent's next move is to tell
// its human which token to mint.
var errReadOnly = errors.New(
	"this token is read-only: writing to the library needs a token owned by an editor, admin or ai user",
)

// viaMCP is stamped into every audit entry this package writes. The actor UID
// already says which token acted, but "which token" and "through which door" are
// different questions, and the second one is the whole reason an audit trail is
// worth reading after an agent has been let loose on the library.
const viaMCP = "mcp"

// caller is the identity behind a tool call: the authenticated user, and the
// audit metadata of the HTTP request that carried the call. It is assembled once
// per request by withCaller, because a tool handler sees only a context — the
// *http.Request the audit trail needs (IP, user agent) does not reach it.
type caller struct {
	user auth.User
	meta audit.Meta
}

// entry builds an audit entry for a mutation this caller is making, so every
// change an agent makes is attributed exactly like a human's.
func (c caller) entry(action, targetType, targetUID string, details map[string]any) audit.Entry {
	return c.meta.Entry(action, targetType, targetUID, details)
}

// callerKey is this package's private context key type; an empty struct type
// cannot collide with a key from any other package.
type callerKey struct{}

// withCaller resolves the authenticated principal into a caller and puts it on
// the request context, where the MCP tool handlers can reach it. It runs behind
// RequireAuth, so an absent principal is a wiring bug and answers 401 rather than
// letting an anonymous call through.
func (a *API) withCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		c := caller{user: user, meta: audit.FromRequest(r, user.UID)}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, c)))
	})
}

// callerFromContext returns the caller withCaller attached to ctx.
func callerFromContext(ctx context.Context) (caller, error) {
	c, ok := ctx.Value(callerKey{}).(caller)
	if !ok {
		return caller{}, errNoCaller
	}
	return c, nil
}

// writerFromContext returns the caller only if it may write, and errReadOnly
// otherwise. Every write tool starts with it. The write tools are already absent
// from a read-only caller's server, so this is the second lock on the same door:
// it keeps the role boundary true even if that registration ever changes.
func writerFromContext(ctx context.Context) (caller, error) {
	c, err := callerFromContext(ctx)
	if err != nil {
		return caller{}, err
	}
	if !c.user.Role.CanWrite() {
		return caller{}, errReadOnly
	}
	return c, nil
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("mcpapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
