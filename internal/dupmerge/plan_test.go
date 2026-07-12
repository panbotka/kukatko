package dupmerge

import (
	"reflect"
	"testing"
)

func TestSubtract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		have   []string
		remove []string
		want   []string
	}{
		{name: "keeper lacks all", have: []string{"b", "a"}, remove: nil, want: []string{"a", "b"}},
		{name: "keeper has some", have: []string{"a", "b", "c"}, remove: []string{"b"}, want: []string{"a", "c"}},
		{name: "keeper has all", have: []string{"a", "b"}, remove: []string{"a", "b"}, want: []string{}},
		{name: "duplicates collapse", have: []string{"a", "a", "b"}, remove: nil, want: []string{"a", "b"}},
		{name: "empty have", have: nil, remove: []string{"a"}, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := subtract(tt.have, tt.remove); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subtract(%v, %v) = %v, want %v", tt.have, tt.remove, got, tt.want)
			}
		})
	}
}

func TestPickFill(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		keeper     string
		candidates []string
		want       *string
	}{
		{name: "keeper has value: never overwrite", keeper: "kept", candidates: []string{"other"}, want: nil},
		{name: "keeper empty, copy has value", keeper: "", candidates: []string{"", "from copy"}, want: new("from copy")},
		{name: "keeper empty, no copy has value", keeper: "", candidates: []string{"", ""}, want: nil},
		{name: "keeper empty, no candidates", keeper: "", candidates: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := pickFill(tt.keeper, tt.candidates)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pickFill(%q, %v) = %v, want %v", tt.keeper, tt.candidates, deref(got), deref(tt.want))
			}
		})
	}
}

// deref renders a *string for test messages ("<nil>" when nil).
func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestScalarFillFilledFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fill scalarFill
		want []string
	}{
		{name: "empty", fill: scalarFill{}, want: []string{}},
		{name: "title only", fill: scalarFill{title: new("t")}, want: []string{"title"}},
		{
			name: "all fields in stable order",
			fill: scalarFill{title: new("t"), description: new("d"), rating: new(4), favorite: true, flag: new("pick")},
			want: []string{"title", "description", "rating", "favorite", "flag"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.fill.filledFields(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("filledFields() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanIsEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan plan
		want bool
	}{
		{name: "nothing to do", plan: plan{}, want: true},
		{name: "albums to add", plan: plan{albumsToAdd: []string{"a"}}, want: false},
		{name: "people to add", plan: plan{subjectsToAdd: []string{"s"}}, want: false},
		{name: "copies to archive", plan: plan{archiveUIDs: []string{"c"}}, want: false},
		{name: "scalar to fill", plan: plan{fill: scalarFill{title: new("t")}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.plan.isEmpty(); got != tt.want {
				t.Errorf("isEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanResult(t *testing.T) {
	t.Parallel()
	p := plan{
		albumsToAdd:   []string{"a1", "a2"},
		labelsToAdd:   []string{"l1"},
		subjectsToAdd: []string{"s1"},
		fill:          scalarFill{title: new("t")},
		archiveUIDs:   []string{"c1", "c2"},
	}
	got := p.result("keep1", true)
	want := Result{
		KeeperUID:      "keep1",
		AlbumsAdded:    2,
		LabelsAdded:    1,
		PeopleAdded:    1,
		MetadataFilled: []string{"title"},
		Archived:       2,
		DryRun:         true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("result() = %+v, want %+v", got, want)
	}
}
