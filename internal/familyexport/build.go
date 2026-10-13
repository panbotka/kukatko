package familyexport

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/panbotka/kukatko/internal/family"
)

// Input is everything Build needs. The caller reads it; this package does no I/O
// of its own, which is what keeps Build a pure function and the format testable
// without a database.
type Input struct {
	// Project names the project the genealogy belongs to; it is written as the
	// document's project block.
	Project Project
	// Export is the project's genealogy as internal/family read it.
	Export family.Export
	// Now is the generation timestamp written into the document. A zero value
	// reads the wall clock; tests pin it.
	Now time.Time
}

// Build assembles in into a Document. It is pure: same input, same output, no
// I/O, no clock unless Now is zero.
//
// The children of each family are folded into the family they belong to, so the
// file reads as a list of households rather than as two tables a human has to
// join by eye. Everything is ordered — families and subjects by uid, children
// within a family by uid — so an unchanged genealogy marshals to identical bytes
// and a diff of two exports shows only what actually changed.
func Build(in Input) Document {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	return Document{
		Version:     Version,
		GeneratedAt: now.UTC(),
		Project:     in.Project,
		Subjects:    subjectsFrom(in.Export.Subjects),
		Families:    familiesFrom(in.Export.Families, in.Export.Children),
	}
}

// subjectsFrom converts the exported subjects into document order.
func subjectsFrom(in []family.ExportSubject) []Subject {
	if len(in) == 0 {
		return nil
	}
	out := make([]Subject, 0, len(in))
	for _, subj := range in {
		out = append(out, Subject{
			UID:       subj.UID,
			Slug:      subj.Slug,
			Name:      subj.Name,
			Nickname:  subj.Nickname,
			Type:      subj.Type,
			BirthYear: subj.BirthYear,
			DeathYear: subj.DeathYear,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out
}

// familiesFrom converts the exported families, attaching each family's children
// to it. A membership naming a family that is not in the list is dropped rather
// than kept as an orphan: the file must not describe a child of nobody.
func familiesFrom(families []family.Family, children []family.ChildMembership) []Family {
	if len(families) == 0 {
		return nil
	}
	byFamily := childrenByFamily(children)
	out := make([]Family, 0, len(families))
	for _, fam := range families {
		out = append(out, Family{
			UID:      fam.UID,
			Partners: partnersOf(fam),
			Kind:     string(fam.Kind),
			FromYear: fam.FromYear,
			ToYear:   fam.ToYear,
			Note:     fam.Note,
			Children: byFamily[fam.UID],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out
}

// childrenByFamily groups the memberships by their family, each group ordered by
// the child's uid.
func childrenByFamily(children []family.ChildMembership) map[string][]Child {
	byFamily := make(map[string][]Child, len(children))
	for _, membership := range children {
		byFamily[membership.FamilyUID] = append(byFamily[membership.FamilyUID], Child{
			SubjectUID: membership.ChildUID,
			Kind:       string(membership.Kind),
		})
	}
	for _, group := range byFamily {
		sort.Slice(group, func(i, j int) bool { return group[i].SubjectUID < group[j].SubjectUID })
	}
	return byFamily
}

// partnersOf returns the family's recorded partners: two uids for a couple, one
// for a lone parent, none for a sibling group. The nil column of a lone parent is
// dropped rather than written as an empty string, so the list means "the people
// in this union" and nothing has to be filtered by a reader.
func partnersOf(fam family.Family) []string {
	out := make([]string, 0, 2)
	for _, uid := range []*string{fam.PartnerA, fam.PartnerB} {
		if uid != nil && *uid != "" {
			out = append(out, *uid)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// header is prepended to every export. It is addressed to whoever opens the file
// cold — quite possibly on the worst day of their sysadmin life — and it says
// what the file is for and what it is not.
const header = `# Kukátko family tree.
#
# This file holds one project's whole genealogy — the project named under
# "project" below: every person who appears in a family, and every family — a
# couple, a lone parent, or nobody at all, plus their children — tying them
# together. Siblings, half-siblings and second marriages are not stored: they
# are read off these families, which is what stops them from contradicting each
# other. A family with no partners is a sibling group: people known to be brother
# and sister whose parents the library never heard of.
#
# It sits at the root of the project's storage, beside its originals and the
# sidecars/ tree, so the tree can be rebuilt from the storage alone — no
# database. A rebuild refuses a file whose project is not the one it restores.
# It is generated: Kukátko rewrites it whenever a relation changes, so edits
# made here are overwritten.
#
# NOT in this file, deliberately: which photos these people appear on, and their
# faces. Those are per-photo facts and they travel in each photo's own sidecar
# under sidecars/, keyed by the same subject uid you will find below.
#
# Format: docs/RESTORE.md. Schema version is the version key below; a reader that
# does not know a version should refuse the file rather than guess.
`

// Marshal renders doc as the bytes of the export file: the explanatory header
// followed by the YAML document.
//
// The header is a YAML comment, so a parser ignores it and Unmarshal round-trips
// the result — the header is for the human who finds the file, not the machine.
func Marshal(doc Document) ([]byte, error) {
	body, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("familyexport: marshaling document: %w", err)
	}
	return append([]byte(header), body...), nil
}

// Unmarshal parses the bytes of an export file back into a Document, ignoring
// the header comment.
//
// It exists to prove the format is sufficient — the round-trip test is what pins
// that, and it is what a future rebuild is built against, together with
// CheckProject. This package does not otherwise read the file.
func Unmarshal(data []byte) (Document, error) {
	var doc Document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Document{}, fmt.Errorf("familyexport: unmarshaling document: %w", err)
	}
	return doc, nil
}

// ErrUnsupportedVersion indicates a document whose schema version this package
// does not know. A reader refuses it rather than guessing at what it means.
var ErrUnsupportedVersion = errors.New("familyexport: unsupported document version")

// ErrProjectMismatch indicates a document that belongs to another project than
// the one it is being read into.
var ErrProjectMismatch = errors.New("familyexport: the document belongs to another project")

// CheckProject is the guard every reader of an export runs before it rebuilds
// anything from doc: the document must be of the current Version and belong to
// the project projectUID. It returns ErrUnsupportedVersion for any other version
// — a version 1 file names no project, so it cannot prove whose tree it is — and
// ErrProjectMismatch when the document's project is not projectUID, which is
// what stops one project's tree from being restored into another's library.
func CheckProject(doc Document, projectUID string) error {
	if doc.Version != Version {
		return fmt.Errorf("%w: %d (this build reads %d)", ErrUnsupportedVersion, doc.Version, Version)
	}
	if projectUID == "" || doc.Project.UID != projectUID {
		return fmt.Errorf("%w: document of %q (%s), target %q",
			ErrProjectMismatch, doc.Project.UID, doc.Project.Slug, projectUID)
	}
	return nil
}
