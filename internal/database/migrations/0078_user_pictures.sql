-- 0078_user_pictures: an account may wear a real picture instead of a letter.
--
-- Until now every user was drawn as a coloured circle with the first letter of
-- their name. This table holds the two answers a person can give instead: a
-- picture they uploaded, or a photograph of the library they pointed at. The
-- third answer — the face of the person their account says they are — needs no
-- storage at all: it is already in users.subject_uid, and it is the default, so
-- an account that has said who it is wears that face without a row here.
--
-- One row per account, so the primary key is the account: a user has one
-- picture, and setting a new one replaces the old. ON DELETE CASCADE, because a
-- picture of a deleted account is nothing at all.
--
-- # Why the bytes live here and not in the object store
--
-- A profile picture is not library media. It has no original to keep, no EXIF to
-- read, no thumbnails to derive and no place in a photo album; it is a ~512 px
-- square the server re-encodes on receipt and can regenerate from nothing if it
-- is lost. Putting it in the bucket would give it a storekeys Kind and therefore
-- a place in backup, storage migration, the orphan sweep and the library wipe —
-- four contracts to extend for a few tens of kilobytes per account. In Postgres
-- it rides the database dump the backup already takes, and every one of those
-- four is left exactly as it is.
--
-- The column is deliberately absent from the users table, where a reader might
-- expect it. Every read of an account goes through one canonical column list
-- (auth.userColumns) and is scanned into the model — a login, a session check,
-- an admin listing — and putting a picture in that list would load the image on
-- every authenticated request in the instance. The picture is read by exactly
-- one endpoint; it belongs in a table only that endpoint touches.
--
-- # Why photo_uid has no foreign key
--
-- A picked photograph is stored as a bare uid on purpose. `kukatko maintenance
-- reset` empties the catalogue with TRUNCATE and refuses to run when a table it
-- preserves points into one it wipes, so a foreign key here would force this
-- table into one of two bad shapes: wiped with the library (throwing away
-- uploads, which are not library data), or the reason the reset needs yet
-- another special case. And the fallback it would enforce is needed anyway: a
-- photo can stop being usable without being deleted — archived, or flagged
-- private or hidden — so the resolver must survive a uid it cannot serve
-- whatever the schema says. A dangling uid is therefore an ordinary state, and
-- it means the same as no picture: fall through to the next source in the chain.

CREATE TABLE user_pictures (
    user_uid VARCHAR(32) PRIMARY KEY REFERENCES users (uid) ON DELETE CASCADE,
    -- Which of the two stored answers this row is. The third source of the chain
    -- (the linked subject's face) never appears here: it is not stored, it is
    -- what the resolver falls through to.
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('upload', 'photo')),
    -- The re-encoded square JPEG, for an upload. The submitted original is never
    -- kept: the server decodes it, crops it square, scales it to at most 512 px
    -- and stores only that.
    image BYTEA,
    -- The photograph of the library a user pointed at, for a pick. Not a foreign
    -- key; see above.
    photo_uid VARCHAR(32),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Exactly one source per row, and it has to be the one `kind` names. Without
    -- this a row could say 'upload' and carry no bytes, which the endpoint would
    -- discover only when somebody asked for the picture.
    CONSTRAINT user_pictures_one_source CHECK (
        (kind = 'upload' AND image IS NOT NULL AND photo_uid IS NULL)
        OR (kind = 'photo' AND photo_uid IS NOT NULL AND image IS NULL)
    )
);

COMMENT ON TABLE user_pictures IS
    'A user''s profile picture: an uploaded square JPEG, or a uid reference to a library photo. Absent means the chain falls through to the linked subject''s face and then to the coloured initial.';
