# Audit coverage: subjects and face assignment

## Summary
Wire the existing audit trail into subject (people/pets) mutations and face↔subject assignment so the audit log records who changed identities and face assignments, and when.

## Background (already in place — follow the established pattern)
- `internal/audit.Write(ctx, exec, Entry)` runs inside the mutation's `pgx.Tx` (atomic). Build the envelope with `audit.FromRequest(r, actorUID)` (actor from `auth.UserFromContext`), stamp with `meta.Entry(action, targetType, targetUID, details)`.
- Copy the established pattern from `internal/photos/audit.go` (`mutateAudited`) / `internal/auth/store_apitoken.go` (`inAuditedTx`).
- `ActionFaceAssign` already exists and is unused. Add new action constants for subject create/update/delete (and face unassign) if none exist.

## Requirements
Each of these mutations writes one audit entry, atomically in the same transaction as the change, capturing the actor, the target UID, and useful `details`:
- **Subject**: create, update, delete.
- **Face**: assign a face to a subject, and unassign/remove a face.
- Atomic with the mutation (rollback ⇒ no audit row).
- No change to endpoint responses.

## Implementation notes
- Handlers/routes: `internal/peopleapi/` (`peopleapi.go` and its handlers); business logic in `internal/people`.
- For face assign/unassign, record the face UID/marker and the subject UID in `details`; for subject delete, record enough to identify what was removed (name/type).
- Thread the audit entry into the transactional store methods and call `audit.Write(ctx, tx, entry)` in the same tx.

## Tests
- Integration tests asserting each mutation inserts the expected `audit_log` row (action/target/actor/details) and that a rolled-back mutation writes none. Follow existing audit integration test patterns.

## Done
- `make check` passes and `make dev` starts and answers `/healthz`.
- Update `docs/PACKAGES.md` if the people packages' responsibilities change.
- Commit + push per project rules.