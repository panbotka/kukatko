-- 0059_review_skips: the review game remembers "I don't know".
--
-- Answering "don't know" in the review game used to live in the per-user
-- in-memory session and nowhere else, so a restart — or twelve idle hours —
-- forgot it and the game asked about the same unrecognisable face again. That is
-- the one answer the game must remember, because unlike yes and no it produces
-- no durable trace anywhere else in the app.
--
-- A row is one player's "I could not recognise <subject> on <photo>". Three of
-- them about one person mute that person for that player for a while; the
-- threshold and the cooling-off period are config keys (review.skip_mute_*), and
-- everything the mute needs is derived from these rows, so there is no second
-- state table to keep in step:
--
--   * how many skips a person has collected from a player is the row count;
--   * when the mute began is the newest of those rows' skipped_at, which is why
--     a photo imported afterwards is still asked about — a new face of that
--     person is exactly what might be recognisable;
--   * how long the pause lasts grows with every skip past the threshold, so a
--     re-skip after the cooling-off period mutes for longer rather than for the
--     same short wait.
--
-- A skip is emphatically NOT a rejection. Nothing here reaches the catalogue: it
-- does not exclude the person from the candidate search, the recognition sweep
-- or any face-assignment path, and it changes no identity. It says "don't ask
-- *me* this", never "this is not that person" — conflating the two would poison
-- recognition for everybody. That is also why the table is keyed by user and why
-- no reader ever crosses user_uid: one player's "I don't know" must never quiet
-- the game for anybody else.
--
-- It is game state rather than a curation act, so it is deliberately absent from
-- the audit trail: the audit records what happened to the library, and nothing
-- here happens to the library.
--
-- The primary key makes recording idempotent — skipping the same face twice is
-- one unresolved photo, not two — and the CASCADEs mean deleting an account, a
-- person or a photo takes the matching skips with it. This migration is wrapped
-- in a transaction by the runner.

CREATE TABLE review_skips (
    user_uid    VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    subject_uid VARCHAR(32) NOT NULL REFERENCES subjects (uid) ON DELETE CASCADE,
    photo_uid   VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    skipped_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_uid, subject_uid, photo_uid)
);

-- The only read path: everything one player has skipped, grouped by person. The
-- primary key already leads with user_uid, so this index exists for the
-- skipped_at the mute window is derived from — it lets the aggregate read the
-- newest skip per subject without visiting the heap.
CREATE INDEX idx_review_skips_user_subject
    ON review_skips (user_uid, subject_uid, skipped_at DESC);
