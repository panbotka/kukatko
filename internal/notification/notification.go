// Package notification is the record behind a push notification: who it was
// for, what kind it was, the text and deeplink it was sent with, when it was
// read, and — for the kinds that are about photographs — the frozen, ordered set
// of photographs tapping it opens. It also keeps each account's choice of which
// kinds it wants at all.
//
// A notification is a record rather than a transient message because it has to
// survive being tapped: "you were tagged in 12 photos" opens exactly those 12,
// even after a thirteenth is tagged and even the next morning. The shape follows
// internal/phototask, whose question is about a frozen group of photos too.
//
// The package sends nothing and serves nothing; delivery (internal/pushjob) and
// the HTTP surface are separate. Every read is scoped to the owning account, and
// a foreign uid is indistinguishable from a missing one (ErrNotFound), so a uid
// cannot be probed for existence.
package notification

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Kind names what a notification is about. It is stored verbatim in the kind
// columns, which deliberately carry no CHECK: adding a kind costs a constant
// here, an entry in registry and a locale string — never a migration.
type Kind string

const (
	// KindTagged is "you were tagged in photos": somebody marked the account's
	// linked person on one or more photographs. Its photo set is those photos.
	KindTagged Kind = "tagged"
	// KindRegistrationPending is "a registration is waiting for approval",
	// sent to the accounts that may approve one. It carries no photographs.
	KindRegistrationPending Kind = "registration_pending"
)

// registry is every known kind in display order, with whether an account that
// never chose wants it. The order is the order Preferences reports them in.
var registry = []struct {
	kind Kind
	def  bool
}{
	{kind: KindTagged, def: true},
	{kind: KindRegistrationPending, def: true},
}

// Kinds returns every known kind, in display order, as a fresh slice.
func Kinds() []Kind {
	out := make([]Kind, 0, len(registry))
	for _, entry := range registry {
		out = append(out, entry.kind)
	}
	return out
}

// Known reports whether k is one of the kinds this package defines.
func (k Kind) Known() bool {
	_, ok := k.lookup()
	return ok
}

// Default reports whether an account that never chose wants kind k. An unknown
// kind is never wanted.
func (k Kind) Default() bool {
	def, _ := k.lookup()
	return def
}

// lookup returns k's default and whether k is a known kind at all.
func (k Kind) lookup() (bool, bool) {
	for _, entry := range registry {
		if entry.kind == k {
			return entry.def, true
		}
	}
	return false, false
}

// Limits on what one notification may carry. A notification is shown on a lock
// screen, so its text is short; the photo cap bounds one insert, not the
// library.
const (
	// MaxTitleLen is the longest title, in characters.
	MaxTitleLen = 200
	// MaxBodyLen is the longest body, in characters.
	MaxBodyLen = 1000
	// MaxLinkLen is the longest deeplink path, in bytes.
	MaxLinkLen = 2048
	// MaxPhotos is the most photographs one notification's set may hold.
	MaxPhotos = 5000
)

// Retention defaults: read notifications go after a month, unread ones after a
// quarter — an unread one is still owed a look, so it is kept longer.
const (
	// DefaultReadRetention is how long a read notification is kept.
	DefaultReadRetention = 30 * 24 * time.Hour
	// DefaultUnreadRetention is how long an unread notification is kept.
	DefaultUnreadRetention = 90 * 24 * time.Hour
)

// Sentinel errors. Callers branch on them with errors.Is.
var (
	// ErrNotFound means no notification with that uid belongs to the asking
	// account — whether it does not exist or belongs to somebody else.
	ErrNotFound = errors.New("notification: not found")
	// ErrUnknownKind means a kind this package does not define.
	ErrUnknownKind = errors.New("notification: unknown kind")
	// ErrInvalid means a notification that breaks one of the field rules.
	ErrInvalid = errors.New("notification: invalid notification")
	// ErrTooManyPhotos means a photo set over MaxPhotos.
	ErrTooManyPhotos = errors.New("notification: too many photos")
	// ErrPhotoNotFound means a photo set naming a photograph that does not exist.
	ErrPhotoNotFound = errors.New("notification: photo not found")
	// ErrUserNotFound means a notification or preference for an account that
	// does not exist.
	ErrUserNotFound = errors.New("notification: user not found")
	// ErrDuplicateKind means a preference replace naming the same kind twice.
	ErrDuplicateKind = errors.New("notification: kind given twice")
	// ErrInvalidRetention means a purge with a non-positive threshold or an
	// unread threshold shorter than the read one.
	ErrInvalidRetention = errors.New("notification: invalid retention")
)

// Notification is one stored notification as its owner reads it.
type Notification struct {
	// UID is the public identifier, "nt" followed by random base32 characters.
	UID string `json:"uid"`
	// UserUID is the account the notification belongs to.
	UserUID string `json:"user_uid"`
	// Kind is what the notification is about.
	Kind Kind `json:"kind"`
	// Title is the title text exactly as it was sent.
	Title string `json:"title"`
	// Body is the body text exactly as it was sent; may be empty.
	Body string `json:"body"`
	// Link is the in-app deeplink path tapping the notification opens; may be
	// empty for a notification that opens nothing in particular.
	Link string `json:"link"`
	// CreatedAt is when the notification was recorded.
	CreatedAt time.Time `json:"created_at"`
	// ReadAt is when the owner first read it; nil while unread.
	ReadAt *time.Time `json:"read_at,omitempty"`
	// PhotoCount is how many photographs of the frozen set still exist. It
	// counts deleted ones out (the cascade removed them) but not archived,
	// hidden or private ones — those are the reader's to filter, see Photos.
	PhotoCount int `json:"photo_count"`
}

// New is a notification to record: everything but what the store assigns.
type New struct {
	// UserUID is the account the notification is for. Required.
	UserUID string
	// Kind must be a known kind.
	Kind Kind
	// Title is required, at most MaxTitleLen characters.
	Title string
	// Body is optional, at most MaxBodyLen characters.
	Body string
	// Link is an in-app path ("/photos?…"): empty, or starting with a single
	// "/", at most MaxLinkLen bytes. An absolute URL or a scheme-relative
	// "//host" is refused — a notification must never open another site.
	Link string
	// SelfLink stores the notification's own page, Path(uid), as its link —
	// the uid is assigned by the store, so a caller cannot spell that path
	// itself. Link must be empty when it is set.
	SelfLink bool
	// PhotoUIDs is the frozen set in the order it is to be shown. A photograph
	// given twice keeps its first position. Nil or empty for a kind with no
	// photographs.
	PhotoUIDs []string
}

// PathPrefix is the in-app route of one notification's own page, the uid
// appended. It is deliberately short: the path travels inside a push payload,
// which has a hard size limit. The frontend page behind it opens the frozen
// photo set (one photo straight in the viewer, several as a grid).
const PathPrefix = "/n/"

// Path returns the in-app path of notification uid's own page.
func Path(uid string) string {
	return PathPrefix + uid
}

// link returns the deeplink to store for n once the store assigned uid: its own
// page when SelfLink is set, the given Link otherwise.
func (n New) link(uid string) string {
	if n.SelfLink {
		return Path(uid)
	}
	return n.Link
}

// validate checks n against the field rules and returns the normalised photo
// set (first occurrence wins). It returns ErrInvalid, ErrUnknownKind or
// ErrTooManyPhotos, wrapped with the rule that failed.
func (n New) validate() ([]string, error) {
	switch {
	case strings.TrimSpace(n.UserUID) == "":
		return nil, fmt.Errorf("%w: no account", ErrInvalid)
	case !n.Kind.Known():
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, n.Kind)
	case strings.TrimSpace(n.Title) == "":
		return nil, fmt.Errorf("%w: empty title", ErrInvalid)
	case utf8.RuneCountInString(n.Title) > MaxTitleLen:
		return nil, fmt.Errorf("%w: title over %d characters", ErrInvalid, MaxTitleLen)
	case utf8.RuneCountInString(n.Body) > MaxBodyLen:
		return nil, fmt.Errorf("%w: body over %d characters", ErrInvalid, MaxBodyLen)
	}
	if n.SelfLink && n.Link != "" {
		return nil, fmt.Errorf("%w: both a link and a link to itself", ErrInvalid)
	}
	if err := validateLink(n.Link); err != nil {
		return nil, err
	}
	photos := dedupe(n.PhotoUIDs)
	if len(photos) > MaxPhotos {
		return nil, fmt.Errorf("%w: %d over the limit of %d", ErrTooManyPhotos, len(photos), MaxPhotos)
	}
	return photos, nil
}

// validateLink accepts an empty link or an in-app absolute path. It refuses a
// scheme-relative "//host" and a backslash (browsers read "/\host" as another
// host), and anything over MaxLinkLen bytes.
func validateLink(link string) error {
	switch {
	case link == "":
		return nil
	case len(link) > MaxLinkLen:
		return fmt.Errorf("%w: link over %d bytes", ErrInvalid, MaxLinkLen)
	case !strings.HasPrefix(link, "/"), strings.HasPrefix(link, "//"), strings.Contains(link, `\`):
		return fmt.Errorf("%w: link %q is not an in-app path", ErrInvalid, link)
	}
	return nil
}

// dedupe returns uids without empty entries and without repeats, keeping each
// uid at its first position.
func dedupe(uids []string) []string {
	out := make([]string, 0, len(uids))
	seen := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		if uid == "" {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		out = append(out, uid)
	}
	return out
}

// Visibility is what the caller of Photos lets through besides ordinary live
// photographs. The zero value is the strict reading — no archived, no hidden,
// no private photograph — which is what a notification page shows an ordinary
// viewer; a caller entitled to more says so explicitly.
type Visibility struct {
	// IncludeArchived keeps photographs that were archived since the
	// notification was sent.
	IncludeArchived bool
	// IncludeHidden keeps photographs hidden from the library.
	IncludeHidden bool
	// IncludePrivate keeps photographs flagged private.
	IncludePrivate bool
}

// VisiblePhotoSQL is the zero Visibility — no archived, no hidden, no private
// photograph — as a SQL predicate over the photos table aliased p. Photos
// applies the same three filters when a notification is read; a producer that
// decides whether a photograph may be named in a notification at all (the
// tagging window, internal/tagnotifyjob) uses this one definition rather than
// spelling its own, so what is announced and what the page later shows agree.
const VisiblePhotoSQL = "(p.archived_at IS NULL AND NOT p.hidden_from_library AND NOT p.private)"

// Retention is the purge's two age thresholds, both measured from when a
// notification was recorded.
type Retention struct {
	// Read is the age past which a read notification is deleted.
	Read time.Duration
	// Unread is the age past which an unread notification is deleted. It must
	// not be shorter than Read: an unread notification is still owed a look.
	Unread time.Duration
}

// DefaultRetention returns the retention the package recommends.
func DefaultRetention() Retention {
	return Retention{Read: DefaultReadRetention, Unread: DefaultUnreadRetention}
}

// validate returns ErrInvalidRetention unless both thresholds are positive and
// Unread is at least Read.
func (r Retention) validate() error {
	if r.Read <= 0 || r.Unread <= 0 || r.Unread < r.Read {
		return fmt.Errorf("%w: read %s, unread %s", ErrInvalidRetention, r.Read, r.Unread)
	}
	return nil
}

// Preference is whether an account wants one kind.
type Preference struct {
	// Kind is the notification kind.
	Kind Kind `json:"kind"`
	// Enabled is whether the account wants it.
	Enabled bool `json:"enabled"`
	// IsDefault is true when the account never chose and Enabled is the kind's
	// default. It is ignored on input.
	IsDefault bool `json:"is_default"`
}

// validatePrefs refuses an unknown kind (ErrUnknownKind) and a kind given twice
// (ErrDuplicateKind) — a replace must say one thing per kind rather than let the
// last entry win silently.
func validatePrefs(prefs []Preference) error {
	seen := make(map[Kind]struct{}, len(prefs))
	for _, pref := range prefs {
		if !pref.Kind.Known() {
			return fmt.Errorf("%w: %q", ErrUnknownKind, pref.Kind)
		}
		if _, dup := seen[pref.Kind]; dup {
			return fmt.Errorf("%w: %q", ErrDuplicateKind, pref.Kind)
		}
		seen[pref.Kind] = struct{}{}
	}
	return nil
}

// effective merges stored choices over the defaults: one Preference per known
// kind, in registry order. A stored row for a kind no longer defined is ignored.
func effective(stored map[Kind]bool) []Preference {
	out := make([]Preference, 0, len(registry))
	for _, entry := range registry {
		enabled, chosen := stored[entry.kind]
		if !chosen {
			enabled = entry.def
		}
		out = append(out, Preference{Kind: entry.kind, Enabled: enabled, IsDefault: !chosen})
	}
	return out
}
