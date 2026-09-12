package family

import (
	"errors"
	"testing"
)

// TestRole_valid accepts the three roles a relation can be recorded in and
// nothing else — notably not "sibling", which is derived from a shared family and
// is recorded by giving two children the same parent.
func TestRole_valid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		role Role
		want bool
	}{
		{role: RoleParent, want: true},
		{role: RoleChild, want: true},
		{role: RolePartner, want: true},
		{role: "sibling", want: false},
		{role: "", want: false},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			t.Parallel()
			if got := tc.role.valid(); got != tc.want {
				t.Errorf("Role(%q).valid() = %v, want %v", tc.role, got, tc.want)
			}
		})
	}
}

// TestCheckRelationTarget refuses every request that does not name exactly one
// other person. The two halves of the rule fail differently on purpose: naming
// both an existing and a new subject is a contradiction the client must resolve,
// while a new subject whose name identifies nobody would be stored under the
// shared fallback slug and read as unnamed everywhere afterwards.
func TestCheckRelationTarget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		rel  AddRelation
		want error
	}{
		{
			name: "existing subject",
			rel:  AddRelation{Role: RoleParent, SubjectUID: "su_dad"},
		},
		{
			name: "new subject",
			rel:  AddRelation{Role: RoleParent, New: &NewPerson{Name: "Marie Nečasová"}},
		},
		{
			name: "both",
			rel:  AddRelation{Role: RoleParent, SubjectUID: "su_dad", New: &NewPerson{Name: "Marie"}},
			want: ErrAmbiguousRelation,
		},
		{
			name: "neither",
			rel:  AddRelation{Role: RoleParent},
			want: ErrAmbiguousRelation,
		},
		{
			name: "blank uid is not a subject",
			rel:  AddRelation{Role: RoleParent, SubjectUID: "   "},
			want: ErrAmbiguousRelation,
		},
		{
			name: "new subject naming nobody",
			rel:  AddRelation{Role: RoleParent, New: &NewPerson{Name: "  ???  "}},
			want: ErrSubjectNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkRelationTarget(tc.rel)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("checkRelationTarget() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("checkRelationTarget() = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestWithinDepth trims a descendant walk to the generations that were asked for,
// keeping the root (depth 0) whatever the bound.
func TestWithinDepth(t *testing.T) {
	t.Parallel()
	members := []Member{
		{Relative: Relative{UID: "su_root"}, Depth: 0},
		{Relative: Relative{UID: "su_kid"}, Depth: 1},
		{Relative: Relative{UID: "su_inlaw"}, Depth: 1, Partner: true},
		{Relative: Relative{UID: "su_grandkid"}, Depth: 2},
	}
	got := withinDepth(members, 1)
	if len(got) != 3 {
		t.Fatalf("withinDepth(…, 1) kept %d members, want 3: %+v", len(got), got)
	}
	for _, m := range got {
		if m.UID == "su_grandkid" {
			t.Errorf("a grandchild survived a one-generation walk: %+v", got)
		}
	}
}
