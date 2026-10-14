-- 0119_notifications_params: the facts a notification's text was rendered from.
--
-- A notification's title and body are rendered once, server-side, in the
-- instance's language (Czech), and stored. An account reading the inbox in
-- English saw them in Czech. The render functions (internal/notification
-- texts.go) now also return the facts they wrote the text from — a count, a
-- name, an album or task title, the variant — and they are stored here, so the
-- inbox renders the same message in the reader's language from kind + params.
--
-- NULL is the stored text alone: every row written before this migration, and
-- any row whose producer had no facts to give. The reader falls back to title
-- and body then, which stay what a push and an e-mail carried.
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS params JSONB;

COMMENT ON COLUMN notifications.params IS
    'Facts the title/body were rendered from (internal/notification Params), so the inbox can re-render them in the reader''s language; NULL = show the stored text.';
