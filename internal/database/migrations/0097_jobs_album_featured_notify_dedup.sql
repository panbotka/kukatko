-- 0097_jobs_album_featured_notify_dedup: at most one *queued* album_featured_notify
-- job per album.
--
-- The `album_featured_notify` job announces one newly featured album (the
-- homepage's "Doporučujeme" strip, migration 0094) to every account that wants
-- the album_featured notification kind. The organize store's feature observer
-- enqueues it inside the feature's transaction, and only when the album was
-- not featured already — so prolonging a running feature announces nothing.
--
-- The queue's existing dedup index (idx_jobs_dedup) keys on
-- (type, payload ->> 'photo_uid'), and this job belongs to no photo: its payload
-- carries an album_uid instead, the photo expression is NULL, and NULLs are
-- distinct in a unique index — so, exactly as 0089 (tag_notify) found, the job
-- needs an index of its own, keyed on the album named in the payload.
--
-- The predicate is state = 'queued' only, for the reason 0044 and 0089 give: a
-- job already *running* has read the album's state, so a feature → unfeature →
-- feature sequence landing after that read must be a fresh job, not swallowed
-- as a duplicate. While it is still queued, the same sequence collapses into
-- the one job, which reads the album once it runs and announces it once.
--
-- It is a partial index over one job type rather than a rewrite of
-- idx_jobs_dedup, so nothing about the per-photo dedup changes and this index
-- costs nothing for every other type. The store maps a violation of it to
-- ErrDuplicate exactly as it does the others.
--
-- This migration is wrapped in a transaction by the runner.

CREATE UNIQUE INDEX idx_jobs_album_featured_notify_dedup ON jobs ((payload ->> 'album_uid'))
    WHERE type = 'album_featured_notify' AND state = 'queued';
