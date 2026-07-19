# MIGRATION_PLAN — cutover from PhotoPrism + photo-sorter

The concrete runbook for making Kukátko the primary photo app. This is the
*executable finish-line*: when every box here is checked, PhotoPrism can go
read-only. It complements [`MIGRATION_AUDIT.md`](MIGRATION_AUDIT.md) (field-level
mapping) with the **verified production topology** and the **wipe + full reimport**
procedure.

## Verified production topology (measured 2026-07-19, live)

The docs' field mapping assumed photo-sorter holds photos natively. **It does not in
production.** Verified against the running services:

- **PhotoPrism** (`https://fotky.kotrzina.cz`) is the source of the photos: **~20,310
  photos** + originals + files + albums + labels + metadata. Photo UIDs are
  PhotoPrism UIDs.
- **photo-sorter** (`https://sorter.kotrzina.cz`) is a **vector/faces layer on top of
  PhotoPrism** — its native `/api/v1/photos` returns `total:0`. It holds only
  **~20,687 embeddings + ~112,806 faces**, keyed by the PhotoPrism photo UID, exposed
  via read-only migration feeds `GET /api/v1/embeddings` (CLIP `ViT-L-14`, dim 768)
  and `GET /api/v1/faces` (`buffalo_l`, dim 512, incl. marker/subject). A feed UID
  resolves `200` in PhotoPrism → **same UID space**.

**So the migration is: photos + files from PhotoPrism, enriched with photo-sorter's
1:1 vectors, joined by `photoprism_uid`.** Importing the vectors 1:1 means **no GPU-box
recompute** for the whole library — which removes the biggest daily-driver blocker.

### Files at stake (measured in PhotoPrism)

- `type:raw` = **12** (JPEG primary + a non-primary RAW sibling).
- `type:live` = **0**. `type:video` = **6** (the video *is* the primary → already
  imported).

The whole "don't lose RAW/live" requirement reduces to **12 RAW siblings** that
`ppimport` currently drops (it imports only `PrimaryFile()`). Fix is on the
PhotoPrism/`ppimport` path, not photo-sorter.

## Auth / config

- photo-sorter API needs a `psat_`-prefixed **read-only** bearer token, minted only via
  `photo-sorter api-tokens create <name>` on the sorter host. Current token lives in
  `.secrets/db.env` (`KUKATKO_IMPORT_PHOTOSORTER_TOKEN=psat_...`) and 1Password (vault
  "Pan Botka", "Kukátko – photo-sorter migration token").
- `import.photosorter.base_url` / `.token` are not yet wired in `config.go` (only
  `.dsn`) — the feeds-importer task wires them. The old direct-DB `psimport` path is
  irrelevant for this deployment (sorter has no native photos; its disk is remote).

## Prerequisite code tasks (Botka, kukatko)

- `1191a2cc` — **photo-sorter feeds importer**: page `/embeddings` + `/faces`, store
  1:1, attach to the PhotoPrism-imported photo by `photoprism_uid` (no recompute).
- `640df480` — **ppimport RAW siblings**: import non-primary PhotoPrism files (the 12
  RAW) as a stack.
- `3f8f3144` — **completeness verify tool** (`kukatko import verify`) + persist
  per-photo/per-file import failures so a run with failures is not `done`.

(Backup→restore rehearsal is still an open cutover gate — see the checklist.)

## Runbook — wipe + full reimport

### Phase 0 — before the wipe
1. Land the three prerequisite tasks above (`make check` + `make dev` green).
2. Confirm the `psat_` token authenticates: `GET /api/v1/stats` returns `200` with the
   ~20k totals.

### Phase 1 — wipe Kukátko's data
3. Take a throwaway note of current counts (for comparison). There is no curation worth
   keeping in Kukátko (this is a fresh migration), so a full reset is safe.
4. Reset Kukátko: truncate the catalog + vectors + faces + originals store (a
   maintenance/CLI reset, or drop & re-migrate the schema). **Do NOT touch PhotoPrism or
   photo-sorter** — they are the source and the rollback.

### Phase 2 — import photos from PhotoPrism
5. Run the full PhotoPrism import; drain the job queue. Every photo + all its files
   (incl. the 12 RAW) lands, deduped on SHA256.

### Phase 3 — enrich with photo-sorter vectors
6. Run the photo-sorter feeds import (`1191a2cc`): embeddings + faces + markers/subjects
   copied 1:1 and attached by `photoprism_uid`. No GPU box needed.

### Phase 4 — verify completeness
7. `kukatko import verify` (`3f8f3144`): reconcile PhotoPrism photo/file counts +
   photo-sorter `/stats` (embeddings/faces) against Kukátko. Resolve every listed
   missing item (missing photo, missing RAW sibling, missing embedding/faces) until the
   report is clean. Cross-check `maintenance scan`.

### Phase 5 — prove backup + restore (point-of-no-return gate)
8. Configure `backup.s3.*` to a second, independent bucket + a non-empty
   `backup.schedule`. Run `kukatko backup`.
9. **Rehearse restore end-to-end on a throwaway DB**: `restore db` → `restore
   originals` → `restore verify` = `Consistent`. Do not skip — this path is currently
   untested.

### Phase 6 — cutover
10. Side-by-side sample compare (counts, a few albums/people) PhotoPrism vs Kukátko.
11. Make Kukátko primary; set PhotoPrism read-only.
12. **Keep the PhotoPrism + photo-sorter libraries intact and read-only** — they are the
    true rollback (sources untouched, no runtime dependency). Keep `storagemigrate
    DeleteLocal` and trash `retention` **off** until backups are proven.

**Point of no return:** switching Kukátko to primary is safe once Phases 1–4 pass;
*retiring* PhotoPrism/photo-sorter is safe only after Phase 5 (a real restore) is
demonstrated. Until then they stay as the rollback.
