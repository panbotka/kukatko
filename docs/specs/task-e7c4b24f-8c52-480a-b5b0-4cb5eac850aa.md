# Foundation for the review features: rejections + searching only unassigned faces

Several upcoming features (find a person among untagged photos, the recognition sweep, expanding an
album/label by similarity, and the review game) all need two things Kukátko does not have yet. Build
them once, properly, so the features on top stay thin.

**Why this matters:** photo-sorter, which these features come from, never persists a rejection. Say "no,
that is not Tomáš" and the very same face is offered again on the next search, forever. The work never
shrinks. Kukátko must not repeat that.

## Part 1 — Rejections

A new package `internal/feedback` with a `Store` over pgx, plus a migration (use the **next free
migration number** — check the directory, do not assume one).

Two kinds of rejection:

- **Face rejection** — "this face is NOT this person". Keyed by the face (the same identity Kukátko
  already uses for a face: `photo_uid` + `face_index`, see `internal/facematch` and the `faces` table)
  plus `subject_uid`.
- **Label rejection** — "this photo should NOT have this label". Keyed by `photo_uid` + `label_uid`.

Both rows carry who rejected it and when. Unique constraint on the natural key — rejecting twice is a
no-op, not an error. FKs cascade so deleting a subject / label / photo cleans up after itself.

Store API (at minimum): reject a face, reject a photo-label, un-reject both (a user must be able to take
it back), check whether a pair is rejected, and — the important one — **bulk lookups the search paths can
use as an exclusion filter without an N+1**: give me all face rejections for subject X, all label
rejections for label Y.

**Rejecting must be idempotent and must never mutate the underlying data** — a rejection records an
opinion; it does not delete a face, unassign a marker, or remove a label.

### API

New endpoints in a new `internal/feedbackapi` (RequireWrite, following the existing package conventions):

- `POST /api/v1/feedback/face-rejections` — body identifies the face and the subject
- `DELETE /api/v1/feedback/face-rejections` — take it back
- `POST /api/v1/feedback/label-rejections` — body identifies the photo and the label
- `DELETE /api/v1/feedback/label-rejections` — take it back

Every write goes through `internal/audit` **in the same transaction as the mutation**, as the project
requires.

## Part 2 — Vector search over unassigned faces only

`internal/vectors` today has `FindSimilarFaceCandidates(vec, limit, maxDistance)`, which scans **all**
faces. There is no way to ask "find me the nearest faces that nobody has named yet", which is exactly the
question every one of these features asks. Add it:

- A search that returns face candidates restricted to **`subject_uid IS NULL`** (unassigned), with the
  same shape as the existing candidate search (bbox, marker, photo). Keep the existing HNSW discipline:
  read-only transaction, `SET LOCAL hnsw.ef_search`, cosine distance (`<=>`), the existing limit caps.
- An **exclusion set** parameter, so a caller can pass the faces already rejected for the subject being
  searched and have them filtered **in SQL**, not in Go after the fact. Filtering after the HNSW limit
  would silently shrink the result set, which is a real bug — a candidate list of 50 that loses 30 to
  rejections must still come back with 50 good candidates. Over-fetch and filter, or filter in the query;
  either is fine, but the caller must get the number of candidates it asked for.

## Part 3 — The negative-exemplar rule

This is what makes a rejection *teach* something rather than just hide one row. Implement it once here, as
a reusable scoring helper, so the feature packages share it:

> For a candidate face C and a subject S: compute C's distance to S's **nearest accepted face** (the faces
> already assigned to S) and to S's **nearest rejected face** (the faces rejected for S). If C is closer to
> a rejected face than to an accepted one, C is a **negative** — drop it from the results.

The same rule applies to labels using the CLIP image embeddings: a photo closer to a photo rejected for
label L than to any photo carrying L is dropped as a candidate for L.

Rationale: this is a nearest-neighbour margin test, it needs no training, it is cheap (the vectors are
already in the query result), and it is trivially explainable in the UI ("looks more like something you
already said no to"). Do not build anything heavier — no model fitting, no learned weights.

When a subject or label has **no rejections at all**, the rule is a no-op and must cost nothing.

## Verification

- Integration tests against the real test DB (`KUKATKO_TEST_DATABASE_URL` from `.secrets/db.env`, use the
  `_HOST`/localhost DSN from the Pi host): the migration applies; rejecting is idempotent; un-rejecting
  works; the bulk lookups return what they should; the FK cascades fire.
- Vector tests: the unassigned-only search never returns an assigned face; the exclusion set is honoured
  **and the caller still gets `limit` results** when rejections eat into the candidate pool (assert this
  explicitly — it is the easy bug).
- Unit tests for the negative-exemplar rule: no rejections → no-op; a candidate nearer a rejected exemplar
  is dropped; a candidate nearer an accepted exemplar survives; ties are resolved deterministically.
- `make check` must pass.
- Document: `docs/PACKAGES.md` (+ one line each in the `## Package map` in `CLAUDE.md` for
  `internal/feedback` and `internal/feedbackapi`), `docs/API.md` (the four endpoints),
  `docs/ARCHITECTURE.md` (the two new tables and the negative-exemplar rule — this is a design decision the
  next iteration must not undo).

## Out of scope

- Any UI. No page, no button — the consumers come in later tasks.
- Changing how faces are detected, clustered or assigned. `internal/facematch`, `internal/cluster` and the
  existing assign state machine stay as they are; they will start *reading* rejections in later tasks.