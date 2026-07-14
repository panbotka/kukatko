package ppimport

import (
	"context"
	"strings"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photoprism"
)

// maxUncertainty is the upper bound of PhotoPrism's label uncertainty, an
// integer percentage where 0 means certain.
const maxUncertainty = 100

// photoContext holds the indexes a scoped run maps its photos' context against:
// the Kukátko albums by title and labels by name, the keys the importer
// finds-or-creates the source's albums and labels on. Reading them once per run
// keeps the mapping to one catalogue read, and caching what it creates means the
// second photo of an album reuses the album the first one created.
type photoContext struct {
	albumsByTitle map[string]string
	labelsByName  map[string]string
}

// newPhotoContext reads the existing Kukátko albums and labels into the indexes a
// scoped run resolves each photo's context against.
func (s *Service) newPhotoContext(ctx context.Context) (*photoContext, error) {
	byTitle, err := s.albumsByTitle(ctx)
	if err != nil {
		return nil, err
	}
	byName, err := s.labelsByName(ctx)
	if err != nil {
		return nil, err
	}
	return &photoContext{albumsByTitle: byTitle, labelsByName: byName}, nil
}

// mapPhotoContext brings one photo's whole context across from the source: every
// album it belongs to (of any album type) and every label it carries are
// found-or-created in Kukátko and attached to the imported photo — including the
// ones the scope never named, so a photo migrated because it sits in one album
// does not arrive stripped of the three others it also lives in.
//
// It costs one detail request per photo, because the listing payload carries
// neither relation. That is the right trade for a slice (17 photos, 17 requests)
// and exactly why a full run does not do it: it maps the same structure by
// walking the album and label catalogues, where a detail per photo would turn one
// listing into 20k requests. Hence the no-op when state.photoCtx is nil, which is
// how a full run is told apart from a scoped one.
//
// Everything here is idempotent (find-or-create by title/name, an idempotent
// AddPhoto, an upserting AttachLabel), so a re-run adds no duplicate album, label
// or membership row. A failure is logged and skipped rather than failing the
// photo: the original is already catalogued, and a missing membership is
// repairable by re-running.
func (s *Service) mapPhotoContext(ctx context.Context, ppUID string, state *runState) {
	if state.photoCtx == nil {
		return
	}
	photo, ok := s.lookupImported(ctx, ppUID)
	if !ok {
		return
	}
	detail, err := s.client.GetPhoto(ctx, ppUID)
	if err != nil {
		s.log.Warn("ppimport: reading photo detail", "pp_uid", ppUID, "err", err)
		return
	}
	for i := range detail.Albums {
		s.attachPhotoToAlbum(ctx, photo.UID, detail.Albums[i], state.photoCtx)
	}
	for i := range detail.Labels {
		s.attachPhotoLabel(ctx, photo.UID, detail.Labels[i], state.photoCtx)
	}
	// The people ride on this same detail — the listing's markers are always empty —
	// and importing them here (rather than on first import only) is what lets a
	// re-run backfill the photos an earlier, marker-blind run already brought over.
	s.importMarkers(ctx, photo.UID, detail.Photo)
}

// attachPhotoToAlbum finds-or-creates the Kukátko album of one of the photo's
// source albums and adds the photo to it.
func (s *Service) attachPhotoToAlbum(
	ctx context.Context, photoUID string, a photoprism.Album, pctx *photoContext,
) {
	albumUID, ok := s.findOrCreateAlbum(ctx, a, pctx.albumsByTitle)
	if !ok {
		return
	}
	if err := s.albums.AddPhoto(ctx, albumUID, photoUID); err != nil {
		s.log.Warn("ppimport: adding photo to album", "album", albumUID, "photo", photoUID, "err", err)
	}
}

// attachPhotoLabel finds-or-creates the Kukátko label of one of the photo's
// source labels and attaches it, carrying over the source and uncertainty
// PhotoPrism recorded for that attachment.
func (s *Service) attachPhotoLabel(
	ctx context.Context, photoUID string, pl photoprism.PhotoLabel, pctx *photoContext,
) {
	labelUID, ok := s.findOrCreateLabel(ctx, pl.Label, pctx.labelsByName)
	if !ok {
		return
	}
	source := mapLabelSource(pl.LabelSrc)
	if err := s.labels.AttachLabel(ctx, photoUID, labelUID, source, clampUncertainty(pl.Uncertainty)); err != nil {
		s.log.Warn("ppimport: attaching label", "label", labelUID, "photo", photoUID, "err", err)
	}
}

// mapLabelSource maps PhotoPrism's label source onto Kukátko's: a hand-attached
// label stays manual, a vision-classified one is ai, and everything PhotoPrism
// derived on its own (batch, keyword, location, metadata…) is recorded as having
// come from the import.
func mapLabelSource(src string) organize.LabelSource {
	switch strings.ToLower(strings.TrimSpace(src)) {
	case "manual":
		return organize.SourceManual
	case "image":
		return organize.SourceAI
	default:
		return organize.SourceImport
	}
}

// clampUncertainty clamps the source's uncertainty into the 0–100 percentage
// Kukátko stores, so an out-of-range value cannot poison the row.
func clampUncertainty(uncertainty int) int {
	switch {
	case uncertainty < 0:
		return 0
	case uncertainty > maxUncertainty:
		return maxUncertainty
	default:
		return uncertainty
	}
}
