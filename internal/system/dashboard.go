package system

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// errNoDashboardCounter is returned when the service was built without a
// dashboard counter, so a mis-wired instance answers with an error instead of
// panicking on a nil interface.
var errNoDashboardCounter = errors.New("system: no dashboard counter configured")

// defaultDashboardTTL is how long the dashboard aggregation is memoised before
// the next status request recomputes it. It matches defaultLibraryTTL: the
// status page polls every few seconds and none of these numbers move faster
// than that, so one aggregation serves every poll in the window.
const defaultDashboardTTL = 30 * time.Second

// Uploads is how much arrived recently, counted over photos.created_at (when a
// photo entered the catalogue), not photos.taken_at (when it was shot): the
// question is "is anything coming in?", and a scan of 1974 negatives uploaded
// yesterday is an upload from yesterday. The windows nest — every photo in Day
// is also in Week — and archived photos are counted too, because a photo that
// arrived and was thrown away still arrived.
type Uploads struct {
	// Day is how many photos arrived in the last 24 hours.
	Day int `json:"day"`
	// Week is how many arrived in the last 7 days.
	Week int `json:"week"`
	// Month is how many arrived in the last 30 days.
	Month int `json:"month"`
	// Year is how many arrived in the last 365 days.
	Year int `json:"year"`
}

// LibrarySummary answers "what is in the library?" for the admin dashboard: the
// browsable catalogue and the three sets kept out of it, what arrived recently,
// what the library is organised by, and what it all weighs.
//
// It is deliberately a different set of numbers from Library (GET /system/stats),
// which answers "how much of the catalogue has been processed?". The one thing
// they must not do is disagree about a shared count, so both are read from the
// same store in the same shape: Photos here is the not-archived catalogue, which
// is Library.PhotosLive.
type LibrarySummary struct {
	// Photos is the library as browsed: every photo that is not in the trash.
	Photos int `json:"photos"`
	// Videos is how many of those are videos (media_type = 'video').
	Videos int `json:"videos"`
	// Trashed is how many photos are soft-deleted (photos.archived_at), i.e.
	// sitting in the trash awaiting the retention purge. They are NOT part of
	// Photos.
	Trashed int `json:"trashed"`
	// Hidden is how many not-archived photos are hidden from the library grid
	// (photos.hidden_from_library). They are part of Photos: hiding keeps a photo
	// out of the firehose, it does not take it out of the library.
	Hidden int `json:"hidden"`
	// Private is how many not-archived photos are marked private. Also part of
	// Photos.
	Private int `json:"private"`
	// Uploads is what arrived in the last day/week/month/year.
	Uploads Uploads `json:"uploads"`
	// Albums is the total number of albums of every type (hand-curated, import
	// folders and the auto-generated month/moment/state ones alike).
	Albums int `json:"albums"`
	// Labels is the total number of labels.
	Labels int `json:"labels"`
	// People is how many subjects are people. Pets and the "other" subjects are
	// deliberately left out: the tile says "people".
	People int `json:"people"`
	// Faces is the total number of detected faces across the catalogue.
	Faces int `json:"faces"`
	// Embeddings is the total number of image-embedding rows.
	Embeddings int `json:"embeddings"`
	// LibraryBytes is what the browsable library weighs: the sum of the originals'
	// file_size as the catalogue records it. It is the catalogue's own arithmetic
	// and NOT a filesystem measurement — on an instance whose originals live in an
	// object store there is nothing on the server's disk to measure, which is
	// exactly why this number exists next to StorageUsage.
	LibraryBytes int64 `json:"library_bytes"`
	// TrashBytes is what the trash weighs, on the same terms — the space a purge
	// would reclaim.
	TrashBytes int64 `json:"trash_bytes"`
	// DerivedBytes is what the derived media (thumbnails, video posters, scrub
	// storyboards) weigh in the local cache. Unlike the two above it *is* a
	// filesystem measurement (see StorageUsage.CacheBytes), because derived media
	// is never in the catalogue.
	DerivedBytes int64 `json:"derived_bytes"`
}

// RemainingWork answers "what is still to do?": the queues of human and machine
// work the library has accumulated. Every number here is a backlog — zero is the
// good value — and every one of them has a screen it is worked through on.
type RemainingWork struct {
	// FacesUnassigned is how many detected faces still name nobody. It is the set
	// the review game, the clustering and the candidate search work over.
	FacesUnassigned int `json:"faces_unassigned"`
	// Clusters is how many auto-clusters are waiting for a name. A cluster is
	// deleted when it is assigned, so this is exactly the pending pile.
	Clusters int `json:"clusters"`
	// PhotosWithoutTakenAt is how many browsable photos carry no capture time, so
	// they sit outside the timeline.
	PhotosWithoutTakenAt int `json:"photos_without_taken_at"`
	// PhotosWithoutGPS is how many browsable photos carry no coordinates, so they
	// are missing from the map.
	PhotosWithoutGPS int `json:"photos_without_gps"`
	// PhotosWithoutPlace is how many browsable photos have no cached place name.
	// It is the wider set: a photo with no coordinates can never get one, so this
	// is always at least PhotosWithoutGPS.
	PhotosWithoutPlace int `json:"photos_without_place"`
	// PhotosWithoutOCR is how many browsable stills have never been through text
	// recognition. Videos are excluded because OCR never runs on them.
	PhotosWithoutOCR int `json:"photos_without_ocr"`
	// DuplicateMarkers is how many (photo, person) pairs carry more than one valid
	// face marker — the same person tagged twice on one photo — with the pairs a
	// curator has already settled left out.
	DuplicateMarkers int `json:"duplicate_markers"`
	// Duplicates is the near-duplicate photo scan's last answer. Unlike every
	// other number here it is not a SQL count (see DuplicateScan).
	Duplicates DuplicateScan `json:"duplicates"`
}

// Dashboard is everything one CountDashboard round trip answers: the library
// summary and the SQL-countable half of the remaining work. The duplicate scan
// is filled in separately, by the service, because it is not a count.
type Dashboard struct {
	// Library is the "what is in the library?" half.
	Library LibrarySummary `json:"library"`
	// Remaining is the "what is still to do?" half, minus the duplicate scan.
	Remaining RemainingWork `json:"remaining"`
}

// DashboardCounter reads the dashboard aggregates from the catalogue. It is
// satisfied by *Store; an interface so the aggregation is unit-testable with a
// fake and so the HTTP layer never talks to the database directly.
type DashboardCounter interface {
	// CountDashboard returns the library summary and the remaining-work counts in
	// one round trip. DerivedBytes is left zero — it is a filesystem measurement,
	// not a count — and so is the duplicate scan.
	CountDashboard(ctx context.Context) (Dashboard, error)
}

// newDashboardCache returns the memoised dashboard aggregation over counter,
// recomputed at most once per TTL so a polled page cannot turn into a query
// storm. A non-positive ttl defaults to defaultDashboardTTL and a nil now to
// time.Now.
//
// A service wired without a counter yields errNoDashboardCounter rather than
// panicking on a nil interface, and neither that nor a query failure is cached
// (see snapshotCache): a dashboard of zeroes must never be mistaken for an empty
// library.
func newDashboardCache(counter DashboardCounter, ttl time.Duration, now func() time.Time) *snapshotCache[Dashboard] {
	return newSnapshotCache(func(ctx context.Context) (Dashboard, error) {
		if counter == nil {
			return Dashboard{}, errNoDashboardCounter
		}
		dash, err := counter.CountDashboard(ctx)
		if err != nil {
			return Dashboard{}, fmt.Errorf("counting dashboard: %w", err)
		}
		return dash, nil
	}, ttl, defaultDashboardTTL, now)
}
