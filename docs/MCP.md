# MCP server — knihovna pro AI agenta

Kukátko umí vystavit svou knihovnu jako **MCP server** (Model Context Protocol), takže s ní AI agent
pracuje přímo: hledá, čte, organizuje, odpovídá na otázky. Motivace není hračička — tahle knihovna se
takhle reálně udržuje: *„najdi všechny fotky babičky ze šedesátých let a dej je do alba"* je běžný
pracovní postup, ne demo.

Implementace: [`internal/mcpapi`](../internal/mcpapi), wiring `buildMCPAPI` v `cmd/kukatko/mcp.go`.

---

## Endpoint a transport

| | |
| --- | --- |
| **Cesta** | `POST /api/v1/mcp` |
| **Transport** | Streamable HTTP, **stateless**, odpověď `application/json` (ne SSE) |
| **Guard** | `RequireAuth` (stejná auth jako zbytek `/api/v1`) |
| **Default** | **vypnuto** — `mcp.enabled: false` |
| **Knihovna** | [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) — čisté Go, drží `CGO_ENABLED=0` |

Cesta je `/api/v1/mcp`, protože **všechny** route Kukátka jedou pod `/api/v1` (`server.WithAPI`
mountuje do jednoho subrouteru). Nemá vlastní top-level `/mcp`.

**Stateless** znamená, že každý POST je samostatný: žádná `Mcp-Session-Id`, žádný stav na serveru,
který by šel unést nebo který by expiroval. Endpoint tím zůstává obyčejná autentizovaná route a
context requestu (principal + audit metadata) doteče až do handlerů toolů.

DNS-rebinding guard SDK je **vypnutý** (`DisableLocalhostProtection`): odmítá request, který přijde
po loopbacku s ne-loopback `Host` hlavičkou, což je přesně to, co dělá reverzní proxy před Kukátkem.
Guard chrání *neautentizované* lokální servery; tenhle endpoint vyžaduje platného principala, takže
by rozbil jen reálné nasazení.

## Auth model — žádný nový mechanismus

Endpoint **nepřidává vlastní auth ani žádný bypass**. Sedí za stejným `RequireAuth` a stejným RBAC
jako každá jiná route:

- Agent se autentizuje **API tokenem** (`Authorization: Bearer kkt_…`), viz `internal/auth`.
- **Roli má uživatel, ne token** — token dědí roli svého vlastníka v okamžiku autentizace.
- Role rozhoduje o zápisu přes `Role.CanWrite()`: `viewer` → jen čtení; `editor`, `admin` a
  **`maintainer`** → čtení i zápis. Pro čistě zapisujícího agenta stačí `editor` (nejnižší role se
  zápisem); dřívější role `ai` byla zrušena (migrace `0036`), jejím nástupcem na vrcholu žebříčku je
  `maintainer`.

Hranice je **dvojitá**, schválně:

1. **Write tooly se read-only volajícímu vůbec neregistrují.** Server se staví dvakrát (read-only a
   write) a `getServer` podle principala requestu vybere. Viewer je v `tools/list` ani neuvidí —
   agent nemá koukat na nářadí, které nesmí použít.
2. **Každý write handler roli znovu ověří** (`writerFromContext`). To je ta bezpečnostní hranice;
   bod 1 je UX. Hranice, která žije na jednom místě, se rozpadne, jakmile někdo upraví to druhé.

## Audit

Každá **mutace** projde `internal/audit` **ve stejné transakci** jako změna sama — přesně jako
u člověka. To je celý důvod, proč audit trail existuje. Do `details` se navíc razítkuje `"via": "mcp"`:
*kdo* to udělal a *kterými dveřmi* jsou dvě různé otázky, a ta druhá je po vypuštění agenta na
knihovnu ta zajímavější.

`set_photo_rating` schválně jede přes `internal/bulk` (a ne přes rating store napřímo), protože bulk
si audit řádek v transakci píše sám — HTTP endpoint pro hodnocení audit nepíše, ale agentovo hodnocení
má být dohledatelné jako každá jiná jeho změna.

## Tvar odpovědí — kontext agenta je ten vzácný zdroj

`photos.Photo` má ~60 polí včetně **syrového `exif` JSONB blobu**. Search, který vrátí 50 takových
objektů, je nepoužitelný. Proto:

- Seznamy vrací `photoSummary`: `uid`, `title`, `taken_at`, `media_type`, `thumb_url`. Nic víc.
- Detail (`get_photo`) vrací kurátorovaný výběr sloupců — **`exif` blob nikdy, z žádného toolu**.
- Vše stránkuje: `total`, `offset` a **`remaining`** (kolik jich ještě zbývá).
- Velikosti stránky drží `mcp.page_size` / `mcp.max_page_size`.

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
