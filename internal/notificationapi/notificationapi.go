// Package notificationapi exposes over HTTP what internal/push and
// internal/notification already do: whether this instance can push at all and
// with which public key, the calling account's push subscriptions (register,
// list, remove), its per-kind notification preferences, and one notification
// record with its frozen photo set.
//
// It holds no business logic of its own. Every operation is scoped to the
// account taken from the auth context, and every role may use every route — a
// viewer wants to know they were tagged just as much as an editor does. A
// notification or subscription that belongs to somebody else is a 404, never a
// 403 (the internal/savedsearchapi rule), so a uid cannot be probed for
// existence.
//
// Two secrets are kept out of every response by construction. The VAPID private
// key never reaches this package — Config carries only the public half — and a
// subscription's client keys (p256dh, auth) are dropped by the one view a
// subscription is ever written through.
//
// The photo set of a notification is filtered by what the caller may see now,
// not by what was visible when the notification was made: a photograph archived,
// hidden or made private since drops out, and the response states how many did
// so the page can explain a short list instead of silently showing ten of
// twelve.
//
// There is deliberately no "list my notifications" route: there is no in-app
// notification centre, and a deeplink only ever needs one record.
package notificationapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/storage"
)

// SubscriptionStore is the subset of push.Store the subscription routes need.
type SubscriptionStore interface {
	// Upsert stores sub for sub.UserUID keyed by its endpoint and returns the
	// stored row; an unusable subscription is push.ErrInvalidSubscription.
	Upsert(ctx context.Context, sub push.Subscription) (push.Subscription, error)
	// ListForUser returns every subscription of userUID, oldest first.
	ListForUser(ctx context.Context, userUID string) ([]push.Subscription, error)
	// DeleteByID removes subscription id of userUID, or returns push.ErrNotFound
	// when there is no such row or it belongs to somebody else.
	DeleteByID(ctx context.Context, userUID, id string) error
}

// NotificationStore is the subset of notification.Store the notification and
// preference routes need. Every method is scoped to the owning account.
type NotificationStore interface {
	// Get returns ownerUID's notification uid, or notification.ErrNotFound.
	Get(ctx context.Context, ownerUID, uid string) (notification.Notification, error)
	// MarkRead stamps ownerUID's notification uid read (idempotently) and
	// returns it, or notification.ErrNotFound.
	MarkRead(ctx context.Context, ownerUID, uid string) (notification.Notification, error)
	// Photos returns the photo uids of the frozen set that vis lets through,
	// in their frozen order, or notification.ErrNotFound.
	Photos(ctx context.Context, ownerUID, uid string, vis notification.Visibility) ([]string, error)
	// Preferences returns userUID's effective preferences, one per known kind.
	Preferences(ctx context.Context, userUID string) ([]notification.Preference, error)
	// ReplacePreferences replaces userUID's stored choices, audited in the same
	// transaction, and returns the new effective preferences.
	ReplacePreferences(
		ctx context.Context, userUID string, prefs []notification.Preference, entry audit.Entry,
	) ([]notification.Preference, error)
}

// PhotoStore is the subset of photos.Store that resolves a frozen set's uids
// into full photo records.
type PhotoStore interface {
	// ListByUIDs returns the photos for the given uids in unspecified order.
	ListByUIDs(ctx context.Context, uids []string) ([]photos.Photo, error)
}

// PushSettings is the part of the push configuration a client may learn. It has
// no field for the VAPID private key on purpose: what this package never holds,
// no response of it can leak.
type PushSettings struct {
	// Enabled mirrors push.enabled: whether this instance delivers push
	// notifications at all.
	Enabled bool
	// PublicKey is the VAPID application server key a browser subscribes with.
	PublicKey string
}

// API exposes the notification endpoints over HTTP. The auth guard and the
// throttle are supplied by the caller, so this package depends on auth only for
// the caller's identity.
type API struct {
	subscriptions SubscriptionStore
	notifications NotificationStore
	photos        PhotoStore
	// media stamps the thumb/download URLs onto every photo this API returns.
	media       *mediaurl.Builder
	push        PushSettings
	requireAuth func(http.Handler) http.Handler
	throttle    func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Every store and RequireAuth are
// required; SubscribeThrottle is optional.
type Config struct {
	// Subscriptions backs the push subscription routes.
	Subscriptions SubscriptionStore
	// Notifications backs the notification and preference routes.
	Notifications NotificationStore
	// Photos resolves a notification's photo set into full records.
	Photos PhotoStore
	// Storage decides where a client fetches the returned photos' media. A nil
	// storage points them at this application's own media routes.
	Storage storage.Storage
	// Push is what the capability route reports and what gates the
	// subscription write.
	Push PushSettings
	// RequireAuth guards every route for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
	// SubscribeThrottle rate-limits the subscription write; it is mounted inside
	// RequireAuth so a keyed limiter sees the caller. Nil throttles nothing.
	SubscribeThrottle func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	throttle := cfg.SubscribeThrottle
	if throttle == nil {
		throttle = func(next http.Handler) http.Handler { return next }
	}
	return &API{
		subscriptions: cfg.Subscriptions,
		notifications: cfg.Notifications,
		photos:        cfg.Photos,
		media:         mediaurl.NewBuilder(cfg.Storage),
		push:          cfg.Push,
		requireAuth:   cfg.RequireAuth,
		throttle:      throttle,
	}
}

// RegisterRoutes mounts the endpoints onto r, which the caller has scoped under
// the API base path (for example /api/v1). Every route requires an
// authenticated user of any role:
//
//	GET    /push/config                  is push on, and the VAPID public key
//	GET    /push/subscriptions           the caller's subscribed devices
//	POST   /push/subscriptions           register or refresh this browser (503 when push is off)
//	DELETE /push/subscriptions?endpoint= remove one of the caller's subscriptions
//	GET    /notifications/preferences    the caller's effective preferences
//	PUT    /notifications/preferences    replace them
//	GET    /notifications/{uid}          one notification with its visible photos
//	POST   /notifications/{uid}/read     mark it read
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/push", func(r chi.Router) {
		r.Use(a.requireAuth)
		r.Get("/config", a.handlePushConfig)
		r.Get("/subscriptions", a.handleListSubscriptions)
		r.With(a.throttle).Post("/subscriptions", a.handleSubscribe)
		r.Delete("/subscriptions", a.handleUnsubscribe)
	})
	r.Route("/notifications", func(r chi.Router) {
		r.Use(a.requireAuth)
		r.Get("/preferences", a.handleGetPreferences)
		r.Put("/preferences", a.handleReplacePreferences)
		r.Get("/{uid}", a.handleGetNotification)
		r.Post("/{uid}/read", a.handleMarkRead)
	})
}

// currentUser returns the authenticated user from the request context, writing a
// 401 and reporting ok=false when none is present (a defensive guard;
// RequireAuth should already have rejected the request).
func currentUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return auth.User{}, false
	}
	return user, true
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("notificationapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
