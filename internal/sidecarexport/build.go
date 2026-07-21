package sidecarexport

import (
	"fmt"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoedit"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
)

// Input is everything Build needs to serialise one photo. The caller gathers it;
// this package does no I/O of its own, which is what keeps Build a pure function
// and the format testable without a database.
//
// The optional members are optional in the domain, not merely in this struct: a
// photo need not have a place, an edit, an album or a face.
type Input struct {
	// Photo is the catalogue row. It is the only required member.
	Photo photos.Photo
	// Albums, Labels, People, Favorites and Ratings are the photo's curation, each
	// empty when there is none.
	Albums    []organize.Album
	Labels    []organize.PhotoLabel
	People    []people.MarkerSubject
	Favorites []organize.UserFavorite
	Ratings   []organize.UserRating
	// Place is the cached reverse-geocode result, nil when the photo has never been
	// geocoded.
	Place *places.Place
	// Edit is the non-destructive edit, nil when the photo has none.
	Edit *photos.Edit
	// UploadedBy is the username of the uploader, empty when unknown.
	UploadedBy string
	// Now is the generation timestamp written into the document. A zero value reads
	// the wall clock; tests pin it.
	Now time.Time
}

// Build assembles in into a Document. It is pure: same input, same output, no
// I/O, no clock unless Now is zero.
//
// Empty groups are left empty rather than omitted here — Marshal's omitempty
// handling is what keeps a sparse photo's file short — so the returned Document
// is always a complete picture of the input.
func Build(in Input) Document {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	return Document{
		Version:     Version,
		GeneratedAt: now.UTC(),
		Identity:    identityOf(in),
		Descriptive: descriptiveOf(in.Photo),
		Temporal:    temporalOf(in.Photo),
		Spatial:     spatialOf(in.Photo, in.Place),
		Technical:   technicalOf(in.Photo),
		Curation:    curationOf(in),
		Edit:        editOf(in.Edit),
	}
}

// identityOf builds the identity group: what the file is about and which bytes it
// describes.
func identityOf(in Input) Identity {
	p := in.Photo
	id := Identity{
		UID:          p.UID,
		SHA256:       p.FileHash,
		FileName:     p.FileName,
		FilePath:     p.FilePath,
		OriginalName: p.OriginalName,
		MediaType:    string(p.MediaType),
		UploadedBy:   in.UploadedBy,
	}
	if ext := externalOf(p); ext != nil {
		id.External = ext
	}
	return id
}

// externalOf builds the external-identifier group, returning nil when the photo
// was not imported from anywhere.
func externalOf(p photos.Photo) *External {
	ext := External{}
	if p.PhotoprismUID != nil {
		ext.PhotoprismUID = *p.PhotoprismUID
	}
	if p.PhotoprismFileHash != nil {
		ext.PhotoprismFileHash = *p.PhotoprismFileHash
	}
	if p.PhotosorterUID != nil {
		ext.PhotosorterUID = *p.PhotosorterUID
	}
	if ext == (External{}) {
		return nil
	}
	return &ext
}

// descriptiveOf builds the descriptive group: the text a human wrote plus the
// IPTC credit fields.
func descriptiveOf(p photos.Photo) Descriptive {
	return Descriptive{
		Title:       p.Title,
		TitleEdited: p.TitleEdited,
		Description: p.Description,
		Notes:       p.Notes,
		AiNote:      p.AiNote,
		Subject:     p.Subject,
		Keywords:    p.Keywords,
		Artist:      p.Artist,
		Copyright:   p.Copyright,
		License:     p.License,
	}
}

// temporalOf builds the temporal group.
func temporalOf(p photos.Photo) Temporal {
	t := Temporal{
		TakenAtSource: p.TakenAtSource,
		Estimated:     p.TakenAtEstimated,
		Note:          p.TakenAtNote,
	}
	if p.TakenAt != nil {
		utc := p.TakenAt.UTC()
		t.TakenAt = &utc
	}
	return t
}

// spatialOf builds the spatial group, returning nil when the photo has no
// coordinates, no source and no cached place — there is nothing spatial to say.
func spatialOf(p photos.Photo, place *places.Place) *Spatial {
	s := Spatial{
		Lat:      p.Lat,
		Lng:      p.Lng,
		Altitude: p.Altitude,
		Source:   p.LocationSource,
		Place:    placeOf(place),
	}
	if s.Lat == nil && s.Lng == nil && s.Altitude == nil && s.Source == "" && s.Place == nil {
		return nil
	}
	return &s
}

// placeOf converts a cached reverse-geocode result, returning nil when there is
// none or when it is empty (a geocode that legitimately found nothing).
func placeOf(place *places.Place) *Place {
	if place == nil {
		return nil
	}
	out := Place{
		Country: place.Country,
		Region:  place.Region,
		City:    place.City,
		Name:    place.PlaceName,
	}
	if out == (Place{}) {
		return nil
	}
	if !place.GeocodedAt.IsZero() {
		at := place.GeocodedAt.UTC()
		out.GeocodedAt = &at
	}
	return &out
}

// technicalOf builds the technical group: the camera and the file.
func technicalOf(p photos.Photo) Technical {
	return Technical{
		CameraMake:   p.CameraMake,
		CameraModel:  p.CameraModel,
		CameraSerial: p.CameraSerial,
		LensModel:    p.LensModel,
		ISO:          p.ISO,
		Aperture:     p.Aperture,
		Exposure:     p.Exposure,
		FocalLength:  p.FocalLength,
		Width:        p.FileWidth,
		Height:       p.FileHeight,
		Orientation:  p.FileOrientation,
		FileSize:     p.FileSize,
		FileMIME:     p.FileMime,
		Software:     p.Software,
		Scan:         p.Scan,
		ColorProfile: p.ColorProfile,
		ImageCodec:   p.ImageCodec,
		Projection:   p.Projection,
		Video:        videoOf(p),
	}
}

// videoOf builds the video sub-group, returning nil for a still image (one with
// no clip detail to record).
func videoOf(p photos.Photo) *Video {
	v := Video{
		DurationMs: p.DurationMs,
		VideoCodec: p.VideoCodec,
		AudioCodec: p.AudioCodec,
		HasAudio:   p.HasAudio,
		FPS:        p.FPS,
	}
	if v.DurationMs == nil && v.VideoCodec == "" && v.AudioCodec == "" && !v.HasAudio && v.FPS == nil {
		return nil
	}
	return &v
}

// curationOf builds the curation group — the part that exists nowhere else.
func curationOf(in Input) Curation {
	p := in.Photo
	c := Curation{
		Albums:    albumsFrom(in.Albums),
		Labels:    labelsFrom(in.Labels),
		People:    peopleFrom(in.People),
		Favorites: favoritesFrom(in.Favorites),
		Ratings:   ratingsFrom(in.Ratings),
		Private:   p.Private,
	}
	if p.ArchivedAt != nil {
		at := p.ArchivedAt.UTC()
		c.ArchivedAt = &at
	}
	if p.StackUID != nil {
		c.Stack = &Stack{UID: *p.StackUID, Primary: p.StackPrimary}
	}
	return c
}

// editOf converts a non-destructive edit, returning nil when there is none or
// when the edit is the identity (nothing is changed, so nothing is worth
// recording).
func editOf(edit *photos.Edit) *Edit {
	if edit == nil || photoedit.IsIdentity(*edit) {
		return nil
	}
	out := Edit{
		Rotation:   edit.Rotation,
		Brightness: edit.Brightness,
		Contrast:   edit.Contrast,
	}
	if edit.CropX != nil && edit.CropY != nil && edit.CropW != nil && edit.CropH != nil {
		out.Crop = &Box{X: *edit.CropX, Y: *edit.CropY, W: *edit.CropW, H: *edit.CropH}
	}
	return &out
}

// header is prepended to every sidecar. It is addressed to whoever opens the file
// cold — quite possibly on the worst day of their sysadmin life — and it states
// the one thing about this format that looks like an omission and is not.
const header = `# Kukátko metadata sidecar.
#
# This file holds one photo's metadata and curation: what it is, when and where it
# was taken, who is in it, which albums and labels it carries, how it was rated.
# It sits next to the originals in storage so this library can be rebuilt from the
# storage alone — originals plus sidecars, no database. It is generated: Kukátko
# rewrites it whenever the photo changes, so edits made here are overwritten.
#
# NOT in this file, deliberately, and please do not "fix" it: the image embedding
# and the face vectors. They are large, they are binary, they would dwarf
# everything above and make the file unreadable — and they are cheap to recompute
# from the original, which is exactly what the embedding and face backfill jobs
# are for. What cannot be recomputed is what a person decided, and that is all
# here.
#
# Format: docs/RESTORE.md. Schema version is the version key below; a reader that
# does not know a version should refuse the file rather than guess.
`

// Marshal renders doc as the bytes of a sidecar file: the explanatory header
// followed by the YAML document.
//
// The header is a YAML comment, so a parser ignores it and Unmarshal round-trips
// the result — the header is for the human who finds the file, not the machine.
func Marshal(doc Document) ([]byte, error) {
	body, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("sidecarexport: marshaling document: %w", err)
	}
	return append([]byte(header), body...), nil
}

// Unmarshal parses the bytes of a sidecar file back into a Document, ignoring the
// header comment.
//
// It exists to prove the format is sufficient — the round-trip test is what pins
// that, and it is what a future kukatko restore --from-sidecars is built against.
// This package does not otherwise read sidecars.
func Unmarshal(data []byte) (Document, error) {
	var doc Document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Document{}, fmt.Errorf("sidecarexport: unmarshaling document: %w", err)
	}
	return doc, nil
}
