# Fix: storage migration leaves the metadata sidecar on the local disk it exists to empty

The storage migration moves originals to the object store so the local disk can be
reclaimed, but it never moves each photo's metadata sidecar — the disaster-recovery
artifact — so reclaiming the disk loses it.

## Root cause (verified)
- `internal/storagemigrate/objects.go:49-66` (`plan`) enumerates only the original
  plus its cached thumbnails. The photo's sidecar
  (`sidecars/YYYY/MM/name.jpg.yml`, written by `internal/sidecarexport` to the same
  local root) is never planned, so it is never uploaded to the destination.
- `internal/storagemigrate/storagemigrate.go:463-472` (`deleteLocal`) removes only the
  original.

## Failure scenario
After a `DeleteLocal` migration and switching the primary store to R2, originals live
in R2 while the pre-migration sidecars remain SOLELY on local disk. When the operator
reclaims that disk (the entire point of the migration), those sidecars are lost — and
the sidecar is precisely the artifact that "lets the catalogue survive losing the DB".
(Thumbnails are regenerable, so leaving them is harmless; sidecars are not.)

## Requirements
- Include each photo's sidecar object in the migration plan: upload + verify it to the
  destination (same durability guarantee as the original — verify before the local
  original is deleted).
- Keep the resumable upload→verify→commit→delete ordering intact for the sidecar too.
- Do not delete a local original until its sidecar is durable in the destination.
- If, for design reasons, sidecars are intentionally kept local, then instead make the
  migration REFUSE / warn loudly and document that a sidecar re-export or backup is
  mandatory before reclaiming the disk — but moving them with the original is preferred.

## Testing
- Integration test: run a migration, assert the sidecar exists in the destination
  store before/at the point the local original is removed. `make check` must pass.
- Update `docs/RESTORE.md` / `docs/OPERATIONS.md` if the migration contract changes.