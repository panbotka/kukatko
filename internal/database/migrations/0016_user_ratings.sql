-- 0016_user_ratings: per-user star ratings and pick/reject flags for photos.
--
-- Mirrors the per-user user_favorites design: a rating/flag is a property of the
-- (user, photo) pair, not a single global value on the photo, so two users can
-- rate the same photo differently. A row exists only when a user has set a
-- non-default value; a missing row reads back as rating 0 / flag 'none', so the
-- table stays sparse (the store deletes a row that falls back to all-defaults).
--
-- Foreign keys cascade on photo and user deletion, so removing either side drops
-- the rating rows rather than leaving orphans. This migration is wrapped in a
-- transaction by the runner.

CREATE TABLE user_ratings (
    user_uid   VARCHAR(32)  NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    photo_uid  VARCHAR(32)  NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    -- Star rating from 0 (unrated) to 5; 0 is the default, never persisted alone.
    rating     SMALLINT     NOT NULL DEFAULT 0 CHECK (rating BETWEEN 0 AND 5),
    -- Pick/reject cull marker; 'none' is the default, never persisted alone.
    flag       TEXT         NOT NULL DEFAULT 'none'
                   CHECK (flag IN ('none', 'pick', 'reject')),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (user_uid, photo_uid)
);

-- Listing/aggregating who rated a photo (the reverse of a user's ratings) is a
-- hot lookup; the composite primary key already covers user-then-photo access.
CREATE INDEX idx_user_ratings_photo_uid ON user_ratings (photo_uid);
