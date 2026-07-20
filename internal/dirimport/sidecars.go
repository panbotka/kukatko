package dirimport

import (
	"context"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/sidecar"
)

// sidecarIndex is the pairing of a run's media files with the metadata sidecars
// lying beside them, resolved once before any file is ingested.
type sidecarIndex struct {
	// pairs maps a media file's absolute path to its sidecar's absolute path.
	pairs map[string]string
	// rel maps any walked path to the path relative to the import root, which is
	// how every path is reported.
	rel map[string]string
	// report is what did and did not pair, in paths relative to the import root.
	report SidecarReport
}

// lookup returns the sidecar paired with a media file, absolute and relative,
// and whether it has one at all.
func (idx sidecarIndex) lookup(entry planEntry) (abs, rel string, ok bool) {
	abs, ok = idx.pairs[entry.abs]
	if !ok {
		return "", "", false
	}
	return abs, idx.rel[abs], true
}

// buildSidecarIndex pairs the media files of a plan with the sidecars found
// beside them. Disabled (opts.NoSidecars) it pairs nothing, so an import that
// wants only the pixels gets exactly that.
//
// The sidecars are the files the walk already classified as SkipSidecar: they are
// not media and are never imported, they are read for what they say about the
// media next to them. `.aae` (an Apple *edit*, not metadata) and `.thm` are among
// them and are ignored here — sidecar.Match only takes the ones it can read.
func buildSidecarIndex(entries []planEntry, opts Options) sidecarIndex {
	rel := make(map[string]string, len(entries))
	for _, entry := range entries {
		rel[entry.abs] = entry.rel
	}
	idx := sidecarIndex{pairs: map[string]string{}, rel: rel}
	if opts.NoSidecars {
		return idx
	}

	var media, sidecars []string
	for _, entry := range entries {
		switch entry.skip {
		case "":
			media = append(media, entry.abs)
		case SkipSidecar:
			sidecars = append(sidecars, entry.abs)
		case SkipHidden, SkipJunk, SkipUnsupported, SkipSymlink, SkipEmpty:
		}
	}

	matches := sidecar.Match(media, sidecars)
	idx.pairs = matches.Pairs
	idx.report = SidecarReport{
		Matched: len(matches.Pairs),
		Orphans: relPaths(matches.Orphans, rel),
		Missing: relPaths(matches.Missing, rel),
	}
	return idx
}

// relPaths maps absolute paths back onto the run-relative ones the report shows.
func relPaths(paths []string, rel map[string]string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if name, ok := rel[path]; ok {
			out = append(out, name)
			continue
		}
		out = append(out, path)
	}
	return out
}

// readSidecar loads the sidecar paired with a media file, if it has one. A
// sidecar that cannot be parsed is reported on the file's result — which the run's
// SidecarReport already surfaces under Unreadable — and the file is imported
// anyway: a corrupt or empty sidecar costs the photo its date, not its place in the
// library. It is deliberately NOT recorded as an import_failures row: an unreadable
// sidecar drops no photo (the photo keeps its own metadata) and is not a data loss
// worth flipping an otherwise-clean folder import to 'partial'; the sidecar-issue
// failures that ARE recorded are the ones that dropped metadata that was available
// (applyCuration / fillFromSidecar).
func (s *Service) readSidecar(ctx context.Context, entry planEntry, idx sidecarIndex) (*sidecar.Metadata, FileResult) {
	abs, rel, ok := idx.lookup(entry)
	if !ok {
		return nil, FileResult{}
	}
	meta, err := sidecar.Read(ctx, abs)
	if err != nil {
		s.log.Warn("dirimport: reading sidecar", "file", entry.rel, "sidecar", rel, "err", err)
		return nil, FileResult{Sidecar: rel, SidecarErr: err}
	}
	meta.Path = abs
	return &meta, FileResult{Sidecar: rel}
}

// applyCuration hands an export's per-user marks to the importing user: Google's
// "favorited" star and an XMP rating. Both are per-user in Kukátko, so with no
// uploader (an unbootstrapped instance) there is nobody to give them to and they
// are dropped.
//
// They are applied only to a freshly imported photo. A photo that is already in
// the library keeps the user's own marks: re-importing an old export must not
// re-favourite what the user has since un-favourited.
func (s *Service) applyCuration(ctx context.Context, photoUID, userUID string, sc *sidecar.Metadata, tal *tally) {
	if sc == nil || s.curation == nil || photoUID == "" || userUID == "" {
		return
	}
	if sc.Favorite {
		if err := s.curation.AddFavorite(ctx, userUID, photoUID); err != nil {
			s.log.Warn("dirimport: marking sidecar favourite", "photo", photoUID, "err", err)
			tal.recordFailure(importer.StageMetadata, photoUID, "", "sidecar favourite", err)
		}
	}
	if sc.Rating > 0 {
		if err := s.curation.SetRating(ctx, userUID, photoUID, sc.Rating); err != nil {
			s.log.Warn("dirimport: setting sidecar rating", "photo", photoUID, "err", err)
			tal.recordFailure(importer.StageMetadata, photoUID, "", "sidecar rating", err)
		}
	}
}

// fillFromSidecar backfills a duplicate's missing metadata from its sidecar — the
// case of a folder that was imported once *without* its sidecars being read, and
// is now imported again. The photo already exists (the content hash says so), so
// nothing is created; only the gaps are filled, and only the gaps. A second run
// finds none left and writes nothing at all.
func (s *Service) fillFromSidecar(ctx context.Context, photoUID string, sc *sidecar.Metadata, tal *tally) {
	if sc == nil || s.filler == nil || photoUID == "" {
		return
	}
	fill := photos.MetadataFill{
		Title:       sc.Title,
		Description: sc.Description,
		Lat:         sc.Lat,
		Lng:         sc.Lng,
		Altitude:    sc.Altitude,
	}
	if sc.TakenAt != nil {
		fill.TakenAt = sc.TakenAt
		fill.TakenAtSource = string(exif.SourceSidecar)
	}
	filled, err := s.filler.FillMissingMetadata(ctx, photoUID, fill)
	if err != nil {
		s.log.Warn("dirimport: filling metadata from sidecar", "photo", photoUID, "err", err)
		tal.recordFailure(importer.StageMetadata, photoUID, "", "sidecar fill", err)
		return
	}
	if filled {
		s.log.Info("dirimport: filled metadata gaps from sidecar", "photo", photoUID, "sidecar", sc.Path)
	}
}
