package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
)

// wantsAll is an audience that receives every kind on every channel.
type wantsAll struct{}

// ChannelsTx delivers everywhere.
func (wantsAll) ChannelsTx(context.Context, pgx.Tx, string, notification.Kind) (notifydeliver.Channels, error) {
	return notifydeliver.Channels{InApp: true, Push: true, Email: true}, nil
}

// TestTagNotifyRecorderConfig checks the recorder follows push.tags.window and
// keeps the audience it is given.
func TestTagNotifyRecorderConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Push: config.PushConfig{Enabled: true, Tags: config.PushTagsConfig{Window: 20 * time.Minute}}}
	got := tagNotifyRecorderConfig(cfg, wantsAll{})
	if got.Window != 20*time.Minute || got.Audience == nil {
		t.Fatalf("recorder config = %+v, want a 20m window and the audience", got)
	}
}
