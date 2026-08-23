-- 0063_users_email_required: an account must hold an e-mail address.
--
-- Every account receives mail — registration, approval, password reset — so
-- users.email has to be a usable address rather than the empty string 0002_auth
-- allowed as its default. This migration makes that true of the rows already in
-- the table and then stops new empty ones from arriving.
--
-- The rows without an address get a placeholder in the reserved .invalid
-- top-level domain (RFC 2606), which by definition can never resolve: a
-- placeholder that leaked into a real send would bounce at the resolver instead
-- of reaching a stranger's mailbox. It is derived from the username so an
-- administrator reading the roster can still tell whose account it is.
--
-- The local part is the username reduced to [a-z0-9-], trimmed of leading and
-- trailing separators — a username is free text and may hold spaces or accents,
-- neither of which belongs in an address — and then joined to the account's uid.
-- That uid is what keeps the placeholders distinct: reducing the character set
-- can map two different usernames onto the same local part ("jan novák" and
-- "jan-novak" both become "jan-nov-k"), and two people must not end up sharing an
-- address by accident. A username that survives the reduction as nothing at all
-- (say "☺") falls back to "user", so the local part never starts with the
-- separator.
--
-- Truncating the username part at 31 characters keeps the whole local part
-- within the 64-octet limit of RFC 5321: 31 + 1 separator + 26 for the uid.
--
-- "Empty" means empty of anything usable, so a column holding only whitespace is
-- filled too and the CHECK refuses it — an address of three spaces is no more a
-- mailbox than no address at all.
--
-- Only the empty-string default is dropped; the column keeps its NOT NULL. The
-- CHECK is deliberately no stricter than "non-blank" — syntax is validated in
-- internal/auth, where a rejection can name the offending field, and a database
-- constraint that encoded an address grammar would be both wrong and unteachable.
--
-- There is deliberately NO unique index: a household mailbox that two accounts
-- share is a real arrangement, and refusing the second account would be wrong.
--
-- This migration is wrapped in a transaction by the runner.

UPDATE users
SET email = left(
        coalesce(
            nullif(trim(both '-' from regexp_replace(lower(username), '[^a-z0-9]+', '-', 'g')), ''),
            'user'
        ),
        31
    ) || '-' || uid || '@kukatko.invalid'
WHERE btrim(email) = '';

ALTER TABLE users ALTER COLUMN email DROP DEFAULT;
ALTER TABLE users ADD CONSTRAINT users_email_not_empty CHECK (btrim(email) <> '');
