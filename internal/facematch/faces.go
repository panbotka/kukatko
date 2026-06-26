package facematch

import (
	"context"
	"fmt"
	"log"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// PhotoFaces returns every stored face on the photo with its marker assignment and,
// for unnamed faces, ranked subject suggestions. Faces are matched to the photo's
// markers by IoU and the best match (when it clears the threshold) is cached on the
// face row. Markers that matched no detected face are appended so the detail UI can
// render manually drawn regions too. A missing photo yields photos.ErrPhotoNotFound
// (wrapped), which the HTTP layer maps to 404.
func (s *Service) PhotoFaces(ctx context.Context, photoUID string) (FacesResponse, error) {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return FacesResponse{}, fmt.Errorf("facematch: loading photo %s: %w", photoUID, err)
	}
	faces, err := s.faces.ListFaces(ctx, photoUID)
	if err != nil {
		return FacesResponse{}, fmt.Errorf("facematch: listing faces for %s: %w", photoUID, err)
	}
	markers, err := s.people.ListMarkersByPhoto(ctx, photoUID)
	if err != nil {
		return FacesResponse{}, fmt.Errorf("facematch: listing markers for %s: %w", photoUID, err)
	}

	names := s.assignedSubjectNames(ctx, markers)
	exclude := subjectUIDSet(names)
	matched := make(map[string]bool, len(markers))
	views := make([]FaceView, 0, len(faces)+len(markers))
	for i := range faces {
		views = append(views, s.buildFaceView(ctx, faces[i], markers, names, exclude, matched))
	}
	views = appendUnmatchedMarkers(views, markers, matched, names)

	return FacesResponse{
		PhotoUID:    photoUID,
		Width:       photo.FileWidth,
		Height:      photo.FileHeight,
		Orientation: photo.FileOrientation,
		Faces:       views,
	}, nil
}

// buildFaceView matches one stored face to the photo's markers, fills its
// assignment and recommended action, caches the match on the face row, and adds
// subject suggestions when the face is still unnamed.
func (s *Service) buildFaceView(
	ctx context.Context, face vectors.Face, markers []people.Marker,
	names map[string]string, exclude map[string]bool, matched map[string]bool,
) FaceView {
	view := FaceView{
		FaceIndex:   face.FaceIndex,
		BBox:        face.BBox,
		DetScore:    face.DetScore,
		Action:      ActionCreateMarker,
		Suggestions: []Suggestion{},
	}
	if best, iou := s.findBestMarker(face.BBox, markers); best != nil {
		matched[best.UID] = true
		view.MarkerUID = best.UID
		view.IoU = iou
		applyMarkerAssignment(&view, *best, names)
		s.cacheFaceMatch(ctx, face, *best, view.SubjectName)
	}
	if view.SubjectUID == "" {
		view.Suggestions = s.suggestForFace(ctx, face, exclude)
	}
	return view
}

// applyMarkerAssignment fills the marker's subject and the recommended action on
// view: already_done when the marker names a subject, otherwise assign_person.
func applyMarkerAssignment(view *FaceView, marker people.Marker, names map[string]string) {
	uid := derefSubject(marker.SubjectUID)
	if uid == "" {
		view.Action = ActionAssignPerson
		return
	}
	view.SubjectUID = uid
	view.SubjectName = names[uid]
	view.Action = ActionAlreadyDone
}

// cacheFaceMatch persists the matched marker (and its subject) on the face row when
// it differs from the cached value, so face↔marker matching is recorded for later
// reads and assignments. It is best-effort: a write failure is logged, not returned,
// because the response is already computed and the cache is regenerable.
func (s *Service) cacheFaceMatch(ctx context.Context, face vectors.Face, marker people.Marker, subjectName string) {
	subjectUID := derefSubject(marker.SubjectUID)
	if derefSubject(face.MarkerUID) == marker.UID && derefSubject(face.SubjectUID) == subjectUID {
		return // already cached identically
	}
	if err := s.faces.UpdateFaceMarker(
		ctx, face.PhotoUID, face.FaceIndex, marker.UID, subjectUID, subjectName,
	); err != nil {
		log.Printf("facematch: caching marker match for %s face %d: %v", face.PhotoUID, face.FaceIndex, err)
	}
}

// appendUnmatchedMarkers adds face-type, non-invalid markers that matched no stored
// face to views, with descending negative face indexes so the detail UI can render
// hand-drawn or stale regions. Already-matched markers are skipped.
func appendUnmatchedMarkers(
	views []FaceView, markers []people.Marker, matched map[string]bool, names map[string]string,
) []FaceView {
	index := -1
	for i := range markers {
		m := markers[i]
		if m.Type != people.MarkerFace || m.Invalid || matched[m.UID] {
			continue
		}
		view := FaceView{
			FaceIndex:   index,
			BBox:        markerBox(m),
			Action:      ActionAssignPerson,
			MarkerUID:   m.UID,
			Suggestions: []Suggestion{},
		}
		index--
		if uid := derefSubject(m.SubjectUID); uid != "" {
			view.SubjectUID = uid
			view.SubjectName = names[uid]
			view.Action = ActionAlreadyDone
		}
		views = append(views, view)
	}
	return views
}

// suggestForFace ranks likely subjects for an unnamed face from its nearest face
// neighbours, widening the search past the primary distance cutoff when the first
// pass returns fewer than the limit. An empty embedding or any search error yields
// no suggestions (the box being offline must not fail the faces view).
func (s *Service) suggestForFace(ctx context.Context, face vectors.Face, exclude map[string]bool) []Suggestion {
	if len(face.Vector) == 0 {
		return []Suggestion{}
	}
	primary, err := s.faces.FindSimilarFaceCandidates(ctx, face.Vector, suggestionSearchLimit, s.maxDistance)
	if err != nil {
		return []Suggestion{}
	}
	suggestions := aggregateSuggestions(primary, face.PhotoUID, exclude, s.minFaceSize, s.suggestionLimit)
	if len(suggestions) >= s.suggestionLimit {
		return suggestions
	}
	return s.fillSuggestions(ctx, face, exclude, suggestions)
}

// fillSuggestions widens the suggestion search to no distance cutoff and appends any
// new subjects not already suggested or excluded, up to the limit. It is the
// distance-threshold fallback so a face always gets some candidates when any named
// neighbour exists.
func (s *Service) fillSuggestions(
	ctx context.Context, face vectors.Face, exclude map[string]bool, have []Suggestion,
) []Suggestion {
	fallback, err := s.faces.FindSimilarFaceCandidates(ctx, face.Vector, suggestionSearchLimit, 0)
	if err != nil {
		return have
	}
	combined := make(map[string]bool, len(exclude)+len(have))
	for uid := range exclude {
		combined[uid] = true
	}
	for _, sug := range have {
		combined[sug.SubjectUID] = true
	}
	extra := aggregateSuggestions(fallback, face.PhotoUID, combined, s.minFaceSize, s.suggestionLimit-len(have))
	return append(have, extra...)
}

// assignedSubjectNames resolves the name of every subject assigned to a marker on
// the photo, returning a subjectUID→name map (best-effort: a subject that cannot be
// loaded maps to an empty name, still excluding it from suggestions).
func (s *Service) assignedSubjectNames(ctx context.Context, markers []people.Marker) map[string]string {
	names := make(map[string]string)
	for i := range markers {
		uid := derefSubject(markers[i].SubjectUID)
		if uid == "" {
			continue
		}
		if _, seen := names[uid]; seen {
			continue
		}
		subj, err := s.people.GetSubjectByUID(ctx, uid)
		if err != nil {
			names[uid] = ""
			continue
		}
		names[uid] = subj.Name
	}
	return names
}

// subjectUIDSet returns the set of subject uids keyed in names, used to exclude
// people already placed on the photo from its faces' suggestions.
func subjectUIDSet(names map[string]string) map[string]bool {
	set := make(map[string]bool, len(names))
	for uid := range names {
		set[uid] = true
	}
	return set
}

// derefSubject returns the string a non-nil pointer points to, or "".
func derefSubject(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
