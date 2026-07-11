-- 0025_user_ratings_eye_flag: allow the new 'eye' personal mark.
--
-- The per-user flag on user_ratings (0016) started as a Lightroom-style pick/reject
-- cull marker. It is being reframed as a neutral personal mark with three
-- mutually-exclusive states shown as icons: eye, thumbs-up and thumbs-down. The
-- stored strings are kept internal and unchanged ('pick' = thumbs-up, 'reject' =
-- thumbs-down) so existing rows stay valid; this migration only widens the CHECK
-- constraint to also accept the new 'eye' value.
--
-- Drop-then-add is idempotent: DROP ... IF EXISTS tolerates a missing constraint,
-- and the recreate installs the widened set. Existing 'none'/'pick'/'reject' rows
-- remain valid. This migration is wrapped in a transaction by the runner.

ALTER TABLE user_ratings DROP CONSTRAINT IF EXISTS user_ratings_flag_check;

ALTER TABLE user_ratings
    ADD CONSTRAINT user_ratings_flag_check CHECK (flag IN ('none', 'pick', 'reject', 'eye'));
