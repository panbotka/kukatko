-- Persisted "these two are genuinely different" decisions for duplicate review.
--
-- Duplicate detection is derived state: it re-runs from scratch on every call to
-- GET /duplicates, from perceptual hashes and embeddings that do not change when
-- a user disagrees with them. So a user telling Kukátko "this pair is not a
-- duplicate" had nowhere to live and evaporated on reload, and the very same pair
-- came back on the next scan, forever. That is the same gap migrations 0031/0032
-- closed for faces and labels; this closes it for duplicate pairs.
--
-- The row records an OPINION about an unordered PAIR, not about a group. Groups
-- are connected components and are not stable: adding one photo can merge two
-- groups, so a group-level dismissal would be meaningless the moment the library
-- changes. A pair is the edge the detector actually drew, so suppressing the edge
-- is what "not a duplicate" means. A three-member group with one edge dismissed
-- stays a group when the remaining edges still connect it — which is correct, and
-- why this is not simply a "hide this group" flag.
--
-- The pair is unordered, so it is stored canonically with the lexicographically
-- smaller uid in photo_uid. The CHECK enforces that at the database level (the
-- store normalises before writing), which is what makes the UNIQUE constraint
-- actually mean "one row per pair" rather than "one row per direction" — without
-- it, (A,B) and (B,A) would both insert and each dismissal could be recorded
-- twice.
--
-- Both uids CASCADE to photos: a purged photo cannot be half of a duplicate pair,
-- so the opinion about it is meaningless and goes with it. dismissed_by is SET
-- NULL rather than CASCADE — the decision outlives the account that made it, the
-- same way rejected_by does in 0031.

CREATE TABLE duplicate_dismissals (
    id           BIGSERIAL   PRIMARY KEY,
    photo_uid    VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    other_uid    VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    dismissed_by VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    dismissed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT duplicate_dismissals_ordered CHECK (photo_uid < other_uid),
    UNIQUE (photo_uid, other_uid)
);

-- The scan reads every dismissal at once (one bulk lookup per GET /duplicates,
-- never an N+1), so the UNIQUE index above already serves it. This second index
-- covers the reverse direction, for looking up what a single photo was dismissed
-- against without scanning the table.
CREATE INDEX idx_duplicate_dismissals_other ON duplicate_dismissals (other_uid);
