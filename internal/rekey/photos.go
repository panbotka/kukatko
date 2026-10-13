package rekey

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// pendingBatch returns the next photos of run's project still in the old
// layout, after the run's cursor, each with its recorded renditions.
func (s *Service) pendingBatch(ctx context.Context, run Run, limit int) ([]PhotoFiles, error) {
	rows, err := s.pool.Query(ctx, pendingBatchSQL, run.ProjectUID, run.TargetPrefix, run.LastKey, limit)
	if err != nil {
		return nil, fmt.Errorf("rekey: listing photos to move: %w", err)
	}
	batch, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PhotoFiles, error) {
		var photo PhotoFiles
		err := row.Scan(&photo.UID, &photo.FilePath, &photo.FileHash, &photo.FileSize)
		return photo, err //nolint:wrapcheck // CollectRows' caller wraps it below.
	})
	if err != nil {
		return nil, fmt.Errorf("rekey: reading photos to move: %w", err)
	}
	if err := s.attachRenditions(ctx, batch); err != nil {
		return nil, err
	}
	return batch, nil
}

// loadPhoto returns one photo of the project with its renditions, reporting
// false when there is none.
func (s *Service) loadPhoto(ctx context.Context, projectUID, uid string) (PhotoFiles, bool, error) {
	var photo PhotoFiles
	err := s.pool.QueryRow(ctx, photoByUIDSQL, uid, projectUID).
		Scan(&photo.UID, &photo.FilePath, &photo.FileHash, &photo.FileSize)
	if errors.Is(err, pgx.ErrNoRows) {
		return PhotoFiles{}, false, nil
	}
	if err != nil {
		return PhotoFiles{}, false, fmt.Errorf("rekey: reading photo %s: %w", uid, err)
	}
	batch := []PhotoFiles{photo}
	if err := s.attachRenditions(ctx, batch); err != nil {
		return PhotoFiles{}, false, err
	}
	return batch[0], true, nil
}

// attachRenditions fills in the recorded streaming renditions of every photo
// of batch with one query.
func (s *Service) attachRenditions(ctx context.Context, batch []PhotoFiles) error {
	if len(batch) == 0 {
		return nil
	}
	index := make(map[string]int, len(batch))
	uids := make([]string, 0, len(batch))
	for i, photo := range batch {
		index[photo.UID] = i
		uids = append(uids, photo.UID)
	}
	rows, err := s.pool.Query(ctx, renditionsSQL, uids)
	if err != nil {
		return fmt.Errorf("rekey: listing renditions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			photoUID  string
			rendition Rendition
		)
		if err := rows.Scan(&photoUID, &rendition.Name, &rendition.Playlist, &rendition.SegmentCount); err != nil {
			return fmt.Errorf("rekey: reading a rendition: %w", err)
		}
		if i, ok := index[photoUID]; ok {
			batch[i].Renditions = append(batch[i].Renditions, rendition)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rekey: iterating renditions: %w", err)
	}
	return nil
}
