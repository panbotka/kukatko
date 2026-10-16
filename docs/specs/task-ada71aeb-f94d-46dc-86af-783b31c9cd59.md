## Context
Round-2 review of the multi-tenancy programme (2026-10-14, main 672357f6). Full reports live on the NAS: `<path>/multitenancy-review-2/` — read `isolation-review.md` §D-1 and `walk-report.md` §F1 first. Design spec: `docs/superpowers/specs/2026-10-11-multi-tenancy-design.md` (projects are FOREIGN tenants: a member of B must learn nothing about A, not even user names). Before you start, verify with `rg` that the code still looks as described; if it is already fixed, say so and finish as needs_review.

Finding (reproduced on box staging): `GET /api/v1/p/{project}/review/leaderboard` (internal/reviewapi/reviewapi.go ~103, 202-219) returns the same instance-wide board for every project. `internal/review/leaderboard.go` (leaderboardQuery ~113-128) and `internal/review/streak.go` (~52) read `audit_log` with no `project_id` predicate, so a member of an empty project B gets uid, display name, yes/no counts and streak of every reviewer in every project (e.g. a v-only curator). The tenancy guard did not catch it because `audit_log` is exempt in `internal/tenancyguard/tables.go` (~51) — its `project_id` is nullable.

## Requirements
- The leaderboard and the streak count only review decisions recorded in the request's project (`audit_log.project_id = $project`), and list only accounts that are approved members of that project at read time (a former member's past decisions disappear from the board; maintainers who are not members do not appear). Decide and document whether a maintainer who reviewed while acting in the project counts — default: only if they are a member.
- Every other reader of `audit_log` in the codebase is checked for the same gap (rg `audit_log` outside internal/audit writes and internal/auditapi); scope or justify each in the PR/commit message.
- Make the tenancy guard cover reads of `audit_log`: a SELECT over `audit_log` must carry a `project_id` predicate unless allow-listed with a reason (the instance-level `/audit` listing for maintainers is the legitimate exception). Keep the guard's existing behaviour for writes (instance entries legitimately have NULL project).
- Integration test with `internal/projecttest`: decisions in A, a member of B gets an empty board; a member of both sees only the current project's counts; a removed member vanishes. The test must fail on the current code.
- Docs: docs/API.md (leaderboard is per project), docs/PACKAGES.md for review + tenancyguard.

## Out of scope
Leaderboard UI redesign; other oracles (audit `?user=`, task participants) — separate tasks.