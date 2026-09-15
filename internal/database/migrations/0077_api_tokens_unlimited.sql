-- 0077_api_tokens_unlimited: a token that the rate limiters let through.
--
-- The throttles on commenting, uploading and bulk editing exist to keep one
-- noisy *person* from spending the box's CPU on everybody else's behalf. An
-- agent driving the library through `kukatko ctl` is not that person: it works
-- through thousands of photographs in one sitting, in bursts that are exactly
-- what the limiters are shaped to stop, and it is doing work somebody asked for.
-- Loosening the limits for everyone to accommodate it would remove the
-- protection; this column instead lets an administrator exempt one named
-- credential.
--
-- Why it lives on the token and not on the user:
--
--   * a session cookie must never carry the exemption, whatever the owner's
--     role — a browser does not need it, and a stolen cookie would otherwise
--     bypass every limit the library has;
--   * the exemption is then revocable the way a credential is: revoke the token
--     and it is gone, without touching the person's account;
--   * the flag travels with the row that bearer authentication already reads, so
--     a limiter costs no second lookup to consult it.
--
-- NOT NULL DEFAULT FALSE, so every token that exists today — and every token
-- minted without saying anything about this — is throttled exactly as before.

ALTER TABLE api_tokens
    ADD COLUMN unlimited BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN api_tokens.unlimited IS
    'When true, requests bearing this token bypass the comment/upload/bulk rate limiters. Admin-only to set.';
