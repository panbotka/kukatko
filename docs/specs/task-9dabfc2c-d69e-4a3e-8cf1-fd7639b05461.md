# API token exempt from rate limits

Let an admin mark an API token as unlimited, so an agent driving the library through `kukatko ctl` is not throttled by the comment, upload and bulk rate limiters.

## Requirements

- A new migration adds a boolean `unlimited` column to `api_tokens`, defaulting to false. `auth.APIToken` carries it and it appears in the token JSON.
- A request authenticated by a Bearer token whose row has `unlimited = true` is exempt from rate limiting. A request authenticated by a session cookie is never exempt, whatever the user's role — a browser does not need it, and a stolen cookie must not bypass the limits.
- The exemption covers three limiters: the per-user comment limiter on `POST /photos/{uid}/comments`, and the per-IP limiters on `POST /upload` and `POST /photos/bulk`. The map-tile limiter is deliberately left alone: it protects paid mapy.com credit, not our own CPU.
- `/upload` and `/photos/bulk` today run their limiter ahead of `RequireWrite`. The limiter must move behind the auth guard so the caller's identity is known when it runs. The bucket key stays the client IP for everyone who is not exempt, and their throttling behaviour must not change.
- An exempt request must not consume a token from any bucket — it passes through untouched rather than being refilled afterwards.
- `POST /auth/tokens` accepts an `unlimited` field, honoured only when the caller is an admin. Any other role asking for it is refused with 403 and no token is created.
- A new `PATCH /auth/tokens/{id}` turns the flag on and off on the caller's own token, again admin-only. A non-admin gets 403; a token id belonging to someone else answers 404.
- Both the creation and the toggle are audited in the same transaction as the write, following the project's audit rule.
- The account page's API-tokens card offers the switch only to an admin, and marks a token that carries the flag.

## Acceptance

- An unlimited token writes well past the comment burst (>10 in a row) without a 429; a plain token gets 429 once the burst is spent.
- A non-admin is refused on both the create and the patch path.
- The audit trail carries a row for each change.
- `make check` passes.

## Implementation notes

- The authenticated identity already reaches handlers through the request context (`principalFromContext` in `internal/auth/http.go`). The flag belongs on that principal, so the limiter reads it without a second database lookup.
- `internal/ratelimit` needs an exemption predicate on its middleware rather than a magic bucket key.
- Moving the limiter behind `RequireWrite` means an unauthenticated flood on those two routes now reaches the auth lookup before being rejected. Both routes already require write access, so no expensive work becomes reachable either way — but record the changed order in `docs/API.md`.
- Docs to update: `docs/API.md` (the two endpoints plus the middleware order), `docs/PACKAGES.md` (auth, ratelimit), `docs/FRONTEND.md` (the tokens card). `CLAUDE.md` stays untouched — no rule changes and no package is added or removed.