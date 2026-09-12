package familyapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/family"
)

// handleRelations returns the subject's four derived lists — parents, siblings,
// partners and children — each entry carrying the person plus their cover and
// photo count, so the strip on the subject page renders from this one response.
// An unknown subject answers 404.
func (a *API) handleRelations(w http.ResponseWriter, r *http.Request) {
	relations, err := a.store.Relations(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		status, msg := familyStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, relations)
}

// handleTree returns the tree walked from the subject in the requested direction,
// bounded by the requested number of generations. A malformed parameter answers
// 400 and an unknown subject 404.
func (a *API) handleTree(w http.ResponseWriter, r *http.Request) {
	direction, generations, err := parseTreeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tree, err := a.store.Tree(r.Context(), chi.URLParam(r, "uid"), direction, generations)
	if err != nil {
		status, msg := familyStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

// handleAddRelation records a relation on the subject named in the path: the
// other person is either an existing subject or one described in the body and
// created by the same transaction. It answers 201 with the family the relation
// landed in and the other person as a chip renders them.
//
// A malformed body answers 400, an unknown subject 404, and a refusal about the
// state of the tree — a cycle, a second parentage — 409. The audit details the
// store fills in (the role, who the other person turned out to be, which family
// the relation joined) are stamped into the map handed over here, inside the
// mutation's transaction.
func (a *API) handleAddRelation(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	rel, err := decodeAddRelation(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry := a.auditEntry(r, audit.ActionSubjectRelationAdd, "subjects", uid, map[string]any{})
	result, err := a.store.AddRelationAudited(r.Context(), uid, rel, entry)
	if err != nil {
		status, msg := familyStatus(err)
		writeError(w, status, msg)
		return
	}
	a.enqueueExport(r.Context())
	writeJSON(w, http.StatusCreated, result)
}

// handleRemoveRelation removes whatever relation ties the two subjects in the
// path together — which one it is follows from the rows, not from the request —
// and answers 204. Two subjects that are not related at all answer 404, so a
// client is never told something was removed when nothing was.
func (a *API) handleRemoveRelation(w http.ResponseWriter, r *http.Request) {
	uid, otherUID := chi.URLParam(r, "uid"), chi.URLParam(r, "uid2")
	entry := a.auditEntry(r, audit.ActionSubjectRelationRemove, "subjects", uid,
		map[string]any{"other_uid": otherUID})
	if err := a.store.RemoveRelationAudited(r.Context(), uid, otherUID, entry); err != nil {
		status, msg := familyStatus(err)
		writeError(w, status, msg)
		return
	}
	a.enqueueExport(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// handleUpdateFamily rewrites a family's editable fields — whether it was a
// marriage, the years it lasted, the note on it — and returns the refreshed
// family. The family is loaded first so the audit entry can record each changed
// field's old→new transition and so a missing family answers 404 before any
// mutation. The body rewrites the whole editable set, so an omitted year clears
// it. A malformed body or an impossible year answers 400.
func (a *API) handleUpdateFamily(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	existing, err := a.store.GetFamily(r.Context(), uid)
	if err != nil {
		status, msg := familyStatus(err)
		writeError(w, status, msg)
		return
	}
	upd, err := decodeFamilyUpdate(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	details := map[string]any{"family_uid": uid}
	familyChanges(existing, upd).StampInto(details)
	entry := a.auditEntry(r, audit.ActionFamilyUpdate, "families", uid, details)
	updated, err := a.store.UpdateFamilyAudited(r.Context(), uid, upd, entry)
	if err != nil {
		status, msg := familyStatus(err)
		writeError(w, status, msg)
		return
	}
	a.enqueueExport(r.Context())
	writeJSON(w, http.StatusOK, updated)
}

// familyChanges builds the old→new diff for a family edit, comparing the family
// before the edit against the update the store will apply and recording only the
// fields whose value changed. The kind is compared as its string form so the
// recorded values read plainly. The result is stamped under the audit "changes"
// key (see internal/audit ChangeSet).
func familyChanges(before family.Family, after family.Update) *audit.ChangeSet {
	changes := audit.NewChangeSet()
	changes.Add("kind", string(before.Kind), string(after.Kind))
	changes.Add("from_year", before.FromYear, after.FromYear)
	changes.Add("to_year", before.ToYear, after.ToYear)
	changes.Add("note", before.Note, after.Note)
	return changes
}
