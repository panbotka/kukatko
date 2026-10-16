## Context
Round-2 review of the multi-tenancy programme (2026-10-14, main 672357f6). Reports: `<path>/multitenancy-review-2/isolation-review.md` §D-2, §D-3, Part I §I-1; `fixes-review.md` §B1; `walk-report.md` §F13 (exact-match add). Design spec: foreign tenants — B must learn nothing about A's accounts. Verify with `rg` each still holds.

Findings:
- `GET /p/{project}/audit?user=<name|uid>` (`internal/auditapi/auditapi.go` ~143-151, ~451-466): a project admin gets 200 (empty) for an account that exists anywhere and 400 for one that does not → instance-wide username oracle (round 1 fixed only `/audit/mine`).
- Exact-match "add member by username/e-mail" (`internal/project/member_identifier.go` ~57-117): always 202, but the project's own audit trail records `no_such_account` with the raw identifier, and a hit adds the account approved without its consent and immediately shows its name + last login in the member list.
- `/auth/me` keeps returning `last_project_slug` after the account lost access to that project (`internal/auth/store.go` ~449).

## Requirements
- Audit `?user=` resolves only against the project's members (current + former members who appear in that project's audit), and answers identically (200 empty) for a non-member and a non-existent account. The instance `/audit` (maintainer) keeps its current behaviour.
- Member add by identifier: the project audit entry must not distinguish hit from miss (no `no_such_account`, no raw identifier — log a neutral "member add requested" or nothing for a miss); a hit creates a **pending invitation-like membership** the account has to accept (or approve themselves on next sign-in) instead of an approved membership — check the design spec §invites/exact match and implement what it says; if the spec says "approved immediately", keep it but stop showing name/last login until the account has signed into that project once, and document the decision. Discuss the trade-off in the commit message.
- `last_project_slug` is cleared/ignored when the account no longer can enter it (removed, project deactivated for a non-maintainer).
- Integration tests: equal responses for non-member vs non-existent; audit content for miss; `/auth/me` after removal. Must fail on current code.
- Docs: API.md, PACKAGES.md (project, auditapi, auth); FRONTEND.md if the members UI changes.

## Out of scope
Task participants / mentions (separate task); leaderboard.