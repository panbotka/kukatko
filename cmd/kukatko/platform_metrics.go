package main

import (
	"context"

	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/system"
)

// registerPlatformMetrics wires the platform collector into reg over the system
// service's scrape-safe accessor, so the backup outcome, the disk, the
// streaming-encode states, the map provider and the curation backlog on /metrics
// are the very numbers the System page shows, out of the same caches. It is a
// no-op when reg is nil.
func registerPlatformMetrics(reg *metrics.Registry, svc *system.Service) {
	if reg == nil {
		return
	}
	reg.RegisterPlatform(func(ctx context.Context) metrics.PlatformSnapshot {
		return platformSnapshot(svc.Platform(ctx))
	})
}

// platformSnapshot maps the system package's platform sections onto the metric
// shape. A section the system could not read stays nil, which the collector
// turns into a gap for exactly that section.
func platformSnapshot(p system.Platform) metrics.PlatformSnapshot {
	out := metrics.PlatformSnapshot{Backup: backupState(p.Backup), Maps: mapsState(p.Maps)}
	if p.Storage != nil {
		out.Disk = &metrics.DiskUsage{
			OriginalsBytes: p.Storage.OriginalsBytes,
			CacheBytes:     p.Storage.CacheBytes,
			FreeBytes:      p.Storage.FreeBytes,
			TotalBytes:     p.Storage.TotalBytes,
		}
	}
	if p.Dashboard != nil {
		out.Catalogue = catalogueState(*p.Dashboard, p.Storage != nil)
	}
	return out
}

// backupState maps the backup status, nil when no destination is configured.
// The latest run succeeded exactly when its finish time is also the latest
// success time: finish stamps both with one instant.
func backupState(s backup.Status) *metrics.BackupState {
	if !s.Configured {
		return nil
	}
	state := &metrics.BackupState{Running: s.Running}
	if s.LastSucceededAt != nil {
		state.LastSucceededAt = *s.LastSucceededAt
	}
	if s.LastFinishedAt != nil {
		state.LastFinishedAt = *s.LastFinishedAt
		state.LastSucceeded = s.LastSucceededAt != nil && s.LastSucceededAt.Equal(*s.LastFinishedAt)
	}
	if s.LastResult != nil {
		state.OriginalsUploaded = s.LastResult.OriginalsUploaded
		state.OriginalsSkipped = s.LastResult.OriginalsSkipped
	}
	return state
}

// mapsState maps the map-provider status, nil when no mapy.com key is set.
func mapsState(m system.Maps) *metrics.MapsState {
	if !m.Configured {
		return nil
	}
	state := &metrics.MapsState{Up: !m.Degraded}
	if m.CheckedAt != nil {
		state.CheckedAt = *m.CheckedAt
	}
	return state
}

// catalogueState maps the dashboard aggregation. The derived bytes are a
// filesystem measurement, so they are exported only when the storage section was
// read; otherwise the zero the dashboard carries would read as an empty cache.
func catalogueState(d system.Dashboard, measured bool) *metrics.CatalogueState {
	state := &metrics.CatalogueState{
		StreamingEnabled: d.Video.StreamingEnabled,
		VideosByEncodeState: map[string]int{
			metrics.EncodeStreamable:   d.Video.Streamable,
			metrics.EncodeQueued:       d.Video.EncodeQueued,
			metrics.EncodeRunning:      d.Video.EncodeRunning,
			metrics.EncodeFailed:       d.Video.EncodeFailed,
			metrics.EncodeNotScheduled: d.Video.NotScheduled,
		},
		Backlog: map[string]int{
			metrics.BacklogFacesUnassigned:      d.Remaining.FacesUnassigned,
			metrics.BacklogClusters:             d.Remaining.Clusters,
			metrics.BacklogPhotosWithoutTakenAt: d.Remaining.PhotosWithoutTakenAt,
			metrics.BacklogPhotosWithoutGPS:     d.Remaining.PhotosWithoutGPS,
			metrics.BacklogPhotosWithoutOCR:     d.Remaining.PhotosWithoutOCR,
			metrics.BacklogDuplicateMarkers:     d.Remaining.DuplicateMarkers,
		},
		Bytes: map[string]int64{
			metrics.BytesLive:  d.Library.LibraryBytes,
			metrics.BytesTrash: d.Library.TrashBytes,
		},
	}
	if measured {
		state.Bytes[metrics.BytesDerived] = d.Library.DerivedBytes
	}
	if d.Video.OldestQueuedAt != nil {
		state.OldestQueuedEncode = *d.Video.OldestQueuedAt
	}
	return state
}
