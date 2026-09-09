package hlsjob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrRenditionNotFound is returned by Get when a photo has no row for the
// requested rendition — which is what "this video has not been transcoded (into
// this quality) yet" looks like, and is not an error condition in itself.
var ErrRenditionNotFound = errors.New("hlsjob: rendition not found")

// Encoded is one photo_hls_renditions row: an HLS rendition of one video as it
// was published, together with everything a player's playlists must advertise
// about it.
//
// Playlist is the media playlist ffmpeg wrote, verbatim, with the bare object
// names it wrote in it. It is never served as it stands — the .m3u8 a player
// fetches is rendered from this text with the URIs rewritten to wherever the
// objects are actually fetched from — but it is the one part of the encode that
// cannot be recomputed without re-running ffmpeg, because ffmpeg measured each
// segment's real duration as it wrote it.
type Encoded struct {
	// PhotoUID identifies the video this rendition belongs to.
	PhotoUID string `json:"photo_uid"`
	// Rendition is the rendition's name, verbatim as it appears in object keys.
	Rendition string `json:"rendition"`
	// Playlist is the media playlist ffmpeg wrote.
	Playlist string `json:"playlist"`
	// Width and Height are the encoded picture's real dimensions in pixels, read
	// off what ffmpeg produced rather than predicted from the source.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Bandwidth is the rendition's peak bandwidth in bits per second.
	Bandwidth int `json:"bandwidth"`
	// Codecs is the RFC 6381 codecs string of the rendition's streams.
	Codecs string `json:"codecs"`
	// SegmentCount is how many media segments were published, init.mp4 excluded.
	SegmentCount int `json:"segment_count"`
	// DurationMs is the rendition's total length in milliseconds.
	DurationMs int `json:"duration_ms"`
	// EncodedAt is when this rendition was last encoded.
	EncodedAt time.Time `json:"encoded_at"`
}

// Store reads and writes photo_hls_renditions over a shared pgx pool. It owns no
// connection; the pool stays owned by the caller.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// renditionColumns is the column list every read of a rendition row selects, in
// the order scanEncoded expects.
const renditionColumns = `photo_uid, rendition, playlist, width, height,
	bandwidth, codecs, segment_count, duration_ms, encoded_at`

// saveSQL upserts one rendition on its (photo_uid, rendition) primary key, which
// is what makes re-encoding a video replace its rendition rather than duplicate
// it. encoded_at moves forward on every write, so "when was this last encoded"
// stays answerable.
const saveSQL = `
INSERT INTO photo_hls_renditions (
	photo_uid, rendition, playlist, width, height,
	bandwidth, codecs, segment_count, duration_ms, encoded_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (photo_uid, rendition) DO UPDATE SET
	playlist      = EXCLUDED.playlist,
	width         = EXCLUDED.width,
	height        = EXCLUDED.height,
	bandwidth     = EXCLUDED.bandwidth,
	codecs        = EXCLUDED.codecs,
	segment_count = EXCLUDED.segment_count,
	duration_ms   = EXCLUDED.duration_ms,
	encoded_at    = now()
RETURNING ` + renditionColumns

// Save writes enc, replacing any rendition of the same name already recorded for
// the same photo, and returns the stored row with its encoded_at stamped by the
// database. A row that violates the table's CHECKs (an empty playlist, a
// zero-pixel picture, no segments) is rejected rather than stored: those are the
// shapes no master playlist may advertise.
func (s *Store) Save(ctx context.Context, enc Encoded) (Encoded, error) {
	row := s.pool.QueryRow(ctx, saveSQL,
		enc.PhotoUID, enc.Rendition, enc.Playlist, enc.Width, enc.Height,
		enc.Bandwidth, enc.Codecs, enc.SegmentCount, enc.DurationMs)
	saved, err := scanEncoded(row)
	if err != nil {
		return Encoded{}, fmt.Errorf("hlsjob: saving rendition %s of %s: %w",
			enc.Rendition, enc.PhotoUID, err)
	}
	return saved, nil
}

// getSQL fetches one photo's row for one rendition.
const getSQL = `SELECT ` + renditionColumns + `
FROM photo_hls_renditions WHERE photo_uid = $1 AND rendition = $2`

// Get returns the recorded rendition of the given name for photoUID, or
// ErrRenditionNotFound when the video has not been encoded into it.
func (s *Store) Get(ctx context.Context, photoUID, rendition string) (Encoded, error) {
	enc, err := scanEncoded(s.pool.QueryRow(ctx, getSQL, photoUID, rendition))
	if errors.Is(err, pgx.ErrNoRows) {
		return Encoded{}, fmt.Errorf("%w: %s/%s", ErrRenditionNotFound, photoUID, rendition)
	}
	if err != nil {
		return Encoded{}, fmt.Errorf("hlsjob: reading rendition %s of %s: %w", rendition, photoUID, err)
	}
	return enc, nil
}

// listSQL fetches every rendition recorded for one photo.
const listSQL = `SELECT ` + renditionColumns + `
FROM photo_hls_renditions WHERE photo_uid = $1 ORDER BY width DESC, rendition`

// ListForPhoto returns every rendition recorded for photoUID, widest picture
// first — the order a master playlist wants, since a player takes the first
// variant it can play. A video with no renditions yields an empty (non-nil)
// slice, not an error: not yet encoded is a state, not a failure.
func (s *Store) ListForPhoto(ctx context.Context, photoUID string) ([]Encoded, error) {
	rows, err := s.pool.Query(ctx, listSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("hlsjob: listing renditions of %s: %w", photoUID, err)
	}
	defer rows.Close()

	out := []Encoded{}
	for rows.Next() {
		enc, scanErr := scanEncoded(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("hlsjob: scanning renditions of %s: %w", photoUID, scanErr)
		}
		out = append(out, enc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hlsjob: iterating renditions of %s: %w", photoUID, err)
	}
	return out, nil
}

// scanner is the one method pgx.Row and pgx.Rows share, so a row scan is written
// once for both the single-row and the listing path.
type scanner interface {
	// Scan copies the current row's columns into dest.
	Scan(dest ...any) error
}

// scanEncoded reads one photo_hls_renditions row in renditionColumns order.
func scanEncoded(row scanner) (Encoded, error) {
	var enc Encoded
	if err := row.Scan(&enc.PhotoUID, &enc.Rendition, &enc.Playlist, &enc.Width, &enc.Height,
		&enc.Bandwidth, &enc.Codecs, &enc.SegmentCount, &enc.DurationMs, &enc.EncodedAt); err != nil {
		return Encoded{}, err //nolint:wrapcheck // callers name the operation; pgx.ErrNoRows must stay matchable.
	}
	return enc, nil
}

// hasSQL asks whether one photo has a row for one rendition, reading no column
// of it. It is the check the segment route runs on every request a player makes,
// which is why it deliberately does not go through Get: the playlist column of a
// two-hour video is tens of kilobytes, and fetching it a thousand times to
// answer "does this rendition exist" would be the most expensive part of
// streaming the clip.
const hasSQL = `SELECT EXISTS (
	SELECT 1 FROM photo_hls_renditions WHERE photo_uid = $1 AND rendition = $2)`

// Has reports whether photoUID has been encoded into the named rendition. It is
// the existence half of Get, for the callers that only need to know whether the
// objects behind a key may be served.
func (s *Store) Has(ctx context.Context, photoUID, rendition string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, hasSQL, photoUID, rendition).Scan(&exists); err != nil {
		return false, fmt.Errorf("hlsjob: checking rendition %s of %s: %w", rendition, photoUID, err)
	}
	return exists, nil
}

// hasAnySQL asks whether one photo has any rendition at all.
const hasAnySQL = `SELECT EXISTS (SELECT 1 FROM photo_hls_renditions WHERE photo_uid = $1)`

// HasAny reports whether photoUID has been encoded into at least one rendition —
// which is what "this video can be streamed" means, since the master playlist
// advertises whatever was produced. It backs the flag on the photo payload, so
// it reads no row: a payload does not need the playlists, and a still image must
// not pay for them.
func (s *Store) HasAny(ctx context.Context, photoUID string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, hasAnySQL, photoUID).Scan(&exists); err != nil {
		return false, fmt.Errorf("hlsjob: checking renditions of %s: %w", photoUID, err)
	}
	return exists, nil
}
