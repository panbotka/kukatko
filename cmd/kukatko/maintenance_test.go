package main

import (
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/storage"
)

// TestMaintenanceStore_kindFollowsTheBackend verifies the scan is told which
// store it is reading: the bucket on an r2 instance, the originals root on a
// filesystem one. Getting this wrong is what made an object-store library report
// "0 originals on disk" and call itself consistent.
func TestMaintenanceStore_kindFollowsTheBackend(t *testing.T) {
	t.Parallel()

	// The store itself is the local backend in both cases — only the configured
	// backend decides what the report calls it, and that mapping is what is under
	// test here (building a real R2 client would need an endpoint).
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	tests := map[string]struct {
		backend string
		want    maintenance.StoreKind
	}{
		"filesystem":                       {backend: config.StorageBackendFS, want: maintenance.StoreDisk},
		"object store":                     {backend: config.StorageBackendR2, want: maintenance.StoreObject},
		"unset defaults to the filesystem": {backend: "", want: maintenance.StoreDisk},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{}
			cfg.Storage.Backend = tt.backend
			scanner, err := maintenanceStore(cfg, store)
			if err != nil {
				t.Fatalf("maintenanceStore: %v", err)
			}
			if scanner.Kind() != tt.want {
				t.Errorf("Kind() = %q, want %q", scanner.Kind(), tt.want)
			}
		})
	}
}

// notAKeyLister is a storage backend that cannot enumerate its keys, which no
// real backend is — the wiring must say so rather than scan nothing.
type notAKeyLister struct{ storage.Storage }

// TestMaintenanceStore_backendCannotList verifies a backend that cannot list its
// keys is refused at wiring time instead of silently inventorying nothing.
func TestMaintenanceStore_backendCannotList(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendR2
	_, err := maintenanceStore(cfg, notAKeyLister{})
	if err == nil || !strings.Contains(err.Error(), "cannot list its keys") {
		t.Errorf("maintenanceStore error = %v, want it to name the missing listing", err)
	}
}

// TestMaintenanceSidecarOrNil verifies the repairs are given a sidecar scheduler
// only where one could run: with the export off no `sidecar` handler is
// registered, so a job enqueued after a repair would sit in the queue for ever.
func TestMaintenanceSidecarOrNil(t *testing.T) {
	t.Parallel()

	enqueuer := jobs.NewEnqueuer(nil)
	on := &config.Config{}
	on.Sidecar.Enabled = true
	if got := maintenanceSidecarOrNil(on, enqueuer); got == nil {
		t.Error("with the export on the scheduler is nil, want the enqueuer")
	}
	if got := maintenanceSidecarOrNil(&config.Config{}, enqueuer); got != nil {
		t.Errorf("with the export off the scheduler is %v, want nil", got)
	}
}

// TestRepairOptionsFromFlags verifies every repair flag reaches its RepairOptions
// field, and that no flag means no repair — the CLI's "nothing was asked for".
func TestRepairOptionsFromFlags(t *testing.T) {
	t.Parallel()

	tests := map[string]func(maintenance.RepairOptions) bool{
		"thumbnails":       func(o maintenance.RepairOptions) bool { return o.Thumbnails },
		"embeddings":       func(o maintenance.RepairOptions) bool { return o.Embeddings },
		"faces":            func(o maintenance.RepairOptions) bool { return o.Faces },
		"phashes":          func(o maintenance.RepairOptions) bool { return o.Phashes },
		"import-orphans":   func(o maintenance.RepairOptions) bool { return o.ImportOrphans },
		"places":           func(o maintenance.RepairOptions) bool { return o.Places },
		"dimensions":       func(o maintenance.RepairOptions) bool { return o.Dimensions },
		"face-markers":     func(o maintenance.RepairOptions) bool { return o.FaceMarkers },
		"sideways-faces":   func(o maintenance.RepairOptions) bool { return o.SidewaysFaces },
		"impossible-dates": func(o maintenance.RepairOptions) bool { return o.ImpossibleDates },
	}
	for flag, selected := range tests {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			cmd := newMaintenanceRepairCmd()
			if err := cmd.Flags().Set(flag, "true"); err != nil {
				t.Fatalf("setting --%s: %v", flag, err)
			}
			opts, err := repairOptionsFromFlags(cmd)
			if err != nil {
				t.Fatalf("repairOptionsFromFlags: %v", err)
			}
			if !selected(opts) {
				t.Errorf("--%s did not select its repair: %+v", flag, opts)
			}
			if !opts.Any() {
				t.Errorf("--%s leaves Any() false", flag)
			}
		})
	}

	opts, err := repairOptionsFromFlags(newMaintenanceRepairCmd())
	if err != nil {
		t.Fatalf("repairOptionsFromFlags: %v", err)
	}
	if opts.Any() {
		t.Errorf("no flag selected %+v, want nothing", opts)
	}
}
