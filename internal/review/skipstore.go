package review

// The persisted half of the skip memory: two statements over review_skips.
//
// It lives in this package rather than beside the other stores because nothing
// else in the app has any business reading it. A skip is game state — "don't ask
// me this" — and the moment a second reader existed it would start to look like
// a fact about the library, which is exactly the confusion migration 0059
// refuses to allow.

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SkipRecorder reads and writes the review game's persisted skips over the
// shared pgx pool. It owns no connection; it borrows the pool supplied at
// construction.
type SkipRecorder struct {
	pool *pgxpool.Pool
}

// NewSkipRecorder returns a SkipRecorder backed by pool. The pool stays owned by
// the caller.
func NewSkipRecorder(pool *pgxpool.Pool) *SkipRecorder {
	return &SkipRecorder{pool: pool}
}

// recordSkipSQL remembers one "don't know". The primary key makes it an upsert:
// skipping the same face again is still one unresolved photo, and it moves the
// skip forward in time so a re-skip after a cooling-off period starts the pause
// again rather than leaving a mute that has already expired.
const recordSkipSQL = `
INSERT INTO review_skips (user_uid, subject_uid, photo_uid)
VALUES ($1, $2, $3)
ON CONFLICT (user_uid, subject_uid, photo_uid) DO UPDATE SET skipped_at = now()`

// RecordSkip remembers that userUID could not place subjectUID on photoUID.
//
// A subject or photo that vanished between the question being served and the
// answer arriving is not an error: the foreign keys reject the row, and there is
// nothing left to mute anyway, so the write is simply reported as done.
func (s *SkipRecorder) RecordSkip(ctx context.Context, userUID, subjectUID, photoUID string) error {
	if userUID == "" || subjectUID == "" || photoUID == "" {
		return nil
	}
	if _, err := s.pool.Exec(ctx, recordSkipSQL, userUID, subjectUID, photoUID); err != nil {
		return fmt.Errorf("review: recording a skip: %w", err)
	}
	return nil
}

// skipMemorySQL reads one player's whole skip history. It is bounded by how many
// faces one person has personally given up on, so it is read whole per rebuild
// rather than probed per question — one indexed read beats a lookup per
// candidate, and the result is small enough to keep for the life of a pool.
//
// Nothing here crosses user_uid, which is the guarantee the whole feature rests
// on: one player's "I don't know" must never quiet the game for anybody else.
const skipMemorySQL = `
SELECT subject_uid, photo_uid, skipped_at
FROM review_skips
WHERE user_uid = $1`

// SkipMemory returns everything userUID has skipped, grouped by subject. A
// player who has never skipped anything yields an empty memory, not an error.
func (s *SkipRecorder) SkipMemory(ctx context.Context, userUID string) (SkipMemory, error) {
	rows, err := s.pool.Query(ctx, skipMemorySQL, userUID)
	if err != nil {
		return nil, fmt.Errorf("review: reading the skip memory: %w", err)
	}
	defer rows.Close()

	memory := make(SkipMemory)
	for rows.Next() {
		var subjectUID, photoUID string
		var skippedAt time.Time
		if err := rows.Scan(&subjectUID, &photoUID, &skippedAt); err != nil {
			return nil, fmt.Errorf("review: scanning a skip: %w", err)
		}
		entry, ok := memory[subjectUID]
		if !ok {
			entry = SubjectSkips{Photos: make(map[string]struct{})}
		}
		entry.Photos[photoUID] = struct{}{}
		if skippedAt.After(entry.LastAt) {
			entry.LastAt = skippedAt
		}
		memory[subjectUID] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review: iterating skips: %w", err)
	}
	return memory, nil
}
