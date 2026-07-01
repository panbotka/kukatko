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
  Virtualizace dlouhých mřížek/seznamů přes **`react-virtuoso`**. Mapový pohled přes
  **`leaflet`** + **`leaflet.markercluster`** (dlaždice přes backend proxy, klíč zůstává server-side).
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
  (jádro foto-katalogu: typované modely `Photo`/`PhotoFile`/`Phash`/`Edit`/`MetadataUpdate`
  (`Photo` nese i per-user anotační pole `Rating int`/`Flag string` — JSON `rating`/`flag`,
  analogická `is_favorite`; neukládají se v `photos`, plní je HTTP handlery z `organize.Store`),
  `MediaType` image/video/live, `FileRole` original/sidecar/edited, UID generátor prefix `ph`,
  `Store` nad pgx s
  `Create`/`GetByUID`/`GetByFileHash`/`GetByPhotoprismUID`/`GetByPhotosorterUID`/`SetPhotoprismRef`
  (backfill `photoprism_uid`+`photoprism_file_hash` na fotku deduplikovanou dle SHA256 — PhotoPrism
  import to volá, aby další inkrement short-circuitnul na uid místo re-downloadu)/`ListByUIDs`
  (batch lookup dle uid, ignoruje neznámé — pro similar API)/`FilterUIDs`
  (z dané množiny uid vrátí ty, co projdou strukturálními List filtry — ignoruje řazení,
  stránkování i `FullText`; companion k sémantickému hledání: caller drží kandidáty z
  embeddings indexu a profiltruje je list filtry, pořadí dle podobnosti si řadí sám)/
  `UpdateMetadata`/`Archive`/`Unarchive`/`Delete`/`List`+`Count` (filtry archived/private/
  uploader/has-GPS/date-range `taken_after`+`taken_before`/camera/lens/substring search +
  **album/label scope** `AlbumUID`/`LabelUID` korelovaným `EXISTS` nad `album_photos`/`photo_labels`
  — podklad sdíleného scoped výpisu fotek alba/štítku přes `GET /photos?album=`/`?label=`,
  plus **place scope** `Country`/`City` (exact match jedním korelovaným `EXISTS` nad `photo_places`)
  — podklad `GET /photos?country=&city=`,
  plus **per-user favorite scope** `FavoriteOf` korelovaným `EXISTS` nad `user_favorites`
  — podklad `GET /photos?favorite=true` a `GET /favorites`,
  plus **per-user rating filtry** `RatedBy` (uid aktuálního uživatele, scopuje anotaci/filtry/řazení)
  + `MinRating` (rating ≥ n korelovaným `EXISTS` nad `user_ratings`, ≤ 0 = bez filtru, fotka bez řádku
  = rating 0) + `Flag` (`pick`/`reject` korelovaným `EXISTS`) — všechny aktivní jen když je `RatedBy`
  nastaveno, fotka bez řádku = rating 0 / flag `none`,
  řazení taken_at/created_at/uid/title/file_size **+ `rating`** (řazení dle ratingu `RatedBy`
  uživatele přes korelovaný poddotaz nad `user_ratings`, `NULLS LAST` — nehodnocené poslední; aktivní
  jen s `RatedBy`), stránkování limit/offset; `Count` sdílí
  `buildWhere` filtry pro `total`)/`Search` (česky-aware fulltext nad generovaným `fts
  tsvector` sloupcem: `ListParams.FullText` přes `websearch_to_tsquery('simple',
  immutable_unaccent(q))`, řazení dle `ts_rank` (title>description>notes>file_name),
  diakritika necitlivá, ctí všechny List filtry + stránkování; prázdný dotaz →
  `ErrEmptySearch`; `Count` s `FullText` vrací total díky sdílenému `buildWhere`),
  `AggregatePlaces(country)` (place hierarchie `[]CountryPlaces{Country,Count,Cities:[]CityCount}` —
  jedním `GROUP BY country, city` JOINem `photos`×`photo_places` přes nearchivované fotky s place
  daty, hierarchii složí v Go, řazení count desc/jméno; prázdné `country`='' = všechny země, jinak
  drill-down do měst jedné země; fotky s prázdným `country` (no-GPS marker) vyloučené — podklad
  `placesapi`),
  `TimelineBuckets(params)` (měsíční date-histogram `Timeline{Buckets:[]TimelineBucket{Year,Month,
  Count,Cumulative},Total}` — jedním `GROUP BY` dle `date_part(year/month, taken_at)` nad
  nearchivovanými fotkami, řazení nejnovější první (`year DESC, month DESC`, jako výchozí mřížka),
  `Cumulative` (běžný součet dřívějších=novějších bucketů) spočítán v Go a rovná se scroll-indexu
  prvního snímku bucketu; sdílí `buildWhere` s `List`/`Count`, takže buckety přesně odpovídají
  seznamu; fotky bez `taken_at` do bucketů nespadají (řadí se na konec), ale `Total` (přes `Count`)
  je zahrnuje — podklad `photoapi` timeline scrubberu),
  plus `CreateFile`/`ListFiles`,
  `ListArchivedUIDs(before,limit,offset)` (uid archivovaných fotek oldest-archived-first,
  `before` nil = vše / non-nil = jen `archived_at <= before` retenční cutoff — podklad koše/purge),
  `CountPhotos()` (total fotek vč. archivovaných) + `ListFilePaths()` (všechny `photo_files.file_path`)
  — podklad post-restore integritního reportu (`backup.PhotoCatalog`),
  `SetPhash`/`GetPhash`, `SetEdit`/`GetEdit`; dedup na SHA256 `file_hash` + externí ID
  `photoprism_uid`/`photoprism_file_hash`(SHA1)/`photosorter_uid`; tabulky v migraci
  `0003_photos.sql`: `photos`, `photo_files` (jeden primary/foto), `photo_phashes`,
  `photo_edits` (all-or-nothing crop, rotace 0/90/180/270); video sloupce v migraci
  `0004_video.sql` (`media_type` image/video/live CHECK+partial index, `duration_ms`,
  `video_codec`, `audio_codec`, `has_audio`, `fps`); generovaný `fts tsvector` sloupec +
  GIN index a IMMUTABLE `immutable_unaccent` wrapper v migraci `0007_fts.sql` (fulltext,
  `setweight` A/B/C/D, `to_tsvector('simple', immutable_unaccent(...))`, `file_name`
  normalizován regexem na tokeny; generated column drží `fts` aktuální i po editaci
  metadat bez triggeru); **výkonové partial composite indexy** v migraci `0015_perf_indexes.sql`
  (`idx_photos_live_taken_at (taken_at DESC NULLS LAST, uid DESC) WHERE archived_at IS NULL` +
  companion `idx_photos_live_created_at` pro `sort=added`) přesně odpovídají nejčastějšímu řazení
  mřížky → stránka časové osy je index scan **bez Sortu** (EXPLAIN integrační test
  `store_perf_integration_test.go`, viz `docs/PERF.md`); FK `ON DELETE CASCADE`
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
  dekód jednou na fotku, paralelní enkód velikostí (errgroup, default `GOMAXPROCS`,
  vázáno přes `thumb.concurrency`),
  **EXIF orientace** (1–8) automaticky; pure-Go JPEG/PNG/WebP + `golang.org/x/image`
  (`draw.CatmullRom` resize); **volitelný vips engine** (`WithVips(bin)`, config `thumb.engine:
  vips`, `vips.go`): pure-Go dekód velkých JPEGů je na Pi pomalý/paměťově náročný (~1 s / ~90 MB
  na `fit_720` z 12 MP, ~4 s / ~1,18 GB na `GenerateAll` — viz `docs/PERF.md`), `vips` přepne
  JPEG/PNG/WebP náhledy na **shell-out na `vipsthumbnail`** (`tryVips` → `vipsArgs`: fit `WxH>`
  bez upscalu, crop `--smartcrop centre`, `[Q=…,strip]`, EXIF autorotace), **stále bez CGO**;
  pure-Go zůstává default, vips **per-foto fallbackuje** na pure-Go pro ostatní formáty
  (HEIC/RAW/video) i při jakémkoli selhání → nikdy nemění výstup, jen rychlost; `VipsAvailable(bin)`
  pro startup log; `Remove(hash)` smaže všechny cachované velikosti pro hash
  (idempotentní, chybějící skip — úklid náhledů při purge fotky); sentinely
  `ErrUnknownSize`/`ErrInvalidHash`/`ErrNotCached`;
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
  `IsVideoPath`/`IsVideoExt`/`FFmpegAvailable`/`FFprobeAvailable`; **on-the-fly transcode pro
  playback** (`transcode.go`): `IsWebFriendlyCodec(codec)` (h264/avc/vp8/vp9/av1/theora hrají
  nativně v prohlížeči, prázdný=neznámý=ne), `TranscodeArgs(src)` (ffmpeg → **fragmentovaný**
  H.264/AAC MP4 na `pipe:1` přes `frag_keyframe+empty_moov`, audio volitelně `0:a?` — testovatelné
  bez ffmpeg) a `Transcode(ctx,src) (*TranscodeStream,error)` (spustí ffmpeg, `Read`/`Close` =
  `io.ReadCloser`, Close zabije proces + reapne; `ErrFFmpegMissing` když ffmpeg chybí); sentinely
  `ErrFFmpegMissing`/`ErrFFprobeMissing`/`ErrNoMetadataTool`/`ErrPosterFailed`), `internal/exif/`
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
  Embedder,Faces,Favorites,Ratings,RequireAuth,RequireWrite,RequireDownload})` + `RegisterRoutes` mountuje `/photos`
  **, `GET /photos/timeline`, `GET /search` a `GET /favorites`**; `parseListParams`
  validuje query → `photos.ListParams` (`limit`≤500/`offset`, `sort`
  newest/oldest/taken_at/added/title/size**/rating** + `order`, `archived` false/true/only, `private`,
  `has_gps`, `taken_after`/`taken_before`, `camera`, `lens`, `uploader`, `q`, **`album`/`label`
  scope** → `AlbumUID`/`LabelUID`, **`country`/`city` place scope** → `Country`/`City`,
  **per-user `min_rating` (int) + `flag` (`pick`/`reject`)**
  → `MinRating`/`Flag`; neplatný → 400) + `favoriteRequested` parsuje `favorite=true`
  → handler nastaví per-user `FavoriteOf` na aktuálního uživatele; handlery list/search/favorites
  nastaví `RatedBy` na aktuálního uživatele, takže `min_rating`/`flag`/`sort=rating` jsou scopnuté na něj;
  list vrací `{photos,total,limit,offset,next_offset}` (každá fotka anotovaná `is_favorite`
  + per-user `rating`/`flag` přes sdílený `annotate`: `FavoriteStore.FavoritedAmong` +
  `RatingStore.RatingsAmong`, fotka bez řádku = rating 0 / flag `none`) pro infinite scroll;
  **per-user oblíbené** (`favorites.go`): `PUT`/`DELETE /photos/{uid}/favorite` (každý přihlášený,
  idempotentní toggle → 204, 404 chybějící fotka, 503 bez `Favorites` backendu) + `GET /favorites`
  (oblíbené aktuálního uživatele ve tvaru list endpointu, ekvivalent `?favorite=true`);
  `FavoriteStore` interface (splňuje ho `organize.Store`) je nil-safe (nezapojeno → `is_favorite`
  false, favorite endpointy 503);
  **per-user hodnocení** (`ratings.go`): `PUT /photos/{uid}/rating` `{rating?:0..5, flag?:none|pick|reject}`
  (každý přihlášený, aspoň jedna hodnota, validace předem → 400 neplatná, 404 chybějící fotka, 503 bez
  `Ratings` backendu; nastaví rating a/nebo flag přes `SetRating`/`SetFlag`) + `DELETE /photos/{uid}/rating`
  (idempotentní clear přes `ClearRating` → 204); `RatingStore` interface (splňuje ho `organize.Store`,
  `SetRating`/`SetFlag`/`ClearRating`/`RatingsAmong`) je nil-safe (nezapojeno → rating 0 / flag `none`,
  rating endpointy 503);
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
  guard `RequireAuthOrDownloadToken` = session cookie nebo `?t=download_token`; **video streaming**
  (`video.go`): `GET /photos/{uid}/video` streamuje video **s HTTP Range** přes `http.ServeContent`
  (206 partial, `Accept-Ranges`, seek, If-Range/If-None-Match, paměťově omezené ze `*os.File` přes
  `storage.AbsPath`) pro inline HTML5 přehrávání; live fotka servíruje svůj **motion klip** sidecar
  (`pickMotionClip` dle video MIME/přípony), still image → 404; **on-the-fly transcode** gated
  `VideoConfig`/`video.transcode` (default off) + `video.IsWebFriendlyCodec` + `video.FFmpegAvailable`
  → `video.Transcode` (H.264/MP4 progressive, žádný range, `no-store`), fallback na originál když
  ffmpeg selže nebo je codec neznámý; **nedestruktivní
  edit** přes `Organizer` (album/label chipy detailu) a `EditService`/`edit.go`+`media_edit.go`
  (`GET`/`PUT /photos/{uid}/edit`, download honoruje edit přes `internal/photoedit`)), `internal/photoedit/`
  (**CGO-free aplikace nedestruktivního editu** na dekódovaný obrázek pro download/preview: `Apply(img,
  photos.Edit) image.Image` aplikuje **crop** (normalizovaný `[x,y,w,h]` 0..1), **rotaci** 0/90/180/270
  a **jas/kontrast** (lineární škála kolem 0.5, mapuje se 1:1 na frontend CSS `brightness(1+b)`/
  `contrast(1+c)`), pure-Go přes `golang.org/x/image`; `IsIdentity(edit)` přeskočí no-op; `orient.go`
  = EXIF orientace; identita = passthrough originálu, jinak render do JPEGu), `internal/trash/`
  (trvalé mazání (purge) soft-deletovaných fotek, vše za rozhraními `PhotoStore`/`FileStorage`/
  `ThumbStore`/`RemoteRemover` (unit-testovatelné s faky): `Service` = `New(Config{Photos,Storage,
  Thumbnailer,Remote?,RetentionDays,BatchSize,Logger})` (panika na nil Photos/Storage/Thumbnailer);
  **purgeOne** smaže artefakty fotky (originál přes `Storage.Delete`, cachované náhledy přes
  `Thumbnailer.Remove`, volitelně S3 objekt přes `RemoteRemover`) **a pak** DB řádek
  (`photos.Delete` kaskáduje embeddingy/faces/markery/album_photos/photo_labels/phashe/edity/oblíbené
  přes `ON DELETE CASCADE`) — artefakty napřed, takže přerušený purge nechá re-purgovatelný řádek
  místo dangling souborů; idempotentní (chybějící soubor/`os.ErrNotExist`/`thumb.ErrInvalidHash`
  se ignoruje); `PurgePhoto(uid)` (404 `photos.ErrPhotoNotFound`, `ErrNotArchived` na živou fotku),
  `EmptyTrash()` (purge všech archivovaných) a `PurgeExpired()` (jen `archived_at` starší než
  `RetentionDays`, ≤ 0 = no-op) iterují `photos.ListArchivedUIDs` v oldest-first dávkách
  (`BatchSize`, default 200) → `Result{Purged,Failed}`; **per-fotka selhání** se zaloguje, počítá a
  přeskočí (offset roste, fotka zůstane v koši pro retry), jen zrušený ctx aborte; `RunPurge(ctx,
  interval)` = plánovaný úklid (hned + každý interval, vypnutý při retenci ≤ 0) pro `serve`
  goroutinu), `internal/jobs/`
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
  `face_detect`/`thumbnail`/`places`/`pp_import`/`ps_migrate`/`backup`; `Enqueuer` = `NewEnqueuer(store)`
  implementuje `ingest.JobEnqueuer` (`EnqueueImageEmbed`/`EnqueueFaceDetect`/`EnqueueThumbnail`/
  `EnqueuePlaces`, `ErrDuplicate`=no-op)),
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
  `internal/wake/`
  (volitelný **Wake-on-LAN auto-wake** boxu, **default OFF** a plně inertní když vypnuto: balík
  pošle magic packet na lokální LAN když čekají `image_embed`/`face_detect` joby a sidecar je
  offline, ať se fronta dožene bez ručního zapnutí; vše za rozhraními `QueueDepth`
  (`PendingEmbeddingJobs(ctx)` — splňuje ho adapter nad `jobs.Store.CountPending`),
  `HealthChecker` (`Healthy(ctx)` — splňuje ho `embedding.Client`) a `Sender`
  (`Send(ctx,mac)` — **fakeovatelné v testech**, žádný reálný síťový provoz); `Packet(mac)`
  staví magic packet přes `mdlayher/wol` (102 B: 6× 0xFF + MAC 16×); `Service` =
  `New(Config{Enabled,MAC,BroadcastAddr,Interface,MinQueue,Cooldown,GracePeriod,Queue,Health,
  Sender,Logger,Clock})` (disabled → inertní; enabled vyžaduje validní MAC + Queue/Health, jinak
  default síťový sender: UDP broadcast na `BroadcastAddr`, nebo raw Ethernet rámec na `Interface`
  přes `wol.NewRawClient`, vyžaduje CAP_NET_RAW); **`Tick(ctx)`** = jeden cyklus: pošle packet jen
  když enabled **&&** `pending ≥ MinQueue` **&&** cooldown uplynul **&&** sidecar offline, pak po
  `GracePeriod` překontroluje zdraví a zaloguje jestli box naběhl (jinak backoff do cooldownu);
  **cooldown se nastaví i při chybě sendu** (nespamuje broken sender); `Run(ctx,interval)` =
  plánovaná smyčka (hned + každý interval) ve vlastní goroutině — **nikdy neblokuje zpracování
  jobů**; chyby se jen logují, nikdy nevrací; defaulty `MinQueue` 1 / `Cooldown` 5 min /
  `GracePeriod` 30 s; tunables v `embedding.wake.*` configu),
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
  první) v **read-only transakci** se `SET LOCAL hnsw.ef_search = 100` (konstanta `efSearch=100`,
  guard test drží `0 < efSearch < efSearchMax=400` — design ji nikdy nezvedá k 400, viz
  `docs/PERF.md`); `limit` ořez `[1,500]`,
  nekladný `maxDistance` filtr vypne; helpery `ToHalfVec`/`FromHalfVec` (`[]float32` ↔
  `pgvector.HalfVector`) a **sdílená vektorová matematika** `Centroid`(L2-normalizovaný
  element-wise průměr)/`Normalize`/`CosineDistance` v `math.go` (jediná implementace, kterou
  znovupoužívá i `internal/cluster` i `internal/outliers`); sentinely
  `ErrEmbeddingNotFound`/`ErrDimMismatch` (validace 768/512)/
  `ErrFaceIndexTaken` (UNIQUE `(photo_uid,face_index)`); `ListPhotosMissingEmbedding(limit)` =
  uid nearchivovaných fotek bez embeddingu (LEFT JOIN, nejnovější první, `limit<=0`=vše) pro
  backfill; `FindDuplicatePairs(neighbours,maxDist)` = near-duplicate páry dle embedding cosine
  vzdálenosti (`duplicate.go`, `CROSS JOIN LATERAL` + HNSW `LIMIT` neighbours per fotka, žádný
  O(n²) sken; `maxDist<=0`→žádné páry; read-only tx s `hnsw.ef_search`) — podklad
  `internal/duplicates`; **face-detection tracking** v tabulce `face_detections` (migrace
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
  `DeleteSubject` (FK odpojí markery, vyčistí faces cache)/`ListPhotoUIDsBySubject` (distinct
  uid nearchivovaných fotek s non-invalid markerem subjektu, newest-first — podklad galerie
  subjektu v `peopleapi`)/`SearchSubjects(q,limit)` (accent/case-insensitive ILIKE nad
  `immutable_unaccent(name)`, cap limit — podklad `globalsearchapi`); **markery** `CreateMarker`
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
  Reclusterer,PlacesBackfiller,RequireAdmin})`+`RegisterRoutes` mountuje `/process`;
  `POST /process/embeddings` →
  `{enqueued}` spustí `embedjob.BackfillEmbeddings`, `POST /process/faces` → `{enqueued}` spustí
  `facejob.BackfillFaces`, `POST /process/clusters` → `{created}` spustí `cluster.Recluster`
  (re-clustering nepřiřazených obličejů; `Reclusterer` volitelný — nil → 503),
  `POST /process/places` → `{enqueued}` spustí `placesjob.BackfillPlaces` (backfill reverse-geokódu
  geotagovaných fotek; `PlacesBackfiller` volitelný — nil → 503, tj. bez mapy.com klíče)),
  `internal/cluster/`
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
  (`buildOutlierAPI` v `cmd/kukatko/outliers.go`)), `internal/peopleapi/`
  (read/curace HTTP API nad subjekty (osoby/zvířata/jiné) — podklad People UI: rozhraní
  `SubjectStore` (podmnožina `people.Store`: `ListSubjects`/`GetSubjectByUID`/`CreateSubject`/
  `UpdateSubject`/`DeleteSubject`/`ListPhotoUIDsBySubject`) a `PhotoStore` (`photos.Store.ListByUIDs`)
  → unit-testovatelné s faky bez DB; `NewAPI(Config{Subjects,Photos,RequireAuth,RequireWrite})`+
  `RegisterRoutes` mountuje **ploché** cesty (ne mounted subrouter, aby koexistovaly s
  `outlierapi` `GET /subjects/{uid}/outliers` bez chi Mount konfliktu): `GET /subjects`
  (RequireAuth, `{subjects:[SubjectCount]}` s počty markerů), `POST /subjects` (RequireWrite,
  create → 201, validace jména/typu), `GET /subjects/{uid}` (RequireAuth), `PATCH /subjects/{uid}`
  (RequireWrite, editace name/type/favorite/private/notes/cover_photo_uid), `DELETE /subjects/{uid}`
  (RequireWrite → 204), `GET /subjects/{uid}/photos` (RequireAuth, paginovaná galerie fotek subjektu
  `{photos,total,limit,offset,next_offset}` — `ListPhotoUIDsBySubject` (distinct non-invalid
  markery, nearchivované, newest-first) → page → `ListByUIDs` → reorder dle uid pořadí); body
  decode `DisallowUnknownFields` + 1 MiB limit + prázdné jméno → 400; sentinely mapované
  `ErrSubjectNotFound`→404/`ErrInvalidType`→400; mountuje se osmým `server.WithAPI`
  (`buildPeopleAPI` v `cmd/kukatko/people.go`)), `internal/organize/`
  (DB vrstva pro **organizaci** — alba, štítky, **per-user oblíbené** (nahrazují globální
  `photos.favorite` z photo-sorteru) a **per-user hodnocení** (hvězdičky 0–5 + pick/reject flag);
  tabulky `albums`/`album_photos`/`labels`/`photo_labels`/
  `user_favorites` v migraci `0011_albums_labels_favorites.sql` a `user_ratings` v migraci
  `0016_user_ratings.sql`: **`albums`** = `uid PK`
  (prefix `al`), `slug UNIQUE` (Slugify z `title`, číselný sufix na kolizi), `title`/`description`,
  `type IN (album|folder|moment|state|month)`, `cover_photo_uid` (FK photos `ON DELETE SET NULL`),
  `private`, `order_by` (free-text řazení galerie, default `added`), `created_by` (FK users
  `ON DELETE SET NULL`), časy; **`album_photos`** = členství `(album_uid, photo_uid) PK`, oba FK
  `ON DELETE CASCADE`, `sort_order`/`added_at`; **`labels`** = `uid PK` (prefix `lb`), `slug UNIQUE`
  (z `name`), `name`, `priority`, časy; **`photo_labels`** = připojení `(photo_uid, label_uid) PK`,
  oba FK `ON DELETE CASCADE`, `source IN (manual|ai|import)`, `uncertainty` (int %), `created_at`;
  **`user_favorites`** = `(user_uid, photo_uid) PK`, oba FK `ON DELETE CASCADE`, `added_at`;
  **`user_ratings`** = `(user_uid, photo_uid) PK`, oba FK `ON DELETE CASCADE`, `rating SMALLINT 0..5`
  (CHECK), `flag TEXT IN (none|pick|reject)` (CHECK), `updated_at` — řádek existuje jen pro
  nedefaultní hodnotu (store maže řádek, který spadne na rating 0 + flag `none`), takže fotka bez
  řádku = rating 0 / flag `none`;
  `Store` = `NewStore(pool)` nad sdíleným pgx poolem: **alba** `CreateAlbum`/`GetAlbumByUID`/
  `GetAlbumBySlug`/`UpdateAlbum` (re-slug z title)/`ListAlbums` (s počty fotek, řazení dle title)/
  `SearchAlbums(q,limit)` (accent/case-insensitive ILIKE nad `immutable_unaccent(title/description)`,
  s počty, cap limit — podklad `globalsearchapi`)/
  `DeleteAlbum`/`AddPhoto` (idempotentní upsert pozice)/`RemovePhoto` (idempotentní)/`ReorderPhotos`
  (atomický přepis `sort_order` dle pořadí v tx)/`SetCover` (set/clear cover)/`ListPhotoUIDs`
  (řazení `sort_order`); **štítky** `CreateLabel`/`GetLabelByUID`/`GetLabelBySlug`/`UpdateLabel`
  (re-slug)/`ListLabels` (s počty, řazení priority DESC)/`SearchLabels(q,limit)` (accent/case-insensitive
  ILIKE nad `immutable_unaccent(name)`, s počty, cap limit — podklad `globalsearchapi`)/`DeleteLabel`/
  `AttachLabel` (idempotentní upsert source/uncertainty)/`DetachLabel` (idempotentní)/`ListPhotoUIDsByLabel`; **oblíbené**
  `AddFavorite`/`RemoveFavorite` (obojí idempotentní)/`IsFavorite`/`ListFavorites` (per-user,
  newest-first)/`FavoritedAmong` (z množiny photo uid vrátí per-user podmnožinu oblíbených jako
  množinu — anotace celé stránky `is_favorite` jedním dotazem); **hodnocení** (`ratings.go`)
  `SetRating(user,photo,rating)` (validace 0–5 → `ErrInvalidRating`) / `SetFlag(user,photo,flag)`
  (validace none/pick/reject → `ErrInvalidFlag`) — idempotentní upsert jedné kolony v transakci,
  druhá kolona se zachová; když řádek spadne na rating 0 + flag `none`, smaže se (tabulka zůstane
  řídká); `ClearRating(user,photo)` smaže rating i flag jedním idempotentním DELETE (mirror
  `RemoveFavorite`, no-op na nehodnocené/chybějící fotce — podklad `DELETE /photos/{uid}/rating`);
  `GetRating(user,photo)` → `PhotoRating{Rating,Flag}` (chybějící řádek = 0/`none`, nil err);
  `RatingsAmong(user,photoUIDs)` → mapa `photo_uid → PhotoRating` jen pro hodnocené fotky (anotace
  celé stránky jedním dotazem, mirror `FavoritedAmong`, chybějící caller defaultuje na 0/`none`);
  typy `AlbumType`/`LabelSource`/`RatingFlag` (none/pick/reject)
  zrcadlí SQL CHECKy, slug helper s per-druh
  fallbackem (`album`/`label`); sentinely `ErrAlbumNotFound`/`ErrLabelNotFound`/`ErrPhotoNotFound`/
  `ErrUserNotFound`/`ErrSlugExhausted`/`ErrInvalidType`/`ErrInvalidSource`/`ErrInvalidRating`/
  `ErrInvalidFlag` — FK porušení při zápisu
  do join tabulek (`user_favorites`/`user_ratings`) se mapuje na not-found sentinel podle porušeného
  sloupce přes sdílený `translateUserPhotoFK` (`photo_uid` → photo, jinak user;
  album/label přes `translateMembershipFK`/`translateAttachFK`)), `internal/organizeapi/`
  (read/curace HTTP API nad alby a štítky — podklad Albums/Labels UI: rozhraní `AlbumStore`/
  `LabelStore` (podmnožiny `organize.Store`) → unit-testovatelné s faky bez DB;
  `NewAPI(Config{Albums,Labels,RequireAuth,RequireWrite})`+`RegisterRoutes` mountuje dva
  subroutery: **alba** `GET /albums` (RequireAuth, `{albums:[AlbumCount]}` s počty + cover),
  `POST /albums` (RequireWrite, 201, `title` povinný, validace typu přes `ErrInvalidType`),
  `GET /albums/{uid}` (RequireAuth), `PATCH /albums/{uid}` (RequireWrite, edituje
  title/description/cover_photo_uid/private/order_by; **strukturální `type` se zachová** —
  handler načte existující album a `type` z těla nepřebírá, takže folder/moment/… nelze přepsat),
  `DELETE /albums/{uid}` (RequireWrite → 204), členství `POST /albums/{uid}/photos`
  `{photo_uids:[…]}` (přidá za stávající fotky — base pozice = `len(ListPhotoUIDs)`),
  `DELETE /albums/{uid}/photos` `{photo_uids:[…]}` (odebere, idempotentní),
  `PATCH /albums/{uid}/order` `{photo_uids:[…]}` (přeřadí přes `ReorderPhotos`) — všechny tři
  membership endpointy vrací aktuální pořadí `{photo_uids:[…]}`, nejdřív ověří existenci alba
  (`requireAlbum` → 404); **štítky** `GET /labels` (RequireAuth, `{labels:[LabelCount]}`),
  `POST /labels` (RequireWrite, 201, `name` povinný), `GET /labels/{uid}` (RequireAuth),
  `PATCH /labels/{uid}` (RequireWrite, name/priority), `DELETE /labels/{uid}` (RequireWrite → 204),
  připojení `POST /labels/{uid}/photos` `{photo_uid,source?,uncertainty?}` → 204 (validace source
  přes `ErrInvalidSource`), `DELETE /labels/{uid}/photos` `{photo_uid}` → 204 (ověří existenci
  štítku → 404, pak idempotentní detach); body decode `DisallowUnknownFields` + 1 MiB limit;
  sentinely mapované `ErrAlbumNotFound`/`ErrLabelNotFound`/`ErrPhotoNotFound`→404,
  `ErrInvalidType`/`ErrInvalidSource`→400; **prohlížení fotek alba/štítku nemá vlastní endpoint** —
  jede přes sdílené `GET /photos` scopnuté `?album={uid}`/`?label={uid}` (viz `photos.ListParams`
  `AlbumUID`/`LabelUID` + `photoapi` `parseListParams`); mountuje se dalším `server.WithAPI`
  (`buildOrganizeAPI` v `cmd/kukatko/organize.go`, sdílí jednu `organize.Store` pro alba i štítky)),
  `internal/savedsearch/`
  (DB vrstva pro **per-user uložená hledání** ("smart albums") — pojmenovaná, vlastníkova soukromá
  definice filtru/hledání, kterou si uživatel znovu otevře; zrcadlí per-user vlastnictví
  `user_favorites`; tabulka `saved_searches` v migraci `0017_saved_searches.sql`: `uid PK` (prefix `ss`),
  `owner_uid` FK users `ON DELETE CASCADE`, `name TEXT NOT NULL`, `params JSONB NOT NULL` (opaktní
  uložený stav pohledu/hledání: filtry, řazení, dotaz, mód), `created_at`/`updated_at`, index na
  `owner_uid`; `Store` = `NewStore(pool)`: `Create(ctx,ownerUID,name,params)`/`List(ctx,ownerUID)`
  (newest-first dle `created_at`)/`Get(ctx,uid)`/`Update(ctx,uid,name,params)` (přepíše name+params,
  stampne `updated_at`)/`Delete(ctx,uid)`; `params` jako `json.RawMessage` (prázdné → `{}`, aby NOT NULL
  sloupec dostal validní JSON), `Get`/`Update`/`Delete` na chybějící řádek → sentinel `ErrNotFound`;
  vlastnictví **neřeší store** — scopuje ho HTTP vrstva nad ním)), `internal/savedsearchapi/`
  (read/curace HTTP API nad uloženými hledáními: rozhraní `Store` (podmnožina `savedsearch.Store`) →
  unit-testovatelné s faky; `NewAPI(Config{Store,RequireAuth})`+`RegisterRoutes` mountuje
  `/saved-searches` **vše za `RequireAuth`** a **scopnuté na přihlášeného uživatele** z auth kontextu
  (`auth.UserFromContext`): `GET /saved-searches` (`{saved_searches:[{uid,name,params,created_at,
  updated_at}]}` aktuálního uživatele, owner_uid se ve view záměrně neukazuje), `POST /saved-searches`
  `{name,params}` → 201 (prázdné jméno → 400, `params` volitelné → `{}`), `GET /saved-searches/{uid}`
  → 200, `PATCH /saved-searches/{uid}` `{name?,params?}` → 200 (vynechané pole beze změny, prázdné
  jméno → 400), `DELETE /saved-searches/{uid}` → 204; **vlastnická izolace** — sdílený helper
  `ownedSearch` načte řádek a porovná `owner_uid` s aktérem, cizí (i neexistující) → **404** (nikdy
  neprozradí cizí hledání); tělo `DisallowUnknownFields` + 1 MiB limit, sentinel `ErrNotFound`→404;
  mountuje se `server.WithAPI` (`buildSavedSearchAPI` v `cmd/kukatko/savedsearch.go`)), `internal/globalsearchapi/`
  (grouped **global search** HTTP API napříč entitami — podklad navbar quick-results i cross-entity sekce
  search stránky: malá rozhraní `Organizer` (`SearchAlbums`/`SearchLabels`, splňuje `organize.Store`),
  `PeopleSearcher` (`SearchSubjects`, splňuje `people.Store`) a `PhotoSearcher` (`Search`, splňuje
  `photos.Store` — reuse existujícího fulltextu přes `ListParams.FullText`) → unit-testovatelné s faky;
  `NewAPI(Config{Organizer,People,Photos,Limit,RequireAuth})`+`RegisterRoutes` mountuje
  `GET /search/global?q=` za `RequireAuth`: každou skupinu odbaví zvlášť (`SearchAlbums`/`SearchLabels`/
  `SearchSubjects` capnuté na `Limit`, default `defaultGroupLimit` 8; fotky přes fulltext s `Limit`),
  vrací grouped envelope `{query, albums:[{uid,title,cover,photo_count}], labels:[{uid,name,photo_count}],
  people:[{uid,name,cover}], photos:[…usual photo shape…]}` (každá skupina vždy non-nil pole); prázdný/
  whitespace `q` → 400, chyba store → 500; mountuje se `server.WithAPI` (`buildGlobalSearchAPI` v
  `cmd/kukatko/globalsearch.go`, sdílí organize/people/photos store)), `internal/placesapi/`
  (read-only HTTP API nad reverse-geokódovanou place hierarchií — podklad Places browse: rozhraní
  `Store` (podmnožina `photos.Store`: `AggregatePlaces`) → unit-testovatelné s fakem; `NewAPI(Config{
  Store,RequireAuth})`+`RegisterRoutes` mountuje `GET /places` za `RequireAuth`: hierarchie s počty
  `{places:[{country,count,cities:[{city,count}]}]}` agregovaná přes nearchivované fotky s place daty
  (count země zahrnuje i fotky bez města, cities vždy pole; řazení count desc/jméno), volitelné
  `?country=` drillne jen do měst jedné země; fotky bez place dat vyloučené (počítá `photos.Store.
  AggregatePlaces` jedním `GROUP BY country, city` JOINem na `photo_places`). **Procházení fotek
  lokality nemá vlastní endpoint** — jede přes sdílené `GET /photos` scopnuté `?country=`/`?city=`
  (`photos.ListParams` `Country`/`City` + `photoapi` `parseListParams`); mountuje se `server.WithAPI`
  (`buildPlacesAPI` v `cmd/kukatko/places.go`, agregace přes photos store nad `photo_places` cache)),
  `internal/audit/`
  (durable audit trail, tabulka `audit_log` v migraci `0012_audit_log.sql` rozšířená v
  `0014_audit_request.sql` o `ip`/`user_agent` + composite index `(target_type, target_uid)`:
  `id BIGSERIAL`, `actor_uid` FK users `ON DELETE SET NULL`, `action`, `target_type`, `target_uid`,
  `details JSONB`, `ip`, `user_agent`, `created_at` (sloupcová jména `actor/target/details` =
  spec termíny `user/entity/metadata`); **klíčový vzor** `Write(ctx, exec, Entry)` zapisuje přes
  rozhraní `Execer` (splňuje ho pool **i** `pgx.Tx`), takže audit řádek jede v **téže transakci**
  jako mutace — commitne/rollbackne s ní (ARCHITECTURE §5.1/§11/§12 „audit log durable", oprava
  photo-sorter after-commit mezery); `Entry{ActorUID,Action,TargetType,TargetUID,Details,IP,
  UserAgent}` (prázdné UID/IP/UA → SQL NULL, nil details → `{}`); **konvence pro handlery**
  `Meta` + `FromRequest(r, actorUID)` (actor z auth kontextu, IP z `X-Forwarded-For`/`X-Real-IP`/
  `RemoteAddr`, UA z hlavičky) → `(Meta).Entry(action, targetType, targetUID, details)` staví
  ostatní entry; action konstanty `ActionPhotosBulk`/`ActionPhoto{Update,Archive,Unarchive}`/
  `ActionAlbum{Create,Update,Delete}`/`ActionLabel{Create,Update,Delete}`/`ActionFaceAssign`/
  `ActionUser{Create,Update,Disable,Password}`; `Store` = `NewStore(pool)` se `Record(ctx,Entry)`
  (vlastní spojení) a **filtrovaným čtením** `List(ctx,Filter)`/`Count(ctx,Filter)` (`Filter{ActorUID,
  TargetType,TargetUID,Action,Since,Until,Limit,Offset}`, newest-first, limit cap 500/default 100)
  pro admin výpis. **Zapojené in-tx mutace**: bulk (`internal/bulk`) + foto PATCH/archive/unarchive
  přes audited varianty `photos.Store.{UpdateMetadata,Archive,Unarchive}Audited` (mutace + audit
  v jedné tx přes sdílený `rowQuerier`/`mutateAudited`); další domény (alba/štítky/lidé/uživatelé)
  následují stejnou konvenci), `internal/auditapi/`
  (admin-only HTTP API nad audit trailem: `NewAPI(Config{Store,RequireAdmin})`+`RegisterRoutes`
  mountuje `GET /audit` za `RequireAdmin`; `parseFilter` z query `user`/`entity_type`/`entity_uid`/
  `action`/`since`/`until` (RFC3339)/`limit`/`offset` → `audit.Filter` (neplatný čas/číslo → 400),
  vrací `{entries,total,limit,offset,next_offset}` newest-first; jen čtení — zápisy jdou přes
  mutační transakce jinde; mountuje se vždy posledním `server.WithAPI` (`buildAuditAPI` v
  `cmd/kukatko/audit.go`)), `internal/bulk/`
  (hromadná editace metadat: `Service` = `NewService(pool, maxBatch)` s `Apply(ctx, actorUID,
  photoUIDs, ops Operations) (Result, error)` — **celá dávka v jediné transakci** s audit
  záznamem; `Operations` = volitelná pole `AddAlbums`/`RemoveAlbums`/`AddLabels`/`RemoveLabels`,
  `Title`/`Description *string` (nil=beze změny, ""=clear), `Location *Location`+`ClearLocation`,
  `Private`/`Archive`/`Favorite *bool`, **`Rating *int` (0–5) + `Flag *string` (none/pick/reject)**;
  `Apply` validuje dávku (ErrNoPhotos/ErrNoOperations/
  ErrBatchTooLarge), ověří existenci alb/štítků v add operacích (ErrAlbumNotFound/ErrLabelNotFound),
  pak per-foto: duplicitní uid → `skipped`, neexistující fotka → `error` **bez abortu ostatních**,
  jinak aplikuje a `updated`; vlastní idempotentní SQL (vlastní tx kvůli atomicitě, nepoužívá
  organize/photos store metody, které mají vlastní spojení); favorite **i hodnocení** jsou
  **per-user** (`actorUID`) — rating/flag upsert + prune all-defaults řádku zrcadlí `organize` store;
  `Result{Results:[{photo_uid,status,error?}],Counts{total,updated,skipped,errored}}`; skutečná DB
  chyba rollbackne celou dávku; `Summary()` (audit details) + `IsEmpty()`), `internal/bulkapi/`
  (HTTP nad `bulk.Service`: rozhraní `Service` (Apply) — fakeovatelné; `NewAPI(Config{Service,
  RequireWrite})`+`RegisterRoutes` mountuje `POST /photos/bulk` za `RequireWrite`; tělo
  `{photo_uids,operations}` přes `operationsInput` se **set/clear páry jako samostatné klíče**
  (jednoznačné, konflikt `set_*`+`clear_*` / `archive`+`unarchive` → 400), `set_caption`→title,
  **`set_rating` (0–5) / `set_flag` (none/pick/reject)** s validací → 400,
  validace souřadnic, `DisallowUnknownFields` (neznámá operace → 400) + 4 MiB limit; chyby mapované
  `ErrNoPhotos`/`ErrNoOperations`/`ErrAlbum/LabelNotFound`→400, `ErrBatchTooLarge`→413, jinak 500;
  per-foto chyby vrací 200 s detailem v těle; mountuje se dalším `server.WithAPI`
  (`buildBulkAPI` v `cmd/kukatko/bulk.go`)),
  `internal/mapy/`
  (server-side HTTP klient k mapy.com REST API, **klíč nikdy neopustí server** — posílá se jen
  v hlavičce `X-Mapy-Api-Key`, nikdy v URL/chybě, vše za rozhraním `Client` (fakeovatelné):
  `New(Config{BaseURL,APIKey,Lang,Timeout,HTTPClient})` → `*HTTPClient`; `Tile(ctx,TileParams{
  Mapset,Z,X,Y,Retina}) (*TileResult,error)` (validuje mapset allowlist, staví URL
  `/v1/maptiles/{mapset}/256[@2x]/{z}/{x}/{y}`, **streamuje** body přes `cancelReadCloser` který
  na Close zruší request ctx — nikdy nedrží dlaždici v RAM), `ReverseGeocode(ctx,lat,lng)
  (*GeocodeResult,error)` (`/v1/rgeocode?lon=&lat=&lang=cs` → zjednodušený první `item` na
  `{Name,Location,RegionalStructure}`); allowlist `basic|outdoor|aerial|winter`
  (`IsValidMapset`), retina jen `basic`/`outdoor` (`RetinaSupported`); sentinely
  `ErrUnauthorized` (401/403) / `ErrNotFound` (404 i prázdné items) / `ErrRateLimited` (429) /
  `ErrUpstream` (jiný status / nečitelná odpověď) / `ErrUnavailable` (transport / 502/503/504) /
  `ErrInvalidMapset` / `ErrInvalidURL`; `statusError` **nepřidává tělo** odpovědi do chyby, aby
  klíč neprosákl ani když ho mapy.com echoují), `internal/mapsapi/`
  (HTTP API pro mapy — tile proxy, reverse geocode a GeoJSON feed; rozhraní `TileFetcher`/
  `Geocoder` (splňuje je `mapy.Client`, nil → 503) a `PhotoLister` (`photos.Store.List`) →
  unit-testovatelné s faky; `NewAPI(Config{Tiles,Geocoder,Photos,RequireAuth,TileCacheMaxAge,
  GeocodeCacheTTL,GeocodeRatePerSec,GeocodeRateBurst,MaxGeoPhotos})`+`RegisterRoutes` mountuje
  `/map` za `RequireAuth`: `GET /map/tiles/{mapset}/{z}/{x}/{y}` (validuje mapset→400/retina ze
  sufixu `@2x` na `{y}` nebo `?retina=true`, **streamuje** přes `io.Copy` s `Cache-Control:
  public, max-age, immutable`; chyby přes `writeTileError` → 404/429/503/502), `GET /map/rgeocode
  ?lat=&lng=` (parsuje+range-checkuje souřadnice→400, **TTL+capacity cache** `geocodeCache` klíč =
  souřadnice na 5 desetin, uncached lookup přes **token-bucket** `rateLimiter`→429 šetří kredity,
  odpověď zjednodušená + `Cache-Control: private`), `GET /map/photos` (GeoJSON
  **FeatureCollection**, `parseGeoParams` vynutí `HasGPS=true` + ctí `taken_after`/`taken_before`/
  `album`/`label`/`archived`/`private`, `Limit=MaxGeoPhotos`, řazení taken_at desc; každá feature
  `Point` se souřadnicí RFC 7946 `[lng,lat]` a properties `uid`/`title`/`taken_at`/`media_type`/
  relativní `thumb` cesta `tile_224`, fotky bez obou souřadnic se přeskočí); defaulty cache 24h /
  rate 5/s burst 10 / max 50000 features; mountuje se `server.WithAPI` (`buildMapsAPI` v
  `cmd/kukatko/maps.go`, klient se staví jen když je `maps.mapy_api_key` nastaven)),
  `internal/places/`
  (DB vrstva pro **cache reverse-geocoded místa** fotky — country/region/city/place_name resolvnuté
  z GPS přes mapy.com a uložené, aby šla knihovna procházet/filtrovat dle lokality bez opakovaného
  volání rate-limitovaného geokodéru; **schema choice: vedlejší tabulka `photo_places`** (ne sloupce
  na široké `photos`) keyovaná `photo_uid` FK `ON DELETE CASCADE` — místo je řídké (jen geotagované
  fotky mají řádek) a je to odvozená regenerovatelná cache plněná asynchronně jobem, zrcadlí
  `face_detections`/`user_ratings`; migrace `0018_photo_places.sql`: `photo_uid PK`, `country`/
  `region`/`city`/`place_name TEXT NOT NULL DEFAULT ''`, `lat`/`lng DOUBLE PRECISION` (souřadnice,
  ze kterých byl geokód spočítán — detekce změny pozice → re-geokód; NULL u fotky bez GPS, jejíž
  řádek jen značí "zpracováno"), `geocoded_at TIMESTAMPTZ`, indexy na `country` a `city` (grouping/
  filtering dle lokality); `Store` = `NewStore(pool)`: `GetPlace(photoUID)` (`ErrPlaceNotFound`)/
  `SavePlace(Place)` (upsert na `photo_uid`, stampne `geocoded_at`)/`ListPhotosMissingPlaces(limit)`
  (uid nearchivovaných **geotagovaných** fotek bez `photo_places` řádku, newest-first, LEFT JOIN —
  podklad backfillu)), `internal/placesjob/`
  (zapojení reverse geokódování do fronty, vše za rozhraními `PhotoStore`/`PlaceStore`/`Geocoder`
  (podmnožina `mapy.Client`, fakeovatelná)/`Enqueuer`/`RateLimiter` → unit-testovatelné s faky bez
  sítě/DB; `Service` = `New(Config{Photos,Places,Geocoder,Enqueuer,Limiter,OfflineRetryDelay,
  RateLimitDelay})` (panika na nil Photos/Places/Geocoder/Enqueuer, `Limiter` nil → always-allow);
  **handler `places`** `Handle`(=`worker.HandlerFunc`, registrovaný v `serve` když je mapy klíč
  nastaven) → z payloadu `{"photo_uid"}` načte fotku; **idempotentní** (fotka s místem cachovaným pro
  **aktuální** souřadnice se přeskočí; změna souřadnic → re-geokód), fotka **bez GPS** → uloží prázdný
  "processed" marker (nikdy se neretryuje); jinak `mapy.ReverseGeocode(lat,lng)` → `parsePlace`
  parsuje `regional_structure` (typy `regional.country`/`region`/`municipality`, prefix `regional.`
  volitelný) na country/region/city + place_name = nejspecifičtější label, uloží přes
  `places.SavePlace` se zdrojovými souřadnicemi; **mapy.com nedostupné/rate-limited**
  (`mapy.ErrUnavailable`/`ErrRateLimited`) → `worker.RetryAfter(5 min)` (odložení bez spálení pokusu,
  zrcadlí embed job), **`mapy.ErrNotFound`** → processed marker se souřadnicemi (neretryuje se forever),
  jiná chyba normální retry; **respekt k mapy.com kreditům**: `RateLimiter` (token-bucket `NewTokenBucket(
  ratePerSec,burst)`, zrcadlí geocode proxy limiter; `maps.geocode_rate_per_sec`/`geocode_burst`) — když
  je prázdný, `worker.RetryAfter(1 min)` (zpracovat pomalu je OK); `BackfillPlaces(ctx)` zařadí `places`
  pro každou geotagovanou fotku bez místa (dedup no-op), vrací počet), `internal/importer/`
  (evidence běhů importu/migrace + high-watermarky pro **inkrementální, idempotentní** import,
  tabulka `import_runs` v migraci `0013_import_runs.sql`: `id BIGSERIAL`, `source TEXT`
  CHECK `photoprism|photosorter`, `started_at`/`finished_at TIMESTAMPTZ`, `status TEXT`
  CHECK `running|done|failed`, `high_watermark TIMESTAMPTZ` (největší zpracovaný zdrojový
  timestamp, např. max PhotoPrism `UpdatedAt`), `counts JSONB` `{imported,updated,skipped,failed}`,
  `last_error TEXT`; partial index `(source, finished_at DESC) WHERE status='done' AND
  high_watermark IS NOT NULL` pro resume dotaz; typy `Source` (`SourcePhotoPrism`/
  `SourcePhotoSorter` + `Valid()`)/`Status` (`StatusRunning`/`StatusDone`/`StatusFailed`)/`Counts`/
  `Run`; `Store` = `NewStore(pool)`: `Start(ctx,source)` otevře `running` řádek (`ErrInvalidSource`),
  `UpdateCounts(ctx,id,counts)` přepíše tally, `Complete(ctx,id,watermark,counts)` uzavře jako
  `done` se stampnutým `finished_at`+watermarkem, `Fail(ctx,id,lastErr,counts)` jako `failed`
  **bez** watermarku (oba matchují jen běžící běh → `ErrRunNotFound` na dvojí uzavření),
  `Get(ctx,id)`, `LatestWatermark(ctx,source)` → `(time.Time, found bool, err)` watermark
  **posledního úspěšného** běhu zdroje pro navázání dalšího inkrementu — ignoruje běžící/failed
  běhy i done bez watermarku, každý zdroj má vlastní kurzor, `LatestRun(ctx,source)` →
  `(Run, found bool, err)` **nejnovější běh zdroje bez ohledu na stav** (running/done/failed —
  na rozdíl od `LatestWatermark` nefiltruje status; podklad system-status dashboardu),
  `List(ctx,limit,offset)` stránka běhů
  **přes všechny zdroje** newest-started-first (limit clamp `[1,200]`, default 50, non-nil prázdná
  stránka) — podklad admin historie importů; sentinely
  `ErrRunNotFound`/`ErrInvalidSource`), `internal/photoprism/`
  (read-only HTTP klient k běžící instanci PhotoPrismu — podklad inkrementálního importu, vše za
  rozhraním `Client` (fakeovatelné): `New(Config{BaseURL,Token,Timeout,MaxRetries,RetryBaseDelay,
  RetryMaxDelay,HTTPClient})` → `*HTTPClient`, `ErrInvalidURL` na nevalidní base URL; **autentizace**
  dlouhožijícím app password/access tokenem v hlavičce `Authorization: Bearer` na **každém**
  requestu (ne per-request login); `ListPhotos(ctx,PhotoListParams{Count,Offset,UpdatedSince,Order,
  AlbumUID,Query})`
  → `GET /api/v1/photos?count=…&offset=…&merged=true&order=updated[&q=updated:"<RFC3339>"]`
  pro **inkrementální** pull (UpdatedSince→filtr `updated:`, count ořez na `MaxCount` 1000, caller
  pageuje přes offset); **scope pro mapování členství**: `AlbumUID`→`s=<albumUID>` (fotky alba),
  `Query`→`q=` natvrdo (přebije watermark, pro `label:"<slug>"`); parsuje
  UID/TakenAt/Lat/Lng/Altitude/Title/Description/Type/Width/Height/
  OriginalName/Camera/Lens/EXIF + `Files[]` (UID, **Hash=SHA1**, Primary, Mime, `Video`/`Codec`,
  `Markers[]`),
  `Photo.PrimaryFile()` vrátí primární soubor, `File.IsVideo()` (Video flag/`video/*` mime),
  `Photo.VideoFile()` (motion soubor video/live fotky) a `Photo.StillFile()` (still fotky);
  `ListAlbums`/`ListLabels`/`ListSubjects(ctx,ListParams
  {Count,Offset})` → `GET /api/v1/{albums,labels,subjects}`, markery z `Files[].Markers[]`;
  `DownloadOriginal(ctx,fileHash)` → `GET /api/v1/dl/{hash}?t=<download_token>` **streamuje** originál
  (`Download{Body,ContentType,ContentLength}`, tělo vlastní caller; nikdy celý v RAM přes
  `cancelReadCloser`), **download token** z create-session `POST /api/v1/session`
  (`config.downloadToken`) thread-safe cachovaný, **rotuje** → přebírá se z hlavičky
  `X-Download-Token`, na 401/403 jednou obnoví session a zopakuje; **robustnost** 429 →
  exponenciální backoff ctící `Retry-After`, JSON endpointy vyžadují `Content-Type:
  application/json`; typové chyby `ErrInvalidURL`/`ErrUnauthorized`/`ErrNotFound`/`ErrRateLimited`/
  `ErrUpstream`/`ErrUnavailable`/`ErrBadResponse` nikdy nenesou token ani tělo odpovědi; konfig
  `import.photoprism.{base_url,token,page_size}`; klient staví importér (`ppimport`)),
  `internal/ppimport/`
  (read-only, **inkrementální a idempotentní** import z PhotoPrismu — vše za rozhraními
  `PhotoPrismClient`/`RunStore`/`PhotoStore`/`Storage`/`Thumbnailer`/`AlbumStore`/`LabelStore`/
  `PeopleStore`/`Enqueuer`/`VideoProber` → unit-testovatelné s faky; `Service` = `New(Config{Client,Runs,Photos,
  Storage,Thumbnailer,Albums,Labels,People,Enqueuer,Prober,PageSize,TempDir,MaxFileSize,Logger})`
  (`Prober` volitelný — nil → `defaultProber` nad `video.Probe`);
  **`Import(ctx) (Result,error)`** otevře `import_runs` běh, navrhne na poslední úspěšný watermark a:
  (1) pageuje `ListPhotos(UpdatedSince=watermark)` — per fotka dedup dle `photoprism_uid` (už
  importovaná → `UpdateMetadata` jen při změně, jinak skip), jinak **vybere média** (`selectMedia`,
  `video.go`): PP `Type` video/animated → **stáhne samotný video soubor** (`Photo.VideoFile()`,
  media_type `video`, video soubor bez streamu graceful → image), live → **still jako primární
  originál + motion klip jako sidecar** (`Photo.StillFile()`+`VideoFile()`, media_type `live`),
  jinak image; **stáhne** vybraný originál do
  tempu + **SHA256**, dedup dle `file_hash` (shodný obsah → backfill ID přes
  `photos.SetPhotoprismRef`, žádná nová fotka), uloží originál, **probne video metadata**
  (`Prober.Probe` → `duration_ms`/`video_codec`/`audio_codec`/`has_audio`/`fps`; u video z originálu,
  u live z motion klipu; best-effort, selhání → nulová pole), `photos.Create` s **PP metadaty**
  (title/desc/taken_at/GPS/camera/EXIF) + media_type + video metadata + `photoprism_uid`/`photoprism_file_hash` + **EXIF orientace
  ze souboru** (PP ji nevystavuje — `exif.Extract` doplní geometrii/orientaci/MIME, PP přebije
  kurátorská pole), **u live** stáhne+uloží motion klip jako `RoleSidecar` photo_file (best-effort),
  náhledy (u videa **poster frame** přes thumbnailer/ffmpeg) a **enqueue `image_embed`** (na posteru)
  **+`face_detect`**; counts **checkpoint po každé
  stránce** přes `UpdateCounts`; (2) **lidé** z `Files[].Markers[]` nově importovaných fotek
  (pojmenovaný validní face marker → find-or-create subjekt dle `Slugify` + přiřazený marker; jen na
  prvním importu, ať re-run neduplikuje); (3) **alba & štítky** find-or-create dle názvu (mapa z
  `ListAlbums`/`ListLabels`), členství přes scopnutý `ListPhotos` (`AlbumUID`/`label:"<slug>"`) →
  idempotentní `AddPhoto`/`AttachLabel`; pak běh `Complete` s watermarkem; **per-fotka chyba** se
  zaznamená do `counts.failed` a **nepřeruší běh** (jen infrastrukturní chyba běh `Fail`ne), 429
  backoff řeší klient, **watermark se nikdy neposune za nejstarší selhání** (`runState`); bezpečné
  re-runovat. **`Handle(ctx,job)`** = `worker.HandlerFunc` pro `pp_import` (ignoruje payload, volá
  `Import`), `JobPayload()` nese pevný sentinel `photo_uid` → dedup fronty pustí jen jeden import),
  `internal/photosorter/`
  (read-only klient k PostgreSQL DB **photo-sorteru** — datový zdroj přímé migrace (ARCHITECTURE.md
  §10), vše za `*Reader`: `New(ctx, Config{DSN,Schema,MaxConns})` otevře **vlastní** pgx pool
  (oddělený od Kukátko) s pgvector typy registrovanými na každém spojení, volitelný `Schema` scopne
  každý dotaz přes `search_path` (integrační test čte fake schéma vedle Kukátko tabulek); `Close()`
  uvolní pool; `ErrInvalidDSN`. Čte **jen** tabulky migrace — `ListPhotos(PhotoListParams{UpdatedSince,
  Limit,Offset})` (řazení `updated_at, uid`, `updated_at > $1` pro resume), `ListSubjects`/`ListAlbums`/
  `ListLabels(ListParams)`, `Embedding`/`Faces`/`FacesProcessed`/`Phash`/`Edit`/`Markers`/
  `AlbumMemberships`/`LabelMemberships(photoUID)` — embeddingy scanují do `[]float32` (pgvector),
  bbox do `[4]float64`; **fotoknihu ani share-linky nikdy nečte**), `internal/psimport/`
  (read-only, **inkrementální a idempotentní** přímá migrace z photo-sorteru — vše za rozhraními
  `Source`/`RunStore`/`PhotoStore`/`VectorStore`/`PeopleStore`/`AlbumStore`/`LabelStore`/`Storage`/
  `Thumbnailer`/`Enqueuer` → unit-testovatelné s faky; `Service` = `New(Config{Source,Runs,Photos,
  Vectors,People,Albums,Labels,Storage,Thumbnailer,Enqueuer,OpenOriginal,PageSize,Logger})` (panika
  na nil collaborator); **`Migrate(ctx) (Result,error)`** otevře `import_runs` běh (`source=photosorter`),
  navrhne na poslední úspěšný watermark: (1) **buildMappings** find-or-create Kukátko subjekt (slug
  z jména) / album (title) / štítek (jméno) pro každý photo-sorter → ps-uid→kk-uid mapy (generický
  `mapCatalogue`); (2) pageuje `ListPhotos(UpdatedSince=watermark)` — per fotka match dle
  `photosorter_uid` (skip), jinak dle **`file_hash`** (backfill `photos.SetPhotosorterRef`, žádné
  kopírování), jinak **zkopíruje originál** z `file_path` (SHA256, náhledy) a `photos.Create` s PS
  metadaty + `photosorter_uid`; (3) **satelity** — embedding (768) a faces (512 + bbox + det_score +
  cache) vloží **1:1** přes `vectors.SaveEmbedding`/`RecordFaceDetection` (zachová model/pretrained,
  remapuje subjekt, zachová marker_uid), fotka **bez** PS embeddingu/detekce dostane Kukátko
  `image_embed`/`face_detect` job; markery (pod původním UID), album/label členství, phash a edit
  best-effort idempotentně; counts **checkpoint po stránce**; pak `Complete` s watermarkem.
  **Per-fotka chyba** → `counts.failed`, **neabortuje běh** (jen infra chyba `Fail`ne); **watermark
  se nikdy neposune za nejstarší selhání** (`runState`); bezpečné re-runovat. **`Handle(ctx,job)`** =
  `worker.HandlerFunc` pro `ps_migrate` (ignoruje payload, volá `Migrate`), `JobPayload()` nese pevný
  sentinel → dedup fronty pustí jen jednu migraci), `internal/importapi/`
  (admin-only HTTP API importů: rozhraní `Queue` (Enqueue, splňuje `*jobs.Store`) a `RunLister`
  (List, splňuje `*importer.Store`); `NewAPI(Config{Queue,Runs,RequireAdmin,EnablePhotoPrism,
  EnablePhotoSorter})`+`RegisterRoutes` mountuje **vždy** `GET /import/runs` (historie + `sources`
  flagy jaké zdroje jsou nakonfigurované) a — **jen pro nakonfigurované zdroje** —
  `POST /import/photoprism` → `pp_import` a `POST /import/photosorter` → `ps_migrate` job (sdílený
  `enqueue` helper, 202 `{job_id,status}`); `jobs.ErrDuplicate` → 409 (už běží), jiná chyba → 500;
  `GET /import/runs` (`parsePaging` limit≤200/offset, neplatný → 400) vrací
  `{runs,limit,offset,sources:{photoprism,photosorter}}` (stránka `import_runs` newest-started-first
  přes `importer.Store.List`); celá API se v `serve` mountuje vždy (`buildImportAPI` v
  `cmd/kukatko/import.go`), aby historie fungovala i bez zdroje; triggery neběží inline — patří na
  background worker), `internal/backup/`
  (v procesu, plánovaná **S3 záloha** databáze a originálů, vše za rozhraními
  `ObjectStore`/`Dumper`/`OriginalSource` → unit-testovatelné s faky bez S3/DB/FS; `Service` =
  `New(Config{Objects,Originals,Dumper,Retention,Logger})` (panika na nil Objects/Originals/Dumper);
  **`Run(ctx,ts)`** dělá tři věci v pořadí: (1) **dump DB** přes `Dumper` streamovaný na S3 jako
  `db/kukatko-<ts>.dump` (`objectSize=-1`, nikdy celý v RAM; ts dodá plánovač/příkaz), (2)
  **inkrementální sync originálů** (`SyncOriginals` — skip dle klíče+velikosti přes `ObjectStore.Stat`,
  klíč = relativní cesta originálu), (3) **retence** (`PruneDumps` — prořeže staré dumpy na posledních
  `Retention`, `≤0` = nechat vše; **jen prefix `db/`, nikdy originály**); **dump je povinný** — selhání
  abortuje běh **před** prořezáním, takže neúspěšná záloha nemůže smazat poslední dobré dumpy;
  `Run` serializuje souběžné běhy (`ErrAlreadyRunning`), `Trigger(ctx,ts)` spustí běh na pozadí
  (detached ctx, pro HTTP handler), `Status()` = stav + poslední běh; **`RunSchedule(ctx,spec)`**
  plánovač přes `ParseSchedule` (standardní 5-pole cron / `@daily`/`@every` deskriptory přes
  `robfig/cron`; prázdný → `ErrNoSchedule`, neplatný → `ErrInvalidSchedule` → plánované zálohy
  vypnuté, manuální fungují) s vlastní ctx-aware smyčkou; **`s3Store`** (`NewS3Store(S3Options)`) =
  minio-go/v7 adaptér, **path-style** (`BucketLookupPath`), `parseEndpoint` (scheme→TLS, bare host =
  TLS), sentinely `ErrNotConfigured`/`ErrInvalidEndpoint`, `isNotFound` (404/NoSuchKey) → Stat
  ok=false / Remove idempotentní; **`pgDumper`** (`NewPgDumper(dsn)`) = shell-out `pg_dump
  --format=custom --no-owner --no-privileges`, **DSN přes env `PGDATABASE`** (ne argument, aby heslo
  nebylo v `ps`), `Dump` vrací reader (Close čeká na proces + surfacuje stderr), `PgDumpAvailable`,
  `ErrPgDumpMissing`; **`DiskOriginals`** (`NewDiskOriginals(root)`) = walk úložiště (skip `.tmp`,
  confine klíče proti traversalu) — **slouží i obnově** přes `Stat(key)` (existuje + velikost, pro
  skip-existing) a `Write(key,r)` (atomický zápis do `.tmp` + rename → resumovatelné); klíče
  tajemství nikdy nelogovat;
  **OBNOVA / disaster recovery** (`restore.go`, `pgrestore.go` — protějšek zálohy): `ObjectStore`
  rozšířeno o **`Open(ctx,key)`** (stream GET z bucketu, na `s3Store` přes `minio GetObject`); nová
  rozhraní **`Restorer`** (`Restore(ctx,archive io.Reader)`), **`LocalOriginals`** (List/Stat/Write,
  splňuje `DiskOriginals`) a **`PhotoCatalog`** (`CountPhotos`/`ListFilePaths`, splňuje `photos.Store`);
  **`RestoreService`** = `NewRestoreService(RestoreConfig{Objects,Restorer,Originals,Photos,Logger})`
  (panika na nil Objects): **`ListDumps`** (dumpy pod `db/` končící `.dump`, nejnovější první) /
  **`LatestDump`** (`ErrNoDumps`) / **`RestoreDatabase(key)`** (prázdný key → nejnovější; streamuje
  dump z S3 rovnou do `Restorer`; `ErrDumpNotFound` na neznámý key — **destruktivní**) /
  **`RestoreOriginals`** (stáhne z bucketu jen chybějící originály — skip dle klíče+velikosti přes
  `LocalOriginals.Stat`, dumpy pod `db/` přeskočí, atomický `Write` → resumovatelné, ctí ctx cancel,
  `RestoreOriginalsResult{Downloaded,Skipped}`) / **`Verify`** (integritní report `VerifyReport`
  {PhotosInDB,FilesInDB,OriginalsOnDisk,MissingOnDisk,ExtraOnDisk,Consistent} přes čistou `reconcile`
  set-diff `photo_files.file_path` vs disk); **`pgRestorer`** (`NewPgRestorer(dsn)`) = shell-out
  `pg_restore --format=custom --clean --if-exists --no-owner --no-privileges --single-transaction
  --dbname=<db>`, čte archiv **ze stdin** (nikdy celý v RAM), **DSN parsován do PG\* env**
  (`PGHOST`/`PGPORT`/`PGUSER`/**`PGPASSWORD`**/`PGDATABASE` přes `pgx.ParseConfig`) → heslo **nikdy
  v argv**; `PgRestoreAvailable`, sentinely `ErrPgRestoreMissing`/`ErrInvalidDSN`; tajemství nikam
  neprosáknou), `internal/backupapi/`
  (admin-only HTTP API nad zálohou: rozhraní `Service` (Status+Trigger, splňuje ho `*backup.Service`,
  fakeovatelné, **nil = nenakonfigurováno**); `NewAPI(Config{Service,RequireAdmin})`+`RegisterRoutes`
  mountuje `GET /backup` (stav + poslední běh, nil service → `configured:false`) a `POST /backup`
  (spustí `Trigger` na pozadí → 202 `{status:"started"}`, `ErrAlreadyRunning` → 409, nil service →
  503); mountuje se v `serve` vždy (`buildBackupAPI` v `cmd/kukatko/backup.go`)), `internal/restoreapi/`
  (admin-only HTTP API nad obnovou, **jen read-only operace**: rozhraní `Service`
  (`ListDumps`+`Verify`, splňuje ho `*backup.RestoreService`, fakeovatelné, **nil = nenakonfigurováno**);
  `NewAPI(Config{Service,RequireAdmin})`+`RegisterRoutes` mountuje `GET /restore/dumps` (seznam dumpů,
  503 bez konfigurace, 502 při chybě S3) a `POST /restore/verify` (integritní report, 503 bez
  konfigurace); **destruktivní obnova DB se přes HTTP záměrně neexponuje** (podtrhla by tabulky
  běžícímu serveru — patří do CLI při zastaveném serveru); mountuje se v `serve` vždy
  (`buildRestoreAPI` v `cmd/kukatko/restore.go`)), `internal/maintenance/`
  (**integritní kontrola & opravy knihovny** — udržuje velkou dlouhožijící knihovnu konzistentní:
  odhalí drift mezi katalogem a soubory na disku a doplní/přegeneruje odvozená data; zrcadlí
  photo-sorter `cache build-thumbs`, ale je širší a bezpečnější (**nikdy nemaže originály** — to je
  práce koše/purge), idempotentní, opravy přes persistentní frontu jobů; vše za rozhraními
  `PhotoCatalog` (`CountPhotos`/`ListPrimaryFiles`/`ListFilePaths`/`ListPhotosMissingPhash`,
  splňuje `photos.Store`)/`VectorCatalog` (`ListPhotosMissingEmbedding`/`ListPhotosMissingFaces`,
  `vectors.Store`)/`OriginalStore` (`Stat`, `storage.Storage`)/`DiskScanner` (`List`, adaptér nad
  `backup.DiskOriginals`)/`ThumbChecker` (`HasThumbnail`, `NewThumbCache` nad `thumb.Thumbnailer`)/
  `Enqueuer` (`EnqueueThumbnail`, `jobs.Enqueuer`)/`EmbedBackfiller` (`embedjob.Service`)/
  `FaceBackfiller` (`facejob.Service`)/`OrphanImporter` (volitelný, nil vypne orphan import) →
  unit-testovatelné s faky bez DB/disku/fronty; `Service` = `New(Config{...,SampleLimit})`
  (panika na nil povinný kolaborant; default `SampleLimit` 20); **`Scan(ctx)`** (read-only) vrátí
  `Report{Photos,FilesInDB,OriginalsOnDisk,MissingOriginals,OrphanFiles,MissingThumbnails,
  MissingEmbeddings,MissingFaces,MissingPhashes}` — každá třída je `Finding{Count,Samples}`
  (count + omezený vzorek identifikátorů); `representativeThumbSize`=`tile_224` je proxy přítomnosti
  náhledů, orphan = soubor na disku bez `photo_files.file_path` (`orphanKeys` set-diff), `Report.Clean()`;
  **`Repair(ctx,RepairOptions{Thumbnails,Embeddings,Faces,Phashes,ImportOrphans})`** (každá opt-in,
  idempotentní, pevné pořadí) → `RepairResult` se scheduling počty: thumbnails/phashes zařadí
  `thumbnail` joby (`EnqueueThumbnail`), embeddings/faces volají backfill, orphan import jede přes
  upload pipeline (per-orphan selhání se počítá bez abortu); `ErrOrphanImportUnavailable` když je
  import vybrán bez importéru), `internal/thumbjob/`
  (worker handler `thumbnail` jobu — **repair path** pro maintenance: regeneruje z originálu odvozená
  data fotky, **náhledy** (`Thumbnailer.GenerateAll`, skip cachovaných) a **pHash/dHash** (jen když
  chybí, `phash.Compute` nad dekódovaným originálem), vše za rozhraními `PhotoStore`/`Thumbnailer`/
  `Decoder` (`StorageDecoder` = `storage.AbsPath`+`imgconvert.EnsureDecodable`, fakeovatelný) →
  unit-testovatelné bez disku; `Service` = `New(Config{Photos,Thumbnailer,Decoder})` (panika na nil),
  `Handle`=`worker.HandlerFunc` (payload `{photo_uid}`, prázdný → `ErrMissingPhotoUID` dead-letter),
  `Regenerate(uid)`/`ensurePhash` idempotentní; registrovaný v `serve` na `jobs.TypeThumbnail`),
  `internal/maintenanceapi/`
  (admin-only HTTP API nad maintenance: rozhraní `Service` (Scan+Repair, splňuje `*maintenance.Service`,
  nil → 503); `NewAPI(Config{Service,RequireAdmin})`+`RegisterRoutes` mountuje `/maintenance`:
  `GET /maintenance/scan` (integritní report) a `POST /maintenance/repair` (tělo `RepairOptions`,
  `DisallowUnknownFields`, prázdný výběr → 400, `ErrOrphanImportUnavailable` → 503, jinak `RepairResult`);
  mountuje se v `serve` (`buildMaintenanceAPI` v `cmd/kukatko/maintenance.go`, service staví
  `buildMaintenanceAndThumb` sdílený s registrací `thumbnail` handleru v `buildJobs`)),
  `internal/duplicates/`
  (**review surface pro near-duplicate fotky** nad rámec upload-time varování: linkuje fotky dvěma
  signály — pHash Hammingova vzdálenost do `duplicate.phash_max_diff` a embedding cosine vzdálenost
  do `duplicate.embedding_max_dist` — a slévá hrany do souvislých komponent přes union-find
  (`algo.go` disjoint-set + path compression/union by rank); **bez O(n²)**: pHash přes **banded-LSH**
  buckety (`bandCount`=`maxDiff+1` pásem dle pigeonhole garantuje sdílený bucket pro páry do prahu,
  kandidáti se ověří plnou Hammingovou vzdáleností), embeddingy přes HNSW (`vectors.FindDuplicatePairs`).
  Vše za rozhraními `PhotoSource` (`ListByUIDs`)/`PhashSource` (`ListActivePhashes`)/`EmbeddingSource`
  (`FindDuplicatePairs`, nil vypne embedding grouping) → unit-testovatelné s faky; `Service` =
  `New(Config{Photos,Phashes,Embeddings,PhashMaxDiff,EmbeddingMaxDist,Neighbours})` (panika na nil
  Photos/Phashes; `PhashMaxDiff<0` vypne pHash, `EmbeddingMaxDist<=0` vypne embedding);
  **`FindGroups(ctx,limit,offset)`** (backing `GET /duplicates`) → `Result{Groups,Total,Limit,Offset,
  NextOffset}`; každá `Group{ID (nejmenší uid),Reason (phash/embedding/both),KeeperUID,Members}`,
  `Member` nese rozměry/velikost/`taken_at`/media_type + `is_keeper` + `phash_distance`/
  `embedding_distance` ke keeperovi; **navržený keeper** = nejvyšší rozlišení → největší soubor →
  nejstarší → nejmenší uid (`selectKeeperIndex`); skupiny řazené largest-first/newest-keeper/id,
  `limit` clamp `[1,100]`; jen čte, **nikdy nemutuje** (úklid jde přes bulk/archive API); archivované
  fotky se nescanují (`ListActivePhashes` filtruje `archived_at IS NULL`)), `internal/duplicatesapi/`
  (editor/admin HTTP API nad detekcí duplikátů: rozhraní `Service` (`FindGroups`, splňuje
  `*duplicates.Service`, **nil → 503** ať route existuje i při vypnuté detekci);
  `NewAPI(Config{Service,RequireWrite})`+`RegisterRoutes` mountuje `GET /duplicates` za `RequireWrite`
  (query `limit`≤100/`offset`, neplatný → 400, sken selže → 500); mountuje se v `serve`
  (`buildDuplicatesAPI` v `cmd/kukatko/duplicates.go`, při `duplicate.enabled=false` nil služba)),
  `internal/system/`
  (agregace provozního stavu instance pro admin **system-status dashboard** — žádná nová data, jen
  sloučení existujících subsystémů; vše za malými rozhraními `DBPinger` (`database.DB`)/
  `EmbeddingHealth` (`embedding.Client.Healthy`)/`JobCounter`
  (`jobs.Store.CountsByState`/`CountsByType`/`CountPending`)/`ImportLister` (`importer.Store.LatestRun`)/
  `BackupReporter` (`backup.Service.Status`, **nil = nenakonfigurováno**) → unit-testovatelné s faky
  bez DB; `Service` = `New(Config{DB,Embeddings,EmbeddingURL,Jobs,Backup,Imports,OriginalsPath,
  CachePath,StorageTTL,Clock})`; **`Collect(ctx) (Status,error)`** sbírá `Status{Version,Database,
  Embeddings,Jobs,Backup,Imports,Storage}`: embeddings online/offline, fronta (by_state/by_type/total/
  dead_letter/pending_embeddings = queued+running `image_embed`/`face_detect`), backup stav+poslední
  výsledek, poslední import per zdroj, úložiště (velikost originálů+cache walkem, volné/celkové místo
  `statfs` přes `golang.org/x/sys/unix`, **memoizováno** `storageCache` na `defaultStorageTTL` 30 s aby
  polling nepřecházel strom), DB reachability (`Ping`, **sanitizovaná** chyba), verze/commit; chyby
  čtení fronty/importů (vyžadují DB) → error (500), nedostupná DB a nečitelné úložiště inline
  best-effort), `internal/systemapi/`
  (admin-only HTTP API nad system stavem: rozhraní `StatusCollector` (`Collect`, splňuje
  `*system.Service`, fakeovatelné); `NewAPI(Config{Service,RequireAdmin})`+`RegisterRoutes` mountuje
  `GET /system/status` za `RequireAdmin` (snapshot; collect selže → 500); mountuje se vždy
  (`buildSystemAPI` v `cmd/kukatko/system.go`, staví vlastní bezstavový embeddings klient jen pro
  Healthy probe, sdílí pool pro job/import stores, backup služba předaná nil-safe; mountuje se
  v `appendOpsAPIs` vedle backup/restore)), `internal/ratelimit/`
  (znovupoužitelný **per-key token-bucket rate limiter** + HTTP middleware pro náročné endpointy:
  `New(ratePerSec, burst)` → `Allow(key)` (lazy refill, per-klíč bucket) / `Cleanup`/`RunMaintenance`
  (úklid plně doplněných bucketů) / `Middleware` (chi-kompatibilní, keyuje **client IP** přes
  `clientIP` z `RemoteAddr` — chi `RealIP` ji plní z `X-Forwarded-For`/`X-Real-IP`; prázdný bucket →
  **429** + `Retry-After`); `ratePerSec ≤ 0` → **disabled** limiter (Allow vždy true, Middleware
  no-op — endpoint se vypne čistě configem); paměťově omezený opportunistickým úklidem při `maxBuckets`
  (8192), takže nepotřebuje externí goroutinu; mountuje se jako outermost middleware ahead-of-auth na
  `POST /upload` (ingest), `POST /photos/bulk` (bulkapi), `POST /import/*` (importapi) a
  `GET /map/tiles/...` (mapsapi) — limity z `ratelimit.*` configu; login a geocode mají vlastní
  limitery), `internal/web/`
  (SPA fallback handler `web.Handler()`/`SPAHandler` + `internal/web/static` embed
  `//go:embed all:dist/*`; Vite build se zapisuje do `internal/web/static/dist`, ten je
  gitignorovaný kromě committed `.gitkeep`, aby embed kompiloval i bez buildnutého
  frontendu). Detail: [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).
- **Frontend layout:** `web/` (Vite + React 19 + TS): `web/src/` s `components/`
  (`Layout` = navbar shell s user-menu/logout + role-gated nav — odkaz **Knihovna**
  míří na `/library`, **Oblíbené** na `/favorites`, **Alba** na `/albums`, **Štítky** na `/labels`,
  **Hledat** na `/search`,
  **Lidé** na `/people`, **Mapa** na `/map`, **Místa** na `/places`, **Nahrát** na `/upload` (jen editor/admin),
  **Koš** na `/trash` (jen editor/admin, gate `canWrite`),
  **Duplikáty** na `/duplicates` (jen editor/admin, gate `canWrite`),
  **Import** na `/import` (jen admin, gate `isAdmin`),
  **Systém** na `/system` (jen admin, gate `isAdmin`),
  `NavbarSearch` (kompaktní vyhledávací pole v navbaru s **živým grouped quick-results dropdownem**:
  jak uživatel píše, debouncovaně (`useGlobalSearch`) volá `GET /search/global` a zobrazí shodné
  **alba/štítky/lidé/fotky** seskupené dle typu s náhledy; klik na řádek naviguje přímo na entitu
  (album→`/albums/{uid}`, štítek→`/labels/{uid}`, osoba→`/people/{uid}`, fotka→`/photos/{uid}`),
  Enter/Submit jde na plnou stránku `/search?q=…`; dropdown je klávesnicově ovladatelný (šipky/Enter),
  zavírá se na blur/Escape, empty/loading/error stavy vč. „Nic nenalezeno"),
  `SavedSearchesMenu` (navbar dropdown uložených hledání vedle vyhledávacího pole — lazy fetch při
  otevření, položky otevírají uložený pohled přes `savedSearchHref`, „Spravovat" míří na `/saved`),
  `LanguageSwitcher`,
  `KeyboardShortcutsHelp` (v navbaru: ikonka klávesnice + **modal nápovědy zkratek** — otevře se
  `?` (Shift+/) kdekoli nebo klikem, vypíše všechny zkratky seskupené dle kontextu (Mřížka / Detail)
  ze `lib/shortcuts.ts` `SHORTCUT_GROUPS`, zavře Escapem/křížkem);
  `components/upload/` = `DropZone` (drag-and-drop zóna + file input `multiple`
  `accept="image/*,video/*"` → mobilní galerie + tlačítko **Vyfotit** `capture="environment"`),
  `UploadItem` (řádek fronty: jméno+velikost, progress-bar, status badge, near-duplicate
  varování, remove/retry akce); `components/library/` = `PhotoTile`
  (čtvercová lazy-load dlaždice → `/photos/{uid}`, badge soukromé, **play badge + délka** u
  videa/live fotky (`▶` + `formatDuration`), placeholder bez
  layout-shiftu; volitelný **favorite heart** overlay `favoritable` → `FavoriteButton`;
  volitelný **rating overlay** `ratable` → kompaktní `RatingStars`+`FlagControl` (per-user
  hvězdy 0–5 + pick/reject) nad `useRating`, plus **hotkeys na fokusnuté dlaždici** `0`–`5`
  nastaví hodnocení a `p`/`r` pick/reject (`ratingHotkey`/`isTypingElement`, nefungují při psaní
  do inputu); **zamítnutá fotka** je ztlumená + má reject badge; heart i rating overlay se
  v selection módu skryjí),
  `PhotoGrid` (virtualizovaný **`react-virtuoso` `VirtuosoGrid`**,
  window-scroll, `endReached` → další stránka, footer spinner/retry; props `favoritable`/`ratable`
  prosáknou srdíčko a hvězdy/flag na dlaždice; volitelný `gridRef` (imperativní `scrollToIndex`
  handle) + `onRangeChanged` (viditelný rozsah) pro časovou osu),
  `TimelineScrubber` (**časová osa** — tenká fixní svislá datová lišta u mřížky: fetchne měsíční
  histogram přes `useTimeline(params)` (refetch při změně filtrů), každý měsíc = klikací tick
  umístěný proporčně dle `cumulative/total`, měsíční popisky přes `lib/format` `formatMonth`;
  klik/tažení skočí na měsíc přes `onJump(bucket.cumulative)`, aktivní měsíc se zvýrazní dle
  `activeIndex` (start viditelného rozsahu); overlay `position: fixed`, takže loading/prázdný
  timeline nerendruje nic a neposouvá layout, na malých šířkách se skryje přes `styles/app.css`
  `.kukatko-timeline*`; jen pro výchozí newest řazení), `FilterBar`
  (datum od/do, poloha, soukromé, fotoaparát, archiv, **min. hodnocení ≥1…≥5**, **flag
  vybrané/zamítnuté**, řazení (vč. **dle hodnocení**) + počet + „zrušit filtry";
  generický nad `LibraryView`+supersetem, props `showSearch`/`showSort` skryjí dotaz/řazení
  na search stránce), `SimilarPhotos` (znovupoužitelný horizontálně scrollovatelný pruh
  podobných fotek nad `GET /photos/{uid}/similar` přes `fetchSimilar`, odkazy na detail,
  empty-friendly + loading/error, refetch při změně `uid`),
  `FavoriteButton` (heart toggle nad `useFavorite` — **optimistický** per-user favorite
  s rollbackem; bez role-gate, smí každý přihlášený; jako overlay na dlaždici je sibling
  linku, takže klik nenaviguje), `RatingStars` (pure controlled 0–5 hvězd; klik na aktuální
  hodnocení maže na 0; bez `onRate` read-only display) + `FlagControl` (pure controlled pick/
  reject toggle, klik na aktivní flag maže na `none`; oba sibling linku → klik nenaviguje),
  `GridSkeleton` (placeholder mřížka při prvním načtení); `PhotoTile`+`PhotoGrid` podporují
  volitelný **selection mód** (props `selectable`/`selected`/`onToggleSelect`, resp. `selection`;
  heart i rating overlay se v selection módu skryjí),
  `components/organize/` = `AlbumTile` (karta alba: cover/název/počet → `/albums/{uid}`),
  `AlbumEditModal` (create/rename alba: název/popis/soukromé), `LabelEditModal` (create/rename
  štítku: jméno/priorita), `ReorderableGrid` (ne-virtualizovaná drag-and-drop mřížka + šipky pro
  přeřazení alba, controlled přes `onReorder`), `SelectionBar` (sticky toolbar výběru: počet +
  akce + zrušit), `BulkEditModal` (**hromadná úprava** výběru přes `POST /photos/bulk`: add/remove
  alba, add/remove štítku, set/clear popisu, set/clear polohy, soukromé, archiv, oblíbené — set/clear
  páry jako samostatné módy; klientská validace souřadnic + „aspoň jedna změna"; po aplikaci
  **per-foto result summary** z odpovědi),
  `pages/` (`HomePage` volá `GET /healthz`, `LoginPage`, `AccountPage` = změna vlastního hesla,
  `LibraryPage` = hlavní foto-knihovna: `FilterBar` nad virtualizovanou nekonečně-scrollující
  mřížkou, loading/empty/error stavy, celý pohled (filtry+řazení) v URL, srdíčka **i hvězdy/flag**
  na dlaždicích (favoritable+ratable, rating hotkeys na fokusnuté dlaždici), tlačítko **Promítání**
  (`slideshowHref` → `/slideshow` s aktuálními filtry/řazením),
  plus **časová osa** (`TimelineScrubber`) vedle mřížky pro rychlé skoky na měsíc — mřížka
  vystaví `gridRef`+`onRangeChanged`, skok jede přes `useGridJump` (donačte stránky, když měsíc
  leží za načtenou částí), zobrazí se jen pro výchozí newest řazení a mimo režim výběru,
  plus pro editory **režim výběru** (`Vybrat`/`Vybrat vše`) → `BulkEditModal`
  (hromadná úprava metadat přes bulk API), plus tlačítko **Uložit pohled** (`SaveSearchModal` →
  `createSavedSearch` s aktuálním view objektem jako `params`),
  `SavedSearchesPage` = `/saved` (jakýkoli přihlášený) „Moje uložená hledání": seznam uložených
  pohledů aktuálního uživatele, každý odkaz otevírá přesně obnovený pohled (`savedSearchHref`), plus
  přejmenování (`SaveSearchModal`) a **optimistické mazání** + empty state,
  `FavoritesPage` = `/favorites` oblíbené aktuálního uživatele: stejná mřížka/filtry jako knihovna
  scopnutá `favorite=true`, srdíčka pro odebrání z oblíbených + hvězdy/flag na místě (ratable),
  `AlbumsPage` = `/albums` mřížka karet alb + `Nové album` (editor/admin),
  `AlbumDetailPage` = `/albums/:uid` hlavička + tlačítko **Promítání** (všem) + editorské akce
  (upravit/smazat/vybrat/přeřadit) nad
  fotomřížkou scopnutou na album (`useScopedPhotos` + `FilterBar` + URL stav); přeřazení přes
  `ReorderableGrid`→`PATCH /albums/{uid}/order`, výběr → odebrat z alba / nastavit cover,
  `LabelsPage` = `/labels` seznam štítků s počty + create/rename/delete (editor/admin),
  `LabelDetailPage` = `/labels/:uid` fotomřížka scopnutá na štítek (`useScopedPhotos` + `FilterBar` + URL)
  + tlačítko **Promítání**,
  `SearchPage` = sémantické/hybridní/fulltext hledání: prominentní debouncované (350 ms)
  vyhledávací pole + přepínač režimu (`q`+`mode` v URL), stejná virtualizovaná mřížka jako
  knihovna + sdílený `FilterBar` (bez dotazu/řazení), `degraded` → neblokující upozornění
  (sidecar offline), idle/loading/empty/error stavy, plus nad mřížkou **cross-entity sekce**
  (`GlobalSearchSections`) s chipy shodných alb/lidí/štítků (grouped `GET /search/global`), aby
  textový dotaz vynesl i nefotkové entity, plus tlačítko **Uložit pohled**
  (`SaveSearchModal` — `params` nese i `mode`, takže obnova míří na `/search`),
  `UploadPage` = multiupload (drag-and-drop + galerie/fotoaparát na mobilu): `DropZone`
  nad frontou `UploadItem`, per-file progress/status, souhrn počtů, start/clear/retry-failed,
  po dokončení odkaz na nově nahrané fotky (`/library?sort=added`),
  `ImportPage` = `/import` (jen admin) admin konzole importu/migrace: dvě sekce (PhotoPrism,
  photo-sorter) s tlačítkem **Spustit import** (gate na `sources` flagy), živý průběh běžícího běhu
  (spinner + counts imported/updated/skipped/failed) a stav fronty na pozadí (`GET /jobs/stats`),
  plus tabulka **historie běhů** (`import_runs`: zdroj/začátek/konec/stav/počty/chyba); polluje
  `GET /import/runs` + `GET /jobs/stats` po 3 s, 409 → „už běží", confirm před prvním (velkým) během
  zdroje, sebe-gate na `isAdmin`,
  `MaintenancePage` = `/maintenance` (jen admin) konzole údržby knihovny: tlačítko **Spustit kontrolu**
  (`GET /maintenance/scan`) → souhrn totálů + tabulka nálezů (počet + vzorky per třída, nebo „knihovna
  konzistentní"), checkboxy oprav (náhledy/embeddingy/obličeje/hashe/import osiřelých — anotované
  zbývajícím počtem z poslední kontroly) → **Spustit opravy** (`POST /maintenance/repair`) s výsledným
  souhrnem, plus stav fronty na pozadí (`GET /jobs/stats` polluje po 3 s) jako progress; sebe-gate na
  `isAdmin`,
  `SystemStatusPage` = `/system` (jen admin) **system-status dashboard**: auto-refresh (polling 5 s)
  `GET /system/status` → kartová mřížka (DB, embeddingy, fronta jobů, záloha, importy, úložiště,
  verze) s **rychlými akcemi** — *znovu zařadit mrtvé úlohy* (`requeueDeadLetterJobs`: list dead →
  per-job `POST /jobs/{id}/requeue`), *spustit zálohu* (`POST /backup`), odkazy na flow importu
  (`/import`) a kontroly údržby (`/maintenance`); **box offline** + čekající embeddingy → zvýrazněná
  hláška „doženou se po návratu"; loading/error/notice stavy, sebe-gate na `isAdmin`,
  `PhotoDetailPage` = `/photos/:uid` **bohatý detail fotky**: velký náhled (`fit_1920`)
  reflektující uložený nedestruktivní edit (CSS) — u **videa** místo obrázku `VideoPlayer`
  (`components/photo/`, HTML5 `<video controls>` nad range endpointem `…/video`, poster `fit_1920`,
  klávesy/fullscreen/touch zdarma, fallback na stažení když codec prohlížeč neumí), u **live fotky**
  `LivePhoto` (still + „Live" badge, motion klip se přehraje při hover/podržení/focusu), **prev/next
  navigace** respektující pořadí
  zdrojového výpisu (`usePhotoNeighbors` pageuje stejný `GET /photos` se scope+filtry z URL),
  deep-linkovatelný + **Zpět** na zdrojový pohled (`lib/detailView` `backHref`/`detailToParams`/
  `detailQueryString`), v hlavičce `RatingStars`+`FlagControl` (per-user hvězdy 0–5 + pick/reject
  nad `useRating`) a `FavoriteButton`, plus **rating hotkeys** `0`–`5`/`p`/`r` na document (mimo
  psaní do inputu), tlačítka **Stáhnout originál** /
  **Stáhnout upravenou** (`downloadUrl`), interaktivní `FaceOverlay` (pojmenování obličejů),
  pruh `SimilarPhotos` a pravý panel se záložkami (`components/photo/`): **Informace**
  (`MetadataPanel` = view/edit title/description/notes/taken_at + camera/lens/EXIF + lat/lng,
  PATCH přes `updatePhoto`; `OrganizePanel` = inline add/remove alb a štítků přes organize API),
  **Poloha** (`PhotoLocation` = Leaflet mini-mapa nad mapy.com proxy + on-demand reverse-geocode
  `reverseGeocode` + clear location) a **Úpravy** (editor/admin: `EditPanel` = rotace/jas/kontrast/
  crop s živým CSS preview, `PUT /photos/{uid}/edit` přes `saveEdit`); viewer vidí read-only
  (žádná záložka Úpravy, žádné edit akce, `FaceOverlay` readOnly); `lib/photoEdit` = pure helpery
  edit→CSS (`editPreviewStyle`/`editFilter`/`editTransform`/`cropClipPath`/`isIdentityEdit`/
  `rotateRight`/`hasCrop`/`NEUTRAL_EDIT`),
  `PeoplePage` = `/people` index osob: responzivní mřížka `SubjectTile` (cover/jméno/počet
  fotek), editorům odkaz na review shluků,
  `SubjectPage` = `/people/:uid` stránka osoby: hlavička (jméno/typ + edit přes
  `SubjectEditModal`), paginovaná galerie (`useSubjectPhotos` + `SubjectPhotoTile` se
  „set as cover" akcí editorům), a sekce `Outliers` (jen editor/admin),
  `ClustersPage` = `/people/clusters` (editor/admin) review fronta nepojmenovaných shluků:
  `ClusterCard` (reprezentant + ukázky + odebrání zatoulaného obličeje + jednorázové pojmenování
  celého shluku) v `Row`/`Col` mřížce, optimistické odebrání po pojmenování,
  `MapPage` = `/map` mapový pohled: geotagované fotky jako shlukované markery nad mapy.com
  dlaždicemi (Leaflet), přepínač podkladu + filtry (datum/archiv/soukromé) v `MapFilterBar`,
  stav (mapset/viewport/filtry) v URL — posun/zoom zapisuje viewport bez refetche, změna filtru
  dotáhne GeoJSON; klik na marker → detail fotky; loading/empty/error stavy,
  `PlacesPage` = `/places` procházení knihovny dle lokality: jedním fetchem `fetchPlaces()` natáhne
  hierarchii zemí→měst s počty; **drill v URL** (`?country=&city=` přes `useUrlState` nad
  `PlacesView` = `LibraryView`+`country`/`city`, takže Zpět prochází úrovně) — úroveň 1 seznam zemí
  (`ListGroup`), úroveň 2 města vybrané země (z nested dat, bez refetche), úroveň 3 fotomřížka
  scopnutá na `{country,city}` přes `useScopedPhotos` (enabled až po výběru města) + sdílený
  `FilterBar` + breadcrumb Místa/země/město; loading/empty/error stavy,
  `SlideshowPage` = `/slideshow` fullscreen promítání (mimo `Layout`, bez navbaru): čte scope
  (`?album=`/`?label=`/žádné) + filtry/řazení z URL (stejný stav jako mřížka), pageuje přes
  `usePaginatedPhotos` (`fetchPhotos`, velké sady se nenačítají najednou), řídí `useSlideshow` +
  `useSlideshowSettings`, renderuje loading/empty/error stavy nebo `Slideshow`; exit → `navigate(-1)`
  (fallback na zdrojový pohled), takže Zpět funguje,
  `TrashPage` = `/trash` (editor/admin) koš: archivované fotky (`useScopedPhotos`-style listing přes
  `usePaginatedPhotos` scopnutý `archived=only`) v mřížce `TrashCard` s `FilterBar`, **obnova**
  (`unarchivePhoto`) a **trvalé mazání** (`purgePhoto`) jednotlivě i hromadně (`useSelection`
  `SelectionBar`), **Vyprázdnit koš** (`emptyTrash`), každá trvalá akce přes potvrzovací `Modal`;
  `fetchTrashInfo` dotáhne retenci pro odpočet na kartách,
  `DuplicatesPage` = `/duplicates` (editor/admin) kontrola duplikátů: stránkovaný seznam skupin
  (`fetchDuplicates`, „načíst další" přes `next_offset`) v `DuplicateGroupCard`; per skupina uživatel
  vybere keeper a **archivuje zbytek** přes `bulkUpdatePhotos(archiveUids,{archive:true})` (zbytek do
  koše, vratné) → skupina zmizí + success alert s počtem, nebo skupinu **odmítne** („není duplikát",
  jen lokálně skryje); 503 → „nedostupné", loading přes `GridSkeleton`, error s retry,
  `NotFoundPage`),
  `components/savedsearch/` = `SaveSearchModal` (modal pro pojmenování při uložení nového pohledu
  i přejmenování existujícího uloženého hledání) + `SavedSearchesMenu` (navbar dropdown, lazy fetch
  při otevření, položky → uložený pohled, „Spravovat" → `/saved`);
  `components/search/` = `GlobalSearchSections` (kompaktní cross-entity sekce nad photo mřížkou
  search stránky: přes `useGlobalSearch(query)` natáhne grouped `GET /search/global` a vyrenderuje
  chipy shodných **alb/lidí/štítků** odkazující na entitu; nezávislé na photo fulltext/semantic
  hledání pod ním, nerendruje nic dokud nepřijde aspoň jedna nefotková shoda — prázdný dotaz /
  probíhající hledání / jen-fotky shoda nepřidá žádné chrome);
  `components/trash/` = `TrashCard` (dlaždice archivované fotky: náhled + odpočet do auto-purge přes
  `trashCountdown` + restore/delete akce + výběr v selection módu);
  `components/duplicates/` = `DuplicateGroupCard` (karta skupiny: členové vedle sebe s náhledem/
  rozměry/velikostí/`taken_at`/vzdálenostmi, radio výběr keepera (default navržený), badge `reason`,
  akce **Archivovat ostatní** / **Není duplikát**, busy stav);
  `components/slideshow/` = `Slideshow` (prezentační fullscreen stage: aktuální fotka v preview
  velikosti `fit_1920` s CSS přechodem dle `settings.effect`, přednačítání sousedních snímků přes
  `new Image()`, ovládání předchozí/play-pause/další/fullscreen/nastavení/zavřít + titulek + pozice
  `n/total`; klávesy ←/→ / mezerník / Esc / F a dotykový swipe; Fullscreen API feature-detected;
  panel nastavení = výběr efektu + rychlosti) + `slideshow.css` (keyframes `slideshow-fade`/
  `slideshow-slide`, fullscreen layout);
  `components/map/` = `LeafletMap` (imperativní Leaflet most: dlaždicová vrstva na **backend
  proxy** `/api/v1/map/tiles/{mapset}/{z}/{x}/{y}{r}` (klíč server-side, `{r}`→`@2x` na retině),
  **povinné mapy.com prvky** — attribution „© Seznam.cz a.s. a další" → `/copyright` a klikatelné
  **logo** vlevo dole → `mapy.com`; `leaflet.markercluster` shluky (klik přibližuje), markery
  z GeoJSON, popup s náhledem → detail fotky; jednorázový setup, výměna URL dlaždic při změně
  mapsetu, přestavba markerů při změně fotek, fit-bounds na markery), `MapFilterBar` (přepínač
  podkladu basic/outdoor/aerial + datum od/do, archiv, soukromé, počet, zrušit filtry);
  `components/people/` = `SubjectTile`/`SubjectPhotoTile`/`SubjectEditModal`, `FaceThumb`
  (čtvercový výřez obličeje z thumbnailu fotky dle normalized bbox přes `faceCropStyle`),
  `FaceOverlay`+`FaceAssignPanel` (boxy přes obrázek z normalized bbox přes `faceBoxStyle`,
  klik → panel s návrhy (one-tap accept) + free-text jméno; optimistický update + refetch),
  `ClusterCard`, `Outliers` (žebříček podezřelých obličejů s one-tap unassign);
  `auth/` (`AuthContext`/`useAuth` + `AuthProvider` = boot `GET /auth/me`,
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
  frontu efektem po `start`/retry, ruší běžící uploady při unmountu;
  `useSubjectPhotos` = obálka nad `usePaginatedPhotos` nad `GET /subjects/{uid}/photos`
  (galerie osoby, reset+reload při změně `uid`); `useScopedPhotos` = obálka nad `usePaginatedPhotos`
  nad `GET /photos` scopnutým na album/štítek/**lokalitu** (`PhotoScope` `{album?,label?,country?,city?}`
  + filtry/sort z URL, options `{reloadKey?,enabled?}` — `reloadKey` pro refetch po mutaci, `enabled:false`
  → idle bez fetche, např. Places před výběrem města); `useMapPhotos` = jednorázový (nestránkovaný) loader
  GeoJSON feedu geotagovaných fotek nad `fetchMapPhotos` (`status` loading/ready/error, `retry`,
  ruší in-flight + ignoruje stale při změně filtrů); `useTimeline(params)` = jednorázový loader
  měsíčního date-histogramu nad `fetchTimeline` (`buckets`/`total`/`status`, refetch při změně
  filtrů, ruší in-flight + ignoruje stale — podklad `TimelineScrubber`); `useGlobalSearch(query,
  debounceMs?)` = debouncovaný (default 250 ms) grouped global-search loader nad `globalSearch`
  (`status` idle/loading/ready/error + `result`, prázdný dotaz → idle bez requestu, ruší in-flight +
  ignoruje stale — podklad navbar quick-results i `GlobalSearchSections`); `useGridJump({gridRef,
  loadedCount,hasMore,loadingMore,loadMore})` = vrátí `jumpTo(index)`, který skočí mřížkou na foto
  index přes `VirtuosoGridHandle.scrollToIndex` a **nejdřív donačte stránky**, když cíl leží za
  infinite-scroll kurzorem (nebo clampne na poslední načtené, když už další stránky nejsou) —
  podklad skoku časové osy na měsíc před načtenou částí; `useSelection` = multi-výběr fotek v mřížce
  (`active`/`selected`/`count`/`enable`/`disable`/`toggle`/`selectMany` (select-all-in-view)/`clear`);
  `useKeyboardShortcuts(handlers,{enabled?})` = sdílené plumbing všech klávesových zkratek: jeden
  document-level `keydown` listener dispatchuje dle normalizovaného `shortcutToken(event.key)` na
  `handlers` (přes refy, bind jednou a vždy vidí aktuální closury), matched key `preventDefault`;
  **nikdy nevystřelí** při držení Ctrl/Meta/Alt, při psaní (`isTypingElement`) ani při otevřeném
  form-modalu (`isFormModalOpen`); `useGridKeyboardNavigation({count,enabled,resetKey,getColumns,
  scrollToIndex,onOpen,onToggleSelect,onToggleFavorite,hasSelection,onClearSelection})` = navigace
  mřížky nad `useKeyboardShortcuts`: drží `focusedIndex` (zvýraznění), šipky + `j`/`k`/`h`/`l` posouvají
  (vlevo/vpravo o 1, nahoru/dolů o řádek dle živého počtu sloupců) a dorolují dlaždici do view, `Enter`
  otevře, `x` vybere (zapne selection mód), `f` přepne oblíbenou, `Escape` zruší nejdřív výběr, pak
  fokus; fokus se resetuje na `resetKey` (nová filtr/sort/scope);
  `useFavorite(uid,initial)` = **optimistický** per-user favorite toggle nad `favoritePhoto`
  (`PUT`/`DELETE …/favorite`), rollback při chybě, ignoruje souběžný toggle, resync na změnu
  `uid`/server stavu; `useRating(uid,initialRating,initialFlag)` = **optimistické** per-user
  hodnocení (hvězdy) + pick/reject flag nad `ratePhoto` (`PUT …/rating` jen s měněným polem),
  `setRating`/`setFlag` s per-poli rollbackem při chybě, no-op na shodnou hodnotu, `pending` přes
  in-flight counter, resync na změnu `uid`/server stavu (mirror `useFavorite`);
  `useSlideshow({length,hasMore,intervalMs,autoPlay?,onLoadMore?})` = řízení promítání: vlastní
  `index`+`playing`, `next`/`prev`/`play`/`pause`/`toggle`/`goTo`, auto-advance na interval
  (setTimeout, manuální nav resetuje odpočet), wrap-around, prefetch `PRELOAD_AHEAD` snímků dopředu
  přes `onLoadMore` (na konci s další stránkou počká místo zacyklení), prázdná sada = no-op, clamp
  indexu při zmenšení sady; `useSlideshowSettings` = persistentní efekt+rychlost přes
  `lib/slideshowSettings` (read once on mount, setteri zapisují do localStorage, sanitizace))),
  `lib/` (`urlState.ts` = hook `useUrlState` +
  pure `readUrlState`/`writeUrlState`: stav pohledu ↔ URL query přes History API, „Zpět vždy
  funguje"; `libraryView.ts` = typ `LibraryView` (vč. `min_rating`/`flag`) + `LIBRARY_DEFAULTS` +
  `viewToParams` (sanitizuje sort/archived, prosákne `min_rating`/`flag`; `sort` union navíc
  `rating`) + `hasActiveFilters` (`{ignoreQuery}` na search stránce, zahrnuje rating/flag) —
  mapování URL stavu na API params; `ratingHotkeys.ts` = pure `ratingHotkey(key)` (`0`–`5` →
  rating, `p`/`r` → pick/reject, jinak null) + `isTypingElement(target)` (input/textarea/select/
  contenteditable → hotkey se přeskočí) — sdíleno detailem fotky i fokusnutou dlaždicí;
  `shortcuts.ts` = registr klávesových zkratek + pure helpery: `shortcutToken(key)` (normalizace
  `KeyboardEvent.key` — single-char lower-case, named keys passthrough, `?` zůstává), `isFormModalOpen`
  (je otevřený `.modal.show` s form controlem? → suppress zkratek za dialogem), `HELP_SHORTCUT_KEY`
  (`?`) a `SHORTCUT_GROUPS` (grouped Grid/Detail zdroj pravdy pro nápovědu, `titleKey`/`descriptionKey`
  typované jako i18next `ParseKeys`, takže neexistující klíč je compile error);
  `searchView.ts` = typ `SearchView` (= `LibraryView` + `mode`)
  + `SEARCH_DEFAULTS` (mode `hybrid`) + `toMode` sanitizér;
  `savedSearchView.ts` = pure `isSearchParams(params)` (přítomnost `mode` rozlišuje search od library
  pohledu) + `savedSearchHref(params)` (složí `pathname?query` na `/library` nebo `/search`, minimálně
  zakóduje uložené params proti defaultům přes `writeUrlState`, ignoruje neznámé/zastaralé klíče) —
  obnova uloženého hledání na přesnou URL;
  `mapView.ts` = typ `MapView` (mapset + viewport `lat`/`lng`/`z` + filtry) + `MAP_DEFAULTS` +
  `mapViewToParams` (sanitizuje archived) + `viewportFromView`/`mapsetFromView`/`hasActiveMapFilters`
  — mapování URL stavu mapy na feed params; `mapPopup.ts` = pure `buildPopupElement` (náhled +
  odkaz na detail fotky jako popup element, plain klik → SPA navigace, modifikovaný klik projde);
  `faceGeometry.ts` = pure `faceBoxStyle` (normalized bbox → absolutní `left/top/width/height`
  v %, pro overlay) + `faceCropStyle` (čtvercový výřez obličeje z thumbnailu přes
  background-position/-size, pro `FaceThumb`);
  `slideshowSettings.ts` = typ `SlideshowSettings{effect,intervalMs}` + `SlideshowEffect`
  (`fade`/`slide`/`none`) + nabídky `SLIDESHOW_EFFECTS`/`SLIDESHOW_INTERVALS_MS` + `SLIDESHOW_DEFAULTS`
  + pure `readSettings`/`writeSettings`/`sanitizeSettings` (localStorage `kukatko.slideshow.settings`,
  sanitizace efektu + clamp intervalu, fallback na defaulty při chybě/nedostupném storage);
  `slideshowView.ts` = pure `slideshowHref(scope,view)` (staví `/slideshow?…` z `LibraryView` přes
  `writeUrlState` + scope `album`/`label`, default filtry vynechá — launch link promítání);
  `trashCountdown.ts` = pure `purgeCountdown(archivedAt,retentionDays,now?)` (zbývající dny do
  auto-purge z `archived_at` + retence → `{daysLeft,due}` nebo `null` když odpočet neplatí
  (nearchivovaná / retence ≤ 0 / neparsovatelné), odpočet na kartách koše);
  `format.ts` = pure `formatBytes(bytes)` (byte count → human-readable binární jednotky, např.
  `1536`→`"1.5 KB"`, neplatné→`"0 B"`) pro velikost souboru na duplicate-group kartách +
  `formatDuration(ms)` (ms → `M:SS`/`H:MM:SS`, neplatné→`"0:00"`) pro délku videa na dlaždicích +
  `formatMonth(year,month,locale)` (1-based rok/měsíc → locale-aware krátký měsíc + rok, např.
  `2026,1,'en'`→`"Jan 2026"`, mimo 1–12 → `""`) pro popisky ticků časové osy +
  **locale-aware** `formatDate(value,locale)`/`formatDateTime(value,locale)` (ISO/epoch/`Date` →
  `toLocaleDateString`/`toLocaleString` s **aktivním jazykem UI** `i18n.language`, ne výchozím
  jazykem prohlížeče; neparseovatelný vstup → původní string; používá PhotoTile/DuplicateGroupCard/
  MetadataPanel/Import/System pro datumy v cs/en formátu))),
  `services/` (`health.ts`, `auth.ts` = login/logout/me/changePassword, typy
  `User`/`Role`/`AuthSession`, `ApiError` se statusem, `canWrite`/`roleAtLeast`,
  `MIN_PASSWORD_LENGTH`; `photos.ts` = `fetchPhotos(params,signal)` nad `GET /api/v1/photos`
  (filtry/řazení/stránkování → `PhotoListResponse{photos,total,limit,offset,next_offset}`),
  `searchPhotos(params,mode?,signal)` nad `GET /api/v1/search` (mód
  `fulltext`/`semantic`/`hybrid`, odpověď navíc `mode`+`degraded`),
  `fetchSimilar(uid,limit?,signal)` nad `GET /api/v1/photos/{uid}/similar` → `SimilarPhoto[]`
  (`Photo`+`distance`; empty-friendly), typy `SimilarPhoto`/`SimilarResponse`,
  `fetchTimeline(params,signal)` nad `GET /api/v1/photos/timeline` → `Timeline{buckets,total}`
  (měsíční date-histogram, stejné filtry jako list; sort/stránkování backend ignoruje), typy
  `Timeline`/`TimelineBucket{year,month,count,cumulative}` — podklad `TimelineScrubber`,
  `favoritePhoto(uid,favorite,signal)` nad `PUT`/`DELETE /api/v1/photos/{uid}/favorite` (per-user
  toggle, 204, podklad optimistického `useFavorite`),
  `ratePhoto(uid,{rating?,flag?},signal)` nad `PUT /api/v1/photos/{uid}/rating` +
  `clearRating(uid,signal)` nad `DELETE …/rating` (per-user hvězdy 0–5 + pick/reject flag, 204,
  podklad `useRating`), typy `RatingUpdate`/`RatingFlag`,
  **koš** `unarchivePhoto(uid)` (`POST …/unarchive` obnova), `purgePhoto(uid)` (`POST …/purge?confirm=true`
  trvalé mazání), `emptyTrash()` (`POST /trash/empty?confirm=true` → `PurgeResult{purged,failed}`),
  `fetchTrashInfo()` (`GET /trash/info` → `TrashInfo{retention_days}`),
  `buildPhotoQuery`, `thumbUrl(uid,size,token?)`, `videoUrl(uid,token?)` (range stream pro
  `<video>`), `GRID_THUMB_SIZE`, typy `Photo` (vč. `is_favorite` + per-user `rating`/`flag` + video pole
  `duration_ms`/`video_codec`/`audio_codec`/`has_audio`/`fps`)/`PhotoListParams`
  (vč. `album`/`label` scope + **`country`/`city` place scope** + `favorite` filtr + `min_rating`/`flag` filtry)/`PhotoSort`
  (vč. `rating`)/`RatingFlag`/`ArchivedFilter`/`SearchMode`, `ApiError`;
  `organize.ts` = Albums/Labels klient: alba `fetchAlbums`/`fetchAlbum`/`createAlbum`/`updateAlbum`/
  `deleteAlbum`/`addAlbumPhotos`/`removeAlbumPhotos`/`reorderAlbumPhotos`, štítky `fetchLabels`/
  `fetchLabel`/`createLabel`/`updateLabel`/`deleteLabel`/`attachLabel`/`detachLabel`; typy
  `Album`/`AlbumCount`/`AlbumInput`/`AlbumType`/`Label`/`LabelCount`/`LabelInput`;
  `savedSearches.ts` = uložená hledání klient: `fetchSavedSearches`/`createSavedSearch(name,params)`/
  `updateSavedSearch(uid,{name?,params?})`/`deleteSavedSearch(uid)` nad `/api/v1/saved-searches`, typy
  `SavedSearch`/`SavedSearchParams` (= verbatim URL view-state `Record<string,string>`)/
  `SavedSearchUpdate`; `search.ts` = grouped **global search** klient: `globalSearch(q,signal)` nad
  `GET /api/v1/search/global` → `GlobalSearchResult{query,albums,labels,people,photos}` (top-N per
  skupina, každá vždy pole) + pure helpery `hasEntityMatches`/`isEmptyResult`, typy
  `GlobalSearchAlbum`/`GlobalSearchLabel`/`GlobalSearchPerson`/`GlobalSearchResult`; oddělené od
  photo `searchPhotos` (fulltext/semantic/hybrid), podklad navbar quick-results i
  `GlobalSearchSections`; `bulk.ts` =
  `bulkUpdatePhotos(uids,ops)` nad `POST /photos/bulk` (hromadná úprava výběru), typy
  `BulkOperations` (add/remove alba+štítku, set/clear caption+popisu+polohy, set_private,
  archive/unarchive, set_favorite per-user)/`BulkLocation`/`BulkResult`; `duplicates.ts` =
  `fetchDuplicates(params,signal)` nad `GET /api/v1/duplicates` (skupiny duplikátů →
  `DuplicatesResponse{groups,total,limit,offset,next_offset}`), typy `DuplicateReason`/
  `DuplicateMember`/`DuplicateGroup`/`DuplicatesParams`; úklid jde přes `bulk.ts`
  `bulkUpdatePhotos`; `upload.ts` =
  `uploadFile(file,{onProgress,signal})`
  nad **`XMLHttpRequest`** (jeden soubor/request kvůli upload-progress eventům, FormData se
  streamuje), `isAbortError`, typy `UploadFileResult`/`UploadResponse`/`UploadWarning`/
  `UploadOutcome`; `photos.ts` navíc `fetchPhoto(uid)` (detail `GET /photos/{uid}` →
  `PhotoDetail` = `Photo`+`files`+`albums`+`labels` inline chipy), `updatePhoto(uid,patch)`
  (`PATCH …` částečná editace metadat → `PhotoMetadataUpdate`, null maže nullable),
  `fetchEdit(uid)`/`saveEdit(uid,edit)` (`GET`/`PUT …/edit` nedestruktivní edit → `PhotoEdit`
  crop/rotation/brightness/contrast), `downloadUrl(uid,{original?,token?})` (URL downloadu,
  default honoruje edit, `original:true` pro originál); typy `PhotoDetail`/`PhotoAlbumRef`/
  `PhotoLabelRef`/`PhotoMetadataUpdate`/`PhotoEdit`; `people.ts` = People/face klient: subjekty
  `fetchSubjects`/`fetchSubject`/`createSubject`/`updateSubject`/`deleteSubject`/
  `fetchSubjectPhotos`, obličeje `fetchFaces`/`assignFace`, shluky `fetchClusters`/
  `assignCluster`/`removeClusterFace`, outlier `fetchOutliers`; typy `Subject`/`SubjectCount`/
  `SubjectInput`/`SubjectType`/`Bbox`/`FaceView`/`FacesResponse`/`AssignRequest`/`Suggestion`/
  `ClusterView`/`ExampleFace`/`ClusterAssignRequest`/`RemoveFaceRequest`/`OutlierResult`/
  `OutlierFace`; sdílí `ApiError`+`buildPhotoQuery` z `auth.ts`/`photos.ts`);
  `map.ts` = mapový klient: `fetchMapPhotos(params,signal)` nad `GET /api/v1/map/photos`
  (GeoJSON FeatureCollection geotagovaných fotek + `buildMapQuery`), `tileLayerUrl(mapset)` (Leaflet
  URL template na backend proxy, **bez API klíče**), `reverseGeocode(lat,lng,signal?)` nad
  `GET /api/v1/map/rgeocode` (on-demand reverse geocode pro detail fotky → `GeocodeResult`),
  `toMapset`/`MAPSETS`; typy
  `MapFeature`/`MapFeatureCollection`/`MapFeatureProperties`/`MapPhotoParams`/`Mapset`/
  `GeocodeResult`/`RegionalItem`);
  `places.ts` = klient hierarchie míst: `fetchPlaces(country?,signal)` nad `GET /api/v1/places`
  → `PlaceCountry[]` (země s počty + nested `cities`, volitelné `country` drillne do měst jedné
  země); typy `PlaceCountry`/`PlaceCity`; procházení fotek lokality jde přes sdílené
  `fetchPhotos({country,city})`;
  `import.ts` = admin import klient: `fetchImportRuns(signal)` nad `GET /api/v1/import/runs`
  (`{runs,limit,offset,sources}`), `fetchJobStats(signal)` nad `GET /api/v1/jobs/stats`,
  `startImport(source,signal)` nad `POST /api/v1/import/{photoprism|photosorter}` (409 → ApiError);
  typy `ImportSource`/`RunStatus`/`ImportCounts`/`ImportRun`/`ImportSources`/`ImportRunsResponse`/
  `StartImportResult`/`JobStats`),
  `maintenance.ts` = admin maintenance klient: `fetchMaintenanceScan(signal)` nad
  `GET /api/v1/maintenance/scan` → `ScanReport`, `runMaintenanceRepair(options,signal)` nad
  `POST /api/v1/maintenance/repair` → `RepairResult`; typy `Finding`/`ScanReport`/`RepairOptions`/
  `RepairResult`; sdílí `ApiError` z `auth.ts` a `fetchJobStats` z `import.ts` pro progress,
  `system.ts` = admin system-status klient: `fetchSystemStatus(signal)` nad `GET /api/v1/system/status`
  → `SystemStatus`, `triggerBackup(signal)` nad `POST /api/v1/backup` (409/503 → ApiError),
  `requeueDeadLetterJobs(signal)` (vylistuje `GET /jobs?state=dead` → per-job `POST /jobs/{id}/requeue`,
  vrací počet, 404/409 skip); typy `SystemStatus`/`DatabaseStatus`/`EmbeddingsStatus`/`JobsStatus`/
  `BackupStatus`/`ImportsStatus`/`StorageStatus`/`VersionInfo`; sdílí `ApiError` z `auth.ts` a `ImportRun`
  z `import.ts`,
  `i18n/` (i18next init + `locales/{cs,en}/common.json`;
  typované klíče přes `types/i18next.d.ts` — nové stringy přidávej do **obou** locale souborů;
  **čeština default**, žádné natvrdo zapsané UI texty — vše přes `t()`. **Pluralizace** přes
  i18next CLDR plural sufixy: count-vázané řetězce kde se podstatné jméno shoduje s číslem mají
  formy `key_one/_few/_many/_other` (čeština) a `key_one/_other` (angličtina) — caller jen předá
  `{ count }` (např. `albums.photoCount`, `clusters.size`, `bulkEdit.title`, `duplicates.memberCount`/
  `archived`, `trash.confirm.bulk`); label-tvary s dvojtečkou/závorkou (`library.count`, `selection.count`)
  zůstávají bez plurálu. **Datumy/čísla respektují jazyk** přes `lib/format` `formatDate`/`formatDateTime`
  (`i18n.language`). **Drift-guard testy** `i18n.test.ts` (cs/en mají identické *logické* klíče po
  odstranění plural sufixu, žádné prázdné hodnoty, každý jazyk má všechny své CLDR plural kategorie,
  interpolační `{{var}}` proměnné se shodují napříč jazyky) + `screens.test.tsx` (reprezentativní
  obrazovky — navbar + dlaždice — se vykreslí bez missing-key warningů v cs i en přes
  `cloneInstance({saveMissing})`, plural rendering 1/3/5, language-switch přepíše viditelný text)),
  `styles/app.css` (**global responzivní polish vrstva** importovaná v `main.tsx` hned za
  Bootswatch CSS — jen cross-cutting mobil/touch věci, které Bootstrap utility neumí: **safe-area
  insety** přes `env(safe-area-inset-*)` (fungují díky `viewport-fit=cover` v `index.html`) na
  navbaru (`.kukatko-navbar`) a hlavním kontejneru (`.kukatko-main`); guard proti vodorovnému
  scrollu/overscroll bounce (`body overflow-x:hidden`, `html overscroll-behavior-y:none`); sdílený
  **sticky-toolbar offset** `.kukatko-sticky-toolbar` (`top: navbar height + safe-area-inset-top`,
  z-index pod navbarem — in-page sticky bary jako `SelectionBar` dosednou pod navbar, ne pod něj);
  **min. tap-target** `.kukatko-tap-target` (2.75rem/44px) pro icon-only ovládání jako
  `FavoriteButton`; **časová osa** `.kukatko-timeline*` (fixní svislá datová lišta u pravého
  okraje pod navbarem, absolutně umístěné ticky, floating popisek aktivního měsíce, `touch-action:
  none` pro tažení, na šířkách ≤ 575.98px skrytá); CSS proměnná `--kukatko-navbar-height`),
  `test/setup.ts` (jsdom **`window.matchMedia` stub** — non-matching default, jednotlivé testy ho
  můžou přepsat pro simulaci telefonu).
  Routing v `App.tsx`: `/login` veřejné, zbytek pod `RequireAuth`; `/slideshow` je pod
  `RequireAuth` ale **mimo `Layout`** (fullscreen bez navbaru), zbytek pod `Layout` (`/`, `/library`,
  `/favorites`, `/albums`, `/albums/:uid`, `/labels`, `/labels/:uid`, `/search`, `/saved`, `/map`,
  `/places`, `/photos/:uid`, `/people`,
  `/people/:uid`, `/account`; `/upload`, `/people/clusters`, `/trash` a `/duplicates`
  navíc pod `RequireRole role="editor"` = write-only, `/import`, `/maintenance` a `/system` pod
  `RequireRole role="admin"` = admin-only). Konfig:
  `vite.config.ts` (build → `../internal/web/static/dist`, vitest jsdom, dev proxy
  `/healthz`+`/api` → `:8080`), `eslint.config.js` (strict typed), `.prettierrc.json`,
  `tsconfig*.json`.
- **CLI:** `kukatko serve` (načte config, **spustí migrace**, **bootstrapne admina**, spustí
  hodinové čištění expirovaných session, **background worker** (`internal/worker`) na
  zpracování fronty jobů a **plánovaný úklid koše** (`internal/trash` `RunPurge`, každých 6 h —
  trvale maže fotky archivované déle než `trash.retention_days`; retence ≤ 0 ho vypne),
  **plánovanou S3 zálohu** (`internal/backup` `RunSchedule` na `backup.schedule`; jen je-li
  `backup.s3.{endpoint,bucket}` nakonfigurováno) a **volitelný Wake-on-LAN auto-wake boxu**
  (`internal/wake` `Run`, každou minutu; jen je-li `embedding.wake.enabled`, jinak plně inertní),
  pak poslouchá na `web.host:web.port`, default
  `0.0.0.0:8080`; `GET /healthz` → 200 JSON `{"status":"ok","version":{…}}`, **`GET /metrics`**
  Prometheus (mimo `/api/v1`, bez autentizace; jen když `metrics.enabled`), auth/admin API
  pod `/api/v1` — viz níže, ostatní cesty servíruje **embedované SPA** s fallbackem na
  `index.html`; `serve` navíc nastaví **strukturované logování** (`obs.Setup`, JSON slog na
  stderr, level `log.level`) a — když `metrics.enabled` — postaví `metrics.Registry`, zaregistruje
  DB-pool + job-queue-depth kolektory a vloží request-metriky + access-log middleware přes
  `server.WithMiddleware`/`WithMetricsHandler`), `kukatko migrate` (spustí pending migrace samostatně a skončí),
  `kukatko migrate photosorter` (synchronní read-only inkrementální **migrace dat z photo-sorteru** —
  `psimport`; aplikuje DB migrace, pak `Service.Migrate`; potřebuje `import.photosorter.dsn`, jinak
  `errPSMigrateNotConfigured`; pro ops/cron bez běžícího serveru),
  `kukatko import photoprism` (synchronní read-only inkrementální import z PhotoPrismu — `ppimport`;
  potřebuje `import.photoprism.base_url`, jinak chyba; pro ops/cron bez běžícího serveru),
  `kukatko backup` (synchronní jednorázová **S3 záloha** — `internal/backup`; pg_dump + sync
  originálů + retence; potřebuje `backup.s3.{endpoint,bucket}`, jinak `errBackupNotConfigured`;
  pro ops/cron bez běžícího serveru),
  **`kukatko restore`** (strom obnovy/disaster recovery — `internal/backup`; sdílí `backup.s3.*`,
  jinak `errRestoreNotConfigured`; pro ops/cron bez běžícího serveru): `restore list` (dumpy v
  bucketu), `restore db [--dump KEY] [--yes] [--verify]` (**destruktivní** obnova DB přes
  `pg_restore` streamovaný z S3 + idempotentní re-migrace; bez `--yes` → `errRestoreNotConfirmed`),
  `restore originals` (stáhne chybějící originály, skip dle klíče+velikosti, resumovatelné),
  `restore verify` (integritní report fotek v DB vs originálů na disku); runbook
  [`docs/RESTORE.md`](docs/RESTORE.md),
  **`kukatko maintenance`** (integritní kontrola & opravy knihovny — `internal/maintenance`; pro
  ops/cron bez běžícího serveru, aplikuje migrace a postaví službu sdílenou s admin API):
  `maintenance scan` (read-only integritní report — disk↔DB drift + chybějící odvozená data) a
  `maintenance repair` s flagy `--thumbnails`/`--embeddings`/`--faces`/`--phashes`/`--import-orphans`
  (každá opt-in; thumbnails/phashes zařadí `thumbnail` joby drainované workerem běžícího serveru,
  embeddings/faces backfill, orphan import synchronně přes upload pipeline; bez flagu no-op),
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
  filtr `?album={uid}`/`?label={uid}` scopne výpis na fotky alba/štítku (sdílený endpoint pro
  galerii alba i štítku, ctí všechny ostatní filtry/řazení/stránkování — viz Albums & Labels API);
  `GET /photos/timeline` (přihlášený) — **měsíční date-histogram** knihovny (podklad rok/měsíc
  scrubberu): přijímá **stejné filtry** jako `GET /photos` přes `parseListParams`, odpověď
  `{buckets:[{year,month,count,cumulative}],total}`, buckety řazené nejnovější první (dle `taken_at`,
  jako výchozí mřížka), `cumulative` = počet fotek **před** bucketem (mapuje bucket na scroll-index),
  `total` (přes `Count`) zahrnuje i fotky bez data pořízení (do žádného bucketu nespadají, řadí se
  na konec); `sort`/`order` se ignorují (vždy grupováno dle data), backuje ho
  `photos.Store.TimelineBuckets` (sdílí `buildWhere` s `List`/`Count`), neplatný param → 400;
  `GET /search?q=&mode=` (přihlášený) — **sémantické + hybridní hledání**, `mode` =
  `fulltext`|`semantic`|`hybrid` (default `hybrid`, neznámý → 400): **fulltext** = česky-aware
  fulltext nad `fts tsvector` (dictionary `simple` + `unaccent`, řazení `ts_rank`
  title>description>notes>file_name); **semantic** = `q` → CLIP embedding přes sidecar →
  cosine HNSW nad `embeddings`, řazení dle podobnosti; **hybrid** = fúze obou přes
  **Reciprocal Rank Fusion (k=60)**, dedup. Všechny módy ctí ostatní list filtry + stránkování,
  odpověď jako list + `mode` + `degraded`; `q` povinný (prázdný → 400); **box offline** →
  `semantic`/`hybrid` graceful fallback na fulltext s `degraded: true`;
  list i search nesou per-fotku `is_favorite` **+ per-user `rating`/`flag`** pro aktuálního uživatele,
  `?favorite=true` scopne list na jeho oblíbené, **`?min_rating=n` / `?flag=pick|reject` / `?sort=rating`**
  scopnuté na něj (fotka bez řádku = rating 0 / flag `none`);
  `GET /photos/{uid}` plný detail + `files` + `is_favorite` + `rating`/`flag`;
  **per-user oblíbené** `PUT`/`DELETE /photos/{uid}/favorite` (každý přihlášený, idempotentní → 204,
  404 chybějící fotka, 503 bez backendu) + `GET /favorites` (oblíbené aktuálního uživatele ve tvaru
  list endpointu, filtry/řazení/stránkování jako `/photos`);
  **per-user hodnocení** `PUT /photos/{uid}/rating` `{rating?:0..5, flag?:none|pick|reject}` (každý
  přihlášený, aspoň jedna hodnota → 204, 400 neplatná, 404 chybějící fotka, 503 bez backendu) +
  `DELETE /photos/{uid}/rating` (idempotentní clear → 204); `GET /photos/{uid}/faces` (přihlášený) — obličeje
  fotky s bboxem, přiřazením (marker/subjekt), akcí (`create_marker`/`assign_person`/`already_done`)
  a **návrhy** identit pro nepojmenované (face↔marker IoU matching, viz `internal/facematch`; 503
  když face backend není zapojen); `POST /photos/{uid}/faces/assign` (editor/admin) — přiřazovací
  akce `{action, face_index?, marker_uid?, subject_uid?, subject_name?, bbox?}`
  (`create_marker`/`assign_person`/`unassign_person`), auto-create subjektu dle jména, drží `faces`
  cache + `marker.reviewed` konzistentní (400 validace, 404 chybějící foto/marker/subjekt);
  `GET /photos/{uid}` plný detail navíc nese **členství** `albums`/`labels` (inline chipy detailu,
  přes `PhotoOrganizer` rozhraní / `organize.Store.AlbumsForPhoto`+`LabelsForPhoto`; nil organizer →
  prázdná pole); **nedestruktivní edit** (`internal/photoedit` + `edit.go`/`media_edit.go`):
  `GET /photos/{uid}/edit` (přihlášený) → uložený `photos.Edit` (crop/rotace 0-90-180-270/jas/kontrast,
  neupravená fotka → neutrální edit) a `PUT /photos/{uid}/edit` (editor/admin) zapíše edit do
  `photo_edits` (validace bounds; originál se nikdy nemění — `GET …/download` ho **renderuje za běhu**
  přes `photoedit.Apply`, pokud caller nedá `?original=true`);
  `PATCH /photos/{uid}` (editor/admin) částečná úprava
  metadat (null maže nullable, validace souřadnic); `POST /photos/{uid}/archive`+`/unarchive`
  (editor/admin) soft-delete přes `archived_at` (archivované mimo výchozí list);
  **koš / trvalé mazání** (`trash.go`, backuje `internal/trash` přes rozhraní `Purger`, nil → 503):
  `POST /photos/{uid}/purge` (editor/admin, `?confirm=true` jinak 400, 404 chybí, 409 fotka není
  archivovaná → 204) a `POST /trash/empty` (editor/admin, `?confirm=true` → `{purged,failed}`)
  trvale mažou archivované fotky, `GET /trash/info` (přihlášený) vrací `{retention_days}` pro odpočet
  do auto-purge; seznam koše jede přes sdílené `GET /photos?archived=only`;
  `GET /photos/{uid}/thumb/{size}` a `/download` (session/`?t=` token) **streamují** média
  (`Cache-Control`/`ETag`/`304`); `GET /photos/{uid}/video` (session/`?t=` token) streamuje video
  **s HTTP Range** (206 partial, `Accept-Ranges`, seek; live fotka = motion klip, still → 404) pro
  inline HTML5 přehrávání, volitelný on-the-fly transcode neweb-friendly codeců přes
  `video.transcode` config (default off). Mountuje se třetím `server.WithAPI` (`buildPhotoAPI` v
  `cmd/kukatko/photos.go`).
- **Jobs API (`/api/v1`, `internal/jobsapi`, admin-only přes `RequireAdmin`):**
  `GET /jobs/stats` → `{by_state,by_type,total}`; `GET /jobs` → `{jobs,limit,offset}`
  (recent/dead-letter výpis, query `state`/`limit`/`offset`, neplatný → 400);
  `POST /jobs/{id}/requeue` → refreshnutý job (dead/failed → queued; 404 missing, 409
  ne-requeueable). Frontend polluje (žádné SSE). Mountuje se šestým `server.WithAPI`
  (`buildJobs` v `cmd/kukatko/jobs.go`), který registruje handlery `image_embed`
  (`embedjob.Service`), `face_detect` (`facejob.Service`) a — když je mapy.com klíč nastaven —
  `places` (`placesjob.Service`, `buildPlacesServiceOrNil` v `cmd/kukatko/places.go`) a zároveň
  postaví a `serve` spustí **background worker** (`internal/worker`) na celý život procesu
  (`startWorker`, zastaví se na shutdownu přes ctx).
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
- **People/Subjects API (`/api/v1`, `internal/peopleapi`):** `GET /subjects` (RequireAuth) →
  `{subjects:[{...subject, marker_count}]}` (řazení dle jména, počty non-invalid markerů);
  `POST /subjects` (RequireWrite) → 201 vytvoří subjekt z `{name,type,favorite,private,notes,
  cover_photo_uid?}` (prázdné jméno / neznámý typ → 400); `GET /subjects/{uid}` (RequireAuth) →
  subjekt (404); `PATCH /subjects/{uid}` (RequireWrite) → editace stejných polí (404/400);
  `DELETE /subjects/{uid}` (RequireWrite) → 204 (markery se odpojí server-side); `GET
  /subjects/{uid}/photos` (RequireAuth) → paginovaná galerie fotek subjektu
  `{photos,total,limit,offset,next_offset}` (newest-first, jen nearchivované, `limit`≤500). Mountuje
  se osmým `server.WithAPI` (`buildPeopleAPI` v `cmd/kukatko/people.go`). Záznamy fotek subjektu
  staví na `people.Store.ListPhotoUIDsBySubject` (distinct non-invalid markery → photo uid).
- **Process API (`/api/v1`, `internal/processapi`, admin-only přes `RequireAdmin`):**
  `POST /process/embeddings` → `{enqueued}` (backfill `image_embed` pro fotky bez embeddingu),
  `POST /process/faces` → `{enqueued}` (backfill `face_detect` pro fotky bez detekce obličejů),
  `POST /process/clusters` → `{created}` (re-clustering nepřiřazených obličejů přes
  `cluster.Recluster`), `POST /process/places` → `{enqueued}` (backfill `places` reverse-geokódu pro
  geotagované fotky bez místa přes `placesjob.BackfillPlaces`; 503 když není mapy.com klíč). Mountuje
  se sedmým `server.WithAPI` (`buildJobs`).
- **Albums & Labels API (`/api/v1`, `internal/organizeapi`):** **alba** `GET /albums`
  (RequireAuth) → `{albums:[{...album, photo_count}]}` (počty + cover); `POST /albums`
  (RequireWrite) → 201 z `{title,description?,type?,cover_photo_uid?,private?,order_by?}` (prázdný
  title / neplatný typ → 400); `GET /albums/{uid}` (RequireAuth, 404); `PATCH /albums/{uid}`
  (RequireWrite) edituje title/description/cover_photo_uid/private/order_by (**`type` se zachová**,
  není editovatelný); `DELETE /albums/{uid}` (RequireWrite → 204); členství
  `POST /albums/{uid}/photos` `{photo_uids:[…]}` (přidá za stávající), `DELETE /albums/{uid}/photos`
  `{photo_uids:[…]}` (odebere), `PATCH /albums/{uid}/order` `{photo_uids:[…]}` (přeřadí) — všechny
  vrací aktuální pořadí `{photo_uids:[…]}`, 404 chybějící album/fotka. **Štítky** `GET /labels`
  (RequireAuth) → `{labels:[{...label, photo_count}]}` (řazení priority DESC); `POST /labels`
  (RequireWrite) → 201 z `{name,priority?}` (prázdné jméno → 400); `GET /labels/{uid}`
  (RequireAuth, 404); `PATCH /labels/{uid}` (RequireWrite, name/priority); `DELETE /labels/{uid}`
  (RequireWrite → 204); připojení `POST /labels/{uid}/photos` `{photo_uid,source?,uncertainty?}`
  → 204 (neplatný source → 400), `DELETE /labels/{uid}/photos` `{photo_uid}` → 204. **Galerie
  fotek alba/štítku** jede přes sdílené `GET /photos?album={uid}`/`?label={uid}` (stejný tvar +
  filtry/řazení/stránkování). Viewer čte, ale nemutuje (403). Mountuje se dalším `server.WithAPI`
  (`buildOrganizeAPI` v `cmd/kukatko/organize.go`).
- **Places API (`/api/v1`, `internal/placesapi`, přihlášený přes `RequireAuth`):** procházení
  reverse-geokódované place hierarchie + scoping výpisu fotek na lokalitu. `GET /places` →
  `{places:[{country, count, cities:[{city, count}]}]}` — počty agregované přes **nearchivované**
  fotky s place daty; `count` země zahrnuje i fotky bez známého města (může převýšit součet měst),
  `cities` je vždy pole; řazení **count desc, pak jméno** (pro země i města); fotky bez place dat
  (žádný `photo_places` řádek nebo prázdný `country` — no-GPS „processed" marker) vyloučené.
  Volitelné `?country=` drillne jen do měst jedné země. Agregaci počítá `photos.Store.AggregatePlaces`
  (jeden `GROUP BY country, city` JOIN na `photo_places`, hierarchii složí v Go). **Galerie fotek
  lokality** jede přes sdílené `GET /photos?country={c}&city={c}` (`Country`/`City` exact match přes
  korelovaný `EXISTS` nad `photo_places` v `buildWhere`, takže `Count` sedí; stejný tvar + ostatní
  filtry/řazení/stránkování, archivní mimo výchozí výpis). Mountuje se `server.WithAPI`
  (`buildPlacesAPI` v `cmd/kukatko/places.go`).
- **Saved Searches API (`/api/v1`, `internal/savedsearchapi` + `internal/savedsearch`, přihlášený přes
  `RequireAuth`):** per-user **uložená hledání** ("smart albums") — pojmenovaná, vlastníkova soukromá
  definice filtru/hledání. `GET /saved-searches` → `{saved_searches:[{uid,name,params,created_at,
  updated_at}]}` (jen aktuálního uživatele, newest-first); `POST /saved-searches` `{name,params}` →
  201 (prázdné jméno → 400, `params` JSONB volitelné → `{}`); `GET /saved-searches/{uid}` → 200;
  `PATCH /saved-searches/{uid}` `{name?,params?}` → 200 (vynechané pole beze změny); `DELETE
  /saved-searches/{uid}` → 204. **Každá operace je scopnutá na přihlášeného uživatele** z auth
  kontextu — uložené hledání cizího vlastníka se **vždy hlásí jako 404** (nikdy se neprozradí), tělo
  `DisallowUnknownFields` + 1 MiB limit. Tabulka `saved_searches` (migrace `0017_saved_searches.sql`).
  Mountuje se `server.WithAPI` (`buildSavedSearchAPI` v `cmd/kukatko/savedsearch.go`).
- **Global Search API (`/api/v1`, `internal/globalsearchapi`, přihlášený přes `RequireAuth`):**
  grouped **cross-entity search** pro navbar quick-results a search stránku. `GET /search/global?q=` →
  `{query, albums:[{uid,title,cover,photo_count}], labels:[{uid,name,photo_count}],
  people:[{uid,name,cover}], photos:[…usual photo shape…]}` — alba/štítky/osoby matchované dle
  name/description **accent- a case-insensitive** (`immutable_unaccent` + ILIKE přes store metody
  `SearchAlbums`/`SearchLabels`/`SearchSubjects`), fotky přes **existující fulltext** (`photos.Store.
  Search` nad `fts` tsvector). Každá skupina je capnutá na malé top-N (default 8, `Config.Limit`), pole
  jsou vždy non-nil. Prázdný/whitespace `q` → 400, chyba store → 500. Existující `GET /search` (per-user
  photo fulltext/semantic/hybrid) zůstává beze změny. Mountuje se `server.WithAPI` (`buildGlobalSearchAPI`
  v `cmd/kukatko/globalsearch.go`, sdílí organize/people/photos store).
- **Bulk metadata API (`/api/v1`, `internal/bulkapi`, editor/admin přes `RequireWrite`):**
  `POST /photos/bulk` `{photo_uids:[…], operations:{…}}` aplikuje sadu operací na mnoho fotek
  **v jediné transakci** s audit-log záznamem. Operace (každá volitelná): `add_to_albums`/
  `remove_from_albums`, `add_labels`/`remove_labels`, `set_caption`/`clear_caption` (→title),
  `set_description`/`clear_description`, `set_location {lat,lng}`/`clear_location`, `set_private`,
  `archive`/`unarchive`, `set_favorite` (**per-user**), `set_rating` (0–5) / `set_flag`
  (none/pick/reject) (**per-user**, neplatná hodnota → 400). Odpověď `{results:[{photo_uid,status,
  error?}],counts:{total,updated,skipped,errored}}` (200 i při dílčích chybách): `updated`/
  `skipped` (duplicitní uid)/`error` (fotka neexistuje — **neabortuje validní**); jen DB chyba
  rollbackne celou dávku (500). Konflikt set/clear nebo archive/unarchive, neznámá operace,
  chybějící album/štítek v add → **400**; dávka nad `bulk.max_batch_size` (default 1000) → **413**.
  Mountuje se dalším `server.WithAPI` (`buildBulkAPI` v `cmd/kukatko/bulk.go`).
- **Maps API (`/api/v1`, `internal/mapsapi` + `internal/mapy`, přihlášený přes `RequireAuth`):**
  backendová proxy na mapy.com (**klíč nikdy do klienta** — jen hlavička `X-Mapy-Api-Key`) +
  GeoJSON feed. `GET /map/tiles/{mapset}/{z}/{x}/{y}` — proxy dlaždice, **streamuje** s dlouhým
  immutable `Cache-Control`; `mapset` allowlist `basic|outdoor|aerial|winter` (jiný → 400, ještě
  před voláním), retina `@2x` (sufix na `{y}` nebo `?retina=true`) jen pro `basic`/`outdoor`,
  neplatné `z`/`x`/`y` → 400. `GET /map/rgeocode?lat=&lng=` — reverse geocode → zjednodušené
  `{name,location,regional_structure}`, **cachované** (klíč = zaokrouhlená souřadnice) a
  **rate-limitované** (token-bucket, geocode = 4 kredity) → 429 přes limit, 404 bez shody.
  `GET /map/photos` — **GeoJSON FeatureCollection** geotagovaných fotek (souřadnice `[lng,lat]`),
  ctí filtry `taken_after`/`taken_before`/`album`/`label`/`archived`/`private`, feature nese
  `uid`/`title`/`taken_at`/`media_type`/relativní `thumb`. mapy.com chyby (401/403→502, 404→404,
  429→429, 5xx→502/503) **neprosakují klíč**; bez `maps.mapy_api_key` vrací tile/rgeocode 503,
  GeoJSON funguje. Mountuje se `server.WithAPI` (`buildMapsAPI` v `cmd/kukatko/maps.go`).
- **Import API (`/api/v1`, `internal/importapi`, admin-only přes `RequireAdmin`):** triggery a
  historie read-only importů. `GET /import/runs` (**vždy registrovaný**) → `{runs,limit,offset,
  sources:{photoprism,photosorter}}` — stránka `import_runs` newest-started-first (query
  `limit`≤200/`offset`, neplatný → 400) + `sources` flagy jaké zdroje jsou nakonfigurované (podklad
  admin Import UI: zapnutí/vypnutí sekcí). `POST /import/photoprism` → `pp_import` a
  `POST /import/photosorter` → `ps_migrate` (jen pro nakonfigurované zdroje, jinak 404) zařadí jeden
  singleton job → 202 `{job_id,status}`; `jobs.ErrDuplicate` (už běží) → 409, jiná chyba → 500.
  Celá API se mountuje vždy (`buildImportAPI` v `cmd/kukatko/import.go`), aby historie fungovala i
  bez konfigurovaného zdroje. Frontend (`ImportPage`) polluje `GET /import/runs` + `GET /jobs/stats`.
- **Backup API (`/api/v1`, `internal/backupapi`, admin-only přes `RequireAdmin`):** stav a trigger
  S3 zálohy. `GET /backup` → stav + poslední běh (`{configured,running,last_started_at,
  last_finished_at,last_error,last_result}`; bez konfigurace `configured:false`); `POST /backup`
  spustí zálohu na **pozadí** (`Trigger`) → 202 `{status:"started"}`, `backup.ErrAlreadyRunning` →
  409, bez konfigurace → 503. Celá API se mountuje **vždy** (`buildBackupAPI` v
  `cmd/kukatko/backup.go`); plánovač (`backup.schedule`) a CLI `kukatko backup` sdílí stejný
  `backup.Service`. Konfig klíče `backup.s3.{endpoint,region,bucket,access_key,secret_key,
  path_style}`, `backup.schedule` (cron), `backup.retention` (kolik posledních dumpů nechat; ≤ 0 =
  vše). Runtime dep `pg_dump` (`postgresql-client`). Tajemství (`access_key`/`secret_key`) přes env.
- **Restore API (`/api/v1`, `internal/restoreapi`, admin-only přes `RequireAdmin`):** **jen
  read-only** operace nad obnovou. `GET /restore/dumps` → `{dumps:[{key,size}]}` (dumpy v bucketu,
  nejnovější první; 503 bez konfigurace, 502 při chybě S3); `POST /restore/verify` → `VerifyReport`
  (fotky v DB vs originály na disku + nesoulady; 503 bez konfigurace). **Destruktivní obnova DB se
  přes HTTP záměrně neexponuje** (podtrhla by tabulky běžícímu serveru) — patří do CLI `kukatko
  restore db` při zastaveném serveru. Celá API se mountuje **vždy** (`buildRestoreAPI` v
  `cmd/kukatko/restore.go`; service nil = nenakonfigurováno). Runtime dep `pg_restore`
  (`postgresql-client`, stejný balík jako pg_dump). Runbook: `docs/RESTORE.md`.
- **Audit API (`/api/v1`, `internal/auditapi`, admin-only přes `RequireAdmin`):** read-only výpis
  durable audit trailu. `GET /audit` → `{entries,total,limit,offset,next_offset}` (entry =
  `{id,actor_uid,action,target_type,target_uid,details,ip,user_agent,created_at}`, newest-first)
  s filtry `?user=`/`?entity_type=`/`?entity_uid=`/`?action=`/`?since=`/`?until=` (RFC3339) a
  stránkováním `?limit=`(≤500)/`?offset=`; neplatný čas/číslo → 400. Audit záznamy se **nezapisují
  přes HTTP** — vznikají uvnitř mutačních transakcí (in-tx `audit.Write`, viz `internal/audit`
  konvence). Mountuje se vždy (`buildAuditAPI` v `cmd/kukatko/audit.go`).
- **Maintenance API (`/api/v1`, `internal/maintenanceapi`, admin-only přes `RequireAdmin`):**
  integritní kontrola & opravy knihovny. `GET /maintenance/scan` → `Report` (counts + vzorky:
  `missing_originals`/`orphan_files`/`missing_thumbnails`/`missing_embeddings`/`missing_faces`/
  `missing_phashes` + totály `photos`/`files_in_db`/`originals_on_disk`); `POST /maintenance/repair`
  `{thumbnails,embeddings,faces,phashes,import_orphans}` (každá opt-in) → `RepairResult` se scheduling
  počty (`*_enqueued` + `orphans_imported/skipped/failed`); `DisallowUnknownFields`, prázdný výběr →
  400, orphan import bez importéru → 503 (`ErrOrphanImportUnavailable`). Opravy jsou idempotentní a
  jedou přes frontu jobů (thumbnail/pHash přes `thumbnail` job, embeddingy/faces backfill), **nikdy
  nemažou originály**. Mountuje se vždy (`buildMaintenanceAPI` v `cmd/kukatko/maintenance.go`).
- **Duplicates API (`/api/v1`, `internal/duplicatesapi` + `internal/duplicates`, editor/admin přes
  `RequireWrite`):** `GET /duplicates?limit=&offset=` → `{groups,total,limit,offset,next_offset}`
  skupiny pravděpodobných duplikátů z pHash Hammingovy vzdálenosti (`duplicate.phash_max_diff`,
  banded-LSH) **a/nebo** embedding cosine vzdálenosti (`duplicate.embedding_max_dist`, HNSW), slité
  union-findem do souvislých komponent (žádný O(n²) sken). Každá skupina nese členy (náhled/rozměry/
  velikost/`taken_at`/vzdálenosti) + `reason` (phash/embedding/both) + navržený `keeper_uid`
  (nejvyšší rozlišení → největší → nejstarší → uid); řazení largest-first, `limit`≤100, neplatný →
  400, sken selže → 500. **Jen čte — nic nemaže.** Při `duplicate.enabled=false` route odpovídá 503.
  Úklid jede klient přes sdílené **bulk API** (`POST /photos/bulk` `{archive:true}` → koš, vratné).
  Mountuje se vždy (`buildDuplicatesAPI` v `cmd/kukatko/duplicates.go`).
- **System status API (`/api/v1`, `internal/systemapi` + `internal/system`, admin-only přes
  `RequireAdmin`):** `GET /system/status` → jeden agregovaný snapshot provozního zdraví:
  `{version,database{reachable,error?},embeddings{online,url},jobs{by_state,by_type,total,dead_letter,
  pending_embeddings},backup (=backup.Status),imports{photoprism,photosorter (=importer.Run|null)},
  storage{originals_bytes,cache_bytes,free_bytes,total_bytes}}`. Sloučení existujících subsystémů
  (embeddings health, fronta jobů, backup stav, poslední import per zdroj přes
  `importer.Store.LatestRun`, využití disku, DB ping); úložiště memoizováno 30 s. Collect selže (DB
  pro fronту/importy) → 500; nedostupná DB/úložiště inline best-effort. Mountuje se **vždy**
  (`buildSystemAPI` v `cmd/kukatko/system.go`). Admin UI **Systém** (`/system`, `SystemStatusPage`)
  polluje po 5 s a nabízí rychlé akce (requeue dead-letter, trigger backup, odkazy na import/údržbu).
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
- **Observability klíče:** `log.level` (debug/info/warn/error, default info, neplatný → chyba při
  startu; `KUKATKO_LOG_LEVEL`) a `metrics.enabled` (bool, default true; vypnuté → `/metrics` se
  nemountuje, request-metriky middleware se neinstaluje, access-log běží dál; `KUKATKO_METRICS_ENABLED`).
- **Thumbnail klíče (`thumb.*`, `internal/config`):** `engine` (`go` **default** / `vips`;
  neznámá hodnota → `ErrInvalidThumbEngine` při startu) — `vips` přepne JPEG/PNG/WebP náhledy na
  shell-out na `vipsthumbnail` (rychlejší/úspornější na velkých obrázcích, **stále bez CGO**),
  pure-Go zůstává default a per-foto fallback; `vips_binary` (executable na PATH, default
  `vipsthumbnail`, jen pro `vips`); `concurrency` (max velikostí enkódovaných paralelně na fotku,
  `0`=GOMAXPROCS — sniž na paměťově omezeném hostu). `KUKATKO_THUMB_ENGINE`/`_VIPS_BINARY`/
  `_CONCURRENCY`. `serve` loguje aktivní engine + varuje, když `vips` chybí na PATH. Viz `docs/PERF.md`.
- **Video klíče (`video.*`, `internal/config`):** `transcode` (bool, **default false**) — zapne
  on-the-fly transcode neweb-friendly codeců (HEVC/H.265 …) na H.264/MP4 přes ffmpeg pro přehrání
  v prohlížeči. Off = video se streamuje as-is (s HTTP Range) a klient nabídne stažení, když ho
  prohlížeč neumí dekódovat. Transcode je CPU-náročný, běží na každé přehrání (necachuje se) a
  transcodovaný stream nelze přesně seekovat — proto opt-in. `KUKATKO_VIDEO_TRANSCODE`.
- **Wake-on-LAN klíče (`embedding.wake.*`, `internal/wake`):** `enabled` (bool, **default false** —
  feature plně inertní), `mac` (MAC boxu, **povinný a parseovaný při validaci** když enabled),
  `broadcast_addr` (UDP broadcast cíl, default `255.255.255.255:9`), `interface` (NIC pro raw
  Ethernet rámec; vyžaduje CAP_NET_RAW), `min_queue` (práh čekajících `image_embed`/`face_detect`
  jobů, default 1), `cooldown` (min. rozestup mezi packety, default 5m). Validace `ErrInvalidWake`:
  enabled vyžaduje validní MAC + aspoň jeden cíl (`broadcast_addr`/`interface`).
- **Rate-limit klíče (`ratelimit.*`, `internal/ratelimit`):** per-client-IP token-bucket limity na
  náročných endpointech. Sekce `upload`/`bulk`/`import`/`tiles`, každá `{rate_per_sec, burst}`;
  defaulty 5/30, 2/10, 1/3, 50/200; `rate_per_sec ≤ 0` pravidlo vypne (middleware no-op). Env např.
  `KUKATKO_RATELIMIT_UPLOAD_RATE_PER_SEC`. Login má vlastní limiter (`auth.login_rate_*`), geocode
  proxy taky (`maps.*`).
- **Maps/geocode klíče (`maps.*`, `internal/config`):** `mapy_api_key` (server-side, env
  `MAPY_API_KEY`; prázdný → tile/rgeocode proxy 503, `places` job se neregistruje a `/process/places`
  vrací 503), `base_url` (default `https://api.mapy.com`), a throttle reverse-geokódu pro background
  **`places` job** (cachuje lokalitu fotky): `geocode_rate_per_sec` (default 5, ≤ 0 vypne) +
  `geocode_burst` (default 10) — chrání měsíční mapy.com kreditní budget, zpracovat pomalu je OK.
  `KUKATKO_MAPS_GEOCODE_RATE_PER_SEC`/`_GEOCODE_BURST`.
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
