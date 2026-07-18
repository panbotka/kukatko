-- 0038_duplicate_dismissals_collate_c: align the duplicate_dismissals ordering
-- CHECK with the canonical ordering the store actually produces.
--
-- internal/feedback normalises a pair to "smaller uid first" with Go string
-- comparison, which is byte-order (code-point) comparison. Migration 0034's
-- CHECK (photo_uid < other_uid) compares in the column's default collation,
-- which on this deployment is en_US.utf8 — a locale collation. The two orderings
-- disagree for any uid containing punctuation such as '_': byte order sorts '_'
-- (0x5F) before the letters, en_US.utf8 does not. When they disagree the store
-- writes a row the CHECK rejects, so a dismissal naming a non-existent photo
-- whose uid contains '_' fails with a raw check_violation (SQLSTATE 23514)
-- instead of the foreign-key error the store maps to ErrTargetNotFound (a clean
-- 404) — the exact mismatch TestDuplicateDismissalRejectsBadKeys exposed.
--
-- Real photo uids are lowercase base32 ([0-9a-v]), where byte order and
-- en_US.utf8 already agree, so stored rows are unaffected and every existing row
-- still satisfies the C-order CHECK (the ADD below re-validates them). Recreating
-- the constraint with COLLATE "C" makes the database enforce the exact ordering
-- the store produces, for every possible uid, so the invariant "the store
-- normalises → the CHECK agrees" holds by construction rather than only for the
-- alphabet real uids happen to use.

ALTER TABLE duplicate_dismissals
    DROP CONSTRAINT IF EXISTS duplicate_dismissals_ordered;

ALTER TABLE duplicate_dismissals
    ADD CONSTRAINT duplicate_dismissals_ordered
    CHECK (photo_uid COLLATE "C" < other_uid COLLATE "C");
