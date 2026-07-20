# Backend packages

A descriptive reference overview of the Go packages. **These are not rules** — the rules live
in [`CLAUDE.md`](../CLAUDE.md). Record a new or changed package here and add one line for it
to `## Package map` in `CLAUDE.md`.

<!-- BODY BEGIN -->
- **Layout:** `cmd/kukatko/` (thin Cobra entrypoint: root + `serve` + `migrate` + `version`),
  `internal/server/` (chi HTTP server, graceful shutdown), `internal/version/`
  (ldflags-injectable `Version`/`Commit`), `internal/config/` (typed configuration,
  Viper, `Load()`), `internal/database/` (pgxpool wrapper `DB` with `Ping`/`Close`/`Pool`,
  embedded migration runner `Migrate`, pgvector types registered on every connection;
  SQL migrations in `internal/database/migrations/*.sql`), `internal/database/dbtest/`
  (integration test harness: `dbtest.New(t)`, `dbtest.TruncateAll`), `internal/auth/`
  (authentication/authorization: `Role` viewer/editor/admin/maintainer + `authorize`, bcrypt cost 12
  `HashPassword`/`CheckPassword`, UID/token generators, sliding-window `Limiter`,
  `Store` over pgx, `Service` orchestrating login/session/bootstrap/user management,
  `API` = HTTP handlers + RBAC middleware
  `RequireAuth`/`RequireWrite`/`RequireAdmin`/`RequireMaintainer`/`RequireImport` +
  `RegisterRoutes`; sessions and users in migration `0002_auth.sql`.
  **Strict role ladder** viewer < editor < admin < maintainer (migration `0036_role_maintainer.sql`
  redefines the CHECK on `users.role` and drops the `ai` role that `0023_role_ai.sql` had added earlier; `ai`
  accounts are promoted to maintainer): every role inherits the rights of the lower ones. `viewer` only reads; `editor` writes media/metadata;
  `admin` adds management (users, audit, trash); `maintainer` is the top — operations (imports, maintenance, status,
  backup/restore, jobs, processing). Predicates: `CanWrite()` = editor+, `IsAdmin()` = admin **or**
  maintainer (inheritance), `CanMaintain()`/`CanImport()` = maintainer only. Import is therefore an operational action
  for maintainers only (`requireImport`/`RequireImport`). **Only a maintainer** may create/promote to the
  `maintainer` role or modify a maintainer account — otherwise `ErrMaintainerRequired` (403); the actor's role is passed into
  the create/update validation from the context. Bootstrap creates the first user as a **maintainer**.
  **Admin note on a user** (`note`, migration `0021_user_note.sql`, nullable TEXT →
  `COALESCE(note,'')` in `userColumns`): `User.Note` is `json:"-"`, so it never leaks through
  `loginResponse` (`/auth/login`, `/auth/me`); admin endpoints add it back via
  `adminUserResponse` (embedded `User` + `note`). Validation `validateNote` → `ErrNoteTooLong`
  (`MaxNoteLen` = 1000 **runes**) → 400. `UpdateUserInput.Note` is a `*string`: `nil` = leave as is,
  `""` = clear (SQL `note = COALESCE($6::text, note)`).
  **Username length cap** `validateUsername` → `ErrUsernameTooLong` (`MaxUsernameLen` = 64 **runes**)
  → 400, enforced in `handleLogin` (on the normalized name, *before* it becomes a limiter key) and in
  `prepareNewUser` (so no account is created that could never log in). Together with the `Limiter`'s
  `maxKeys` = 8192 hard cap — insertion first drops expired keys, then evicts the least recently seen
  down to `evictTargetKeys` (¾ of the cap, so the O(n log n) sweep is amortised) — the login limiter's
  memory is bounded without waiting for the hourly `Cleanup` tick. Eviction ranks by a per-key
  `lastSeen` that is refreshed even on *blocked* attempts, so flooding fresh keys cannot evict, and
  thereby clear, an active block.
  **User-management audit** (`store_user_audit.go`): admin handlers call the audited variants
  `Service.CreateUserAudited`/`UpdateUserAudited`/`SetUserDisabledAudited`/`ResetPasswordAudited`,
  which via `Store.CreateUserAudited`/`UpdateUserProfileAudited`/`SetUserDisabledAudited`/
  `SetPasswordHashAudited` write a `user.create`/`user.update`/`user.disable`/`user.password` audit
  row `inAuditedTx` — **in the same transaction** as the change (rollback ⇒ no audit row). The non-
  audited `CreateUser`/`UpdateUser`/`SetUserDisabled`/`ResetPassword` remain for bootstrap and test
  seeding (they share the core `prepareNewUser`/`validateUserUpdate`/`invalidateIfDisabled`). The handler takes
  the actor from `UserFromContext` and builds `audit.FromRequest(r,uid).Entry(...)`; `details` carries
  `username`/`role` (create) or `role`/`disabled` (update/disable).
  **API tokens** (`apitoken.go`, `store_apitoken.go`, `service_apitoken.go`,
  `handlers_apitoken.go`, migration `0020_api_tokens.sql`): a long-lived bearer credential
  `kkt_<id>_<secret>` for non-interactive clients. `<id>` is the row PK (prefix `at`), so verification
  is **a single indexed lookup**, not a scan over hashes; `<secret>` carries 256 bits from `crypto/rand`.
  Only the **hex SHA-256** of the secret is stored (`hashAPITokenSecret`) — **deliberately not bcrypt**: bcrypt
  protects low-entropy passwords against a dictionary and is paid once per login, whereas a token is verified
  on *every* request and a 256-bit random secret has no dictionary; the comparison is constant-
  time (`subtle.ConstantTimeCompare`). The plaintext is returned **exactly once**, at creation.
  The `APIToken` model (`name`, `expires_at`, `last_used_at`, `revoked_at`) + pure predicates
  `Revoked`/`Expired`/`Active`; a token **inherits the owner's role** (no role column, no second
  permission system). `Service.AuthenticateAPIToken` returns, on any failure, the single
  `ErrInvalidAPIToken` (→ 401, never 403, the body doesn't distinguish the case) and stamps `last_used_at`
  at most once per `apiTokenUseInterval` (= a minute, mirrors `slidingRenewInterval`).
  `Store.CreateAPITokenAudited`/`RevokeAPITokenAudited` write the audit `inAuditedTx` — mutation and audit
  row in one transaction; `errNoAuditableChange` turns a repeated revocation into a no-op with no audit
  record. `bearerToken` parses `Authorization` case-insensitively per RFC 7235; a missing or
  non-Bearer scheme falls through to the cookie), `internal/photos/`
  (the photo-catalog core: typed models `Photo`/`PhotoFile`/`Phash`/`Edit`/`MetadataUpdate`
  (`Photo` also carries per-user annotation fields `Rating int`/`Flag string` — JSON `rating`/`flag`,
  analogous to `is_favorite`; they are not stored in `photos`, HTTP handlers fill them from `organize.Store`;
  `Photo` further carries **IPTC/XMP credits** `Subject`/`Keywords`/`Artist`/`Copyright`/`License`/`Scan`
  (editable → also in `MetadataUpdate`) and **machine-derived** `Software`/`ColorProfile`/
  `ImageCodec`/`CameraSerial`/`OriginalName`/`Projection` (**not** in `MetadataUpdate` — they describe
  the file, written by ingest/import; for the columns see `docs/ARCHITECTURE.md` §5.1);
  **approximate date** `TakenAtEstimated bool`/`TakenAtNote string` (JSON `taken_at_estimated`/
  `taken_at_note`, editable → also in `MetadataUpdate`): the date is an **estimate**, not a fact, plus
  free text explaining what the estimate rests on. `TakenAt` remains the sole anchor of sorting/timeline/filters —
  the flag is presentation, not a second date axis; the note lives only alongside the flag
  (`internal/photoapi` clears it when the flag is dropped)),
  `MediaType` image/video/live, `FileRole` original/sidecar/edited, UID generator prefix `ph`,
  `Store` over pgx with
  `Create`/`GetByUID`/`GetByFileHash`/`GetByPhotoprismUID`/`GetByPhotosorterUID`/`SetPhotoprismRef`
  (backfill `photoprism_uid`+`photoprism_file_hash` onto a photo deduplicated by SHA256 — the PhotoPrism
  import calls it so the next increment short-circuits on the uid instead of re-downloading)/`ListByUIDs`
  (batch lookup by uid, ignores unknown ones — for the similar API)/`FilterUIDs`
  (from a given set of uids returns those that pass the structural List filters — ignores sorting,
  pagination and `FullText`; companion to semantic search: the caller holds candidates from
  the embeddings index and filters them through the list filters, ordering by similarity itself)/
  `UpdateMetadata`/**`FillMissingMetadata(ctx,uid,MetadataFill) (changed,error)`**
  (fills **only empty** fields of a photo already in the catalog — from the import sidecar:
  `taken_at`+`taken_at_source` (only when `taken_at` is NULL or the source is **weak**, i.e.
  `unknown`/`filename` — `exif`/`manual` is never overwritten), `lat`+`lng` (**only as a pair**, half a
  fix is not a location), `altitude`, `title`, `description`; a single UPDATE whose WHERE repeats every
  guard → a photo with nothing to fill is **not written at all** (not even `updated_at`) and a second import run
  is a true no-op; the basis of `internal/dirimport`'s duplicate backfill — a folder imported
  *before* the sidecars were read can be fixed by a re-run, not by deleting and redoing)/
  **`FillFileMetadata(ctx,uid,FileMetadata) (filled,error)`**
  (the write side of the metadata backfill, `internal/metajob`: fills **only empty** IPTC/XMP and
  file-technical columns (`subject`/`keywords`/`artist`/`copyright`/`license`/`software`/
  `camera_serial`/`color_profile`/`image_codec`/`projection`/`original_name`) from a fresh extraction
  of the original and stamps `metadata_extracted_at = now()`. The SQL is built once from `fileMetadataColumns`
  (`buildFillFileMetadataSQL`), so the statement cannot diverge from the structure; **a self-join via the `o`
  subquery** gives the guards and `RETURNING` the *old* values (a plain `RETURNING` already sees the written row).
  An empty extraction never erases what the user wrote; `updated_at` moves **only** when something was
  actually filled, so a no-op backfill is invisible to every reader; `metadata_extracted_at`
  is always stamped — the file was read, whatever it said. Nothing outside `fileMetadataColumns` is
  touched: captions, `taken_at`, GPS, ratings and curation data are out of scope. `ErrPhotoNotFound`)/
  **`ApplyImportMetadata(ctx,uid,ImportMetadata) (changed,error)`**
  (the write side of an **import from a foreign catalog** (`internal/ppimport`: the PhotoPrism `Details` block +
  file-technical fields from the photo detail) onto a photo already in the catalog. Differs from
  `FillFileMetadata` in **precedence**: the source **owns** its fields, so a non-empty value overrides what
  is in the photo (just like camera/exposure from the first import) — `subject`/`keywords`/`artist`/
  `copyright`/`license`/`software`/`camera_serial`/`color_profile`/`image_codec`/`projection`/
  `original_name`. What it must **never** do is **destroy**: an empty value from the source leaves a non-empty
  column alone, `scan` can be **set, not unset**, and `notes` — Kukátko's own field — is
  **only filled into emptiness**, so the source won't overwrite a user's note. The SQL is built once from
  `importOwnedColumns` (`buildApplyImportMetadataSQL`) with the same **self-join `o` subquery** trick
  as fill; every guard is also the assignment condition → applying the same metadata twice writes
  **nothing** (not even `updated_at`), so a re-import is a true no-op. It does not touch captions, `taken_at`,
  GPS, ratings, favorites, or `ai_note`. `ErrPhotoNotFound`)/
  `Archive`/`Unarchive`/`Delete`/`List`+`Count` (filters archived/
  uploader/has-GPS/date-range `taken_after`+`taken_before`/camera/lens/substring search +
  **album/label scope** `AlbumUID`/`LabelUID` via a correlated `EXISTS` over `album_photos`/`photo_labels`
  — the basis of the shared scoped listing of an album's/label's photos through `GET /photos?album=`/`?label=`,
  plus **person/subject scope** `SubjectUIDs` (multi, AND combination: one correlated `EXISTS` over
  `markers` per subject, `invalid = FALSE`) — the basis of `GET /photos?person=` and the person filter facet,
  plus **place scope** `Country`/`City` (exact match via one correlated `EXISTS` over `photo_places`)
  — the basis of `GET /photos?country=&city=`,
  plus **per-user favorite scope** `FavoriteOf` via a correlated `EXISTS` over `user_favorites`
  — the basis of `GET /photos?favorite=true` and `GET /favorites`,
  plus **per-user rating filters** `RatedBy` (the current user's uid, scopes annotation/filters/sorting)
  + `MinRating` (rating ≥ n via a correlated `EXISTS` over `user_ratings`, ≤ 0 = no filter, a photo with no row
  = rating 0) + `Flag` (`pick`/`reject`/`eye` via a correlated `EXISTS`) — all active only when `RatedBy`
  is set, a photo with no row = rating 0 / flag `none`,
  sorting taken_at/created_at/uid/title/file_size **+ `rating`** (sort by the `RatedBy`
  user's rating via a correlated subquery over `user_ratings`, `NULLS LAST` — unrated last; active
  only with `RatedBy`) **+ `chronology`** (`SortByChronology`: `COALESCE(taken_at, created_at)` — a complete,
  stable chronological order, an undated photo falls back to its upload time; internal sort for
  the album view, not a public sort alias), pagination limit/offset; `Count` shares
  the `buildWhere` filters for `total`)/`Search` (Czech-aware fulltext over the generated `fts
  tsvector` column: `ListParams.FullText` via `websearch_to_tsquery('simple',
  immutable_unaccent(q))`, ordered by `ts_rank` (title>description>notes>file_name),
  diacritics-insensitive, honours all List filters + pagination; an empty query →
  `ErrEmptySearch`; `Count` with `FullText` returns the total thanks to the shared `buildWhere`),
  `AggregatePlaces(country)` (place hierarchy `[]CountryPlaces{Country,Count,Cities:[]CityCount}` —
  one `GROUP BY country, city` joining `photos`×`photo_places` over non-archived photos with place
  data, the hierarchy assembled in Go, ordered count desc/name; empty `country`='' = all countries, otherwise
  drill-down into the cities of one country; photos with empty `country` (no-GPS marker) are excluded — the basis of
  `placesapi`),
  `TimelineBuckets(params)` (monthly date-histogram `Timeline{Buckets:[]TimelineBucket{Year,Month,
  Count,Cumulative},Total}` — one `GROUP BY` by `date_part(year/month, taken_at)` over
  non-archived photos, ordered newest first (`year DESC, month DESC`, like the default grid),
  `Cumulative` (running sum of earlier=newer buckets) computed in Go and equal to the scroll index of
  the bucket's first image; shares `buildWhere` with `List`/`Count`, so the buckets exactly match
  the list; photos without `taken_at` don't fall into buckets (they sort last), but `Total` (via `Count`)
  includes them — the basis of `photoapi`'s timeline scrubber),
  `YearBuckets(params)` (year-histogram `Years{Years:[]YearBucket{Year,Count},Total}` in
  `store_years.go` — one `GROUP BY date_part('year', taken_at)`, ordered `year DESC`; shares
  `buildWhere` with `List`/`Count`, so a bucket's count = exactly what `List` returns for the same filters
  plus that year; `params.Sort`/`Order`/pagination are ignored, photos without `taken_at` don't fall into
  buckets, but `Total` (via `Count`) includes them — the basis of `photoapi`'s year facet),
  plus `CreateFile`/`ListFiles`,
  `ListArchivedUIDs(before,limit,offset)` (uids of archived photos oldest-archived-first,
  `before` nil = all / non-nil = only `archived_at <= before` retention cutoff — the basis of trash/purge),
  `CountPhotos()` (total photos incl. archived) + `ListFilePaths()` (all `photo_files.file_path`)
  — the basis of the post-restore integrity report (`backup.PhotoCatalog`),
  maintenance listers (`store_maintenance.go`): `ListPrimaryFiles()`,
  `ListPhotosMissingPhash(limit)` (uids of non-archived photos without a pHash — the basis of thumbnail
  backfill/repair), `ListPhotosMissingFileMetadata(limit)` (uids of non-archived photos with
  `metadata_extracted_at IS NULL`, i.e. whose **file has never been read** — the basis of metadata
  backfill; the predicate is a *marker*, not "the columns are empty", so the backfill converges even for photos
  without IPTC tags; it is covered by the partial index `idx_photos_metadata_pending` from migration
  `0028_photos_metadata_extracted.sql`, which is empty once the backfill is exhausted) and `ListActiveUIDs()`
  (uids of all non-archived photos — the basis of the forced full thumbnail/metadata backfill
  `?all=true`), **stack methods** (`store_stacks.go`, see `docs/ARCHITECTURE.md` §5.1):
  `ListStackCandidates` (not-yet-stacked non-archived photos for detection)/`StackInfoByUIDs`/
  `ListStackMembers` (stack members, **primary first** — the strip of variants)/`StackCounts` (member count
  per `stack_uid` — the tile badge)/`CreateStack`/`SetStackPrimary`/`UnstackMember`/`UnstackAll`
  (reversible bookkeeping over `stack_uid`/`stack_primary`), plus `ListParams.IncludeStackMembers`
  (lifts the shared visibility predicate `(stack_uid IS NULL OR stack_primary)` for a caller that wants
  **all** members) and the exported **`LeaveStackTx(ctx,tx,uid)`** (takes one photo out of its stack and
  repairs the remnant — dissolve below 2 members, re-elect a lost primary — on the caller's transaction).
  Every path that removes a photo from circulation calls it in the **same transaction** as the mutation:
  `Archive`/`ArchiveAudited`, `Delete`/`DeleteAudited` (and thus `internal/trash`'s retention purge),
  `internal/dupmerge`'s copy-archival and `internal/bulk`'s archive operation. Without it an archived or
  purged primary left its live siblings carrying a primary-less `stack_uid`, which the
  `(stack_uid IS NULL OR stack_primary)` gate hides from **every** default view — and after a purge
  irrecoverably, since `ListStackCandidates` skips rows that already carry a `stack_uid`. Unarchiving does
  not rejoin a stack: a restored photo comes back standalone and therefore visible.
  `SetPhash`/`GetPhash`, `SetEdit`/`GetEdit`; dedup on SHA256 `file_hash` + external IDs
  `photoprism_uid`/`photoprism_file_hash`(SHA1)/`photosorter_uid`; tables in migration
  `0003_photos.sql`: `photos`, `photo_files` (one primary/photo), `photo_phashes`,
  `photo_edits` (all-or-nothing crop, rotation 0/90/180/270); video columns in migration
  `0004_video.sql` (`media_type` image/video/live CHECK+partial index, `duration_ms`,
  `video_codec`, `audio_codec`, `has_audio`, `fps`); the generated `fts tsvector` column +
  GIN index and IMMUTABLE `immutable_unaccent` wrapper in migration `0007_fts.sql` (fulltext,
  `setweight` A/B/C/D, `to_tsvector('simple', immutable_unaccent(...))`, `file_name`
  normalized by a regex into tokens; the generated column keeps `fts` current even after editing
  metadata without a trigger); **performance partial composite indexes** in migration `0015_perf_indexes.sql`
  (`idx_photos_live_taken_at (taken_at DESC NULLS LAST, uid DESC) WHERE archived_at IS NULL` +
  companion `idx_photos_live_created_at` for `sort=added`) exactly match the most common grid
  ordering → a timeline page is an index scan **with no Sort** (EXPLAIN integration test
  `store_perf_integration_test.go`, see `docs/PERF.md`); FK `ON DELETE CASCADE`
  on satellites, `uploaded_by` `ON DELETE SET NULL`), `internal/storage/`
  (storage of originals: the `Storage` interface + **two** implementations — filesystem `FS`
  `NewFS(root)` and Cloudflare R2 `NewR2(R2Options)`. `storage.backend` (`fs` **default** /
  `r2`) chooses between them via `newStorage(cfg)` in `cmd/kukatko/storage.go`; above the interface no package can
  tell them apart. Common to both: `Store(ctx,src,takenAt,originalName)` streams + computes **SHA256**,
  layout `YYYY/MM/<filename>` (date from `taken_at`, fallback the import time); name collisions: identical
  content → `ErrAlreadyExists` (a dedup signal), different content → a numeric suffix `name_1.ext` **without
  overwriting**; `Open`/`Stat`/`Delete`/`Materialize` with paths confined to the root
  (`ErrInvalidPath`), a missing file/object wraps `os.ErrNotExist`; MIME from content (sniff
  512 B) + the extension as a hint (`mediaTypeByExt` for HEIC/RAW/video); sentinels
  `ErrAlreadyExists`/`ErrInvalidPath`/`ErrTooManyCollisions`; never holds the whole file in RAM
  (shared `streamToTemp` in `temp.go`).
  The trio for **bulk moves** (`put.go`): `Put(ctx,src,StoredFile)` writes a stream to a key
  **chosen by the caller** (which `Store` can't do — it derives the key from `taken_at` and the name), and only
  when the content matches the declared size and SHA256 — otherwise `ErrSizeMismatch`
  /`ErrHashMismatch` and **no usable object remains** (`FS` renames only after verification,
  `R2` in turn deletes a badly uploaded object: a leaked object is a lesser evil than an object whose metadata
  lie about its bytes). `Head(ctx,relPath)` returns the object identity (size, digest, MIME) without
  transferring the content — on `R2` one cheap metadata request, on `FS` a full read; an empty `Hash` =
  "digest unknown" (the object was written by a foreign tool), never "the digest matches". `Check(ctx)` verifies that the root
  exists / the bucket exists and keys reach it (`ErrBucketNotFound`), so an hours-long
  job fails in the first second on a typo, not only at the first upload. `storage.IsSystemic(err)`
  distinguishes an **unusable target** (bad keys, a missing/forbidden bucket, a broken endpoint; plus
  401/403 with an unknown code) from a per-object failure (a missing key, throttle, a truncated
  upload) — that is the decision "stop the whole run" vs. "collect it and keep going".
  **`FS`** publishes via an **atomic hard-link** through a temp in `<root>/.tmp`.
  **`R2`** (`r2.go`, the **minio-go v7** client — the same library as `internal/backup`, no new
  dependency) runs over a **private** bucket where the **object key = `photos.file_path` verbatim**
  (no new column, no key migration). A hard-link has no equivalent and isn't needed: `PutObject`
  is atomic, catalog dedup is held by the unique constraint on `photos.file_hash`. The upload goes through
  a staged temp file in `storage.temp_path`, because the key depends on the content — without the hash you can't
  distinguish a byte-identical re-upload from a same-named different file; SHA256 is stored as
  the user-metadata `x-amz-meta-sha256` and is the only way to detect dedup without downloading the object
  (the ETag is MD5, opaque for multipart). An object without that metadata (written by a foreign tool) is treated
  as different content → suffix.
  The interface **does not reveal the filesystem**: `URL(relPath)` returns the address the client reaches
  directly — `FS` returns `""` (originals on disk are not reachable over HTTP, the application serves them),
  `R2` returns a **signed short-lived URL** (or `""` when `media_base_url` is missing);
  `Materialize(ctx,relPath)` returns a **real local file** for tools that only understand a file
  name (exiftool, ffprobe, ffmpeg, heif-convert, vipsthumbnail) + a `cleanup` that the caller
  **always** calls (even on the error path, otherwise the remote backend leaks temps); `FS` **does not copy** —
  it returns the path of the original itself and a no-op `cleanup` (idempotent), so local development and tests
  stay zero-copy; `R2` downloads the object into `storage.temp_path` with the **extension preserved**
  (`imgconvert` dispatches RAW/video by it) and `cleanup` (idempotent via `sync.Once`) deletes it —
  even on the error path, where the partial file is deleted immediately.
  **Signed URLs** (`sign.go`, `URLSigner`): `https://<media_base_url>/<key>?exp=<unix>&sig=<hex>`,
  where `sig = HMAC-SHA256(secret, key + "\n" + exp)` — the signature covers both the key and the expiry, and the key is
  signed **unescaped** (the UTF-8 name is percent-encoded only when the path is rendered).
  `Verify(key,exp,sig)` compares **in constant time** against **two** secrets (the current +
  the previous), so rotating `url_signing_secret` has no window of broken URLs; signing always uses the
  current one. The signature is verified first (a forged key or expiry → `ErrInvalidSignature`), and only
  then the expiry (`ErrURLExpired`). Default TTL 1 h. The key **is not a secret** — without a valid
  signature the edge Worker rejects it. Neither the access key nor the signing secret ever reach a log
  or an error. **The Worker (verifier) lives in the infra repo** (`cloudflare-r2/`, Terraform), so
  the contract is held by the golden vectors `testdata/url_signature_vectors.json` — a published artifact against
  which both the Go signer (`sign_test.go`) and the Worker test; an algorithm change = regenerating the file
  and simultaneously updating the Worker. Integration tests `r2_integration_test.go` (tag `integration`) run against a real
  S3-compatible endpoint from `KUKATKO_TEST_S3_ENDPOINT` (MinIO is enough; without the variable they are skipped)),
  `internal/storagemigrate/`
  (a one-off **resumable** move of the library from local disk to object storage; drives
  `kukatko storage migrate-to-r2`, for the flags and billing see [`docs/OPERATIONS.md`](OPERATIONS.md).
  `New(Config)` → `Migrator`, `Run(ctx)` → `Result`. Config takes the narrow interfaces `Catalogue`
  /`Source`/`Destination` (not `storage.Storage`), so the whole pipeline can be tested with `FS`
  instead of a bucket; `Store` over a pgx pool is the production `Catalogue`. **Binding order per photo:**
  upload all objects (the original + the thumbnails already in cache — it generates no new ones) →
  `Head` reads them back and verifies size and SHA256 → `MarkMigrated` commits the row → only then
  the optional `Delete` of the local original. There is no path where the bytes live only where
  nobody has vouched for them. **The cursor** is `photos.storage_migrated_at` (migration `0019`), i.e.
  the `internal/importer` high-watermark **per row** — a scalar watermark would lie, because with
  `Concurrency > 1` photo N+1 commonly finishes before N; it pages by a `uid` cursor, so
  a failed photo doesn't fall into an infinite loop within the same run. An object that lies in the bucket with
  the correct size and digest is **not re-uploaded** (`Skipped`) — that is the whole difference between
  a free migration and a paid one. Per-photo failures are **collected** into `Result.Failures` and the run keeps
  going; `storage.IsSystemic` escalates an error to an immediate stop. `DryRun` touches neither the bucket, the DB, nor
  the disk — it only counts objects and bytes. The `Report` callback (throttled by `ReportEvery`, default
  15 s) prints progress + an estimate of the remainder. It streams; never holds a file in RAM. The integration test
  `storagemigrate_integration_test.go` (tag `integration`, needs MinIO **and**
  `KUKATKO_TEST_DATABASE_URL`) kills the run mid-photo, resumes it, and asserts that every object
  landed **exactly once** and that nobody deleted the original of a photo whose verification failed),
  `internal/mediaurl/`
  (mints client media addresses and stamps them onto photo payloads; the only decision is made by the storage
  backend via `URL`. `NewBuilder(store)` → `Builder` with `Thumb(uid,fileHash,size)` /
  `Download(uid,filePath)` (the client address: the signed Worker URL, otherwise a fallback to the own
  route `/api/v1/photos/...`), `Object(relPath)` / `ThumbObject(fileHash,size)` (the **raw** backend
  response — an empty string = "stream it yourself", non-empty = "redirect there"; the media routes use this)
  and `Decorate(list)` / `DecorateOne(&photo)`, which fill `Photo.ThumbURL`+`Photo.DownloadURL`.
  `Download` forces `?original=true` on the fallback so both branches mean the same thing (the stored original,
  never the rendering of a non-destructive edit). **A nil `*Builder` is valid** and behaves like a backend that
  publishes nothing → an API built without storage (test) still returns a working payload. `uid`/`size` are
  percent-encoded into the route. The grid size is `thumb.GridSize` (`tile_500`) — the only one the payload carries.
  **Authorization guards discovery**: a URL is minted only into a response the caller was already entitled to; the object
  is then guarded by the signature the Worker verifies. The package doc comment says so explicitly, because **an older design
  with a public bucket** made the archive just a presentation filter — that **no longer holds**,
  it is a real security boundary. It is called by `photoapi` (`annotate`/`handleUpdate`/`runArchive`/
  `resolveSimilar` + media routes), `peopleapi` and `globalsearchapi`; `cmd/kukatko/serve.go` passes them the
  storage as the shared `mediaStore`),
  `internal/thumb/`
  (the thumbnailer, **CGO-free**: a size registry `sizes`+`sizeOrder` in two modes
  `fit` (longest-side, preserves aspect, doesn't upscale) and `crop-square` (center-crop), default set
  `fit_720/1280/1920/2560/3840` + `tile_100/224/500`; cache layout under `storage.cache_path`
  `thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg` (shard from the hex SHA256), regenerable +
  **idempotent** (skips existing ones) + atomic write temp+rename; `Thumbnailer` =
  `New(store,cacheDir,WithConcurrency(n))` with the API `Generate(ctx,photo,sizes...)`/
  `GenerateAll(ctx,photo)` (a size→abs-path map, skips existing)/
  `RegenerateAll(ctx,photo)` (**force** — overwrites all sizes in-place with an atomic
  temp+rename, and republishes to object store; the basis of the "regenerate thumbnail" service action)/
  `Path(hash,size)`/`Open(hash,size)`;
  the package-level `RelPath(hash,size)` returns the same cache path relatively — it is also the **object key**
  of the thumbnail in the remote backend, which is why the layout is exported instead of derived a second time elsewhere;
  **publishing to object store**: after a size is written to cache, `publishSize` uploads it with `Put` under
  `RelPath` to the backend that publishes URLs (`store.URL(rel) != ""`, i.e. R2) — on FS it is a no-op;
  if the upload fails, the local file is deleted, so the size counts as not generated and the next
  `Generate` renders and uploads it again (invariant: a cached size on a publishing
  backend is always in the bucket too, so the client object URL resolves). This way a fresh ingest on R2
  gets its thumbnails into the bucket the same as `storage migrate-to-r2`;
  `GridSize` (`tile_500`) is the size the grid renders and that `thumb_url` carries in the payload;
  decode once per photo, parallel encode of the sizes (errgroup, default `GOMAXPROCS`,
  bound via `thumb.concurrency`),
  **EXIF orientation** (1–8) automatically; pure-Go JPEG/PNG/WebP + `golang.org/x/image`
  (`draw.CatmullRom` resize); **an optional vips engine** (`WithVips(bin)`, config `thumb.engine:
  vips`, `vips.go`): pure-Go decoding of large JPEGs is slow/memory-heavy on the Pi (~1 s / ~90 MB
  for `fit_720` from 12 MP, ~4 s / ~1.18 GB for `GenerateAll` — see `docs/PERF.md`), `vips` switches
  JPEG/PNG/WebP thumbnails to a **shell-out to `vipsthumbnail`** (`tryVips` → `vipsArgs`: fit `WxH>`
  without upscaling, crop `--smartcrop centre`, `[Q=…,strip]`, EXIF autorotation), **still without CGO**;
  pure-Go remains the default, vips **falls back per-photo** to pure-Go for other formats
  (HEIC/RAW/video) and on any failure → never changes the output, only the speed; `VipsAvailable(bin)`
  for the startup log; `Remove(hash)` deletes all cached sizes for a hash
  (idempotent, skips missing ones — thumbnail cleanup on photo purge); sentinels
  `ErrUnknownSize`/`ErrInvalidHash`/`ErrNotCached`;
  `SizeNames()`/`IsValidSize`), `internal/imgconvert/`
  (HEIC/RAW/video → a decodable JPEG, **shell-out**: `EnsureDecodable(ctx,path)` →
  (path, cleanup, err); **pure-Go passthrough** JPEG/PNG/WebP/**BMP/GIF/TIFF** (animated GIF →
  first frame; the decoders are registered by a blank import in `ingest` and `thumb`), **HEIC** via `heif-convert`
  to a temp JPEG, **RAW** (cr2/cr3/nef/nrw/arw/srf/dng/raf/orf/rw2/pef/srw/3fr/iiq/x3f/kdc/mrw/mef)
  pulls the embedded preview via `exiftool -b -PreviewImage` (fallback `-JpgFromRaw`/`-ThumbnailImage`)
  instead of demosaicing, **video** (`FormatVideo`) delegates to `video.ExtractPoster` (poster frame via
  `ffmpeg`) — the thumbnailer and pHash process the poster as a photo; `DetectFormat` prefers **magic
  bytes** whenever they recognize a directly decodable format (JPEG/PNG/WebP/BMP/GIF/TIFF/HEIC) — so a JPEG
  renamed to `.dng`/`.tif` is decoded by content, **not** sent down the RAW branch (where it would have no
  embedded preview); **exception: TIFF magic doesn't carry RAW** — most RAW containers are TIFF-based
  (`II*`/`MM*`), so the RAW **extension** takes precedence over TIFF magic and the file goes through embedded-preview,
  not as a flat TIFF; otherwise RAW is chosen only when magic recognizes nothing (other RAW headers) → falls back to
  the extension; `IsSupportedFormat`; sentinels
  `ErrConverterMissing`/`ErrUnsupportedFormat`/`ErrNoEmbeddedPreview`; a missing tool = a clear
  error), `internal/video/`
  (video without CGO, a **shell-out** to the FFmpeg suite: `Probe(ctx,path) (Metadata,error)` via
  `ffprobe -print_format json -show_format -show_streams` → `DurationMs`/`VideoCodec`/`AudioCodec`/
  `HasAudio`/`FPS` (rational parsing)/dimensions/`TakenAt` (creation_time)/GPS (ISO 6709), **fallback
  to `exiftool`** via `internal/exif` when `ffprobe` is missing; `ExtractPoster(ctx,path)` →
  a representative frame via `ffmpeg` (~1 s, fallback the first frame) to a temp JPEG + once-cleanup;
  `IsVideoPath`/`IsVideoExt`/`FFmpegAvailable`/`FFprobeAvailable`; **on-the-fly transcode for
  playback** (`transcode.go`): `IsWebFriendlyCodec(codec)` (h264/avc/vp8/vp9/av1/theora play
  natively in the browser, empty=unknown=no), `TranscodeArgs(src)` (ffmpeg → a **fragmented**
  H.264/AAC MP4 to `pipe:1` via `frag_keyframe+empty_moov`, audio optionally `0:a?` — testable
  without ffmpeg) and `Transcode(ctx,src) (*TranscodeStream,error)` (starts ffmpeg, `Read`/`Close` =
  `io.ReadCloser`, Close kills the process + reaps it; `ErrFFmpegMissing` when ffmpeg is missing); sentinels
  `ErrFFmpegMissing`/`ErrFFprobeMissing`/`ErrNoMetadataTool`/`ErrPosterFailed`), `internal/exif/`
  (extraction of EXIF/GPS metadata at import, **CGO-free**: `Extract(ctx,path) (Metadata,error)`
  → `TakenAt`+`TakenAtSource` (`exif`/`filename`/`unknown`), `Lat`/`Lng`/`Altitude`,
  `CameraMake`/`CameraModel`/`LensModel`, `ISO`/`Aperture`/`Exposure`/`FocalLength`,
  `Width`/`Height`/`Orientation`, `Mime` and the full EXIF as a JSON-able map — maps 1:1 onto
  `photos.Photo`; **primarily** a shell-out `exiftool -json -n`, **fallback** pure-Go
  `rwcarlsen/goexif` (+ `image.DecodeConfig`/`http.DetectContentType` for dimensions/MIME) when
  `exiftool` is missing/fails; GPS rational→decimal degrees per the `N/S/E/W` refs, `GPSAltitudeRef=1`
  → negative altitude; `taken_at` from `DateTimeOriginal` (zone-less = UTC), otherwise from the file name,
  otherwise `unknown`; a file without EXIF (PNG) = zero values, **not an error**;
  **IPTC/XMP + file-technical fields** (`iptc.go`, mapped onto the same-named `photos` columns):
  `Subject` ← `Subject`(scalar)/`Headline`/`XPSubject`/`ObjectName`, `Keywords` ←
  `Keywords`/`Subject`(**list**)/`XPKeywords`, `Artist` ← `Artist`/`Creator`/`By-line`/`XPAuthor`,
  `Copyright` ← `Copyright`/`Rights`/`CopyrightNotice`, `License` ←
  `License`/`UsageTerms`/`WebStatement`, `Software` ← `Software`/`CreatorTool`/`ProcessingSoftware`,
  `CameraSerial` ← `SerialNumber`/`BodySerialNumber`/`InternalSerialNumber`, `ColorProfile` ←
  `ICCProfileName`/`ProfileDescription`/`ColorSpace` (a numeric code → name: `1`=sRGB, `2`=Adobe RGB,
  `65535`=Uncalibrated; an unknown code → empty, not a bare digit), `ImageCodec` ←
  `Compression`(JPEG codes 6/7/34892)/`FileType`/`FileTypeExtension`/MIME → a short lowercase token
  (`jpeg`/`heic`/`png`/`webp`/`avif`/`tiff`/`gif`/`bmp`/`raw`, every vendor RAW = `raw`; video →
  empty), `Projection` ← `ProjectionType` (XMP GPano); in every string the **first non-empty**
  value **wins**, everything is trimmed and junk (`""`/`unknown`/`0`) is discarded. **Scalar vs. list for `Subject`** is
  the only non-trivial branch: scalar = IPTC headline → `Subject`, list = XMP `dc:subject` → `Keywords`
  (comma-separated, trimmed, **deduplicated, order preserved**; a scalar tag is split on `,`/`;`).
  `scan` is **never derived** — it's a manual user flag. The pure-Go fallback handles only baseline
  TIFF/EXIF tags (`Artist`/`Copyright`/`Software`/`ColorSpace` + codec from MIME); it doesn't read IPTC/XMP
  segments, so the other fields stay **empty, not wrong**.
  **Exported normalizers for importers**: `NormalizeKeywords(raw) string` (a foreign comma/semicolon
  list → exactly the shape the own extraction stores: trim, junk gone, dedup, order preserved,
  joined by commas) and `CodecToken(s) string` (any codec spelling — `HEIC`, `image/x-canon-cr2`,
  PhotoPrism's `jpeg` — → a token for `image_codec`, otherwise empty). `internal/ppimport` runs them through
  these so an imported photo has its columns in the **same vocabulary** as an extracted one — a column that after
  extraction says `jpeg` and after import `JPEG` isn't one column, but two), `internal/phash/`
  (perceptual hashes, **CGO-free**: `Compute(img) Hashes{Phash,Dhash int64}` — **pHash** via
  a 2-D DCT 32×32 → low-freq 8×8 block with a median-without-DC threshold, **dHash** gradient 9×8; `Distance(a,b)`
  = Hamming distance via `bits.OnesCount64`; near-dup = a small distance), `internal/ingest/`
  (the upload/ingest pipeline: `Service` = `New(Config{Storage,Photos,Thumbnailer,Enqueuer,Duplicate,
  MaxFileSize,TempDir})` with **`IngestFile(ctx,src,Request{Filename,UploadedBy,Sidecar})`** (the full form;
  `Ingest(ctx,src,filename,uploadedBy)` = a thin wrapper for an upload without a sidecar) `→ FileResult`
  — streams to a temp +
  SHA256, exact-dup check, metadata (`mediaMeta`: **photo** → EXIF; **video** per `video.IsVideoPath`
  → `media_type=video` + `video.Probe`, requires `ffmpeg` otherwise a per-file error `ErrFFmpegMissing`,
  `taken_at` falls back to the original name via `exif.FilenameTakenAt`),
  **`applySidecar`** (if the file has a sidecar — `internal/sidecar`, see below — it is applied **before**
  storing the original: the merged `taken_at` decides the `YYYY/MM`, so a Takeout photo with a stripped
  EXIF falls into the month it was **created**, not when it was imported; `Title`/`Description` from the sidecar
  go into `photos` — they have no equivalent in EXIF), `storage.Store` (`YYYY/MM`),
  insert `photos` (incl. video columns; `buildPhoto` also fills `original_name` = the base name of the name
  the upload arrived under — the storage layout renames the file, this is the only trace of the original
  name — and via `applyFileMetadata` the **IPTC/XMP and file-technical columns** from `exif.Metadata`
  (`subject`/`keywords`/`artist`/`copyright`/`license`/`software`/`camera_serial`/`color_profile`/
  `projection`; `image_codec` = the token from extraction, fallback the MIME subtype (`image/jpeg` → `jpeg`),
  empty for video — the clip's compression belongs in `video_codec`) and **`metadata_extracted_at = now()`**:
  the file was read, so the metadata backfill (`internal/metajob`) no longer schedules this photo)
  +primary `photo_files`, pHash/dHash → `photo_phashes`
  (from the poster frame for video), thumbnails (the poster for video), enqueue of jobs (the poster frame takes part in
  search/people); **per-file** `FileResult{Filename,Status,
  Outcome (created/duplicate/error),PhotoUID,Error,Warnings}` — never returns an error, everything is in the result;
  **race**: concurrent identical uploads → one photo (storage hard-link + unique `file_hash`), the loser
  a clean duplicate; **near-dup warning** config-gated via `photos.NearestPhash`; `JobEnqueuer` =
  a TODO hook `EnqueueImageEmbed`/`EnqueueFaceDetect`, default `NopEnqueuer` until the queue exists;
  `API` = `NewAPI(svc, requireWrite)` + `RegisterRoutes` mounts `POST /upload` behind `RequireWrite`;
  multipart is streamed part-by-part, never the whole file in RAM),
  `internal/sidecar/`
  (**metadata next to the media** — reads what the export wrote *into a file next to* the photo, not into it:
  `Read(ctx,path) (Metadata,error)` by extension — **`.json`** = Google Photos (Takeout;
  `photoTakenTime` → `TakenAt`, `description`, `geoData`/`geoDataExif` → `Lat`/`Lng`/`Altitude`,
  `favorited`, `people[].name`; **an exact 0/0 means "unknown"**, not a point in the Gulf of Guinea;
  `title` is the file name, **not** a caption) and **`.xmp`** = Apple/Lightroom (via **exiftool**,
  i.e. `exif.Extract` over the sidecar: date, GPS, `dc:title`/`dc:description`, `dc:subject` → `Keywords`,
  `xmp:Rating` 0–5 (a negative "rejected" = 0), `dc:creator`/`Artist`); `.aae` is a description of an **edit**,
  not metadata → never read;
  `Match(media,sidecars) Matches{Pairs,Orphans,Missing}` pairs **within a directory** and survives the whole
  minefield of Takeout names: `IMG.jpg.json`, `IMG.jpg.supplemental-metadata.json` (even ones **truncated**
  by Google due to the name-length limit: `…supplemental-me.json`, `IMG_1234.jp.json`), a shifted copy-index
  (`IMG_1234(1).jpg` ↔ `IMG_1234.jpg(1).json`), Apple `IMG.HEIC.xmp` and `IMG.xmp`; **an exact match takes
  precedence** over a truncated one, an **ambiguous** truncated match pairs nothing (better to report than
  to sew one photo's history onto another), for a Live Photo pair the **photo wins over the video**, for a photo
  with both JSON and XMP the JSON wins; Takeout's own `metadata.json` (album) is not a sidecar → **ignored**
  (and not reported as an orphan), **albums are never created from an export**;
  `Apply(*exif.Metadata, Metadata)` resolves **precedence**: EXIF is primary, the sidecar **fills gaps**
  — but the sidecar **wins** when the EXIF date is missing, is only guessed from the name (`SourceFilename`), or
  lies **more than 24 h behind** the sidecar (that is the *export* date, which Takeout wrote into
  `DateTimeOriginal` on re-encode; the window is a day, because EXIF carries no zone); the source is recorded as
  **`exif.SourceSidecar`** (`taken_at_source = "sidecar"`); GPS is filled **only as a pair** and only when
  the file has none; people's names and keywords are **only stored** in the EXIF document under the key `Sidecar`
  — Google has no face boxes, so from them **no subject or marker may be created**),
  `internal/dirimport/`
  (**import of a directory from disk** — `kukatko import dir <path>`; `Service` = `New(Config{Ingest,Runs,
  Photos,Filler,Curation,Albums,Labels,Concurrency,Logger})` with `Import(ctx, Options{Root,Recursive,
  DryRun,NoSidecars,Album,Labels,UploadedBy,Progress}) (Result,error)`; **no second pipeline** —
  every media file goes
  through `ingest.IngestFile` exactly like an upload (stream, SHA256 dedup, metadata, `YYYY/MM`, thumbnails,
  jobs), all behind the interfaces `Ingester`/`RunStore`/`PhotoLookup`/`PhotoFiller`/`CurationStore`/
  `AlbumStore`/`LabelStore` → unit-testable
  with fakes; **sidecars** (`internal/sidecar`): `buildSidecarIndex` pairs media with neighbouring
  `.json`/`.xmp` **before** the first file, each sidecar is read in the worker and goes with the photo into
  `ingest.Request.Sidecar`; **per-user marks** from the export go to the importing user
  (Google `favorited` → `AddFavorite`, XMP rating → `SetRating`; **only for a newly created photo** —
  re-importing an old export must not restore a favorite the user has since cleared); for a **duplicate**
  `photos.FillMissingMetadata` is called → a folder imported *before* the sidecars were read is
  fixed by a mere re-run (nothing is created, **only gaps** are filled, the second run writes nothing);
  `Result.Sidecars` = `SidecarReport{Matched,Applied,Unreadable,Orphans,Missing}` — **whatever didn't pair
  is named**: a sidecar with no photo, a photo with no sidecar (only in directories that have some sidecars —
  in a folder straight from the camera it would be just noise), and a sidecar that couldn't be read (the photo is imported
  **anyway**, it just loses its date); `--dry-run` pairs **and reads** the sidecars (the report is the one a
  real run would give), `NoSidecars` disables them; **idempotent** (identity = SHA256 → a re-run reports duplicates and writes nothing) and
  **resumable** (each file committed separately, a crash/Ctrl-C leaves the imported ones in the library,
  a re-run finishes the rest); originals are **copied, never moved or modified**;
  `plan()` walks the tree lexically (deterministically) and classifies skip reasons: `SkipHidden` (dot-files),
  `SkipJunk` (`@eaDir`, `__MACOSX`, `Thumbs.db`, `.DS_Store`, `desktop.ini`, Picasa),
  `SkipSidecar` (`.xmp`/`.json`/`.aae`/`.thm` — sidecars **are not media**, so they aren't imported;
  metadata **is read** from `.xmp`/`.json` and attached to the neighbouring photo, see `internal/sidecar` above),
  `SkipUnsupported` (outside `imgconvert.IsSupportedFormat`, i.e. HEIC/RAW/video go in),
  `SkipSymlink` (**symlinks are skipped, never followed** → the walk can't loop; only the
  root itself is resolved via `EvalSymlinks`) and `SkipEmpty` (0 B); hidden/junk directories are pruned whole,
  `--no-recursive` prunes everything below the root; a per-file error falls into `Counts.Failed` and the run **continues**
  (one broken JPEG must not bring down a 2000-file run); fan-out `DefaultConcurrency` 3 /
  `MaxConcurrency` 8 (thumbnailing is memory-expensive, the 16 GB box shared with everything else);
  `--album`/`--labels` are **resolved up front** (uid or name; whatever doesn't exist is created — a typo
  thus fails immediately, not after two thousand files) and assigned to **duplicates** too (`AddPhoto`/`AttachLabel`
  are idempotent → re-running a folder into an album is the way to fix a forgotten `--album`);
  the run is recorded via `internal/importer` as `importer.SourceFolder` (migration
  `0026_import_runs_folder.sql` extends the CHECK on `import_runs.source`), **without a watermark** (a folder
  has no source time, dedup is done by SHA256), the tally is checkpointed every 25 files;
  `Counts{Imported,Duplicates,Skipped,Failed,ByReason}` → `importer.Counts` (both duplicates and skipped
  junk fall into `skipped`, `updated` is always 0); a cancelled context → `ErrInterrupted` + the run closed
  as `failed` (no forever-"running" row); `--dry-run` only **hashes files and looks them up in the catalog**
  (new/duplicate) and **writes nothing at all** — not even `import_runs`), `internal/photoapi/`
  (a read/curation HTTP API over the catalog: `NewAPI(Config{Store,Storage,Thumbnailer,Similar,
  Embedder,Faces,Favorites,Ratings,RequireAuth,RequireWrite,RequireAdmin,RequireDownload})`
  — **`RequireAdmin` guards only the irreversible trash operations** (`POST /trash/empty`, per-photo
  `POST /photos/{uid}/purge` delete originals, hence tightened from write to admin); archiving
  (reversible soft-delete) stays `RequireWrite`, `GET /trash/info` `RequireAuth` — `RegisterRoutes` mounts `/photos`
  **, `GET /photos/timeline`, **`GET /photos/years`**, `GET /search` and `GET /favorites`**; `parseListParams`
  validates the query → `photos.ListParams` (`limit`≤500/`offset`, `sort`
  newest/oldest/taken_at/added/title/size**/rating** + `order` — **`album` scope overrides both**
  to `SortByChronology`+`asc` (an album is always chronological, the defaults of other views are unchanged),
  `archived` false/true/only,
  `has_gps`, `taken_after`/`taken_before`, `camera`, `lens`, `uploader`, `q`, **`year` (four-digit
  1000–9999) → `Year`**, **`album`/`label`
  scope** → `AlbumUID`/`LabelUID`, **`person` scope (multi, AND)** → `SubjectUIDs`,
  **`country`/`city` place scope** → `Country`/`City`,
  **per-user `min_rating` (int) + `flag` (`pick`/`reject`/`eye`)**
  → `MinRating`/`Flag`; invalid → 400) + `favoriteRequested` parses `favorite=true`
  → the handler sets per-user `FavoriteOf` to the current user; the list/search/favorites handlers
  set `RatedBy` to the current user, so `min_rating`/`flag`/`sort=rating` are scoped to them;
  the list returns `{photos,total,limit,offset,next_offset}` (each photo annotated with `is_favorite`
  + per-user `rating`/`flag` via the shared `annotate`: `FavoriteStore.FavoritedAmong` +
  `RatingStore.RatingsAmong`, a photo with no row = rating 0 / flag `none`) for infinite scroll;
  **per-user favorites** (`favorites.go`): `PUT`/`DELETE /photos/{uid}/favorite` (any logged-in user,
  idempotent toggle → 204, 404 missing photo, 503 without a `Favorites` backend) + `GET /favorites`
  (the current user's favorites in the list-endpoint shape, equivalent to `?favorite=true`);
  the `FavoriteStore` interface (satisfied by `organize.Store`) is nil-safe (not wired → `is_favorite`
  false, favorite endpoints 503);
  **per-user ratings** (`ratings.go`): `PUT /photos/{uid}/rating` `{rating?:0..5, flag?:none|pick|reject|eye}`
  (any logged-in user, at least one value, validated up front → 400 invalid, 404 missing photo, 503 without a
  `Ratings` backend; sets rating and/or flag via `SetRating`/`SetFlag`) + `DELETE /photos/{uid}/rating`
  (idempotent clear via `ClearRating` → 204); the `RatingStore` interface (satisfied by `organize.Store`,
  `SetRating`/`SetFlag`/`ClearRating`/`RatingsAmong`) is nil-safe (not wired → rating 0 / flag `none`,
  rating endpoints 503);
  **thumbnail regeneration** (`thumbnail.go`): `POST /photos/{uid}/regenerate-thumbnail` (editor/admin via
  `RequireWrite`, the `ThumbnailRegenerator` interface satisfied by `*thumbjob.Service`, nil-safe → 503) synchronously
  overwrites the thumbnails + pHash via `ForceRegenerate` and returns `{status,sizes}` (200), 404 missing photo,
  **422** `thumbjob.ErrRegenerateFailed` (the original is missing/undecodable); best-effort audit `photo.thumbnail`
  via `AuditRecorder` (`*audit.Store`, a failure is only logged — the thumbnail is already regenerated);
  `GET /photos/years` (`handleYears`, `years.go`) = a **year-histogram** for the library's year facet
  → `photos.Store.YearBuckets` → `{years:[{year,count}],total}`; takes the same filters as the list
  (incl. per-user `FavoriteOf`/`RatedBy`), but **zeroes out `params.Year` itself** — a facet must not narrow
  its own offering; an invalid param → 400;
  `GET /search?q=&mode=` (`handleSearch`, `search.go`) = **semantic + hybrid search**,
  `mode` = `fulltext`|`semantic`|`hybrid` (default `hybrid`, unknown → 400), `q` required
  (empty/whitespace → 400): **fulltext** orders by `ts_rank` via `store.Search`; **semantic**
  embeds `q` via `TextEmbedder` (sidecar) → `Similar.FindSimilar` (cosine HNSW) →
  filters the candidates through `store.FilterUIDs` → orders by distance; **hybrid** merges both
  rankings with **Reciprocal Rank Fusion** (`fuseRRF`, constant `rrfK=60`), dedups, orders by
  the fusion score. All modes honour List filters + pagination (`sort`/`order` ignored),
  the response = the list shape + `mode` (effective) + `degraded`; **box offline** (`Embedder` nil or
  `embedding.IsUnavailable`) → `semantic`/`hybrid` fall back to fulltext with `degraded: true`;
  the `TextEmbedder` interface (fakeable, satisfied by `embedding.Client`); `PATCH` is
  partial via raw-key presence (an omitted field unchanged, `null` clears a nullable one, coordinate
  validation); media `thumb/{size}`+`download` **stream** via `io.Copy` with `streamMedia`
  (`Cache-Control`/`ETag`/`304`, `Content-Length` from the DB, the thumbnail generated on-miss),
  guard `RequireAuthOrDownloadToken` = a session cookie or `?t=download_token`; **video streaming**
  (`video.go`): `GET /photos/{uid}/video` streams video **with HTTP Range** via `http.ServeContent`
  (206 partial, `Accept-Ranges`, seek, If-Range/If-None-Match, memory-bounded from `*os.File` via
  `storage.Materialize`, once per request — the transcode fallback shares it too) for inline HTML5
  playback; a live photo serves its **motion clip** sidecar
  (`pickMotionClip` by video MIME/extension), a still image → 404; **on-the-fly transcode** gated by
  `VideoConfig`/`video.transcode` (default off) + `video.IsWebFriendlyCodec` + `video.FFmpegAvailable`
  → `video.Transcode` (H.264/MP4 progressive, no range, `no-store`), falls back to the original when
  ffmpeg fails or the codec is unknown; **the non-destructive
  edit** via `Organizer` (the detail's album/label chips); **the detail's uploader** via the `UserResolver`
  interface (satisfied by `auth.Store.GetUserByUID`, wired by `buildPhotoAPI`): `handleDetail`
  resolves `photo.UploadedBy` → `uploader{uid,name}` (`name` = `display_name`, fallback `username`),
  nil-safe (not wired / no uploader / an unresolvable user → `uploader` omitted, only on the
  detail, no N+1 in the list); **the detail's place** (`place.go`) via the `PlaceResolver` interface
  (satisfied by `places.Store.GetPlace`): `writeDetail` attaches `place{country,region,city,place_name}`
  from the `photo_places` cache — **cache-read only, the detail never geocodes** (mapy.com credits are
  metered; the on-demand lookup stays in `mapsapi`), nil-safe just like the uploader and also omitted for a
  "processed" marker (a row with all levels empty); and `EditService`/`edit.go`+`media_edit.go`
  (`GET`/`PUT /photos/{uid}/edit`, download honours the edit via `internal/photoedit`)), `internal/photoedit/`
  (**CGO-free application of a non-destructive edit** to a decoded image for download/preview: `Apply(img,
  photos.Edit) image.Image` applies **crop** (normalized `[x,y,w,h]` 0..1), **rotation** 0/90/180/270
  and **brightness/contrast** (a linear scale around 0.5, maps 1:1 to the frontend CSS `brightness(1+b)`/
  `contrast(1+c)`), pure-Go via `golang.org/x/image`; `IsIdentity(edit)` skips a no-op; `orient.go`
  = EXIF orientation; identity = passthrough of the original, otherwise render to a JPEG), `internal/trash/`
  (permanent deletion (purge) of soft-deleted photos, all behind the interfaces `PhotoStore`/`FileStorage`/
  `ThumbStore`/`RemoteRemover` (unit-testable with fakes): `Service` = `New(Config{Photos,Storage,
  Thumbnailer,Remote?,RetentionDays,BatchSize,Logger})` (panics on nil Photos/Storage/Thumbnailer);
  **purgeOne** deletes a photo's artifacts (the original via `Storage.Delete`, the cached thumbnails via
  `Thumbnailer.Remove`, optionally the S3 object via `RemoteRemover`) **and then** the DB row via
  `photos.DeleteAudited(uid,entry)` — deletes the row (cascading embeddings/faces/markers/album_photos/
  photo_labels/phashes/edits/favorites via `ON DELETE CASCADE`) **and writes a `photo.purge` audit row
  in the same transaction** (durable-audit; rollback ⇒ no audit row); artifacts first, so an
  interrupted purge leaves a re-purgeable row instead of dangling files; idempotent (a missing
  file/`os.ErrNotExist`/`thumb.ErrInvalidHash` is ignored); `PurgePhoto(uid,meta)` (404
  `photos.ErrPhotoNotFound`, `ErrNotArchived` for a live photo), `EmptyTrash(meta)` (purge of all
  archived) and `PurgeExpired()` (only `archived_at` older than `RetentionDays`, ≤ 0 = no-op)
  iterate `photos.ListArchivedUIDs` in oldest-first batches (`BatchSize`, default 200) →
  `Result{Purged,Failed}`; **each purge = one `photo.purge` audit row** (`audit.Meta` with an
  actor for manual purges, an empty system actor for the scheduled `PurgeExpired`; `details.source` =
  `manual`/`empty_trash`/`retention`); a **per-photo failure** is logged, counted and skipped (the offset
  grows, the photo stays in the trash for retry), only a cancelled ctx aborts; `RunPurge(ctx, interval)` =
  scheduled cleanup (immediately + every interval, disabled when retention ≤ 0) for the `serve` goroutine),
  `internal/jobs/`
  (a persistent job queue in Postgres, **the main robustness gain over photo-sorter** —
  jobs survive a restart, retry, dedup, wait when the box is offline; the `jobs` table in migration
  `0005_jobs.sql`: `state` queued/running/done/failed/dead, `priority`, `payload` JSONB,
  `attempts`/`max_attempts` (default 5), `run_after` backoff, `locked_by`/`locked_at`; indexes
  (migration `0040_jobs_claim_index.sql`) `(priority DESC, run_after, id) WHERE state='queued'` —
  matching the claim `ORDER BY` exactly, so a deep backlog is walked, not re-sorted — plus
  `(locked_at) WHERE state='running'` for the stale-lock scan, and the **dedup** partial unique on
  `(type, payload->>'photo_uid') WHERE state IN (queued,running)`; `Store` = `NewStore(pool)` with
  `Enqueue(ctx,type,payload,opts)` (idempotent on the dedup key → `ErrDuplicate`,
  `EnqueueOptions{Priority,MaxAttempts,RunAfter}`),
  `Claim(ctx,workerID,types...)` (atomically via `SELECT … FOR UPDATE SKIP LOCKED`,
  `run_after<=now()`, ordered priority DESC/run_after ASC/id ASC, mark running+lock →
  an empty queue `ErrNoJobs`), `Complete(id,workerID)`/`Fail(id,workerID,err)` (increments attempts →
  requeue with exponential backoff via `run_after` base 30 s/cap 1 h, otherwise
  `state=dead`+`last_error`),
  `Defer(id,workerID,delay)` (requeue to `now()+delay` **without** counting an attempt — an offline box waits without
  burning the retry budget); **every lifecycle write is fenced by `locked_by = workerID`** → a worker whose
  job was meanwhile reclaimed gets `ErrLockLost` and its late result is dropped instead of clobbering the new
  owner's run; `Heartbeat(id,workerID)` (refreshes `locked_at`; the worker ticks it for as long as a handler
  runs, so a job that legitimately outlives the stale window — a full import pass — is not recovered and run
  twice)/`RecoverStaleLocks(staleAfter)` (a stale lock = a dead worker → requeue as an attempt, **with the same
  backoff `Fail` applies** so a job that kills its process cannot be re-claimed instantly in a crash loop;
  an exhausted job is dead-lettered),
  helpers `CountsByState`/`CountsByType`/`ListDead`/`RequeueDead`/`Requeue` (dead **and**
  failed → queued, for the admin endpoint)/`List`(`ListOptions{State,Limit,Offset}`, ordered
  updated_at DESC, limit cap 500, for the admin listing)/`Get`; sentinels
  `ErrDuplicate`/`ErrNoJobs`/`ErrJobNotFound`/`ErrLockLost`/`ErrNotDead`; **job types** `image_embed`/
  `face_detect`/`thumbnail`/`places`/`metadata`/`pp_import`/`ps_migrate`/`backup`; `Enqueuer` =
  `NewEnqueuer(store)`
  implements `ingest.JobEnqueuer` (`EnqueueImageEmbed`/`EnqueueFaceDetect`/`EnqueueThumbnail`/
  `EnqueuePlaces`/`EnqueueMetadata`, `ErrDuplicate`=no-op)),
  `internal/worker/`
  (the in-process background worker runtime, **the main queue execution loop**: `Registry` =
  `NewRegistry()`+`Register(type, HandlerFunc)`+`Handler`/`Types` (panics on an empty type/nil
  handler/duplicate registration); `HandlerFunc` = `func(ctx, jobs.Job) error`; `Worker` =
  `New(Config{Queue,Registry,Concurrency,PollInterval,StaleAfter,StaleScanInterval,IDPrefix})`
  with `Run(ctx)` — starts `Concurrency` goroutines polling `Claim` (filtered to the registered
  `Types`), dispatches to the handler by `job.Type`, `Complete`/`Fail` by the result **under the
  claiming worker's id** via a **shutdown-immune** bookkeeping context (`context.WithoutCancel`) — a
  `jobs.ErrLockLost` there means the job was reclaimed, so the result is dropped, not written — plus a
  stale-lock recovery ticker; while a handler runs, a **heartbeat goroutine** refreshes the lock every
  `StaleAfter/3` (floor 100 ms) so a long job is never recovered underneath itself, and it stops (waited
  for, so it cannot race the outcome write) when the handler returns or the lock is reported lost;
  the `Queue` interface = a subset of `jobs.Store` (`Claim`/`Complete`/`Fail`/`Defer`/`Heartbeat`/
  `RecoverStaleLocks`) for testability; **graceful shutdown** = a ctx cancel stops claiming,
  a job whose handler errored at shutdown is abandoned (the queue recovers the lock) — but a
  `RetryAfterError` is **still written as a `Defer`**, since a deferral must never burn a retry attempt;
  a handler panic →
  `ErrHandlerPanic` (job fail, not a crash), an unknown type → `ErrNoHandler`; a handler can return
  `RetryAfter(delay,cause)`/`RetryAfterError` → the worker calls `Defer(delay)` instead of `Fail` (a transient
  error-free failure, no burned attempt — used by `image_embed` when the box is offline); a built-in **noop**
  handler (`TypeNoop`/`NoopHandler`/`RegisterBuiltins`) only for sanity/tests; `Run` returns nil),
  `internal/wake/`
  (optional **Wake-on-LAN auto-wake** of the box, **default OFF** and fully inert when off: the package
  sends a magic packet to the local LAN when `image_embed`/`face_detect` jobs are waiting and the sidecar is
  offline, so the queue catches up without a manual power-on; all behind the interfaces `QueueDepth`
  (`PendingEmbeddingJobs(ctx)` — satisfied by an adapter over `jobs.Store.CountPending`),
  `HealthChecker` (`Healthy(ctx)` — satisfied by `embedding.Client`) and `Sender`
  (`Send(ctx,mac)` — **fakeable in tests**, no real network traffic); `Packet(mac)`
  builds the magic packet via `mdlayher/wol` (102 B: 6× 0xFF + MAC 16×); `Service` =
  `New(Config{Enabled,MAC,BroadcastAddr,Interface,MinQueue,Cooldown,GracePeriod,Queue,Health,
  Sender,Logger,Clock})` (disabled → inert; enabled requires a valid MAC + Queue/Health, otherwise a
  default network sender: UDP broadcast to `BroadcastAddr`, or a raw Ethernet frame on `Interface`
  via `wol.NewRawClient`, requires CAP_NET_RAW); **`Tick(ctx)`** = one cycle: sends a packet only
  when enabled **&&** `pending ≥ MinQueue` **&&** the cooldown has elapsed **&&** the sidecar is offline, then after
  `GracePeriod` re-checks health and logs whether the box came up (otherwise a backoff into the cooldown);
  **the cooldown is set even on a send error** (doesn't spam a broken sender); `Run(ctx,interval)` =
  a scheduled loop (immediately + every interval) in its own goroutine — **never blocks job
  processing**; errors are only logged, never returned; defaults `MinQueue` 1 / `Cooldown` 5 min /
  `GracePeriod` 30 s; tunables in the `embedding.wake.*` config),
  `internal/reachability/`
  (a small **background reachability checker** of the embeddings sidecar, which caches the result in
  an `atomic.Bool` so an HTTP handler can read it without a live probe — the box is often offline, so a probe
  on every request would be slow; the structure mirrors the `internal/wake` ticker. `HealthChecker`
  (`Healthy(ctx)` — satisfies `embedding.Client`); `New(Config{Health,Enabled,Logger}) → *Checker`;
  `Reachable() bool` (never blocks, false before the first probe and for a disabled checker); `Tick(ctx)`
  = one probe + storing the result (logs only a state change, no-op for disabled, so it never touches
  a nil Health); `Run(ctx,interval)` = immediately + every interval in its own goroutine, disabled → returns
  immediately. **Disabled** = `Enabled:false` (built when `embedding.url` is empty) → always
  unreachable, no probe. It is used by `internal/capabilitiesapi` as the source of `semantic_search`;
  started by `cmd/kukatko/capabilities.go` after 60 s alongside the other background services),
  `internal/jobsapi/`
  (a maintainer-only HTTP API over the queue: `NewAPI(Config{Store,RequireMaintainer})`+`RegisterRoutes`
  mounts `/jobs`; `GET /jobs/stats` (counts by_state/by_type+total), `GET /jobs`
  (recent/dead-letter listing, query `state`/`limit`≤500/`offset`, invalid → 400),
  `POST /jobs/{id}/requeue` (dead/failed → queued; 404 missing, 409 non-requeueable);
  the frontend polls, no SSE), `internal/embedding/`
  (an HTTP client to the inference sidecar on the **box**, the same contract as photo-sorter, all behind
  the `Client` interface (fakeable in tests): `New(Config{BaseURL,ImageDim,FaceDim,
  RequestTimeout,HealthTimeout,HealthPath,HTTPClient})` → `*HTTPClient`; `ImageEmbedding(ctx,
  img io.Reader)`/`TextEmbedding(ctx,text)` → a 768-dim CLIP vector + `model`/`pretrained`
  (`POST /embed/image` multipart `file` streamed via `io.Pipe` / `POST /embed/text` JSON
  `{text}`), `FaceEmbeddings(ctx,img)` → `[]Face` (512-dim embedding, `BBox [4]float64`
  in px `[x1,y1,x2,y2]`, `DetScore`)+`model` (`POST /embed/face` multipart `file`),
  `Healthy(ctx) bool` (probe `GET /health`, any HTTP response = the box is reachable, only a
  transport-error/timeout = offline); **box offline-aware typed errors** `ErrUnavailable`
  (transport failed / status 502/503/504, retryable — helper `IsUnavailable`) vs `ErrBadResponse`
  (a malformed response) vs `ErrDimMismatch` (dimension validation 768/512) vs `ErrInvalidURL`; a cancelled
  context is not passed off as unavailability; per-request timeouts via context (default request 60 s /
  health 5 s), never holds the whole image in RAM), `internal/vectors/`
  (the DB layer for embeddings and faces, **stored directly in Postgres** as `halfvec` (float16)
  columns with HNSW cosine indexes — tables `embeddings`/`faces` in migration `0006_embeddings.sql`;
  `halfvec` instead of `vector` halves the HNSW index memory at a negligible recall loss on
  normalized CLIP/ArcFace vectors (important on the Pi); `Store` = `NewStore(pool)` over
  the shared pgx pool:
  `SaveEmbedding`(upsert)/`GetEmbedding`(`ErrEmbeddingNotFound`)/`FindSimilar(vec,limit,maxDistance)`
  for 768-dim image embeddings, `SaveFaces`(idempotent replace in a transaction)/`ListFaces`/
  `ListFacesBySubject(subjectUID)` (faces with the given `subject_uid`, ordered `(photo_uid,
  face_index)` — the basis of outlier detection; shares `queryFaces`/`scanFace` with `ListFaces`)/
  `DeleteFaces`/`FindSimilarFaces`/`FindSimilarFaceCandidates` (like `FindSimilarFaces`, but
  also returns the cache `subject_uid`/`subject_name`/`marker_uid` + `bbox` — the basis of identity suggestions)/
  `FindSimilarUnassignedFaceCandidates(vec,limit,maxDistance,exclude)` (like the previous one, but only
  **unassigned** faces `subject_uid IS NULL` and with an **exclusion set** `[]FaceKey` filtered out
  directly in SQL (an anti-join via `unnest` of two parallel arrays) — the basis of finding a person among
  untagged photos, the recognition sweep and the review game; filters **before** `LIMIT` and runs under
  `hnsw.iterative_scan = strict_order`, so the caller gets `limit` candidates even when rejections
  take away the nearest neighbours — filtering only after the HNSW limit would silently shrink the result, which is a real
  bug)/
  `FacesByKeys(keys)` (batch fetch of `faces` rows by `[]FaceKey` `(photo_uid,face_index)` in one
  query via `JOIN unnest` — **including embeddings**; keys with no row (a face deleted by re-detection)
  are missing from the result, order undefined, empty input → `nil`; the basis of `internal/candidates`,
  where the negative-exemplar rule needs the embeddings of the filtered candidate set without N+1)/
  `UpdateFaceMarker(photoUID,faceIndex,markerUID,subjectUID,subjectName)` (writes the cache columns onto
  a single face, empty marker/subject → `NULL`; this is how an IoU match is cached) for 512-dim face
  embeddings + cache columns
  marker_uid/subject_uid/subject_name/photo_width/photo_height/orientation and a normalized
  `bbox DOUBLE PRECISION[4]` `[x,y,w,h]`; similarity via `embedding <=> $vec` (cosine, nearest
  first) in a **read-only transaction** with `SET LOCAL hnsw.ef_search = 100` (constant `efSearch=100`,
  a guard test keeps `0 < efSearch < efSearchMax=400` — the design never raises it to 400, see
  `docs/PERF.md`); `limit` clamped `[1,500]`,
  a non-positive `maxDistance` disables the filter; helpers `ToHalfVec`/`FromHalfVec` (`[]float32` ↔
  `pgvector.HalfVector`) and **shared vector math** `Centroid`(L2-normalized
  element-wise mean)/`Normalize`/`CosineDistance` in `math.go` (the single implementation reused
  by both `internal/cluster` and `internal/outliers`) and the **negative-exemplar rule**
  `IsNegativeExemplar(candidate,accepted,rejected)`/`NearestDistance(v,set)` in `negative.go`
  (a nearest-neighbour margin test: a candidate closer to some **rejected** exemplar than to its
  nearest **accepted** one is "negative" and drops out of the results; without rejections **a no-op in O(1)**;
  equal distances = survives (deterministic, "strictly closer to the rejected one" drops out); a shared
  scoring helper for both faces (ArcFace) and labels (CLIP), so the feature packages don't merely hide one
  rejected row but learn something); sentinels
  `ErrEmbeddingNotFound`/`ErrDimMismatch` (validation 768/512)/
  `ErrFaceIndexTaken` (UNIQUE `(photo_uid,face_index)`); `ListPhotosMissingEmbedding(limit)` =
  uids of non-archived photos without an embedding (LEFT JOIN, newest first, `limit<=0`=all) for
  backfill; `FindDuplicatePairs(neighbours,maxDist)` = near-duplicate pairs by embedding cosine
  distance (`duplicate.go`, `CROSS JOIN LATERAL` + HNSW `LIMIT` neighbours per photo, no
  O(n²) scan; `maxDist<=0`→no pairs; a read-only tx with `hnsw.ef_search`) — the basis of
  `internal/duplicates`; **face-detection tracking** in the `face_detections` table (migration
  `0009_face_detections.sql`: `photo_uid PK` FK `ON DELETE CASCADE`, `face_count`, `model`,
  `detected_at`) — because `faces` can have zero rows, it is the only way to distinguish a photo
  with no faces from an unprocessed one; `RecordFaceDetection(uid,faces,model)` (atomically replaces the photo's
  faces **and** upserts the `face_detections` row — even for zero faces; shares the `replaceFaces` tx
  helper with `SaveFaces`), `FacesDetected(uid)` (does a row exist?), `ListPhotosMissingFaces(limit)`
  (uids of photos with no `face_detections` row, like `ListPhotosMissingEmbedding`); FK
  `ON DELETE CASCADE` — deleting a photo
  deletes embeddings, faces and face_detections, fixing the photo-sorter gap with orphans),
  `internal/people/`
  (the DB layer for **subjects** (people/animals/other) and **markers** (face/label regions on
  photos), tables `subjects`/`markers` in migration `0008_subjects_markers.sql`: `subjects`
  = `uid PK` (prefix `su`), `slug UNIQUE`, `name`, `type IN (person|pet|other)`, `favorite`,
  `private`, `notes`, `cover_photo_uid` (FK photos `ON DELETE SET NULL`), timestamps; `markers` =
  `uid PK` (prefix `mk`), `photo_uid` (FK photos `ON DELETE CASCADE`), `subject_uid` (FK
  subjects `ON DELETE SET NULL`), `type IN (face|label)`, a normalized bbox `x,y,w,h`
  DOUBLE PRECISION (0..1 display space, like `faces.bbox`), `score`, `invalid`, `reviewed`,
  timestamps + indexes on `photo_uid`/`subject_uid`; `Store` = `NewStore(pool)` over the shared pgx
  pool: **subjects** `CreateSubject`(generates a uid + a **unique slug from name** — `Slugify`
  without diacritics/ASCII, a collision → a numeric suffix `name-2`)/`GetSubjectByUID`/`GetSubjectBySlug`/
  `UpdateSubject`(re-slugging + refresh of the `faces.subject_name` cache)/`ListSubjects` (with counts of
  non-archived... i.e. **non-invalid** markers per subject, ordered by name; plus
  `CoverFace *SubjectFace` = the face that illustrates the subject in the people grid when it has no
  `cover_photo_uid` — the `best_face` CTE in `listSubjectsSQL` takes per subject a `DISTINCT ON` with
  the order **`w*h DESC, score DESC, uid`**: the tile is a square zoomed from the crop of the cache
  thumbnail, so readability is decided by the number of pixels behind the face → the largest box wins,
  `score` only breaks a size tie (the reverse order would put a tiny sharp
  mug before a large decent one) and `uid` keeps the choice deterministic; filters: `type='face'`
  (drawn label boxes aren't faces), `invalid=FALSE` (rejected false-positives aren't returned),
  a non-zero box and photo dimensions, and a visible photo as with the count. It also carries the photo's `width/height/orientation`
  — the client crops the region itself and without the frame would distort it)/
  `DeleteSubject` (the FK detaches the markers, clears the faces cache)/`ListPhotoUIDsBySubject` (distinct
  uids of non-archived photos with a non-invalid marker of the subject, newest-first — the basis of the subject's
  gallery in `peopleapi`)/`SearchSubjects(q,limit)` (accent/case-insensitive ILIKE over
  `immutable_unaccent(name)`, cap limit — the basis of `globalsearchapi`); **markers** `CreateMarker`
  (validation of type/`0..1` bounds, optionally a subject right away → faces cache)/`GetMarkerByUID`/
  `ListMarkersByPhoto`/`AssignSubject`+`UnassignSubject` (in a transaction they update the
  denormalized **faces cache** `marker_uid`/`subject_uid`/`subject_name` via
  `WHERE marker_uid = $1`)/`SetMarkerInvalid`/`SetMarkerReviewed`/`DeleteMarker` (clears the
  faces cache); sentinels `ErrSubjectNotFound`/`ErrMarkerNotFound`/`ErrSlugExhausted`/
  `ErrInvalidType`/`ErrInvalidBounds`; **the faces cache is kept consistent** on every change of a
  marker/subject (delete, rename, assign/unassign); **audited variants**
  `CreateSubjectAudited`/`UpdateSubjectAudited`/`DeleteSubjectAudited` and
  `CreateMarkerAudited`/`AssignSubjectAudited`/`UnassignSubjectAudited` (`internal/people/audit.go`)
  take an `audit.Entry` and write it **in the same transaction** as the change (`audit.Write(ctx,tx,entry)`),
  so the audit row commits/rolls back atomically with the mutation (the `internal/photos`/`internal/organize` convention);
  a shared tx-core (`insertMarkerTx`/`assignSubjectTx`/`unassignSubjectTx`/`prepareSubjectInsert`) is used by
  both variants), `internal/facematch/`
  (linking detected faces to markers/subjects + identity suggestions, all behind the interfaces
  `PhotoStore`/`FaceStore`/`PeopleStore` (unit-testable with fakes without a DB): `Service` =
  `New(Config{Photos,Faces,People,IoUThreshold,SuggestionLimit,SuggestionMaxDistance,MinFaceSize})`;
  **IoU geometry** `IoU(a,b [4]float64)` (a pure function, Intersection-over-Union of normalized
  boxes `[x,y,w,h]`), `findBestMarker` picks the most-overlapping **face** marker (ignores
  `invalid`), a match at `IoU ≥ faces.iou_threshold` (default 0.1, mirrors photo-sorter);
  **`PhotoFaces(ctx,photoUID)`** (backing `GET /photos/{uid}/faces`) → for each stored face
  computes the best marker by IoU, determines the action (`create_marker`/`assign_person`/`already_done`),
  **caches the match onto the face row** via `vectors.UpdateFaceMarker`, and adds suggestions to **every** face
  with an embedding (candidates for an unnamed one, alternatives for reassignment for an assigned one —
  the own subject filters itself out, because `exclude` holds all people on the photo; widening the
  threshold without a cutoff runs only for unnamed ones, so an assigned face with no close alternative
  honestly gets an empty list); markers without a matching face are attached (a negative `face_index`);
  **suggestions** (`aggregateSuggestions`, a pure function) from `vectors.FindSimilarFaceCandidates`
  (HNSW cosine) aggregate candidates by subject, exclude faces on the same photo, subjects already
  assigned on the photo (other people) and faces below `faces.min_face_size`, order by average
  distance, `confidence = 1 − distance`, limit `faces.suggestion_limit`, primary threshold
  `faces.suggestion_max_distance` with a fallback to unbounded distance when there are few suggestions;
  **the assignment state machine** `Apply(ctx,AssignRequest,audit.Meta)` (backing
  `POST /photos/{uid}/faces/assign`, editor/admin): `create_marker` (creates a face marker + assigns the
  subject + links the face), `assign_person` (assigns a subject to an existing marker),
  `unassign_person` (detaches the subject), keeps the `faces` cache and `marker.reviewed` consistent
  (assign → reviewed, unassign → unreviewed), **auto-creates a subject by name** (find-or-create
  via `Slugify`+`GetSubjectBySlug`); **audit**: each transition writes 1 row via the audited `people`
  methods in the same transaction as the change — `create_marker`/`assign_person` → `face.assign`,
  `unassign_person` → `face.unassign` (target = marker, details action/photo/subject/face_index);
  `meta` is the actor+request from `photoapi.handleFaceAssign`, empty for the system cluster caller
  (actor NULL); sentinels `ErrInvalidAction`/`ErrMissingBBox`/
  `ErrMissingMarker`/`ErrMissingSubject`, a missing photo/marker/subject → 404 in the HTTP layer
  (`photoapi.FaceService` interface + handlers in `internal/photoapi/faces.go`); tunables in
  `faces.*` config), `internal/embedjob/`
  (wiring of the CLIP embedding into the queue + embedding queries, all behind the interfaces
  `PhotoStore`/`VectorStore`/`Previewer`/`Enqueuer`+`embedding.Client`: `Service` =
  `New(Config{Photos,Vectors,Client,Previewer,Enqueuer,PreviewSize,OfflineRetryDelay,
  DuplicateMaxDist})`; **the `image_embed` handler** `Handle`(=`worker.HandlerFunc`, registered
  in `serve`) → from the payload `{"photo_uid"}` loads the photo, renders (idempotently) the `fit_720` thumbnail,
  sends `ImageEmbedding` to the sidecar, stores a 768-dim `halfvec` via `vectors.SaveEmbedding`+`model`/
  `pretrained`; **idempotent** (a photo with an embedding is skipped without calling the sidecar), **box
  offline** (`embedding.IsUnavailable`) → `worker.RetryAfter(5 min)` (deferral without burning an attempt),
  any other error a normal retry; `BackfillEmbeddings(ctx)` enqueues `image_embed` for every photo without
  an embedding (dedup no-op), returns the count; `Duplicates(ctx,uid)` embedding-based detection of near
  duplicates within `duplicate.embedding_max_dist`, excluding itself (`<=0` disables it)), `internal/facejob/`
  (wiring of face detection into the queue, all behind the interfaces
  `PhotoStore`/`VectorStore`/`ImageSource`/`Enqueuer`+`embedding.Client`: `Service` =
  `New(Config{Photos,Vectors,Client,Source,Enqueuer,OfflineRetryDelay,MinDetScore})`; **the
  `face_detect` handler** `Handle`(=`worker.HandlerFunc`, registered in `serve`) → from the payload
  `{"photo_uid"}` loads the photo, opens the **full-resolution decodable original** via
  `StorageSource` (= `storage.Materialize` + `imgconvert.EnsureDecodable` behind the interface
  `Materializer`, HEIC/RAW/video are converted, `Close` frees the temp and the materialized original),
  sends `FaceEmbeddings` to the sidecar (512-dim + pixel bbox + det_score) and
  stores it via `vectors.RecordFaceDetection`; the original (not the thumbnail) because the sidecar (InsightFace)
  rotates by EXIF itself and returns the bbox in display pixels; **bbox conversion** `normalizeBBox` pixel
  `[x1,y1,x2,y2]` → normalized `[x,y,w,h]` (0..1) by the photo dimensions and **EXIF orientation** (swap of
  width/height for orientations 5–8), mirrors the photo-sorter logic; **det_score filter**
  (`faces.min_det_score`, default 0.5, `<=0` disables) drops weak detections, reindexes the survivors
  contiguously; **idempotent** (a photo with a `face_detections` row is skipped; zero faces is still
  recorded), **box offline** → `worker.RetryAfter(5 min)`; `BackfillFaces(ctx)` enqueues
  `face_detect` for every unprocessed photo (`ListPhotosMissingFaces`, dedup no-op), returns
  the count), `internal/processapi/`
  (a maintainer-only HTTP API for bulk processing: `NewAPI(Config{Backfiller,FaceBackfiller,
  Reclusterer,PlacesBackfiller,ThumbnailBackfiller,MetadataBackfiller,RequireMaintainer})`+`RegisterRoutes`
  mounts `/process`;
  `POST /process/embeddings` →
  `{enqueued}` runs `embedjob.BackfillEmbeddings`, `POST /process/faces` → `{enqueued}` runs
  `facejob.BackfillFaces`, `POST /process/clusters` → `{created}` runs `cluster.Recluster`
  (re-clustering of unassigned faces; `Reclusterer` optional — nil → 503),
  `POST /process/places` → `{enqueued}` runs `placesjob.BackfillPlaces` (backfill of reverse-geocode for
  geotagged photos; `PlacesBackfiller` optional — nil → 503, i.e. without a mapy.com key),
  `POST /process/thumbnails` → `{enqueued}` runs `thumbjob.BackfillThumbnails(all)` (backfill of
  `thumbnail` for photos without a thumbnail = without a pHash; `?all=true` schedules every non-archived photo;
  `ThumbnailBackfiller` optional — nil → 503; local, works even with the box offline; `queryFlag`
  parses `?all`),
  `POST /process/metadata` → `{enqueued}` runs `metajob.BackfillMetadata(all)` (backfill of `metadata`
  for photos whose file has never been read = `metadata_extracted_at IS NULL`; `?all=true`
  forces a re-read of every non-archived photo; `MetadataBackfiller` optional — nil → 503;
  local, works even with the box offline)), `internal/cluster/`
  (face auto-clustering: groups **not-yet-assigned faces** (without a subject) into clusters of the same
  person, so a whole cluster can be named in one go (a key UX improvement over photo-sorter's per-face naming);
  the `face_clusters` table (migration `0010_face_clusters.sql`: `uid` PK prefix `fc`,
  `centroid halfvec(512)` cosine, `size`, `model`, timestamps) + cache column `faces.cluster_uid` FK
  `ON DELETE SET NULL`; all behind the interfaces `FaceSearcher` (a subset of `vectors.Store`) and `FaceAssigner`
  (a subset of `facematch.Service`) → unit-testable with fakes; `Service` =
  `New(Config{Store,Faces,Assigner,Threshold,MinSize,SuggestionMaxDistance})`, defaults
  `DefaultThreshold` 0.4 / `DefaultMinSize` 2 / `DefaultSuggestionMaxDistance` 0.5; **the algorithm**
  (pure functions `algo.go`/`suggest.go`): greedy **connected components** (union-find) over the HNSW
  nearest neighbours of each clusterable face up to a cosine-distance threshold — an edge = two
  faces closer than `threshold`, a component `≥ minSize` becomes a cluster, smaller ones stay
  unclustered; a per-cluster L2-normalized **centroid** (`centroid`/`normalize`/`cosineDistance`)
  for picking the representative (`nearestToCentroid`) and the subject suggestion; **`Recluster(ctx)`** clusters
  only faces **without a subject AND without a cluster** (`subject_uid IS NULL AND cluster_uid IS NULL`) →
  incremental and re-runnable, never touches assigned or clustered ones, deterministic;
  **`ListClusters(ctx)`** (backing `GET /faces/clusters`) → per cluster the size, a representative
  face, examples (`maxExamples` 4) and **a suggestion of an existing subject** (`bestSubjectSuggestion`
  aggregates `FindSimilarFaceCandidates` over the centroid by subject, `confidence = 1 − distance`,
  null when no named neighbour < `suggestionMaxDistance`); **`AssignCluster(ctx,req)`**
  (backing `POST /faces/clusters/{id}/assign`) assigns **all** faces of the cluster to one subject
  (by `subject_uid`, otherwise find-or-create by `subject_name`) via the **shared facematch state
  machine** (`create_marker`, the subject is resolved once and pinned for the rest), then deletes the consumed
  cluster (the FK releases `cluster_uid`); **`RemoveFace(ctx,clusterUID,ref)`** (backing
  `POST /faces/clusters/{id}/remove-face`) detaches a stray face **before** naming, recomputes the
  centroid/size (`RefreshCluster`), deletes an orphaned cluster; `Store` over the shared pgx pool
  (`ListUnclusteredFaces`/`ListClusterFaces`/`CreateCluster`/`AddFacesToCluster`/`ListClusters`/
  `GetCluster`/`DeleteCluster`/`RemoveFaceFromCluster`/`RefreshCluster`); sentinels
  `ErrClusterNotFound`/`ErrEmptyCluster`/`ErrMissingSubject`/`ErrFaceNotInCluster`; tunables in
  `cluster.*` config), `internal/clusterapi/`
  (an editor/admin HTTP API over the clustering: the `Service` interface (satisfied by `cluster.Service`),
  `NewAPI(Config{Service,RequireWrite})`+`RegisterRoutes` mounts `/faces/clusters`:
  `GET /faces/clusters` (list of clusters + suggestions), `POST /faces/clusters/{id}/assign` (assigns the whole
  cluster), `POST /faces/clusters/{id}/remove-face` (detaches a face); 503 when the backend is not wired,
  400/404/409 per the sentinels; mounted in `serve` (`buildClusterAPI` in `cmd/kukatko/clusters.go`,
  which shares the `facematch.Service` from `buildFaceMatch`)), `internal/outliers/`
  (per-person outlier detection of faces: reveals probably **misassigned faces**
  by ordering them by distance from the centroid of the person's embeddings, mirrors photo-sorter; all behind the interfaces
  `FaceStore` (a subset of `vectors.Store`) and `PeopleStore` (a subset of `people.Store`) →
  unit-testable with fakes without a DB; `Service` = `New(Config{Faces,People,Feedback})`;
  **`Outliers(ctx,subjectUID,opts)`** (backing `GET /subjects/{uid}/outliers`) verifies the subject
  (`people.ErrSubjectNotFound`), loads `vectors.ListFacesBySubject`, computes a **trimmed centroid**
  and scores each face by `vectors.CosineDistance` from it, descending (most suspicious first,
  tie-break `photo_uid`/`face_index`); **trimming is the crux:** a plain centroid is computed also
  from the outliers themselves, so three badly assigned faces **pull the centroid toward themselves** and mask
  exactly what you're looking for — hence: compute the centroid, discard the farthest decile (`trimCount(n)` =
  `(n+9)/10`, rounded up, but with a **floor** of `MinMeaningful`, so someone with 4 faces loses
  1, not half; a set ≤ `MinMeaningful` is not trimmed at all), recompute and score against the
  trimmed one — **all faces are scored including those the trim removed from the centroid**, otherwise the
  outlier would hide from itself; deterministic, no clustering step; `Options{Threshold,Limit}` narrows the result
  (0/0 = the historical "everything, sorted"), **confirmed faces** (`feedback.FaceConfirmationsForSubject`)
  are excluded even before the filter; `Result` = `{subject_uid,count,meaningful,avg_distance,no_embedding,
  faces:[OutlierFace{photo_uid,face_index,bbox,det_score,distance,marker_uid?,width,height,
  orientation}]}`, where `count`/`meaningful`/`avg_distance` describe the **whole scored set**
  (before threshold/limit), so the statistics don't lie for a narrowed list, and `no_embedding` counts
  assignments without an embedding that **cannot** be checked (the sidecar was offline) and are not in `faces` —
  the client should acknowledge them; **small sets** (< `MinMeaningful`=3 faces) → `meaningful:false` (nothing
  is singled out), the faces are still returned sorted; no mutation — a wrong
  face is detached via the existing assign API), `internal/outlierapi/`
  (an editor/admin HTTP API over outlier detection: the `Service` interface (satisfied by `outliers.Service`),
  `NewAPI(Config{Service,RequireWrite})`+`RegisterRoutes` mounts `GET /subjects/{uid}/outliers`
  behind `RequireWrite`; 503 without a backend, 404 missing subject; mounted in `serve`
  (`buildOutlierAPI` in `cmd/kukatko/outliers.go`)), `internal/candidates/`
  (**"find a person among untagged photos"**: for a named subject finds **unassigned**
  faces that resemble it — the counterpart to `GET /photos?person=`, complementing clustering too, which
  won't surface a lone unnamed face of a well-known person; all behind the interfaces `FaceStore`
  (`vectors.Store`), `PeopleStore` (`people.Store`), `FeedbackStore` (`feedback.Store`) and `PhotoStore`
  (`photos.Store`) → unit-testable with fakes without a DB; `Service` = `New(Config{Faces,People,Feedback,
  Photos,Media,MaxDistance,SearchLimit,MinFacePx,Concurrency,MinFaceRel})`, tunables default via the
  `Default*` constants (0.5/1000/32/8). **`Find(ctx,subjectUID,Request{Threshold,Limit})`**: verifies the
  subject (`people.ErrSubjectNotFound`); loads `ListFacesBySubject` and **deduplicates exemplars to
  one per source photo** (highest `det_score`, tie lowest `face_index`), so a photo with three
  faces of that person doesn't vote three times; for each exemplar runs `FindSimilarUnassignedFaceCandidates`
  with **bounded concurrency** (`errgroup.SetLimit`) and an exclusion set of already-rejected faces
  (`feedback.FaceRejectionsForSubject`); **merges candidates with voting** (`match_count` = the number of
  distinct exemplars, `distance` = the **minimum** across votes); **vote rule** `min_match_count`
  (`computeMinMatchCount`, scales `√exemplarCount * threshold/base / 2`, **clamp 1..5** and ≤ the number of
  exemplars — a single-face subject always 1; returned in the response so the UI can explain the filter); then
  a **relative size floor** (`bbox[2] ≥ MinFaceRel`, shares `faces.min_face_size`); the survivors are
  hydrated (`photos.ListByUIDs` + `mediaurl.Decorate`), an **absolute pixel floor** is applied
  (`MinFacePx`) and the **negative-exemplar rule** (`vectors.IsNegativeExemplar` over embeddings from
  `FacesByKeys` — **a no-op without rejections**, the embeddings are then not loaded at all); finally the **action
  classification** (`create_marker` without a marker / `assign_person` a marker without (another) subject / `already_done`
  the marker already points at this subject = a stale cache, via `GetMarkerByUID` with a cache), ordered by distance
  and truncated to `Limit` (0 = all). `Result` = `{subject_uid,source_photo_count,source_face_count,
  faces_without_embedding,min_match_count,threshold,reason?,counts{create_marker,assign_person,
  already_done},candidates:[Candidate{photo,face_index,bbox{relative,pixel},distance,match_count,
  action}]}`; `bbox` carries **both relative 0..1 and pixels** honouring EXIF orientation (`displayDims` swaps
  W/H for orientations 5–8). **Edge cases**: a subject with no faces → an empty **non-error** result with
  `reason:"no_faces"`; a subject with markers but no embedded faces → `reason:"no_embeddings"` +
  `faces_without_embedding`; the box being offline doesn't matter — the vectors are read already in Postgres. **Read-only** —
  confirmation goes through the existing assign path; the sweep across all people can call `Find` per subject
  without reimplementation), `internal/candidatesapi/`
  (an editor/admin HTTP API over the candidate search: the `Service` interface (satisfied by `candidates.Service`),
  `NewAPI(Config{Service,RequireWrite})`+`RegisterRoutes` mounts `POST /subjects/{uid}/candidates`
  behind `RequireWrite`; the body `{threshold?,limit?}` is **optional** (empty → defaults),
  `DisallowUnknownFields` + 64 KiB, a negative `threshold`/`limit` → 400; 503 without a backend, 404 missing
  subject (`people.ErrSubjectNotFound`); mounted in `serve` (`buildCandidatesAPI` in
  `cmd/kukatko/candidates.go`, takes `mediaStore` for URL stamping)), `internal/sweep/`
  (**recognition sweep** — composes the per-subject candidate search across **all** named
  subjects at once: the `Finder` interface (satisfied by `*candidates.Service`) and `SubjectLister` (satisfied by
  `*people.Store`), `New(Config{Subjects,Finder,Concurrency,MaxSubjects,Log})` (defaults 4/500, nil
  Log→`slog.Default()`, nil store→panic), `Sweep(ctx, Params{Threshold,Limit}, emit func(Event) error)`;
  lists subjects with `MarkerCount>0`, caps at `MaxSubjects` (`capped`+`SubjectsTotal`
  in the summary), the scan runs in a **bounded worker pool** (`errgroup.SetLimit`) and **funnels** the results
  through one consumer, so `emit` is always called **serially** (the handler writes it straight into
  the response); `emit` returns an error → the sweep stops and the workers cleanly unblock (no leak);
  a `Find` error for one subject is logged and skipped (`emitResult`), only the subject listing is
  fatal; `already_done` candidates are filtered out of the work list (`actionableCandidates`),
  but counted into `TotalAlreadyDone`; **never auto-confirms**. `Event` = `{type,progress?,
  person?,summary?}` (`events.go`). Unit tests with fakes (concurrency/cap/filter/omit-empty/emit-fail),
  an integration test over real candidates+DB), `internal/sweepapi/`
  (an editor/admin HTTP API over the sweep: the `Service` interface (satisfied by `*sweep.Service`),
  `NewAPI(Config{Service,RequireWrite})` (nil RequireWrite → pass-through) + `RegisterRoutes` mounts
  `GET /faces/sweep` behind `RequireWrite`; `parseConfidence` (percent-or-distance → distance, floor
  `0.01`, default 75 %) + `parseLimit`, errors → 400; streams **NDJSON** via the `stream` helper, which
  sets the headers (`application/x-ndjson`, `Cache-Control: no-store`) **lazily** at the first line and
  **flushes** after each one (`http.Flusher`, propagated through `internal/metrics` `statusRecorder.Flush`);
  an error before the first line → 500 JSON, mid-stream → only a log; 503 without a backend; mounted in `serve`
  (`buildSweepAPI` in `cmd/kukatko/sweep.go`, shares `candidates.Service` via `buildCandidatesService`)),
  `internal/expand/`
  (**"find photos similar to an album / label"** — completing a partially-tagged collection, the counterpart to the per-photo
  `GET /photos/{uid}/similar`: for an album or label finds photos similar to its members that are not yet
  in it; all behind the interfaces `VectorStore` (`vectors.Store`), `OrganizeStore` (`organize.Store`),
  `FeedbackStore` (`feedback.Store`) and `PhotoStore` (`photos.Store`) → unit-testable with fakes without a DB;
  `Service` = `New(Config{Vectors,Organize,Feedback,Photos,Media,MaxDistance,Limit,MaxLimit,SearchLimit,
  SourceCap,Concurrency})`, tunables default via the `Default*` constants (0.30/50/200/200/500/8), nil
  store→panic, nil `Media` is OK. **`Album(ctx,uid,Request)`** / **`Label(ctx,uid,Request)`** share
  one core `find`, differing only in how the source set is resolved (validation via `GetAlbumByUID`/
  `GetLabelByUID` → `organize.ErrAlbumNotFound`/`ErrLabelNotFound`; membership via `ListPhotoUIDs`/
  `ListPhotoUIDsByLabel` — **natively, no PhotoPrism**; a label additionally `LabelRejectionsForLabel`).
  The core: **samples** the sources down to `SourceCap` (`sampleSource`, a deterministic even stride, reports
  `source_capped`); loads the sample's embeddings (`GetEmbedding`, `ErrEmbeddingNotFound` is **skipped and
  counted**, not an error — the box is often offline); for each source `FindSimilar` with **bounded concurrency**
  (`errgroup.SetLimit`); **merges with voting** (`match_count` = the number of sources, `distance` = the **minimum**);
  **excludes the collection's members** (the whole point); **vote rule** `min_match_count` (`computeMinMatchCount`,
  `√sourceCount * threshold/base / 2`, **clamp 1..5**, returned); for labels the **rejected** UIDs drop out and
  the **negative-exemplar rule** applies (`vectors.IsNegativeExemplar` — **a no-op without rejections**; albums
  have no rejection model → an asymmetry); hydration (`ListByUIDs` + `mediaurl.DecorateOne`, a non-primary
  stack member is skipped); ordered **`match_count` DESC then `distance` ASC**, truncated to `Limit`. `Result` =
  `{kind,collection_uid,source_photo_count,source_photos_sampled,source_photos_with_embedding,
  source_capped,source_cap,min_match_count,threshold,limit,result_count,reason?,candidates:[Candidate{
  photo,distance,similarity,match_count}]}`; `similarity` = `1 - distance`. **Edge cases**: an empty
  collection → `reason:"empty_collection"`, a collection with no embeddings → `reason:"no_source_embeddings"` (both
  a non-error empty `Candidates:[]`); a single photo degenerates to per-photo similarity. **Read-only** —
  adding goes through `POST /photos/bulk`. Unit tests with fakes + an integration test over real embeddings+DB),
  `internal/expandapi/`
  (an editor/admin HTTP API over collection expansion: the `Service` interface (satisfied by `*expand.Service`) with two
  methods `Album`/`Label`, `NewAPI(Config{Service,RequireWrite})` + `RegisterRoutes` mounts
  `GET /albums/{uid}/similar` and `GET /labels/{uid}/similar` behind `RequireWrite`; both share `respond` +
  `finder`, differing only in the not-found sentinel; `parseRequest` reads the query `?threshold=&limit=` (empty →
  default, non-numeric / negative → 400); 503 without a backend, 404 missing album/label
  (`organize.ErrAlbumNotFound`/`ErrLabelNotFound`); mounted in `serve` (`buildExpandAPI` in
  `cmd/kukatko/expand.go`, takes `mediaStore` for URL stamping)),
  `internal/mcpapi/`
  (**MCP server** — a library exposed to an AI agent over the Model Context Protocol at `POST /api/v1/mcp`;
  `NewAPI(Config{Enabled,Photos,Organize,People,Bulk,Similar,Media,RequireAuth,PageSize,MaxPageSize})`
  + `RegisterRoutes`. **`Enabled:false` → `RegisterRoutes` registers nothing and the servers aren't even built**
  (the route doesn't exist, rather than returning a 403 — a 403 would reveal that the endpoint is there; in the full binary the
  path then falls into the SPA catch-all and returns `index.html`, in tests 404, because their router has no
  fallback); this is a departure from the local "nil service → 503" idiom and is deliberate, the endpoint is an opt-in attack
  surface. Transport:
  `github.com/modelcontextprotocol/go-sdk` (pure Go, keeps `CGO_ENABLED=0`), `NewStreamableHTTPHandler`
  with `Stateless:true` (each POST standalone → no session state **and** the request context reaches the
  tool handlers), `JSONResponse:true` (tools don't stream) and `DisableLocalhostProtection:true`
  (the DNS-rebinding guard rejects a loopback+non-loopback `Host`, i.e. a reverse proxy; it protects unauthenticated
  local servers, this one requires a principal). **Auth: nothing new** — behind `RequireAuth` (the agent sends
  `Bearer kkt_…`), the role is the **token owner's**. The boundary is **double**: `buildHandler` builds **two servers**
  (read-only and write) and `getServer(*http.Request)` picks by `auth.UserFromContext(...).Role.CanWrite()`
  → a viewer doesn't even see the write tools in `tools/list`; **and** every write handler calls `writerFromContext`
  (registration = UX, the check = the security boundary). The `withCaller` middleware assembles `caller{user, meta}`
  (`audit.FromRequest`) into the context, because a tool handler sees only `ctx`, not `*http.Request` — without a
  principal **fail-closed 401**. The tools (`tools_search.go` / `tools_collections.go` / `tools_write.go` /
  `tools_bulk.go`): reads `search_photos` (`query.Parse` → `ListParams.QueryFilters` + `RatedBy` =
  the caller, so `favorite:`/`rating:`/`flag:` mean theirs; free text → `FullText` and the **ranked path**
  `Store.Search`, only when no explicit `sort` came, otherwise `Store.List`), `get_photo`,
  `find_similar_photos` (kNN, `limit+1` because a photo is its own nearest neighbour; without an embedding
  **an empty non-error**), `list_/get_` albums/labels/people, `library_stats`; writes `create_album`,
  `add_/remove_photos_from_album`, `create_label`, `attach_/detach_label` (`SourceManual`, uncertainty 0),
  `set_photo_metadata` (**read-modify-write** via `metadataOf` — the store does a full-record replace, so a
  naive "set title" would null out the description, date and location; pointer fields = omit vs. clear),
  `set_photo_rating` (goes through `internal/bulk`, because that **writes the audit row in the transaction itself** —
  the rating store has no audited variant) and `bulk_edit_photos`. `shape.go` is an **allow-list**, not a copy:
  `photoSummary` = `{uid,title,taken_at,media_type,thumb_url}`, `photoDetail` curated columns,
  **the `exif` blob nowhere**; `page()` reports `total`/`offset`/`remaining` (clamped to 0). **Nothing destructive**
  — no purge/trash/**archiving**/restore/backup/users; `bulkEditIn` therefore omits `Archive`
  (archive → trash → retention purge = deletion in installments) and `Location`. The `jsonschema` tags carry the descriptions
  of the tool arguments → `//nolint:lll` (the tag is one unbreakable token and is the agent's real interface).
  Unit tests without a DB (helpers, RBAC, `exif` doesn't leak, disabled route) + integration tests over the **real
  MCP transport**, real auth and real `kkt_` tokens; mounted in `serve`
  (`buildMCPAPI` in `cmd/kukatko/mcp.go`, in `discoveryAPIOptions`). See `docs/MCP.md`),
  `internal/review/`
  (**the review game** — a queue of "one at a time" questions over the **uncertainty band** and the application of answers;
  **it composes existing pieces, reimplements nothing**: face questions via the `Sweeper` interface (satisfied by
  `*sweep.Service` → per-subject candidate search with all its filters: unassigned-only,
  rejections, negative exemplar, min. face size), label questions via `Expander` (satisfied by
  `*expand.Service` → excludes members and rejected ones), writes via `Assigner` (`*facematch.Service`),
  `OrganizeStore.AttachLabelAudited` and `FeedbackStore.RejectFace/RejectLabel`; `New(Config{...,BandMin,
  BandMax,QueueSize,CacheTTL,MaxLabels,LabelConcurrency,Now})` (an invalid band → the default pair
  0.45/0.75, `Now` = a test hook). **`Queue(ctx,userUID,limit)`**: candidates with confidence
  (= 1 − distance) in `[BandMin,BandMax)` — the sweep runs with `Threshold: 1−BandMin` and review trims the upper edge
  (above it a candidate belongs on `/recognition`/expand, not in the game); labels with `PhotoCount>0`
  (cap `MaxLabels`, fan-out `errgroup.SetLimit(LabelConcurrency)`, an error on one label is
  logged and skipped); ordered by **distance from the band's center** (tie-break a stable id), the kinds are
  **interleaved** deterministically (comparison of integer fractions, no `rand`); the queue is **cached
  per user** (`CacheTTL`) and the session holds `answered`/`skipped` sets + a counter (in-memory, idle-pruned after
  12 h; skip is **deliberately** only session-scoped — "I don't know" is not "no"). An empty library → `reason:
  "no_people_no_labels"`, an empty band → `"no_candidates"` (both non-error). **`Answer(ctx,userUID,
  questionID,answer,meta)`**: the id is **content-derived** (`face:<photo>:<idx>:<subject>` /
  `label:<photo>:<label>`) → the endpoint is stateless; a yes on a face **re-reads** the current face
  row (`FacesByKeys`) and derives the action (marker → `assign_person`, otherwise `create_marker` with the stored
  bbox; a face already carrying a subject → short-circuit, no duplicate marker), a yes on a label
  `AttachLabelAudited` (idempotent upsert), a no → `RejectFace`/`RejectLabel` (permanent, idempotent,
  audited in the mutation's transaction); a vanished target (`ErrPhotoNotFound`/`ErrMarkerNotFound`/
  `ErrSubjectNotFound`/`ErrLabelNotFound`/`ErrTargetNotFound`) → `result:"gone"`, not an error; invalid
  input → `ErrInvalidQuestion`/`ErrInvalidAnswer`. Unit tests with fakes (band, ordering, interleaving,
  determinism, cache TTL, skip, idempotence, gone), integration tests over real
  sweep+candidates+expand+facematch+feedback+DB. Additionally **`LeaderboardStore`** (`NewLeaderboardStore(
  pool)`, separate from `Service` — read-only) aggregates a **review leaderboard** directly from `audit_log`: per
  `actor_uid` it counts decisions marked `details.via = "review"` — yes = `face.assign`+`label.attach`,
  no = `face.reject`+`label.reject`; a skip writes nothing, so it isn't counted — with the windows `WindowAllTime`/
  `WindowWeek`/`WindowToday` (`ParseWindow` maps `?window=`, empty → all, other → `ErrInvalidWindow`;
  `windowCutoff` computes the bound from `created_at`), a NULL actor is skipped, ordered total desc → yes desc →
  `display_name` (fallback to `username`); so that a review face confirmation also lands in the leaderboard,
  `applyFaceYes` sends `AssignRequest.Via = "review"` into facematch `Service.Apply` (until now the only
  unmarked of the four actions). Unit tests `ParseWindow`/`windowCutoff` + an integration test of windows, the yes/no
  split, ordering, NULL-actor/non-review exclusion; for the partial index see migration `0037`),
  `internal/reviewapi/`
  (an HTTP API over the review game: the `Service` interface (satisfied by `*review.Service`) and `Leaderboarder`
  (satisfied by `*review.LeaderboardStore`), `NewAPI(Config{Service,Leaderboard,RequireWrite,RequireAuth})`
  (nil guards → pass-through) + `RegisterRoutes` mounts `GET /review/queue` and `POST /review/answer`
  behind **`RequireWrite`** (editor/admin — they mutate the library) and `GET /review/leaderboard` behind **`RequireAuth`**
  (only aggregates → any logged-in user, even a viewer); the queue reads `?limit=` (empty → default,
  non-numeric/negative → 400), answer decodes `{question_id,answer}` (`DisallowUnknownFields`, 64 KiB,
  empty fields → 400) and builds `audit.Meta` via `audit.FromRequest` + `auth.UserFromContext`;
  `ErrInvalidQuestion`/`ErrInvalidAnswer` → 400, any other error → 500, `result:"gone"` stays 200;
  the leaderboard reads `?window=all|7d|today` (default all, `ParseWindow` → 400 on other) and via
  `buildLeaderboardResponse` returns `{window,caller_uid,entries:[…is_me]}` (the caller from `auth.
  UserFromContext`, `is_me` on the own row, entries never null); 503 without a backend; mounted
  in `serve` (`buildReviewAPI` in `cmd/kukatko/review.go`)),
  `internal/peopleapi/`
  (a read/curation HTTP API over subjects (people/animals/other) — the basis of the People UI: the interfaces
  `SubjectStore` (a subset of `people.Store`: `ListSubjects`/`GetSubjectByUID`/`CreateSubjectAudited`/
  `UpdateSubjectAudited`/`DeleteSubjectAudited`/`ListPhotoUIDsBySubject` — each mutation takes an `audit.Entry`
  built in `auditEntry` (`subject.create`/`update`/`delete`, actor from the auth context, details name/type;
  `DELETE` first loads the subject for the details and a clean 404)) and `PhotoStore` (`photos.Store.ListByUIDs`)
  → unit-testable with fakes without a DB; `NewAPI(Config{Subjects,Photos,RequireAuth,RequireWrite})`+
  `RegisterRoutes` mounts **flat** paths (not a mounted subrouter, so they coexist with
  `outlierapi`'s `GET /subjects/{uid}/outliers` without a chi Mount conflict): `GET /subjects`
  (RequireAuth, `{subjects:[SubjectCount]}` with marker counts), `POST /subjects` (RequireWrite,
  create → 201, name/type validation), `GET /subjects/{uid}` (RequireAuth), `PATCH /subjects/{uid}`
  (RequireWrite, editing name/type/favorite/private/notes/cover_photo_uid), `DELETE /subjects/{uid}`
  (RequireWrite → 204), `GET /subjects/{uid}/photos` (RequireAuth, a paginated gallery of the subject's photos
  `{photos,total,limit,offset,next_offset}` — `ListPhotoUIDsBySubject` (distinct non-invalid
  markers, non-archived, newest-first) → page → `ListByUIDs` → reorder by the uid order); body
  decode `DisallowUnknownFields` + 1 MiB limit + empty name → 400; sentinels mapped
  `ErrSubjectNotFound`→404/`ErrInvalidType`→400; mounted by the eighth `server.WithAPI`
  (`buildPeopleAPI` in `cmd/kukatko/people.go`)), `internal/organize/`
  (the DB layer for **organization** — albums, labels, **per-user favorites** (replacing the global
  `photos.favorite` from photo-sorter) and **per-user ratings** (0–5 stars + a personal flag none/pick/reject/eye);
  tables `albums`/`album_photos`/`labels`/`photo_labels`/
  `user_favorites` in migration `0011_albums_labels_favorites.sql` and `user_ratings` in migration
  `0016_user_ratings.sql`: **`albums`** = `uid PK`
  (prefix `al`), `slug UNIQUE` (Slugify from `title`, a numeric suffix on collision), `title`/`description`,
  `type IN (album|folder|moment|state|month)`, `cover_photo_uid` (FK photos `ON DELETE SET NULL`),
  `private`, `created_by` (FK users
  `ON DELETE SET NULL`), timestamps — the `order_by` column was removed by migration
  `0022_chronological_albums.sql` (an album always displays chronologically, there is no sort choice);
  **`album_photos`** = membership `(album_uid, photo_uid) PK`, both FK
  `ON DELETE CASCADE`, `added_at` (the manual position `sort_order` was removed by the same migration); **`labels`** = `uid PK` (prefix `lb`), `slug UNIQUE`
  (from `name`), `name`, `priority`, timestamps; **`photo_labels`** = attachment `(photo_uid, label_uid) PK`,
  both FK `ON DELETE CASCADE`, `source IN (manual|ai|import)`, `uncertainty` (int %), `created_at`;
  **`user_favorites`** = `(user_uid, photo_uid) PK`, both FK `ON DELETE CASCADE`, `added_at`;
  **`user_ratings`** = `(user_uid, photo_uid) PK`, both FK `ON DELETE CASCADE`, `rating SMALLINT 0..5`
  (CHECK), `flag TEXT IN (none|pick|reject|eye)` (CHECK; `eye` added by migration 0025, `pick`/`reject`
  = 👍/👎, `eye` = 👁), `updated_at` — a row exists only for
  a non-default value (the store deletes a row that falls back to rating 0 + flag `none`), so a photo with no
  row = rating 0 / flag `none`;
  `Store` = `NewStore(pool)` over the shared pgx pool: **albums** `CreateAlbum`/`GetAlbumByUID`/
  `GetAlbumBySlug`/`UpdateAlbum` (re-slug from title)/`ListAlbums` → `[]AlbumSummary` (ordered **by the
  newest album**: `MAX(p.taken_at) DESC NULLS LAST, a.uid` — undated and empty albums
  aggregate NULL and go last, `uid` makes the order total and stable; **no COALESCE on
  `created_at`** — for an album that would give an undated album the upload time and float it to the top;
  `AlbumCount` + `CoverUID`/`TakenFrom`/`TakenTo` — all computed **in one SQL**, without a
  migration: `photo_count` from a LEFT JOIN on `album_photos`, `MIN`/`MAX(taken_at)` from a LEFT JOIN on `photos`
  with `archived_at IS NULL`, a fallback cover from `LEFT JOIN LATERAL … ORDER BY taken_at DESC NULLS LAST,
  uid LIMIT 1`; `CoverUID = COALESCE(cover_photo_uid, fallback)` → a manually chosen cover wins,
  otherwise the newest **live** photo, deterministically the same on every query. An archived photo is
  counted in `photo_count`, but supplies neither the cover nor shifts the range; an undated photo can be the cover,
  but doesn't enter the range)/
  `SearchAlbums(q,limit)` (accent/case-insensitive ILIKE over `immutable_unaccent(title/description)`,
  with counts → `[]AlbumCount`, cap limit — the basis of `globalsearchapi`)/
  `DeleteAlbum`/`AddPhoto` (idempotent, no position — `ON CONFLICT DO NOTHING`)/`RemovePhoto`
  (idempotent)/`SetCover` (set/clear cover)/`ListPhotoUIDs`
  (chronologically: `COALESCE(taken_at, created_at), photo_uid` via a JOIN on `photos`); **labels** `CreateLabel`/`GetLabelByUID`/`GetLabelBySlug`/`UpdateLabel`
  (re-slug)/`ListLabels` (with counts, ordered priority DESC)/`SearchLabels(q,limit)` (accent/case-insensitive
  ILIKE over `immutable_unaccent(name)`, with counts, cap limit — the basis of `globalsearchapi`)/`DeleteLabel`/
  `AttachLabel` (idempotent upsert source/uncertainty)/`DetachLabel` (idempotent)/`ListPhotoUIDsByLabel`; **favorites**
  `AddFavorite`/`RemoveFavorite` (both idempotent)/`IsFavorite`/`ListFavorites` (per-user,
  newest-first)/`FavoritedAmong` (from a set of photo uids returns the per-user subset of favorites as a
  set — annotates a whole page's `is_favorite` in one query); **ratings** (`ratings.go`)
  `SetRating(user,photo,rating)` (validation 0–5 → `ErrInvalidRating`) / `SetFlag(user,photo,flag)`
  (validation none/pick/reject/eye → `ErrInvalidFlag`) — idempotent upsert of one column in a transaction,
  the other column is preserved; when the row falls back to rating 0 + flag `none`, it is deleted (the table stays
  sparse); `ClearRating(user,photo)` deletes both rating and flag in one idempotent DELETE (mirror of
  `RemoveFavorite`, a no-op on an unrated/missing photo — the basis of `DELETE /photos/{uid}/rating`);
  `GetRating(user,photo)` → `PhotoRating{Rating,Flag}` (a missing row = 0/`none`, nil err);
  `RatingsAmong(user,photoUIDs)` → a map `photo_uid → PhotoRating` only for rated photos (annotates
  a whole page in one query, mirror of `FavoritedAmong`, a missing caller defaults to 0/`none`);
  types `AlbumType`/`LabelSource`/`RatingFlag` (none/pick/reject/eye)
  mirror the SQL CHECKs, a slug helper with a per-kind
  fallback (`album`/`label`); sentinels `ErrAlbumNotFound`/`ErrLabelNotFound`/`ErrPhotoNotFound`/
  `ErrUserNotFound`/`ErrSlugExhausted`/`ErrInvalidType`/`ErrInvalidSource`/`ErrInvalidRating`/
  `ErrInvalidFlag` — an FK violation when writing
  to the join tables (`user_favorites`/`user_ratings`) is mapped to a not-found sentinel by the violated
  column via the shared `translateUserPhotoFK` (`photo_uid` → photo, otherwise user;
  album/label via `translateMembershipFK`/`translateAttachFK`);
  **audited variants** of the mutations (`audit.go`): `CreateAlbumAudited`/`UpdateAlbumAudited`/`DeleteAlbumAudited`/
  `AddPhotosAudited`/`RemovePhotosAudited` and `CreateLabelAudited`/`UpdateLabelAudited`/`DeleteLabelAudited`/
  `AttachLabelAudited`/`DetachLabelAudited` run the change and `audit.Write` **in one transaction** (durable
  audit — when the mutation rolls back, no audit record is created; the shared `inAuditedTx` +
  `insertAuditedWithUniqueSlug`, which resolves a slug collision on create/update by retrying through separate transactions
  and writes the audit only for the successful attempt); the non-audited variants remain for the system importers
  (`psimport`/`ppimport`, without an actor)), `internal/organizeapi/`
  (a read/curation HTTP API over albums and labels — the basis of the Albums/Labels UI: the interfaces `AlbumStore`/
  `LabelStore` (subsets of `organize.Store`) → unit-testable with fakes without a DB;
  `NewAPI(Config{Albums,Labels,RequireAuth,RequireWrite})`+`RegisterRoutes` mounts two
  subrouters: **albums** `GET /albums` (RequireAuth, `{albums:[AlbumSummary]}` — counts, the effective
  `cover_uid` and the range `taken_from`/`taken_to`),
  `POST /albums` (RequireWrite, 201, `title` required, type validation via `ErrInvalidType`),
  `GET /albums/{uid}` (RequireAuth), `PATCH /albums/{uid}` (RequireWrite, edits
  title/description/cover_photo_uid/private; **the structural `type` is preserved** —
  the handler loads the existing album and does not take `type` from the body, so folder/moment/… can't be overwritten),
  `DELETE /albums/{uid}` (RequireWrite → 204), membership `POST /albums/{uid}/photos`
  `{photo_uids:[…]}` (adds, no position — an album is always chronological),
  `DELETE /albums/{uid}/photos` `{photo_uids:[…]}` (removes, idempotent) — both
  membership endpoints return the current chronological order `{photo_uids:[…]}`, first verifying the
  existence of the album (`requireAlbum` → 404); manual reordering `PATCH /albums/{uid}/order` was
  removed (→ 404); **labels** `GET /labels` (RequireAuth, `{labels:[LabelCount]}`),
  `POST /labels` (RequireWrite, 201, `name` required), `GET /labels/{uid}` (RequireAuth),
  `PATCH /labels/{uid}` (RequireWrite, name/priority), `DELETE /labels/{uid}` (RequireWrite → 204),
  attachment `POST /labels/{uid}/photos` `{photo_uid,source?,uncertainty?}` → 204 (source validation
  via `ErrInvalidSource`), `DELETE /labels/{uid}/photos` `{photo_uid}` → 204 (verifies the existence of the
  label → 404, then an idempotent detach); body decode `DisallowUnknownFields` + 1 MiB limit;
  **each mutation writes exactly one audit record in the same transaction** (calls the audited store variants,
  the actor from `auth.UserFromContext` + `audit.FromRequest`, actions `album.create`/`update`/`delete`/
  `add_photos`/`remove_photos` and `label.create`/`update`/`delete`/`attach`/`detach`; add/remove of photos =
  one batch record with `photo_uids`/`count`, attach/detach carries `photo_uid` in details); the responses
  don't change; sentinels mapped `ErrAlbumNotFound`/`ErrLabelNotFound`/`ErrPhotoNotFound`→404,
  `ErrInvalidType`/`ErrInvalidSource`→400; **browsing an album's/label's photos has no own endpoint** —
  it goes through the shared `GET /photos` scoped `?album={uid}`/`?label={uid}` (see `photos.ListParams`
  `AlbumUID`/`LabelUID` + `photoapi` `parseListParams`); mounted by another `server.WithAPI`
  (`buildOrganizeAPI` in `cmd/kukatko/organize.go`, sharing one `organize.Store` for both albums and labels)),
  `internal/feedback/`
  (the DB layer for **persisted rejections** (negative feedback) — a permanent user "no" to a face↔subject
  or photo↔label guess; it closes the photo-sorter gap where a rejection wasn't kept and the same
  wrong face was offered endlessly, so the review work never shrank; tables `face_rejections`/
  `label_rejections` in migration `0031_feedback_rejections.sql`: `face_rejections` keyed by the face identity
  (`photo_uid`+`face_index`, as in `internal/facematch` and the `faces` table) + `subject_uid`,
  `label_rejections` keyed by `photo_uid`+`label_uid`; both carry `rejected_by` (FK users
  `ON DELETE SET NULL`) and `rejected_at`, a **UNIQUE natural key** (rejecting twice = a no-op via
  `ON CONFLICT DO NOTHING`), FK photos/subjects/labels `ON DELETE CASCADE`; **`face_rejections`
  deliberately has NO FK on `faces`** — faces are deleted and re-inserted on re-detection, so a cascade
  would delete the rejection (it must survive it); `Store` = `NewStore(pool)`: `RejectFace`/`RejectLabel`
  (idempotent audited insert, `rejected_by` from `entry.ActorUID`, an FK violation → `ErrTargetNotFound`),
  `UnrejectFace`/`UnrejectLabel` (undo, audited, a no-op when there is nothing), `IsFaceRejected`/
  `IsLabelRejected` (a pair check), **bulk lookups** `FaceRejectionsForSubject(subjectUID)` (→ `[]FaceRef`
  = `photo_uid`+`face_index` exclusion keys) and `LabelRejectionsForLabel(labelUID)` (→ `[]photoUID`) as an
  exclusion filter of the search paths **without N+1**; every write goes through `audit.Write` **in the same transaction** as the
  mutation (the shared `inAuditedTx`, the `internal/organize` convention); **a rejection is an opinion — it never mutates**
  the underlying data (doesn't delete a face, doesn't detach a marker, doesn't remove a label); sentinels `ErrEmptyKey`(→400)/
  `ErrTargetNotFound`(→404);
  **Confirmations** (`confirmations.go`, table `face_confirmations`, migration
  `0032_face_confirmations.sql`) are the **opposite polarity**: "this face **IS** this person,
  the assignment is correct". The same shape and rules as `face_rejections` (key
  `photo_uid`+`face_index`+`subject_uid`, `UNIQUE natural key` → a double confirmation = a no-op,
  `confirmed_by` FK users `ON DELETE SET NULL`, FK photos/subjects `ON DELETE CASCADE`, **no FK
  on `faces`** for the same reason — re-detection of a face deletes and re-inserts);
  `ConfirmFace`/`UnconfirmFace` (idempotent audited insert/delete, actions `face.confirm`/
  `face.unconfirm`), `IsFaceConfirmed`, bulk `FaceConfirmationsForSubject(subjectUID)` (→ `[]FaceRef`).
  **Why it exists:** outlier review needs to record "no, this really is them" — and using
  `RejectFace` for that would write **the exact opposite** of what the user said. `internal/outliers` excludes confirmed
  faces, so a list that keeps offering the same false alarms converges).
  **Duplicate dismissals** (`dismissals.go`, table `duplicate_dismissals`, migration
  `0034_duplicate_dismissals.sql`) are a third kind of opinion: "these two photos are **NOT** duplicates".
  Keyed by an **unordered pair** `photo_uid`+`other_uid`, which the store normalizes (smaller uid
  first, **bytewise** like Go's `<`) and the DB enforces it with `CHECK (photo_uid COLLATE "C" < other_uid COLLATE
  "C")` — only that turns the `UNIQUE` into "one row per pair" instead of "one per direction". `COLLATE "C"`
  (migration `0038`) must match the bytewise ordering of `normalized()`; the default `en_US.utf8` orders `_` differently,
  so without it a uid with an underscore would trip the CHECK instead of the expected FK/`ErrTargetNotFound`. Both uids
  FK photos `ON DELETE CASCADE`, `dismissed_by` FK users
  `ON DELETE SET NULL`. **The pair is keyed, not the group:** a group is a connected component and is not
  stable (adding one photo merges two groups), whereas a pair is an edge the detector actually
  drew. `DismissDuplicate`/`UndismissDuplicate` (idempotent audited insert/delete, actions
  `duplicate.dismiss`/`duplicate.undismiss`), `IsDuplicateDismissed`, bulk
  `DismissedDuplicatePairs()` (→ `[]DuplicateDismissalKey`, the whole table in one query — detection
  scans the catalog in one pass and needs the whole exclusion set up front); sentinel `ErrSamePhoto`
  (→400, a photo isn't a duplicate of itself). **Why it exists:** duplicate detection is derived state,
  recomputed on every `GET /duplicates` from hashes and embeddings, which the user's
  disagreement doesn't change — without persistence the same pair would be offered forever),
  `internal/feedbackapi/`
  (an HTTP API over rejections — the `Store` interface (a subset of `feedback.Store`) → unit-testable with fakes;
  `NewAPI(Config{Store,RequireWrite})`+`RegisterRoutes` mounts the subrouter `/feedback`:
  `POST /feedback/face-rejections` `{photo_uid,face_index,subject_uid}` (RequireWrite → 204),
  `DELETE /feedback/face-rejections` (undo → 204), `POST /feedback/label-rejections`
  `{photo_uid,label_uid}` (→ 204), `DELETE /feedback/label-rejections` (→ 204) — DELETE carries a body too
  (like label-detach); body decode `DisallowUnknownFields` + 64 KiB, a missing id → 400, a negative
  `face_index` → 400; **each mutation writes an audit record in the same transaction** (the actor from
  `auth.UserFromContext` + `audit.FromRequest`, actions `face.reject`/`face.unreject`/`label.reject`/
  `label.unreject`, `entry.ActorUID` is also `rejected_by`); `ErrTargetNotFound`→404, `ErrEmptyKey`→400,
  otherwise 500; mounted by another `server.WithAPI` (`buildFeedbackAPI` in `cmd/kukatko/feedback.go`)),
  `internal/savedsearch/`
  (the DB layer for **per-user saved searches** ("smart albums") — a named, owner's private
  filter/search definition the user reopens; mirrors the per-user ownership of
  `user_favorites`; the `saved_searches` table in migration `0017_saved_searches.sql`: `uid PK` (prefix `ss`),
  `owner_uid` FK users `ON DELETE CASCADE`, `name TEXT NOT NULL`, `params JSONB NOT NULL` (the opaque
  stored state of the view/search: filters, sorting, query, mode), `created_at`/`updated_at`, an index on
  `owner_uid`; `Store` = `NewStore(pool)`: `Create(ctx,ownerUID,name,params)`/`List(ctx,ownerUID)`
  (newest-first by `created_at`)/`Get(ctx,uid)`/`Update(ctx,uid,name,params)` (overwrites name+params,
  stamps `updated_at`)/`Delete(ctx,uid)`; `params` as `json.RawMessage` (empty → `{}`, so the NOT NULL
  column gets valid JSON), `Get`/`Update`/`Delete` on a missing row → the sentinel `ErrNotFound`;
  ownership is **not handled by the store** — the HTTP layer above it scopes it)), `internal/savedsearchapi/`
  (a read/curation HTTP API over saved searches: the `Store` interface (a subset of `savedsearch.Store`) →
  unit-testable with fakes; `NewAPI(Config{Store,RequireAuth})`+`RegisterRoutes` mounts
  `/saved-searches` **all behind `RequireAuth`** and **scoped to the logged-in user** from the auth context
  (`auth.UserFromContext`): `GET /saved-searches` (`{saved_searches:[{uid,name,params,created_at,
  updated_at}]}` of the current user, owner_uid is deliberately not shown in the view), `POST /saved-searches`
  `{name,params}` → 201 (an empty name → 400, `params` optional → `{}`), `GET /saved-searches/{uid}`
  → 200, `PATCH /saved-searches/{uid}` `{name?,params?}` → 200 (an omitted field unchanged, an empty
  name → 400), `DELETE /saved-searches/{uid}` → 204; **ownership isolation** — the shared helper
  `ownedSearch` loads the row and compares `owner_uid` with the actor, a foreign one (even a non-existent one) → **404** (never
  reveals someone else's search); the body `DisallowUnknownFields` + 1 MiB limit, sentinel `ErrNotFound`→404;
  mounted by `server.WithAPI` (`buildSavedSearchAPI` in `cmd/kukatko/savedsearch.go`)), `internal/announcement/`
  (the DB layer for **a single instance-wide announcement** — a short message the administrator publishes and every
  logged-in user sees as a banner at the top; the single-row `announcements` table in migration
  `0039_announcement.sql`: `id BOOLEAN PK DEFAULT true CHECK (id)` (a single-row invariant → publish is
  an **upsert**), `message TEXT NOT NULL`, `level TEXT NOT NULL DEFAULT 'info' CHECK (info|warning)`,
  `author_uid VARCHAR(32)` FK users `ON DELETE SET NULL` (losing the author must not take down a live announcement),
  `updated_at TIMESTAMPTZ`; `Store` = `NewStore(pool)`: `Get(ctx)` (→ the sentinel `ErrNotFound` when nothing),
  `Set(ctx,message,level,authorUID,entry)` (upsert + validation: an empty/whitespace message → `ErrEmptyMessage`,
  an unknown level → `ErrInvalidLevel`, an empty level → `info`), `Clear(ctx,entry)` (delete); **both publish and clear
  write the audit** (`announcement.set`/`announcement.clear`, message/level into details) in the **same transaction**
  as the change (mirrors `internal/organize`)), `internal/announcementapi/`
  (a dual-guard HTTP API over the announcement: the `Store` interface (a subset of `announcement.Store`) → unit-testable
  with a fake; `NewAPI(Config{Store,RequireAuth,RequireMaintainer})`+`RegisterRoutes` mounts `/announcement`:
  `GET /` behind `RequireAuth` (anyone logged in reads; when nothing is published → **200 `{"message":""}`** instead of 404,
  friendlier for the polling banner client), `PUT /` and `DELETE /` behind `RequireMaintainer` (publish/clear,
  `author_uid` = the actor from the auth context); the body `{message,level}` with `DisallowUnknownFields` + 16 KiB limit,
  `ErrEmptyMessage`/`ErrInvalidLevel` → 400, response `{message, level?, author_uid?, updated_at?}`
  (`updated_at` RFC3339, otherwise omitted); mounted by `server.WithAPI` (`buildAnnouncementAPI` in
  `cmd/kukatko/announcement.go`)), `internal/globalsearchapi/`
  (a grouped **global search** HTTP API across entities — the basis of the navbar quick-results and the cross-entity section
  of the search page: the small interfaces `Organizer` (`SearchAlbums`/`SearchLabels`, satisfied by `organize.Store`),
  `PeopleSearcher` (`SearchSubjects`, satisfied by `people.Store`) and `PhotoSearcher` (`Search`, satisfied by
  `photos.Store` — reusing the existing fulltext via `ListParams.FullText`) → unit-testable with fakes;
  `NewAPI(Config{Organizer,People,Photos,Limit,RequireAuth})`+`RegisterRoutes` mounts
  `GET /search/global?q=` behind `RequireAuth`: handles each group separately (`SearchAlbums`/`SearchLabels`/
  `SearchSubjects` capped at `Limit`, default `defaultGroupLimit` 8; photos via fulltext with `Limit`),
  returns a grouped envelope `{query, albums:[{uid,title,cover,photo_count}], labels:[{uid,name,photo_count}],
  people:[{uid,name,cover}], photos:[…usual photo shape…]}` (each group always a non-nil array); an empty/
  whitespace `q` → 400, a store error → 500; mounted by `server.WithAPI` (`buildGlobalSearchAPI` in
  `cmd/kukatko/globalsearch.go`, sharing the organize/people/photos store)), `internal/placesapi/`
  (a read-only HTTP API over the reverse-geocoded place hierarchy — the basis of Places browse: the interface
  `Store` (a subset of `photos.Store`: `AggregatePlaces`) → unit-testable with a fake; `NewAPI(Config{
  Store,RequireAuth})`+`RegisterRoutes` mounts `GET /places` behind `RequireAuth`: a hierarchy with counts
  `{places:[{country,count,cities:[{city,count}]}]}` aggregated over non-archived photos with place data
  (a country's count includes photos without a city too, cities always an array; ordered count desc/name), an optional
  `?country=` drills only into the cities of one country; photos without place data are excluded (`photos.Store.
  AggregatePlaces` computes it with one `GROUP BY country, city` joining on `photo_places`). **Browsing a
  locality's photos has no own endpoint** — it goes through the shared `GET /photos` scoped `?country=`/`?city=`
  (`photos.ListParams` `Country`/`City` + `photoapi` `parseListParams`); mounted by `server.WithAPI`
  (`buildPlacesAPI` in `cmd/kukatko/places.go`, aggregation via the photos store over the `photo_places` cache)),
  `internal/audit/`
  (durable audit trail, tabulka `audit_log` v migraci `0012_audit_log.sql` rozšířená v
  `0014_audit_request.sql` o `ip`/`user_agent` + composite index `(target_type, target_uid)`:
  `id BIGSERIAL`, `actor_uid` FK users `ON DELETE SET NULL`, `action`, `target_type`, `target_uid`,
  `details JSONB`, `ip`, `user_agent`, `created_at` (sloupcová jména `actor/target/details` =
  spec termíny `user/entity/metadata`); **klíčový vzor** `Write(ctx, exec, Entry)` zapisuje přes
  rozhraní `Execer` (splňuje ho pool **i** `pgx.Tx`), takže audit řádek jede v **téže transakci**
  jako mutace — commitne/rollbackne s ní (ARCHITECTURE §5.1/§11/§12 „audit log durable", oprava
  photo-sorter after-commit mezery); `Entry{ActorUID,Action,TargetType,TargetUID,Details,IP,
  UserAgent}` (prázdné UID/IP/UA → SQL NULL, nil details → `{}`); **konvence pro handlery**
  `Meta` + `FromRequest(r, actorUID)` (actor z auth kontextu, IP z `X-Forwarded-For`/`X-Real-IP`/
  `RemoteAddr`, UA z hlavičky) → `(Meta).Entry(action, targetType, targetUID, details)` staví
  ostatní entry; **konvence `changes` pro editace** (`changes.go`): `ChangeSet` = `NewChangeSet()` +
  `Add(field, old, new)` (přeskočí neměněná pole přes `reflect.DeepEqual`, ukazatele porovná
  hodnotou) → `Map()`/`StampInto(details)` zapíše pod klíč `ChangesKey` (`"changes"`) mapu
  `{"<pole>":{"old":…,"new":…}}` **jen se skutečně změněnými poli** (nil ukazatel → JSON `null`);
  používají ji všechny editační cesty (foto PATCH + MCP `photo.update`, album/label/subjekt update),
  aby log ukázal `stary popisek` → `novy popisek`; **hromadná editace `internal/bulk` je záměrně
  vynechaná** (jeden `UPDATE` nad mnoha fotkami bez načtení starých řádků — SELECT-před-UPDATE by
  zdvojnásobil dotazy na dávku), ponechává si původní souhrn v details; action konstanty `ActionPhotosBulk`/`ActionPhoto{Update,Archive,Unarchive,Purge}`/
  `ActionAlbum{Create,Update,Delete}`/`ActionLabel{Create,Update,Delete}`/`ActionFaceAssign`/
  `ActionUser{Create,Update,Disable,Password}`/`ActionAuditPurge`; `Store` = `NewStore(pool)` se `Record(ctx,Entry)`
  (vlastní spojení) a **filtrovaným čtením** `List(ctx,Filter)`/`Count(ctx,Filter)` (`Filter{ActorUID,
  TargetType,TargetUID,Action,Since,Until,Limit,Offset}`, newest-first, limit cap 500/default 100)
  pro admin výpis; **retenční purge** `PurgeOlderThan(ctx, cutoff) (int, error)` = jeden
  `DELETE FROM audit_log WHERE created_at < $1` přes `idx_audit_log_created_at`, vrací počet smazaných
  (maintainer-only přes `internal/maintenanceapi`, action `audit.purge`, sám se auditne — čerstvý
  záznam purge přežije). **Zapojené in-tx mutace**: bulk (`internal/bulk`) + foto PATCH/archive/unarchive
  přes audited varianty `photos.Store.{UpdateMetadata,Archive,Unarchive}Audited`, **trvalý purge**
  `photos.Store.DeleteAudited` (`internal/trash` → `photo.purge`, systémový actor u plánované retence)
  a **správa uživatelů** `auth.Store.{CreateUser,UpdateUserProfile,SetUserDisabled,SetPasswordHash}Audited`
  (`user.*`) — vše mutace + audit v jedné tx přes sdílený `rowQuerier`/`mutateAudited` (photos) resp.
  `inAuditedTx` (auth); další domény (alba/štítky/lidé) následují stejnou konvenci), `internal/auditapi/`
  (admin-only HTTP API nad audit trailem: `NewAPI(Config{Store,RequireAdmin})`+`RegisterRoutes`
  mountuje `GET /audit` za `RequireAdmin`; `parseFilter` z query `user`/`entity_type`/`entity_uid`/
  `action`/`via`/`decision`/`since`/`until` (RFC3339)/`limit`/`offset` → `audit.Filter` (neplatný
  čas/číslo/`via`/`decision` → 400), vrací `{entries,total,limit,offset,next_offset}` newest-first;
  **`via=review`** → `Filter.ReviewOnly` (literál `details ->> 'via' = 'review'`, sedí na partial
  index 0037), **`decision=yes|no`** → `Filter.Actions` (Ano = `face.assign`+`label.attach` / Ne =
  `face.reject`+`label.reject`) — podklad pro admin per-user přehled review rozhodnutí; jen čtení — zápisy jdou přes
  mutační transakce jinde; mountuje se vždy posledním `server.WithAPI` (`buildAuditAPI` v
  `cmd/kukatko/audit.go`)), `internal/bulk/`
  (hromadná editace metadat: `Service` = `NewService(pool, maxBatch)` s `Apply(ctx, actorUID,
  photoUIDs, ops Operations) (Result, error)` — **celá dávka v jediné transakci** s audit
  záznamem; `Operations` = volitelná pole `AddAlbums`/`RemoveAlbums`/`AddLabels`/`RemoveLabels`,
  `Title`/`Description *string` (nil=beze změny, ""=clear), `Location *Location`+`ClearLocation`,
  `Archive`/`Favorite *bool`, **`Rating *int` (0–5) + `Flag *string` (none/pick/reject/eye)**;
  `Apply` validuje dávku (ErrNoPhotos/ErrNoOperations/
  ErrBatchTooLarge), ověří existenci alb/štítků v add operacích (ErrAlbumNotFound/ErrLabelNotFound),
  pak per-foto: duplicitní uid → `skipped`, neexistující fotka → `error` **bez abortu ostatních**,
  jinak aplikuje a `updated`; vlastní idempotentní SQL (vlastní tx kvůli atomicitě, nepoužívá
  organize/photos store metody, které mají vlastní spojení); favorite **i hodnocení** jsou
  **per-user** (`actorUID`) — rating/flag upsert + prune all-defaults řádku zrcadlí `organize` store;
  `Result{Results:[{photo_uid,status,error?}],Counts{total,updated,skipped,errored}}`; skutečná DB
  chyba rollbackne celou dávku; an archive operation additionally calls `photos.LeaveStackTx` for each
  archived photo **in the same transaction**, so archiving a stack's primary does not hide its still-live
  siblings behind the `(stack_uid IS NULL OR stack_primary)` gate (an unarchive leaves stacks untouched);
  `Summary()` (audit details) + `IsEmpty()`), `internal/bulkapi/`
  (HTTP nad `bulk.Service`: rozhraní `Service` (Apply) — fakeovatelné; `NewAPI(Config{Service,
  RequireWrite})`+`RegisterRoutes` mountuje `POST /photos/bulk` za `RequireWrite`; tělo
  `{photo_uids,operations}` přes `operationsInput` se **set/clear páry jako samostatné klíče**
  (jednoznačné, konflikt `set_*`+`clear_*` / `archive`+`unarchive` → 400), `set_caption`→title,
  **`set_rating` (0–5) / `set_flag` (none/pick/reject/eye)** s validací → 400,
  validace souřadnic, `DisallowUnknownFields` (neznámá operace → 400) + 4 MiB limit; chyby mapované
  `ErrNoPhotos`/`ErrNoOperations`/`ErrAlbum/LabelNotFound`→400, `ErrBatchTooLarge`→413, jinak 500;
  per-foto chyby vrací 200 s detailem v těle; mountuje se dalším `server.WithAPI`
  (`buildBulkAPI` v `cmd/kukatko/bulk.go`)),
  `internal/mapy/`
  (server-side HTTP klient k mapy.com REST API, **klíč nikdy neopustí server** — posílá se jen
  v hlavičce `X-Mapy-Api-Key`, nikdy v URL/chybě, vše za rozhraním `Client` (fakeovatelné):
  `New(Config{BaseURL,APIKey,Lang,Timeout,HTTPClient})` → `*HTTPClient`; `Tile(ctx,TileParams{
  Mapset,Z,X,Y,Retina}) (*TileResult,error)` (validuje mapset allowlist, staví URL
  `/v1/maptiles/{mapset}/256[@2x]/{z}/{x}/{y}`, **streamuje** body přes `cancelReadCloser` který
  na Close zruší request ctx — nikdy nedrží dlaždici v RAM), `ReverseGeocode(ctx,lat,lng)
  (*GeocodeResult,error)` (`/v1/rgeocode?lon=&lat=&lang=cs` → zjednodušený první `item` na
  `{Name,Location,RegionalStructure}`), `Geocode(ctx,query,limit) ([]Place,error)` (**forward**,
  `/v1/geocode?query=&lang=cs&limit=` → `[]Place{Name,Label,Type,Location,Lat,Lng}` v pořadí od
  nejlepší shody; mapuje `position.lon/lat` → `Lng/Lat` a zahazuje bbox/zip/regionalStructure;
  prázdný dotaz = `ErrEmptyQuery` **bez volání nahoru**, žádná shoda = **prázdný slice + nil**,
  ne `ErrNotFound` — nedopsaný název není chyba; `ClampGeocodeLimit` ořízne na
  1–`MaxGeocodeLimit` (15), ≤0 → `DefaultGeocodeLimit` (5), a volá se i uvnitř `Geocode`, takže
  žádné call-site nepošle nahoru neomezený počet); allowlist `basic|outdoor|aerial|winter`
  (`IsValidMapset`), retina jen `basic`/`outdoor` (`RetinaSupported`); sentinely
  `ErrUnauthorized` (401/403) / `ErrNotFound` (404 i prázdné items) / `ErrRateLimited` (429) /
  `ErrUpstream` (jiný status / nečitelná odpověď) / `ErrUnavailable` (transport / 502/503/504) /
  `ErrInvalidMapset` / `ErrInvalidURL`; `statusError` **nepřidává tělo** odpovědi do chyby, aby
  klíč neprosákl ani když ho mapy.com echoují; každý non-200 se navíc **loguje WARN** se
  statusem + mapsetem (`slog.WarnContext`, 404 z rgeocode ne — to je normální odpověď), takže
  odmítnutý klíč nekončí jen jako šedá dlaždice; **`Health`** (`health.go`, nil-safe, concurrency-
  safe) skládá výsledky volání do `HealthStatus{State,Detail,CheckedAt}`: `Record(err)` klasifikuje
  sentinel → `HealthState` `ok|key_rejected|rate_limited|unavailable|error` (`ErrNotFound`/
  `ErrInvalidMapset`/`context.Canceled` **ignoruje** — o zdraví upstreamu nic neříkají),
  `Snapshot()` čte, `State.Degraded()` = vše kromě `ok`/`unknown`; `Detail` je z chyb klienta,
  takže nikdy nenese klíč), `internal/mapsapi/`
  (HTTP API pro mapy — tile proxy, reverse geocode, place search a GeoJSON feed; rozhraní
  `TileFetcher`/`Geocoder`/`PlaceSearcher` (splňuje je `mapy.Client`, nil → 503) a `PhotoLister`
  (`photos.Store.List`) →
  unit-testovatelné s faky; `NewAPI(Config{Tiles,Geocoder,Places,Photos,Health,RequireAuth,TileCacheMaxAge,
  TileCacheTTL,TileCacheBytes,GeocodeCacheTTL,GeocodeRatePerSec,GeocodeRateBurst,MaxGeoPhotos})`+
  `RegisterRoutes` mountuje
  `/map` za `RequireAuth`: `GET /map/tiles/{mapset}/{z}/{x}/{y}` (validuje mapset→400/retina ze
  sufixu `@2x` na `{y}` nebo `?retina=true`, s `Cache-Control: public, max-age, immutable`;
  **server-side cache** `tileCache` (`tilecache.go`: bounded na bajty + TTL, lazy expiry,
  **LRU** eviction, klíč `mapset/z/x/y[@2x]`) — hit se servíruje z paměti bez volání mapy.com
  (= ušetřený kredit, free tier 1 dlaždice = 1 kredit), miss se streamuje a **jen úspěch** se
  uloží (chyba se **nikdy** necachuje, jinak by výpadek/odmítnutý klíč zamrzl v mapě na celé TTL);
  dlaždice nad `maxCachedTileBytes` (512 KiB) se streamuje bez cachování, takže se nikdy nebufferuje
  celá do RAM; outcome hlásí hlavička `X-Tile-Cache: hit|miss`; chyby přes `writeTileError` →
  404/429/503/502 a **401/403 → `StatusMapKeyRejected` (424)**, tj. vlastní status pro *odmítnutý
  náš klíč* (syrová 403 by lhala, že je špatný request volajícího) — frontend ho pozná a řekne
  proč je mapa prázdná; každé volání upstreamu zapíše výsledek do `mapy.Health` (→ system status)),
  `GET /map/rgeocode
  ?lat=&lng=` (parsuje+range-checkuje souřadnice→400, **TTL+capacity cache** `ttlCache[GeocodeResult]`
  klíč = souřadnice na 5 desetin, uncached lookup přes **token-bucket** `rateLimiter`→429 šetří kredity,
  odpověď zjednodušená + `Cache-Control: private`), `GET /map/geocode?q=&limit=` (`geocode.go`:
  našeptávač pro editor polohy; pořadí guardů je dané cenou — prázdné/>200 znaků `q` → 400 **před**
  voláním, pak `ttlCache[[]Place]` (klíč = `limit` + casefoldnutý dotaz se sraženými mezerami,
  **diakritika zůstává** — `veseli`/`veselí` jsou nahoře různé dotazy), a teprve zbytek jde na
  **stejný `rateLimiter` jako rgeocode** (jeden kreditový rozpočet = jeden limiter) → 429; klient
  navíc debouncuje, tohle je půlka škrtiče, kterou nejde obejít. `limit` se **ořízne**
  (`mapy.ClampGeocodeLimit`), ne 400. `mapy.ErrNotFound` (404 nahoře) se překlápí na **prázdný
  `items` + 200**; jinak `writeGeocodeError` jako u rgeocode. Cache `ttlCache` (`cache.go`,
  generická: TTL + capacity, lazy expiry, evikce nejdřív-expirujícího — schválně **ne LRU**, na
  rozdíl od `tileCache`, protože všechny záznamy jsou stejně drahé), default 2000 záznamů),
  `GET /map/photos` (GeoJSON
  **FeatureCollection**, `parseGeoParams` vynutí `HasGPS=true` + ctí `taken_after`/`taken_before`/
  `album`/`label`/`archived`, `Limit=MaxGeoPhotos`, řazení taken_at desc; každá feature
  `Point` se souřadnicí RFC 7946 `[lng,lat]` a properties `uid`/`title`/`taken_at`/`media_type`/
  relativní `thumb` cesta `tile_224`, fotky bez obou souřadnic se přeskočí); defaulty cache 24h /
  tile cache 64 MiB + 24h / rate 5/s burst 10 / max 50000 features; mountuje se `server.WithAPI`
  (`buildMapsAPI` v `cmd/kukatko/maps.go`, klient i `mapy.Health` se staví jen když je
  `maps.mapy_api_key` nastaven — `newMapsHealth`; stejný tracker dostane i `buildSystemAPI`)),
  `internal/places/`
  (DB vrstva pro **cache reverse-geocoded místa** fotky — country/region/city/place_name resolvnuté
  z GPS přes mapy.com a uložené, aby šla knihovna procházet/filtrovat dle lokality bez opakovaného
  volání rate-limitovaného geokodéru; **schema choice: vedlejší tabulka `photo_places`** (ne sloupce
  na široké `photos`) keyovaná `photo_uid` FK `ON DELETE CASCADE` — místo je řídké (jen geotagované
  fotky mají řádek) a je to odvozená regenerovatelná cache plněná asynchronně jobem, zrcadlí
  `face_detections`/`user_ratings`; migrace `0018_photo_places.sql`: `photo_uid PK`, `country`/
  `region`/`city`/`place_name TEXT NOT NULL DEFAULT ''`, `lat`/`lng DOUBLE PRECISION` (souřadnice,
  ze kterých byl geokód spočítán — detekce změny pozice → re-geokód; NULL u fotky bez GPS, jejíž
  řádek jen značí "zpracováno"), `geocoded_at TIMESTAMPTZ`, indexy na `country` a `city` (grouping/
  filtering dle lokality); `Store` = `NewStore(pool)`: `GetPlace(photoUID)` (`ErrPlaceNotFound`)/
  `SavePlace(Place)` (upsert na `photo_uid`, stampne `geocoded_at`)/`ListPhotosMissingPlaces(limit)`
  (uid nearchivovaných **geotagovaných** fotek bez `photo_places` řádku, newest-first, LEFT JOIN —
  podklad backfillu)), `internal/placesjob/`
  (zapojení reverse geokódování do fronty, vše za rozhraními `PhotoStore`/`PlaceStore`/`Geocoder`
  (podmnožina `mapy.Client`, fakeovatelná)/`Enqueuer`/`RateLimiter` → unit-testovatelné s faky bez
  sítě/DB; `Service` = `New(Config{Photos,Places,Geocoder,Enqueuer,Limiter,OfflineRetryDelay,
  RateLimitDelay})` (panika na nil Photos/Places/Geocoder/Enqueuer, `Limiter` nil → always-allow);
  **handler `places`** `Handle`(=`worker.HandlerFunc`, registrovaný v `serve` když je mapy klíč
  nastaven) → z payloadu `{"photo_uid"}` načte fotku; **idempotentní** (fotka s místem cachovaným pro
  **aktuální** souřadnice se přeskočí; změna souřadnic → re-geokód), fotka **bez GPS** → uloží prázdný
  "processed" marker (nikdy se neretryuje); jinak `mapy.ReverseGeocode(lat,lng)` → `parsePlace`
  parsuje `regional_structure` (typy `regional.country`/`region`/`municipality`, prefix `regional.`
  volitelný) na country/region/city + place_name = nejspecifičtější label, uloží přes
  `places.SavePlace` se zdrojovými souřadnicemi; **mapy.com nedostupné/rate-limited**
  (`mapy.ErrUnavailable`/`ErrRateLimited`) → `worker.RetryAfter(5 min)` (odložení bez spálení pokusu,
  zrcadlí embed job), **`mapy.ErrNotFound`** → processed marker se souřadnicemi (neretryuje se forever),
  jiná chyba normální retry; **respekt k mapy.com kreditům**: `RateLimiter` (token-bucket `NewTokenBucket(
  ratePerSec,burst)`, zrcadlí geocode proxy limiter; `maps.geocode_rate_per_sec`/`geocode_burst`) — když
  je prázdný, `worker.RetryAfter(1 min)` (zpracovat pomalu je OK); `BackfillPlaces(ctx)` zařadí `places`
  pro každou geotagovanou fotku bez místa (dedup no-op), vrací počet), `internal/importer/`
  (evidence běhů importu/migrace + high-watermarky pro **inkrementální, idempotentní** import,
  tabulka `import_runs` v migraci `0013_import_runs.sql`: `id BIGSERIAL`, `source TEXT`
  CHECK `photoprism|photosorter`, `started_at`/`finished_at TIMESTAMPTZ`, `status TEXT`
  CHECK `running|done|failed`, `high_watermark TIMESTAMPTZ` (největší zpracovaný zdrojový
  timestamp, např. max PhotoPrism `UpdatedAt`), `counts JSONB` `{imported,updated,skipped,failed}`,
  `last_error TEXT`; partial index `(source, finished_at DESC) WHERE status='done' AND
  high_watermark IS NOT NULL` pro resume dotaz; typy `Source` (`SourcePhotoPrism`/
  `SourcePhotoSorter` + `Valid()`)/`Status` (`StatusRunning`/`StatusDone`/`StatusFailed`)/`Counts`/
  `Run`; `Store` = `NewStore(pool)`: `Start(ctx,source)` otevře `running` řádek (`ErrInvalidSource`),
  `UpdateCounts(ctx,id,counts)` přepíše tally, `Complete(ctx,id,watermark,counts)` uzavře jako
  `done` se stampnutým `finished_at`+watermarkem, `Fail(ctx,id,lastErr,counts)` jako `failed`
  **bez** watermarku (oba matchují jen běžící běh → `ErrRunNotFound` na dvojí uzavření),
  `Get(ctx,id)`, `LatestWatermark(ctx,source)` → `(time.Time, found bool, err)` watermark
  **posledního úspěšného** běhu zdroje pro navázání dalšího inkrementu — ignoruje běžící/failed
  běhy i done bez watermarku, každý zdroj má vlastní kurzor, `LatestRun(ctx,source)` →
  `(Run, found bool, err)` **nejnovější běh zdroje bez ohledu na stav** (running/done/failed —
  na rozdíl od `LatestWatermark` nefiltruje status; podklad system-status dashboardu),
  `List(ctx,limit,offset)` stránka běhů
  **přes všechny zdroje** newest-started-first (limit clamp `[1,200]`, default 50, non-nil prázdná
  stránka) — podklad admin historie importů; sentinely
  `ErrRunNotFound`/`ErrInvalidSource`), `internal/photoprism/`
  (read-only HTTP klient k běžící instanci PhotoPrismu — podklad inkrementálního importu, vše za
  rozhraním `Client` (fakeovatelné): `New(Config{BaseURL,Token,Timeout,MaxRetries,RetryBaseDelay,
  RetryMaxDelay,HTTPClient})` → `*HTTPClient`, `ErrInvalidURL` na nevalidní base URL; **autentizace**
  dlouhožijícím app password/access tokenem v hlavičce `Authorization: Bearer` na **každém**
  requestu (ne per-request login); `ListPhotos(ctx,PhotoListParams{Count,Offset,UpdatedSince,Order,
  AlbumUID,Query})`
  → `GET /api/v1/photos?count=…&offset=…&merged=true&order=updated[&q=updated:"<RFC3339>"]`
  pro **inkrementální** pull (UpdatedSince→filtr `updated:`, count ořez na `MaxCount` 1000, caller
  pageuje přes offset); **scope pro mapování členství**: `AlbumUID`→`s=<albumUID>` (fotky alba),
  `Query`→`q=` natvrdo (přebije watermark, pro `label:"<slug>"`); parsuje
  UID/TakenAt/Lat/Lng/Altitude/Title/**Caption**/Type/Width/Height/
  OriginalName/**Scan**/**CameraSerial**/Camera/Lens/EXIF + `Files[]` (UID, **Hash=SHA1**, Primary,
  Mime, `Video`/`Codec`/**`ColorProfile`**/**`Projection`**, `Markers[]`),
  `Photo.PrimaryFile()` vrátí primární soubor, `File.IsVideo()` (Video flag/`video/*` mime),
  `Photo.VideoFile()` (motion soubor video/live fotky) a `Photo.StillFile()` (still fotky);
  **`Caption` je živé pole, `Description` mrtvé** — PP přejmenoval `photo_description` na
  `photo_caption` (`description_src`→`caption_src`) a starý Go field má `gorm:"-"`, takže se
  **neperzistuje a vždy přijde prázdný**; obojí je namodelováno (Caption = co odpoví dnešní instance,
  Description = co stará) a importér bere první neprázdné;
  `ListAlbums`/`ListLabels`/`ListSubjects(ctx,ListParams
  {Count,Offset,Type})` → `GET /api/v1/{albums,labels,subjects}`, markery z `Files[].Markers[]`;
  **`GetPhoto(ctx,uid)`** → `GET /api/v1/photos/{uid}` vrací `PhotoDetail` = `Photo` +
  **`Details`** (`{Keywords,Notes,Subject,Artist,Copyright,License,Software}` — IPTC/XMP kredity,
  které PP drží ve vedlejší tabulce; fotka indexovaná starou verzí nemá `photo_details` řádek vůbec →
  přijde `null` → zero value) + **`Albums[]`** (všechna alba fotky, libovolného typu) + **`Labels[]`**
  (`PhotoLabel{LabelSrc,Uncertainty,Label}`). **Výpis fotek (`?merged=true`) je plochá search
  struktura a nenese z toho NIC**: žádný `Details` objekt (tedy ani Subject/Artist/Copyright/License/
  Keywords/Notes/Software), žádný `CameraSerial`, `Files[].Markers` **vždy prázdné** a
  `Files[].Codec`/`ColorProfile`/`Projection` taky (`Caption`, `Scan` a `OriginalName` naopak
  **ve výpisu jsou**). Stojí 1 request na fotku, takže ho volá **scoped import** pro každou fotku a
  plný import jen pro fotky, které **zapisuje** nebo se kterými zdroj **hnul** po watermarku
  (`ppimport.importPhotoDetail`); prázdné uid → `ErrBadResponse`, neznámé → `ErrNotFound`;
  **`Type` je u alb povinný** — `/api/v1/albums` bez typu (i s víc typy naráz, `album,folder`) vrací
  **400 „Permission denied"**, takže katalog alb se prochází typ po typu (`AlbumTypes` =
  album/folder/moment/state/month); štítky a subjekty typ neberou a ignorují ho;
  `DownloadOriginal(ctx,fileHash)` → `GET /api/v1/dl/{hash}?t=<download_token>` **streamuje** originál
  (`Download{Body,ContentType,ContentLength}`, tělo vlastní caller; nikdy celý v RAM přes
  `cancelReadCloser`), **download token** z create-session `POST /api/v1/session`
  (`config.downloadToken`) thread-safe cachovaný, **rotuje** → přebírá se z hlavičky
  `X-Download-Token`, na 401/403 jednou obnoví session a zopakuje; **robustnost** 429 →
  exponenciální backoff ctící `Retry-After`, JSON endpointy vyžadují `Content-Type:
  application/json`; typové chyby `ErrInvalidURL`/`ErrUnauthorized`/`ErrNotFound`/`ErrRateLimited`/
  `ErrUpstream`/`ErrUnavailable`/`ErrBadResponse` nikdy nenesou token ani tělo odpovědi; konfig
  `import.photoprism.{base_url,token,page_size}`; klient staví importér (`ppimport`)),
  `internal/ppimport/`
  (read-only, **inkrementální a idempotentní** import z PhotoPrismu — vše za rozhraními
  `PhotoPrismClient`/`RunStore`/`PhotoStore`/`Storage`/`Thumbnailer`/`AlbumStore`/`LabelStore`/
  `PeopleStore`/`Enqueuer`/`VideoProber` → unit-testovatelné s faky; `Service` = `New(Config{Client,Runs,Photos,
  Storage,Thumbnailer,Albums,Labels,People,Enqueuer,Prober,PageSize,TempDir,MaxFileSize,Logger})`
  (`Prober` volitelný — nil → `defaultProber` nad `video.Probe`);
  **`Import(ctx) (Result,error)`** otevře `import_runs` běh, navrhne na poslední úspěšný watermark a:
  (1) pageuje `ListPhotos(UpdatedSince=watermark)` — per fotka dedup dle `photoprism_uid` (už
  importovaná → `UpdateMetadata` jen při změně, jinak skip), jinak **vybere média** (`selectMedia`,
  `video.go`): PP `Type` video/animated → **stáhne samotný video soubor** (`Photo.VideoFile()`,
  media_type `video`, video soubor bez streamu graceful → image), live → **still jako primární
  originál + motion klip jako sidecar** (`Photo.StillFile()`+`VideoFile()`, media_type `live`),
  jinak image; **stáhne** vybraný originál do
  tempu + **SHA256**, dedup dle `file_hash` (shodný obsah → backfill ID přes
  `photos.SetPhotoprismRef`, žádná nová fotka), uloží originál, **probne video metadata**
  (`Prober.Probe` → `duration_ms`/`video_codec`/`audio_codec`/`has_audio`/`fps`; u video z originálu,
  u live z motion klipu; best-effort, selhání → nulová pole), `photos.Create` s **PP metadaty**
  (title/**caption**/taken_at/GPS/camera/EXIF) + media_type + video metadata + `photoprism_uid`/`photoprism_file_hash` + **EXIF orientace
  ze souboru** (PP ji nevystavuje — `exif.Extract` doplní geometrii/orientaci/MIME, PP přebije
  kurátorská pole), **u live** stáhne+uloží motion klip jako `RoleSidecar` photo_file (best-effort),
  náhledy (u videa **poster frame** přes thumbnailer/ffmpeg) a **enqueue `image_embed`** (na posteru)
  **+`face_detect`**; counts **checkpoint po každé
  stránce** přes `UpdateCounts`. **Popisek se bere z `Caption`, ne z `Description`** (`metadata.go`,
  `caption()`): `photo_description` je v PP mrtvý sloupec (přejmenovaný na `photo_caption`, Go field
  `gorm:"-"`), takže čtení `Description` tiše zahazovalo **každý** popisek v knihovně. Precedence
  update patche: **PP vyhraje, když má hodnotu, ale prázdná PP hodnota nikdy nesmaže neprázdnou
  Kukátkovou** (`UpdateMetadata` přepisuje celý řádek, takže titulek smazaný ve zdroji by jinak
  zničil ten, co napsal uživatel); `notes`/`ai_note` a IPTC kredity se patchem **protahují beze
  změny** (mapují se z detailu, ne z výpisu), stejně tak `taken_at_estimated`/`taken_at_note` —
  PhotoPrism přibližné datum vůbec nezná, je to Kukátkovo pole a inkrement ho nesmí přepsat. **`Favorite` se záměrně NEMAPUJE**: Kukátkovy oblíbené
  jsou **per-user** a import běžící jako job (nebo z CLI) nemá uživatele, komu ji připsat — a
  `psimport` to nepřekládá taky (jeho `Favorite` je subjektův, ne fotčin);
  (2) **detail fotky** (`details.go`, `importPhotoDetail`) — **POZOR: půlku toho, co PP o fotce ví,
  servíruje JEN detail endpoint**, výpis je plochá search struktura bez `Details` objektu a s
  **vždy prázdnými** `Files[].Markers` (na tomhle import dřív tiše nepřivezl nikoho). Z **jednoho**
  `GetPhoto` se proto veze všechno najednou: **IPTC/XMP kredity** (`Details.Subject`/`Artist`/
  `Copyright`/`License`/`Keywords`/`Software` + `Details.Notes` **jen do prázdna**), **file-technical**
  (`Scan`, `CameraSerial`, `OriginalName`, primární `Files[].ColorProfile`/`Projection` a
  `Files[].Codec` → `image_codec` **jen u stillů** — `video_codec`/`audio_codec` zůstávají ffprobu),
  **markery** i (ve scoped běhu) alba a štítky. Mapuje `importMetadata` → `photos.ApplyImportMetadata`
  (zdroj svá pole vlastní, ale prázdná hodnota nikdy nemaže; keywords se přeženou přes
  `exif.NormalizeKeywords` a codec přes `exif.CodecToken`, aby importovaná fotka měla sloupce ve
  **stejném slovníku** jako extrahovaná). Když detail něco přinesl, **skip se povýší na update**.
  **Kdo detail dostane** (`wantsDetail`) je nákladová hranice importu: **scoped** běh každá fotka
  (řez knihovny, 17 fotek = 17 requestů), **plný** běh jen fotka, kterou právě **zapsal**, nebo se
  kterou zdroj **hnul po watermarku** (`UpdatedAt.After(since)` — editovaný copyright hne fotčiným
  `UpdatedAt`, ale ve výpisu nezmění nic, takže běh, který se dívá jen na verdikt výpisu, by ho
  **nikdy neuviděl**); rozhodně **ne** na každou vylistovanou fotku (inkrementální výpis servíruje
  fotky watermarku pokaždé znovu a při prvním průchodu celou 20tis. knihovnu). Chyba detailu se
  **jen zaloguje** (fotka zůstane importovaná, re-run to opraví). Pojmenovaný validní face marker →
  find-or-create subjekt dle `Slugify` + přiřazený
  marker, který si **ponechá PhotoPrism UID** → import je idempotentní (`GetMarkerByUID` → skip) a
  identita markerů sedí s `psimport` (photo-sorterovy face řádky odkazují právě na tyhle UID, protože
  jeho markery JSOU PhotoPrismovy). **Nově zakládaný subjekt se obohatí o `type`/`favorite`/`private`
  z PP subjektu** (`loadSubjectIndex` přečte `ListSubjects` jednou za běh — best-effort, selhání jen
  neobohatí — a marker svůj subjekt najde přes `Marker.SubjUID`, fallback slug jména; `newSubject` +
  `mapSubjectType`). Obohacení **jen při založení**: existující (třeba editovaný) subjekt zůstane beze
  změny, takže re-run nepřepíše lokální úpravu (symetrie s `psimport`). Obličeje si k markerům dopáruje
  `facematch` přes IoU;
  (3) **alba & štítky** find-or-create dle názvu (mapa z
  `ListAlbums`/`ListLabels`), členství přes scopnutý `ListPhotos` (`AlbumUID`/`label:"<slug>"`) →
  idempotentní `AddPhoto`/`AttachLabel`; pak běh `Complete` s watermarkem; **per-fotka chyba** se
  zaznamená do `counts.failed` a **nepřeruší běh** (jen infrastrukturní chyba běh `Fail`ne), 429
  backoff řeší klient, **watermark se nikdy neposune za nejstarší selhání** (`runState`); bezpečné
  re-runovat. **`Handle(ctx,job)`** = `worker.HandlerFunc` pro `pp_import` (ignoruje payload, volá
  `Import`), `JobPayload()` nese pevný sentinel `photo_uid` → dedup fronty pustí jen jeden import.
  **`ImportScoped(ctx, Scope{AlbumUID,Label,Person,Year})`** = scoped (částečný) běh (CLI
  `--album`/`--label`/`--person`/`--year`, `scope.go`): `Scope.Query()` složí `q=` výraz —
  `label:"<slug>"`, `person:"<jméno>"`, `year:<YYYY>`, termy oddělené mezerou (zdroj je ANDuje,
  hodnoty v uvozovkách kvůli mezerám ve jméně), album jde zvlášť jako `s=` → flagy se **kombinují a
  běh zužují**; pageuje `ListPhotos(AlbumUID=…, Query=…)` **bez** watermarku (řez se natáhne celý bez
  ohledu na stáří fotek — `q=` má u klienta přednost před filtrem watermarku). Nejdřív **ověří scope**
  (`validateScope`, `organize.go`: album uid hledá napříč `photoprism.AlbumTypes` → neznámé
  `ErrAlbumNotFound`, slug štítku v katalogu štítků → neznámý `ErrLabelNotFound`; kontroluje se **před**
  stahováním, aby překlep nevypadal jako čistý běh) a pak **každá fotka přinese svůj celý kontext**
  (`context.go`, `mapPhotoContext` nad detailem, který `importPhotoDetail` už stáhl → **všechna** alba
  fotky (find-or-create dle
  názvu → `AddPhoto`) i **všechny** její štítky (find-or-create dle jména → `AttachLabel` se `source`
  a `uncertainty` ze zdroje: `manual`→`manual`, `image`→`ai`, ostatní (batch/keyword/location/…)
  →`import`) — **i ta alba a štítky, které scope nejmenoval**, takže fotka ze tří alb importovaná přes
  scope na jedno album skončí ve všech třech. Indexy alb/štítků se čtou 1× na běh (`photoContext`),
  mapuje se po **každém** úspěšném outcome (i skip — fotka nezměněná nebo deduplikovaná dle obsahu do
  svých alb patří taky), vše je idempotentní (find-or-create + `AddPhoto`/`AttachLabel`), chyba detailu
  se **jen zaloguje** (fotka zůstane importovaná, kontext doplní re-run). Stojí to **1 request na
  fotku** — proto to plný běh nedělá (20 tis. fotek = 20 tis. requestů) a strukturu mapuje průchodem
  katalogu alb/štítků (`mapAlbums`/`mapLabels`); kredity, lidi a file-technical pole veze týž detail
  (viz výš) → **scoped re-run je i cesta, jak knihovnu naimportovanou dřív dotáhnout na paritu**,
  bez stažení jediného bajtu.
  Uzavře se **`Complete` s `nil` watermarkem** — scoped běh vidí jen řez knihovny, takže zapsat jeho
  nejnovější timestamp jako kurzor by přiměl další plný import přeskočit všechno starší. Prázdný scope
  → `ErrEmptyScope` (na plný běh je `Import`), rok mimo 1826–9999 → `ErrInvalidYear`.
  Alba se listují **po typech** (`Config.AlbumTypes`, default `DefaultAlbumTypes` = album/folder/moment/state,
  bez `month` = 560 automatických kalendářních alb) — zdroj vyžaduje právě jeden typ na dotaz),
  `internal/photosorter/`
  (read-only klient k PostgreSQL DB **photo-sorteru** — datový zdroj přímé migrace (ARCHITECTURE.md
  §10), vše za `*Reader`: `New(ctx, Config{DSN,Schema,MaxConns})` otevře **vlastní** pgx pool
  (oddělený od Kukátko) s pgvector typy registrovanými na každém spojení, volitelný `Schema` scopne
  každý dotaz přes `search_path` (integrační test čte fake schéma vedle Kukátko tabulek); `Close()`
  uvolní pool; `ErrInvalidDSN`. Čte **jen** tabulky migrace — `ListPhotos(PhotoListParams{UpdatedSince,
  Limit,Offset})` (řazení `updated_at, uid`, `updated_at > $1` pro resume), `ListSubjects`/`ListAlbums`/
  `ListLabels(ListParams)`, `Embedding`/`Faces`/`FacesProcessed`/`Phash`/`Edit`/`Markers`/
  `AlbumMemberships`/`LabelMemberships(photoUID)` — embeddingy scanují do `[]float32` (pgvector),
  bbox do `[4]float64`; **fotoknihu ani share-linky nikdy nečte**), `internal/psimport/`
  (read-only, **inkrementální a idempotentní** přímá migrace z photo-sorteru — vše za rozhraními
  `Source`/`RunStore`/`PhotoStore`/`VectorStore`/`PeopleStore`/`AlbumStore`/`LabelStore`/`Storage`/
  `Thumbnailer`/`Enqueuer` → unit-testovatelné s faky; `Service` = `New(Config{Source,Runs,Photos,
  Vectors,People,Albums,Labels,Storage,Thumbnailer,Enqueuer,OpenOriginal,PageSize,Logger})` (panika
  na nil collaborator); **`Migrate(ctx) (Result,error)`** otevře `import_runs` běh (`source=photosorter`),
  navrhne na poslední úspěšný watermark: (1) **buildMappings** find-or-create Kukátko subjekt (slug
  z jména) / album (title) / štítek (jméno) pro každý photo-sorter → ps-uid→kk-uid mapy (generický
  `mapCatalogue`); (2) pageuje `ListPhotos(UpdatedSince=watermark)` — per fotka match dle
  `photosorter_uid` (skip), jinak dle **`file_hash`** (backfill `photos.SetPhotosorterRef`, žádné
  kopírování), jinak **zkopíruje originál** z `file_path` (SHA256, náhledy) a `photos.Create` s PS
  metadaty (vč. IPTC kreditů `artist`/`copyright`/`license`/`software`, `keywords` normalizovaných
  přes `exif.NormalizeKeywords`, `scan` a `panorama`→`projection` přes `panoramaProjection`) +
  `photosorter_uid`; (3) **satelity** — embedding (768) a faces (512 + bbox + det_score +
  cache) vloží **1:1** přes `vectors.SaveEmbedding`/`RecordFaceDetection` (zachová model/pretrained,
  remapuje subjekt, zachová marker_uid), fotka **bez** PS embeddingu/detekce dostane Kukátko
  `image_embed`/`face_detect` job; markery (pod původním UID), album/label členství, phash a edit
  best-effort idempotentně; counts **checkpoint po stránce**; pak `Complete` s watermarkem.
  **Per-fotka chyba** → `counts.failed`, **neabortuje běh** (jen infra chyba `Fail`ne); **watermark
  se nikdy neposune za nejstarší selhání** (`runState`); bezpečné re-runovat. **`Handle(ctx,job)`** =
  `worker.HandlerFunc` pro `ps_migrate` (ignoruje payload, volá `Migrate`), `JobPayload()` nese pevný
  sentinel → dedup fronty pustí jen jednu migraci), `internal/importapi/`
  (maintainer-only HTTP API importů za `RequireMaintainer`: rozhraní `Queue` (Enqueue, splňuje `*jobs.Store`) a `RunLister`
  (List, splňuje `*importer.Store`); `NewAPI(Config{Queue,Runs,RequireMaintainer,EnablePhotoPrism,
  EnablePhotoSorter})`+`RegisterRoutes` mountuje **vždy** `GET /import/runs` (historie + `sources`
  flagy jaké zdroje jsou nakonfigurované) a — **jen pro nakonfigurované zdroje** —
  `POST /import/photoprism` → `pp_import` a `POST /import/photosorter` → `ps_migrate` job (sdílený
  `enqueue` helper, 202 `{job_id,status}`); `jobs.ErrDuplicate` → 409 (už běží), jiná chyba → 500;
  `GET /import/runs` (`parsePaging` limit≤200/offset, neplatný → 400) vrací
  `{runs,limit,offset,sources:{photoprism,photosorter}}` (stránka `import_runs` newest-started-first
  přes `importer.Store.List`); celá API se v `serve` mountuje vždy (`buildImportAPI` v
  `cmd/kukatko/import.go`), aby historie fungovala i bez zdroje; triggery neběží inline — patří na
  background worker), `internal/backup/`
  (v procesu, plánovaná **S3 záloha** databáze a originálů do **druhého, nezávislého bucketu**, vše
  za rozhraními `ObjectStore`/`Dumper`/`OriginalSource` → unit-testovatelné s faky bez S3/DB/FS;
  `Service` =
  `New(Config{Objects,Originals,Dumper,Retention,Logger})` (panika na nil Objects/Originals/Dumper);
  **`Run(ctx,ts)`** dělá tři věci v pořadí: (1) **dump DB** přes `Dumper` streamovaný na S3 jako
  `db/kukatko-<ts>.dump` (`objectSize=-1`, nikdy celý v RAM; ts dodá plánovač/příkaz), (2)
  **inkrementální sync originálů** (`SyncOriginals` — skip dle klíče+velikosti přes `ObjectStore.Stat`,
  klíč = relativní cesta originálu; **čistě aditivní**, smazání ve zdroji se nepropaguje), (3)
  **retence** (`PruneDumps` — prořeže staré dumpy na posledních
  `Retention`, `≤0` = nechat vše; **jen prefix `db/`, nikdy originály**); **dump je povinný** — selhání
  abortuje běh **před** prořezáním, takže neúspěšná záloha nemůže smazat poslední dobré dumpy;
  `Run` serializuje souběžné běhy (`ErrAlreadyRunning`), `Trigger(ctx,ts)` spustí běh na pozadí
  (detached ctx, pro HTTP handler), `Status()` = stav + poslední běh; **`RunSchedule(ctx,spec)`**
  plánovač přes `ParseSchedule` (standardní 5-pole cron / `@daily`/`@every` deskriptory přes
  `robfig/cron`; prázdný → `ErrNoSchedule`, neplatný → `ErrInvalidSchedule` → plánované zálohy
  vypnuté, manuální fungují) s vlastní ctx-aware smyčkou; **`s3Store`** (`NewS3Store(S3Options)`) =
  minio-go/v7 adaptér, **path-style** (`BucketLookupPath`), `parseEndpoint` (scheme→TLS, bare host =
  TLS), sentinely `ErrNotConfigured`/`ErrInvalidEndpoint`, `isNotFound` (404/NoSuchKey) → Stat
  ok=false / Remove idempotentní, **`CopyFrom(srcBucket,srcKey,key)`** = **server-side copy** přes
  `ComposeObject` (jeden zdroj → degraduje na prostý `CopyObject`, nad 5 GiB sáhne po multipart
  copy) — bajty **neprojdou procesem**; request jde na *tenhle* endpoint, takže jeho credentials
  musí umět **číst `srcBucket`**; **`pgDumper`** (`NewPgDumper(dsn)`) = shell-out `pg_dump
  --format=custom --no-owner --no-privileges`, **DSN přes env `PGDATABASE`** (ne argument, aby heslo
  nebylo v `ps`), `Dump` vrací reader (Close čeká na proces + surfacuje stderr), `PgDumpAvailable`,
  `ErrPgDumpMissing`;
  **zdroj originálů** = `OriginalSource` (`List` + `CopyTo(ctx,dst,original)`; `CopyTo` si sám volí,
  jak bajty přenese) a vybírá ho `storage.backend` v `cmd/kukatko/backup.go` (`buildBackupOriginals`):
  **`DiskOriginals`** (`NewDiskOriginals(root)`, backend `fs`) = walk úložiště (skip `.tmp`,
  confine klíče proti traversalu), `CopyTo` streamuje soubor nahoru přes `Put` — **slouží i obnově**
  přes `Stat(key)` (existuje + velikost, pro skip-existing) a `Write(key,r)` (atomický zápis do
  `.tmp` + rename → resumovatelné);
  **`BucketOriginals`** (`bucket.go`, `NewBucketOriginals(source,bucket)`, backend `r2`) = `List`
  vylistuje primární bucket (skip prefixů `db/` a `.tmp/` — dump ani rozdělaný upload není originál),
  `CopyTo` deleguje na `dst.CopyFrom` → **kopie bucket→bucket server-side**, takže knihovna se nikdy
  netahá na VPS, aby se odtud nahrála zpět; sentinely `ErrNoSourceStore`/`ErrNoSourceBucket`
  (nenakonfigurovaný primár **nesmí** vypadat jako prázdná knihovna) a `errBackupSameBucket` ve
  wiringu (mířit zálohu na primární bucket = nezálohovat nic). **Objektový store nemá verzování**,
  druhý bucket je jediná ochrana proti smazání → originály se **nikdy** neexpirují; klíče ani
  tajemství nikdy nelogovat;
  **OBNOVA / disaster recovery** (`restore.go`, `pgrestore.go` — protějšek zálohy): `ObjectStore`
  rozšířeno o **`Open(ctx,key)`** (stream GET z bucketu, na `s3Store` přes `minio GetObject`); nová
  rozhraní **`Restorer`** (`Restore(ctx,archive io.Reader)`), **`LocalOriginals`** (List/Stat/Write,
  splňuje `DiskOriginals`) a **`PhotoCatalog`** (`CountPhotos`/`ListFilePaths`, splňuje `photos.Store`);
  **`RestoreService`** = `NewRestoreService(RestoreConfig{Objects,Restorer,Originals,Photos,Logger})`
  (panika na nil Objects): **`ListDumps`** (dumpy pod `db/` končící `.dump`, nejnovější první) /
  **`LatestDump`** (`ErrNoDumps`) / **`RestoreDatabase(key)`** (prázdný key → nejnovější; streamuje
  dump z S3 rovnou do `Restorer`; `ErrDumpNotFound` na neznámý key — **destruktivní**) /
  **`RestoreOriginals`** (stáhne z bucketu jen chybějící originály — skip dle klíče+velikosti přes
  `LocalOriginals.Stat`, dumpy pod `db/` přeskočí, atomický `Write` → resumovatelné, ctí ctx cancel,
  `RestoreOriginalsResult{Downloaded,Skipped}`) / **`Verify`** (integritní report `VerifyReport`
  {PhotosInDB,FilesInDB,OriginalsOnDisk,MissingOnDisk,ExtraOnDisk,Consistent} přes čistou `reconcile`
  set-diff `photo_files.file_path` vs disk); **`pgRestorer`** (`NewPgRestorer(dsn)`) = shell-out
  `pg_restore --format=custom --clean --if-exists --no-owner --no-privileges --single-transaction
  --dbname=<db>`, čte archiv **ze stdin** (nikdy celý v RAM), **DSN parsován do PG\* env**
  (`PGHOST`/`PGPORT`/`PGUSER`/**`PGPASSWORD`**/`PGDATABASE` přes `pgx.ParseConfig`) → heslo **nikdy
  v argv**; `PgRestoreAvailable`, sentinely `ErrPgRestoreMissing`/`ErrInvalidDSN`; tajemství nikam
  neprosáknou), `internal/backupapi/`
  (maintainer-only HTTP API nad zálohou: rozhraní `Service` (Status+Trigger, splňuje ho `*backup.Service`,
  fakeovatelné, **nil = nenakonfigurováno**); `NewAPI(Config{Service,RequireMaintainer})`+`RegisterRoutes`
  mountuje `GET /backup` (stav + poslední běh, nil service → `configured:false`) a `POST /backup`
  (spustí `Trigger` na pozadí → 202 `{status:"started"}`, `ErrAlreadyRunning` → 409, nil service →
  503); mountuje se v `serve` vždy (`buildBackupAPI` v `cmd/kukatko/backup.go`)), `internal/restoreapi/`
  (maintainer-only HTTP API nad obnovou, **jen read-only operace**: rozhraní `Service`
  (`ListDumps`+`Verify`, splňuje ho `*backup.RestoreService`, fakeovatelné, **nil = nenakonfigurováno**);
  `NewAPI(Config{Service,RequireMaintainer})`+`RegisterRoutes` mountuje `GET /restore/dumps` (seznam dumpů,
  503 bez konfigurace, 502 při chybě S3) a `POST /restore/verify` (integritní report, 503 bez
  konfigurace); **destruktivní obnova DB se přes HTTP záměrně neexponuje** (podtrhla by tabulky
  běžícímu serveru — patří do CLI při zastaveném serveru); mountuje se v `serve` vždy
  (`buildRestoreAPI` v `cmd/kukatko/restore.go`)), `internal/maintenance/`
  (**integritní kontrola & opravy knihovny** — udržuje velkou dlouhožijící knihovnu konzistentní:
  odhalí drift mezi katalogem a soubory na disku a doplní/přegeneruje odvozená data; zrcadlí
  photo-sorter `cache build-thumbs`, ale je širší a bezpečnější (**nikdy nemaže originály** — to je
  práce koše/purge), idempotentní, opravy přes persistentní frontu jobů; vše za rozhraními
  `PhotoCatalog` (`CountPhotos`/`ListPrimaryFiles`/`ListFilePaths`/`ListPhotosMissingPhash`,
  splňuje `photos.Store`)/`VectorCatalog` (`ListPhotosMissingEmbedding`/`ListPhotosMissingFaces`,
  `vectors.Store`)/`OriginalStore` (`Stat`, `storage.Storage`)/`DiskScanner` (`List`, adaptér nad
  `backup.DiskOriginals`)/`ThumbChecker` (`HasThumbnail`, `NewThumbCache` nad `thumb.Thumbnailer`)/
  `Enqueuer` (`EnqueueThumbnail`, `jobs.Enqueuer`)/`EmbedBackfiller` (`embedjob.Service`)/
  `FaceBackfiller` (`facejob.Service`)/`OrphanImporter` (volitelný, nil vypne orphan import) →
  unit-testovatelné s faky bez DB/disku/fronty; `Service` = `New(Config{...,SampleLimit})`
  (panika na nil povinný kolaborant; default `SampleLimit` 20); **`Scan(ctx)`** (read-only) vrátí
  `Report{Photos,FilesInDB,OriginalsOnDisk,MissingOriginals,OrphanFiles,MissingThumbnails,
  MissingEmbeddings,MissingFaces,MissingPhashes}` — každá třída je `Finding{Count,Samples}`
  (count + omezený vzorek identifikátorů); `representativeThumbSize`=`tile_224` je proxy přítomnosti
  náhledů, orphan = soubor na disku bez `photo_files.file_path` (`orphanKeys` set-diff), `Report.Clean()`;
  **`Repair(ctx,RepairOptions{Thumbnails,Embeddings,Faces,Phashes,ImportOrphans})`** (každá opt-in,
  idempotentní, pevné pořadí) → `RepairResult` se scheduling počty: thumbnails/phashes zařadí
  `thumbnail` joby (`EnqueueThumbnail`), embeddings/faces volají backfill, orphan import jede přes
  upload pipeline (per-orphan selhání se počítá bez abortu); `ErrOrphanImportUnavailable` když je
  import vybrán bez importéru), `internal/thumbjob/`
  (worker handler `thumbnail` jobu — **repair path** pro maintenance: regeneruje z originálu odvozená
  data fotky, **náhledy** (`Thumbnailer.GenerateAll`, skip cachovaných) a **pHash/dHash** (jen když
  chybí, `phash.Compute` nad dekódovaným originálem), vše za rozhraními `PhotoStore`/`Thumbnailer`/
  `Decoder` (`StorageDecoder` = `storage.Materialize`+`imgconvert.EnsureDecodable`, fakeovatelný) →
  unit-testovatelné bez disku; `Service` = `New(Config{Photos,Thumbnailer,Decoder,Lister?,Enqueuer?})`
  (panika na nil povinný kolaborant; `Lister`/`Enqueuer` volitelné — zapnou backfill),
  `Handle`=`worker.HandlerFunc` (payload `{photo_uid}`, prázdný → `ErrMissingPhotoUID` dead-letter),
  `Regenerate(uid)`/`ensurePhash` idempotentní; registrovaný v `serve` na `jobs.TypeThumbnail`.
  **Force path** `ForceRegenerate(uid) ([]string,error)` je on-demand protějšek (podklad servisní
  akce "regenerate thumbnail" v `photoapi`): **přepíše** všechny náhledy (`Thumbnailer.RegenerateAll`,
  atomický overwrite) a **vždy** přepočítá pHash (`recomputePhash`, sdílené s `ensurePhash`), vrací
  seřazené názvy velikostí; chybějící foto → `photos.ErrPhotoNotFound`, chybějící/nedekódovatelný
  originál zabalen do `ErrRegenerateFailed` (HTTP 422). **Backfill** `BackfillThumbnails(ctx,all)
  (int,error)` (podklad `POST /process/thumbnails`): zařadí `thumbnail` job pro každou fotku **bez
  náhledu** = bez pHashe (`PhotoLister.ListPhotosMissingPhash`), nebo — když `all` — pro každou
  nearchivovanou (`ListActiveUIDs`, dožene chybějící velikost i u fotky s pHashem); enqueue přes
  `Enqueuer.EnqueueThumbnail` (dedup no-op → idempotentní), vrací počet; `ErrBackfillUnavailable`
  když `Service` neměl `Lister`/`Enqueuer`),
  `internal/sidecarexport/`
  (**formát** metadatového sidecaru + jeho atomický zápis do storage — YAML soubor na fotku vedle
  originálů, aby šla knihovna obnovit **jen ze storage**: originály + sidecary, bez databáze.
  Neplest s `internal/sidecar`, které čte *cizí* sidecary (Google Takeout `.json`, Apple `.xmp`) při
  importu — tenhle balík jen **zapisuje**, a jen vlastní formát. `Document` = verzované, seskupené
  schéma (`version`/`generated_at`/`identity`/`descriptive`/`temporal`/`spatial`/`technical`/
  `curation`/`edit`), `Version = 1`; `Build(Input) Document` je **čistá funkce** (bez I/O, bez
  hodin — kolaboranty sbírá volající), `Marshal`/`Unmarshal` přidávají/ignorují hlavičkový komentář,
  který vysvětluje, **proč v souboru nejsou embeddingy** (velké, binární, levné přepočítat z
  originálu — od toho jsou backfill joby), aby to nikdo „neopravil“. `KeyFor(fileKey)` = paralelní
  strom `sidecars/<klíč>.yml` (přípona se **přidává**, ne nahrazuje → `IMG_1.jpg` a `IMG_1.png`
  nekolidují; `ErrEmptyKey` na řádek bez cesty). `Writer` = `NewWriter(ObjectStore)` nad úzkým
  rozhraním (`Put`/`Delete`, splňuje ho `storage.Storage`) → funguje **na FS i R2**;
  `Write(ctx,fileKey,doc)` marshaluje do paměti (pár kB), spočítá SHA256 a předá storage přesnou
  velikost+digest, takže **atomicitu** garantuje storage (FS temp+rename, R2 verifikace+smazání při
  neshodě) — polovičatý YAML není horší sidecar, je nečitelný; `Delete` je idempotentní (chybějící
  sidecar není chyba). **Round-trip test** (`TestRoundTrip` + `TestDocument_fixtureIsExhaustive`,
  který reflexí hlídá, že fixture nemá ani jedno nulové pole) je smlouva formátu a podklad budoucího
  `restore --from-sidecars`),
  `internal/sidecarjob/`
  (worker handler `sidecar` jobu + backfill — **plánovaná** polovina `sidecarexport`: ten umí formát
  a zápis, tenhle ví *kdy*. Job zařadí každá mutace metadat/kurátorských dat; handler fotku
  **znovu přečte** a přepíše soubor, takže je **idempotentní a bezstavový** (dvakrát = stejné bajty,
  pozdě = aktuální stav) — proto je per-photo dedup fronty bezpečný **debounce**, ne zahozený update.
  Vše za rozhraními `PhotoStore`/`Organizer`/`PeopleStore`/`PlaceStore`/`UserStore`/`SidecarWriter`/
  `PhotoLister`/`Enqueuer` → unit-testovatelné bez DB i disku; `Service` =
  `New(Config{Photos,Organize,People,Writer,Places?,Users?,Lister?,Enqueuer?,Logger?})` (panika na
  nil povinný kolaborant; `Places`/`Users` volitelné — skupina se vynechá, `Lister`/`Enqueuer`
  zapnou backfill), `Handle` = `worker.HandlerFunc` (payload `{photo_uid}`, prázdný →
  `ErrMissingPhotoUID` dead-letter), registrovaný v `serve` na `jobs.TypeSidecar` (jen když
  `sidecar.enabled`). `Export(uid)` posbírá kurátorská data, zapíše a **až potom** orazítkuje
  `MarkSidecarWritten` — když zápis selže, fotka zůstane pending a backfill ji dožene; **chybějící
  fotka je logovaný skip** (purge mezi enqueue a během je race, který fronta má prohrát elegantně),
  ale **selhání čtení kurátorských dat je chyba** (soubor, co tvrdí „žádná alba“, protože dotaz
  spadl, je horší než žádný — vypadá autoritativně); neresolvnutelný uploader stojí jméno, ne
  sidecar. `Remove(fileKey)` maže sidecar při purge (sidecar, co přežije fotku, je přesně ten
  soubor, ze kterého by obnova vzkřísila smazanou fotku). **Backfill** `BackfillSidecars(ctx,all)
  (int,error)` (podklad `POST /process/sidecars` a `kukatko sidecar backfill`): zařadí job pro každou
  fotku s chybějícím/zastaralým sidecarem (`ListPhotosMissingSidecar`), nebo — když `all` — pro
  každou nearchivovanou (`ListActiveUIDs`, dožene kurátorská data mimo řádek fotky); idempotentní a
  resumable **bez vlastní evidence** (dedup fronty + self-clearing predikát);
  `ErrBackfillUnavailable` bez `Lister`/`Enqueuer`),
  `internal/metajob/`
  (worker handler `metadata` jobu — **znovu přečte originál fotky** a doplní sloupce, jejichž
  autoritou je sám soubor: IPTC/XMP kredit (`subject`/`keywords`/`artist`/`copyright`/`license`)
  a file-technical (`software`/`color_profile`/`image_codec`/`camera_serial`/`projection`/
  `original_name`). Existuje kvůli fotkám, které vznikly **dřív než extrakce**: řádky z PhotoPrism
  importu, z photo-sorter migrace a všechno nahrané předtím, než `internal/exif` tyhle tagy uměl —
  originály pořád leží ve storage, metadata se tedy pořád **dají** přečíst. Vše za rozhraními
  `PhotoStore`/`Extractor`/`PhotoLister`/`Enqueuer` (`StorageExtractor` = `storage.Materialize` +
  `exif.Extract`, funguje pro **lokální FS i R2**, temp kopii vždy uklidí) → unit-testovatelné bez
  disku; `Service` = `New(Config{Photos,Extractor,Lister?,Enqueuer?,Logger?})` (panika na nil povinný
  kolaborant; `Lister`/`Enqueuer` volitelné — zapnou backfill), `Handle` = `worker.HandlerFunc`
  (payload `{photo_uid}`, prázdný → `ErrMissingPhotoUID` dead-letter), registrovaný v `serve` na
  `jobs.TypeMetadata`. **`Reextract(uid)`** je **výhradně gap-filler**: zapisuje jen do sloupců, které
  jsou pořád prázdné (`photos.FillFileMetadata`), takže prázdná extrakce nikdy nepřepíše hodnotu,
  kterou napsal uživatel, a ničeho jiného se nedotkne (titulky, `taken_at`, GPS, hodnocení, alba jsou
  mimo jeho dosah) → **idempotentní**, druhý běh nezmění nic (ani `updated_at`). Video: `image_codec`
  zůstává prázdný (komprese klipu je ffprobe-derived `video_codec`, ten se netýká); `original_name` se
  rekonstruuje z `photo.FileName` (storage drží originál pod jménem, se kterým přišel — blíž se
  katalog nedostane, a stejně se píše jen do prázdného sloupce). **Chybějící originál** (`os.ErrNotExist`
  z `Materialize`/`Extract`) se **zaloguje a přeskočí** (nil error): soubor je pryč, retry nikdy
  neuspěje a dead-letter by jen rozbil celoknihovní běh; ostatní storage/DB chyby se vrací → fronta
  retryuje. **Backfill** `BackfillMetadata(ctx,all) (int,error)` (podklad `POST /process/metadata`):
  zařadí `metadata` job pro každou fotku, jejíž soubor **nikdy nebyl přečten**
  (`PhotoLister.ListPhotosMissingFileMetadata` = `metadata_extracted_at IS NULL`), nebo — když `all` —
  pro každou nearchivovanou (`ListActiveUIDs`, vynucené znovu-přečtení celé knihovny, tak se doženou
  pole, která se nový extraktor naučil číst); enqueue přes `Enqueuer.EnqueueMetadata` (dedup no-op),
  vrací počet; `ErrBackfillUnavailable` když `Service` neměl `Lister`/`Enqueuer`. **Konverguje a je
  resumable**: značka se stampuje ve chvíli, kdy job doběhne, takže přerušený běh naváže přesně tam,
  kde skončil, a druhý běh nad vyčerpanou knihovnou zařadí **nula** jobů — i pro fotku, jejíž soubor
  žádné IPTC tagy nemá („podívali jsme se a nic tam nebylo" je hotová fotka, ne čekající)),
  `internal/maintenanceapi/`
  (maintainer-only HTTP API nad maintenance: rozhraní `Service` (Scan+Repair, splňuje `*maintenance.Service`,
  nil → 503) a `AuditPurger` (`PurgeOlderThan`+`Record`, splňuje `*audit.Store`, nil → 503);
  `NewAPI(Config{Service,Audit,RequireMaintainer})`+`RegisterRoutes` mountuje `/maintenance`:
  `GET /maintenance/scan` (integritní report), `POST /maintenance/repair` (tělo `RepairOptions`,
  `DisallowUnknownFields`, prázdný výběr → 400, `ErrOrphanImportUnavailable` → 503, jinak `RepairResult`)
  a `POST /maintenance/audit/purge` (tělo `{older_than_days}` 1..36500, cutoff = `now − older_than_days`,
  `audit.Store.PurgeOlderThan` → `{deleted,older_than_days,cutoff}`; chybějící/nekladné/přílišné okno
  nebo neznámé pole → 400; **self-audit** `audit.purge` s cutoffem/oknem/počtem přes `Record` — čerstvý
  záznam přežije purge, takže mazání trailu je dohledatelné, actor z `auth.UserFromContext`);
  mountuje se v `serve` (`buildMaintenanceAPI` v `cmd/kukatko/maintenance.go` injektuje `audit.NewStore`,
  service staví `buildMaintenanceAndThumb` sdílený s registrací `thumbnail` handleru v `buildJobs`)),
  `internal/duplicates/`
  (**review surface pro near-duplicate fotky** nad rámec upload-time varování: linkuje fotky dvěma
  signály — pHash Hammingova vzdálenost do `duplicate.phash_max_diff` a embedding cosine vzdálenost
  do `duplicate.embedding_max_dist` — a slévá hrany do souvislých komponent přes union-find
  (`algo.go` disjoint-set + path compression/union by rank); **bez O(n²)**: pHash přes **banded-LSH**
  buckety (`bandCount`=`maxDiff+1` pásem dle pigeonhole garantuje sdílený bucket pro páry do prahu,
  kandidáti se ověří plnou Hammingovou vzdáleností), embeddingy přes HNSW (`vectors.FindDuplicatePairs`).
  Vše za rozhraními `PhotoSource` (`ListByUIDs`)/`PhashSource` (`ListActivePhashes`)/`EmbeddingSource`
  (`FindDuplicatePairs`, nil vypne embedding grouping)/`FeedbackStore` (`DismissedDuplicatePairs`,
  nil nechá všechny hrany) → unit-testovatelné s faky; `Service` =
  `New(Config{Photos,Phashes,Embeddings,Feedback,PhashMaxDiff,EmbeddingMaxDist,Neighbours})` (panika na nil
  Photos/Phashes; `PhashMaxDiff<0` vypne pHash, `EmbeddingMaxDist<=0` vypne embedding);
  **Zamítnuté dvojice** (`feedback.DismissedDuplicatePairs`, „nechat obě" z compare view) se v
  `buildGraph` registrují (`graph.addDismissals`) **po `addPhashes` a před `addEmbedPairs`/`runPhash`**
  a oba linkovací kroky je přeskočí — union-find neumí hranu odebrat, takže se dismissed pár musí
  potlačit ve chvíli, kdy by hrana vznikla, ne rozplétat potom. Důsledek je záměrný: dvoučlenná
  skupina po zamítnutí zmizí (bez hrany jsou to singletony, ty se zahazují), **větší skupina přežije
  na zbývajících hranách** — „A není B" není tvrzení o C. Dvojice s uid, které scan nezná (archivovaná
  /purgnutá fotka), se ignoruje; pár se skenuje na **indexech uzlů**, kdežto `seen` v `unionBucket` na
  **pozicích entries** — jsou to různé prostory klíčů, proto se dismissal hledá přes `entries[i].idx`;
  **`FindGroups(ctx,limit,offset)`** (backing `GET /duplicates`) → `Result{Groups,Total,Limit,Offset,
  NextOffset}`; každá `Group{ID (nejmenší uid),Reason (phash/embedding/both),KeeperUID,Members}`,
  `Member` nese rozměry/velikost/`taken_at`/media_type + `is_keeper` + `phash_distance`/
  `embedding_distance` ke keeperovi; **navržený keeper** = nejvyšší rozlišení → největší soubor →
  nejstarší → nejmenší uid (`selectKeeperIndex`); skupiny řazené largest-first/newest-keeper/id,
  `limit` clamp `[1,100]`; jen čte, **nikdy nemutuje** (řešení jde přes `dupmerge`); archivované
  fotky se nescanují (`ListActivePhashes` filtruje `archived_at IS NULL`)), `internal/dupmerge/`
  (**transakční sloučení near-duplicate skupiny do keepera** — mutační protějšek read-only `duplicates`;
  `Service=NewService(pool)`, `Merge(ctx,Input{KeeperUID,MemberUIDs,ActorUID})→Result{albums_added,
  labels_added,people_added,metadata_filled[],archived,dry_run}` a `Preview` (dry-run, tx rollback).
  V **jedné `pgx.Tx`** (jako `bulk` — audited store metody si otevírají vlastní tx, nejdou složit)
  spočítá `plan`: union alb/štítků/osob z kopií mínus co keeper už má, `pickFill` chybějících skalárů
  (title/description z fotky + per-user favorite/rating/flag actora, **nikdy nepřepíše** existující
  hodnotu), aktivní kopie k archivaci; aplikuje raw SQL (`INSERT … ON CONFLICT DO NOTHING`, osoba =
  box-less `label` marker s vygenerovaným `mk…` uid — nová marker nemá `faces` řádek, cache netřeba),
  archivuje (`archived_at IS NULL` guard) a zapíše `audit.ActionPhotosMerge`. A copy this call actually
  archived also leaves its stack via `photos.LeaveStackTx` in the same tx (skipped for an already-archived
  copy, which left its stack back then), so archiving a copy that happened to be a stack's primary does not
  hide that stack's still-live members. **Prázdný plán = no-op**
  (nezapíše nic → idempotentní re-run na vyřešené skupině); validace `ErrNoKeeper`/`ErrTooFewMembers`/
  `ErrKeeperNotInGroup`/`ErrKeeperNotFound`), `internal/duplicatesapi/`
  (editor/admin HTTP API nad detekcí a řešením duplikátů: rozhraní `Service` (`FindGroups`, splňuje
  `*duplicates.Service`, **nil → 503**) a `MergeService` (`Merge`/`Preview`, splňuje `*dupmerge.Service`,
  **nil → 503**); `NewAPI(Config{Service,Merge,RequireWrite})`+`RegisterRoutes` mountuje `GET /duplicates`
  a `POST /duplicates/merge` za `RequireWrite` (listing: `limit`≤100/`offset`, neplatný → 400, sken selže
  → 500; merge: chybná skupina → 400, neexistující keeper → 404, actor z `auth.UserFromContext`);
  mountuje se v `serve` (`buildDuplicatesAPI` v `cmd/kukatko/duplicates.go`, `Merge` vždy, `Service` při
  `duplicate.enabled=false` nil)),
  `internal/stacks/`
  (**detekce a správa stacků** — seskupení více souborů jednoho snímku (RAW+JPEG, exportovaná úprava,
  kopie) pod jednu viditelnou **primární** fotku, **bez slévání řádků** (protějšek `dupmerge`, který
  slévá skutečné duplicity — členové stacku se drží schválně; viz `docs/ARCHITECTURE.md` §5.1 + migrace
  `0030_photo_stacks.sql`); `Config{Enabled,Rules RuleSet}` + `Store` rozhraní (splňuje `*photos.Store`,
  fake v unit testech: `ListStackCandidates`/`StackInfoByUIDs`/`CreateStack`/`SetStackPrimary`/
  `UnstackMember`/`UnstackAll`); `Service = New(store,cfg)` (panika na nil store):
  **`DetectStacks(ctx) (created,error)`** (backing `POST /process/stacks`) seskupí **dosud nestacknuté
  nearchivované** fotky enabled pravidly — synchronní, inkrementální a **idempotentní**: re-run nad
  usazenou knihovnou nevytvoří nic a nesáhne na existující/ruční stack; no-op (0) když je funkce nebo
  každé pravidlo vyplé; **`StackSelection(ctx,uids)`** ručně seskupí výběr (`photos.ErrStackTooSmall`
  < 2 distinct, `photos.ErrPhotoNotFound` chybí/archivovaná), **`SetPrimary`/`Unstack`/`UnstackWhole`**
  delegují na store; **čistá detekce** (`rules.go`): čtyři nezávisle přepínatelná pravidla
  (`RuleSet{BaseName,SequentialCopy,UniqueID,TimeGPS}`, každé jiná míra falešných shod — špatně
  stacknutá fotka je neviditelná, takže pravidla linkují jen fotky, co **plausibilně jsou** tentýž
  snímek) klíčují kandidáty (`baseNameKey` bare stem / `canonicalNameKey` strhne trailing
  `(2)`/`copy`/`-edited` / `uniqueIDKey` = ImageUniqueID/InstanceID / `timeGPSKey` = vteřina+GPS),
  `Group` je slije **union-findem** (`unionfind.go`) do souvislých komponent ≥ 2, deterministicky pro
  fixní pořadí vstupu; **výběr primárního** (`primary.go` `PickPrimary`): still před videem (live
  pairing ukáže fotku, ne klip), rendrovaný obraz (JPEG/HEIC) před camera RAW (`rawExtensions`), pak
  vyšší rozlišení, pak větší soubor, tie-break menší uid), `internal/system/`
  (agregace provozního stavu instance pro admin **system-status dashboard** — žádná nová data, jen
  sloučení existujících subsystémů; vše za malými rozhraními `DBPinger` (`database.DB`)/
  `EmbeddingHealth` (`embedding.Client.Healthy`)/`JobCounter`
  (`jobs.Store.CountsByState`/`CountsByType`/`CountPending`)/`ImportLister` (`importer.Store.LatestRun`)/
  `BackupReporter` (`backup.Service.Status`, **nil = nenakonfigurováno**)/`MapsReporter`
  (`mapy.Health.Snapshot`, **nil = bez mapy.com klíče**) → unit-testovatelné s faky
  bez DB; `Service` = `New(Config{DB,Embeddings,EmbeddingURL,Jobs,Backup,Maps,Imports,OriginalsPath,
  CachePath,StorageTTL,Clock})`; **`Collect(ctx) (Status,error)`** sbírá `Status{Version,Database,
  Embeddings,Jobs,Backup,Imports,Storage,Maps}`: embeddings online/offline, fronta (by_state/by_type/total/
  dead_letter/pending_embeddings = queued+running `image_embed`/`face_detect`), backup stav+poslední
  výsledek, poslední import per zdroj, úložiště (velikost originálů+cache walkem, volné/celkové místo
  `statfs` přes `golang.org/x/sys/unix`, **memoizováno** `storageCache` na `defaultStorageTTL` 30 s aby
  polling nepřecházel strom), DB reachability (`Ping`, **sanitizovaná** chyba), **maps**
  (`Maps{Configured,State,Degraded,Detail,CheckedAt}` z `mapy.Health` — poslední pozorovaný stav
  proxy, žádný vlastní probe/kredit; `key_rejected` = mapy.com odmítá klíč → `degraded`, vidět
  na dashboardu bez otevření mapy), verze/commit; chyby
  čtení fronty/importů (vyžadují DB) → error (500), nedostupná DB a nečitelné úložiště inline
  best-effort), `internal/systemapi/`
  (maintainer-only HTTP API nad system stavem: rozhraní `StatusCollector` (`Collect`, splňuje
  `*system.Service`, fakeovatelné); `NewAPI(Config{Service,RequireMaintainer})`+`RegisterRoutes` mountuje
  `GET /system/status` za `RequireMaintainer` (snapshot; collect selže → 500); mountuje se vždy
  (`buildSystemAPI` v `cmd/kukatko/system.go`, staví vlastní bezstavový embeddings klient jen pro
  Healthy probe, sdílí pool pro job/import stores, backup služba předaná nil-safe; mountuje se
  v `appendOpsAPIs` vedle backup/restore)), `internal/capabilitiesapi/`
  (all-authenticated HTTP API instančních feature-flagů: rozhraní `Reachability` (`Reachable() bool`,
  splňuje `*reachability.Checker`, fakeovatelné); `NewAPI(Config{Embeddings,RequireAuth})`+
  `RegisterRoutes` mountuje `GET /capabilities` za `RequireAuth` → `{semantic_search:bool}` čtený
  z cache-ovaného flagu (nikdy živý probe, takže levné a smí ho číst každý přihlášený — na rozdíl od
  maintainer-only `/system/status`); tvar záměrně otevřený pro budoucí flagy; mountuje se **vždy**
  (`buildCapabilitiesAPI`+`buildReachabilityChecker` v `cmd/kukatko/capabilities.go`)),
  `internal/query/`
  (čistý **parser vyhledávacího jazyka** `q=` — volný text + `klíč:hodnota` filtry v jednom stringu
  (`dovolená camera:"Canon EOS R6" iso:100-400 faces:2`), žádné I/O: `Parse(input) Query` **nikdy
  neselže** — tokenizer respektuje uvozovky a `\` escapy, operátory `|` (OR mezi alternativami
  hodnoty), `!` (NOT per alternativa), `-` (NOT volného textu), rozsahy `lo-hi` s otevřenými konci
  (`800-`, `-200`), `*` wildcard v textu; registr filtrů `specs` (Key → Kind
  text/number/date/bool/enum/id/count + validace mezí: rating 0–5, month 1–12, year 1000–9999, …)
  s aliasy `subject:`→`person:`, `keyword:`→`keywords:`; **neznámý klíč nebo nevalidní hodnota
  degraduje celý token na volný text** a hlásí se v `Query.Unknown` (UI z toho staví hint,
  API `unknown_tokens`). AST: `Query{Terms,Filters,Unknown}`, `Term{Text,Phrase,Not}`,
  `Filter{Key,Values}`, `Value{Not,Text,Bool,Min,Max,From,Until}` (číselné meze / half-open
  datumové intervaly); renderingy `FreeText()` (websearch syntax pro FTS vč. frází a `-` negací),
  `PlainText()` (pozitivní termy pro ILIKE substring a embedding dotazu), `NotTerms()`,
  `HasFilter(key)`. Do SQL AST kompiluje `internal/photos/store_query.go` (`queryClauses` — mapa
  builderů per klíč, vše přes bind parametry; per-user filtry scopnuté na `RatedBy`, `near:`
  sférická vzdálenost s poloměrem `dist:` default 5 km, `faces:` počítá ne-invalid face markery,
  exact fractional match ±0.005 kvůli float4). Uživatelská gramatika: docs/API.md
  „Vyhledávací jazyk (q=)“), `internal/ratelimit/`
  (znovupoužitelný **per-key token-bucket rate limiter** + HTTP middleware pro náročné endpointy:
  `New(ratePerSec, burst)` → `Allow(key)` (lazy refill, per-klíč bucket) / `Cleanup`/`RunMaintenance`
  (úklid plně doplněných bucketů) / `Middleware` (chi-kompatibilní, keyuje **client IP** přes
  `clientIP` z `RemoteAddr` — chi `RealIP` ji plní z `X-Forwarded-For`/`X-Real-IP`; prázdný bucket →
  **429** + `Retry-After`); `ratePerSec ≤ 0` → **disabled** limiter (Allow vždy true, Middleware
  no-op — endpoint se vypne čistě configem); paměťově omezený opportunistickým úklidem při `maxBuckets`
  (8192), takže nepotřebuje externí goroutinu; mountuje se jako outermost middleware ahead-of-auth na
  `POST /upload` (ingest), `POST /photos/bulk` (bulkapi), `POST /import/*` (importapi) a
  `GET /map/tiles/...` (mapsapi) — limity z `ratelimit.*` configu; login a geocode mají vlastní
  limitery), `internal/obs/`
  (strukturované logování + request-scoped plumbing: slog **JSON** handler na konfigurovatelné
  úrovni (`ParseLevel`/`NewLogger`/`Setup`, `log.level`, neplatná úroveň → chyba při startu),
  **redakční `ReplaceAttr` hook** (`redactAttr`) škrtne hodnotu každého atributu, jehož klíč nese
  tajemství (password/passwd/secret/token/apikey/access_key/secret_key/authorization/cookie/
  credential/dsn) na `[REDACTED]` — i uvnitř skupin, takže secret nikdy neuteče do logu, ani když
  ho někdo omylem zaloguje; **`AccessLog` middleware** vypíše jeden strukturovaný řádek na HTTP
  request (request id z chi `RequestID`, method/path/route pattern/status/duration/bytes/remote IP
  + autentizovaný uživatel, když je znám — auth middleware ho stampne přes `SetUser` do
  request-scoped `fields` bagu sdíleného pointerem přes kontext, protože zápis hluboko v řetězu musí
  vidět top-level logger); level dle statusu (5xx=error, 4xx=warn, jinak info), `/metrics` scrape se
  přeskočí, request id se zrcadlí do hlavičky `X-Request-Id` i sdíleného route labelu metrik),
  `internal/geoestimate/`
  (**odhad chybějící polohy z fotek pořízených blízko v čase** — fotka bez GPS (foťák bez přijímače,
  sken, ořezaný export) byla velmi často pořízená tentýž den na tomtéž místě jako fotky, které
  souřadnice mají; balík stojí na jediném pravidle: **špatná poloha je horší než žádná** — nejen že
  vypadá blbě na detailu, ale tiše otráví mapu, hierarchii míst i každé `near:` hledání nad nimi, a to
  se tváří stejně důvěryhodně jako změřená souřadnice, takže odhadovač **mnohem raději odmítá než
  hádá**; **čisté jádro** (`estimate.go`, bez DB): `Point{Lat,Lng}`, **`Estimate(neighbours,
  radiusMeters) (Point,bool)`** = těžiště sousedů + kontrola, že **každý** z nich je do `radiusMeters`
  od něj (jediný odlehlý bod shodí celou množinu — zamýšlené selhání: cena za odmítnutí je prázdné
  pole, cena za špatný tip je lež, kterou uživatel nemá důvod zpochybnit; **žádné** clusterování ani
  hlasování, protože „většina souhlasí“ je jiné a mnohem slabší tvrzení, než jaké by za ně UI dělalo),
  **`DistanceMeters`** (haversine); množina přes ±180° dostane těžiště uprostřed Pacifiku → nesoudržná
  → nic, což je správná odpověď ze špatného důvodu a nechává se být; **služba** (`service.go`):
  `Config{Store,Enqueuer,Window,RadiusMeters}` (non-positive → `DefaultWindow` 6h /
  `DefaultRadiusMeters` 5000 — 6h drží fotku uvnitř jednoho výletu, ne jednoho kalendářního dne, což je
  přesně ten případ (Brno ráno, Vídeň večer), kdy je same-day odhad špatně), `Store` rozhraní (splňuje
  `*photos.Store`: `ListLocationCandidates`/`ListLocatedNeighbours`/`SetEstimatedLocation`), `Enqueuer`
  (splňuje `*jobs.Enqueuer`, **smí být nil** = odhady se uloží, ale negeokódují);
  **`BackfillLocations(ctx) (estimated,error)`** (backing `POST /process/locations`): pro každého
  kandidáta načte sousedy v okně, zavolá `Estimate`, a při shodě zapíše přes `SetEstimatedLocation`
  (**guarded UPDATE** — když fotka mezitím polohu získala nebo ji uživatel rozhodl, odhad **prohraje**
  a zahodí se) + naplánuje `places` job **až po zápisu**, aby `placesjob` viděl nové souřadnice a
  nepřeskočil geokód jako „už aktuální“; sousedé bez odhadu (`location_source <> 'estimate'`), aby
  jeden tip nepropagoval knihovnou, kde každý skok vypadá stejně sebejistě jako minulý; fotka bez
  sousedů / s nesoudržnými sousedy = `(false, nil)`, **ne chyba** — odmítnutí je normální výsledek;
  idempotentní a resumable **bez kurzoru** (množina kandidátů se prací zmenšuje, takže re-run *je* ten
  resume), wiring `cmd/kukatko/geoestimate.go` (`buildGeoEstimateServiceOrNil` /
  `locationEstimatorOrNil` — nil **interface**, ne typed-nil pointer, aby processapi vrátilo 503)),
  `internal/metrics/`
  (Prometheus instrumentace HTTP serveru, workeru fronty a infra (pgx pool, embeddings sidecar,
  importy, thumbnaily), namespace `kukatko`; **izolovaný `*prometheus.Registry`** místo
  process-global `DefaultRegisterer`, takže testy staví nezávislé metric surface bez cross-test
  leaku; `New()` → `Registry` zaregistruje HTTP (`kukatko_http_requests_total` counter + latency
  histogram + inflight gauge, route label = **chi route pattern**, nikdy raw URL), job lifecycle
  (started/finished counter + duration histogram by type/outcome), embeddings (duration histogram +
  up gauge), import progress (gauge per source/outcome) a thumbnail duration + standardní
  `go_`/`process_` kolektory; **pull-at-scrape kolektory** `RegisterDBPool` (živé pgx pool stats)
  a `RegisterJobQueue` (hloubka fronty by_state/by_type přes `QueueDepthFunc`, `collectTimeout` 5 s,
  aby pomalá DB neblokovala scrape) čtou data ve chvíli scrapu bez extra goroutin; `Handler()`
  mountuje `serve` na `/metrics` (middleware ten path přeskočí, scrape neinstrumentuje sám sebe),
  observační metody `JobStarted`/`JobFinished`/`ObserveEmbeddingCall`/`SetEmbeddingUp`/
  `SetImportProgress`/`ObserveThumbnail` a `Middleware(routeOf)` se předají subsystémům, které
  emitují události; zrcadlí lehký photo-sorter přístup — jeden namespace, omezené label sety;
  tunables v `metrics.*` configu), `internal/web/`
  (SPA fallback handler `web.Handler()`/`SPAHandler` + `internal/web/static` embed
  `//go:embed all:dist/*`; Vite build se zapisuje do `internal/web/static/dist`, ten je
  gitignorovaný kromě committed `.gitkeep`, aby embed kompiloval i bez buildnutého
  frontendu). Detail: [`docs/DEVELOPMENT.md`](DEVELOPMENT.md).

- **Remote CLI klient (`internal/ctl`):** klientská polovina `kukatko ctl` — jediný kus stromu, který
  Kukátko volá **přes HTTP jako cizí server**, ne přes DB a disk. Nemá nic společného s `internal/config`
  (ten popisuje *server* a o vzdáleném endpointu nic neví); jediný stav, který vlastní, je klientský
  soubor `~/.config/kukatko/ctl.yaml`. Motivace: levnější v tokenech než MCP server — žádné tool schema
  se nenačítá do kontextu modelu, jen krátký příkaz a úzký výsledek. Proto je výstup kompaktní.
  - `config.go` — `Context{Name,Server,Token}` + `Config{CurrentContext,Contexts}` ve stylu kubectl.
    `Load(path)` (chybějící soubor = prázdný config, ne chyba — běh jen z env proměnných), `Save(path,cfg)`
    (atomicky: temp 0600 → `Rename` → `Chmod` 0600, adresář 0700; **existující world-readable soubor
    utáhne**, nikdy do něj token nezapíše tak, jak je). `DefaultConfigPath()` ctí `XDG_CONFIG_HOME`.
    `Resolve(cfg, contextName, env)` → `Endpoint`: vybere kontext (jménem → jinak `current-context`),
    pak `KUKATKO_SERVER`/`KUKATKO_TOKEN` **přebijí po jednotlivých polích**, takže samotné
    `KUKATKO_TOKEN` přecredentialuje uložený kontext. Chyby `ErrContextNotFound`, `ErrNoServer`.
  - `client.go` — `NewClient(server, token)` (validuje absolutní http(s) URL → `ErrInvalidServerURL`),
    interní `get(ctx, path, query)` a `send(ctx, method, path, body)` posílají
    `Authorization: Bearer <token>` a vracejí **surové** tělo (`json.RawMessage`), protože `-o json`
    tiskne bajty serveru beze změny; `204 No Content` vrací `nil` tělo. Úspěch je celý rozsah `2xx` —
    API odpovídá 200, 201 i 204 podle endpointu. `401` → `*UnauthorizedError` s krátkou akční hláškou
    (token chybí / expiroval / byl revokován + jak vyrobit nový); `403` → `*ForbiddenError`, který
    **řekne, že nestačí role** (mutace chtějí editor/admin, viewer jen čte), místo výpisu serverového
    `insufficient permissions`. **Nikdy** výpis těla ani tokenu; jiný non-2xx → `*StatusError`
    s `{"error":…}` textem serveru (jinak omezený úryvek těla). Tělo se čte přes `io.LimitReader`,
    timeout 30 s.
  - `photos.go` — `ListPhotos`/`GetPhoto`/`SearchPhotos` + `DecodePhotoPage`/`DecodePhotoDetail`.
    **Dekodér je per-resource záměrně:** API nemá jednotnou list obálku (`photos` vrací
    `{photos,total,limit,offset,next_offset}`, ostatní zdroje holý seznam) a sjednocovat ho nesmíme —
    rozbil by se frontend. `ListOptions` (limit/offset/sort/order/year/album/label/favorite/archived)
    se validuje lokálně (`ErrInvalidPaging`/`ErrInvalidYear`/`ErrInvalidArchived`), takže překlep
    nestojí round trip. **`--year` API nezná** — překládá se na inkluzivní rozsah
    `taken_after`/`taken_before` (`taken_at >= … <= …`), horní mez je poslední instant 31. 12.
    `SearchOptions` přidává `q` + `mode` (`fulltext`/`semantic`/`hybrid`).
  - `albums.go` — `ListAlbums`/`GetAlbum`/`CreateAlbum`/`AddAlbumPhotos`/`RemoveAlbumPhotos`
    + `DecodeAlbums`/`DecodeAlbum`/`DecodePhotoUIDs`. Obálka je **holé `{"albums":[…]}` bez stránkování**
    — proto vlastní dekodér. `PhotoCount` plní jen list; detail ho neposílá, takže ho renderer netiskne.
    `AlbumInput` se validuje lokálně (`ErrEmptyTitle`, `ErrInvalidAlbumType`); membership posílá celý
    seznam uidů v **jednom** požadavku a server vrací obnovené pořadí.
  - `labels.go` — `ListLabels`/`GetLabel`/`CreateLabel`/`AttachLabel`/`DetachLabel` + `DecodeLabels`/
    `DecodeLabel`. Obálka je **holé `{"labels":[…]}`** řazené dle priority (třetí tvar). Attach/detach
    odpovídají `204`. Prázdný `source` se ze těla vypustí, ať server dosadí vlastní `manual`
    (`ErrInvalidLabelSource`, `ErrEmptyName`).
  - `subjects.go` — `ListSubjects`/`GetSubject`/`SubjectPhotos` + `DecodeSubjects`/`DecodeSubject`.
    Obálka je **holé `{"subjects":[…]}`**; galerie subjektu ale **má tvar `/photos`**, takže se čte
    `DecodePhotoPage` (stejný tvar, ne sjednocený). `PageOptions` nabízí jen limit/offset — filtry
    katalogu endpoint nečte, tak je ctl ani nenabízí.
  - `curate.go` — `ListFavorites` (obálka `/photos`, parametr `favorite` se zahazuje: endpoint se
    scopne sám), `AddFavorite`/`RemoveFavorite`, `SetRating`/`ClearRating`. Oblíbené i hodnocení jsou
    **per-user**, takže je smí měnit i viewer. Hvězdy i flag jsou nezávislé ukazatele — co pošleš `nil`,
    to server nechá být (`ErrEmptyRating`, `ErrInvalidRating`, `ErrInvalidFlag`).
  - `bulk.go` — `Bulk(ctx, photoUIDs, ops)` posílá **jeden** `POST /photos/bulk` na celou dávku, protože
    server ji aplikuje v jedné transakci; smyčka po fotkách by atomicitu vyměnila za N transakcí a N
    audit řádků. `BulkOperations` má tagy 1:1 s API (endpoint odmítá neznámá pole) a všechno `omitempty`,
    aby se nulová hodnota neposlala jako reálná změna. `Validate()` zrcadlí serverové kontroly (vzájemně
    se vylučující set/clear páry, rozsah hvězd, flag, souřadnice) → `ErrNoOperations`,
    `ErrConflictingOperations`, `ErrInvalidLocation`. `DecodeBulkResult` čte `{results,counts}` (čtvrtý
    tvar). `ParseLocation("lat,lng")`.
  - `uids.go` — `ParsePhotoUIDs(r)` čte množinu fotek ze stdin ve **čtyřech** tvarech: obálka
    `{"photos":[…]}` (přesně to, co tiskne `ctl photos list -o json`), holé JSON pole uidů, holé pole
    objektů s `uid`, nebo prostý seznam oddělený bílými znaky. `NormalizeUIDs` trimuje, zahazuje prázdné
    a **deduplikuje** (aby počet v potvrzovacím dotazu odpovídal tomu, co se opravdu pošle) →
    `ErrNoPhotoUIDs`. `ConfirmThreshold = 50` je hranice, nad kterou se příkaz ptá.
  - `output.go` — `ParseFormat` (`table`/`json`; **`yaml` schválně ne**), `WriteJSON` (echo bajtů beze
    změny), sdílené `writeTable`/`writeKeyValues`/`writeLine`, `WritePhotoPage` (tabulka + jeden souhrnný
    řádek: kolik z kolika, `offset`, `next offset`, u hledání efektivní `mode` a případné `degraded`),
    `WritePhotoDetail`, `WriteContexts` (**token se nikdy netiskne**, jen `stored`/`not set`).
    Prázdný výsledek = jediný řádek `no photos found`, žádná hlavička — agent si nesplete hlavičku
    s řádkem.
  - `render.go` — `WriteAlbums`/`WriteAlbum`, `WriteLabels`/`WriteLabel`, `WriteSubjects`/`WriteSubject`,
    `WriteMembership` (jeden řádek: kolik fotek album nově drží), `WriteBulkResult` (souhrn + tabulka
    **jen** neúspěšných fotek) a `WriteAck`. `Ack` je jediný payload, který si CLI **vyrábí samo**: kde
    API odpoví `204`, není co propustit beze změny, takže `-o json` dostane
    `{"status":"ok","message":…}` a pipeline pozná úspěch od chyby.

  Strom příkazů, konfigurační soubor a symlink `kukatkoctl` popisuje
  [`docs/OPERATIONS.md`](OPERATIONS.md).
