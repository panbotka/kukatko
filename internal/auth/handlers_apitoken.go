package auth

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// apiTokenTargetType names the audited entity kind for API tokens.
const apiTokenTargetType = "api_tokens"

// createAPITokenRequest is the JSON body of POST /auth/tokens. Omitting
// expires_at mints a token that never expires.
type createAPITokenRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// createAPITokenResponse is returned by POST /auth/tokens. Secret is the
// plaintext credential and is the only time it is ever disclosed: the server
// stores nothing but its hash.
type createAPITokenResponse struct {
	Token  APIToken `json:"token"`
	Secret string   `json:"secret"`
}

// listAPITokensResponse is returned by GET /auth/tokens; it never carries
// secrets, only their metadata.
type listAPITokensResponse struct {
	Tokens []APIToken `json:"tokens"`
}

// apiTokenCreateLimitKey namespaces token minting inside the login rate limiter,
// which is reused here rather than standing up a second limiter. The key is
// scoped per user and client IP, exactly as the login key is.
func apiTokenCreateLimitKey(userUID, ip string) string {
	return "apitoken:" + userUID + "|" + ip
}

// handleCreateAPIToken mints an API token for the authenticated caller and
// returns the plaintext credential exactly once. It responds 201, 400 (bad body,
// empty name, or expiry in the past), 429 (rate limited), or 500.
func (a *API) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !a.limiter.Allow(apiTokenCreateLimitKey(p.user.UID, clientIP(r)), a.now()) {
		writeError(w, http.StatusTooManyRequests, "too many token creations; try again later")
		return
	}

	var req createAPITokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	entry := audit.FromRequest(r, p.user.UID).Entry(audit.ActionAPITokenCreate, apiTokenTargetType, "", nil)
	tok, secret, err := a.svc.CreateAPIToken(r.Context(), p.user.UID, CreateAPITokenInput(req), entry)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, createAPITokenResponse{Token: tok, Secret: secret})
	case errors.Is(err, ErrAPITokenNameRequired), errors.Is(err, ErrAPITokenExpiryInPast):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("auth: creating api token: %v", err)
		writeError(w, http.StatusInternalServerError, "could not create api token")
	}
}

// handleListAPITokens returns the authenticated caller's own tokens, never their
// secrets. It responds 200 or 500.
func (a *API) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tokens, err := a.svc.ListAPITokens(r.Context(), p.user.UID)
	if err != nil {
		log.Printf("auth: listing api tokens: %v", err)
		writeError(w, http.StatusInternalServerError, "could not list api tokens")
		return
	}
	writeJSON(w, http.StatusOK, listAPITokensResponse{Tokens: tokens})
}

// handleRevokeAPIToken revokes a token owned by the caller, or any token when
// the caller is an admin. Someone else's token is a 404, not a 403, so a
// non-admin cannot probe which ids exist. It responds 204 (also for an already
// revoked token: revocation is idempotent), 404, or 500.
func (a *API) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := chi.URLParam(r, "id")
	entry := audit.FromRequest(r, p.user.UID).Entry(audit.ActionAPITokenRevoke, apiTokenTargetType, id, nil)
	err := a.svc.RevokeAPIToken(r.Context(), id, p.user, entry)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrAPITokenNotFound):
		writeError(w, http.StatusNotFound, "api token not found")
	default:
		log.Printf("auth: revoking api token: %v", err)
		writeError(w, http.StatusInternalServerError, "could not revoke api token")
	}
}
