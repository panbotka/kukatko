# Backend packages

A descriptive reference overview of the Go packages. **These are not rules** — the rules live
in [`CLAUDE.md`](../CLAUDE.md). Record a new or changed package here and add one line for it
to `## Package map` in `CLAUDE.md`.

<!-- BODY BEGIN -->
- **Layout:** `cmd/kukatko/` (thin Cobra entrypoint: root + `serve` + `migrate` + `version`; the
  serve wiring splits the API groups by where they mount: `buildServices`, `discoveryAPIOptions`,
  `readAPIOptions` and `appendOpsAPIs` fill one `projectAPI` struct with every **project-scoped** group
  (`projectapi.go`) and return the **instance-wide** ones as `server.WithAPI` options; `requestOptions`
  adds `projectRoutes`, which mounts the `projectAPI` once through `mountProjectAPI`, under
  `/api/v1/p/{project}` behind `auth.WithProject` — there is no unprefixed copy and no outer
  `WithProject`, so a project-scoped path without the prefix is the API's 404
  (`TestProjectRoutes_mountsTheAPIOnlyUnderTheProject`, `TestProjectAPI_onlyThePrefixServesAProject`). A
  package that serves both halves exposes them apart (`RegisterInstanceRoutes`/`RegisterProjectRoutes`
  in `auth`, `maintenanceapi`, `userpicapi`, …). The request-project shim (`projectshim.go`) is gone: candidates, outliers, sweep,
  review and the cluster assignment pass the project themselves since step 14, and expand — its last
  consumer — since step 15 hands `vectors.Store.FindSimilar` the project it was given. A package with
  routes on both sides exposes the halves separately:
  `auth` `RegisterInstanceRoutes`/`RegisterDirectoryRoutes`, `notificationapi`
  `RegisterPushRoutes`/`RegisterInboxRoutes`, `uploadlinkapi` `RegisterManagementRoutes`/
  `RegisterPublicRoutes`, `settingsapi` `RegisterRoutes`/`RegisterInstanceRoutes`, `buildIngest` an
  `ingestRoutes` triple),
  `internal/server/` (chi HTTP server, graceful shutdown, `WithTrustedProxies` →
  `clientip.Middleware` in place of chi's `middleware.RealIP`; the SPA catch-all
  `router.NotFound(web.Handler())` is carved out for **`/.well-known`** and **`/.well-known/*`**,
  which answer a JSON **404** via `handleNotFound` — the application publishes nothing under that
  prefix (ACME is terminated by the reverse proxy, the PWA manifest is `/manifest.webmanifest`), and
  a client probing for `oauth-protected-resource` metadata has to get "nothing here" rather than
  `index.html` with a 200, which it would try to parse as JSON; likewise the `/api/v1` group has its own
  `NotFound(handleNotFound)`, so an unmatched API path — a project-scoped route without its
  `/p/{project}` prefix among them — is the API's JSON 404, never the SPA (`TestUnknownAPIPathIsTheAPI404`)),
  `internal/version/`
  (ldflags-injectable `Version`/`Commit`), `internal/config/` (typed configuration,
  Viper, `Load()`), `internal/database/` (pgxpool wrapper `DB` with `Ping`/`Close`/`Pool`,
  embedded migration runner `Migrate` — each file in its own transaction, logged as
  `migration applied file=… duration_ms=…` when it commits, the per-file wall time the upgrade
  outage estimate in `docs/MIGRATION_SAFETY.md` is measured from; the whole run holds one fixed session
  `pg_advisory_lock` on a dedicated connection, so concurrent migrators (a `serve` start + a CLI
  subcommand) serialise and the second finds nothing to do — unbounded wait, no `lock_timeout`, on purpose
  (a timed-out start would restart-loop); `Pending` lists the not-yet-applied files read-only and
  `SchemaStatus` (`SchemaStatusOf`) adds the highest applied (`Current`) and embedded (`Expected`) version,
  which the CLI's `requireCurrentSchema`/`openCurrentDB` use to **refuse** every subcommand but
  `migrate`/`serve`/`maintenance inventory` on a database that is behind (`errSchemaBehind`) —
  **every connection announces `application_name` `kukatko/<version> <instance>`** (`ApplicationName`, one
  random 8-hex instance token per process, version sanitised to ASCII and the whole name kept within the
  server's 63 bytes; it overrides a DSN's `application_name`), and `OtherClients` reads `pg_stat_activity`
  for the client sessions on the same database that are not this process's — any `kukatko/…` name (any user)
  plus any other session of the same user (how 0.31.0 and older, which announce nothing, show up) — which
  `migrate` refuses on and `serve` logs at `ERROR` before migrating a database that is behind —
  pgvector types registered on every connection,
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
  (integration test harness: `dbtest.New(t)`, `dbtest.TruncateAll`), `internal/apitest/`
  (**the HTTP client the API packages' tests drive their `httptest` servers with**:
  `apitest.Client()` returns one shared client whose transport pools nothing.
  `http.DefaultClient` is the wrong one, and a `forbidigo` rule in `.golangci.yml` now says so:
  `httptest.Server.Close()` empties `http.DefaultTransport`'s idle connections on behalf of its
  users, and that transport is process-wide, so in a package of parallel tests one test's teardown
  evicts the connection another test is reusing — the request in flight then fails with
  `net/http: HTTP/1.x transport connection broken: http: CloseIdleConnections called`. A POST is
  what surfaces it, since net/http silently replays only an idempotent request. Which test loses
  the race is arbitrary, which is why it moves between runs and reads as someone else's
  regression), `internal/auth/`
  (authentication/authorization on **two ladders**: the instance `Role` (`user`/`maintainer`, `users.role`;
  `CanMaintain`/`CanImport`) and the per-project `project.Role` of the caller's membership, which every
  project-level guard reads. `role.go` also holds `AccountRole`, which **survives only as the seeding
  vocabulary** of `CreateUserInput`/`Service.CreateUser` (the five rungs viewer…maintainer; `Split` →
  instance role + a membership of the bootstrap project, never an authorization decision). Its callers
  are `Bootstrap` (the first maintainer) and the integration-test seeders of ~100 test files, which need
  "a curator of the library" in one line — rewriting them all buys nothing, so it stays for them. No HTTP
  route accepts it and no account read returns it. Also the pure `authorize(instance, projectRole, req)`. **`project.go`**: `WithProject(store)` names the request's
  project (chi param `{project}`; a request without it names no project — there is no default), the
  guards' `enterProject` decides access after authentication (a project-scoped guard on a request that
  names no project → 500 with the route logged, a wiring bug) (unknown/foreign/deactivated → identical 404, unapproved → 403
  `pending_approval`, maintainer → `admin` + `obs.SetActorRole("maintainer")`), clears a withheld
  membership's `identity_withheld` on its first entry (`revealIdentity` → `project.Store.RevealIdentity`;
  a failure is logged, never a refusal), throttles
  `TouchLastProject` to once a minute per account, and the principal carries `project` + `projectRole` +
  `projectSubject` (the membership's `subject_uid`, read for a maintainer too when it has a membership;
  **`LinkedSubjectFromContext(ctx)`** is what `person:me`/`family:me` resolve to).
  `WithProject` also records the prefix the request came through: **`APIBase(ctx)`** (`apibase.go`) is
  `/api/v1/p/<slug>` under the project prefix and `APIBasePath` (`/api/v1`) on an instance-wide route
  or a router without `WithProject`; every handler that links to another
  project-scoped route builds the link on it (`ContextWithAPIBase` sets it in tests).
  `ProjectFromContext`/`ProjectUIDFromContext`/`ProjectRoleFromContext` are the **only** way a handler
  or store learns the current project. `RequireSignedIn` (every `/auth/*` route but the project's
  `PUT /auth/subject`, and `/push/*`, `/users/{uid}/avatar`) authenticates and enters no project;
  `RequireMaintainer`/`RequireImport` on a route that names no project check the instance role alone. A
  `User` is the **account record and nothing project-shaped**: `Role` (instance, JSON `instance_role`),
  profile, `Disabled`, the derived `ApprovedAt`; `userColumns` reads no membership but the approval (the
  `ProjectRole`/`AccountRole` fields and the bootstrap subqueries are gone). Its unexported `join` is the
  membership a new account is **created with** — set by `CreateUserInput` (bootstrap project), by
  registration (the registered project) and by the seeding builder **`User.InProject(projectUID, role)`**
  for a hand-built `Store.CreateUser`; empty means no membership. `NewAccountInput` +
  `Service.CreateUserAudited` (the `POST /admin/users` path) create an account **with no membership**, and
  `UpdateUserInput` (`InstanceRole`, no subject) writes the `users` row only — no `/admin/users` write
  touches `project_members`. `sessions.role` is gone (0103). Bcrypt cost 12
  `HashPassword`/`CheckPassword` — the cost is a **build-tag-selected** identifier
  (`password_cost.go` = production 12, `password_cost_integration.go` = `bcrypt.MinCost` and the
  `KUKATKO_TEST_BCRYPT_COST` override, compiled only under the `integration` tag), so no running
  server can lower it and `TestHashPassword_productionCost` in `make test` fails if the default
  moves; UID/token generators, sliding-window `Limiter`,
  `Store` over pgx, `Service` orchestrating login/session/bootstrap/user management,
  `API` = HTTP handlers + RBAC middleware
  `RequireAuth`/`RequireCurator`/`RequireWrite`/`RequireAdmin`/`RequireMaintainer`/`RequireImport` +
  `RegisterRoutes`; sessions and users in migration `0002_auth.sql`.
  **`directory.go`** is the one all-authenticated view of accounts: `DirectoryEntry{UID,Name}`,
  `Store.Directory(ctx, projectUID)`/`Service.Directory` and `GET /p/{project}/users`, listing the
  project's **approved members** whose account is not disabled and whose identity is not withheld (an
  exact-match add that has not entered yet; a `project_members` join; an empty uid is
  `ErrNoProject`) by the name each is shown under — never an account of another project. It exists because naming a person is now something an ordinary user does —
  putting somebody on a task is how a question reaches the relative who would know — and it is a type and
  a query of its own rather than a trimmed `User`: the address, the role, the approval state and the hash
  never leave the database, so no handler can forget to strip them.
  **Every 401 the package writes goes through `writeUnauthorized(w, message)`**, which sets
  `WWW-Authenticate: `+the exported `auth.Challenge` (`Bearer`) before the usual JSON error body, so
  no guard or handler can forget the header RFC 9110 requires there. The scheme is `Bearer` and not
  `Basic` on purpose: `Basic` makes a browser pop its own sign-in dialog over the SPA, which handles
  its 401s itself. It also matters to a non-browser client — an MCP client that finds no challenge
  starts guessing OAuth metadata under `/.well-known/…` instead of reporting that it needs a token
  (see `docs/MCP.md`); `Challenge` is where `resource_metadata="…"`/`scope="…"` would be appended if
  Kukátko ever grows its own OAuth.
  **The linked person** ("this is me") lives on the membership, `project_members.subject_uid` (migration
  0101; the account-wide `users.subject_uid` of 0060 was dropped by 0110): nullable, **not** unique
  (several accounts may legitimately be the same person), FK `project_members_subject_uid_fkey` →
  `subjects(uid)` **ON DELETE SET NULL** — named explicitly because `kukatko maintenance reset` drops and
  restores it around its truncation (see `internal/reset`). `auth.User` carries **no** subject: `GET
  /auth/me` lists every membership's own (`projects[].subject_uid`, `project.MembershipSummary.SubjectUID`)
  and the frontend reads the current project's from there; the system administrator's `GET /users` shows
  it per membership (`AllMemberships` resolves `subject_name`). `Store.SetUserSubject(ctx, projectUID,
  uid, subject)` is the self-service write behind `PUT /auth/subject` (outside the maintainer guard: the
  link carries no permission): it writes the membership of `projectUID` and returns a **`LinkedUser`**
  (the account + `SubjectUID`, JSON `subject_uid`; nil when it is no member there — nothing is written).
  `/admin/users` no longer writes the link (a `subject_uid` in its bodies is an unknown field → 400); the
  seeding `CreateUserInput.SubjectUID` links the bootstrap membership. Every
  writer first runs `checkSubject`: a UID naming no person **of that project** (unknown, or another
  project's) is `ErrSubjectNotFound` (→ 400); a FK violation (SQLSTATE 23503) maps to the same error.
  **Approval and the welcome.** `users.welcome_seen_at` (migration `0064_users_approval_welcome.sql`)
  is when the owner last dismissed the first-run welcome (`NULL` = never). The account's **approval**
  is no column any more (0110 dropped `users.approved_at`): `accountApprovedSQL` derives it — the
  earliest `approved_at` of the account's memberships, or `created_at` for a maintainer with none
  approved (a maintainer enters every project) and for an account in **no** project (waiting for nobody:
  it may sign in, sees an empty chooser and can accept an invitation) — and `userColumns` carries it as `approved_at`, so
  `auth.User.ApprovedAt` (nil = registered, waiting; sign-in answers `ErrNotApproved`) and the
  `?pending=` filter of the admin listing read the memberships. A new account's membership is written
  with an explicit stamp: `insertUserQuery`/`insertUserArgs` (shared by the plain and audited insert
  paths) pass `User.ApprovedAt`, which `Service.prepareNewUser` fills from the service clock via
  `approvedNow` — its callers are the admin API and the bootstrap, and creating an account is the
  approval. The one path that must not keep the stamp is `Service.prepareRegistration`, which wraps
  `prepareNewUser` and strips it again, so every rule about usernames, addresses and passwords stays in
  one place.
  Approval is **not** the inverse of `disabled` and no read may collapse them; both are carried on
  `auth.User` (`ApprovedAt`/`WelcomeSeenAt *time.Time`, serialised) and shown by the admin roster.
  `Store.MarkWelcomeSeen` is the self-service write behind `POST /auth/welcome-seen`: its
  `COALESCE(welcome_seen_at, $2)` makes it idempotent (the stamp lands only while the column is still
  NULL, so a repeat call can never move it backwards) and, like `SetUserSubject`, it leaves `updated_at`
  alone and records no audit entry.
  **Self-service registration** (`register.go`, `handlers_register.go`, `store_register.go`) is the
  flow behind `POST /auth/register`: somebody who knows a project's shared secret creates their own
  account, an unapproved viewer of that project, which exists but cannot be used until a project admin
  approves the membership. The secret path is **per project**: the body's `project` (the slug the
  registration page `/p/<slug>/register` sends) is resolved by the handler (`registrationProject`; an
  empty, unknown or deactivated slug = `ErrRegistrationClosed`) into
  `RegisterInput.ProjectUID`, whose switch and secret `checkSecret` reads — so one project's secret posted
  with another's slug is the same `ErrRegistrationSecret` a made-up one gets. It is a separate type,
  `Registration` (built with `NewRegistration`, wired into `APIConfig.Registration`), rather than more
  methods on `Service`, because it is the only part of `auth` that consults the instance settings and
  sends mail — both injected as narrow interfaces (`SettingsSource`, satisfied by `*settings.Store`;
  `MailScheduler`, satisfied by `*mailjob.Enqueuer`) — and because an API built without one simply
  answers the closed-door 403 (`ErrRegistrationClosed`), which is what an instance with
  registration switched off answers too, so a client never has to tell a missing route from a closed
  door. `checkSecret` reads `settings.Settings`, refuses outright when registration is disabled **or**
  the stored secret is blank (an open door with no lock), and otherwise compares with
  `crypto/subtle.ConstantTimeCompare` on both trimmed values → `ErrRegistrationSecret` (over HTTP the same
  403 and message as `ErrRegistrationClosed`, `registrationRefused`, so the answer never tells an open door from a closed one).
  `RegisterWithLink(ctx, in, LinkGrant, entry)` is the other gate: an upload link the handler admitted
  through the optional `UploadLinkGate` (`APIConfig.UploadLinks`, implemented by
  `uploadlinkapi.RegistrationGate`; `RegisterInput.UploadLink` / the body's `upload_link`) replaces the
  secret — registration must still be enabled, a dead or unknown link is `ErrRegistrationLink` — and its
  `Claim` runs inside the same `alongside` transaction (claiming the photos the browser uploaded), its
  `Committed` after it. **Everything of a link registration happens in the link's project**
  (`LinkGrant.ProjectUID`, empty = bootstrap): its settings decide whether registration is open, the new
  account's membership is created there only (the unexported `User.joinProject`, read by
  `insertUserArgs`/`projectToJoin`), the entry's `ProjectUID` names it and `ListApprovalRecipients(ctx,
  projectUID)` picks its admins (plus the maintainers) to notify.
  `RegisterWithInvite(ctx, in, InviteGrant, entry)` is the third gate: an invitation admitted through
  the optional `InviteGate` (`APIConfig.Invites`, implemented by `inviteapi.RegistrationGate`;
  `RegisterInput.InviteCode` / the body's `invite_code`, which wins over a link) replaces the secret
  **and the approval** and ignores the registration switch: the account is built by `prepareRegistration`
  with `grant.Role` (approval stamped back to now) — capped at editor again here, defence in depth — joins
  `grant.ProjectUID` only, and `grant.Redeem` runs in the `alongside` transaction (the invite's use and
  its `project.invite.accept` entry; an invite that died meanwhile rolls the account back). No mail, no
  approver notices; a dead invite is `ErrRegistrationInvite`. `withRegistrationDetails` stamps the
  username/role details all three paths share. The account is
  built by `prepareRegistration` (a `user`, a `viewer` membership of the project, `ApprovedAt` nil) and
  written by
  `Store.CreateUserAuditedWith` — `CreateUserAudited` plus an `alongside` hook that runs on the **same
  transaction**, which is how the mails are enqueued: a rolled-back registration schedules
  nothing. `Store.ListApprovalRecipients` reads the enabled admins and maintainers to notify (no
  filtering on the address: skipping a `.invalid` placeholder is `internal/mailjob`'s single decision);
  reading them fails soft, and so does an individual notice that will not schedule — the person's own
  confirmation is the only mail whose failure fails the registration. **The administrators are notified
  through `internal/notifydeliver`** (`notifyApprovers`, same transaction, after the confirmation): when
  `RegistrationConfig.Notify` (`Notifier`, satisfied by `*notifydeliver.Deliverer`) is set, every
  recipient gets one `Deliver` of `KindRegistrationPending` — the Czech
  `notification.RenderRegistrationPending` text naming the newcomer (display name, else username), link
  `ApprovalPath` = `/members` (stored under the project: `/p/<slug>/members`, the project's member screen)
  — through **exactly the channels they opted into** (in-app, push, e-mail; **all off by default**). The
  e-mail is the detailed `new_registration_pending` template naming the project (`MailContext.ProjectName`)
  and ending with the absolute approval link (`MailContext.URL` = `mail.base_url` + `/p/<slug>/members`;
  no base URL leaves the link out). The person's own `registration_received` confirmation names the
  project too, read through the optional `RegistrationConfig.Projects` (`ProjectReader`, `*project.Store`;
  a failed read names none and never refuses the registration). Each recipient runs under its own **savepoint**, because a failed statement
  would otherwise abort the whole registration: a delivery that will not record or schedule is rolled back
  to it **whole** (no mail without its record, no record without its push), logged, and the registration
  goes on. `Notify` nil means only the person is mailed. The audit entry
  (`audit.ActionUserRegister`, `user.register`) names the new account as both actor and target and is
  written in that same transaction. `perAddressLimiter` derives the endpoint's per-address
  `ratelimit.Limiter` from the login budget when the caller supplies none (shared with the
  password-reset endpoints as `perAddressLimiter`), and `RegisterRoutes` mounts it as middleware on the
  route — one of the two writes an anonymous caller may perform.
  **Approving a waiting account** is no longer here: the instance-wide `POST /admin/users/{uid}/approve`,
  its `Approval` type and `Store.ApproveUserAudited` were removed in multi-tenancy step 24. Approval is a
  membership's, given by that project's admin through `internal/memberapi` over
  `project.Store.ApproveWith`; the account's own approval is derived from its memberships. The
  `noMail{}` scheduler the mail-sending flows fall back to lives in `register.go`.
  `PATCH /admin/users/{uid}` (a patch — `UpdateUserInput`'s fields are all pointers, nil = keep — of
  profile, `disabled`, `instance_role`; `updateUserProfileQuery` COALESCEs each one, an empty patch writes
  nothing) is **`RequireMaintainer`** like every
  `/admin/users` route: an account spans tenants, so a project admin manages their members' roles through
  the members API instead.
  **Renaming an account** (`rename.go`, `store_user_rename.go`) is behind `PUT /admin/users/{uid}/username`.
  Another mail-sending type beside `Registration`, `Rename` (built with `NewRename(RenameConfig{Service,Mail,SignInURL})`,
  wired into `APIConfig.Rename`, derived mail-less by `renameFor`). `Rename.Rename` applies
  `guardMaintainerBoundary`, normalises and validates the name with `validateAccountUsername` (the rule creation
  and registration share: not empty → `ErrUsernameRequired`, at most `MaxUsernameLen` runes → `ErrUsernameTooLong`;
  login keeps the length-only `validateUsername`), then `Store.RenameUserAudited` locks the row, returns it
  unchanged through `errNoAuditableChange` when the name is the same, refuses a name another row holds
  **case-insensitively** (`lower(username)`, then the unique violation for a concurrent taker) with
  `ErrUsernameTaken`, updates `username`/`updated_at`, runs the `alongside` hook that enqueues the
  `username_changed` mail, and writes `user.rename` with `old_username`/`new_username` stamped into the entry's
  details map (shared with `inAuditedTx`'s copy of the entry, which is why the map is created before the call).
  Nothing else is touched: every foreign key into `users` references `uid`.
  **Password reset by link** (`passwordreset.go`, `store_passwordreset.go`, `handlers_passwordreset.go`,
  migration `0065_password_reset_tokens.sql`) lets an administrator start a reset for somebody without
  ever learning the password that comes out of it. It is its own type, `PasswordReset` (built with
  `NewPasswordReset`, wired into `APIConfig.PasswordReset`, derived mail-less by `passwordResetFor`
  when the caller supplies none — the link comes back in the response, which is exactly what an
  instance without mail needs), holding the `MailScheduler`, the link base and the TTL.
  `PasswordResetTTL` is **7 days**: the person is by definition locked out and may read the mail after
  a weekend away, and the window costs nothing — the token is 256 bits, single-use, and issuing a new
  link deletes the old.
  `password_reset_tokens` stores **only a hash** of the token (`hashPasswordResetToken`, hex SHA-256 —
  not bcrypt, for the reason at `hashAPITokenSecret`: a full-entropy secret has no dictionary to
  defend against, and the hash is the table's `UNIQUE` lookup key), with `user_uid` **ON DELETE
  CASCADE** (a token of a deleted account is not a token) and `issued_by_uid` **ON DELETE SET NULL**
  (removing an administrator must not void links other people hold). The table is **preserved** by
  `kukatko maintenance reset` for the same reason `sessions` is (see `internal/reset`).
  `Store.IssuePasswordResetAudited` runs one transaction: `lockUser` (`SELECT … FOR UPDATE`, shared
  with the approval path) → `ErrUserDisabled` for a blocked account → delete the account's **unused**
  links (which is the whole of "only the most recent link works") → insert → the `alongside` hook that
  enqueues the `password_reset` mail → the audit entry (`user.password_reset`, details `reset_id` +
  `expires_at`). `Store.ConsumePasswordResetAudited` is the mirror: `lockPasswordReset` by hash →
  `Usable(at)` → `lockUser` → set the hash, stamp `used_at` (conditionally, so a racing second request
  loses), delete **every** session of the account, audit entry (`user.password_reset_use`, actor =
  target = the account). Every refusal a public caller can trigger — unknown, spent, expired, blocked —
  collapses into the single `ErrPasswordResetInvalid`, so the token cannot be probed.
  `Service.CleanupFinishedPasswordResets` prunes expired and consumed rows on the same hourly tick as
  the expired sessions (`cleanupOnce`, called by `RunCleanup`). The link base comes from `cmd/kukatko`'s
  `passwordResetURL(cfg.Mail.BaseURL)` (`mail.base_url` + `/password-reset`); an empty base falls back
  to the site-relative path. `mintPasswordReset`/`replacePasswordReset`/`withResetDetails` are the
  token, the "drop unused, insert" write and the audit details, shared with `ProjectSetup`.
  **Setting a project up for somebody** (`projectsetup.go`, `store_projectsetup.go`) is `ProjectSetup`
  (`NewProjectSetup(ProjectSetupConfig{Service, Mail SetupMail, LinkBase, ProjectURL, TTL})`, wired by
  `cmd/kukatko/projects.go` into `projectapi.Config.Setup`; `SetupMail` = `MailScheduler` + `Delivers(to)`,
  satisfied by `*mailjob.Enqueuer`). `Create(meta, project.CreateParams, FirstAdminInput{Email,
  DisplayName, Username})` is **one transaction** (`Store.inTx`, the flow writes its own entries):
  `lockAccountsByEmail` (case-insensitive, `FOR UPDATE`, two rows = `ErrEmailAmbiguous`, a blocked one =
  `ErrUserDisabled`) → for no match a **passwordless** plain user (`password_hash = ''`; the given
  username, taken = `ErrUsernameTaken`, or `derivedUsername` + `freeUsername`: `jana`, `jana2` …) via
  `insertUser` without a membership + `user.create` (`via: project_setup`) → `project.CreateTx` with the
  admin as `CreatorUID` (`project.create`, `project.member.add`; the caller never becomes a member) →
  `welcome`: while the account has no password a set-password link (`user.password_reset` with
  `project_uid`; link = base + `/<token>?project=<slug>`) mailed as `mailjob.ProjectSetup`, otherwise
  `mailjob.ProjectAdminAdded` with the project's address. The answer `FirstAdmin{…, Account
  created|existing, MailSent, SetPasswordURL, ExpiresAt}` sets `MailSent` from `Delivers` after the
  commit. `ResendLink(meta, projectUID, userUID)` re-issues for a member with no password
  (`project.ErrNotMember`, `ErrUserDisabled`, `ErrPasswordAlreadySet`); `AwaitingPassword(projectUID)`
  lists such members with their live link's expiry (`Store.AwaitingPassword`). `User.HasPassword()`
  says whether an account has one: `checkLoginPassword` compares a passwordless account against the
  dummy hash and refuses it, and `PasswordResetStatus.FirstPassword` tells the page to say *set* rather
  than *reset*.
  **The user listing takes an approval filter**: `Store.ListUsers(ctx, UserFilter{Pending *bool})` —
  `true` only the accounts waiting for an administrator, `false` only the ones let in, `nil` everybody —
  expressed as `($1::boolean IS NULL OR (approved_at IS NULL) = $1)` so the roster stays one query.
  `pendingParam` parses `?pending=` off `GET /admin/users` and refuses a non-boolean with 400.
  **Waiting for approval is a sign-in outcome:** `checkLoginPassword` returns `ErrNotApproved` (→ 403)
  for an account with a NULL `approved_at`, *after* the bcrypt comparison and *after* the disabled
  check, so only a caller who already holds the credentials learns it and a blocked account stays
  indistinguishable from a wrong password.
  **Strict role ladder** viewer < curator < editor < admin < maintainer (migration `0036_role_maintainer.sql`
  redefines the CHECK on `users.role` and drops the `ai` role that `0023_role_ai.sql` had added earlier; `ai`
  accounts are promoted to maintainer; `0085_role_curator.sql` widens the same CHECK by `curator`): every
  role inherits the rights of the lower ones. `viewer` only reads; `curator` curates the catalogue (faces,
  people, albums, labels) without rewriting a photo's own metadata; `editor` writes media/metadata;
  `admin` adds management (users, audit, trash); `maintainer` is the top — operations (imports, maintenance, status,
  backup/restore, jobs, processing). Predicates: `CanCurate()` = curator **or** any `CanWrite()` role
  (`requireCurate`/`RequireCurator`), `CanWrite()` = editor+, `IsAdmin()` = admin **or**
  maintainer (inheritance), `CanMaintain()`/`CanImport()` = maintainer only. **No rank arithmetic in
  Go:** every predicate names its roles explicitly, so an inserted rung inherits nothing by accident
  (the numeric rank lives only in the frontend). A curator fails `CanWrite()`/`IsAdmin()`/`CanMaintain()`/
  `CanImport()`; as of the role's first step no route hangs on `RequireCurator` yet, so a curator
  behaves exactly like a viewer, and self-registration still lands on `viewer`. Import is therefore an operational action
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
  **The e-mail address is mandatory** (migration `0063_users_email_required.sql`): `users.email` keeps
  its `NOT NULL` but loses the empty-string default and gains `users_email_not_empty`
  (`CHECK (btrim(email) <> '')`). The migration first fills every blank address with an undeliverable
  placeholder in the reserved `.invalid` domain — the username reduced to `[a-z0-9-]`, truncated to 31
  characters and joined to the account's `uid` (`jan-novak-us…@kukatko.invalid`); the uid is what keeps
  two usernames that reduce to the same local part apart. There is deliberately **no unique index**: a
  household mailbox two accounts share is a real arrangement. `normalizeEmail`/`validateEmail`
  (`service.go`, beside `validateUsername`) are the Go half — trim, lower-case the **domain** only, then
  require a single `net/mail`-parseable address that comes back **unchanged** (so a display-name form or
  a quoted local part with spaces is out), with a dot in the domain and at most `MaxEmailLen` = 254
  bytes; otherwise `ErrInvalidEmail` → 400. `prepareNewUser` and `validateUserUpdate` both run it, the
  latter returning the *normalized* input so what is stored is what was validated. `placeholderEmail`
  gives the same `.invalid` address to the **bootstrap maintainer**, the one account created before
  anybody could configure a mailbox; `internal/mailer` and `internal/mailjob` refuse such a recipient
  rather than dialling it.
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
  `username`/`role` (create) or `role`/`disabled` (update — only the ones the patch sets — /disable). A
  write of the **disabled flag** (`SetUserDisabledAudited`, or `UpdateUserProfileAudited` with `Disabled`)
  goes through `updateUserNotingStranded`, which first takes the locks a membership change takes:
  `lockMemberProjects` locks the row of every project the account is a member of (any role, uid order,
  repeated until no new one appears; `FOR NO KEY UPDATE`, which still excludes `lockProject` but lets a
  foreign-key check through — a write by the account into its project takes KEY SHARE on the account, then
  the project, and `FOR UPDATE` turned that into a deadlock with the disable, a 500 for one of them), then — under `withMaintainerGuard` — the account row (`lockUser`).
  Only on the **enabled → disabled transition** does `strandedProjectsQuery` then list the projects the
  account is the last working admin of (approved admin membership, no other approved admin on an enabled
  account); the disable is **allowed**, the list lands on a copy of the entry's details as
  `stranded_projects` (absent when empty) and is logged as a warning after commit. Holding the project
  lock serialises the disable with `internal/project`'s last-admin guard (`ensureAnotherAdmin` runs under
  the same lock), so a disable racing another admin's self-demotion either sees the project stranded or
  makes the demotion fail with `ErrLastAdmin`.
  `Store.SetInstanceRoleAudited(uid, Role, entry)` is the instance-role write behind the system
  administrator's promote/demote (`internal/projectapi`): it touches no membership, validates the role
  (`ErrInvalidRole`) and runs under the same `withMaintainerGuard` (`ErrLastMaintainer`).
  `POST /admin/users/{uid}/disable` is mounted behind `RequireMaintainer` (inside the `RequireAdmin`
  group): blocking an account locks it out of every project, so it is the system administrator's call.
  **API tokens** (`apitoken.go`, `store_apitoken.go`, `service_apitoken.go`,
  `handlers_apitoken.go`, migration `0020_api_tokens.sql`): a long-lived bearer credential
  `kkt_<id>_<secret>` for non-interactive clients. `<id>` is the row PK (prefix `at`), so verification
  is **a single indexed lookup**, not a scan over hashes; `<secret>` carries 256 bits from `crypto/rand`.
  Only the **hex SHA-256** of the secret is stored (`hashAPITokenSecret`) — **deliberately not bcrypt**: bcrypt
  protects low-entropy passwords against a dictionary and is paid once per login, whereas a token is verified
  on *every* request and a 256-bit random secret has no dictionary; the comparison is constant-
  time (`subtle.ConstantTimeCompare`). The plaintext is returned **exactly once**, at creation.
  The `APIToken` model (`name`, `expires_at`, `last_used_at`, `revoked_at`, `unlimited`) + pure predicates
  `Revoked`/`Expired`/`Active`; a token **inherits the owner's role** (no role column, no second
  permission system). `Service.AuthenticateAPIToken` returns, on any failure, the single
  `ErrInvalidAPIToken` (→ 401, never 403, the body doesn't distinguish the case) and stamps `last_used_at`
  at most once per `apiTokenUseInterval` (= a minute, mirrors `slidingRenewInterval`).
  `Store.CreateAPITokenAudited`/`RevokeAPITokenAudited` write the audit `inAuditedTx` — mutation and audit
  row in one transaction; `errNoAuditableChange` turns a repeated revocation into a no-op with no audit
  record. `bearerToken` parses `Authorization` case-insensitively per RFC 7235; a missing or
  non-Bearer scheme falls through to the cookie.
  **The rate-limit exemption** (`unlimited`, migration `0077_api_tokens_unlimited.sql`): a token a maintainer
  marked unlimited skips the comment, upload and bulk throttles entirely — the case is an agent driving the
  library through `kukatko ctl`, whose bursts are exactly what those limiters are shaped to stop while being
  work somebody asked for. It is a property of **the credential, never of the person**: `authenticateRequest`
  copies it onto the request's `principal` only on the bearer path, so a session cookie is throttled whatever
  the owner's role (a stolen cookie gains nothing) and the exemption dies with the token when it is revoked.
  `RateLimitExempt(r)` is the exported predicate the throttled routes hand to
  `ratelimit.Limiter.MiddlewareExcept`; it reads the principal, so a route mounting it must sit **inside** its
  auth guard. Setting the flag is maintainer-only in the **service** (`User.governs`: the instance role alone — a
  project's admin governs a project, never an account's credentials), not in middleware —
  `Service.CreateAPIToken` (which now takes the acting `User`) refuses a non-maintainer's `Unlimited` with
  `ErrAPITokenUnlimitedForbidden` → **403 and no token at all**, and `Service.SetAPITokenUnlimited` refuses the
  same way — because creation needs that check *conditionally* (only when the body asks) and one place cannot
  drift from the other. The toggle reaches **only the caller's own** token (somebody else's → `ErrAPITokenNotFound`
  → 404, even for a maintainer: revocation is the power a maintainer holds over a colleague's credential, this is not),
  and `Store.SetAPITokenUnlimitedAudited` writes it `inAuditedTx` with `UPDATE … WHERE unlimited IS DISTINCT
  FROM $2`, so re-sending the value a token already carries changes nothing and writes no audit row
  (`api_token.update`, details `name` + the value moved to).
  **Passkeys** (`passkey.go`, `passkey_ceremony.go`, `store_passkey.go`, `handlers_passkey.go`,
  migration `0067_passkey_credentials.sql`, library `github.com/go-webauthn/webauthn`): WebAuthn
  sign-in beside the password. It lives **in this package rather than a package of its own** on
  purpose — a passkey login produces exactly the session a password login produces, from the same
  `Service` under the same policy, and splitting that across a boundary would mean exporting the
  session machinery to build a second door into the same house. `Passkeys` is therefore a peer of
  `Registration` and `PasswordReset`: optional (`APIConfig.Passkeys` nil ⇒ every route answers 501),
  built by `NewPasskeys` from `PasskeysConfig{RPID, RPDisplayName, Origins}`, which **refuses an empty
  ID or origin list itself** rather than letting the library infer one — the ID is what a credential is
  permanently bound to, so an inferred one would mint keys that stop working when the inference changes.
  `passkeyUser` adapts an account to `webauthn.User` with **the account UID as the user handle**
  (26 printable characters, well inside the protocol's 64-byte limit, and resolvable back to the
  account — which is what makes the discoverable, usernameless login possible at all).
  `BeginRegistration`/`FinishRegistration` and `BeginLogin`/`FinishLogin` are the four halves;
  the login pair uses `BeginDiscoverableLogin`/`ValidatePasskeyLogin`, so the credential decides whose
  session is created. `checkPasskeyLogin` then applies the *account* rules a verified signature must
  still not get past — disabled ⇒ `ErrPasskeyRejected` (as unspecific as a bad signature), unapproved
  ⇒ `ErrNotApproved`, the same distinction `checkLoginPassword` draws.
  `ceremonyStore` (`passkey_ceremony.go`) holds the in-flight challenges **in memory**, keyed by a
  25-character random id the client carries in an HttpOnly `kukatko_passkey_ceremony` cookie: a
  challenge is worthless the moment it is answered or expires, so a table of them would be a table
  whose every row is garbage within `defaultCeremonyTTL` (5 min). `take` is **one-shot** — the entry
  goes whether the answer verified or not, which is what makes a captured response unreplayable — and
  refuses a registration ceremony offered as a login (`userUID` non-empty). The map is capped at
  `maxCeremonies` = 4096, dropping expired entries before refusing, because the begin half is
  anonymous; `API.RunMaintenance` prunes it on the same tick as the limiters.
  The row (`passkey_credentials`, PK prefix `pk`) keeps the credential id (`BYTEA`, UNIQUE — it *is*
  the lookup key of a discoverable login), the COSE public key, the sign counter (`BIGINT`, because the
  protocol's counter is an unsigned 32-bit), the transports, the AAGUID and attestation type/format,
  the owner's name for it, `created_at`/`last_used_at` — and **the four credential flags**, which are
  not decoration: go-webauthn refuses a login whose backup-eligibility flag disagrees with the one
  recorded at registration, so a row scanned without them would reject the very authenticator it
  belongs to (most synced platform passkeys). `CreatePasskeyAudited`/`TouchPasskeyAudited`/
  `DeletePasskeyAudited` all go through `inAuditedTx`; the touch carries the counter and the latched
  flags forward in the same transaction that records `passkey.login`. The table is classified
  **preserved** in `internal/reset` — it holds the only copy of the key a sign-in is checked against.
  Rate limiting reuses the login `Limiter` on two per-address keys (`passkeyBeginLimitKey`,
  `passkeyLoginLimitKey`) so ceremony-opening cannot spend the verification budget.
  The tests drive a **virtual authenticator** (`passkey_virtual_test.go`, integration tag): a real
  ES256 key producing real attestation objects, assertions and signatures, because every step of a
  ceremony *is* a signature and a stub that answered "verified" would test the stub),
  `internal/photos/`
  (the photo-catalog core.
  **Project scope — the canonical convention every multi-tenancy scoping step follows**
  (`store_project.go`): a store method that reads a **root table** (one carrying `project_id`,
  see `docs/superpowers/specs/2026-10-11-multi-tenancy-design.md` §2.4) takes `projectUID string` as
  its **first argument after `ctx`** and **never defaults it** — an empty value is the typed
  `ErrNoProject` (`requireProject`), a programming error, never "the whole instance". The predicate is
  `projectClause` = `photos.project_id = $n` (qualified, so it stays unambiguous next to a joined table
  that carries `project_id` too). A row of another project is **the same error as a missing row**
  (`ErrPhotoNotFound`, an empty list), so the HTTP layer answers it with the very 404 a missing uid
  gets. Handlers pass `auth.ProjectUIDFromContext(ctx)`; nothing else may name the project. **Child
  tables** (`photo_files`, `photo_edits`, markers, comments, renditions…) carry no column and are scoped
  through their root: a reader keyed by a photo uid stays unscoped, and the handler gates on a scoped
  load of the photo first (`photoapi.photoInProject`). Scoped so far: the whole listing path —
  `List`/`Search`/`FilterUIDs`/`Count`/`CountMedia`/`TimelineBuckets`/`YearBuckets`/`UploaderBuckets`
  share `buildWhere(projectUID, params)`, whose **first clause is always the project predicate bound as
  `$1`** (also under `MatchNone`), and whose placeholder `whereClauses` hands to the query-language
  compiler (`condEnv.project`): every condition that resolves a name or an id against another root table
  — `album:`, `label:`, `task:`, `person:`, `family:` (the root of the walk and every family it steps
  through, kept uncorrelated), the
  alias arm of `uid:`, the reference photo of `near:` — confines that lookup to the project too
  (`TestQueryKeys_compileInsideTheProject` walks `query.Keys()`, so a new key is covered the day it is
  added); the single-row lookups `GetByUIDInProject`, `GetByFileHashInProject`, `GetByPhotoprismUID`,
  `GetByPhotoprismFileHash`, `GetByPhotosorterUID`, `GetByPhotoprismAlias` (the alias table's own
  `project_id` in the sub-select); `ListByUIDsInProject`, `ListStackMembers`, `StackCounts`, `GetOCR`;
  the scans behind duplicate detection, stacking and the trash — `ListActivePhashes`,
  `ListStackCandidates`, `StackInfoByUIDs`, `ListArchivedUIDs` — list only the project's photos; so do the
  **backfill sources** (`ListActiveUIDs`, `CountActivePhotos`, `ListPhotosMissingPhash`/`CountPhotosMissingPhash`,
  `ListPhotosMissingBlurhash`/`CountPhotosMissingBlurhash`, `ListPhotosMissingFileMetadata`,
  `ListVideosMissingTechnicalMetadata`, `ListPhotosMissingSidecar`, `ListPhotosMissingOCR`, `ListActiveImageUIDs`,
  `ListVideosMissingHLS`, `ListActiveVideoUIDs`/`CountActiveVideos`), the place tree `AggregatePlaces` and the
  location estimator's `ListLocationCandidates`/`ListLocatedNeighbours`/`SetEstimatedLocation`/
  `ListEstimatedLocations`/`CountEstimatedLocations`.
  **Writes** follow the same rule: a mutation takes `projectUID` after `ctx` and touches a row only
  inside that project — `UpdateMetadata`(`Audited`), `Archive`/`Unarchive`(`Audited`),
  `SetHiddenFromLibrary`(`Audited`) carry `projectClause` in the `UPDATE`'s `WHERE` (0 rows =
  `ErrPhotoNotFound`); the child-table writers `SetEdit` and `CreateFile` (the `photo_files` writer of
  ingest, which passes the fresh row's `ProjectID`) insert through `INSERT … SELECT … FROM photos WHERE
  uid = $1 AND project_id = $n`, so the parent is proved in the same statement; the transactional ones —
  the audited mutations (`mutateAudited`) and the stack operations `CreateStack`(`Audited`),
  `SetStackPrimary`/`UnstackMember`/`UnstackAll`(`Audited`, through `inProjectTx`), the purge's
  `DeleteAudited(ctx, projectUID, uid, entry)` (`mutateAudited`, its `DELETE` carrying the project too)
  and the detection pass `CreateStacksAudited(ctx, projectUID, plans, entry)` (`requirePlansInProject`:
  every member of every plan, before the first stack) — first run `requirePhotosInProject` on their own
  transaction, so a foreign uid is refused before a stack it would leave is touched, writes no audit row,
  and **a stack never spans projects**. An audited write defaults
  the entry's `ProjectUID` to the project (`withProject`). `ListFiles(ctx, projectUID, uid)` is scoped by
  an `EXISTS` on the parent. **Worker loaders:** a background job has no request and so no project;
  it loads through the explicitly named, unscoped **`GetForJob(ctx, uid)`** (`FilesForJob` had no caller and
  is gone) and learns the project from the row — every read now fills **`Photo.ProjectID`** (`photos.project_id`
  is the last of `photoColumns`; `json:"-"`, it never leaves the server). **`Create`** writes it as the
  last insert parameter and refuses an empty `Photo.ProjectID` with `ErrNoProject` (the column has had
  no default since migration 0114). `thumbjob`, `embedjob`, `facejob`,
  `metajob`, `ocrjob`, `sidecarjob`, `placesjob`, `storyboardjob` and `hlsjob`
  use them (`tagnotifyjob` reads its rows with its own SQL). `forjob_guard_test.go` walks every
  `internal/*api` package's non-test source and fails on `ForJob(` / `.…ForJob` (vectors'
  `GetEmbeddingForJob` included) and on `GetAcrossProjects`: a handler loads through the scoped lookups,
  never these. The job-side write-backs keyed by a uid the job just loaded
  (`SaveBlurhash`, `SetPhash`, `MarkThumbnailsBuilt`, `FillFileMetadata`, `SaveOCR`, `MarkSidecarWritten`,
  …) stay unscoped for the same reason. **Still unscoped:** **`GetAcrossProjects`** (was `GetByUID`,
  renamed so the name says it reads past the boundary; its one caller is `internal/userpic`, whose picked
  photo may live in any project the viewer is an approved member of and which checks that membership on every
  read — an API package may not call it, see the guard above), `Delete` (ingest's rollback of
  its own fresh row) and the maintenance scans. The unscoped helpers nothing in production called any more
  (`GetByFileHash`, `ListByUIDs`, `SetPhotoprismRef`, `SetPhotoprismFileHash`, `SetPhotosorterRef`,
  `ListPhotoprismAliases`, `ApplyImportMetadata`) were deleted on 2026-10-14 with their tenancy-guard
  allow-list entries; the scoped `GetByFileHashInProject`/`ListByUIDsInProject` are the lookups. The pure builders are tested through
  `stripProject` (store_project_test.go), which removes the scope and renumbers, so the filter tests keep
  reading as the filter they test while the scope itself is asserted on the real output.
  Typed models `Photo`/`PhotoFile`/`Phash`/`Edit`/`MetadataUpdate`
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
  built by hand cannot invent a grain);
  **the blurred placeholder** `Blurhash string` (JSON/column `blurhash`, `omitempty`, migration
  `0068_photos_blurhash.sql`): a BlurHash of the photo's rendering, derived data like a thumbnail and
  regenerable at any time. The column is nullable and the NULL is load-bearing — "not computed yet", the
  predicate the backfill lists — so the model carries the absence as the empty string and `blurhashOrNil`
  maps it back to NULL on write, which the `photos_blurhash_not_empty` CHECK insists on. `store_blurhash.go`:
  `SaveBlurhash(uid,hash)` overwrites it (an empty hash clears it back to NULL, `ErrPhotoNotFound` for an
  unknown uid) and deliberately does **not** bump `updated_at`, for the reason `SaveOCR` does not — a
  machine-derived value nobody asked for must not reorder every "recently edited" listing the first time a
  backfill drains; `ListPhotosMissingBlurhash(limit)`/`CountPhotosMissingBlurhash()` are the backfill's
  predicate (no placeholder, not archived, newest first, served by the partial index
  `idx_photos_blurhash_pending`), videos included since their thumbnails come from the poster frame);
  **the date put away** `TakenAtBeforeUnknown *time.Time` (JSON/column `taken_at_before_unknown`,
  migration `0066`, **read-only**, absent from `MetadataUpdate`): the capture time the photo carried at the
  moment its date was declared unknown, kept so that declaration is reversible — the wrong date a scan was
  stamped with is the usual reason for saying "unknown", and destroying it made the statement one-way. The
  rule lives in the store, not in a caller: `TakenAtBeforeUnknownAssignment(takenAtExpr)` (`store_takenat.go`)
  returns the `taken_at_before_unknown = CASE …` SET clause that **stating a date → drops the preserved
  value / clearing a date → preserves the outgoing one / a clear after a clear → keeps what is already
  preserved**, and all three write paths use it (`updateMetadataRow`, `FillMissingMetadata`, and
  `internal/bulk`'s `clear_taken_at`). Hence the invariant it is read under: a photo with a `TakenAt` never
  has one. Provenance only — no index, nothing sorts, groups or filters by it (`dated:no` filters on
  `taken_at` itself); `TakenAtSourceUnknown` is the `taken_at_source` both clear paths stamp),
  `MediaType` image/video/live (`MediaType.Kind()` reduces it to the `MediaKind` still/video boundary
  — **a live photo counts as a still**, its hashes and embeddings come from the still and it carries no footage
  of its own; the one definition `internal/duplicates` and `internal/dupmerge` both read),
  `FileRole` original/sidecar/edited, UID generator prefix `ph`,
  `Store` over pgx with
  `Create`/`GetAcrossProjects`/`GetByFileHashInProject`/`GetByPhotoprismUID`/`GetByPhotoprismFileHash`
  (the identity of a single SOURCE FILE, not of a source photo: a source photo made of several
  files — a RAW next to its JPEG — becomes one row per file grouped in one stack, and only the
  displayable one carries the `photoprism_uid`, so a sibling is found again by its own SHA1 alone;
  partial index from `0045`)/`GetByPhotosorterUID`/
  **`AddPhotoprismAlias(ctx,projectUID,ppUID,photoUID,ppFileHash)`**/**`GetByPhotoprismAlias(ctx,projectUID,ppUID)`**
  (the `photoprism_aliases` table from `0046`: `photoprism_uid` (per project)
  PRIMARY KEY → `photo_uid` many-to-one `ON DELETE CASCADE`. `photoprism_uid` is a 1:1 column and cannot
  express the source keeping the SAME BYTES as two photos — the second one has no row of its own, because
  `file_hash` is UNIQUE, and the row holding its content already wears the first one's uid. The alias records
  that collapse, so the second source uid still resolves to a row; the write is an idempotent upsert (a re-run
  re-records it, a moved winner re-points it), an `INSERT … SELECT` that writes only for a photo of the named
  project (`ErrPhotoNotFound` otherwise) — nothing in production records aliases since the PhotoPrism import
  was removed, the tests seed them through it — and the read is the second half of resolving a source uid,
  after `GetByPhotoprismUID`)/`ListByUIDsInProject`
  (batch lookup by uid inside the project, ignores unknown ones)/`FilterUIDs`
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
  **`RepairVideoMetadata(ctx,uid,VideoRepair) (changed,error)`**
  (the write side of the **video re-probe**, `internal/metajob`: it applies a decision the caller has already
  made, column by column. Every `VideoRepair` field is optional and a nil one becomes a `COALESCE` over the
  column → "leave it exactly as it is", so the statement carries **no policy of its own**; the two provenance
  columns (`taken_at_source`, `location_source`) are written **only together with** the value they describe,
  and `taken_at_before_unknown` goes through the shared `TakenAtBeforeUnknownAssignment`. The type spells the
  split out: **file-derived** (`duration_ms`, `file_width`+`file_height`, `fps`, `video_codec`,
  `audio_codec`+`has_audio`) versus **curated** (`taken_at`, `lat`+`lng`+`altitude`) — see `internal/metajob`
  for who may overwrite what. An **empty repair never reaches the database** (no statement, no `updated_at`),
  so re-probing a healthy clip is a true no-op; the statement is guarded by `media_type = 'video'`, because it
  speaks about a container and aiming it at a still is a caller's bug → `ErrPhotoNotFound`)/
  **`ListVideosMissingTechnicalMetadata(ctx,limit) ([]uid,error)`**
  (the candidate set of the **video half of the metadata backfill**: non-archived videos with no
  `duration_ms`, no `video_codec` or no frame size — exactly what a *failed* probe leaves behind, newest
  first, `limit <= 0` = all. `fps` and audio are deliberately **not** in the predicate: a container can
  legitimately carry neither, and a clip that could never satisfy it would be re-scheduled by every run
  for ever)/
  `Archive`/`Unarchive`/`Delete`/`List`+`Count` (filters archived/
  uploader (`UploadedBy` a user uid, or **`NoUploader`** = `uploaded_by IS NULL`, the imported photos;
  `NoUploader` wins over `UploadedBy` the way `OnlyArchived` wins over `IncludeArchived`)/has-GPS/**bounding box** (`BBox *BBox` — `bboxClauses`: `lat BETWEEN` + `lng BETWEEN`, or the
  OR of the two halves for a box whose west edge lies east of its east edge, i.e. across the antimeridian;
  `ParseBBox` reads the `south,west,north,east` form and returns `ErrInvalidBBox` for anything but four
  in-range numbers with south ≤ north; it is the map's "photos in this area", which the viewer pages
  through)/date-range `taken_after`+`taken_before`/camera/lens/substring search +
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
  the `buildWhere` filters for `total`)/`CountMedia` (the same filters, answering `MediaCounts{Total,
  Videos}` from one scan: `count(*)` plus a `FILTER (WHERE media_type = $n)` aggregate, the media type
  bound like every other value. `Videos` counts **standalone clips only** — a live photo counts with the
  stills, because it is a photograph that happens to carry motion. It is what lets a listing say
  "Videos: 7" instead of "Photos: 7" over a shelf of clips; `Total` is always identical to `Count`'s)/`Search` (Czech-aware fulltext over the generated `fts
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
  scrubber; `flagImplausible` then sets `Implausible` on every bucket whose year fails
  `exif.PlausibleCaptureYear`, the one place that rule lives, so the rail does not restate it),
  `YearBuckets(params)` (year-histogram `Years{Years:[]YearBucket{Year,Count},Total}` in
  `store_years.go` — one `GROUP BY date_part('year', taken_at)`, ordered `year DESC`; shares
  `buildWhere` with `List`/`Count`, so a bucket's count = exactly what `List` returns for the same filters
  plus that year; `params.Sort`/`Order`/pagination are ignored, photos without `taken_at` don't fall into
  buckets, but `Total` (via `Count`) includes them — the basis of `photoapi`'s year facet),
  `UploaderBuckets(params)` (who uploaded the matching photos, `[]UploaderBucket{UID,Username,
  DisplayName,Former,Count}` in `store_uploaders.go` — a former member of the project keeps the bucket and
  uid, names empty, `Former` set (`project.FormerMemberSQL` on `$1`) — one `GROUP BY uploaded_by` ordered count desc, uid asc,
  the names resolved by a `LEFT JOIN users` **outside** the aggregating subquery, since the shared filters
  name photo columns unqualified and `users` carries columns of those very names; shares `buildWhere` with
  `List`/`Count`, so a bucket's count = exactly what `List` returns for the same filters plus that uploader;
  the photos with **no** uploader are their own bucket (empty `UID`), so the counts add up to `Count`
  — the basis of `photoapi`'s uploader facet),
  plus `CreateFile`/`ListFiles`,
  `ListArchivedUIDs(before,limit,offset)` (uids of archived photos oldest-archived-first,
  `before` nil = all / non-nil = only `archived_at <= before` retention cutoff — the basis of trash/purge),
  `CountPhotos()` (total photos incl. archived) + `ListFilePaths()` (all `photo_files.file_path`)
  — the basis of the post-restore integrity report (`backup.PhotoCatalog`) — and `ListCataloguedFiles()` (every
  `photo_files` row as `CataloguedFile{ProjectUID,FilePath}`, ordered by key, every project: the instance-wide
  maintenance scan groups by project itself),
  maintenance listers (`store_maintenance.go`): `ListPrimaryFiles()` (`PrimaryFile{PhotoUID,ProjectUID,FilePath,
  FileHash}`, every project, for the same reason),
  `ListPhotosMissingPhash(limit)` (uids of non-archived photos without a pHash — the basis of thumbnail
  backfill/repair), `ListPhotosMissingFileMetadata(limit)` (uids of non-archived photos with
  `metadata_extracted_at IS NULL`, i.e. whose **file has never been read** — the basis of metadata
  backfill; the predicate is a *marker*, not "the columns are empty", so the backfill converges even for photos
  without IPTC tags; it is covered by the partial index `idx_photos_metadata_pending` from migration
  `0028_photos_metadata_extracted.sql`, which is empty once the backfill is exhausted) and `ListActiveUIDs()`
  (uids of all non-archived photos — the basis of the forced full thumbnail/metadata backfill
  `?all=true`) and, next to the HLS listers (`store_hls.go`), `CountActiveVideos()` (a `count(*)` over
  `media_type = 'video' AND archived_at IS NULL`, i.e. what `ListActiveVideoUIDs()` returns — the dry run of
  the video-scoped poster rebuild `/process/thumbnails?videos=true`) and `CountVideos()` (the same count
  **without** the archived filter — "can this library stream anything at all?", the guard the integrity check's
  streaming half asks before it touches the store, archived videos included because archiving removes neither a
  rendition row nor a segment, only a purge does), the **orientation-geometry repair** (`store_geometry.go`):
  `ListDimensionMismatches()` → `[]DimensionMismatch{UID,StoredWidth,StoredHeight,Orientation,RawWidth,RawHeight}`
  = quarter-turned photos (`file_orientation` 5–8) whose columns hold their own file's dimensions **transposed**,
  i.e. the displayed frame instead of the stored one — the import defect that letterboxed the
  viewer and drifted the face boxes. The raw pair is read out of the `exif` jsonb document
  (`ImageWidth`/`ExifImageWidth`/`PixelXDimension` + the height equivalents, each `jsonb_typeof`-checked so a
  non-numeric value degrades to "unknown" instead of aborting the query with a cast error), so **the comparison
  is the evidence** — a photo whose document says nothing, whose columns already agree, or whose frame is square
  is never reported and no provenance is guessed. It is read-only, hence the **dry run** of
  `RepairDimensions(m)` → `bool` (writes the file's own pair, guarded on the transposed one it replaces, so a
  repeat is a no-op and can never swap a corrected row back), the **impossible-capture-date repair**
  (`store_impossibledate.go`): `ListImpossibleTakenAt(minYear,maxYear)` →
  `[]ImpossibleDate{UID,FileName,TakenAt,TakenAtSource}` = every photo (archived ones included) dated outside
  the inclusive year range, compared as instants (`captureYearWindow` turns the years into the half-open UTC
  interval, so the predicate does not depend on the session time zone) — read-only, hence the **dry run** of
  `ClearImpossibleTakenAtAudited(uid,minYear,maxYear,entry)` → `bool`, which declares the date unknown
  (`taken_at` NULL, `taken_at_source` `unknown`, `taken_at_precision` `day`, the outgoing value preserved by
  `TakenAtBeforeUnknownAssignment` exactly as the single-photo PATCH and the bulk clear do) and writes `entry`
  **in the same transaction**; it is guarded on the same predicate, so a row re-dated in between changes nothing
  and no audit entry is written for it. The range itself is not this package's business — `internal/maintenance`
  takes it from `exif.CaptureYearBounds()`, **stack methods** (`store_stacks.go`, see `docs/ARCHITECTURE.md` §5.1):
  `ListStackCandidates` (not-yet-stacked non-archived photos for detection)/`StackInfoByUIDs`/
  `ListStackMembers` (stack members, **primary first** — the strip of variants)/`StackCounts` (member count
  per `stack_uid` — the tile badge)/`CreateStack`/`SetStackPrimary`/`UnstackMember`/`UnstackAll`
  (reversible bookkeeping over `stack_uid`/`stack_primary`) and their **audited variants**
  (`store_stacks_audit.go`): `CreateStackAudited`/`SetStackPrimaryAudited`/`UnstackMemberAudited`/
  `UnstackAllAudited` plus `CreateStacksAudited(projectUID,plans,entry)`, which applies a whole detection pass
  (`StackPlan{PrimaryUID,MemberUIDs}`, decided purely by `internal/stacks`) and its one entry in a single
  transaction. Each writes `entry` on the **mutation's own transaction**, so a refused operation leaves no
  row, and each fills the details in itself — the `stack_uid`, the promoted photo's `previous_primary_uid`,
  the members a dissolved stack had — because those are facts only the transaction knows. `stackEntry`
  **copies** the caller's entry before completing it: a target stamped onto the original inside the closure
  would be thrown away with it, and the row would record a NULL target. Plus `ListParams.IncludeStackMembers`
  (lifts the shared visibility predicate `(stack_uid IS NULL OR stack_primary)` for a caller that wants
  **all** members — as a `uid:` filter and a **task scope** do on their own, the latter because a task's
  frozen group names files rather than shots) and the exported **`LeaveStackTx(ctx,tx,uid)`** (takes one photo out of its stack and
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
  listing is scoped to an album (`AlbumUIDs`), a label (`LabelUIDs`), a task (`TaskUIDs`) or the
  caller's favourites (`FavoriteOf`): a photo filed there was put there deliberately, and that one rule
  spares every caller a flag. `ListParams.IncludeHidden` is the explicit escape hatch, and an explicit `hidden:` in the
  query language yields the default too (`archivedClauses`' precedent — otherwise `hidden:yes` would
  match nothing). It is neither `archived_at` (on its way out, purged after retention) nor `private`
  (a sharing concept, out of scope). The two hand-written library queries repeat the predicate by hand:
  `AggregatePlaces` (`store_places.go`) and `ListLocationCandidates` (`store_location.go`, no geocoder
  credits on a photo no map shows) — `ListLocatedNeighbours` deliberately does **not**, since a hidden
  photo's GPS tag is still a real measurement. Duplicate detection, maintenance, the backfills, backup
  and import verify are all untouched: they are about data integrity, not browsing.
  `SetPhash`/`GetPhash` (the upsert **restamps `created_at`** on every write: the stamp is when the thumbnails
  were last built, which is what the processing report's `thumbnail.at` — and through it the viewer's rebuild
  watch — reads), `NearestPhash(projectUID, phash)` (the ingest near-duplicate lookup: the closest pHash
  **of that project's photos only**, joined through `photos.project_id` — the warning names the match, so a
  whole-table search handed an uploader, even an anonymous one on an upload link, another project's uid;
  empty project = `ErrNoProject`), `SetEdit`/`GetEdit`; dedup on SHA256 `file_hash` + external IDs
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
  512 B) + the extension as a hint (`mediaTypeByExt` for HEIC/RAW/video), with `mediaTypeByExtFirst`
  (`.m3u8` → `application/vnd.apple.mpegurl`, `.m4s` → `video/iso.segment`) decided by extension **before**
  anything is sniffed — a playlist is UTF-8 text that sniffing confidently calls `text/plain`, which no player
  will load a playlist from, and a fragment's `styp` box it does not recognise at all; sentinels
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
  **`PrefixStorer`** (optional, both backends) is `Store` under a key prefix:
  `StoreUnder(ctx, src, prefix, takenAt, name)` publishes at `<prefix>/YYYY/MM/<name>` (the prefix confined
  by `prefixedDir`: no `..`, no stray slashes), collisions and `ErrAlreadyExists` judged inside the prefix
  only; `Store` is `StoreUnder` with `""`. It is how a project with its own storage prefix
  (`projects/<uid>/`) keeps its originals apart, so two projects never share a key.
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
  **`SizedKeyLister`** (also optional, both backends) adds the size: `KeySizes(ctx, yield(key, size))` — `R2`
  takes it from the listing itself (no `HEAD` per object), `FS` stats each walked file. Only the upgrade
  inventory's storage check (`internal/inventory`) uses it.
  **`Copy(ctx, from, to)`** (part of `Storage`) duplicates an object inside the store, replacing whatever
  occupies `to`, leaving the source untouched; copying a key onto itself is a no-op and a missing source
  wraps `os.ErrNotExist`. `FS` **hard-links** through the `.tmp` staging dir and renames into place (a byte
  copy when the filesystem refuses a link; a staged link left by `rename(2)` between two links of one inode is
  removed), `R2` issues one **server-side `CopyObject`** (no byte crosses the network; content type and the
  `sha256` metadata travel with the copy) and refuses an object over 5 GiB with **`ErrTooLargeToCopy`** rather
  than copying it in parts. Only the storage move (`internal/rekey`) calls it.
  **`ETagStater`** (optional, `R2` only — the filesystem has no entity tags): `StatETag(ctx, key)` →
  `ObjectStat{Size, ETag}` from one metadata request; `IsContentETag(etag)` tells a single-part tag (the hex
  MD5 of the content, comparable) from a multipart one (`-<parts>` suffix, says nothing about the bytes).
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
  upload all objects → `Head` reads them back and
  verifies size and SHA256 → `MarkMigrated` commits the row → only then the optional `Delete` of the local
  copies. **What "all objects" means is asked once per kind of object the store can hold**, not once per kind
  the author remembered: `plan` walks `storekeys.Kinds()` and `planKind` is a default-less switch over them
  (`internal/storekeys`, `exhaustive` linter), so a new prefix in the store cannot be silently stranded —
  which is exactly what happened to the streaming segments. Today that is the original, its metadata sidecar
  if one exists on disk (`sidecarexport.KeyFor` keys it), the thumbnails already in cache (it generates no new
  ones), and **every HLS segment the source holds under `hls/<file_hash>/`** (`planHLS`, found by
  `Source.KeysWithPrefix` — how many segments a video was cut into is known only to the store, and the
  catalogue's rendition rows are not touched by a migration, so segments left behind would turn every video
  into a playlist of 404s). `KindFamilies` is the one kind `planKind` **declines**: the genealogy export
  belongs to the library rather than to a photo, so planning it per photo would upload the same file once
  per photo in the archive. `planLibrary` moves it instead — the root layout's `families.yaml` and the one at
  the root of every project's tree, `Catalogue.StoragePrefixes()` (`SELECT DISTINCT storage_prefix FROM
  projects`) naming the prefixes, every key moved verbatim like an original's — once per run and **after** the walk — the
  running server rewrites it whenever a relation changes, so the end of the run is the smallest window in
  which an edit lands on a disk the migration has already emptied — and a failure there aborts with an error
  rather than joining the per-photo failures: there is one such object and no later photo to retry it beside.
  The sidecar is the non-regenerable disaster-recovery artifact, so
  it is carried into the store with the original and the original is never deleted until the sidecar is
  durable there. `deleteLocal` removes only the **planned, verified** keys (never a fresh listing: an object
  written after the plan was verified nowhere) and only of the kinds `livesLocally` claims — original,
  sidecar and segments, all under the originals root the move empties — while thumbnails (regenerable,
  in a separate cache) stay; `Deleted` counts originals. There is no path where the bytes live only where nobody has vouched for
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
  `internal/rekey/`
  (**the storage move of one project** from the root layout (`storage_prefix = ''`, the bootstrap project
  until its move) under `projects/<uid>/`, one photo at a time while the app runs; the model — the worker
  job is `internal/rekeyjob`, the routes `internal/rekeyapi`, the CLI `kukatko storage rekey` (runbook in
  `OPERATIONS.md`). **`Plan(photo PhotoFiles, newPrefix) []Move{From, To, Kind}`**
  is pure: the original first, then the sidecar (`sidecarexport.KeyFor`), every size of `thumb.SizeNames()`
  and every object of every recorded HLS rendition (`init.mp4` + `00000.m4s…` up to `segment_count`, unioned
  with the names the stored media playlist lists, invalid names dropped), each `To =
  storekeys.Join(newPrefix, rest-of-From)`; it walks `storekeys.Kinds()` with a default-less switch like
  `storagemigrate`, `KindFamilies` declined (project-wide, moved by `Finish`). **`Service`** (`New(Config{Pool,
  Store, Jobs})`, `Store` = `Copy`/`Stat`/`Open`/`Delete`/`KeysWithPrefix`, satisfied by both backends; `Jobs`
  = `EnqueueFamilyExport`/`EnqueueSidecar`, satisfied by `*jobs.Enqueuer`) owns `storage_rekey_runs`
  (migration `0112`; preserved by the wipe). **`Start(ctx, meta, projectUID)`** in one transaction locks the
  project, refuses (`ErrProjectNotFound`; `ErrRunActive` for a running/paused run; `ErrAlreadyPrefixed` for a
  project with its own prefix and no run ever — a created project), sets `projects.storage_prefix =
  'projects/<uid>/'` — **new uploads land in the new layout at once** — audits it
  (`project.storage_prefix`, with `old`/`new`/`rekey_run`; a second run of a moved project does not switch
  again) and inserts the run with totals = count and `sum(file_size)` of the photos whose `file_path` lacks
  the prefix, then audits `project.rekey.start` and queues the `storage_rekey` job
  (`jobs.EnqueueStorageRekey`) — the run and the job that drives it commit together. `Pause(ctx, meta, …)`
  (running→paused), `Resume` (paused→running), `Retry` (failed→running, **cursor reset** so the pass takes the
  photos it skipped again) act on the project's latest run (`ErrInvalidState` otherwise) in one transaction
  each that audits it (`project.rekey.pause|resume|retry`, details `rekey_run`/`target_prefix`) and, for
  resume and retry, queues the job; `Status` returns the latest run (`ErrNoRun`). **`Preview(ctx, projectUID,
  limit)`** is the dry run: Start's refusals checked in a transaction it always rolls back, the totals a start
  would record and the first `limit` photos of the pass (`PreviewPhoto{UID, Original Move, Derived}`) —
  nothing written. **`Step(ctx, run, batchSize)`** re-reads the run
  (`ErrNotRunning` unless running), finishes the **pending** photo first, then takes the next photos still in
  the old layout in `file_path` order after `last_key`; per photo, in this order and no other: copy the
  original unless a verifying copy is already there → **verify** (size = `photos.file_size`, then the copy's
  ETag = the source's *content* ETag, else stream it and compare SHA-256 with `file_hash`) → one transaction
  moves `photos.file_path`, the `photo_files` row and names the photo in `pending_photo_uid`/`pending_old_key`
  → move the derived objects that exist (a missing one is skipped): the sidecar on its own, the thumbnails
  and segments **together** (`moveTracked` in `derived.go`: every one copied first, the root copies deleted
  only when all copies succeeded) → delete the old original (never while any photo row names it) → count it,
  advance `last_key`, clear pending. A thumbnail/segment copy that fails keeps **every** root thumbnail and
  segment of the photo and records it in **`storage_rekey_derived_failures`** (migration `0115`; photo uid,
  project, `kinds`, last `message`, `attempts`; emptied by the wipe) before the photo is counted; the run's
  `DerivedFailed` (`derived_failed`) counts the project's rows. Once no photo is left in the old layout,
  `Step` retries the recorded photos instead (`stepDerived`, uid order after the run's `derived_cursor`,
  which `Resume`/`Retry` reset): re-plan the photo's root thumbnails/segments against the prefix its row
  points at — **plus every root key the final sweep recorded for it** (`root_keys`, migration `0116`;
  `sweptMoves`) — keep a prefixed copy that exists, copy a missing one, delete the root copies only once all
  exist, then forget the photo (or refresh its record). A recorded key becomes a move under the prefix when
  the layout's own builder spells it for the photo's hash (`thumb.RelPath` of a registered size, `hls.Key` of
  a valid rendition + name), otherwise a **drop** (`Move` without `To`: deleted in the second phase, never
  copied — an unregistered thumbnail size, a name the streaming layout cannot hold); only root `thumb/`/`hls/`
  keys named after the photo's own hash are taken (`rootDerivedMove`). A crash before
  the commit leaves both copies and the old row (the next pass re-verifies the copy); a crash after it leaves
  the photo pending, and the next step verifies the new copy again — re-copying from the still-present old key
  when it does not verify — before deleting anything. A per-photo failure goes into the `errors` ring (last
  50 `{key, message, at}`) and is skipped; `error_count > 100` fails the run (`ErrRunFailed`); a cancelled
  context or `storage.IsSystemic` error returns without recording. **`Finish(ctx, run)`**: no photo left in
  the old layout (`ErrPendingCleanup` while one is pending; photos left after a pass with **no** recorded
  error can only be uploads in flight at `Start`, stored at the root below the cursor, so the cursor is
  rewound — `rewindSQL`, guarded by `error_count = 0` — and `ErrPendingCleanup` returned for the job to step
  them; after a pass with errors the run fails with `ErrPhotosRemain` for a `Retry`) →
  **sweep** the bucket's root `thumb/` and `hls/` trees — each listed **once per `Finish`** — for objects of
  moved photos (matched by the content hash in the key) and record every key found with its photo
  (`root_keys`, merged into an existing record); a photo recorded **only now** resets the derived cursor and
  returns `ErrPendingCleanup` so the job steps it, while one already recorded (its retry failed) only gains
  the keys — so the sweep and the retry own the same objects and a stray no plan knows can never loop the
  close → no recorded derived failure left (else the run fails with `ErrDerivedRemain`) →
  enqueue a `sidecar` rewrite for **every** photo → copy the root `families.yaml` under the prefix when no
  export landed there yet, then delete the root one and every root `sidecars/` key **only once a listing shows
  its prefixed replacement** (nothing at the root is touched while any project is still in the root layout) →
  only then enqueue `family_export` for the project, so an export job that wrote the fresh file between the
  listing and the copy (which overwrites it with the pre-move root file) is followed by one that writes it
  again → `done`. The local thumbnail cache is not moved: it is a
  regenerable cache, and the published objects are. **`FindRootDerived(ctx)`** /
  **`MoveRootDerived(ctx, found)`** are the same root sweep and retry for every project a move touched,
  read-only and repairing respectively (`RootDerivedPhoto{UID, ProjectUID, Moves}`,
  `RootDerivedResult{Photos, Objects, Failed}`) — the `root_derived` check and repair of
  `internal/maintenance`. The closing sweep, `FindRootDerived` and so `MoveRootDerived` find nothing while
  another project is still in the root layout (`rootShared`, like `cleanRoot`): a hash is unique only per
  project, so a matching root tile may be that project's. **The move and the publishing jobs (`gate.go`)**: the `thumbnail`,
  `hls_transcode` and `sidecar` handlers wait for the move, never the move for them — the chosen
  direction, because a photo the pass skipped for a busy job would sit behind the `file_path` cursor until a
  `Retry`, while a deferred job just runs later. **`Gate`** (`NewGate(pool)`) answers
  **`AwaitingMove(ctx, projectUID, filePath)`**: true while the project has a **running** run and `filePath`
  is not under its target prefix — the handler then defers with `worker.RetryAfter` (from `MoveRetryDelay`,
  a minute, backing off to 30 minutes — `worker.WaitedDelay`) until the move has taken the photo; a
  paused/failed/done run awaits nothing. **The photo lock (`lock.go`)**: a transaction-scoped advisory lock
  per photo uid (`hashtextextended('rekey_photo:'||uid, 0)`). `movePhoto` copies and verifies the original
  unlocked, then takes it **exclusively** (`lockPhoto`, waiting) for re-reading the photo (a photo gone or
  changed since the batch is `ErrPhotoChanged`, and **`discardCopy`** deletes the copy this move made unless
  a row names it), the commit, and `completePhoto`; `finishPending`, `retryDerived` and `MoveRootDerived`
  run each photo under it too. **`Gate.Publish(ctx, projectUID, photoUID, fn(filePath, found))`** takes it
  **shared without waiting**, re-reads the photo's key on the lock's own transaction and runs `fn` — the
  thumbnailer's upload (`thumb.WithLocation`), `hlsjob`'s segment upload + row, `sidecarjob`'s write — under
  it; while the move holds the photo, or a crashed step left it `pending_photo_uid`, it returns
  `storekeys.ErrMoving` without calling `fn`, and the caller defers (or the HTTP rebuild answers 503).
  **`Gate.Purge(ctx, projectUID, photoUID, fn)`** is the same shared lock **waiting**, for
  `internal/trash` (refusing a pending photo the same way). So a fresh object lands before the move copies
  (at the root, taken along) or after it is through (under the new prefix), never in between where the stale
  root copy would overwrite it or it would be orphaned at an emptied root, and a purge and a move never
  interleave on one photo. Publishers never wait, so a pooled connection is never held by a job waiting for a
  move that itself needs the pool. **`ActiveRuns(ctx)`** lists the running/paused runs, oldest first — what
  `internal/maintenance` checks before a repair that deletes at the root. Tests: `gate_integration_test.go`
  (the gate and `ActiveRuns` through start, pause, resume and finish), `plan_test.go` (plan, rendition names,
  verification over an in-memory fake with S3-style ETags), `derived_test.go` (the two-phase move: a copy
  failure keeps every root object, a retry moves them, a newer prefixed copy is kept, a systemic failure is
  fatal; a drop deletes without copying and waits for the copies; the key-hash parsers; which root keys move,
  which drop and which are never taken), `derived_integration_test.go` (a derived failure keeps the root tiles
  and a later pass moves them with `derived_failed` back to 0; a failed run retried; the final sweep; strays no
  plan names — an old thumbnail size, an unrecorded rendition, an invalid segment name — reach `done` in two
  `Finish` calls with the root trees listed once per call (a listing-counting fake); a stray that cannot move
  fails the run instead of looping, and `Retry` moves it; the maintenance find/move; the retry query's project
  scoping and order) and `rekey_integration_test.go` (tag
  `integration`, real DB + the fake): a full run, a crash after copy and after commit, a verification failure
  (old kept, run continues, `Finish` fails it, `Retry` completes it), the error threshold, pause/resume and
  the `Start` refusals, the transitions' audit entries and their one queued job, the preview, with an
  invariant — every `photos.file_path` names an existing object of the right size — checked after **every**
  step), `lock_integration_test.go` (the photo lock: a thumbnail published between the commit and the
  derived copy is refused and its deferred re-run is what the prefix serves; a publish in flight holds the
  move off and its fresh root tile is taken along; a purge — the real `trash.Service` over the fake store,
  finding every object itself — before the commit leaves nothing of the photo, one during the copy waits and
  leaves nothing either, one of a photo whose segment copy failed takes its root leftovers too and the run
  then ends `done` with the root empty; a crashed step's pending photo refuses both purge and publish until
  the next step finishes it), `finish_race_integration_test.go` (the family export queued only after
  `Finish`'s copy writes last; an upload committed at the root below the cursor after an error-free pass is
  taken by the rewound pass with no `Retry`; after a pass with errors the `Retry` takes it),
  `internal/rekeyjob/`
  (**the `storage_rekey` job**: the scheduled half of `internal/rekey`. `New(Config{Mover, PerSecond, Clock,
  Logger})`, `Mover` = `Status`/`Step`/`Finish`, satisfied by `*rekey.Service`; `PerSecond` is
  `storage.rekey.per_second` (`DefaultPerSecond` 10 when not positive). **`Handle(ctx, job)`** reads the run
  of the project the payload names (`jobs.ProjectUIDOf`; none → `worker.Terminal`): **running** → one
  `Step` of `PerSecond` photos, then a deferral (`worker.RetryAfter`, no attempt burned) of whatever is left
  of that batch's share of the pace (`processed/PerSecond` s minus the time the batch took, never negative),
  so the job holds its slot for about a second and a restart loses at most the photo in hand; the pass over
  → `Finish` and complete; **paused** → a deferral of `PausedRecheck` (1 min); **done/failed/no run** →
  complete, nothing re-enqueues it. `ErrRunFailed` (the step failed the run), `ErrPhotosRemain` and
  `ErrDerivedRemain` (Finish failed it) complete the job — the run says failed, `Retry` queues a new one; `ErrNotRunning` and
  `ErrPendingCleanup` defer 0 s (read the run again); anything else (database, store) is returned so the
  queue retries with backoff. One slot (`internal/worker`). Tests: `rekeyjob_test.go` (every state and
  outcome over a fake mover, the pace with a frozen clock) and `rekeyjob_integration_test.go` (tag
  `integration`, the real move over an in-memory store: progress per pass, a paused pass, the finishing
  pass, no re-enqueue once done)),
  `internal/rekeyapi/`
  (**the maintainer's storage-move routes**, instance-level: `GET /projects/{uid}/rekey` (the latest run,
  404 `not_found`), `POST /projects/{uid}/rekey` (start, 201; 409 `already_running`/`already_prefixed`, 404
  for an unknown project), `POST …/rekey/{pause,resume,retry}` (409 `invalid_state`). `NewAPI(Config{Mover,
  RequireMaintainer})`, `Mover` = `Status`/`Start`/`Pause`/`Resume`/`Retry` (`*rekey.Service`); the audit
  meta is the signed-in maintainer's (`audit.FromRequest`), the auditing and the job enqueue happen in
  `internal/rekey`'s transactions. **Flat routes** in a `Group` with the guard, not a `/projects`
  sub-router — `internal/projectapi` owns that one, and chi routes the longer paths here in either
  registration order (`TestRoutes_besideTheProjectsSubRouter`). Tests: unit (dispatch, guard, error codes,
  the coexistence) and integration (403 for a project admin, 401 anonymous, the 409 cases, the whole
  start → pause → resume → retry lifecycle with its audit entries and queued job)),
  `internal/storekeys/`
  (**the one place that says what lives in the object store and which layout owns it.** The store has no
  per-application prefix — an original sits at `YYYY/MM/<name>` and the derived artefacts sit beside it — so
  "what is this object?" cannot be answered by a single prefix test, and before this package each whole-store
  operation carried its own list of prefixes to remember. **A leaf package** (it imports nothing of Kukátko's
  own), so every key builder can use it. It declares each layout's top-level name — `ThumbnailDir` (`thumb`),
  `SidecarDir` (`sidecars`), `HLSDir` (`hls`), `FamiliesKey` (`families.yaml`), re-exported by the builders as
  `thumb.CacheSubdir`, `sidecarexport.Prefix`, `hls.Prefix`, `familyexport.Key` — plus the store-wide
  `DumpPrefix`=`db/`, `PartialPrefix`=`.tmp/` and `ProjectsDir`=`projects/`. `Kind` = `KindOriginal`
  /`KindSidecar`/`KindFamilies`/`KindThumbnail`/`KindHLS`/`KindDump`/`KindPartial`/`KindForeign`
  (+`String()`), `Kinds()` (every kind, a fresh copy, so an operation can be tested against the whole enum).
  **Two layouts:** every project's tree hangs off its storage prefix — `""` (the root layout) for the
  bootstrap project until its storage move, `projects/<uid>/` for every created one. The prefix grammar:
  `PrefixOf(originalKey)` (`projects/<uid>/` when the key starts with a **well-formed** project prefix —
  `pj` + 24 symbols of the uid alphabet, checked so a foreign `projects/` tree is never a project — else `""`),
  `Strip(key)` → `(prefix, rest)`, `Join(prefix, rest)` (its inverse), `ValidatePrefix(prefix)`
  (`ErrInvalidPrefix` unless `""` or exactly `projects/<uid>/` — every builder calls it, so a malformed
  prefix never becomes a cache path or a key) and `ProjectUID(prefix)`. `ErrMoving` is the transient "a
  storage move is moving this photo right now" that `rekey.Gate` returns and the publishers and the purge
  recognise — defined here so `internal/thumb`, which the move imports, can see it. **The rule every builder follows:**
  a photo's derived keys take `PrefixOf(photos.file_path)` — the prefix of its **own original**, never the
  project row — so a half-moved project stays consistent per photo and no read falls back or guesses.
  `ClassifyWithProject(key)` → `(Kind, projectUID)` accepts both layouts: it strips a project prefix and
  classifies the rest by the root rules (prefix tests against the four layout names and the anchored
  `YYYY/MM/<name>` pattern, plus the one match by **exact key**, `families.yaml`, which owns no prefix, so a
  foreign file whose name merely begins the same way stays foreign); `db/` and `.tmp/` exist only at the
  root, so inside a project's tree they are foreign, and a foreign key names no project. `Classify(key)` is
  the same without the uid. A leading slash and surrounding whitespace are tolerated, everything else is
  foreign. **Why it exists:** the streaming segments were added to the store and two whole-store operations
  (the backup and the storage migration) went on not knowing about them for four commits. Consumers —
  `internal/backup` (`backedUp`), `internal/storagemigrate` (`planKind`/`livesLocally`), `internal/rekey`
  (`planKind`), `internal/reset`
  (`classifyKey`) and `internal/maintenance` (`projectObject`) — switch over or match on `Kind`, all four
  **without a default clause**, so a new member fails the `exhaustive` linter in each of
  them until that operation says what it does about the new prefix; per-kind tests in each package back this
  up with a length check against `Kinds()`),
  `internal/mediaurl/`
  (mints client media addresses and stamps them onto photo payloads; the only decision is made by the storage
  backend via `URL`. `NewBuilder(store)` → `Builder` with `Thumb(ctx,uid,filePath,fileHash,size)` /
  `Download(ctx,uid,filePath,fileName)` (the client address: the signed Worker URL, otherwise a fallback to the own
  route `<auth.APIBase(ctx)>/photos/...` — `/api/v1/p/<slug>/photos/...`, the project prefix the request
  came through), `Object(relPath)` / `ThumbObject(filePath,fileHash,size)` (the thumbnail key under the storage prefix of
  the original at `filePath`; the **raw** backend
  response — an empty string = "stream it yourself", non-empty = "redirect there"; the media routes use this),
  `DownloadObject(filePath,fileName)` (the same raw answer for an **original**, decorated to download)
  and `Decorate(ctx,list)` / `DecorateOne(ctx,&photo)`, which fill `Photo.ThumbURL`+`Photo.PreviewURL`+`Photo.DownloadURL`.
  `Download` forces `?original=true` on the fallback so both branches mean the same thing (the stored original,
  never the rendering of a non-destructive edit).
  **The signed download asks the edge for an attachment** (`downloadParam` = `dl`, valued with
  `photos.file_name`, which the object key need not equal — the store suffixes a colliding name on ingest).
  It must: a signed URL lives on the media domain, which is **another origin**, and a browser ignores
  `<a download>` off-origin — so without the parameter "download the original" merely opened the photo in a
  tab (and a video stand-in's "download the video" handed back a player that would not play it). The Worker
  answers `Content-Disposition: attachment; filename=…` for it (`infra`: `workers/kukatko-media/index.js`,
  which sanitizes the name and adds RFC 5987 `filename*` for a non-ASCII one). The parameter is
  **not covered by the signature** on purpose — it grants nothing the signature does not already grant, it
  only decides what the browser does with bytes the holder may fetch anyway — so the two repositories deploy
  independently; a URL that will not parse is returned untouched (`withDownloadParam`), since a download
  that opens in a tab beats a mangled URL the edge refuses. It rides on the **download URL alone**:
  `Object`/`Thumb`/`ThumbObject` mint the very same objects as inline `<img>`/`<video>` sources out of one
  shared edge cache entry, so downloading must stay a property of the URL, never of the object.
  `photoapi.serveOriginal` redirects through `DownloadObject` for the same reason — the route promises an
  attachment, and a `302` must not lose it. **A nil `*Builder` is valid** and behaves like a backend that
  publishes nothing → an API built without storage (test) still returns a working payload. `uid`/`size` are
  percent-encoded into the route. The payload carries **two** thumbnails, because the client picks between them
  by shape: `thumb.GridSize` (`tile_500`, the square crop a medallion or a card draws) and `thumb.PreviewSize`
  (`fit_720`, the aspect-preserving rendition the justified photo wall draws).
  **Project check:** `DecorateOne` reads the request's project (`auth.ProjectUIDFromContext`) and addresses a row
  only when `Photo.ProjectID` equals it (`mayAddress`); a row of another project — or one with no `ProjectID`, which
  no catalogue read produces — gets its three fields **cleared and nothing signed**. The callers already pass rows
  they filtered; this is the defensive second lock, so a signed URL is never minted for a photo of a project the
  request did not address. A request with **no** project (an instance route such as the notifications inbox) is
  not checked here — what it may see is its handler's rule. Tested through `decorateForProject` (`export_test.go`)
  and, with a real principal, `project_integration_test.go`.
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
  `<prefix>thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg` (shard from the hex SHA256; `<prefix>` =
  `storekeys.PrefixOf(photo.FilePath)`, the storage prefix of the photo's own original — `""` for the root
  layout — so two projects holding identical bytes never share a thumbnail; with `WithLocation(l)` — a
  `LocationLock`, wired with `*rekey.Gate` by `cmd/kukatko`'s `thumbOptions` — each size's upload runs
  inside `l.Publish` (the storage move's shared photo lock), right after the photo's row is re-read there: a
  size whose prefix moved since the generation started (a storage move committed the photo), or that the move
  holds right now (`storekeys.ErrMoving`, wrapped too), is refused with `ErrPrefixMoved`, its cache file
  removed, nothing published; a row not committed yet publishes as planned), regenerable +
  **idempotent** (skips existing ones) + atomic write temp+rename; `Thumbnailer` =
  `New(store,cacheDir,WithConcurrency(n),WithMaxPixels(px),WithDecodeBudget(b))` with the API `Generate(ctx,photo,sizes...)`/
  `GenerateAll(ctx,photo)` (a size→abs-path map, skips existing)/
  `RegenerateAll(ctx,photo)` (**force** — overwrites all sizes in-place with an atomic
  temp+rename, and republishes to object store; the basis of the "regenerate thumbnail" service action)/
  `Path(prefix,hash,size)`/`OpenCached(prefix,hash,size)` (the **local cache alone**)/
  `OpenOrGenerate(ctx,photo,size)` (**the backend-independent read** — see below);
  the package-level `RelPath(prefix,hash,size)` returns the same cache path relatively — it is also the **object key**
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
  `GridSize` (`tile_500`) is the square size a card or a medallion renders and that `thumb_url` carries in the
  payload, `PreviewSize` (`fit_720`) the aspect-preserving one the justified library wall renders, carried as
  `preview_url` — a wall tile is the shape of its photograph, so a square crop would cut its ends off and go
  soft spread across it; it is an **existing** rung on purpose (every photo already has that thumbnail, so the
  wall needed no backfill, and an unknown size would be a signed URL to an object that does not exist),
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
  (see `internal/thumbjob`). **The client must not re-apply the saved edit**: a rendition already IS the saved
  edit, so the viewer styles only the difference between what its rendition carries and what it should show
  (`web/src/lib/photoEdit` `editDelta`, the edit-preview contract in `docs/FRONTEND.md`) — a saved 90° styled
  onto a rendition turned 90° was a photo lying on its side;
  **decompression-bomb guard**: `WithMaxPixels(px)` (config `thumb.max_pixels`, default 200 MP) makes
  `decodeAndOrient` call `imgconvert.EnforcePixelBound` before the full decode, so a source whose
  `width×height` exceeds the cap fails with `imgconvert.ErrImageTooLarge` instead of allocating a
  multi-GB bitmap; `0` disables it; **decode budget** (SEC-018): `WithDecodeBudget(b)` (the process's one
  `imgconvert.DecodeBudget`, config `thumb.decode_budget_mb`) makes `encodePureGo` reserve, before the decode,
  the bitmap (bit-depth-aware) **plus** everything rendered from it — `pureGoCost`: a full-size copy for an
  orientation that turns/flips, two for a non-destructive edit, the cascade below — so it waits while the
  budget is spent and a source that alone exceeds it fails with `ErrImageTooLarge`/`ErrOverBudget`;
  **the resize cascade** (`cascade.go`): `x/image/draw`'s kernel scaler allocates a `dstW×srcH×32`-byte scratch
  (fit_3840 of a 14142² source = 1.7 GB), so the sizes are no longer each scaled from the source in parallel.
  The source is first **box-reduced** by the largest integer `k` that still covers every needed size
  (`boxFactor`/`boxReduce`, an exact `k×k` average streamed a band at a time — libvips' shrink-on-load), then
  the sizes are rendered **largest first, one at a time**, each from the smallest rendition already made that
  is ≥ `cascadeMargin` (2) times it, else from that base (`renderOrder`/`pickSource`); the encode/write/publish
  of each rendition still runs in the `WithConcurrency` pool. A size's geometry always comes from the
  full-size picture (`renderSpec(src,spec,w,h)`/`centredSquare`), so it is pixel-identical in size to a
  direct resize and within a level on average in content (`TestGenerateAll_cascadeMatchesDirectResize`);
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
  for the startup log; `Remove(prefix,hash)` deletes all cached sizes for a hash under a storage prefix
  (idempotent, skips missing ones — thumbnail cleanup on photo purge); sentinels
  `ErrUnknownSize`/`ErrInvalidHash`/`ErrNotCached`, and **`ErrUndecodable`** — the original itself can
  never become a picture (corrupt/truncated bytes, `imgconvert.ErrUnsupportedFormat`/`ErrNoEmbeddedPreview`/
  `ErrImageTooLarge`, a converter that exited non-zero), marked in `decodeAndOrient` by `undecodable`; never
  for a cancelled context, a converter that is not installed or an I/O failure. The thumb, face-crop and
  avatar routes answer it 404 instead of 500;
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
  pulls **the largest** embedded preview instead of demosaicing: one metadata-only `exiftool -s3 -f -n` call
  reads the offset+length of `PreviewImage`/`JpgFromRaw`/`ThumbnailImage`, `image/jpeg`'s `DecodeConfig`
  measures the frame header sitting at each of those offsets (a few kB, never the image data), and only the
  winner is extracted with `exiftool -b <tag>` — Nikon bodies fill `PreviewImage` with a 640x424 thumbnail
  while the 6000x4000 image waits in `JpgFromRaw`, so taking the first non-empty tag served 10% of the pixels
  to thumbnails, embeddings and face detection alike; a candidate whose header won't parse ranks below every
  measured one (then by byte size), a file that advertises no offsets at all — or a garbled metadata answer —
  falls back to the old preference order, and a RAW with nothing in any tag still fails with
  `ErrNoEmbeddedPreview`, **video** (`FormatVideo`) delegates to `video.ExtractPoster` (poster frame via
  `ffmpeg`) — the thumbnailer and pHash process the poster as a photo; `DetectFormat` prefers **magic
  bytes** whenever they recognize a directly decodable format (JPEG/PNG/WebP/BMP/GIF/TIFF/HEIC) — so a JPEG
  renamed to `.dng`/`.tif` is decoded by content, **not** sent down the RAW branch (where it would have no
  embedded preview); **exception: TIFF magic doesn't carry RAW** — most RAW containers are TIFF-based
  (`II*`/`MM*`), so the RAW **extension** takes precedence over TIFF magic and the file goes through embedded-preview,
  not as a flat TIFF; otherwise RAW is chosen only when magic recognizes nothing (other RAW headers) → falls back to
  the extension; `DetectFormatNamed(path,name)` = the same rules with the extension read from `name` (an
  upload staged under a temp name); **AVIF** is recognised by its `ftyp` brand (`avif`/`avis`, or `mif1`/`msf1`
  with an AV1 and no HEVC compatible brand, checked before HEIC) as `FormatAVIF` — only so it can be refused:
  `EnsureDecodable` reports `ErrUnsupportedFormat`, `.avif` is not in `IsSupportedFormat`;
  `IsSupportedFormat`; `SupportedExtensions()` lists that whole set (images + `video.Extensions()`, dotless,
  sorted) and `TestSupportedExtensions_matchWebList` pins `web/src/lib/mediaFormats.json` — the web pickers'
  `accept` list — to it; `RAWExtensions()` exports that RAW set (lowercase, no dot, sorted) —
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
  decode — used by `thumb` and `ingest`; **process-wide decode budget** (`budget.go`, SEC-018):
  `DecodeBudget` = a byte-weighted semaphore (`NewDecodeBudget(bytes)`, `nil` = unbounded;
  `Reserve(ctx,cost) (release,error)` waits while the budget is spent, refuses a cost over the whole budget
  with `ErrImageTooLarge`+`ErrOverBudget`, and releasing ≥ 64 MiB runs a GC first so the next decode does not
  stack on the garbage of the last); `ReserveDecode(ctx,path,maxPixels,budget,extra)` /
  `ReserveConfig(ctx,cfg,…)` are the gate every in-process decoder calls before `image.Decode` — pixel cap +
  reserve `DecodedBytes(cfg)` (= `w×h×BytesPerPixel(cfg.ColorModel)`, **bit depth included**: 16-bit PNG = 8 B,
  8-bit RGBA = 4, YCbCr charged 3, gray 1) plus the caller's `extra(cfg)` (`RGBABytes` copies, resize scratch);
  `PeekConfig(path)` reads the header only. Callers sharing the one budget: `ingest`, `thumb`, `thumbjob`'s
  decoder, `facejob`'s rotation, `photoapi`'s edited download, `userpic`'s upload; sentinels
  `ErrConverterMissing`/`ErrUnsupportedFormat`/`ErrNoEmbeddedPreview`/`ErrImageTooLarge`/`ErrOverBudget`; a missing
  tool = a clear error), `internal/video/`
  (video without CGO, a **shell-out** to the FFmpeg suite: `Probe(ctx,path) (Metadata,error)` via
  `ffprobe -print_format json -show_format -show_streams` → `DurationMs`/`VideoCodec`/`AudioCodec`/
  `HasAudio`/`FPS` (rational parsing)/dimensions/`TakenAt` (creation_time)/GPS (ISO 6709), **fallback
  to `exiftool`** via `internal/exif` when `ffprobe` is missing; `ExtractPoster(ctx,path)` →
  a representative frame via `ffmpeg` to a temp JPEG + once-cleanup — the frame is **chosen**, not taken at a
  fixed offset (`posterframe.go`): the duration is probed, cut at `{5,15,30,50,70}%` (fixed
  `0,1,3,7 s` when no prober is installed, clamped 50 ms clear of the end and de-duplicated, so a 2 s clip
  still yields five usable offsets and a single-frame clip one), each candidate decoded to a 160 px JPEG and
  scored from a 64-bucket luminance histogram (`scoreFrame` → `dominant`/`spread`/`mean`; `uniform()` =
  `dominant ≥ 0.90`, `value()` = normalised entropy × an exposure factor falling to 0 at pure black/white);
  `pickFrame`/`betterFrame` prefer any non-uniform candidate over a uniform one and the highest `value` among
  equals, so a black intro loses to a later frame — and when **every** candidate is uniform the best of those
  still wins (never "no thumbnail", never an error that fails an upload). Sampling **stops early** at the first
  candidate with `value ≥ 0.30`, so an ordinary clip costs one sample; the winner is then re-extracted at full
  size, and offset 0 is always the last fallback. Pure and deterministic on purpose — ingest, a thumbnail
  rebuild and `image_embed` each re-derive the poster independently and must land on the same frame
  (`face_detect` was in that list until it stopped running on a video at all). Cost and the measurements
  behind the thresholds: `docs/PERF.md` §2;
  `IsVideoPath`/`IsVideoExt`/`Extensions()` (the set, dotless, sorted)/`FFmpegAvailable`/`FFprobeAvailable`;
  **the input guard** (`input.go`, SEC-017): `InputArgs(src)` = `-format_whitelist DemuxerAllowlist
  -protocol_whitelist <file | http,https,tls,tcp>`, which **every** ffprobe/ffmpeg argv over a user's file puts
  before its input — `ffprobeArgs`, `posterArgs`/`sampleArgs`, `TranscodeArgs`, `hls.EncodeArgs`,
  `storyboard.FFmpegArgs`. `DemuxerAllowlist` (`mov,matroska,avi,asf,flv,mpeg,mpegvideo,mpegts,h264,hevc`) is
  exactly the containers behind `videoExts`; leaving out dash/hls/concat is the point — they fetch the URLs a
  manifest names. The protocol half follows `src`: the local file alone for a path, the HTTP(S) stack for a
  signed object-store URL (transcode/encode read a remote original straight from it). A new video extension
  whose container is not on the list is refused by ffprobe — extend both together;
  **`Metadata.HasContainerMetadata()`** — the difference between a reading and a failure: a real clip always
  yields at least one of a duration, a codec name and a full frame size, so a `Metadata` with none of the
  three came from a probe that read **nothing** (ffprobe failed and the exiftool fallback happily described a
  truncated download as a text file). `Probe` reports that as a nil error, which is why a failed probe used
  to be invisible; `internal/ingest` logs it and `internal/metajob` refuses to write those zero values back
  over a catalogued clip; **on-the-fly transcode for
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
  upload in thirty. **The file-name date must also land on a plausible year** — no earlier than
  1826 (the oldest surviving photograph) and no later than a year from now, which forgives a
  camera whose clock is set wrong but drops the long digit runs Facebook and WhatsApp name their
  downloads with (`90090310_100000000000001_…` reads as 9009-03-10); a rejected year is simply
  no date, i.e. `unknown`, and the bound is the guess's alone — an EXIF date is kept however odd.
  That bound lives in `captureyear.go` (`MinCaptureYear`, `CaptureYearLookahead`,
  `PlausibleCaptureYear(year)` and `CaptureYearBounds() (min,max)`) because it has a **second**
  caller: `internal/maintenance` finds the rows that were dated before this guard existed and offers to
  withdraw their date. `CaptureYearBounds` is how a caller that must express the same rule as a **query**
  asks for it, so the two can never drift apart;
  An empty `name` disables the fallback (used by `video.probeWithExiftool`, whose
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
  shell-out, returning 1–8 or 0 when the file says nothing. **EXIF first, then XMP** (`xmp.go`): with no EXIF
  orientation it walks the JPEG segment headers itself (`image/jpeg` throws the APP segments away), takes the
  APP1 whose payload starts with the Adobe XMP signature and reads `tiff:Orientation` out of it — **both**
  serialisations, the attribute form (`tiff:Orientation="8"`) and the element form
  (`<tiff:Orientation>8</tiff:Orientation>`); a truncated segment or a packet that is not XML reads as 0 and
  never panics. EXIF keeps precedence when both are present. XMP is read at all because a file can carry its
  orientation nowhere else: the batch that exposed this were JPEGs misnamed `.dng` with **no EXIF block
  whatsoever** and `tiff:Orientation="8"` in their packet, which made this reader weaker than the `exiftool`
  one `Extract` uses at ingest — the two disagreed, and `facejob` handed the detector a picture lying on its
  side. It answers the one question a caller has about bytes it is about to hand to something that ignores
  EXIF — does this still need turning? — and it has to be asked of those bytes, not of the catalogue: an
  `imgconvert` intermediate may already carry the rotation in its pixels, and applying the original's tag on
  top would turn the picture twice (which is why `facejob` consults the catalogue **only** when the bytes are
  the untouched original)), `internal/phash/`
  (perceptual hashes, **CGO-free**: `Compute(img) Hashes{Phash,Dhash int64}` — **pHash** via
  a 2-D DCT 32×32 → low-freq 8×8 block with a median-without-DC threshold, **dHash** gradient 9×8; `Distance(a,b)`
  = Hamming distance via `bits.OnesCount64`; near-dup = a small distance), `internal/blurhash/`
  (the **blurred placeholder**, **CGO-free**: `Encode(img image.Image) (string,error)` → a BlurHash string
  (woltapp/blurhash) the frontend decodes into the blur a tile paints while the real thumbnail loads.
  Encoding is `github.com/bbrks/go-blurhash` (MIT, zero-dependency, pure Go); this package owns the three
  decisions around it. **(1) Scale first**: `downscale` renders the source into an opaque `image.NRGBA`
  whose longest side is at most `WorkingSide = 64` (`draw.CatmullRom`, never upscaling) — the encoder's cost
  is proportional to the pixel count and a placeholder computed from 64 px is indistinguishable from one
  computed from a 24 Mpx original; NRGBA because it is the encoder's fast path. Transparency is composited
  **over white** (the destination is filled white and drawn `draw.Over`), so a PNG with a cut-out background
  yields the light placeholder its rendering suggests rather than whatever colour sits under the transparent
  pixels. **(2) Match the grid to the aspect ratio** (`componentsFor`): 4×3 for a landscape, 3×4 for a
  portrait, 4×4 within 20 % of square → 28 or 36 bytes, small enough to ride along in every photo of every
  list payload. **(3) Refuse an empty image** (`ErrEmptyImage` for nil or zero-sized bounds) rather than
  encode nothing. The image is expected **in display orientation** — the placeholder stands in for the
  rendition the user sees, so a caller holding an untouched original orients it first — or, cheaper, passes
  the orientation to `EncodeOriented(img, orient)`, which scales down first and orients only the 64-px working
  copy (the same hash, without an 800 MB oriented copy of a 200 MP decode; what `internal/ingest` does).
  `internal/thumbjob` gets display orientation for free by reading a preview. Sub-images (non-origin bounds) encode from their own pixels), `internal/ingest/`
  (the upload/ingest pipeline: `Service` = `New(Config{Storage,Photos,Thumbnailer,Enqueuer,Sidecar,OCR,
  Places,Duplicate,MaxFileSize,Usage,MaxPixels,DecodeBudget,TempDir,Logger?})` (`Logger` defaults to `slog.Default()`) (`MaxPixels` = the same decompression-bomb cap as `thumb.max_pixels`,
  and `DecodeBudget` the process's one `imgconvert.DecodeBudget`, both applied through
  `imgconvert.ReserveDecode` to the pre-store decode (`verifyPixels`, whose reservation rides in `stagedPixels`
  and is released by `done()` right after the hashes — before the thumbnailer decodes again) and to the pHash
  decode; a rejected source becomes a `phash_failed` warning, the photo is still catalogued; the blurhash is
  encoded with `blurhash.EncodeOriented`, which orients the 64-px working copy instead of the full bitmap) with **`IngestFile(ctx,proj,src,Request{Filename,UploadedBy,Sidecar,DeclaredSize,MaxFileSize})`** (the full form; `proj` is the
  **`*project.Project`** the caller resolved — the request's project on `POST /upload`
  (`auth.ProjectFromContext`), the link's on the public upload-link route, the `--project` target of
  `import dir`, the project whose prefix an orphan key is under for the maintenance re-import
  (`project.ForKey`). The SHA-256 dedup looks up `(project_id, file_hash)` only
  (`GetByFileHashInProject`), so the same bytes in two projects become **two photo rows**; the row is
  created there (`Photo.ProjectID` → `photos.Store.Create` writes `project_id`); and the original is
  published at `proj.StoragePrefix` + the usual `YYYY/MM/<name>` key through `storage.PrefixStorer` (a
  store without it refuses a prefixed upload rather than share an unprefixed key). The bootstrap project's
  prefix is empty and every other one's is `projects/<uid>/`, so **two projects never share a key**
  (`TestIngest_dedupAndKeysArePerProject`). A nil project is refused before a byte is read (`ErrNoProject`,
  a 500 result — a wiring bug, there is no default project);
  `Ingest(ctx,proj,src,filename,uploadedBy)` = a thin wrapper for an upload without a sidecar) `→ FileResult`
  — **project limits** (`limits.go`): the effective per-file cap (`fileCap`) is the smallest non-zero of
  `proj.MaxFileBytes`, `Config.MaxFileSize` (`upload.max_file_size_mb`) and `Request.MaxFileSize` (an upload
  link's `upload_links.max_file_size_mb`); **`preflight`** refuses a `Request.DeclaredSize` over it — and one
  over the quota's remainder, or (no declared size) any file into a project whose quota is used up —
  before a byte is read; **`stage`** stops the copy one byte past the cap; **`CheckQuota(ctx,proj,incoming)`**
  (exported — the HTTP handlers ask it with `incoming = 0` before reading a request's body, via
  `QuotaRefusal(ctx,QuotaChecker,proj)`) reads `Config.Usage` (`project.Store.Usage`, served by the
  `idx_photos_project_file_size` index-only scan, see `docs/PERF.md`) and refuses when stored + incoming >
  `proj.MaxTotalBytes`; it runs again after the exact-dup check (a duplicate stores nothing) and before the
  decode, so before the original is stored. A refusal is a **`*LimitError{Code,LimitBytes,UsedBytes}`**
  (unwraps to `ErrFileTooLarge`/`ErrQuotaExceeded`, marshals as the request-level 413 body) and becomes a
  per-file **413** with `Code` `CodeFileTooLarge`/`CodeQuotaExceeded` and `FileResult.LimitBytes`/`UsedBytes`.
  No lock: concurrent uploads may overshoot the quota by a file each (accepted). A nil `Usage` enforces no
  quota — only the maintenance orphan rescue, which re-catalogues bytes already stored. A multipart caller
  learns the declared size with **`PartSize(part, declared)`** (the part's `Content-Length`, else the
  preceding **`SizeField`** (`"size"`) read by `ReadSizeField`); `import dir` declares the size on disk.
  Then it streams to a temp +
  SHA256, **`admit`** (`sniff.go`: is the content media at all? The leading 512 bytes are matched against
  every signature the pipeline ingests — `imgconvert.MagicFormat`'s rasters/HEIC/TIFF, the non-TIFF RAW
  headers (ORF/RW2/RAF/X3F/MRW), ISO-BMFF/QuickTime boxes (MP4/MOV/3GP/CR3/AVIF), EBML, AVI, ASF, FLV,
  MPEG-PS/TS/M2TS, Annex-B; a **streaming manifest** — `#EXTM3U`, `ffconcat`, `<MPD`, or `<?xml` with an
  `<MPD` behind it, after an optional BOM/whitespace (`isStreamingManifest`) — is `ErrNotMedia` before any
  tool reads it (SEC-017); a file matching none is asked `video.Probe` (named as video) or
  `exif.ExtractNamed` (otherwise) and passes only with dimensions/a duration/a codec or an image/video MIME.
  Anything else is **`ErrNotMedia`** → per-file **415** `Code: CodeNotMedia` (`"not_media"`, the new
  `FileResult.Code`) **before** the dedup lookup, the store and the insert, so junk never becomes a row, an
  original or a job; an **AVIF** — `imgconvert.MagicFormat` = `FormatAVIF`, or a `.avif` name — is
  **`ErrUnsupportedType`** → 415 `CodeUnsupportedType` (`"unsupported_type"`, the code the upload link's
  extension check uses too), checked first because it would pass both sniff and probe), exact-dup check,
  **`verifyPixels`** (`pixels.go`: the staged still is decoded **before it is stored** when its
  `imgconvert.DetectFormatNamed` format is JPEG/PNG/GIF — decoders complete for every real file; a decode
  error that is not a `jpeg`/`png` `UnsupportedError` is **`ErrDamaged`** → 415 `CodeDamaged`
  (`"damaged"`); HEIC/RAW/video/TIFF/BMP/WebP and an image over `MaxPixels` are not judged. The decoded
  image rides along in `stagedPixels` into `postProcess` → `hashPixels`, so the pHash/blurhash reuse it
  instead of decoding the original a second time), metadata (`mediaMeta`: **photo** → EXIF; **video** per `video.IsVideoPath`
  → `media_type=video` + `video.Probe`, requires `ffmpeg` otherwise a per-file error `ErrFFmpegMissing`,
  `taken_at` falls back to the original name via `exif.FilenameTakenAt`; **a probe that fails is non-fatal but
  no longer invisible** — the clip is still catalogued, and the failure is **logged with the file name**
  (`WARN`), both when `Probe` errors and, the commoner shape, when it comes back with
  `!HasContainerMetadata()`: a broken probe or a missing `ffprobe` otherwise produces dozens of
  half-catalogued clips nobody can notice, and the repair route is
  `POST /process/metadata?videos=true`),
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
  search/people); `hashPixels` decodes the stored original **once** and derives from it both the pHash/dHash
  (`storePhash`) and the **blurred placeholder** (`storeBlurhash` → `blurhash.Encode` → `photos.SaveBlurhash`,
  a `blurhash_failed` warning on failure), because the decode is by far the most expensive part of either and a
  second one for a value the size of a tweet would be absurd. The placeholder is encoded from the
  **oriented** image (`imgconvert.Orient`) while the pHash deliberately reads the unoriented one: the
  placeholder stands in for the rendering, the pHash exists to recognise two copies of one shot however
  either is turned; **per-file** `FileResult{Filename,Status,
  Outcome (created/duplicate/error),PhotoUID,Error,Warnings}` — never returns an error, everything is in the result;
  **race**: concurrent identical uploads → one photo (storage hard-link + unique `file_hash`), the loser
  a clean duplicate; **near-dup warning** config-gated via `photos.NearestPhash`, searched **in the new
  photo's own project only** (`photo.ProjectID`; a byte-identical or near-identical file already in
  another project is neither its duplicate nor named in a warning — the same bytes become a photo of the
  uploader's project, per the `(project_id, file_hash)` dedup key); `JobEnqueuer` =
  a TODO hook `EnqueueImageEmbed`/`EnqueueFaceDetect`, default `NopEnqueuer` until the queue exists;
  the three **switchable** enqueuers are separate one-method interfaces (`OCREnqueuer`, `PlacesEnqueuer`,
  `SidecarEnqueuer`), all satisfied by `jobs.Enqueuer`, and **a nil one is how "the feature is off" reaches
  the pipeline** — with no handler registered for that type an enqueued job would wait in the queue for ever.
  `scheduledJobs(photo)` decides which apply: embedding and faces always, `ocr` for stills only (a video's
  poster frame is deliberately not read), **`places` only for a photo that arrived with coordinates** (nothing
  to look up otherwise, and every lookup costs a metered mapy.com credit), `sidecar` always when the export is
  on. The `places` enqueue is what makes an uploaded photo's place fill in **on its own** instead of waiting
  for the maintenance backfill; the credit budget in `internal/placesjob` still bounds the spend, deferring a
  job rather than failing it when the window is empty. A failed enqueue is a `enqueue_failed` **warning**, not
  a failed upload — every one of these jobs is re-schedulable by a backfill;
  the thumbnails are stamped `photos.thumbnails_at` (`MarkThumbnailsBuilt`) only **after** every size was
  generated, so the processing report reads them from the thumbnails rather than from the pHash written ahead
  of them; **the caller's context governs only the staging read** — everything after it runs on
  `context.WithoutCancel` bounded by `processTimeout` (30 min), because a client that hangs up after its last
  byte used to cancel the pipeline between the stored original and its thumbnails/jobs (2026-10-05); **a
  duplicate completes its photo** (`completeDuplicate`, on both the pre-insert lookup and the lost insert
  race, live photos only): `GenerateAll` fills any missing size (a stat per size, or one listing on an object
  store, when nothing is missing), `MarkThumbnailsPresent` stamps it without moving an existing stamp, and the
  optional `Config.Pending` (`PendingScheduler`, satisfied by `processing.Service`) schedules every `pending`
  step; failures are logged, the result stays `duplicate`;
  `API` = `NewAPI(svc, requireAuth, rateLimit)` + `RegisterRoutes` mounts `POST /upload` behind
  `RequireAuth` (every signed-in role, viewers included; filing into albums/labels stays curation);
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
  (**import of a directory from disk** — `kukatko import dir --project <slug> <path>`; `Service` = `New(Config{Ingest,Runs,
  Photos,Filler,Curation,Albums,Labels,Project,Concurrency,Logger})` — `Project` (`*project.Project`) is **the one
  project of the whole run** (required: `Import` without one is `ErrNoProject` and touches nothing): every file goes
  to `ingest.IngestFile` with that project (catalogued there, original under its prefix, its size on disk passed
  as `DeclaredSize` so a file over the project's/instance's cap or its quota is refused — a failed file of the
  run — before it is read), the dry
  run and the duplicate report look up `GetByFileHashInProject`/`GetByUIDInProject` (the same bytes in another
  project are new here), the run is `RunStore.Start(ctx, projectUID, SourceFolder)`, and `--album`, `--labels` and
  the export's favourites/ratings resolve there; the CLI resolves `--project` by slug and refuses a deactivated
  project (`resolveImportProject`). It reads only the foreign sidecars beside the media on disk, never the exported
  `sidecars/` tree, so no project filter is needed for them) with `Import(ctx, Options{Root,Recursive,
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
  Embedder,Faces,Attacher,Favorites,Ratings,RequireAuth,RequireCurator,RequireWrite,RequireAdmin,RequireDownload})`
  — **`RequireCurator` guards the three people-and-faces writes** (`POST /photos/{uid}/faces/assign`,
  `POST`/`DELETE /photos/{uid}/people…`, curator and above), **`RequireAdmin` guards only the irreversible trash operations** (`POST /trash/empty`, per-photo
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
  a **task-scoped** listing (`ListParams.TaskUIDs` set, `taskScoped`) additionally stamps
  **`last_comment`** on every row (`lastcomment.go`: `annotateLastComments` asks
  `comments.LatestAmong` once for the page; the field is a tri-state `lastCommentField` — absent from
  the JSON outside a task scope via `omitzero`, an explicit `null` for a row with no live comment,
  the compact `{uid,body,author_uid,author_name,created_at}` otherwise — because a plain pointer
  with `omitempty` could not tell "not asked" from "asked, none"); it is what the review ledger and
  `ctl tasks ledger` read per row, and no other listing pays for it;
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
  rating endpoints 503); both stores, and the detail's album/label chips (`PhotoOrganizer`), take the
  request's project — a favourite, rating or membership is read and written only for the project's photos;
  **one face as its own rendition** (`facecrop.go`): `GET /photos/{uid}/face?box=x,y,w,h` (`RequireDownload`,
  the same guard as a thumbnail, so an `<img>` works from a cookie or a `?t=` token) streams the small square
  JPEG of that one face through the `FaceCropRenderer` interface (satisfied by `*avatar.Renderer` — the very
  renderer the subject avatar is cut by, so the padding/squaring/source-rung geometry exists once, nil-safe →
  503). `parseFaceBox` takes four comma-separated numbers in the photo's **display** space, rejecting anything
  non-finite, of no size, or lying wholly outside the frame (→ 400) while letting a box that merely *overhangs*
  an edge through — the renderer slides it back inside rather than clipping, and that is the geometry to keep.
  404 for an unknown photo, logged 500 for a render failure. `private, max-age=31536000, immutable` + the
  renderer's ETag: unlike the avatar's, this URL names a photo and an exact box, so what it answers is as fixed
  as a thumbnail's. It always streams (the rendition is cache-only, never in the object store) and never
  redirects. It exists because a face shown as a small square used to cost a photograph: the outlier section of
  one person's page fetched 290 `fit_1280` previews to paint 290 tiles of 96 px;
  **HLS streaming** (`hls.go`, the `HLSRenditions` interface satisfied by `*hlsjob.Store`, nil-safe → all
  three routes 404 and the payload flag false): `GET /photos/{uid}/hls/master.m3u8`,
  `/hls/{rendition}/index.m3u8` and `/hls/{rendition}/{segment}`, all `RequireDownload` like `/video`. The
  playlists are `application/vnd.apple.mpegurl` and are rendered by `internal/hls`'s pure functions
  (`BuildMaster` over the rendition rows, `RewriteMedia` over the text ffmpeg wrote) — **no playlist text is
  assembled here**. The segment route validates the name with `hls.ValidateName` before `hls.Key` builds a
  key from it (the guard between a request path and the store), checks `Has` (an `EXISTS`, so a fragment
  request never drags a playlist column along) and answers **302** to `mediaurl.Builder.Object`'s freshly
  signed URL via the shared `redirectToMedia` (`private, no-store` — a cached redirect would outlive its
  signature and 403 mid-clip), or streams the object with its own MIME (`video/mp4` / `video/iso.segment`)
  when the backend publishes no URLs. **The URIs point at the application, not the CDN** — that is what lets
  each segment be authorised and signed as it is asked for — and are relative, so they resolve under any API
  base path; they repeat the `?t=` download token when the request carried one, because a player fetches each
  URI as a bare GET and a cookie-less `<video>` would otherwise 401 on its first segment. Every absence is a
  404: no rendition, an unknown or malformed rendition name, a segment name the layout cannot hold. The
  detail response carries `hls` (bool, from `HasAny`) so the player never probes — and so does every
  list/search row, which `internal/photos` computes inside the listing query itself (`photoListColumns`)
  rather than asking per tile; the key is absent for anything that is not a standalone video, and `false`
  is what a grid tile draws its "still being prepared" mark from;
  **thumbnail regeneration** (`thumbnail.go`): `POST /photos/{uid}/regenerate-thumbnail` (editor/admin via
  `RequireWrite`, the `ThumbnailRegenerator` interface satisfied by `*thumbjob.Service`, nil-safe → 503) synchronously
  overwrites the thumbnails + pHash via `ForceRegenerate` and returns `{status,sizes}` (200), 404 missing photo,
  **422** `thumbjob.ErrRegenerateFailed` (the original is missing/undecodable); best-effort audit `photo.thumbnail`
  via `AuditRecorder` (`*audit.Store`, a failure is only logged — the thumbnail is already regenerated);
  **the other three rebuilds** (`rebuild.go`): `POST /photos/{uid}/reembed`, `/redetect-faces` and `/regeocode`
  (**maintainer** via `RequireMaintainer` — a rebuild discards stored work, so it is held to the stricter of its
  two neighbours), over the `PhotoReembedder`/`PhotoRedetector`/`PhotoRegeocoder` interfaces (satisfied by
  `*embedjob.Service`/`*facejob.Service`/`*placesjob.Service`, each nil-safe → 503). They exist because
  `process/{step}` cannot do this: it enqueues the *repair*, which skips a photo that already has the data and so
  answers 200 having changed nothing. Each calls the job service's `Force…` path synchronously and answers
  `{step,status,faces?}` — `status` `rebuilt`, `faces` the count **after** a re-detection (0 is a result).
  A backing service that is merely asleep is recognised via `worker.IsDeferral` and the forced job is enqueued
  through the `RebuildEnqueuer` (`jobs.Enqueuer`) instead → `status:"queued"`, 200; with no enqueuer wired the
  outage is an honest 503. `queueRebuild` branches on the `jobs.ForceOutcome` the enqueue reports: `ForceInFlight`
  — a job of that type for this photo is already **running**, so the force could only be dropped and the run will
  take its idempotent skip — is a **409** telling the caller to retry once it finishes; `ForceScheduled`/
  `ForceUpgraded`/`ForceAbsorbed` all really do end in a forced job and keep the 200 `queued`. That branch is the
  whole reason the outcome crosses the package boundary; resolving the collision itself belongs to
  `internal/jobs`, so all three endpoints inherit it. 404 missing photo, 500 otherwise. Best-effort audit `photo.embedding`/`photo.faces`/
  `photo.place` carrying the status (and the face count) in details, by the same rule as `photo.thumbnail`;
  **the two video rebuilds and the rendition inventory** (`renditions.go`):
  `POST /photos/{uid}/regenerate-storyboard` (**maintainer**, over `StoryboardService.Regenerate`, nil-safe → 503)
  discards a clip's cached scrub-preview sprite and queues a fresh render — the sprite's *only* way back, since
  the lazy `GET /storyboard` schedules nothing for a clip that already has one and the renderer skips it anyway.
  It answers `{step:"storyboard",state:"queued"}` — the processing report's vocabulary, deliberately, because
  the work is scheduled and not run, and `queued` rather than `storyboardjob.StatePending` because there
  `pending` means the opposite, that nothing was scheduled — 409 for a photo that can never have a preview (`ErrNotAVideo`,
  `storyboard.ErrNoDuration`), 503 for `storyboardjob.ErrCannotRender`, 404 missing photo; best-effort audit
  `photo.storyboard`, by the same rule as the other rebuilds. `storyboard` is **not** one of
  `processing.Steps` and the step-repair endpoint still refuses it: it leaves no persisted evidence, so the
  report has nothing to say about it. The **encode** rebuild needs no endpoint of its own —
  `POST /photos/{uid}/process/hls_transcode` already is one, since that handler re-encodes wholesale rather
  than skipping a clip it has already done. `GET /photos/{uid}/renditions` (**RequireAuth** — a read that says
  no more than the master playlist the caller may already play) answers `{renditions:[…]}` from
  `HLSRenditions.ListForPhoto`, widest first, each row `{rendition,width,height,bandwidth,codecs,segment_count,
  duration_ms,encoded_at}` — **the playlist text left behind**, being the largest column of the row and already
  served, rewritten, by the media-playlist route. The photo is looked up first, so an unknown uid is a 404 and
  never an empty inventory reading as "not encoded"; a still, an unencoded clip and an instance with no HLS
  reader all answer an empty **list**, never a null. Nothing else exposes when a rendition was made, which is
  what "is what is stored still current?" needs after a changed rendition plan or a restore;
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
  the response = the list shape + `mode` (effective) + `ranked_total` + `degraded`; **box offline**
  (`Embedder` nil or `embedding.IsUnavailable`) → `semantic`/`hybrid` fall back to fulltext with
  `degraded: true`;
  **`total` is two different numbers and the response says which** (`searchResult.ranked` →
  `ranked_total`): fulltext, a filter-only query and a degraded fallback run a real `store.Count`, so
  their total is exact and the flag stays false; the ranked modes report the **size of the ranked set
  they built out of a bounded candidate pool** (`semanticCandidatePool`=500 nearest neighbours,
  `fusionPool`=200 from each ranking before `fuseRRF`), which saturates there however many photos
  actually match — so it is the number of best matches returned, and the flag makes the client word it
  as such instead of as a library total;
  **`no_text_match`** (`searchResult.noTextMatch`, hybrid only): the full-text pool fed into the fusion
  was empty while the fused set was not — every result is a semantic neighbour, and nearest-neighbour
  search has no relevance floor, so gibberish gets a full pool; the flag lets the client say nothing
  matched exactly instead of calling them best matches (deliberately no cosine cutoff — no measured
  threshold separates a real visual match from noise, and the neighbours are the feature);
  **`video_total` rides with whichever total it is** (`searchResult.videos` → `video_total`): the
  counted paths take it from `store.CountMedia` (one extra aggregate over the same scan), the ranked
  ones from `countVideos` over the ranked uids they already hold in memory, so the breakdown never
  costs a second query. It is what lets the client word a count for what it counts — a page of clips is
  not a page of photographs — with a live photo counted among the stills;
  the `TextEmbedder` interface (fakeable, satisfied by `embedding.Client`); `PATCH` is
  partial via raw-key presence (an omitted field unchanged, `null` clears a nullable one, coordinate
  validation); media `thumb/{size}`+`download` **stream** via `io.Copy` with `streamMedia`
  (`Cache-Control`/`ETag`/`304`, `Content-Length` from the DB, the thumbnail generated on-miss),
  guard `RequireAuthOrDownloadToken` = a session cookie or `?t=download_token`; `OptionalAuth` (auth
package) puts a valid credential's principal on the context and lets an anonymous request through — the
public upload-link upload uses it to attribute a signed-in uploader; `RequireSignedIn` (auth package)
admits any authenticated account **without entering a project** (no membership consulted) — the
instance-level invitation accept, for an account that belongs to no project yet; `RequireActiveProject`
(auth package, a plain function — it needs no API) admits **anybody, signed in or not**, into an
existing, active project named by `WithProject` and records it for `PublicProjectFromContext` only —
never as an authorised project, so `ProjectFromContext`/`ProjectUIDFromContext` stay empty behind it and
no project-scoped store can mistake the request for an admitted one; an unknown slug, a deactivated
project and a request that never passed `WithProject` are the one shared 404 — the guard of the one
anonymous project route, `GET /p/{project}/settings/public`; **video streaming**
  (`video.go`): `GET /photos/{uid}/video` streams video **with HTTP Range** via `http.ServeContent`
  (206 partial, `Accept-Ranges`, seek, If-Range/If-None-Match, memory-bounded from `*os.File` via
  `storage.Materialize`, once per request — the transcode fallback shares it too) for inline HTML5
  playback; a live photo serves its **motion clip** sidecar
  (`pickMotionClip` by video MIME/extension), a still image → 404; **on-the-fly transcode** gated by
  `VideoConfig`/`video.transcode` (default off) + `video.IsWebFriendlyCodec` + `video.FFmpegAvailable`
  → `video.Transcode` (H.264/MP4 progressive, no range, `no-store`), falls back to the original when
  ffmpeg fails or the codec is unknown; **the non-destructive
  edit** via `Organizer` (the detail's album/label chips); **the detail's uploader** via the `UserResolver`
  interface (satisfied by `auth.Store.ProjectUserName`, wired by `buildPhotoAPI`): `handleDetail`
  resolves `photo.UploadedBy` → `uploader{uid,name,former}` (`name` = `display_name`, fallback `username`;
  a former member of the request's project is nameless with `former` set),
  nil-safe (not wired / no uploader / an unresolvable user → `uploader` omitted, only on the
  detail, no N+1 in the list); **the detail's upload link** (`uploadlink.go`) via the
  `UploadLinkProvenance` interface (satisfied by `uploadlink.Store.Provenance`): `resolveUploadLink`
  attaches `upload_link{uid,title,uploader_name?,account?,uploaded_at}` **for curators and above only**
  (the caller's role from the request context), nil-safe and best-effort (a failed lookup is logged and
  omitted); **the detail's place** (`place.go`) via the `PlaceResolver` interface
  (satisfied by `places.Store.GetPlace`): `writeDetail` attaches `place{country,region,city,place_name}`
  from the `photo_places` cache — **cache-read only, the detail never geocodes** (mapy.com credits are
  metered; the on-demand lookup stays in `mapsapi`), nil-safe just like the uploader and also omitted for a
  "processed" marker (a row with all levels empty); **rescheduling that cache** after a metadata edit
  through the `PlacesEnqueuer` interface (also `place.go`, satisfied by `jobs.Enqueuer.EnqueuePlaces`,
  `Config.Geocodes`, nil-safe): `handleUpdate` compares the row before the edit with the row the store
  stored (`coordinateMoved`) and enqueues the `places` job only when the photo actually moved or lost its
  location, because a geocode costs a metered credit and a retitle must not spend one — a cleared location
  is enqueued too, since the job answers it with the empty "processed" marker that takes the photo out of
  the hierarchy; best-effort and after the commit, exactly like `enqueueSidecar` (the job re-reads the row,
  so enqueuing earlier would have it compare against the old coordinates and skip);
  `EditService`/`edit.go`+`media_edit.go`
  (`GET`/`PUT /photos/{uid}/edit`, download honours the edit via `internal/photoedit` — the render decodes the
  original in-process, so it is gated by `Config.MaxPixels`/`Config.DecodeBudget` (`imgconvert.ReserveDecode`,
  bitmap + three full-size copies); an original over either is served unedited; a saved edit also
  **audits** `photo.edit` through the same `AuditRecorder` as the thumbnail action — after the write, because
  `Store.SetEdit` exposes no transaction to join — and **enqueues a forced thumbnail rebuild** through the
  `ThumbnailEnqueuer` interface (`jobs.Enqueuer.EnqueueThumbnailRebuild`, nil-safe), so the grid stops showing
  the previous rendering; both are best-effort, exactly like `enqueueSidecar`, and the **neutral** edit is no
  special case — a reset audits and rebuilds too); and the **comment
  thread** (`comments.go`, over the `CommentStore` interface satisfied by `comments.Store`):
  `GET`/`POST /photos/{uid}/comments` + `PATCH`/`DELETE /photos/{uid}/comments/{commentUID}`, all four behind
  **`RequireAuth`** — the create deliberately so, a viewer may comment, see docs/API.md — with the per-user
  throttle (`Config.CommentRateLimit`, nil = none, resolved by `commentThrottle`) mounted *inside* the auth
  guard (likewise `Config.VectorRateLimit`, resolved by `vectorThrottle`, on `GET /search` and
  `GET /photos/{uid}/similar` — the per-user-then-per-project vector buckets); the pure predicates `canEditComment` (author only) / `canDeleteComment` (author **or**
  `Role.IsAdmin()`) hold the authorization decision and are made against the **stored** row —
  `resolveComment` reads it first and answers 404 when it belongs to a different photo than the path names,
  so the route cannot be used to probe which comment UIDs exist; `writeDetail` stamps **`comment_count`** via
  `commentCount`, which asks the bulk `CountsAmong` for the one UID so the detail can never grow an N+1)),
  `internal/comments/`
  (the store behind those endpoints — **project scope:** a comment carries no project of its own and
  belongs to the project of the photo, task or album it hangs off; every `Store` method takes
  `projectUID` after `ctx` (`List`/`CountsAmong`/`LatestAmong`/`Get`/`Create`/`CreateAlong`/`Update`/
  `Delete`), never defaulted (`ErrNoProject`); the per-kind reads **join the parent confined to the
  project** (`parentInProjectJoin`), the uid-addressed reads and writes check whichever subject the row has
  (`inProjectSQL`), and the insert is an `INSERT … SELECT … WHERE EXISTS (parent in project)` — a subject
  of another project is `ErrSubjectNotFound` with nothing written, another project's comment is the same
  `ErrNotFound` as a missing one, and audited writes default the entry's `ProjectUID`; with `LatestAmong(ctx, subject, uids)`, the **newest live comment
  per subject** for a whole page in one `DISTINCT ON (subject uid) … ORDER BY created_at DESC, uid DESC`
  query joined to the author's name, soft-deleted rows excluded, a map keyed by subject uid with no entry
  for a thread that has none — the task-scoped listing's `last_comment` is built from it —
  the `comments` table from migration `0052_photo_comments.sql`,
  renamed and given a second subject by `0080_comments_subject.sql` and a third by
  `0099_comments_album.sql` (a comment hangs off a photo, a task **or an album** — `album_uid` FK
  **CASCADE**, partial index `(album_uid, created_at)` — exactly one of the three, enforced by
  `comments_one_subject` via `num_nonnulls`; `AlbumSubject(uid)`, the album thread's endpoints in
  `internal/organizeapi/comments.go`, its mention text "… v komentáři u alba" in `internal/mentionnotify`; the store takes a `Subject` and builds
  its per-kind statements once at package initialisation, so no request ever assembles SQL):
  `uid PK` (prefix `cm`), `photo_uid` FK **CASCADE** (purging a photo takes its thread with it), `author_uid`
  FK **SET NULL** (a comment outlives its author's account: authorless, and thereafter editable by nobody),
  `body TEXT` with `CHECK (char_length(body) BETWEEN 1 AND 2000)` mirroring `normalizeBody` (trim →
  `ErrEmptyBody`/`ErrBodyTooLong`, counted in **runes**, so a Czech comment gets the same allowance as an
  English one), `created_at`, `edited_at` (NULL until the first edit — that is what lets a client mark a
  comment as edited without comparing timestamps) and `deleted_at` (NULL while live), plus the partial index
  `(photo_uid, created_at) WHERE deleted_at IS NULL` serving both the thread read and its count;
  `NewStore(pool)` → `List(photoUID)` (live only, oldest first, `author_name` resolved by a LEFT JOIN on
  `users`: `display_name`, fallback `username`, empty for a deleted account, so one query renders a thread —
  and `author_photo_uid` by LEFT JOINs through the author's membership of the comment's project
  (`project_members.subject_uid`, the project read off the photo/task/album by `commentProjectSQL`) onto
  `subjects.cover_photo_uid`, so the thread can draw the author's face where it would otherwise draw a
  letter; it is NULL for an authorless comment, an account unlinked in that project and a linked person
  with no cover photo, which is the common case; a mention's `subject_uid` is read the same way),
  `CountsAmong(photoUIDs)` (one `GROUP BY`; deliberately the bulk shape even though the detail asks about one
  photo, so a per-photo count cannot be written by accident), `Get(uid)` and
  `Create`/`Update`/`Delete` — each a CTE that mutates and reads the row back with its author in one
  round-trip, run through the shared `mutateAudited`: begin → mutate → `audit.Write` on the **same
  transaction** → commit, so a comment that exists always has a record of who wrote it;
  **`CreateAlong(…, along Along)`** is `Create` with a caller's further write (`Along func(ctx, pgx.Tx)
  error`) run on the same transaction after the comment and its audit row and before the commit — its error
  rolls the comment back and reaches the caller **unwrapped** (so a `phototask` sentinel stays
  classifiable); `Create` is `CreateAlong` with `nil`. It exists for the task answer that moves the task
  ("Odeslat a předat agentovi"), so the package still knows nothing about tasks;
  `entryWithComment` stamps the affected comment's UID into `details.comment_uid` (the audit target stays the
  **photo**) on a **copy** of the caller's details map. **Delete is soft** — `deleted_at` is stamped, the row
  stays, every read filters it out — so deleting twice, or editing a deleted comment, is `ErrNotFound` rather
  than a silent success. A body is plain text in and plain text out: nothing is rendered or sanitised
  server-side, so the client escapes what it displays. **The one thing the store reads out of a body is who
  it mentions** (`mention.go`): **`ParseMentions(body)`** finds every `@[Name](user:<uid>)` token
  (`mentionPattern` — an `@` straight before a Markdown link to the `user:` pseudo-scheme and an account
  uid of letters, digits, `_`, `-`; a bare `@Name` is never matched) **outside code** — `stripCode` blanks
  fenced blocks (``` or ~~~, closing fence at least as long, unclosed runs to the end), indented blocks
  (four spaces or a tab after a blank line or at the top) and inline code spans (a backtick run closed by
  one of the same length; an unclosed run is literal), so what the renderer shows as code is not a mention
  here either — and returns `[]MentionRef{UserUID, Name}`, each account once, in order of first mention;
  `MentionToken(name, uid)` writes the token; `Excerpt(body, limit)` is the notification's quote (tokens as
  `@Name`, whitespace collapsed, cut with an ellipsis at `MaxExcerptLen` 140). On **every create and
  edit** the shared `mutateAudited` runs a `postStep` (`mentionsThen`, `store_mentions.go`) after the audit
  row and before the caller's `Along`: **`syncMentions`** resolves the refs on the transaction against the
  accounts (`resolveMentionsSQL`: existing, `NOT disabled`, an approved member of the comment's project by
  `project.ApprovedMemberSQL` — a maintainer outside the project included, the rest are dropped), prunes the `comment_mentions` rows the body no longer carries, upserts the rest at their body
  position, sets `Comment.Mentions` (the create's read-back ran before any row existed) and reports the
  **fresh** ones — not in the previous set, not the author's own (a self-mention is stored, so it renders,
  but is nobody's news) — to the **`MentionObserver`** (`WithMentionObserver`, a copy of the store like
  `people.WithTagObserver`; `Mentioned(ctx, tx, Comment, []Mention)` on the comment's transaction; an error
  fails the comment, so an observer that must not cost one contains its failures, see
  `internal/mentionnotify`). Every read resolves the mentions afresh: `commentColumns` ends in
  **`mentionsColumn`**, a correlated `jsonb_agg` over `comment_mentions ⋈ users` ordered by position,
  `'[]'` when none, scanned into **`Comment.Mentions []Mention{UserUID, Name, SubjectUID *string}`**
  (`json:"mentions"`, never nil) — so a renamed account reads under its new name in every old comment and a
  deleted one (its rows cascaded away) is simply absent, the client falling back to the token's own name.
  **A former member** — an account that still exists but has no membership of the comment's project any
  more, whatever its instance role (`authorFormerSQL`/`mentionFormerSQL`, over `authorJoin`'s and the mention's
  `project_members` LEFT JOIN) — reads with **no name** and `Comment.AuthorFormer` / `Mention.Former` set (no
  person either), so the project stops seeing a live name renamed later elsewhere (CAT-4,
  `TestRead_formerMembers`); `internal/photoapi`'s last-comment field carries `author_former` too.
  Table `comment_mentions` (migration `0098_comment_mentions.sql`: `(comment_uid, user_uid)` PK, both FKs
  `CASCADE`, `position`, `idx_comment_mentions_user (user_uid, created_at DESC)` for a later "mentions of
  me"; catalogue in `internal/reset`). A soft-deleted comment keeps its rows. Tests: the parser table (token
  forms, duplicates, bad uid, every code case), the token round-trip and the excerpt as unit tests;
  over a real database — create resolves, stores, returns and reports (an unknown uid, a disabled and an
  unapproved account dropped, the writer's own left out of the report), a rename shows on the next read, a
  deleted account drops out, a plain comment carries `[]` and never reaches the observer, an edit prunes
  and reports only the new names, re-mentioning a dropped name is fresh again, a task thread resolves the
  same, a soft delete keeps the rows. `translateMutation` maps no-rows → `ErrNotFound` and a
  `photo_uid` foreign-key violation → `ErrPhotoNotFound`), `internal/photoedit/`
  (**CGO-free application of a non-destructive edit** to a decoded image for download/preview: `Apply(img,
  photos.Edit) image.Image` applies **crop** (normalized `[x,y,w,h]` 0..1), **rotation** 0/90/180/270
  and **brightness/contrast** (a linear scale around 0.5, maps 1:1 to the frontend CSS `brightness(1+b)`/
  `contrast(1+c)`), pure-Go via `golang.org/x/image`; `IsIdentity(edit)` skips a no-op; `orient.go`
  = EXIF orientation; identity = passthrough of the original, otherwise render to a JPEG), `internal/trash/`
  (permanent deletion (purge) of soft-deleted photos, all behind the interfaces `PhotoStore`/`ProjectLister`/
  `FileStorage`/`ThumbStore`/`RemoteRemover` (unit-testable with fakes): `Service` = `New(Config{Photos,Projects,
  Storage,Thumbnailer,Remote?,Moves?,RetentionDays,BatchSize,Logger})` (panics on nil Photos/Projects/Storage/Thumbnailer);
  **during a storage move** — with `Moves` (a `MoveLock`, `*rekey.Gate`) — each photo is purged inside
  `Moves.Purge`, the move's photo lock held shared and **waited for** while the move copies that photo, so a
  purge and a move never interleave on one photo (the move deletes the copy it made of a photo purged before
  it committed); a photo a crashed move has yet to finish is refused with `storekeys.ErrMoving` (a single
  purge returns it — 503 over HTTP —, a batch counts it `failed`);
  **project scope:** every operation works inside one project — `PhotoStore` is the scoped
  `GetByUIDInProject`/`ListFiles`/`DeleteAudited(projectUID,…)`/`ListArchivedUIDs(projectUID,…)`, so a foreign
  uid is `photos.ErrPhotoNotFound` exactly like a missing one and emptying one project's trash never reaches
  another's; the scheduled retention purge has no request, so `PurgeExpired` lists the projects
  (`ProjectLister`, `*project.Store`) and sweeps **each active one** in turn (a deactivated project's data is
  kept; one project's failure is logged and the next still runs); the keys a purge deletes come from the
  photo's own file rows (`file_path` for the original and its sidecar, `file_hash` for the derived media),
  never recomputed from the project, so a photo is cleaned up wherever its rows say it lives;
  **purgeOne** deletes a photo's artifacts (the original via `Storage.Delete`, the cached thumbnails via
  `Thumbnailer.Remove`, the **thumbnail objects the store holds** — everything under
  `thumb.ObjectPrefix(prefix, file_hash)` (`<prefix>thumb/aa/bb/cc/<hash>_`, a retired size included) when the
  backend is a `storage.PrefixLister`, else each registered size's `thumb.RelPath` key; a publishing backend
  (R2) keeps every size there, and the cache removal never reached them —, the scrub-preview sprite via
  `StoryboardStore.Remove` (cache-only, never stored), the **HLS segments** — everything the store holds
  under `hls.PrefixFor(prefix, file_hash)` — found by listing that prefix when the backend is a
  `storage.PrefixLister`, because their keys are not derivable from the catalogue; a backend that cannot list
  and a hash the layout cannot address leave them behind rather than failing the purge, while a failed
  listing or deletion is reported — optionally the S3 object via `RemoteRemover`; `prefix` is that of the
  file's own original, `storekeys.PrefixOf(file_path)`, and for a file under a prefix the derived media are
  looked for **under the root too** (`derivedLayouts`) once `ProjectLister.List` shows no project left in the
  root layout (`rootVacatedFor`): the root keys named after its hash are then the leftovers of a storage
  move's failed derived copy, which would otherwise outlive the photo, its `storage_rekey_derived_failures`
  row cascading away; while another project still uses the root they may be that project's and are kept)
  **and then** the DB row via
  `photos.DeleteAudited(projectUID,uid,entry)` — deletes the row (cascading embeddings/faces/markers/album_photos/
  photo_labels/phashes/edits/favorites via `ON DELETE CASCADE`) **and writes a `photo.purge` audit row
  in the same transaction** (durable-audit; rollback ⇒ no audit row); artifacts first, so an
  interrupted purge leaves a re-purgeable row instead of dangling files; idempotent (a missing
  file/`os.ErrNotExist`/`thumb.ErrInvalidHash` is ignored); `PurgePhoto(projectUID,uid,meta)` (404
  `photos.ErrPhotoNotFound`, `ErrNotArchived` for a live photo), `EmptyTrash(projectUID,meta)` (purge of all
  the project's archived), `PurgeOlderThan(projectUID,days,meta)` and `PurgeExpired()` (only `archived_at`
  older than `RetentionDays`, ≤ 0 = no-op, every active project)
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
  running job wrote the file before it saw that edit; other types keep queued|running dedup), plus
  `idx_jobs_family_export_dedup` on `(payload->>'project_uid') WHERE type='family_export' AND state='queued'`
  (migration `0075` keyed it on the type alone, `0106` on the project): the genealogy export carries **no**
  `photo_uid`, so the index above cannot see it and every enqueue would be distinct — the queued-only scope is
  the sidecar's reasoning applied to the one file per project. Both index names map to `ErrDuplicate`
  (`dedupIndexes`);
  **the three job categories** (`project.go`, the package doc) — every type is exactly one of:
  **per-photo** (`thumbnail`, `image_embed`, `face_detect`, `metadata`, `ocr`, `sidecar`, `places`,
  `storyboard`, `hls_transcode`; payload `{photo_uid}`, deduped on (type, `photo_uid`), the handler reads the
  project from the photo row through the photos store's `ForJob` loader; `album_featured_notify` is the same
  shape keyed on an album), **library-wide** (`face_cluster`, `family_export`, `nameless_detach`,
  `nameless_restore`, `task_digest`, `notification_purge`, `storage_rekey` = `TypeStorageRekey`
  (`internal/rekeyjob`); `IsLibraryWide(type)`) whose payload must name the project as `project_uid`
  (`ProjectPayload{ProjectUID}`) — **`Enqueue` and `EnqueueOrSkip` refuse one without it with
  `ErrProjectRequired`** before any SQL runs (so a caller's transaction survives), and the handler reads it
  back with **`ProjectUIDOf(job)`** (`""` for a payload naming none), never from a global; the four handlers
  that must still run a pre-upgrade job (`face_cluster`, `family_export`, `notification_purge`, the nameless
  repair) read it with **`ProjectUIDOrLegacy(ctx, log, job, project.BootstrapUID)`**, which falls back to the
  bootstrap project — the only one a job queued before the field existed can mean, and since `Enqueue`
  refuses one without it the only job that can lack it — **logged at warn level** naming the job, so the
  fallback is never silent and an unexpected one (a hand-inserted row, a regressed producer) is seen — and
  **instance-wide** (`backup`, `mail_send`, `push_send`, `tag_notify` keyed per account and grouping by
  project inside its run). The library-wide dedup indexes key on **(type, `payload->>'project_uid'`)** plus
  the job's further identifier: migration **0108** rebuilds `idx_jobs_notification_purge_dedup` (on the type
  alone since 0096), `idx_jobs_face_cluster_dedup` and `idx_jobs_family_export_dedup` (on the project alone
  since 0105/0106) to that one shape, all still `state='queued'` only; the task digest keeps (project,
  person) from 0107 and the nameless repair (type, project, subject) from 0105. **`storage_rekey`**
  (migration **0113**, `idx_jobs_storage_rekey_dedup`) keys on (type, project) over **queued and running**
  jobs alike: its one job requeues its own row (`Defer`), so a twin beside a running one would break that
  `UPDATE`; **`EnqueueStorageRekey(ctx, exec, projectUID)`** (`rekey.go`) enqueues it through
  `EnqueueOrSkip` on the caller's transaction and, on a duplicate, makes a queued job that waits for later
  runnable now (a resume does not wait out a paused job's minute). Every scheduler-style trigger
  enqueues one job per **active** project (`project.Active` over `project.Store.List`): the daily digest,
  the recluster trigger, the notification purge (the trash retention purge sweeps the active projects in
  process, no job);
  `Store` = `NewStore(pool)` with
  `Enqueue(ctx,type,payload,opts)` (idempotent on the dedup key → `ErrDuplicate`,
  `EnqueueOptions{Priority,MaxAttempts,RunAfter}`) — a thin wrapper over the **package-level
  `Enqueue(ctx, exec, type, payload, opts)`, whose `exec` is an `Execer` (`QueryRow`; both `*pgxpool.Pool`
  and `pgx.Tx` satisfy it), so a job can be scheduled **inside the caller's transaction** exactly the way
  `audit.Write` records a mutation: a registration that rolls back schedules no mail, one that commits
  schedules it once. A payload with no `photo_uid` (a mail, a backup) never dedupes, NULLs being distinct
  in a unique index — except `tag_notify`, whose own index `idx_jobs_tag_notify_dedup` (migration 0089)
  keys on the payload's `user_uid` while **queued**, one open tagging window per account, and
  `album_featured_notify`, whose `idx_jobs_album_featured_notify_dedup` (migration 0097) keys on the
  payload's `album_uid` while queued, one pending announcement per album, and the library-wide jobs that
  carry `project_uid` (migration 0105): `idx_jobs_face_cluster_dedup` keys `face_cluster` on the payload's
  `project_uid` (one queued pass per project) and `idx_jobs_nameless_dedup` keys `nameless_detach`/
  `nameless_restore` on (type, `project_uid`, `subject_uid`), both while queued; a payload queued before the
  field existed has a NULL key and never collides. **`CountPendingForProject(ctx,projectUID,types...)`**
  counts the queued-or-running jobs of the types whose payload names the project (0 for no types or no
  project) — the per-project pending check of `internal/clusterjob`.
  **`EnqueueOrSkip(ctx, exec, type, payload, opts)`** is the same insert with `ON CONFLICT DO NOTHING`:
  a duplicate is still `ErrDuplicate`, but it **does not abort the caller's transaction** the way a
  unique violation would — for a caller that enqueues "unless already scheduled" in the middle of its
  own mutation (the tagging recorder, `internal/tagnotifyjob`);
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
  hopeless for a dead letter of thousands)/`List`(`ListOptions{State,Project,Limit,Offset}` →
  `[]ListedJob{Job, Project}`, ordered updated_at DESC, limit cap 500, for the admin listing: `Project` is the
  slug of the job's project — joined on `payload->>'project_uid'` for a library-wide job, through the photo
  for a per-photo one, `""` for an instance-wide one — and `ListOptions.Project` keeps only that slug's
  jobs, an unknown slug none)/`Get`; sentinels
  `ErrDuplicate`/`ErrNoJobs`/`ErrJobNotFound`/`ErrLockLost`/`ErrNotDead`/`ErrNoActiveJob`/`ErrEnqueueRaced`/
  `ErrProjectRequired`;
  `UpgradeToForced(ctx,type,photoUID,payload)` rewrites the payload of the **queued** job covering that photo to
  the forced one and reports what the collision was — `ForceUpgraded` (a queued plain job was rewritten),
  `ForceAbsorbed` (it was already forced) or `ForceInFlight` (it is running and beyond reach), `ErrNoActiveJob`
  when it finished meanwhile. **One statement** on purpose (a CTE that describes the collision, an `UPDATE`
  repeating `state='queued'` in its own `WHERE`, and a `SELECT` reporting both): a read-then-write could stamp the
  forced flag onto a run that already read its payload, and PostgreSQL re-checks that `WHERE` against the committed
  row if a worker claims it concurrently — so the flag is never lost *and* never applied in flight. It inserts
  nothing: the active-job count for that photo and type is unchanged; **job types** `image_embed`/
  `face_detect`/`thumbnail`/`places`/`metadata`/`ocr`/`sidecar`/`family_export`/`storyboard`/`mail_send`/
  `push_send`/`nameless_detach`/
  `nameless_restore`/`face_cluster`/`pp_import`/`ps_migrate`/`backup`; `Enqueuer` =
  `NewEnqueuer(store)`
  implements `ingest.JobEnqueuer` (`EnqueueImageEmbed`/`EnqueueFaceDetect`/`EnqueueThumbnail`/
  `EnqueuePlaces`/`EnqueueMetadata`, `ErrDuplicate`=no-op);
  `EnqueueSidecar` delays its job by `SidecarDebounce` (5 s), the window a burst of edits collapses into,
  and **`EnqueueFamilyExport(ctx,projectUID)`** names only the project — there is one genealogy per project, so
  the payload is `FamilyExportPayload{project_uid}` — refuses an empty project, and waits `FamilyExportDebounce`
  (15 s), longer because filling in a family is a denser burst over a bigger unit of work;
  `EnqueueThumbnailRebuild` is the **forced** thumbnail enqueue (payload `{photo_uid,force:true}`, handled by
  `thumbjob`): what a changed rendering — a saved or reset non-destructive edit — schedules, since the thumbnail
  cache is keyed by the original's hash and would otherwise serve the previous rendering forever. The flag rides in
  the payload so **dedup is unchanged** (`idx_jobs_dedup` keys on type + `photo_uid`): at most one active thumbnail
  job per photo. It discards its outcome — the caller saving an edit has nothing to do with it.
  `EnqueueImageEmbedRebuild`/`EnqueueFaceDetectRebuild`/`EnqueuePlacesRebuild` are the same pattern for the other
  three per-photo jobs that own an idempotent skip (`{photo_uid,force:true}` on `image_embed`/`face_detect`/`places`;
  all four share `enqueueForcedPhotoJob`), and they **return `(ForceOutcome, error)`**. They are what the rebuild
  endpoints queue when the backing service is offline, and what makes a forced job survive a sleeping box instead of
  failing the request.
  **A collision is resolved, not swallowed** (the change that made the outcome worth returning): `enqueueForcedPhotoJob`
  inserts, and on `ErrDuplicate` calls `UpgradeToForced` rather than treating the dedup hit as success —
  because the job it collided with is usually the *plain* one `POST /photos/{uid}/process/{step}` queues, whose
  handler then takes its idempotent skip, which is the silent no-op the rebuild endpoints exist to kill. So a queued
  job is upgraded (`ForceUpgraded`), an already-forced one absorbs the request (`ForceAbsorbed`), a fresh insert is
  `ForceScheduled`, and a running one is `ForceInFlight` — the only outcome that means the force is *not* going to
  happen. A collision that vanishes between the two statements retries the insert (`forceEnqueueAttempts` = 3, then
  `ErrEnqueueRaced`). The **plain** enqueuers are untouched: they still swallow `ErrDuplicate` and never rewrite a
  forced payload back to a plain one — a forced job outranks a plain one in both directions. `metadata`, `ocr`,
  `sidecar` and `storyboard` have **no** forced counterpart and need none: `ocr` and `sidecar` deliberately re-run
  every time, `metadata` is a gap-filler that never overwrites a value, and `storyboard` is a content-addressed
  cache file with nothing persisted behind it),
  `internal/worker/`
  (the in-process background worker runtime, **the main queue execution loop**; it is project-blind —
  a handler that needs the project of a library-wide job reads it with `jobs.ProjectUIDOf(job)`, a per-photo
  handler from the photo it loads, never from a global: `Registry` =
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
  mail server, and a message that never waits behind a queue of thumbnails. **`push_send`** gets a
  **two-slot** pool (`defaultPushConcurrency`) for the second of those reasons. **`storage_rekey`** gets a
  **one-slot** pool (`defaultRekeyConcurrency`): one project's storage move at a time, at its own pace. A type with no
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
  error-free failure, no burned attempt — used by `image_embed` when the box is offline);
  `WaitedDelay(created, now, floor, ceiling)` is the delay of a deferral that repeats while a job waits for
  something outside its control: as long as the job has waited already, clamped to `[floor, ceiling]` (a zero
  `created` counts as long) — the storage move's deferrals of `thumbnail`/`hls_transcode`/`sidecar` use it to
  back off from a minute to 30 instead of polling every minute while a stalled move holds them;
  `IsDeferral(err)` is the same distinction for callers **outside** the worker — the rebuild endpoints, which
  run a handler's work synchronously and queue the forced job instead of failing when the answer is "not now";
  or
  `Terminal(cause)`/`TerminalError` → `FailTerminal` instead of `Fail` (the job can never succeed, so it is
  parked in `failed` rather than retried with backoff — used by `mail_send` for a permanently undeliverable
  address); the optional `Observer` (`JobStarted`/`JobFinished`/`StaleLocksRecovered`, satisfied by
  `*metrics.Registry`) gets outcome `success`/`error`/`deferred`/`terminal` from `outcomeFor`, which classifies
  in the same order `record` writes, and the count of every stale-lock scan that requeued something; a built-in **noop**
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
  normalized SigLIP/ArcFace vectors (important on the Pi); both tables carry a **denormalised
  `project_id`** (migration 0102) that the two writers (the embedding upsert and the face insert behind
  `SaveFaces`/`RecordFaceDetection`) copy from the photo's row, so a project-filtered kNN needs no join.
  The face insert reads that copy only **inside the project its caller names** (`Detection.ProjectUID`, the
  project the face job loaded the photo in; `SaveFaces` — no production caller, the tests' face fixture —
  reads the photo's own): a photo of another project yields NULL, the NOT NULL column refuses it, and the
  write is `ErrPhotoNotInProject` (an empty `Detection.ProjectUID` is `ErrNoProject`).
  **Project scope of the kNN (`project.go`):** every similarity search — `FindSimilar`, `FindSimilarFaces`,
  `FindSimilarFaceCandidates`, `FindSimilarUnassignedFaceCandidates` — takes `projectUID` after `ctx`, never
  defaulted (empty = `ErrNoProject`), and filters `project_id = $n` **inside** the statement the HNSW index
  serves: a `WITH nearest AS MATERIALIZED (… WHERE project_id = $n … ORDER BY embedding <=> $1 LIMIT $k)`,
  re-sorted by `distance` (then uid) outside the CTE, where the distance cutoff is applied too — inside it
  the cutoff would blind the iterative scan (`TestKNNQueriesFilterTheProjectInsideTheScan` guards the shape).
  Each runs in `withProjectReadTx`: `hnsw.ef_search = 100` plus `SET LOCAL hnsw.iterative_scan =
  relaxed_order` **when the installed pgvector supports it** — `iterativeScanSupported` reads
  `pg_extension.extversion` once per process and caches the answer (`supportsIterativeScan`: ≥ 0.8.0; a
  failed probe is not cached). The iterative scan is what keeps a small project's neighbours complete next
  to a big one: without it the filter would discard whatever of the first `ef_search` candidates belongs to
  another project. The planner picks the btree `idx_faces_project`/`idx_embeddings_project` + an exact sort
  for a small project and the HNSW scan with the filter for a big one (plans in `docs/PERF.md` §1). `Face`
  and `Embedding` carry `ProjectID` on read, so a job or a later-scoped caller searches the neighbours of a
  face inside that face's own project (`embedjob`'s duplicate pass, `internal/cluster`). The **loaders**
  keyed by a photo or subject uid take `projectUID` after `ctx` too and name it in their own statement
  (`AND project_id = $n`): `GetEmbedding`, `ListFaces`, `FacesByKeys`, `ListFacesBySubject`,
  `SampleFacesBySubject` — a row of another project is answered exactly like a missing one (no row,
  `ErrEmbeddingNotFound`), an empty project is `ErrNoProject`, so their correctness no longer depends on the
  caller having proved the photo first. The one unscoped loader left is **`GetEmbeddingForJob`**, the
  embedding job's read (no request, the row names its project); the `*ForJob` guard keeps it out of every API
  package. The writes keyed by a photo uid (`UpdateFaceMarker`, …) stay unscoped — child rows
  of a photo the caller has already loaded inside its project; the face replace of `SaveFaces`/
  `RecordFaceDetection` deletes and inserts within the named project. `FindDuplicatePairs` (the duplicates scan) filters
  `project_id` on both sides of its lateral join — the outer row and, inside the HNSW scan, the neighbour —
  under the same iterative-scan transaction, so a pair never names two projects.
  `Store` = `NewStore(pool)` over the shared pgx pool:
  `SaveEmbedding`(upsert)/`GetEmbedding(projectUID,photoUID)`(`ErrEmbeddingNotFound`)/`GetEmbeddingForJob(photoUID)`/
  `FindSimilar(projectUID,vec,limit,maxDistance)`
  for 1152-dim image embeddings (768 until migration `0057` swapped the image tower),
  `SaveFaces`(idempotent replace in a transaction)/`ListFaces(projectUID,photoUID)`/
  `ListFacesBySubject(projectUID,subjectUID)` (faces with the given `subject_uid`, ordered `(photo_uid,
  face_index)` — the basis of outlier detection; shares `queryFaces`/`scanFace` with `ListFaces`)/
  `SampleFacesBySubject(projectUID,subjectUID,limit)` → `SubjectFaces{Faces,Total,Photos}` (the **bounded** read of the
  same set: at most `limit` rows selected by an even stride **in SQL** — a row survives where
  `floor(rn·limit/total)` steps up — plus the subject's true face count and distinct-photo count, since a
  sample can no longer be counted from; `limit ≤ 0` = all. It backs the candidate search, whose cost is one
  kNN per exemplar: without it a person tagged on thousands of photos transfers and decodes a 512-dim
  embedding per row, which is how one request reached 10.9 GB — see `docs/PERF.md` §3)/
  `FindSimilarFaces`/`FindSimilarFaceCandidates` (like `FindSimilarFaces`, but
  also returns the cache `subject_uid`/`subject_name`/`marker_uid` + `bbox` — the basis of identity suggestions)/
  `FindSimilarUnassignedFaceCandidates(projectUID,vec,limit,maxDistance,exclude)` (like the previous one, but only
  **unassigned** faces `subject_uid IS NULL` and with an **exclusion set** `[]FaceKey` filtered out
  directly in SQL (an anti-join via `unnest` of two parallel arrays) — the basis of finding a person among
  untagged photos, the recognition sweep and the review game; the exclusion filters **before** `LIMIT` and
  runs under the iterative scan (`relaxed_order`, re-sorted outside the CTE), so the caller gets `limit` candidates
  even when rejections
  take away the nearest neighbours — filtering only after the HNSW limit would silently shrink the result, which is a real
  bug. `subject_uid IS NULL` is served by the **partial index** `idx_faces_unassigned_hnsw` (migration 0047),
  whose predicate is this `WHERE` clause verbatim; keep the two in step or the search costs 9× again.
  `maxDistance` is the one filter applied **in Go**, by cutting the distance-ordered rows at the first one
  beyond it — the same set a SQL predicate returns, but a `(embedding <=> $1) <= x` in the `WHERE` clause is
  invisible to the index scan and made the iterative scan walk to `hnsw.max_scan_tuples` on every call:
  90 ms per exemplar instead of 10, see `docs/PERF.md` §3)/
  `FacesByKeys(projectUID,keys)` (batch fetch of `faces` rows by `[]FaceKey` `(photo_uid,face_index)` in one
  query via `JOIN unnest` — **including embeddings**; keys with no row (a face deleted by re-detection, or a
  face of another project) are missing from the result, order undefined, empty input → `nil`; the basis of `internal/candidates`,
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
  `ErrFaceIndexTaken` (UNIQUE `(photo_uid,face_index)`); `ListPhotosMissingEmbedding(projectUID,limit)` =
  uids of **the project's** non-archived photos without an embedding (LEFT JOIN, newest first, `limit<=0`=all) for
  backfill; `FindDuplicatePairs(projectUID,neighbours,maxDist)` = the project's near-duplicate pairs by embedding cosine
  distance (`duplicate.go`, `CROSS JOIN LATERAL` + HNSW `LIMIT` neighbours per photo, no
  O(n²) scan; `maxDist<=0`→no pairs; `ErrNoProject` without a project; a read-only tx with `hnsw.ef_search`
  and the iterative scan, `withProjectReadTx`) — the basis of
  `internal/duplicates`; **face-detection tracking** in the `face_detections` table (migration
  `0009_face_detections.sql`: `photo_uid PK` FK `ON DELETE CASCADE`, `face_count`, `model`,
  `detected_at`, + `detect_width`/`detect_height` from `0061_face_detection_frame.sql`) — because `faces`
  can have zero rows, it is the only way to distinguish a photo
  with no faces from an unprocessed one; `RecordFaceDetection(uid, Detection{Faces,Model,FrameWidth,FrameHeight,ProjectUID})`
  (atomically replaces the photo's
  faces **and** upserts the `face_detections` row — even for zero faces; shares the `replaceFaces` tx
  helper with `SaveFaces`).
  **Replacing is not forgetting** (`carryover.go`): before the delete it reads the photo's rows that carry an
  assignment (`listAssignedFaces`) and hands each one — `marker_uid`, `subject_uid`, `subject_name` — to the new
  face that lands on the same place, pairing old and new greedily and **exclusively** by overlap
  (`IoU`, threshold `CarryAssignmentIoU` = 0.5, deliberately far above the 0.1 `facematch` uses for
  face↔marker matching: the cost of being wrong here is a photo attributed to the wrong person). A new face
  the caller already named is skipped — an explicit assignment is never replaced by an inherited one — and a
  face detected somewhere new arrives unassigned. Without it, a forced re-detection would leave
  `faces.subject_uid` NULL until somebody next opened the photo (`facematch` re-derives the cache from the
  markers on read), so the photo would quietly drop out of that person's gallery and out of `person:` search
  in the meantime. `IoU` lives here rather than in `facematch` because both callers need it and two copies of
  a geometric primitive drift; `facematch.IoU` is a one-line delegation.
  `FacesDetected(uid)` (does a row exist?), `ListPhotosMissingFaces(projectUID,limit)`
  (uids of **stills** with no `face_detections` row, like `ListPhotosMissingEmbedding`; a video is excluded —
  detection does not run on footage, so a clip would otherwise be reported as a permanent gap and rescheduled
  forever; both per project, `ErrNoProject` without one), `CountVideosMissingFaces(projectUID)` (how many live videos that listing leaves out, so a backfill can say
  what it passed over);
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
  `internal/family/`
  (the DB layer for the **genealogy over subjects** — who is whose parent, whose partner, whose child —
  tables `subject_families`/`subject_family_children` in migration `0073_subject_families.sql`; it reads
  subjects without owning them (`internal/people` does) and is a package of its own because the recursion
  and the invariants are a separate concern from the fifteen files of `people`.
  **The family is the node, not the edge:** a family is a couple, a lone parent or a bare sibling group, plus
  their children — the shape genealogy software settled on decades ago. A relationship table of
  `(from, to, type)` with parent/spouse/sibling was the obvious first idea and the wrong one — symmetric edges
  have to be kept symmetric by hand, a sibling edge can contradict the parents, half-siblings are
  unrepresentable — whereas here **siblings, partners, half-siblings and step-relations are all derived**
  and therefore cannot disagree with one another; a **childless marriage** is still a recordable row, which a plain parent edge cannot
  express at all. `subject_families` = `uid PK` (prefix `fm`), two **nullable** partner columns (FK subjects
  `ON DELETE CASCADE`), `kind IN (marriage|partnership|unknown)`, `from_year`/`to_year`, `note`, timestamps;
  `subject_family_children` = `(family_uid, child_uid)` PK + `kind IN (birth|adopted|step)`.
  **Three constraints carry the weight.** `subject_families_partners_ordered` holds the pair in **byte order**
  with `COLLATE "C"` — `normalisePair` normalises with Go's `<`, and the database default collation is
  locale- and version-dependent and orders `_` differently, so without the pin a pair Go considers normalised
  could be refused (the same trap as `0038` for duplicate dismissals). `idx_subject_families_pair` is UNIQUE
  **`NULLS NOT DISTINCT`**, which makes one couple exactly one family *and* one lone parent exactly one family
  — the two NULLs of a lone parent then compare equal. `idx_subject_family_children_child` makes a person a
  child in **at most one** family, which is what keeps the descendant walk a tree rather than a general graph.
  The deliberate limitation that follows: two children of one mother by two unknown fathers read as full
  siblings, and the escape hatch is a placeholder subject for the father (a subject with no photos is legal).
  **Migration `0076` made a partnerless family legal** — the *sibling group*: two people known to be brother
  and sister whose parents are not in the library had a real shared fact and nowhere to write it, and a
  placeholder "unknown parent" subject would have bought the constraint at the price of a phantom in the
  people list. It drops `subject_families_has_partner` and makes the pair index **partial** (`WHERE
  partner_a_uid IS NOT NULL OR partner_b_uid IS NOT NULL`); without that second half the all-NULL pairs of
  every group in the library would compare equal and collapse into one row. What holds the row together
  instead is the store: a group is created with two children at once and `pruneFamily` deletes it the moment
  it drops below two, so it cannot linger as the orphan the CHECK was guarding against.
  **Cycles** are the one contradiction SQL cannot refuse on its own (A a child of B's family while B is a child
  of A's), so every attachment first walks the prospective parent's ancestors (`ancestorUIDsSQL` → the pure
  `wouldCycle`) and refuses with `ErrCycle`; every recursive walk also carries a **`depth < 20`** guard
  (`MaxDepth`), so a cycle that somehow reached the tables ends the query instead of the request.
  `Store` = `NewStore(pool)`.
  **Project scope (`project.go`, the people convention):** `subject_families` carries `project_id`,
  `subject_family_children` belongs to its family's project. Every method takes `projectUID` after `ctx`,
  never defaulted (`ErrNoProject`), and a subject or family of another project is the same error as a missing
  one (`ErrSubjectNotFound`, `ErrFamilyNotFound`; `RemoveRelationAudited` answers `ErrRelationNotFound`, what a
  missing subject always got). The scope is enforced **where a request enters the graph**: every write proves
  each subject it names inside the project (the scoped `requireSubjects`) before it touches a row — so a
  relation never ties two projects together and a refused write leaves no audit row — an inline `NewPerson`
  is created in the project, and `insertFamilySQL` takes the new family's `project_id` **from the subject it is
  created for** (the first partner, or a sibling group's first child) — read only inside the request's project
  (`s.project_id = $6`, the project threaded down every write helper), so a family always lives in its
  members' project even if a caller forgot to prove the subject. A walk that starts from a subject of the project therefore stays in it by construction;
  the read statements (`getRelativeSQL`, the four relation lists, `networkFamiliesSQL`/`networkMembersSQL`,
  the three export reads) still filter on the project, so a broken row could not leak either.
  **Reads:** `Relations(projectUID,subjectUID)` → `{Parents,Siblings,Partners,Children}`, the
  four derived lists the subject page's strip draws, each carrying a `Relative` (subject identity, life years,
  cover and the **photo count counted the way the people index counts it**, so a chip's badge is a promise about
  the next page) — a `Partnership` keeps its `Family` and a **nil `Partner` for a lone-parent family**;
  `Tree(projectUID,rootUID)` → the **layout-ready** payload (root, members with their signed `Generation`, the family
  boxes tying them together, `Truncated`, `Total`), produced by the package's **one read walk**
  (`network.go`) — the whole **connected component** over the bipartite graph of people and families, a
  person joined to a family by being one of its partners *or one of its children*, both edges followed both
  ways. It is breadth-first in Go rather than a recursive CTE, one `networkFamiliesSQL` query per
  frontier (every family a frontier person belongs to, with **all** its children — no filter to the walked
  set, which is what makes aunts and cousins appear), because the two things it promises are awkward in SQL:
  a **signed generation** assigned **first-write-wins** (a family takes its partners' generation, its children
  one more, so the nearest relationship names it and nobody is reported twice even when cousins marry), and a
  **cap** (`NetworkLimit` = 400) that keeps the nearest and reports `Tree.Truncated`. Past the cap the walk
  goes on to the end of the component **only counting**, so `Tree.Total` says how many people it holds (the
  page's "nearest *N* of *M*"); the order is deterministic, so what a capped walk keeps is exactly the prefix an
  uncapped one had reached when the first family stopped fitting (`keptPeople`/`keptFamilies`). A family is
  taken whole or not at all, so every uid a returned box names is a member. The traversal (`walkNetwork`)
  takes a `familyFetcher` and is a pure function over it; ties resolve in a fixed order (frontier order,
  families by uid, partners before children), so the result does not depend on row order. Visiting each
  person and family once, it terminates on any graph and needs no depth guard — `MaxDepth` stays the cycle
  check's guard (`ancestorUIDsSQL`), not repurposed. The directional walks it replaced — `Descendants`,
  `Ancestors`, their `descendantsSQL`/`ancestorsSQL`, `clampGenerations`, `Member.Depth`, `Member.Partner`
  and the `Direction` type — were removed in step 3 of the 2026-09-26 network redesign. The `family:` search
  filter is **not** built on this walk: it keeps its own recursive SQL in `internal/photos` (`familyDepthLimit`)
  and keeps meaning *the descendants of a root plus their partners* — the tree page shows a family, the filter
  narrows a listing, and the two answer **different questions on purpose**; do not unify them.
  `GetFamily(projectUID,uid)`.
  **Writes** all go through the generic `mutateAudited(ctx, pool, entry, fn)` (the `internal/people` convention),
  so the audit row lands **in the mutation's own transaction** — and `entry.TargetUID` is stamped **before** the
  call, because `mutateAudited` copies the entry and a stamp made inside the closure is lost:
  `AddParentAudited`/`AddChildAudited` (the same core with the roles swapped, actions `subject.relation.add`),
  `AddPartnerAudited` (find-or-create, so adding the same couple twice returns the one family),
  `RemoveRelationAudited` (`subject.relation.remove`) and `UpdateFamilyAudited` (`family.update`, kind/years/note).
  **`AddRelationAudited(projectUID, subjectUID, AddRelation{Role,SubjectUID|New,ChildKind}, entry)`** is the one
  the HTTP API
  uses: one call for all four roles (`RoleParent`/`RoleChild`/`RolePartner`/`RoleSibling`),
  and the other person is **either** an existing subject **or** a `NewPerson` created by that same transaction
  through `people.CreateSubjectTx`. That inline half is the affordance the whole data-entry story rests on, and
  it is only worth anything if it is **atomic**: a relation refused after the subject row was written — a cycle,
  a third parent — must leave **no orphan subject** behind, which two API calls could not promise. It returns
  `AddResult{Family,Relative,Created}` and refuses with `ErrInvalidRole` (unrecognised role, wrapping
  `ErrInvalidKind`) or `ErrAmbiguousRelation` (both an existing and a new subject named, or neither). It is the
  one write that stamps **into `entry.Details`** from inside the closure — legitimate, and the mirror image of
  the `TargetUID` trap: the entry is copied but its `Details` **map is shared**, so the facts only the mutation
  knows (which family the relation joined, who the other person turned out to be) do reach the audit row.
  Attaching a parent is deliberately forgiving about the order somebody types things in: no family yet → the
  parent's lone-parent family is found or created; the family already lists that parent → only the membership
  kind is rewritten (an **empty kind means "leave it as it was"**, so adding the second parent of an *adopted*
  child does not reclassify the adoption); one parent recorded and this is the second → the lone-parent family's
  pair is completed **in place only when that child is its only one** — a lone parent's other children are theirs
  alone and rewriting the pair would silently hand all of them a second parent — otherwise the child **moves
  into** the couple's family (the one they already have, or a fresh one) and the emptied lone-parent family is
  pruned, which is also how **half-siblings** arise correctly; the family records no parent at all → it is a
  sibling group and the parent joins **it** (`adoptGroup`), so every child in the group becomes that parent's
  child — in place when the parent has no family of their own, otherwise the whole group moves into their
  lone-parent family, each membership keeping its kind, and the emptied group is pruned; two other parents
  already → `ErrAlreadyChild`.
  **`attachSibling`** is the sibling role's write, and its rule is that a child is never moved between families
  silently: one of the two is already somebody's child → the other joins that family; neither is → a family with
  no partners is created for the pair (`newSiblingGroup`, `KindUnknown` — claiming a partnership between nobody
  and nobody would be the one thing the row says that is untrue); both are, in **different** families →
  `ErrDifferentFamilies`. `checkNoCycleFor` is the many-to-many cycle check both paths need (a parent adopting a
  group gains every child of it at once); a group has no partners and therefore no ancestry to contradict.
  Removal is symmetric and conservative: losing
  one parent moves the child to the **remaining** parent's lone-parent family rather than taking both away, a
  separating couple with children keeps its row (renormalised into the first column, since the pair index treats
  `(X,NULL)` and `(NULL,X)` as different rows) and a childless union's row goes. A **sibling** link is removable
  only when the family behind it records no parent — there it is the whole relation (`detachSibling`), and
  `pruneFamily` deletes a group that drops below two children; with a parent on the family the two are siblings
  *because* they are that parent's children, which is `ErrSiblingsDerived`. Sentinels `ErrSubjectNotFound`/
  `ErrFamilyNotFound`/`ErrRelationNotFound` (→404), `ErrCycle`/`ErrAlreadyChild`/`ErrSelfRelation`/
  `ErrFamilyConflict`/`ErrDifferentFamilies`/`ErrSiblingsDerived`/`ErrInvalidKind`/`ErrInvalidRole`/
  `ErrInvalidYears`/`ErrAmbiguousRelation` (→400/409).
  **`Export(ctx,projectUID)`** is the read behind a project's `families.yaml` (`internal/familyexport`): three
  statements — every family of the project, every child membership of those families, and every subject of the
  project either of them names — each in a deterministic order by uid, so an unchanged genealogy exports to identical bytes. They are deliberately **not** in one
  transaction: the file is rewritten on every relation change, so a run that catches a concurrent edit half
  applied is superseded by the run that edit itself schedules. Both tables are classified in
  `internal/reset` as catalogue tables (a wipe empties them). Unit tests cover pair normalisation, the cycle rule
  and the validators; the integration test seeds a **three-generation family with a cousin marriage** and proves
  the diamond is drawn once, at the nearest generation, that the one-family-per-child index refuses a second parentage (asserted against the
  schema, not only through the store), that an ancestor cannot be attached as a child, and that a refused
  mutation writes **no** audit row; `network_test.go` drives `walkNetwork` over an in-memory graph and
  `network_integration_test.go` seeds the production shape (a couple with a child, a parentless sibling group,
  the sibling's own child — all five reached with the right signed generations), a cousin-once-removed
  marriage (each person once, the nearer reading wins) and a component past the cap (`truncated`, the nearest
  kept; the cap is lowered through the test-only `NetworkWithin` hook); a second integration file covers the
  sibling group end to end — two
  parentless groups coexisting (which the pre-`0076` index would have collapsed), a parent adopting a whole
  group with and without a family of their own, the two-families refusal, both removal rules, and the group's
  round trip through `families.yaml`; `isolation_integration_test.go` seeds two projects
  (`internal/projecttest`) with a family each and proves the relations, tree and export of one never name the
  other's rows, that every write naming a foreign subject or family answers like a missing one and changes
  nothing, that a sibling group and an inline subject land in the request's project, and that every method
  refuses an empty project),
  `internal/familyapi/`
  (the HTTP surface over `internal/family`: `Store` (the interface the handlers depend on — `Relations`,
  `Tree`, `AddRelationAudited`, `RemoveRelationAudited`, `GetFamily`, `UpdateFamilyAudited`, satisfied by
  `*family.Store` and fakeable in tests), `NewAPI(Config{Store,RequireAuth,RequireCurator})` + `RegisterRoutes`
  mounting five **flat** patterns — `GET|POST /subjects/{uid}/relations`,
  `DELETE /subjects/{uid}/relations/{uid2}`, `GET /subjects/{uid}/tree`, `PATCH /families/{uid}`. Flat rather
  than a `chi.Mount` for the same reason `peopleapi` is: `/subjects/{uid}` is shared with `peopleapi` and
  `outlierapi`, and a mounted subrouter would collide with both. Mounted in `readAPIOptions`
  (`buildFamilyAPI` in `cmd/kukatko/family.go`) rather than with a fresh `server.WithAPI` line in
  `buildServices`, which sits at the `funlen` limit.
  The handlers are thin — decode, guard, delegate, map the error — and the three decisions worth naming are
  the status mapping, the audit details and the tree's lack of parameters. **`familyStatus`** answers **404** for a
  missing subject/family/relation, **409** for a refusal about the *state* of the tree (`ErrCycle`,
  `ErrAlreadyChild`, `ErrFamilyConflict`, `ErrDifferentFamilies`, `ErrSiblingsDerived` — the request was well
  formed and the same one would have been
  accepted against other rows, so 400 would be a lie and 500 would send somebody hunting a bug that is not
  there — `ErrDifferentFamilies` and `ErrSiblingsDerived` join them for the sibling role), **400** for what the
  request itself got wrong (`ErrSelfRelation`, `ErrInvalidKind`/`ErrInvalidRole`,
  `ErrInvalidYears`, `ErrAmbiguousRelation`, and `people.ErrInvalidType`/`ErrInvalidLifeYears` from an inline
  `new_subject`), 500 only for an error it does not recognise. **Audit:** `POST` hands the store an **empty
  details map** for it to stamp inside the transaction (`subject.relation.add`), `DELETE` names the other side
  up front (`subject.relation.remove`), and `PATCH` loads the family **first** so `familyChanges` can record
  the old→new diff via `ChangeSet.StampInto` and so a missing family answers 404 before anything is written —
  `decodeFamilyUpdate` resolves an omitted `kind` to `partnership` there, the same default the store applies,
  so the recorded diff matches what is stored. **The tree** takes no parameters and reads no query string:
  the `direction`/`generations` it once parsed (`parseTreeParams`) were removed with the directional walks,
  and a request still carrying them — valid or not — is answered exactly like one without, so a stale client
  gets the network rather than a 400. Bodies are decoded with
  `DisallowUnknownFields` under a 1 MiB limit. Unit tests drive the handlers behind pass-through guards with a
  fake store (routing, decoding, the status table, the audit entry); the integration test mounts the **real**
  auth guards over the test database and covers each route end to end plus the three things only the real
  stack can show: a **viewer gets 403 on every write** (and an anonymous client 401 on the reads), the inline
  `new_subject` path leaves **no orphan** when the relation is refused, and a cycle surfaces as **409**.
  **Project scope:** every handler passes `auth.ProjectUIDFromContext` to the store, so a subject or family of
  another project is the same 404 as a missing one and a relation naming one is refused; the isolation
  integration test mounts the API under `/p/{project}` behind `auth.WithProject` over `internal/projecttest`'s
  two projects and covers the foreign reads, every foreign write, and that an inline subject lands in — and the
  export is scheduled for — the request's project.
  Every mutation additionally calls **`enqueueExport(ctx,projectUID)`** after the store has committed —
  `Config.Export` (`ExportEnqueuer`, satisfied by `jobs.Enqueuer`, nil = the export is off) — so the
  project's `families.yaml` on disk follows the tree in the database; it is best-effort in exactly the way the
  sidecar enqueues are: a failed schedule is logged, never returned, because the relation is safely in
  Postgres and the safety net must not be the thing that drops you),
  `internal/familyexport/`
  (the **per-project genealogy export**: a project's whole family tree as one versioned YAML document at
  `families.yaml` in the root of the project's storage prefix (`KeyFor(storagePrefix)`; the bootstrap project's
  empty prefix keeps it at the root of the store), so the tree survives losing the database. It is the
  subject-level counterpart of `internal/sidecarexport` and exists because a relationship is true about
  **two people, not about any photograph** — it belongs in no photo's sidecar, and a great-grandmother
  nobody photographed has no sidecar to belong to. `Version` (2 — version 1 had no project block), `Key`
  (`families.yaml`), `KeyFor(prefix)`, `MIME`;
  `Document{Version,GeneratedAt,Project{UID,Slug},Subjects,Families}` with `Subject{UID,Slug,Name,Nickname,Type,BirthYear,
  DeathYear}` and `Family{UID,Partners []string,Kind,FromYear,ToYear,Note,Children []Child{SubjectUID,Kind}}`
  — partners as a **list** (two uids for a couple, one for a lone parent, and the key omitted altogether for a
  sibling group, whose children are known to be siblings while their parents are not in the library) because
  the two columns carry no roles, and the children folded into the family they belong to so the file reads as
  households rather than as two tables to join by eye. `Build(Input{Project, Export family.Export, Now})` is
  **pure**
  and orders everything (families and subjects by uid, children within a family by uid), which is what makes
  an unchanged genealogy marshal to identical bytes; `Marshal`/`Unmarshal` add and ignore the explanatory
  header; **`CheckProject(doc, projectUID)`** is the guard every reader runs before rebuilding anything —
  `ErrUnsupportedVersion` for any version but the current one, `ErrProjectMismatch` for a document of another
  project — so one project's tree is never restored into another's library;
  `NewWriter(store).Write(ctx, storagePrefix, doc)` writes to `KeyFor(storagePrefix)`, renders the document into
  memory and hands the store its exact size and digest, so the key holds either the whole file or the old one — a truncated write here is not a slightly
  worse tree but every relation in the archive gone. The document is **closed over its own identifiers**
  (every uid a family names is described in `Subjects`); a subject in **no** family is deliberately left
  out, since they have no place in a tree and what is known about them rides in the sidecars of their
  photos. Tests: the round-trip over a fixture that `TestDocument_fixtureIsExhaustive` keeps exhaustive —
  the same pair `internal/sidecarexport` uses to hold its format sufficient — plus the closure property, the
  header, the project block, `CheckProject`, `KeyFor` and the writer's key and declared identity),
  `internal/familyexportjob/`
  (the **`family_export` job**: read one project's whole genealogy, write its `families.yaml`. It is the
  scheduled half of `internal/familyexport` and follows `internal/sidecarjob`'s shape, differing in the one way
  the data does — a sidecar job is per photo, this one is per **project**: its payload is
  `jobs.FamilyExportPayload{project_uid}` and it needs no backfill: "write it now" is the whole operation,
  exposed as `Export(ctx,projectUID)` (→ the key written) and `ExportAll(ctx)` (every project, active or not —
  what `kukatko sidecar families` runs).
  `New(Config{Families,Projects,Writer,Logger})` over three narrow interfaces (`FamilyStore.Export`, satisfied
  by `*family.Store`; `ProjectStore.Get`/`List`, by `*project.Store`, which give the document's project block
  and the storage prefix; `DocumentWriter.Write`, by `*familyexport.Writer`), panicking on a missing one
  because that is a wiring bug. `Handle` reads the payload's project — a payload naming none (a job queued
  before 0106) is the bootstrap project, a malformed one fails the job, a project that no longer exists is
  skipped (its families went with it) — and rewrites only that project's file. The handler is **idempotent and stateless** — it
  reads the genealogy as it is now and writes the file as it should be now — which is why a coalesced job
  costs nothing. An **empty** genealogy is written rather than skipped: a project whose last relation was
  removed is described by a file with no families in it, and leaving the previous one would let a rebuild
  restore a tree the user deleted. Built by `buildFamilyExportServiceOrNil` in `cmd/kukatko` behind
  `sidecar.enabled` — the same switch as the photo sidecars, because it is the same promise — and registered
  as a worker handler only when it exists, so no job of a type nothing can claim is ever enqueued. The
  integration test records a relation through the real store, then reads the file back as if the database
  were gone, and seeds two projects to prove each job writes its own project's document at its own key with
  the right project block — and that a job for A leaves B's file byte-identical),
  `internal/people/`
  (the DB layer for **subjects** (people/animals/other) and **markers** (face/label regions on
  photos), tables `subjects`/`markers` in migration `0008_subjects_markers.sql`.
  **Project scope (`store_project.go`, the photos convention):** `subjects` is a root table; a marker
  belongs to the project of its photo (`markerProjectClause` = an `EXISTS` on `photos.project_id`). Every
  subject, marker, gallery, avatar, cover, search, stats, attach/detach and merge method takes
  `projectUID` after `ctx`, never defaulted (`ErrNoProject`), and a row of another project is the same
  error as a missing one (`ErrSubjectNotFound`/`ErrMarkerNotFound`/`ErrPhotoNotFound`, an empty list).
  A marker write proves its marker — or, for `CreateMarker`(`Audited`), its photo
  (`requirePhotoInProject`) — inside the project on its own transaction, and resolves the subject it names
  through the scoped `subjectName`, so **a marker never points across projects** and a refused write
  leaves no audit row; the audited writes default the entry's `ProjectUID`. The slug is unique **per
  project** (`(project_id, slug)`), so `GetSubjectBySlug` and the find-or-create of a face assignment work
  inside one project. `Tagging` carries `ProjectUID`, so the tag observer (`internal/tagnotifyjob`) hears
  which project a tagging happened in. Deliberately unscoped: `ListMarkersWithSubjects` (the sidecar job, keyed by the photo
  it loaded) and `CreateSubjectTx`, which takes the project from its caller (`internal/family` passes the
  request's). The nameless repair is scoped since step 14: `ListNamelessSubjects`/`SnapshotSubject`/
  `DetachSubject`/`RestoreSubject` take the project (a subject of another project is `ErrSubjectNotFound`,
  writing nothing), `RestoreSubject` inserts the row **into** the project and re-links only markers on the
  project's photos and faces of the project, and the audited ones default the entry's `ProjectUID`. A
  hand-picked **cover must be a photo of the project** on every write (`requireCoverInProject`,
  `store_project.go`: `CreateSubject`, `CreateSubjectAudited`, `CreateSubjectTx`, `UpdateSubject`,
  `UpdateSubjectAudited`) — another project's photo and a uid naming nothing are the same `ErrPhotoNotFound`
  (`peopleapi` → 404 `photo not found`), checked before the instance-wide FK could fail; `RestoreSubject`
  drops such a cover from the hand-supplied snapshot instead (`withCoverInProject`), and `SubjectCovers` /
  `SubjectAvatar` join the picked photo on the subject's project as defence in depth. `subjects`
  = `uid PK` (prefix `su`), `slug UNIQUE (project_id, slug)`, `name`, `nickname` (`TEXT NOT NULL DEFAULT ''`, migration
  `0074`), `type IN (person|pet|other)`, `favorite`,
  `private`, `notes`, `cover_photo_uid` (FK photos `ON DELETE SET NULL`),
  `birth_year`/`death_year` (nullable INTEGER, migration `0051`), timestamps; `markers` =
  `uid PK` (prefix `mk`), `photo_uid` (FK photos `ON DELETE CASCADE`), `subject_uid` (FK
  subjects `ON DELETE SET NULL`), `type IN (face|label|person)`, a normalized bbox `x,y,w,h`
  DOUBLE PRECISION (0..1 display space, like `faces.bbox`), `score`, `invalid`, `reviewed`,
  timestamps + indexes on `photo_uid`/`subject_uid`; `person` (migration `0071`) is a **hand-attached**
  person — the claim "this subject is in this picture" with **no box, no detected face, no embedding**,
  for what detection cannot see: **anybody in a video at all** (detection does not run on footage), a
  profile, a back of a head. A
  CHECK forbids it any geometry and a partial UNIQUE on `(photo_uid, subject_uid) WHERE type = 'person'`
  makes attaching idempotent; `Store` = `NewStore(pool)` over the shared pgx
  pool: **subjects** `CreateSubject`(generates a uid + a **unique slug from name** — `Slugify`
  without diacritics/ASCII, a collision → a numeric suffix `name-2`)/`GetSubjectByUID`/`GetSubjectBySlug`/
  — **two slug functions, and mixing them up is a data bug**: `Slugify` is **total** (a name it cannot
  slugify falls back to the constant `subject`) and is for slug *generation*; **`NameSlug`** returns `""`
  when the name identifies nobody (empty/whitespace/punctuation) and a digest slug `subject-<8 hex>` for a
  name written outside ASCII (CJK/Cyrillic/…, still a name, so still distinct), and is the **only** key a
  find-or-create-by-name path may use. Keying such a path on `Slugify` makes the fallback a **catch-all**:
  every nameless face resolves to it, the first creates one empty-named subject and the rest are *found* by
  it — in production that collected ~16k markers on a single fake person (fixed in the importers of the
  day and in `facematch`; `peopleapi` rejects such a name with 400; the repair for existing
  data is `kukatko maintenance nameless-subjects`, see [`OPERATIONS.md`](OPERATIONS.md))/
  `UpdateSubject`(re-slugging + refresh of the `faces.subject_name` cache)/
  — **the nickname** (`Nickname string`, migration `0074`): what people actually call the subject, `""` when
  nobody recorded one. It is matched **wherever the name is** — `searchSubjectsSQL` (the predicate
  `subjectMatchCond`, both arms folded through `immutable_unaccent` and reading the *same* `$1`, so the
  pattern is bound once) behind `GET /search/global`, and `personCond` in `internal/photos` for `person:`.
  An empty nickname widens nothing: a "contains" pattern never matches `''`, and the one pattern that would
  (`'%%'`) already matched every row through the name arm. Three deliberate non-properties: **no UNIQUE**
  (two men in one village are both "Bohouš"), **no length CHECK** (`name` has none either), and it
  **never reaches the slug** — `Slugify`/`NameSlug` read `name` alone, because the slug is UNIQUE, appears
  in URLs and is recorded in the audit trail. There is **no `faces.subject_nickname` cache** to match
  `subject_name`: one denormalised copy per rename is enough, so a surface that needs the nickname joins
  `subjects` (`ListMarkersWithSubjects` does, for the sidecar). `SubjectUpdate` rewrites it like every
  other field, so an empty one **clears**, and a merge fills it onto a keeper that has none/
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
  same joins so it cannot become a third opinion — a primary-key lookup plus `idx_markers_subject_uid`,
  `ErrSubjectNotFound` for an unknown uid; plus
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
  to settle, and their number is reported as `SharedPhotos`. The single exception is a **hand-attached**
  link (`type = 'person'`), which carries no region: two of them on one photo would be the same sentence
  written twice, there is no box for a curator to judge, and migration 0071's partial unique index refuses
  the second anyway — so `dedupePersonMarkersSQL` drops the source's before the move, and such a photo is
  not counted as shared. (2) **A positive record beats a rejection**,
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
  its `(photo_uid,face_index)` face refs, clear the faces cache, **delete its hand-attached links** (the
  undo cannot bring those back — a subject that identifies nobody is not one anybody attaches by hand),
  delete the subject, audit — returns the
  `SubjectSnapshot` that **is** the undo, since nothing else records the removed links)/
  `RestoreSubject(snap,entry)` (re-inserts the row under its original uid/name/timestamps — only the slug
  may be disambiguated — and re-points its markers and faces; each slug attempt runs in its own audited
  transaction, because a unique violation aborts the transaction it happens in)/`ListPhotoUIDsBySubject` (distinct
  uids of non-archived photos with a non-invalid marker of the subject, newest-first — the basis of the subject's
  gallery in `peopleapi`)/`SearchSubjects(q,limit)` (accent/case-insensitive ILIKE over
  `immutable_unaccent(name)`, cap limit — the basis of `globalsearchapi`)/`SubjectCovers(uids)` (`covers.go`;
  → `map[uid]Cover{PhotoUID,FileHash,FilePath}` (the path
  so the thumbnail URL takes the prefix of the cover's own original), the photo standing for each named subject, resolved for the **whole
  batch in one query**: a `DISTINCT ON` over the batch's non-invalid markers, newest-first by
  `COALESCE(taken_at, created_at)`, restricted to the photos the subject's gallery shows and overridden by a
  hand-picked `cover_photo_uid`. It is a whole **photo**, not the `SubjectFace` the people index crops — a
  medallion drawn from a face crop of a small tile is mush; a subject on no visible photo is absent from the
  map)/`SubjectAvatar(uid)` (`avatar.go`; → `AvatarSource{PhotoUID,Face *Box}`, what one subject's avatar is
  cut from, in **one** statement: a hand-picked `cover_photo_uid` (still visible) wins and comes back with no
  box — shown whole — otherwise the subject's biggest valid face, ordered exactly as `listSubjectsSQL`'s
  `best_face` is (`w*h DESC, score DESC, uid`) and filtered the same way, because the grid decides whether to
  ask for an avatar from the *list* payload and the route must then cut the face that payload named.
  `ErrSubjectNotFound` for a uid naming nothing, **`ErrNoAvatar`** for a subject with neither — an ordinary
  answer the HTTP layer turns into the placeholder's 404); **markers** `CreateMarker`
  (validation of type/`0..1` bounds, optionally a subject right away → faces cache)/`GetMarkerByUID`/
  `ListMarkersByPhoto`/`AssignSubject`+`UnassignSubject` (in a transaction they update the
  denormalized **faces cache** `marker_uid`/`subject_uid`/`subject_name` via
  `WHERE marker_uid = $1`)/`SetMarkerInvalid`/`SetMarkerReviewed`/`DeleteMarker` (clears the
  faces cache); **hand-attached people** (`attach.go`) `ListPhotoSubjects(photoUID)` → `[]PhotoSubject`
  (`{SubjectUID,Slug,Name,Type,MarkerUID,AttachedAt}`, ordered by name — the `type = 'person'` markers
  resolved to their subjects, the array `GET /photos/{uid}` carries) and the two audited mutations
  `AttachSubjectToPhoto`/`DetachSubjectFromPhoto(photoUID,subjectUID,entry)`, each answering the photo's
  resulting list. Both run in **one** transaction (`auditedAttachTx`, which stamps the audit target only
  after the write, because an idempotent re-attach keeps the *existing* marker and that is what the trail
  must point at), check both ends first (`requireLinkable` → `ErrPhotoNotFound`/`ErrSubjectNotFound`, so a
  missing end is a 404 rather than a foreign-key violation or a silent no-op) and are idempotent — the
  attach's `ON CONFLICT` names migration 0071's partial unique index. `deletePersonMarkersTx` runs in all
  three subject-delete paths (`DeleteSubject`, `DeleteSubjectAudited`, `DetachSubject`): the FK's
  `ON DELETE SET NULL` is right for a region, which survives losing its name as an unassigned box, and
  wrong for a bare "this person is here", which would become a row counted forever as a nameless marker
  nobody can ever name; sentinels `ErrSubjectNotFound`/`ErrMarkerNotFound`/`ErrPhotoNotFound`/
  `ErrSlugExhausted`/`ErrInvalidType`/`ErrInvalidBounds` (the last also guards a `person` marker's box:
  `validBounds` mirrors the SQL CHECK, so a caller gets a sentinel instead of a constraint violation); **the faces cache is kept consistent** on every change of a
  marker/subject (delete, rename, assign/unassign); **audited variants**
  `CreateSubjectAudited`/`UpdateSubjectAudited`/`DeleteSubjectAudited` and
  `CreateMarkerAudited`/`AssignSubjectAudited`/`UnassignSubjectAudited`/`SetMarkerInvalidAudited`
  (`internal/people/audit.go`)
  take an `audit.Entry` and write it **in the same transaction** as the change (`audit.Write(ctx,tx,entry)`),
  so the audit row commits/rolls back atomically with the mutation (the `internal/photos`/`internal/organize` convention);
  a shared tx-core (`insertMarkerTx`/`assignSubjectTx`/`unassignSubjectTx`/`setMarkerInvalid`/
  `prepareSubjectInsert`) is used by
  both variants. **`CreateSubjectTx(ctx, tx, subj)`** is the same insert on somebody else's transaction, so
  another package can create a subject as part of a larger mutation of its own and the two commit or roll back
  together — `internal/family` records a relation to a person who does not exist yet without ever leaving an
  orphan behind. Each slug attempt runs in a **nested transaction (a savepoint)**, because a colliding insert
  aborts the statement's transaction and the caller's work has to survive the collision; the pooled variants
  can simply start a fresh transaction per attempt, which is why they do. **Tag observer:**
  `WithTagObserver(people.TagObserver)` returns a copy of the store that reports every marker change
  putting a subject on a photo (`Tagged`: a marker created with a subject, an assignment to a different
  subject, a new hand attachment) or taking one off (`Untagged`: unassign, re-assignment away,
  `DeleteMarker`, a detach that removed a link) as a
  `Tagging{PhotoUID, SubjectUID, ActorUID}` **inside the change's transaction** — the actor is the audit
  entry's, empty for the plain and cluster paths; re-assigning to the same subject reports nothing, and
  an observer error fails the change. `buildFaceMatch` and the photo API's `Attacher` wire the
  `internal/tagnotifyjob` recorder here, which is why every way of tagging (face UI, review game,
  clusters, candidates, attaching a person by hand) is covered.
  `SetMarkerInvalidAudited` (action `marker.invalidate`, used by the repeated-marker review)
  changes **nothing but the flag**: the row survives and keeps its subject, so the decision is reversible and an
  invalidation stays distinguishable from an unassignment), `internal/facematch/`
  (linking detected faces to markers/subjects + identity suggestions, all behind the interfaces
  `PhotoStore`/`FaceStore`/`PeopleStore` (unit-testable with fakes without a DB). **Project scope:**
  `Apply`, `PhotoFaces` and `PhotoPeople` take `projectUID` after `ctx`; `Apply` first loads the request's
  photo with `GetByUIDInProject` (a foreign photo = `photos.ErrPhotoNotFound`, nothing written), then —
  for `assign_person`/`unassign_person` — checks the named marker sits **on that photo**
  (`requireMarkerOnPhoto` via `PeopleStore.GetMarkerByUID`; a marker of another photo of the project =
  `people.ErrMarkerNotFound`, nothing written, so photo Y's route cannot re-tag photo X), every
  marker/subject read and write goes through the project-scoped people store, a subject auto-created by
  name lands in the project, and the suggestion search (`FindSimilarFaceCandidates`) runs inside it — so
  a face is never suggested a person of another project. `ClearSurplusLinks` is the one request-less
  method (the maintenance repair sweeps the instance): it loads the photo through `GetForJob` and works in
  the photo's own project. `Service` =
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
  `POST /photos/{uid}/faces/assign`, curator and above): `create_marker` (creates a face marker + assigns the
  subject + links the face), `assign_person` (assigns a subject to an existing marker),
  `unassign_person` (detaches the subject), keeps the `faces` cache and `marker.reviewed` consistent
  (assign → reviewed, unassign → unreviewed), **auto-creates a subject by name** (find-or-create
  via `Slugify`+`GetSubjectBySlug`); **audit**: each transition writes 1 row via the audited `people`
  methods in the same transaction as the change — `create_marker`/`assign_person` → `face.assign`,
  `unassign_person` → `face.unassign` (target = marker, details action/photo/subject/face_index);
  `meta` is the actor+request from `photoapi.handleFaceAssign`, empty for the system cluster caller
  (actor NULL);
  **`PhotoPeople(ctx,projectUID,photoUID)`** (`people.go`, backing the detail response's opt-in `?people=true` block,
  served under the key **`faces`** since hand-attached people took the name `people`)
  answers the narrower question "who is on this photo?" → `[]PersonOnPhoto`
  (`subject_uid`/`subject_name`/`marker_uid`/`bbox`/`det_score`): every detection with the subject its
  matched marker names, then the face markers no detection claimed (the regions drawn by hand). It shares
  the exclusive pairing with `PhotoFaces` and does **none** of its expensive or mutating work — no
  suggestion search (up to two HNSW queries per face) and no rewriting of the cached face↔marker link —
  which is what makes it cheap enough to answer a detail request with and keeps a plain read a plain read.
  A **hand-attached** person (`people.MarkerPerson`) appears in neither this nor `PhotoFaces`: every entry
  they carry is rendered as a rectangle, and such a link has none — it would paint a box at `(0,0,0,0)`.
  `assignedSubjectNames` skips them too, so they stay *suggestable*: the link exists precisely because the
  detector never found that person's face, so once detection does produce one they are the likeliest answer
  for it, not the one candidate to hide;
  **`ClearSurplusLinks(ctx,photoUID)`** re-derives one photo's pairing and clears the
  cached `marker_uid`/`subject_uid`/`subject_name` of every face holding a surplus claim (→ how many rows
  were cleared) — the bulk counterpart of what `PhotoFaces` does when a photo is viewed, for the links
  written before matching was exclusive; it is what `maintenance repair --face-markers` drives and it
  **never deletes a face or a marker** nor touches a face whose link is the only one on its marker;
  sentinels `ErrInvalidAction`/`ErrMissingBBox`/
  `ErrMissingMarker`/`ErrMissingSubject`, a missing photo/marker/subject → 404 in the HTTP layer
  (`photoapi.FaceService` interface + handlers in `internal/photoapi/faces.go`); tunables in
  `faces.*` config; **attaching a person by hand** is the deliberate opposite and lives elsewhere —
  `internal/photoapi/people.go` over the `PeopleAttacher` interface (= `people.Store`), because it needs no
  face, no box and no vector search: `POST /photos/{uid}/people` and
  `DELETE /photos/{uid}/people/{subject_uid}` (both `RequireCurator`, both idempotent, both answering the
  photo's resulting `{people:[…]}`) plus `resolveAttachedPeople`, the unconditional detail block — one
  indexed read joined to `subjects`, degrading to an empty array when the backend is missing or fails,
  because nobody should lose the photo over the list of who is on it), `internal/embedjob/`
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
  an embedding (dedup no-op), returns the count; the payload's **`force` flag** routes to `ForceEmbed(uid)`,
  which shares `embed(uid,force)` with `Embed` and differs in exactly one thing — the existing embedding is not
  a reason to stop — so the vector is recomputed and **replaces** the stored one (there is one `embeddings` row
  per photo). Everything else is `Embed`'s behaviour on purpose: an offline box still defers, a missing photo is
  still an error. It backs `POST /photos/{uid}/reembed` and `jobs.Enqueuer.EnqueueImageEmbedRebuild`;
  `Duplicates(ctx,uid)` embedding-based detection of near
  duplicates within `duplicate.embedding_max_dist`, excluding itself (`<=0` disables it)), `internal/facejob/`
  (wiring of face detection into the queue, all behind the interfaces
  `PhotoStore`/`VectorStore`/`ImageSource`/`Enqueuer`+`embedding.Client`: `Service` =
  `New(Config{Photos,Vectors,Client,Source,Enqueuer,OfflineRetryDelay,MinDetScore})`; **the
  `face_detect` handler** `Handle`(=`worker.HandlerFunc`, registered in `serve`) → from the payload
  `{"photo_uid"}` loads the photo, opens the **full-resolution UPRIGHT original** via
  `StorageSource.OpenUpright` (= `storage.Materialize` + `imgconvert.EnsureDecodable` behind the interface
  `Materializer`, HEIC/RAW/video are converted, `Close` frees the temp and the materialized original),
  sends `FaceEmbeddings` to the sidecar (512-dim + pixel bbox + det_score) and
  stores it via `vectors.RecordFaceDetection(uid, vectors.Detection{Faces,Model,FrameWidth,FrameHeight,ProjectUID})`
  (the project the photo was loaded in, so the faces land only while the photo is still that project's);
  the original (not the thumbnail) so the detector sees every pixel there is.
  **The sidecar does NOT apply EXIF** — measured against the running service: one production iPhone original
  with `orientation=6` returned 2 misshapen boxes as it lay and 6 face-shaped ones pre-rotated, a second
  returned 0 against 2 — so **this package owns the rotation**: `OpenUpright` reads the orientation of
  **the file it is about to send** (`exif.FileOrientation`), and for a tag > 1 decodes, applies
  `imgconvert.Orient` and re-encodes a JPEG with no EXIF at all (`NewStorageSource(storage, maxPixels, budget)`,
  cap = `thumb.max_pixels` and the decode budget via `imgconvert.ReserveDecode` (bitmap + the oriented copy),
  over either = a visible job failure, not a sideways send); a file with no tag is streamed byte-for-byte, so the common case pays nothing.
  **Two orientation readers, one hazard:** this one reads the file being sent, ingest's (`exiftool`) read
  the original and wrote `photos.file_orientation`, and they can disagree — so the catalogue is the
  fallback **only** when the file says nothing **and** the bytes are the untouched original, which
  `materializeDecodable` decides by comparing the path `imgconvert.EnsureDecodable` returned with the one
  it was given (identical = the passthrough path = the original itself; any other path is an intermediate
  whose pixels may already be turned, and there today's behaviour is kept exactly). Without that fallback a
  JPEG whose orientation lives only in XMP went to the detector on its side: nine production photos with
  `file_orientation = 8` detected on a `6048x4032` frame that should have been `4032x6048`, boxes beside
  faces and two of them with no faces found at all. It returns
  `UprightImage{Reader,Width,Height,Orientation}` whose frame is **measured on those very bytes** and whose
  orientation is the one that was applied to them (or that they already embodied), and **bbox conversion**
  `NormalizeBBox(bbox, frameWidth, frameHeight)` divides pixel `[x1,y1,x2,y2]` → normalized `[x,y,w,h]`
  (0..1) by exactly that frame — no orientation enters the conversion, which is what makes the old defect
  (divide by the stored pair swapped, while the pixels were in the raw frame) unrepresentable; the photo's
  stored pair is still copied onto the face row as the render hint it always was, while the row's
  `orientation` comes from `UprightImage` — the source that turned the pixels has the last word, so a row
  cannot carry a detection frame and an orientation that describe different pictures;
  **det_score filter**
  (`faces.min_det_score`, default 0.5, `<=0` disables) drops weak detections, reindexes the survivors
  contiguously; **idempotent** (a photo with a `face_detections` row is skipped; zero faces is still
  recorded), **box offline** → `worker.RetryAfter(5 min)`; **a video is skipped** (nil error, nothing written —
  not even the detection record, which would report the step as done for work that no longer happens: the
  detector could only ever read the poster, one arbitrary frame, so who is in a clip is recorded by hand
  instead, and migration `0072` removed what it had already stored); `BackfillFaces(ctx)` enqueues
  `face_detect` for every unprocessed still (`ListPhotosMissingFaces`, dedup no-op), returns
  `BackfillResult{Enqueued,SkippedVideos}` — the second number reported rather than dropped, because a
  library of mostly footage would otherwise answer a flat zero and look broken; the payload's **`force` flag** routes to `ForceDetect(uid) (int,error)`, which shares
  `detect(uid,force)` with `Detect` and differs only in ignoring the recorded detection, then **returns how many
  faces the photo has afterwards** (0 is a result). The faces it finds replace the stored ones in one
  transaction, with the assignments carried over (see `internal/vectors`), so a rebuild never leaves duplicate
  detections behind and never un-names anybody. It backs `POST /photos/{uid}/redetect-faces` and
  `jobs.Enqueuer.EnqueueFaceDetectRebuild`), `internal/processapi/`
  (a maintainer-only HTTP API for bulk processing: `NewAPI(Config{Backfiller,FaceBackfiller,
  Reclusterer,PlacesBackfiller,ThumbnailBackfiller,MetadataBackfiller,RequireMaintainer})`+`RegisterRoutes`
  mounts `/process` — **per project**: the group is mounted with the project-scoped API, and every handler hands
  the request's project (`projectOf` = `auth.ProjectUIDFromContext`) to its backfill, whose listing (`photos`,
  `vectors`, `places` stores — each `List…Missing…`/`Count…` takes `projectUID` and refuses an empty one) sees that
  project's photos only; every interface method below therefore takes `projectUID` after `ctx`. The
  instance-level triggers that remain — the maintenance scan/repair and `kukatko sidecar backfill` — run the
  per-project backfill once per **active** project through the adapters in `cmd/kukatko/allprojects.go`;
  `POST /process/embeddings` →
  `{enqueued}` runs `embedjob.BackfillEmbeddings`, `POST /process/faces` → `{enqueued, skipped_videos}` runs
  `facejob.BackfillFaces`, `POST /process/clusters` → **202** `{scheduled}` runs
  `clusterjob.ScheduleProjectRecluster(projectUID)` (it **queues** the request project's `face_cluster` pass — regroup the unassigned faces,
  then prepare the cluster summaries — rather than running minutes of vector search inside the
  request; `scheduled:false` = a pass was already waiting or running; `Reclusterer` optional — nil → 503),
  `POST /process/places` → `{enqueued}` runs `placesjob.BackfillPlaces` (backfill of reverse-geocode for
  geotagged photos; `PlacesBackfiller` optional — nil → 503, i.e. without a mapy.com key),
  `POST /process/thumbnails` → `{enqueued,pending,dry_run}` runs `thumbjob.BackfillThumbnails(all)` (backfill of
  `thumbnail` for photos without a thumbnail = without a pHash; `?all=true` schedules every non-archived photo;
  `?dry_run=true` schedules **nothing** and only reports `pending` — the count `thumbjob.CountBackfillThumbnails`
  answers, so a full-library run is a number read beforehand rather than a surprise; a real run reports both,
  so its size is visible in the response too; **`?videos=true`** switches the same endpoint to
  `thumbjob.BackfillVideoPosters`/`CountBackfillVideoPosters` (every non-archived video, **forced**) — how the
  library re-picks its video poster frames, which `?all=true` could not do because those thumbnails are already
  cached; the `ignoreAll` adapter drops the scope flag the shared driver passes, since the video scope is
  already every video; `ThumbnailBackfiller` optional — nil → 503; local, works even
  with the box offline; `queryFlag` parses `?all`/`?videos`/`?dry_run`),
  `POST /process/blurhash` → `{enqueued,pending,dry_run}` runs `thumbjob.BackfillBlurhash(all)` (backfill of
  the **blurred placeholder** for photos without one; it schedules `thumbnail` jobs, since that is the job
  which computes a placeholder, so this endpoint and `/process/thumbnails` differ only in the predicate;
  `?all=true`/`?dry_run=true` behave exactly as there, both handlers going through the shared
  `serveCountedBackfill`; `BlurhashBackfiller` optional — nil → 503; local, works even with the box offline),
  `POST /process/metadata` → `{enqueued}` runs `metajob.BackfillMetadata(all)` (backfill of `metadata`
  for photos whose file has never been read = `metadata_extracted_at IS NULL`; `?all=true`
  forces a re-read of every non-archived photo; **`?videos=true`** instead runs
  `metajob.BackfillVideoMetadata()` over the videos whose container was never read out (no duration, no codec,
  no frame size) — the clips the plain run passes by for ever, because their rows *are* marked as read; the
  video scope has no `all` of its own, so the two flags do not combine (`runMetadataBackfill`);
  `MetadataBackfiller` optional — nil → 503; local, works even with the box offline),
  `POST /process/hls` → `{enqueued}` runs `hlsjob.BackfillHLS(all)` (backfill of `hls_transcode` for
  non-archived videos with **no `photo_hls_renditions` row**; `?all=true` schedules every non-archived video,
  a forced full re-encode — how a newly enabled quality level or a changed `segment_seconds` reaches the
  library, since the job replaces a rendition rather than adding to it; `HLSBackfiller` optional — nil → 503,
  i.e. with `video.hls.enabled: false`; local ffmpeg, works even with the box offline, but the one-slot pool
  makes a library-wide run hours of background work)), `internal/processing/`
  (the same question as `processapi` asked about **one photo**: what has the library already computed about
  it, and schedule the one step it missed. `Step` values **are** the `internal/jobs` type constants (no
  parallel vocabulary), `Steps` fixes the report order `metadata`, `thumbnail`, `hls_transcode`, `image_embed`,
  `face_detect`, `ocr`, `places`, `sidecar`, and `ParseStep` is the untrusted-input boundary; `storyboard` is deliberately
  **not** a step — it is rendered lazily on first playback into the local derived-media cache and leaves no
  persisted evidence, so it has no honest state to report and nothing a button could usefully schedule ahead
  of a viewer pressing play. `Store` = `NewStore(pool)`: `Evidence(photoUID)` reads the whole evidence in
  **one** query — `photos.media_type`/`lat`/`lng`/`metadata_extracted_at`/`ocr_at`/`ocr_text`/
  `sidecar_written_at`/`thumbnails_at` plus four LEFT JOINs on the PK-keyed side tables `photo_phashes` (the
  thumbnail step's `at` is `photos.thumbnails_at`, but **only** when the pHash row exists too — the step
  produces both; the stamp is written after every size was generated (`photos.MarkThumbnailsBuilt`, moved
  by every forced rebuild — the viewer's only way to know the rebuilt renditions exist), never before, so an
  upload interrupted between its pHash and its thumbnails reads as `pending`, not `done`), `embeddings`,
  `face_detections` (also the `face_count`) and `photo_places`, plus a lateral
  `max(photo_hls_renditions.encoded_at)` for the streaming step — an aggregate rather than a fifth join,
  because that table holds one row per rendition and a plain join would multiply the whole report by however
  many qualities the video was encoded into; the place column is **conditional**
  (`lat IS NOT NULL AND lng IS NOT NULL`) because `photo_places` also holds the coordinate-less marker the
  geocoder writes for a photo with no GPS, and that records that there was nothing to do rather than a place.
  A step **applies** to a photo or it does not: `places` needs a coordinate, `ocr` and `face_detect` both
  read a still (never a video — each could only ever have looked at the poster, one arbitrary frame of the
  footage, so who is in a clip is recorded by hand instead), and `hls_transcode` is the mirror image — only a
  standalone video has anything to encode, a live photo's motion clip being a hover preview nobody streams.
  `Service` = `New(Config{Evidence,Jobs,Enqueuer,Disabled})` (panics on a nil collaborator):
  `Report(photoUID)` = evidence + `jobs.Store.UnfinishedForPhoto` (**two** round trips, never an N+1) →
  `[]Status{Step,State,At?,Error?,FaceCount?,TextFound?}`; the precedence is landed evidence → the queue →
  skipped → pending, so work that has landed is `done` however the queue looks, work in flight beats "will
  not run", and only an absence with nothing behind it is `pending`. `jobState` maps a queue row: running
  wins over its own previous error, a dead job **and** a queued one carrying a `last_error` (a retry pending
  after a failure) are both `failed` with that text. `Disabled` carries the steps whose feature is off
  instance-wide (`ocr`, `sidecar`, `places`, `hls_transcode` — exactly the config-gated handlers of
  `buildRegistry`); with no
  handler registered a job of that type would sit queued forever, so those read as `skipped` and `Run`
  refuses them. `Run(photoUID,step)` validates, refuses an inapplicable step (`ErrStepNotApplicable`) and an
  unknown one (`ErrUnknownStep`), then dispatches to the step's own `jobs.Enqueuer` method (keeping the
  sidecar's debounce) and answers with the step's new state; the queue's dedup index makes a double click
  harmless. `SchedulePending(photoUID)` enqueues every step `Report` calls `pending` (and nothing else) and
  returns them — what a re-upload of an already-catalogued file calls through `ingest.PendingScheduler`.
  With the optional `Config.Library` (`LibraryReader`, satisfied by `Store`: `EachLiveEvidence(projectUID, fn)` streams
  the same evidence columns for every non-archived photo **of one project** (`ErrNoProject` without one),
  `UnfinishedSteps` reads every photo's unfinished job types in one scan of `jobs`), `Unscheduled(projectUID)` returns
  every `Gap{photo_uid,step}` in that project's live library (the per-photo `Evidence`/`Report`/`Run` act on a photo
  the caller already proved in its project) —
  the `pending` predicate (`Evidence.pending`, kept equal to the report's by test) asked once per photo, two
  queries in all; without one it is `ErrLibraryUnavailable`. It backs the maintenance scan's
  `unscheduled_steps`. Read-only apart from those enqueues — it never runs the work itself), `internal/cluster/`
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
  **`ListPage(ctx,PageRequest{Limit,Offset})`** (backing `GET /faces/clusters`) → `Listing`
  `{clusters,total,pending,limit,offset,next_offset}`: one page of the clusters **whose cached
  summary exists**, newest first, built from `face_clusters.summary` (`DefaultPageSize` 24,
  `MaxPageSize` 100, `clamp`/`nextOffset`/`viewOf` pure) — two indexed queries, **no per-cluster
  query and no vector search**, which is what makes the page open on a 20k-photo library where the
  old per-request assembly (a face listing **plus an HNSW search per cluster**) never finished;
  **`BuildSummaries(ctx,limit)`** → `SummaryRun{Built,Dropped,Remaining}` prepares up to `limit`
  (`DefaultSummaryBatch` 200) of the clusters that have none — the representative, the examples
  (`maxExamples` 4) and **the suggestion of an existing subject** (`bestSubjectSuggestion` aggregates
  `FindSimilarFaceCandidates` over the centroid by subject, `confidence = 1 − distance`, null when no
  named neighbour < `suggestionMaxDistance`) — stored as JSON on the cluster row (migration
  `0069_face_clusters_summary.sql`: `summary jsonb` NULL = "not prepared yet", `summary_at`, the
  partial indexes `idx_face_clusters_ready`/`idx_face_clusters_summary_pending`); it is called from
  the `face_cluster` job (`internal/clusterjob`), never from a request, drops a cluster whose faces
  are all gone and skips one that vanished mid-pass; **`AssignCluster(ctx,req)`**
  (backing `POST /faces/clusters/{id}/assign`) assigns **all** faces of the cluster to one subject
  (by `subject_uid`, otherwise find-or-create by `subject_name`) via the **shared facematch state
  machine** (`create_marker`, the subject is resolved once and pinned for the rest), then deletes the consumed
  cluster (the FK releases `cluster_uid`); **`RemoveFace(ctx,clusterUID,ref)`** (backing
  `POST /faces/clusters/{id}/remove-face`) detaches a stray face **before** naming, recomputes the
  centroid/size (`RefreshCluster`, which also drops the stale summary) **and rebuilds that one
  cluster's summary inline** — the reader is looking at that card and one suggestion search is theirs
  to pay — deletes an orphaned cluster; **`GroupingState(ctx)`** → `GroupingState{Ready,Unprepared,
  Groupable}` is the read the scheduler decides from: the two cluster counts plus — **only for a
  library with no clusters at all** — whether at least `minSize` clusterable faces are waiting
  (`CountClusterableFaces(ctx,limit)` counts through a `LIMIT`, so the question costs the same on an
  empty library and on one with 200k faces). `Groupable` is deliberately not answered once clusters
  exist: every pass leaves the components below `minSize` unclustered, so "there are unassigned
  faces" would otherwise mean "regroup for ever"; `Store` over the shared pgx pool
  (`ListUnclusteredFaces`/`ListClusterFaces`/`CountClusterableFaces`/`CreateCluster`/
  `AddFacesToCluster`/`ListReadyClusters`/
  `ListPendingClusters`/`CountClusters`/`SaveSummary`/
  `GetCluster`/`DeleteCluster`/`RemoveFaceFromCluster`/`RefreshCluster`); sentinels
  `ErrClusterNotFound`/`ErrEmptyCluster`/`ErrMissingSubject`/`ErrFaceNotInCluster`; tunables in
  `cluster.*` config. **Project scope:** every `Service` and `Store` method takes `projectUID` right after
  `ctx` and never defaults it (`ErrNoProject`); `face_clusters.project_id` and the faces' denormalised
  `project_id` confine every query, so clustering runs **per project** — the union-find only ever sees one
  project's unassigned faces and their neighbours are searched inside it, `CreateCluster` stamps the project,
  the cached summaries and their counts are per project (`idx_face_clusters_project_ready` from 0102,
  `idx_face_clusters_project_pending` from 0105), and a cluster of another project is `ErrClusterNotFound` to
  `GetCluster`/`AssignCluster`/`RemoveFace` — refused before anything is written; the assigner is handed the
  same project, so a cluster can only be named after a subject of its own project), `internal/clusterapi/`
  (a curator-and-above HTTP API over the clustering: the `Service` interface (satisfied by `cluster.Service`)
  and the optional `Preparer` (satisfied by `clusterjob.Service`),
  `NewAPI(Config{Service,Preparer,RequireCurator})`+`RegisterRoutes` mounts `/faces/clusters`:
  `GET /faces/clusters?limit&offset` (one **page** of prepared clusters + suggestions + the `pending`
  count + **`grouping`**; a bad `limit`/`offset` → 400; **every** listing calls
  `Preparer.EnsureGrouping` — the library that needs a pass most is exactly the one whose listing
  reports nothing at all — and `grouping` passes its verdict on, so an empty page can say "the groups
  are being worked out" instead of "there are none"; a scheduling failure is logged and answers
  `grouping:false`, never fatal to the read), `POST /faces/clusters/{id}/assign` (assigns
  the whole cluster), `POST /faces/clusters/{id}/remove-face` (detaches a face); 503 when the backend
  is not wired, 400/404/409 per the sentinels. Every handler passes `auth.ProjectUIDFromContext`: the page
  lists the request project's clusters, `EnsureGrouping` schedules that project's pass, and a cluster uid of
  another project answers the same 404 body as a missing one (`cluster: cluster not found`); mounted in `serve` (`buildClusterAPI` in
  `cmd/kukatko/clusters.go`, which shares the `facematch.Service` from `buildFaceMatch` and the job
  store, and hands the same `clusterjob.Service` on to `buildJobs`)), `internal/clusterjob/`
  (the face-grouping work as background work: the `face_cluster` job handler and the two schedulers.
  `New(Clusterer,Queue,Projects,batch,logger)` over the interfaces `Clusterer` (`Recluster`/`BuildSummaries`/
  `GroupingState`, all taking the project, satisfied by `cluster.Service`), `Queue` (`Enqueue`/
  `CountPendingForProject`, satisfied by `jobs.Store`) and `Projects` (`List`, satisfied by `project.Store`).
  **Per project:** the payload is `{"project_uid":…,"recluster":…}`; `Handle` works on the project it names (a
  payload queued before the field existed names none and works on the bootstrap project, the only library
  there was) and its successor names the same project; `EnsureGrouping(ctx,projectUID)` reads and schedules
  one project; `ScheduleProjectRecluster(ctx,projectUID)` queues the one project's pass (what the per-project
  `POST /process/clusters` calls) and `ScheduleRecluster(ctx)`, the instance-level trigger, calls it for
  **every active project** (a deactivated one is skipped) and reports whether any was queued. The pending check is per project (`CountPendingForProject`), backed by
  the unique `idx_jobs_face_cluster_dedup` on `payload->>'project_uid'` (migration 0105, queued jobs only, so a
  running pass can enqueue its successor); a racing duplicate is read as "scheduled", never an error;
  **`ScheduleRecluster(ctx)`** queues `{"recluster":true}` (the maintainer trigger) and
  **`EnsureGrouping(ctx)`** queues the pass the library's own state asks for, reporting whether one
  is queued or running afterwards: `{"recluster":true}` when `GroupingState.Groupable` (faces and no
  groups — **nothing else in the app ever starts the grouping**, face detection fills `faces` and
  stops, so before this a library nobody grouped by hand stayed empty for ever), `{}` when groups are
  waiting for their summary (regroups nothing — browsing must never change who a face belongs to),
  and **nothing** for a library that is empty or already grouped and prepared. Both are **deduped by
  an explicit per-project pending check** (a running pass counts too, which the queued-only index cannot
  see): at most one pass per project is queued or running, so repeated page loads collapse into one. **`Handle(ctx,job)`** regroups when asked, then prepares one batch of summaries and, when
  `Remaining > 0`, **enqueues its own successor** — so a backlog drains in bounded steps and the page
  can honestly report how many groups are ready), `internal/outliers/`
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
  face is detached via the existing assign API. **Project scope:** `Outliers(ctx,projectUID,subjectUID,opts)`
  proves the subject inside the project first (`people.GetSubjectByUID`), so a subject of another project is
  `people.ErrSubjectNotFound` exactly like a missing one; once proven, the faces read are its own and a
  subject's faces never leave its project), `internal/outlierapi/`
  (a curator-and-above HTTP API over outlier detection: the `Service` interface (satisfied by `outliers.Service`),
  `NewAPI(Config{Service,RequireCurator})`+`RegisterRoutes` mounts `GET /subjects/{uid}/outliers`
  behind `RequireCurator`; 503 without a backend, 404 missing subject — also for a subject of another project
  (the request's project via `auth.ProjectUIDFromContext`), with the fixed body `subject not found`, so the two
  cannot be told apart; mounted in `serve`
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
  hydrated (`photos.ListByUIDsInProject` + `mediaurl.Decorate`), an **absolute pixel floor** is applied
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
  without reimplementation. **Project scope:** `Find(ctx,projectUID,subjectUID,req)` — the subject, its
  markers and marked photos are read inside the project (`people.ErrSubjectNotFound` for one of another
  project, `people.ErrNoProject` for none), the unassigned-face kNN is the project-filtered
  `FindSimilarUnassignedFaceCandidates`, and the survivors are hydrated through `ListByUIDsInProject`, so no
  candidate ever comes from another project however close its face sits), `internal/candidatesapi/`
  (a curator-and-above HTTP API over the candidate search: the `Service` interface (satisfied by `candidates.Service`),
  `NewAPI(Config{Service,RequireCurator,RateLimit})`+`RegisterRoutes` mounts `POST /subjects/{uid}/candidates`
  behind `RequireCurator` and then the heavy vector throttle `RateLimit` (inside the guard; nil = none); the body `{threshold?,limit?}` is **optional** (empty → defaults),
  `DisallowUnknownFields` + 64 KiB, a negative `threshold`/`limit` → 400; 503 without a backend, 404 missing
  subject (`people.ErrSubjectNotFound`) — the same fixed `subject not found` body for a subject of another
  project, searched in the request's project (`auth.ProjectUIDFromContext`); mounted in `serve` (`buildCandidatesAPI` in
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
  skipped failures, collector error, empty library). **Project scope:** `Sweep(ctx,projectUID,…)` and
  `Scan(ctx,projectUID,…)` plan over `ListSubjects(ctx,projectUID)` and hand the project to every `Find`, so
  a sweep walks only the project's people and offers only its faces; `SubjectsTotal` counts the project's
  subjects), `internal/sweepapi/`
  (a curator-and-above HTTP API over the sweep: the `Service` interface (satisfied by `*sweep.Service`),
  `NewAPI(Config{Service,RequireCurator,RateLimit})` (nil RequireCurator/RateLimit → pass-through) +
  `RegisterRoutes` mounts `GET /faces/sweep` behind `RequireCurator` and then the heavy vector throttle; `parseConfidence` (percent-or-distance → distance, floor
  `0.01`, default 75 %) + `parseLimit`, errors → 400; streams **NDJSON** via the `stream` helper, which
  sets the headers (`application/x-ndjson`, `Cache-Control: no-store`) **lazily** at the first line and
  **flushes** after each one (`http.Flusher`, propagated through `internal/metrics` `statusRecorder.Flush`);
  an error before the first line → 500 JSON, mid-stream → only a log; 503 without a backend; the sweep runs in
  the request's project (`auth.ProjectUIDFromContext`); mounted in `serve`
  (`buildSweepAPI` in `cmd/kukatko/sweep.go`, shares `candidates.Service` via `buildCandidatesService`)),
  `internal/expand/`
  (**"find photos similar to an album / label"** — completing a partially-tagged collection, the counterpart to the per-photo
  `GET /photos/{uid}/similar`: for an album or label finds photos similar to its members that are not yet
  in it; all behind the interfaces `VectorStore` (`vectors.Store`), `OrganizeStore` (`organize.Store`),
  `FeedbackStore` (`feedback.Store`) and `PhotoStore` (`photos.Store`) → unit-testable with fakes without a DB;
  `Service` = `New(Config{Vectors,Organize,Feedback,Photos,Media,MaxDistance,Limit,MaxLimit,SearchLimit,
  SourceCap,Concurrency})`, tunables default via the `Default*` constants (0.20/50/200/200/500/8 —
  `DefaultMaxDistance` is model-specific and was re-derived for SigLIP 2, see `THRESHOLDS.md`), nil
  store→panic, nil `Media` is OK. **`Album(ctx,projectUID,uid,Request)`** /
  **`Label(ctx,projectUID,uid,Request)`** share one core `find`, differing only in how the source set is
  resolved (validation via `GetAlbumByUID`/`GetLabelByUID(ctx,projectUID,uid)` →
  `organize.ErrAlbumNotFound`/`ErrLabelNotFound`, **another project's collection alike**; membership via
  `ListPhotoUIDs`/`ListPhotoUIDsByLabel(ctx,projectUID,uid)` — **native, no foreign catalog**; a label
  additionally `LabelRejectionsForLabel`, keyed by the label already resolved in the project). **Project
  scope:** an empty project → `expand.ErrNoProject`; every per-source kNN is
  `FindSimilar(ctx,projectUID,vec,limit,maxDistance)` (the denormalised `embeddings.project_id`), so a
  foreign photo never collects a vote, and hydration goes through `ListByUIDsInProject`.
  The core: **samples** the sources down to `SourceCap` (`sampleSource`, a deterministic even stride, reports
  `source_capped`); loads the sample's embeddings (`GetEmbedding`, `ErrEmbeddingNotFound` is **skipped and
  counted**, not an error — the box is often offline); for each source `FindSimilar` with **bounded concurrency**
  (`errgroup.SetLimit`); **merges with voting** (`match_count` = the number of sources, `distance` = the **minimum**);
  **excludes the collection's members** (the whole point); **vote rule** `min_match_count` (`computeMinMatchCount`,
  `√sourceCount * threshold/base / 2`, **clamp 1..5**, returned); for labels the **rejected** UIDs drop out and
  the **negative-exemplar rule** applies (`vectors.IsNegativeExemplar` — **a no-op without rejections**; albums
  have no rejection model → an asymmetry); hydration (`ListByUIDsInProject` + `mediaurl.DecorateOne`, a non-primary
  stack member is skipped); ordered **`match_count` DESC then `distance` ASC**, truncated to `Limit`. `Result` =
  `{kind,collection_uid,source_photo_count,source_photos_sampled,source_photos_with_embedding,
  source_capped,source_cap,min_match_count,threshold,limit,result_count,reason?,candidates:[Candidate{
  photo,distance,similarity,match_count}]}`; `similarity` = `1 - distance`. **Edge cases**: an empty
  collection → `reason:"empty_collection"`, a collection with no embeddings → `reason:"no_source_embeddings"` (both
  a non-error empty `Candidates:[]`); a single photo degenerates to per-photo similarity. **Read-only** —
  adding goes through `POST /photos/bulk`. Unit tests with fakes + an integration test over real embeddings+DB
  + a tenant-isolation test: identical vectors in both projects, each side finds only its own look-alike),
  `internal/expandapi/`
  (a curator-and-above HTTP API over collection expansion: the `Service` interface (satisfied by `*expand.Service`)
  with two methods `Album`/`Label(ctx,projectUID,uid,req)`, `NewAPI(Config{Service,RequireCurator,RateLimit})` +
  `RegisterRoutes` mounts
  `GET /albums/{uid}/similar` and `GET /labels/{uid}/similar` behind `RequireCurator` then the vector
  throttle `RateLimit` (nil = none; `cmd/kukatko` hands the five whole-project routes one shared `newHeavyVectorThrottle`) (they lead into the
  album/label curation a curator owns); both share `respond` +
  `finder`, differing only in the not-found sentinel; `parseRequest` reads the query `?threshold=&limit=` (empty →
  default, non-numeric / negative → 400); 503 without a backend, 404 missing album/label
  (`organize.ErrAlbumNotFound`/`ErrLabelNotFound`) — one fixed body, so another project's album/label reads
  exactly like a missing one; the search runs in the request's project (`auth.ProjectUIDFromContext`);
  mounted in `serve` (`buildExpandAPI` in
  `cmd/kukatko/expand.go`, takes `mediaStore` for URL stamping)),
  `internal/mcpapi/`
  (**MCP server** — a library exposed to an AI agent over the Model Context Protocol at
  `POST /api/v1/p/{project}/mcp` (there is no unprefixed path);
  `NewAPI(Config{Enabled,Photos,Organize,People,Bulk,Similar,Processing,Media,RequireAuth,PageSize,MaxPageSize})`
  + `RegisterRoutes`. **`Enabled:false` → the servers aren't built and the path answers a bare JSON 404**
  (`handleDisabled`), not a 403 — the caller learns the feature is absent, not that it exists and is being
  withheld. The 404 is mounted rather than left to the router precisely so the path does **not** fall into the
  full binary's SPA catch-all, which used to hand an MCP client `index.html` with a 200 for it to parse as
  JSON-RPC. This is a departure from the local "nil service → 503" idiom and is deliberate, the endpoint is an
  opt-in attack surface. Transport:
  `github.com/modelcontextprotocol/go-sdk` (pure Go, keeps `CGO_ENABLED=0`), `NewStreamableHTTPHandler`
  with `Stateless:true` (each POST standalone → no session state **and** the request context reaches the
  tool handlers), `JSONResponse:true` (tools don't stream) and `DisableLocalhostProtection:true`
  (the DNS-rebinding guard rejects a loopback+non-loopback `Host`, i.e. a reverse proxy; it protects unauthenticated
  local servers, this one requires a principal). **Auth: nothing new** — behind `RequireAuth` (the agent sends
  `Bearer kkt_…`), the role is the **token owner's** role in the URL's project. The boundary is **double**:
  `getServer(*http.Request)` picks a server keyed by `serverKey{canWrite, instructions}` — `canWrite` from
  `auth.ProjectRoleFromContext(...).CanWrite()`, `instructions` from `instructionsFor(name, slug)`, the
  general text plus a paragraph naming the request's project — built on first use and cached under a mutex
  (`API.server`), so one pair per project → a viewer doesn't even see the write tools in `tools/list`; **and** every write handler calls `writerFromContext`
  (registration = UX, the check = the security boundary). The `withCaller` middleware assembles `caller{user, meta}`
  (`audit.FromRequest`) into the context, because a tool handler sees only `ctx`, not `*http.Request` — without a
  principal **fail-closed 401**. Every 401 on the endpoint carries `resourceChallenge` (`challenge.go`):
  `Bearer realm="<absolute URL of the endpoint called>"` — `resourceURL` = scheme (TLS or
  `X-Forwarded-Proto`) + `Host` + `auth.APIBase(ctx)` + `/mcp`, quotes and backslashes dropped;
  `withResourceChallenge` wraps `RequireAuth` so only the guard's own 401 passes the rewriting
  `challengeWriter`, and a request it lets through reaches the transport with the original writer. The URL
  rides in `realm`, not `resource_metadata`, because there is no OAuth to discover (docs/MCP.md).
  **Every tool carries a `Title` and annotations** (`annotations.go`): `readAnnotations` (readOnly),
  `additiveAnnotations` (`destructiveHint:false` — `create_album`/`create_label` only add a row, and MCP's
  default of `true` makes a client ask a human to approve "make an empty album"), `idempotentAnnotations`
  and the plain `writeAnnotations` for `bulk_edit_photos`, which removes as readily as it adds and whose
  repetition is **not** a no-op. All four pin `openWorldHint:false`: the library is a closed world, and MCP
  defaults that hint to `true`. The title is English and not translated — the server has no idea what
  language the agent's human speaks — and without one a client renders the raw `snake_case` name.
  The tools (`tools_search.go` / `tools_collections.go` / `tools_write.go` /
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
  `photoSummary` = `{uid,title,taken_at,media_type,duration_ms?,thumb_url}`, `photoDetail` curated columns,
  **the `exif` blob nowhere**; `page()` reports `total`/`offset`/`remaining` (clamped to 0). **Nothing destructive**
  — no purge/trash/**archiving**/restore/backup/users; `bulkEditIn` therefore omits `Archive`
  (archive → trash → retention purge = deletion in installments) and `Location`.
  **The video fields are reads, and only reads.** A summary carries `duration_ms` so a listing of clips is
  useful without a `get_photo` per row; the detail adds `fps`, `video_codec`, `audio_codec`, `has_audio` (a
  **pointer**: a silent video states its silence, a still is never asked) and `encode_state` — the
  `hls_transcode` step of the processing report, which is what answers "which videos still need preparing".
  `encodeState` asks the optional `EncodeReporter` (satisfied by `*processing.Service`) **only for a
  standalone video**: the report costs two queries and for anything else the answer would be `skipped`
  repeated on every still in the library. A read that fails costs the field, never the record. No tool
  schedules an encode — `POST /process/hls` is the admin surface this server deliberately withholds, and the
  reporter is wired read-only. The `jsonschema` tags carry the descriptions
  of the tool arguments → `//nolint:lll` (the tag is one unbreakable token and is the agent's real interface).
  Unit tests without a DB (helpers, RBAC, `exif` doesn't leak, disabled route) + integration tests over the **real
  MCP transport**, real auth and real `kkt_` tokens; mounted in `serve`
  (`buildMCPAPI` in `cmd/kukatko/mcp.go`, in `discoveryAPIOptions`). See `docs/MCP.md`),
  `internal/review/`
  (**the review game** — a queue of "one at a time" questions and the application of answers;
  **it composes existing pieces, reimplements nothing**. **Project scope (`scope.go`):** `Queue(ctx,projectUID,
  userUID,…)` and `Answer(ctx,projectUID,userUID,…)` (`ErrNoProject` without one); the session is keyed by
  (project, user) and the rotation cursors are per project (`rotation`, `offset`/`advance`), so one player's
  games in two projects never share a pool, a shelf or a window; the face and outlier sources pass the project
  to `Sweeper.Scan`/`OutlierRanker.Outliers`/`SubjectLister.ListSubjects`, every photo is hydrated through
  `PhotoStore.ListByUIDsInProject` (a duplicate pair with a foreign half drops out), the breathers are picked
  from the project's photos, the place estimates of other projects are dropped (`Photo.ProjectID`), and the
  label rotation keeps only the project's labels (the label store lists only the project's, and
  `Scope.LabelsInProject` keeps the filter as a second lock), the label similarity search runs inside the
  project (`Expander.Label(ctx, projectUID, …)`) and the album-variety lookup reads only the project's albums. Before an answer writes anything — a skip included — `proveInProject` checks the question's
  photo(s), subject and label through the required `Scope` (`*ScopeStore`, three `uid = ANY` reads with
  `project_id`): one the project does not hold (another project's, or nobody's) is **`ErrQuestionNotFound`**
  and nothing is written; the write paths (`Assigner.Apply`, `RejectFace`, `ConfirmFace`, `AttachLabelAudited`,
  `RejectLabel`, `ConfirmDuplicate`, `DismissDuplicate`) get the project too. **Five kinds** (`Kinds`): face questions via the
  `Sweeper` interface (satisfied by
  `*sweep.Service` → per-subject candidate search with all its filters: unassigned-only,
  rejections, negative exemplar, min. face size), label questions via `Expander` (satisfied by
  `*expand.Service` → excludes members and rejected ones), and the **three checks over work the machine already
  did** (`extras.go`): `place` via `PlaceReviewer` (`*geoestimate.Reviewer`), `duplicate` via `DuplicateFinder`
  (`*duplicates.Service`; `FindGroups(ctx, projectUID, …)` — the groups are detected inside the session's
  project) and `outlier` via `OutlierRanker`+`SubjectLister` (`*outliers.Service` +
  `*people.Store`), all hydrating their photos through `PhotoStore.ListByUIDsInProject`+`Media`. **Each of the three is
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
  `reasonFor` cannot name it — a game restricted to a switched-off kind reports `"source_off"` instead, since
  a total nothing counted is no evidence about the library), the round mixer prefers whichever kind its
  running share is furthest behind on
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
  library, and a player saying "I don't know" is a fact about the player. Two further dependencies are **read-only extras**, each
  optional and each switched off by a nil: `Albums` (`AlbumMembership`, satisfied by `*organize.Store` →
  `AlbumUIDsForPhotos`) feeds the mixer's album rule, and `Breathers` (`BreatherSource`, satisfied by
  `*review.BreatherStore`) picks the round's non-question card. **Neither can fail a round** — every failure is
  logged and degrades to "that extra is absent". An answer carries back only its `Result` and the session
  counters; there is no per-answer payoff (a confirmed face used to bring back a card with the person's photo
  count and year span — it interrupted the run and was removed).
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
  degraded rebuild); a restricted source whose kind the shares switched off → `"source_off"`, because that
  kind is skipped in `collect` and its zero total would otherwise be read as an empty library — the box
  staging instance, ten populated review-enabled labels under the default `face: 1`, is exactly the case that
  used to answer `"no_labels"` and send the operator to a labels page with nothing wrong on it; neither tier
  producing anything → `"no_candidates"` (all non-error).
  **Infinite means degrading, not stopping.** Running out of one tier fills from the other (both scans see the
  same material, so this is automatic), and a round that came back empty **rotates to the next window and tries
  again** — `collectRotating`, up to `maxRebuildRounds` (3) **inside the one `BuildTimeout`**, stopping early
  when a round found something, was cut short, or the library holds no source to rotate through. An empty
  *window* is not an empty library, and only a genuinely empty one may report so.
  **A rebuild is bounded — the game asks one question at a time, so it must never cost a library-wide
  work list** (it did: 250 s for ~100 subjects on production, see `docs/PERF.md` §3). Faces go through
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
  row (`FacesByKeys`, **read inside the answer's project**: on top of the up-front `proveInProject`, a
  face of another project is simply not found, so the answer is the `"gone"` a photo that exists nowhere
  gets — never `"assigned"`, which would confirm that a guessed face exists and whom it belongs to; the
  outlier no's `detachOutlier` reads it the same way) and derives the action (marker → `assign_person`, otherwise `create_marker` with the stored
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
  split, ordering, NULL-actor/non-review exclusion; for the partial index see migration `0037`).
  **The board is per project** — `Leaderboard(ctx, projectUID, window)`: both queries count only rows with
  `audit_log.project_id = projectUID` and join `project_members`, so only **approved members of that project
  at read time** appear. A member removed from the project vanishes from its board with their past decisions
  there; an instance maintainer who answered while acting in the project counts **only if they are a member**
  (decided: the board is a game among the project's people, and a non-member's name is what a foreign
  tenant must not learn); the same account is counted per project with what it decided in each, and its
  streak is per project too (`TestLeaderboard_countsOnlyTheProjectsApprovedMembers`;
  `internal/reviewapi`'s `TestLeaderboardRoute_isPerProject` proves it over HTTP with `projecttest`),
  `internal/reviewapi/`
  (an HTTP API over the review game: the `Service` interface (satisfied by `*review.Service`) and `Leaderboarder`
  (satisfied by `*review.LeaderboardStore`), `NewAPI(Config{Service,Leaderboard,RequireCurator,RequireAuth,RateLimit})`
  (nil guards/throttle → pass-through) + `RegisterRoutes` mounts `GET /review/queue` (then the heavy vector
  throttle `RateLimit`, inside the guard) and `POST /review/answer`
  behind **`RequireCurator`** (curator and above — they mutate the library) and `GET /review/leaderboard` behind **`RequireAuth`**
  (only aggregates → any logged-in user, even a viewer); the queue reads `?source=` (empty → `both`,
  `review.ParseSource` → 400 on anything else) and `?limit=` (empty → default,
  non-numeric/negative → 400), answer decodes `{question_id,answer}` (`DisallowUnknownFields`, 64 KiB,
  empty fields → 400) and builds `audit.Meta` via `audit.FromRequest` + `auth.UserFromContext`;
  `ErrInvalidQuestion`/`ErrInvalidAnswer` → 400, `ErrQuestionNotFound` (a photo, subject or label outside the
  request's project — `auth.ProjectUIDFromContext` — or nowhere) → 404 with one fixed body, any other error →
  500, `result:"gone"` (a vanished face or marker) stays 200;
  the leaderboard reads `?window=all|7d|today` (default all, `ParseWindow` → 400 on other), asks the
  `Leaderboarder` for the request's project (`auth.ProjectUIDFromContext`) and via
  `buildLeaderboardResponse` returns `{window,caller_uid,entries:[…is_me]}` (the caller from `auth.
  UserFromContext`, `is_me` on the own row, entries never null); 503 without a backend; mounted
  in `serve` (`buildReviewAPI` in `cmd/kukatko/review.go`)),
  `internal/peopleapi/`
  (a read/curation HTTP API over subjects (people/animals/other) — the basis of the People UI. **Project
  scope:** every handler passes `auth.ProjectUIDFromContext` to the store, so a subject of another project
  answers the same 404 as a missing one; the gallery (`GET /subjects/{uid}/photos`) loads the subject first
  (a missing or foreign subject is a 404, no longer an empty page) and resolves the page through
  `ListByUIDsInProject`. The interfaces
  `SubjectStore` (a subset of `people.Store`: `ListSubjects`/`GetSubjectByUID`/`CreateSubjectAudited`/
  `UpdateSubjectAudited`/`DeleteSubjectAudited`/`MergeSubjectsAudited`/`ListPhotoUIDsBySubject` — each mutation
  takes an `audit.Entry`
  built in `auditEntry` (`subject.create`/`update`/`delete`/`merge`, actor from the auth context, details
  name/type; `DELETE` first loads the subject for the details and a clean 404)) and `PhotoStore`
  (`photos.Store.ListByUIDsInProject`)
  → unit-testable with fakes without a DB; `NewAPI(Config{Subjects,Photos,FamilyExport,RequireAuth,
  RequireCurator})`+
  `RegisterRoutes` mounts **flat** paths (not a mounted subrouter, so they coexist with
  `outlierapi`'s `GET /subjects/{uid}/outliers` without a chi Mount conflict): `GET /subjects`
  (RequireAuth, `{subjects:[SubjectCount]}` with marker **and** photo counts), `POST /subjects` (RequireCurator,
  create → 201, name/type validation), `GET /subjects/{uid}` (RequireAuth), `PATCH /subjects/{uid}`
  (RequireCurator, editing name/type/favorite/private/notes/cover_photo_uid), `DELETE /subjects/{uid}`
  (RequireCurator → 204), `POST /subjects/{uid}/merge` (RequireCurator, `{keeper_uid}` → `people.MergeResult`;
  the **path** subject is the one merged away and deleted. `handleMerge` loads **both** subjects first, so a
  missing one is a 404 before any mutation and the audit details keep the source's name — the source is gone
  by the time anyone reads the trail, and a merge has no undo. Merging a subject into itself is refused here,
  before the store, so the answer is the same 400 whether or not the uid exists),
  `GET /subjects/{uid}/photos` (RequireAuth, a paginated gallery of the subject's photos
  `{photos,total,limit,offset,next_offset}` — `ListPhotoUIDsBySubject` (distinct non-invalid
  markers, non-archived, newest-first) → page → `ListByUIDsInProject` → reorder by the uid order); body
  decode `DisallowUnknownFields` + 1 MiB limit + empty name (or empty `keeper_uid`) → 400; sentinels mapped
  `ErrSubjectNotFound`→404/`ErrInvalidType`+`ErrMergeIntoSelf`→400; mounted by the eighth `server.WithAPI`
  (`buildPeopleAPI` in `cmd/kukatko/people.go`). The **edit, the delete and the merge** additionally schedule
  a rewrite of the library's genealogy export (`FamilyExportEnqueuer`, nil = the export is off): a subject
  write is a change to the family tree more often than it looks — a rename changes what the tree says
  somebody is called, a delete cascades away every family they were a partner or a child in, and a merge
  moves their place in it — so `families.yaml` would otherwise describe people who no longer exist that way.
  Like the sidecar enqueues it is best-effort and never fails the edit),
  `internal/avatar/`
  (the **small square picture of a face**, cut server-side — handed a photo its caller has already loaded
  inside the request's project, so it has no project of its own; its cache is keyed by the file hash: the subject avatar the people index draws
  (`internal/avatarapi`) and, by the same renderer, any single face a page shows as a square
  (`GET /photos/{uid}/face`, `internal/photoapi/facecrop.go`) — it takes a photo and a plain box and knows
  nothing about subjects. A tile is a
  ~150 px square but a face box spans a few per cent of its photo, so cropping one in the browser meant
  fetching a whole-frame preview per tile — measured on the real library, **125 Mpx of image for 72 tiles**.
  `Renderer` = `New(source,cacheDir)` + `Open(ctx,photo,face) (io.ReadCloser, etag, error)`: a `nil` face is a
  hand-picked cover, shown whole (`centreSquare`, the same crop a `tile_*` size takes); a face is padded 0.3
  per side, clamped to the frame, squared on its longer **pixel** edge and slid back inside rather than
  clipped (`faceSquare`) — the geometry the client's `padBbox`+`squareCrop` used to do, so the pictures did
  not change when the work moved. Rendered at `Side` = 320 px (a 2× display's worth of a 150 px tile) at
  quality 85 — measured at ~15 kB a tile against ~190 kB for the `fit_1280` it used to take — and **never upscaled**: a crop with fewer pixels than that is emitted at its own
  size. The source is never the original — `sourceSize` picks the smallest `fit_*` rung (720/1280/**ceiling
  1920**) that still puts 320 px across the padded crop, measured against the photo's *display* frame
  (`displayFrame` swaps the quarter-turn orientations), and a cover takes `thumb.GridSize`; so rendering is a
  JPEG decode of at most a 1920 px preview, with no HEIC/RAW shell-out. **Storage is cache-only**, in the
  thumbnail cache's sharded layout under its own prefix —
  `<prefix>avatar/<aa>/<bb>/<cc>/<hash>_<variant>_320.jpg` (`RelPath(prefix,hash,variant)`, the prefix being
  that of the photo's own original),
  `CacheSubdir` exported for the wipe (`internal/reset` sweeps it beside `thumb` and `storyboard`) — and
  deliberately **never uploaded to the object store**. `variantKey` is `cover` or a digest of the box rounded
  to four decimals, so one photo standing for two people yields two entries and float noise forks neither;
  the ETag is `hash-variant-side`. Writes are atomic (temp + rename), so a concurrent render costs a
  duplicated decode and never a torn file. `ErrInvalidHash` (a hash that cannot address the cache),
  `ErrRenderFailed`),
  `internal/avatarapi/`
  (an HTTP API over it: the interfaces `Subjects` (`people.Store.SubjectAvatar(projectUID, uid)`), `Photos`
  (`photos.Store.GetByUIDInProject`) and `Renderer`; both lookups run in the request's project, so the
  avatar of another project's subject is the same 404 as a missing one, and no byte of another project's
  photo is cut into an avatar (`*avatar.Renderer`, nil → 503) → unit-testable with fakes and no
  disk; `NewAPI(Config{Subjects,Photos,Renderer,RequireAuth})`+`RegisterRoutes` mounts the **flat**
  `GET /subjects/{uid}/avatar` (RequireAuth — it illustrates the subject list, which any signed-in user
  reads), coexisting with `peopleapi`'s `/subjects` group and `outlierapi`'s `/subjects/{uid}/outliers`.
  Answers `image/jpeg` with the renderer's ETag and `private, max-age=600, must-revalidate` — *not* immutable,
  because the URL names a subject and which picture stands for it changes; `If-None-Match` → 304. Everything
  that means "no picture" — `ErrSubjectNotFound`, `ErrNoAvatar`, `photos.ErrPhotoNotFound` — is the same 404,
  since the grid draws its placeholder for all three; a render failure is a logged 500. Mounted in `serve`
  (`buildAvatarAPI` in `cmd/kukatko/avatar.go`, which builds its own `thumb.Thumbnailer` over the shared
  cache)),
  `internal/userpic/`
  (a **user's profile picture** — the small square that stands for an *account* wherever a person is named,
  instead of the coloured initial. It is not one thing but the first of four answers that exists, and the
  order is the point: an uploaded picture, a library photo the user picked (shown only to a viewer who may
  see that photo's project), the avatar of the subject the account's membership of the **viewer's
  project** links — the **default**, so a linked account wears that face with no action from anybody; a
  request with no project skips the step — and nothing, which the HTTP layer answers 404 and a client draws
  as the initial. **In front of the whole chain** sits `mayShowAccount` (SEC-025): the account is shown
  only to itself, a maintainer, or a viewer it shares an active project with as approved, not withheld
  members (`Members.SharesProject` = `project.Store.SharesProject`); with `viewer.ProjectUID` set the
  account must be such a member of that project (a maintainer asks `SharesProject(project, account,
  account)`). Anybody else gets `ErrNoPicture` before a row is read — the answer a made-up uid gets —
  because an upload holds no photo whose project could be checked. `Service` =
  `NewService(Config{Pictures,Members,Subjects,Photos})` with `Resolve(ctx, viewer, userUID)` (→
  `Source{Upload|PhotoUID+Photo,Face}` + `Origin` — `Photo` is the record the chain loaded and authorized, so
  the HTTP layer renders it and never loads a photo by uid itself: a picked photo via
  `photos.GetAcrossProjects` followed by the `maySee` check, an inherited face via `GetByUIDInProject` in the
  viewer's project, an avatar photo no longer there being `ErrNoPicture`), `Describe(ctx, viewer)` (→ `State{Origin,PhotoUID,SubjectUID}`,
  the same walk without the bytes, for the caller's own account page), `SetUpload`, `SetPhoto(ctx, viewer,
  photoUID)`, `Clear`. `Viewer{UserUID, Maintainer, ProjectUID}` is who asks and from which project
  (`userpicapi` fills it from the principal and `auth.ProjectUIDFromContext`): the face step reads the
  pictured account's `project.Member.SubjectUID` in `viewer.ProjectUID` and asks
  `people.Store.SubjectAvatar` in that project; a picked photo answers only when `maySee` — a maintainer,
  or an **approved member** of `photo.ProjectID` — and otherwise falls through exactly as a private one
  does (so the serving route ends in the same 404); `SetPhoto` refuses a photo of a project the caller may
  not see with `ErrPhotoNotFound`. The four dependencies are interfaces (`*Store`, `*project.Store`,
  `*people.Store`, `*photos.Store`) so the chain unit-tests with fakes. **Every link is
  re-checked on every call** rather than trusted from when it was set: a picked photo that has since been
  archived, purged or flagged **falls through** instead of failing, which is the difference between a chain
  and a stored decision. `Showable(photo)` is the one judgement — not archived, not `private`, not
  `hidden_from_library` — made both when a photo is picked (`SetPhoto` → `ErrPhotoNotAllowed` /
  `ErrPhotoNotFound`) and every time one is served, since a photo hidden after the fact must stop being
  shown; letting a private photo onto a profile, seen by every reader of every thread the account writes in,
  would be the plainest way to sidestep the flag. `Normalize(data)` is what is actually stored for an upload:
  decode (a **whitelist** of `jpeg`/`png`/`webp` — the registered decoders are a global set the thumbnailer
  also contributes to, so "whatever decoded" would make the accepted formats an accident of the import
  graph), turn upright by the EXIF orientation read out of the submitted bytes with goexif + `imgconvert.Orient`
  (a phone writes an upright picture and a tag; without this a portrait selfie would be stored on its side
  for good), centre-crop square, scale to at most `MaxSide` = 512 **never upscaling**, re-encode JPEG q85 —
  so the stored picture carries no metadata, no GPS and no 40 Mpx panorama, and the submitted original is
  never kept. `ReadUpload(r)` bounds a body at `MaxUploadBytes` = 8 MiB by reading one byte past it — which
  bounds nothing once decoded (8 MB of single-colour PNG can name 30000×30000), so `Normalize` also refuses,
  from the header alone, a picture whose bitmap exceeds `MaxDecodedBytes` = 256 MiB (`ErrTooLarge`), and
  `SetUpload` reserves the decode from `Config.DecodeBudget` (the process's one `imgconvert.DecodeBudget`;
  over the whole budget = `ErrTooLarge`).
  `Store` over table **`user_pictures`** (migration `0078`: `user_uid` PK → `users` `ON DELETE CASCADE`,
  `kind IN (upload|photo)`, `image BYTEA`, `photo_uid`, `updated_at`, plus a CHECK that exactly one source is
  set and that it is the one `kind` names) — `Get`/`SetUpload`/`SetPhoto`/`Clear`, the set an upsert so
  switching from an upload to a pick leaves no orphaned bytes. Two deliberate schema decisions: the bytes are
  **in Postgres, not the object store** (no `storekeys` Kind → backup, storage migration, the orphan sweep and
  the wipe all unchanged; it rides the database dump the backup already takes), and **not on `users`**, whose
  one canonical column list (`auth.userColumns`) is read on every authenticated request — a picture there
  would be loaded by every login. `photo_uid` carries **no foreign key**: the reset preserves this table
  (`internal/reset`, `preservedTables`) and TRUNCATE refuses a preserved table pointing into a wiped one, and
  the fallback a FK would enforce is needed anyway. **Nothing here is audited**, following
  `POST /auth/password` — the audit trail records what was done *to* an account by somebody else),
  `internal/userpicapi/`
  (its HTTP surface: `NewAPI(Config{Pictures,Renderer,RequireAuth})` — no photo store: it renders
  `Source.Photo` as the chain resolved it, so it holds no unscoped photo read of its own; + `RegisterRoutes` mounts four
  **flat** routes so `/auth/picture` coexists with `auth`'s own `/auth` group — `GET /users/{uid}/avatar`,
  `GET`/`PUT`/`DELETE /auth/picture`, all RequireAuth. Serving has the same shape as `avatarapi`: an upload
  streams straight from its row with an ETag that is a SHA-256 of the bytes (so replacing the picture
  invalidates every cached copy and re-uploading the same file invalidates none), a picked or inherited photo
  goes through the **same** `*avatar.Renderer` (`nil` face = whole frame for a pick, the subject's box for an
  inherited face; a nil renderer → 503, which an upload is unaffected by), `If-None-Match` → 304, and every
  "no picture" — `ErrNoPicture`,
  `photos.ErrPhotoNotFound` — is the same **404**. The `Cache-Control` is picked per caller (`cachePolicy`):
  `private, max-age=600, must-revalidate` for somebody else's picture, `private, max-age=0, must-revalidate`
  for the **caller's own**, which is the only one that changes under its own reader — ten minutes of
  freshness on that one left the bar showing the picture its owner had just replaced, across a reload, since
  `must-revalidate` governs only a *stale* entry. `PUT` tells its two payloads apart by content type
  (`multipart/form-data` field `picture` → upload, anything else → JSON `{photo_uid}`), bounds the upload
  twice (the declared `Content-Length` and a `http.MaxBytesReader`, so a client that lies is stopped too) and
  maps outcomes: `ErrTooLarge` → **413**, `ErrUnsupportedFormat`/`ErrPhotoNotAllowed`/`ErrPhotoNotFound` →
  **400**, everything else a logged 500. The three writes are **self-scoped** — the account comes from
  `auth.UserFromContext`, never from the request. `viewerOf(r)` builds the chain's `userpic.Viewer` from the
  principal (uid, `Role.CanMaintain()`) and `auth.ProjectUIDFromContext` — empty on the instance routes
  of `RegisterRoutes` (`RequireSignedIn`: `GET /users/{uid}/avatar`, `GET`/`PUT`/`DELETE /auth/picture`), so
  the inherited face is skipped there, and the request's project on `RegisterProjectRoutes` (`RequireAuth`:
  the avatar and `GET /auth/picture` under `/p/{project}`, `projectAPI.userPictures`), which the SPA asks
  inside a project. Mounted in `serve` (`buildUserPicAPI` in
  `cmd/kukatko/userpic.go`, in `discoveryAPIOptions`, sharing the avatar renderer's cache directory)),
  `internal/organize/`
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
  **covers** (`covers.go`) `AlbumCovers(uids)`/`LabelCovers(uids)` → `map[uid]Cover{PhotoUID,FileHash,FilePath}` (the path
  so the thumbnail URL takes the prefix of the cover's own original):
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
  `AddPhotoTx`/`AttachLabelTx` (package functions on the caller's `pgx.Tx`, idempotent, the label manual —
used by `uploadlink.RecordUpload` so filing an upload joins its audited transaction),
`AddPhotosAudited`/`RemovePhotosAudited` and `CreateLabelAudited`/`UpdateLabelAudited`/`DeleteLabelAudited`/
  `AttachLabelAudited`/`DetachLabelAudited` run the change and `audit.Write` **in one transaction** (durable
  audit — when the mutation rolls back, no audit record is created; the shared `inAuditedTx` +
  `insertAuditedWithUniqueSlug`, which resolves a slug collision on create/update by retrying through separate transactions
  and writes the audit only for the successful attempt); the non-audited variants remain for system callers
  that have no actor);
  **featured albums** (`featured.go`, migration `0094_albums_featured.sql`: `albums.featured_until`/
  `featured_at`/`featured_by` FK users `ON DELETE SET NULL`, a CHECK that only `type = 'album'` is ever
  featured, a partial index on `featured_at` over the ever-featured rows): `Album.FeaturedUntil` is read by
  **every** album projection (`albumColumns`, the index and search SQL) as the *active* end —
  `CASE WHEN featured_until > now() …` — so a lapsed feature is nil without any job clearing it;
  `FeatureAlbumAudited(uid,until,byUID,entry)` (locks the row, `ErrAlbumNotFound`/`ErrNotFeaturable`, a
  running feature keeps its `featured_at`, a lapsed one restarts it; a **`FeatureObserver`** set with
  `WithFeatureObserver` hears `Featured(ctx, tx, Featuring{AlbumUID, ActorUID})` inside the transaction —
  **only for a fresh feature**, read under the row lock as `featured_until > now()` before the write, so
  re-dating a running feature is silent; it is how `internal/albumfeaturedjob` queues the "new featured
  album" announcement), `UnfeatureAlbumAudited` (clears all
  three, idempotent), `ListFeaturedAlbums(includePrivate)` → `[]FeaturedAlbum` (active, newest feature
  first, the index's count, the cover resolved as `AlbumCovers` does plus its BlurHash, `FeaturedBy` — one
  statement whose CTEs scan only the featured albums' membership) and `FeaturedAlbumTx(ctx, tx, uid)` (the
  same record for one album on the caller's transaction, private or not, `ErrNotFeatured` when the strip
  would not list it — for the announcement job); validating `until` is the caller's job.
  **`ProjectCover(ctx, projectUID, includePrivate)`** (`projectcover.go`) → `(Cover, ok)`: the picture
  the project chooser shows for a project — the first featured album's hand-picked cover, else that
  album's newest photo, else the project's most recently added photo; only a live, library-visible,
  non-private photo, a private album only with `includePrivate`, every statement confined to the
  project. `cmd/kukatko` (`projectcover.go`) turns it into `GET /auth/me`'s `cover_thumb_url` through
  `mediaurl` behind `auth.APIConfig.Covers` (`auth.ProjectCovers`), asked only for an enterable membership.
  **Project scope** (`store_project.go`, the `internal/photos` convention): every method takes `projectUID`
  after `ctx` (`AddPhotoTx`/`AttachLabelTx` after the `tx`), never defaulted — empty is `ErrNoProject`.
  `albums`/`labels` carry `project_id` (every insert writes it, slugs are unique per project); the join
  tables and favourites/ratings are scoped through their album, label or photo. An album, label or photo
  of another project is the same error as a missing one (`ErrAlbumNotFound`/`ErrLabelNotFound`/
  `ErrPhotoNotFound`, an empty list). Writes prove their targets on their own transaction first:
  `addAlbumPhotoRows` proves the album and **the whole photo batch in one statement**
  (`requirePhotosInProject`, `unnest` + `NOT EXISTS`) — one foreign uid refuses the batch, nothing is added,
  no audit row — and inserts it in one statement; `attachLabelRow` proves label and photo; a hand-picked
  cover (create/update/`SetCover`) must be a photo of the project; update/delete/feature carry
  `project_id` in their `WHERE`. Audited writes default the entry's `ProjectUID`. Reads filter the album/label
  by project and join only the project's photos (counts, covers, the featured strip's collage, the
  photo's memberships, favourites/ratings listings and `…Among` annotations, the sidecar readers).
  `FeaturedAlbumTx` is the one explicitly **unscoped job loader** (the announcement job's payload carries
  only the album uid); the sidecar job passes the loaded photo's `ProjectID`)), `internal/organizeapi/`
  (a read/curation HTTP API over albums and labels — the basis of the Albums/Labels UI: the interfaces `AlbumStore`/
  `LabelStore` (subsets of `organize.Store`) → unit-testable with fakes without a DB; every store call
  passes the request's project (`projectOf(r)` = `auth.ProjectUIDFromContext`), so another project's album
  or label answers the very 404 a missing uid gets, and adding a foreign photo is the "photo not found" 404;
  `NewAPI(Config{Albums,Labels,RequireAuth,RequireCurator})`+`RegisterRoutes` mounts two
  subrouters: **albums** `GET /albums` (RequireAuth, `{albums:[AlbumSummary]}` — counts, the effective
  `cover_uid`, the further covers `cover_uids` and the range `taken_from`/`taken_to`),
  `POST /albums` (RequireCurator, 201, `title` required, type validation via `ErrInvalidType`),
  `GET /albums/{uid}` (RequireAuth), `PATCH /albums/{uid}` (RequireCurator, edits
  title/description/cover_photo_uid/private; **the structural `type` is preserved** —
  the handler loads the existing album and does not take `type` from the body, so folder/moment/… can't be overwritten),
  `DELETE /albums/{uid}` (RequireCurator → 204), membership (RequireCurator) `POST /albums/{uid}/photos`
  `{photo_uids:[…]}` (adds, no position — an album is always chronological),
  `DELETE /albums/{uid}/photos` `{photo_uids:[…]}` (removes, idempotent) — both
  membership endpoints return the current chronological order `{photo_uids:[…]}`, first verifying the
  existence of the album (`requireAlbum` → 404); manual reordering `PATCH /albums/{uid}/order` was
  removed (→ 404); **labels** `GET /labels` (RequireAuth, `{labels:[LabelCount]}`),
  `POST /labels` (RequireCurator, 201, `name` required), `GET /labels/{uid}` (RequireAuth),
  `PATCH /labels/{uid}` (RequireCurator, name/priority), `DELETE /labels/{uid}` (RequireCurator → 204),
  attachment (RequireCurator) `POST /labels/{uid}/photos` `{photo_uid,source?,uncertainty?}` → 204 (source validation
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
  (`buildOrganizeAPI` in `cmd/kukatko/organize.go`, sharing one `organize.Store` for both albums and labels);
  **featured albums** (`featured.go`) behind their own `FeaturedStore` interface (`Config.Featured`; nil =
  the routes are not mounted) so featured labels can join it later: `GET /albums/featured` (RequireAuth;
  private albums only for `Role.CanCurate()`, covers minted into `thumb_url` via `Config.Media`, plus
  `default_days` = `Config.FeaturedDefaultDays` — `albums.featured.default_days`, outside 1…366 → 61 — and
  `max_days` = `MaxFeaturedDays` 366), `PUT /albums/{uid}/featured` `{until?}` (empty body = now + default;
  past or beyond 366 days → 400, `ErrNotFeaturable` → 400; audited `album.feature`) and
  `DELETE /albums/{uid}/featured` (audited `album.unfeature`), both RequireCurator and both returning the
  album; `Config.Now` pins the clock in tests)),
  `internal/feedback/`
  (the DB layer for **persisted rejections** (negative feedback) — a permanent user "no" to a face↔subject
  or photo↔label guess. **Project scope of the face opinions (`project.go`):** `RejectFace`/`UnrejectFace`/
  `ConfirmFace`/`UnconfirmFace` take `projectUID` after `ctx` and, on their own transaction, prove the face's
  photo and the subject both belong to it (`requireFaceTargetInProject`) — otherwise `ErrTargetNotFound`
  (also for an un-reject/un-confirm of a missing target, which used to be a silent no-op), with no row and no
  audit entry; the entry's `ProjectUID` is defaulted. The reads keyed by a subject the caller resolved in
  its project (`FaceRejectionsForSubject`, `FaceConfirmationsForSubject`, the `Is…` probes) stay keyed by uid.
  **Label and near-duplicate opinions** follow the same rule through the shared `inScopedAuditedTx` (a
  `proof` run on the write's transaction): `RejectLabel`/`UnrejectLabel` prove the photo **and** the
  label in the project (`labelProof`), `DismissDuplicate`/`UndismissDuplicate`/
  `ConfirmDuplicate`/`UnconfirmDuplicate` prove both photos of the pair (`pairProof`) — otherwise
  `ErrTargetNotFound`, no row, no audit entry; `ErrNoProject` without a project.
  **Repeated-marker dismissals** do the same: `DismissDuplicateMarkers`/`UndismissDuplicateMarkers` take
  `projectUID` and prove the photo **and** the subject with the face opinions' proof (`inProjectAuditedTx`).
  Until 2026-10-14 they took no project, so a curator of one project could settle another's group
  (SECURITY_AUDIT SEC-024). `LabelRejectionsForLabel` stays keyed by a label its caller resolved in the
  project. The three **bulk exclusion lists** a per-project scan reads (`DismissedDuplicatePairs`,
  `ConfirmedDuplicatePairs`, `DismissedDuplicateMarkerGroups`) take `projectUID` (`ErrNoProject` without
  one). They join the opinion's photos (and the group's subject) on `project_id`, so they return only that
  project's rows, never the whole instance's. The review game passes its session's project to the
  label and pair writes like the HTTP layer does. The face opinions close the gap where a rejection wasn't kept and the same
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
  `DismissedDuplicatePairs(ctx, projectUID)` (→ `[]DuplicateDismissalKey`, the project's whole set in one
  query — detection scans the project's catalog in one pass and needs the whole exclusion set up front); sentinel `ErrSamePhoto`
  (→400, a photo isn't a duplicate of itself). **Why it exists:** duplicate detection is derived state,
  recomputed on every `GET /duplicates` from hashes and embeddings, which the user's
  disagreement doesn't change — without persistence the same pair would be offered forever).
  **Duplicate confirmations** (`duplicateconfirmations.go`, table `duplicate_confirmations`, migration
  `0054_duplicate_confirmations.sql`) are the dismissal's **positive mirror**: "yes, this really IS the same
  photo twice", written by the review game's duplicate check. The same shape and rules as the dismissal, down to
  the unordered pair and the `CHECK (photo_uid COLLATE "C" < other_uid COLLATE "C")` — carried **from the start**
  here, unlike `0034`, which needed `0038` to repair it. `ConfirmDuplicate`/`UnconfirmDuplicate` (idempotent
  audited insert/delete, actions `duplicate.confirm`/`duplicate.unconfirm`), `IsDuplicateConfirmed`, bulk
  `ConfirmedDuplicatePairs(ctx, projectUID)`. **Why it exists:** the duplicates page only ever offered one way to agree —
  merging, which archives copies and is not something a curator does one pair at a time from a game — so
  agreement evaporated and a group somebody had already looked at was indistinguishable from one nobody had.
  It changes **nothing** about detection: a confirmed pair is still detected, still shown, still mergeable; what
  it buys is `Group.Confirmed` and the ranking that puts it first. Merging stays an explicit, separate act.
  Both pair opinions share their machinery (`pairs.go`: `photoPair`, the `pairOpinion` statement bundle,
  `recordPair`/`removePair`/`hasPair`/`listPairs` and `checkPairKey`, which is where the ErrEmptyKey-vs-ErrSamePhoto
  distinction and the smaller-uid-first normalisation now live **once**) — the two tables are the same shape down
  to the CHECK, and writing the four operations twice is exactly how the second one drifts from the first. What
  stays duplicated is the documented API surface of each (its key type, its sentinels, the paragraph saying why
  it mutates nothing): collapsing two documented APIs into one generic pair would cost more in readability than
  the repetition does.
  **Repeated-marker dismissals** (`markerdismissals.go`, table `duplicate_marker_dismissals`, migration
  `0050_duplicate_marker_dismissals.sql`) are a fourth kind: "this person **really is** marked more than once on
  this photo" — a double exposure, a mirror, a photo of a photo, a face on a poster behind its owner. Keyed by
  the **(photo, subject) pair**, which is exactly the group `internal/dupmarkers` shows; deliberately **not** by
  the markers, whose uids come and go when a photo is re-detected and whose replacement would resurrect a
  settled group. `UNIQUE (photo_uid, subject_uid)`, both FKs `ON DELETE CASCADE`, `dismissed_by` FK users
  `ON DELETE SET NULL`. `DismissDuplicateMarkers`/`UndismissDuplicateMarkers(ctx, projectUID, key, entry)`
  (idempotent audited insert/delete, actions `duplicate_marker.dismiss`/`duplicate_marker.undismiss`; the
  photo and the subject must both be the project's, else `ErrTargetNotFound`),
  `IsDuplicateMarkersDismissed`, bulk `DismissedDuplicateMarkerGroups(ctx, projectUID)`
  (→ `[]DuplicateMarkerDismissalKey`, the project's whole set in one query — the listing recomputes its
  groups in one pass and needs the whole exclusion set up front). **Why it exists:** the repeated-marker listing is derived
  state too, so without this the same false alarm would be re-offered on every reload and a curator would keep
  clicking past it),
  `internal/feedbackapi/`
  (an HTTP API over rejections — the `Store` interface (a subset of `feedback.Store`) → unit-testable with fakes;
  the four face, the two label and the four near-duplicate-photo routes pass `auth.ProjectUIDFromContext`
  (the label/pair ones through the `…InProject` store methods), so an opinion about another project's face,
  subject, photo or label is the same 404 as one about a missing uid;
  `NewAPI(Config{Store,RequireCurator,RequireWrite})`+`RegisterRoutes` mounts the subrouter `/feedback`
  (the face, label and repeated-marker routes behind `RequireCurator`, the four near-duplicate-photo ones —
  `duplicate-dismissals`, `duplicate-confirmations` — behind `RequireWrite`):
  `POST /feedback/face-rejections` `{photo_uid,face_index,subject_uid}` (RequireCurator → 204),
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
  `internal/phototask/`
  (**project scope (`store_project.go`, the photos convention):** `photo_tasks.project_id` is the task's
  project, and its photos, participants and thread belong to it through the task; every `Store` method
  takes `projectUID` after `ctx` (`Get`/`List`/`Summary`/`WaitingOnMe`/`OpenForPhotos`/`Create`/`Update`/
  `UpdateTx(ctx, tx, projectUID, …)`/`Delete`/`AddPhotos`/`RemovePhotos`/`Participants`/`Assign`/
  `Unassign`/`Join`), never defaulted (`ErrNoProject`); a task of another project is the same `ErrNotFound`
  as a missing one (`Participants` included, `Join` writes nothing), listings, filters and the summary
  behind the navbar badge count only the project (`Filter.conditions(projectUID)` binds `t.project_id` as
  `$2`), `Create` writes `project_id` explicitly and **freezes only photos of its project** — the batch is
  proved in one statement (`photosInProjectSQL`) before anything is written, one foreign uid is
  `ErrPhotoNotFound` and nothing is created or added (`AddPhotos` likewise); `InProject(ctx, projectUID,
  taskUIDs)` tells a foreign or missing task from an empty one for `GET /photos?task=`; audited writes default
  the entry's `ProjectUID`. `Digests` (the daily task digest) stays instance-wide until the digest step;
  the DB layer for the **work queue**: a question about a frozen group of photographs — what the
  curation of an inherited library actually ends at, "in which year was this rebuilt", which only a
  person can answer; two tables from migration `0079_photo_tasks.sql`: `photo_tasks` (`uid PK` (prefix
  `tk`), `title` CHECK 1–200 (the question in one line — it is the page heading and the text of the
  link that gets sent around), `body` ≤ 8000 (Markdown context), `state VARCHAR(16)` CHECK in
  `question`/`working`/`review`/`done`/`rejected`, `resolution` ≤ 4000, `source_query` ≤ 2000, `created_by`/`closed_by`
  FK users **SET NULL**, `created_at`/`updated_at`/`state_at`/`closed_at`, `state_by` (migration
  `0082_photo_tasks_state_by.sql`: who last moved the state, FK users SET NULL, stamped with `state_at`;
  NULL on the rows that predate it, read as the creator), `options TEXT[] NOT NULL DEFAULT '{}'`
  (migration `0083_photo_tasks_options.sql`: the **quick answers** — up to five short strings a client
  draws as buttons; choosing one posts a comment with exactly that text, so the thread stays the one
  record; the CHECK pins only the count, the per-option rules — trimmed, non-empty, ≤ `MaxOptionLen`
  60 characters, distinct — are `normalizeOptions` in `models.go` with its own sentinels
  `ErrTooManyOptions`/`ErrEmptyOption`/`ErrDuplicateOption` (+ `ErrTooLong`), and `ReviewApprove`
  (`Schvaluji.`) / `ReviewReturn` (`Vrátit k přepracování.`) are the two built-in answers a `review`
  task without options is given by the client, fixed strings so an agent can match them whatever
  language the person read the page in), plus the two constraints that
  make a closed task inseparable from its explanation — `photo_tasks_closed_is_explained` (a closed
  state carries `closed_at` **and** a non-empty resolution) and `photo_tasks_open_is_not_closed` (an open
  one carries neither, so reopening cannot leave a stale "closed by")) and `photo_task_photos`
  (`(task_uid, photo_uid)` PK, both **CASCADE**, `added_at`; the **frozen** membership — an explicit list,
  never a stored query, because a task exists *because* the data is wrong and is answered by fixing it,
  so a live group would empty itself the moment the work was done and take the record of what was
  touched with it. `source_query` keeps the rule that produced the group as evidence and the server
  never runs it); `State` with `Valid`/`Closed`/`NeedsHuman` + `States`/`OpenStates`, `Task`,
  `Update` (pointer fields: nil = leave alone),
  `Filter{CallerUID,States,Open,Answered,Waiting,Search,PhotoUID,ParticipantUID,Limit,Offset}`
  with `Page()` (clamped to `DefaultLimit` 50 / `MaxLimit` 200) and `conditions()` — where `Search` runs
  through `immutable_unaccent` on **both** sides, like every other text search in the library, so "dum"
  finds "dům" and a queue nobody can spell is still a queue somebody can search; the rules of an edit
  are the **pure** `applyUpdate`/`newFields`/`closure`/`diff` in `apply.go`, so the store writes what they
  return and decides nothing (a state that moves stamps `state_at` + `state_by` and recomputes the closing
  marks; one that stays put keeps both, which is what makes "has anybody replied since" mean anything);
  `Store` = `NewStore(pool)`: `Get(ctx, uid, callerUID)`/`List` (returns the page **and** the total before
  it; **every read takes the caller** — it is `$1` in the projection, see below)/`Create`/`Update`/
  `Delete`/`AddPhotos`/`RemovePhotos` (each in one transaction with its `audit.Write`, via `inTx`;
  membership writes `touch` the task so a batch counts as activity) and `OpenForPhotos` (the reverse
  lookup the photo detail draws its chip from, bulk-shaped so it can never become an N+1);
  **`UpdateTx(ctx, tx, uid, upd, entry)`** is `Update` on the **caller's** transaction (same rules, same
  participation join, same audit entry, sentinels unwrapped, nothing read back) — how a thread answer moves
  the task in one commit: `phototaskapi` runs it as the `comments.Along` of `CreateAlong`;
  **what a task is opened with** beyond its own text is one struct, `Opening{PhotoUIDs,AskedUIDs}`, the
  second argument of `Create` — two lists that may each be empty and are both written in the opening's
  transaction. `PhotoUIDs` is the frozen group and **zero is allowed**: a task about the library rather
  than about particular photographs has nothing to freeze (see `docs/ARCHITECTURE.md`), `MaxPhotos`
  (1000) is unchanged and photographs may be added later. `AskedUIDs` hands the task over as it is
  opened — `askParticipants` runs the same `assignSQL` a later `Assign` does and writes one
  `audit.ActionTaskAssign` entry per person, so a task handed over at birth is indistinguishable, in the
  participants and in the trail, from one handed over afterwards; `uniqueUIDs` drops blanks and repeats
  so asking the same person twice asks them once, and an account that is not an **approved member of the
  task's project** (`project.RequireApprovedMembers` in `ask`, the one step `Assign` and `askParticipants`
  share; no account at all, another project's account, a pending member, a maintainer outside the project)
  fails the whole transaction with the same `ErrUserNotFound` rather than leaving half a hand-over; **the daily
  digest's reads** (`digest.go`), **per (project, person)**: `Digests(ctx)` →
  `[]Digest{ProjectUID,ProjectSlug,ProjectName,UserUID,Email,DisplayName,Total,Tasks
  (≤ DigestLimit = 20, newest activity first),NewestActivityAt,DigestedAt}` — the listing's `waitingSQL`
  turned inside out, one query over every participant of every open task (reusing `activityJoin`, so the
  mail and the „Na mně" filter can never disagree about whose move it is), grouped by the task's project
  and the participant, restricted to enabled accounts with an **approved membership** of that (active)
  project and to pairs with a waiting task whose activity is newer than **`project_members.task_digest_at`**
  (the membership's watermark; the account-wide `users.task_digest_at` of migration 0084 was dropped by
  0110; NULL = never written to); `DigestTargets(ctx)` → the `[]DigestTarget{ProjectUID,UserUID}` the
  daily scheduler enqueues one job for each; `DigestFor(ctx, projectUID, userUID)` → that one pair's
  digest or nil (both required); and `MarkDigested(ctx, projectUID, uid, at)` which stamps the membership
  (an unknown membership is not an error); consumed by `internal/taskdigestjob`;
  **who is on a task** (`participants.go`, table `photo_task_participants` from migration
  `0081_photo_task_participants.sql`): `Participant{UserUID,Name,JoinedAt,AddedByUID,AddedByName,Former,
  AddedByFormer}` (a participant — or whoever asked them — who has left the task's project, a system
  administrator included, reads with no name and the flag set, `participantFormerParam`/`…Task`, CAT-4; so do the task's
  creator, closer and last actor — `Task.CreatedByFormer`/`ClosedByFormer`/`LastActivityByFormer`,
  `project.ShownNameSQL` over `t.project_id`),
  `Participants`/`Assign`/`Unassign`/`Join`, `Filter.ParticipantUID`, and — the part that makes the list
  true without anybody maintaining it — an idempotent `joinParticipant` inside *every* write's transaction,
  so acting on a question is what puts you on it. `added_by` is the one distinction the table carries:
  NULL = joined by acting, a uid = was asked by that person, which is the difference between "Anna is on
  this" and "you put Anna on this". `Join` is the unaudited, transaction-free form for the one act the
  package does not own — writing in the thread, which belongs to `internal/comments` — and `Assign`
  *upgrades* an existing row (somebody who acted and is then asked has been asked) while `joinParticipant`
  never downgrades one. **`AssignObserver`** (`WithAssignObserver`, the people store's `TagObserver`
  pattern) hears every *fresh* hand-over — `Assignment{TaskUID, UserUID, ActorUID}`, inside the
  assignment's transaction, from `Assign` and from the `AskedUIDs` of an opening alike — and nothing
  else: `assignSQL` returns `(xmax = 0)`, true only for a row it inserted, so re-asking somebody already
  on the task (asked, or there by acting) reports nothing. An observer's error fails the assignment.
  It is how `internal/taskassignnotify` sends the `task_assigned` notification. The aggregate rides along with every read as a `json_agg` LATERAL, so a listing
  renders who is on each task without a second request, while the **filter** is an `EXISTS` over the
  `(user_uid, joined_at)` index rather than a predicate over that aggregate;
  **the read joins the `comments` table itself** rather than enriching afterwards — the thread is where
  a task's answer arrives, not an ornament on it, so `comment_count`, `last_comment_at` and the derived
  `has_new_answer` have to be filterable and pageable in the same query; enriching after paging would
  silently return short pages. **Whose turn it is** (`store.go`, `callerParam`/`answeredSQL`/`waitingSQL`):
  the caller's uid is always `$1` of every read, the thread LATERAL adds `last_reply_at` = the newest live
  comment by somebody **other than the caller**, a second LATERAL picks the **last activity** (`la.at`,
  `la.by`: the newest of the opening, the last state change with a NULL `state_by` read as the creator,
  and the newest live comment; a tie at one instant goes to the comment), and the two flags are one SQL
  fragment each, used both as a column of `taskColumns` and as the `Answered`/`Waiting` clause of
  `conditions()` — so the badge a client draws and the list it asks for are the same predicate, evaluated
  server-side. `HasNewAnswer` = `last_reply_at > state_at` (one's own comment never lights it; a closed
  task may still carry it); `WaitingOnMe` = open ∧ caller is a participant ∧ `la.by IS DISTINCT FROM`
  caller (an unknown actor counts as somebody else); `LastActivityAt/ByUID/ByName` ride along; membership
  and participant writes deliberately do not count as activity). **The queue at a glance** (`summary.go`):
  `Summary(ctx, callerUID)` → `Summary{ByState (every state, zero included), Open, WaitingOnMe, Answered}`
  in **one** `count(*) FILTER (…)` statement over `photo_tasks` plus the same two LATERALs the listing uses
  (`threadJoin`/`activityJoin`, split out of `taskJoins` for exactly this; the participants aggregate and the
  user joins are left out) — `WaitingOnMe` is the `waitingSQL` fragment, `Answered` is `answeredSQL` over the
  open states, so the navigation badge, the chips and the lists behind them are one predicate;
  `summarySQL` is *built* from `States` (`buildSummarySQL`, guarded by a unit test) so a sixth state could
  not be forgotten. `WaitingOnMe(ctx, projectUID, callerUID)` is the same query narrowed to the one number
  `internal/whatsnew` asks for (the store satisfies `whatsnew.TaskCounter` directly, counting in the
  digest's project), `internal/phototaskapi/`
  (the HTTP API over tasks, their threads and their people; every handler passes the request's project
  (`projectOf(r)` = `auth.ProjectUIDFromContext`) to the task and comment stores, so through
  `/p/{project}/tasks/…` another project's task — its detail, edits, photos, participants (now a 404 instead
  of an empty list) and thread — is the very 404 a missing one gets, and a comment on it writes nothing; `Store` + `CommentStore` interfaces →
  unit-testable with fakes;
  `NewAPI(Config{Store,Comments,RequireAuth,RequireWrite,CommentThrottle})`+`RegisterRoutes` mounts
  `/tasks` with the guards split where the feature needs them: `GET /tasks` (filters `state` (repeatable
  and comma-separated, an unknown one → **400** rather than an empty result), `open`, `answered`,
  `waiting` (both caller-relative: `parseFilter` puts the signed-in uid into `Filter.CallerUID`), `q`,
  `photo`, `participant` (with `me` resolving to the caller, so a client need not learn its own uid),
  `limit`, `offset`; the envelope echoes `limit`/`offset` as actually applied), **`GET /tasks/summary`**
  (`handleSummary`: the store's `Summary` as the caller — a static segment chi matches ahead of `/{uid}`) and
  `GET /tasks/{uid}` (read **as the caller**, so its two flags are theirs) behind **`RequireAuth`**, `POST`/`PATCH`/`DELETE /tasks[/{uid}]` (both
  bodies carry `options`; on PATCH a `*[]string`, so absent, `[]` and a list are three different
  things, and the three option sentinels map to **400** in `writeTaskError`; the create body's
  `photo_uids` is **optional** — absent or `[]` opens a task over nothing — while `decodeMembership`
  still refuses an empty list on the two photo routes, where the list *is* the request, and its
  `participants` becomes `Opening.AskedUIDs`, so one POST both opens the task and hands it over) and
  `POST`/`DELETE /tasks/{uid}/photos` behind `RequireWrite`, and the whole thread
  (`GET`/`POST /tasks/{uid}/comments` + `PATCH`/`DELETE /tasks/{uid}/comments/{commentUID}`) behind
  **`RequireAuth`** — answering is what a viewer account is *for* here, since the person who remembers
  the year is rarely the person who edits the library; the per-user comment throttle is built from the
  same `ratelimit.Comment` configuration as the photo threads but in its own bucket, so a burst of
  answers cannot use up somebody's allowance for commenting on photographs; `resolveComment` answers
  **404** for a comment that belongs to a different task, `canEditComment`/`canDeleteComment` are the
  same predicates the photo thread uses; the answer's optional **`state`** (`answerRequest`) is turned by
  `stateMove` into a `comments.Along` running `phototask.UpdateTx`, so the comment and the move commit
  together — refused **before anything is written** for a caller without `Role.CanWrite` (403) or an
  unknown state (400), and `writeAnswerError` maps a move the task refuses (`ErrClosedNeedsResolution`)
  to 400 with the comment rolled back;
  **the people on a task** are `GET /tasks/{uid}/participants` (RequireAuth — knowing who else is on a
  question is part of deciding whether to answer it) plus `POST`/`DELETE …/participants[/{userUID}]`
  behind `RequireWrite`, both answering with the list as it now stands so a caller never follows a write
  with a read. A plain answer's `handleCreateComment` calls `joinAuthor` **after** the comment lands
  rather than with it (an answer that moves the state is joined inside `UpdateTx` instead):
  the thread belongs to `internal/comments`, which knows nothing about tasks, and threading a task store
  through it to save one statement would couple the two for no gain — so a failure there is logged and
  swallowed, because the answer *is* written and must not be reported as failed over its bookkeeping;
  **a task's photographs have no route here** — they are read
  through the catalogue with `GET /photos?task=…`, the same projection, signed media URLs and paging as
  any other listing, and a second gallery endpoint would only have drifted from the first;
  mounted by `server.WithAPI` (`buildPhotoTaskAPI` in `cmd/kukatko/phototask.go`, in `readAPIOptions`)),
  `internal/savedsearch/`
  (the DB layer for **per-user saved searches** ("smart albums") — a named, owner's private
  filter/search definition the user reopens; mirrors the per-user ownership of
  `user_favorites`; the `saved_searches` table in migration `0017_saved_searches.sql`: `uid PK` (prefix `ss`),
  `owner_uid` FK users `ON DELETE CASCADE`, `name TEXT NOT NULL`, `params JSONB NOT NULL` (the opaque
  stored state of the view/search: filters, sorting, query, mode), `created_at`/`updated_at`, an index on
  `owner_uid`; migration 0102 adds `project_id` + the index `(project_id, owner_uid)`; `Store` =
  `NewStore(pool)`: `Create(ctx,projectUID,ownerUID,name,params)`/`List(ctx,projectUID,ownerUID)`
  (newest-first by `created_at`)/`Get(ctx,projectUID,ownerUID,uid)`/`Update(ctx,projectUID,ownerUID,uid,
  name,params)` (overwrites name+params, stamps `updated_at`)/`Delete(ctx,projectUID,ownerUID,uid)`;
  `params` as `json.RawMessage` (empty → `{}`, so the NOT NULL column gets valid JSON).
  **Project scope (`store_project.go`, the photos convention):** a saved search belongs to one owner
  inside one project, and **the store enforces both** — every statement names the project and the owner
  (`ownerProjectClause`), an empty project is `ErrNoProject`, `Create` writes `project_id` explicitly (the
  column has had no default since migration 0114), and a missing uid, another owner's search and the
  owner's own search in another project are all the **same `ErrNotFound`** (a refused `Update`/`Delete`
  writes nothing); `SavedSearch` carries `ProjectUID`)), `internal/savedsearchapi/`
  (a read/curation HTTP API over saved searches: the `Store` interface (a subset of `savedsearch.Store`) →
  unit-testable with fakes; `NewAPI(Config{Store,RequireAuth})`+`RegisterRoutes` mounts
  `/saved-searches` **all behind `RequireAuth`** and **scoped to the logged-in user and the request's
  project** from the auth context (`auth.UserFromContext`, `auth.ProjectUIDFromContext`): `GET /saved-searches` (`{saved_searches:[{uid,name,params,created_at,
  updated_at}]}` of the current user, owner_uid is deliberately not shown in the view), `POST /saved-searches`
  `{name,params}` → 201 (an empty name → 400, `params` optional → `{}`), `GET /saved-searches/{uid}`
  → 200, `PATCH /saved-searches/{uid}` `{name?,params?}` → 200 (an omitted field unchanged, an empty
  name → 400), `DELETE /saved-searches/{uid}` → 204; **ownership isolation** — the shared helper
  `ownedSearch` loads the row through the owner- and project-scoped `Get`, so another user's search and the
  caller's own search in another project are the store's `ErrNotFound` → the **very 404** a missing uid gets
  (never reveals someone else's search); the body `DisallowUnknownFields` + 1 MiB limit, sentinel `ErrNotFound`→404;
  mounted by `server.WithAPI` (`buildSavedSearchAPI` in `cmd/kukatko/savedsearch.go`)),
  `internal/uploadlink/`
  (the domain of **upload links** — a short link a curator posts to a group chat, through which anybody
  uploads photos into preset albums and labels (filed by `organize.AddPhotoTx`/`AttachLabelTx` inside the
  **link's own project**, read from `upload_links.project_id`, so a target outside it fails the filing); owns the four tables of migration `0090_upload_links.sql`:
  `upload_links` (`uid` prefix `ul`, `code_hash` UNIQUE — the SHA-256 of the short code, which every
  lookup goes through — and, since `0091_upload_links_code.sql`, the nullable readable `code`: kept so a
  manager can copy the URL again, NULL for a link created before 0091 until it is restored,
  title, note, `created_by` FK users `ON DELETE SET NULL`, `expires_at`, final `revoked_at`,
  `upload_count`/`last_used_at`), `upload_link_albums`/`upload_link_labels` (targets, cascading from both
  sides) and `upload_link_photos` (provenance per photo and link: `outcome` created|duplicate,
  `uploader_name`, `uploaded_by` FK users SET NULL, `session_hash` of an anonymous browser session); all
  four are catalogue tables for the wipe. `NewCode` draws 8 symbols from a 55-character alphabet with no
  ambiguous glyphs (rejection sampling, no modulo bias), `ValidCode` refuses a malformed one before a
  lookup, `HashSecret` is the plain SHA-256 used for codes and session tokens alike (high-entropy random
  secrets, as for API tokens), `NewSessionToken`, `NormalizeUploaderName` (trim, collapse, cut to 100).
  `Link.StateAt(now)` → `active|expired|revoked` (revoked wins); `Link.Code` is the readable code (`""`
  when unknown) and is tagged `json:"-"` — the HTTP layer decides per caller whether to show it.
  `Store` = `NewStore(pool)`:
  **every management method takes `projectUID` after `ctx`** (empty = `ErrNoProject`; another project's
  link = `ErrNotFound`, identical to a missing one; audited writes default the entry's `ProjectUID`):
  `Create(ctx, projectUID, NewLink, entry)` → the link **plus its plaintext code**, stored readably too, with
  `project_id` (code collision → redraw via
  `ON CONFLICT DO NOTHING`; `NewLink.Validate`: ≥ 1 target, ≤ 20 of each, title/note caps; a missing
  album/label — or one of another project, inserted only `WHERE project_id` matches — →
  `ErrTargetNotFound`, nothing written), `Get(ctx, projectUID, uid)`, `ByCode` (**instance-wide**: the public
  routes' lookup, which learns the project from `Link.ProjectUID`, `json:"-"`), `List(ctx, projectUID,
  creatorUID)` ("" = all of the project; one query with
  the creator name and both target lists as JSON), `Extend` (`ErrRevoked` for a revoked link),
  `Revoke` (idempotent, a second revoke writes no audit), `RestoreCode(ctx, projectUID, uid, code, entry)`
  (`recode.go`: stores `code` only when its hash equals `code_hash` — constant-time compare — else
  `ErrCodeMismatch` with nothing changed; already readable = no-op without audit), `RotateCode(ctx,
  projectUID, uid, entry)` (a fresh code replaces `code_hash` and `code`, redrawn on a collision; the old URL dies) — both
  lock the row and refuse a revoked link with `ErrRevoked` (an expired one is accepted),
  `ReserveUpload(ctx, uid, maxUploads, now)` — one file's slot of the lifetime cap, a single `UPDATE`
  bumping `upload_count` only while the link is not revoked, not expired at `now` and (unless 0) under the
  cap, so concurrent requests serialise on the row; a refusal is named `ErrFull`/`ErrRevoked`/`ErrExpired`/
  `ErrNotFound` — `ReleaseUpload(ctx, uid)` (gives a slot back, floored at 0), `RecordUpload(ctx, Upload,
  entry)` — provenance row, `organize.AddPhotoTx`/`AttachLabelTx` into every target (manual label),
  `last_used_at` and the `upload_link.upload` audit entry in **one transaction** (it does not count: the
  reservation did; the link-uid-keyed upload methods need no project, the link was resolved by code) —
  `AttributeSessionTx(ctx, tx, projectUID, sessionHash, userUID)` (on the registration's transaction: hands
  the session's **created**, still unowned photos **of that project's links** to the new account, once — a
  registration into B never claims what the same browser uploaded into A) and `Provenance(ctx, photoUID)` (the earliest link upload that created the photo
  — link uid/title, typed name, when, and the attributed account `AccountUID`/`AccountName` (display
  name, fallback username; nil once deleted) — for the sidecar's `identity.upload_link` and the photo
  detail's `upload_link`); `RecordUpload`'s audit details carry the link's `link_title` (read by the
  `last_used_at` `UPDATE … RETURNING title`); every mutation audits in its own transaction)),
  `internal/uploadlinkapi/`
  (the HTTP half: `NewAPI(Config{Store, Ingest, Projects, Members?, Sidecar, Registration?, RequireCurator, OptionalAuth,
  CurrentUser?, CurrentRole?, CurrentProject?, IPLimit, LinkLimit, MaxFileSize, Quota?, MaxUploadsPerLink,
  DefaultDays, MaxDays, SecureCookies, Now?})` mounts
  `/upload-links` **per project** (`RegisterManagementRoutes`, under `/p/{project}`: every route works in
  the request's project, another project's link is the 404 a missing one gets; curator management incl.
  `restore-code` (422 on `ErrCodeMismatch`) and `new-code`; somebody else's link is 404 unless the caller
  is an admin of the project, and the listing's `code`/`path` are set only by
  `view(user, link)` for a link `canManage` allows) and the public, **instance-level**
  `/u/{code}` pair behind the per-IP limiter (`RegisterPublicRoutes`; the page's `GET` also reports
  `project`, the **name** of the link's project read through `Projects` (never its uid or slug),
  `registration_enabled`, the link's own project's switch read through `Registration`, and `storage_full`,
  `ingest.QuotaRefusal` over `Quota` (an `ingest.QuotaChecker`, the shared `*ingest.Service`; nil = never
  full), and `credits_account` — `creditedAccount`: a signed-in account is credited (`uploaderFor`) only
  when `Members` (`MemberChecker`, `*project.Store.IsApprovedMember`; nil credits nobody) says it is an
  approved member of the link's project, anybody else signed in uploads like an anonymous visitor and gets
  the session cookie — the upload asks the same before reading the body and answers a full project 413
  `{"error":"quota_exceeded"}` with no sizes; `Registration` is a
  `RegistrationSettings`, `*settings.Store`; nil or a failed read = closed): `liveLink` resolves the link by code,
  then its project through `Projects` (`*project.Store`; a missing or deactivated project = the unknown
  link's 404, and so is an expired or revoked link — one `linkInvalidMessage` body for all four, so no code
  tells whether it ever worked) into a `liveTarget{Link, project}`, and every file is handed to `IngestFile` with
  **the link's project** (its audit entry names it too); the upload walks the multipart stream once (the `name`
  field must precede the files), refuses an extension `imgconvert.IsSupportedFormat` does not know (415,
  `Code: "unsupported_type"`; content that is not media is the pipeline's own 415 `not_media`),
  caps a request at `maxFilesPerRequest` = 50 file parts (refused ones count; the 51st is a 413 and ends
  the walk), hands the link's cap to the pipeline as `Request.MaxFileSize` with the file's declared size
  (`ingest.PartSize`: its `Content-Length`, else a preceding `size` field) — the pipeline combines it with
  the project's and the instance's caps (413 `file_too_large`) — strips `limit_bytes`/`used_bytes` off a
  per-file `quota_exceeded` (`withoutUsage`: the link's holder learns no sizes), takes the
  file's slot with `Store.ReserveUpload` **before** the pipeline reads it (`ErrFull` → 429, a link revoked/
  expired/deleted since the request began → per-file 410 `"upload link is not valid"`, SEC-019), runs
  `ingest.Service.IngestFile`, then `Store.RecordUpload` for every photo — new or duplicate — and
  reschedules its sidecar; a file that errs or is not filed gets `ReleaseUpload` (on a context that
  outlives the request); an anonymous uploader is identified by the
  `kukatko_upload_session` cookie (`SessionCookieName`, path `/api/v1`) whose hash marks their photos —
  minted by the page's `GET /u/{code}`, not the first upload, since the page sends three files at once and
  each would otherwise start a session of its own (only the last cookie would survive in the browser). `RegistrationGate`
  implements `auth.UploadLinkGate`: a live link admits `POST /auth/register` without the secret, its grant
  names the link's project (`LinkGrant.ProjectUID` — the account joins that project only, an unapproved
  viewer, under that project's registration switch and approvers) and its
  `Claim` runs `uploadlink.AttributeSessionTx` for that project on the account's transaction, `Committed` reschedules the
  claimed photos' sidecars. Built by `buildUploadLinkAPI` inside `buildIngest` (it shares the signed-in
  upload's `ingest.Service`) and `uploadLinkGate` in `cmd/kukatko/auth.go`)),
  `internal/searchhistory/`
  (the DB layer for **each user's recent searches** — the short ordered list of what somebody actually
  searched for, kept server-side so a query composed on a laptop is offered on the phone; the
  `search_history` table in migration `0056_search_history.sql`: `user_uid` FK users `ON DELETE CASCADE`,
  `query TEXT NOT NULL CHECK (char_length BETWEEN 1 AND 500)`, `searched_at TIMESTAMPTZ DEFAULT now()`,
  **PK `(user_uid, query)`** (which is what makes recording an idempotent upsert) + the index
  `(user_uid, searched_at DESC)`; migration 0102 adds `project_id` (bootstrap DEFAULT, never relied on) and
  widens the PK to `(user_uid, project_id, query)`, so the history is **per (user, project)**; `MaxEntries = 20`, `MaxQueryLength = 500`, `Normalize(query)` (trims,
  caps at 500 **characters** and trims what the cut left, `""` for a whitespace-only query — it deliberately
  does *not* fold case or collapse inner whitespace: the string is handed back to the search box verbatim,
  and collapsing spaces would change `title:"a  b"`), `Entry{Query,SearchedAt}`;
  `Store` = `NewStore(pool)`, **every method project-scoped** (`store_project.go`: `projectUID` after `ctx`,
  never defaulted — empty → `ErrNoProject`; every statement names user and project via `userProjectClause`):
  `Record(ctx,projectUID,userUID,query)` (normalize → `ErrEmptyQuery` for a blank one →
  `INSERT … ON CONFLICT (user_uid, project_id, query) DO UPDATE SET searched_at = now()` **then** the prune, both in
  **one transaction** so no reader sees a history past the cap; the prune keeps the newest `MaxEntries`
  identified **by query** rather than by timestamp, since the PK is unique per (user, project) while two rows
  written in one transaction share `now()`; the prune's DELETE and subquery both name the project, so the
  ring of 20 is **per (user, project)** and trimming one project never deletes another's history),
  `List(ctx,projectUID,userUID)` (newest first, `LIMIT MaxEntries`, tie-broken by query — the same total
  order the prune uses), `Clear(ctx,projectUID,userUID)` (idempotent, that project only);
  **deduplication is on the exact trimmed text** — "Praha" and "praha" are two entries, because folding
  them would mean choosing which spelling the user gets handed back; there is **no retention job**, the
  per-write prune bounds the table by the number of memberships), `internal/searchhistoryapi/`
  (the HTTP API over it: the `Store` interface (a subset of `searchhistory.Store`) → unit-testable with a fake;
  `NewAPI(Config{Store,RequireAuth})`+`RegisterRoutes` mounts `/search-history` **all behind `RequireAuth`**
  and **scoped to the logged-in user and the request's project** (`auth.UserFromContext`,
  `auth.ProjectUIDFromContext`) — there is no path parameter and no owner in any body, so there is nothing to
  tamper with: `GET` → `{searches:[{query,searched_at}]}` (an empty history is
  `[]`, never `null`), `POST {query}` → **204** (no body: the client already knows what it searched for, and the
  refreshed list is only wanted when the dropdown next opens, which is a GET), `DELETE` → 204 (idempotent);
  a blank query → 400 both in `decodeRecord` and via the store's `ErrEmptyQuery`, the body
  `DisallowUnknownFields` + an 8 KiB limit; **not audited** — a private per-user convenience list is not a
  library mutation; mounted by `server.WithAPI` (`buildSearchHistoryAPI` in `cmd/kukatko/searchhistory.go`)),
  `internal/announcement/`
  (the DB layer for **a project's announcement** — a short message the system administrator publishes and every
  member of the project sees as a banner at the top; the single-row `announcements` table in migration
  `0039_announcement.sql`: `id BOOLEAN PK DEFAULT true CHECK (id)` (a single-row invariant → publish is
  an **upsert**), `message TEXT NOT NULL`, `level TEXT NOT NULL DEFAULT 'info' CHECK (info|warning)`,
  `author_uid VARCHAR(32)` FK users `ON DELETE SET NULL` (losing the author must not take down a live announcement),
  `updated_at TIMESTAMPTZ`; **since migration 0102 one row per project**: the `id` key is gone, PK
  `project_id` (FK `projects`, the old row became the bootstrap project's) and every method takes `projectUID`
  right after `ctx` (`internal/announcementapi` passes the request's `auth.ProjectUIDFromContext`);
  `Store` = `NewStore(pool)`: `Get(ctx,projectUID)` (→ the sentinel `ErrNotFound` when nothing),
  `Set(ctx,projectUID,message,level,authorUID,entry)` (upsert + validation: an empty/whitespace message → `ErrEmptyMessage`,
  an unknown level → `ErrInvalidLevel`, an empty level → `info`), `Clear(ctx,projectUID,entry)` (delete); **both publish and clear
  write the audit** (`announcement.set`/`announcement.clear`, message/level into details) in the **same transaction**
  as the change (mirrors `internal/organize`)), `internal/announcementapi/`
  (a dual-guard HTTP API over the announcement of the request's project — `auth.ProjectUIDFromContext`, mounted
  **only under `/p/{project}`**, so a banner published in one project is never
  read in another: the `Store` interface (a subset of `announcement.Store`) → unit-testable
  with a fake; `NewAPI(Config{Store,RequireAuth,RequireMaintainer})`+`RegisterRoutes` mounts `/announcement`:
  `GET /` behind `RequireAuth` (anyone logged in reads; when nothing is published → **200 `{"message":""}`** instead of 404,
  friendlier for the polling banner client), `PUT /` and `DELETE /` behind `RequireMaintainer` (publish/clear,
  `author_uid` = the actor from the auth context); the body `{message,level}` with `DisallowUnknownFields` + 16 KiB limit,
  `ErrEmptyMessage`/`ErrInvalidLevel` → 400, response `{message, level?, author_uid?, updated_at?}`
  (`updated_at` RFC3339, otherwise omitted); mounted by `projectRoutes` (`buildAnnouncementAPI` in
  `cmd/kukatko/announcement.go`)), `internal/settings/`
  (the DB layer for **the settings a project's admin edits at runtime** — whether self-service
  registration into the project is open, the shared secret registration asks for, and the Markdown greeting shown on a first
  sign-in; none of the three is a deployment concern, so they live in the database and change without a
  redeploy. The single-row `instance_settings` table in migration `0062_instance_settings.sql`:
  `id BOOLEAN PK DEFAULT true CHECK (id)` (a single-row invariant → a write is an **upsert**),
  `registration_enabled BOOLEAN NOT NULL DEFAULT false`, `registration_secret TEXT NOT NULL DEFAULT ''`,
  `welcome_markdown TEXT NOT NULL DEFAULT ''`, `updated_at TIMESTAMPTZ`, `updated_by_uid VARCHAR(32)` FK users
  `ON DELETE SET NULL` (losing that account must not take the instance's own configuration with it); **the
  migration seeds the row**, so every read finds one — the anonymous sign-in screen asks whether registration
  is open before anybody has ever opened the admin area. **Since migration 0102 one row per project**: the `id`
  key is gone, PK `project_id` (FK `projects`, the seeded row became the bootstrap project's; another project
  without a row reads the defaults) and every method takes `projectUID` right after `ctx` (`internal/settingsapi`
  passes the request's project, `internal/auth`'s registration the project the registration names, the
  upload-link page the link's). **Migration 0118** adds `footer_text TEXT NOT NULL DEFAULT ''` — the
  plain-text line every page of the project ends with — and backfills the bootstrap project with
  "Provozuje SDH Veselice" (only an empty value, so a re-run never overwrites an admin's line).
  `Store` = `NewStore(pool)`: `Get(ctx,projectUID)` (→ the
  zero-value defaults when the row is missing rather than an error, so a hand-deleted row cannot block the
  sign-in screen), `Set(ctx,projectUID,Update{RegistrationEnabled,RegistrationSecret,WelcomeMarkdown,FooterText},actorUID,entry)`
  (upsert of **the three registration values at once** — the flag and the secret guard each other, so they
  are never saved apart; enabling registration with a blank/whitespace-only secret → `ErrSecretRequired` and
  nothing is written; the secret is stored trimmed, the Markdown verbatim; `FooterText *string` is optional —
  nil keeps the stored line (`COALESCE($6, footer_text)`), a value is stored trimmed and refused with
  `ErrFooterMultiline` (a line break) or `ErrFooterTooLong` (over `MaxFooterRunes` = 200 characters)); the update **writes the audit**
  (`settings.update`) in the **same transaction** as the change (mirrors `internal/announcement`).
  **The secret is stored readable, not hashed** — an administrator has to read it back to tell people what
  it is — which is what makes the full record admin-only above. The table is classified **preserved** in
  `internal/reset/tables.go`: it describes the instance, not the library), `internal/settingsapi/`
  (the HTTP API over the settings, with **separate audiences and separate wire types** rather than one
  record filtered per role — a field added to `settings.Settings` then has to be given an audience on purpose
  instead of leaking into whichever payload happens to embed it. The `Store` interface (a subset of
  `settings.Store`) and `UsageSource` (`Usage(ctx,uid)`, satisfied by `*project.Store`) → unit-testable with
  fakes; `NewAPI(Config{Store,Usage,Passkeys,Mail,IPLimit,RequireActiveProject,RequireAuth,RequireAdmin})`.
  `RegisterRoutes` mounts the **project's** `/settings`, only under `/p/{project}`, every route on the project the guard admitted (`auth.ProjectUIDFromContext`):
  `GET /settings/public` behind the per-IP `IPLimit` and `RequireActiveProject` (anonymous,
  `auth.PublicProjectFromContext`) → `{project_name,registration_enabled}` only — what the project's
  registration page needs before there is an account — and **only while registration is open**: a closed
  project is the same 404 `project not found` an unknown slug gets from the guard, so the route is no slug
  oracle (SEC-025); `GET /settings/welcome` behind `RequireAuth` → `{welcome_markdown}` only (the project's;
  `users.welcome_seen_at` stays per account, so a second project does not greet again); `GET /settings/footer`
  behind `RequireAuth` → `{footer_text}` only (the project's footer line, `footerResponse`); `GET /settings` and
  `PUT /settings` behind `RequireAdmin` → the full record, secret and footer included; the PUT body
  `{registration_enabled,registration_secret,welcome_markdown,footer_text?}` with `DisallowUnknownFields` + a
  64 KiB limit replaces the three registration values (and the footer line when sent; left out, it stays),
  `ErrSecretRequired`/`ErrFooterTooLong`/`ErrFooterMultiline` → 400, `updated_at` RFC3339; the audit details carry the flag and
  *whether* a secret and a greeting are set, **never the secret itself** — the trail is a permanent record
  that outlives the secret it would copy — plus the footer line when the update writes it; `GET /settings/usage` behind `RequireAdmin` →
  `{photos,originals_bytes,max_file_bytes,max_total_bytes}`, the usage beside the admitted project's two
  limits (`null` = none), read-only. `RegisterInstanceRoutes` mounts the **instance's** unguarded
  `GET /settings/public` → `{passkeys_enabled,mail_enabled}` only — the two facts the sign-in screen needs
  before anybody is signed in. Neither is a stored setting: `passkeys_enabled` is `Config.Passkeys`
  (`auth.API.PasskeysEnabled()`) because `GET /capabilities` carries the same flag for the rest of the app
  but is behind `RequireAuth`, so it cannot answer an anonymous sign-in screen; `mail_enabled` is
  `Config.Mail` (`cfg.Mail.Enabled`, the switch that decides whether `buildMailServiceOrNil` wires a real
  SMTP sender), so the screens around registration promise an e-mail only where one is actually coming.
  Mounted by `projectRoutes` and `server.WithAPI` (`buildSettingsAPI` in `cmd/kukatko/settings.go`)), `internal/whatsnew/`
  (**the returning-reader digest** behind the "what's new since your last visit" panel on the library home —
  a shared family library otherwise makes somebody else's evening of uploading invisible to the next person
  who opens the app; **read-only over the catalogue**, its single write is the visit bookkeeping on the
  caller's own **membership** row. **Per project** (multi-tenancy): a visit is a visit to one project, so
  the two timestamps and the linked person are read and written on `project_members` (the matching `users`
  columns of migration 0053 are a transition copy nobody writes any more), and every count and list reads
  only that project's rows (`photos`/`albums`/`subjects.project_id`; a comment through the photo it is on).
  **What a visit is** (migration `0053_user_visits.sql` put two nullable
  `TIMESTAMPTZ` columns on `users`; 0101 copied them per project onto `project_members`): `last_seen_at` is a heartbeat stamped to now on **every** summary read,
  `visit_reference_at` is the digest's "since" and moves **only** when a new visit begins — a gap of at least
  `VisitGap` (**6 h**) between two reads — taking the previous `last_seen_at` on that transition. Within one
  visit the reference does not move however often the page reloads (the "a refresh must not reset the panel"
  requirement); both NULL on an account that never read a summary, and a NULL reference = **no digest**
  (a new account does not want its whole library announced). Neither column touches `updated_at` — being here
  is not a profile edit. Since only this read stamps the heartbeat, "inactivity" means precisely
  "the library home was not opened". `Store` = `NewStore(pool)` (+ `WithGap(d)`, a copy with a shorter
  threshold **for tests** — production never calls it): `Summary(ctx, projectUID, userUID, now)` →
  `Summary{HasNews, Since, Photos, MinePhotos, Comments, Albums[]{UID,Title}, AlbumCount, People[]{UID,Name},
  PersonCount}`, `ErrNoProject` for an empty project, the sentinel
  `ErrUserNotFound` when there is no membership to stamp (the account vanished mid-request, or a maintainer
  entered a project it is not a member of — no bookkeeping there, no digest). It rotates the visit in **one** atomic
  `UPDATE … RETURNING` (two tabs loading at the same instant cannot both rotate: the second waits on the row
  lock and then sees a `last_seen_at` that is already now), then counts. **What counts:** photos under the
  library grid's own base filter (`archived_at IS NULL AND (stack_uid IS NULL OR stack_primary) AND NOT
  hidden_from_library` — so the number printed equals the number of tiles the link opens), live comments
  (`deleted_at IS NULL`, **photo threads only** — `photo_uid IS NOT NULL`, since migration 0080 put both
  subjects in one table and a task's thread is answered work rather than library news), **hand-curated**
  albums only (`type = 'album'`; an import mints folder/moment/month groupings by the hundred), **named**
  subjects only (`name <> ''`) and **waiting questions** (`photo_tasks` opened since the reference point
  whose state is still `question`) — the one count that asks something *of* the reader rather than reporting
  what happened, and the reason somebody who never opens the task list still meets the question they were
  meant to answer. It takes part in `counts.empty()` like the rest, so it can raise the panel on its own. **Since 2026-09-20 it is
  not a "since" count at all:** `NewStore(pool, tasks TaskCounter)` takes the task store (interface
  `WaitingOnMe(ctx, projectUID, callerUID)`, satisfied by `*phototask.Store`; nil = the line is always zero) and `countSince` reads the caller's
  **`waiting_on_me`** from it — the queue's own "whose move is it" predicate (open, the reader on it,
  somebody else acted last) — because a question asked a fortnight ago is still theirs to answer and one
  opened yesterday that they already answered is not; the old `photo_tasks` subquery (state `question`,
  opened since the visit, own excluded) is gone from `countsSQL`. **Whose news:** every count — and the
  album list with it — subtracts the **reader's own** work (`photos.uploaded_by`, `comments.author_uid`,
  `albums.created_by`, all `IS DISTINCT FROM $2`): the digest reports what *others* did while the reader was
  away, and "1 new comment" for the comment they just wrote is an echo, not news. `IS DISTINCT FROM` rather
  than `<>` because all three columns are `ON DELETE SET NULL` — work whose actor has since been deleted
  belongs to nobody and stays news to everybody. **Subjects are the deliberate exception:** the schema records
  no creator for a name put on a face and deliberately gets none, so a newly named person is counted for
  everyone, whoever named them included. `listAlbums` repeats the exclusion verbatim, so the "Podrobnosti"
  list can never name an album the count left out; when the exclusion empties every count the digest is the
  zero-value `Summary` and **no panel appears at all**. **`MinePhotos`** is the one line
  about the reader rather than about the library: the same photo predicate narrowed by a non-invalid marker
  naming the member's linked person in the project (`project_members.subject_uid`), read out of the very
  `UPDATE … RETURNING` that stamps the visit (the
  row is already being written, so the link costs no second round trip) and skipped entirely for an
  unlinked account or a visit with no new photos at all. Being a strict subset of `Photos` it can never be
  the only news, so `counts.empty()` — and therefore `HasNews` — ignores it. Albums and people are capped at
  `MaxItems` (**6**) links while the counts report the true totals. A `HasNews false` (zero-value) `Summary`
  covers both "first visit" and "nothing happened" so the client branches on one flag. Every count is an
  indexed range over a creation timestamp (`idx_albums_created_at`, `idx_subjects_created_at`,
  `idx_comments_created_at` from 0053 (renamed with its table by 0080); `idx_photos_live_created_at` from 0015), with the actor exclusion
  a plan `Filter` on top of that `Index Cond` rather than a new access path — pinned by
  `TestCountsStayIndexBacked`, which seeds 4 000 old rows per table and reads the plan back)),
  `internal/whatsnewapi/`
  (a one-route HTTP API over it: the `Summarizer` interface (a subset of `whatsnew.Store`) → unit-testable
  with a fake; `NewAPI(Config{Store,RequireAuth,Now})` (`Now` defaults to `time.Now`, injectable so tests move
  a visit forward without waiting) + `RegisterRoutes` mounts `GET /whats-new` (project-scoped, the handler
  passes `auth.ProjectUIDFromContext`) behind **`RequireAuth`** —
  every role including a **viewer**, since learning what the family added is not a curation power.
  **A GET that writes** on purpose: reading the digest is what stamps the visit, and a separate POST the
  client must remember to send would leave the reference wrong exactly when the tab was closed. Returns
  **200** with the `Summary` in every non-failure case (`{"has_news":false}` for a first visit, an empty visit
  **and** a `whatsnew.ErrUserNotFound`) — never a 404 or a 204, so the client parses one shape; any other
  store error → 500 (logged). Mounted by `server.WithAPI` (`buildWhatsNewAPI` in `cmd/kukatko/whatsnew.go`)),
  `internal/globalsearchapi/`
  (a grouped **global search** HTTP API across entities — the basis of the navbar quick-results and the cross-entity section
  of the search page: the small interfaces `Organizer` (`SearchAlbums`/`SearchLabels` + `GetAlbumByUID`/
  `GetLabelByUID` + `AlbumCovers`/`LabelCovers`, satisfied by `organize.Store`, each taking the request's
  project — another project's album or label is neither found nor resolved from a pasted uid), `PeopleSearcher`
  (`SearchSubjects` + `GetSubjectByUID`/`GetMarkerByUID` + `SubjectCovers`, satisfied by
  `people.Store`) and `PhotoSearcher` (`Search` + `GetByUIDInProject`/
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
  result set, and only a store failure is a 500; **the query language itself is published here too**
  (`schema.go`): `GET /search/schema` → `{keys:[{key,kind,values?}]}` read straight off `query.Keys` — the
  canonical keys sorted, each with its value kind's wire name and, for a closed vocabulary only, the words it
  accepts (an enum's, or yes/no for a bool and for a count's yes/no form); aliases are deliberately absent and
  so is any prose (what a key *means* is translated, so it lives in the frontend's i18n under
  `searchCommand.queryKeys.<key>` — `schema_test.go` fails the moment the two lists disagree, in either
  language, and a second test holds the frontend's own `FILTER_KEYS` literal to the parser's keys **plus** its
  aliases). The response is compiled in, touches no store and is constant for the binary's life, so the command
  palette fetches it once and completes filter keys and values from it rather than from a list of its own;
  mounted by `server.WithAPI` (`buildGlobalSearchAPI` in
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
  Details,IP,UserAgent,ProjectUID}` (an empty UID/IP/UA/project → SQL NULL, nil details → `{}`; `ProjectUID`
  fills the nullable, FK-less `audit_log.project_id` of migration 0102 — empty for an instance-level action); **the handler convention**
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
  `ActionUser{Create,Update,Disable,Password}`/`ActionAuditPurge`; the labels are a **closed set** and
  `KnownAction(action)` is its membership test — a reader filtering the trail (`internal/auditapi`) refuses an
  unrecognised value instead of answering with the empty listing it would match, and
  `TestKnownActionsCoverEveryConstant` parses `audit.go` itself so a new constant cannot be added without
  landing in the set; `Store` = `NewStore(pool)` with `Record(ctx,Entry)`
  (its own connection) and **filtered reads** `List(ctx,Filter)`/`Count(ctx,Filter)` (`Filter{ProjectUID,
  InstanceOnly,ActorUID,TargetType,TargetUID,Action,Since,Until,Limit,Offset}` — `InstanceOnly` = the entries
  filed under no project, `audit_log.project_id IS NULL` —, newest-first, limit cap 500/default 100;
  `person.go`: `ProjectPerson(ctx, projectUID, ref)` resolves a uid or username among the project's people —
  its members and the accounts that appear as actors in its trail — else `ErrNoProjectPerson`, the project
  admin's `?user=` filter; `ActorNames(ctx, uids)` → uid → display name (or username) of the accounts that
  still exist, the `actors` map every listing page carries for the actors of its own entries;
  `ProjectActorNames(ctx, projectUID, uids)` is the same for a project's listing, returning the former
  members of that project (the `project.FormerMemberSQL` rule, written out) apart and nameless;
  `List` joins `projects` so a `Record` carries `ProjectUID` and `Project` = the slug, both nil for an
  instance entry) for the admin listing; **projects**: `Entry.ProjectUID` files an entry under a project
  (`audit_log.project_id`); `ProjectScoped(action)` is every known action but the hand-kept
  `instanceActions` (accounts and credentials, notification preferences, a project's
  create/update/purge/storage prefix/rekey, `audit.purge`, `library.reset`, and
  `project.member.add_resolve` — the outcome of an exact-match add, kept from the project's own trail on
  purpose, see `internal/project` `AddByIdentifier`); `FromRequest` reads the
  request's project (`obs.ProjectUID`, stamped by `auth` when it admits the caller) into
  `Meta.ProjectUID`, and `Meta.Entry` files only a project-scoped action under it — an instance action is
  never attributed to whatever project its request resolved; `ProjectScopedActions()` lists them
  (byte-ordered, fresh slice) — migration 0120 carries that list literally to file every pre-tenancy entry
  of those actions (no project, older than the bootstrap project's row) under `v`, so its trail,
  leaderboard, streaks and person filter keep their history while instance actions stay instance-only;
  `internal/database`'s `TestAuditProjectBackfill_listIsProjectScoped` keeps the migration's and its down
  script's list equal to it (a project-scoped action added later goes into that test's
  `actionsAddedAfter0120`, never into the applied migration);
  **`internal/audit/audittest`** (test helper): `Guard(t, db)` marks the end of the trail and, at
  cleanup, fails the test for every entry written since that is `ProjectScoped` but names no project
  (`Unscoped(ctx, pool, mark)` is the query); the API packages' integration tests call it right after
  `dbtest.TruncateAll` (not `auditapi`, whose seeded rows are fixtures) — it found `settings`,
  `announcement`, `family` and `uploadlink` stores that did not stamp their project, which now default an
  entry's project to the one they write into; **retention purge** `PurgeOlderThan(ctx, cutoff) (int, error)` = one
  `DELETE FROM audit_log WHERE created_at < $1` over `idx_audit_log_created_at`, returns the number deleted
  (maintainer-only via `internal/maintenanceapi`, action `audit.purge`, it audits itself — the fresh
  purge record survives the purge). **Wired-in in-tx mutations**: bulk (`internal/bulk`) + photo PATCH/archive/unarchive
  via the audited variants `photos.Store.{UpdateMetadata,Archive,Unarchive}Audited`, **permanent purge**
  `photos.Store.DeleteAudited` (`internal/trash` → `photo.purge`, a system actor for the scheduled retention)
  and **user management** `auth.Store.{CreateUser,UpdateUserProfile,SetUserDisabled,SetPasswordHash}Audited`
  (`user.*`) — every mutation + audit in one tx via the shared `rowQuerier`/`mutateAudited` (photos) and
  `inAuditedTx` (auth); further domains (albums/labels/people) follow the same convention), `internal/auditapi/`
  (HTTP API over the audit trail: `NewAPI(Config{Store,ResolveUser,ResolveProjectUser,ResolveProject,
  RequireMaintainer,RequireAdmin,RequireAuth,RequireSignedIn})`; `RegisterRoutes` (instance, mounted once) mounts `GET /audit`
  behind `RequireMaintainer` (with `project=<slug>` → `Filter.ProjectUID` through `ResolveProject`, unknown →
  400, and `instance_only=true` → `Filter.InstanceOnly` (`parseInstanceOnly`: `true`/`false` only, next to a
  `project` → 400)) and `GET /audit/mine` behind `RequireSignedIn`; `RegisterProjectRoutes` (the `projectAPI.audit` group,
  under `/p/{project}` only) mounts the same two behind `RequireAdmin`/`RequireAuth`,
  wrapped in `inProject`, which refuses a `project` (on `/audit/mine` `ownOnly` has already dropped it) and an
  `instance_only` parameter by name and pins `Filter.ProjectUID` to
  `auth.ProjectUIDFromContext`; `parseFilter` from the query `user`/`entity_type`/`entity_uid`/
  `action`/`via`/`decision`/`since`/`until` (RFC3339)/`limit`/`offset` → `audit.Filter` (an invalid
  time/number/`via`/`decision` → 400), returns `{entries,actors,total,limit,offset,next_offset}` newest-first —
  `actors` (`respond` → `actorNames` → `audit.Store.ActorNames` over `actorUIDs(entries)`, on a project route
  `ProjectActorNames`) names every account that acted in the page, so a project admin reads a waiting member
  by name without the map ever naming anybody who did not act in an entry the reader may already see; a
  project's former member is listed in `former_actors` instead, nameless;
  **every filter is checked and the parameter set is closed**, because an ignored typo answers 200 + an empty
  list, which reads as "this person did nothing": a key outside `recognisedParams` → 400 naming it
  (`rejectUnknownParams`, sorted so the message is deterministic), an `action` outside `audit.KnownAction` → 400
  naming it, and `user` → `resolveActor` through the **`ResolveUser` function in `Config`** — a narrow
  `func(ctx, value) (uid, error)` rather than the whole user service, wired in `cmd/kukatko/audit.go` to
  `auth.Store.GetUserByUIDOrUsername`, so a **UID or a username** both work and a value naming neither → 400,
  while an existing account with no rows keeps answering 200 + an empty list. **That is the instance
  listing.** Inside a project (`parseRequest` sees `inProject`'s scope) `user` resolves through
  **`projectPeople(projectUID)`** instead, i.e. the **`ResolveProjectUser`** function in `Config`
  (`func(ctx, projectUID, value)`, wired to `audit.Store.ProjectPerson`: the account by uid or username among
  the project's current members **and** every account that acted in it): `ErrUnknownUser` there — a foreign
  account and a non-existent one alike — becomes the actor filter `noActor` (`"-"`, no uid's shape), so the
  request runs the very same queries as for a member who did nothing and answers the same empty page byte
  for byte; the instance 400 was an instance-wide username oracle for every project admin. An empty page
  is `"entries": []` (`buildResponse`), never `null`. A failure of the lookup itself is
  wrapped in `errLookup` (the project lookup's too) and answered **500** by `writeFilterError`, the one filter error that is not the
  caller's fault. `entity_uid`/`entity_type` are **deliberately left unchecked**: the trail outlives what it
  describes (an entry about a purged photo is the point of keeping one) and the entity type is a free string;
  **`via=review`** → `Filter.ReviewOnly` (the literal `details ->> 'via' = 'review'`, matches the partial
  index 0037), **`decision=yes|no`** → `Filter.Actions` ("Ano" = `face.assign`+`label.attach` / "Ne" =
  `face.reject`+`label.reject`) — the basis of the admin per-user review-decision overview;
  **`handleListMine`** (the own-activity listing behind `RequireAuth`) parses the very same filters, then
  **overwrites `Filter.ActorUID` with the session user's UID unconditionally** (`auth.UserFromContext`) — so on
  every page, under every filter, the query can only reach the caller's own rows, and the system's actor-less
  ones (empty `actor_uid`) match nothing. Both `/audit/mine` mounts sit behind **`ownOnly`**, which drops the
  `user` and `project` parameters before anything resolves them, for every caller: validating them was a
  username/slug oracle (400 for a missing one vs 403/200 for an existing one — SEC-025), so an unknown, foreign
  or valid value now gets the bare request's 200, byte for byte; a maintainer filters on `/audit`. No principal
  on the context → 401 (fail closed). It is a **separate route rather than a looser guard
  on `/audit`** so the impossibility of reading foreign rows is a property of the route's shape, not of a branch
  a later edit could weaken; `respond` (list+count+`buildResponse`) is shared by both handlers. The narrowed
  records are returned whole, `ip`/`user_agent` included — the caller's own request metadata; read-only — writes go through
  mutation transactions elsewhere; always mounted by the last `server.WithAPI` (`buildAuditAPI` in
  `cmd/kukatko/audit.go`)), `internal/bulk/`
  (bulk metadata editing: `Service` = `NewService(pool, maxBatch)` with `Apply(ctx, projectUID, actorUID,
  photoUIDs, ops Operations) (Result, error)` — **the whole batch in a single transaction** with an audit
  record under the project; **project scope:** the batch must consist of the project's photos —
  `requireBatchInProject` proves it in **one query** (`count(*) … WHERE uid = ANY($1) AND project_id = $2`
  over the distinct uids equals their number), otherwise the **whole batch** is refused with
  `ErrPhotoNotFound` before anything is written (no row, no audit entry), and the error names no uid — a
  foreign photo and a missing one are one answer, so the response never tells which uid was foreign; the
  albums/labels an add operation names must be the project's (`requireAllExist` filters `project_id`), and an
  empty `projectUID` is `ErrNoProject`; `Operations` = the optional fields `AddAlbums`/`RemoveAlbums`/`AddLabels`/`RemoveLabels`,
  `Title`/`Description *string` (nil=unchanged, ""=clear), **`TakenAt *TakenAt{At,Precision}`**,
  `Location *Location{Lat,Lng,OnlyMissing}`+`ClearLocation`,
  `Archive`/`Hide`/`Favorite *bool` (`Hide` = `photos.hidden_from_library`, the operation the
  hide-from-library feature actually needs — the real use is fifty document scans at once),
  **`Rating *int` (0–5) + `Flag *string` (none/pick/reject/eye)**;
  `Apply` validates the batch (ErrNoPhotos/ErrNoOperations/
  ErrBatchTooLarge), proves the batch (above), checks that the albums/labels of the add operations exist in
  the project (ErrAlbumNotFound/ErrLabelNotFound), then per photo: a duplicate uid → `skipped`, a photo
  that vanished since the proof → `error`, otherwise it applies and `updated`; its own idempotent SQL (its own tx for atomicity, it does not use
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
  `ClearTakenAt` is the opposite statement — **the date is unknown** — and the bulk twin of a `PATCH` with
  `taken_at: null`: `appendClearTakenAt` wipes `taken_at`, stamps `taken_at_source='unknown'`, resets
  `taken_at_precision` to `day` and takes the preserve-the-outgoing-date clause from
  `photos.TakenAtBeforeUnknownAssignment("NULL")` rather than spelling it out, so the two clear paths cannot
  drift apart; `taken_at_estimated`/`taken_at_note` are deliberately untouched;
  a set-location operation writes `lat`/`lng` + `location_source='manual'` (a picked location is never an
  estimate) and is narrowed **per photo** by `forPhoto`: with `Location.OnlyMissing` a photo that already
  has both components keeps them and, if nothing else was asked of it, comes back `skipped` ("location
  already set") — the batch answer to a box of scans with a few geotagged strays in it. The same per-photo
  read (`loadCoordinate`, which doubles as the existence check) also decides `Result.LocationChanged`
  (`json:"-"`): the photos the batch really **moved**, compared against the stored row rather than the
  request, so a re-sent coordinate schedules no reverse geocode and spends no mapy.com credit;
  `Summary()` (audit details, recording `only_missing` — the same coordinate with and without it is a
  different edit) + `IsEmpty()`; **`BeyondCuration()`** — the pure, I/O-free predicate behind the curator's
  field rule: does the batch ask for anything beyond album/label membership and the per-user
  `Favorite`/`Rating`/`Flag` (which a viewer already sets one photo at a time)? It is an **allow-list**: it
  blanks the fields a curator may touch and asks `IsEmpty()` of the rest, so a field added to `Operations`
  later is an editor's until somebody decides otherwise; `LocationSummary(ctx, projectUID, photoUIDs) (LocationSummary{Total,WithLocation},
  error)` is the read-only preview behind the dialog: how many of the selection exist in the project (a
  repeated uid counts once, a foreign one not at all) and how many already carry a full coordinate, validated against the same batch limits as
  `Apply`; `MembershipSummary(ctx, projectUID, photoUIDs) (MembershipSummary{Total,Filed,Albums,Labels}, error)` is the
  read-only "where is this batch already" behind the upload page's done stage: how many of the project's
  photos exist, how many are in an album or under a label, and the project's albums/labels (`AlbumRef`/`LabelRef` with a per-selection `PhotoCount`),
  same batch limits; the lists are skipped — and returned empty, never nil — when nothing is filed),
  `internal/bulkapi/`
  (HTTP over `bulk.Service`: the `Service` interface (Apply + LocationSummary + MembershipSummary, each
  handed `auth.ProjectUIDFromContext`) — fakeable; `NewAPI(Config{Service,
  Sidecar, Places, RequireWrite, RequireCurator, RateLimit})`+`RegisterRoutes` mounts `POST /photos/bulk` behind
  `RequireCurator` and the read-only `POST /photos/bulk/location-summary` (a POST because its argument is the
  whole selection) behind `RequireWrite` — it only feeds the bulk location operation, which a curator may not
  use — and the read-only `POST /photos/bulk/membership-summary` behind `RequireCurator`, the guard of the
  upload whose done stage it feeds; `handleBulk` then authorizes **on the fields**: a caller whose role is not `CanWrite()` and whose batch
  is `bulk.Operations.BeyondCuration()` gets **403 before `Apply` runs**, so nothing of the batch is written; after a committed batch `enqueueSidecars` schedules one sidecar rewrite per updated photo
  and `enqueueGeocodes` one `places` job per photo in `LocationChanged` — the same derived work a
  single-photo location edit owes (`photoapi.PlacesEnqueuer`), both best-effort: a queue failure is logged,
  never returned, and never breaks the edit it derives from; the body
  `{photo_uids,operations}` via `operationsInput` with **set/clear pairs as separate keys**
  (unambiguous, a `set_*`+`clear_*` / `archive`+`unarchive` conflict → 400), `set_caption`→title,
  **`set_rating` (0–5) / `set_flag` (none/pick/reject/eye)** with validation → 400,
  **`set_taken_at {precision,value}`** where the value's shape follows the precision
  (`day`/`1974-06-14`, `month`/`1974-06`, `year`/`1974`, `decade`/`1970` — a year mid-decade rounds down),
  parsed in UTC by `resolveTakenAt` to the period's first instant; no time of day (the operation is for
  scans, where the hour is never known); an unknown precision or a value that does not parse at it → 400,
  **`clear_taken_at`** (bool, mirroring `clear_location`) declares the date unknown, and together with
  `set_taken_at` → 400 (one states a date, the other states that nobody knows it),
  **`set_location {lat,lng,only_missing?}`** with coordinate validation (lat −90..90, lng −180..180 → 400)
  and `only_missing` deciding what happens to the targets that already have a location, `DisallowUnknownFields` (an unknown operation → 400) + 4 MiB limit; errors mapped
  `ErrNoPhotos`/`ErrNoOperations`/`ErrAlbum/LabelNotFound`→400, `ErrBatchTooLarge`→413,
  **`ErrPhotoNotFound`→404 `{"error":"photo not found"}`** (a batch with any uid outside the request's
  project, foreign or missing — one body, no uid, nothing changed), otherwise 500;
  `isolation_integration_test.go` proves the foreign batch against the missing one with
  `projecttest.AssertSameShape` and that the summaries count only the project; mounted by a further `server.WithAPI`
  (`buildBulkAPI` in `cmd/kukatko/bulk.go`)),
  `internal/mapy/`
  (a server-side HTTP client of the mapy.com REST API, **the key never leaves the server** — it is sent only
  in the `X-Mapy-Api-Key` header, never in a URL/error, all behind the `Client` interface (fakeable):
  `New(Config{BaseURL,APIKey,Lang,Timeout,HTTPClient})` → `*HTTPClient` (**without an `HTTPClient` the
  client gets a connection pool of its own** — `ownConnectionPool()`, a clone of `http.DefaultTransport`,
  so it keeps the default's proxy/dial settings without sharing the process-wide pool that
  `httptest.Server.Close()` empties behind everyone's back; see `internal/apitest`); `Tile(ctx,TileParams{
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
  TileCacheTTL,TileCacheBytes,GeocodeCacheTTL,GeocodeRatePerSec,GeocodeRateBurst,GeocodeProjectRatePerSec,
  GeocodeProjectRateBurst,MaxGeoPhotos})`+
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
  keyed by the **project** + the coordinates to 5 decimals, an uncached lookup goes through `allowGeocode`→429 to save credits:
  the **project's** `ratelimit.Limiter` bucket first (default 3/s, burst 10; negative rate = off), then the
  **instance-wide** one (5/s, burst 10) — a project over its share is refused without spending the
  instance's token, so it cannot starve the others (`ratelimit.go`),
  the response simplified + `Cache-Control: private`), `GET /map/geocode?q=&limit=` (`geocode.go`:
  the type-ahead for the location editor; the order of the guards follows cost — an empty/>200-character `q` → 400 **before**
  the call, then `ttlCache[[]Place]` (key = the project + `limit` + the case-folded query with collapsed spaces,
  **diacritics are kept** — `veseli`/`veselí` are different queries upstream), and only the rest goes to
  **the same two buckets as rgeocode** (one credit budget = one limiter, shared fairly) → 429. Both
  caches are keyed by project (`projectCacheKey`) because a hit answers faster than a miss: a shared entry
  would tell one project that someone in another had looked the place up — chosen over masking the
  latency, which would slow every hit; the price is that two projects each pay once for one place; the client
  debounces on top of that, this is the half of the throttle that cannot be bypassed. `limit` is **clamped**
  (`mapy.ClampGeocodeLimit`), not a 400. `mapy.ErrNotFound` (a 404 upstream) is turned into an **empty
  `items` + 200**; otherwise `writeGeocodeError` as with rgeocode. The `ttlCache` cache (`cache.go`,
  generic: TTL + capacity, lazy expiry, eviction of the soonest-expiring — deliberately **not LRU**, unlike
  `tileCache`, because every entry is equally expensive), default 2000 entries),
  `GET /map/photos` (a GeoJSON
  **FeatureCollection**, `parseGeoParams` forces `HasGPS=true` + honours `taken_after`/`taken_before`/
  `album`/`label`/`archived`, `Limit=MaxGeoPhotos`, ordering taken_at desc; every feature is a
  `Point` with an RFC 7946 `[lng,lat]` coordinate and the properties `uid`/`title`/`taken_at`/`media_type`/
  the relative `thumb` path `tile_224` (built on `auth.APIBase(ctx)`, so it follows the project prefix the
  feed was asked through) and the photo's `blurhash` (omitted when none — the map's photo tiles
  paint it while the thumbnail loads), photos missing either coordinate are skipped; the route alone is wrapped
  in chi's `middleware.Compress` (gzip level 5, `geoCompressLevel`) — ~0.8 MB of JSON for prod's located
  library, ~0.18 MB on the wire (docs/PERF.md § 6); the collection also
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
  `SavePlace(Place)` (upsert on `photo_uid`, stamps `geocoded_at`)/`ListPhotosMissingPlaces(projectUID,limit)`
  (uids of **one project's** non-archived **geotagged** photos with no `photo_places` row, newest-first, LEFT JOIN
  with `p.project_id = $1` — the basis of the backfill; an empty project is `places.ErrNoProject`). `photo_places`
  is a **child** of `photos` and carries no project: the per-photo reads/writes act on a photo the caller holds, and
  every library-wide read reaches the project through the joined photo — the place tree is
  `photos.Store.AggregatePlaces(projectUID, country)`, the review's `PlacesByPhotoUIDs` is handed the uids of an
  already-scoped window)), `internal/placesjob/`
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
  shared by the job, the status service and the collector; `BackfillPlaces(ctx, projectUID)` enqueues `places`
  for every geotagged photo **of the project** without a place (dedup no-op), returns the count, and
  `MissingPlaces(ctx, projectUID)` is its
  **dry run** — the same uids listed rather than enqueued, which is what the maintenance scan's
  `missing_places` finding counts and what labels the "fill in the places" option with how much work it is.
  Scheduling is not spending: a run over a whole library may queue thousands of jobs and the window budget
  still lets only `Limit` of them reach mapy.com per window, **deferring** the rest (no attempt burned, no
  credit spent) until it refills, so they drain across windows instead of overrunning the quota; the payload's **`force` flag**
  routes to `ForceGeocode(uid)`, which shares `geocode(uid,force)` with `Geocode` and differs only in not
  treating the cached place as a reason to stop, so mapy.com is asked again and the answer **replaces** the row.
  It costs a credit every time by definition, which is why nothing schedules it automatically — the upload
  pipeline and the backfill keep the skipping path. It backs `POST /photos/{uid}/regeocode` and
  `jobs.Enqueuer.EnqueuePlacesRebuild`), `internal/importer/`
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
  `Run`; **`import_runs` is a root table — every run belongs to one project**: `Start`, `Get`, `List`, `LatestRun`
  and `ListFailures` take the project (an empty one is `ErrNoProject`) and see that project's runs only (a run of
  another project is `ErrRunNotFound`); the lifecycle writes `UpdateCounts`/`Complete`/`Fail`/`RecordFailures` address
  the id `Start` returned to the same caller. `import_failures` is a child of its run and is scoped through it
  (`run_id IN (SELECT id FROM import_runs WHERE project_id = $1)`, always the first clause).
  `Store` = `NewStore(pool)`: `Start(ctx,projectUID,source)` opens a `running` row (`ErrInvalidSource`),
  `UpdateCounts(ctx,id,counts)` overwrites the tally, `Complete(ctx,id,watermark,counts)` closes it as
  `done`/`partial` with `finished_at` stamped, `Fail(ctx,id,lastErr,counts)` as `failed`
  **without** a watermark (both match a running run only → `ErrRunNotFound` on a double close);
  every live caller passes a `nil` watermark (a folder has no source timestamp to resume from), so the
  column only carries the finished migration's values,
  `Get(ctx,projectUID,id)`, `LatestRun(ctx,projectUID,source)` →
  `(Run, found bool, err)` **the newest run of the source regardless of state** (running/done/failed;
  the basis of the system-status dashboard),
  `List(ctx,projectUID,limit,offset)` a page of the project's runs
  **across all sources** newest-started-first (limit clamp `[1,200]`, default 50, a non-nil empty
  page) — the basis of the admin import history; the sentinels
  `ErrRunNotFound`/`ErrInvalidSource`; **`import_failures`** (migration `0042_import_failures.sql`) keeps
  the individual **per-photo and per-file** defects of a run, which used to go only into `slog.Warn` and vanish (satellites —
  markers/album membership/edits/pHash/files): `Failure`
  (`RunID`/`Source`/`Stage`/`PhotoUID`/`SourceRef`/`Detail`/`Error`/`CreatedAt`/`ResolvedAt`), `Stage` ∈
  `photo|file|marker|album_member|label|thumbnail|embedding|faces|phash|edit|metadata`, the helper
  `NewFailure(runID,source,stage,photoUID,sourceRef,detail,err)`; `RecordFailures(ctx,[]Failure)` (batch),
  `CountUnresolvedFailures(ctx,id)`, `ListFailures(ctx,FailureFilter{ProjectUID,RunID,Source,UnresolvedOnly,Limit,Offset})`.
  `Counts` (the `counts` JSONB) is `imported`/`updated`/`skipped`/**`deduplicated`**/`failed` — the fourth bucket
  counts SOURCE photos whose content was already catalogued under another source photo (see the
  `photoprism_aliases` table in `internal/photos`); reading that as "skipped" is how hundreds of production
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
  **the request project's** `import_runs` newest-started-first via `importer.Store.List`, and `GET /import/failures`
  (the project's failures only — `FailureFilter.ProjectUID` is the request's project; the group is mounted per
  project, so `/api/v1/p/<slug>/import/…`)
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
  **what counts as an original is decided in one place** (`classify.go` over `internal/storekeys`):
  `backedUp(kind)` is a **default-less switch over every kind of object the store can hold**, so a new prefix
  cannot be added to the store without this package deciding about it (`TestBackedUp_decidesEveryKind` +
  the `exhaustive` linter). Originals, metadata sidecars and the library's genealogy export
  (`families.yaml`, the only copy of the family tree outside the database) travel; `thumb/` and **`hls/`**
  do not — both are
  reproducible from an original by an ordinary background job (the streaming segments used to be copied
  as if each segment were an original: 470 MB of regenerable data against 1.9 GB of video originals on
  staging, growing with every encode); `db/` and `.tmp/` are not library content; a **foreign** object *is*
  backed up, since its value is not this package's to judge. `skippedDir` lets the disk walk turn away at
  the root of a derived prefix instead of statting everything inside it. Every verdict holds in **both layouts**
  (`storekeys.Classify` strips a `projects/<uid>/` prefix first): a created project's originals, sidecars and
  `families.yaml` travel, its `thumb/` and `hls/` are skipped whole by the walk, and a `db/` or `.tmp/` inside a
  project's tree is foreign — so it is backed up. `reconciledKey` decides what the post-restore `Verify` holds
  against the catalogue's file rows: originals (either layout) and foreign files, **never** sidecars or
  genealogy exports, which no file row names (they used to come back as "extra on disk", one per photo);
  **`DiskOriginals`** (`NewDiskOriginals(root)`, backend `fs`) = a walk of the storage (skipping the derived and
  temporary prefixes whole, confining keys against traversal), `CopyTo` streams the file up via `Put` —
  **it also serves the restore** through `Stat(key)` (exists + size, for skip-existing) and `Write(key,r)`
  (an atomic write into `.tmp` + rename → resumable);
  **`BucketOriginals`** (`bucket.go`, `NewBucketOriginals(source,bucket)`, backend `r2`) = `List`
  lists the primary bucket and keeps only what `backedUpKey` says belongs in a backup,
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
  `RestoreOriginalsResult{Downloaded,Skipped}`; **extra objects are tolerated, never fatal** — a backup taken
  before the derived prefixes were classified holds thumbnails and HLS segments, and a restore runs on the
  worst day of an instance's life, so they simply come back and are ignored by `Verify`) / **`Verify`** (the integrity report `VerifyReport`
  {PhotosInDB,FilesInDB,OriginalsOnDisk,MissingOnDisk,ExtraOnDisk,Consistent} via the pure `reconcile`
  set-diff of `photo_files.file_path` vs the disk); **`pgRestorer`** (`NewPgRestorer(dsn)`) = a shell-out to
  `pg_restore --format=custom --clean --if-exists --no-owner --no-privileges --single-transaction
  --dbname=<db>`, reading the archive **from stdin** (never whole in RAM), with the **DSN parsed into PG\* env**
  (`PGHOST`/`PGPORT`/`PGUSER`/**`PGPASSWORD`**/`PGDATABASE` via `pgx.ParseConfig`) → the password is **never
  in argv**; `PgRestoreAvailable`, the sentinels `ErrPgRestoreMissing`/`ErrInvalidDSN`; no secret leaks
  anywhere. **What a restore leaves behind is the streaming renditions**: the backup carries no segments, while
  the dump carries every `photo_hls_renditions` row, and a row promises a player it can fetch them — so
  `kukatko restore db` clears the table after re-applying the migrations (`repairRestoredDatabase` →
  `hlsjob.Store.ClearAll`) and prints what to run, leaving every video in exactly the state the ordinary
  `POST /process/hls` backfill looks for; runbook [`docs/RESTORE.md`](RESTORE.md)), `internal/backupapi/`
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
  it reveals drift between the catalogue and the originals the store holds and fills in/regenerates derived data; it mirrors
  the previous system's `cache build-thumbs`, but is broader and safer (**it never deletes originals** — that is the
  job of the trash/purge), idempotent, with repairs going through the persistent job queue; all behind the interfaces
  `PhotoCatalog` (`CountPhotos`/`ListPrimaryFiles`/`ListCataloguedFiles`/`ListPhotosMissingPhash`/
  `ListDimensionMismatches`/`RepairDimensions`/`ListImpossibleTakenAt`/`ClearImpossibleTakenAtAudited`,
  satisfied by `photos.Store`)/`VectorCatalog` (`ListPhotosMissingEmbedding`/`ListPhotosMissingFaces`/
  `PlanFaceBoxRepair`/`ApplyFaceBoxRepair`/`ListDuplicateFaceMarkers`/`ListSidewaysDetections`/
  `ClearFaceDetection`, `vectors.Store`)/`OriginalStore` (`Stat`, `storage.Storage`)/`StoreScanner`
  (`Kind`+`ListObjects`, yielding every object a project's tree owns as `StoreEntry{Key,Kind,ProjectUID}`;
  satisfied by `NewStoreOriginals(kind, storage.KeyLister)`)/`ProjectLister` (`List`, `project.Store`; optional,
  **nil = one library, the bootstrap project in the root layout**)/
  `ThumbChecker` (`HasThumbnail(filePath,fileHash)` — the cache entry under the prefix of the original at
  `filePath` —, `NewThumbCache` over `thumb.Thumbnailer`)/
  `Enqueuer` (`EnqueueThumbnail`+`EnqueueFaceDetect`, `jobs.Enqueuer`)/`SidecarScheduler`
  (`EnqueueSidecar`, `jobs.Enqueuer`; optional, **nil = the sidecar export is off**, so no job is scheduled
  that no handler could drain — `maintenanceSidecarOrNil` in `cmd/kukatko/maintenance.go` is the same switch
  every mutating API goes through)/`EmbedBackfiller` (`embedjob.Service`)/
  `FaceBackfiller` (`facejob.Service`)/`FaceCache` (`ClearSurplusLinks`, `facematch.Service` — which owns
  the face↔marker pairing rules the cache has to agree with)/`OrphanImporter` (optional, nil turns the
  orphan import off)/`PlaceBackfiller` (`MissingPlaces`+`BackfillPlaces`, `placesjob.Service`; optional,
  **nil = no mapy.com key**, which empties the finding and makes the repair refuse)/
  `Streaming{Renditions StreamingCatalog, Segments SegmentStore}` (`ListRecorded`+`EncodingHashes`+`Delete`,
  `hlsjob.Store`; `KeysWithPrefix`+`Stat`+`Delete`, the storage backend — both optional, **a zero `Streaming`
  = `video.hls.enabled: false`**, which reports nothing and asks the store nothing) →
  unit-testable with fakes without DB/disk/queue; `Service` = `New(Config{...,SampleLimit})`
  (panics on a nil mandatory collaborator; default `SampleLimit` 20); **`Scan(ctx)`** (read-only) returns
  `Report{Photos,FilesInDB,Store,Projects,OrphanProject,LegacyLayout,MissingOriginals,OrphanFiles,MissingThumbnails,
  MissingEmbeddings,MissingFaces,MissingPhashes,MissingPlaces,TransposedDimensions,TransposedFaceBoxes,
  DuplicateFaceMarkers,SidewaysFaceDetections,ImpossibleDates,MissingRenditions,OrphanSegments,
  UnscheduledSteps,RootDerived}` — each class is a
  `Finding{Count,Samples}` (`OrphanSegments` is a `SegmentOrphans`, a `Finding` plus `Bytes`)
  (a count + a limited sample of identifiers); `representativeThumbSize`=`tile_224` is the proxy for the presence of
  thumbnails, an orphan = an original **in the store** with no `photo_files.file_path`, `Report.Clean()`;
  **`Store`** is the inventory of whichever store the instance runs on (`StoreInventory{Kind,Originals,Error}`,
  `Kind` ∈ `disk`/`object`, `Listed()`): `StoreOriginals` streams `storage.KeyLister.Keys` — `*storage.FS` walks
  the originals root, `*storage.R2` lists the bucket — and keeps only the keys matching the `YYYY/MM/<name>`
  layout, so thumbnails, metadata sidecars and anything else sharing a bucket are never counted and never offered
  to the orphan import (the same positive classification `internal/reset` uses, and the reason the wiring in
  `cmd/kukatko/maintenance.go` passes the backend itself rather than a walk of `storage.originals_path` — on an
  `r2` instance that root is empty, which used to report a full library as **0 originals on disk** and then call
  it consistent). The scan holds only the catalogue's key set and the orphans, never the store's keys, so a
  bucket of twenty thousand objects costs no more memory than a small one. A **failed listing is carried, not
  returned** (`Error`, `Originals` stays 0): the rest of the scan is still reported, but `Clean()` is false and
  the UI shows the failure instead of a green verdict — a store nobody could read must not pass for an empty one.
  The orphan *import* does abort on that failure, since importing nothing is not success.
  **Per project** (`projects.go`): the store is listed **once** and every object attributed by a `reconciler` —
  a `projects/<uid>/` key to that project, or to `OrphanProject` when no such project exists; a root-layout key
  to the project still using the root layout (`StoragePrefix == ""`); once none does (the bootstrap project's
  storage moved), a root original to the project whose catalogue row still points at it (the move is per photo)
  and otherwise to `LegacyLayout`, and a root derived object is "in use" while any catalogued file lives at the
  root and `LegacyLayout` once none does. A `projectLedger` folds the attributed originals, orphans, missing
  originals and missing thumbnails into one `ProjectReport{UID,Slug,Name,StoragePrefix,Photos,FilesInDB,Originals,
  MissingOriginals,OrphanFiles,MissingThumbnails}` per project (deactivated ones included), whose findings add up
  to the instance-wide ones. `OrphanFiles` and the orphan import cover only the originals of a **real** project's
  tree; `OrphanProject`/`LegacyLayout` are reported, never repaired, and make `Clean()` false;
  **`Repair(ctx,RepairOptions{Thumbnails,Embeddings,Faces,Phashes,ImportOrphans,Places,Dimensions,FaceMarkers,
  SidewaysFaces,ImpossibleDates,MissingRenditions,DeleteOrphanSegments,UnscheduledSteps,RootDerived},meta audit.Meta)`**
  (each opt-in,
  idempotent, in a fixed order) → `RepairResult` with the scheduling counts: thumbnails/phashes enqueue
  `thumbnail` jobs (`EnqueueThumbnail`), embeddings/faces call the backfill, the orphan import goes through the
  upload pipeline (a per-orphan failure is counted without aborting); `ErrOrphanImportUnavailable` when the
  import is selected without an importer.
  **`UnscheduledSteps`** (`unscheduled.go`) is the finding nothing else ever resolves: the optional
  `Config.Processing` (`ProcessingGaps`, satisfied by `processing.Service`) lists every (live photo, step)
  whose processing report would read `pending` — no evidence, no unfinished job, not switched off — sampled as
  `<photo_uid>/<step>`, which is exactly what an upload cut short after its original was stored leaves behind.
  `deferredSteps` drops `places` from both the finding and the repair: its pending backlog is deferred on
  purpose (paid geocoding) and already the `MissingPlaces` finding with its own repair, so it would only be
  double-counted and queued behind the curator's back;
  the repair schedules each through `processing.Service.Run` (`StepsScheduled`; a step that stopped applying in
  the meantime is skipped). Unwired → an empty finding and `ErrProcessingUnavailable` for the repair.
  **`RootDerived`** (`rootderived.go`): the optional `Config.RootDerived` (`RootDerivedMover`, satisfied by
  `*rekey.Service`) lists the thumbnails and streaming segments a storage move left at the root for photos whose
  row already points under their project's prefix — from the bucket listing, never the local thumbnail cache —
  sampled by root key; the ledger's `rootDerived` set makes the reconciler skip those keys, so they are not
  counted again as `LegacyLayout`. The repair hands them to `MoveRootDerived` (a prefixed copy that exists is
  kept, root copies deleted only once all exist; `RootDerivedMoved`/`RootDerivedObjects`/`RootDerivedFailed`).
  Unwired → an empty finding and `ErrRootDerivedUnavailable` for the repair.
  **`Moves`** (`moves.go`): the optional `Config.Moves` (`MoveWatcher` = `ActiveRuns`, satisfied by
  `*rekey.Service`). The repairs that delete at the root or judge derived media by the row's prefix —
  `DeleteOrphanSegments`, `RootDerived`, `MissingRenditions` (and `ImportOrphans`, the mirror case) —
  refuse with **`ErrStorageMoveActive`** (naming the oldest active move's project and state; 409 over HTTP)
  while any move is running or paused, touching nothing: a photo a move committed but has not finished has its
  row under the prefix and its segments still at the root, so they would read as orphans and its rendition as
  missing. Any move counts, since the root is what every move empties. The sweep additionally keeps (`OrphanSegmentsKept`) the root segments of a recorded rendition whose
  video already lives under a prefix (`movedRootPrefixes`) — the move's leftovers, `RootDerived`'s to move.
  Unwired → no refusal (`moves_integration_test.go`; `moves_streaming_integration_test.go` over the filesystem
  and the MinIO bucket: `MissingRenditions` refused mid-move, a failed move's root segments keep the row, an
  orphan rendition under `projects/<uid>/hls/` reported and swept).
  **`Places`** is the reverse-geocode backfill: `MissingPlaces` is its dry run (`MissingPlaces` finding —
  live photos carrying coordinates with no `photo_places` row, the same predicate as
  `system.LibrarySummary.PhotosPendingGeocode` and the dashboard's `PhotosWithoutPlace`) and the repair calls
  `BackfillPlaces` → `PlacesEnqueued`. It is a **catch-up**, not routine work: `internal/ingest` now enqueues
  `places` at upload, so this exists for the library that predates that and for photos whose coordinates were
  added later. It only fills the queue — the window budget still bounds what reaches mapy.com and defers the
  rest — and with no key wired the finding is empty and the repair returns
  `ErrPlaceBackfillUnavailable` (503 over HTTP), because no `places` handler is registered then and the jobs
  would wait for ever.
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
  waits), and a photo re-detected upright drops out of the scan, so a re-run is a no-op.
  **`ImpossibleDates`** is the fourth direct-write repair and the only one that **removes** a value.
  `photos.ListImpossibleTakenAt(minYear,maxYear)` is its dry run (`ImpossibleDates`, sampled by photo uid): the
  photos dated to a year no photograph can have been taken in, the bounds asked of `exif.CaptureYearBounds()` so
  the guard that stops a **new** file-name guess and the finding for the rows dated **before** that guard existed
  cannot disagree (`90090310_100000000000001_…` reads as 9009-03-10, and one such row sorts ahead of the whole
  library and stretches the year axis to meet it — `internal/metajob` cannot fix it, being a gap-filler that
  holds the capture time outside its reach). The repair **withdraws** the date rather than guessing a
  replacement (`photos.ClearImpossibleTakenAtAudited` → `ImpossibleDatesCleared`): `taken_at` → NULL,
  `taken_at_source` → `unknown`, so the photo becomes one **without** a date, findable as `dated:no`. Nothing is
  lost — the outgoing value goes into `taken_at_before_unknown` through the shared
  `photos.TakenAtBeforeUnknownAssignment` rule, so the clear is reversible and still readable, and originals are
  never touched. Each clear writes a **`photo.date_clear` audit entry in the clear's own transaction** (the
  durable-audit convention), carrying the acting maintainer from the `audit.Meta` `Repair` is given — a zero
  `Meta` is the CLI, i.e. a system action — plus the discarded date, its provenance and the file name. The
  `UPDATE` is guarded on the finding's own predicate, so a photo re-dated by hand between the scan and the
  repair is skipped (no row changed → no audit entry, not counted) and a re-run is a no-op; every cleared photo
  also gets an `EnqueueSidecar` (when the export is on), so the metadata on disk drops the date too and a
  restore cannot hand the year 9009 back).
  **Streaming** (`streaming.go`) is the half that knows a video's segments live in the store and its promise of
  them lives in the catalogue, so the two can rot apart in both directions. `MissingRenditions` is a row of
  `photo_hls_renditions` whose objects are gone — the state a storage migration that moved the originals but not
  the segments leaves behind, in which the photo still advertises itself as streamable and every segment request
  answers 404 — sampled as `<photo_uid>/<rendition>`. Presence is judged by the rendition's **initialisation
  segment alone** (`hls.InitName`): a player fetches it before any media segment, so a rendition without it
  cannot be played whatever else survives, and asking about every segment of every clip would be tens of
  thousands of round trips per scan. The stored playlist is no help — it is never written to the store, it is a
  column. A video under a project prefix whose init segment is absent there but present at the root (a storage
  move committed it and has not — or, failed, could not — copy its segments) is **not** missing: the segments are
  the move's retry's or `RootDerived`'s to bring under the prefix, and dropping the row would stop a video
  whose segments all exist. `OrphanSegments` is the other direction: objects under the streaming prefix of
  **every storage layout in use** (`streamingPrefixes`: the root's `hls/` plus each project's
  `projects/<uid>/hls/`, from `ProjectLister`; `parseSegmentKey` keeps the key's storage prefix in its group) that
  no recorded rendition claims, what a failed or re-run encode leaves, counted per object with their total `Bytes` and **sampled by the
  `hls/<file_hash>/<rendition>/` prefix** they sit under, because one abandoned encode is thousands of segments
  under a single prefix and the prefix is what names the video and the quality. Objects belonging to a video with
  an **unfinished `hls_transcode` job** (`hlsjob.EncodingHashes`: `queued`/`running`/`failed`) are excluded from
  both the finding and the sweep — `encodeOne` publishes a rendition's objects *before* it writes the row, so for
  the length of a transcode a healthy job's uploads are indistinguishable from abandoned ones.
  **Cost**: nothing at all with streaming off or with no video in the catalogue (`photos.CountVideos`, archived
  included — archiving removes neither rows nor segments, only a purge does), otherwise **one `Stat` per recorded
  rendition** (one per video under the default single-quality plan, one more for a moved video whose init
  segment is not under its prefix), **one prefix listing of `hls/` per storage layout in use**, and one further
  `Stat` per orphan object found — so a healthy library pays only the listings.
  The repairs are asymmetric on purpose. `MissingRenditions` drops the row (`hlsjob.Store.Delete` →
  `RenditionsDropped`): the clip stops claiming it streams and lands in exactly the state the ordinary encode
  backfill (`POST /process/hls`) looks for, so recovery is one background job; the original is never touched and
  stray segments of a partly-surviving rendition are left where they are, becoming orphans the scan reports.
  `DeleteOrphanSegments` is the **only** part of the integrity check that removes anything from the store, which
  is why the scan never sweeps on its own and the flag has to be asked for by name; it re-reads the queue rather
  than trusting the scan (so an encode that started in between is still safe → `OrphanSegmentsKept`), collects
  every key **before** deleting any (removing entries from under a filesystem walk silently skips half of them)
  and reports `OrphanSegmentsDeleted`. Both refuse with `ErrStreamingUnavailable` (503 over HTTP) on an instance
  that does not stream: nothing encodes there, so a dropped row would never be produced again), `internal/reset/`
  (**the guarded library wipe** — what `kukatko maintenance reset` runs and what phase 1 of
  [`docs/MIGRATION_PLAN.md`](MIGRATION_PLAN.md) had nothing to run before: it empties every catalogue table and
  every object the store owns so the library can be re-imported from scratch. The deployment has **no S3 backup**
  ([`READINESS_AUDIT.md`](READINESS_AUDIT.md) §4), so the guards *are* the package and the truncation is the easy
  part. **Two explicit table lists** in `tables.go` — `catalogueTables` (44, wiped, incl. the four `upload_link*` tables from `0090`, `photoprism_aliases`
  from `0046` and `review_skips` from `0059`) and `preservedTables` (10: `users`/`sessions`/`api_tokens`/
  `passkey_credentials`/`password_reset_tokens`/`user_pictures`/`announcements`/`instance_settings`/
  `audit_log`/`schema_migrations`, never touched), exported as
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
  `PrefixCounts{Originals,Thumbnails,Sidecars,Families,HLS}`; **`Execute(ctx,Options,before)`** deletes, in
  this order —
  `ErrNotExecuting` without `Options.Execute`, target re-verified, `checkConfirmation` (`Options.Confirm` must
  equal the target database name → `ErrConfirmationMismatch`, **and** `Options.ConfirmBucket` must equal
  `Target.Bucket` → `ErrBucketConfirmationMismatch` — the two come from independent config keys and can name
  independent deployments, so a dev database pointed at the production bucket is refused rather than confirmed
  by proxy; an empty `Target.Bucket` (the `fs` backend) makes a typed bucket name a mismatch too, since the
  operator was aiming at something this run cannot reach), the **store emptied before the catalogue** (the catalogue is where
  the object keys come from), and only then `TRUNCATE … RESTART IDENTITY` (**no CASCADE**) plus
  `audit.Write(ActionLibraryReset)` **in one transaction**, then a re-count → `Result{Before,After,Storage}`.
  That transaction opens with the wipe's one piece of surgery (`unlinkAccounts`): drop
  `project_members_subject_uid_fkey`, null every `project_members.subject_uid`, truncate, put the
  constraint straight back. `project_members` is preserved and therefore outside the TRUNCATE list, and
  Postgres refuses to truncate a table an unlisted table references — structurally, whatever the rows say —
  so that one foreign key would fail the whole wipe, and CASCADE (which would take the memberships) is the
  one escape this command must never use. DDL is transactional, so a failure anywhere puts the constraint
  back with everything else. Nulling the column is also simply true: the person that link named is about to
  stop existing. Every account keeps its credentials, role, note and history, every membership its role —
  the link is the only thing a preserved membership loses.
  Any object that fails to delete skips the truncation and returns `ErrStorageIncomplete` — the catalogue is
  the only remaining record of what those objects are, and the whole run is idempotent, so the answer is to fix
  the store and repeat. `keys.go` owns the scope: `classifyKey` translates `storekeys.Classify`
  (the shared classification of the store's layouts — `YYYY/MM/<name>` by an anchored regexp, since the bucket
  root *is* the namespace and there is no Kukátko prefix, plus `thumb/`, `sidecars/` and `hls/`) into what a
  wipe owns, and calls everything else — a dump, a half-written upload, anything foreign — `kindForeign`,
  which is counted and **never** deleted; the switch has no default clause, so a new prefix in the store must
  decide here too rather than silently surviving a reset; the HLS segments are the one owned
  prefix `PrefixCounts` can only ever count on a **sweep** — how many segments a video was cut into, and in
  which renditions, is known to the store and not to the catalogue, so `Referenced.HLS` is always 0 and only
  `Options.OrphanSweep` reaches them; `catalogueFiles.objectKeys` expands each
  catalogued path into its original + `sidecarexport.KeyFor` sidecar and each hash into `thumb.RelPath` × every
  registered size (blind on purpose: probing first would cost a request per candidate), and names
  `familyexport.Key` unconditionally — the genealogy export belongs to no photo, so no catalogue row implies
  it, and a family tree that survived a wipe would restore itself over the empty library; `deleteKeys` runs the
  deletions through an `errgroup` bounded by `Options.Concurrency` (default 8), folding each outcome into
  `StorageResult{Deleted,Missing,Skipped,Foreign,Failed,Failures,ThumbCacheCleared,ThumbCacheSwept}` with the
  failure sample capped at 20 (`Touched()` reports whether the run got as far as doing anything);
  `sweepKeys` is the opt-in `Options.OrphanSweep` path over `storage.KeyLister` (`ErrSweepUnsupported` when the
  store cannot list) and **supersedes** the catalogue's candidates — it deletes what is actually there under the
  owned prefixes, referenced or not, which is both the correct wipe and the cheaper one. The local thumbnail
  cache goes too: `Thumbs.Remove(prefix,hash)` per catalogued (file prefix, hash) pair, or the whole `<cache_path>/thumb` tree under a
  sweep. `Options{Execute,Confirm,ConfirmBucket,OrphanSweep,Concurrency,ActorUID,Operator}` — a CLI run has no session, so
  `ActorUID` stays empty (a system action) and `Operator` carries `$USER@$HOSTNAME` into the entry's details
  together with the target, the per-table row counts and the object counts. Deliberately **no HTTP surface**,
  for the reason `restore db` has none: it pulls the tables out from under a running server.
  **Both layouts:** `catalogueFiles` also reads every project's `storage_prefix`, and `objectKeys` names
  `families.yaml` at the root, under every project prefix and under the prefix of every catalogued file
  (`familyPrefixes`, malformed prefixes skipped); a catalogued key carries its prefix, a thumbnail set the prefix
  of its own original, and the sweep classifies `projects/<uid>/…` exactly as the root.
  **`kukatko project purge`** (`purge.go`): `NewPurger(PurgeConfig{Pool,Store PurgeStore,CacheDir,Concurrency})`
  (`PurgeStore` = `ObjectStore` + `storage.SizedKeyLister`) removes **one** project. `Plan(ctx,PurgeOptions)`
  resolves the slug, refuses the bootstrap project without `AllowBootstrap` (`ErrPurgeBootstrap`) and any project
  with an empty prefix (`ErrPurgeRootPrefix` — its "prefix" is the whole store), and counts `PurgePlan{Project,
  Tables,Objects,Bytes}`: the rows of `jobs` (payload `project_uid`, or the photo/album/subject it is about;
  running ones excluded), of every `catalogueTables` entry that carries `project_id` (filtered against the live
  schema, `classifySchema` first; ordered by `referencingFirst` over the live foreign keys, so a table goes before
  the tables it references), of `project_members` and `project_invites`; the objects/bytes under the prefix
  from one `KeySizes` listing. `Execute(ctx,opts,plan)` (`ErrPurgeNotExecuted` without `Execute`,
  `ErrPurgeNotConfirmed` unless `Confirm` is the slug) deletes those rows, then the project row (cascading its
  settings and announcement), and writes the `project.purge` audit entry (`ProjectUID` = the project) **in one
  transaction**; only then every object under the prefix (`deleteKeys`) and `<cache_dir>/projects/<uid>`. Returns
  `PurgeResult{Plan,PurgedAt,Storage,CacheRemoved}`, the CLI's JSON report), `internal/thumbjob/`
  (the worker handler of the `thumbnail` job — the **repair path** for maintenance: it regenerates a photo's derived
  data from the original, the **thumbnails** (`Thumbnailer.GenerateAll`, cached ones skipped), the **pHash/dHash** (only when
  they are missing, `phash.Compute` over the decoded original) and the **blurred placeholder**
  (`ensureBlurhash`, only when missing: `Thumbnailer.OpenOrGenerate` the `PlaceholderSize` = `fit_720`
  preview → `image.Decode` → `blurhash.Encode` → `PhotoStore.SaveBlurhash`), all behind the interfaces `PhotoStore`/`Thumbnailer`/
  `Decoder` (`StorageDecoder` = `NewStorageDecoder(store, maxPixels, budget)`: `storage.Materialize`+
  `imgconvert.EnsureDecodable`, then the pixel cap and the decode budget via `imgconvert.ReserveDecode` — the
  reservation is given back by the returned cleanup; fakeable) →
  unit-testable without a disk; `Service` = `New(Config{Photos,Thumbnailer,Decoder,Lister?,Enqueuer?})`
  (panics on a nil mandatory collaborator; `Lister`/`Enqueuer` optional — they turn the backfill on),
  `Handle`=`worker.HandlerFunc` (payload `{photo_uid,force?}`, empty uid → `ErrMissingPhotoUID` dead-letter),
  `Regenerate(uid)`/`ensurePhash`/`ensureBlurhash` idempotent; registered in `serve` on `jobs.TypeThumbnail`.
  **During a storage move** `Handle` (the job path only; the on-demand rebuild relies on the thumbnailer's
  locked re-read) defers — `worker.RetryAfter`, from `MoveRetryDelay` backing off with the job's age to
  `MaxMoveRetryDelay` (`moveDeferral`) — a photo a running move has yet to take (`Config.Moves`,
  `*rekey.Gate`, `ErrAwaitingMove`) and a generation the thumbnailer refused because the photo's prefix moved
  mid-job or the move holds the photo right now (`thumb.ErrPrefixMoved`); the re-run renders from the moved
  row (`move_test.go`). `ForceRegenerate` returns such a refusal wrapping `thumb.ErrPrefixMoved` and **not**
  `ErrRegenerateFailed`, so the HTTP rebuild answers 503 + `Retry-After`, not 422.
  After the thumbnails are in place it stamps `photos.thumbnails_at` — the processing report's evidence for
  the step: the repair through `MarkThumbnailsPresent` (only an unstamped photo, so a repair never passes for
  a rebuild), `ForceRegenerate` through `MarkThumbnailsBuilt` (always moved — what a viewer waiting for a
  saved edit's rebuild watches); a failed stamp fails the job, so it is retried.
  **The placeholder lives here rather than in a job of its own** because it is derived from the *rendering*,
  not from the original: it is read back out of the preview this job has just written, so a photo whose
  rendering changed (a saved crop or rotation, which forces a rebuild) gets a matching placeholder in the
  same run, with no second job to race against. It is the one derived value the job reads from a rendition
  instead of from the original, which is also what keeps it cheap on a photo whose thumbnails are all cached.
  The payload's **`force` flag** routes the job to `ForceRegenerate` instead: the repair skips a size already cached,
  which is right when the cache is merely incomplete and wrong when the photo's *rendering* changed, since nothing
  about the cache key records the edit. It is what `jobs.Enqueuer.EnqueueThumbnailRebuild` schedules from `PUT
  …/photos/{uid}/edit`. The pHash it recomputes stays the **original's**, not the edited rendering's — a perceptual
  hash exists to recognise two copies of one shot, and turning one of them upright must not stop it matching the
  other, so a rotation changes the grid and deliberately leaves duplicate detection where it was.
  The **force path** `ForceRegenerate(uid) ([]string,error)` is the on-demand counterpart (the basis of the service
  action "regenerate thumbnail" in `photoapi`): it **overwrites** every thumbnail (`Thumbnailer.RegenerateAll`,
  an atomic overwrite) and **always** recomputes the pHash (`recomputePhash`, shared with `ensurePhash`) and the
  placeholder (`recomputeBlurhash`, run **after** `RegenerateAll` so it reads the rendering that was just
  written rather than the one it replaced), returning
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
  shrink what a later real run schedules.
  **The video-poster backfill** `BackfillVideoPosters(ctx) (int,error)` + `CountBackfillVideoPosters(ctx)`
  (the basis of `POST /process/thumbnails?videos=true`) covers every non-archived video
  (`PhotoLister.ListActiveVideoUIDs`/`CountActiveVideos`) and enqueues a **forced** rebuild
  (`Enqueuer.EnqueueThumbnailRebuild`, `enqueueAllRebuilds`) rather than a plain job: a video's poster frame is
  chosen while its original is decoded, so rebuilding its thumbnails re-picks it — but only a *forced* job
  re-encodes a size that is already cached, and every one of these videos has a full set. It is how black video
  tiles are repaired without re-uploading anything; `ErrBackfillUnavailable` without a `Lister`/`Enqueuer`.
  **The placeholder backfill** `BackfillBlurhash(ctx,all) (int,error)` + `CountBackfillBlurhash(ctx,all)`
  (the basis of `POST /process/blurhash`) is the same machinery over a different predicate: every photo
  **without a placeholder** (`PhotoLister.ListPhotosMissingBlurhash`/`CountPhotosMissingBlurhash`, served by
  the partial index `idx_photos_blurhash_pending`), or — when `all` — every non-archived one; it enqueues
  `thumbnail` jobs through the same `Enqueuer.EnqueueThumbnail`, so the two backfills differ only in which
  photos they pick and both inherit the queue's per-photo dedup. Videos are **not** excluded: their
  thumbnails come from the poster frame the pipeline extracts, so a clip has a first frame to blur),
  `internal/sidecarexport/`
  (**the format** of the metadata sidecar + its atomic write into storage — a YAML file per photo next to
  the originals, so the library can be restored **from storage alone**: originals + sidecars, without a database.
  Not to be confused with `internal/sidecar`, which reads *foreign* sidecars (Google Takeout `.json`, Apple `.xmp`) during
  an import — this package only **writes**, and only its own format. `Document` = a versioned, grouped
  schema (`version`/`generated_at`/`identity`/`descriptive`/`temporal`/`spatial`/`technical`/
  `curation`/`edit`), `Version = 6` (v2 added `curation.hidden_from_library` — additive, but still a bump,
  because a reader that ignored the key would un-hide every hidden photo on restore; v3 added
  `temporal.precision`, omitted at the ordinary `day` grain, for the same reason: a reader that ignored it
  would restore "somewhere in the seventies" as a photo taken on 1 January 1970; v4 added
  `temporal.taken_at_before_unknown`, the date put away when somebody declared a photo's date unknown —
  a reader that dropped it would turn a reversible declaration into a permanent one, since after losing
  the database the sidecar is the only place that date still exists; v5 added `curation.people[].nickname`,
  what a village actually calls somebody — a fact only the database holds, so dropping the key would restore
  the person under the name on their documents and lose the one everybody used; v6 added
  `identity.upload_link` `{uid, title, uploader_name, uploaded_at}` — the upload link a photo came through
  and the name its anonymous sender typed, filled by `sidecarjob`'s optional `UploadLinks` source
  (`uploadlink.Store.Provenance`, best-effort like the uploader name));
  `Build(Input) Document` is a **pure function** (no I/O, no
  clock — the caller collects the collaborators), `Marshal`/`Unmarshal` add/ignore the header comment
  that explains **why there are no embeddings in the file** (large, binary, cheap to recompute from
  the original — that is what the backfill jobs are for), so that nobody "fixes" it. `KeyFor(fileKey)` = the parallel
  tree `<prefix>sidecars/<rest>.yml` under the original's own storage prefix (`storekeys.Strip`): the sidecar of
  `projects/<uid>/2024/05/x.jpg` is `projects/<uid>/sidecars/2024/05/x.jpg.yml`, never `sidecars/projects/…` (the extension is **appended**, not replaced → `IMG_1.jpg` and `IMG_1.png`
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
  and the write, this one knows *when*. **During a storage move** (`Config.Moves`, a `MoveGate` =
  `*rekey.Gate`) `Handle` defers (`worker.RetryAfter`, `ErrAwaitingMove`) a photo a running move has yet to
  take, and `Export` re-reads the photo right before the write, under the move's photo lock
  (`MoveGate.Publish`), which the write finishes inside: a changed `file_path` rebuilds the sidecar next to
  the current key (up to `maxExportAttempts`, then `ErrPathMoved` deferred), a photo the move holds is
  deferred (`ErrPathMoved` wrapping `storekeys.ErrMoving`) — never written next to the key the photo left;
  move deferrals back off with the job's age to `MaxMoveRetryDelay` (`backOff`, `move_test.go`). The organise readers get the project of the photo the job loaded
  (`Photo.ProjectID`), so a sidecar records only its own project's albums, labels, favourites and ratings. The job is enqueued by every mutation of metadata/curation data; the handler
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
  out of its reach) → **idempotent**, a second run changes nothing (not even `updated_at`); `original_name` is
  reconstructed from `photo.FileName` (storage keeps the original under the name it arrived with — the catalogue
  cannot get closer, and it is written into an empty column anyway). **A missing original** (`os.ErrNotExist`
  from `Materialize`/`Extract`) is **logged and skipped** (a nil error): the file is gone, a retry will never
  succeed and a dead-letter would only break a library-wide run; other storage/DB errors are returned → the queue
  retries. **The video re-probe** (`videorepair.go`, `Extractor.ProbeVideo` = `storage.Materialize` +
  `video.Probe`): for a `media_type=video` the handler *also* probes the container again, because those
  columns were read **exactly once**, while the clip was being ingested — a probe that failed there left them
  empty for ever, and a clip with no duration can never get its scrub preview (`internal/storyboardjob`
  refuses without one). `planVideoRepair(photo,probe)` is a pure function and holds the whole policy:
  **file-derived** fields (`duration_ms`, dimensions, `fps`, `video_codec`, `audio_codec`+`has_audio`, the
  last two always as a pair) may be **corrected**, since nothing but a probe ever writes them — a value that
  differs from the file is offered, an identical one is not (so a re-probe of a healthy clip is a no-op), and
  a field the probe could not read is never written ("I did not see it" ≠ "it is not there").
  The **curated** fields are gap-fill only: a `taken_at` is offered solely when the photo has none or one
  merely guessed from its file name (a `manual` date, an `exif` date, a date declared unknown — the disowned
  value sits in `taken_at_before_unknown` — and a `taken_at_estimated` one all stand), coordinates solely
  when the photo has **none at all and an empty `location_source`** (the rule `internal/geoestimate` already
  follows, which is what keeps `manual` + no coordinates the tombstone it is meant to be). `video.Metadata.
  HasContainerMetadata()` guards the lot: a probe that read no container writes **nothing**, so a second
  failure cannot "correct" a duration to unknown and sound to silence — it is logged instead. A gone original
  and a host with no `ffprobe`/`exiftool` are logged skips (neither is fixable by a retry, and dead-lettering
  them would make a library-wide run look broken); every other probe failure is returned → the queue retries.
  Video: `image_codec` still stays empty — a clip's compression is `video_codec`, which the re-probe owns.
  The **backfill** `BackfillMetadata(ctx,all) (int,error)` (the basis of `POST /process/metadata`):
  it enqueues a `metadata` job for every photo whose file has **never been read**
  (`PhotoLister.ListPhotosMissingFileMetadata` = `metadata_extracted_at IS NULL`), or — when `all` —
  for every non-archived one (`ListActiveUIDs`, a forced re-read of the whole library, which is how the
  fields a new extractor has learned to read get caught up); enqueue via `Enqueuer.EnqueueMetadata` (a dedup no-op),
  returns the count; `ErrBackfillUnavailable` when the `Service` had no `Lister`/`Enqueuer`. **It converges and is
  resumable**: the marker is stamped the moment the job finishes, so an interrupted run picks up exactly where
  it stopped, and a second run over an exhausted library enqueues **zero** jobs — even for a photo whose file
  has no IPTC tags at all ("we looked and there was nothing there" is a finished photo, not a pending one).
  **`BackfillVideoMetadata(ctx) (int,error)`** (the basis of `POST /process/metadata?videos=true`) is the
  repair route for a library that ingested clips while its probe was broken: it enqueues a `metadata` job for
  every video in `PhotoLister.ListVideosMissingTechnicalMetadata` — which the backfill above cannot reach,
  because those rows carry a `metadata_extracted_at` — and it has **no `all` scope** of its own, since
  re-reading every video is what `BackfillMetadata(all)` already does. Same guarantees: enqueue-only,
  idempotent, resumable, and a repaired clip drops out of the candidate set),
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
  layout under its own prefix — `<prefix>storyboard/<aa>/<bb>/<cc>/<hash>_sb.jpg` (`RelPath(prefix,hash)`, the
  prefix being that of the video's own original), `CacheSubdir` exported for the wipe —
  and deliberately **never uploaded to the object store**: it adds no bucket prefix and nothing to the
  orphan-sweep contract, and a pruned cache costs one background job. `Generator` = `New(storage,cacheDir)` with
  `Path`/`Exists`/`Open`/`Remove` (each `(prefix,hash)`)/`Generate(ctx,hash,srcRelPath,spec)` (prefix from
  `srcRelPath`); `Generate` is **idempotent** (a cached
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
  most videos are never watched. Every read path a request drives takes the project —
  `Status`/`Open`/`FileHash`/`Regenerate(ctx,projectUID,uid)` — and loads the photo through
  `PhotoStore.GetByUIDInProject`, so a foreign clip is `photos.ErrPhotoNotFound` even for a handler that
  forgot its own project check; only the job's `Generate(uid)` reads unscoped (`GetForJob`). `Status` answers `StateReady` (+ the `Spec`), `StatePending` — and
  schedules the job, the queue's per-photo dedup absorbing every later ask — or `StateUnavailable`, which is
  permanent and tells the client to stop: a still, a **live photo** (its motion clip is a hover preview with no
  scrubbable timeline, so a storyboard would be a decode spent on a control that is never shown), a clip of
  unknown length, a wiring with no `Enqueuer`, or a host with no ffmpeg (`FFmpegAvailable`, injectable so tests
  do not depend on the machine). `Open` yields the sprite bytes + `Spec`, `FileHash` the ETag
  source. `Handle` = `worker.HandlerFunc` (payload `{photo_uid}`, empty → `ErrMissingPhotoUID` dead-letter),
  registered in `serve` on `jobs.TypeStoryboard`; `Generate(uid)` renders one, and a photo that **cannot** have a
  storyboard is a quiet no-op rather than a dead-letter — the job was scheduled from a state that no longer holds.
  `Regenerate` is the **forced** rebuild behind `POST /photos/{uid}/regenerate-storyboard`: it removes the
  cached sprite and *then* enqueues, in that order, because the queue dedups per photo and a job scheduled first
  could claim the work while the stale sprite is still there and take `Generate`'s idempotent skip. It refuses
  rather than deletes when nothing here could render a replacement (`ErrCannotRender`, no enqueuer or no ffmpeg):
  a clip with a stale preview is strictly better off than one with none. `ErrNotAVideo` / a wrapped
  `storyboard.ErrNoDuration` for a photo that can never have one, `photos.ErrPhotoNotFound` for an unknown uid.
  All behind `PhotoStore`/`Generator`/`Enqueuer` → unit-testable with no ffmpeg and no disk),
  `internal/hls/`
  (the **object layout of a video's HLS renditions**, and nothing else — no ffmpeg, no store, no database, so
  every layer that touches streaming (the encoder, the purge, the library wipe, the HTTP endpoints) agrees on
  the same strings without depending on each other. Segments live under
  `<storagePrefix>hls/<file_hash>/<rendition>/`, `storagePrefix` being `storekeys.PrefixOf(photo.FilePath)`
  (`Prefix` = `hls`, `PrefixFor(storagePrefix,fileHash)` = `<storagePrefix>hls/<hash>/` **with** the trailing slash, so a prefix listing
  cannot spill into a hash that merely starts the same), holding the fMP4 init segment `InitName` =
  `init.mp4` and zero-padded five-digit media segments `00000.m4s`, `00001.m4s`, …; rendition names are
  lowercase alphanumeric, `Rendition1080p` = `1080p` being the first. **Playlists are never stored** — the
  `.m3u8` is generated per request, a stored one being a second source of truth that goes stale the moment a
  rendition changes. `Key(storagePrefix,fileHash,rendition,name)` builds the key and validates all four parts →
  `storekeys.ErrInvalidPrefix`, `ErrInvalidHash` (lowercase hex, 6..64), `ErrInvalidRendition` (`^[a-z0-9]+$`, ≤ 16) or `ErrInvalidName`;
  `ValidateName` accepts **only** `init.mp4` or `^[0-9]{5}\.m4s$`, which is what stands between a
  client-supplied path segment and an object key — traversal, an empty name, wrong padding, a stray second
  extension, an uppercase variant and a playlist name are all refused, and the fixed padding is also what
  makes the keys sort in playback order. `MIMEFor(name)` gives an object its media type by name —
  `InitMIME` = `video/mp4`, `SegmentMIME` = `video/iso.segment` — decided rather than sniffed (a fMP4
  segment's leading box is not something content sniffing recognises as video at all), and it lives here
  because everything that moves these objects (the encoder writing them, the storage migration
  re-publishing them elsewhere) must serve them as the same type.
  It also holds the **encoding plan** and the **playlist rendering** — pure functions, no ffmpeg ever
  executed, so all of it is testable on a host without ffmpeg. A `Rendition` is a name + the **square** box
  its picture is fitted into (`MaxDimension`) + the video bitrate ceiling + its RFC 6381 `Codecs`;
  `All()` (a copy) and `ByName` read the list, today `{1080p, 1920, 6 Mbit/s, avc1.640028,mp4a.40.2}` —
  a list because adding 720p must be an entry plus data, never a rewrite. The box is square, not 1920x1080,
  so a portrait clip is capped on its long edge like a landscape one instead of at a quarter of the pixels.
  `Bandwidth()` = ceiling + `AudioBitrate` (128 kbit/s), the *peak* a master playlist's BANDWIDTH means.
  `ScaleFilter()` = `scale=w=min(1920\,iw):h=min(1920\,ih):force_original_aspect_ratio=decrease:force_divisible_by=2`
  — the `min` with `iw`/`ih` is what stops a small source being blown up to fill the box, the commas are
  escaped because the string is a filtergraph, and it is left an **expression** rather than concrete numbers
  because ffmpeg applies a source's rotation before the filter runs. `Fit(w,h)` performs the same arithmetic
  in Go (nearest even inside the fit, then floored to even) for the plan to be reasoned about and tested;
  measured against ffmpeg 6.1.1: 3840x2160 → 1920x1080, 2160x3840 → 1080x1920, 640x480 unchanged,
  1279x721 → 1278x720, 1921x1000 → 1920x1000. `EncodeArgs(src,outDir,rendition,segmentSeconds)` is the ffmpeg argument list —
  **the quality decision**: H.264 `high` in `yuv420p` (what every browser and iOS device decodes), preset
  `medium` (worth it for output encoded once and served many times, unlike `internal/video`'s throwaway
  `veryfast` transcode), CRF 21 with `-maxrate` at the ceiling and `-bufsize` twice it, AAC-LC stereo at
  128 kbit/s, `-force_key_frames expr:gte(t,n_forced*<segment>)` so segments are cut where they were planned,
  `-f hls -hls_time <segment> -hls_playlist_type vod -hls_segment_type fmp4 -hls_list_size 0 -hls_flags
  independent_segments`, `init.mp4` + `%05d.m4s` + the scratch playlist `EncodePlaylistName` = `index.m3u8`
  in `outDir` (scratch: read, rewritten, thrown away — `ValidateName` refuses it). Audio is mapped `0:a?`
  so a silent clip still encodes; the builder creates no directory, probes nothing and reads no clock.
  The segment length is a **parameter**, not a constant: `SegmentLength(configured)` resolves ≤ 0 to
  `DefaultSegmentSeconds` = 6 in one place, so `video.hls.segment_seconds` left unset still asks for a
  playable encode, and the value reaches both `-hls_time` and the forced keyframes — they have to agree or a
  segment does not start on a keyframe.
  `RewriteMedia(playlist, SegmentURL)` returns what ffmpeg wrote with **only** the segment URIs and the
  `EXT-X-MAP` URI replaced by the caller's URLs — every other byte, line endings included, passes through
  untouched, because ffmpeg has already measured the `EXTINF`s and a synthesised duration would drift from
  the segments a player receives. The resolver is handed the URI's **base name**, validated by `ValidateName`
  first (how ffmpeg spells the path in front of it depends on flags no URL builder should know about);
  errors: `ErrNoSegmentURL`, `ErrMalformedPlaylist` (an `EXT-X-MAP` with no quoted URI), `ErrInvalidName`,
  or the resolver's own, wrapped — never a playlist with a hole in it. `BuildMaster([]Variant)` renders the
  master (`#EXT-X-VERSION:7`, `EXT-X-INDEPENDENT-SEGMENTS`, one `EXT-X-STREAM-INF` with
  `BANDWIDTH`/`RESOLUTION`/`CODECS` + the caller's URL per variant, in the given order = preference order),
  refusing an empty list (`ErrNoVariants`) or a variant missing URL/bandwidth/resolution/codecs
  (`ErrInvalidVariant`). Both playlist functions are held to golden files in `testdata/`, the media one's
  input being a playlist real ffmpeg wrote),
  `internal/hlsjob/`
  (**the `hls_transcode` job** — the acting half of streaming to `internal/hls`'s pure one: it runs ffmpeg over
  an uploaded video, publishes the segments and records what the player's playlists must advertise. `Service` =
  `New(Config{Photos,Objects,Renditions,Lister,Enqueuer,Moves,Plan,SegmentSeconds,TempDir,FFmpegAvailable,Metrics})`
  (panics on a nil Photos/Objects/Renditions; the `Plan` is resolved from `video.hls.renditions` by
  `cmd/kukatko/hls.go:hlsPlan`, an unknown name failing **startup** rather than silently producing a library
  with a missing quality level). **During a storage move**: with `Moves` (`*rekey.Gate`) a video a running
  move has yet to take is deferred before anything is encoded (`ErrAwaitingMove`); after the encode,
  `publishCurrent` publishes under the move's photo lock (`MoveGate.Publish`: re-read, segment upload and
  row, so the move never copies an old encode's segments over fresh ones; a video the move holds is deferred,
  `ErrPrefixMoved` wrapping `storekeys.ErrMoving`), re-reading the photo's key immediately before the publish
  and once more before the row — a prefix that changed in between (only possible without a gate) removes
  what went under the stale prefix and re-publishes the same segments under the current one (at most
  `maxPublishAttempts`, then `ErrPrefixMoved` deferred), so no row is ever written for a prefix the video
  left; `Handle` stretches a move deferral with the job's age to `MaxMoveRetryDelay` (`backOff`,
  `move_test.go`). `Handle` = `worker.HandlerFunc` (payload `{photo_uid}`, empty →
  `ErrMissingPhotoUID` dead-letter); `Transcode(ctx,uid)` encodes one photo into every rendition of the plan.
  **A photo that is not a standalone video is a quiet no-op**, live photos included — their motion clip is a
  hover preview nobody streams — because the job was scheduled from a state that may have changed; an empty
  plan is `ErrNoRenditions` and a host with no ffmpeg a wrapped `video.ErrFFmpegMissing`.
  One rendition: `os.MkdirTemp(TempDir)` → ffmpeg (`hls.EncodeArgs`) → read the scratch `index.m3u8` →
  `parsePlaylist` → probe → publish → `Store.Save`. **`parsePlaylist` is what the run believes**, not a
  directory listing: ffmpeg's own statement of what it produced and in which order, so a stray file cannot
  become a segment and the tail of an interrupted run is not published as part of the rendition. It takes the
  `EXT-X-MAP` URI, the segment URIs (each reduced to its base name and put through `hls.ValidateName`) and the
  total duration **summed from the `EXTINF`s** — every deviation is `ErrUnusablePlaylist`: no init segment, no
  segments, a segment with no or an unreadable duration, a name the layout cannot hold. The picture's real
  dimensions are read off `init.mp4` with `video.Probe` (its sample description answers without a media segment
  being read) rather than predicted with `hls.Fit`, because ffmpeg applies a container's rotation **before** the
  scale filter and a portrait clip stored as a rotated landscape one would be described as a picture nobody
  encoded. Each object is digested and sized first and then streamed (`storage.Storage.Put` verifies against the
  declared identity), and stamped by name — `video/mp4` for the init segment, `video/iso.segment` for a media
  one — because sniffing does not recognise an fMP4 box as video at all.
  **Ordering and cleanup are the contract:** publish first, write the row last (a row is a promise the objects
  can be fetched); on any failure the keys **this run created** are deleted again, on success the ones a
  previous encode left that this one did not produce are swept. Both diffs come from one `KeysWithPrefix`
  listing taken before the upload (`existingKeys`, a `storage.PrefixLister` type assertion — a store without it
  degrades, never fails), which is what keeps a failed re-encode from deleting the objects the previous,
  still-valid row points at. The **timeout scales with the clip** (`encodeTimeout`: 5 min + 10× its length,
  4 h when the length is unknown) — a constant generous enough for a two-hour video is no protection for a
  ten-second one, and a tight one kills every long video at the same point, which looks exactly like a clip
  that "cannot be transcoded". The job has a **one-slot pool** (`internal/worker`), so a batch of uploaded
  videos serialises instead of taking the machine.
  **What an encode cost is reported through `Observer`** (`observe.go`: `ObserveRenditionEncode(rendition,
  outcome,d,written)` + `ObserveEncodedSource(d)`, satisfied by `*metrics.Registry`, wired as `Config.Metrics`
  by `cmd/kukatko/hls.go`; nil = `nopObserver`, so the encode behaves identically with metrics off and this
  package never imports `internal/metrics`). The grain is the **rendition**, because a job encodes one video
  into every quality and those cost wildly different amounts — `encodeOne` times the whole of
  `encodeRendition` (ffmpeg + uploads + the row), labels it `success`/`error` so the hours a repeatedly failing
  encode burns are visible, and reports the bytes `publish` uploaded (for a failure, the part that landed
  before it broke and was removed again). The **footage** is counted once per video, after the whole plan is
  through, so `duration_sum / source_seconds_total` is the pipeline's cost per second of video rather than one
  quality's; a run that failed part-way counts none, since the retry will put the same minutes through again,
  and a clip whose length the catalogue never recorded counts none either (`clipLength`).
  `Store` (`NewStore(pool)`) is the `photo_hls_renditions` access layer: `Save` (upsert on
  `(photo_uid,rendition)` — what makes a re-encode replace rather than duplicate — returning the row with its
  `encoded_at`), `Get` (→ `ErrRenditionNotFound`), `ListForPhoto` (**widest picture first**, the order a
  master playlist wants, empty slice for a video not encoded yet) and the two existence checks the serving
  routes run on the request path, `Has(uid,rendition)` and `HasAny(uid)` — `EXISTS` queries that read no
  column, because the playlist of a two-hour video is tens of kilobytes and fetching it once per fragment
  would be the most expensive part of streaming the clip.
  `inventory.go` adds what a whole-library reconciliation needs (`internal/maintenance`'s streaming check):
  `ListRecorded` → `[]Recorded{PhotoUID,Rendition,FileHash,FilePath}`, every row joined with the hash and the
  original's key, which together name its prefix
  in the store and **without the playlist column**, since none of it is needed to ask whether the objects are
  still there; `Delete(uid,rendition)` withdraws one row, reporting whether there was one (a second call is a
  no-op, so a repair converges); and `EncodingHashes` → the file hashes of the videos with an unfinished
  `hls_transcode` job (`queued`/`running`/`failed` — `done` is finished and `dead` will never run again).
  That last one exists because `encodeOne` publishes before it records: between those two moments a healthy
  job's uploads look exactly like abandoned objects, so anything reasoning about "objects no rendition claims"
  has to subtract these hashes or it will offer to delete what a job is still writing.
  `BackfillHLS(ctx, all)` (optional `Lister` = `*photos.Store` + `Enqueuer` = `*jobs.Enqueuer`, either
  missing → `ErrBackfillUnavailable`) is the `POST /process/hls` half: it enqueues an `hls_transcode` job per
  video with **no rendition row at all** (`photos.ListVideosMissingHLS`) or, with `all`, per non-archived
  video (`photos.ListActiveVideoUIDs`). The predicate is "no row", not "fewer rows than the plan": a
  rendition list is an instance's configuration, not a property of the catalogue, so counting against it
  would re-schedule the library whenever a quality level is added — which is what `all` is for. It stops at
  the first enqueue failure reporting how many had landed, since those jobs are already in the queue. The
  integration tests synthesize a clip with
  ffmpeg at test time, encode it end to end and assert both halves — the objects in the store *and* the row —
  plus that a re-run leaves one row, moved forward in time, the same object set, and sweeps a segment planted
  as an earlier encode's leftover, and that the instrumentation reports every rendition of the plan once, as a
  success, with the bytes its prefix really holds and the clip's length counted once),
  `internal/maintenanceapi/`
  (a maintainer-only HTTP API over maintenance: the interfaces `Service` (Scan+Repair, satisfied by `*maintenance.Service`,
  nil → 503) and `AuditPurger` (`PurgeOlderThan`+`Record`, satisfied by `*audit.Store`, nil → 503);
  plus `NamelessRepair` (`List`/`Snapshot`/`EnqueueDetach`/`EnqueueRestore`, satisfied by
  `*namelessjob.Service`, nil → 503), every call made in the request's project (`auth.ProjectFromContext`
  through the swappable `projectOf`; the three nameless routes are mounted by `RegisterProjectRoutes` under
  `/p/{project}` (`projectAPI.maintenance`), the scan/repair/purge by `RegisterInstanceRoutes` once — the
  CLI's `--project` reaches the same repair; a request without a project → 500, an undo file of another
  project → 400);
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
  fields **allowed** — an old file must still replay — refused with `400 {error, code}`: `undo_invalid`
  unparsable, `undo_empty` subject-less, `undo_wrong_project` another project's file; `writeCodedError`) →
  `202 {queued}`;
  mounted in `serve` (`buildMaintenanceAPI` in `cmd/kukatko/maintenance.go` injects `audit.NewStore`,
  the service is built by `buildMaintenanceAndThumb`, shared with the registration of the `thumbnail` handler in `buildJobs`)),
  `internal/namelessjob/`
  (**the nameless-subject repair as background work**: the `Undo` file format (`{project_uid,project_slug,
  subjects:[people.SubjectSnapshot]}`, shared verbatim with the CLI's `--undo-file`/`--undo`, so a file crosses
  freely between the two; a file from before projects carries no project and belongs to the bootstrap one,
  `Undo.Project`/`CheckProject` → `ErrProjectMismatch` in any other), the `Service`
  the admin surface drives (`List` — read-only report; `Snapshot` — the undo of every nameless subject via
  `people.SnapshotSubject`, skipping one that vanished mid-read; `EnqueueDetach`/`EnqueueRestore` — one job per
  subject, the scheduling maintainer's `audit.Meta` carried in the payload so the job's audit row names them),
  and the two handlers `HandleDetach`/`HandleRestore` over `people.DetachSubject`/`RestoreSubject`.
  **Why the queue and not the request:** detaching production's catch-all sets `subject_uid` NULL on ~100k
  faces, moving every one into the partial `WHERE subject_uid IS NULL` HNSW index of migration 0047 — minutes of
  index maintenance no HTTP request may sit on. `HandleDetach` treats `ErrSubjectNotFound` as done (a retry, a
  double-click or the CLI having got there first all mean the repair happened) and **warns** when it detached
  more than the delivered undo file recorded, since replaying that file would leave the difference unassigned.
  Registered unconditionally in `buildRegistry` — the repair for an importer artefact must not depend on an
  optional feature being on. **Per project:** each project may have its own catch-all; `List`/`Snapshot`/
  `EnqueueDetach`/`EnqueueRestore` take the project (`ErrNoProject` without one, `ErrProjectMismatch` for an
  undo file of another project, nothing queued), both payloads carry `project_uid` (the restore one also a
  top-level `subject_uid`) and the handlers run in it — a payload queued before the field existed in the
  bootstrap project — stamping it on the audit entry; the dedup key is (type, project, subject)
  (`idx_jobs_nameless_dedup`, migration 0105, queued jobs only), so the same detach or restore queued twice
  collapses and is not counted again),
  `internal/duplicates/`
  (**a review surface for near-duplicate photos** beyond the upload-time warning: it links photos by two
  signals — pHash Hamming distance up to `duplicate.phash_max_diff` and embedding cosine distance
  up to `duplicate.embedding_max_dist` — and merges the edges into connected components via union-find
  (`algo.go` disjoint-set + path compression/union by rank); **without O(n²)**: pHash through **banded-LSH**
  buckets (`bandCount`=`maxDiff+1` bands, which by the pigeonhole principle guarantees a shared bucket for pairs within the threshold,
  and the candidates are verified by the full Hamming distance), embeddings through HNSW (`vectors.FindDuplicatePairs`).
  All behind the interfaces `PhotoSource` (`ListByUIDsInProject`)/`PhashSource` (`ListActivePhashes`)/`EmbeddingSource`
  (`FindDuplicatePairs`, nil turns embedding grouping off)/`FeedbackStore` (`DismissedDuplicatePairs` +
  `ConfirmedDuplicatePairs`, both called with the project being scanned; nil leaves every edge in place and
  marks nothing) → unit-testable with fakes; `Service` =
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
  **A group never mixes stills and videos.** `ListActivePhashes` carries each hash's `media_type`, `addPhashes`
  records `MediaType.Kind()` per node, and `graph.linkable(a,b)` — "not dismissed **and** the same kind" — is
  what both linking steps ask before drawing an edge (`phashUnion`/`unionBucket` take it as a predicate rather
  than a set, because "not the same kind of media" is a property of the pair and materialising it would mean
  listing every cross-kind pair in the catalogue). **Why an edge filter and not a filter over finished groups:**
  the union-find has no "remove edge", so a cross-kind link would drag the clip and the photographs into one
  component that cannot afterwards be taken apart honestly — the test
  `TestFindGroups_videoDoesNotJoinAStillGroup` pins that. **What a video contributes** is one frame: its pHash
  *and* its embedding are computed from the poster frame grabbed a second in (`internal/thumbjob`,
  `internal/embedjob`), so to both signals a clip opening on the room a photograph was taken in *is* that
  photograph — and resolving the group would archive two minutes of footage in favour of one frame of it.
  **What it does not contribute** is any evidence that it is a video, which is why the boundary is drawn rather
  than a threshold tightened. **Two videos still pair**, and that is the conservative option rather than a
  confident one: one frame each is weak evidence, and two clips sharing a dark intro, a slate or an afternoon
  can pair on nothing more. Judging clips properly means several frames per clip or the audio — a heavier
  comparison, deliberately not invented here; until it exists a video pair is a suggestion to look at, the
  compare screen says what the candidates are, and `dupmerge` guards the archive. A live photo is a still
  (`photos.MediaKind`), and an empty `media_type` — a row from before the column was read — reads as a still,
  so nothing that used to group stops grouping;
  **Project scope: groups are built within one project.** `FindGroups`/`CountGroups` take `projectUID` after
  `ctx` (`ErrNoProject` without one) and hand it to every source: the pHash candidates
  (`photos.ListActivePhashes(ctx, projectUID)`), the embedding neighbours
  (`vectors.FindDuplicatePairs(ctx, projectUID, …)`, the project predicate inside the HNSW lateral scan) and
  the member hydration (`ListByUIDsInProject`), so the same picture uploaded to two projects is never a pair;
  the graph's nodes are the project's hashed photos, so a feedback pair naming another project's uid is
  ignored by construction. `TestService_groupsStayInsideTheirProject` gives four photos of two projects the
  identical hash *and* embedding and asserts each project sees one group of its own two;
  **`FindGroups(ctx,projectUID,limit,offset)`** (backing `GET /duplicates`) → `Result{Groups,Total,Limit,Offset,
  NextOffset}`; each `Group{ID (the smallest uid),Reason (phash/embedding/both),KeeperUID,Confirmed,Members}`,
  a `Member` carries dimensions/size/`taken_at`/`media_type`/`duration_ms` (the clip length, videos only —
  the compare screen and the listing say what a candidate is *before* the keep-or-archive answer) + `is_keeper`
  + `phash_distance`/`embedding_distance` to the keeper; the **proposed keeper** = the highest resolution → the largest file →
  the oldest → the smallest uid (`selectKeeperIndex`); **`Confirmed`** is set by `graph.addConfirmations` when any
  pair inside the component carries a `feedback` confirmation ("yes, the same shot", written by the review game's
  duplicate check) — unlike a dismissal it suppresses no edge and removes nothing, one confirmed pair marks the
  whole component (the curator answered about the group they were shown), and `sortGroups` puts confirmed groups
  **first**: confirmation is the only ordering key here that is not a guess, so it outranks size. Groups then
  ordered largest-first/newest-keeper/id,
  `limit` clamped to `[1,100]`; **`CountGroups(ctx,projectUID)`** = the same scan reduced to `Result.Total` (detection is
  derived state, so there is no cheaper way to count it) — which is why its one caller, the admin dashboard's
  duplicates tile, runs it in the background and never on a request (`system.DuplicateScan`, once per active
  project — the detector satisfies `system.DuplicateCounter` directly);
  it only reads, **never mutates** (resolution goes through `dupmerge`); archived
  photos are not scanned (`ListActivePhashes` filters `archived_at IS NULL`)), `internal/dupmerge/`
  (**the transactional merge of a near-duplicate group into the keeper** — the mutating counterpart of the read-only `duplicates`;
  `Service=NewService(pool)`, `Merge(ctx,Input{ProjectUID,KeeperUID,MemberUIDs,ActorUID})→Result{albums_added,
  labels_added,people_added,metadata_filled[],archived,dry_run}` and `Preview` (a dry run, tx rollback).
  In **one `pgx.Tx`** (like `bulk` — the audited store methods open their own tx and cannot be composed)
  it computes a `plan`: the union of the copies' albums/labels/people minus what the keeper already has, `pickFill` of the missing scalars
  (title/description from the photo + the actor's per-user favorite/rating/flag, **never overwriting** an existing
  value), and the active copies to archive; it applies raw SQL (`INSERT … ON CONFLICT DO NOTHING`, a person =
  a box-less `person` marker with a generated `mk…` uid — the same row a hand attach writes, since a face
  marker's box is pixel-specific to the copy it came from; a new marker has no `faces` row, no cache needed,
  and the `ON CONFLICT` on migration 0071's partial unique index makes a concurrent attach harmless),
  archives (with an `archived_at IS NULL` guard) and writes `audit.ActionPhotosMerge`.
  **Project scope:** before anything else `buildPlan` proves the **whole group** is the project's photos
  (`requireGroupInProject`: one `count(*) … WHERE uid = ANY($1) AND project_id = $2` over the distinct
  members), so a group mixing two projects — or naming a photo that does not exist — is refused whole with
  `ErrPhotoNotFound` (no uid in it) before any association of another project is read, let alone copied; the
  member load is scoped too, and the audit entry carries the project. An already-archived copy still counts
  (it is still the project's photo), so the idempotent re-run holds.
  **`checkSameKind` refuses to archive across the still/video boundary** (`ErrCrossKindGroup`, → 400): `loadPhotos`
  reads `media_type`, and a copy the plan would archive whose `photoRow.kind()` differs from the keeper's fails the
  whole merge before anything is written. It is a **guard, not a convention** — detection no longer offers such a
  group, but the endpoint takes its membership from the caller, so a hand-written request or a stale client can
  still ask for one, and quietly archiving a clip in favour of a still is the one outcome that loses data here.
  Only the copies **actually being archived** are checked: an already-archived copy is not being acted on, and
  erring over one would break the idempotent re-run of a resolved group. A copy this call actually
  archived also leaves its stack via `photos.LeaveStackTx` in the same tx (skipped for an already-archived
  copy, which left its stack back then), so archiving a copy that happened to be a stack's primary does not
  hide that stack's still-live members. **An empty plan = a no-op**
  (it writes nothing → an idempotent re-run on a resolved group); validation `ErrNoProject`/`ErrNoKeeper`/
  `ErrTooFewMembers`/`ErrKeeperNotInGroup`/`ErrPhotoNotFound`/`ErrKeeperNotFound`/`ErrCrossKindGroup`), `internal/duplicatesapi/`
  (an editor/admin HTTP API over duplicate detection and resolution: the interfaces `Service` (`FindGroups`, satisfied by
  `*duplicates.Service`, **nil → 503**) and `MergeService` (`Merge`/`Preview`, satisfied by `*dupmerge.Service`,
  **nil → 503**); `NewAPI(Config{Service,Merge,RequireWrite,RateLimit})`+`RegisterRoutes` mounts `GET /duplicates`
  and `POST /duplicates/merge` behind `RequireWrite`, the listing then behind the heavy vector throttle
  `RateLimit` (inside the guard; nil = none — every page is a whole-project scan) (listing: `limit`≤100/`offset`, invalid → 400, a failed scan
  → 500; merge: a bad group → 400 — including one mixing a video with a still (`ErrCrossKindGroup`), whose
  reason is passed through to the body — a member outside the request's project, the keeper included,
  foreign or missing alike → **404 `{"error":"photo not found"}`**, one body naming no uid, nothing
  changed; the actor from `auth.UserFromContext`, the project from `auth.ProjectUIDFromContext` for both the
  listing and the merge; `isolation_integration_test.go` drives B's listing past A's identical hashes and a
  mixed-group merge against `projecttest.AssertSameShape`);
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
  (`DismissedDuplicateMarkerGroups(ctx, projectUID)`, the project's dismissals only, satisfied by
  `*feedback.Store`) →
  `Service = New(Config{Markers,Dismissals})`, **`FindGroups(ctx,projectUID,limit,offset)`** (the project's
  photos only — `ListRepeatedMarkers(ctx, projectUID)` filters `p.project_id`; `ErrNoProject` without one) →
  `Result{Groups,Total,Limit,Offset,NextOffset}`; a `Group{PhotoUID,PhotoTitle,TakenAt,Width,Height,
  Orientation,SubjectUID,SubjectName,Markers[]{uid,bbox,score,reviewed}}` with the markers ordered **left to
  right** so a client's numbering reads in reading order, `limit` clamped to `[1,200]` (default 50).
  **`GroupMarkers(rows,dismissed)` is exported and pure**: it keeps only valid (`invalid=false`) `face` markers of
  **named** subjects, groups by (photo, subject), drops the dismissed groups and the ones left under
  `minGroupSize`=2, and orders most-markers-first → by name → by uid. The SQL (`store.go`) applies the same
  predicates plus a `COUNT(*) OVER (PARTITION BY photo_uid, subject_uid) > 1` window so Postgres ships a few
  dozen rows instead of the whole catalogue — an optimisation, **not** the rule, which lives in Go where it is
  unit-tested. The nameless catch-all subject is excluded (it holds thousands of untagged regions and would bury
  every real finding, cf. the ~7k photos in `docs/API.md`); archived photos are excluded, **non-primary stack
  members are not** — a RAW sibling with the same person twice is the same mistake),
  `internal/dupmarkersapi/`
  (the HTTP surface of the repeated-marker review, every route inside the request's project — the listing
  passes `auth.ProjectUIDFromContext`, the repairs read and write markers through the project-scoped people
  store: `Service` (`FindGroups`, **nil → 503**)/`MarkerStore`
  (`ListMarkersByPhoto`/`GetMarkerByUID`/`SetMarkerInvalidAudited`, satisfied by `*people.Store`)/`Assigner`
  (`Apply`, satisfied by `*facematch.Service`); `NewAPI(Config{Service,Markers,Assigner,RequireAuth,
  RequireCurator})`+`RegisterRoutes` mounts `GET /duplicate-markers` behind **RequireAuth** (reading is not a write)
  and `POST /duplicate-markers/keep` + `POST /duplicate-markers/invalid` behind `RequireCurator`.
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
  a fake in unit tests: `ListStackCandidates`/`StackInfoByUIDs`/`CreateStackAudited`/`CreateStacksAudited`/
  `SetStackPrimaryAudited`/`UnstackMemberAudited`/`UnstackAllAudited`); `Service = New(store,cfg)` (panics on a
  nil store). **Every mutating method takes the `audit.Entry` its change is recorded with** and only carries it
  through — who acted is the caller's knowledge, the stack uid and the members are the store's, and the store
  writes the entry in the mutation's own transaction:
  **Project scope:** every `Store` method takes the project, so the candidates and a selection are read from
  that project only and every write proves its photos belong to it — **a stack never contains photos of two
  projects** (asserted in the store: `requirePhotosInProject`/`requirePlansInProject` in the write's own
  transaction). **`DetectStacks(ctx,projectUID,entry) (created,error)`** (backing `POST /process/stacks`, which
  passes the request's project) groups the project's **not-yet-stacked non-archived** photos by the enabled
  rules — synchronous, incremental and **idempotent**: a re-run over a
  settled library creates nothing and does not touch an existing/manual stack; a no-op (0) when the feature or
  every rule is off, and then it records nothing either, because nothing ran. It decides the whole pass as pure
  `photos.StackPlan`s (`planComponent`) and hands them to `CreateStacksAudited`, so the stacks and the single
  `stacks.detect` entry share **one** transaction; **`StackSelection(ctx,projectUID,uids,entry)`** groups a selection
  manually (`photos.ErrStackTooSmall` below 2 distinct, `photos.ErrPhotoNotFound` when one is
  missing/archived), **`SetPrimary`/`Unstack`/`UnstackWhole`** (each `(ctx,uid,entry)`) delegate to the store;
  **pure detection** (`rules.go`): four independently switchable rules
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
  (`jobs.Store.CountsByTypeState`/`CountPending`)/`ImportLister` (`importer.Store.LatestRun(projectUID,source)`)/
  `ProjectLister` (`project.Store.List`; nil = no per-project import or duplicate reads)/
  `BackupReporter` (`backup.Service.Status`, **nil = not configured**)/`MapsReporter`
  (`mapy.Health.Snapshot`, **nil = no mapy.com key**)/`GeocodeReporter`
  (`placesjob.WindowBudget.Snapshot`, **nil = no mapy.com key**)/`DashboardCounter` (`Store.CountDashboard`)/
  `DuplicateCounter` (`CountGroups(ctx,projectUID)` = `duplicates.Service`; **nil = detection off**) →
  unit-testable with fakes
  without a DB; `Service` = `New(Config{DB,Embeddings,EmbeddingURL,Jobs,Backup,Maps,Geocode,Imports,Projects,Library,Charts,
  Dashboard,Duplicates,StreamingEnabled,OriginalsPath,CachePath,StorageTTL,LibraryTTL,ChartsTTL,
  DashboardTTL,DuplicateTTL,DuplicateTimeout,Clock})`; **`Collect(ctx) (Status,error)`** gathers
  `Status{Version,Database,
  Embeddings,Jobs,Backup,Imports,Storage,Maps,Geocode,Library,Remaining,Video,Projects}`. **Every count is an instance
  total; `Projects` is how it divides** (`projects.go`): one `ProjectSummary{UID,Slug,Name,Active,Photos,Videos,Trashed,
  OriginalsBytes,Faces,Subjects,Albums,Queue{Queued,Running,Failed},Duplicates,LastImport}` per project (deactivated
  ones included, slug order), counted by `projectsBreakdownSQL` — a `jsonb_agg` column of the **same** dashboard
  statement (one grouped pass per root table joined onto `projects`, so the dashboard stays one round trip; a job is
  attributed through its payload's `photo_uid` or `project_uid`, a job with neither is nobody's), then stamped per
  request on a **copy** of the memoised rows (`fillProjects`): `LastImport` = that project's `LatestRun(folder)`,
  `Duplicates` = its share of the background scan (`asyncCache[map[projectUID]int]`, active projects only, so a
  deactivated project's stays nil). `Imports.Folder` is the newest of the projects' last imports and
  `Remaining.Duplicates.Groups` the scan's sum. Embeddings online/offline, the queue
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
  best-effort; **`LatestRuns(ctx)`** → the newest run (across the active projects) of **every** `importer.AllSources()`
  keyed by source
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
  PhotosWithoutOCR,DuplicateMarkers,VideosWithoutStreaming,Duplicates}`, cheap SQL throughout (the OCR predicate matches
  `idx_photos_ocr_pending` exactly; `DuplicateMarkers` mirrors `dupmarkers.GroupMarkers` — valid face markers of a
  named subject on a live photo, grouped by (photo,subject), minus the dismissed pairs) **except**
  `Duplicates`: `DuplicateScan{Configured,Available,Groups,ComputedAt}` comes from `asyncCache[int]`
  (`async.go`), which never computes on the request path — a read returns the last finished scan and schedules
  the next one in the background under its own timeout (`defaultDuplicateTTL` 15 min /
  `defaultDuplicateTimeout` 2 min, a failure keeping the last good value and backing off a whole TTL), because the
  near-duplicate scan is the one aggregation here that walks the HNSW index; `Available:false` means "no answer
  yet", not zero groups, and a failed scan never fails the snapshot;
  the same round trip also answers **how the streaming encode is going**, counted over *videos* rather than
  over jobs (the queue's `hls_transcode` row is a lifetime tally of jobs and cannot say how much of the
  library still plays as the whole original file):
  `Video{StreamingEnabled,Videos,Streamable,Missing,EncodeQueued,EncodeRunning,EncodeFailed,NotScheduled,
  Renditions,OldestQueuedAt}` — `Streamable` = has a `photo_hls_renditions` row, and the four states of the
  rest are **disjoint and sum to `Missing`**, a video being classified by the furthest its jobs got (running
  > queued > failed/dead > finished) so an old `done` row cannot hide the encode running now;
  `NotScheduled` (nothing queued, nothing failed) is the state that stalls silently and the reason the
  section exists, `OldestQueuedAt` says whether the queue is moving at all, and `Renditions` deliberately
  comes **without a byte size** — segment sizes are recorded nowhere, so the only honest answer would be a
  listing of the whole `hls/` prefix. `StreamingEnabled` is `video.hls.enabled` stamped by `Collect` (like
  `DerivedBytes`, it is not a count): with it false nothing is ever enqueued, so `Missing` is every video and
  is not a backlog. `RemainingWork.VideosWithoutStreaming` is the same number as `Video.Missing`, filled by
  the store from the same relation so the backlog list and the card cannot disagree.
  **`VideoBacklog(ctx) (Video,error)`** hands that section out on its own, straight off the memoised dashboard
  cache and with `StreamingEnabled` stamped exactly as `Collect` stamps it, so the Prometheus collector
  (`kukatko_library_videos_without_streaming`) reads the dashboard's own number instead of counting again — a
  failure is an error, never an empty backlog, so a scrape drops the gauge rather than exporting a zero that
  reads as "everything is encoded". **`Platform(ctx) Platform`** (`platform.go`) is the scrape-safe accessor
  the platform collector is built on: the memoised dashboard (streaming flag stamped, `DerivedBytes` from the
  storage measurement), the memoised `StorageUsage`, `backup.Status` and `Maps` — **no** DB ping, sidecar
  probe, queue or import query and no duplicate scan (its background refresh would be kept warm forever by
  the scraper). A failed dashboard or storage read is a **nil** section, not an error, so a reader drops only
  that section; `storageCache` memoises the measurement's **error** with its value, so every read inside the
  TTL agrees on whether it is partial. The three video
  aggregates are folded into `countDashboardSQL` as CTEs (`hls_encoded` = renditions reduced to one row per
  video, `hls_encodes` = the `hls_transcode` queue grouped by the photo uid in its payload, `video_streaming`
  = both left-joined onto the browsable videos), each scanned once — **no per-video query**, which is what
  keeps the dashboard the single memoized round trip a polled page can afford
  (`TestCountDashboard_OneRoundTripPerVideoCount` counts the queries a traced pool actually issues);
  alongside the operational snapshot it also aggregates the **library statistics** of one project for its
  members: `LibraryCounter` (`CountLibrary(ctx, projectUID)` — every subselect scoped by `project_id`,
  `markers`/`photo_places` through their photo; an empty project is `ErrNoProject`, never "all", satisfied by its own `Store` = `NewStore(pool)` — a single query of scalar
  subselects `countLibrarySQL`, **not** a `maintenance scan` over the tree; partial indexes for archived/video/live,
  semi-joins on the `embeddings` PK / the unique `faces`, one CTE for the album types),
  **`LibraryStats(ctx, projectUID) (Library,error)`** (memoised per project, `projectSnapshotCache`) returns
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
  (`AggregateCharts(ctx,projectUID,since)`, every query led by `project_id = $1`, satisfied by the same `Store`) runs five grouped queries in
  `store_charts.go` — capture years, arrivals per month (bounded by `since`, so the range predicate stays
  sargable on `idx_photos_live_created_at`), the top `topCameras`=10 cameras (make+model grouped, folded into a
  display name by `cameraName`, the bare model kept for the library's `camera` filter), the bytes per media
  bucket (image/live/video + **raw**, recognized by the extension list `imgconvert.RAWExtensions()` and carved
  out of the images so the buckets stay disjoint) and the bytes per year of addition; every query counts the
  **browsable** library (`archived_at IS NULL`, so a bar matches the library view it links to) and buckets time
  **in UTC** (`AT TIME ZONE 'UTC'`, so buckets do not shift with the server's zone); the generic `collect[T]`
  helper is the single query/scan/iterate loop behind all five;
  **`LibraryCharts(ctx, projectUID) (Charts,error)`** (per project) returns `Charts{PhotosByYear,AddedByMonth,TopCameras,StorageByMedia,
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
  sections plus `Library`/`Remaining` as instance totals and the per-project breakdown `projects`; a failed collect → 500),
  and `RegisterProjectRoutes` mounts, under `/p/{project}` (the `projectAPI.stats` group), `GET /stats`
  behind `RequireAuth` (the **project's** library counts for every member, read with
  `auth.ProjectUIDFromContext`; a failed aggregation → 500,
  never a body of zeros) and `GET /stats/charts` behind `RequireAuth` (the chart series; a failed
  aggregation → 500, never empty series, which would draw as an empty library) — two endpoints rather than one
  because the counts are cheap and are what an import is watched with, while the charts are heavier and change
  slowly, so they get their own longer memoization and never hold the numbers back; always mounted
  (`buildSystemAPI` in `cmd/kukatko/system.go`, which builds its own stateless embeddings client just for the
  Healthy probe, shares the pool for the job/import/library stores, and passes the backup service nil-safely; mounted
  in `appendOpsAPIs` next to backup/restore)), `internal/capabilitiesapi/`
  (an all-authenticated HTTP API of what the instance is — its feature flags and the build it runs: the
  `Reachability` interface (`Reachable() bool`,
  satisfied by `*reachability.Checker`, fakeable);
  `NewAPI(Config{Embeddings,Passkeys,VideoStreaming,Build,RequireAuth})`+
  `RegisterRoutes` mounts `GET /capabilities` behind `RequireAuth` → `{semantic_search:bool,
  passkeys:bool, video_streaming:bool, version:version.Info}` — `semantic_search` read
  from the cached probe result (never a live probe, so it is cheap and every logged-in user may read it — unlike the
  maintainer-only `/system/status`), `passkeys` a plain `bool` rather than an interface because it is a
  **static** fact about the deployment (`authAPI.PasskeysEnabled()` — whether a relying party is
  configured at all) and nothing about it changes while the process runs, `video_streaming` static in exactly
  the same way (`cfg.Video.HLS.Enabled` — whether uploaded videos are encoded into HLS renditions at all),
  which is what lets a grid tile mark a clip whose rendition has not been produced yet without lying on an
  instance that will never produce one,
  the build injected as a value (`version.Get()` at wiring, so tests pin
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
  with the aliases `subject:`→`person:`, `keyword:`→`keywords:`;
  **`family:`** takes a person — by name, by nickname or by uid, exactly as `person:` does — and means
  that person, everybody descended from them and all of their partners: the same set the drawn tree walks
  (`internal/family`), so a page and a filter can never disagree about who a family is. It has **no Czech
  alias** (`rodina:`), for the reason `osoba:` is unsupported too: the registry is English-only;
  **`text:`** matches the text a recogniser
  read *inside* the photo (`photos.ocr_text`) and is deliberately its own key rather than folded into
  `description:`/`notes:` — OCR output is machine-read and noisy, and someone hunting for the photo of a
  particular shop wants to say so; the bool keys include
  `hidden:` (photos hidden from the library — like `archived:`, using it lifts the store's default scope,
  so `hidden:yes` is the way back to a hidden photo) and `dated:` (has / has no capture date; `dated:no` is
  the worklist of everything the timeline cannot place and covers **both** the photos whose date was declared
  unknown and those that never had one — for the person working through them it is the same job);
  the **video** keys are `duration:` (a length, not raw milliseconds: each bound is a number with an
  optional `s`/`min`/`h` suffix — bare is seconds — and is as precise as it is written, so `duration:10s` is
  every clip of ten-point-something seconds and `duration:1m-` is "longer than a minute"; the parser resolves
  the units into the millisecond bounds the column stores), `sound:` (an audio track), `fps:` (frame rate)
  and `streaming:` (already encoded for playback, or still waiting);
  the id key **`uid:`** names exactly one photo — by its own
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
  builders per key, everything through bind parameters; **every text arm folded through
  `immutable_unaccent` on both sides** (`unaccentLike`) — one rule for the lot, so `city:Chotemice` finds
  "Chotěmice" and `text:pouť` finds the "Pout" a recogniser read, while the UID arms stay plain equality;
  per-user filters scoped to `RatedBy`, `near:`
  a spherical distance with the radius `dist:` default 5 km, `faces:` counts non-invalid face markers,
  `family:` an `EXISTS` over `markers` whose subject is in an inner `WITH RECURSIVE` walk of the root's
  descendants and their partners — the pattern and the uid bound as **two** placeholders, because one `$n`
  serving both a text comparison and a `VARCHAR` one fails with SQLSTATE 42P08, and the walk guarded at 20
  generations because it dedups on `(uid, depth)`; the walk does not depend on the outer row, so the planner
  builds the member set once (measured on production — docs/PERF.md §3),
  the four video filters each narrowed by `queryCondGuards` to the rows their question means something for —
  the guard sits **outside** the negation, so `sound:no` and `duration:!10s` stay within the clips instead of
  answering for every still — and `fps:`'s bounds widened by half a percent so `fps:30` finds the 29.97 an
  NTSC file records,
  every **decimal** bound (both an exact one and the ends of a range) allowed ±0.005 because of float4, integer bounds
  stay exact; `likePattern` makes a wildcard only out of an unescaped `*` and escapes `%`/`_`, just like
  the substring filters `Search`/`?camera=`/`?lens=` in `store_list.go`'s `textClauses`, which share the
  accent fold for the same reason — on the list path q's own free text *is* `Search`). The package also owns the **uid
  router** (`uidref.go`, pure, no I/O): `ClassifyUID(token) (UIDRef, bool)` reads a uid's two-letter prefix and
  says what it names — `ph` photo, `al` album, `lb` label, `su` subject (`EntityPerson`), `st` stack, `mk`
  marker, all 26 characters of lowercase base32, plus `pt` = an **imported** photo uid at 16 characters of
  base36, a length that keeps the two families apart; `FindUID(input)` returns the first uid-shaped word of an
  input, so an id pasted with a word beside it is still recognised. A token with an **unknown** prefix is
  deliberately **not** accepted — probing every table per keystroke buys nothing. `internal/globalsearchapi`
  routes a pasted id with it. The registry is also **published** (`schema.go`, pure): `Keys() []KeyInfo`
  returns every canonical key sorted, each with its `Kind` and — for a closed vocabulary only — the words it
  accepts (`Kind.String()` gives the wire names `text`/`number`/`date`/`bool`/`enum`/`id`/`count`/`duration`;
  a bool and a count's yes/no form offer `yes`/`no`, the two spellings a suggestion wants out of the six the
  parser takes), and `Aliases()` hands out a copy of the alias table for a client that must *recognise* a
  typed `subject:` without keeping its own list. `internal/globalsearchapi` serves both to the search
  palette, so no list of filter keys anywhere else has to be maintained by hand. The user-facing grammar:
  docs/API.md "Search language (q=)".
  **`person:me`, `family:me` and `uploader:me` are deliberately *not* resolved here** — the parser
  knows nothing about who is asking, which is what keeps a filter's meaning independent of the request that
  carried it (`uploader:none`, which says nothing about the caller, is compiled by the store instead);
  see `internal/personme`), `internal/personme/`
  (resolution of the words of the query language that mean something different to every caller:
  **`person:me`**, **`family:me`** and **`uploader:me`**. `Resolve(filters, linkedSubjectUID) (used, resolved bool)`
  rewrites every `person:` **and `family:`**
  alternative whose text is exactly `me` — the negated `!me` included — to the caller's linked subject UID
  (both `Text` and `Pattern`, so the compiled LIKE cannot still match a person *named* "me"), in place.
  The linked subject is **the caller's membership of the request's project** (`project_members.subject_uid`,
  read by the auth guard when it admits the caller and exposed as `auth.LinkedSubjectFromContext`; nil for a
  maintainer without a membership) — never an account-wide link (there is none since 0110): the same account is a
  different person, or nobody, in each of its projects. `photoapi.applyMeTokens(params, user, linked)` and
  the MCP caller (`caller.linked`) pass it.
  (both keys name a subject, so one pass rewrites both and they share one verdict).
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
  (8192), so it needs no external goroutine; mounted on `POST /upload` (ingest), `POST /photos/bulk`
  (bulkapi), `POST /import/*` (importapi) and `GET /map/tiles/...` (mapsapi) — the limits come from the
  `ratelimit.*` config; login and geocode have their own limiters (geocode now *uses* this package: a
  per-project limiter in front of a one-key instance limiter). The browse vector searches (`photoapi`'s
  search/similar) are throttled by `cmd/kukatko`'s `newVectorThrottle`: a per-user
  `KeyedMiddlewareExcept` wrapped around a per-project one (`ratelimit.vector`, `ratelimit.vector_project`),
  so a member over their own share never spends the project's tokens. The five whole-project routes
  (`duplicatesapi`'s listing, `reviewapi`'s queue, `candidatesapi`, `expandapi`, `sweepapi`) share one
  `newHeavyVectorThrottle` built the same way from `ratelimit.heavy_vector`/`heavy_vector_project`. `KeyedMiddleware(keyFn)` is the same
  middleware with the bucket key chosen by the caller instead of the IP; `Middleware` is now
  `KeyedMiddleware(clientIP)`. It exists for `POST /photos/{uid}/comments`, which is throttled **per user**
  and therefore mounted *inside* the auth guard (the principal is only on the context once auth has run) — a
  household behind one NAT is one address but many people. A key function returning `""` is not
  special-cased: every such request shares one bucket, the safe reading of "no identity to attribute this to".
  `MiddlewareExcept(exempt)`/`KeyedMiddlewareExcept(keyFn, exempt)` add an **exemption predicate**: a request
  for which `exempt` reports true is passed straight through, never reaching a bucket at all — not charged a
  token and not refunded one, so an exempt caller cannot drain the budget a throttled one shares (an exemption
  is the absence of a bucket, not a private one; a nil predicate is the plain middleware). It is how an API
  token marked `unlimited` skips the throttle (`auth.RateLimitExempt`), which is also why `POST /upload` and
  `POST /photos/bulk` now mount the limiter **behind their role guard (`RequireCurator`) rather than ahead of
  it**: the caller's identity has to be known when the limiter runs. Nothing expensive became reachable — both
  routes already required a role above viewer, so an unauthenticated flood merely pays one indexed credential lookup before its
  401), `internal/clientip/`
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
  somebody logs it by mistake; the request's project: `SetProject(ctx, uid, slug)` stamps the fields bag,
  `WithProject(ctx, uid, slug)` does that and returns a context carrying the uid, `ProjectUID(ctx)` reads
  it back (context first, then the bag) — `auth` calls `WithProject` when it admits a caller into a
  project, so the access line carries `project=<slug>` and `audit.FromRequest` the uid even where no
  `AccessLog` installed a bag; the **`AccessLog` middleware** emits one structured line per HTTP
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
  **`BackfillLocations(ctx, projectUID) (estimated,error)`** (backing `POST /process/locations`, which passes the
  request's project): **everything is one project's** — `ListLocationCandidates(projectUID,limit)`,
  `ListLocatedNeighbours(projectUID,from,to)` and the guarded `SetEstimatedLocation(projectUID,uid,…)` all carry
  `photos.project_id`, so another project's trip on the same afternoon is never evidence; for every
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
  every guess unnamed), `Pending(ctx,projectUID,offset,limit) ([]Pending,total,error)` (one rotating window of the
  **project's** photos carrying an estimate — `ListEstimatedLocations`/`CountEstimatedLocations` take the project + the full size of that set, each with its cached `photo_places` row resolved in
  **one** bulk read; `Pending.PlaceLabel()` = place name → city → region → country, `""` when nothing is
  geocoded) and the two verdicts `Accept`/`Reject(ctx,projectUID,photoUID,meta)`, which load the photo through
  `GetByUIDInProject` (a photo of another project is `ErrPhotoNotFound`, the review game's "gone"). Both do a
  **read-modify-write** through
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
  (started/finished counter + a duration histogram by type/outcome — outcomes `success`/`error`/`deferred`/
  `terminal` — plus `kukatko_jobs_stale_locks_recovered_total`), embeddings (a duration histogram +
  `service_up{target}`, target `box`|`text` = the role of the host, never its URL), import progress (a gauge per source/outcome), thumbnail duration and the geocode credit
  counter (`kukatko_geocode_credits_spent_total` — metered mapy.com money, so the spend of a running import
  is watchable, not inferred from the bill), the **streaming encode** (`registerVideoEncode`:
  `kukatko_video_encode_duration_seconds{rendition,outcome}` + `_output_bytes_total{rendition,outcome}`, one
  observation per **rendition** rather than per job — a job is one row in the generic series whether it chewed
  through a six-second clip or a forty-minute one — plus `_source_seconds_total`, the footage counted once per
  **video**, so the summed duration divided by it is what a minute of video costs to encode; the histogram uses
  `ExponentialBuckets(5,3,8)` — 5 s tripling to just over three hours — because the default buckets top out at
  ten seconds, shorter than every encode this exists to measure), `kukatko_build_info{version,commit} 1` from
  `internal/version` + the standard `go_`/`process_` collectors; the job, embedding and thumbnail histograms use
  `workDurationBuckets` (`ExponentialBuckets(0.01,2,17)`, 10 ms → ~655 s) and HTTP `httpDurationBuckets` (the
  defaults + 30/60/120/300 s for streaming routes) — the defaults' 10 s ceiling put every minute-long job in
  `+Inf`; `preinit.go` creates every label combination at zero at startup (`InitJobTypes` from the worker
  registry's `Types()`, `InitEmbeddingOperations` from `embedding.Operations()`, `InitVideoEncode` from the
  configured rendition plan, all called from `cmd/kukatko/obs.go`), so an idle family reads 0 instead of
  absent — except `service_up`, whose 0 would claim an outage nobody observed; the **pull-at-scrape collectors** `RegisterDBPool` (live pgx pool stats),
  `RegisterJobQueue` (`QueueDepthFunc` → `map[QueueCell]int` keyed by (type,state); the collector folds that
  one breakdown into `kukatko_jobs_queue_depth{state}`/`_by_type{type}`/`_by_type_state{type,state}`, so the
  three families are sums of each other and one query per scrape answers all of them; `collectTimeout` 5 s,
  so a slow DB does not block the scrape) and `RegisterGeocodeBudget` (`GeocodeBudgetFunc` →
  `kukatko_geocode_credits_remaining`/`_limit`, sampled at scrape time so the gauge follows the budget
  window rolling over even while no job runs; a nil func = no budget wired) and `RegisterNotificationDrops`
  (`NotificationDropsFunc` → `[]NotificationDrop{Stage, Reason, Count}` → the counter
  `kukatko_notifications_dropped_total{stage,reason}`, `serve` adapting `notifystanding.Drops()`) read their data at scrape time
  without extra goroutines; `RegisterLibrary(LibraryStatsFunc, ttl)` (`library.go`) adds the **library-content**
  gauges, **one series set per active project, each labelled `project="<slug>"`** (the slug, never the
  name — `/metrics` is unauthenticated; the instance total is `sum without (project)`) —
  `kukatko_library_photos{media_type}`/`_photos_archived`/`_photos_processed{stage}`/
  `_photos_pending{stage}`/`_embeddings`/`_faces`/`_markers{state}`/`_subjects{type}`/`_albums{type}`/`_labels`/
  `_videos_without_streaming` (the streaming-encode backlog, exported whatever `video.hls.enabled` says — with
  streaming off it is every video, and saying so is the alert's job, not the gauge's)
  and `kukatko_import_last_run_status{source,status}` (1 for the status the run is in, **0** for every other
  known one, so a transition is visible instead of a series vanishing) + `_start`/`_finish_timestamp_seconds{source}`
  (Unix seconds; the age is `time() - gauge`, not a pre-computed one; instance-wide, no project label) — over a
  `LibrarySnapshot{Projects map[slug]LibraryCounts, Imports}` whose every labelled dimension is a
  `map[string]int`, so a new album/media type needs no change here; `_collect_errors_total` carries no project
  label (one aggregation fails for every project at once); the snapshot is
  **memoised** for `metrics.library_ttl` (`defaultLibraryTTL` 1 min) because these are aggregates over the
  largest tables and a scrape arrives forever — without it `/metrics` becomes the load it reports on; a stale
  value is **never** served past the TTL: a failure exports no library gauges (a gap, not a number the library
  no longer has) and bumps `kukatko_library_collect_errors_total`; the gauges carry no `_total` suffix on
  purpose, that is reserved for counters. The func is wired in `cmd/kukatko/obs.go`
  (`registerLibraryMetrics`/`libraryByProject`/`libraryCounts`/`importRuns`) onto
  `system.Service.LibraryByProject` (one grouped statement, `system.Store.CountLibraryByProject`, over the
  active projects — the instance statistics' own predicates grouped by `project_id`, `markers` and
  `photo_places` through their photo, so each project's gauges equal its `GET /p/{project}/stats`) + `LatestRuns`;
  `/metrics` is unauthenticated, so only aggregates go in it — per project at most, labelled by slug — never
  a per-user number or a photo/album/label/person/project name as a label value. The platform collector's
  `kukatko_library_*` families (encode states, backlogs, bytes, below) stay instance-wide: they read the
  dashboard's instance-wide caches and describe the shared pipeline and disk, like the pool and queue gauges; `RegisterPlatform(PlatformFunc)` (`platform.go`) adds the
  **platform** families read from one `PlatformSnapshot` per scrape — `kukatko_backup_configured`/`_running`/
  `_last_run_success`/`_last_run_finish_timestamp_seconds`/`_last_success_timestamp_seconds`/
  `_last_run_originals{result}`, `kukatko_disk_tree_bytes{tree}`/`_filesystem_free_bytes`/`_filesystem_size_bytes`,
  `kukatko_maps_configured`/`_up`/`_last_check_timestamp_seconds`, `kukatko_library_video_streaming_enabled`/
  `_videos_by_encode_state{state}`/`_video_encode_oldest_queued_timestamp_seconds`/`_backlog{kind}`/
  `_bytes{set}` — with **no cache of its own** (the sources are the system caches or in-memory state; a second
  layer would only make backup/maps lag); a nil `Disk`/`Catalogue` drops exactly that section and bumps
  `kukatko_platform_collect_errors_total{source}`, and an event that has not happened in this process (no
  finished backup, no observed map call, nothing queued) is **absent**, never a zero. Wired in
  `cmd/kukatko/platform_metrics.go` onto `system.Service.Platform`; `Handler()`
  is mounted by `serve` on `/metrics` (the middleware skips that path, a scrape does not instrument itself),
  the observation methods `JobStarted`/`JobFinished`/`StaleLocksRecovered`/`ObserveEmbeddingCall`/
  `SetEmbeddingUp(target,up)`/
  `ObserveThumbnail`/`GeocodeCreditSpent`/`ObserveRenditionEncode`/`ObserveEncodedSource`
  and `Middleware(routeOf)` are handed to the subsystems that
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
  - **Eight Czech templates**, each a **pure function of its own data struct** returning
    `Rendered{Template,Subject,Body}` (`Rendered.Message(to)` addresses it), unit-tested against the exact
    expected text. **Every data struct of a project's event carries `ProjectName`**: with one, the subject
    is `InProject(name, subject)` = `"Kukátko · <name>: <subject>"` and the body says where it happened
    (`v projektu „<name>“ v Kukátku`; the generic notification mail adds a `Z projektu „<name>“ v Kukátku:`
    line, the digest keeps its `Projekt <name>` heading), all links already under `/p/<slug>/`; without one —
    a job queued before projects — the old text, byte for byte (the goldens of both live side by side,
    `templates_project_test.go`). `RenderRegistrationReceived` (`RegistrationReceivedData{DisplayName,
    Username,ProjectName}` — your account exists and waits for an administrator), `RenderAccountApproved`
    (`AccountApprovedData{DisplayName,ProjectName,ProjectURL,SignInURL}`: with a project it names it and
    links to its `/p/<slug>/`; without one — a job queued before projects — the old sign-in text), `RenderNewRegistrationPending`
    (`NewRegistrationPendingData{Username,DisplayName,Email,ProjectName,ApprovalURL}` — the administrator's
    copy, naming all three and linking the project's member screen),
    and `RenderPasswordReset` (`PasswordResetData{DisplayName,ResetURL,ValidFor}`). `templates_setup.go` holds
    the two mails of a project set up for somebody: `RenderProjectSetup` (`ProjectSetupData{DisplayName,
    ProjectName,Username,SetPasswordURL,ValidFor}` — „Byl pro vás založen projekt „X" v Kukátku… Nastavte si
    heslo:", the link, its validity, the username; not the reset text) and `RenderProjectAdminAdded`
    (`ProjectAdminAddedData{DisplayName,ProjectName,Username,ProjectURL}` — for an existing account: the
    project's link, no password link). The `Template*` name
    constants exist so a log or audit line can say which message went out without repeating its subject. Two
    details the tests pin: every mail opens with the bare `Dobrý den,` — never the display name, whose
    vocative cannot be derived and whose nominative reads wrong — and `ValidFor` is rendered with the
    **Czech plural rule** in the largest unit it divides evenly into (`jednu hodinu`, `2 hodiny`, `7 dnů`), with a non-positive duration falling back to
    `Odkaz má omezenou platnost.` instead of printing a zero. The fifth, `RenderTasksWaitingDigest`
    (`TasksWaitingDigestData{DisplayName,Total,Tasks []DigestTask{Title,State,URL},QueueURL}`), is the
    daily digest of tasks waiting on a person: subject `Čeká na vás N úkolů` (the verb and the noun follow
    the plural rule — `Čeká na vás 1 úkol`, `Čekají na vás 3 úkoly`), one line per listed task with its
    state in the queue's own words (`čeká na odpověď`/`pracuje se`/`ke schválení`, an unknown state printed
    as it is) and its absolute link, `…a dalších M úkolů.` when the list was cut, and the link to
    `/tasks?waiting=1`. It is formal like every mail, although the queue in the app says „na tebe".
    **One register for every e-mail:** all templates speak the formal „vy" (`Dobrý den,`, „vás", „můžete"),
    since a mail may reach somebody the sender does not know; the app UI keeps its own voice.
    `templates_register_test.go` renders every template and fails on an informal word („Ahoj", „tě", „ti",
    „můžeš", „sis"…) in a subject or body, and on a greeting that names the recipient.
    The sixth, `RenderUsernameChanged` (`UsernameChangedData{DisplayName,Username,SignInURL}`), tells somebody an
    administrator renamed their account: the new name in the subject and the body, that the password stays the
    same, and the sign-in link — left out entirely when `SignInURL` is blank.
    The last three are the **e-mail channel of the notification kinds** (`templates_notification.go`, each
    closed by the footer „Tento e-mail vám chodí, protože jste si ho zapnul(a) v nastavení oznámení… Tam ho
    můžete i vypnout."): `RenderTagged` (`TaggedData{DisplayName,Count,URL,Groups}` — "označil na jedné
    fotce / 3 fotkách", subject „Označili vás na fotce/fotkách", the link to the notification's page; with
    `Groups` (`TaggedGroup{Album,Count,URL}`, the tagging window split by album) one mail all the same: the
    total, "Podle alb:", then one line per album — title, "5 fotek" and its link — and the photos outside
    the albums last as "Další fotky", `Groups` winning over `URL`), `RenderAlbumFeatured`
    (`AlbumFeaturedData{DisplayName,Title,URL}` — subject „Nové doporučené album: <title>", the album is
    on the homepage, the link to it; a blank title reads „(bez názvu)"), `RenderTaskAssigned`
    (`TaskAssignedData{DisplayName,Who,Title,URL}` — subject „<who> vás přidal(a) k úkolu: <title>", the
    body „v Kukátku vás <who> přidal(a) k úkolu „<title>"." and the link to the task; a blank `Who` reads
    „Někdo", a blank title „(bez názvu)"), `RenderCommentMention`
    (`CommentMentionData{DisplayName,Who,OnTask,Title,Excerpt,URL}` — subject „<who> vás zmínil(a) v
    komentáři u úkolu „<title>"" / „u fotky „<title>"" / bare „u fotky" for an untitled photo, the excerpt
    indented as a quote, the link to the comment) and the
    generic `RenderNotification` (`NotificationData{DisplayName,Title,Body,URL}` — the title as the subject,
    the body (or the title when there is none) and the link), which `internal/notifydeliver` uses for any
    kind without a template of its own. `internal/mailjob` has a constructor for each (`Tagged`,
    `AlbumFeatured`, `TaskAssigned`, `CommentMention`, `Notification`) and renders all five.
  - Configuration is `mail.*` (`internal/config`, [`OPERATIONS.md`](OPERATIONS.md)); an enabled mailer with no
    `host`, `from_address` or `base_url` fails startup naming every missing key.

- **Queued mail (`internal/mailjob`):** the delivery path — `internal/mailer` renders and speaks SMTP, this
  package decides **when** a message is scheduled, **whether** it is scheduled at all, and **which** failures
  are worth another try. Sending from the HTTP handler that caused the mail has two faults: the request waits
  on a remote host, and a mail server that is briefly away loses the message. Both go away once the persistent
  queue is the only path.
  - **The payload is a recipe, not a message:** `{template, to, data, for?}` — the template *name* plus its
    data struct as JSON, never rendered text and never a live object, so a job means the same thing after a
    restart or an upgrade. `Handle` decodes it, renders through the generic `renderWith[T]` (which is what keeps a
    template and its data struct paired **by the compiler**, not by convention) and hands the `mailer.Message`
    to the `Sender`.
  - **`Enqueuer`** (`NewEnqueuer(EnqueuerConfig{Enabled,Schedule,Logger})`) is what callers use instead of
    touching the queue: `Enqueue(ctx, exec, Mail)`, where `exec` is a `jobs.Execer` — pass **the transaction of
    the mutation that caused the mail** and the message is scheduled if and only if that mutation commits, the
    way `audit.Write` joins its mutation. Build the `Mail` with `RegistrationReceived`/`AccountApproved`/
    `NewRegistrationPending`/`PasswordReset`/`TasksWaitingDigest`/`UsernameChanged`/`ProjectSetup`/`ProjectAdminAdded`, which pair each template with the data it expects.
    **`Delivers(to)`** answers after the fact whether a message to `to` is really scheduled (mail on and
    a deliverable, non-`.invalid` address) — the "was it mailed or must I pass the link on?" of
    `auth.ProjectSetup`.
  - **A notification's mail is addressed.** `Mail.Addressed(notifystanding.Addressee{UserUID, ProjectUID,
    Kind})` sets `Mail.For`, stored as the payload's `for`; `internal/notifydeliver` addresses every mail it
    builds and the task digest addresses its own (kind `taskdigestjob.DigestKind`). When the job runs,
    **`Handle` asks the standing question again** (`ServiceConfig.Standing`, a `notifystanding.Checker`,
    required — `NewService` panics without it): an account disabled, a membership removed or never approved,
    or a project deactivated since the mail was queued → the job **completes unsent**, logged and counted
    (`notifystanding.StageMail`/`ReasonStanding`); a failed standing query is retried. The approval mail is
    addressed too (no kind — the member rule): it names a project and links into it, so a member removed, a
    project deactivated or an account disabled before it goes out is not told about a project it can no longer
    open. The account's own mails (registration, password reset, rename) carry no addressee and are never held back.
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
  - Wired in `cmd/kukatko` next to the other handlers and **only when `mail.enabled`** (`buildMailServiceOrNil`,
    over a `notifystanding.Checker` on the pool);
    with mail off nothing enqueues either, and a job left over from a period when mail was on simply waits in
    the queue until it is configured again. The worker gives `mail_send` its own single-slot pool.

- **Daily tasks digest (`internal/taskdigestjob`):** the one message the task queue sends out of the app —
  once a day, every person is mailed, **per project**, the open tasks of that project whose move is theirs,
  so a question an agent opened reaches the human it waits on. It is the `task_digest` job plus the
  scheduler that enqueues it, and it sends nothing itself: every message is a `mail_send` job scheduled
  through `mailjob.Enqueuer`, so it inherits the queue's retries and the mailer's address guard. Who is
  mailed is the shared standing rule (`phototask`'s `waitingForEveryoneSQL` embeds
  `notifystanding.MemberSQL`: an approved member of an active project, enabled), and every digest mail is
  **addressed** (`Mail.Addressed`, kind **`DigestKind`** = `task_digest`), so a person who falls out of
  standing before the `mail_send` job runs is not mailed after all.
  - **The unit of work is a (project, person) pair.** The job's payload is `jobs.TaskDigestPayload{
    ProjectUID, UserUID}` (`project_uid` + `user_uid`) and the dedup index keys on both (migration
    `0107_task_digest_per_project.sql`); the watermark is the membership's `project_members.task_digest_at`.
    A person in two projects gets two jobs and two mails, each headed with its project's name
    (`mailer.TasksWaitingDigestData.ProjectName` → a `Projekt <name>` line above the list; absent for a
    mail queued before projects existed) and linking inside it.
  - **`Service`** (`New(Config{Source,Mail,Exec,BaseURL,Clock,Logger})`, panicking on a missing `Source`/
    `Mail`/`Exec`) is the handler. `Handle` decodes the pair (a legacy job with no payload does nothing —
    the next daily run covers everybody) and `Run(ctx, projectUID, userUID)` asks `Source.DigestFor`
    (satisfied by `*phototask.Store`) whether that pair is due, schedules one mail on `Exec` (the pool — a
    digest is nobody's mutation) **and then stamps** the membership (`MarkDigested(ctx, projectUID, uid,
    clock())`), in that order: a stamp without a mail would silence somebody, a mail without a stamp only
    repeats itself. It returns an `Outcome` (`sent`/`nothing`/`skipped`/`mail_off`, logged by `Handle`). A
    recipient the mailer refuses (`ErrPlaceholderAddress` for a `.invalid` placeholder,
    `ErrInvalidAddress` for none) is **skipped and left unstamped** — they hear the news once the address is
    fixed — and is not an error; a read, queue or stamp failure is the job's error, which the queue retries.
    With mail off (`Mail.Enabled()` false) nothing is scheduled **and nobody is stamped**. Links are
    `TaskURL(base, slug, uid)` = `<base>/p/<slug>/tasks/{uid}` and `QueueURL(base, slug)` =
    `<base>/p/<slug>/tasks?waiting=1` (`project.AppPath`), on `mail.base_url` exactly as the password-reset
    link is (site-relative when it is empty).
  - **`Scheduler`** (`NewScheduler(SchedulerConfig{Enabled,Enqueuer,Targets,Hour,Clock,Logger})`, panicking
    when enabled without `Enqueuer` or `Targets`) follows the trash retention purge's shape — one goroutine
    for the life of the server, `Run` returning at once when disabled — but enqueues jobs rather than doing
    the work, so a run is retried, observable and dead-lettered like every other. `NextRun(now, hour)`
    (pure, tested) is the next `hour:00:00 UTC` strictly after `now`; a missed hour is not caught up. At
    the hour, `Tick` reads `Targets.DigestTargets` (`*phototask.Store`: every due pair) and calls
    `jobs.Enqueuer.EnqueueTaskDigest(ctx, projectUID, userUID)` once per pair (a refused pair does not
    keep the others from theirs; the failures are joined and logged), deduped on the queued state per pair,
    so a restart straddling the hour cannot queue the same day twice.
  - Wired in `cmd/kukatko` (`taskdigest.go`) **only when `tasks.digest.enabled` and `mail.enabled` are
    both true** — the handler is registered and the scheduler is live under the same `taskDigestOn` answer,
    so a job is never enqueued that nothing will claim. Configuration `tasks.digest.*`
    ([`OPERATIONS.md`](OPERATIONS.md)). Tests: the store query over a real database (who gets one, the
    stamp, the cap, one digest per project with its own stamp and two queued jobs for a person in two
    projects), the service against the real `mailjob.Enqueuer` + `mail_send` handler + `mailer.Fake` with a
    fixed clock (one mail per pair with the project heading and `/p/<slug>/` links, two for two projects,
    none when nothing is due or the job is legacy, none when mail is off, the skip, the failures), and the
    scheduler's timing and per-pair fan-out.

- **Web Push (`internal/push`):** the one way Kukátko sends a push notification, the same shape as
  `internal/mailer`. Its only caller is the `push_send` handler in `internal/pushjob`; the HTTP surface
  (subscriptions) is `internal/notificationapi`, the frontend service worker later work. Everything goes through the
  **`Sender`** interface (`Send(ctx, Subscription, Notification) error` — one notification to one
  subscription): **`VAPIDSender`** in production, **`Noop`** when `push.enabled` is false (accepts
  everything, encrypts and dials nothing), and **`Fake`** in tests (`Sent`/`Last`/`Reset`, `FailWith` for
  every send, `FailEndpoint` for one dead device among live ones; **never opens a socket**, applies the
  same guards as the real sender). `New(Config)` is the wiring decision: `Noop` when disabled without
  reading a key, otherwise `NewVAPID`.
  - **`Notification`** is `{Title, Body, URL, Kind, Tag}` marshalled as JSON with the field names
    `title`/`body`/`url`/`kind`/`tag` — a **contract with the service worker**, its only reader, pinned by
    a test. `URL` is a deeplink **path on this instance** (`/tasks/…`; empty = home) — `Encode` refuses an
    absolute, `//host` or `/\host` URL with `ErrInvalidNotification`, so a notification can never link off
    the instance; a blank title is refused too. `Tag` is the collapse key; one that is also a valid RFC
    8030 `Topic` (≤ 32 base64url characters) is sent as that header, so the push service replaces a
    message it still holds for an offline device.
  - **The size limit is checked before anything is dialled.** webpush-go encrypts into one 4096-byte
    `aes128gcm` record; less the 86-byte header, the 16-byte GCM tag and the padding delimiter that leaves
    **`MaxPayloadSize` = 3993** bytes of JSON. `Encode` refuses more with **`ErrPayloadTooLarge`**
    (permanent — retrying cannot make it fit); a test proves a payload of exactly that size goes through
    the real library and decrypts intact.
  - **Result classification** is the point of the typed errors: a push service's non-2xx answer is a
    **`*StatusError{StatusCode, RetryAfter, Body}`** that unwraps to **`ErrGone`** for 404/410 (dead for
    good — the **caller** deletes the row; the sender only reports, the lifecycle is the caller's),
    **`ErrRetryable`** for 429 and 5xx (with `Retry-After` in seconds when sent), `ErrPayloadTooLarge` for
    413 and **`ErrRejected`** for any other 4xx (typically a 403: the subscription was made for another
    VAPID public key) **and for a 3xx, which is never followed**. `Body` is the start of the answer as one
    line of plain text (`plainText`: control/format runes → spaces, ≤ 200 runes), because it lands in a
    job's `last_error`. An unreachable endpoint or an expired context is `ErrRetryable` too.
    `Retryable(err)` is the caller's one-line test. An invalid subscription (`ValidateSubscription`: an
    `https` endpoint ≤ 2048 characters, `p256dh` an uncompressed P-256 point, `auth` 16 bytes, any base64
    flavour) is `ErrInvalidSubscription` before dialling.
  - **VAPID.** `GenerateKeys` mints a pair with `crypto/ecdh` (unpadded base64url, the encoding browsers
    use); `ValidateKeys` checks both decode, are P-256 **and form a pair** (a mismatched pair would sign
    with a key no browser subscribed against); `ValidateSubject` wants `mailto:` or `https:`. Every error is
    `ErrInvalidConfig` and names the problem, **never a key's value**. One library quirk is absorbed here:
    webpush-go prefixes `mailto:` to any subscriber that is not `https:`, so `subscriberFor` hands it the
    bare address — passed as configured, the JWT `sub` would read `mailto:mailto:…` (a test verifies the
    signed claims). Requests use `Urgency: normal` and a TTL of `DefaultTTL` (24 h).
  - **Destination policy** (`destination.go`, SEC-020 in [`SECURITY_AUDIT.md`](SECURITY_AUDIT.md)): the
    endpoint is chosen by whoever subscribed, so the production client (`HTTPClient` nil) has a 30 s
    timeout, **no redirects, no environment proxy, and a dialer `Control` hook that refuses every
    non-public address after DNS resolution** (loopback, private/ULA, link-local, unspecified, multicast,
    CGNAT/tailnet and a few reserved ranges; mapped and NAT64 addresses judged by their IPv4) — a refused
    dial is a permanent `ErrRejected`. An injected `HTTPClient` (the tests' `httptest` client) keeps its
    transport but is copied with redirects off. `Store.Upsert` also refuses visibly internal endpoints
    (non-public IP literals, single-label hosts, `*.localhost`) as `ErrInvalidSubscription`; there is
    deliberately no allowlist of push-service hosts. The library (`github.com/SherClockHolmes/webpush-go` v1.4.0) is pure Go —
    `golang-jwt/jwt/v5` + `golang.org/x/crypto` — so `CGO_ENABLED=0` still holds.
  - **`Store`** over `push_subscriptions` (migration 0086: one row per browser, `user_uid` → `users`
    `ON DELETE CASCADE`, **`endpoint` UNIQUE**, `p256dh`, `auth`, `user_agent`, `created_at`,
    `last_used_at`, `failure_count`, `last_failure_at`; ids are `ps` + 24 base32). `Upsert` is keyed by
    endpoint — the same browser subscribing again rewrites its row and keeps its id, and clears the failure
    bookkeeping; an endpoint that changes hands (another person subscribed on the same browser) moves to
    them with its created/last-used stamps started over (kept on purpose: the endpoint is an unguessable
    capability no API hands to another account). It validates first, so nothing unsendable or visibly
    internal is stored. `Get(id)` (whoever owns it; `ErrNotFound` once it is gone), `ListForUser` (oldest first),
    **`SubscriptionIDs(ctx, Querier, userUID)`** (a package function over any `Query`-er, pool or
    `pgx.Tx`, so the push fan-out reads the devices **inside the caller's transaction**),
    `DeleteByEndpoint` (whoever owns it — what an unsubscribing
    browser and an `ErrGone` both name), `DeleteByID(userUID, id)` (**owner-scoped**: a foreign id is
    `ErrNotFound` like a missing one), `DeleteForUserByEndpoint(userUID, endpoint)` (**owner-scoped**, returns
    the count, `0` is no error — the sign-out's delete, `auth.APIConfig.PushDevices`), `DeleteAllForUser`,
    `RecordSuccess` (stamps `last_used_at`, resets
    the count) and `RecordFailure` (returns the failures since the last success, so the caller can retire a
    subscription that keeps failing). The table is **account data**: `maintenance reset` keeps it
    (`internal/reset` `preservedTables`).
  - Configuration `push.*` (`internal/config` validates an enabled section through `ValidateKeys` +
    `ValidateSubject`, so a bad pair fails startup), CLI `kukatko push generate-keys`
    ([`OPERATIONS.md`](OPERATIONS.md)). Tests: payload encoding and limits; key/subject/subscription
    validation; the VAPID sender against an `httptest` TLS push service that **decrypts the payload the way
    a browser does** (RFC 8291) and **verifies the ES256 signature and claims**; every status class; no
    redirect followed, a loopback endpoint refused at dial time, the address/host policy; the
    fake; and the store over a real database (upsert by endpoint, moving hands, owner-scoped delete,
    bookkeeping, the cascade, `Get` and `SubscriptionIDs` inside a transaction).

- **Queued push (`internal/pushjob`):** the delivery path for Web Push, the twin of `internal/mailjob` —
  `internal/push` encrypts and speaks the protocol, this package decides **what** is scheduled, **whether**
  anything is scheduled at all, and what each answer of the push service does to the job **and to the
  device**.
  - **One job per device, addressed to an account.** The payload is `{subscription_id, to, notification}` —
    one `push_subscriptions` row, the `notifystanding.Addressee` (account, project, kind) the push is for, and
    the `push.Notification` for it. The subscription is read by id **when the
    job runs**, so a browser that re-subscribed meanwhile gets the fresh keys and one that unsubscribed gets
    nothing. That split is what makes a retry safe: a job retried because the laptop's push service was
    unreachable re-sends to the laptop only, never a second time to the phone.
  - **`Enqueuer`** (`NewEnqueuer(EnqueuerConfig{Enabled,Schedule,List,Logger})`) is the fan-out:
    `Enqueue(ctx, exec, to notifystanding.Addressee, Notification) (queued int, err)` reads the account's
    (`to.UserUID`) subscription ids
    (`push.SubscriptionIDs`) and inserts one `push_send` per id (`jobs.Enqueue`, `MaxAttempts` **8** ≈ an
    hour of the 30 s-doubling backoff), both through `exec` — a `pushjob.Execer` (`jobs.Execer` +
    `push.Querier`: a pool or **the transaction of the mutation that caused the notification**, so it is
    scheduled if and only if that mutation commits). **Two silent refusals returning `0, nil`:** push is
    disabled, and the account has no subscription — nothing queued is the correct outcome, a job that can
    never deliver is not. **Refused with an error:** no account (`ErrMissingUser`), no project
    (`ErrMissingProject`) and a notification
    `Encode` rejects (`ErrInvalidNotification`, `ErrPayloadTooLarge`) — caller bugs, dead on arrival.
    `Schedule`/`List` are injectable so the unit tests need no database.
  - **`Service.Handle`** (`NewService(ServiceConfig{Enabled,Sender,Store,Standing,MaxFailures,Logger})`,
    panicking on a nil `Sender`/`Store`/`Standing`) first checks **whom the job is for** (`addressed`): a job
    naming no account (queued by a release before pushes were addressed) is dropped (`ReasonUnaddressed`); a
    subscription that now belongs to **another account** — `push.Store.Upsert` moves an endpoint to whoever
    subscribed it last, so a shared browser changes hands — is dropped (`ReasonDeviceMoved`), never shown to
    the wrong person; and an account **no longer in standing** in the notification's project
    (`notifystanding.Checker`: disabled, membership removed or unapproved, project deactivated) is dropped
    (`ReasonStanding`). Each drop completes the job, is logged and counted (`StagePush`); a failed standing
    query is retried. Then it maps every outcome: accepted → `RecordSuccess` and complete (a failed stamp
    is only logged — failing would send twice); **`ErrGone` → the subscription is deleted and the job
    completes** (the notification was fine, the device was not — this handler is the **only** place dead
    subscriptions are pruned); `ErrPayloadTooLarge`/`ErrInvalidNotification` → `worker.Terminal`, the
    device untouched; anything else is the device's failure: `RecordFailure`, and once the run of
    consecutive failures reaches **`MaxFailures` (20)** the device is **retired** (deleted, job complete)
    so a permanently broken endpoint stops feeding the queue retries; below it a transient failure
    (`push.Retryable`: 429, 5xx, network) is an ordinary error the queue retries with backoff, and a
    permanent one (`ErrRejected`, `ErrInvalidSubscription`) is terminal. **Completes without sending**
    when push was switched off after the job was queued, and when the subscription is gone at claim time
    (unsubscribed, or the account was deleted and the FK cascade took its devices). A payload that does
    not decode or names no subscription (`ErrMissingSubscription`) is terminal.
  - Wired in `cmd/kukatko` (`buildPushService`, over `push.New` + `push.Store`) and registered
    **unconditionally** — with push off the no-op sender is wired and leftover jobs drain unsent rather
    than wait forever for a claimant. The worker gives `push_send` its own **two-slot** pool. Callers:
    self-service registration (`internal/auth`) and the tagging window (`internal/tagnotifyjob`). Tests: the enqueue fan-out and refusals against
    a fake scheduler, the handler's outcome table against an in-memory store and `push.Fake`, and over a
    real database — delivery to each device once with the send recorded, 410 deletes, 500 records a
    failure and requeues, the threshold retires, an oversized payload is terminal, the nothing-to-do
    completions, and `Enqueue` queuing nothing with push off, without devices or in a rolled-back
    transaction; a push queued for one account and its browser then subscribed by another is not sent
    (J-3); a push whose account is disabled, removed, unapproved or whose project is deactivated after it
    was queued is not sent, the same push in standing is.

- **Notification records (`internal/notification`):** what a notification **is** once it has been
  sent — the record the in-app inbox lists and a tapped push or a mail's link opens, with its frozen
  photo set — plus each account's choice of which kinds it wants through which delivery channel, and the
  inbox reads (listing, unread count, mark-all-read). It sends nothing and serves nothing: delivery is
  `internal/notifydeliver` (over `internal/pushjob` and `internal/mailjob`), the HTTP surface is
  `internal/notificationapi`. Tables `notifications`, `notification_photos`, `notification_prefs`
  (migrations `0087`, `0093` for the channel, `0095` for `notifications.in_app`,
  [`ARCHITECTURE.md`](ARCHITECTURE.md) §5.2). The shape follows `internal/phototask`.
  - **Project scope.** A notification belongs to one project (`notifications.project_id`): `New` carries
    `ProjectUID` (required, `ErrNoProject`) and `ProjectSlug`, and its own page is `Path(slug, uid)` =
    `/p/<slug>/n/<uid>` (`project.AppPath`), so a `SelfLink` opens inside its project. The inbox reads —
    `List`/`UnreadCount`/`MarkAllRead`, and `Get`/`MarkRead`/`Photos` by uid — take `projectUID` after `ctx`
    and see only that project's notifications; another project's is the same `ErrNotFound` as another
    account's. `Photos` keeps its visibility filter. **`notification_prefs` and the push subscriptions stay
    per account and instance-wide**: a person chooses once which kinds reach them through which channel,
    whichever project the notification comes from. The retention purge stays instance-wide too (a pure
    age rule).
  - **Render params.** Every `Render*` function returns, beside the text it wrote in the instance's language,
    the facts it wrote it from — `Text.Params` (`Params{Variant, Count, Name, Album, Title, Place}`; names
    trimmed and cut as the text cuts them, a blank one left `""`; the tagged kind's variants
    `TaggedVariantAlbum`/`TaggedVariantOther`). `internal/notifydeliver` copies them into `New.Params`, the
    store keeps them in `notifications.params` (JSONB, migration `0119`; the zero `Params` is NULL) and every
    read returns them as `Notification.Params` (nil for an older row), so the SPA says the notification in
    the reader's language (`web/src/lib/notificationText`) and falls back to the stored title/body — which
    stay what the push and the mail carried (`TestRender_params`, `TestCreateStoresParams`).
  - **Kinds** are `Kind` string constants — **`KindTagged`** ("you were tagged in photos", its set is those
    photos), **`KindRegistrationPending`** ("a registration is waiting for approval", no photos),
    **`KindAlbumFeatured`** ("a new album is featured on the homepage", its set is the album's cover, the
    link is the album's page), **`KindTaskAssigned`** ("somebody put you on a task", no photos, the
    link is the task's page) and **`KindCommentMention`** ("somebody mentioned you in a comment", no
    photos, the link is the thread scrolled to the comment; `RenderCommentMention(lang, who, MentionPlace,
    title, excerpt)` — „<who> tě zmínil(a) v komentáři u fotky <title>" / „u úkolu <title>", the photo
    named only when it has a title, the excerpt as the body) — in a
    package registry in display order, each with the **channels** it is delivered through
    (`AllChannels()`: **`ChannelInApp`** `in_app`, **`ChannelPush`**, **`ChannelEmail`**, in that order) —
    **every kind offers all three, and every default is off** (`everyChannelOff()`; the 2026-10-11
    product decision "nothing unless asked for" replaced the earlier tagged/registration push-on
    defaults; a stored explicit choice is untouched, only the absent-row default changed). `Kinds()`,
    `Kind.Known()`, `Kind.Channels()`, `Kind.Supports(channel)`, `Kind.Default(channel)`. A new kind is a
    constant + a registry entry + a render function (+ optionally a mail template — delivery is
    `internal/notifydeliver`'s), a new channel a constant + the kinds that offer it; neither `kind` nor
    `channel` carries a CHECK on its values, so neither costs a migration.
  - **`Store.Create(ctx, New)`** writes the notification **and its whole photo set in one transaction**
    (`unnest … WITH ORDINALITY` → zero-based `position`, so the set keeps the given order; a repeated uid
    keeps its first position). Validation first: an account, a known kind (`ErrUnknownKind`), a non-blank
    title ≤ `MaxTitleLen` (200) characters, a body ≤ `MaxBodyLen` (1000), a `Link` that is empty or an
    in-app path (no absolute URL, no `//host`, no `/\host`; ≤ 2048 bytes) — all `ErrInvalid` — and at most
    `MaxPhotos` (5000, `ErrTooManyPhotos`). A missing photograph — **or one of another project**: the set's
    insert joins `photos` on the notification's project and a short row count refuses the whole record — is
    `ErrPhotoNotFound`, a missing account
    `ErrUserNotFound`, and either leaves **nothing** written. It does not consult preferences — the caller
    asks `Wants` first. **`New.InApp`** lists the record in the owner's inbox (`notifications.in_app`);
    false writes a record that only backs a push or a mail. **`CreateTx(ctx, tx, New)`**,
    **`WantsTx(ctx, tx, userUID, kind, channel)`** and **`WantedTx(ctx, tx, userUID, kind)`** (every wanted
    channel in one read) are the same on the caller's open transaction, for a notification that must commit with the mutation that caused
    it (self-service registration in `internal/auth`); a caller that must survive a failed insert runs
    them under a savepoint. **`New.SelfLink`** stores the notification's own page, **`Path(slug, uid)`** =
    `/p/<slug>` + `PathPrefix` (`/n/`) + uid, as its link — the uid is assigned by the store, so a producer cannot spell
    it; the short route is the frontend's deeplink page (it must stay short: it rides in the push
    payload). Setting both `SelfLink` and `Link` is `ErrInvalid`.
  - **`VisiblePhotoSQL`** is the zero `Visibility` (not archived, not hidden, not private) as a predicate
    over `photos p` — the one definition a producer uses to decide whether a photo may be named in a
    notification at all, so what is announced and what the page shows agree (`internal/tagnotifyjob`).
  - **Wording** (`texts.go`) is rendered the way `internal/mailer` renders its mails: a pure function of a
    data struct, in Czech, returning `Text{Title, Body}` — `RenderRegistrationPending` ("Nová registrace
    čeká na schválení", the body naming the person by display name, else username) and
    **`RenderTagged(Language, count)`** ("Označili vás na fotce/fotkách", "Přibyla 1 fotka / Přibyly 3
    fotky / Přibylo 12 fotek, na kterých vás označili." — all three Czech plural forms with the verb
    agreeing, CLDR categories; `LanguageEnglish` "You were tagged in a photo / in 12 photos."; any other
    language is Czech). It names **nobody**: several people may tag inside one window. Its two siblings
    for the window grouped by album: **`RenderTaggedInAlbum(Language, count, album)`** ("Označili vás na
    fotkách v albu Pouť 2026", "Přibylo 5 fotek v albu Pouť 2026, na kterých vás označili."; the album's
    title trimmed and cut at `MaxAlbumNameLen` (60) with an ellipsis so the title stays under
    `MaxTitleLen`, a blank title reading "(bez názvu)"/"(untitled)") and **`RenderTaggedOther(Language,
    count)`** ("Označili vás na dalších fotkách", "Přibyly 3 další fotky, na kterých vás označili."), used
    only beside at least one album notification. **`RenderAlbumFeatured(Language, title)`** ("Nové
    doporučené album: Pouť 2026", the title cut at `MaxAlbumNameLen`) and **`RenderTaskAssigned(Language,
    who, title)`** ("Pan Botka tě přidal(a) k úkolu: Kdy to bylo?" / "Otevři úkol a odpověz."; English
    "Anna added you to the task: …"; the asker's name trimmed, a blank one "Někdo"/"Somebody", the task's
    title cut at `MaxTaskTitleLen` (100) by the shared `shorten`, a blank one untitled).
  - **Owner-scoped reads.** `Get(ctx, ownerUID, uid)`, `MarkRead` and `Photos` all filter by owner, and a
    foreign uid is the **same `ErrNotFound`** as a missing one (the `internal/savedsearchapi` rule), so a
    uid cannot be probed. `Notification.PhotoCount` is how many set members still exist. `MarkRead` is
    idempotent — `COALESCE` keeps the first reading. These answer for any record, listed or not — a push
    and a mail open them by uid.
  - **The inbox** (`inbox.go`) reads only `in_app` rows of the owner: **`List(ctx, ownerUID,
    ListOptions{Unread, Limit, Offset})`** → `Page{Notifications, Total, Limit, Offset}`, newest first
    (`created_at DESC, uid DESC`, stable across pages), limit defaulting to `DefaultListLimit` (30) and
    clamped to `MaxListLimit` (100), a negative offset read as 0, an empty inbox an empty non-nil page;
    **`UnreadCount(ctx, ownerUID)`** (the bell's number); **`MarkAllRead(ctx, ownerUID)`** stamps every
    unread inbox row and returns how many — an already-read one keeps its first reading, a record outside
    the inbox and other accounts are untouched. Two partial indexes (migration 0095) serve the page and
    the count. **`UnreadByProject(ctx, ownerUID, standing)`** is the one inbox read spanning projects —
    the owner's unread in-app count per project, projects with none left out, ordered by uid — narrowed
    by the injected `StandingSQL` (the caller passes `notifystanding.MemberSQL`; this package cannot
    import it, `notifystanding` imports it for the kinds); a nil rule is `ErrNoStanding`, never an
    unfiltered count. Allow-listed in `internal/tenancyguard` with that reason.
  - **`Photos(ctx, ownerUID, uid, Visibility)`** is the only way to read a set, and it makes the caller
    state visibility instead of handing out a raw list to render blindly: the zero `Visibility` drops
    archived, hidden-from-library and private photographs; `IncludeArchived`/`IncludeHidden`/
    `IncludePrivate` let each through. The order is the frozen `position`. A set that is empty or wholly
    deleted yields an empty non-nil slice and no error.
  - **Preferences.** `Preferences(ctx, userUID)` returns one `Preference{Kind, Channel, Enabled,
    IsDefault}` per known kind and channel it supports, in registry order (in-app, push, e-mail), stored
    choices merged over the defaults (a row for a retired kind or channel is ignored);
    `Wants(ctx, userUID, kind, channel)` answers one (`ErrUnknownKind`, or `ErrUnsupportedChannel` for a
    channel the kind lacks). **`ReplacePreferences(ctx, userUID, prefs, audit.Entry)`** replaces every
    stored row (a pair left out returns to its default; a pair named is stored even when it equals the
    default) and writes the audit entry **in the same transaction** — action `notification_prefs.update`,
    target `users`/`userUID` unless the caller set them, the stored choices under `details.preferences`
    keyed `"kind/channel"`. A (kind, channel) named twice is **`ErrDuplicateKind`** (no silent
    last-write-wins), an unknown kind `ErrUnknownKind`, a channel the kind lacks (an empty one included)
    `ErrUnsupportedChannel`, a missing account `ErrUserNotFound`; each writes nothing.
  - **Retention.** `Purge(ctx, projectUID, now, Retention{Read, Unread})` deletes the project's read
    notifications recorded more than `Read` ago and unread ones more than `Unread` ago (both measured from
    `created_at`); another project's rows are never touched; an empty project is `ErrNoProject`, `Unread`
    shorter than `Read`, or a non-positive threshold, is `ErrInvalidRetention`. `DefaultRetention()` is
    30 d / 90 d. It runs daily, one job per project (deactivated ones included), through the `notification_purge` job
    (`internal/notifypurgejob`).
  - The wipe (`internal/reset`) empties `notifications` + `notification_photos` and preserves
    `notification_prefs`. Tests: field rules, the registry, preference merge and audit-entry shaping as
    unit tests; over a real database — atomic create (a missing photo or account leaves nothing), the
    order, a photo-less record, foreign uid = not found for every read, idempotent mark-read, visibility
    per flag, a wholly deleted set, the default merge, an audited replace with the acting user, the
    refused replaces writing neither rows nor audit, the purge at both thresholds, the account cascade,
    and the inbox — owner-only and in-app-only, newest first, stable paging, the unread filter and clamped
    bounds, the unread count, mark-all-read keeping first readings and leaving other records alone.

- **Notification HTTP surface (`internal/notificationapi`):** exposes over HTTP what `internal/push` and
  `internal/notification` already do, with **no business logic of its own** — the endpoints are in
  [`API.md`](API.md). Wired by `buildNotificationAPI` (`cmd/kukatko/notification.go`) inside
  `readAPIOptions`, every route behind `RequireAuth` (all roles).
  - **Interfaces, not stores:** `SubscriptionStore` (`Upsert`/`ListForUser`/`DeleteByID` of
    `push.Store`), `NotificationStore` (`Get`/`MarkRead`/`List`/`UnreadCount`/`UnreadByProject`/`MarkAllRead`/`Photos`/
    `Preferences`/`ReplacePreferences` of `notification.Store`) and `PhotoStore` (`ListByUIDsInProject`).
    The inbox routes (`RegisterInboxRoutes`, mounted under `/p/{project}`) pass the request's project
    (`projectOf(r)`) to every read and mark, so another project's notification is the very 404 a missing one
    gets and the inbox, the unread count and read-all are per project; the preferences ignore the project
    (one set per account) and the push routes stay instance-wide. `RegisterInstanceRoutes` (mounted
    once, `RequireSignedIn`) is `/push/*` plus `GET /notifications/unread-by-project`, which counts
    through `notifystanding.MemberSQL` so only projects the caller is in standing in are named. `Storage` feeds an `internal/mediaurl` builder
    that stamps the media URLs on the returned photos.
  - **Two secrets kept out by construction.** `PushSettings{Enabled, PublicKey}` is all of the push
    configuration the package is given — there is no field the VAPID private key could travel in
    (`pushSettings(cfg)` in the wiring copies the two, and a `cmd/kukatko` test follows a full key pair
    through it to the response). A subscription is only ever serialised through `subscriptionView`, which
    has no `p256dh`/`auth`.
  - **Removal by endpoint without a new store method:** the caller's own rows are listed, the endpoint is
    matched among them and the row deleted with the owner-scoped `DeleteByID` — another account's endpoint
    is a 404 and can never be touched.
  - **Visibility now:** the detail reads the frozen set with the **zero `notification.Visibility`**
    (archived, hidden and private all drop out, whoever the caller is), restores the frozen order after
    `ListByUIDsInProject`, and reports `dropped_count = PhotoCount − visible` (clamped at 0, the two reads being
    separate statements).
  - `/push/*` sits behind `Config.RequireSignedIn` (a device is the account's, so no project is entered;
    nil falls back to `RequireAuth` for a test router), the inbox behind `RequireAuth` in the request's
    project.
  - The subscription write takes an optional `SubscribeThrottle`, mounted inside the auth guard; the wiring
    passes a per-account limiter built from the `ratelimit.comment` settings (its own bucket) with the
    unlimited-token exemption. With push off the write is a 503 before anything is decoded.
  - Tests: unit — the capability response (public key only, nothing when off, no private key), the
    throttle wrapping only the write, the defensive 401s, the subscription view without client keys, the
    body decoders, the user-agent cut, the order/count helpers. Integration over a real database with real
    sessions — the subscription lifecycle (refresh keeps one row, keys never listed, a foreign delete is
    404), unusable subscriptions refused, push off → 503 and nothing stored, the rate limit, preferences
    (defaults, audited replace, refused replaces changing nothing, reset), the detail filtering archived /
    hidden / private in frozen order with counts and media URLs (and un-archiving bringing one back),
    foreign/unknown uid 404 for read and mark-read, idempotent mark-read, the inbox (only the caller's
    in-app records, newest first and paged, the unread filter, the count, read-all leaving another
    account alone, malformed query → 400), anonymous 401 on every route.

- **Tagging window (`internal/tagnotifyjob`):** "you were tagged" — a handful of notifications per
  **window**, grouped by project and then by album, not one per photo. **Project scope:** a notice carries
  no project column — it is its photo's (`tag_notices` is a child of `photos`); the job stays keyed per
  account, but `consumeSQL` returns each photo's `project_id` and the pure **`groupByProject`** (table-tested)
  splits the window into one `projectWindow` per project, ordered by its newest photo; each is announced
  on its own — the recipient's role and the albums read in that project, the notifications in that
  project's inbox with `/p/<slug>/n/<uid>` links, one mail per project — so nothing about one project is
  said inside another. An afternoon of naming faces from a village event is one message per album it was
  filed into, not forty. Table `tag_notices` (migration 0088:
  `(user_uid, photo_uid)` PK, `recorded_at`, both sides `ON DELETE CASCADE`), job type `tag_notify`
  (payload `{user_uid}`, dedup index migration 0089), config `notifications.tags.window` (default **1h**;
  `push.tags.window` is its deprecated alias). The flow is in [`ARCHITECTURE.md`](ARCHITECTURE.md) §8.
  - **`Recorder`** (`NewRecorder(RecorderConfig{Window, Audience, Logger, Now})`, panicking without
    `Audience` — `*notifydeliver.Deliverer`'s `ChannelsTx`) is the `people.TagObserver`. **`Tagged`** records, for every
    account **in standing** (`notifystanding.MemberSQL`) whose membership of the subject's project links the subject (several may —
    migration 0060), one notice, and opens the account's
    window when none is open: **`jobs.EnqueueOrSkip`** of `tag_notify` with `RunAfter = now + window`.
    It records **nothing at all** when the acting account is itself linked to the
    subject (nobody hears about their own work — nor does a second account of the same person), when the
    photo is private, hidden or archived (`notification.VisiblePhotoSQL`; a notification would disclose
    it) or the subject is not actually on it through a valid marker, and for an account that would
    receive `KindTagged` through **no channel** — every channel off (the default), or only the ones the
    instance switched off (so the table does not fill up for somebody who would get nothing). The same
    photo twice is one notice. **`Untagged`** deletes the matching notices of every linked account unless
    the subject is still on the photo through another valid marker, whatever the channels.
  - **"First notice of a window" = no queued job for the account**, checked under a per-account
    **advisory transaction lock** (`pg_advisory_xact_lock(hashtextextended('tag_notify:'||uid, 0))`)
    that the job takes too. So a tag either lands before the job reads (and is in its notification) or
    after it committed (and opens a new window) — never between; a cluster of hundreds assigned in one
    transaction queues **one** job; and a window whose job was dead-lettered self-heals on the next tag,
    which a count of notices would not. A tag landing while the job **runs** opens a fresh window (the
    dedup index is queued-only) — correct, not a bug.
  - **Best-effort, never costs an assignment:** every call runs under a savepoint of the assignment's
    transaction; a failure rolls back to it, is logged, and the assignment commits. A rolled-back
    assignment announces nothing (notice and job share its transaction).
  - **`Service.Handle`/`Close`** (`New(Config{DB, Deliver, Language, Logger})`, panicking on a
    missing dependency; `Deliver` is `*notifydeliver.Deliverer`'s `Deliver` + `Absolute`): in one
    transaction, take the lock, `DELETE … RETURNING` every notice of the account and keep the photos that
    **still** qualify — visible now, still carrying the account's linked person on a valid marker, and the
    account **still in standing** in the photo's project (`notifystanding.MemberSQL` in `consumeSQL`: an
    account disabled, a membership unapproved or a project deactivated inside the window is told nothing —
    review finding J-4; `Deliver` checks again) — (an archive, a merge or an invalidation inside the window
    is caught here too) **newest photo first**
    (`taken_at DESC NULLS LAST, created_at DESC`); nothing left → commit and send nothing (never "0
    photos"). Otherwise **`collect`** reads the recipient's role and, for the surviving photos, the
    hand-made albums (`albums.type = 'album'`, as `whatsnew` and the featured strip count them) each is
    in, and **`groupByAlbum`** (pure, `group.go`) splits the window: one group per album — a **private
    album only when the recipient `CanCurate`**, the rule the album surfaces apply — ordered by the newest
    tagged photo (ties by title, then uid), a photo in two albums in each, only the first
    **`MaxAlbumGroups` (5)** albums kept, then one **"other photos"** group of everything no kept album
    names (no album, an album the recipient may not see, an album past the cap), in the window's order and
    never repeating a photo already named; a window without albums is one plain group. **`announce`**
    delivers one `notifydeliver.Deliver` per group on the same transaction (`KindTagged`, `SelfLink`, the
    group's set capped at `MaxPhotos` with the count matching the set): `RenderTaggedInAlbum` for an album
    group, `RenderTagged` for the only group of a window without albums, `RenderTaggedOther` beside album
    groups. The **e-mail is one per window**: every delivery but the last sets `SkipMail`, and the last
    one's mail (`mailer.TaggedData` with `Groups` — title, count and the absolute link of every group's
    notification, built with `Absolute`; the plain `Count` + `URL` shape for a window without albums) lists
    them all. Every channel turned off meanwhile → consume and send nothing. A crash part-way rolls back
    and the retry re-runs cleanly. A payload naming no account is `worker.Terminal`. **Why a constant
    cap:** it is a sanity bound on a fan-out, not a tuning knob — a window announcing 40 albums separately
    would be the per-photo spam the window exists to prevent, and nobody should need to turn it.
  - Wired in `cmd/kukatko/tagnotify.go`: the recorder into `buildFaceMatch`'s people store and the photo
    API's `Attacher` (a person attached by hand — the only way a video names anybody — is a tagging), the handler
    registered **unconditionally** (so a window always closes; a channel switched off instance-wide
    then delivers nothing). Both share `buildDeliverer` (`cmd/kukatko/notifydeliver.go`). Tests: payload, refusals and defaults as unit tests; over a
    real database — twelve tags collapse into one notification with the right Czech count, newest first,
    its own link and one push per device; a hand attachment records and its detach clears; a second
    window after the first closed; a tag during a run
    opens a new window; an in-flight tag is waited for by the closing job; untagging removes a notice; an
    all-untagged window sends nothing; a photo archived inside the window is left out; nothing recorded
    for self-assignment, private/hidden/archived photos, the kind off, nothing ever chosen, an unlinked
    subject, a disabled account, push off for a push-only account, a rolled-back assignment; an in-app-only
    account gets an inbox record and no push (even with push off), a push-only one a record outside the
    inbox, an e-mail-only one the tagged mail with the right count and link; 300 notices in one transaction queue exactly one job; a
    deleted account's window finds nothing. Grouping (`albums_integration_test.go`): a window of photos in
    no, one and two albums plus a private one, closed for a viewer (the private album is "other") and for a
    curator (its own group), each notification with its own frozen set and page and one push; no albums =
    one plain notification; every photo in one album = no "other"; a folder/moment is not an album; seven
    albums collapse to `MaxAlbumGroups` + "other" with nothing repeated; an untag inside the window still
    un-announces; one `mail_send` per window listing the groups in delivery order with their counts and
    links. Unit (`group_test.go`): the grouping rules, the cap, the order, the mail builder, the wording.
    Standing at close: an account disabled, a membership unapproved or a project deactivated after the tag
    was recorded → no notification, no push, the notices consumed.

- **Featured album announcement (`internal/albumfeaturedjob`):** "a new album is featured" — the
  `album_featured` kind, one notification per account that asked for it when a curator puts an album on the
  homepage's "Doporučujeme" strip. The fan-out never runs in the request: **`Scheduler`** is the organize
  store's `FeatureObserver` (`NewScheduler(enqueue)`, nil = `jobs.EnqueueOrSkip` so a duplicate cannot abort
  the feature's transaction) and queues one **`album_featured_notify`** job, payload `{album_uid}`, inside
  the feature's transaction — the store reports only a *fresh* feature, so prolonging a running one queues
  nothing, and the dedup index (migration 0097) keeps one queued job per album, so feature → unfeature →
  feature while it waits collapses into that job. **`Service`** (`New(Config{DB, Albums, Deliver,
  Language, Logger})`, `Handle` → **`Announce(ctx, albumUID)`**) runs in one transaction: it reads the album
  through `organize.Store.FeaturedAlbumTx` — `ErrNotFeatured` (unfeatured before the run, lapsed, deleted)
  completes with nothing sent — then reads the album's project and walks the **members of that project in
  standing there** (`accountsSQL` embeds `notifystanding.MemberSQL`: an approved membership, an enabled
  account, an active project — a pending registration, a maintainer who is no member and every member of a
  deactivated project are not read; review finding J-1, where a pending member got the project's name and
  the album's title by push and mail) with their role there, through the pure **`recipients`** rules: never `featured_by` (nobody is told about their own work), never a
  disabled account, and a **private album only to curator+** (the strip's own rule); whether an account wants
  the kind is `notifydeliver.Deliver`'s per-account question. Each delivery runs **under a savepoint**, so
  one failing account is logged and skipped, never costing the others. The message: `KindAlbumFeatured`,
  `notification.RenderAlbumFeatured` ("Nové doporučené album: <title>", the title cut at
  `MaxAlbumNameLen`), `ProjectUID` = the album's project, `Link` = `AlbumPath(uid)` (`/albums/<uid>`, stored
  under `/p/<slug>` by `notifydeliver`), `PhotoUIDs` = the album's cover (none
  without one) and the `album_featured` mail (`mailer.AlbumFeaturedData{DisplayName, Title, URL}`) with the
  absolute album link. A payload naming no album is `worker.Terminal`.
  - Wired in `cmd/kukatko/albumfeatured.go`: the scheduler onto the organize store in `buildOrganizeAPI`,
    the handler registered **unconditionally** (like `tag_notify`) over `buildDeliverer`. Tests: payload,
    refusals, the recipient table, the scheduler against a fake queue (duplicate swallowed, failure wrapped),
    the message and mail builder as unit tests; over a real database — feature → job → exactly the viewer and
    the editor who opted in (title, body, album link, the cover as the frozen set, in the inbox) and the
    editor's mail, not the curator, the admin who never chose or the disabled account; prolonging queues no
    second job and a lapsed feature renewed announces again; unfeatured before the run (twice, collapsing
    into one job) sends nothing; a private album reaches the admin and the editor only; nobody opted in →
    no record; a pending member and a maintainer who is no member, both wanting every channel, get nothing
    (no record, no mail) while a member maintainer does; a project deactivated before the run tells nobody.

- **Task hand-over notification (`internal/taskassignnotify`):** "somebody put you on a task" — the
  `task_assigned` kind, one notification to the person asked, **inside the assignment's transaction**.
  **`Notifier`** (`New(Config{Deliver, Language, Logger})`, panicking without `Deliver`) is the task
  store's `AssignObserver`, wired onto the store in `cmd/kukatko/phototask.go` (`buildTaskAssignNotifier`
  over `buildDeliverer`); the store reports only a fresh hand-over, so "already on the task" costs
  nothing here. **`Assigned(ctx, tx, Assignment)`** runs under a **savepoint** of the assignment's
  transaction (the tagging recorder's rule: a notification is worth less than the assignment, so a
  failed delivery is rolled back to the savepoint, logged and swallowed; only a broken transaction is
  returned), reads one row — the task's project and title, the asker's name (display name, else username) and the
  asked account's `disabled` and whether it is **in standing** in the task's project
  (`notifystanding.MemberSQL`: an approved membership of an active project; a maintainer only as a member)
  — and applies the pure **`wanted`** rules: never without an
  actor (a NULL `added_by` is joined by acting, not asked), never the person who asked themselves,
  never a disabled or unapproved account. Whether the person wants the kind at all is
  `notifydeliver.Deliver`'s per-account question (every channel off by default). The message:
  `KindTaskAssigned`, `notification.RenderTaskAssigned` ("Pan Botka tě přidal(a) k úkolu: Kdy to
  bylo?"), `ProjectUID` = the task's project, `Link` = **`TaskPath(uid)`** (`/tasks/<uid>`, stored under
  `/p/<slug>` by `notifydeliver`), no photographs, and the `task_assigned` mail
  (`mailer.TaskAssignedData{DisplayName, Who, Title, URL}`) with the absolute task link. The daily
  `task_digest` mail (`internal/taskdigestjob`) is untouched: a separate digest with its own switch.
  Tests: the rules table, the path, the defaults and the message as unit tests; over a real database —
  an opening asking Anna, Bedřich, the creator and a newcomer who never chose → exactly Anna's and
  Bedřich's notifications (title, body, task link, in the inbox) and Anna's mail; `Assign` once → one,
  re-asked (by anybody) → none, self-assign → none, a disabled account → none, somebody already on the
  task by acting → none, a fresh task → one; the plain store without the hook announces nothing.

- **Mention notification (`internal/mentionnotify`):** "somebody mentioned you in a comment" — the
  `comment_mention` kind, one notification per freshly mentioned account, **inside the comment's
  transaction**. **`Notifier`** (`New(Config{Deliver, Language, Logger})`, panicking without `Deliver`) is
  the comments store's `MentionObserver`, wired onto the one store both threads write through in
  `cmd/kukatko/mention.go` (`buildCommentStore` over `buildDeliverer`, used by `photos.go` and
  `phototask.go`); the store reports only the mentions **new on the comment** and never the writer's own,
  so an edit that fixes a typo costs nothing here. **`Mentioned(ctx, tx, Comment, []Mention)`** runs under
  a **savepoint** of the comment's transaction (a notification is worth less than the remark: a failed
  delivery is rolled back to it, logged and swallowed; only a broken transaction is returned), reads the
  **place** with its project (`photoPlaceSQL`: the photo's title and whether it may be named at all —
  `notification.VisiblePhotoSQL`, not private, hidden or archived; `taskPlaceSQL`: the title, always
  visible) and the mentioned accounts' **standing** in one query (`standingSQL` embeds
  `notifystanding.MemberSQL`: enabled, an approved member of the comment's project, the project active —
  a maintainer only as a member),
  and applies the pure **`wanted(userUID, authorUID, standing, place)`** rules: never the writer, never an
  account out of standing, never for a photo that may not be named — the mention is still highlighted in
  the thread, only the announcement is skipped. Whether the person wants the kind at all is
  `notifydeliver.Deliver`'s per-account question (every channel off by default). The message:
  `KindCommentMention`, `notification.RenderCommentMention` with the writer's resolved name, the place and
  `comments.Excerpt` of the body, `ProjectUID` = the subject's project, `Link` =
  **`CommentPath(subject, commentUID)`** (a path inside the project, stored under `/p/<slug>`)
  (`/photos/<uid>?panel=info#comment-<cuid>` — the drawer must be open for the thread to mount — or
  `/tasks/<uid>#comment-<cuid>`), no photographs, and the `comment_mention` mail
  (`mailer.CommentMentionData{DisplayName, Who, OnTask, Title, Excerpt, URL}`) with the absolute link.
  **Deleting the comment afterwards cancels nothing** — a notification is a frozen record, like a push
  already sent. Tests: the rules table, the paths, the defaults and the message as unit tests; over a real
  database — a photo comment naming Anna, Bedřich, the writer, a newcomer who never chose and a disabled
  account → exactly Anna's and Bedřich's notifications (title with the photo's title, excerpt, anchored
  link, in the inbox) and Anna's mail; a task comment → the task's title and anchor; a private, hidden or
  archived photo → the mention stored, nobody told; an edit → only the newly named account; a maintainer
  who is no member and a pending account → nobody but Anna, and nobody once the project is deactivated.

- **Notification standing (`internal/notifystanding`):** the one answer to **"may this account still
  receive a notification of this project?"** — asked by every producer when it picks recipients, by
  `internal/notifydeliver` when it records and schedules, and again by the `push_send` and `mail_send`
  handlers when a queued delivery runs (an account can fall out of standing while its notification waits).
  Review II findings J-1, J-3, J-4 and J-6 all came down to a producer or a queue asking a weaker question.
  - **The rule:** an **approved member** of the project (`project_members.approved_at`) **whose identity
    is not withheld** (`NOT identity_withheld` — an exact-match add the account has not entered is no
    membership here; round 3 F1: a project admin who knew only an address pushed their own album title
    into that account's inbox), an **enabled** account, an **active** project. A maintainer gets no exception for being a maintainer — only as a
    member — except for the one kind addressed to the instance's operators, `registration_pending`
    (`ReachesMaintainers(kind)`): an enabled maintainer of an active project, member or not, because a
    maintainer may approve a registration into any project.
  - **`MemberSQL(userExpr, projectExpr)`** returns the rule as an `EXISTS (…)` condition over SQL
    expressions of the enclosing statement (`"u.uid"`, `"$1"`, `"s.project_id"`) — embedded by
    `albumfeaturedjob` (`accountsSQL`), `tagnotifyjob` (`recipientsSQL`, `consumeSQL`), `mentionnotify`
    (`standingSQL`), `taskassignnotify` (`contextSQL`) and `phototask`'s digest query, so the rule is written
    once. **`MaintainerSQL`** is the registration kind's second audience.
  - **`InStanding(ctx, q, Addressee{UserUID, ProjectUID, Kind})`** asks it on any `Querier` (a pool or the
    producer's `pgx.Tx`); an addressee without an account or a project is never in standing. **`Checker`**
    (`NewChecker(pool)`, panicking on nil) is the same on a pool for the queue handlers.
  - **Drops are counted**, not logged only: `CountDrop(Stage, Reason)` into process-wide atomic counters —
    stages `deliver` (notifydeliver), `push`, `mail`; reasons `standing`, `device_moved` (a push whose browser
    subscription now belongs to another account), `unaddressed` (a push queued before pushes carried an
    account). `Drops()` lists every pair, zeros included; `serve` exports it as
    `kukatko_notifications_dropped_total{stage,reason}` (`metrics.RegisterNotificationDrops`). Process-wide,
    like the runtime statistics, because the drops happen in deliverers built in many places.
  - **Accepted queue semantics, documented:** a notification already *sent* (pushed, mailed, recorded) is
    not recalled when somebody falls out of standing later; what is still queued is dropped at send.
  - Tests: `Valid`, `ReachesMaintainers` over every kind, the fragment's parts, the question asked (member
    vs maintainer form, none for an incomplete addressee, a failure passed on), the counters; over a real
    database the whole rule — approved member, pending, disabled, another project only, a deactivated
    project, a maintainer who is no member, a member maintainer, the registration kind for a maintainer of
    an active and of a deactivated project — and `MemberSQL` agreeing with `InStanding`.

- **Notification delivery (`internal/notifydeliver`):** the **one way a notification leaves Kukátko**.
  A producer builds a `Message{ProjectUID, Kind, Text, Link | SelfLink, PhotoUIDs, Mail, SkipMail}` and
  calls **`Deliverer.Deliver(ctx, tx, userUID, msg)`**; it never checks a preference or touches a queue
  itself. `New(Config{Notifications, Projects, Push, Mail, BaseURL, Standing, Logger})` (panicking on a missing
  dependency; wired once as `buildDeliverer` in `cmd/kukatko/notifydeliver.go` over `notification.Store`,
  `project.Store`, the push and mail enqueuers and `mail.base_url`).
  - **Project and deep links.** `Message.ProjectUID` is required (`notification.ErrNoProject`, nothing
    delivered) and `Message.Link` is the path **inside** the project (`/tasks/tk1`); `Deliver` loads the
    project (`Projects.Get` = `project.Store.Get`) and stores the link under its root (`projectLink` →
    `/p/<slug>/tasks/tk1`), a `SelfLink` as `/p/<slug>/n/<uid>` — so the record, the push's `URL` and the
    mail's absolute link all open in the notification's project, and no producer spells or hard-codes a
    slug. The project's **name** travels as well: the push's title is `PushTitle(name, title)` =
    `"<name> · <title>"`, and every `MailBuilder` receives a `MailContext{URL, ProjectName}` (the absolute
    link and the name its template puts into the subject — `mailer.InProject`: `"Kukátko · <name>: …"` —
    and the body). Producers: the tagging window (the photos' project), mentions (the subject's), task hand-over
    (the task's), featured albums (the album's), registration (the project registered into).
  - **`ChannelsTx(ctx, tx, userUID, kind)`** → `Channels{InApp, Push, Email}`: the channels the account
    wants (`notification.Store.WantedTx`, one read) **and** the instance can deliver — push counts only
    with `push.enabled`, e-mail only with `mail.enabled`, in-app always. The tagging recorder asks it
    before it collects anything.
  - **Standing first.** Before anything else `Deliver` asks `notifystanding.InStanding` on `tx` (or the
    `Config.Standing` stub a unit test injects) whether the account may get a notification of
    `msg.ProjectUID` at all — an approved member, enabled, the project active; a maintainer only as a member,
    except the `registration_pending` kind. Out of standing → nothing, no record, zero `Result`, counted
    (`StageDeliver`). Every producer delivers through here, so this one check covers them all.
    The push and the mail it queues are **addressed** (`notifystanding.Addressee{user, project, kind}`), and
    their handlers ask the same question again when they run.
  - **`Deliver`**: no channel → nothing at all, no record (zero `Result`). Otherwise **one record**
    (`CreateTx`, `InApp` = whether in-app is among the channels) — written for any channel, since the push
    opens it and the mail links to it, and kept out of the inbox unless in-app was wanted; then the push
    (`pushjob.Enqueuer.Enqueue`, `Tag` = the record's uid, `URL` = its link) when push is on the list, and
    the mail when e-mail is: the recipient's address and display name read on `tx`, the message built by
    `Message.Mail` (or **`GenericMail`**, the `notification` template carrying title, body and the link)
    with the link made absolute on `BaseURL` (empty without one). An account with a blank address is not
    mailed and the other channels still deliver; a `.invalid` placeholder is `mailjob`'s skip. Every write
    joins `tx`; a failure is returned wrapped and leaves `tx` aborted — a best-effort caller runs it under
    a savepoint. `Result{Channels, Notification, Devices}` says what happened.
  - Adding a kind = a registry entry in `internal/notification`, a render function for its text and
    optionally a `MailBuilder` + template; nothing here changes. Producers: `internal/tagnotifyjob`,
    `internal/albumfeaturedjob` and self-service registration (`internal/auth`). Tests (unit, fakes for the store, both queues and the
    transaction's recipient read): the fan-out table — none, in-app only, push only, mail only, all — with
    the record and its in-app flag, the push and generic-mail payloads (absolute link), a kind's own mail
    builder without a base URL, both instance switches off (no record), a blank address skipping only the
    mail, the wrapped failures, the required dependencies; both queued deliveries addressed to the account,
    project and kind; an account out of standing getting nothing on any channel (counted); a failed
    standing query failing the delivery.

- **Notification retention (`internal/notifypurgejob`):** the `notification_purge` job and its scheduler,
  **one purge per project** (a library-wide job). `New(Config{Notifications, Retention, Clock, Logger})` —
  `Handle` runs `notification.Store.Purge(ctx, projectUID, now, retention)` for the project the payload
  names (`jobs.ProjectUIDOrLegacy`; a legacy payload naming none = the bootstrap project, logged) as of the clock with
  `notification.DefaultRetention()` (read 30 d, unread 90 d) unless a retention is given; the purge deletes
  only that project's rows (`notifications.project_id`); a failure goes back to the queue.
  `NewScheduler(enqueuer, projects, interval, logger)` enqueues through
  `jobs.Enqueuer.EnqueueNotificationPurge(ctx, projectUID)` one purge per project — **a deactivated one
  included** (`Tick`; a failed project does not stop the others) **at startup and every `Interval` (24 h)**
  after, until its context ends; a failed round is logged and the loop goes on. Dedup:
  `idx_jobs_notification_purge_dedup` (migration 0108, on the type alone since 0096) keeps at most one
  **queued** purge per project, so restarts cannot pile them up. Wired in `cmd/kukatko/notifydeliver.go`
  (`buildNotifyPurgeService` registered unconditionally, `buildNotifyPurgeScheduler` started by `serve`).
  **A deactivated project is decided as "normal retention, out of reach meanwhile"**, not "purged at
  deactivation": its members get a 404 at `/p/<slug>/` so its inbox is unreachable, every push or mail
  still queued for it is dropped at send (`internal/notifystanding`), and the daily purge then deletes its
  notifications at the usual ages — a project switched back on finds its recent inbox as it was. Before
  the review fix (J-6) the scheduler skipped deactivated projects and their notifications lived forever.
  Tests: the retention defaulting, the clock, the payload's project and the legacy fallback, the returned
  failure, the scheduler's startup + periodic enqueues surviving failures, one enqueue per project (the
  deactivated one included), its defaults; over a real database one round over the real project list
  purges a deactivated project's old notification and keeps its fresh one; the dedup is covered in
  `internal/jobs`' integration tests.

- **Projects (`internal/project`):** the first step of multi-tenancy
  ([design](superpowers/specs/2026-10-11-multi-tenancy-design.md)) — the `projects` table (migration
  0100), its store, and since migration 0101 the **membership** (`project_members`) and its store. A
  project is one independent library of the instance; the catalogue does not reference it yet (no
  `project_id`, no HTTP route — later steps), and nothing reads the membership yet either.
  - `models.go` — `Project` (mirrors the row), `CreateParams`, `UpdatePatch`, `Usage`; the constants
    `BootstrapUID = "pj0000000000000000veselice"` / `BootstrapSlug = "v"` (the row migration 0100 inserts:
    Veselice, active, `storage_prefix = ''`, no limits — the library as it was before multi-tenancy),
    `TargetType = "project"`; `ValidateSlug` (`^[a-z0-9][a-z0-9-]{0,39}$`, identical to the CHECK, plus
    the reserved words `p`, `api`, `u`, `invite`, `system`, `login`, `register`, `projects`, `account`,
    `healthz`, `metrics` — path segments the app itself needs next to `/p/<slug>/`), `ValidateStoragePrefix`
    (empty, or relative and ending in `/` — the CHECK again), `StoragePrefixFor(uid)` = `projects/<uid>/`;
    `Active(list)` keeps the active projects (what a scheduler enqueues one job per), `ForKey(list, key)` names
    the project whose storage layout an object key belongs to (the longest non-empty prefix it is under, else
    the empty-prefix root-layout project; false when none claims it);
    `AppPath(slug, path)` = `/p/<slug>` + path, the in-app address of a project's page that every deep link
    leaving the app (push, e-mail, a notification's link) carries (`AppPathPrefix = "/p/"`);
    the `pj` uid generator (the `internal/photos` pattern: 24 base32 chars, 26 total). Sentinels
    `ErrNotFound`, `ErrSlugTaken`, `ErrInvalidSlug`, `ErrInvalidName`, `ErrInvalidLimit`,
    `ErrInvalidStoragePrefix`.
  - `store.go` — `Store` over the pgx pool. Reads: `Get(uid)`, `GetBySlug(slug)` (unknown = `ErrNotFound`),
    `List` (by name, then uid). Writes, each **audited in its own transaction** with the caller's
    `audit.Meta` (the package must not import `internal/auth`, so the actor is passed in):
    `Create` → `project.create` (fresh `pj` uid, `storage_prefix = projects/<uid>/` — **a created project
    never has the empty root prefix**, only the bootstrap row does; name/description trimmed; the slug's
    unique violation → `ErrSlugTaken`; in the same transaction the project's `instance_settings` row with
    the column defaults — registration closed, empty secret, empty welcome — and, when
    `CreateParams.CreatorUID` is set, the creator's **approved `admin` membership** with its own
    `project.member.add` entry; an unknown creator rolls it all back with `ErrUserNotFound`). The
    package-level **`CreateTx(ctx, tx, meta, p)`** is the same on a caller's transaction (`Create` is
    begin → `CreateTx` → commit), so `internal/auth`'s `ProjectSetup` creates the project together with
    its first admin's account and rolls back with it; `Update(uid, UpdatePatch)` → `project.update` with the
    `audit.ChangesKey` old → new diff, row locked `FOR UPDATE` for the read-modify-write; **the slug is
    immutable** (no field for it); a limit is set by value or removed by its `Clear…` flag (both at once =
    `ErrInvalidLimit`); a patch that changes nothing writes neither the row nor an audit entry;
    `SetStoragePrefix(uid, prefix)` → `project.storage_prefix` (details `old`/`new`) — the only way the
    prefix changes, meant for the storage move. `Usage(uid)` = photo rows + `sum(file_size)` (archived
    included: their originals are still stored until purged) of **that project's** photos
    (`photos.project_id = uid`). **Every audit entry of the package — project and membership mutations
    alike — carries the project it touched as `audit.Entry.ProjectUID`** (`projectEntry`), so a
    project's own trail lists its governance.
  - `overview.go` — the system administrator's reads: `Overviews()` / `OverviewOf(uid)` return `Overview`
    (the `Project` embedded + `MembersCount`, `PhotosCount`, `OriginalsBytes`) from one query — the member
    count a scalar subquery, the usage a lateral sum identical to `Usage`'s; `AllMemberships()` maps every
    user uid to its `MembershipSummary` list (by project name) in one query, instead of one
    `MembershipsOf` per account, each with its linked person and that person's name (`SubjectName`,
    looked up in the membership's own project; `MembershipsOf` leaves it empty).
  - `member.go` — the per-project `Role` (`RoleViewer` < `RoleCurator` < `RoleEditor` < `RoleAdmin`, a
    strict ladder like `internal/auth`'s **without maintainer**, which stays the instance role on `users`):
    `ParseRole` (anything else, `maintainer` included = `ErrInvalidRole`), `Valid`, `CanCurate` (curator+),
    `CanWrite` (editor+), `IsAdmin` (admin only) — each predicate names its roles explicitly. `Member` (the
    row joined with `users.username`/`display_name`/`last_login_at`, and `Maintainer` — the account is the
    instance's system administrator, withheld like the identity), `MembershipSummary` (`ProjectUID`,
    `Slug`, `Name`, `Description` (filled by `MembershipsOf` only), `Active`, `Role`, `Approved` — what the
    project chooser lists),
    `MemberTargetType = "project_member"`. Sentinels `ErrNotMember`, `ErrAlreadyMember`, `ErrLastAdmin`,
    `ErrUserNotFound`, `ErrSubjectNotFound`. `Member.HasPicture` (`json:"has_picture"`) is computed in
    `memberColumns`: a `user_pictures` row or the membership's own `subject_uid` — "worth asking for the avatar".
    `Member.IdentityWithheld` (`project_members.identity_withheld`, migration 0117) marks an exact-match add
    the account has not entered yet: `memberColumns` then reads `Username`/`DisplayName` as `""`,
    `LastLoginAt` nil and `HasPicture` false, for every reader of a membership. A withheld membership is
    **no membership to the project**: not listed (`Members`), no working admin, not in notification
    standing (`internal/notifystanding`), not a current member for the former-member rule (comments,
    mentions, task participants), the review leaderboard/streaks and the project audit's `?user=` filter.
  - `member_store.go` — reads: `Members(projectUID)` (by display name, the username standing in for an empty
    one; **withheld memberships are left out**, so the list after an add that found an account equals the
    list after one that found nobody; unknown project = `ErrNotFound`, no members = empty slice), `Membership(projectUID, userUID)`
    (`ErrNotMember`), `MembershipsOf(userUID)` (by project name, inactive projects included, each with the membership's
    `SubjectUID` — what `GET /auth/me` lists per project). Writes, each
    **audited in its transaction** with the caller's `audit.Meta` (target type `project_member`, target uid =
    the account, details `project_uid` + the role): `AddMember(…, role, approved)` → `project.member.add`
    (an existing row is left untouched and `ErrAlreadyMember` returned, nothing audited; unknown project =
    `ErrNotFound`, unknown account = `ErrUserNotFound`); `SetRole` → `project.member.role` (+
    `previous_role`); `Approve` → `project.member.approve` (an approved member keeps its original time;
    `ApproveWith(…, hook ApproveHook)` runs `hook(ctx, tx, member)` on the approval's transaction when it
    really changes something — how `internal/memberapi` queues the approval mail); nothing is stamped on
    the account — its approval is derived from its memberships (`auth`'s `accountApprovedSQL`);
    `RemoveMember` → `project.member.remove`; `SetSubject(…, *string)` → `project.member.subject` (+
    `previous_subject_uid`; nil clears; a subject that is not the project's — unknown, or another
    project's — is `ErrSubjectNotFound`, checked on the write's transaction with the query
    `internal/auth`'s `checkSubject` runs). A change to nothing writes
    neither the row nor an audit entry. Every write **locks the project row** first, so concurrent changes
    to one project's members are serialised and the **last-admin guard** cannot be raced: `SetRole` away
    from admin and `RemoveMember` of an admin return `ErrLastAdmin` when the member is the project's last
    **working** admin — only approved memberships whose account is not disabled and whose identity is not
    withheld count (`workingAdminsSQL`), so a pending, disabled or withheld "admin" row neither lets the
    last working one go nor is itself ever refused (a withheld row counting only while its hidden account is
    enabled would make the sole admin's removal an oracle on that account)
    (who may remove whom — themselves included — is the HTTP layer's rule, later). Disabling an account
    is `internal/auth`'s write and is not refused here; it notes the projects it strands instead.
    `TouchLastProject(userUID, projectUID)` writes `users.last_project_uid` (not audited, no membership
    check; unknown account = `ErrUserNotFound`, unknown project = `ErrNotFound`). The read side,
    `auth.Store.LastProjectSlug` (`GET /auth/me`'s `last_project_slug`), checks instead: it answers the
    slug only while the account can still enter the project (a maintainer, or an approved membership of
    an active project) — a removed, pending or deactivated-out account gets `""`.
    `RevealIdentity(projectUID, userUID)` clears `identity_withheld` (a no-op on a row without it, or no
    row; not audited — it records the account's own act). `internal/auth`'s `enterProject` calls it the
    first time a withheld member enters the project.
  - `member_identifier.go` — `AddByIdentifier(meta, projectUID, identifier, role) (AddOutcome, error)`, the
    project admin's exact-match add: one transaction locks the project, resolves the identifier as an
    exact username (trimmed + lower-cased, as sign-in normalises it) or else a case-insensitive e-mail
    address (`LIMIT 2`: two hits = `OutcomeAmbiguous`, nobody added), inserts the **approved** membership
    **with `identity_withheld`** (`addWithheldMemberSQL`) and **always** writes two entries (`addEntries`):
    `project.member.add_request` under the project, identical for every outcome (`{project_uid, role}`, no
    target — the project's admin reads that trail), and the **instance** entry `project.member.add_resolve`
    **without an actor** (so it is not in the admin's `/audit/mine` either; `requested_by` names the admin)
    with `details.outcome` (`added`/`already_member`/`no_such_account`/`ambiguous`; the typed `identifier`
    when nobody was resolved, the account as target when one was). The outcome is for the maintainer's
    trail and the tests, never for the person who asked. `ErrEmptyIdentifier` for a blank one.
    **Why approved-but-withheld rather than a pending invitation the account accepts:** the design (§4.1)
    says an exact-match add makes the account a member ("if that account exists, it has been added"),
    and a consent step would be a second invitation flow beside `internal/invite`. Withholding the
    identity removes what the add leaked — the name, the last sign-in anywhere on the instance, the
    picture, the trail's outcome. Round 3 (F1) showed that even a nameless row leaks: it appears only on
    a hit (an existence oracle), and it carried the real uid, which fetched the account's picture and let
    the project's notifications reach it. So a withheld membership is not listed at all, and the undo is
    as blind as the add (`member_withdraw.go`).
  - `member_withdraw.go` — `WithdrawByIdentifier(meta, projectUID, identifier) (WithdrawOutcome, error)`,
    the blind undo of an exact-match add: one transaction locks the project, resolves the identifier
    exactly as the add does (`matchAccount`) and deletes the membership **only while
    `identity_withheld`** (`withdrawWithheldMemberSQL`; a visible membership is never touched — it is
    removed by uid). Always two entries (`withdrawEntries`): `project.member.withdraw_request` under the
    project (`{project_uid}` only, no target) and the actor-less instance entry
    `project.member.withdraw_resolve` with `details.outcome` (`withdrawn`/`not_withheld`/`no_such_account`/
    `ambiguous`; the typed `identifier` when nobody was resolved). `ErrEmptyIdentifier` for a blank one.
  - `member_check.go` — **the one rule for naming an account in a project**: `ApprovedMemberSQL(projectExpr,
    userExpr)` is the SQL predicate (an `EXISTS` over `project_members … approved_at IS NOT NULL AND NOT
    identity_withheld` — a withheld member cannot be named until it enters; both
    arguments are SQL expressions pasted verbatim, never user input) and `RequireApprovedMembers(ctx, q,
    projectUID, uids...)` its Go check on a pool or a transaction (`RowQuerier`) — `ErrNotMember` unless every
    distinct uid is an approved member, a uid with no account included, so a caller answering both alike is no
    account-existence oracle. Every write that stores somebody else's account uid where the project displays
    it uses it: task participants (`internal/phototask` `ask`), mention resolution (`internal/comments`
    `resolveMentionsSQL`), the import's `--uploader` (`cmd/kukatko` `resolveUploader`) and the public upload
    link's crediting (`Store.IsApprovedMember`, wired as `uploadlinkapi.Config.Members`: a signed-in
    non-member uploads like an anonymous guest). A pending member and a maintainer outside the project do
    not pass.
  - `member_former.go` — **the one rule for showing a stored account by name in a project**:
    `FormerMemberSQL(projectExpr, userAlias)` is true for an account row with no membership of the project
    whose identity the project may see (none, or only a withheld exact-match add) — a system administrator
    included, whose implicit access a project must not learn of — a **former member**, also an account credited before the upload link checked membership; NULL for a
    missing LEFT JOINed account. `ShownNameSQL` is its name column (display name, else username, `''` for a
    former member or a missing account). Read by the task creator/closer/last actor (`internal/phototask`),
    the uploader facet and the `uploader:` filter's name arms (`internal/photos`), the photo detail's uploader
    (`auth.Store.ProjectUserName`), upload-link provenance and creators (`internal/uploadlink`) and invite
    creators (`internal/invite`); each reports the flag so the client says "former member". The comments
    package and `phototask/participants.go` spell the same predicate inline, and `audit.ProjectActorNames`
    writes it out because `internal/audit` sits below this package.
  - `member_share.go` — **the one rule for showing one account's profile to another** (SEC-025):
    `SharesProject(ctx, projectUID, viewerUID, accountUID)` holds when both accounts pass
    `ApprovedMemberSQL` in one **active** project — `projectUID`, or any project when it is `""`. Asking
    with the same uid twice answers "is this account such a member of the project". `internal/userpic`
    gates the whole picture chain on it, and the membership read's `has_picture` (`memberColumns`) is
    `false` for a pending membership to match.
  - `internal/reset` keeps `projects` and `project_members` among the **preserved** tables (account-side,
    like `users`); `project_members.subject_uid` points into the wiped `subjects`, so the reset lifts,
    nulls and restores `project_members_subject_uid_fkey` (`subjectLinks` in `reset.go`; the
    `users_subject_uid_fkey` it once handled too went with `users.subject_uid` in 0110), and
    `dbtest.TruncateAll` re-seeds the bootstrap row after truncating, so a truncated test database looks
    like a freshly migrated one.
- **System administrator's projects API (`internal/projectapi`):** the instance-level `/projects` and
  account routes (see `docs/API.md`, *Projects (maintainer)*), mounted once with `server.WithAPI` in
  `readAPIOptions` (`cmd/kukatko/projects.go` → `buildProjectsAPI`), every route behind the injected
  `RequireMaintainer`. Depends on three interfaces so the handlers unit-test over fakes: `Projects` (the
  `*project.Store` methods it calls), `Accounts` (`*auth.Store`: `ListUsers`, `GetUserByUID`,
  `SetInstanceRoleAudited`) and `Setup` (`*auth.ProjectSetup`: `Create`, `ResendLink`,
  `AwaitingPassword`).
  - `projectapi.go` — `Config`, `NewAPI`, `RegisterRoutes` (`/projects` as a sub-router; `GET /users` and
    `PATCH /users/{uid}` flat, because `GET /users/{uid}/avatar` shares the prefix under another guard).
  - `projects.go` — list/get/create/update, the two membership routes and the set-password pair
    (`GET /projects/{uid}/awaiting-password`, `POST /projects/{uid}/members/{user_uid}/password-link`).
    The create passes the caller as `CreatorUID` — unless the body carries `admin`, when
    `handleCreateFor` hands it to `Setup.Create` and answers `createResponse{Overview, FirstAdmin}`
    (the auth refusals map to `invalid_email`, `invalid_username`, `username_taken`, `email_ambiguous`,
    `user_disabled`, `password_set` in `respond.go`); the PATCH body tells an absent limit from `null` from a number through `optionalLimit`
    (its `UnmarshalJSON` runs for an explicit `null` too) and refuses a `slug` key with `slug_immutable`
    before touching the store; a membership is always added **approved**.
  - `users.go` — the account list (`ListUsers` joined with `AllMemberships`: the record incl. `note`,
    `approved_at`, `last_login_at`, and per membership the linked person, read-only) and the
    instance-role change:
    `self` when a maintainer would demote themselves, a no-op (no audit row) when the role is unchanged,
    otherwise `auth.Store.SetInstanceRoleAudited` with a `user.instance_role` entry (`instance_role`,
    `previous`) under the store's last-maintainer guard.
  - `respond.go` — the error codes and `classify`, one table mapping every `project`/`auth` sentinel to
    status + code (the slug and limit rules carry the store's own explanation as `message`); anything else
    is a logged `500 internal`.
- **Project members API (`internal/memberapi`):** a project admin's `/members` (see `docs/API.md`,
  *Members*), project-scoped and mounted **only under `/p/{project}`** (`projectAPI.members`; `aliasAPI`
  blanks it like the name directory), every route behind the injected `RequireAdmin`
  (`cmd/kukatko/members.go` → `buildMemberAPI`, wired in `readAPIOptions`). Interfaces so the handlers
  unit-test over fakes: `Members` (the `*project.Store` methods), `Accounts` (`auth.Store.GetUserByUID`,
  for the mail's address) and `MailScheduler` (`mailjob.Enqueuer`).
  - `memberapi.go` — `Config` (+ `ProjectURL func(slug)`, built from `mail.base_url` by `projectURL` in
    `cmd/kukatko/auth.go`; + `Throttle`, the per-caller `ratelimit.member_add` limiter `buildMemberAPI`
    wraps the two blind routes in, nil = unthrottled), `NewAPI`, `RegisterRoutes`.
  - `handlers.go` — the six routes. Every route addressing a member by uid reads it through
    `visibleMember` first, which answers a membership whose identity is withheld with `ErrNotMember` —
    404 `not_member`, byte-identical to a uid that is no member — so an admin who knows the account's uid
    from elsewhere cannot confirm whose address the blind add found. `POST /members/withdraw` answers the
    add's one `202` whatever `WithdrawByIdentifier` reports. The project is always `auth.ProjectUIDFromContext`; the self rules
    are the handler's (`PATCH` of the caller's own uid → `self` before the store; `DELETE` of oneself maps
    the store's `ErrLastAdmin` to `self`). A system administrator's membership is an ordinary one: neither
    `project.Member` nor the member JSON carries the instance role, so the project never learns of it. `POST /members` answers the one `202 {"status":"accepted"}`
    whatever `AddByIdentifier` reports. The member JSON's time is `last_seen_at`
    (`project.Member.LastSeenAt`, the membership's own visit clock), never the account's `last_login_at`,
    which would show the project's admin when the account was active elsewhere. `approvalMail` builds the `project.ApproveHook` that reads the
    account and enqueues `mailjob.AccountApproved` (project name + `/p/<slug>/` link) on the approval's
    transaction, **addressed** to the membership (`Mail.Addressed`, so the send re-checks it) — nil without
    mail, so an approval then sends nothing. The member JSON carries no address.
  - `respond.go` — the error codes and the sentinel → status table; anything else is a logged `500`.
- **Invitation links (`internal/invite`):** the domain of `project_invites` (migration 0109): a short
  code (the upload links' `uploadlink.NewCode`/`ValidCode`/`HashSecret`, looked up by hash, kept
  readable), a project role, `expires_at` (`NewInvite.Days`, 0 → `DefaultDays` 7, at most `MaxDays` 90),
  an optional `max_uses`, `uses`, final `revoked_at`, `created_by` (SET NULL). Catalogue-side for the
  reset (`internal/reset/tables.go`); the project's deletion cascades.
  - `models.go` — `Invite` (`Code`, `ProjectUID/Slug/Name` never serialised), `StateAt`
    (`revoked` > `expired` > `used_up` > `active`), `NewInvite.Validate` (`ErrInvalidRole`,
    `ErrInvalidDays`, `ErrInvalidMaxUses`), `Acceptance`, `RegistrationRole` (admin → editor) and the
    local role ladder `rank`/`higher` that "raise, never lower" needs.
  - `store.go` — `Store` (`NewStore(pool)`, clock `time.Now`): `Create(ctx, projectUID, NewInvite, meta)`
    (code redrawn on a hash collision; `project.invite.create` in the transaction), `Get`/`List` (scoped
    to the project; another project's uid is `ErrNotFound`), `Revoke` (`project.invite.revoke`; repeat =
    no-op) and `Resolve(ctx, code)` — instance-wide by code, **`ErrInvalid` for every unusable reason**
    (malformed, unknown, expired, revoked, used up, deactivated project).
  - `accept.go` — `Accept(ctx, code, userUID, meta)` in one transaction: locks the invite **and its
    project** (`FOR UPDATE OF i, p` — the project row is the membership mutex `internal/project` takes),
    then the membership; an approved membership at or above the invite's role → `Changed: false`, nothing
    written (idempotent, even past the cap); otherwise the cap is checked, the membership upserted
    (`higher(current, invite)`, approval filled), one use counted and `project.invite.accept` written.
    `RedeemTx(ctx, tx, code, userUID, meta)` is the registration half on the account's transaction: the
    same lock and checks, one use, the audit entry with `RegistrationRole` (auth inserts the membership).
- **Invites API (`internal/inviteapi`):** the HTTP half (see `docs/API.md`, *Invites*), wired by
  `cmd/kukatko/invites.go` (`buildInviteAPI` in `readAPIOptions`, `inviteGate` in `buildAuth`'s
  `APIConfig.Invites`). `RegisterRoutes` is the project-scoped `/invites` (`projectAPI.invites`,
  blanked by `aliasAPI`, `RequireAdmin`); `RegisterPublicRoutes` the instance-level `/invite/{code}`
  (`IPLimit` = `ratelimit.invite` ahead of everything, `OptionalAuth` on the page, `RequireSignedIn` on
  the accept). `Store` is an interface over `*invite.Store` so the handlers unit-test with a fake.
  - `handlers.go` — the five handlers; the view adds `code`, `url` (`Config.BaseURL` = `mail.base_url` +
    `InvitePath` + code) and `state`; `days: 0` is refused before the store.
  - `gate.go` — `RegistrationGate` implements `auth.InviteGate` over a `Redeemer` (`Resolve` +
    `RedeemTx`): the grant's role is `invite.RegistrationRole`, its `Redeem` maps `ErrInvalid` to
    `auth.ErrRegistrationInvite`.
  - `respond.go` — the error codes; `ErrInvalid` and `ErrNotFound` share the one `404 not_found`, anything
    unknown is a logged bare `500 internal`.
- **Old-link resolver (`internal/resolveapi`):** `GET /resolve/{kind}/{uid}` (see `docs/API.md`,
  *Old-link resolver*), wired by `cmd/kukatko/resolve.go` (`buildResolveAPI` in `readAPIOptions`,
  `RequireSignedIn`, throttled per caller from the `ratelimit.comment` settings in a bucket of its own).
  - `store.go` — `Store.ProjectSlug(ctx, kind, uid, Caller)`: one statement per kind (photos, albums,
    labels, photo_tasks, subjects, notifications), each joining the entity to `projects` and keeping it
    only when the caller may enter the project — the project guard's rules (`auth.enterProject`)
    restated in SQL: `$2` maintainer, or an approved `project_members` row of an active project; a
    notification also needs `user_uid = caller`. The statements span every project on purpose and are
    allow-listed in `internal/tenancyguard/allow.txt`. Every miss — unknown kind, empty uid, no row,
    no access — is the one `ErrNotFound`.
  - `resolveapi.go` — the handler: `ErrNotFound` → the one `404 {"error":"not found"}`, any other error a
    logged 500. `Resolver` is an interface so the handler unit-tests without a database; the access rules
    are covered by `resolveapi_integration_test.go` over `projecttest.Seed` (foreign = missing via
    `AssertSameShape`, pending/approved/deactivated memberships, maintainer, addressee, 401, 429).
- **Tenant-isolation harness (`internal/projecttest`):** test support, **compiled only under the
  `integration` build tag** (so `make check` neither builds nor lints it — run
  `golangci-lint run --build-tags=integration ./internal/projecttest/...` after changing it). It exists
  because the biggest risk of multi-tenancy is a forgotten `WHERE project_id`, and every scoping step
  proves the opposite with the same helper.
  - `Seed(t, pool, opts...) *Fixture` (call it after `dbtest.TruncateAll`, which leaves the bootstrap
    project in place) — side **A** = the bootstrap project (`project.BootstrapUID`, slug `v`), side **B**
    = a project it creates with `project.Store.Create` (slug `b-<random>`, prefix `projects/<uid>/`). In
    each: one approved member (admin; `WithRole(SideB, project.RoleViewer)` changes it; `AsMaintainer(side)` makes
    the account an instance maintainer too, for the maintainer-only project routes) who belongs to
    **that project only** (the account is created with no membership, then `AddMember`), one photo with
    its primary `photo_files` row (no object in storage), a person with a face marker on it, an album
    holding it, a label on it, a task over it, the member's saved search and an upload link. The rows
    are plain SQL naming `project_id` **explicitly**, never through the stores: the stores are what the
    scoping steps change, and a seed through an unscoped store would land in whatever project it
    defaults to — the very bug the harness exists to catch. Every uid is on the `Tenant`
    (`f.A.PhotoUID`, `f.B.AlbumUID`, `f.A.UserUID`, `f.A.Slug`, `UploadLinkCode`, …); each title/name
    contains the tenant's random `Token`, so a search for it should match one side only;
    `Tenant.UIDs()` lists everything that must never surface on the other side (the slug is left out —
    `v` matches any body).
  - `f.ClientFor(t, server, side) *http.Client` — signed in through `POST /api/v1/auth/login`, a cookie
    jar of its own on `apitest.Client()`'s pool-free transport (the spec's `*apitest.Client` is that
    client: `apitest` exports a function, not a type). The fixture remembers each client's side and
    server, so the HTTP helpers take a bare path. `f.PathFor(side, suffix)` = `/api/v1/p/<slug>/<suffix>`;
    `f.Get(t, client, path)` returns a response whose body is already buffered;
    `f.Send(t, client, method, path, body)` is its write twin (JSON body, nil for none), so a test fires
    a PATCH/PUT/POST/DELETE at the other side's row and compares it with `AssertSameShape`.
  - Assertions (all take `testing.TB`): `f.AssertInvisible(t, client, path)` — 404, and the body names
    none of the **other** side's `UIDs()` (a uid the path itself carries is exempt — echoing the request
    leaks nothing); `f.AssertListExcludes(t, client, path, otherUIDs...)` — 200 and none of the given
    uids (at least one is required, an empty exclusion would pass every leak); `AssertSameShape(t,
    respForeign, respMissing)` — same status, Content-Type and byte-identical body, so another project's
    row is indistinguishable from a missing one (`MissingUID("ph")` mints the missing half);
    `f.AssertStoreScoped(t, fn)` — `fn(A's project uid)` and `fn(B's)` both non-empty and disjoint. The
    two that need the fixture's clients or project uids are methods, which is why they read `f.Assert…`.
  - Tests: the assertions' failure paths through a recording `testing.TB` against canned servers; the
    seed's shape and membership against the DB; `AssertStoreScoped` over every seeded root table; the
    real project guard (each member enters its own project as admin, the other is a 404 identical to an
    unknown slug). Worked examples in a package: `internal/photoapi/isolation_integration_test.go`
    (reads; its test env mounts the photo routes under `/p/{project}` too) and
    `write_isolation_integration_test.go` (writes and media bytes, plus the row left unchanged).

- **Tenancy guard (`internal/tenancyguard`):** the static half of tenant isolation, run by `make check`
  (a plain test, no build tag). `internal/projecttest` proves the scoped paths; this catches the statement
  nobody wrote a test for — a future query that forgets `project_id`. In the spirit of
  `web/src/styles/colourLiterals.test.ts`: a ban checked over the source, with a reasoned allow-list.
  - `RootTables` (`tables.go`) is **the one list** of catalogue root tables, each with the column that
    names its project (`project_id`; `project_invites` uses `project_uid`). `ReadScopedTables` lists the
    tables whose **reads** must name the project but whose writes need not — today `audit_log`: its
    `project_id` is nullable because an account or maintenance entry belongs to no project, so an
    `INSERT`/`UPDATE`/`DELETE` without one is legitimate, but a `SELECT` reading it (`FROM`/`JOIN`, never a
    `DELETE`'s own `FROM`) must carry the predicate or be allow-listed (the instance-wide `/audit` count is,
    the review leaderboard once leaked every project's reviewers through exactly this gap). `ExemptTables`
    lists the tables that carry a project column but are neither (`project_members`,
    `storage_rekey_runs`, `storage_rekey_derived_failures`), each with its reason. `ProjectColumnTables`
    replays `internal/database/migrations/*.sql` (`ALTER TABLE … ADD COLUMN project_id|project_uid`, the
    column inside `CREATE TABLE`, `DROP COLUMN`/`DROP TABLE`) and `TestRootTables_matchMigrations` requires
    the schema's project tables to be exactly `RootTables` ∪ `ReadScopedTables` ∪ `ExemptTables` — a new
    root table cannot be forgotten in the list.
  - `KeyedChildTables` lists the child tables of a root table that carry **no** project column — today 29:
    the per-photo tables (`photo_phashes`, `photo_files`, `face_detections`, …), `markers`, the membership
    tables, the feedback and review tables (`face_rejections`, `review_skips`, `duplicate_*`, …), per-user
    `user_ratings`/`user_favorites`, `comments` and `comment_mentions`, the task, notification and upload-link
    children, `tag_notices`, `import_failures`, `subject_family_children` — each with the keys that tie a row
    to a parent the caller resolves inside a project (`photo_uid`, `subject_uid`, `task_uid`, …; `markers` and
    `comments` also their own `uid`). **The list is the schema's:** `ForeignKeys` replays the migrations'
    foreign keys (a column's inline `REFERENCES`, a `[CONSTRAINT n] FOREIGN KEY (…) REFERENCES` table
    constraint, `ADD COLUMN`/`ADD`, `DROP COLUMN`, `RENAME [COLUMN]`, `DROP TABLE`), `ChildTables` derives
    every table without a project column that has a foreign key into a root table (or into another such
    child, so `comment_mentions` → `comments`), and `TestKeyedChildTables_matchMigrations` requires the map
    to list exactly those tables, each keyed by exactly those foreign-key columns (plus `uid`). A key into
    `users` or `projects` makes no child. A **read** of one (`FROM`/`JOIN`, never a `DELETE`'s own `FROM`)
    must be **keyed** by a top-level `AND` term `<key> = <value>` (`= ANY(…)` included), `<key> IN (<values
    or a subquery>)`, or `<key> = <a column of another item>` once that item is scoped — a root table by
    its project, a child by its own key — or is none of the guard's tables (a CTE, a derived table,
    `unnest`, an account table); `JOIN … USING (<key>)` is the same equality. Any other comparison keys
    nothing (`<>`, `LIKE`, `NOT IN`, a `NULL` test, `ph.photo_uid = ph.photo_uid`), and two children keyed
    only to each other stay unkeyed. A qualifier the statement never defines is the outer item of a
    correlated fragment built apart from its statement (`EXISTS (SELECT 1 FROM user_ratings ur WHERE
    ur.photo_uid = photos.uid …)` appended to a photo listing) and keys the child: that outer item is
    judged where the statement is assembled. What it catches is the **whole-table scan** — the upload's
    near-duplicate lookup (`SELECT … FROM photo_phashes ORDER BY dist LIMIT 1`) searched every project's
    pHashes and named the match to the uploader until 2026-10-14. Limits: a key value is trusted however
    the caller built it (`photo_uid = ANY($1)`), and writes are not judged.
  - `Scan(root, dirs...)` parses every non-test `.go` file under `internal/` and `cmd/` (`go/parser`, one
    goroutine per package; ~0.2 s on the Pi) and **folds** each string expression into a statement text:
    literals, `+` chains, package-level `const`/`var` values (a value folded in at a use site is judged
    there, not on its own), a function's locals accumulated across `:=`/`=`/`+=` (`q := "SELECT …";
    q += " WHERE …"` is one statement), `fmt.Sprintf` (each verb replaced by its argument, `%[n]s`
    included), `strings.Join` over a literal slice and a call of the package's own **SQL helper** — a
    function returning `string` whose body is one `return <string expression>`, such as
    `projectClause(ph)` or `inProjectSQL(alias, ph)`: it is read **in place**, each parameter standing for
    the argument the call passes, its own literals anchored at the call (and its body judged on its own
    only when nothing calls it). A piece it cannot read — a parameter, a call into another package, a
    slice built by `append` — folds to the stand-in `?`, **whatever it is called**: the round-3 review
    showed the old "name contains `project`" stand-in let `p.project_id IS NOT NULL`, `(… OR TRUE)` and
    `filter(projectUID)` pass, so it was removed (2026-10-15) and the statements it had covered were scoped
    through the helper reading or allow-listed (`buildWhere` callers, whose first clause is always
    `projectClause`). A piece appended to a local **under a branch or loop** the statement's start is not
    under (`q := …; if x { q += " AND p.project_id = $2" }`) is conditional: the statement is judged twice,
    whole and without its conditional pieces, since it may run either way. SQL comments (`-- …`, `/* … */`)
    are blanked first (same length, so offsets still locate the source), then the text is split on every
    `;` outside a quoted literal and each statement is judged by `unscopedRefs` (below). **One finding per
    statement**, anchored at its **use site**: the line of its
    first unscoped table name when the SQL is written in the expression itself (a raw string's own line,
    not the literal's first), or the line **naming the package-level constant** that holds it (the
    outermost name when one constant folds another). `Finding.Origin` keeps the table name's own
    `path:line`, and the failure message names it. So a constant shared by several statements is judged —
    and allow-listed — once per unscoped use: an entry never silently covers the next statement somebody
    builds from the same constant (the comments store's `commentProjectSQL` is listed at each of its seven
    uses, each with its own reason).
  - **The predicate rule** (`clause.go`, `sqlwalk.go`, `sqltoken.go`; `unscopedRefs`): a small pragmatic
    parser, not a SQL grammar. It tokenizes the statement (words, `$n`, `'…'`, operator runs — so `<=` is
    never an `=` —, parenthesised groups), splits each query on `UNION`/`INTERSECT`/`EXCEPT` into
    **levels** (a subquery, a CTE body and each set-operation arm is a level of its own, whose correlated
    aliases resolve outward), and records per level every item named after `FROM`, `JOIN` (comma joins
    included), a `DELETE`'s `USING`, `UPDATE` or `INSERT INTO`, with its alias, plus the condition
    segments: the `WHERE` and every `JOIN … ON`. Keywords count in **any case** when the statement is
    shaped like SQL (`sqlShaped`: it opens with `SELECT … FROM`, `WITH x AS`, `INSERT INTO`, `UPDATE x
    [alias] SET` or `DELETE FROM`); otherwise only upper-case ones do, so an error message's "update
    photo: %w" is prose. Each segment is split into **top-level `AND` terms** (a bracketed condition is
    split in turn, `BETWEEN`'s own `AND` is not a split, nothing inside `CASE … END` is). **`OR` binds
    looser than `AND`**: a segment (or bracketed condition) with a top-level `OR` has no term every row
    satisfies and yields none — `p.project_id = $1 AND p.uid = $2 OR p.uid = $3` scopes nothing. A term
    holding `NOT`, `CASE` or an `IS` test states nothing. An item is **scoped** by a term
    `<alias>.<project column> = <value>` (either side; the column bare binds to the items of its own level
    that carry it) where the value is built only of parameters, literals, casts (`$1::text`, `CAST($1 AS
    text)`), calls over such values, subqueries and `?` — **no column anywhere** (`COALESCE(NULLIF($1,
    ''), p.project_id)` is a tautology), never `NULL`, never `ANY/ALL/SOME` —, or by `<alias>.<project
    column> = <other alias>.<project column>` once the other item is scoped (equalities spread to a
    fixpoint; `projects.uid` counts as the project column of `projects`, an unknown table such as
    `project_members` or a CTE carries any project column name but is scoped only by its own predicate). An
    `INSERT`'s target is scoped by its project column in its **own** column list or `ON CONFLICT` target;
    an `INSERT … SELECT` (or a `VALUES` subquery) is judged on the tables it reads like any read.
    Nothing else scopes: a project column in a select list, `SET` list, `FILTER (WHERE …)`, `GROUP BY`/
    `ORDER BY`, `RETURNING`, a comment or `IS [NOT] NULL` test; a predicate on one alias (the "wrong
    alias"), a join to a scoped row by a foreign key (`s.uid = m.subject_uid` — say `AND s.project_id =
    p.project_id`) or a project filter inside a subquery says nothing about an outer table.
    `TestUnscopedRefs_unscoped` pins every bypass the earlier rules let through (`OR project_id = $2`,
    `project_id IS NOT NULL`, the wrong alias, `project_id` in a comment, an `INSERT … SELECT … FROM photos p
    WHERE p.uid = $2`; since 2026-10-15 the `AND … OR` precedence, the column tautologies, a key compared
    by `<>`/`LIKE`/`NOT IN`, lower-case SQL and the 20 child tables the list used to miss),
    `TestUnscopedRefs_scoped` the forms it accepts, and `TestScanFiles` the folding ones (a helper read in
    place, a project-named piece that scopes nothing, a conditional append, a `;` inside a literal).
  - `allow.txt` + `ParseAllowList`/`Judge`: `path:line reason` per line (`#` comments; a missing reason, a
    malformed key or a repeated one is `ErrAllowList`), keyed by the finding's use site.
    `TestRepository_rootStatementsNameTheirProject` fails on every finding without an entry and on every
    **stale allow-list entry** — one whose line no longer holds an unscoped statement — so the list cannot
    outlive what it excuses. The entries fall in a few kinds: worker handlers keyed by a photo uid from the
    job payload (`GetForJob` and the write-backs after it), instance-wide maintenance scan/repair, reset,
    inventory, the maintainer's dashboards and listings, storage migration and the rekey service, lookups by
    an instance-unique code hash (invites, upload links), and writes by a uid the same transaction already
    proved inside the project (`requireSubjectInProject`, a project-scoped `FOR UPDATE`, …). The 2026-10-14
    tightening deleted the unscoped helpers nothing in production called (with their entries) and scoped
    the two `INSERT … SELECT` writers it had trusted callers for (`insertFamilySQL`, `insertFaceSQL`).
  - Deliberately textual, so it has blind spots: a table name built at run time (`FROM %s` with a variable),
    an unreadable piece in a value position (`p.project_id = ?` is trusted to be a placeholder), a value that
    is a project but not *the* project (a subquery returning another project's id is a value like any
    other), a key of a keyed child table trusted however it was built, a correlated fragment's outer item,
    a `LEFT JOIN`'s `ON` term about its *left* table (it filters nothing), an exported SQL constant used
    from another package (a selector folds to `?`), a lower-case fragment that does not open a statement,
    and `if`/`else` branches that each add the predicate (judged without either, so allow-listed). It is a
    tripwire for the common mistake, not a proof; the isolation tests stay the proof.
  - `BootstrapReferences(root, patterns...)` (`bootstrap.go`) is the request-path half of "there is no
    default project": it parses the non-test `.go` files directly inside every directory matching a
    pattern and reports each `project.BootstrapUID` / `project.BootstrapSlug` (under the package's import
    name, aliases included) and each string literal spelling out the bootstrap uid.
    `TestRepository_noRequestPathNamesTheBootstrapProject` runs it over `internal/*api` and `cmd/kukatko`
    and fails on every reference not in its (empty) `allowedBootstrapReferences`; the migrations, the
    rekey runbook, the stores' tests and the test fixtures may still name `v`.

- **Privacy guard (`internal/privacyguard`):** the repository is public, so private data in it is a
  red `make check` (a plain test, no build tag), not a leak. Operator view — what is checked, how to
  allow-list, how to write a spec without private values — in `docs/OPERATIONS.md` ("Privacy guard").
  - `Files(ctx, root)` (`scan.go`) is `git ls-files` (or, when `KUKATKO_PRIVACY_FILES` names one, a
    file list — the build box's synced copy has no `.git`) plus a walk of `docs/specs/`, tracked or
    not: a task's spec sits there uncommitted while the task runs. It stats git's index so a cached
    pass of `go test` is invalidated by a `git add`; the walk's directory reads invalidate it on a
    new spec.
  - `Check(root, files, rules)` reads each file on one worker per CPU (missing, symlinked and binary
    files skipped) and `CheckText` reports one `Finding` per (line, rule). A `Rule` has a `Hint`
    literal (a file or line without it is never handed to the regexp — the generic patterns have no
    literal prefix, and Go's regexp is slow on those) and a `Benign(line, start, end)` that judges a
    candidate in its context (a documentation IP, a version token, a reserved domain, a longer run
    of digit groups). A rule first runs over the whole text and only a rule that matched somewhere
    runs line by line: the repository (~3.7k files, 158 rules) scans in ~2 s on the Pi.
  - `GenericRules()` (`rules.go`) are the committed rules, each with a one-line `Reason`; their
    patterns are spelled (`-{5}`, `op:/{2}`) so their own source passes them, and
    `TestGenericRules_sourceIsClean` holds the file and its test to that. `LoadPrivateRules(path)`
    (`private.go`) reads `.secrets/privacy-patterns.txt` into rules named `private pattern #<line>`;
    a missing file is `found == false` (the test logs it and runs the generic rules alone), an
    invalid line is `ErrPrivatePattern` naming the line only — regexp's own message would quote it.
  - `Finding.String()` is `path:line: <rule>` — never the text. `ParseAllowList`/`Judge`
    (`allow.go`) mirror the tenancy guard's: `path:line reason`, one entry excusing every rule on
    its line, an unused entry stale. A line only a private pattern flags cannot be allow-listed: the
    entry would be stale wherever the pattern file is absent.
  - `TestRepository_noPrivateData` (`repo_test.go`) is the guard; the unit tests build their hits
    from pieces (`"ssh someone" + "@somehost"`) and run `Files` over a throwaway `git init` tree for
    the untracked-spec case.

- **Upgrade inventory (`internal/inventory`):** the before/after check of an upgrade that migrates data,
  behind `kukatko maintenance inventory` / `inventory-diff` (runbook in `docs/OPERATIONS.md`, first use in
  `docs/MIGRATION_SAFETY.md`). `Collect(ctx, pool, dir, store)` reads inside **one `READ ONLY REPEATABLE READ`
  transaction** with the session in UTC and writes sorted, escaped TSV files: `counts.tsv` (every table of
  `current_schema()`), `columns.tsv` (table, column, `udt_name`), `digests.tsv`, `photos.tsv`
  (`uid|file_hash|file_path|file_size|archived_at` — the catalogue's soft delete is `archived_at`; there is no
  `deleted_at`) and `users.tsv` (`uid|username|role|disabled`). It **never migrates**, so the new binary can
  inventory the old schema before a deploy (the old release has no such command).
  The digests cover an **explicit column allow-list per table** (`spec.go`: the columns that exist at 0099 and
  after the tenancy migrations; `project_id`, `last_project_uid`, the generated `photos.fts` and `users.role`
  are left out — the role is judged row by row from `users.tsv`). A row is projected to its allow-listed
  columns (`jsonb`/`halfvec`/`vector`/`bytea` as `md5(col::text)`), rendered with `row_to_json`, hashed; the
  row hashes are concatenated in key order (text keys `COLLATE "C"`) and hashed once more — equivalent to
  hashing the concatenated rows, in bounded memory. An allow-listed column the schema lacks is named in the
  row's fourth field instead of failing; an absent table digests as `absent`. With a `SizedKeyLister` it
  also lists the store **once** and writes `storage-missing.tsv`: every `photos.file_path` absent or of a
  size other than `file_size` (empty = good).
  `Load`/`Diff`/`Judge` are pure: `Diff` emits keyed `Difference`s (`table-added:`, `table-removed:`, `count:`,
  `column-added:`/`-removed:`/`-type:` (`table.column`; a new table's columns are its table's difference),
  `digest:`, `photo[-added|-removed]:`, `user[-added|-removed]:`, `user-role:` when **only** the role changed,
  `storage-missing:` for originals newly missing in B); `ParseExpectations` takes `path.Match` globs over those
  keys plus the named rule **`role-collapse`** (viewer/curator/editor/admin → `user` and nothing else);
  `Result.Err` is `ErrUnexpected` when anything is left unexplained.
- **Remote CLI client (`internal/ctl`):** the client half of `kukatko ctl` — the one piece of the tree that
  Kukátko calls **over HTTP as a foreign server**, not through the DB and the disk. It has nothing in common with `internal/config`
  (which describes the *server* and knows nothing about a remote endpoint); the only state it owns is the client
  file `~/.config/kukatko/ctl.yaml`. The motivation: cheaper in tokens than the MCP server — no tool schema
  is loaded into the model's context, just a short command and a narrow result. Hence the compact output.
  - `config.go` — `Context{Name,Server,Token,Project}` + `Config{CurrentContext,Contexts}` in the kubectl style.
    `Load(path)` (a missing file = an empty config, not an error — running from env variables alone), `Save(path,cfg)`
    (atomically: a temp at 0600 → `Rename` → `Chmod` 0600, the directory 0700; it **tightens an existing
    world-readable file**, it never writes a token into one as it is). `DefaultConfigPath()` honours `XDG_CONFIG_HOME`.
    `Resolve(cfg, Selection{Context,Project}, env)` → `Endpoint{ContextName,Server,Token,Project}`: it picks
    the context (by name → otherwise `current-context`), then `KUKATKO_SERVER`/`KUKATKO_TOKEN` **override field
    by field**, so `KUKATKO_TOKEN` on its own re-credentials the stored context; the project is the `--project`
    flag (`Selection.Project`), else `KUKATKO_PROJECT` (`EnvProject`), else the context's (`cmp.Or`). A missing
    project is not `Resolve`'s error — only the project-scoped commands need one. The errors
    `ErrContextNotFound`, `ErrNoServer`, `ErrNoCurrentContext` (a command that edits the current context
    while none is) and **`ErrNoProject`**, whose text is the exact message a project-scoped command fails with:
    `no project selected: run 'kukatko ctl projects use <slug>' or pass --project`.
  - `client.go` — `NewClient(server, token)` (validates an absolute http(s) URL → `ErrInvalidServerURL`);
    `WithProject(slug)` returns a copy bound to a project, whose **every** path goes under
    `/api/v1/p/<slug>` (escaped into its own segment) — `apiPath`, applied once in `newStreamRequest`, the
    choke point of every request — while a client without one talks to the bare `/api/v1` (the instance-wide
    routes: `/projects`, `/users`, `/auth/me`). Which client a command gets is decided in `cmd/kukatko/ctl.go`
    (below);
    the internal `get(ctx, path, query)` and `send(ctx, method, path, body)` send
    `Authorization: Bearer <token>` and return the **raw** body (`json.RawMessage`), because `-o json`
    prints the server's bytes unchanged; `204 No Content` returns a `nil` body. Success is the whole `2xx` range —
    the API answers 200, 201 and 204 depending on the endpoint. `401` → `*UnauthorizedError` with a short, actionable message
    (the token is missing / expired / was revoked + how to make a new one); `403` → `*ForbiddenError`, which
    **says that the role is not enough** (mutations want editor/admin, a viewer only reads), instead of printing the server's
    `insufficient permissions`. It **never** prints the body or the token; another non-2xx → `*StatusError`
    with the server's `{"error":…}` text (otherwise a limited excerpt of the body). The body is read through `io.LimitReader`,
    timeout 30 s.
  - `ledger.go` — `TaskLedger(ctx, taskUID)`: the review ledger behind `ctl tasks ledger` — pages
    `GET /photos?task=` through the whole frozen group (`ledgerPageSize` 500, never past
    `ledgerMaxPhotos` 1000, the server's own cap) and keeps the rows as the server's bytes so `-o json`/
    `-o llm` reassemble them verbatim; the table renderer prints `UID`, `TAKEN` at its precision
    (`1974-06-14`/`1974-06`/`1974`/`1970s`), `SOURCE`, the comment's first line cut to `commentWidth` and
    `BY`.
  - `photos.go` — `ListPhotos`/`GetPhoto(uid, PhotoDetailOptions)`/`SearchPhotos` +
    `DecodePhotoPage`/`DecodePhotoDetail`. `PhotoDetail` carries every field the renderer prints, which since
    `photos edit` exists means every field that command can write — an agent that edits a photo has to be able
    to read back what it changed — plus `ocr_text` and `people` (`[]PhotoPerson`, `nil` when the caller did not
    ask, which is **not** the same as nobody being on the photo). `PhotoDetailOptions{People}` renders the
    server's opt-in `?people=true` and nothing at all when it is false, so a plain read never makes the server
    do the match. `ctl photos get` asks for it by default — reading the photo whole is the point of that
    command — while `photos edit` does not, unless asked.
    **The decoder is per-resource on purpose:** the API has no uniform list envelope (`photos` returns
    `{photos,total,limit,offset,next_offset}`, the other resources a bare list) and we must not unify it —
    it would break the frontend. `ListOptions` (limit/offset/sort/order/year/album/label/favorite/archived)
    is validated locally (`ErrInvalidPaging`/`ErrInvalidYear`/`ErrInvalidArchived`), so a typo does not
    cost a round trip. **The API does not know `--year`** — it is translated into the inclusive range
    `taken_after`/`taken_before` (`taken_at >= … <= …`), the upper bound being the last instant of 31 December.
    `SearchOptions` adds `q` + `mode` (`fulltext`/`semantic`/`hybrid`).
  - `albums.go` — `ListAlbums`/`GetAlbum`/`CreateAlbum`/`UpdateAlbum`/`DeleteAlbum`/`AddAlbumPhotos`/
    `RemoveAlbumPhotos` + `DecodeAlbums`/`DecodeAlbum`/`DecodePhotoUIDs`/`FetchAlbum`/`DescribeAlbum`.
    The envelope is a **bare `{"albums":[…]}` without paging**
    — hence its own decoder. `PhotoCount` is filled only by the list; the detail does not send it, so the renderer does not print it.
    `AlbumInput` is validated locally (`ErrEmptyTitle`, `ErrInvalidAlbumType`); membership sends the whole
    list of uids in **one** request and the server returns the refreshed order.
    **`UpdateAlbum` reads before it writes**, for the same reason `RenameSubject` does: `PATCH /albums/{uid}`
    rewrites the whole editable record, so a body carrying only a new title would empty the description and
    clear the cover. `AlbumPatch` is all pointers (a `nil` field means "leave it"), `apply` folds it onto the
    stored album and `albumUpdateBody` states every field including the nulls — `Cover` pointing at `""` is the
    explicit "no cover". The structural type is deliberately not in the body: it is not user-editable and the
    server carries the existing one across. `ErrNoAlbumEdits` refuses an update that would change nothing but
    still cost an audit entry.
  - `labels.go` — `ListLabels`/`GetLabel`/`CreateLabel`/`UpdateLabel`/`DeleteLabel`/`AttachLabel`/`DetachLabel`
    + `DecodeLabels`/`DecodeLabel`/`FetchLabel`/`DescribeLabel`. The envelope is a **bare `{"labels":[…]}`**
    ordered by priority (a third shape). Attach/detach
    answer `204`. An empty `source` is dropped from the body so that the server fills in its own `manual`
    (`ErrInvalidLabelSource`, `ErrEmptyName`). **`UpdateLabel` reads before it writes** too: `PATCH
    /labels/{uid}` is a whole-record write, so a body carrying only a new name would reset the priority to
    zero. `LabelPatch` is all pointers and `labelUpdateBody` sends name, priority and `review_enabled`
    unconditionally — priority `0` is a value here, not an absence (`ErrNoLabelEdits`).
  - `subjects.go` — the whole subject surface, read and write. Reads: `ListSubjects`/`GetSubject`/
    `SubjectPhotos` + `DecodeSubjects`/`DecodeSubject`/`FetchSubject`. The envelope is a **bare
    `{"subjects":[…]}`**; a subject's gallery, however, **has the `/photos` shape**, so it is read by
    `DecodePhotoPage` (the same shape, not a unified one). `PageOptions` offers only limit/offset — the endpoint
    does not read the catalogue filters, so ctl does not offer them either.
    Writes: `CreateSubject`/`UpdateSubject`/`RenameSubject`/`DeleteSubject`/`MergeSubjects` +
    `DecodeMergeResult`. `SubjectInput` is validated locally (`ErrEmptySubjectName`, `ErrInvalidSubjectType`).
    **`RenameSubject` reads before it writes** — `PATCH /subjects/{uid}` rewrites the whole editable record, so a
    body carrying the new name alone would reclassify a pet as a person and erase the notes, the cover and the
    life years with it; `Subject.input()` is that round trip. `MergeSubjects` posts the keeper to the **source's**
    path (the subject in the path is the one deleted) and refuses a merge into self locally
    (`ErrMergeIntoSelf`). `MergeReport` pairs the server's counts with both people's names, because the merge
    deletes the source and its name then exists nowhere else (see `recognition.go`). `SubjectRef`
    (+ `SubjectRefFromArgs`, `ErrSubjectRequired`/`ErrSubjectAmbiguous`) is the "by uid **or** by name"
    reference the face and cluster assignments share — a name is resolved, or created, server-side.
    `DescribeSubject` renders one as `Name (uid)` for the commands whose endpoint answers `204` and gives
    nothing back to name.
  - `faces.go` — the per-photo recognition surface: `ListFaces` (`GET /photos/{uid}/faces`) + `DecodeFaceList`,
    `AssignFace` (`POST /photos/{uid}/faces/assign`) + `DecodeFaceAssign`, and the four persisted opinions
    `RejectFace`/`UnrejectFace`/`ConfirmFace`/`UnconfirmFace` (`/feedback/face-{rejections,confirmations}`,
    `204`, idempotent, **the DELETE half carries a body too**). `FaceList.Find(index)` resolves the per-photo
    face index the tables print — a missing one reports the indexes the photo *does* have rather than becoming a
    server error — and `FaceView.AssignTo(SubjectRef)`/`FaceView.Detach()` build the request body. That routing
    is the whole of what ctl decides: a detection that already carries a marker gets `assign_person`, a bare one
    `create_marker` with its own box, a named one `unassign_person` — the same rule the web face editor
    applies. **The assignment state machine, the find-or-create by name and every threshold stay on the
    server.** Errors: `ErrUnknownFace`, `ErrFaceNotMarked`, `ErrFaceNotNamed`, `ErrNegativeFaceIndex`.
  - `family.go` — the genealogy over subjects, read and write: `GetRelations`/`FetchRelations` +
    `DecodeRelations`, `AddRelation` + `DecodeRelationResult`, `RemoveRelation` (a `204`), `UpdateFamily` +
    `DecodeFamily`, and the renderers `WriteRelations`/`WriteRelationReport`/`WriteFamily`. `Relations` is the
    four derived lists; `Relations.Role(uid)`/`Find(uid)` say how somebody is related **before** a removal is
    sent, which is what lets an unrelated pair fail locally instead of as a `404` and lets a sibling be told
    that there is nothing to remove (`RoleSibling` is reported, never requested — siblings are derived from a
    shared parent). `Partnership.LoneParent` marks the family that has nobody on the other side: `WriteRelations`
    prints it under `RoleLoneParent` (reported, never requested, like `RoleSibling`) and leaves it out of the
    summary's partner count — the row is kept because its family uid is what `family edit` takes, but a lone
    parent is a family, not a partner. `RelationInput` is validated locally (`ErrInvalidRole`, `ErrInvalidChildKind`,
    `ErrSubjectRequired`/`ErrSubjectAmbiguous` for naming both an existing and a new person or neither, and
    `SubjectInput.validate` for the person being created); `FamilyUpdate` mirrors the SQL CHECKs of migration
    `0073` (`ErrInvalidFamilyKind`, `ErrInvalidFamilyYear`) and is a **whole-record** write, so `ErrNoFamilyEdits`
    refuses an edit that names nothing — it would erase rather than do nothing. **`FindSubjectByName` resolves a
    name client-side**, unlike the face and cluster assignments: the inline half of `POST /relations` *always*
    creates, so a name that already exists would quietly split one person into two. It matches the stored name
    or slug case-insensitively, reports "nobody" so the caller can create instead, and refuses an ambiguous name
    naming every candidate. `RelationReport` is the third result ctl **synthesizes** (with `MergeReport` and the
    `204` `Ack`): the answer carries neither the role that was asked for nor the name of the person the relation
    was recorded on, and "created: true" without saying who became whose parent is unreadable.
  - `clusters.go` — the auto-clustered groups of unassigned faces: `ListClusters` + `DecodeClusters`,
    `AssignCluster(uid, SubjectRef)` + `DecodeClusterAssign` (one action names every face in the group and
    consumes it), `RemoveClusterFace(cluster, photo, index)` + `DecodeClusterRemoval`, whose `nil` cluster means
    the removal emptied the group and the server deleted it — which is why the decoder returns a pointer.
    `Cluster.Suggestion` is a pointer for the same reason: "nobody was close enough" is not a suggestion with a
    zero distance.
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
  - `stacks.go` — the manual grouping of the several files one shot was stored as: `StackPhotos`
    (`POST /photos/stack`), `SetStackPrimary`, `UnstackPhoto`, `UnstackAll` (the three per-photo verbs) +
    `DecodeStack`/`WriteStack`. All four answer with a **photo detail**, so `Stack` decodes only the part that
    is the result — the photo, its `stack_uid` and the whole `stack_members` strip — while `-o json` still
    passes the full response through. A selection is normalised and de-duplicated before it is counted, so a
    uid given twice is one photo and is refused as `ErrStackTooSmall` rather than becoming a puzzling `400`.
    An unstacked photo renders as one line, not an empty table: after `ungroup` that *is* the result. Nothing
    here merges — every member keeps its uid, its file and its metadata.
  - `imageedit.go` — the **non-destructive image edit** (`GET`/`PUT /photos/{uid}/edit`), which is not
    `edit.go`: that one writes the photo's metadata, this one how the library renders it. `GetImageEdit`/
    `FetchImageEdit`, `SetImageEdit`, `ResetImageEdit` + `DecodeImageEdit`/`WriteImageEdit`. **`SetImageEdit`
    reads before it writes**: `PUT` replaces the whole edit, so a body carrying only a rotation would silently
    drop an existing crop. `ImageEditPatch` is pointers plus a separate `ClearCrop`, because a `nil` crop
    already means "do not touch it"; `imageEditBody` always states all seven fields, the crop as four explicit
    nulls when there is none. `ResetImageEdit` writes the neutral edit **outright and reads nothing first** —
    a reset is a write, and only a write makes the thumbnail cache re-render. `Crop`/`ParseCrop` read and print
    `x,y,w,h` as fractions of the image; the rotation allow-list and the `[-1,1]` adjustment range mirror
    `internal/photoapi` so a typo costs neither a round trip nor an audit entry (`ErrInvalidRotation`,
    `ErrInvalidAdjustment`, `ErrInvalidCrop`, `ErrNoImageEdits`). `WriteImageEdit` says outright when an edit
    is neutral, since a table of zeroes cannot otherwise be told from "nobody has edited this".
  - `uploadlinks.go` — upload links for `ctl upload-links`: `ListUploadLinks`/`RestoreUploadLinkCode`/
    `NewUploadLinkCode` + `DecodeUploadLinks` (`{"links":[…]}`)/`DecodeUploadLink` (`{"link":…}`) and
    `WriteUploadLinks`/`WriteUploadLink`, which take the context's server so the `URL` column is the absolute
    `/u/<code>` address (`UploadLinkURL`; `unknown (restore-code)` for a link whose code is not stored).
    `ParseUploadLinkCode` reads the bare code out of a pasted code or whole link; a blank one is
    `ErrEmptyLinkCode` without a round trip. `cmd/kukatko/ctl_uploadlinks.go` puts `new-code` behind the
    irreversible `--yes`/`--dry-run` gate, naming the link from the listing.
  - `projects.go` — the system administrator's `ctl projects` / `ctl users`: `ListProjects`/`GetProject`/
    `CreateProject`/`UpdateProject` (`ProjectPatch.body` sends only the set keys, a cleared limit as `null`;
    empty = `ErrEmptyProjectPatch`, set + clear = `ErrConflictingLimit`, both before any request)/
    `AddProjectMember`/`ListAccounts`/`SetInstanceRole`, `ResolveProject` (slug or uid) and
    `ResolveAccount` (username or uid) over the lists (`ErrProjectNotFound`/`ErrAccountNotFound`), the
    bare-array decoders and `WriteProjects`/`WriteProject`/`WriteAccounts`/`WriteAccount`.
    `cmd/kukatko/ctl_projects.go` builds both trees; `changedInt64` keeps "flag not given" apart from 0.
    `errorMessage` (client.go) appends a refusal's `message` to its code (`invalid_slug: …`).
  - `members.go` — a project admin's `ctl members`: `ListMembers`/`AddMember`/`SetMemberRole`/
    `ApproveMember`/`RemoveMember` on `/members` of the client's project (a project-less client =
    `ErrNoProject`, blank identifier = `ErrNoIdentifier`, both before any request), `ResolveMember` (username
    or uid over the project's list, `ErrMemberNotFound`), `DecodeMembers`/`DecodeMember`,
    `WriteMembers`/`WriteMember`. `add` confirms neutrally, never claiming the account exists. `People`
    (`participants.go`) reads the name directory at `/users` of the client's project and refuses a
    project-less client with `ErrNoProject`, since the unprefixed path is the maintainer's account list.
  - **The project, in `cmd/kukatko/ctl.go`.** `--project` is a **persistent** flag of `ctl` (beside `-o`,
    `--fields`, `--context`); no subcommand declares a local one, which would shadow it. Top-level trees
    whose routes are instance-wide are wrapped in `markInstance` (an annotation: `context`, `projects`,
    `users`); the root's `PersistentPreRunE` records `isInstanceCommand(cmd)` on the options, and
    `ctlOptions.resolve` hands an instance command a project-less client and every other command a client
    `WithProject(endpoint.Project)` — or fails with `ctl.ErrNoProject`, unwrapped, before any request.
    `ctl_context.go` is the context tree (`context set|list|use|current`, alias `config` with the old
    `set-context`/`use-context` names); `context set` reads the persistent `--project`
    (`Flags().Changed`) and validates it with `project.ValidateSlug`. `ctl projects use <slug>` stores the
    slug on the current context without a request; `ctl projects list` reads `Me` (`GET /auth/me`,
    `projects.go`): a maintainer (`Me.IsMaintainer`) gets `GET /projects`, anybody else their memberships
    (`DecodeMemberships`/`WriteMemberships`, `-o json` = the `projects` array as sent). `WriteEndpoint`
    (`output.go`) renders `context current`; `WriteContexts` has a `PROJECT` column. `ctl photos edit
    --dry-run` names `PATCH /api/v1/p/<slug>/photos/<uid>` (`<project>` when none is selected).
  - `savedsearch.go` — the per-user "smart albums": `ListSavedSearches`/`GetSavedSearch`/`CreateSavedSearch`/
    `UpdateSavedSearch`/`DeleteSavedSearch` + `DecodeSavedSearches`/`DecodeSavedSearch` and
    `WriteSavedSearches`/`WriteSavedSearch`. A fifth envelope, `{"saved_searches":[…]}`. **This is the one
    update that needs no read first**: `PATCH /saved-searches/{uid}` genuinely merges, so an omitted field is
    left alone server-side. The stored `params` is the flat string map the app serialises into its URL, which
    is why `ParseSearchParams` refuses anything but a JSON object of strings — a number stored here is a saved
    search the web UI cannot open — and `ParseSearchParam` reads one repeated `key=value` (the value may hold
    `=`). A search that is missing **or somebody else's** answers `404`, never `403`, so `notYours` rewrites
    exactly that status into `*NotYoursError`, which names both readings; every other status passes through as
    the server's own `*StatusError`.
  - `similar.go` — `ListSimilar` (`GET /photos/{uid}/similar`) + `DecodeSimilar`/`WriteSimilar`. Every row
    carries its cosine distance, because without it a neighbour list is just a list. A limit the server would
    silently clamp is refused instead (`ErrInvalidSimilarLimit`), so a caller asking for 500 learns it cannot
    have them; a limit of `0` sends no parameter at all. An empty answer is not an error and not a claim: a
    photo the box has not embedded yet, and an instance with no embeddings backend, both answer `200` with an
    empty list, which the renderer says out loud.
  - `duplicates.go` — the read-only scan and the two opinions: `ListDuplicates` (`GET /duplicates`, paged) +
    `DecodeDuplicates`/`WriteDuplicates`, and `ConfirmDuplicate`/`UnconfirmDuplicate`/`DismissDuplicate`/
    `UndismissDuplicate` over `/feedback/duplicate-{confirmations,dismissals}` (`204`, idempotent, the DELETE
    half carrying a body). **Nothing in this file changes a photo** — merging lives in `dupmerge.go`, behind
    the gate. The pair is unordered server-side; a pair naming one photo twice is
    refused locally (`ErrSameDuplicatePhoto`). The table prints **one row per member**, since the member uids
    are what confirm and dismiss take, and each distance names its detector — a Hamming distance between
    perceptual hashes and a cosine distance between embeddings are not the same number.
  - `dupmerge.go` — resolving a group: `MergeGroup(keeperUID, others, dryRun)` (`POST /duplicates/merge`) +
    `DecodeDuplicateMerge`/`WriteDuplicateMerge`. **The keeper is folded into its own group** by
    `mergeInput`, so the API's `ErrKeeperNotInGroup` is unreachable from ctl; the set is deduplicated and a
    group of one is refused locally (`ErrNoMergeMembers`). A dry run is **the server's own `dry_run`
    preview** — the same computation, writing nothing — rather than a second guess at what a merge would do,
    which is the whole reason a rehearsal is trustworthy. The renderer says out loud that the archived copies
    are **in the trash, not deleted**: a merge archives, and only a purge destroys.
  - `upload.go` — `UploadPhotos(ctx, paths)` (`POST /upload`) + `DecodeUploadReport`/`WriteUploadReport`.
    The multipart body is written into an `io.Pipe` as the files are read, so **a whole original is never
    held in memory**, and like `media.go` it uses `Client.stream` (no timeout) and `Client.newStreamRequest`,
    the streamed half of `newRequest`. Every path is `os.Stat`ed **before the first byte is sent**
    (`ErrNoUploadFiles`, `ErrNotRegularFile` — a directory is refused, not walked: that is
    `kukatko import dir`), so a typo costs no half-sent batch. `UploadReport.Counts()` treats an outcome that
    is neither `created` nor `duplicate` as a failure, so an outcome added server-side later is reported as a
    problem rather than silently ignored; a **duplicate is not an error**, which is the point of the hashing.
  - `state.go` — the reversible half of a photo's life: `SetPhotoState(uid, state)` over
    `/photos/{uid}/{archive,unarchive,hide,unhide}` + `DecodePhotoState`/`WritePhotoState`, and
    `DescribePurgeTarget`. `PhotoState` decodes only the two lifecycle fields out of the whole refreshed
    photo the endpoints answer with, because everything else in that body is what the photo already was —
    and the line prints **both** flags, since archiving a hidden photo leaves it in two states and reporting
    only the change would not say so. `DescribePurgeTarget` is what a purge rehearsal reads out: the size,
    and whether the photo is even archived — a rehearsal that did not say so would let somebody confirm a
    command the server is going to refuse with `409`.
  - `rebuild.go` — redoing one photo's derived data: `RebuildPhoto(uid, name)` over the four endpoints
    `RebuildSpecs` lists (`regenerate-thumbnail`, `reembed`, `redetect-faces`, `regeocode`) +
    `RebuildSpecFor`/`DecodePhotoRebuild`/`WritePhotoRebuild`, with `ErrUnknownRebuild` for a name that is not
    one of them (refused client-side, before a request is spent). The CLI names are what is being rebuilt
    (`thumbnail`/`embedding`/`faces`/`place`), not the endpoints' verbs, because the paths predate the idea of
    a set. `PhotoRebuild` is the **union** of what the four answer — the thumbnail endpoint is the oldest and
    says `{status,sizes}` while the newer three say `{step,status,faces?}` — and `DecodePhotoRebuild` stamps
    the requested name onto a response that does not name its own step. The line reports what the
    recomputation **produced** (the face count, the regenerated sizes) rather than a bare acknowledgement:
    a rebuild is run because the previous answer was wrong, so the new one is the point. `status:"queued"`
    is printed as the wait it is, not as a failure — the box being asleep is not the operator's problem.
  - `video.go` — the video surface: `PrepareVideo(uid)` (`POST /photos/{uid}/process/hls_transcode`),
    `RebuildStoryboard(uid)` (`POST /photos/{uid}/regenerate-storyboard`), `VideoRenditions(uid)`
    (`GET /photos/{uid}/renditions`) and `BackfillHLS(all)` (`POST /process/hls`), with
    `DecodePhotoStep`/`WritePhotoStep`, `DecodeVideoRenditions`/`WriteVideoRenditions` and
    `DecodeBackfill`/`WriteBackfill`. The two per-photo calls are rebuilds and are named as such
    (`RebuildVideo` = `video`, `RebuildStoryboard` = `storyboard`) but they are **not** in `RebuildSpecs`,
    because neither is run in the request: an encode and a sprite are each a full pass over the clip, so both
    are queued and both answer with a step and its state rather than with a result. That is why the reply
    shape here is `PhotoStep` — the processing report's own `{step,state,at?,error?}` — and not
    `PhotoRebuild`: `queued` and `running` are honest answers. `done` is the one that has to be spelled out
    rather than printed: the report resolves persisted evidence **before** the queue, so an already-encoded
    clip answers `done` with the *old* stamp while the encode this call just scheduled sits behind it, and
    the state word alone would read as "nothing happened". `WritePhotoStep` prints what happens next rather than the
    state word alone, so `skipped` reads as "this will never run here" instead of as a silent success.
    `VideoRendition` is the row of `photo_hls_renditions` **without** its playlist text, which is by far the
    largest column and is already served, rewritten, by the media-playlist route; the table renders the
    bitrate in Mbit/s and the length as `m:ss`, and its footer names the **oldest** encode, which is what
    answers "is what is stored still current?". An unencoded clip prints a sentence, not a headed empty
    table — never having been encoded is a normal state of a video. `BackfillHLS(all=true)` is the full
    re-encode of the library and is gated by the CLI's `--yes`: nothing is destroyed, so it is not one of the
    irreversible commands, but a mistyped flag would occupy the worker for a day.
  - `trash.go` — the trash and the two batch purges: `FetchTrash` (`GET /trash/info` for the retention
    window + paged `GET /photos?archived=only` for the photos), `PurgePhoto`, `EmptyTrash`,
    `PurgeOlderThan(days)` — each carrying the API's own `confirm=true`, which is **not** the CLI's `--yes`
    but what the endpoint demands of any client — plus `PurgeCutoff`, `Trash.ArchivedBefore` and
    `DecodePurgeResult`/`WritePurgeResult`/`WriteTrash`. The listing is sorted **oldest-archived first**
    client-side: the API has no `archived_at` sort key, and a "what goes next" answer computed over one
    arbitrary page would be wrong rather than merely partial; paging stops at `maxTrashItems` and the summary
    says when it did. `PurgeAt` is left unset when retention is off — then nothing goes away on its own and a
    countdown would be a promise the instance does not make. `TrashView` pairs the listing with the heading
    it is printed under and a `DryRun` flag, because the same rows mean something very different under
    "what is in the trash" and "what this would destroy"; like `MergeReport` it is a value ctl composes out
    of two endpoints, so the machine formats emit it rather than passing server bytes through.
  - `comments.go` — `ListComments`/`AddComment` + `DecodeComments`/`DecodeComment` and `WriteComments`/
    `WriteComment`. **The author is always the token's owner** — the API takes it from the authenticated
    principal and the audit trail records it there too — which is why the MCP server exposes no comment tool
    at all and why `ctl`'s help says to write only under your own account. Every role may write, viewers
    included: a comment is participation, not curation. The thread renders as **prose, not a table**: one
    header line per comment and the body indented whole underneath, because a comment is often the only record
    of who, where and when, and a column width would cut off the answer somebody came for. `MaxCommentLen`
    mirrors `comments.MaxBodyLen` rather than importing it, so `ctl` links none of the server's domain
    packages; the server enforces it either way (`ErrEmptyComment`, `ErrCommentTooLong`).
  - `uids.go` — `ParsePhotoUIDs(r)` reads a set of photos from stdin in **four** shapes: the envelope
    `{"photos":[…]}` (exactly what `ctl photos list -o json` prints), a bare JSON array of uids, a bare array of
    objects with a `uid`, or a plain whitespace-separated list. `NormalizeUIDs` trims, drops the empty ones
    and **deduplicates** (so that the count in the confirmation prompt matches what is actually sent) →
    `ErrNoPhotoUIDs`. `ConfirmThreshold = 50` is the boundary above which the command asks.
  - `edit.go` — `PhotoEdit`, the body of `PATCH /photos/{uid}`, built one field at a time with
    `Set`/`SetTime`/`Clear`, plus `EditPhoto(ctx, uid, edit, opts)` and `ParseTakenAt`. It is a **key/value map,
    not a struct of pointers**, because the API reads *presence*, not nullness: an untouched field must not
    appear in the request at all (re-sending an unchanged `taken_at` would flip its source from `exif` to
    `manual`) while a cleared one must appear as an explicit `null`. `Validate()` refuses only an edit that
    carries nothing — the length caps, the dropping of `taken_at_note` and which `location_source` a client may
    claim stay on the server, and a second copy here would be one that drifts. `Body()` renders the indented
    JSON `--dry-run` prints. Errors: `ErrNoEdits`, `ErrConflictingEdits`, `ErrIncompleteLocation`,
    `ErrInvalidTimestamp`.
  - `media.go` — `SaveRendition(ctx, uid, size, path)` behind `ctl photos image`: a thumbnail size or
    `RenditionOriginal`, validated locally against the `thumb` registry (`ValidRendition`, `RenditionSizes`,
    `DefaultRenditionSize = fit_720`). It is the one call that does **not** go through `Client.do`: the bytes are
    copied straight from the socket into the file, so a hundred-megabyte original is never held in memory, and
    it uses `Client.stream` — a second `http.Client` with **no timeout**, because a big download is slow on
    purpose and only the caller's context should end it. The file lands on a temporary name beside its
    destination and is renamed into place only when the copy succeeds, so a failed transfer leaves nothing that
    looks finished. With no destination the name comes from the response (`Content-Disposition` for an
    original, `<uid>_<size><ext>` otherwise), always reduced to a bare file name so a header cannot steer the
    write out of the working directory.
  - `llm.go` — `WriteLLM(w, raw, fields)` and `ParseFields`, the `-o llm` renderer. It slims the server's body
    generically — the keys in `droppedKeys` (the raw `exif`, the signed media URLs, the file-internal and
    machine-derived columns, the row bookkeeping) and every empty or zero-valued field — and re-encodes it
    compactly, keeping numbers as `json.Number` so a coordinate is not rounded through a float. It is a rule
    about **keys**, not a projection per resource, which is what makes one implementation cover every command
    (see `renderRaw` in `cmd/kukatko/ctl_render.go`, which handles the format before the per-resource decoder
    runs) and everything added later. `pickFields` applies the `--fields` allowlist: a named key keeps its whole
    value, an unnamed one survives only as the road to a named descendant, so the allowlist reaches through a
    list envelope without naming it; an allowlist nothing matches yields `{}` rather than the whole body.
  - `output.go` — `ParseFormat` (`table`/`json`/`llm`; **deliberately no `yaml`**), `Output{Format,Fields}` and
    `NewOutput` (one value rather than two parameters, so a renderer added later cannot quietly drop the
    allowlist), `WriteJSON` (echoing the bytes
    unchanged), the shared `writeTable`/`writeKeyValues`/`writeLine`, `WritePhotoPage` (a table + one summary
    line: how many out of how many, `offset`, `next offset`, and for a search the effective `mode` and any `degraded`),
    `WritePhotoDetail` (the date and the location beside their provenance, so an inferred value never reads
    like a measured one; the roll-call as names plus a count of the unassigned detections, and
    `- (not requested)` when `--people` was not given; the OCR reading folded onto one line and elided),
    `WriteContexts` (**the token is never printed**, only `stored`/`not set`).
    `WriteRendition` (`render.go`) confirms a file saved by `photos image`: the bare path as a table, the
    path with its size and type in `-o json`/`-o llm`.
    An empty result = the single line `no photos found`, no header — so that an agent does not mistake a header
    for a row.
  - `render.go` — `NamedUID` (**`Name (uid)`** — the way every command that must name a record reports one,
    because a uid alone is unreadable and a name alone cannot be passed to the next command; `SubjectLabel`
    is its person-shaped alias), `formatDistance` (three decimals, the precision
    [`docs/THRESHOLDS.md`](THRESHOLDS.md) states its thresholds to),
    `WriteAlbums`/`WriteAlbum`, `WriteLabels`/`WriteLabel`, `WriteSubjects` (both counts,
    `PHOTOS` and `MARKERS`, because they answer different questions)/`WriteSubject` (the life years
    among them, dashed when unknown — `subjects create` writes them and has to print back what it stored),
    `WriteMembership` (one line: how many photos the album now holds), `WriteBulkResult` (a summary + a table of
    the failed photos **only**) and `WriteAck`. `Ack` is the payload the CLI **makes up itself**: where
    the API answers `204` there is nothing to pass through unchanged, so `-o json` (and `-o llm`) gets
    `{"status":"ok","message":…}` and the pipeline can tell success from failure.
  - `recognition.go` — the renderers of the face surface: `SubjectLabel` (`NamedUID` under a name that says
    it is a person), `WriteFaceList` (a row per detection: who it names, its marker, the detector score, the
    box, the recommended action and the suggested identities with their distances, closed by a summary saying
    how many faces are still unnamed), `WriteFaceAssign` (a detach says *`names nobody`* outright rather than
    printing an empty name), `WriteClusters`/`WriteClusterAssign`/`WriteClusterRemoval`, and
    `WriteMergeReport`. That last one is the **second** result ctl does not pass through unchanged in the
    machine formats, and for a reason peculiar to the operation: a merge deletes the subject it merged away, so
    afterwards the source's name exists nowhere — not in the library, not in the response, which carries only
    uids. Echoing the server's bytes would throw away the only account of who was merged into whom, on the one
    operation that cannot be undone.

  The command tree, the configuration file and the `kukatkoctl` symlink are described in
  [`docs/OPERATIONS.md`](OPERATIONS.md).
