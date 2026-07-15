-- 0030_photo_stacks: group the several files of one shot into a single library
-- item — a "stack" — without ever merging rows.
--
-- Shooting RAW+JPEG produces IMG_1234.CR2 and IMG_1234.jpg; an exported edit
-- lands a second JPEG next to the original; each is ingested as its own photos
-- row, so the same moment occupies two or three tiles in the grid and is counted
-- twice in every album and search. A stack groups those rows behind one visible
-- "primary" member and hides the rest from the default views.
--
-- Grouping, NOT merging. Two columns are added to photos; every member keeps its
-- own full row — its own dimensions, EXIF, embedding, faces and thumbnails.
-- Stacking and unstacking are then pure, reversible bookkeeping (set the column,
-- clear the column), so nothing is ever lost. That reversibility is the whole
-- reason a user can trust the automatic detection over their entire library. Do
-- NOT "simplify" this into folding one row into another the way dupmerge does for
-- genuine duplicates: stack members are kept on purpose, not redundant.
--
--   * stack_uid     — the identifier shared by every member of one stack. NULL
--     means the photo is not stacked. It is a VARCHAR(32) like every other uid.
--   * stack_primary — the one member shown in grids/albums/search/counts. Exactly
--     one member of a stack carries it; the rest are hidden from the default view.
--
-- The photo_files table (roles original/sidecar/edited) is a DIFFERENT concept —
-- the several files belonging to one photo row (e.g. a Live Photo's still + its
-- companion clip). Stacks group whole photo rows. The two do not conflate.

ALTER TABLE photos
    ADD COLUMN stack_uid     VARCHAR(32),
    ADD COLUMN stack_primary BOOLEAN NOT NULL DEFAULT false,
    -- A primary must belong to a stack; a bare primary flag is meaningless.
    ADD CONSTRAINT ck_photos_stack_primary_has_uid
        CHECK (NOT stack_primary OR stack_uid IS NOT NULL);

-- Exactly one primary per stack: the partial unique index rejects a second
-- primary for the same stack_uid. NULL stack_uids are distinct, so unstacked
-- photos never collide.
CREATE UNIQUE INDEX idx_photos_stack_primary ON photos (stack_uid) WHERE stack_primary;

-- Group lookups (list a stack's members) and the "hide non-primary members"
-- view predicate both filter on stack_uid; only stacked rows are the minority,
-- so index just those.
CREATE INDEX idx_photos_stack_uid ON photos (stack_uid) WHERE stack_uid IS NOT NULL;
