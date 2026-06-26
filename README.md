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
| GET | `/search?q=&mode=` | přihlášený | sémantické + hybridní hledání; `mode` = `fulltext`/`semantic`/`hybrid` (default `hybrid`): fulltext (tsvector + unaccent, `ts_rank`), semantic (CLIP text→embedding → cosine HNSW), hybrid (fúze obou přes **Reciprocal Rank Fusion**, k=60, dedup); všechny módy ctí list filtry + stránkování; `q` povinný → tvar jako `/photos` + `mode`+`degraded`; box offline → fallback na fulltext s `degraded:true` |
| GET | `/photos/{uid}` | přihlášený | plný detail fotky (metadata, EXIF, GPS) + `files` |
| GET | `/photos/{uid}/similar` | přihlášený | vizuálně podobné fotky dle cosine vzdálenosti embeddingu (`?limit`, default 24, max 100) → `{similar:[{…photo, distance}]}` |
| GET | `/photos/{uid}/faces` | přihlášený | obličeje fotky s bboxem, přiřazením (marker/subjekt), akcí (`create_marker`/`assign_person`/`already_done`) a **návrhy** identit pro nepojmenované — face↔marker IoU matching (viz `internal/facematch`) |
| POST | `/photos/{uid}/faces/assign` | editor/admin | přiřazovací akce `{action, face_index?, marker_uid?, subject_uid?, subject_name?, bbox?}`: `create_marker`/`assign_person`/`unassign_person`; auto-create subjektu dle jména; drží `faces` cache + `marker.reviewed` konzistentní |
| PATCH | `/photos/{uid}` | editor/admin | částečná úprava `title/description/notes/taken_at/lat/lng/private` (null maže nullable pole) |
| POST | `/photos/{uid}/archive` | editor/admin | soft-delete (nastaví `archived_at`) → vrátí fotku |
| POST | `/photos/{uid}/unarchive` | editor/admin | obnoví archivovanou fotku |
| GET | `/photos/{uid}/thumb/{size}` | session/token | náhled (cache, generuje se on-miss) — streamuje JPEG, `ETag`/304 |
| GET | `/photos/{uid}/download` | session/token | originál jako příloha — streamuje (nikdy celý v RAM), `Content-Length`/`ETag` |
| GET | `/jobs/stats`, `GET /jobs`, `POST /jobs/{id}/requeue` | admin | fronta jobů (viz Admin Jobs API) |
| POST | `/process/embeddings` | admin | backfill — zařadí `image_embed` pro fotky bez embeddingu → `{enqueued}` (viz Process API) |
| POST | `/process/faces` | admin | backfill — zařadí `face_detect` pro fotky bez detekce obličejů → `{enqueued}` (viz Process API) |

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
