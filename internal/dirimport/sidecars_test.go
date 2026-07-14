package dirimport

import (
	"net/http"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/photos"
)

// takeoutJSON is a Takeout sidecar carrying everything the import reads: the
// capture time the exported JPEG no longer has, the caption, a GPS fix, the star
// and the people Google recognised.
const takeoutJSON = `{
	"title": "a.jpg",
	"description": "Sunset over Lipno",
	"photoTakenTime": {"timestamp": "1465236142"},
	"geoData": {"latitude": 48.6417, "longitude": 14.0453, "altitude": 726.0},
	"people": [{"name": "Jan Novák"}],
	"favorited": true
}`

// TestImportReadsSidecars is the point of the whole feature: the folder is a
// Google Photos export, so the metadata lives beside the media and the import has
// to carry it over — otherwise every photo lands with no date and no caption.
func TestImportReadsSidecars(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	writeFile(t, root, "a.jpg.supplemental-metadata.json", takeoutJSON)
	env := newEnv(t, nil)

	result, err := env.svc.Import(t.Context(), Options{Root: root, UploadedBy: "user-1"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	sc := env.ingester.sidecarFor("a.jpg")
	if sc == nil {
		t.Fatal("the ingest pipeline was handed no sidecar for a.jpg")
	}
	want := time.Unix(1465236142, 0).UTC()
	if sc.TakenAt == nil || !sc.TakenAt.Equal(want) {
		t.Errorf("sidecar TakenAt = %v, want %v", sc.TakenAt, want)
	}
	if sc.Description != "Sunset over Lipno" {
		t.Errorf("sidecar Description = %q", sc.Description)
	}
	if sc.Lat == nil || *sc.Lat != 48.6417 {
		t.Errorf("sidecar Lat = %v, want 48.6417", sc.Lat)
	}
	if result.Sidecars.Matched != 1 || result.Sidecars.Applied != 1 {
		t.Errorf("Sidecars = %+v, want one matched and applied", result.Sidecars)
	}
	// The photo's favourite is the importing user's, not the photo's: favourites
	// are per-user in Kukátko.
	if got := env.curation.favoritesOf("user-1"); !slices.Equal(got, []string{"uid-a.jpg"}) {
		t.Errorf("favourites of user-1 = %v, want the imported photo", got)
	}
}

// TestImportCreatesNoAlbumsFromTheExport: Takeout's album files are full of
// auto-generated junk from the phone, and the user does not want them. The photos
// come in; the albums do not.
func TestImportCreatesNoAlbumsFromTheExport(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "Takeout/Photos from 2016/a.jpg", "aaa")
	writeFile(t, root, "Takeout/Photos from 2016/a.jpg.json", takeoutJSON)
	writeFile(t, root, "Takeout/Photos from 2016/metadata.json",
		`{"title":"Photos from 2016","access":"protected"}`)
	env := newEnv(t, nil)

	result, err := env.svc.Import(t.Context(), Options{Root: root, Recursive: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if env.organizer.createdAlbums != 0 {
		t.Errorf("created %d albums from the export, want none", env.organizer.createdAlbums)
	}
	// The album's own metadata.json is not a media sidecar, so it is not reported
	// as one that matched nothing either.
	if len(result.Sidecars.Orphans) != 0 {
		t.Errorf("Orphans = %v, want none: metadata.json is an album file, not a sidecar",
			result.Sidecars.Orphans)
	}
	if result.Sidecars.Matched != 1 {
		t.Errorf("Sidecars.Matched = %d, want 1", result.Sidecars.Matched)
	}
}

// TestImportReportsUnpairedSidecars: a sidecar that matched no photo, and a photo
// that got no sidecar, are both named. A silent mismatch is how somebody loses a
// decade of dates without ever being told.
func TestImportReportsUnpairedSidecars(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	writeFile(t, root, "a.jpg.json", takeoutJSON)
	writeFile(t, root, "b.jpg", "bbb")
	writeFile(t, root, "gone.jpg.json", takeoutJSON)
	env := newEnv(t, nil)

	result, err := env.svc.Import(t.Context(), Options{Root: root})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if !slices.Equal(result.Sidecars.Orphans, []string{"gone.jpg.json"}) {
		t.Errorf("Orphans = %v, want the sidecar of the photo that was not exported", result.Sidecars.Orphans)
	}
	if !slices.Equal(result.Sidecars.Missing, []string{"b.jpg"}) {
		t.Errorf("Missing = %v, want the photo with no sidecar", result.Sidecars.Missing)
	}
}

// TestImportReportsUnreadableSidecar: a corrupt sidecar costs the photo its date,
// not its place in the library. The photo is imported and the sidecar is named.
func TestImportReportsUnreadableSidecar(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	writeFile(t, root, "a.jpg.json", `{"photoTakenTime":`)
	env := newEnv(t, nil)

	result, err := env.svc.Import(t.Context(), Options{Root: root})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if result.Counts.Imported != 1 {
		t.Errorf("Imported = %d, want the photo imported anyway", result.Counts.Imported)
	}
	if !slices.Equal(result.Sidecars.Unreadable, []string{"a.jpg.json"}) {
		t.Errorf("Unreadable = %v, want the corrupt sidecar", result.Sidecars.Unreadable)
	}
	if result.Sidecars.Applied != 0 {
		t.Errorf("Applied = %d, want 0", result.Sidecars.Applied)
	}
	if sc := env.ingester.sidecarFor("a.jpg"); sc != nil {
		t.Errorf("the pipeline was handed metadata from a corrupt sidecar: %+v", sc)
	}
}

// TestImportNoSidecars gives the operator the escape hatch: --no-sidecars imports
// the pixels and nothing beside them.
func TestImportNoSidecars(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	writeFile(t, root, "a.jpg.json", takeoutJSON)
	env := newEnv(t, nil)

	result, err := env.svc.Import(t.Context(), Options{Root: root, NoSidecars: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if sc := env.ingester.sidecarFor("a.jpg"); sc != nil {
		t.Errorf("--no-sidecars still read a sidecar: %+v", sc)
	}
	if result.Sidecars.Matched != 0 || len(result.Sidecars.Missing) != 0 {
		t.Errorf("Sidecars = %+v, want an empty report", result.Sidecars)
	}
}

// TestImportFillsDuplicateGapsFromSidecar covers the folder that was already
// imported once, before its sidecars were read: the files come back as duplicates,
// nothing is created, and the dates they never got are written anyway.
func TestImportFillsDuplicateGapsFromSidecar(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	writeFile(t, root, "a.jpg.json", takeoutJSON)
	env := newEnv(t, map[string]ingest.FileResult{
		"a.jpg": {
			Filename: "a.jpg",
			Status:   http.StatusConflict,
			Outcome:  ingest.OutcomeDuplicate,
			PhotoUID: "photo-1",
		},
	})

	result, err := env.svc.Import(t.Context(), Options{Root: root, UploadedBy: "user-1"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if result.Counts.Duplicates != 1 {
		t.Errorf("Duplicates = %d, want 1", result.Counts.Duplicates)
	}
	fill, ok := env.filler.fillFor("photo-1")
	if !ok {
		t.Fatal("the duplicate's metadata gaps were not filled from its sidecar")
	}
	want := time.Unix(1465236142, 0).UTC()
	if fill.TakenAt == nil || !fill.TakenAt.Equal(want) {
		t.Errorf("fill.TakenAt = %v, want %v", fill.TakenAt, want)
	}
	if fill.TakenAtSource != string(exif.SourceSidecar) {
		t.Errorf("fill.TakenAtSource = %q, want %q", fill.TakenAtSource, exif.SourceSidecar)
	}
	if fill.Description != "Sunset over Lipno" {
		t.Errorf("fill.Description = %q", fill.Description)
	}
	// A photo already in the library keeps the user's own marks: re-importing an
	// old export must not re-favourite what the user has since un-favourited.
	if got := env.curation.favoritesOf("user-1"); len(got) != 0 {
		t.Errorf("favourites of user-1 = %v, want none for a duplicate", got)
	}
}

// TestDryRunReportsSidecars: a dry run writes nothing, and the sidecar report it
// prints is the one the real run would produce — including the sidecar that
// matches nothing, which is worth knowing before the import, not after.
func TestDryRunReportsSidecars(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	writeFile(t, root, "a.jpg.json", takeoutJSON)
	writeFile(t, root, "gone.jpg.json", takeoutJSON)
	env := newEnv(t, nil)

	result, err := env.svc.Import(t.Context(), Options{Root: root, DryRun: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if result.Sidecars.Matched != 1 || result.Sidecars.Applied != 1 {
		t.Errorf("Sidecars = %+v, want one matched and read", result.Sidecars)
	}
	if !slices.Equal(result.Sidecars.Orphans, []string{"gone.jpg.json"}) {
		t.Errorf("Orphans = %v", result.Sidecars.Orphans)
	}
	if len(env.ingester.ingested()) != 0 {
		t.Errorf("a dry run ingested %v, want nothing", env.ingester.ingested())
	}
	if _, filled := env.filler.fillFor("photo-1"); filled {
		t.Error("a dry run wrote metadata")
	}
}

// TestSidecarsNotConfigured keeps the optional collaborators optional: with no
// filler and no curation store, an import still reads sidecars and still imports
// (the metadata simply lands nowhere per-user).
func TestSidecarsNotConfigured(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	writeFile(t, root, "a.jpg.json", takeoutJSON)
	env := newEnv(t, nil)
	svc := New(Config{
		Ingest: env.ingester,
		Runs:   env.runs,
		Photos: &fakePhotos{byHash: map[string]photos.Photo{}, byUID: map[string]photos.Photo{}},
	})

	result, err := svc.Import(t.Context(), Options{Root: root, UploadedBy: "user-1"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 1 || result.Sidecars.Applied != 1 {
		t.Errorf("Counts = %+v, Sidecars = %+v", result.Counts, result.Sidecars)
	}
}

// appleXMP is an Apple-shaped XMP sidecar: the standalone metadata file an Apple
// Photos export writes beside the media, carrying the capture date, the caption
// and the star rating.
const appleXMP = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:exif="http://ns.adobe.com/exif/1.0/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
    exif:DateTimeOriginal="2018-07-14T11:30:00"
    xmp:Rating="4">
   <dc:description>
    <rdf:Alt><rdf:li xml:lang="x-default">Na Petříně</rdf:li></rdf:Alt>
   </dc:description>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// TestImportReadsAppleXMP covers the other export shape: an Apple .xmp beside the
// media, read through exiftool. Its star rating lands on the importing user —
// ratings, like favourites, are per-user in Kukátko.
func TestImportReadsAppleXMP(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool is not installed")
	}

	root := t.TempDir()
	writeFile(t, root, "a.jpg", "aaa")
	writeFile(t, root, "a.jpg.xmp", appleXMP)
	// Apple writes .AAE files for edits; they are not metadata and are never read.
	writeFile(t, root, "a.aae", "<plist/>")
	env := newEnv(t, nil)

	result, err := env.svc.Import(t.Context(), Options{Root: root, UploadedBy: "user-1"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	sc := env.ingester.sidecarFor("a.jpg")
	if sc == nil {
		t.Fatal("the ingest pipeline was handed no sidecar for a.jpg")
	}
	want := time.Date(2018, 7, 14, 11, 30, 0, 0, time.UTC)
	if sc.TakenAt == nil || !sc.TakenAt.Equal(want) {
		t.Errorf("sidecar TakenAt = %v, want %v", sc.TakenAt, want)
	}
	if sc.Description != "Na Petříně" {
		t.Errorf("sidecar Description = %q", sc.Description)
	}
	if got := env.curation.ratingOf("user-1", "uid-a.jpg"); got != 4 {
		t.Errorf("rating of the imported photo = %d, want the XMP's 4 stars", got)
	}
	if result.Sidecars.Matched != 1 || result.Sidecars.Applied != 1 {
		t.Errorf("Sidecars = %+v, want the XMP matched and applied", result.Sidecars)
	}
	if len(result.Sidecars.Orphans) != 0 {
		t.Errorf("Orphans = %v, want none: an .aae is an edit description, not a sidecar",
			result.Sidecars.Orphans)
	}
}
