package query

import (
	"fmt"
	"strings"
	"testing"
)

// fmtQuery renders a parsed Query in a compact canonical form so the parser
// table below can state expectations as single strings:
//
//	terms=[t(word) t!(excluded) p(a phrase)] filters=[iso=num:100..400] unknown=[foo:bar]
//
// Empty sections are omitted; an all-empty query renders as "empty".
func fmtQuery(q Query) string {
	var sections []string
	if len(q.Terms) > 0 {
		parts := make([]string, len(q.Terms))
		for i, t := range q.Terms {
			parts[i] = fmtTerm(t)
		}
		sections = append(sections, "terms=["+strings.Join(parts, " ")+"]")
	}
	if len(q.Filters) > 0 {
		parts := make([]string, len(q.Filters))
		for i, f := range q.Filters {
			parts[i] = fmtFilter(f)
		}
		sections = append(sections, "filters=["+strings.Join(parts, " ")+"]")
	}
	if len(q.Unknown) > 0 {
		sections = append(sections, "unknown=["+strings.Join(q.Unknown, " ")+"]")
	}
	if len(sections) == 0 {
		return "empty"
	}
	return strings.Join(sections, " ")
}

// fmtTerm renders one free-text term: t(word), t!(negated), p(phrase),
// p!(negated phrase).
func fmtTerm(t Term) string {
	kind := "t"
	if t.Phrase {
		kind = "p"
	}
	if t.Not {
		kind += "!"
	}
	return kind + "(" + t.Text + ")"
}

// fmtFilter renders one filter as key=value|value with each value in its
// canonical shape (text:, num:lo..hi, date:from..until, bool:).
func fmtFilter(f Filter) string {
	parts := make([]string, len(f.Values))
	for i, v := range f.Values {
		parts[i] = fmtValue(v)
	}
	return string(f.Key) + "=" + strings.Join(parts, "|")
}

// fmtValue renders one value alternative, prefixing '!' for a negation.
func fmtValue(v Value) string {
	var b strings.Builder
	if v.Not {
		b.WriteString("!")
	}
	switch {
	case v.Bool != nil:
		fmt.Fprintf(&b, "bool:%t", *v.Bool)
	case v.From != nil:
		fmt.Fprintf(&b, "date:%s..%s", v.From.Format("2006-01-02"), v.Until.Format("2006-01-02"))
	case v.Min != nil || v.Max != nil:
		b.WriteString("num:")
		if v.Min != nil {
			fmt.Fprintf(&b, "%g", *v.Min)
		}
		b.WriteString("..")
		if v.Max != nil {
			fmt.Fprintf(&b, "%g", *v.Max)
		}
	default:
		b.WriteString("text:" + v.Text)
	}
	return b.String()
}

// TestParse_table is the exhaustive grammar table: every filter, every
// operator, quoting, escaping, ranges and malformed input. Expectations use
// the canonical rendering of fmtQuery.
func TestParse_table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		// --- Free text ---
		{"empty input", "", "empty"},
		{"whitespace only", "   \t  ", "empty"},
		{"plain words", "dovolená u moře", "terms=[t(dovolená) t(u) t(moře)]"},
		{"negated word", "-blurry", "terms=[t!(blurry)]"},
		{"quoted phrase", `"red car"`, "terms=[p(red car)]"},
		{"negated phrase", `-"red car"`, "terms=[p!(red car)]"},
		{"escaped dash is literal", `\-dash`, "terms=[t(-dash)]"},
		{"lone dash is literal", "-", "terms=[t(-)]"},
		{"colon after digits stays text", "12:30", "terms=[t(12:30)]"},
		{"fully quoted filter shape stays text", `"title:x"`, "terms=[p(title:x)]"},
		{"escaped colon stays text", `title\:x`, "terms=[t(title:x)]"},
		{"url-ish token reports unknown key", "http://x", "terms=[t(http://x)] unknown=[http://x]"},

		// --- Unknown keys and malformed values degrade + report ---
		{"unknown key", "color:red", "terms=[t(color:red)] unknown=[color:red]"},
		{"unknown key keeps quoted value", `foo:"bar baz"`, "terms=[p(foo:bar baz)] unknown=[foo:\"bar baz\"]"},
		{"empty value", "title:", "terms=[t(title:)] unknown=[title:]"},
		{"empty alternative", "label:cat|", "terms=[t(label:cat|)] unknown=[label:cat|]"},
		{"bare negation value", "label:!", "terms=[t(label:!)] unknown=[label:!]"},
		{"non-numeric number", "iso:abc", "terms=[t(iso:abc)] unknown=[iso:abc]"},
		{"escaped dash breaks range", `iso:100\-400`, "terms=[t(iso:100-400)] unknown=[iso:100\\-400]"},
		{"double dash range", "iso:1-2-3", "terms=[t(iso:1-2-3)] unknown=[iso:1-2-3]"},
		{"dash only range", "iso:-", "terms=[t(iso:-)] unknown=[iso:-]"},
		{"unknown enum word", "type:raw", "terms=[t(type:raw)] unknown=[type:raw]"},
		{"unknown flag word", "flag:maybe", "terms=[t(flag:maybe)] unknown=[flag:maybe]"},
		{"unknown face state", "face:old", "terms=[t(face:old)] unknown=[face:old]"},
		{"bad bool word", "favorite:maybe", "terms=[t(favorite:maybe)] unknown=[favorite:maybe]"},
		{"bad date", "taken:yesterday", "terms=[t(taken:yesterday)] unknown=[taken:yesterday]"},
		{"two-digit year", "year:99", "terms=[t(year:99)] unknown=[year:99]"},
		{"month out of range", "month:13", "terms=[t(month:13)] unknown=[month:13]"},
		{"rating above five", "rating:6", "terms=[t(rating:6)] unknown=[rating:6]"},
		{"fractional rating", "rating:1.5", "terms=[t(rating:1.5)] unknown=[rating:1.5]"},
		{"one bad alternative degrades whole token", "type:image|raw",
			"terms=[t(type:image|raw)] unknown=[type:image|raw]"},
		{"several unknowns keep order", "foo:1 bar:2",
			"terms=[t(foo:1) t(bar:2)] unknown=[foo:1 bar:2]"},

		// --- Text filters ---
		{"title", "title:cat", "filters=[title=text:cat]"},
		{"description quoted", `description:"family trip"`, "filters=[description=text:family trip]"},
		{"notes", "notes:todo", "filters=[notes=text:todo]"},
		{"filename wildcard", "filename:IMG_*", "filters=[filename=text:IMG_*]"},
		{"keywords", "keywords:beach", "filters=[keywords=text:beach]"},
		{"keyword alias", "keyword:beach", "filters=[keywords=text:beach]"},

		// --- Organisation ---
		{"album quoted", `album:"Léto 2024"`, "filters=[album=text:Léto 2024]"},
		{"label", "label:cat", "filters=[label=text:cat]"},
		{"label or", "label:cat|dog", "filters=[label=text:cat|text:dog]"},
		{"label not", "label:!blurry", "filters=[label=!text:blurry]"},
		{"label mixed not", "label:cat|!dog", "filters=[label=text:cat|!text:dog]"},
		{"person", "person:Anna", "filters=[person=text:Anna]"},
		{"subject alias", "subject:Anna", "filters=[person=text:Anna]"},
		{"favorite yes", "favorite:yes", "filters=[favorite=bool:true]"},
		{"favorite true", "favorite:true", "filters=[favorite=bool:true]"},
		{"favorite no", "favorite:no", "filters=[favorite=bool:false]"},
		{"private", "private:no", "filters=[private=bool:false]"},
		{"archived", "archived:yes", "filters=[archived=bool:true]"},
		{"rating exact", "rating:3", "filters=[rating=num:3..3]"},
		{"rating range", "rating:2-5", "filters=[rating=num:2..5]"},
		{"rating open low", "rating:-2", "filters=[rating=num:..2]"},
		{"rating open high", "rating:3-", "filters=[rating=num:3..]"},
		{"flag", "flag:pick", "filters=[flag=text:pick]"},
		{"flag or", "flag:pick|reject", "filters=[flag=text:pick|text:reject]"},
		{"flag eye", "flag:eye", "filters=[flag=text:eye]"},

		// --- Time ---
		{"year", "year:2024", "filters=[year=num:2024..2024]"},
		{"year range", "year:2020-2023", "filters=[year=num:2020..2023]"},
		{"year reversed range normalises", "year:2023-2020", "filters=[year=num:2020..2023]"},
		{"month range", "month:6-8", "filters=[month=num:6..8]"},
		{"day", "day:24", "filters=[day=num:24..24]"},
		{"taken day", "taken:2024-05-01", "filters=[taken=date:2024-05-01..2024-05-02]"},
		{"taken month", "taken:2024-05", "filters=[taken=date:2024-05-01..2024-06-01]"},
		{"taken year", "taken:2024", "filters=[taken=date:2024-01-01..2025-01-01]"},
		{"before", "before:2024", "filters=[before=date:2024-01-01..2025-01-01]"},
		{"after", "after:2024-06-15", "filters=[after=date:2024-06-15..2024-06-16]"},
		{"added", "added:2025-01", "filters=[added=date:2025-01-01..2025-02-01]"},
		{"taken negated", "taken:!2024", "filters=[taken=!date:2024-01-01..2025-01-01]"},

		// --- Place ---
		{"country", "country:Czechia", "filters=[country=text:Czechia]"},
		{"city or", "city:Praha|Brno", "filters=[city=text:Praha|text:Brno]"},
		{"geo yes", "geo:yes", "filters=[geo=bool:true]"},
		{"geo no", "geo:no", "filters=[geo=bool:false]"},
		{"alt range", "alt:300-500", "filters=[alt=num:300..500]"},
		{"alt open", "alt:800-", "filters=[alt=num:800..]"},
		{"near", "near:pht123abc", "filters=[near=text:pht123abc]"},
		{"dist", "dist:5", "filters=[dist=num:5..5]"},
		{"near with dist", "near:pht1 dist:2.5", "filters=[near=text:pht1 dist=num:2.5..2.5]"},

		// --- Camera / optics ---
		{"camera quoted", `camera:"Canon EOS R6"`, "filters=[camera=text:Canon EOS R6]"},
		{"lens", "lens:50mm", "filters=[lens=text:50mm]"},
		{"iso range", "iso:100-400", "filters=[iso=num:100..400]"},
		{"iso open high", "iso:800-", "filters=[iso=num:800..]"},
		{"iso open low", "iso:-200", "filters=[iso=num:..200]"},
		{"aperture range", "f:2.8-4.5", "filters=[f=num:2.8..4.5]"},
		{"aperture single", "f:1.8", "filters=[f=num:1.8..1.8]"},
		{"focal length", "mm:28-35", "filters=[mm=num:28..35]"},
		{"megapixels", "mp:3-6", "filters=[mp=num:3..6]"},

		// --- Media ---
		{"type video", "type:video", "filters=[type=text:video]"},
		{"type or", "type:image|live", "filters=[type=text:image|text:live]"},
		{"type case-insensitive", "type:VIDEO", "filters=[type=text:video]"},
		{"type negated", "type:!video", "filters=[type=!text:video]"},
		{"codec", "codec:hevc", "filters=[codec=text:hevc]"},
		{"portrait", "portrait:yes", "filters=[portrait=bool:true]"},
		{"landscape no", "landscape:no", "filters=[landscape=bool:false]"},
		{"square numeric bool", "square:1", "filters=[square=bool:true]"},
		{"panorama", "panorama:true", "filters=[panorama=bool:true]"},

		// --- Faces ---
		{"faces yes", "faces:yes", "filters=[faces=bool:true]"},
		{"faces no", "faces:no", "filters=[faces=bool:false]"},
		{"faces minimum", "faces:3", "filters=[faces=num:3..]"},
		{"faces range", "faces:2-4", "filters=[faces=num:2..4]"},
		{"face new", "face:new", "filters=[face=text:new]"},

		// --- Operators, quoting, escaping ---
		{"key case-insensitive", "ISO:100", "filters=[iso=num:100..100]"},
		{"camera key mixed case", "Camera:x", "filters=[camera=text:x]"},
		{"escaped pipe is literal", `label:a\|b`, "filters=[label=text:a|b]"},
		{"escaped bang is literal", `title:\!x`, "filters=[title=text:!x]"},
		{"quoted operators are literal", `label:"cat|dog"`, "filters=[label=text:cat|dog]"},
		{"quote ends mid token", `camera:"Canon" EOS`, "terms=[t(EOS)] filters=[camera=text:Canon]"},
		{"unterminated quote closes at end", `camera:"Canon EOS`, "filters=[camera=text:Canon EOS]"},
		{"near negated", "near:!pht1", "filters=[near=!text:pht1]"},
		{"filters and mixes with text", "dovolená iso:100 f:2.8",
			"terms=[t(dovolená)] filters=[iso=num:100..100 f=num:2.8..2.8]"},
		{"headline example", `dovolená camera:"Canon EOS R6" iso:100-400 faces:2`,
			"terms=[t(dovolená)] filters=[camera=text:Canon EOS R6 iso=num:100..400 faces=num:2..]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fmtQuery(Parse(tt.input))
			if got != tt.want {
				t.Errorf("Parse(%q)\n got: %s\nwant: %s", tt.input, got, tt.want)
			}
		})
	}
}

// TestQuery_renderings covers the free-text renderings the API layer feeds to
// the full-text, substring and semantic search paths.
func TestQuery_renderings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		freeText  string
		plainText string
		notTerms  []string
	}{
		{"pure filters render empty", "iso:100-400 label:cat", "", "", nil},
		{"words pass through", "dovolená moře", "dovolená moře", "dovolená moře", nil},
		{"phrase keeps quotes for fts only", `"red car" cat`, `"red car" cat`, "red car cat", nil},
		{"negation renders dash and is dropped from plain", "cat -dog", "cat -dog", "cat", []string{"dog"}},
		{"unknown filter joins free text", "cat foo:bar", "cat foo:bar", "cat foo:bar", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q := Parse(tt.input)
			if got := q.FreeText(); got != tt.freeText {
				t.Errorf("FreeText() = %q, want %q", got, tt.freeText)
			}
			if got := q.PlainText(); got != tt.plainText {
				t.Errorf("PlainText() = %q, want %q", got, tt.plainText)
			}
			got := q.NotTerms()
			if len(got) != len(tt.notTerms) {
				t.Fatalf("NotTerms() = %v, want %v", got, tt.notTerms)
			}
			for i := range got {
				if got[i] != tt.notTerms[i] {
					t.Errorf("NotTerms()[%d] = %q, want %q", i, got[i], tt.notTerms[i])
				}
			}
		})
	}
}

// TestQuery_HasFilter covers the key-presence probe the store uses to decide
// whether the query language already constrains the archive state.
func TestQuery_HasFilter(t *testing.T) {
	t.Parallel()

	q := Parse("archived:yes iso:100")
	if !q.HasFilter(KeyArchived) {
		t.Error("HasFilter(KeyArchived) = false, want true")
	}
	if !q.HasFilter(KeyISO) {
		t.Error("HasFilter(KeyISO) = false, want true")
	}
	if q.HasFilter(KeyLabel) {
		t.Error("HasFilter(KeyLabel) = true, want false")
	}
}
