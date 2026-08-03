package query

import "testing"

// photoUID is a well-formed photo uid used as the shape template below: two
// prefix characters plus 24 base32 characters.
const photoUID = "ph7lpul2io09bcg2rvp2rljsr6"

// TestClassifyUID_recognisedShapes verifies every prefix routes to its entity
// and that the PhotoPrism shape is told apart by its length.
func TestClassifyUID_recognisedShapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		uid  string
		want EntityKind
	}{
		{"ph7lpul2io09bcg2rvp2rljsr6", EntityPhoto},
		{"al7lpul2io09bcg2rvp2rljsr6", EntityAlbum},
		{"lb7lpul2io09bcg2rvp2rljsr6", EntityLabel},
		{"su7lpul2io09bcg2rvp2rljsr6", EntityPerson},
		{"st7lpul2io09bcg2rvp2rljsr6", EntityStack},
		{"mk7lpul2io09bcg2rvp2rljsr6", EntityMarker},
		{"pt8suk5b57jgshdz", EntityPhotoprism},
	}
	for _, tt := range tests {
		t.Run(tt.uid, func(t *testing.T) {
			t.Parallel()
			got, ok := ClassifyUID(tt.uid)
			if !ok {
				t.Fatalf("ClassifyUID(%q) not recognised", tt.uid)
			}
			if got.Kind != tt.want || got.UID != tt.uid {
				t.Fatalf("ClassifyUID(%q) = %+v, want {%s %s}", tt.uid, got, tt.uid, tt.want)
			}
		})
	}
}

// TestClassifyUID_rejects verifies the shapes that must NOT be taken for a uid:
// an unknown prefix (never probed against every table), a wrong length, a
// character outside the alphabet, and ordinary words.
func TestClassifyUID_rejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		token string
	}{
		{"unknown prefix", "zz7lpul2io09bcg2rvp2rljsr6"},
		{"too short", photoUID[:25]},
		{"too long", photoUID + "x"},
		{"out of alphabet", "ph7lpul2io09bcg2rvp2rljszz"},
		{"photoprism wrong prefix", "xx8suk5b57jgshdz"},
		{"photoprism out of alphabet", "pt8suk5b57jgsh_z"},
		{"plain word", "dovolena"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, ok := ClassifyUID(tt.token); ok {
				t.Fatalf("ClassifyUID(%q) = %+v, want not recognised", tt.token, got)
			}
		})
	}
}

// TestClassifyUID_lowercases verifies a shouted or auto-capitalised id still
// resolves, and resolves to the lowercase form the database stores.
func TestClassifyUID_lowercases(t *testing.T) {
	t.Parallel()
	got, ok := ClassifyUID("PH7LPUL2IO09BCG2RVP2RLJSR6")
	if !ok || got.Kind != EntityPhoto || got.UID != photoUID {
		t.Fatalf("ClassifyUID(upper) = %+v, %v; want {%s photo}, true", got, ok, photoUID)
	}
}

// TestFindUID verifies a uid is found whether it is the whole input or one word
// of it, that the first one wins, and that ordinary text finds none.
func TestFindUID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
		found bool
	}{
		{"bare uid", photoUID, photoUID, true},
		{"padded", "  " + photoUID + " ", photoUID, true},
		{"with a word next to it", "photo " + photoUID, photoUID, true},
		{"first wins", photoUID + " al7lpul2io09bcg2rvp2rljsr6", photoUID, true},
		{"plain text", "dovolená u moře", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := FindUID(tt.input)
			if ok != tt.found {
				t.Fatalf("FindUID(%q) found = %v, want %v", tt.input, ok, tt.found)
			}
			if ok && got.UID != tt.want {
				t.Fatalf("FindUID(%q) = %q, want %q", tt.input, got.UID, tt.want)
			}
		})
	}
}

// TestParse_uidFilter verifies uid: is a recognised key of the query language,
// parsed as an opaque id and combinable with another filter.
func TestParse_uidFilter(t *testing.T) {
	t.Parallel()
	q := Parse("uid:" + photoUID + " archived:yes")
	if len(q.Unknown) != 0 {
		t.Fatalf("Unknown = %v, want none", q.Unknown)
	}
	if len(q.Filters) != 2 {
		t.Fatalf("Filters = %+v, want 2", q.Filters)
	}
	if q.Filters[0].Key != KeyUID || q.Filters[0].Values[0].Text != photoUID {
		t.Fatalf("first filter = %+v, want uid:%s", q.Filters[0], photoUID)
	}
	if !q.HasFilter(KeyArchived) {
		t.Fatalf("archived: was lost: %+v", q.Filters)
	}
	if q.FreeText() != "" {
		t.Fatalf("FreeText = %q, want empty (a uid query is pure filter)", q.FreeText())
	}
}
