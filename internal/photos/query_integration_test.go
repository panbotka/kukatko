//go:build integration

package photos_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// queryLibrary is the seeded fixture the query-language tests run against: a
// small varied library plus the name→UID mapping the expectations use.
type queryLibrary struct {
	store *photos.Store
	// uids maps the fixture names below to the created photo UIDs.
	uids map[string]string
	// names maps back from UID to fixture name for readable failures.
	names map[string]string
	// user is the UID of the seeded user owning the ratings and favorites.
	user string
}

// ptrOf returns a pointer to v, keeping the photo fixtures below readable.
func ptrOf[T any](v T) *T {
	return &v
}

// seedQueryLibrary builds the library every filter assertion runs against:
//
//	beach   image  Canon EOS R6, RF lens, ISO 100 f/2.8 50mm, 24MP landscape,
//	               Prague GPS alt 300, taken 2024-05-01, keywords, label cat,
//	               person Alice (face marker) + one unassigned face marker,
//	               album "Léto 2024", rated 5 + pick, place Praha/Czechia
//	prague2 image  Prague GPS ~0.9 km from beach, 6MP panorama, taken
//	               2024-05-03, album "Léto 2024", place Praha/Czechia
//	winter  image  Nikon Z6, ISO 800 f/1.8 35mm, 24MP portrait, private, Brno
//	               GPS alt 240, taken 2023-12-24, notes, label dog, one face
//	               marker + one invalid marker + one unassigned faces row,
//	               favorite of the user, rated 2, place Brno/Czechia
//	clip    video  hevc, 2MP landscape, no GPS, taken 2022-06-15, label
//	               blurry, flagged reject
//	shelf   image  square 4MP, no GPS, taken 2021-03-10
//	attic   image  archived, taken 2020-01-01
func seedQueryLibrary(t *testing.T) queryLibrary {
	t.Helper()
	store, db := newStore(t)
	ctx := t.Context()
	lib := queryLibrary{store: store, uids: map[string]string{}, names: map[string]string{}, user: "u_ql"}

	add := func(name string, p photos.Photo) photos.Photo {
		created := mustCreate(t, store, p)
		lib.uids[name] = created.UID
		lib.names[created.UID] = name
		return created
	}

	beach := add("beach", photos.Photo{
		FileHash: "ql-1", FilePath: "p/1.jpg", FileName: "IMG_0001.jpg", FileMime: "image/jpeg",
		FileWidth: 6000, FileHeight: 4000,
		Title: "Beach sunset", Description: "warm evening", Keywords: "beach,summer",
		TakenAt: ptrOf(time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)), TakenAtSource: "exif",
		Lat: ptrOf(50.08), Lng: ptrOf(14.42), Altitude: ptrOf(300.0),
		CameraMake: "Canon", CameraModel: "Canon EOS R6", LensModel: "RF 50mm F1.8",
		ISO: ptrOf(100), Aperture: ptrOf(2.8), FocalLength: ptrOf(50.0), ImageCodec: "jpeg",
	})
	prague2 := add("prague2", photos.Photo{
		FileHash: "ql-2", FilePath: "p/2.jpg", FileName: "IMG_0002.jpg", FileMime: "image/jpeg",
		FileWidth: 4000, FileHeight: 1500,
		Title:   "Prague sunset streets",
		TakenAt: ptrOf(time.Date(2024, 5, 3, 18, 0, 0, 0, time.UTC)), TakenAtSource: "exif",
		Lat: ptrOf(50.085), Lng: ptrOf(14.43),
	})
	winter := add("winter", photos.Photo{
		FileHash: "ql-3", FilePath: "p/3.jpg", FileName: "DSC_0003.jpg", FileMime: "image/jpeg",
		FileWidth: 4000, FileHeight: 6000,
		Title: "Winter cabin", Notes: "todo edit", Private: true,
		TakenAt: ptrOf(time.Date(2023, 12, 24, 9, 0, 0, 0, time.UTC)), TakenAtSource: "exif",
		Lat: ptrOf(49.19), Lng: ptrOf(16.61), Altitude: ptrOf(240.0),
		CameraMake: "Nikon", CameraModel: "Nikon Z6",
		ISO: ptrOf(800), Aperture: ptrOf(1.8), FocalLength: ptrOf(35.0),
	})
	clip := add("clip", photos.Photo{
		FileHash: "ql-4", FilePath: "p/4.mp4", FileName: "MOV_0004.mp4", FileMime: "video/mp4",
		FileWidth: 1920, FileHeight: 1080, MediaType: photos.MediaVideo, VideoCodec: "hevc",
		Title:   "Birthday clip",
		TakenAt: ptrOf(time.Date(2022, 6, 15, 15, 0, 0, 0, time.UTC)), TakenAtSource: "exif",
	})
	add("shelf", photos.Photo{
		FileHash: "ql-5", FilePath: "p/5.jpg", FileName: "IMG_0005.jpg", FileMime: "image/jpeg",
		FileWidth: 2000, FileHeight: 2000,
		Title:   "Book shelf",
		TakenAt: ptrOf(time.Date(2021, 3, 10, 12, 0, 0, 0, time.UTC)), TakenAtSource: "exif",
	})
	attic := add("attic", photos.Photo{
		FileHash: "ql-6", FilePath: "p/6.jpg", FileName: "IMG_0006.jpg", FileMime: "image/jpeg",
		Title:   "Old attic",
		TakenAt: ptrOf(time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)), TakenAtSource: "exif",
	})
	if _, err := store.Archive(ctx, attic.UID); err != nil {
		t.Fatalf("Archive(attic): %v", err)
	}

	seedQueryOrganisation(t, db.Pool(), lib, beach, prague2, winter, clip)
	return lib
}

// seedQueryOrganisation attaches the organisation and people fixtures (albums,
// labels, subjects, markers, faces, places, ratings, favorites) to the seeded
// photos.
func seedQueryOrganisation(
	t *testing.T, pool *pgxpool.Pool, lib queryLibrary, beach, prague2, winter, clip photos.Photo,
) {
	t.Helper()
	ctx := t.Context()
	org := organize.NewStore(pool)
	ppl := people.NewStore(pool)

	album, err := org.CreateAlbum(ctx, organize.Album{Title: "Léto 2024"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	for _, uid := range []string{beach.UID, prague2.UID} {
		if err := org.AddPhoto(ctx, album.UID, uid); err != nil {
			t.Fatalf("AddPhoto: %v", err)
		}
	}

	labels := map[string]string{"cat": beach.UID, "dog": winter.UID, "blurry": clip.UID}
	for name, photoUID := range labels {
		label, err := org.CreateLabel(ctx, organize.Label{Name: name})
		if err != nil {
			t.Fatalf("CreateLabel(%s): %v", name, err)
		}
		if err := org.AttachLabel(ctx, photoUID, label.UID, organize.SourceManual, 0); err != nil {
			t.Fatalf("AttachLabel(%s): %v", name, err)
		}
	}

	alice, err := ppl.CreateSubject(ctx, people.Subject{Name: "Alice", Type: people.SubjectPerson})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	markers := []people.Marker{
		{PhotoUID: beach.UID, SubjectUID: &alice.UID, Type: people.MarkerFace, X: 0.1, Y: 0.1, W: 0.2, H: 0.2},
		{PhotoUID: beach.UID, Type: people.MarkerFace, X: 0.5, Y: 0.1, W: 0.2, H: 0.2},
		{PhotoUID: winter.UID, Type: people.MarkerFace, X: 0.1, Y: 0.1, W: 0.2, H: 0.2},
		{PhotoUID: winter.UID, Type: people.MarkerFace, X: 0.5, Y: 0.5, W: 0.2, H: 0.2, Invalid: true},
	}
	for i, m := range markers {
		if _, err := ppl.CreateMarker(ctx, m); err != nil {
			t.Fatalf("CreateMarker(%d): %v", i, err)
		}
	}

	// One detected-but-unassigned face on winter for face:new. The embedding
	// content is irrelevant to the filter; a zero vector satisfies the column.
	zeroVec := "[" + strings.TrimSuffix(strings.Repeat("0,", 512), ",") + "]"
	if _, err := pool.Exec(ctx,
		`INSERT INTO faces (photo_uid, face_index, embedding, bbox, det_score, model)
		 VALUES ($1, 0, $2::halfvec, '{0.1,0.1,0.2,0.2}', 0.9, 'test')`,
		winter.UID, zeroVec); err != nil {
		t.Fatalf("insert face: %v", err)
	}

	places := [][3]any{
		{beach.UID, "Czechia", "Praha"},
		{prague2.UID, "Czechia", "Praha"},
		{winter.UID, "Czechia", "Brno"},
	}
	for _, p := range places {
		if _, err := pool.Exec(ctx,
			`INSERT INTO photo_places (photo_uid, country, city) VALUES ($1, $2, $3)`,
			p[0], p[1], p[2]); err != nil {
			t.Fatalf("insert place: %v", err)
		}
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (uid, username, password_hash, role) VALUES ($1, 'ql-user', 'x', 'editor')`,
		lib.user); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := org.SetRating(ctx, lib.user, beach.UID, 5); err != nil {
		t.Fatalf("SetRating(beach): %v", err)
	}
	if err := org.SetRating(ctx, lib.user, winter.UID, 2); err != nil {
		t.Fatalf("SetRating(winter): %v", err)
	}
	if err := org.SetFlag(ctx, lib.user, beach.UID, "pick"); err != nil {
		t.Fatalf("SetFlag(beach): %v", err)
	}
	if err := org.SetFlag(ctx, lib.user, clip.UID, "reject"); err != nil {
		t.Fatalf("SetFlag(clip): %v", err)
	}
	if err := org.AddFavorite(ctx, lib.user, winter.UID); err != nil {
		t.Fatalf("AddFavorite(winter): %v", err)
	}
}

// runLanguage parses input through the query language, maps it onto ListParams
// exactly like the API layer does, and returns the sorted fixture names of the
// photos List yields — the end-to-end parse→compile→SQL round trip.
func runLanguage(t *testing.T, lib queryLibrary, input string) []string {
	t.Helper()
	parsed := query.Parse(input)
	params := photos.ListParams{
		Search:       parsed.PlainText(),
		SearchNot:    parsed.NotTerms(),
		QueryFilters: parsed.Filters,
		RatedBy:      &lib.user,
	}
	list, err := lib.store.List(t.Context(), params)
	if err != nil {
		t.Fatalf("List(%q): %v", input, err)
	}
	names := make([]string, 0, len(list))
	for _, p := range list {
		name, ok := lib.names[p.UID]
		if !ok {
			name = p.UID
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestQueryLanguage_filters drives every filter of the search query language
// through the real store: each returns what it should, '|' widens, '!'
// excludes, ranges bound correctly, near: is geographic and faces: counts
// markers.
func TestQueryLanguage_filters(t *testing.T) {
	lib := seedQueryLibrary(t)
	nearBeach := "near:" + lib.uids["beach"]

	tests := []struct {
		query string
		want  []string
	}{
		// Text filters.
		{"title:beach", []string{"beach"}},
		{"title:*sunset", []string{"beach"}},
		{"description:warm", []string{"beach"}},
		{"notes:todo", []string{"winter"}},
		{"keywords:summer", []string{"beach"}},
		{"filename:IMG_*", []string{"beach", "prague2", "shelf"}},
		// Organisation.
		{`album:"Léto 2024"`, []string{"beach", "prague2"}},
		{"label:cat", []string{"beach"}},
		{"label:cat|dog", []string{"beach", "winter"}},
		{"label:!blurry", []string{"beach", "prague2", "shelf", "winter"}},
		{"label:cat|!dog", []string{"beach", "clip", "prague2", "shelf"}},
		{"person:alice", []string{"beach"}},
		{"subject:Alice", []string{"beach"}},
		{"favorite:yes", []string{"winter"}},
		{"favorite:no", []string{"beach", "clip", "prague2", "shelf"}},
		{"private:yes", []string{"winter"}},
		{"rating:4-5", []string{"beach"}},
		{"rating:2", []string{"winter"}},
		{"rating:0", []string{"clip", "prague2", "shelf"}},
		{"flag:pick", []string{"beach"}},
		{"flag:pick|reject", []string{"beach", "clip"}},
		// Archive state: the filter lifts the default live-only scope.
		{"archived:yes", []string{"attic"}},
		{"archived:no", []string{"beach", "clip", "prague2", "shelf", "winter"}},
		// Time.
		{"year:2024", []string{"beach", "prague2"}},
		{"year:2022-2023", []string{"clip", "winter"}},
		{"month:5", []string{"beach", "prague2"}},
		{"day:24", []string{"winter"}},
		{"taken:2024-05-01", []string{"beach"}},
		{"taken:2024-05", []string{"beach", "prague2"}},
		{"before:2023", []string{"clip", "shelf"}},
		{"after:2024-05-02", []string{"prague2"}},
		{fmt.Sprintf("added:%d", time.Now().UTC().Year()),
			[]string{"beach", "clip", "prague2", "shelf", "winter"}},
		// Place and geography.
		{"country:czechia", []string{"beach", "prague2", "winter"}},
		{"city:Praha", []string{"beach", "prague2"}},
		{"city:praha|brno", []string{"beach", "prague2", "winter"}},
		{"geo:yes", []string{"beach", "prague2", "winter"}},
		{"geo:no", []string{"clip", "shelf"}},
		{"alt:250-400", []string{"beach"}},
		{"alt:-250", []string{"winter"}},
		{nearBeach, []string{"beach", "prague2"}},
		{nearBeach + " dist:0.1", []string{"beach"}},
		{nearBeach + " dist:300", []string{"beach", "prague2", "winter"}},
		// Camera / optics.
		{"camera:canon", []string{"beach"}},
		{`camera:"EOS R6"`, []string{"beach"}},
		{"lens:rf", []string{"beach"}},
		{"iso:100-400", []string{"beach"}},
		{"iso:800-", []string{"winter"}},
		{"iso:-200", []string{"beach"}},
		{"f:2.8-4.5", []string{"beach"}},
		{"f:1.8", []string{"winter"}},
		{"mm:28-35", []string{"winter"}},
		{"mp:20-", []string{"beach", "winter"}},
		{"mp:2-3", []string{"clip"}},
		// Media.
		{"type:video", []string{"clip"}},
		{"type:image|live", []string{"beach", "prague2", "shelf", "winter"}},
		{"type:!video", []string{"beach", "prague2", "shelf", "winter"}},
		{"codec:hevc", []string{"clip"}},
		{"codec:jpeg", []string{"beach"}},
		{"portrait:yes", []string{"winter"}},
		{"landscape:yes", []string{"beach", "clip", "prague2"}},
		{"square:yes", []string{"shelf"}},
		{"panorama:yes", []string{"prague2"}},
		// Faces.
		{"faces:yes", []string{"beach", "winter"}},
		{"faces:no", []string{"clip", "prague2", "shelf"}},
		{"faces:2", []string{"beach"}},
		{"faces:1-1", []string{"winter"}},
		{"face:new", []string{"winter"}},
		// Free text mixed with filters, '-' negation, unknown keys.
		{"sunset", []string{"beach", "prague2"}},
		{"sunset -warm", []string{"prague2"}},
		{"sunset year:2024 iso:100", []string{"beach"}},
		{"color:red", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := runLanguage(t, lib, tt.query)
			want := append([]string{}, tt.want...)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("query %q = %v, want %v", tt.query, got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("query %q = %v, want %v", tt.query, got, want)
				}
			}
		})
	}
}

// TestQueryLanguage_countMatchesList verifies Count shares the query-language
// filters with List, the invariant pagination totals rely on.
func TestQueryLanguage_countMatchesList(t *testing.T) {
	lib := seedQueryLibrary(t)
	parsed := query.Parse("year:2024 label:cat|dog")
	params := photos.ListParams{QueryFilters: parsed.Filters, RatedBy: &lib.user}
	list, err := lib.store.List(t.Context(), params)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	total, err := lib.store.Count(t.Context(), params)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != len(list) {
		t.Fatalf("Count = %d, List = %d, want equal", total, len(list))
	}
}
