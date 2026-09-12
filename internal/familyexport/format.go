// Package familyexport writes the library's genealogy — who is whose parent,
// whose partner, whose child — to a single YAML file at the root of the store,
// so the family tree survives losing the database.
//
// It is the subject-level counterpart of internal/sidecarexport. A photo's
// sidecar can hold everything that is true about one photo, and a relationship
// is true about two people rather than about any photograph: it belongs to no
// photo's file, and a great-grandmother nobody ever photographed has no file to
// belong to at all. Left in Postgres alone, the tree is the one thing a user
// builds in Kukátko that the "yours to keep" promise does not cover — lose the
// database, lose the tree.
//
// So the whole genealogy travels as one document, families.yaml, beside the
// originals. It is small (a family archive holds tens of families, not
// millions), it is rewritten whole rather than patched, and it is closed over
// its own identifiers: every subject a family names is described in the same
// file, because a tree that points at people the file does not contain is not a
// tree anybody can rebuild.
//
// This package is the export half — the format, and putting it in the store.
// Reading it back and re-creating the rows is a separate concern and
// deliberately not here; the format is designed to be sufficient for it, which is
// what TestRoundTrip pins. internal/familyexportjob knows *when* to write.
//
// The format is documented in full in docs/RESTORE.md — the document someone
// reads when the database is gone.
package familyexport

import (
	"time"
)

// Version is the schema version, written as the document's first key. It is
// bumped when the format changes in a way a reader must know about; a reader
// that meets a version it does not understand should refuse the file rather than
// guess at it.
//
// Version 1 is the initial format.
const Version = 1

// Key is the storage key of the export: a single object at the root of the
// store, next to the YYYY/ directories of the originals.
//
// One file at the root rather than a prefix of many, because the genealogy is
// one graph and is only ever meaningful whole: a reader that found half of the
// families would build half a tree and not know it. Its own key (rather than a
// place inside sidecars/) is what gives it its own storekeys.Kind, so backup,
// storage migration and the library wipe each have to say what they do with it.
const Key = "families.yaml"

// MIME is the media type the export is stored as.
const MIME = "application/yaml"

// Document is the whole genealogy: every person who appears in a family, and
// every family tying them together.
//
// What is deliberately NOT in here: which photos anybody appears on, their
// faces, and everything else the photo sidecars already carry. This file answers
// one question — who is related to whom — and a rebuild reads it alongside them.
type Document struct {
	// Version is the schema version — see Version. It is first so a reader can
	// dispatch on it before parsing the rest.
	Version int `yaml:"version"`
	// GeneratedAt is when this file was written. It is provenance, not a fact
	// about anybody: it says how current the file is, which is the first thing
	// someone rebuilding from it wants to know.
	GeneratedAt time.Time `yaml:"generated_at"`

	// Subjects is every person named anywhere below, ordered by uid. The list is
	// closed: a uid appearing in a family always appears here too.
	Subjects []Subject `yaml:"subjects,omitempty"`
	// Families is every family — a couple or a lone parent plus their children —
	// ordered by uid.
	Families []Family `yaml:"families,omitempty"`
}

// Subject is one person in the tree, in the shape a rebuild re-creates the
// subject row from.
//
// Name and the life years are the point: a person's name is a fact only the
// database holds, and the years are what an archive knows about a person instead
// of a date of birth. UID and Slug travel so a rebuild can reconnect the tree to
// the people the photo sidecars already named — the sidecars carry a subject's
// uid on every marker.
type Subject struct {
	// UID is the subject's Kukátko UID, the identifier every family below refers
	// to them by.
	UID string `yaml:"uid"`
	// Slug is the subject's URL name, unique in the library.
	Slug string `yaml:"slug"`
	// Name is what the subject is called, and Nickname what people actually call
	// them, empty when nobody recorded one.
	Name     string `yaml:"name"`
	Nickname string `yaml:"nickname,omitempty"`
	// Type is person, pet or other.
	Type string `yaml:"type,omitempty"`
	// BirthYear and DeathYear are the life span, omitted when unknown — which in a
	// village archive is the normal case.
	BirthYear *int `yaml:"birth_year,omitempty"`
	DeathYear *int `yaml:"death_year,omitempty"`
}

// Family is one couple (or lone parent) plus the children who belong to them.
//
// The family is the node of this model, not the edge: siblings, half-siblings
// and second marriages are all read off these rows rather than stored, which is
// what stops them from ever contradicting each other. See migration 0073 for the
// reasoning in full.
type Family struct {
	// UID is the family's Kukátko UID.
	UID string `yaml:"uid"`
	// Partners names the two sides of the union — one uid for a lone parent, two
	// for a couple, and never none. It is a list rather than a pair of keys
	// because the two carry no roles: which of them is the mother is not
	// something this file claims to know.
	Partners []string `yaml:"partners"`
	// Kind is marriage, partnership or unknown.
	Kind string `yaml:"kind,omitempty"`
	// FromYear and ToYear bound the union, omitted when unknown.
	FromYear *int `yaml:"from_year,omitempty"`
	ToYear   *int `yaml:"to_year,omitempty"`
	// Note is whatever a human wrote about the union.
	Note string `yaml:"note,omitempty"`
	// Children are the family's children, ordered by uid. A childless union is a
	// legitimate family with an empty list: a marriage nobody has children from
	// is still the box a tree draws.
	Children []Child `yaml:"children,omitempty"`
}

// Child is one child's membership of a family.
type Child struct {
	// SubjectUID is the child, described in the document's Subjects list.
	SubjectUID string `yaml:"subject_uid"`
	// Kind is how they belong: birth, adopted or step.
	Kind string `yaml:"kind,omitempty"`
}
