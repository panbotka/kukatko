package main

import (
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processapi"
	"github.com/panbotka/kukatko/internal/stacks"
)

// buildStacksServiceOrNil builds the stacking service (automatic detection and
// the manual stack/unstack/set-primary operations) from config. It returns nil
// when the feature's master switch is off, so the /process/stacks endpoint and
// the manual stacking routes degrade to 503 rather than acting. The service
// borrows a fresh photos.Store over the shared pool.
func buildStacksServiceOrNil(cfg *config.Config, db *database.DB) *stacks.Service {
	if !cfg.Stacks.Enabled {
		return nil
	}
	return stacks.New(photos.NewStore(db.Pool()), stacks.Config{
		Enabled: cfg.Stacks.Enabled,
		Rules: stacks.RuleSet{
			BaseName:       cfg.Stacks.Rules.BaseName,
			SequentialCopy: cfg.Stacks.Rules.SequentialCopy,
			UniqueID:       cfg.Stacks.Rules.UniqueID,
			TimeGPS:        cfg.Stacks.Rules.TimeGPS,
		},
	})
}

// stacksDetectorOrNil returns the stack detector as a processapi.StacksDetector,
// or a nil interface (not a typed-nil pointer, so processapi's == nil check fires
// and disables /process/stacks) when the feature is off.
func stacksDetectorOrNil(cfg *config.Config, db *database.DB) processapi.StacksDetector {
	if s := buildStacksServiceOrNil(cfg, db); s != nil {
		return s
	}
	return nil
}
