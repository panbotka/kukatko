# HTTP API

Popisný referenční přehled HTTP endpointů pod `/api/v1`. **Nejsou to pravidla** —
pravidla jsou v [`CLAUDE.md`](../CLAUDE.md). Nový nebo změněný endpoint zapiš sem.

<!-- BODY BEGIN -->
- **Auth API (`/api/v1`):** `POST /auth/login` (set HttpOnly+SameSite=Strict cookie + opaque
  `download_token`), `POST /auth/logout`, `GET /auth/me`, `POST /auth/password` (zruší ostatní
  session). Admin-only: `GET|POST /admin/users`, `PATCH /admin/users/{uid}`,
  `POST /admin/users/{uid}/disable`, `POST /admin/users/{uid}/password` (reset zruší všechny
  session uživatele). Odpovědi admin user endpointů nesou vedle `display_name` i volný **`note`**
  (admin poznámka, proč účet existuje / kdo to je). Obě pole jsou volitelná, default prázdný
  řetězec. `note` delší než **1000 znaků** (runy, ne bajty) → 400 se zprávou pojmenující pole.
  `PATCH` má u `note` **partial-update** sémantiku: vynechaný klíč nechá uloženou poznámku beze
  změny, `""` ji smaže. **`note` čte jen admin** — nikdy není v payloadu `POST /auth/login` ani
  `GET /auth/me`. Role: admin/editor/viewer/ai (editor+admin+ai write; `ai` = automat na API token
  s editorskými právy zápisu **plus** import, ale bez ostatních admin práv). **Sliding session expiry**
  (`auth.session_ttl` do cap `auth.session_max_lifetime`), **login rate-limit**
  (`auth.login_rate_limit`/`auth.login_rate_window` → 429), **bootstrap admin** z
  `auth.bootstrap_admin_username/password`. Middleware navíc `RequireAuthOrDownloadToken`
  (session cookie nebo `?t=download_token` přes `Service.AuthenticateDownloadToken` →
  `Store.GetSessionByDownloadToken`) pro média bez cookie.
- **API tokeny (`/api/v1/auth/tokens`, vše za `RequireAuth`):** dlouhodobé bearer credentials pro
  neinteraktivní klienty (CLI, skripty, agenti). `POST /auth/tokens` (`{name, expires_at?}`) → 201
  `{token:{id,user_uid,name,created_at,expires_at?,last_used_at?,revoked_at?}, secret:"kkt_<id>_<secret>"}`
  — **`secret` se vrací jediný a poslední raz**, server drží jen SHA-256 hash; 400 (prázdné jméno /
  expirace v minulosti / neznámé pole), **429** (creation rate-limit sdílí login limiter, klíč
  `apitoken:<uid>|<ip>`). `GET /auth/tokens` → `{tokens:[…]}` — **jen vlastní tokeny volajícího**,
  nikdy secrety ani hashe. `DELETE /auth/tokens/{id}` → 204 (idempotentní; už revokovaný token je
  taky 204 a nepíše druhý audit záznam); **cizí token → 404, ne 403** (admin smí revokovat komukoli).
  Create i revoke píšou audit (`api_token.create`/`api_token.revoke`) **ve stejné transakci** jako
  mutaci.
- **Bearer autentizace:** `authenticateRequest` bere `Authorization: Bearer kkt_<id>_<secret>`
  **vedle** session cookie (cookie cesta beze změny). Token **dědí roli svého uživatele** → žádný
  druhý permission systém, `RequireAuth`/`RequireWrite`/`RequireAdmin`/`RequireImport` platí beze
  změny (typicky token role `ai`: write + import, zbytek admin-only vrací 403). Špatný
  bearer je **finální** (nezkouší se cookie téhož requestu); jiné schéma než Bearer propadne na
  cookie. Revokovaný / expirovaný / neznámý / poškozený token i token zakázaného uživatele → vždy
  **401** (nikdy 403) se **stejným tělem** — nelze rozlišit, který případ nastal. `last_used_at` se
  přepisuje nejvýš jednou za minutu (stejná pojistka jako sliding session).
- **Upload API (`/api/v1`):** `POST /upload` (editor/admin přes `RequireWrite`) — `multipart/form-data`
  s jedním+ soubory, **streamuje** se. Vrací `{"results":[{filename,status,outcome,photo_uid?,error?,
  warnings?}]}` (celkově 200, 409 sémantika duplicit per-file). Mountuje se druhým `server.WithAPI`
  v `serve` (`buildIngest` v `cmd/kukatko/ingest.go`). Limit `upload.max_file_size_mb` (0 = bez limitu).
- **Photos API (`/api/v1`, `internal/photoapi`):** `GET /photos` (přihlášený) — list s filtry/
  řazením/stránkováním (query params, neplatný → 400) → `{photos,total,limit,offset,next_offset}`;
  filtr `?album={uid}`/`?label={uid}` scopne výpis na fotky alba/štítku (sdílený endpoint pro
  galerii alba i štítku, ctí všechny ostatní filtry/řazení/stránkování — viz Albums & Labels API);
  **`album`/`label` jsou multi-hodnotové**: opakované parametry (`?album=a&album=b&label=x&label=y`)
  vyberou několik alb/štítků najednou, kombinace **AND** — fotka musí být ve **všech** vybraných
  albech a nést **všechny** vybrané štítky (každý UID = vlastní korelovaný `EXISTS`). Jedna hodnota
  (`?album={uid}`) je zpětně kompatibilní jedno-albový scope;
  **`person` scope** (`?person={uid}`, také multi-hodnotový, opakované `?person=a&person=b`,
  kombinace **AND**) zúží výpis na fotky obsahující **všechny** vybrané subjekty (osoba/zvíře/jiné) —
  join přes **markery** (pojmenovaná tvář/oblast, `invalid = FALSE`; zamítnuté markery se nepočítají),
  každý UID = vlastní korelovaný `EXISTS` nad `markers`;
  **album scope si vždy vynutí chronologii** (stačí ≥ 1 vybrané album): fotky alba jdou od nejstarší
  (`taken_at ASC`, fotka bez data pořízení padá na svůj upload čas `created_at`, takže pořadí je úplné
  a stabilní) a `sort`/`order` z query se pro album ignorují — defaulty endpointu pro ostatní pohledy
  se nemění;
  `GET /photos/timeline` (přihlášený) — **měsíční date-histogram** knihovny (podklad rok/měsíc
  scrubberu): přijímá **stejné filtry** jako `GET /photos` přes `parseListParams`, odpověď
  `{buckets:[{year,month,count,cumulative}],total}`, buckety řazené nejnovější první (dle `taken_at`,
  jako výchozí mřížka), `cumulative` = počet fotek **před** bucketem (mapuje bucket na scroll-index),
  `total` (přes `Count`) zahrnuje i fotky bez data pořízení (do žádného bucketu nespadají, řadí se
  na konec); `sort`/`order` se ignorují (vždy grupováno dle data), backuje ho
  `photos.Store.TimelineBuckets` (sdílí `buildWhere` s `List`/`Count`), neplatný param → 400;
  `GET /photos/years` (přihlášený) — **rok-histogram** knihovny (podklad **year facetu** filtrů):
  přijímá **stejné filtry** jako `GET /photos` přes `parseListParams`, odpověď
  `{years:[{year,count}],total}`, buckety **nejnovější rok první**; ctí viditelnost volajícího
  (`archived`) i per-user filtry (`favorite`, `min_rating`/`flag`) přesně jako list, takže
  count bucketu = přesně to, co mřížka ukáže po výběru toho roku. Filtr `year` je **jediný
  ignorovaný** — facet nesmí zúžit vlastní nabídku (jinak by po výběru 2019 zbyl v nabídce jen 2019);
  `sort`/`order` a stránkování se ignorují (vždy grupováno dle roku). `total` (přes `Count`) zahrnuje
  i fotky **bez data pořízení** (nespadají do žádného roku), takže může převýšit součet countů.
  Backuje ho `photos.Store.YearBuckets` (sdílí `buildWhere` s `List`/`Count`), neplatný param → 400;
  filtr `?year=YYYY` na `GET /photos` (čtyřciferný rok 1000–9999, jinak 400) drží jen fotky pořízené
  v tom kalendářním roce — fotky s neznámým `taken_at` nikdy nematchují;
  `GET /search?q=&mode=` (přihlášený) — **sémantické + hybridní hledání**, `mode` =
  `fulltext`|`semantic`|`hybrid` (default `hybrid`, neznámý → 400): **fulltext** = česky-aware
  fulltext nad `fts tsvector` (dictionary `simple` + `unaccent`, řazení `ts_rank`
  title>description>notes>file_name); **semantic** = `q` → CLIP embedding přes sidecar →
  cosine HNSW nad `embeddings`, řazení dle podobnosti; **hybrid** = fúze obou přes
  **Reciprocal Rank Fusion (k=60)**, dedup. Všechny módy ctí ostatní list filtry + stránkování,
  odpověď jako list + `mode` + `degraded`; `q` povinný (prázdný → 400); **box offline** →
  `semantic`/`hybrid` graceful fallback na fulltext s `degraded: true`;
  list i search nesou per-fotku `is_favorite` **+ per-user `rating`/`flag`** pro aktuálního uživatele,
  `?favorite=true` scopne list na jeho oblíbené, **`?min_rating=n` / `?flag=pick|reject|eye` / `?sort=rating`**
  scopnuté na něj (fotka bez řádku = rating 0 / flag `none`);
  `GET /photos/{uid}` plný detail + `files` + `is_favorite` + `rating`/`flag`;
  `GET /photos/{uid}/similar` (přihlášený) — **vizuálně podobné fotky** dle kosinové vzdálenosti
  embeddingů (HNSW nad `embeddings`, `SimilarSearcher`/`vectors.Store`), nejbližší první: odpověď
  `{similar:[{…fotka, distance}]}` (`distance` = kosinová vzdálenost ke zdrojové fotce, menší =
  bližší), `?limit` (default 24, max 100); zdrojová fotka je z výsledku vyloučená. Fotka bez
  embeddingu nebo bez similar backendu → prázdné `{similar:[]}` (200), 404 chybějící fotka;
  **per-user oblíbené** `PUT`/`DELETE /photos/{uid}/favorite` (každý přihlášený, idempotentní → 204,
  404 chybějící fotka, 503 bez backendu) + `GET /favorites` (oblíbené aktuálního uživatele ve tvaru
  list endpointu, filtry/řazení/stránkování jako `/photos`);
  **per-user hodnocení** `PUT /photos/{uid}/rating` `{rating?:0..5, flag?:none|pick|reject|eye}` (osobní
  označení: `pick`=👍, `reject`=👎, `eye`=👁; každý
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
  prázdná pole) a **`uploader`** `{uid,name}` — kdo fotku nahrál, jméno resolvnuté server-side přes
  `UserResolver` (`auth.Store.GetUserByUID`; `name` = `display_name`, fallback `username`); vynechán
  (`omitempty`) u fotek bez `uploaded_by` (importy z PhotoPrism/photo-sorteru) i když uživatele nelze
  resolvnout — resoluce je **jen na detailu**, list/search per-fotku uploadera neřeší (žádné N+1);
  **nedestruktivní edit** (`internal/photoedit` + `edit.go`/`media_edit.go`):
  `GET /photos/{uid}/edit` (přihlášený) → uložený `photos.Edit` (crop/rotace 0-90-180-270/jas/kontrast,
  neupravená fotka → neutrální edit) a `PUT /photos/{uid}/edit` (editor/admin) zapíše edit do
  `photo_edits` (validace bounds; originál se nikdy nemění — `GET …/download` ho **renderuje za běhu**
  přes `photoedit.Apply`, pokud caller nedá `?original=true`);
  `PATCH /photos/{uid}` (editor/admin) částečná úprava
  metadat — `title/description/notes/ai_note/taken_at/lat/lng` (null maže nullable, validace
  souřadnic). **Odpověď má stejný tvar jako `GET /photos/{uid}`** — plný detail včetně `files`,
  `albums`, `labels`, `is_favorite` a `uploader` (sdílený `writeDetail` v `internal/photoapi`), ne
  holý `photos.Photo`: klient si detailem z odpovědi nahradí ten, co drží, takže chybějící pole by mu
  z detailu zmizela (dřív padal na `albums.map` z `undefined`). Klient posílá jen **skutečně
  změněná** pole: přeposlání nezměněného `taken_at` by přepnulo `taken_at_source` `exif` → `manual`,
  přeposlání nezměněných souřadnic by je zaokrouhlilo na 6 desetinných míst z textového pole.
  `ai_note` je volný text z externí AI klasifikace (píše ho i automat přes tuto routu),
  vrací se v detailu i listu jako součást `photos.Photo` a je zahrnutý ve fulltextu (§ Vyhledávání);
  `POST /photos/{uid}/archive`+`/unarchive`
  (editor/admin) soft-delete přes `archived_at` (archivované mimo výchozí list);
  `POST /photos/{uid}/regenerate-thumbnail` (editor/admin) — **servisní akce** pro
  chybějící/zastaralý náhled: znovu vygeneruje náhledy fotky a její perceptuální hashe
  z originálu přes `thumbjob.Service.ForceRegenerate` (sdílí thumbnailer i job handler,
  žádná duplicitní logika), **přepíše** existující cache náhledů i hashe (na rozdíl od
  repair cesty `thumbnail` jobu, která present data přeskakuje), originál se **nikdy
  nemění**. Běží **synchronně**, aby mohla vrátit jasný výsledek `{status:"regenerated",
  sizes:[…]}` (200) nebo typovanou chybu: 404 chybí fotka, **422** originál chybí nebo ho
  nelze dekódovat (`thumbjob.ErrRegenerateFailed`), 503 když regenerace není zapojená.
  Idempotentní (bezpečné klikat opakovaně); zapisuje se do audit logu jako
  `photo.thumbnail` se seznamem regenerovaných velikostí v details;
  **koš / trvalé mazání** (`trash.go`, backuje `internal/trash` přes rozhraní `Purger`, nil → 503):
  `POST /photos/{uid}/purge` (editor/admin, `?confirm=true` jinak 400, 404 chybí, 409 fotka není
  archivovaná → 204) a `POST /trash/empty` (editor/admin, `?confirm=true` → `{purged,failed}`)
  trvale mažou archivované fotky, `GET /trash/info` (přihlášený) vrací `{retention_days}` pro odpočet
  do auto-purge; seznam koše jede přes sdílené `GET /photos?archived=only`;
  **adresy médií v payloadu** (`internal/mediaurl`): každá vrácená fotka nese `thumb_url`
  (grid náhled `tile_500`) a `download_url` (originál, `?original=true` sémantika — nikdy rendering
  editu). Hodnoty razí storage backend přes `Storage.URL`: `FS` vrací prázdno → fallback na vlastní
  routy níže, `R2` vrací **krátkodobě podepsanou URL** (default 1 h) na doménu edge Workeru, takže
  aplikace nepřenáší ani bajt médií. Klient je bere **jak jsou** a nikdy je neskládá z UID (podpis
  nedokáže spočítat). Podepsaná URL expiruje → viz `useThumbSrc` v `docs/FRONTEND.md`;
  `GET /photos/{uid}/thumb/{size}` a `/download` (session/`?t=` token) **streamují** média
  (`Cache-Control`/`ETag`/`304`), nebo — když backend publikuje objekty — odpoví **`302` redirectem**
  na podepsanou URL (`Cache-Control: private, no-store`, aby cache nepřežila podpis); routy zůstávají,
  takže staré odkazy a záložky fungují dál. `GET /photos/{uid}/video` (session/`?t=` token) streamuje
  video **s HTTP Range** (206 partial, `Accept-Ranges`, seek; live fotka = motion klip, still → 404)
  pro inline HTML5 přehrávání, resp. redirectuje na Worker, který Range obsluhuje přímo z R2 (odpadá
  požadavek na seekovatelný lokální soubor); volitelný on-the-fly transcode neweb-friendly codeců přes
  `video.transcode` config (default off) krmí `ffmpeg` rovnou podepsanou URL (`ffmpeg` čte http(s)).
  **Hromadné stažení ZIP** (`internal/photoapi/zip.go`): `POST /photos/download-zip`
  (session/`?t=` token — **stejná autorizace jako single download**, kdo smí stáhnout jednu, smí
  víc) **streamuje ZIP originálů** rovnou na odpověď (`archive/zip`, metoda `Store` — originály jsou
  už komprimované; nic se nebufferuje celé v RAM, `CGO_ENABLED=0`). Tělo `{photo_uids?, album_uid?,
  name?, date?}`: `album_uid` se expanduje server-side na **živé** (nearchivované) fotky alba v
  chronologickém pořadí (přes `photos.List` s `AlbumUIDs`, takže archivované ani neuvidí),
  `photo_uids` je explicitní výběr v pořadí klienta (chybějící UID se **tiše přeskočí**, jako u single
  downloadu); obě množiny se sloučí a deduplikují podle UID. `file_name` fotky je jméno položky,
  kolidující jména se odliší příponou ` (2)`, ` (3)`… před koncovkou. Originál chybějící ve storage se
  **přeskočí a zapíše** do textové položky `MISSING.txt` v archivu — nepřeruší celý ZIP. Jméno archivu:
  `name` (např. titul alba) + `.zip`, jinak `kukatko-photos-<date>.zip` (`date` posílá klient, server
  se na téhle cestě **vyhýbá wall-clocku**); mtime položek je `taken_at` fotky. Strop **1000 souborů**
  na požadavek (`maxZipFiles`), nad ním **413** ještě před prvním bajtem archivu; požadavek bez fotek →
  400. Vždy **streamuje přes `storage.Open`** (i na publikujícím backendu — jeden archiv nejde poskládat
  z redirectů, na rozdíl od single `/download`).
  **Autorizace hlídá discovery:** podepsaná URL se razí jen do odpovědi, na kterou už caller měl
  právo, takže archivovanou fotku nikdy neuvidí. Na rozdíl od dřívějšího návrhu s veřejným
  bucketem je archiv **skutečná bezpečnostní hranice** (viz doc comment `internal/mediaurl`).
  Mountuje se třetím `server.WithAPI` (`buildPhotoAPI` v `cmd/kukatko/photos.go`).
- **Jobs API (`/api/v1`, `internal/jobsapi`, admin-only přes `RequireAdmin`):**
  `GET /jobs/stats` → `{by_state,by_type,total}`; `GET /jobs` → `{jobs,limit,offset}`
  (recent/dead-letter výpis, query `state`/`limit`/`offset`, neplatný → 400);
  `POST /jobs/{id}/requeue` → refreshnutý job (dead/failed → queued; 404 missing, 409
  ne-requeueable). Frontend polluje (žádné SSE). Mountuje se `server.WithAPI`
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
  404 chybějící subjekt. Mountuje se `server.WithAPI` (`buildOutlierAPI` v
  `cmd/kukatko/outliers.go`).
- **People/Subjects API (`/api/v1`, `internal/peopleapi`):** `GET /subjects` (RequireAuth) →
  `{subjects:[{...subject, marker_count}]}` (řazení dle jména, počty non-invalid markerů);
  `POST /subjects` (RequireWrite) → 201 vytvoří subjekt z `{name,type,favorite,private,notes,
  cover_photo_uid?}` (prázdné jméno / neznámý typ → 400); `GET /subjects/{uid}` (RequireAuth) →
  subjekt (404); `PATCH /subjects/{uid}` (RequireWrite) → editace stejných polí (404/400);
  `DELETE /subjects/{uid}` (RequireWrite) → 204 (markery se odpojí server-side); `GET
  /subjects/{uid}/photos` (RequireAuth) → paginovaná galerie fotek subjektu
  `{photos,total,limit,offset,next_offset}` (newest-first, jen nearchivované, `limit`≤500). Mountuje
  se `server.WithAPI` (`buildPeopleAPI` v `cmd/kukatko/people.go`). Záznamy fotek subjektu
  staví na `people.Store.ListPhotoUIDsBySubject` (distinct non-invalid markery → photo uid).
- **Process API (`/api/v1`, `internal/processapi`, admin-only přes `RequireAdmin`):**
  `POST /process/embeddings` → `{enqueued}` (backfill `image_embed` pro fotky bez embeddingu),
  `POST /process/faces` → `{enqueued}` (backfill `face_detect` pro fotky bez detekce obličejů),
  `POST /process/clusters` → `{created}` (re-clustering nepřiřazených obličejů přes
  `cluster.Recluster`), `POST /process/places` → `{enqueued}` (backfill `places` reverse-geokódu pro
  geotagované fotky bez místa přes `placesjob.BackfillPlaces`; 503 když není mapy.com klíč),
  `POST /process/thumbnails` → `{enqueued}` (backfill `thumbnail` pro fotky **bez vygenerovaného
  náhledu** přes `thumbjob.BackfillThumbnails`; „chybí náhled“ = fotka bez perceptuálního hashe,
  který `thumbnail` job počítá spolu s náhledem). Volitelné `?all=true` naplánuje **každou
  nearchivovanou fotku** (vynucený úplný re-run — dožene i chybějící velikost náhledu u fotky, která
  hash už má; job přeskočí velikosti již v cache, takže je běh levný a originál nikdy nemění).
  Náhledy se generují **lokálně**, takže backfill funguje i když je box offline; fronta jobů
  deduplikuje, takže opakované spuštění je idempotentní. Mountuje se `server.WithAPI` (`buildJobs`).
- **Albums & Labels API (`/api/v1`, `internal/organizeapi`):** **alba** `GET /albums`
  (RequireAuth) → `{albums:[{...album, photo_count, cover_uid?, taken_from?, taken_to?}]}`
  (`organize.AlbumSummary`): `cover_uid` je **efektivní obálka** — ručně zvolené
  `cover_photo_uid`, jinak **nejnovější živá fotka alba** (deterministicky: `taken_at DESC NULLS
  LAST, uid`); `taken_from`/`taken_to` je **rozsah `taken_at`** přes fotky alba. Obojí agreguje
  jediný SQL dotaz (LEFT JOIN + LATERAL, bez migrace) a počítá **jen s živými fotkami** —
  archivovaná fotka se započítá do `photo_count`, ale obálku nedodá ani rozsah neposune. Chybí,
  když album nemá co ukázat / žádná fotka nemá známý `taken_at`. `POST /albums`
  (RequireWrite) → 201 z `{title,description?,type?,cover_photo_uid?,private?}` (prázdný
  title / neplatný typ → 400); `GET /albums/{uid}` (RequireAuth, 404); `PATCH /albums/{uid}`
  (RequireWrite) edituje title/description/cover_photo_uid/private (**`type` se zachová**,
  není editovatelný); `DELETE /albums/{uid}` (RequireWrite → 204); členství
  `POST /albums/{uid}/photos` `{photo_uids:[…]}` (přidá), `DELETE /albums/{uid}/photos`
  `{photo_uids:[…]}` (odebere) — obě vrací aktuální **chronologické** pořadí `{photo_uids:[…]}`,
  404 chybějící album/fotka. Ruční řazení alba neexistuje: `PATCH /albums/{uid}/order` byl
  odstraněn (→ 404) a album se vždy zobrazuje od nejstarší fotky (viz Photos API). **Štítky** `GET /labels`
  (RequireAuth) → `{labels:[{...label, photo_count}]}` (řazení priority DESC); `POST /labels`
  (RequireWrite) → 201 z `{name,priority?}` (prázdné jméno → 400); `GET /labels/{uid}`
  (RequireAuth, 404); `PATCH /labels/{uid}` (RequireWrite, name/priority); `DELETE /labels/{uid}`
  (RequireWrite → 204); připojení `POST /labels/{uid}/photos` `{photo_uid,source?,uncertainty?}`
  → 204 (neplatný source → 400), `DELETE /labels/{uid}/photos` `{photo_uid}` → 204. **Galerie
  fotek alba/štítku** jede přes sdílené `GET /photos?album={uid}`/`?label={uid}` (stejný tvar +
  filtry/stránkování; album scope má vždy vynucenou chronologii, štítek ctí zvolené řazení). Viewer čte, ale nemutuje (403).
  Každá mutace (create/update/delete alba i štítku, add/remove fotek, attach/detach) píše audit záznam
  (`album.*`/`label.*`) **ve stejné transakci** jako změna — odpovědi se nemění. Mountuje se dalším `server.WithAPI`
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
  `set_description`/`clear_description`, `set_location {lat,lng}`/`clear_location`,
  `archive`/`unarchive`, `set_favorite` (**per-user**), `set_rating` (0–5) / `set_flag`
  (none/pick/reject/eye) (**per-user**, neplatná hodnota → 400). Odpověď `{results:[{photo_uid,status,
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
  neplatné `z`/`x`/`y` → 400. Úspěšné dlaždice se **cachují i server-side** (bounded LRU +
  TTL, `maps.tile_cache_bytes`/`maps.tile_cache_ttl`) — hit neplatí kredit mapy.com a hlásí se
  hlavičkou `X-Tile-Cache: hit|miss`; **chyba se nikdy necachuje**. `GET /map/rgeocode?lat=&lng=` —
  reverse geocode → zjednodušené `{name,location,regional_structure}`, **cachované** (klíč =
  zaokrouhlená souřadnice) a **rate-limitované** (token-bucket, geocode = 4 kredity) → 429 přes
  limit, 404 bez shody. `GET /map/photos` — **GeoJSON FeatureCollection** geotagovaných fotek
  (souřadnice `[lng,lat]`), ctí filtry `taken_after`/`taken_before`/`album`/`label`/`archived`,
  feature nese `uid`/`title`/`taken_at`/`media_type`/relativní `thumb`. mapy.com chyby
  (**401/403 → 424** `mapsapi.StatusMapKeyRejected` = odmítnutý *náš* klíč, syrová 403 se
  neprosakuje ven — request volajícího je v pořádku; 404→404, 429→429, 5xx→502/503)
  **neprosakují klíč**; každý výsledek se zapisuje do `mapy.Health` (→ `GET /system/status`
  sekce `maps`). Bez `maps.mapy_api_key` vrací tile/rgeocode 503, GeoJSON funguje. Mountuje se
  `server.WithAPI` (`buildMapsAPI` v `cmd/kukatko/maps.go`).
- **Import API (`/api/v1`, `internal/importapi`, přes `RequireImport` = admin **nebo** ai):** triggery a
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
  400, sken selže → 500. Listing **jen čte**; při `duplicate.enabled=false` route `GET` odpovídá 503.
  `POST /duplicates/merge` (`internal/dupmerge`, `RequireWrite`) `{keeper_uid,member_uids[],dry_run?}` →
  `{keeper_uid,albums_added,labels_added,people_added,metadata_filled[],archived,dry_run}`: v **jedné
  transakci** sloučí zbylé kopie do zvoleného keepera — union alb, štítků a osob (subject↔keeper marker
  bez boxu, typ `label`), doplní chybějící skalární pole (title/description + per-user rating/favorite/
  flag; nikdy nepřepíše existující hodnotu), archivuje kopie (`archived_at`, originály do purge) a zapíše
  `photos.merge` do auditu. Idempotentní (opětovné spuštění na vyřešené skupině = no-op); `dry_run:true`
  jen spočítá náhled bez změn. Neplatná skupina → 400, neexistující keeper → 404, `merge=nil` → 503.
  Route `merge` běží i při vypnuté detekci. Mount vždy `buildDuplicatesAPI` (`cmd/kukatko/duplicates.go`).
- **System status API (`/api/v1`, `internal/systemapi` + `internal/system`, admin-only přes
  `RequireAdmin`):** `GET /system/status` → jeden agregovaný snapshot provozního zdraví:
  `{version,database{reachable,error?},embeddings{online,url},jobs{by_state,by_type,total,dead_letter,
  pending_embeddings},backup (=backup.Status),imports{photoprism,photosorter (=importer.Run|null)},
  storage{originals_bytes,cache_bytes,free_bytes,total_bytes},
  maps{configured,state,degraded,detail?,checked_at?}}`. `maps` = poslední pozorovaný stav mapy.com
  z proxy (`mapy.Health`, bez vlastního probu): `state` ∈ `unknown|ok|key_rejected|rate_limited|
  unavailable|error`, `degraded=true` u všech kromě `ok`/`unknown` — **odmítnutý klíč (403) je
  vidět tady**, ne až jako šedá mapa; `detail` je sanitizovaný (klíč nikdy).
  Sloučení existujících subsystémů
  (embeddings health, fronta jobů, backup stav, poslední import per zdroj přes
  `importer.Store.LatestRun`, využití disku, DB ping); úložiště memoizováno 30 s. Collect selže (DB
  pro fronту/importy) → 500; nedostupná DB/úložiště inline best-effort. Mountuje se **vždy**
  (`buildSystemAPI` v `cmd/kukatko/system.go`). Admin UI **Systém** (`/system`, `SystemStatusPage`)
  polluje po 5 s a nabízí rychlé akce (requeue dead-letter, trigger backup, odkazy na import/údržbu).
