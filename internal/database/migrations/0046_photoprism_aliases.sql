-- 0046_photoprism_aliases: let ONE catalogue row answer to SEVERAL PhotoPrism
-- source identities.
--
-- photos.photoprism_uid is a 1:1 key: one catalogue row, one source photo. That
-- holds right up until the source keeps the same bytes twice. PhotoPrism happily
-- indexes two photo entries over byte-identical files; Kukátko deduplicates on the
-- SHA256 content hash (photos.file_hash is UNIQUE), so the second entry has no row
-- of its own to be written to — and the row that DOES hold its content already
-- wears the first entry's uid, which is why the import could not simply stamp its
-- references onto it (internal/ppimport, dedupByContent).
--
-- Until this table existed the second source photo was therefore dropped in
-- silence: counted as "skipped", nothing logged, no failure recorded, and its
-- albums, labels and face markers left behind with it because nothing could
-- resolve its uid to a row. On the production library that was 450 photos of
-- 20 660 — with the run reporting failed=0.
--
-- An alias records exactly that collapse: "source photo X is held by catalogue row
-- Y". The uid stays resolvable (internal/ppimport lookupImported,
-- internal/psfeedsimport, internal/importverify all consult it), so the
-- duplicate's albums, labels and markers land on the surviving row and the
-- reconciliation can account for it instead of reporting it missing.
--
-- Shape:
--   * photoprism_uid is the PRIMARY KEY: a source photo collapses onto exactly one
--     row, and re-running the import re-records the same alias idempotently.
--   * photo_uid is many-to-one and CASCADEs: if the surviving row is ever deleted,
--     the aliases pointing at it go with it and the source photos become
--     importable again (they will be re-downloaded and catalogued).
--   * photoprism_file_hash carries the SHA1 of the source FILE the alias came
--     from, purely as provenance for the reconciliation and for debugging.
--
-- A uid may not be both a photos.photoprism_uid and an alias; that is enforced by
-- the importer (an alias is only ever written for a uid no row carries), not by a
-- constraint — a cross-table exclusion would cost a trigger on the hot photos
-- path, and the reconciliation counts a photo once either way.
--
-- No data is rewritten and nothing is dropped. This migration is wrapped in a
-- transaction by the runner.

CREATE TABLE photoprism_aliases (
    photoprism_uid       VARCHAR(64) PRIMARY KEY,
    photo_uid            VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    photoprism_file_hash VARCHAR(64),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_photoprism_aliases_photo_uid ON photoprism_aliases (photo_uid);
