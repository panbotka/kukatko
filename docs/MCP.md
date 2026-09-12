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
| **Path** | `/api/v1/mcp` — a client only ever POSTs to it |
| **Transport** | Streamable HTTP, **stateless**, `application/json` response (not SSE) |
| **Guard** | `RequireAuth` (the same auth as the rest of `/api/v1`) |
| **Default** | **disabled** — `mcp.enabled: false` |
| **Library** | [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) — pure Go, keeps `CGO_ENABLED=0` |

The path is `/api/v1/mcp` because **all** of Kukátko's routes live under `/api/v1` (`server.WithAPI`
mounts into a single subrouter). It has no top-level `/mcp` of its own.

The route is registered with `chi.Handle`, i.e. for **every** method, and the SDK's handler decides what
each one means: `POST` is the protocol, a `GET` (which in a session-ful server would open the SSE stream)
is answered **405** because this one is stateless, and a `DELETE` carrying a session header is answered
**204**. There is nothing to gain from restricting the route to POST in chi — it would only move the same
refusal one layer out and turn 405 into a different 405.

**Stateless** means every POST stands on its own: no server-side state that could be hijacked or that
could expire. That keeps the endpoint an ordinary authenticated route, and the request context (principal
+ audit metadata) reaches all the way down into the tool handlers. The SDK still *emits* an
`Mcp-Session-Id` on `initialize` (`Mcp-Session-Id: YN2FLRSQHIBBJ4AYL2XBJMHS5H` and the like) — it simply
never validates one on the way back in, so a client that echoes it, drops it or invents one is treated
identically. Do not read the header's presence as session state; there is none.

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

### The 401 and why there is no OAuth discovery

An unauthenticated call is answered **401 with `WWW-Authenticate: Bearer`** (`auth.Challenge`, set by
`writeUnauthorized`, which every 401 the RBAC guards write goes through). That header is the first thing an MCP client reads.
Anthropic's connector documentation says that when it is **missing**, Claude falls back to guessing the
server's OAuth metadata: it probes `/.well-known/oauth-protected-resource/<path>` and then
`/.well-known/oauth-protected-resource`, and expects a **404** to mean "this server has no OAuth". Until
September 2026 Kukátko answered those probes with **200 and `index.html`** — the SPA catch-all swallowed
them — so the client parsed a web page as JSON and failed incomprehensibly instead of concluding that
there is nothing to discover. `internal/server` now answers everything under `/.well-known/` with a JSON
404 (it publishes nothing there: ACME is terminated by the reverse proxy, the PWA manifest lives at
`/manifest.webmanifest`).

The scheme is `Bearer` and deliberately not `Basic`: `Basic` would make a browser pop its own native
sign-in dialog over the SPA, which handles its 401s itself. **Kukátko implements no OAuth 2.1, no RFC 9728
protected-resource metadata and no dynamic client registration** — a `kkt_` API token is the whole story.
When that changes, `resource_metadata="…"` and `scope="…"` get appended to `auth.Challenge`.

Sources: [Anthropic — connector authentication](https://claude.com/docs/connectors/building/authentication) ·
[MCP — authorization](https://modelcontextprotocol.io/specification/draft/basic/authorization).

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

- Lists return `photoSummary`: `uid`, `title`, `taken_at`, `media_type`, `duration_ms` (videos only),
  `thumb_url`. Nothing more.
- The detail (`get_photo`) returns a curated selection of columns — **never the `exif` blob, from any tool**.
- Everything paginates: `total`, `offset` and **`remaining`** (how many are still left).
- Page sizes are held by `mcp.page_size` / `mcp.max_page_size`.

### Video

A clip is not a photograph with a duration, and the payloads say so. The **summary** carries `duration_ms`,
which is the one video fact worth a list row: an agent asked "how long are these clips" would otherwise have
to call `get_photo` once per result, which is the exact shape this package exists to avoid. The **detail**
adds four more columns and one derived field:

| Field | Meaning |
| --- | --- |
| `fps` | the clip's average frame rate |
| `video_codec` / `audio_codec` | the container's primary streams (`h264`, `aac`) |
| `has_audio` | whether the clip carries sound |
| `encode_state` | where the streaming encode stands: `done`, `queued`, `running`, `failed`, `pending`, `skipped` |

`has_audio` is a **tri-state**: present and `false` on a video that was asked and has no audio stream,
**absent** on a still, where "does it have sound" is not a question with a false answer. The same rule puts
every video field off a still's payload entirely, rather than sending zeroes an agent could report as facts.

`encode_state` is the `hls_transcode` step of the per-photo processing report, and it is what answers **"which
videos still need preparing"** — a clip with no encode cannot be played in a browser at all. `pending` means
nothing has ever been scheduled for it, `skipped` means it never will be here (not a standalone video, or
streaming switched off instance-wide), and `failed` means the last attempt errored. It is read **only for a
standalone video**: the report costs two queries, and for anything else the answer would be `skipped` repeated
on every still in the library. A report that cannot be read costs the field, never the record — `get_photo`
still answers with the photo.

## Tools

### Reading (available to every token)

| Tool | What it does |
| --- | --- |
| `search_photos` | The main entry point. Free text + the **search language** (`person:babicka year:1960-1969 -album:dovolena`), plus exact scoping via `album_uid` / `label_uid` / `person_uid`, `sort`, `order`, `limit`, `offset`. Returns a compact page + `total` + `remaining`. `uid:` names one photo by its own id or the source id it was imported under, and reaches it even when archived, hidden or a stack variant. |
| `get_photo` | A single photo in detail: texts, date, location, exposure, the caller's favorite/rating + **the albums, labels and people** it carries. For a video also its frame rate, codecs, whether it has sound, and the state of its streaming encode. |
| `find_similar_photos` | Visually similar photos (kNN over embeddings), with the distance. If embeddings are missing it says so. |
| `list_albums`, `list_labels`, `list_subjects` | Catalogs with counts, optionally filtered by `name`. Used to turn **a name a human said into the `uid`** the other tools want. A subject carries both `face_count` (recognised faces) and `photo_count` (photos the person appears on) — one photo can hold several of their faces, so they are not the same number. |
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

### Titles and annotations

Every tool carries a **`title`** (`Search photos`, `Add photos to album`) as well as its `name`: a client
that has none renders the raw `snake_case` identifier in front of a human. They are English and are *not*
translated — MCP is server-side and the server has no idea what language the agent's human speaks, so
there is no i18n hook to hang them on.

The annotations (built by the four constructors in `annotations.go`) say only what is true:

| Hint | Where | Why |
| --- | --- | --- |
| `readOnlyHint: true` | the ten read tools | so a client need not confirm them |
| `destructiveHint: false` | `create_album`, `create_label` | they only add a row. MCP **defaults this to `true`**, and a client that believes the default asks a human to approve "create an empty album" |
| `idempotentHint: true` | `add_/remove_photos_from_album`, `attach_/detach_label`, `set_photo_metadata`, `set_photo_rating` | the second identical call leaves the library as the first did |
| `openWorldHint: false` | **all nineteen** | MCP defaults it to `true`, i.e. "may reach an unpredictable external system". None of these do: they touch this instance's database and nothing else |

`bulk_edit_photos` gets neither `destructiveHint: false` nor `idempotentHint`: it removes albums and
labels as readily as it adds them, and a repeated run applies its changes to whatever the photos look
like by then.

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
- **No starting an encode.** The detail *reports* `encode_state`, so an agent can answer "which videos still
  need preparing" and hand that list to a human — but there is no tool that schedules `hls_transcode`, for one
  clip or for the library. `POST /process/hls` is a process backfill, and those are withheld by the line
  above; the fact that reporting the state makes the gap visible is not an argument for closing it. A
  streaming encode is the most expensive job this application runs and drains the worker one clip at a time,
  so an agent that could start one over a library could occupy the instance for a day with a single tool call
  nobody watched. The person holding a token can: `kukatkoctl process hls`
  ([`OPERATIONS.md`](OPERATIONS.md) → *`ctl process`*), which is the same distinction the whole section rests
  on. The same goes for the scrub preview: `ctl photos rebuild storyboard` exists, no tool does.
- **No setting the location.** A coordinate an agent made up is, once written, indistinguishable from
  a measured one; for estimating a location the library has its own, honestly-labeled path (`internal/geoestimate`).
  That is why `bulk_edit_photos` leaves out `Location` / `ClearLocation` too.
- **No writing, editing or deleting comments** (`internal/comments`, see [`API.md`](API.md)). A comment is
  one person speaking to their family in their own name; an agent posting into that thread would put words
  in a human's mouth, and the audit trail would record a person as their author. Reading is not exposed
  either — a thread is the most personal text in the library and nothing an agent does needs it.

The `TestMCPDestructiveToolsAreNotExposed` test guards this **by intent, not by a list of names**: it takes
the highest role, walks `tools/list` and fails on any tool whose name contains `delete`, `purge`,
`trash`, `archive`, `restore`, `backup`, `user` or `empty`.

**`kukatko ctl` does offer several of these, and that is not an inconsistency.** Uploading, hiding,
archiving, purging, emptying the trash and merging a duplicate group all live in the CLI, the
irreversible ones behind an explicit `--yes` with a `--dry-run` that lists what would be lost (see
[`OPERATIONS.md`](OPERATIONS.md) → *The irreversible commands and their gate*). The difference is who
is on the other end: the CLI is the door for a **person holding a token**, who is asked to confirm and
can be shown what is at stake first; MCP is the door for an **agent running unattended**, where there
is nobody to ask. Adding any of it here would erase that distinction, which is the reason both doors
exist.

## Enabling it and connecting an agent

See [`docs/OPERATIONS.md`](OPERATIONS.md) → the `mcp.*` keys. In brief:

```yaml
mcp:
  enabled: true
```

Disabled = **no MCP server is built** and the path answers a bare **404** (`handleDisabled`). Not a 403 — a
403 would still tell an attacker that the endpoint is there. The 404 is *mounted* rather than left to the
router on purpose: without it `/api/v1/mcp` fell into the **SPA catch-all** (`server.routes()` has
`router.NotFound(web.Handler())`) like any other unknown path and returned **`200` and `index.html`**, so
the client got HTML where it expected JSON-RPC and failed for the wrong reason. A client can now tell
"this server does not have that" from "this server did not answer".

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

The same token is what the **OpenAI Responses API** wants in its `mcp` tool's `authorization` field. What
neither path gives you is a **connector tile in the claude.ai or ChatGPT UI**: those ask the server for OAuth,
which this one does not have — the 401 says `WWW-Authenticate: Bearer` and every `/.well-known/` probe is a
404. What it would take, whether it is worth it, and how it would be split up is worked out in
[`docs/superpowers/specs/2026-09-12-mcp-oauth-design.md`](superpowers/specs/2026-09-12-mcp-oauth-design.md).
Nothing is decided yet; until it is, a token is the way in.

## Tests

`internal/mcpapi/mcpapi_integration_test.go` (tag `integration`) runs over the **real MCP transport**,
real auth middleware and real `kkt_` tokens against `KUKATKO_TEST_DATABASE_URL`. It covers:
a disabled server answers 404 and not 403 (and not HTML) · the endpoint requires auth · an unauthenticated call
carries the `Bearer` challenge · every tool has a title, states `openWorldHint: false`, and no write tool claims
`readOnlyHint` · the `initialize` handshake ·
a viewer sees only the read tools · a viewer is rejected on **every** write tool and nothing changed ·
destructive tools are not exposed · search returns the compact shape without EXIF and with pagination ·
the search language works · a write token creates an album and attaches a label · **every mutation writes an audit
row** · a partial edit does not null out the other fields · bulk is atomic · the tool descriptions are written.

`internal/server/server_test.go` pins the other half of the discovery fix without a database: every
`/.well-known/…` probe is 404 and not `text/html`, while an ordinary client-side route still reaches the SPA.

Unit tests (`mcpapi_test.go`, run in `make check` without a DB) hold the pure helpers, the RBAC check and that
`exif` does not leak into any payload. `shape_test.go` pins the payload shapes against the three rows they
have to tell apart — a still, a video with sound and a video without — including that every video-only key is
**absent** from a still rather than present and empty, that a silent clip states its silence instead of
leaving it to be inferred from a missing audio codec, and that `encode_state` is never asked about anything
but a standalone video.
