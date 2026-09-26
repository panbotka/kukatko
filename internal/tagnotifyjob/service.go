package tagnotifyjob

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/pushjob"
	"github.com/panbotka/kukatko/internal/worker"
)

// Beginner opens the transaction a window is closed in. *pgxpool.Pool
// satisfies it.
type Beginner interface {
	// Begin starts a transaction.
	Begin(ctx context.Context) (pgx.Tx, error)
}

// NotificationRecorder reads the account's preference and records the
// notification, both on the job's transaction. It is satisfied by
// *notification.Store.
type NotificationRecorder interface {
	PreferenceReader
	// CreateTx records n on tx and returns the stored notification.
	CreateTx(ctx context.Context, tx pgx.Tx, n notification.New) (notification.Notification, error)
}

// PushScheduler schedules one push notification to every device of an account
// through the caller's executor. It is satisfied by *pushjob.Enqueuer, which
// itself queues nothing when push is off or the account has no device.
type PushScheduler interface {
	// Enqueue schedules n for userUID's devices using exec and returns how many
	// deliveries it queued.
	Enqueue(ctx context.Context, exec pushjob.Execer, userUID string, n push.Notification) (int, error)
}

// Config bundles the dependencies of New.
type Config struct {
	// DB opens the job's transaction. Required.
	DB Beginner
	// Notifications records the notification. Required.
	Notifications NotificationRecorder
	// Push schedules its delivery. Required.
	Push PushScheduler
	// Language is the language the message is written in; empty is Czech, the
	// instance's language. Accounts carry no language of their own yet.
	Language notification.Language
	// Logger receives what each run did; nil uses slog.Default().
	Logger *slog.Logger
}

// Service closes tagging windows: the `tag_notify` job handler.
type Service struct {
	db    Beginner
	notes NotificationRecorder
	push  PushScheduler
	lang  notification.Language
	log   *slog.Logger
}

// New returns a Service from cfg. It panics when a required dependency is
// missing, which is a wiring bug and should fail at startup rather than on the
// first job.
func New(cfg Config) *Service {
	if cfg.DB == nil || cfg.Notifications == nil || cfg.Push == nil {
		panic("tagnotifyjob: DB, Notifications and Push are required")
	}
	lang := cfg.Language
	if lang == "" {
		lang = notification.LanguageCzech
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{db: cfg.DB, notes: cfg.Notifications, push: cfg.Push, lang: lang, log: log}
}

// consumeSQL reads and deletes every pending notice of the account in one
// statement, and returns the photos that may still be named — visible under the
// notification reading and still carrying the account's linked person on a
// valid marker — newest photo first. Notices that no longer qualify are deleted
// all the same: they are what the window collected, and the window is closing.
const consumeSQL = `
WITH consumed AS (
    DELETE FROM tag_notices WHERE user_uid = $1 RETURNING photo_uid
)
SELECT p.uid
FROM consumed c
JOIN photos p ON p.uid = c.photo_uid
WHERE ` + notification.VisiblePhotoSQL + `
  AND EXISTS (SELECT 1 FROM markers m
              JOIN users u ON u.subject_uid = m.subject_uid
              WHERE u.uid = $1 AND m.photo_uid = p.uid AND NOT m.invalid)
ORDER BY p.taken_at DESC NULLS LAST, p.created_at DESC, p.uid`

// Handle runs one tag_notify job: it closes the window of the account the
// payload names. A payload that names no account is a terminal failure — no
// retry can make it name one.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	userUID, err := decodePayload(job.Payload)
	if err != nil {
		// The cause is already wrapped with this package's context; Terminal only
		// marks it permanent, so wrapping it again would add nothing.
		return worker.Terminal(err) //nolint:wrapcheck // see above
	}
	return s.Close(ctx, userUID)
}

// Close closes userUID's tagging window, in one transaction: it consumes every
// pending notice, records one notification naming the photos that still
// qualify, newest first, and schedules its push delivery. Either all of it
// commits or none of it does, so a failure part-way leaves the notices for the
// retry instead of losing them, and a success cannot announce them twice.
//
// It sends nothing — and completes — when nothing qualifies (everything was
// untagged, archived or made private inside the window, or the account is gone
// and the cascade took its notices), and when the account turned the kind off
// since the window opened; the notices are consumed either way.
func (s *Service) Close(ctx context.Context, userUID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("tagnotifyjob: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The same lock the recorder takes: a tag still in flight commits before
	// the notices are read, or waits until this run has committed and then
	// opens a new window.
	if _, err := tx.Exec(ctx, lockSQL, userUID); err != nil {
		return fmt.Errorf("tagnotifyjob: locking the window of %s: %w", userUID, err)
	}
	photos, err := consume(ctx, tx, userUID)
	if err != nil {
		return err
	}
	if len(photos) > 0 {
		if err := s.announce(ctx, tx, userUID, photos); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("tagnotifyjob: commit: %w", err)
	}
	return nil
}

// consume runs consumeSQL for userUID on tx.
func consume(ctx context.Context, tx pgx.Tx, userUID string) ([]string, error) {
	rows, err := tx.Query(ctx, consumeSQL, userUID)
	if err != nil {
		return nil, fmt.Errorf("tagnotifyjob: consuming the notices of %s: %w", userUID, err)
	}
	photos, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("tagnotifyjob: consuming the notices of %s: %w", userUID, err)
	}
	return photos, nil
}

// announce records the notification for photos and schedules its delivery, on
// tx, unless the account no longer wants the kind. A set over
// notification.MaxPhotos keeps its newest MaxPhotos photos, and the count says
// what the set holds, so the message never promises more than tapping it opens.
func (s *Service) announce(ctx context.Context, tx pgx.Tx, userUID string, photos []string) error {
	wants, err := s.notes.WantsTx(ctx, tx, userUID, notification.KindTagged)
	if err != nil {
		return fmt.Errorf("tagnotifyjob: reading the preference of %s: %w", userUID, err)
	}
	if !wants {
		s.log.DebugContext(ctx, "tag window closed unannounced: the kind is off", slog.String("user_uid", userUID))
		return nil
	}
	if len(photos) > notification.MaxPhotos {
		photos = photos[:notification.MaxPhotos]
	}
	text := notification.RenderTagged(s.lang, len(photos))
	record, err := s.notes.CreateTx(ctx, tx, notification.New{
		UserUID: userUID, Kind: notification.KindTagged, Title: text.Title, Body: text.Body,
		SelfLink: true, PhotoUIDs: photos,
	})
	if err != nil {
		return fmt.Errorf("tagnotifyjob: recording the notification of %s: %w", userUID, err)
	}
	devices, err := s.push.Enqueue(ctx, tx, userUID, push.Notification{
		Title: record.Title, Body: record.Body, URL: record.Link, Kind: string(record.Kind), Tag: record.UID,
	})
	if err != nil {
		return fmt.Errorf("tagnotifyjob: scheduling the push of %s: %w", userUID, err)
	}
	s.log.DebugContext(ctx, "tag window announced",
		slog.String("user_uid", userUID), slog.String("notification_uid", record.UID),
		slog.Int("photos", len(photos)), slog.Int("devices", devices))
	return nil
}
