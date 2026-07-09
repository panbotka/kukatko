# API tokens and `kukatko ctl` — design

**Date:** 2026-07-09
**Status:** approved

## Problem

Kukátko should be operable from a terminal — and, through that terminal, by an AI agent —
against a running production instance over HTTP. Today it cannot be.

`authenticateRequest` reads the `kukatko_session` cookie and nothing else
(`internal/auth/middleware.go:89-99`). There is no `Authorization` header path, no API key,
no personal access token. The only non-cookie credential is a per-session `download_token`
accepted via `?t=` on exactly three media endpoints (`internal/auth/middleware.go:26-64`).

The CLI is entirely local: `serve`, `migrate`, `import`, `backup`, `restore`, `maintenance`,
and `version` all act directly on the database and filesystem. No HTTP client for Kukátko's
own API exists anywhere in the tree — the only outbound clients target PhotoPrism, mapy.com
and the embedding sidecar.

## Why a CLI rather than an MCP server

A CLI is cheaper in tokens than MCP: no tool schemas are loaded into the model's context on
every turn — just a short command and a narrow result. An MCP server remains desirable, but
is **deferred**, for a reason worth recording.

Anthropic's connector documentation states that a pure machine-to-machine `client_credentials`
grant is not supported and that every connection requires user consent. The auth types
available out of the box are OAuth with Dynamic Client Registration or a Client ID Metadata
Document; a fixed bearer token (`static_headers`) is in beta and granted on request. The
official Go MCP SDK implements the protocol and the Streamable HTTP transport but provides
**no OAuth support at all** — no RFC 9728 protected-resource metadata, no token verification,
no registration endpoint.

Choosing DCR would therefore make Kukátko a full OAuth authorization server: RFC 8414
discovery, RFC 9728 protected-resource metadata, RFC 7591 dynamic registration, PKCE S256,
a form-urlencoded token endpoint, and refresh-token rotation for public clients. That is the
most security-sensitive code in the project, and Anthropic's own docs discourage DCR for
servers with real traffic. It also cannot work until Kukátko runs on a public HTTPS domain:
Anthropic reaches connectors from `160.79.104.0/21`, so a Tailscale-only instance is
unreachable. MCP is revisited once Kukátko is on the VPS.

## Phase 1 — API tokens

### Credential

Format `kkt_<id>_<secret>`, where `secret` carries 256 bits of entropy. Only a hash of the
secret is stored. The `<id>` prefix indexes the row, so verification is a single lookup
rather than a table scan.

**Hash with SHA-256, not bcrypt.** Passwords use bcrypt today, and copying that here would be
a mistake: bcrypt is deliberately slow, and a token is verified on *every* API request, not
once per login. Bcrypt's cost exists to defend low-entropy secrets against dictionary attack;
a 256-bit random secret has no dictionary. A fast hash is both correct and necessary.

### Model

A token belongs to a user and **inherits that user's role**. No second permission system:
`RequireAuth` / `RequireWrite` / `RequireAdmin` keep working untouched
(`internal/auth/middleware.go:66-84`).

Each token carries a human-readable name, `expires_at`, `last_used_at` and `revoked_at`.
`last_used_at` is updated at most once a minute, mirroring the existing sliding-session guard
(`internal/auth/models.go:60-78`), so a busy client does not write on every request.

### Surface

`POST /api/v1/auth/tokens` creates a token and returns the secret **once**; `GET` lists the
caller's tokens without secrets; `DELETE /api/v1/auth/tokens/{id}` revokes one. Every mutation
writes to `internal/audit` in the same transaction, per the project rule.

Authentication gains one branch: `Authorization: Bearer <token>` is accepted alongside the
cookie. The cookie path is unchanged, so the frontend is untouched. A revoked or expired token
is a 401, never a 403.

## Phase 2 — `kukatko ctl`

One binary, as requested. `kukatko ctl photos list --year 2024 -o json` speaks HTTP to a
remote instance. When the binary is invoked through a symlink named `kukatkoctl`, the `ctl`
subcommand is implied, so `kukatkoctl photos list` works.

### Client configuration

`~/.config/kukatko/ctl.yaml` holds named contexts in the style of `kubectl`: each has a server
URL and a token, with one marked current. `KUKATKO_SERVER` and `KUKATKO_TOKEN` override the
active context. This is a client-side file and has nothing to do with `internal/config`, which
is purely server-side and has no notion of a remote endpoint.

### Output

A compact table by default; `-o json` for machine consumption. Compactness is the entire
point — it is what makes this cheaper than MCP.

### Coverage

Not all sixty endpoints. Only what is worth driving from a terminal or an agent: `photos`
(list, get, search), `albums`, `labels`, `subjects`, `favorites`, `rating`, and `bulk`.
Backup, restore, migration and maintenance stay local commands; they are not worth exposing
over the network.

## Two complications, recorded deliberately

**The API has no uniform envelope.** `photos` returns `{photos, total, limit, offset,
next_offset}` (`internal/photoapi/http.go:175-181`), while `albums` returns a bare
`{albums: [...]}` and `labels` a bare `{labels: [...]}` with no pagination at all
(`internal/organizeapi/albums.go:12-13`, `internal/organizeapi/labels.go:12-13`). A generic
list decoder is therefore impossible; each resource needs its own. Normalising the API is
explicitly **out of scope** — it would break the frontend for no benefit to this work.

**Rate limiting is keyed by client IP** and guards only four endpoints: upload, bulk edit,
the two import triggers, and map tiles (`internal/ratelimit/ratelimit.go:144-174`). Ordinary
reads are unlimited, so `ctl` will not hit a limiter unless it drives uploads or bulk edits.

## Testing

Unit tests for token generation, hashing, and the expiry and revocation predicates.
Integration tests against the real test database for the token endpoints and for the
`Authorization: Bearer` branch, including the revoked and expired paths. For `ctl`, tests
against an `httptest` server covering context resolution, the environment overrides, both
output formats, and a 401 producing a useful error rather than a stack trace.

## Out of scope

- An MCP server (deferred; see above).
- An OpenAPI document. It would help generate the CLI mechanically, but the route tree does
  not expose role requirements or request/response schemas, so it is its own project.
- Any change to the existing cookie-session flow.
