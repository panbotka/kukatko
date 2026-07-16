// Package query parses the photo search query language: free text mixed with
// key:value filters in one string, e.g.
//
//	dovolená camera:"Canon EOS R6" iso:100-400 faces:2
//
// The package is pure logic with no I/O: Parse turns the input into an AST
// (free-text terms, recognised filters, unknown filter-shaped tokens) and the
// photos store compiles that AST into parameterised SQL. Parsing never fails —
// anything malformed degrades to free text so a colon in a caption still
// matches — but filter-shaped tokens that were not understood are reported so
// the UI can hint at them.
package query

import (
	"strings"
	"time"
)

// Key identifies a recognised filter of the query language. Its string value
// is the canonical key the user types before the colon.
type Key string

// The recognised filter keys. Aliases (subject: for person:, keyword: for
// keywords:) are resolved during parsing, so a Filter always carries the
// canonical key.
const (
	// KeyTitle matches the photo title (text).
	KeyTitle Key = "title"
	// KeyDescription matches the photo description (text).
	KeyDescription Key = "description"
	// KeyNotes matches the photo notes (text).
	KeyNotes Key = "notes"
	// KeyFilename matches the original file name (text).
	KeyFilename Key = "filename"
	// KeyKeywords matches the verbatim IPTC keywords (text).
	KeyKeywords Key = "keywords"
	// KeyAlbum matches photos in an album by title or UID (text).
	KeyAlbum Key = "album"
	// KeyLabel matches photos carrying a label by name or UID (text).
	KeyLabel Key = "label"
	// KeyPerson matches photos containing a subject by name or UID (text).
	KeyPerson Key = "person"
	// KeyFavorite keeps (or drops) the caller's favorites (yes/no).
	KeyFavorite Key = "favorite"
	// KeyPrivate keeps (or drops) private photos (yes/no).
	KeyPrivate Key = "private"
	// KeyArchived keeps (or drops) archived photos (yes/no); using it lifts
	// the default live-only scope so archived:yes can actually match.
	KeyArchived Key = "archived"
	// KeyRating matches the caller's star rating, 0–5 with ranges (number).
	KeyRating Key = "rating"
	// KeyFlag matches the caller's pick/reject/eye flag (enum).
	KeyFlag Key = "flag"
	// KeyYear matches the capture year, with ranges (number).
	KeyYear Key = "year"
	// KeyMonth matches the capture month 1–12, with ranges (number).
	KeyMonth Key = "month"
	// KeyDay matches the capture day of month 1–31, with ranges (number).
	KeyDay Key = "day"
	// KeyTaken matches a capture date: YYYY, YYYY-MM or YYYY-MM-DD (date).
	KeyTaken Key = "taken"
	// KeyBefore keeps photos taken strictly before the date (date).
	KeyBefore Key = "before"
	// KeyAfter keeps photos taken on or after the date (date).
	KeyAfter Key = "after"
	// KeyAdded matches the date the photo entered the catalogue (date).
	KeyAdded Key = "added"
	// KeyCountry matches the reverse-geocoded country (text).
	KeyCountry Key = "country"
	// KeyCity matches the reverse-geocoded city (text).
	KeyCity Key = "city"
	// KeyGeo keeps photos with (yes) or without (no) GPS coordinates.
	KeyGeo Key = "geo"
	// KeyAlt matches the GPS altitude in metres, with ranges (number).
	KeyAlt Key = "alt"
	// KeyNear keeps photos geographically near the given photo UID.
	KeyNear Key = "near"
	// KeyDist is the km radius for near:; it adds no condition on its own.
	KeyDist Key = "dist"
	// KeyCamera matches the camera make or model (text).
	KeyCamera Key = "camera"
	// KeyLens matches the lens model (text).
	KeyLens Key = "lens"
	// KeyISO matches the ISO sensitivity, with ranges (number).
	KeyISO Key = "iso"
	// KeyAperture matches the f-number, with ranges (number).
	KeyAperture Key = "f"
	// KeyFocalLength matches the focal length in mm, with ranges (number).
	KeyFocalLength Key = "mm"
	// KeyMegapixels matches the resolution in megapixels, with ranges (number).
	KeyMegapixels Key = "mp"
	// KeyType matches the media type: image, video or live (enum).
	KeyType Key = "type"
	// KeyCodec matches the image or video codec (text).
	KeyCodec Key = "codec"
	// KeyPortrait keeps photos taller than wide (yes/no).
	KeyPortrait Key = "portrait"
	// KeyLandscape keeps photos wider than tall (yes/no).
	KeyLandscape Key = "landscape"
	// KeySquare keeps photos with equal sides (yes/no).
	KeySquare Key = "square"
	// KeyPanorama keeps photos at least 1.9× wider than tall (yes/no).
	KeyPanorama Key = "panorama"
	// KeyFaces matches the face count: yes/no, a minimum, or a range.
	KeyFaces Key = "faces"
	// KeyFace matches face states; the only value is new (unassigned face).
	KeyFace Key = "face"
)

// Kind classifies how a filter's value is parsed.
type Kind int

// The value kinds of the query language.
const (
	// KindText matches a string case-insensitively; `*` acts as a wildcard.
	KindText Kind = iota
	// KindNumber accepts a number or a lo-hi range with open ends.
	KindNumber
	// KindDate accepts a calendar value: YYYY, YYYY-MM or YYYY-MM-DD.
	KindDate
	// KindBool accepts yes/no (also true/false and 1/0).
	KindBool
	// KindEnum accepts one word of a fixed set.
	KindEnum
	// KindID accepts an opaque identifier with no wildcard meaning.
	KindID
	// KindCount accepts yes/no or a number/range; a bare number is a minimum.
	KindCount
)

// spec describes how one filter key's value is parsed and validated: its value
// kind, the allowed words for KindEnum, the inclusive numeric bounds and the
// whole-number requirement for KindNumber/KindCount.
type spec struct {
	kind    Kind
	enum    []string
	lo, hi  float64
	integer bool
}

// specs is the filter registry: every canonical key with its value spec.
// Adding a filter means adding a row here, a condition builder in the photos
// store, and a line in the docs/API.md grammar.
var specs = map[Key]spec{
	KeyTitle:       {kind: KindText},
	KeyDescription: {kind: KindText},
	KeyNotes:       {kind: KindText},
	KeyFilename:    {kind: KindText},
	KeyKeywords:    {kind: KindText},
	KeyAlbum:       {kind: KindText},
	KeyLabel:       {kind: KindText},
	KeyPerson:      {kind: KindText},
	KeyFavorite:    {kind: KindBool},
	KeyPrivate:     {kind: KindBool},
	KeyArchived:    {kind: KindBool},
	KeyRating:      {kind: KindNumber, lo: 0, hi: 5, integer: true},
	KeyFlag:        {kind: KindEnum, enum: []string{"pick", "reject", "eye"}},
	KeyYear:        {kind: KindNumber, lo: 1000, hi: 9999, integer: true},
	KeyMonth:       {kind: KindNumber, lo: 1, hi: 12, integer: true},
	KeyDay:         {kind: KindNumber, lo: 1, hi: 31, integer: true},
	KeyTaken:       {kind: KindDate},
	KeyBefore:      {kind: KindDate},
	KeyAfter:       {kind: KindDate},
	KeyAdded:       {kind: KindDate},
	KeyCountry:     {kind: KindText},
	KeyCity:        {kind: KindText},
	KeyGeo:         {kind: KindBool},
	KeyAlt:         {kind: KindNumber, lo: 0, hi: 100000},
	KeyNear:        {kind: KindID},
	KeyDist:        {kind: KindNumber, lo: 0, hi: 20000},
	KeyCamera:      {kind: KindText},
	KeyLens:        {kind: KindText},
	KeyISO:         {kind: KindNumber, lo: 0, hi: 10000000, integer: true},
	KeyAperture:    {kind: KindNumber, lo: 0, hi: 1000},
	KeyFocalLength: {kind: KindNumber, lo: 0, hi: 100000},
	KeyMegapixels:  {kind: KindNumber, lo: 0, hi: 10000},
	KeyType:        {kind: KindEnum, enum: []string{"image", "video", "live"}},
	KeyCodec:       {kind: KindText},
	KeyPortrait:    {kind: KindBool},
	KeyLandscape:   {kind: KindBool},
	KeySquare:      {kind: KindBool},
	KeyPanorama:    {kind: KindBool},
	KeyFaces:       {kind: KindCount, lo: 0, hi: 1000, integer: true},
	KeyFace:        {kind: KindEnum, enum: []string{"new"}},
}

// aliases maps alternative spellings the user may type to their canonical key.
var aliases = map[string]Key{
	"subject": KeyPerson,
	"keyword": KeyKeywords,
}

// lookupKey resolves a lowercased key candidate to its canonical Key,
// reporting whether the key is recognised at all.
func lookupKey(raw string) (Key, bool) {
	if key, ok := aliases[raw]; ok {
		return key, true
	}
	key := Key(raw)
	if _, ok := specs[key]; ok {
		return key, true
	}
	return "", false
}

// Term is one free-text unit of a query: a word or a quoted phrase, possibly
// negated with a leading '-'.
type Term struct {
	// Text is the term's literal text with quotes and escapes resolved.
	Text string
	// Phrase is true when the term was quoted, so full-text search should
	// match it as a phrase rather than independent words.
	Phrase bool
	// Not is true when the term was negated with a leading '-'.
	Not bool
}

// Value is one OR-alternative of a filter's value. Exactly one value shape is
// populated, matching the filter key's Kind: Text for text/enum/id values,
// Bool for yes/no values, Min/Max for numeric ranges (either may be nil for an
// open end), and From/Until for a half-open calendar range.
type Value struct {
	// Not negates the alternative ('!'): the condition must not match.
	Not bool
	// Text is the literal value for KindText (where '*' is a wildcard),
	// KindEnum (canonical lowercased word) and KindID.
	Text string
	// Bool is the parsed yes/no for KindBool, and for KindCount's yes/no form.
	Bool *bool
	// Min is the inclusive numeric lower bound; nil when the range is open.
	Min *float64
	// Max is the inclusive numeric upper bound; nil when the range is open.
	Max *float64
	// From is the inclusive start of a calendar value for KindDate.
	From *time.Time
	// Until is the exclusive end of a calendar value for KindDate.
	Until *time.Time
}

// Filter is one recognised key:value condition. Values holds the OR-ed
// alternatives from '|'; separate Filters combine with AND.
type Filter struct {
	// Key is the canonical filter key.
	Key Key
	// Values are the OR-ed alternatives; always at least one.
	Values []Value
}

// Query is the parsed form of a search input.
type Query struct {
	// Terms is the residual free text with the filters removed.
	Terms []Term
	// Filters are the recognised key:value conditions, in input order.
	Filters []Filter
	// Unknown lists the raw filter-shaped tokens that were not understood
	// (unknown key or malformed value). Each also joined the free text, so it
	// can still match a caption verbatim; this list lets the UI hint at them.
	Unknown []string
}

// FreeText renders the free-text part of the query in the websearch syntax
// PostgreSQL's websearch_to_tsquery understands: phrases quoted, negated terms
// prefixed with '-'. It returns "" for a pure filter query.
func (q Query) FreeText() string {
	parts := make([]string, 0, len(q.Terms))
	for _, t := range q.Terms {
		text := t.Text
		if t.Phrase {
			text = `"` + strings.ReplaceAll(text, `"`, " ") + `"`
		}
		if t.Not {
			text = "-" + text
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " ")
}

// PlainText renders the positive free-text terms as one plain string, the form
// suited to substring matching and to embedding the query for semantic search
// (negations and quoting carry no meaning there).
func (q Query) PlainText() string {
	parts := make([]string, 0, len(q.Terms))
	for _, t := range q.Terms {
		if !t.Not {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, " ")
}

// NotTerms returns the negated free-text terms ('-term'), the exclusions the
// substring search path applies as NOT ILIKE filters.
func (q Query) NotTerms() []string {
	var out []string
	for _, t := range q.Terms {
		if t.Not && strings.TrimSpace(t.Text) != "" {
			out = append(out, t.Text)
		}
	}
	return out
}

// HasFilter reports whether any parsed filter uses the given canonical key.
func (q Query) HasFilter(key Key) bool {
	for _, f := range q.Filters {
		if f.Key == key {
			return true
		}
	}
	return false
}
