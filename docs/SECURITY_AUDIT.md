# Security Audit — Kukátko

- **Audit date:** 2026-07-14
- **Commit reviewed:** `c410b23a7004f8124e489f2181a213c36a1b7535` (branch `main`)
- **Scope:** whole application — Go backend + DB, React frontend (`web/`), data-at-rest &
  secrets, dependencies & build (all four areas of the task spec).
- **Method:** read-only. Traced from the HTTP boundary inward (every `/api/v1` handler:
  who reaches it, what they can request), then followed each candidate to confirm it is
  reachable with attacker input. Ran `govulncheck ./...` and `npm audit`.

**Summary (5 lines).** The codebase is unusually security-disciplined: no SQL injection,
no command injection, no path traversal, no XSS, RBAC guards are consistent and correct,
IDOR is closed (saved-searches 404 a foreign owner; favorites/ratings are server-scoped),
secrets never leave the server, and the MAPY key stays server-side. **One HIGH** issue is
real and reachable: attacker-controlled IP headers (`True-Client-IP`) defeat the login
brute-force limiter because `middleware.RealIP` is trusted unconditionally. The remaining
items are DoS-hardening gaps (unbounded upload default, missing socket timeouts), a
data-at-rest weakness (session/download tokens stored in cleartext, amplified by an
unencrypted S3 dump), an out-of-date build toolchain (10 stdlib vulns from `govulncheck`),
and several low/info hardening + config-hygiene notes. `npm audit` is clean (0
vulnerabilities). Nothing here required a code change — this report only documents.

> **Severity scale:** critical / high / medium / low / info. A weakness that is not
> reachable from the HTTP layer with attacker input is rated **info** unless a concrete,
> plausible non-HTTP attacker (e.g. a leaked backup) makes it exploitable, in which case it
> is rated on that scenario. Findings are ordered most-severe first.

---

## Findings

### SEC-001 — HIGH — Client-controlled IP header defeats the login brute-force limiter (and forges audit/access-log IPs)

- **Where:**
  - `internal/server/server.go:89` — `router.Use(middleware.RealIP)` applied unconditionally,
    with no trusted-proxy allow-list.
  - `internal/auth/handlers_auth.go:69` — login limiter key = `normalizeUsername(username) + "|" + clientIP(r)`.
  - `internal/auth/handlers_auth.go:49-56` — `clientIP` reads `r.RemoteAddr`, which `RealIP`
    has already overwritten from request headers.
  - `internal/auth/handlers_apitoken.go:41-56` — same limiter also guards token minting.
  - `internal/audit/audit.go:194-209` — the audit `ip` field is likewise taken from the
    forgeable headers.
  - Root cause in the dependency: `go-chi/chi/v5@v5.2.1/middleware/realip.go:45` checks
    `True-Client-IP` **first**, then `X-Real-IP`, then the leftmost `X-Forwarded-For`, and at
    `:34` assigns `r.RemoteAddr = rip` — with no check that the request actually came from a
    trusted proxy.
- **Attack scenario:** An **anonymous** attacker repeatedly `POST`s
  `/api/v1/auth/login` with body `{"username":"admin","password":"<guess>"}` and a rotating
  header `True-Client-IP: 10.0.0.<n>` (a different value per request). The login limiter
  (default 10 failed attempts / 15 min *per username+IP*, `internal/config/config.go:365-368`,
  defaults `:585-586`) keys on the IP half, so every request forms a fresh bucket and
  `limiter.Allow` never returns false. The **only** brute-force control is fully neutralised,
  enabling unlimited online password guessing against any known account. bcrypt cost 12 slows
  each attempt (~hundreds of ms) but does not stop the attack. `True-Client-IP` is **not** set
  or stripped by a default Traefik front end, so the bypass holds even behind the documented
  reverse proxy. The same spoofing writes attacker-chosen source IPs into every audit-trail
  row and access-log line (forensic/attribution integrity), and bypasses the per-IP throttles
  on `/upload`, `/photos/bulk`, `/import/*`, and `/map/tiles`.
- **Suggested fix:** Do not trust client IP headers unconditionally. Either (a) drop
  `middleware.RealIP` and key the limiters + audit IP on the real transport peer
  (`r.RemoteAddr`), or (b) only honour `RealIP` when the direct peer is inside a configured
  trusted-proxy CIDR allow-list, and have Traefik strip inbound
  `True-Client-IP`/`X-Real-IP`/`X-Forwarded-For` and set them itself. Additionally add an
  **IP-independent per-username failure counter** so one account cannot be brute-forced
  regardless of source IP.

### SEC-002 — MEDIUM — Unbounded upload → disk-exhaustion DoS (default config ships with no cap)

- **Where:**
  - `internal/config/config.go:660` — `v.SetDefault("upload.max_file_size_mb", 0)` (0 =
    unlimited); mirrored in `config.example.yaml:269` (`max_file_size_mb: 0`).
  - `internal/ingest/ingest.go:220-221,230` — the `io.LimitReader` cap is applied only
    `if s.maxFileSize > 0`, so with the shipped default there is **no per-file limit**.
  - `internal/ingest/http.go:67-108` — `handleUpload` streams the multipart body part-by-part
    with **no `http.MaxBytesReader` on the request body and no cap on the number of parts**.
- **Attack scenario:** An authenticated **editor or admin** (the `/upload` route is
  `RequireWrite`-gated, `internal/ingest/http.go:52`) POSTs one enormous file, or an endless
  multipart body with unlimited parts. Bytes stream to the temp dir / storage root (correctly
  *not* buffered in RAM, so no OOM) and **fill the disk**, taking down Kukátko — and, because
  this host shares one filesystem with the co-located Postgres and other stacks, potentially
  those too. The default config ships with zero backstop.
- **Suggested fix:** Default `upload.max_file_size_mb` to a sane non-zero value; wrap the
  upload body in `http.MaxBytesReader`; cap the number of parts per request; optionally guard
  free disk space before publishing an original.

### SEC-003 — MEDIUM — Session & media-download tokens stored in cleartext at rest (amplified by an unencrypted backup)

- **Where:** `internal/auth/store_session.go:14,35,48` — the `sessions` table stores `token`
  and `download_token` verbatim and looks them up by plaintext equality. Contrast
  `internal/auth/apitoken.go:115-118`, where API-token secrets are **SHA-256-hashed at rest**
  precisely so a DB/backup leak yields no usable credential. Amplifier:
  `internal/backup/s3.go:127` uploads the `pg_dump` (which contains this table) with no
  server-side encryption set (see SEC-010).
- **Attack scenario:** This is **not reachable from the HTTP layer** — it requires the
  attacker to already have read access to the database or to a backup dump. Given that the DB
  is dumped to S3 on a schedule, that precondition is realistic: anyone who obtains a dump
  (leaked bucket, stolen S3 keys, a misconfigured replica, an operator with DB read) copies
  any live `token` and replays it as a session cookie to impersonate that user, or uses
  `download_token` as `?t=` to pull that user's originals — valid until `expires_at` (sliding
  TTL, default 168 h, max 720 h). No cracking needed; the value is used verbatim.
- **Suggested fix:** Store only a SHA-256 of the session/download token and look up by hash
  (mirror the API-token path); the client keeps the plaintext in its cookie/URL.

### SEC-004 — MEDIUM — Out-of-date build toolchain: 10 known standard-library vulnerabilities (`govulncheck`)

- **Where:** the module toolchain (`go1.26.1`, per `go version`). `govulncheck ./...` reports
  **11 vulnerabilities in called code** — 10 in the Go standard library and 1 in
  `go-chi/chi/v5` (the chi one is *not* reachable, see below).
- **Attack scenario:** Several of the reported stdlib bugs are reachable from the running HTTP
  server. The clearest DoS vectors: `GO-2026-4918` (infinite loop in the HTTP/2 transport on a
  crafted `SETTINGS_MAX_FRAME_SIZE`), `GO-2026-5038` (quadratic blow-up in
  `mime.WordDecoder.DecodeHeader` — reachable via multipart upload header parsing), and
  `GO-2026-4870` (a TLS 1.3 `KeyUpdate` record causing persistent connection consumption). A
  remote client sending the right bytes drives CPU/goroutine exhaustion. The remaining stdlib
  entries are `crypto/x509`/`crypto/tls`/`net` issues that are lower-impact for this
  server-side deployment (it does not validate client certificates), but they are the same
  class of "toolchain is behind." Full list (all fixed by a newer Go):
  - `GO-2026-5856` (crypto/tls, fixed 1.26.5), `GO-2026-5039` (net/textproto, 1.26.4),
    `GO-2026-5038` (mime, 1.26.4), `GO-2026-5037` (crypto/x509, 1.26.4),
    `GO-2026-4971` (net, 1.26.3), `GO-2026-4947` (crypto/x509, 1.26.2),
    `GO-2026-4946` (crypto/x509, 1.26.2), `GO-2026-4918` (net/http HTTP/2, 1.26.3),
    `GO-2026-4870` (crypto/tls, 1.26.2), `GO-2026-4866` (crypto/x509, 1.26.2).
  - `GO-2025-3770` (`go-chi/chi/v5` — host-header injection → open redirect in
    `RedirectSlashes`, fixed in v5.2.2): **not reachable** — `RedirectSlashes` is not used
    anywhere in the repo (`grep -rn RedirectSlashes` is empty); the vulnerable symbol is never
    called. Bump for hygiene only.
- **Suggested fix:** Build the release binary with **Go ≥ 1.26.5** (the CI/build toolchain,
  not the repo source, is the fix), and `go get -u github.com/go-chi/chi/v5@v5.2.2`. Add
  `govulncheck` to CI so future toolchain drift is caught.

### SEC-005 — LOW — No HTTP security headers (clickjacking / MIME-sniffing hardening absent)

- **Where:** `internal/server/server.go:88-108` — the middleware stack is `RequestID`,
  `RealIP`, injected metrics/log middlewares, `Recoverer`, and nothing else. No
  `Content-Security-Policy`, `X-Frame-Options`/`frame-ancestors`,
  `X-Content-Type-Options: nosniff`, or HSTS is emitted on API responses, the embedded SPA, or
  media.
- **Attack scenario:** An attacker frames the Kukátko admin/login UI on a page they control to
  attempt a clickjacking overlay, or relies on a browser MIME-sniffing user-uploaded media
  into an executable type. Impact is bounded here: session cookies are `SameSite=Strict` (a
  cross-site framed page cannot carry the cookie into a state-changing request), originals are
  served `Content-Disposition: attachment` with a sniffed MIME (`internal/photoapi/media.go:160-185`),
  and React auto-escaping keeps the reflected-XSS surface small — so this is defence-in-depth,
  not an open door. There is no CSP backstop if an HTML sink is ever introduced.
- **Suggested fix:** Add a small headers middleware: `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY` (or CSP `frame-ancestors 'none'`), a conservative CSP for the SPA,
  and HSTS when `secure_cookies` is on.

### SEC-006 — LOW — Username enumeration via a login timing oracle

- **Where:** `internal/auth/service.go:52-65` (`Login`). The unknown-user and disabled-user
  branches return `ErrInvalidCredentials` **before** any bcrypt call, while a valid enabled
  user runs `bcrypt.CompareHashAndPassword` (~250 ms at cost 12).
- **Attack scenario:** An **anonymous** attacker POSTs `/api/v1/auth/login` with candidate
  usernames and a dummy password and measures response latency: ~250 ms ⇒ a valid, active
  account; sub-millisecond ⇒ nonexistent or disabled. This enumerates valid admin/editor
  usernames, which feeds directly into SEC-001's unlimited guessing. (Note: the *error text*
  is correctly generic — this is purely a timing side-channel.)
- **Suggested fix:** On the not-found / disabled branches, run a dummy
  `bcrypt.CompareHashAndPassword` against a fixed dummy hash so every login path takes constant
  time.

### SEC-007 — LOW — Session cookie `Secure` flag is off by default

- **Where:** `internal/config/config.go:271,559` — `web.secure_cookies` defaults to `false`;
  wired at `cmd/kukatko/auth.go:29` → `internal/auth/cookie.go:20`. (`HttpOnly` and
  `SameSite=Strict` are always set — verified `internal/auth/cookie.go:19-21`.)
- **Attack scenario:** If an operator serves the app over HTTPS but forgets to set
  `web.secure_cookies=true`, the browser will also transmit the opaque session token over any
  accidental/downgraded plain-HTTP request (ssl-strip, mixed content, a stray `http://` link),
  exposing a full session credential to an on-path attacker. `.env.example` sets `true`, so
  this is operator responsibility, but the insecure default is easy to miss.
- **Suggested fix:** Default `secure_cookies` to `true` (opt-out for local HTTP dev), or emit
  a startup warning when auth runs with non-secure cookies on a non-loopback bind.

### SEC-008 — LOW — No socket read/write/idle timeouts (Slowloris-on-body)

- **Where:** `internal/server/server.go:92-96` — the `http.Server` sets `ReadHeaderTimeout`
  only; there is no `ReadTimeout`, `WriteTimeout`, or `IdleTimeout`.
- **Attack scenario:** A slow client (a slow request body on `/upload`, or a slow reader on a
  large download/transcode) holds a connection and its goroutine open indefinitely
  (Slowloris-on-body). Combined with SEC-001's throttle bypass, an attacker can accumulate
  many such connections to exhaust server resources. Uploads are auth-gated, which limits who
  can trigger the body variant.
- **Suggested fix:** Set bounded `ReadTimeout`/`WriteTimeout`/`IdleTimeout` (or per-route
  timeouts that still accommodate large streaming transfers).

### SEC-009 — LOW — Media download token travels in the URL query string (`?t=<token>`)

- **Where:** `internal/auth/middleware.go:36,63` (server reads `?t=`), minted into
  `<img>`/`<video>`/download URLs by `web/src/services/photos.ts:512-513,678,698` and consumed
  in `web/src/pages/PhotoDetailPage.tsx` and the `Lightbox`/`LivePhoto`/`VideoPlayer`
  components.
- **Attack scenario:** The per-session download token rides in the query string so that
  `<img>`/`<video>` tags (which cannot send an `Authorization` header) can fetch originals.
  Query-string credentials can surface in browser history and be leaked to third-party origins
  via the `Referer` header of any resource loaded on the same page, and appear if a user
  copy-pastes a media link. Impact is bounded: it is a **distinct**, media-read-only token
  (not the session cookie), it expires with the session, and it is deliberately kept **out of
  the access log** (the request logger records `r.URL.Path` only, `internal/obs/middleware.go:106`).
- **Suggested fix:** Prefer the session cookie for same-origin `<img>`/`<video>` where
  possible; for the paths that truly need a bearer, move to a short-lived **signed URL path
  segment** (the R2 backend already does this via `internal/storage/sign.go`) and/or send
  `Referrer-Policy: no-referrer`.

### SEC-010 — LOW — Backup database dump uploaded without explicit server-side encryption

- **Where:** `internal/backup/s3.go:127` — `PutObject` sets only `ContentType`; no
  `ServerSideEncryption` (and no client-side encryption). The dump contains every bcrypt hash,
  all user emails, and (per SEC-003) every live session/download token in cleartext. No ACL is
  set either, so objects are private by default — that part is fine.
- **Attack scenario:** If the backup bucket lacks *default* encryption (nothing in code
  enforces or checks it) and the object store is later compromised (stolen keys, provider
  breach, snapshot leak), the attacker reads the full user table and the replayable tokens of
  SEC-003.
- **Suggested fix:** Set `minio.PutObjectOptions.ServerSideEncryption` (SSE-S3 / SSE-KMS) on
  dump uploads, or document a hard requirement that the backup bucket has default encryption +
  block-public-access.

### SEC-011 — INFO — `web.allowed_origins` is a dead config key (misleading, not exploitable)

- **Where:** `internal/config/config.go:268,558` are the **only** references in the whole repo;
  there is no CORS middleware anywhere (`grep -rn 'Access-Control|cors' internal cmd` finds
  nothing else).
- **Attack scenario:** This is **not** an over-permissive-CORS problem — no
  `Access-Control-Allow-Origin` header is ever sent, so cross-origin browser reads are blocked
  by default (safe). The risk is misleading configuration: an operator who sets
  `allowed_origins` to enable a legitimate second origin gets a silent no-op and may work
  around it insecurely elsewhere.
- **Suggested fix:** Either wire `allowed_origins` into a `go-chi/cors` middleware, or remove
  the key and its docs so it does not imply a control that does not exist.

### SEC-012 — INFO — `web.session_secret` is dead, and docs promise a startup warning that does not exist

- **Where:** `internal/config/config.go:267,557` are the only references; nothing reads
  `SessionSecret`. Session cookies carry a raw 256-bit random token
  (`internal/auth/cookie.go` sets `Value: token`, not a signed value), so the secret is
  genuinely unused — which is acceptable. But `deb/kukatko.env` and `.env.example` state "the
  server logs a warning at startup if it is unset," and no such warning is implemented.
- **Attack scenario:** No exploit — documentation/implementation mismatch only. An operator may
  believe an unused knob is protecting them.
- **Suggested fix:** Remove the unused key and its docs, or implement the promised startup
  warning.

### SEC-013 — INFO — `/metrics` is exposed without authentication

- **Where:** `internal/server/server.go:130-132`, enabled by default (`metrics.enabled=true`,
  `internal/config/config.go:591`). Mounted outside `/api/v1` with no auth guard (by design,
  for Prometheus scraping).
- **Attack scenario:** Exposes DB-pool stats, job-queue depth, and per-route request counts —
  no secrets. On the documented tailnet/Traefik-fronted deployment the impact is negligible; it
  would leak operational cardinality only if the port ever became broadly reachable.
- **Suggested fix:** Keep it network-restricted (bind/scrape over the tailnet only); optionally
  gate behind a metrics token if the port could ever be publicly exposed.

### SEC-014 — INFO — `vipsthumbnail` receives the source path without a `--` separator (theoretical, falls back safely)

- **Where:** `internal/thumb/vips.go:159,174` — `src` is passed as the first positional
  argument to `vipsthumbnail` with no `--` end-of-options separator, and libvips also
  interprets a trailing `filename[opts]` suffix.
- **Attack scenario:** A user-chosen upload filename becomes a path segment
  (`<root>/YYYY/MM/<name>`), so a name like `x.jpg[shrink=2]` is theoretically passed through.
  But the part before `[` then points at a non-existent file, so vips merely errors and the
  pipeline falls back to the pure-Go thumbnailer. No file disclosure or execution — not
  reachable, listed for completeness. (The `vips` engine is also opt-in; the default is
  pure-Go.)
- **Suggested fix:** Prefix the path (`./`) or add a `--` separator when invoking
  `vipsthumbnail`, as hardening.

---

## Areas checked — no finding ("prověřeno, bez nálezu")

Silence is not evidence; these areas were examined and are clean.

### Backend & DB

- **SQL injection — clean.** Every query across `internal/photos`, `internal/photoapi`,
  `internal/savedsearch`, `internal/organize`, `internal/globalsearchapi`, `internal/auth`,
  `internal/audit`, `internal/bulk` binds caller values via `$N` placeholders. The only
  string-built SQL fragments are *identifiers* chosen from closed sets, never request text:
  `ORDER BY` comes from the `sortColumns` allow-list (`internal/photos/store_list.go:41-43,604`;
  unknown ⇒ `taken_at`), direction is a hardcoded `ASC`/`DESC`
  (`internal/photos/store_list.go:589-593`), the API sort param is mapped through a whitelist
  (`internal/photoapi/params.go:36-41`) before it becomes an enum, and the `fmt.Sprintf` hits
  in `store_trash.go`/`store_maintenance.go`/`places.go`/`audit.go`/`bulk/apply.go` only build
  `$N` placeholders or interpolate internal constants (e.g. the `"albums"`/`"labels"` table
  name, `"title"`/`"description"` column names — never user input).
- **IDOR — clean.** Saved-searches are owner-scoped at the API layer: `ownedSearch`
  (`internal/savedsearchapi/savedsearchapi.go:159-174`) fetches by `{uid}` then checks
  `saved.OwnerUID != user.UID` → **404** for GET/PATCH/DELETE; list/create bind `user.UID`
  from the auth context. Per-user favorites and ratings take the acting user from
  `auth.UserFromContext` only (`internal/photoapi/favorites.go:159,181`,
  `internal/photoapi/ratings.go:117,147`), never from a path/query param. API-token list/revoke
  is owner-scoped (others → 404, so ids can't be probed; admin may revoke any). Albums, labels,
  and subjects are **intentionally shared** (household model), all mutations `RequireWrite`-gated
  — not an IDOR. Since the "private photo" feature was removed, the catalog has no per-photo
  ownership, so all authenticated users legitimately see all photos.
- **RBAC — clean.** Every mutating route across all 21 `RegisterRoutes` is guarded, split along the
  role ladder: writes by `RequireWrite`; **operations surfaces** (`/jobs`, `/process`,
  `/maintenance`, `/backup`, `/restore`, `/system`, and the import triggers `/import/*`) by
  `RequireMaintainer` (maintainer only — a plain admin is refused); **governance surfaces**
  (`/admin/users`, `/audit`) by `RequireAdmin` (admin or maintainer via the ladder); the
  permanent trash operations (`POST /trash/empty` and the per-photo `POST /photos/{uid}/purge`)
  tightened from write to `RequireAdmin` because they destroy originals irreversibly — the
  reversible archive (soft delete) stays `RequireWrite` and `GET /trash/info` stays `RequireAuth`;
  media by `RequireAuthOrDownloadToken`. No under-guarded mutating route; a nil middleware would
  panic at wiring, not silently pass. No editor/viewer can reach a governance or operations
  surface, and a plain admin cannot reach an operations surface; no viewer can reach a mutation.
  The only unauthenticated routes are `/healthz`, `/metrics` (SEC-013), and the SPA static
  handler. No `pprof`/`expvar`/`/debug` endpoints exist.
- **Auth primitives — clean.** bcrypt cost **12** (`internal/auth/password.go:13`); session and
  download tokens are 256-bit from `crypto/rand`, independently generated
  (`internal/auth/token.go`, `internal/auth/service.go:80-96`); API-token secret is 256-bit,
  **SHA-256-hashed at rest**, compared with `subtle.ConstantTimeCompare`
  (`internal/auth/apitoken.go:107-126`), shown in plaintext once — the deliberate non-use of
  bcrypt for full-entropy tokens is correctly justified in-code. `User.PasswordHash` is
  `json:"-"` and never serialized.
- **Session lifecycle & CSRF — clean.** Password change/reset and account-disable delete the
  relevant sessions (`internal/auth/service.go:214`, `internal/auth/service_admin.go:240-285`);
  a disabled user is rejected and their sessions purged on next `Authenticate`; sliding expiry
  is capped by an absolute `MaxLifetime`. CSRF is covered by `SameSite=Strict` + `HttpOnly` for
  cookie auth, and Bearer endpoints are inherently CSRF-immune.
- **Audit trail — clean.** Verified atomic: `internal/organize/audit.go`,
  `internal/people/audit.go`, and `internal/bulk/apply.go` all `Begin` → mutate →
  `audit.Write(ctx, tx, entry)` → `Commit`, so the audit row commits/rolls back with the
  mutation and cannot be suppressed. `actor_uid` comes from the auth context (unforgeable). No
  plaintext passwords or tokens are persisted (`internal/auth/handlers_admin.go:164` passes
  `nil` details; token entries store only the token *name*). (The audit `ip` field is forgeable
  — folded into SEC-001.)
- **SSRF (map tile proxy) — clean.** `internal/mapsapi` + `internal/mapy`: `mapset` is
  allow-listed, `z`/`x`/`y` parsed as non-negative ints, the upstream URL is built with
  `url.JoinPath` (escapes segments) off a fixed config base; the API key is header-only.

### File handling, upload, storage, command injection

- **Command injection (RCE) — clean.** Every `exec` site (`internal/imgconvert/{heif,raw}.go`,
  `internal/video/{poster,probe,transcode}.go`, `internal/exif/exiftool.go`,
  `internal/thumb/vips.go`, `internal/backup/{pgdump,pgrestore}.go`) uses
  `exec.CommandContext(binary, arg1, arg2, …)` with the binary a fixed constant and each
  argument a separate slice element — **no `sh -c`, no shell string, no filename/EXIF value
  concatenated into a command line**. Shell-metacharacter RCE is impossible.
- **Argument injection — clean.** `internal/exif/exiftool.go:55` (`exiftool -json -n -- <path>`)
  and `internal/video/probe.go` (`ffprobe … -- <path>`) use the `--` end-of-options separator.
  The other exec sites lack `--` but always pass an **absolute filesystem path** (storage root
  or an `os.CreateTemp` file) or a **server-signed `https://` URL** — never a value an attacker
  can make begin with `-`. `photo.FilePath` is generated by the storage layer as
  `YYYY/MM/<name>` and materialized to an absolute path before any exec call. (The lone
  theoretical exception is `vipsthumbnail`, SEC-014, which falls back safely.) `pg_dump`/
  `pg_restore` pass the DSN via libpq environment variables (`PGDATABASE`, `PGPASSWORD`), never
  on argv.
- **Path traversal — clean.** `internal/storage/fs.go:256 confine()` does
  `path.Clean("/" + …)` then strips the leading `/`, collapsing `../` and absolute paths before
  `filepath.Join(root, …)`; used by `FS.safeAbs` and `R2.objectKey`. `sanitizeName` reduces the
  upload filename to `filepath.Base`. Media routes (`internal/photoapi/{media,video}.go`)
  resolve the storage path from the **DB row looked up by `{uid}`**, never from a raw URL
  segment; `internal/thumb/thumb.go` additionally validates the hash is hex and long enough.
  ZIP export (`internal/photoapi/zip.go`) strips separators, `..`, and control chars — no
  zip-slip.
- **Signed URLs — clean.** `internal/storage/sign.go`: HMAC-SHA256 over `key\n<expiry>` (both
  tamper-covered), constant-time `hmac.Equal`, default 1 h TTL, dual-secret rotation, signature
  checked before expiry. A signed URL is an intended short-lived bearer capability, minted
  fresh per response and only into responses the caller was already authorized to receive.
- **Upload streaming — clean (no OOM).** The ingest pipeline streams to a temp file computing
  SHA-256 during the copy (`internal/ingest/ingest.go:212-235`) — no whole-file buffering. (The
  *size/part* limits are the SEC-002 gap; RAM is safe.)

### Frontend (`web/`)

- **XSS — clean.** **Zero `dangerouslySetInnerHTML` in application code** (the only `innerHTML`
  hits are `document.body.innerHTML` inside test files). No `eval`, `new Function`,
  `document.write`, `insertAdjacentHTML`, or `setAttribute('href'/'src')`. No markdown/HTML
  renderer is present (no `react-markdown`/`marked`/`dompurify` in `web/package.json`). The one
  imperative DOM path — the map popup — correctly uses `textContent`/`img.alt`
  (`web/src/lib/mapPopup.ts:34`), so a malicious photo title renders as literal text. No
  `href`/`src` is built from a free-text user field, so there is no `javascript:`-URL vector.
- **Token storage — clean.** The session is an **HttpOnly cookie** (unreadable from JS); auth
  calls use `credentials: 'same-origin'`. The `download_token` lives **only in React state in
  memory** (`web/src/auth/AuthProvider.tsx:71`), never in `localStorage`/`sessionStorage`.
  `localStorage`/`sessionStorage` hold only UI preferences (grid density, slideshow settings,
  language) — no secrets.
- **No sensitive data in console/URL — clean.** There is **no `console.log/error/warn/debug`
  anywhere in `web/src`**. The only credential in a URL is the download token (SEC-009). The
  MAPY key never reaches the client — the Leaflet layer points at the backend proxy path
  `${API_BASE}/map/tiles/...` (`web/src/services/map.ts:143-144`); the only mapy.com references
  are the public attribution text and logo.

### Data at rest & secrets

- **No committed secrets — clean.** `rg 'password=|secret=|api_key|AKIA|BEGIN.*PRIVATE KEY|bearer '`
  over the repo (minus `node_modules`) returns nothing. The only git-tracked env files are
  `.env.example` and `deb/kukatko.env`, both placeholders (`CHANGE_ME`/`CHANGEME`);
  `config.example.yaml`'s DSN uses a literal `password` placeholder. `.gitignore` correctly
  excludes `.secrets/`, `*.local.yaml`, `.env`/`.env.*` (keeping `.env.example`), and
  `config.local.*`.
- **MAPY_API_KEY stays server-side — clean.** `internal/mapy/mapy.go` sends the key only in the
  `X-Mapy-Api-Key` header; it never appears in a returned URL, error, or the
  tile/rgeocode/geojson response bodies. `statusError` deliberately drops the upstream body
  (which mapy.com sometimes echoes the key into).
- **Logging — clean.** `internal/obs`: the access log records `slog.String("path", r.URL.Path)`
  only — **not** `RawQuery` — so the `?t=<download_token>` media tokens never reach the log, and
  a redacting `ReplaceAttr` hook scrubs any attr keyed `password/token/secret/dsn/authorization/cookie`.
  Startup logs only the thumb engine, never the config/DSN.

### Dependencies & build

- **`npm audit` — clean.** 0 vulnerabilities across 390 dependencies (59 prod / 332 dev / 52
  optional). Output: `{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0}`.
- **`govulncheck` — see SEC-004** (10 reachable stdlib vulns from an out-of-date toolchain; 1
  chi vuln flagged but the vulnerable `RedirectSlashes` symbol is unused).
- **`CGO_ENABLED=0` — confirmed.** The release build (`Makefile:123`, `.goreleaser.yaml:17`,
  `scripts/dev.sh:63`) is a pure-Go static binary — no C toolchain, eliminating that class of
  memory-safety bugs. (`CGO_ENABLED=1` appears only in the test-only `-race` targets, not in
  any shipped artifact.)

---

## Appendix — how to reproduce the scans

```sh
# Go standard library + module vulnerabilities (from the repo root)
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...

# Frontend dependency audit
cd web && npm audit
```

*This report is documentation only; per the task spec no production code, packages, or
configuration were changed. Confirmed findings were each verified reachable from the HTTP
layer (or, for data-at-rest items, from a concrete non-HTTP attacker) before inclusion.*
