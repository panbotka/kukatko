# Maintenance scan on an object-store instance reports 0 originals and calls it consistent

Verified on the box staging (commit 68e805e, 2026-09-06), which runs the same `storage.backend: r2` production runs (MinIO there, Cloudflare R2 in production). "Spustit kontrolu" on Údržba knihovny reports:

> 398 fotek · 398 souborů v katalogu · 0 originálů na disku
> Čísla by si měla zhruba odpovídat. Větší rozdíl mezi katalogem a diskem znamená, že se něco rozešlo …
> Katalog a soubory si odpovídají, není co opravovat.

Zero originals next to a hint that the numbers should match, followed by a green verdict, is contradictory. Production shows the same shape. Screenshot: `/mnt/nas-botka/kukatko-stage-test/65-maintenance-scan.png`.

## Root cause (verified, do not re-derive)

- `internal/maintenance` gets its file inventory from `DiskScanner.List` (`maintenance.go` ≈ lines 106–110 and 416), which "walks the originals root" — the local filesystem. With the r2 backend the root holds nothing, so the orphan half of the scan sees an empty store and the summary's `originals_on_disk` is 0.
- The missing-originals half asks `OriginalStore` per photo, which does reach the bucket, so "missing = 0" is honest — only the inventory number and its wording are wrong.
- Listing the store is already possible: `internal/storage/r2.go` has `(*R2).Keys(ctx, yield)` (≈ line 423) and `fs.go` has `(*FS).Keys`; see `docs/PACKAGES.md` ("`FS.Keys` walks the root and skips the `.tmp` staging dir; `R2.Keys` lists the bucket recursively").
- The frontend strings live in `web/src/i18n/locales/cs/common.json` (≈ lines 2712–2736: `summary`, `summaryHint`, `missing_originals`) and `web/src/pages/MaintenancePage.tsx` (≈ line 159, `originals_on_disk`).

## Requirements

- The scan inventories whatever store the instance uses: the local root for `fs`, the bucket (Kukátko's own originals prefix only) for `r2`. Orphan detection and the orphan-import repair then work against the bucket too.
- The report says which store was scanned and names it honestly in the UI: "originálů v úložišti" for r2, "na disku" for fs; the hint text stops talking about disk when there is none. English locale likewise.
- The green verdict is only shown when the inventory actually ran; if listing the store fails, the summary shows the failure instead of 0 and a green box.
- Listing a production-sized bucket (~20 000 objects) must stream and stay bounded in memory; the scan already runs on demand by a maintainer, so wall time is acceptable, but do not hold every key in a map unless the current code already does — match its approach.
- Tests: unit tests with a fake store for both backends (empty bucket, bucket with an orphan, listing error); the S3 integration suite in `internal/storage`/`internal/maintenance` (skips without `KUKATKO_TEST_S3_ENDPOINT`; `.secrets/db.env` provides the local MinIO) covers the real listing; Vitest for the summary wording per backend.
- Docs: `docs/PACKAGES.md` (`internal/maintenance`), `docs/API.md` if the report payload gains a field, `docs/OPERATIONS.md` if the scan's behaviour per backend deserves a line.

## Out of scope

- Do not run a scan or repair against production. Never delete originals (the package's rule).

## Implementation notes

- Definition of Done per `CLAUDE.md`: docs, `make check` (`make check-box` on the build box), commit, push. Skip `make dev`.