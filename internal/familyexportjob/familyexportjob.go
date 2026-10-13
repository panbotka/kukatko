// Package familyexportjob is the queue handler that keeps each project's
// genealogy export current: one job, one file, one project's whole family tree.
//
// It is the scheduled half of internal/familyexport — that package knows the
// format and how to put it in the store, this one knows when. A `family_export`
// job naming a project (jobs.FamilyExportPayload) is enqueued by every mutation
// that changes a relation in that project; the handler re-reads the project's
// whole genealogy and rewrites its families.yaml, under the project's storage
// prefix. Doing it in the queue rather than in the request is what keeps filling
// in a family — a partner, then four children, then their years — from writing
// the same file once per click against an object store the user is waiting on.
//
// The handler is idempotent and stateless: it reads the genealogy as it is now
// and writes the file as it should be now. Running it twice writes the same bytes
// twice; running it late writes the current state, not the state that triggered
// it. That is why a coalesced job costs nothing and why the queue's dedup — per
// project — is a safe debounce rather than a dropped update.
//
// It follows internal/sidecarjob's shape, and differs from it in the one way the
// data does: a sidecar job is per photo, this one is per project. There is one
// genealogy per project, so there is one file and one kind of job for it — which
// is also why it needs no backfill. "Write it now" is the whole operation, and it
// is exposed as Export (one project) and ExportAll (every project) for the CLI.
package familyexportjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyexport"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/project"
)

// FamilyStore reads one project's genealogy. It is satisfied by family.Store.
type FamilyStore interface {
	// Export returns every family of the project, every child membership of
	// those families and every subject either of them names.
	Export(ctx context.Context, projectUID string) (family.Export, error)
}

// ProjectStore resolves the project a job names: its slug for the document's
// project block and its storage prefix for the key the file lands at. It is
// satisfied by project.Store.
type ProjectStore interface {
	// Get returns the project with the given uid, or project.ErrNotFound.
	Get(ctx context.Context, uid string) (*project.Project, error)
	// List returns every project, active or not.
	List(ctx context.Context) ([]project.Project, error)
}

// DocumentWriter renders and stores the export document. It is satisfied by
// familyexport.Writer.
type DocumentWriter interface {
	// Write stores doc at the export's key under storagePrefix and returns the
	// key written.
	Write(ctx context.Context, storagePrefix string, doc familyexport.Document) (string, error)
}

// Config bundles the dependencies of New. Families, Projects and Writer are
// required.
type Config struct {
	// Families reads the genealogy.
	Families FamilyStore
	// Projects resolves the project a job names.
	Projects ProjectStore
	// Writer renders and stores the document.
	Writer DocumentWriter
	// Logger receives the write confirmations. Defaults to slog.Default().
	Logger *slog.Logger
}

// Service writes the projects' genealogy exports.
type Service struct {
	families FamilyStore
	projects ProjectStore
	writer   DocumentWriter
	log      *slog.Logger
}

// New returns a Service from cfg. It panics when a required dependency is
// missing, which is a wiring bug and should fail at startup rather than on the
// first job.
func New(cfg Config) *Service {
	if cfg.Families == nil || cfg.Projects == nil || cfg.Writer == nil {
		panic("familyexportjob: Families, Projects and Writer are required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{families: cfg.Families, projects: cfg.Projects, writer: cfg.Writer, log: log}
}

// Handle runs one family_export job: it rewrites the export of the project the
// payload names from that project's genealogy as it stands now. Only that
// project's file is written; another project's is a different key.
//
// A payload naming no project — a job queued before the export was per project
// — is read as the bootstrap project, the only one there was. A project that no
// longer exists is skipped rather than failed: deleting it removed its families
// with it, and there is nobody left whose tree the file could describe.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	projectUID, err := projectOf(job)
	if err != nil {
		return err
	}
	_, err = s.Export(ctx, projectUID)
	if errors.Is(err, project.ErrNotFound) {
		s.log.InfoContext(ctx, "family tree export skipped: the project is gone",
			slog.String("project_uid", projectUID))
		return nil
	}
	return err
}

// projectOf returns the project a family_export job's payload names, or the
// bootstrap project for a job that names none.
func projectOf(job jobs.Job) (string, error) {
	var payload jobs.FamilyExportPayload
	if len(job.Payload) > 0 {
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return "", fmt.Errorf("familyexportjob: decoding the payload of job %d: %w", job.ID, err)
		}
	}
	if payload.ProjectUID == "" {
		return project.BootstrapUID, nil
	}
	return payload.ProjectUID, nil
}

// Export reads the whole genealogy of the project projectUID and writes its
// families.yaml under the project's storage prefix, returning the key written.
// It returns project.ErrNotFound (wrapped) for an unknown project.
//
// An empty genealogy is written rather than skipped: a project whose last
// relation was just removed is described by a file with no families in it, and
// leaving the previous file in place would let a rebuild restore a tree the user
// has deleted.
func (s *Service) Export(ctx context.Context, projectUID string) (string, error) {
	proj, err := s.projects.Get(ctx, projectUID)
	if err != nil {
		return "", fmt.Errorf("familyexportjob: resolving project %s: %w", projectUID, err)
	}
	return s.exportProject(ctx, *proj)
}

// ExportAll writes the export of every project, active or not — a deactivated
// project keeps its data, and its tree is as worth keeping as anybody's — and
// returns the keys written in project order. It stops at the first failure.
func (s *Service) ExportAll(ctx context.Context) ([]string, error) {
	all, err := s.projects.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("familyexportjob: listing projects: %w", err)
	}
	keys := make([]string, 0, len(all))
	for _, proj := range all {
		key, err := s.exportProject(ctx, proj)
		if err != nil {
			return keys, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// exportProject reads one project's genealogy and writes its document.
func (s *Service) exportProject(ctx context.Context, proj project.Project) (string, error) {
	export, err := s.families.Export(ctx, proj.UID)
	if err != nil {
		return "", fmt.Errorf("familyexportjob: reading the genealogy of project %s: %w", proj.Slug, err)
	}
	doc := familyexport.Build(familyexport.Input{
		Project: familyexport.Project{UID: proj.UID, Slug: proj.Slug},
		Export:  export,
	})
	key, err := s.writer.Write(ctx, proj.StoragePrefix, doc)
	if err != nil {
		return "", fmt.Errorf("familyexportjob: writing the family tree export of project %s: %w", proj.Slug, err)
	}
	s.log.DebugContext(ctx, "family tree exported",
		slog.String("project_uid", proj.UID),
		slog.String("key", key),
		slog.Int("families", len(export.Families)),
		slog.Int("subjects", len(export.Subjects)))
	return key, nil
}
