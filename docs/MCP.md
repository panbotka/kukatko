# MCP server — the library for an AI agent

Kukátko can expose its library as an **MCP server** (Model Context Protocol) so that an AI agent works
with it directly: it searches, reads, organizes, answers questions. The motivation is no gimmick — this
is genuinely how the library gets maintained: *"find all of grandma's photos from the sixties and drop
them into an album"* is an everyday workflow, not a demo.

Implementation: [`internal/mcpapi`](../internal/mcpapi), wiring `buildMCPAPI` in `cmd/kukatko/mcp.go`.

---

## Endpoint and transport

| | |
| --- | --- |
| **Path** | `POST /api/v1/mcp` |
| **Transport** | Streamable HTTP, **stateless**, `application/json` response (not SSE) |
| **Guard** | `RequireAuth` (the same auth as the rest of `/api/v1`) |
| **Default** | **disabled** — `mcp.enabled: false` |
| **Library** | [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) — pure Go, keeps `CGO_ENABLED=0` |

The path is `/api/v1/mcp` because **all** of Kukátko's routes live under `/api/v1` (`server.WithAPI`
mounts into a single subrouter). It has no top-level `/mcp` of its own.

**Stateless** means every POST stands on its own: no `Mcp-Session-Id`, no server-side state that could
be hijacked or that could expire. That keeps the endpoint an ordinary authenticated route, and the
request context (principal + audit metadata) reaches all the way down into the tool handlers.

The SDK's DNS-rebinding guard is **off** (`DisableLocalhostProtection`): it rejects a request that comes
in over loopback with a non-loopback `Host` header, which is exactly what a reverse proxy in front of
Kukátko does. The guard protects *unauthenticated* local servers; this endpoint requires a valid
principal, so it would only break a real deployment.

## Auth model — no new mechanism

The endpoint **adds no auth of its own and no bypass**. It sits behind the same `RequireAuth` and the
same RBAC as every other route:

- The agent authenticates with an **API token** (`Authorization: Bearer kkt_…`), see `internal/auth`.
- **The role belongs to the user, not the token** — a token inherits its owner's role at the moment of
  authentication.
- The role decides write access via `Role.CanWrite()`: `viewer` → read-only; `editor`, `admin` and
  **`maintainer`** → read and write. A purely writing agent only needs `editor` (the lowest role with
  write access); the former `ai` role was removed (migration `0036`), and its successor at the top of
  the ladder is `maintainer`.

The boundary is **doubled**, on purpose:

1. **Write tools are never even registered for a read-only caller.** The server is built twice
   (read-only and write) and `getServer` picks based on the request's principal. A viewer will not
   so much as see them in `tools/list` — an agent has no business looking at tools it must not use.
2. **Every write handler re-checks the role** (`writerFromContext`). That is the security boundary;
   point 1 is UX. A boundary that lives in a single place falls apart the moment someone edits the other.

## Audit

Every **mutation** passes through `internal/audit` **in the same transaction** as the change itself —
exactly as it does for a human. That is the whole reason the audit trail exists. On top of that,
`"via": "mcp"` is stamped into `details`: *who* did it and *through which door* are two different
questions, and once an agent is turned loose on the library the second one is the more interesting.

`set_photo_rating` deliberately goes through `internal/bulk` (and not the rating store directly), because
bulk writes its own audit row inside the transaction — the HTTP rating endpoint writes no audit entry,
but an agent's rating should be traceable like every other change it makes.

## Response shape — the agent's context is the scarce resource

`photos.Photo` has ~60 fields, including the **raw `exif` JSONB blob**. A search that returns 50 such
objects is unusable. Hence:

- Lists return `photoSummary`: `uid`, `title`, `taken_at`, `media_type`, `thumb_url`. Nothing more.
- The detail (`get_photo`) returns a curated selection of columns — **never the `exif` blob, from any tool**.
- Everything paginates: `total`, `offset` and **`remaining`** (how many are still left).
- Page sizes are held by `mcp.page_size` / `mcp.max_page_size`.

## Tools

### Reading (available to every token)

| Tool | What it does |
| --- | --- |
| `search_photos` | The main entry point. Free text + the **search language** (`person:babicka year:1960-1969 -album:dovolena`), plus exact scoping via `album_uid` / `label_uid` / `person_uid`, `sort`, `order`, `limit`, `offset`. Returns a compact page + `total` + `remaining`. |
| `get_photo` | A single photo in detail: texts, date, location, exposure, the caller's favorite/rating + **the albums, labels and people** it carries. |
| `find_similar_photos` | Visually similar photos (kNN over embeddings), with the distance. If embeddings are missing it says so. |
| `list_albums`, `list_labels`, `list_subjects` | Catalogs with counts, optionally filtered by `name`. Used to turn **a name a human said into the `uid`** the other tools want. |
| `get_album`, `get_label`, `get_subject` | A single record by `uid` **or `slug`**. |
| `library_stats` | Counts in a single call: photos, of which videos, archived, with GPS, the caller's favorites, albums, labels, people. The answer to "how many…" without pagination. |

**An album's / label's / person's photos** are read through `search_photos` with `album_uid` / `label_uid` /
`person_uid` — it is the same list path, so all the other filters, sorting and pagination apply too.
There are deliberately no separate tools for it.

### Writing (write-capable tokens only)

| Tool | What it does |
| --- | --- |
| `create_album` | Creates an empty album, returns its `uid`. |
| `add_photos_to_album`, `remove_photos_from_album` | A batch in a single transaction; adding is idempotent. |
| `create_label` | Creates a label, returns its `uid`. |
| `attach_label`, `detach_label` | A label on a single photo (`SourceManual`, uncertainty 0). |
| `set_photo_metadata` | Title / description / notes. **Pointer semantics:** an omitted field is left unchanged, an empty string clears it. Internally read-modify-write, because the store does a full-record replace. |
| `set_photo_rating` | Favorite / 0–5 stars / flag. **Per-user** — the opinion of the token's owner, not a fact about the library. |
| `bulk_edit_photos` | One set of changes applied to many photos **in a single transaction**. The preferred tool for batches — an agent that calls the single-photo tools in a loop is slow and can end up applying a change only halfway. |

## What is deliberately NOT exposed

**Nothing destructive or irreversible.** This is not a gap in the tool list for someone to "fill in"
later — it is a decision about what an autonomous agent may do to someone else's family photos:

- **No deleting a photo.** No purge, no emptying the trash, no retention.
- **No archiving.** Archiving is the path into the trash, and the trash is **purged by retention** —
  an agent that can archive can, with a little patience, delete. That is why `bulk_edit_photos`
  leaves out `Archive` too, which the bulk service otherwise supports.
- **No restore and no backup.**
- **No user or token management.**
- **No admin surface** — jobs, maintenance, process backfills, import.
- **No setting the location.** A coordinate an agent made up is, once written, indistinguishable from
  a measured one; for estimating a location the library has its own, honestly-labeled path (`internal/geoestimate`).
  That is why `bulk_edit_photos` leaves out `Location` / `ClearLocation` too.

The `TestMCPDestructiveToolsAreNotExposed` test guards this **by intent, not by a list of names**: it takes
the highest role, walks `tools/list` and fails on any tool whose name contains `delete`, `purge`,
`trash`, `archive`, `restore`, `backup`, `user` or `empty`.

## Enabling it and connecting an agent

See [`docs/OPERATIONS.md`](OPERATIONS.md) → the `mcp.*` keys. In brief:

```yaml
mcp:
  enabled: true
```

Disabled = **the route is not mounted at all** (`RegisterRoutes` registers nothing). Not that it exists and
returns 403 — a 403 would still tell an attacker that the endpoint is there.

**Careful when verifying by hand with curl:** in the full binary `/api/v1/mcp` then falls into the **SPA catch-all**
(`server.routes()` has `router.NotFound(web.Handler())`) like any other unknown path, so it returns
**`200` and `index.html`**, not 404 — the MCP client gets HTML, does not parse it and never opens the connection. That the route
truly does not exist is visible in the access log: the `"route":"/api/v1/mcp"` field is missing. The tests see `404`,
because their router has no SPA fallback; that is the clean signal that "there is nothing on the router".

A token for the agent (the lowest write-capable role is `editor`; `admin`/`maintainer` write too). A token is minted
**by a user for themselves** — `POST /auth/tokens` always issues it to the calling principal, an admin cannot create
one on someone else's behalf. So two steps:

```bash
# 1) an admin creates a user with the editor role
curl -X POST https://<host>/api/v1/admin/users \
  -b admin-session.txt -H 'Content-Type: application/json' \
  -d '{"username":"agent","password":"…","role":"editor"}'

# 2) that user logs in and mints a token for themselves
curl -X POST https://<host>/api/v1/auth/login -c agent.txt \
  -H 'Content-Type: application/json' -d '{"username":"agent","password":"…"}'
curl -X POST https://<host>/api/v1/auth/tokens -b agent.txt \
  -H 'Content-Type: application/json' -d '{"name":"claude"}'
```

The response carries `secret` — **the plaintext `kkt_…` is shown only once**, save it right away. For a read-only agent
create a user with the `viewer` role; a token inherits its owner's role, it has none of its own.

Connecting a client (Claude Code):

```bash
claude mcp add --transport http kukatko https://<host>/api/v1/mcp \
  --header "Authorization: Bearer kkt_…"
```

## Tests

`internal/mcpapi/mcpapi_integration_test.go` (tag `integration`) runs over the **real MCP transport**,
real auth middleware and real `kkt_` tokens against `KUKATKO_TEST_DATABASE_URL`. It covers:
a disabled server does not mount the route (404, not 403) · the endpoint requires auth · the `initialize` handshake ·
a viewer sees only the read tools · a viewer is rejected on **every** write tool and nothing changed ·
destructive tools are not exposed · search returns the compact shape without EXIF and with pagination ·
the search language works · a write token creates an album and attaches a label · **every mutation writes an audit
row** · a partial edit does not null out the other fields · bulk is atomic · the tool descriptions are written.

Unit tests (`mcpapi_test.go`, run in `make check` without a DB) hold the pure helpers, the RBAC check and that
`exif` does not leak into any payload.
