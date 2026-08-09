-- 0055_photos_taken_at_precision: how fine the capture date actually is.
--
-- 0029 gave a photo an "this date is a guess" flag (taken_at_estimated) and a
-- free-text note, which says *whether* to trust taken_at but not *how much of it*
-- to trust. A box of scans dated "somewhere in 1974" has to be stored as some
-- concrete timestamp — the timeline, the period filter, the year facets and the
-- query language all read taken_at and nothing else — so the app picks the first
-- instant of the period. Without this column the detail view then reads that
-- anchor back as "1 January 1974", a day nobody ever claimed.
--
-- taken_at_precision records the grain the date was stated at:
--
--   * day    — a real date (EXIF, or one the user typed). The default, and what
--              every existing row means.
--   * month  — "March 1974"; the anchor is the first day of that month.
--   * year   — "1974"; the anchor is 1 January of that year.
--   * decade — "the seventies"; the anchor is 1 January of the decade's first year.
--
-- The anchor is always the **first instant of the period, in UTC**, so
-- date_part('year', taken_at) is the stated year by construction — the same UTC
-- reading the year facets and the period bounds already agree on. That is what
-- keeps a bulk-dated scan sorting and filtering into its period everywhere while
-- the presentation layer still shows only as much of the date as was claimed.
--
-- It is deliberately separate from taken_at_estimated: precision is how coarse
-- the statement is, the flag is whether it is a guess at all. A date can be an
-- exact-day guess ("cca 14. 6. 1974, podle babičky"), and a year can be a fact
-- ("the stamp on the back says 1974"). The bulk editor sets both together for
-- the coarse grains because a period picked for fifty scans at once is by nature
-- approximate, but nothing in the model ties them.
--
-- Like 0029's columns this is NOT NULL with a zero-ish default, so the Go model
-- stays a plain string and every existing row simply means what it always meant:
-- a date at day precision. No index: the column is presentation, never a filter
-- or a sort key, and taken_at remains the single ordering anchor.

ALTER TABLE photos
    ADD COLUMN taken_at_precision TEXT NOT NULL DEFAULT 'day'
        CHECK (taken_at_precision IN ('day', 'month', 'year', 'decade'));
