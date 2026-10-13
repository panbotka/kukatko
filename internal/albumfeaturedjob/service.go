package albumfeaturedjob

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/worker"
)

// Beginner opens the transaction an announcement runs in. *pgxpool.Pool
// satisfies it.
type Beginner interface {
	// Begin starts a transaction.
	Begin(ctx context.Context) (pgx.Tx, error)
}

// AlbumReader reads a featured album as the homepage strip lists it. It is
// satisfied by *organize.Store.
type AlbumReader interface {
	// FeaturedAlbumTx returns the album uid as the strip would list it, on
	// tx, and organize.ErrNotFeatured when the strip would not list it.
	FeaturedAlbumTx(ctx context.Context, tx pgx.Tx, uid string) (organize.FeaturedAlbum, error)
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
	// Albums reads the featured album. Required.
	Albums AlbumReader
	// Deliver records each notification and schedules its delivery. Required.
	Deliver Deliverer
	// Language is the language the message is written in; empty is Czech, the
	// instance's language. Accounts carry no language of their own yet.
	Language notification.Language
	// Logger receives what each run did; nil uses slog.Default().
	Logger *slog.Logger
}

// Service announces featured albums: the `album_featured_notify` job handler.
type Service struct {
	db      Beginner
	albums  AlbumReader
	deliver Deliverer
	lang    notification.Language
	log     *slog.Logger
}

// New returns a Service from cfg. It panics when a required dependency is
// missing, which is a wiring bug and should fail at startup rather than on the
// first job.
func New(cfg Config) *Service {
	if cfg.DB == nil || cfg.Albums == nil || cfg.Deliver == nil {
		panic("albumfeaturedjob: DB, Albums and Deliver are required")
	}
	lang := cfg.Language
	if lang == "" {
		lang = notification.LanguageCzech
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{db: cfg.DB, albums: cfg.Albums, deliver: cfg.Deliver, lang: lang, log: log}
}

// Handle runs one album_featured_notify job: it announces the album the payload
// names. A payload that names no album is a terminal failure — no retry can
// make it name one.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	albumUID, err := decodePayload(job.Payload)
	if err != nil {
		// The cause is already wrapped with this package's context; Terminal only
		// marks it permanent, so wrapping it again would add nothing.
		return worker.Terminal(err) //nolint:wrapcheck // see above
	}
	return s.Announce(ctx, albumUID)
}

// accountsSQL reads every account of the album's project $1 — its members and
// the maintainers, who see every project — with what the recipient rules need,
// in a stable order so a retry delivers in the same sequence. The role is the
// one in that project, admin for a maintainer. An account of another project
// only is not read at all: an album is news only inside its own library.
const accountsSQL = `
SELECT u.uid, CASE WHEN u.role = 'maintainer' THEN 'admin' ELSE COALESCE(m.role, '') END, u.disabled
FROM users u
LEFT JOIN project_members m ON m.user_uid = u.uid AND m.project_uid = $1
WHERE u.role = 'maintainer' OR m.user_uid IS NOT NULL
ORDER BY u.uid`

// albumProjectSQL reads the project an album belongs to.
const albumProjectSQL = `SELECT project_id FROM albums WHERE uid = $1`

// Announce delivers the "a new album is featured" notification for albumUID,
// in one transaction, to every account the rules allow (see recipients) that
// wants the kind through some channel — the album's title in the headline,
// its cover as the frozen photo set, the album's page as the link, and the
// album_featured mail for the e-mail channel.
//
// It sends nothing — and completes — when the album is no longer featured
// (unfeatured before the job ran, lapsed, or deleted). Each delivery runs
// under a savepoint, so one account whose delivery fails is logged and skipped
// rather than costing everybody else their notification; the transaction
// commits either way.
func (s *Service) Announce(ctx context.Context, albumUID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("albumfeaturedjob: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	album, err := s.albums.FeaturedAlbumTx(ctx, tx, albumUID)
	if errors.Is(err, organize.ErrNotFeatured) {
		s.log.InfoContext(ctx, "featured album not announced: no longer featured", slog.String("album_uid", albumUID))
		return nil
	}
	if err != nil {
		return fmt.Errorf("albumfeaturedjob: reading album %s: %w", albumUID, err)
	}
	var projectUID string
	if err := tx.QueryRow(ctx, albumProjectSQL, album.UID).Scan(&projectUID); err != nil {
		return fmt.Errorf("albumfeaturedjob: reading the project of album %s: %w", album.UID, err)
	}
	accounts, err := readAccounts(ctx, tx, projectUID)
	if err != nil {
		return err
	}
	msg := s.message(projectUID, album)
	var actor string
	if album.FeaturedBy != nil {
		actor = *album.FeaturedBy
	}
	for _, userUID := range recipients(accounts, actor, album.Private) {
		s.announceTo(ctx, tx, userUID, album.UID, msg)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("albumfeaturedjob: commit: %w", err)
	}
	return nil
}

// announceTo delivers msg to userUID under a savepoint of tx, logging a failed
// delivery instead of returning it.
func (s *Service) announceTo(ctx context.Context, tx pgx.Tx, userUID, albumUID string, msg notifydeliver.Message) {
	var result notifydeliver.Result
	err := inSavepoint(ctx, tx, func(savepoint pgx.Tx) error {
		var err error
		result, err = s.deliver.Deliver(ctx, savepoint, userUID, msg)
		if err != nil {
			return fmt.Errorf("albumfeaturedjob: delivering: %w", err)
		}
		return nil
	})
	if err != nil {
		s.log.WarnContext(ctx, "featured album: could not notify an account",
			slog.String("user_uid", userUID), slog.String("album_uid", albumUID), slog.String("error", err.Error()))
		return
	}
	if result.Notification == nil {
		return
	}
	s.log.DebugContext(ctx, "featured album announced",
		slog.String("user_uid", userUID), slog.String("album_uid", albumUID),
		slog.String("notification_uid", result.Notification.UID), slog.Int("devices", result.Devices),
		slog.Bool("in_app", result.Channels.InApp), slog.Bool("email", result.Channels.Email))
}

// readAccounts runs accountsSQL for the project projectUID on tx.
func readAccounts(ctx context.Context, tx pgx.Tx, projectUID string) ([]account, error) {
	rows, err := tx.Query(ctx, accountsSQL, projectUID)
	if err != nil {
		return nil, fmt.Errorf("albumfeaturedjob: reading the accounts: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (account, error) {
		var a account
		var role string
		if err := row.Scan(&a.UID, &role, &a.Disabled); err != nil {
			return account{}, fmt.Errorf("scanning an account: %w", err)
		}
		a.Role = project.Role(role)
		return a, nil
	})
	if err != nil {
		return nil, fmt.Errorf("albumfeaturedjob: reading the accounts: %w", err)
	}
	return out, nil
}

// message is the notification every recipient gets for album, in the album's
// project projectUID: the kind, the
// text in the service's language, the album's page as the link, the cover as
// the frozen photo set (none for an album with nothing to show) and the mail
// builder for the e-mail channel.
func (s *Service) message(projectUID string, album organize.FeaturedAlbum) notifydeliver.Message {
	msg := notifydeliver.Message{
		ProjectUID: projectUID,
		Kind:       notification.KindAlbumFeatured,
		Text:       notification.RenderAlbumFeatured(s.lang, album.Title),
		Link:       AlbumPath(album.UID),
		Mail:       albumFeaturedMail(album.Title),
	}
	if album.CoverUID != nil && *album.CoverUID != "" {
		msg.PhotoUIDs = []string{*album.CoverUID}
	}
	return msg
}

// albumFeaturedMail returns the builder of the album_featured mail for the
// album titled title; url is the absolute link to the album's page.
func albumFeaturedMail(title string) notifydeliver.MailBuilder {
	return func(to notifydeliver.Recipient, _ notification.Notification, url string) mailjob.Mail {
		return mailjob.AlbumFeatured(to.Email, mailer.AlbumFeaturedData{
			DisplayName: to.DisplayName, Title: title, URL: url,
		})
	}
}

// inSavepoint runs fn on a savepoint of tx and releases it when fn returns nil;
// on an error, fn's writes are rolled back and tx stays usable — the reason a
// best-effort step runs under one at all. It returns fn's error, or the failure
// to open or release the savepoint.
func inSavepoint(ctx context.Context, tx pgx.Tx, fn func(savepoint pgx.Tx) error) error {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("albumfeaturedjob: opening a savepoint: %w", err)
	}
	// After a commit the rollback is a no-op; on every early return it undoes
	// whatever the savepoint wrote.
	defer func() { _ = savepoint.Rollback(ctx) }()
	if err := fn(savepoint); err != nil {
		return err
	}
	if err := savepoint.Commit(ctx); err != nil {
		return fmt.Errorf("albumfeaturedjob: releasing a savepoint: %w", err)
	}
	return nil
}
