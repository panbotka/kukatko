package tagnotifyjob

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/worker"
)

// Beginner opens the transaction a window is closed in. *pgxpool.Pool
// satisfies it.
type Beginner interface {
	// Begin starts a transaction.
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Deliverer delivers one notification to the channels an account wants, on the
// caller's transaction. It is satisfied by *notifydeliver.Deliverer.
type Deliverer interface {
	// Deliver records msg for userUID and fans it out to the wanted channels,
	// all on tx.
	Deliver(ctx context.Context, tx pgx.Tx, userUID string, msg notifydeliver.Message) (notifydeliver.Result, error)
}

// Config bundles the dependencies of New.
type Config struct {
	// DB opens the job's transaction. Required.
	DB Beginner
	// Deliver records the notification and schedules its delivery. Required.
	Deliver Deliverer
	// Language is the language the message is written in; empty is Czech, the
	// instance's language. Accounts carry no language of their own yet.
	Language notification.Language
	// Logger receives what each run did; nil uses slog.Default().
	Logger *slog.Logger
}

// Service closes tagging windows: the `tag_notify` job handler.
type Service struct {
	db      Beginner
	deliver Deliverer
	lang    notification.Language
	log     *slog.Logger
}

// New returns a Service from cfg. It panics when a required dependency is
// missing, which is a wiring bug and should fail at startup rather than on the
// first job.
func New(cfg Config) *Service {
	if cfg.DB == nil || cfg.Deliver == nil {
		panic("tagnotifyjob: DB and Deliver are required")
	}
	lang := cfg.Language
	if lang == "" {
		lang = notification.LanguageCzech
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{db: cfg.DB, deliver: cfg.Deliver, lang: lang, log: log}
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
// pending notice and delivers one notification naming the photos that still
// qualify, newest first, to every channel the account wants. Either all of it
// commits or none of it does, so a failure part-way leaves the notices for the
// retry instead of losing them, and a success cannot announce them twice.
//
// It sends nothing — and completes — when nothing qualifies (everything was
// untagged, archived or made private inside the window, or the account is gone
// and the cascade took its notices), and when the account turned every channel
// of the kind off since the window opened; the notices are consumed either way.
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

// announce delivers the notification for photos on tx, to whichever channels
// the account still wants (none: nothing is sent). A set over
// notification.MaxPhotos keeps its newest MaxPhotos photos, and the count says
// what the set holds, so the message never promises more than tapping it opens.
func (s *Service) announce(ctx context.Context, tx pgx.Tx, userUID string, photos []string) error {
	if len(photos) > notification.MaxPhotos {
		photos = photos[:notification.MaxPhotos]
	}
	count := len(photos)
	result, err := s.deliver.Deliver(ctx, tx, userUID, notifydeliver.Message{
		Kind: notification.KindTagged, Text: notification.RenderTagged(s.lang, count),
		SelfLink: true, PhotoUIDs: photos, Mail: taggedMail(count),
	})
	if err != nil {
		return fmt.Errorf("tagnotifyjob: delivering the notification of %s: %w", userUID, err)
	}
	if result.Notification == nil {
		s.log.DebugContext(ctx, "tag window closed unannounced: the kind is off", slog.String("user_uid", userUID))
		return nil
	}
	s.log.DebugContext(ctx, "tag window announced",
		slog.String("user_uid", userUID), slog.String("notification_uid", result.Notification.UID),
		slog.Int("photos", count), slog.Int("devices", result.Devices),
		slog.Bool("in_app", result.Channels.InApp), slog.Bool("email", result.Channels.Email))
	return nil
}

// taggedMail builds the tagged kind's own e-mail for a set of count photos: the
// count and the link to the notification's page, which opens exactly that set.
func taggedMail(count int) notifydeliver.MailBuilder {
	return func(to notifydeliver.Recipient, _ notification.Notification, url string) mailjob.Mail {
		return mailjob.Tagged(to.Email, mailer.TaggedData{DisplayName: to.DisplayName, Count: count, URL: url})
	}
}
