package facematch

import (
	"sort"

	"github.com/panbotka/kukatko/internal/vectors"
)

// subjectAggregate accumulates the neighbouring assigned faces of one subject so a
// single suggestion can be derived from their average distance.
type subjectAggregate struct {
	uid     string
	name    string
	sumDist float64
	count   int
}

// aggregateSuggestions turns nearest-neighbour face candidates into ranked subject
// suggestions. It is the pure core of the suggestion logic, kept free of I/O so the
// filtering rules are unit-tested directly.
//
// A candidate is skipped when it is on the query face's own photo (selfPhotoUID),
// is unassigned (no subject), names a subject already in exclude (a person already
// placed on the photo), or — when minFaceSize is positive — is narrower than
// minFaceSize in normalised width. Surviving candidates are grouped by subject; the
// suggestion's distance is the group's average and its confidence is 1 - distance
// clamped to [0, 1]. Suggestions are sorted by descending confidence (ties broken by
// ascending distance then subject uid for determinism) and truncated to limit.
func aggregateSuggestions(
	candidates []vectors.FaceCandidate, selfPhotoUID string,
	exclude map[string]bool, minFaceSize float64, limit int,
) []Suggestion {
	groups := make(map[string]*subjectAggregate)
	for i := range candidates {
		c := candidates[i]
		uid := subjectUIDOf(c)
		if !suggestionEligible(c, uid, selfPhotoUID, exclude, minFaceSize) {
			continue
		}
		agg, ok := groups[uid]
		if !ok {
			agg = &subjectAggregate{uid: uid, name: c.SubjectName}
			groups[uid] = agg
		}
		if agg.name == "" {
			agg.name = c.SubjectName
		}
		agg.sumDist += c.Distance
		agg.count++
	}
	return rankSuggestions(groups, limit)
}

// subjectUIDOf returns the candidate's cached subject uid, or "" when unassigned.
func subjectUIDOf(c vectors.FaceCandidate) string {
	if c.SubjectUID == nil {
		return ""
	}
	return *c.SubjectUID
}

// suggestionEligible reports whether a candidate may contribute a suggestion: it
// must be assigned, on a different photo than the query face, not already placed on
// the photo (exclude), and at least minFaceSize wide when that filter is enabled.
func suggestionEligible(
	c vectors.FaceCandidate, subjectUID, selfPhotoUID string,
	exclude map[string]bool, minFaceSize float64,
) bool {
	if subjectUID == "" || c.PhotoUID == selfPhotoUID || exclude[subjectUID] {
		return false
	}
	if minFaceSize > 0 && c.BBox[2] < minFaceSize {
		return false
	}
	return true
}

// rankSuggestions converts the per-subject aggregates into sorted, truncated
// suggestions (highest confidence first).
func rankSuggestions(groups map[string]*subjectAggregate, limit int) []Suggestion {
	suggestions := make([]Suggestion, 0, len(groups))
	for _, agg := range groups {
		avg := agg.sumDist / float64(agg.count)
		confidence := 1 - avg
		if confidence < 0 {
			confidence = 0
		}
		suggestions = append(suggestions, Suggestion{
			SubjectUID:  agg.uid,
			SubjectName: agg.name,
			Distance:    avg,
			Confidence:  confidence,
		})
	}
	sort.Slice(suggestions, func(i, j int) bool {
		a, b := suggestions[i], suggestions[j]
		if a.Confidence != b.Confidence {
			return a.Confidence > b.Confidence
		}
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		return a.SubjectUID < b.SubjectUID
	})
	if limit > 0 && len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}
	return suggestions
}
