package system

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/project"
)

// ProjectLister enumerates the instance's projects, for the parts of the
// snapshot that ask one project at a time: the latest import run and the
// background duplicate scan. It is satisfied by *project.Store; a nil one
// leaves both empty rather than reading across projects.
type ProjectLister interface {
	// List returns every project of the instance.
	List(ctx context.Context) ([]project.Project, error)
}

// ProjectQueue is one project's share of the job queue: the jobs that are
// still to run or that need a look. A per-photo job counts for the project of
// its photo; a library-wide job for the project its payload names. Jobs that
// belong to no project (mail, push, the notification purge) are counted only in
// the instance-wide Jobs section.
type ProjectQueue struct {
	// Queued is how many of the project's jobs wait for a worker.
	Queued int `json:"queued"`
	// Running is how many are being worked on now.
	Running int `json:"running"`
	// Failed is how many failed or exhausted their retries (failed + dead).
	Failed int `json:"failed"`
}

// ProjectSummary is one project's row in the dashboard's per-project
// breakdown: what its catalogue holds and what is still queued for it. The
// instance totals (Library, Remaining, Video, Jobs) are the sums across all
// projects; this is how they divide.
type ProjectSummary struct {
	// UID identifies the project; Slug and Name are how it is shown.
	UID  string `json:"uid"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Active is false for a deactivated project, which nobody may enter.
	Active bool `json:"active"`
	// Photos is how many live (non-archived) photos and videos it holds.
	Photos int `json:"photos"`
	// Videos is how many of those are videos.
	Videos int `json:"videos"`
	// Trashed is how many archived photos wait for purge.
	Trashed int `json:"trashed"`
	// OriginalsBytes is the summed size of its originals, the trash included —
	// they stay stored until a purge deletes them. It is the number the
	// project's storage quota counts.
	OriginalsBytes int64 `json:"originals_bytes"`
	// Faces is how many detected faces it holds.
	Faces int `json:"faces"`
	// Subjects is how many people, pets and other subjects it holds.
	Subjects int `json:"subjects"`
	// Albums is how many albums it holds.
	Albums int `json:"albums"`
	// Queue is its share of the job queue.
	Queue ProjectQueue `json:"queue"`
	// Duplicates is how many near-duplicate groups the last background scan
	// found in it; nil while no scan has finished or no finder is wired.
	Duplicates *int `json:"duplicates"`
	// LastImport is its most recent `kukatko import dir` run, nil when it has
	// never had one.
	LastImport *importer.Run `json:"last_import"`
}

// projectsBreakdownSQL is the per-project breakdown of the catalogue and the
// queue, as one scalar subquery the dashboard statement selects alongside its
// counts (so the whole dashboard stays one round trip): a JSON array of
// ProjectSummary objects, one per project, ordered by slug. Each count is one
// grouped pass over a root table (project_id leads its listing indexes), joined
// onto the project list so a project with nothing in it still gets its row of
// zeroes.
//
// The queue half reads only unfinished or broken jobs — the queue keeps its
// finished rows forever, and those are history, not work. A per-photo job is
// attributed through its payload's photo uid, a library-wide one through its
// payload's project uid; a job with neither belongs to no project.
const projectsBreakdownSQL = `(
    SELECT coalesce(jsonb_agg(jsonb_build_object(
               'uid', pr.uid, 'slug', pr.slug, 'name', pr.name, 'active', pr.active,
               'photos', coalesce(ph.photos, 0), 'videos', coalesce(ph.videos, 0),
               'trashed', coalesce(ph.trashed, 0), 'originals_bytes', coalesce(ph.originals_bytes, 0),
               'faces', coalesce(fc.faces, 0), 'subjects', coalesce(sj.subjects, 0),
               'albums', coalesce(al.albums, 0),
               'queue', jsonb_build_object('queued', coalesce(q.queued, 0),
                   'running', coalesce(q.running, 0), 'failed', coalesce(q.failed, 0)))
           ORDER BY pr.slug), '[]'::jsonb)
    FROM projects pr
    LEFT JOIN (
        SELECT project_id,
               count(*) FILTER (WHERE archived_at IS NULL) AS photos,
               count(*) FILTER (WHERE archived_at IS NULL AND media_type = 'video') AS videos,
               count(*) FILTER (WHERE archived_at IS NOT NULL) AS trashed,
               coalesce(sum(file_size), 0)::bigint AS originals_bytes
        FROM photos
        GROUP BY project_id
    ) ph ON ph.project_id = pr.uid
    LEFT JOIN (SELECT project_id, count(*) AS faces FROM faces GROUP BY project_id) fc
        ON fc.project_id = pr.uid
    LEFT JOIN (SELECT project_id, count(*) AS subjects FROM subjects GROUP BY project_id) sj
        ON sj.project_id = pr.uid
    LEFT JOIN (SELECT project_id, count(*) AS albums FROM albums GROUP BY project_id) al
        ON al.project_id = pr.uid
    LEFT JOIN (
        SELECT coalesce(j.payload ->> 'project_uid', p.project_id) AS project_id,
               count(*) FILTER (WHERE j.state = 'queued') AS queued,
               count(*) FILTER (WHERE j.state = 'running') AS running,
               count(*) FILTER (WHERE j.state IN ('failed', 'dead')) AS failed
        FROM jobs j
        LEFT JOIN photos p ON p.uid = j.payload ->> 'photo_uid'
        WHERE j.state IN ('queued', 'running', 'failed', 'dead')
        GROUP BY 1
    ) q ON q.project_id = pr.uid
)`

// decodeProjects turns the breakdown column into rows. Duplicates and
// LastImport are left nil: neither is a count the statement can take, so the
// service fills them in.
func decodeProjects(raw []byte) ([]ProjectSummary, error) {
	out := make([]ProjectSummary, 0)
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("system: decoding the project breakdown: %w", err)
	}
	return out, nil
}

// activeProjects returns the uids of the instance's active projects, or none
// when no lister is wired.
func (s *Service) activeProjects(ctx context.Context) ([]string, error) {
	if s.projects == nil {
		return nil, nil
	}
	all, err := s.projects.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	uids := make([]string, 0, len(all))
	for i := range all {
		if all[i].Active {
			uids = append(uids, all[i].UID)
		}
	}
	return uids, nil
}

// fillProjects stamps onto each breakdown row what the catalogue query cannot
// count: the project's latest folder import and its share of the last duplicate
// scan. A deactivated project is not scanned, so its Duplicates stays nil
// rather than claiming a count of zero.
func (s *Service) fillProjects(ctx context.Context, rows []ProjectSummary, dupes map[string]int, scanned bool) error {
	for i := range rows {
		run, err := s.latestRun(ctx, rows[i].UID, importer.SourceFolder)
		if err != nil {
			return err
		}
		rows[i].LastImport = run
		if scanned && rows[i].Active {
			n := dupes[rows[i].UID]
			rows[i].Duplicates = &n
		}
	}
	return nil
}
