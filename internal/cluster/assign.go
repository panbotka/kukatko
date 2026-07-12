package cluster

import (
	"context"
	"fmt"
	"strings"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// AssignCluster assigns every face in a cluster to one subject and returns the
// subject plus the markers created for each member. The subject is named by
// SubjectUID, or — failing that — found-or-created from SubjectName (by the
// underlying assignment state machine). Each member face gets a face marker
// assigned to the subject; once all faces are assigned (and so no longer
// clusterable), the now-consumed cluster is deleted, detaching its faces. It
// returns ErrMissingSubject when neither subject field is set, ErrClusterNotFound
// for an unknown cluster and ErrEmptyCluster for a cluster with no faces.
func (s *Service) AssignCluster(ctx context.Context, req AssignRequest) (AssignResult, error) {
	if req.SubjectUID == "" && strings.TrimSpace(req.SubjectName) == "" {
		return AssignResult{}, ErrMissingSubject
	}
	if _, err := s.store.GetCluster(ctx, req.ClusterUID); err != nil {
		return AssignResult{}, err
	}
	faces, err := s.store.ListClusterFaces(ctx, req.ClusterUID)
	if err != nil {
		return AssignResult{}, err
	}
	if len(faces) == 0 {
		return AssignResult{}, ErrEmptyCluster
	}
	result, err := s.assignFaces(ctx, req, faces)
	if err != nil {
		return AssignResult{}, err
	}
	if err := s.store.DeleteCluster(ctx, req.ClusterUID); err != nil {
		return AssignResult{}, err
	}
	return result, nil
}

// assignFaces creates a face marker assigned to the request's subject for every
// face, reusing the facematch assignment state machine. The subject is resolved
// once: the first create — which may find-or-create the subject by name — pins
// the subject uid used for the remaining faces, so a name resolves to a single
// subject rather than one per face.
func (s *Service) assignFaces(ctx context.Context, req AssignRequest, faces []Face) (AssignResult, error) {
	subjectUID := req.SubjectUID
	subjectName := req.SubjectName
	markers := make([]people.Marker, 0, len(faces))
	var subject people.Subject
	for i := range faces {
		faceIndex := faces[i].FaceIndex
		bbox := faces[i].BBox
		// Cluster auto-assignment is a batch state transition rather than a single
		// user action; it carries an empty audit.Meta, so each resulting face.assign
		// row is attributed to no actor (stored NULL) but still records the change.
		res, err := s.assigner.Apply(ctx, facematch.AssignRequest{
			PhotoUID:    faces[i].PhotoUID,
			Action:      facematch.ActionCreateMarker,
			FaceIndex:   &faceIndex,
			BBox:        &bbox,
			SubjectUID:  subjectUID,
			SubjectName: subjectName,
		}, audit.Meta{})
		if err != nil {
			return AssignResult{}, fmt.Errorf(
				"cluster: assigning face %d of %s: %w", faceIndex, faces[i].PhotoUID, err)
		}
		markers = append(markers, res.Marker)
		if res.Subject != nil {
			subject = *res.Subject
			subjectUID = subject.UID
			subjectName = ""
		}
	}
	return AssignResult{ClusterUID: req.ClusterUID, Subject: subject, Markers: markers}, nil
}

// RemoveFace detaches one face from a cluster before assignment, so a stray face
// does not pollute the name. It returns the refreshed cluster view (with the
// centroid recomputed from the remaining faces) and deleted=false, or a zero view
// and deleted=true when removing the face emptied — and so deleted — the cluster.
// It returns ErrClusterNotFound for an unknown cluster and ErrFaceNotInCluster
// when the face is not a member.
func (s *Service) RemoveFace(ctx context.Context, clusterUID string, ref Ref) (View, bool, error) {
	if _, err := s.store.GetCluster(ctx, clusterUID); err != nil {
		return View{}, false, err
	}
	removed, err := s.store.RemoveFaceFromCluster(ctx, clusterUID, ref)
	if err != nil {
		return View{}, false, err
	}
	if !removed {
		return View{}, false, ErrFaceNotInCluster
	}
	faces, err := s.store.ListClusterFaces(ctx, clusterUID)
	if err != nil {
		return View{}, false, err
	}
	if len(faces) == 0 {
		if err := s.store.DeleteCluster(ctx, clusterUID); err != nil {
			return View{}, false, err
		}
		return View{}, true, nil
	}
	view, err := s.refreshView(ctx, clusterUID, faces)
	return view, false, err
}

// refreshView recomputes the cluster's centroid and size from faces, then returns
// its refreshed listing view.
func (s *Service) refreshView(ctx context.Context, clusterUID string, faces []Face) (View, error) {
	vecs := make([][]float32, len(faces))
	for i := range faces {
		vecs[i] = faces[i].Vector
	}
	if err := s.store.RefreshCluster(ctx, clusterUID, vectors.Centroid(vecs), len(faces)); err != nil {
		return View{}, err
	}
	refreshed, err := s.store.GetCluster(ctx, clusterUID)
	if err != nil {
		return View{}, err
	}
	return s.clusterView(ctx, refreshed)
}
