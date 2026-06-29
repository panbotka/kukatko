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
./bin/kukatko migrate photosorter         # read-only inkrementální migrace dat z photo-sorteru
./bin/kukatko import photoprism           # read-only inkrementální import z PhotoPrismu
./bin/kukatko backup                      # jednorázová záloha (pg_dump + sync originálů) na S3
./bin/kukatko restore list                # vypíše dumpy dostupné v bucketu (nejnovější první)
./bin/kukatko restore db --yes            # obnoví DB z nejnovějšího dumpu (DESTRUKTIVNÍ) + migrace
./bin/kukatko restore originals           # stáhne chybějící originály z S3 (přeskočí existující)
./bin/kukatko restore verify              # integritní report: fotky v DB vs originály na disku
./bin/kukatko maintenance scan            # integritní kontrola knihovny (disk↔DB drift, odvozená data)
./bin/kukatko maintenance repair --thumbnails --phashes  # opt-in opravy (náhledy/hashe/embeddingy/faces/orphans)
./bin/kukatko serve                       # spustí migrace, pak HTTP server (default 0.0.0.0:8080)
./bin/kukatko serve --config config.yaml  # explicitní cesta ke konfiguraci
./bin/kukatko version                     # vypíše verzi a commit
```

`migrate photosorter` potřebuje read-only DSN photo-sorter DB v `import.photosorter.dsn`
(`KUKATKO_IMPORT_PHOTOSORTER_DSN`); bez něj příkaz i jeho admin trigger
`POST /api/v1/import/photosorter` selžou/se neregistrují.

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
  **Video** (migrace `0004_video.sql`): `media_type` (`image`/`video`/`live`, default `image`,
  CHECK + partial index) + `duration_ms`, `video_codec`, `audio_codec`, `has_audio`, `fps`
  (vyplněné jen u videí). Live foto = still jako primární image + motion klip jako další
  `photo_files` řádek.
  **Fulltext** (migrace `0007_fts.sql`): generovaný sloupec `fts tsvector` (GIN index) =
  `setweight` nad `to_tsvector('simple', immutable_unaccent(...))` z title (A) > description
  (B) > notes (C) > normalizovaný file_name (D); diakritika necitlivá přes IMMUTABLE
  `immutable_unaccent` wrapper. `GENERATED ALWAYS … STORED` drží `fts` aktuální při každém
  insertu/úpravě metadat bez triggeru.
- **`photo_files`** — originál + odvozeniny, `role` original/sidecar/edited, max. jeden
  `is_primary` na fotku. **`photo_phashes`** — `phash`/`dhash` (near-dup). **`photo_edits`**
  — nedestruktivní úpravy (crop 0..1 all-or-nothing, rotace 0/90/180/270, brightness/contrast).
  Satelitní tabulky mají FK `ON DELETE CASCADE`.

`photos.Store` (nad pgx poolem) nabízí `Create`, `GetByUID`/`GetByFileHash`/
`GetByPhotoprismUID`/`GetByPhotosorterUID`, `UpdateMetadata`, `Archive`/`Unarchive`,
`Delete`, `List`/`Count` (filtr archived/private/uploader, řazení, stránkování),
`Search` (fulltext nad `fts` sloupcem, řazení dle `ts_rank`, ctí list filtry +
stránkování; prázdný dotaz → `ErrEmptySearch`), `FilterUIDs` (z množiny uid vrátí ty,
co projdou strukturálními list filtry — pro sémantické hledání: profiltruje vektorové
kandidáty) a metody pro soubory/phash/edits.

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
- **HEIC/RAW/video** (`internal/imgconvert`): `EnsureDecodable(ctx, path)` vrátí cestu k souboru,
  který umí `image.Decode`. JPEG/PNG/WebP projdou beze změny; **HEIC** se převede přes
  `heif-convert` na dočasný JPEG; **RAW** (cr2/cr3/nef/arw/dng/raf/orf/rw2/pef/srw) vytáhne
  **embedded JPEG preview** přes `exiftool -b -PreviewImage` (fallback `-JpgFromRaw`/
  `-ThumbnailImage`) místo plného demosaicu; **video** (mp4/mov/m4v/avi/mkv/webm/…) deleguje na
  `internal/video.ExtractPoster` (poster frame přes `ffmpeg`). Díky tomu thumbnailer i pHash
  zpracují video poster úplně stejně jako fotku. Chybějící externí nástroj vrací jasný
  `ErrConverterMissing` (resp. `video.ErrFFmpegMissing`). Runtime apt závislosti:
  `libheif-examples`/`libheif-bin`, `libimage-exiftool-perl`, `ffmpeg`.

### Video (`internal/video`)

CGO-free shell-out na **FFmpeg suite** (`ffprobe`/`ffmpeg`):

- **`Probe(ctx, path)`** — metadata přes `ffprobe -print_format json -show_format -show_streams`:
  `duration_ms`, video/audio kodeky, `has_audio`, `fps` (parsing racionálu `30000/1001`),
  rozměry, `creation_time` (→ `taken_at`), GPS (ISO 6709 `+lat+lng+alt/`). **Fallback na
  `exiftool`** (přes `internal/exif`) když `ffprobe` chybí; celý probe dokument se ukládá do
  `photos.exif`.
- **`ExtractPoster(ctx, path)`** — reprezentativní snímek přes `ffmpeg` (~1 s do klipu, fallback
  na první frame u kratších) do dočasného JPEG + once-only cleanup.
- **`IsVideoPath` / `FFmpegAvailable` / `FFprobeAvailable`** + sentinely `ErrFFmpegMissing`/
  `ErrFFprobeMissing`/`ErrNoMetadataTool`/`ErrPosterFailed`.

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
3. Detekce typu média podle přípony (`video.IsVideoPath`). **Foto** → EXIF/GPS (`internal/exif`);
   **video** → `media_type=video` + probe (`internal/video.Probe`: duration/kodeky/fps/GPS/čas),
   `taken_at` fallback na původní jméno souboru. Pak publikace originálu do úložiště (`YYYY/MM`).
4. Insert `photos` (vč. video sloupců) + primární `photo_files` + výpočet **pHash/dHash** →
   `photo_phashes` (u videa z poster framu).
5. Generování **náhledů** (thumbnailer) — u videa z poster framu, takže grid ukazuje poster.
6. **Enqueue** jobů `image_embed` + `face_detect` přes `ingest.JobEnqueuer` — `serve` injektuje
   perzistentní `jobs.Enqueuer` (viz [fronta jobů](#persistentní-fronta-jobů-internaljobs)), takže
   nová fotka hned dostane embedding/face joby; v testech bez fronty se použije `NopEnqueuer`.
   U videa běží na poster framu, takže se účastní sémantického/face vyhledávání.

Video vyžaduje **`ffmpeg`** (poster nemá fallback) — chybějící `ffmpeg` u video uploadu vrací
jasný per-file error `video.ErrFFmpegMissing`. `ffprobe` má fallback na `exiftool`.

Vrací **per-file** seznam výsledků `{filename, status, outcome (created/duplicate/error),
photo_uid?, error?, warnings?}` se 409 sémantikou duplicit v `status` (celková odpověď je 200,
takže parciální dávky reportují čistě). **Race**: souběžné uploady identického obsahu konvergují
na jednu fotku (storage hard-link + unique constraint `file_hash`), poražený dostane čistou
duplicitu, ne 500. **Near-duplicate** (config-gated `duplicate.*`): pokud existuje velmi podobný
pHash, výsledek nese `warning` (neblokuje). Limit velikosti souboru: `upload.max_file_size_mb`
(0 = bez limitu).

### Persistentní fronta jobů (`internal/jobs`)

Durable, Postgresem podložená fronta (migrace `0005_jobs.sql`, tabulka `jobs`) — hlavní
robustnostní vylepšení proti photo-sorteru, jehož in-memory joby se ztrácely při restartu. Joby
přežijí restart, retryují s exponenciálním backoffem, dedupují se podle fotky a čekají v `queued`,
když je embeddings box offline (upload a prohlížení fungují bez něj).

- **Tabulka `jobs`**: `id BIGSERIAL`, `type`, `state` (`queued`/`running`/`done`/`failed`/`dead`),
  `priority`, `payload` JSONB, `attempts`/`max_attempts` (default 5), `last_error`, `run_after`
  (backoff/odložení), `locked_by`/`locked_at`, `created_at`/`updated_at`. Index na
  `(state, run_after, priority)`; **dedup** = partial unique index na
  `(type, payload->>'photo_uid') WHERE state IN ('queued','running')` (NULL photo_uid se nededupuje,
  takže joby bez fotky — např. `backup` — nekolidují).
- **`Store`** (`jobs.NewStore(pool)`):
  - `Enqueue(ctx, type, payload, opts)` — idempotentní vůči dedup klíči; aktivní duplikát vrací
    `ErrDuplicate`. `EnqueueOptions{Priority, MaxAttempts, RunAfter}`.
  - `Claim(ctx, workerID, types...)` — atomicky vezme další běhuschopný job přes
    `SELECT … FOR UPDATE SKIP LOCKED` (`run_after <= now()`, řazení `priority DESC, run_after ASC,
    id ASC`), označí `running` + `locked_by`/`locked_at`. Prázdná fronta → `ErrNoJobs`. Souběžní
    workeři nikdy nedostanou stejný job.
  - `Complete(id)` / `Fail(id, err)` — `Fail` inkrementuje `attempts`; dokud `attempts < max_attempts`
    requeue s exponenciálním backoffem přes `run_after` (base 30 s, cap 1 h), jinak `state=dead` +
    `last_error` (dead-letter).
  - `Defer(id, delay)` — requeue běžícího jobu na `now()+delay` **bez** započtení pokusu (atributy
    `attempts` se nemění, zámek se uvolní). Pro přechodné, bezchybné stavy — hlavně když je
    embeddings box offline — takže job počká ve frontě na návrat boxu, aniž by vyčerpal retry budget.
  - `Heartbeat(id, workerID)` + `RecoverStaleLocks(staleAfter)` — běžící joby se zastaralým zámkem
    (mrtvý worker) se requeují (počítá se jako pokus); heartbeat zámek osvěží a chrání před recovery.
  - Helpery: `CountsByState` / `CountsByType`, `ListDead`, `RequeueDead`, `Requeue` (dead **i**
    failed → queued), `List(ListOptions{State,Limit,Offset})` (recent výpis, řazení
    `updated_at DESC`, limit cap 500), `Get`.
- **`jobs.Enqueuer`** (`NewEnqueuer(store)`) implementuje `ingest.JobEnqueuer`
  (`EnqueueImageEmbed`/`EnqueueFaceDetect` s payloadem `{"photo_uid": …}`, `ErrDuplicate` =
  no-op) — wiring fronty do uploadu.

### Background worker (`internal/worker`)

Exekuční smyčka, která frontu drénuje, běží **v procesu `kukatko serve`**:

- **`Registry`** (`NewRegistry()`) mapuje `type` → `HandlerFunc` (`func(ctx, jobs.Job) error`)
  přes `Register(type, fn)`; panika na prázdný typ, nil handler nebo duplicitní registraci
  (programátorská chyba při startu). Built-in **noop** handler (`TypeNoop`, `RegisterBuiltins`)
  jen pro sanity/testy; reálný handler `image_embed` registruje `embedjob.Service.Handle`
  (viz níže), `face_detect` a další doplní pozdější milníky.
- **`RetryAfter(delay, cause)` / `RetryAfterError`** — handler jím signalizuje „přechodná chyba,
  zopakuj později bez započtení pokusu". Worker takový výsledek pozná (`errors.As`) a místo `Fail`
  zavolá frontové `Defer(delay)`. Používá ho `image_embed`, když je box offline.
- **`Worker`** (`New(Config{Queue, Registry, Concurrency, PollInterval, StaleAfter,
  StaleScanInterval, IDPrefix})`) — `Run(ctx)` spustí `Concurrency` goroutin, které pollují
  `Claim` (filtr na registrované `Types`), dispatchnou job na handler dle `job.Type` a podle
  výsledku zavolají `Complete`/`Fail`. Bookkeeping (`Complete`/`Fail`) běží přes
  **shutdown-immune** kontext (`context.WithoutCancel`), takže výsledek dopočítaný těsně před
  vypnutím se ještě uloží. Vedle workerů běží **stale-lock recovery** ticker.
- **Graceful shutdown**: zrušení `ctx` (SIGINT/SIGTERM) zastaví claiming; job běžící při
  vypnutí je **opuštěn** (jeho zámek později requeue fronta přes `RecoverStaleLocks`), `Run`
  se čistě vrátí. Panika handleru → `ErrHandlerPanic` (job se failne, worker nespadne),
  neznámý typ jobu → `ErrNoHandler`.
- **`Queue`** je interface = podmnožina `jobs.Store` (`Claim`/`Complete`/`Fail`/`Defer`/
  `RecoverStaleLocks`), takže runtime jde unit-testovat s fakem.
- Tuning přes `worker.*` config (`count`, `poll_interval`, `stale_after`,
  `stale_scan_interval`).

### Wake-on-LAN auto-wake boxu (`internal/wake`)

Volitelně **probudí GPU box přes Wake-on-LAN**, když ve frontě čekají embeddingové joby a sidecar
je offline, takže se fronta dožene bez ručního zapnutí boxu. **Defaultně vypnuto** a plně inertní.

- **Trigger:** `Run(ctx, interval)` běží ve vlastní goroutině v `serve` (každou minutu) a pošle
  magic packet **jen** když je `embedding.wake.enabled`, počet čekajících (`queued`/`running`)
  `image_embed`/`face_detect` jobů dosáhne `min_queue`, **uplynul cooldown** a health check
  sidecaru hlásí **offline**. Po `GracePeriod` (30 s) překontroluje zdraví a zaloguje, jestli box
  naběhl; jinak se backoffne do dalšího cooldownu. Smyčka **nikdy neblokuje zpracování jobů** a
  chyby jen loguje.
- **Síť:** Wake-on-LAN **nefunguje přes Tailscale** (L3 overlay bez L2 broadcastu) — host musí být
  na stejné fyzické LAN jako box. Default je UDP broadcast na `broadcast_addr` (knihovna
  `mdlayher/wol`), volitelně raw Ethernet rámec na `interface` (vyžaduje CAP_NET_RAW). Uspávání
  boxu je mimo rozsah.
- **Testovatelnost:** `QueueDepth`/`HealthChecker`/`Sender` jsou rozhraní → unit testy běží s
  fakeem sendera, **žádný reálný síťový provoz**; `Packet(mac)` staví magic packet (102 B) a je
  testovaný samostatně.
- Konfig `embedding.wake.{enabled,mac,broadcast_addr,interface,min_queue,cooldown}`; enabled
  vyžaduje validní `mac` (jinak `ErrInvalidWake` při startu).

### Admin Jobs API (`internal/jobsapi`)

Admin-only HTTP API nad frontou (guard `RequireAdmin`), frontend ho polluje (žádné SSE):

- `GET /api/v1/jobs/stats` → `{by_state, by_type, total}` (agregované counts pro dashboard).
- `GET /api/v1/jobs` → `{jobs, limit, offset}` (recent / dead-letter výpis; query `state`,
  `limit` ≤ 500, `offset`; neplatný parametr → 400).
- `POST /api/v1/jobs/{id}/requeue` → refreshnutý job (dead/failed → `queued`; 404 missing,
  409 ne-requeueable).

### Embeddings sidecar client (`internal/embedding`)

HTTP klient k inferenční službě (sidecar na **boxu** — GPU stroj, který bývá offline). Stejný
kontrakt jako photo-sorter; vše za rozhraním `Client` (v testech fakeovatelné, žádná reálná síť):

- `ImageEmbedding(ctx, img io.Reader)` → `POST {url}/embed/image` (multipart pole `file`,
  streamuje se přes `io.Pipe`) → 768-dim CLIP vektor + `model`/`pretrained`.
- `TextEmbedding(ctx, text)` → `POST {url}/embed/text` (JSON `{text}`) → 768-dim vektor ve
  sdíleném prostoru s obrázky.
- `FaceEmbeddings(ctx, img io.Reader)` → `POST {url}/embed/face` (multipart `file`) → `[]Face`
  (512-dim embedding, `bbox` v pixelech `[x1,y1,x2,y2]`, `det_score`) + `model`.
- `Healthy(ctx)` → levný probe na `{url}/health`: jakákoli HTTP odpověď = box dostupný, jen
  transport-error/timeout = offline.

**Box offline-aware:** typové chyby rozlišují **`ErrUnavailable`** (transport selhal nebo
status 502/503/504 — retryable, helper `IsUnavailable`) od **`ErrBadResponse`** (chybná
odpověď) a **`ErrDimMismatch`** (špatný rozměr vektoru, validace 768/512). Zrušený kontext se
nevydává za nedostupnost. Base URL z `embedding.url`, rozměry z `embedding.image_dim`/`face_dim`;
timeouty mají rozumné defaulty (request 60 s, health 5 s). Joby `image_embed`/`face_detect`
přes tohle čekají ve frontě, dokud box nenaběhne.

### Embeddings & faces schema (`internal/vectors`)

Embeddingy se ukládají **přímo do PostgreSQL** jako `halfvec` (float16) sloupce s **HNSW
cosine** indexy (migrace `0006_embeddings.sql`) — žádný externí vektorový store, podobnostní
hledání je obyčejný SQL dotaz přes operátor `<=>`. `halfvec` místo `vector` (float32) **půlí
paměť HNSW indexu** s zanedbatelnou ztrátou recall na normalizovaných CLIP/ArcFace vektorech,
což je na Pi podstatné.

- **`embeddings`**: jeden CLIP obrázkový embedding na fotku (`photo_uid` PK FK `ON DELETE
  CASCADE`, `embedding halfvec(768)`, `model`/`pretrained`/`dim`/`created_at`), HNSW index
  `hnsw (embedding halfvec_cosine_ops) WITH (m=16, ef_construction=200)`.
- **`faces`**: nula a více detekovaných obličejů na fotku (`id` BIGSERIAL, `photo_uid` FK
  `ON DELETE CASCADE`, `face_index`, `embedding halfvec(512)`, `bbox DOUBLE PRECISION[4]`
  normalizovaný `[x,y,w,h]` 0..1, `det_score`, `model`/`dim`/`created_at` + cache sloupce
  `marker_uid`/`subject_uid`/`subject_name`/`photo_width`/`photo_height`/`orientation`),
  `UNIQUE(photo_uid, face_index)` + HNSW index na `embedding`. FK `ON DELETE CASCADE` opravuje
  mezeru photo-sorteru, kde embeddingy/faces neměly FK a vznikali sirotci.
- **`face_detections`** (migrace `0009_face_detections.sql`): jeden řádek na fotku, která prošla
  detekcí obličejů (`photo_uid` PK FK `ON DELETE CASCADE`, `face_count`, `model`, `detected_at`).
  Protože `faces` může mít nula řádků, je tahle tabulka jediný způsob, jak odlišit fotku **bez
  obličejů** od fotky **dosud nezpracované** — drží idempotenci `face_detect` jobu i backfill.

`vectors.Store` (`NewStore(pool)`) nad sdíleným pgx poolem:
`SaveEmbedding`/`GetEmbedding` (`ErrEmbeddingNotFound`), `FindSimilar(vec, limit, maxDistance)`
nad `embedding <=> $vec` (nejbližší první), `SaveFaces`(idempotentní replace v transakci)/
`ListFaces`/`DeleteFaces`, `FindSimilarFaces`,
`FindSimilarFaceCandidates(vec, limit, maxDistance)` (jako `FindSimilarFaces`, ale vrací i cache
`subject_uid`/`subject_name`/`marker_uid` a `bbox` — podklad pro návrhy identit),
`UpdateFaceMarker(photoUID, faceIndex, markerUID, subjectUID, subjectName)` (zapíše cache sloupce
na jeden obličej; prázdný `markerUID`/`subjectUID` → `NULL` — tudy se cachuje IoU match a linkuje
obličej k markeru), `RecordFaceDetection(uid, faces, model)` (atomicky nahradí faces fotky **a** zapíše
`face_detections` řádek — i pro nula obličejů), `FacesDetected(uid)` (existuje `face_detections`
řádek?), `ListPhotosMissingFaces(limit)`. Dotazy běží v **read-only transakci** se
`SET LOCAL hnsw.ef_search = 100` pro lepší recall; `limit` se ořezává do `[1,500]`,
nekladný `maxDistance` filtr vypne. Helpery `ToHalfVec`/`FromHalfVec` (`[]float32` ↔
`pgvector.HalfVector`), validace rozměrů přes `ErrDimMismatch`, duplicitní `face_index` →
`ErrFaceIndexTaken`. `ListPhotosMissingEmbedding(limit)` vrací uid nearchivovaných fotek bez
embeddingu (LEFT JOIN na `embeddings`, nejnovější první; `limit <= 0` = všechny) a
`ListPhotosMissingFaces(limit)` analogicky uid fotek bez `face_detections` řádku — podklady pro
backfill.

### Subjekty & markery (`internal/people`)

Pojmenované **subjekty** (osoby / zvířata / jiné) a **markery** (face/label regiony na fotkách)
v migraci `0008_subjects_markers.sql`:

- **`subjects`**: `uid` PK (prefix `su`), `slug` UNIQUE (generovaný z `name`, bez diakritiky a
  unikátní díky číselnému sufixu), `name`, `type IN (person|pet|other)`, `favorite`, `private`,
  `notes`, `cover_photo_uid` (FK photos `ON DELETE SET NULL` — subjekt přežije smazání cover
  fotky), `created_at`/`updated_at`.
- **`markers`**: `uid` PK (prefix `mk`), `photo_uid` (FK photos `ON DELETE CASCADE`),
  `subject_uid` (FK subjects `ON DELETE SET NULL`), `type IN (face|label)`, normalizovaný bbox
  `x,y,w,h DOUBLE PRECISION` (0..1 display space, stejná konvence jako `faces.bbox`), `score`,
  `invalid`, `reviewed`, časy; indexy na `photo_uid` a `subject_uid`.

`people.Store` (`NewStore(pool)`) nad sdíleným pgx poolem:

- **Subjekty:** `CreateSubject` (generuje uid + **unikátní slug z jména** přes `Slugify`,
  kolize → `name-2`/`name-3`/…), `GetSubjectByUID`/`GetSubjectBySlug`, `UpdateSubject`
  (přeslugování při změně jména + refresh cache `faces.subject_name`), `ListSubjects`
  (subjekty s počtem **non-invalid** markerů, řazení dle jména), `DeleteSubject` (FK odpojí
  markery na `NULL`, vyčistí faces cache).
- **Markery:** `CreateMarker` (validace typu a `0..1` bounds → `ErrInvalidType`/
  `ErrInvalidBounds`; volitelně rovnou se subjektem), `GetMarkerByUID`, `ListMarkersByPhoto`,
  `AssignSubject`/`UnassignSubject`, `SetMarkerInvalid`/`SetMarkerReviewed`, `DeleteMarker`.

**Konzistence denormalizovaného faces cache:** `faces` (migrace 0006) drží cache sloupce
`marker_uid`/`subject_uid`/`subject_name` kvůli rychlému renderu. `people` je udržuje v
synchronizaci — každá změna markeru/subjektu (assign/unassign, rename subjektu, mazání markeru
i subjektu) aktualizuje odpovídající `faces` řádky **ve stejné transakci** (`WHERE marker_uid =
$1`, resp. `WHERE subject_uid = $1`). Sentinely: `ErrSubjectNotFound`/`ErrMarkerNotFound`/
`ErrSlugExhausted`/`ErrInvalidType`/`ErrInvalidBounds`.

### Face↔marker matching, přiřazování & návrhy (`internal/facematch`)

Propojuje detekované obličeje s markery/subjekty a navrhuje pravděpodobné identity. Vše za
rozhraními (`PhotoStore`/`FaceStore`/`PeopleStore`), takže se unit-testuje s faky bez DB.

- **IoU geometrie** (`IoU(a, b [4]float64)`, čistá funkce): Intersection-over-Union dvou
  normalizovaných boxů `[x,y,w,h]` (0..1). `findBestMarker` vybere nejpřekrývající se **face**
  marker (ignoruje `invalid`), match platí při `IoU ≥ faces.iou_threshold` (default 0.1, mirror
  photo-sorteru).
- **`PhotoFaces(ctx, photoUID)`** (backing `GET /photos/{uid}/faces`): pro každý uložený obličej
  spočítá nejlepší marker dle IoU, určí akci (`create_marker` / `assign_person` / `already_done`),
  **zacachuje match na řádek obličeje** (`UpdateFaceMarker`) a pro nepojmenované obličeje přidá
  návrhy. Markery bez odpovídajícího obličeje se připojí (záporné `face_index`) pro detail UI.
- **Návrhy** (`aggregateSuggestions`, čistá funkce): z nejbližších face embeddingů
  (`FindSimilarFaceCandidates`, HNSW cosine) agreguje kandidáty dle subjektu, vyloučí obličeje na
  stejné fotce, subjekty **už přiřazené na fotce** (jiné osoby) a obličeje **pod minimální
  velikostí** (`faces.min_face_size`), seřadí dle průměrné vzdálenosti, `confidence = 1 −
  distance`, limit `faces.suggestion_limit` (~5). Primární práh `faces.suggestion_max_distance`,
  s **fallbackem** na neomezenou vzdálenost když je návrhů málo.
- **Přiřazování (state machine)** (`Apply(ctx, AssignRequest)`, backing
  `POST /photos/{uid}/faces/assign`, editor/admin): `create_marker` (vytvoří face marker + přiřadí
  subjekt + zalinkuje obličej), `assign_person` (přiřadí subjekt existujícímu markeru),
  `unassign_person` (odpojí subjekt). Drží `faces` cache i `marker.reviewed` konzistentní
  (assign → `reviewed=true`, unassign → `reviewed=false`). **Auto-create subjektu dle jména**
  (find-or-create přes `Slugify` + `GetSubjectBySlug`). Sentinely `ErrInvalidAction`/
  `ErrMissingBBox`/`ErrMissingMarker`/`ErrMissingSubject`; chybějící foto/marker/subjekt se mapuje
  na 404.

### Auto-clustering obličejů (`internal/cluster` + `internal/clusterapi`)

Seskupuje **dosud nepřiřazené obličeje** (bez subjektu) do shluků téže osoby, aby šel celý shluk
pojmenovat jedním tahem — klíčové zlepšení UX oproti photo-sorteru, kde se obličeje pojmenovávaly
po jednom. Tabulka `face_clusters` (migrace `0010_face_clusters.sql`: `uid` PK prefix `fc`,
`centroid halfvec(512)`, `size`, `model`, časy) + cache sloupec `faces.cluster_uid` (FK
`ON DELETE SET NULL`).

- **Algoritmus** (`internal/cluster`, čisté funkce v `algo.go`/`suggest.go` jsou unit-testované):
  greedy **souvislé komponenty** (union-find) nad HNSW nejbližšími sousedy každého clusterovatelného
  obličeje do **prahu cosine vzdálenosti** (`cluster.threshold`, default 0.4). Hrana = dva obličeje
  blíž než práh; každá komponenta o velikosti `≥ cluster.min_size` (default 2) se stane shlukem,
  menší zůstanou nesclustrované. Pro každý shluk se spočítá L2-normalizovaný **centroid**
  (průměr embeddingů) — slouží k výběru reprezentativního obličeje a k návrhu existujícího subjektu.
- **Inkrementální a re-spustitelné** (`Recluster(ctx)`): clusterovatelný je jen obličej **bez
  subjektu** (`subject_uid IS NULL`) **a bez shluku** (`cluster_uid IS NULL`), takže re-clustering
  nikdy nesáhne na přiřazené ani na už sclustrované obličeje — seskupí jen čerstvé nepřiřazené.
  Deterministické pro danou množinu obličejů.
- **`ListClusters(ctx)`** (backing `GET /faces/clusters`): pro každý shluk vrátí velikost,
  reprezentativní obličej (nejblíž centroidu), pár příkladů a **návrh existujícího subjektu** —
  nejbližší **už pojmenovaný** centroid (`FindSimilarFaceCandidates` nad centroidem, agregace dle
  subjektu, `confidence = 1 − distance`). Návrh je `null`, když žádný pojmenovaný soused není dost
  blízko (`cluster.suggestion_max_distance`, default 0.5).
- **`AssignCluster(ctx, req)`** (backing `POST /faces/clusters/{id}/assign`, editor/admin): přiřadí
  **všechny** obličeje shluku jednomu subjektu (dle `subject_uid`, jinak find-or-create dle
  `subject_name`) — pro každý obličej vytvoří face marker přes **sdílenou facematch state machine**
  (žádná duplikace logiky vytváření markerů), pak spotřebovaný shluk smaže (FK uvolní `cluster_uid`).
- **`RemoveFace(ctx, clusterUID, ref)`** (backing `POST /faces/clusters/{id}/remove-face`,
  editor/admin): odpojí zatoulaný obličej ze shluku **před** pojmenováním (aby nezašpinil jméno),
  přepočítá centroid/velikost; když shluk osiří, smaže ho. Vrací refreshnutý view (nebo `deleted`).
- **HTTP vrstva** (`internal/clusterapi`): `Service` rozhraní (splňuje ho `cluster.Service`),
  `NewAPI(Config{Service, RequireWrite})` + `RegisterRoutes` mountuje `/faces/clusters`; 503 když
  backend není zapojen, 400/404/409 dle sentinelů. Admin trigger re-clusteringu je
  `POST /api/v1/process/clusters` (viz Process API). Tunables v `cluster.*` configu.

### Outlier detekce obličejů (`internal/outliers` + `internal/outlierapi`)

Pro danou osobu odhalí pravděpodobně **špatně přiřazené obličeje** seřazením podle vzdálenosti
od centroidu jejích embeddingů (mirror photo-sorteru). Vše za rozhraními (`FaceStore` =
podmnožina `vectors.Store`, `PeopleStore` = podmnožina `people.Store`), takže se unit-testuje
s faky bez DB.

- **`Outliers(ctx, subjectUID)`** (backing `GET /subjects/{uid}/outliers`): ověří existenci
  subjektu (`people.ErrSubjectNotFound` → 404), načte všechny obličeje s `subject_uid =
  subjectUID` (`vectors.ListFacesBySubject`), spočítá **centroid** (element-wise průměr,
  L2-normalizovaný) přes sdílené `vectors.Centroid`, ohodnotí každý obličej **kosinovou
  vzdáleností** od centroidu (`vectors.CosineDistance`) a vrátí je seřazené **sestupně**
  (nejpodezřelejší první; tie-break dle `photo_uid`/`face_index` pro determinismus).
- Odpověď = `{subject_uid, count, meaningful, faces:[{photo_uid, face_index, bbox, det_score,
  distance, marker_uid?, width, height, orientation}]}`. UI z `faces` vyrenderuje ořez náhledu
  a špatný obličej **odpojí přes existující assign API** (`POST /photos/{uid}/faces/assign`
  s `unassign_person`) — tahle vrstva žádnou mutaci nepřidává.
- **Malé množiny:** 1–2 obličeje → `meaningful: false` (z tak mála se žádný obličej nevyčlení),
  obličeje se přesto vrátí seřazené.
- **HTTP vrstva** (`internal/outlierapi`): `Service` rozhraní (splňuje ho `outliers.Service`),
  `NewAPI(Config{Service, RequireWrite})` + `RegisterRoutes` mountuje `GET /subjects/{uid}/
  outliers` za `RequireWrite` (editor/admin); 503 bez backendu, 404 chybějící subjekt.
- Sdílená vektorová matematika `vectors.Centroid`/`vectors.Normalize`/`vectors.CosineDistance`
  (v `internal/vectors/math.go`) je jediná implementace — `internal/cluster` ji znovupoužívá.

### Subjekty / People API (`internal/peopleapi`)

Read/curační HTTP API nad subjekty (osoby/zvířata/jiné) — backend pro **People UI**. Stojí na
rozhraních (`SubjectStore` = podmnožina `people.Store`, `PhotoStore` = `photos.Store.ListByUIDs`),
takže se unit-testuje s faky bez DB.

- `GET /subjects` (RequireAuth) → `{subjects:[{...subject, marker_count}]}` (řazení dle jména).
- `POST /subjects` (RequireWrite) → 201 vytvoří subjekt z `{name, type, favorite, private, notes,
  cover_photo_uid?}`; tělo přes `DisallowUnknownFields` + 1 MiB limit, prázdné jméno/neznámý typ → 400.
- `GET /subjects/{uid}` (RequireAuth) → subjekt (404 chybějící).
- `PATCH /subjects/{uid}` (RequireWrite) → editace `name/type/favorite/private/notes/cover_photo_uid`.
- `DELETE /subjects/{uid}` (RequireWrite) → 204 (markery se odpojí přes FK).
- `GET /subjects/{uid}/photos` (RequireAuth) → paginovaná galerie fotek subjektu
  `{photos, total, limit, offset, next_offset}` (newest-first, jen nearchivované, `limit` ≤ 500).
  Staví na `people.Store.ListPhotoUIDsBySubject` (distinct photo uid z non-invalid markerů) →
  ořez stránky → `photos.Store.ListByUIDs` → reorder dle uid pořadí.
- **Cesty jsou ploché** (ne `chi.Route`/Mount), aby koexistovaly s `outlierapi`
  `GET /subjects/{uid}/outliers` na témže routeru. Mountuje se osmým `server.WithAPI`
  (`buildPeopleAPI` v `cmd/kukatko/people.go`).

### Alba, štítky & oblíbené (`internal/organize`)

Organizační schéma nad katalogem (migrace `0011_albums_labels_favorites.sql`, balíček
`internal/organize` se `Store` nad sdíleným pgx poolem). Oblíbené jsou v Kukátku **per-user**
(nahrazují globální `photos.favorite` z photo-sorteru).

- **`albums`** — PK `uid` (prefix `al`), `slug` UNIQUE (z `title`, Slugify + číselný sufix),
  `title`/`description`, `type` CHECK (`album`/`folder`/`moment`/`state`/`month`),
  `cover_photo_uid` (FK `photos` `ON DELETE SET NULL`), `private`, `order_by` (free-text řazení
  galerie, default `added`), `created_by` (FK `users` `ON DELETE SET NULL`), časy.
  **`album_photos`** — členství: PK `(album_uid, photo_uid)`, oba FK `ON DELETE CASCADE`,
  `sort_order` (manuální pořadí), `added_at`.
- **`labels`** — PK `uid` (prefix `lb`), `slug` UNIQUE (z `name`), `name`, `priority` (řazení
  v UI), časy. **`photo_labels`** — připojení: PK `(photo_uid, label_uid)`, oba FK
  `ON DELETE CASCADE`, `source` CHECK (`manual`/`ai`/`import`), `uncertainty` (int %), `created_at`.
- **`user_favorites`** — per-user oblíbené: PK `(user_uid, photo_uid)`, oba FK
  `ON DELETE CASCADE`, `added_at`.

`organize.Store` API:

- **Alba** — `CreateAlbum`/`GetAlbumByUID`/`GetAlbumBySlug`/`UpdateAlbum` (re-slug z title)/
  `ListAlbums` (s počty fotek, řazení dle title)/`DeleteAlbum`; členství `AddPhoto`
  (idempotentní upsert pozice)/`RemovePhoto` (idempotentní)/`ReorderPhotos` (atomický přepis
  `sort_order` dle pořadí)/`SetCover` (set/clear cover)/`ListPhotoUIDs` (řazení `sort_order`).
- **Štítky** — `CreateLabel`/`GetLabelByUID`/`GetLabelBySlug`/`UpdateLabel` (re-slug)/
  `ListLabels` (s počty, řazení priority DESC)/`DeleteLabel`; připojení `AttachLabel`
  (idempotentní upsert source/uncertainty)/`DetachLabel` (idempotentní)/`ListPhotoUIDsByLabel`.
- **Oblíbené** — `AddFavorite`/`RemoveFavorite` (obojí idempotentní), `IsFavorite`,
  `ListFavorites` (per-user, newest-first), `FavoritedAmong` (z dané množiny photo uid vrátí
  per-user podmnožinu oblíbených jako množinu — anotace celé stránky `is_favorite` jedním dotazem).
- **Sentinely** — `ErrAlbumNotFound`/`ErrLabelNotFound`/`ErrPhotoNotFound`/`ErrUserNotFound`/
  `ErrSlugExhausted`/`ErrInvalidType`/`ErrInvalidSource`. FK porušení při zápisu do join tabulek
  se mapují na not-found sentinely podle porušeného sloupce (`photo_uid` → photo apod.).

### Alba & štítky API (`internal/organizeapi`)

HTTP API nad katalogem alb a štítků (`NewAPI(Config{Albums,Labels,RequireAuth,RequireWrite})` +
`RegisterRoutes`). `Albums`/`Labels` jsou rozhraní (podmnožiny `organize.Store`), takže se
handlery unit-testují s faky bez DB. Čtení je pro každého přihlášeného (`RequireAuth`), mutace
pro editora/admina (`RequireWrite`). Prohlížení fotek alba/štítku **nemá vlastní endpoint** —
jede přes sdílené `GET /photos` scopnuté `?album={uid}` / `?label={uid}` (viz níže), takže
frontend znovupoužije stejnou virtualizovanou mřížku.

- **Alba** — `GET /albums` (list s počty + cover), `POST /albums` (201, `title` povinný, validace
  typu), `GET /albums/{uid}`, `PATCH /albums/{uid}` (title/description/cover/order_by/private;
  **strukturální `type` se zachová**, není editovatelný), `DELETE /albums/{uid}` (204);
  členství `POST /albums/{uid}/photos` `{photo_uids:[…]}` (přidá za stávající fotky),
  `DELETE /albums/{uid}/photos` `{photo_uids:[…]}` (odebere), `PATCH /albums/{uid}/order`
  `{photo_uids:[…]}` (přeřadí) — všechny tři vrací aktuální pořadí `{photo_uids:[…]}`.
- **Štítky** — `GET /labels` (list s počty), `POST /labels` (201, `name` povinný),
  `GET /labels/{uid}`, `PATCH /labels/{uid}` (name/priority), `DELETE /labels/{uid}` (204);
  připojení `POST /labels/{uid}/photos` `{photo_uid,source?,uncertainty?}` (204),
  `DELETE /labels/{uid}/photos` `{photo_uid}` (204).
- **Scoped listing** — `GET /photos?album={uid}` a `GET /photos?label={uid}` (a stejně tak
  `GET /search`) přidávají do `photos.ListParams` korelované `EXISTS` filtry (`AlbumUID`/`LabelUID`),
  takže scope ctí všechny ostatní list filtry, řazení i stránkování a odpověď má identický tvar
  jako běžný výpis knihovny.
- **Stavové kódy** — 400 (validace/neznámé pole/neplatný typ/source), 404 (chybějící album/štítek/
  fotka), 403 (viewer na mutaci), 401 (nepřihlášený). Mountuje se devátým `server.WithAPI`
  (`buildOrganizeAPI` v `cmd/kukatko/organize.go`).

### Hromadná editace metadat (`internal/bulk` + `internal/bulkapi`)

Jeden endpoint **`POST /api/v1/photos/bulk`** (editor/admin přes `RequireWrite`) aplikuje sadu
operací na **mnoho fotek najednou v jediné transakci** spolu s durable audit-log záznamem, takže
celá dávka commitne nebo se rollbackne atomicky. Tělo: `{"photo_uids":[…],"operations":{…}}`.
Podporované operace (každá volitelná, neuvedené pole = beze změny):

- `add_to_albums`/`remove_from_albums` `[al…]`, `add_labels`/`remove_labels` `[lb…]` (idempotentní);
- `set_caption`/`clear_caption` (→ `title`), `set_description`/`clear_description`;
- `set_location {lat,lng}` (validace rozsahu) / `clear_location`;
- `set_private` (bool), `archive` / `unarchive` (vzájemně se vylučují);
- `set_favorite` (bool) — **per-user** oblíbená pro volajícího.

Set/clear páry jsou samostatné klíče (ne presence/null), takže payload je jednoznačný a konflikt
(`set_*` + `clear_*`, `archive` + `unarchive`) je **400**. Neznámý klíč operace → **400**
(`DisallowUnknownFields`). Dávka nad limit `bulk.max_batch_size` (default 1000) → **413**.

- **Sémantika výsledku** — odpověď `{results:[{photo_uid,status,error?}],counts:{total,updated,
  skipped,errored}}` (HTTP 200 i při dílčích chybách). Per-foto stavy: `updated` (aplikováno),
  `skipped` (duplicitní uid v dávce), `error` (fotka neexistuje). **Chybějící fotka neabortuje
  validní** — zaznamená se jako error, ostatní se aplikují a commitnou; jen skutečná DB chyba
  rollbackne celou dávku (500). Alba/štítky v add operacích se ověřují předem (chybějící → 400).
- **Audit log** (`internal/audit`) — bulk zápis vloží audit řádek do **téže transakce** jako mutace
  přes `audit.Write(ctx, tx, Entry)`. Plný popis durable audit trailu a admin API viz sekce
  [Durable audit log](#durable-audit-log-internalaudit--internalauditapi) níže.
- **Vrstvy** — `bulk.Service` (`NewService(pool, maxBatch)`, `Apply(ctx, actorUID, photoUIDs, ops)`)
  drží transakční logiku a vlastní SQL (vlastní tx kvůli atomicitě), `bulkapi` dělá HTTP +
  validaci payloadu. Mountuje se dalším `server.WithAPI` (`buildBulkAPI` v `cmd/kukatko/bulk.go`).

### Durable audit log (`internal/audit` + `internal/auditapi`)

Append-only audit trail zapisovaný **ve stejné transakci** jako mutace, kterou eviduje — opravuje
mezeru photo-sorteru, který audit zapisoval až po commitu (při pádu mezi commitem mutace a zápisem
auditu se záznam ztratil). Tabulka `audit_log` (migrace `0012_audit_log.sql`, rozšířená v
`0014_audit_request.sql`): `id BIGSERIAL`, `actor_uid` (FK users `ON DELETE SET NULL` — trail
přežije smazání účtu), `action`, `target_type`, `target_uid`, `details JSONB`, `ip`, `user_agent`,
`created_at`; indexy na `(created_at)`, `(target_type, target_uid)`, `(action)`, `(actor_uid)`.
(Sloupce `actor`/`target`/`details` odpovídají spec termínům `user`/`entity`/`metadata` — zachována
jsou původně shipnutá jména, přejmenování aplikované migrace by bylo destruktivní.)

- **Mechanismus** — `Write(ctx, exec, Entry)` přijímá `Execer` (pool **i** `pgx.Tx`), takže audit
  insert jede na téže transakci jako mutace a commitne/rollbackne s ní (žádný osiřelý ani chybějící
  záznam). `Store.Record` zapíše na vlastním spojení; `Store.List(ctx, Filter)` + `Count(ctx, Filter)`
  filtrovaně čtou (actor/entity/action/datum, stránkování, newest-first, limit cap 500/default 100).
- **Konvence pro handlery** — `audit.FromRequest(r, actorUID)` posbírá actor (z auth kontextu), IP
  (`X-Forwarded-For` → `X-Real-IP` → `RemoteAddr`) a User-Agent do `Meta`; `meta.Entry(action,
  entityType, entityUID, details)` z toho postaví `Entry`. Action konstanty: `ActionPhotosBulk`,
  `ActionPhoto{Update,Archive,Unarchive}`, `ActionAlbum/Label{Create,Update,Delete}`,
  `ActionFaceAssign`, `ActionUser{Create,Update,Disable,Password}`.
- **Zapojené in-tx mutace** — hromadná editace (`internal/bulk`) a foto PATCH/archive/unarchive přes
  audited varianty `photos.Store.{UpdateMetadata,Archive,Unarchive}Audited` (sdílený `rowQuerier`
  spustí mutaci na tx, `mutateAudited` přidá audit a commitne). Ostatní mutační domény (alba/štítky,
  lidé, správa uživatelů) přebírají stejný vzor v navazujících iteracích.
- **Admin API** — `GET /api/v1/audit` (admin-only přes `RequireAdmin`, `internal/auditapi`) vrací
  `{entries,total,limit,offset,next_offset}` newest-first s filtry `?user=`/`?entity_type=`/
  `?entity_uid=`/`?action=`/`?since=`/`?until=` (RFC3339) a `?limit=`(≤500)/`?offset=`; neplatný
  čas/číslo → 400. Jen čtení — zápisy jdou výhradně přes mutační transakce. Mountuje se
  `buildAuditAPI` v `cmd/kukatko/audit.go`.

### Mapy: dlaždice, reverse geocode & GeoJSON (`internal/mapy` + `internal/mapsapi`)

Backendová proxy na [mapy.com](https://mapy.com), aby **API klíč nikdy neopustil server** (posílá
se jen v hlavičce `X-Mapy-Api-Key`), plus GeoJSON feed geotagovaných fotek pro mapový pohled.
Všechny endpointy vyžadují přihlášení (`RequireAuth`), mountuje se `server.WithAPI` (`buildMapsAPI`
v `cmd/kukatko/maps.go`).

- **`GET /api/v1/map/tiles/{mapset}/{z}/{x}/{y}`** — proxy dlaždice: backend doplní klíč a **streamuje**
  bajty zpět s dlouhým `Cache-Control` (immutable). `mapset` je omezen na allowlist
  `basic|outdoor|aerial|winter` (jiný → 400, ještě před voláním mapy.com); retina `@2x` (sufix na `{y}`
  nebo `?retina=true`) se aplikuje jen pro `basic`/`outdoor`. Neplatné `z`/`x`/`y` → 400.
- **`GET /api/v1/map/rgeocode?lat=&lng=`** — reverse geocode → zjednodušené
  `{name, location, regional_structure}`. **Cachuje se** (klíč = zaokrouhlená souřadnice) a uncached
  lookupy jsou **rate-limitované** kvůli kreditům (geocode = 4 kredity); přes limit → 429, bez shody → 404.
- **`GET /api/v1/map/photos`** — **GeoJSON FeatureCollection** geotagovaných fotek (jen ty s lat/lng;
  souřadnice RFC 7946 `[lng, lat]`). Ctí standardní list filtry (`taken_after`/`taken_before`, `album`,
  `label`, `archived`, `private`); každá feature nese `uid`, `title`, `taken_at`, `media_type` a relativní
  `thumb` cestu pro markery/clustering.
- **mapy.com chyby** (401/403/404/429/5xx) se mapují na rozumné statusy (bad key/upstream → 502,
  nedostupné → 503, 404/429 propagovány) a **nikdy neprosakuje klíč** do odpovědí ani chyb. Bez
  nakonfigurovaného klíče (`maps.mapy_api_key`) vrací tile/rgeocode 503, GeoJSON funguje dál.
- **Vrstvy** — `mapy.Client` (HTTP klient k mapy.com za rozhraním, fakeovatelný; sentinely
  `ErrUnauthorized`/`ErrNotFound`/`ErrRateLimited`/`ErrUpstream`/`ErrUnavailable`/`ErrInvalidMapset`),
  `mapsapi` dělá HTTP handlery + cache + rate-limit + parsing filtrů. Base URL je konfigurovatelná
  (`maps.base_url`, default `https://api.mapy.com`) hlavně pro test double (httptest fake mapy.com).

### People UI (frontend)

Kompletní lidský zážitek nad výše uvedenými API (react-bootstrap Superhero, i18n cs/en,
responzivní/touch). Routy v `Layout` navbaru pod odkazem **Lidé** (`/people`):

- **`/people`** (`PeoplePage`) — mřížka osob (`SubjectTile`: cover/jméno/počet fotek);
  editorům odkaz na review shluků.
- **`/people/:uid`** (`SubjectPage`) — stránka osoby: hlavička (jméno/typ, edit přes
  `SubjectEditModal`), paginovaná galerie (`useSubjectPhotos` + `SubjectPhotoTile` se „set as
  cover" akcí), a sekce **outlierů** (`Outliers` — žebříček podezřelých obličejů, one-tap unassign;
  jen editor/admin).
- **`/people/clusters`** (`ClustersPage`, editor/admin) — **primární rychlá cesta**: fronta
  nepojmenovaných shluků obličejů, každý `ClusterCard` (reprezentant + ukázky + odebrání zatoulaného
  obličeje + **jednorázové pojmenování celého shluku** na nový/existující subjekt); optimistické
  odebrání po pojmenování.
- **`/photos/:uid`** (`PhotoDetailPage`) — detail fotky s interaktivním **`FaceOverlay`**: boxy
  obličejů kreslené z normalized bbox (`faceBoxStyle`), klik → panel s návrhy identit (one-tap
  accept) + free-text jméno; optimistický update + refetch. Plus pruh `SimilarPhotos`.
- Společné: `FaceThumb` (výřez obličeje z thumbnailu přes `faceCropStyle`), klient `services/people.ts`,
  geometrie `lib/faceGeometry.ts`. Vitest pokrývá pojmenování shluku, pozicování/přiřazení v overlay
  a unassign outlierů (mock API).

### Image embedding & similar photos (`internal/embedjob`)

`embedjob.Service` zapojuje CLIP embedding do fronty jobů a staví nad ním embeddingové dotazy.
Vše za rozhraními (`PhotoStore`/`VectorStore`/`Previewer`/`Enqueuer` + `embedding.Client`), takže
se unit-testuje s faky bez sítě/DB/disku.

- **Handler `image_embed`** (`Handle` = `worker.HandlerFunc`, registrovaný v `serve`): z payloadu
  `{"photo_uid": …}` načte fotku, vyrenderuje (idempotentně) náhled `fit_720`, pošle ho sidecaru
  (`Client.ImageEmbedding`) a uloží 768-dim `halfvec` přes `vectors.SaveEmbedding` (+ `model`/
  `pretrained`). **Idempotentní** — fotka, která už embedding má, se přeskočí bez volání sidecaru.
  **Box offline** (`embedding.IsUnavailable`) → vrátí `worker.RetryAfter(5 min, …)`, takže se job
  jen odloží (`Defer`) bez spálení pokusu; jiná chyba jde normální retry/dead-letter cestou.
- **`BackfillEmbeddings(ctx)`** — pro každou fotku bez embeddingu (`ListPhotosMissingEmbedding`)
  zařadí `image_embed` (dedup = no-op), vrací počet. Bezpečné spouštět opakovaně.
- **`Duplicates(ctx, photoUID)`** — embeddingová detekce blízkých duplikátů: najde fotky do
  konfigurované cosine vzdálenosti (`duplicate.embedding_max_dist`) od embeddingu fotky, bez ní
  samé; `<= 0` ji vypne, fotka bez embeddingu → nil. Doplňuje pHash kontrolu, kterou upload dělá
  už při ingestu (kdy embedding ještě neexistuje).

### Detekce obličejů (`internal/facejob`)

`facejob.Service` zapojuje detekci obličejů do fronty jobů. Vše za rozhraními
(`PhotoStore`/`VectorStore`/`ImageSource`/`Enqueuer` + `embedding.Client`), takže se unit-testuje
s faky bez sítě/DB/disku.

- **Handler `face_detect`** (`Handle` = `worker.HandlerFunc`, registrovaný v `serve`): z payloadu
  `{"photo_uid": …}` načte fotku, otevře **dekódovatelný originál v plném rozlišení** (přes
  `StorageSource` = `storage` + `imgconvert.EnsureDecodable`, takže HEIC/RAW/video se převedou) a
  pošle ho sidecaru (`Client.FaceEmbeddings` → 512-dim ArcFace embeddingy + pixelové bboxy +
  det_score). Originál (ne náhled) proto, že sidecar (InsightFace) sám rotuje podle EXIF a vrací
  bbox v display pixelech — normalizace dle uložených rozměrů sedí jen ve stejném měřítku. Každý
  obličej se uloží přes `vectors.RecordFaceDetection` (512-dim `halfvec`, **normalizovaný bbox**,
  det_score, `face_index`, model, cache `photo_width`/`photo_height`/`orientation`).
- **Převod bboxu** (`normalizeBBox`): pixelový `[x1,y1,x2,y2]` → normalizovaný `[x,y,w,h]` (0..1)
  podle rozměrů fotky a **EXIF orientace** — pro orientace 5–8 (rotace o 90°/270°) se prohodí
  šířka a výška display prostoru. Mirror photo-sorter logiky, otestováno přes všech 8 orientací.
- **Filtr det_score** (`faces.min_det_score`, default `0.5`): obličeje s nižší confidence se
  zahodí; přeživší se přeindexují souvisle (žádné mezery v `face_index`). `<= 0` filtr vypne.
- **Idempotence**: fotka, která už má `face_detections` řádek, se přeskočí bez volání sidecaru;
  detekce s **nula obličeji** se přesto zaznamená, takže se znovu nezpracovává. **Box offline**
  (`embedding.IsUnavailable`) → `worker.RetryAfter(5 min, …)` (odložení bez spálení pokusu).
- **`BackfillFaces(ctx)`** — zařadí `face_detect` pro každou nezpracovanou fotku
  (`ListPhotosMissingFaces`, dedup = no-op), vrací počet. Upload zařazuje `face_detect` rovnou
  při ingestu; backfill je recovery cesta pro fotky nahrané, když byl box offline.

### Sledování importů (`internal/importer`)

Evidence běhů importu/migrace a jejich high-watermarků pro **inkrementální, idempotentní** import
(migrace `0013_import_runs.sql`, tabulka `import_runs`; viz ARCHITECTURE.md §5.2/§9/§10). Každý běh
importu z PhotoPrismu (`photoprism`) nebo migrace z photo-sorteru (`photosorter`) si uloží časové
okno, které zpracoval: `high_watermark` (`TIMESTAMPTZ`) = největší zdrojový timestamp (např. max
PhotoPrism `UpdatedAt`). Další běh téhož zdroje naváže na watermark **posledního úspěšného** běhu,
takže spadlý/chybný běh kurzor neposune a práce se jen zopakuje.

- **Schéma** — `import_runs`: `id BIGSERIAL PK`, `source TEXT CHECK(photoprism|photosorter)`,
  `started_at`/`finished_at TIMESTAMPTZ`, `status TEXT CHECK(running|done|failed)`,
  `high_watermark TIMESTAMPTZ` (NULL dokud běh neskončí / nic nezpracoval), `counts JSONB`
  (`{imported,updated,skipped,failed}`), `last_error TEXT`. Partial index
  `(source, finished_at DESC) WHERE status='done' AND high_watermark IS NOT NULL` zlevňuje resume
  dotaz na přesně ty řádky, které čte.
- **`importer.Store`** (`NewStore(pool)`) — lifecycle běhu: `Start(ctx, source)` otevře řádek ve
  stavu `running` (neznámý zdroj → `ErrInvalidSource`), `UpdateCounts(ctx, id, counts)` průběžně
  přepíše tally, `Complete(ctx, id, watermark, counts)` uzavře běh jako `done` se stampnutým
  `finished_at` + watermarkem, `Fail(ctx, id, lastErr, counts)` jako `failed` **bez** watermarku.
  `Complete`/`Fail` matchují jen běžící běh (žádné dvojí uzavření → `ErrRunNotFound`). `Get(ctx, id)`
  čte jeden běh.
- **`LatestWatermark(ctx, source)`** → `(time.Time, found bool, err)` — watermark **posledního
  úspěšného** běhu zdroje (řazení dle `finished_at`), kterým má další inkrementální běh navázat;
  `found=false` při prvním (plném) běhu. **Ignoruje** běžící i failed běhy a done běhy bez
  watermarku. Každý zdroj má vlastní nezávislý kurzor.

### PhotoPrism API klient (`internal/photoprism`)

Read-only HTTP klient k běžící instanci PhotoPrismu — podklad inkrementálního importu (viz
ARCHITECTURE.md §9). Vše za rozhraním `Client` (fakeovatelné v testech), takže importér ani
testy nepotřebují reálný PhotoPrism, síť ani token.

- **Autentizace** — dlouhožijící **app password / access token** (PP:
  `photoprism auth add -n Kukatko -s "photos albums"`) se posílá v hlavičce `Authorization: Bearer`
  na **každém** requestu; nikdy se neloguje per-request (login je nejtvrději rate-limited).
  Konfiguruje se přes `import.photoprism.{base_url,token}` (token přes
  `KUKATKO_IMPORT_PHOTOPRISM_TOKEN`, necommituj).
- **`ListPhotos(ctx, PhotoListParams)`** → `GET /api/v1/photos?count=1000&offset=N&merged=true&
  order=updated&q=updated:"<RFC3339>"`. `UpdatedSince` (nenulové) přidá filtr `updated:` pro
  **inkrementální** pull; `count` se ořezává na `MaxCount` (1000), `offset` řídí stránkování
  (caller pageuje, dokud stránka vrací plný `count`). Parsují se pole UID, TakenAt, Lat/Lng/Altitude,
  Title/Description, Type, Width/Height, OriginalName, Camera/Lens/EXIF a `Files[]`
  (UID, **Hash = SHA1**, Primary, Mime, `Markers[]`). `Photo.PrimaryFile()` vrátí primární soubor.
  `PhotoListParams` navíc umí **scopnout** výpis pro mapování členství: `AlbumUID` přidá filtr
  `s=<albumUID>` (fotky alba), `Query` nastaví `q=` natvrdo (přebije watermark — používá se pro
  `label:"<slug>"`).
- **`ListAlbums`/`ListLabels`/`ListSubjects(ctx, ListParams)`** → `GET /api/v1/{albums,labels,subjects}`
  (count/offset); markery jedou přes `Files[].Markers[]`.
- **`DownloadOriginal(ctx, fileHash)`** → `GET /api/v1/dl/{hash}?t=<download_token>` **streamuje**
  originál (nikdy celý v RAM; tělo vlastní caller a zavře ho). Download token se získá z
  create-session (`POST /api/v1/session`, čte `config.downloadToken`), **může rotovat** — klient ho
  průběžně přebírá z hlavičky `X-Download-Token` a při 401/403 jednou obnoví session a zopakuje.
- **Robustnost** — **429** se retryuje s exponenciálním backoffem (ctí `Retry-After`); JSON endpointy
  vyžadují `Content-Type: application/json`; rozumné timeouty (JSON volání `Timeout`, download jen
  ctx callera); typové chyby `ErrInvalidURL`/`ErrUnauthorized`/`ErrNotFound`/`ErrRateLimited`/
  `ErrUpstream`/`ErrUnavailable`/`ErrBadResponse` — nikdy neobsahují token ani tělo odpovědi.

### PhotoPrism import (`internal/ppimport` + `internal/importapi`)

Read-only, **inkrementální a idempotentní** import z PhotoPrismu (ARCHITECTURE.md §9). Stáhne
nové/změněné fotky, dedupuje, namapuje externí ID a po importu nechá běžné joby dopočítat
embeddingy/obličeje. Všechny spolupracovníky drží za rozhraními → unit-testovatelné s faky bez
PhotoPrismu, sítě, DB i disku.

- **Spuštění** — buď CLI `kukatko import photoprism` (synchronně, pro ops/cron bez běžícího
  serveru), nebo admin endpoint `POST /api/v1/import/photoprism`, který zařadí **`pp_import` job**
  (běží v background workeru). `pp_import` payload nese pevný sentinel `photo_uid`, takže dedup
  fronty pustí **jen jeden import** naráz (druhý trigger → 409). Handler i CLI volají stejnou
  `Service.Import`.
- **Běh** (`Service.Import`) — otevře `import_runs` běh, navrhne na poslední úspěšný watermark a:
  1. **Fotky** — pageuje `ListPhotos(UpdatedSince=watermark)`; per fotka dedup dle `photoprism_uid`
     (už importovaná → update změněných metadat, jinak skip), jinak **vybere média dle PP `Type`**
     (`selectMedia`): **video/animated** → stáhne samotný **video soubor** (`Photo.VideoFile()`,
     `media_type=video`; video bez detekovatelného streamu graceful degraduje na image), **live** →
     **still** jako primární originál + **motion klip** jako `sidecar` photo_file
     (`Photo.StillFile()`+`VideoFile()`, `media_type=live`), jinak **image** (primární soubor);
     stáhne vybraný originál, spočítá **SHA256**, dedupuje dle `file_hash` (shodný obsah už v katalogu
     → backfill `photoprism_uid`/`photoprism_file_hash` přes `photos.SetPhotoprismRef`, žádná nová
     fotka), uloží originál, **u videa/live probne video metadata** (`Prober.Probe` →
     `duration_ms`/`video_codec`/`audio_codec`/`has_audio`/`fps`; u video z originálu, u live z motion
     klipu; best-effort), založí `photos` řádek s **PP metadaty** (title/desc/taken_at/GPS/camera/EXIF)
     + `media_type` + video metadata + externími ID + **EXIF orientací ze souboru** (PP ji nevystavuje),
     **u live** uloží i motion klip jako `RoleSidecar`, vyrenderuje náhledy (**u videa poster frame**
     přes ffmpeg) a **zařadí `image_embed`** (na posteru) **+ `face_detect`** joby. Counts se
     **checkpointují po každé stránce**.
  2. **Lidé** — z `Files[].Markers[]` nově importované fotky: každý **pojmenovaný validní face
     marker** find-or-create subjekt (dle `Slugify`) + Kukátko marker přiřazený subjektu (markery
     jen při prvním importu, ať re-run neduplikuje).
  3. **Alba & štítky** — find-or-create dle názvu (mapa z `ListAlbums`/`ListLabels`), členství přes
     scopnutý `ListPhotos` (`AlbumUID` / `label:"<slug>"`) → `AddPhoto` / `AttachLabel` (oboje
     idempotentní), jen pro už importované fotky.
  4. **Uzavření** — zapíše counts + nový watermark a běh `done`.
- **Robustnost** — per-fotka chyba se zaznamená do `counts.failed` a **nepřeruší běh** (jen
  infrastrukturní chyba — nelze listovat / DB — běh `fail`ne); 429 backoff řeší klient; **watermark
  se nikdy neposune za nejstarší selhání** (selhaná fotka se příště znovu nabere); celý import je
  bezpečný k opakování. Konfiguruje se přes `import.photoprism.{base_url,token,page_size}`; bez
  `base_url` se import job ani endpoint neregistrují (CLI vrátí chybu).

### photo-sorter migrace (`internal/photosorter` + `internal/psimport`)

Read-only, **inkrementální a idempotentní** přímá migrace z PostgreSQL DB **photo-sorteru**
(ARCHITECTURE.md §10). Protože photo-sorter i Kukátko používají **stejné modely a rozměry**
(CLIP 768 + InsightFace 512) a **stejné SHA256** file hashe, **embeddingy a obličeje se přenášejí
1:1** bez přepočtu a fotky deduplikují přímo. Všechny spolupracovníky drží za rozhraními →
unit-testovatelné s faky bez photo-sorteru, sítě, DB i disku; **integrační testy** jedou proti
**naseedovanému fake photo-sorter schématu** (`ps_fixture`) vedle Kukátko tabulek v jedné test DB.

- **`internal/photosorter`** — read-only klient s vlastním pgx poolem (oddělený od Kukátko),
  pgvector typy registrované na každém spojení, volitelný `Schema` scopne každý dotaz přes
  `search_path` (tak integrační test čte fake schéma). Čte **jen** tabulky, které migrace potřebuje
  (`photos`, `embeddings`, `faces`, `faces_processed`, `subjects`, `markers`, `albums`/
  `album_photos`, `labels`/`photo_labels`, `photo_phashes`, `photo_edits`) — **fotoknihu ani
  share-linky nikdy nečte**.
- **Spuštění** — buď CLI `kukatko migrate photosorter` (synchronně, pro ops/cron), nebo admin
  endpoint `POST /api/v1/import/photosorter`, který zařadí **`ps_migrate` job** (běží v background
  workeru). `ps_migrate` payload nese pevný sentinel, takže dedup fronty pustí **jen jednu migraci**
  naráz (druhý trigger → 409). Handler i CLI volají stejnou `Service.Migrate`.
- **Běh** (`Service.Migrate`) — otevře `import_runs` běh (`source=photosorter`), navrhne na poslední
  úspěšný watermark a:
  1. **Katalogy** — find-or-create Kukátko **subjekt** (dle slug z jména), **album** (dle title)
     a **štítek** (dle jména) pro každý photo-sorter; vznikne ps-uid → kk-uid mapa pro satelity.
  2. **Fotky** — pageuje `ListPhotos(UpdatedSince=watermark)` (řazení `updated_at`); per fotka
     match dle `photosorter_uid` (už migrovaná → skip), jinak dle **`file_hash`** (už v katalogu,
     např. z PhotoPrism importu → backfill `photosorter_uid` přes `photos.SetPhotosorterRef`, žádné
     kopírování), jinak **zkopíruje originál** z `file_path` do Kukátko storage (SHA256, náhledy),
     založí `photos` řádek s photo-sorter metadaty + `photosorter_uid`.
  3. **Satelity** — **embedding** (768) a **faces** (512 + bbox + det_score + cache) se vloží
     **1:1** (zachová `model`/`pretrained`, remapuje subjekt, zachová `marker_uid`); fotka, kterou
     photo-sorter **neembedoval/nedetekoval**, dostane Kukátko `image_embed`/`face_detect` job.
     **markery** se migrují pod původním UID (idempotence), **album/label členství**, **phash**
     a **edit** se přenesou (best-effort, idempotentně).
  4. **Uzavření** — zapíše counts + nový watermark a běh `done`.
- **Robustnost** — per-fotka chyba se zaznamená do `counts.failed` a **nepřeruší běh** (jen
  infrastrukturní chyba běh `fail`ne); **watermark se nikdy neposune za nejstarší selhání**; celá
  migrace je bezpečná k opakování. Konfiguruje se přes `import.photosorter.{dsn,page_size}` (DSN
  přes `KUKATKO_IMPORT_PHOTOSORTER_DSN`, necommituj); bez `dsn` se migrace job ani endpoint
  neregistrují (CLI vrátí chybu).

### Import admin UI (`internal/importapi` + `web` `ImportPage`)

Admin-only konzole na `/import` ([`web/src/pages/ImportPage.tsx`](web/src/pages/ImportPage.tsx)) pro
spuštění a sledování importů — viditelná v navbaru jen administrátorům (gate `isAdmin`), route pod
`RequireRole role="admin"`.

- **Backend** (`internal/importapi`, vše admin-only přes `RequireAdmin`) přidává k triggerům i
  **historii**: `GET /api/v1/import/runs` (vždy registrovaný) → `{runs,limit,offset,sources}` —
  stránka `import_runs` newest-started-first (query `limit`≤200/`offset`, neplatný → 400) plus
  `sources:{photoprism,photosorter}` flagy jaké zdroje jsou nakonfigurované. Stránku čte
  `importer.Store.List`. Celá API se mountuje **vždy** (i bez konfigurovaného zdroje), aby historie
  fungovala; triggery `POST /import/{photoprism,photosorter}` se registrují jen pro konfigurované
  zdroje (jinak 404).
- **Frontend** ([`web/src/services/import.ts`](web/src/services/import.ts)) polluje
  `GET /import/runs` + `GET /jobs/stats` po 3 s. Dvě sekce (PhotoPrism, photo-sorter) s tlačítkem
  **Spustit import** (gate na `sources` flagy + běžící běh), **živý průběh** běžícího běhu (spinner +
  counts imported/updated/skipped/failed), souhrn **fronty na pozadí** (queued/running/failed/dead)
  a tabulka **historie běhů** (zdroj / začátek / konec / stav / počty / poslední chyba). Jasně sděluje,
  že PhotoPrism zůstává primární a import je inkrementální/opakovatelný; před **prvním** (potenciálně
  velkým) během zdroje se ptá na potvrzení. 409 → „import už běží". i18n cs/en.

### S3 záloha (`internal/backup` + `internal/backupapi`)

V procesu, plánovaná záloha **databáze a originálů** na libovolný **S3-kompatibilní** endpoint
(AWS / MinIO / Backblaze / Wasabi) přes **`minio-go/v7`** s **path-style** adresováním a
**streamovaným** uploadem (`objectSize=-1`, nikdy nedrží soubor celý v RAM). Vše za rozhraními
(`ObjectStore`/`Dumper`/`OriginalSource`) → unit-testovatelné s faky bez S3, DB i FS. Tajné klíče
nikdy neprosáknou do logu ani chyby.

Jeden běh dělá tři věci v pořadí:

1. **Dump databáze** — shell-out na **`pg_dump`** (custom/komprimovaný formát, `--no-owner
   --no-privileges`) streamovaný rovnou na S3 jako `db/kukatko-<timestamp>.dump`. DSN se předává
   přes env proměnnou `PGDATABASE` (ne argument), aby heslo nebylo vidět v `ps`. Timestamp dodává
   plánovač/příkaz.
2. **Sync originálů** — projde úložiště originálů a **inkrementálně** nahraje jen ty, co v bucketu
   ještě nejsou (skip dle klíče + velikosti), streamovaně; klíč = relativní cesta originálu, dump
   se ukládá pod `db/` prefix. Dočasná upload složka `.tmp` se přeskakuje.
3. **Retence** — po úspěšném dumpu prořeže staré dumpy na posledních `backup.retention` (≤ 0 =
   nechat vše). **Nikdy nesahá na originály** (jen prefix `db/`) a **nikdy neprořezává po
   neúspěšném dumpu**, takže selhání zálohy nemůže smazat poslední dobré dumpy.

- **Plánovač**: `backup.schedule` (standardní 5-pole cron nebo `@daily`/`@hourly`/`@every 6h`
  deskriptory přes `robfig/cron`) běží uvnitř `kukatko serve`; prázdný/neplatný rozvrh plánované
  zálohy vypne (manuální stále fungují). Souběžné běhy se serializují (`ErrAlreadyRunning`).
- **CLI**: `kukatko backup` spustí jednu zálohu synchronně a vypíše počty (ops/cron bez běžícího
  serveru). Vyžaduje `backup.s3.endpoint` + `backup.s3.bucket`.
- **Admin API** (`internal/backupapi`, admin-only): `GET /api/v1/backup` (stav + poslední běh,
  `configured:false` když není nakonfigurováno) a `POST /api/v1/backup` (spustí zálohu na pozadí →
  202, 409 když už běží, 503 bez konfigurace).
- Runtime apt závislost: **`postgresql-client`** (pg_dump **i** pg_restore). Konfig klíče
  `backup.s3.{endpoint,region,bucket,access_key,secret_key,path_style}`, `backup.schedule`,
  `backup.retention`; tajemství (`access_key`/`secret_key`) přes env.

### Obnova / disaster recovery (`internal/backup` + `internal/restoreapi`)

Protějšek zálohy, aby byla skutečně **použitelná**. Sdílí `backup.s3.*` konfiguraci (bucket =
zdroj obnovy), `database.url` (cíl) a `storage.originals_path` (kam se zapisují originály). Vše za
stejnými rozhraními (`ObjectStore` rozšířeno o `Open`, nové `Restorer`/`LocalOriginals`/
`PhotoCatalog`) → unit-testovatelné s faky. Tajemství nikdy do logu ani argv.

CLI strom **`kukatko restore`** (ops/cron bez běžícího serveru; vyžaduje `backup.s3.{endpoint,bucket}`):

- **`restore list`** — vypíše dumpy v bucketu (`db/kukatko-*.dump`), nejnovější první.
- **`restore db [--dump KEY] [--yes] [--verify]`** — **DESTRUKTIVNÍ**: stáhne dump z S3 a streamuje
  ho rovnou do **`pg_restore`** (`--clean --if-exists --single-transaction --no-owner
  --no-privileges`, čte archiv ze stdin → nikdy celý v RAM). Heslo se předává přes `PGPASSWORD` env
  (parsované z DSN), **nikdy v argv**. Po obnově idempotentně re-aplikuje migrace. Bez `--dump`
  obnoví nejnovější dump; **vyžaduje `--yes`** (přepisuje všechna data). `--verify` rovnou spustí
  integritní report.
- **`restore originals`** — stáhne z bucketu jen originály, které na disku ještě nejsou (skip dle
  **klíče + velikosti**), atomickým zápisem přes `.tmp` + rename → **resumovatelné** (přerušený běh
  se bezpečně zopakuje). Dumpy pod `db/` se přeskakují.
- **`restore verify`** — integritní report: **fotek v DB vs originálů na disku** + nesoulady
  (`photo_files.file_path` chybějící na disku / soubory na disku bez záznamu v katalogu), s
  omezeným vzorkem na výpis.

**Admin API** (`internal/restoreapi`, admin-only, jen read-only operace): `GET /api/v1/restore/dumps`
(seznam dumpů, 503 bez konfigurace) a `POST /api/v1/restore/verify` (integritní report). Destruktivní
obnova DB se přes HTTP **záměrně neexponuje** (podtrhla by tabulky běžícímu serveru) — patří do CLI
při zastaveném serveru. Náhledy se po obnově regenerují líně on-demand; embeddingy/faces jsou v dumpu.

Plný postup (fresh machine → install → restore → verify) s přesnými příkazy: [`docs/RESTORE.md`](docs/RESTORE.md).

### Údržba knihovny — integritní kontrola & opravy (`internal/maintenance` + `internal/maintenanceapi`)

Udržuje velkou, dlouhožijící knihovnu konzistentní: odhalí drift mezi katalogem a soubory na disku
a doplní/přegeneruje odvozená data. Zrcadlí photo-sorter `cache build-thumbs`, ale je širší a
bezpečnější — **nikdy nemaže originály** (to je práce koše/purge), je idempotentní a opravy jedou
přes persistentní frontu jobů (ohraničená souběžnost, resumovatelné). Vše za rozhraními
(`PhotoCatalog`/`VectorCatalog`/`OriginalStore`/`DiskScanner`/`ThumbChecker`/`Enqueuer`/
`EmbedBackfiller`/`FaceBackfiller`/`OrphanImporter`) → unit-testovatelné s faky bez DB/disku/fronty.

- **Integritní kontrola** (`Scan`, read-only): vrátí `Report` s počty + omezenými vzorky pro každou
  třídu problému — fotky s **chybějícím originálem** na disku, **osiřelé soubory** na disku bez
  záznamu v katalogu, fotky s **chybějícími náhledy**, **embeddingy**, **detekcí obličejů** a
  **perceptuálními hashi**, plus totály (fotek / souborů v DB / originálů na disku).
- **Opravy** (`Repair`, každá opt-in, idempotentní): přegeneruje chybějící náhledy a přepočítá
  chybějící pHash/dHash (přes job `thumbnail` → handler `internal/thumbjob`, který náhledy i pHash
  rebuilduje z originálu), zařadí `image_embed`/`face_detect` pro fotky bez nich, a volitelně
  **importuje osiřelé originály** do katalogu přes upload pipeline (dedup na obsah). Pevné pořadí;
  per-orphan selhání se počítá bez abortu, výsledek je `RepairResult` se scheduling počty.
- **CLI**: `kukatko maintenance scan` (vytiskne report) a `kukatko maintenance repair`
  s flagy `--thumbnails`/`--embeddings`/`--faces`/`--phashes`/`--import-orphans` (ops/cron bez
  běžícího serveru; opravy zařadí joby, které drainuje worker běžícího serveru).
- **Admin API** (`internal/maintenanceapi`, admin-only): `GET /api/v1/maintenance/scan` (integritní
  report) a `POST /api/v1/maintenance/repair` `{thumbnails,embeddings,faces,phashes,import_orphans}`
  (spustí vybrané opravy → `RepairResult`; 400 bez vybrané opravy, 503 když orphan import není
  nakonfigurovaný). Admin UI **Údržba** (`/maintenance`, `MaintenancePage`) spustí kontrolu, zobrazí
  nálezy a spustí opravy s progressem přes polling fronty jobů.

### Stav systému — admin dashboard (`internal/system` + `internal/systemapi`)

Jedno místo s provozním zdravím běžící instance, agregované z existujících subsystémů (žádná nová
data, jen sloučení). Doménová služba (`internal/system`) sbírá vše za malými rozhraními
(`DBPinger`/`EmbeddingHealth`/`JobCounter`/`ImportLister`/`BackupReporter`) → unit-testovatelné
s faky bez DB; HTTP vrstva (`internal/systemapi`) je tenká.

- **Agregace** (`Service.Collect`): **dostupnost embeddings sidecaru** (online/offline přes
  `embedding.Client.Healthy`), **hloubka fronty jobů** (counts by state/type, total, dead-letter,
  „pending embeddings" = queued/running `image_embed`+`face_detect`), **stav zálohy** (poslední
  běh + výsledek, nil-safe když není nakonfigurováno), **poslední import per zdroj**
  (`importer.Store.LatestRun` — nejnovější běh bez ohledu na stav), **využití úložiště**
  (velikost originálů + cache, volné/celkové místo přes `statfs`; měření je memoizované na
  `defaultStorageTTL` = 30 s, aby polling nepřecházel velký strom originálů), **dostupnost DB**
  (`db.Ping`, sanitizovaná chyba — neuniká connection string) a **verze/commit** buildu. Chyby
  čtení fronty/importů (vyžadují DB) vrací 500; nedostupná DB a nečitelné úložiště se hlásí inline
  (best-effort), ne jako chyba.
- **Admin API** (`internal/systemapi`, admin-only přes `RequireAdmin`): `GET /api/v1/system/status`
  → jeden snapshot. Mountuje se vždy (`buildSystemAPI` v `cmd/kukatko/system.go`, staví vlastní
  bezstavový embeddings klient jen pro Healthy probe a sdílí pool pro job/import stores; backup
  služba se předává nil-safe).
- **Admin UI** **Systém** (`/system`, `SystemStatusPage`, admin-only) — auto-refresh (polling 5 s)
  kartová mřížka (DB, embeddingy, fronta, záloha, importy, úložiště, verze) s **rychlými akcemi**:
  *znovu zařadit mrtvé úlohy* (vylistuje dead-letter a requeueuje je přes `POST /jobs/{id}/requeue`),
  *spustit zálohu* (`POST /backup`), a odkazy do flow *importu* (`/import`) a *kontroly údržby*
  (`/maintenance`). Když je **box offline** a čekají embeddingové joby, karta to zvýrazní hláškou
  „box offline → embeddingy ve frontě, doženou se po návratu".

### Observability — metriky & strukturované logy (`internal/metrics` + `internal/obs`)

Lehká observabilita po vzoru photo-sorteru, zapojená v `kukatko serve`.

**Prometheus metriky** (`internal/metrics`): jeden izolovaný registr (ne globální
`DefaultRegisterer`, takže testy mají vlastní povrch) v namespace `kukatko_`. `serve` ho
mountuje na **`GET /metrics`** (mimo `/api/v1`, **bez autentizace** — chraň ho na síťové
vrstvě) a instaluje request-metriky middleware. Série:

- **HTTP** — `kukatko_http_requests_total{method,route,status}`,
  `kukatko_http_request_duration_seconds{method,route}`, `kukatko_http_inflight_requests`.
  `route` je **chi route pattern** (`/photos/{uid}`), neshoda → `unmatched` (omezená kardinalita,
  nikdy syrová URL). Scrape `/metrics` se sám nepočítá.
- **Joby** — `kukatko_jobs_started_total{type}`, `kukatko_jobs_finished_total{type,outcome}`,
  `kukatko_jobs_execution_duration_seconds{type,outcome}` (outcome `success`/`error`/`deferred`)
  přes `worker.Observer` hook; hloubka fronty `kukatko_jobs_queue_depth{state}` +
  `kukatko_jobs_queue_depth_by_type{type}` přes kolektor, který čte `jobs.Store` na scrape.
- **Embeddings sidecar** — `kukatko_embedding_request_duration_seconds{operation,outcome}` +
  `kukatko_embedding_service_up` přes dekorátor `embedding.Instrument` (transparentní, vrací
  vnitřní chybu beze změny, takže `errors.Is(ErrUnavailable)` funguje dál).
- **Import** — `kukatko_import_run_photos{source,outcome}` (poslední checkpointnutá tally běhu)
  přes `importer.ProgressObserver` v ppimport/psimport.
- **Náhledy** — `kukatko_thumbnail_generation_duration_seconds` přes `thumb.WithObserver`.
- **DB pool** — `kukatko_db_pool_*` (total/acquired/idle/max + wait/empty-acquire) přes kolektor
  nad `pgxpool.Stat`.
- Standardní `go_*` a `process_*` rodiny.

**Strukturované logy** (`internal/obs`): slog **JSON** na stderr, level z `log.level`
(`KUKATKO_LOG_LEVEL`; debug/info/warn/error, neplatný → chyba při startu). **Access-log
middleware** vypisuje jeden řádek na request s konzistentními poli `request_id` (z chi
`RequestID`, váže logy + `X-Request-Id` hlavičku), `method`, `path`, `route`, `status`, `bytes`,
`duration_ms`, `remote_ip` a `user` (UID, stampnutý auth middlewarem do request-scoped bagu);
`/metrics` se neloguje. **Redakce tajemství**: slog `ReplaceAttr` hook nahradí hodnotu jakéhokoli
atributu, jehož klíč obsahuje `password`/`token`/`secret`/`api_key`/`access_key`/`secret_key`/
`authorization`/`cookie`/`credential`/`dsn`, za `[REDACTED]` — mapy klíč, S3 klíče, session token
ani heslo nikdy neprosáknou do logu. Vypnutí metrik: `metrics.enabled=false`
(`KUKATKO_METRICS_ENABLED=false`) — `/metrics` se nemountuje, access-log běží dál.

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

### Rate-limity náročných endpointů (`internal/ratelimit`)

Mimo login mají **per-client-IP token-bucket** limity i zdrojově náročné endpointy, aby jeden
hlučný klient nevyčerpal sdílené prostředky. `internal/ratelimit` je znovupoužitelný balík
(`New(ratePerSec, burst)` → `Allow(key)` / `Middleware`), keyovaný IP adresou klienta (z
`X-Forwarded-For`/`X-Real-IP` za důvěryhodnou proxy přes chi `RealIP`); prázdný bucket → **HTTP
429** s hlavičkou `Retry-After`. Limiter běží **před** auth checkem (flood se zahodí dřív než
DB lookup), je **paměťově omezený** (opportunistický úklid plně doplněných bucketů, cap
`maxBuckets`) a `rate_per_sec ≤ 0` celé pravidlo **vypne** (pak je middleware no-op).

Pokryté endpointy a defaulty (config sekce `ratelimit.*`):

| Pravidlo | Endpoint | `rate_per_sec` | `burst` |
|----------|----------|----------------|---------|
| `upload` | `POST /upload` | 5 | 30 |
| `bulk` | `POST /photos/bulk` | 2 | 10 |
| `import` | `POST /import/{photoprism,photosorter}` | 1 | 3 |
| `tiles` | `GET /map/tiles/...` | 50 | 200 |

Reverse-geocode proxy (`GET /map/rgeocode`) si drží vlastní credit-šetřící limiter pod `maps.*`.
Klíče přepíšeš env, např. `KUKATKO_RATELIMIT_UPLOAD_RATE_PER_SEC=10`.

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
| GET | `/search?q=&mode=` | přihlášený | sémantické + hybridní hledání; `mode` = `fulltext`/`semantic`/`hybrid` (default `hybrid`): fulltext (tsvector + unaccent, `ts_rank`), semantic (CLIP text→embedding → cosine HNSW), hybrid (fúze obou přes **Reciprocal Rank Fusion**, k=60, dedup); všechny módy ctí list filtry + stránkování; `q` povinný → tvar jako `/photos` + `mode`+`degraded`; box offline → fallback na fulltext s `degraded:true` |
| GET | `/photos/{uid}` | přihlášený | plný detail fotky (metadata, EXIF, GPS) + `files` + `is_favorite` (pro aktuálního uživatele) |
| GET | `/photos?favorite=true` | přihlášený | seznam scopnutý na oblíbené **aktuálního uživatele** (per-user); každá fotka v seznamu/hledání/detailu nese `is_favorite` |
| PUT | `/photos/{uid}/favorite` | přihlášený | označí fotku jako oblíbenou aktuálního uživatele (idempotentní) → 204; 404 chybějící fotka |
| DELETE | `/photos/{uid}/favorite` | přihlášený | zruší oblíbenou aktuálního uživatele (idempotentní) → 204 |
| GET | `/favorites` | přihlášený | oblíbené aktuálního uživatele ve tvaru `/photos` (sdílí filtry/řazení/stránkování) |
| GET | `/photos/{uid}/similar` | přihlášený | vizuálně podobné fotky dle cosine vzdálenosti embeddingu (`?limit`, default 24, max 100) → `{similar:[{…photo, distance}]}` |
| GET | `/photos/{uid}/faces` | přihlášený | obličeje fotky s bboxem, přiřazením (marker/subjekt), akcí (`create_marker`/`assign_person`/`already_done`) a **návrhy** identit pro nepojmenované — face↔marker IoU matching (viz `internal/facematch`) |
| POST | `/photos/{uid}/faces/assign` | editor/admin | přiřazovací akce `{action, face_index?, marker_uid?, subject_uid?, subject_name?, bbox?}`: `create_marker`/`assign_person`/`unassign_person`; auto-create subjektu dle jména; drží `faces` cache + `marker.reviewed` konzistentní |
| GET | `/faces/clusters` | editor/admin | shluky nepřiřazených obličejů (auto-clustering) → `{clusters:[{uid,size,representative,examples,suggestion?}]}`; `suggestion` = nejbližší pojmenovaný subjekt (viz `internal/cluster`) |
| POST | `/faces/clusters/{id}/assign` | editor/admin | přiřadí **celý shluk** jednomu subjektu `{subject_uid?,subject_name?}` (find-or-create dle jména) → markery pro všechny obličeje; shluk se spotřebuje |
| POST | `/faces/clusters/{id}/remove-face` | editor/admin | odpojí zatoulaný obličej `{photo_uid,face_index}` ze shluku před pojmenováním → refreshnutý shluk (nebo `null` když osiří) |
| GET | `/subjects/{uid}/outliers` | editor/admin | obličeje osoby seřazené dle vzdálenosti od centroidu (nejpodezřelejší první) → `{subject_uid,count,meaningful,faces:[{photo_uid,face_index,bbox,distance,…}]}`; 1–2 obličeje → `meaningful:false` (viz `internal/outliers`); špatný obličej se odpojí přes assign API |
| GET | `/subjects` | přihlášený | seznam subjektů s počty fotek → `{subjects:[{…subject, marker_count}]}` (viz Subjekty / People API) |
| POST | `/subjects` | editor/admin | `{name,type,favorite,private,notes,cover_photo_uid?}` → 201 vytvoří subjekt (prázdné jméno/neznámý typ → 400) |
| GET | `/subjects/{uid}` | přihlášený | detail subjektu (404 chybějící) |
| PATCH | `/subjects/{uid}` | editor/admin | editace `name/type/favorite/private/notes/cover_photo_uid` |
| DELETE | `/subjects/{uid}` | editor/admin | smaže subjekt (markery se odpojí) → 204 |
| GET | `/subjects/{uid}/photos` | přihlášený | paginovaná galerie fotek subjektu → `{photos,total,limit,offset,next_offset}` (newest-first, nearchivované) |
| GET | `/albums` | přihlášený | seznam alb s počty fotek + cover → `{albums:[{…album, photo_count}]}` (viz Alba & štítky API) |
| POST | `/albums` | editor/admin | `{title,description?,type?,cover_photo_uid?,private?,order_by?}` → 201 (prázdný title/neplatný typ → 400) |
| GET | `/albums/{uid}` | přihlášený | detail alba (404 chybějící) |
| PATCH | `/albums/{uid}` | editor/admin | editace `title/description/cover_photo_uid/private/order_by` (strukturální `type` se zachová) |
| DELETE | `/albums/{uid}` | editor/admin | smaže album (členství se odpojí) → 204 |
| POST | `/albums/{uid}/photos` | editor/admin | `{photo_uids:[…]}` přidá fotky za stávající → `{photo_uids:[…]}` (aktuální pořadí) |
| DELETE | `/albums/{uid}/photos` | editor/admin | `{photo_uids:[…]}` odebere fotky → `{photo_uids:[…]}` |
| PATCH | `/albums/{uid}/order` | editor/admin | `{photo_uids:[…]}` přeřadí fotky alba → `{photo_uids:[…]}` |
| GET | `/labels` | přihlášený | seznam štítků s počty fotek → `{labels:[{…label, photo_count}]}` (řazení priority DESC) |
| POST | `/labels` | editor/admin | `{name,priority?}` → 201 (prázdné jméno → 400) |
| GET | `/labels/{uid}` | přihlášený | detail štítku (404 chybějící) |
| PATCH | `/labels/{uid}` | editor/admin | editace `name/priority` |
| DELETE | `/labels/{uid}` | editor/admin | smaže štítek (připojení se odpojí) → 204 |
| POST | `/labels/{uid}/photos` | editor/admin | `{photo_uid,source?,uncertainty?}` připojí štítek k fotce → 204 |
| DELETE | `/labels/{uid}/photos` | editor/admin | `{photo_uid}` odpojí štítek od fotky → 204 |
| GET | `/photos?album={uid}` / `?label={uid}` | přihlášený | scoped výpis fotek alba/štítku přes sdílené `/photos` (ctí filtry/řazení/stránkování, stejný tvar) |
| PATCH | `/photos/{uid}` | editor/admin | částečná úprava `title/description/notes/taken_at/lat/lng/private` (null maže nullable pole) |
| POST | `/photos/{uid}/archive` | editor/admin | soft-delete (nastaví `archived_at`) → vrátí fotku |
| POST | `/photos/{uid}/unarchive` | editor/admin | obnoví archivovanou fotku |
| POST | `/photos/{uid}/purge` | editor/admin | **trvale** smaže archivovanou fotku (řádek+kaskáda, originál, náhledy, případně S3); vyžaduje `?confirm=true` → 204, 400 bez potvrzení, 404 chybí, 409 fotka není archivovaná |
| GET | `/trash/info` | přihlášený | retenční okno `{retention_days}` pro odpočet do auto-purge |
| POST | `/trash/empty` | editor/admin | **trvale** smaže všechny archivované fotky (vyžaduje `?confirm=true`) → `{purged,failed}` |
| GET | `/duplicates` | editor/admin | skupiny pravděpodobných duplikátů (pHash + embedding) → `{groups,total,limit,offset,next_offset}`; query `limit`(≤100)/`offset`; 503 když `duplicate.enabled=false` (viz Duplicates) |
| GET | `/photos/{uid}/thumb/{size}` | session/token | náhled (cache, generuje se on-miss) — streamuje JPEG, `ETag`/304 |
| GET | `/photos/{uid}/download` | session/token | originál jako příloha — streamuje (nikdy celý v RAM), `Content-Length`/`ETag` |
| GET | `/jobs/stats`, `GET /jobs`, `POST /jobs/{id}/requeue` | admin | fronta jobů (viz Admin Jobs API) |
| POST | `/process/embeddings` | admin | backfill — zařadí `image_embed` pro fotky bez embeddingu → `{enqueued}` (viz Process API) |
| POST | `/process/faces` | admin | backfill — zařadí `face_detect` pro fotky bez detekce obličejů → `{enqueued}` (viz Process API) |
| POST | `/process/clusters` | admin | re-clustering — seskupí nepřiřazené obličeje do shluků → `{created}` (viz Process API) |
| GET | `/import/runs` | admin | historie běhů importu/migrace + `sources` flagy → `{runs,limit,offset,sources}` (viz Import admin UI) |
| POST | `/import/photoprism` | admin | zařadí `pp_import` job (jen je-li zdroj konfigurován) → 202 `{job_id,status}`, 409 už běží |
| POST | `/import/photosorter` | admin | zařadí `ps_migrate` job (jen je-li zdroj konfigurován) → 202 `{job_id,status}`, 409 už běží |
| GET | `/backup` | admin | stav S3 zálohy + poslední běh (`configured:false` bez konfigurace) |
| POST | `/backup` | admin | spustí zálohu na pozadí → 202 `{status}`, 409 už běží, 503 bez konfigurace |
| GET | `/restore/dumps` | admin | seznam dumpů v bucketu (nejnovější první) → `{dumps}`, 503 bez konfigurace, 502 při chybě S3 |
| POST | `/restore/verify` | admin | integritní report (fotky v DB vs originály na disku) → `VerifyReport`, 503 bez konfigurace |

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
    jen archiv), `favorite=true` (jen fotky, které **přihlášený uživatel** označil jako oblíbené —
    per-user scope přes korelované `EXISTS` nad `user_favorites`).
  - **Řazení:** `sort` = `newest` (default) / `oldest` / `taken_at` / `added` / `title` / `size`,
    s volitelným `order=asc|desc`.
  - **Stránkování:** `limit` (default 100, max 500) + `offset`. Odpověď nese `total` a
    `next_offset` (null na poslední stránce) pro infinite scroll.
  - **`is_favorite`:** každá fotka v odpovědi (seznam, hledání i detail) nese `is_favorite`
    příznak pro **aktuálního uživatele** (anotace celé stránky jedním `FavoritedAmong` dotazem).
  - **Neplatný parametr → HTTP 400.**
- **Detail** `GET /photos/{uid}` — fotka + `files` (seznam `photo_files`) + `is_favorite`,
  `404` když chybí.
- **Oblíbené** `PUT /photos/{uid}/favorite` + `DELETE /photos/{uid}/favorite` (každý přihlášený,
  oblíbené jsou osobní) — idempotentní toggle oblíbené pro aktuálního uživatele → `204`; `404`
  na chybějící fotku, `503` bez favorites backendu. **Výpis** `GET /favorites` (přihlášený) —
  oblíbené aktuálního uživatele ve stejném tvaru jako `GET /photos` (sdílí filtry/řazení/stránkování,
  je to ekvivalent `GET /photos?favorite=true`). Favorites backend (`FavoriteStore`, splňuje ho
  `organize.Store`) se injektuje přes `Config.Favorites`.
- **Podobné** `GET /photos/{uid}/similar` — fotky seřazené dle rostoucí cosine vzdálenosti
  embeddingu zdrojové fotky, **bez** ní samé; každá nese plný `Photo` + `distance`. `?limit`
  (default 24, max 100). **Empty-friendly:** fotka bez embeddingu (ještě nezpracovaná) → prázdný
  seznam s `200`, neexistující fotka → `404`. Vektorové hledání zajišťuje injektovaný
  `SimilarSearcher` (= `vectors.Store`); sousedi se dohledají batch dotazem `photos.ListByUIDs`.
- **Úprava** `PATCH /photos/{uid}` (editor/admin) — částečná: pole vynechané v těle se nemění,
  explicitní `null` maže nullable pole (`taken_at`/`lat`/`lng`); rozsah souřadnic se validuje
  (`lat ∈ ⟨-90,90⟩`, `lng ∈ ⟨-180,180⟩`).
- **Archivace** `POST /photos/{uid}/archive` + `/unarchive` (editor/admin) — soft-delete přes
  `archived_at`; archivované jsou z výchozího seznamu vyloučené.
- **Koš / trvalé mazání** (`internal/trash`) — archivované fotky se po uplynutí retence
  (`trash.retention_days`, default 30) **natvrdo smažou** plánovaným úklidem v `kukatko serve`
  (každých 6 h, `RunPurge`; retence ≤ 0 ho vypne). Purge smaže DB řádek (kaskáda embeddingů/
  obličejů/markerů/album_photos/photo_labels/phashů/editů/oblíbených přes `ON DELETE CASCADE`),
  originál + cachované náhledy z disku a (je-li nakonfigurován) odpovídající S3 objekt; je
  **idempotentní** a maže artefakty **před** řádkem, takže přerušený běh nechá re-purgovatelný
  řádek místo osiřelých souborů (žádné dangling files). Manuální ovládání:
  `POST /photos/{uid}/purge` (jedna fotka) a `POST /trash/empty` (vše) — obojí vyžaduje
  `?confirm=true`; `GET /trash/info` vrací retenční okno pro odpočet v UI. Seznam koše jede přes
  sdílené `GET /photos?archived=only`. HTTP vrstva (`internal/photoapi/trash.go`) volá purge
  službu přes rozhraní `Purger` (nil → 503); službu staví `buildTrashService`
  (`cmd/kukatko/trash.go`). **Trash UI** je stránka `/trash` (editor/admin) s obnovou a trvalým
  mazáním (jednotlivě i hromadně) a odpočtem do auto-purge.
- **Duplikáty — kontrola a úklid** (`internal/duplicates` + `internal/duplicatesapi`) — vedle
  upload-time varování existuje **review surface**: `GET /duplicates` (editor/admin) vrací
  **skupiny** pravděpodobných duplikátů. Linkuje fotky dvěma signály — perceptual-hash (pHash)
  Hammingovou vzdáleností do `duplicate.phash_max_diff` bitů a embedding cosine vzdáleností do
  `duplicate.embedding_max_dist` — a sloučí hrany do souvislých komponent přes union-find. **Žádný
  O(n²) sken:** pHash používá banded-LSH buckety (pigeonhole na `maxDiff+1` pásem garantuje sdílený
  bucket pro páry do prahu), embeddingy jdou přes HNSW index (`vectors.FindDuplicatePairs`,
  korelovaný `CROSS JOIN LATERAL` s `LIMIT` neighbours per fotka). Každá skupina nese členy
  s detaily pro porovnání (náhled, rozměry, velikost, `taken_at`, vzdálenost ke keeperovi)
  a **navržený keeper** (nejvyšší rozlišení → největší soubor → nejstarší → nejmenší uid); řazení
  skupin largest-first, stránkování `limit`(≤100)/`offset`. Detekce **nikdy nic nemaže** — jen čte.
  Embeddingy se čtou z Postgresu, takže to funguje i když je box offline. Zapojeno `buildDuplicatesAPI`
  (`cmd/kukatko/duplicates.go`); při `duplicate.enabled=false` se route mountuje s nil službou
  a odpovídá 503. **Duplicates UI** je stránka `/duplicates` (editor/admin): skupiny vedle sebe,
  uživatel vybere fotku k zachování a **archivuje zbytek** přes sdílené bulk API
  (`POST /photos/bulk` `{archive:true}` → koš, vratné), nebo skupinu **odmítne** jako „není
  duplikát" (zmizí z pohledu). Žádné auto-mazání, vždy potvrzení uživatelem.
- **Média** `GET /photos/{uid}/thumb/{size}` a `/download` — **streamují** se (`io.Copy`, nikdy
  celý soubor v RAM), s `Cache-Control`/`ETag` (a `304` na `If-None-Match`). Přístup přes session
  cookie **nebo** `download_token` v query parametru `?t=…` (`RequireAuthOrDownloadToken`), takže
  fungují i `<img>`/`<video>` bez cookie. Náhled se na cache-miss vygeneruje on-demand; download
  posílá originál jako `attachment` s `Content-Length` z DB.

### Process API (`internal/processapi`)

Admin-only HTTP API pro hromadné zpracování katalogu (guard `RequireAdmin`), montuje
`buildJobs` (`cmd/kukatko/jobs.go`) přes `server.WithAPI`:

- `POST /api/v1/process/embeddings` → `{enqueued}` — spustí `embedjob.BackfillEmbeddings`:
  zařadí `image_embed` job pro každou fotku bez embeddingu (dedup = no-op), vrátí počet. Recovery
  cesta pro fotky nahrané, když byl box offline, nebo importované před zavedením embeddingů.
- `POST /api/v1/process/faces` → `{enqueued}` — spustí `facejob.BackfillFaces`: zařadí
  `face_detect` job pro každou fotku bez detekce obličejů (dedup = no-op), vrátí počet. Recovery
  cesta stejně jako u embeddingů.
- `POST /api/v1/process/clusters` → `{created}` — spustí `cluster.Recluster`: seskupí dosud
  nepřiřazené, nesclustrované obličeje do shluků téže osoby (souvislé komponenty nad HNSW sousedy do
  prahu cosine vzdálenosti), vrátí počet nově vzniklých shluků. Inkrementální a re-spustitelné —
  nesáhne na přiřazené ani už sclustrované obličeje (viz `internal/cluster`).

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
Výchozí hodnoty se z URL vynechávají (čisté URL), update defaultně pushuje historii
(`{ replace: true }` pro živé psaní).

**Knihovna (`/library`):** hlavní pohled
([`web/src/pages/LibraryPage.tsx`](web/src/pages/LibraryPage.tsx)) je **virtualizovaná, nekonečně
scrollující mřížka náhledů** s filtrovacím panelem. Mřížku renderuje
[`react-virtuoso`](https://virtuoso.dev/) `VirtuosoGrid` (window-scroll, responzivní sloupce přes
CSS `auto-fill`, mountuje jen viditelné řádky); dosažení konce (`endReached`) dotáhne další
stránku. Dlaždice ([`components/library/PhotoTile`](web/src/components/library/PhotoTile.tsx)) jsou
čtvercové, **lazy-load** (`loading="lazy"`, pevný `aspect-ratio` → bez layout-shiftu) a vedou na
detail `/photos/{uid}`. **Filtr-bar** ([`components/library/FilterBar`](web/src/components/library/FilterBar.tsx))
nabízí hledání, řazení (nejnovější/nejstarší/přidané/název/velikost), rozsah dat pořízení,
poloha (GPS), soukromé, fotoaparát a přepínač archivu — **celý stav pohledu (filtry + řazení)
žije v URL** přes `useUrlState`, takže Back/Forward obnoví přesný pohled a sdílení URL ho
reprodukuje. Stránkování řeší hook
[`usePhotoLibrary`](web/src/hooks/usePhotoLibrary.ts) — tenká obálka nad sdíleným
[`usePaginatedPhotos`](web/src/hooks/usePaginatedPhotos.ts) (akumuluje stránky, `loadMore`/`retry`,
reset + refetch při změně dotazu, ruší in-flight requesty a ignoruje stale odpovědi); data čte
přes [`services/photos.ts`](web/src/services/photos.ts) (`fetchPhotos` nad `GET /api/v1/photos`,
`thumbUrl`). Pohled má i18n loading-skeleton, prázdný stav i chybový stav s „Zkusit znovu".

**Hledání (`/search`):** stránka
([`web/src/pages/SearchPage.tsx`](web/src/pages/SearchPage.tsx)) s **prominentním vyhledávacím
polem** (přítomným i v navbaru přes [`components/NavbarSearch`](web/src/components/NavbarSearch.tsx))
a **přepínačem režimu** (hybridní – výchozí / fulltext / sémantické). **Dotaz i režim žijí v URL**
(`?q=…&mode=hybrid`) přes `useUrlState`, takže Back funguje a URL je sdílitelná; psaní je
**debouncované** (350 ms) než se zapíše do URL a spustí dotaz. Výsledky se renderují ve **stejné
virtualizované mřížce** jako knihovna a sdílí `FilterBar` (s prop `showSearch`/`showSort` skrytými,
protože dotaz a relevance řízené řazení patří hledání). Data čte hook
[`usePhotoSearch`](web/src/hooks/usePhotoSearch.ts) (nad `usePaginatedPhotos`) přes `searchPhotos`
nad `GET /api/v1/search`; prázdný dotaz je `idle` stav (žádný request, výzva uživateli). Když je
**inferenční služba offline**, backend vrátí `degraded: true` a pohled zobrazí **neblokující
upozornění**, že sémantické hledání je dočasně nedostupné (výsledky padnou na fulltext). Pohled má
i18n idle/loading/empty/error stavy. Mapování URL ↔ stav je v
[`lib/searchView.ts`](web/src/lib/searchView.ts) (`SearchView`, `SEARCH_DEFAULTS`, `toMode`).

**Podobné fotky:** znovupoužitelná komponenta
[`components/library/SimilarPhotos`](web/src/components/library/SimilarPhotos.tsx) (pro pozdější
detail fotky) načte `GET /api/v1/photos/{uid}/similar` přes `fetchSimilar` a zobrazí **horizontálně
scrollovatelný pruh** náhledů podobných fotek, každý odkazující na svůj detail. Je empty-friendly
(fotka bez embeddingu → prázdná odpověď → nic nerenderuje), má loading/error stav a refetchuje při
změně `uid`.

**Alba (`/albums`, `/albums/{uid}`):** [`AlbumsPage`](web/src/pages/AlbumsPage.tsx) je responzivní
mřížka karet alb ([`components/organize/AlbumTile`](web/src/components/organize/AlbumTile.tsx) —
cover, název, počet fotek), každá vede na detail. Editoři/admini mají tlačítko **Nové album**
([`AlbumEditModal`](web/src/components/organize/AlbumEditModal.tsx) = create/rename, popis,
soukromé). [`AlbumDetailPage`](web/src/pages/AlbumDetailPage.tsx) ukazuje hlavičku (název, badge
soukromé) s editorskými akcemi (upravit/smazat/**vybrat**/**přeřadit**) nad fotomřížkou
**scopnutou na album** přes sdílené `GET /photos?album={uid}` (hook
[`useScopedPhotos`](web/src/hooks/useScopedPhotos.ts), stejný `FilterBar` + URL stav jako knihovna).
**Přeřazení** ([`ReorderableGrid`](web/src/components/organize/ReorderableGrid.tsx)) jde
drag-and-dropem i šipkami (přístupné) a ukládá se přes `PATCH /albums/{uid}/order`; výběr
([`useSelection`](web/src/hooks/useSelection.ts) + [`SelectionBar`](web/src/components/organize/SelectionBar.tsx))
umí odebrat fotky z alba nebo nastavit obálku.

**Štítky (`/labels`, `/labels/{uid}`):** [`LabelsPage`](web/src/pages/LabelsPage.tsx) je seznam
štítků s počty fotek; editoři je vytvářejí/přejmenovávají/mažou
([`LabelEditModal`](web/src/components/organize/LabelEditModal.tsx) = jméno + priorita). Klik na
štítek otevře [`LabelDetailPage`](web/src/pages/LabelDetailPage.tsx) — fotomřížka scopnutá na
štítek přes `GET /photos?label={uid}` (opět `useScopedPhotos` + `FilterBar` + URL stav).

**Hromadná úprava z výběru:** knihovna má pro editory **režim výběru** (`useSelection`):
dlaždice se přepnou na zaškrtávací (`PhotoTile` `selectable`), sticky `SelectionBar` ukáže počet
a nabídne **Vybrat vše** (select-all-in-view přes `useSelection.selectMany`) a **Hromadná úprava**
přes [`BulkEditModal`](web/src/components/organize/BulkEditModal.tsx). Modal načte alba/štítky a
v jednom `POST /photos/bulk` ([`services/bulk.ts`](web/src/services/bulk.ts)) aplikuje libovolnou
podmnožinu operací — **přidat/odebrat album**, **přidat/odebrat štítek**, **nastavit/vymazat popis**,
**nastavit/vymazat polohu**, **soukromé**, **archiv**, **oblíbené** (per-user); set/clear páry jsou
samostatné módy, souřadnice se validují klientsky a vyžaduje se aspoň jedna změna. Po aplikaci se
místo formuláře zobrazí **per-foto result summary** (kolik upraveno/přeskočeno/selhalo + seznam
chyb) z odpovědi. Alba/štítky API volá [`services/organize.ts`](web/src/services/organize.ts)
(CRUD + členství/připojení), `photos.ts` přidává `album`/`label` scope do `PhotoListParams`. Odkazy
**Alba** a **Štítky** jsou v navbaru.

**Oblíbené (`/favorites` + srdíčko všude):** každá dlaždice v knihovně i hlavička detailu fotky
nesou **heart toggle** ([`FavoriteButton`](web/src/components/library/FavoriteButton.tsx) nad hookem
[`useFavorite`](web/src/hooks/useFavorite.ts)) — **optimistický** per-user toggle nad
`PUT`/`DELETE /photos/{uid}/favorite` (`favoritePhoto` v `photos.ts`) s **rollbackem** při chybě.
Oblíbení je osobní akce **dostupná i prohlížečům** (bez role-gate), na rozdíl od hromadné úpravy
(jen editor/admin). Stránka [`FavoritesPage`](web/src/pages/FavoritesPage.tsx) (`/favorites`, odkaz
v navbaru) je stejná mřížka/filtry jako knihovna, scopnutá `favorite=true`, takže fotku lze z
oblíbených odebrat přímo na místě. Každá fotka v seznamu/hledání/detailu nese `is_favorite` pro
aktuálního uživatele.

**Multiupload (`/upload`, editor/admin):** stránka
([`web/src/pages/UploadPage.tsx`](web/src/pages/UploadPage.tsx)) pro hromadné nahrávání fotek/videí
včetně **mobilu**. [`components/upload/DropZone`](web/src/components/upload/DropZone.tsx) nabízí
**drag-and-drop** zónu i file input (`multiple`, `accept="image/*,video/*"` → na mobilu otevře
galerii) a samostatné tlačítko **Vyfotit** (`capture="environment"` → fotoaparát). Frontu řídí hook
[`useUploadQueue`](web/src/hooks/useUploadQueue.ts): přidávání/odebírání souborů (dedup podle
jméno+velikost+mtime), upload s **konkurenčním stropem** (`MAX_CONCURRENT_UPLOADS`, výchozí 3),
**per-file progress** a koncový stav (nahráno / duplikát / selhalo) se souhrnem počtů, **retry**
neúspěšných (jednotlivě i hromadně) a abort běžících. Každý soubor jde **vlastním** `POST
/api/v1/upload` requestem přes [`services/upload.ts`](web/src/services/upload.ts) (`uploadFile` nad
**`XMLHttpRequest`** kvůli upload-progress eventům; FormData se streamuje, nikdy celé v RAM). Per-file
[`UploadItem`](web/src/components/upload/UploadItem.tsx) ukazuje progress-bar, status badge a
**near-duplicate varování** z API (neblokuje). Po dokončení nabídne odkaz na nově nahrané fotky
v knihovně (`/library?sort=added`). Vše i18n (cs/en), touch-friendly. Odkaz **Nahrát** v navbaru je
viditelný jen pro editory/adminy.

**Mapa (`/map`):** stránka ([`web/src/pages/MapPage.tsx`](web/src/pages/MapPage.tsx)) zobrazuje
geotagované fotky jako **shlukované markery** nad dlaždicemi [mapy.com](https://mapy.com) přes
**[Leaflet](https://leafletjs.com/) + [Leaflet.markercluster](https://github.com/Leaflet/Leaflet.markercluster)**.
Dlaždicová vrstva míří na **backendovou proxy** (`/api/v1/map/tiles/{mapset}/{z}/{x}/{y}{r}`), takže
**API klíč nikdy neopustí server**; `{r}` se na retina displejích změní na `@2x`. Imperativní
Leaflet logika je izolovaná v [`components/map/LeafletMap`](web/src/components/map/LeafletMap.tsx)
(most React props → Leaflet přes efekty: jednorázový setup, výměna URL dlaždic při změně mapsetu,
přestavba markerů při změně fotek). **Povinné ovládací prvky mapy.com** jsou vždy přítomné:
attribution s odkazem „© Seznam.cz a.s. a další" (→ `mapy.com/copyright`) a **klikatelné logo**
vlevo dole odkazující na `mapy.com`. Klik na **shluk** přibližuje (default markercluster), klik na
**marker** otevře popup s náhledem ([`lib/mapPopup.ts`](web/src/lib/mapPopup.ts)) odkazujícím na
detail fotky (`/photos/{uid}`, SPA navigace). **Přepínač podkladu** (základní/turistická/letecká)
a **filtry** (rozsah dat, archiv, soukromé) jsou v
[`components/map/MapFilterBar`](web/src/components/map/MapFilterBar.tsx). **Stav žije v URL** přes
`useUrlState` ([`lib/mapView.ts`](web/src/lib/mapView.ts) — mapset, viewport `lat`/`lng`/`z`,
filtry), takže Back/Forward i sdílení URL reprodukují mapu; **posun/zoom** zapisuje viewport bez
refetche, **změna filtru** dotáhne GeoJSON znovu. Data čte hook
[`useMapPhotos`](web/src/hooks/useMapPhotos.ts) přes `fetchMapPhotos` nad `GET /api/v1/map/photos`
([`services/map.ts`](web/src/services/map.ts), GeoJSON FeatureCollection). Pohled má i18n
loading/empty/error stavy a je responzivní/touch. Odkaz **Mapa** je v navbaru.

**Slideshow (`/slideshow`):** fullscreen promítání fotek
([`web/src/pages/SlideshowPage.tsx`](web/src/pages/SlideshowPage.tsx)) spustitelné tlačítkem
**Promítání** z detailu **alba**, **štítku** i z (filtrované) **knihovny**. Cílový pohled si nese
**stejné řazení/filtry** jako mřížka, ze které se spouští — launch link staví
[`lib/slideshowView.ts`](web/src/lib/slideshowView.ts) (`slideshowHref`) z aktuálního URL stavu,
takže scope (`?album=`/`?label=`) i filtry round-trippují přes URL a **Zpět** se vrací do
předchozího pohledu. Stránka pageuje katalog přes sdílený
[`usePaginatedPhotos`](web/src/hooks/usePaginatedPhotos.ts) (`fetchPhotos`), takže **velké sady se
nenačítají najednou**. Routa žije **mimo layout shell** (bez navbaru), aby zabrala celý viewport.

Přehrávání řídí hook [`useSlideshow`](web/src/hooks/useSlideshow.ts): vlastní index + play/pause,
**auto-advance na nastavitelný interval** (setTimeout, manuální další/předchozí resetuje odpočet),
wrap-around na konci, a **prefetch dalších stránek** (`PRELOAD_AHEAD` snímků dopředu přes
`onLoadMore`) — na samém konci s další stránkou počká místo zacyklení. Prázdná sada je no-op.
Prezentační vrstva [`components/slideshow/Slideshow`](web/src/components/slideshow/Slideshow.tsx)
zobrazí aktuální fotku v **preview velikosti** (`fit_1920`), **přednačítá sousední snímky**
(`new Image()`), a nese ovládání **předchozí / play-pause / další / celá obrazovka / nastavení /
zavřít** plus titulek a pozici `n / total`. **Klávesy** (←/→ navigace, mezerník play/pause, Esc
ukončí nebo opustí fullscreen, F fullscreen) a **dotyk** (vodorovný swipe) fungují na mobilu/tabletu;
Fullscreen API se feature-detectuje. **Efekt přechodu** (prolnutí / posun / bez efektu, CSS v
[`slideshow.css`](web/src/components/slideshow/slideshow.css)) a **rychlost** se volí v panelu
nastavení a **persistují do `localStorage`** přes [`useSlideshowSettings`](web/src/hooks/useSlideshowSettings.ts)
+ [`lib/slideshowSettings.ts`](web/src/lib/slideshowSettings.ts) (sanitizace při čtení i zápisu),
takže volba přežije reload a další promítání. Vše i18n (cs/en).

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
