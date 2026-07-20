package psfeedsimport

import (
	"context"
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/psfeeds"
	"github.com/panbotka/kukatko/internal/vectors"
)

// importEmbeddings pages the embeddings feed and upserts each item's CLIP vector
// onto the Kukátko photo whose photoprism_uid equals the item's photo_uid. It
// checkpoints the counts after each page and returns only on an infrastructure
// error (a feed fetch or a database failure); a photo not yet imported or a
// dimension-rejected vector is counted and the pass continues.
func (s *Service) importEmbeddings(ctx context.Context, runID int64, st *runState) error {
	after := ""
	for {
		page, err := s.feeds.ListEmbeddings(ctx, s.pageSize, after)
		if err != nil {
			return fmt.Errorf("listing embeddings (after %q): %w", after, err)
		}
		for i := range page.Embeddings {
			if err := s.importEmbedding(ctx, st, page.Embeddings[i]); err != nil {
				return err
			}
		}
		if err := s.runs.UpdateCounts(ctx, runID, st.counts); err != nil {
			return fmt.Errorf("checkpointing embedding counts: %w", err)
		}
		if page.NextAfter == nil || *page.NextAfter == after {
			return nil
		}
		after = *page.NextAfter
	}
}

// importEmbedding attaches one embedding to its photo. It returns an error only on
// an infrastructure failure; a missing photo (Skipped) or a rejected vector
// (Failed) is counted, not returned.
func (s *Service) importEmbedding(ctx context.Context, st *runState, e psfeeds.Embedding) error {
	st.trackTime(e.CreatedAt)
	photo, err := s.photos.GetByPhotoprismUID(ctx, e.PhotoUID)
	if errors.Is(err, photos.ErrPhotoNotFound) {
		st.counts.Skipped++
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolving photo %q: %w", e.PhotoUID, err)
	}
	_, err = s.vectors.SaveEmbedding(ctx, vectors.Embedding{
		PhotoUID:   photo.UID,
		Vector:     e.Vector,
		Model:      e.Model,
		Pretrained: e.Pretrained,
	})
	if errors.Is(err, vectors.ErrDimMismatch) {
		s.log.Warn("psfeedsimport: skipping embedding with wrong dimension",
			"photo", e.PhotoUID, "dim", len(e.Vector))
		st.counts.Failed++
		return nil
	}
	if err != nil {
		return fmt.Errorf("saving embedding for %q: %w", photo.UID, err)
	}
	st.counts.Imported++
	return nil
}
