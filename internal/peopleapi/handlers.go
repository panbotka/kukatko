package peopleapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// subjectsResponse is the JSON body returned by the subject-list endpoint.
type subjectsResponse struct {
	Subjects []people.SubjectCount `json:"subjects"`
}

// subjectInput is the JSON body accepted by the create and update endpoints. It
// carries the user-editable subject fields; UID, slug and timestamps are managed
// by the store.
type subjectInput struct {
	Name          string             `json:"name"`
	Type          people.SubjectType `json:"type"`
	Favorite      bool               `json:"favorite"`
	Private       bool               `json:"private"`
	Notes         string             `json:"notes"`
	CoverPhotoUID *string            `json:"cover_photo_uid"`
}

// photosResponse is the paginated JSON body returned by the subject-photos
// endpoint. NextOffset is the offset to request for the following page, or null
// on the last page, so an infinite-scroll client can page until it is absent.
type photosResponse struct {
	Photos     []photos.Photo `json:"photos"`
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Offset     int            `json:"offset"`
	NextOffset *int           `json:"next_offset"`
}

// handleList returns every subject together with its non-invalid marker count,
// ordered by name. It answers 500 if the store fails.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	subjects, err := a.subjects.ListSubjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing subjects failed")
		return
	}
	writeJSON(w, http.StatusOK, subjectsResponse{Subjects: subjects})
}

// handleCreate creates a subject from the request body and returns it with its
// generated UID and unique slug. A malformed body or invalid type answers 400.
func (a *API) handleCreate(w http.ResponseWriter, r *http.Request) {
	in, err := decodeSubjectInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	subj, err := a.subjects.CreateSubject(r.Context(), in.toSubject())
	if err != nil {
		status, msg := subjectStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusCreated, subj)
}

// handleGet returns the subject identified by the path UID, or 404 if missing.
func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	subj, err := a.subjects.GetSubjectByUID(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		status, msg := subjectStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, subj)
}

// handleUpdate rewrites the editable fields of the subject identified by the path
// UID and returns the refreshed subject. A malformed body or invalid type answers
// 400; a missing subject answers 404.
func (a *API) handleUpdate(w http.ResponseWriter, r *http.Request) {
	in, err := decodeSubjectInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	subj, err := a.subjects.UpdateSubject(r.Context(), chi.URLParam(r, "uid"), in.toUpdate())
	if err != nil {
		status, msg := subjectStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, subj)
}

// handleDelete removes the subject identified by the path UID, answering 204 on
// success or 404 if no such subject exists.
func (a *API) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.subjects.DeleteSubject(r.Context(), chi.URLParam(r, "uid")); err != nil {
		status, msg := subjectStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePhotos returns the requested page of the subject's photos, newest first,
// plus the total and next-page offset for infinite scroll. Invalid pagination
// answers 400.
func (a *API) handlePhotos(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePagination(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	uids, err := a.subjects.ListPhotoUIDsBySubject(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing subject photos failed")
		return
	}
	page := pageUIDs(uids, limit, offset)
	list, err := a.resolvePhotos(r, page)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading subject photos failed")
		return
	}
	writeJSON(w, http.StatusOK, buildPhotosResponse(list, len(uids), limit, offset))
}

// resolvePhotos loads the photos for the given page of UIDs, restores the
// newest-first UID order (ListByUIDs returns rows in unspecified order) and
// stamps on the media URLs the client fetches.
func (a *API) resolvePhotos(r *http.Request, page []string) ([]photos.Photo, error) {
	list, err := a.photos.ListByUIDs(r.Context(), page)
	if err != nil {
		return nil, fmt.Errorf("peopleapi: loading subject photos: %w", err)
	}
	ordered := orderByUIDs(page, list)
	a.media.Decorate(ordered)
	return ordered, nil
}

// toSubject converts the request input into a people.Subject for creation.
func (in subjectInput) toSubject() people.Subject {
	return people.Subject{
		Name:          in.Name,
		Type:          in.Type,
		Favorite:      in.Favorite,
		Private:       in.Private,
		Notes:         in.Notes,
		CoverPhotoUID: in.CoverPhotoUID,
	}
}

// toUpdate converts the request input into a people.SubjectUpdate for editing.
func (in subjectInput) toUpdate() people.SubjectUpdate {
	return people.SubjectUpdate{
		Name:          in.Name,
		Type:          in.Type,
		Favorite:      in.Favorite,
		Private:       in.Private,
		Notes:         in.Notes,
		CoverPhotoUID: in.CoverPhotoUID,
	}
}

// buildPhotosResponse assembles the paginated response, computing the next-page
// offset (nil on the last page).
func buildPhotosResponse(list []photos.Photo, total, limit, offset int) photosResponse {
	resp := photosResponse{Photos: list, Total: total, Limit: limit, Offset: offset}
	if next := offset + len(list); next < total && len(list) > 0 {
		resp.NextOffset = &next
	}
	return resp
}

// parsePagination reads the limit and offset query parameters, applying the
// default and maximum page size. A non-numeric or negative value returns an error
// so the caller can answer 400.
func parsePagination(r *http.Request) (limit, offset int, err error) {
	q := r.URL.Query()
	limit, err = parseBoundedInt(q.Get("limit"), defaultPageLimit, 1, maxPageLimit)
	if err != nil {
		return 0, 0, err
	}
	offset, err = parseBoundedInt(q.Get("offset"), 0, 0, 0)
	if err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}

// parseBoundedInt parses raw as an int, returning def when raw is empty. The
// result is clamped to [lo, hi] when hi > 0, otherwise only to a lower bound of
// lo. A non-numeric value returns an error.
func parseBoundedInt(raw string, def, lo, hi int) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &paramError{param: "pagination", value: raw}
	}
	if n < lo {
		n = lo
	}
	if hi > 0 && n > hi {
		n = hi
	}
	return n, nil
}

// pageUIDs returns the slice of uids for the page starting at offset with the
// given limit, tolerating an offset past the end (yielding an empty page).
func pageUIDs(uids []string, limit, offset int) []string {
	if offset >= len(uids) {
		return []string{}
	}
	end := min(offset+limit, len(uids))
	return uids[offset:end]
}

// orderByUIDs returns the photos reordered to match the order of uids, dropping
// any uid that resolved to no photo. It restores the deterministic order that
// ListByUIDs does not guarantee.
func orderByUIDs(uids []string, list []photos.Photo) []photos.Photo {
	byUID := make(map[string]photos.Photo, len(list))
	for _, p := range list {
		byUID[p.UID] = p
	}
	out := make([]photos.Photo, 0, len(uids))
	for _, uid := range uids {
		if p, ok := byUID[uid]; ok {
			out = append(out, p)
		}
	}
	return out
}

// paramError is returned when a query parameter cannot be parsed; its Error
// message is safe to return to the client.
type paramError struct {
	param string
	value string
}

// Error implements error, describing the offending parameter and value.
func (e *paramError) Error() string {
	return "invalid " + e.param + " value: " + e.value
}
