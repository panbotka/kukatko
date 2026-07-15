# Find a person among untagged photos — backend

Kukátko can tell you which photos a person **is** tagged in (`GET /photos?person=<uid>`). It cannot answer
the far more useful question: **"where else does Tomáš appear, that nobody has named yet?"** Today the only
way to reach an unnamed face of a well-known person is to open that exact photo by hand. Clustering does
not help — it only surfaces groups of ≥2 mutually-similar unassigned faces, so a lone unnamed face of a
person you have named a hundred times is invisible.

Build the search. The UI is a separate task.

## Algorithm

Given a subject and a threshold:

1. **Source exemplars** — load every face already assigned to the subject (`ListFacesBySubject` in
   `internal/vectors`). Deduplicate to **one exemplar per source photo**: a photo containing three faces of
   the same person must not get three votes.
2. **kNN per exemplar** — for each exemplar, run the **unassigned-only** face candidate search from
   `internal/vectors` (the one restricted to `subject_uid IS NULL`, added by the foundation task),
   with the caller's max cosine distance. Run these concurrently, bounded — do not fan out unboundedly over
   a person with 500 photos.
3. **Union with voting** — merge candidates keyed by face. For each candidate track `match_count` (how many
   distinct source exemplars returned it) and `distance` (the **minimum** across votes).
4. **The vote rule** — a candidate must be voted for by at least `min_match_count` exemplars. This is the
   single most important quality lever; without it a multi-exemplar union is a firehose. Scale it with both
   the source-set size and the threshold, and **clamp it to 1..5**. Return the computed `min_match_count`
   in the response — the UI shows it, and an unexplained filter is a black box.
5. **Rejections** — drop candidates the user has already rejected for this subject, and apply the
   **negative-exemplar rule** from `internal/feedback` (a candidate closer to a face rejected for this
   subject than to any face accepted for it is dropped).
6. **Noise floor** — drop faces below a minimum size (both an absolute pixel width and a relative width);
   a 20-pixel face in a crowd is not reviewable. Reuse `faces.min_face_size` if it fits, otherwise add
   config keys.
7. **Sort** by distance ascending, truncate to the limit.

## API

`POST /api/v1/subjects/{uid}/candidates` (RequireWrite), body: `threshold` (max cosine distance),
`limit` (0 = all).

Response per candidate: the photo, the face (index + bbox, **both pixel and display-relative, honouring EXIF
orientation** — the UI draws the box on a thumbnail), the distance, the `match_count`, and an **action**
classifying what confirming would do:

- the face has no marker yet → `create_marker`
- the face has a marker but no subject → `assign_person`
- it already belongs to this subject → `already_done` (should be rare — it means a stale cache)

Plus a summary: source photo count, source face count, `min_match_count`, and counts per action.

**Confirming reuses the existing assign state machine** (`POST /photos/{uid}/faces/assign` in
`internal/facematch`) — do not build a second write path. Verify it covers `create_marker` and
`assign_person`; extend it only if it genuinely does not.

## Thresholds and config

Defaults as config keys (add to the `Config` struct, `setDefaults`, `config.example.yaml`, the tests **and**
`docs/OPERATIONS.md` — all at once, as the project requires). Suggested starting points, taken from what
works in photo-sorter: default max distance **0.5**, search limit per exemplar **1000**. The UI speaks
percent and converts; the API speaks cosine distance.

## Edge cases

- A subject with **no faces yet**: return an empty result with a clear reason, not an error — the UI must be
  able to say "name a few faces first".
- A subject whose faces have **no embeddings** (the sidecar was offline when they were detected): report the
  count so the UI can surface it, rather than silently returning nothing.
- The embeddings sidecar being offline must **not** matter here — this reads vectors already in Postgres.
- A large library: the query must not load every face into Go memory. Keep the work in SQL and bounded.

## Verification

- Integration tests against the real test DB with seeded face vectors: a planted unassigned face of the
  subject is found; a face assigned to a **different** person is never returned; a rejected face is not
  returned; a candidate that trips the negative-exemplar rule is not returned; the vote rule filters a
  candidate seen by only one exemplar when `min_match_count` is 2; `already_done` is classified correctly.
- A test that a subject with zero faces returns an empty, non-error result.
- `make check` must pass.
- Document: `docs/API.md` (the endpoint), `docs/PACKAGES.md` (the package that owns this), `docs/OPERATIONS.md`
  (the new config keys), and one line in the `CLAUDE.md` package map if a new package appears.

## Out of scope

- Any frontend (separate task).
- The all-people sweep (separate task) — but design the service so the sweep can call it per subject without
  re-implementing anything.
- Album/label similarity (separate task).