package family

import (
	"errors"
	"testing"
	"time"
)

// pairOf renders a normalised pair for a test message, writing an absent partner
// as "-" so a nil is visible.
func pairOf(first, second *string) string {
	out := "-"
	if first != nil {
		out = *first
	}
	if second == nil {
		return out + "/-"
	}
	return out + "/" + *second
}

// The database stores a couple in byte order and holds that with a COLLATE "C"
// CHECK, so normalisePair has to agree with it exactly — including on the
// underscore and the digits, where the locale-dependent default collation does
// not.
func TestNormalisePair_ordersByByteOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want string
	}{
		{name: "already ordered stays", a: "su1", b: "su2", want: "su1/su2"},
		{name: "reversed is swapped", a: "su2", b: "su1", want: "su1/su2"},
		{name: "lone parent lands in the first column", a: "su9", b: "", want: "su9/-"},
		{name: "lone parent given second still lands first", a: "", b: "su9", want: "su9/-"},
		{name: "neither is an empty pair", a: "", b: "", want: "-/-"},
		{name: "underscore sorts before a letter, as in C", a: "su_a", b: "suza", want: "su_a/suza"},
		{name: "digits sort before letters", a: "sub", b: "su1", want: "su1/sub"},
		{name: "uppercase sorts before lowercase", a: "suA", b: "sua", want: "suA/sua"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			first, second := normalisePair(tt.a, tt.b)
			if got := pairOf(first, second); got != tt.want {
				t.Errorf("normalisePair(%q, %q) = %s, want %s", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// A normalised pair must be stable: normalising it again changes nothing, which
// is what makes "find the family of this couple" a single equality lookup.
func TestNormalisePair_isIdempotent(t *testing.T) {
	t.Parallel()

	first, second := normalisePair("sub", "sua")
	again, againSecond := normalisePair(*first, *second)
	if pairOf(first, second) != pairOf(again, againSecond) {
		t.Errorf("re-normalising %s gave %s", pairOf(first, second), pairOf(again, againSecond))
	}
}

// The cycle check is the one contradiction SQL cannot refuse on its own, so the
// rule itself is a pure function over the ancestor set the store reads.
func TestWouldCycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		child     string
		ancestors []string
		want      bool
	}{
		{name: "an unrelated child is fine", child: "su1", ancestors: []string{"su2", "su3"}, want: false},
		{name: "the parent itself is a cycle", child: "su1", ancestors: []string{"su1"}, want: true},
		{name: "a distant ancestor is a cycle", child: "su9", ancestors: []string{"su2", "su5", "su9"}, want: true},
		{name: "nobody above the parent", child: "su1", ancestors: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wouldCycle(tt.child, tt.ancestors); got != tt.want {
				t.Errorf("wouldCycle(%q, %v) = %v, want %v", tt.child, tt.ancestors, got, tt.want)
			}
		})
	}
}

func TestCheckPair(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		a, b    string
		wantErr error
	}{
		{name: "two different subjects", a: "su1", b: "su2", wantErr: nil},
		{name: "the same subject twice", a: "su1", b: "su1", wantErr: ErrSelfRelation},
		{name: "an empty first uid", a: "", b: "su2", wantErr: ErrSubjectNotFound},
		{name: "an empty second uid", a: "su1", b: "", wantErr: ErrSubjectNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := checkPair(tt.a, tt.b); !errors.Is(err, tt.wantErr) {
				t.Errorf("checkPair(%q, %q) = %v, want %v", tt.a, tt.b, err, tt.wantErr)
			}
		})
	}
}

func TestCheckYears(t *testing.T) {
	t.Parallel()

	year := func(v int) *int { return &v }
	tests := []struct {
		name     string
		from, to *int
		wantErr  error
	}{
		{name: "both unknown", from: nil, to: nil, wantErr: nil},
		{name: "a plain span", from: year(1921), to: year(1979), wantErr: nil},
		{name: "one year only", from: year(1921), to: nil, wantErr: nil},
		{name: "before photography", from: year(1799), to: nil, wantErr: ErrInvalidYears},
		{name: "a mistyped year", from: year(198), to: nil, wantErr: ErrInvalidYears},
		{name: "in the future", from: year(2100), to: nil, wantErr: ErrInvalidYears},
		{name: "ending before it began", from: year(1950), to: year(1949), wantErr: ErrInvalidYears},
		{name: "ending the year it began", from: year(1950), to: year(1950), wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := checkYears(tt.from, tt.to, 2026); !errors.Is(err, tt.wantErr) {
				t.Errorf("checkYears = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckUpdate(t *testing.T) {
	t.Parallel()

	next := time.Now().Year() + 1
	tests := []struct {
		name     string
		upd      Update
		wantKind Kind
		wantErr  error
	}{
		{name: "an empty kind defaults to a partnership", upd: Update{}, wantKind: KindPartnership},
		{name: "a marriage stays a marriage", upd: Update{Kind: KindMarriage}, wantKind: KindMarriage},
		{name: "an unknown kind is refused", upd: Update{Kind: "engagement"}, wantErr: ErrInvalidKind},
		{name: "a future year is refused", upd: Update{FromYear: &next}, wantErr: ErrInvalidYears},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := checkUpdate(tt.upd)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("checkUpdate(%+v) error = %v, want %v", tt.upd, err, tt.wantErr)
			}
			if tt.wantErr == nil && got.Kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", got.Kind, tt.wantKind)
			}
		})
	}
}

func TestChildKindOrDefault(t *testing.T) {
	t.Parallel()

	if got := childKindOrDefault(""); got != ChildBirth {
		t.Errorf("childKindOrDefault(\"\") = %q, want %q", got, ChildBirth)
	}
	if got := childKindOrDefault(ChildStep); got != ChildStep {
		t.Errorf("childKindOrDefault(%q) = %q, want it kept", ChildStep, got)
	}
}

func TestCheckChildKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    ChildKind
		want    ChildKind
		wantErr error
	}{
		{name: "empty is left for the write path to resolve", kind: "", want: ""},
		{name: "adopted is kept", kind: ChildAdopted, want: ChildAdopted},
		{name: "step is kept", kind: ChildStep, want: ChildStep},
		{name: "anything else is refused", kind: "foster", wantErr: ErrInvalidKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := checkChildKind(tt.kind)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("checkChildKind(%q) error = %v, want %v", tt.kind, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("checkChildKind(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

// A family's partners are read back the way the tree draws them: the recorded
// ones, and "who is the other one" for a given side.
func TestFamilyPartners(t *testing.T) {
	t.Parallel()

	a, b := "su1", "su2"
	couple := Family{PartnerA: &a, PartnerB: &b}
	lone := Family{PartnerA: &a}

	if got := couple.partnerUIDs(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("couple.partnerUIDs() = %v, want [%s %s]", got, a, b)
	}
	if got := lone.partnerUIDs(); len(got) != 1 || got[0] != a {
		t.Errorf("lone.partnerUIDs() = %v, want [%s]", got, a)
	}
	if got := couple.other(a); got == nil || *got != b {
		t.Errorf("couple.other(%s) = %v, want %s", a, got, b)
	}
	if got := couple.other(b); got == nil || *got != a {
		t.Errorf("couple.other(%s) = %v, want %s", b, got, a)
	}
	if got := lone.other(a); got != nil {
		t.Errorf("lone.other(%s) = %v, want nil", a, *got)
	}
	if got := couple.other("su9"); got != nil {
		t.Errorf("couple.other(a stranger) = %v, want nil", *got)
	}
}

func TestKindsAndDirection_valid(t *testing.T) {
	t.Parallel()

	for _, k := range []Kind{KindMarriage, KindPartnership, KindUnknown} {
		if !k.valid() {
			t.Errorf("Kind(%q).valid() = false", k)
		}
	}
	if Kind("engagement").valid() {
		t.Error(`Kind("engagement").valid() = true`)
	}
	for _, k := range []ChildKind{ChildBirth, ChildAdopted, ChildStep} {
		if !k.valid() {
			t.Errorf("ChildKind(%q).valid() = false", k)
		}
	}
	if ChildKind("foster").valid() {
		t.Error(`ChildKind("foster").valid() = true`)
	}
	for _, d := range []Direction{DirectionDescendants, DirectionAncestors} {
		if !d.valid() {
			t.Errorf("Direction(%q).valid() = false", d)
		}
	}
	if Direction("sideways").valid() {
		t.Error(`Direction("sideways").valid() = true`)
	}
}

func TestClampGenerations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "a plain request is kept", in: 4, want: 4},
		{name: "zero means the whole bounded walk", in: 0, want: MaxDepth},
		{name: "negative means the same", in: -3, want: MaxDepth},
		{name: "the guard is the ceiling", in: 1000, want: MaxDepth},
		{name: "the ceiling itself is kept", in: MaxDepth, want: MaxDepth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := clampGenerations(tt.in); got != tt.want {
				t.Errorf("clampGenerations(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
