package facematch

import (
	"context"
	"fmt"
	"log"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// PhotoFaces returns every stored face on the photo with its marker assignment and
// ranked subject suggestions — for an unnamed face the candidates to name it with,
// for an assigned one the alternatives to reassign it to. Faces and markers are
// paired exclusively by IoU (see matchMarkers) and each pairing is cached on the
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

	matched := matchMarkers(faces, markers, s.iouThreshold)
	names := s.assignedSubjectNames(ctx, markers)
	state := photoPairing{
		pairs:   matched,
		claimed: matched.claims(),
		surplus: surplusFaces(faces, matched),
		markers: markersByUID(markers),
		names:   names,
		exclude: subjectUIDSet(names),
	}
	views := make([]FaceView, 0, len(faces)+len(markers))
	for i := range faces {
		views = append(views, s.buildFaceView(ctx, faces[i], state))
	}
	views = appendUnmatchedMarkers(views, markers, state.claimed, names)

	return FacesResponse{
		PhotoUID:    photoUID,
		Width:       photo.FileWidth,
		Height:      photo.FileHeight,
		Orientation: photo.FileOrientation,
		Faces:       views,
	}, nil
}

// photoPairing is the per-photo matching state a face view is built from: the
// exclusive pairing, its inverse (marker → claiming face), the faces whose cached
// link is surplus, the photo's markers by uid, and the subject names/exclusions
// shared by every face on the photo.
type photoPairing struct {
	pairs   pairing
	claimed map[string]int
	surplus map[int]bool
	markers map[string]people.Marker
	names   map[string]string
	exclude map[string]bool
}

// buildFaceView turns one stored face into its view: the marker the exclusive
// pairing awarded it (if any), the resulting assignment and recommended action,
// the refreshed face-row cache, and the subject suggestions.
//
// A face the pairing left empty is a face without a marker — create_marker, no
// subject, suggestions like any other unnamed face — even when its row still
// caches a marker another face claims. That stale link is cleared here rather than
// merely ignored, because everything downstream reads faces.subject_uid and would
// otherwise keep seeing one person twice on the photo.
//
// Suggestions are computed for an assigned face too, so the UI can offer a
// reassignment. The face never suggests the person it already names: exclude holds
// every subject assigned on this photo, its own included. Only an unnamed face
// widens the search past the distance cutoff — for a correctly assigned face the
// near neighbours are its own (excluded) subject, so an empty result is the honest
// answer "no plausible alternative", while a misassigned one still surfaces the
// right person from the primary pass.
func (s *Service) buildFaceView(ctx context.Context, face vectors.Face, state photoPairing) FaceView {
	view := FaceView{
		FaceIndex:   face.FaceIndex,
		BBox:        face.BBox,
		DetScore:    face.DetScore,
		Action:      ActionCreateMarker,
		Suggestions: []Suggestion{},
	}
	switch match, ok := state.pairs[face.FaceIndex]; {
	case ok:
		marker := state.markers[match.MarkerUID]
		view.MarkerUID = marker.UID
		view.IoU = match.IoU
		applyMarkerAssignment(&view, marker, state.names)
		s.cacheFaceLink(ctx, face, marker.UID, view.SubjectUID, view.SubjectName)
	case state.surplus[face.FaceIndex]:
		s.cacheFaceLink(ctx, face, "", "", "")
	}
	view.Suggestions = s.suggestForFace(ctx, face, state.exclude, view.SubjectUID == "")
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

// cacheFaceLink persists the face's marker link (and that marker's subject) on the
// face row when it differs from the cached value, so face↔marker matching is
// recorded for later reads and assignments. All-empty arguments clear the link,
// which is how a surplus claim on a marker another face won is dropped. It is
// best-effort: a write failure is logged, not returned, because the response is
// already computed and the cache is regenerable.
func (s *Service) cacheFaceLink(
	ctx context.Context, face vectors.Face, markerUID, subjectUID, subjectName string,
) {
	if derefSubject(face.MarkerUID) == markerUID && derefSubject(face.SubjectUID) == subjectUID {
		return // already cached identically
	}
	if err := s.faces.UpdateFaceMarker(
		ctx, face.PhotoUID, face.FaceIndex, markerUID, subjectUID, subjectName,
	); err != nil {
		log.Printf("facematch: caching marker link for %s face %d: %v", face.PhotoUID, face.FaceIndex, err)
	}
}

// appendUnmatchedMarkers adds face-type, non-invalid markers that no face claimed
// in the exclusive pairing to views, with descending negative face indexes so the
// detail UI can render hand-drawn or stale regions. claimed maps a marker uid to
// the face index that won it; markers listed there are skipped.
func appendUnmatchedMarkers(
	views []FaceView, markers []people.Marker, claimed map[string]int, names map[string]string,
) []FaceView {
	index := -1
	for i := range markers {
		m := markers[i]
		if _, taken := claimed[m.UID]; m.Type != people.MarkerFace || m.Invalid || taken {
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

// suggestForFace ranks likely subjects for a face from its nearest face neighbours.
// When widen is set and the primary pass returns fewer than the limit, the search is
// repeated with no distance cutoff to fill the remaining slots — pass it only for an
// unnamed face, which needs candidates at any distance; an assigned one only wants
// alternatives that are genuinely close. An empty embedding or any search error yields
// no suggestions (the box being offline must not fail the faces view).
func (s *Service) suggestForFace(
	ctx context.Context, face vectors.Face, exclude map[string]bool, widen bool,
) []Suggestion {
	if len(face.Vector) == 0 {
		return []Suggestion{}
	}
	primary, err := s.faces.FindSimilarFaceCandidates(ctx, face.Vector, suggestionSearchLimit, s.maxDistance)
	if err != nil {
		return []Suggestion{}
	}
	suggestions := aggregateSuggestions(primary, face.PhotoUID, exclude, s.minFaceSize, s.suggestionLimit)
	if !widen || len(suggestions) >= s.suggestionLimit {
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
