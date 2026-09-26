package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notificationapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/storage"
)

// buildNotificationAPI assembles the notification HTTP API over the shared pool:
// the push capability and the caller's push subscriptions (internal/push), the
// caller's per-kind preferences and one notification with its frozen photo set
// (internal/notification). Every route uses the read guard — a viewer wants to
// know they were tagged as much as an editor does — and mediaStore decides where
// the returned photos' media is fetched from.
//
// The subscription write is rate-limited from the comment rule's settings, keyed
// by the caller, in a bucket of its own: one number to tune, and a loop
// re-subscribing cannot use up anybody's allowance for commenting.
func buildNotificationAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage,
) *notificationapi.API {
	limit := ratelimit.New(cfg.RateLimit.Comment.RatePerSec, cfg.RateLimit.Comment.Burst)
	return notificationapi.NewAPI(notificationapi.Config{
		Subscriptions:     push.NewStore(db.Pool()),
		Notifications:     notification.NewStore(db.Pool()),
		Photos:            photos.NewStore(db.Pool()),
		Storage:           mediaStore,
		Push:              pushSettings(cfg),
		RequireAuth:       authAPI.RequireAuth,
		SubscribeThrottle: limit.KeyedMiddlewareExcept(commentRateKey, auth.RateLimitExempt),
	})
}

// pushSettings is what of the push configuration a client may learn: whether
// push is on and the VAPID public key. The private key is not copied — the API
// never holds it, so no response of it can carry it.
func pushSettings(cfg *config.Config) notificationapi.PushSettings {
	return notificationapi.PushSettings{Enabled: cfg.Push.Enabled, PublicKey: cfg.Push.VAPID.PublicKey}
}
