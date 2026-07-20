# HTTP API

Descriptive reference overview of the HTTP endpoints under `/api/v1`. **These are not rules** —
the rules live in [`CLAUDE.md`](../CLAUDE.md). Record any new or changed endpoint here.

<!-- BODY BEGIN -->
- **Auth API (`/api/v1`):** `POST /auth/login` (set HttpOnly+SameSite=Strict cookie + opaque
  `download_token`), `POST /auth/logout`, `GET /auth/me`, `POST /auth/password` (revokes other
  sessions). Admin-only: `GET|POST /admin/users`, `PATCH /admin/users/{uid}`,
  `POST /admin/users/{uid}/disable`, `POST /admin/users/{uid}/password` (reset revokes all of the
  user's sessions). Responses of the admin user endpoints carry a free-form **`note`** alongside
  `display_name` (an admin note on why the account exists / who it is). Both fields are optional,
  defaulting to the empty string. A `note` longer than **1000 characters** (runes, not bytes) → 400
  with a message naming the field. `PATCH` gives `note` **partial-update** semantics: an omitted key
  leaves the stored note unchanged, `""` clears it. **Only an admin reads `note`** — it is never in the
  `POST /auth/login` or `GET /auth/me` payload. Roles: a **strict ladder** viewer < editor < admin <
  maintainer (each inherits the rights of the lower ones): viewer read-only, editor adds writing of
  media/metadata, admin governance (user management, audit log, permanent deletion / emptying the
  trash), maintainer operations (imports, maintenance, system, backup/restore, jobs, process).
  A **username** longer than **64 characters** (runes, not bytes) → 400 with a message naming the
  field, on both `POST /auth/login` and `POST /admin/users`; login checks it *before* the username
  reaches the rate limiter, so the public endpoint cannot be flooded with oversized limiter keys.
  **Sliding session expiry**
  (`auth.session_ttl` up to the cap `auth.session_max_lifetime`), **login rate-limit**
  (`auth.login_rate_limit`/`auth.login_rate_window` → 429), **bootstrap admin** from
  `auth.bootstrap_admin_username/password`. In addition, the middleware `RequireAuthOrDownloadToken`
  (session cookie or `?t=download_token` via `Service.AuthenticateDownloadToken` →
  `Store.GetSessionByDownloadToken`) for media without a cookie.
- **API tokens (`/api/v1/auth/tokens`, all behind `RequireAuth`):** long-lived bearer credentials for
  non-interactive clients (CLI, scripts, agents). `POST /auth/tokens` (`{name, expires_at?}`) → 201
  `{token:{id,user_uid,name,created_at,expires_at?,last_used_at?,revoked_at?}, secret:"kkt_<id>_<secret>"}`
  — **`secret` is returned once and only once**, the server keeps only a SHA-256 hash; 400 (empty name /
  expiry in the past / unknown field), **429** (the creation rate-limit shares the login limiter, key
  `apitoken:<uid>|<ip>`). `GET /auth/tokens` → `{tokens:[…]}` — **only the caller's own tokens**,
  never secrets or hashes. `DELETE /auth/tokens/{id}` → 204 (idempotent; an already-revoked token is
  also 204 and writes no second audit entry); **someone else's token → 404, not 403** (an admin may
  revoke anyone's). Both create and revoke write an audit entry (`api_token.create`/`api_token.revoke`)
  **in the same transaction** as the mutation.
- **Bearer authentication:** `authenticateRequest` accepts `Authorization: Bearer kkt_<id>_<secret>`
  **alongside** the session cookie (the cookie path is unchanged). A token **inherits its user's role**
  → no second permission system, `RequireAuth`/`RequireWrite`/`RequireAdmin`/`RequireMaintainer` apply
  unchanged (e.g. a maintainer-role token passes all guards; a plain admin hits 403 on operational
  `RequireMaintainer` surfaces). A bad
  bearer is **final** (the same request's cookie is not tried); a scheme other than Bearer falls through
  to the cookie. A revoked / expired / unknown / malformed token, and the token of a disabled user →
  always **401** (never 403) with the **same body** — it cannot be told which case occurred. `last_used_at`
  is rewritten at most once a minute (the same safeguard as the sliding session).
- **Upload API (`/api/v1`):** `POST /upload` (editor/admin via `RequireWrite`) — `multipart/form-data`
  with one+ files, **streamed**. Returns `{"results":[{filename,status,outcome,photo_uid?,error?,
  warnings?}]}` (200 overall, per-file 409 duplicate semantics). Mounted by the second `server.WithAPI`
  in `serve` (`buildIngest` in `cmd/kukatko/ingest.go`). Limit `upload.max_file_size_mb` (0 = no limit).
- **Photos API (`/api/v1`, `internal/photoapi`):** `GET /photos` (authenticated) — list with filters/
  sorting/pagination (query params, invalid → 400) → `{photos,total,limit,offset,next_offset}`;
  the `?album={uid}`/`?label={uid}` filter scopes the listing to an album's/label's photos (a shared
  endpoint for both the album and the label gallery, honouring all other filters/sorting/pagination —
  see Albums & Labels API);
  **`album`/`label` are multi-valued**: repeated parameters (`?album=a&album=b&label=x&label=y`)
  select several albums/labels at once, combined with **AND** — a photo must be in **all** selected
  albums and carry **all** selected labels (each UID = its own correlated `EXISTS`). A single value
  (`?album={uid}`) is a backward-compatible single-album scope;
  the **`person` scope** (`?person={uid}`, also multi-valued, repeated `?person=a&person=b`,
  combined with **AND**) narrows the listing to photos containing **all** selected subjects
  (person/animal/other) —
  a join over **markers** (a named face/region, `invalid = FALSE`; rejected markers do not count),
  each UID = its own correlated `EXISTS` over `markers`;
  **an album scope always forces chronology** (≥ 1 selected album is enough): an album's photos run from
  the oldest (`taken_at ASC`, a photo without a capture date falls back to its upload time `created_at`,
  so the order is complete and stable) and `sort`/`order` from the query are ignored for an album — the
  endpoint's defaults for other views are unchanged;
  `GET /photos/timeline` (authenticated) — a **monthly date histogram** of the library (backing the
  year/month scrubber): accepts the **same filters** as `GET /photos` via `parseListParams`, response
  `{buckets:[{year,month,count,cumulative}],total}`, buckets ordered newest first (by `taken_at`,
  like the default grid), `cumulative` = the number of photos **before** the bucket (maps the bucket to
  a scroll index), `total` (via `Count`) also includes photos without a capture date (they fall into no
  bucket, sorted last); `sort`/`order` are ignored (always grouped by date), backed by
  `photos.Store.TimelineBuckets` (shares `buildWhere` with `List`/`Count`), invalid param → 400;
  `GET /photos/years` (authenticated) — a **year histogram** of the library (backing the filters' **year
  facet**): accepts the **same filters** as `GET /photos` via `parseListParams`, response
  `{years:[{year,count}],total}`, buckets **newest year first**; honours the caller's visibility
  (`archived`) and per-user filters (`favorite`, `min_rating`/`flag`) exactly like the list, so a
  bucket's count = exactly what the grid shows after that year is selected. The `year` filter is **the
  only one ignored** — a facet must not narrow its own offering (otherwise selecting 2019 would leave only
  2019 in the offering); `sort`/`order` and pagination are ignored (always grouped by year). `total`
  (via `Count`) also includes photos **without a capture date** (they fall into no year), so it may
  exceed the sum of the counts.
  Backed by `photos.Store.YearBuckets` (shares `buildWhere` with `List`/`Count`), invalid param → 400;
  the `?year=YYYY` filter on `GET /photos` (a four-digit year 1000–9999, otherwise 400) keeps only photos
  taken in that calendar year — photos with an unknown `taken_at` never match;
  `GET /search?q=&mode=` (authenticated) — **semantic + hybrid search**, `mode` =
  `fulltext`|`semantic`|`hybrid` (default `hybrid`, unknown → 400): **fulltext** = Czech-aware
  full-text over `fts tsvector` (dictionary `simple` + `unaccent`, ranking `ts_rank`
  title>description>notes>file_name); **semantic** = `q` → CLIP embedding via sidecar →
  cosine HNSW over `embeddings`, ranked by similarity; **hybrid** = a fusion of both via
  **Reciprocal Rank Fusion (k=60)**, deduplicated. All modes honour the other list filters + pagination,
  the response is a list + `mode` + `degraded`; `q` is required (empty → 400); **box offline** →
  `semantic`/`hybrid` gracefully fall back to fulltext with `degraded: true`;
  **`q` speaks the search language** (see [Search language](#search-language-q) below): free
  text + `key:value` filters in one string — filters narrow the result in all modes, the free-text
  ranking is left untouched. A query **made only of filters** (no free text) runs the plain-list
  path (ordered by date), the response reports `mode: "filter"` and **never calls the embedding sidecar**;
  a `q` made only of negative terms (`-word`) is forced to `fulltext` (there is nothing to embed). Filters
  the language did not understand are left alone (searched as text) and the response returns them in
  `unknown_tokens: []string` (also on `GET /photos`), so the UI can offer a gentle hint;
  both list and search carry per-photo `is_favorite` **+ per-user `rating`/`flag`** for the current user,
  `?favorite=true` scopes the list to their favourites, **`?min_rating=n` / `?flag=pick|reject|eye` / `?sort=rating`**
  scoped to them (a photo without a row = rating 0 / flag `none`);
  `GET /photos/{uid}` full detail + `files` + `is_favorite` + `rating`/`flag`;
  `GET /photos/{uid}/similar` (authenticated) — **visually similar photos** by cosine distance of
  embeddings (HNSW over `embeddings`, `SimilarSearcher`/`vectors.Store`), nearest first: response
  `{similar:[{…photo, distance}]}` (`distance` = cosine distance to the source photo, smaller =
  closer), `?limit` (default 24, max 100); the source photo is excluded from the result. A photo without
  an embedding or without a similar backend → empty `{similar:[]}` (200), 404 for a missing photo;
  **per-user favourites** `PUT`/`DELETE /photos/{uid}/favorite` (any authenticated user, idempotent → 204,
  404 missing photo, 503 without a backend) + `GET /favorites` (the current user's favourites in the shape
  of the list endpoint, filters/sorting/pagination as for `/photos`);
  **per-user rating** `PUT /photos/{uid}/rating` `{rating?:0..5, flag?:none|pick|reject|eye}` (a personal
  mark: `pick`=👍, `reject`=👎, `eye`=👁; any
  authenticated user, at least one value → 204, 400 invalid, 404 missing photo, 503 without a backend) +
  `DELETE /photos/{uid}/rating` (idempotent clear → 204); `GET /photos/{uid}/faces` (authenticated) — a
  photo's faces with bbox, assignment (marker/subject), action (`create_marker`/`assign_person`/`already_done`)
  and identity **suggestions** **for every face** with an embedding — for an unnamed one, naming
  candidates; for an assigned one, **alternatives for reassignment** (the person the face already carries,
  and everyone else already on the photo, are excluded from the suggestions, so an assigned face without a
  close alternative gets an empty list; threshold widening without a cutoff runs only for unnamed ones).
  Face↔marker IoU matching, see `internal/facematch`; 503 when the face backend is not wired in;
  `POST /photos/{uid}/faces/assign` (editor/admin) — an assignment
  action `{action, face_index?, marker_uid?, subject_uid?, subject_name?, bbox?}`
  (`create_marker`/`assign_person`/`unassign_person`), auto-creates a subject by name, keeps the `faces`
  cache + `marker.reviewed` consistent (400 validation, 404 missing photo/marker/subject);
  `GET /photos/{uid}` full detail additionally carries **membership** `albums`/`labels` (inline detail
  chips, via the `PhotoOrganizer` interface / `organize.Store.AlbumsForPhoto`+`LabelsForPhoto`; a nil
  organizer → empty arrays) and **`uploader`** `{uid,name}` — who uploaded the photo, the name resolved
  server-side via `UserResolver` (`auth.Store.GetUserByUID`; `name` = `display_name`, fallback
  `username`); omitted (`omitempty`) for photos without `uploaded_by` (imports from PhotoPrism/photo-sorter),
  and also when the user cannot be resolved — resolution is **only on the detail**, list/search do not
  resolve a per-photo uploader (no N+1);
  and **`place`** `{country,region,city,place_name}` — the photo's **cached** reverse geocoding from
  `photo_places` (filled by the background job `places`), read via the `PlaceResolver` interface
  (`places.Store.GetPlace`). **The detail never geocodes**: mapy.com credits are metered, so
  opening a photo must not cost a credit — the on-demand lookup stays exclusively in `GET /maps/reverse`,
  which the user requests. The block is `omitempty` and is omitted for a photo the job has not yet reached,
  for a photo without GPS, and for a "processed" marker (a row with all levels empty); individual levels
  may be empty when the geocoder knew nothing more precise. Rendered by `TechnicalDetails` (the Location
  group);
  **non-destructive edit** (`internal/photoedit` + `edit.go`/`media_edit.go`):
  `GET /photos/{uid}/edit` (authenticated) → the stored `photos.Edit` (crop/rotation 0-90-180-270/brightness/contrast,
  an unedited photo → a neutral edit) and `PUT /photos/{uid}/edit` (editor/admin) writes the edit into
  `photo_edits` (bounds validation; the original is never changed — `GET …/download` **renders it at run time**
  via `photoedit.Apply` unless the caller passes `?original=true`);
  `PATCH /photos/{uid}` (editor/admin) partial edit of
  metadata — `title/description/notes/ai_note/taken_at/lat/lng` (null clears a nullable, coordinate
  validation) **+ approximate date** `taken_at_estimated` (bool — the date is an estimate, not a fact) and
  `taken_at_note` (free text about the dating, whitespace trimmed, **max 500 characters**, longer = 400).
  The note applies only to an estimate: once the resulting `taken_at_estimated` is `false` (the client
  dropped it, or the photo never had it), the server **clears** `taken_at_note` — a date presented as a
  fact never keeps a stale note hanging (the length is still validated first, so an overly long note is
  reported, not silently discarded). `taken_at` NULL + `taken_at_estimated` `true` is legal
  (the note carries the meaning) and the flag has no effect on sorting/timeline/filters
  **+ location origin** `location_source` (`exif`/`manual`/`estimate`/`""`, see `internal/geoestimate`):
  in the payload it is **read-only information**, in PATCH the only allowed value is `"manual"` and only
  on a photo that has a location — this **accepts the estimate** (promotes it to the user's decision)
  without sending the coordinates back and rounding them to what the client rendered. Anything else = 400:
  `exif`/`estimate` are written by the server, the client must not invent the origin of a coordinate it
  entered itself. **Any touch of `lat`/`lng`**
  (moving or clearing) writes `location_source: "manual"` on its own; clearing therefore **does not reset**
  the origin to empty the way `taken_at` → `unknown` does — `"manual"` without coordinates is a deliberate
  **tombstone** ("the user decided this photo has no location"), thanks to which backfill never brings back
  an estimate the user discarded
  **+ IPTC/XMP credits** `subject/artist/copyright/license/keywords/scan`: free text,
  whitespace trimmed, length caps (`subject`/`copyright`/`license` 1000, `keywords` 2000,
  `artist` 255 **characters**, not bytes), longer = 400; `scan` is a plain bool. Machine-derived fields
  (`software`, `color_profile`, `image_codec`, `camera_serial`, `original_name`, `projection`) are
  **served but not editable** — the decoder rejects them as an unknown key (400), they describe the file, not
  the user's view of it. **The response has the same shape as `GET /photos/{uid}`** — full detail including `files`,
  `albums`, `labels`, `is_favorite` and `uploader` (the shared `writeDetail` in `internal/photoapi`), not
  a bare `photos.Photo`: the client replaces the detail it holds with the one from the response, so missing
  fields would disappear from its detail (it used to crash on `albums.map` of `undefined`). The client sends
  only **actually changed** fields: resending an unchanged `taken_at` would flip `taken_at_source` `exif` →
  `manual`, resending unchanged coordinates would round them to 6 decimal places out of a text field.
  `ai_note` is free text from external AI classification (an automaton also writes it via this route),
  returned in both detail and list as part of `photos.Photo` and included in the full text (§ Search);
  likewise all the IPTC/XMP and technical fields above **and the pair `taken_at_estimated`/`taken_at_note`**
  — they are part of `photos.Photo`, so they are carried by
  **every** response with a photo (detail, list, search), and `subject` and `keywords` additionally fall into
  the full text (weight B and C respectively). `keywords` is the original IPTC value **verbatim**
  (comma-separated), **they are not labels** — `/labels` remains a separate curatorial taxonomy;
  `POST /photos/{uid}/archive`+`/unarchive`
  (editor/admin) soft-delete via `archived_at` (archived ones outside the default list);
  `POST /photos/{uid}/regenerate-thumbnail` (editor/admin) — a **service action** for a
  missing/stale thumbnail: it regenerates the photo's thumbnails and its perceptual hashes
  from the original via `thumbjob.Service.ForceRegenerate` (sharing the thumbnailer and the job handler,
  no duplicated logic), **overwrites** the existing thumbnail cache and the hashes (unlike the
  repair path of the `thumbnail` job, which skips present data), the original is **never
  changed**. It runs **synchronously** so it can return a clear result `{status:"regenerated",
  sizes:[…]}` (200) or a typed error: 404 missing photo, **422** the original is missing or
  cannot be decoded (`thumbjob.ErrRegenerateFailed`), 503 when regeneration is not wired in.
  Idempotent (safe to click repeatedly); recorded in the audit log as
  `photo.thumbnail` with the list of regenerated sizes in details;
  **trash / permanent deletion** (`trash.go`, backed by `internal/trash` via the `Purger` interface, nil → 503):
  `POST /photos/{uid}/purge` (**admin** via `RequireAdmin`, `?confirm=true` otherwise 400, 404 missing,
  409 photo not archived → 204) and `POST /trash/empty` (**admin** via `RequireAdmin`,
  `?confirm=true` → `{purged,failed}`) permanently and irreversibly delete archived photos, so they are
  tightened from write to admin; `POST /trash/purge-older` (**admin** via `RequireAdmin`,
  `?days=N&confirm=true`; `N` = an integer ≥ 0, otherwise 400, missing `confirm` → 400, nil purger →
  503) permanently deletes every photo archived longer than `now − N days` via the **same** purge path as
  empty-trash (`{purged,failed}`); `N=0` = the whole trash (equivalent to empty-trash). In the audit log it is
  distinguished via `details.source=purge_older` and credited to the calling admin (not the system retention
  actor `source=retention`); archiving (reversible soft-delete) stays `RequireWrite` and
  `GET /trash/info` (authenticated) returns `{retention_days}` for the countdown
  to auto-purge; the trash listing runs via the shared `GET /photos?archived=only`;
  **media URLs in the payload** (`internal/mediaurl`): every returned photo carries `thumb_url`
  (the grid thumbnail `tile_500`) and `download_url` (the original, `?original=true` semantics — never
  rendering an edit). The values are minted by the storage backend via `Storage.URL`: `FS` returns
  empty → fallback to the own routes below, `R2` returns a **short-lived signed URL** (default 1 h) on
  the edge Worker's domain, so the application does not transfer a single byte of media. The client takes
  them **as-is** and never assembles them from a UID (it cannot compute the signature). A signed URL
  expires → see `useThumbSrc` in `docs/FRONTEND.md`;
  `GET /photos/{uid}/thumb/{size}` and `/download` (session/`?t=` token) **stream** the media
  (`Cache-Control`/`ETag`/`304`), or — when the backend publishes objects — answer with a **`302` redirect**
  to a signed URL (`Cache-Control: private, no-store`, so the cache does not outlive the signature); the routes
  remain, so old links and bookmarks keep working. `GET /photos/{uid}/video` (session/`?t=` token) streams
  video **with HTTP Range** (206 partial, `Accept-Ranges`, seek; a live photo = a motion clip, still → 404)
  for inline HTML5 playback, or redirects to the Worker, which serves Range directly from R2 (dropping the
  requirement of a seekable local file); an optional on-the-fly transcode of non-web-friendly codecs via
  the `video.transcode` config (default off) feeds `ffmpeg` a signed URL directly (`ffmpeg` reads http(s)).
  **Bulk ZIP download** (`internal/photoapi/zip.go`): `POST /photos/download-zip`
  (session/`?t=` token — **the same authorization as a single download**, whoever may download one may
  download more) **streams a ZIP of originals** straight to the response (`archive/zip`, `Store` method —
  originals are already compressed; nothing is buffered whole in RAM, `CGO_ENABLED=0`). Body `{photo_uids?,
  album_uid?, name?, date?}`: `album_uid` is expanded server-side into the album's **live** (non-archived)
  photos in chronological order (via `photos.List` with `AlbumUIDs`, so archived ones are not even seen),
  `photo_uids` is an explicit selection in the client's order (a missing UID is **silently skipped**, as with a
  single download); the two sets are merged and deduplicated by UID. A photo's `file_name` is the entry name,
  colliding names are disambiguated with a ` (2)`, ` (3)`… suffix before the extension. An original missing from
  storage is **skipped and recorded** in a text entry `MISSING.txt` in the archive — it does not abort the whole
  ZIP. The archive name: `name` (e.g. the album title) + `.zip`, otherwise `kukatko-photos-<date>.zip` (`date` is
  sent by the client, the server **avoids the wall-clock** on this path); an entry's mtime is the photo's
  `taken_at`. A cap of **1000 files**
  per request (`maxZipFiles`), above which **413** before the first byte of the archive; a request with no
  photos → 400. Always **streams via `storage.Open`** (even on a publishing backend — a single archive cannot be
  assembled from redirects, unlike a single `/download`).
  **Authorization guards discovery:** a signed URL is minted only into a response the caller was already
  entitled to, so it never reveals an archived photo. Unlike the earlier design with a public
  bucket, the archive is a **real security boundary** (see the doc comment of `internal/mediaurl`).
  **Stacks** (`internal/photoapi/stacks.go`, the `Stacker` interface = `stacks.Service`, **nil → 503**):
  `POST /photos/stack` (editor/admin) body `{photo_uids:["…","…"]}` manually groups a selection (**≥ 2**),
  picks the primary member by a rule and returns the **detail of the new primary** — 400 (< 2 photos),
  404 (photo missing/archived), 503 (disabled); `POST /photos/{uid}/stack/primary` (editor/admin) makes
  `{uid}` the primary of its stack → refreshed detail `{uid}` (404 missing, 409 not in a stack, 503);
  `POST /photos/{uid}/unstack` (editor/admin) removes `{uid}` from the stack (it becomes standalone; a
  two-member stack thereby dissolves, a stack that loses its primary picks a new one) → refreshed detail
  (409 when it is not in a stack); `POST /photos/{uid}/unstack-all` (editor/admin) dissolves the whole stack
  `{uid}` belongs to → refreshed detail. **Fields in the responses:** every photo in list/search/detail may
  carry `stack_uid` (string) and `stack_count` (int; **≥ 2 only for a stacked primary**, otherwise omitted —
  it drives the tile badge); the detail (`GET /photos/{uid}`) additionally `stack_members` — an array (primary
  first) `{uid, file_name, media_type, file_mime, file_width, file_height, file_size, is_primary, thumb_url,
  download_url}` (a strip of variants), omitted for a non-stacked photo (distinct from `files`, which are the
  `photo_files` of a single row).
  Mounted by the third `server.WithAPI` (`buildPhotoAPI` in `cmd/kukatko/photos.go`).
- **Jobs API (`/api/v1`, `internal/jobsapi`, maintainer-only via `RequireMaintainer`):**
  `GET /jobs/stats` → `{by_state,by_type,total}`; `GET /jobs` → `{jobs,limit,offset}`
  (recent/dead-letter listing, query `state`/`limit`/`offset`, invalid → 400);
  `POST /jobs/{id}/requeue` → refreshed job (dead/failed → queued; 404 missing, 409
  non-requeueable). The frontend polls (no SSE). Mounted by `server.WithAPI`
  (`buildJobs` in `cmd/kukatko/jobs.go`), which registers the handlers `image_embed`
  (`embedjob.Service`), `face_detect` (`facejob.Service`) and — when the mapy.com key is set —
  `places` (`placesjob.Service`, `buildPlacesServiceOrNil` in `cmd/kukatko/places.go`), and also
  builds — and `serve` starts — a **background worker** (`internal/worker`) for the whole life of the
  process (`startWorker`, stopped on shutdown via ctx).
- **Clusters API (`/api/v1`, `internal/clusterapi`, editor/admin via `RequireWrite`):**
  `GET /faces/clusters` → `{clusters:[{uid,size,representative,examples,suggestion?}]}` (clusters of
  unassigned faces from auto-clustering, `suggestion` = the nearest named subject);
  `POST /faces/clusters/{id}/assign` `{subject_uid?,subject_name?}` assigns the **whole cluster** to one
  subject (find-or-create by name) → markers for all faces, the cluster is consumed;
  `POST /faces/clusters/{id}/remove-face` `{photo_uid,face_index}` detaches a stray face before
  naming → the refreshed cluster (or `null` when it is orphaned); 503 without a backend, 400/404/409 per the
  sentinels. Mounted by the fourth `server.WithAPI` (`buildClusterAPI` in `cmd/kukatko/clusters.go`).
- **Outliers API (`/api/v1`, `internal/outlierapi`, editor/admin via `RequireWrite`):**
  `GET /subjects/{uid}/outliers` → `{subject_uid,count,meaningful,avg_distance,no_embedding,
  faces:[{photo_uid,face_index,bbox,det_score,distance,marker_uid?,width,height,orientation}]}`
  (a person's faces sorted descending by cosine distance from the **trimmed** centroid of their
  embeddings — the most likely mis-assigned ones first); 1–2 faces → `meaningful:false`;
  a wrong face is detached via the existing `POST /photos/{uid}/faces/assign` (`unassign_person`),
  this layer does not mutate; 503 without a backend, 404 missing subject.
  **Optional query parameters** `threshold` (the minimum cosine distance from the centroid, 0–2,
  default **0 = return everything**) and `limit` (max number of faces, default **0 = all**) narrow the list,
  so the page need not pull all the faces of a well-tagged person; non-numeric, negative or
  `threshold > 2` → 400. The historical behaviour ("everything, sorted") stays the default.
  `count`/`meaningful`/`avg_distance` describe the **whole scored set** (before the filter), so the
  statistics do not lie when a threshold narrows the list; `no_embedding` is the count of assignments
  **without an embedding** that cannot be checked (a face recognized while the sidecar was offline) and are
  **not** in `faces` — the client should own up to them, not silently drop them. **Faces confirmed by the
  user** (see Feedback API below) are excluded from the result, so repeated passes converge instead of
  offering the same false alarms over and over. Mounted by `server.WithAPI` (`buildOutlierAPI` in
  `cmd/kukatko/outliers.go`).
- **Candidates API (`/api/v1`, `internal/candidatesapi`, editor/admin via `RequireWrite`):**
  "find a person among untagged photos". `POST /subjects/{uid}/candidates` with an **optional** body
  `{threshold?,limit?}` (`threshold` = max cosine distance, default `candidates.max_distance`;
  `limit` 0 = all; `DisallowUnknownFields` + 64 KiB, negative values → 400) →
  `{subject_uid,source_photo_count,source_face_count,faces_without_embedding,min_match_count,threshold,
  reason?,counts:{create_marker,assign_person,already_done},candidates:[{photo,face_index,
  bbox:{relative:[x,y,w,h],pixel:[x,y,w,h]},distance,match_count,action,marker_uid?}]}`. For a subject it finds
  **unassigned** faces that resemble its own tagged ones (per-exemplar kNN over
  `subject_uid IS NULL` + voting; `min_match_count` is a vote rule scaled by the number of exemplars and
  the threshold, clamped 1..5, returned so the UI can explain the filter). Already-rejected faces drop out
  (`internal/feedback`), as do those tripping the negative-exemplar rule, and faces too small
  (relative `faces.min_face_size` + absolute `candidates.min_face_px`). `action` says what
  confirmation will do (`create_marker`/`assign_person`/`already_done`) — **confirmation goes through the
  existing** `POST /photos/{uid}/faces/assign`, this layer **does not mutate**. `marker_uid` is filled when
  the face already overlaps a marker (`assign_person`/`already_done`), so the UI can send the right assign
  (present → `assign_person` over that marker, empty → `create_marker`). `bbox` is relative 0..1 **and** pixels
  (honouring EXIF orientation). An empty **non-error** result with `reason` `"no_faces"` (a subject without faces)
  or `"no_embeddings"` (tagged, but the faces have no embedding — the box was offline); the box being offline
  otherwise does not matter (it reads vectors already in the DB). 503 without a backend, 404 missing subject.
  Mounted by `server.WithAPI` (`buildCandidatesAPI` in `cmd/kukatko/candidates.go`).
- **Recognition sweep API (`/api/v1`, `internal/sweepapi`, editor/admin via `RequireWrite`):**
  "go through all named people and find certain matches among unlabelled faces" — a server-side
  fan-out via the **candidate search** (`internal/candidates`) over all subjects, not client-side.
  `GET /faces/sweep?confidence=<percent-or-distance>&limit=<per-person>`. `confidence`: a value
  `>1` (max 100) is a **confidence percentage** → mapped to the cosine distance `1 - percent/100`
  (floor `0.01`), a value `≤1` is a **direct distance**, empty = default 75 % (0.25); negative /
  `>100` / non-numeric → 400. `limit` = the cap on candidates per person (0 = all; negative → 400). It iterates
  subjects with `marker_count > 0` (i.e. they have a face), each one's scan runs at **high confidence** (a tight
  distance) and with **bounded concurrency** (a worker pool, `sweep.concurrency`); the number of subjects is
  capped (`sweep.max_subjects`), the overflow is **visible** (`capped`), not silently discarded. The response
  is an **NDJSON stream** (`application/x-ndjson`), a line = one JSON message `{type,...}`: `progress`
  `{scanned,total,name}` after each finished subject (moves the bar), `person`
  `{subject,candidates,counts,actionable}` only for subjects with **actionable** candidates (`candidates`
  in the same shape as the per-subject endpoint; `already_done` is **filtered out** of the work list),
  and one final `summary` `{people_scanned,people_with_matches,total_actionable,total_already_done,
  capped,subjects_total}`. A subject with **zero** actionable candidates never even makes the list;
  a subject without faces is **skipped** (not an error); an error scanning one subject is logged and skipped,
  the whole sweep does not fail. **It never auto-confirms** — confidence only narrows the list, every confirmation
  still goes through `POST /photos/{uid}/faces/assign`, a rejection through `POST /feedback/face-rejections`. An error
  **before** the first line (listing subjects failed) → clean 500 JSON; an error **mid**-stream (the client
  disconnected) only logs (200 has already been sent). 503 without a backend. Mounted by `server.WithAPI`
  (`buildSweepAPI` in `cmd/kukatko/sweep.go`), sharing `candidates.Service` with the candidates endpoint.
- **Expand-a-collection API (`/api/v1`, `internal/expandapi`, editor/admin via `RequireWrite`):**
  "find photos similar to a whole album / label" — filling out a half-tagged collection. `GET /albums/{uid}/similar`
  and `GET /labels/{uid}/similar` with query `?threshold=&limit=` (`threshold` = max cosine distance,
  default `expand.max_distance` = 0.30, i.e. 70 % similarity; `limit` default `expand.limit`, cap
  `expand.max_limit`; non-numeric / negative → 400). Membership is resolved **natively** (`internal/organize`),
  **no PhotoPrism call**. Response `{kind,collection_uid,source_photo_count,source_photos_sampled,
  source_photos_with_embedding,source_capped,source_cap,min_match_count,threshold,limit,result_count,
  reason?,candidates:[{photo,distance,similarity,match_count}]}`. The algorithm: **per-photo kNN + voting**
  (not the average of the collection's embeddings — a collection is not a single visual concept); `match_count` =
  how many source photos returned the candidate, `distance` = the **minimum** across them. Photos **already in the
  collection** drop out (that is the whole point), as do those below `min_match_count` (a vote rule scaled by the
  number of sources and the threshold, clamped 1..5, returned for the UI), those rejected for the given label
  (`internal/feedback`) and those tripping the negative-exemplar rule.
  **Albums have no rejection model** — the rejection/negative-exemplar filters apply only to labels (an asymmetry,
  not an omission). Ordering `match_count` DESC, then `distance` ASC (a match from more sources beats one strong
  match), truncated to `limit`. Photos **with** an embedding are **counted and reported** (the box is often offline →
  a collection may be half-embedded and the results thin). A huge album is **sampled** (deterministically, evenly
  across the members) to `expand.source_cap` and **the cap is reported** (`source_capped`), not silently. An empty
  album/label or a collection without embeddings → a **non-error** empty result with `reason` `"empty_collection"` /
  `"no_source_embeddings"`. A collection of **one** photo degenerates into per-photo similarity. **Read-only** —
  adding the found photos goes through the existing `POST /photos/bulk`. 503 without a backend, 404 missing
  album/label. Mounted by `server.WithAPI` (`buildExpandAPI` in `cmd/kukatko/expand.go`).
- **MCP server (`POST /api/v1/mcp`, `internal/mcpapi`, via `RequireAuth` + per-tool RBAC):** the library
  exposed to an **AI agent** via the **Model Context Protocol** — it searches, reads, organizes ("find all
  photos of grandma from the sixties and put them in an album"). Transport **Streamable HTTP, stateless**, response
  `application/json` (not SSE), the body is JSON-RPC 2.0 (`initialize`, `tools/list`, `tools/call`, `ping`);
  the client must send `Content-Type: application/json` and `Accept: application/json, text/event-stream`.
  The library `github.com/modelcontextprotocol/go-sdk` (pure Go, keeps `CGO_ENABLED=0`); the SDK's DNS-rebinding
  guard is **disabled**, because it rejects even a legitimate request from a reverse proxy and the endpoint is
  authenticated. **Off by default** (`mcp.enabled: false`) — and when `false` the route is **not mounted at
  all** (`RegisterRoutes` registers nothing), so the path **does not exist**, rather than returning 403;
  in the whole binary it then falls into the SPA catch-all and returns `index.html` like any unknown path (the
  access log lacks `"route":"/api/v1/mcp"`). **It calls the service layer in-process**, not its own HTTP API, so it
  keeps the transaction boundaries. **Auth: no new mechanism** — `RequireAuth` as everywhere, the agent sends
  `Authorization: Bearer kkt_…`, the role is the **token owner's** (`viewer` = read only; `editor`/`admin`/`ai`
  = also write). The boundary is **double**: write tools are **not registered at all** for a read-only caller (they
  do not see them in `tools/list` — two servers are built and `getServer` picks by principal) **and** every write
  handler re-verifies the role. Tools — reading: `search_photos` (free text + the **search language** +
  scope `album_uid`/`label_uid`/`person_uid` + `sort`/`order`/`limit`/`offset`), `get_photo`,
  `find_similar_photos`, `list_albums`/`get_album`, `list_labels`/`get_label`,
  `list_subjects`/`get_subject`, `library_stats`; writing: `create_album`, `add_photos_to_album`,
  `remove_photos_from_album`, `create_label`, `attach_label`, `detach_label`, `set_photo_metadata`,
  `set_photo_rating`, `bulk_edit_photos`. An album's/label's/person's photos are read via `search_photos` with a
  scope — it is the same list path, so the other filters and pagination apply too. **The response shape is
  compact**: lists return only `{uid,title,taken_at,media_type,thumb_url}` + `total`/`offset`/
  **`remaining`**, **no tool returns the raw `exif` blob** (the agent's context is the scarce resource).
  **Every mutation writes an audit row in its transaction** with `"via": "mcp"`. **Nothing destructive is
  exposed** — no deletion, purge, trash, **archiving** (archiving = the path to the trash, which is purged
  by retention), restore, backup, user management or admin surface; `bulk_edit_photos` therefore
  omits even `Archive` and `Location`, which the bulk service otherwise supports. Mounted by `server.WithAPI`
  (`buildMCPAPI` in `cmd/kukatko/mcp.go`). In detail: [`docs/MCP.md`](MCP.md).
- **Review game API (`/api/v1`, `internal/reviewapi`, editor/admin via `RequireWrite`):** a "game" for
  tidying up the library — one question at a time ("Is this Tomáš?", "Should this photo have the label Ostatky?"),
  answer yes/no/skip. Questions are drawn from the **uncertainty band** (`review.band_min ≤ confidence <
  review.band_max`, confidence = 1 − cosine distance, default 0.45–0.75) — below the band it is noise,
  above it, confirmation happens in bulk on `/recognition` or via expand. `GET /review/queue?limit=N`
  (empty/0 → `review.queue_size`, cap 100, non-numeric/negative → 400) → `{questions:[{id,kind:
  "face"|"label",confidence,photo,subject?,face_index?,bbox?{relative,pixel},action?
  ("create_marker"|"assign_person"),marker_uid?,label?}],answered,remaining,reason?}`; `id` is
  **stable, derived from content** (`face:<photo>:<index>:<subject>` / `label:<photo>:<label>`),
  `bbox` relative 0..1 **and** pixels (honouring EXIF orientation), the queue is **deterministic** for a given
  library state (ordered by distance from the centre of the band, tie-break id; face/label questions are
  **interleaved** proportionally, no `rand`). The queue is **cached per user** (`review.cache_ttl`, default 60 s) —
  a batch fetch does not recompute the expensive vector searches; `remaining`/`answered` are cheap session counters.
  An empty library (no named people or labels) → a **non-error** empty queue with `reason:
  "no_people_no_labels"`; sources exist, but the band is empty → `reason:"no_candidates"`.
  `POST /review/answer` with `{question_id,answer:"yes"|"no"|"skip"}` → `{result,answered,remaining}`.
  **yes** on a face goes through the **existing** assign state machine (the same path as
  `POST /photos/{uid}/faces/assign`; the action is derived from the face's current state — a marker exists →
  `assign_person`, otherwise `create_marker` with the stored bbox), yes on a label via `AttachLabelAudited`
  (source `manual`); **no** writes a **permanent rejection** into `internal/feedback` (the question never comes
  back and the negative-exemplar rule kills similar candidates); **skip** = "don't know" — the question is not
  offered again in this session, but is **not recorded** (a restart may bring it back; skip is not a rejection).
  Answers are **idempotent** (`result:"already_answered"`, no second write or duplicate
  marker) and audited in the same transaction as the mutation (via the reused write paths). A deleted
  photo/face/label between fetch and answer → 200 with `result:"gone"` (the UI moves on), **not** 500; an invalid
  `question_id`/`answer` → 400. Mounted by `server.WithAPI` (`buildReviewAPI` in
  `cmd/kukatko/review.go`, sharing the facematch service with photoapi and the candidates/expand services with the
  sweep and expand endpoints).
  **Leaderboard** `GET /review/leaderboard?window=all|7d|today` (default `all`, other value → 400)
  gated by **`RequireAuth`** — it returns only aggregated counts + names, so **every authenticated user**
  sees it (even a viewer), not just an editor. It ranks players by the number of **decisions** in the review
  game, sourced from durable audit rows with `details.via = "review"`: **yes** = `face.assign` + `label.attach`,
  **no** = `face.reject` + `label.reject`; **skip** records nothing, so on principle it does not count. Because
  of this, a review face confirmation (`face.assign`) now carries `via:review` (until now the only one of the four
  actions missing it — it goes through facematch `Service.Apply`, which assembles the audit itself; ordinary
  assignments stay unmarked). The response `{window,caller_uid,entries:[{user_uid,display_name,yes_count,no_count,total,
  is_me}]}` is sorted (total desc → yes desc → display_name), only users with ≥1 decision in the window
  (zero = absent), a NULL actor (a deleted user) is omitted, `is_me`/`caller_uid` mark one's own
  row. The windows are computed from `created_at` (7 d = a sliding 7×24 h, today = midnight of the day). Served by
  `review.LeaderboardStore` over the shared pool; the partial index `idx_audit_log_review_actor`
  (migration `0037`) keeps the scan cheap.
- **People/Subjects API (`/api/v1`, `internal/peopleapi`):** `GET /subjects` (RequireAuth) →
  `{subjects:[{...subject, marker_count, cover_face?}]}` (ordered by name, counts of non-invalid
  markers). `cover_face` = `{photo_uid,x,y,w,h,width,height,orientation}` — the face by which the
  subject is illustrated in the people grid when it **has no** `cover_photo_uid`; absent when the subject has no
  usable marker. Picked by `listSubjectsSQL` (the largest box, then `score`, then `uid`; only
  `type='face'`, non-invalid, on a visible photo). `width`/`height`/`orientation` are the stored frame of the
  photo — the client crops the cutout itself from the thumbnail cache (there is no face-thumbnail endpoint) and
  without the frame would distort it. **An explicit `cover_photo_uid` always wins**, `cover_face` is only a
  fallback;
  `POST /subjects` (RequireWrite) → 201 creates a subject from `{name,type,favorite,private,notes,
  cover_photo_uid?}` (empty name / unknown type → 400); `GET /subjects/{uid}` (RequireAuth) →
  the subject (404); `PATCH /subjects/{uid}` (RequireWrite) → editing the same fields (404/400);
  `DELETE /subjects/{uid}` (RequireWrite) → 204 (the markers are detached server-side); `GET
  /subjects/{uid}/photos` (RequireAuth) → a paginated gallery of the subject's photos
  `{photos,total,limit,offset,next_offset}` (newest-first, non-archived only, `limit`≤500). Mounted
  by `server.WithAPI` (`buildPeopleAPI` in `cmd/kukatko/people.go`). The subject's photo records
  build on `people.Store.ListPhotoUIDsBySubject` (distinct non-invalid markers → photo uid).
- **Process API (`/api/v1`, `internal/processapi`, maintainer-only via `RequireMaintainer`):**
  `POST /process/embeddings` → `{enqueued}` (backfill `image_embed` for photos without an embedding),
  `POST /process/faces` → `{enqueued}` (backfill `face_detect` for photos without face detection),
  `POST /process/clusters` → `{created}` (re-clustering of unassigned faces via
  `cluster.Recluster`), `POST /process/places` → `{enqueued}` (backfill `places` reverse-geocode for
  geotagged photos without a place via `placesjob.BackfillPlaces`; 503 when there is no mapy.com key),
  `POST /process/thumbnails` → `{enqueued}` (backfill `thumbnail` for photos **without a generated
  thumbnail** via `thumbjob.BackfillThumbnails`; "missing thumbnail" = a photo without a perceptual hash,
  which the `thumbnail` job computes together with the thumbnail). Optional `?all=true` schedules **every
  non-archived photo** (a forced full re-run — it also catches up a missing thumbnail size on a photo that
  already has the hash; the job skips sizes already in cache, so the run is cheap and never changes the original).
  `POST /process/metadata` → `{enqueued}` (backfill `metadata` for photos whose **file has never
  been read** into the IPTC/XMP and file-technical columns, via `metajob.BackfillMetadata`; "unread"
  = `photos.metadata_extracted_at IS NULL`, which are rows from a PhotoPrism import, a photo-sorter migration
  and everything uploaded before extraction). Optional `?all=true` schedules **every non-archived photo**
  (a forced re-read of the whole library — this catches up fields the new extractor has learned to read).
  The job is a pure **gap-filler**: it fills only columns that are still empty, so an empty extraction
  never overwrites a value the user wrote, and it does not touch `taken_at`/GPS/captions/curatorial data
  at all. A missing original is **logged and skipped** (the run does not fail).
  `POST /process/sidecars` → `{enqueued}` (backfill `sidecar` for photos whose **metadata
  sidecar is missing or stale**, via `sidecarjob.BackfillSidecars`; "missing/stale" =
  `photos.sidecar_written_at IS NULL OR sidecar_written_at < updated_at`). A sidecar is a YAML file
  next to the originals in storage (`sidecars/<original key>.yml`) with the photo's metadata and curatorial data
  — it exists so the library survives losing the database; the format is fully in `docs/RESTORE.md`. Optional
  `?all=true` schedules **every non-archived photo** (a forced full re-run — this catches up
  curatorial data that changed **without** touching the photo's row: album membership, a label, and therefore
  do not look stale). The endpoint only **enqueues** the jobs, the worker writes the files; the run is idempotent
  (over a library with current sidecars it schedules zero) and an interrupted run catches up. **503** when
  `sidecar.enabled: false`. The CLI counterpart: `kukatko sidecar backfill [--all]`.
  `POST /process/stacks` → `{created}` (detection and grouping of photos into stacks over the whole library via
  `stacks.Service.DetectStacks`; **synchronous**, the candidates are **only the not-yet-stacked non-archived**
  photos, so a re-run is idempotent and does not break a manual or an existing stack; **503** when
  `stacks.enabled: false`).
  `POST /process/locations` → `{estimated}` (location estimation for photos without GPS from photos taken close
  in time, via `geoestimate.BackfillLocations`; **synchronous**, **503** when
  `location_estimate.enabled: false`). The candidates are only photos **without coordinates** with an empty
  `location_source`, with a **known and non-estimated** `taken_at`, that are neither a scan nor archived.
  The neighbours are photos with a **measured** location (`location_source <> 'estimate'`) in the window
  ±`location_estimate.window`; an estimate arises **only when they are coherent** — all within
  `location_estimate.radius_meters` of their centroid. Otherwise **nothing is created**: a day between Prague and
  Vienna has no honest answer. The written location is always marked `location_source: "estimate"` and
  the photo gets a `places` job, so the estimate propagates into the place hierarchy (the geocode itself is metered
  and runs through the existing `maps.geocode_rate_per_sec` limiter). A re-run is idempotent: an estimated photo
  stops being a candidate and **an estimate the user deleted never comes back** (deletion writes
  `location_source: "manual"` without coordinates — a tombstone, not a gap).
  Thumbnails and metadata are computed **locally**, so the backfill works even when the box is offline; the job queue
  deduplicates, so a repeated run is idempotent. Mounted by `server.WithAPI` (`buildJobs`).
- **Albums & Labels API (`/api/v1`, `internal/organizeapi`):** **albums** `GET /albums`
  (RequireAuth) → `{albums:[{...album, photo_count, cover_uid?, taken_from?, taken_to?}]}`
  (`organize.AlbumSummary`): `cover_uid` is the **effective cover** — a manually chosen
  `cover_photo_uid`, otherwise the **newest live photo of the album** (deterministically: `taken_at DESC NULLS
  LAST, uid`); `taken_from`/`taken_to` is the **`taken_at` range** across the album's photos. Both are aggregated
  by a single SQL query (LEFT JOIN + LATERAL, no migration) and count **only live photos** —
  an archived photo is counted into `photo_count`, but supplies no cover and does not move the range. Absent
  when the album has nothing to show / no photo has a known `taken_at`. **The list order** is always
  **newest album first**: sorted by the **newest live photo of the album** (`MAX(taken_at) DESC
  NULLS LAST`, `uid` as the tiebreak for a total and stable order). Albums that cannot be assigned
  a date — no photo has `taken_at`, or the album is empty — are **at the end**; an archived
  photo does not affect the order. The ordering is **not a user choice**: the endpoint has no `sort`/`order`
  parameter and the frontend does not change the server's order. `POST /albums`
  (RequireWrite) → 201 from `{title,description?,type?,cover_photo_uid?,private?}` (empty
  title / invalid type → 400); `GET /albums/{uid}` (RequireAuth, 404); `PATCH /albums/{uid}`
  (RequireWrite) edits title/description/cover_photo_uid/private (**`type` is preserved**,
  not editable); `DELETE /albums/{uid}` (RequireWrite → 204); membership
  `POST /albums/{uid}/photos` `{photo_uids:[…]}` (adds), `DELETE /albums/{uid}/photos`
  `{photo_uids:[…]}` (removes) — both return the current **chronological** order `{photo_uids:[…]}`,
  404 for a missing album/photo. There is no manual album ordering: `PATCH /albums/{uid}/order` was
  removed (→ 404) and an album always displays from the oldest photo (see Photos API). **Labels** `GET /labels`
  (RequireAuth) → `{labels:[{...label, photo_count}]}` (ordered by priority DESC); `POST /labels`
  (RequireWrite) → 201 from `{name,priority?}` (empty name → 400); `GET /labels/{uid}`
  (RequireAuth, 404); `PATCH /labels/{uid}` (RequireWrite, name/priority); `DELETE /labels/{uid}`
  (RequireWrite → 204); attaching `POST /labels/{uid}/photos` `{photo_uid,source?,uncertainty?}`
  → 204 (invalid source → 400), `DELETE /labels/{uid}/photos` `{photo_uid}` → 204. **An album's/label's
  photo gallery** runs via the shared `GET /photos?album={uid}`/`?label={uid}` (the same shape +
  filters/pagination; an album scope always has forced chronology, a label honours the chosen order). A viewer reads, but does not mutate (403).
  Every mutation (create/update/delete of an album or label, add/remove of photos, attach/detach) writes an audit entry
  (`album.*`/`label.*`) **in the same transaction** as the change — the responses do not change. Mounted by another `server.WithAPI`
  (`buildOrganizeAPI` in `cmd/kukatko/organize.go`).
- **Feedback / Rejections API (`/api/v1`, `internal/feedbackapi`):** persisted feedback —
  a user's "no" (and now also "yes") to a face↔subject or photo↔label estimate, and its undo.
  **Feedback is an opinion — it never mutates** the underlying data (does not detach a marker, remove a label,
  archive anything). Eight endpoints, all **RequireWrite** (editor/admin, viewer 403): `POST /feedback/face-rejections`
  `{photo_uid,face_index,subject_uid}` → 204 (rejects "this face is NOT this person"),
  `DELETE /feedback/face-rejections` (same body) → 204 (undo); `POST /feedback/label-rejections`
  `{photo_uid,label_uid}` → 204 (rejects "this photo should NOT have this label"),
  `DELETE /feedback/label-rejections` (same body) → 204 (undo) — DELETE too carries a body (like a
  label-detach); `POST /feedback/face-confirmations` `{photo_uid,face_index,subject_uid}` → 204
  and `DELETE /feedback/face-confirmations` (same body) → 204;
  `POST /feedback/duplicate-dismissals` `{photo_uid,other_uid}` → 204 ("these two photos are NOT
  duplicates") and `DELETE /feedback/duplicate-dismissals` (same body) → 204 (undo).
  The pair is **unordered** — the backend normalizes it (smaller uid first), so the uid order
  does not matter and `(A,B)` and `(B,A)` are one decision; both photos the same → 400 (`ErrSamePhoto`),
  a non-existent photo → 404. `GET /duplicates` **discards rejected pairs as edges** of the graph
  before assembling the components (`internal/duplicates`), so a two-member group disappears
  permanently, whereas a larger group survives on its remaining edges — rejecting "A is not B" is not
  a claim about C. Without this the same pair would be offered forever: detection is recomputed on every
  call, the opinion in the response does not survive.
  **Beware the polarity:** a confirmation is the **opposite** of a rejection — it says "this face **IS** this
  person, the assignment is correct". It serves the outlier review (✗ = "no, it really is them"): a confirmed face
  is excluded by `internal/outliers` from further results, so the same false alarm is not offered over and over.
  Swapping it for `face-rejections` means recording the exact opposite of what the user said.
  The `face_confirmations` table (migration `0032`) has a natural `UNIQUE (photo_uid, face_index,
  subject_uid)` and FKs with `ON DELETE CASCADE` on both photo and subject (`confirmed_by` → `SET NULL`).
  **Idempotent**: a double POST, and a DELETE of something that was not rejected/confirmed, returns 204.
  Body `DisallowUnknownFields` + 64 KiB; a missing `photo_uid`/`subject_uid`/`label_uid` or a negative
  `face_index` → 400; a non-existent photo/subject/label → 404 (`ErrTargetNotFound`). Every mutation writes an
  audit entry **in the same transaction** as the write (actions `face.reject`/`face.unreject`/`label.reject`/
  `label.unreject`/`face.confirm`/`face.unconfirm`; actor = `rejected_by`/`confirmed_by`).
  Mounted by another `server.WithAPI` (`buildFeedbackAPI` in
  `cmd/kukatko/feedback.go`). The consumers (find a person among untagged ones, recognition sweep, the review game)
  come in later tasks.
- **Places API (`/api/v1`, `internal/placesapi`, authenticated via `RequireAuth`):** browsing the
  reverse-geocoded place hierarchy + scoping the photo listing to a locality. `GET /places` →
  `{places:[{country, count, cities:[{city, count}]}]}` — counts aggregated over **non-archived**
  photos with place data; a country's `count` also includes photos without a known city (may exceed the sum of
  the cities), `cities` is always an array; ordered **count desc, then name** (for both countries and cities); photos
  without place data (no `photo_places` row or an empty `country` — a no-GPS "processed" marker) are excluded.
  Optional `?country=` drills down only into the cities of one country. The aggregation is computed by
  `photos.Store.AggregatePlaces` (a single `GROUP BY country, city` JOIN on `photo_places`, the hierarchy is
  assembled in Go). **A locality's photo gallery** runs via the shared `GET /photos?country={c}&city={c}`
  (`Country`/`City` exact match via a correlated `EXISTS` over `photo_places` in `buildWhere`, so `Count` matches;
  the same shape + the other filters/sorting/pagination, archived ones outside the default listing). Mounted by
  `server.WithAPI` (`buildPlacesAPI` in `cmd/kukatko/places.go`).
- **Saved Searches API (`/api/v1`, `internal/savedsearchapi` + `internal/savedsearch`, authenticated via
  `RequireAuth`):** per-user **saved searches** ("smart albums") — a named, owner's private
  definition of a filter/search. `GET /saved-searches` → `{saved_searches:[{uid,name,params,created_at,
  updated_at}]}` (only the current user, newest-first); `POST /saved-searches` `{name,params}` →
  201 (empty name → 400, `params` JSONB optional → `{}`); `GET /saved-searches/{uid}` → 200;
  `PATCH /saved-searches/{uid}` `{name?,params?}` → 200 (an omitted field unchanged); `DELETE
  /saved-searches/{uid}` → 204. **Every operation is scoped to the authenticated user** from the auth
  context — a saved search of another owner is **always reported as 404** (never disclosed), the body
  `DisallowUnknownFields` + a 1 MiB limit. The `saved_searches` table (migration `0017_saved_searches.sql`).
  Mounted by `server.WithAPI` (`buildSavedSearchAPI` in `cmd/kukatko/savedsearch.go`).
- **Announcement API (`/api/v1`, `internal/announcementapi` + `internal/announcement`, dual-guard):**
  a single **instance-wide announcement** (a banner for everyone logged in). `GET /announcement` behind `RequireAuth`
  (anyone authenticated reads) → `{message, level?, author_uid?, updated_at?}`; **when nothing is published it returns
  `200 {"message":""}`** (not 404 — friendlier for a polling banner client). `PUT /announcement`
  `{message,level}` behind `RequireMaintainer` → 200 with the stored record (upsert; an empty message or an unknown
  `level` (other than `info`/`warning`) → 400, an empty level → `info`); `DELETE /announcement` behind
  `RequireMaintainer` → 204 (removes the announcement for everyone). Body `DisallowUnknownFields` + a 16 KiB limit, `updated_at`
  is RFC3339. **Both publish and clear are audited** (`announcement.set`/`announcement.clear`) in the same transaction
  as the change; `author_uid` = the actor from the auth context. The single-row `announcements` table (migration
  `0039_announcement.sql`). Mounted by `server.WithAPI` (`buildAnnouncementAPI` in
  `cmd/kukatko/announcement.go`).
- **Global Search API (`/api/v1`, `internal/globalsearchapi`, authenticated via `RequireAuth`):**
  grouped **cross-entity search** for the navbar quick-results and the search page. `GET /search/global?q=` →
  `{query, albums:[{uid,title,cover,photo_count}], labels:[{uid,name,photo_count}],
  people:[{uid,name,cover}], photos:[…usual photo shape…]}` — albums/labels/people matched by
  name/description **accent- and case-insensitive** (`immutable_unaccent` + ILIKE via the store methods
  `SearchAlbums`/`SearchLabels`/`SearchSubjects`), photos via the **existing full text** (`photos.Store.
  Search` over the `fts` tsvector). Each group is capped at a small top-N (default 8, `Config.Limit`), the arrays
  are always non-nil. An empty/whitespace `q` → 400, a store error → 500. The existing `GET /search` (per-user
  photo fulltext/semantic/hybrid) stays unchanged. Mounted by `server.WithAPI` (`buildGlobalSearchAPI`
  in `cmd/kukatko/globalsearch.go`, sharing the organize/people/photos store).
- **Bulk metadata API (`/api/v1`, `internal/bulkapi`, editor/admin via `RequireWrite`):**
  `POST /photos/bulk` `{photo_uids:[…], operations:{…}}` applies a set of operations to many photos
  **in a single transaction** with an audit-log entry. Operations (each optional): `add_to_albums`/
  `remove_from_albums`, `add_labels`/`remove_labels`, `set_caption`/`clear_caption` (→title),
  `set_description`/`clear_description`, `set_location {lat,lng}`/`clear_location`,
  `archive`/`unarchive`, `set_favorite` (**per-user**), `set_rating` (0–5) / `set_flag`
  (none/pick/reject/eye) (**per-user**, invalid value → 400). Response `{results:[{photo_uid,status,
  error?}],counts:{total,updated,skipped,errored}}` (200 even on partial errors): `updated`/
  `skipped` (duplicate uid)/`error` (the photo does not exist — it **does not abort valid** ones); only a DB error
  rolls back the whole batch (500). A set/clear or archive/unarchive conflict, an unknown operation,
  a missing album/label in an add → **400**; a batch above `bulk.max_batch_size` (default 1000) → **413**.
  Mounted by another `server.WithAPI` (`buildBulkAPI` in `cmd/kukatko/bulk.go`).
- **Maps API (`/api/v1`, `internal/mapsapi` + `internal/mapy`, authenticated via `RequireAuth`):**
  a backend proxy to mapy.com (**the key never reaches the client** — only the `X-Mapy-Api-Key` header) +
  a GeoJSON feed. `GET /map/tiles/{mapset}/{z}/{x}/{y}` — a tile proxy, **streamed** with a long
  immutable `Cache-Control`; the `mapset` allowlist `basic|outdoor|aerial|winter` (other → 400, still
  before the call), retina `@2x` (a suffix on `{y}` or `?retina=true`) only for `basic`/`outdoor`,
  invalid `z`/`x`/`y` → 400. Successful tiles are **cached server-side too** (a bounded LRU +
  TTL, `maps.tile_cache_bytes`/`maps.tile_cache_ttl`) — a hit pays no mapy.com credit and is reported by
  the `X-Tile-Cache: hit|miss` header; **an error is never cached**. `GET /map/rgeocode?lat=&lng=` —
  reverse geocode → a simplified `{name,location,regional_structure}`, **cached** (key =
  the rounded coordinate) and **rate-limited** (token-bucket, geocode = 4 credits) → 429 over the
  limit, 404 for no match. `GET /map/geocode?q=&limit=` — **forward** geocode (name → coordinates)
  for the location editor → `{items:[{name,label,type,location,lat,lng}]}` ordered from the best match
  (`label` = the localized place kind „Město“/„Zámek“, `type` = the machine `regional.municipality`/
  `poi`/…, `location` = what the place contains, to distinguish several *Veselí*). An empty/long `q`
  (>200 characters) → 400 **before** the upstream call, `limit` is **clamped** to 1–15 (default 5), not 400.
  **No match = `items: []` and 200**, not 404 (even if mapy.com answers 404) — an unfinished name is
  a normal autocomplete state, not an error. It shares the cache and rate-limiter with `rgeocode` (one credit
  budget = one limiter); the cache key = the casefolded query + `limit`, **diacritics are
  preserved** (`veseli` and `veselí` are different queries at that level). `GET /map/photos` — a **GeoJSON FeatureCollection** of geotagged photos
  (coordinates `[lng,lat]`), honouring the filters `taken_after`/`taken_before`/`album`/`label`/`archived`,
  a feature carries `uid`/`title`/`taken_at`/`media_type`/a relative `thumb` and, for an estimated location,
  `location_estimated: true` (otherwise the key is **not sent at all**). Estimated photos are in the feed
  **by default** — that is what the estimate is for — but a pin that looks the same as a measured one is a silent lie, so
  the client draws them in a **different shape** (dashed, not just a different colour) + a `title`. mapy.com errors
  (**401/403 → 424** `mapsapi.StatusMapKeyRejected` = *our* key rejected, a raw 403 does not
  leak out — the caller's request is fine; 404→404, 429→429, 5xx→502/503)
  **do not leak the key**; every result is written into `mapy.Health` (→ the `GET /system/status`
  section `maps`). Without `maps.mapy_api_key`, tile/rgeocode/geocode return 503 (the location editor shows this
  as „vyhledávání míst není dostupné“ and continues on coordinates and a click into the map), GeoJSON
  works. Mounted by `server.WithAPI` (`buildMapsAPI` in `cmd/kukatko/maps.go`).
- **Import API (`/api/v1`, `internal/importapi`, maintainer-only via `RequireMaintainer`):** triggers and
  history of read-only imports. `GET /import/runs` (**always registered**) → `{runs,limit,offset,
  sources:{photoprism,photosorter,photosorter_feeds}}` — a page of `import_runs` newest-started-first (query
  `limit`≤200/`offset`, invalid → 400) + `sources` flags of which sources are configured (backing the
  admin Import UI: showing/hiding sections). The history also carries runs of the **`folder`** source
  (`kukatko import dir`, `internal/dirimport`) — those are triggered **only from the CLI** (they read a directory on
  the server's disk), so they have no trigger endpoint or `sources` flag, but they appear in `runs` like any other run. `POST /import/photoprism` → `pp_import`,
  `POST /import/photosorter` → `ps_migrate` (the legacy direct-DB migration) and
  `POST /import/photosorter-feeds` → `ps_feeds_import` (the production path: enrich PhotoPrism-imported photos
  with photo-sorter's 1:1 embeddings + faces, matched by `photoprism_uid`; configured via
  `import.photosorter.base_url`/`token`) — each (only for configured sources, otherwise 404) enqueues one
  singleton job → 202 `{job_id,status}`; `jobs.ErrDuplicate` (already running) → 409, another error → 500.
  The whole API is always mounted (`buildImportAPI` in `cmd/kukatko/import.go`), so the history works even
  without a configured source. The frontend (`ImportPage`) polls `GET /import/runs` + `GET /jobs/stats`.
- **Backup API (`/api/v1`, `internal/backupapi`, maintainer-only via `RequireMaintainer`):** the status and trigger of
  the S3 backup. `GET /backup` → status + the last run (`{configured,running,last_started_at,
  last_finished_at,last_error,last_result}`; without configuration `configured:false`); `POST /backup`
  starts a backup in the **background** (`Trigger`) → 202 `{status:"started"}`, `backup.ErrAlreadyRunning` →
  409, without configuration → 503. The whole API is mounted **always** (`buildBackupAPI` in
  `cmd/kukatko/backup.go`); the scheduler (`backup.schedule`) and the CLI `kukatko backup` share the same
  `backup.Service`. Config keys `backup.s3.{endpoint,region,bucket,access_key,secret_key,
  path_style}`, `backup.schedule` (cron), `backup.retention` (how many recent dumps to keep; ≤ 0 =
  all). Runtime dep `pg_dump` (`postgresql-client`). Secrets (`access_key`/`secret_key`) via env.
- **Restore API (`/api/v1`, `internal/restoreapi`, maintainer-only via `RequireMaintainer`):** **only
  read-only** operations over restore. `GET /restore/dumps` → `{dumps:[{key,size}]}` (the dumps in the bucket,
  newest first; 503 without configuration, 502 on an S3 error); `POST /restore/verify` → `VerifyReport`
  (photos in the DB vs originals on disk + mismatches; 503 without configuration). **A destructive DB restore is
  deliberately not exposed over HTTP** (it would pull the tables out from under a running server) — it belongs in the CLI `kukatko
  restore db` with the server stopped. The whole API is mounted **always** (`buildRestoreAPI` in
  `cmd/kukatko/restore.go`; a nil service = not configured). Runtime dep `pg_restore`
  (`postgresql-client`, the same package as pg_dump). Runbook: `docs/RESTORE.md`.
- **Audit API (`/api/v1`, `internal/auditapi`, admin-only via `RequireAdmin`):** a read-only listing of
  the durable audit trail. `GET /audit` → `{entries,total,limit,offset,next_offset}` (entry =
  `{id,actor_uid,action,target_type,target_uid,details,ip,user_agent,created_at}`, newest-first;
  for edit actions `details.changes` = `{"<field>":{"old":…,"new":…}}` with only the changed fields — see
  the `internal/audit` convention; a bulk edit `photos.bulk` does not have it)
  with the filters `?user=`/`?entity_type=`/`?entity_uid=`/`?action=`/`?since=`/`?until=` (RFC3339) and
  pagination `?limit=`(≤500)/`?offset=`; an invalid time/number → 400. In addition, **filters for the admin
  overview of one user's decisions in the review game**: `?via=review` (only review decisions —
  `details.via='review'`, i.e. the actions `face.assign`/`label.attach`/`face.reject`/`label.reject`;
  the literal matches the partial index from migration 0037) and `?decision=yes|no` (the Yes bucket = assign+attach /
  No = reject); another `via`/`decision` value → 400. Audit entries are **not added
  over HTTP** — they arise inside mutation transactions (in-tx `audit.Write`, see the `internal/audit`
  convention); the only HTTP mutation of the trail is the maintainer-only **retention purge**
  (`POST /maintenance/audit/purge`, see Maintenance API), which deletes old entries and audits itself.
  Mounted always (`buildAuditAPI` in `cmd/kukatko/audit.go`).
- **Maintenance API (`/api/v1`, `internal/maintenanceapi`, maintainer-only via `RequireMaintainer`):**
  the library's integrity check & repairs. `GET /maintenance/scan` → `Report` (counts + samples:
  `missing_originals`/`orphan_files`/`missing_thumbnails`/`missing_embeddings`/`missing_faces`/
  `missing_phashes` + the totals `photos`/`files_in_db`/`originals_on_disk`); `POST /maintenance/repair`
  `{thumbnails,embeddings,faces,phashes,import_orphans}` (each opt-in) → `RepairResult` with scheduling
  counts (`*_enqueued` + `orphans_imported/skipped/failed`); `DisallowUnknownFields`, an empty selection →
  400, an orphan import without an importer → 503 (`ErrOrphanImportUnavailable`). The repairs are idempotent and
  run through the job queue (thumbnail/pHash via the `thumbnail` job, embeddings/faces backfill), and **never
  delete originals**. `POST /maintenance/audit/purge` `{older_than_days}` (a positive integer of days,
  1..36500) deletes audit entries older than `now − older_than_days` (`audit.Store.PurgeOlderThan`,
  a single `DELETE` via `idx_audit_log_created_at`) → `{deleted,older_than_days,cutoff}`;
  a missing/non-positive/excessive window or an unknown field → 400, an unwired audit store → 503. The
  purge itself is **audited** (`audit.purge` with the cutoff, the window and the number deleted; the entry is fresh, so
  the purge survives) — deleting the trail stays traceable. Mounted always (`buildMaintenanceAPI`
  in `cmd/kukatko/maintenance.go`).
- **Duplicates API (`/api/v1`, `internal/duplicatesapi` + `internal/duplicates`, editor/admin via
  `RequireWrite`):** `GET /duplicates?limit=&offset=` → `{groups,total,limit,offset,next_offset}`
  groups of likely duplicates from pHash Hamming distance (`duplicate.phash_max_diff`,
  banded-LSH) **and/or** embedding cosine distance (`duplicate.embedding_max_dist`, HNSW), merged by
  union-find into connected components (no O(n²) scan). Each group carries members (thumbnail/dimensions/
  size/`taken_at`/distances) + `reason` (phash/embedding/both) + a suggested `keeper_uid`
  (highest resolution → largest → oldest → uid); ordered largest-first, `limit`≤100, invalid →
  400, scan fails → 500. The listing **only reads**; when `duplicate.enabled=false` the `GET` route answers 503.
  `POST /duplicates/merge` (`internal/dupmerge`, `RequireWrite`) `{keeper_uid,member_uids[],dry_run?}` →
  `{keeper_uid,albums_added,labels_added,people_added,metadata_filled[],archived,dry_run}`: in **one
  transaction** it merges the remaining copies into the chosen keeper — a union of albums, labels and people
  (a subject↔keeper marker without a box, type `label`), fills the missing scalar fields (title/description +
  per-user rating/favorite/flag; never overwrites an existing value), archives the copies (`archived_at`, originals
  to purge) and writes `photos.merge` to the audit. Idempotent (re-running on a resolved group = a no-op);
  `dry_run:true` only computes a preview without changes. An invalid group → 400, a non-existent keeper → 404,
  `merge=nil` → 503. The `merge` route runs even with detection off. Mounted always by `buildDuplicatesAPI` (`cmd/kukatko/duplicates.go`).
- **System status API (`/api/v1`, `internal/systemapi` + `internal/system`, maintainer-only via
  `RequireMaintainer`):** `GET /system/status` → one aggregated snapshot of operational health:
  `{version,database{reachable,error?},embeddings{online,url},jobs{by_state,by_type,total,dead_letter,
  pending_embeddings},backup (=backup.Status),imports{photoprism,photosorter (=importer.Run|null)},
  storage{originals_bytes,cache_bytes,free_bytes,total_bytes},
  maps{configured,state,degraded,detail?,checked_at?}}`. `maps` = the last observed mapy.com state
  from the proxy (`mapy.Health`, no probe of its own): `state` ∈ `unknown|ok|key_rejected|rate_limited|
  unavailable|error`, `degraded=true` for all except `ok`/`unknown` — **a rejected key (403) is
  visible here**, not only as a grey map; `detail` is sanitized (never the key).
  A merge of existing subsystems
  (embeddings health, the job queue, backup status, the last import per source via
  `importer.Store.LatestRun`, disk usage, a DB ping); storage is memoized for 30 s. Collect fails (the DB
  for the queue/imports) → 500; an unavailable DB/storage is inline best-effort. Mounted **always**
  (`buildSystemAPI` in `cmd/kukatko/system.go`). The admin UI **System** (`/system`, `SystemStatusPage`)
  polls every 5 s and offers quick actions (requeue dead-letter, trigger backup, links to import/maintenance).
- **Capabilities API (`/api/v1`, `internal/capabilitiesapi`, authenticated via `RequireAuth`):**
  `GET /capabilities` → `{semantic_search:bool}` — a small object of instance feature-flags that
  **every authenticated user** may read (unlike the maintainer-only `/system/status`). `semantic_search` is
  the **cached** reachability state of the embeddings sidecar (not a live probe): filled by the background loop
  `internal/reachability` (a probe every 60 s, `cmd/kukatko/capabilities.go`); when `embedding.url` is not
  set, it is always `false`. The shape is **deliberately open** for future flags (e.g. maps-configured).
  The frontend (`CapabilitiesProvider`) polls it and hides the link to semantic search in
  `FilterBar` accordingly, when the box is offline (full text keeps working). Mounted **always**.

## Search language (q=)

The `q` parameter on `GET /photos` and `GET /search` (and, through `parseListParams`, also on `/photos/timeline`,
`/photos/years` and `GET /favorites`) accepts a **search language**: free text and `key:value`
filters mixed in one string. It is parsed by `internal/query` (a pure parser → AST), compiled to SQL by
`internal/photos` (`store_query.go`) — **everything via pgx parameters**, no concatenation of
user values.

```
dovolená camera:"Canon EOS R6" iso:100-400 faces:2
```

**Free-text semantics do not change:** on `GET /photos` the remaining free text is a substring filter
(ILIKE over title/description/notes), on `GET /search` it goes into the fulltext/semantic/hybrid ranking.
Filters only narrow the result (AND). A query **without free text** is a pure filter query — `/search`
handles it on the list path (`mode: "filter"`) and **does not touch the embedding sidecar**.

### Operators

| Syntax | Meaning |
| --- | --- |
| a space between filters | AND — `iso:100-400 faces:2` |
| `\|` inside a value | OR — `label:cat\|dog` |
| `!` before a value | NOT — `label:!blurry`; combinable per alternative: `label:cat\|!dog` |
| `-` before a word | NOT for free text — `-rozmazané` |
| `lo-hi` | a numeric range, both sides optional — `iso:200-400`, `iso:800-`, `iso:-200` |
| `*` | a wildcard in a text value — `filename:IMG_*`; without `*` it matches a substring |
| `"…"` | a value with spaces — `camera:"Canon EOS R6"`; text in quotes is literal |
| `\` | escapes an operator (pipe, `!`, `-`, `"`, `:`), so it matches literally |

An escape example: `label:a\|b` (a backslash before the pipe) searches for a label with a literal
pipe `a|b` instead of an OR of two alternatives; likewise `iso:100\-400` is no longer a range, and therefore
degrades to free text. Keys are case-insensitive (`ISO:100` = `iso:100`). **An unknown key or an invalid value is not
an error**: the whole token is searched as ordinary text (so `foo:bar` still finds a photo by its caption)
and the response returns it in `unknown_tokens`, so the UI can show a hint. An exact fractional match
(`f:1.8`) is tolerated within ±0.005 due to the rounding of single-precision EXIF columns.

### Filters

| Filter | Value | Matches |
| --- | --- | --- |
| `title:` `description:` `notes:` | text | the corresponding photo column (substring, `*` wildcard) |
| `filename:` | text | the file name |
| `keywords:` (alias `keyword:`) | text | IPTC keywords |
| `album:` | text | album membership by **name** (substring) or exact UID |
| `label:` | text | a label by **name** or UID |
| `person:` (alias `subject:`) | text | a subject by **name** or UID, via non-invalid markers |
| `favorite:` `private:` `archived:` | `yes\|no` | per-user favourite / private / archived; `archived:` **removes the default live-only scope** |
| `rating:` | `0-5`, ranges | the current user's rating; no row = 0, so `rating:0` finds the unrated |
| `flag:` | `pick\|reject\|eye` | the current user's flag |
| `year:` `month:` `day:` | number, ranges | year (1000–9999) / month (1–12) / day (1–31) of capture |
| `taken:` `added:` | `YYYY`, `YYYY-MM`, `YYYY-MM-DD` | date of capture / of adding to the catalog (whole day/month/year) |
| `before:` / `after:` | a date as above | captured **before** the start of the date / **from** the start of the date |
| `country:` `city:` | text | country/city from reverse geocoding (`photo_places`) |
| `geo:` | `yes\|no` | has / has no GPS coordinates |
| `alt:` | number (m), ranges | altitude (non-negative only — `-` is the range operator) |
| `near:` | a photo's UID | photos within `dist:` km of the given photo (spherical distance; the reference photo matches too) |
| `dist:` | km | the radius for `near:` (default **5 km**); it does not filter on its own |
| `camera:` | text | the camera's make **or** model |
| `lens:` | text | the lens model |
| `iso:` `f:` `mm:` `mp:` | number, ranges | ISO / aperture / focal length / megapixels (`width×height/10⁶`) |
| `type:` | `image\|video\|live` | the media type |
| `codec:` | text | the image **or** video codec (`hevc`, `jpeg`, …) |
| `portrait:` `landscape:` `square:` `panorama:` | `yes\|no` | orientation by effective dimensions (EXIF orientation 5–8 swaps the sides); panorama = ratio ≥ 1.9 |
| `faces:` | `yes\|no`, number, range | the count of non-invalid face **markers**; a bare number = a **minimum** (`faces:3` ≥ 3), a range bounds both sides |
| `face:new` | enum | the photo has a detected, still **unassigned** face (`faces.subject_uid IS NULL`) |

Booleans accept `yes/no`, `true/false` and `1/0`. Per-user filters (`favorite:`, `rating:`, `flag:`)
are always scoped to the caller (`RatedBy`); without an authenticated user they are inert.
Structured query params (`?album=`, `?label=`, `?year=`, …) **keep working unchanged** —
the language is purely additive and saved searches stay compatible.
