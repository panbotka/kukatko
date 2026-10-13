package comments

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// resolveMentionsSQL turns the uids a body mentions into the accounts that can
// be mentioned on the comment $2: existing, enabled and in its project — an
// approved member, or a maintainer, who enters every project. A uid that
// matches no such row is dropped — the token still renders from its own name,
// but nothing is stored or announced for an account that could not be
// notified anyway. The linked person is the one the membership of the
// comment's project names.
const resolveMentionsSQL = `
SELECT u.uid, COALESCE(NULLIF(u.display_name, ''), u.username), pm.subject_uid
FROM users u
CROSS JOIN (SELECT ` + commentProjectSQL + ` AS project_uid FROM comments c WHERE c.uid = $2) cp
LEFT JOIN project_members pm ON pm.user_uid = u.uid AND pm.project_uid = cp.project_uid
WHERE u.uid = ANY($1) AND NOT u.disabled
  AND (pm.approved_at IS NOT NULL OR u.role = 'maintainer')`

// existingMentionsSQL reads who a comment mentioned before this write, so an
// edit can tell a fresh mention from one the previous body already carried.
const existingMentionsSQL = `SELECT user_uid FROM comment_mentions WHERE comment_uid = $1`

// pruneMentionsSQL drops the stored mentions an edited body no longer carries.
const pruneMentionsSQL = `DELETE FROM comment_mentions WHERE comment_uid = $1 AND NOT (user_uid = ANY($2))`

// upsertMentionSQL records one mention at its position in the body, moving a
// known one rather than duplicating it.
const upsertMentionSQL = `
INSERT INTO comment_mentions (comment_uid, user_uid, position)
VALUES ($1, $2, $3)
ON CONFLICT (comment_uid, user_uid) DO UPDATE SET position = EXCLUDED.position`

// mentionsThen returns the post-mutation step of a create or an edit: sync the
// comment's stored mentions with its body, report the fresh ones to the
// observer, then run the caller's along (nil runs nothing). The caller's along
// goes last so its error reaches the caller exactly as it came, as Along
// promises.
func (s *Store) mentionsThen(along Along) postStep {
	return func(ctx context.Context, tx pgx.Tx, c *Comment) error {
		fresh, err := s.syncMentions(ctx, tx, c)
		if err != nil {
			return err
		}
		if len(fresh) > 0 && s.mentions != nil {
			if err := s.mentions.Mentioned(ctx, tx, *c, fresh); err != nil {
				return fmt.Errorf("comments: reporting the mentions of %s: %w", c.UID, err)
			}
		}
		if along != nil {
			return along(ctx, tx)
		}
		return nil
	}
}

// syncMentions makes comment_mentions agree with c's body on tx: it resolves
// the body's tokens to mentionable accounts, prunes the rows the body no longer
// carries, writes the rest in body order and sets c.Mentions to the resolved
// list — the read-back of a create ran before any row existed. It returns the
// mentions that are new on the comment, the author's own left out: a
// self-mention is stored (it renders like any other) but is nobody's news.
func (s *Store) syncMentions(ctx context.Context, tx pgx.Tx, c *Comment) ([]Mention, error) {
	resolved, err := resolveMentions(ctx, tx, c.UID, ParseMentions(c.Body))
	if err != nil {
		return nil, fmt.Errorf("comments: resolving the mentions of %s: %w", c.UID, err)
	}
	before, err := existingMentions(ctx, tx, c.UID)
	if err != nil {
		return nil, fmt.Errorf("comments: reading the mentions of %s: %w", c.UID, err)
	}
	uids := make([]string, 0, len(resolved))
	for _, m := range resolved {
		uids = append(uids, m.UserUID)
	}
	if _, err := tx.Exec(ctx, pruneMentionsSQL, c.UID, uids); err != nil {
		return nil, fmt.Errorf("comments: pruning the mentions of %s: %w", c.UID, err)
	}
	fresh := make([]Mention, 0, len(resolved))
	for i, m := range resolved {
		if _, err := tx.Exec(ctx, upsertMentionSQL, c.UID, m.UserUID, i); err != nil {
			return nil, fmt.Errorf("comments: storing the mention of %s on %s: %w", m.UserUID, c.UID, err)
		}
		if !slices.Contains(before, m.UserUID) && m.UserUID != c.AuthorUID {
			fresh = append(fresh, m)
		}
	}
	c.Mentions = resolved
	return fresh, nil
}

// resolveMentions returns the accounts among refs that can be mentioned on the
// comment commentUID, in the order of refs, each with its current name and
// linked person. It is an empty slice, never nil, when nothing resolves.
func resolveMentions(ctx context.Context, tx pgx.Tx, commentUID string, refs []MentionRef) ([]Mention, error) {
	out := make([]Mention, 0, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	uids := make([]string, 0, len(refs))
	for _, ref := range refs {
		uids = append(uids, ref.UserUID)
	}
	rows, err := tx.Query(ctx, resolveMentionsSQL, uids, commentUID)
	if err != nil {
		return nil, fmt.Errorf("querying accounts: %w", err)
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Mention, error) {
		var m Mention
		if err := row.Scan(&m.UserUID, &m.Name, &m.SubjectUID); err != nil {
			return Mention{}, fmt.Errorf("scanning account: %w", err)
		}
		return m, nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading accounts: %w", err)
	}
	byUID := make(map[string]Mention, len(found))
	for _, m := range found {
		byUID[m.UserUID] = m
	}
	for _, ref := range refs {
		if m, ok := byUID[ref.UserUID]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// existingMentions returns the accounts comment uid mentioned before this
// write.
func existingMentions(ctx context.Context, tx pgx.Tx, uid string) ([]string, error) {
	rows, err := tx.Query(ctx, existingMentionsSQL, uid)
	if err != nil {
		return nil, fmt.Errorf("querying comment_mentions: %w", err)
	}
	before, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("reading comment_mentions: %w", err)
	}
	return before, nil
}
