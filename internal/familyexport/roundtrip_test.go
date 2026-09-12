package familyexport

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fullDocument returns a Document with every single field set to a distinct
// non-zero value.
//
// "Every field" is the point: this is the fixture the round-trip test asserts
// against, and a field left at its zero value here would round-trip vacuously and
// prove nothing. When a field is added to the format, it is added here — and if
// it is forgotten, TestDocument_fixtureIsExhaustive fails.
func fullDocument() Document {
	return Document{
		Version:     Version,
		GeneratedAt: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
		Subjects: []Subject{
			{
				UID:       "sbj000000001",
				Slug:      "bohumil-necas-st",
				Name:      "Bohumil Nečas st.",
				Nickname:  "Bohuš",
				Type:      "person",
				BirthYear: new(1901),
				DeathYear: new(1978),
			},
			{UID: "sbj000000002", Slug: "marie-necasova", Name: "Marie Nečasová", Type: "person"},
			{UID: "sbj000000003", Slug: "bohumil-necas-ml", Name: "Bohumil Nečas ml.", Type: "person"},
		},
		Families: []Family{{
			UID:      "fam000000001",
			Partners: []string{"sbj000000001", "sbj000000002"},
			Kind:     "marriage",
			FromYear: new(1925),
			ToYear:   new(1978),
			Note:     "svatba ve Vavřinci",
			Children: []Child{{SubjectUID: "sbj000000003", Kind: "birth"}},
		}},
	}
}

// TestRoundTrip is the sufficiency test: everything the format holds survives
// being written and read back. It is what a rebuild from storage is entitled to
// rely on.
func TestRoundTrip(t *testing.T) {
	t.Parallel()

	want := fullDocument()

	data, err := Marshal(want)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v\n\nYAML:\n%s", got, want, data)
	}
}

// TestDocument_fixtureIsExhaustive guards the round-trip test's fixture: it walks
// the Document type and fails on any field the fixture left at its zero value.
//
// Without this, adding a field to the format and forgetting to add it to
// fullDocument would leave TestRoundTrip passing while silently no longer
// covering the new field — the exact failure mode that lets an unreadable field
// ship.
func TestDocument_fixtureIsExhaustive(t *testing.T) {
	t.Parallel()

	if zero := zeroFields(reflect.ValueOf(fullDocument()), "Document"); len(zero) > 0 {
		t.Errorf("fullDocument leaves these fields zero, so TestRoundTrip does not cover them: %v\n"+
			"Add them to the fixture.", zero)
	}
}

// zeroFields returns the paths of every field at or under v that holds its type's
// zero value, recursing through structs, pointers and the first element of each
// slice. path names v for the report.
func zeroFields(v reflect.Value, path string) []string {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return []string{path}
		}
		return zeroFields(v.Elem(), path)
	case reflect.Slice:
		if v.Len() == 0 {
			return []string{path}
		}
		return zeroFields(v.Index(0), path+"[0]")
	case reflect.Struct:
		return zeroStructFields(v, path)
	default:
		if v.IsZero() {
			return []string{path}
		}
		return nil
	}
}

// zeroStructFields returns the zero-valued field paths of the struct v. time.Time
// is treated as a leaf rather than recursed into, since its fields are
// unexported.
func zeroStructFields(v reflect.Value, path string) []string {
	if v.Type() == reflect.TypeFor[time.Time]() {
		if v.IsZero() {
			return []string{path}
		}
		return nil
	}
	var out []string
	for i := range v.NumField() {
		field := v.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		out = append(out, zeroFields(v.Field(i), path+"."+field.Name)...)
	}
	return out
}

// TestDocument_isClosedOverItsSubjects pins the property that makes the file
// worth writing at all: every uid a family names — a partner or a child — is
// described in the document's own subject list. A tree pointing at people the
// file does not contain is a tree nobody can rebuild.
func TestDocument_isClosedOverItsSubjects(t *testing.T) {
	t.Parallel()

	doc := fullDocument()
	known := make(map[string]bool, len(doc.Subjects))
	for _, subj := range doc.Subjects {
		known[subj.UID] = true
	}
	for _, fam := range doc.Families {
		for _, uid := range fam.Partners {
			if !known[uid] {
				t.Errorf("family %s names partner %s, who is in no subject entry", fam.UID, uid)
			}
		}
		for _, child := range fam.Children {
			if !known[child.SubjectUID] {
				t.Errorf("family %s names child %s, who is in no subject entry", fam.UID, child.SubjectUID)
			}
		}
	}
}

// TestMarshal_headerIsPresentAndParseable asserts the file carries the
// explanatory header and that the header does not break parsing. The header is
// what tells whoever finds this file cold what it is and where the rest of the
// library's meaning lives.
func TestMarshal_headerIsPresentAndParseable(t *testing.T) {
	t.Parallel()

	data, err := Marshal(fullDocument())
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	text := string(data)
	for _, want := range []string{"# Kukátko family tree.", "sidecars/", "RESTORE.md"} {
		if !strings.Contains(text, want) {
			t.Errorf("header does not mention %q; file:\n%s", want, text)
		}
	}
	if !strings.HasPrefix(text, "#") {
		t.Error("file does not start with the header comment")
	}
	if _, err := Unmarshal(data); err != nil {
		t.Errorf("header broke parsing: %v", err)
	}
}

// TestVersion_isWritten asserts the schema version is present and first, so a
// reader can dispatch on it before parsing anything else.
func TestVersion_isWritten(t *testing.T) {
	t.Parallel()

	data, err := Marshal(fullDocument())
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	body := body(t, data)
	if !strings.HasPrefix(body, fmt.Sprintf("version: %d\n", Version)) {
		t.Errorf("document does not start with the schema version; body starts:\n%.80s", body)
	}
}

// TestMarshal_emptyGenealogyStaysShort asserts a library nobody has recorded a
// relation in yields a short document rather than a wall of empty keys. The file
// is still written — that is what says "there is no tree", instead of leaving
// yesterday's tree standing.
func TestMarshal_emptyGenealogyStaysShort(t *testing.T) {
	t.Parallel()

	data, err := Marshal(Document{
		Version:     Version,
		GeneratedAt: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	for _, absent := range []string{"subjects:", "families:"} {
		if strings.Contains(body(t, data), absent) {
			t.Errorf("empty document contains %q:\n%s", absent, body(t, data))
		}
	}
}

// TestUnmarshal_rejectsGarbage asserts a file that is not YAML is an error rather
// than an empty document, so a corrupt export is noticed rather than read as "a
// library with no families".
func TestUnmarshal_rejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := Unmarshal([]byte("\tthis: is: not: yaml: at: all\n  - [")); err == nil {
		t.Error("Unmarshal accepted garbage, want an error")
	}
}

// body returns the document without its header comment, so a test can assert on
// the YAML alone.
func body(t *testing.T, data []byte) string {
	t.Helper()

	text := string(data)
	i := strings.Index(text, "version:")
	if i < 0 {
		t.Fatalf("document has no version key:\n%s", text)
	}
	return text[i:]
}
