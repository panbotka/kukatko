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
  (batch lookup dle uid, ignoruje neznámé — pro similar API)/
  `UpdateMetadata`/`Archive`/`Unarchive`/`Delete`/`List`+`Count` (filtry archived/private/
  uploader/has-GPS/date-range `taken_after`+`taken_before`/camera/lens/fulltext search,
  řazení taken_at/created_at/uid/title/file_size, stránkování limit/offset; `Count` sdílí
  `buildWhere` filtry pro `total`), plus `CreateFile`/`ListFiles`,
  `SetPhash`/`GetPhash`, `SetEdit`/`GetEdit`; dedup na SHA256 `file_hash` + externí ID
  `photoprism_uid`/`photoprism_file_hash`(SHA1)/`photosorter_uid`; tabulky v migraci
  `0003_photos.sql`: `photos`, `photo_files` (jeden primary/foto), `photo_phashes`,
  `photo_edits` (all-or-nothing crop, rotace 0/90/180/270); video sloupce v migraci
  `0004_video.sql` (`media_type` image/video/live CHECK+partial index, `duration_ms`,
  `video_codec`, `audio_codec`, `has_audio`, `fps`); FK `ON DELETE CASCADE`
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
  (read/curace HTTP API nad katalogem: `NewAPI(Config{Store,Storage,Thumbnailer,RequireAuth,
  RequireWrite,RequireDownload})` + `RegisterRoutes` mountuje `/photos`; `parseListParams`
  validuje query → `photos.ListParams` (`limit`≤500/`offset`, `sort`
  newest/oldest/taken_at/added/title/size + `order`, `archived` false/true/only, `private`,
  `has_gps`, `taken_after`/`taken_before`, `camera`, `lens`, `uploader`, `q`; neplatný → 400),
  list vrací `{photos,total,limit,offset,next_offset}` pro infinite scroll; `PATCH` je
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
  `DeleteFaces`/`FindSimilarFaces` pro 512-dim face embeddingy + cache sloupce
  marker_uid/subject_uid/subject_name/photo_width/photo_height/orientation a normalizovaný
  `bbox DOUBLE PRECISION[4]` `[x,y,w,h]`; podobnost přes `embedding <=> $vec` (cosine, nejbližší
  první) v **read-only transakci** se `SET LOCAL hnsw.ef_search = 100`; `limit` ořez `[1,500]`,
  nekladný `maxDistance` filtr vypne; helpery `ToHalfVec`/`FromHalfVec` (`[]float32` ↔
  `pgvector.HalfVector`); sentinely `ErrEmbeddingNotFound`/`ErrDimMismatch` (validace 768/512)/
  `ErrFaceIndexTaken` (UNIQUE `(photo_uid,face_index)`); `ListPhotosMissingEmbedding(limit)` =
  uid nearchivovaných fotek bez embeddingu (LEFT JOIN, nejnovější první, `limit<=0`=vše) pro
  backfill; FK `ON DELETE CASCADE` — mazání fotky
  smaže embeddingy i faces, oprava photo-sorter mezery se sirotky), `internal/embedjob/`
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
  duplikátů do `duplicate.embedding_max_dist`, bez sebe sama (`<=0` vypne)), `internal/processapi/`
  (admin-only HTTP API pro hromadné zpracování: `NewAPI(Config{Backfiller,RequireAdmin})`+
  `RegisterRoutes` mountuje `/process`; `POST /process/embeddings` → `{enqueued}` spustí
  `embedjob.BackfillEmbeddings`), `internal/web/`
  (SPA fallback handler `web.Handler()`/`SPAHandler` + `internal/web/static` embed
  `//go:embed all:dist/*`; Vite build se zapisuje do `internal/web/static/dist`, ten je
  gitignorovaný kromě committed `.gitkeep`, aby embed kompiloval i bez buildnutého
  frontendu). Detail: [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).
- **Frontend layout:** `web/` (Vite + React 19 + TS): `web/src/` s `components/`
  (`Layout` = navbar shell s user-menu/logout + role-gated nav — odkaz **Knihovna**
  míří na `/library`, **Nahrát** na `/upload` (jen editor/admin), `LanguageSwitcher`;
  `components/upload/` = `DropZone` (drag-and-drop zóna + file input `multiple`
  `accept="image/*,video/*"` → mobilní galerie + tlačítko **Vyfotit** `capture="environment"`),
  `UploadItem` (řádek fronty: jméno+velikost, progress-bar, status badge, near-duplicate
  varování, remove/retry akce); `components/library/` = `PhotoTile`
  (čtvercová lazy-load dlaždice → `/photos/{uid}`, badge soukromé, placeholder bez
  layout-shiftu), `PhotoGrid` (virtualizovaný **`react-virtuoso` `VirtuosoGrid`**,
  window-scroll, `endReached` → další stránka, footer spinner/retry), `FilterBar`
  (datum od/do, poloha, soukromé, fotoaparát, archiv, řazení + počet + „zrušit filtry"),
  `GridSkeleton` (placeholder mřížka při prvním načtení)),
  `pages/` (`HomePage` volá `GET /healthz`, `LoginPage`, `AccountPage` = změna vlastního hesla,
  `LibraryPage` = hlavní foto-knihovna: `FilterBar` nad virtualizovanou nekonečně-scrollující
  mřížkou, loading/empty/error stavy, celý pohled (filtry+řazení) v URL,
  `UploadPage` = multiupload (drag-and-drop + galerie/fotoaparát na mobilu): `DropZone`
  nad frontou `UploadItem`, per-file progress/status, souhrn počtů, start/clear/retry-failed,
  po dokončení odkaz na nově nahrané fotky (`/library?sort=added`),
  `NotFoundPage`), `auth/` (`AuthContext`/`useAuth` + `AuthProvider` = boot `GET /auth/me`,
  vystavuje `user`/`role`/`login`/`logout`/`refresh`/`canWrite`/`isAdmin`; `ProtectedRoute` =
  `RequireAuth` + `RequireRole` route guardy), `hooks/` (`usePhotoLibrary` = paginovaný
  infinite-scroll loader nad `fetchPhotos`: akumuluje stránky, `loadMore`/`retry`,
  reset+refetch při změně filtrů, ruší in-flight requesty a ignoruje stale odpovědi;
  `useUploadQueue` = fronta uploadu: `addFiles` (dedup jméno+velikost+mtime)/`removeItem`/
  `start`/`retry`/`retryFailed`/`clear`, konkurenční strop `MAX_CONCURRENT_UPLOADS` (3),
  per-file status+progress, souhrn počtů, `createdUids` pro odkaz do knihovny; auto-drainuje
  frontu efektem po `start`/retry, ruší běžící uploady při unmountu),
  `lib/` (`urlState.ts` = hook `useUrlState` +
  pure `readUrlState`/`writeUrlState`: stav pohledu ↔ URL query přes History API, „Zpět vždy
  funguje"; `libraryView.ts` = typ `LibraryView` + `LIBRARY_DEFAULTS` + `viewToParams`
  (sanitizuje sort/archived) + `hasActiveFilters` — mapování URL stavu na API params),
  `services/` (`health.ts`, `auth.ts` = login/logout/me/changePassword, typy
  `User`/`Role`/`AuthSession`, `ApiError` se statusem, `canWrite`/`roleAtLeast`,
  `MIN_PASSWORD_LENGTH`; `photos.ts` = `fetchPhotos(params,signal)` nad `GET /api/v1/photos`
  (filtry/řazení/stránkování → `PhotoListResponse{photos,total,limit,offset,next_offset}`),
  `buildPhotoQuery`, `thumbUrl(uid,size,token?)`, `GRID_THUMB_SIZE`, typy `Photo`/`PhotoListParams`/
  `PhotoSort`/`ArchivedFilter`, `ApiError`; `upload.ts` = `uploadFile(file,{onProgress,signal})`
  nad **`XMLHttpRequest`** (jeden soubor/request kvůli upload-progress eventům, FormData se
  streamuje), `isAbortError`, typy `UploadFileResult`/`UploadResponse`/`UploadWarning`/
  `UploadOutcome`), `i18n/` (i18next init + `locales/{cs,en}/common.json`;
  typované klíče přes `types/i18next.d.ts` — nové stringy přidávej do **obou** locale souborů),
  `test/setup.ts`.
  Routing v `App.tsx`: `/login` veřejné, zbytek pod `RequireAuth` → `Layout` (`/`, `/library`,
  `/account`; `/upload` navíc pod `RequireRole role="editor"` = write-only). Konfig:
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
  `GET /photos/{uid}` plný detail + `files`; `PATCH /photos/{uid}` (editor/admin) částečná úprava
  metadat (null maže nullable, validace souřadnic); `POST /photos/{uid}/archive`+`/unarchive`
  (editor/admin) soft-delete přes `archived_at` (archivované mimo výchozí list);
  `GET /photos/{uid}/thumb/{size}` a `/download` (session/`?t=` token) **streamují** média
  (`Cache-Control`/`ETag`/`304`). Mountuje se třetím `server.WithAPI` (`buildPhotoAPI` v
  `cmd/kukatko/photos.go`).
- **Jobs API (`/api/v1`, `internal/jobsapi`, admin-only přes `RequireAdmin`):**
  `GET /jobs/stats` → `{by_state,by_type,total}`; `GET /jobs` → `{jobs,limit,offset}`
  (recent/dead-letter výpis, query `state`/`limit`/`offset`, neplatný → 400);
  `POST /jobs/{id}/requeue` → refreshnutý job (dead/failed → queued; 404 missing, 409
  ne-requeueable). Frontend polluje (žádné SSE). Mountuje se čtvrtým `server.WithAPI`
  (`buildJobs` v `cmd/kukatko/jobs.go`), který zároveň postaví a `serve` spustí
  **background worker** (`internal/worker`) na celý život procesu (`startWorker`, zastaví
  se na shutdownu přes ctx).
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
