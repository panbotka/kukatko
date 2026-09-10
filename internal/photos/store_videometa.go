package photos

import (
	"context"
	"fmt"
	"time"
)

// VideoRepair is what a fresh probe of a video's container is allowed to write
// back into the catalogue. It is a decision, not an extraction: every field is
// optional, and nil means "leave this column exactly as it is". The caller
// (internal/metajob) compares a probe against the row it holds and fills in only
// the columns it concluded may be written, so this type carries no policy of its
// own — it is the applied half of that decision.
//
// The split matters and is deliberate. The first group is file-derived: the
// container is the only authority on how long a clip is, how large its frames
// are, how fast they run and what encoded them, so a probe may correct a wrong
// value there, not merely fill an empty one. The second group is curated: a
// capture time and a location can have been typed, corrected or deliberately
// removed by a person, and the catalogue records that provenance — so those two
// are only ever offered for a gap, and the caller is the one that decides whether
// the gap exists.
type VideoRepair struct {
	// DurationMs is the clip length in milliseconds (file-derived).
	DurationMs *int
	// Width and Height are the video's pixel dimensions (file-derived). They are
	// set together or not at all: half a frame size is not a frame size.
	Width  *int
	Height *int
	// FPS is the average frame rate (file-derived).
	FPS *float64
	// VideoCodec names the primary video stream's codec (file-derived).
	VideoCodec *string
	// AudioCodec names the primary audio stream's codec and HasAudio says whether
	// there is one at all (file-derived). They are written together: a clip that
	// turns out to have no sound must not keep the codec name of a stream it does
	// not have.
	AudioCodec *string
	HasAudio   *bool

	// TakenAt is the container's creation time and TakenAtSource its provenance,
	// written only together (curated: offered for a gap).
	TakenAt       *time.Time
	TakenAtSource string
	// Lat and Lng are the container's coordinates and LocationSource their
	// provenance, all written only together and only as a pair (curated: offered
	// for a gap).
	Lat            *float64
	Lng            *float64
	LocationSource string
	// Altitude is metres above sea level (curated: offered for a gap).
	Altitude *float64
}

// Empty reports whether the repair would write nothing, which is the answer for
// a video whose technical metadata is already what its file says. The caller
// checks it and skips the write entirely, so a re-probe of a complete clip leaves
// the row — down to updated_at — untouched.
func (r VideoRepair) Empty() bool {
	return !r.hasFileDerived() && !r.hasCurated()
}

// hasFileDerived reports whether the repair carries any of the fields only a probe
// of the container ever writes.
func (r VideoRepair) hasFileDerived() bool {
	return r.DurationMs != nil || r.Width != nil || r.Height != nil || r.FPS != nil ||
		r.VideoCodec != nil || r.AudioCodec != nil || r.HasAudio != nil
}

// hasCurated reports whether the repair carries any of the fields a person can own,
// which are only ever offered for a gap.
func (r VideoRepair) hasCurated() bool {
	return r.TakenAt != nil || r.Lat != nil || r.Lng != nil || r.Altitude != nil
}

// values returns r's fields as the query arguments $2…$14 of
// repairVideoMetadataSQL, in that order.
func (r VideoRepair) values() []any {
	return []any{
		r.DurationMs, r.Width, r.Height, r.FPS, r.VideoCodec, r.AudioCodec, r.HasAudio,
		r.TakenAt, r.TakenAtSource, r.Lat, r.Lng, r.LocationSource, r.Altitude,
	}
}

// repairVideoMetadataSQL writes the columns a video re-probe decided on and
// leaves every other column alone.
//
// Every assignment is a COALESCE over its own parameter, so a NULL argument means
// "keep what is there": the statement applies a decision the caller has already
// made rather than making one of its own. The two provenance columns are the
// exception — taken_at_source and location_source describe the value beside them,
// so they are written if and only if that value is.
//
// The media_type guard is not decoration: this statement speaks about a container
// (duration, codecs, frame rate), and pointing it at a still would be a bug in
// the caller worth failing on rather than a row worth writing.
var repairVideoMetadataSQL = `
UPDATE photos SET
	duration_ms = COALESCE($2::int, duration_ms),
	file_width = COALESCE($3::int, file_width),
	file_height = COALESCE($4::int, file_height),
	fps = COALESCE($5::double precision, fps),
	video_codec = COALESCE($6::text, video_codec),
	audio_codec = COALESCE($7::text, audio_codec),
	has_audio = COALESCE($8::boolean, has_audio),
	taken_at = COALESCE($9::timestamptz, taken_at),
	taken_at_source = CASE WHEN $9::timestamptz IS NOT NULL THEN $10::text ELSE taken_at_source END,
	` + TakenAtBeforeUnknownAssignment("$9") + `,
	lat = COALESCE($11::double precision, lat),
	lng = COALESCE($12::double precision, lng),
	location_source = CASE WHEN $11::double precision IS NOT NULL AND $12::double precision IS NOT NULL
		THEN $13::text ELSE location_source END,
	altitude = COALESCE($14::double precision, altitude),
	updated_at = now()
WHERE uid = $1 AND media_type = '` + string(MediaVideo) + `'`

// RepairVideoMetadata applies a re-probe's decision to one video and reports
// whether a row was written. An empty repair writes nothing and returns false
// without touching the database, so re-probing a complete clip is a genuine
// no-op.
//
// It returns ErrPhotoNotFound when no such photo exists — or when it is not a
// video, which for this call is the same mistake: there is no container to have
// been probed.
func (s *Store) RepairVideoMetadata(ctx context.Context, uid string, r VideoRepair) (bool, error) {
	if r.Empty() {
		return false, nil
	}
	args := append([]any{uid}, r.values()...)
	tag, err := s.pool.Exec(ctx, repairVideoMetadataSQL, args...)
	if err != nil {
		return false, fmt.Errorf("photos: repairing video metadata of %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return false, ErrPhotoNotFound
	}
	return true, nil
}

// listVideosMissingTechnicalSQL selects the uids of non-archived videos whose
// container has evidently never been read out: no duration, no codec name, or no
// frame size. The trailing %s receives an optional LIMIT clause.
//
// The predicate is what a *failed* probe leaves behind, not everything a probe
// could have found. A successful probe of a real clip always yields all three, so
// missing any of them means the reading never happened — which makes the backfill
// converge: once a clip has been repaired it drops out of the set for good.
// Frame rate and audio are deliberately not in it, because a container can
// legitimately have neither, and a photo that can never satisfy the predicate
// would be re-scheduled by every run forever.
const listVideosMissingTechnicalSQL = `
SELECT uid
FROM photos
WHERE archived_at IS NULL AND media_type = 'video'
  AND (duration_ms IS NULL OR duration_ms <= 0 OR video_codec = '' OR file_width = 0 OR file_height = 0)
ORDER BY created_at DESC, uid DESC%s`

// ListVideosMissingTechnicalMetadata returns the uids of non-archived videos
// whose technical metadata is missing — the clips whose probe failed at upload,
// or that were catalogued before there was one — newest first. A positive limit
// caps the result; a non-positive limit returns every one of them. It backs the
// video half of the metadata backfill, which enqueues a `metadata` job per
// returned uid.
func (s *Store) ListVideosMissingTechnicalMetadata(ctx context.Context, limit int) ([]string, error) {
	query := fmt.Sprintf(listVideosMissingTechnicalSQL, "")
	args := []any(nil)
	if limit > 0 {
		query = fmt.Sprintf(listVideosMissingTechnicalSQL, "\nLIMIT $1")
		args = []any{limit}
	}
	return s.queryUIDs(ctx, "listing videos missing technical metadata", query, args...)
}
