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
  Virtualizace dlouhých mřížek/seznamů přes **`react-virtuoso`**.
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
  `MediaType` image/video/live, `FileRole` original/sidecar/edited, UID generátor prefix `ph`,
  `Store` nad pgx s
  `Create`/`GetByUID`/`GetByFileHash`/`GetByPhotoprismUID`/`GetByPhotosorterUID`/`ListByUIDs`
  (batch lookup dle uid, ignoruje neznámé — pro similar API)/`FilterUIDs`
  (z dané množiny uid vrátí ty, co projdou strukturálními List filtry — ignoruje řazení,
  stránkování i `FullText`; companion k sémantickému hledání: caller drží kandidáty z
  embeddings indexu a profiltruje je list filtry, pořadí dle podobnosti si řadí sám)/
  `UpdateMetadata`/`Archive`/`Unarchive`/`Delete`/`List`+`Count` (filtry archived/private/
  uploader/has-GPS/date-range `taken_after`+`taken_before`/camera/lens/substring search,
  řazení taken_at/created_at/uid/title/file_size, stránkování limit/offset; `Count` sdílí
  `buildWhere` filtry pro `total`)/`Search` (česky-aware fulltext nad generovaným `fts
  tsvector` sloupcem: `ListParams.FullText` přes `websearch_to_tsquery('simple',
  immutable_unaccent(q))`, řazení dle `ts_rank` (title>description>notes>file_name),
  diakritika necitlivá, ctí všechny List filtry + stránkování; prázdný dotaz →
  `ErrEmptySearch`; `Count` s `FullText` vrací total díky sdílenému `buildWhere`),
  plus `CreateFile`/`ListFiles`,
  `SetPhash`/`GetPhash`, `SetEdit`/`GetEdit`; dedup na SHA256 `file_hash` + externí ID
  `photoprism_uid`/`photoprism_file_hash`(SHA1)/`photosorter_uid`; tabulky v migraci
  `0003_photos.sql`: `photos`, `photo_files` (jeden primary/foto), `photo_phashes`,
  `photo_edits` (all-or-nothing crop, rotace 0/90/180/270); video sloupce v migraci
  `0004_video.sql` (`media_type` image/video/live CHECK+partial index, `duration_ms`,
  `video_codec`, `audio_codec`, `has_audio`, `fps`); generovaný `fts tsvector` sloupec +
  GIN index a IMMUTABLE `immutable_unaccent` wrapper v migraci `0007_fts.sql` (fulltext,
  `setweight` A/B/C/D, `to_tsvector('simple', immutable_unaccent(...))`, `file_name`
  normalizován regexem na tokeny; generated column drží `fts` aktuální i po editaci
  metadat bez triggeru); FK `ON DELETE CASCADE`
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
  (HEIC/RAW/video → dekódovatelný JPEG, **shell-out**: `EnsureDecodable(ctx,path)` →
  (cesta, cleanup, err); JPEG/PNG/WebP passthrough, **HEIC** přes `heif-convert` na temp JPEG,
  **RAW** (cr2/cr3/nef/arw/dng/raf/orf/rw2/pef/srw) vytáhne embedded preview přes
  `exiftool -b -PreviewImage` (fallback `-JpgFromRaw`/`-ThumbnailImage`) místo demosaicu,
  **video** (`FormatVideo`) deleguje na `video.ExtractPoster` (poster frame přes `ffmpeg`) —
  thumbnailer i pHash zpracují poster jako fotku; `DetectFormat`/`IsSupportedFormat`; sentinely
  `ErrConverterMissing`/`ErrUnsupportedFormat`/`ErrNoEmbeddedPreview`; chybějící nástroj = jasná
  chyba), `internal/video/`
  (video bez CGO, **shell-out** na FFmpeg suite: `Probe(ctx,path) (Metadata,error)` přes
  `ffprobe -print_format json -show_format -show_streams` → `DurationMs`/`VideoCodec`/`AudioCodec`/
  `HasAudio`/`FPS` (parsing racionálu)/rozměry/`TakenAt` (creation_time)/GPS (ISO 6709), **fallback
  na `exiftool`** přes `internal/exif` když `ffprobe` chybí; `ExtractPoster(ctx,path)` →
  reprezentativní snímek přes `ffmpeg` (~1 s, fallback první frame) na temp JPEG + once-cleanup;
  `IsVideoPath`/`IsVideoExt`/`FFmpegAvailable`/`FFprobeAvailable`; sentinely `ErrFFmpegMissing`/
  `ErrFFprobeMissing`/`ErrNoMetadataTool`/`ErrPosterFailed`), `internal/exif/`
  (extrakce EXIF/GPS metadat při importu, **CGO-free**: `Extract(ctx,path) (Metadata,error)`
  → `TakenAt`+`TakenAtSource` (`exif`/`filename`/`unknown`), `Lat`/`Lng`/`Altitude`,
  `CameraMake`/`CameraModel`/`LensModel`, `ISO`/`Aperture`/`Exposure`/`FocalLength`,
  `Width`/`Height`/`Orientation`, `Mime` a plný EXIF jako JSON-able mapa — mapuje se 1:1 na
  `photos.Photo`; **primárně** shell-out `exiftool -json -n`, **fallback** pure-Go
  `rwcarlsen/goexif` (+ `image.DecodeConfig`/`http.DetectContentType` pro rozměry/MIME) když
  `exiftool` chybí/selže; GPS rational→desetinné stupně dle `N/S/E/W` refů, `GPSAltitudeRef=1`
  → záporná výška; `taken_at` z `DateTimeOriginal` (zóna-prosté = UTC), jinak z názvu souboru,
  jinak `unknown`; soubor bez EXIF (PNG) = nulové hodnoty, **ne error**), `internal/phash/`
  (perceptuální hashe, **CGO-free**: `Compute(img) Hashes{Phash,Dhash int64}` — **pHash** přes
  2-D DCT 32×32 → low-freq 8×8 blok s prahem medián-bez-DC, **dHash** gradientní 9×8; `Distance(a,b)`
  = Hammingova vzdálenost přes `bits.OnesCount64`; near-dup = malá vzdálenost), `internal/ingest/`
  (upload/ingest pipeline: `Service` = `New(Config{Storage,Photos,Thumbnailer,Enqueuer,Duplicate,
  MaxFileSize,TempDir})` s `Ingest(ctx,src,filename,uploadedBy) FileResult` — streamuje do tempu +
  SHA256, exact-dup check, metadata (`mediaMeta`: **foto** → EXIF; **video** dle `video.IsVideoPath`
  → `media_type=video` + `video.Probe`, vyžaduje `ffmpeg` jinak per-file error `ErrFFmpegMissing`,
  `taken_at` fallback na původní jméno přes `exif.FilenameTakenAt`), `storage.Store` (`YYYY/MM`),
  insert `photos` (vč. video sloupců)+primární `photo_files`, pHash/dHash → `photo_phashes`
  (u videa z poster framu), náhledy (u videa poster), enqueue jobů (poster frame se účastní
  search/people); **per-file** `FileResult{Filename,Status,
  Outcome (created/duplicate/error),PhotoUID,Error,Warnings}` — nikdy nevrací error, vše v resultu;
  **race**: souběžné identické uploady → jedna fotka (storage hard-link + unique `file_hash`), poražený
  čistá duplicita; **near-dup warning** config-gated přes `photos.NearestPhash`; `JobEnqueuer` =
  TODO hook `EnqueueImageEmbed`/`EnqueueFaceDetect`, default `NopEnqueuer` než vznikne fronta;
  `API` = `NewAPI(svc, requireWrite)` + `RegisterRoutes` mountuje `POST /upload` za `RequireWrite`;
  multipart se streamuje part-by-part, nikdy celý soubor v RAM), `internal/photoapi/`
  (read/curace HTTP API nad katalogem: `NewAPI(Config{Store,Storage,Thumbnailer,Similar,
  Embedder,RequireAuth,RequireWrite,RequireDownload})` + `RegisterRoutes` mountuje `/photos`
  **a `GET /search`**; `parseListParams`
  validuje query → `photos.ListParams` (`limit`≤500/`offset`, `sort`
  newest/oldest/taken_at/added/title/size + `order`, `archived` false/true/only, `private`,
  `has_gps`, `taken_after`/`taken_before`, `camera`, `lens`, `uploader`, `q`; neplatný → 400),
  list vrací `{photos,total,limit,offset,next_offset}` pro infinite scroll;
  `GET /search?q=&mode=` (`handleSearch`, `search.go`) = **sémantické + hybridní hledání**,
  `mode` = `fulltext`|`semantic`|`hybrid` (default `hybrid`, neznámý → 400), `q` povinný
  (prázdný/whitespace → 400): **fulltext** řadí dle `ts_rank` přes `store.Search`; **semantic**
  embedne `q` přes `TextEmbedder` (sidecar) → `Similar.FindSimilar` (cosine HNSW) →
  profiltruje kandidáty `store.FilterUIDs` → řadí dle vzdálenosti; **hybrid** sloučí oba
  rankingy **Reciprocal Rank Fusion** (`fuseRRF`, konstanta `rrfK=60`), dedup, řadí dle
  fúzního skóre. Všechny módy ctí List filtry + stránkování (`sort`/`order` ignorovány),
  odpověď = list tvar + `mode` (efektivní) + `degraded`; **box offline** (`Embedder` nil nebo
  `embedding.IsUnavailable`) → `semantic`/`hybrid` spadnou na fulltext s `degraded: true`;
  `TextEmbedder` interface (fakeovatelný, splňuje ho `embedding.Client`); `PATCH` je
  částečný přes raw-key presence (vynechané pole beze změny, `null` maže nullable, validace
  souřadnic); média `thumb/{size}`+`download` **streamují** přes `io.Copy` se `streamMedia`
  (`Cache-Control`/`ETag`/`304`, `Content-Length` z DB, náhled generován on-miss),
  guard `RequireAuthOrDownloadToken` = session cookie nebo `?t=download_token`), `internal/jobs/`
  (persistentní fronta jobů v Postgresu, **hlavní robustnost proti photo-sorteru** —
  joby přežijí restart, retryují, dedupují, čekají když je box offline; tabulka `jobs` v migraci
  `0005_jobs.sql`: `state` queued/running/done/failed/dead, `priority`, `payload` JSONB,
  `attempts`/`max_attempts` (default 5), `run_after` backoff, `locked_by`/`locked_at`; index
  `(state, run_after, priority)` + **dedup** partial unique na `(type, payload->>'photo_uid')
  WHERE state IN (queued,running)`; `Store` = `NewStore(pool)` s
  `Enqueue(ctx,type,payload,opts)` (idempotentní na dedup klíč → `ErrDuplicate`,
  `EnqueueOptions{Priority,MaxAttempts,RunAfter}`),
  `Claim(ctx,workerID,types...)` (atomicky přes `SELECT … FOR UPDATE SKIP LOCKED`,
  `run_after<=now()`, řazení priority DESC/run_after ASC/id ASC, mark running+lock →
  prázdná fronta `ErrNoJobs`), `Complete`/`Fail(err)` (inkrement attempts → requeue s
  exponenciálním backoffem přes `run_after` base 30 s/cap 1 h, jinak `state=dead`+`last_error`),
  `Defer(id,delay)` (requeue na `now()+delay` **bez** započtení pokusu — offline box počká bez
  spálení retry budgetu), `Heartbeat`/`RecoverStaleLocks(staleAfter)` (zastaralý zámek = mrtvý worker → requeue jako pokus),
  helpery `CountsByState`/`CountsByType`/`ListDead`/`RequeueDead`/`Requeue` (dead **i**
  failed → queued, pro admin endpoint)/`List`(`ListOptions{State,Limit,Offset}`, řazení
  updated_at DESC, limit cap 500, pro admin výpis)/`Get`; sentinely
  `ErrDuplicate`/`ErrNoJobs`/`ErrJobNotFound`/`ErrNotDead`; **typy jobů** `image_embed`/
  `face_detect`/`thumbnail`/`pp_import`/`ps_migrate`/`backup`; `Enqueuer` = `NewEnqueuer(store)`
  implementuje `ingest.JobEnqueuer` (`EnqueueImageEmbed`/`EnqueueFaceDetect`, `ErrDuplicate`=no-op)),
  `internal/worker/`
  (in-process background worker runtime, **hlavní exekuční smyčka fronty**: `Registry` =
  `NewRegistry()`+`Register(type, HandlerFunc)`+`Handler`/`Types` (panika na prázdný typ/nil
  handler/duplicitní registraci); `HandlerFunc` = `func(ctx, jobs.Job) error`; `Worker` =
  `New(Config{Queue,Registry,Concurrency,PollInterval,StaleAfter,StaleScanInterval,IDPrefix})`
  s `Run(ctx)` — spustí `Concurrency` goroutin pollujících `Claim` (filtr na registrované
  `Types`), dispatch na handler dle `job.Type`, `Complete`/`Fail` dle výsledku přes
  **shutdown-immune** bookkeeping kontext (`context.WithoutCancel`), plus stale-lock recovery
  ticker; `Queue` interface = podmnožina `jobs.Store` (`Claim`/`Complete`/`Fail`/`Defer`/
  `RecoverStaleLocks`) pro testovatelnost; **graceful shutdown** = ctx cancel zastaví claiming,
  job běžící při shutdownu je opuštěn (lock recoveruje fronta), panika handleru →
  `ErrHandlerPanic` (job fail, ne crash), neznámý typ → `ErrNoHandler`; handler může vrátit
  `RetryAfter(delay,cause)`/`RetryAfterError` → worker místo `Fail` zavolá `Defer(delay)` (přechodná
  bezchybná chyba, žádný spálený pokus — používá `image_embed` při offline boxu); built-in **noop**
  handler (`TypeNoop`/`NoopHandler`/`RegisterBuiltins`) jen pro sanity/testy; `Run` vrací nil),
  `internal/jobsapi/`
  (admin-only HTTP API nad frontou: `NewAPI(Config{Store,RequireAdmin})`+`RegisterRoutes`
  mountuje `/jobs`; `GET /jobs/stats` (counts by_state/by_type+total), `GET /jobs`
  (recent/dead-letter výpis, query `state`/`limit`≤500/`offset`, neplatný → 400),
  `POST /jobs/{id}/requeue` (dead/failed → queued; 404 missing, 409 ne-requeueable);
  frontend polluje, žádné SSE), `internal/embedding/`
  (HTTP klient k inferenčnímu sidecaru na **boxu**, stejný kontrakt jako photo-sorter, vše za
  rozhraním `Client` (fakeovatelné v testech): `New(Config{BaseURL,ImageDim,FaceDim,
  RequestTimeout,HealthTimeout,HealthPath,HTTPClient})` → `*HTTPClient`; `ImageEmbedding(ctx,
  img io.Reader)`/`TextEmbedding(ctx,text)` → 768-dim CLIP vektor + `model`/`pretrained`
  (`POST /embed/image` multipart `file` streamovaný přes `io.Pipe` / `POST /embed/text` JSON
  `{text}`), `FaceEmbeddings(ctx,img)` → `[]Face` (512-dim embedding, `BBox [4]float64`
  v px `[x1,y1,x2,y2]`, `DetScore`)+`model` (`POST /embed/face` multipart `file`),
  `Healthy(ctx) bool` (probe `GET /health`, jakákoli HTTP odpověď = box dostupný, jen
  transport-error/timeout = offline); **box offline-aware typové chyby** `ErrUnavailable`
  (transport selhal / status 502/503/504, retryable — helper `IsUnavailable`) vs `ErrBadResponse`
  (chybná odpověď) vs `ErrDimMismatch` (validace rozměrů 768/512) vs `ErrInvalidURL`; zrušený
  kontext se nevydává za nedostupnost; per-request timeouty přes context (default request 60 s /
  health 5 s), nikdy nedrží obrázek celý v RAM), `internal/vectors/`
  (DB vrstva pro embeddingy a obličeje, **uloženo přímo v Postgresu** jako `halfvec` (float16)
  sloupce s HNSW cosine indexy — tabulky `embeddings`/`faces` v migraci `0006_embeddings.sql`;
  `halfvec` místo `vector` půlí paměť HNSW indexu při zanedbatelné ztrátě recall na
  normalizovaných CLIP/ArcFace vektorech (důležité na Pi); `Store` = `NewStore(pool)` nad
  sdíleným pgx poolem:
  `SaveEmbedding`(upsert)/`GetEmbedding`(`ErrEmbeddingNotFound`)/`FindSimilar(vec,limit,maxDistance)`
  pro 768-dim obrázkové embeddingy, `SaveFaces`(idempotentní replace v transakci)/`ListFaces`/
  `ListFacesBySubject(subjectUID)` (obličeje s daným `subject_uid`, řazení `(photo_uid,
  face_index)` — podklad pro outlier detekci; sdílí `queryFaces`/`scanFace` se `ListFaces`)/
  `DeleteFaces`/`FindSimilarFaces`/`FindSimilarFaceCandidates` (jako `FindSimilarFaces`, ale
  vrací i cache `subject_uid`/`subject_name`/`marker_uid` + `bbox` — podklad pro návrhy identit)/
  `UpdateFaceMarker(photoUID,faceIndex,markerUID,subjectUID,subjectName)` (zapíše cache sloupce na
  jeden obličej, prázdný marker/subject → `NULL`; tudy se cachuje IoU match) pro 512-dim face
  embeddingy + cache sloupce
  marker_uid/subject_uid/subject_name/photo_width/photo_height/orientation a normalizovaný
  `bbox DOUBLE PRECISION[4]` `[x,y,w,h]`; podobnost přes `embedding <=> $vec` (cosine, nejbližší
  první) v **read-only transakci** se `SET LOCAL hnsw.ef_search = 100`; `limit` ořez `[1,500]`,
  nekladný `maxDistance` filtr vypne; helpery `ToHalfVec`/`FromHalfVec` (`[]float32` ↔
  `pgvector.HalfVector`) a **sdílená vektorová matematika** `Centroid`(L2-normalizovaný
  element-wise průměr)/`Normalize`/`CosineDistance` v `math.go` (jediná implementace, kterou
  znovupoužívá i `internal/cluster` i `internal/outliers`); sentinely
  `ErrEmbeddingNotFound`/`ErrDimMismatch` (validace 768/512)/
  `ErrFaceIndexTaken` (UNIQUE `(photo_uid,face_index)`); `ListPhotosMissingEmbedding(limit)` =
  uid nearchivovaných fotek bez embeddingu (LEFT JOIN, nejnovější první, `limit<=0`=vše) pro
  backfill; **face-detection tracking** v tabulce `face_detections` (migrace
  `0009_face_detections.sql`: `photo_uid PK` FK `ON DELETE CASCADE`, `face_count`, `model`,
  `detected_at`) — protože `faces` může mít nula řádků, je to jediný způsob, jak odlišit fotku
  bez obličejů od nezpracované; `RecordFaceDetection(uid,faces,model)` (atomicky nahradí faces
  fotky **a** upsertne `face_detections` řádek — i pro nula obličejů; sdílí `replaceFaces` tx
  helper se `SaveFaces`), `FacesDetected(uid)` (existuje řádek?), `ListPhotosMissingFaces(limit)`
  (uid fotek bez `face_detections` řádku, jako `ListPhotosMissingEmbedding`); FK
  `ON DELETE CASCADE` — mazání fotky
  smaže embeddingy, faces i face_detections, oprava photo-sorter mezery se sirotky),
  `internal/people/`
  (DB vrstva pro **subjekty** (osoby/zvířata/jiné) a **markery** (face/label regiony na
  fotkách), tabulky `subjects`/`markers` v migraci `0008_subjects_markers.sql`: `subjects`
  = `uid PK` (prefix `su`), `slug UNIQUE`, `name`, `type IN (person|pet|other)`, `favorite`,
  `private`, `notes`, `cover_photo_uid` (FK photos `ON DELETE SET NULL`), časy; `markers` =
  `uid PK` (prefix `mk`), `photo_uid` (FK photos `ON DELETE CASCADE`), `subject_uid` (FK
  subjects `ON DELETE SET NULL`), `type IN (face|label)`, normalizovaný bbox `x,y,w,h`
  DOUBLE PRECISION (0..1 display space, jako `faces.bbox`), `score`, `invalid`, `reviewed`,
  časy + indexy na `photo_uid`/`subject_uid`; `Store` = `NewStore(pool)` nad sdíleným pgx
  poolem: **subjekty** `CreateSubject`(generuje uid + **unikátní slug z name** — `Slugify`
  bez diakritiky/ASCII, kolize → číselný sufix `name-2`)/`GetSubjectByUID`/`GetSubjectBySlug`/
  `UpdateSubject`(přeslugování + refresh `faces.subject_name` cache)/`ListSubjects` (s počty
  nearchivovaných... resp. **non-invalid** markerů per subjekt, řazení dle jména)/
  `DeleteSubject` (FK odpojí markery, vyčistí faces cache); **markery** `CreateMarker`
  (validace typu/`0..1` bounds, volitelně rovnou subjekt → faces cache)/`GetMarkerByUID`/
  `ListMarkersByPhoto`/`AssignSubject`+`UnassignSubject` (v transakci aktualizují
  denormalizovaný **faces cache** `marker_uid`/`subject_uid`/`subject_name` přes
  `WHERE marker_uid = $1`)/`SetMarkerInvalid`/`SetMarkerReviewed`/`DeleteMarker` (vyčistí
  faces cache); sentinely `ErrSubjectNotFound`/`ErrMarkerNotFound`/`ErrSlugExhausted`/
  `ErrInvalidType`/`ErrInvalidBounds`; **faces cache se drží konzistentní** při každé změně
  markeru/subjektu (mazání, rename, assign/unassign)), `internal/facematch/`
  (propojení detekovaných obličejů s markery/subjekty + návrhy identit, vše za rozhraními
  `PhotoStore`/`FaceStore`/`PeopleStore` (unit-testovatelné s faky bez DB): `Service` =
  `New(Config{Photos,Faces,People,IoUThreshold,SuggestionLimit,SuggestionMaxDistance,MinFaceSize})`;
  **IoU geometrie** `IoU(a,b [4]float64)` (čistá funkce, Intersection-over-Union normalizovaných
  boxů `[x,y,w,h]`), `findBestMarker` vybere nejpřekrývající se **face** marker (ignoruje
  `invalid`), match při `IoU ≥ faces.iou_threshold` (default 0.1, mirror photo-sorteru);
  **`PhotoFaces(ctx,photoUID)`** (backing `GET /photos/{uid}/faces`) → pro každý uložený obličej
  spočítá nejlepší marker dle IoU, určí akci (`create_marker`/`assign_person`/`already_done`),
  **zacachuje match na řádek obličeje** přes `vectors.UpdateFaceMarker`, a pro nepojmenované
  obličeje přidá návrhy; markery bez odpovídajícího obličeje připojí (záporné `face_index`);
  **návrhy** (`aggregateSuggestions`, čistá funkce) z `vectors.FindSimilarFaceCandidates`
  (HNSW cosine) agregují kandidáty dle subjektu, vyloučí obličeje na stejné fotce, subjekty už
  přiřazené na fotce (jiné osoby) a obličeje pod `faces.min_face_size`, řadí dle průměrné
  vzdálenosti, `confidence = 1 − distance`, limit `faces.suggestion_limit`, primární práh
  `faces.suggestion_max_distance` s fallbackem na neomezenou vzdálenost když je návrhů málo;
  **přiřazovací state machine** `Apply(ctx,AssignRequest)` (backing
  `POST /photos/{uid}/faces/assign`, editor/admin): `create_marker` (vytvoří face marker + přiřadí
  subjekt + zalinkuje obličej), `assign_person` (přiřadí subjekt existujícímu markeru),
  `unassign_person` (odpojí subjekt), drží `faces` cache i `marker.reviewed` konzistentní
  (assign → reviewed, unassign → unreviewed), **auto-create subjektu dle jména** (find-or-create
  přes `Slugify`+`GetSubjectBySlug`); sentinely `ErrInvalidAction`/`ErrMissingBBox`/
  `ErrMissingMarker`/`ErrMissingSubject`, chybějící foto/marker/subjekt → 404 v HTTP vrstvě
  (`photoapi.FaceService` interface + handlery v `internal/photoapi/faces.go`); tunables v
  `faces.*` configu), `internal/embedjob/`
  (zapojení CLIP embeddingu do fronty + embeddingové dotazy, vše za rozhraními
  `PhotoStore`/`VectorStore`/`Previewer`/`Enqueuer`+`embedding.Client`: `Service` =
  `New(Config{Photos,Vectors,Client,Previewer,Enqueuer,PreviewSize,OfflineRetryDelay,
  DuplicateMaxDist})`; **handler `image_embed`** `Handle`(=`worker.HandlerFunc`, registrovaný
  v `serve`) → z payloadu `{"photo_uid"}` načte fotku, vyrenderuje (idempotentně) náhled `fit_720`,
  pošle sidecaru `ImageEmbedding`, uloží 768-dim `halfvec` přes `vectors.SaveEmbedding`+`model`/
  `pretrained`; **idempotentní** (fotka s embeddingem se přeskočí bez volání sidecaru), **box
  offline** (`embedding.IsUnavailable`) → `worker.RetryAfter(5 min)` (odložení bez spálení pokusu),
  jiná chyba normální retry; `BackfillEmbeddings(ctx)` zařadí `image_embed` pro každou fotku bez
  embeddingu (dedup no-op), vrací počet; `Duplicates(ctx,uid)` embeddingová detekce blízkých
  duplikátů do `duplicate.embedding_max_dist`, bez sebe sama (`<=0` vypne)), `internal/facejob/`
  (zapojení detekce obličejů do fronty, vše za rozhraními
  `PhotoStore`/`VectorStore`/`ImageSource`/`Enqueuer`+`embedding.Client`: `Service` =
  `New(Config{Photos,Vectors,Client,Source,Enqueuer,OfflineRetryDelay,MinDetScore})`; **handler
  `face_detect`** `Handle`(=`worker.HandlerFunc`, registrovaný v `serve`) → z payloadu
  `{"photo_uid"}` načte fotku, otevře **dekódovatelný originál v plném rozlišení** přes
  `StorageSource` (= `storage.AbsPath` + `imgconvert.EnsureDecodable`, HEIC/RAW/video se převedou,
  cleanup tempu na `Close`), pošle sidecaru `FaceEmbeddings` (512-dim + pixel bbox + det_score) a
  uloží přes `vectors.RecordFaceDetection`; originál (ne náhled) proto, že sidecar (InsightFace)
  sám rotuje dle EXIF a vrací bbox v display pixelech; **převod bboxu** `normalizeBBox` pixel
  `[x1,y1,x2,y2]` → normalizovaný `[x,y,w,h]` (0..1) dle rozměrů fotky a **EXIF orientace** (swap
  šířky/výšky pro orientace 5–8), mirror photo-sorter logiky; **filtr det_score**
  (`faces.min_det_score`, default 0.5, `<=0` vypne) zahodí slabé detekce, přeživší přeindexuje
  souvisle; **idempotentní** (fotka s `face_detections` řádkem se přeskočí; nula obličejů se přesto
  zaznamená), **box offline** → `worker.RetryAfter(5 min)`; `BackfillFaces(ctx)` zařadí
  `face_detect` pro každou nezpracovanou fotku (`ListPhotosMissingFaces`, dedup no-op), vrací
  počet), `internal/processapi/`
  (admin-only HTTP API pro hromadné zpracování: `NewAPI(Config{Backfiller,FaceBackfiller,
  Reclusterer,RequireAdmin})`+`RegisterRoutes` mountuje `/process`; `POST /process/embeddings` →
  `{enqueued}` spustí `embedjob.BackfillEmbeddings`, `POST /process/faces` → `{enqueued}` spustí
  `facejob.BackfillFaces`, `POST /process/clusters` → `{created}` spustí `cluster.Recluster`
  (re-clustering nepřiřazených obličejů; `Reclusterer` volitelný — nil → 503)), `internal/cluster/`
  (face auto-clustering: seskupuje **dosud nepřiřazené obličeje** (bez subjektu) do shluků téže
  osoby, aby šel celý shluk pojmenovat jedním tahem (klíčové UX zlepšení oproti per-face naming
  photo-sorteru); tabulka `face_clusters` (migrace `0010_face_clusters.sql`: `uid` PK prefix `fc`,
  `centroid halfvec(512)` cosine, `size`, `model`, časy) + cache sloupec `faces.cluster_uid` FK
  `ON DELETE SET NULL`; vše za rozhraními `FaceSearcher` (podmnožina `vectors.Store`) a `FaceAssigner`
  (podmnožina `facematch.Service`) → unit-testovatelné s faky; `Service` =
  `New(Config{Store,Faces,Assigner,Threshold,MinSize,SuggestionMaxDistance})`, defaulty
  `DefaultThreshold` 0.4 / `DefaultMinSize` 2 / `DefaultSuggestionMaxDistance` 0.5; **algoritmus**
  (čisté funkce `algo.go`/`suggest.go`): greedy **souvislé komponenty** (union-find) nad HNSW
  nejbližšími sousedy každého clusterovatelného obličeje do prahu cosine vzdálenosti — hrana = dva
  obličeje blíž než `threshold`, komponenta `≥ minSize` se stane shlukem, menší zůstanou
  nesclustrované; per-shluk L2-normalizovaný **centroid** (`centroid`/`normalize`/`cosineDistance`)
  pro výběr reprezentanta (`nearestToCentroid`) i návrh subjektu; **`Recluster(ctx)`** clusteruje
  jen obličeje **bez subjektu A bez shluku** (`subject_uid IS NULL AND cluster_uid IS NULL`) →
  inkrementální a re-spustitelné, nikdy nesáhne na přiřazené ani sclustrované, deterministické;
  **`ListClusters(ctx)`** (backing `GET /faces/clusters`) → per shluk velikost, reprezentativní
  obličej, příklady (`maxExamples` 4) a **návrh existujícího subjektu** (`bestSubjectSuggestion`
  agreguje `FindSimilarFaceCandidates` nad centroidem dle subjektu, `confidence = 1 − distance`,
  null když žádný pojmenovaný soused < `suggestionMaxDistance`); **`AssignCluster(ctx,req)`**
  (backing `POST /faces/clusters/{id}/assign`) přiřadí **všechny** obličeje shluku jednomu subjektu
  (dle `subject_uid`, jinak find-or-create dle `subject_name`) přes **sdílenou facematch state
  machine** (`create_marker`, subjekt se resolvuje jednou a pinuje pro zbytek), pak spotřebovaný
  shluk smaže (FK uvolní `cluster_uid`); **`RemoveFace(ctx,clusterUID,ref)`** (backing
  `POST /faces/clusters/{id}/remove-face`) odpojí zatoulaný obličej **před** pojmenováním, přepočítá
  centroid/velikost (`RefreshCluster`), osiřelý shluk smaže; `Store` nad sdíleným pgx poolem
  (`ListUnclusteredFaces`/`ListClusterFaces`/`CreateCluster`/`AddFacesToCluster`/`ListClusters`/
  `GetCluster`/`DeleteCluster`/`RemoveFaceFromCluster`/`RefreshCluster`); sentinely
  `ErrClusterNotFound`/`ErrEmptyCluster`/`ErrMissingSubject`/`ErrFaceNotInCluster`; tunables v
  `cluster.*` configu), `internal/clusterapi/`
  (editor/admin HTTP API nad clusteringem: `Service` rozhraní (splňuje ho `cluster.Service`),
  `NewAPI(Config{Service,RequireWrite})`+`RegisterRoutes` mountuje `/faces/clusters`:
  `GET /faces/clusters` (list shluků + návrhy), `POST /faces/clusters/{id}/assign` (přiřadí celý
  shluk), `POST /faces/clusters/{id}/remove-face` (odpojí obličej); 503 když backend nezapojen,
  400/404/409 dle sentinelů; mountuje se v `serve` (`buildClusterAPI` v `cmd/kukatko/clusters.go`,
  které sdílí `facematch.Service` z `buildFaceMatch`)), `internal/outliers/`
  (per-osoba outlier detekce obličejů: odhalí pravděpodobně **špatně přiřazené obličeje**
  seřazením dle vzdálenosti od centroidu embeddingů osoby, mirror photo-sorteru; vše za rozhraními
  `FaceStore` (podmnožina `vectors.Store`) a `PeopleStore` (podmnožina `people.Store`) →
  unit-testovatelné s faky bez DB; `Service` = `New(Config{Faces,People})`;
  **`Outliers(ctx,subjectUID)`** (backing `GET /subjects/{uid}/outliers`) ověří subjekt
  (`people.ErrSubjectNotFound`), načte `vectors.ListFacesBySubject`, spočítá centroid
  (`vectors.Centroid`), ohodnotí každý obličej `vectors.CosineDistance` od centroidu a vrátí je
  **sestupně** (nejpodezřelejší první, tie-break `photo_uid`/`face_index`); `Result` =
  `{subject_uid,count,meaningful,faces:[OutlierFace{photo_uid,face_index,bbox,det_score,distance,
  marker_uid?,width,height,orientation}]}`; **malé množiny** (< `MinMeaningful`=3 obličeje) →
  `meaningful:false` (žádný se nevyčlení), obličeje se přesto vrátí seřazené; žádná mutace — wrong
  obličej se odpojí přes existující assign API), `internal/outlierapi/`
  (editor/admin HTTP API nad outlier detekcí: `Service` rozhraní (splňuje ho `outliers.Service`),
  `NewAPI(Config{Service,RequireWrite})`+`RegisterRoutes` mountuje `GET /subjects/{uid}/outliers`
  za `RequireWrite`; 503 bez backendu, 404 chybějící subjekt; mountuje se v `serve`
  (`buildOutlierAPI` v `cmd/kukatko/outliers.go`)), `internal/web/`
  (SPA fallback handler `web.Handler()`/`SPAHandler` + `internal/web/static` embed
  `//go:embed all:dist/*`; Vite build se zapisuje do `internal/web/static/dist`, ten je
  gitignorovaný kromě committed `.gitkeep`, aby embed kompiloval i bez buildnutého
  frontendu). Detail: [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).
- **Frontend layout:** `web/` (Vite + React 19 + TS): `web/src/` s `components/`
  (`Layout` = navbar shell s user-menu/logout + role-gated nav — odkaz **Knihovna**
  míří na `/library`, **Hledat** na `/search`, **Nahrát** na `/upload` (jen editor/admin),
  `NavbarSearch` (kompaktní vyhledávací pole v navbaru → submit naviguje na `/search?q=…`),
  `LanguageSwitcher`;
  `components/upload/` = `DropZone` (drag-and-drop zóna + file input `multiple`
  `accept="image/*,video/*"` → mobilní galerie + tlačítko **Vyfotit** `capture="environment"`),
  `UploadItem` (řádek fronty: jméno+velikost, progress-bar, status badge, near-duplicate
  varování, remove/retry akce); `components/library/` = `PhotoTile`
  (čtvercová lazy-load dlaždice → `/photos/{uid}`, badge soukromé, placeholder bez
  layout-shiftu), `PhotoGrid` (virtualizovaný **`react-virtuoso` `VirtuosoGrid`**,
  window-scroll, `endReached` → další stránka, footer spinner/retry), `FilterBar`
  (datum od/do, poloha, soukromé, fotoaparát, archiv, řazení + počet + „zrušit filtry";
  generický nad `LibraryView`+supersetem, props `showSearch`/`showSort` skryjí dotaz/řazení
  na search stránce), `SimilarPhotos` (znovupoužitelný horizontálně scrollovatelný pruh
  podobných fotek nad `GET /photos/{uid}/similar` přes `fetchSimilar`, odkazy na detail,
  empty-friendly + loading/error, refetch při změně `uid`),
  `GridSkeleton` (placeholder mřížka při prvním načtení)),
  `pages/` (`HomePage` volá `GET /healthz`, `LoginPage`, `AccountPage` = změna vlastního hesla,
  `LibraryPage` = hlavní foto-knihovna: `FilterBar` nad virtualizovanou nekonečně-scrollující
  mřížkou, loading/empty/error stavy, celý pohled (filtry+řazení) v URL,
  `SearchPage` = sémantické/hybridní/fulltext hledání: prominentní debouncované (350 ms)
  vyhledávací pole + přepínač režimu (`q`+`mode` v URL), stejná virtualizovaná mřížka jako
  knihovna + sdílený `FilterBar` (bez dotazu/řazení), `degraded` → neblokující upozornění
  (sidecar offline), idle/loading/empty/error stavy,
  `UploadPage` = multiupload (drag-and-drop + galerie/fotoaparát na mobilu): `DropZone`
  nad frontou `UploadItem`, per-file progress/status, souhrn počtů, start/clear/retry-failed,
  po dokončení odkaz na nově nahrané fotky (`/library?sort=added`),
  `NotFoundPage`), `auth/` (`AuthContext`/`useAuth` + `AuthProvider` = boot `GET /auth/me`,
  vystavuje `user`/`role`/`login`/`logout`/`refresh`/`canWrite`/`isAdmin`; `ProtectedRoute` =
  `RequireAuth` + `RequireRole` route guardy), `hooks/` (`usePaginatedPhotos` = sdílený
  paginovaný infinite-scroll loader nad libovolným `PageFetcher`: akumuluje stránky,
  `loadMore`/`retry`, reset+refetch při změně dotazu/`key`/`enabled`, ruší in-flight requesty
  a ignoruje stale odpovědi, vystavuje i `mode`/`degraded`; `enabled:false` → `idle` stav bez
  requestu; `usePhotoLibrary` = tenká obálka nad ním nad `fetchPhotos`; `usePhotoSearch` =
  obálka nad `searchPhotos` s injektovaným `mode`, vypnutá při prázdném `q` (idle);
  `useUploadQueue` = fronta uploadu: `addFiles` (dedup jméno+velikost+mtime)/`removeItem`/
  `start`/`retry`/`retryFailed`/`clear`, konkurenční strop `MAX_CONCURRENT_UPLOADS` (3),
  per-file status+progress, souhrn počtů, `createdUids` pro odkaz do knihovny; auto-drainuje
  frontu efektem po `start`/retry, ruší běžící uploady při unmountu),
  `lib/` (`urlState.ts` = hook `useUrlState` +
  pure `readUrlState`/`writeUrlState`: stav pohledu ↔ URL query přes History API, „Zpět vždy
  funguje"; `libraryView.ts` = typ `LibraryView` + `LIBRARY_DEFAULTS` + `viewToParams`
  (sanitizuje sort/archived) + `hasActiveFilters` (`{ignoreQuery}` na search stránce) —
  mapování URL stavu na API params; `searchView.ts` = typ `SearchView` (= `LibraryView` + `mode`)
  + `SEARCH_DEFAULTS` (mode `hybrid`) + `toMode` sanitizér),
  `services/` (`health.ts`, `auth.ts` = login/logout/me/changePassword, typy
  `User`/`Role`/`AuthSession`, `ApiError` se statusem, `canWrite`/`roleAtLeast`,
  `MIN_PASSWORD_LENGTH`; `photos.ts` = `fetchPhotos(params,signal)` nad `GET /api/v1/photos`
  (filtry/řazení/stránkování → `PhotoListResponse{photos,total,limit,offset,next_offset}`),
  `searchPhotos(params,mode?,signal)` nad `GET /api/v1/search` (mód
  `fulltext`/`semantic`/`hybrid`, odpověď navíc `mode`+`degraded`),
  `fetchSimilar(uid,limit?,signal)` nad `GET /api/v1/photos/{uid}/similar` → `SimilarPhoto[]`
  (`Photo`+`distance`; empty-friendly), typy `SimilarPhoto`/`SimilarResponse`,
  `buildPhotoQuery`, `thumbUrl(uid,size,token?)`, `GRID_THUMB_SIZE`, typy `Photo`/`PhotoListParams`/
  `PhotoSort`/`ArchivedFilter`/`SearchMode`, `ApiError`; `upload.ts` = `uploadFile(file,{onProgress,signal})`
  nad **`XMLHttpRequest`** (jeden soubor/request kvůli upload-progress eventům, FormData se
  streamuje), `isAbortError`, typy `UploadFileResult`/`UploadResponse`/`UploadWarning`/
  `UploadOutcome`), `i18n/` (i18next init + `locales/{cs,en}/common.json`;
  typované klíče přes `types/i18next.d.ts` — nové stringy přidávej do **obou** locale souborů),
  `test/setup.ts`.
  Routing v `App.tsx`: `/login` veřejné, zbytek pod `RequireAuth` → `Layout` (`/`, `/library`,
  `/search`, `/account`; `/upload` navíc pod `RequireRole role="editor"` = write-only). Konfig:
  `vite.config.ts` (build → `../internal/web/static/dist`, vitest jsdom, dev proxy
  `/healthz`+`/api` → `:8080`), `eslint.config.js` (strict typed), `.prettierrc.json`,
  `tsconfig*.json`.
- **CLI:** `kukatko serve` (načte config, **spustí migrace**, **bootstrapne admina**, spustí
  hodinové čištění expirovaných session a **background worker** (`internal/worker`) na
  zpracování fronty jobů, pak poslouchá na `web.host:web.port`, default
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
  `auth.bootstrap_admin_username/password`. Middleware navíc `RequireAuthOrDownloadToken`
  (session cookie nebo `?t=download_token` přes `Service.AuthenticateDownloadToken` →
  `Store.GetSessionByDownloadToken`) pro média bez cookie.
- **Upload API (`/api/v1`):** `POST /upload` (editor/admin přes `RequireWrite`) — `multipart/form-data`
  s jedním+ soubory, **streamuje** se. Vrací `{"results":[{filename,status,outcome,photo_uid?,error?,
  warnings?}]}` (celkově 200, 409 sémantika duplicit per-file). Mountuje se druhým `server.WithAPI`
  v `serve` (`buildIngest` v `cmd/kukatko/ingest.go`). Limit `upload.max_file_size_mb` (0 = bez limitu).
- **Photos API (`/api/v1`, `internal/photoapi`):** `GET /photos` (přihlášený) — list s filtry/
  řazením/stránkováním (query params, neplatný → 400) → `{photos,total,limit,offset,next_offset}`;
  `GET /search?q=&mode=` (přihlášený) — **sémantické + hybridní hledání**, `mode` =
  `fulltext`|`semantic`|`hybrid` (default `hybrid`, neznámý → 400): **fulltext** = česky-aware
  fulltext nad `fts tsvector` (dictionary `simple` + `unaccent`, řazení `ts_rank`
  title>description>notes>file_name); **semantic** = `q` → CLIP embedding přes sidecar →
  cosine HNSW nad `embeddings`, řazení dle podobnosti; **hybrid** = fúze obou přes
  **Reciprocal Rank Fusion (k=60)**, dedup. Všechny módy ctí ostatní list filtry + stránkování,
  odpověď jako list + `mode` + `degraded`; `q` povinný (prázdný → 400); **box offline** →
  `semantic`/`hybrid` graceful fallback na fulltext s `degraded: true`;
  `GET /photos/{uid}` plný detail + `files`; `GET /photos/{uid}/faces` (přihlášený) — obličeje
  fotky s bboxem, přiřazením (marker/subjekt), akcí (`create_marker`/`assign_person`/`already_done`)
  a **návrhy** identit pro nepojmenované (face↔marker IoU matching, viz `internal/facematch`; 503
  když face backend není zapojen); `POST /photos/{uid}/faces/assign` (editor/admin) — přiřazovací
  akce `{action, face_index?, marker_uid?, subject_uid?, subject_name?, bbox?}`
  (`create_marker`/`assign_person`/`unassign_person`), auto-create subjektu dle jména, drží `faces`
  cache + `marker.reviewed` konzistentní (400 validace, 404 chybějící foto/marker/subjekt);
  `PATCH /photos/{uid}` (editor/admin) částečná úprava
  metadat (null maže nullable, validace souřadnic); `POST /photos/{uid}/archive`+`/unarchive`
  (editor/admin) soft-delete přes `archived_at` (archivované mimo výchozí list);
  `GET /photos/{uid}/thumb/{size}` a `/download` (session/`?t=` token) **streamují** média
  (`Cache-Control`/`ETag`/`304`). Mountuje se třetím `server.WithAPI` (`buildPhotoAPI` v
  `cmd/kukatko/photos.go`).
- **Jobs API (`/api/v1`, `internal/jobsapi`, admin-only přes `RequireAdmin`):**
  `GET /jobs/stats` → `{by_state,by_type,total}`; `GET /jobs` → `{jobs,limit,offset}`
  (recent/dead-letter výpis, query `state`/`limit`/`offset`, neplatný → 400);
  `POST /jobs/{id}/requeue` → refreshnutý job (dead/failed → queued; 404 missing, 409
  ne-requeueable). Frontend polluje (žádné SSE). Mountuje se šestým `server.WithAPI`
  (`buildJobs` v `cmd/kukatko/jobs.go`), který registruje handlery `image_embed`
  (`embedjob.Service`) i `face_detect` (`facejob.Service`) a zároveň postaví a `serve` spustí
  **background worker** (`internal/worker`) na celý život procesu (`startWorker`, zastaví
  se na shutdownu přes ctx).
- **Clusters API (`/api/v1`, `internal/clusterapi`, editor/admin přes `RequireWrite`):**
  `GET /faces/clusters` → `{clusters:[{uid,size,representative,examples,suggestion?}]}` (shluky
  nepřiřazených obličejů z auto-clusteringu, `suggestion` = nejbližší pojmenovaný subjekt);
  `POST /faces/clusters/{id}/assign` `{subject_uid?,subject_name?}` přiřadí **celý shluk** jednomu
  subjektu (find-or-create dle jména) → markery pro všechny obličeje, shluk se spotřebuje;
  `POST /faces/clusters/{id}/remove-face` `{photo_uid,face_index}` odpojí zatoulaný obličej před
  pojmenováním → refreshnutý shluk (nebo `null` když osiří); 503 bez backendu, 400/404/409 dle
  sentinelů. Mountuje se čtvrtým `server.WithAPI` (`buildClusterAPI` v `cmd/kukatko/clusters.go`).
- **Outliers API (`/api/v1`, `internal/outlierapi`, editor/admin přes `RequireWrite`):**
  `GET /subjects/{uid}/outliers` → `{subject_uid,count,meaningful,faces:[{photo_uid,face_index,
  bbox,det_score,distance,marker_uid?,width,height,orientation}]}` (obličeje osoby seřazené
  sestupně dle kosinové vzdálenosti od centroidu jejích embeddingů — nejpravděpodobněji špatně
  přiřazené první); 1–2 obličeje → `meaningful:false`; špatný obličej se odpojí přes existující
  `POST /photos/{uid}/faces/assign` (`unassign_person`), tahle vrstva nemutuje; 503 bez backendu,
  404 chybějící subjekt. Mountuje se pátým `server.WithAPI` (`buildOutlierAPI` v
  `cmd/kukatko/outliers.go`).
- **Process API (`/api/v1`, `internal/processapi`, admin-only přes `RequireAdmin`):**
  `POST /process/embeddings` → `{enqueued}` (backfill `image_embed` pro fotky bez embeddingu),
  `POST /process/faces` → `{enqueued}` (backfill `face_detect` pro fotky bez detekce obličejů),
  `POST /process/clusters` → `{created}` (re-clustering nepřiřazených obličejů přes
  `cluster.Recluster`). Mountuje se sedmým `server.WithAPI` (`buildJobs`).
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
  kroku + apt `ffmpeg`/`libimage-exiftool-perl` (video probe/poster), `KUKATKO_TEST_DATABASE_URL`
  na efemérní CI DB). `.github/workflows/release.yml`
  (tag `v*.*.*`) pouští **goreleaser** (`.goreleaser.yaml`): `CGO_ENABLED=0` pro arm64+amd64,
  verze/commit přes ldflags do `internal/version`, frontend build v before-hooku, **.deb**
  přes nfpm. `deb/`: `kukatko.service` (systemd, user `kukatko`, `EnvironmentFile`,
  `Restart=always`), `kukatko.env` (dpkg conffile `config|noreplace`),
  `postinstall.sh`/`preremove.sh`/`postremove.sh` (user + `/var/lib/kukatko/{originals,cache}`).
  Apt deps: `libimage-exiftool-perl`, `libheif-examples|libheif-bin`, `dcraw`, `ffmpeg`,
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
