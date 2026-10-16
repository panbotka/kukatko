## Context
Round-2 review of the multi-tenancy programme (2026-10-14, main 672357f6). Report: `<path>/multitenancy-review-2/isolation-review.md` part "background jobs and notification fan-outs": §J-1, §J-3, §J-4, §J-6. Verify with `rg` each still holds.

Findings:
- J-1 (should-fix): `internal/albumfeaturedjob/service.go` (~104-109) does not check `approved_at`; a pending (unapproved) member of B gets a push/e-mail with B's name and the album title. Every other kind requires approval.
- J-4: the tag notification (`internal/tagnotifyjob`) checks enabled/approved when the notice is recorded, not when the window closes and is sent; an account disabled/removed/unapproved in between still gets it.
- J-3: a queued `push_send` follows the browser subscription (endpoint), not the account; on a shared browser where a second account later subscribed the same endpoint, account A's notification reaches account B.
- J-6: notifications (and push subscriptions' pending jobs) of a deactivated project are never purged / keep being delivered.

## Requirements
- One shared "may receive a notification of project P" predicate (approved member, account enabled, project active — maintainers only if member) used by every producer (featured album, tags, mentions, task assigned, task digest, what's new if it notifies) AND re-checked at delivery time in `internal/notifydeliver` / `pushjob` / `mailjob` (drop silently, count in a metric).
- `push_send` carries the account uid and is dropped if the subscription now belongs to another account.
- Deactivating a project stops its pending notification jobs; retention purge covers deactivated projects (decide: purge their in-app rows at deactivation or leave them to normal retention but hide them — document).
- Integration tests for each, failing on current code.
- Docs: PACKAGES.md for the touched packages, OPERATIONS.md if behaviour visible to operators.

## Out of scope
New notification kinds; preference UI.