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

## Tooly

### Čtení (dostane každý token)

| Tool | Co dělá |
| --- | --- |
| `search_photos` | Hlavní vstupní bod. Volný text + **vyhledávací jazyk** (`person:babicka year:1960-1969 -album:dovolena`), plus přesné scope `album_uid` / `label_uid` / `person_uid`, `sort`, `order`, `limit`, `offset`. Vrací kompaktní stránku + `total` + `remaining`. |
| `get_photo` | Jedna fotka v detailu: texty, datum, poloha, expozice, favorite/rating volajícího + **alba, štítky a lidé**, které nese. |
| `find_similar_photos` | Vizuálně podobné fotky (kNN nad embeddingy) i se vzdáleností. Bez embeddingů to řekne. |
| `list_albums`, `list_labels`, `list_subjects` | Katalogy s počty, volitelně filtr `name`. Slouží k převodu **jména, které řekl člověk, na `uid`**, které chtějí ostatní tooly. |
| `get_album`, `get_label`, `get_subject` | Jeden záznam podle `uid` **nebo `slug`**. |
| `library_stats` | Počty v jednom volání: fotky, z toho videa, archivované, s GPS, oblíbené volajícího, alba, štítky, lidé. Odpověď na „kolik…" bez stránkování. |

**Fotky alba / štítku / osoby** se čtou přes `search_photos` s `album_uid` / `label_uid` /
`person_uid` — je to ten samý list path, takže platí i všechny ostatní filtry, řazení a stránkování.
Samostatné tooly na to schválně nejsou.

### Zápis (jen tokeny s právem zápisu)

| Tool | Co dělá |
| --- | --- |
| `create_album` | Založí prázdné album, vrátí `uid`. |
| `add_photos_to_album`, `remove_photos_from_album` | Dávka v jedné transakci; přidání je idempotentní. |
| `create_label` | Založí štítek, vrátí `uid`. |
| `attach_label`, `detach_label` | Štítek na jednu fotku (`SourceManual`, uncertainty 0). |
| `set_photo_metadata` | Název / popis / poznámky. **Pointerová sémantika:** vynechané pole se nemění, prázdný řetězec maže. Uvnitř read-modify-write, protože store dělá full-record replace. |
| `set_photo_rating` | Favorite / hvězdy 0–5 / flag. **Per-user** — názor vlastníka tokenu, ne fakt o knihovně. |
| `bulk_edit_photos` | Jedna sada změn na mnoho fotek **v jedné transakci**. Preferovaný nástroj pro dávky — agent, který volá single-photo tooly ve smyčce, je pomalý a umí změnu aplikovat z poloviny. |

## Co záměrně vystavené NENÍ

**Nic destruktivního ani nevratného.** Tohle není mezera v seznamu toolů, kterou má někdo příště
„doplnit" — je to rozhodnutí o tom, co smí autonomní agent udělat s cizími rodinnými fotkami:

- **Žádné mazání fotky.** Ani purge, ani vysypání koše, ani retention.
- **Žádné archivování.** Archivace je cesta do koše a koš se **purguje podle retention** —
  agent, který umí archivovat, umí s trochou trpělivosti mazat. Proto `bulk_edit_photos`
  vynechává i `Archive`, který bulk service jinak umí.
- **Žádný restore ani backup.**
- **Žádná správa uživatelů ani tokenů.**
- **Žádný admin povrch** — jobs, maintenance, process backfilly, import.
- **Žádné nastavování polohy.** Souřadnice, kterou si agent vymyslel, je po zápisu k nerozeznání od
  změřené; na odhad polohy má knihovna vlastní, poctivě označkovanou cestu (`internal/geoestimate`).
  Proto `bulk_edit_photos` vynechává i `Location` / `ClearLocation`.

Test `TestMCPDestructiveToolsAreNotExposed` to hlídá **podle záměru, ne podle seznamu jmen**: sáhne po
nejvyšší roli, projde `tools/list` a spadne na jakémkoli toolu, jehož jméno obsahuje `delete`, `purge`,
`trash`, `archive`, `restore`, `backup`, `user` nebo `empty`.

## Zapnutí a připojení agenta

Viz [`docs/OPERATIONS.md`](OPERATIONS.md) → `mcp.*` klíče. Stručně:

```yaml
mcp:
  enabled: true
```

Vypnuto = **route se vůbec nemountuje** (`RegisterRoutes` neregistruje nic). Ne že by existovala a
vracela 403 — 403 by útočníkovi pořád prozradilo, že tam endpoint je.

**Pozor při ručním ověřování curlem:** v celém binárce pak `/api/v1/mcp` spadne do **SPA catch-all**
(`server.routes()` má `router.NotFound(web.Handler())`) jako každá jiná neznámá cesta, takže vrátí
**`200` a `index.html`**, ne 404 — MCP klient dostane HTML, neparsuje ho a spojení neustaví. Že route
opravdu neexistuje, je vidět v access logu: chybí pole `"route":"/api/v1/mcp"`. Testy vidí `404`,
protože jejich router SPA fallback nemá; to je ten čistý signál „na routeru nic není".

Token pro agenta (nejnižší role se zápisem je `editor`; `admin`/`maintainer` píšou taky). Token si razí
**uživatel sám pro sebe** — `POST /auth/tokens` ho vždycky vystaví volajícímu principalovi, admin ho za
někoho jiného udělat nemůže. Takže dva kroky:

```bash
# 1) admin založí uživatele s rolí editor
curl -X POST https://<host>/api/v1/admin/users \
  -b admin-session.txt -H 'Content-Type: application/json' \
  -d '{"username":"agent","password":"…","role":"editor"}'

# 2) ten uživatel se přihlásí a vyrazí si token
curl -X POST https://<host>/api/v1/auth/login -c agent.txt \
  -H 'Content-Type: application/json' -d '{"username":"agent","password":"…"}'
curl -X POST https://<host>/api/v1/auth/tokens -b agent.txt \
  -H 'Content-Type: application/json' -d '{"name":"claude"}'
```

Odpověď nese `secret` — **plaintext `kkt_…` se ukáže jen jednou**, uložit hned. Pro read-only agenta
založ uživatele s rolí `viewer`; token dědí roli svého vlastníka, vlastní roli nemá.

Připojení klienta (Claude Code):

```bash
claude mcp add --transport http kukatko https://<host>/api/v1/mcp \
  --header "Authorization: Bearer kkt_…"
```

## Testy

`internal/mcpapi/mcpapi_integration_test.go` (tag `integration`) jede přes **skutečný MCP transport**,
skutečnou auth middleware a skutečné `kkt_` tokeny proti `KUKATKO_TEST_DATABASE_URL`. Pokrývá:
vypnutý server nemountuje route (404, ne 403) · endpoint chce auth · `initialize` handshake ·
viewer vidí jen read tooly · viewer je odmítnut na **každém** write toolu a nic se nezměnilo ·
destruktivní tooly nejsou vystavené · search vrací kompaktní tvar bez EXIFu a se stránkováním ·
vyhledávací jazyk funguje · write token založí album a připne štítek · **každá mutace píše audit
řádek** · částečná editace nevynuluje ostatní pole · bulk je atomický · popisy toolů jsou napsané.

Unit testy (`mcpapi_test.go`, běží v `make check` bez DB) drží čisté helpery, RBAC check a to, že
`exif` neproteče do žádného payloadu.
