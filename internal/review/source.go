package review

// Question sources: which of the two searches a batch is drawn from.
//
// The player picks what the game asks about — people, labels, or both — and the
// choice is an input of the rebuild, not a filter on its result. That is the
// whole point: the two candidate searches *are* the cost of a rebuild (a
// subject sweep hydrates a full photo record per match, EXIF blob included), so
// running the face scan for a player who only wants label questions would spend
// the time and the memory on material that is thrown away before it is ever
// shown. Source therefore travels all the way down into collect.
//
// The sources share one session: skips and answers recorded under one selection
// still hold under another, because "don't know" and "no" are statements about
// a question, not about the toggle that surfaced it.

import "fmt"

// Source selects which kinds of question a queue is built from.
type Source string

// The accepted sources.
const (
	// SourceBoth draws on people and labels alike, interleaved — the default,
	// and what the game did before the choice existed.
	SourceBoth Source = "both"
	// SourcePeople asks only face questions ("is this <person>?").
	SourcePeople Source = "people"
	// SourceLabels asks only label questions ("does <label> fit this photo?").
	SourceLabels Source = "labels"
)

// Sources lists the accepted sources in the order the UI offers them.
var Sources = []Source{SourceBoth, SourcePeople, SourceLabels}

// ParseSource maps a request's source query parameter to a Source. An empty
// value defaults to both; any other unrecognised value returns ErrInvalidSource
// so the HTTP layer can answer 400 — quietly sorting the wrong material would
// read as a broken toggle, which is worse than a rejected request.
func ParseSource(raw string) (Source, error) {
	switch Source(raw) {
	case "", SourceBoth:
		return SourceBoth, nil
	case SourcePeople:
		return SourcePeople, nil
	case SourceLabels:
		return SourceLabels, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidSource, raw)
	}
}

// orBoth folds an unset or unknown source into the default. Queue applies it so
// a caller that skipped ParseSource still gets a playable queue rather than an
// empty one that looks like an exhausted library.
func (s Source) orBoth() Source {
	switch s {
	case SourcePeople, SourceLabels, SourceBoth:
		return s
	default:
		return SourceBoth
	}
}

// wantsFaces reports whether the source includes face questions, i.e. whether a
// rebuild has to run the subject sweep at all.
func (s Source) wantsFaces() bool {
	return s.orBoth() != SourceLabels
}

// wantsLabels reports whether the source includes label questions, i.e. whether
// a rebuild has to run the label-similarity searches at all.
func (s Source) wantsLabels() bool {
	return s.orBoth() != SourcePeople
}

// reasonFor explains an empty queue in terms of what the rebuild actually
// scanned. A source the player restricted the game to and that holds nothing is
// a different message from a library with no sources at all ("there is nothing
// left in what you chose" vs "name some people first"), and neither may be
// concluded from a scan the deadline cut short — a degraded rebuild undercounts
// by construction.
func reasonFor(src Source, mat material) string {
	if mat.degraded {
		return ReasonNoCandidates
	}
	switch src.orBoth() {
	case SourcePeople:
		if mat.subjectsTotal == 0 {
			return ReasonNoPeople
		}
	case SourceLabels:
		// A library whose every label has been switched off on the labels page
		// reads as "no labels" here, and that is the honest message: the game has
		// nothing to ask about, and the fix is the toggle, not more photos.
		if mat.labelsTotal == 0 {
			return ReasonNoLabels
		}
	case SourceBoth:
		if mat.subjectsTotal == 0 && mat.labelsTotal == 0 {
			return ReasonNoSources
		}
	}
	return ReasonNoCandidates
}
