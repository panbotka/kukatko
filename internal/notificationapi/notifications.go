package notificationapi

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/photos"
)

// errNotificationNotFound is the one message for a notification the caller does
// not own, whether it does not exist or belongs to somebody else.
const errNotificationNotFound = "notification not found"

// notificationView is a notification as its owner reads it. The owner is
// omitted — it is always the caller.
type notificationView struct {
	UID       string            `json:"uid"`
	Kind      notification.Kind `json:"kind"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Link      string            `json:"link"`
	CreatedAt time.Time         `json:"created_at"`
	ReadAt    *time.Time        `json:"read_at"`
}

// toNotificationView projects a stored notification onto its client-facing view.
func toNotificationView(n notification.Notification) notificationView {
	return notificationView{
		UID:       n.UID,
		Kind:      n.Kind,
		Title:     n.Title,
		Body:      n.Body,
		Link:      n.Link,
		CreatedAt: n.CreatedAt,
		ReadAt:    n.ReadAt,
	}
}

// detailView is one notification with the part of its frozen photo set the
// caller may see now.
type detailView struct {
	notificationView

	// Photos are the visible photographs in the frozen order, shaped like every
	// other photo payload of the API (media URLs included).
	Photos []photos.Photo `json:"photos"`
	// TotalCount is how many photographs of the set still exist, visible or not.
	TotalCount int `json:"total_count"`
	// DroppedCount is how many of those the caller may no longer see — archived,
	// hidden or made private since the notification was sent.
	DroppedCount int `json:"dropped_count"`
}

// droppedCount returns how many of total set members are not among the visible
// ones. It never goes below zero: the two numbers are read in separate
// statements, and a photograph deleted between them must not yield a negative.
func droppedCount(total, visible int) int {
	return max(total-visible, 0)
}

// preferencesEnvelope is both the body of a preference replace and the response
// of a read or a replace.
type preferencesEnvelope struct {
	Preferences []notification.Preference `json:"preferences"`
}

// preferenceInput is one entry of a preference replace. Enabled is a pointer so
// an entry that forgets it is refused instead of read as "off".
type preferenceInput struct {
	Kind    notification.Kind `json:"kind"`
	Enabled *bool             `json:"enabled"`
	// IsDefault is what a read reports; it is accepted so a read can be sent
	// back as a replace unchanged, and ignored — naming a kind is a choice.
	IsDefault *bool `json:"is_default"`
}

// preferencesInput is the JSON body of a preference replace. Preferences is a
// pointer so a body that forgets the field is refused instead of read as
// "everything back to its default".
type preferencesInput struct {
	Preferences *[]preferenceInput `json:"preferences"`
}

// decodePreferences decodes and validates a preference replace: the list must be
// present (it may be empty), and every entry must name a known kind and say
// whether it is enabled. A kind named twice is left for the store to refuse.
func decodePreferences(r *http.Request) ([]notification.Preference, error) {
	var in preferencesInput
	if err := decodeJSON(r, &in); err != nil {
		return nil, err
	}
	if in.Preferences == nil {
		return nil, errors.New("preferences is required")
	}
	out := make([]notification.Preference, 0, len(*in.Preferences))
	for _, entry := range *in.Preferences {
		if !entry.Kind.Known() {
			return nil, fmt.Errorf("unknown notification kind %q", entry.Kind)
		}
		if entry.Enabled == nil {
			return nil, fmt.Errorf("enabled is required for kind %q", entry.Kind)
		}
		out = append(out, notification.Preference{Kind: entry.Kind, Enabled: *entry.Enabled})
	}
	return out, nil
}

// handleGetPreferences writes the caller's effective preferences: one entry per
// known kind, the stored choice or the kind's default, so the client needs no
// knowledge of what the defaults are.
func (a *API) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	prefs, err := a.notifications.Preferences(r.Context(), user.UID)
	if err != nil {
		log.Printf("notificationapi: reading preferences of %s: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "reading preferences failed")
		return
	}
	writeJSON(w, http.StatusOK, preferencesEnvelope{Preferences: prefs})
}

// handleReplacePreferences replaces the caller's stored choices and writes the
// new effective set. A kind left out returns to its default; an unknown kind, a
// kind named twice or a malformed body is a 400.
func (a *API) handleReplacePreferences(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	prefs, err := decodePreferences(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry := audit.FromRequest(r, user.UID).Entry(audit.ActionNotificationPrefsUpdate, "users", user.UID, nil)
	stored, err := a.notifications.ReplacePreferences(r.Context(), user.UID, prefs, entry)
	switch {
	case errors.Is(err, notification.ErrUnknownKind), errors.Is(err, notification.ErrDuplicateKind):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, notification.ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "authentication required")
	case err != nil:
		log.Printf("notificationapi: replacing preferences of %s: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "replacing preferences failed")
	default:
		writeJSON(w, http.StatusOK, preferencesEnvelope{Preferences: stored})
	}
}

// handleGetNotification writes one of the caller's notifications with the part
// of its frozen photo set the caller may see now, in the frozen order. It does
// not mark the notification read; that is its own route.
func (a *API) handleGetNotification(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	uid := chi.URLParam(r, "uid")
	n, err := a.notifications.Get(r.Context(), user.UID, uid)
	if err != nil {
		writeNotificationError(w, err, "reading the notification failed")
		return
	}
	// The zero Visibility is the strict reading: whatever was archived, hidden
	// or made private since the notification was sent drops out now, whoever
	// was allowed to see it then.
	uids, err := a.notifications.Photos(r.Context(), user.UID, uid, notification.Visibility{})
	if err != nil {
		writeNotificationError(w, err, "reading the notification's photos failed")
		return
	}
	list, err := a.photos.ListByUIDs(r.Context(), uids)
	if err != nil {
		log.Printf("notificationapi: loading photos of notification %s: %v", uid, err)
		writeError(w, http.StatusInternalServerError, "reading the notification's photos failed")
		return
	}
	ordered := orderByUIDs(uids, list)
	a.media.Decorate(ordered)
	writeJSON(w, http.StatusOK, detailView{
		notificationView: toNotificationView(n),
		Photos:           ordered,
		TotalCount:       n.PhotoCount,
		DroppedCount:     droppedCount(n.PhotoCount, len(ordered)),
	})
}

// handleMarkRead marks one of the caller's notifications read and writes it.
// Marking it again keeps the moment it was first read.
func (a *API) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	n, err := a.notifications.MarkRead(r.Context(), user.UID, chi.URLParam(r, "uid"))
	if err != nil {
		writeNotificationError(w, err, "marking the notification read failed")
		return
	}
	writeJSON(w, http.StatusOK, toNotificationView(n))
}

// writeNotificationError maps a notification store error to a response: a
// notification the caller does not own is a 404 — never a 403 — and anything
// else a 500 with fallback as its message.
func writeNotificationError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, notification.ErrNotFound) {
		writeError(w, http.StatusNotFound, errNotificationNotFound)
		return
	}
	log.Printf("notificationapi: %s: %v", fallback, err)
	writeError(w, http.StatusInternalServerError, fallback)
}

// orderByUIDs returns the photos reordered to match uids, dropping any uid that
// resolved to no photo (deleted in between). ListByUIDs promises no order; the
// frozen set's order is the point of it.
func orderByUIDs(uids []string, list []photos.Photo) []photos.Photo {
	byUID := make(map[string]photos.Photo, len(list))
	for _, p := range list {
		byUID[p.UID] = p
	}
	out := make([]photos.Photo, 0, len(uids))
	for _, uid := range uids {
		if p, ok := byUID[uid]; ok {
			out = append(out, p)
		}
	}
	return out
}
