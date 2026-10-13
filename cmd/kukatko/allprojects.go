package main

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/embedjob"
	"github.com/panbotka/kukatko/internal/facejob"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/placesjob"
	"github.com/panbotka/kukatko/internal/processing"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/vectors"
)

// The instance-level triggers of the per-project backfills.
//
// A backfill lists and schedules one project's photos (the request's project
// over HTTP). The few callers that work on the whole instance — the library
// maintenance scan and repair, and the `kukatko sidecar backfill` CLI — have no
// project of their own, so these adapters run the scoped operation once per
// active project and fold the answers together. A deactivated project is
// skipped: nobody can enter it, and its photos wait until it is reactivated.
//
// The maintenance scan reconciles the store per project itself (it is wired
// with the project store); these adapters only fold its derived-data backlogs —
// embeddings, faces, pHashes, places, unscheduled steps — into instance totals.

// projectLister enumerates the instance's projects. *project.Store satisfies it.
type projectLister interface {
	// List returns every project of the instance.
	List(ctx context.Context) ([]project.Project, error)
}

// eachActiveProject calls fn with the uid of every active project, in the
// lister's order, stopping at the first error.
func eachActiveProject(ctx context.Context, projects projectLister, fn func(projectUID string) error) error {
	all, err := projects.List(ctx)
	if err != nil {
		return fmt.Errorf("listing projects: %w", err)
	}
	for _, p := range project.Active(all) {
		if err := fn(p.UID); err != nil {
			return err
		}
	}
	return nil
}

// collectAcrossProjects concatenates list's answer for every active project. A
// positive limit caps the combined result; a non-positive one returns all.
func collectAcrossProjects[T any](
	ctx context.Context, projects projectLister, limit int,
	list func(ctx context.Context, projectUID string) ([]T, error),
) ([]T, error) {
	var out []T
	err := eachActiveProject(ctx, projects, func(projectUID string) error {
		if limit > 0 && len(out) >= limit {
			return nil
		}
		found, err := list(ctx, projectUID)
		if err != nil {
			return err
		}
		out = append(out, found...)
		return nil
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, err
}

// sumAcrossProjects adds up count's answer for every active project, returning
// what was already counted alongside an error.
func sumAcrossProjects(
	ctx context.Context, projects projectLister, count func(ctx context.Context, projectUID string) (int, error),
) (int, error) {
	total := 0
	err := eachActiveProject(ctx, projects, func(projectUID string) error {
		n, err := count(ctx, projectUID)
		total += n
		return err
	})
	return total, err
}

// instancePhotos is the maintenance photo catalogue over every active project:
// the photos store with its one per-project backfill listing run per project.
type instancePhotos struct {
	*photos.Store
	projects projectLister
}

// ListPhotosMissingPhash lists the photos missing perceptual hashes in every
// active project.
func (p instancePhotos) ListPhotosMissingPhash(ctx context.Context, limit int) ([]string, error) {
	return collectAcrossProjects(ctx, p.projects, limit, func(ctx context.Context, projectUID string) ([]string, error) {
		return p.Store.ListPhotosMissingPhash(ctx, projectUID, limit)
	})
}

// instanceVectors is the maintenance vector catalogue over every active
// project: the vectors store with its per-project backfill listings run per
// project.
type instanceVectors struct {
	*vectors.Store
	projects projectLister
}

// ListPhotosMissingEmbedding lists the photos missing an embedding in every
// active project.
func (v instanceVectors) ListPhotosMissingEmbedding(ctx context.Context, limit int) ([]string, error) {
	return collectAcrossProjects(ctx, v.projects, limit, func(ctx context.Context, projectUID string) ([]string, error) {
		return v.Store.ListPhotosMissingEmbedding(ctx, projectUID, limit)
	})
}

// ListPhotosMissingFaces lists the stills missing face detection in every
// active project.
func (v instanceVectors) ListPhotosMissingFaces(ctx context.Context, limit int) ([]string, error) {
	return collectAcrossProjects(ctx, v.projects, limit, func(ctx context.Context, projectUID string) ([]string, error) {
		return v.Store.ListPhotosMissingFaces(ctx, projectUID, limit)
	})
}

// instanceEmbed runs the embedding backfill for every active project.
type instanceEmbed struct {
	svc      *embedjob.Service
	projects projectLister
}

// BackfillEmbeddings schedules the missing embeddings of every active project
// and returns how many jobs were queued in total.
func (e instanceEmbed) BackfillEmbeddings(ctx context.Context) (int, error) {
	return sumAcrossProjects(ctx, e.projects, e.svc.BackfillEmbeddings)
}

// instanceFaces runs the face-detection backfill for every active project.
type instanceFaces struct {
	svc      *facejob.Service
	projects projectLister
}

// BackfillFaces schedules the missing face detections of every active project
// and sums the per-project results.
func (f instanceFaces) BackfillFaces(ctx context.Context) (facejob.BackfillResult, error) {
	var total facejob.BackfillResult
	err := eachActiveProject(ctx, f.projects, func(projectUID string) error {
		res, err := f.svc.BackfillFaces(ctx, projectUID)
		total.Enqueued += res.Enqueued
		total.SkippedVideos += res.SkippedVideos
		if err != nil {
			return fmt.Errorf("backfilling faces of project %s: %w", projectUID, err)
		}
		return nil
	})
	return total, err
}

// instancePlaces runs the reverse-geocode backlog listing and backfill for
// every active project.
type instancePlaces struct {
	svc      *placesjob.Service
	projects projectLister
}

// MissingPlaces lists the photos missing a place in every active project.
func (p instancePlaces) MissingPlaces(ctx context.Context) ([]string, error) {
	return collectAcrossProjects(ctx, p.projects, 0, p.svc.MissingPlaces)
}

// BackfillPlaces schedules the missing places of every active project.
func (p instancePlaces) BackfillPlaces(ctx context.Context) (int, error) {
	return sumAcrossProjects(ctx, p.projects, p.svc.BackfillPlaces)
}

// instanceProcessing is the processing-gap scan over every active project; the
// per-photo Run needs no project of its own.
type instanceProcessing struct {
	*processing.Service
	projects projectLister
}

// Unscheduled returns the processing gaps of every active project.
func (p instanceProcessing) Unscheduled(ctx context.Context) ([]processing.Gap, error) {
	return collectAcrossProjects(ctx, p.projects, 0, p.Service.Unscheduled)
}

// The adapters must keep satisfying the instance-wide maintenance interfaces.
var (
	_ maintenance.PhotoCatalog    = instancePhotos{}
	_ maintenance.VectorCatalog   = instanceVectors{}
	_ maintenance.EmbedBackfiller = instanceEmbed{}
	_ maintenance.FaceBackfiller  = instanceFaces{}
	_ maintenance.PlaceBackfiller = instancePlaces{}
	_ maintenance.ProcessingGaps  = instanceProcessing{}
)
