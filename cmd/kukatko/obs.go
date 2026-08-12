package main

import (
	"context"
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/placesjob"
	"github.com/panbotka/kukatko/internal/system"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/worker"
)

// thumbOptions returns the thumbnailer options shared by every thumb.New call
// site: generation-timing instrumentation when reg is non-nil, the configured
// per-photo encode concurrency, the decode pixel cap that rejects a
// decompression bomb before it can OOM a worker, the photo's saved
// non-destructive edit (so every size renders what the viewer shows), and the
// vips engine when thumb.engine is "vips" (resolved on PATH; a no-op when the
// binary is missing). It keeps the engine selection and instrumentation
// consistent across the process.
//
// The edit resolver is part of that consistency and not an optional extra: every
// thumbnailer writes into the one cache keyed by the original's file hash, so a
// call site left without it would publish the unedited rendering of an edited
// photo under the key the rest of the process reads. db may be nil only where no
// database is open at all.
func thumbOptions(cfg *config.Config, reg *metrics.Registry, db *database.DB) []thumb.Option {
	var opts []thumb.Option
	if reg != nil {
		opts = append(opts, thumb.WithObserver(reg))
	}
	if db != nil {
		opts = append(opts, thumb.WithEdits(photos.NewStore(db.Pool())))
	}
	if cfg.Thumb.Concurrency > 0 {
		opts = append(opts, thumb.WithConcurrency(cfg.Thumb.Concurrency))
	}
	opts = append(opts, thumb.WithMaxPixels(cfg.Thumb.MaxPixels))
	if cfg.Thumb.VipsEnabled() {
		opts = append(opts, thumb.WithVips(cfg.Thumb.VipsBinary))
	}
	return opts
}

// instrumentEmbedding wraps c so its calls report latency and availability to
// reg, returning c unchanged when reg is nil.
func instrumentEmbedding(c embedding.Client, reg *metrics.Registry) embedding.Client {
	if reg == nil {
		return c
	}
	return embedding.Instrument(c, reg)
}

// workerObserver returns reg as a worker.Observer, or a nil interface when reg
// is nil so the worker uses its no-op observer (avoiding a typed-nil pitfall).
func workerObserver(reg *metrics.Registry) worker.Observer {
	if reg == nil {
		return nil
	}
	return reg
}

// creditMeter returns reg as a placesjob.CreditMeter, or a nil interface when
// reg is nil so the places job uses its no-op meter.
func creditMeter(reg *metrics.Registry) placesjob.CreditMeter {
	if reg == nil {
		return nil
	}
	return reg
}

// registerJobQueueMetrics wires the job-queue depth collector into reg, adapting
// the jobs store's type/state breakdown to the metrics package's own key type.
// It is a no-op when reg is nil.
func registerJobQueueMetrics(reg *metrics.Registry, store *jobs.Store) {
	if reg == nil {
		return
	}
	reg.RegisterJobQueue(func(ctx context.Context) (map[metrics.QueueCell]int, error) {
		counts, err := store.CountsByTypeState(ctx)
		if err != nil {
			return nil, fmt.Errorf("counting jobs by type and state: %w", err)
		}
		out := make(map[metrics.QueueCell]int, len(counts))
		for cell, n := range counts {
			out[metrics.QueueCell{Type: cell.Type, State: string(cell.State)}] = n
		}
		return out, nil
	})
}

// registerLibraryMetrics wires the library-content collector into reg, reshaping
// internal/system's aggregation — the very one the admin dashboard reads — into
// the label maps the metric families need. Sharing that source is deliberate:
// the counts have one SQL statement behind them, so /metrics and the dashboard
// can never disagree. It is a no-op when reg is nil.
func registerLibraryMetrics(reg *metrics.Registry, svc *system.Service, ttl time.Duration) {
	if reg == nil {
		return
	}
	reg.RegisterLibrary(func(ctx context.Context) (metrics.LibrarySnapshot, error) {
		counts, err := svc.LibraryStats(ctx)
		if err != nil {
			return metrics.LibrarySnapshot{}, fmt.Errorf("collecting library stats: %w", err)
		}
		runs, err := svc.LatestRuns(ctx)
		if err != nil {
			return metrics.LibrarySnapshot{}, fmt.Errorf("collecting import runs: %w", err)
		}
		snapshot := librarySnapshot(counts)
		snapshot.Imports = importRuns(runs)
		return snapshot, nil
	}, ttl)
}

// librarySnapshot maps the system package's library counts onto the metric
// shape, one map entry per label value.
func librarySnapshot(counts system.Library) metrics.LibrarySnapshot {
	return metrics.LibrarySnapshot{
		PhotosByMediaType: map[string]int{
			"image": counts.Images, "video": counts.Videos, "live": counts.LivePhotos,
		},
		PhotosArchived: counts.PhotosArchived,
		PhotosProcessed: map[string]int{
			metrics.StageEmbedding: counts.PhotosWithEmbedding,
			metrics.StageFaces:     counts.PhotosWithFaces,
			metrics.StagePlaces:    counts.PhotosGeocoded,
		},
		PhotosPending: map[string]int{
			metrics.StageEmbedding: counts.PhotosWithoutEmbedding,
			metrics.StageFaces:     counts.PhotosWithoutFaces,
			metrics.StagePlaces:    counts.PhotosPendingGeocode,
		},
		Embeddings: counts.Embeddings,
		Faces:      counts.Faces,
		MarkersByState: map[string]int{
			metrics.MarkerAssigned:   counts.MarkersAssigned,
			metrics.MarkerUnassigned: counts.MarkersUnassigned,
		},
		SubjectsByType: map[string]int{
			"person": counts.SubjectsPerson, "pet": counts.SubjectsPet, "other": counts.SubjectsOther,
		},
		AlbumsByType: map[string]int{
			string(organize.AlbumManual): counts.AlbumsManual,
			string(organize.AlbumFolder): counts.AlbumsFolder,
			string(organize.AlbumMoment): counts.AlbumsMoment,
			string(organize.AlbumState):  counts.AlbumsState,
			string(organize.AlbumMonth):  counts.AlbumsMonth,
		},
		Labels: counts.Labels,
	}
}

// importRuns maps the latest run per source onto the metric shape. Every run
// carries the full status vocabulary so the collector can publish a zero for the
// statuses it is not in, making a transition visible.
func importRuns(runs map[importer.Source]importer.Run) []metrics.ImportRun {
	statuses := make([]string, 0, len(importer.AllStatuses()))
	for _, status := range importer.AllStatuses() {
		statuses = append(statuses, string(status))
	}
	out := make([]metrics.ImportRun, 0, len(runs))
	for _, source := range importer.AllSources() {
		run, ok := runs[source]
		if !ok {
			continue
		}
		exported := metrics.ImportRun{
			Source:    string(source),
			Status:    string(run.Status),
			Statuses:  statuses,
			StartedAt: run.StartedAt,
		}
		if run.FinishedAt != nil {
			exported.FinishedAt = *run.FinishedAt
		}
		out = append(out, exported)
	}
	return out
}
