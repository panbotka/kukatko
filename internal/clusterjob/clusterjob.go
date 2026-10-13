// Package clusterjob is the face-grouping work as background work: the
// `face_cluster` job that groups unassigned faces into clusters and prepares the
// cached summary each group is listed from, plus the two schedulers the HTTP
// surfaces call instead of doing that work inside a request.
//
// Both halves are vector search at library scale. The clustering pass runs one
// HNSW query per clusterable face; preparing a group's summary runs one more per
// group, on top of a face-listing query. On the production library that is
// minutes of work either way, which is why the admin trigger only schedules the
// pass now, and why the face-groups page reads prepared summaries instead of
// recomputing them for every viewer.
//
// A run bounds its own work: it prepares at most a batch of summaries and, if
// groups are still waiting, enqueues its successor. The queue therefore drains
// in visible steps, and the page can honestly say how many groups are ready
// while the rest are being prepared. Nothing here assigns a face to anybody —
// browsing and preparing are read-only in effect.
//
// The grouping itself has to start somewhere, and nothing else in the app ever
// starts it: face detection fills the faces table and stops. EnsureGrouping is
// that start. It reads the library's own state — faces but no groups, or groups
// with no summaries — and queues the one pass that state is missing, at most one
// at a time, so a library nobody has grouped by hand still ends up with groups
// while a fully grouped one queues nothing.
//
// Everything here is per project. A pass groups, prepares and lists one
// project's faces and clusters, named by the payload's project_uid; "at most one
// pass in flight" means at most one per project (the per-project pending check,
// backed by the idx_jobs_face_cluster_dedup index of migration 0105); and the
// maintainer's recluster trigger queues one pass for every active project.
package clusterjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/project"
)

// Clusterer is the slice of cluster.Service this package drives, kept as an
// interface so the handler can be tested without a database or a vector index.
type Clusterer interface {
	// Recluster groups the project's currently clusterable faces into clusters
	// and returns how many clusters were created.
	Recluster(ctx context.Context, projectUID string) (int, error)
	// BuildSummaries prepares the cached listing summary of up to limit of the
	// project's clusters that have none, reporting what it did and how many
	// still wait.
	BuildSummaries(ctx context.Context, projectUID string, limit int) (cluster.SummaryRun, error)
	// GroupingState reports what the project's groups look like: how many are
	// listable, how many await a summary, and whether a project with no groups
	// holds enough unassigned faces to make one.
	GroupingState(ctx context.Context, projectUID string) (cluster.GroupingState, error)
}

// Queue is the slice of the job store this package needs: appending a job and
// asking whether one of its kind is already waiting for a project, which is what
// keeps a page load from stacking up a queue of identical passes.
type Queue interface {
	// Enqueue appends a job of jobType carrying payload to the persistent queue.
	Enqueue(ctx context.Context, jobType string, payload json.RawMessage, opts jobs.EnqueueOptions) (jobs.Job, error)
	// CountPendingForProject returns how many jobs of the given types are queued
	// or running for the project.
	CountPendingForProject(ctx context.Context, projectUID string, types ...string) (int, error)
}

// Projects lists the instance's projects, so the maintainer's recluster trigger
// can queue a pass for each active one; *project.Store satisfies it.
type Projects interface {
	// List returns every project, active or not.
	List(ctx context.Context) ([]project.Project, error)
}

// Service schedules and runs the face-grouping work.
type Service struct {
	clusters Clusterer
	queue    Queue
	projects Projects
	batch    int
	log      *slog.Logger
}

// New returns a Service over clusters, queue and projects. A non-positive batch
// uses cluster.DefaultSummaryBatch; a nil logger uses slog.Default(). projects
// is only read by ScheduleRecluster; a nil one makes that trigger fail rather
// than silently queue nothing.
func New(clusters Clusterer, queue Queue, projects Projects, batch int, logger *slog.Logger) *Service {
	if batch <= 0 {
		batch = cluster.DefaultSummaryBatch
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{clusters: clusters, queue: queue, projects: projects, batch: batch, log: logger}
}

// payload is the argument of a face_cluster job. ProjectUID names the project
// the pass works on. Recluster distinguishes the admin trigger — regroup the
// unassigned faces first, then prepare what that produced — from the plain
// preparation pass a page load schedules, which must never regroup anything on
// a reader's behalf.
type payload struct {
	ProjectUID string `json:"project_uid"`
	Recluster  bool   `json:"recluster,omitempty"`
}

// projectOf returns the project a job's payload names. A job queued before the
// payload carried a project (an upgrade with a pass still waiting) names none;
// the only project that existed when it was queued is the bootstrap one, so that
// is the project it works on — never "every project".
func (p payload) projectOf() string {
	if p.ProjectUID == "" {
		return project.BootstrapUID
	}
	return p.ProjectUID
}

// ScheduleRecluster queues a full pass for every active project: regroup the
// project's currently unassigned faces, then prepare the summaries of its groups
// that have none. It reports whether at least one job was actually queued —
// false means every active project already has a pass waiting or running, and
// scheduling a second would only repeat its work. A deactivated project is
// skipped: nobody can open its face-groups page.
//
// It is the instance-level trigger; the maintainer-only recluster endpoint,
// which is mounted per project, calls ScheduleProjectRecluster instead.
func (s *Service) ScheduleRecluster(ctx context.Context) (bool, error) {
	if s.projects == nil {
		return false, errors.New("clusterjob: no project lister wired")
	}
	all, err := s.projects.List(ctx)
	if err != nil {
		return false, fmt.Errorf("clusterjob: listing projects: %w", err)
	}
	queued := false
	for i := range all {
		if !all[i].Active {
			continue
		}
		ok, err := s.ScheduleProjectRecluster(ctx, all[i].UID)
		if err != nil {
			return queued, err
		}
		queued = queued || ok
	}
	return queued, nil
}

// ScheduleProjectRecluster queues a full pass for the one project projectUID:
// regroup its currently unassigned faces, then prepare the summaries of its
// groups that have none. It reports whether a job was actually queued — false
// means the project already has a pass waiting or running. An empty projectUID
// is refused: a pass always works on exactly one project.
//
// It is what the maintainer-only recluster endpoint calls: the pass itself takes
// minutes on a real library and must not be run inside the request that asks
// for it.
func (s *Service) ScheduleProjectRecluster(ctx context.Context, projectUID string) (bool, error) {
	if projectUID == "" {
		return false, errors.New("clusterjob: scheduling a recluster without a project")
	}
	return s.schedule(ctx, payload{ProjectUID: projectUID, Recluster: true})
}

// EnsureGrouping queues the grouping work the project projectUID is missing, if
// any, and reports whether a pass is queued or running for it once it has looked — which is what
// the face-groups page says out loud instead of showing a bare empty state.
//
// Two things can be missing, and the library's own state says which:
//
//   - It has faces and no groups at all. Nothing else in the app ever starts the
//     grouping pass — face detection fills the faces table and stops there — so
//     without this a library that nobody grouped by hand stays empty for ever.
//     The pass groups only unassigned, unclustered faces: an already-named face
//     is never regrouped or reassigned by somebody merely opening the page.
//   - It has groups whose cached listing summary has not been built yet. That
//     pass regroups nothing at all; it only prepares what the listing draws.
//
// A library that is fully grouped and fully prepared queues nothing, and neither
// does one with no faces: an empty library must stay calmly empty rather than
// promise work that will never happen. At most one pass is in flight, so
// reloading the page collapses into the pass already queued instead of stacking
// up a queue of identical ones.
func (s *Service) EnsureGrouping(ctx context.Context, projectUID string) (bool, error) {
	if projectUID == "" {
		return false, cluster.ErrNoProject
	}
	pending, err := s.queue.CountPendingForProject(ctx, projectUID, jobs.TypeFaceCluster)
	if err != nil {
		return false, fmt.Errorf("clusterjob: checking for a pending pass: %w", err)
	}
	if pending > 0 {
		return true, nil
	}
	state, err := s.clusters.GroupingState(ctx, projectUID)
	if err != nil {
		return false, fmt.Errorf("clusterjob: reading the project's grouping state: %w", err)
	}
	due, ok := duePass(state)
	if !ok {
		return false, nil
	}
	due.ProjectUID = projectUID
	if _, err := s.enqueue(ctx, due); err != nil {
		return false, err
	}
	s.log.InfoContext(ctx, "face grouping pass queued", slog.String("project_uid", projectUID),
		slog.Bool("recluster", due.Recluster), slog.Int("groups_ready", state.Ready),
		slog.Int("groups_unprepared", state.Unprepared))
	return true, nil
}

// duePass returns the pass state is asking for, and whether one is due at all.
// Grouping comes first: a library with faces and no groups needs the groups
// before their summaries can mean anything.
func duePass(state cluster.GroupingState) (payload, bool) {
	switch {
	case state.Groupable:
		return payload{Recluster: true}, true
	case state.Unprepared > 0:
		return payload{}, true
	default:
		return payload{}, false
	}
}

// schedule appends a face_cluster job carrying p unless one is already pending
// for p's project. The check is explicit because a running pass counts too (the
// dedup index only sees queued ones): at most one grouping pass per project is
// in flight, and a second request collapses into it.
func (s *Service) schedule(ctx context.Context, p payload) (bool, error) {
	pending, err := s.queue.CountPendingForProject(ctx, p.ProjectUID, jobs.TypeFaceCluster)
	if err != nil {
		return false, fmt.Errorf("clusterjob: checking for a pending pass: %w", err)
	}
	if pending > 0 {
		return false, nil
	}
	return s.enqueue(ctx, p)
}

// enqueue marshals p and appends the job, reporting whether a job was added. A
// pass already queued for the same project (the per-project dedup index of
// migration 0105, hit by a racing scheduler) is not an error: the work is
// scheduled, just not by this call.
func (s *Service) enqueue(ctx context.Context, p payload) (bool, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return false, fmt.Errorf("clusterjob: encoding the payload: %w", err)
	}
	if _, err := s.queue.Enqueue(ctx, jobs.TypeFaceCluster, raw, jobs.EnqueueOptions{}); err != nil {
		if errors.Is(err, jobs.ErrDuplicate) {
			return false, nil
		}
		return false, fmt.Errorf("clusterjob: enqueuing the pass: %w", err)
	}
	return true, nil
}

// Handle runs one face_cluster job for the project its payload names: it
// regroups that project's unassigned faces when the payload asks for it, then
// prepares a batch of the project's cluster summaries the listing is served
// from. When groups are still waiting at the end of the batch
// it enqueues its own successor, so a large backlog drains in bounded steps
// instead of one run that holds a worker for minutes on end.
//
// The successor is enqueued unconditionally rather than through the pending
// check: this job is still running, so the check would see itself.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var p payload
	if len(job.Payload) > 0 {
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return fmt.Errorf("clusterjob: decoding the payload of job %d: %w", job.ID, err)
		}
	}
	projectUID := p.projectOf()
	if p.Recluster {
		created, err := s.clusters.Recluster(ctx, projectUID)
		if err != nil {
			return fmt.Errorf("clusterjob: regrouping unassigned faces: %w", err)
		}
		s.log.InfoContext(ctx, "face clustering finished", slog.String("project_uid", projectUID),
			slog.Int("clusters_created", created))
	}
	run, err := s.clusters.BuildSummaries(ctx, projectUID, s.batch)
	if err != nil {
		return fmt.Errorf("clusterjob: preparing cluster summaries: %w", err)
	}
	s.log.InfoContext(ctx, "face groups prepared", slog.String("project_uid", projectUID),
		slog.Int("prepared", run.Built), slog.Int("dropped", run.Dropped),
		slog.Int("remaining", run.Remaining))
	if run.Remaining > 0 {
		_, err := s.enqueue(ctx, payload{ProjectUID: projectUID})
		return err
	}
	return nil
}
