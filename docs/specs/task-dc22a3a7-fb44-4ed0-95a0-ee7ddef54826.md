# The isolation test harness

## Context

Kukátko is becoming multi-tenant with explicit `project_id` predicates in every query. The biggest risk is a forgotten `WHERE project_id`: a silent leak between tenants. Every later step that scopes a package must prove isolation with the same helper, so this step builds that helper first. Design: `docs/superpowers/specs/2026-10-11-multi-tenancy-design.md`, section 6 ("Isolation tests"). Step 07 of 40. Already in the tree (verify): `internal/project`, `project_members`, `project_id` columns on the root tables (with a transitional DEFAULT), the API mounted at `/api/v1/p/{project}` (plus unprefixed aliases), `internal/apitest` (the HTTP client the API tests use — never `http.DefaultClient`).

Look at how existing integration tests seed data (`rg -n "func seed" internal --type go -g '*_test.go' | head`) and reuse those idioms; the harness is test-support code behind the `integration` build tag.

## Requirements

- Package `internal/projecttest`:
  - `Seed(t testing.TB, pool *pgxpool.Pool, opts ...Option) *Fixture` creates two projects: `A` = the bootstrap project (`project.BootstrapUID`, slug `v`) and `B` = a fresh project (slug `b-<random>`, prefix `projects/<uid>/`). In **each** project it creates: one approved member (role `admin` by default; `WithRole(which, role)` option), one photo with its `photo_files` row (no object in storage needed), one subject with one marker/face on that photo, one album containing the photo, one label on the photo, one task over the photo, one saved search for the member, one upload link. Every uid is on the fixture: `f.A.PhotoUID`, `f.B.AlbumUID`, `f.A.UserUID`, … plus `f.A.Slug`.
  - `f.ClientFor(t, server *httptest.Server, which Side) *apitest.Client` signed in as that side's member (session cookie); `f.PathFor(which, suffix string) string` → `/api/v1/p/<slug>/<suffix>`.
  - `AssertInvisible(t, client, path)` — expects 404 and asserts the body contains none of the other side's uids; `AssertListExcludes(t, client, path, otherUIDs ...string)` — 200 and none of the uids in the body; `AssertSameShape(t, respForeign, respMissing *http.Response)` — identical status and body (a foreign row must be indistinguishable from a missing one); `AssertStoreScoped(t, fn func(projectUID string) ([]string, error))` — calls fn for A and B, asserts both non-empty and disjoint.
- Worked example: an integration test in `internal/photoapi` using the harness: `GET /p/<B>/photos/<A photo uid>` is 404 and `AssertSameShape` against a random missing uid. If it is red because `photoapi` is not scoped yet (expected at this step), keep the test and `t.Skip("unscoped until the photos step")` with that exact reason, so the photos step only has to delete the skip.
- Docs: `docs/ARCHITECTURE.md` testing strategy gains "Tenant isolation": the helper, and the rule that every scoped package adds at least one store-level and one HTTP-level isolation test; `docs/PACKAGES.md` entry; `CLAUDE.md` package map line.

## Out of scope

Scoping any package. The harness must not change production code except where a seed needs an exported constructor that already exists.
