package vectors

import "math"

// NearestDistance returns the smallest cosine distance from v to any vector in set,
// or +Inf when set is empty (nothing to be near). It is the primitive the
// negative-exemplar rule uses to compare a candidate's nearest accepted exemplar
// against its nearest rejected one. Vectors of unequal length compare over their
// common prefix, matching CosineDistance.
func NearestDistance(v []float32, set [][]float32) float64 {
	nearest := math.Inf(1)
	for _, exemplar := range set {
		if d := CosineDistance(v, exemplar); d < nearest {
			nearest = d
		}
	}
	return nearest
}

// IsNegativeExemplar implements the negative-exemplar rule that makes a rejection
// teach something rather than just hide one row: a candidate is a negative — and
// must be dropped from a subject's (or label's) candidate list — when it is closer
// to one of the rejected exemplars than to its nearest accepted exemplar. accepted
// are the vectors already assigned to the subject / carrying the label; rejected are
// the vectors the user has turned down for it.
//
// It is a nearest-neighbour margin test: no training, no learned weights, and cheap
// because the vectors are already at hand. When rejected is empty the rule is a
// no-op — it returns false immediately, at O(1) cost, computing no distances — so a
// subject or label with no rejections pays nothing. A tie (the nearest accepted and
// nearest rejected exemplar are exactly equidistant) is NOT a negative: the
// candidate must be strictly closer to a rejection to be dropped, which keeps the
// decision deterministic and never discards a candidate the accepted set matches
// just as well. With accepted empty but rejected non-empty, the accepted distance is
// +Inf, so any candidate near a rejection is a negative.
func IsNegativeExemplar(candidate []float32, accepted, rejected [][]float32) bool {
	if len(rejected) == 0 {
		return false
	}
	return NearestDistance(candidate, rejected) < NearestDistance(candidate, accepted)
}
