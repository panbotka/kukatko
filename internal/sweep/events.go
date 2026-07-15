package sweep

import (
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/people"
)

// EventType tags a streamed sweep message so the client can dispatch on it.
type EventType string

const (
	// EventProgress reports that one more subject has been scanned. It is emitted for
	// every scanned subject, matched or not, so the client can drive a determinate
	// progress bar.
	EventProgress EventType = "progress"
	// EventPerson carries a subject that has at least one actionable candidate. It is
	// emitted only for subjects with matches, so the stream is a work list, not a
	// report.
	EventPerson EventType = "person"
	// EventSummary is the terminal message with the global totals. Exactly one is sent,
	// last, after every subject has been scanned (unless the client disconnects first).
	EventSummary EventType = "summary"
)

// Event is one line of the newline-delimited sweep stream. Exactly one of the
// payload pointers is set, selected by Type; the others are omitted from the JSON.
type Event struct {
	// Type selects which payload below is populated.
	Type EventType `json:"type"`
	// Progress is set when Type is EventProgress.
	Progress *Progress `json:"progress,omitempty"`
	// Person is set when Type is EventPerson.
	Person *Person `json:"person,omitempty"`
	// Summary is set when Type is EventSummary.
	Summary *Summary `json:"summary,omitempty"`
}

// Progress reports the running scan position after a subject has been scanned.
type Progress struct {
	// Scanned is how many subjects have been scanned so far (1-based, monotonic).
	Scanned int `json:"scanned"`
	// Total is how many subjects this sweep will scan (after the max-subjects cap).
	Total int `json:"total"`
	// Name is the display name of the subject just scanned, so the bar can label it.
	Name string `json:"name"`
}

// Person is a subject with at least one actionable candidate, plus its candidate
// faces in the same shape the per-subject candidate endpoint returns.
type Person struct {
	// Subject is the person the candidates resemble.
	Subject people.Subject `json:"subject"`
	// Candidates are the actionable unnamed faces (already-done ones are filtered out),
	// nearest first, exactly as the per-subject search returns them.
	Candidates []candidates.Candidate `json:"candidates"`
	// Counts summarises the actionable candidates per action.
	Counts candidates.Counts `json:"counts"`
	// Actionable is how many candidates still need a human decision (create_marker +
	// assign_person). It equals len(Candidates).
	Actionable int `json:"actionable"`
}

// Summary is the sweep's global tally, emitted once at the end.
type Summary struct {
	// PeopleScanned is how many subjects were scanned.
	PeopleScanned int `json:"people_scanned"`
	// PeopleWithMatches is how many of them had at least one actionable candidate.
	PeopleWithMatches int `json:"people_with_matches"`
	// TotalActionable is the sum of actionable candidates across all people.
	TotalActionable int `json:"total_actionable"`
	// TotalAlreadyDone is how many surfaced faces already belonged to their subject (a
	// rare stale-cache case), summed across every scanned subject.
	TotalAlreadyDone int `json:"total_already_done"`
	// Capped reports that more subjects had faces than MaxSubjects, so the sweep scanned
	// only the first SubjectsTotal-capped worth; the client can say so instead of
	// pretending it covered everyone.
	Capped bool `json:"capped"`
	// SubjectsTotal is how many subjects had faces before the cap was applied.
	SubjectsTotal int `json:"subjects_total"`
}
