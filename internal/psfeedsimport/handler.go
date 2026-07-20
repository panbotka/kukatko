package psfeedsimport

import (
	"context"
	"encoding/json"

	"github.com/panbotka/kukatko/internal/jobs"
)

// singletonPhotoUID is the sentinel photo_uid carried by every ps_feeds_import
// job's payload. The queue's dedup key is (type, payload->>'photo_uid'), so giving
// every run the same sentinel means at most one ps_feeds_import job is ever queued
// or running at a time: a second trigger is a clean jobs.ErrDuplicate rather than
// a concurrent, redundant enrichment pass.
const singletonPhotoUID = "__ps_feeds_import__"

// Handle is the worker.HandlerFunc for ps_feeds_import jobs: it runs one full
// feeds import. The payload is ignored (it exists only to serialise runs via the
// dedup key). A returned error fails the job so the worker retries it; per-item
// problems are recorded in the run and do not surface here.
func (s *Service) Handle(ctx context.Context, _ jobs.Job) error {
	_, err := s.Import(ctx)
	return err
}

// JobPayload returns the canonical ps_feeds_import job payload, carrying the
// singleton sentinel so enqueuing twice while a run is active is deduplicated by
// the queue. The handler ignores the payload contents.
func JobPayload() json.RawMessage {
	raw, err := json.Marshal(map[string]string{"photo_uid": singletonPhotoUID})
	if err != nil {
		// A fixed, single-field map cannot fail to marshal; fall back defensively.
		return json.RawMessage(`{"photo_uid":"` + singletonPhotoUID + `"}`)
	}
	return raw
}
