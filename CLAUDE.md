# CLAUDE.md — Kukátko

Konvence a tvrdá pravidla projektu. **Čti tohle i [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
před jakoukoli prací.** Tato pravidla platí pro každý task.

Tenhle soubor obsahuje **jen pravidla a rozcestník**. Popisné detaily (balíčky, endpointy,
komponenty, konfigurační klíče) žijí v `docs/` a čteš je až když je potřebuješ.

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
  binárky přes `//go:embed` (SPA fallback). Ikony **jen `bootstrap-icons`** přes komponentu `Icon`
  (jedna sada, dekorativní `aria-hidden`). i18n přes i18next: **čeština default**, angličtina.
  Virtualizace dlouhých mřížek/seznamů přes **`react-virtuoso`**. Mapový pohled přes
  **`leaflet`** + **`leaflet.markercluster`** (dlaždice přes backend proxy, klíč zůstává server-side).
- **Obrázky/videa bez CGO:** pure-Go pro JPEG/PNG/WebP; **shell-out** na `heif-convert` (HEIC),
  `exiftool`/`dcraw` (RAW preview), `ffmpeg`/`ffprobe` (video poster/metadata/streaming).

## Kde co najdeš
Otevři **jeden** dokument podle toho, na co saháš. Nečti je preventivně všechny.

| Sáhnu na… | Čtu |
| --- | --- |
| Go balíček (`internal/*`, `cmd/*`) | [`docs/PACKAGES.md`](docs/PACKAGES.md) |
| HTTP endpoint pod `/api/v1` | [`docs/API.md`](docs/API.md) |
| Frontend (`web/`) — komponenta, hook, stránka, služba | [`docs/FRONTEND.md`](docs/FRONTEND.md) |
| CLI příkaz, konfigurační klíč, `make` cíl, CI/balíčkování | [`docs/OPERATIONS.md`](docs/OPERATIONS.md) |
| Architektura, datový model, milníky, testovací strategie | [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) |
| Lokální vývoj, build frontendu, embed | [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) |
| Výkon (náhledy, vips, HNSW `ef_search`, indexy) | [`docs/PERF.md`](docs/PERF.md) |
| Obnova ze zálohy / disaster recovery | [`docs/RESTORE.md`](docs/RESTORE.md) |
| UX rozhodnutí a audit | [`docs/UX_AUDIT.md`](docs/UX_AUDIT.md) |

## Mapa balíčků
Jeden řádek na balíček — ať víš, co existuje, aniž bys otevíral `docs/PACKAGES.md`.

- `cmd/kukatko` — tenký Cobra entrypoint (`serve`/`migrate`/`import`/`backup`/`restore`/`maintenance`/`ctl`/`version`) + `buildXxxAPI` wiring
- `web/` — Vite + React 19 + TS frontend, buildí se do `internal/web/static/dist`
- `internal/audit` — durable audit trail; `Write(ctx, exec, Entry)` běží **v téže transakci** jako mutace
- `internal/auditapi` — admin-only `GET /audit` (read-only výpis)
- `internal/auth` — role admin/editor/viewer, bcrypt, sliding sessions, RBAC middleware, API tokeny (Bearer)
- `internal/backup` — S3 záloha (pg_dump + sync originálů + retence) **a** obnova
- `internal/backupapi` — admin-only `GET`/`POST /backup`
- `internal/bulk` — hromadná editace metadat, celá dávka v jedné transakci
- `internal/bulkapi` — `POST /photos/bulk`
- `internal/cluster` — auto-clustering nepřiřazených obličejů (union-find nad HNSW sousedy)
- `internal/clusterapi` — `/faces/clusters` (list, assign, remove-face)
- `internal/config` — typovaná konfigurace, Viper, `Load()`
- `internal/ctl` — **klient** vlastního API pro `kukatko ctl`: kontexty (kubectl-style), Bearer token, table/JSON výstup
- `internal/database` — pgxpool wrapper, embedded migration runner, pgvector typy
- `internal/duplicates` — near-dup skupiny (pHash banded-LSH + embedding HNSW, union-find); jen čte
- `internal/duplicatesapi` — `GET /duplicates`
- `internal/embedding` — HTTP klient inferenčního sidecaru na boxu; offline-aware typové chyby
- `internal/embedjob` — worker handler `image_embed` + backfill
- `internal/exif` — extrakce EXIF/GPS (exiftool, fallback pure-Go)
- `internal/facejob` — worker handler `face_detect` + backfill
- `internal/facematch` — face↔marker IoU matching, návrhy identit, přiřazovací state machine
- `internal/globalsearchapi` — `GET /search/global` (grouped cross-entity)
- `internal/imgconvert` — HEIC/RAW/video → dekódovatelný JPEG (shell-out)
- `internal/importapi` — admin-only triggery importů + historie běhů
- `internal/importer` — evidence běhů importu/migrace + high-watermarky
- `internal/ingest` — upload pipeline: stream, SHA256 dedup, metadata, náhledy, enqueue jobů
- `internal/jobs` — persistentní fronta jobů v Postgresu (retry, dedup, backoff, `Defer`)
- `internal/jobsapi` — admin-only `/jobs` (stats, list, requeue)
- `internal/maintenance` — integritní kontrola & opravy knihovny; **nikdy nemaže originály**
- `internal/maintenanceapi` — admin-only `/maintenance` (scan, repair)
- `internal/mapsapi` — tile proxy, reverse geocode, GeoJSON feed
- `internal/mapy` — server-side klient mapy.com; **klíč nikdy neopustí server**
- `internal/mediaurl` — razí `thumb_url`/`download_url` do payloadů; podepsaná URL, nebo vlastní routa
- `internal/metrics` — Prometheus registry + kolektory (DB pool, hloubka fronty)
- `internal/obs` — strukturované logování (JSON slog na stderr)
- `internal/organize` — alba, štítky, **per-user** oblíbené a hodnocení
- `internal/organizeapi` — `/albums`, `/labels`
- `internal/outlierapi` — `GET /subjects/{uid}/outliers`
- `internal/outliers` — per-osoba outlier detekce obličejů (vzdálenost od centroidu)
- `internal/people` — subjekty (osoby/zvířata/jiné) a markery; drží `faces` cache konzistentní
- `internal/peopleapi` — `/subjects` + galerie fotek subjektu
- `internal/phash` — perceptuální hashe (pHash přes DCT, dHash gradientní)
- `internal/photoapi` — read/curace API nad katalogem: list, search, média, edit, faces, rating
- `internal/photoedit` — aplikace nedestruktivního editu (crop/rotace/jas/kontrast), pure-Go
- `internal/photoprism` — read-only HTTP klient běžícího PhotoPrismu
- `internal/photos` — **jádro foto-katalogu**, `Store` nad pgx; dedup na SHA256 `file_hash`
- `internal/photosorter` — read-only klient PostgreSQL DB photo-sorteru
- `internal/places` — cache reverse-geokódovaných míst (vedlejší tabulka `photo_places`)
- `internal/placesapi` — `GET /places` (hierarchie zemí → měst s počty)
- `internal/placesjob` — worker handler `places` (reverse geokód, rate-limited kvůli kreditům)
- `internal/ppimport` — inkrementální **idempotentní** import z PhotoPrismu
- `internal/processapi` — admin-only `/process/*` backfilly (embeddingy, faces, clustery, places)
- `internal/psimport` — inkrementální **idempotentní** přímá migrace z photo-sorteru
- `internal/ratelimit` — per-key token-bucket limiter + HTTP middleware
- `internal/restoreapi` — admin-only **read-only** `/restore/*` (destruktivní obnova jen přes CLI)
- `internal/savedsearch` — per-user uložená hledání („smart alba")
- `internal/savedsearchapi` — `/saved-searches`, vše scopnuté na vlastníka (cizí → 404)
- `internal/server` — chi HTTP server, graceful shutdown, `New(addr, WithAPI(...))`
- `internal/storage` — úložiště originálů (`YYYY/MM`, SHA256): lokální `FS` nebo Cloudflare `R2` s podepsanými URL
- `internal/storagemigrate` — resumovatelný přesun knihovny do object storu; ověř → commitni řádek → teprve pak smaž originál
- `internal/system` — agregace provozního stavu instance pro admin dashboard
- `internal/systemapi` — admin-only `GET /system/status`
- `internal/thumb` — thumbnailer (pure-Go default, volitelný `vips` engine), cache layout
- `internal/thumbjob` — worker handler `thumbnail` (regenerace náhledů + pHashe)
- `internal/trash` — trvalé mazání (purge) archivovaných fotek + plánovaná retence
- `internal/vectors` — embeddingy a obličeje přímo v Postgresu (`halfvec` + HNSW cosine)
- `internal/version` — ldflags-injectable `Version`/`Commit`
- `internal/video` — shell-out na ffprobe/ffmpeg: metadata, poster frame, on-the-fly transcode
- `internal/wake` — volitelný Wake-on-LAN auto-wake boxu (**default off**, plně inertní)
- `internal/web` — SPA fallback handler + `//go:embed` embedovaný frontend
- `internal/worker` — in-process worker runtime nad frontou jobů (claim/dispatch/complete)

## Tvrdá brána kvality (NEPŘESKAKOVAT)
- **`make check` MUSÍ projít.** Je to verification command projektu — červený lint/testy = task
  skončí jako `needs_review`. **`check` nikdy nemění soubory** (formátování jen ověřuje;
  aplikuje ho `make fmt`), takže po úspěšném běhu je `git status --short` prázdný.
  Race detector žije v `make test-race` (běží v CI), ne v bráně.
- **`CLAUDE.md` obsahuje jen pravidla a rozcestník.** Popisné detaily patří do `docs/`.
  Limit 300 řádků vynucuje `make docs-budget`. Neobcházej ho — přesuň text do správného dokumentu.
- Pro Go kód **používej skill `golang-developer`**.
- **`.golangci.yml` je přísný** (převzatý z photo-sorteru). Needěl ho slabší. `//nolint` jen
  s odůvodněním.
- **Testy jsou povinné u každé změny:** unit testy pro logiku; **integrační testy** pro DB/HTTP
  proti reálné test DB. Nové chování = nové/aktualizované testy. Cíl: rozšiřitelná aplikace,
  kterou další iterace nerozbije. Detail v `docs/ARCHITECTURE.md` §19.
- Frontend: **ESLint** (strict) + **Prettier** (`--check`) + **Vitest** musí projít (zapojeno do
  `make`). Žádné `any` bez důvodu.

## Konfigurace
- **`internal/config`** (`config.Load(path)`): YAML + env override přes Viper, **env vždy
  vyhrává**. Cesta: `--config` flag → `KUKATKO_CONFIG` env → default `config.yaml`. Soubor je
  volitelný (chybějící = jen defaulty + env). Required: `database.url`.
- Env: prefix `KUKATKO_`, tečka → podtržítko (`database.url` → `KUKATKO_DATABASE_URL`,
  `backup.s3.bucket` → `KUKATKO_BACKUP_S3_BUCKET`). Výjimka: `maps.mapy_api_key` ↔ `MAPY_API_KEY`.
- **`config.example.yaml`** dokumentuje všechny klíče + defaulty; je commitnutý. Reálný config
  (`config.yaml`/`config.local.yaml`) a tajemství **necommituj**. Nové konfig klíče přidávej do
  `Config` structu, `setDefaults`, `config.example.yaml`, testů **a `docs/OPERATIONS.md`** zároveň.
- Katalog všech klíčů (`thumb.*`, `video.*`, `embedding.wake.*`, `ratelimit.*`, `maps.*`, `log.*`,
  `metrics.*`) je v [`docs/OPERATIONS.md`](docs/OPERATIONS.md).

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

## Definition of Done — na konci KAŽDÉHO tasku
**Task NENÍ hotový, dokud není commitnutý a pushnutý.** Dokončení tasku vždy zahrnuje
commit — nikdy nenechávej necommitnuté změny v pracovním stromu ani „hotovou" práci bez
commitu. Vždy, na konci každého tasku, v tomto pořadí:

1. **Zapiš změnu do správného dokumentu.** Dokumentace nesmí zestárnout. Routing:
   - nový/změněný Go balíček → `docs/PACKAGES.md` (+ jeden řádek do `## Mapa balíčků` výš)
   - nový/změněný HTTP endpoint → `docs/API.md`
   - nová/změněná frontend komponenta, hook, stránka, služba → `docs/FRONTEND.md`
   - nový konfigurační klíč → `docs/OPERATIONS.md` **a** `config.example.yaml`
   - nový CLI subkomand nebo `make` cíl → `docs/OPERATIONS.md`
   - velká architektonická změna → `docs/ARCHITECTURE.md`
   - uživatelsky viditelná featura → `README.md`
   - **`CLAUDE.md` sáhni jen tehdy, když se změnilo _pravidlo_ nebo přibyl/zmizel balíček.**
     Nikdy do něj nepiš popisné detaily — na to je `docs/` a hlídá to `make docs-budget`.
2. **`make check`** musí projít (docs-budget + fmt-check + lint + typecheck + testy + frontend).
3. **`make dev`** (= `./scripts/dev.sh`) musí projít — dev server nastartuje a odpoví na
   `/healthz`. Zachytí, co `make check` z principu nevidí: chybějící migraci, rozbité wiring
   v `cmd/kukatko`, panic při startu. Neúspěšný start (exit 1) = **necommituj**. Detail
   v `docs/DEVELOPMENT.md`.
4. **Commit** (anglicky, výstižně) a **push** — tímto krokem task teprve končí, viz pravidlo
   výše. Commit message zakonči řádkem:
   `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`

## Mimo rozsah
- **Fotokniha** (z photo-sorteru se nepřebírá).
- Veřejné sdílení/share-linky nejsou priorita.

## Jazyk
Kód, komentáře, commity, identifikátory **anglicky**. UI texty přes i18n (cs default, en).
