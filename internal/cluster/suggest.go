package cluster

import (
	"sort"

	"github.com/panbotka/kukatko/internal/vectors"
)

// subjectAggregate accumulates a subject's neighbouring assigned faces so a
// single suggestion can be derived from their average distance to the centroid.
type subjectAggregate struct {
	uid     string
	name    string
	sumDist float64
	count   int
}

// bestSubjectSuggestion picks the most likely already-named subject for a cluster
// from its centroid's nearest face candidates. Unassigned candidates are ignored;
// the rest are grouped by subject and each subject's average distance to the
// centroid is taken. The subject with the smallest average distance wins (ties
// broken by uid for determinism), reported with confidence 1 - distance clamped
// to [0, 1]. It returns ok=false when no candidate names a subject.
func bestSubjectSuggestion(candidates []vectors.FaceCandidate) (Suggestion, bool) {
	groups := make(map[string]*subjectAggregate)
	for i := range candidates {
		c := candidates[i]
		if c.SubjectUID == nil || *c.SubjectUID == "" {
			continue
		}
		uid := *c.SubjectUID
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
	return pickBest(groups)
}

// pickBest reduces the per-subject aggregates to the single closest suggestion,
// or ok=false when the map is empty.
func pickBest(groups map[string]*subjectAggregate) (Suggestion, bool) {
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
	if len(suggestions) == 0 {
		return Suggestion{}, false
	}
	sort.Slice(suggestions, func(i, j int) bool {
		if suggestions[i].Distance != suggestions[j].Distance {
			return suggestions[i].Distance < suggestions[j].Distance
		}
		return suggestions[i].SubjectUID < suggestions[j].SubjectUID
	})
	return suggestions[0], true
}
