package photoapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// maxUpdateBody caps the PATCH request body so a malformed or hostile client
// cannot stream an unbounded JSON document into memory.
const maxUpdateBody = 1 << 20 // 1 MiB

// updateBody is the editable metadata accepted by PATCH /photos/{uid}. Every
// field is a pointer so an omitted key leaves the value unchanged while an
// explicit null clears a nullable one (taken_at, lat, lng). Presence is taken
// from the decoded key set, not from the pointer being non-nil, so "set to null"
// and "absent" are distinguished.
type updateBody struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	Notes       *string    `json:"notes"`
	AiNote      *string    `json:"ai_note"`
	Subject     *string    `json:"subject"`
	Keywords    *string    `json:"keywords"`
	Artist      *string    `json:"artist"`
	Copyright   *string    `json:"copyright"`
	License     *string    `json:"license"`
	Scan        *bool      `json:"scan"`
	TakenAt     *time.Time `json:"taken_at"`
	Lat         *float64   `json:"lat"`
	Lng         *float64   `json:"lng"`
}

// creditLimits caps each IPTC/XMP credit field a PATCH may set. The values are
// free text with no syntax to validate, so length is the only guard — generous
// enough for a real headline or a licence sentence, tight enough that the column
// cannot be used as a blob store. Counted in runes, so a Czech caption is not cut
// short by its accents' extra bytes.
var creditLimits = map[string]int{
	"subject":   1000,
	"copyright": 1000,
	"license":   1000,
	"keywords":  2000,
	"artist":    255,
}

// handleUpdate applies a partial metadata update to the photo named in the path
// and returns the refreshed photo as the same full detail body GET /photos/{uid}
// answers with — the client swaps the detail it holds for this response, so a bare
// photo would strip its files, albums, labels and is_favorite flag. Omitted fields
// are left unchanged; an explicit null clears a nullable field. A malformed body or
// out-of-range coordinate is answered with 400 and a missing photo with 404.
func (a *API) handleUpdate(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")

	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	present, body, err := decodeUpdate(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	current, err := a.store.GetByUID(r.Context(), uid)
	if err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}

	update, err := mergeUpdate(current, present, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	entry := audit.FromRequest(r, user.UID).Entry(
		audit.ActionPhotoUpdate, "photos", uid, map[string]any{"fields": presentFields(present)},
	)
	updated, err := a.store.UpdateMetadataAudited(r.Context(), uid, update, entry)
	if err != nil {
		writePhotoError(w, err, "updating photo failed")
		return
	}
	a.writeDetail(w, r, user.UID, updated)
}

// presentFields returns the sorted names of the metadata fields the caller sent,
// recorded in the audit entry so the trail shows which fields a PATCH touched.
func presentFields(present map[string]struct{}) []string {
	fields := make([]string, 0, len(present))
	for name := range present {
		fields = append(fields, name)
	}
	sort.Strings(fields)
	return fields
}

// decodeUpdate reads the JSON request body once, returning the set of keys that
// were present and the decoded values. It rejects an oversized body, unknown
// fields, malformed JSON or trailing data.
func decodeUpdate(r *http.Request) (map[string]struct{}, updateBody, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxUpdateBody+1))
	if err != nil {
		return nil, updateBody{}, errors.New("reading request body failed")
	}
	if len(raw) > maxUpdateBody {
		return nil, updateBody{}, errors.New("request body too large")
	}

	// First pass: which keys did the caller actually send?
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, updateBody{}, errors.New("malformed JSON body")
	}
	present := make(map[string]struct{}, len(keys))
	for k := range keys {
		present[k] = struct{}{}
	}

	// Second pass: typed values, rejecting unknown fields.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var body updateBody
	if err := dec.Decode(&body); err != nil {
		return nil, updateBody{}, errors.New("invalid field in JSON body")
	}
	return present, body, nil
}

// mergeUpdate overlays the present fields of body onto the photo's current
// metadata, producing the full MetadataUpdate the store overwrites with. It
// validates coordinate ranges and keeps taken_at_source in step with taken_at.
func mergeUpdate(current photos.Photo, present map[string]struct{}, body updateBody) (photos.MetadataUpdate, error) {
	update := photos.MetadataUpdate{
		Title:         current.Title,
		Description:   current.Description,
		Notes:         current.Notes,
		AiNote:        current.AiNote,
		Subject:       current.Subject,
		Keywords:      current.Keywords,
		Artist:        current.Artist,
		Copyright:     current.Copyright,
		License:       current.License,
		Scan:          current.Scan,
		TakenAt:       current.TakenAt,
		TakenAtSource: current.TakenAtSource,
		Lat:           current.Lat,
		Lng:           current.Lng,
		Altitude:      current.Altitude,
		// The private column is no editable field any more, but the importers still
		// write it, so it is carried over unchanged: UpdateMetadata overwrites the
		// whole row and would otherwise clear an imported flag on every edit.
		Private: current.Private,
	}

	applyScalars(&update, present, body)
	if err := applyCredits(&update, present, body); err != nil {
		return photos.MetadataUpdate{}, err
	}
	if _, ok := present["taken_at"]; ok {
		applyTakenAt(&update, body.TakenAt)
	}
	if err := applyCoordinate(&update, present, body); err != nil {
		return photos.MetadataUpdate{}, err
	}
	return update, nil
}

// applyScalars overlays the present non-nullable scalar fields (title,
// description, notes, ai_note) onto update. An explicit JSON null for one of
// these is ignored, since the columns are not nullable.
func applyScalars(update *photos.MetadataUpdate, present map[string]struct{}, body updateBody) {
	applyPresentString(present, "title", body.Title, &update.Title)
	applyPresentString(present, "description", body.Description, &update.Description)
	applyPresentString(present, "notes", body.Notes, &update.Notes)
	applyPresentString(present, "ai_note", body.AiNote, &update.AiNote)
}

// applyCredits overlays the present IPTC/XMP credit fields onto update. Each is
// trimmed of surrounding whitespace and rejected when it exceeds its cap in
// creditLimits, which the caller turns into a 400. The scan flag is a plain
// boolean with nothing to validate. As with the other non-nullable columns, an
// explicit JSON null is ignored.
func applyCredits(update *photos.MetadataUpdate, present map[string]struct{}, body updateBody) error {
	credits := []struct {
		key   string
		value *string
		dst   *string
	}{
		{"subject", body.Subject, &update.Subject},
		{"keywords", body.Keywords, &update.Keywords},
		{"artist", body.Artist, &update.Artist},
		{"copyright", body.Copyright, &update.Copyright},
		{"license", body.License, &update.License},
	}
	for _, c := range credits {
		if _, ok := present[c.key]; !ok || c.value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*c.value)
		if limit := creditLimits[c.key]; utf8.RuneCountInString(trimmed) > limit {
			return fmt.Errorf("%s must be at most %d characters", c.key, limit)
		}
		*c.dst = trimmed
	}
	if _, ok := present["scan"]; ok && body.Scan != nil {
		update.Scan = *body.Scan
	}
	return nil
}

// applyPresentString copies value onto dst when key is present and value is
// non-null. An omitted key or an explicit JSON null leaves dst unchanged, since
// the backing column is NOT NULL.
func applyPresentString(present map[string]struct{}, key string, value *string, dst *string) {
	if _, ok := present[key]; ok && value != nil {
		*dst = *value
	}
}

// applyTakenAt sets or clears the capture time and tracks its source: a provided
// time is marked "manual", clearing it resets the source to "unknown".
func applyTakenAt(update *photos.MetadataUpdate, takenAt *time.Time) {
	update.TakenAt = takenAt
	if takenAt != nil {
		update.TakenAtSource = "manual"
	} else {
		update.TakenAtSource = "unknown"
	}
}

// applyCoordinate sets or clears latitude and longitude from the present body
// fields, validating that any supplied value is within the geographic range.
func applyCoordinate(update *photos.MetadataUpdate, present map[string]struct{}, body updateBody) error {
	if _, ok := present["lat"]; ok {
		if body.Lat != nil && (*body.Lat < -90 || *body.Lat > 90) {
			return errors.New("lat must be between -90 and 90")
		}
		update.Lat = body.Lat
	}
	if _, ok := present["lng"]; ok {
		if body.Lng != nil && (*body.Lng < -180 || *body.Lng > 180) {
			return errors.New("lng must be between -180 and 180")
		}
		update.Lng = body.Lng
	}
	return nil
}
