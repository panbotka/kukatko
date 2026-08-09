package geoestimate

// Reviewing what the estimator guessed.
//
// BackfillLocations invents coordinates and marks them 'estimate' so nothing
// downstream can mistake a guess for a measurement. That marking is only half a
// feature: until somebody rules on it the photo sits on the map forever wearing a
// question mark, and the estimator itself will never revisit it (an estimated
// photo has stopped being a candidate). This file is the other half — the
// listing a human is asked about, and the two verdicts.
//
// Both verdicts go through the ordinary metadata write path
// (photos.UpdateMetadataAudited), read-modify-write against the current row, so
// they are exactly the edits a user could make by hand on the photo page and the
// audit row commits in the mutation's transaction. Only the audit action differs
// from a hand edit, and deliberately: 'location.confirm'/'location.reject' make
// the decision countable, where a photo.update would only record a diff.
//
// What each verdict means is asymmetric, and the asymmetry is the point:
//
//   - accept — the coordinates are right. They stay; location_source is promoted
//     to 'manual', the same value the photo page stamps when a user places a pin.
//     From then on nothing presents them as a guess and the estimator will not
//     touch them.
//   - reject — the coordinates are wrong. They are cleared, and location_source
//     is stamped 'manual' anyway: a location the user deleted is a decision, not a
//     gap, and that tombstone is what stops the next nightly backfill handing the
//     very same guess straight back.

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
)

// CatalogueStore is the photo access the review path needs: the pending window,
// its size, and the read-modify-write of one photo's metadata. *photos.Store
// satisfies it.
type CatalogueStore interface {
	// ListEstimatedLocations returns one window of the photos carrying an
	// estimated location, ordered by uid.
	ListEstimatedLocations(ctx context.Context, offset, limit int) ([]photos.Photo, error)
	// CountEstimatedLocations returns how many such photos there are in total.
	CountEstimatedLocations(ctx context.Context) (int, error)
	// GetByUID returns one photo or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
	// UpdateMetadataAudited overwrites a photo's editable metadata, writing entry
	// in the same transaction.
	UpdateMetadataAudited(ctx context.Context, uid string, m photos.MetadataUpdate,
		entry audit.Entry) (photos.Photo, error)
}

// PlaceStore names where an estimated coordinate is thought to be. *places.Store
// satisfies it. It is optional: without a geocode the question still has a
// photo and a coordinate, it just cannot spell the place out.
type PlaceStore interface {
	// PlacesByPhotoUIDs returns the cached place rows for the given photos.
	PlacesByPhotoUIDs(ctx context.Context, photoUIDs []string) ([]places.Place, error)
}

// Pending is one photo whose location the estimator guessed and nobody has
// ruled on yet, together with the cached place those coordinates reverse-geocoded
// to. (The estimator's own Estimate is the pure geometry function; this is the
// row a human is about to be asked about.)
type Pending struct {
	// Photo is the full catalogue record, so a caller can render it without a
	// second read.
	Photo photos.Photo
	// Place is the cached place hierarchy for the guessed coordinates. Its zero
	// value means the photo has not been reverse geocoded (yet, or at all — the
	// geocoder is rate-limited and optional), in which case only the coordinates
	// themselves are known.
	Place places.Place
}

// PlaceLabel returns the most specific human-readable name of the estimated
// place — the place name, else the city, else the region, else the country — or
// "" when the coordinates have not been geocoded. It is what a question can put
// in front of a person; a caller with "" has to fall back to asking about the
// coordinates alone, or skip the photo.
func (e Pending) PlaceLabel() string {
	for _, candidate := range []string{e.Place.PlaceName, e.Place.City, e.Place.Region, e.Place.Country} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// ReviewConfig bundles the Reviewer's collaborators. Catalogue is required;
// Places may be nil, which leaves every Pending's place empty.
type ReviewConfig struct {
	// Catalogue reads the pending estimates and applies the verdicts.
	Catalogue CatalogueStore
	// Places names the guessed coordinates. May be nil.
	Places PlaceStore
}

// Reviewer lists the estimator's unresolved guesses and applies a human verdict
// to one of them. It is read-mostly and safe for concurrent use; it holds no
// state of its own.
type Reviewer struct {
	catalogue CatalogueStore
	places    PlaceStore
}

// NewReviewer returns a Reviewer from cfg. It panics when Catalogue is nil — a
// wiring bug, not a runtime condition, matching the other services here.
func NewReviewer(cfg ReviewConfig) *Reviewer {
	if cfg.Catalogue == nil {
		panic("geoestimate: NewReviewer requires a Catalogue")
	}
	return &Reviewer{catalogue: cfg.Catalogue, places: cfg.Places}
}

// Pending returns one window of the photos awaiting a verdict on their estimated
// location — at most limit of them starting at offset, ordered by uid — plus the
// total size of that set, so a caller can rotate a cursor over it without
// reading it all. A non-positive limit returns nothing but still reports the
// total.
func (r *Reviewer) Pending(ctx context.Context, offset, limit int) ([]Pending, int, error) {
	total, err := r.catalogue.CountEstimatedLocations(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("geoestimate: counting pending estimates: %w", err)
	}
	if total == 0 || limit <= 0 {
		return nil, total, nil
	}
	rows, err := r.catalogue.ListEstimatedLocations(ctx, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("geoestimate: listing pending estimates: %w", err)
	}
	byUID, err := r.placesFor(ctx, rows)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Pending, 0, len(rows))
	for _, photo := range rows {
		out = append(out, Pending{Photo: photo, Place: byUID[photo.UID]})
	}
	return out, total, nil
}

// placesFor resolves the cached places of a window of photos in one read. With
// no place store wired it returns an empty map, which leaves every estimate's
// place zero rather than failing — a missing geocode is a normal state.
func (r *Reviewer) placesFor(ctx context.Context, rows []photos.Photo) (map[string]places.Place, error) {
	byUID := make(map[string]places.Place, len(rows))
	if r.places == nil || len(rows) == 0 {
		return byUID, nil
	}
	uids := make([]string, 0, len(rows))
	for _, photo := range rows {
		uids = append(uids, photo.UID)
	}
	found, err := r.places.PlacesByPhotoUIDs(ctx, uids)
	if err != nil {
		return nil, fmt.Errorf("geoestimate: loading places for review: %w", err)
	}
	for _, place := range found {
		byUID[place.PhotoUID] = place
	}
	return byUID, nil
}

// Accept records that an estimated location is right: the coordinates stay and
// location_source is promoted from 'estimate' to 'manual', so nothing presents
// them as a guess any more. It is idempotent — a photo whose location is already
// 'manual' is simply written back unchanged — and returns
// photos.ErrPhotoNotFound when the photo has gone.
//
// A photo whose location is no longer an estimate (somebody edited it meanwhile)
// is left exactly as it is: the verdict was about a guess that no longer exists,
// and overwriting a decision with a stale one would be worse than doing nothing.
func (r *Reviewer) Accept(ctx context.Context, photoUID string, meta audit.Meta) error {
	return r.decide(ctx, photoUID, meta, audit.ActionLocationConfirm, func(update *photos.MetadataUpdate) {
		update.LocationSource = photos.LocationSourceManual
	})
}

// Reject records that an estimated location is wrong: the coordinates are
// cleared and location_source is stamped 'manual', the tombstone that keeps the
// nightly backfill from re-adding the guess the user just threw away. It is
// idempotent and returns photos.ErrPhotoNotFound when the photo has gone. Like
// Accept it leaves a photo whose location is no longer an estimate alone.
func (r *Reviewer) Reject(ctx context.Context, photoUID string, meta audit.Meta) error {
	return r.decide(ctx, photoUID, meta, audit.ActionLocationReject, func(update *photos.MetadataUpdate) {
		update.Lat, update.Lng, update.Altitude = nil, nil, nil
		update.LocationSource = photos.LocationSourceManual
	})
}

// decide applies one verdict: re-read the row, refuse unless it still carries an
// estimate, let apply shape the location fields, and write the whole record back
// with the verdict's own audit action in the mutation's transaction.
//
// The read-modify-write is not incidental. UpdateMetadata overwrites the entire
// editable record, so building the update from anything other than the current
// row would silently blank a title or a date somebody typed in between the queue
// being built and the answer arriving.
func (r *Reviewer) decide(
	ctx context.Context, photoUID string, meta audit.Meta, action string,
	apply func(*photos.MetadataUpdate),
) error {
	current, err := r.catalogue.GetByUID(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("geoestimate: loading photo %s: %w", photoUID, err)
	}
	if current.LocationSource != photos.LocationSourceEstimate {
		return nil
	}
	update := metadataOf(current)
	apply(&update)
	details := map[string]any{"via": audit.ViaReview, "lat": current.Lat, "lng": current.Lng}
	entry := meta.Entry(action, "photos", photoUID, details)
	if _, err := r.catalogue.UpdateMetadataAudited(ctx, photoUID, update, entry); err != nil {
		return fmt.Errorf("geoestimate: applying location verdict to %s: %w", photoUID, err)
	}
	return nil
}

// metadataOf projects a photo onto the full editable record UpdateMetadata
// overwrites with, so a caller only has to change the fields it means to change.
// It mirrors internal/photoapi's mergeUpdate baseline; the private flag is
// carried through for the same reason it is there — the importers still write it
// and a full-record replace would otherwise clear it.
func metadataOf(p photos.Photo) photos.MetadataUpdate {
	return photos.MetadataUpdate{
		Title:            p.Title,
		TitleEdited:      p.TitleEdited,
		Description:      p.Description,
		Notes:            p.Notes,
		AiNote:           p.AiNote,
		Subject:          p.Subject,
		Keywords:         p.Keywords,
		Artist:           p.Artist,
		Copyright:        p.Copyright,
		License:          p.License,
		Scan:             p.Scan,
		TakenAt:          p.TakenAt,
		TakenAtSource:    p.TakenAtSource,
		TakenAtEstimated: p.TakenAtEstimated,
		TakenAtNote:      p.TakenAtNote,
		TakenAtPrecision: p.TakenAtPrecision,
		Lat:              p.Lat,
		Lng:              p.Lng,
		Altitude:         p.Altitude,
		LocationSource:   p.LocationSource,
		Private:          p.Private,
	}
}
