package resolveapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kind names what an old link points at: the entity whose project the resolver
// looks up. The values are the {kind} segment of GET /resolve/{kind}/{uid}.
type Kind string

// The entities an address from before projects existed can name. Each is one
// of the frontend's routes with a uid (/photos/:uid, /albums/:uid and
// /albums/:uid/faces, /labels/:uid, /tasks/:uid, /people/:uid and its tree,
// /n/:uid).
const (
	KindPhoto        Kind = "photo"
	KindAlbum        Kind = "album"
	KindLabel        Kind = "label"
	KindTask         Kind = "task"
	KindSubject      Kind = "subject"
	KindNotification Kind = "notification"
)

// ErrNotFound is the one answer for every uid the caller may not be sent to: a
// uid nothing holds, one held by a project the caller cannot enter, and — for
// a notification — one addressed to somebody else. They are deliberately the
// same error, so the resolver is no oracle of another project's contents.
var ErrNotFound = errors.New("resolveapi: nothing the caller may open")

// The lookups, one per kind. Each joins the entity to its project and keeps the
// row only when the caller may enter that project, by the same rules the
// project guard applies to a request (auth.enterProject): a maintainer ($2)
// enters every project, deactivated or not, member or not; anybody else needs
// an approved membership ($3 is the caller) of an active project. A pending
// membership enters nothing and so resolves nothing.
//
// They are written out per table rather than built from one template so the
// tenancy guard reads every statement. Each one deliberately spans every
// project — finding the project is the whole point — which is why they are
// allow-listed there; the project-access predicate is what scopes them instead.
const (
	photoSQL = `SELECT p.slug FROM photos e JOIN projects p ON p.uid = e.project_id
		WHERE e.uid = $1 AND ` + enterableSQL
	albumSQL = `SELECT p.slug FROM albums e JOIN projects p ON p.uid = e.project_id
		WHERE e.uid = $1 AND ` + enterableSQL
	labelSQL = `SELECT p.slug FROM labels e JOIN projects p ON p.uid = e.project_id
		WHERE e.uid = $1 AND ` + enterableSQL
	taskSQL = `SELECT p.slug FROM photo_tasks e JOIN projects p ON p.uid = e.project_id
		WHERE e.uid = $1 AND ` + enterableSQL
	subjectSQL = `SELECT p.slug FROM subjects e JOIN projects p ON p.uid = e.project_id
		WHERE e.uid = $1 AND ` + enterableSQL
	// A notification is its addressee's alone: one addressed to somebody else
	// is as missing as one that never existed, even to a maintainer.
	notificationSQL = `SELECT p.slug FROM notifications e JOIN projects p ON p.uid = e.project_id
		WHERE e.uid = $1 AND e.user_uid = $3 AND ` + enterableSQL
)

// enterableSQL is the project-access predicate every lookup ends with; see the
// lookups above. p is the entity's project.
const enterableSQL = `($2::boolean OR (p.active AND EXISTS (
		SELECT 1 FROM project_members m
		WHERE m.project_uid = p.uid AND m.user_uid = $3 AND m.approved_at IS NOT NULL)))`

// lookups maps every Kind to its statement; a kind missing here does not exist.
var lookups = map[Kind]string{
	KindPhoto:        photoSQL,
	KindAlbum:        albumSQL,
	KindLabel:        labelSQL,
	KindTask:         taskSQL,
	KindSubject:      subjectSQL,
	KindNotification: notificationSQL,
}

// Caller is who asks: the account and whether it is the instance's maintainer.
type Caller struct {
	UserUID    string
	Maintainer bool
}

// Store answers which project holds an entity, over the shared pool.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store over pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ProjectSlug returns the slug of the project holding the kind entity uid when
// caller may enter that project, and ErrNotFound otherwise — for an unknown
// kind, a uid nothing holds, a project the caller cannot enter and a
// notification addressed to somebody else alike. Any other error is the
// database's, wrapped.
func (s *Store) ProjectSlug(ctx context.Context, kind Kind, uid string, caller Caller) (string, error) {
	sql, ok := lookups[kind]
	if !ok || uid == "" || caller.UserUID == "" {
		return "", ErrNotFound
	}
	var slug string
	err := s.pool.QueryRow(ctx, sql, uid, caller.Maintainer, caller.UserUID).Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolveapi: resolving %s %s: %w", kind, uid, err)
	}
	return slug, nil
}
