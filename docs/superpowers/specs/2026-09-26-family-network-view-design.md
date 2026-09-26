# The family tree as a reachable network — design

**Status:** approved 2026-09-26 — awaiting implementation. No code has been written for
this document.

The tree page at `/people/{uid}/tree` asks the database one question at a time: *who is
above this person*, or *who is below them*. Both answers are drawn well, and neither is
the question a reader actually has, which is *who is this person's family*.

Measured on production (0.25.0, `52e5de4`), the gap is concrete. The library holds 130
subjects, 7 rows in `subject_families`, 7 in `subject_family_children`, and 11 people who
appear in any family at all. For the subject `sudh96iqevipv1v2cjfn85a26q` — Tomáš Kozák —
the family links reach **five** people:

- `Ludmila Kozáková` + `Aleš Kozák` → `Tomáš Kozák`
- a family of kind `unknown` with **no partners at all** → `Ludmila Kozáková`,
  `Dagmar Andrlíková` (the parentless sibling group migration 0076 exists for)
- `Dagmar Andrlíková` → `Petra Houdková`

The page can show **three** of those five. `Dagmar` (an aunt) and `Petra` (a cousin) appear
in neither direction, because the path to them runs sideways through a sibling group. No
setting reveals them; the payload does not contain them.

This document replaces the two directional views with one view of everything the family
links reach.

## 1. What the page does today

- `GET /subjects/{uid}/tree` takes `direction` (`descendants` — the default — or
  `ancestors`) and `generations`, and answers with **one** walk. There is no combined mode
  at any layer.
- `internal/family/walk.go` holds the two walks. `MaxDepth = 20` bounds both; its own
  comment is explicit that it is a **cycle guard, not a product limit**. The descendant
  walk is therefore already effectively unbounded, and only the pedigree is user-limited.
- `Member.depth` is unsigned and counts generations *away* from the root, so an ancestor
  two up and a grandchild two down are indistinguishable.
- `treeFamiliesSQL` filters a family's `child_uids` to `= ANY($1)`, the walked set, with a
  deliberate reason: "the renderer must not be handed an edge to a person it was given no
  node for, which is exactly what a pedigree's uncles would be." That filter is where the
  aunts and cousins are lost.
- The frontend mounts one of two renderers. `FamilyTreeCanvas` draws descendants as a
  Reingold–Tilford tidy tree over family boxes, growing downward from `y = 0` with contour
  packing and collapsible branches. `FamilyPedigreeCanvas` draws ancestors as a binary
  Ahnentafel pedigree, growing upward from the bottom, with no folds.
  `MAX_PEDIGREE_GENERATIONS = 6` clamps the pedigree client-side because a *full* binary
  pedigree doubles every generation.
- A two-link button group switches direction; a select chooses generations, and is rendered
  only for ancestors.
- `family.Store.Tree` has exactly one caller (`internal/familyapi/handlers.go`) and
  `fetchTree` exactly one (`FamilyTreePage`). `kukatko ctl` does not use the endpoint. The
  directional walks exist solely to serve this page.

## 2. The decision

The page always shows **every person reachable from the root through family links** — up,
down and sideways, in-laws' relatives included — with no direction switch and no
generation control. "Everything available" is the whole connected component of the
person-and-family graph.

The design spec of 2026-09-12 argued against a component in §2 decision 3: "in a village
the families eventually marry into each other, and a component-based family would one day
swallow the whole of Veselice and stop filtering anything." That argument is sound and it
is about the **`family:` search filter**, whose job is to narrow a photo listing. It does
not transfer to a page whose job is to show a family: a reader looking at a tree wants the
cousins, and a drawing that grows is a drawing you pan. The concern is answered in §3 with
a measured budget rather than with a missing direction. §14 open question 1 of that spec
already recorded the omission as provisional.

## 3. The walk

The graph is **bipartite**: persons and families. A person is joined to a family by being
one of its partners, or by being one of its children. Both kinds of edge are walked in both
directions, breadth-first from the root, until nothing new is reached.

Because the walk closes over the component, **`treeFamiliesSQL`'s `child_uids` filter
becomes a no-op**: every child of every reached family is itself reached. The deliberate
omission of §1 disappears without a rule of its own. That is the whole fix for Dagmar and
Petra.

### 3.1 Generation, signed

`Member.depth` is replaced for this walk by a **signed `generation`**: the root is 0, a
parent is −1, a child is +1, a partner shares the generation of the person they married.
Propagation follows the bipartite edges — a family takes the generation of its partners,
its children take one more.

Assignment is breadth-first from the root and **the first write wins**, so the nearest
relationship names the generation. This matters because a component can be inconsistent:
marry a second cousin and someone is reachable both as a cousin and as a parent-in-law. A
first-write-wins BFS resolves that deterministically, and the same person is never drawn
twice — the existing `UNION` plus `MIN`-depth collapse already establishes that discipline.

### 3.2 The budget

The walk takes a **cap on the number of people** (400 is the proposed value) and, because
it is breadth-first, keeps the nearest relatives when it runs out. The response reports
`truncated`, and the page says so plainly — "showing the 400 nearest of N". On today's data
it cannot fire: the largest component is five people.

This is the §2 concern made measurable. `MaxDepth = 20` stays exactly what it is, a cycle
guard.

## 4. The API contract

`GET /subjects/{uid}/tree` loses `direction` and `generations` and answers with the
network. The response keeps `root`, `members` and `families`, with two changes:

- a member carries `generation` (signed) instead of `depth`
- the payload carries `truncated` (bool)

The `partner` flag on a member loses its meaning — in a network there is no line to be
married *into* — and goes.

Nothing outside the binary consumes this endpoint, and the frontend ships embedded in the
same binary, so there is no compatibility window to manage. The step-by-step delivery in
§10 nonetheless keeps every intermediate state working.

## 5. The layout

Neither existing layout survives the change, for structural reasons rather than taste.
`layoutAncestors` is an Ahnentafel pedigree, and a network has no fixed binary slots.
`layoutDescendants` is Reingold–Tilford, which assumes a **rooted tree**: a component has
cycles, and its `y = depth * LEVEL_STRIDE` cannot express a negative generation.

One new pure function, `layoutNetwork`, replaces both — a layered (Sugiyama-style) layout:

1. **Layer assignment** is free: the layer is the generation the server computed.
   `y = (generation − minGeneration) * LEVEL_STRIDE`.
2. **Ordering within a layer** treats a couple as one box, exactly as today's `buildNodes`
   does, seeded in breadth-first order from the root, then improved by a few barycentre
   sweeps — a box's target is the mean position of its partners, its children and its
   parents.
3. **Coordinates** are packed left to right with the existing constants
   (`PERSON_WIDTH`, `SIBLING_GAP`, `COUPLE_GAP`), then nudged toward the barycentre by
   priority.

Everything else is reused unchanged: `TreeStage` and its pan, zoom, fit and
centre-on-root; `TreePersonCard`; `TreeBox`; the square elbow `edgePath`; the page's
`orderChildren` sort by birth year then Czech-collated name. The layout stays a pure
function with no DOM, which is where the 2026-09-12 spec located the real risk and where
the vitest coverage belongs.

The header summary becomes people plus `max(generation) − min(generation) + 1` generations.

## 6. What is removed, and the one affordance that moves

Removed: `layoutAncestors`, `FamilyPedigreeCanvas`, `FamilyTreeCanvas`, the direction
button group, the generations select, `MAX_PEDIGREE_GENERATIONS` and its clamp, and — once
nothing calls them — `family.Store.Ancestors`, `family.Store.Descendants` and their SQL.
Two renderers becoming one is a net reduction in code.

One real loss has to be paid for. The pedigree draws **empty slots**: a `PedigreeGap` with
a dashed plate, a `?` for a reader and a `+` for an editor that opens `AddRelationModal` on
the child, because a parent is recorded on their child. A layered layout has no empty
binary slots, so the affordance cannot survive in that form. It **moves onto the person
card**: a `+` on a card opens the same `AddRelationModal`, so filling in a missing parent
from the drawing is kept, in a place that works for every direction rather than only
upward. `FamilyStrip` on the subject page remains the other way in.

## 7. What deliberately does not change

- **The `family:` search filter.** It has its own recursive SQL in
  `internal/photos/store_query.go` with its own `familyDepthLimit`, touches nothing in
  `internal/family`, and keeps meaning *the descendants of a root, plus their partners*.
  After this change the tree page and `family:` answer **different questions on purpose** —
  the page shows a family, the filter narrows a listing. A future reader will be tempted to
  "fix" the inconsistency; this paragraph is why they should not.
- **`GET /subjects/{uid}/relations`** and the family strip: unchanged.
- **The write surface** — adding, editing and deleting relations — is untouched. This
  change is read-only.
- **`MaxDepth = 20`** keeps its job as a cycle guard.
- **Roles.** The endpoint stays `RequireAuth`; the `+` on a card follows the same
  permission the pedigree's `+` follows today.

## 8. Testing

- `internal/family`: integration tests for the component walk over a seeded graph that
  reproduces the production shape — a couple with a child, a **parentless sibling group**,
  and the sibling's own child — asserting all five people come back with the right signed
  generations. Plus a cycle (a cousin marriage) asserting each person appears once and the
  nearest relationship wins, and a seeded graph past the cap asserting `truncated` and that
  the nearest relatives are the ones kept.
- `internal/familyapi`: the response shape, and that the removed parameters are gone.
- `web`: vitest over `layoutNetwork` as a pure function — layer assignment from signed
  generations, a couple as one box, no overlapping boxes within a layer, a cycle laid out
  without duplication, and the empty-graph case. Then the page: the controls are gone, the
  drawing mounts once, a truncated response shows the banner.

## 9. Documentation routing

- `docs/API.md` — the endpoint's parameters and response shape.
- `docs/PACKAGES.md` — `internal/family` (the walk), `internal/familyapi`.
- `docs/FRONTEND.md` — `layoutNetwork`, the single canvas, the removed components and
  controls, the `+` on the card.
- `docs/ARCHITECTURE.md` — the tree view decision, which §8.2 of the 2026-09-12 spec set
  and this document revises.
- `README.md` — the tree is user-visible.
- `CLAUDE.md` — only if a package is added or removed.

## 10. Delivery in three steps

Ordered so that no intermediate state leaves the page broken.

1. **The network walk, alongside the old ones.** The component SQL, signed generations, the
   cap and `truncated`, reachable as a third `direction=network` while `depth` and both
   existing directions stay. The page is untouched and keeps working; the new walk is
   covered by tests.
2. **The page.** `layoutNetwork`, one canvas, the controls removed, both old canvases and
   `layoutAncestors` deleted, the `+` moved onto the card, i18n and vitest. The feature is
   complete here.
3. **The cleanup.** `direction` and `generations` leave the endpoint,
   `family.Store.Ancestors` and `Descendants` and their SQL go, docs follow. No dead code
   left behind.

## 11. Out of scope

- **Changing `family:`** — see §7.
- **A list or outline fallback** for a very large component. The cap plus pan and zoom is
  the answer until a real component gets big enough to argue otherwise.
- **Collapsible branches.** The descendants tree has folds today; a network has no single
  direction to fold away, and with five-person components there is nothing to hide. If a
  filled-in library makes the drawing unwieldy, folding a family box is the natural place
  to start, and the layout being pure keeps that cheap to add.
- **Layout libraries.** The 2026-09-12 spec chose a hand-written pure layout over d3, dagre
  or elk deliberately; that choice stands.
