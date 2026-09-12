-- 0073_subject_families: a genealogy over subjects — who is whose parent, whose
-- partner, whose child.
--
-- The library knows 118 people and a hundred thousand faces of them, but nothing
-- about how they are related. Those facts are currently encoded in display names
-- — `Bohumil Nečas st.` is senior to somebody, `Nečasová` is the same family as
-- `Nečas` — which is where a data model goes when it does not exist.
--
-- The family is the node, not the edge. A relationship table of the shape
-- (from, to, type) with parent/spouse/sibling is the obvious first idea and the
-- wrong one: sibling and spouse edges are symmetric and have to be kept so by
-- hand, nothing stops a sibling edge from contradicting the parents, and
-- half-siblings are unrepresentable without a convention nobody remembers.
-- Genealogy software settled on this shape decades ago. A family is a couple (or
-- a lone parent) plus their children; everything else is derived and therefore
-- cannot disagree with itself: siblings are the other children of the family I
-- am a child in, a partner is the other partner of a family I am a partner in,
-- half-siblings are the children of another family one of my parents is a
-- partner in, and a second marriage is simply a second family.
--
-- The two-partner row also *is* the box the tree draws (`Marie ⚭ Bohumil`),
-- which a plain parent edge cannot express at all: a childless marriage would
-- otherwise be unrecordable.
--
-- Both partner columns are nullable, because a lone parent is a family: the
-- great-grandmother whose husband nobody remembers still has children. The
-- CHECKs then carry the invariants a nullable pair needs:
--
--   * subject_families_has_partner — a family with neither partner is not a
--     family, it is an orphaned row that children would hang off nothing.
--   * subject_families_partners_distinct — nobody is their own partner.
--   * subject_families_partners_ordered — the pair is unordered in meaning, so
--     it is normalised on write and the constraint holds the normalisation.
--     COLLATE "C" is load-bearing and not decoration: the Go side orders the
--     pair with `<` on strings, which is byte order, while this database's
--     default collation is locale- and version-dependent and orders `_` and
--     case differently. Without the pin, a pair Go considers normalised could be
--     rejected by the database; the repository already carries this trap
--     recorded from the duplicate-dismissal pair of migration 0038.
--   * subject_families_kind — a marriage, an unmarried partnership, or an
--     admitted "we do not know which".
--   * the two year CHECKs — 1800 is far below photography itself, so a mistyped
--     year (198, 19) is refused while every family a photo archive can hold
--     still fits, and a partnership cannot end before it began. Both years stay
--     nullable: in an archive nobody wrote most of them down.
--
-- idx_subject_families_pair is UNIQUE with NULLS NOT DISTINCT (Postgres 15+,
-- and 17 is what runs here), which makes one couple exactly one family — and,
-- because the two NULLs of a lone parent then compare equal, one lone parent
-- exactly one family too. Without NULLS NOT DISTINCT every lone-parent family
-- would be silently duplicable and a person's children would scatter across
-- rows nothing joins back together.
--
-- subject_family_children is the membership, keyed by (family, child) so a child
-- is listed once per family, with kind recording how they belong: born to it,
-- adopted into it, or a step-child brought in by a partner.
--
-- idx_subject_family_children_child is the constraint that makes the whole thing
-- walkable: a person is a child in **at most one** family. That is what keeps
-- the descendant walk a tree rather than a general graph, and it removes an
-- entire class of contradictory data (two sets of parents disagreeing about who
-- somebody's mother was).
--
-- The deliberate limitation that follows: because a lone parent has exactly one
-- family row, two children of the same mother by two unknown fathers appear as
-- full siblings rather than half. The escape hatch is a placeholder subject for
-- the unknown father, which the model already supports — a subject with no
-- photos is legal here. That is the right trade for a photo archive and the
-- wrong one for a genealogy program, and the difference is the point.
--
-- Every foreign key cascades: a subject that no longer exists is not a partner
-- and not a child, and a family whose row is gone has no membership.
--
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE subject_families (
    uid            VARCHAR(32) PRIMARY KEY,
    partner_a_uid  VARCHAR(32) REFERENCES subjects (uid) ON DELETE CASCADE,
    partner_b_uid  VARCHAR(32) REFERENCES subjects (uid) ON DELETE CASCADE,
    kind           TEXT        NOT NULL DEFAULT 'partnership',
    from_year      INTEGER,
    to_year        INTEGER,
    note           TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT subject_families_has_partner
        CHECK (partner_a_uid IS NOT NULL OR partner_b_uid IS NOT NULL),
    CONSTRAINT subject_families_partners_distinct
        CHECK (partner_a_uid IS NULL OR partner_b_uid IS NULL
               OR partner_a_uid <> partner_b_uid),
    CONSTRAINT subject_families_partners_ordered
        CHECK (partner_a_uid IS NULL OR partner_b_uid IS NULL
               OR partner_a_uid < partner_b_uid COLLATE "C"),
    CONSTRAINT subject_families_kind
        CHECK (kind IN ('marriage', 'partnership', 'unknown')),
    CONSTRAINT subject_families_from_year_range
        CHECK (from_year IS NULL OR from_year >= 1800),
    CONSTRAINT subject_families_to_after_from
        CHECK (to_year IS NULL OR from_year IS NULL OR to_year >= from_year)
);

CREATE UNIQUE INDEX idx_subject_families_pair
    ON subject_families (partner_a_uid, partner_b_uid) NULLS NOT DISTINCT;
CREATE INDEX idx_subject_families_partner_a ON subject_families (partner_a_uid);
CREATE INDEX idx_subject_families_partner_b ON subject_families (partner_b_uid);

CREATE TABLE subject_family_children (
    family_uid VARCHAR(32) NOT NULL REFERENCES subject_families (uid) ON DELETE CASCADE,
    child_uid  VARCHAR(32) NOT NULL REFERENCES subjects (uid) ON DELETE CASCADE,
    kind       TEXT        NOT NULL DEFAULT 'birth',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (family_uid, child_uid),
    CONSTRAINT subject_family_children_kind
        CHECK (kind IN ('birth', 'adopted', 'step'))
);

CREATE UNIQUE INDEX idx_subject_family_children_child
    ON subject_family_children (child_uid);
