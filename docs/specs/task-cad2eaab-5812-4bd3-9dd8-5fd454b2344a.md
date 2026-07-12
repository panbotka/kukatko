# Audit coverage: album and label mutations

## Summary
Wire the existing audit trail into album and label mutations so the audit log records who created, changed, deleted, or re-organized albums and labels, and when.

## Background (already in place — follow the established pattern)
- `internal/audit` provides `Write(ctx, exec, Entry)` where `exec` may be a `pgx.Tx`, so the audit row commits **atomically in the same transaction** as the mutation. `audit.FromRequest(r, actorUID)` builds the actor/IP/UA envelope; `meta.Entry(action, targetType, targetUID, details)` stamps a record.
- Wired examples to copy: `internal/photos/audit.go` (`mutateAudited`) and `internal/auth/store_apitoken.go` (`inAuditedTx`) — both run the mutation and audit write in one transaction.
- Action constants already exist but are currently unused: `ActionAlbumCreate/Update/Delete`, `ActionLabelCreate/Update/Delete`. Add new constants only for actions that have none (e.g. album add/remove photos, label attach/detach).

## Requirements
Each of these mutations writes exactly one audit entry, in the same transaction as the change, capturing the acting user, the target album/label UID, and useful `details` (changed fields, affected photo UIDs/counts):
- **Album**: create, update (metadata), delete, add photos, remove photos.
- **Label**: create, update, delete, attach to a photo, detach from a photo.
- Atomic with the mutation: if the mutation rolls back, no audit row is written, and vice-versa.
- No change to endpoint responses.

## Implementation notes
- Routes/handlers: `internal/organizeapi/albums.go`, `internal/organizeapi/labels.go` (registered in `internal/organizeapi/organizeapi.go`); business logic in `internal/organize`. The package does not currently import `audit`.
- Follow `mutateAudited`/`inAuditedTx`: get the actor via `auth.UserFromContext` + `audit.FromRequest(r, actorUID)` in the handler, thread the entry into the transactional store method, and call `audit.Write(ctx, tx, entry)` inside the same tx.
- For add/remove-photos and attach/detach, record the affected photo UID(s) in `details`.

## Tests
- Integration tests (real test DB) asserting each mutation inserts the expected `audit_log` row (action, target, actor, details) and that a rolled-back mutation writes none. Follow existing audit integration test patterns (`internal/photos`, `internal/bulk`).

## Done
- `make check` passes and `make dev` starts and answers `/healthz`.
- Update `docs/PACKAGES.md` if the organize packages' responsibilities change (now emit audit); `docs/API.md` only if an endpoint contract note is affected (no response change expected).
- Commit + push per project rules.