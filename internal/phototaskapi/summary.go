package phototaskapi

import "net/http"

// handleSummary writes the queue's counts as the caller sees them: per state,
// open, waiting on them, answered. Every authenticated role may read it — it is
// the number on the navigation badge, and a viewer who was asked a question is
// exactly who the badge is for.
func (a *API) handleSummary(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	summary, err := a.store.Summary(r.Context(), user.UID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "summarising tasks failed")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
