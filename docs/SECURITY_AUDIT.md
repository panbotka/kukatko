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

**Status since the audit** (the summary above is the 2026-07-14 snapshot; each finding's own
section is authoritative). Fixed: **SEC-001** (HIGH, the trusted-proxy allow-list + the
per-username login budget) and **SEC-006** (the login timing oracle) on **2026-08-12** — they were
the only two reachable *anonymously*, and they composed into one working online password-guessing
chain, so they were closed together. **SEC-015** and **SEC-016** were fixed earlier. **SEC-017** (an
uploaded DASH manifest made ffprobe/ffmpeg fetch its URLs) was reproduced and fixed on **2026-10-09**,
and so was **SEC-018** (in-process image decodes had no memory bound across concurrent uploads), and
**SEC-019** (concurrent requests overshot an upload link's file cap; a revoke did not stop a running request),
and **SEC-020** (a Web Push subscription endpoint made the server POST to internal hosts).
**SEC-021** (account recovery revokes sessions but not API tokens or passkeys) was confirmed on
**2026-10-09** and is open: it waits on a product decision, and behaviour is unchanged.
**SEC-022** (an expiring API token mints a non-expiring token and registers a passkey) was confirmed on
**2026-10-09** and is open on the same terms; its recommended fix is the bearer half of SEC-021's.
Everything else below is open as written.

> **Severity scale:** critical / high / medium / low / info. A weakness that is not
> reachable from the HTTP layer with attacker input is rated **info** unless a concrete,
> plausible non-HTTP attacker (e.g. a leaked backup) makes it exploitable, in which case it
> is rated on that scenario. Findings are ordered most-severe first.

---

## Findings

### SEC-001 — HIGH — **FIXED** — Client-controlled IP header defeats the login brute-force limiter (and forges audit/access-log IPs)

- **Where (before the fix):**
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
- **Fix (2026-08-12):** option (b), plus the suggested per-username counter, so neither
  half alone has to hold.
  1. **`internal/clientip`** (new package) replaces `middleware.RealIP`.
     `clientip.Middleware` (`internal/clientip/clientip.go:144-152`, mounted at
     `internal/server/server.go:117`) resolves the client address **once** per request and puts it
     on the context; `clientip.FromRequest` reads it back. A forwarding header is honoured **only**
     when the socket peer is in the configured trusted set (`resolve`, `:181-195`): the
     `X-Forwarded-For` chain is then walked right-to-left and the first hop that is not itself a
     trusted proxy wins (`rightmostUntrusted`, `:203-218`). From anyone else the socket address
     wins. `True-Client-IP` — the header chi checked *first*, which nothing in this deployment sets
     or strips — is **never** read at all. Without the middleware, `FromRequest` falls back to the
     socket peer, so a handler mounted on another router fails safe.
  2. **The trusted set is configuration:** `web.trusted_proxies`
     (`internal/config/config.go:277-297`), default `["loopback", "private"]`, validated at startup
     (`ErrInvalidTrustedProxy`). The `100.64/10` Tailscale range is deliberately excluded: a tailnet
     carries clients, not proxies. Deployment notes in `docs/OPERATIONS.md` §Configuration keys.
  3. **One address everywhere:** the limiter key (`internal/ratelimit/ratelimit.go:187-193`), the
     login key (`internal/auth/handlers_auth.go:48-54,100-106`), the audit row
     (`internal/audit/audit.go:344-352`) and the access log's `remote_ip`
     (`internal/obs/middleware.go:113-116`) all read `clientip.FromRequest`, so forensics and
     throttling can no longer disagree — and neither can be forged.
  4. **An IP-independent per-username budget** (`internal/auth/handlers_auth.go:108-121`,
     `internal/auth/http.go:17-24,63-73`): every login attempt is charged to the per-(username, IP)
     bucket *and* to a per-username one of `3 × auth.login_rate_limit` over the same window. Even if
     addresses ever became forgeable again, one account cannot be guessed at faster than that.
  Tests: `internal/clientip/clientip_test.go` (resolution table, rotating headers → one address),
  `internal/server/trustedproxy_test.go` (a forged header cannot refill a real limiter's bucket; a
  trusted proxy's header still gives each client its own), `internal/auth/login_limit_test.go`
  (the login key, the per-username budget).

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

### SEC-006 — LOW — **FIXED** — Username enumeration via a login timing oracle

- **Where (before the fix):** `internal/auth/service.go:52-65` (`Login`). The unknown-user and
  disabled-user branches returned `ErrInvalidCredentials` **before** any bcrypt call, while a valid
  enabled user ran `bcrypt.CompareHashAndPassword` (~250 ms at cost 12).
- **Attack scenario:** An **anonymous** attacker POSTs `/api/v1/auth/login` with candidate
  usernames and a dummy password and measures response latency: ~250 ms ⇒ a valid, active
  account; sub-millisecond ⇒ nonexistent or disabled. This enumerates valid admin/editor
  usernames, which fed directly into SEC-001's unlimited guessing. (Note: the *error text*
  was correctly generic even then — this was purely a timing side-channel.)
- **Fix (2026-08-12):** `Login` (`internal/auth/service.go:60-84`) no longer branches before the
  comparison; it hands the lookup's outcome to **`checkLoginPassword`**
  (`internal/auth/login_password.go:40-48`), which runs **exactly one** bcrypt comparison on every
  path — against the account's hash when the username resolved, against `dummyPasswordHash()` when
  it did not — and only then decides. The dummy hash is minted **through `HashPassword`**
  (`login_password.go:21-27`), so it always carries the same work factor as real accounts; a
  cheaper stand-in would silently restore the oracle, which is why
  `TestDummyPasswordHash_matchesProductionCost` asserts `bcrypt.Cost` equals `hashCost`.
  `TestCheckLoginPassword_timingIsIndistinguishable` measures all three failure branches (unknown,
  disabled, wrong password): 320.9 / 321.0 / 320.8 ms on the ARM dev box at cost 12, against a
  tolerance of 2× — the failure it exists to catch is a branch skipping bcrypt entirely, which is
  three orders of magnitude off.

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
  (Slowloris-on-body). Combined with SEC-001's throttle bypass (since fixed), an attacker can
  accumulate many such connections to exhaust server resources. Uploads are auth-gated, which limits who
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

### SEC-015 — HIGH — **FIXED** — Unbounded login rate-limiter map → RAM-exhaustion DoS

- **Where (before the fix):** `internal/auth/handlers_auth.go` keyed the limiter on
  `normalizeUsername(req.Username) + "|" + clientIP(r)` with **no length validation**, and
  `internal/auth/ratelimit.go` backed it with an uncapped `map[string][]time.Time` that only
  ever shrank on the hourly `Cleanup` tick (`cmd/kukatko/serve.go`,
  `sessionCleanupInterval`).
- **Attack scenario:** An **unauthenticated** attacker floods `POST /api/v1/auth/login`
  (public, no outer IP limit) with a distinct ~1 MB username per request — the body cap is
  1 MiB and an unknown username returns before bcrypt, so the requests are cheap. Every one
  leaves a megabyte-scale key in the single process-global map. On this shared 16 GB host
  (co-resident Postgres and other stacks) RAM is exhausted within minutes. Independent of
  SEC-001 (no header spoofing needed) and of SEC-002 (RAM, not disk).
- **Fix:** two independent bounds, either of which alone caps the damage.
  1. `MaxUsernameLen` = 64 runes, `validateUsername` → `ErrUsernameTooLong` → **400**, checked
     in `handleLogin` *before* the username is used as a limiter key or looked up, and in
     `prepareNewUser` so no unusable account is created.
  2. `Limiter` gained a hard `maxKeys` = 8192 cap enforced **on insertion**: it first drops
     expired keys, then evicts the least recently seen down to `evictTargetKeys`. Eviction
     ranks by a per-key `lastSeen` refreshed even on *blocked* attempts, so a key flood
     cannot evict — and thereby clear — an active block. The per-(username, IP) throttling
     is otherwise unchanged.
- **Not addressed here:** the optional IP-independent per-username failure counter suggested
  under SEC-001 — since added, see SEC-001's fix note.

### SEC-016 — MEDIUM — **FIXED** — Irreversible loss of all maintainer (operations) capability

- **Where (before the fix):** `internal/auth/service_admin.go` `authorizeUserManagement`
  returned `nil` immediately for a maintainer actor and `guardMaintainerBoundary` authorized
  the action without asserting any invariant; the handlers
  (`internal/auth/handlers_admin.go`) never compared actor against target. Nothing counted
  how many maintainers were left.
- **Attack scenario:** availability, not confidentiality — and reachable by accident as
  easily as on purpose. The sole maintainer demotes (`PATCH /admin/users/{uid}` with a lower
  `role`) or disables (`POST /admin/users/{uid}/disable`) their own account, or the last
  other maintainer. With zero enabled maintainers left, `authorizeUserManagement` refuses
  every non-maintainer that tries to grant `maintainer` back (`newRole == RoleMaintainer` →
  `ErrMaintainerRequired`), there is no delete-user endpoint, and `Bootstrap` only runs when
  the users table is **empty** — so backup, restore, import, maintenance, jobs and processing
  are permanently unreachable and only direct database surgery brings them back. A
  single-account instance (the common case here) reaches this in one click.
- **Fix:** `internal/auth/store_maintainer.go`. `withMaintainerGuard` counts **enabled**
  maintainers before and after the mutation, inside the mutation's own transaction, and
  returns `ErrLastMaintainer` (→ **409**, message `auth: cannot remove the last maintainer`)
  when the count would drop from ≥1 to 0; the error rolls back the change *and* its audit
  row. Counting the outcome rather than inspecting the request makes the guard indifferent to
  how the capability was lost — role change, disable, both at once, and any delete path added
  later are covered by routing through it, which
  `UpdateUserProfile{,Audited}`/`SetUserDisabled{,Audited}` all now do. The before-count is a
  `SELECT … FOR UPDATE` over the enabled maintainers (ordered by `uid`), so two concurrent
  demotions of two *different* maintainers queue instead of each seeing the other and both
  committing into the forbidden state. A **disabled** maintainer does not satisfy the
  invariant — it cannot log in — and an instance that already has zero stays fully editable,
  so the guard forbids *dropping* to zero without ever freezing an install that is already
  there.
- **Not addressed here:** a maintainer may still lock *themselves* out individually (demote
  or disable their own account) as long as another enabled maintainer remains — that is
  recoverable by the other maintainer and therefore intentionally allowed.

### SEC-017 — MEDIUM — **FIXED** — A DASH manifest uploaded as `clip.mp4` made ffprobe/ffmpeg fetch its URLs (SSRF)

- **Origin:** an unconfirmed lead from a source-only audit (2026-10-06, commit `3792f7c`), evaluated and
  reproduced on **2026-10-09** against commit `d8fe125`.
- **Where (before the fix):** `internal/uploadlinkapi/public.go` checks only the filename extension; the
  bytes are staged extensionless (`kukatko-ingest-*`, `internal/ingest/ingest.go`) and handed to `admit`
  (`internal/ingest/sniff.go`). A text file matches no media signature, so for a video-named file
  `probedAsMedia` ran `video.Probe` → `ffprobeArgs` (`internal/video/probe.go`), which passed no
  `-format_whitelist`/`-protocol_whitelist`/`-f`. libavformat auto-detected the demuxer, and the **dash**
  demuxer fetched the `BaseURL` the manifest names. Every later ffmpeg run on a stored copy had the same
  argv shape: poster + candidate samples (`internal/video/poster.go`), HLS encode
  (`internal/hls/encode.go`), storyboard (`internal/storyboard/generate.go`), on-the-fly transcode
  (`internal/video/transcode.go`), metadata re-probe (`internal/metajob/extractor.go`, via `video.Probe`).
- **Attack scenario:** an **anonymous upload-link holder** (`POST /api/v1/u/{code}/upload`) or any curator
  uploads `clip.mp4` whose bytes are a static DASH MPD with `<BaseURL>http://<internal-host>/…</BaseURL>`.
  During admission — before anything is stored — the server sends a GET to that host (a blind SSRF into
  the tailnet/loopback, e.g. an unauthenticated internal admin GET). If the target answers with a real
  media file (an attacker host does), ffprobe reports a codec, the "video" is catalogued, and every
  poster/encode/storyboard/transcode/re-probe repeats the fetch.
- **What was tested** (locally only; no deployed host was contacted): a loopback listener and two
  extensionless fixtures naming it — a minimal static DASH MPD and an HLS media playlist — run through the
  exact `ffprobeArgs`, then through `posterArgs` on a copy named `clip.mp4`. Local build: **ffmpeg
  6.1.1-3ubuntu5** (Ubuntu 24.04, aarch64), `-demuxers` lists `dash`, `hls`, `concat`,
  `webm_dash_manifest`; `-buildconf` has `--enable-libxml2`.
- **Result — confirmed for DASH:** the listener received `GET /dash/v.mp4` from both the ffprobe argv and
  the poster argv. **HLS was not followed** on this build: `hls_probe` refuses a playlist whose name is not
  `.m3u8`/`.m3u` ("Not detecting m3u8/hls with non standard extension"). Both outcomes depend on the
  ffmpeg build — dash exists only with libxml2, the hls name check only in FFmpeg ≥ 6. Production runs
  the unpinned `alpine:3` ffmpeg package (`Dockerfile`), whose build was not checked; the fix does not
  rely on it.
- **Fix:**
  1. `internal/video/input.go` — `InputArgs(src)` puts `-format_whitelist DemuxerAllowlist` (exactly
     the containers behind the ingested extensions: `mov,matroska,avi,asf,flv,mpeg,mpegvideo,mpegts,h264,hevc`)
     and a `-protocol_whitelist` before the input of **every** ffprobe/ffmpeg argv that reads user media:
     `ffprobeArgs`, `posterArgs`, `sampleArgs`, `TranscodeArgs`, `hls.EncodeArgs`, `storyboard.FFmpegArgs`.
     The protocol half follows the input: `file` alone for a local path, `http,https,tls,tcp` for an
     http(s) URL — the transcode and the encode still read a remote original straight from its signed
     R2 URL (or a dev MinIO's plain-HTTP one). ffmpeg now answers the manifest with "Format not on
     whitelist".
  2. `internal/ingest/sniff.go` — `admit` refuses a streaming manifest (`#EXTM3U`, `ffconcat`, `<MPD`,
     or `<?xml` with an `<MPD` behind it, after an optional BOM/whitespace) as **`ErrNotMedia`** before
     any tool is asked, whatever the file's name.
- **Regression test:** `internal/video/input_test.go` `TestManifestUpload_fetchesNothing` runs the DASH
  and HLS fixtures through every argv builder in `internal/video` (staged extensionless and as
  `clip.mp4`) against a counting loopback listener and fails on any request — verified to fail with the
  guard stubbed out. `TestManifestUpload_unguardedControl` logs what the unguarded argv does on the
  installed build; `TestInputArgs_remoteClipStillOpens` probes and transcodes a real clip served over HTTP.
  Every container behind `videoExts` (mp4, m4v, mov, 3gp, mkv, webm, avi, wmv, flv, mpg, mts, m2ts, h264,
  hevc) was rendered and still probes with the guard in place.
- **Fix commit:** `65a3954` (2026-10-09).

### SEC-018 — MEDIUM — **FIXED** — In-process image decodes had no memory bound: ~1 MB uploads cost GBs, with no limit on concurrent decodes

- **Origin:** an unconfirmed lead from a source-only audit (2026-10-06, commit `3792f7c`), measured and
  confirmed on **2026-10-09** against commit `5c74033`.
- **Where (before the fix):** each uploaded file is ingested synchronously in its request goroutine
  (`internal/uploadlinkapi/public.go` → `internal/ingest/ingest.go` `IngestFile`). `verifyPixels`
  (`internal/ingest/pixels.go`) fully decoded every JPEG/PNG/GIF whose `width×height` was under
  `thumb.max_pixels` (200 MP default). That check (`imgconvert.EnforcePixelBound`) ignores bit depth, so a
  200 MP 16-bit PNG decodes to 1.6 GB. The image was then kept for the pHash, and the blurhash took a full-size
  `Orient` copy of it. `generateThumbnails` → `internal/thumb` `decodeAndOrient` decoded the stored original a
  **second** time and scaled all eight sizes **from the source in parallel**. Each scale is
  `x/image/draw` `CatmullRom`, whose two-pass scratch is `dstWidth × srcHeight × 32 bytes` (1.7 GB for
  `fit_3840` of a 14142² image). Nothing in `ingest`, `uploadlinkapi` or `thumb` limited concurrency or memory.
  The public route has only request-rate buckets (per-IP burst 60, per-link burst 300). Production runs
  with no container memory limit on a 15 GB VPS (`docs/PERF.md`, the candidate-search OOM).
- **Attack scenario:** an **anonymous upload-link holder** (`POST /api/v1/u/{code}/upload`) or any curator
  posts a few single-colour 14142×14142 PNGs: 0.8 MB at 8 bits, 1.6 MB at 16. Each takes the server past
  3 GB, and the request-rate buckets admit dozens at once. That is a global OOM of the shared host, not one
  dead container. The same pattern, smaller, sat behind every ordinary upload: one 24 MP camera JPEG
  peaked at 1.48 GB, so even three honest concurrent uploads exceeded 3 GB.
- **Measured** (locally only, never against production): `internal/ingest/pixelbomb_test.go` (build tag
  `pixelbomb`; `go test -tags pixelbomb -run TestPixelBombMemory -v ./internal/ingest/`). It generates the
  bombs without ever holding their bitmap, then runs ingest's decode sequence (`verifyPixels` → pHash +
  blurhash → the thumbnailer's decode) for one upload and for three concurrent ones. Each scenario runs in its
  own process, the peak is read from `VmHWM`, and a scenario stops itself at 3 GiB. Before the fix:

  | Upload | ×1 | ×3 |
  |---|---|---|
  | 14142² 8-bit RGBA PNG, 0.81 MB | > 3 GiB (stopped) | > 3 GiB (stopped) |
  | 14142² 16-bit RGBA PNG, 1.59 MB | > 3 GiB (stopped) | > 3 GiB (stopped) |
  | 24 MP JPEG, 10.4 MB (control) | 1 481 MiB | > 3 GiB (stopped) |

  Lead **confirmed**. Peak per upload was in the GB range and grew with concurrency. It was worse than the lead
  estimated: most of it was resize scratch, not the bitmap.
- **Fix:**
  1. `internal/imgconvert/budget.go` adds a **process-wide byte budget**, `DecodeBudget` (config
     `thumb.decode_budget_mb`, default **1536**; one instance per process, built by
     `cmd/kukatko` `processDecodeBudget`). Before any `image.Decode`, every in-process decoder calls
     `ReserveDecode`/`ReserveConfig`. That applies the pixel cap and then reserves `w×h×BytesPerPixel(colour model)`,
     **bit depth included**, plus the caller's working copies. It waits while the budget is spent and refuses
     (`ErrImageTooLarge` + `ErrOverBudget`) a decode that alone exceeds the budget. A refused upload degrades
     exactly as an over-cap one always did: it is catalogued with a warning and has no thumbnail or pHash.
     Releasing ≥ 64 MiB runs a GC first, so the next admitted decode does not stack on the previous one's
     garbage. Callers covered: `ingest` (both decodes; the pre-store image is released right after the hashes,
     before the thumbnail decode), `thumb` (bitmap + `pureGoCost`: orientation copy, edit copies, the cascade),
     `thumbjob`'s pHash decoder (it had **no pixel cap at all**), `facejob`'s rotation, `photoapi`'s edited
     download (no pixel cap either; over budget it serves the original unedited), and `userpic`'s upload.
  2. `internal/thumb/cascade.go` adds the **resize cascade**: an integer box pre-shrink, then sizes rendered
     largest first, one at a time, each from a rendition at least twice its size. This takes the scratch from
     the sum of eight source-sized scales down to the largest single one (`docs/PERF.md` §2).
  3. `blurhash.EncodeOriented` orients the 64-px working copy instead of the full bitmap. The hashes are
     identical (`TestEncodeOriented_matchesEncodingTheOrientedImage`).
  4. `internal/userpic` (profile picture, **any signed-in role**, 8 MB body) had no pixel bound at all; 8 MB of
     PNG names a 30000×30000 image. `Normalize` now refuses a header whose bitmap exceeds `MaxDecodedBytes` =
     256 MiB, and `SetUpload` reserves from the budget.
- **After the fix** (same harness, default budget): 8-bit bomb 1 876 MiB ×1, **2 294 MiB ×3**. 16-bit bomb
  1 587 MiB ×1, 1 592 MiB ×3; its 2.38 GB thumbnail decode is refused. 24 MP JPEG 1 135 MiB ×1, **1 833 MiB ×3**.
  An end-to-end `serve` with a 512 MiB budget catalogued the 8-bit bomb with `phash_failed`/`thumbnail_failed`
  warnings, and the whole process peaked at 413 MiB.
- **Regression tests:** `internal/imgconvert/budget_test.go` `TestDecodeBudget_concurrentDecodesStayWithinBudget`
  (12 concurrent decodes never hold more than the budget, and decodes that fit do overlap),
  `TestReserveDecode_bitDepth` (the same dimensions are admitted at 8 bits and refused at 16), plus per-caller
  tests in `ingest`, `thumb`, `thumbjob`, `facejob` and `userpic`.
- **Not addressed here:** the budget bounds *live* bytes; resident memory runs ~1.2–1.5× it by Go's GC
  slack. The container memory limit proposed in `docs/PERF.md` and a `GOMEMLIMIT` are deployment changes in
  the `vps` repo and are still not applied. `thumb.max_pixels` is unchanged (200 MP): the byte budget now
  prices bit depth, and lowering the cap would refuse real panoramas. HEIC/RAW/video conversions run in
  external processes (`heif-convert`, `exiftool`, `ffmpeg`) and are outside the in-process budget.

### SEC-019 — MEDIUM — **FIXED** — Concurrent requests overshot an upload link's lifetime file cap, and a link revoked or expired mid-request kept taking files

- **Origin:** an unconfirmed lead from a source-only audit (2026-10-06, commit `3792f7c`), reproduced and
  confirmed on **2026-10-09** against commit `543b958`.
- **Where (before the fix):** `POST /api/v1/u/{code}/upload` (`internal/uploadlinkapi/public.go`).
  `ingestParts` seeded an in-memory counter from `link.UploadCount`, read **once** by the link lookup when the
  request began, and `ingestOne` compared only that counter with `upload_links.max_uploads_per_link`
  (default 2000). The authoritative increment in `RecordUpload` (`internal/uploadlink/uploads.go`,
  `countUploadSQL`) carried no cap, `revoked_at` or `expires_at` predicate. Liveness was checked only by
  that same lookup (`liveLink`). The rate limiters take one token per **request**, not per file (per-IP
  burst 60, per-link burst 300), and a request could carry any number of file parts; parts the pipeline
  refused did not count toward anything, so one request could stream junk parts for ever.
- **Attack scenario:** an **anonymous upload-link holder** — the link is posted to a group chat and may
  leak — opens N requests at once, each with many file parts. Every request passes the cap against the
  same stale count, so the link takes up to N × the cap (the per-IP burst alone admits 60 requests: 120 000
  files on a 2000-file link) into the library and the link's albums. And when a curator revokes a leaked
  link, a request already running keeps storing and filing files into its albums and labels until its body
  ends.
- **Reproduced:** `internal/uploadlinkapi/uploadlinkapi_integration_test.go`. With the cap at 2 and a barrier
  in a fake pipeline holding both requests' first files until both requests had passed the lookup, two
  concurrent requests of two distinct photos each ended with `upload_count = 4`, 4 provenance rows and **4
  photos in the album** (`TestConcurrentRequestsCannotOvershootTheFileCap`, run against the pre-fix
  `public.go`/`uploadlinkapi.go`/`uploads.go`). A store wrapper that revokes — or expires — the link right
  after the first of a request's two files is recorded saw the second file created and filed into the dead
  link's album (`TestLinkDyingMidRequestStopsItsFiles`). Lead **confirmed** on both counts.
- **Fix:**
  1. `uploadlink.Store.ReserveUpload(ctx, uid, maxUploads, now)` takes one file's slot in a **single
     `UPDATE`**: `upload_count + 1` only `WHERE revoked_at IS NULL AND expires_at > now AND (cap = 0 OR
     upload_count < cap)`. Concurrent requests serialise on the row, so no interleaving takes more slots than
     the cap holds. No row updated is a refusal, named afterwards as `ErrFull`, `ErrRevoked`, `ErrExpired` or
     `ErrNotFound`.
  2. The handler reserves **before the pipeline reads a byte** of the file, so no photo is stored outside
     the cap. A file the pipeline refuses, or one that cannot be filed, gives its slot back
     (`ReleaseUpload`, on a context that outlives the request). `RecordUpload` no longer counts; it only
     stamps `last_used_at`. The reservation is the moment a file is accepted, so a revoke that lands while
     one file is streaming lets that one file finish. Every later file is refused.
  3. Refusals stay what the handler already answered: a full link → per-file 429 `"this link accepts no
     more uploads"`. A link that died mid-request → per-file 410 with the dead link's own text `"upload link
     is no longer valid"`, which the upload page already recognises as "the link died". A link dead at the
     start of a request is still the whole-request 410.
  4. A request now carries at most **50 file parts** (`maxFilesPerRequest`), refused ones included. The 51st
     gets a per-file 413 and nothing after it is read.
  5. The misleading comment on `ratelimit.upload_link*` (`internal/config/config.go`, `config.example.yaml`)
     now says those buckets count requests, not files, and that the file cap is what bounds the files.
- **Regression tests:** the two integration tests above (now: 2 accepted + 2 refused with 429, count 2, 2
  photos in the album; the second file of the dying-link request is a 410, count 1, one photo filed).
  `internal/uploadlink/store_integration_test.go` `TestReserveUpload_capAndLiveness` covers the cap, 0 =
  unlimited, release and re-reserve, expiry, revoke, a missing link, and the floor at 0. The `TestUpload_limits`
  and `TestUpload_refusals` unit tests cover the slot release, the per-file 410, the failed reservation and
  the per-request part cap.
- **Not addressed here:** a server crash between a reservation and its release leaves the link one slot
  short. That fails safe (fewer uploads, never more), and a curator can always create a new link.

---

### SEC-020 — MEDIUM — **FIXED** — A Web Push subscription endpoint made the server POST to internal hosts (blind SSRF)

- **Origin:** an unconfirmed lead from a source-only audit (2026-10-06, commit `3792f7c`), reproduced and
  confirmed on **2026-10-09** against commit `c374238`.
- **Where (before the fix):** `POST /api/v1/push/subscriptions` (`internal/notificationapi/push.go`) stores
  any endpoint `push.ValidateSubscription` (`internal/push/keys.go`) accepts. That check is format only:
  length ≤ 2048, scheme `https`, a non-empty host. Production built the sender without an `HTTPClient`
  (`cmd/kukatko/push.go`), so `push.NewVAPID` fell back to `&http.Client{Timeout: 30s}`. That client has no
  redirect policy, no address policy and honours an environment proxy. webpush-go POSTs through it.
- **Attack scenario:** with `push.enabled` on, **any signed-in account, viewer included,** subscribes an
  endpoint of its choice: an IP literal (`https://127.0.0.1:…`, `https://10.…`, `https://169.254.169.254/…`,
  a tailnet `100.x` address), a hostname that resolves to one, or a public https server that answers `302`
  to `http://127.0.0.1:<port>/…`. Another account tagging the attacker's linked subject, or a registration
  notice to approvers, triggers a delivery, and the server's `push_send` job sends the request from inside
  its own network. A `301`/`302`/`303` is followed as a GET, a `307`/`308` replays the POST, and plain
  `http` is followed too. The body is never shown, but two status bits are: a 2xx stamps the subscription's
  `last_used_at` (visible in `GET /push/subscriptions`), and a 404/410 deletes it. That is enough to probe
  which internal ports and paths exist.
- **Reproduced:** `internal/push` test (run against the pre-fix `vapid.go`). A TLS `httptest` server
  answering `302 → http://127.0.0.1:<B>/probe`, a client trusting its certificate with the default redirect
  policy: `Send` returned **nil** and the plain-http recorder B received **one request**.
  `ValidateSubscription` accepted `https://127.0.0.1:1/x`, `https://10.0.0.1/x` and
  `https://169.254.169.254/x`. Lead **confirmed**.
- **Fix** (`internal/push/destination.go`):
  1. **No redirects.** Every sender client returns a 3xx to `Send` unfollowed (`CheckRedirect` →
     `http.ErrUseLastResponse`). `classify` reports it as a permanent `ErrRejected`, so the job fails without
     retrying. An injected client is **copied** with that policy, so the test seam cannot reintroduce it.
  2. **Public addresses only, at dial time.** The production client's `net.Dialer` has a `Control` hook
     (`refuseNonPublic`). It runs after DNS resolution on the exact address being connected to, so DNS
     rebinding is covered too. It refuses loopback, RFC 1918/ULA, link-local, unspecified and multicast
     addresses, CGNAT `100.64.0.0/10` (the tailnet), `0/8`, `192.0.0.0/24`, `198.18.0.0/15`, `240/4` and
     `fec0::/10`. IPv4-mapped and NAT64 (`64:ff9b::/96`) addresses are judged by the IPv4 inside, and
     `64:ff9b:1::/48` is refused. A refusal is a permanent `ErrRejected`, and nothing is sent. The client
     also takes **no proxy from the environment**, because a proxy would dial the endpoint itself, past the
     check.
  3. **Subscribe-time courtesy check.** `Store.Upsert` refuses visibly internal endpoints with
     `ErrInvalidSubscription` (→ 400): non-public IP literals, single-label hosts (`localhost`, `box`) and
     `*.localhost`. It resolves nothing, so the dialer stays the guard.
  4. **The test seam stays.** `push.Config.HTTPClient` still injects an `httptest` client, which keeps its
     transport and so reaches loopback. Only production (`HTTPClient` nil) gets the guarded dialer.
- **No allowlist of push-service hosts (decision).** The known services (FCM, Mozilla autopush, Windows
  `*.notify.windows.com`, Apple `web.push.apple.com`) were considered as an allowlist, with a config override.
  Rejected for now. Once nothing internal can be dialled and no redirect is followed, all an attacker has
  left is making the server POST an encrypted, VAPID-signed body to a **public** https host of their
  choosing, which they can do from their own machine. An allowlist would break silently for any browser
  whose push service is not on it, and every such breakage would need an operator to edit config.
- **Hardening done with it:**
  - **Push service answers as plain text.** The endpoint's error body (read up to 512 bytes) goes into the
    job's `last_error`, which maintainers read through `/jobs` and `kukatko ctl`. It is untrusted text from
    an endpoint the subscriber chose. `plainText` now replaces invalid UTF-8, turns every non-printable rune
    (escape sequences, newlines, bidi overrides) into a space, collapses whitespace and cuts the text at 200
    runes with `…`.
  - **Re-subscribing a known endpoint still moves it to the caller (decision, kept).** The endpoint is an
    unguessable capability URL. Only the browser, its push service and this table know it, and no API
    returns another account's endpoint (`GET /push/subscriptions` lists the caller's own rows), so whoever
    presents it is that browser. Refusing the move would keep delivering the previous account's
    notifications to a shared browser that someone else is now signed in on. The reasoning is in
    `upsertSubscriptionSQL`'s comment.
- **Regression tests:** `internal/push/destination_test.go`:
  - `TestVAPIDSender_doesNotFollowRedirects`: 301/302/303/307/308 → `ErrRejected`, the internal recorder
    gets 0 requests, and the caller's client is left unchanged.
  - `TestVAPIDSender_defaultClientRefusesNonPublic`: the production client to loopback by IP literal and by
    `localhost` → permanent `ErrRejected`, and the server receives nothing.
  - `TestIsPublicAddr`, `TestRefuseNonPublic`, `TestCheckEndpointHost`, `TestNewDefaultClient` and
    `TestPlainText`.
  - `TestValidateSubscription_formatOnly` pins that the format check alone still accepts the internal
    endpoints.
  - `TestVAPIDSender_roundTrip` still shows a normal endpoint working.
  - `TestStore_upsertRefusesInvalid` (integration) stores none of the internal endpoints.
- **Not addressed here:** a failed send's transport error names the endpoint URL in `last_error`, so a
  **maintainer** can read other accounts' endpoints through `/jobs`. Maintainers already hold the database
  and the backups, so this is not a new capability.
- **Fix commit:** `be84566` (2026-10-09).

---

### SEC-021 — MEDIUM — **OPEN (decision pending)** — Account recovery revokes sessions only: a planted API token or passkey survives it

- **Origin:** an unconfirmed lead from a source-only audit (2026-10-06, commit `3792f7c`), confirmed on
  **2026-10-09** against commit `f8b7bc0`. This entry only documents and evaluates. Auth behaviour is
  **unchanged**.
- **Where:** there are three ways to replace a password, and all three delete only `sessions` rows:
  - `Service.ChangePassword` (`internal/auth/service.go`, `POST /auth/password`) deletes every session
    except the caller's.
  - `Service.ResetPasswordAudited` (`internal/auth/service_admin.go`,
    `POST /admin/users/{uid}/password`) deletes every session.
  - `consumePasswordReset` (`internal/auth/store_passwordreset.go`, `POST /auth/password-reset/{token}`)
    deletes every session in the link's transaction.

  None of them touches `api_tokens` or `passkey_credentials`. A token keeps passing
  `AuthenticateAPIToken`, which checks only the token's own expiry/revocation and `users.disabled`. A
  passkey keeps minting fresh sessions through `/auth/passkeys/login/*`.
- **Why a planted credential is cheap:** `POST /auth/tokens` needs only `RequireAuth`. It asks for no
  password, and a token without `expires_at` never expires. `RequireAuth` accepts a bearer token as well as
  a session, so **a token alone mints further tokens and registers a passkey**: the ceremony state rides in
  a cookie, which any HTTP client keeps. That passkey then produces an ordinary session cookie.
- **What the victim and an admin can do today:**
  - The account owner lists and revokes their own tokens (`GET`/`DELETE /auth/tokens`) and passkeys
    (`GET`/`DELETE /auth/passkeys`).
  - An admin can revoke **any** token by id (`RevokeAPIToken`), but no route lists another user's tokens, so
    the id is unknown in practice.
  - An admin cannot delete another user's passkey: `Passkeys.Delete` answers `ErrPasskeyNotFound` to a
    foreign owner.
  - Disabling the account stops both credentials (`users.disabled` is checked on both), but it also locks
    out the rightful owner.
- **Attack scenario (persistence after account recovery):**
  1. Someone holds the victim's principal briefly: a stolen session cookie, an unattended signed-in
     browser, or a leaked API token.
  2. In one request they mint a non-expiring token. In two more they register a passkey on their own
     authenticator.
  3. The victim notices and recovers the account the documented way: they change their password, or an
     admin sets one or issues a reset link.
  4. Every session dies, and the account looks recovered. The token still answers `GET /auth/me` with
     200, and the passkey still signs in. Access keeps the victim's role (an admin's, for an admin
     account) indefinitely, until somebody thinks to open the token and passkey lists.

  The precondition is a prior compromise, which is why this is MEDIUM and not HIGH. The impact is that the
  recovery control fails silently: it reports success and leaves the intruder in.
- **Confirmed by:** `internal/auth/recovery_revocation_integration_test.go`. The tests are kept and pin the
  current behaviour, so a fix has to change them deliberately.
  - `TestRecovery_leavesAPITokensAlive`, for each of the three paths: mint a token from a second
    ("stolen") session, recover, then call `GET /auth/me`. The stolen session gets **401**, and the token
    gets **200**, with its row still unrevoked.
  - `TestRecovery_leavesPasskeysAlive`, for each path: register a passkey (virtual authenticator) from the
    stolen session, recover, then sign in with the passkey from an empty browser. The sign-in returns
    **200**, and the new session gets **200** on `/auth/me`.
  - `TestRecovery_bearerTokenAloneMintsTokenAndPasskey`: with every session deleted and only a bearer
    secret, the holder mints a second working token and registers a passkey whose login yields a session.
- **Fix options:**
  1. **Revoke everything on recovery, in the same transaction.** All three paths stamp `revoked_at` on
     every live token and delete every passkey, audited with the counts. This is the simplest option and
     cannot be forgotten. Its cost: the self-service change also kills legitimate automation (every
     `kukatko ctl` context, the curating agent's included) and the owner's own passkeys, so people learn to avoid changing their password. On the admin and reset-link paths that
     cost is right, because those are the paths used when control was lost.
  2. **Make the credentials visible instead of revoking them.** After a self-service change, show the
     account's tokens and passkeys with last-used times and a "revoke all" button. Add admin routes that
     list and delete **another user's** tokens and passkeys (`GET /admin/users/{uid}/tokens`,
     `…/passkeys`, plus `DELETE`), audited, so an admin can clean up selectively. Nothing legitimate breaks,
     but it relies on a person noticing an unfamiliar row. The admin routes are needed anyway, because
     today an admin cannot remove a passkey at all.
  3. **Make planting harder: re-authentication before minting.** Require the current password (or a fresh
     passkey assertion) on `POST /auth/tokens` and `/auth/passkeys/register/*`, or at least refuse both to
     a **bearer** principal. This closes step 2 of the scenario for a stolen cookie or token, and costs
     one password prompt for a rare action. Nothing in the repo mints tokens or registers passkeys with a
     bearer token: `kukatko ctl` only uses a token that was created in the browser. It does not help when
     the planting already happened, so it complements 1 or 2 and does not replace them.
- **Recommendation:**
  - **1** for the two "lost control" paths: an admin set and a consumed reset link revoke all tokens and
    delete all passkeys in their existing transaction, and the response or audit entry reports the counts.
  - For the self-service change, a **"also revoke all API tokens and passkeys"** choice, defaulting
    **on**, so a deliberate owner can keep their automation.
  - **3** for bearer principals (no token minting or passkey registration from a token), which is
    cheap.
  - The **admin list and delete routes from 2** as follow-up work.

  The decision belongs to the product owner. Until it is made, `docs/ARCHITECTURE.md` §11 states that
  recovery revokes sessions only.

---

### SEC-022 — MEDIUM — **OPEN (decision pending)** — An expiring API token mints a non-expiring token and registers a passkey

- **Origin:** an unconfirmed lead from a source-only audit (2026-10-06, commit `3792f7c`), confirmed on
  **2026-10-09** against commit `656a774`. This entry only documents and evaluates. Auth behaviour is
  **unchanged**. It shares its cause and one fix with SEC-021, which covers the same writes from the angle of
  account recovery.
- **Where:**
  - `authenticateRequest` (`internal/auth/middleware.go`) turns a valid bearer token into a `principal` that
    holds the owner's `User`, no `Session`, and the token's `unlimited` flag. It does not keep the token's id
    or its `expires_at`. Downstream, a token principal is the owner in full; only the missing session tells it
    apart from a browser.
  - The credential-management routes need only `RequireAuth` (`internal/auth/routes.go`): `POST /auth/tokens`,
    `PATCH /auth/tokens/{id}`, `POST /auth/passkeys/register/begin` and `…/register/finish`.
  - `handleCreateAPIToken` (`internal/auth/handlers_apitoken.go`) passes the body to `Service.CreateAPIToken`
    (`internal/auth/service_apitoken.go`). That accepts a nil `ExpiresAt` ("never") from any principal and
    checks a given expiry only against *now*, never against the presenting credential. `unlimited` is checked
    against the owner's role, not against the presenting token.
  - Tokens have no parent: the `api_tokens` row records nobody but the owner, so revoking or expiring a token
    reaches no token it minted.
- **Confirmed behaviour:**
  - Token A with `expires_at = now + 1 h` mints token B with no `expires_at`, or with one a year after A's.
    Both are 201.
  - Once A has expired (clock moved past it) or been revoked by the owner, A gets **401** and B still gets
    **200** on `GET /auth/me`. B is the only live token left.
  - A alone completes a passkey registration. The ceremony cookie rides in any HTTP client's jar. After A
    expires, that passkey signs in from an empty browser and yields an ordinary session cookie.
  - An admin's **throttled**, expiring token mints an **unlimited**, never-expiring child.
  - The role never grows. What is lost is the presenting credential's **bounded lifetime** and its
    **single point of revocation**: an expiry or a revoke on A no longer ends what A's holder can do.
- **Realistic threat:**
  - **A leaked agent token.** The documented MCP setup (`docs/MCP.md`) and every `kukatko ctl` context
    keep a bearer secret in a config file, an MCP client entry, or an environment variable. An owner who
    deliberately gives such a token a short life ("one hour for this job"), or revokes it when it shows up in
    a log, a paste, or a backup, reasonably believes the exposure is now over.
  - Whoever read the secret in that window makes one request to `POST /auth/tokens` with no `expires_at`, or
    three to plant a passkey, and keeps the owner's role indefinitely. For an `ai`, `editor` or `admin`
    account that means write access, and for an admin account also user management.
  - **A prompt-injected or otherwise misbehaving agent** holding its own token can do the same thing without
    anyone leaking anything. The token is meant to be the agent's leash, and it can lengthen the leash itself.
    MCP exposes no credential tool, but `kukatko ctl` is driven through a shell, and an agent with a shell
    can call `curl` with the token it already holds.
  - The owner can still find the child: it appears in their own `GET /auth/tokens`, the passkey appears in
    `GET /auth/passkeys`, and each mint writes an `api_token.create` or `passkey.register` audit row.
    Nothing prompts them to look, and an admin cannot list another user's tokens or delete their passkeys
    (see SEC-021).
  - The precondition is a leaked or misused token, and the role does not increase, so this is MEDIUM, in line
    with SEC-021.
- **Confirmed by:** `internal/auth/token_minting_integration_test.go`. The tests are kept and pin the current
  behaviour, so a fix has to change them deliberately.
  - `TestTokenMinting_expiringTokenMintsLongerLivedToken`: a 1 h token mints a child stored with
    `expires_at IS NULL`, and another that outlives it by a year.
  - `TestTokenMinting_childOutlivesExpiredParent`: after the frozen clock passes the parent's expiry, the
    parent gets 401 and the child 200.
  - `TestTokenMinting_childOutlivesRevokedParent`: the owner revokes the parent from their browser; the parent
    gets 401, the child 200, and one unrevoked token is left.
  - `TestTokenMinting_expiringTokenRegistersPasskey`: a 1 h token registers a passkey (virtual
    authenticator). After the token expires, the passkey signs in and its session gets 200.
  - `TestTokenMinting_adminTokenMintsUnlimitedChild`: an admin's throttled 1 h token mints an unlimited,
    never-expiring child.
  - `TestRecovery_bearerTokenAloneMintsTokenAndPasskey` (SEC-021) shows the same writes with a non-expiring
    token and no session at all.
- **Who mints tokens or registers passkeys today, and how:**
  - **`kukatko ctl`** never calls `/auth/tokens` or `/auth/passkeys/*`. It only presents a token. Its 401
    message (`internal/ctl/client.go`, `UnauthorizedError`) tells the user to create a new one "while logged
    in", which means from a session.
  - **The MCP agent token** (`docs/MCP.md`, "A token for the agent"; `docs/OPERATIONS.md`, `mcp.*`) is minted
    by logging the agent user in with a password (`POST /auth/login`, cookie jar) and then calling
    `POST /auth/tokens` with that cookie. That is a session principal.
  - **The web UI** (`components/account/ApiTokensCard.tsx` and `PasskeysCard.tsx` on the account page, over
    `web/src/services/auth.ts` and `passkeys.ts`) runs on the session cookie.
  - **`docs/API.md`** describes tokens as "long-lived bearer credentials for non-interactive clients" and
    documents no flow that mints a token with a token. Nothing in the repo, and nothing in the curating
    agent's workspace (`~/projects/fotky`), does so.
  - Passkey registration from a token has **no legitimate use at all**: a passkey is created by an
    authenticator in a browser, which already has a session.
- **Fix options:**
  1. **Session only for credential management.** `POST /auth/tokens`, `PATCH /auth/tokens/{id}` and
     `/auth/passkeys/register/*` answer **403** to a token principal (`session == nil`, which the principal
     already reveals; a small `requireSession` middleware beside `RequireAuth`). Revocation
     (`DELETE /auth/tokens/{id}`) stays open to tokens, because it only ever reduces access and a script that
     retires its own token is legitimate. `DELETE /auth/passkeys/{id}` has no non-interactive use either and
     can go session-only with the rest.
     - *For:* small, needs no schema change, and closes the whole class: no child token, no passkey, no
       unlimited flag from a bearer. It is exactly the bearer half of SEC-021's option 3, so one change fixes
       both. It breaks nothing in the repo or in any documented workflow.
     - *Against:* there is no programmatic rotation. A headless client that wants a fresh token has to log in
       with a password. That is what the documentation already prescribes, and a client that can log in with a
       password is not bounded by its token anyway.
  2. **Cap a child at its parent.** The principal carries the token's id and `expires_at`. A child minted by
     a token gets at most the parent's expiry: a nil or later value is clamped, or refused with 400. It may be
     unlimited only if the parent is. To make revocation reach it as well, `api_tokens` gains a
     `parent_id` (FK, `ON DELETE CASCADE`), and revoking a token revokes its descendants in the same
     transaction.
     - *For:* keeps self-rotation and short-lived child tokens for sub-tasks, with the parent's bounds
       inherited.
     - *Against:* a migration, a recursive revoke, and clamp-or-refuse semantics to document. It still leaves
       passkey registration open, because a passkey has no expiry to cap, so registration must be refused to
       tokens anyway (option 1's passkey half). A never-expiring parent caps nothing. That covers the common
       agent token minted without `expires_at` (as `docs/MCP.md` shows), so the leaked-agent case is not
       improved.
  3. **Let the token's creator opt in.** A per-token flag such as `can_manage_credentials` (default off, set
     only from a session at mint time, shown in the token list, audited). Without it the token is refused as
     in option 1; with it, today's behaviour.
     - *For:* the most flexible, and the default is safe.
     - *Against:* a column, a UI control, API and audit surface, and a decision every token creator has to
       understand. The tokens most likely to get the flag are the agent tokens, which are the most likely to
       leak, and for them the flag restores exactly the behaviour this entry describes. No current workflow
       needs it.
- **Impact on `kukatko ctl` and documented workflows:** none for option 1. `ctl` mints nothing, the MCP setup
  and the web UI mint from a session, and `ctl`'s 401 hint already says "while logged in". The only docs to
  touch are `docs/API.md` (the 403 for a token principal on the three routes) and the `UnauthorizedError` text,
  which could say "from the web UI or a password login" more explicitly. Option 2 adds a `parent_id` field to
  the token JSON and a revocation cascade to describe. Option 3 adds a create-time field and a UI toggle.
- **Recommendation:** **option 1**, landed together with SEC-021's option 3, since it is the same change:
  - Token minting, the unlimited toggle, passkey registration and passkey deletion become session-only, and
    answer 403 to a bearer principal.
  - Token revocation stays open to tokens.
  - The tests above flip to assert 403 and the parent's bounds.

  Option 2 is worth building only if a real need for programmatic rotation appears, and then on top of option
  1's passkey refusal, not instead of it. Option 3 is not recommended: it adds surface whose only effect, when
  switched on, is to restore the risk. The decision belongs to the product owner. Until it is made,
  `docs/ARCHITECTURE.md` §11 states that a token principal is the owner, not the token.

---

## Areas checked — no finding ("reviewed, no findings")

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
  (`/admin/users`, `/audit`) by `RequireAdmin` (admin or maintainer via the ladder — note the sibling
  read `GET /audit/mine` is `RequireAuth` by design: it is a **separate route** whose handler overwrites
  the actor filter with the session's user unconditionally, so it cannot serve another user's rows,
  and a `?user=` naming somebody else is refused with 403 rather than silently narrowed); the
  permanent trash operations (`POST /trash/empty` and the per-photo `POST /photos/{uid}/purge`)
  tightened from write to `RequireAdmin` because they destroy originals irreversibly — the
  reversible archive (soft delete) stays `RequireWrite` and `GET /trash/info` stays `RequireAuth`;
  media by `RequireAuthOrDownloadToken`. No under-guarded mutating route; a nil middleware would
  panic at wiring, not silently pass. The ladder's top is additionally **irreversibility-guarded**:
  a change that would leave zero enabled maintainers is refused with 409 (SEC-016), because that
  state cannot be undone through the API. No editor/viewer can reach a governance or operations
  surface, and a plain admin cannot reach an operations surface; no viewer can reach a mutation.
  The only unauthenticated routes are `/healthz`, `/metrics` (SEC-013), and the SPA static
  handler. No `pprof`/`expvar`/`/debug` endpoints exist.
- **Auth primitives — clean.** bcrypt cost **12** (`internal/auth/password.go`, `bcryptCost`).
  The work factor `HashPassword` mints at is selected by build tag: `password_cost.go` pins it to
  12 in every build that is not the integration-test build, and the cheaper test cost lives in
  `password_cost_integration.go`, which a shipped binary does not compile at all — it is not a
  variable an importer could lower. `TestHashPassword_productionCost` (in `make test`) fails if a
  tagless build ever mints below 12. Session and
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
  `nil` details; token entries store only the token *name*). (The audit `ip` field was forgeable
  — folded into SEC-001, fixed there.)
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
- **Upload-link codes are stored readable — accepted decision (2026-10).** Migration `0091` keeps an
  upload link's short code in `upload_links.code` next to its SHA-256 (`code_hash`), dropping the
  original "plaintext only in the create response" rule so a curator can copy the URL again. Same
  trade as the registration secret in `instance_settings`: the code opens one time-boxed upload
  drop-box into preset albums/labels — no read access to the library, no account — and is shown only
  to whoever manages the link (creator or admin; the listing omits the key for anybody else, and only
  lists a curator's own links anyway). A database or backup leak therefore exposes live links' codes;
  the remedy is revoke or "new code", which replaces both columns and kills the old URL. Lookup still
  goes through the hash. "Restore code" for pre-0091 rows stores a code only when it hashes to the
  stored `code_hash` (constant-time compare), so it cannot be used to plant a chosen code, and it is
  open only to the link's manager, who could rotate the link anyway — no extra rate limit.
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
