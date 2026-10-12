// Package namelessjob is the nameless-subject repair as background work: the
// undo-file format, the service the admin surface drives, and the two queue
// handlers that actually touch the catalogue.
//
// A subject whose name identifies nobody cannot be created deliberately — the
// subject API rejects a name with no letter or digit — so one in the catalogue
// was minted by an importer keying find-or-create on the fallback slug, and every
// nameless face assigned after it joined it. In production that catch-all owns
// 96 % of the library's faces. Repairing it is data loss if it guesses wrong, so
// the repair reports by default and applies only against a written undo file:
// `kukatko maintenance nameless-subjects --apply --undo-file <path>` on the
// command line, or the maintainer-only card on the admin Maintenance page, which
// hands the same file to the browser as a download before it schedules anything.
//
// The destructive step runs through the job queue rather than inline. Detaching
// the production catch-all sets subject_uid NULL on ~111 000 faces, which moves
// every one of them into the partial "unassigned faces" HNSW index (migration
// 0047) — minutes of index maintenance no HTTP request should sit on. Each job
// reuses people.Store.DetachSubject / RestoreSubject, so there is exactly one
// implementation of the detach and it stays audited inside its own transaction.
//
// The repair is per project. Each project may have a catch-all of its own; the
// report, the snapshot and both jobs work inside one project, the undo file
// records which (uid and slug) and is refused by any other, and the job payloads
// carry project_uid — part of their dedup key (migration 0105), so the same
// subject's detach is never queued twice while the first is waiting.
package namelessjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/project"
)

// ErrProjectMismatch is returned when an undo file is replayed into a project
// other than the one it was taken in. Replaying it there would re-create the
// subjects in the wrong library, so it is refused before anything is queued.
var ErrProjectMismatch = errors.New("namelessjob: the undo file belongs to another project")

// ErrNoProject is returned when the repair is asked to work on no project at
// all — a programming error, never read as "every project".
var ErrNoProject = errors.New("namelessjob: no project given")

// The reasons recorded in the audit details, so the trail says why a subject
// vanished or reappeared rather than only that it did.
const (
	detachReason  = "nameless catch-all subject detached by the maintenance repair"
	restoreReason = "nameless catch-all subject restored from an undo file"
)

// Undo is the undo file of the nameless-subject repair: every subject a detach
// removed, together with the markers and cached faces that pointed at it. It is
// the exact format `kukatko maintenance nameless-subjects --undo-file` writes and
// `--undo` replays, so a file downloaded from the admin page can be replayed on
// the command line and vice versa.
type Undo struct {
	// ProjectUID is the uid of the project the subjects were detached from. A
	// file written before projects existed carries none; it belongs to the
	// bootstrap project, the only library there was.
	ProjectUID string `json:"project_uid,omitempty"`
	// ProjectSlug is that project's slug, so a person reading the file can tell
	// which library it is about. It is informational: the uid is what is checked.
	ProjectSlug string `json:"project_slug,omitempty"`
	// Subjects are the detached subjects, in the order they were detached.
	Subjects []people.SubjectSnapshot `json:"subjects"`
}

// Project returns the uid of the project the undo file belongs to: the one it
// records, or the bootstrap project for a file written before projects existed.
func (u Undo) Project() string {
	if u.ProjectUID == "" {
		return project.BootstrapUID
	}
	return u.ProjectUID
}

// CheckProject returns ErrProjectMismatch unless the undo file belongs to the
// project projectUID.
func (u Undo) CheckProject(projectUID string) error {
	if u.Project() != projectUID {
		return fmt.Errorf("%w: it records %s, the repair runs in %s", ErrProjectMismatch, u.Project(), projectUID)
	}
	return nil
}

// Store is the slice of people.Store the repair needs, kept as an interface so
// the service can be unit-tested without a database. Every method works inside
// one project.
type Store interface {
	// ListNamelessSubjects reports every subject of the project whose name
	// identifies nobody.
	ListNamelessSubjects(ctx context.Context, projectUID string) ([]people.NamelessSubject, error)
	// SnapshotSubject reads what a detach of uid would remove, changing nothing.
	SnapshotSubject(ctx context.Context, projectUID, uid string) (people.SubjectSnapshot, error)
	// DetachSubject deletes the subject and returns the snapshot that undoes it.
	DetachSubject(ctx context.Context, projectUID, uid string, entry audit.Entry) (people.SubjectSnapshot, error)
	// RestoreSubject re-creates a snapshotted subject and re-links what it owned.
	RestoreSubject(
		ctx context.Context, projectUID string, snap people.SubjectSnapshot, entry audit.Entry,
	) (people.Subject, error)
}

// Queue is the enqueue half of the job store, kept as an interface for the same
// reason.
type Queue interface {
	// Enqueue appends a job of jobType carrying payload to the persistent queue.
	Enqueue(ctx context.Context, jobType string, payload json.RawMessage, opts jobs.EnqueueOptions) (jobs.Job, error)
}

// Service reports the nameless subjects, schedules the repair and runs the
// scheduled jobs.
type Service struct {
	store Store
	queue Queue
	log   *slog.Logger
}

// New returns a Service over store and queue. A nil logger uses slog.Default().
func New(store Store, queue Queue, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, queue: queue, log: logger}
}

// List reports every subject of the project whose name identifies nobody, with
// the markers and cached faces currently pointing at it. It is read-only — the
// dry run.
func (s *Service) List(ctx context.Context, projectUID string) ([]people.NamelessSubject, error) {
	if projectUID == "" {
		return nil, ErrNoProject
	}
	found, err := s.store.ListNamelessSubjects(ctx, projectUID)
	if err != nil {
		return nil, fmt.Errorf("namelessjob: listing nameless subjects: %w", err)
	}
	return found, nil
}

// Snapshot reads the undo file a detach of every nameless subject of the project
// would need, without changing anything. The file records the project's uid and
// slug. It is what the admin repair hands to the browser
// before it schedules the detach; an empty Undo means there is nothing to repair.
//
// A subject that disappears between the listing and its snapshot is skipped
// rather than failing the whole read: the repair is idempotent and a concurrent
// CLI run getting there first is a success, not an error.
func (s *Service) Snapshot(ctx context.Context, projectUID, projectSlug string) (Undo, error) {
	found, err := s.List(ctx, projectUID)
	if err != nil {
		return Undo{}, err
	}
	undo := Undo{
		ProjectUID: projectUID, ProjectSlug: projectSlug,
		Subjects: make([]people.SubjectSnapshot, 0, len(found)),
	}
	for _, ns := range found {
		snap, err := s.store.SnapshotSubject(ctx, projectUID, ns.UID)
		if err != nil {
			if errors.Is(err, people.ErrSubjectNotFound) {
				continue
			}
			return Undo{}, fmt.Errorf("namelessjob: snapshotting subject %s: %w", ns.UID, err)
		}
		undo.Subjects = append(undo.Subjects, snap)
	}
	return undo, nil
}

// EnqueueDetach schedules one detach job per subject in undo, inside the project
// projectUID, and returns how many were queued. Call it only once the undo file
// is in the operator's hands: the jobs are what make the change, and nothing else
// records the links they remove. An undo file of another project is refused with
// ErrProjectMismatch before anything is queued.
func (s *Service) EnqueueDetach(ctx context.Context, projectUID string, undo Undo, meta audit.Meta) (int, error) {
	if projectUID == "" {
		return 0, ErrNoProject
	}
	if err := undo.CheckProject(projectUID); err != nil {
		return 0, err
	}
	queued := 0
	for _, snap := range undo.Subjects {
		payload := detachPayload{
			ProjectUID:  projectUID,
			SubjectUID:  snap.Subject.UID,
			MarkerCount: len(snap.MarkerUIDs),
			FaceCount:   len(snap.Faces),
			Actor:       actorFrom(meta),
		}
		added, err := s.enqueue(ctx, jobs.TypeNamelessDetach, payload)
		if err != nil {
			return queued, err
		}
		if added {
			queued++
		}
	}
	return queued, nil
}

// EnqueueRestore schedules one restore job per snapshot in undo, inside the
// project projectUID, and returns how many were queued. It is the undo of
// EnqueueDetach, taking back the file that detach handed out — and only in the
// project that file was taken in (ErrProjectMismatch otherwise, nothing queued).
func (s *Service) EnqueueRestore(ctx context.Context, projectUID string, undo Undo, meta audit.Meta) (int, error) {
	if projectUID == "" {
		return 0, ErrNoProject
	}
	if err := undo.CheckProject(projectUID); err != nil {
		return 0, err
	}
	for _, snap := range undo.Subjects {
		if snap.Subject.UID == "" {
			return 0, errors.New("namelessjob: undo file holds a snapshot with no subject uid")
		}
	}
	queued := 0
	for _, snap := range undo.Subjects {
		added, err := s.enqueue(ctx, jobs.TypeNamelessRestore, restorePayload{
			ProjectUID: projectUID, SubjectUID: snap.Subject.UID, Snapshot: snap, Actor: actorFrom(meta),
		})
		if err != nil {
			return queued, err
		}
		if added {
			queued++
		}
	}
	return queued, nil
}

// enqueue marshals payload and appends a job of jobType, reporting whether a job
// was added. The dedup key is (type, project, subject) (migration 0105): the same
// subject's detach or restore already waiting in the queue — a double-click, the
// same undo file replayed twice — collapses into it and is not counted again.
func (s *Service) enqueue(ctx context.Context, jobType string, payload any) (bool, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("namelessjob: encoding %s payload: %w", jobType, err)
	}
	if _, err := s.queue.Enqueue(ctx, jobType, raw, jobs.EnqueueOptions{}); err != nil {
		if errors.Is(err, jobs.ErrDuplicate) {
			return false, nil
		}
		return false, fmt.Errorf("namelessjob: enqueuing %s: %w", jobType, err)
	}
	return true, nil
}

// actor is the audit provenance of the operator who scheduled the job, carried in
// the payload so the audit row the job writes names them and their client rather
// than the anonymous worker that happened to run it.
type actor struct {
	UID       string `json:"uid,omitempty"`
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

// actorFrom converts request-derived audit metadata into the payload's actor.
func actorFrom(meta audit.Meta) actor {
	return actor{UID: meta.ActorUID, IP: meta.IP, UserAgent: meta.UserAgent}
}

// entry builds the audit entry for one repair step, stamped with the scheduling
// operator. A job scheduled by the CLI carries no actor (stored NULL), the same
// convention offline batch transitions already use.
func (a actor) entry(action, subjectUID string, details map[string]any) audit.Entry {
	return audit.Entry{
		ActorUID:   a.UID,
		Action:     action,
		TargetType: "subject",
		TargetUID:  subjectUID,
		Details:    details,
		IP:         a.IP,
		UserAgent:  a.UserAgent,
	}
}

// detachPayload is the argument of a nameless_detach job: which project's subject
// to detach and how much the undo file handed to the operator says it owned, so
// the handler can report a divergence rather than silently detaching more than
// the undo can put back.
type detachPayload struct {
	ProjectUID  string `json:"project_uid"`
	SubjectUID  string `json:"subject_uid"`
	MarkerCount int    `json:"marker_count"`
	FaceCount   int    `json:"face_count"`
	Actor       actor  `json:"actor,omitzero"`
}

// restorePayload is the argument of a nameless_restore job: one snapshot out of
// an undo file and the project it goes back into. The whole snapshot travels in
// the payload so the job is self-contained and survives a restart — the links it
// restores exist nowhere else once the detach ran. SubjectUID repeats the
// snapshot's subject at the top level, where the dedup index reads it.
type restorePayload struct {
	ProjectUID string                 `json:"project_uid"`
	SubjectUID string                 `json:"subject_uid"`
	Snapshot   people.SubjectSnapshot `json:"snapshot"`
	Actor      actor                  `json:"actor,omitzero"`
}

// projectOrBootstrap returns the project a job's payload names, or the bootstrap
// project for a job queued before the payload carried one — the only project
// there was when it was queued.
func projectOrBootstrap(projectUID string) string {
	if projectUID == "" {
		return project.BootstrapUID
	}
	return projectUID
}

// HandleDetach runs one nameless_detach job: it deletes the subject and leaves
// its markers and cached faces unassigned, audited inside the same transaction.
//
// A subject that is already gone completes the job instead of failing it — a
// retry after a lost result, a double-click, or the CLI having got there first
// all mean the repair is done. Detaching more than the delivered undo file
// records is logged as a warning: replaying that file would leave the difference
// unassigned, and the operator has to know.
func (s *Service) HandleDetach(ctx context.Context, job jobs.Job) error {
	var p detachPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("namelessjob: decoding detach payload of job %d: %w", job.ID, err)
	}
	if p.SubjectUID == "" {
		return fmt.Errorf("namelessjob: detach job %d carries no subject uid", job.ID)
	}
	entry := p.Actor.entry(audit.ActionSubjectDelete, p.SubjectUID, map[string]any{
		"reason":  detachReason,
		"markers": p.MarkerCount,
		"faces":   p.FaceCount,
	})
	projectUID := projectOrBootstrap(p.ProjectUID)
	entry.ProjectUID = projectUID
	snap, err := s.store.DetachSubject(ctx, projectUID, p.SubjectUID, entry)
	if err != nil {
		if errors.Is(err, people.ErrSubjectNotFound) {
			s.log.InfoContext(ctx, "nameless subject already detached",
				slog.String("subject_uid", p.SubjectUID))
			return nil
		}
		return fmt.Errorf("namelessjob: detaching subject %s: %w", p.SubjectUID, err)
	}
	if len(snap.MarkerUIDs) != p.MarkerCount || len(snap.Faces) != p.FaceCount {
		s.log.WarnContext(ctx, "nameless subject grew between the undo file and the detach",
			slog.String("subject_uid", p.SubjectUID),
			slog.Int("detached_markers", len(snap.MarkerUIDs)),
			slog.Int("detached_faces", len(snap.Faces)),
			slog.Int("undo_markers", p.MarkerCount),
			slog.Int("undo_faces", p.FaceCount))
	}
	return nil
}

// HandleRestore runs one nameless_restore job: it re-creates the snapshotted
// subject under its original uid and timestamps and re-links the markers and
// cached faces it owned, audited inside the same transaction. Only the slug may
// differ, because another subject may have taken the base slug meanwhile.
func (s *Service) HandleRestore(ctx context.Context, job jobs.Job) error {
	var p restorePayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("namelessjob: decoding restore payload of job %d: %w", job.ID, err)
	}
	if p.Snapshot.Subject.UID == "" {
		return fmt.Errorf("namelessjob: restore job %d carries no subject uid", job.ID)
	}
	entry := p.Actor.entry(audit.ActionSubjectCreate, p.Snapshot.Subject.UID, map[string]any{
		"reason":  restoreReason,
		"markers": len(p.Snapshot.MarkerUIDs),
		"faces":   len(p.Snapshot.Faces),
	})
	projectUID := projectOrBootstrap(p.ProjectUID)
	entry.ProjectUID = projectUID
	restored, err := s.store.RestoreSubject(ctx, projectUID, p.Snapshot, entry)
	if err != nil {
		return fmt.Errorf("namelessjob: restoring subject %s: %w", p.Snapshot.Subject.UID, err)
	}
	s.log.InfoContext(ctx, "nameless subject restored from an undo file",
		slog.String("subject_uid", restored.UID), slog.String("slug", restored.Slug),
		slog.Int("markers", len(p.Snapshot.MarkerUIDs)), slog.Int("faces", len(p.Snapshot.Faces)))
	return nil
}
