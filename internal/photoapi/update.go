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
	Title            *string    `json:"title"`
	Description      *string    `json:"description"`
	Notes            *string    `json:"notes"`
	AiNote           *string    `json:"ai_note"`
	Subject          *string    `json:"subject"`
	Keywords         *string    `json:"keywords"`
	Artist           *string    `json:"artist"`
	Copyright        *string    `json:"copyright"`
	License          *string    `json:"license"`
	Scan             *bool      `json:"scan"`
	TakenAt          *time.Time `json:"taken_at"`
	TakenAtEstimated *bool      `json:"taken_at_estimated"`
	TakenAtNote      *string    `json:"taken_at_note"`
	Lat              *float64   `json:"lat"`
	Lng              *float64   `json:"lng"`
	// LocationSource accepts exactly one value, "manual", and exists for one
	// action: accepting an estimated location, promoting it to a decision the user
	// owns. Sending the coordinates back instead would work but would round them to
	// whatever precision the client rendered, so accepting is its own key.
	//
	// The other values are not settable. "estimate" is the estimator's to write, and
	// letting a client claim "exif" would let it forge the provenance of a
	// coordinate it typed in — which is precisely what this column exists to
	// prevent. Clearing is done by sending lat/lng null, which stamps "manual" on
	// its own.
	LocationSource *string `json:"location_source"`
}

// takenAtNoteLimit caps the free-text dating note ("kolem roku 1950", "podle
// babičky někdy před svatbou"). It is a remark about one date, not a caption, so a
// few sentences are plenty; counted in runes, so a Czech note is not cut short by
// its accents' extra bytes.
const takenAtNoteLimit = 500

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

	details := map[string]any{"fields": presentFields(present)}
	metadataChanges(current, update).StampInto(details)
	entry := audit.FromRequest(r, user.UID).Entry(audit.ActionPhotoUpdate, "photos", uid, details)
	updated, err := a.store.UpdateMetadataAudited(r.Context(), uid, update, entry)
	if err != nil {
		writePhotoError(w, err, "updating photo failed")
		return
	}
	a.enqueueSidecar(r.Context(), uid)
	a.writeDetail(w, r, user.UID, updated)
}

// metadataChanges builds the old→new diff for a photo metadata edit, comparing
// the row before the edit (before) against the merged update the store will
// apply (after) and recording only the user-editable fields whose value changed.
// Because after is built from before overlaid with the request's present fields,
// a field the caller did not touch is byte-for-byte the old value and is skipped,
// so no `present` set is needed here. The result is stamped under the audit
// "changes" key so the trail shows each field's previous value beside its new one
// (see internal/audit ChangeSet).
func metadataChanges(before photos.Photo, after photos.MetadataUpdate) *audit.ChangeSet {
	changes := audit.NewChangeSet()
	changes.Add("title", before.Title, after.Title)
	changes.Add("description", before.Description, after.Description)
	changes.Add("notes", before.Notes, after.Notes)
	changes.Add("ai_note", before.AiNote, after.AiNote)
	changes.Add("subject", before.Subject, after.Subject)
	changes.Add("keywords", before.Keywords, after.Keywords)
	changes.Add("artist", before.Artist, after.Artist)
	changes.Add("copyright", before.Copyright, after.Copyright)
	changes.Add("license", before.License, after.License)
	changes.Add("scan", before.Scan, after.Scan)
	changes.Add("taken_at", before.TakenAt, after.TakenAt)
	changes.Add("taken_at_estimated", before.TakenAtEstimated, after.TakenAtEstimated)
	changes.Add("taken_at_note", before.TakenAtNote, after.TakenAtNote)
	changes.Add("lat", before.Lat, after.Lat)
	changes.Add("lng", before.Lng, after.Lng)
	changes.Add("location_source", before.LocationSource, after.LocationSource)
	return changes
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
// validates coordinate ranges and the dating note's length, and keeps
// taken_at_source in step with taken_at.
func mergeUpdate(current photos.Photo, present map[string]struct{}, body updateBody) (photos.MetadataUpdate, error) {
	update := photos.MetadataUpdate{
		Title:            current.Title,
		Description:      current.Description,
		Notes:            current.Notes,
		AiNote:           current.AiNote,
		Subject:          current.Subject,
		Keywords:         current.Keywords,
		Artist:           current.Artist,
		Copyright:        current.Copyright,
		License:          current.License,
		Scan:             current.Scan,
		TakenAt:          current.TakenAt,
		TakenAtSource:    current.TakenAtSource,
		TakenAtEstimated: current.TakenAtEstimated,
		TakenAtNote:      current.TakenAtNote,
		Lat:              current.Lat,
		Lng:              current.Lng,
		Altitude:         current.Altitude,
		LocationSource:   current.LocationSource,
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
	if err := applyTakenAtEstimate(&update, present, body); err != nil {
		return photos.MetadataUpdate{}, err
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

// locationSourceManual is the only location_source a client may set, and the one
// stamped on any user edit of the coordinates.
const locationSourceManual = photos.LocationSourceManual

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

// applyTakenAtEstimate overlays the approximate-date pair: the "this date is a
// guess" flag and the free-text dating note. The note is trimmed and rejected
// beyond takenAtNoteLimit runes, which the caller turns into a 400. As with the
// other non-nullable columns, an explicit JSON null is ignored.
//
// It also enforces the pair's one invariant: a note only means something while the
// date is flagged as an estimate, so a photo whose flag is false — because the
// caller has just cleared it, or because it never was set — is stored without a
// note. Unchecking the flag therefore drops the note as well, and no stale dating
// remark can survive on a photo whose date is presented as a fact. The length is
// still validated first, so an over-long note is reported rather than silently
// discarded.
//
// The flag is deliberately independent of taken_at: a photo with no capture time at
// all may carry an estimate whose note ("někdy ve 40. letech") is the whole
// meaning, and it goes on behaving everywhere exactly like any other undated photo.
func applyTakenAtEstimate(update *photos.MetadataUpdate, present map[string]struct{}, body updateBody) error {
	if _, ok := present["taken_at_estimated"]; ok && body.TakenAtEstimated != nil {
		update.TakenAtEstimated = *body.TakenAtEstimated
	}
	if _, ok := present["taken_at_note"]; ok && body.TakenAtNote != nil {
		note := strings.TrimSpace(*body.TakenAtNote)
		if utf8.RuneCountInString(note) > takenAtNoteLimit {
			return fmt.Errorf("taken_at_note must be at most %d characters", takenAtNoteLimit)
		}
		update.TakenAtNote = note
	}
	if !update.TakenAtEstimated {
		update.TakenAtNote = ""
	}
	return nil
}

// applyCoordinate sets or clears latitude and longitude from the present body
// fields, validating that any supplied value is within the geographic range, and
// keeps location_source in step with the coordinates it describes.
//
// Touching either coordinate stamps the location "manual", whether it moved or
// was cleared. Clearing deliberately does NOT reset the source to "" the way
// applyTakenAt resets an emptied date to "unknown": an empty location with no
// source is what the estimator considers fair game, so resetting it would have
// the backfill hand back the very estimate the user just threw away, every night,
// forever. "manual" with no coordinates is the tombstone that says "the user
// decided this photo has no location" — a decision, not a gap.
func applyCoordinate(update *photos.MetadataUpdate, present map[string]struct{}, body updateBody) error {
	touched, err := applyLatLng(update, present, body)
	if err != nil {
		return err
	}
	if touched {
		update.LocationSource = locationSourceManual
	}
	return applyLocationSource(update, present, body)
}

// applyLatLng overlays the present coordinates onto update after range-checking
// them, reporting whether either was touched (moved or cleared) so the caller can
// keep the provenance in step.
func applyLatLng(update *photos.MetadataUpdate, present map[string]struct{}, body updateBody) (bool, error) {
	_, latPresent := present["lat"]
	_, lngPresent := present["lng"]
	if latPresent {
		if body.Lat != nil && (*body.Lat < -90 || *body.Lat > 90) {
			return false, errors.New("lat must be between -90 and 90")
		}
		update.Lat = body.Lat
	}
	if lngPresent {
		if body.Lng != nil && (*body.Lng < -180 || *body.Lng > 180) {
			return false, errors.New("lng must be between -180 and 180")
		}
		update.Lng = body.Lng
	}
	return latPresent || lngPresent, nil
}

// applyLocationSource handles accepting an estimated location: promoting it to
// "manual", the user's own. It rejects any other value, and rejects accepting a
// location that is not there — "the user vouches for this coordinate" is
// meaningless without a coordinate to vouch for.
//
// It runs after the coordinates so that a request touching both still ends up
// "manual" either way, and so the presence of a location is judged on the merged
// result rather than on what was already in the row.
func applyLocationSource(update *photos.MetadataUpdate, present map[string]struct{}, body updateBody) error {
	if _, ok := present["location_source"]; !ok {
		return nil
	}
	if body.LocationSource == nil || *body.LocationSource != locationSourceManual {
		return fmt.Errorf("location_source may only be set to %q", locationSourceManual)
	}
	if update.Lat == nil || update.Lng == nil {
		return errors.New("location_source cannot be set on a photo with no location")
	}
	update.LocationSource = locationSourceManual
	return nil
}
