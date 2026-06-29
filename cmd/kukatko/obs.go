package main

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/worker"
)

// thumbOptions returns the thumbnailer options that wire generation timing into
// reg, or no options when reg is nil (metrics disabled). It keeps every
// thumb.New call site instrumented consistently.
func thumbOptions(reg *metrics.Registry) []thumb.Option {
	if reg == nil {
		return nil
	}
	return []thumb.Option{thumb.WithObserver(reg)}
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

// importObserver returns reg as an importer.ProgressObserver, or a nil interface
// when reg is nil so the import services use their no-op observer.
func importObserver(reg *metrics.Registry) importer.ProgressObserver {
	if reg == nil {
		return nil
	}
	return reg
}

// registerJobQueueMetrics wires the job-queue depth collector into reg, adapting
// the jobs store's state/type tallies to the metrics package's string-keyed
// signature. It is a no-op when reg is nil.
func registerJobQueueMetrics(reg *metrics.Registry, store *jobs.Store) {
	if reg == nil {
		return
	}
	byState := func(ctx context.Context) (map[string]int, error) {
		counts, err := store.CountsByState(ctx)
		if err != nil {
			return nil, fmt.Errorf("counting jobs by state: %w", err)
		}
		out := make(map[string]int, len(counts))
		for state, n := range counts {
			out[string(state)] = n
		}
		return out, nil
	}
	reg.RegisterJobQueue(byState, store.CountsByType)
}
