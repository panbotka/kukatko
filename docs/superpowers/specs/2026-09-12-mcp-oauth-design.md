# OAuth 2.1 over the MCP server — design and recommendation

**Date:** 2026-09-12
**Status:** proposed — awaiting a decision. **No code was written for this document.**
**Supersedes** the deferral recorded in
[`2026-07-09-api-tokens-and-ctl-design.md`](2026-07-09-api-tokens-and-ctl-design.md)
("no OAuth support at all"), which is now out of date on one important point (see §3).

## The question

> "so that people can just add it as an MCP server in the UI (Claude, ChatGPT)."

The tools are finished and good; authentication is the only thing standing between them and a
connector tile in someone's chat client. This document decides **whether** to build that and
**what exactly** gets built, so that the implementation tasks that follow are typing, not
deciding.

---

## 1. Where we stand today (measured 2026-09-12)

A throwaway instance with `KUKATKO_MCP_ENABLED=true`:

| Probe | Answer |
| --- | --- |
| `POST /api/v1/mcp` with no credential | `401 {"error":"authentication required"}` + `WWW-Authenticate: Bearer` |
| `/.well-known/oauth-protected-resource…` | `404` JSON |
| `/.well-known/oauth-authorization-server` | `404` JSON |
| `/register`, `/authorize`, `/token` | not routed → SPA |

The 401 challenge and the `/.well-known/` 404s landed in `8de8ca2` (the prerequisite task).
Before it, every discovery probe was answered with `200` and `index.html`, so a client parsed
a web page as JSON. That is fixed; the ground is now clean for metadata to be *added* rather
than fought for.

Authentication is a session cookie or `Authorization: Bearer kkt_…` (an API token owned by a
user, inheriting that user's role), enforced by `auth.API.RequireAuth`. MCP is **off in
production** — the VPS compose file sets no `KUKATKO_MCP_ENABLED`.

### What already works with no change whatsoever

This is the baseline every option below must beat.

```bash
# Claude Code
claude mcp add --transport http kukatko https://fotky.kotrzina.cz/api/v1/mcp \
  --header "Authorization: Bearer kkt_…"
```

```jsonc
// OpenAI Responses API — a bare access token in the tool object
{"type": "mcp", "server_url": "https://fotky.kotrzina.cz/api/v1/mcp",
 "authorization": "kkt_…"}
```

Both are real, supported paths — the Responses API documents `authorization` as "an OAuth
access token", but it never validates where the string came from, so a `kkt_` token works.
**Neither is a UI connector.** The claude.ai / ChatGPT connector tiles do not offer a "paste a
bearer token" field to an ordinary user.

---

## 2. What the two clients actually require

Verified against the vendors' own documentation on 2026-09-12 (sources in §14).

### claude.ai

Six authentication types exist. Only three are reachable without a conversation with
Anthropic:

| Type | What it needs from us | Verdict |
| --- | --- | --- |
| `oauth_cimd` | An authorization server advertising **both** `"client_id_metadata_document_supported": true` **and** `"none"` in `token_endpoint_auth_methods_supported` | **the target** |
| `oauth_dcr` | An RFC 7591 `registration_endpoint` | fallback only; deprecated in the MCP draft |
| `none` | An authless MCP server | never, for a family archive |
| `static_headers` | A fixed header entered once by an **org admin**; **beta**, header names other than `authorization`/`x-api-key` reviewed by Anthropic | see option C in §4 |
| `oauth_anthropic_creds` | Credentials mailed to `mcp-review@anthropic.com` | not for a self-hosted one-instance server |
| `custom_connection` | Each user creates their own OAuth client | worse than doing it properly |

Hard requirements once we are in the OAuth family: PKCE **S256** always
(`code_challenge_methods_supported` must list it); redirect URI
`https://claude.ai/api/mcp/auth_callback` for the hosted surfaces and an **RFC 8252 loopback
with a per-session ephemeral port** for Claude Code (`http://localhost/callback`,
`http://127.0.0.1/callback` — the port component must be ignored when matching); 10 s timeout
on discovery/registration/token and 30 s on refresh; `/token` must accept
`application/x-www-form-urlencoded` (a JSON-only parser returns 415 and the connection dies);
refresh tokens rotated for public clients, an invalid one answered `invalid_grant`;
Anthropic's egress is `160.79.104.0/21`; the PRM `resource` must match the URL **exactly as
the user typed it**; `WWW-Authenticate` is honoured only on a 401, never on a 200.

### ChatGPT

Developer mode offers **OAuth / No Authentication / Mixed**. It "prioritizes CIMD when it is
available", supports DCR "when configured", accepts a pre-registered client, and requires RFC
9728 protected-resource metadata plus RFC 8414 (or OIDC) authorization-server metadata, PKCE
S256, and the `resource` parameter echoed into the token audience. Its redirect URI is
`https://chatgpt.com/connector_platform_oauth_redirect` when we advertise
`authorization_response_iss_parameter_supported: true` (RFC 9207), otherwise a per-callback-id
URL we would have to read off the connector's management page.

**So one server design satisfies both:** an authorization server that speaks CIMD with public
clients, PKCE S256, and RFC 9207 issuer identification.

### The MCP specification itself

`MUST` for us as a resource server: implement RFC 9728 protected-resource metadata; include
`authorization_servers`; return 401 for an invalid or expired token; **validate that the token
was issued for us as the audience and reject every other token**. `SHOULD`: put `scope` in the
`WWW-Authenticate` challenge, and answer an in-scope-but-insufficient call with `403` +
`error="insufficient_scope"`. The spec explicitly allows the authorization server to be
"hosted with the resource server" — co-hosting is not a smell.

---

## 3. What changed since the 2026-07-09 deferral

That document recorded, correctly at the time, that the Go SDK "provides **no OAuth support at
all** — no RFC 9728 protected-resource metadata, no token verification, no registration
endpoint."

**That is no longer true.** `github.com/modelcontextprotocol/go-sdk v1.6.1`, already in
`go.mod`, ships:

- `auth.RequireBearerToken(verifier, opts)` — middleware that verifies a bearer token through
  a **callback we write** and, on failure, emits `WWW-Authenticate: Bearer
  resource_metadata="…", scope="…"`.
- `auth.ProtectedResourceMetadataHandler(*oauthex.ProtectedResourceMetadata)` — RFC 9728 with
  the CORS header discovery needs.
- `oauthex` — typed `ProtectedResourceMetadata`, `AuthServerMeta`, DCR request/response,
  `WWW-Authenticate` challenge parsing.

What it still does **not** ship is an authorization *server*: there is no `/authorize`, no
`/token`, no consent, no code or refresh-token storage. The client half
(`auth.AuthorizationCodeHandler`) is for an MCP *client*, not for us.

Two consequences, both good: the boilerplate half of "be a resource server" is a library call,
and `golang.org/x/oauth2` is already an indirect dependency. **Nothing in this design adds a
direct dependency or touches `CGO_ENABLED=0`.**

---

## 4. Decision 1 — who is the authorization server

### Option A — Kukátko is its own authorization server ✅ **recommended**

`/authorize` (a consent screen in the SPA), `/token`, discovery, CIMD, PKCE S256, opaque
tokens in Postgres, refresh rotation.

What makes this far smaller than it sounds:

- **The subject is already authenticated.** `/authorize` runs behind the existing session
  cookie. There is no user database to build, no password flow to write, no federation: the
  person consenting *is* a `users` row, with a role, possibly a passkey, already approved by
  an admin.
- **Opaque tokens, not JWTs.** Tokens are random secrets stored as SHA-256 hashes, exactly
  like `kkt_` tokens already are (`internal/auth/apitoken.go`). No signing keys, no JWKS
  endpoint, no key rotation, no clock-skew handling, no `alg` confusion. Audience validation
  becomes a string comparison against a column, and revocation becomes an `UPDATE`. This
  removes most of what makes "writing an OAuth server" the scary sentence it is.
- **No `/register`.** CIMD-only (§5) deletes the RFC 7591 endpoint, its input validation, its
  abuse surface and its client-management UI.
- **No new dependencies.** `crypto/rand`, `crypto/sha256`, `net/http`, `pgx` — all present.

What it costs: an `/authorize` + `/token` pair and a grant store are genuinely the most
security-sensitive code in the project, and they must be right the first time. §11 says what
the tests must pin.

### Option B — an external IdP in front (Authentik / Keycloak / Auth0)

Kukátko would only verify JWTs and the audience. Rejected, for four reasons in ascending order
of severity:

1. **There is no IdP anywhere on the VPS.** A search of `~/projects/vps` finds no Authentik,
   Keycloak, Authelia, oauth2-proxy or Dex — only Traefik basic-auth middlewares used
   elsewhere. This would be a brand-new operational component: its own database, its own
   upgrades, its own backup, its own Traefik router. The VPS has 8 vCPU and already runs
   `embeddings-text` with a 6 GB limit next to Traefik, three database engines and redis.
2. **CIMD almost certainly is not there.** Client ID Metadata Documents are a 2025/2026
   addition. An IdP that does not advertise `client_id_metadata_document_supported` pushes
   both Claude and ChatGPT down to DCR — which is deprecated in the MCP draft, discouraged by
   Anthropic for servers with real traffic, and means opening anonymous dynamic client
   registration on the family archive's IdP. We would take on the whole operational component
   *and still* end up on the worse registration mechanism.
3. **It splits the identity model in two.** Kukátko owns users, the strict role ladder,
   registration with an admin approval step, passkeys, the per-user favourites and ratings
   that are *keyed by `users.uid`*. An external IdP would either have to become the source of
   truth (a rewrite of `internal/auth` far larger than writing `/token`) or be provisioned
   from Kukátko and drift.
4. **It buys no safety.** The hard part of this feature is not "issue a token correctly", it
   is "decide who may connect an agent to a family archive and record what it did". None of
   that moves to the IdP.

### Option C — ask Anthropic for `static_headers`, write no code

`static_headers` makes today's `kkt_` token work in the claude.ai UI: an org admin enters
`Authorization: Bearer kkt_…` once. Zero code. It is listed here because it is the honest
cheapest path, and rejected as a *primary* plan because it is beta, needs an Anthropic
conversation, applies per organisation rather than per person (every member of the org shares
one identity — an audit trail that says "the org token did it" is not the audit trail this
project has), and does nothing at all for ChatGPT.

> **Worth doing in parallel, not instead:** it costs one e-mail and might unblock the
> claude.ai half of this weeks before any code ships.

### What Option A means for the `vps` repository and for `make check`

- `vps`: two new environment variables on the existing `kukatko` service in
  `roles/apps/files/apps/vps/docker-compose.yml` (`KUKATKO_MCP_ENABLED=true`,
  `KUKATKO_MCP_OAUTH_ENABLED=true`) and nothing else. **No new container, no new Traefik
  router, no new secret** — the token secrets are random values in our own database, not
  configuration. Anthropic's egress range `160.79.104.0/21` must *not* be turned into a
  Traefik IP allow-list: the `/authorize` leg arrives from the user's own browser, and ChatGPT
  publishes no equivalent range.
- `make check`: unchanged in shape. New Go packages bring unit tests (pure OAuth logic: PKCE
  verification, redirect matching, scope algebra, metadata documents) and integration tests
  against `KUKATKO_TEST_DATABASE_URL` (the whole grant → code → token → refresh → revoke path
  over real HTTP). New frontend route and page bring Vitest files. Nothing here needs CGO, a
  browser, or a network call in a test — the one outbound call (fetching a CIMD document) goes
  behind an interface with a fake, like every other external dependency in this project.

---

## 5. Decision 2 — client registration: CIMD only

**Advertise CIMD. Do not implement DCR.**

- Claude picks CIMD whenever the two metadata fields are present, and falls back to DCR only
  if they are not. ChatGPT "prioritizes CIMD when it is available".
- The MCP draft marks DCR **deprecated** ("New implementations should use Client ID Metadata
  Documents instead") and CIMD a `SHOULD`.
- DCR would mean an unauthenticated endpoint that writes a row to our database on every
  request, on a server whose entire legitimate client population is three URLs.

A CIMD client id is an `https` URL that serves its own metadata. We fetch it, and **must**
validate that the document's `client_id` equals the URL exactly, that it is JSON with
`client_id`, `client_name` and `redirect_uris`, and that the redirect URI in the authorization
request is one of those listed.

On top of the spec's rules, this design adds a **host allow-list**,
`mcp.oauth.allowed_client_hosts`, defaulting to `["claude.ai", "chatgpt.com"]`. A client id
URL on any other host is refused before it is fetched. This is the single cheapest piece of
hardening available: it turns "any party on the internet may become a client" into "the two
vendors we are doing this for", it makes SSRF through the CIMD fetch a non-issue, and it is
one config line for anyone who wants a third client. Claude Code's own CIMD
(`https://claude.ai/oauth/claude-code-client-metadata`) is on `claude.ai`, so the allow-list
covers all three surfaces.

Fetched documents are cached in Postgres with a short TTL so that a `/authorize` does not
block on a vendor's CDN and so that the consent screen can name the client while offline.

---

## 6. Decision 3 — identity mapping, and what happens to API tokens

**There is nothing to map.** `/authorize` is reached in a browser carrying the Kukátko session
cookie, so the OAuth subject *is* the signed-in user. The grant row stores `user_uid` — the
same foreign key `api_tokens` already stores. Every downstream consumer (audit actor, per-user
favourites, `person:me`, saved searches) keeps working unchanged because the principal
reaching the handlers is the same `auth.User` it is today.

Role is read **live at authentication time**, exactly as an API token's is; it is not frozen
into the token. Demoting a user to `viewer` takes effect on their agent's next call.

**API tokens stay, unchanged, side by side.** They are the CLI/`ctl` credential and the only
credential a script can mint non-interactively; OAuth is the UI-connector credential. They
meet in one place, `auth.API.authenticateRequest`, which learns a third credential shape and
distinguishes them by prefix (`kkt_` vs `kko_`). Two rules keep them from blurring:

- An **OAuth access token authenticates the MCP endpoint only.** It is issued for the resource
  `https://<host>/api/v1/mcp` and is refused everywhere else with a plain 401. An agent that
  slips its token into a `curl` against `/api/v1/admin/users` gets nothing. This is a
  deliberate containment boundary, not an oversight, and it is cheap: audience validation
  already has to happen, and this is that same check.
- An **API token is not accepted as an OAuth token** and vice versa; neither path ever
  consults the other's table.

In production the MCP payloads' `thumb_url` is a signed R2/CDN URL that carries its own
credential, so an agent can actually see thumbnails under this boundary. On a local-filesystem
instance `thumb_url` is an application route and an OAuth-token caller will be refused it —
acceptable, and worth one sentence in `docs/MCP.md` when the time comes.

---

## 7. Decision 4 — scopes

Two, and a rule.

| Scope | Meaning |
| --- | --- |
| `kukatko:read` | every read tool |
| `kukatko:write` | additionally the write tools |

**The rule: the effective permission is the narrower of the role and the scope.** A viewer who
grants `kukatko:write` still cannot write — the role wins, and `writerFromContext` stays
exactly as it is, with the scope check added beside it. An editor who connected a read-only
agent gets the read-only tool list. This is the same two-lock arrangement `internal/mcpapi`
already documents: the tool *registration* narrows by `role ∩ scope` so an agent never sees a
tool it cannot use, and each write handler *re-checks* both, because that is the boundary.

Where each list goes:

- `WWW-Authenticate` on the MCP 401 carries `scope="kukatko:read kukatko:write"` — the spec
  `SHOULD`s it, Claude honours it in preference to everything else, and it is one field.
- Protected-resource metadata `scopes_supported`: `["kukatko:read", "kukatko:write"]`.
- Authorization-server metadata `scopes_supported`: the same **plus `offline_access`**. The
  split is deliberate: the MCP draft says a resource `SHOULD NOT` advertise `offline_access`
  (a refresh token is not a property of the resource), while Claude only requests it when the
  *authorization server* lists it — and without a refresh token the connector breaks an hour
  after it is added.
- A 403 from an in-scope-but-insufficient call carries `error="insufficient_scope"` and the
  needed `scope`, which is what makes Claude offer re-authorisation instead of giving up.

---

## 8. Decision 5 — audience (RFC 8707)

The `resource` parameter is required on both the authorization and the token request, and both
clients send it. Handling:

1. `/authorize` and `/token` compare `resource` against this instance's canonical MCP URL and
   refuse a mismatch (`invalid_target`).
2. The canonical URL is **derived from the request's own host** when that host is one this
   instance answers on (`web.allowed_origins`), with `mcp.oauth.issuer` as an explicit
   override. This matters here specifically: production answers on **two** hostnames,
   `fotky.kotrzina.cz` and `kukatko.kotrzina.cz`, and the PRM `resource` must equal the URL
   the user typed *exactly*. A hard-coded single host would silently break whichever name the
   user chose.
3. The granted `resource` is stored on the token row.
4. The resource server compares the token's stored audience with its own canonical URL for the
   incoming request and answers a mismatch `401` with `error="invalid_token"`.

Because the tokens are opaque and minted by this same instance, cross-issuer confusion is
impossible by construction — but the check is written and tested anyway, because it is a spec
`MUST` and because it is what keeps the containment boundary of §6 honest.

---

## 9. Decision 6 — the security case, which is the actual decision

Everything above is mechanics. This is the part to think about before approving.

**What opening OAuth really changes:** today, connecting an agent to the family archive
requires someone to mint a `kkt_` token in the account page and paste it into a config file.
That friction is doing real work. After this, any account holder clicks "Connect" in claude.ai
and a third-party model provider begins reading the archive — faces, locations, the comments'
authors, children's photographs — at machine speed. The protocol risk is manageable; **this**
is the risk, and no amount of PKCE addresses it.

Hence the design is gated three times over:

1. **Instance-wide, off by default.** `mcp.enabled: false` already, and
   `mcp.oauth.enabled: false` on top. Neither is set in the VPS compose today.
2. **Per user, off by default.** A new
   `users.oauth_connect_allowed boolean NOT NULL DEFAULT false`, toggled by an admin in the
   existing user administration. Not derived from the role:
   a `viewer` is exactly the account one might want to hand a read-only agent, while an
   `admin` is not automatically someone whose browser should be able to mint an agent
   credential. (A column with a `DEFAULT` is safe for the ~35 packages that seed `users` in
   tests; a `CHECK` or a `NOT NULL` without a default would not be.) `/authorize` refuses with
   `access_denied` for a user without the flag, and says so in the UI.
3. **Per connection, by the person.** The consent screen names the client (`client_name` from
   its CIMD), the redirect host, and the scopes in plain Czech, and asks. Approve/deny.

And observable afterwards:

- **The audit trail keeps working and gets sharper.** Every mutation already writes an audit
  row in the mutation's transaction with `"via": "mcp"`. Under OAuth the actor is still the
  user's `uid` — the answer to "who did this" does not degrade. `details` gains `client_id`
  and the grant id, so "which agent, on whose behalf" is answerable. The grant and the
  revocation are themselves audited (`oauth.grant`, `oauth.revoke`), which is the record of
  *when a human attached an agent to the archive* — arguably the single most valuable row in
  the whole feature.
- **Connected apps are listed and revocable** on `AccountPage`, next to API tokens: client
  name, scopes, when it was granted, when it was last used, and a Revoke button that cascades
  to every token in the grant. An admin sees everyone's.
- **Rate limits.** `/token` and the authorization endpoints get a per-IP limiter
  (`ratelimit.oauth`, strict — these are unauthenticated or near-unauthenticated endpoints on
  the public internet), and the MCP endpoint gets a per-grant limiter so one runaway agent
  cannot saturate the worker or the mapy.com credits. `internal/ratelimit` and
  `internal/clientip` already provide both shapes.
- **Token lifetimes.** Access token 1 h. Refresh token 30 days, **rotated on every use**, with
  **reuse detection**: presenting a refresh token that has already been rotated revokes the
  entire grant, because the only explanation is that a copy leaked. Codes live 60 s, are
  single-use, and are bound to the client id, the redirect URI, the PKCE challenge and the
  resource.

Three more things this design refuses to do, on purpose:

- No `client_credentials` grant (and neither vendor supports one): every connection is
  consented to by a person.
- No token in a query string, ever — `bearer_methods_supported: ["header"]`.
- No widening of the MCP tool surface as part of this work. The "what is deliberately NOT
  exposed" list in `docs/MCP.md` is unchanged; making the door easier to open is not an
  argument for putting more behind it.

### One non-obvious trap, found while writing this

The session cookie is `SameSite=Strict`. A connector flow arrives as a **cross-site top-level
navigation** from `claude.ai`, and a Strict cookie is *not* sent on that request. A
server-rendered consent page would therefore see an anonymous request and bounce a signed-in
user to the login screen every single time. The SPA-route design in §10 sidesteps this for
free: the navigation only loads `index.html` (no cookie needed), and the SPA's subsequent
same-origin `fetch` to the API *does* carry the Strict cookie. **Do not move the consent
screen to a server-rendered page without changing the cookie policy**, and do not change the
cookie policy.

---

## 10. The shape in code

### Endpoints

| Route | Purpose | Guard |
| --- | --- | --- |
| `GET /.well-known/oauth-protected-resource/api/v1/mcp` | RFC 9728, path-insertion form (probed first) | anonymous, CORS `*` |
| `GET /.well-known/oauth-protected-resource` | RFC 9728, root form (probed second) | anonymous, CORS `*` |
| `GET /.well-known/oauth-authorization-server` | RFC 8414 metadata | anonymous, CORS `*` |
| `GET /oauth/authorize` | **frontend** route: the consent screen | browser |
| `GET /api/v1/oauth/authorization-request` | validates the parameters, describes the pending grant | `RequireAuth` (cookie) |
| `POST /api/v1/oauth/authorization-request` | approve or deny → returns the redirect URL with `code`/`error` and `iss` | `RequireAuth` (cookie) |
| `POST /api/v1/oauth/token` | `authorization_code` and `refresh_token`, **form-urlencoded** | anonymous (public client) |
| `GET`/`DELETE /api/v1/oauth/grants[/{id}]` | connected apps: list and revoke, own only | `RequireAuth` |

`authorization_endpoint` points at the **SPA** route `https://<host>/oauth/authorize`, which
the existing catch-all serves with `index.html`. Parameter validation errors are shown by the
SPA and are *not* redirected back to the client, as OAuth requires for an untrusted
`redirect_uri`.

### Documents

```jsonc
// /.well-known/oauth-authorization-server
{
  "issuer": "https://fotky.kotrzina.cz",
  "authorization_endpoint": "https://fotky.kotrzina.cz/oauth/authorize",
  "token_endpoint": "https://fotky.kotrzina.cz/api/v1/oauth/token",
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code", "refresh_token"],
  "code_challenge_methods_supported": ["S256"],
  "token_endpoint_auth_methods_supported": ["none"],
  "client_id_metadata_document_supported": true,
  "authorization_response_iss_parameter_supported": true,
  "scopes_supported": ["kukatko:read", "kukatko:write", "offline_access"]
  // deliberately NO "registration_endpoint" — see §5
}

// /.well-known/oauth-protected-resource/api/v1/mcp
{
  "resource": "https://fotky.kotrzina.cz/api/v1/mcp",
  "authorization_servers": ["https://fotky.kotrzina.cz"],
  "scopes_supported": ["kukatko:read", "kukatko:write"],
  "bearer_methods_supported": ["header"],
  "resource_name": "Kukátko"
}
```

### Packages

- **`internal/oauth`** (new) — the authorization server's domain: metadata documents, PKCE
  S256 verification, CIMD fetch + validation + cache, redirect-URI matching including the RFC
  8252 port-agnostic loopback rule, the scope model and the `role ∩ scope` algebra, the
  grant/code/token store, rotation and reuse detection. The pure half carries no I/O and is
  unit-tested; the CIMD fetch sits behind an interface with a fake, like every external
  dependency in this project.
- **`internal/oauthapi`** (new) — the HTTP surface above: the two authorization-request
  endpoints, `/token`, `/grants`, and the three discovery documents.
- **`internal/auth`** — `authenticateRequest` learns the `kko_` prefix; a `RequireMCPAuth`
  variant (or a challenge option on the existing guard) so that the MCP route's 401 carries
  `resource_metadata` and `scope` while the rest of the API keeps the bare `Bearer` the SPA
  expects. Plus the `oauth_connect_allowed` column and its admin endpoint.
- **`internal/mcpapi`** — `caller` gains the granted scopes; `getServer` narrows the tool list
  by `role ∩ scope`; `writerFromContext` checks both; audit `details` gains `client_id`.
- **`internal/server`** — the blanket `/.well-known/*` → 404 (added in `8de8ca2`) needs a
  registered exception: an option such as `WithWellKnown(path, handler)` consulted *before*
  the
  404. **Keep the 404 as the default** — it is what stops a future SPA from swallowing a
       document again.
- **`cmd/kukatko`** — `buildOAuthAPI`, wired into an existing `*APIOptions` helper rather than
  as a new `server.WithAPI` line (`buildServices` is at the `funlen` limit and a new line
  there fails lint).
- **`web/`** — `OAuthAuthorizePage` (the consent screen, cs/en) and a "Connected apps" section
  on `AccountPage` beside API tokens.

### Database

One migration, four tables: `oauth_clients` (the CIMD cache), `oauth_authorization_codes`,
`oauth_grants` (one per user × client × resource — this is the row a person sees and revokes)
and `oauth_tokens` (access and refresh in one table, `grant_id` `ON DELETE CASCADE`, so
rotation and reuse detection are a single code path). Every new table must be classified in
`internal/reset/tables.go` or the `internal/reset` integration test fails; all four are
library state and belong in the wiped set, while `users.oauth_connect_allowed` rides on the
preserved `users` row.

### Configuration

```yaml
mcp:
  enabled: false
  oauth:
    enabled: false                  # KUKATKO_MCP_OAUTH_ENABLED
    issuer: ""                      # empty = derive from the request host
    allowed_client_hosts: ["claude.ai", "chatgpt.com"]
    access_token_ttl: 1h
    refresh_token_ttl: 720h
ratelimit:
  oauth: { rate_per_sec: 1, burst: 10 }
```

Each key lands in the `Config` struct, `setDefaults`, `config.example.yaml`, the config tests
and `docs/OPERATIONS.md` in the same change, as the project requires.

---

## 11. What the tests must pin

Beyond "it works": a code that is replayed is rejected · a code with the wrong PKCE verifier
is rejected · a code bound to another client or another `redirect_uri` is rejected · an
expired code is rejected · a rotated refresh token, presented twice, revokes the whole grant ·
a token issued for a different `resource` is refused by the MCP endpoint with `invalid_token`
· a `kkt_` token is not accepted as an OAuth token and a `kko_` token is refused outside
`/api/v1/mcp` · a CIMD document whose `client_id` does not equal its URL is refused · a client
id on a host outside the allow-list is never fetched · a loopback redirect matches on any port
but `http://evil/callback` never does · a user without `oauth_connect_allowed` cannot complete
`/authorize` · a viewer who granted `kukatko:write` sees no write tool and is refused by every
write handler · the 401 carries `resource_metadata` **and** `scope` · the `/.well-known/`
documents are JSON and not the SPA · the grant and the revocation each write an audit row
(whose test must seed the acting user — the audit actor is a foreign key).

---

## 12. Recommendation, and how it splits into tasks

**Recommendation: build Option A**, and send the `static_headers` e-mail to Anthropic in
parallel as a cheap hedge. If the honest answer to "so that people can add it" turns out to be
*three relatives who all use Claude Code*, then **stop here**: write half a page in
`docs/MCP.md` about the `kkt_` + `claude mcp add` path and the Responses API, and this
document's cost was one afternoon of reading vendor documentation. That fork is the user's to
take, and it is the first question to answer before any of the tasks below are created.

If the answer is the connector tile, here is the split. Each task ends green on `make check`
and is independently reviewable.

| # | Task | Size |
| --- | --- | --- |
| 1 | `internal/oauth` foundations: metadata documents, PKCE, CIMD fetch + validation + allow-list, redirect matching, scope algebra. Plus the `WithWellKnown` exception in `internal/server` and the three discovery documents served behind `mcp.oauth.enabled`. Config keys. No tokens issued yet. | M |
| 2 | The grant: migration + store, `/authorize` validate/describe/approve/deny, `/token` for both grants, rotation and reuse detection, audit rows, `oauth_connect_allowed`. | **L — the core** |
| 3 | The resource server: `kko_` verification in `internal/auth`, audience + scope enforcement, the MCP 401/403 challenges, `role ∩ scope` in `internal/mcpapi`, rate limits. | M |
| 4 | Frontend: the consent screen, "Connected apps" on `AccountPage`, admin toggle, cs/en. | M |
| 5 | Rollout: `docs/MCP.md` + `docs/OPERATIONS.md` + `config.example.yaml` + `README.md`, the `vps` compose variables, and a walkthrough on the box staging stack that actually adds the connector in claude.ai and in ChatGPT before production is touched. | S, but do not skip |

Tasks 1–3 are strictly ordered. Task 4 can run beside task 3. Task 2 is the one to review
hardest; if it wants splitting, the seam is "codes and the authorization endpoint" versus
"tokens and refresh".

---

## 13. Open questions for the decision

1. **Is the audience really UI connectors**, or three people who would be equally served by a
   documentation page? (§12)
2. `oauth_connect_allowed` defaulting to **false for everyone including admins** — correct, or
   needlessly annoying for a household of four?
3. Is confining OAuth tokens to `/api/v1/mcp` (§6) the right boundary, or should a connector
   also be able to fetch originals from an instance that is not on R2?
4. 30-day refresh tokens: right for "add it once and forget", or should a connector have to be
   re-consented every, say, 90 days regardless?

---

## 14. Sources

All read 2026-09-12.

- MCP authorization, draft:
  <https://modelcontextprotocol.io/specification/draft/basic/authorization> (plus
  `/authorization-server-discovery`, `/client-registration`, `/security-considerations`)
- MCP authorization, 2025-11-25:
  <https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization>
- Claude connector authentication:
  <https://claude.com/docs/connectors/building/authentication>
- Claude lazy authentication:
  <https://claude.com/docs/connectors/building/lazy-authentication>
- Claude Code's CIMD document: <https://claude.ai/oauth/claude-code-client-metadata>
- ChatGPT developer mode: <https://developers.openai.com/api/docs/guides/developer-mode>
- OpenAI MCP guide: <https://developers.openai.com/api/docs/mcp>
- OpenAI connector authentication: <https://developers.openai.com/plugins/build/auth>
- Responses API MCP tool: <https://developers.openai.com/api/docs/guides/tools-connectors-mcp>
- Go SDK `auth` and `oauthex`: `github.com/modelcontextprotocol/go-sdk@v1.6.1` (already in
  `go.mod`)
