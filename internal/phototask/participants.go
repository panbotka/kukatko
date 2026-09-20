package phototask

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
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

// Join puts userUID on the task as somebody who acted on it, outside any
// transaction of the caller's. It is the path for an act the task store does not
// own — writing in the thread, which belongs to internal/comments — and it is
// idempotent, so the caller may replay it freely.
//
// It is deliberately not audited: what is worth recording is the act (the
// comment), and the participation is a consequence of it that the row itself
// already dates.
func (s *Store) Join(ctx context.Context, taskUID, userUID string) error {
	if userUID == "" {
		return nil
	}
	if _, err := s.pool.Exec(ctx, joinSQL, taskUID, userUID, nil); err != nil {
		return translate(err, "joining task participant")
	}
	return nil
}

// assignSQL puts somebody on a task by somebody else's decision. Unlike joining,
// it upgrades an existing row: a person who was already there by acting and is
// then explicitly asked has been asked, and the UI says so.
const assignSQL = `
INSERT INTO photo_task_participants (task_uid, user_uid, added_by)
VALUES ($1, $2, $3)
ON CONFLICT (task_uid, user_uid) DO UPDATE SET added_by = excluded.added_by`

// Assign puts userUID on the task on purpose — asking a particular person —
// and writes entry in the same transaction. A missing task yields ErrNotFound
// and a person with no account ErrUserNotFound; in either case nothing is
// written. Assigning somebody already on the task is not an error.
func (s *Store) Assign(ctx context.Context, taskUID, userUID string, entry audit.Entry) error {
	return s.inTx(ctx, "assigning task participant", func(tx pgx.Tx) error {
		if err := lockTask(ctx, tx, taskUID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, assignSQL, taskUID, userUID, nullableUID(entry.ActorUID)); err != nil {
			return fmt.Errorf("inserting task participant: %w", err)
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
// once; a person with no account fails the whole transaction (ErrUserNotFound
// once translated), because half a hand-over is worse than none.
func askParticipants(
	ctx context.Context, tx pgx.Tx, taskUID string, userUIDs []string, entry audit.Entry,
) error {
	for _, userUID := range uniqueUIDs(userUIDs) {
		if _, err := tx.Exec(ctx, assignSQL, taskUID, userUID, nullableUID(entry.ActorUID)); err != nil {
			return fmt.Errorf("inserting asked participant: %w", err)
		}
		asked := audit.Entry{
			ActorUID: entry.ActorUID, Action: audit.ActionTaskAssign,
			IP: entry.IP, UserAgent: entry.UserAgent,
		}
		if err := audit.Write(ctx, tx, withTarget(asked, taskUID, "user_uid", userUID)); err != nil {
			return err
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
	ctx context.Context, taskUID, userUID string, entry audit.Entry,
) (bool, error) {
	var removed bool
	err := s.inTx(ctx, "unassigning task participant", func(tx pgx.Tx) error {
		if err := lockTask(ctx, tx, taskUID); err != nil {
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

// participantsSQL reads one task's people, oldest membership first. Both user
// joins resolve a display name; added_by is a LEFT JOIN because the account that
// asked somebody may since have been deleted.
const participantsSQL = `
SELECT p.user_uid, COALESCE(NULLIF(pu.display_name, ''), pu.username, ''), p.joined_at,
	COALESCE(p.added_by, ''), COALESCE(NULLIF(au.display_name, ''), au.username, '')
FROM photo_task_participants p
JOIN users pu ON pu.uid = p.user_uid
LEFT JOIN users au ON au.uid = p.added_by
WHERE p.task_uid = $1
ORDER BY p.joined_at, p.user_uid`

// Participants returns the people on a task, oldest membership first. A task
// nobody is on yields an empty slice, not nil, and a task that does not exist is
// indistinguishable from one with nobody on it — the caller has already read the
// task by the time it asks.
func (s *Store) Participants(ctx context.Context, taskUID string) ([]Participant, error) {
	rows, err := s.pool.Query(ctx, participantsSQL, taskUID)
	if err != nil {
		return nil, fmt.Errorf("phototask: listing participants of %s: %w", taskUID, err)
	}
	defer rows.Close()

	out := make([]Participant, 0)
	for rows.Next() {
		var p Participant
		if err := rows.Scan(&p.UserUID, &p.Name, &p.JoinedAt,
			&p.AddedByUID, &p.AddedByName); err != nil {
			return nil, fmt.Errorf("phototask: scanning participant: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("phototask: iterating participants: %w", err)
	}
	return out, nil
}
