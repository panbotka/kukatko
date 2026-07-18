-- 0036_role_maintainer: add the 'maintainer' role and retire the 'ai' role.
--
-- Redefines the users.role CHECK constraint (last set by 0023_role_ai) to the
-- strict role ladder viewer < editor < admin < maintainer. The 'ai' role is
-- removed: maintainer is its successor at the top of the ladder, holding every
-- admin power plus operations (imports, maintenance, system status, backup,
-- restore, jobs, processing backfills).
--
-- Order matters. The old (0023) constraint admits 'ai' but NOT 'maintainer', so
-- it is dropped first — otherwise the data migration could not run. Then any
-- account still on the retired 'ai' role becomes a maintainer, and only after
-- that is the new constraint added: doing the UPDATE before the ADD is what keeps
-- the surviving rows legal (the new constraint forbids 'ai'). This is how the
-- botka automation account (currently 'ai') carries over as a maintainer. The
-- runner wraps the whole migration in a transaction and DDL takes an exclusive
-- lock, so no concurrent write can slip a bad role in while the constraint is off.
--
-- sessions.role carries no CHECK constraint, so no change is needed there; any
-- live 'ai' session simply becomes invalid on next login, which is acceptable.

ALTER TABLE users DROP CONSTRAINT users_role_check;

UPDATE users SET role = 'maintainer' WHERE role = 'ai';

ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (role IN ('viewer', 'editor', 'admin', 'maintainer'));
