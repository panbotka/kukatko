# Notification records, frozen photo sets and per-kind preferences

A push notification in Kukátko has to survive being tapped. The message says "you were tagged in 12 photos", and tapping it must open exactly those 12 — even if somebody tags a thirteenth a minute later, and even if the notification is opened the next morning. So a notification is a **record with a frozen set of photos**, not a transient message. This task builds that record, plus the per-account preferences that decide which kinds an account wants at all. Nothing sends anything yet; delivery and the HTTP surface are separate tasks.

The closest existing shape in this repo is `internal/phototask` — a question about a **frozen** group of photos. Read it before designing this one.

## Requirements

- New package `internal/notification` with a store over pgx.
- A migration adding three tables:
  - **notifications** — one notification: a public `uid` following this repo's uid convention, the account it belongs to (foreign key to `users`, `ON DELETE CASCADE`), the `kind`, the title and body text as they were sent, the deeplink path, when it was created, and when it was read (nullable).
  - **notification_photos** — the frozen set: the notification and a photo (foreign key to `photos`, `ON DELETE CASCADE`), with the order preserved. A notification with no photos is legitimate — the pending-registration kind has none.
  - **notification_prefs** — one row per (account, kind) saying whether that account wants that kind. **An absent row means the default**, which is what lets a new kind ship without a backfill and without a migration.
- Two kinds for now, both named as constants in the package: one for "you were tagged in photos", one for "a registration is waiting for approval". Both default to on. Adding a third kind later must cost a constant and a locale string, never a schema change.
- Store operations: create a notification together with its photo set in **one transaction**; read one by uid **scoped to its owner**, where a foreign uid is indistinguishable from a missing one (the rule `internal/savedsearchapi` already follows, so a uid cannot be probed for existence); mark one read; read an account's effective preferences (the defaults merged with its stored rows); replace an account's preferences.
- A preference change is **audited in the mutation's own transaction**, the way every mutation in this repo is audited. Note that the audit actor is a foreign key, so an integration test that writes an audit entry has to seed the acting user or it fails on a constraint violation.
- Retention: notifications are not kept forever. Expose a purge operation that deletes read notifications older than one threshold and unread ones older than a longer one. Do **not** wire a scheduler in this task — expose the operation and test it; scheduling it is a later decision.
- **Classify all three new tables in `internal/reset/tables.go`.** A table missing from that file fails the `internal/reset` integration tests, and it fails late in the run — do it in this same commit. A notification points at photos, so the guarded library wipe must clear these: a notification referencing photos that no longer exist is a dangling record with no meaning.

## Edge cases

- A photo in a frozen set is later archived, hidden or made private. Deletion is handled by the cascade; the others are not — and the **reader** is who must filter, because visibility depends on who is asking. Make reading a notification's photos a path where the caller can apply the ordinary visibility rules, rather than handing back a raw list that a caller might render blindly.
- The same (account, kind) appearing twice in a preference replace: reject it rather than letting last-write-wins decide silently.
- A notification whose entire photo set has been deleted still exists as a record. That is fine and must not error; the page that renders it will say the photos are gone.

## Verification

Integration tests against the real test database: create-with-photos is atomic (a failure leaves no half-written notification); the set keeps its order; reading somebody else's uid is not found rather than forbidden; marking read is idempotent; effective preferences merge defaults with stored rows; a replace is audited with the acting user; the purge respects both thresholds and leaves everything newer alone.

## Documentation

`docs/PACKAGES.md`, one line in the `## Package map` in `CLAUDE.md`, and the data model in `docs/ARCHITECTURE.md`.