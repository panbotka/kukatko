package people

import (
	"strings"
	"testing"
)

// TestNewSubjectAndMarkerUID checks the prefix, length and alphabet of generated
// UIDs and that successive calls differ.
func TestNewSubjectAndMarkerUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		gen    func() (string, error)
		prefix string
	}{
		{name: "subject", gen: newSubjectUID, prefix: subjectUIDPrefix},
		{name: "marker", gen: newMarkerUID, prefix: markerUIDPrefix},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			uid, err := tt.gen()
			if err != nil {
				t.Fatalf("%s uid: %v", tt.name, err)
			}
			if !strings.HasPrefix(uid, tt.prefix) {
				t.Errorf("uid %q lacks prefix %q", uid, tt.prefix)
			}
			if len(uid) != len(tt.prefix)+uidSuffixLen {
				t.Errorf("uid %q length = %d, want %d", uid, len(uid), len(tt.prefix)+uidSuffixLen)
			}
			if len(uid) > uidMaxLen {
				t.Errorf("uid %q exceeds VARCHAR(%d)", uid, uidMaxLen)
			}
			for _, r := range uid[len(tt.prefix):] {
				if !strings.ContainsRune(uidAlphabet, r) {
					t.Errorf("uid %q has char %q outside alphabet", uid, r)
				}
			}
			other, _ := tt.gen()
			if uid == other {
				t.Errorf("two %s uids collided: %q", tt.name, uid)
			}
		})
	}
}

// TestSubjectTypeValid and the marker variant check the type-validation helpers.
func TestSubjectTypeValid(t *testing.T) {
	t.Parallel()

	valid := []SubjectType{SubjectPerson, SubjectPet, SubjectOther}
	for _, v := range valid {
		if !v.valid() {
			t.Errorf("SubjectType %q should be valid", v)
		}
	}
	for _, v := range []SubjectType{"", "robot", "Person"} {
		if v.valid() {
			t.Errorf("SubjectType %q should be invalid", v)
		}
	}
}

// TestMarkerTypeValid checks the marker type-validation helper.
func TestMarkerTypeValid(t *testing.T) {
	t.Parallel()

	for _, v := range []MarkerType{MarkerFace, MarkerLabel} {
		if !v.valid() {
			t.Errorf("MarkerType %q should be valid", v)
		}
	}
	for _, v := range []MarkerType{"", "blob", "Face"} {
		if v.valid() {
			t.Errorf("MarkerType %q should be invalid", v)
		}
	}
}

// TestMarkerValidBounds checks the normalised 0..1 bounds invariant.
func TestMarkerValidBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    Marker
		want bool
	}{
		{name: "all in range", m: Marker{X: 0.1, Y: 0.2, W: 0.3, H: 0.4}, want: true},
		{name: "zero box", m: Marker{}, want: true},
		{name: "full box", m: Marker{X: 0, Y: 0, W: 1, H: 1}, want: true},
		{name: "negative x", m: Marker{X: -0.1, W: 0.5, H: 0.5}, want: false},
		{name: "w over one", m: Marker{W: 1.5}, want: false},
		{name: "h over one", m: Marker{H: 2}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.m.validBounds(); got != tt.want {
				t.Errorf("validBounds(%+v) = %v, want %v", tt.m, got, tt.want)
			}
		})
	}
}
