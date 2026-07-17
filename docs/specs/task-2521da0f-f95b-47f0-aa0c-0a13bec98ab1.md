# Migration Audit — PhotoPrism → Kukátko

Produce a complete, field-by-field audit of what the PhotoPrism import carries into Kukátko and what it drops, and write it up as `docs/MIGRATION_AUDIT.md`. This task DOCUMENTS; it does not change import behaviour.

## Why

Kukátko replaces PhotoPrism, and the import is the only path the library's history takes into the new database. No document today enumerates source field → target column: `docs/PACKAGES.md` (~lines 1400-1557) describes the import in Czech prose but is not a mapping table. The owner needs certainty that nothing is silently lost.

## Scope

ALL entities, not just photos: photos and their metadata, albums, labels, subjects/people, markers/faces, places, ratings/favourites.

## Where the mapping lives

- `internal/photoprism/models.go` — the SOURCE struct catalogue (`Photo`, `PhotoDetail`, `File`, `Marker`, `Album`, `Label`, `Subject`). `internal/photoprism/photoprism.go` + `download.go` — which endpoints are actually called (`ListPhotos`, `GetPhoto`, `ListAlbums`, `ListLabels`, `ListSubjects`, `DownloadOriginal`).
- `internal/ppimport/metadata.go` — `buildPhoto()` (insert-path mapper, ~line 33), `metadataUpdate()` (incremental patch mapper, ~line 129), `metadataUnchanged()` (idempotency comparator, ~line 184), helpers `caption()`, `applyCaptureMeta()`, `applyCameraMeta()`, `mapMediaType()`.
- `internal/ppimport/details.go` — `importMetadata()` (~line 101). PhotoPrism splits a photo's data across the LISTING and DETAIL endpoints; both halves must be audited.
- `internal/ppimport/people.go` (`importMarkers`, `importOneMarker`, `findOrCreateSubject`, `isNamedFaceMarker`), `organize.go` (`findOrCreateAlbum`, `findOrCreateLabel`, `mapAlbumType`, `attachAlbumMembers`, `attachLabelMembers`), `context.go` (`mapPhotoContext`, `attachPhotoLabel`, `mapLabelSource`, `clampUncertainty`), `video.go` (`videoFields.apply`).
- TARGET: `internal/photos/store.go` `photoInsertColumns` (~lines 32-44, 55 columns) and `UpdateMetadata` (~line 268), `internal/photos/store_import.go` (`importOwnedColumns`), `internal/photos/models.go`. Schema: migrations `0003_photos.sql`, `0004_video.sql`, `0027_photos_iptc_metadata.sql`, `0029`, `0030`, `0033`, `0008_subjects_markers.sql`, `0011_albums_labels_favorites.sql`, `0018_photo_places.sql`.

## Deliverable

Create `docs/MIGRATION_AUDIT.md` **in Czech** — it is a report for the project owner, and the repo's docs are Czech. Structure it as a section "PhotoPrism → Kukátko" with one table per entity. Every row: source field → target column → verdict.

Exactly three verdicts:

- **MAPPED** — the value lands in a column. Note any precedence/fallback rule.
- **WAIVED** — deliberately not carried, WITH the reason (e.g. `Photo.Favorite` is per-user in Kukátko by design, documented at `ppimport.go` ~lines 18-20; `AlbumPhoto.SortOrder` is dropped because Kukátko orders chronologically).
- **GAP** — a real loss nobody decided on. State what is lost, when it bites, and a recommended fix.

Coverage must be provable in BOTH directions: every source field defined in `internal/photoprism/models.go` appears in a table, and every Kukátko column the import is meant to populate is accounted for. Open the document with a summary: how many fields audited, how many mapped / waived / gaps.

## Concrete leads to confirm or refute

These came out of a prior code read. Verify each against the code — do not take them on trust, and do not stop at them.

1. **`metadataUpdate` completeness.** `photos.Store.UpdateMetadata` overwrites 19 columns wholesale. Any editable column not carried in the patch is silently blanked on every incremental re-run. Cross-check the patch against `store.go` ~lines 268-273.
2. **`metadataUnchanged` symmetry.** It must compare exactly the fields `UpdateMetadata` writes. A dropped comparison makes a real rewrite look like a skip.
3. **Zero-value trap.** `buildPhoto` uses `firstPositive`/`firstFloatPtr` for `Iso`, `FNumber`, `FocalLength`, `Altitude`. A legitimate zero (ISO 0, aperture 0, altitude 0 = sea level) falls through to EXIF or is dropped. Same for `Lat == 0 && Lng == 0` (Null Island / the equator).
4. **`photoprism.Subject` and `ListSubjects` look like dead code** — defined on the `Client` interface and implemented, but apparently no caller outside their own tests. Subjects seem to be created only indirectly from marker names, so PP subject `Favorite`/`Private`/`Slug`/`FileCount` never reach Kukátko's `subjects` table — even though `psimport` DOES map the equivalents. Confirm this asymmetry between the two importers.
5. **`Album.Category`** — a PhotoPrism field with no apparent Kukátko home.
6. **`markers.invalid`** — `ppimport` filters invalid markers out via `isNamedFaceMarker` and never sets the column, while `psimport` preserves `Invalid`. Unnamed PhotoPrism faces appear to be dropped entirely. Decision or oversight?
7. Classify each of: `Photo.TakenAtLocal`, `Photo.CreatedAt`, `Photo.UpdatedAt`, `File.UID`, `File.Root`, `File.FileType`, `File.Width`/`Height`, `Marker.FileUID`, `Marker.SubjUID`, `Marker.SubjSrc`, `Marker.Type=label`, `Album.Slug`, `Album.Favorite`, `Album.CreatedAt`/`UpdatedAt`, `Label.Favorite`.
8. **`Photo.Caption` vs `Photo.Description`** — `Description` is believed to be a dead PhotoPrism column and `caption()` prefers `Caption`. Confirm the precedence is right and cannot drop a real description.

## Test-coverage angle

Existing tests assert PRECEDENCE, not COVERAGE — nothing asserts that every source field lands somewhere or is explicitly waived (`ppimport/logic_test.go` `TestBuildPhoto_precedence`, `details_test.go`, `ppimport_integration_test.go`). Note this in the document as a standing risk: without a completeness test, the next added PhotoPrism field can be silently ignored.

## Rules

- **Do not change import behaviour in this task.** The deliverable is the document. Write recommended fixes into it; do not implement them.
- Never put secrets in the document — no DSNs, no API keys, nothing from `.secrets/`.
- Add one signpost line for `docs/MIGRATION_AUDIT.md` to the "Where to find what" table in `CLAUDE.md`. Detail goes in the new doc, never in `CLAUDE.md` — `make docs-budget` enforces its 300-line limit.
- `make check` must pass.