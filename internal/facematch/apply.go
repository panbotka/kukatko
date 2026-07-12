package facematch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
)

// Apply performs one assignment-state transition for a face: create_marker (create a
// face marker and assign a subject), assign_person (assign a subject to an existing
// marker) or unassign_person (clear a marker's subject). It keeps the faces cache and
// the marker's reviewed flag consistent with the assignment, and auto-creates a
// subject by name (find-or-create by slug) when one is not given by uid. Each
// transition records one audit entry — stamped with meta (the acting user and
// request, empty for a system caller) — in the same transaction as the assignment
// change. An unknown action returns ErrInvalidAction; missing required fields return
// the matching ErrMissing* sentinel; a missing marker or subject is surfaced
// (wrapped) for the HTTP layer to map to 404.
func (s *Service) Apply(ctx context.Context, req AssignRequest, meta audit.Meta) (AssignResult, error) {
	switch req.Action {
	case ActionCreateMarker:
		return s.applyCreateMarker(ctx, req, meta)
	case ActionAssignPerson:
		return s.applyAssignPerson(ctx, req, meta)
	case ActionUnassignPerson:
		return s.applyUnassign(ctx, req, meta)
	default:
		return AssignResult{}, fmt.Errorf("%w: %q", ErrInvalidAction, req.Action)
	}
}

// applyCreateMarker creates a face marker for the request's box, assigns it the
// resolved subject, and links the named face to it. The new marker is the audit
// entry's target (its UID is stamped by the store).
func (s *Service) applyCreateMarker(ctx context.Context, req AssignRequest, meta audit.Meta) (AssignResult, error) {
	if req.BBox == nil {
		return AssignResult{}, ErrMissingBBox
	}
	subject, err := s.resolveSubject(ctx, req)
	if err != nil {
		return AssignResult{}, err
	}
	box := clampBox(*req.BBox)
	entry := meta.Entry(audit.ActionFaceAssign, "markers", "", assignDetails(req, subject))
	marker, err := s.people.CreateMarkerAudited(ctx, people.Marker{
		PhotoUID:   req.PhotoUID,
		SubjectUID: &subject.UID,
		Type:       people.MarkerFace,
		X:          box[0], Y: box[1], W: box[2], H: box[3],
		Reviewed: true,
	}, entry)
	if err != nil {
		return AssignResult{}, fmt.Errorf("facematch: creating marker: %w", err)
	}
	s.linkFace(ctx, req, marker.UID, subject.UID, subject.Name)
	return AssignResult{Action: ActionCreateMarker, Marker: marker, Subject: &subject}, nil
}

// applyAssignPerson assigns the resolved subject to an existing marker, marks the
// marker reviewed, and links the named face to it.
func (s *Service) applyAssignPerson(ctx context.Context, req AssignRequest, meta audit.Meta) (AssignResult, error) {
	if req.MarkerUID == "" {
		return AssignResult{}, ErrMissingMarker
	}
	subject, err := s.resolveSubject(ctx, req)
	if err != nil {
		return AssignResult{}, err
	}
	entry := meta.Entry(audit.ActionFaceAssign, "markers", req.MarkerUID, assignDetails(req, subject))
	if _, err := s.people.AssignSubjectAudited(ctx, req.MarkerUID, subject.UID, entry); err != nil {
		return AssignResult{}, fmt.Errorf("facematch: assigning subject: %w", err)
	}
	marker, err := s.people.SetMarkerReviewed(ctx, req.MarkerUID, true)
	if err != nil {
		return AssignResult{}, fmt.Errorf("facematch: marking reviewed: %w", err)
	}
	s.linkFace(ctx, req, req.MarkerUID, subject.UID, subject.Name)
	return AssignResult{Action: ActionAssignPerson, Marker: marker, Subject: &subject}, nil
}

// applyUnassign clears a marker's subject and its reviewed flag. The face keeps its
// marker_uid link (the region is still valid), only the subject cache is cleared,
// which UnassignSubjectAudited does for every face tied to the marker.
func (s *Service) applyUnassign(ctx context.Context, req AssignRequest, meta audit.Meta) (AssignResult, error) {
	if req.MarkerUID == "" {
		return AssignResult{}, ErrMissingMarker
	}
	entry := meta.Entry(audit.ActionFaceUnassign, "markers", req.MarkerUID, unassignDetails(req))
	if _, err := s.people.UnassignSubjectAudited(ctx, req.MarkerUID, entry); err != nil {
		return AssignResult{}, fmt.Errorf("facematch: unassigning subject: %w", err)
	}
	marker, err := s.people.SetMarkerReviewed(ctx, req.MarkerUID, false)
	if err != nil {
		return AssignResult{}, fmt.Errorf("facematch: marking unreviewed: %w", err)
	}
	return AssignResult{Action: ActionUnassignPerson, Marker: marker}, nil
}

// assignDetails builds the audit details for a face assignment (create_marker or
// assign_person): the effective action, the photo, the resolved subject and, when
// present, the linked face index. The affected marker is the entry's target.
func assignDetails(req AssignRequest, subject people.Subject) map[string]any {
	details := map[string]any{
		"action":       req.Action,
		"photo_uid":    req.PhotoUID,
		"subject_uid":  subject.UID,
		"subject_name": subject.Name,
	}
	if req.MarkerUID != "" {
		details["marker_uid"] = req.MarkerUID
	}
	if req.FaceIndex != nil {
		details["face_index"] = *req.FaceIndex
	}
	return details
}

// unassignDetails builds the audit details for clearing a face's subject: the
// photo, the affected marker and, when present, the linked face index.
func unassignDetails(req AssignRequest) map[string]any {
	details := map[string]any{
		"action":     req.Action,
		"photo_uid":  req.PhotoUID,
		"marker_uid": req.MarkerUID,
	}
	if req.FaceIndex != nil {
		details["face_index"] = *req.FaceIndex
	}
	return details
}

// resolveSubject returns the subject named by the request: the one identified by
// SubjectUID, or — failing that — found-or-created from a non-empty SubjectName.
// A request naming neither returns ErrMissingSubject.
func (s *Service) resolveSubject(ctx context.Context, req AssignRequest) (people.Subject, error) {
	if req.SubjectUID != "" {
		subj, err := s.people.GetSubjectByUID(ctx, req.SubjectUID)
		if err != nil {
			return people.Subject{}, fmt.Errorf("facematch: loading subject %s: %w", req.SubjectUID, err)
		}
		return subj, nil
	}
	name := strings.TrimSpace(req.SubjectName)
	if name == "" {
		return people.Subject{}, ErrMissingSubject
	}
	return s.findOrCreateSubject(ctx, name)
}

// findOrCreateSubject returns the subject whose slug matches name's slug, creating a
// new person subject when none exists. It is the auto-create-by-name path so an
// assignment can name a fresh person without a separate create step.
func (s *Service) findOrCreateSubject(ctx context.Context, name string) (people.Subject, error) {
	slug := people.Slugify(name)
	subj, err := s.people.GetSubjectBySlug(ctx, slug)
	if err == nil {
		return subj, nil
	}
	if !errors.Is(err, people.ErrSubjectNotFound) {
		return people.Subject{}, fmt.Errorf("facematch: looking up subject %q: %w", name, err)
	}
	created, err := s.people.CreateSubject(ctx, people.Subject{Name: name})
	if err != nil {
		return people.Subject{}, fmt.Errorf("facematch: creating subject %q: %w", name, err)
	}
	return created, nil
}

// linkFace caches the marker/subject assignment on the request's face (when one is
// named), so the specific face's cache is in step even if IoU matching never ran for
// it. It is best-effort: a write failure is logged, not returned.
func (s *Service) linkFace(ctx context.Context, req AssignRequest, markerUID, subjectUID, subjectName string) {
	if req.FaceIndex == nil {
		return
	}
	if err := s.faces.UpdateFaceMarker(
		ctx, req.PhotoUID, *req.FaceIndex, markerUID, subjectUID, subjectName,
	); err != nil {
		log.Printf("facematch: linking face %d of %s to marker %s: %v",
			*req.FaceIndex, req.PhotoUID, markerUID, err)
	}
}

// clampBox clamps every coordinate of a normalised box into [0, 1] so an edge face
// detection just outside the unit square still satisfies the marker bounds check.
func clampBox(b [4]float64) [4]float64 {
	for i := range b {
		b[i] = clampUnit(b[i])
	}
	return b
}

// clampUnit clamps v into the closed unit interval [0, 1].
func clampUnit(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
