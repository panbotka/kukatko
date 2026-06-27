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
- **Audit log** (`internal/audit`, migrace `0012_audit_log.sql`, tabulka `audit_log`) — `Write(ctx,
  exec, Entry)` zapisuje přes libovolný executor (pool **nebo** `pgx.Tx`), takže bulk zápis vloží
  audit řádek do **téže transakce** jako mutace. `Store.Record`/`List` pro samostatný zápis a
  admin/test čtení; sloupce `actor_uid` (FK users `ON DELETE SET NULL`), `action`, `target_type`,
  `target_uid`, `details JSONB`, `created_at`.
- **Vrstvy** — `bulk.Service` (`NewService(pool, maxBatch)`, `Apply(ctx, actorUID, photoUIDs, ops)`)
  drží transakční logiku a vlastní SQL (vlastní tx kvůli atomicitě), `bulkapi` dělá HTTP +
  validaci payloadu. Mountuje se dalším `server.WithAPI` (`buildBulkAPI` v `cmd/kukatko/bulk.go`).

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
| GET | `/photos/{uid}/thumb/{size}` | session/token | náhled (cache, generuje se on-miss) — streamuje JPEG, `ETag`/304 |
| GET | `/photos/{uid}/download` | session/token | originál jako příloha — streamuje (nikdy celý v RAM), `Content-Length`/`ETag` |
| GET | `/jobs/stats`, `GET /jobs`, `POST /jobs/{id}/requeue` | admin | fronta jobů (viz Admin Jobs API) |
| POST | `/process/embeddings` | admin | backfill — zařadí `image_embed` pro fotky bez embeddingu → `{enqueued}` (viz Process API) |
| POST | `/process/faces` | admin | backfill — zařadí `face_detect` pro fotky bez detekce obličejů → `{enqueued}` (viz Process API) |
| POST | `/process/clusters` | admin | re-clustering — seskupí nepřiřazené obličeje do shluků → `{created}` (viz Process API) |

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
