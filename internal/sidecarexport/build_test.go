package sidecarexport

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
)

// fixedNow is the pinned generation time used by the build tests.
var fixedNow = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

// TestBuild_identity carries the photo's identifiers and the bytes it describes.
func TestBuild_identity(t *testing.T) {
	t.Parallel()

	doc := Build(Input{
		Photo: photos.Photo{
			UID: "pht1", FileHash: "deadbeef", FileName: "a.jpg",
			FilePath: "2024/05/a.jpg", OriginalName: "DSC_1.JPG", MediaType: photos.MediaImage,
		},
		UploadedBy: "pan.botka",
		Now:        fixedNow,
	})

	if doc.Version != Version {
		t.Errorf("Version = %d, want %d", doc.Version, Version)
	}
	if !doc.GeneratedAt.Equal(fixedNow) {
		t.Errorf("GeneratedAt = %v, want %v", doc.GeneratedAt, fixedNow)
	}
	want := Identity{
		UID: "pht1", SHA256: "deadbeef", FileName: "a.jpg",
		FilePath: "2024/05/a.jpg", OriginalName: "DSC_1.JPG",
		MediaType: "image", UploadedBy: "pan.botka",
	}
	if doc.Identity != want {
		t.Errorf("Identity = %+v, want %+v", doc.Identity, want)
	}
}

// TestBuild_externalOmittedWhenNotImported asserts a photo that came from nowhere
// carries no external block, rather than a block of empty strings.
func TestBuild_externalOmittedWhenNotImported(t *testing.T) {
	t.Parallel()

	doc := Build(Input{Photo: photos.Photo{UID: "pht1"}, Now: fixedNow})
	if doc.Identity.External != nil {
		t.Errorf("External = %+v, want nil for a photo that was not imported", doc.Identity.External)
	}
}

// TestBuild_externalPresentWhenImported carries the source system's identifiers
// so a re-import recognises what it already has.
func TestBuild_externalPresentWhenImported(t *testing.T) {
	t.Parallel()

	uid, hash := "ppuid", "pphash"
	doc := Build(Input{
		Photo: photos.Photo{UID: "pht1", PhotoprismUID: &uid, PhotoprismFileHash: &hash},
		Now:   fixedNow,
	})
	if doc.Identity.External == nil {
		t.Fatal("External is nil, want the PhotoPrism identifiers")
	}
	if doc.Identity.External.PhotoprismUID != "ppuid" {
		t.Errorf("PhotoprismUID = %q, want ppuid", doc.Identity.External.PhotoprismUID)
	}
}

// TestBuild_spatial carries the coordinates, the source and the cached place.
func TestBuild_spatial(t *testing.T) {
	t.Parallel()

	geocoded := time.Date(2024, 5, 18, 9, 0, 0, 0, time.UTC)
	doc := Build(Input{
		Photo: photos.Photo{UID: "pht1", Lat: new(50.1), Lng: new(14.4), LocationSource: "estimate"},
		Place: &places.Place{Country: "Česko", City: "Praha", GeocodedAt: geocoded},
		Now:   fixedNow,
	})

	if doc.Spatial == nil {
		t.Fatal("Spatial is nil, want the location")
	}
	if doc.Spatial.Source != "estimate" {
		t.Errorf("Source = %q, want estimate", doc.Spatial.Source)
	}
	if doc.Spatial.Place == nil || doc.Spatial.Place.City != "Praha" {
		t.Errorf("Place = %+v, want the cached place", doc.Spatial.Place)
	}
}

// TestBuild_spatialOmittedWhenNothingKnown asserts a photo with no location and
// no place carries no spatial block at all.
func TestBuild_spatialOmittedWhenNothingKnown(t *testing.T) {
	t.Parallel()

	if doc := Build(Input{Photo: photos.Photo{UID: "pht1"}, Now: fixedNow}); doc.Spatial != nil {
		t.Errorf("Spatial = %+v, want nil", doc.Spatial)
	}
}

// TestBuild_spatialKeptForManualTombstone pins a subtle one: "manual" with no
// coordinates means the user deleted the location on purpose. Dropping the block
// would lose that decision, and a rebuild would let the estimator hand the
// location straight back.
func TestBuild_spatialKeptForManualTombstone(t *testing.T) {
	t.Parallel()

	doc := Build(Input{Photo: photos.Photo{UID: "pht1", LocationSource: "manual"}, Now: fixedNow})
	if doc.Spatial == nil {
		t.Fatal("Spatial is nil, want the manual tombstone to survive")
	}
	if doc.Spatial.Source != "manual" || doc.Spatial.Lat != nil {
		t.Errorf("Spatial = %+v, want source manual with no coordinates", doc.Spatial)
	}
}

// TestBuild_videoOmittedForStillImage asserts a photo carries no video block.
func TestBuild_videoOmittedForStillImage(t *testing.T) {
	t.Parallel()

	doc := Build(Input{Photo: photos.Photo{UID: "pht1", MediaType: photos.MediaImage}, Now: fixedNow})
	if doc.Technical.Video != nil {
		t.Errorf("Video = %+v, want nil for a still image", doc.Technical.Video)
	}
}

// TestBuild_videoPresentForVideo carries the clip detail.
func TestBuild_videoPresentForVideo(t *testing.T) {
	t.Parallel()

	doc := Build(Input{
		Photo: photos.Photo{UID: "pht1", MediaType: photos.MediaVideo, VideoCodec: "h264", DurationMs: new(1200)},
		Now:   fixedNow,
	})
	if doc.Technical.Video == nil {
		t.Fatal("Video is nil, want the clip detail")
	}
	if doc.Technical.Video.VideoCodec != "h264" {
		t.Errorf("VideoCodec = %q, want h264", doc.Technical.Video.VideoCodec)
	}
}

// TestBuild_curation carries the whole of what the users decided — the group that
// exists nowhere but the database.
func TestBuild_curation(t *testing.T) {
	t.Parallel()

	subjectUID := "sub1"
	doc := Build(Input{
		Photo:  photos.Photo{UID: "pht1", Private: true},
		Albums: []organize.Album{{UID: "alb1", Slug: "svatba", Title: "Svatba", Type: organize.AlbumManual}},
		Labels: []organize.PhotoLabel{{
			Label:  organize.Label{UID: "lbl1", Name: "Portrét", Priority: 3},
			Source: organize.SourceAI, Uncertainty: 20,
		}},
		People: []people.MarkerSubject{{
			Marker: people.Marker{
				UID: "mrk1", SubjectUID: &subjectUID, Type: people.MarkerFace,
				X: 0.1, Y: 0.2, W: 0.3, H: 0.4, Score: 91,
			},
			SubjectName: "Jana", SubjectType: people.SubjectPerson,
		}},
		Favorites: []organize.UserFavorite{{UserUID: "usr1", Username: "pan.botka"}},
		Ratings:   []organize.UserRating{{UserUID: "usr1", Username: "pan.botka", Rating: 5, Flag: "pick"}},
		Now:       fixedNow,
	})

	c := doc.Curation
	if len(c.Albums) != 1 || c.Albums[0].Title != "Svatba" {
		t.Errorf("Albums = %+v, want the one album", c.Albums)
	}
	if len(c.Labels) != 1 || c.Labels[0].Source != "ai" || c.Labels[0].Uncertainty != 20 {
		t.Errorf("Labels = %+v, want the label with its provenance", c.Labels)
	}
	if len(c.People) != 1 {
		t.Fatalf("People = %+v, want the one marker", c.People)
	}
	person := c.People[0]
	if person.Name != "Jana" || person.SubjectUID != "sub1" {
		t.Errorf("Person = %+v, want the named subject", person)
	}
	if want := (Box{X: 0.1, Y: 0.2, W: 0.3, H: 0.4}); person.Box != want {
		t.Errorf("Box = %+v, want %+v — a marker without its box cannot be rebuilt", person.Box, want)
	}
	if len(c.Favorites) != 1 || c.Favorites[0].User != "pan.botka" {
		t.Errorf("Favorites = %+v, want the one favorite", c.Favorites)
	}
	if len(c.Ratings) != 1 || c.Ratings[0].Stars != 5 {
		t.Errorf("Ratings = %+v, want the one rating", c.Ratings)
	}
	if !c.Private {
		t.Error("Private = false, want true")
	}
}

// TestBuild_keepsUnnamedAndInvalidMarkers asserts the export keeps a face nobody
// has named and one the user rejected. The first is work in progress and the
// second is a decision; writing only the named ones loses both, and a rebuild
// would resurrect every face the user already said no to.
func TestBuild_keepsUnnamedAndInvalidMarkers(t *testing.T) {
	t.Parallel()

	doc := Build(Input{
		Photo: photos.Photo{UID: "pht1"},
		People: []people.MarkerSubject{
			{Marker: people.Marker{UID: "mrk1", Type: people.MarkerFace, W: 0.1, H: 0.1}},
			{Marker: people.Marker{UID: "mrk2", Type: people.MarkerFace, W: 0.2, H: 0.2, Invalid: true}},
		},
		Now: fixedNow,
	})

	if len(doc.Curation.People) != 2 {
		t.Fatalf("People = %+v, want both the unnamed and the rejected marker", doc.Curation.People)
	}
	if doc.Curation.People[0].Name != "" || doc.Curation.People[0].SubjectUID != "" {
		t.Errorf("unnamed marker = %+v, want no subject", doc.Curation.People[0])
	}
	if !doc.Curation.People[1].Invalid {
		t.Error("rejected marker lost its Invalid flag")
	}
}

// TestBuild_archivedAndStack carries the trash state and the stack membership.
func TestBuild_archivedAndStack(t *testing.T) {
	t.Parallel()

	archived := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	stackUID := "stk1"
	doc := Build(Input{
		Photo: photos.Photo{UID: "pht1", ArchivedAt: &archived, StackUID: &stackUID, StackPrimary: true},
		Now:   fixedNow,
	})
	if doc.Curation.ArchivedAt == nil || !doc.Curation.ArchivedAt.Equal(archived) {
		t.Errorf("ArchivedAt = %v, want %v", doc.Curation.ArchivedAt, archived)
	}
	if doc.Curation.Stack == nil || doc.Curation.Stack.UID != "stk1" || !doc.Curation.Stack.Primary {
		t.Errorf("Stack = %+v, want the primary of stk1", doc.Curation.Stack)
	}
}

// TestBuild_edit carries the non-destructive edit, which is a visible change that
// exists only in the database.
func TestBuild_edit(t *testing.T) {
	t.Parallel()

	doc := Build(Input{
		Photo: photos.Photo{UID: "pht1"},
		Edit: &photos.Edit{
			PhotoUID: "pht1",
			CropX:    new(0.1), CropY: new(0.2), CropW: new(0.6), CropH: new(0.5),
			Rotation: 90, Brightness: 0.2, Contrast: -0.1,
		},
		Now: fixedNow,
	})
	if doc.Edit == nil {
		t.Fatal("Edit is nil, want the edit")
	}
	if doc.Edit.Rotation != 90 || doc.Edit.Brightness != 0.2 || doc.Edit.Contrast != -0.1 {
		t.Errorf("Edit = %+v, want the stored adjustments", doc.Edit)
	}
	if want := (Box{X: 0.1, Y: 0.2, W: 0.6, H: 0.5}); doc.Edit.Crop == nil || *doc.Edit.Crop != want {
		t.Errorf("Crop = %+v, want %+v", doc.Edit.Crop, want)
	}
}

// TestBuild_editOmittedWhenIdentity asserts a no-op edit is not written: it
// changes nothing, so recording it would be noise in every file.
func TestBuild_editOmittedWhenIdentity(t *testing.T) {
	t.Parallel()

	doc := Build(Input{Photo: photos.Photo{UID: "pht1"}, Edit: &photos.Edit{PhotoUID: "pht1"}, Now: fixedNow})
	if doc.Edit != nil {
		t.Errorf("Edit = %+v, want nil for an identity edit", doc.Edit)
	}
}

// TestBuild_editOmittedWhenAbsent asserts a photo with no edit row carries none.
func TestBuild_editOmittedWhenAbsent(t *testing.T) {
	t.Parallel()

	if doc := Build(Input{Photo: photos.Photo{UID: "pht1"}, Now: fixedNow}); doc.Edit != nil {
		t.Errorf("Edit = %+v, want nil", doc.Edit)
	}
}

// TestBuild_temporalEstimate carries the estimated flag and the user's note, so a
// guessed date can never be rebuilt as a fact.
func TestBuild_temporalEstimate(t *testing.T) {
	t.Parallel()

	taken := time.Date(1950, 6, 1, 0, 0, 0, 0, time.UTC)
	doc := Build(Input{
		Photo: photos.Photo{
			UID: "pht1", TakenAt: &taken, TakenAtSource: "manual",
			TakenAtEstimated: true, TakenAtNote: "kolem roku 1950",
		},
		Now: fixedNow,
	})
	if !doc.Temporal.Estimated || doc.Temporal.Note != "kolem roku 1950" {
		t.Errorf("Temporal = %+v, want the estimate and its note", doc.Temporal)
	}
	if doc.Temporal.TakenAt == nil || !doc.Temporal.TakenAt.Equal(taken) {
		t.Errorf("TakenAt = %v, want %v", doc.Temporal.TakenAt, taken)
	}
}

// TestBuild_isPure asserts Build does not depend on anything but its input: the
// same input twice yields the same document, which is what makes the format
// testable and the handler safely repeatable.
func TestBuild_isPure(t *testing.T) {
	t.Parallel()

	in := Input{Photo: photos.Photo{UID: "pht1", Title: "x"}, Now: fixedNow}
	first, err := Marshal(Build(in))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	second, err := Marshal(Build(in))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if string(first) != string(second) {
		t.Error("Build is not deterministic for the same input")
	}
}
