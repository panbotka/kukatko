# Family tree over subjects — design

**Status:** proposed — awaiting review. No code has been written for this document.

Kukátko knows 118 people and 116 704 faces. What it does not know is that
`Bohumil Nečas st.` is the father of `Bohumil Nečas ml.`, that `Nečasová` is the same
family as `Nečas`, or who `Miloslav Novotný st.st.` was senior to. Those facts are
currently encoded in display names, which is where a data model goes when it does not
exist.

This document designs the model, the API, the tree view and the search key that let the
library hold them properly.

## 1. What the library looks like today

Measured against production on 2026-09-12:

| | |
| --- | --- |
| subjects | 118, all `type='person'`, all named |
| subjects with a birth year | 3 |
| subjects with a death year | 0 |
| subjects appearing on a photo | 117 |
| largest surname groups | Nečas ×8, Fabiánek ×6, Skoták ×6, Souček ×6, Jarůšek ×4, Novotný ×4 |
| names carrying `st.` / `ml.` / `st.st.` | 8 |

Two consequences shape the design. First, a genealogy cannot be derived from dates —
three birth years is nothing, so relationships have to stand on their own. Second,
feminine surnames (`Nečas`/`Nečasová`, `Souček`/`Součková`, `Straka`/`Straková`) split
one family into two groups, so surname is not even a usable heuristic.

## 2. Decisions taken before this document

Settled with the user during design:

1. **A real genealogy, with a drawn tree** — not merely a naming aid.
2. **A person who appears on no photo is an ordinary subject** with an empty gallery.
   A great-grandmother nobody photographed is a normal node; `/people` must render a
   tile without a face crop, and "a subject with zero photos" stops being an anomaly.
3. **"The Nečas family" means the descendants of a chosen root** (plus their partners),
   not a connected component and not a hand-curated group. Reason: in a village the
   families eventually marry into each other, and a component-based family would one day
   swallow the whole of Veselice and stop filtering anything.

## 3. Data model

### 3.1 Why families and not edges

A relationship table of the shape `(from, to, type)` with `parent` / `spouse` / `sibling`
is the obvious first idea and the wrong one. Sibling and spouse edges are symmetric and
have to be kept so by hand; nothing stops a sibling edge from contradicting the parents;
half-siblings are unrepresentable without a convention nobody remembers. Genealogy
software settled on a different shape decades ago, and this design follows it:

**The family is the node.** A family is a couple (or a lone parent) plus their children.
A person is a child in at most one family and a partner in any number of families.

Everything else is derived, and therefore cannot disagree with itself:

- **siblings** = the other children of the family I am a child in
- **partner** = the other partner of a family I am a partner in
- **half-siblings** = children of another family one of my parents is a partner in
- **a second marriage** = a second family, and the step-relations fall out correctly

The two-column couple also *is* the box the tree draws (`Marie ⚭ Bohumil`), which a plain
parent edge cannot express at all: a childless marriage would be unrecordable.

### 3.2 Migration `0073_subject_families.sql`

```sql
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
```

Three constraints carry the weight:

- **`subject_families_partners_ordered` with `COLLATE "C"`.** The pair is normalised in
  Go by byte order; the database default is `en_US.utf8`, whose ordering is
  locale-dependent, version-dependent (the test database currently warns that its
  collation was built against glibc 2.41 while the OS provides 2.36) and not byte order.
  Pinning `COLLATE "C"` makes the check deterministic and identical to the comparison the
  Go side performs. A pair that Go considers normalised must never be rejected by the
  database, and this is the only way to guarantee it. The repository already carries this
  trap recorded from an earlier ordered-pair constraint.
- **`idx_subject_families_pair … NULLS NOT DISTINCT`** makes one couple exactly one
  family, and one lone parent exactly one family. Postgres 17 is in production, so the
  `NULLS NOT DISTINCT` form is available.
- **`idx_subject_family_children_child`** enforces *a person is a child in at most one
  family*. This is what keeps the descendant walk a tree rather than a general graph, and
  it removes an entire class of contradictory data.

**Deliberate limitation.** Because a lone parent has exactly one family row, two children
of the same mother by two unknown fathers appear as full siblings rather than half. The
escape hatch is to create a placeholder subject for the unknown father, which the model
already supports (a subject with no photos is legal — decision 2). This is the right
trade for a photo archive; it is not the right trade for a genealogy program, and that
difference is the point.

### 3.3 Cycles

The one-family-per-child rule does not by itself prevent a cycle: A can be a child in a
family whose partner is B, while B is a child in a family whose partner is A. Before a
child is attached, the store walks the prospective parents' ancestors and refuses if the
child is among them (`ErrCycle`). The recursive walks additionally carry a `depth < 20`
guard, so a cycle that somehow reached the table cannot hang a request.

### 3.4 Reset classification

Both tables go into `catalogueTables` in `internal/reset/tables.go`, alphabetically near
`subjects`. Omitting them aborts `maintenance reset` against the live schema, and the
integration test in `internal/reset` fails — which is the intended safety net, but it
fails late, so do it in the same commit as the migration.

### 3.5 Evidence

The DDL in §3.2 and the walk in §4 were executed against the test database on 2026-09-12
in a throwaway schema inside a rolled-back transaction. Confirmed there:

- the DDL is accepted as written, including `NULLS NOT DISTINCT` on a `CREATE UNIQUE INDEX`
  and `COLLATE "C"` inside a `CHECK`;
- a second lone-parent family for the same subject is refused by `idx_..._pair`
  (`duplicate key … (m1, null)`), so `NULLS NOT DISTINCT` behaves as relied upon;
- attaching a child who already has a family is refused by `idx_..._child`;
- the descendant walk over a three-generation seed returns the root at depth 0, both
  children at depth 1 and the grandchild at depth 2;
- a `WITH RECURSIVE` nested inside a correlated `EXISTS` — the exact shape §7 needs — is
  valid and returns only the photos of the walked set.

## 4. The descendant walk

```sql
WITH RECURSIVE descendants(uid, depth) AS (
        SELECT $1::varchar, 0
    UNION
        SELECT c.child_uid, d.depth + 1
        FROM descendants d
        JOIN subject_families f
          ON f.partner_a_uid = d.uid OR f.partner_b_uid = d.uid
        JOIN subject_family_children c ON c.family_uid = f.uid
        WHERE d.depth < 20
)
SELECT uid FROM descendants;
```

`UNION` rather than `UNION ALL` is load-bearing: when cousins marry — which in a village
they do — the same person is reachable by two paths, and `UNION ALL` would both duplicate
the rows and, with the depth guard alone, explode combinatorially.

Partners of descendants are a second, non-recursive step: every subject that shares a
family with someone in the set. That is the "plus their partners" half of decision 3.

Ancestors are the mirror image and are bounded by construction (2, 4, 8, 16 …), so the
pedigree query takes a `generations` argument and needs no dedup subtlety beyond `UNION`.

## 5. Package `internal/family`

A new package rather than more files in `internal/people`. `people` is already fifteen
files and owns subjects, markers and the `faces` name cache; genealogy is a separate
concern with its own recursion and its own invariants, and it reads subjects without
owning them.

```go
type Family struct {
    UID       string
    PartnerA  *string
    PartnerB  *string
    Kind      Kind      // marriage | partnership | unknown
    FromYear  *int
    ToYear    *int
    Note      string
    CreatedAt time.Time
    UpdatedAt time.Time
}

// Relations is the derived view of one subject's immediate family.
type Relations struct {
    Parents  []Relative
    Siblings []Relative
    Partners []Partnership
    Children []Relative
}
```

`Relative` carries the subject plus its cover and photo count, so the strip on the
subject page renders from one response.

Store methods, over `*pgxpool.Pool`, matching `internal/people` conventions (package-level
`const …SQL` strings, a `scanX(pgx.Row)` helper, sentinel errors, every wrapped error
prefixed `"family: "`):

| Method | Notes |
| --- | --- |
| `Relations(ctx, subjectUID)` | the four derived lists |
| `Descendants(ctx, rootUID, opts)` | §4; `opts.WithPartners` |
| `Ancestors(ctx, subjectUID, generations)` | pedigree |
| `Tree(ctx, rootUID, direction)` | layout-ready payload for the page |
| `AddParentAudited` / `AddChildAudited` / `AddPartnerAudited` | via `mutateAudited` |
| `RemoveRelationAudited` | |
| `UpdateFamilyAudited` | kind, years, note |

Sentinel errors: `ErrSubjectNotFound`, `ErrCycle`, `ErrAlreadyChild`, `ErrSelfRelation`.

Writes go through the existing generic `mutateAudited(ctx, pool, entry, func(tx) (T, error))`
so the audit row lands in the mutation's own transaction. Two recorded traps apply:
`entry.TargetUID` must be stamped **before** the call, because `mutateAudited` copies the
entry and a stamp made inside the closure is lost; and integration tests writing an audit
entry must seed the acting user, since the audit actor is a foreign key.

## 6. HTTP API

Flat patterns, no `chi.Mount` — matching `peopleapi`, which is deliberately flat so that
`outlierapi` can hang `GET /subjects/{uid}/outliers` off the same prefix.

| Method | Path | Guard |
| --- | --- | --- |
| `GET` | `/subjects/{uid}/relations` | RequireAuth |
| `POST` | `/subjects/{uid}/relations` | RequireWrite |
| `DELETE` | `/subjects/{uid}/relations/{uid2}` | RequireWrite |
| `GET` | `/subjects/{uid}/tree?direction=…&generations=…` | RequireAuth |
| `PATCH` | `/families/{uid}` | RequireWrite |

The `POST` body takes either an existing `subject_uid` **or** an inline `new_subject`, and
creates the subject and the relation in one audited transaction:

```json
{ "role": "parent", "new_subject": { "name": "Marie Nečasová", "birth_year": 1921 } }
```

That single affordance is what makes filling the tree bearable — otherwise every
great-grandmother is a trip to another screen and back. See §10.

New audit actions in `internal/audit`: `subject.relation.add`, `subject.relation.remove`,
`family.update`.

**Mounting.** `buildServices` in `cmd/kukatko` sits at the `funlen` limit, so a fresh
`server.WithAPI(...)` line fails lint. Mount the new routes through the existing
`peopleapi` options helper instead.

## 7. Search: the `family:` key

`family:<name|uid>` matches photos showing the chosen subject or any of their descendants
and those descendants' partners — the same set the tree draws, so the page and the filter
can never disagree.

1. `KeyFamily Key = "family"` in `internal/query`, `specs[KeyFamily] = {kind: KindText}`.
   No Czech alias: the key registry is English-only by convention, and `osoba:` is
   already deliberately unsupported.
2. `familyCond` in `internal/photos/store_query.go`, shaped like `personCond` — a
   correlated `EXISTS` over `markers`, with the descendant set as an inner
   `WITH RECURSIVE`. Bind the LIKE pattern and the UID as **two separate placeholders**,
   as `personCond` does: reusing one `$n` for a `VARCHAR` uid and a text expression fails
   with SQLSTATE 42P08.
3. `family:me` resolves through `internal/personme`, exactly as `person:me` does, keeping
   `internal/query` caller-blind.
4. `Keys()` publishes the key, so the command palette and `GET /search/schema` pick it up
   with no further work.

**Performance.** The inner recursive CTE does not depend on the outer row, so Postgres
should hoist it into an InitPlan. Measure it against production's 21 018 photos before
declaring victory. If the planner does not cooperate, the fallback is to resolve the
descendant set in Go and pass it as `= ANY($n)` — which needs a pre-pass before
compilation, because `condBuilder` receives neither a context nor a pool. Prefer the
in-SQL form; take the fallback only on evidence.

## 8. Frontend

No new dependency. The dependency list is notably lean (`leaflet` is the only
visualisation library) and a genealogy layout is two hundred lines of arithmetic.

### 8.1 `FamilyStrip` on the subject page

`SubjectPage.tsx` gains a section between the header and the gallery: four rows —
*Rodiče · Sourozenci · Partner · Děti* — of round face chips. The Informace panel in the
viewer already renders people exactly this way; reuse that presentation rather than
inventing a second one. Under `canWrite`, each row gets a `+`.

This is what a reader wants in nine cases out of ten. A *Zobrazit rodokmen* link leads to
the full page.

### 8.2 `FamilyTreePage` at `/people/:uid/tree`

Split by direction, because each direction is a different problem:

- **Descendants** are a genuine tree once a couple is one box, so a classic tidy-tree
  (Reingold–Tilford) layout applies. Collapsible branches, pan and zoom, click a node to
  re-root. This is also exactly the set `family:` returns.
- **Ancestors** are a binary pedigree, bounded by generation, and need only a fixed grid.

`direction` and the root live in the URL, so Back works — the project's standing rule.

The layout is a **pure function** in `web/src/lib/familyLayout.ts`: nodes in, coordinates
out, no DOM. That makes it unit-testable in vitest, which is where the real risk lives;
the SVG renderer on top of it is thin. Verify the rendered result visually with a
standalone harness driven by `agent-browser` rather than trusting jsdom.

### 8.3 `AddRelationModal`

Role picker, subject search (diacritics-insensitive, reusing `SearchSubjects`), and — the
important part — *not found → create* inline, posting `new_subject`.

### 8.4 Plumbing

A new `web/src/services/family.ts` (`people.ts` is already 21 KB), types mirroring the Go
structs field for field, and cs + en i18n.

## 9. Metadata sidecars

Relationships are subject-level; sidecars are per-photo. They therefore do not belong in
a photo's sidecar, and leaving it there would put a hole in the "yours to keep" promise:
lose the database, lose the tree.

The proposal is a library-level `families.yaml` at the store root, written by a small
job whenever a relation changes, with its own `Kind` in `internal/storekeys` so backup,
migration and wipe cannot forget it. This is the one item in the plan that can be dropped
without making the rest incoherent.

## 10. The real risk is data entry, not drawing

The tree can be drawn in an afternoon. Recording relationships for 118 people is several
evenings of clicking, and if that part is awkward the feature stays empty and the whole
thing was wasted. Hence the emphasis on inline subject creation, keyboard operation and
searching without diacritics.

A later helper could propose relationships from the surname clusters and the `st.`/`ml.`
suffixes — the data practically dictates them — but it is not part of this design.

## 11. Out of scope

- **GEDCOM import/export.** Deliberately not built, though the model is GEDCOM-shaped
  precisely so that it stays possible.
- **Renaming subjects to drop `st.` / `ml.` / `st.st.`** Once relationships exist the
  suffixes are redundant, but editing the library's content is a decision for the owner,
  not an autonomous change.
- **Using relationships as a face-matching signal** ("this is probably his mother").
  Attractive, but worthless until the relationships exist; a separate feature afterwards.
- **Places of birth and death, occupations, sources, citations.** This is a photo
  library, not a genealogy database.

## 12. Testing

Unit:

- `familyLayout.ts` — coordinates for a straight line, a wide sibship, a remarriage, a
  collapsed branch.
- Pair normalisation and cycle detection in `internal/family`.

Integration, against the real test database:

- A seeded three-generation family, including a **cousin marriage**, to prove `UNION`
  dedupes and the diamond does not double-count.
- The one-family-per-child index refusing a second parentage.
- The cycle check refusing an ancestor as a child.
- `family:` compiling to the expected photo set, including `family:me`.
- The audit row landing inside the mutation's transaction.

Two recorded gotchas apply to running these: integration packages need `-p 1`, because
they share one test database and deadlock in parallel; and `go vet -tags integration ./...`
before the gate, because an interface change can compile green under `make check` and fail
only under the integration tag.

## 13. Task breakdown

Ordered; usable from step 4 onward.

1. Migration `0073`, `internal/family` (model, store, recursive walks, cycle check),
   reset classification.
2. `internal/familyapi` — routes, RBAC, audit actions, mounted via the `peopleapi`
   options helper.
3. `FamilyStrip` on the subject page, `AddRelationModal` with inline subject creation,
   `services/family.ts`, i18n.
4. `familyLayout.ts` (pure, unit-tested) and the descendants tree page with pan and zoom.
5. The ancestors pedigree and the direction switch in the URL.
6. The `family:` query key, `familyCond`, `family:me`, `docs/API.md`.
7. `kukatko ctl family` — read relations, add and remove, `--dry-run` where destructive.
8. `families.yaml` library sidecar, `storekeys` kind, `README.md` and
   `docs/ARCHITECTURE.md` §8.1.

## 14. Open questions

1. Should `family:` include the root's **ancestors** by default, or only descendants and
   their partners as specified here? The decision was descendants; a `family:x+up` form
   could be added later if the omission is felt.
2. Should a subject with no photos be hidden from `/people` behind a filter, or listed
   with a placeholder tile? This design assumes listed — it is the simpler promise — but
   with 118 people and possibly forty photo-less ancestors it may crowd the index.
3. Is `families.yaml` (§9) worth its job and its store prefix, or is the tree acceptably
   covered by the nightly `pg_dump`?
