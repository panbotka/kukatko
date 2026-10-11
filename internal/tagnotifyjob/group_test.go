package tagnotifyjob

import (
	"reflect"
	"strings"
	"testing"
)

// TestGroupByAlbum walks the grouping rules over one window: photos in no
// album, one album and two albums; a private album for a recipient who may
// and may not see it; the order of the groups; and the cap collapsing the
// remaining albums into "other" without repeating a photo already named.
func TestGroupByAlbum(t *testing.T) {
	t.Parallel()

	fair := albumRef{UID: "al-fair", Title: "Pouť 2026"}
	fire := albumRef{UID: "al-fire", Title: "Hasiči"}
	secret := albumRef{UID: "al-secret", Title: "Soukromé", Private: true}
	// Newest first, as the window reads them.
	window := []taggedPhoto{
		{UID: "p1", Albums: []albumRef{fire, fair}},
		{UID: "p2"},
		{UID: "p3", Albums: []albumRef{fair}},
		{UID: "p4", Albums: []albumRef{secret}},
		{UID: "p5", Albums: []albumRef{fair, secret}},
	}

	tests := []struct {
		name        string
		photos      []taggedPhoto
		seesPrivate bool
		maxAlbums   int
		want        []group
	}{
		{
			name: "viewer: the private album is other", photos: window, maxAlbums: MaxAlbumGroups,
			want: []group{
				// p1 is the newest photo of both albums; Hasiči sorts before Pouť.
				{Album: &fire, Photos: []string{"p1"}},
				{Album: &fair, Photos: []string{"p1", "p3", "p5"}},
				{Photos: []string{"p2", "p4"}},
			},
		},
		{
			name: "curator: the private album is a group of its own", photos: window, seesPrivate: true,
			maxAlbums: MaxAlbumGroups,
			want: []group{
				{Album: &fire, Photos: []string{"p1"}},
				{Album: &fair, Photos: []string{"p1", "p3", "p5"}},
				{Album: &secret, Photos: []string{"p4", "p5"}},
				{Photos: []string{"p2"}},
			},
		},
		{
			name: "cap: albums past the cap collapse into other", photos: window, seesPrivate: true, maxAlbums: 1,
			want: []group{
				{Album: &fire, Photos: []string{"p1"}},
				// p1 is already named under Hasiči, so it is not other as well.
				{Photos: []string{"p2", "p3", "p4", "p5"}},
			},
		},
		{
			name: "no cap at all: everything is other", photos: window, seesPrivate: true, maxAlbums: 0,
			want: []group{{Photos: []string{"p1", "p2", "p3", "p4", "p5"}}},
		},
		{
			name: "no albums: one group of other", maxAlbums: MaxAlbumGroups,
			photos: []taggedPhoto{{UID: "p1"}, {UID: "p2"}},
			want:   []group{{Photos: []string{"p1", "p2"}}},
		},
		{
			name: "every photo in an album: no other group", maxAlbums: MaxAlbumGroups,
			photos: []taggedPhoto{{UID: "p1", Albums: []albumRef{fair}}, {UID: "p2", Albums: []albumRef{fair}}},
			want:   []group{{Album: &fair, Photos: []string{"p1", "p2"}}},
		},
		{
			name: "the same album twice on a photo counts once", maxAlbums: MaxAlbumGroups,
			photos: []taggedPhoto{{UID: "p1", Albums: []albumRef{fair, fair}}},
			want:   []group{{Album: &fair, Photos: []string{"p1"}}},
		},
		{
			name: "order follows the newest photo, not the first album seen", maxAlbums: MaxAlbumGroups,
			photos: []taggedPhoto{{UID: "p1", Albums: []albumRef{fire}}, {UID: "p2", Albums: []albumRef{fair}}},
			want:   []group{{Album: &fire, Photos: []string{"p1"}}, {Album: &fair, Photos: []string{"p2"}}},
		},
		{name: "empty window", maxAlbums: MaxAlbumGroups, want: []group{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := groupByAlbum(tt.photos, tt.seesPrivate, tt.maxAlbums)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("groupByAlbum =\n%s\nwant\n%s", describe(got), describe(tt.want))
			}
		})
	}
}

// describe renders groups for a failure message.
func describe(groups []group) string {
	var out strings.Builder
	for _, g := range groups {
		title := "<other>"
		if g.Album != nil {
			title = g.Album.Title
		}
		out.WriteString("  " + title + ": " + strings.Join(g.Photos, ", ") + "\n")
	}
	return out.String()
}
