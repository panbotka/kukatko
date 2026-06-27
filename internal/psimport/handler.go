package psimport

import (
	"context"
	"encoding/json"

	"github.com/panbotka/kukatko/internal/jobs"
)

// singletonPhotoUID is the sentinel photo_uid carried by every ps_migrate job's
// payload. The job queue's dedup key is (type, payload->>'photo_uid'), so giving
// every migration the same sentinel means at most one ps_migrate job is ever
// queued or running at a time: a second trigger is a clean ErrDuplicate rather
// than a concurrent, redundant migration.
const singletonPhotoUID = "__ps_migrate__"

// Handle is the worker.HandlerFunc for ps_migrate jobs: it runs a full migration
// pass. The job payload is ignored (it exists only to serialise migrations via
// the dedup key). A returned error fails the job so the worker retries it;
// per-photo failures are recorded inside the run and do not surface here.
func (s *Service) Handle(ctx context.Context, _ jobs.Job) error {
	_, err := s.Migrate(ctx)
	return err
}

// JobPayload returns the canonical ps_migrate job payload. It carries the
// singleton sentinel so enqueuing twice while one migration is active is
// deduplicated by the queue. The handler ignores the payload contents.
func JobPayload() json.RawMessage {
	raw, err := json.Marshal(map[string]string{"photo_uid": singletonPhotoUID})
	if err != nil {
		// A fixed, single-field map cannot fail to marshal; fall back defensively.
		return json.RawMessage(`{"photo_uid":"` + singletonPhotoUID + `"}`)
	}
	return raw
}
