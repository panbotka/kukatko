-- 0017_saved_searches: per-user saved searches (smart albums).
--
-- A saved search is a named, owner-private filter/search definition the user can
-- re-open later. It mirrors the per-user ownership model of user_favorites: only
-- the owner can see or modify their saved searches, enforced by scoping every
-- query to the acting user in the API layer.
--
-- params holds opaque saved view/search state (filters, sort, search query, mode)
-- as JSONB so the backend stays agnostic to the frontend's view shape. The owner
-- foreign key cascades on user deletion, so removing a user drops their saved
-- searches rather than leaving orphans. This migration is wrapped in a
-- transaction by the runner.

CREATE TABLE saved_searches (
    uid        VARCHAR(32)  PRIMARY KEY,
    owner_uid  VARCHAR(32)  NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    name       TEXT         NOT NULL,
    params     JSONB        NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Listing a user's saved searches is the hot path; index the owner column so the
-- owner-scoped lookups are an index scan rather than a sequential scan.
CREATE INDEX idx_saved_searches_owner_uid ON saved_searches (owner_uid);
