// Package cluster implements face auto-clustering: it groups currently
// unassigned faces (faces with no subject) into clusters of the same person so a
// whole cluster can be named in one action, rather than one face at a time as
// photo-sorter required.
//
// Clustering is greedy connected components over the HNSW nearest neighbours of
// each clusterable face within a cosine-distance threshold: two faces closer than
// the threshold share an edge, and each connected component of at least the
// minimum size becomes a cluster. A face is clusterable only when it is
// unassigned (subject_uid IS NULL) and not yet in a cluster (cluster_uid IS NULL),
// so re-clustering is incremental — it never touches assigned or already-clustered
// faces. Each cluster caches the L2-normalised mean of its members' embeddings
// (the centroid), used to pick a representative face and to suggest the nearest
// already-named subject.
//
// The DB access lives in Store; the orchestration (Recluster, ListClusters,
// AssignCluster, RemoveFace) lives in Service, which reuses the vectors HNSW
// search and the facematch assignment state machine so marker creation stays in
// one place.
package cluster

import (
	"errors"
	"time"

	"github.com/panbotka/kukatko/internal/people"
)

// Sentinel errors returned by the Service and Store so callers (handlers, tests)
// can branch with errors.Is.
var (
	// ErrClusterNotFound indicates no cluster matched the given uid.
	ErrClusterNotFound = errors.New("cluster: cluster not found")
	// ErrEmptyCluster indicates an assignment targeted a cluster with no faces.
	ErrEmptyCluster = errors.New("cluster: cluster has no faces")
	// ErrMissingSubject indicates an assignment named neither a subject uid nor a
	// subject name to resolve.
	ErrMissingSubject = errors.New("cluster: subject_uid or subject_name is required")
	// ErrFaceNotInCluster indicates a remove-face request named a face that is not
	// a member of the cluster.
	ErrFaceNotInCluster = errors.New("cluster: face is not in the cluster")
)

// Face is a clusterable detected face: its database identity, location and
// embedding, loaded for the clustering algorithm and for building cluster views.
type Face struct {
	// ID is the faces row identity, used to map HNSW neighbours back to members.
	ID int64
	// PhotoUID and FaceIndex identify the face within its photo.
	PhotoUID  string
	FaceIndex int
	// Vector is the FaceDim-element ArcFace embedding.
	Vector []float32
	// BBox is the normalised bounding box [x, y, w, h] in 0..1.
	BBox [4]float64
	// DetScore is the detector confidence for the face.
	DetScore float64
	// Model is the sidecar's face model identifier, recorded on the cluster.
	Model string
}

// Ref identifies one face by its photo and per-photo index, used when adding
// faces to or removing them from a cluster.
type Ref struct {
	PhotoUID  string
	FaceIndex int
}

// Cluster is one face_clusters row: a group of same-person faces plus the cached
// centroid used for suggestions and representative selection.
type Cluster struct {
	UID       string    `json:"uid"`
	Centroid  []float32 `json:"-"`
	Size      int       `json:"size"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Suggestion is one likely identity for a cluster: the nearest already-named
// subject, how close (cosine distance) and how confident (1 - distance, clamped)
// the match is.
type Suggestion struct {
	SubjectUID  string  `json:"subject_uid"`
	SubjectName string  `json:"subject_name"`
	Distance    float64 `json:"distance"`
	Confidence  float64 `json:"confidence"`
}

// ExampleFace is one face shown for a cluster in its listing: enough to render a
// cropped thumbnail (photo thumbnail plus the normalised box) without loading the
// embedding.
type ExampleFace struct {
	PhotoUID  string     `json:"photo_uid"`
	FaceIndex int        `json:"face_index"`
	BBox      [4]float64 `json:"bbox"`
	DetScore  float64    `json:"det_score"`
}

// View is one cluster in the listing response: its size, a representative
// face, a handful of example faces and the suggested existing subject (nil when
// no named neighbour is close enough).
type View struct {
	UID            string        `json:"uid"`
	Size           int           `json:"size"`
	Representative ExampleFace   `json:"representative"`
	Examples       []ExampleFace `json:"examples"`
	Suggestion     *Suggestion   `json:"suggestion,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
}

// AssignRequest names a cluster and the subject every face in it should be
// assigned to: by SubjectUID, or — failing that — by SubjectName (find-or-create
// by slug, performed by the underlying assignment state machine).
type AssignRequest struct {
	ClusterUID  string `json:"-"`
	SubjectUID  string `json:"subject_uid,omitempty"`
	SubjectName string `json:"subject_name,omitempty"`
}

// AssignResult is the outcome of assigning a whole cluster: the subject every
// face was assigned to and the markers created for each member face.
type AssignResult struct {
	ClusterUID string          `json:"cluster_uid"`
	Subject    people.Subject  `json:"subject"`
	Markers    []people.Marker `json:"markers"`
}
