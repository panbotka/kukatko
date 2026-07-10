package psimport

import (
	"context"
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/photosorter"
)

// transferPhash copies the photo-sorter perceptual hashes onto the Kukátko photo
// (idempotent upsert). It is best-effort: a failure is logged, not propagated.
func (s *Service) transferPhash(ctx context.Context, kkUID string, ps photosorter.Photo) {
	ph, ok, err := s.src.Phash(ctx, ps.UID)
	if err != nil {
		s.log.Warn("psimport: reading phash", "ps_uid", ps.UID, "err", err)
		return
	}
	if !ok {
		return
	}
	if err := s.photos.SetPhash(ctx, photos.Phash{PhotoUID: kkUID, Phash: ph.Phash, Dhash: ph.Dhash}); err != nil {
		s.log.Warn("psimport: setting phash", "photo", kkUID, "err", err)
	}
}

// transferEdit copies the photo-sorter non-destructive edits onto the Kukátko
// photo (idempotent upsert). Best-effort: a failure is logged, not propagated.
func (s *Service) transferEdit(ctx context.Context, kkUID string, ps photosorter.Photo) {
	e, ok, err := s.src.Edit(ctx, ps.UID)
	if err != nil {
		s.log.Warn("psimport: reading edit", "ps_uid", ps.UID, "err", err)
		return
	}
	if !ok {
		return
	}
	if err := s.photos.SetEdit(ctx, photos.Edit{
		PhotoUID:   kkUID,
		CropX:      e.CropX,
		CropY:      e.CropY,
		CropW:      e.CropW,
		CropH:      e.CropH,
		Rotation:   e.Rotation,
		Brightness: e.Brightness,
		Contrast:   e.Contrast,
	}); err != nil {
		s.log.Warn("psimport: setting edit", "photo", kkUID, "err", err)
	}
}

// transferMarkers migrates the photo-sorter markers onto the Kukátko photo,
// preserving each marker UID (so the migrated faces' cached marker_uid stays
// valid) and remapping the subject. Best-effort per marker: a failure is logged.
func (s *Service) transferMarkers(ctx context.Context, kkUID string, ps photosorter.Photo, maps mappings) {
	markers, err := s.src.Markers(ctx, ps.UID)
	if err != nil {
		s.log.Warn("psimport: listing markers", "ps_uid", ps.UID, "err", err)
		return
	}
	for i := range markers {
		if err := s.transferOneMarker(ctx, kkUID, markers[i], maps); err != nil {
			s.log.Warn("psimport: migrating marker", "marker", markers[i].UID, "err", err)
		}
	}
}

// transferOneMarker creates a Kukátko marker mirroring a photo-sorter one,
// preserving its UID for idempotency (an already-migrated marker is skipped) and
// remapping the subject onto Kukátko's.
func (s *Service) transferOneMarker(
	ctx context.Context, kkUID string, m photosorter.Marker, maps mappings,
) error {
	_, err := s.people.GetMarkerByUID(ctx, m.UID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, people.ErrMarkerNotFound) {
		return fmt.Errorf("looking up marker %s: %w", m.UID, err)
	}
	if _, err := s.people.CreateMarker(ctx, people.Marker{
		UID:        m.UID,
		PhotoUID:   kkUID,
		SubjectUID: remapSubject(m.SubjectUID, maps.subjects),
		Type:       mapMarkerType(m.Type),
		X:          m.X,
		Y:          m.Y,
		W:          m.W,
		H:          m.H,
		Score:      m.Score,
		Invalid:    m.Invalid,
		Reviewed:   m.Reviewed,
	}); err != nil {
		return fmt.Errorf("creating marker %s: %w", m.UID, err)
	}
	return nil
}

// transferMemberships attaches the photo to its mapped albums and labels,
// mirroring photo-sorter membership. Best-effort: per-row failures are logged.
func (s *Service) transferMemberships(ctx context.Context, kkUID string, ps photosorter.Photo, maps mappings) {
	s.transferAlbumMemberships(ctx, kkUID, ps, maps)
	s.transferLabelMemberships(ctx, kkUID, ps, maps)
}

// transferAlbumMemberships adds the photo to every mapped album it belongs to
// in photo-sorter (AddPhoto is idempotent). Kukátko presents albums
// chronologically, so photo-sorter's manual sort order is not carried over.
func (s *Service) transferAlbumMemberships(ctx context.Context, kkUID string, ps photosorter.Photo, maps mappings) {
	members, err := s.src.AlbumMemberships(ctx, ps.UID)
	if err != nil {
		s.log.Warn("psimport: listing album memberships", "ps_uid", ps.UID, "err", err)
		return
	}
	for i := range members {
		albumUID, ok := maps.albums[members[i].AlbumUID]
		if !ok || albumUID == "" {
			continue
		}
		if err := s.albums.AddPhoto(ctx, albumUID, kkUID); err != nil {
			s.log.Warn("psimport: adding to album", "album", albumUID, "photo", kkUID, "err", err)
		}
	}
}

// transferLabelMemberships attaches every mapped label the photo carries in
// photo-sorter, preserving the provenance source and uncertainty (AttachLabel is
// idempotent).
func (s *Service) transferLabelMemberships(ctx context.Context, kkUID string, ps photosorter.Photo, maps mappings) {
	members, err := s.src.LabelMemberships(ctx, ps.UID)
	if err != nil {
		s.log.Warn("psimport: listing label memberships", "ps_uid", ps.UID, "err", err)
		return
	}
	for i := range members {
		labelUID, ok := maps.labels[members[i].LabelUID]
		if !ok || labelUID == "" {
			continue
		}
		if err := s.labels.AttachLabel(
			ctx, kkUID, labelUID, mapLabelSource(members[i].Source), members[i].Uncertainty,
		); err != nil {
			s.log.Warn("psimport: attaching label", "label", labelUID, "photo", kkUID, "err", err)
		}
	}
}
