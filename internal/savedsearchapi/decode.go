package savedsearchapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/panbotka/kukatko/internal/savedsearch"
)

// maxBodyBytes caps the request body size for saved-search mutations. The params
// blob is opaque view state, so the limit is generous but still bounded.
const maxBodyBytes = 1 << 20 // 1 MiB

// errEmptyName is returned when a create/update request supplies a blank name,
// which the saved search requires for display.
var errEmptyName = errors.New("name is required")

// savedSearchView is the JSON shape returned for a saved search. It deliberately
// omits owner_uid: searches are always served scoped to the owner, so the field
// is redundant and is not surfaced to the client.
type savedSearchView struct {
	UID       string          `json:"uid"`
	Name      string          `json:"name"`
	Params    json.RawMessage `json:"params"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// toView projects a savedsearch.SavedSearch onto the client-facing view.
func toView(s savedsearch.SavedSearch) savedSearchView {
	return savedSearchView{
		UID:       s.UID,
		Name:      s.Name,
		Params:    s.Params,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// listEnvelope wraps a slice of saved-search views under the saved_searches key.
type listEnvelope struct {
	SavedSearches []savedSearchView `json:"saved_searches"`
}

// listResponse builds the list endpoint envelope from store records.
func listResponse(searches []savedsearch.SavedSearch) listEnvelope {
	views := make([]savedSearchView, len(searches))
	for i, s := range searches {
		views[i] = toView(s)
	}
	return listEnvelope{SavedSearches: views}
}

// createInput is the JSON body accepted by the create endpoint.
type createInput struct {
	Name   string          `json:"name"`
	Params json.RawMessage `json:"params"`
}

// updateInput is the JSON body accepted by the update endpoint. Both fields are
// optional: an omitted field leaves the corresponding column unchanged.
type updateInput struct {
	Name   *string          `json:"name"`
	Params *json.RawMessage `json:"params"`
}

// decodeJSON reads dst from the JSON request body, rejecting unknown fields and an
// oversized body. The returned error message is safe to surface to the client.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}

// decodeCreate decodes and validates a create body, requiring a non-empty name.
func decodeCreate(r *http.Request) (createInput, error) {
	var in createInput
	if err := decodeJSON(r, &in); err != nil {
		return createInput{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return createInput{}, errEmptyName
	}
	return in, nil
}

// decodeUpdate decodes a patch body and merges it onto existing, returning the
// name and params to persist. An omitted field keeps the existing value; a
// supplied but blank name is rejected.
func decodeUpdate(r *http.Request, existing savedsearch.SavedSearch) (string, json.RawMessage, error) {
	var in updateInput
	if err := decodeJSON(r, &in); err != nil {
		return "", nil, err
	}
	name := existing.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if name == "" {
			return "", nil, errEmptyName
		}
	}
	params := existing.Params
	if in.Params != nil {
		params = *in.Params
	}
	return name, params, nil
}
