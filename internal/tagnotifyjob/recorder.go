package tagnotifyjob

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/people"
)

// PreferenceReader says whether an account wants a kind of notification, read
// on the caller's transaction. It is satisfied by *notification.Store.
type PreferenceReader interface {
	// WantsTx reports whether userUID wants notifications of kind, read on tx.
	WantsTx(ctx context.Context, tx pgx.Tx, userUID string, kind notification.Kind) (bool, error)
}

// RecorderConfig bundles what NewRecorder needs.
type RecorderConfig struct {
	// Enabled mirrors push.enabled: with push off nothing is recorded, because
	// nothing would ever be delivered. Untagging still clears notices, so a
	// window left open when push was switched off never names a photo the person
	// has since been taken off.
	Enabled bool
	// Window is how long a tagging window stays open (push.tags.window); a
	// non-positive value uses DefaultWindow.
	Window time.Duration
	// Preferences reads whether the account wants the tagged kind. Required.
	Preferences PreferenceReader
	// Logger records the notices that could not be recorded; nil uses
	// slog.Default().
	Logger *slog.Logger
	// Now reads the clock the window is measured from; nil uses time.Now.
	Now func() time.Time
}

// Recorder is the tagging half of the window: the people store's TagObserver.
// It records which photos an account was tagged on and opens the account's
// window with the first one.
//
// It is best-effort by design. A notification is worth less than the tag that
// caused it, so every call runs under a savepoint of the assignment's
// transaction: a notice that will not record is rolled back to it and logged,
// and the assignment commits all the same. Only a savepoint that cannot be
// opened or released — a transaction that is already broken — is returned.
type Recorder struct {
	enabled bool
	window  time.Duration
	prefs   PreferenceReader
	log     *slog.Logger
	now     func() time.Time
}

// Recorder satisfies the people store's hook.
var _ people.TagObserver = (*Recorder)(nil)

// NewRecorder builds a Recorder from cfg. It panics without Preferences, which
// is a wiring bug and should fail at startup rather than on the first tag.
func NewRecorder(cfg RecorderConfig) *Recorder {
	if cfg.Preferences == nil {
		panic("tagnotifyjob: Preferences is required")
	}
	window := cfg.Window
	if window <= 0 {
		window = DefaultWindow
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Recorder{enabled: cfg.Enabled, window: window, prefs: cfg.Preferences, log: log, now: now}
}

// recipientsSQL lists the accounts that are the tagged subject: those linked to
// it, still enabled and approved. A subject may be linked to several accounts (a
// household login and a personal one, migration 0060); ordering by uid keeps
// the advisory locks of one transaction in a stable order.
const recipientsSQL = `
SELECT uid FROM users
WHERE subject_uid = $1 AND NOT disabled AND approved_at IS NOT NULL
ORDER BY uid`

// announceableSQL reports whether the photo may be named to anybody at all: it
// is visible under the notification reading (not private, hidden or archived —
// the one definition in internal/notification) and the subject is actually on
// it through a marker that is not flagged invalid.
const announceableSQL = `
SELECT EXISTS (
    SELECT 1 FROM photos p
    WHERE p.uid = $1 AND ` + notification.VisiblePhotoSQL + `
      AND EXISTS (SELECT 1 FROM markers m
                  WHERE m.photo_uid = p.uid AND m.subject_uid = $2 AND NOT m.invalid))`

// insertNoticeSQL records one pending notice; the same photo twice is one notice.
const insertNoticeSQL = `
INSERT INTO tag_notices (user_uid, photo_uid) VALUES ($1, $2)
ON CONFLICT (user_uid, photo_uid) DO NOTHING`

// queuedSQL reports whether the account already has a window waiting to close:
// a queued tag_notify job. The partial dedup index of migration 0089 serves it.
const queuedSQL = `
SELECT EXISTS (SELECT 1 FROM jobs
               WHERE type = 'tag_notify' AND state = 'queued' AND payload ->> 'user_uid' = $1)`

// Tagged records that t.SubjectUID was tagged on t.PhotoUID for every account
// linked to that subject, on tx, and opens each account's window when it has
// none. It records nothing at all when push is off, when the photo is private,
// hidden or archived (a notification would disclose that it exists), or when
// the acting account is itself linked to the subject — nobody is told about
// their own work, and "at all" includes a second account of the same person. An
// account that turned the tagged kind off gets no notice, so the table does not
// fill up for somebody who does not want the message; a subject linked to no
// account produces nothing.
func (r *Recorder) Tagged(ctx context.Context, tx pgx.Tx, t people.Tagging) error {
	if !r.enabled {
		return nil
	}
	return r.underSavepoint(ctx, tx, "recording a tag notice", t, func(sp pgx.Tx) error {
		return r.record(ctx, sp, t)
	})
}

// record is Tagged's work on the savepoint sp.
func (r *Recorder) record(ctx context.Context, sp pgx.Tx, t people.Tagging) error {
	recipients, err := r.recipients(ctx, sp, t.SubjectUID)
	if err != nil || len(recipients) == 0 {
		return err
	}
	for _, uid := range recipients {
		if t.ActorUID != "" && uid == t.ActorUID {
			return nil
		}
	}
	var announceable bool
	if err := sp.QueryRow(ctx, announceableSQL, t.PhotoUID, t.SubjectUID).Scan(&announceable); err != nil {
		return fmt.Errorf("reading whether %s may be announced: %w", t.PhotoUID, err)
	}
	if !announceable {
		return nil
	}
	for _, uid := range recipients {
		if err := r.recordFor(ctx, sp, uid, t.PhotoUID); err != nil {
			return err
		}
	}
	return nil
}

// recordFor records photoUID for the account userUID, if it wants the kind, and
// opens its window when this notice is the first of one.
func (r *Recorder) recordFor(ctx context.Context, sp pgx.Tx, userUID, photoUID string) error {
	wants, err := r.prefs.WantsTx(ctx, sp, userUID, notification.KindTagged)
	if err != nil {
		return fmt.Errorf("reading the preference of %s: %w", userUID, err)
	}
	if !wants {
		return nil
	}
	if _, err := sp.Exec(ctx, lockSQL, userUID); err != nil {
		return fmt.Errorf("locking the window of %s: %w", userUID, err)
	}
	tag, err := sp.Exec(ctx, insertNoticeSQL, userUID, photoUID)
	if err != nil {
		return fmt.Errorf("recording the notice of %s on %s: %w", userUID, photoUID, err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return r.open(ctx, sp, userUID)
}

// open enqueues the job that closes userUID's window, one window from now,
// unless one is already queued.
//
// "The first notice of a window" is decided by the job, not by counting
// notices: the window is open exactly while a tag_notify job for the account is
// queued. The two readings agree while things go well, and this one also
// recovers when they do not — a job that was dead-lettered leaves its notices
// behind, and a count would then never see "first" again. The check runs under
// the account's advisory lock, which the job also takes before it consumes the
// notices; so a tag either lands before the job reads (and is in its
// notification) or after it committed (and finds no queued job, opening a new
// window). That lock is also why a whole cluster assigned in one transaction
// queues one job: every notice after the first sees the job the first one
// queued.
//
// A tag landing while the job is *running* therefore opens a fresh window —
// the dedup index only covers queued jobs. That is correct, not a bug: the
// running job has already read its notices, and swallowing the new tag as a
// duplicate would lose it.
func (r *Recorder) open(ctx context.Context, sp pgx.Tx, userUID string) error {
	var queued bool
	if err := sp.QueryRow(ctx, queuedSQL, userUID).Scan(&queued); err != nil {
		return fmt.Errorf("reading the window of %s: %w", userUID, err)
	}
	if queued {
		return nil
	}
	raw, err := encodePayload(userUID)
	if err != nil {
		return err
	}
	runAfter := r.now().Add(r.window)
	_, err = jobs.EnqueueOrSkip(ctx, sp, jobs.TypeTagNotify, raw, jobs.EnqueueOptions{RunAfter: &runAfter})
	if err != nil && !errors.Is(err, jobs.ErrDuplicate) {
		return fmt.Errorf("opening the window of %s: %w", userUID, err)
	}
	return nil
}

// deleteNoticesSQL un-announces a photo for every account linked to the
// subject — unless the subject is still on the photo through another valid
// marker, in which case it was not really taken off.
const deleteNoticesSQL = `
DELETE FROM tag_notices n
USING users u
WHERE u.subject_uid = $1 AND n.user_uid = u.uid AND n.photo_uid = $2
  AND NOT EXISTS (SELECT 1 FROM markers m
                  WHERE m.photo_uid = $2 AND m.subject_uid = $1 AND NOT m.invalid)`

// Untagged un-announces t.PhotoUID for every account linked to t.SubjectUID, on
// tx, so a notification never names a photo the person is no longer on. It runs
// with push off too (see RecorderConfig.Enabled), and it leaves a window it
// empties open: the job then finds nothing and sends nothing.
func (r *Recorder) Untagged(ctx context.Context, tx pgx.Tx, t people.Tagging) error {
	return r.underSavepoint(ctx, tx, "clearing a tag notice", t, func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, deleteNoticesSQL, t.SubjectUID, t.PhotoUID); err != nil {
			return fmt.Errorf("deleting the notices of %s on %s: %w", t.SubjectUID, t.PhotoUID, err)
		}
		return nil
	})
}

// recipients returns the accounts linked to subjectUID, read on q.
func (r *Recorder) recipients(ctx context.Context, q pgx.Tx, subjectUID string) ([]string, error) {
	rows, err := q.Query(ctx, recipientsSQL, subjectUID)
	if err != nil {
		return nil, fmt.Errorf("reading the accounts of %s: %w", subjectUID, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("reading the accounts of %s: %w", subjectUID, err)
	}
	return out, nil
}

// underSavepoint runs fn on a savepoint of tx and releases it. When fn fails,
// the savepoint is rolled back — undoing whatever fn wrote, which a failed
// statement would otherwise have left aborting the whole transaction — the
// failure is logged, and nil is returned so the assignment goes on.
func (r *Recorder) underSavepoint(
	ctx context.Context, tx pgx.Tx, what string, t people.Tagging, fn func(pgx.Tx) error,
) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("tagnotifyjob: opening a savepoint: %w", err)
	}
	if err := fn(sp); err != nil {
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("tagnotifyjob: rolling back to the savepoint: %w", rbErr)
		}
		r.log.WarnContext(ctx, "tagnotifyjob: "+what+" failed; the assignment goes on without it",
			slog.String("photo_uid", t.PhotoUID), slog.String("subject_uid", t.SubjectUID),
			slog.String("error", err.Error()))
		return nil
	}
	if err := sp.Commit(ctx); err != nil {
		return fmt.Errorf("tagnotifyjob: releasing the savepoint: %w", err)
	}
	return nil
}
