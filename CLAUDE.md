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
  (integrační test harness: `dbtest.New(t)`, `dbtest.TruncateAll`), `internal/auth/`
  (autentizace/autorizace: `Role` admin/editor/viewer + `authorize`, bcrypt cost 12
  `HashPassword`/`CheckPassword`, UID/token generátory, sliding-window `Limiter`,
  `Store` nad pgx, `Service` orchestrace login/session/bootstrap/správa uživatelů,
  `API` = HTTP handlery + RBAC middleware `RequireAuth`/`RequireWrite`/`RequireAdmin` +
  `RegisterRoutes`; session a users v migraci `0002_auth.sql`), `internal/photos/`
  (jádro foto-katalogu: typované modely `Photo`/`PhotoFile`/`Phash`/`Edit`/`MetadataUpdate`,
  `FileRole` original/sidecar/edited, UID generátor prefix `ph`, `Store` nad pgx s
  `Create`/`GetByUID`/`GetByFileHash`/`GetByPhotoprismUID`/`GetByPhotosorterUID`/
  `UpdateMetadata`/`Archive`/`Unarchive`/`Delete`/`List` (filtr archived/private/uploader,
  řazení taken_at/created_at/uid, stránkování), plus `CreateFile`/`ListFiles`,
  `SetPhash`/`GetPhash`, `SetEdit`/`GetEdit`; dedup na SHA256 `file_hash` + externí ID
  `photoprism_uid`/`photoprism_file_hash`(SHA1)/`photosorter_uid`; tabulky v migraci
  `0003_photos.sql`: `photos`, `photo_files` (jeden primary/foto), `photo_phashes`,
  `photo_edits` (all-or-nothing crop, rotace 0/90/180/270); FK `ON DELETE CASCADE`
  na satelity, `uploaded_by` `ON DELETE SET NULL`), `internal/storage/`
  (on-disk úložiště originálů: rozhraní `Storage` + filesystemová implementace `FS`
  `NewFS(root)`; `Store(ctx,src,takenAt,originalName)` streamuje na disk + počítá **SHA256**,
  layout `YYYY/MM/<filename>` (datum z `taken_at`, fallback čas importu), publikace
  **atomickým hard-linkem** přes temp v `<root>/.tmp`; kolize jmen: shodný obsah →
  `ErrAlreadyExists` (dedup signál), jiný obsah → číselný sufix `name_1.ext` bez přepisu;
  `Open`/`Stat`/`Delete`/`AbsPath` s cestami confinovanými do rootu (`ErrInvalidPath`);
  MIME z obsahu (sniff 512 B) + přípona jako hint (`mediaTypeByExt` pro HEIC/RAW/video);
  sentinely `ErrAlreadyExists`/`ErrInvalidPath`/`ErrTooManyCollisions`; nikdy nedrží soubor
  celý v RAM), `internal/thumb/`
  (thumbnailer náhledů, **CGO-free**: registr velikostí `sizes`+`sizeOrder` ve dvou režimech
  `fit` (max-strana, zachová poměr, neupscaluje) a `crop-square` (center-crop), default sada
  `fit_720/1280/1920/2560/3840` + `tile_100/224/500`; cache layout pod `storage.cache_path`
  `thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg` (shard z hex SHA256), regenerovatelné +
  **idempotentní** (skip existujících) + atomický zápis temp+rename; `Thumbnailer` =
  `New(store,cacheDir,WithConcurrency(n))` s API `Generate(ctx,photo,sizes...)`/
  `GenerateAll(ctx,photo)` (mapa size→abs cesta)/`Path(hash,size)`/`Open(hash,size)`;
  dekód jednou na fotku, paralelní enkód velikostí (errgroup, default `GOMAXPROCS`),
  **EXIF orientace** (1–8) automaticky; pure-Go JPEG/PNG/WebP + `golang.org/x/image`
  (`draw.CatmullRom` resize); sentinely `ErrUnknownSize`/`ErrInvalidHash`/`ErrNotCached`;
  `SizeNames()`/`IsValidSize`), `internal/imgconvert/`
  (HEIC/RAW → dekódovatelný JPEG, **shell-out**: `EnsureDecodable(ctx,path)` →
  (cesta, cleanup, err); JPEG/PNG/WebP passthrough, **HEIC** přes `heif-convert` na temp JPEG,
  **RAW** (cr2/cr3/nef/arw/dng/raf/orf/rw2/pef/srw) vytáhne embedded preview přes
  `exiftool -b -PreviewImage` (fallback `-JpgFromRaw`/`-ThumbnailImage`) místo demosaicu;
  `DetectFormat`/`IsSupportedFormat`; sentinely `ErrConverterMissing`/`ErrUnsupportedFormat`/
  `ErrNoEmbeddedPreview`; chybějící nástroj = jasná chyba), `internal/web/`
  (SPA fallback handler `web.Handler()`/`SPAHandler` + `internal/web/static` embed
  `//go:embed all:dist/*`; Vite build se zapisuje do `internal/web/static/dist`, ten je
  gitignorovaný kromě committed `.gitkeep`, aby embed kompiloval i bez buildnutého
  frontendu). Detail: [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).
- **Frontend layout:** `web/` (Vite + React 19 + TS): `web/src/` s `components/`
  (`Layout` = navbar shell s user-menu/logout + role-gated nav, `LanguageSwitcher`),
  `pages/` (`HomePage` volá `GET /healthz`, `LoginPage`, `AccountPage` = změna vlastního hesla,
  `NotFoundPage`), `auth/` (`AuthContext`/`useAuth` + `AuthProvider` = boot `GET /auth/me`,
  vystavuje `user`/`role`/`login`/`logout`/`refresh`/`canWrite`/`isAdmin`; `ProtectedRoute` =
  `RequireAuth` + `RequireRole` route guardy), `lib/` (`urlState.ts` = hook `useUrlState` +
  pure `readUrlState`/`writeUrlState`: stav pohledu ↔ URL query přes History API, „Zpět vždy
  funguje"), `services/` (`health.ts`, `auth.ts` = login/logout/me/changePassword, typy
  `User`/`Role`/`AuthSession`, `ApiError` se statusem, `canWrite`/`roleAtLeast`,
  `MIN_PASSWORD_LENGTH`), `i18n/` (i18next init + `locales/{cs,en}/common.json`; typované klíče
  přes `types/i18next.d.ts` — nové stringy přidávej do **obou** locale souborů), `test/setup.ts`.
  Routing v `App.tsx`: `/login` veřejné, zbytek pod `RequireAuth` → `Layout`. Konfig:
  `vite.config.ts` (build → `../internal/web/static/dist`, vitest jsdom, dev proxy
  `/healthz`+`/api` → `:8080`), `eslint.config.js` (strict typed), `.prettierrc.json`,
  `tsconfig*.json`.
- **CLI:** `kukatko serve` (načte config, **spustí migrace**, **bootstrapne admina** a spustí
  hodinové čištění expirovaných session, pak poslouchá na `web.host:web.port`, default
  `0.0.0.0:8080`; `GET /healthz` → 200 JSON `{"status":"ok","version":{…}}`, auth/admin API
  pod `/api/v1` — viz níže, ostatní cesty servíruje **embedované SPA** s fallbackem na
  `index.html`), `kukatko migrate` (spustí pending migrace samostatně a skončí),
  `kukatko version` (verze + commit). Persistentní flag `--config <path>` určuje YAML config.
  `server.New(addr, server.WithAPI(register))` mountuje route-skupiny pod `/api/v1`.
- **Auth API (`/api/v1`):** `POST /auth/login` (set HttpOnly+SameSite=Strict cookie + opaque
  `download_token`), `POST /auth/logout`, `GET /auth/me`, `POST /auth/password` (zruší ostatní
  session). Admin-only: `GET|POST /admin/users`, `PATCH /admin/users/{uid}`,
  `POST /admin/users/{uid}/disable`, `POST /admin/users/{uid}/password` (reset zruší všechny
  session uživatele). Role: admin/editor/viewer (editor+admin write). **Sliding session expiry**
  (`auth.session_ttl` do cap `auth.session_max_lifetime`), **login rate-limit**
  (`auth.login_rate_limit`/`auth.login_rate_window` → 429), **bootstrap admin** z
  `auth.bootstrap_admin_username/password`.
- **Make cíle:** `fmt` (golangci-lint fmt + Prettier `--write`), `vet`, `lint` (golangci-lint
  + ESLint + Prettier `--check`), `lint-fix`, `test` (Go unit `-race` + Vitest; Go vyžaduje
  cgo/gcc), `test-integration` (tag `integration` + `KUKATKO_TEST_DATABASE_URL`, `-p 1` —
  integrační balíky sdílí jednu test DB, takže běží sériově), `check`
  (brána), `build` (frontend build + `CGO_ENABLED=0` → `bin/kukatko`), `clean`, `help`.
  Frontend-only cíle: `web-deps` (`npm ci`), `web-build`, `web-fmt`, `web-lint`, `web-test`.
  Verzi injectuješ `make build VERSION=x.y.z`. Frontend potřebuje **Node.js 22+**.
- **CI/CD a balíčkování:** `.github/workflows/ci.yml` (push/PR → job `check` = `make check`
  na Go 1.26 + Node 22 + golangci-lint v2.11.4; job `integration` = `make test-integration`
  proti service containeru `pgvector/pgvector:pg17`, extensions `vector`/`unaccent` v setup
  kroku, `KUKATKO_TEST_DATABASE_URL` na efemérní CI DB). `.github/workflows/release.yml`
  (tag `v*.*.*`) pouští **goreleaser** (`.goreleaser.yaml`): `CGO_ENABLED=0` pro arm64+amd64,
  verze/commit přes ldflags do `internal/version`, frontend build v before-hooku, **.deb**
  přes nfpm. `deb/`: `kukatko.service` (systemd, user `kukatko`, `EnvironmentFile`,
  `Restart=always`), `kukatko.env` (dpkg conffile `config|noreplace`),
  `postinstall.sh`/`preremove.sh`/`postremove.sh` (user + `/var/lib/kukatko/{originals,cache}`).
  Apt deps: `libimage-exiftool-perl`, `libheif-examples|libheif-bin`, `dcraw`,
  `postgresql-client`, `ca-certificates`; **bez texlive**.

## Tvrdá brána kvality (NEPŘESKAKOVAT)
- **`make check` (gofmt + go vet + golangci-lint + unit testy) MUSÍ projít.** Je to verification
  command projektu — červený lint/testy = task skončí jako `needs_review`.
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
