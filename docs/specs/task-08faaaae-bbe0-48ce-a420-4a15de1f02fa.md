# Scope duplicate-marker dismissals by project

The only write surface in `internal/feedback` that takes no project: a curator of project A can dismiss or undismiss a repeated-marker group of project B by posting B's photo and subject uids under `/p/A/…`. B's duplicate-marker review silently stops offering that group, the 204-vs-404 answer is an existence oracle for B's uids, and the audit entry lands in A naming a B subject.

## Context (verify with rg before starting)
- `internal/feedback/markerdismissals.go` `DismissDuplicateMarkers` (~51–66) and `UndismissDuplicateMarkers` (~76–91) take no `projectUID`, run `inAuditedTx` and a bare `INSERT INTO duplicate_marker_dismissals (photo_uid, subject_uid, …)`; the FK is the only check.
- Every other opinion in the package goes through `inProjectAuditedTx`/`inScopedAuditedTx` with `requireFaceTargetInProject`/`pairProof` (`internal/feedback/project.go`).
- Handlers: `internal/feedbackapi/handlers.go` (~196–232), routes `POST`/`DELETE /p/{project}/feedback/duplicate-marker-dismissals` (RequireCurator), wired in `feedbackapi.go` (~144–145).
- Instance-wide exclusion readers used in per-project computations: `internal/feedback/dismissals.go` (~97), `duplicateconfirmations.go` (~97), `markerdismissals.go` (~115), consumed by `internal/duplicates/duplicates.go` (~317, 330) and `internal/dupmarkers/dupmarkers.go` (~187).
- `internal/project/member_store.go` `Store.SetSubject` (~241–263) links a membership to a subject with no proof the subject belongs to the project (no production caller; `internal/auth/store.go` `checkSubject` is the live path and does prove it).
- `internal/auth/store.go` (~48–49): `/auth/me` top-level `subject_uid` comes from the bootstrap project's membership (MT04 transition code); `projects[]` already carries a per-membership `subject_uid`.

## Requirements
- `DismissDuplicateMarkers` and `UndismissDuplicateMarkers` take `projectUID` after `ctx` and prove, inside the audited transaction, that the photo and the subject both belong to that project (reuse the existing face-target proof); a foreign or missing pair answers the same 404 as a missing one, and nothing is written. The handlers pass the request's project; the audit entry carries it.
- Isolation tests with `internal/projecttest`: a curator of A dismissing a B photo+subject gets 404, B's `GET /duplicate-markers` still lists the group, no `duplicate_marker_dismissals` row exists; the same for `DELETE` against an existing B dismissal.
- The three exclusion-list readers take `projectUID` and return only that project's pairs/groups; their consumers pass the project they are computing for. Store-level tests show B's dismissals are not returned for A.
- `project.Store.SetSubject` either verifies the subject is in the project (same query the auth path uses) or is deleted.
- `/auth/me`: the top-level `subject_uid` is documented in `docs/API.md` as the bootstrap membership's value kept for compatibility, or removed in favour of `projects[].subject_uid` with the frontend updated — pick one and keep the frontend tests green.
- `docs/API.md`, `docs/PACKAGES.md` (feedback) and `docs/SECURITY_AUDIT.md` record the change.

## Out of scope
- The tenancy guard (child tables are outside its scope by design); a separate task strengthens it.