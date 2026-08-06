# MIGRATION_PLAN — cutover from PhotoPrism + photo-sorter

> ## ⛳ Closed — 2026-08-05
>
> **The cutover happened and this runbook is history.** `kukatko import verify` reported
> COMPLETE: photos, faces, albums, labels and people all reconciled without a gap. Kukátko is
> the primary photo app.
>
> On **2026-08-06** the code that carried the migration was **removed** — `internal/ppimport`,
> `internal/psimport`, `internal/psfeedsimport`, `internal/importverify`, the
> `internal/photoprism`/`internal/photosorter`/`internal/psfeeds` clients, the
> `import photoprism` / `import photosorter-feeds` / `migrate photosorter` / `import verify`
> commands, the `POST /api/v1/import/*` triggers, `GET /api/v1/import/verify` and the
> `import.photoprism.*`/`import.photosorter.*` configuration. Roughly 20 000 lines that could
> never run again. Every command and endpoint this document mentions is therefore **gone**;
> the document is kept as the record of what was done, not as instructions.
>
> What stayed: the `photos.photoprism_uid`/`photoprism_file_hash`/`photosorter_uid` columns and
> the `photoprism_aliases` table (provenance, `uid:pt…` search, every metadata sidecar), and the
> migration's own rows in `import_runs`/`import_failures`.

The concrete runbook for making Kukátko the primary photo app. This is the
*executable finish-line*: when every box here is checked, PhotoPrism can go
read-only.

It complements [`MIGRATION_AUDIT.md`](MIGRATION_AUDIT.md) (field-level
mapping) with the **verified production topology** and the **wipe + full reimport**
procedure.

> **Unblocked 2026-08-01.** [`READINESS_AUDIT.md`](READINESS_AUDIT.md) §2 found three
> defects that made this runbook unsafe to execute — the import and its verifier both
> stopped after the first page of the source, the verifier expected albums the import
> skips, and the vector counters read as complete on an empty catalogue. All three are
> fixed and the fix is measured: `import verify` now enumerates 20 660 source photos
> where it saw 994. The audit's §4 also records the risks accepted for this cutover —
> no S3 backup, no restore rehearsal — which is why **phase 5 below is waived** and the
> untouched PhotoPrism + photo-sorter libraries are the only rollback.

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

(Backup→restore rehearsal was an open cutover gate; it is **waived** — see phase 5.)

## Runbook — wipe + full reimport

### Phase 0 — before the wipe
1. Land the three prerequisite tasks above (`make check` + `make dev` green).
2. Confirm the `psat_` token authenticates: `GET /api/v1/stats` returns `200` with the
   ~20k totals.

### Phase 1 — wipe Kukátko's data
3. Take a throwaway note of current counts (for comparison). There is no curation worth
   keeping in Kukátko (this is a fresh migration), so a full reset is safe.
   `kukatko maintenance reset` prints exactly those counts and deletes nothing, so this
   step *is* the dry run.
4. Stop the server, then reset Kukátko with **`kukatko maintenance reset --execute
   --orphan-sweep`** (`internal/reset`, documented in
   [`OPERATIONS.md`](OPERATIONS.md#kukatko-maintenance-reset--the-guarded-library-wipe)).
   It truncates the whole catalogue — photos, files, vectors, faces, albums, labels,
   places, edits, import history, the job queue and the per-user curation — and deletes the
   originals, thumbnails and sidecars from the configured store, leaving the accounts,
   the announcement, the audit trail and the migration history intact. It refuses to run
   against a database the config does not name, refuses without the target database's name
   typed in full, refuses a non-interactive run without `--force`, and never deletes a key
   outside Kukátko's own prefixes. It is idempotent, so an interrupted wipe is simply re-run.
   **Do NOT touch PhotoPrism or photo-sorter** — they are the source and the rollback.

### Phase 2 — import photos from PhotoPrism
5. Run the full PhotoPrism import; drain the job queue. Every photo + all its files
   (incl. the 12 RAW) lands, deduped on SHA256.
6. **Geocode credits**: ~29 % of the library carries GPS (≈6k photos), and every reverse
   geocode is one metered mapy.com credit. `maps.geocode_budget` (default **1000 / 24h**)
   caps the spend, so the `places` queue drains over roughly a week instead of in one
   pass; an exhausted budget defers the jobs until it refills, nothing fails. Raise it if
   the run must finish sooner, and watch `GET /system/status` → `geocode` (or
   `kukatko_geocode_credits_spent_total`) while it runs — see
   [`OPERATIONS.md`](OPERATIONS.md).

### Phase 3 — enrich with photo-sorter vectors
7. Run the photo-sorter feeds import (`1191a2cc`): embeddings + faces + markers/subjects
   copied 1:1 and attached by `photoprism_uid`. No GPU box needed.

### Phase 4 — verify completeness
8. `kukatko import verify` (`3f8f3144`): reconcile PhotoPrism photo/file counts +
   photo-sorter `/stats` (embeddings/faces) against Kukátko. Resolve every listed
   missing item (missing photo, missing RAW sibling, missing embedding/faces) until the
   report is clean. Cross-check `maintenance scan`.

   **What "clean" means for albums.** The check reconciles only the album types the
   importer maps (`ppimport.DefaultAlbumTypes` = `album, folder, moment, state`).
   PhotoPrism's auto-generated `month` albums — 321 of them, one per calendar month,
   covered by Kukátko's timeline — are reported as `structure.albums.skipped_by_design_count`
   (with `skipped_types`), never as missing. So `structure.albums.source_count` is the
   source album catalogue **minus** those, and `758 = source_count + skipped_by_design_count`
   is the sanity check against PhotoPrism's own album total; anything still listed under
   `missing` is a real gap to resolve. Before this the verifier defaulted to all five
   types and the report listed all 321 monthly albums as missing forever, so "clean" was
   unreachable by construction.

### Phase 5 — ~~prove backup + restore~~ **WAIVED 2026-08-01**
9. Dropped by an explicit owner decision: Kukátko gets **no S3 backup**, and the restore
   path is **not rehearsed**. Recorded in [`READINESS_AUDIT.md`](READINESS_AUDIT.md) §4
   under *Accepted risks*, not as an oversight.

   **What this makes load-bearing.** With no backup of its own, the untouched PhotoPrism
   and photo-sorter libraries are Kukátko's *only* safety net, so the rollback in phase 6
   stops being prudence and becomes a hard operating rule: they cannot be retired,
   `storagemigrate DeleteLocal` stays off, and trash `retention` stays off. Anything that
   would delete an original from a source library requires revisiting the backup decision
   first.

### Phase 6 — cutover
11. Side-by-side sample compare (counts, a few albums/people) PhotoPrism vs Kukátko.
12. Make Kukátko primary; set PhotoPrism read-only.
13. **Keep the PhotoPrism + photo-sorter libraries intact and read-only** — they are the
    true rollback (sources untouched, no runtime dependency). Keep `storagemigrate
    DeleteLocal` and trash `retention` **off** — permanently, not "until backups are
    proven", because phase 5 is waived and there will be no backup to prove.

**Point of no return:** switching Kukátko to primary is safe once phases 1–4 pass. With
phase 5 waived there is no restore to demonstrate, so *retiring* PhotoPrism/photo-sorter
is never demonstrably safe — the point of no return is the moment either source library
is deleted or written to, and that moment should not come.
