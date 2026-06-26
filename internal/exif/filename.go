package exif

import (
	"path/filepath"
	"regexp"
	"time"
)

// filenameDatePatterns lists the regexps tried, in order, against a file's base
// name to recover a capture time. The first match wins. Each pattern captures
// year/month/day and, where present, hour/minute/second named groups so a
// single parser can consume them all.
var filenameDatePatterns = []*regexp.Regexp{
	// IMG_20230115_143052, VID_20230115-143052, 20230115_143052, etc.
	regexp.MustCompile(`(?P<y>\d{4})(?P<mo>\d{2})(?P<d>\d{2})[ _\-T]?(?P<h>\d{2})(?P<mi>\d{2})(?P<s>\d{2})`),
	// 2023-01-15 14.30.52, 2023_01_15-14_30_52, Screenshot 2023-01-15 14-30-52.
	regexp.MustCompile(`(?P<y>\d{4})[-_.](?P<mo>\d{2})[-_.](?P<d>\d{2})[ _\-T]+` +
		`(?P<h>\d{2})[-_.:](?P<mi>\d{2})[-_.:](?P<s>\d{2})`),
	// Date only: 20230115 or 2023-01-15 (time defaults to midnight).
	regexp.MustCompile(`(?P<y>\d{4})[-_.]?(?P<mo>\d{2})[-_.]?(?P<d>\d{2})`),
}

// parseFilenameDate attempts to recover a capture time from path's base name,
// trying each known naming convention in turn. It returns the parsed time in
// UTC and true on success, or the zero time and false when no pattern matches or
// the matched components do not form a valid calendar date.
func parseFilenameDate(path string) (time.Time, bool) {
	name := filepath.Base(path)
	for _, re := range filenameDatePatterns {
		match := re.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		if when, ok := timeFromGroups(re, match); ok {
			return when, true
		}
	}
	return time.Time{}, false
}

// timeFromGroups builds a UTC time from a regexp match's named capture groups,
// defaulting hour/minute/second to zero when the pattern did not capture them.
// It returns false when the captured numbers do not round-trip to the same
// calendar date (e.g. month 13 or day 32), rejecting false positives.
func timeFromGroups(re *regexp.Regexp, match []string) (time.Time, bool) {
	g := namedGroups(re, match)
	year, month, day := g["y"], g["mo"], g["d"]
	hour, minute, second := g["h"], g["mi"], g["s"]
	if year == 0 || month == 0 || day == 0 {
		return time.Time{}, false
	}
	when := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
	if when.Year() != year || int(when.Month()) != month || when.Day() != day {
		return time.Time{}, false
	}
	return when, true
}

// namedGroups maps each named capture group of re to its integer value for the
// given match, treating absent or non-numeric groups as zero. Group names that
// are not present in the pattern simply never appear in the result.
func namedGroups(re *regexp.Regexp, match []string) map[string]int {
	out := make(map[string]int, len(match))
	for i, name := range re.SubexpNames() {
		if name == "" || i >= len(match) {
			continue
		}
		out[name] = atoiSafe(match[i])
	}
	return out
}

// atoiSafe parses a base-10 integer from s, returning 0 for empty or malformed
// input. The filename patterns only capture fixed-width digit runs, so this is
// just a defensive convenience over strconv.Atoi.
func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
