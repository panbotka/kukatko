// Package stacks groups the several files of one shot — a RAW next to its JPEG,
// an exported edit next to the original — into a single library item called a
// stack, with one visible "primary" member and the rest hidden from the default
// views. It never merges rows: grouping is pure, reversible bookkeeping on the
// photos.stack_uid / stack_primary columns (see migration 0030), so stacking and
// unstacking lose nothing. This file holds the pure detection rules; the Service
// (service.go) applies them and drives the manual stacking operations.
package stacks

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/panbotka/kukatko/internal/photos"
)

// RuleSet selects which detection rules run. Each rule has a very different
// false-positive rate, so each is switched independently in config. A wrongly
// stacked photo is invisible, so every rule only ever links photos that
// plausibly are the same shot; when in doubt they do not link.
type RuleSet struct {
	// BaseName links files that share a base filename but differ in extension
	// (IMG_1234.CR2 + IMG_1234.jpg). The safest rule.
	BaseName bool
	// SequentialCopy links copy/edit derivatives to their original by canonical
	// name (IMG_1234 (2).jpg, IMG_1234 copy.jpg, IMG_1234-edited.jpg → IMG_1234).
	SequentialCopy bool
	// UniqueID links files carrying the same EXIF ImageUniqueID or XMP InstanceID.
	// Very reliable where the identifier exists.
	UniqueID bool
	// TimeGPS links files captured in the same second at the same GPS point. The
	// loosest rule — it will wrongly stack burst shots — so it is off by default.
	TimeGPS bool
}

// Any reports whether at least one rule is enabled.
func (r RuleSet) Any() bool {
	return r.BaseName || r.SequentialCopy || r.UniqueID || r.TimeGPS
}

// Group partitions candidates into stacks by the enabled rules. Each rule links
// candidates it judges to be the same shot; the connected components of at least
// two candidates become stacks (returned as slices of indices into candidates).
// Candidates no rule links stay standalone and are not returned. The grouping is
// deterministic for a fixed input order, so a re-run reproduces the same stacks.
func Group(candidates []photos.StackCandidate, rules RuleSet) [][]int {
	uf := newUnionFind(len(candidates))
	if rules.BaseName {
		linkByKey(uf, candidates, baseNameKey)
	}
	if rules.SequentialCopy {
		linkByKey(uf, candidates, canonicalNameKey)
	}
	if rules.UniqueID {
		linkByKey(uf, candidates, uniqueIDKey)
	}
	if rules.TimeGPS {
		linkByKey(uf, candidates, timeGPSKey)
	}
	return components(uf, len(candidates))
}

// linkByKey unions every pair of candidates that share the same non-empty key
// produced by key. Candidates whose key is empty (the rule does not apply to
// them) are left unlinked.
func linkByKey(uf *unionFind, candidates []photos.StackCandidate, key func(photos.StackCandidate) string) {
	buckets := make(map[string]int, len(candidates))
	for i, c := range candidates {
		k := key(c)
		if k == "" {
			continue
		}
		if first, ok := buckets[k]; ok {
			uf.union(first, i)
		} else {
			buckets[k] = i
		}
	}
}

// copyMarkerRE matches a trailing copy/sequence/edit marker on a base filename:
// " (2)", " copy", " copy 3", "-edited", "_edit". It is anchored to the end and
// applied repeatedly so "IMG_1234 copy 2" strips down to "IMG_1234".
var copyMarkerRE = regexp.MustCompile(`(?i)(?:\s*\(\d+\)|[\s_-]*cop(?:y|ie)(?:\s*\d+)?|[\s_-]*edit(?:ed)?)$`)

// nameForKey returns the photo's base filename (its stored file name, or the
// original name when the stored one is empty), lowercased and stripped of its
// directory and extension. It is the common stem the name rules key on.
func nameForKey(c photos.StackCandidate) string {
	name := c.FileName
	if name == "" {
		name = c.OriginalName
	}
	base := filepath.Base(name)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return strings.ToLower(strings.TrimSpace(base))
}

// baseNameKey is rule 1's key: the bare base name, so IMG_1234.CR2 and
// IMG_1234.jpg (same stem, different extension) share it while IMG_1234.jpg and
// IMG_12345.jpg do not. Empty when the photo has no usable name.
func baseNameKey(c photos.StackCandidate) string {
	return nameForKey(c)
}

// canonicalNameKey is rule 2's key: the base name with any trailing copy,
// sequence or edit marker removed, so a derivative collapses onto its original
// (IMG_1234 copy.jpg and IMG_1234.jpg share it). Empty when nothing but a marker
// remains ("copy.jpg"), to avoid grouping every stray copy together.
func canonicalNameKey(c photos.StackCandidate) string {
	base := nameForKey(c)
	for {
		stripped := strings.TrimSpace(copyMarkerRE.ReplaceAllString(base, ""))
		if stripped == base {
			break
		}
		base = stripped
	}
	return base
}

// uniqueIDKey is rule 3's key: the EXIF ImageUniqueID / XMP InstanceID, empty
// when the photo carries neither.
func uniqueIDKey(c photos.StackCandidate) string {
	return c.UniqueID
}

// timeGPSKey is rule 4's key: the capture second together with the GPS point
// rounded to ~1 m. Empty unless the photo has both a capture time and GPS, so
// undated or location-less photos are never grouped by this rule.
func timeGPSKey(c photos.StackCandidate) string {
	if c.TakenAt == nil || c.Lat == nil || c.Lng == nil {
		return ""
	}
	return fmt.Sprintf("%d|%.5f|%.5f", c.TakenAt.UTC().Unix(), *c.Lat, *c.Lng)
}
