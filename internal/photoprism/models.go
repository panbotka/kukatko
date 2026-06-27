package photoprism

import "time"

// Photo is the subset of a PhotoPrism photo-search result that Kukátko imports.
// Field names match PhotoPrism's JSON keys (upper-camel-case) via struct tags.
// A photo carries one or more Files; the primary file's SHA1 Hash identifies the
// original to download.
type Photo struct {
	// UID is PhotoPrism's stable photo identifier (stored as photoprism_uid).
	UID string `json:"UID"`
	// Type is the media kind (e.g. "image", "video", "live", "raw").
	Type string `json:"Type"`
	// Title and Description are user-facing metadata.
	Title       string `json:"Title"`
	Description string `json:"Description"`
	// TakenAt is the capture time; TakenAtLocal is its local-zone rendering.
	TakenAt      time.Time `json:"TakenAt"`
	TakenAtLocal time.Time `json:"TakenAtLocal"`
	// UpdatedAt drives the incremental high-watermark (max UpdatedAt per run).
	UpdatedAt time.Time `json:"UpdatedAt"`
	CreatedAt time.Time `json:"CreatedAt"`
	// Lat, Lng and Altitude are the GPS position; zero when ungeotagged.
	Lat      float64 `json:"Lat"`
	Lng      float64 `json:"Lng"`
	Altitude int     `json:"Altitude"`
	// Width and Height are the pixel dimensions of the primary file.
	Width  int `json:"Width"`
	Height int `json:"Height"`
	// OriginalName is the file's original base name at import into PhotoPrism.
	OriginalName string `json:"OriginalName"`
	// Camera, lens and core EXIF fields, mapped 1:1 onto Kukátko's photo model.
	CameraMake  string  `json:"CameraMake"`
	CameraModel string  `json:"CameraModel"`
	LensModel   string  `json:"LensModel"`
	Iso         int     `json:"Iso"`
	FNumber     float64 `json:"FNumber"`
	Exposure    string  `json:"Exposure"`
	FocalLength int     `json:"FocalLength"`
	// Favorite and Private mirror PhotoPrism's per-photo flags.
	Favorite bool `json:"Favorite"`
	Private  bool `json:"Private"`
	// Files are the photo's underlying files (merged=true on the listing).
	Files []File `json:"Files"`
}

// PrimaryFile returns the photo's primary File and true, or a zero File and
// false when no file is marked primary. The primary file's Hash is the SHA1 used
// to download the original.
func (p Photo) PrimaryFile() (File, bool) {
	for _, f := range p.Files {
		if f.Primary {
			return f, true
		}
	}
	return File{}, false
}

// File is one underlying file of a PhotoPrism photo. Hash is the SHA1 content
// hash PhotoPrism uses both to identify the file and as the download key.
type File struct {
	// UID is PhotoPrism's stable file identifier.
	UID string `json:"UID"`
	// Hash is the SHA1 content hash; Kukátko stores it as photoprism_file_hash and
	// downloads the original via /api/v1/dl/<Hash>.
	Hash string `json:"Hash"`
	// Name is the file's storage-relative path within PhotoPrism's originals.
	Name string `json:"Name"`
	// Primary marks the representative file of the photo.
	Primary bool `json:"Primary"`
	// Mime is the file's MIME type.
	Mime string `json:"Mime"`
	// Root is PhotoPrism's storage root tag (e.g. "/" for originals).
	Root string `json:"Root"`
	// Width and Height are the file's pixel dimensions.
	Width  int `json:"Width"`
	Height int `json:"Height"`
	// FileType is PhotoPrism's file-type tag (e.g. "jpg", "mp4").
	FileType string `json:"FileType"`
	// Markers are the face/label regions detected on this file.
	Markers []Marker `json:"Markers"`
}

// Marker is a face or label region on a file, used to seed Kukátko's markers and
// to associate faces with subjects (people). Coordinates are normalised (0..1).
type Marker struct {
	// UID is PhotoPrism's stable marker identifier.
	UID string `json:"UID"`
	// FileUID and FileHash link the marker back to its file.
	FileUID  string `json:"FileUID"`
	FileHash string `json:"FileHash"`
	// Type is "face" or "label".
	Type string `json:"Type"`
	// Name is the marker's display name (e.g. the recognised person's name).
	Name string `json:"Name"`
	// SubjUID and SubjSrc identify the subject this marker is assigned to and the
	// source of that assignment.
	SubjUID string `json:"SubjUID"`
	SubjSrc string `json:"SubjSrc"`
	// X, Y, W and H are the normalised bounding box (top-left origin, 0..1).
	X float64 `json:"X"`
	Y float64 `json:"Y"`
	W float64 `json:"W"`
	H float64 `json:"H"`
	// Score is PhotoPrism's detection/quality score for the marker.
	Score int `json:"Score"`
	// Invalid marks a region rejected as not a real face/label.
	Invalid bool `json:"Invalid"`
	// Review marks a marker awaiting human confirmation.
	Review bool `json:"Review"`
}

// Album is the subset of a PhotoPrism album surfaced for import.
type Album struct {
	// UID is PhotoPrism's stable album identifier.
	UID string `json:"UID"`
	// Title, Slug and Description are user-facing metadata.
	Title       string `json:"Title"`
	Slug        string `json:"Slug"`
	Description string `json:"Description"`
	// Type is PhotoPrism's album kind (e.g. "album", "folder", "moment", "month").
	Type string `json:"Type"`
	// Category groups albums (e.g. by topic); empty when uncategorised.
	Category string `json:"Category"`
	// Favorite and Private mirror PhotoPrism's per-album flags.
	Favorite bool `json:"Favorite"`
	Private  bool `json:"Private"`
	// CreatedAt and UpdatedAt are PhotoPrism's timestamps.
	CreatedAt time.Time `json:"CreatedAt"`
	UpdatedAt time.Time `json:"UpdatedAt"`
}

// Label is the subset of a PhotoPrism label surfaced for import.
type Label struct {
	// UID is PhotoPrism's stable label identifier.
	UID string `json:"UID"`
	// Name and Slug are the label's display name and URL slug.
	Name string `json:"Name"`
	Slug string `json:"Slug"`
	// Priority orders labels (higher first); Favorite mirrors PhotoPrism's flag.
	Priority int  `json:"Priority"`
	Favorite bool `json:"Favorite"`
}

// Subject is the subset of a PhotoPrism subject (a person, pet, or other named
// entity) surfaced for import.
type Subject struct {
	// UID is PhotoPrism's stable subject identifier.
	UID string `json:"UID"`
	// Type is the subject kind (e.g. "person").
	Type string `json:"Type"`
	// Name and Slug are the subject's display name and URL slug.
	Name string `json:"Name"`
	Slug string `json:"Slug"`
	// Favorite and Private mirror PhotoPrism's per-subject flags.
	Favorite bool `json:"Favorite"`
	Private  bool `json:"Private"`
	// FileCount is how many files PhotoPrism associates with the subject.
	FileCount int `json:"FileCount"`
}
