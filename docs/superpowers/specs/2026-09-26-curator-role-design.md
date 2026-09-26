# The curator role — design

**Status:** approved 2026-09-26 — awaiting implementation. No code has been written for
this document.

Kukátko's role ladder has one rung for "may look" and the next one for "may change
anything". That is too coarse for handing the library to a family member. The people who
should be naming faces, filling albums and attaching labels are not the same people who
should be moving capture dates, archiving photos or merging near-duplicates — not on day
one, anyway.

This document inserts a rung between `viewer` and `editor`: **`curator`**, the role that
curates the catalogue (people, faces, albums, labels) without being able to rewrite or
destroy what the catalogue says about a photo. Promotion to `editor` stays a manual act
by an admin, once the person has earned it.

## 1. What the ladder looks like today

`internal/auth/role.go` defines `viewer < editor < admin < maintainer` and four
predicates — `CanWrite()`, `IsAdmin()`, `CanMaintain()`, `CanImport()` — each an explicit
`==` disjunction. There is **no rank arithmetic in Go**: a role satisfies a predicate only
by being named in it. The private `requirement` enum and `authorize(role, req)` turn those
predicates into the `RequireAuth` / `RequireWrite` / `RequireAdmin` / `RequireMaintainer`
middlewares, which every API package receives as an injected
`func(http.Handler) http.Handler` config field, wired in `cmd/kukatko/*.go`.

`RequireWrite` is the single gate for **67 endpoints**. That is the number this design has
to split.

The frontend is the mirror image: `web/src/services/auth.ts` already has a numeric
`ROLE_RANK` and a `roleAtLeast()`, plus `GuardRole = Exclude<Role, 'viewer'>` and an
exhaustive `MESSAGE_KEYS` map in `ForbiddenPage` — which means TypeScript will refuse to
build until a new role gets its sentence. That is a feature, not an obstacle.

## 2. The mechanism: one explicit predicate

`CanCurate()` joins the four existing predicates, and nothing already there changes:

```go
// CanCurate reports whether r may curate the catalogue — faces, people, albums and
// labels — without the wider write access that rewrites a photo's own metadata.
func (r Role) CanCurate() bool { return r == RoleCurator || r.CanWrite() }
```

`CanWrite()`, `IsAdmin()`, `CanMaintain()` and `CanImport()` are left byte-for-byte alone,
so none of the 25 endpoints that stay behind `RequireWrite` can be opened by accident. A
`requireCurate` constant, one `case` in `authorize()` and a `RequireCurator` middleware
complete the Go side.

**The rejected alternative** was to give Go the frontend's numeric rank and express the
predicates as `r.AtLeast(RoleEditor)`. It would make the *next* role a one-line change,
but it buys that by rewriting all four predicates of the security core for no functional
gain today, and it has a property that is wrong for authorization code: a newly inserted
rung would silently inherit permissions instead of having to be named. Explicit
enumeration is the safer default here. The rank stays where it already is — the frontend.

## 3. Where the line falls

`viewer` keeps what it already has, which is more than its name suggests: favorites,
ratings, comments on a photo, and answering a task — all deliberately on `RequireAuth`
with ownership checks in the handler.

### 3.1 Curator (42 endpoints move to `RequireCurator`)

| Package | Endpoints |
| --- | --- |
| `photoapi` | `POST /photos/{uid}/faces/assign`, `POST /photos/{uid}/people`, `DELETE /photos/{uid}/people/{subjectUID}` |
| `organizeapi` | all 10: `POST`/`PATCH`/`DELETE /albums`, `POST`/`DELETE /albums/{uid}/photos`, and the same five for `/labels` |
| `peopleapi` | `POST /subjects`, `PATCH /subjects/{uid}`, `DELETE /subjects/{uid}`, `POST /subjects/{uid}/merge` |
| `familyapi` | `POST /subjects/{uid}/relations`, `DELETE /subjects/{uid}/relations/{uid2}`, `PATCH /families/{uid}` |
| `clusterapi` | `GET /faces/clusters`, `POST /faces/clusters/{id}/assign`, `POST /faces/clusters/{id}/remove-face` |
| `reviewapi` | `GET /review/queue`, `POST /review/answer` |
| `feedbackapi` | `POST`+`DELETE` of `face-rejections`, `face-confirmations`, `label-rejections`, `duplicate-marker-dismissals` (8) |
| `dupmarkersapi` | `POST /duplicate-markers/keep`, `POST /duplicate-markers/invalid` |
| `sweepapi` | `GET /faces/sweep` |
| `candidatesapi` | `POST /subjects/{uid}/candidates` |
| `outlierapi` | `GET /subjects/{uid}/outliers` |
| `expandapi` | `GET /albums/{uid}/similar`, `GET /labels/{uid}/similar` |
| `ingest` | `POST /upload` |
| `bulkapi` | `POST /photos/bulk` — **field-limited**, see §4 |

Several of those are `GET`s. They are gated not because they mutate but because they lead
into a write flow (the sweep, the candidate search, "photos similar to this album"), and a
curator owns those flows end to end, so they move with them.

`duplicate-marker-dismissals` belongs here, not with the other duplicate feedback: it
records "these two markers on one photo are not the same person twice", which is face
work, the same workflow as `dupmarkersapi`.

### 3.2 Editor and above (25 endpoints keep `RequireWrite`)

| Package | Endpoints |
| --- | --- |
| `photoapi` | `PATCH /photos/{uid}` (metadata, **including the capture date**), `PUT /photos/{uid}/edit`, `archive`, `unarchive`, `hide`, `unhide`, `POST /photos/stack`, `stack/primary`, `unstack`, `unstack-all`, `regenerate-thumbnail` (11) |
| `phototaskapi` | all 7 writes — creating a task, editing it, its photos and its participants (answering and commenting stay `RequireAuth`, so a viewer keeps them) |
| `duplicatesapi` | `GET /duplicates`, `POST /duplicates/merge` |
| `feedbackapi` | `POST`+`DELETE` of `duplicate-dismissals` and `duplicate-confirmations` (4) |
| `bulkapi` | `POST /photos/bulk/location-summary` — it exists to feed the bulk location operation, which a curator may not use |

The rule behind the line: **at the level of subjects and of collections a curator may
delete and merge; at the level of a photo it may not.** Deleting an album throws away a
stitching-together, not a photograph; merging two subjects is undone by hand but loses no
media. Archiving a photo, rewriting its date or merging two near-duplicates touches the
one thing the library exists to preserve.

`POST /subjects/{uid}/merge` is the sharpest tool on the curator's side of the line, and
it was a deliberate choice, confirmed during design.

## 4. The mixed endpoint: `POST /photos/bulk`

`internal/bulk`'s `Operations` carries `AddAlbums`, `RemoveAlbums`, `AddLabels` and
`RemoveLabels` in the same struct as `Title`, `Description`, `TakenAt`, `Location`,
`ClearLocation`, `Archive`, `Hide`, `Favorite` and `Rating` — and the whole batch UI
(`BatchActionBar`, `BulkEditModal`) goes through it. Leaving it to editors would cost a
curator "select forty photos, put them in an album", which is the work the role exists
for; opening it would hand over the capture date.

So the guard moves to `RequireCurator` and authorization continues **inside the handler**,
on the fields:

- A pure predicate over `Operations` answers "does this batch ask for anything beyond
  album and label membership?" It lives next to the type it judges, in `internal/bulk`,
  takes no I/O, and is unit-tested on its own.
- `bulkapi`'s handler asks it, and if the answer is yes while the caller only
  `CanCurate()`, the request is 403 before a single row is touched.
- The per-user operations (`Favorite`, `Rating`) are the one wrinkle: a viewer may already
  set both one photo at a time, so counting them as "beyond curation" would make bulk
  stricter than the single-photo route. They are therefore allowed for a curator too.

This is not a new pattern: comments and saved searches already authorize in the handler
rather than at the gate.

## 5. Database

A migration in the shape of `0036_role_maintainer.sql` — drop the constraint, re-add it
with the new value (no data migration needed, nothing is being retired):

```sql
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('viewer', 'curator', 'editor', 'admin', 'maintainer'));
```

The next free number is `0085` as of writing; the implementer confirms it, because a
parallel task may have claimed it. `sessions.role` carries no CHECK and needs no change.
`authorize()` reads `p.user.Role`, not the session's cached copy, so a promotion takes
effect on the next request rather than the next login. No new table, so
`internal/reset/tables.go` is untouched.

Self-registration keeps landing on `RoleViewer` (`internal/auth/register.go:119`): the
shared-secret registration is open, so a new account must not arrive holding write access
of any kind. The curator role is reached only by an admin changing it.

## 6. What deliberately does not change

- **MCP.** `internal/mcpapi` splits into a writable and a read-only server on
  `CanWrite()`, which is untouched, so a curator gets the read-only server and every write
  tool keeps its second lock in `writerFromContext`. This is the right answer for now and
  costs no code — only a line in `docs/MCP.md`.
- **API tokens and `ctl`.** A token authenticates as its owner and carries their role, so
  a curator's token is a curator. Nothing to do.
- **Role management.** `UsersPage`'s `availableRoles` filter restricts only the
  `maintainer` role to maintainers; `curator` is assignable by any admin, like `editor`.
  The last-enabled-maintainer guard is unaffected.
- **Comment moderation** (`IsAdmin()`) and the pending-registration notification
  (`admin` + `maintainer`) are unchanged.

## 7. Frontend

38 files under `web/src` consult `canWrite`; roughly twenty of them gate curation and swap
to `canCurate`.

- `services/auth.ts` — `'curator'` into the `Role` union and into `ROLE_RANK` between
  `viewer` and `editor`; a `canCurate()` helper beside `canWrite()`. `GuardRole` picks the
  new role up automatically.
- `auth/AuthContext.tsx` + `AuthProvider.tsx` — expose `canCurate`.
- `pages/ForbiddenPage.tsx` — `MESSAGE_KEYS` is exhaustive over `GuardRole`, so the build
  fails until `curator` has its sentence. That is the compiler finding the work.
- `App.tsx` route gates move to `role="curator"`: `/review`, `/albums/:uid/faces`,
  `/people/clusters`, `/faces`, `/expand`, `/recognition`, `/outliers`,
  `/duplicate-markers`, `/upload`. They stay `role="editor"` for `/duplicates`,
  `/duplicates/compare` and `/trash`.
- `services/users.ts` — `'curator'` into `ROLES` in ladder order, which drives the admin
  select.
- Navigation — `components/navItems.ts`, `Layout.tsx`, `MobileNavDrawer.tsx`,
  `MobileTabBar.tsx`: the curation entries follow `canCurate`, the rest keeps `canWrite`.
- Page and panel level — albums, labels, people, subject, family tree, faces panel,
  upload, share target and the organize panel follow `canCurate`; the metadata panel,
  technical details, photo location, stack strip and the task pages keep `canWrite`.
- `BulkEditModal` shows a curator only the album and label fields (plus favorite and
  rating), matching §4. `useBulkEdit` follows.
- A forbidden action is **not shown at all** — the same mechanic `canWrite` already uses
  for a viewer. No greyed-out controls, no explanatory badges.
- i18n, both `web/src/i18n/locales/cs/common.json` and `en/common.json`: `roles.curator`
  (cs: *kurátor*), `forbidden.message.curator`, and the role's paragraph in
  `help.sections.roles`.

## 8. Testing

There is no harness that mounts the whole API — `server.WithAPI` meets only in
`cmd/kukatko/serve.go` — and role boundaries are tested per package today (14 assertions
on 403 in `photoapi`, 8 in `auth`, and so on). This design keeps that shape and adds three
layers:

1. **The decision matrix**, pure, in `internal/auth/role_test.go`: five roles × five
   requirements in one table. It pins what each role may do with no HTTP in the way.
2. **One curator case per touched package**, added to the boundary test that package
   already has. Only this proves the *wiring* — that the route actually hangs on the new
   gate rather than merely deserving to.
3. **A unit test for the bulk field predicate**, covering an album-only batch, a batch
   with a capture date, a batch with a location, and the favorite/rating exception.

Frontend: vitest over the gating helpers and the navigation, following the existing
`canWrite` tests.

## 9. Documentation routing

- `docs/API.md` — the guard column of 42 route rows, in the task that moves them.
- `docs/PACKAGES.md` — `internal/auth` (the new predicate and middleware),
  `internal/bulk` (the field predicate).
- `docs/MCP.md` — one line: a curator gets the read-only server.
- `docs/FRONTEND.md` — `canCurate`, the route gates, the `BulkEditModal` field set.
- `docs/ARCHITECTURE.md` — the ladder is part of the data model description.
- `README.md` — the role list is user-visible.
- `CLAUDE.md` — the `internal/auth` package-map line names the roles, so it changes once.
  Nothing else; `make docs-budget` guards the file.

## 10. Delivery in five steps

Each step is independently shippable, and every intermediate state errs the safe way: a
curator is **under**-privileged until the last step, never over.

1. **The role and the gate.** `role.go`, `RequireCurator`, the migration, the decision
   matrix, docs. The role exists, is used nowhere, and cannot even be assigned from the UI
   (`ROLES` is untouched) — a curator behaves exactly like a viewer.
2. **People and faces** — 28 endpoints: `photoapi` faces/people, `peopleapi`,
   `familyapi`, `clusterapi`, `reviewapi`, `sweepapi`, `candidatesapi`, `outlierapi`,
   `dupmarkersapi`, and the eight curation entries of `feedbackapi`.
3. **Albums, labels, upload, bulk** — 14 endpoints: `organizeapi`, `expandapi`,
   `POST /upload`, and `POST /photos/bulk` with the field predicate of §4. The backend is
   then complete and the line closed.
4. **Frontend foundation.** `auth.ts`, the context, the route gates, `ForbiddenPage`,
   `ROLES` and the admin select, navigation, i18n. The role becomes assignable and lands
   in the right places; individual controls on pages may still be hidden.
5. **Unlock the actions.** The `canWrite` → `canCurate` swap across the curation
   components, `BulkEditModal`'s field set, vitest, README and the help section.

## 11. Out of scope

- **Granular per-user capabilities.** Considered and rejected: a ladder rung needs no new
  table, no new admin UI, and keeps the strict inheritance the codebase is built on.
- **Automatic promotion** after a time or an activity threshold. An admin promotes.
- **A second new rung** (faces first, albums later). One rung covers the need.
- **`cmd/kukatko/dirimport.go:347`** compares `== auth.RoleAdmin` and therefore misses a
  maintainer when picking an actor for CLI import audit entries. A pre-existing bug this
  work merely uncovered; it is unrelated to the curator role and belongs in its own task.
