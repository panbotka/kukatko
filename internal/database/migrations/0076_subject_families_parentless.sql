-- 0076_subject_families_parentless: a family may have no partners at all, so
-- "these two are siblings" can be recorded when neither parent is in the library.
--
-- Migration 0073 required at least one partner (subject_families_has_partner),
-- on the reasoning that a family with neither partner is an orphaned row that
-- children would hang off nothing. That reasoning holds for a family nobody put
-- children in; it is wrong for the case this migration opens. Two people known
-- to be brother and sister, whose parents were never photographed and are not in
-- the library, have a real shared fact and nowhere to write it: the siblings row
-- of the family strip dead-ended with "add a parent first", which is not advice —
-- the parents exist, they simply have no page here. The alternative, a
-- placeholder "unknown parent" subject, buys the constraint at the price of a
-- phantom person in the people list, which is a worse lie than an empty pair.
--
-- So a bare sibling group is now a legal family: no partners, two or more
-- children, and every derived relation still reads off it unchanged — the other
-- children of the family I am a child in are my siblings, whether or not anybody
-- is recorded above us. Attaching a parent later attaches them to *this* family
-- (never a second one), which is why the group keeps its uid and its children
-- keep their memberships.
--
-- The index has to become partial for exactly the reason the CHECK existed.
-- idx_subject_families_pair is UNIQUE with NULLS NOT DISTINCT, which is what
-- makes one couple exactly one family and one lone parent exactly one family;
-- with both columns NULL those two NULLs also compare equal to every *other*
-- all-NULL row, so a second parentless family in the library would be refused
-- and every sibling group would collapse into a single row. Restricting the
-- index to rows that name at least one partner keeps both guarantees it was
-- written for and drops the one it was never asked to make.
--
-- The remaining CHECKs are untouched and still do their work: a parentless row
-- has no pair to order and no pair to tell apart, so partners_ordered and
-- partners_distinct pass it trivially.
--
-- What holds a parentless family together instead is the store: it is created
-- with two children at once and deleted as soon as it drops below two (see
-- internal/family pruneFamily), so the row this migration permits cannot linger
-- as the orphan 0073 was guarding against.
--
-- This migration is wrapped in a transaction by the runner. Both statements are
-- catalogue-level: the table holds tens of rows in the library this was written
-- for, so rebuilding the index takes no measurable lock time and CONCURRENTLY —
-- which cannot run in a transaction anyway — would buy nothing.

ALTER TABLE subject_families DROP CONSTRAINT subject_families_has_partner;

DROP INDEX idx_subject_families_pair;

CREATE UNIQUE INDEX idx_subject_families_pair
    ON subject_families (partner_a_uid, partner_b_uid) NULLS NOT DISTINCT
    WHERE partner_a_uid IS NOT NULL OR partner_b_uid IS NOT NULL;
