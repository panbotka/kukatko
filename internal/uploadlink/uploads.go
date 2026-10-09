package uploadlink

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/organize"
)

// photoTargetType is the audit target type of a recorded upload: the photo.
const photoTargetType = "photos"

// Outcome values stored in upload_link_photos.outcome.
const (
	outcomeCreated   = "created"
	outcomeDuplicate = "duplicate"
)

// insertUploadSQL records one file's provenance. A photo uploaded twice through
// the same link keeps its first row — the first uploader is its provenance.
const insertUploadSQL = `
INSERT INTO upload_link_photos (link_uid, photo_uid, outcome, uploader_name, uploaded_by, session_hash)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
ON CONFLICT (link_uid, photo_uid) DO NOTHING`

// reserveUploadSQL takes one slot of a link's lifetime file cap: it bumps the
// counter only while the link is neither revoked nor expired at $2 and, unless $3
// is 0, still under the cap $3. One statement, so concurrent requests serialise
// on the row and can never take more slots than the cap holds.
const reserveUploadSQL = `
UPDATE upload_links SET upload_count = upload_count + 1
WHERE uid = $1 AND revoked_at IS NULL AND expires_at > $2 AND ($3 = 0 OR upload_count < $3)
RETURNING upload_count`

// reservationRefusalSQL reads what a refused reservation found: whether the
// link is revoked and when it expires.
const reservationRefusalSQL = `SELECT revoked_at IS NOT NULL, expires_at FROM upload_links WHERE uid = $1`

// releaseUploadSQL hands a reserved slot back.
const releaseUploadSQL = `UPDATE upload_links SET upload_count = upload_count - 1 WHERE uid = $1 AND upload_count > 0`

// stampUploadSQL marks a link used and returns its title, which the upload's
// audit entry carries.
const stampUploadSQL = `
UPDATE upload_links SET last_used_at = now()
WHERE uid = $1
RETURNING title`

// ReserveUpload takes one slot of the link uid's lifetime file cap for a file
// about to be ingested, atomically with checking the link is live at now: the
// counter moves only while the link is not revoked, not expired and — unless
// maxUploads is 0 — under maxUploads. It is the point a file is accepted: a link
// revoked or filled after it does not take the slot back. A refusal returns
// ErrFull, ErrRevoked, ErrExpired or ErrNotFound. A file that then fails, or is
// not recorded, gives the slot back with ReleaseUpload.
func (s *Store) ReserveUpload(ctx context.Context, uid string, maxUploads int, now time.Time) error {
	var count int
	err := s.pool.QueryRow(ctx, reserveUploadSQL, uid, now, max(maxUploads, 0)).Scan(&count)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("uploadlink: reserving upload: %w", err)
	}
	// The refusal was decided atomically above; this read only names it.
	var revoked bool
	var expiresAt time.Time
	err = s.pool.QueryRow(ctx, reservationRefusalSQL, uid).Scan(&revoked, &expiresAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("uploadlink: reading refused reservation: %w", err)
	case revoked:
		return ErrRevoked
	case !now.Before(expiresAt):
		return ErrExpired
	default:
		return ErrFull
	}
}

// ReleaseUpload gives back a slot ReserveUpload took for a file that did not end
// up recorded — refused by the pipeline, or not filed. Releasing a link that is
// gone, or whose counter is already 0, changes nothing.
func (s *Store) ReleaseUpload(ctx context.Context, uid string) error {
	if _, err := s.pool.Exec(ctx, releaseUploadSQL, uid); err != nil {
		return fmt.Errorf("uploadlink: releasing upload: %w", err)
	}
	return nil
}

// RecordUpload records up — one file that came in through a link, whose slot
// ReserveUpload already took — and files its photo into every album and label
// of the link, so it is visible there at once. The provenance row, the
// memberships, the link's last use and entry (stamped with the photo as its
// target and the link, its title, the typed name and the outcome in its details)
// commit together. It does not count the file: the reservation did. It returns
// ErrNotFound when the link is gone.
func (s *Store) RecordUpload(ctx context.Context, up Upload, entry audit.Entry) error {
	outcome := outcomeDuplicate
	if up.Created {
		outcome = outcomeCreated
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		var title string
		err := tx.QueryRow(ctx, stampUploadSQL, up.LinkUID).Scan(&title)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("uploadlink: stamping upload: %w", err)
		}
		if _, err := tx.Exec(ctx, insertUploadSQL, up.LinkUID, up.PhotoUID, outcome,
			up.UploaderName, up.UploadedBy, up.SessionHash); err != nil {
			return fmt.Errorf("uploadlink: recording upload: %w", err)
		}
		if err := fileIntoTargets(ctx, tx, up.LinkUID, up.PhotoUID); err != nil {
			return err
		}
		entry.TargetType, entry.TargetUID = photoTargetType, up.PhotoUID
		// The title rides along so the audit list can name an anonymous upload by
		// its link without a second lookup, and keeps naming it after the link is
		// renamed or deleted.
		entry.Details = withDetails(entry.Details, map[string]any{
			"link_uid": up.LinkUID, "link_title": title, "uploader_name": up.UploaderName, "outcome": outcome,
		})
		return audit.Write(ctx, tx, entry)
	})
}

// fileIntoTargets adds photoUID to every album of the link and attaches every
// label of it, on tx.
func fileIntoTargets(ctx context.Context, tx pgx.Tx, linkUID, photoUID string) error {
	albums, err := targetUIDs(ctx, tx, "SELECT album_uid FROM upload_link_albums WHERE link_uid = $1", linkUID)
	if err != nil {
		return err
	}
	labels, err := targetUIDs(ctx, tx, "SELECT label_uid FROM upload_link_labels WHERE link_uid = $1", linkUID)
	if err != nil {
		return err
	}
	for _, albumUID := range albums {
		if err := organize.AddPhotoTx(ctx, tx, albumUID, photoUID); err != nil {
			return fmt.Errorf("uploadlink: filing into album %s: %w", albumUID, err)
		}
	}
	for _, labelUID := range labels {
		if err := organize.AttachLabelTx(ctx, tx, photoUID, labelUID); err != nil {
			return fmt.Errorf("uploadlink: attaching label %s: %w", labelUID, err)
		}
	}
	return nil
}

// targetUIDs runs query (one UID column, the link UID as $1) on tx and returns
// the UIDs.
func targetUIDs(ctx context.Context, tx pgx.Tx, query, linkUID string) ([]string, error) {
	rows, err := tx.Query(ctx, query, linkUID)
	if err != nil {
		return nil, fmt.Errorf("uploadlink: reading targets: %w", err)
	}
	uids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("uploadlink: reading targets: %w", err)
	}
	return uids, nil
}

// attributeSQL hands the photos an anonymous session created to the account $2:
// the provenance rows first, then the photos themselves — only those still
// owned by nobody, so a photo is never taken from anybody and a session can be
// claimed once.
const attributeSQL = `
WITH claimed AS (
    UPDATE upload_link_photos SET uploaded_by = $2
    WHERE session_hash = $1 AND outcome = 'created' AND uploaded_by IS NULL
    RETURNING photo_uid
)
UPDATE photos SET uploaded_by = $2, updated_at = now()
WHERE uid IN (SELECT photo_uid FROM claimed) AND uploaded_by IS NULL
RETURNING uid`

// AttributeSessionTx attributes to userUID, on tx, every photo the anonymous
// session whose token hashes to sessionHash created through any link, and
// returns the UIDs of the photos it changed (whose sidecars are now stale). It
// runs in the transaction that creates the account, so the photos are claimed if
// and only if the account exists. An empty sessionHash claims nothing.
func AttributeSessionTx(ctx context.Context, tx pgx.Tx, sessionHash, userUID string) ([]string, error) {
	if sessionHash == "" {
		return nil, nil
	}
	rows, err := tx.Query(ctx, attributeSQL, sessionHash, userUID)
	if err != nil {
		return nil, fmt.Errorf("uploadlink: attributing session photos: %w", err)
	}
	uids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("uploadlink: attributing session photos: %w", err)
	}
	return uids, nil
}

// provenanceSQL reads the earliest link upload that created the photo, with the
// account it is attributed to (the signed-in uploader, or the one that claimed
// the anonymous session at registration) when that account still exists.
const provenanceSQL = `
SELECT l.uid, l.title, p.uploader_name, p.created_at, u.uid,
       COALESCE(NULLIF(u.display_name, ''), u.username, '')
FROM upload_link_photos p
JOIN upload_links l ON l.uid = p.link_uid
LEFT JOIN users u ON u.uid = p.uploaded_by
WHERE p.photo_uid = $1 AND p.outcome = 'created'
ORDER BY p.created_at, l.uid
LIMIT 1`

// Provenance returns where photoUID came from when an upload link created it,
// or nil when it did not (a photo merely filed into a link's targets as a
// duplicate came from elsewhere). A photo created through one link and later
// re-filed by others as a duplicate reports the link that created it.
func (s *Store) Provenance(ctx context.Context, photoUID string) (*Provenance, error) {
	var p Provenance
	err := s.pool.QueryRow(ctx, provenanceSQL, photoUID).Scan(&p.LinkUID, &p.LinkTitle, &p.UploaderName, &p.UploadedAt,
		&p.AccountUID, &p.AccountName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // "no provenance" is a normal answer, not an error.
	}
	if err != nil {
		return nil, fmt.Errorf("uploadlink: reading provenance: %w", err)
	}
	return &p, nil
}
