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
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/worker"
)

// Beginner opens the transaction a window is closed in. *pgxpool.Pool
// satisfies it.
type Beginner interface {
	// Begin starts a transaction.
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Deliverer delivers one notification to the channels an account wants, on the
// caller's transaction, and knows the public address its links live under. It
// is satisfied by *notifydeliver.Deliverer.
type Deliverer interface {
	// Deliver records msg for userUID and fans it out to the wanted channels,
	// all on tx.
	Deliver(ctx context.Context, tx pgx.Tx, userUID string, msg notifydeliver.Message) (notifydeliver.Result, error)
	// Absolute returns an in-app link under the instance's public address, or
	// "" when there is no address to put it under.
	Absolute(link string) string
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
// notification reading and still carrying, on a valid marker, the person the
// account's membership of the photo's project links — with their project,
// newest photo first. Notices that no longer qualify are deleted all the same:
// they are what the window collected, and the window is closing.
const consumeSQL = `
WITH consumed AS (
    DELETE FROM tag_notices WHERE user_uid = $1 RETURNING photo_uid
)
SELECT p.uid, p.project_id
FROM consumed c
JOIN photos p ON p.uid = c.photo_uid
WHERE ` + notification.VisiblePhotoSQL + `
  AND EXISTS (SELECT 1 FROM markers m
              JOIN project_members pm ON pm.subject_uid = m.subject_uid AND pm.project_uid = p.project_id
              WHERE pm.user_uid = $1 AND m.photo_uid = p.uid AND NOT m.invalid)
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
// pending notice, keeps the photos that still qualify, splits them by project
// and, inside each project, groups them by album and delivers one notification
// per group — one per album the photos are in (visible to the account; at most
// MaxAlbumGroups of them), then one for the photos outside those albums — each
// with its frozen photo set, newest first, into that project's inbox and to
// every channel the account wants; the e-mail is one per project of the window.
// Either all of it commits or none of it does, so a failure part-way leaves the
// notices for the retry instead of losing them, and a success cannot announce
// them twice.
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
	for _, w := range groupByProject(photos) {
		groups, err := collect(ctx, tx, userUID, w)
		if err != nil {
			return err
		}
		if err := s.announce(ctx, tx, userUID, w.ProjectUID, groups, len(w.Photos)); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("tagnotifyjob: commit: %w", err)
	}
	return nil
}

// consume runs consumeSQL for userUID on tx.
func consume(ctx context.Context, tx pgx.Tx, userUID string) ([]consumedPhoto, error) {
	rows, err := tx.Query(ctx, consumeSQL, userUID)
	if err != nil {
		return nil, fmt.Errorf("tagnotifyjob: consuming the notices of %s: %w", userUID, err)
	}
	photos, err := pgx.CollectRows(rows, pgx.RowToStructByPos[consumedPhoto])
	if err != nil {
		return nil, fmt.Errorf("tagnotifyjob: consuming the notices of %s: %w", userUID, err)
	}
	return photos, nil
}

// albumsSQL reads, for the window's photos, the albums of the project $2 each
// is in — the hand-made ones (type album), the ones a person knows by name, as
// the "what's new" digest and the featured strip count them — with what decides
// whether the recipient may be told about them.
const albumsSQL = `
SELECT ap.photo_uid, a.uid, a.title, a.private
FROM album_photos ap
JOIN albums a ON a.uid = ap.album_uid
WHERE ap.photo_uid = ANY($1) AND a.project_id = $2 AND a.type = 'album'
ORDER BY ap.photo_uid, a.title, a.uid`

// roleSQL reads the recipient's role in the project $2 the photos belong to —
// admin for a maintainer, "" for an account that is no member — which decides
// whether a private album may be named to them.
const roleSQL = `
SELECT CASE WHEN u.role = 'maintainer' THEN 'admin' ELSE COALESCE(m.role, '') END
FROM users u
LEFT JOIN project_members m ON m.user_uid = u.uid AND m.project_uid = $2
WHERE u.uid = $1`

// collect reads on tx what the grouping of one project's window needs — the
// albums of its photos and whether userUID may see a private one there (a
// curator or above, the rule the album surfaces apply) — and returns its
// groups. The window's photos are newest first, an order the groups keep.
func collect(ctx context.Context, tx pgx.Tx, userUID string, w projectWindow) ([]group, error) {
	photos := w.Photos
	var role string
	if err := tx.QueryRow(ctx, roleSQL, userUID, w.ProjectUID).Scan(&role); err != nil {
		return nil, fmt.Errorf("tagnotifyjob: reading the role of %s: %w", userUID, err)
	}
	rows, err := tx.Query(ctx, albumsSQL, photos, w.ProjectUID)
	if err != nil {
		return nil, fmt.Errorf("tagnotifyjob: reading the albums of the photos of %s: %w", userUID, err)
	}
	albums := make(map[string][]albumRef)
	var photoUID string
	var ref albumRef
	if _, err := pgx.ForEachRow(rows, []any{&photoUID, &ref.UID, &ref.Title, &ref.Private}, func() error {
		albums[photoUID] = append(albums[photoUID], ref)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("tagnotifyjob: reading the albums of the photos of %s: %w", userUID, err)
	}
	tagged := make([]taggedPhoto, len(photos))
	for i, uid := range photos {
		tagged[i] = taggedPhoto{UID: uid, Albums: albums[uid]}
	}
	return groupByAlbum(tagged, project.Role(role).CanCurate(), MaxAlbumGroups), nil
}

// mailGroup is one line of the window's single e-mail: a delivered group with
// the absolute link to its notification.
type mailGroup struct {
	group
	url string
}

// announce delivers one notification per group of the project projectUID on
// tx, in order, into that project's inbox and to whichever channels the account
// still wants (none: nothing is sent). A group's set over
// notification.MaxPhotos keeps its newest MaxPhotos photos, and its count says
// what the set holds, so no message promises more than tapping it opens. The
// e-mail is one per project of the window: every delivery but the last skips
// the mail, and the last one's mail lists every group with its count and link.
// total is how many distinct photos the project's window holds, which the mail
// reports.
func (s *Service) announce(
	ctx context.Context, tx pgx.Tx, userUID, projectUID string, groups []group, total int,
) error {
	delivered := make([]mailGroup, 0, len(groups))
	for i, g := range groups {
		if len(g.Photos) > notification.MaxPhotos {
			g.Photos = g.Photos[:notification.MaxPhotos]
		}
		last := i == len(groups)-1
		msg := notifydeliver.Message{
			ProjectUID: projectUID, Kind: notification.KindTagged, Text: s.text(g, len(groups)), SelfLink: true,
			PhotoUIDs: g.Photos, SkipMail: !last,
		}
		if last {
			msg.Mail = s.taggedMail(delivered, g, total)
		}
		result, err := s.deliver.Deliver(ctx, tx, userUID, msg)
		if err != nil {
			return fmt.Errorf("tagnotifyjob: delivering the notification of %s: %w", userUID, err)
		}
		if result.Notification == nil {
			s.log.DebugContext(ctx, "tag window closed unannounced: the kind is off", slog.String("user_uid", userUID))
			return nil
		}
		delivered = append(delivered, mailGroup{group: g, url: s.deliver.Absolute(result.Notification.Link)})
		s.log.DebugContext(ctx, "tag window announced",
			slog.String("user_uid", userUID), slog.String("notification_uid", result.Notification.UID),
			slog.String("album", albumUID(g)), slog.Int("photos", len(g.Photos)), slog.Int("devices", result.Devices),
			slog.Bool("in_app", result.Channels.InApp), slog.Bool("email", result.Channels.Email))
	}
	return nil
}

// text is what a group's notification says: the album's wording for an album
// group; for the other-photos group the plain wording when it is the window's
// only group, and "other photos" when album notifications stand beside it.
func (s *Service) text(g group, groups int) notification.Text {
	switch {
	case g.Album != nil:
		return notification.RenderTaggedInAlbum(s.lang, len(g.Photos), g.Album.Title)
	case groups == 1:
		return notification.RenderTagged(s.lang, len(g.Photos))
	default:
		return notification.RenderTaggedOther(s.lang, len(g.Photos))
	}
}

// albumUID names a group's album for the log; "" for the other-photos group.
func albumUID(g group) string {
	if g.Album == nil {
		return ""
	}
	return g.Album.UID
}

// taggedMail builds the window's one e-mail, sent with its last notification:
// a window of one group that is no album carries the count and the link to that
// notification's page; any window with an album lists every group — the ones
// delivered before plus last, whose link the builder receives — with its title,
// count and link, total being the window's distinct photos.
func (s *Service) taggedMail(before []mailGroup, last group, total int) notifydeliver.MailBuilder {
	return func(to notifydeliver.Recipient, _ notification.Notification, mail notifydeliver.MailContext) mailjob.Mail {
		data := mailer.TaggedData{DisplayName: to.DisplayName, ProjectName: mail.ProjectName, Count: total}
		if len(before) == 0 && last.Album == nil {
			data.URL = mail.URL
			return mailjob.Tagged(to.Email, data)
		}
		for _, g := range append(before, mailGroup{group: last, url: mail.URL}) {
			line := mailer.TaggedGroup{Count: len(g.Photos), URL: g.url}
			if g.Album != nil {
				line.Album = g.Album.Title
			}
			data.Groups = append(data.Groups, line)
		}
		return mailjob.Tagged(to.Email, data)
	}
}
