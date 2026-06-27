package ppimport

import "time"

// firstNonEmpty returns the first non-empty string among its arguments, or "".
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstPositive returns the first strictly positive integer among its arguments,
// or 0 when none is positive.
func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

// firstIntPtr returns a pointer to ppValue when it is positive, otherwise the
// fallback pointer (which may itself be nil).
func firstIntPtr(ppValue int, fallback *int) *int {
	if ppValue > 0 {
		v := ppValue
		return &v
	}
	return fallback
}

// firstFloatPtr returns a pointer to ppValue when it is strictly positive,
// otherwise the fallback pointer (which may itself be nil).
func firstFloatPtr(ppValue float64, fallback *float64) *float64 {
	if ppValue > 0 {
		v := ppValue
		return &v
	}
	return fallback
}

// timeEqual reports whether two optional times are both nil or equal instants.
func timeEqual(a, b *time.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Equal(*b)
	}
}

// floatEqual reports whether two optional floats are both nil or equal values.
func floatEqual(a, b *float64) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}
