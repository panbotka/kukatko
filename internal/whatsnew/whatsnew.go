// Package whatsnew answers one question for a returning reader: what happened in
// the library while I was away?
//
// It is a digest, not a feed. The library is shared by a family, so somebody else
// uploads an evening's photographs, names a face, starts an album — and the next
// person to open the app has no way of noticing. The summary this package
// produces is deliberately small: how many photos arrived, which albums were
// created, which people were named, how many comments were written. Anything
// bigger would be a second timeline competing with the real one.
//
// # What a visit is
//
// The digest needs a reference point ("since when?"), and the hard part is that
// it must survive a reload. A reader who refreshes the library, walks to an
// album and comes back must see the same digest, not an empty one — otherwise
// the panel destroys itself the moment it is used.
//
// So a visit is defined server-side by two timestamps on the account (migration
// 0053_user_visits):
//
// The digest also reports how many of the new photographs the reader is on,
// which is the one line of it that is about them rather than about the library.
// It needs the account's linked person (users.subject_uid), read in the same
// statement that stamps the visit — see [Store.Summary].
//
//   - last_seen_at is stamped to now on every summary read. It is a heartbeat:
//     while the reader is around, it keeps moving.
//   - visit_reference_at is the digest's "since". It moves only when a new visit
//     begins — a gap of at least [VisitGap] between two reads — and on that
//     transition it takes the previous last_seen_at, the last moment the reader
//     was demonstrably present last time.
//
// Within one visit the reference does not move at all, however many times the
// page is loaded. A first-ever read has no reference (NULL) and therefore no
// digest: a new account does not want its whole library announced as news.
//
// Because only the summary read stamps the heartbeat, "inactivity" means
// precisely "the library home was not opened", which is the surface the panel
// lives on. That is the intended reading: the digest covers the time since the
// reader was last at the front door.
//
// # Cost
//
// Every count is a range over a creation timestamp backed by its own index
// (0053, plus idx_photos_live_created_at from 0015), so the work is proportional
// to what is new rather than to the size of the library. The package only reads
// the catalogue; the sole write it makes is the visit bookkeeping on the caller's
// own account row.
package whatsnew

import (
	"errors"
	"time"
)

// ErrUserNotFound indicates the account the summary was requested for no longer
// exists — it was deleted between authenticating the request and stamping the
// visit. Callers treat it as "no digest" rather than as a failure.
var ErrUserNotFound = errors.New("whatsnew: user not found")

// VisitGap is how long an account must go without reading the summary before the
// next read counts as a new visit and rotates the reference point.
//
// Six hours is the shortest span the requirement allows, and it is the right end
// of the range for a family library: it makes "morning" and "evening" separate
// visits (so the evening reader learns what arrived during the day) while a
// single sitting — including a long lunch in the middle of it — stays one visit
// with one stable digest.
const VisitGap = 6 * time.Hour

// MaxItems caps how many albums and how many people the digest names. The panel
// is a glance, not a list; past a handful of links it stops being readable, and
// the count that accompanies the links still reports the full total.
const MaxItems = 6

// Album is a newly created album, named and linked in the digest.
type Album struct {
	UID   string `json:"uid"`
	Title string `json:"title"`
}

// Person is a newly named subject, named and linked in the digest.
type Person struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

// Summary is the digest for one reader: what changed in the library between
// Since and now.
//
// HasNews is the single flag the client branches on. It is false both for a
// first-ever visit (there is no reference point yet) and for a visit that found
// nothing at all, because in both cases the panel must not appear — an empty
// "nothing happened" box is worse than no box.
//
// Albums and People are capped at [MaxItems] entries while AlbumCount and
// PersonCount report the true totals, so a digest can honestly say "8 new
// people" and still link only the first six.
// MinePhotos is how many of those new photos the reader themselves is on. It is
// zero — and the client shows no line for it — both for an account that has not
// said which person it is and for a visit where none of the new photographs was
// of them; an empty "0 new photos of you" is noise, and the digest is already
// telling them how many arrived in total.
type Summary struct {
	HasNews     bool      `json:"has_news"`
	Since       time.Time `json:"since,omitzero"`
	Photos      int       `json:"photos"`
	MinePhotos  int       `json:"mine_photos"`
	Comments    int       `json:"comments"`
	Albums      []Album   `json:"albums,omitempty"`
	AlbumCount  int       `json:"album_count"`
	People      []Person  `json:"people,omitempty"`
	PersonCount int       `json:"person_count"`
}

// counts holds the "how many since" numbers, kept together so the assembly of a
// Summary is a pure function of them.
type counts struct {
	photos   int
	comments int
	albums   int
	people   int
	// mine counts the new photos the reader appears on. It is a subset of photos
	// — same base predicate, plus a marker naming the reader's linked person — so
	// it can never be the only thing that happened, and empty() ignores it.
	mine int
}

// empty reports whether nothing at all happened since the reference point, which
// is what decides that no panel is shown.
func (c counts) empty() bool {
	return c.photos == 0 && c.comments == 0 && c.albums == 0 && c.people == 0
}

// newSummary assembles the digest from the reference point, the counts and the
// (already capped) album and people lists. It returns a zero-value Summary —
// HasNews false — when nothing happened, so the caller never has to decide
// separately whether the panel should appear.
func newSummary(since time.Time, c counts, albums []Album, people []Person) Summary {
	if c.empty() {
		return Summary{}
	}
	return Summary{
		HasNews:     true,
		Since:       since,
		Photos:      c.photos,
		MinePhotos:  c.mine,
		Comments:    c.comments,
		Albums:      albums,
		AlbumCount:  c.albums,
		People:      people,
		PersonCount: c.people,
	}
}
