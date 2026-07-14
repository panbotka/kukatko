package ppimport

import (
	"encoding/json"
	"testing"

	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
)

// detailJSON is a photo detail as PhotoPrism serves it on GET /api/v1/photos/{uid}:
// the Details block and the per-file Codec/ColorProfile/Projection that the photo
// LISTING carries nowhere. The fixture is decoded rather than hand-built so the
// struct tags are pinned too — a misspelt json key would map a whole column to "".
const detailJSON = `{
	"UID": "pqbemz8276mhtobb",
	"Type": "image",
	"Title": "Ostatky",
	"Caption": "Masopust v Ostrovačicích",
	"Scan": true,
	"CameraSerial": "BX-40023199",
	"OriginalName": "2016/02/IMG_4821",
	"Details": {
		"Keywords": "masopust, maska ,  masopust,  ",
		"Notes": "Nalezeno v krabici po babičce.",
		"Subject": "Masopustní průvod",
		"Artist": "Jan Novák",
		"Copyright": "© 2016 Jan Novák",
		"License": "CC BY-NC 4.0",
		"Software": "Adobe Photoshop Lightroom"
	},
	"Files": [
		{
			"UID": "fqbemz8276mhtobc",
			"Hash": "1a2b3c",
			"Primary": true,
			"Mime": "image/jpeg",
			"Codec": "JPEG",
			"ColorProfile": "Display P3",
			"Projection": "equirectangular"
		}
	]
}`

// decodeDetail decodes a PhotoPrism photo-detail payload, failing the test on a
// malformed fixture.
func decodeDetail(t *testing.T, payload string) photoprism.PhotoDetail {
	t.Helper()
	var detail photoprism.PhotoDetail
	if err := json.Unmarshal([]byte(payload), &detail); err != nil {
		t.Fatalf("decoding detail fixture: %v", err)
	}
	return detail
}

// TestImportMetadata_fromDetail verifies every field the detail carries lands in the
// import patch, that the keywords are re-rendered in the form Kukátko's own
// extraction stores (trimmed, de-duplicated, comma-joined) and that the codec is
// normalised onto the same token vocabulary ("JPEG" -> "jpeg"), not copied verbatim.
func TestImportMetadata_fromDetail(t *testing.T) {
	t.Parallel()
	detail := decodeDetail(t, detailJSON)

	got := importMetadata(detail)
	want := photos.ImportMetadata{
		Subject:      "Masopustní průvod",
		Keywords:     "masopust,maska",
		Artist:       "Jan Novák",
		Copyright:    "© 2016 Jan Novák",
		License:      "CC BY-NC 4.0",
		Notes:        "Nalezeno v krabici po babičce.",
		Software:     "Adobe Photoshop Lightroom",
		Scan:         true,
		CameraSerial: "BX-40023199",
		ColorProfile: "Display P3",
		ImageCodec:   "jpeg",
		Projection:   "equirectangular",
		OriginalName: "2016/02/IMG_4821",
	}
	if got != want {
		t.Errorf("importMetadata =\n%+v\nwant\n%+v", got, want)
	}
	if detail.Caption != "Masopust v Ostrovačicích" {
		t.Errorf("caption = %q, want the Caption field (Description is PhotoPrism's dead column)", detail.Caption)
	}
}

// TestImportMetadata_noDetailsBlock verifies a photo indexed by an older PhotoPrism,
// which has no photo_details row at all and answers a null Details block, yields an
// inert patch. Every column stays empty — the import must map nothing rather than
// write eleven empty strings over a photo the user may have curated.
func TestImportMetadata_noDetailsBlock(t *testing.T) {
	t.Parallel()

	for _, payload := range []string{`{"UID":"pp1","Details":null}`, `{"UID":"pp1"}`} {
		if got := importMetadata(decodeDetail(t, payload)); got != (photos.ImportMetadata{}) {
			t.Errorf("importMetadata(%s) = %+v, want the zero (inert) patch", payload, got)
		}
	}
}

// TestImportMetadata_videoKeepsImageCodecEmpty verifies a video's codec never leaks
// into image_codec. That column is the STILL's compression; a clip's "avc1" belongs
// in video_codec, which ffprobe owns and this import must not touch.
func TestImportMetadata_videoKeepsImageCodecEmpty(t *testing.T) {
	t.Parallel()
	detail := photoprism.PhotoDetail{
		Photo: photoprism.Photo{
			UID:  "pp1",
			Type: "video",
			Files: []photoprism.File{{
				Primary: true, Video: true, Mime: "video/mp4",
				Codec: "avc1", ColorProfile: "sRGB",
			}},
		},
	}

	got := importMetadata(detail)
	if got.ImageCodec != "" {
		t.Errorf("image_codec = %q, want empty: avc1 is a video codec", got.ImageCodec)
	}
	if got.ColorProfile != "sRGB" {
		t.Errorf("color_profile = %q, want sRGB: it describes the file whatever the file is", got.ColorProfile)
	}
}

// TestImportMetadata_noPrimaryFile verifies a detail with no primary file still maps
// its photo-level fields; only the per-file ones stay empty.
func TestImportMetadata_noPrimaryFile(t *testing.T) {
	t.Parallel()
	detail := photoprism.PhotoDetail{
		Photo:   photoprism.Photo{UID: "pp1", Scan: true},
		Details: photoprism.Details{Artist: "  Jan Novák  "},
	}

	got := importMetadata(detail)
	if got.Artist != "Jan Novák" || !got.Scan {
		t.Errorf("photo-level fields = %+v, want the trimmed artist and scan", got)
	}
	if got.ImageCodec != "" || got.ColorProfile != "" || got.Projection != "" {
		t.Errorf("per-file fields = %+v, want empty: there is no primary file", got)
	}
}

// TestCaption verifies the caption is read from PhotoPrism's live field. photo_description
// was renamed to photo_caption and its Go field marked gorm:"-", so a current instance
// always answers Description="" — reading it alone drops every caption in the library.
func TestCaption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pp   photoprism.Photo
		want string
	}{
		{name: "current photoprism", pp: photoprism.Photo{Caption: "Masopust"}, want: "Masopust"},
		{name: "old photoprism", pp: photoprism.Photo{Description: "Masopust"}, want: "Masopust"},
		{
			name: "caption wins",
			pp:   photoprism.Photo{Caption: "Masopust", Description: "stale"},
			want: "Masopust",
		},
		{name: "neither", pp: photoprism.Photo{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := caption(tt.pp); got != tt.want {
				t.Errorf("caption = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestMetadataUpdate_emptyNeverClobbers pins the import's precedence rule on the two
// fields the listing pass owns: PhotoPrism wins when it has a value, but an empty
// PhotoPrism value must never erase a non-empty Kukátko one. Store.UpdateMetadata
// overwrites the whole row, so a title the source no longer carries would otherwise
// be destroyed on the next incremental run.
func TestMetadataUpdate_emptyNeverClobbers(t *testing.T) {
	t.Parallel()
	existing := photos.Photo{Title: "Ostatky", Description: "Masopust v Ostrovačicích"}

	kept := metadataUpdate(existing, photoprism.Photo{})
	if kept.Title != existing.Title || kept.Description != existing.Description {
		t.Errorf("update = %q / %q, want the existing values kept", kept.Title, kept.Description)
	}

	won := metadataUpdate(existing, photoprism.Photo{Title: "Ostatky 2016", Caption: "Průvod"})
	if won.Title != "Ostatky 2016" || won.Description != "Průvod" {
		t.Errorf("update = %q / %q, want PhotoPrism's values", won.Title, won.Description)
	}
}
