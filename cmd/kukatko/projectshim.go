package main

import (
	"context"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// The request-project shim (multi-tenancy transition).
//
// internal/people, internal/vectors' similarity searches, internal/facematch
// and internal/feedback's face writes take the project uid explicitly. The
// packages that consume them but are scoped in a later step — candidates,
// outliers, sweep, review, the cluster assignment and expand — still speak the
// old, project-less signatures. These adapters bridge the gap by reading the
// project of the request the call is serving (auth.ProjectUIDFromContext), so
// every one of those calls already runs inside the caller's project instead of
// across the instance. A call without a request project fails with the store's
// ErrNoProject rather than reading everything.
//
// Each adapter disappears when its consumer is scoped and passes the project
// itself; nothing new may be wired through them. The adapters return the store's
// errors unwrapped on purpose: the consumers branch on the store's sentinels
// exactly as they did when they held the store itself.

// reqProject returns the project of the request ctx is serving.
func reqProject(ctx context.Context) string {
	return auth.ProjectUIDFromContext(ctx)
}

// requestProjectPeople adapts *people.Store to the project-less subject and
// marker reads of candidates, outliers, sweep and review.
type requestProjectPeople struct {
	*people.Store
}

// GetSubjectByUID reads the subject inside the request's project.
func (p requestProjectPeople) GetSubjectByUID(ctx context.Context, uid string) (people.Subject, error) {
	return p.Store.GetSubjectByUID(ctx, reqProject(ctx), uid) //nolint:wrapcheck // transparent
}

// GetMarkerByUID reads the marker inside the request's project.
func (p requestProjectPeople) GetMarkerByUID(ctx context.Context, uid string) (people.Marker, error) {
	return p.Store.GetMarkerByUID(ctx, reqProject(ctx), uid) //nolint:wrapcheck // transparent
}

// ListPhotoUIDsBySubject lists the subject's photos inside the request's project.
func (p requestProjectPeople) ListPhotoUIDsBySubject(ctx context.Context, subjectUID string) ([]string, error) {
	return p.Store.ListPhotoUIDsBySubject(ctx, reqProject(ctx), subjectUID) //nolint:wrapcheck // transparent
}

// ListSubjects lists the subjects of the request's project.
func (p requestProjectPeople) ListSubjects(ctx context.Context) ([]people.SubjectCount, error) {
	return p.Store.ListSubjects(ctx, reqProject(ctx)) //nolint:wrapcheck // transparent
}

// requestProjectVectors adapts *vectors.Store to the project-less similarity
// searches of candidates and expand.
type requestProjectVectors struct {
	*vectors.Store
}

// FindSimilar searches the request's project.
func (v requestProjectVectors) FindSimilar(
	ctx context.Context, vec []float32, limit int, maxDistance float64,
) ([]vectors.Match, error) {
	return v.Store.FindSimilar(ctx, reqProject(ctx), vec, limit, maxDistance) //nolint:wrapcheck // transparent
}

// FindSimilarUnassignedFaceCandidates searches the request's project.
func (v requestProjectVectors) FindSimilarUnassignedFaceCandidates(
	ctx context.Context, vec []float32, limit int, maxDistance float64, exclude []vectors.FaceKey,
) ([]vectors.FaceCandidate, error) {
	return v.Store.FindSimilarUnassignedFaceCandidates( //nolint:wrapcheck // transparent
		ctx, reqProject(ctx), vec, limit, maxDistance, exclude)
}

// requestProjectAssigner adapts *facematch.Service to the project-less Apply of
// the review game and the cluster assignment.
type requestProjectAssigner struct {
	*facematch.Service
}

// Apply performs the transition inside the request's project.
func (a requestProjectAssigner) Apply(
	ctx context.Context, req facematch.AssignRequest, meta audit.Meta,
) (facematch.AssignResult, error) {
	return a.Service.Apply(ctx, reqProject(ctx), req, meta) //nolint:wrapcheck // transparent
}

// requestProjectFeedback adapts *feedback.Store to the project-less face
// opinions the review game records.
type requestProjectFeedback struct {
	*feedback.Store
}

// RejectFace records the rejection inside the request's project.
func (f requestProjectFeedback) RejectFace(
	ctx context.Context, key feedback.FaceRejectionKey, entry audit.Entry,
) error {
	return f.Store.RejectFace(ctx, reqProject(ctx), key, entry) //nolint:wrapcheck // transparent
}

// ConfirmFace records the confirmation inside the request's project.
func (f requestProjectFeedback) ConfirmFace(
	ctx context.Context, key feedback.FaceConfirmationKey, entry audit.Entry,
) error {
	return f.Store.ConfirmFace(ctx, reqProject(ctx), key, entry) //nolint:wrapcheck // transparent
}
