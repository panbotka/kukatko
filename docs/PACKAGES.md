# Backend packages

A descriptive reference overview of the Go packages. **These are not rules** — the rules live
in [`CLAUDE.md`](../CLAUDE.md). Record a new or changed package here and add one line for it
to `## Package map` in `CLAUDE.md`.

<!-- BODY BEGIN -->
- **Layout:** `cmd/kukatko/` (thin Cobra entrypoint: root + `serve` + `migrate` + `version`),
  `internal/server/` (chi HTTP server, graceful shutdown, `WithTrustedProxies` →
  `clientip.Middleware` in place of chi's `middleware.RealIP`), `internal/version/`
  (ldflags-injectable `Version`/`Commit`), `internal/config/` (typed configuration,
  Viper, `Load()`), `internal/database/` (pgxpool wrapper `DB` with `Ping`/`Close`/`Pool`,
  embedded migration runner `Migrate`, pgvector types registered on every connection,
  **session time zone pinned to UTC** (`pinSessionTimeZone` sets the `timezone` startup runtime
  param, overriding the DSN) so SQL calendar arithmetic (`date_part('year', taken_at)`,
  `make_timestamptz`) shares one reference frame with the UTC boundaries the Go side builds —
  `year:`/`?year=`/histograms and `taken:` then agree on a photo taken around New Year;
  **the `vector` extension is bootstrapped before the pool exists** (`ensureVectorExtension` →
  `ensureExtension`, one plain connection from the same DSN): the pool's `AfterConnect` registers the
  pgvector types, so on a never-migrated database every connection fails with `vector type not found
  in the database` and migration `0001`, which creates the extension, can never run over that pool —
  a fresh install would deadlock on itself. An already-installed extension issues **no**
  `CREATE EXTENSION` at all, only a `pg_extension` read, so a managed/shared instance whose role may
  not create extensions is unaffected;
  SQL migrations in `internal/database/migrations/*.sql`), `internal/database/dbtest/`
  (integration test harness: `dbtest.New(t)`, `dbtest.TruncateAll`), `internal/auth/`
  (authentication/authorization: `Role` viewer/editor/admin/maintainer + `authorize`, bcrypt cost 12
  `HashPassword`/`CheckPassword` — the cost is a **build-tag-selected** identifier
  (`password_cost.go` = production 12, `password_cost_integration.go` = `bcrypt.MinCost` and the
  `KUKATKO_TEST_BCRYPT_COST` override, compiled only under the `integration` tag), so no running
  server can lower it and `TestHashPassword_productionCost` in `make test` fails if the default
  moves; UID/token generators, sliding-window `Limiter`,
  `Store` over pgx, `Service` orchestrating login/session/bootstrap/user management,
  `API` = HTTP handlers + RBAC middleware
  `RequireAuth`/`RequireWrite`/`RequireAdmin`/`RequireMaintainer`/`RequireImport` +
  `RegisterRoutes`; sessions and users in migration `0002_auth.sql`.
  **`users.subject_uid`** (migration `0060_users_subject.sql`) is the person of the library an account
  belongs to: nullable, **not** unique (several accounts may legitimately be the same person), FK
  `users_subject_uid_fkey` → `subjects(uid)` **ON DELETE SET NULL** (deleting a person never deletes an
  account), with a partial index on the linked rows for that cleanup. The constraint is **named
  explicitly** because `kukatko maintenance reset` has to drop and restore it around its truncation — see
  `internal/reset`. It is carried on `auth.User` as `SubjectUID *string` and — unlike `Note` — **is**
  serialised into the login/`/auth/me` payloads, deliberately: the client needs it for the "my photos"
  entry, for `person:me` and for the account's avatar. `Store.SetUserSubject` is the self-service write
  behind `PUT /auth/subject` (outside the maintainer guard: the link carries no permission), while the
  admin create/update paths take it as part of the replaced profile and record it in their audit entries;
  a UID naming no subject surfaces as `ErrSubjectNotFound` (SQLSTATE 23503 → 400).
  **Strict role ladder** viewer < editor < admin < maintainer (migration `0036_role_maintainer.sql`
  redefines the CHECK on `users.role` and drops the `ai` role that `0023_role_ai.sql` had added earlier; `ai`
  accounts are promoted to maintainer): every role inherits the rights of the lower ones. `viewer` only reads; `editor` writes media/metadata;
  `admin` adds management (users, audit, trash); `maintainer` is the top — operations (imports, maintenance, status,
  backup/restore, jobs, processing). Predicates: `CanWrite()` = editor+, `IsAdmin()` = admin **or**
  maintainer (inheritance), `CanMaintain()`/`CanImport()` = maintainer only. Import is therefore an operational action
  for maintainers only (`requireImport`/`RequireImport`). **Only a maintainer** may create/promote to the
  `maintainer` role or modify a maintainer account — otherwise `ErrMaintainerRequired` (403); the actor's role is passed into
  the create/update validation from the context. Bootstrap creates the first user as a **maintainer**.
  **Last-maintainer guard** (`store_maintainer.go`): an instance that has an enabled maintainer must keep
  one, because losing the role entirely is irreversible through the API (granting it is maintainer-only,
  no delete-user endpoint, `Bootstrap` only on an empty table) and would strand every operations surface.
  `withMaintainerGuard` counts enabled maintainers **before and after** the mutation inside the
  mutation's own transaction and returns `ErrLastMaintainer` (→ **409**) when `strandsInstance(before,
  after)` — i.e. `before > 0 && after == 0` — so a refusal rolls the change *and* its audit row back.
  The before-count runs as `SELECT … FOR UPDATE` (in a CTE, ordered by `uid`) so two concurrent
  demotions of two different maintainers queue instead of both seeing the other and committing.
  Counting rather than inspecting the request keeps the guard indifferent to *how* the capability was
  lost: role change, disable, both at once, and any future delete are covered by routing through it.
  `UpdateUserProfile{,Audited}` and `SetUserDisabled{,Audited}` all do — the audited pair nests the
  guard inside `inAuditedTx`, the plain pair gets its own `inGuardedTx`. A **disabled** maintainer does
  not count (`disabled = false` in both queries), and an instance already at zero stays editable
  (`before == 0` ⇒ never stranded), so the guard never freezes a maintainer-less install.
  `Store.CountEnabledMaintainers` is the read-only view of the same invariant.
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
  **Two login budgets** (`handlers_auth.go`, `allowLoginAttempt`): every attempt is charged to the
  per-(username, address) bucket `loginLimitKey` = `username|clientIP(r)` **and** to a second, address-blind
  per-username one (`usernameLimiter`, derived in `usernameLimiterFor` as `usernameLimitFactor` = 3 × the
  per-IP budget over the same window; `APIConfig.UsernameLimiter` overrides it). The address half is only
  worth anything because it comes from `internal/clientip` and not from a header the caller picked; the
  username half is what remains if that ever stops being true, since it cannot be escaped by moving. It is
  the looser of the two on purpose — it is also the one an attacker could aim at somebody else's account to
  lock them out, and a person mistyping their own password meets the per-IP limit long before it. A
  successful login resets both. Both are pruned by `RunMaintenance`.
  **Login takes the same time however it fails** (`login_password.go`, SEC-006): `checkLoginPassword` runs
  **exactly one** bcrypt comparison on every path — against the account's hash when the username resolved,
  against `dummyPasswordHash()` (minted once through `HashPassword`, so at the *same* work factor real
  accounts use; a cheaper stand-in would restore the leak) when it did not — and only then decides. The
  early returns it replaced answered an unknown or disabled account in microseconds against ~250 ms for a
  real one, which told an anonymous caller which usernames exist.
  `TestCheckLoginPassword_timingIsIndistinguishable` compares the fastest of a few runs per branch within a
  factor of two: the failure worth catching is a branch that skips bcrypt entirely, which is three orders of
  magnitude off, not one that is 30 % slower.
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
  (`internal/photoapi` clears it when the flag is dropped);
  **date precision** `TakenAtPrecision string` (JSON/column `taken_at_precision`, the
  `TakenAtPrecision{Day,Month,Year,Decade}` constants + `ValidTakenAtPrecision`, editable → also in
  `MetadataUpdate`): how fine the date was **stated**, a different question from whether it is a guess.
  Anything coarser than `day` means `TakenAt` is the **first instant of that period in UTC**, so the photo
  sorts and filters into it while presentation shows only what was claimed; the store normalises an empty
  value to `day` on write (`takenAtPrecisionOrDay`) rather than tripping the column's CHECK, so a caller
  built by hand cannot invent a grain),
  `MediaType` image/video/live, `FileRole` original/sidecar/edited, UID generator prefix `ph`,
  `Store` over pgx with
  `Create`/`GetByUID`/`GetByFileHash`/`GetByPhotoprismUID`/`GetByPhotoprismFileHash`
  (the identity of a single SOURCE FILE, not of a source photo: a source photo made of several
  files — a RAW next to its JPEG — becomes one row per file grouped in one stack, and only the
  displayable one carries the `photoprism_uid`, so a sibling is found again by its own SHA1 alone;
  partial index from `0045`)/`GetByPhotosorterUID`/`SetPhotoprismRef`
  (backfill `photoprism_uid`+`photoprism_file_hash` onto a photo deduplicated by SHA256, so the row
  still answers to the source uid it came from)/
  `SetPhotoprismFileHash` (the same backfill for a NON-primary source file, **leaving `photoprism_uid`
  alone** — a sibling never claims the source photo's key)/
  **`AddPhotoprismAlias(ctx,ppUID,photoUID,ppFileHash)`**/**`GetByPhotoprismAlias(ctx,ppUID)`**/
  `ListPhotoprismAliases` (the `photoprism_aliases` table from `0046`, `PhotoprismAlias`: `photoprism_uid`
  PRIMARY KEY → `photo_uid` many-to-one `ON DELETE CASCADE`. `photoprism_uid` is a 1:1 column and cannot
  express the source keeping the SAME BYTES as two photos — the second one has no row of its own, because
  `file_hash` is UNIQUE, and the row holding its content already wears the first one's uid. The alias records
  that collapse, so the second source uid still resolves to a row; the write is an idempotent upsert (a re-run
  re-records it, a moved winner re-points it) and the read is the second half of resolving a source uid, after
  `GetByPhotoprismUID`)/`ListByUIDs`
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
  (the write side of an **import from a foreign catalog** (it carried the source system's `Details` block and
  the file-technical fields of a photo detail) onto a photo already in the catalog. Differs from
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
  uploader (`UploadedBy` a user uid, or **`NoUploader`** = `uploaded_by IS NULL`, the imported photos;
  `NoUploader` wins over `UploadedBy` the way `OnlyArchived` wins over `IncludeArchived`)/has-GPS/date-range `taken_after`+`taken_before`/camera/lens/substring search +
  **album/label scope** `AlbumUID`/`LabelUID` via a correlated `EXISTS` over `album_photos`/`photo_labels`
  — the basis of the shared scoped listing of an album's/label's photos through `GET /photos?album=`/`?label=`,
  plus **person/subject scope** `SubjectUIDs` (multi, AND combination: one correlated `EXISTS` over
  `markers` per subject, `invalid = FALSE`) — the basis of `GET /photos?person=` and the person filter facet,
  plus **place scope** `Country`/`City` (exact match via one correlated `EXISTS` over `photo_places`)
  — the basis of `GET /photos?country=&city=`,
  plus **`MatchNone`**, which short-circuits `whereClauses` to a single `FALSE` and so makes every list,
  count and aggregation over those params come back empty. It exists for a scope that is well-formed but
  cannot be satisfied — `person:me` asked by an account with no linked person — where dropping the filter
  would silently widen the query to the whole library and "no photos of you" is the answer the reader
  actually asked for,
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
  the album view, not a public sort alias) **+ `random`** (`SortByRandom`: `ORDER BY md5(uid || Seed)`,
  the pseudo-random order the slideshow's shuffle plays — a function of the photo and `ListParams.Seed`
  alone, so one seed is one permutation on every page and paging through a shuffled show neither
  repeats a photo nor drops one; the direction does not apply), pagination limit/offset; `Count` shares
  the `buildWhere` filters for `total`)/`Search` (Czech-aware fulltext over the generated `fts
  tsvector` column: `ListParams.FullText` via `websearch_to_tsquery('simple',
  immutable_unaccent(q))`, ordered by `ts_rank` (title>description>notes>file_name),
  diacritics-insensitive, honours all List filters + pagination; an empty query →
  `ErrEmptySearch`; `Count` with `FullText` returns the total thanks to the shared `buildWhere`),
  `AggregatePlaces(country)` (place hierarchy `[]CountryPlaces{Country,Count,CoverUID,Cities:[]CityCount}` —
  one `GROUP BY country, city` joining `photos`×`photo_places` over non-archived photos with place
  data, the hierarchy assembled in Go, ordered count desc/name; empty `country`='' = all countries, otherwise
  drill-down into the cities of one country; photos with empty `country` (no-GPS marker) are excluded — the basis of
  `placesapi`. Every level also carries a `CoverUID`, the place's newest visible photo: the same
  `array_agg(… ORDER BY taken_at DESC NULLS LAST, uid)[1]` subscript the album index uses, one more
  aggregate over the pass the count already makes and **never** a correlated `ORDER BY … LIMIT 1`. A country
  takes the newest across all its groups, the unknown-city one included, resolved by the pure `newerCover`
  so the answer does not depend on the order Postgres returned the groups in),
  `TimelineBuckets(params)` (monthly date-histogram `Timeline{Buckets:[]TimelineBucket{Year,Month,
  Count,Cumulative},Total}` — one `GROUP BY` by `date_part(year/month, …)` over non-archived photos,
  **in the grid's own order**: the date it groups on is `timelineDateExpr(params)` (`taken_at`, or the
  album view's `COALESCE(taken_at, created_at)` under `SortByChronology`) and the direction
  `timelineDirection(params)` (`DESC` by default, `ASC` for `OrderAsc`), so `Cumulative` (a running sum
  computed in Go) is the scroll index of the bucket's first image whichever way the grid runs; shares
  `buildWhere` with `List`/`Count`, so the buckets exactly match the list; photos the date expression
  leaves NULL don't fall into buckets (they sort last), but `Total` (via `Count`) includes them — under
  chronology nothing is left out and the counts sum to the total — the basis of `photoapi`'s timeline
  scrubber),
  `YearBuckets(params)` (year-histogram `Years{Years:[]YearBucket{Year,Count},Total}` in
  `store_years.go` — one `GROUP BY date_part('year', taken_at)`, ordered `year DESC`; shares
  `buildWhere` with `List`/`Count`, so a bucket's count = exactly what `List` returns for the same filters
  plus that year; `params.Sort`/`Order`/pagination are ignored, photos without `taken_at` don't fall into
  buckets, but `Total` (via `Count`) includes them — the basis of `photoapi`'s year facet),
  `UploaderBuckets(params)` (who uploaded the matching photos, `[]UploaderBucket{UID,Username,
  DisplayName,Count}` in `store_uploaders.go` — one `GROUP BY uploaded_by` ordered count desc, uid asc,
  the names resolved by a `LEFT JOIN users` **outside** the aggregating subquery, since the shared filters
  name photo columns unqualified and `users` carries columns of those very names; shares `buildWhere` with
  `List`/`Count`, so a bucket's count = exactly what `List` returns for the same filters plus that uploader;
  the photos with **no** uploader are their own bucket (empty `UID`), so the counts add up to `Count`
  — the basis of `photoapi`'s uploader facet),
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
  `?all=true`), the **orientation-geometry repair** (`store_geometry.go`):
  `ListDimensionMismatches()` → `[]DimensionMismatch{UID,StoredWidth,StoredHeight,Orientation,RawWidth,RawHeight}`
  = quarter-turned photos (`file_orientation` 5–8) whose columns hold their own file's dimensions **transposed**,
  i.e. the displayed frame instead of the stored one — the import defect that letterboxed the
  viewer and drifted the face boxes. The raw pair is read out of the `exif` jsonb document
  (`ImageWidth`/`ExifImageWidth`/`PixelXDimension` + the height equivalents, each `jsonb_typeof`-checked so a
  non-numeric value degrades to "unknown" instead of aborting the query with a cast error), so **the comparison
  is the evidence** — a photo whose document says nothing, whose columns already agree, or whose frame is square
  is never reported and no provenance is guessed. It is read-only, hence the **dry run** of
  `RepairDimensions(m)` → `bool` (writes the file's own pair, guarded on the transposed one it replaces, so a
  repeat is a no-op and can never swap a corrected row back), **stack methods** (`store_stacks.go`, see `docs/ARCHITECTURE.md` §5.1):
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
  **Hidden from the library** (`photos.hidden_from_library`, migration `0049`, see `docs/ARCHITECTURE.md`
  §5.1): `SetHiddenFromLibrary(uid,hidden)`/`SetHiddenFromLibraryAudited` toggle a photo out of the
  firehose, and `hiddenClauses` — `stackClauses`' sibling, a bare `photos.*` predicate in
  `whereClauses`, so it reaches `List`, `Count`, `Search`, `FilterUIDs`, `YearBuckets` and
  `TimelineBuckets` at once — emits `NOT hidden_from_library` by default. It **lifts itself** when the
  listing is scoped to an album (`AlbumUIDs`), a label (`LabelUIDs`) or the caller's favourites
  (`FavoriteOf`): a photo filed there was put there deliberately, and that one rule spares every caller
  a flag. `ListParams.IncludeHidden` is the explicit escape hatch, and an explicit `hidden:` in the
  query language yields the default too (`archivedClauses`' precedent — otherwise `hidden:yes` would
  match nothing). It is neither `archived_at` (on its way out, purged after retention) nor `private`
  (a sharing concept, out of scope). The two hand-written library queries repeat the predicate by hand:
  `AggregatePlaces` (`store_places.go`) and `ListLocationCandidates` (`store_location.go`, no geocoder
  credits on a photo no map shows) — `ListLocatedNeighbours` deliberately does **not**, since a hidden
  photo's GPS tag is still a real measurement. Duplicate detection, maintenance, the backfills, backup
  and import verify are all untouched: they are about data integrity, not browsing.
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
  **`KeyLister`** is an **optional** capability alongside `Storage`, satisfied by both backends
  (`FS.Keys` walks the root and skips the `.tmp` staging dir; `R2.Keys` lists the bucket recursively via
  minio-go's own paging and skips directory markers): `Keys(ctx, yield)` streams the key of **everything the
  store holds**, including objects Kukátko never wrote, and returns a `yield` error untouched so a caller can
  stop with its own sentinel. It is kept out of `Storage` on purpose — every consumer of `Storage` addresses
  objects whose key it already has, and each of their test fakes would otherwise grow a method it never calls;
  only the operations that reason about the store *as a whole* (reconciliation, `internal/reset`'s orphan sweep)
  type-assert for it, and report plainly when a store cannot answer.
  **`PrefixLister`** is the narrowed form, also optional and also on both backends
  (`FS.KeysWithPrefix` walks only the directory the prefix names; `R2.KeysWithPrefix` passes the prefix to S3,
  which applies it server-side): `KeysWithPrefix(ctx, prefix, yield)` answers *"what do you already hold under
  here?"* in **one round trip**. The prefix is matched **literally, not as a path component**, so it may end
  mid-filename — which is how `internal/thumb` asks about exactly one photo's eight derived objects
  (`thumb/<aa>/<bb>/<cc>/<hash>_`) instead of paying a `Head` per size. An empty prefix enumerates everything,
  and both `Keys` implementations are now that call with `""`. A prefix nothing matches — or one naming an
  absent directory — yields nothing and is **not** an error.
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
  upload all objects (the original, its metadata sidecar if one exists on disk — `sidecarexport.KeyFor`
  keys it — and the thumbnails already in cache; it generates no new ones) → `Head` reads them back and
  verifies size and SHA256 → `MarkMigrated` commits the row → only then the optional `Delete` of the
  local original **and its sidecar**. The sidecar is the non-regenerable disaster-recovery artifact, so
  it is carried into the store with the original and the original is never deleted until the sidecar is
  durable there; both live under the originals root the move empties, while thumbnails (regenerable,
  in a separate cache) stay. There is no path where the bytes live only where nobody has vouched for
  them. **The cursor** is `photos.storage_migrated_at` (migration `0019`), i.e.
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
  landed **exactly once** and that nobody deleted the original of a photo whose verification failed;
  a second case asserts each photo's sidecar lands in the bucket and its local copy is reclaimed),
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
  `New(store,cacheDir,WithConcurrency(n),WithMaxPixels(px))` with the API `Generate(ctx,photo,sizes...)`/
  `GenerateAll(ctx,photo)` (a size→abs-path map, skips existing)/
  `RegenerateAll(ctx,photo)` (**force** — overwrites all sizes in-place with an atomic
  temp+rename, and republishes to object store; the basis of the "regenerate thumbnail" service action)/
  `Path(hash,size)`/`OpenCached(hash,size)` (the **local cache alone**)/
  `OpenOrGenerate(ctx,photo,size)` (**the backend-independent read** — see below);
  the package-level `RelPath(hash,size)` returns the same cache path relatively — it is also the **object key**
  of the thumbnail in the remote backend, which is why the layout is exported instead of derived a second time elsewhere;
  `CacheSubdir` (`thumb`) exports the top of that layout for the operations that address the **whole prefix**
  rather than one file (`internal/reset` sweeping the cache directory and the bucket prefix);
  **publishing to object store**: after a size is written to cache, `publishSize` uploads it with `Put` under
  `RelPath` to the backend that publishes URLs (`store.URL(rel) != ""`, i.e. R2) — on FS it is a no-op;
  if the upload fails, the local file is deleted, so the size counts as not generated and the next
  `Generate` renders and uploads it again (invariant: a cached size on a publishing
  backend is always in the bucket too, so the client object URL resolves). This way a fresh ingest on R2
  gets its thumbnails into the bucket the same as `storage migrate-to-r2`;
  **the published object also counts as done** (`dropPublished`): a cold local cache used to mean a full
  re-encode + re-upload of objects that were already in the bucket — on a library whose cache is pruned by
  construction (8 sizes ≈ 2.76 MB per photo, more than the free disk) that is the whole library, twice. Before
  encoding anything, `Generate` asks the backend once per photo — `storage.PrefixLister.KeysWithPrefix` over
  `thumb/<aa>/<bb>/<cc>/<hash>_`, which returns exactly that photo's sizes — and drops every size the store
  already holds. **One listing, not one `Head` per size** (measured on the dev MinIO: 2.2 ms for the listing vs
  8.3 ms for 8 `Head`s, against ~4 s of encoding — `docs/PERF.md` §2); a warm cache never lists at all, since
  nothing is missing to ask about. It is gated on `store.URL(rel) != ""` (only there does the object *make* the
  size available; an FS backend serves thumbnails from the cache and must write the file), skipped when the
  backend cannot list by prefix, and a **failed listing falls back to encoding** — being slower is a cost,
  skipping a size that is not really there would leave a thumbnail no client can fetch. `RegenerateAll` (force)
  ignores the check entirely and rebuilds regardless;
  **reading a size back is not the same question as "is it cached"** — the flip side of the above. On a publishing
  backend a size that `dropPublished` skipped leaves no local file at all (nor does one whose cache was pruned),
  so `Generate` returning success promises nothing about the disk. `OpenOrGenerate(ctx,photo,size)` is the read
  that does not care which backend it is on: local cache → the object the store published (`storage.Open` under
  `RelPath`, gated on `URL(rel) != ""`, a missing object mapped onto `ErrNotCached`) → encode. Everything that
  wants the **bytes** goes through it (`internal/embedjob`'s preview, `photoapi`'s thumbnail-route fallback);
  `OpenCached` answers about the local disk alone and is only for callers that really mean the cache. Mistaking
  the one for the other is what dead-lettered **every** `image_embed` job after the move to R2 — the thumbnail
  job published the preview, `Generate` rightly declined to re-encode it, and the handler then failed on the
  cache file that had never existed (spec `task-08a84c07`);
  `GridSize` (`tile_500`) is the size the grid renders and that `thumb_url` carries in the payload,
  `AvatarSize` (`tile_100`) the one a compact row's medallion takes — the command palette's album/label/person
  previews, two dozen of which would otherwise fetch a grid page's worth of bytes;
  decode once per photo, parallel encode of the sizes (errgroup, default `GOMAXPROCS`,
  bound via `thumb.concurrency`);
  **the photo's saved edit is rendered in** (`WithEdits(EditResolver)`, `resolveEdit`): a thumbnailer wired with an
  edit resolver (`*photos.Store`, i.e. `GetEdit`) asks once per generation and applies `photoedit.Apply` after the
  EXIF orientation and before any resize — the same composition as the edited download — so a photo the user turned
  upright is upright in the library grid, in search results and in the map popovers, not only in the viewer. The
  option is **wired at every `thumb.New` site** (`cmd/kukatko/obs.go` `thumbOptions(cfg,reg,db)`), because all of them
  write into the one cache keyed by the *original's* file hash: a site left without it would publish the unedited
  rendering under the key the rest of the process reads. A non-identity edit **skips the vips fast path** —
  `vipsthumbnail` renders the file as it lies on disk and cannot apply the edit. `photos.ErrEditNotFound` (the answer
  for almost every photo) and a photo with no uid mean "unedited"; **any other resolver error fails the generation**,
  since caching the unedited rendering under the edited key is a wrong thumbnail nothing would ever notice. And
  because the key does not depend on the edit, **a changed edit must force a rebuild** — that is what the `thumbnail`
  job's `force` flag and `photoapi`'s enqueue on `PUT …/edit` are for; the pHash deliberately stays the original's
  (see `internal/thumbjob`);
  **decompression-bomb guard**: `WithMaxPixels(px)` (config `thumb.max_pixels`, default 200 MP) makes
  `decodeAndOrient` call `imgconvert.EnforcePixelBound` before the full decode, so a source whose
  `width×height` exceeds the cap fails with `imgconvert.ErrImageTooLarge` instead of allocating a
  multi-GB bitmap; `0` disables it;
  **EXIF orientation** (1–8) automatically; pure-Go JPEG/PNG/WebP + `golang.org/x/image`
  (`draw.CatmullRom` resize); **an optional vips engine** (`WithVips(bin)`, config `thumb.engine:
  vips`, `vips.go`): pure-Go decoding of large JPEGs is slow/memory-heavy on the Pi (~1 s / ~90 MB
  for `fit_720` from 12 MP, ~4 s / ~1.18 GB for `GenerateAll` — see `docs/PERF.md`), `vips` switches
  JPEG/PNG/WebP thumbnails to a **shell-out to `vipsthumbnail`** (`tryVips` → `vipsArgs`: fit `WxH>`
  without upscaling, crop `--smartcrop centre`, `[Q=…,strip]`, EXIF autorotation), **still without CGO**;
  pure-Go remains the default, vips **falls back per-photo** to pure-Go for other formats
  (HEIC/RAW/video) and on any failure → never changes the output, only the speed; every
  `vipsthumbnail` invocation is bounded by a **60s per-invocation timeout** (`runVips` wraps
  `context.WithTimeout`, matching the RAW/poster shell-outs) so a wedged vips fails the job instead of
  hanging a worker forever; `VipsAvailable(bin)`
  for the startup log; `Remove(hash)` deletes all cached sizes for a hash
  (idempotent, skips missing ones — thumbnail cleanup on photo purge); sentinels
  `ErrUnknownSize`/`ErrInvalidHash`/`ErrNotCached`;
  `SizeNames()`/`IsValidSize`), `internal/imgconvert/`
  (**`Orient(img, orientation) image.Image`** (`orient.go`) is the one implementation of the EXIF
  orientation pixel transform in the codebase — 2–4 mirror/flip, 5–8 exchange the sides, `<=1` and `>8`
  are no-ops; `internal/thumb` applies it before rendering and `internal/facejob` before handing an image
  to the sidecar, which reads no EXIF. Two copies of a mapping this fiddly drift, and a box detected in one
  frame then divided by another is exactly how faces end up beside faces.
  HEIC/RAW/video → a decodable JPEG, **shell-out**: `EnsureDecodable(ctx,path)` →
  (path, cleanup, err); **pure-Go passthrough** JPEG/PNG/WebP/**BMP/GIF/TIFF** (animated GIF →
  first frame; the decoders are registered by a blank import in `imgconvert`, `ingest` and `thumb`), **HEIC** via `heif-convert`
  to a temp JPEG, **RAW** (cr2/cr3/nef/nrw/arw/srf/dng/raf/orf/rw2/pef/srw/3fr/iiq/x3f/kdc/mrw/mef)
  pulls the embedded preview via `exiftool -b -PreviewImage` (fallback `-JpgFromRaw`/`-ThumbnailImage`)
  instead of demosaicing, **video** (`FormatVideo`) delegates to `video.ExtractPoster` (poster frame via
  `ffmpeg`) — the thumbnailer and pHash process the poster as a photo; `DetectFormat` prefers **magic
  bytes** whenever they recognize a directly decodable format (JPEG/PNG/WebP/BMP/GIF/TIFF/HEIC) — so a JPEG
  renamed to `.dng`/`.tif` is decoded by content, **not** sent down the RAW branch (where it would have no
  embedded preview); **exception: TIFF magic doesn't carry RAW** — most RAW containers are TIFF-based
  (`II*`/`MM*`), so the RAW **extension** takes precedence over TIFF magic and the file goes through embedded-preview,
  not as a flat TIFF; otherwise RAW is chosen only when magic recognizes nothing (other RAW headers) → falls back to
  the extension; `IsSupportedFormat`; `RAWExtensions()` exports that RAW set (lowercase, no dot, sorted) —
  because an upload is gated on `IsSupportedFormat` it is exactly the set of RAW files that can be in the
  catalogue, which is how `internal/system` splits the library's storage by media type without keeping a second
  list that would drift; `IsRAWName(name)` answers the same question **for a name alone** (a bare file name or a
  path, case-insensitive, opening nothing) — for a caller holding only a catalogue row: `photoapi`'s share manifest
  decides by it whether a photo reaches a phone as its original or as a JPEG preview. It is a weaker witness than
  `DetectFormat` (a JPEG misnamed `.dng` answers true), so use it only where a wrong answer is a worse
  *presentation*, never where it would corrupt a conversion;
  **decompression-bomb guard** `EnforcePixelBound(path,maxPixels)` peeks `image.DecodeConfig` and
  returns `ErrImageTooLarge` when `width×height` exceeds the cap (before the caller's full decode
  allocates the bitmap); `maxPixels<=0` disables it and an unreadable header is left to the caller's
  decode — used by `thumb` and `ingest`; sentinels
  `ErrConverterMissing`/`ErrUnsupportedFormat`/`ErrNoEmbeddedPreview`/`ErrImageTooLarge`; a missing tool = a clear
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
  otherwise `unknown`; **the file-name fallback reads the name, not the path** —
  `ExtractNamed(ctx,path,name)` takes the bytes from `path` and the date from `name`, and `Extract` is
  it with both the same. A caller holding the bytes under a generated name (`ingest` stages every upload
  as `os.CreateTemp` → `kukatko-ingest-<digits>`) **must** pass the real name: eight digits that happen
  to read as `YYYYMMDD` parse as a date, which invented capture times in the year 2879 for roughly one
  upload in thirty. An empty `name` disables the fallback (used by `video.probeWithExiftool`, whose
  caller applies its own with the upload's name); a file without EXIF (PNG) = zero values, **not an error**;
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
  a source system's `jpeg` — → a token for `image_codec`, otherwise empty). The retired importers ran
  every value through these so an imported photo had its columns in the **same vocabulary** as an extracted
  one — a column that after extraction says `jpeg` and after import `JPEG` isn't one column, but two.
  **Orientation geometry** (`geometry.go`): `QuarterTurn(orientation) bool` (5–8 — the only values that
  exchange the sides) and `RawDimensions(w,h,orientation) (int,int)`, which converts an **already oriented**
  („displayed") pair **back** to the file's stored one. It exists because the whole geometry stack —
  `internal/thumb` (which decodes the untouched original and applies the tag itself), the frontend's
  `displayFrame`, `facejob.NormalizeBBox` — reads `photos.file_width`/`file_height` as the bytes on disk with
  `file_orientation` still to be applied. A source catalogue reported the opposite (already-oriented
  dimensions for 5–8), so the retired importers de-oriented on the way in; without it the pair contradicted
  itself and every consumer rotated a second time. The transform is **its own inverse**.
  `FileOrientation(path) int` (`orientation.go`) reads **only** the orientation tag, pure-Go and with no
  shell-out, returning 1–8 or 0 when the file says nothing. It answers the one question a caller has about
  bytes it is about to hand to something that ignores EXIF — does this still need turning? — and it has to be
  asked of those bytes, not of the catalogue: an `imgconvert` intermediate may already carry the rotation in
  its pixels, and applying the original's tag on top would turn the picture twice), `internal/phash/`
  (perceptual hashes, **CGO-free**: `Compute(img) Hashes{Phash,Dhash int64}` — **pHash** via
  a 2-D DCT 32×32 → low-freq 8×8 block with a median-without-DC threshold, **dHash** gradient 9×8; `Distance(a,b)`
  = Hamming distance via `bits.OnesCount64`; near-dup = a small distance), `internal/ingest/`
  (the upload/ingest pipeline: `Service` = `New(Config{Storage,Photos,Thumbnailer,Enqueuer,Duplicate,
  MaxFileSize,MaxPixels,TempDir})` (`MaxPixels` = the same decompression-bomb cap as `thumb.max_pixels`,
  applied to the pHash decode via `imgconvert.EnforcePixelBound`; a rejected oversize source becomes a
  `phash_failed` warning, the photo is still catalogued) with **`IngestFile(ctx,src,Request{Filename,UploadedBy,Sidecar})`** (the full form;
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
  newest/oldest/taken_at/added/title/size**/rating**`/random` + `order` — **an `album` scope pins the sort key**
  to `SortByChronology` and takes only the **direction** from the request (`asc` unless it explicitly
  asked to descend: `sort=newest` or `order=desc`), so an album is always chronological but can be read
  from either end; the defaults of other views are unchanged. **`random` is the one exemption** from that
  pin (a shuffle is an outright request to abandon the order, not a stale sort key) and pairs with
  **`seed`** (free text, ≤ 64 chars, longer → 400) → `ListParams.Seed`; `search.go`'s `shuffled` applies
  the same permutation to the semantic/hybrid uid rankings, which are ordered in Go and would otherwise
  ignore the shuffle entirely),
  `archived` false/true/only,
  `has_gps`, `taken_after`/`taken_before`, `camera`, `lens`, `uploader`, `q`, **`year` (four-digit
  1000–9999) → `Year`**, **`album`/`label`
  scope** → `AlbumUID`/`LabelUID`, **`person` scope (multi, AND)** → `SubjectUIDs`,
  **`country`/`city` place scope** → `Country`/`City`,
  **per-user `min_rating` (int) + `flag` (`pick`/`reject`/`eye`)**
  → `MinRating`/`Flag`; invalid → 400) + `favoriteRequested` parses `favorite=true`
  → the handler sets per-user `FavoriteOf` to the current user; the list/search/favorites handlers
  set `RatedBy` to the current user, so `min_rating`/`flag`/`sort=rating` are scoped to them;
  `personme.go`'s **`applyMeTokens`** then resolves both caller-dependent tokens — `person:me` against the
  caller's linked subject and `uploader:me` against their own account — in one call, made by the list,
  search, favorites, timeline **and** facet handlers, so the tokens mean the same
  thing on the grid and on the aggregations beside it and a new handler cannot half-resolve them. An unlinked caller gets
  `photos.ListParams.MatchNone` (an empty page rather than the whole library) and the notice
  `person_me_unlinked`; `pageHints{unknown,notices}` carries both that and `unknown_tokens` through the
  four write-the-page helpers, so a path cannot stamp one and forget the other;
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
  `GET /photos/uploaders` (`handleUploaders`, `uploaders.go`) = **who uploaded into the current view**, for
  the filter bar's uploader control → `photos.Store.UploaderBuckets` → `{uploaders:[{uid,name,count}]}`,
  largest contribution first; takes the same filters as the list (incl. per-user `FavoriteOf`/`RatedBy` and
  the `q` language), but **zeroes out the uploader filter itself** (`UploadedBy`+`NoUploader`) for the same
  reason years zeroes `Year`; the photos with no uploader are one entry with an empty `uid` **and** an empty
  `name` — naming that group ("imported") is the client's job, only it knows the reader's language; every
  other entry is named by `uploaderName` (display name, falling back to the username), the same rule the
  detail's `uploader` object uses; an invalid param → 400;
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
  "processed" marker (a row with all levels empty); `EditService`/`edit.go`+`media_edit.go`
  (`GET`/`PUT /photos/{uid}/edit`, download honours the edit via `internal/photoedit`; a saved edit also
  **audits** `photo.edit` through the same `AuditRecorder` as the thumbnail action — after the write, because
  `Store.SetEdit` exposes no transaction to join — and **enqueues a forced thumbnail rebuild** through the
  `ThumbnailEnqueuer` interface (`jobs.Enqueuer.EnqueueThumbnailRebuild`, nil-safe), so the grid stops showing
  the previous rendering; both are best-effort, exactly like `enqueueSidecar`, and the **neutral** edit is no
  special case — a reset audits and rebuilds too); and the **comment
  thread** (`comments.go`, over the `CommentStore` interface satisfied by `comments.Store`):
  `GET`/`POST /photos/{uid}/comments` + `PATCH`/`DELETE /photos/{uid}/comments/{commentUID}`, all four behind
  **`RequireAuth`** — the create deliberately so, a viewer may comment, see docs/API.md — with the per-user
  throttle (`Config.CommentRateLimit`, nil = none, resolved by `commentThrottle`) mounted *inside* the auth
  guard; the pure predicates `canEditComment` (author only) / `canDeleteComment` (author **or**
  `Role.IsAdmin()`) hold the authorization decision and are made against the **stored** row —
  `resolveComment` reads it first and answers 404 when it belongs to a different photo than the path names,
  so the route cannot be used to probe which comment UIDs exist; `writeDetail` stamps **`comment_count`** via
  `commentCount`, which asks the bulk `CountsAmong` for the one UID so the detail can never grow an N+1)),
  `internal/comments/`
  (the store behind those endpoints — the `photo_comments` table from migration `0052_photo_comments.sql`:
  `uid PK` (prefix `cm`), `photo_uid` FK **CASCADE** (purging a photo takes its thread with it), `author_uid`
  FK **SET NULL** (a comment outlives its author's account: authorless, and thereafter editable by nobody),
  `body TEXT` with `CHECK (char_length(body) BETWEEN 1 AND 2000)` mirroring `normalizeBody` (trim →
  `ErrEmptyBody`/`ErrBodyTooLong`, counted in **runes**, so a Czech comment gets the same allowance as an
  English one), `created_at`, `edited_at` (NULL until the first edit — that is what lets a client mark a
  comment as edited without comparing timestamps) and `deleted_at` (NULL while live), plus the partial index
  `(photo_uid, created_at) WHERE deleted_at IS NULL` serving both the thread read and its count;
  `NewStore(pool)` → `List(photoUID)` (live only, oldest first, `author_name` resolved by a LEFT JOIN on
  `users`: `display_name`, fallback `username`, empty for a deleted account, so one query renders a thread —
  and `author_photo_uid` by a second LEFT JOIN through `users.subject_uid` onto `subjects.cover_photo_uid`,
  so the thread can draw the author's face where it would otherwise draw a letter; it is NULL for an
  authorless comment, an unlinked account and a linked person with no cover photo, which is the common case),
  `CountsAmong(photoUIDs)` (one `GROUP BY`; deliberately the bulk shape even though the detail asks about one
  photo, so a per-photo count cannot be written by accident), `Get(uid)` and
  `Create`/`Update`/`Delete` — each a CTE that mutates and reads the row back with its author in one
  round-trip, run through the shared `mutateAudited`: begin → mutate → `audit.Write` on the **same
  transaction** → commit, so a comment that exists always has a record of who wrote it;
  `entryWithComment` stamps the affected comment's UID into `details.comment_uid` (the audit target stays the
  **photo**) on a **copy** of the caller's details map. **Delete is soft** — `deleted_at` is stamped, the row
  stays, every read filters it out — so deleting twice, or editing a deleted comment, is `ErrNotFound` rather
  than a silent success. A body is plain text in and plain text out: nothing is parsed, rendered or sanitised
  server-side, so the client escapes what it displays. `translateMutation` maps no-rows → `ErrNotFound` and a
  `photo_uid` foreign-key violation → `ErrPhotoNotFound`), `internal/photoedit/`
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
  (a persistent job queue in Postgres, **the main robustness gain over the previous system** —
  jobs survive a restart, retry, dedup, wait when the box is offline; the `jobs` table in migration
  `0005_jobs.sql`: `state` queued/running/done/failed/dead, `priority`, `payload` JSONB,
  `attempts`/`max_attempts` (default 5), `run_after` backoff, `locked_by`/`locked_at`; indexes
  (migration `0040_jobs_claim_index.sql`) `(priority DESC, run_after, id) WHERE state='queued'` —
  matching the claim `ORDER BY` exactly, so a deep backlog is walked, not re-sorted — plus
  `(locked_at) WHERE state='running'` for the stale-lock scan, and the **dedup** partial unique on
  `(type, payload->>'photo_uid') WHERE state='queued' OR (state='running' AND type<>'sidecar')`
  (migration `0044` scopes the **sidecar** dedup to `queued` only, so an edit landing while a
  sidecar job runs schedules a follow-up rewrite instead of being swallowed as a duplicate — the
  running job wrote the file before it saw that edit; other types keep queued|running dedup);
  `Store` = `NewStore(pool)` with
  `Enqueue(ctx,type,payload,opts)` (idempotent on the dedup key → `ErrDuplicate`,
  `EnqueueOptions{Priority,MaxAttempts,RunAfter}`) — a thin wrapper over the **package-level
  `Enqueue(ctx, exec, type, payload, opts)`, whose `exec` is an `Execer` (`QueryRow`; both `*pgxpool.Pool`
  and `pgx.Tx` satisfy it), so a job can be scheduled **inside the caller's transaction** exactly the way
  `audit.Write` records a mutation: a registration that rolls back schedules no mail, one that commits
  schedules it once. A payload with no `photo_uid` (a mail, a backup) never dedupes, NULLs being distinct
  in a unique index;
  `Claim(ctx,workerID,types...)` (atomically via `SELECT … FOR UPDATE SKIP LOCKED`,
  `run_after<=now()`, ordered priority DESC/run_after ASC/id ASC, mark running+lock →
  an empty queue `ErrNoJobs`), `Complete(id,workerID)`/`Fail(id,workerID,err)` (increments attempts →
  requeue with exponential backoff via `run_after` base 30 s/cap 1 h, otherwise
  `state=dead`+`last_error`),
  `Defer(id,workerID,delay)` (requeue to `now()+delay` **without** counting an attempt — an offline box waits without
  burning the retry budget), `FailTerminal(id,workerID,err)` (the opposite end: a failure no retry can fix —
  a mail the server rejected as permanently undeliverable, a payload no version of the handler could read —
  is parked in `state=failed`+`last_error` **on the first attempt**, whatever budget is left, and stays
  requeueable by hand once the cause is fixed); **every lifecycle write is fenced by `locked_by = workerID`** → a worker whose
  job was meanwhile reclaimed gets `ErrLockLost` and its late result is dropped instead of clobbering the new
  owner's run; `Heartbeat(id,workerID)` (refreshes `locked_at`; the worker ticks it for as long as a handler
  runs, so a job that legitimately outlives the stale window — a full import pass — is not recovered and run
  twice)/`RecoverStaleLocks(staleAfter)` (a stale lock = a dead worker → requeue as an attempt, **with the same
  backoff `Fail` applies** so a job that kills its process cannot be re-claimed instantly in a crash loop;
  an exhausted job is dead-lettered),
  helpers `CountsByState`/`CountsByType`/`CountsByTypeState` (the `(type,state)` breakdown keyed by the
  comparable `TypeState`; one `GROUP BY type, state` answers all three `/metrics` queue-depth families, so
  a scrape runs one query instead of one per dimension)/`ListDead`/`RequeueDead`/`Requeue` (dead **and**
  failed → queued, for the admin endpoint)/`RequeueAllDead(ctx, types...)` (the **bulk** requeue behind
  `POST /jobs/requeue-dead`: one `UPDATE` over the whole dead letter, or only the given job types, returning
  how many rows moved — the dashboard used to list the dead jobs and requeue one row per round trip, which is
  hopeless for a dead letter of thousands)/`List`(`ListOptions{State,Limit,Offset}`, ordered
  updated_at DESC, limit cap 500, for the admin listing)/`Get`; sentinels
  `ErrDuplicate`/`ErrNoJobs`/`ErrJobNotFound`/`ErrLockLost`/`ErrNotDead`; **job types** `image_embed`/
  `face_detect`/`thumbnail`/`places`/`metadata`/`ocr`/`sidecar`/`storyboard`/`mail_send`/`nameless_detach`/
  `nameless_restore`/`pp_import`/`ps_migrate`/`backup`; `Enqueuer` =
  `NewEnqueuer(store)`
  implements `ingest.JobEnqueuer` (`EnqueueImageEmbed`/`EnqueueFaceDetect`/`EnqueueThumbnail`/
  `EnqueuePlaces`/`EnqueueMetadata`, `ErrDuplicate`=no-op);
  `EnqueueThumbnailRebuild` is the **forced** thumbnail enqueue (payload `{photo_uid,force:true}`, handled by
  `thumbjob`): what a changed rendering — a saved or reset non-destructive edit — schedules, since the thumbnail
  cache is keyed by the original's hash and would otherwise serve the previous rendering forever. The flag rides in
  the payload so **dedup is unchanged** (`idx_jobs_dedup` keys on type + `photo_uid`): at most one active thumbnail
  job per photo, which does mean a plain job already queued absorbs the forced one — acceptable, since the photo is
  scheduled for thumbnailing either way and the next edit schedules a fresh forced job),
  `internal/worker/`
  (the in-process background worker runtime, **the main queue execution loop**: `Registry` =
  `NewRegistry()`+`Register(type, HandlerFunc)`+`Handler`/`Types` (panics on an empty type/nil
  handler/duplicate registration); `HandlerFunc` = `func(ctx, jobs.Job) error`; `Worker` =
  `New(Config{Queue,Registry,Concurrency,TypeConcurrency,PollInterval,StaleAfter,StaleScanInterval,
  IDPrefix})` with `Run(ctx)` — splits the registered `Types` into **pools** (`pools()`): one dedicated
  pool per type named in `TypeConcurrency`, plus a **shared** pool of `Concurrency` goroutines for
  everything else; each pool polls `Claim` filtered to **its own** types, so a type's pool size *is* its
  concurrency limit. `effectiveTypeConcurrency` overlays the config on the built-in caps, and
  **`sidecarBoundTypes` (`image_embed`, `face_detect`) default to one slot each** — the embeddings box
  serves one request at a time, so CPU-bound work must not queue behind it and raising them must be
  explicit (config maps replace rather than merge, so the cap cannot be lost by omission). **`mail_send`
  gets the same one-slot treatment** (`defaultMailConcurrency`): one conversation at a time with a remote
  mail server, and a message that never waits behind a queue of thumbnails. A type with no
  handler gets no pool, and an empty shared pool is not started at all (a type-less `Claim` would mean
  "claim anything"). Worker ids are `<IDPrefix>-<pool>-<i>`, unique across pools. It
  dispatches to the handler by `job.Type`, `Complete`/`Fail` by the result **under the
  claiming worker's id** via a **shutdown-immune** bookkeeping context (`context.WithoutCancel`) — a
  `jobs.ErrLockLost` there means the job was reclaimed, so the result is dropped, not written — plus a
  stale-lock recovery ticker; while a handler runs, a **heartbeat goroutine** refreshes the lock every
  `StaleAfter/3` (floor 100 ms) so a long job is never recovered underneath itself, and it stops (waited
  for, so it cannot race the outcome write) when the handler returns or the lock is reported lost;
  the `Queue` interface = a subset of `jobs.Store` (`Claim`/`Complete`/`Fail`/`FailTerminal`/`Defer`/
  `Heartbeat`/`RecoverStaleLocks`) for testability; **graceful shutdown** = a ctx cancel stops claiming,
  a job whose handler errored at shutdown is abandoned (the queue recovers the lock) — but a
  `RetryAfterError` is **still written as a `Defer`**, since a deferral must never burn a retry attempt;
  a handler panic →
  `ErrHandlerPanic` (job fail, not a crash), an unknown type → `ErrNoHandler`; a handler can return
  `RetryAfter(delay,cause)`/`RetryAfterError` → the worker calls `Defer(delay)` instead of `Fail` (a transient
  error-free failure, no burned attempt — used by `image_embed` when the box is offline), or
  `Terminal(cause)`/`TerminalError` → `FailTerminal` instead of `Fail` (the job can never succeed, so it is
  parked in `failed` rather than retried with backoff — used by `mail_send` for a permanently undeliverable
  address); a built-in **noop**
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
  immediately. **Disabled** = `Enabled:false` (built when neither `embedding.text_url` nor
  `embedding.url` is set) → always unreachable, no probe. It is used by `internal/capabilitiesapi` as
  the source of `semantic_search`; started by `cmd/kukatko/capabilities.go` after 60 s alongside the
  other background services. It is the **one** probe that follows `embedding.text_url` when set
  (`buildReachabilityChecker` overrides the client's `BaseURL` with it): the flag is about semantic
  search, not about the box, and probing a sleeping box would grey the semantic mode out in the UI
  while the search itself answers fine — whereas `internal/wake`'s probe must stay on the box),
  `internal/jobsapi/`
  (a maintainer-only HTTP API over the queue: `NewAPI(Config{Store,RequireMaintainer})`+`RegisterRoutes`
  mounts `/jobs`; `GET /jobs/stats` (counts by_state/by_type+total), `GET /jobs`
  (recent/dead-letter listing, query `state`/`limit`≤500/`offset`, invalid → 400),
  `POST /jobs/{id}/requeue` (dead/failed → queued; 404 missing, 409 non-requeueable);
  the frontend polls, no SSE), `internal/embedding/`
  (an HTTP client to the inference sidecar on the **box** — `kozaktomas/image-embeddings`, all behind
  the `Client` interface (fakeable in tests): `New(Config{BaseURL,TextBaseURL,ImageDim,FaceDim,
  RequestTimeout,TextTimeout,DialTimeout,HealthTimeout,HealthPath,HTTPClient})` → `*HTTPClient`; `ImageEmbedding(ctx,
  img io.Reader)`/`TextEmbedding(ctx,text)` → a 1152-dim SigLIP 2 vector + `model`/`pretrained`
  (`POST /embed/image` multipart `file` streamed via `io.Pipe` / `POST /embed/text` JSON
  `{text}`), `FaceEmbeddings(ctx,img)` → `[]Face` (512-dim embedding, `BBox [4]float64`
  in px `[x1,y1,x2,y2]`, `DetScore`)+`model` (`POST /embed/face` multipart `file`),
  `ImageOCR(ctx,img,minConfidence)` → `OCRResult{Text,Blocks,Lang,Model}` (`POST /ocr/image` multipart
  `file` + the optional scalar field `min_confidence`, omitted when non-positive so the service's own
  default applies — PP-OCRv5 via RapidOCR, latin model; `Text` is the blocks joined by newlines in
  reading order, `Blocks[]{Text,BBox,Confidence}` are returned but **not persisted** anywhere, and an
  image with no text at all is a normal empty result, not an error),
  `Healthy(ctx) bool` (probe `GET /health`, any HTTP response = the box is reachable, only a
  transport-error/timeout = offline), `Health(ctx)` → `SidecarHealth{Model,Pretrained,Dim,Precision}`
  (the same route with the body parsed — the `clip` block of `/health`, `Dim==0` = the sidecar
  reports none; `cmd/kukatko`'s `verifyEmbeddingDim` runs it once at startup and warns when `Dim`
  differs from the configured `image_dim`); **box offline-aware typed errors** `ErrUnavailable`
  (transport failed / status 502/503/504, retryable — helper `IsUnavailable`) vs `ErrBadResponse`
  (a malformed response) vs `ErrDimMismatch` (dimension validation 1152/512) vs `ErrInvalidURL`; a cancelled
  context is not passed off as unavailability; per-request timeouts via context — image/face 60 s (queue
  work on a cold GPU, generous on purpose), **text 5 s** (`TextEmbedding` answers an interactive search,
  which degrades to full-text on failure, so waiting longer is strictly worse than the results it already
  had), health 5 s — over a transport of its own with a **3 s dial timeout**: the box is usually powered off
  and that shows up as a dial nobody answers, where the stock `http.DefaultTransport` would sit for 30 s;
  an injected `HTTPClient` keeps its own transport. All three are configurable (`embedding.dial_timeout`/
  `request_timeout`/`text_timeout`, applied at every construction site through `embeddingClientConfig` in
  `cmd/kukatko`); never holds the whole image in RAM. **`TextBaseURL` (config `embedding.text_url`,
  empty = same host) routes `/embed/text` — and nothing else — to a second instance of the service**
  (`endpointURL` picks the host per endpoint, so a new endpoint lands on `BaseURL` by default): the box
  is a desktop that is usually off, which queue work tolerates and an interactive search does not, so
  production sends the query to an always-on CPU text-only container while images, faces and OCR stay
  on the box (a text-only instance answers those with 503). `Healthy`/`Health` deliberately stay on
  `BaseURL` — `internal/wake` reads that probe, and an always-on green light would mean the magic
  packet is never sent and the embedding queue waits forever), `internal/vectors/`
  (the DB layer for embeddings and faces, **stored directly in Postgres** as `halfvec` (float16)
  columns with HNSW cosine indexes — tables `embeddings`/`faces` in migration `0006_embeddings.sql`;
  `halfvec` instead of `vector` halves the HNSW index memory at a negligible recall loss on
  normalized SigLIP/ArcFace vectors (important on the Pi); `Store` = `NewStore(pool)` over
  the shared pgx pool:
  `SaveEmbedding`(upsert)/`GetEmbedding`(`ErrEmbeddingNotFound`)/`FindSimilar(vec,limit,maxDistance)`
  for 1152-dim image embeddings (768 until migration `0057` swapped the image tower),
  `SaveFaces`(idempotent replace in a transaction)/`ListFaces`/
  `ListFacesBySubject(subjectUID)` (faces with the given `subject_uid`, ordered `(photo_uid,
  face_index)` — the basis of outlier detection; shares `queryFaces`/`scanFace` with `ListFaces`)/
  `SampleFacesBySubject(subjectUID,limit)` → `SubjectFaces{Faces,Total,Photos}` (the **bounded** read of the
  same set: at most `limit` rows selected by an even stride **in SQL** — a row survives where
  `floor(rn·limit/total)` steps up — plus the subject's true face count and distinct-photo count, since a
  sample can no longer be counted from; `limit ≤ 0` = all. It backs the candidate search, whose cost is one
  kNN per exemplar: without it a person tagged on thousands of photos transfers and decodes a 512-dim
  embedding per row, which is how one request reached 10.9 GB — see `docs/PERF.md` §3)/
  `DeleteFaces`/`FindSimilarFaces`/`FindSimilarFaceCandidates` (like `FindSimilarFaces`, but
  also returns the cache `subject_uid`/`subject_name`/`marker_uid` + `bbox` — the basis of identity suggestions)/
  `FindSimilarUnassignedFaceCandidates(vec,limit,maxDistance,exclude)` (like the previous one, but only
  **unassigned** faces `subject_uid IS NULL` and with an **exclusion set** `[]FaceKey` filtered out
  directly in SQL (an anti-join via `unnest` of two parallel arrays) — the basis of finding a person among
  untagged photos, the recognition sweep and the review game; the exclusion filters **before** `LIMIT` and
  runs under `hnsw.iterative_scan = strict_order`, so the caller gets `limit` candidates even when rejections
  take away the nearest neighbours — filtering only after the HNSW limit would silently shrink the result, which is a real
  bug. `subject_uid IS NULL` is served by the **partial index** `idx_faces_unassigned_hnsw` (migration 0047),
  whose predicate is this `WHERE` clause verbatim; keep the two in step or the search costs 9× again.
  `maxDistance` is the one filter applied **in Go**, by cutting the distance-ordered rows at the first one
  beyond it — the same set a SQL predicate returns, but a `(embedding <=> $1) <= x` in the `WHERE` clause is
  invisible to the index scan and made the iterative scan walk to `hnsw.max_scan_tuples` on every call:
  90 ms per exemplar instead of 10, see `docs/PERF.md` §3)/
  `FacesByKeys(keys)` (batch fetch of `faces` rows by `[]FaceKey` `(photo_uid,face_index)` in one
  query via `JOIN unnest` — **including embeddings**; keys with no row (a face deleted by re-detection)
  are missing from the result, order undefined, empty input → `nil`; the basis of `internal/candidates`,
  where the negative-exemplar rule needs the embeddings of the filtered candidate set without N+1)/
  `UpdateFaceMarker(photoUID,faceIndex,markerUID,subjectUID,subjectName)` (writes the cache columns onto
  a single face, empty marker/subject → `NULL`; this is how an IoU match is cached — and, all-empty, how a
  surplus one is cleared)/
  `ListDuplicateFaceMarkers()` (read-only; `GROUP BY marker_uid,photo_uid HAVING count(*) > 1` →
  `[]DuplicateFaceMarker{MarkerUID,PhotoUID,Faces}` ordered by marker uid — the markers cached on more than
  one face, i.e. the surplus links `maintenance` reports and `facematch.ClearSurplusLinks` repairs; the photo
  comes from the faces, because they are what gets re-matched) for 512-dim face
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
  scoring helper for both faces (ArcFace) and labels (image embeddings), so the feature packages don't merely hide one
  rejected row but learn something); sentinels
  `ErrEmbeddingNotFound`/`ErrDimMismatch` (validation 1152/512)/
  `ErrFaceIndexTaken` (UNIQUE `(photo_uid,face_index)`); `ListPhotosMissingEmbedding(limit)` =
  uids of non-archived photos without an embedding (LEFT JOIN, newest first, `limit<=0`=all) for
  backfill; `FindDuplicatePairs(neighbours,maxDist)` = near-duplicate pairs by embedding cosine
  distance (`duplicate.go`, `CROSS JOIN LATERAL` + HNSW `LIMIT` neighbours per photo, no
  O(n²) scan; `maxDist<=0`→no pairs; a read-only tx with `hnsw.ef_search`) — the basis of
  `internal/duplicates`; **face-detection tracking** in the `face_detections` table (migration
  `0009_face_detections.sql`: `photo_uid PK` FK `ON DELETE CASCADE`, `face_count`, `model`,
  `detected_at`, + `detect_width`/`detect_height` from `0061_face_detection_frame.sql`) — because `faces`
  can have zero rows, it is the only way to distinguish a photo
  with no faces from an unprocessed one; `RecordFaceDetection(uid, Detection{Faces,Model,FrameWidth,FrameHeight})`
  (atomically replaces the photo's
  faces **and** upserts the `face_detections` row — even for zero faces; shares the `replaceFaces` tx
  helper with `SaveFaces`), `FacesDetected(uid)` (does a row exist?), `ListPhotosMissingFaces(limit)`
  (uids of photos with no `face_detections` row, like `ListPhotosMissingEmbedding`);
  **the detection frame** (`Detection.FrameWidth`/`FrameHeight` → `detect_width`/`detect_height`, a
  non-positive value stored as NULL) is the pixel size of the image the detector actually **saw**, in display
  orientation because `facejob` rotates before sending. It turns "which frame is this bbox normalised against?"
  from an assumption into evidence, and gives the sideways defect a fingerprint (`sideways.go`):
  `ListSidewaysDetections()` = quarter-turned, non-square photos whose detection has **no** recorded frame
  (everything written before `0061`, all of which ran sideways) or a frame equal to the photo's raw pair, newest
  first — read-only, the dry run of `maintenance repair --sideways-faces`; `ClearFaceDetection(uid)` deletes the
  detection record (keeping the face rows, which the next detection replaces wholesale) so the photo counts as
  unprocessed and is detected again. A detection recorded against the display frame never matches, so the
  repair converges and re-runs as a no-op; FK
  `ON DELETE CASCADE` — deleting a photo
  deletes embeddings, faces and face_detections, fixing the orphan gap the previous system had;
  **orientation geometry** (`geometry.go` + `facerepair.go`), the faces half of
  `maintenance repair --dimensions`: a face row whose cached `photo_width`/`photo_height` are its photo's stored
  pair **swapped** is the fingerprint of the defect, but those rows are **not all in one coordinate space** —
  whether the embeddings sidecar auto-rotated an image before detecting on it has varied over the library's life
  — so the correction is chosen **per row from evidence**, never from provenance. `FrameTransform` =
  `TransformSkip` (write nothing at all) / `TransformFrame` (the box is already in display space, only the cached
  frame was transposed) / `TransformRotate` (`RotateRawBBox(bbox,orientation)`: the numbers are a correct box in
  the **RAW** frame and need the quarter turn the EXIF tag describes — 5 transposes, 6 turns 90° CW, 7
  transverses, 8 turns 270° CW; a box inside the raw frame is always inside the display frame after it) /
  `TransformRescale` (`RenormalizeTransposedBBox(bbox,rawW,rawH,orientation)`: a per-axis rescale by `rawW/rawH`
  and its reciprocal, for a box the sidecar had **already** auto-rotated and that was only divided by the wrong
  pair; a degenerate frame or anything but a quarter turn returns the box unchanged). The evidence is the photo's
  **valid face markers** (display space, regions a person has seen sit on a face): the candidate whose
  transformed box lands nearest a marker **centre** (not IoU — the two detectors size a face differently) wins if
  it is within 5 % of the frame and beats the runner-up by both a factor of 2 and 0.02; a candidate whose box
  would fall off the photo is refuted before it is scored. A row the markers cannot place inherits its photo's
  verdict only when every decided row of that photo agrees (a photo's faces come from one detection run, hence
  one space), otherwise it stays `TransformSkip`. Measured against the live catalogue the rotation reconciled 4
  detection/marker pairs out of 4 and the rescale 0, which is why the old blind rescale was replaced.
  `PlanFaceBoxRepair()` → `[]FaceBoxPlan{Face TransposedFace,Transform,BBox}` is read-only — **the dry run** —
  and finds its candidates by joining `photos`, so a row stays findable after the photos half has corrected the
  photo row; `ApplyFaceBoxRepair(plans)` writes only the plans that carry a transform, each guarded on the
  fingerprint it was planned from, so a **skipped row keeps the fingerprint a later run finds it by** (a marker
  added tomorrow is new evidence) and no box is ever moved twice),
  `internal/people/`
  (the DB layer for **subjects** (people/animals/other) and **markers** (face/label regions on
  photos), tables `subjects`/`markers` in migration `0008_subjects_markers.sql`: `subjects`
  = `uid PK` (prefix `su`), `slug UNIQUE`, `name`, `type IN (person|pet|other)`, `favorite`,
  `private`, `notes`, `cover_photo_uid` (FK photos `ON DELETE SET NULL`),
  `birth_year`/`death_year` (nullable INTEGER, migration `0051`), timestamps; `markers` =
  `uid PK` (prefix `mk`), `photo_uid` (FK photos `ON DELETE CASCADE`), `subject_uid` (FK
  subjects `ON DELETE SET NULL`), `type IN (face|label)`, a normalized bbox `x,y,w,h`
  DOUBLE PRECISION (0..1 display space, like `faces.bbox`), `score`, `invalid`, `reviewed`,
  timestamps + indexes on `photo_uid`/`subject_uid`; `Store` = `NewStore(pool)` over the shared pgx
  pool: **subjects** `CreateSubject`(generates a uid + a **unique slug from name** — `Slugify`
  without diacritics/ASCII, a collision → a numeric suffix `name-2`)/`GetSubjectByUID`/`GetSubjectBySlug`/
  — **two slug functions, and mixing them up is a data bug**: `Slugify` is **total** (a name it cannot
  slugify falls back to the constant `subject`) and is for slug *generation*; **`NameSlug`** returns `""`
  when the name identifies nobody (empty/whitespace/punctuation) and a digest slug `subject-<8 hex>` for a
  name written outside ASCII (CJK/Cyrillic/…, still a name, so still distinct), and is the **only** key a
  find-or-create-by-name path may use. Keying such a path on `Slugify` makes the fallback a **catch-all**:
  every nameless face resolves to it, the first creates one empty-named subject and the rest are *found* by
  it — in production that collected 16 532 markers on a single fake person (fixed in the importers of the
  day and in `facematch`; `peopleapi` rejects such a name with 400; the repair for existing
  data is `kukatko maintenance nameless-subjects`, see [`OPERATIONS.md`](OPERATIONS.md))/
  `UpdateSubject`(re-slugging + refresh of the `faces.subject_name` cache)/
  — **the life span** `BirthYear`/`DeathYear *int` (nil = nobody recorded it, which is the normal case;
  plain years, because a year is what anybody knows about the people in a family archive, and every age
  derived from them is shown as an approximation). Both write paths run them through
  `validateLifeYears(birth,death,nowYear)` → `ErrInvalidLifeYears`: each known year within
  `MinLifeYear`(=1800)…**the current year**, and `death >= birth`. The clock-independent half is also a
  SQL CHECK (`>= 1800`, `death >= birth`); the "not in the future" half **cannot** be, since a CHECK may
  only use immutable expressions. Nothing ties the years to `type='person'` — the type is editable, and a
  constraint firing on an unrelated reclassification would break a harmless edit. `SubjectUpdate` rewrites
  them like every other field, so a nil **clears**/`ListSubjects` (ordered by
  name, with **two** counts over the same non-invalid markers on visible photos: `MarkerCount` =
  `COUNT(p.uid)`, what the face tools mean, and `PhotoCount` = `COUNT(DISTINCT p.uid)`, what the
  people index shows — they part company when one photo carries several markers of the same subject,
  and `PhotoCount` is exactly the length of `ListPhotoUIDsBySubject`, so a "N photos" badge keeps its
  promise about the gallery behind it; `SubjectStats(uid)` (`stats.go`) answers the same two numbers plus the
  **year span** (`OldestYear`/`NewestYear`, both 0 for a wholly undated person) for **one** subject, with the
  same joins so it cannot become a third opinion — a primary-key lookup plus `idx_markers_subject_uid`, cheap
  enough for the review game's per-answer reveal, `ErrSubjectNotFound` for an unknown uid; plus
  `CoverFace *SubjectFace` = the face that illustrates the subject in the people grid when it has no
  `cover_photo_uid` — the `best_face` CTE in `listSubjectsSQL` takes per subject a `DISTINCT ON` with
  the order **`w*h DESC, score DESC, uid`**: the tile is a square zoomed from the crop of the cache
  thumbnail, so readability is decided by the number of pixels behind the face → the largest box wins,
  `score` only breaks a size tie (the reverse order would put a tiny sharp
  mug before a large decent one) and `uid` keeps the choice deterministic; filters: `type='face'`
  (drawn label boxes aren't faces), `invalid=FALSE` (rejected false-positives aren't returned),
  a non-zero box and photo dimensions, and a visible photo as with the count. It also carries the photo's `width/height/orientation`
  — the client crops the region itself and without the frame would distort it)/
  `DeleteSubject` (the FK detaches the markers, clears the faces cache)/**the merge of two subjects**
  (`internal/people/merge.go`) `MergeSubjectsAudited(sourceUID,keeperUID,entry)` → `MergeResult`
  (the repair for one person catalogued twice, all in **one transaction**: it takes a row lock on both
  subjects (`WHERE uid = $1 OR uid = $2 ORDER BY uid FOR UPDATE`, one statement so two merges naming the
  same pair in opposite directions queue instead of deadlocking), notes the photos carrying a marker of
  **both** (while the markers still tell them apart), repoints `markers` and the denormalized `faces`
  cache (`subject_uid`+`subject_name`, no FK there — this package is what keeps it in step), carries
  `face_confirmations`, `face_rejections` and `duplicate_marker_dismissals` over
  (`INSERT … SELECT … ON CONFLICT DO NOTHING`, keeping who said it and when), fills the keeper's **empty**
  fields from the source (`favorite`/`private` OR-ed — a merge must not undo a favorite or weaken a
  privacy flag — `notes`/`cover_photo_uid` only when the keeper has none, its own values never
  overwritten; `birth_year`/`death_year` as a **pair**, onto a keeper carrying neither, since filling
  them separately could pair one person's birth with another's death and trip the `death >= birth`
  CHECK, aborting the whole merge), deletes the source (whose CASCADEs sweep the feedback rows the merge deliberately left)
  and writes the `subject.merge` audit entry on the same tx. **Three rules decide the disagreements.**
  (1) **Markers are never deduplicated**: a photo that carried a marker of each keeps both, so no region
  and no assignment is thrown away — it simply becomes a repeated-marker group for `internal/dupmarkers`
  to settle, and their number is reported as `SharedPhotos`. (2) **A positive record beats a rejection**,
  whichever side it came from: the keeper's "not them" for a face the source is assigned to or has
  confirmed is deleted (this runs **before** the assignments move — naming the source is what identifies
  those rows), and a rejection of the source's is not carried onto a face the keeper is assigned to or
  has confirmed. A rejection only ever *excludes a face from a search*, while an assignment is a
  statement about the library, so a surviving contradiction could only hide a face the merged person
  demonstrably owns. (3) A **repeated-marker dismissal is not carried onto a shared photo** — that group
  is new, nobody has judged it, and a dismissal about a different pair of boxes would hide exactly the
  duplicates the merge just made. `ErrMergeIntoSelf` and `ErrSubjectNotFound` are returned before
  anything is written; ordering is load-bearing throughout and `mergeSubjectsTx` documents it step by
  step)/**the nameless-subject repair**
  (`internal/people/nameless.go`) `ListNamelessSubjects` (every subject whose `NameSlug` is `""`, with its
  marker and face counts — the **dry run**; the predicate stays in Go so the repair cannot drift from the
  guard the importers use)/`SnapshotSubject(uid)` (the same snapshot **read-only** — the half the admin HTTP
  repair needs *before* it schedules anything, since over HTTP the browser's download is the undo file and it
  has to be in hand first; a plain, not read-only, tx because the read takes `FOR UPDATE`)/
  `DetachSubject(uid,entry)` (one tx: snapshot the subject + its marker uids +
  its `(photo_uid,face_index)` face refs, clear the faces cache, delete the subject, audit — returns the
  `SubjectSnapshot` that **is** the undo, since nothing else records the removed links)/
  `RestoreSubject(snap,entry)` (re-inserts the row under its original uid/name/timestamps — only the slug
  may be disambiguated — and re-points its markers and faces; each slug attempt runs in its own audited
  transaction, because a unique violation aborts the transaction it happens in)/`ListPhotoUIDsBySubject` (distinct
  uids of non-archived photos with a non-invalid marker of the subject, newest-first — the basis of the subject's
  gallery in `peopleapi`)/`SearchSubjects(q,limit)` (accent/case-insensitive ILIKE over
  `immutable_unaccent(name)`, cap limit — the basis of `globalsearchapi`)/`SubjectCovers(uids)` (`covers.go`;
  → `map[uid]Cover{PhotoUID,FileHash}`, the photo standing for each named subject, resolved for the **whole
  batch in one query**: a `DISTINCT ON` over the batch's non-invalid markers, newest-first by
  `COALESCE(taken_at, created_at)`, restricted to the photos the subject's gallery shows and overridden by a
  hand-picked `cover_photo_uid`. It is a whole **photo**, not the `SubjectFace` the people index crops — a
  medallion drawn from a face crop of a small tile is mush; a subject on no visible photo is absent from the
  map); **markers** `CreateMarker`
  (validation of type/`0..1` bounds, optionally a subject right away → faces cache)/`GetMarkerByUID`/
  `ListMarkersByPhoto`/`AssignSubject`+`UnassignSubject` (in a transaction they update the
  denormalized **faces cache** `marker_uid`/`subject_uid`/`subject_name` via
  `WHERE marker_uid = $1`)/`SetMarkerInvalid`/`SetMarkerReviewed`/`DeleteMarker` (clears the
  faces cache); sentinels `ErrSubjectNotFound`/`ErrMarkerNotFound`/`ErrSlugExhausted`/
  `ErrInvalidType`/`ErrInvalidBounds`; **the faces cache is kept consistent** on every change of a
  marker/subject (delete, rename, assign/unassign); **audited variants**
  `CreateSubjectAudited`/`UpdateSubjectAudited`/`DeleteSubjectAudited` and
  `CreateMarkerAudited`/`AssignSubjectAudited`/`UnassignSubjectAudited`/`SetMarkerInvalidAudited`
  (`internal/people/audit.go`)
  take an `audit.Entry` and write it **in the same transaction** as the change (`audit.Write(ctx,tx,entry)`),
  so the audit row commits/rolls back atomically with the mutation (the `internal/photos`/`internal/organize` convention);
  a shared tx-core (`insertMarkerTx`/`assignSubjectTx`/`unassignSubjectTx`/`setMarkerInvalid`/
  `prepareSubjectInsert`) is used by
  both variants. `SetMarkerInvalidAudited` (action `marker.invalidate`, used by the repeated-marker review)
  changes **nothing but the flag**: the row survives and keeps its subject, so the decision is reversible and an
  invalidation stays distinguishable from an unassignment), `internal/facematch/`
  (linking detected faces to markers/subjects + identity suggestions, all behind the interfaces
  `PhotoStore`/`FaceStore`/`PeopleStore` (unit-testable with fakes without a DB): `Service` =
  `New(Config{Photos,Faces,People,IoUThreshold,SuggestionLimit,SuggestionMaxDistance,MinFaceSize})`;
  **IoU geometry** `IoU(a,b [4]float64)` (a pure function, Intersection-over-Union of normalized
  boxes `[x,y,w,h]`); **`matchMarkers`** (`pairing.go`, pure) pairs a photo's faces with its **face**
  markers (ignores `invalid`) **exclusively**: it scores every (face,marker) pair reaching
  `IoU ≥ faces.iou_threshold` (default 0.1, carried over unchanged), sorts them by descending IoU and
  takes them greedily while both sides are free, so **one marker is claimed by at most one face**
  (ties break on face index, then marker uid, so consecutive requests reach the same pairing instead of
  rewriting the cache back and forth); the per-face "best marker" search it replaces was not exclusive
  and rendered one person twice on a photo; `surplusFaces` names the faces whose cached link sits on a
  marker another face won (a marker nobody claims any more is kept by the lowest face index, so a
  repair converges instead of reporting the same duplicate forever);
  **`PhotoFaces(ctx,photoUID)`** (backing `GET /photos/{uid}/faces`) → resolves the pairing once,
  determines each face's action (`create_marker`/`assign_person`/`already_done`),
  **caches the link onto the face row** via `vectors.UpdateFaceMarker` — and **clears** a surplus one,
  so a face the pairing left empty behaves as a face without a marker everywhere
  `faces.subject_uid` is read — and adds suggestions to **every** face
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
  (actor NULL); **`ClearSurplusLinks(ctx,photoUID)`** re-derives one photo's pairing and clears the
  cached `marker_uid`/`subject_uid`/`subject_name` of every face holding a surplus claim (→ how many rows
  were cleared) — the bulk counterpart of what `PhotoFaces` does when a photo is viewed, for the links
  written before matching was exclusive; it is what `maintenance repair --face-markers` drives and it
  **never deletes a face or a marker** nor touches a face whose link is the only one on its marker;
  sentinels `ErrInvalidAction`/`ErrMissingBBox`/
  `ErrMissingMarker`/`ErrMissingSubject`, a missing photo/marker/subject → 404 in the HTTP layer
  (`photoapi.FaceService` interface + handlers in `internal/photoapi/faces.go`); tunables in
  `faces.*` config), `internal/embedjob/`
  (wiring of the image embedding into the queue + embedding queries, all behind the interfaces
  `PhotoStore`/`VectorStore`/`Previewer`/`Enqueuer`+`embedding.Client`: `Service` =
  `New(Config{Photos,Vectors,Client,Previewer,Enqueuer,PreviewSize,OfflineRetryDelay,
  DuplicateMaxDist})`; **the `image_embed` handler** `Handle`(=`worker.HandlerFunc`, registered
  in `serve`) → from the payload `{"photo_uid"}` loads the photo, opens the `fit_720` preview through
  `Previewer.OpenOrGenerate(ctx,photo,size)` — **the whole `Previewer` interface**, one method: the preview is
  asked for by photo, never resolved out of the thumbnail cache by hash, because on the object-store backend a
  published preview routinely has no cache file (that mistake dead-lettered every `image_embed` job on R2, see
  `internal/thumb`) —
  sends `ImageEmbedding` to the sidecar, stores a 1152-dim `halfvec` via `vectors.SaveEmbedding`+`model`/
  `pretrained`; **idempotent** (a photo with an embedding is skipped without calling the sidecar), **box
  offline** (`embedding.IsUnavailable`) → `worker.RetryAfter(5 min)` (deferral without burning an attempt),
  any other error a normal retry; `BackfillEmbeddings(ctx)` enqueues `image_embed` for every photo without
  an embedding (dedup no-op), returns the count; `Duplicates(ctx,uid)` embedding-based detection of near
  duplicates within `duplicate.embedding_max_dist`, excluding itself (`<=0` disables it)), `internal/facejob/`
  (wiring of face detection into the queue, all behind the interfaces
  `PhotoStore`/`VectorStore`/`ImageSource`/`Enqueuer`+`embedding.Client`: `Service` =
  `New(Config{Photos,Vectors,Client,Source,Enqueuer,OfflineRetryDelay,MinDetScore})`; **the
  `face_detect` handler** `Handle`(=`worker.HandlerFunc`, registered in `serve`) → from the payload
  `{"photo_uid"}` loads the photo, opens the **full-resolution UPRIGHT original** via
  `StorageSource.OpenUpright` (= `storage.Materialize` + `imgconvert.EnsureDecodable` behind the interface
  `Materializer`, HEIC/RAW/video are converted, `Close` frees the temp and the materialized original),
  sends `FaceEmbeddings` to the sidecar (512-dim + pixel bbox + det_score) and
  stores it via `vectors.RecordFaceDetection(uid, vectors.Detection{Faces,Model,FrameWidth,FrameHeight})`;
  the original (not the thumbnail) so the detector sees every pixel there is.
  **The sidecar does NOT apply EXIF** — measured against the running service: one production iPhone original
  with `orientation=6` returned 2 misshapen boxes as it lay and 6 face-shaped ones pre-rotated, a second
  returned 0 against 2 — so **this package owns the rotation**: `OpenUpright` reads the orientation tag of
  **the file it is about to send** (`exif.FileOrientation`, never the catalogue — an `imgconvert` copy may
  already have the rotation in its pixels), and for a tag > 1 decodes, applies `imgconvert.Orient` and
  re-encodes a JPEG with no EXIF at all (`NewStorageSource(storage, maxPixels)`, cap = `thumb.max_pixels`
  via `imgconvert.EnforcePixelBound`, over the cap = a visible job failure, not a sideways send); a file with
  no tag is streamed byte-for-byte, so the common case pays nothing. It returns `UprightImage{Reader,Width,
  Height}` whose frame is **measured on those very bytes**, and **bbox conversion**
  `NormalizeBBox(bbox, frameWidth, frameHeight)` divides pixel `[x1,y1,x2,y2]` → normalized `[x,y,w,h]`
  (0..1) by exactly that frame — no orientation enters the conversion, which is what makes the old defect
  (divide by the stored pair swapped, while the pixels were in the raw frame) unrepresentable; the photo's
  stored pair/orientation are still copied onto the face row as the render hints they always were;
  **det_score filter**
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
  `POST /process/thumbnails` → `{enqueued,pending,dry_run}` runs `thumbjob.BackfillThumbnails(all)` (backfill of
  `thumbnail` for photos without a thumbnail = without a pHash; `?all=true` schedules every non-archived photo;
  `?dry_run=true` schedules **nothing** and only reports `pending` — the count `thumbjob.CountBackfillThumbnails`
  answers, so a full-library run is a number read beforehand rather than a surprise; a real run reports both,
  so its size is visible in the response too; `ThumbnailBackfiller` optional — nil → 503; local, works even
  with the box offline; `queryFlag` parses `?all`/`?dry_run`),
  `POST /process/metadata` → `{enqueued}` runs `metajob.BackfillMetadata(all)` (backfill of `metadata`
  for photos whose file has never been read = `metadata_extracted_at IS NULL`; `?all=true`
  forces a re-read of every non-archived photo; `MetadataBackfiller` optional — nil → 503;
  local, works even with the box offline)), `internal/processing/`
  (the same question as `processapi` asked about **one photo**: what has the library already computed about
  it, and schedule the one step it missed. `Step` values **are** the `internal/jobs` type constants (no
  parallel vocabulary), `Steps` fixes the report order `metadata`, `thumbnail`, `image_embed`, `face_detect`,
  `ocr`, `places`, `sidecar`, and `ParseStep` is the untrusted-input boundary; `storyboard` is deliberately
  **not** a step — it is rendered lazily on first playback into the local derived-media cache and leaves no
  persisted evidence, so it has no honest state to report and nothing a button could usefully schedule ahead
  of a viewer pressing play. `Store` = `NewStore(pool)`: `Evidence(photoUID)` reads the whole evidence in
  **one** query — `photos.media_type`/`lat`/`lng`/`metadata_extracted_at`/`ocr_at`/`ocr_text`/
  `sidecar_written_at` plus four LEFT JOINs on the PK-keyed side tables `photo_phashes`, `embeddings`,
  `face_detections` (also the `face_count`) and `photo_places`; the place column is **conditional**
  (`lat IS NOT NULL AND lng IS NOT NULL`) because `photo_places` also holds the coordinate-less marker the
  geocoder writes for a photo with no GPS, and that records that there was nothing to do rather than a place.
  `Service` = `New(Config{Evidence,Jobs,Enqueuer,Disabled})` (panics on a nil collaborator):
  `Report(photoUID)` = evidence + `jobs.Store.UnfinishedForPhoto` (**two** round trips, never an N+1) →
  `[]Status{Step,State,At?,Error?,FaceCount?,TextFound?}`; the precedence is landed evidence → the queue →
  skipped → pending, so work that has landed is `done` however the queue looks, work in flight beats "will
  not run", and only an absence with nothing behind it is `pending`. `jobState` maps a queue row: running
  wins over its own previous error, a dead job **and** a queued one carrying a `last_error` (a retry pending
  after a failure) are both `failed` with that text. `Disabled` carries the steps whose feature is off
  instance-wide (`ocr`, `sidecar`, `places` — exactly the config-gated handlers of `buildRegistry`); with no
  handler registered a job of that type would sit queued forever, so those read as `skipped` and `Run`
  refuses them. `Run(photoUID,step)` validates, refuses an inapplicable step (`ErrStepNotApplicable`) and an
  unknown one (`ErrUnknownStep`), then dispatches to the step's own `jobs.Enqueuer` method (keeping the
  sidecar's debounce) and answers with the step's new state; the queue's dedup index makes a double click
  harmless. Read-only apart from that one enqueue — it never runs the work itself), `internal/cluster/`
  (face auto-clustering: groups **not-yet-assigned faces** (without a subject) into clusters of the same
  person, so a whole cluster can be named in one go (a key UX improvement over naming faces one at a time);
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
  by ordering them by distance from the centroid of the person's embeddings, carried over unchanged; all behind the interfaces
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
  Photos,Media,MaxDistance,SearchLimit,MinFacePx,Concurrency,MinFaceRel,MaxExemplars,MaxCandidates})`, tunables
  default via the `Default*` constants (0.5/1000/32/8/500/500).
  **`Find(ctx,subjectUID,Request{Threshold,MinDistance,Limit})`**: verifies the
  subject (`people.ErrSubjectNotFound`); reads `SampleFacesBySubject` — at most `MaxExemplars` faces, sampled
  with an even stride **in SQL**, plus the subject's true face/photo totals — and **deduplicates exemplars to
  one per source photo** (highest `det_score`, tie lowest `face_index`), so a photo with three
  faces of that person doesn't vote three times; for each exemplar runs `FindSimilarUnassignedFaceCandidates`
  with **bounded concurrency** (`errgroup.SetLimit`), an exclusion set of already-rejected faces
  (`feedback.FaceRejectionsForSubject`) and a **per-exemplar neighbour cap that follows the source set**
  (`perExemplarLimit`: a lone exemplar keeps `SearchLimit` — the store caps it at its own 500-row maximum,
  which makes its ranking exact — a crowd gets `4·MaxCandidates` shared between them, floored at 100; every
  neighbour is paid for once per exemplar, which is why a 428-exemplar subject took 13.7 s, see
  `docs/PERF.md` §3); **merges candidates with voting** (`match_count` = the number of
  distinct exemplars, `distance` = the **minimum** across votes); **vote rule** `min_match_count`
  (`computeMinMatchCount`, scales `√exemplarCount * threshold/base / 2`, **clamp 1..5** and ≤ the number of
  exemplars — a single-face subject always 1; returned in the response so the UI can explain the filter); then
  a **relative size floor** (`bbox[2] ≥ MinFaceRel`, shares `faces.min_face_size`); `boundSurvivors` then applies
  the request's `MinDistance` floor (drop candidates *nearer* than it — a caller after the uncertain middle
  only), orders nearest-first and **cuts to `MaxCandidates` before hydration** (`capped` in the response), which
  is the memory bound: past that cut every candidate costs a whole `photos.Photo`, EXIF blob included, and the
  request's `Limit` truncating afterwards bounds the answer but not the work; the survivors are
  hydrated (`photos.ListByUIDs` + `mediaurl.Decorate`), an **absolute pixel floor** is applied
  (`MinFacePx`) and the **negative-exemplar rule** (`vectors.IsNegativeExemplar` over embeddings from
  `FacesByKeys` — **a no-op without rejections**, the embeddings are then not loaded at all); finally the **action
  classification** (`create_marker` without a marker / `assign_person` a marker without (another) subject / `already_done`
  the marker already points at this subject = a stale cache, via `GetMarkerByUID` with a cache), ordered by distance
  and truncated to `Limit` (0 = as many as `MaxCandidates` allows). `Result` =
  `{subject_uid,source_photo_count,source_face_count,exemplars_used,source_capped,capped,
  faces_without_embedding,min_match_count,threshold,reason?,counts{create_marker,assign_person,
  already_done},candidates:[Candidate{photo,face_index,bbox{relative,pixel},distance,match_count,
  action}]}` — the counts describe the **subject**, not the sample, and both caps are surfaced rather
  than applied silently; `bbox` carries **both relative 0..1 and pixels** honouring EXIF orientation (`displayDims` swaps
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
  Log→`slog.Default()`, nil store→panic), `Sweep(ctx, Params{Threshold,MinDistance,Limit}, emit func(Event) error)`
  (`MinDistance` passes straight through to `candidates.Request`, so a caller wanting only the uncertain middle
  never pays to hydrate the confident matches);
  lists subjects with `MarkerCount>0`, caps at `MaxSubjects` (`capped`+`SubjectsTotal`
  in the summary), the scan runs in a **bounded worker pool** (`errgroup.SetLimit`) and **funnels** the results
  through one consumer, so `emit` is always called **serially** (the handler writes it straight into
  the response); `emit` returns an error → the sweep stops and the workers cleanly unblock (no leak);
  a `Find` error for one subject is logged and skipped (`emitResult`), only the subject listing is
  fatal; `already_done` candidates are filtered out of the work list (`actionableCandidates`),
  but counted into `TotalAlreadyDone`; **never auto-confirms**. `Event` = `{type,progress?,
  person?,summary?}` (`events.go`). Unit tests with fakes (concurrency/cap/filter/omit-empty/emit-fail),
  an integration test over real candidates+DB. **`Scan(ctx, Params, Window{Offset,Budget}, collect Collect)
  (Coverage{SubjectsTotal,Scanned,NextOffset}, error)`** (`scan.go`) is the **bounded** form of the same
  scan for callers that answer inside one request instead of streaming: it walks a **rotating window**
  (`Offset` wraps, `Budget` <= 0 = every planned subject) of the same plan and stops dispatching as soon as
  `collect` returns `enough` — subjects already in flight still finish and are still collected, so a stop
  can overshoot by up to `Concurrency` but never throws computed work away; `Coverage.SubjectsTotal` is the
  library-wide count (so a bounded caller can still tell "no named people" from "nobody in this window"),
  `NextOffset` is fed back to continue the rotation with no gap. Per-subject failures are logged and skipped
  exactly as in `Sweep`; only the subject listing is fatal. `Sweep` shares the dispatcher (`dispatchState`)
  and the `Person` projection (`personOf`) and is otherwise unchanged — `GET /faces/sweep` still covers
  everyone. Unit tests in `scan_test.go` (budget bound, rotation without gaps, early stop, wrapping offset,
  skipped failures, collector error, empty library)), `internal/sweepapi/`
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
  SourceCap,Concurrency})`, tunables default via the `Default*` constants (0.20/50/200/200/500/8 —
  `DefaultMaxDistance` is model-specific and was re-derived for SigLIP 2, see `THRESHOLDS.md`), nil
  store→panic, nil `Media` is OK. **`Album(ctx,uid,Request)`** / **`Label(ctx,uid,Request)`** share
  one core `find`, differing only in how the source set is resolved (validation via `GetAlbumByUID`/
  `GetLabelByUID` → `organize.ErrAlbumNotFound`/`ErrLabelNotFound`; membership via `ListPhotoUIDs`/
  `ListPhotoUIDsByLabel` — **native, no foreign catalog**; a label additionally `LabelRejectionsForLabel`).
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
  (**the review game** — a queue of "one at a time" questions and the application of answers;
  **it composes existing pieces, reimplements nothing**. **Five kinds** (`Kinds`): face questions via the
  `Sweeper` interface (satisfied by
  `*sweep.Service` → per-subject candidate search with all its filters: unassigned-only,
  rejections, negative exemplar, min. face size), label questions via `Expander` (satisfied by
  `*expand.Service` → excludes members and rejected ones), and the **three checks over work the machine already
  did** (`extras.go`): `place` via `PlaceReviewer` (`*geoestimate.Reviewer`), `duplicate` via `DuplicateFinder`
  (`*duplicates.Service`) and `outlier` via `OutlierRanker`+`SubjectLister` (`*outliers.Service` +
  `*people.Store`), all hydrating their photos through `PhotoStore.ListByUIDs`+`Media`. **Each of the three is
  optional** — a nil dependency simply switches that question type off, so a half-wired server still serves the
  other kinds. Writes via `Assigner` (`*facematch.Service`),
  `OrganizeStore.AttachLabelAudited`, the `PlaceReviewer`'s own accept/reject and
  `FeedbackStore.RejectFace/RejectLabel/ConfirmFace/ConfirmDuplicate/DismissDuplicate`; `New(Config{...,BandMin,
  BandMax,SureMin,SureShare,QueueSize,RoundSize,RoundMaxPerEntity,CacheTTL,MaxLabels,LabelConcurrency,
  FaceBudget,LabelBudget,BuildTimeout,MaxPerEntity,OutlierBudget,OutlierThreshold,KindShares,
  SkipMuteThreshold,SkipMuteCooldown,Now})` (an invalid band → the
  default pair 0.45/0.75, `Now` = a test hook).
  **What the game is about (`kinds.go`) — configuration, not an accident.** `kindShares` is one weight per
  kind (`review.kind_shares.*`, **default `face: 1` and nothing else**), normalised over the enabled ones;
  `newKindShares` drops non-positive entries and falls back to faces when everything is off. It decides three
  things: a kind at zero is **never scanned** (`collect`/`collectChecks` skip it, so its total stays 0 and
  `reasonFor` cannot name it), the round mixer prefers whichever kind its running share is furthest behind on
  (`wanted`, the same positional rule `blend` uses for the tiers), and `presentIn` narrows the shares to the
  kinds a pool actually holds so a share reserved for a kind with no material does not pace the ones that have
  some. What a share deliberately does **not** do is shrink a collector's target: every enabled kind still
  gathers a full pool's worth, so a kind that is the only one with material fills the pool alone. Restoring the
  mix the game started as is `face: 0.95, label: 0.05`; only the ratios matter, so `19 / 1` says the same.
  **Remembering "I don't know" (`skips.go`, `skipstore.go`, migration `0059`).** Yes and no leave a durable
  trace of their own; "don't know" left none, so a restart forgot it and the game asked again. A skip on a
  **face or outlier** question is now written per (user, subject, photo) through the optional `SkipStore`
  (`*review.SkipRecorder`; a nil one leaves skips session-scoped as before), and the whole mute is *derived*
  from those rows — there is no second state table. `SkipMuteThreshold` (default 3, `review.skip_mute_threshold`)
  skips about one person mute them for that player; two are forgiven, because the first couple are far more
  often an unclear photo than an unknown face. `mutePolicy.muteFor` puts the mute's start at the newest skip
  and its length at `SkipMuteCooldown` (default 168 h, `review.skip_mute_cooldown`) **doubled once per skip past
  the threshold**, capped at a year — so a re-skip after the pause expires mutes for longer rather than for the
  same short wait. `SkipMemory.silences` then drops a question when the photo was itself skipped for that
  person (silent for good — that is the "not asked about before" rule the post-mute retry needs) or when the
  mute holds **and** the photo was already in the library when it began, so a newly imported face of that person
  is asked about at once. The memory is read once per rebuild (`loadSkipMemory`, cached on the session) and
  applied in `excludeSeen` beside the session's own shelf. It is strictly **per user**, it is **best effort** on
  write (a game answered at one keypress per second must not fail because a row could not be written), and it
  reaches **nothing** in the catalogue: never a `feedback` rejection, never a narrowing of `candidates`/`sweep`,
  never an identity change, and deliberately **not in the audit trail** — the audit records what happened to the
  library, and a player saying "I don't know" is a fact about the player. Three further dependencies are **read-only extras**, each
  optional and each switched off by a nil: `Albums` (`AlbumMembership`, satisfied by `*organize.Store` →
  `AlbumUIDsForPhotos`) feeds the mixer's album rule, `Breathers` (`BreatherSource`, satisfied by
  `*review.BreatherStore`) picks the round's non-question card, and `Stats` (`SubjectStatsReader`, satisfied by
  `*people.Store` → `SubjectStats`) reads the answer reveal. **None of them can fail a round or an answer** —
  every failure is logged and degrades to "that extra is absent".
  **The rounds (`mixer.go`) — a playlist, not a sorted list.** Everything above produces a *pool*
  (`QueueSize`, default 20); what a player is served is a **round** mixed out of it (`RoundSize`, default 10,
  `review.round_size`), and **one request is one round**. `mixRound(pool,mixConfig,albumLookup) → (round,rest)`
  fills the round a slot at a time, scoring every unplaced question against what the round already holds and
  taking the cheapest. The penalty weights are powers of two an order of magnitude apart, so a rule never
  trades against a more important one: `costEntityCap` (over `RoundMaxPerEntity`, default 3,
  `review.round_max_per_entity`) > `costKind` (a kind other than the one the configured shares are furthest
  behind on) > `costTier` (the tier the running confident share does not want — this is what
  *interleaves* the tiers instead of blocking them, while keeping `SureShare` over the round) > `costAlbum` >
  `costMoment` (within `nearMoment`, 10 min — the same burst) > `costEra` (the same decade).
  **One rule is a refusal rather than a price**: `MaxRun` (= `maxSameEntityRun`, 2) consecutive questions about
  one entity. Pricing it was the bug — the price sat *below* `costEntityCap`, so once every entity in the round
  had had its three questions the cheapest candidate was whichever also continued a run, and one person got
  asked about five times running. `mixer.breaksRun` now removes such a candidate from consideration
  (`cheapest(…, keepRun: true)`), falling back to the whole pool only when every unplaced question belongs to
  that entity. **Everything else is forbidden nothing**: a pool of one person, or of one afternoon, still yields
  a full round, because the remaining rules are preferences over a total order and there is no way for them to
  return an empty round while a question exists. Ties go to the earlier position in the pool's informativeness
  order, so variety is never bought with a less relevant question. The only seeded choice is the round's opening
  kind (`roundSeed(userUID,sequence)`), and it is expressed as the tie-break inside `kindShares.wanted` rather
  than as a rule of its own — so it decides only between kinds the shares want equally, which keeps the mixer a
  pure function of (pool, config, seed) — so a
  **re-fetch before answering returns the same round** and the variety tests can assert against `mixRound`
  directly. The round is the *head of the pool* rather than a second list (`session.roundLen`), so answering
  shortens it through the same bookkeeping as everything else, and `roundSummary` freezes its composition
  (`RoundInfo{Index,Size,Remaining,Kinds,Sure,Band,Entities,Last}`) at mint time for the between-rounds summary.
  **The breather (`breathers.go`)** is the one card in a round that asks nothing: `BreatherStore.PickBreathers`
  (a `DISTINCT ON (decade)` over `photos` × `user_ratings`/`user_favorites`, rating ≥ 4 or favourited, newest
  era first, the library's own visibility rules applied) yields one candidate per era; the round takes its turn
  out of that list, so consecutive rounds show different decades. It is carried **outside** `questions`, typed
  `BreatherKind`, and has **no id the answer endpoint would accept**.
  **The reveal (`answer.go`, `revealFor`)** rides back on a `resultAssigned` face answer only: `people.Store.
  SubjectStats` (one aggregate keyed on `subjects.uid` + `idx_markers_subject_uid`, the same visibility rule
  `listSubjectsSQL` applies) gives the person's photo count and year span, read **after** the write so it
  includes it. A missing subject or a failed read yields no reveal, never an error.
  **The three new checks (`extras.go`) — checking what the machine already acted on.** The first two kinds
  clean up guesses about things nobody had decided; these clean up guesses the machine *wrote*: a coordinate on a
  photo, a pair the detector linked, an assignment somebody made. No other page lists those as questions, which
  is why they sit in a library unnoticed. Each is collected in a bounded, rotating window with its own cursor —
  `placeCursor` over `Reviewer.Pending`, `dupCursor` over `FindGroups(limit,offset)`, `outlierCursor` over
  `OutlierBudget` (default 4) subjects with ≥`outliers.MinMeaningful` markers — and each **degrades to "no
  questions of this kind"** rather than failing a rebuild. **None goes through the tiers**: "the estimator
  guessed Brno" and "these two files are 0.02 apart" are not points on one scale, so each carries its own
  ordering (duplicates by confidence descending, outliers **most suspicious first** via `sortBySuspicion` —
  deliberately the opposite, matching the /outliers page — places in uid order).
  Three restrictions are load-bearing rather than simplifications: a place question needs a **geocoded** place
  (a pair of decimal degrees is not something a human can answer about, and the photo comes back once the
  `places` job catches up); a duplicate question is only ever a **two-member, unconfirmed** group (a "no" is
  recorded as an edge dismissal, and a two-member group with its one edge dismissed disappears for good, whereas
  inside a larger component the detector cannot say which edges are already settled, so the game would re-ask
  forever — and a five-photo group is a "which do I keep?", which is the duplicates page's job); an outlier
  question needs a **marker** (a "no" detaches through the assign state machine, and a face with no marker has
  nothing to detach) and a **meaningful** ranking (below `MinMeaningful` faces every face is equidistant from
  the centroid, so "furthest" is noise and the question an accusation drawn from it). `OutlierThreshold`
  (default 0.5, `review.outlier_threshold`) is where a face is far enough to be worth asking about: two people's
  embeddings sit around 1.0, so half of that is comfortably past "a bad photo of the right person" — which
  matters more here than elsewhere, because asking about ten correct assignments to find one wrong one is how a
  player learns to answer yes without looking.
  **The tiers (`tiers.go`) — the game must mostly be a click on "yes".** Band-only maximises information per
  answer but optimises the wrong quantity: what an evening of clicking buys is **confirmed assignments per
  minute of attention**, and a 90 %-confident candidate answered yes in one click is real work done, merely
  unsurprising — while excluding it by design made *every* question a hard one. So a batch is **`SureShare`
  (default 0.70, `review.sure_share`) from the confident tier** (confidence ≥ `sureFloor()` = `max(SureMin,
  BandMax)`, default 0.80 via `review.sure_min`; the clamp keeps the tiers disjoint, and setting `sure_min` =
  `band_max` leaves no confidence between them unasked) **and the rest from the band** `[BandMin,BandMax)`.
  Below `BandMin` nothing is ever asked — the guess is noise and the question demoralising. The minority of hard
  questions is **load-bearing**: a game that is 95 % yes turns the player into a rubber stamp who stops looking,
  and wrong assignments then enter the library through the very feature meant to clean it. `blend` enforces the
  ratio **positionally** (take from the confident tier while the running share stays ≤ `SureShare`), because a
  batch is a *prefix* of the built queue and a queue that is 70/30 only in aggregate could still open with ten
  hard questions; the measured deviation on a full batch is the per-entity rounding, ±0.15.
  **`Queue(ctx,userUID,source,limit)`**: within a tier the order is that tier's own — the band by **distance
  from its center** (closest to the decision boundary first), the confident tier by **confidence descending**
  (the surest is the cheapest yes) — tie-break a stable id, then `blend`, then `capEntities`; **all five kinds
  are then merged by `interleaveKinds`**, which places each list's i-th question at the exact rational
  (2i+1)/(2·len) and takes the earliest (integer cross-multiplication, ties by the fixed order in `Kinds`, no
  `rand`). That merge — not a quota — is what satisfies "no type may dominate a session", and it does so
  **positionally**: a batch is a *prefix* of the built queue, so a queue that were merely balanced overall could
  still open with twenty duplicate pairs in a row. Every collector stops at `need`, so with k kinds supplying
  material a batch of n holds about n/k of each; a kind that is the only one left fills the batch alone, which
  is the right degradation — an exhausted library must not withhold the work it still has. Labels need `PhotoCount>0`
  **and `ReviewEnabled`** (cap `MaxLabels`, fan-out `errgroup.SetLimit(LabelConcurrency)`, an error on one label
  is logged and skipped).
  **The face scan asks for both tiers in one pass** (`Threshold: 1−BandMin`, **no `MinDistance`**), reversing
  d0d6518's floor — which had been added after a rebuild reached 10.9 GB anon-rss and the host OOM killer took
  the box down. That floor was never the structural bound: `candidates.MaxExemplars`, `MaxCandidates` and the
  **cut to `MaxCandidates` before hydration** are, so the number of full photo records built is fixed whatever
  the window admits (`internal/candidates/memory_test.go` measures exactly this window). One consequence is
  named rather than hidden: `MaxCandidates` keeps the *nearest* survivors, so a subject with more than 500
  confident matches contributes no band candidates — the shape the catch-all-subject bug produced, not a
  healthy library, and the rotation moves past it either way.
  **The per-label switch.** `labels.review_enabled` (migration `0048`, default `TRUE`) takes a label out of the
  game: it produces no questions **and is not scanned**, which is half the point — a label search is a
  per-member kNN fan-out, so a label nobody wants to be asked about must not cost a rebuild anything either.
  It is dropped in `labelPlan`, before the plan, and dropped from the label total too, so a library whose every
  label is switched off honestly reports `no_labels`. Subjects have no equivalent flag. The switch is about the
  label as a whole; "not this photo for this label" stays a per-photo `internal/feedback` rejection.
  **Photos hidden from the library** (`photos.hidden_from_library`) produce no question of either kind: the cut
  is made in `personQuestions`/`labelResultQuestions`, not in the searches, because hiding is a statement about
  browsing rather than about the data — the face is still detected and its vector still indexed, and a
  candidate search that skipped hidden photos would quietly weaken everything else built on it. The game *is* a
  browse, and a scanned document is exactly what nobody wants twenty questions about.
  **Variety (`variety.go`) — the game must not be an interrogation.** Informativeness alone let a single label
  that matches half the library own a whole batch; the measurement (`longestEntityRun`, `countEntities`, logged
  per rebuild at debug as `review: queue rebuilt`) on the reproduction fixture was **19 of 20 questions about one
  label, 11 of them in a row, 2 entities** → now **8 entities, longest run 2**. Two rules, both deterministic
  reorderings of the informativeness order (no `rand`, so a rebuild over an unchanged library and unchanged
  cursors is still byte-for-byte reproducible): (1) **`MaxPerEntity`** (default 4, config `review.max_per_entity`)
  — `keepBest` orders one subject's/label's questions **per tier** and keeps only its share of each, applied
  **inside** `personQuestions`/`labelResultQuestions`, and `capEntities` then enforces the share **across** the
  blended sequence so an entity with material in both tiers cannot claim twice the allowance; what a scan counts
  toward "enough" is `batchShare` (the entity's material capped at the share), so that a scan stopping at `need`
  yields a batch of `need` rather than one `capEntities` cuts back. That makes "I have `need` questions, stop"
  mean *enough from enough different entities* — filling a batch of 20 therefore visits ≥ 5 people/labels instead of stopping
  at the first prolific one (a rebuild spends more of its `FaceBudget`/`LabelBudget`; that is the price of the
  variety and the budgets still bound it); (2) **`spread(questions, maxSameEntityRun)`** (`maxSameEntityRun` = 2,
  a constant — a game property, not an ops trade-off) — a greedy that always takes the **most informative
  question left** whose entity was not just asked about twice in a row, falling back to it only when nothing
  else remains (a one-subject library stays playable). It runs **per source before `interleave`**: interleaving
  only inserts the other kind between two of the same kind and a face and a label are never the same entity, so
  bounding the sources bounds the queue. `questionEntity` keys on kind+uid, so a subject and a label sharing a
  uid never collide. `capEntities` runs **once more over the merged pool** (`rebuild`), because a face question
  and an outlier question about one person are two sources and one entity: capping each source alone let one
  person claim twice the share, which is what allowed a pool a round could not be mixed out of.
  **The source is the player's choice (`source.go`)** — `SourceBoth` (default), `SourcePeople`, `SourceLabels`;
  the three new checks ride with `SourceBoth` alone (`wantsChecks`): "people" and "labels" are promises about
  what the game will ask, and a place or duplicate question breaks either one, while a six-way toggle would
  spend the game's simplest control on question types that run out quickly.
  `ParseSource` maps `?source=` (empty → both, other → `ErrInvalidSource` → 400), `orBoth` folds an unknown
  value from a non-HTTP caller back to the default. It travels **into `collect`**, not onto its result: a
  labels-only rebuild never calls `Sweeper.Scan` and a people-only one never calls `Expander.Label`. That is
  the point of the knob — the scans **are** the cost of a rebuild (a subject sweep hydrates a full photo record
  per match), so filtering afterwards would spend the whole price of a batch on questions nobody asked for.
  `QueueResult.Source` echoes what was applied.
  The queue is **cached
  per user _and per source_** (`CacheTTL`) and the session holds `answered`/`skipped` sets + a counter (in-memory,
  idle-pruned after 12 h; the shelf is session-scoped, while a skip about a *person* also goes to the persisted
  memory above — it is still never a "no"). Those sets
  span all sources (a skip is about the question, not about the toggle), but the cached queue does not:
  `needsRebuild` compares `sess.source`, so a **switch always rebuilds** — a warm cache handing back the
  questions the player just turned off is indistinguishable from a broken toggle. An empty library → `reason:
  "no_people_no_labels"`, or the single enabled kind's own reason when the shares narrow the game to one
  (`soleKindReason`: a faces-only game says `"no_people"`, not "no people and no labels"); an empty **chosen**
  source → `"no_people"`/`"no_labels"` (`reasonFor`; only for a
  restricted source, because the unscanned side's total is 0 by construction, and never after a
  degraded rebuild), neither tier producing anything → `"no_candidates"` (all non-error).
  **Infinite means degrading, not stopping.** Running out of one tier fills from the other (both scans see the
  same material, so this is automatic), and a round that came back empty **rotates to the next window and tries
  again** — `collectRotating`, up to `maxRebuildRounds` (3) **inside the one `BuildTimeout`**, stopping early
  when a round found something, was cut short, or the library holds no source to rotate through. An empty
  *window* is not an empty library, and only a genuinely empty one may report so.
  **A rebuild is bounded — the game asks one question at a time, so it must never cost a library-wide
  work list** (it did: 250 s for 105 subjects on production, see `docs/PERF.md` §3). Faces go through
  `Sweeper.Scan` with `Window{Offset: cursor, Budget: FaceBudget}` (default **24** subjects — 8 until the kind
  shares arrived, raised because most subjects have nothing unassigned left and a window of eight regularly
  produced candidates from one or two people; the dropped label scan pays for it, measured 375 ms → 141 ms per
  cold rebuild against +26 ms in the worst unproductive window), labels through a
  `LabelConcurrency`-sized chunked loop over a rotating window of `LabelBudget` labels (default 6); both
  stop as soon as the batch holds `limit` candidates, and `BuildTimeout` (default 15 s) caps the whole
  rebuild — a deadline serves a **partial** queue with a logged warning instead of a 500 and never reports
  `no_people_no_labels` (a timed-out scan cannot prove the library is empty), while a caller's own
  cancellation still propagates. Two **instance-wide cursors** (`faceCursor`/`labelCursor`, own mutex)
  advance by what each rebuild scanned, so successive rebuilds rotate through the library; a **dry queue
  rebuilds immediately** instead of waiting out `CacheTTL`, which is what makes the rotation reach everyone.
  A built queue is also **capped at `maxQueued` (500) questions** (`capQueue`): a `Question` carries a whole
  photo record and the queue is cached per user until the session is pruned, so an uncapped one pins photo
  rows in memory for half a day; what is dropped is not lost, since the cursors have moved on and the next
  rebuild covers new ground. With the default `MaxPerEntity` the per-entity share is the tighter of the two
  bounds (budgets × share ≪ 500) and `capQueue` is the backstop for a wide `max_per_entity`.
  The library-wide subject/label totals still come from the full listings, so the reason codes stay exact;
  content, ordering, exclusions and the HTTP shape are unchanged. **`Answer(ctx,userUID,
  questionID,answer,meta)`**: the id is **content-derived** (`face:<photo>:<idx>:<subject>` /
  `label:<photo>:<label>`) → the endpoint is stateless; a yes on a face **re-reads** the current face
  row (`FacesByKeys`) and derives the action (marker → `assign_person`, otherwise `create_marker` with the stored
  bbox; a face already carrying a subject → short-circuit, no duplicate marker), a yes on a label
  `AttachLabelAudited` (idempotent upsert), a no → `RejectFace`/`RejectLabel` (permanent, idempotent,
  audited in the mutation's transaction); a vanished target (`ErrPhotoNotFound`/`ErrMarkerNotFound`/
  `ErrSubjectNotFound`/`ErrLabelNotFound`/`ErrTargetNotFound`) → `result:"gone"`, not an error; invalid
  input → `ErrInvalidQuestion`/`ErrInvalidAnswer`.
  **The new kinds' answers** (`answer.go`, all through paths that already exist elsewhere): `place` yes →
  `Reviewer.Accept` (coordinates kept, `estimate`→`manual`), no → `Reviewer.Reject` (coordinates cleared,
  `manual` tombstone left); `duplicate` yes → `ConfirmDuplicate`, no → `DismissDuplicate` — **the game NEVER
  merges**, because merging archives copies and a game played at one keypress per second is the last place a
  photo should be able to disappear from; `outlier` yes → `ConfirmFace` (the same set `/outliers` excludes, so
  the two views agree without either knowing about the other), no → `unassign_person` through the assign state
  machine, re-reading the marker so a face detached meanwhile resolves to `gone` and leaving the marker itself
  intact. Ids: `place:<photo>` (a photo has one location, so there is nothing else to key on),
  `duplicate:<a>:<b>` **normalised smaller-uid-first** (one pair must be one question however a scan names it,
  or a session asks it twice) and `outlier:<photo>:<idx>:<subject>` (the same shape as a face question — it
  identifies the same thing, only the direction of the doubt differs, which is also why `questionEntity` gives
  them one entity key: four of each would be eight questions about one person). Unit tests with fakes (band, ordering, interleaving,
  determinism, cache TTL, skip, idempotence, gone) plus `queue_bound_test.go` (the bounded-work property
  over synthetic libraries of 10/105/1000 named subjects at the production exemplar ratio, rotation,
  early stop, dry-queue rebuild, deadline degradation) and `variety_test.go` (**`TestQueue_monotonyBaseline`
  runs the pre-fix pipeline — order + interleave, share out of reach, no spread — over the same fixture and
  fails if it stops reproducing the complaint**, then: run cap, per-entity share, ≥ 5 entities per batch,
  every question still inside one of the two tiers, reproducibility, `spread`/`longestEntityRun` unit tables)
  and `extras_test.go` (the merge holding one of every kind in every five-long window, its degradation to the one
  kind left, its reproducibility, the id round trip and the invalid ids per kind, `pairConfidence` taking the
  stronger of the two incommensurable signals, `pairGroups` keeping only unjudged pairs, the outlier order being
  the reverse of the duplicate one, the shared face/outlier entity key, `wantsChecks`, and the pixel box
  projection over a rotated photo)
  and `source_test.go` (`ParseSource`, a restricted queue **not calling** the other source's search at all,
  an unknown source falling back to both, a switch rebuilding inside a warm `CacheTTL`, the per-source empty
  reasons, a skip holding across a switch)
  and `tiers_test.go` (the measured mix within a stated ±0.15, the mix holding in every prefix, a configurable
  `SureShare`, either tier exhausted degrading to the other, an empty window rotating while a genuinely empty
  library does not, surest-first ordering, the per-entity share counting both tiers together, a switched-off
  label neither asked about nor searched, plus `blend`/`capEntities`/`tierOf`/fallback unit tables)
  and `kinds_test.go` (the faces-only default, a share of zero switching a kind off and never being asked,
  relative weights normalising, a 95/5 game landing on 5 labels in 100 slots, `presentIn` ignoring kinds the
  pool lacks, and **the run limit holding over a pool one subject dominates** — the reported defect, asserted
  from both pool orders)
  and `skips_test.go` (two skips forgiven, the third muting for the cooldown, every further skip doubling the
  wait and the doubling capped, a skipped photo silent for good while a photo added after the mute is not,
  outlier questions covered by a person's mute and label questions not, only the person-naming kinds recorded,
  and a skip still counting when the memory write fails),
  integration tests over real
  sweep+candidates+expand+facematch+feedback+DB, incl. `queue_scale_integration_test.go` (105 named
  subjects, an instrumented face store counting the kNN queries, and a bounded-vs-unbounded content
  comparison), `skips_integration_test.go` (every case starting a **fresh service** between the skips and the
  queue, i.e. a restart: three skips muting a person, the mute surviving that restart while another player's
  queue is untouched, a skip writing neither a `face_rejections` row nor an audit entry nor an assignment, a
  photo imported after the mute still being asked about, the post-cooldown retry landing only on faces never
  shown before, and a fourth skip re-muting for twice as long) and `extras_integration_test.go` (each new kind's yes and no over real stores, asserting the
  **whole** effect of one answer and not merely the intended one: a place accept keeps the coordinates and
  clears the estimate without touching the neighbouring metadata, a reject leaves the tombstone that keeps the
  photo out of the backfill's candidate set, a duplicate yes confirms **and archives nothing**, a duplicate no
  dismisses and the pair does not come back from a cold service, an outlier no detaches the person while the
  marker, the face and the person's other three assignments survive, an outlier yes stops the face surfacing on
  the /outliers page too — plus the mixed batch holding every kind with none past half of it, a people-only
  queue serving none of the three, and the place verdict's audit row carrying `via:review`)
  and the three selections driven over one real library in one session (plus a people-only
  queue on a labels-only library reporting `no_people`), the tier mix over **planted unassigned faces**
  (production's are almost all on the nameless catch-all subject, so the face half cannot be observed there
  at all), the confident tier exhausted degrading to the band, and the label switch driven off and back on. Additionally **`LeaderboardStore`** (`NewLeaderboardStore(
  pool)`, separate from `Service` — read-only) aggregates a **review leaderboard** directly from `audit_log`: per
  `actor_uid` it counts decisions marked `details.via = "review"` — yes = `face.assign`+`label.attach`+
  `face.confirm`+`location.confirm`+`duplicate.confirm`,
  no = `face.reject`+`label.reject`+`face.unassign`+`location.reject`+`duplicate.dismiss` — the buckets come
  from **`audit.ReviewYesActions()`/`ReviewNoActions()`**, one shared list also read by `internal/auditapi`, so a
  new question type becomes countable the moment its write path is wired instead of the next time somebody
  remembers the file; a skip writes nothing, so it isn't counted — with the windows `WindowAllTime`/
  `WindowWeek`/`WindowToday` (`ParseWindow` maps `?window=`, empty → all, other → `ErrInvalidWindow`;
  `windowCutoff` computes the bound from `created_at`), a NULL actor is skipped, ordered total desc → yes desc →
  `display_name` (fallback to `username`), each row carrying **`StreakDays`** — the player's current run of
  consecutive days with a decision, ending today *or yesterday* (the day is not over yet), computed by a second
  query that is deliberately **not** narrowed by the window. `streak.go` reduces the same `via:review` rows to
  the distinct hours each user was active in over `streakLookback` (400 days) and folds them into local days in
  Go (`currentStreak`/`distinctDays`/`startOfDay`), which is where the day arithmetic is testable; the hour
  reduction is exact for any zone whose offset from UTC is a whole number of hours. **No new table** — the
  audit trail already holds it. So that a review face confirmation also lands in the leaderboard,
  `applyFaceYes` sends `AssignRequest.Via = "review"` into facematch `Service.Apply` (until now the only
  unmarked of the four actions). Unit tests `ParseWindow`/`windowCutoff` + an integration test of windows, the yes/no
  split, ordering, NULL-actor/non-review exclusion; for the partial index see migration `0037`),
  `internal/reviewapi/`
  (an HTTP API over the review game: the `Service` interface (satisfied by `*review.Service`) and `Leaderboarder`
  (satisfied by `*review.LeaderboardStore`), `NewAPI(Config{Service,Leaderboard,RequireWrite,RequireAuth})`
  (nil guards → pass-through) + `RegisterRoutes` mounts `GET /review/queue` and `POST /review/answer`
  behind **`RequireWrite`** (editor/admin — they mutate the library) and `GET /review/leaderboard` behind **`RequireAuth`**
  (only aggregates → any logged-in user, even a viewer); the queue reads `?source=` (empty → `both`,
  `review.ParseSource` → 400 on anything else) and `?limit=` (empty → default,
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
  `UpdateSubjectAudited`/`DeleteSubjectAudited`/`MergeSubjectsAudited`/`ListPhotoUIDsBySubject` — each mutation
  takes an `audit.Entry`
  built in `auditEntry` (`subject.create`/`update`/`delete`/`merge`, actor from the auth context, details
  name/type; `DELETE` first loads the subject for the details and a clean 404)) and `PhotoStore`
  (`photos.Store.ListByUIDs`)
  → unit-testable with fakes without a DB; `NewAPI(Config{Subjects,Photos,RequireAuth,RequireWrite})`+
  `RegisterRoutes` mounts **flat** paths (not a mounted subrouter, so they coexist with
  `outlierapi`'s `GET /subjects/{uid}/outliers` without a chi Mount conflict): `GET /subjects`
  (RequireAuth, `{subjects:[SubjectCount]}` with marker **and** photo counts), `POST /subjects` (RequireWrite,
  create → 201, name/type validation), `GET /subjects/{uid}` (RequireAuth), `PATCH /subjects/{uid}`
  (RequireWrite, editing name/type/favorite/private/notes/cover_photo_uid), `DELETE /subjects/{uid}`
  (RequireWrite → 204), `POST /subjects/{uid}/merge` (RequireWrite, `{keeper_uid}` → `people.MergeResult`;
  the **path** subject is the one merged away and deleted. `handleMerge` loads **both** subjects first, so a
  missing one is a 404 before any mutation and the audit details keep the source's name — the source is gone
  by the time anyone reads the trail, and a merge has no undo. Merging a subject into itself is refused here,
  before the store, so the answer is the same 400 whether or not the uid exists),
  `GET /subjects/{uid}/photos` (RequireAuth, a paginated gallery of the subject's photos
  `{photos,total,limit,offset,next_offset}` — `ListPhotoUIDsBySubject` (distinct non-invalid
  markers, non-archived, newest-first) → page → `ListByUIDs` → reorder by the uid order); body
  decode `DisallowUnknownFields` + 1 MiB limit + empty name (or empty `keeper_uid`) → 400; sentinels mapped
  `ErrSubjectNotFound`→404/`ErrInvalidType`+`ErrMergeIntoSelf`→400; mounted by the eighth `server.WithAPI`
  (`buildPeopleAPI` in `cmd/kukatko/people.go`)), `internal/organize/`
  (the DB layer for **organization** — albums, labels, **per-user favorites** (replacing the global
  the old global `photos.favorite`) and **per-user ratings** (0–5 stars + a personal flag none/pick/reject/eye);
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
  `AlbumCount` + `CoverUID`/`CoverUIDs`/`TakenFrom`/`TakenTo` — all computed **in one SQL and in one pass**, without a
  migration: a single LEFT JOIN chain `albums → album_photos → photos` restricted to the visible members
  (`archived_at IS NULL AND (stack_uid IS NULL OR stack_primary)`) carries every derived column —
  `photo_count = COUNT(p.uid)`, `MIN`/`MAX(taken_at)`, and the fallback cover as
  `(array_agg(p.uid ORDER BY p.taken_at DESC NULLS LAST, p.uid) FILTER (WHERE p.uid IS NOT NULL))[1]`;
  `CoverUID = COALESCE(cover_photo_uid, fallback)` → a manually chosen cover wins, otherwise the newest
  **visible** photo, deterministically the same on every query. A hidden photo (archived, or a non-primary
  stack member) joins as a NULL row, so it neither counts, nor supplies the cover, nor shifts the range; an
  undated photo can be the cover, but doesn't enter the range.
  `CoverUIDs` is the head of that very same array — `[1:$1]` instead of `[1]`, with `CoverCandidates` (8,
  passed as the statement's only parameter so the number lives in one place) as the bound and `'{}'` for an
  empty album. One cover per album cannot tell overlapping albums apart (they share their newest photo), so
  the index draws a collage or steps to the next candidate; a hand-picked cover stays **out** of the list —
  it answers a different question, and a client must not dilute it into one cell of four. The two
  projections spell the aggregate identically, so the executor computes it once: the candidates cost
  nothing beyond the payload (measured unchanged, 210 ms / 1 024 blocks on the plan-test fixture). The cover came from a `LEFT JOIN LATERAL …
  ORDER BY taken_at DESC LIMIT 1` until 2026-08-02: the planner serves such a per-album `LIMIT 1` by walking
  the **global** `photos.taken_at` order, which cost 17.3M buffer hits and 33 s on the production library —
  **never pick a per-group row with a correlated `ORDER BY … LIMIT 1` here**, see `docs/PERF.md` §
  "The album index" and the plan-budget regression test `albums_plan_integration_test.go`)/
  `SearchAlbums(q,limit)` (accent/case-insensitive ILIKE over `immutable_unaccent(title/description)`,
  with counts → `[]AlbumCount`, cap limit — the basis of `globalsearchapi`)/
  `DeleteAlbum`/`AddPhoto` (idempotent, no position — `ON CONFLICT DO NOTHING`)/`RemovePhoto`
  (idempotent)/`SetCover` (set/clear cover)/`ListPhotoUIDs`
  (chronologically: `COALESCE(taken_at, created_at), photo_uid` via a JOIN on `photos`)/`AlbumsForPhoto`
  (one photo's albums, with titles, for the photo detail chips)/`AlbumUIDsForPhotos(uids)` (the batch
  reverse of it over `idx_album_photos_photo_uid`: album **uids only**, `map[photoUID][]albumUID`, a photo in
  no album simply absent — the review mixer asks "were these two photos in the same album?" for a whole pool at
  once and needs no titles); **labels** `CreateLabel`/`GetLabelByUID`/`GetLabelBySlug`/`UpdateLabel`
  (re-slug; also writes `LabelUpdate.ReviewEnabled` **unconditionally**, so a caller meaning "leave it alone"
  carries the current value across — `internal/organizeapi` does exactly that for a body omitting the field)/
  `ListLabels` (with counts, ordered priority DESC, plus each label's `CoverUID` from one batched
  `LabelCovers` call over the whole listing)/`SearchLabels(q,limit)` (accent/case-insensitive
  ILIKE over `immutable_unaccent(name)`, with counts, cap limit — the basis of `globalsearchapi`;
  leaves `CoverUID` nil, the search endpoint reads the covers itself because it also needs their file
  hashes)/`DeleteLabel`/
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
  `Label.ReviewEnabled` (migration `0048`, `labels.review_enabled`, default `TRUE`) is whether the review game
  may ask about the label; it is **never read on the way in** — `insertLabelSQL` omits the column so the DB
  default governs and a zero-valued struct literal can never create a label the game silently ignores, and
  switching it off is an explicit `UpdateLabel`. Every label read path carries it (`labelColumns`, the two
  count projections, `LabelsForPhoto`, `PhotoLabelsForPhoto`);
  **covers** (`covers.go`) `AlbumCovers(uids)`/`LabelCovers(uids)` → `map[uid]Cover{PhotoUID,FileHash}`:
  the photo standing for each named album/label, resolved for the **whole batch in one query** (a
  `DISTINCT ON` over the batch's membership rows, never a correlated `ORDER BY … LIMIT 1` per entity — the
  same trap `listAlbumsSQL` documents), ranked newest-first by `COALESCE(taken_at, created_at)` with the uid
  breaking ties and restricted to the photos the entity's own grid shows (not archived, not
  `hidden_from_library`, not a non-primary stack member); an album's hand-picked `cover_photo_uid` wins over
  the derivation and is honoured whatever state its photo is in, a label has none to pick. An entity with
  nothing to show is **absent from the map**, and the file hash rides along so a caller can address the
  thumbnail without fetching the photo row (`internal/mediaurl`, `globalsearchapi`, the labels index);
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
  and writes the audit only for the successful attempt); the non-audited variants remain for system callers
  that have no actor)), `internal/organizeapi/`
  (a read/curation HTTP API over albums and labels — the basis of the Albums/Labels UI: the interfaces `AlbumStore`/
  `LabelStore` (subsets of `organize.Store`) → unit-testable with fakes without a DB;
  `NewAPI(Config{Albums,Labels,RequireAuth,RequireWrite})`+`RegisterRoutes` mounts two
  subrouters: **albums** `GET /albums` (RequireAuth, `{albums:[AlbumSummary]}` — counts, the effective
  `cover_uid`, the further covers `cover_uids` and the range `taken_from`/`taken_to`),
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
  `ErrInvalidType`/`ErrInvalidSource`→400; **every 5xx goes through `fail`**, which logs the discarded store
  error (`slog.ErrorContext`, message `organizeapi: <what failed>`) before writing the generic client body —
  the access log otherwise records only `status:500` and the cause reaches nobody; 4xx is deliberately not
  logged (the caller is already told what was wrong); **browsing an album's/label's photos has no own endpoint** —
  it goes through the shared `GET /photos` scoped `?album={uid}`/`?label={uid}` (see `photos.ListParams`
  `AlbumUID`/`LabelUID` + `photoapi` `parseListParams`); mounted by another `server.WithAPI`
  (`buildOrganizeAPI` in `cmd/kukatko/organize.go`, sharing one `organize.Store` for both albums and labels)),
  `internal/feedback/`
  (the DB layer for **persisted rejections** (negative feedback) — a permanent user "no" to a face↔subject
  or photo↔label guess; it closes the gap where a rejection wasn't kept and the same
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
  disagreement doesn't change — without persistence the same pair would be offered forever).
  **Duplicate confirmations** (`duplicateconfirmations.go`, table `duplicate_confirmations`, migration
  `0054_duplicate_confirmations.sql`) are the dismissal's **positive mirror**: "yes, this really IS the same
  photo twice", written by the review game's duplicate check. The same shape and rules as the dismissal, down to
  the unordered pair and the `CHECK (photo_uid COLLATE "C" < other_uid COLLATE "C")` — carried **from the start**
  here, unlike `0034`, which needed `0038` to repair it. `ConfirmDuplicate`/`UnconfirmDuplicate` (idempotent
  audited insert/delete, actions `duplicate.confirm`/`duplicate.unconfirm`), `IsDuplicateConfirmed`, bulk
  `ConfirmedDuplicatePairs()`. **Why it exists:** the duplicates page only ever offered one way to agree —
  merging, which archives copies and is not something a curator does one pair at a time from a game — so
  agreement evaporated and a group somebody had already looked at was indistinguishable from one nobody had.
  It changes **nothing** about detection: a confirmed pair is still detected, still shown, still mergeable; what
  it buys is `Group.Confirmed` and the ranking that puts it first. Merging stays an explicit, separate act.
  Both pair opinions share their machinery (`pairs.go`: `photoPair`, the `pairOpinion` statement bundle,
  `recordPair`/`removePair`/`hasPair`/`listPairs` and `checkPairKey`, which is where the ErrEmptyKey-vs-ErrSamePhoto
  distinction and the smaller-uid-first normalisation now live **once**) — the two tables are the same shape down
  to the CHECK, and writing the four operations twice is exactly how the second one drifts from the first. What
  stays duplicated is the documented API surface of each (its key type, its sentinels, the paragraph saying why
  it mutates nothing), carrying a justified `//nolint:dupl`: collapsing two documented APIs into one generic pair
  would cost more in readability than the repetition does.
  **Repeated-marker dismissals** (`markerdismissals.go`, table `duplicate_marker_dismissals`, migration
  `0050_duplicate_marker_dismissals.sql`) are a fourth kind: "this person **really is** marked more than once on
  this photo" — a double exposure, a mirror, a photo of a photo, a face on a poster behind its owner. Keyed by
  the **(photo, subject) pair**, which is exactly the group `internal/dupmarkers` shows; deliberately **not** by
  the markers, whose uids come and go when a photo is re-detected and whose replacement would resurrect a
  settled group. `UNIQUE (photo_uid, subject_uid)`, both FKs `ON DELETE CASCADE`, `dismissed_by` FK users
  `ON DELETE SET NULL`. `DismissDuplicateMarkers`/`UndismissDuplicateMarkers` (idempotent audited
  insert/delete, actions `duplicate_marker.dismiss`/`duplicate_marker.undismiss`),
  `IsDuplicateMarkersDismissed`, bulk `DismissedDuplicateMarkerGroups()`
  (→ `[]DuplicateMarkerDismissalKey`, the whole table in one query — the listing recomputes its groups in one
  pass and needs the whole exclusion set up front). **Why it exists:** the repeated-marker listing is derived
  state too, so without this the same false alarm would be re-offered on every reload and a curator would keep
  clicking past it),
  `internal/feedbackapi/`
  (an HTTP API over rejections — the `Store` interface (a subset of `feedback.Store`) → unit-testable with fakes;
  `NewAPI(Config{Store,RequireWrite})`+`RegisterRoutes` mounts the subrouter `/feedback`:
  `POST /feedback/face-rejections` `{photo_uid,face_index,subject_uid}` (RequireWrite → 204),
  `DELETE /feedback/face-rejections` (undo → 204), `POST /feedback/label-rejections`
  `{photo_uid,label_uid}` (→ 204), `DELETE /feedback/label-rejections` (→ 204) — DELETE carries a body too
  (like label-detach); body decode `DisallowUnknownFields` + 64 KiB, a missing id → 400, a negative
  `face_index` → 400; **each mutation writes an audit record in the same transaction** (the actor from
  `auth.UserFromContext` + `audit.FromRequest`, actions `face.reject`/`face.unreject`/`label.reject`/
  `label.unreject`, `entry.ActorUID` is also `rejected_by`); the confirmation, duplicate-dismissal and
  **repeated-marker dismissal** routes ride the same subrouter and the same rules —
  `POST`/`DELETE /feedback/duplicate-marker-dismissals` `{photo_uid,subject_uid}` → 204 (actions
  `duplicate_marker.dismiss`/`duplicate_marker.undismiss`, targeting the subject with the photo in the details);
  `ErrTargetNotFound`→404, `ErrEmptyKey`→400,
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
  mounted by `server.WithAPI` (`buildSavedSearchAPI` in `cmd/kukatko/savedsearch.go`)),
  `internal/searchhistory/`
  (the DB layer for **each user's recent searches** — the short ordered list of what somebody actually
  searched for, kept server-side so a query composed on a laptop is offered on the phone; the
  `search_history` table in migration `0056_search_history.sql`: `user_uid` FK users `ON DELETE CASCADE`,
  `query TEXT NOT NULL CHECK (char_length BETWEEN 1 AND 500)`, `searched_at TIMESTAMPTZ DEFAULT now()`,
  **PK `(user_uid, query)`** (which is what makes recording an idempotent upsert) + the index
  `(user_uid, searched_at DESC)`; `MaxEntries = 20`, `MaxQueryLength = 500`, `Normalize(query)` (trims,
  caps at 500 **characters** and trims what the cut left, `""` for a whitespace-only query — it deliberately
  does *not* fold case or collapse inner whitespace: the string is handed back to the search box verbatim,
  and collapsing spaces would change `title:"a  b"`), `Entry{Query,SearchedAt}`;
  `Store` = `NewStore(pool)`: `Record(ctx,userUID,query)` (normalize → `ErrEmptyQuery` for a blank one →
  `INSERT … ON CONFLICT (user_uid, query) DO UPDATE SET searched_at = now()` **then** the prune, both in
  **one transaction** so no reader sees a history past the cap; the prune keeps the newest `MaxEntries`
  identified **by query** rather than by timestamp, since the PK is unique per user while two rows written in
  one transaction share `now()`), `List(ctx,userUID)` (newest first, `LIMIT MaxEntries`, tie-broken by query —
  the same total order the prune uses), `Clear(ctx,userUID)` (idempotent);
  **deduplication is on the exact trimmed text** — "Praha" and "praha" are two entries, because folding
  them would mean choosing which spelling the user gets handed back; there is **no retention job**, the
  per-write prune bounds the table by the number of accounts), `internal/searchhistoryapi/`
  (the HTTP API over it: the `Store` interface (a subset of `searchhistory.Store`) → unit-testable with a fake;
  `NewAPI(Config{Store,RequireAuth})`+`RegisterRoutes` mounts `/search-history` **all behind `RequireAuth`**
  and **scoped to the logged-in user** (`auth.UserFromContext`) — there is no path parameter and no owner in
  any body, so there is nothing to tamper with: `GET` → `{searches:[{query,searched_at}]}` (an empty history is
  `[]`, never `null`), `POST {query}` → **204** (no body: the client already knows what it searched for, and the
  refreshed list is only wanted when the dropdown next opens, which is a GET), `DELETE` → 204 (idempotent);
  a blank query → 400 both in `decodeRecord` and via the store's `ErrEmptyQuery`, the body
  `DisallowUnknownFields` + an 8 KiB limit; **not audited** — a private per-user convenience list is not a
  library mutation; mounted by `server.WithAPI` (`buildSearchHistoryAPI` in `cmd/kukatko/searchhistory.go`)),
  `internal/announcement/`
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
  `cmd/kukatko/announcement.go`)), `internal/whatsnew/`
  (**the returning-reader digest** behind the "what's new since your last visit" panel on the library home —
  a shared family library otherwise makes somebody else's evening of uploading invisible to the next person
  who opens the app; **read-only over the catalogue**, its single write is the visit bookkeeping on the
  caller's own `users` row. **What a visit is** (migration `0053_user_visits.sql`, two nullable
  `TIMESTAMPTZ` columns on `users`): `last_seen_at` is a heartbeat stamped to now on **every** summary read,
  `visit_reference_at` is the digest's "since" and moves **only** when a new visit begins — a gap of at least
  `VisitGap` (**6 h**) between two reads — taking the previous `last_seen_at` on that transition. Within one
  visit the reference does not move however often the page reloads (the "a refresh must not reset the panel"
  requirement); both NULL on an account that never read a summary, and a NULL reference = **no digest**
  (a new account does not want its whole library announced). Neither column touches `updated_at` — being here
  is not a profile edit. Since only this read stamps the heartbeat, "inactivity" means precisely
  "the library home was not opened". `Store` = `NewStore(pool)` (+ `WithGap(d)`, a copy with a shorter
  threshold **for tests** — production never calls it): `Summary(ctx, userUID, now)` → `Summary{HasNews,
  Since, Photos, MinePhotos, Comments, Albums[]{UID,Title}, AlbumCount, People[]{UID,Name}, PersonCount}`, the sentinel
  `ErrUserNotFound` when the account vanished mid-request. It rotates the visit in **one** atomic
  `UPDATE … RETURNING` (two tabs loading at the same instant cannot both rotate: the second waits on the row
  lock and then sees a `last_seen_at` that is already now), then counts. **What counts:** photos under the
  library grid's own base filter (`archived_at IS NULL AND (stack_uid IS NULL OR stack_primary) AND NOT
  hidden_from_library` — so the number printed equals the number of tiles the link opens), live comments
  (`deleted_at IS NULL`), **hand-curated** albums only (`type = 'album'`; an import mints folder/moment/month
  groupings by the hundred) and **named** subjects only (`name <> ''`). **`MinePhotos`** is the one line
  about the reader rather than about the library: the same photo predicate narrowed by a non-invalid marker
  naming the account's linked person, read out of the very `UPDATE … RETURNING` that stamps the visit (the
  row is already being written, so the link costs no second round trip) and skipped entirely for an
  unlinked account or a visit with no new photos at all. Being a strict subset of `Photos` it can never be
  the only news, so `counts.empty()` — and therefore `HasNews` — ignores it. Albums and people are capped at
  `MaxItems` (**6**) links while the counts report the true totals. A `HasNews false` (zero-value) `Summary`
  covers both "first visit" and "nothing happened" so the client branches on one flag. Every count is an
  indexed range over a creation timestamp (`idx_albums_created_at`, `idx_subjects_created_at`,
  `idx_photo_comments_created_at` from 0053; `idx_photos_live_created_at` from 0015)), `internal/whatsnewapi/`
  (a one-route HTTP API over it: the `Summarizer` interface (a subset of `whatsnew.Store`) → unit-testable
  with a fake; `NewAPI(Config{Store,RequireAuth,Now})` (`Now` defaults to `time.Now`, injectable so tests move
  a visit forward without waiting) + `RegisterRoutes` mounts `GET /whats-new` behind **`RequireAuth`** —
  every role including a **viewer**, since learning what the family added is not a curation power.
  **A GET that writes** on purpose: reading the digest is what stamps the visit, and a separate POST the
  client must remember to send would leave the reference wrong exactly when the tab was closed. Returns
  **200** with the `Summary` in every non-failure case (`{"has_news":false}` for a first visit, an empty visit
  **and** a `whatsnew.ErrUserNotFound`) — never a 404 or a 204, so the client parses one shape; any other
  store error → 500 (logged). Mounted by `server.WithAPI` (`buildWhatsNewAPI` in `cmd/kukatko/whatsnew.go`)),
  `internal/globalsearchapi/`
  (a grouped **global search** HTTP API across entities — the basis of the navbar quick-results and the cross-entity section
  of the search page: the small interfaces `Organizer` (`SearchAlbums`/`SearchLabels` + `GetAlbumByUID`/
  `GetLabelByUID` + `AlbumCovers`/`LabelCovers`, satisfied by `organize.Store`), `PeopleSearcher`
  (`SearchSubjects` + `GetSubjectByUID`/`GetMarkerByUID` + `SubjectCovers`, satisfied by
  `people.Store`) and `PhotoSearcher` (`Search` + `GetByUID`/
  `GetByPhotoprismUID`/`GetByPhotoprismAlias`/`ListStackMembers`, satisfied by
  `photos.Store` — reusing the existing fulltext via `ListParams.FullText`) → unit-testable with fakes;
  `NewAPI(Config{Organizer,People,Photos,Limit,RequireAuth})`+`RegisterRoutes` mounts
  `GET /search/global?q=` behind `RequireAuth`: handles each group separately (`SearchAlbums`/`SearchLabels`/
  `SearchSubjects` capped at `Limit`, default `defaultGroupLimit` 8; photos via fulltext with `Limit`),
  returns a grouped envelope `{query, albums:[{uid,title,cover?,thumb_url?,photo_count}],
  labels:[{uid,name,cover?,thumb_url?,photo_count}], people:[{uid,name,cover?,thumb_url?}],
  photos:[…usual photo shape…]}` (each group always a non-nil array); an empty/
  whitespace `q` → 400, a store error → 500; **every entity hit carries its picture** (`covers.go`): after the
  searches, each group is stamped in **one batched cover lookup per group** (`AlbumCovers`/`LabelCovers`/
  `SubjectCovers` — eight hits cost one query each, never one per row; an empty group asks for no uids and so
  touches no database) with `cover` (the photo standing for it) and `thumb_url` (its medallion at
  `thumb.AvatarSize`, minted through `internal/mediaurl` exactly as a photo hit's `thumb_url` is, so a
  published bucket yields a signed edge URL). The pair is set **together or not at all**, and an entity with no
  photo behind it carries neither — the client's cue to draw its own glyph rather than a gap; **the uid branch**
  (`direct.go`): when `query.FindUID` recognises an
  id in `q`, the id is resolved against the one table its prefix names and returned as `direct`
  `{uid,kind,found,target_kind?,target_uid?,title?,photo?,cover?,thumb_url?,states?}` (the same cover pair, read
  with the same batch call asked for one uid, so a pasted id draws the picture a typed name would), and the
  fuzzy fan-out is **skipped**
  (the groups serialise as `[]`) — it replaces the four searches instead of becoming a fifth, since a uid matches
  no title, name or full text anyway; a `mk…` resolves to the photo it sits on, an `st…` to the stack's primary
  (`ListStackMembers` orders it first), a `pt…` through `photos.photoprism_uid` and then the
  `photoprism_aliases` of migration 0046; the lookups are **unscoped** (an archived, hidden, private or
  non-primary stack member resolves) and `states` names which of those it is, so a hit outside the library view
  is labelled rather than merely puzzling; a well-formed id matching nothing → `found:false`, **not** an empty
  result set, and only a store failure is a 500; mounted by `server.WithAPI` (`buildGlobalSearchAPI` in
  `cmd/kukatko/globalsearch.go`, sharing the organize/people/photos store)), `internal/placesapi/`
  (a read-only HTTP API over the reverse-geocoded place hierarchy — the basis of Places browse: the interface
  `Store` (a subset of `photos.Store`: `AggregatePlaces`) → unit-testable with a fake; `NewAPI(Config{
  Store,RequireAuth})`+`RegisterRoutes` mounts `GET /places` behind `RequireAuth`: a hierarchy with counts
  `{places:[{country,count,cover_uid,cities:[{city,count,cover_uid}]}]}` aggregated over non-archived photos
  with place data
  (a country's count includes photos without a city too, cities always an array; ordered count desc/name;
  `cover_uid` is the place's newest visible photo, omitted when there is none — Places is a browse of a photo
  library, so its rows carry pictures), an optional
  `?country=` drills only into the cities of one country; photos without place data are excluded (`photos.Store.
  AggregatePlaces` computes it with one `GROUP BY country, city` joining on `photo_places`). **Browsing a
  locality's photos has no own endpoint** — it goes through the shared `GET /photos` scoped `?country=`/`?city=`
  (`photos.ListParams` `Country`/`City` + `photoapi` `parseListParams`); mounted by `server.WithAPI`
  (`buildPlacesAPI` in `cmd/kukatko/places.go`, aggregation via the photos store over the `photo_places` cache)),
  `internal/audit/`
  (durable audit trail, the `audit_log` table in migration `0012_audit_log.sql` extended in
  `0014_audit_request.sql` with `ip`/`user_agent` + composite index `(target_type, target_uid)`:
  `id BIGSERIAL`, `actor_uid` FK users `ON DELETE SET NULL`, `action`, `target_type`, `target_uid`,
  `details JSONB`, `ip`, `user_agent`, `created_at` (the column names `actor/target/details` =
  the spec terms `user/entity/metadata`); **the key pattern** `Write(ctx, exec, Entry)` writes through
  the `Execer` interface (satisfied by the pool **and** `pgx.Tx`), so the audit row runs in the **same
  transaction** as the mutation — it commits/rolls back with it (ARCHITECTURE §5.1/§11/§12 "audit log
  durable", a fix of the previous system's after-commit gap); `Entry{ActorUID,Action,TargetType,TargetUID,
  Details,IP,UserAgent}` (an empty UID/IP/UA → SQL NULL, nil details → `{}`); **the handler convention**
  `Meta` + `FromRequest(r, actorUID)` (the actor from the auth context, IP from `clientip.FromRequest` — the
  socket peer unless a *trusted* proxy named the client, so the trail records where a request came from and
  not what its sender claimed; UA from the header) → `(Meta).Entry(action, targetType, targetUID, details)` builds
  the rest of the entry; **the `changes` convention for edits** (`changes.go`): `ChangeSet` = `NewChangeSet()` +
  `Add(field, old, new)` (skips unchanged fields via `reflect.DeepEqual`, compares pointers by
  value) → `Map()`/`StampInto(details)` writes under the key `ChangesKey` (`"changes"`) a map
  `{"<field>":{"old":…,"new":…}}` **with only the fields that actually changed** (a nil pointer → JSON `null`);
  every editing path uses it (photo PATCH + MCP `photo.update`, album/label/subject update),
  so the log shows `old caption` → `new caption`; **bulk editing in `internal/bulk` is deliberately
  left out** (one `UPDATE` over many photos without loading the old rows — a SELECT-before-UPDATE would
  double the queries per batch), it keeps its original summary in details; action constants `ActionPhotosBulk`/`ActionPhoto{Update,Edit,Archive,Unarchive,Purge}`/
  `ActionAlbum{Create,Update,Delete}`/`ActionLabel{Create,Update,Delete}`/`ActionFaceAssign`/
  `ActionUser{Create,Update,Disable,Password}`/`ActionAuditPurge`; `Store` = `NewStore(pool)` with `Record(ctx,Entry)`
  (its own connection) and **filtered reads** `List(ctx,Filter)`/`Count(ctx,Filter)` (`Filter{ActorUID,
  TargetType,TargetUID,Action,Since,Until,Limit,Offset}`, newest-first, limit cap 500/default 100)
  for the admin listing; **retention purge** `PurgeOlderThan(ctx, cutoff) (int, error)` = one
  `DELETE FROM audit_log WHERE created_at < $1` over `idx_audit_log_created_at`, returns the number deleted
  (maintainer-only via `internal/maintenanceapi`, action `audit.purge`, it audits itself — the fresh
  purge record survives the purge). **Wired-in in-tx mutations**: bulk (`internal/bulk`) + photo PATCH/archive/unarchive
  via the audited variants `photos.Store.{UpdateMetadata,Archive,Unarchive}Audited`, **permanent purge**
  `photos.Store.DeleteAudited` (`internal/trash` → `photo.purge`, a system actor for the scheduled retention)
  and **user management** `auth.Store.{CreateUser,UpdateUserProfile,SetUserDisabled,SetPasswordHash}Audited`
  (`user.*`) — every mutation + audit in one tx via the shared `rowQuerier`/`mutateAudited` (photos) and
  `inAuditedTx` (auth); further domains (albums/labels/people) follow the same convention), `internal/auditapi/`
  (HTTP API over the audit trail: `NewAPI(Config{Store,RequireAdmin,RequireAuth})`+`RegisterRoutes`
  mounts `GET /audit` behind `RequireAdmin` and `GET /audit/mine` behind `RequireAuth`; `parseFilter` from the query `user`/`entity_type`/`entity_uid`/
  `action`/`via`/`decision`/`since`/`until` (RFC3339)/`limit`/`offset` → `audit.Filter` (an invalid
  time/number/`via`/`decision` → 400), returns `{entries,total,limit,offset,next_offset}` newest-first;
  **`via=review`** → `Filter.ReviewOnly` (the literal `details ->> 'via' = 'review'`, matches the partial
  index 0037), **`decision=yes|no`** → `Filter.Actions` ("Ano" = `face.assign`+`label.attach` / "Ne" =
  `face.reject`+`label.reject`) — the basis of the admin per-user review-decision overview;
  **`handleListMine`** (the own-activity listing behind `RequireAuth`) parses the very same filters, then
  **overwrites `Filter.ActorUID` with the session user's UID unconditionally** (`auth.UserFromContext`) — so on
  every page, under every filter, the query can only reach the caller's own rows, and the system's actor-less
  ones (empty `actor_uid`) match nothing; a `user` parameter naming **somebody else → 403** (refusing beats
  silently rewriting: the caller must not believe they are reading someone else's history), naming oneself is
  accepted; no principal on the context → 401 (fail closed). It is a **separate route rather than a looser guard
  on `/audit`** so the impossibility of reading foreign rows is a property of the route's shape, not of a branch
  a later edit could weaken; `respond` (list+count+`buildResponse`) is shared by both handlers. The narrowed
  records are returned whole, `ip`/`user_agent` included — the caller's own request metadata; read-only — writes go through
  mutation transactions elsewhere; always mounted by the last `server.WithAPI` (`buildAuditAPI` in
  `cmd/kukatko/audit.go`)), `internal/bulk/`
  (bulk metadata editing: `Service` = `NewService(pool, maxBatch)` with `Apply(ctx, actorUID,
  photoUIDs, ops Operations) (Result, error)` — **the whole batch in a single transaction** with an audit
  record; `Operations` = the optional fields `AddAlbums`/`RemoveAlbums`/`AddLabels`/`RemoveLabels`,
  `Title`/`Description *string` (nil=unchanged, ""=clear), **`TakenAt *TakenAt{At,Precision}`**,
  `Location *Location`+`ClearLocation`,
  `Archive`/`Hide`/`Favorite *bool` (`Hide` = `photos.hidden_from_library`, the operation the
  hide-from-library feature actually needs — the real use is fifty document scans at once),
  **`Rating *int` (0–5) + `Flag *string` (none/pick/reject/eye)**;
  `Apply` validates the batch (ErrNoPhotos/ErrNoOperations/
  ErrBatchTooLarge), checks that the albums/labels of the add operations exist (ErrAlbumNotFound/ErrLabelNotFound),
  then per photo: a duplicate uid → `skipped`, a non-existent photo → `error` **without aborting the rest**,
  otherwise it applies and `updated`; its own idempotent SQL (its own tx for atomicity, it does not use
  the organize/photos store methods, which have their own connection); favorites **and ratings** are
  **per-user** (`actorUID`) — the rating/flag upsert + the all-defaults row prune mirror the `organize` store;
  `Result{Results:[{photo_uid,status,error?}],Counts{total,updated,skipped,errored}}`; a real DB
  error rolls the whole batch back; an archive operation additionally calls `photos.LeaveStackTx` for each
  archived photo **in the same transaction**, so archiving a stack's primary does not hide its still-live
  siblings behind the `(stack_uid IS NULL OR stack_primary)` gate (an unarchive leaves stacks untouched);
  a set-taken-date operation writes `taken_at` (`At`, always the **first instant of the stated period in
  UTC**), `taken_at_source='manual'`, `taken_at_precision` and `taken_at_estimated` = "the grain is coarser
  than a day" — an exact date instead lowers that flag and clears `taken_at_note` with it (0029's invariant:
  a note only lives beside a date presented as a guess); it never touches the original file or its EXIF;
  `Summary()` (audit details) + `IsEmpty()`), `internal/bulkapi/`
  (HTTP over `bulk.Service`: the `Service` interface (Apply) — fakeable; `NewAPI(Config{Service,
  RequireWrite})`+`RegisterRoutes` mounts `POST /photos/bulk` behind `RequireWrite`; the body
  `{photo_uids,operations}` via `operationsInput` with **set/clear pairs as separate keys**
  (unambiguous, a `set_*`+`clear_*` / `archive`+`unarchive` conflict → 400), `set_caption`→title,
  **`set_rating` (0–5) / `set_flag` (none/pick/reject/eye)** with validation → 400,
  **`set_taken_at {precision,value}`** where the value's shape follows the precision
  (`day`/`1974-06-14`, `month`/`1974-06`, `year`/`1974`, `decade`/`1970` — a year mid-decade rounds down),
  parsed in UTC by `resolveTakenAt` to the period's first instant; no time of day (the operation is for
  scans, where the hour is never known); an unknown precision or a value that does not parse at it → 400,
  coordinate validation, `DisallowUnknownFields` (an unknown operation → 400) + 4 MiB limit; errors mapped
  `ErrNoPhotos`/`ErrNoOperations`/`ErrAlbum/LabelNotFound`→400, `ErrBatchTooLarge`→413, otherwise 500;
  per-photo errors return 200 with the detail in the body; mounted by a further `server.WithAPI`
  (`buildBulkAPI` in `cmd/kukatko/bulk.go`)),
  `internal/mapy/`
  (a server-side HTTP client of the mapy.com REST API, **the key never leaves the server** — it is sent only
  in the `X-Mapy-Api-Key` header, never in a URL/error, all behind the `Client` interface (fakeable):
  `New(Config{BaseURL,APIKey,Lang,Timeout,HTTPClient})` → `*HTTPClient`; `Tile(ctx,TileParams{
  Mapset,Z,X,Y,Retina}) (*TileResult,error)` (validates the mapset allowlist, builds the URL
  `/v1/maptiles/{mapset}/256[@2x]/{z}/{x}/{y}`, **streams** the body through a `cancelReadCloser` that
  cancels the request ctx on Close — it never holds a tile in RAM), `ReverseGeocode(ctx,lat,lng)
  (*GeocodeResult,error)` (`/v1/rgeocode?lon=&lat=&lang=cs` → the first `item` simplified to
  `{Name,Location,RegionalStructure}`), `Geocode(ctx,query,limit) ([]Place,error)` (**forward**,
  `/v1/geocode?query=&lang=cs&limit=` → `[]Place{Name,Label,Type,Location,Lat,Lng}` ordered from
  the best match; maps `position.lon/lat` → `Lng/Lat` and drops bbox/zip/regionalStructure;
  an empty query = `ErrEmptyQuery` **without calling upstream**, no match = **an empty slice + nil**,
  not `ErrNotFound` — a half-typed name is not an error; `ClampGeocodeLimit` clamps to
  1–`MaxGeocodeLimit` (15), ≤0 → `DefaultGeocodeLimit` (5), and is called inside `Geocode` too, so
  no call site sends an unbounded count upstream); allowlist `basic|outdoor|aerial|winter`
  (`IsValidMapset`), retina only for `basic`/`outdoor` (`RetinaSupported`); the sentinels
  `ErrUnauthorized` (401/403) / `ErrNotFound` (404 and empty items) / `ErrRateLimited` (429) /
  `ErrUpstream` (another status / an unreadable response) / `ErrUnavailable` (transport / 502/503/504) /
  `ErrInvalidMapset` / `ErrInvalidURL`; `statusError` **does not add the response body** to the error, so
  the key cannot leak even if mapy.com echoes it; every non-200 is additionally **logged at WARN** with the
  status + mapset (`slog.WarnContext`, not a 404 from rgeocode — that is a normal answer), so
  a rejected key does not end up as merely a grey tile; **`Health`** (`health.go`, nil-safe, concurrency-
  safe) folds call results into `HealthStatus{State,Detail,CheckedAt}`: `Record(err)` classifies the
  sentinel → `HealthState` `ok|key_rejected|rate_limited|unavailable|error` (it **ignores** `ErrNotFound`/
  `ErrInvalidMapset`/`context.Canceled` — they say nothing about upstream health),
  `Snapshot()` reads it, `State.Degraded()` = everything except `ok`/`unknown`; `Detail` comes from client errors,
  so it never carries the key), `internal/mapsapi/`
  (the HTTP API for maps — tile proxy, reverse geocode, place search and a GeoJSON feed; the interfaces
  `TileFetcher`/`Geocoder`/`PlaceSearcher` (satisfied by `mapy.Client`, nil → 503) and `PhotoLister`
  (`photos.Store.List`+`Count`) →
  unit-testable with fakes; `NewAPI(Config{Tiles,Geocoder,Places,Photos,Health,RequireAuth,TileCacheMaxAge,
  TileCacheTTL,TileCacheBytes,GeocodeCacheTTL,GeocodeRatePerSec,GeocodeRateBurst,MaxGeoPhotos})`+
  `RegisterRoutes` mounts
  `/map` behind `RequireAuth`: `GET /map/tiles/{mapset}/{z}/{x}/{y}` (validates the mapset→400/retina from
  the `@2x` suffix on `{y}` or `?retina=true`, with `Cache-Control: public, max-age, immutable`;
  a **server-side cache** `tileCache` (`tilecache.go`: bounded by bytes + TTL, lazy expiry,
  **LRU** eviction, key `mapset/z/x/y[@2x]`) — a hit is served from memory without calling mapy.com
  (= a saved credit, on the free tier 1 tile = 1 credit), a miss is streamed and **only a success** is
  stored (an error is **never** cached, otherwise an outage/rejected key would freeze into the map for the whole TTL);
  a tile above `maxCachedTileBytes` (512 KiB) is streamed without caching, so it is never buffered
  whole into RAM; the outcome is reported by the `X-Tile-Cache: hit|miss` header; errors via `writeTileError` →
  404/429/503/502 and **401/403 → `StatusMapKeyRejected` (424)**, i.e. a dedicated status for *our
  rejected key* (a raw 403 would lie that the caller's request is bad) — the frontend recognises it and says
  why the map is empty; every upstream call records its outcome into `mapy.Health` (→ system status)),
  `GET /map/rgeocode
  ?lat=&lng=` (parses+range-checks the coordinates→400, a **TTL+capacity cache** `ttlCache[GeocodeResult]`
  keyed by the coordinates to 5 decimals, an uncached lookup goes through the **token-bucket** `rateLimiter`→429 to save credits,
  the response simplified + `Cache-Control: private`), `GET /map/geocode?q=&limit=` (`geocode.go`:
  the type-ahead for the location editor; the order of the guards follows cost — an empty/>200-character `q` → 400 **before**
  the call, then `ttlCache[[]Place]` (key = `limit` + the case-folded query with collapsed spaces,
  **diacritics are kept** — `veseli`/`veselí` are different queries upstream), and only the rest goes to
  **the same `rateLimiter` as rgeocode** (one credit budget = one limiter) → 429; the client
  debounces on top of that, this is the half of the throttle that cannot be bypassed. `limit` is **clamped**
  (`mapy.ClampGeocodeLimit`), not a 400. `mapy.ErrNotFound` (a 404 upstream) is turned into an **empty
  `items` + 200**; otherwise `writeGeocodeError` as with rgeocode. The `ttlCache` cache (`cache.go`,
  generic: TTL + capacity, lazy expiry, eviction of the soonest-expiring — deliberately **not LRU**, unlike
  `tileCache`, because every entry is equally expensive), default 2000 entries),
  `GET /map/photos` (a GeoJSON
  **FeatureCollection**, `parseGeoParams` forces `HasGPS=true` + honours `taken_after`/`taken_before`/
  `album`/`label`/`archived`, `Limit=MaxGeoPhotos`, ordering taken_at desc; every feature is a
  `Point` with an RFC 7946 `[lng,lat]` coordinate and the properties `uid`/`title`/`taken_at`/`media_type`/
  the relative `thumb` path `tile_224`, photos missing either coordinate are skipped; the collection also
  carries the foreign member `coverage:{located,total}` — the markers drawn against one extra `Count` over the
  **same** params with `HasGPS` lifted and paging stripped, so the map can say what it is leaving out instead
  of showing 11 % of a library in silence; a failing count is a 500, not a collection quietly claiming the
  library is empty); defaults cache 24h /
  tile cache 64 MiB + 24h / rate 5/s burst 10 / max 50000 features; mounted by `server.WithAPI`
  (`buildMapsAPI` in `cmd/kukatko/maps.go`, both the client and `mapy.Health` are built only when
  `maps.mapy_api_key` is set — `newMapsHealth`; `buildSystemAPI` gets the same tracker)),
  `internal/places/`
  (the DB layer for the **cache of a photo's reverse-geocoded place** — country/region/city/place_name resolved
  from GPS via mapy.com and stored, so the library can be browsed/filtered by locality without repeatedly
  calling the rate-limited geocoder; **schema choice: the side table `photo_places`** (not columns
  on the wide `photos`) keyed by `photo_uid` FK `ON DELETE CASCADE` — the place is sparse (only geotagged
  photos have a row) and it is a derived, regenerable cache filled asynchronously by a job, mirroring
  `face_detections`/`user_ratings`; migration `0018_photo_places.sql`: `photo_uid PK`, `country`/
  `region`/`city`/`place_name TEXT NOT NULL DEFAULT ''`, `lat`/`lng DOUBLE PRECISION` (the coordinates
  the geocode was computed from — detecting a position change → re-geocode; NULL for a photo without GPS, whose
  row only marks "processed"), `geocoded_at TIMESTAMPTZ`, indexes on `country` and `city` (grouping/
  filtering by locality); `Store` = `NewStore(pool)`: `GetPlace(photoUID)` (`ErrPlaceNotFound`)/
  `SavePlace(Place)` (upsert on `photo_uid`, stamps `geocoded_at`)/`ListPhotosMissingPlaces(limit)`
  (uids of non-archived **geotagged** photos with no `photo_places` row, newest-first, LEFT JOIN —
  the basis of the backfill)), `internal/placesjob/`
  (wiring reverse geocoding into the queue, all behind the interfaces `PhotoStore`/`PlaceStore`/`Geocoder`
  (a subset of `mapy.Client`, fakeable)/`Enqueuer`/`RateLimiter`/`CreditBudget`/`CreditMeter` → unit-testable
  with fakes without network/DB; `Service` = `New(Config{Photos,Places,Geocoder,Enqueuer,Limiter,Budget,
  Meter,OfflineRetryDelay,RateLimitDelay})` (panics on nil Photos/Places/Geocoder/Enqueuer, `Limiter` nil →
  always-allow, `Budget` nil → unlimited, `Meter` nil → no-op), `BudgetSnapshot()` exposes the budget readout;
  **the `places` handler** `Handle`(=`worker.HandlerFunc`, registered in `serve` when the mapy key is
  set) → loads the photo from the `{"photo_uid"}` payload; **idempotent** (a photo whose place is cached for its
  **current** coordinates is skipped; a coordinate change → re-geocode), a photo **without GPS** → stores an empty
  "processed" marker (never retried); otherwise `mapy.ReverseGeocode(lat,lng)` → `parsePlace`
  parses `regional_structure` (the types `regional.country`/`region`/`municipality`, the `regional.` prefix
  optional) into country/region/city + place_name = the most specific label, and stores it via
  `places.SavePlace` with the source coordinates; **mapy.com unavailable/rate-limited**
  (`mapy.ErrUnavailable`/`ErrRateLimited`) → `worker.RetryAfter(5 min)` (a deferral without burning an attempt,
  mirrors the embed job), **`mapy.ErrNotFound`** → a processed marker with the coordinates (not retried forever),
  another error a normal retry; **respect for mapy.com credits — two independent bounds**: (1) `RateLimiter`
  (token-bucket `NewTokenBucket(ratePerSec,burst)`, mirrors the geocode proxy limiter;
  `maps.geocode_rate_per_sec`/`geocode_burst`) caps how **fast** credits go — when it is empty,
  `worker.RetryAfter(1 min)` (processing slowly is OK); (2) `CreditBudget` (`budget.go`) caps how **many**:
  `WindowBudget` = `NewWindowBudget(BudgetConfig{Limit,Window,Clock})` is an in-memory fixed-window counter
  (`maps.geocode_budget`/`geocode_budget_window`, default 1000/24h, `Limit<=0` → unlimited) granting
  `Limit` geocodes per window, rolled lazily on first use after it elapses (a quiet period accumulates
  nothing) — an exhausted budget → `worker.RetryAfter(time until the window refills, floor
  `MinBudgetRetryDelay` 1 min)`, i.e. the job sleeps until credits exist instead of churning the queue every
  minute for the rest of the window. **Ordering matters**: the budget is reserved *before* the limiter is
  asked, so an empty budget yields the long deferral; a credit reserved for a call the limiter then blocks is
  `Refund()`ed. So is one for a call mapy.com never performed (`ErrUnavailable`/`ErrRateLimited`) — every
  answer it did give, **including `ErrNotFound`**, costs a credit and is reported to `CreditMeter`
  (`*metrics.Registry` → `kukatko_geocode_credits_spent_total`). The count is in memory, like the token
  bucket: a restart starts a fresh window. `Snapshot()` → `BudgetSnapshot{Enabled,Limit,Spent,Remaining,
  Window,ResetsAt}` feeds `GET /system/status` → `geocode` and the `kukatko_geocode_credits_remaining`/
  `_limit` gauges; one instance is built in `runServe` (`newGeocodeBudget`, nil without a mapy key) and
  shared by the job, the status service and the collector; `BackfillPlaces(ctx)` enqueues `places`
  for every geotagged photo without a place (dedup no-op), returns the count), `internal/importer/`
  (bookkeeping of import runs, the `import_runs` table in migration `0013_import_runs.sql`:
  `id BIGSERIAL`, `source TEXT` CHECK `photoprism|photosorter` (`0026` adds `folder`, `0041` adds
  `photosorter_feeds`), `started_at`/`finished_at TIMESTAMPTZ`, `status TEXT`
  CHECK `running|done|failed`, `high_watermark TIMESTAMPTZ`, `counts JSONB`
  `{imported,updated,skipped,failed}`, `last_error TEXT`. **Only `folder` is still written**: the
  one-off import finished in August 2026 and its importers were removed, so its
  runs — and the watermarks they stamped — are now a **provenance record** the table keeps and every
  reader still decodes. The legacy `Source` values (`SourcePhotoPrism`/`SourcePhotoSorter`/
  `SourcePhotoSorterFeeds`/`SourceFolder` + `Valid()` + `AllSources()`, both reading one package-level
  list so the predicate a writer validates against and the enumeration a reporter iterates — the system
  status, the `/metrics` import gauges — cannot drift apart; both hand out a **copy**)/`Status`
  (`StatusRunning`/`StatusDone`/`StatusPartial`/`StatusFailed` + `AllStatuses()` in lifecycle order, so
  a reporter can publish one series per status instead of only the one a run happens to be in)/`Counts`/
  `Run`; `Store` = `NewStore(pool)`: `Start(ctx,source)` opens a `running` row (`ErrInvalidSource`),
  `UpdateCounts(ctx,id,counts)` overwrites the tally, `Complete(ctx,id,watermark,counts)` closes it as
  `done`/`partial` with `finished_at` stamped, `Fail(ctx,id,lastErr,counts)` as `failed`
  **without** a watermark (both match a running run only → `ErrRunNotFound` on a double close);
  every live caller passes a `nil` watermark (a folder has no source timestamp to resume from), so the
  column only carries the finished migration's values,
  `Get(ctx,id)`, `LatestRun(ctx,source)` →
  `(Run, found bool, err)` **the newest run of the source regardless of state** (running/done/failed;
  the basis of the system-status dashboard),
  `List(ctx,limit,offset)` a page of runs
  **across all sources** newest-started-first (limit clamp `[1,200]`, default 50, a non-nil empty
  page) — the basis of the admin import history; the sentinels
  `ErrRunNotFound`/`ErrInvalidSource`; **`import_failures`** (migration `0042_import_failures.sql`) keeps
  the individual **per-photo and per-file** defects of a run, which used to go only into `slog.Warn` and vanish (satellites —
  markers/album membership/edits/pHash/files): `Failure`
  (`RunID`/`Source`/`Stage`/`PhotoUID`/`SourceRef`/`Detail`/`Error`/`CreatedAt`/`ResolvedAt`), `Stage` ∈
  `photo|file|marker|album_member|label|thumbnail|embedding|faces|phash|edit|metadata`, the helper
  `NewFailure(runID,source,stage,photoUID,sourceRef,detail,err)`; `RecordFailures(ctx,[]Failure)` (batch),
  `CountUnresolvedFailures(ctx,id)`, `ListFailures(ctx,FailureFilter{RunID,Source,UnresolvedOnly,Limit,Offset})`.
  `Counts` (the `counts` JSONB) is `imported`/`updated`/`skipped`/**`deduplicated`**/`failed` — the fourth bucket
  counts SOURCE photos whose content was already catalogued under another source photo (see the
  `photoprism_aliases` table in `internal/photos`); reading that as "skipped" is how 450 production
  photos went missing under a clean-looking migration run. A run recorded before the field existed simply has no key for it and reads back as 0.
  **`Complete` auto-detects the status**: a run with ≥1 unresolved defect closes as `StatusPartial`
  (`partial`, 0042 extends the status CHECK) instead of `done`. 0042 also
  restores `folder` in the source CHECK, which `0041` dropped by mistake. `internal/dirimport` collects
  defects into `runState` and persists them via `RecordFailures` before `Complete`),
  `internal/importapi/`
  (maintainer-only, **read-only** HTTP API over the import bookkeeping, behind `RequireMaintainer`:
  interfaces `RunLister` (List) and `FailureLister` (ListFailures), both satisfied by
  `*importer.Store`; `NewAPI(Config{Runs,Failures,RequireMaintainer})`+`RegisterRoutes` mounts
  `GET /import/runs` (`parsePaging` limit≤200/offset, invalid → 400) → `{runs,limit,offset}`, a page of
  `import_runs` newest-started-first via `importer.Store.List`, and `GET /import/failures`
  (`?source=&run_id=&unresolved=&limit=&offset=`, an unknown source → 400 via `importer.ErrInvalidSource`)
  → `{failures,limit,offset}`. **There is nothing to trigger.** The `POST /import/photoprism`,
  `/import/photosorter`, `/import/photosorter-feeds` triggers and `GET /import/verify` went with the
  importers in August 2026; the only import left, `kukatko import dir`, reads a directory on the
  server's disk and is therefore driven from the CLI. Wired in `serve` by `buildImportAPI` in
  `cmd/kukatko/import.go`), `internal/backup/`
  (an in-process, scheduled **S3 backup** of the database and the originals into **a second, independent bucket**, all
  behind the interfaces `ObjectStore`/`Dumper`/`OriginalSource` → unit-testable with fakes without S3/DB/FS;
  `Service` =
  `New(Config{Objects,Originals,Dumper,Retention,Logger})` (panics on nil Objects/Originals/Dumper);
  **`Run(ctx,ts)`** does three things in order: (1) a **DB dump** via `Dumper` streamed to S3 as
  `db/kukatko-<ts>.dump` (`objectSize=-1`, never whole in RAM; ts is supplied by the scheduler/command), (2)
  an **incremental sync of the originals** (`SyncOriginals` — skip by key+size via `ObjectStore.Stat`,
  the key = the original's relative path; **purely additive**, a deletion in the source is not propagated), (3)
  **retention** (`PruneDumps` — prunes old dumps down to the last
  `Retention`, `≤0` = keep everything; **only the `db/` prefix, never the originals**); **the dump is mandatory** — a failure
  aborts the run **before** pruning, so an unsuccessful backup cannot delete the last good dumps;
  `Run` serialises concurrent runs (`ErrAlreadyRunning`), `Trigger(ctx,ts)` starts a run in the background
  (a detached ctx, for the HTTP handler), `Status()` = the state + the last run; **`RunSchedule(ctx,spec)`**
  is a scheduler over `ParseSchedule` (a standard 5-field cron / the `@daily`/`@every` descriptors via
  `robfig/cron`; empty → `ErrNoSchedule`, invalid → `ErrInvalidSchedule` → scheduled backups are
  off, manual ones still work) with its own ctx-aware loop; **`s3Store`** (`NewS3Store(S3Options)`) =
  a minio-go/v7 adapter, **path-style** (`BucketLookupPath`), `parseEndpoint` (scheme→TLS, a bare host =
  TLS), the sentinels `ErrNotConfigured`/`ErrInvalidEndpoint`, `isNotFound` (404/NoSuchKey) → Stat
  ok=false / Remove idempotent, **`CopyFrom(srcBucket,srcKey,key)`** = a **server-side copy** via
  `ComposeObject` (a single source → it degrades to a plain `CopyObject`, above 5 GiB it reaches for a multipart
  copy) — the bytes **do not pass through the process**; the request goes to *this* endpoint, so its credentials
  must be able to **read `srcBucket`**; **`pgDumper`** (`NewPgDumper(dsn)`) = a shell-out to `pg_dump
  --format=custom --no-owner --no-privileges`, the **DSN via the `PGDATABASE` env** (not an argument, so the password
  is not in `ps`), `Dump` returns a reader (Close waits for the process + surfaces stderr), `PgDumpAvailable`,
  `ErrPgDumpMissing`;
  **the source of the originals** = `OriginalSource` (`List` + `CopyTo(ctx,dst,original)`; `CopyTo` chooses for itself
  how it transfers the bytes) and it is picked by `storage.backend` in `cmd/kukatko/backup.go` (`buildBackupOriginals`):
  **`DiskOriginals`** (`NewDiskOriginals(root)`, backend `fs`) = a walk of the storage (skipping `.tmp`,
  confining keys against traversal), `CopyTo` streams the file up via `Put` — **it also serves the restore**
  through `Stat(key)` (exists + size, for skip-existing) and `Write(key,r)` (an atomic write into
  `.tmp` + rename → resumable);
  **`BucketOriginals`** (`bucket.go`, `NewBucketOriginals(source,bucket)`, backend `r2`) = `List`
  lists the primary bucket (skipping the `db/` and `.tmp/` prefixes — neither a dump nor an unfinished upload is an original),
  `CopyTo` delegates to `dst.CopyFrom` → a **bucket→bucket server-side copy**, so the library is never
  dragged onto the VPS just to be uploaded back from there; the sentinels `ErrNoSourceStore`/`ErrNoSourceBucket`
  (an unconfigured primary **must not** look like an empty library) and `errBackupSameBucket` in the
  wiring (pointing the backup at the primary bucket = backing up nothing). **The object store has no versioning**,
  the second bucket is the only protection against a deletion → the originals are **never** expired; keys and
  secrets are never logged;
  **RESTORE / disaster recovery** (`restore.go`, `pgrestore.go` — the counterpart of the backup): `ObjectStore`
  is extended with **`Open(ctx,key)`** (a streaming GET from the bucket, on `s3Store` via `minio GetObject`); the new
  interfaces **`Restorer`** (`Restore(ctx,archive io.Reader)`), **`LocalOriginals`** (List/Stat/Write,
  satisfied by `DiskOriginals`) and **`PhotoCatalog`** (`CountPhotos`/`ListFilePaths`, satisfied by `photos.Store`);
  **`RestoreService`** = `NewRestoreService(RestoreConfig{Objects,Restorer,Originals,Photos,Logger})`
  (panics on nil Objects): **`ListDumps`** (the dumps under `db/` ending in `.dump`, newest first) /
  **`LatestDump`** (`ErrNoDumps`) / **`RestoreDatabase(key)`** (an empty key → the newest; streams the
  dump from S3 straight into `Restorer`; `ErrDumpNotFound` on an unknown key — **destructive**) /
  **`RestoreOriginals`** (downloads from the bucket only the missing originals — skip by key+size via
  `LocalOriginals.Stat`, the dumps under `db/` are skipped, an atomic `Write` → resumable, honours ctx cancel,
  `RestoreOriginalsResult{Downloaded,Skipped}`) / **`Verify`** (the integrity report `VerifyReport`
  {PhotosInDB,FilesInDB,OriginalsOnDisk,MissingOnDisk,ExtraOnDisk,Consistent} via the pure `reconcile`
  set-diff of `photo_files.file_path` vs the disk); **`pgRestorer`** (`NewPgRestorer(dsn)`) = a shell-out to
  `pg_restore --format=custom --clean --if-exists --no-owner --no-privileges --single-transaction
  --dbname=<db>`, reading the archive **from stdin** (never whole in RAM), with the **DSN parsed into PG\* env**
  (`PGHOST`/`PGPORT`/`PGUSER`/**`PGPASSWORD`**/`PGDATABASE` via `pgx.ParseConfig`) → the password is **never
  in argv**; `PgRestoreAvailable`, the sentinels `ErrPgRestoreMissing`/`ErrInvalidDSN`; no secret leaks
  anywhere), `internal/backupapi/`
  (a maintainer-only HTTP API over the backup: the `Service` interface (Status+Trigger, satisfied by `*backup.Service`,
  fakeable, **nil = not configured**); `NewAPI(Config{Service,RequireMaintainer})`+`RegisterRoutes`
  mounts `GET /backup` (the state + the last run, a nil service → `configured:false`) and `POST /backup`
  (starts `Trigger` in the background → 202 `{status:"started"}`, `ErrAlreadyRunning` → 409, a nil service →
  503); always mounted in `serve` (`buildBackupAPI` in `cmd/kukatko/backup.go`)), `internal/restoreapi/`
  (a maintainer-only HTTP API over the restore, **read-only operations only**: the `Service` interface
  (`ListDumps`+`Verify`, satisfied by `*backup.RestoreService`, fakeable, **nil = not configured**);
  `NewAPI(Config{Service,RequireMaintainer})`+`RegisterRoutes` mounts `GET /restore/dumps` (the list of dumps,
  503 without configuration, 502 on an S3 error) and `POST /restore/verify` (the integrity report, 503 without
  configuration); **the destructive DB restore is deliberately not exposed over HTTP** (it would pull the tables out from under
  a running server — it belongs in the CLI with the server stopped); always mounted in `serve`
  (`buildRestoreAPI` in `cmd/kukatko/restore.go`)), `internal/maintenance/`
  (**library integrity check & repair** — it keeps a large, long-lived library consistent:
  it reveals drift between the catalogue and the files on disk and fills in/regenerates derived data; it mirrors
  the previous system's `cache build-thumbs`, but is broader and safer (**it never deletes originals** — that is the
  job of the trash/purge), idempotent, with repairs going through the persistent job queue; all behind the interfaces
  `PhotoCatalog` (`CountPhotos`/`ListPrimaryFiles`/`ListFilePaths`/`ListPhotosMissingPhash`/
  `ListDimensionMismatches`/`RepairDimensions`,
  satisfied by `photos.Store`)/`VectorCatalog` (`ListPhotosMissingEmbedding`/`ListPhotosMissingFaces`/
  `PlanFaceBoxRepair`/`ApplyFaceBoxRepair`/`ListDuplicateFaceMarkers`/`ListSidewaysDetections`/
  `ClearFaceDetection`, `vectors.Store`)/`OriginalStore` (`Stat`, `storage.Storage`)/`DiskScanner` (`List`, an adapter over
  `backup.DiskOriginals`)/`ThumbChecker` (`HasThumbnail`, `NewThumbCache` over `thumb.Thumbnailer`)/
  `Enqueuer` (`EnqueueThumbnail`+`EnqueueFaceDetect`, `jobs.Enqueuer`)/`EmbedBackfiller` (`embedjob.Service`)/
  `FaceBackfiller` (`facejob.Service`)/`FaceCache` (`ClearSurplusLinks`, `facematch.Service` — which owns
  the face↔marker pairing rules the cache has to agree with)/`OrphanImporter` (optional, nil turns the
  orphan import off) →
  unit-testable with fakes without DB/disk/queue; `Service` = `New(Config{...,SampleLimit})`
  (panics on a nil mandatory collaborator; default `SampleLimit` 20); **`Scan(ctx)`** (read-only) returns
  `Report{Photos,FilesInDB,OriginalsOnDisk,MissingOriginals,OrphanFiles,MissingThumbnails,
  MissingEmbeddings,MissingFaces,MissingPhashes,TransposedDimensions,TransposedFaceBoxes,
  DuplicateFaceMarkers,SidewaysFaceDetections}` — each class is a
  `Finding{Count,Samples}`
  (a count + a limited sample of identifiers); `representativeThumbSize`=`tile_224` is the proxy for the presence of
  thumbnails, an orphan = a file on disk with no `photo_files.file_path` (the `orphanKeys` set-diff), `Report.Clean()`;
  **`Repair(ctx,RepairOptions{Thumbnails,Embeddings,Faces,Phashes,ImportOrphans,Dimensions,FaceMarkers,
  SidewaysFaces})`** (each opt-in,
  idempotent, in a fixed order) → `RepairResult` with the scheduling counts: thumbnails/phashes enqueue
  `thumbnail` jobs (`EnqueueThumbnail`), embeddings/faces call the backfill, the orphan import goes through the
  upload pipeline (a per-orphan failure is counted without aborting); `ErrOrphanImportUnavailable` when the
  import is selected without an importer.
  **`Dimensions`** is the one repair that writes the catalogue instead of enqueuing regenerable work, and it has
  two halves. For every photo `TransposedDimensions` reports it writes the file's own pair
  (`photos.RepairDimensions`, `DimensionsFixed`); then it runs the faces half over the **whole** catalogue
  (`vectors.PlanFaceBoxRepair` + `ApplyFaceBoxRepair` → `FaceBoxesFixed`/`FaceBoxesSkipped`) rather than only
  over the photos it just fixed — a photo corrected by an earlier run still carries face rows to decide, and a
  row skipped for want of evidence has to stay reachable once that evidence arrives. Each face row gets the
  transform its photo's markers support and nothing at all when they support none (`FaceBoxesSkipped`), which is
  why `TransposedFaceBoxes` counts only the rows that would actually be written: a finding is what the repair
  would do. Both halves are guarded on the state they replace, so an interrupted run resumes, a re-run is a
  no-op and no box is moved twice; `Scan` is the **dry run**.
  **`FaceMarkers`** likewise writes the catalogue directly: `DuplicateFaceMarkers` (from
  `vectors.ListDuplicateFaceMarkers`, sampled by marker uid) is its dry run, and the repair visits each
  affected photo once (`affectedPhotos`, sorted, deduplicated) and delegates to `FaceCache.ClearSurplusLinks`,
  which re-derives that photo's exclusive pairing and clears the cached link of every face claiming a marker
  another face won → `FaceLinksCleared`. It only ever nulls three columns on a face row: no face and no marker
  is deleted, and a marker with a single face link is untouched — the genuinely **duplicated markers** an import
  created are a different problem and must not be swept up here.
  **`SidewaysFaces`** is the repair for the quarter-turned photos the sidecar detected **on their side** (it reads
  no EXIF and `facejob` used to send the original untouched): `vectors.ListSidewaysDetections` is its dry run
  (`SidewaysFaceDetections`), and per photo it clears the detection record and enqueues `face_detect`
  (`ClearFaceDetection` → `EnqueueFaceDetect` → `SidewaysFacesEnqueued`), in that order — clearing is what makes
  the photo eligible again, since the job would otherwise take its idempotent skip. It cannot be a coordinate fix
  the way `Dimensions` is: a detector looking at a sideways picture also **missed** faces on it, and no arithmetic
  recovers a face that was never found. The existing face rows stay until the new detection replaces them
  wholesale (the sidecar's box is usually asleep, and boxes in the wrong frame still beat none while the queue
  waits), and a photo re-detected upright drops out of the scan, so a re-run is a no-op), `internal/reset/`
  (**the guarded library wipe** — what `kukatko maintenance reset` runs and what phase 1 of
  [`docs/MIGRATION_PLAN.md`](MIGRATION_PLAN.md) had nothing to run before: it empties every catalogue table and
  every object the store owns so the library can be re-imported from scratch. The deployment has **no S3 backup**
  ([`READINESS_AUDIT.md`](READINESS_AUDIT.md) §4), so the guards *are* the package and the truncation is the easy
  part. **Two explicit table lists** in `tables.go` — `catalogueTables` (27, wiped, incl. `photoprism_aliases`
  from `0046` and `review_skips` from `0059`) and `preservedTables` (6:
  `users`/`sessions`/`api_tokens`/`announcements`/`audit_log`/`schema_migrations`, never touched), exported as
  `CatalogueTables()`/`PreservedTables()`; an allowlist rather than "everything except", because a forgotten
  entry in the first merely survives while a forgotten entry in an exclusion list would be **destroyed**.
  `classifySchema` compares them with `pg_tables` and aborts on `ErrSchemaDrift` naming the offender, so a
  migration that adds a table cannot silently leave part of the library behind. `Service` =
  `New(Config{Pool,Target,Storage,Thumbs?,CacheDir?})` (panics on a nil Pool/Storage or an empty
  `Target.Database`); **`Preflight(ctx,Options)`** (read-only) = the dry run: `verifyTarget` (reads
  `current_database()` **from the server** and compares it with
  `TargetFromConfig(config.database.url, <the configured bucket>)` → `ErrTargetMismatch`; a DSN naming no
  database is refused too), the schema check, `Counts{Catalogue,Preserved}`
  of `[]TableCount` (`Rows()`, `NonEmpty()`) and a `StoragePlan{Referenced,Stored,Foreign,Sweep}` of
  `PrefixCounts{Originals,Thumbnails,Sidecars}`; **`Execute(ctx,Options,before)`** deletes, in this order —
  `ErrNotExecuting` without `Options.Execute`, target re-verified, `checkConfirmation` (`Options.Confirm` must
  equal the target database name → `ErrConfirmationMismatch`, **and** `Options.ConfirmBucket` must equal
  `Target.Bucket` → `ErrBucketConfirmationMismatch` — the two come from independent config keys and can name
  independent deployments, so a dev database pointed at the production bucket is refused rather than confirmed
  by proxy; an empty `Target.Bucket` (the `fs` backend) makes a typed bucket name a mismatch too, since the
  operator was aiming at something this run cannot reach), the **store emptied before the catalogue** (the catalogue is where
  the object keys come from), and only then `TRUNCATE … RESTART IDENTITY` (**no CASCADE**) plus
  `audit.Write(ActionLibraryReset)` **in one transaction**, then a re-count → `Result{Before,After,Storage}`.
  That transaction opens with the wipe's one piece of surgery (`unlinkAccounts`): drop
  `users_subject_uid_fkey`, null every `users.subject_uid`, truncate, put the constraint straight back.
  `users` is preserved and therefore outside the TRUNCATE list, and Postgres refuses to truncate a table an
  unlisted table references — structurally, whatever the rows say — so that one foreign key would fail the
  whole wipe, and CASCADE (which would take the accounts) is the one escape this command must never use.
  DDL is transactional, so a failure anywhere puts the constraint back with everything else. Nulling the
  column is also simply true: the person that link named is about to stop existing. Every account keeps its
  credentials, role, note and history — the link is the only thing a preserved account loses.
  Any object that fails to delete skips the truncation and returns `ErrStorageIncomplete` — the catalogue is
  the only remaining record of what those objects are, and the whole run is idempotent, so the answer is to fix
  the store and repeat. `keys.go` owns the scope: `classifyKey` recognises exactly `YYYY/MM/<name>` (an anchored
  regexp — the bucket root *is* the namespace, there is no Kukátko prefix), `thumb/` and `sidecars/`, and calls
  everything else `kindForeign`, which is counted and **never** deleted; `catalogueFiles.objectKeys` expands each
  catalogued path into its original + `sidecarexport.KeyFor` sidecar and each hash into `thumb.RelPath` × every
  registered size (blind on purpose: probing first would cost a request per candidate); `deleteKeys` runs the
  deletions through an `errgroup` bounded by `Options.Concurrency` (default 8), folding each outcome into
  `StorageResult{Deleted,Missing,Skipped,Foreign,Failed,Failures,ThumbCacheCleared,ThumbCacheSwept}` with the
  failure sample capped at 20 (`Touched()` reports whether the run got as far as doing anything);
  `sweepKeys` is the opt-in `Options.OrphanSweep` path over `storage.KeyLister` (`ErrSweepUnsupported` when the
  store cannot list) and **supersedes** the catalogue's candidates — it deletes what is actually there under the
  owned prefixes, referenced or not, which is both the correct wipe and the cheaper one. The local thumbnail
  cache goes too: `Thumbs.Remove(hash)` per catalogued hash, or the whole `<cache_path>/thumb` tree under a
  sweep. `Options{Execute,Confirm,ConfirmBucket,OrphanSweep,Concurrency,ActorUID,Operator}` — a CLI run has no session, so
  `ActorUID` stays empty (a system action) and `Operator` carries `$USER@$HOSTNAME` into the entry's details
  together with the target, the per-table row counts and the object counts. Deliberately **no HTTP surface**,
  for the reason `restore db` has none: it pulls the tables out from under a running server), `internal/thumbjob/`
  (the worker handler of the `thumbnail` job — the **repair path** for maintenance: it regenerates a photo's derived
  data from the original, the **thumbnails** (`Thumbnailer.GenerateAll`, cached ones skipped) and the **pHash/dHash** (only when
  they are missing, `phash.Compute` over the decoded original), all behind the interfaces `PhotoStore`/`Thumbnailer`/
  `Decoder` (`StorageDecoder` = `storage.Materialize`+`imgconvert.EnsureDecodable`, fakeable) →
  unit-testable without a disk; `Service` = `New(Config{Photos,Thumbnailer,Decoder,Lister?,Enqueuer?})`
  (panics on a nil mandatory collaborator; `Lister`/`Enqueuer` optional — they turn the backfill on),
  `Handle`=`worker.HandlerFunc` (payload `{photo_uid,force?}`, empty uid → `ErrMissingPhotoUID` dead-letter),
  `Regenerate(uid)`/`ensurePhash` idempotent; registered in `serve` on `jobs.TypeThumbnail`.
  The payload's **`force` flag** routes the job to `ForceRegenerate` instead: the repair skips a size already cached,
  which is right when the cache is merely incomplete and wrong when the photo's *rendering* changed, since nothing
  about the cache key records the edit. It is what `jobs.Enqueuer.EnqueueThumbnailRebuild` schedules from `PUT
  …/photos/{uid}/edit`. The pHash it recomputes stays the **original's**, not the edited rendering's — a perceptual
  hash exists to recognise two copies of one shot, and turning one of them upright must not stop it matching the
  other, so a rotation changes the grid and deliberately leaves duplicate detection where it was.
  The **force path** `ForceRegenerate(uid) ([]string,error)` is the on-demand counterpart (the basis of the service
  action "regenerate thumbnail" in `photoapi`): it **overwrites** every thumbnail (`Thumbnailer.RegenerateAll`,
  an atomic overwrite) and **always** recomputes the pHash (`recomputePhash`, shared with `ensurePhash`), returning
  the sorted size names; a missing photo → `photos.ErrPhotoNotFound`, a missing/undecodable
  original is wrapped in `ErrRegenerateFailed` (HTTP 422). The **backfill** `BackfillThumbnails(ctx,all)
  (int,error)` (the basis of `POST /process/thumbnails`): it enqueues a `thumbnail` job for every photo **without a
  thumbnail** = without a pHash (`PhotoLister.ListPhotosMissingPhash`), or — when `all` — for every
  non-archived one (`ListActiveUIDs`, which also catches a missing size on a photo that has a pHash); enqueue via
  `Enqueuer.EnqueueThumbnail` (a dedup no-op → idempotent), returns the count; `ErrBackfillUnavailable`
  when the `Service` had no `Lister`/`Enqueuer`. **The dry run** `CountBackfillThumbnails(ctx,all) (int,error)`
  answers the same number **without scheduling anything** (`PhotoLister.CountPhotosMissingPhash`/
  `CountActivePhotos` — a `count(*)`, not a listing), which is what makes the cost of a run legible before it is
  paid: "the narrow predicate" is no promise of a small number — a library imported before the import scheduled
  thumbnail jobs has no pHash anywhere, so *every* photo in it matches — and a thumbnail job re-reads an original.
  It backs `?dry_run=true`; the count is a snapshot, so a concurrent import may grow it and the queue's dedup may
  shrink what a later real run schedules),
  `internal/sidecarexport/`
  (**the format** of the metadata sidecar + its atomic write into storage — a YAML file per photo next to
  the originals, so the library can be restored **from storage alone**: originals + sidecars, without a database.
  Not to be confused with `internal/sidecar`, which reads *foreign* sidecars (Google Takeout `.json`, Apple `.xmp`) during
  an import — this package only **writes**, and only its own format. `Document` = a versioned, grouped
  schema (`version`/`generated_at`/`identity`/`descriptive`/`temporal`/`spatial`/`technical`/
  `curation`/`edit`), `Version = 3` (v2 added `curation.hidden_from_library` — additive, but still a bump,
  because a reader that ignored the key would un-hide every hidden photo on restore; v3 added
  `temporal.precision`, omitted at the ordinary `day` grain, for the same reason: a reader that ignored it
  would restore "somewhere in the seventies" as a photo taken on 1 January 1970);
  `Build(Input) Document` is a **pure function** (no I/O, no
  clock — the caller collects the collaborators), `Marshal`/`Unmarshal` add/ignore the header comment
  that explains **why there are no embeddings in the file** (large, binary, cheap to recompute from
  the original — that is what the backfill jobs are for), so that nobody "fixes" it. `KeyFor(fileKey)` = the parallel
  tree `sidecars/<key>.yml` (the extension is **appended**, not replaced → `IMG_1.jpg` and `IMG_1.png`
  do not collide; `ErrEmptyKey` for a row without a path). `Writer` = `NewWriter(ObjectStore)` over a narrow
  interface (`Put`/`Delete`, satisfied by `storage.Storage`) → it works **on FS and on R2**;
  `Write(ctx,fileKey,doc)` marshals into memory (a few kB), computes the SHA256 and hands storage the exact
  size+digest, so **atomicity** is guaranteed by storage (FS temp+rename, R2 verification+deletion on a
  mismatch) — a half-written YAML is not a worse sidecar, it is an unreadable one; `Delete` is idempotent (a missing
  sidecar is not an error). The **round-trip test** (`TestRoundTrip` + `TestDocument_fixtureIsExhaustive`,
  which uses reflection to check that the fixture has not a single zero field) is the format's contract and the basis of a future
  `restore --from-sidecars`),
  `internal/sidecarjob/`
  (the worker handler of the `sidecar` job + the backfill — the **scheduling** half of `sidecarexport`: that one knows the format
  and the write, this one knows *when*. The job is enqueued by every mutation of metadata/curation data; the handler
  **re-reads** the photo and rewrites the file, so it is **idempotent and stateless** (twice = the same bytes,
  late = the current state) — which is why the queue's per-photo dedup is a safe **debounce**, not a dropped update.
  All behind the interfaces `PhotoStore`/`Organizer`/`PeopleStore`/`PlaceStore`/`UserStore`/`SidecarWriter`/
  `PhotoLister`/`Enqueuer` → unit-testable without a DB or a disk; `Service` =
  `New(Config{Photos,Organize,People,Writer,Places?,Users?,Lister?,Enqueuer?,Logger?})` (panics on
  a nil mandatory collaborator; `Places`/`Users` optional — the group is then omitted, `Lister`/`Enqueuer`
  turn the backfill on), `Handle` = `worker.HandlerFunc` (payload `{photo_uid}`, empty →
  `ErrMissingPhotoUID` dead-letter), registered in `serve` on `jobs.TypeSidecar` (only when
  `sidecar.enabled`). `Export(uid)` collects the curation data, writes it and **only then** stamps
  `MarkSidecarWritten` — when the write fails, the photo stays pending and the backfill picks it up; **a missing
  photo is a logged skip** (a purge between the enqueue and the run is a race the queue is meant to lose gracefully),
  but **a failure to read the curation data is an error** (a file claiming "no albums" because the query
  crashed is worse than no file — it looks authoritative); an unresolvable uploader costs a name, not the
  sidecar. `Remove(fileKey)` deletes the sidecar on a purge (a sidecar that outlives its photo is exactly the
  file a restore would resurrect the deleted photo from). The **backfill** `BackfillSidecars(ctx,all)
  (int,error)` (the basis of `POST /process/sidecars` and `kukatko sidecar backfill`): it enqueues a job for every
  photo with a missing/stale sidecar (`ListPhotosMissingSidecar`), or — when `all` — for
  every non-archived one (`ListActiveUIDs`, which catches curation data living outside the photo's row); idempotent and
  resumable **without bookkeeping of its own** (the queue's dedup + a self-clearing predicate);
  `ErrBackfillUnavailable` without `Lister`/`Enqueuer`),
  `internal/metajob/`
  (the worker handler of the `metadata` job — it **re-reads a photo's original** and fills in the columns whose
  authority is the file itself: the IPTC/XMP credits (`subject`/`keywords`/`artist`/`copyright`/`license`)
  and the file-technical ones (`software`/`color_profile`/`image_codec`/`camera_serial`/`projection`/
  `original_name`). It exists because of photos that came into being **before the extraction did**: rows from the
  one-off imports and everything uploaded before `internal/exif` could read these tags —
  the originals still lie in storage, so the metadata still **can** be read. All behind the interfaces
  `PhotoStore`/`Extractor`/`PhotoLister`/`Enqueuer` (`StorageExtractor` = `storage.Materialize` +
  `exif.Extract`, works for **local FS and R2**, and always cleans the temp copy up) → unit-testable without
  a disk; `Service` = `New(Config{Photos,Extractor,Lister?,Enqueuer?,Logger?})` (panics on a nil mandatory
  collaborator; `Lister`/`Enqueuer` optional — they turn the backfill on), `Handle` = `worker.HandlerFunc`
  (payload `{photo_uid}`, empty → `ErrMissingPhotoUID` dead-letter), registered in `serve` on
  `jobs.TypeMetadata`. **`Reextract(uid)`** is **exclusively a gap-filler**: it writes only into columns that
  are still empty (`photos.FillFileMetadata`), so an empty extraction never overwrites a value
  the user wrote, and it touches nothing else (captions, `taken_at`, GPS, ratings and albums are
  out of its reach) → **idempotent**, a second run changes nothing (not even `updated_at`). Video: `image_codec`
  stays empty (a clip's compression is the ffprobe-derived `video_codec`, which is out of scope); `original_name` is
  reconstructed from `photo.FileName` (storage keeps the original under the name it arrived with — the catalogue
  cannot get closer, and it is written into an empty column anyway). **A missing original** (`os.ErrNotExist`
  from `Materialize`/`Extract`) is **logged and skipped** (a nil error): the file is gone, a retry will never
  succeed and a dead-letter would only break a library-wide run; other storage/DB errors are returned → the queue
  retries. The **backfill** `BackfillMetadata(ctx,all) (int,error)` (the basis of `POST /process/metadata`):
  it enqueues a `metadata` job for every photo whose file has **never been read**
  (`PhotoLister.ListPhotosMissingFileMetadata` = `metadata_extracted_at IS NULL`), or — when `all` —
  for every non-archived one (`ListActiveUIDs`, a forced re-read of the whole library, which is how the
  fields a new extractor has learned to read get caught up); enqueue via `Enqueuer.EnqueueMetadata` (a dedup no-op),
  returns the count; `ErrBackfillUnavailable` when the `Service` had no `Lister`/`Enqueuer`. **It converges and is
  resumable**: the marker is stamped the moment the job finishes, so an interrupted run picks up exactly where
  it stopped, and a second run over an exhausted library enqueues **zero** jobs — even for a photo whose file
  has no IPTC tags at all ("we looked and there was nothing there" is a finished photo, not a pending one)),
  `internal/ocrjob/`
  (the worker handler of the `ocr` job — it **reads the text printed in a photo** (a street sign, a shop
  front, a scanned page, a banner over a stage) and stores it, so the library's search finds the photo by
  what it says. One call to the sidecar's `POST /ocr/image` over the photo's **`fit_1920`** preview: the
  size is a deliberate departure from the `fit_720` embedding uses, because small print on a sign or in a
  newspaper simply is not there at 720 px, while the image tower downsamples to a small square anyway. All
  behind the interfaces `PhotoStore`/`Recognizer`/`Previewer`/`PhotoLister`/`Enqueuer` → unit-testable
  without a network, DB or disk; `Service` = `New(Config{Photos,Client,Previewer,Lister?,Enqueuer?,
  PreviewSize?,MinConfidence?,OfflineRetryDelay?,Logger?})` (panics on a nil mandatory collaborator;
  `Lister`/`Enqueuer` optional — they turn the backfill on), `Handle` = `worker.HandlerFunc` (payload
  `{photo_uid}`, empty → `ErrMissingPhotoUID` dead-letter), registered in `serve` on `jobs.TypeOCR` (only
  when `embedding.ocr.enabled`). **`Recognize(uid)`** asks the previewer for the image **by photo** rather
  than reaching into the thumbnail cache (on an object-store backend a preview routinely has no cache file
  behind it — the mistake that once left every `image_embed` job dead-lettering with "thumbnail not
  cached"), then writes `photos.SaveOCR`. Two behaviours carry the design: an **empty reading is a
  success** that is still recorded (most photographs have no writing in them, and "we looked and found
  nothing" is what stops every backfill from re-scheduling the whole library forever — the marker is
  `ocr_at`, not an empty `ocr_text`), and it **never short-circuits on "already done"**, unlike
  `embedjob.Embed`, because a forced re-run with a better model exists precisely to replace an older
  reading. A **video is a logged skip** (a nil error): OCR runs on stills only, there is no poster-frame
  recognition, and failing would dead-letter work that was never meant to happen. An offline box →
  `worker.RetryAfter(OfflineRetryDelay, err)`, i.e. requeued **without burning an attempt**, exactly as
  `image_embed` behaves; every other sidecar/DB failure is returned so the queue retries. The **backfill**
  `BackfillOCR(ctx,all) (int,error)` (the basis of `POST /process/ocr`): a job per photo the recogniser has
  never seen (`ListPhotosMissingOCR` = `ocr_at IS NULL`, non-archived, not a video), or — when `all` — per
  non-archived still (`ListActiveImageUIDs`, the forced full re-run); enqueue via `Enqueuer.EnqueueOCR` (a
  dedup no-op), `ErrBackfillUnavailable` without `Lister`/`Enqueuer`. It only enqueues — at the measured
  ~4.4 photos/s on the box a whole library is a long queue drain, not a request),
  `internal/storyboard/`
  (the **scrub-preview sprite** of a video: one JPEG holding a row-major grid of evenly spaced frames, which the
  player offsets as a CSS background to show the frame under the cursor. `Spec{Columns,Rows,Count,TileWidth,
  TileHeight,IntervalMs}` is the layout and `Plan(durationMs,width,height)` computes it — a **full** grid
  (`Count = Columns×Rows`, never a ragged last row): 10 columns, `rows = clamp(round(duration/2s)/10, 1, 10)`,
  so a 20 s clip is one row of ten and anything from ~3.5 min up is the 10×10 ceiling with a proportionally
  longer interval; `IntervalMs = duration/Count`, the tile is 160 px wide (never upscaled past the source),
  the height follows the source aspect ratio rounded to an **even** number (ffmpeg's scaler rejects odd sides)
  and unknown dimensions fall back to 16:9. `duration <= 0` → `ErrNoDuration`, the "this clip will never have
  one" signal. `Spec.TileIndex(positionMs)` is the position→tile mapping, stated (and tested) here so the
  client mirrors a contract rather than a guess. **Storage is cache-only**, in the thumbnail cache's sharded
  layout under its own prefix — `storyboard/<aa>/<bb>/<cc>/<hash>_sb.jpg`, `CacheSubdir` exported for the wipe —
  and deliberately **never uploaded to the object store**: it adds no bucket prefix and nothing to the
  orphan-sweep contract, and a pruned cache costs one background job. `Generator` = `New(storage,cacheDir)` with
  `Path`/`Exists`/`Open`/`Remove`/`Generate(ctx,hash,srcRelPath,spec)`; `Generate` is **idempotent** (a cached
  sprite returns immediately, without even materializing the source), materializes the original (ffmpeg needs a
  real file), renders via `FFmpegArgs` — `fps=1000/<interval>,scale=W:H,tile=CxR` plus `-frames:v 1`, the
  rational fps avoiding any float formatting — into a temp file and **renames it into place**, so no reader ever
  sees a partial JPEG and a failed run leaves nothing behind. `ErrNotGenerated` (not there yet — the ordinary
  answer, not a failure), `ErrGenerateFailed` (ffmpeg ran, produced nothing usable), `ErrInvalidHash`,
  and a wrapped `video.ErrFFmpegMissing`. The end-to-end test synthesizes a clip with ffmpeg and asserts the
  sprite's pixel dimensions **are** the grid the `Spec` promised),
  `internal/storyboardjob/`
  (the worker handler and read service for storyboards — the **when** to `internal/storyboard`'s *how*.
  Generation is **lazy and per-video**: nothing enqueues the library, because a sprite costs one full decode and
  most videos are never watched. `Status(ctx,uid)` answers `StateReady` (+ the `Spec`), `StatePending` — and
  schedules the job, the queue's per-photo dedup absorbing every later ask — or `StateUnavailable`, which is
  permanent and tells the client to stop: a still, a **live photo** (its motion clip is a hover preview with no
  scrubbable timeline, so a storyboard would be a decode spent on a control that is never shown), a clip of
  unknown length, a wiring with no `Enqueuer`, or a host with no ffmpeg (`FFmpegAvailable`, injectable so tests
  do not depend on the machine). `Open(ctx,uid)` yields the sprite bytes + `Spec`, `FileHash(ctx,uid)` the ETag
  source. `Handle` = `worker.HandlerFunc` (payload `{photo_uid}`, empty → `ErrMissingPhotoUID` dead-letter),
  registered in `serve` on `jobs.TypeStoryboard`; `Generate(uid)` renders one, and a photo that **cannot** have a
  storyboard is a quiet no-op rather than a dead-letter — the job was scheduled from a state that no longer holds.
  All behind `PhotoStore`/`Generator`/`Enqueuer` → unit-testable with no ffmpeg and no disk),
  `internal/maintenanceapi/`
  (a maintainer-only HTTP API over maintenance: the interfaces `Service` (Scan+Repair, satisfied by `*maintenance.Service`,
  nil → 503) and `AuditPurger` (`PurgeOlderThan`+`Record`, satisfied by `*audit.Store`, nil → 503);
  plus `NamelessRepair` (`List`/`Snapshot`/`EnqueueDetach`/`EnqueueRestore`, satisfied by
  `*namelessjob.Service`, nil → 503);
  `NewAPI(Config{Service,Audit,Nameless,RequireMaintainer})`+`RegisterRoutes` mounts `/maintenance`:
  `GET /maintenance/scan` (the integrity report), `POST /maintenance/repair` (body `RepairOptions`,
  `DisallowUnknownFields`, an empty selection → 400, `ErrOrphanImportUnavailable` → 503, otherwise `RepairResult`)
  and `POST /maintenance/audit/purge` (body `{older_than_days}` 1..36500, cutoff = `now − older_than_days`,
  `audit.Store.PurgeOlderThan` → `{deleted,older_than_days,cutoff}`; a missing/non-positive/excessive window
  or an unknown field → 400; a **self-audit** `audit.purge` with the cutoff/window/count via `Record` — the fresh
  record survives the purge, so deleting the trail is traceable, the actor from `auth.UserFromContext`);
  and the three **nameless-subject** routes: `GET /maintenance/nameless-subjects`
  (`{subjects,marker_total,face_total}`, read-only), `POST /maintenance/nameless-subjects/detach` (the response
  body **is** the undo file — `attachment` disposition, `X-Kukatko-Nameless-Subjects`/`-Markers`/`-Faces` headers
  — written and flushed by `deliverUndo` *before* `EnqueueDetach` is called, so a snapshot that cannot be read
  (500), a client the file cannot be written to, or nothing to detach (409) all leave the catalogue untouched;
  the scheduling runs on `context.WithoutCancel`+`enqueueTimeout`, because a client that has the file and closes
  the body cancels the request the instant delivery succeeded) and
  `POST /maintenance/nameless-subjects/restore` (the undo file as the body, ≤`maxUndoBytes` = 64 MiB, unknown
  fields **allowed** — an old file must still replay — unparsable/subject-less → 400) → `202 {queued}`;
  mounted in `serve` (`buildMaintenanceAPI` in `cmd/kukatko/maintenance.go` injects `audit.NewStore`,
  the service is built by `buildMaintenanceAndThumb`, shared with the registration of the `thumbnail` handler in `buildJobs`)),
  `internal/namelessjob/`
  (**the nameless-subject repair as background work**: the `Undo` file format (`{subjects:[people.SubjectSnapshot]}`,
  shared verbatim with the CLI's `--undo-file`/`--undo`, so a file crosses freely between the two), the `Service`
  the admin surface drives (`List` — read-only report; `Snapshot` — the undo of every nameless subject via
  `people.SnapshotSubject`, skipping one that vanished mid-read; `EnqueueDetach`/`EnqueueRestore` — one job per
  subject, the scheduling maintainer's `audit.Meta` carried in the payload so the job's audit row names them),
  and the two handlers `HandleDetach`/`HandleRestore` over `people.DetachSubject`/`RestoreSubject`.
  **Why the queue and not the request:** detaching production's catch-all sets `subject_uid` NULL on ~111 000
  faces, moving every one into the partial `WHERE subject_uid IS NULL` HNSW index of migration 0047 — minutes of
  index maintenance no HTTP request may sit on. `HandleDetach` treats `ErrSubjectNotFound` as done (a retry, a
  double-click or the CLI having got there first all mean the repair happened) and **warns** when it detached
  more than the delivered undo file recorded, since replaying that file would leave the difference unassigned.
  Registered unconditionally in `buildRegistry` — the repair for an importer artefact must not depend on an
  optional feature being on),
  `internal/duplicates/`
  (**a review surface for near-duplicate photos** beyond the upload-time warning: it links photos by two
  signals — pHash Hamming distance up to `duplicate.phash_max_diff` and embedding cosine distance
  up to `duplicate.embedding_max_dist` — and merges the edges into connected components via union-find
  (`algo.go` disjoint-set + path compression/union by rank); **without O(n²)**: pHash through **banded-LSH**
  buckets (`bandCount`=`maxDiff+1` bands, which by the pigeonhole principle guarantees a shared bucket for pairs within the threshold,
  and the candidates are verified by the full Hamming distance), embeddings through HNSW (`vectors.FindDuplicatePairs`).
  All behind the interfaces `PhotoSource` (`ListByUIDs`)/`PhashSource` (`ListActivePhashes`)/`EmbeddingSource`
  (`FindDuplicatePairs`, nil turns embedding grouping off)/`FeedbackStore` (`DismissedDuplicatePairs` +
  `ConfirmedDuplicatePairs`, nil leaves every edge in place and marks nothing) → unit-testable with fakes; `Service` =
  `New(Config{Photos,Phashes,Embeddings,Feedback,PhashMaxDiff,EmbeddingMaxDist,Neighbours})` (panics on nil
  Photos/Phashes; `PhashMaxDiff<0` turns pHash off, `EmbeddingMaxDist<=0` turns embeddings off);
  **Dismissed pairs** (`feedback.DismissedDuplicatePairs`, "nechat obě" from the compare view) are registered in
  `buildGraph` (`graph.addDismissals`) **after `addPhashes` and before `addEmbedPairs`/`runPhash`**
  and both linking steps skip them — union-find cannot remove an edge, so a dismissed pair has to be
  suppressed at the moment the edge would appear, not untangled afterwards. The consequence is deliberate: a two-member
  group disappears after a dismissal (without the edge they are singletons, which are dropped), **a larger group survives
  on its remaining edges** — "A is not B" is not a claim about C. A pair with a uid the scan does not know (an archived
  /purged photo) is ignored; a pair is scanned over **node indexes**, whereas `seen` in `unionBucket` is over the
  **positions of entries** — those are different key spaces, which is why a dismissal is looked up via `entries[i].idx`;
  **`FindGroups(ctx,limit,offset)`** (backing `GET /duplicates`) → `Result{Groups,Total,Limit,Offset,
  NextOffset}`; each `Group{ID (the smallest uid),Reason (phash/embedding/both),KeeperUID,Confirmed,Members}`,
  a `Member` carries dimensions/size/`taken_at`/media_type + `is_keeper` + `phash_distance`/
  `embedding_distance` to the keeper; the **proposed keeper** = the highest resolution → the largest file →
  the oldest → the smallest uid (`selectKeeperIndex`); **`Confirmed`** is set by `graph.addConfirmations` when any
  pair inside the component carries a `feedback` confirmation ("yes, the same shot", written by the review game's
  duplicate check) — unlike a dismissal it suppresses no edge and removes nothing, one confirmed pair marks the
  whole component (the curator answered about the group they were shown), and `sortGroups` puts confirmed groups
  **first**: confirmation is the only ordering key here that is not a guess, so it outranks size. Groups then
  ordered largest-first/newest-keeper/id,
  `limit` clamped to `[1,100]`; **`CountGroups(ctx)`** = the same scan reduced to `Result.Total` (detection is
  derived state, so there is no cheaper way to count it) — which is why its one caller, the admin dashboard's
  duplicates tile, runs it in the background and never on a request (`system.DuplicateScan`);
  it only reads, **never mutates** (resolution goes through `dupmerge`); archived
  photos are not scanned (`ListActivePhashes` filters `archived_at IS NULL`)), `internal/dupmerge/`
  (**the transactional merge of a near-duplicate group into the keeper** — the mutating counterpart of the read-only `duplicates`;
  `Service=NewService(pool)`, `Merge(ctx,Input{KeeperUID,MemberUIDs,ActorUID})→Result{albums_added,
  labels_added,people_added,metadata_filled[],archived,dry_run}` and `Preview` (a dry run, tx rollback).
  In **one `pgx.Tx`** (like `bulk` — the audited store methods open their own tx and cannot be composed)
  it computes a `plan`: the union of the copies' albums/labels/people minus what the keeper already has, `pickFill` of the missing scalars
  (title/description from the photo + the actor's per-user favorite/rating/flag, **never overwriting** an existing
  value), and the active copies to archive; it applies raw SQL (`INSERT … ON CONFLICT DO NOTHING`, a person =
  a box-less `label` marker with a generated `mk…` uid — a new marker has no `faces` row, no cache needed),
  archives (with an `archived_at IS NULL` guard) and writes `audit.ActionPhotosMerge`. A copy this call actually
  archived also leaves its stack via `photos.LeaveStackTx` in the same tx (skipped for an already-archived
  copy, which left its stack back then), so archiving a copy that happened to be a stack's primary does not
  hide that stack's still-live members. **An empty plan = a no-op**
  (it writes nothing → an idempotent re-run on a resolved group); validation `ErrNoKeeper`/`ErrTooFewMembers`/
  `ErrKeeperNotInGroup`/`ErrKeeperNotFound`), `internal/duplicatesapi/`
  (an editor/admin HTTP API over duplicate detection and resolution: the interfaces `Service` (`FindGroups`, satisfied by
  `*duplicates.Service`, **nil → 503**) and `MergeService` (`Merge`/`Preview`, satisfied by `*dupmerge.Service`,
  **nil → 503**); `NewAPI(Config{Service,Merge,RequireWrite})`+`RegisterRoutes` mounts `GET /duplicates`
  and `POST /duplicates/merge` behind `RequireWrite` (listing: `limit`≤100/`offset`, invalid → 400, a failed scan
  → 500; merge: a bad group → 400, a non-existent keeper → 404, the actor from `auth.UserFromContext`);
  mounted in `serve` (`buildDuplicatesAPI` in `cmd/kukatko/duplicates.go`, `Merge` always, `Service` nil when
  `duplicate.enabled=false`)),
  `internal/dupmarkers/`
  (**"one person marked more than once on the same photo"** — a different defect from `duplicates`, which is about
  duplicate *photos*: here it is one photo where the matcher put the same name on two or three neighbouring boxes
  of a group shot, so the people beside her lost their tag and her own face count is inflated. It counts **markers,
  not faces** — several detected faces matched onto one marker is a face↔marker pairing bug in
  `internal/facematch` and a separate task, and mixing the two would mean neither count falls when either is
  fixed. Read-only: it finds the groups, every repair goes through an existing write path.
  `MarkerSource` (`ListRepeatedMarkers`, satisfied by `*Store`)/`DismissalSource`
  (`DismissedDuplicateMarkerGroups`, satisfied by `*feedback.Store`) →
  `Service = New(Config{Markers,Dismissals})`, **`FindGroups(ctx,limit,offset)`** →
  `Result{Groups,Total,Limit,Offset,NextOffset}`; a `Group{PhotoUID,PhotoTitle,TakenAt,Width,Height,
  Orientation,SubjectUID,SubjectName,Markers[]{uid,bbox,score,reviewed}}` with the markers ordered **left to
  right** so a client's numbering reads in reading order, `limit` clamped to `[1,200]` (default 50).
  **`GroupMarkers(rows,dismissed)` is exported and pure**: it keeps only valid (`invalid=false`) `face` markers of
  **named** subjects, groups by (photo, subject), drops the dismissed groups and the ones left under
  `minGroupSize`=2, and orders most-markers-first → by name → by uid. The SQL (`store.go`) applies the same
  predicates plus a `COUNT(*) OVER (PARTITION BY photo_uid, subject_uid) > 1` window so Postgres ships a few
  dozen rows instead of the whole catalogue — an optimisation, **not** the rule, which lives in Go where it is
  unit-tested. The nameless catch-all subject is excluded (it holds thousands of untagged regions and would bury
  every real finding, cf. the 6 767 photos in `docs/API.md`); archived photos are excluded, **non-primary stack
  members are not** — a RAW sibling with the same person twice is the same mistake),
  `internal/dupmarkersapi/`
  (the HTTP surface of the repeated-marker review: `Service` (`FindGroups`, **nil → 503**)/`MarkerStore`
  (`ListMarkersByPhoto`/`GetMarkerByUID`/`SetMarkerInvalidAudited`, satisfied by `*people.Store`)/`Assigner`
  (`Apply`, satisfied by `*facematch.Service`); `NewAPI(Config{Service,Markers,Assigner,RequireAuth,
  RequireWrite})`+`RegisterRoutes` mounts `GET /duplicate-markers` behind **RequireAuth** (reading is not a write)
  and `POST /duplicate-markers/keep` + `POST /duplicate-markers/invalid` behind `RequireWrite`.
  **Neither repair is new behaviour**: `keep` resolves the group **server-side** from (photo, subject) —
  the losing markers are deliberately not in the body, so a stale client list cannot detach a marker that has
  meanwhile been re-tagged — and detaches each one through `facematch.Apply(unassign_person)`, the same
  transition the photo detail and the review game use (subject cleared, `reviewed` cleared, `faces` cache
  refreshed, `face.unassign` audited); `invalid` flips the flag through `people.SetMarkerInvalidAudited`
  (`marker.invalidate`), which changes **nothing but the flag** — the row survives and keeps its subject, so the
  decision is reversible and an invalidation stays distinguishable from an unassignment. A keeper that is not one
  of that person's valid face markers on that photo → **404 before anything is detached**;
  mounted in `serve` (`buildDupMarkersAPI` in `cmd/kukatko/dupmarkers.go`, sharing the photo API's
  `facematch.Service`). The third decision — "leave it be" — is an opinion, not a repair, so it lives with the rest
  of the persisted feedback at `POST`/`DELETE /feedback/duplicate-marker-dismissals`),
  `internal/stacks/`
  (**stack detection and management** — grouping several files of one shot (RAW+JPEG, an exported edit,
  a copy) under one visible **primary** photo, **without merging rows** (the counterpart of `dupmerge`, which
  merges genuine duplicates — stack members are kept on purpose; see `docs/ARCHITECTURE.md` §5.1 + migration
  `0030_photo_stacks.sql`); `Config{Enabled,Rules RuleSet}` + the `Store` interface (satisfied by `*photos.Store`,
  a fake in unit tests: `ListStackCandidates`/`StackInfoByUIDs`/`CreateStack`/`SetStackPrimary`/
  `UnstackMember`/`UnstackAll`); `Service = New(store,cfg)` (panics on a nil store):
  **`DetectStacks(ctx) (created,error)`** (backing `POST /process/stacks`) groups the **not-yet-stacked
  non-archived** photos by the enabled rules — synchronous, incremental and **idempotent**: a re-run over a
  settled library creates nothing and does not touch an existing/manual stack; a no-op (0) when the feature or
  every rule is off; **`StackSelection(ctx,uids)`** groups a selection manually (`photos.ErrStackTooSmall`
  below 2 distinct, `photos.ErrPhotoNotFound` when one is missing/archived), **`SetPrimary`/`Unstack`/`UnstackWhole`**
  delegate to the store; **pure detection** (`rules.go`): four independently switchable rules
  (`RuleSet{BaseName,SequentialCopy,UniqueID,TimeGPS}`, each with a different rate of false matches — a wrongly
  stacked photo is invisible, so the rules link only photos that **plausibly are** the same
  shot) key the candidates (`baseNameKey` the bare stem / `canonicalNameKey` strips a trailing
  `(2)`/`copy`/`-edited` / `uniqueIDKey` = ImageUniqueID/InstanceID / `timeGPSKey` = the second + GPS),
  `Group` merges them with **union-find** (`unionfind.go`) into connected components ≥ 2, deterministically for a
  fixed input order; **picking the primary** (`primary.go` `PickPrimary`): a still before a video (live
  pairing shows the photo, not the clip), a rendered image (JPEG/HEIC) before a camera RAW (`rawExtensions`), then
  the higher resolution, then the larger file, tie-break the smaller uid), `internal/system/`
  (the aggregation of the instance's operational state for the admin **system-status dashboard** — no new data, just
  a merge of the existing subsystems; all behind the small interfaces `DBPinger` (`database.DB`)/
  `EmbeddingHealth` (`embedding.Client.Healthy`)/`JobCounter`
  (`jobs.Store.CountsByTypeState`/`CountPending`)/`ImportLister` (`importer.Store.LatestRun`)/
  `BackupReporter` (`backup.Service.Status`, **nil = not configured**)/`MapsReporter`
  (`mapy.Health.Snapshot`, **nil = no mapy.com key**)/`GeocodeReporter`
  (`placesjob.WindowBudget.Snapshot`, **nil = no mapy.com key**)/`DashboardCounter` (`Store.CountDashboard`)/
  `DuplicateCounter` (`duplicates.Service.CountGroups`, **nil = detection off**) → unit-testable with fakes
  without a DB; `Service` = `New(Config{DB,Embeddings,EmbeddingURL,Jobs,Backup,Maps,Geocode,Imports,Library,Charts,
  Dashboard,Duplicates,OriginalsPath,CachePath,StorageTTL,LibraryTTL,ChartsTTL,DashboardTTL,DuplicateTTL,
  DuplicateTimeout,Clock})`; **`Collect(ctx) (Status,error)`** gathers
  `Status{Version,Database,
  Embeddings,Jobs,Backup,Imports,Storage,Maps,Geocode,Library,Remaining}`: embeddings online/offline, the queue
  (**one** `CountsByTypeState` scan folded into `ByTypeState` + the `ByState`/`ByType`/`Total` sums over it, so the
  three views cannot disagree; `ByType`/`Total` are **lifetime** tallies because the queue keeps finished jobs —
  the UI labels them "ever run" — while `dead_letter` and `pending_embeddings` = queued+running
  `image_embed`/`face_detect` are the actionable ones), the backup state+last
  result, the last import per source, the storage (the size of the originals+cache by a walk, free/total space via
  `statfs` through `golang.org/x/sys/unix`, **memoized** by `storageCache` for `defaultStorageTTL` 30 s so that
  polling does not keep walking the tree), DB reachability (`Ping`, a **sanitized** error), **maps**
  (`Maps{Configured,State,Degraded,Detail,CheckedAt}` from `mapy.Health` — the last observed state of the
  proxy, no probe or credit of its own; `key_rejected` = mapy.com is rejecting the key → `degraded`, visible
  on the dashboard without opening the map), **geocode credits**
  (`Geocode{Configured,BudgetEnabled,Limit,Spent,Remaining,WindowSeconds,ResetsAt}` from
  `placesjob.WindowBudget` — what the current budget window has spent on reverse geocoding and when it
  refills, so an import's metered mapy.com spend is watchable while it happens; `Configured:false` = no key,
  `BudgetEnabled:false` = the cap is switched off), version/commit; errors while
  reading the queue/imports (which require the DB) → an error (500), an unreachable DB and unreadable storage are handled inline
  best-effort; **`LatestRuns(ctx)`** → the newest run of **every** `importer.AllSources()` keyed by source
  (a source that never ran is **absent**, not a zero run), which `collectImports` picks the dashboard's two
  migration paths out of and `/metrics` exports whole — one implementation, not two;
  the **dashboard half** of the same snapshot answers the two questions the page is opened with, from one
  `Store.CountDashboard` round trip (`store_dashboard.go`, scalar subselects like `countLibrarySQL`) memoized by
  `newDashboardCache` for `defaultDashboardTTL` 30 s: `LibrarySummary{Photos,Videos,Trashed,Hidden,Private,
  Uploads{Day,Week,Month,Year},Albums,Labels,People,Faces,Embeddings,LibraryBytes,TrashBytes,DerivedBytes}` —
  `Photos` is the browsable catalogue (= `Library.PhotosLive`), the uploads are counted on `created_at` against
  the **database** clock (so the four windows of one snapshot agree) and include archived photos, and the two byte
  sums are the catalogue's own `sum(file_size)`, which is the only meaningful size when the originals live in an
  object store and the disk walk therefore measures nothing; `DerivedBytes` is filled from
  `StorageUsage.CacheBytes` because derived media is never in the catalogue — and
  `RemainingWork{FacesUnassigned,Clusters,PhotosWithoutTakenAt,PhotosWithoutGPS,PhotosWithoutPlace,
  PhotosWithoutOCR,DuplicateMarkers,Duplicates}`, cheap SQL throughout (the OCR predicate matches
  `idx_photos_ocr_pending` exactly; `DuplicateMarkers` mirrors `dupmarkers.GroupMarkers` — valid face markers of a
  named subject on a live photo, grouped by (photo,subject), minus the dismissed pairs) **except**
  `Duplicates`: `DuplicateScan{Configured,Available,Groups,ComputedAt}` comes from `asyncCache[int]`
  (`async.go`), which never computes on the request path — a read returns the last finished scan and schedules
  the next one in the background under its own timeout (`defaultDuplicateTTL` 15 min /
  `defaultDuplicateTimeout` 2 min, a failure keeping the last good value and backing off a whole TTL), because the
  near-duplicate scan is the one aggregation here that walks the HNSW index; `Available:false` means "no answer
  yet", not zero groups, and a failed scan never fails the snapshot;
  alongside the operational snapshot it also aggregates the **library statistics** for every logged-in user:
  `LibraryCounter` (`CountLibrary`, satisfied by its own `Store` = `NewStore(pool)` — a single query of scalar
  subselects `countLibrarySQL`, **not** a `maintenance scan` over the tree; partial indexes for archived/video/live,
  semi-joins on the `embeddings` PK / the unique `faces`, one CTE for the album types),
  **`LibraryStats(ctx) (Library,error)`** returns
  `Library{Photos,Videos,LivePhotos,Images,PhotosLive,PhotosArchived,PhotosHidden,PhotosStacked,PhotosListed,
  PhotosWith(out)Embedding,PhotosWith(out)Faces,
  PhotosWithGPS,PhotosGeocoded,PhotosPendingGeocode,Embeddings,Faces,FacesAssigned,FacesUnassigned,Subjects,
  SubjectsPerson/Pet/Other,Markers,
  MarkersAssigned/Unassigned,Albums,AlbumsManual/Folder/Moment/State/Month,Labels}`
  — the store returns **only raw counts**, the derived values (the live split, `Images` = total − video − live
  because the `media_type` index deliberately excludes the majority value, + the coverage gaps, clamped to 0) are computed by
  `Library.derive()`; **`PhotosListed` is the number the library grid reports**, `PhotosLive` is not:
  `PhotosLive` = `PhotosListed` + `PhotosHidden` (not archived, `hidden_from_library`) + `PhotosStacked` (not
  archived, **not hidden**, a non-primary stack member) — three disjoint parts whose predicates mirror
  `photos.hiddenClauses`/`photos.stackClauses`, so the walk down lands exactly on `photos.Store.Count` with
  default `ListParams` and `/stats` can say why it and the library disagree instead of leaving the reader
  to work it out; `PhotosGeocoded` counts `photo_places` rows **with** coordinates (a row without them
  only records a GPS-less photo as processed) and `PhotosPendingGeocode` the live geotagged photos with no row
  at all — the outstanding metered mapy.com spend; `PhotosWithGPS` (photos with coordinates of their own, whatever
  the source — the numerator of the "on the map" coverage meter, deliberately **not** `PhotosGeocoded`) and
  `FacesAssigned` (detected faces that name a subject — unlike `MarkersAssigned` it excludes hand-drawn label
  boxes) are the two counts the coverage meters need; **faces and markers are two grains that never add
  across** — `Faces` = `FacesAssigned` + `FacesUnassigned` (one row per detection) while `Markers` =
  `MarkersAssigned` + `MarkersUnassigned` (boxes drawn on photos), the sets overlap and most detections never
  became a marker, so `FacesUnassigned` (derived, `faces.subject_uid IS NULL`) is the naming backlog the review
  game / clustering / candidate search actually work over and `MarkersUnassigned` is not;
  `newLibraryCache` memoizes for `defaultLibraryTTL` 30 s and
  **caches only a success**
  (an error goes out, the page must not render zeros as real counts); a nil `Library` → `errNoLibraryCounter`
  instead of a panic;
  beside the counts it aggregates the **chart series** of the same statistics page: `ChartCounter`
  (`AggregateCharts(ctx,since)`, satisfied by the same `Store`) runs five grouped queries in
  `store_charts.go` — capture years, arrivals per month (bounded by `since`, so the range predicate stays
  sargable on `idx_photos_live_created_at`), the top `topCameras`=10 cameras (make+model grouped, folded into a
  display name by `cameraName`, the bare model kept for the library's `camera` filter), the bytes per media
  bucket (image/live/video + **raw**, recognized by the extension list `imgconvert.RAWExtensions()` and carved
  out of the images so the buckets stay disjoint) and the bytes per year of addition; every query counts the
  **browsable** library (`archived_at IS NULL`, so a bar matches the library view it links to) and buckets time
  **in UTC** (`AT TIME ZONE 'UTC'`, so buckets do not shift with the server's zone); the generic `collect[T]`
  helper is the single query/scan/iterate loop behind all five;
  **`LibraryCharts(ctx) (Charts,error)`** returns `Charts{PhotosByYear,AddedByMonth,TopCameras,StorageByMedia,
  StorageByYear}` after `Charts.fill(now)` turns the raw series into drawable ones — empty years restored on both
  year axes (a histogram whose gaps are merely missing draws a lie), the month window padded to exactly
  `monthWindow`=12 months ending with the current one, every media bucket present in `mediaBuckets` order, the
  running `CumulativeBytes` accumulated, and every slice non-nil (`[]`, never `null`); memoized by
  `newChartsCache` for `defaultChartsTTL` **5 min** — ten times the counts' TTL, because a century-long histogram
  does not move in five minutes and these aggregates are the more expensive of the two — off the **same clock**
  the month window is derived from, so a cached snapshot's twelve months are the twelve that were current when it
  was computed; a nil `Charts` → `errNoChartCounter`; both memoizations are the one generic `snapshotCache[T]`
  (`cache.go`, success-only, never serving past its TTL) — the storage-usage cache deliberately stays its own,
  because it caches failures too), `internal/systemapi/`
  (the HTTP API over the system state: the `StatusCollector` interface (`Collect`+`LibraryStats`+`LibraryCharts`,
  satisfied by
  `*system.Service`, fakeable); `NewAPI(Config{Service,RequireMaintainer,RequireAuth})`+`RegisterRoutes`
  mounts `GET /system/status` behind `RequireMaintainer` (the whole dashboard snapshot — the operational
  sections plus `Library`/`Remaining`; a failed collect → 500),
  `GET /system/stats` behind `RequireAuth` (the library counts for **every logged-in user**; a failed aggregation → 500,
  never a body of zeros) and `GET /system/stats/charts` behind `RequireAuth` (the chart series; a failed
  aggregation → 500, never empty series, which would draw as an empty library) — two endpoints rather than one
  because the counts are cheap and are what an import is watched with, while the charts are heavier and change
  slowly, so they get their own longer memoization and never hold the numbers back; always mounted
  (`buildSystemAPI` in `cmd/kukatko/system.go`, which builds its own stateless embeddings client just for the
  Healthy probe, shares the pool for the job/import/library stores, and passes the backup service nil-safely; mounted
  in `appendOpsAPIs` next to backup/restore)), `internal/capabilitiesapi/`
  (an all-authenticated HTTP API of what the instance is — its feature flags and the build it runs: the
  `Reachability` interface (`Reachable() bool`,
  satisfied by `*reachability.Checker`, fakeable); `NewAPI(Config{Embeddings,Build,RequireAuth})`+
  `RegisterRoutes` mounts `GET /capabilities` behind `RequireAuth` → `{semantic_search:bool,
  version:version.Info}` — the flag read
  from the cached probe result (never a live probe, so it is cheap and every logged-in user may read it — unlike the
  maintainer-only `/system/status`), the build injected as a value (`version.Get()` at wiring, so tests pin
  it) and reported verbatim, `dev`/`none` placeholders included. The build lives here, not in the frontend
  bundle: the bundle is `//go:embed`-ed into this binary, so a version compiled into it would drift from the
  binary that serves it — read from the server it cannot. The shape is deliberately open for future flags;
  mounted **always**
  (`buildCapabilitiesAPI`+`buildReachabilityChecker` in `cmd/kukatko/capabilities.go`)),
  `internal/query/`
  (a pure **parser of the search language** `q=` — free text + `key:value` filters in one string
  (`dovolená camera:"Canon EOS R6" iso:100-400 faces:2`), no I/O: `Parse(input) Query` **never
  fails** — the tokenizer honours quotes and `\` escapes, the operators `|` (OR between alternatives of a
  value), `!` (NOT per alternative), `-` (NOT of free text), the ranges `lo-hi` with open ends
  (`800-`, `-200`), `*` as a wildcard in text (**an escaped or quoted asterisk is a literal** —
  `title:foo\*bar` searches for an asterisk); the filter registry `specs` (Key → Kind
  text/number/date/bool/enum/id/count + bound validation: rating 0–5, month 1–12, year 1000–9999, …)
  with the aliases `subject:`→`person:`, `keyword:`→`keywords:`; **`text:`** matches the text a recogniser
  read *inside* the photo (`photos.ocr_text`) and is deliberately its own key rather than folded into
  `description:`/`notes:` — OCR output is machine-read and noisy, and someone hunting for the photo of a
  particular shop wants to say so; the bool keys include
  `hidden:` (photos hidden from the library — like `archived:`, using it lifts the store's default scope,
  so `hidden:yes` is the way back to a hidden photo); the id key **`uid:`** names exactly one photo — by its own
  uid **or** by the source uid it was imported under, one key for both because the two shapes cannot collide
  — and lifts the live-only, visible-only **and** stack-primary scopes at once (`uidLookup` in
  `store_list.go`), because naming an id is explicit intent and silence about a photo that exists is the one
  useless answer; **an unknown key or an invalid value
  degrades the whole token to free text** and is reported in `Query.Unknown` (the UI builds a hint from it,
  the API `unknown_tokens`). AST: `Query{Terms,Filters,Unknown}`, `Term{Text,Phrase,Not}`,
  `Filter{Key,Values}`, `Value{Not,Text,Pattern,Bool,Min,Max,From,Until}` (numeric bounds / half-open
  date intervals; `Pattern` on KindText carries the `*`-literalness that `Text` loses — it is read via
  `Value.TextPattern()`, fallback to `Text` for values built in code); renderings `FreeText()` (websearch syntax for FTS incl. phrases and `-` negations),
  `PlainText()` (the positive terms for an ILIKE substring and for the embedding query), `NotTerms()`,
  `HasFilter(key)`. The AST is compiled into SQL by `internal/photos/store_query.go` (`queryClauses` — a map of
  builders per key, everything through bind parameters; per-user filters scoped to `RatedBy`, `near:`
  a spherical distance with the radius `dist:` default 5 km, `faces:` counts non-invalid face markers,
  every **decimal** bound (both an exact one and the ends of a range) allowed ±0.005 because of float4, integer bounds
  stay exact; `likePattern` makes a wildcard only out of an unescaped `*` and escapes `%`/`_`, just like
  the substring filters `Search`/`?camera=`/`?lens=` in `store_list.go`). The package also owns the **uid
  router** (`uidref.go`, pure, no I/O): `ClassifyUID(token) (UIDRef, bool)` reads a uid's two-letter prefix and
  says what it names — `ph` photo, `al` album, `lb` label, `su` subject (`EntityPerson`), `st` stack, `mk`
  marker, all 26 characters of lowercase base32, plus `pt` = an **imported** photo uid at 16 characters of
  base36, a length that keeps the two families apart; `FindUID(input)` returns the first uid-shaped word of an
  input, so an id pasted with a word beside it is still recognised. A token with an **unknown** prefix is
  deliberately **not** accepted — probing every table per keystroke buys nothing. `internal/globalsearchapi`
  routes a pasted id with it. The user-facing grammar: docs/API.md
  "Search language (q=)". **`person:me` and `uploader:me` are deliberately *not* resolved here** — the parser
  knows nothing about who is asking, which is what keeps a filter's meaning independent of the request that
  carried it (`uploader:none`, which says nothing about the caller, is compiled by the store instead);
  see `internal/personme`), `internal/personme/`
  (resolution of the words of the query language that mean something different to every caller:
  **`person:me`** and **`uploader:me`**. `Resolve(filters, linkedSubjectUID) (used, resolved bool)` rewrites every `person:`
  alternative whose text is exactly `me` — the negated `!me` included — to the caller's linked subject UID
  (both `Text` and `Pattern`, so the compiled LIKE cannot still match a person *named* "me"), in place.
  `resolved` is false only when the token was used by an account with **no** link, which the caller answers
  with an empty result and a reason: `internal/photoapi` sets `photos.ListParams.MatchNone` and returns the
  notice `person_me_unlinked`, the MCP search tool returns an error naming the fix. Never "everything", never
  a free-text search for the word "me". The **collision** with a subject actually called "me" is decided for
  the token, but only in its exact lower-case spelling: `person:Me` stays an ordinary (case-insensitive) name
  match, and a UID always reaches a subject that no name can shadow.
  `ResolveUploader(filters, callerUID)` is the simpler twin for `uploader:me`: it rewrites the token to the
  **account making the request**, which every surface that resolves it has in hand, so it cannot fail and
  reports nothing; an empty `callerUID` is left alone rather than rewritten, since an empty uid would
  compile to a pattern matching every uploader. Pure, no I/O, no `auth` dependency —
  it takes the linked UID, not a user), `internal/ratelimit/`
  (a reusable **per-key token-bucket rate limiter** + HTTP middleware for expensive endpoints:
  `New(ratePerSec, burst)` → `Allow(key)` (lazy refill, a bucket per key) / `Cleanup`/`RunMaintenance`
  (cleaning up fully refilled buckets) / `Middleware` (chi-compatible, keyed by the **client IP** via
  `clientIP` = `clientip.FromRequest` — the address `internal/clientip` resolved, **not** a header the caller
  chose, so nobody mints a fresh bucket per request; an empty bucket →
  **429** + `Retry-After`); `ratePerSec ≤ 0` → a **disabled** limiter (Allow always true, Middleware a
  no-op — the endpoint is switched off cleanly by config); memory-bounded by an opportunistic cleanup at `maxBuckets`
  (8192), so it needs no external goroutine; mounted as the outermost middleware ahead of auth on
  `POST /upload` (ingest), `POST /photos/bulk` (bulkapi), `POST /import/*` (importapi) and
  `GET /map/tiles/...` (mapsapi) — the limits come from the `ratelimit.*` config; login and geocode have their own
  limiters. `KeyedMiddleware(keyFn)` is the same middleware with the bucket key chosen by the caller instead
  of the IP; `Middleware` is now `KeyedMiddleware(clientIP)`. It exists for `POST /photos/{uid}/comments`,
  which is throttled **per user** and therefore mounted *inside* the auth guard (the principal is only on the
  context once auth has run) — a household behind one NAT is one address but many people. A key function
  returning `""` is not special-cased: every such request shares one bucket, the safe reading of "no identity
  to attribute this to"), `internal/clientip/`
  (**who a request actually came from** — the one answer the rate limiters, the audit trail and the access log
  all key on. A forwarding header is request data like any other, so chi's `middleware.RealIP`, which believed
  `True-Client-IP`/`X-Real-IP`/`X-Forwarded-For` from **anybody**, let an anonymous caller rotate a header and
  hand itself a fresh limiter bucket per request — the brute-force bypass of SEC-001. This package replaces it:
  `Set` (a list of trusted-proxy networks; `ParseSet` accepts CIDR blocks, single addresses and the keywords
  `loopback`/`private` — the latter deliberately **without** the `100.64/10` Tailscale range, which carries
  clients rather than proxies; a nil/empty `Set` trusts nothing), `Middleware(set)` (resolves the address once
  per request onto the context), `FromRequest(r)` (reads it back) and `Peer(r)` (the socket host, headers
  ignored). Resolution: the socket peer, unless that peer is in the set, in which case the `X-Forwarded-For`
  chain is walked **right-to-left** and the first hop that is not itself a trusted proxy wins (all-trusted →
  the leftmost; no chain → `X-Real-Ip`; nothing parseable → the peer). `True-Client-IP` and the vendor
  variants are never read at all — no proxy here sets them, and a header nobody writes is one nobody has to be
  trusted about. `FromRequest` **without** the middleware falls back to the socket peer, so a handler mounted
  on another router (a test) fails safe rather than open. Mounted by `internal/server` right after
  `RequestID`, configured from `web.trusted_proxies`), `internal/obs/`
  (structured logging + request-scoped plumbing: a slog **JSON** handler at a configurable
  level (`ParseLevel`/`NewLogger`/`Setup`, `log.level`, an invalid level → an error at startup),
  a **redaction `ReplaceAttr` hook** (`redactAttr`) blanks the value of every attribute whose key carries a
  secret (password/passwd/secret/token/apikey/access_key/secret_key/authorization/cookie/
  credential/dsn) to `[REDACTED]` — inside groups too, so a secret never escapes into the log, not even when
  somebody logs it by mistake; the **`AccessLog` middleware** emits one structured line per HTTP
  request (the request id from chi's `RequestID`, method/path/route pattern/status/duration/bytes/remote IP
  + the authenticated user when known — the auth middleware stamps it via `SetUser` into a
  request-scoped `fields` bag shared by pointer through the context, because a write deep in the chain must
  be visible to the top-level logger); the level follows the status (5xx=error, 4xx=warn, otherwise info), the `/metrics` scrape is
  skipped, and the request id is mirrored both into the `X-Request-Id` header and into the shared route label of the metrics),
  `internal/geoestimate/`
  (**estimating a missing location from photos taken close in time** — a photo without GPS (a camera without a
  receiver, a scan, a cropped export) was very often taken on the same day in the same place as photos that
  do have coordinates; the package rests on a single rule: **a wrong location is worse than none** — not only does it
  look bad on the detail, it silently poisons the map, the place hierarchy and every `near:` search over them, and it
  looks just as trustworthy as a measured coordinate, so the estimator **much rather refuses than
  guesses**; the **pure core** (`estimate.go`, no DB): `Point{Lat,Lng}`, **`Estimate(neighbours,
  radiusMeters) (Point,bool)`** = the centroid of the neighbours + a check that **every** one of them is within `radiusMeters`
  of it (a single outlier brings the whole set down — an intended failure: the price of a refusal is an empty
  field, the price of a bad guess is a lie the user has no reason to question; **no** clustering and no
  voting, because "the majority agrees" is a different and far weaker claim than the one the UI would make on its behalf),
  **`DistanceMeters`** (haversine); a set spanning ±180° gets a centroid in the middle of the Pacific → incoherent
  → nothing, which is the right answer for the wrong reason and is left alone; the **service** (`service.go`):
  `Config{Store,Enqueuer,Window,RadiusMeters}` (non-positive → `DefaultWindow` 6h /
  `DefaultRadiusMeters` 5000 — 6h keeps a photo inside one trip, not inside one calendar day, which is
  exactly the case (Brno in the morning, Vienna in the evening) where a same-day estimate is wrong), the `Store` interface (satisfied by
  `*photos.Store`: `ListLocationCandidates`/`ListLocatedNeighbours`/`SetEstimatedLocation`), `Enqueuer`
  (satisfied by `*jobs.Enqueuer`, it **may be nil** = the estimates are stored but not geocoded);
  **`BackfillLocations(ctx) (estimated,error)`** (backing `POST /process/locations`): for every
  candidate it loads the neighbours in the window, calls `Estimate`, and on a match writes via `SetEstimatedLocation`
  (a **guarded UPDATE** — when the photo has gained a location in the meantime or the user has decided it, the estimate **loses**
  and is dropped) + schedules a `places` job **only after the write**, so that `placesjob` sees the new coordinates and
  does not skip the geocode as "already current"; the neighbours exclude estimates (`location_source <> 'estimate'`), so that
  one guess does not propagate through the library, where every hop would look exactly as confident as the last; a photo without
  neighbours / with incoherent neighbours = `(false, nil)`, **not an error** — a refusal is a normal outcome;
  idempotent and resumable **without a cursor** (the candidate set shrinks as the work is done, so a re-run *is* the
  resume), wiring `cmd/kukatko/geoestimate.go` (`buildGeoEstimateServiceOrNil` /
  `locationEstimatorOrNil` — a nil **interface**, not a typed-nil pointer, so that processapi returns 503);
  **the reviewer** (`review.go`) is the other half of the feature: marking a location `estimate` is only useful
  if somebody eventually rules on it, and the estimator itself never revisits an estimated photo (it has stopped
  being a candidate), so without this the guess would sit on the map forever wearing a question mark.
  `NewReviewer(ReviewConfig{Catalogue,Places})` (panics on a nil catalogue; `Places` may be nil, which leaves
  every guess unnamed), `Pending(ctx,offset,limit) ([]Pending,total,error)` (one rotating window of the
  photos carrying an estimate + the full size of that set, each with its cached `photo_places` row resolved in
  **one** bulk read; `Pending.PlaceLabel()` = place name → city → region → country, `""` when nothing is
  geocoded) and the two verdicts `Accept`/`Reject`. Both do a **read-modify-write** through
  `photos.UpdateMetadataAudited` — the same path a hand edit on the photo page takes, because
  `UpdateMetadata` replaces the whole editable record and building the update from anything but the live row
  would blank a title somebody typed in meanwhile. Accept keeps the coordinates and promotes
  `location_source` `estimate`→`manual`; Reject clears them and stamps `manual` anyway, the **tombstone** that
  keeps the nightly backfill from handing the same guess straight back. A photo whose location is no longer an
  estimate is left untouched (the verdict was about a guess that no longer exists), which also makes both
  idempotent. The only thing that differs from a hand edit is the audit action — `location.confirm`/
  `location.reject` rather than `photo.update` — so the decision is **countable** on the review leaderboard and
  findable in the admin decision view, where a diff would not be. It backs the review game's place check
  (`cmd/kukatko/review.go` `buildPlaceReviewerOrNil`, nil when `location_estimate.enabled=false`)),
  `internal/metrics/`
  (Prometheus instrumentation of the HTTP server, the queue worker and the infrastructure (pgx pool, embeddings sidecar,
  imports, thumbnails), namespace `kukatko`; an **isolated `*prometheus.Registry`** instead of the
  process-global `DefaultRegisterer`, so tests build independent metric surfaces without cross-test
  leakage; `New()` → `Registry` registers HTTP (`kukatko_http_requests_total` counter + a latency
  histogram + an inflight gauge, the route label = the **chi route pattern**, never a raw URL), the job lifecycle
  (started/finished counter + a duration histogram by type/outcome), embeddings (a duration histogram +
  an up gauge), import progress (a gauge per source/outcome), thumbnail duration and the geocode credit
  counter (`kukatko_geocode_credits_spent_total` — metered mapy.com money, so the spend of a running import
  is watchable, not inferred from the bill) + the standard
  `go_`/`process_` collectors; the **pull-at-scrape collectors** `RegisterDBPool` (live pgx pool stats),
  `RegisterJobQueue` (`QueueDepthFunc` → `map[QueueCell]int` keyed by (type,state); the collector folds that
  one breakdown into `kukatko_jobs_queue_depth{state}`/`_by_type{type}`/`_by_type_state{type,state}`, so the
  three families are sums of each other and one query per scrape answers all of them; `collectTimeout` 5 s,
  so a slow DB does not block the scrape) and `RegisterGeocodeBudget` (`GeocodeBudgetFunc` →
  `kukatko_geocode_credits_remaining`/`_limit`, sampled at scrape time so the gauge follows the budget
  window rolling over even while no job runs; a nil func = no budget wired) read their data at scrape time
  without extra goroutines; `RegisterLibrary(LibraryStatsFunc, ttl)` (`library.go`) adds the **library-content**
  gauges — `kukatko_library_photos{media_type}`/`_photos_archived`/`_photos_processed{stage}`/
  `_photos_pending{stage}`/`_embeddings`/`_faces`/`_markers{state}`/`_subjects{type}`/`_albums{type}`/`_labels`
  and `kukatko_import_last_run_status{source,status}` (1 for the status the run is in, **0** for every other
  known one, so a transition is visible instead of a series vanishing) + `_start`/`_finish_timestamp_seconds{source}`
  (Unix seconds; the age is `time() - gauge`, not a pre-computed one) — over a `LibrarySnapshot` whose every
  labelled dimension is a `map[string]int`, so a new album/media type needs no change here; the snapshot is
  **memoised** for `metrics.library_ttl` (`defaultLibraryTTL` 1 min) because these are aggregates over the
  largest tables and a scrape arrives forever — without it `/metrics` becomes the load it reports on; a stale
  value is **never** served past the TTL: a failure exports no library gauges (a gap, not a number the library
  no longer has) and bumps `kukatko_library_collect_errors_total`; the gauges carry no `_total` suffix on
  purpose, that is reserved for counters. The func is wired in `cmd/kukatko/obs.go`
  (`registerLibraryMetrics`/`librarySnapshot`/`importRuns`) onto `system.Service.LibraryStats`+`LatestRuns` —
  the **same** aggregation the admin dashboard reads, so `/metrics` and `GET /system/stats` cannot disagree;
  `/metrics` is unauthenticated, so only instance-wide aggregates go in it, never a per-user number or a
  photo/album/label/person name as a label value; `Handler()`
  is mounted by `serve` on `/metrics` (the middleware skips that path, a scrape does not instrument itself),
  the observation methods `JobStarted`/`JobFinished`/`ObserveEmbeddingCall`/`SetEmbeddingUp`/
  `SetImportProgress`/`ObserveThumbnail`/`GeocodeCreditSpent` and `Middleware(routeOf)` are handed to the subsystems that
  emit the events; it keeps the lightweight approach — one namespace, limited label sets;
  tunables in the `metrics.*` config), `internal/web/`
  (the SPA fallback handler `web.Handler()`/`SPAHandler` + the `internal/web/static` embed
  `//go:embed all:dist/*`; the Vite build writes into `internal/web/static/dist`, which is
  gitignored except for the committed `.gitkeep`, so that the embed compiles even without a built
  frontend — the build empties that directory and restores the placeholder afterwards
  (`web/build/gitkeep.ts`), because a deleted tracked file makes Go stamp the binary `+dirty`. Two rules serve the PWA: `servedVerbatim` keeps `sw.js` — like everything under
  `assets/` — **out of the SPA fallback**, because a worker script answered with the index document
  fails registration with a MIME error instead of a plain 404; and `contentTypeFor` pins the media
  type of `.webmanifest` to `application/manifest+json`, since `mime.TypeByExtension` reads the
  host's mime database and on most hosts has no answer for it, which would leave `net/http` sniffing
  the manifest as `text/plain`. Everything outside `assets/` — the worker, the manifest, the icons —
  is served `Cache-Control: no-cache`, which is what lets a deployment replace the worker. A third
  rule is the absence of one: the fallback **ignores the request method**, so the share target's
  `POST /share-target` — normally intercepted by the service worker, and reaching the server only when
  none is installed — is answered with the shell and lands on the page that explains the shared files
  did not arrive, where a 405 would show a bare protocol error).
  Details: [`docs/DEVELOPMENT.md`](DEVELOPMENT.md).

- **Mail (`internal/mailer`):** the one way Kukátko sends an e-mail. Its only caller is `internal/mailjob`,
  the `mail_send` worker handler — nothing sends from a request — so the account work that follows
  (registration, approval, password reset) has a single, tested path instead of an SMTP call per handler. Everything goes through the **`Sender`** interface (`Send(ctx, Message) error`), so a
  caller never depends on SMTP: `SMTPSender` in production, **`Noop`** when `mail.enabled` is false (accepts
  everything, delivers nothing — an instance nobody gave a mail server to still works), and **`Fake`** in
  tests, which records what it was handed (`Sent`/`Last`/`Reset`, `FailWith` to exercise the failure path) and
  **never opens a socket**. `Fake` lives in the package, not in a `_test.go` file, because every caller of
  `Sender` wants it. `Message` is `{To, Subject, Body}` — one recipient, plain text, no HTML alternative, no
  attachments, no copies.
  - **The address guard.** The user table holds placeholder addresses in the reserved `.invalid` domain
    (RFC 6761), and mail to one is **refused before anything is dialled**: `ValidateAddress`/`IsPlaceholder` →
    `ErrPlaceholderAddress`, an unparseable or empty recipient → `ErrInvalidAddress`, and only a delivery that
    genuinely failed → **`ErrSendFailed`**. That split is the point of the typed errors: the first two are
    permanent and the caller should stop, the third is worth retrying. `SMTPSender` and `Fake` both apply the
    guard; `Noop` does not, because it attempts nothing and an instance with mail off must not fail a
    registration over an address it was never going to write to.
  - **`SMTPSender`** — `NewSMTP(SMTPConfig{Host,Port,Username,Password,Encryption,FromAddress,FromName,
    Timeout})`, **standard library only** (`net/smtp` + `crypto/tls`), so the binary keeps building with
    `CGO_ENABLED=0`. Three modes: `EncryptionSTARTTLS` (default, port 587), `EncryptionTLS` (implicit, port
    465) and `EncryptionNone` (a local relay). Authentication is optional — an empty username skips `AUTH`
    entirely, which is what an unauthenticated relay on localhost needs. `NewSMTP` fills in the missing port
    (`DefaultPort` 587) and timeout (`DefaultTimeout` 15 s) but refuses a missing host, a missing/unparseable
    sender address or an unknown encryption mode with `ErrIncompleteConfig`. One delivery is **one
    connection** (a few messages a day; a pool would only add a way to hold a socket open to a server that has
    forgotten about it), the context deadline becomes the socket deadline, and TLS is pinned to ≥ 1.2 with the
    configured host as the server name.
  - **The wire format** (`renderMessage`, pure): CRLF headers — `From` (display name encoded by `net/mail`),
    `To`, `Subject` **RFC 2047 Q-encoded**, `Date`, `MIME-Version`, `Content-Type: text/plain; charset=utf-8`,
    `Content-Transfer-Encoding: quoted-printable` — a blank line, then the quoted-printable body. Quoted-
    printable rather than raw 8-bit because it needs no `8BITMIME` from the server and it folds the long lines
    the 998-octet limit would otherwise break; the Q-encoded subject is what makes `Váš účet byl schválen`
    survive a server that only takes ASCII headers.
  - **Four Czech templates**, each a **pure function of its own data struct** returning
    `Rendered{Template,Subject,Body}` (`Rendered.Message(to)` addresses it), unit-tested against the exact
    expected text: `RenderRegistrationReceived` (`RegistrationReceivedData{DisplayName,Username}` — your
    account exists and waits for an administrator), `RenderAccountApproved`
    (`AccountApprovedData{DisplayName,SignInURL}`), `RenderNewRegistrationPending`
    (`NewRegistrationPendingData{Username,DisplayName,Email}` — the administrator's copy, naming all three),
    and `RenderPasswordReset` (`PasswordResetData{DisplayName,ResetURL,ValidFor}`). The `Template*` name
    constants exist so a log or audit line can say which message went out without repeating its subject. Two
    details the tests pin: an empty display name degrades to the impersonal `Dobrý den,` rather than greeting a
    blank, and `ValidFor` is rendered with the **Czech plural rule** in the largest unit it divides evenly into
    (`jednu hodinu`, `2 hodiny`, `7 dnů`), with a non-positive duration falling back to
    `Odkaz má omezenou platnost.` instead of printing a zero.
  - Configuration is `mail.*` (`internal/config`, [`OPERATIONS.md`](OPERATIONS.md)); an enabled mailer with no
    `host`, `from_address` or `base_url` fails startup naming every missing key.

- **Queued mail (`internal/mailjob`):** the delivery path — `internal/mailer` renders and speaks SMTP, this
  package decides **when** a message is scheduled, **whether** it is scheduled at all, and **which** failures
  are worth another try. Sending from the HTTP handler that caused the mail has two faults: the request waits
  on a remote host, and a mail server that is briefly away loses the message. Both go away once the persistent
  queue is the only path.
  - **The payload is a recipe, not a message:** `{template, to, data}` — the template *name* plus its data
    struct as JSON, never rendered text and never a live object, so a job means the same thing after a restart
    or an upgrade. `Handle` decodes it, renders through the generic `renderWith[T]` (which is what keeps a
    template and its data struct paired **by the compiler**, not by convention) and hands the `mailer.Message`
    to the `Sender`.
  - **`Enqueuer`** (`NewEnqueuer(EnqueuerConfig{Enabled,Schedule,Logger})`) is what callers use instead of
    touching the queue: `Enqueue(ctx, exec, Mail)`, where `exec` is a `jobs.Execer` — pass **the transaction of
    the mutation that caused the mail** and the message is scheduled if and only if that mutation commits, the
    way `audit.Write` joins its mutation. Build the `Mail` with `RegistrationReceived`/`AccountApproved`/
    `NewRegistrationPending`/`PasswordReset`, which pair each template with the data it expects.
  - **Two silent refusals, both logged, both returning nil** (they must not fail the mutation that asked):
    `mail.enabled` is false → nothing is enqueued, because an instance nobody gave an SMTP server to must not
    grow a queue that can only fail (`Enabled()` lets a caller skip minting a reset token for it); and a
    recipient in the reserved `.invalid` domain → skipped, since those are the placeholder addresses of
    accounts imported without a real one. **Two refusals with an error**, before any job exists: a recipient
    that is not an address, and a template this binary cannot render.
  - **Failure classification is the whole point of the handler.** A transient failure (the host is down, the
    greeting timed out, a 4yz "try again later") is returned as an ordinary error, so the queue retries with
    its own backoff — `MaxAttempts` is **10** rather than the queue's 5, which with the 30 s→1 h backoff spans
    roughly three hours of outage. A permanent one — `ErrInvalidAddress`/`ErrPlaceholderAddress`, or any SMTP
    **5yz** reply dug out with `errors.As(err, *textproto.Error)` — is a `worker.Terminal` failure: parked in
    `failed` on the first attempt, never retried, still requeueable by hand.
  - Wired in `cmd/kukatko` next to the other handlers and **only when `mail.enabled`** (`buildMailServiceOrNil`);
    with mail off nothing enqueues either, and a job left over from a period when mail was on simply waits in
    the queue until it is configured again. The worker gives `mail_send` its own single-slot pool.

- **Remote CLI client (`internal/ctl`):** the client half of `kukatko ctl` — the one piece of the tree that
  Kukátko calls **over HTTP as a foreign server**, not through the DB and the disk. It has nothing in common with `internal/config`
  (which describes the *server* and knows nothing about a remote endpoint); the only state it owns is the client
  file `~/.config/kukatko/ctl.yaml`. The motivation: cheaper in tokens than the MCP server — no tool schema
  is loaded into the model's context, just a short command and a narrow result. Hence the compact output.
  - `config.go` — `Context{Name,Server,Token}` + `Config{CurrentContext,Contexts}` in the kubectl style.
    `Load(path)` (a missing file = an empty config, not an error — running from env variables alone), `Save(path,cfg)`
    (atomically: a temp at 0600 → `Rename` → `Chmod` 0600, the directory 0700; it **tightens an existing
    world-readable file**, it never writes a token into one as it is). `DefaultConfigPath()` honours `XDG_CONFIG_HOME`.
    `Resolve(cfg, contextName, env)` → `Endpoint`: it picks the context (by name → otherwise `current-context`),
    then `KUKATKO_SERVER`/`KUKATKO_TOKEN` **override field by field**, so `KUKATKO_TOKEN` on its own
    re-credentials the stored context. The errors `ErrContextNotFound`, `ErrNoServer`.
  - `client.go` — `NewClient(server, token)` (validates an absolute http(s) URL → `ErrInvalidServerURL`),
    the internal `get(ctx, path, query)` and `send(ctx, method, path, body)` send
    `Authorization: Bearer <token>` and return the **raw** body (`json.RawMessage`), because `-o json`
    prints the server's bytes unchanged; `204 No Content` returns a `nil` body. Success is the whole `2xx` range —
    the API answers 200, 201 and 204 depending on the endpoint. `401` → `*UnauthorizedError` with a short, actionable message
    (the token is missing / expired / was revoked + how to make a new one); `403` → `*ForbiddenError`, which
    **says that the role is not enough** (mutations want editor/admin, a viewer only reads), instead of printing the server's
    `insufficient permissions`. It **never** prints the body or the token; another non-2xx → `*StatusError`
    with the server's `{"error":…}` text (otherwise a limited excerpt of the body). The body is read through `io.LimitReader`,
    timeout 30 s.
  - `photos.go` — `ListPhotos`/`GetPhoto`/`SearchPhotos` + `DecodePhotoPage`/`DecodePhotoDetail`.
    **The decoder is per-resource on purpose:** the API has no uniform list envelope (`photos` returns
    `{photos,total,limit,offset,next_offset}`, the other resources a bare list) and we must not unify it —
    it would break the frontend. `ListOptions` (limit/offset/sort/order/year/album/label/favorite/archived)
    is validated locally (`ErrInvalidPaging`/`ErrInvalidYear`/`ErrInvalidArchived`), so a typo does not
    cost a round trip. **The API does not know `--year`** — it is translated into the inclusive range
    `taken_after`/`taken_before` (`taken_at >= … <= …`), the upper bound being the last instant of 31 December.
    `SearchOptions` adds `q` + `mode` (`fulltext`/`semantic`/`hybrid`).
  - `albums.go` — `ListAlbums`/`GetAlbum`/`CreateAlbum`/`AddAlbumPhotos`/`RemoveAlbumPhotos`
    + `DecodeAlbums`/`DecodeAlbum`/`DecodePhotoUIDs`. The envelope is a **bare `{"albums":[…]}` without paging**
    — hence its own decoder. `PhotoCount` is filled only by the list; the detail does not send it, so the renderer does not print it.
    `AlbumInput` is validated locally (`ErrEmptyTitle`, `ErrInvalidAlbumType`); membership sends the whole
    list of uids in **one** request and the server returns the refreshed order.
  - `labels.go` — `ListLabels`/`GetLabel`/`CreateLabel`/`AttachLabel`/`DetachLabel` + `DecodeLabels`/
    `DecodeLabel`. The envelope is a **bare `{"labels":[…]}`** ordered by priority (a third shape). Attach/detach
    answer `204`. An empty `source` is dropped from the body so that the server fills in its own `manual`
    (`ErrInvalidLabelSource`, `ErrEmptyName`).
  - `subjects.go` — `ListSubjects`/`GetSubject`/`SubjectPhotos` + `DecodeSubjects`/`DecodeSubject`.
    The envelope is a **bare `{"subjects":[…]}`**; a subject's gallery, however, **has the `/photos` shape**, so it is read by
    `DecodePhotoPage` (the same shape, not a unified one). `PageOptions` offers only limit/offset — the endpoint does not
    read the catalogue filters, so ctl does not offer them either.
  - `curate.go` — `ListFavorites` (the `/photos` envelope, the `favorite` parameter is dropped: the endpoint scopes
    itself), `AddFavorite`/`RemoveFavorite`, `SetRating`/`ClearRating`. Favorites and ratings are both
    **per-user**, so even a viewer may change them. The stars and the flag are independent indicators — whatever you send as `nil`
    the server leaves alone (`ErrEmptyRating`, `ErrInvalidRating`, `ErrInvalidFlag`).
  - `bulk.go` — `Bulk(ctx, photoUIDs, ops)` sends **one** `POST /photos/bulk` for the whole batch, because
    the server applies it in a single transaction; a loop over the photos would trade that atomicity for N transactions and N
    audit rows. `BulkOperations` has tags 1:1 with the API (the endpoint rejects unknown fields) and everything `omitempty`,
    so that a zero value is not sent as a real change. `Validate()` mirrors the server-side checks (mutually
    exclusive set/clear pairs, the star range, the flag, the coordinates) → `ErrNoOperations`,
    `ErrConflictingOperations`, `ErrInvalidLocation`. `DecodeBulkResult` reads `{results,counts}` (a fourth
    shape). `ParseLocation("lat,lng")`.
  - `uids.go` — `ParsePhotoUIDs(r)` reads a set of photos from stdin in **four** shapes: the envelope
    `{"photos":[…]}` (exactly what `ctl photos list -o json` prints), a bare JSON array of uids, a bare array of
    objects with a `uid`, or a plain whitespace-separated list. `NormalizeUIDs` trims, drops the empty ones
    and **deduplicates** (so that the count in the confirmation prompt matches what is actually sent) →
    `ErrNoPhotoUIDs`. `ConfirmThreshold = 50` is the boundary above which the command asks.
  - `output.go` — `ParseFormat` (`table`/`json`; **deliberately no `yaml`**), `WriteJSON` (echoing the bytes
    unchanged), the shared `writeTable`/`writeKeyValues`/`writeLine`, `WritePhotoPage` (a table + one summary
    line: how many out of how many, `offset`, `next offset`, and for a search the effective `mode` and any `degraded`),
    `WritePhotoDetail`, `WriteContexts` (**the token is never printed**, only `stored`/`not set`).
    An empty result = the single line `no photos found`, no header — so that an agent does not mistake a header
    for a row.
  - `render.go` — `WriteAlbums`/`WriteAlbum`, `WriteLabels`/`WriteLabel`, `WriteSubjects` (both counts,
    `PHOTOS` and `MARKERS`, because they answer different questions)/`WriteSubject`,
    `WriteMembership` (one line: how many photos the album now holds), `WriteBulkResult` (a summary + a table of
    the failed photos **only**) and `WriteAck`. `Ack` is the only payload the CLI **makes up itself**: where
    the API answers `204` there is nothing to pass through unchanged, so `-o json` gets
    `{"status":"ok","message":…}` and the pipeline can tell success from failure.

  The command tree, the configuration file and the `kukatkoctl` symlink are described in
  [`docs/OPERATIONS.md`](OPERATIONS.md).
