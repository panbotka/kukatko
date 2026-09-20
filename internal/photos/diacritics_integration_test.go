//go:build integration

package photos_test

import (
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

// diacriticsLibrary is the fixture the accent-folding assertions run against:
// one photo carrying an accented value in every text-searchable place, and one
// carrying the *un*accented spelling of the same word — so both directions of
// the fold are covered by real rows rather than by the compiled SQL alone.
type diacriticsLibrary struct {
	store *photos.Store
	// accented is the UID of the photo whose values carry their diacritics.
	accented string
	// bare is the UID of the photo whose values are spelled without them.
	bare string
	// user is the UID of the acting user, needed for the per-user filters and
	// as the uploader the uploader: filter resolves.
	user string
}

// seedDiacriticsLibrary creates the two photos and hangs every text-searchable
// relation off them: places, an album, a label, a subject with a marker, a task
// and an uploader.
//
// The "bare" photo is not merely a second row: it is the mirror case. Its title
// and its OCR text are what a recogniser or an import actually produces — Czech
// words with the diacritics dropped — and a search typed *with* háčky has to
// reach them, which only folding both sides of the comparison achieves.
func seedDiacriticsLibrary(t *testing.T) diacriticsLibrary {
	t.Helper()
	store, db := newStore(t)
	ctx := t.Context()
	lib := diacriticsLibrary{store: store, user: "u_dia"}

	taken := time.Date(2024, 8, 10, 12, 0, 0, 0, time.UTC)
	accented := mustCreate(t, store, photos.Photo{
		FileHash: "dia-1", FilePath: "p/1.jpg", FileName: "Dovolená_2024.jpg", FileMime: "image/jpeg",
		FileWidth: 4000, FileHeight: 3000,
		Title: "Pouť ve Veselici", Description: "Krásné odpoledne", Notes: "Dodělat popisky",
		Keywords: "pouť,vesnice", LensModel: "Meyer-Optik Görlitz Trioplan",
		CameraMake: "Škoda", CameraModel: "Škoda Fotoaparát", ImageCodec: "jpeg",
		TakenAt: &taken, TakenAtSource: "exif",
	})
	bare := mustCreate(t, store, photos.Photo{
		FileHash: "dia-2", FilePath: "p/2.jpg", FileName: "Dovolena_2025.jpg", FileMime: "image/jpeg",
		FileWidth: 4000, FileHeight: 3000,
		Title:   "Pout ve Veselici",
		TakenAt: &taken, TakenAtSource: "exif",
	})
	lib.accented, lib.bare = accented.UID, bare.UID

	// The recogniser reads Latin script and routinely drops the háčky; the
	// accented photo is the one carrying that bare reading.
	if err := store.SaveOCR(ctx, accented.UID, photos.OCR{Text: "Pout 2026", Model: "test"}); err != nil {
		t.Fatalf("SaveOCR: %v", err)
	}
	seedDiacriticsRelations(t, db.Pool(), lib)
	return lib
}

// seedDiacriticsRelations attaches the accented relations — place, album, label,
// subject, task, uploader — to the accented photo.
func seedDiacriticsRelations(t *testing.T, pool *pgxpool.Pool, lib diacriticsLibrary) {
	t.Helper()
	ctx := t.Context()
	org := organize.NewStore(pool)
	ppl := people.NewStore(pool)

	if _, err := pool.Exec(ctx,
		`INSERT INTO photo_places (photo_uid, country, city) VALUES ($1, 'Česko', 'Chotěmice')`,
		lib.accented); err != nil {
		t.Fatalf("insert place: %v", err)
	}

	album, err := org.CreateAlbum(ctx, organize.Album{Title: "Dovolená 2024"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if err := org.AddPhoto(ctx, album.UID, lib.accented); err != nil {
		t.Fatalf("AddPhoto: %v", err)
	}

	label, err := org.CreateLabel(ctx, organize.Label{Name: "kytička"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if err := org.AttachLabel(ctx, lib.accented, label.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel: %v", err)
	}

	subject, err := ppl.CreateSubject(ctx, people.Subject{
		Name: "Tomáš Nečas", Nickname: "Křeček", Type: people.SubjectPerson,
	})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	if _, err := ppl.CreateMarker(ctx, people.Marker{
		PhotoUID: lib.accented, SubjectUID: &subject.UID, Type: people.MarkerFace,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	}); err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (uid, username, display_name, email, password_hash, role)
		 VALUES ($1, 'dia-user', 'Tomáš Nečas', 'dia-user@example.test', 'x', 'editor')`,
		lib.user); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE photos SET uploaded_by = $2 WHERE uid = $1`, lib.accented, lib.user); err != nil {
		t.Fatalf("set uploader: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO photo_tasks (uid, title, created_by) VALUES ('pt_dia', 'Doplnit popisky pouti', $1)`,
		lib.user); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO photo_task_photos (task_uid, photo_uid) VALUES ('pt_dia', $1)`,
		lib.accented); err != nil {
		t.Fatalf("insert task photo: %v", err)
	}
}

// runDiacritics parses input through the query language, maps it onto
// ListParams exactly like the API layer does, and returns the matched UIDs
// sorted — the end-to-end parse→compile→SQL round trip.
func runDiacritics(t *testing.T, lib diacriticsLibrary, input string) []string {
	t.Helper()
	parsed := query.Parse(input)
	list, err := lib.store.List(t.Context(), photos.ListParams{
		Search:       parsed.PlainText(),
		SearchNot:    parsed.NotTerms(),
		QueryFilters: parsed.Filters,
		RatedBy:      &lib.user,
	})
	if err != nil {
		t.Fatalf("List(%q): %v", input, err)
	}
	uids := make([]string, 0, len(list))
	for _, p := range list {
		uids = append(uids, p.UID)
	}
	sort.Strings(uids)
	return uids
}

// TestQueryLanguage_diacriticsInsensitive is the end-to-end proof of the rule:
// every text filter the query language compiles matches regardless of which
// side of the comparison carries the diacritics.
//
// It is the key:value counterpart of TestSearch_diacriticsInsensitive, which
// already held the same line for the ranked full-text path.
//
// Each case names one filter and gives two spellings of the same value. Both
// must return the same photos, and that set must be non-empty — an assertion
// that would otherwise pass trivially for a filter that matches nothing at all.
func TestQueryLanguage_diacriticsInsensitive(t *testing.T) {
	lib := seedDiacriticsLibrary(t)

	tests := []struct {
		name string
		// accented and bare are the two spellings that must agree.
		accented string
		bare     string
		// want is how many photos both spellings must return.
		want int
	}{
		{"title", "title:Pouť", "title:Pout", 2},
		{"description", "description:Krásné", "description:Krasne", 1},
		{"notes", "notes:Dodělat", "notes:Dodelat", 1},
		{"filename", "filename:Dovolená", "filename:Dovolena", 2},
		{"keywords", "keywords:pouť", "keywords:pout", 1},
		{"text", "text:Pouť", "text:Pout", 1},
		{"lens", "lens:Görlitz", "lens:Gorlitz", 1},
		{"camera", "camera:Škoda", "camera:Skoda", 1},
		{"country", "country:Česko", "country:Cesko", 1},
		{"city", "city:Chotěmice", "city:Chotemice", 1},
		{"album", "album:Dovolená", "album:Dovolena", 1},
		{"label", "label:kytička", "label:kyticka", 1},
		{"task", "task:pouti", "task:poutí", 1},
		{"person by name", "person:Nečas", "person:Necas", 1},
		{"person by nickname", "person:Křeček", "person:Krecek", 1},
		{"family by root name", "family:Nečas", "family:Necas", 1},
		{"uploader", "uploader:Nečas", "uploader:Necas", 1},
		{"free text", "Pouť", "Pout", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withAccents := runDiacritics(t, lib, tt.accented)
			without := runDiacritics(t, lib, tt.bare)
			if len(withAccents) != tt.want {
				t.Fatalf("%q matched %d photos, want %d", tt.accented, len(withAccents), tt.want)
			}
			if strings.Join(withAccents, ",") != strings.Join(without, ",") {
				t.Errorf("%q matched %v but %q matched %v; the two spellings must agree",
					tt.accented, withAccents, tt.bare, without)
			}
		})
	}
}

// TestQueryLanguage_diacriticsInsensitiveNegatedTerm covers the free-text exclusion,
// which has no key of its own: `-pout` has to drop the "Pouť" photo as surely
// as `-pouť` does, or an exclusion silently fails to exclude.
func TestQueryLanguage_diacriticsInsensitiveNegatedTerm(t *testing.T) {
	lib := seedDiacriticsLibrary(t)

	withAccents := runDiacritics(t, lib, "-Pouť")
	without := runDiacritics(t, lib, "-Pout")
	if len(withAccents) != 0 {
		t.Fatalf("-Pouť left %v; both fixture titles hold the word", withAccents)
	}
	if strings.Join(withAccents, ",") != strings.Join(without, ",") {
		t.Errorf("-Pouť left %v but -Pout left %v; the two spellings must agree", withAccents, without)
	}
}
