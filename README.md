# Kukátko

Samostatná aplikace pro správu fotek — náhrada za PhotoPrism, která kombinuje to nejlepší
z PhotoPrismu a z [photo-sorteru](https://github.com/kozaktomas/photo-sorter), ale je
**robustnější a použitelnější**.

- **Jeden spustitelný binár** (Go) včetně embedovaného frontendu (React + Bootstrap/Superhero).
- **PostgreSQL + pgvector** jako jediný zdroj pravdy pro metadata i vektory.
- **Sémantické i fulltextové hledání**, podobné fotky, **rozpoznávání obličejů/lidí**.
- **Pi-first:** běží na Raspberry Pi, výpočet embeddingů deleguje na výkonný stroj (box s GPU).
- **Import z PhotoPrismu** přes API (+ stažení originálů) a **migrace dat z photo-sorteru**.
- Mapy ([mapy.com](https://mapy.com)), slideshow, alba, štítky, hromadná editace metadat,
  per-user oblíbené, dvojjazyčné UI (čeština default + angličtina), S3 zálohování.

> **Stav:** aktivní vývoj (milník M0 — kostra backendu + frontendu). Architektura:
> [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), vývojářský návod:
> [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).
>
> PhotoPrism zůstává **primární** systém až do ostrého přechodu na Kukátko; do té doby
> Kukátko běží paralelně a importuje z PhotoPrismu read-only.

## Rychlý start

Potřebuješ **Go 1.26+**, **golangci-lint v2** a **Node.js 22+** (npm) pro frontend.

```bash
make check            # brána kvality: fmt + vet + lint + unit testy (Go i frontend)
make build            # build frontendu (Vite) + statický binár do bin/kukatko (CGO_ENABLED=0)

# serve i migrate potřebují aspoň database.url (typicky přes env):
export KUKATKO_DATABASE_URL="postgres://kukatko:…@localhost:5432/kukatko"
./bin/kukatko migrate                     # spustí pending DB migrace a skončí
./bin/kukatko serve                       # spustí migrace, pak HTTP server (default 0.0.0.0:8080)
./bin/kukatko serve --config config.yaml  # explicitní cesta ke konfiguraci
./bin/kukatko version                     # vypíše verzi a commit
```

## Databáze a migrace

Kukátko používá **PostgreSQL + pgvector** (typy `vector`/`halfvec`) a **unaccent**. Migrace
jsou SQL soubory embedované v binárce (`internal/database/migrations/NNNN_name.sql`); spouští
se automaticky na startu `serve` (nebo ručně přes `kukatko migrate`) ve vzestupném pořadí
verze, každá ve vlastní transakci, a evidují se v tabulce `schema_migrations` (idempotentní —
nikdy se neaplikují dvakrát).

Integrační testy běží proti reálné test DB:

```bash
export KUKATKO_TEST_DATABASE_URL="postgres://kukatko:…@localhost:5432/kukatko_test"
make test-integration   # bez KUKATKO_TEST_DATABASE_URL se DB testy přeskočí (t.Skip)
```

## Konfigurace

Kukátko se konfiguruje **YAML souborem s env override** (Viper; env vždy vyhrává).
Zkopíruj [`config.example.yaml`](config.example.yaml) na `config.yaml` (nebo gitignorovaný
`config.local.yaml`) a uprav. Cesta k souboru se bere z `--config` flagu, jinak z env
`KUKATKO_CONFIG`, jinak default `config.yaml`. **Soubor je volitelný** — se `KUKATKO_DATABASE_URL`
v prostředí běží appka na defaultech.

Env proměnné mají prefix `KUKATKO_` a tečky v klíči nahrazují podtržítka
(`database.url` → `KUKATKO_DATABASE_URL`, `web.port` → `KUKATKO_WEB_PORT`,
`backup.s3.bucket` → `KUKATKO_BACKUP_S3_BUCKET`). Výjimka: mapy.com klíč se čte z
neprefixované `MAPY_API_KEY`. Tajemství (DSN, session secret, admin heslo, S3 klíče,
mapy klíč) drž v prostředí, ne v commitnutém souboru. Všechny klíče a defaulty popisuje
`config.example.yaml`.

## Autentizace a autorizace

Kukátko má vlastní účty (bcrypt cost 12), role **admin / editor / viewer** (editor a admin mají
write, viewer je read-only) a session přes **HttpOnly + SameSite=Strict** cookie s opaque
tokenem. Vylepšení proti photo-sorteru: **sliding expiry** (aktivní session se prodlužuje),
**rate-limit na loginu** a **změna hesla zruší ostatní session** uživatele.

- **Bootstrap admina:** na prázdné tabulce `users` se z `auth.bootstrap_admin_username` +
  `auth.bootstrap_admin_password` vytvoří první admin (jinak `serve` jen zaloguje varování).
  Heslo dej přes env `KUKATKO_AUTH_BOOTSTRAP_ADMIN_PASSWORD`, necommituj ho.
- **Sliding expiry:** každý ověřený request posune `expires_at` na `now+session_ttl`, ale nikdy
  za `created_at+session_max_lifetime`. Expirované session čistí hodinová úloha.
- **Rate-limit:** víc než `auth.login_rate_limit` neúspěšných pokusů na (username+IP) za
  `auth.login_rate_window` vrací HTTP 429.

Endpointy pod `/api/v1` (JSON):

| Metoda | Cesta | Přístup | Popis |
|--------|-------|---------|-------|
| POST | `/auth/login` | veřejné | `{username,password}` → nastaví session cookie, vrátí uživatele + `download_token` |
| POST | `/auth/logout` | veřejné | zruší session a cookie (idempotentní) |
| GET  | `/auth/me` | přihlášený | aktuální uživatel + `download_token` |
| POST | `/auth/password` | přihlášený | `{current_password,new_password}` → změní heslo, zruší ostatní session |
| GET  | `/admin/users` | admin | seznam uživatelů |
| POST | `/admin/users` | admin | `{username,password,display_name,email,role}` → vytvoří uživatele |
| PATCH | `/admin/users/{uid}` | admin | `{display_name,email,role,disabled}` → upraví profil |
| POST | `/admin/users/{uid}/disable` | admin | zakáže účet (zruší jeho session) |
| POST | `/admin/users/{uid}/password` | admin | `{new_password}` → reset hesla (zruší všechny jeho session) |

RBAC se vynucuje middlewarem (`RequireAuth` / `RequireWrite` / `RequireAdmin`). Konfigurační
klíče (`auth.session_ttl`, `auth.session_max_lifetime`, `auth.login_rate_limit`,
`auth.login_rate_window`, `web.secure_cookies`) popisuje [`config.example.yaml`](config.example.yaml).

## Frontend

SPA je **React 19 + TypeScript + Vite** v adresáři [`web/`](web/), stylovaná tématem
**Bootswatch Superhero** (dark) přes **react-bootstrap**, s routováním `react-router-dom`
a i18n přes **i18next** (**čeština default** + angličtina, volba se persistuje do
`localStorage`). Build (`npm run build`) se zapisuje do `internal/web/static/dist`, odkud ho
Go embeduje (`//go:embed`) a servíruje s **SPA fallbackem** (neznámé ne-asset cesty →
`index.html`; fingerprintované soubory pod `/assets/` mají immutable cache). `kukatko serve`
tak vrací jak `GET /healthz`, tak celé SPA.

**Autentizace ve frontendu:** `AuthProvider` ([`web/src/auth/`](web/src/auth/)) načte na startu
`GET /auth/me` a přes hook `useAuth()` vystavuje `user`/`role`/`login`/`logout`/`refresh` +
odvozené `canWrite`/`isAdmin`. Přihlašovací stránka (`/login`) je veřejná; vše ostatní hlídá
`RequireAuth` (nepřihlášený → redirect na `/login` s uložením původní cesty, po přihlášení
návrat zpět), role hlídá `RequireRole`. Navbar ukazuje přihlášeného uživatele s odhlášením a
odkazem na **Můj účet** (`/account` — změna vlastního hesla přes `POST /auth/password`); write
akce jsou skryté prohlížečům (`viewer`). Auth volání backendu jsou v
[`web/src/services/auth.ts`](web/src/services/auth.ts) (typy `User`/`Role`/`AuthSession`,
`ApiError` se statusem, helpery `canWrite`/`roleAtLeast`).

**URL = stav (Zpět vždy funguje):** sdílený hook `useUrlState`
([`web/src/lib/urlState.ts`](web/src/lib/urlState.ts)) čte/zapisuje stav pohledu (filtry, řazení,
hledání, stránka) do query parametrů přes History API, takže Back/Forward obnoví předchozí stav.
Tohle je konvence pro budoucí seznam/knihovnu — výchozí hodnoty se z URL vynechávají (čisté URL),
update defaultně pushuje historii (`{ replace: true }` pro živé psaní).

Vývoj frontendu (dev server s proxy na Go backend) a samostatné cíle:

```bash
cd web && npm install     # jednorázová instalace závislostí
npm run dev               # Vite dev server (proxy /healthz a /api → localhost:8080)

# nebo přes Makefile (z kořene repa):
make web-build            # build SPA do internal/web/static/dist
make web-lint             # ESLint (strict) + Prettier --check
make web-test             # Vitest (React Testing Library)
make web-fmt              # Prettier --write
```

Frontendové cíle jsou zapojené do hlavní brány: `make lint`/`make test`/`make fmt`/`make check`
spouští i ESLint, Prettier a Vitest. Build SPA běží v `make build` před `go build`.

Více v [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) (layout, make cíle, brána kvality).
