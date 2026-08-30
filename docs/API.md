# HTTP API

Descriptive reference overview of the HTTP endpoints under `/api/v1`. **These are not rules** —
the rules live in [`CLAUDE.md`](../CLAUDE.md). Record any new or changed endpoint here.

<!-- BODY BEGIN -->
- **Auth API (`/api/v1`):** `POST /auth/login` (set HttpOnly+SameSite=Strict cookie + opaque
  `download_token`), `POST /auth/register` (anonymous, see below), `POST /auth/logout`,
  `GET /auth/me`, `POST /auth/password` (revokes other
  sessions), `GET|POST /auth/password-reset/{token}` (anonymous, see below), `PUT /auth/subject`,
  `POST /auth/welcome-seen`. Admin-only: `GET|POST /admin/users`,
  `PATCH /admin/users/{uid}`, `POST /admin/users/{uid}/approve`,
  `POST /admin/users/{uid}/disable`, `POST /admin/users/{uid}/password` (reset revokes all of the
  user's sessions), `POST /admin/users/{uid}/password-reset` (see below).
  Responses of the admin user endpoints carry a free-form **`note`** alongside
  `display_name` (an admin note on why the account exists / who it is). Both fields are optional,
  defaulting to the empty string. A `note` longer than **1000 characters** (runes, not bytes) → 400
  with a message naming the field. `PATCH` gives `note` **partial-update** semantics: an omitted key
  leaves the stored note unchanged, `""` clears it. **Only an admin reads `note`** — it is never in the
  `POST /auth/login` or `GET /auth/me` payload.
  **The e-mail address is required.** Every account receives mail — registration, approval, password
  reset — so `email` is not optional on either `POST /admin/users` or `PATCH /admin/users/{uid}`: a
  missing, blank or syntactically invalid address → **400** (`auth: email must be a valid address`).
  The address is normalised on the way in — surrounding whitespace trimmed, **domain lower-cased**,
  local part left exactly as typed (RFC 5321 leaves its meaning to the receiving host) — and capped at
  **254 bytes**. Rejected are a display-name form (`Jan <jan@example.cz>`), an address with inner
  whitespace, and a dotless domain (`jan@localhost`), none of which an account here could receive mail
  at. **Two accounts may share an address**: a household mailbox is a real arrangement, so there is no
  unique constraint. The `PATCH` *replaces* the profile, so an update that omits `email` clears it and
  is refused like any other blank one — the client echoes the stored address back (or offers it for
  editing). Accounts that predate this rule were given an undeliverable placeholder in the reserved
  `.invalid` domain (`<username>-<uid>@kukatko.invalid`, migration 0063), as is the **bootstrap admin**
  created on an empty database, so a first start still needs no mailbox.
  **Which person the account is:** every user payload — login, `/auth/me` and the admin listings —
  carries **`subject_uid`**, the subject of the library this account belongs to, or `null`. Unlike
  `note` it *is* in the login and `/auth/me` payloads, deliberately: the client cannot render the
  "my photos" entry, resolve `person:me` in a link or draw the account's face without it, and it is
  the caller's own fact about themselves. **Several accounts may name the same subject** (a shared
  family login and a personal one are both that person), and **deleting the subject clears the link**
  (`ON DELETE SET NULL`), so every reader must survive `null` — the common case.
  Self-service: **`PUT /auth/subject`** (`RequireAuth`, any role) with `{"subject_uid": "sub…"}` or
  `{"subject_uid": null}` → 200 with the refreshed user; the account written to is **the session's**,
  never one named in the body. A UID naming no subject → **400**. It writes **no audit entry**,
  following `POST /auth/password`: the trail records what was done *to* an account by somebody else.
  Admin: `POST /admin/users` and `PATCH /admin/users/{uid}` accept `subject_uid` as part of the
  **replaced** profile (unlike `note`, an omitted or null value **clears** the link), 400 for an
  unknown subject, and both record it in the `user.create`/`user.update` audit details.
  Linking an account **publishes that person's cover photo** next to every comment the account has
  written (see the comment thread below); the surfaces that set it say so. A subject's `private` flag
  gates no reading today and is unchanged by this.
  **Approval and the first-run welcome:** every user payload carries two nullable timestamps.
  **`approved_at`** is when an administrator let the account in; `null` means *registered, waiting for
  an administrator*. Every account made through `POST /admin/users` (and the bootstrap maintainer) is
  approved on creation — an administrator creating an account **is** the approval — so `null` belongs
  to the accounts `POST /auth/register` makes, until `POST /admin/users/{uid}/approve` (below) fills it; accounts predating migration 0064
  were backfilled as approved with their `created_at`. It is **not** the inverse of `disabled` and no
  reader may collapse the two: *never approved* and *approved and later blocked* are different states,
  they mean opposite things to the person holding the account, and the admin listing shows both
  columns. **`welcome_seen_at`** is when the account's owner last dismissed or completed the first-run
  welcome (the Markdown an admin writes in `PUT /settings`), or `null` when they never have; it is in
  the `GET /auth/me` payload for the same reason `subject_uid` is — the client cannot decide whether to
  open the welcome without it.
  **`POST /auth/welcome-seen`** (`RequireAuth`, **any role** — the welcome is shown to everybody who
  signs in) stamps `welcome_seen_at` with the current time and answers **200** with the refreshed user,
  so the client needs no second trip to `/auth/me`. The account written to is **the session's**; there
  is no body and nothing to point at somebody else. It is **idempotent**: the stamp is written only
  while the column is still `null`, so a second call returns the first call's timestamp unchanged and
  the time can never move backwards. Like `PUT /auth/subject` it writes **no audit entry** (the trail
  records what was done *to* an account by somebody else) and leaves `updated_at` alone (the admin
  screens read that as "an administrator edited this profile").
  Roles: a **strict ladder** viewer < editor < admin <
  maintainer (each inherits the rights of the lower ones): viewer read-only, editor adds writing of
  media/metadata, admin governance (user management, audit log, permanent deletion / emptying the
  trash), maintainer operations (imports, maintenance, system, backup/restore, jobs, process).
  **Last-maintainer guard:** `PATCH /admin/users/{uid}` and `POST /admin/users/{uid}/disable` answer
  **409** (`auth: cannot remove the last maintainer`) when the change would drop the number of
  **enabled** maintainers to zero — a demotion, a disable, or both at once, including the caller's own
  account. Such a state has no way back through the API (granting `maintainer` is itself
  maintainer-only, there is no delete-user endpoint, and `Bootstrap` only runs on an empty users
  table), so every operations surface would need database surgery to restore. It is a 409 rather than
  a 403 because the caller *is* allowed to make the change — promoting a second maintainer first makes
  the identical request succeed. A disabled maintainer does not count towards the invariant; an
  instance that has no enabled maintainer at all stays fully editable (the guard only forbids
  *dropping* to zero, never being there).
  A **username** longer than **64 characters** (runes, not bytes) → 400 with a message naming the
  field, on both `POST /auth/login` and `POST /admin/users`; login checks it *before* the username
  reaches the rate limiter, so the public endpoint cannot be flooded with oversized limiter keys.
  **Sliding session expiry**
  (`auth.session_ttl` up to the cap `auth.session_max_lifetime`), **login rate-limit**
  (`auth.login_rate_limit`/`auth.login_rate_window` → 429 — counted per (username, client IP) **and**,
  independently of the address, per username at 3× that budget over the same window, so an attacker who
  moves between addresses still runs out; the address comes from `internal/clientip`, i.e. the socket
  peer unless `web.trusted_proxies` says the peer is a proxy, never from a header the caller picked).
  A login **fails in the same time** whichever way it fails — unknown user, disabled account and wrong
  password all run one bcrypt comparison — so response latency does not reveal which usernames exist;
  all three answer **401** with the same body.
  **Waiting for approval is its own sign-in outcome:** an account whose `approved_at` is `null`, given
  the *right* password, answers **403** `auth: account is waiting for approval` and **creates no
  session** — distinct from the 401 of wrong credentials and from a blocked account (which stays a
  401, blocked *and* unapproved included), so the sign-in screen can say what is being waited for
  instead of blaming a password that works. It is only ever reached by a caller who already holds the
  credentials — the check runs after the bcrypt comparison — so it tells a guesser nothing. The
  client maps it to its own message (`login.errorPendingApproval`).
  **`POST /auth/register` (no authentication) — self-service registration.** Body:
  `{username, display_name?, email, password, secret}`; unknown fields → 400. It creates the account
  as a **viewer**, **not approved**, so it exists and cannot be used until an administrator fills
  `approved_at`; there is no session and no cookie in the answer. **201** returns
  `{username, display_name, email, pending_approval: true}` — the stored (normalised) values, and
  deliberately not the user payload: an anonymous caller learns neither the UID nor the role.
  Whether it is open at all, and the shared `secret` it demands, come from the instance settings
  (`PUT /settings`, see the Settings API) and change without a redeploy. Refusals: **403**
  `auth: registration is not open` when registration is switched off — and equally when it is on with
  a **blank** stored secret, an open door with no lock; **403** `auth: wrong registration secret` for
  a secret that does not match (compared in **constant time**, so the answer's latency says nothing
  about how much of it was right); **409** for a username somebody already holds; **400** for the same
  input the admin user API refuses (over-long username, missing/malformed address, weak password —
  identical validation and identical bcrypt hashing, so nothing can be registered that an
  administrator could not have created). Two accounts may share an e-mail address here too.
  It is **rate-limited per client address** (`internal/ratelimit` middleware, → **429**) with the
  login budget (`auth.login_rate_limit`/`auth.login_rate_window`) spent from one bucket per address —
  at least as strict as signing in, whose budget is also split per username; a registration names no
  existing account, so the address is all there is to attribute it to.
  On success **two mails are enqueued on the job queue** (`mail_send`, see `internal/mailjob`), in the
  **same transaction** as the account and its `user.register` audit entry: the `registration_received`
  confirmation to the person, and one `new_registration_pending` notice per **enabled admin or
  maintainer**, naming the username, display name and address. A registration that rolls back
  therefore sends nothing, and mail switched off (`mail.enabled: false`) or an administrator with a
  placeholder `.invalid` address costs only the notification — the registration still succeeds. The
  audit entry names the new account as **both actor and target**: nobody else was involved.
  **`POST /admin/users/{uid}/approve` (admin) — letting a waiting account in.** No body; **200**
  returns the updated account (the admin view, `note` included) with `approved_at` filled from the
  server clock. It **does not change the role**: the account was registered on `viewer` and raising it
  is a separate, deliberate `PATCH /admin/users/{uid}` — "may come in" and "may edit the library" are
  two decisions. Approving an **already approved** account is **200 with the account unchanged** —
  no second stamp, no second mail, no second audit entry — because an administrator clicking twice must
  not see a failure. Approving a **blocked** account is **409** (`auth: user is disabled`): unblocking
  is the existing action (`PATCH` clearing `disabled`) and conflating the two would hide which one was
  meant; it is a 409 rather than a 403 for the same reason `ErrLastMaintainer` is — re-enabling the
  account first makes the identical request succeed. The **maintainer boundary** applies exactly as on
  the other user-management endpoints: an admin approving a **maintainer** account → **403**
  (`auth: only a maintainer may manage the maintainer role`); a UID naming nobody → **404**. On a real
  approval one mail is enqueued on the job queue (`account_approved`, `mail_send`) **in the same
  transaction** as the stamp and the `user.approve` audit entry, so a rolled-back approval promises
  nobody anything; its sign-in link is `mail.base_url` + `/login`.
  **Password reset by link — three endpoints.** They exist because `POST /admin/users/{uid}/password`
  works but means the administrator learns a password its owner may use elsewhere; a link moves the
  choice back to that owner.
  **`POST /admin/users/{uid}/password-reset` (admin)** — no body; **200**
  `{reset_url, expires_at, email}`. It mints a one-time token valid for **7 days**, enqueues the
  `password_reset` mail carrying the link to the account's address (`mail_send`, in the **same
  transaction** as the token and its `user.password_reset` audit entry) **and answers the whole link**,
  so an administrator can also pass it on by hand — which is the only way on an instance with
  `mail.enabled: false` or an account with a placeholder `.invalid` address. **Issuing invalidates the
  account's earlier unused link**: only the most recent one works. The **maintainer boundary** applies
  (an admin resetting a **maintainer** → **403**), a blocked account is **409** (`auth: user is
  disabled`) — a link that could never be used would only make somebody wait — and an unknown UID is
  **404**. The link is `mail.base_url` + `/password-reset/<token>`; the database keeps only a SHA-256
  **hash** of the token, so reading the table hands nobody a working link.
  **`GET /auth/password-reset/{token}` (no authentication)** — **always 200**: `{"valid": false}` for a
  link that is unknown, already used, expired or whose account has since been blocked (the four are
  deliberately indistinguishable), and `{"valid": true, "display_name", "expires_at"}` for a usable
  one, so the page can say *this link has expired* instead of showing a form that is going to fail. It
  publishes **nothing else** about the account — a display name to greet the person with is the most a
  bearer of the token needs.
  **`POST /auth/password-reset/{token}` (no authentication)** — body `{password}` (unknown fields →
  400) → **204**. The token is **consumed**, so a second use fails; **all** of that account's sessions
  are deleted, including any signed in at that moment; the password goes through the same length rule
  and the same bcrypt hashing as every other password change (**400**, `auth: password must be at
  least 8 characters`, for a weak one — and a refused password does **not** spend the link). Every
  unusable link is one unspecific **404** (`auth: the password-reset link is not valid`). The
  consumption is audited as `user.password_reset_use` with the account as **both actor and target**, in
  the same transaction; both entries carry the link's id in `details.reset_id`, which is what ties an
  issued link to its use.
  Both public endpoints are **rate-limited per client address** (`internal/ratelimit` middleware →
  **429**) out of the login budget, exactly like `POST /auth/register`. Expired and consumed rows are
  pruned by the hourly cleanup that already removes expired sessions.
  **`GET /admin/users?pending=` — finding the accounts that are waiting.** `pending=true` lists only
  the accounts with `approved_at = null`, `pending=false` only the ones already let in, and an absent
  parameter everybody (the default). A value that is not a boolean → **400** (`pending must be true or
  false`) rather than a listing of something else than what was asked for.
  **Bootstrap admin** from
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
  **an album scope always forces chronology** (≥ 1 selected album is enough): the sort **key** is pinned
  to the capture time (a photo without one falls back to its upload time `created_at`, so the order is
  complete and stable) whatever `sort` the query asks for, and only the **direction** is the caller's —
  an explicitly descending request (`?sort=newest`, or `?order=desc`) reverses it, everything else, an
  absent `sort` included, stays oldest-first. The endpoint's defaults for other views are unchanged;
  **`?sort=random&seed=…` plays a pseudo-random order** — what the slideshow's shuffle asks for. The
  permutation is `md5(uid || seed)`, so it depends on nothing but the photo and the seed: repeating the
  seed on every request means the pages of one shuffled show never overlap and never drop a photo
  between them, and a new seed deals a new order. `seed` is free-form text, max 64 characters
  (longer → 400), and is ignored by every other sort; `order` does not apply (a random order has no
  direction). It is the **one sort an album scope does not override** — a shuffle is an outright request
  to abandon the order, not a stale sort key — and the one sort `GET /search` honours instead of its
  relevance ranking (`fulltext` orders by the same digest; `semantic`/`hybrid`, ranked in Go, permute
  their fused result the same way before paginating);
  `GET /photos/timeline` (authenticated) — a **monthly date histogram** of the library (backing the
  year/month scrubber): accepts the **same filters** as `GET /photos` via `parseListParams`, response
  `{buckets:[{year,month,count,cumulative}],total}`, `cumulative` = the number of photos **before** the
  bucket (maps the bucket to a scroll index), `total` (via `Count`) also includes photos without a
  capture date. The histogram **mirrors the grid's order**: newest first by `taken_at` by default,
  oldest first for an ascending request, and grouped on the same `COALESCE(taken_at, created_at)` an
  album scope is ordered by — so under an album no photo falls outside a bucket and `cumulative` is an
  exact grid index. The sort *key* is otherwise ignored (always grouped by date). Backed by
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
  `GET /photos/uploaders` (authenticated) — **who uploaded into the current view** (backing the filters'
  **uploader control**): accepts the **same filters** as `GET /photos` via `parseListParams`, response
  `{uploaders:[{uid,name,count}]}`, **largest contribution first** (ties by uid). In an event album it
  therefore lists that event's contributors, not every account on the instance. The photos with **no**
  uploader are reported as their own entry (`uid: ""`, `name: ""` — the client names the group), so the
  counts add up to what the grid shows. `name` is the uploader's display name, falling back to their
  username. The `uploader` filter is **the only one ignored** — a facet must not narrow its own offering;
  `sort`/`order` and pagination are ignored (always grouped by uploader).
  Backed by `photos.Store.UploaderBuckets` (shares `buildWhere` with `List`/`Count`), invalid param → 400;
  the `?uploader=` filter on `GET /photos` takes a **user UID**, or the reserved value **`none`** for the
  photos with no uploader at all (the imported ones) — the same word the query language's `uploader:none`
  uses, and no UID can collide with it;
  `GET /search?q=&mode=` (authenticated) — **semantic + hybrid search**, `mode` =
  `fulltext`|`semantic`|`hybrid` (default `hybrid`, unknown → 400): **fulltext** = Czech-aware
  full-text over `fts tsvector` (dictionary `simple` + `unaccent`, ranking `ts_rank`
  title>description>notes>file_name); **semantic** = `q` → SigLIP 2 embedding via sidecar →
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
  a query the server understood but **cannot satisfy** (today only `person:me` from an account with no
  linked person) comes back **empty** with the reason in `notices: []string` (`person_me_unlinked`),
  never widened to the whole library;
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
  `username`); omitted (`omitempty`) for photos without `uploaded_by` (the one-off imports),
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
  via `photoedit.Apply` unless the caller passes `?original=true`). A saved edit also **audits**
  (`photo.edit`, the whole edit in the details) and **enqueues a forced `thumbnail` job**
  (`jobs.Enqueuer.EnqueueThumbnailRebuild`), so the derived thumbnails are rebuilt from the edited
  rendering and the library grid shows the photo the way its owner turned it. Saving the **neutral** edit
  is not a special case: it audits and rebuilds too, which is what makes a reset restore the original
  rendering everywhere rather than only in the viewer. Both are best-effort — a failure is logged, never
  returned, the edit is stored either way;
  `PATCH /photos/{uid}` (editor/admin) partial edit of
  metadata — `title/description/notes/ai_note/taken_at/lat/lng` (null clears a nullable, coordinate
  validation) **+ approximate date** `taken_at_estimated` (bool — the date is an estimate, not a fact) and
  `taken_at_note` (free text about the dating, whitespace trimmed, **max 500 characters**, longer = 400).
  The note applies only to an estimate: once the resulting `taken_at_estimated` is `false` (the client
  dropped it, or the photo never had it), the server **clears** `taken_at_note` — a date presented as a
  fact never keeps a stale note hanging (the length is still validated first, so an overly long note is
  reported, not silently discarded). `taken_at` NULL + `taken_at_estimated` `true` is legal
  (the note carries the meaning) and the flag has no effect on sorting/timeline/filters.
  **Clearing `taken_at` is reversible**: the date the photo carried is moved into the read-only
  `taken_at_before_unknown` in the same statement, so the wrong date a scan was stamped with — the usual
  reason for saying "unknown" — is recoverable rather than destroyed. Stating a real date afterwards clears
  it again (there is nothing set aside any more); clearing an already-cleared date keeps what is preserved
  instead of overwriting it, and clearing a photo that never had a date preserves nothing. The field is
  **provenance only**: it is served on every photo (`omitempty`, so absent unless a date was cleared), it is
  never editable, nothing sorts, groups or filters by it, and it rides in the metadata sidecar
  (`temporal.taken_at_before_unknown`, format version 4) so the fact survives losing the database
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
  `POST /photos/{uid}/hide`+`/unhide` (editor/admin) set/clear `hidden_from_library` and return the
  refreshed photo (404 missing, audited as `photo.hide`/`photo.unhide` in the mutation's transaction).
  This is **not** archiving and **not** `private`: nothing is deleted or scheduled for deletion. A hidden
  photo leaves the library grid and its counts, the timeline/year buckets, the map and places, the
  slideshow, the review game and the **default** search — and stays fully visible in album and label
  galleries, in favourites, and at its own `GET /photos/{uid}`. The filter lifts itself whenever the
  listing is scoped to an album, a label or the caller's favourites, so those galleries need no flag;
  `q=hidden:yes` lists the hidden ones (§ Search query language). The flag rides in `photos.Photo` as
  `hidden_from_library`, so **every** response with a photo carries it;
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
  to a signed URL (`Cache-Control: private, no-store`, so the cache does not outlive the signature), unless
  **`?proxy=true`** asks for the streaming branch anyway (see the share manifest below); the routes
  remain, so old links and bookmarks keep working. The streaming branch of `/thumb/{size}` reads the size
  through `thumb.OpenOrGenerate` (local cache → the published object → generate), so it also answers on a
  backend that publishes objects but mints no signed URL, where the thumbnail may exist only in the bucket. `GET /photos/{uid}/video` (session/`?t=` token) streams
  video **with HTTP Range** (206 partial, `Accept-Ranges`, seek; a live photo = a motion clip, still → 404)
  for inline HTML5 playback, or redirects to the Worker, which serves Range directly from R2 (dropping the
  requirement of a seekable local file); an optional on-the-fly transcode of non-web-friendly codecs via
  the `video.transcode` config (default off) feeds `ffmpeg` a signed URL directly (`ffmpeg` reads http(s)).
  **Scrub previews** (`internal/photoapi/storyboard.go`, `internal/storyboard` + `internal/storyboardjob`):
  `GET /photos/{uid}/storyboard` (**RequireAuth** — whoever may watch may see the previews, viewers included)
  answers **always 200** for a photo that exists, with `{status}` ∈ `ready|pending|unavailable` and — **only when
  `ready`** — the sprite's grid `{columns,rows,count,tile_width,tile_height,interval_ms}` (omitted otherwise, so a
  client that reads `status` first can never place a preview against a zero grid). It is **a GET that schedules
  work**, deliberately: a `pending` answer enqueues the `storyboard` job (queue dedup ⇒ asking on every playback
  costs nothing), while a POST would either be denied to viewers or duplicate the read. `unavailable` is permanent
  (a still, a live photo, a clip of unknown length, no ffmpeg) and means "stop asking"; no wiring at all also
  answers `unavailable` rather than 503 — a video without scrub thumbnails is not a broken page. A missing photo
  → 404. `GET /photos/{uid}/storyboard/sprite` (session/`?t=` token, like every other media route) **streams**
  the sprite JPEG (`Cache-Control: private, max-age=31536000, immutable`, `ETag "<file_hash>-sb"`, `304`);
  every reason there is no sprite — not generated yet, not a video, unknown duration, unknown photo — is a
  **404**, which the player reads as "no preview" and nothing worse. The sprite is **cache-only derived media**
  (never published to the object store), so this route is always where the bytes come from — the client
  builds the address rather than reading it off a payload.
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
  **Share manifest** (`internal/photoapi/share.go`): `POST /photos/share-manifest`
  (session/`?t=` token — **the same authorization as a single download**; naming a file is a step towards
  fetching it, and the fetch goes through that same guard) answers what a selection *is as files*, for handing
  them to the phone's own share sheet (iOS "Save Images" → Apple Photos, Android → Google Photos). Body
  `{photo_uids}` (the client's selection order, a UID resolving to nothing is **silently skipped**), answer
  `{files:[{uid,name,mime,size,preview}]}`. It exists because the page holds **UIDs, not photos** — a selection
  outlives the grid rows it was made from — while the batching it must do needs each file's name, type and size;
  one request answers for the whole selection instead of one detail fetch per photo. `name` is de-duplicated
  within the manifest by the ZIP's own rules (`sanitizeEntryName` + `uniqueEntryName`, so two `IMG_0001.jpg`
  arrive as two files), `preview` marks a **RAW original** (`imgconvert.IsRAWName`) to be fetched as its largest
  cached JPEG instead — a phone library handles a CR2 badly — and is then named `.jpg` and typed `image/jpeg`,
  with `size` staying the *original's* (an upper bound, so a batch can only come out smaller than the budget).
  Cap **1000 files** (`maxShareFiles` = `maxZipFiles`) → **413**; a selection resolving to nothing → 400. It
  deliberately returns **no URLs**: the client already addresses originals and previews, and must be free to
  retry a preview at a smaller size. **`?proxy=true` on `/photos/{uid}/download` and `/photos/{uid}/thumb/{size}`**
  is what makes those bytes fetchable by the page: it suppresses the redirect branch and streams through the
  application, because `fetch` follows a 302 to the media domain and then refuses to let the page read it (an
  opaque cross-origin response is the most likely way this feature would silently fail). It is a choice of
  *transport only* — the route's guard is unchanged, and proxying is the same grant of access the signed URL
  performs — so it stays opt-in per request rather than the default, which would move every byte through the
  application for an `<img>` that only wanted to paint it.
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
  **Processing** (`internal/photoapi/processing.go`, the `ProcessingService` interface =
  `processing.Service`, **nil → the detail omits the block and the endpoint answers 503**):
  `GET /photos/{uid}` carries **`processing`** — one entry per per-photo computation, in the fixed order
  `metadata`, `thumbnail`, `image_embed`, `face_detect`, `ocr`, `places`, `sidecar`
  (`storyboard` is deliberately absent: it is rendered lazily on first playback and leaves no persisted
  evidence). Each entry is `{step, state, at?, error?, face_count?, text_found?}`. The state is decided by
  **persisted evidence first** (`photos.metadata_extracted_at`, a `photo_phashes` row, an `embeddings` row,
  a `face_detections` row, `photos.ocr_at`, a `photo_places` row **with coordinates**,
  `photos.sidecar_written_at`) → `done` with `at`; then by the **queue** (`jobs` for this `photo_uid` and
  type) → `running`/`queued`/`failed`, where `failed` covers a dead job **and** one whose last attempt
  errored, carrying its `last_error` as `error`; then `skipped` for a step that cannot apply (`places`
  without GPS, `face_detect`/`ocr` on a video, or a feature switched off instance-wide — no worker handler
  is registered for it); otherwise `pending`. `face_detect` adds `face_count` and `ocr` adds `text_found`
  on a done step, so a result that legitimately found nothing does not read as a gap. The whole array costs
  **two round trips** (one for the evidence, one for `jobs.Store.UnfinishedForPhoto`), never an N+1.
  `POST /photos/{uid}/process/{step}` (**maintainer** via `RequireMaintainer` — scheduling background work
  is operations, not curation) enqueues that one step for that one photo through the shared
  `jobs.Enqueuer`, so the dedup index makes a double click harmless, and answers **200** with the step's new
  state (the same entry shape) — 400 unknown step (including `storyboard`), 404 missing photo, **409** the
  step does not apply to this photo, 503 unwired.
  Mounted by the third `server.WithAPI` (`buildPhotoAPI` in `cmd/kukatko/photos.go`).
- **Comments API (`/api/v1`, `internal/photoapi` + `internal/comments`):** per-photo comment threads — the
  family conversation around a picture ("who is the boy on the left?", "this was the summer before the barn
  burned down"). `GET /photos/{uid}/comments` (authenticated) →
  `{comments:[{uid,photo_uid,author_uid,author_name,author_photo_uid?,body,created_at,edited_at?}]}`, **oldest first** (a
  conversation reads forwards), `author_name` resolved server-side (display name, fallback username) so a
  thread needs no second lookup; **`author_photo_uid`** is the cover photo of the person the author's
  account is linked to (`users.subject_uid` → `subjects.cover_photo_uid`, one LEFT JOIN in the same
  statement), so the client can draw a face instead of an initial. It is **absent** for an authorless
  comment, an unlinked account, and a linked person with no cover photo — the common case, which is why
  the initials fallback is the normal rendering rather than an error path.
  A photo with no comments — or one that does not exist — is an empty array,
  not a 404. `POST /photos/{uid}/comments` `{body}` → **201** with the created comment; guarded by
  **`RequireAuth`, not `RequireWrite` — the one deliberate exception to the read-only rule: a viewer may
  comment.** Commenting is social participation, not curation of the library (a viewer still cannot retitle a
  photo, move it between albums or name a face), and locking the read-only half of a family out of the
  conversation would defeat the feature. That route also carries the **per-user** rate limit
  `ratelimit.comment` (default 0.5/s, burst 10 → 429), mounted *inside* the auth guard so it keys on the
  caller rather than on a household's shared IP.
  `PATCH /photos/{uid}/comments/{commentUID}` `{body}` → 200 with the edited comment, **author only** —
  anyone else, admins included, gets 403: an admin may remove a comment but never rewrite what someone is
  recorded as having said. The edit stamps `edited_at` (null until the first edit).
  `DELETE /photos/{uid}/comments/{commentUID}` → 204 for **the author or an admin** (403 otherwise), the same
  moderation power admins already hold over the rest of the library. The delete is **soft**: the row keeps its
  place with `deleted_at` stamped and drops out of every read, so deleting twice is 404 rather than a silent
  success, and editing a deleted comment is 404 too. **Body:** plain text, whitespace-trimmed, **1–2000
  characters** (runes, not bytes; blank → 400, longer → 400, unknown JSON field → 400); nothing is parsed or
  rendered server-side — no HTML, no markdown — so **the client escapes what it displays**. A comment
  addressed through the wrong photo (`{uid}` is not the comment's) is **404, not 403**, so the endpoint cannot
  be used to probe which comment UIDs exist. `GET /photos/{uid}` carries **`comment_count`** (always present,
  0 when there are none) so the detail can badge the thread without fetching it; it resolves through the bulk
  `CountsAmong` with a single UID, so the count can never quietly become a per-item query. Every mutation
  writes an audit entry (`comment.create` / `comment.update` / `comment.delete`) **in the same transaction**
  as the change, targeting the **photo** with the comment's UID in `details.comment_uid`. Purging a photo
  removes its thread (FK `ON DELETE CASCADE`); deleting a user leaves their comments in place, authorless
  (`ON DELETE SET NULL`) and therefore editable by nobody. **MCP deliberately exposes no comment tool** (see
  [`MCP.md`](MCP.md)). Table `photo_comments` (migration `0052_photo_comments.sql`); 503 when no comments
  backend is wired.
- **Jobs API (`/api/v1`, `internal/jobsapi`, maintainer-only via `RequireMaintainer`):**
  `GET /jobs/stats` → `{by_state,by_type,total}`; `GET /jobs` → `{jobs,limit,offset}`
  (recent/dead-letter listing, query `state`/`limit`/`offset`, invalid → 400);
  `POST /jobs/{id}/requeue` → refreshed job (dead/failed → queued; 404 missing, 409
  non-requeueable);
  `POST /jobs/requeue-dead` → `{requeued}` — the **bulk** requeue, optionally narrowed by the query
  parameter `type` to one job type (an unknown type matches nothing and answers `0`, exactly like an
  already-empty dead letter). One `UPDATE` (`jobs.Store.RequeueAllDead`), because the case it exists for is
  a dead letter of thousands: the System page used to list the dead jobs and `POST` a requeue per row.
  The frontend polls (no SSE). Mounted by `server.WithAPI`
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
  `limit` 0 = as many as `candidates.max_candidates` allows; `DisallowUnknownFields` + 64 KiB, negative
  values → 400) →
  `{subject_uid,source_photo_count,source_face_count,exemplars_used,source_capped,capped,
  faces_without_embedding,min_match_count,threshold,
  reason?,counts:{create_marker,assign_person,already_done},candidates:[{photo,face_index,
  bbox:{relative:[x,y,w,h],pixel:[x,y,w,h]},distance,match_count,action,marker_uid?}]}`. The two caps
  behind `exemplars_used`/`source_capped` (only `candidates.max_exemplars` of a heavily tagged subject's
  faces seed the search) and `capped` (only `candidates.max_candidates` survivors are hydrated and returned,
  nearest first) are **memory bounds** — one request must not grow with the library — and are reported rather
  than applied silently; the `source_*` counts still describe the subject, not the sample. For a subject it finds
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
  default `expand.max_distance` = 0.20, i.e. 80 % similarity — re-derived for SigLIP 2, it was 0.30 under
  CLIP ViT-L-14 (see [`THRESHOLDS.md`](THRESHOLDS.md)); `limit` default `expand.limit`, cap
  `expand.max_limit`; non-numeric / negative → 400). Membership is resolved **natively** (`internal/organize`),
  **no call to any foreign system**. Response `{kind,collection_uid,source_photo_count,source_photos_sampled,
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
  tidying up the library — one question at a time, answer yes/no/skip. There are **five kinds of question**:
  `face` ("Is this Tomáš?"), `label` ("Should this photo have the label Ostatky?"), `place` ("Was this photo
  taken in Brno?" over a location the geo-estimator guessed), `duplicate` ("Is this the same photo?" over a
  near-duplicate pair, shown side by side) and `outlier` ("Is this really Tomáš?" over a face already assigned
  to him but sitting far from his centroid). The first two check guesses about things nobody had decided; the
  last three check guesses the machine already **acted on** — a coordinate written onto a photo, a pair the
  detector linked, an assignment somebody made — which no other page lists as questions. None of them destroys
  anything: **the duplicate check NEVER merges**, the outlier check detaches through the ordinary assign state
  machine (the marker survives) and the place check only moves a coordinate the estimator invented.
  The face and label questions are mixed from **two confidence tiers** (confidence = 1 − cosine distance):
  `review.sure_share` of a batch (default 0.70) from the **confident tier** (confidence ≥ `review.sure_min`,
  default 0.80 — the answer is almost always a one-click yes, and a yes is real work done), the rest from the
  **uncertainty band** (`review.band_min ≤ confidence < review.band_max`, default 0.45–0.75 — where a human
  answer teaches the system the most). Below `band_min` nothing is asked: the guess is noise. The ratio holds
  in **any prefix** of the queue, not merely on average, so a batch never opens with a run of hard questions —
  and the minority of hard questions is deliberate, since an all-easy game turns the player into a rubber
  stamp. Running out of one tier fills from the other, and a rebuild whose window came back empty rotates to
  the next one, so the queue only reports "nothing" for a genuinely empty library. `GET
  /review/queue?source=both|people|labels&limit=N` (source empty → `both`, unknown → 400; limit empty/0 →
  `review.round_size`, cap 100, non-numeric/negative → 400) → `{questions:[{id,kind:
  "face"|"label"|"place"|"duplicate"|"outlier",tier?:"sure"|"band",confidence,photo,subject?,face_index?,
  bbox?{relative,pixel},action?("create_marker"|"assign_person"),marker_uid?,label?,
  place?{name,country?,city?,place_name?,lat,lng},other?,group_id?,distance?}],
  round:{index,size,remaining,kinds{},sure,band,entities,last},breathers?:[{kind:"breather",photo,title,
  year?,reason:"favorite"|"rated"}],source,answered,remaining,reason?}`.
  **One request is one round** — the `questions` array *is* the round, so there are no boundary markers inside
  it — and `round` says which round of the session it is (`index`, from 1), how long it was minted (`size`),
  how much of it is still unanswered (`remaining`, = the array's length) and what it is made of
  (`kinds`/`sure`/`band`/`entities`, fixed at mint time so a **between-rounds summary** reports what the player
  just played rather than what is left of it). `last` says nothing is queued behind it.
  **Re-fetching before answering returns the same round** (the mix is a pure function of an unchanged pool), so
  a client that retries cannot lose questions; the round shrinks as it is answered and the next one is minted
  only once it is finished. `breathers` are **non-question cards** — a photo the caller rated ≥ 4 or
  favourited, with its title and year — carried *outside* `questions`, typed `"breather"` and carrying no id
  the answer endpoint would accept, so they can never be mistaken for a question. One per round, rotating
  through eras (one candidate per decade, newest first), and the field is **omitted entirely** when the library
  has nothing worth pausing on.
  `place` carries the guessed location and the most specific name it geocoded to (a photo with no cached place is
  **not asked about** — a pair of decimal degrees is not a place anybody can answer about); `other` is the second
  photo of a duplicate pair and `group_id` its group; `distance` is an outlier face's cosine distance from its
  person's centroid. `tier` is absent on the three new kinds — their confidences are not points on one comparable
  scale, so each carries its own ordering instead (duplicates surest-first, outliers **most suspicious** first,
  places by uid). `id` is
  **stable, derived from content** (`face:<photo>:<index>:<subject>` / `label:<photo>:<label>` /
  `place:<photo>` / `duplicate:<photo>:<other>`, the pair ordered smaller-uid-first so one pair is one question /
  `outlier:<photo>:<index>:<subject>`),
  `tier` says which tier the question came from (the UI asks the same question either way; it is there so the
  mix can be observed), `bbox` relative 0..1 **and** pixels (honouring EXIF orientation), and the whole thing is
  **deterministic** for a given library state. It is built in two steps.
  First the **pool** (`review.queue_size`, default 20 — material, not a response length; several rounds come out
  of one pool, so the vector searches run once per pool): within a tier the band by distance from its centre and
  the confident tier by confidence descending, tie-break id; then the tier blend; then all five kinds
  interleaved proportionally (each list's i-th question at the exact rational (2i+1)/(2·len), earliest position
  wins, ties by the fixed order in `Kinds`) and capped at `review.max_per_entity` (default 4) questions per
  entity.
  Then the **round** is mixed out of that pool (`internal/review/mixer.go`), one slot at a time: every unplaced
  question is scored against what the round already holds and the cheapest goes next. The penalties, in
  descending weight, are a question over `review.round_max_per_entity` (default 3) about one entity, a **third
  in a row** about one entity, a third and then a second in a row of one **kind**, the **tier** the running
  confident share does not want (so the configured mix holds *and* the tiers interleave instead of forming
  blocks), a photo from an **album** the previous photo was also in, one taken within **ten minutes** of it, and
  one from the same **decade**. Ties go to whichever question sits earlier in the pool's informativeness order,
  so variety is never bought with a less relevant question. Nothing is forbidden outright: a pool with one
  person in it, or ten photos from one wedding, still yields a **full round** — every candidate is merely
  expensive, and the cheapest expensive one wins. No `rand` anywhere; the only seeded choice is which kind the
  round tries to open with (seeded from the user and the round number), so two players do not get the same
  opening. **`source` decides what the game asks about** — only faces
  (`people`), only labels (`labels`) or both interleaved (`both`, the default) — and it is applied **inside the
  rebuild**, not as a filter on its result: a labels-only queue never runs the subject sweep at all (the scans
  are the whole cost of a rebuild, and a subject sweep hydrates a full photo record per match). **The three new
  kinds ride with `both` only** — "people" and "labels" are promises about what the game will ask, and a place
  question would break either one; the toggle deliberately stays three buttons wide rather than six. The applied
  source is echoed back in `source`, so a client can recognise a batch that arrived after the player switched.
  A label whose `review_enabled` is false is **not asked about and not even searched** (see Labels below).
  The queue is **cached per user *and per source*** (`review.cache_ttl`, default 60 s) — a batch fetch does not
  recompute the expensive vector searches, but a **changed source always rebuilds** (a warm cache serving the
  previous selection would look exactly like a broken toggle); skips/answers are session-wide, so they hold
  across a switch. `remaining`/`answered` are cheap session counters.
  An empty library (no named people or labels) → a **non-error** empty queue with `reason:
  "no_people_no_labels"`; the chosen source itself is empty → `reason:"no_people"` / `"no_labels"` (only for a
  restricted source; the untouched source is never counted); sources exist, but the band is empty →
  `reason:"no_candidates"`.
  `POST /review/answer` with `{question_id,answer:"yes"|"no"|"skip"}` → `{result,answered,remaining,
  reveal?{subject_uid,name,photo_count,oldest_year?,newest_year?}}`
  (`result` ∈ assigned / labeled / confirmed / cleared / detached / rejected / skipped / already_answered / gone).
  `reveal` is present **only** on `result:"assigned"` — the payoff of a confirmed face: how many visible photos
  that person is on now (counted per photo, not per marker, so it matches their gallery) and the years their
  dated photos span. It is one indexed read (`people.Store.SubjectStats`) taken **after** the write, so the
  numbers include it, and any failure simply omits the field rather than failing an answer that already
  succeeded.
  Every verdict routes through a write path that already exists elsewhere; the package opens none of its own:
  **face** yes → the **existing** assign state machine (the same path as `POST /photos/{uid}/faces/assign`; the
  action is derived from the face's current state — a marker exists → `assign_person`, otherwise `create_marker`
  with the stored bbox), no → a face rejection; **label** yes → `AttachLabelAudited` (source `manual`), no → a
  label rejection; **place** yes → the coordinates stay and `location_source` is promoted `estimate` → `manual`,
  no → the coordinates are cleared and `manual` is stamped as the **tombstone** that stops the nightly backfill
  handing the same guess back (both through `photos.UpdateMetadataAudited`, read-modify-write against the live
  row, so a photo edited meanwhile is not clobbered and one whose location is no longer an estimate is left
  alone); **duplicate** yes → a `duplicate_confirmations` row (which ranks the group first on `/duplicates`),
  no → the existing dismissal that drops the edge from every later scan — **neither merges**; **outlier** yes →
  a `face_confirmations` row (the same set `/outliers` excludes, so the two views agree), no → `unassign_person`
  through the assign state machine, re-reading the marker so a face detached meanwhile resolves to `gone`.
  A rejection is **permanent** (the question never comes back and the negative-exemplar rule kills similar
  candidates); **skip** = "don't know" — the question is not
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
  game, sourced from durable audit rows with `details.via = "review"`. Which actions those are is
  **`audit.ReviewYesActions()` / `ReviewNoActions()`** — one shared list, so a new question type becomes
  countable (here and in the admin decision view, `GET /audit?via=review&decision=yes|no`) the moment its write
  path is wired: **yes** = `face.assign` + `label.attach` + `face.confirm` + `location.confirm` +
  `duplicate.confirm`, **no** = `face.reject` + `label.reject` + `face.unassign` + `location.reject` +
  `duplicate.dismiss`; **skip** records nothing, so on principle it does not count. Because
  of this, a review face confirmation (`face.assign`) and an outlier detach (`face.unassign`) carry `via:review`
  (they go through facematch `Service.Apply`, which assembles the audit itself; ordinary assignments and
  detaches stay unmarked, so the same action performed on a curation page is never counted). The response `{window,caller_uid,entries:[{user_uid,display_name,yes_count,no_count,total,
  streak_days,is_me}]}` is sorted (total desc → yes desc → display_name), only users with ≥1 decision in the
  window (zero = absent), a NULL actor (a deleted user) is omitted, `is_me`/`caller_uid` mark one's own
  row. The windows are computed from `created_at` (7 d = a sliding 7×24 h, today = midnight of the day).
  **`streak_days`** is the player's *current* run of consecutive days with at least one review decision, ending
  today **or yesterday** (the day is not over, so an unfinished today must not break a run); 0 when no run is
  alive. It is **not** narrowed by `window` — a streak is a fact about the habit, not about the slice being
  shown — and needs **no new table**: it is a second query over the same `via:review` audit rows, reduced in
  SQL to the distinct *hours* each user was active in over the last 400 days and folded into local days in Go
  (`internal/review/streak.go`), where the day arithmetic is testable. The hour reduction is exact wherever the
  zone's offset from UTC is a whole number of hours. Served by
  `review.LeaderboardStore` over the shared pool; the partial index `idx_audit_log_review_actor`
  (migration `0037`) keeps both scans cheap.
- **People/Subjects API (`/api/v1`, `internal/peopleapi`):** `GET /subjects` (RequireAuth) →
  `{subjects:[{...subject, marker_count, photo_count, cover_face?}]}` (ordered by name). Both counts
  cover only non-invalid markers on visible photos and they are **not interchangeable**:
  `marker_count` counts markers (the figure the face tools want), `photo_count` counts **distinct
  photos** and is smaller whenever one photo carries several of the person's faces (on production the
  catch-all subject had 16 531 markers on 6 767 photos). A "N photos" label must use `photo_count` —
  it is the length of the list `GET /subjects/{uid}/photos` pages through.
  `cover_face` = `{photo_uid,x,y,w,h,width,height,orientation}` — the face by which the
  subject is illustrated in the people grid when it **has no** `cover_photo_uid`; absent when the subject has no
  usable marker. Picked by `listSubjectsSQL` (the largest box, then `score`, then `uid`; only
  `type='face'`, non-invalid, on a visible photo). `width`/`height`/`orientation` are the stored frame of the
  photo — the client crops the cutout itself from the thumbnail cache (there is no face-thumbnail endpoint) and
  without the frame would distort it. **An explicit `cover_photo_uid` always wins**, `cover_face` is only a
  fallback;
  `POST /subjects` (RequireWrite) → 201 creates a subject from `{name,type,favorite,private,notes,
  cover_photo_uid?,birth_year?,death_year?}` (empty name / unknown type → 400; a name that identifies **nobody** — punctuation or
  symbols alone, no letter and no digit — is also 400 `subject name must contain a letter or a digit`: it has
  no slug of its own, would be stored under the shared fallback slug, read as unnamed everywhere and act as a
  magnet for find-or-create-by-name lookups, which is exactly the catch-all `docs/OPERATIONS.md` §
  `maintenance nameless-subjects` describes); `GET /subjects/{uid}` (RequireAuth) →
  the subject (404); `PATCH /subjects/{uid}` (RequireWrite) → editing the same fields (404/400);
  **`birth_year`/`death_year` are the person's life span** — plain years, `null` (or omitted) for the usual
  "nobody recorded it". Like every other field of this body they are **rewritten**, not patched: a `null`
  **clears** a stored year, so a caller that changes only the cover must send the years it read back or it
  erases them. Validation (`people.validateLifeYears`, 400 `people: invalid birth or death year`):
  each known year within **1800…the current year** — a year in the future is a typo, not a fact — and
  `death_year >= birth_year` when both are set. Nothing ties them to `type='person'`, though only the UI
  for a person offers them: the type is editable, and refusing a save because somebody was reclassified
  would punish the reclassification. The `>= 1800` and `death >= birth` halves are also SQL CHECKs
  (migration `0051`); the "not in the future" half is Go-only, because a CHECK may not read the clock.
  Both appear in the `subject.update` audit entry's `changes` diff like any other edited field;
  `DELETE /subjects/{uid}` (RequireWrite) → 204 (the markers are detached server-side);
  `POST /subjects/{uid}/merge` (RequireWrite) `{keeper_uid}` → `{keeper_uid,source_uid,markers_moved,
  faces_moved,confirmations_moved,rejections_moved,rejections_dropped,dismissals_moved,shared_photos}` —
  **the path subject is merged into the keeper and deleted**, in one transaction, with one `subject.merge`
  audit entry naming both (the source's name survives nowhere else: a merge cannot be undone). Everything
  the source carried moves — markers, the faces cache, confirmations, rejections, repeated-marker
  dismissals — and the keeper's *empty* fields are filled from it (`favorite`/`private` OR-ed, `notes` and
  `cover_photo_uid` only when it has none). `birth_year`/`death_year` travel as a **pair**, and only onto a
  keeper carrying **neither**: filling them one at a time could pair one person's birth with another's
  death, which is both a lie and a `death >= birth` CHECK violation — and a failed constraint would take
  the whole merge with it. Three rules cover the disagreements, all in
  [`PACKAGES.md`](PACKAGES.md) § `internal/people`: **markers are never deduplicated** (a photo carrying
  both people keeps both markers and becomes a repeated-marker group `GET /duplicate-markers` surfaces —
  `shared_photos` counts them), **a positive record beats a rejection** (a rejection contradicting an
  assignment or a confirmation is dropped, from whichever side — `rejections_dropped` counts them), and a
  dismissal is **not** carried onto a photo the merge itself turns into a group. `keeper_uid` missing → 400,
  either subject unknown → 404, a subject merged into itself → 400. Like a rename or a delete, a merge does
  **not** rewrite the affected photos' metadata sidecars; `POST /process/sidecars?all=true` does.
  Not exposed over MCP. `GET
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
  `POST /process/thumbnails` → `{enqueued,pending,dry_run}` (backfill `thumbnail` for photos **without a
  generated thumbnail** via `thumbjob.BackfillThumbnails`; "missing thumbnail" = a photo without a perceptual
  hash, which the `thumbnail` job computes together with the thumbnail). Optional `?all=true` schedules **every
  non-archived photo** (a forced full re-run — it also catches up a missing thumbnail size on a photo that
  already has the hash; the job skips sizes already in cache — and, on a publishing backend, sizes already in
  the bucket — so the run is cheap and never changes the original). Optional **`?dry_run=true`** schedules
  nothing and answers only `pending`, the number of photos the same call would cover
  (`thumbjob.CountBackfillThumbnails`): a thumbnail job re-reads an original, and "the narrow predicate" is no
  promise of a small run, so the cost is reportable before it is paid. A real run answers `pending` too, so the
  size of what was just started is visible in the response.
  `POST /process/metadata` → `{enqueued}` (backfill `metadata` for photos whose **file has never
  been read** into the IPTC/XMP and file-technical columns, via `metajob.BackfillMetadata`; "unread"
  = `photos.metadata_extracted_at IS NULL`, which are rows from the one-off imports
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
  `POST /process/ocr` → `{enqueued}` (backfill `ocr` for photos the text recogniser has **never seen**, via
  `ocrjob.BackfillOCR`; "never seen" = `photos.ocr_at IS NULL`, restricted to non-archived photos that are not
  videos — OCR runs on stills only, there is no poster-frame recognition). Optional `?all=true` schedules
  **every non-archived still** (a forced full re-run — how the library picks up a better recognition model).
  The endpoint only **enqueues**; the worker calls the sidecar's `POST /ocr/image` over each photo's `fit_1920`
  preview and writes `ocr_text`/`ocr_model`/`ocr_at`. An empty reading is a **success** that is still recorded,
  so a photograph with no writing in it stops being a candidate instead of returning on every run. The run is
  idempotent (the queue dedupes per photo) and resumable. It needs the embeddings box, which is usually
  offline: the jobs then wait in the queue — enqueueing never blocks on it. **503** when
  `embedding.ocr.enabled: false`.
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
  (RequireAuth) → `{albums:[{...album, photo_count, cover_uid?, cover_uids?, taken_from?, taken_to?}]}`
  (`organize.AlbumSummary`): `cover_uid` is the **effective cover** — a manually chosen
  `cover_photo_uid`, otherwise the **newest live photo of the album** (deterministically: `taken_at DESC NULLS
  LAST, uid`); `cover_uids` is that same photo **and the ones behind it** in the same order,
  `organize.CoverCandidates` (8) at most and **never the hand-picked cover**, because one cover per album is
  not enough to tell albums apart — overlapping albums share their newest photo, so the client draws a
  2 × 2 collage or steps to a photo a neighbouring tile has not used (`web/src/lib/albumCovers.ts`).
  It is a slice of the array the cover already aggregates, so it costs nothing extra;
  `taken_from`/`taken_to` is the **`taken_at` range** across the album's photos. All are aggregated
  by a single SQL query (one LEFT JOIN + aggregates over it, no migration, no per-album lookup — see
  `docs/PERF.md` § "The album index") and count **only live photos** —
  an archived photo is not counted into `photo_count`, supplies no cover and does not move the range. Absent
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
  (RequireAuth) → `{labels:[{...label, photo_count, cover_uid?}]}` (ordered by priority DESC), where `cover_uid`
  is the photo standing for the label — its newest visible one, derived exactly as the global-search hits' cover
  is and in **one batched query** for the whole listing (`organize.Store.LabelCovers`), absent for a label on no
  visible photo; a label has no cover to pick by hand. `POST /labels`
  (RequireWrite) → 201 from `{name,priority?}` (empty name → 400); `GET /labels/{uid}`
  (RequireAuth, 404); `PATCH /labels/{uid}` (RequireWrite, name/priority/review_enabled); `DELETE /labels/{uid}`
  (RequireWrite → 204); attaching `POST /labels/{uid}/photos` `{photo_uid,source?,uncertainty?}`
  → 204 (invalid source → 400), `DELETE /labels/{uid}/photos` `{photo_uid}` → 204.
  Every label payload carries **`review_enabled`** — whether the review game may ask about it. It is `true` for
  every created label (`POST` ignores the field) and is switched on the labels page via `PATCH`; **omitting it
  from a PATCH body keeps the stored value**, so the rename form and the toggle can each send only what they
  know without clobbering the other's field. Switching it off means the label produces no questions *and is not
  scanned* — a label search is a per-member kNN fan-out, so it costs a queue rebuild nothing either. It is a
  statement about the label as a whole; "not this photo for this label" remains a per-photo rejection under
  `/feedback/label-rejections`. Subjects have no equivalent flag. **An album's/label's
  photo gallery** runs via the shared `GET /photos?album={uid}`/`?label={uid}` (the same shape +
  filters/pagination; an album scope is always chronological and takes only the direction from `sort`,
  a label honours the chosen order). A viewer reads, but does not mutate (403).
  Every mutation (create/update/delete of an album or label, add/remove of photos, attach/detach) writes an audit entry
  (`album.*`/`label.*`) **in the same transaction** as the change — the responses do not change. Mounted by another `server.WithAPI`
  (`buildOrganizeAPI` in `cmd/kukatko/organize.go`).
- **Feedback / Rejections API (`/api/v1`, `internal/feedbackapi`):** persisted feedback —
  a user's "no" (and now also "yes") to a face↔subject or photo↔label estimate, and its undo.
  **Feedback is an opinion — it never mutates** the underlying data (does not detach a marker, remove a label,
  archive anything). Ten endpoints, all **RequireWrite** (editor/admin, viewer 403): `POST /feedback/face-rejections`
  `{photo_uid,face_index,subject_uid}` → 204 (rejects "this face is NOT this person"),
  `DELETE /feedback/face-rejections` (same body) → 204 (undo); `POST /feedback/label-rejections`
  `{photo_uid,label_uid}` → 204 (rejects "this photo should NOT have this label"),
  `DELETE /feedback/label-rejections` (same body) → 204 (undo) — DELETE too carries a body (like a
  label-detach); `POST /feedback/face-confirmations` `{photo_uid,face_index,subject_uid}` → 204
  and `DELETE /feedback/face-confirmations` (same body) → 204;
  `POST /feedback/duplicate-dismissals` `{photo_uid,other_uid}` → 204 ("these two photos are NOT
  duplicates") and `DELETE /feedback/duplicate-dismissals` (same body) → 204 (undo);
  `POST /feedback/duplicate-confirmations` (same body) → 204 ("yes, this really IS the same photo twice")
  and `DELETE /feedback/duplicate-confirmations` (same body) → 204 (undo) — the positive mirror of the
  dismissal, written by the review game's duplicate check. It **merges nothing**: agreeing that two files are
  one shot and deciding which copy survives are different acts, and only the second destroys anything. What it
  buys is ranking — `GET /duplicates` marks a group holding a confirmed pair `confirmed:true` and sorts it
  **first**, above the machine's own suspicions, as the one where merging is a decision already made
  (`duplicate_confirmations`, migration `0054`, ordered-pair CHECK with `COLLATE "C"` from the start);
  `POST /feedback/duplicate-marker-dismissals` `{photo_uid,subject_uid}` → 204 ("this person really IS
  marked more than once on this photo" — a double exposure, a mirror, a photo of a photo) and
  `DELETE /feedback/duplicate-marker-dismissals` (same body) → 204 (undo). The latter keys the **(photo,
  subject) pair**, which is exactly the group `GET /duplicate-markers` shows — never the markers, whose uids
  change when a photo is re-detected and would resurrect a settled group; it detaches and invalidates nothing.
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
  `label.unreject`/`face.confirm`/`face.unconfirm`/`duplicate_marker.dismiss`/`duplicate_marker.undismiss`;
  actor = `rejected_by`/`confirmed_by`/`dismissed_by`).
  Mounted by another `server.WithAPI` (`buildFeedbackAPI` in
  `cmd/kukatko/feedback.go`). The consumers (find a person among untagged ones, recognition sweep, the review game)
  come in later tasks.
- **Places API (`/api/v1`, `internal/placesapi`, authenticated via `RequireAuth`):** browsing the
  reverse-geocoded place hierarchy + scoping the photo listing to a locality. `GET /places` →
  `{places:[{country, count, cover_uid, cities:[{city, count, cover_uid}]}]}` — counts aggregated over
  **non-archived**
  photos with place data; a country's `count` also includes photos without a known city (may exceed the sum of
  the cities), `cities` is always an array; ordered **count desc, then name** (for both countries and cities); photos
  without place data (no `photo_places` row or an empty `country` — a no-GPS "processed" marker) are excluded.
  `cover_uid` is the place's **newest visible photo** — the thumbnail the browse list draws (Places is a way
  through a photo library, not a table of names); a country takes the newest across all of its cities, the
  unknown-city group included. It is the same `array_agg(… ORDER BY taken_at DESC NULLS LAST, uid)[1]`
  subscript the album index uses — one more aggregate over the pass the count already makes, never a
  correlated `ORDER BY … LIMIT 1` (see `docs/PERF.md` § "The album index"). The key is **omitted** when
  there is nothing to draw.
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
- **Search History API (`/api/v1`, `internal/searchhistoryapi` + `internal/searchhistory`, authenticated via
  `RequireAuth`):** each user's **recent searches** — the short ordered list the search box offers back (whole
  while it is empty, as prefix matches once something is typed) and the command palette offers while empty. It
  lives server-side, not in the browser, so a query composed on a laptop is offered on the phone. Only a query
  the reader **submitted** is posted — Enter, picking a recent one, running one from the palette — never one a
  typing pause merely ran, or the capped ring would fill with prefixes of itself (`useRecordSearch` in
  `web/src/hooks/useSearchHistory.ts`). `GET /search-history` → `{searches:[{query,searched_at}]}`, most recent
  first, at most **20** (`searchhistory.MaxEntries`); an empty history is `[]`, never `null`, so a client always
  parses one shape. `POST /search-history` `{query}` → **204** with no body (the caller already knows what it
  searched for; the refreshed list is only wanted when the dropdown next opens, which is a GET). `DELETE
  /search-history` → 204, **idempotent** (clearing an empty history succeeds). A blank/whitespace-only query
  → 400; the body is `DisallowUnknownFields` + an 8 KiB limit. Recording is an **upsert**: re-running a query
  moves it to the front instead of appending a duplicate, and each write prunes the history back to 20 in the
  same transaction, so there is no retention job. Queries are stored verbatim apart from trimming and a
  **500-character** cap (`searchhistory.Normalize`, which truncates rather than rejecting); deduplication is on
  that exact trimmed text, so `Praha` and `praha` are two entries. **Every operation is scoped to the
  authenticated user** — there is no path parameter and no owner in any body, so a caller can only ever read,
  extend or clear their own history, and one user's queries can never appear in another's list. Readable and
  writable by **every role, viewers included** (searching is not curation), and deliberately **not audited**:
  a private convenience list is not a library mutation. The `search_history` table (migration
  `0056_search_history.sql`). Mounted by `server.WithAPI` (`buildSearchHistoryAPI` in
  `cmd/kukatko/searchhistory.go`).
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
- **Instance settings API (`/api/v1`, `internal/settingsapi` + `internal/settings`, three audiences):**
  the three instance-wide values an **administrator** changes at runtime without a redeploy — whether
  self-service registration is open, the shared secret registration asks for, and the Markdown greeting
  shown on a first sign-in. The registration secret is stored **readable, not hashed** (an administrator has
  to read it back to tell people what it is), which is why the responses are three separate payloads rather
  than one record filtered per role: `GET /settings/public` **unauthenticated** → `{registration_enabled}`
  and nothing else (the sign-in screen has to know before anybody is signed in); `GET /settings/welcome`
  behind `RequireAuth` → `{welcome_markdown}` and nothing else; `GET /settings` behind `RequireAdmin` →
  `{registration_enabled, registration_secret, welcome_markdown, updated_at, updated_by_uid?}` — the only
  place the secret appears. `PUT /settings` behind `RequireAdmin` **replaces all three values** → 200 with
  the stored record; a field left out of the body is written as its zero value. **Enabling registration
  while the secret is blank (or whitespace-only) → 400** (`settings: registration secret must not be empty…`)
  — an open door with no lock is never what the administrator meant, and the two are saved together so the
  combination can be refused. The secret is stored trimmed, the welcome Markdown verbatim. Body
  `DisallowUnknownFields` + a 64 KiB limit, `updated_at` is RFC3339. **The update is audited**
  (`settings.update`) in the same transaction as the change; the details record the flag and *whether* a
  secret and a greeting are set — **never the secret itself**. The single-row `instance_settings` table
  (migration `0062_instance_settings.sql`, seeded so every read finds a row). Mounted by `server.WithAPI`
  (`buildSettingsAPI` in `cmd/kukatko/settings.go`).
- **What's New API (`/api/v1`, `internal/whatsnewapi` + `internal/whatsnew`, authenticated via
  `RequireAuth`):** the digest behind the **"what's new since your last visit"** panel on the library home.
  `GET /whats-new` → `200 {has_news, since?, photos, mine_photos, comments, albums:[{uid,title}], album_count,
  people:[{uid,name}], person_count}`. **`has_news` is the only flag the client branches on** — it is false
  (and everything else absent or zero) for a **first-ever visit** and for a visit that found nothing, and in
  both cases no panel is shown; a vanished account is reported the same way, so the endpoint returns 200 in
  every non-failure case (never 404/204, one shape to parse). A store failure → 500.
  **Readable by every role, viewers included.** `since` is RFC3339 and stays **constant for the whole
  visit**, which is what makes it a stable key for the client-side dismissal.
  **This GET writes:** reading the digest stamps the caller's visit (`users.last_seen_at`, and
  `users.visit_reference_at` when a new visit begins — see migration `0053_user_visits.sql` and
  `internal/whatsnew` in `docs/PACKAGES.md` for the two-timestamp mechanism and the **6 h** inactivity
  threshold). It must therefore be issued **once per library-home load and never polled**. Counts mirror the
  library grid (live, non-hidden, stack-primary photos; live comments; hand-curated albums; named subjects
  only) and `albums`/`people` are capped at 6 links while `album_count`/`person_count` report the true
  totals. **`mine_photos`** counts how many of those new photos the reader themselves is on, via the
  subject their account is linked to (`users.subject_uid`, read in the same statement that stamps the
  visit) and a non-invalid marker. It is **0** both for an unlinked account and for a visit where none
  of the new photographs was of them, and the client shows no line for it either way; being a subset of
  `photos` it can never be the only news, so it does not affect `has_news`. Mounted by `server.WithAPI` (`buildWhatsNewAPI` in `cmd/kukatko/whatsnew.go`).
- **Global Search API (`/api/v1`, `internal/globalsearchapi`, authenticated via `RequireAuth`):**
  grouped **cross-entity search** for the navbar quick-results and the search page. `GET /search/global?q=` →
  `{query, albums:[{uid,title,cover?,thumb_url?,photo_count}], labels:[{uid,name,cover?,thumb_url?,photo_count}],
  people:[{uid,name,cover?,thumb_url?}], photos:[…usual photo shape…]}` — albums/labels/people matched by
  name/description **accent- and case-insensitive** (`immutable_unaccent` + ILIKE via the store methods
  `SearchAlbums`/`SearchLabels`/`SearchSubjects`), photos via the **existing full text** (`photos.Store.
  Search` over the `fts` tsvector). Each group is capped at a small top-N (default 8, `Config.Limit`), the arrays
  are always non-nil. An empty/whitespace `q` → 400, a store error → 500. The existing `GET /search` (per-user
  photo fulltext/semantic/hybrid) stays unchanged. Mounted by `server.WithAPI` (`buildGlobalSearchAPI`
  in `cmd/kukatko/globalsearch.go`, sharing the organize/people/photos store).
  **Every entity hit carries its picture**, `cover` (the photo standing for it) and `thumb_url` (where to fetch
  that photo's medallion, `thumb.AvatarSize` = `tile_100`, stamped through `internal/mediaurl` exactly like a
  photo hit's — so a published bucket yields a signed edge URL). The two are set **together or not at all**, and
  an entity with nothing to show carries neither, which is the client's cue to draw its own glyph. An album uses
  its hand-picked cover and otherwise its newest visible photo; a **label and a person get a derived** one — the
  newest photo carrying that label / showing that person (`COALESCE(taken_at, created_at)` descending, uid
  breaking ties), never an archived, hidden or non-primary-stack photo. Each group's covers are resolved in **one
  query for the whole group** (`organize.Store.AlbumCovers`/`LabelCovers`, `people.Store.SubjectCovers`), so the
  previews cost three queries whatever the top-N holds — never one per row.
  **A `q` that carries a UID takes a different branch:** the id is resolved against the one table its prefix
  names and returned as `direct`, and the four-way fuzzy fan-out is **skipped** (the groups come back as `[]`) —
  a uid matches no title, name or full text anyway, so this replaces the fan-out rather than adding a fifth query.
  Recognised prefixes (`query.ClassifyUID`, 26 characters unless noted): `ph` photo, `al` album, `lb` label,
  `su` subject, `st` stack, `mk` marker, and `pt` = an **imported** photo uid (16 characters). An id with an
  unknown prefix is **not** probed against every table. The id may be the whole `q` or one word of it.
  `direct` = `{uid, kind, found, target_kind?, target_uid?, title?, photo?, cover?, thumb_url?, states?}`, the
  cover pair being the same one the fuzzy groups carry (a pasted id draws the picture a typed name would):
  `kind` is what the
  id itself names, `target_kind`/`target_uid` what to open — a `mk…` resolves to the photo it sits on, an `st…`
  to the stack's primary, a `pt…` through `photos.photoprism_uid` and then `photoprism_aliases`. A well-formed id
  that matches nothing comes back with `found: false` rather than an empty result set. The lookups are **unscoped**
  — an archived, hidden, private or non-primary-stack-member photo resolves, and `states` (`archived`, `hidden`,
  `private`, `stack_member`) says which, so a hit outside the library view is labelled instead of confusing.
- **Bulk metadata API (`/api/v1`, `internal/bulkapi`, editor/admin via `RequireWrite`):**
  `POST /photos/bulk` `{photo_uids:[…], operations:{…}}` applies a set of operations to many photos
  **in a single transaction** with an audit-log entry. Operations (each optional): `add_to_albums`/
  `remove_from_albums`, `add_labels`/`remove_labels`, `set_caption`/`clear_caption` (→title),
  `set_description`/`clear_description`, `set_taken_at {precision,value}`/`clear_taken_at` (see below),
  `set_location {lat,lng}`/`clear_location`,
  `archive`/`unarchive`, `hide`/`unhide` (library visibility, see above),
  `set_favorite` (**per-user**), `set_rating` (0–5) / `set_flag`
  (none/pick/reject/eye) (**per-user**, invalid value → 400). Response `{results:[{photo_uid,status,
  error?}],counts:{total,updated,skipped,errored}}` (200 even on partial errors): `updated`/
  `skipped` (duplicate uid)/`error` (the photo does not exist — it **does not abort valid** ones); only a DB error
  rolls back the whole batch (500). A set/clear, archive/unarchive or hide/unhide conflict, an unknown operation,
  a missing album/label in an add → **400**; a batch above `bulk.max_batch_size` (default 1000) → **413**.
  `set_taken_at` re-dates a whole selection at once — the repair for a shelf of scans carrying the day the
  scanner was switched on. The value's shape follows the precision, so the two cannot disagree:
  `{"precision":"day","value":"1974-06-14"}`, `"month"`/`1974-06`, `"year"`/`1974`,
  `"decade"`/`1970` (a year mid-decade is rounded down). There is **no time of day**. It stores the
  **first instant of the stated period in UTC** into `taken_at` — so the photos sort and filter into that
  period everywhere (timeline, period filter, year facets, `year:`) — plus `taken_at_source = manual`,
  `taken_at_precision` and, for a grain coarser than a day, `taken_at_estimated = true`; an exact date
  instead lowers that flag and clears `taken_at_note` with it. A precision outside the four, or a value that
  does not parse at its precision, → **400**. It never touches the original file or its EXIF; the caller's
  sidecar rewrite carries the change to storage.
  `clear_taken_at` (bool) is the opposite statement — **the date is unknown** — and the bulk twin of a
  `PATCH` with `taken_at: null`: it wipes `taken_at`, stamps `taken_at_source = unknown`, resets
  `taken_at_precision` to `day` and **moves the outgoing date into `taken_at_before_unknown`** so the
  declaration can be undone (see the `PATCH` above and migration `0066`). `taken_at_estimated` and
  `taken_at_note` are left alone: "the date is unknown, but grandma says it was a wedding" is a state worth
  keeping. Sending it together with `set_taken_at` → **400**: one states a date, the other states that
  nobody knows it. The cleared photos are then reachable as `dated:no`.
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
  the client draws them in a **different shape** (dashed, not just a different colour) + a `title`. The collection
  also carries the foreign member `coverage: {located, total}` (RFC 7946 §6.1 permits foreign members):
  `located` = markers actually drawn, `total` = photos matching **the same filters** with the has-GPS
  restriction lifted (one extra `photos.Store.Count`, paging stripped). It is computed server-side because
  only the server knows the exact filter set; the map states it in words ("Na mapě je 2 378 z 20 906 fotek
  — u ostatních není uložená poloha"), since a map showing 11 % of a library and saying nothing reads as
  broken rather than sparse. mapy.com errors
  (**401/403 → 424** `mapsapi.StatusMapKeyRejected` = *our* key rejected, a raw 403 does not
  leak out — the caller's request is fine; 404→404, 429→429, 5xx→502/503)
  **do not leak the key**; every result is written into `mapy.Health` (→ the `GET /system/status`
  section `maps`). Without `maps.mapy_api_key`, tile/rgeocode/geocode return 503 (the location editor shows this
  as „vyhledávání míst není dostupné“ and continues on coordinates and a click into the map), GeoJSON
  works. Mounted by `server.WithAPI` (`buildMapsAPI` in `cmd/kukatko/maps.go`).
- **Import API (`/api/v1`, `internal/importapi`, maintainer-only via `RequireMaintainer`):** the
  **read-only** bookkeeping of imports — there is nothing here to trigger. `GET /import/runs` →
  `{runs,limit,offset}` — a page of `import_runs` newest-started-first (query `limit`≤200/`offset`,
  invalid → 400). The only source still written is **`folder`** (`kukatko import dir`,
  `internal/dirimport`), which reads a directory on the server's disk and is therefore started **only
  from the CLI**. The three legacy sources in the history — `photoprism`, `photosorter`, `photosorter_feeds` —
  are the migration that closed in August 2026; its importers were removed and its runs stay as the
  catalogue's provenance record, so every reader must keep decoding them.
  A run's `counts` object is `{imported,updated,skipped,deduplicated,failed}`: **`deduplicated`** counts SOURCE
  photos whose content was already catalogued under a different source photo (they collapse onto that row and
  an alias keeps their uid resolvable — see `photoprism_aliases`), and runs recorded before the bucket existed
  simply have no key for it.
  A run can also carry the **`partial`** status: it finished its scan but recorded ≥1 unresolved
  per-photo/per-file failure (see `import_failures`), so it is deliberately not reported as a clean `done`.
  `GET /import/failures` → `{failures,limit,offset}` — a page of `import_failures` newest-recorded-first
  (`failure = {id,run_id,source,stage,photo_uid,source_ref,detail,error,created_at,resolved_at}`;
  `stage` ∈ `photo|file|marker|album_member|label|thumbnail|embedding|faces|phash|edit|metadata`), with the
  filters `?source=`/`?run_id=`/`?unresolved=true` and paging `?limit=`(≤200)/`?offset=` (invalid → 400;
  an unknown source → 400).
  **Removed with the one-off importers:** `POST /import/photoprism`, `POST /import/photosorter`,
  `POST /import/photosorter-feeds` and `GET /import/verify` (the completeness reconciliation) are gone —
  they now 404. Mounted by `buildImportAPI` in `cmd/kukatko/import.go`. The frontend (`ImportPage`) polls
  `GET /import/runs` + `GET /jobs/stats` + `GET /import/failures`.
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
- **Audit API (`/api/v1`, `internal/auditapi`):** a read-only listing of
  the durable audit trail — the whole trail for an admin (`GET /audit`, `RequireAdmin`), one's own actions for
  anybody signed in (`GET /audit/mine`, `RequireAuth`). `GET /audit` → `{entries,total,limit,offset,next_offset}` (entry =
  `{id,actor_uid,action,target_type,target_uid,details,ip,user_agent,created_at}`, newest-first;
  for edit actions `details.changes` = `{"<field>":{"old":…,"new":…}}` with only the changed fields — see
  the `internal/audit` convention; a bulk edit `photos.bulk` does not have it)
  with the filters `?user=`/`?entity_type=`/`?entity_uid=`/`?action=`/`?since=`/`?until=` (RFC3339) and
  pagination `?limit=`(≤500)/`?offset=`; an invalid time/number → 400. In addition, **filters for the admin
  overview of one user's decisions in the review game**: `?via=review` (only review decisions —
  `details.via='review'`, i.e. the actions `face.assign`/`label.attach`/`face.reject`/`label.reject`;
  the literal matches the partial index from migration 0037) and `?decision=yes|no` (the Yes bucket = assign+attach /
  No = reject); another `via`/`decision` value → 400.
  **`GET /audit/mine`** (`RequireAuth`, so viewer and up) answers the same body with the same filters and paging,
  except the actor is **taken from the session** and overwrites whatever the query asked for, on every page —
  a non-admin therefore never reads somebody else's rows, and never the **system's** either (an entry with an
  empty `actor_uid` matches no actor filter). `?user=` naming **somebody else → 403**, not a silent narrowing:
  quietly rewriting the request would leave the caller believing they see something they do not; `?user=` naming
  **oneself** is accepted and changes nothing. It is a route of its own rather than a looser guard on `/audit`
  precisely so the narrowing is a property of the route's shape, not of a branch a later edit could weaken.
  The records are served **whole, `ip` and `user_agent` included** — that is the caller's own address and
  browser, and seeing it is how a user recognises (or disowns) an action. For an admin nothing changes:
  `GET /audit` still lists every actor including the system's, and `?user=` still selects anybody.
  Audit entries are **not added
  over HTTP** — they arise inside mutation transactions (in-tx `audit.Write`, see the `internal/audit`
  convention); the only HTTP mutation of the trail is the maintainer-only **retention purge**
  (`POST /maintenance/audit/purge`, see Maintenance API), which deletes old entries and audits itself.
  Mounted always (`buildAuditAPI` in `cmd/kukatko/audit.go`).
- **Maintenance API (`/api/v1`, `internal/maintenanceapi`, maintainer-only via `RequireMaintainer`):**
  the library's integrity check & repairs. `GET /maintenance/scan` → `Report` (counts + samples:
  `missing_originals`/`orphan_files`/`missing_thumbnails`/`missing_embeddings`/`missing_faces`/
  `missing_phashes`/`transposed_dimensions`/`transposed_face_boxes`/`duplicate_face_markers`/
  `sideways_face_detections` + the totals
  `photos`/`files_in_db`/`originals_on_disk`);
  `POST /maintenance/repair`
  `{thumbnails,embeddings,faces,phashes,import_orphans,dimensions,face_markers,sideways_faces}` (each opt-in)
  → `RepairResult`
  with scheduling counts (`*_enqueued` + `orphans_imported/skipped/failed` +
  `dimensions_fixed`/`face_boxes_fixed`/`face_boxes_skipped`/`face_links_cleared`/
  `sideways_faces_enqueued`);
  `DisallowUnknownFields`, an empty selection →
  400, an orphan import without an importer → 503 (`ErrOrphanImportUnavailable`). The repairs are idempotent and
  run through the job queue (thumbnail/pHash via the `thumbnail` job, embeddings/faces backfill), and **never
  delete originals**. `dimensions` is the exception that writes the catalogue directly, in two halves. It
  rewrites the pixel dimensions of quarter-turned photos whose columns hold the **displayed** frame instead of
  the stored one (the import defect that letterboxed the viewer and drifted the face boxes),
  each row corrected from **the file's own EXIF document** rather than from a guess about where it came from
  (`transposed_dimensions` is that half's dry run). It then corrects the face boxes recorded against the same
  transposed frame — **not** with one blind transform: those rows are not all in the same coordinate space, so
  each is decided from the photo's own face markers (a quarter turn for a box that is in the raw frame, a
  per-axis rescale for one the sidecar had already rotated, or only the cached frame when the box is right), and
  a row whose space the markers cannot establish is left **completely untouched** (`face_boxes_skipped`) so a
  later run can pick it up once the photo carries a marker to reconcile it against. `transposed_face_boxes`
  counts what would be written (per face row, sampled by photo uid), so it is the dry run of that half; every
  write is guarded on the exact state it replaces, so a re-run is a no-op and no box is ever moved twice. `face_markers` is the other
  direct-write repair: a marker describes one region, so at most one detected face may claim it, and
  `duplicate_face_markers` (sampled by **marker uid**) counts the markers more than one face row still caches —
  the surplus links non-exclusive matching wrote, which render one person twice and mislead everything reading
  `faces.subject_uid`. The repair re-derives each affected photo's exclusive pairing and clears the cached
  `marker_uid`/`subject_uid`/`subject_name` of every face but the one the pairing awards the marker to
  (`face_links_cleared`); it deletes no face and no marker, and leaves a marker with a single face link alone —
  genuinely duplicated **markers** from an import are a different problem. `sideways_faces` is the third
  face repair, and the only one that re-runs detection: the embeddings sidecar reads no EXIF, so a
  quarter-turned photo sent as it lies on disk was detected **on its side** — its boxes are in a frame nobody
  displays and the faces the detector missed on a turned picture are simply absent. `sideways_face_detections`
  (quarter-turned photos whose recorded detection frame is not the displayed one, sampled by photo uid) is the
  dry run; the repair clears each photo's detection record and enqueues `face_detect`
  (`sideways_faces_enqueued`), so the fixed job re-detects it on an upright image once the sidecar's box is
  awake. The face rows are kept until that detection replaces them, nothing is deleted, and a photo re-detected
  upright leaves the finding for good. `POST /maintenance/audit/purge` `{older_than_days}` (a positive integer of days,
  1..36500) deletes audit entries older than `now − older_than_days` (`audit.Store.PurgeOlderThan`,
  a single `DELETE` via `idx_audit_log_created_at`) → `{deleted,older_than_days,cutoff}`;
  a missing/non-positive/excessive window or an unknown field → 400, an unwired audit store → 503. The
  purge itself is **audited** (`audit.purge` with the cutoff, the window and the number deleted; the entry is fresh, so
  the purge survives) — deleting the trail stays traceable.
  `GET /maintenance/nameless-subjects` → `{subjects,marker_total,face_total}` is the **read-only report** of the
  importer-minted catch-all subject (`people.ListNamelessSubjects`; each entry is a `Subject` plus
  `marker_count`/`face_count`). `POST /maintenance/nameless-subjects/detach` **applies** it: the response body
  *is* the undo file (`namelessjob.Undo`, `Content-Disposition: attachment`, the plan in
  `X-Kukatko-Nameless-Subjects`/`-Markers`/`-Faces`), and the detach is enqueued **only after that body has been
  written and flushed** — the HTTP form of the CLI's `--apply` refusing without `--undo-file`, since the
  browser's download is the operator's only way back. A snapshot that cannot be read → 500, a client the file
  cannot be written to → nothing scheduled, nothing to detach → 409; all three leave the catalogue untouched.
  The body carries no `Content-Length`, so a client that reads it to EOF (any browser taking it as a Blob) knows
  the jobs are queued. `POST /maintenance/nameless-subjects/restore` takes that file back (raw JSON body, ≤64 MiB;
  unparsable or subject-less → 400) → `202 {queued}`. Both destructive directions run as the `nameless_detach` /
  `nameless_restore` jobs over `people.DetachSubject`/`RestoreSubject` (audited inside their own transaction),
  never inline: detaching production's catch-all moves ~111 000 faces into the partial HNSW index of migration
  0047 and takes minutes. Mounted always (`buildMaintenanceAPI`
  in `cmd/kukatko/maintenance.go`). The `maintenance` operation that stays CLI-only, the **library wipe**
  (`kukatko maintenance reset`, `internal/reset`), is deliberately **not exposed here** — like the destructive
  `restore db`, it would pull the tables out from under a running server, so it lives only in the CLI. It is
  still audited (`library.reset`, written in the truncation's own transaction), so it shows up in `GET /audit`
  like everything else.
- **Duplicates API (`/api/v1`, `internal/duplicatesapi` + `internal/duplicates`, editor/admin via
  `RequireWrite`):** `GET /duplicates?limit=&offset=` → `{groups,total,limit,offset,next_offset}`
  groups of likely duplicates from pHash Hamming distance (`duplicate.phash_max_diff`,
  banded-LSH) **and/or** embedding cosine distance (`duplicate.embedding_max_dist`, HNSW), merged by
  union-find into connected components (no O(n²) scan). Each group carries members (thumbnail/dimensions/
  size/`taken_at`/distances) + `reason` (phash/embedding/both) + `confirmed` (a human has answered "yes, the
  same shot" about one of its pairs, via the review game or `POST /feedback/duplicate-confirmations`) + a
  suggested `keeper_uid`
  (highest resolution → largest → oldest → uid); ordered **confirmed-first**, then largest, then newest keeper,
  then id — confirmation outranks size because it is the only key here that is not a guess. `limit`≤100, invalid →
  400, scan fails → 500. The listing **only reads**; when `duplicate.enabled=false` the `GET` route answers 503.
  `POST /duplicates/merge` (`internal/dupmerge`, `RequireWrite`) `{keeper_uid,member_uids[],dry_run?}` →
  `{keeper_uid,albums_added,labels_added,people_added,metadata_filled[],archived,dry_run}`: in **one
  transaction** it merges the remaining copies into the chosen keeper — a union of albums, labels and people
  (a subject↔keeper marker without a box, type `label`), fills the missing scalar fields (title/description +
  per-user rating/favorite/flag; never overwrites an existing value), archives the copies (`archived_at`, originals
  to purge) and writes `photos.merge` to the audit. Idempotent (re-running on a resolved group = a no-op);
  `dry_run:true` only computes a preview without changes. An invalid group → 400, a non-existent keeper → 404,
  `merge=nil` → 503. The `merge` route runs even with detection off. Mounted always by `buildDuplicatesAPI` (`cmd/kukatko/duplicates.go`).
- **Repeated-marker API (`/api/v1`, `internal/dupmarkersapi` + `internal/dupmarkers`):** the other kind of
  duplicate — not two photos, but **one person marked more than once on one photo**. On a group shot the
  matcher puts the same name on two or three neighbouring boxes, so the people beside her lose their tag and
  her own face count is inflated; it is always a mistake.
  `GET /duplicate-markers?limit=&offset=` (**`RequireAuth`** — reading is not a write) →
  `{groups,total,limit,offset,next_offset}`, each group
  `{photo_uid,photo_title,taken_at?,width,height,orientation,subject_uid,subject_name,
  markers:[{uid,bbox,score,reviewed}]}`, worst (most markers) first, then by the person's name; markers ordered
  **left to right**, so a client's numbering reads in reading order. `limit` ≤ 200 (default 50), invalid → 400,
  scan fails → 500, no backend → 503. It counts **markers, not faces**: several detected faces matched onto one
  marker is a face↔marker pairing bug (`internal/facematch`) and looks identical in the UI, so counting faces
  here would mix the two and neither figure would fall when either is fixed. Valid (`invalid=false`) `face`
  markers of **named** subjects only — the nameless catch-all holds thousands of untagged regions and would
  bury every real finding — and non-archived photos only.
  The two repairs are `RequireWrite` and **neither is new behaviour**:
  `POST /duplicate-markers/keep` `{photo_uid,subject_uid,keep_marker_uid}` →
  `{photo_uid,subject_uid,keep_marker_uid,detached[]}` keeps that one marker and clears the subject from every
  other valid face marker of that person on that photo, each through the existing
  `unassign_person` transition (`subject_uid` → NULL, `reviewed` → false, `faces` cache refreshed,
  `face.unassign` audited) — **detached, never deleted**: the box almost always belongs to the person standing
  next to her and is worth re-tagging. The losing markers are deliberately **not** in the request body: the
  server resolves the group from (photo, subject) itself, so a stale client list cannot detach a marker that has
  meanwhile been re-tagged. A keeper that is not one of that person's valid face markers there → **404 before
  anything is detached**.
  `POST /duplicate-markers/invalid` `{marker_uid}` → 204 flags one box as holding no face at all
  (`marker.invalidate`); the row survives and **keeps its subject**, so the decision is reversible and an
  invalidation stays distinguishable from an unassignment — every listing that means "a real face" already
  filters `invalid = FALSE`, this one included, so the group shrinks and, at one marker, disappears.
  The third decision, "leave it be", is a durable opinion rather than a repair and lives with the rest of the
  persisted feedback at `POST`/`DELETE /feedback/duplicate-marker-dismissals` (see the Feedback API below).
  Mounted always by `buildDupMarkersAPI` (`cmd/kukatko/dupmarkers.go`), sharing the photo API's
  `facematch.Service`. The UI is `/duplicate-markers` (`DuplicateMarkersPage`, editor/admin).
- **System status API (`/api/v1`, `internal/systemapi` + `internal/system`, maintainer-only via
  `RequireMaintainer`):** `GET /system/status` → one aggregated snapshot behind the whole admin dashboard —
  what is in the library, what is still to do, and how the instance is doing:
  `{version,database{reachable,error?},embeddings{online,url},jobs{by_state,by_type,by_type_state,total,
  dead_letter,pending_embeddings},backup (=backup.Status),imports{folder (=importer.Run|null)},
  storage{originals_bytes,cache_bytes,free_bytes,total_bytes},
  maps{configured,state,degraded,detail?,checked_at?},
  geocode{configured,budget_enabled,limit,spent,remaining,window_seconds,resets_at?},
  library{photos,videos,trashed,hidden,private,uploads{day,week,month,year},albums,labels,people,faces,
  embeddings,library_bytes,trash_bytes,derived_bytes},
  remaining{faces_unassigned,clusters,photos_without_taken_at,photos_without_gps,photos_without_place,
  photos_without_ocr,duplicate_markers,duplicates{configured,available,groups,computed_at?}}}`.
  `jobs.by_type_state` (type → state → count) is what the dashboard renders; `by_type`/`total` are
  **lifetime** tallies, because the queue table keeps finished jobs — `image_embed: 41 594` against 20 930
  photos described a one-off re-embedding, not a backlog, which is why the UI labels them "ever run". All
  three views are sums over **one** `jobs.Store.CountsByTypeState` scan, so they cannot disagree.
  `library` is the browsable catalogue (`trashed` is **not** part of `photos`; `hidden`/`private` are), its
  `uploads` windows are counted on `created_at` against the database clock and include archived photos, and
  `library_bytes`/`trash_bytes` are the **catalogue's** `sum(file_size)` — the number that is meaningful when
  the originals live in an object store and the server's disk (`storage`, clearly labelled as the server disk
  in the UI) holds none of them. `derived_bytes` is the one measured value, `storage.cache_bytes`.
  `remaining` is the backlogs, all cheap SQL except `duplicates`: the near-duplicate scan is far too
  expensive for a polled endpoint, so it is refreshed **in the background** (15 min TTL, 2 min timeout,
  `duplicates.Service.CountGroups`) and reported with `available` + `computed_at`; `available:false` means
  "no answer yet" and `groups` is then not a count of zero. `configured:false` = duplicate detection is off
  (`duplicate.enabled`), the same switch that makes `GET /duplicates` answer 503.
  `library` + the SQL half of `remaining` are one query (`system.Store.CountDashboard`) memoized for 30 s.
  `maps` = the last observed mapy.com state
  from the proxy (`mapy.Health`, no probe of its own): `state` ∈ `unknown|ok|key_rejected|rate_limited|
  unavailable|error`, `degraded=true` for all except `ok`/`unknown` — **a rejected key (403) is
  visible here**, not only as a grey map; `detail` is sanitized (never the key). `geocode` = the
  **reverse-geocode credit budget** (`placesjob.WindowBudget`): how many of the `limit` geocodes the current
  window has `spent` and when it `resets_at`, so a running import's metered mapy.com spend is visible while
  it happens instead of being reconstructed from the bill; `configured:false` = no mapy.com key (nothing
  geocodes), `budget_enabled:false` = the cap is off (`maps.geocode_budget ≤ 0`), leaving only the
  per-second limiter. The same numbers are exported as `kukatko_geocode_credits_spent_total` (counter) and
  `kukatko_geocode_credits_remaining`/`_limit` (gauges).
  A merge of existing subsystems
  (embeddings health, the job queue, backup status, the last import per source via
  `importer.Store.LatestRun`, disk usage, a DB ping); storage is memoized for 30 s. Collect fails (the DB
  for the queue/imports/dashboard counts) → 500; an unavailable DB/storage is inline best-effort, and a
  failed duplicate scan leaves the tile unavailable rather than failing the snapshot. Mounted **always**
  (`buildSystemAPI` in `cmd/kukatko/system.go`). The admin UI **System** (`/system`, `SystemStatusPage`)
  polls every 5 s and offers quick actions (requeue the dead letter whole or per type, trigger backup, links
  to import/maintenance).
- **Library statistics (`/api/v1`, `internal/systemapi` + `internal/system`, **every authenticated
  user** via `RequireAuth`):** `GET /system/stats` → instance-wide counts of the catalogue, modelled on
  the previous system's status page: `{photos,videos,live_photos,images,photos_live,photos_archived,
  photos_hidden,photos_stacked,photos_listed,
  photos_with_embedding,photos_with_faces,photos_without_embedding,photos_without_faces,photos_with_gps,
  photos_geocoded,
  photos_pending_geocode,embeddings,faces,faces_assigned,faces_unassigned,subjects,subjects_person,
  subjects_pet,subjects_other,markers,
  markers_assigned,markers_unassigned,albums,albums_manual,albums_folder,albums_moment,albums_state,
  albums_month,labels}`.
  Never per-user and never a `maintenance scan` tree walk — a single query of cheap `COUNT(*)`s
  (`system.Store.CountLibrary`), memoized for **30 s** like the storage block. `photos_archived` is
  exactly the trash (a photo is soft-deleted by stamping `archived_at`; there is no second trash state),
  and `videos`/`live_photos`/`images` are the three `media_type` values (`images` derived, see below).
  **`photos_live` is not what the library grid lists** — that is `photos_listed`, and the three counts
  between them say why: `photos_live = photos_listed + photos_hidden + photos_stacked`, where
  `photos_hidden` is the not-archived photos the user hid (`hidden_from_library`) and `photos_stacked` the
  not-archived, **not-hidden** non-primary stack members (`stack_uid IS NOT NULL AND NOT stack_primary`) —
  the RAW sibling or edited variant the grid shows once, behind the stack's primary. The stacked count
  excludes the hidden ones **on purpose**: the parts are disjoint, so the subtraction is exact and no photo
  is deducted twice. The two predicates mirror `photos.hiddenClauses` / `photos.stackClauses`, so
  `photos_listed` equals what `photos.Store.Count` answers with default `ListParams` — i.e. the „Počet
  fotek" the library page prints. They exist because the statistics page reported 20 890 where the grid said
  20 619 with nothing to explain the difference; `/stats` now shows the whole walk down (see
  `docs/FRONTEND.md`, `LibraryStatsCards`).
  `photos_geocoded` counts photos with a cached place resolved from coordinates, `photos_pending_geocode`
  the live geotagged photos that still have none — the outstanding, metered mapy.com spend.
  `photos_with_gps` (photos carrying coordinates of their own, whatever the source) and `faces_assigned`
  (detected faces that name a subject — unlike `markers_assigned` it excludes hand-drawn label boxes) are the
  numerators of the statistics page's three coverage meters; the third is `photos_with_embedding`.
  **Faces and markers are two grains and never add across:** `faces` is one row per detection
  (`faces_assigned + faces_unassigned`, so the halves always make the whole), `markers` is the boxes drawn on
  photos (`markers_assigned + markers_unassigned`) — the sets overlap and most detections never became a
  marker. `faces_unassigned` is the library's actual naming backlog, exactly what the review game, the
  clustering and the candidate search work over (`faces.subject_uid IS NULL`); `markers_unassigned` is not.
  The derived values — `photos_live`, `photos_listed`, `images` (total minus the other two media types,
  because the `media_type` index deliberately excludes the majority value) and the coverage gaps
  `photos_without_embedding` / `photos_without_faces` / `faces_unassigned` / `markers_unassigned` — are
  computed by the service
  from the raw counts (clamped at 0) and are what makes the endpoint useful during import verification;
  `photos_without_faces` cannot distinguish "not yet detected" from "genuinely no face". A failed
  aggregation → **500** (never a body of zeroes, which would read as an empty library). Deliberately
  **not** part of `/system/status` and it does not loosen that endpoint's maintainer guard. Mounted
  **always** (`buildSystemAPI`). The frontend renders it on **Statistiky** (`/stats`, all roles);
  `SystemStatusPage` does **not** read it — its Library section comes from the dashboard half of
  `/system/status`, which answers a different question ("what is in the library?" rather than "how much of
  it is processed?") from the same store, so the shared counts still agree. The same aggregation also backs
  the `kukatko_library_*` gauges on `/metrics` (see `docs/OPERATIONS.md` § Prometheus metrics), so the two
  cannot disagree — one query, two readers.
- **Library charts (`/api/v1`, `internal/systemapi` + `internal/system`, **every authenticated user** via
  `RequireAuth`):** `GET /system/stats/charts` → the series the statistics page draws over those counts:
  `{photos_by_year:[{year,photos}], added_by_month:[{month:"YYYY-MM",photos}],
  top_cameras:[{camera,model,photos}], storage_by_media:[{media,photos,bytes}],
  storage_by_year:[{year,photos,bytes,cumulative_bytes}]}`.
  A **second endpoint rather than more fields on `/system/stats`**: the counts are cheap and are what an
  import is watched with, these aggregates are heavier and change slowly, so the two are fetched side by side,
  fail apart, and are cached apart — the counts for 30 s, the charts for **5 min**
  (`system.defaultChartsTTL`), which is the answer to "cheap enough per view, or cached?" for these five.
  Each series is **one grouped query** (`system.Store.AggregateCharts`, `store_charts.go`) with no join:
  capture years and years-of-addition group the whole live table, the month window is bounded by a sargable
  `created_at >= $1` on `idx_photos_live_created_at`, and the camera ranking is a `LIMIT 10` over a grouped
  make/model.
  Every series counts the **browsable** library — archived photos are excluded throughout, so a bar matches
  the library view it links to — and buckets time **in UTC**, so the buckets do not shift with the database
  server's time zone. `photos_by_year` covers only photos with a capture time; `top_cameras` folds make and
  model into `camera` for display and keeps the bare `model`, which is what the library's `camera` filter
  matches; `storage_by_media` splits `image`/`live`/`video`/`raw`, where **RAW is not a `media_type`** but a
  file format recognised by the original's extension (`imgconvert.RAWExtensions()`) and carved out of the
  images, so the four buckets are disjoint and add up to the library.
  The service returns them **drawable**: both year axes have their empty years restored (a histogram whose
  gaps are merely missing draws a lie), `added_by_month` is always exactly 12 months ending with the current
  one, `storage_by_media` always carries all four buckets, `cumulative_bytes` is the running total, and every
  array is `[]` rather than `null`. An empty library therefore answers **200** with empty series and a full
  window of zeroes, not an error. A failed aggregation → **500** (never empty series, which would draw as an
  empty library). Mounted **always** (`buildSystemAPI`). The frontend renders it on **Statistiky**
  (`/stats`, all roles) below the counts; `SystemStatusPage` does not read it.
- **Capabilities API (`/api/v1`, `internal/capabilitiesapi`, authenticated via `RequireAuth`):**
  `GET /capabilities` → `{semantic_search:bool, version:{version,commit}}` — a small object saying what this
  instance is, which **every authenticated user** may read (unlike the maintainer-only `/system/status`).
  `semantic_search` is
  the **cached** reachability state of the embeddings sidecar (not a live probe): filled by the background loop
  `internal/reachability` (a probe every 60 s, `cmd/kukatko/capabilities.go`); when `embedding.url` is not
  set, it is always `false`. `version` is the link-time build metadata of the running binary (`version.Info`,
  injected at wiring by `version.Get()`) — the same value `/healthz` reports, verbatim including a
  development build's `dev`/`none` placeholders. It rides along here rather than being read from `/healthz`
  (a monitoring endpoint) or baked into the frontend bundle: the bundle is `//go:embed`-ed into the binary,
  so a version compiled into it would drift from the binary that serves it, while a value read from the
  server cannot. The shape is **deliberately open** for future flags (e.g. maps-configured).
  The frontend (`CapabilitiesProvider`) polls it and hides the link to semantic search in
  `FilterBar` accordingly, when the box is offline (full text keeps working), and prints `version` in the
  user menu (`Layout`, `MobileNavDrawer`) and in full — with the commit linked — on `/help`. Mounted **always**.

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
| `"…"` | a value with spaces — `camera:"Canon EOS R6"`; text in quotes is literal, `*` included |
| `\` | escapes an operator (pipe, `!`, `-`, `"`, `:`, `*`), so it matches literally |

An escape example: `label:a\|b` (a backslash before the pipe) searches for a label with a literal
pipe `a|b` instead of an OR of two alternatives; likewise `iso:100\-400` is no longer a range, and therefore
degrades to free text, and `title:foo\*bar` (or `title:"foo*bar"`) searches for a literal star instead of
using the wildcard. Keys are case-insensitive (`ISO:100` = `iso:100`). **An unknown key or an invalid value is not
an error**: the whole token is searched as ordinary text (so `foo:bar` still finds a photo by its caption)
and the response returns it in `unknown_tokens`, so the UI can show a hint. `*` is the **only** wildcard —
SQL's `%` and `_` always match themselves, in a filter value, in free text and in its `-term` negation alike.
**`person:me` — the caller's own person.** `me` in a `person:`/`subject:` value resolves to the subject
the caller's account is linked to, so it composes with everything else (`person:me year:1998`) and can be
saved as a smart album. It is resolved **outside** `internal/query` (which stays a pure parser with no
notion of a caller) by `internal/personme`, called by the photo API's list/search/timeline/years handlers
and by the MCP search tool. A caller with **no linked person** gets an **empty** result plus the reason
`person_me_unlinked` in `notices` — never everything, and never a free-text search for the word "me";
the MCP tool answers with an error naming the fix instead. **The collision with a person actually called
"me" is resolved in favour of the token**, but only in its exact lower-case spelling: `person:Me` or
`person:ME` is an ordinary (case-insensitive) name match that still finds a subject named "me", and any
subject is always reachable by UID (`person:<uid>`), which no name can shadow.
**`uploader:me` — what the caller uploaded.** The same word under the uploader key, resolved the same way
(`internal/personme`, `ResolveUploader`) against the **account making the request** rather than a person it
is linked to — so it cannot fail and needs no notice; every other spelling (`uploader:Me`) is an ordinary
name match. `uploader:none` is not caller-dependent at all and is compiled by the photos store into
"no uploader".
Every **fractional** bound (`f:1.8`, `f:1.8-2.8`, `f:1.8-`) is tolerated within ±0.005 due to the rounding of
single-precision EXIF columns; whole-number bounds stay exact. Capture-time filters resolve in **UTC**
(the connection pool pins the session zone), so `year:`, `?year=`, `taken:` and the year/timeline histograms
put a photo taken minutes either side of New Year in the same year.

### Filters

| Filter | Value | Matches |
| --- | --- | --- |
| `uid:` | a photo's UID **or** the source UID it was imported under | exactly one photo, by its own id or by the `pt…` id it was imported under (`photos.photoprism_uid` and `photoprism_aliases`). It **removes the default live-only, visible-only and stack-primary scopes**, so an archived, hidden or stacked photo is found — naming an id is explicit intent. One key covers both id shapes because they cannot collide (26 vs 16 characters) |
| `title:` `description:` `notes:` | text | the corresponding photo column (substring, `*` wildcard) |
| `filename:` | text | the file name |
| `keywords:` (alias `keyword:`) | text | IPTC keywords |
| `text:` | text | the text a recogniser read **inside** the photo (`photos.ocr_text`): a sign, a shop front, a scanned page. Substring, `*` wildcard, and **accent-insensitive** unlike its siblings — the latin recogniser routinely returns a Czech word without its diacritics, so `text:pouť` must still find a sign read as "Pout" |
| `album:` | text | album membership by **name** (substring) or exact UID |
| `label:` | text | a label by **name** or UID |
| `person:` (alias `subject:`) | text | a subject by **name** or UID, via non-invalid markers. The exact lower-case value **`me`** is reserved: it means the person the caller's own account is linked to (`users.subject_uid`) — see below |
| `uploader:` | text | who uploaded the photo, by the account's **username or display name** (substring, `*` wildcard, and **accent-insensitive** like `text:` — a name is typed from memory, so `uploader:tomas` finds "Tomáš") or by exact UID. Two exact lower-case values are reserved: **`me`** is the caller's own account (see below) and **`none`** are the photos with **no** uploader, the ones an import brought in — so `uploader:!none` is everything somebody did upload |
| `favorite:` `private:` `archived:` | `yes\|no` | per-user favourite / private / archived; `archived:` **removes the default live-only scope** |
| `hidden:` | `yes\|no` | hidden from the library (`photos.hidden_from_library`); like `archived:` it **removes the default visible-only scope**, so `hidden:yes` is the documented way back to a hidden photo |
| `rating:` | `0-5`, ranges | the current user's rating; no row = 0, so `rating:0` finds the unrated |
| `flag:` | `pick\|reject\|eye` | the current user's flag |
| `year:` `month:` `day:` | number, ranges | year (1000–9999) / month (1–12) / day (1–31) of capture |
| `taken:` `added:` | `YYYY`, `YYYY-MM`, `YYYY-MM-DD` | date of capture / of adding to the catalog (whole day/month/year) |
| `dated:` | `yes\|no` | has / has no capture date. `dated:no` is the worklist of everything the timeline cannot place, and it deliberately covers **both** the photos whose date somebody declared unknown and those that never had one — the same job either way. Provenance separates them on the photo (`taken_at_source`, `taken_at_before_unknown`) |
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

### Complexity limits (400)

To stop a single authenticated request from forcing an arbitrarily expensive query, `q` is capped
before it compiles to SQL (constants live in `internal/query`; enforced by `parseListParams` and by the
MCP `search_photos` tool):

- **Length** — `q` may be at most **8192** characters (`query.MaxLength`).
- **Complexity** — the parsed query may compile to at most **256** conditions (`query.MaxComplexity`),
  counting one per free-text term plus one per filter OR-alternative. So `label:cat\|dog` is 2 and
  `title:a\|a\|…\|a` with hundreds of pipes is that many; a legitimate search is a handful.
- **Scope params** — the repeatable `?album=`/`?label=`/`?person=` params add at most **256** UIDs combined
  (the param-path equivalent of the alternatives cap).

Anything past a cap is rejected with **400** and a descriptive message; every honest query is far below
the limits and is unaffected.
