package system

import (
	"context"

	"github.com/panbotka/kukatko/internal/backup"
)

// Platform is the part of the status snapshot that is safe to read on every
// Prometheus scrape: the memoised dashboard aggregation, the memoised storage
// measurement and the two in-memory provider states. It is what /metrics exports
// about the machine and the catalogue underneath the request path.
//
// It deliberately leaves out everything Collect does per request — the database
// ping, the sidecar probe, the queue and import-history queries — and the
// near-duplicate scan, whose background refresh is far too expensive to be kept
// warm by a scraper that never stops.
type Platform struct {
	// Dashboard is the memoised dashboard aggregation, with the streaming flag
	// stamped as Collect stamps it and DerivedBytes filled in from Storage when
	// that measurement succeeded. Remaining.Duplicates is left zero (see above).
	// Nil when the aggregation failed: zeroes would describe an empty library.
	Dashboard *Dashboard
	// Storage is the memoised on-disk measurement; nil when it failed, because a
	// partial one (a free-space reading of zero after a failed statfs) would read
	// as a full disk.
	Storage *StorageUsage
	// Backup is the backup subsystem's state, Configured false when no
	// destination is wired. It is an in-memory read and cannot fail.
	Backup backup.Status
	// Maps is the map provider's last observed state, Configured false when no
	// mapy.com key is set. It is an in-memory read and cannot fail.
	Maps Maps
}

// Platform hands over the already-cached dashboard data, the storage measurement
// and the backup and map-provider states for a scrape-time reader. Nothing here
// costs more than a cache read: the dashboard and the storage measurement are
// recomputed only when their own TTL has run out (so a scraper shares the
// admin page's caches rather than adding a load of its own), and the other two
// are in-memory snapshots.
//
// A failed source is reported by its nil section rather than as an error, so a
// reader can drop exactly that section and keep the rest.
func (s *Service) Platform(ctx context.Context) Platform {
	out := Platform{Backup: s.collectBackup(), Maps: s.collectMaps()}
	if usage, err := s.storage.usage(ctx); err == nil {
		out.Storage = &usage
	}
	if dashboard, err := s.dashboard.get(ctx); err == nil {
		dashboard.Video.StreamingEnabled = s.streaming
		if out.Storage != nil {
			dashboard.Library.DerivedBytes = out.Storage.CacheBytes
		}
		out.Dashboard = &dashboard
	}
	return out
}
