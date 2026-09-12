// Package familyexportjob is the queue handler that keeps the library's
// genealogy export current: one job, one file, the whole family tree.
//
// It is the scheduled half of internal/familyexport — that package knows the
// format and how to put it in the store, this one knows when. A `family_export`
// job is enqueued by every mutation that changes a relation; the handler re-reads
// the whole genealogy and rewrites families.yaml. Doing it in the queue rather
// than in the request is what keeps filling in a family — a partner, then four
// children, then their years — from writing the same file once per click against
// an object store the user is waiting on.
//
// The handler is idempotent and stateless: it reads the genealogy as it is now
// and writes the file as it should be now. Running it twice writes the same bytes
// twice; running it late writes the current state, not the state that triggered
// it. That is why a coalesced job costs nothing and why the queue's dedup is a
// safe debounce rather than a dropped update.
//
// It follows internal/sidecarjob's shape, and differs from it in the one way the
// data does: a sidecar job is per photo, this one has no subject at all. There is
// one genealogy, so there is one file and one kind of job for it — which is also
// why it needs no backfill. "Write it now" is the whole operation, and it is
// exposed as Export for the CLI to call.
package familyexportjob

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyexport"
	"github.com/panbotka/kukatko/internal/jobs"
)

// FamilyStore reads the whole genealogy. It is satisfied by family.Store.
type FamilyStore interface {
	// Export returns every family, every child membership and every subject
	// either of them names.
	Export(ctx context.Context) (family.Export, error)
}

// DocumentWriter renders and stores the export document. It is satisfied by
// familyexport.Writer.
type DocumentWriter interface {
	// Write stores doc at the export's key and returns the key written.
	Write(ctx context.Context, doc familyexport.Document) (string, error)
}

// Config bundles the dependencies of New. Both are required.
type Config struct {
	// Families reads the genealogy.
	Families FamilyStore
	// Writer renders and stores the document.
	Writer DocumentWriter
	// Logger receives the write confirmations. Defaults to slog.Default().
	Logger *slog.Logger
}

// Service writes the library's genealogy export.
type Service struct {
	families FamilyStore
	writer   DocumentWriter
	log      *slog.Logger
}

// New returns a Service from cfg. It panics when a required dependency is
// missing, which is a wiring bug and should fail at startup rather than on the
// first job.
func New(cfg Config) *Service {
	if cfg.Families == nil || cfg.Writer == nil {
		panic("familyexportjob: Families and Writer are required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{families: cfg.Families, writer: cfg.Writer, log: log}
}

// Handle runs one family_export job: it rewrites the export from the genealogy
// as it stands now.
//
// The job's payload is ignored, and it carries none. There is one genealogy in
// the library, so there is nothing for a payload to name and nothing a malformed
// one could mean — which is also why this handler, unlike the per-photo ones, has
// no "the thing this job was about is gone" skip.
func (s *Service) Handle(ctx context.Context, _ jobs.Job) error {
	return s.Export(ctx)
}

// Export reads the whole genealogy and writes families.yaml.
//
// An empty genealogy is written rather than skipped: a library whose last
// relation was just removed is described by a file with no families in it, and
// leaving the previous file in place would let a rebuild restore a tree the user
// has deleted.
func (s *Service) Export(ctx context.Context) error {
	export, err := s.families.Export(ctx)
	if err != nil {
		return fmt.Errorf("familyexportjob: reading the genealogy: %w", err)
	}
	key, err := s.writer.Write(ctx, familyexport.Build(familyexport.Input{Export: export}))
	if err != nil {
		return fmt.Errorf("familyexportjob: writing the family tree export: %w", err)
	}
	s.log.DebugContext(ctx, "family tree exported",
		slog.String("key", key),
		slog.Int("families", len(export.Families)),
		slog.Int("subjects", len(export.Subjects)))
	return nil
}
