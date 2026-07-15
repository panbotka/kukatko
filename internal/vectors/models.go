// Package vectors is Kukátko's database access layer for image and face
// embeddings. The vectors live directly in PostgreSQL as pgvector halfvec
// columns (see migration 0006), so similarity search is a plain SQL query
// against an HNSW cosine index rather than a call to an external vector store.
//
// halfvec (float16) is used instead of vector (float32) because, on the
// normalised CLIP/ArcFace embeddings the embeddings sidecar produces, the recall
// loss is negligible while the HNSW index uses roughly half the memory — which
// matters on the Pi. Distance is cosine throughout, expressed with the pgvector
// `<=>` operator; a smaller distance means a closer match.
//
// The Store borrows the shared pgx pool (it owns no connection) and registers no
// types of its own: database.New already registers the pgvector codecs on every
// connection, so halfvec values bind and scan directly.
package vectors

import (
	"errors"
	"time"

	"github.com/pgvector/pgvector-go"
)

// Embedding dimensions for the two embedding spaces Kukátko stores. They match
// the embeddings sidecar contract (CLIP image/text and ArcFace faces) and the
// halfvec column widths in migration 0006.
const (
	// ImageDim is the dimensionality of a CLIP image embedding.
	ImageDim = 768
	// FaceDim is the dimensionality of an ArcFace face embedding.
	FaceDim = 512
)

// defaultLimit and maxLimit bound the number of rows a similarity search may
// return when the caller passes a non-positive or oversized limit.
const (
	defaultLimit = 50
	maxLimit     = 500
)

// noDistanceLimit is substituted for a non-positive maxDistance so the SQL
// distance filter degenerates to "match anything"; the largest possible cosine
// distance is 2, so this comfortably disables the filter.
const noDistanceLimit = 1e9

var (
	// ErrEmbeddingNotFound is returned by GetEmbedding when no image embedding
	// exists for the requested photo.
	ErrEmbeddingNotFound = errors.New("vectors: embedding not found")
	// ErrDimMismatch is returned when a supplied vector does not have the
	// dimensionality expected for its space (ImageDim or FaceDim). It signals a
	// caller/model mismatch and is not transient.
	ErrDimMismatch = errors.New("vectors: vector dimension mismatch")
	// ErrFaceIndexTaken is returned by SaveFaces when two faces in the same batch
	// share a face_index, violating the UNIQUE(photo_uid, face_index) constraint.
	ErrFaceIndexTaken = errors.New("vectors: duplicate face index for photo")
)

// Embedding is a single image embedding row: one CLIP vector per photo plus the
// model tags reported by the sidecar.
type Embedding struct {
	// PhotoUID is the owning photo's uid and the table's primary key.
	PhotoUID string
	// Vector is the ImageDim-element CLIP embedding.
	Vector []float32
	// Model and Pretrained are the sidecar's model identifiers, stored so a later
	// model change can be detected and re-embedded.
	Model      string
	Pretrained string
	// Dim is the stored vector length; it equals len(Vector) on save.
	Dim int
	// CreatedAt is when the row was last written (set by the database).
	CreatedAt time.Time
}

// Face is a single detected face: its embedding, bounding box and the cached
// people-clustering / render metadata stored alongside it.
type Face struct {
	// ID is the database-assigned identity (zero until saved).
	ID int64
	// PhotoUID is the owning photo's uid.
	PhotoUID string
	// FaceIndex is the per-photo face slot; unique within a photo.
	FaceIndex int
	// Vector is the FaceDim-element ArcFace embedding.
	Vector []float32
	// BBox is the normalised bounding box [x, y, w, h] in 0..1.
	BBox [4]float64
	// DetScore is the detector's confidence for this face.
	DetScore float64
	// Model is the sidecar's face model identifier.
	Model string
	// Dim is the stored vector length; it equals len(Vector) on save.
	Dim int
	// CreatedAt is when the row was written (set by the database).
	CreatedAt time.Time
	// MarkerUID and SubjectUID are external identifiers, nil until the face is
	// assigned to a subject by people clustering or PhotoPrism import.
	MarkerUID  *string
	SubjectUID *string
	// SubjectName, PhotoWidth, PhotoHeight and Orientation are denormalised
	// render/display hints cached on the face row.
	SubjectName string
	PhotoWidth  int
	PhotoHeight int
	Orientation int
}

// Match is one image-embedding similarity hit: the photo and its cosine distance
// to the query vector (smaller is closer).
type Match struct {
	PhotoUID string
	Distance float64
}

// FaceMatch is one face-embedding similarity hit: the face row, its owning photo
// and the cosine distance to the query vector (smaller is closer).
type FaceMatch struct {
	ID        int64
	PhotoUID  string
	FaceIndex int
	Distance  float64
}

// FaceCandidate is a face-embedding similarity hit enriched with the cached
// people-assignment columns and the bounding box, so the face-suggestion logic
// can aggregate neighbours by subject and apply a size filter without a second
// query. Distance is the cosine distance to the query vector (smaller is closer).
type FaceCandidate struct {
	// PhotoUID and FaceIndex identify the neighbouring face row.
	PhotoUID  string
	FaceIndex int
	// Distance is the cosine distance to the query vector.
	Distance float64
	// BBox is the neighbour's normalised bounding box [x, y, w, h] in 0..1.
	BBox [4]float64
	// SubjectUID and SubjectName are the cached assignment, nil/empty when the
	// neighbour is not assigned to any subject.
	SubjectUID  *string
	SubjectName string
	// MarkerUID is the cached marker the neighbour is tied to, nil when unmatched.
	MarkerUID *string
}

// FaceKey identifies a face by the (photo, per-photo slot) identity Kukátko uses
// throughout — the same key internal/feedback records a rejection against. It is
// the element of the exclusion set FindSimilarUnassignedFaceCandidates filters out
// in SQL, so a search can drop already-rejected faces without an N+1.
type FaceKey struct {
	// PhotoUID is the owning photo's uid.
	PhotoUID string
	// FaceIndex is the per-photo face slot (faces.face_index).
	FaceIndex int
}

// ToHalfVec wraps a []float32 as a pgvector.HalfVector so it can be bound as a
// halfvec query parameter or column value.
func ToHalfVec(vec []float32) pgvector.HalfVector {
	return pgvector.NewHalfVector(vec)
}

// FromHalfVec returns the []float32 underlying a pgvector.HalfVector scanned from
// a halfvec column.
func FromHalfVec(hv pgvector.HalfVector) []float32 {
	return hv.Slice()
}

// normalizeLimit clamps a caller-supplied row limit into [1, maxLimit], applying
// defaultLimit when the value is non-positive.
func normalizeLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultLimit
	case limit > maxLimit:
		return maxLimit
	default:
		return limit
	}
}

// normalizeMaxDistance returns maxDistance unchanged when positive, or
// noDistanceLimit when non-positive so the distance filter matches everything.
func normalizeMaxDistance(maxDistance float64) float64 {
	if maxDistance <= 0 {
		return noDistanceLimit
	}
	return maxDistance
}
