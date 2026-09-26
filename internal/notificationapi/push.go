package notificationapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/panbotka/kukatko/internal/push"
)

// maxBodyBytes caps every request body this package reads. A subscription is a
// URL and two short keys, a preference replace a handful of kinds.
const maxBodyBytes = 64 << 10

// maxUserAgentLen caps the stored user-agent, in bytes. It is display text for
// telling devices apart, so a longer header is cut rather than refused.
const maxUserAgentLen = 512

// pushConfigView is the capability response. It has no field for the VAPID
// private key, so none can be serialised.
type pushConfigView struct {
	// Enabled is whether this instance delivers push notifications at all.
	Enabled bool `json:"enabled"`
	// PublicKey is the application server key to subscribe with; empty while
	// push is off, since there is nothing to subscribe to.
	PublicKey string `json:"public_key"`
}

// subscriptionView is the only shape a subscription is ever written in. It
// deliberately omits p256dh and auth — shared secrets for encryption, not display
// data — as well as the owner (always the caller) and the failure bookkeeping.
type subscriptionView struct {
	ID string `json:"id"`
	// Endpoint lets the page recognise "this browser" by comparing it with the
	// endpoint of the browser's own PushSubscription.
	Endpoint   string     `json:"endpoint"`
	UserAgent  string     `json:"user_agent"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// toSubscriptionView projects a stored subscription onto its client-facing view.
func toSubscriptionView(sub push.Subscription) subscriptionView {
	return subscriptionView{
		ID:         sub.ID,
		Endpoint:   sub.Endpoint,
		UserAgent:  sub.UserAgent,
		CreatedAt:  sub.CreatedAt,
		LastUsedAt: sub.LastUsedAt,
	}
}

// subscriptionsEnvelope wraps the list response under the subscriptions key.
type subscriptionsEnvelope struct {
	Subscriptions []subscriptionView `json:"subscriptions"`
}

// subscribeInput is the JSON body of the subscription write. The keys object is
// the shape PushSubscription.toJSON() produces, so the browser's value can be
// posted as it is.
type subscribeInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	// UserAgent is optional; when absent the request's own User-Agent header
	// names the device.
	UserAgent *string `json:"user_agent"`
	// ExpirationTime is what PushSubscription.toJSON() emits alongside the
	// rest. It is accepted so the browser's value posts unchanged, and ignored.
	ExpirationTime *float64 `json:"expirationTime"`
}

// decodeJSON reads dst from the JSON request body, rejecting unknown fields and
// an oversized body. The returned error message is safe to surface to a client.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}

// decodeSubscribe decodes the subscription write for userUID into the
// subscription to store. It only checks that the three required fields are
// present; whether they are usable is push.ValidateSubscription's call, made by
// the store.
func decodeSubscribe(r *http.Request, userUID string) (push.Subscription, error) {
	var in subscribeInput
	if err := decodeJSON(r, &in); err != nil {
		return push.Subscription{}, err
	}
	if strings.TrimSpace(in.Endpoint) == "" || strings.TrimSpace(in.Keys.P256dh) == "" ||
		strings.TrimSpace(in.Keys.Auth) == "" {
		return push.Subscription{}, errors.New("endpoint, keys.p256dh and keys.auth are required")
	}
	userAgent := r.UserAgent()
	if in.UserAgent != nil {
		userAgent = *in.UserAgent
	}
	return push.Subscription{
		UserUID:   userUID,
		Endpoint:  strings.TrimSpace(in.Endpoint),
		P256dh:    in.Keys.P256dh,
		Auth:      in.Keys.Auth,
		UserAgent: truncateUTF8(strings.TrimSpace(userAgent), maxUserAgentLen),
	}, nil
}

// truncateUTF8 returns s as valid UTF-8 cut to at most maxBytes bytes without
// splitting a character. Invalid bytes are dropped first: the column is TEXT,
// which refuses them.
func truncateUTF8(s string, maxBytes int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// handlePushConfig writes whether push is available and the public key to
// subscribe with. Only the public half of the VAPID pair is ever known here.
func (a *API) handlePushConfig(w http.ResponseWriter, _ *http.Request) {
	view := pushConfigView{Enabled: a.push.Enabled}
	if a.push.Enabled {
		view.PublicKey = a.push.PublicKey
	}
	writeJSON(w, http.StatusOK, view)
}

// handleListSubscriptions writes the caller's subscriptions, oldest first,
// without their client keys.
func (a *API) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	subs, err := a.subscriptions.ListForUser(r.Context(), user.UID)
	if err != nil {
		log.Printf("notificationapi: listing subscriptions of %s: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "listing subscriptions failed")
		return
	}
	views := make([]subscriptionView, 0, len(subs))
	for _, sub := range subs {
		views = append(views, toSubscriptionView(sub))
	}
	writeJSON(w, http.StatusOK, subscriptionsEnvelope{Subscriptions: views})
}

// handleSubscribe registers or refreshes the calling browser's subscription,
// bound to the calling account. With push off instance-wide it answers 503
// rather than store a subscription nothing will ever be delivered to.
func (a *API) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if !a.push.Enabled {
		writeError(w, http.StatusServiceUnavailable, "push notifications are disabled on this instance")
		return
	}
	sub, err := decodeSubscribe(r, user.UID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stored, err := a.subscriptions.Upsert(r.Context(), sub)
	if errors.Is(err, push.ErrInvalidSubscription) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		log.Printf("notificationapi: storing a subscription of %s: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "storing the subscription failed")
		return
	}
	writeJSON(w, http.StatusOK, toSubscriptionView(stored))
}

// handleUnsubscribe removes the caller's subscription named by the endpoint
// query parameter. It works with push off too, so a device can always be
// cleaned up. An endpoint the caller does not hold — unknown, or somebody
// else's — is a 404.
func (a *API) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	endpoint := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	if endpoint == "" {
		writeError(w, http.StatusBadRequest, "endpoint is required")
		return
	}
	// The subscription is found among the caller's own rows and removed by its
	// id under the caller's account, so another account's endpoint can never be
	// deleted from here.
	subs, err := a.subscriptions.ListForUser(r.Context(), user.UID)
	if err != nil {
		log.Printf("notificationapi: listing subscriptions of %s: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "removing the subscription failed")
		return
	}
	id, found := subscriptionIDFor(subs, endpoint)
	if !found {
		writeError(w, http.StatusNotFound, "subscription not found")
		return
	}
	err = a.subscriptions.DeleteByID(r.Context(), user.UID, id)
	switch {
	case errors.Is(err, push.ErrNotFound):
		writeError(w, http.StatusNotFound, "subscription not found")
	case err != nil:
		log.Printf("notificationapi: removing subscription %s of %s: %v", id, user.UID, err)
		writeError(w, http.StatusInternalServerError, "removing the subscription failed")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// subscriptionIDFor returns the id of the subscription in subs whose endpoint is
// endpoint, and whether there was one.
func subscriptionIDFor(subs []push.Subscription, endpoint string) (string, bool) {
	for _, sub := range subs {
		if sub.Endpoint == endpoint {
			return sub.ID, true
		}
	}
	return "", false
}
