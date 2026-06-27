package ppimport

import (
	"context"
	"encoding/json"

	"github.com/panbotka/kukatko/internal/jobs"
)

// singletonPhotoUID is the sentinel photo_uid carried by every pp_import job's
// payload. The job queue's dedup key is (type, payload->>'photo_uid'), so giving
// every import the same sentinel means at most one pp_import job is ever queued or
// running at a time — a second trigger is a clean ErrDuplicate rather than a
// concurrent, redundant import.
const singletonPhotoUID = "__pp_import__"

// JobPayload returns the canonical pp_import job payload. It carries the singleton
// sentinel so enqueuing twice while one import is active is deduplicated by the
// queue. The handler ignores the payload contents.
func JobPayload() json.RawMessage {
	raw, err := json.Marshal(map[string]string{"photo_uid": singletonPhotoUID})
	if err != nil {
		// A fixed, two-field map cannot fail to marshal; fall back defensively.
		return json.RawMessage(`{"photo_uid":"` + singletonPhotoUID + `"}`)
	}
	return raw
}

// Handle is the worker.HandlerFunc for pp_import jobs: it runs a full import pass.
// The job payload is ignored (it exists only to serialise imports via the dedup
// key). A returned error fails the job so the worker retries it; per-photo
// failures are recorded inside the run and do not surface here.
func (s *Service) Handle(ctx context.Context, _ jobs.Job) error {
	_, err := s.Import(ctx)
	return err
}
