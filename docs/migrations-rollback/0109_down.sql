-- 0109_down: drop the invitation links.
--
-- An accepted invite already left its membership in project_members, which
-- survives; only the links themselves (and their use counters) go. A release
-- older than 0109 has no invite route, so a live link would be dead anyway.
DROP TABLE IF EXISTS project_invites;

DELETE FROM schema_migrations WHERE version = 109;
