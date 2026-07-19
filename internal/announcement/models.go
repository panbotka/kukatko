// Package announcement is the database access layer for the single instance-wide
// announcement: one short message a maintainer publishes from the admin area and
// every signed-in user then sees as a banner at the top of the app. There is at
// most one announcement at a time, enforced by a single-row table (see migration
// 0039), so publishing is an upsert and clearing is a delete.
//
// The store keeps no state beyond the shared pgx pool. Publishing and clearing
// are audited in the same transaction as the mutation (mirroring internal/organize),
// so the record of who changed the banner commits atomically with the change or
// not at all. Access control (any signed-in user reads, only a maintainer writes)
// is enforced by the HTTP layer above this store.
package announcement

import (
	"errors"
	"time"
)

// Announcement levels drive the banner variant on the client. They are stored
// verbatim in the level column and guarded by a CHECK constraint.
const (
	// LevelInfo is the neutral, informational banner variant (the default).
	LevelInfo = "info"
	// LevelWarning is the attention-grabbing banner variant, for things like an
	// imminent outage.
	LevelWarning = "warning"
)

// ErrNotFound is returned by Get when no announcement is currently published.
var ErrNotFound = errors.New("announcement not found")

// ErrEmptyMessage is returned by Set when the message is blank: an empty banner
// is meaningless, and clearing the announcement is done with Clear, not by
// publishing an empty message.
var ErrEmptyMessage = errors.New("announcement message must not be empty")

// ErrInvalidLevel is returned by Set when the level is not one of the recognised
// LevelInfo/LevelWarning values.
var ErrInvalidLevel = errors.New("announcement level must be 'info' or 'warning'")

// Announcement is the single instance-wide banner message. AuthorUID is empty
// when the publishing user has since been deleted (the column cascades to NULL).
type Announcement struct {
	// Message is the maintainer-authored banner text shown to every user.
	Message string `json:"message"`
	// Level is the banner variant, one of LevelInfo or LevelWarning.
	Level string `json:"level"`
	// AuthorUID is the UID of the user who last published the announcement, or
	// empty when that user has since been deleted.
	AuthorUID string `json:"author_uid"`
	// UpdatedAt is when the announcement was last published; the frontend keys a
	// per-user "dismissed" flag on it so a freshly published message reappears.
	UpdatedAt time.Time `json:"updated_at"`
}

// normalizeLevel validates and defaults an announcement level: an empty level
// becomes LevelInfo, LevelInfo/LevelWarning pass through unchanged, and anything
// else yields ErrInvalidLevel so a bad value never reaches the CHECK-guarded column.
func normalizeLevel(level string) (string, error) {
	switch level {
	case "":
		return LevelInfo, nil
	case LevelInfo, LevelWarning:
		return level, nil
	default:
		return "", ErrInvalidLevel
	}
}
