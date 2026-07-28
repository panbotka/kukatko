-- 0045_photos_photoprism_file_hash_index: index the PhotoPrism SHA1 file hash,
-- which the import now looks a photo up by.
--
-- Until now photoprism_file_hash was write-only bookkeeping: the import found an
-- already-imported photo by photoprism_uid (indexed since 0003) and the hash was
-- only ever read back off a row it had already found. That changed when the
-- import started bringing across a PhotoPrism photo's NON-PRIMARY files — the RAW
-- next to its JPEG (internal/ppimport, siblings.go).
--
-- Such a sibling becomes its own catalogue row, stacked with the displayable
-- original (photos.stack_uid, see 0030), and deliberately carries NO
-- photoprism_uid: that column is the 1:1 key of the source photo, the one
-- internal/ppimport dedups incremental runs on and internal/psfeedsimport joins
-- photo-sorter's embeddings and faces onto, and a second row wearing it would
-- make both ambiguous. The sibling's identity is its own file hash instead, so
-- every run resolves it with an equality lookup on this column — once per
-- non-primary source file — and skips a download it has already made.
--
-- Partial, like its photoprism_uid counterpart: the column is NULL for every
-- photo that did not come from PhotoPrism (uploads, the photo-sorter migration),
-- and those rows are never the target of the lookup.
--
-- This migration is wrapped in a transaction by the runner.

CREATE INDEX idx_photos_photoprism_file_hash ON photos (photoprism_file_hash)
    WHERE photoprism_file_hash IS NOT NULL;
