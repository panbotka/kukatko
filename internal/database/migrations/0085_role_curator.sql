-- 0085_role_curator: add the 'curator' role between viewer and editor.
--
-- Redefines the users.role CHECK constraint (last set by 0036_role_maintainer) to
-- the ladder viewer < curator < editor < admin < maintainer. A curator may curate
-- the catalogue (faces, people, albums, labels) without the wider write access
-- that rewrites a photo's own metadata; which endpoints admit it is decided in Go
-- (Role.CanCurate), not here.
--
-- Nothing is retired, so unlike 0036 there is no data migration: every existing
-- row already satisfies the wider constraint. The runner wraps the migration in a
-- transaction and DDL takes an exclusive lock, so no concurrent write can slip a
-- bad role in while the constraint is off.
--
-- sessions.role carries no CHECK constraint, so no change is needed there.

ALTER TABLE users DROP CONSTRAINT users_role_check;

ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (role IN ('viewer', 'curator', 'editor', 'admin', 'maintainer'));
