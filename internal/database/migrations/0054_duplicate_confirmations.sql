-- 0054_duplicate_confirmations: persisted "yes, this really is the same photo
-- twice" decisions about a duplicate pair.
--
-- Migration 0034 gave the negative answer a home: a pair a user settled as
-- genuinely different stops being linked on later scans. The positive answer had
-- nowhere to go at all, because the duplicates page only ever offered one way to
-- agree — merging, which archives copies and is not something a curator wants to
-- do one pair at a time from a review game. So agreement evaporated, and a group
-- somebody had already looked at was indistinguishable from one nobody had.
--
-- This table records that agreement. It changes nothing about detection: a
-- confirmed pair is still detected exactly as before, still shown, still
-- mergeable. What it buys is ranking — a group with a human "yes" on it sorts to
-- the top of the duplicates page, because it is the one where merging is a
-- decision already made rather than one still to be judged. Merging stays an
-- explicit, separate act; the review game NEVER merges.
--
-- The shape mirrors duplicate_dismissals deliberately, down to the ordered-pair
-- CHECK: the opinion is about an unordered PAIR (the edge the detector drew),
-- not about a group, because groups are connected components and are not stable
-- — adding one photo can merge two groups, so a group-level opinion would be
-- meaningless the moment the library changes.
--
-- The CHECK carries COLLATE "C" from the start (unlike 0034, which needed 0038
-- to repair it): the Go store normalises a pair by Go's byte-wise string
-- comparison, while the database's default collation is locale-aware and orders
-- '_' differently, so without the explicit collation a perfectly normalised pair
-- of imported uids can trip the constraint.
--
-- Both uids CASCADE to photos: a purged photo cannot be half of a pair. The
-- confirming user is SET NULL — the decision outlives the account that made it,
-- the same way rejected_by and dismissed_by do.
--
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE duplicate_confirmations (
    id           BIGSERIAL   PRIMARY KEY,
    photo_uid    VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    other_uid    VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    confirmed_by VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    confirmed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT duplicate_confirmations_ordered
        CHECK (photo_uid COLLATE "C" < other_uid COLLATE "C"),
    UNIQUE (photo_uid, other_uid)
);

-- The scan reads every confirmation at once (one bulk lookup per GET
-- /duplicates, never an N+1), so the UNIQUE index above already serves it. This
-- second index covers the reverse direction, for looking up what a single photo
-- was confirmed against without scanning the table.
CREATE INDEX idx_duplicate_confirmations_other ON duplicate_confirmations (other_uid);
