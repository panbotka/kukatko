package photoprism

import (
	"strings"
	"time"
)

// Photo is the subset of a PhotoPrism photo-search result that Kukátko imports.
// Field names match PhotoPrism's JSON keys (upper-camel-case) via struct tags.
// A photo carries one or more Files; the primary file's SHA1 Hash identifies the
// original to download.
type Photo struct {
	// UID is PhotoPrism's stable photo identifier (stored as photoprism_uid).
	UID string `json:"UID"`
	// Type is the media kind (e.g. "image", "video", "live", "raw").
	Type string `json:"Type"`
	// Title is the photo's headline.
	Title string `json:"Title"`
	// Caption is the photo's long description — the live field. PhotoPrism renamed
	// photo_description to photo_caption; Description below is its dead predecessor,
	// still serialised but no longer persisted (the Go field is gorm:"-"), so a
	// caption read from Description is always empty on a current PhotoPrism. Both are
	// modelled: Caption is what a current instance answers with, Description what an
	// old one does, and the importer prefers the first non-empty of the two.
	Caption     string `json:"Caption"`
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
	// CameraSerial is the camera body's serial number.
	CameraSerial string `json:"CameraSerial"`
	// Scan marks an image digitised from a physical print or negative rather than
	// captured by a camera.
	Scan bool `json:"Scan"`
	// Favorite and Private mirror PhotoPrism's per-photo flags.
	Favorite bool `json:"Favorite"`
	Private  bool `json:"Private"`
	// Files are the photo's underlying files (merged=true on the listing).
	Files []File `json:"Files"`
}

// PhotoDetail is a single photo read from the photo *detail* endpoint. It is a
// Photo plus everything the photo *listing* payload does not carry: the two
// relations (every album the photo belongs to and every label it is tagged with),
// the Details block (the IPTC/XMP credit fields), and — on the photo's files — the
// face markers and the per-file technicals. A scoped import reads it per photo so a
// photo migrated because it sits in one album still arrives with the other albums,
// the labels and the credits it also carries.
//
// The listing endpoint (`GET /photos?merged=true`) answers a flattened search
// struct that has NO Details object at all: Subject, Artist, Copyright, License,
// Keywords, Notes and Software exist ONLY here. Read them off a listed photo and
// they are silently all empty.
type PhotoDetail struct {
	Photo
	// Details are the IPTC/XMP credit fields. PhotoPrism keeps them in a side table
	// and a photo indexed by an old version may have no details row at all, in which
	// case the endpoint answers null and this is the zero value.
	Details Details `json:"Details"`
	// Albums is every album the photo is a member of, of any album type.
	Albums []Album `json:"Albums"`
	// Labels is every label attached to the photo, each with the source and
	// uncertainty of its attachment.
	Labels []PhotoLabel `json:"Labels"`
}

// Details is PhotoPrism's photo_details row: the IPTC/XMP credit metadata it keeps
// beside a photo rather than on it. It is served on the photo detail endpoint only.
type Details struct {
	// Keywords is the IPTC keyword list as one comma-separated string.
	Keywords string `json:"Keywords"`
	// Notes is the free-text note field.
	Notes string `json:"Notes"`
	// Subject is the IPTC subject/headline — what the photo is about.
	Subject string `json:"Subject"`
	// Artist is who made the photo (IPTC By-line / XMP dc:creator).
	Artist string `json:"Artist"`
	// Copyright is the copyright notice.
	Copyright string `json:"Copyright"`
	// License is the licence the photo is published under.
	License string `json:"License"`
	// Software is what produced the image (camera firmware, an editor, a scanner).
	Software string `json:"Software"`
}

// PhotoLabel is one label attached to a photo, together with where PhotoPrism
// got the attachment from and how uncertain it is about it.
type PhotoLabel struct {
	// LabelSrc is PhotoPrism's source tag of the attachment ("manual" for a
	// hand-attached label, "image" for a vision-classified one, and "batch",
	// "keyword", "location", "meta"… for the ones PhotoPrism derived itself).
	LabelSrc string `json:"LabelSrc"`
	// Uncertainty is the classifier's uncertainty as a percentage (0 = certain).
	Uncertainty int `json:"Uncertainty"`
	// Label is the attached label itself.
	Label Label `json:"Label"`
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

// VideoFile returns the photo's first video file (the motion clip of a video or
// live photo) and true, or a zero File and false when the photo has no video
// stream (a plain still image). It backs the video/live import path, which stores
// the actual motion file rather than PhotoPrism's generated still.
func (p Photo) VideoFile() (File, bool) {
	for _, f := range p.Files {
		if f.IsVideo() {
			return f, true
		}
	}
	return File{}, false
}

// StillFile returns the photo's still-image file and true: the primary file when
// it is not itself a video, otherwise the first non-video file. It returns false
// when every file is a video (a plain clip with no extracted frame). It backs the
// live-photo import path, which stores the still as the primary original.
func (p Photo) StillFile() (File, bool) {
	if f, ok := p.PrimaryFile(); ok && !f.IsVideo() {
		return f, true
	}
	for _, f := range p.Files {
		if !f.IsVideo() {
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
	// Video marks the file as a playable video stream; PhotoPrism sets it on the
	// motion file of a video or live photo.
	Video bool `json:"Video"`
	// Codec is the file's media codec — "avc1"/"hvc1" for a video stream, the still
	// image's compression ("jpeg", "heic") for an image. It is served on the photo
	// detail only; a listed photo's files carry it empty.
	Codec string `json:"Codec"`
	// ColorProfile names the file's embedded ICC profile ("sRGB", "Display P3").
	// Detail-only, like Codec.
	ColorProfile string `json:"ColorProfile"`
	// Projection is a panorama's projection ("equirectangular"), empty for an
	// ordinary photo. Detail-only, like Codec.
	Projection string `json:"Projection"`
	// Markers are the face/label regions detected on this file.
	Markers []Marker `json:"Markers"`
}

// IsVideo reports whether the file is a playable video stream, recognised by
// PhotoPrism's Video flag or a video/* MIME type. It is the criterion the import
// uses to tell a video or live photo's motion file from its still.
func (f File) IsVideo() bool {
	return f.Video || strings.HasPrefix(strings.ToLower(f.Mime), "video/")
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
