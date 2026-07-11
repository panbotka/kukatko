-- 0025_user_ratings_eye: widen the per-user flag with a neutral 'eye' mark.
--
-- The per-user flag (0016_user_ratings) started as a Lightroom-style pick/reject
-- cull marker. It is reframed into a neutral personal marking with three
-- mutually-exclusive states shown as icons: 👁 eye, 👍 thumbs-up, 👎 thumbs-down.
-- The stored strings stay unchanged so existing rows remain valid — 'pick' backs
-- thumbs-up and 'reject' backs thumbs-down — and this migration only adds the new
-- 'eye' value to the CHECK constraint. Wrapped in a transaction by the runner.
--
-- The original constraint was created inline and unnamed in 0016, so Postgres
-- assigned it the conventional name user_ratings_flag_check. Dropping it by that
-- name (IF EXISTS, so a re-run is a no-op) and re-adding the widened constraint
-- keeps existing 'none'/'pick'/'reject' rows valid.

ALTER TABLE user_ratings DROP CONSTRAINT IF EXISTS user_ratings_flag_check;
ALTER TABLE user_ratings
    ADD CONSTRAINT user_ratings_flag_check
    CHECK (flag IN ('none', 'pick', 'reject', 'eye'));
