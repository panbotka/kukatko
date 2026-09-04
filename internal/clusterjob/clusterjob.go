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
package clusterjob

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/jobs"
)

// Clusterer is the slice of cluster.Service this package drives, kept as an
// interface so the handler can be tested without a database or a vector index.
type Clusterer interface {
	// Recluster groups the currently clusterable faces into clusters and returns
	// how many clusters were created.
	Recluster(ctx context.Context) (int, error)
	// BuildSummaries prepares the cached listing summary of up to limit clusters
	// that have none, reporting what it did and how many still wait.
	BuildSummaries(ctx context.Context, limit int) (cluster.SummaryRun, error)
}

// Queue is the slice of the job store this package needs: appending a job and
// asking whether one of its kind is already waiting, which is what keeps a page
// load from stacking up a queue of identical passes.
type Queue interface {
	// Enqueue appends a job of jobType carrying payload to the persistent queue.
	Enqueue(ctx context.Context, jobType string, payload json.RawMessage, opts jobs.EnqueueOptions) (jobs.Job, error)
	// CountPending returns how many jobs of the given types are queued or running.
	CountPending(ctx context.Context, types ...string) (int, error)
}

// Service schedules and runs the face-grouping work.
type Service struct {
	clusters Clusterer
	queue    Queue
	batch    int
	log      *slog.Logger
}

// New returns a Service over clusters and queue. A non-positive batch uses
// cluster.DefaultSummaryBatch; a nil logger uses slog.Default().
func New(clusters Clusterer, queue Queue, batch int, logger *slog.Logger) *Service {
	if batch <= 0 {
		batch = cluster.DefaultSummaryBatch
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{clusters: clusters, queue: queue, batch: batch, log: logger}
}

// payload is the argument of a face_cluster job. Recluster distinguishes the
// admin trigger — regroup the unassigned faces first, then prepare what that
// produced — from the plain preparation pass a page load schedules, which must
// never regroup anything on a reader's behalf.
type payload struct {
	Recluster bool `json:"recluster,omitempty"`
}

// ScheduleRecluster queues a full pass: regroup the currently unassigned faces,
// then prepare the summaries of the groups that have none. It reports whether a
// job was actually queued — false means one is already waiting or running, and
// scheduling a second would only repeat its work.
//
// It is what the maintainer-only recluster endpoint calls: the pass itself takes
// minutes on a real library and must not be run inside the request that asks
// for it.
func (s *Service) ScheduleRecluster(ctx context.Context) (bool, error) {
	return s.schedule(ctx, payload{Recluster: true})
}

// EnsureSummaries queues a preparation pass for the groups whose summary has not
// been built yet, unless one is already waiting or running. It reports whether a
// job was queued.
//
// The face-groups page calls it when its listing says groups are pending, so
// opening the page is what starts (and only ever starts) the work that fills it
// in. It regroups nothing: browsing must never change who a face belongs to.
func (s *Service) EnsureSummaries(ctx context.Context) (bool, error) {
	return s.schedule(ctx, payload{})
}

// schedule appends a face_cluster job carrying p unless one is already pending.
// The queue's dedup index keys on a payload's photo_uid, which these payloads do
// not carry, so the check is explicit: at most one grouping pass is in flight,
// and a second request collapses into it.
func (s *Service) schedule(ctx context.Context, p payload) (bool, error) {
	pending, err := s.queue.CountPending(ctx, jobs.TypeFaceCluster)
	if err != nil {
		return false, fmt.Errorf("clusterjob: checking for a pending pass: %w", err)
	}
	if pending > 0 {
		return false, nil
	}
	if err := s.enqueue(ctx, p); err != nil {
		return false, err
	}
	return true, nil
}

// enqueue marshals p and appends the job unconditionally.
func (s *Service) enqueue(ctx context.Context, p payload) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("clusterjob: encoding the payload: %w", err)
	}
	if _, err := s.queue.Enqueue(ctx, jobs.TypeFaceCluster, raw, jobs.EnqueueOptions{}); err != nil {
		return fmt.Errorf("clusterjob: enqueuing the pass: %w", err)
	}
	return nil
}

// Handle runs one face_cluster job: it regroups the unassigned faces when the
// payload asks for it, then prepares a batch of the cluster summaries the
// listing is served from. When groups are still waiting at the end of the batch
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
	if p.Recluster {
		created, err := s.clusters.Recluster(ctx)
		if err != nil {
			return fmt.Errorf("clusterjob: regrouping unassigned faces: %w", err)
		}
		s.log.InfoContext(ctx, "face clustering finished", slog.Int("clusters_created", created))
	}
	run, err := s.clusters.BuildSummaries(ctx, s.batch)
	if err != nil {
		return fmt.Errorf("clusterjob: preparing cluster summaries: %w", err)
	}
	s.log.InfoContext(ctx, "face groups prepared",
		slog.Int("prepared", run.Built), slog.Int("dropped", run.Dropped),
		slog.Int("remaining", run.Remaining))
	if run.Remaining > 0 {
		return s.enqueue(ctx, payload{})
	}
	return nil
}
