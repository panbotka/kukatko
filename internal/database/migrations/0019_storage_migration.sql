-- 0019_storage_migration: record which photos are confirmed present in the
-- object store, so the local-disk-to-R2 move can be interrupted and resumed.
--
-- `kukatko storage migrate-to-r2` copies roughly 120 GB of originals and their
-- cached thumbnails into the bucket over hours, on a machine that may be killed
-- at any moment. storage_migrated_at is that job's cursor: it is stamped only
-- after every object of the photo has been uploaded and read back with the
-- expected size and SHA256, and the local original is removed only after the
-- stamp is committed. A photo that failed verification, or that was interrupted
-- mid-upload, keeps a NULL stamp and is simply retried by the next run.
--
-- This is the high-watermark rule of import_runs (0013) — only a successful
-- step advances the cursor, so a crash never loses work and never skips it —
-- applied per row rather than per run. It has to be: a scalar watermark assumes
-- work completes in the order it was handed out, and under bounded upload
-- concurrency photo N+1 routinely lands before photo N. A per-photo stamp is
-- exact under any interleaving, and the resume query below is what a scalar
-- cursor would only have approximated.
--
-- NULL means "not known to be in the object store", not "not there". Rows
-- ingested straight into an R2 deployment start NULL as well; a migration run
-- over them uploads nothing (every object is already in place with the right
-- digest, which the run confirms with one cheap metadata request each) and just
-- stamps them. This migration is wrapped in a transaction by the runner.

ALTER TABLE photos ADD COLUMN storage_migrated_at TIMESTAMPTZ;

-- Resume index: every batch asks for the next page of photos that still lack the
-- stamp, ordered by uid. The partial predicate keeps the index to exactly those
-- rows, so it costs nothing on a migrated library and shrinks to empty as the
-- job completes.
CREATE INDEX idx_photos_storage_pending ON photos (uid) WHERE storage_migrated_at IS NULL;
