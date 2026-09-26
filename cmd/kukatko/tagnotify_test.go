package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/notification"
)

// wantsAll is a preference reader that wants every kind.
type wantsAll struct{}

// WantsTx wants everything.
func (wantsAll) WantsTx(context.Context, pgx.Tx, string, notification.Kind) (bool, error) {
	return true, nil
}

// TestTagNotifyRecorderConfig checks the recorder follows push.enabled and
// push.tags.window.
func TestTagNotifyRecorderConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Push: config.PushConfig{Enabled: true, Tags: config.PushTagsConfig{Window: 20 * time.Minute}}}
	got := tagNotifyRecorderConfig(cfg, wantsAll{})
	if !got.Enabled || got.Window != 20*time.Minute || got.Preferences == nil {
		t.Fatalf("recorder config = %+v, want push on, a 20m window and the preferences", got)
	}
}
