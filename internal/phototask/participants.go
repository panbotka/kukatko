package phototask

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/project"
)

// joinSQL records that somebody is on a task. It is idempotent and it never
// downgrades: a person who was *asked* and then acts keeps the record of having
// been asked, because ON CONFLICT DO NOTHING leaves the original row — including
// its added_by — exactly as it was.
const joinSQL = `
INSERT INTO photo_task_participants (task_uid, user_uid, added_by)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING`

// joinParticipant puts actorUID on the task inside the caller's transaction, so
// participation lands with — and rolls back with — the act that caused it.
//
// An empty actor is a no-op rather than an error: a write can come from an
// unauthenticated internal path, and refusing it there would make the task
// itself unwritable for the sake of bookkeeping.
func joinParticipant(ctx context.Context, tx pgx.Tx, taskUID, actorUID string) error {
	if actorUID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, joinSQL, taskUID, actorUID, nil); err != nil {
		return fmt.Errorf("joining task participant: %w", err)
	}
	return nil
}

// joinInProjectSQL is joinSQL for a caller outside the task's transaction: the
// row is written only when the task belongs to the project $3, so a join can
// never reach into another project. A task that is not there writes nothing. The
// casts pin $1 and $2 to one type: each is used both as a value and in a
// comparison, and PostgreSQL refuses to deduce two types for one parameter.
const joinInProjectSQL = `
INSERT INTO photo_task_participants (task_uid, user_uid, added_by)
SELECT $1::text, $2::text, NULL
WHERE EXISTS (SELECT 1 FROM photo_tasks t WHERE t.uid = $1::text AND t.project_id = $3)
ON CONFLICT DO NOTHING`

// Join puts userUID on the project's task as somebody who acted on it, outside any
// transaction of the caller's. It is the path for an act the task store does not
// own — writing in the thread, which belongs to internal/comments — and it is
// idempotent, so the caller may replay it freely.
//
// It is deliberately not audited: what is worth recording is the act (the
// comment), and the participation is a consequence of it that the row itself
// already dates.
func (s *Store) Join(ctx context.Context, projectUID, taskUID, userUID string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	if userUID == "" {
		return nil
	}
	if _, err := s.pool.Exec(ctx, joinInProjectSQL, taskUID, userUID, projectUID); err != nil {
		return translate(err, "joining task participant")
	}
	return nil
}

// assignSQL puts somebody on a task by somebody else's decision. Unlike joining,
// it upgrades an existing row: a person who was already there by acting and is
// then explicitly asked has been asked, and the UI says so. It reports whether
// the row is new — xmax is zero for a row this statement inserted and the
// updating transaction's id for one it upgraded — which is what tells a fresh
// hand-over (worth announcing) from a repeated one.
const assignSQL = `
INSERT INTO photo_task_participants (task_uid, user_uid, added_by)
VALUES ($1, $2, $3)
ON CONFLICT (task_uid, user_uid) DO UPDATE SET added_by = excluded.added_by
RETURNING (xmax = 0)`

// ask runs assignSQL for userUID on taskUID — a task of projectUID — as asked
// by actorUID and reports the fresh assignment to the store's observer when the
// person was not on the task before. what names the step for the error.
//
// Only an approved member of the project can be asked (project.
// RequireApprovedMembers): anybody else — an account of another project, a
// pending member, the system administrator outside their memberships — is
// ErrUserNotFound, exactly like a uid with no account, so a task is neither a
// way to plant a foreign name in the project nor an oracle of which accounts
// exist.
func (s *Store) ask(ctx context.Context, tx pgx.Tx, projectUID, taskUID, userUID, actorUID, what string) error {
	if err := project.RequireApprovedMembers(ctx, tx, projectUID, userUID); err != nil {
		if errors.Is(err, project.ErrNotMember) {
			return ErrUserNotFound
		}
		return fmt.Errorf("%s: %w", what, err)
	}
	var inserted bool
	if err := tx.QueryRow(ctx, assignSQL, taskUID, userUID, nullableUID(actorUID)).Scan(&inserted); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	a := Assignment{TaskUID: taskUID, UserUID: userUID, ActorUID: actorUID}
	if err := s.assigned(ctx, tx, inserted, a); err != nil {
		return fmt.Errorf("reporting the assignment of %s to %s: %w", userUID, taskUID, err)
	}
	return nil
}

// Assign puts userUID on the task on purpose — asking a particular person —
// and writes entry in the same transaction. A missing task — or one of another
// project — yields ErrNotFound and a person who is not an approved member of
// the project — no account at all included — ErrUserNotFound; in either case
// nothing is written. Assigning somebody already on the task is not an error.
// An assign observer (WithAssignObserver) hears the change only when the
// person was not on the task before — re-asking is silent.
func (s *Store) Assign(ctx context.Context, projectUID, taskUID, userUID string, entry audit.Entry) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	entry = withProject(entry, projectUID)
	return s.inTx(ctx, "assigning task participant", func(tx pgx.Tx) error {
		if err := lockTask(ctx, tx, projectUID, taskUID); err != nil {
			return err
		}
		if err := s.ask(ctx, tx, projectUID, taskUID, userUID, entry.ActorUID, "inserting task participant"); err != nil {
			return err
		}
		// The person doing the asking is on the task too — they are the one
		// waiting for the answer.
		if err := joinParticipant(ctx, tx, taskUID, entry.ActorUID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, withTarget(entry, taskUID, "user_uid", userUID))
	})
}

// askParticipants puts each of userUIDs on the task as asked by entry's actor
// and audits each as an assign, inside the caller's transaction — so a task
// handed to somebody at its opening reads, in the participants and in the
// trail, exactly as one handed over afterwards. A uid given twice is asked
// once; a person who is not an approved member of projectUID (no account at
// all included) fails the whole transaction with ErrUserNotFound, because half
// a hand-over is worse than none. The store's
// assign observer hears each person who was not on the task yet — at an
// opening that is everybody but the actor, who joined by opening it.
func (s *Store) askParticipants(
	ctx context.Context, tx pgx.Tx, projectUID, taskUID string, userUIDs []string, entry audit.Entry,
) error {
	for _, userUID := range uniqueUIDs(userUIDs) {
		if err := s.ask(ctx, tx, projectUID, taskUID, userUID, entry.ActorUID, "inserting asked participant"); err != nil {
			return err
		}
		asked := audit.Entry{
			ActorUID: entry.ActorUID, Action: audit.ActionTaskAssign,
			IP: entry.IP, UserAgent: entry.UserAgent, ProjectUID: entry.ProjectUID,
		}
		if err := audit.Write(ctx, tx, withTarget(asked, taskUID, "user_uid", userUID)); err != nil {
			return fmt.Errorf("auditing asked participant: %w", err)
		}
	}
	return nil
}

// uniqueUIDs drops blanks and repeats from uids, keeping first-seen order, so
// asking the same person twice in one request asks them once.
func uniqueUIDs(uids []string) []string {
	seen := make(map[string]struct{}, len(uids))
	out := make([]string, 0, len(uids))
	for _, uid := range uids {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		out = append(out, uid)
	}
	return out
}

// Unassign takes userUID off the task and writes entry in the same transaction,
// reporting whether a row was actually removed. Somebody who was not on the task
// is not an error — the end state is what was asked for either way.
//
// Taking a person off is allowed whether they were asked or joined by acting:
// participation is a statement about who is involved now, not an archive of who
// once touched it. The audit trail is that archive.
func (s *Store) Unassign(
	ctx context.Context, projectUID, taskUID, userUID string, entry audit.Entry,
) (bool, error) {
	if err := requireProject(projectUID); err != nil {
		return false, err
	}
	entry = withProject(entry, projectUID)
	var removed bool
	err := s.inTx(ctx, "unassigning task participant", func(tx pgx.Tx) error {
		if err := lockTask(ctx, tx, projectUID, taskUID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			"DELETE FROM photo_task_participants WHERE task_uid = $1 AND user_uid = $2",
			taskUID, userUID)
		if err != nil {
			return fmt.Errorf("deleting task participant: %w", err)
		}
		removed = tag.RowsAffected() > 0
		return audit.Write(ctx, tx, withTarget(entry, taskUID, "user_uid", userUID))
	})
	return removed, err
}

// participantsSQL reads one task's people, oldest membership first, $2 being
// the task's project. Both user joins resolve a display name; added_by is a LEFT
// JOIN because the account that asked somebody may since have been deleted.
// An account that has left the project is a former member (participantFormerParam):
// its name reads empty and its former flag is set.
const participantsSQL = `
SELECT p.user_uid,
	CASE WHEN ` + participantFormerParam + ` THEN '' ELSE COALESCE(NULLIF(pu.display_name, ''), pu.username, '') END,
	p.joined_at, COALESCE(p.added_by, ''),
	CASE WHEN ` + askerFormerParam + ` THEN '' ELSE COALESCE(NULLIF(au.display_name, ''), au.username, '') END,
	` + participantFormerParam + `, COALESCE(` + askerFormerParam + `, false)
FROM photo_task_participants p
JOIN users pu ON pu.uid = p.user_uid
LEFT JOIN users au ON au.uid = p.added_by
WHERE p.task_uid = $1
ORDER BY p.joined_at, p.user_uid`

// The former-member tests: true when the account row aliased pu (the
// participant) or au (whoever asked) is no member of the task's project any
// more — a system administrator included, whose implicit access the project
// must not learn of (project.FormerMemberSQL). A task's people keep their rows after
// they leave, and the project must not keep reading their live names. A
// membership whose identity is withheld counts as none: a blind re-add of a
// former member's guessed address must not bring the name back. Each is NULL
// for a missing (LEFT JOINed) account. The Param pair reads the project
// from $2 (participantsSQL), the Task pair from the task row t (taskJoins).
const (
	participantFormerParam = `(CASE WHEN pu.uid IS NULL THEN NULL ELSE NOT EXISTS (SELECT 1 FROM project_members fm
	WHERE fm.user_uid = pu.uid AND fm.project_uid = $2 AND NOT fm.identity_withheld) END)`
	askerFormerParam = `(CASE WHEN au.uid IS NULL THEN NULL ELSE NOT EXISTS (SELECT 1 FROM project_members fm
	WHERE fm.user_uid = au.uid AND fm.project_uid = $2 AND NOT fm.identity_withheld) END)`
	participantFormerTask = `(CASE WHEN pu.uid IS NULL THEN NULL ELSE NOT EXISTS (SELECT 1 FROM project_members fm
	WHERE fm.user_uid = pu.uid AND fm.project_uid = t.project_id AND NOT fm.identity_withheld) END)`
	askerFormerTask = `(CASE WHEN au.uid IS NULL THEN NULL ELSE NOT EXISTS (SELECT 1 FROM project_members fm
	WHERE fm.user_uid = au.uid AND fm.project_uid = t.project_id AND NOT fm.identity_withheld) END)`
)

// Participants returns the people on the project's task, oldest membership
// first. A task nobody is on yields an empty slice, not nil; a task that does not
// exist — or belongs to another project — is ErrNotFound, so the participants of
// a foreign task are answered exactly like a missing one.
func (s *Store) Participants(ctx context.Context, projectUID, taskUID string) ([]Participant, error) {
	found, err := s.InProject(ctx, projectUID, []string{taskUID})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, participantsSQL, taskUID, projectUID)
	if err != nil {
		return nil, fmt.Errorf("phototask: listing participants of %s: %w", taskUID, err)
	}
	defer rows.Close()

	out := make([]Participant, 0)
	for rows.Next() {
		var p Participant
		if err := rows.Scan(&p.UserUID, &p.Name, &p.JoinedAt,
			&p.AddedByUID, &p.AddedByName, &p.Former, &p.AddedByFormer); err != nil {
			return nil, fmt.Errorf("phototask: scanning participant: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("phototask: iterating participants: %w", err)
	}
	return out, nil
}
