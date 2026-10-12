package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The project scope of people, markers and the faces cache.
//
// subjects is a root table and carries project_id; markers carry none and belong
// to the project of the photo they sit on. Every Store method that reads or
// writes either takes the project uid as its first argument after the context
// and never defaults it: an empty uid is ErrNoProject, a programming error rather
// than "the whole instance". A subject, marker or photo of another project is
// answered exactly like a missing one (ErrSubjectNotFound, ErrMarkerNotFound,
// ErrPhotoNotFound, an empty list), so the HTTP layer gives the same 404.
//
// A marker write proves the marker (or, when it creates one, the photo) inside
// the project on its own transaction before it changes anything, and every
// subject it names is looked up inside the same project — so a marker never
// points at a subject of another project. The handful of methods a background job
// or a later multi-tenancy step still calls without a request (the nameless
// repair, the sidecar writer, the family tree's inline create) are documented as
// unscoped where they are declared.

// requireProject returns ErrNoProject when projectUID is empty and nil
// otherwise. Every scoped Store method calls it before touching the database.
func requireProject(projectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	return nil
}

// subjectProjectClause is the predicate confining a query over subjects to one
// project, given the placeholder the project uid is bound to. The column is
// qualified so the clause stays unambiguous in a query joining photos.
func subjectProjectClause(placeholder string) string {
	return "subjects.project_id = " + placeholder
}

// markerProjectClause is the predicate confining a query over markers to the
// markers sitting on a photo of one project, given the placeholder the project
// uid is bound to.
func markerProjectClause(placeholder string) string {
	return "EXISTS (SELECT 1 FROM photos WHERE photos.uid = markers.photo_uid AND photos.project_id = " +
		placeholder + ")"
}

// requirePhotoInProject returns ErrPhotoNotFound unless photoUID names a photo
// of the project, run on q — the pool, or the transaction of the write it guards.
func requirePhotoInProject(ctx context.Context, q rowQuerier, projectUID, photoUID string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	var exists bool
	if err := q.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM photos WHERE uid = $1 AND project_id = $2)",
		photoUID, projectUID).Scan(&exists); err != nil {
		return fmt.Errorf("people: looking up photo %s: %w", photoUID, err)
	}
	if !exists {
		return ErrPhotoNotFound
	}
	return nil
}

// requireMarkerInProject returns ErrMarkerNotFound unless markerUID names a
// marker on a photo of the project, run on q.
func requireMarkerInProject(ctx context.Context, q rowQuerier, projectUID, markerUID string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	var exists bool
	if err := q.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM markers WHERE uid = $1 AND "+markerProjectClause("$2")+")",
		markerUID, projectUID).Scan(&exists); err != nil {
		return fmt.Errorf("people: looking up marker %s: %w", markerUID, err)
	}
	if !exists {
		return ErrMarkerNotFound
	}
	return nil
}

// requireSubjectInProject returns ErrSubjectNotFound unless subjectUID names a
// subject of the project, run on q.
func requireSubjectInProject(ctx context.Context, q rowQuerier, projectUID, subjectUID string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	var exists bool
	if err := q.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM subjects WHERE uid = $1 AND "+subjectProjectClause("$2")+")",
		subjectUID, projectUID).Scan(&exists); err != nil {
		return fmt.Errorf("people: looking up subject %s: %w", subjectUID, err)
	}
	if !exists {
		return ErrSubjectNotFound
	}
	return nil
}

// subjectName returns the name of the subject identified by uid inside the
// project, translating a missing (or foreign) subject into ErrSubjectNotFound. It
// is how every marker write resolves the subject it names, so a marker can never
// point across projects, and it keeps the denormalised faces.subject_name cache
// in step when a marker is (re)assigned.
func subjectName(ctx context.Context, q rowQuerier, projectUID, uid string) (string, error) {
	if err := requireProject(projectUID); err != nil {
		return "", err
	}
	var name string
	err := q.QueryRow(ctx,
		"SELECT name FROM subjects WHERE uid = $1 AND "+subjectProjectClause("$2"), uid, projectUID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrSubjectNotFound
	}
	if err != nil {
		return "", fmt.Errorf("people: looking up subject %s: %w", uid, err)
	}
	return name, nil
}
