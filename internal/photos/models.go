// Package photos is Kukátko's core photo catalogue: the typed models and the
// pgx-backed repository for the photos table and its satellites (photo_files,
// photo_phashes, photo_edits). It deduplicates on the SHA256 file_hash and
// records external IDs (PhotoPrism, photo-sorter) so imports and the
// photo-sorter migration stay idempotent.
package photos

import (
	"encoding/json"
	"errors"
	"time"
)

// Sentinel errors returned by the store so callers (handlers, importers, tests)
// can branch with errors.Is.
var (
	// ErrPhotoNotFound indicates no photo matched the given key.
	ErrPhotoNotFound = errors.New("photos: photo not found")
	// ErrFileHashTaken indicates a photos.file_hash unique-constraint violation,
	// i.e. an identical original is already catalogued (dedup hit).
	ErrFileHashTaken = errors.New("photos: file hash already exists")
	// ErrFileNotFound indicates no photo_files row matched the given key.
	ErrFileNotFound = errors.New("photos: file not found")
	// ErrFilePathTaken indicates a (photo_uid, file_path) unique-constraint
	// violation on photo_files.
	ErrFilePathTaken = errors.New("photos: file path already exists for photo")
	// ErrPrimaryFileExists indicates an attempt to mark a second file primary
	// while the photo already has one (one-primary-per-photo constraint).
	ErrPrimaryFileExists = errors.New("photos: photo already has a primary file")
	// ErrPhashNotFound indicates no photo_phashes row matched the given photo.
	ErrPhashNotFound = errors.New("photos: perceptual hashes not found")
	// ErrEditNotFound indicates no photo_edits row matched the given photo.
	ErrEditNotFound = errors.New("photos: edits not found")
)

// FileRole enumerates the kind of file a photo_files row represents.
type FileRole string

// The recognised photo_files roles, mirrored by the SQL CHECK constraint.
const (
	// RoleOriginal is the imported/uploaded source file.
	RoleOriginal FileRole = "original"
	// RoleSidecar is an associated sidecar (e.g. XMP, RAW companion JPEG).
	RoleSidecar FileRole = "sidecar"
	// RoleEdited is a rendered derivative of a non-destructive edit.
	RoleEdited FileRole = "edited"
)

// Photo is one catalogued image or video. Mutable text fields are plain strings
// (the columns default to the empty string in SQL); genuinely optional values
// use pointers so
// a missing value is distinguishable from a zero value. Exif holds the raw EXIF
// document as JSONB, nil when absent.
type Photo struct {
	UID             string `json:"uid"`
	FileHash        string `json:"file_hash"`
	FilePath        string `json:"file_path"`
	FileName        string `json:"file_name"`
	FileSize        int64  `json:"file_size"`
	FileMime        string `json:"file_mime"`
	FileWidth       int    `json:"file_width"`
	FileHeight      int    `json:"file_height"`
	FileOrientation int    `json:"file_orientation"`

	TakenAt       *time.Time `json:"taken_at,omitempty"`
	TakenAtSource string     `json:"taken_at_source"`

	Title       string `json:"title"`
	Description string `json:"description"`
	Notes       string `json:"notes"`

	Lat      *float64 `json:"lat,omitempty"`
	Lng      *float64 `json:"lng,omitempty"`
	Altitude *float64 `json:"altitude,omitempty"`

	CameraMake  string   `json:"camera_make"`
	CameraModel string   `json:"camera_model"`
	LensModel   string   `json:"lens_model"`
	ISO         *int     `json:"iso,omitempty"`
	Aperture    *float64 `json:"aperture,omitempty"`
	Exposure    string   `json:"exposure"`
	FocalLength *float64 `json:"focal_length,omitempty"`

	Exif json.RawMessage `json:"exif,omitempty"`

	Private    bool       `json:"private"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	UploadedBy *string    `json:"uploaded_by,omitempty"`

	PhotoprismUID      *string `json:"photoprism_uid,omitempty"`
	PhotoprismFileHash *string `json:"photoprism_file_hash,omitempty"`
	PhotosorterUID     *string `json:"photosorter_uid,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PhotoFile is an original or derivative file belonging to a photo. At most one
// row per photo may have IsPrimary set.
type PhotoFile struct {
	ID        int64     `json:"id"`
	PhotoUID  string    `json:"photo_uid"`
	FilePath  string    `json:"file_path"`
	FileHash  string    `json:"file_hash"`
	FileSize  int64     `json:"file_size"`
	FileMime  string    `json:"file_mime"`
	IsPrimary bool      `json:"is_primary"`
	Role      FileRole  `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Phash holds the perceptual hashes used for near-duplicate detection. Both
// hashes are stored as signed 64-bit integers.
type Phash struct {
	PhotoUID  string    `json:"photo_uid"`
	Phash     int64     `json:"phash"`
	Dhash     int64     `json:"dhash"`
	CreatedAt time.Time `json:"created_at"`
}

// Edit holds non-destructive adjustments for a photo. Crop coordinates are
// normalised to 0..1 and are all-or-nothing (either all four are set or none).
// Rotation is one of 0, 90, 180, 270 degrees.
type Edit struct {
	PhotoUID   string    `json:"photo_uid"`
	CropX      *float64  `json:"crop_x,omitempty"`
	CropY      *float64  `json:"crop_y,omitempty"`
	CropW      *float64  `json:"crop_w,omitempty"`
	CropH      *float64  `json:"crop_h,omitempty"`
	Rotation   int       `json:"rotation"`
	Brightness float64   `json:"brightness"`
	Contrast   float64   `json:"contrast"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// MetadataUpdate carries the user-editable metadata fields applied by
// Store.UpdateMetadata. Pointer fields clear (set NULL) when nil.
type MetadataUpdate struct {
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Notes         string     `json:"notes"`
	TakenAt       *time.Time `json:"taken_at"`
	TakenAtSource string     `json:"taken_at_source"`
	Lat           *float64   `json:"lat"`
	Lng           *float64   `json:"lng"`
	Altitude      *float64   `json:"altitude"`
	Private       bool       `json:"private"`
}
