# CLAUDE.md — Kukátko

Konvence a tvrdá pravidla projektu. **Čti tohle i [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
před jakoukoli prací.** Tato pravidla platí pro každý task.

## Co to je
Kukátko = samostatná aplikace pro správu fotek/videí, náhrada PhotoPrismu (kombinuje featury
PhotoPrismu + photo-sorteru, robustnější). Plný návrh: `docs/ARCHITECTURE.md`. Fáze: aktivní
vývoj přes autonomní tasky; PhotoPrism zůstává **primární** do cutoveru (import je read-only,
inkrementální).

## Tech stack (závazné)
- **Backend: Go**, jeden statický binár, **`CGO_ENABLED=0`**. Modul `github.com/panbotka/kukatko`.
  Router chi/v5, CLI Cobra, config Viper, DB `pgx`/`pgvector-go`.
- **DB: PostgreSQL + pgvector.** Embeddingy se ukládají **přímo do DB** (`halfvec` + HNSW cosine).
- **Frontend: React + TypeScript + Vite + react-bootstrap + Bootswatch Superhero**, embedovaný do
  binárky přes `//go:embed` (SPA fallback). i18n přes i18next: **čeština default**, angličtina.
- **Obrázky/videa bez CGO:** pure-Go pro JPEG/PNG/WebP; **shell-out** na `heif-convert` (HEIC),
  `exiftool`/`dcraw` (RAW preview), `ffmpeg`/`ffprobe` (video poster/metadata/streaming).

## Struktura a příkazy (scaffold M0)
- **Layout:** `cmd/kukatko/` (tenký Cobra entrypoint: root + `serve` + `migrate` + `version`),
  `internal/server/` (chi HTTP server, graceful shutdown), `internal/version/`
  (ldflags-injectable `Version`/`Commit`), `internal/config/` (typovaná konfigurace,
  Viper, `Load()`), `internal/database/` (pgxpool wrapper `DB` s `Ping`/`Close`/`Pool`,
  embedded migration runner `Migrate`, pgvector typy registrované na každém spojení;
  SQL migrace v `internal/database/migrations/*.sql`), `internal/database/dbtest/`
  (integrační test harness: `dbtest.New(t)`, `dbtest.TruncateAll`). Detail:
  [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).
- **CLI:** `kukatko serve` (načte config, **spustí migrace**, pak poslouchá na
  `web.host:web.port`, default `0.0.0.0:8080`; `GET /healthz` → 200 JSON
  `{"status":"ok","version":{…}}`), `kukatko migrate` (spustí pending migrace samostatně
  a skončí), `kukatko version` (verze + commit). Persistentní flag `--config <path>`
  určuje YAML config.
- **Make cíle:** `fmt` (golangci-lint fmt = gofmt+goimports), `vet`, `lint`, `lint-fix`,
  `test` (unit, `-race`, vyžaduje cgo/gcc), `test-integration` (tag `integration` +
  `KUKATKO_TEST_DATABASE_URL`), `check` (brána), `build` (`CGO_ENABLED=0` → `bin/kukatko`),
  `clean`, `help`. Verzi injectuješ `make build VERSION=x.y.z`.

## Tvrdá brána kvality (NEPŘESKAKOVAT)
- **`make check` (gofmt + go vet + golangci-lint + unit testy) MUSÍ projít.** Je to verification
  command projektu — červený lint/testy = task skončí jako `needs_review`.
- Pro Go kód **používej skill `golang-developer`**.
- **`.golangci.yml` je přísný** (převzatý z photo-sorteru). Needěl ho slabší. `//nolint` jen
  s odůvodněním.
- **Testy jsou povinné u každé změny:** unit testy pro logiku; **integrační testy** pro DB/HTTP
  proti reálné test DB. Nové chování = nové/aktualizované testy. Cíl: rozšiřitelná aplikace,
  kterou další iterace nerozbije. Detail v `docs/ARCHITECTURE.md` §19.
- Frontend: **ESLint** (strict) + **Vitest** musí projít (zapojeno do `make`). Žádné `any` bez důvodu.

## Konfigurace
- **`internal/config`** (`config.Load(path)`): YAML + env override přes Viper, **env vždy
  vyhrává**. Cesta: `--config` flag → `KUKATKO_CONFIG` env → default `config.yaml`. Soubor je
  volitelný (chybějící = jen defaulty + env). Required: `database.url`.
- Env: prefix `KUKATKO_`, tečka → podtržítko (`database.url` → `KUKATKO_DATABASE_URL`,
  `backup.s3.bucket` → `KUKATKO_BACKUP_S3_BUCKET`). Výjimka: `maps.mapy_api_key` ↔ `MAPY_API_KEY`.
- **`config.example.yaml`** dokumentuje všechny klíče + defaulty; je commitnutý. Reálný config
  (`config.yaml`/`config.local.yaml`) a tajemství **necommituj**. Nové konfig klíče přidávej do
  `Config` structu, `setDefaults`, `config.example.yaml` a testů zároveň.

## Databáze
- DB je **už provisionovaná** v shared Postgresu (pgvector 0.8.1 + unaccent).
- DSN čti z gitignorovaného **`.secrets/db.env`**: `KUKATKO_DATABASE_URL` (app),
  `KUKATKO_TEST_DATABASE_URL` (integrační testy, DB `kukatko_test`, bezpečné truncatovat).
  Tamtéž je `MAPY_API_KEY`.
- **Nikdy necommituj tajemství.** `.secrets/`, `*.local.yaml`, `.env*` jsou gitignored.
- Migrace = SQL v `embed.FS` (`internal/database/migrations/NNNN_name.sql`), auto-apply na
  startu ve vzestupném pořadí verze, každá ve vlastní transakci, idempotentně evidované
  v tabulce `schema_migrations`. Jména `0001_init.sql`. FK s `ON DELETE CASCADE`/`SET NULL`
  (žádné sirotky).

## Klíčové vzory
- **Embeddings sidecar se NESTAVÍ.** Kukátko volá existující službu na **boxu** (stejné modely
  jako photo-sorter → 1:1 migrace) na konfigurovatelné `embedding.url`. **Box bývá offline** →
  joby (`image_embed`, `face_detect`) čekají v **persistentní frontě** v Postgresu, upload
  a prohlížení fungují bez něj. Externí závislosti (sidecar, PhotoPrism API, mapy.com, S3) vždy
  za rozhraním → v testech fake/mock.
- **„Zpět vždy funguje":** stav pohledu (filtry/řazení/hledání/stránka) je v **URL query params**
  + History API.
- **Import/migrace:** ukládej externí ID (`photoprism_uid`, `photoprism_file_hash`,
  `photosorter_uid`). PhotoPrism file hash je SHA1, Kukátko používá SHA256.
- **Per-user oblíbené** (ne globální). **mapy.com klíč drž server-side** (backend proxy).
- Streamuj velké soubory (upload/download/video) — nedrž je celé v RAM.

## Definition of Done — před KAŽDÝM commitem
Vždy, na konci každého tasku, v tomto pořadí:
1. **Aktualizuj `README.md` a `CLAUDE.md`** — rozšiř/eedituj je tak, aby odrážely tvé změny:
   nové featury, příkazy/subkomandy, konfigurační klíče, env proměnné, `make` cíle, závislosti,
   konvence. Dokumentace nesmí zestárnout. Pokud se nic relevantního nezměnilo, ověř a nech být.
   Velké architektonické změny promítni i do `docs/ARCHITECTURE.md`.
2. **`make check`** musí projít (gofmt + vet + lint + testy + frontend lint/test).
3. **Commit** (anglicky, výstižně) a **push**. Commit message zakonči řádkem:
   `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`

## Mimo rozsah
- **Fotokniha** (z photo-sorteru se nepřebírá).
- Veřejné sdílení/share-linky nejsou priorita.

## Jazyk
Kód, komentáře, commity, identifikátory **anglicky**. UI texty přes i18n (cs default, en).
