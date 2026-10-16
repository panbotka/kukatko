package project

import (
	"context"
	"fmt"
)

// Overview is one project as the system administrator's project list shows it:
// the row itself plus how much of the instance it occupies. The embedded
// Project's fields are promoted, so the wire shape is the project plus the
// three counts and the state of its storage move.
type Overview struct {
	Project
	// MembersCount is the number of memberships, approved or still waiting.
	MembersCount int64 `json:"members_count"`
	// PhotosCount is the number of photo rows, archived ones included (see Usage).
	PhotosCount int64 `json:"photos_count"`
	// OriginalsBytes is the summed size of those photos' originals.
	OriginalsBytes int64 `json:"originals_bytes"`
	// Rekey is the project's most recent storage move, or nil when it never had
	// one. It is carried here so the list needs no request per project to learn
	// which projects are moving; the run itself is read from the rekey API.
	Rekey *RekeySummary `json:"rekey"`
}

// RekeySummary is what the project list shows of a storage move: its state
// (running, paused, done or failed — the states of internal/rekey) and how
// many photos of how many have moved.
type RekeySummary struct {
	State       string `json:"state"`
	DonePhotos  int64  `json:"done_photos"`
	TotalPhotos int64  `json:"total_photos"`
}

// overviewSQL reads projects with their member count and library usage in one
// round trip; $1 narrows it to one uid, NULL lists every project by name. The
// usage is the same sum Usage computes, so the two can never disagree. The move
// is the project's most recent storage_rekey_runs row (the same pick as
// internal/rekey's Status), all NULL when there is none.
const overviewSQL = `
SELECT ` + columns + `,
       (SELECT count(*) FROM project_members m WHERE m.project_uid = projects.uid),
       usage.photos, usage.bytes, move.state, move.done_photos, move.total_photos
FROM projects
CROSS JOIN LATERAL (
    SELECT count(*) AS photos, COALESCE(sum(file_size), 0)::bigint AS bytes
    FROM photos WHERE project_id = projects.uid
) usage
LEFT JOIN LATERAL (
    SELECT r.state, r.done_photos, r.total_photos FROM storage_rekey_runs r
    WHERE r.project_uid = projects.uid ORDER BY r.started_at DESC, r.uid DESC LIMIT 1
) move ON true
WHERE $1::text IS NULL OR projects.uid = $1
ORDER BY name, uid`

// Overviews returns every project with its member count and usage, ordered by
// name (then uid). An instance always has at least the bootstrap project.
func (s *Store) Overviews(ctx context.Context) ([]Overview, error) {
	return s.overviews(ctx, nil)
}

// OverviewOf returns the project uid with its member count and usage, or
// ErrNotFound when there is no such project.
func (s *Store) OverviewOf(ctx context.Context, uid string) (*Overview, error) {
	list, err := s.overviews(ctx, &uid)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, uid)
	}
	return &list[0], nil
}

// overviews runs overviewSQL for one uid (non-nil) or for every project (nil).
func (s *Store) overviews(ctx context.Context, uid *string) ([]Overview, error) {
	rows, err := s.pool.Query(ctx, overviewSQL, uid)
	if err != nil {
		return nil, fmt.Errorf("project: listing overviews: %w", err)
	}
	defer rows.Close()

	out := []Overview{}
	for rows.Next() {
		var (
			o                   Overview
			moveState           *string
			moveDone, moveTotal *int64
		)
		if err := rows.Scan(&o.UID, &o.Slug, &o.Name, &o.Description, &o.Active, &o.StoragePrefix,
			&o.MaxFileBytes, &o.MaxTotalBytes, &o.CreatedAt, &o.UpdatedAt,
			&o.MembersCount, &o.PhotosCount, &o.OriginalsBytes,
			&moveState, &moveDone, &moveTotal); err != nil {
			return nil, fmt.Errorf("project: scanning overview: %w", err)
		}
		if moveState != nil && moveDone != nil && moveTotal != nil {
			o.Rekey = &RekeySummary{State: *moveState, DonePhotos: *moveDone, TotalPhotos: *moveTotal}
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project: iterating overviews: %w", err)
	}
	return out, nil
}

// allMembershipsSQL lists every membership of the instance with its project
// and the person it is linked to there (looked up in that project only), each
// account's in project-name order.
const allMembershipsSQL = `
SELECT m.user_uid, p.uid, p.slug, p.name, p.active, m.role, m.approved_at IS NOT NULL,
	m.subject_uid, COALESCE(s.name, '')
FROM project_members m JOIN projects p ON p.uid = m.project_uid
LEFT JOIN subjects s ON s.uid = m.subject_uid AND s.project_id = m.project_uid
ORDER BY p.name, p.uid`

// AllMemberships returns the memberships of every account of the instance,
// keyed by user uid, each list ordered by project name as MembershipsOf orders
// one account's, each carrying its linked person and that person's name. An
// account without memberships has no key. It is the one
// query behind the system administrator's account list, instead of one
// MembershipsOf per account.
func (s *Store) AllMemberships(ctx context.Context) (map[string][]MembershipSummary, error) {
	rows, err := s.pool.Query(ctx, allMembershipsSQL)
	if err != nil {
		return nil, fmt.Errorf("project: listing all memberships: %w", err)
	}
	defer rows.Close()

	out := map[string][]MembershipSummary{}
	for rows.Next() {
		var (
			userUID string
			ms      MembershipSummary
		)
		if err := rows.Scan(&userUID, &ms.ProjectUID, &ms.Slug, &ms.Name, &ms.Active, &ms.Role,
			&ms.Approved, &ms.SubjectUID, &ms.SubjectName); err != nil {
			return nil, fmt.Errorf("project: scanning a membership: %w", err)
		}
		out[userUID] = append(out[userUID], ms)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project: iterating all memberships: %w", err)
	}
	return out, nil
}
