//go:build integration

package photos_test

import (
	"sort"
	"testing"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// TestPersonFilter_nickname runs `person:` against the real database over two
// named people, one with a nickname and one without.
//
// The second person is the point of the fixture, not padding: `person:` matches
// the name OR the nickname, and an OR over a column that is the empty string for
// most of the library is an easy way to make every filter match everybody. A
// search for „Bohouš" must return his photo and only his.
func TestPersonFilter_nickname(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()
	ppl := people.NewStore(db.Pool())

	withNickname := mustCreatePhoto(t, store, "nick-bohous")
	withoutNickname := mustCreatePhoto(t, store, "nick-anna")

	bohous, err := ppl.CreateSubject(ctx, people.Subject{Name: "Bohumil Nečas", Nickname: "Bohouš"})
	if err != nil {
		t.Fatalf("CreateSubject(Bohumil): %v", err)
	}
	anna, err := ppl.CreateSubject(ctx, people.Subject{Name: "Anna Nováková"})
	if err != nil {
		t.Fatalf("CreateSubject(Anna): %v", err)
	}
	for _, m := range []people.Marker{
		{PhotoUID: withNickname, SubjectUID: &bohous.UID, Type: people.MarkerFace,
			X: 0.1, Y: 0.1, W: 0.2, H: 0.2},
		{PhotoUID: withoutNickname, SubjectUID: &anna.UID, Type: people.MarkerFace,
			X: 0.1, Y: 0.1, W: 0.2, H: 0.2},
	} {
		if _, err := ppl.CreateMarker(ctx, m); err != nil {
			t.Fatalf("CreateMarker on %s: %v", m.PhotoUID, err)
		}
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "the nickname finds the photo",
			query: `person:Bohouš`,
			want:  []string{withNickname},
		},
		{
			name:  "the nickname is matched case-insensitively",
			query: `person:bohouš`,
			want:  []string{withNickname},
		},
		{
			name:  "a partial nickname still matches",
			query: "person:Boho",
			want:  []string{withNickname},
		},
		{
			name:  "the name keeps working beside it",
			query: `person:Nečas`,
			want:  []string{withNickname},
		},
		{
			name:  "a person with no nickname is found by name",
			query: "person:Anna",
			want:  []string{withoutNickname},
		},
		{
			name:  "a nickname nobody carries matches nothing",
			query: "person:Pepa",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := query.Parse(tt.query)
			list, err := store.List(ctx, photos.ListParams{QueryFilters: parsed.Filters})
			if err != nil {
				t.Fatalf("List(%q): %v", tt.query, err)
			}
			got := make([]string, 0, len(list))
			for _, p := range list {
				got = append(got, p.UID)
			}
			sort.Strings(got)
			want := append([]string{}, tt.want...)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("query %q matched %v, want %v", tt.query, got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("query %q matched %v, want %v", tt.query, got, want)
				}
			}
		})
	}
}
