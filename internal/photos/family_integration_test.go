//go:build integration

package photos_test

import (
	"sort"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/personme"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// The `family:` filter walks two tables the photos store never otherwise
// touches, inside a correlated EXISTS, so only the real schema can say whether
// it means what it claims. The fixture below is deliberately the same village
// internal/family's own walk tests use: the filter's promise is that it matches
// exactly the set the drawn tree walks, and a second, differently shaped fixture
// would let the two drift apart without anything noticing.

// clan is the seeded genealogy plus one photo of each person, so a query's
// answer can be read back as "whose photos came out".
//
//	     Bohumil ⚭ Marie
//	      /             \
//	Josef ⚭ Ludmila   Anna ⚭ Karel
//	    |                  |
//	  Petr       ⚭        Eva        (first cousins, as villages do)
//	             |
//	            Jan
//
// Vojtěch stands outside it entirely: he is in no family at all, which is what
// makes "a subject with no descendants" a case rather than an assumption.
type clan struct {
	subject map[string]string
	photo   map[string]string
}

// photoOf returns the uid of the photo the named person is marked on.
func (c clan) photoOf(name string) string { return c.photo[name] }

// seedClan creates the nine relatives, the outsider, one photo of each and the
// family rows relating them, and returns the lookups the assertions read.
func seedClan(t *testing.T, store *photos.Store, db *database.DB) clan {
	t.Helper()
	ctx := t.Context()
	ppl := people.NewStore(db.Pool())
	fam := family.NewStore(db.Pool())

	actor := "usfamflt00000000000000001"
	if err := auth.NewStore(db.Pool()).CreateUser(ctx, auth.User{
		UID: actor, Username: "genealogist", Email: "genealogist@example.test",
		PasswordHash: "x", Role: auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating actor: %v", err)
	}

	c := clan{subject: map[string]string{}, photo: map[string]string{}}
	names := map[string]string{
		"bohumil": "Bohumil Nečas", "marie": "Marie Nečasová",
		"josef": "Josef Nečas", "ludmila": "Ludmila Nečasová",
		"anna": "Anna Skotáková", "karel": "Karel Skoták",
		"petr": "Petr Nečas", "eva": "Eva Skotáková", "jan": "Jan Nečas",
		"vojtech": "Vojtěch Souček",
	}
	for key, name := range names {
		subj, err := ppl.CreateSubject(ctx, people.Subject{Name: name})
		if err != nil {
			t.Fatalf("creating subject %s: %v", name, err)
		}
		c.subject[key] = subj.UID
		c.photo[key] = mustCreatePhoto(t, store, "family-"+key)
		if _, err := ppl.CreateMarker(ctx, people.Marker{
			PhotoUID: c.photo[key], SubjectUID: &subj.UID, Type: people.MarkerFace,
			X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
		}); err != nil {
			t.Fatalf("marking %s: %v", name, err)
		}
	}

	for _, rel := range []struct{ child, parentA, parentB string }{
		{"josef", "bohumil", "marie"},
		{"anna", "bohumil", "marie"},
		{"petr", "josef", "ludmila"},
		{"eva", "anna", "karel"},
		{"jan", "petr", "eva"},
	} {
		for _, parent := range []string{rel.parentA, rel.parentB} {
			if _, err := fam.AddParentAudited(ctx, c.subject[rel.child], c.subject[parent], "",
				audit.Entry{
					ActorUID: actor, Action: audit.ActionSubjectRelationAdd, TargetType: "subjects",
				}); err != nil {
				t.Fatalf("relating %s to %s: %v", rel.child, parent, err)
			}
		}
	}
	return c
}

// matchedUIDs runs one query through the store and returns the photo uids it
// matched, sorted so a comparison does not depend on the ordering.
func matchedUIDs(t *testing.T, store *photos.Store, filters []query.Filter) []string {
	t.Helper()
	list, err := store.List(t.Context(), photos.ListParams{QueryFilters: filters})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := make([]string, 0, len(list))
	for _, p := range list {
		got = append(got, p.UID)
	}
	sort.Strings(got)
	return got
}

// wantUIDs sorts an expected set the same way matchedUIDs sorts the answer.
func wantUIDs(uids ...string) []string {
	sorted := append([]string{}, uids...)
	sort.Strings(sorted)
	return sorted
}

// sameUIDs reports whether two already-sorted uid sets are equal.
func sameUIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestFamilyFilter_walksDescendantsAndTheirPartners(t *testing.T) {
	store, db := newStore(t)
	c := seedClan(t, store, db)

	// Everybody below Bohumil, plus the partners who married in: Marie is his
	// own, Ludmila and Karel married his children, Eva is both a granddaughter
	// and Petr's wife. Vojtěch, who is in no family, is the control.
	wholeClan := wantUIDs(
		c.photoOf("bohumil"), c.photoOf("marie"), c.photoOf("josef"), c.photoOf("ludmila"),
		c.photoOf("anna"), c.photoOf("karel"), c.photoOf("petr"), c.photoOf("eva"),
		c.photoOf("jan"),
	)

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "by name, from the root",
			query: `family:"Bohumil Nečas"`,
			want:  wholeClan,
		},
		{
			name:  "by uid, from the root",
			query: "family:" + c.subject["bohumil"],
			want:  wholeClan,
		},
		{
			name: "from the middle it is the branch below, not the whole clan",
			// Josef's line: Josef, his wife Ludmila, their son Petr, Petr's wife
			// Eva and their son Jan. His sister Anna's household is above him.
			query: "family:Josef",
			want: wantUIDs(c.photoOf("josef"), c.photoOf("ludmila"), c.photoOf("petr"),
				c.photoOf("eva"), c.photoOf("jan")),
		},
		{
			name: "a leaf is their own family, plus whoever they married",
			// Jan has no children, so the walk is just him. He also has no
			// partner, so nothing is added around him either.
			query: "family:Jan",
			want:  wantUIDs(c.photoOf("jan")),
		},
		{
			name:  "a subject in no family at all is only themselves",
			query: "family:Vojtěch",
			want:  wantUIDs(c.photoOf("vojtech")),
		},
		{
			name:  "an unknown name matches nothing rather than everything",
			query: "family:Kdokoliv",
			want:  nil,
		},
		{
			name: "negation is the rest of the library",
			// `!` is the language's filter negation ('-' negates free text
			// only). Vojtěch is the only person outside Bohumil's clan.
			query: "family:!" + c.subject["bohumil"],
			want:  wantUIDs(c.photoOf("vojtech")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := query.Parse(tt.query)
			got := matchedUIDs(t, store, parsed.Filters)
			if !sameUIDs(got, wantUIDs(tt.want...)) {
				t.Fatalf("query %q matched %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

// TestFamilyFilter_cousinMarriageIsCountedOnce is the diamond: Jan descends from
// Bohumil through his father Petr and through his mother Eva, so the walk reaches
// him twice. A photo may still appear exactly once in the answer — a duplicated
// row here would double-count a whole branch of the library.
func TestFamilyFilter_cousinMarriageIsCountedOnce(t *testing.T) {
	store, db := newStore(t)
	c := seedClan(t, store, db)

	parsed := query.Parse("family:" + c.subject["bohumil"])
	got := matchedUIDs(t, store, parsed.Filters)
	seen := map[string]int{}
	for _, uid := range got {
		seen[uid]++
	}
	if seen[c.photoOf("jan")] != 1 {
		t.Fatalf("Jan's photo matched %d times, want 1 (whole answer: %v)",
			seen[c.photoOf("jan")], got)
	}
	if len(got) != 9 {
		t.Fatalf("matched %d photos, want 9: %v", len(got), got)
	}
}

// TestFamilyFilter_me resolves the caller's own family the way person:me does —
// through internal/personme, which is what keeps internal/query caller-blind.
func TestFamilyFilter_me(t *testing.T) {
	store, db := newStore(t)
	c := seedClan(t, store, db)

	linked := c.subject["josef"]
	parsed := query.Parse("family:me")
	used, resolved := personme.Resolve(parsed.Filters, &linked)
	if !used || !resolved {
		t.Fatalf("Resolve(family:me) used=%v resolved=%v, want true/true", used, resolved)
	}
	got := matchedUIDs(t, store, parsed.Filters)
	want := wantUIDs(c.photoOf("josef"), c.photoOf("ludmila"), c.photoOf("petr"),
		c.photoOf("eva"), c.photoOf("jan"))
	if !sameUIDs(got, want) {
		t.Fatalf("family:me matched %v, want %v", got, want)
	}

	// An account linked to nobody cannot answer it, exactly as with person:me.
	unlinked := query.Parse("family:me")
	if _, resolved := personme.Resolve(unlinked.Filters, nil); resolved {
		t.Fatal("family:me resolved for an account with no linked person")
	}
}
