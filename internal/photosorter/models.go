// Package photosorter is a read-only client for a photo-sorter PostgreSQL
// database. It is the data source for Kukátko's one-off (optionally repeatable)
// photo-sorter migration (see ARCHITECTURE.md §10): because photo-sorter and
// Kukátko share the same embedding models and dimensions (CLIP 768 + InsightFace
// 512) and the same SHA256 file hashes, the migration transfers embeddings and
// faces 1:1 without recomputation and deduplicates photos directly.
//
// The client opens its own pgx pool against a read-only DSN and never writes. The
// pgvector types are registered on every connection so the vector(768)/vector(512)
// embedding columns scan straight into []float32. An optional schema name scopes
// every query (via search_path) so an integration test can seed a fake
// photo-sorter schema alongside Kukátko's own tables in one database.
//
// Only the tables the migration cares about are exposed; photo-book and
// share-link tables are deliberately never read.
package photosorter

import (
	"errors"
	"time"
)

// Sentinel errors returned by the reader so callers (the migration, tests) can
// branch with errors.Is.
var (
	// ErrInvalidDSN indicates the supplied connection string could not be parsed.
	ErrInvalidDSN = errors.New("photosorter: invalid dsn")
)

// ListParams bounds a paged catalogue listing (subjects, albums, labels). A
// non-positive Limit defaults to DefaultPageSize.
type ListParams struct {
	// Limit caps the page size.
	Limit int
	// Offset skips the first Offset rows.
	Offset int
}

// PhotoListParams bounds an incremental, paged photo listing ordered by
// updated_at so a migration can resume from the last successful watermark.
type PhotoListParams struct {
	// UpdatedSince, when non-zero, restricts the page to photos modified strictly
	// after it (the resume cursor). The zero value lists every photo.
	UpdatedSince time.Time
	// Limit caps the page size; a non-positive value defaults to DefaultPageSize.
	Limit int
	// Offset skips the first Offset rows of the (stable, read-only) result set.
	Offset int
}

// Photo is one row of photo-sorter's photos table, carrying the metadata the
// migration maps onto a Kukátko photo. Optional values use pointers so a missing
// value is distinguishable from a zero value.
type Photo struct {
	UID             string
	FileHash        string
	FilePath        string
	FileName        string
	FileSize        int64
	FileMime        string
	FileWidth       int
	FileHeight      int
	FileOrientation int
	TakenAt         *time.Time
	TakenAtSource   string
	Title           string
	Description     string
	Notes           string
	Lat             *float64
	Lng             *float64
	Altitude        *float64
	CameraMake      string
	CameraModel     string
	LensModel       string
	ISO             *int
	Aperture        *float64
	Exposure        string
	FocalLength     *float64
	Exif            []byte
	// Keywords is the IPTC keyword list, stored as a TEXT[] in photo-sorter; the
	// migration joins and normalises it into Kukátko's comma-separated column.
	Keywords []string
	// Artist, Copyright, License and Software are the IPTC/XMP credit fields.
	Artist    string
	Copyright string
	License   string
	Software  string
	// Scan marks an image digitised from a physical print; Panorama flags a
	// panoramic image (mapped to Kukátko's projection column).
	Scan       bool
	Panorama   bool
	Private    bool
	ArchivedAt *time.Time
	UpdatedAt  time.Time
}

// Embedding is one row of photo-sorter's embeddings table: a CLIP image vector
// plus its model tags, keyed by photo UID.
type Embedding struct {
	PhotoUID   string
	Vector     []float32
	Model      string
	Pretrained string
}

// Face is one row of photo-sorter's faces table: an InsightFace embedding, its
// bounding box and detection score, plus the denormalised people-assignment
// cache columns that transfer across unchanged (after the subject UID is
// remapped).
type Face struct {
	PhotoUID    string
	FaceIndex   int
	Vector      []float32
	BBox        [4]float64
	DetScore    float64
	Model       string
	MarkerUID   *string
	SubjectUID  *string
	SubjectName string
	PhotoWidth  int
	PhotoHeight int
	Orientation int
}

// Subject is one row of photo-sorter's subjects table: a named person, pet or
// other entity that faces are grouped under.
type Subject struct {
	UID      string
	Slug     string
	Name     string
	Type     string
	Favorite bool
	Private  bool
	Notes    string
}

// Marker is one row of photo-sorter's markers table: a normalised [x, y, w, h]
// region on a photo, optionally tied to a subject.
type Marker struct {
	UID        string
	PhotoUID   string
	SubjectUID *string
	Type       string
	X          float64
	Y          float64
	W          float64
	H          float64
	Score      int
	Invalid    bool
	Reviewed   bool
}

// Album is one row of photo-sorter's albums table.
type Album struct {
	UID         string
	Slug        string
	Title       string
	Description string
	Type        string
	Private     bool
}

// AlbumPhoto is one membership row tying a photo to an album with its sort order.
type AlbumPhoto struct {
	AlbumUID  string
	PhotoUID  string
	SortOrder int
}

// Label is one row of photo-sorter's labels table.
type Label struct {
	UID      string
	Slug     string
	Name     string
	Priority int
}

// PhotoLabel is one membership row tying a photo to a label with its provenance.
type PhotoLabel struct {
	PhotoUID    string
	LabelUID    string
	Source      string
	Uncertainty int
}

// Phash is one row of photo-sorter's photo_phashes table: the perceptual hashes
// used for near-duplicate detection.
type Phash struct {
	PhotoUID string
	Phash    int64
	Dhash    int64
}

// Edit is one row of photo-sorter's photo_edits table: the non-destructive crop,
// rotation and tone adjustments for a photo.
type Edit struct {
	PhotoUID   string
	CropX      *float64
	CropY      *float64
	CropW      *float64
	CropH      *float64
	Rotation   int
	Brightness float64
	Contrast   float64
}
