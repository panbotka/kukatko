// Package sidecarexport writes a photo's metadata and curation to a YAML sidecar
// file in storage, so the catalogue can be rebuilt from the storage alone —
// originals plus sidecars, no database.
//
// Everything a user builds in Kukátko — titles, who is in the photo, which album
// it belongs to, the rating — otherwise exists in exactly one place: Postgres.
// There is an S3 backup, but that is a single mechanism, and a backup quietly
// failing for three months is discovered on the day it is needed. A sidecar is a
// second, independent one of a different kind: the curation lives next to the
// photo it describes, in a plain-text file any tool can read, on the same storage
// that holds the original.
//
// This package is the export half. Reading sidecars back and rebuilding the
// catalogue from them is a separate concern and deliberately not here; the format
// is designed to be sufficient for it, which is what TestRoundTrip pins.
//
// Do not confuse it with internal/sidecar, which reads *inbound* sidecars written
// by other software (Google Takeout JSON, Apple XMP) during import. This package
// only ever writes, and only ever Kukátko's own format.
//
// The format is documented in full in docs/RESTORE.md — the document someone
// reads when the database is gone.
package sidecarexport

import (
	"time"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
)

// Version is the sidecar schema version, written as the document's first key. It
// is bumped when the format changes in a way a reader must know about; a reader
// that meets a version it does not understand should refuse the file rather than
// guess at it.
//
// Version 1 is the initial format.
const Version = 1

// Document is one photo's sidecar: everything a human created or a machine
// derived that would be expensive or impossible to recompute from the original
// alone.
//
// It is deliberately grouped rather than flat — a person who opens this file cold
// should be able to find what they are looking for — and every group is optional
// except identity, so a sparse photo yields a short file.
//
// What is deliberately NOT in here: the image embedding and the face vectors. See
// the header written by Marshal for why.
type Document struct {
	// Version is the schema version — see Version. It is first so a reader can
	// dispatch on it before parsing the rest.
	Version int `yaml:"version"`
	// GeneratedAt is when this file was written. It is provenance, not metadata
	// about the photo: it says how current the file is, which is the first thing
	// someone rebuilding from sidecars wants to know.
	GeneratedAt time.Time `yaml:"generated_at"`

	// Identity is what this file is about and which bytes it describes.
	Identity Identity `yaml:"identity"`
	// Descriptive is the text a human wrote or an AI suggested.
	Descriptive Descriptive `yaml:"descriptive,omitempty"`
	// Temporal is when the photo was taken and how well that is known.
	Temporal Temporal `yaml:"temporal,omitempty"`
	// Spatial is where the photo was taken, omitted entirely for a photo with no
	// location and no cached place.
	Spatial *Spatial `yaml:"spatial,omitempty"`
	// Technical describes the camera and the file.
	Technical Technical `yaml:"technical,omitempty"`
	// Curation is what the users did with the photo — the part that exists nowhere
	// else and the reason this file exists at all.
	Curation Curation `yaml:"curation,omitempty"`
	// Edit is the non-destructive edit, omitted when the photo has none or the edit
	// is a no-op.
	Edit *Edit `yaml:"edit,omitempty"`
}

// Identity names the photo and the bytes it describes. It is the only mandatory
// group: without it a sidecar cannot be matched back to an original.
type Identity struct {
	// UID is the photo's Kukátko UID. A rebuild may or may not honour it, but a
	// sidecar that does not carry it cannot be correlated with anything else that
	// references the photo.
	UID string `yaml:"uid"`
	// SHA256 is the original file's content hash, lowercase hex. It is the durable
	// link between this file and its original: paths move, content does not.
	SHA256 string `yaml:"sha256"`
	// FileName is the original's name within the storage layout, and FilePath its
	// storage key (for example 2024/05/IMG_1234.jpg).
	FileName string `yaml:"file_name"`
	FilePath string `yaml:"file_path"`
	// OriginalName is the name the file carried before ingest, when it differs from
	// the name it was given in storage.
	OriginalName string `yaml:"original_name,omitempty"`
	// MediaType is image, video or live.
	MediaType string `yaml:"media_type,omitempty"`
	// UploadedBy is the username of whoever uploaded the photo, empty when unknown
	// or when the uploading account is gone.
	UploadedBy string `yaml:"uploaded_by,omitempty"`
	// External carries the identifiers of the systems this photo was imported
	// from, so a re-import can recognise what it already has instead of
	// duplicating it.
	External *External `yaml:"external,omitempty"`
}

// External holds the source-system identifiers of an imported photo.
type External struct {
	// PhotoprismUID and PhotoprismFileHash identify the photo in PhotoPrism. The
	// hash is PhotoPrism's SHA1, not Kukátko's SHA256.
	PhotoprismUID      string `yaml:"photoprism_uid,omitempty"`
	PhotoprismFileHash string `yaml:"photoprism_file_hash,omitempty"`
	// PhotosorterUID identifies the photo in photo-sorter.
	PhotosorterUID string `yaml:"photosorter_uid,omitempty"`
}

// Descriptive is the free text on a photo: what a human wrote, what an AI
// suggested, and the IPTC/XMP credit fields the file or the user supplied.
type Descriptive struct {
	Title       string `yaml:"title,omitempty"`
	Description string `yaml:"description,omitempty"`
	Notes       string `yaml:"notes,omitempty"`
	// AiNote is free text produced by an automatic classification pass.
	AiNote string `yaml:"ai_note,omitempty"`
	// Subject is the IPTC subject/headline — what the photo is about.
	Subject string `yaml:"subject,omitempty"`
	// Keywords are the IPTC keywords, comma-separated, verbatim as the source file
	// wrote them. They are deliberately not labels: labels are Kukátko's own
	// curated taxonomy and live under curation.
	Keywords string `yaml:"keywords,omitempty"`
	// Artist, Copyright and License are the IPTC/XMP credit fields.
	Artist    string `yaml:"artist,omitempty"`
	Copyright string `yaml:"copyright,omitempty"`
	License   string `yaml:"license,omitempty"`
}

// Temporal is when the photo was taken and how much that is trusted.
type Temporal struct {
	// TakenAt is the capture time, nil when nothing is known.
	TakenAt *time.Time `yaml:"taken_at,omitempty"`
	// TakenAtSource records where TakenAt came from (exif, manual, …).
	TakenAtSource string `yaml:"taken_at_source,omitempty"`
	// Estimated marks TakenAt as a guess rather than a fact, and Note records in
	// the user's own words what the guess rests on ("kolem roku 1950"). A photo
	// with no TakenAt at all may still be estimated, in which case the note carries
	// the whole meaning.
	Estimated bool   `yaml:"estimated,omitempty"`
	Note      string `yaml:"note,omitempty"`
}

// Spatial is where the photo was taken, plus the reverse-geocoded place. The
// place is cached rather than derived on demand because geocoding costs credits;
// recording it here means a rebuild does not pay for it twice.
type Spatial struct {
	Lat      *float64 `yaml:"lat,omitempty"`
	Lng      *float64 `yaml:"lng,omitempty"`
	Altitude *float64 `yaml:"altitude,omitempty"`
	// Source records where the coordinates came from: exif (the file's GPS),
	// manual (the user decided), estimate (inferred from photos taken nearby in
	// time) or empty (unknown). It matters: an inferred location must never pass
	// itself off as a measured one, and a rebuild that dropped this would promote
	// every guess to a fact.
	//
	// "manual" with no coordinates is not a contradiction but a tombstone: the user
	// deleted the location on purpose.
	Source string `yaml:"source,omitempty"`
	// Place is the cached reverse-geocode result, omitted when the photo has never
	// been geocoded.
	Place *Place `yaml:"place,omitempty"`
}

// Place is a cached reverse-geocode result.
type Place struct {
	Country string `yaml:"country,omitempty"`
	Region  string `yaml:"region,omitempty"`
	City    string `yaml:"city,omitempty"`
	Name    string `yaml:"name,omitempty"`
	// GeocodedAt is when the lookup was made, so a rebuild can tell a fresh result
	// from a decade-old one.
	GeocodedAt *time.Time `yaml:"geocoded_at,omitempty"`
}

// Technical describes the camera and the file. Most of it is recomputable from
// the original, and it is recorded anyway: it is small, and a sidecar that can be
// read on its own is worth more than a few saved bytes.
type Technical struct {
	CameraMake   string   `yaml:"camera_make,omitempty"`
	CameraModel  string   `yaml:"camera_model,omitempty"`
	CameraSerial string   `yaml:"camera_serial,omitempty"`
	LensModel    string   `yaml:"lens_model,omitempty"`
	ISO          *int     `yaml:"iso,omitempty"`
	Aperture     *float64 `yaml:"aperture,omitempty"`
	Exposure     string   `yaml:"exposure,omitempty"`
	FocalLength  *float64 `yaml:"focal_length,omitempty"`

	Width       int `yaml:"width,omitempty"`
	Height      int `yaml:"height,omitempty"`
	Orientation int `yaml:"orientation,omitempty"`

	FileSize int64  `yaml:"file_size,omitempty"`
	FileMIME string `yaml:"file_mime,omitempty"`

	Software     string `yaml:"software,omitempty"`
	Scan         bool   `yaml:"scan,omitempty"`
	ColorProfile string `yaml:"color_profile,omitempty"`
	ImageCodec   string `yaml:"image_codec,omitempty"`
	Projection   string `yaml:"projection,omitempty"`

	// Video is present only for videos and live photos.
	Video *Video `yaml:"video,omitempty"`
}

// Video is the container detail of a video or live photo.
type Video struct {
	DurationMs *int     `yaml:"duration_ms,omitempty"`
	VideoCodec string   `yaml:"video_codec,omitempty"`
	AudioCodec string   `yaml:"audio_codec,omitempty"`
	HasAudio   bool     `yaml:"has_audio,omitempty"`
	FPS        *float64 `yaml:"fps,omitempty"`
}

// Curation is what the users did with the photo. This is the group that exists
// nowhere else: everything above can, in the last resort, be re-derived from the
// original, and none of this can.
type Curation struct {
	// Albums is every album the photo belongs to.
	Albums []Album `yaml:"albums,omitempty"`
	// Labels is every label attached to the photo, with its provenance.
	Labels []Label `yaml:"labels,omitempty"`
	// People is every marker on the photo — the face boxes and who is in them. A
	// marker without its box cannot be rebuilt, so the box is recorded even for a
	// marker nobody has named yet.
	People []Person `yaml:"people,omitempty"`
	// Favorites and Ratings are per-user, so every user's is recorded: favorites
	// and ratings in Kukátko are personal, not a single value on the photo.
	Favorites []Favorite `yaml:"favorites,omitempty"`
	Ratings   []Rating   `yaml:"ratings,omitempty"`
	// Private hides the photo from shared views.
	Private bool `yaml:"private,omitempty"`
	// ArchivedAt is set when the photo is in the trash, awaiting purge. A rebuild
	// should honour it: a photo the user deleted, silently resurrected, is the
	// worst kind of restore bug.
	ArchivedAt *time.Time `yaml:"archived_at,omitempty"`
	// Stack groups this photo with the other files of the same shot.
	Stack *Stack `yaml:"stack,omitempty"`
}

// Album is one album membership.
type Album struct {
	// UID is the album's UID in this database; Slug and Title are what identify it
	// to a human and what a rebuild should match on, since UIDs are regenerated.
	UID   string `yaml:"uid,omitempty"`
	Slug  string `yaml:"slug,omitempty"`
	Title string `yaml:"title"`
	// Type distinguishes a hand-curated album from a generated one (folder, moment,
	// month, state). A rebuild may reasonably skip regenerable types.
	Type string `yaml:"type,omitempty"`
}

// Label is one label attachment, with the provenance needed to replay it.
type Label struct {
	UID  string `yaml:"uid,omitempty"`
	Slug string `yaml:"slug,omitempty"`
	Name string `yaml:"name"`
	// Priority orders the label in the UI.
	Priority int `yaml:"priority,omitempty"`
	// Source is who attached it: manual, ai or import. It matters on rebuild: a
	// hand-attached label is a fact, an AI one is a suggestion.
	Source string `yaml:"source,omitempty"`
	// Uncertainty is the classifier's uncertainty as an integer percentage, where
	// 0 means certain. It is recorded as uncertainty, not confidence, because that
	// is what is stored — inverting it here would invent precision. A manual
	// attachment is always 0.
	Uncertainty int `yaml:"uncertainty,omitempty"`
}

// Person is one marker: a region of the photo and, when known, who is in it.
type Person struct {
	// MarkerUID and SubjectUID are this database's identifiers; Name is what
	// survives a rebuild.
	MarkerUID  string `yaml:"marker_uid,omitempty"`
	SubjectUID string `yaml:"subject_uid,omitempty"`
	// Name is the subject's name, empty for a detected face nobody has named.
	Name string `yaml:"name,omitempty"`
	// SubjectType is person, pet or other.
	SubjectType string `yaml:"subject_type,omitempty"`
	// Type is the marker kind: face or label.
	Type string `yaml:"type,omitempty"`
	// Box is the region, in 0..1 display space.
	Box Box `yaml:"box"`
	// Score is the detector's or matcher's confidence, 0..100.
	Score int `yaml:"score,omitempty"`
	// Invalid marks a marker the user rejected, Reviewed one they have looked at.
	// Both are recorded because both are decisions a human made: dropping Invalid
	// on rebuild would resurrect every face the user already said no to.
	Invalid  bool `yaml:"invalid,omitempty"`
	Reviewed bool `yaml:"reviewed,omitempty"`
}

// Box is a normalised region of the photo, in 0..1 display space (EXIF-aware —
// relative to the upright image the user sees, not the raw stored pixels).
type Box struct {
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
	W float64 `yaml:"w"`
	H float64 `yaml:"h"`
}

// Favorite records one user having favorited the photo.
type Favorite struct {
	// User is the username — the half that survives a rebuild. UserUID is this
	// database's identifier.
	User    string     `yaml:"user"`
	UserUID string     `yaml:"user_uid,omitempty"`
	AddedAt *time.Time `yaml:"added_at,omitempty"`
}

// Rating records one user's star rating and flag on the photo.
type Rating struct {
	User    string `yaml:"user"`
	UserUID string `yaml:"user_uid,omitempty"`
	// Stars is 0 (unrated) to 5.
	Stars int `yaml:"stars,omitempty"`
	// Flag is the personal marker: none, pick or reject.
	Flag      string     `yaml:"flag,omitempty"`
	UpdatedAt *time.Time `yaml:"updated_at,omitempty"`
}

// Stack records the photo's membership of a stack — the group of files that are
// one shot (RAW+JPEG, an exported edit, a live photo's still and clip).
type Stack struct {
	// UID is shared by every member of the stack.
	UID string `yaml:"uid"`
	// Primary marks the one member shown in grids and counts.
	Primary bool `yaml:"primary,omitempty"`
}

// Edit is the non-destructive edit applied to the photo on display. The original
// bytes are never touched, so an edit that is lost is a visible change silently
// reverted — which is why it is recorded here.
type Edit struct {
	// Crop is the crop rectangle in 0..1 space, nil when the photo is uncropped.
	// It is all-or-nothing: either all four values are present or none are.
	Crop *Box `yaml:"crop,omitempty"`
	// Rotation is 0, 90, 180 or 270 degrees.
	Rotation int `yaml:"rotation,omitempty"`
	// Brightness and Contrast are CSS-filter-style adjustments, meaningful in
	// [-1, 1], where 0 is neutral.
	Brightness float64 `yaml:"brightness,omitempty"`
	Contrast   float64 `yaml:"contrast,omitempty"`
}

// albumsFrom converts organize albums into their sidecar form.
func albumsFrom(in []organize.Album) []Album {
	if len(in) == 0 {
		return nil
	}
	out := make([]Album, 0, len(in))
	for _, a := range in {
		out = append(out, Album{UID: a.UID, Slug: a.Slug, Title: a.Title, Type: string(a.Type)})
	}
	return out
}

// labelsFrom converts organize photo-labels into their sidecar form, preserving
// the attachment's source and uncertainty.
func labelsFrom(in []organize.PhotoLabel) []Label {
	if len(in) == 0 {
		return nil
	}
	out := make([]Label, 0, len(in))
	for _, l := range in {
		out = append(out, Label{
			UID:         l.UID,
			Slug:        l.Slug,
			Name:        l.Name,
			Priority:    l.Priority,
			Source:      string(l.Source),
			Uncertainty: l.Uncertainty,
		})
	}
	return out
}

// peopleFrom converts markers into their sidecar form. Every marker is kept,
// including the unassigned and the invalid ones: an unnamed face is work in
// progress and a rejected one is a decision, and both are lost if only the named
// ones are written.
func peopleFrom(in []people.MarkerSubject) []Person {
	if len(in) == 0 {
		return nil
	}
	out := make([]Person, 0, len(in))
	for _, m := range in {
		person := Person{
			MarkerUID:   m.UID,
			Name:        m.SubjectName,
			SubjectType: string(m.SubjectType),
			Type:        string(m.Type),
			Box:         Box{X: m.X, Y: m.Y, W: m.W, H: m.H},
			Score:       m.Score,
			Invalid:     m.Invalid,
			Reviewed:    m.Reviewed,
		}
		if m.SubjectUID != nil {
			person.SubjectUID = *m.SubjectUID
		}
		out = append(out, person)
	}
	return out
}

// favoritesFrom converts per-user favorites into their sidecar form.
func favoritesFrom(in []organize.UserFavorite) []Favorite {
	if len(in) == 0 {
		return nil
	}
	out := make([]Favorite, 0, len(in))
	for _, f := range in {
		addedAt := f.AddedAt
		out = append(out, Favorite{User: f.Username, UserUID: f.UserUID, AddedAt: &addedAt})
	}
	return out
}

// ratingsFrom converts per-user ratings into their sidecar form.
func ratingsFrom(in []organize.UserRating) []Rating {
	if len(in) == 0 {
		return nil
	}
	out := make([]Rating, 0, len(in))
	for _, r := range in {
		updatedAt := r.UpdatedAt
		out = append(out, Rating{
			User:      r.Username,
			UserUID:   r.UserUID,
			Stars:     r.Rating,
			Flag:      string(r.Flag),
			UpdatedAt: &updatedAt,
		})
	}
	return out
}
