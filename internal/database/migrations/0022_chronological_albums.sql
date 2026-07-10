-- Albums are always presented chronologically: oldest capture time first, with
-- the upload (catalogue insertion) time standing in for photos whose capture
-- time is unknown. The per-album ordering choice and the manual drag-and-drop
-- positions have nothing left to drive, so both columns go: albums.order_by
-- held the chosen ordering mode, album_photos.sort_order the manual positions.
ALTER TABLE albums DROP COLUMN IF EXISTS order_by;
ALTER TABLE album_photos DROP COLUMN IF EXISTS sort_order;
