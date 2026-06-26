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

### Foto-schéma (`internal/photos`)

Jádro katalogu je v migraci `0003_photos.sql` a balíčku `internal/photos`:

- **`photos`** — jeden řádek na fotku/video; PK `uid` (app-generovaný, prefix `ph`),
  dedup na **SHA256** `file_hash` (UNIQUE), EXIF/kamera/GPS metadata, `exif` JSONB (GIN),
  `archived_at`/`private`, `uploaded_by` (FK `users` `ON DELETE SET NULL`). Externí ID pro
  import/migraci: `photoprism_uid`, `photoprism_file_hash` (SHA1), `photosorter_uid`.
- **`photo_files`** — originál + odvozeniny, `role` original/sidecar/edited, max. jeden
  `is_primary` na fotku. **`photo_phashes`** — `phash`/`dhash` (near-dup). **`photo_edits`**
  — nedestruktivní úpravy (crop 0..1 all-or-nothing, rotace 0/90/180/270, brightness/contrast).
  Satelitní tabulky mají FK `ON DELETE CASCADE`.

`photos.Store` (nad pgx poolem) nabízí `Create`, `GetByUID`/`GetByFileHash`/
`GetByPhotoprismUID`/`GetByPhotosorterUID`, `UpdateMetadata`, `Archive`/`Unarchive`,
`Delete`, `List` (filtr archived/private/uploader, řazení, stránkování) a metody pro
soubory/phash/edits. Plné CRUD filtry a HTTP API přijdou v dalším tasku.

### Úložiště originálů (`internal/storage`)

On-disk vrstva pro originální média. Rozhraní `Storage` + filesystemová implementace `FS`
(`NewFS(root)`):

- **`Store(ctx, src, takenAt, originalName)`** streamuje vstup na disk (nikdy nedrží celý
  soubor v RAM), počítá při zápisu **SHA256** a vrací `StoredFile{Hash, RelPath, Size, MIME}`.
  Layout je `YYYY/MM/<filename>` (datum z `taken_at`, fallback na čas importu). Zápis je
  crash-safe a race-free: data jdou do temp souboru v `<root>/.tmp` a publikují se **atomickým
  hard-linkem** na cílovou cestu.
- **Kolize jmen:** stejné jméno + **shodný obsah** → vrátí existující `StoredFile` se sentinelem
  `ErrAlreadyExists` (dedup signál pro volajícího); stejné jméno + **jiný obsah** → uloží pod
  číselným sufixem (`name_1.ext`), nikdy nepřepíše. Autoritativní katalogový dedup je věcí DB
  (`photos.file_hash` UNIQUE).
- **`Open`/`Stat`/`Delete`/`AbsPath`** pracují s relativní cestou; všechny cesty jsou
  confinované do rootu (žádný únik přes `..`), neplatné cesty vrací `ErrInvalidPath`.
- **MIME** se detekuje z obsahu (sniffing prvních 512 B) s příponou jako fallback; tabulka
  `mediaTypeByExt` pokrývá formáty, které stdlib nezná (HEIC/HEIF/AVIF, RAW, kontejnerové video).

### Náhledy / thumbnailer (`internal/thumb` + `internal/imgconvert`)

Generování a cache odvozených JPEG náhledů přímo na Pi, **bez CGO** (pure-Go dekódování +
shell-out na externí nástroje pro HEIC/RAW).

- **Registr velikostí** (`internal/thumb`): pojmenované velikosti ve dvou režimech — `fit_*`
  (nejdelší strana ≤ limit, zachová poměr, neupscaluje) a `tile_*` (center-crop na čtverec).
  Výchozí sada: `fit_720/1280/1920/2560/3840` a `tile_100/224/500` (kvalita JPEG ~85–90).
  Sada se snadno rozšiřuje — přidáš položku do `sizes` + `sizeOrder` a propíše se do celé
  pipeline. Introspekce: `SizeNames()`, `IsValidSize(name)`.
- **Cache layout** pod `storage.cache_path`: `thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg`
  (shardováno podle prvních tří byte-párů hex SHA256 originálu). Plně regenerovatelné
  z originálů a **idempotentní** — existující velikost se nepřegeneruje ani nepřepíše. Zápis
  je atomický (temp + rename), takže paralelní zápis stejného obsahu konverguje race-free.
- **API** (`thumb.New(store, cachePath)`): `Generate(ctx, photo, sizes...)`,
  `GenerateAll(ctx, photo)` (vrací mapu `size → absolutní cesta`), `Path(hash, size)`,
  `Open(hash, size)` (vrací `ErrNotCached`, dokud náhled neexistuje). Zdroj se dekóduje
  **jednou na fotku**, jednotlivé velikosti se enkódují paralelně s omezenou konkurencí
  (`WithConcurrency(n)`, default `GOMAXPROCS`). **EXIF orientace** (`photo.FileOrientation`,
  1–8) se aplikuje automaticky.
- **HEIC/RAW** (`internal/imgconvert`): `EnsureDecodable(ctx, path)` vrátí cestu k souboru,
  který umí `image.Decode`. JPEG/PNG/WebP projdou beze změny; **HEIC** se převede přes
  `heif-convert` na dočasný JPEG; **RAW** (cr2/cr3/nef/arw/dng/raf/orf/rw2/pef/srw) vytáhne
  **embedded JPEG preview** přes `exiftool -b -PreviewImage` (fallback `-JpgFromRaw`/
  `-ThumbnailImage`) místo plného demosaicu. Chybějící externí nástroj vrací jasný
  `ErrConverterMissing`. Runtime apt závislosti: `libheif-examples`/`libheif-bin`,
  `libimage-exiftool-perl`.

### EXIF / GPS metadata (`internal/exif`)

Extrakce metadat při importu, **bez CGO**. `exif.Extract(ctx, path)` vrací `exif.Metadata`
(mapuje se 1:1 na sloupce `internal/photos.Photo`): `TakenAt` + `TakenAtSource`
(`exif`/`filename`/`unknown`), `Lat`/`Lng`/`Altitude`, `CameraMake`/`CameraModel`/`LensModel`,
`ISO`/`Aperture`/`Exposure`/`FocalLength`, `Width`/`Height`/`Orientation`, `Mime` a plný EXIF
jako JSON-able mapa (`Exif`).

- **Primární cesta**: shell-out na `exiftool -json -n` (numerické hodnoty → deterministické
  parsování rozměrů, orientace a souřadnic). **Fallback** na čistě-Go parser
  (`rwcarlsen/goexif`), když `exiftool` není na PATH nebo selže — fallback čte i rozměry/MIME
  přes `image.DecodeConfig` + `http.DetectContentType`.
- **GPS**: EXIF rational souřadnice se převádějí na desetinné stupně, hemisféra dle
  `N/S/E/W` refů (jih/západ → záporné); `GPSAltitudeRef = 1` → záporná výška.
- **`taken_at`**: preferuje EXIF `DateTimeOriginal` (zóna-prosté časy jako UTC); když chybí,
  zkusí datum z **názvu souboru** (`IMG_20230115_143052`, `2023-01-15 14.30.52`, …); jinak
  `source = unknown`.
- **Tolerance**: soubor bez EXIF (např. PNG screenshot) **není chyba** — vrací nulové hodnoty,
  ne error. Error jen pro prázdnou cestu / nečitelný soubor.
- Runtime apt závislost (volitelná, jinak fallback): `libimage-exiftool-perl`.

### Perceptuální hashe (`internal/phash`)

Čistě-Go (bez CGO) výpočet dvou 64bitových perceptuálních hashů pro detekci **podobných**
(ne jen byte-identických) fotek: **pHash** přes 2-D DCT (32×32 → low-freq 8×8 blok, práh
medián bez DC) a **dHash** (gradientní, 9×8). `phash.Compute(img)` vrací `Hashes{Phash, Dhash int64}`,
`phash.Distance(a, b)` je Hammingova vzdálenost. Ukládá se do `photo_phashes`; menší vzdálenost
= vizuálně podobnější. Near-duplicate dotaz `photos.Store.NearestPhash(ctx, phash)` počítá
vzdálenost v Postgresu (`bit_count(phash::bit(64) # …)`).

### Upload / ingest pipeline (`internal/ingest`)

Endpoint **`POST /api/v1/upload`** (přístup editor/admin) přijímá `multipart/form-data` s jedním
či více soubory a **streamuje** je (nikdy nedrží celý soubor v RAM). Pipeline na soubor:

1. Stream do dočasného souboru + průběžný **SHA256**.
2. **Exact-dup** podle SHA256 (`photos.GetByFileHash`) → friendly per-file „duplicate".
3. Detekce formátu/MIME + extrakce EXIF/GPS, publikace originálu do úložiště (`YYYY/MM`).
4. Insert `photos` + primární `photo_files` + výpočet **pHash/dHash** → `photo_phashes`.
5. Generování **náhledů** (thumbnailer).
6. **Enqueue** jobů `image_embed` + `face_detect` přes `ingest.JobEnqueuer` — zatím
   `NopEnqueuer` (TODO hook, fronta vznikne v jobs tasku).

Vrací **per-file** seznam výsledků `{filename, status, outcome (created/duplicate/error),
photo_uid?, error?, warnings?}` se 409 sémantikou duplicit v `status` (celková odpověď je 200,
takže parciální dávky reportují čistě). **Race**: souběžné uploady identického obsahu konvergují
na jednu fotku (storage hard-link + unique constraint `file_hash`), poražený dostane čistou
duplicitu, ne 500. **Near-duplicate** (config-gated `duplicate.*`): pokud existuje velmi podobný
pHash, výsledek nese `warning` (neblokuje). Limit velikosti souboru: `upload.max_file_size_mb`
(0 = bez limitu).

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
| POST | `/upload` | editor/admin | `multipart/form-data` s jedním+ soubory → per-file `{outcome, photo_uid, warnings}` (viz Upload / ingest) |
| GET | `/photos` | přihlášený | seznam s filtry/řazením/stránkováním → `{photos,total,limit,offset,next_offset}` (viz Foto API) |
| GET | `/photos/{uid}` | přihlášený | plný detail fotky (metadata, EXIF, GPS) + `files` |
| PATCH | `/photos/{uid}` | editor/admin | částečná úprava `title/description/notes/taken_at/lat/lng/private` (null maže nullable pole) |
| POST | `/photos/{uid}/archive` | editor/admin | soft-delete (nastaví `archived_at`) → vrátí fotku |
| POST | `/photos/{uid}/unarchive` | editor/admin | obnoví archivovanou fotku |
| GET | `/photos/{uid}/thumb/{size}` | session/token | náhled (cache, generuje se on-miss) — streamuje JPEG, `ETag`/304 |
| GET | `/photos/{uid}/download` | session/token | originál jako příloha — streamuje (nikdy celý v RAM), `Content-Length`/`ETag` |

RBAC se vynucuje middlewarem (`RequireAuth` / `RequireWrite` / `RequireAdmin` /
`RequireAuthOrDownloadToken`). Konfigurační
klíče (`auth.session_ttl`, `auth.session_max_lifetime`, `auth.login_rate_limit`,
`auth.login_rate_window`, `web.secure_cookies`) popisuje [`config.example.yaml`](config.example.yaml).

### Foto API (`internal/photoapi`)

Prohlížení a kurace katalogu jsou v balíčku [`internal/photoapi`](internal/photoapi/) (HTTP
vrstva nad `internal/photos` + `internal/storage` + `internal/thumb`; guardy se injektují
z auth subsystému, takže balíček nezná jeho wiring). Endpointy montuje `buildPhotoAPI`
(`cmd/kukatko/photos.go`) třetím `server.WithAPI` v `serve`.

- **Seznam** `GET /photos` — query parametry zrcadlitelné do URL (FE „Zpět vždy funguje"):
  - **Filtry:** `taken_after`/`taken_before` (RFC3339 nebo `YYYY-MM-DD`), `has_gps` (`true`/`false`),
    `camera`, `lens`, `q` (fulltext title/description/notes), `private` (`true`/`false`),
    `uploader` (UID), `archived` (`false` výchozí = jen živé, `true` = včetně archivu, `only` =
    jen archiv).
  - **Řazení:** `sort` = `newest` (default) / `oldest` / `taken_at` / `added` / `title` / `size`,
    s volitelným `order=asc|desc`.
  - **Stránkování:** `limit` (default 100, max 500) + `offset`. Odpověď nese `total` a
    `next_offset` (null na poslední stránce) pro infinite scroll.
  - **Neplatný parametr → HTTP 400.**
- **Detail** `GET /photos/{uid}` — fotka + `files` (seznam `photo_files`), `404` když chybí.
- **Úprava** `PATCH /photos/{uid}` (editor/admin) — částečná: pole vynechané v těle se nemění,
  explicitní `null` maže nullable pole (`taken_at`/`lat`/`lng`); rozsah souřadnic se validuje
  (`lat ∈ ⟨-90,90⟩`, `lng ∈ ⟨-180,180⟩`).
- **Archivace** `POST /photos/{uid}/archive` + `/unarchive` (editor/admin) — soft-delete přes
  `archived_at`; archivované jsou z výchozího seznamu vyloučené.
- **Média** `GET /photos/{uid}/thumb/{size}` a `/download` — **streamují** se (`io.Copy`, nikdy
  celý soubor v RAM), s `Cache-Control`/`ETag` (a `304` na `If-None-Match`). Přístup přes session
  cookie **nebo** `download_token` v query parametru `?t=…` (`RequireAuthOrDownloadToken`), takže
  fungují i `<img>`/`<video>` bez cookie. Náhled se na cache-miss vygeneruje on-demand; download
  posílá originál jako `attachment` s `Content-Length` z DB.

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

## CI a release (balíčkování)

**CI** ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) běží na push/PR do `main`:

- **`check`** — Go 1.26 + Node 22 (+ golangci-lint v2.11.4), spustí brzdu kvality `make check`
  (gofmt + vet + golangci-lint + Go unit testy + frontend ESLint/Prettier/Vitest).
- **`integration`** — `make test-integration` proti **service containeru
  `pgvector/pgvector:pg17`**; setup krok vytvoří rozšíření `vector` a `unaccent`,
  `KUKATKO_TEST_DATABASE_URL` míří na efemérní CI databázi (žádné tajemství v logu).

Cache: Go modul/build cache (`actions/setup-go`) a npm (`actions/setup-node`,
`web/package-lock.json`).

**Release** ([`.github/workflows/release.yml`](.github/workflows/release.yml)) se spustí na tagu
`v*.*.*` a pustí **goreleaser** ([`.goreleaser.yaml`](.goreleaser.yaml)): build `CGO_ENABLED=0`
pro **arm64** (Raspberry Pi, produkce) i **amd64** (dev), verze/commit přes ldflags do
`internal/version`, frontend se buildí v before-hooku, takže embedovaná SPA je aktuální. Lokální
ověření celé pipeline: `goreleaser release --snapshot --clean`.

**.deb balíček** (nfpm v goreleaseru) instaluje:

- binár do `/usr/bin/kukatko`,
- **systemd unit** [`deb/kukatko.service`](deb/kukatko.service) do `/lib/systemd/system/`
  (`kukatko serve`, `Restart=always`, `EnvironmentFile=/etc/kukatko/kukatko.env`, dedikovaný
  uživatel `kukatko`),
- env-file šablonu [`deb/kukatko.env`](deb/kukatko.env) do `/etc/kukatko/kukatko.env` jako dpkg
  **conffile** (`config|noreplace` — operátorské úpravy přežijí upgrade),
- postinstall ([`deb/postinstall.sh`](deb/postinstall.sh)) založí systémového uživatele a datové
  adresáře `/var/lib/kukatko/{originals,cache}`.

Apt závislosti: `libimage-exiftool-perl` (exiftool), `libheif-examples | libheif-bin`
(`heif-convert`), `dcraw` (RAW preview), `postgresql-client`, `ca-certificates`. **Bez texlive**
(fotokniha je mimo rozsah).

Více v [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) (layout, make cíle, brána kvality).
