package notificationapi

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/panbotka/kukatko/internal/notification"
)

// listEnvelope is one page of the caller's inbox: the notifications, newest
// first, how many the whole selection holds and the bounds the page was read
// with — the shape of the photo listing's paging.
type listEnvelope struct {
	Notifications []notificationView `json:"notifications"`
	Total         int                `json:"total"`
	Limit         int                `json:"limit"`
	Offset        int                `json:"offset"`
}

// unreadEnvelope is the bell's number.
type unreadEnvelope struct {
	Unread int `json:"unread"`
}

// markedEnvelope says how many notifications a "mark all read" marked.
type markedEnvelope struct {
	Marked int64 `json:"marked"`
}

// decodeListOptions reads ?unread, ?limit and ?offset. unread accepts the
// strconv.ParseBool spellings; limit must be a positive integer (clamped to
// notification.MaxListLimit by the store) and offset a non-negative one.
// Anything else is an error the handler answers with a 400.
func decodeListOptions(r *http.Request) (notification.ListOptions, error) {
	query := r.URL.Query()
	var opts notification.ListOptions
	if raw := query.Get("unread"); raw != "" {
		unread, err := strconv.ParseBool(raw)
		if err != nil {
			return opts, errors.New("unread must be true or false")
		}
		opts.Unread = unread
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return opts, errors.New("limit must be a positive integer")
		}
		opts.Limit = limit
	}
	if raw := query.Get("offset"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return opts, errors.New("offset must be a non-negative integer")
		}
		opts.Offset = offset
	}
	return opts, nil
}

// handleList writes one page of the caller's inbox, newest first. Only
// notifications delivered in-app are listed.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	opts, err := decodeListOptions(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := a.notifications.List(r.Context(), user.UID, opts)
	if err != nil {
		log.Printf("notificationapi: listing the inbox of %s: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "listing notifications failed")
		return
	}
	views := make([]notificationView, 0, len(page.Notifications))
	for _, n := range page.Notifications {
		views = append(views, toNotificationView(n))
	}
	writeJSON(w, http.StatusOK, listEnvelope{
		Notifications: views, Total: page.Total, Limit: page.Limit, Offset: page.Offset,
	})
}

// handleUnreadCount writes how many notifications in the caller's inbox are
// unread — what the bell polls.
func (a *API) handleUnreadCount(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	count, err := a.notifications.UnreadCount(r.Context(), user.UID)
	if err != nil {
		log.Printf("notificationapi: counting the unread of %s: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "counting notifications failed")
		return
	}
	writeJSON(w, http.StatusOK, unreadEnvelope{Unread: count})
}

// handleMarkAllRead marks every unread notification in the caller's inbox
// read and writes how many it marked; an inbox with nothing unread marks 0.
func (a *API) handleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	marked, err := a.notifications.MarkAllRead(r.Context(), user.UID)
	if err != nil {
		log.Printf("notificationapi: marking the inbox of %s read: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "marking notifications read failed")
		return
	}
	writeJSON(w, http.StatusOK, markedEnvelope{Marked: marked})
}
