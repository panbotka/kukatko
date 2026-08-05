// Package dupmarkers finds the photos where one and the same person carries more
// than one valid face marker — "Marie is tagged three times on this group shot".
//
// It is always a mistake, and a specific one: the matcher put the same name on
// two or three neighbouring boxes of a group photo, so the people beside her lost
// their tag and her own face count is inflated. Nothing here fixes that on its
// own. The package only *finds* the groups; every repair goes through a write
// path that already exists (unassigning a marker, flagging one invalid, or
// recording the "leave it be" opinion in internal/feedback), so this package
// never mutates. That is the same division of labour as internal/outliers and
// internal/duplicates.
//
// It counts MARKERS, not faces. The two look identical in the UI and have
// different causes: several detected faces matched onto one marker is a
// face↔marker pairing bug (internal/facematch), while several markers of one
// person on one photo is a tagging mistake. Counting faces here would mix the two
// and the numbers would not fall when either is fixed.
//
// The nameless catch-all subject is excluded — a subject whose name is the empty
// string. It exists precisely to hold thousands of untagged regions, so counting
// it would bury the real findings under every photo in the library.
package dupmarkers

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/panbotka/kukatko/internal/feedback"
)

const (
	// defaultLimit is the page size used when a caller requests a non-positive
	// limit.
	defaultLimit = 50
	// maxLimit caps how many groups a single page may return.
	maxLimit = 200
	// minGroupSize is how many valid markers of one person on one photo make a
	// finding. One is the normal, correct case; two is already a mistake.
	minGroupSize = 2
	// MarkerTypeFace is the marker type this package considers. Label markers are
	// hand-drawn regions, not identity claims, so several of them are no finding.
	MarkerTypeFace = "face"
)

// MarkerRow is one candidate marker as loaded from the catalogue: the marker
// itself plus the photo and subject it belongs to, flattened so the grouping is a
// pure function of a row slice.
type MarkerRow struct {
	// MarkerUID identifies the marker; it is what a repair action addresses.
	MarkerUID string
	// PhotoUID and SubjectUID are the pair the rows are grouped by.
	PhotoUID   string
	SubjectUID string
	// SubjectName is the person's display name; an empty name is the nameless
	// catch-all subject and is never a finding.
	SubjectName string
	// Type is the marker type; only MarkerTypeFace is considered.
	Type string
	// Invalid marks a region a user already rejected. Such a marker is not a face
	// and does not count toward the group.
	Invalid bool
	// Reviewed marks a marker a user confirmed, shown as a hint on the card.
	Reviewed bool
	// X, Y, W, H are the normalised bounding box in 0..1 display space.
	X, Y, W, H float64
	// Score is the detector/matcher confidence as an integer percentage.
	Score int
	// PhotoTitle, TakenAt, Width, Height and Orientation are the photo's display
	// hints: the title identifies the photo, the dimensions and the raw EXIF
	// orientation tag let a client crop the box out of a `fit_*` thumbnail.
	PhotoTitle  string
	TakenAt     *time.Time
	Width       int
	Height      int
	Orientation int
}

// Marker is one marker of a group as returned to a client.
type Marker struct {
	UID string `json:"uid"`
	// BBox is the normalised bounding box [x, y, w, h] in 0..1 display space.
	BBox [4]float64 `json:"bbox"`
	// Score is the detector/matcher confidence as an integer percentage. It is
	// import provenance as much as quality (0 means "never recorded"), so it is
	// reported but never used to rank.
	Score int `json:"score"`
	// Reviewed reports whether a user already confirmed this marker.
	Reviewed bool `json:"reviewed"`
}

// Group is one finding: a person marked more than once on one photo, with every
// one of those markers.
type Group struct {
	// PhotoUID identifies the photo the markers sit on.
	PhotoUID string `json:"photo_uid"`
	// PhotoTitle and TakenAt identify the photo for a human.
	PhotoTitle string     `json:"photo_title"`
	TakenAt    *time.Time `json:"taken_at,omitempty"`
	// Width, Height and Orientation are the photo's stored (pre-rotation) pixel
	// dimensions and its raw EXIF orientation tag, the frame a normalised bbox is
	// measured against once the rotation is applied.
	Width       int `json:"width"`
	Height      int `json:"height"`
	Orientation int `json:"orientation"`
	// SubjectUID and SubjectName identify the over-tagged person.
	SubjectUID  string `json:"subject_uid"`
	SubjectName string `json:"subject_name"`
	// Markers are the person's markers on this photo, ordered left to right, so
	// the numbering a client draws over the preview reads in reading order.
	Markers []Marker `json:"markers"`
}

// Result is one page of findings plus the pagination cursor.
type Result struct {
	Groups []Group `json:"groups"`
	// Total is how many groups exist across the whole library, not just this page.
	Total      int  `json:"total"`
	Limit      int  `json:"limit"`
	Offset     int  `json:"offset"`
	NextOffset *int `json:"next_offset"`
}

// MarkerSource loads the candidate marker rows. It is an interface so the service
// depends on the behaviour rather than on pgx, and so the grouping can be
// exercised without a database; *Store satisfies it.
//
// An implementation MAY narrow the rows it returns (the SQL one does, so Postgres
// need not ship the whole catalogue for a page of a few dozen findings), but it
// does not have to: every rule that decides what counts as a finding is applied
// again by the service, over the rows it is given.
type MarkerSource interface {
	// ListRepeatedMarkers returns candidate face markers of named subjects.
	ListRepeatedMarkers(ctx context.Context) ([]MarkerRow, error)
}

// DismissalSource lists the (photo, subject) groups a user has already settled as
// "this really is them twice". *feedback.Store satisfies it.
type DismissalSource interface {
	// DismissedDuplicateMarkerGroups returns every dismissed group.
	DismissedDuplicateMarkerGroups(ctx context.Context) ([]feedback.DuplicateMarkerDismissalKey, error)
}

// Config bundles the Service's collaborators. Both fields are required.
type Config struct {
	// Markers loads the candidate marker rows.
	Markers MarkerSource
	// Dismissals lists the groups a user already settled.
	Dismissals DismissalSource
}

// Service finds the repeated-marker groups. It holds no state and never writes.
type Service struct {
	markers    MarkerSource
	dismissals DismissalSource
}

// New returns a Service from cfg.
func New(cfg Config) *Service {
	return &Service{markers: cfg.Markers, dismissals: cfg.Dismissals}
}

// FindGroups loads the candidate markers, groups them by (photo, subject), drops
// everything a user has already settled, and returns the requested page — worst
// (most markers) first, then by person, so a curator can work top-down and watch
// the list shrink. limit is clamped into [1, maxLimit] (defaulting when
// non-positive); a negative offset is treated as zero. It returns a wrapped error
// if either source fails.
func (s *Service) FindGroups(ctx context.Context, limit, offset int) (Result, error) {
	limit, offset = clampPaging(limit, offset)

	rows, err := s.markers.ListRepeatedMarkers(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("dupmarkers: listing markers: %w", err)
	}
	dismissed, err := s.dismissals.DismissedDuplicateMarkerGroups(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("dupmarkers: listing dismissals: %w", err)
	}
	return paginate(GroupMarkers(rows, dismissed), limit, offset), nil
}

// groupKey is the (photo, subject) pair a finding is keyed by.
type groupKey struct {
	photoUID   string
	subjectUID string
}

// GroupMarkers turns candidate marker rows into findings: it keeps only valid face
// markers of named subjects, groups them by (photo, subject), drops the groups a
// user already dismissed and the ones left with fewer than minGroupSize markers,
// and orders what remains most-markers-first, then by person's name, then by
// subject and photo uid so the order is stable across requests.
//
// It is exported and pure so the rule that decides what a finding *is* can be read
// and tested in one place, independent of how the rows were loaded. A group that
// falls to a single marker — because the others were flagged invalid — is not a
// finding and disappears, which is exactly what makes the page converge as it is
// worked through.
func GroupMarkers(rows []MarkerRow, dismissed []feedback.DuplicateMarkerDismissalKey) []Group {
	skip := make(map[groupKey]struct{}, len(dismissed))
	for _, key := range dismissed {
		skip[groupKey{photoUID: key.PhotoUID, subjectUID: key.SubjectUID}] = struct{}{}
	}

	order := make([]groupKey, 0, len(rows))
	byKey := make(map[groupKey][]MarkerRow, len(rows))
	for _, row := range rows {
		if !counts(row) {
			continue
		}
		key := groupKey{photoUID: row.PhotoUID, subjectUID: row.SubjectUID}
		if _, ok := skip[key]; ok {
			continue
		}
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], row)
	}

	groups := make([]Group, 0, len(order))
	for _, key := range order {
		members := byKey[key]
		if len(members) < minGroupSize {
			continue
		}
		groups = append(groups, buildGroup(members))
	}
	sortGroups(groups)
	return groups
}

// counts reports whether a row may take part in a finding: a valid face marker of
// a named subject. An invalid marker is a region a user already rejected, a label
// marker is a hand-drawn box rather than an identity claim, and an empty name is
// the nameless catch-all subject.
func counts(row MarkerRow) bool {
	return !row.Invalid && row.Type == MarkerTypeFace && row.SubjectName != ""
}

// buildGroup assembles one finding from its rows, ordering the markers left to
// right (then top to bottom, then by uid) so the numbering drawn over the preview
// reads in the order a human scans the photo. The photo and subject facts are
// taken from the first row; every row of a group carries the same ones.
func buildGroup(rows []MarkerRow) Group {
	sort.SliceStable(rows, func(i, j int) bool {
		switch {
		case rows[i].X != rows[j].X:
			return rows[i].X < rows[j].X
		case rows[i].Y != rows[j].Y:
			return rows[i].Y < rows[j].Y
		default:
			return rows[i].MarkerUID < rows[j].MarkerUID
		}
	})
	head := rows[0]
	markers := make([]Marker, 0, len(rows))
	for _, row := range rows {
		markers = append(markers, Marker{
			UID:      row.MarkerUID,
			BBox:     [4]float64{row.X, row.Y, row.W, row.H},
			Score:    row.Score,
			Reviewed: row.Reviewed,
		})
	}
	return Group{
		PhotoUID:    head.PhotoUID,
		PhotoTitle:  head.PhotoTitle,
		TakenAt:     head.TakenAt,
		Width:       head.Width,
		Height:      head.Height,
		Orientation: head.Orientation,
		SubjectUID:  head.SubjectUID,
		SubjectName: head.SubjectName,
		Markers:     markers,
	}
}

// sortGroups orders findings worst-first: the most markers, then alphabetically by
// person so one person's mess is reviewed in one go, then by subject and photo uid
// so the order never shifts between two requests for the same data.
func sortGroups(groups []Group) {
	sort.SliceStable(groups, func(i, j int) bool {
		switch {
		case len(groups[i].Markers) != len(groups[j].Markers):
			return len(groups[i].Markers) > len(groups[j].Markers)
		case groups[i].SubjectName != groups[j].SubjectName:
			return groups[i].SubjectName < groups[j].SubjectName
		case groups[i].SubjectUID != groups[j].SubjectUID:
			return groups[i].SubjectUID < groups[j].SubjectUID
		default:
			return groups[i].PhotoUID < groups[j].PhotoUID
		}
	})
}

// clampPaging normalises the requested page size and offset: a non-positive limit
// becomes defaultLimit, anything above maxLimit is capped, and a negative offset
// becomes zero.
func clampPaging(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// paginate slices groups into the page at offset and reports the next offset (nil
// on the last page), leaving Total describing the whole finding set.
func paginate(groups []Group, limit, offset int) Result {
	total := len(groups)
	res := Result{Groups: []Group{}, Total: total, Limit: limit, Offset: offset}
	if offset >= total {
		return res
	}
	end := min(offset+limit, total)
	res.Groups = groups[offset:end]
	if end < total {
		next := end
		res.NextOffset = &next
	}
	return res
}
