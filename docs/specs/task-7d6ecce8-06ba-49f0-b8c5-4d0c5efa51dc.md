# Back Up The Bucket

`internal/backup` walks originals on the local disk and syncs them to S3. Once originals live in Cloudflare R2, that source is meaningless — and copying the library down to the VPS just to upload it again would be absurd on a small disk.

Object storage is not a backup. **This design deliberately does not rely on bucket versioning:** R2 has no native object versioning that could be confirmed in its documentation, so a second, independent bucket carries the whole of the protection against an accidental or malicious delete.

## Requirements

- `internal/backup` gains an originals source that reads from the object store and copies to a second bucket **server-side**, without streaming the payload through the application.
- The second bucket is configured independently of the primary one: its own endpoint, region, bucket, and credentials, so it can live with a different provider. Do not assume both are R2.
- The database dump keeps working exactly as it does today, and lands in the backup bucket alongside the originals.
- Retention continues to apply to dumps. **Do not expire originals** — a deleted original is a lost photo, and with no versioning there is nothing to restore it from.
- Deletion in the primary bucket must never propagate to the backup bucket. The copy is additive.
- If the backup target is not configured, the backup must fail loudly rather than silently backing up nothing.

## Documentation

`docs/RESTORE.md` must state plainly that there is no versioning to fall back on, that the second bucket is therefore the only protection against deletion, and must give the exact steps to restore both the database and the originals into a fresh bucket. Update `docs/OPERATIONS.md` with the new configuration keys and `docs/PACKAGES.md` with the new source.

## Verification

`make check` must pass. Add an integration test against a real S3-compatible endpoint (MinIO is acceptable) covering a bucket-to-bucket copy, a re-run that copies nothing new, the fact that an object deleted from the primary survives in the backup, and the loud failure when no target is configured.