package system

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// errNoLibraryCounter is returned by LibraryStats when the service was built
// without a library counter, so a mis-wired instance answers with an error
// instead of panicking on a nil interface.
var errNoLibraryCounter = errors.New("system: no library counter configured")

// defaultLibraryTTL is how long a library-counts aggregation is memoised before
// the next request recomputes it. It mirrors defaultStorageTTL: the numbers are
// cheap but not free, and a page that polls must not turn into a COUNT(*) storm.
const defaultLibraryTTL = 30 * time.Second

// LibraryCounter reads the library-wide counts from the catalogue. It is
// satisfied by *Store; an interface so the aggregation is unit-testable with a
// fake and so the HTTP layer never talks to the database directly.
type LibraryCounter interface {
	// CountLibrary returns the raw instance-wide counts. The derived coverage
	// gaps are filled in by the caller, not by the store.
	CountLibrary(ctx context.Context) (Library, error)
}

// Library is the library-statistics snapshot returned by GET /system/stats: a
// set of cheap instance-wide counts (never per-user, never a filesystem walk)
// modelled on photo-sorter's status page. It answers "is the catalogue fully
// processed?" — how many photos exist, how many carry an embedding or a detected
// face, and, explicitly, how many still carry neither.
//
// Note on the archived count: in Kukátko a photo is soft-deleted by stamping
// photos.archived_at, and the trash view is exactly the set of archived photos —
// there is no second, separate trash state — so PhotosArchived is both "archived"
// and "in the trash".
type Library struct {
	// Photos is every row in the catalogue, archived ones included.
	Photos int `json:"photos"`
	// Videos is how many of those are videos (media_type = 'video').
	Videos int `json:"videos"`
	// LivePhotos is how many are live photos (media_type = 'live'), i.e. a still
	// paired with a short motion clip. Not to be confused with PhotosLive, which
	// is the not-archived count.
	LivePhotos int `json:"live_photos"`
	// Images is how many are plain stills (media_type = 'image'). Derived from the
	// total and the two other media types, which is what the partial index on
	// media_type makes cheap to count.
	Images int `json:"images"`
	// PhotosLive is the catalogue as browsed: total minus archived. Derived.
	PhotosLive int `json:"photos_live"`
	// PhotosArchived is how many photos are soft-deleted, i.e. sitting in the
	// trash awaiting the retention purge.
	PhotosArchived int `json:"photos_archived"`
	// PhotosWithEmbedding is how many photos have an image embedding.
	PhotosWithEmbedding int `json:"photos_with_embedding"`
	// PhotosWithFaces is how many photos have at least one detected face.
	PhotosWithFaces int `json:"photos_with_faces"`
	// PhotosWithoutEmbedding is the embedding coverage gap: photos the sidecar
	// has not embedded yet. Derived from Photos and PhotosWithEmbedding.
	PhotosWithoutEmbedding int `json:"photos_without_embedding"`
	// PhotosWithoutFaces is the face-detection coverage gap. It counts both
	// photos not yet run through detection and photos genuinely containing no
	// face, which the counts alone cannot tell apart. Derived.
	PhotosWithoutFaces int `json:"photos_without_faces"`
	// PhotosGeocoded is how many photos carry a cached place resolved from their
	// coordinates. The `places` job also records a GPS-less photo as processed, but
	// such a row has no coordinates and is deliberately not counted here.
	PhotosGeocoded int `json:"photos_geocoded"`
	// PhotosPendingGeocode is the reverse-geocode backlog: live photos that carry
	// coordinates but have no cached place yet. Every one of them costs a metered
	// mapy.com credit to resolve, so this is the outstanding spend.
	PhotosPendingGeocode int `json:"photos_pending_geocode"`
	// Embeddings is the total number of image-embedding rows.
	Embeddings int `json:"embeddings"`
	// Faces is the total number of detected-face rows across all photos.
	Faces int `json:"faces"`
	// Subjects is the total number of named subjects (people, animals, other).
	Subjects int `json:"subjects"`
	// SubjectsPerson is how many subjects are people.
	SubjectsPerson int `json:"subjects_person"`
	// SubjectsPet is how many subjects are animals.
	SubjectsPet int `json:"subjects_pet"`
	// SubjectsOther is how many subjects are neither a person nor an animal.
	SubjectsOther int `json:"subjects_other"`
	// Markers is the total number of markers (face and label regions).
	Markers int `json:"markers"`
	// MarkersAssigned is how many markers name a subject.
	MarkersAssigned int `json:"markers_assigned"`
	// MarkersUnassigned is how many markers are still nameless. Derived.
	MarkersUnassigned int `json:"markers_unassigned"`
	// Albums is the total number of albums, of every type.
	Albums int `json:"albums"`
	// AlbumsManual is how many albums were curated by hand (type 'album').
	AlbumsManual int `json:"albums_manual"`
	// AlbumsFolder is how many albums came from an import folder (type 'folder').
	AlbumsFolder int `json:"albums_folder"`
	// AlbumsMoment is how many albums are auto-generated events (type 'moment').
	AlbumsMoment int `json:"albums_moment"`
	// AlbumsState is how many albums are auto-generated place groupings (type
	// 'state').
	AlbumsState int `json:"albums_state"`
	// AlbumsMonth is how many albums are auto-generated calendar months (type
	// 'month').
	AlbumsMonth int `json:"albums_month"`
	// Labels is the total number of labels.
	Labels int `json:"labels"`
}

// derive fills in the values that follow from the raw counts rather than from
// their own query: the live/archived split, the plain-image share of the media
// types, the two coverage gaps and the unassigned markers. Each difference is
// clamped at zero so a snapshot taken while rows are being written concurrently
// can never report a negative gap.
func (l Library) derive() Library {
	l.PhotosLive = nonNegative(l.Photos - l.PhotosArchived)
	l.Images = nonNegative(l.Photos - l.Videos - l.LivePhotos)
	l.PhotosWithoutEmbedding = nonNegative(l.Photos - l.PhotosWithEmbedding)
	l.PhotosWithoutFaces = nonNegative(l.Photos - l.PhotosWithFaces)
	l.MarkersUnassigned = nonNegative(l.Markers - l.MarkersAssigned)
	return l
}

// nonNegative returns value, or zero when it is negative.
func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

// libraryCache memoises the library counts for a short TTL so a polled page does
// not re-run the aggregation on every request. Only successful aggregations are
// cached: a failure must reach the caller, which reports an error rather than
// letting the page render zeroes as if they were real counts. It is safe for
// concurrent use.
type libraryCache struct {
	counter LibraryCounter
	ttl     time.Duration
	now     func() time.Time

	mu         sync.Mutex
	cached     Library
	computedAt time.Time
	valid      bool
}

// newLibraryCache returns a libraryCache over counter. A non-positive ttl
// defaults to defaultLibraryTTL and a nil now defaults to time.Now, so callers
// may leave them unset.
func newLibraryCache(counter LibraryCounter, ttl time.Duration, now func() time.Time) *libraryCache {
	if ttl <= 0 {
		ttl = defaultLibraryTTL
	}
	if now == nil {
		now = time.Now
	}
	return &libraryCache{counter: counter, ttl: ttl, now: now}
}

// counts returns the memoised library counts, recomputing them when the cached
// value is older than the TTL (or has never been computed). It returns an error
// when the aggregation fails and nothing valid is cached to fall back on; the
// stale value is never served past its TTL.
func (c *libraryCache) counts(ctx context.Context) (Library, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.valid && c.now().Sub(c.computedAt) < c.ttl {
		return c.cached, nil
	}
	if c.counter == nil {
		return Library{}, errNoLibraryCounter
	}
	raw, err := c.counter.CountLibrary(ctx)
	if err != nil {
		return Library{}, fmt.Errorf("counting library: %w", err)
	}
	c.cached = raw.derive()
	c.computedAt = c.now()
	c.valid = true
	return c.cached, nil
}
