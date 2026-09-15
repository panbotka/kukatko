# User profile pictures

Let a user give their account a real picture instead of today's coloured initial: upload one, pick a photo from the library, or inherit the face of the person the account is linked to.

Today every user is drawn as `InitialAvatar` — a coloured circle with the first letter of their name. Much of the machinery for a better answer already exists: `GET /subjects/{uid}/avatar` (`internal/avatarapi`) already cuts a square picture from a library photo server-side via `internal/avatar`, and `users.subject_uid` already links an account to a person.

## Requirements

- A user's picture resolves through this chain, first match wins: an uploaded picture, then a library photo the user picked, then the avatar of the subject named by `users.subject_uid`, and finally today's coloured initial.
- The linked subject's face is the **default** — an account that already names a person shows that face with no action from the user.
- An uploaded picture is stored **in Postgres**, not in the object store. The server re-encodes it on receipt to a square JPEG no larger than 512 px a side and keeps only those bytes; the submitted original is never retained. This deliberately adds no `storekeys` Kind, so backup, storage migration and library reset need no changes.
- Upload accepts the formats the library decodes without CGO (JPEG, PNG, WebP), and refuses an over-large request with 400 rather than reading it into memory.
- A picked library photo is stored as a uid reference and rendered through the existing `internal/avatar` centre-crop path — the same renderer the subject avatar uses. The pixels are not copied.
- **A photo flagged `private` or `hidden_from_library` may not become an avatar.** The server refuses with 400 and the UI explains why; otherwise the private flag could be sidestepped by putting the photo on a profile where everyone sees it.
- If a picked photo later disappears or becomes private — archived, purged, flagged — the avatar falls back down the chain instead of serving it or erroring.
- A new endpoint serves a user's picture with the same shape as the subject avatar: RequireAuth, JPEG, an ETag answering 304 on revalidation, and **404 when the user has no picture of any kind**, which is the client's cue to draw the initial.
- The account page gains a section to set and clear the picture, covering all three sources. Clearing returns the user to the initial.
- The picture replaces the initial wherever a user is named today — at minimum the comment threads and the navbar account control.
- **This is not audited.** Following the existing rule at `internal/auth/handlers_auth.go:177`, the audit trail records what was done to an account by somebody else; a user changing their own profile is not that.

## Acceptance

- A user with a linked subject sees that face without configuring anything.
- Upload, pick and clear each round-trip, and the chain falls back correctly when the picked photo is archived or flagged private after the fact.
- Picking a private or hidden photo is refused with 400.
- The endpoint answers 404 for a user with no picture, and 304 to a caller presenting the current ETag.
- Frontend tests cover the account section and the fallback to the initial.
- `make check` passes.

## Implementation notes

- Follow `internal/avatarapi/avatarapi.go` for the serving shape — the ETag, the ten-minute cache, and the "nothing to show is a 404" decision are already worked out there.
- `internal/avatar` already centre-crops a hand-picked photo (that is how a subject's `cover_photo_uid` works); the picked-photo case should reuse it rather than grow a second cropper.
- Keep the chain's resolution on the server. The client asks one endpoint and either gets a picture or a 404 — it should not need to know which of the three sources answered.
- Adding a column to `users` breaks hand-rolled scans elsewhere that do not use the canonical column list, and a new CHECK on `users` would break test seeders in many packages. Grep repo-wide and run the integration tests, not just `make check`.
- Docs: `docs/API.md` (the new endpoint), `docs/PACKAGES.md`, `docs/FRONTEND.md` (the account section and the avatar components).