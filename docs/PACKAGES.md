# Balíčky backendu

Popisný referenční přehled Go balíčků. **Nejsou to pravidla** — pravidla jsou
v [`CLAUDE.md`](../CLAUDE.md). Nový nebo změněný balíček zapiš sem a přidej mu
jeden řádek do `## Mapa balíčků` v `CLAUDE.md`.

<!-- BODY BEGIN -->
- **Layout:** `cmd/kukatko/` (tenký Cobra entrypoint: root + `serve` + `migrate` + `version`),
  `internal/server/` (chi HTTP server, graceful shutdown), `internal/version/`
  (ldflags-injectable `Version`/`Commit`), `internal/config/` (typovaná konfigurace,
  Viper, `Load()`), `internal/database/` (pgxpool wrapper `DB` s `Ping`/`Close`/`Pool`,
  embedded migration runner `Migrate`, pgvector typy registrované na každém spojení;
  SQL migrace v `internal/database/migrations/*.sql`), `internal/database/dbtest/`
  (integrační test harness: `dbtest.New(t)`, `dbtest.TruncateAll`), `internal/auth/`
  (autentizace/autorizace: `Role` admin/editor/viewer/ai + `authorize`, bcrypt cost 12
  `HashPassword`/`CheckPassword`, UID/token generátory, sliding-window `Limiter`,
  `Store` nad pgx, `Service` orchestrace login/session/bootstrap/správa uživatelů,
  `API` = HTTP handlery + RBAC middleware `RequireAuth`/`RequireWrite`/`RequireAdmin`/`RequireImport` +
  `RegisterRoutes`; session a users v migraci `0002_auth.sql`.
  **Role `ai`** (migrace `0023_role_ai.sql` rozšiřuje CHECK na `users.role`) = automatizovaný agent
  přihlašovaný API tokenem: `CanWrite()`=true (jako editor) a `CanImport()`=true, ale `IsAdmin()`=false.
  Import je jediná jinak admin-only akce, kterou `ai` smí — proto samostatný predikát `requireImport`/
  middleware `RequireImport` (admin **nebo** ai); ostatní admin moduly drží `RequireAdmin` (jen admin).
  **Admin poznámka u uživatele** (`note`, migrace `0021_user_note.sql`, nullable TEXT →
  `COALESCE(note,'')` v `userColumns`): `User.Note` je `json:"-"`, takže neuteče přes
  `loginResponse` (`/auth/login`, `/auth/me`); admin endpointy ho přidávají zpět přes
  `adminUserResponse` (embedded `User` + `note`). Validace `validateNote` → `ErrNoteTooLong`
  (`MaxNoteLen` = 1000 **run**) → 400. `UpdateUserInput.Note` je `*string`: `nil` = nech být,
  `""` = smaž (SQL `note = COALESCE($6::text, note)`).
  **Audit správy uživatelů** (`store_user_audit.go`): admin handlery volají auditované varianty
  `Service.CreateUserAudited`/`UpdateUserAudited`/`SetUserDisabledAudited`/`ResetPasswordAudited`,
  které přes `Store.CreateUserAudited`/`UpdateUserProfileAudited`/`SetUserDisabledAudited`/
  `SetPasswordHashAudited` zapíšou `user.create`/`user.update`/`user.disable`/`user.password` audit
  řádek `inAuditedTx` — **ve stejné transakci** jako změna (rollback ⇒ žádný audit řádek). Neaudito­
  vané `CreateUser`/`UpdateUser`/`SetUserDisabled`/`ResetPassword` zůstávají pro bootstrap a test
  seeding (sdílejí jádro `prepareNewUser`/`validateUserUpdate`/`invalidateIfDisabled`). Handler bere
  actora z `UserFromContext` a staví `audit.FromRequest(r,uid).Entry(...)`; `details` nese
  `username`/`role` (create) resp. `role`/`disabled` (update/disable).
  **API tokeny** (`apitoken.go`, `store_apitoken.go`, `service_apitoken.go`,
  `handlers_apitoken.go`, migrace `0020_api_tokens.sql`): dlouhodobý bearer credential
  `kkt_<id>_<secret>` pro neinteraktivní klienty. `<id>` je PK řádku (prefix `at`), takže ověření
  je **jeden indexovaný lookup**, ne scan přes hashe; `<secret>` nese 256 bitů z `crypto/rand`.
  Ukládá se **jen hex SHA-256** secretu (`hashAPITokenSecret`) — **záměrně ne bcrypt**: bcrypt
  chrání nízkoentropická hesla proti slovníku a platí se jednou za login, kdežto token se ověřuje
  na *každém* requestu a 256bitový náhodný secret žádný slovník nemá; porovnání je konstantní
  v čase (`subtle.ConstantTimeCompare`). Plaintext se vrací **právě jednou**, při vytvoření.
  Model `APIToken` (`name`, `expires_at`, `last_used_at`, `revoked_at`) + čisté predikáty
  `Revoked`/`Expired`/`Active`; token **dědí roli vlastníka** (žádný role sloupec, žádný druhý
  permission systém). `Service.AuthenticateAPIToken` vrací u všech selhání jediný
  `ErrInvalidAPIToken` (→ 401, nikdy 403, tělo nerozlišuje případ) a stampuje `last_used_at`
  nejvýš jednou za `apiTokenUseInterval` (= minuta, zrcadlí `slidingRenewInterval`).
  `Store.CreateAPITokenAudited`/`RevokeAPITokenAudited` píšou audit `inAuditedTx` — mutace i audit
  řádek v jedné transakci; `errNoAuditableChange` udělá z opakované revokace no-op bez audit
  záznamu. `bearerToken` parsuje `Authorization` case-insensitive dle RFC 7235; neexistující nebo
  ne-Bearer schéma propadne na cookie), `internal/photos/`
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
  = rating 0) + `Flag` (`pick`/`reject`/`eye` korelovaným `EXISTS`) — všechny aktivní jen když je `RatedBy`
  nastaveno, fotka bez řádku = rating 0 / flag `none`,
  řazení taken_at/created_at/uid/title/file_size **+ `rating`** (řazení dle ratingu `RatedBy`
  uživatele přes korelovaný poddotaz nad `user_ratings`, `NULLS LAST` — nehodnocené poslední; aktivní
  jen s `RatedBy`) **+ `chronology`** (`SortByChronology`: `COALESCE(taken_at, created_at)` — úplné,
  stabilní chronologické pořadí, nedatovaná fotka padá na svůj upload čas; interní řazení pro
  album view, není veřejný sort alias), stránkování limit/offset; `Count` sdílí
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
  `YearBuckets(params)` (rok-histogram `Years{Years:[]YearBucket{Year,Count},Total}` v
  `store_years.go` — jedním `GROUP BY date_part('year', taken_at)`, řazení `year DESC`; sdílí
  `buildWhere` s `List`/`Count`, takže count bucketu = přesně to, co `List` vrátí pro tytéž filtry
  plus ten rok; `params.Sort`/`Order`/stránkování se ignorují, fotky bez `taken_at` do bucketů
  nespadají, ale `Total` (přes `Count`) je zahrnuje — podklad `photoapi` year facetu),
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
  (úložiště originálů: rozhraní `Storage` + **dvě** implementace — filesystemová `FS`
  `NewFS(root)` a Cloudflare R2 `NewR2(R2Options)`. Vybírá je `storage.backend` (`fs` **default** /
  `r2`) přes `newStorage(cfg)` v `cmd/kukatko/storage.go`; nad rozhraním žádný balíček rozdíl
  nepozná. Společné oběma: `Store(ctx,src,takenAt,originalName)` streamuje + počítá **SHA256**,
  layout `YYYY/MM/<filename>` (datum z `taken_at`, fallback čas importu); kolize jmen: shodný
  obsah → `ErrAlreadyExists` (dedup signál), jiný obsah → číselný sufix `name_1.ext` **bez
  přepisu**; `Open`/`Stat`/`Delete`/`Materialize` s cestami confinovanými do rootu
  (`ErrInvalidPath`), chybějící soubor/objekt wrapuje `os.ErrNotExist`; MIME z obsahu (sniff
  512 B) + přípona jako hint (`mediaTypeByExt` pro HEIC/RAW/video); sentinely
  `ErrAlreadyExists`/`ErrInvalidPath`/`ErrTooManyCollisions`; nikdy nedrží soubor celý v RAM
  (sdílený `streamToTemp` v `temp.go`).
  Trojice pro **hromadné přesuny** (`put.go`): `Put(ctx,src,StoredFile)` zapíše stream na klíč,
  který **volí volající** (to `Store` neumí — ten si klíč odvozuje z `taken_at` a jména), a to
  přesně tehdy, když obsah sedí na deklarovanou velikost i SHA256 — jinak `ErrSizeMismatch`
  /`ErrHashMismatch` a **žádný použitelný objekt nezůstane** (`FS` přejmenuje až po ověření,
  `R2` špatně nahraný objekt zase smaže: uniklý objekt je menší zlo než objekt, jehož metadata
  lžou o jeho bajtech). `Head(ctx,relPath)` vrátí identitu objektu (velikost, digest, MIME) bez
  přenosu obsahu — u `R2` jeden levný metadata request, u `FS` plné čtení; prázdný `Hash` =
  „digest neznám" (objekt psal cizí nástroj), nikdy „digest sedí". `Check(ctx)` ověří, že root
  existuje / bucket existuje a klíče na něj dosáhnou (`ErrBucketNotFound`), aby hodiny běžící
  job spadl v první vteřině na překlepu, ne až na prvním uploadu. `storage.IsSystemic(err)`
  odliší **nepoužitelný cíl** (špatné klíče, chybějící/zakázaný bucket, rozbitý endpoint; navíc
  401/403 s neznámým kódem) od per-objektového selhání (chybějící klíč, throttle, useknutý
  upload) — to je rozhodnutí „zastav celý běh" vs. „posbírej a jeď dál".
  **`FS`** publikuje **atomickým hard-linkem** přes temp v `<root>/.tmp`.
  **`R2`** (`r2.go`, klient **minio-go v7** — stejná knihovna jako `internal/backup`, žádná nová
  závislost) jede nad **privátním** bucketem, kde **object key = `photos.file_path` doslova**
  (žádný nový sloupec, žádná migrace klíčů). Hard-link nemá ekvivalent a není potřeba: `PutObject`
  je atomický, katalogový dedup drží unique constraint na `photos.file_hash`. Upload jde přes
  staged temp soubor v `storage.temp_path`, protože klíč závisí na obsahu — bez hashe nelze
  odlišit byte-identický re-upload od stejnojmenného jiného souboru; SHA256 se ukládá jako
  user-metadata `x-amz-meta-sha256` a je to jediný způsob, jak dedup poznat bez stažení objektu
  (ETag je MD5, u multipartu opaque). Objekt bez té metadaty (zapsaný cizím nástrojem) se bere
  jako jiný obsah → sufix.
  Rozhraní **neprozrazuje filesystem**: `URL(relPath)` vrací adresu, na kterou si klient sáhne
  přímo — `FS` vrací `""` (originály na disku nejsou přes HTTP dostupné, servíruje je aplikace),
  `R2` vrací **podepsanou krátkodobou URL** (nebo `""`, když `media_base_url` chybí);
  `Materialize(ctx,relPath)` vrací **reálný lokální soubor** pro nástroje, které umí jen jméno
  souboru (exiftool, ffprobe, ffmpeg, heif-convert, vipsthumbnail) + `cleanup`, který volající
  **vždy** zavolá (i na error path, jinak vzdálený backend leakuje temp); `FS` **nekopíruje** —
  vrátí cestu samotného originálu a no-op `cleanup` (idempotentní), takže lokální vývoj i testy
  zůstávají zero-copy; `R2` stáhne objekt do `storage.temp_path` se **zachovanou příponou**
  (`imgconvert` na ni dispatchuje RAW/video) a `cleanup` (idempotentní přes `sync.Once`) ho smaže —
  i na error path, kde se částečný soubor maže hned.
  **Podepsané URL** (`sign.go`, `URLSigner`): `https://<media_base_url>/<key>?exp=<unix>&sig=<hex>`,
  kde `sig = HMAC-SHA256(secret, key + "\n" + exp)` — podpis kryje klíč i expiraci a klíč se
  podepisuje **neescapovaný** (UTF-8 jméno se percent-enkóduje až při renderu cesty).
  `Verify(key,exp,sig)` porovnává **v konstantním čase** proti **dvěma** tajemstvím (současné +
  předchozí), takže rotace `url_signing_secret` nemá okno rozbitých URL; podepisuje se vždy tím
  současným. Nejdřív se ověří podpis (podvržený klíč i expirace → `ErrInvalidSignature`), pak
  teprve expirace (`ErrURLExpired`). Default TTL 1 h. Klíč **není tajemství** — bez platného
  podpisu ho edge Worker odmítne. Access key ani signing secret se nikdy nedostanou do logu
  ani do chyby. **Worker (verifikátor) žije v infra repu** (`cloudflare-r2/`, Terraform), takže
  kontrakt drží golden vektory `testdata/url_signature_vectors.json` — publikovaný artefakt, proti
  kterému testuje Go signer (`sign_test.go`) i Worker; změna algoritmu = regenerace souboru
  a souběžná úprava Workeru. Integrační testy `r2_integration_test.go` (tag `integration`) běží proti reálnému
  S3-kompatibilnímu endpointu z `KUKATKO_TEST_S3_ENDPOINT` (stačí MinIO; bez proměnné se skipnou)),
  `internal/storagemigrate/`
  (jednorázový **resumovatelný** přesun knihovny z lokálního disku do object storu; pohání
  `kukatko storage migrate-to-r2`, flagy a billing viz [`docs/OPERATIONS.md`](OPERATIONS.md).
  `New(Config)` → `Migrator`, `Run(ctx)` → `Result`. Config bere úzká rozhraní `Catalogue`
  /`Source`/`Destination` (ne `storage.Storage`), takže celá pipeline jde protestovat s `FS`
  místo bucketu; `Store` nad pgx pool je produkční `Catalogue`. **Závazné pořadí na fotku:**
  nahraj všechny objekty (originál + náhledy, které už v cache jsou — negeneruje nové) →
  `Head` je přečti zpátky a ověř velikost i SHA256 → `MarkMigrated` commitne řádek → teprve pak
  volitelný `Delete` lokálního originálu. Neexistuje cesta, kde bajty žijí jen tam, kde se za ně
  nikdo nezaručil. **Kurzor** je `photos.storage_migrated_at` (migrace `0019`), tedy
  high-watermark `internal/importeru` **per řádek** — skalární watermark by lhal, protože při
  `Concurrency > 1` doběhne fotka N+1 běžně dřív než N; stránkuje se `uid` kurzorem, takže
  selhaná fotka nepadá do nekonečné smyčky ve stejném běhu. Objekt, který v bucketu leží se
  správnou velikostí i digestem, se **znovu nenahrává** (`Skipped`) — to je celý rozdíl mezi
  migrací zdarma a placenou. Per-fotková selhání se **sbírají** do `Result.Failures` a běh jede
  dál; `storage.IsSystemic` chybu eskaluje na okamžitý stop. `DryRun` neošahá bucket, DB ani
  disk — jen spočítá objekty a bajty. `Report` callback (throttlovaný `ReportEvery`, default
  15 s) tiskne průběh + odhad zbytku. Streamuje; nikdy nedrží soubor v RAM. Integrační test
  `storagemigrate_integration_test.go` (tag `integration`, potřebuje MinIO **i**
  `KUKATKO_TEST_DATABASE_URL`) zabije běh uprostřed fotky, resumne ho a tvrdí, že každý objekt
  přistál **právě jednou** a že fotce, které selhala verifikace, nikdo nesmazal originál),
  `internal/mediaurl/`
  (razí klientské adresy médií a razítkuje je na foto-payloady; jediné rozhodnutí dělá storage
  backend přes `URL`. `NewBuilder(store)` → `Builder` s `Thumb(uid,fileHash,size)` /
  `Download(uid,filePath)` (adresa pro klienta: podepsaná URL Workeru, jinak fallback na vlastní
  routu `/api/v1/photos/...`), `Object(relPath)` / `ThumbObject(fileHash,size)` (**syrová** odpověď
  backendu — prázdný řetězec = „stream to sám", neprázdný = „redirectuj tam"; tohle používají media
  routy) a `Decorate(list)` / `DecorateOne(&photo)`, které plní `Photo.ThumbURL`+`Photo.DownloadURL`.
  `Download` si u fallbacku vynutí `?original=true`, aby obě větve znamenaly totéž (uložený originál,
  nikdy rendering nedestruktivního editu). **Nil `*Builder` je platný** a chová se jako backend, který
  nic nepublikuje → API postavené bez storage (test) pořád vrací funkční payload. `uid`/`size` se do
  routy percent-enkódují. Grid velikost je `thumb.GridSize` (`tile_500`) — jediná, kterou payload nese.
  **Autorizace hlídá discovery**: URL se razí jen do odpovědi, na kterou už caller měl právo; objekt
  pak hlídá podpis, který Worker ověřuje. Doc comment balíčku to říká výslovně, protože **starší návrh
  s veřejným bucketem** dělal z `photos.private` a archivu jen prezentační filtr — to už **neplatí**,
  jsou to reálné bezpečnostní hranice. Volají ho `photoapi` (`annotate`/`handleUpdate`/`runArchive`/
  `resolveSimilar` + media routy), `peopleapi` a `globalsearchapi`; storage jim předává
  `cmd/kukatko/serve.go` jako sdílený `mediaStore`),
  `internal/thumb/`
  (thumbnailer náhledů, **CGO-free**: registr velikostí `sizes`+`sizeOrder` ve dvou režimech
  `fit` (max-strana, zachová poměr, neupscaluje) a `crop-square` (center-crop), default sada
  `fit_720/1280/1920/2560/3840` + `tile_100/224/500`; cache layout pod `storage.cache_path`
  `thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg` (shard z hex SHA256), regenerovatelné +
  **idempotentní** (skip existujících) + atomický zápis temp+rename; `Thumbnailer` =
  `New(store,cacheDir,WithConcurrency(n))` s API `Generate(ctx,photo,sizes...)`/
  `GenerateAll(ctx,photo)` (mapa size→abs cesta)/`Path(hash,size)`/`Open(hash,size)`;
  balíčkové `RelPath(hash,size)` vrací tentýž cache path relativně — je to zároveň **object key**
  náhledu ve vzdáleném backendu, proto se layout exportuje místo aby se odvozoval podruhé jinde;
  **publikace na object store**: po zápisu velikosti do cache ji `publishSize` nahraje `Put`em pod
  `RelPath` do backendu, který publikuje URL (`store.URL(rel) != ""`, tj. R2) — u FS je to no-op;
  když upload selže, lokální soubor se smaže, takže velikost platí za nevygenerovanou a příští
  `Generate` ji znovu vyrenderuje i nahraje (invariant: nacachovaná velikost na publikujícím
  backendu je vždy i v bucketu, aby klientská object URL rozlišila). Tím čerstvý ingest na R2
  dostane náhledy do bucketu stejně jako `storage migrate-to-r2`;
  `GridSize` (`tile_500`) je velikost, kterou renderuje mřížka a kterou nese `thumb_url` v payloadu;
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
  **, `GET /photos/timeline`, **`GET /photos/years`**, `GET /search` a `GET /favorites`**; `parseListParams`
  validuje query → `photos.ListParams` (`limit`≤500/`offset`, `sort`
  newest/oldest/taken_at/added/title/size**/rating** + `order` — **`album` scope obojí přebije**
  na `SortByChronology`+`asc` (album je vždy chronologické, defaulty ostatních pohledů se nemění),
  `archived` false/true/only, `private`,
  `has_gps`, `taken_after`/`taken_before`, `camera`, `lens`, `uploader`, `q`, **`year` (čtyřciferný
  1000–9999) → `Year`**, **`album`/`label`
  scope** → `AlbumUID`/`LabelUID`, **`country`/`city` place scope** → `Country`/`City`,
  **per-user `min_rating` (int) + `flag` (`pick`/`reject`/`eye`)**
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
  **per-user hodnocení** (`ratings.go`): `PUT /photos/{uid}/rating` `{rating?:0..5, flag?:none|pick|reject|eye}`
  (každý přihlášený, aspoň jedna hodnota, validace předem → 400 neplatná, 404 chybějící fotka, 503 bez
  `Ratings` backendu; nastaví rating a/nebo flag přes `SetRating`/`SetFlag`) + `DELETE /photos/{uid}/rating`
  (idempotentní clear přes `ClearRating` → 204); `RatingStore` interface (splňuje ho `organize.Store`,
  `SetRating`/`SetFlag`/`ClearRating`/`RatingsAmong`) je nil-safe (nezapojeno → rating 0 / flag `none`,
  rating endpointy 503);
  `GET /photos/years` (`handleYears`, `years.go`) = **rok-histogram** pro year facet knihovny
  → `photos.Store.YearBuckets` → `{years:[{year,count}],total}`; bere tytéž filtry jako list
  (vč. per-user `FavoriteOf`/`RatedBy`), ale **`params.Year` sám nuluje** — facet nesmí zúžit
  vlastní nabídku; neplatný param → 400;
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
  `storage.Materialize`, jednou za request — sdílí ho i transcode fallback) pro inline HTML5
  přehrávání; live fotka servíruje svůj **motion klip** sidecar
  (`pickMotionClip` dle video MIME/přípony), still image → 404; **on-the-fly transcode** gated
  `VideoConfig`/`video.transcode` (default off) + `video.IsWebFriendlyCodec` + `video.FFmpegAvailable`
  → `video.Transcode` (H.264/MP4 progressive, žádný range, `no-store`), fallback na originál když
  ffmpeg selže nebo je codec neznámý; **nedestruktivní
  edit** přes `Organizer` (album/label chipy detailu); **uploader detailu** přes `UserResolver`
  interface (splňuje ho `auth.Store.GetUserByUID`, drátuje `buildPhotoAPI`): `handleDetail`
  resolvuje `photo.UploadedBy` → `uploader{uid,name}` (`name` = `display_name`, fallback `username`),
  nil-safe (nezapojeno / bez uploadera / neresolvovatelný uživatel → `uploader` vynechán, jen na
  detailu, žádné N+1 v listu); a `EditService`/`edit.go`+`media_edit.go`
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
  `Thumbnailer.Remove`, volitelně S3 objekt přes `RemoteRemover`) **a pak** DB řádek přes
  `photos.DeleteAudited(uid,entry)` — smaže řádek (kaskáduje embeddingy/faces/markery/album_photos/
  photo_labels/phashe/edity/oblíbené přes `ON DELETE CASCADE`) **a zapíše `photo.purge` audit řádek
  ve stejné transakci** (durable-audit; rollback ⇒ žádný audit řádek); artefakty napřed, takže
  přerušený purge nechá re-purgovatelný řádek místo dangling souborů; idempotentní (chybějící
  soubor/`os.ErrNotExist`/`thumb.ErrInvalidHash` se ignoruje); `PurgePhoto(uid,meta)` (404
  `photos.ErrPhotoNotFound`, `ErrNotArchived` na živou fotku), `EmptyTrash(meta)` (purge všech
  archivovaných) a `PurgeExpired()` (jen `archived_at` starší než `RetentionDays`, ≤ 0 = no-op)
  iterují `photos.ListArchivedUIDs` v oldest-first dávkách (`BatchSize`, default 200) →
  `Result{Purged,Failed}`; **každý purge = jeden `photo.purge` audit řádek** (`audit.Meta` s
  actorem u ručních purgů, prázdný systémový actor u plánovaného `PurgeExpired`; `details.source` =
  `manual`/`empty_trash`/`retention`); **per-fotka selhání** se zaloguje, počítá a přeskočí (offset
  roste, fotka zůstane v koši pro retry), jen zrušený ctx aborte; `RunPurge(ctx, interval)` =
  plánovaný úklid (hned + každý interval, vypnutý při retenci ≤ 0) pro `serve` goroutinu),
  `internal/jobs/`
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
  markeru/subjektu (mazání, rename, assign/unassign); **auditované varianty**
  `CreateSubjectAudited`/`UpdateSubjectAudited`/`DeleteSubjectAudited` a
  `CreateMarkerAudited`/`AssignSubjectAudited`/`UnassignSubjectAudited` (`internal/people/audit.go`)
  přijmou `audit.Entry` a zapíšou ho **ve stejné transakci** jako změnu (`audit.Write(ctx,tx,entry)`),
  takže audit řádek commitne/rollbackne atomicky s mutací (konvence `internal/photos`/`internal/organize`);
  sdílené tx-jádro (`insertMarkerTx`/`assignSubjectTx`/`unassignSubjectTx`/`prepareSubjectInsert`) používají
  obě varianty), `internal/facematch/`
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
  **přiřazovací state machine** `Apply(ctx,AssignRequest,audit.Meta)` (backing
  `POST /photos/{uid}/faces/assign`, editor/admin): `create_marker` (vytvoří face marker + přiřadí
  subjekt + zalinkuje obličej), `assign_person` (přiřadí subjekt existujícímu markeru),
  `unassign_person` (odpojí subjekt), drží `faces` cache i `marker.reviewed` konzistentní
  (assign → reviewed, unassign → unreviewed), **auto-create subjektu dle jména** (find-or-create
  přes `Slugify`+`GetSubjectBySlug`); **audit**: každý přechod zapíše přes auditované `people`
  metody 1 řádek ve stejné transakci jako změnu — `create_marker`/`assign_person` → `face.assign`,
  `unassign_person` → `face.unassign` (target = marker, details akce/foto/subjekt/face_index);
  `meta` je actor+request z `photoapi.handleFaceAssign`, prázdná pro systémový cluster caller
  (actor NULL); sentinely `ErrInvalidAction`/`ErrMissingBBox`/
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
  `StorageSource` (= `storage.Materialize` + `imgconvert.EnsureDecodable` za rozhraním
  `Materializer`, HEIC/RAW/video se převedou, `Close` uvolní temp i materializovaný originál),
  pošle sidecaru `FaceEmbeddings` (512-dim + pixel bbox + det_score) a
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
  `SubjectStore` (podmnožina `people.Store`: `ListSubjects`/`GetSubjectByUID`/`CreateSubjectAudited`/
  `UpdateSubjectAudited`/`DeleteSubjectAudited`/`ListPhotoUIDsBySubject` — každá mutace bere `audit.Entry`
  postavenou v `auditEntry` (`subject.create`/`update`/`delete`, actor z auth kontextu, details name/type;
  `DELETE` napřed načte subjekt kvůli details a čistému 404)) a `PhotoStore` (`photos.Store.ListByUIDs`)
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
  `photos.favorite` z photo-sorteru) a **per-user hodnocení** (hvězdičky 0–5 + osobní označení none/pick/reject/eye);
  tabulky `albums`/`album_photos`/`labels`/`photo_labels`/
  `user_favorites` v migraci `0011_albums_labels_favorites.sql` a `user_ratings` v migraci
  `0016_user_ratings.sql`: **`albums`** = `uid PK`
  (prefix `al`), `slug UNIQUE` (Slugify z `title`, číselný sufix na kolizi), `title`/`description`,
  `type IN (album|folder|moment|state|month)`, `cover_photo_uid` (FK photos `ON DELETE SET NULL`),
  `private`, `created_by` (FK users
  `ON DELETE SET NULL`), časy — sloupec `order_by` odstranila migrace
  `0022_chronological_albums.sql` (album se vždy zobrazuje chronologicky, volba řazení neexistuje);
  **`album_photos`** = členství `(album_uid, photo_uid) PK`, oba FK
  `ON DELETE CASCADE`, `added_at` (ruční pozice `sort_order` odstranila táž migrace); **`labels`** = `uid PK` (prefix `lb`), `slug UNIQUE`
  (z `name`), `name`, `priority`, časy; **`photo_labels`** = připojení `(photo_uid, label_uid) PK`,
  oba FK `ON DELETE CASCADE`, `source IN (manual|ai|import)`, `uncertainty` (int %), `created_at`;
  **`user_favorites`** = `(user_uid, photo_uid) PK`, oba FK `ON DELETE CASCADE`, `added_at`;
  **`user_ratings`** = `(user_uid, photo_uid) PK`, oba FK `ON DELETE CASCADE`, `rating SMALLINT 0..5`
  (CHECK), `flag TEXT IN (none|pick|reject|eye)` (CHECK; `eye` přidán migrací 0025, `pick`/`reject`
  = 👍/👎, `eye` = 👁), `updated_at` — řádek existuje jen pro
  nedefaultní hodnotu (store maže řádek, který spadne na rating 0 + flag `none`), takže fotka bez
  řádku = rating 0 / flag `none`;
  `Store` = `NewStore(pool)` nad sdíleným pgx poolem: **alba** `CreateAlbum`/`GetAlbumByUID`/
  `GetAlbumBySlug`/`UpdateAlbum` (re-slug z title)/`ListAlbums` → `[]AlbumSummary` (řazení dle
  title; `AlbumCount` + `CoverUID`/`TakenFrom`/`TakenTo` — vše dopočtené **v jednom SQL**, bez
  migrace: `photo_count` z LEFT JOIN `album_photos`, `MIN`/`MAX(taken_at)` z LEFT JOINu na `photos`
  s `archived_at IS NULL`, fallback obálka z `LEFT JOIN LATERAL … ORDER BY taken_at DESC NULLS LAST,
  uid LIMIT 1`; `CoverUID = COALESCE(cover_photo_uid, fallback)` → ručně zvolená obálka vyhrává,
  jinak nejnovější **živá** fotka, deterministicky stejná při každém dotazu. Archivovaná fotka se
  počítá do `photo_count`, ale obálku nedodá ani rozsah neposune; nedatovaná fotka může být obálkou,
  ale do rozsahu nevstupuje)/
  `SearchAlbums(q,limit)` (accent/case-insensitive ILIKE nad `immutable_unaccent(title/description)`,
  s počty → `[]AlbumCount`, cap limit — podklad `globalsearchapi`)/
  `DeleteAlbum`/`AddPhoto` (idempotentní, bez pozice — `ON CONFLICT DO NOTHING`)/`RemovePhoto`
  (idempotentní)/`SetCover` (set/clear cover)/`ListPhotoUIDs`
  (chronologicky: `COALESCE(taken_at, created_at), photo_uid` přes JOIN na `photos`); **štítky** `CreateLabel`/`GetLabelByUID`/`GetLabelBySlug`/`UpdateLabel`
  (re-slug)/`ListLabels` (s počty, řazení priority DESC)/`SearchLabels(q,limit)` (accent/case-insensitive
  ILIKE nad `immutable_unaccent(name)`, s počty, cap limit — podklad `globalsearchapi`)/`DeleteLabel`/
  `AttachLabel` (idempotentní upsert source/uncertainty)/`DetachLabel` (idempotentní)/`ListPhotoUIDsByLabel`; **oblíbené**
  `AddFavorite`/`RemoveFavorite` (obojí idempotentní)/`IsFavorite`/`ListFavorites` (per-user,
  newest-first)/`FavoritedAmong` (z množiny photo uid vrátí per-user podmnožinu oblíbených jako
  množinu — anotace celé stránky `is_favorite` jedním dotazem); **hodnocení** (`ratings.go`)
  `SetRating(user,photo,rating)` (validace 0–5 → `ErrInvalidRating`) / `SetFlag(user,photo,flag)`
  (validace none/pick/reject/eye → `ErrInvalidFlag`) — idempotentní upsert jedné kolony v transakci,
  druhá kolona se zachová; když řádek spadne na rating 0 + flag `none`, smaže se (tabulka zůstane
  řídká); `ClearRating(user,photo)` smaže rating i flag jedním idempotentním DELETE (mirror
  `RemoveFavorite`, no-op na nehodnocené/chybějící fotce — podklad `DELETE /photos/{uid}/rating`);
  `GetRating(user,photo)` → `PhotoRating{Rating,Flag}` (chybějící řádek = 0/`none`, nil err);
  `RatingsAmong(user,photoUIDs)` → mapa `photo_uid → PhotoRating` jen pro hodnocené fotky (anotace
  celé stránky jedním dotazem, mirror `FavoritedAmong`, chybějící caller defaultuje na 0/`none`);
  typy `AlbumType`/`LabelSource`/`RatingFlag` (none/pick/reject/eye)
  zrcadlí SQL CHECKy, slug helper s per-druh
  fallbackem (`album`/`label`); sentinely `ErrAlbumNotFound`/`ErrLabelNotFound`/`ErrPhotoNotFound`/
  `ErrUserNotFound`/`ErrSlugExhausted`/`ErrInvalidType`/`ErrInvalidSource`/`ErrInvalidRating`/
  `ErrInvalidFlag` — FK porušení při zápisu
  do join tabulek (`user_favorites`/`user_ratings`) se mapuje na not-found sentinel podle porušeného
  sloupce přes sdílený `translateUserPhotoFK` (`photo_uid` → photo, jinak user;
  album/label přes `translateMembershipFK`/`translateAttachFK`);
  **audited varianty** mutací (`audit.go`): `CreateAlbumAudited`/`UpdateAlbumAudited`/`DeleteAlbumAudited`/
  `AddPhotosAudited`/`RemovePhotosAudited` a `CreateLabelAudited`/`UpdateLabelAudited`/`DeleteLabelAudited`/
  `AttachLabelAudited`/`DetachLabelAudited` spustí změnu i `audit.Write` **v jedné transakci** (durable
  audit — když se mutace rollbackne, audit záznam nevznikne; sdílený `inAuditedTx` +
  `insertAuditedWithUniqueSlug`, který kolizi slugu u create/update řeší retry přes samostatné transakce
  a audit píše jen úspěšný pokus); ne-audited varianty zůstávají pro systémové importery
  (`psimport`/`ppimport`, bez aktora)), `internal/organizeapi/`
  (read/curace HTTP API nad alby a štítky — podklad Albums/Labels UI: rozhraní `AlbumStore`/
  `LabelStore` (podmnožiny `organize.Store`) → unit-testovatelné s faky bez DB;
  `NewAPI(Config{Albums,Labels,RequireAuth,RequireWrite})`+`RegisterRoutes` mountuje dva
  subroutery: **alba** `GET /albums` (RequireAuth, `{albums:[AlbumSummary]}` — počty, efektivní
  `cover_uid` a rozsah `taken_from`/`taken_to`),
  `POST /albums` (RequireWrite, 201, `title` povinný, validace typu přes `ErrInvalidType`),
  `GET /albums/{uid}` (RequireAuth), `PATCH /albums/{uid}` (RequireWrite, edituje
  title/description/cover_photo_uid/private; **strukturální `type` se zachová** —
  handler načte existující album a `type` z těla nepřebírá, takže folder/moment/… nelze přepsat),
  `DELETE /albums/{uid}` (RequireWrite → 204), členství `POST /albums/{uid}/photos`
  `{photo_uids:[…]}` (přidá, bez pozice — album je vždy chronologické),
  `DELETE /albums/{uid}/photos` `{photo_uids:[…]}` (odebere, idempotentní) — oba
  membership endpointy vrací aktuální chronologické pořadí `{photo_uids:[…]}`, nejdřív ověří
  existenci alba (`requireAlbum` → 404); ruční přeřazení `PATCH /albums/{uid}/order` bylo
  odstraněno (→ 404); **štítky** `GET /labels` (RequireAuth, `{labels:[LabelCount]}`),
  `POST /labels` (RequireWrite, 201, `name` povinný), `GET /labels/{uid}` (RequireAuth),
  `PATCH /labels/{uid}` (RequireWrite, name/priority), `DELETE /labels/{uid}` (RequireWrite → 204),
  připojení `POST /labels/{uid}/photos` `{photo_uid,source?,uncertainty?}` → 204 (validace source
  přes `ErrInvalidSource`), `DELETE /labels/{uid}/photos` `{photo_uid}` → 204 (ověří existenci
  štítku → 404, pak idempotentní detach); body decode `DisallowUnknownFields` + 1 MiB limit;
  **každá mutace píše přesně jeden audit záznam ve stejné transakci** (volá audited store varianty,
  aktor z `auth.UserFromContext` + `audit.FromRequest`, akce `album.create`/`update`/`delete`/
  `add_photos`/`remove_photos` a `label.create`/`update`/`delete`/`attach`/`detach`; add/remove fotek =
  jeden dávkový záznam s `photo_uids`/`count`, attach/detach nese `photo_uid` v details); odpovědi
  se nemění; sentinely mapované `ErrAlbumNotFound`/`ErrLabelNotFound`/`ErrPhotoNotFound`→404,
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
  ostatní entry; action konstanty `ActionPhotosBulk`/`ActionPhoto{Update,Archive,Unarchive,Purge}`/
  `ActionAlbum{Create,Update,Delete}`/`ActionLabel{Create,Update,Delete}`/`ActionFaceAssign`/
  `ActionUser{Create,Update,Disable,Password}`; `Store` = `NewStore(pool)` se `Record(ctx,Entry)`
  (vlastní spojení) a **filtrovaným čtením** `List(ctx,Filter)`/`Count(ctx,Filter)` (`Filter{ActorUID,
  TargetType,TargetUID,Action,Since,Until,Limit,Offset}`, newest-first, limit cap 500/default 100)
  pro admin výpis. **Zapojené in-tx mutace**: bulk (`internal/bulk`) + foto PATCH/archive/unarchive
  přes audited varianty `photos.Store.{UpdateMetadata,Archive,Unarchive}Audited`, **trvalý purge**
  `photos.Store.DeleteAudited` (`internal/trash` → `photo.purge`, systémový actor u plánované retence)
  a **správa uživatelů** `auth.Store.{CreateUser,UpdateUserProfile,SetUserDisabled,SetPasswordHash}Audited`
  (`user.*`) — vše mutace + audit v jedné tx přes sdílený `rowQuerier`/`mutateAudited` (photos) resp.
  `inAuditedTx` (auth); další domény (alba/štítky/lidé) následují stejnou konvenci), `internal/auditapi/`
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
  `Private`/`Archive`/`Favorite *bool`, **`Rating *int` (0–5) + `Flag *string` (none/pick/reject/eye)**;
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
  **`set_rating` (0–5) / `set_flag` (none/pick/reject/eye)** s validací → 400,
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
  (HTTP API importů za `RequireImport` (admin **nebo** ai): rozhraní `Queue` (Enqueue, splňuje `*jobs.Store`) a `RunLister`
  (List, splňuje `*importer.Store`); `NewAPI(Config{Queue,Runs,RequireImport,EnablePhotoPrism,
  EnablePhotoSorter})`+`RegisterRoutes` mountuje **vždy** `GET /import/runs` (historie + `sources`
  flagy jaké zdroje jsou nakonfigurované) a — **jen pro nakonfigurované zdroje** —
  `POST /import/photoprism` → `pp_import` a `POST /import/photosorter` → `ps_migrate` job (sdílený
  `enqueue` helper, 202 `{job_id,status}`); `jobs.ErrDuplicate` → 409 (už běží), jiná chyba → 500;
  `GET /import/runs` (`parsePaging` limit≤200/offset, neplatný → 400) vrací
  `{runs,limit,offset,sources:{photoprism,photosorter}}` (stránka `import_runs` newest-started-first
  přes `importer.Store.List`); celá API se v `serve` mountuje vždy (`buildImportAPI` v
  `cmd/kukatko/import.go`), aby historie fungovala i bez zdroje; triggery neběží inline — patří na
  background worker), `internal/backup/`
  (v procesu, plánovaná **S3 záloha** databáze a originálů do **druhého, nezávislého bucketu**, vše
  za rozhraními `ObjectStore`/`Dumper`/`OriginalSource` → unit-testovatelné s faky bez S3/DB/FS;
  `Service` =
  `New(Config{Objects,Originals,Dumper,Retention,Logger})` (panika na nil Objects/Originals/Dumper);
  **`Run(ctx,ts)`** dělá tři věci v pořadí: (1) **dump DB** přes `Dumper` streamovaný na S3 jako
  `db/kukatko-<ts>.dump` (`objectSize=-1`, nikdy celý v RAM; ts dodá plánovač/příkaz), (2)
  **inkrementální sync originálů** (`SyncOriginals` — skip dle klíče+velikosti přes `ObjectStore.Stat`,
  klíč = relativní cesta originálu; **čistě aditivní**, smazání ve zdroji se nepropaguje), (3)
  **retence** (`PruneDumps` — prořeže staré dumpy na posledních
  `Retention`, `≤0` = nechat vše; **jen prefix `db/`, nikdy originály**); **dump je povinný** — selhání
  abortuje běh **před** prořezáním, takže neúspěšná záloha nemůže smazat poslední dobré dumpy;
  `Run` serializuje souběžné běhy (`ErrAlreadyRunning`), `Trigger(ctx,ts)` spustí běh na pozadí
  (detached ctx, pro HTTP handler), `Status()` = stav + poslední běh; **`RunSchedule(ctx,spec)`**
  plánovač přes `ParseSchedule` (standardní 5-pole cron / `@daily`/`@every` deskriptory přes
  `robfig/cron`; prázdný → `ErrNoSchedule`, neplatný → `ErrInvalidSchedule` → plánované zálohy
  vypnuté, manuální fungují) s vlastní ctx-aware smyčkou; **`s3Store`** (`NewS3Store(S3Options)`) =
  minio-go/v7 adaptér, **path-style** (`BucketLookupPath`), `parseEndpoint` (scheme→TLS, bare host =
  TLS), sentinely `ErrNotConfigured`/`ErrInvalidEndpoint`, `isNotFound` (404/NoSuchKey) → Stat
  ok=false / Remove idempotentní, **`CopyFrom(srcBucket,srcKey,key)`** = **server-side copy** přes
  `ComposeObject` (jeden zdroj → degraduje na prostý `CopyObject`, nad 5 GiB sáhne po multipart
  copy) — bajty **neprojdou procesem**; request jde na *tenhle* endpoint, takže jeho credentials
  musí umět **číst `srcBucket`**; **`pgDumper`** (`NewPgDumper(dsn)`) = shell-out `pg_dump
  --format=custom --no-owner --no-privileges`, **DSN přes env `PGDATABASE`** (ne argument, aby heslo
  nebylo v `ps`), `Dump` vrací reader (Close čeká na proces + surfacuje stderr), `PgDumpAvailable`,
  `ErrPgDumpMissing`;
  **zdroj originálů** = `OriginalSource` (`List` + `CopyTo(ctx,dst,original)`; `CopyTo` si sám volí,
  jak bajty přenese) a vybírá ho `storage.backend` v `cmd/kukatko/backup.go` (`buildBackupOriginals`):
  **`DiskOriginals`** (`NewDiskOriginals(root)`, backend `fs`) = walk úložiště (skip `.tmp`,
  confine klíče proti traversalu), `CopyTo` streamuje soubor nahoru přes `Put` — **slouží i obnově**
  přes `Stat(key)` (existuje + velikost, pro skip-existing) a `Write(key,r)` (atomický zápis do
  `.tmp` + rename → resumovatelné);
  **`BucketOriginals`** (`bucket.go`, `NewBucketOriginals(source,bucket)`, backend `r2`) = `List`
  vylistuje primární bucket (skip prefixů `db/` a `.tmp/` — dump ani rozdělaný upload není originál),
  `CopyTo` deleguje na `dst.CopyFrom` → **kopie bucket→bucket server-side**, takže knihovna se nikdy
  netahá na VPS, aby se odtud nahrála zpět; sentinely `ErrNoSourceStore`/`ErrNoSourceBucket`
  (nenakonfigurovaný primár **nesmí** vypadat jako prázdná knihovna) a `errBackupSameBucket` ve
  wiringu (mířit zálohu na primární bucket = nezálohovat nic). **Objektový store nemá verzování**,
  druhý bucket je jediná ochrana proti smazání → originály se **nikdy** neexpirují; klíče ani
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
  `Decoder` (`StorageDecoder` = `storage.Materialize`+`imgconvert.EnsureDecodable`, fakeovatelný) →
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
  limitery), `internal/obs/`
  (strukturované logování + request-scoped plumbing: slog **JSON** handler na konfigurovatelné
  úrovni (`ParseLevel`/`NewLogger`/`Setup`, `log.level`, neplatná úroveň → chyba při startu),
  **redakční `ReplaceAttr` hook** (`redactAttr`) škrtne hodnotu každého atributu, jehož klíč nese
  tajemství (password/passwd/secret/token/apikey/access_key/secret_key/authorization/cookie/
  credential/dsn) na `[REDACTED]` — i uvnitř skupin, takže secret nikdy neuteče do logu, ani když
  ho někdo omylem zaloguje; **`AccessLog` middleware** vypíše jeden strukturovaný řádek na HTTP
  request (request id z chi `RequestID`, method/path/route pattern/status/duration/bytes/remote IP
  + autentizovaný uživatel, když je znám — auth middleware ho stampne přes `SetUser` do
  request-scoped `fields` bagu sdíleného pointerem přes kontext, protože zápis hluboko v řetězu musí
  vidět top-level logger); level dle statusu (5xx=error, 4xx=warn, jinak info), `/metrics` scrape se
  přeskočí, request id se zrcadlí do hlavičky `X-Request-Id` i sdíleného route labelu metrik),
  `internal/metrics/`
  (Prometheus instrumentace HTTP serveru, workeru fronty a infra (pgx pool, embeddings sidecar,
  importy, thumbnaily), namespace `kukatko`; **izolovaný `*prometheus.Registry`** místo
  process-global `DefaultRegisterer`, takže testy staví nezávislé metric surface bez cross-test
  leaku; `New()` → `Registry` zaregistruje HTTP (`kukatko_http_requests_total` counter + latency
  histogram + inflight gauge, route label = **chi route pattern**, nikdy raw URL), job lifecycle
  (started/finished counter + duration histogram by type/outcome), embeddings (duration histogram +
  up gauge), import progress (gauge per source/outcome) a thumbnail duration + standardní
  `go_`/`process_` kolektory; **pull-at-scrape kolektory** `RegisterDBPool` (živé pgx pool stats)
  a `RegisterJobQueue` (hloubka fronty by_state/by_type přes `QueueDepthFunc`, `collectTimeout` 5 s,
  aby pomalá DB neblokovala scrape) čtou data ve chvíli scrapu bez extra goroutin; `Handler()`
  mountuje `serve` na `/metrics` (middleware ten path přeskočí, scrape neinstrumentuje sám sebe),
  observační metody `JobStarted`/`JobFinished`/`ObserveEmbeddingCall`/`SetEmbeddingUp`/
  `SetImportProgress`/`ObserveThumbnail` a `Middleware(routeOf)` se předají subsystémům, které
  emitují události; zrcadlí lehký photo-sorter přístup — jeden namespace, omezené label sety;
  tunables v `metrics.*` configu), `internal/web/`
  (SPA fallback handler `web.Handler()`/`SPAHandler` + `internal/web/static` embed
  `//go:embed all:dist/*`; Vite build se zapisuje do `internal/web/static/dist`, ten je
  gitignorovaný kromě committed `.gitkeep`, aby embed kompiloval i bez buildnutého
  frontendu). Detail: [`docs/DEVELOPMENT.md`](DEVELOPMENT.md).

- **Remote CLI klient (`internal/ctl`):** klientská polovina `kukatko ctl` — jediný kus stromu, který
  Kukátko volá **přes HTTP jako cizí server**, ne přes DB a disk. Nemá nic společného s `internal/config`
  (ten popisuje *server* a o vzdáleném endpointu nic neví); jediný stav, který vlastní, je klientský
  soubor `~/.config/kukatko/ctl.yaml`. Motivace: levnější v tokenech než MCP server — žádné tool schema
  se nenačítá do kontextu modelu, jen krátký příkaz a úzký výsledek. Proto je výstup kompaktní.
  - `config.go` — `Context{Name,Server,Token}` + `Config{CurrentContext,Contexts}` ve stylu kubectl.
    `Load(path)` (chybějící soubor = prázdný config, ne chyba — běh jen z env proměnných), `Save(path,cfg)`
    (atomicky: temp 0600 → `Rename` → `Chmod` 0600, adresář 0700; **existující world-readable soubor
    utáhne**, nikdy do něj token nezapíše tak, jak je). `DefaultConfigPath()` ctí `XDG_CONFIG_HOME`.
    `Resolve(cfg, contextName, env)` → `Endpoint`: vybere kontext (jménem → jinak `current-context`),
    pak `KUKATKO_SERVER`/`KUKATKO_TOKEN` **přebijí po jednotlivých polích**, takže samotné
    `KUKATKO_TOKEN` přecredentialuje uložený kontext. Chyby `ErrContextNotFound`, `ErrNoServer`.
  - `client.go` — `NewClient(server, token)` (validuje absolutní http(s) URL → `ErrInvalidServerURL`),
    interní `get(ctx, path, query)` a `send(ctx, method, path, body)` posílají
    `Authorization: Bearer <token>` a vracejí **surové** tělo (`json.RawMessage`), protože `-o json`
    tiskne bajty serveru beze změny; `204 No Content` vrací `nil` tělo. Úspěch je celý rozsah `2xx` —
    API odpovídá 200, 201 i 204 podle endpointu. `401` → `*UnauthorizedError` s krátkou akční hláškou
    (token chybí / expiroval / byl revokován + jak vyrobit nový); `403` → `*ForbiddenError`, který
    **řekne, že nestačí role** (mutace chtějí editor/admin, viewer jen čte), místo výpisu serverového
    `insufficient permissions`. **Nikdy** výpis těla ani tokenu; jiný non-2xx → `*StatusError`
    s `{"error":…}` textem serveru (jinak omezený úryvek těla). Tělo se čte přes `io.LimitReader`,
    timeout 30 s.
  - `photos.go` — `ListPhotos`/`GetPhoto`/`SearchPhotos` + `DecodePhotoPage`/`DecodePhotoDetail`.
    **Dekodér je per-resource záměrně:** API nemá jednotnou list obálku (`photos` vrací
    `{photos,total,limit,offset,next_offset}`, ostatní zdroje holý seznam) a sjednocovat ho nesmíme —
    rozbil by se frontend. `ListOptions` (limit/offset/sort/order/year/album/label/favorite/archived)
    se validuje lokálně (`ErrInvalidPaging`/`ErrInvalidYear`/`ErrInvalidArchived`), takže překlep
    nestojí round trip. **`--year` API nezná** — překládá se na inkluzivní rozsah
    `taken_after`/`taken_before` (`taken_at >= … <= …`), horní mez je poslední instant 31. 12.
    `SearchOptions` přidává `q` + `mode` (`fulltext`/`semantic`/`hybrid`).
  - `albums.go` — `ListAlbums`/`GetAlbum`/`CreateAlbum`/`AddAlbumPhotos`/`RemoveAlbumPhotos`
    + `DecodeAlbums`/`DecodeAlbum`/`DecodePhotoUIDs`. Obálka je **holé `{"albums":[…]}` bez stránkování**
    — proto vlastní dekodér. `PhotoCount` plní jen list; detail ho neposílá, takže ho renderer netiskne.
    `AlbumInput` se validuje lokálně (`ErrEmptyTitle`, `ErrInvalidAlbumType`); membership posílá celý
    seznam uidů v **jednom** požadavku a server vrací obnovené pořadí.
  - `labels.go` — `ListLabels`/`GetLabel`/`CreateLabel`/`AttachLabel`/`DetachLabel` + `DecodeLabels`/
    `DecodeLabel`. Obálka je **holé `{"labels":[…]}`** řazené dle priority (třetí tvar). Attach/detach
    odpovídají `204`. Prázdný `source` se ze těla vypustí, ať server dosadí vlastní `manual`
    (`ErrInvalidLabelSource`, `ErrEmptyName`).
  - `subjects.go` — `ListSubjects`/`GetSubject`/`SubjectPhotos` + `DecodeSubjects`/`DecodeSubject`.
    Obálka je **holé `{"subjects":[…]}`**; galerie subjektu ale **má tvar `/photos`**, takže se čte
    `DecodePhotoPage` (stejný tvar, ne sjednocený). `PageOptions` nabízí jen limit/offset — filtry
    katalogu endpoint nečte, tak je ctl ani nenabízí.
  - `curate.go` — `ListFavorites` (obálka `/photos`, parametr `favorite` se zahazuje: endpoint se
    scopne sám), `AddFavorite`/`RemoveFavorite`, `SetRating`/`ClearRating`. Oblíbené i hodnocení jsou
    **per-user**, takže je smí měnit i viewer. Hvězdy i flag jsou nezávislé ukazatele — co pošleš `nil`,
    to server nechá být (`ErrEmptyRating`, `ErrInvalidRating`, `ErrInvalidFlag`).
  - `bulk.go` — `Bulk(ctx, photoUIDs, ops)` posílá **jeden** `POST /photos/bulk` na celou dávku, protože
    server ji aplikuje v jedné transakci; smyčka po fotkách by atomicitu vyměnila za N transakcí a N
    audit řádků. `BulkOperations` má tagy 1:1 s API (endpoint odmítá neznámá pole) a všechno `omitempty`,
    aby se nulová hodnota neposlala jako reálná změna. `Validate()` zrcadlí serverové kontroly (vzájemně
    se vylučující set/clear páry, rozsah hvězd, flag, souřadnice) → `ErrNoOperations`,
    `ErrConflictingOperations`, `ErrInvalidLocation`. `DecodeBulkResult` čte `{results,counts}` (čtvrtý
    tvar). `ParseLocation("lat,lng")`.
  - `uids.go` — `ParsePhotoUIDs(r)` čte množinu fotek ze stdin ve **čtyřech** tvarech: obálka
    `{"photos":[…]}` (přesně to, co tiskne `ctl photos list -o json`), holé JSON pole uidů, holé pole
    objektů s `uid`, nebo prostý seznam oddělený bílými znaky. `NormalizeUIDs` trimuje, zahazuje prázdné
    a **deduplikuje** (aby počet v potvrzovacím dotazu odpovídal tomu, co se opravdu pošle) →
    `ErrNoPhotoUIDs`. `ConfirmThreshold = 50` je hranice, nad kterou se příkaz ptá.
  - `output.go` — `ParseFormat` (`table`/`json`; **`yaml` schválně ne**), `WriteJSON` (echo bajtů beze
    změny), sdílené `writeTable`/`writeKeyValues`/`writeLine`, `WritePhotoPage` (tabulka + jeden souhrnný
    řádek: kolik z kolika, `offset`, `next offset`, u hledání efektivní `mode` a případné `degraded`),
    `WritePhotoDetail`, `WriteContexts` (**token se nikdy netiskne**, jen `stored`/`not set`).
    Prázdný výsledek = jediný řádek `no photos found`, žádná hlavička — agent si nesplete hlavičku
    s řádkem.
  - `render.go` — `WriteAlbums`/`WriteAlbum`, `WriteLabels`/`WriteLabel`, `WriteSubjects`/`WriteSubject`,
    `WriteMembership` (jeden řádek: kolik fotek album nově drží), `WriteBulkResult` (souhrn + tabulka
    **jen** neúspěšných fotek) a `WriteAck`. `Ack` je jediný payload, který si CLI **vyrábí samo**: kde
    API odpoví `204`, není co propustit beze změny, takže `-o json` dostane
    `{"status":"ok","message":…}` a pipeline pozná úspěch od chyby.

  Strom příkazů, konfigurační soubor a symlink `kukatkoctl` popisuje
  [`docs/OPERATIONS.md`](OPERATIONS.md).
