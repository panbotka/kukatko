# Migration audit — PhotoPrism → Kukátko

- **Audit date:** 2026-07-17
- **Audited commit:** `6e2600e` (branch `main`)
- **Scope:** the complete field-by-field mapping by which the import from a running
  PhotoPrism instance populates the Kukátko catalog — photos and their metadata, albums,
  labels, subjects/people, markers/faces, places, ratings and favorites.
- **Purpose:** to give confidence that the migration from PhotoPrism — the only path by
  which the library's history enters the new database — loses nothing silently. The document
  was originally **descriptive only**; its four gaps (GAP) in the "PhotoPrism → Kukátko"
  section **are now resolved** — three subject ones (`Subject.Type`/`Favorite`/`Private`) as
  MAPPED, `Album.Category` as WAIVED — and the rows below reflect that. The
  "photo-sorter → Kukátko" section goes on to describe the rest, and its recommended fixes
  remain as written.

Audited code: `internal/photoprism/` (source structs `models.go` and the endpoints
`photoprism.go` + `download.go`), `internal/ppimport/` (the mappers), `internal/photos/`
(target columns and the INSERT/UPDATE paths), migrations `internal/database/migrations/`.
The comparison importer is `internal/psimport/` (the direct migration from photo-sorter), to
which the asymmetry refers in several places.

## Verdict legend

- **MAPPED** — the value is carried into a column. For fields with precedence/fallback the
  rule is in the note.
- **WAIVED** — deliberately not carried, always with a reason.
- **GAP** — a real loss that nobody decided on. The note says what is lost, when it hurts, and
  the recommended fix.

## Summary

Audited **89 source fields** defined in `internal/photoprism/models.go` (including 6
container/relational fields — `Photo.Files`, `PhotoDetail.Albums`, `PhotoDetail.Labels`,
`File.Markers` and references to `Details`):

| Verdict | Count |
| --- | --- |
| **MAPPED** | 61 |
| **WAIVED** | 28 |
| **GAP** | 0 |
| **Total** | 89 |

**Gap status — all four resolved (this task's commit):**

1. **`Subject.Private` → `subjects.private`** — **FIXED (MAPPED).** A person marked private in
   PhotoPrism stays private after import. This was the most serious gap: it had a privacy
   impact.
2. **`Subject.Favorite` → `subjects.favorite`** — **FIXED (MAPPED).** The favorite-person flag
   is carried over.
3. **`Subject.Type` → `subjects.type`** — **FIXED (MAPPED).** An animal (`pet`) or other entity
   (`other`) keeps its type; no longer is every subject hard-coded to `person`.
4. **`Album.Category` → (no column)** — **WAIVED (product decision).** Kukátko has no concept
   of an album category (no column, no UI, no query) — adding a write-only column that nobody
   reads would be a dead column. See "Albums" and verified clue #5.

Gaps 1–3 shared a common cause and a common fix — see the "Subjects" section and verified
clue #4 below.
Besides these (now resolved) four, there are several **deliberately omitted** items with a
non-trivial impact (unnamed/invalid face markers, `month`-type albums on a full run) — they
are described in "Risks and deliberate trade-offs".

## How the import fills a photo row (context for the tables)

PhotoPrism splits a photo's data between **two endpoints**, so the audit has to track both
halves:

- **The listing** `GET /photos?merged=true` (`ListPhotos`) returns a flat search structure
  `Photo`. It carries the core metadata (title, caption, time, GPS, camera), but **no
  `Details` block**, files with an **always-empty `Markers` field**, and no per-file
  codec/color profile.
- **The detail** `GET /photos/{uid}` (`GetPhoto`) returns `PhotoDetail` = `Photo` + the
  `Details` block (IPTC/XMP credits) + the file's technical fields + the **face markers** + the
  **albums** and **labels** relations. Everything that is "detail-only" is carried from this
  single request (`ppimport.importPhotoDetail` → `importMetadata` / `importMarkers` /
  `mapPhotoContext`).

The insert path: `buildPhoto` (`metadata.go`) assembles the row from the listing and the
original's own downloaded EXIF, `videoFields.apply` (`video.go`) tops up the video columns
from ffprobe, and `photos.Store.Create` (`store.go`, `photoInsertColumns`) inserts the row.
The detail fields are then written by `photos.Store.ApplyImportMetadata`
(`store_import.go`) with the rule "the source owns the credits, empty never erases". An
incremental pass changes metadata via `metadataUpdate` → `UpdateMetadata`.

---

## Photos — the `Photo` structure (listing)

The target is the `photos` table (migrations `0003`, `0004`, `0024`, `0027`, `0028`, `0029`,
`0030`, `0033`). Precedence for the curatorial fields: **PhotoPrism wins when it has a value;
empty falls back to the file's own EXIF** (`applyCameraMeta`, `applyCaptureMeta`).

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Photo.UID` | `photoprism_uid` | **MAPPED** | Stable import and dedup key (`GetByPhotoprismUID`). |
| `Photo.Type` | `media_type` | **MAPPED** | `mapMediaType`: `video`/`animated`→video, `live`→live, the rest→image. But the actual downloaded file also decides (`selectMedia`): a video with no stream degrades to image. |
| `Photo.Title` | `title` | **MAPPED** | Inserted directly; incremental `firstNonEmpty(pp.Title, existing.Title)` — a title deleted upstream won't overwrite a filled one. |
| `Photo.Caption` | `description` | **MAPPED** | The primary source of the caption; `caption() = firstNonEmpty(Caption, Description)`. |
| `Photo.Description` | `description` | **MAPPED** | Fallback in `caption()`. On the current PhotoPrism the field is dead (`gorm:"-"`, always empty), but modeled for an older instance; the precedence is right (the live `Caption` first) and a caption cannot slip through the cracks. |
| `Photo.TakenAt` | `taken_at` (+ `taken_at_source='exif'`) | **MAPPED** | `applyCaptureMeta`; on a zero time, falls back to the file's EXIF with its source. |
| `Photo.TakenAtLocal` | — | **WAIVED** | Kukátko keeps a single canonical `taken_at` (timestamptz); local rendering is derived on output, `TakenAt` already carries the instant. |
| `Photo.UpdatedAt` | — (no column) | **WAIVED** | Drives the incremental high-watermark in `import_runs` (max `UpdatedAt` per run), it is not a photo column; `photos.updated_at` is the time of the row's own mutation. |
| `Photo.CreatedAt` | — (no column) | **WAIVED** | `photos.created_at` = when the photo was created locally (DB `now()`). PhotoPrism's indexing time is not the library's history. |
| `Photo.Lat` | `lat` (+ `location_source`) | **MAPPED** | Carried only when `Lat != 0 || Lng != 0`; provenance `exif`. Edge: exactly `(0,0)` "Null Island" is treated as no location (a universal convention), see Risks. |
| `Photo.Lng` | `lng` | **MAPPED** | See `Lat`. |
| `Photo.Altitude` | `altitude` | **MAPPED** | Carried only when `Altitude != 0`, otherwise EXIF. Edge: altitude `0` = sea level falls back to EXIF/nil (see Risks, "the zero trap"). |
| `Photo.Width` | `file_width` | **MAPPED** | `firstPositive(pp.Width, meta.Width)`. |
| `Photo.Height` | `file_height` | **MAPPED** | `firstPositive(pp.Height, meta.Height)`. |
| `Photo.OriginalName` | `original_name` | **MAPPED** | Via `ImportMetadata` from the detail; also drives the name in storage (`originalName()`). |
| `Photo.CameraMake` | `camera_make` | **MAPPED** | `firstNonEmpty(pp, meta)`. |
| `Photo.CameraModel` | `camera_model` | **MAPPED** | ditto. |
| `Photo.LensModel` | `lens_model` | **MAPPED** | ditto. |
| `Photo.Iso` | `iso` | **MAPPED** | `firstIntPtr`: only positive wins, ISO `0` = unknown → fallback to EXIF (correct, ISO 0 is not a real value). |
| `Photo.FNumber` | `aperture` | **MAPPED** | `firstFloatPtr`: only positive; `f/0` does not exist → fallback (correct). |
| `Photo.Exposure` | `exposure` | **MAPPED** | `firstNonEmpty`. |
| `Photo.FocalLength` | `focal_length` | **MAPPED** | `firstFloatPtr(float64(pp.FocalLength), …)`; `0` → fallback. |
| `Photo.CameraSerial` | `camera_serial` | **MAPPED** | Via `ImportMetadata` from the detail (`detail.CameraSerial`). |
| `Photo.Scan` | `scan` | **MAPPED** | Via `ImportMetadata` from the detail; a "true-wins" rule (can be set, not cleared). |
| `Photo.Favorite` | — | **WAIVED** | Favorites in Kukátko are **per-user** by design (`ppimport.go` ~ll. 18–20); an import running as a job/CLI has nobody to attribute the flag to. |
| `Photo.Private` | `private` | **MAPPED** | `buildPhoto` and `metadataUpdate`. |
| `Photo.Files` | → the `File` table | **MAPPED** | The files container; see the "Files" section. |

**`PhotoDetail` — relations beyond `Photo`** (the detail endpoint's envelope):

| Source field | Target | Verdict | Note |
| --- | --- | --- | --- |
| `PhotoDetail.Details` | → the `Details` table | **MAPPED** | IPTC/XMP credits, see below. |
| `PhotoDetail.Albums` | `album_photos` + `albums` | **MAPPED** | On a scoped run, **every** album of the photo is mapped from the detail (`mapPhotoContext`), not just the one that selected it. |
| `PhotoDetail.Labels` | `photo_labels` + `labels` | **MAPPED** | ditto for labels; see "Labels". |

## Photos — the `Details` block (detail, IPTC/XMP credits)

Written by `ApplyImportMetadata`: a non-empty source value wins over the current one (the same
precedence as the camera), empty never erases. `notes` is the exception — it is Kukátko's own
field, **gap-fill** only (it never overwrites the user's note).

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Details.Subject` | `subject` | **MAPPED** | Trimmed. |
| `Details.Keywords` | `keywords` | **MAPPED** | `exif.NormalizeKeywords` — read as if natively extracted. |
| `Details.Notes` | `notes` | **MAPPED** | Gap-fill only (empty `notes` → filled; otherwise left alone). |
| `Details.Artist` | `artist` | **MAPPED** | |
| `Details.Copyright` | `copyright` | **MAPPED** | |
| `Details.License` | `license` | **MAPPED** | |
| `Details.Software` | `software` | **MAPPED** | |

## Files — the `File` structure

Kukátko does not keep a 1:1 catalog of PhotoPrism files; from the primary file it takes the
content (the download hash + a reference), and the technical fields from the detail. The rest
are either internal to PhotoPrism or redundant with the photo's fields.

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `File.Hash` | `photoprism_file_hash` | **MAPPED** | SHA1 of the primary file; also the download key (`/dl/<Hash>`). |
| `File.Mime` | `file_mime` | **MAPPED** | `firstNonEmpty(primary.Mime, meta.Mime, stored.MIME)`. |
| `File.Name` | `original_name` (fallback) / name in storage | **MAPPED** | `originalName()` uses `Photo.OriginalName`, otherwise `path.Base(File.Name)`; `companionName()` names the motion clip. |
| `File.Primary` | (selection of the primary file) | **MAPPED** | The `PrimaryFile()` behavior; the primary file is the original. |
| `File.Video` | (media selection / `IsVideo`) | **MAPPED** | Distinguishes a still from a motion clip and co-decides `media_type`. |
| `File.Codec` | `image_codec` (stills only) | **MAPPED** | `exif.CodecToken`; the video codec (`avc1`/`hvc1`) is **deliberately not taken from PP** — `video_codec` is owned by ffprobe. |
| `File.ColorProfile` | `color_profile` | **MAPPED** | Detail-only. |
| `File.Projection` | `projection` | **MAPPED** | Detail-only (panorama). |
| `File.Markers` | → the `Marker` table | **MAPPED** | Only from the detail (the listing's is empty); see "Markers". |
| `File.UID` | — | **WAIVED** | Kukátko's files are keyed by `photo_uid` + path; the PP file UID is not needed. |
| `File.Root` | — | **WAIVED** | PhotoPrism's internal storage-root tag. |
| `File.Width` | — | **WAIVED** | Redundant — the geometry is taken from `Photo.Width` (see above). |
| `File.Height` | — | **WAIVED** | ditto. |
| `File.FileType` | — | **WAIVED** | The type is carried by `file_mime` + `image_codec`; the text tag is not stored. |

## Markers / faces — the `Marker` structure

The target is the `markers` table (migration `0008`). `importMarkers` seeds **only named,
valid face** markers (`isNamedFaceMarker`: `Type=="face"` && `!Invalid` && `Name != ""`); the
subject is found/created from the name (`findOrCreateSubject`) and the marker is attached to
it. The marker **keeps its PhotoPrism UID** → the import is idempotent and marker identity is
shared with `psimport` (whose markers ARE PhotoPrism's).

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Marker.UID` | `markers.uid` | **MAPPED** | Idempotence (`GetMarkerByUID`) and shared identity across importers. |
| `Marker.Name` | `subjects.name` (indirectly) | **MAPPED** | Seeds the subject by name (`findOrCreateSubject`). A marker with no name is not carried over. |
| `Marker.X` / `Y` / `W` / `H` | `markers.x/y/w/h` | **MAPPED** | Normalized bbox (0..1). |
| `Marker.Score` | `markers.score` | **MAPPED** | Note: `score` is import provenance, not quality (0 = unrecorded); never rank faces by it. |
| `Marker.Review` | `markers.reviewed` | **MAPPED** | `Reviewed = !Review`. |
| `Marker.Type` | (filter: `face` only) | **WAIVED** | Only `face` is carried; **label markers are discarded** — Kukátko's labels come from the `Labels` relation, not from label markers. |
| `Marker.Invalid` | `markers.invalid` (never set) | **WAIVED** | `isNamedFaceMarker` filters out invalid markers; the column stays `false`. **Asymmetry:** `psimport` preserves `Invalid`. A deliberate decision (Kukátko's `face_detect` rediscovers the regions), but it has a cost — see Risks. |
| `Marker.FileUID` | — | **WAIVED** | Kukátko's markers reference `photo_uid`, not the file. |
| `Marker.FileHash` | — | **WAIVED** | ditto. |
| `Marker.SubjUID` | — | **WAIVED** | The subject link is **re-derived from the name**, not from the PP subject UID; PP subjects are not read at all (see "Subjects"). |
| `Marker.SubjSrc` | — | **WAIVED** | ditto. |

## Subjects / people — the `Subject` structure

The target is the `subjects` table (migration `0008`: `uid, slug, name, type, favorite,
private, notes, cover_photo_uid, …`; no `file_count` column). **An earlier finding** (now
resolved): both `photoprism.Subject` and the `ListSubjects` client were **dead code** as far as
the import was concerned — the import interface `ppimport.PhotoPrismClient` did not declare
`ListSubjects`, so no field of the PP subject was read and subjects were created hard-coded as
`person`. **Fix (implemented):** `ppimport.PhotoPrismClient` now declares `ListSubjects`;
`Service.loadSubjectIndex` reads the subjects once per run (best-effort) into an index by both
UID and name slug, and `findOrCreateSubject` → `newSubject` enriches a **newly created**
subject with `type`/`favorite`/`private` from the PP subject that the marker names (paired via
`Marker.SubjUID`, falling back to the name slug). The enrichment happens **only on creation** —
an existing subject (possibly edited in Kukátko) is left unchanged, so the pass is idempotent
and does not overwrite a local edit (the same behavior as `psimport`).

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Subject.Name` | `subjects.name` | **WAIVED** | Redundant: the name arrives via `Marker.Name`, the `Subject.Name` field itself is not read — nothing of value is lost. |
| `Subject.UID` | (index `SubjUID`→subject) | **MAPPED** | Not stored as a column, but `loadSubjectIndex` uses it as the key through which a marker (`Marker.SubjUID`) finds its PP subject to enrich `type`/`favorite`/`private`. Kukátko's `subjects.uid` is still generated on its own. |
| `Subject.Slug` | `subjects.slug` | **WAIVED** | Kukátko generates the slug from the name (`people.Slugify`); the PP subject's slug only serves as a fallback index key. |
| `Subject.FileCount` | — (no column) | **WAIVED** | Kukátko counts markers live (`ListSubjects`/`SubjectCount`), it does not cache. |
| `Subject.Type` | `subjects.type` | **MAPPED** | `mapSubjectType` (`pet`/`other`/default `person`), same as `psimport`. An animal keeps its type; set when the subject is created. |
| `Subject.Favorite` | `subjects.favorite` | **MAPPED** | Carried when the subject is created (`newSubject`); a global column, not per-user. |
| `Subject.Private` | `subjects.private` | **MAPPED** | A private person from PP stays private. Set when the subject is created. |

## Albums — the `Album` structure

The target is the `albums` table (migration `0011`; `0022` removed `order_by`/`sort_order`).
`findOrCreateAlbum` matches/creates an album **by title**. A full run maps the types
`album/folder/moment/state` (default `DefaultAlbumTypes`, `month` omitted); a scoped run maps
any album type of the photo (`mapPhotoContext`, `requireAlbum` walks all types).

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Album.Title` | `albums.title` | **MAPPED** | Find-or-create key; an empty title = the album is skipped. |
| `Album.Description` | `albums.description` | **MAPPED** | |
| `Album.Type` | `albums.type` | **MAPPED** | `mapAlbumType`; unknown/empty → `album`. Note: `month` albums (560 auto-generated) are not mapped by a full run — see Risks. |
| `Album.Private` | `albums.private` | **MAPPED** | |
| `Album.UID` | — | **WAIVED** | Only used to list members; Kukátko generates its own `uid`. |
| `Album.Slug` | `albums.slug` | **WAIVED** | Regenerated from the title. |
| `Album.Favorite` | — (no column) | **WAIVED** | `albums` has no `favorite` — there is no concept of a favorite album in Kukátko. |
| `Album.CreatedAt` | — | **WAIVED** | `albums.created_at` is DB `now()`. |
| `Album.UpdatedAt` | — | **WAIVED** | ditto. |
| `Album.Category` | — (no column) | **WAIVED** | An album category (grouping by theme) has nowhere to land in Kukátko. **Product decision:** Kukátko **has no** album-category concept — no column, no UI, no query over it (grep of the whole repo: the only "category" hits are CLDR plurals). Adding a write-only `albums.category` that nobody reads would be a dead column, not a fix; hence formally WAIVED (the same decision as for the photo-sorter import, where `albums.category` stays a GAP only until it is decided — here decided: no home). |

**Album membership** (`album_photos`): PhotoPrism's ordering in the listing is not carried —
Kukátko sorts albums **chronologically** (`0022` dropped `sort_order`). `AddPhoto` is
idempotent. Members not yet imported are skipped.

## Labels — the `Label` and `PhotoLabel` structures

The targets are the `labels` table (`uid, slug, name, priority`) and the `photo_labels` join
(`source`, `uncertainty`), both migration `0011`. `findOrCreateLabel` matches/creates **by
name**.

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Label.Name` | `labels.name` | **MAPPED** | Find-or-create key. |
| `Label.Priority` | `labels.priority` | **MAPPED** | |
| `Label.UID` | — | **WAIVED** | Its own `uid`. |
| `Label.Slug` | `labels.slug` | **WAIVED** | Used to query members (`label:"<slug>"`); the stored slug is regenerated. |
| `Label.Favorite` | — (no column) | **WAIVED** | `labels` has no `favorite`. |
| `PhotoLabel.LabelSrc` | `photo_labels.source` | **MAPPED** | `mapLabelSource`: `manual`→manual, `image`→ai, others (`batch`/`keyword`/`location`/`meta`…)→import. |
| `PhotoLabel.Uncertainty` | `photo_labels.uncertainty` | **MAPPED** | `clampUncertainty` to 0–100. |
| `PhotoLabel.Label` | `labels` | **MAPPED** | The nested label, see above. |

## Places

PhotoPrism has **no place fields** in the imported structures (`Country`, `PlaceLabel` and the
like are not read into `photoprism.models`). Kukátko's `photo_places` table (migration `0018`:
`country, region, city, place_name, lat, lng, geocoded_at`) is a **reverse-geocoding cache**
filled by the `places` job from the photo's GPS, not from PhotoPrism. The coordinates
(`lat`/`lng`) are therefore migrated (see "Photos"), while the place names are **recomputed**.
For this audit: no source field, derived in Kukátko (**WAIVED**, no loss — it arises from the
migrated location).

## Ratings and favorites

- **Star ratings:** PhotoPrism does not have them, there is nothing to migrate. Kukátko's
  `user_ratings` (`rating` 0–5, `flag` `none/pick/reject/eye`; migrations `0016`, `0025`) is
  **per-user** and starts at zero after import.
- **Favorite photos:** `Photo.Favorite` → **WAIVED** (per-user `user_favorites`, see
  "Photos").
- **Favorite albums / labels:** the target columns do not exist → **WAIVED** (see "Albums",
  "Labels").
- **Favorite / private people:** `Subject.Favorite`/`Private` → **MAPPED** (global columns, the
  import now fills them when the subject is created; see "Subjects").

## The target side — `photos` columns the import does not fill from PP

To make coverage provable in both directions: each of the 56 inserted `photos` columns
(`photoInsertColumns`) is either mapped from a PP field (above), or **derived by Kukátko** from
the downloaded original, or **Kukátko's own** (the import leaves it at its default). The
derived and own columns, which therefore *have no* source PP field:

| Column | Origin | Note |
| --- | --- | --- |
| `uid`, `created_at`, `updated_at` | DB | Generated on insert. |
| `file_hash`, `file_path`, `file_name`, `file_size` | Storage | SHA256 and the layout of the downloaded original. |
| `file_orientation`, `exif` | file EXIF | PhotoPrism does not return orientation. |
| `taken_at_source`, `location_source` | Derived | `exif`/`unknown`/`""` depending on the origin of the time and GPS. |
| `duration_ms`, `video_codec`, `audio_codec`, `has_audio`, `fps` | ffprobe | Video columns from `videoFields`, not from PP. |
| `taken_at_estimated`, `taken_at_note` | Kukátko-only | PhotoPrism has no approximate date; the incremental pass carries them through unchanged. |
| `ai_note` | Kukátko-only | An external AI pass. |
| `archived_at`, `uploaded_by` | Kukátko-only | Not set by the import (the job has no user). |
| `stack_uid`, `stack_primary` | Kukátko-only | Stack detection (`internal/stacks`). |
| `metadata_extracted_at` | Kukátko-only | The import leaves it `nil` (it maps from the source, not from the file) → the `metadata` backfill schedules it. |
| `photosorter_uid` | another import | Filled only by `psimport`. |

---

## Verification of specific clues from the brief

### 1. Completeness of `metadataUpdate` vs. the 19 overwritten columns — **confirmed correct (not a bug)**

`UpdateMetadata` (`store.go` ~ll. 268–274) overwrites the whole row — 19 columns:
`title, description, notes, ai_note, taken_at, taken_at_source, lat, lng, altitude,
private, subject, keywords, artist, copyright, license, scan, taken_at_estimated,
taken_at_note, location_source`. `metadataUpdate` (`metadata.go` ~l. 129) **carries all of
them**: the fields mapped from PP (`Title`, `Description`, `Private`, conditionally
`TakenAt`/GPS/`Altitude`), and the rest are **provably carried from `existing`** (`Notes`,
`AiNote`, `Subject`, `Keywords`, `Artist`, `Copyright`, `License`, `Scan`,
`TakenAtEstimated`, `TakenAtNote`, `LocationSource`). No editable column is silently zeroed by
the incremental run. Credits are changed separately by `ApplyImportMetadata` from the detail;
`metadataUpdate` only "carries them through unchanged" so the bulk overwrite does not erase
them. **The clue about silent erasure is refuted.**

### 2. Symmetry of `metadataUnchanged` — **confirmed symmetric**

`metadataUnchanged` (`captionsUnchanged` + `creditsUnchanged` + `placementUnchanged`) compares
exactly the same 19 fields that `UpdateMetadata` writes. No field drops out of the comparison,
so a real overwrite cannot masquerade as a no-op.

### 3. The zero trap (`firstPositive`/`firstFloatPtr`, GPS) — **mostly harmless, one real edge**

- `Iso`, `FNumber`, `FocalLength`: `firstIntPtr`/`firstFloatPtr` take only **strictly
  positive** values. Zero for these quantities is the sentinel for "unknown" (ISO 0, `f/0`,
  focal length 0 do not really exist), so the fallback to EXIF is **correct**, not a loss.
- `Altitude`: `if pp.Altitude != 0`. An altitude of **0 = sea level is legitimate** and falls
  back to EXIF/nil. A real (if marginal) fidelity loss for photos exactly at sea level, where
  PP had 0 and EXIF did not. Low impact.
- GPS `if pp.Lat != 0 || pp.Lng != 0`: the equator or the prime meridian alone pass (OR), the
  problem is **only exactly `(0,0)` "Null Island"** — treated as no location. This is the
  universal convention for "not geotagged" and a real photo practically never originates there;
  acceptable.

### 4. `photoprism.Subject` and `ListSubjects` were dead code — **confirmed; RESOLVED**

`ListSubjects` and `photoprism.Subject` were defined on `photoprism.Client`, but the import
interface `ppimport.PhotoPrismClient` did not declare them, so the import never called them and
the PP subject's `Favorite`/`Private`/`Type` never reached `subjects`, even though the columns
exist and `psimport` fills them. This was the source of gaps 1–3 from the summary.
**Fix (implemented):** `ppimport.PhotoPrismClient` declares `ListSubjects`;
`loadSubjectIndex` reads them once per run (best-effort, a failure does not spoil the import —
it just skips enrichment) into an index by both `SubjUID` and the name slug; `newSubject`
enriches a newly created subject with `type`/`favorite`/`private` (pairing on the marker's
`SubjUID`, falling back to the slug). Symmetric with `psimport`. `Slug`/`FileCount` still have
no target (the slug is generated, the count is computed live).

### 5. `Album.Category` — **confirmed homeless → WAIVED (product decision)**

`findOrCreateAlbum` reads only `Title`/`Description`/`Type`/`Private`. `Category` is not read
and `albums` has no column for it. Grep of the whole repo: Kukátko has no album-category
concept whatsoever (column, UI, or query) — the only "category" hits are CLDR plurals in i18n.
Adding a write-only column that nothing reads would be a dead column, not a fix; hence formally
**WAIVED**, not inventing a column. See "Albums".

### 6. `markers.invalid` — **a deliberate decision (WAIVED) with an asymmetry vs. `psimport`**

`ppimport` filters out invalid markers (`isNamedFaceMarker`) and never sets the `invalid`
column; `psimport` preserves `Invalid` (`people.Marker{Invalid: m.Invalid, …}`). On top of
that, `ppimport` also discards **unnamed** faces (`Name == ""`) and **label markers**. This is
not a mistake — both the comment and the architecture say that unnamed/invalid regions will be
rediscovered by Kukátko's `face_detect` (paired via IoU). But the cost of the decision is real,
see Risks.

### 7. Classification of the enumerated fields

| Field | Verdict | Where in the document |
| --- | --- | --- |
| `Photo.TakenAtLocal` | **WAIVED** | Photos (single canonical `taken_at`). |
| `Photo.CreatedAt` | **WAIVED** | Photos (local creation time). |
| `Photo.UpdatedAt` | **WAIVED** | Photos (drives the high-watermark, not a column). |
| `File.UID` | **WAIVED** | Files. |
| `File.Root` | **WAIVED** | Files (internal PP). |
| `File.FileType` | **WAIVED** | Files (carried by mime + `image_codec`). |
| `File.Width` / `File.Height` | **WAIVED** | Files (redundant with `Photo.Width/Height`). |
| `Marker.FileUID` | **WAIVED** | Markers (reference to `photo_uid`). |
| `Marker.SubjUID` | **WAIVED** | Markers (link re-derived from the name). |
| `Marker.SubjSrc` | **WAIVED** | Markers. |
| `Marker.Type == label` | **WAIVED** | Markers (`face` only; labels from the relation). |
| `Album.Slug` | **WAIVED** | Albums (regenerated). |
| `Album.Favorite` | **WAIVED** | Albums (no target). |
| `Album.CreatedAt` / `UpdatedAt` | **WAIVED** | Albums (DB times). |
| `Label.Favorite` | **WAIVED** | Labels (no target). |

### 8. `Photo.Caption` vs. `Photo.Description` — **confirmed correct**

`caption() = firstNonEmpty(pp.Caption, pp.Description)`. `Caption` is the live field
(PhotoPrism renamed `photo_description` → `photo_caption`); `Description` is the dead
predecessor (`gorm:"-"`, always empty from the current instance). The precedence is correct:
`Caption` first, `Description` only a fallback for an old instance. A real caption cannot slip
through the cracks.

---

## Risks and deliberate trade-offs

Items that are not a "GAP" (either decided on, or with a marginal impact), but which the owner
should know about:

1. **Unnamed and invalid face markers are not carried** (`isNamedFaceMarker`). The biggest
   behavioral difference on the people side. Only **named, valid** faces are carried; the rest
   is to be rediscovered by `face_detect`. Two costs: (a) while the box is offline, these faces
   are **not** in the library (the job waits in the queue); (b) a face that a person manually
   marked as **invalid** in PhotoPrism (`Invalid`) may be pulled back in by `face_detect` — the
   human "no" is lost. `psimport` does not have this problem (it copies markers including
   `Invalid`). Consider preserving at least the `Invalid` regions as invalid markers for the PP
   import.
2. **`month`-type albums are not mapped on a full run** (`DefaultAlbumTypes` omits them).
   Deliberate (560 auto-generated monthly albums, covered by the timeline). A scoped run does
   map them, though, so the result depends on how the import is run — worth documenting for
   consistency.
3. **Altitude 0 and "Null Island"** — the marginal edges of the zero trap (clue 3).
4. **Subjects are paired by name slug** — `Marker.SubjUID` is now read for enrichment (to find
   the right PP subject for `type`/`favorite`/`private`), but the Kukátko subject itself is
   still created by the name slug, so two different people with the same name in PhotoPrism
   merge into one Kukátko subject (and conversely, a rename in PP creates a new one). `psimport`
   also pairs by name slug, so both paths behave the same — but it is a property, not an
   identity guarantee. Enrichment edge: for a merged name, the PP subject whose marker creates
   the Kukátko subject first wins.

## Test-coverage risk (standing)

The existing tests verify **precedence, not coverage**: `ppimport/logic_test.go`
`TestBuildPhoto_precedence`, `details_test.go`, `ppimport_integration_test.go` check that PP
wins over EXIF and that empty does not erase — but **nothing asserts that every source field
lands somewhere or is deliberately omitted**. Without a completeness test, the next field added
to `photoprism.models` can slip through silently, exactly as `Album.Category` or
`Subject.Private` once did (both gaps are resolved today, but this audit — not a test — found
them).

**Recommendation:** a table test that reflectively walks the fields of `photoprism.Photo` /
`File` / `Marker` / `Album` / `Label` / `Subject` and fails until each is either in a mapping
function or on an explicit "WAIVED" allow-list referencing this document. Such a test turns a
silent regression into a red `make check`.

---

# Migration audit — photo-sorter → Kukátko

- **Audit date:** 2026-07-17
- **Audited commit:** `3d6a51e` (branch `main`)
- **Scope:** the complete field-by-field mapping by which the direct migration from
  photo-sorter (`internal/psimport`) populates the Kukátko catalog — photos and metadata,
  embeddings, faces, subjects/people, markers, albums and membership, labels and membership,
  perceptual hashes and non-destructive edits.
- **Purpose:** to give confidence that the migration from photo-sorter — the only path by which
  this library enters the new database — loses nothing silently. Seven gaps at the `photos`
  column level (IPTC credits `exif_artist`/`copyright`/`license`/`software`, `keywords`, `scan`,
  `panorama`) are **resolved (MAPPED)** as of this task's commit; `albums.category` and the free
  text of migration 037 (`subjects.bio`/`about`/`alias`, `labels.description`/`categories`,
  `albums.location`/`notes`) are **WAIVED (product decision — no target in Kukátko)**;
  `photo_files` and `cover_photo_uid` (of subjects and albums) remain **GAP** — their fix is
  larger than a field fix (see "Scope and what remains GAP"). The rows below reflect that.

Audited code: `internal/photosorter/` (the read-only pgx reader: `models.go`, `photos.go`,
`vectors.go`, `organize.go`, `people.go`, `extras.go`), `internal/psimport/` (the mappers:
`photos.go` `buildPhoto`, `mappings.go`, `vectors.go`, `satellites.go`, `helpers.go`),
`internal/photos/`, `internal/vectors/`, `internal/people/`, `internal/organize/` (the target
columns) and migrations `internal/database/migrations/`. The actual source schema was verified
against the photo-sorter migrations (`…/postgres/migrations/001–045`, not against the production
DB — the DSN is not written into the document).

**The verdict legend** is the same as the PhotoPrism section above (MAPPED / WAIVED / GAP).

## Summary

The audit has **two layers**, because the losses with photo-sorter do not lie in the mappers
but in what the reader loads at all:

**Layer A — the fields of the `internal/photosorter/models.go` models** (what the reader brings
into Kukátko). Audited **97 fields** across 11 structures (`Photo`, `Embedding`, `Face`,
`Subject`, `Marker`, `Album`, `AlbumPhoto`, `Label`, `PhotoLabel`, `Phash`, `Edit`); 7 of them
(IPTC credits, `keywords`, `scan`, `panorama`) were added to `Photo` by this task:

| Verdict | Count |
| --- | --- |
| **MAPPED** | 89 |
| **WAIVED** | 8 |
| **GAP** | 0 |
| **Total** | 97 |

At this layer **nothing is missing**: every field the reader exposes is carried 1:1 by the
mappers (embeddings, faces and markers are copied including their UIDs, and the subject is
merely re-pointed). The eight WAIVED are identifiers and slugs that Kukátko generates on its own
(`Subject/Album/Label.UID`+`Slug`), album ordering (`AlbumPhoto.SortOrder`), and the watermark
(`Photo.UpdatedAt`).

**Layer B — photo-sorter columns and tables the reader NEVER reads.** This is where the real
gaps are. The reader `SELECT`s only a subset of columns and only 12 of 28 tables; data that does
not enter the models cannot reach Kukátko, even if the target column exists. Overview (detail in
the section "What the reader drops at the DB boundary"):

| | Count |
| --- | --- |
| Unread catalog-bucket tables | **1 GAP** (`photo_files`, retained) + 1 WAIVED (`era_embeddings`) |
| Dropped `photos` columns | **0 GAP** (7 newly MAPPED by this task) + 6 WAIVED |
| Dropped extra `subjects`/`labels`/`albums` columns (photo-sorter migration 037) | **2 GAP** (`cover_photo_uid` of both subjects and albums, retained) + 13 WAIVED |

**The original gaps (GAP) and their status after this task's commit, in descending order of
impact:**

1. **`photo_files` — the whole physical-files table is not read. → LEFT AS GAP.**
   photo-sorter keeps RAW+JPEG stacks, HEIC+JPEG sidecars and edited variants in `photo_files`
   (roles `original`/`sidecar`/`edited`). The migration copies **only the primary original**
   (one `photo_files` row in Kukátko); the sibling files of a single shot (the RAW next to the
   JPEG, the motion part of a live photo, the edited variant) are **lost**. Kukátko has its own
   `photo_files` + `internal/stacks` where they would belong. The fix is **a whole table, not a
   field** (listing `photo_files` in the reader + copying the secondary files + grouping via
   `internal/stacks`) — deliberately left as a GAP, see "Scope and what remains GAP" (clue #1).
2. **IPTC/XMP credits + `keywords`/`scan` — 6 columns. → FIXED (MAPPED).**
   `photos.exif_artist`, `exif_copyright`, `exif_license`, `exif_software`, `keywords` (TEXT[])
   and `scan` **exist** in photo-sorter and Kukátko has columns for them
   (`artist`/`copyright`/`license`/`software`/`keywords`/`scan`, migration `0027`). The reader
   now reads them (`photoColumns`) and `buildPhoto` maps them; `keywords` is joined from the
   TEXT[] and normalized (`exif.NormalizeKeywords`) into the comma-separated column (clue #3).
3. **`photos.panorama` (bool) → Kukátko's `projection`. → FIXED (MAPPED).**
   The reader reads `panorama` and `buildPhoto` maps it via `panoramaProjection` onto
   `projection` (`true`→`equirectangular`, otherwise empty) — the only projection Kukátko
   models.
4. **`subjects.cover_photo_uid`, `albums.cover_photo_uid`. → LEFT AS GAP.**
   The chosen cover photo of both a person and an album is discarded; the target columns exist in
   Kukátko, but the fix requires **remapping the photo UID only after the photos are imported**:
   subjects/albums are created in `buildMappings` **before** the photo loop, so the cover photo
   does not yet exist. The correct fix is a new idempotent write-back pass after the photo loop,
   not adding a field — hence left as a GAP, see "Scope and what remains GAP".
5. **`albums.category`. → WAIVED (product decision).** An album category has nowhere to land in
   Kukátko (no column, UI, or query) — **the same decision as `Album.Category` in the PhotoPrism
   section above**. Adding a write-only column that nobody reads would be a dead column, not a
   fix.
6. **The extra free text of migration 037. → WAIVED (product decision).**
   `subjects.bio`/`about`/`alias`, `labels.description`/`categories`, `albums.location`/`notes`
   have no target column in Kukátko (a subject has only `notes`, mapped from `Subject.Notes`; a
   label only `slug`/`name`/`priority`; an album only `title`/`description`/`private`/`type`).
   Modeling a person's bio/alias, a label's description, or an album's location/note is a
   **product feature, not a migration fix**; adding a write-only column with no reader would be a
   dead column (the same standard as `albums.category`). WAIVED until Kukátko introduces such a
   feature — then reopen the row. `cover_photo_uid` from the same bucket is, by contrast, a real
   GAP (the target exists), see #4.

Beyond the gaps there are several **deliberate trade-offs** (per-user favorites, dropped
ordering, the absence of video columns on the source side) in the section "Risks and deliberate
trade-offs".

## How the import fills a row (context for the tables)

Unlike the PhotoPrism import (two HTTP endpoints), photo-sorter is a **direct DB→DB copy**.
`resolvePhoto` finds a photo by `photosorter_uid` (already migrated) or `file_hash` (already in
the catalog, e.g. from PhotoPrism — it just fills in `photosorter_uid`), otherwise `createPhoto`
copies the original from `FilePath` into storage under the capture month and `buildPhoto`
assembles the row. **Both photo-sorter and Kukátko use SHA256**, so dedup via `file_hash` works
across importers. The satellites (`transferSatellites`) are carried along with the photo: the
embedding and faces are a core 1:1 transfer (a failure retries the whole photo), while
hashes/edits/markers/membership are best-effort (an error is logged). Embeddings (CLIP 768) and
faces (InsightFace 512) share models with Kukátko, so they are copied **without recomputation**.

## Photos — the `Photo` structure

The target is the `photos` table (migrations `0003`, `0004`, `0027`). `buildPhoto` maps both the
core and the curatorial fields **1:1 — the same column names on both sides**. The content
identity (`file_hash`/`file_path`/`file_size`) comes from the **freshly stored** file
(`stored`), so it describes the bytes actually on disk; the values are identical to
photo-sorter's (same content, same SHA256).

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Photo.UID` | `photosorter_uid` | **MAPPED** | Idempotence key (`GetByPhotosorterUID`) and dedup; stored via a pointer. |
| `Photo.FileHash` | `file_hash` | **MAPPED** | SHA256; dedup key (`resolvePhoto`→`GetByFileHash`). `file_hash` = `stored.Hash` of the same bytes. |
| `Photo.FilePath` | (copy source) → `file_path` | **MAPPED** | The path to the original from which the bytes are read (`copyOriginal`); the new storage layout goes into `file_path`. Also a fallback for the name (`originalName`). |
| `Photo.FileName` | `file_name` | **MAPPED** | `originalName(ps)`; on an empty name, `path.Base(FilePath)`, otherwise the UID. |
| `Photo.FileSize` | `file_size` | **MAPPED** | `file_size` = `stored.Size` of the copied bytes (the same content); the `ps.FileSize` field itself is not read, but it is equal. |
| `Photo.FileMime` | `file_mime` | **MAPPED** | `photoMime`: prefers `ps.FileMime`, otherwise the MIME sniffed from the stored bytes. |
| `Photo.FileWidth` | `file_width` | **MAPPED** | |
| `Photo.FileHeight` | `file_height` | **MAPPED** | |
| `Photo.FileOrientation` | `file_orientation` | **MAPPED** | |
| `Photo.TakenAt` | `taken_at` | **MAPPED** | Also drives the month in storage (`Store(..., takenAt, ...)`). |
| `Photo.TakenAtSource` | `taken_at_source` | **MAPPED** | Carried **directly** from the source (asymmetry: ppimport hard-stamps `taken_at_source` as `exif`). |
| `Photo.Title` | `title` | **MAPPED** | |
| `Photo.Description` | `description` | **MAPPED** | A direct transfer (photo-sorter has a single `description` field). |
| `Photo.Notes` | `notes` | **MAPPED** | The user's own note; carried directly. |
| `Photo.Lat` | `lat` | **MAPPED** | Transferred without the "zero trap" — GPS goes through directly (even `(0,0)`), see clue #3. But `location_source` is not stamped (see the target side). |
| `Photo.Lng` | `lng` | **MAPPED** | |
| `Photo.Altitude` | `altitude` | **MAPPED** | A pointer — `nil` = unknown, `0` = sea level is preserved (unlike ppimport, where `0` falls back to EXIF). |
| `Photo.CameraMake` | `camera_make` | **MAPPED** | |
| `Photo.CameraModel` | `camera_model` | **MAPPED** | |
| `Photo.LensModel` | `lens_model` | **MAPPED** | |
| `Photo.ISO` | `iso` | **MAPPED** | A pointer; `nil` = unknown (no "zero trap"). |
| `Photo.Aperture` | `aperture` | **MAPPED** | A pointer. |
| `Photo.Exposure` | `exposure` | **MAPPED** | |
| `Photo.FocalLength` | `focal_length` | **MAPPED** | A pointer. |
| `Photo.Exif` | `exif` | **MAPPED** | The raw EXIF JSON is copied **unchanged** (asymmetry: ppimport re-extracts EXIF from the downloaded file). |
| `Photo.Keywords` | `keywords` | **MAPPED** | The source TEXT[] `keywords` is joined and normalized (`exif.NormalizeKeywords`) into Kukátko's comma-separated column. **Newly read by this task** (was a GAP). |
| `Photo.Artist` | `artist` | **MAPPED** | IPTC credit `exif_artist`. **Newly read** (was a GAP). |
| `Photo.Copyright` | `copyright` | **MAPPED** | `exif_copyright`. **Newly read** (was a GAP). |
| `Photo.License` | `license` | **MAPPED** | `exif_license`. **Newly read** (was a GAP). |
| `Photo.Software` | `software` | **MAPPED** | `exif_software`. **Newly read** (was a GAP). |
| `Photo.Scan` | `scan` | **MAPPED** | The "scan of a physical original" flag. **Newly read** (was a GAP). |
| `Photo.Panorama` | `projection` | **MAPPED** | bool→string via `panoramaProjection` (`true`→`equirectangular`, otherwise empty). **Newly read** (was a GAP). |
| `Photo.Private` | `private` | **MAPPED** | |
| `Photo.ArchivedAt` | `archived_at` | **MAPPED** | The archive state is **preserved** (asymmetry: ppimport does not carry `archived_at`). |
| `Photo.UpdatedAt` | — (no column) | **WAIVED** | Drives the incremental watermark (paging `ORDER BY updated_at`, resume), it is not a photo column; `photos.updated_at` is the time of the row's own mutation. |

**34 MAPPED, 1 WAIVED, 0 GAP** at the model level (7 fields of IPTC credits/`keywords`/`scan`/
`panorama` added to the model and the mapper by this task). The remaining photo losses lie in the
columns the reader still does not read — see "What the reader drops at the DB boundary".

## Embeddings — the `Embedding` structure

The target is the `embeddings` table (migration `0006`, `halfvec` + HNSW). `transferEmbedding`
copies the vector 1:1; when photo-sorter has no embedding, Kukátko's `image_embed` job is
enqueued.

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Embedding.PhotoUID` | `embeddings.photo_uid` (kk) | **MAPPED** | Re-pointed to Kukátko's photo UID. |
| `Embedding.Vector` | `embeddings.embedding` | **MAPPED** | CLIP 768, 1:1 without recomputation (the same models). |
| `Embedding.Model` | `embeddings.model` | **MAPPED** | |
| `Embedding.Pretrained` | `embeddings.pretrained` | **MAPPED** | |

## Faces — the `Face` structure

The target is the `faces` table (migrations `0009`/`0010`). `transferFaces` + `convertFace`
copy each face 1:1, re-point `SubjectUID` and **preserve `MarkerUID`** (the marker migrates with
the same UID). `RecordFaceDetection` records a detection even for a zero face count so the photo
is not re-detected; a photo that photo-sorter never processed (`faces_processed` missing) is
handed to Kukátko's `face_detect`.

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Face.PhotoUID` | `faces.photo_uid` (kk) | **MAPPED** | Re-pointed to Kukátko's UID. |
| `Face.FaceIndex` | `faces.face_index` | **MAPPED** | |
| `Face.Vector` | `faces.embedding` | **MAPPED** | InsightFace 512, 1:1. |
| `Face.BBox` | `faces.bbox` | **MAPPED** | Normalized `[x,y,w,h]` (0..1). |
| `Face.DetScore` | `faces.det_score` | **MAPPED** | |
| `Face.Model` | `faces.model` | **MAPPED** | Also `faceModel()` determines the detection model. |
| `Face.MarkerUID` | `faces.marker_uid` | **MAPPED** | **Preserved** — markers migrate with the same UID, the cache stays valid. |
| `Face.SubjectUID` | `faces.subject_uid` | **MAPPED** | `remapSubject`: re-pointed to Kukátko's subject; an unknown subject → `nil`. |
| `Face.SubjectName` | `faces.subject_name` | **MAPPED** | A denormalized render hint. |
| `Face.PhotoWidth` | `faces.photo_width` | **MAPPED** | The bbox reference frame — see clue #7. |
| `Face.PhotoHeight` | `faces.photo_height` | **MAPPED** | ditto. |
| `Face.Orientation` | `faces.orientation` | **MAPPED** | ditto; carried with the bbox, so re-orientation does not shift the box. |

Everything MAPPED. The reader reads `faces_processed.face_count`, but `transferFaces` **drops**
it (it uses `len(faces)`) — see clue #8.

## Subjects / people — the `Subject` structure

The target is the `subjects` table (migration `0008`: `uid, slug, name, type, favorite,
private, notes, cover_photo_uid`). `findOrCreateSubject` matches an existing subject **by name
slug** (`people.Slugify`), otherwise creates a new one and **preserves the type and the flags**.

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Subject.Name` | `subjects.name` | **MAPPED** | Find-or-create key. |
| `Subject.Type` | `subjects.type` | **MAPPED** | `mapSubjectType` (`pet`/`other`/default `person`). **An animal keeps its type** (unlike ppimport, where everything is `person`). |
| `Subject.Favorite` | `subjects.favorite` | **MAPPED** | `subjects.favorite` is a **real subject column (global)**, not a per-user concern as with photos — hence it is filled (see clue #6). |
| `Subject.Private` | `subjects.private` | **MAPPED** | A private person stays private (unlike ppimport, where it is lost). |
| `Subject.Notes` | `subjects.notes` | **MAPPED** | |
| `Subject.UID` | — | **WAIVED** | Kukátko generates its own `uid`; the subject is paired by slug. |
| `Subject.Slug` | `subjects.slug` | **WAIVED** | Regenerated from the name (`people.Slugify`). |

**5 MAPPED, 2 WAIVED.** Caution: `type`/`favorite`/`private`/`notes` are set only on
**creation**. An existing subject (paired by slug — e.g. seeded earlier by ppimport as a bare
`person`) is taken **unchanged**; photo-sorter's richer type/flag does not overwrite it (see
Risks). The reader does not read the extra fields
`subjects.bio`/`about`/`alias`/`cover_photo_uid` — see "What the reader drops".

## Markers — the `Marker` structure

The target is the `markers` table (migration `0008`). `transferMarkers` migrates **every**
marker (idempotently via the preserved UID); `mapMarkerType` maps the type, and the subject is
re-pointed. **A key asymmetry vs. ppimport:** because this is a DB copy, *all* markers are
carried — named and unnamed, valid and invalid, `face` and `label` — not just named valid faces.

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Marker.UID` | `markers.uid` | **MAPPED** | **Preserved** — idempotence (`GetMarkerByUID`) and shared identity with `faces.marker_uid`. |
| `Marker.PhotoUID` | `markers.photo_uid` (kk) | **MAPPED** | Re-pointed. |
| `Marker.SubjectUID` | `markers.subject_uid` | **MAPPED** | `remapSubject`. |
| `Marker.Type` | `markers.type` | **MAPPED** | `mapMarkerType` (`label`/default `face`); **label markers are preserved** (ppimport discards them). |
| `Marker.X` / `Y` / `W` / `H` | `markers.x/y/w/h` | **MAPPED** | Normalized bbox (0..1). |
| `Marker.Score` | `markers.score` | **MAPPED** | Import provenance, not quality (0 = unrecorded); do not rank faces by it. |
| `Marker.Invalid` | `markers.invalid` | **MAPPED** | **Preserved** — the human "this is not a face" survives (asymmetry: ppimport filters `Invalid` out). |
| `Marker.Reviewed` | `markers.reviewed` | **MAPPED** | A direct transfer (ppimport derives `Reviewed = !Review`). |

Everything MAPPED (11 fields).

## Albums — the `Album` and `AlbumPhoto` structures

The target is the `albums` table (migration `0011`; `0022` removed `order_by`/`sort_order`) and
the `album_photos` join. `findOrCreateAlbum` matches/creates an album **by title**; an empty
title → the album is skipped. `mapAlbumType` maps **all** types including `month` (unlike
ppimport's `DefaultAlbumTypes`, where `month` drops out).

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Album.Title` | `albums.title` | **MAPPED** | Find-or-create key. |
| `Album.Description` | `albums.description` | **MAPPED** | |
| `Album.Type` | `albums.type` | **MAPPED** | `mapAlbumType`; unknown/empty → `album` (manual). Including `month`. |
| `Album.Private` | `albums.private` | **MAPPED** | |
| `Album.UID` | — | **WAIVED** | Kukátko generates its own `uid`; the album is paired by title. |
| `Album.Slug` | `albums.slug` | **WAIVED** | Regenerated from the title. |
| `AlbumPhoto.AlbumUID` | (key, remapped) | **MAPPED** | Via `maps.albums`. |
| `AlbumPhoto.PhotoUID` | (key → kk UID) | **MAPPED** | `AddPhoto` idempotent; a non-imported member is skipped. |
| `AlbumPhoto.SortOrder` | — | **WAIVED** | Kukátko sorts albums **chronologically** (`0022` dropped `album_photos.sort_order`). |

**Album: 4 MAPPED, 2 WAIVED. AlbumPhoto: 2 MAPPED, 1 WAIVED.** The reader does not read the extra
album columns (`category`, `cover_photo_uid`, `location`, `notes`, `filter`, `favorite`,
`album_order`/`order_by`) — see "What the reader drops".

## Labels — the `Label` and `PhotoLabel` structures

The targets are the `labels` table (`uid, slug, name, priority`) and `photo_labels`
(`source, uncertainty`), migration `0011`. `findOrCreateLabel` matches **by name**.

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Label.Name` | `labels.name` | **MAPPED** | Find-or-create key. |
| `Label.Priority` | `labels.priority` | **MAPPED** | |
| `Label.UID` | — | **WAIVED** | Its own `uid`. |
| `Label.Slug` | `labels.slug` | **WAIVED** | Regenerated from the name. |
| `PhotoLabel.PhotoUID` | (key → kk UID) | **MAPPED** | |
| `PhotoLabel.LabelUID` | (key, remapped) | **MAPPED** | Via `maps.labels`; `AttachLabel` idempotent. |
| `PhotoLabel.Source` | `photo_labels.source` | **MAPPED** | `mapLabelSource`: `manual`→manual, `ai`→ai, others→import. |
| `PhotoLabel.Uncertainty` | `photo_labels.uncertainty` | **MAPPED** | A direct transfer. |

**Label: 2 MAPPED, 2 WAIVED. PhotoLabel: 4 MAPPED.** The reader does not read the extra
`labels.description`/`categories`/`favorite` — see "What the reader drops".

## Perceptual hashes — the `Phash` structure

The target is `photo_phashes`. `transferPhash` is an idempotent upsert (best-effort).

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Phash.PhotoUID` | `photo_phashes.photo_uid` (kk) | **MAPPED** | |
| `Phash.Phash` | `photo_phashes.phash` | **MAPPED** | pHash (DCT). |
| `Phash.Dhash` | `photo_phashes.dhash` | **MAPPED** | dHash (gradient). |

## Edits — the `Edit` structure

The target is `photo_edits`. `transferEdit` is an idempotent upsert (best-effort). The
non-destructive crop/rotation/tone are carried 1:1.

| Source field | Target column | Verdict | Note |
| --- | --- | --- | --- |
| `Edit.PhotoUID` | `photo_edits.photo_uid` (kk) | **MAPPED** | |
| `Edit.CropX` / `CropY` / `CropW` / `CropH` | `photo_edits.crop_x/y/w/h` | **MAPPED** | Pointers — `nil` = no crop. |
| `Edit.Rotation` | `photo_edits.rotation` | **MAPPED** | 0/90/180/270. |
| `Edit.Brightness` | `photo_edits.brightness` | **MAPPED** | |
| `Edit.Contrast` | `photo_edits.contrast` | **MAPPED** | |

Everything MAPPED (8 fields).

---

## What the reader drops at the DB boundary (clues #1 and #3)

This is the core of the audit. `internal/photosorter` is the only gateway between photo-sorter
and Kukátko; what its `SELECT`s do not load does not enter the models, and the mappers have
nothing to carry. photo-sorter's actual schema (migrations `001–045`) contains **28 tables**;
the reader `SELECT`s from **12** and even from those it takes only a subset of columns.

### Tables nobody reads

The catalog bucket (tables with library data) has **14 tables**; the reader reads 12. Remaining:

| Table | Verdict | What is lost / why not |
| --- | --- | --- |
| `photo_files` | **GAP** | **The most serious.** The physical files of a shot — RAW+JPEG stacks, HEIC+JPEG sidecars, edited variants (`role` `original`/`sidecar`/`edited`). The migration copies only the primary original and creates **one** `photo_files` row in Kukátko; the sibling files are lost. **Hits** users with stacks (RAW next to JPEG, a live-photo clip). Kukátko has its own `photo_files` + `internal/stacks`. **Fix:** add listing of `photo_files` in the reader and, in `psimport`, copy the secondary files (roles `sidecar`/`edited`) as additional `photo_files` rows and let `internal/stacks` group them. |
| `era_embeddings` | **WAIVED** | Reference CLIP centroids of "eras" for period estimation — derived/recomputable data, not library content; Kukátko has no "eras" feature. |

The other unread tables (`users`, `sessions`, `photo_books`, `book_sections`,
`section_photos`, `book_pages`, `page_slots`, `book_chapters`, `text_versions`,
`text_check_results`, `album_share_links`, `smart_albums`, `audit_log`, `api_tokens`) are
**out of scope** (photo book, sharing, accounts, audit) — deliberately not carried, no loss of
library data. This confirms clue #1: the only unread table with library data is `photo_files`.

### `photos` columns: the earlier 7 GAPs now read (clue #3)

`photoColumns` (the reader) now takes **35 columns** after this task (was 28); the 7 previously
dropped credits/`keywords`/`scan`/`panorama` are now read and `buildPhoto` maps them. The
`photos` schema (after migrations `032`, `035`, `036`) still has more:

| photo-sorter column | Target in Kukátko | Verdict | Note |
| --- | --- | --- | --- |
| `exif_artist` | `photos.artist` | **MAPPED** | Target column (`0027`); newly read and mapped by this task. |
| `exif_copyright` | `photos.copyright` | **MAPPED** | ditto. |
| `exif_license` | `photos.license` | **MAPPED** | ditto. |
| `exif_software` | `photos.software` | **MAPPED** | ditto. |
| `keywords` (TEXT[]) | `photos.keywords` | **MAPPED** | TEXT[] joined and normalized (`exif.NormalizeKeywords`) into the comma-separated column. Newly read. |
| `scan` (bool) | `photos.scan` | **MAPPED** | Newly read. |
| `panorama` (bool) | `photos.projection` | **MAPPED** | bool→string via `panoramaProjection` (`true`→`equirectangular`). Newly read. |
| `favorite` (bool) | (per-user `user_favorites`) | **WAIVED** | Favorites in Kukátko are **per-user** — migration `0011` explicitly: "per-user favorites that **replace** photo-sorter's global `photos.favorite` flag". The job has nobody to attribute the flag to (same as `Photo.Favorite` for PhotoPrism). |
| `quality` (smallint) | — | **WAIVED** | A recomputable quality score; Kukátko does not model it. |
| `time_zone` / `taken_at_offset` | — | **WAIVED** | Kukátko keeps a canonical `taken_at` (timestamptz, an absolute instant); like `TakenAtLocal` for PhotoPrism. A minor fidelity loss when reconstructing local time (see Risks). |
| `uploaded_by` / `created_at` | — | **WAIVED** | Internal/DB-managed, not library content. |

**Credit-GAP fix — implemented.** The reader's `photoColumns` is extended with
`exif_artist/copyright/license/software`, `keywords`, `scan`, `panorama`; the corresponding
fields were added to `photosorter.Photo` and `buildPhoto` maps them onto the existing Kukátko
columns (`keywords` via `exif.NormalizeKeywords`, `panorama` via `panoramaProjection`). A purely
additive change to the reader + `buildPhoto`. Note: `psimport` writes metadata only when a photo
is **created** (`createPhoto`); a re-run skips a photo paired via `photosorter_uid`, so the
credits do not overwrite a Kukátko-side edit.

**Video columns** (`media_type`, `duration_ms`, `video_codec`, `audio_codec`, `has_audio`,
`fps`; clue #4): photo-sorter's `photos` is **image-only** — no video column exists. `psimport`
therefore does not fill them and **has nothing to fill them from** (nor does it run ffprobe).
`media_type` defaults to `image`. This is not a loss (the source does not model video), but it is
a limitation: if a video does slip into photo-sorter, it arrives as an image without
duration/codec (see Risks).

### Extra `subjects` / `labels` / `albums` columns (photo-sorter migration 037)

Migration `037` added richer subject, label and album metadata to photo-sorter; the reader
(`people.go`, `organize.go`) reads only the core of it.

| photo-sorter column | Target in Kukátko | Verdict | Note |
| --- | --- | --- | --- |
| `subjects.cover_photo_uid` | `subjects.cover_photo_uid` | **GAP** (retained) | The target column exists, but the fix requires remapping the photo UID **after the photos are imported** (subjects are created in `buildMappings` before the photo loop) — a new write-back pass, not a field fix. See "Scope and what remains GAP". |
| `subjects.bio` / `about` / `alias` | — | **WAIVED** | Free text about the person with no target in Kukátko (which has only `notes`, mapped from `Subject.Notes`). Modeling a person's bio/alias is a **product feature, not a migration fix**; a write-only column with no reader = a dead column (the `albums.category` standard). WAIVED until such a feature exists. |
| `albums.cover_photo_uid` | `albums.cover_photo_uid` | **GAP** (retained) | Like `subjects.cover_photo_uid` — remapping the photo UID after the photos are imported. |
| `albums.category` | — (no column) | **WAIVED** | An album category has nowhere to land in Kukátko (no column, UI, or query) — **the same product decision as `Album.Category` for PhotoPrism**. Adding a write-only column that nobody reads would be a dead column. |
| `albums.location` / `notes` | — | **WAIVED** | An album's free text/location with no target in Kukátko. A product feature, not a migration fix; WAIVED (the `albums.category` standard). |
| `labels.description` / `categories` | — | **WAIVED** | A label's description/category with no target in Kukátko (`labels` has only `slug, name, priority`). A product feature, not a migration fix; WAIVED (the `albums.category` standard). |
| `albums.filter` | (`internal/savedsearch`) | **WAIVED** | photo-sorter's "smart album" as a filter; Kukátko has smart albums separately (`saved_searches`), the album is migrated as static. |
| `albums.album_order` / `order_by` | — | **WAIVED** | Album ordering/sorting — Kukátko sorts chronologically (`0022`). |
| `albums.favorite` / `labels.favorite` | — | **WAIVED** | Kukátko's `albums`/`labels` have no `favorite` column (there is no favorite-album/label concept). |

Internal columns (`faces.id`/`dim`/`file_uid`, `embeddings.dim`/`created_at`, the
`created_at`/`updated_at` times, `album_photos.added_at`, `photo_labels.created_at`) are
DB-internal and are not meant to be carried — **WAIVED** (not listed as separate rows).

---

## The target side — `photos` columns `psimport` does not fill

For provability in both directions: each of the 55 inserted `photos` columns
(`photoInsertColumns`) is either mapped from a photo-sorter field (above), or
Kukátko-generated/own. `buildPhoto` sets 34 columns + `uid` (DB) and `media_type` (default
`image` in `Create`). `artist`, `copyright`, `license`, `software`, `keywords`, `scan` and
`projection` are **newly filled** by this task (they were GAPs). Not filled (default/empty):

| Column | Origin | Note |
| --- | --- | --- |
| `duration_ms`, `video_codec`, `audio_codec`, `has_audio`, `fps` | — | Video; the source is image-only, `psimport` does not run ffprobe. |
| `subject`, `color_profile`, `image_codec`, `camera_serial`, `original_name` | — | The source column does not exist. `file_name` carries the file name. |
| `location_source` | Not stamped | `psimport` fills `lat`/`lng`, but leaves the location provenance empty (asymmetry: ppimport stamps `exif`). Not a data loss, just an empty provenance. |
| `taken_at_estimated`, `taken_at_note`, `ai_note` | Kukátko-only | The source has none; default. |
| `uploaded_by` | — | The job has no user. |
| `photoprism_uid`, `photoprism_file_hash` | another import | Filled only by `ppimport`. |
| `metadata_extracted_at` | Kukátko-only | `nil` → schedules the `metadata` backfill. |
| `stack_uid`, `stack_primary` | Kukátko-only | Stack detection (`internal/stacks`). |
| `uid`, `created_at`, `updated_at` | DB | Generated on insert. |

---

## Verification of specific clues from the brief

### 1. Tables nobody reads — **confirmed: the only one is `photo_files` (GAP)**

photo-sorter's schema has 28 tables; 14 are out of scope (photo book/sharing/accounts/
audit/smart albums), deliberately not carried. Of the 14 catalog tables the reader reads 12.
Unread: **`photo_files`** (physical files / stacks — a real loss, GAP) and `era_embeddings`
(reference, WAIVED). `photo_files` is the biggest single gap of the whole audit — a whole table
of library data does not enter the models.

### 2. `TestBuildPhoto` was thin — **RESOLVED: the test extended to all fields**

Originally `TestBuildPhoto` (`helpers_test.go`) verified only **7** fields: `FileHash`,
`FilePath`, `FileSize`, `Title`, `Private`, `FileOrientation`, `PhotosorterUID` — so 20 of the
27 mapped were untested. **Fix (implemented):** `TestBuildPhoto` now assembles a fully populated
`photosorter.Photo` and compares the **whole** output of `buildPhoto` against the expected
`photos.Photo` (`reflect.DeepEqual`), so every mapped field (all 34 including the new
credits/`keywords`/`scan`/`projection`) is covered and a newly added/omitted field fails the
test — the completeness guardrail the audit recommended. The second level remains (a reflective
test over the models + "reader vs. source schema") — see "Test-coverage risk".

### 3. IPTC/XMP columns — **RESOLVED (MAPPED)**

photo-sorter **holds** the data (`exif_artist`/`copyright`/`license`/`software`, `keywords`,
`scan`, `panorama`), Kukátko has the target columns (`0027`). The reader now **reads** them
(`photoColumns`) and `buildPhoto` maps them (`keywords` via `exif.NormalizeKeywords`,
`panorama`→`projection` via `panoramaProjection`). A photo from photo-sorter thus arrives with
credits and tags. (`subject`, `color_profile`, `image_codec`, `camera_serial`, `original_name`
do not exist in photo-sorter — there was nothing to lose there.)

### 4. Video columns — **confirmed: the source does not model video, `psimport` does not fill**

photo-sorter's `photos` has no video column (`media_type`/`duration_ms`/`video_codec`/
`audio_codec`/`has_audio`/`fps`). `psimport` has nothing to fill them from and does not run
ffprobe; `media_type` defaults to `image`. Not a loss (an image-only source), see Risks about a
possible video in photo-sorter.

### 5. `Marker.Invalid` and `Marker.Reviewed` — **confirmed: `psimport` preserves them**

`transferOneMarker` maps both `Invalid: m.Invalid` and `Reviewed: m.Reviewed` directly. On top
of that, `psimport` carries **all** markers (including unnamed and `label` ones) because it
copies the DB. **An asymmetry vs. ppimport**, which filters out invalid/unnamed/`label` markers
(`isNamedFaceMarker`) and never sets `Invalid`. For migrating people, `psimport` is therefore
more faithful — the human "this is not a face" (`Invalid`) and the manual regions both survive.

### 6. `Subject.Favorite` — **a real subject column, not per-user (MAPPED)**

`subjects.favorite` is a global BOOLEAN column of the `subjects` table (migration `0008`), not a
per-user concern like `Photo.Favorite` (which has `user_favorites`). `findOrCreateSubject` fills
it (`people.Subject{Favorite: ps.Favorite, ...}`), likewise `Private`, `Type`, `Notes`. The
difference from photos: a **person's** favorite-ness is a property of the subject, a **photo's**
favorite-ness is a user↔photo relation. Hence `Subject.Favorite` = MAPPED, whereas
`Photo.Favorite`/`photos.favorite` = WAIVED.

### 7. Face geometry (`photo_width`/`height`/`orientation`) — **confirmed safe**

`convertFace` copies `BBox` (normalized 0..1) **together with** `PhotoWidth`, `PhotoHeight` and
`Orientation` — the whole reference frame of the box. Because the normalized box and its frame
are carried 1:1 (no recomputation of dimensions or re-orientation), the box cannot silently
"drift". Kukátko's `vectors.Face` has the corresponding columns (`bbox`, `photo_width`,
`photo_height`, `orientation`). No loss.

### 8. Classification of the enumerated fields

| Field | Verdict | Where in the document |
| --- | --- | --- |
| `AlbumPhoto.SortOrder` | **WAIVED** | Albums (chronological ordering, `0022`). |
| `Album.Slug` | **WAIVED** | Albums (regenerated). |
| `Label.Slug` | **WAIVED** | Labels (regenerated). |
| `Subject.Slug` | **WAIVED** | Subjects (regenerated). |
| `ps.UpdatedAt` (`Photo.UpdatedAt`) | **WAIVED** | Photos (watermark, not a column). |
| `faces_processed.face_count` | **WAIVED** | Faces — the reader reads it (`FacesProcessed`), but `transferFaces` drops it and uses `len(faces)`; the detection is recorded from the actual faces. |
| `PhotoLabel.Source` / `Uncertainty` | **MAPPED** | Labels (`mapLabelSource` / direct transfer). |

---

## Test-coverage risk (partially resolved)

`psimport` has integration tests for the transfer (embeddings, faces, markers, membership).
`TestBuildPhoto` is now a **completeness test** for `buildPhoto`: it compares the whole output
against the expected `photos.Photo`, so no mapped photo field passes silently (clue #2). The new
integration test `TestIntegration_photoMetadataSurvives` additionally verifies that credits/tags
survive a fresh migration and a re-run. What **still remains** missing: a completeness test over
the *other* models (`Face`/`Subject`/`Marker`/…) and a "reader vs. source schema" test — which
is exactly where the layer-B gaps arose (`photo_files`, the earlier IPTC).

**Recommendation (two levels):**

1. **A reflective table test over the models** (as in the PhotoPrism section): it walks the
   fields of `photosorter.Photo`/`Face`/`Marker`/`Subject`/`Album`/`Label`/`PhotoLabel`/
   `Phash`/`Edit` and fails until each is either in a mapper or on an explicit "WAIVED"
   allow-list referencing this document. Protects layer A.
2. **A "reader vs. source schema" test**: it compares the columns the reader `SELECT`s with the
   current photo-sorter schema (or at least an allow-list of deliberately unread columns/tables).
   Protects layer B — which is exactly where the losses are today.

## Risks and deliberate trade-offs

1. **`photo_files` is not migrated (left as GAP)** — a shot with a stack (RAW+JPEG, sidecar, an
   edited variant) arrives as a lone original; the secondary files are lost. The biggest
   structural loss; the fix is a whole table, not a field — see "Scope and what remains GAP".
2. **IPTC credits + `keywords`/`scan`/`panorama` — FIXED (MAPPED)** by this task: the reader
   reads them and `buildPhoto` maps them. The earlier silent loss at the reader boundary is
   gone.
3. **The extra metadata of `037`** — `bio`/`about`/`alias`, `description`/`categories`,
   `location`/`notes` are **WAIVED** (a product decision — no target in Kukátko; modeling them is
   a feature, not a migration fix). `cover_photo_uid` of subjects and albums is, by contrast, a
   real **GAP** (the target exists) left for a write-back pass — see "Scope and what remains
   GAP".
4. **An existing subject is not overwritten** — `findOrCreateSubject` sets
   `type`/`favorite`/`private`/`notes` only on creation. If the subject was seeded earlier (by
   ppimport as a bare `person`, or by a previous run), photo-sorter's richer type/flag does not
   catch it up. Recommendation: on a slug match, fill in the missing fields.
5. **`location_source` is not stamped** — migrated photos have `lat`/`lng` but an empty location
   provenance (ppimport stamps `exif`). Cosmetic, but inconsistent.
6. **`time_zone`/`taken_at_offset` are lost** — Kukátko keeps an absolute `taken_at`;
   reconstructing the original local time (the capture offset) is not possible. Low impact.
7. **Video in an image source** — if a video slips into photo-sorter, `psimport` stores it as
   `image` without duration/codec (ffprobe is not run). The source does not model video, so this
   is marginal in practice.
8. **Pairing subjects/albums/labels by name/title** — two different people of the same name
   merge into one subject (and conversely). The same behavior as ppimport; a property, not an
   identity guarantee.

## Scope and what remains GAP

This task fixed the gaps at the `photos` column level whose fix is purely additive and whose
target exists in Kukátko. The other gaps are either a product decision (resolved as WAIVED) or
changes larger than a field fix (left as GAP so they stay visible).

**Fixed (GAP → MAPPED):** `photos.exif_artist`→`artist`, `exif_copyright`→`copyright`,
`exif_license`→`license`, `exif_software`→`software`, `keywords`→`keywords` (TEXT[] joined +
`exif.NormalizeKeywords`), `scan`→`scan`, `panorama`→`projection` (via `panoramaProjection`).
The reader (`photoColumns` + `scanPhoto`), the model (`photosorter.Photo`) and the mapper
(`buildPhoto`) were extended; covered by the unit test `TestBuildPhoto` (the whole row) and the
integration test `TestIntegration_photoMetadataSurvives` (a fresh migration + a re-run).

**Resolved as WAIVED (product decision, no target in Kukátko):** `albums.category` (as with
`Album.Category` for PhotoPrism), and the free text of migration 037
`subjects.bio`/`about`/`alias`, `labels.description`/`categories`, `albums.location`/`notes`.
Adding a write-only column with no reader would be a dead column; modeling these concepts is a
product feature, not a migration fix. Reopen once Kukátko introduces such a feature (or a
production audit shows heavy use).

**Left as GAP (larger than a field fix — deliberately deferred):**

- **`photo_files` (the whole table).** The fix = list `photo_files` in the reader + copy the
  secondary files (roles `sidecar`/`edited`) as additional `photo_files` rows in `psimport` +
  let `internal/stacks` group them. That is a new table path, not adding a field; half-migrating
  it (only some files) would be worse than leaving the GAP visible. Recommended as a separate
  task.
- **`subjects.cover_photo_uid`, `albums.cover_photo_uid`.** The target exists, but the cover
  photo is a photo-sorter photo UID that must be remapped to Kukátko's **only after the photos
  are imported**. Subjects/albums are created in `buildMappings` **before** the photo loop, so at
  that moment the cover photo does not yet exist. The correct fix is a new, idempotent write-back
  pass after the photo loop (for each subject/album, find the Kukátko photo by `photosorter_uid`
  and set the cover) — a structural addition, not a field fix. Recommended as a separate task.
