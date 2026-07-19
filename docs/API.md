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
  `GET /auth/me`. Role: **striktní žebřík** viewer < editor < admin < maintainer (každá dědí práva
  nižších): viewer read-only, editor přidává zápis médií/metadat, admin governance (správa
  uživatelů, audit log, trvalé mazání/vyprázdnění koše), maintainer provoz (importy, maintenance,
  system, backup/restore, jobs, process). **Sliding session expiry**
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
  druhý permission systém, `RequireAuth`/`RequireWrite`/`RequireAdmin`/`RequireMaintainer` platí beze
  změny (např. token role maintainer projde všemi guardy; plain admin narazí na 403 u provozních
  `RequireMaintainer` surfaces). Špatný
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
  **`q` mluví vyhledávacím jazykem** (viz [Vyhledávací jazyk](#vyhledávací-jazyk-q) níže): volný
  text + `klíč:hodnota` filtry v jednom stringu — filtry zužují výsledek ve všech módech, na
  ranking volného textu se nesahá. Dotaz **jen z filtrů** (žádný volný text) běží po plain-list
  cestě (řazení dle data), odpověď hlásí `mode: "filter"` a **nikdy nevolá embedding sidecar**;
  `q` jen ze záporných termů (`-slovo`) se vynutí do `fulltext` (není co embedovat). Filtrům,
  kterým jazyk nerozuměl, se nic nestane (hledají se jako text) a odpověď je vrací v
  `unknown_tokens: []string` (i na `GET /photos`), aby UI umělo jemně napovědět;
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
  a **návrhy** identit **pro každý obličej** s embeddingem — u nepojmenovaného kandidáti na
  pojmenování, u přiřazeného **alternativy pro přeřazení** (osoba, kterou obličej už nese, i všichni
  ostatní lidé na fotce jsou z návrhů vyloučeni, takže přiřazený obličej bez blízké alternativy
  dostane prázdný seznam; rozšíření prahu bez cutoffu běží jen u nepojmenovaných). Face↔marker IoU
  matching, viz `internal/facematch`; 503 když face backend není zapojen;
  `POST /photos/{uid}/faces/assign` (editor/admin) — přiřazovací
  akce `{action, face_index?, marker_uid?, subject_uid?, subject_name?, bbox?}`
  (`create_marker`/`assign_person`/`unassign_person`), auto-create subjektu dle jména, drží `faces`
  cache + `marker.reviewed` konzistentní (400 validace, 404 chybějící foto/marker/subjekt);
  `GET /photos/{uid}` plný detail navíc nese **členství** `albums`/`labels` (inline chipy detailu,
  přes `PhotoOrganizer` rozhraní / `organize.Store.AlbumsForPhoto`+`LabelsForPhoto`; nil organizer →
  prázdná pole) a **`uploader`** `{uid,name}` — kdo fotku nahrál, jméno resolvnuté server-side přes
  `UserResolver` (`auth.Store.GetUserByUID`; `name` = `display_name`, fallback `username`); vynechán
  (`omitempty`) u fotek bez `uploaded_by` (importy z PhotoPrism/photo-sorteru) i když uživatele nelze
  resolvnout — resoluce je **jen na detailu**, list/search per-fotku uploadera neřeší (žádné N+1);
  a **`place`** `{country,region,city,place_name}` — **cachnuté** reverzní geokódování fotky z
  `photo_places` (plní ho background job `places`), čtené přes rozhraní `PlaceResolver`
  (`places.Store.GetPlace`). **Detail nikdy negeokóduje**: kredity mapy.com jsou měřené, takže
  otevření fotky nesmí stát kredit — on-demand lookup zůstává výhradně v `GET /maps/reverse`, které
  si vyžádá uživatel. Blok je `omitempty` a vynechá se u fotky, kterou job ještě nedošel, u fotky bez
  GPS a i u „processed" markeru (řádek se všemi úrovněmi prázdnými); jednotlivé úrovně můžou být
  prázdné, když geokodér nic přesnějšího neznal. Renderuje ho `TechnicalDetails` (skupina Poloha);
  **nedestruktivní edit** (`internal/photoedit` + `edit.go`/`media_edit.go`):
  `GET /photos/{uid}/edit` (přihlášený) → uložený `photos.Edit` (crop/rotace 0-90-180-270/jas/kontrast,
  neupravená fotka → neutrální edit) a `PUT /photos/{uid}/edit` (editor/admin) zapíše edit do
  `photo_edits` (validace bounds; originál se nikdy nemění — `GET …/download` ho **renderuje za běhu**
  přes `photoedit.Apply`, pokud caller nedá `?original=true`);
  `PATCH /photos/{uid}` (editor/admin) částečná úprava
  metadat — `title/description/notes/ai_note/taken_at/lat/lng` (null maže nullable, validace
  souřadnic) **+ přibližné datum** `taken_at_estimated` (bool — datum je odhad, ne fakt) a
  `taken_at_note` (volný text k datování, ořízne se whitespace, **max 500 znaků**, delší = 400).
  Poznámka platí jen u odhadu: jakmile je výsledný `taken_at_estimated` `false` (klient ho shodil,
  nebo ho fotka nikdy neměla), server `taken_at_note` **vymaže** — u data prezentovaného jako fakt
  nikdy nezůstane viset zastaralá poznámka (délka se přesto validuje první, takže příliš dlouhá
  poznámka se ohlásí, ne tiše zahodí). `taken_at` NULL + `taken_at_estimated` `true` je legální
  (význam nese poznámka) a na řazení/timeline/filtry nemá příznak žádný vliv
  **+ původ polohy** `location_source` (`exif`/`manual`/`estimate`/`""`, viz `internal/geoestimate`):
  v payloadu **read-only informace**, v PATCH jediná povolená hodnota `"manual"` a jen na fotce, která
  polohu má — tím se **přijme odhad** (povýší na uživatelovo rozhodnutí), aniž by se posílaly zpět
  souřadnice a zaokrouhlily na to, co klient vykreslil. Cokoli jiného = 400: `exif`/`estimate` píše
  server, klient si původ souřadnice, kterou sám zadal, vymyslet nesmí. **Jakýkoli dotyk `lat`/`lng`**
  (posun i smazání) sám o sobě zapíše `location_source: "manual"`; smazání tedy **nevrací** původ na
  prázdný jako u `taken_at` → `unknown` — `"manual"` bez souřadnic je záměrný **náhrobek** („uživatel
  rozhodl, že tahle fotka polohu nemá“), díky kterému backfill odhad, který uživatel zahodil, nikdy
  nevrátí zpět
  **+ IPTC/XMP kredity** `subject/artist/copyright/license/keywords/scan`: volný text,
  ořízne se whitespace, délkový strop (`subject`/`copyright`/`license` 1000, `keywords` 2000,
  `artist` 255 **znaků**, ne bajtů), delší = 400; `scan` je prostý bool. Strojově odvozená pole
  (`software`, `color_profile`, `image_codec`, `camera_serial`, `original_name`, `projection`) se
  **servírují, ale needitují** — dekodér je odmítne jako neznámý klíč (400), popisují soubor, ne
  uživatelův pohled na něj. **Odpověď má stejný tvar jako `GET /photos/{uid}`** — plný detail včetně `files`,
  `albums`, `labels`, `is_favorite` a `uploader` (sdílený `writeDetail` v `internal/photoapi`), ne
  holý `photos.Photo`: klient si detailem z odpovědi nahradí ten, co drží, takže chybějící pole by mu
  z detailu zmizela (dřív padal na `albums.map` z `undefined`). Klient posílá jen **skutečně
  změněná** pole: přeposlání nezměněného `taken_at` by přepnulo `taken_at_source` `exif` → `manual`,
  přeposlání nezměněných souřadnic by je zaokrouhlilo na 6 desetinných míst z textového pole.
  `ai_note` je volný text z externí AI klasifikace (píše ho i automat přes tuto routu),
  vrací se v detailu i listu jako součást `photos.Photo` a je zahrnutý ve fulltextu (§ Vyhledávání);
  stejně tak všechna IPTC/XMP i technická pole výše **a dvojice `taken_at_estimated`/`taken_at_note`**
  — jsou součástí `photos.Photo`, takže je nese
  **každá** odpověď s fotkou (detail, list, search), `subject` a `keywords` navíc padají do fulltextu
  (váha B, resp. C). `keywords` je původní IPTC hodnota **verbatim** (comma-separated), **nejsou to
  labely** — `/labels` zůstávají samostatná kurátorská taxonomie;
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
  `POST /photos/{uid}/purge` (**admin** přes `RequireAdmin`, `?confirm=true` jinak 400, 404 chybí,
  409 fotka není archivovaná → 204) a `POST /trash/empty` (**admin** přes `RequireAdmin`,
  `?confirm=true` → `{purged,failed}`) trvale a nevratně mažou archivované fotky, takže jsou
  zpřísněné z write na admin; archivace (vratné soft-delete) zůstává `RequireWrite` a
  `GET /trash/info` (přihlášený) vrací `{retention_days}` pro odpočet
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
  **Stacky** (`internal/photoapi/stacks.go`, `Stacker` rozhraní = `stacks.Service`, **nil → 503**):
  `POST /photos/stack` (editor/admin) tělo `{photo_uids:["…","…"]}` ručně seskupí výběr (**≥ 2**),
  primárního člena vybere podle pravidla a vrátí **detail nového primárního** — 400 (< 2 fotky),
  404 (fotka chybí/archivovaná), 503 (vypnuto); `POST /photos/{uid}/stack/primary` (editor/admin) udělá
  `{uid}` primárním svého stacku → refreshnutý detail `{uid}` (404 chybí, 409 není ve stacku, 503);
  `POST /photos/{uid}/unstack` (editor/admin) vyjme `{uid}` ze stacku (osamostatní se; dvoučlenný stack
  se tím rozpadne, stack, který přijde o primárního, si nového zvolí) → refreshnutý detail (409 když
  není ve stacku); `POST /photos/{uid}/unstack-all` (editor/admin) rozpustí celý stack, do kterého
  `{uid}` patří → refreshnutý detail. **Pole v odpovědích:** každá fotka v list/search/detail může nést
  `stack_uid` (string) a `stack_count` (int; **≥ 2 jen u stacknutého primárního**, jinak vynecháno —
  pohání badge dlaždice); detail (`GET /photos/{uid}`) navíc `stack_members` — pole (primary první)
  `{uid, file_name, media_type, file_mime, file_width, file_height, file_size, is_primary, thumb_url,
  download_url}` (pruh variant), u nestacknuté fotky vynecháno (odlišné od `files`, což jsou
  `photo_files` jednoho řádku).
  Mountuje se třetím `server.WithAPI` (`buildPhotoAPI` v `cmd/kukatko/photos.go`).
- **Jobs API (`/api/v1`, `internal/jobsapi`, maintainer-only přes `RequireMaintainer`):**
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
  `GET /subjects/{uid}/outliers` → `{subject_uid,count,meaningful,avg_distance,no_embedding,
  faces:[{photo_uid,face_index,bbox,det_score,distance,marker_uid?,width,height,orientation}]}`
  (obličeje osoby seřazené sestupně dle kosinové vzdálenosti od **trimovaného** centroidu jejích
  embeddingů — nejpravděpodobněji špatně přiřazené první); 1–2 obličeje → `meaningful:false`;
  špatný obličej se odpojí přes existující `POST /photos/{uid}/faces/assign` (`unassign_person`),
  tahle vrstva nemutuje; 503 bez backendu, 404 chybějící subjekt.
  **Volitelné query parametry** `threshold` (minimální kosinová vzdálenost od centroidu, 0–2,
  default **0 = vrať vše**) a `limit` (max. počet obličejů, default **0 = všechny**) zužují seznam,
  aby stránka nemusela tahat všechny obličeje dobře otagovaného člověka; nečíselný, záporný nebo
  `threshold > 2` → 400. Historické chování („vše, seřazené") zůstává defaultem.
  `count`/`meaningful`/`avg_distance` popisují **celou oskórovanou množinu** (před filtrem), takže
  statistiky nelžou, když práh seznam zúží; `no_embedding` je počet přiřazení **bez embeddingu**,
  která zkontrolovat nejde (obličej rozpoznaný, když byl sidecar offline) a v `faces` **nejsou** —
  klient je má přiznat, ne tiše vynechat. **Obličeje potvrzené uživatelem** (viz Feedback API níže)
  jsou z výsledku vyloučené, takže opakované průchody konvergují místo dokola nabízených planých
  poplachů. Mountuje se `server.WithAPI` (`buildOutlierAPI` v `cmd/kukatko/outliers.go`).
- **Candidates API (`/api/v1`, `internal/candidatesapi`, editor/admin přes `RequireWrite`):**
  „najdi osobu mezi neotagovanými fotkami". `POST /subjects/{uid}/candidates` s **volitelným** tělem
  `{threshold?,limit?}` (`threshold` = max kosinová vzdálenost, default `candidates.max_distance`;
  `limit` 0 = vše; `DisallowUnknownFields` + 64 KiB, záporné hodnoty → 400) →
  `{subject_uid,source_photo_count,source_face_count,faces_without_embedding,min_match_count,threshold,
  reason?,counts:{create_marker,assign_person,already_done},candidates:[{photo,face_index,
  bbox:{relative:[x,y,w,h],pixel:[x,y,w,h]},distance,match_count,action,marker_uid?}]}`. Pro subjekt najde
  **nepřiřazené** obličeje, které se podobají jeho vlastním otagovaným (kNN per exemplár nad
  `subject_uid IS NULL` + hlasování; `min_match_count` je vote rule škálovaný počtem exemplárů a
  prahem, clamp 1..5, vrací se, aby UI filtr vysvětlilo). Vypadnou už zamítnuté obličeje
  (`internal/feedback`) i ty, co tripnou pravidlo negativního exempláře, a moc malé obličeje
  (relativní `faces.min_face_size` + absolutní `candidates.min_face_px`). `action` říká, co
  potvrzení udělá (`create_marker`/`assign_person`/`already_done`) — **potvrzuje se přes existující**
  `POST /photos/{uid}/faces/assign`, tahle vrstva **nemutuje**. `marker_uid` je vyplněný, když
  obličej už překrývá marker (`assign_person`/`already_done`), aby UI umělo poslat správný assign
  (present → `assign_person` nad tím markerem, prázdný → `create_marker`). `bbox` je relativní 0..1 **i** pixely
  (ctí EXIF orientaci). Prázdný **non-error** výsledek s `reason` `"no_faces"` (subjekt bez obličejů)
  nebo `"no_embeddings"` (otagovaný, ale obličeje bez embeddingu — box byl offline); box offline
  jinak nevadí (čte vektory už v DB). 503 bez backendu, 404 chybějící subjekt. Mountuje se
  `server.WithAPI` (`buildCandidatesAPI` v `cmd/kukatko/candidates.go`).
- **Recognition sweep API (`/api/v1`, `internal/sweepapi`, editor/admin přes `RequireWrite`):**
  „projdi všechny pojmenované osoby a najdi jisté shody mezi neoznačenými obličeji" — server-side
  fan-out přes **kandidátské hledání** (`internal/candidates`) nad všemi subjekty, ne client-side.
  `GET /faces/sweep?confidence=<percent-or-distance>&limit=<per-person>`. `confidence`: hodnota
  `>1` (max 100) je **procento jistoty** → mapuje se na kosinovou vzdálenost `1 - percent/100`
  (floor `0.01`), hodnota `≤1` je **přímá vzdálenost**, prázdné = default 75 % (0.25); záporné /
  `>100` / nečíselné → 400. `limit` = strop kandidátů na osobu (0 = vše; záporné → 400). Iteruje
  subjekty s `marker_count > 0` (tj. mají obličej), scan každého běží s **vysokou jistotou** (těsná
  vzdálenost) a **omezenou souběžností** (worker pool, `sweep.concurrency`); počet subjektů je
  zastropován (`sweep.max_subjects`), přesah je **viditelný** (`capped`), ne tiše zahozený. Odpověď
  je **stream NDJSON** (`application/x-ndjson`), řádek = jedna JSON zpráva `{type,...}`: `progress`
  `{scanned,total,name}` po každém dokončeném subjektu (hýbe pruhem), `person`
  `{subject,candidates,counts,actionable}` jen pro subjekty s **akceschopnými** kandidáty (`candidates`
  ve stejném tvaru jako per-subject endpoint; `already_done` se z pracovního seznamu **vyfiltruje**),
  a jeden závěrečný `summary` `{people_scanned,people_with_matches,total_actionable,total_already_done,
  capped,subjects_total}`. Subjekt s **nula** akceschopnými kandidáty se do seznamu vůbec nedostane;
  subjekt bez obličejů se **přeskočí** (ne error); chyba scanu jednoho subjektu se zaloguje a přeskočí,
  celý sweep nepadá. **Nikdy neautoconfirmuje** — jistota jen zužuje seznam, každé potvrzení jde pořád
  přes `POST /photos/{uid}/faces/assign`, zamítnutí přes `POST /feedback/face-rejections`. Chyba
  **před** prvním řádkem (výpis subjektů selhal) → čisté 500 JSON; chyba **uprostřed** streamu (klient
  odpojen) už jen zaloguje (200 je odeslané). 503 bez backendu. Mountuje se `server.WithAPI`
  (`buildSweepAPI` v `cmd/kukatko/sweep.go`), sdílí `candidates.Service` s candidates endpointem.
- **Expand-a-collection API (`/api/v1`, `internal/expandapi`, editor/admin přes `RequireWrite`):**
  „najdi fotky podobné celému albu / štítku" — dotažení polotagované sbírky. `GET /albums/{uid}/similar`
  a `GET /labels/{uid}/similar` s query `?threshold=&limit=` (`threshold` = max kosinová vzdálenost,
  default `expand.max_distance` = 0.30, tj. 70 % podobnost; `limit` default `expand.limit`, strop
  `expand.max_limit`; nečíselné / záporné → 400). Členství se řeší **nativně** (`internal/organize`),
  **žádné volání PhotoPrism**. Odpověď `{kind,collection_uid,source_photo_count,source_photos_sampled,
  source_photos_with_embedding,source_capped,source_cap,min_match_count,threshold,limit,result_count,
  reason?,candidates:[{photo,distance,similarity,match_count}]}`. Algoritmus: **per-foto kNN + hlasování**
  (ne průměr embeddingů sbírky — sbírka není jeden vizuální koncept); `match_count` = kolik zdrojových
  fotek kandidáta vrátilo, `distance` = **minimum** přes ně. Vypadnou fotky **už ve sbírce** (to je celý
  smysl), pod `min_match_count` (vote rule škálovaný počtem zdrojů a prahem, clamp 1..5, vrací se pro
  UI), zamítnuté pro daný štítek (`internal/feedback`) a ty, co tripnou pravidlo negativního exempláře.
  **Albumy nemají model zamítnutí** — rejection/negative-exemplar filtry platí jen pro štítky (asymetrie,
  ne opomenutí). Řazení `match_count` DESC, pak `distance` ASC (shoda víc zdrojů poráží jednu silnou),
  truncate na `limit`. Fotky **s** embeddingem se **počítají a hlásí** (box bývá offline → sbírka může
  být napůl embedovaná a výsledky tenké). Obří album se **navzorkuje** (deterministicky, rovnoměrně přes
  členy) na `expand.source_cap` a **cap se hlásí** (`source_capped`), ne tiše. Prázdné album/štítek nebo
  sbírka bez embeddingů → **non-error** prázdný výsledek s `reason` `"empty_collection"` /
  `"no_source_embeddings"`. Sbírka o **jedné** fotce degeneruje na per-foto podobnost. **Read-only** —
  přidání nalezených fotek jde přes existující `POST /photos/bulk`. 503 bez backendu, 404 chybějící
  album/štítek. Mountuje se `server.WithAPI` (`buildExpandAPI` v `cmd/kukatko/expand.go`).
- **MCP server (`POST /api/v1/mcp`, `internal/mcpapi`, přes `RequireAuth` + RBAC per tool):** knihovna
  vystavená **AI agentovi** přes **Model Context Protocol** — hledá, čte, organizuje („najdi všechny
  fotky babičky ze šedesátých a dej je do alba"). Transport **Streamable HTTP, stateless**, odpověď
  `application/json` (ne SSE), tělo je JSON-RPC 2.0 (`initialize`, `tools/list`, `tools/call`, `ping`);
  klient musí poslat `Content-Type: application/json` a `Accept: application/json, text/event-stream`.
  Knihovna `github.com/modelcontextprotocol/go-sdk` (čisté Go, drží `CGO_ENABLED=0`); DNS-rebinding
  guard SDK je **vypnutý**, protože odmítá i legitimní request z reverzní proxy a endpoint je
  autentizovaný. **Vypnuto by default** (`mcp.enabled: false`) — a při `false` se route **vůbec
  nemountuje** (`RegisterRoutes` neregistruje nic), takže cesta **neexistuje**, ne že by vracela 403;
  v celém binárce pak spadne do SPA catch-all a vrátí `index.html` jako každá neznámá cesta (v access
  logu chybí `"route":"/api/v1/mcp"`). **Volá service layer in-process**, ne vlastní HTTP API, takže si drží
  transakční hranice. **Auth: žádný nový mechanismus** — `RequireAuth` jako všude, agent posílá
  `Authorization: Bearer kkt_…`, roli má **vlastník tokenu** (`viewer` = jen čtení; `editor`/`admin`/`ai`
  = i zápis). Hranice je **dvojitá**: write tooly se read-only volajícímu **vůbec neregistrují** (nevidí
  je v `tools/list` — staví se dva servery a `getServer` vybírá podle principala) **a** každý write
  handler roli znovu ověří. Tooly — čtení: `search_photos` (volný text + **vyhledávací jazyk** +
  scope `album_uid`/`label_uid`/`person_uid` + `sort`/`order`/`limit`/`offset`), `get_photo`,
  `find_similar_photos`, `list_albums`/`get_album`, `list_labels`/`get_label`,
  `list_subjects`/`get_subject`, `library_stats`; zápis: `create_album`, `add_photos_to_album`,
  `remove_photos_from_album`, `create_label`, `attach_label`, `detach_label`, `set_photo_metadata`,
  `set_photo_rating`, `bulk_edit_photos`. Fotky alba/štítku/osoby se čtou přes `search_photos` se
  scope — je to tentýž list path, takže platí i ostatní filtry a stránkování. **Tvar odpovědí je
  kompaktní**: seznamy vrací jen `{uid,title,taken_at,media_type,thumb_url}` + `total`/`offset`/
  **`remaining`**, **syrový `exif` blob nevrací žádný tool** (kontext agenta je ten vzácný zdroj).
  **Každá mutace píše audit řádek ve své transakci** s `"via": "mcp"`. **Nic destruktivního není
  vystavené** — žádné mazání, purge, koš, **archivace** (archivace = cesta do koše, který se purguje
  podle retention), restore, backup, správa uživatelů ani admin povrch; `bulk_edit_photos` proto
  vynechává i `Archive` a `Location`, které bulk service jinak umí. Mountuje se `server.WithAPI`
  (`buildMCPAPI` v `cmd/kukatko/mcp.go`). Detailně: [`docs/MCP.md`](MCP.md).
- **Review game API (`/api/v1`, `internal/reviewapi`, editor/admin přes `RequireWrite`):** „hra" na
  úklid knihovny — jedna otázka po druhé („Je tohle Tomáš?", „Má tahle fotka mít štítek Ostatky?"),
  odpověď yes/no/skip. Otázky se berou z **pásma nejistoty** (`review.band_min ≤ confidence <
  review.band_max`, confidence = 1 − kosinová vzdálenost, default 0.45–0.75) — pod pásmem je to šum,
  nad ním se potvrzuje hromadně na `/recognition` resp. přes expand. `GET /review/queue?limit=N`
  (prázdné/0 → `review.queue_size`, strop 100, nečíselné/záporné → 400) → `{questions:[{id,kind:
  "face"|"label",confidence,photo,subject?,face_index?,bbox?{relative,pixel},action?
  ("create_marker"|"assign_person"),marker_uid?,label?}],answered,remaining,reason?}`; `id` je
  **stabilní, odvozené z obsahu** (`face:<photo>:<index>:<subject>` / `label:<photo>:<label>`),
  `bbox` relativní 0..1 **i** pixely (ctí EXIF orientaci), fronta je **deterministická** pro daný stav
  knihovny (řazení dle vzdálenosti od středu pásma, tie-break id; face/label otázky se **prokládají**
  proporčně, žádný `rand`). Fronta se **cachuje per user** (`review.cache_ttl`, default 60 s) — batch
  fetch nepřepočítává drahá vektorová hledání; `remaining`/`answered` jsou levné session čítače.
  Prázdná knihovna (žádné pojmenované osoby ani štítky) → **non-error** prázdná fronta s `reason:
  "no_people_no_labels"`; zdroje existují, ale pásmo je prázdné → `reason:"no_candidates"`.
  `POST /review/answer` s `{question_id,answer:"yes"|"no"|"skip"}` → `{result,answered,remaining}`.
  **yes** na face jde přes **existující** assign state machine (stejná cesta jako
  `POST /photos/{uid}/faces/assign`; akce se odvodí z aktuálního stavu obličeje — marker existuje →
  `assign_person`, jinak `create_marker` s uloženým bboxem), yes na label přes `AttachLabelAudited`
  (source `manual`); **no** zapíše **trvalé zamítnutí** do `internal/feedback` (otázka se už nikdy
  nevrátí a pravidlo negativního exempláře zabíjí podobné kandidáty); **skip** = „nevím" — otázka se
  v této session už nenabídne, ale **nezapisuje se** (restart ji smí vrátit; skip není zamítnutí).
  Odpovědi jsou **idempotentní** (`result:"already_answered"`, žádný druhý zápis ani duplicitní
  marker) a auditované ve stejné transakci jako mutace (přes reusnuté write paths). Smazané
  foto/obličej/štítek mezi fetch a answer → 200 s `result:"gone"` (UI jde dál), **ne** 500; nevalidní
  `question_id`/`answer` → 400. Mountuje se `server.WithAPI` (`buildReviewAPI` v
  `cmd/kukatko/review.go`, sdílí facematch service s photoapi a candidates/expand služby se sweep a
  expand endpointy).
  **Leaderboard** `GET /review/leaderboard?window=all|7d|today` (default `all`, jiná hodnota → 400)
  gate **`RequireAuth`** — vrací jen agregované počty + jména, takže ho vidí **každý přihlášený**
  (i viewer), ne jen editor. Žebříčkuje hráče podle počtu **rozhodnutí** v review hře, zdrojem jsou
  durable audit řádky s `details.via = "review"`: **yes** = `face.assign` + `label.attach`, **no** =
  `face.reject` + `label.reject`; **skip** nic nezapisuje, takže se z principu nepočítá. Kvůli tomu
  review potvrzení obličeje (`face.assign`) nově nese `via:review` (dosud jako jediná ze čtyř akcí
  chybělo — jde přes facematch `Service.Apply`, který si audit skládá sám; běžné assignmenty zůstávají
  neoznačené). Odpověď `{window,caller_uid,entries:[{user_uid,display_name,yes_count,no_count,total,
  is_me}]}` je řazená (total desc → yes desc → display_name), jen uživatelé s ≥1 rozhodnutím v okně
  (nula = chybí), NULL actor (smazaný uživatel) se vynechává, `is_me`/`caller_uid` označí vlastní
  řádek. Okna se počítají z `created_at` (7 d = klouzavých 7×24 h, dnes = půlnoc dne). Obsluhuje
  `review.LeaderboardStore` nad sdíleným poolem; parciální index `idx_audit_log_review_actor`
  (migrace `0037`) drží scan levný.
- **People/Subjects API (`/api/v1`, `internal/peopleapi`):** `GET /subjects` (RequireAuth) →
  `{subjects:[{...subject, marker_count, cover_face?}]}` (řazení dle jména, počty non-invalid
  markerů). `cover_face` = `{photo_uid,x,y,w,h,width,height,orientation}` — obličej, kterým se
  subjekt ilustruje v mřížce lidí, když **nemá** `cover_photo_uid`; chybí, když subjekt nemá
  použitelný marker. Vybírá ho `listSubjectsSQL` (největší box, pak `score`, pak `uid`; jen
  `type='face'`, non-invalid, na viditelné fotce). `width`/`height`/`orientation` jsou uložený rám
  fotky — klient si výřez ořezává sám z cache thumbnailu (endpoint na face thumbnail neexistuje) a
  bez rámu by ho zdeformoval. **Explicitní `cover_photo_uid` vždy vyhrává**, `cover_face` je jen
  fallback;
  `POST /subjects` (RequireWrite) → 201 vytvoří subjekt z `{name,type,favorite,private,notes,
  cover_photo_uid?}` (prázdné jméno / neznámý typ → 400); `GET /subjects/{uid}` (RequireAuth) →
  subjekt (404); `PATCH /subjects/{uid}` (RequireWrite) → editace stejných polí (404/400);
  `DELETE /subjects/{uid}` (RequireWrite) → 204 (markery se odpojí server-side); `GET
  /subjects/{uid}/photos` (RequireAuth) → paginovaná galerie fotek subjektu
  `{photos,total,limit,offset,next_offset}` (newest-first, jen nearchivované, `limit`≤500). Mountuje
  se `server.WithAPI` (`buildPeopleAPI` v `cmd/kukatko/people.go`). Záznamy fotek subjektu
  staví na `people.Store.ListPhotoUIDsBySubject` (distinct non-invalid markery → photo uid).
- **Process API (`/api/v1`, `internal/processapi`, maintainer-only přes `RequireMaintainer`):**
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
  `POST /process/metadata` → `{enqueued}` (backfill `metadata` pro fotky, jejichž **soubor nikdy
  nebyl přečten** do IPTC/XMP a file-technical sloupců, přes `metajob.BackfillMetadata`; „nepřečtený“
  = `photos.metadata_extracted_at IS NULL`, což jsou řádky z PhotoPrism importu, photo-sorter migrace
  a všechno nahrané před extrakcí). Volitelné `?all=true` naplánuje **každou nearchivovanou fotku**
  (vynucené znovu-přečtení celé knihovny — tak se doženou pole, která se nový extraktor naučil číst).
  Job je čistý **gap-filler**: doplní jen sloupce, které jsou pořád prázdné, takže prázdná extrakce
  nikdy nepřepíše hodnotu, kterou napsal uživatel, a `taken_at`/GPS/titulků/kurátorských dat se
  vůbec nedotkne. Chybějící originál se **zaloguje a přeskočí** (běh nepadá).
  `POST /process/sidecars` → `{enqueued}` (backfill `sidecar` pro fotky, jejichž **metadatový
  sidecar chybí nebo je zastaralý**, přes `sidecarjob.BackfillSidecars`; „chybí/zastaralý“ =
  `photos.sidecar_written_at IS NULL OR sidecar_written_at < updated_at`). Sidecar je YAML soubor
  vedle originálů ve storage (`sidecars/<klíč originálu>.yml`) s metadaty a kurátorskými daty fotky
  — existuje, aby knihovna přežila ztrátu databáze; formát celý v `docs/RESTORE.md`. Volitelné
  `?all=true` naplánuje **každou nearchivovanou fotku** (vynucený úplný re-run — tak se doženou
  kurátorská data, která se změnila **bez** dotyku řádku fotky: členství v albu, štítek, a proto
  nevypadají zastarale). Endpoint jen **zařadí** joby, soubory zapisuje worker; běh je idempotentní
  (nad knihovnou s aktuálními sidecary naplánuje nulu) a přerušený běh se dožene. **503** když
  `sidecar.enabled: false`. CLI protějšek: `kukatko sidecar backfill [--all]`.
  `POST /process/stacks` → `{created}` (detekce a seskupení fotek do stacků nad celou knihovnou přes
  `stacks.Service.DetectStacks`; **synchronní**, kandidáty jsou **jen dosud nestacknuté nearchivované**
  fotky, takže re-run je idempotentní a nerozbije ruční ani existující stack; **503** když
  `stacks.enabled: false`).
  `POST /process/locations` → `{estimated}` (odhad polohy pro fotky bez GPS z fotek pořízených blízko
  v čase, přes `geoestimate.BackfillLocations`; **synchronní**, **503** když
  `location_estimate.enabled: false`). Kandidáti jsou jen fotky **bez souřadnic** s prázdným
  `location_source`, se **známým a neodhadnutým** `taken_at`, které nejsou sken ani archivované.
  Sousedy jsou fotky se **změřenou** polohou (`location_source <> 'estimate'`) v okně
  ±`location_estimate.window`; odhad vznikne **jen když jsou soudržné** — všechny do
  `location_estimate.radius_meters` od svého těžiště. Jinak se **nevytvoří nic**: den mezi Prahou a
  Vídní žádnou poctivou odpověď nemá. Zapsaná poloha je vždy označená `location_source: "estimate"` a
  fotka dostane `places` job, takže se odhad propíše do hierarchie míst (geokód sám je metrovaný a jede
  přes stávající `maps.geocode_rate_per_sec` limiter). Re-run je idempotentní: odhadnutá fotka
  přestává být kandidát a **uživatelem smazaný odhad se nikdy nevrátí** (smazání zapíše
  `location_source: "manual"` bez souřadnic — náhrobek, ne mezera).
  Náhledy i metadata se počítají **lokálně**, takže backfill funguje i když je box offline; fronta jobů
  deduplikuje, takže opakované spuštění je idempotentní. Mountuje se `server.WithAPI` (`buildJobs`).
- **Albums & Labels API (`/api/v1`, `internal/organizeapi`):** **alba** `GET /albums`
  (RequireAuth) → `{albums:[{...album, photo_count, cover_uid?, taken_from?, taken_to?}]}`
  (`organize.AlbumSummary`): `cover_uid` je **efektivní obálka** — ručně zvolené
  `cover_photo_uid`, jinak **nejnovější živá fotka alba** (deterministicky: `taken_at DESC NULLS
  LAST, uid`); `taken_from`/`taken_to` je **rozsah `taken_at`** přes fotky alba. Obojí agreguje
  jediný SQL dotaz (LEFT JOIN + LATERAL, bez migrace) a počítá **jen s živými fotkami** —
  archivovaná fotka se započítá do `photo_count`, ale obálku nedodá ani rozsah neposune. Chybí,
  když album nemá co ukázat / žádná fotka nemá známý `taken_at`. **Pořadí seznamu** je vždy
  **od nejnovějšího alba**: řadí se podle **nejnovější živé fotky alba** (`MAX(taken_at) DESC
  NULLS LAST`, `uid` jako tiebreak pro totální a stabilní pořadí). Alba, kterým se nedá přiřadit
  datum — žádná fotka nemá `taken_at`, nebo je album prázdné — jsou **na konci**; archivovaná
  fotka pořadí neovlivní. Řazení **není volba uživatele**: endpoint nemá žádný `sort`/`order`
  parametr a frontend pořadí serveru nemění. `POST /albums`
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
- **Feedback / Rejections API (`/api/v1`, `internal/feedbackapi`):** persistovaný feedback —
  uživatelské „ne" (a nově i „ano") k odhadu obličej↔subjekt nebo fotka↔label, a jeho vzetí zpět.
  **Feedback je názor — nikdy nemutuje** podkladová data (neodpojí marker, neodebere label, nic
  nearchivuje). Osm endpointů, všechny **RequireWrite** (editor/admin, viewer 403): `POST /feedback/face-rejections`
  `{photo_uid,face_index,subject_uid}` → 204 (zamítne „tento obličej NENÍ tato osoba"),
  `DELETE /feedback/face-rejections` (stejné tělo) → 204 (vezme zpět); `POST /feedback/label-rejections`
  `{photo_uid,label_uid}` → 204 (zamítne „tato fotka NEMÁ mít tento label"),
  `DELETE /feedback/label-rejections` (stejné tělo) → 204 (vezme zpět) — i DELETE nese tělo (jako
  label-detach); `POST /feedback/face-confirmations` `{photo_uid,face_index,subject_uid}` → 204
  a `DELETE /feedback/face-confirmations` (stejné tělo) → 204;
  `POST /feedback/duplicate-dismissals` `{photo_uid,other_uid}` → 204 („tyhle dvě fotky NEJSOU
  duplikáty") a `DELETE /feedback/duplicate-dismissals` (stejné tělo) → 204 (vezme zpět).
  Dvojice je **neuspořádaná** — backend ji normalizuje (menší uid první), takže na pořadí uid
  nezáleží a `(A,B)` i `(B,A)` je jedno rozhodnutí; obě fotky stejné → 400 (`ErrSamePhoto`),
  neexistující fotka → 404. `GET /duplicates` zamítnuté dvojice **zahodí jako hrany** grafu
  ještě před sestavením komponent (`internal/duplicates`), takže dvoučlenná skupina zmizí
  natrvalo, kdežto větší skupina přežije na zbývajících hranách — zamítnutí „A není B" není
  tvrzení o C. Bez toho by se stejná dvojice nabízela donekonečna: detekce se počítá při každém
  volání znovu, názor v odpovědi nepřežije.
  **Pozor na polaritu:** confirmation je **opak** rejection — říká „tenhle obličej **JE** tahle
  osoba, přiřazení je správné". Slouží outlier review (✗ = „ne, fakt je to on"): potvrzený obličej
  `internal/outliers` z dalších výsledků vyloučí, takže se stejný planý poplach nenabízí dokola.
  Zaměnit ji za `face-rejections` znamená zapsat pravý opak toho, co uživatel řekl.
  Tabulka `face_confirmations` (migrace `0032`) má přirozený `UNIQUE (photo_uid, face_index,
  subject_uid)` a FK s `ON DELETE CASCADE` na fotku i subjekt (`confirmed_by` → `SET NULL`).
  **Idempotentní**: dvojí POST i DELETE něčeho, co nebylo zamítnuto/potvrzeno, vrací 204.
  Body `DisallowUnknownFields` + 64 KiB; chybějící `photo_uid`/`subject_uid`/`label_uid` nebo záporný
  `face_index` → 400; neexistující fotka/subjekt/label → 404 (`ErrTargetNotFound`). Každá mutace píše
  audit záznam **ve stejné transakci** jako zápis (akce `face.reject`/`face.unreject`/`label.reject`/
  `label.unreject`/`face.confirm`/`face.unconfirm`; aktor = `rejected_by`/`confirmed_by`).
  Mountuje se dalším `server.WithAPI` (`buildFeedbackAPI` v
  `cmd/kukatko/feedback.go`). Konzumenti (hledání osoby mezi neotagovanými, recognition sweep, review hra)
  přijdou v dalších taskách.
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
  limit, 404 bez shody. `GET /map/geocode?q=&limit=` — **forward** geocode (název → souřadnice)
  pro editor polohy → `{items:[{name,label,type,location,lat,lng}]}` seřazené od nejlepší shody
  (`label` = lokalizovaný druh místa „Město“/„Zámek“, `type` = strojové `regional.municipality`/
  `poi`/…, `location` = co místo obsahuje, kvůli rozlišení několika *Veselí*). Prázdné/dlouhé `q`
  (>200 znaků) → 400 **před** voláním nahoru, `limit` se **ořízne** na 1–15 (default 5), ne 400.
  **Bez shody = `items: []` a 200**, ne 404 (i když mapy.com odpoví 404) — nedopsaný název je
  normální stav našeptávače, ne chyba. Sdílí cache i rate-limiter s `rgeocode` (jeden kreditový
  rozpočet = jeden limiter); klíč cache = casefoldnutý dotaz + `limit`, **diakritika se
  zachovává** (`veseli` a `veselí` jsou nahoře různé dotazy). `GET /map/photos` — **GeoJSON FeatureCollection** geotagovaných fotek
  (souřadnice `[lng,lat]`), ctí filtry `taken_after`/`taken_before`/`album`/`label`/`archived`,
  feature nese `uid`/`title`/`taken_at`/`media_type`/relativní `thumb` a u odhadnuté polohy
  `location_estimated: true` (jinak se klíč **vůbec neposílá**). Odhadnuté fotky jsou ve feedu
  **defaultně** — od toho odhad je — ale špendlík, který vypadá stejně jako změřený, je tichá lež, tak
  je klient kreslí **jiným tvarem** (čárkovaný, ne jen jiná barva) + `title`. mapy.com chyby
  (**401/403 → 424** `mapsapi.StatusMapKeyRejected` = odmítnutý *náš* klíč, syrová 403 se
  neprosakuje ven — request volajícího je v pořádku; 404→404, 429→429, 5xx→502/503)
  **neprosakují klíč**; každý výsledek se zapisuje do `mapy.Health` (→ `GET /system/status`
  sekce `maps`). Bez `maps.mapy_api_key` vrací tile/rgeocode/geocode 503 (editor polohy to ukáže
  jako „vyhledávání míst není dostupné“ a jede dál na souřadnicích a kliku do mapy), GeoJSON
  funguje. Mountuje se `server.WithAPI` (`buildMapsAPI` v `cmd/kukatko/maps.go`).
- **Import API (`/api/v1`, `internal/importapi`, maintainer-only přes `RequireMaintainer`):** triggery a
  historie read-only importů. `GET /import/runs` (**vždy registrovaný**) → `{runs,limit,offset,
  sources:{photoprism,photosorter}}` — stránka `import_runs` newest-started-first (query
  `limit`≤200/`offset`, neplatný → 400) + `sources` flagy jaké zdroje jsou nakonfigurované (podklad
  admin Import UI: zapnutí/vypnutí sekcí). Historie nese i běhy zdroje **`folder`**
  (`kukatko import dir`, `internal/dirimport`) — ty se spouštějí **jen z CLI** (čtou adresář na disku
  serveru), takže nemají trigger endpoint ani flag v `sources`, ale v `runs` se objeví jako každý jiný běh. `POST /import/photoprism` → `pp_import` a
  `POST /import/photosorter` → `ps_migrate` (jen pro nakonfigurované zdroje, jinak 404) zařadí jeden
  singleton job → 202 `{job_id,status}`; `jobs.ErrDuplicate` (už běží) → 409, jiná chyba → 500.
  Celá API se mountuje vždy (`buildImportAPI` v `cmd/kukatko/import.go`), aby historie fungovala i
  bez konfigurovaného zdroje. Frontend (`ImportPage`) polluje `GET /import/runs` + `GET /jobs/stats`.
- **Backup API (`/api/v1`, `internal/backupapi`, maintainer-only přes `RequireMaintainer`):** stav a trigger
  S3 zálohy. `GET /backup` → stav + poslední běh (`{configured,running,last_started_at,
  last_finished_at,last_error,last_result}`; bez konfigurace `configured:false`); `POST /backup`
  spustí zálohu na **pozadí** (`Trigger`) → 202 `{status:"started"}`, `backup.ErrAlreadyRunning` →
  409, bez konfigurace → 503. Celá API se mountuje **vždy** (`buildBackupAPI` v
  `cmd/kukatko/backup.go`); plánovač (`backup.schedule`) a CLI `kukatko backup` sdílí stejný
  `backup.Service`. Konfig klíče `backup.s3.{endpoint,region,bucket,access_key,secret_key,
  path_style}`, `backup.schedule` (cron), `backup.retention` (kolik posledních dumpů nechat; ≤ 0 =
  vše). Runtime dep `pg_dump` (`postgresql-client`). Tajemství (`access_key`/`secret_key`) přes env.
- **Restore API (`/api/v1`, `internal/restoreapi`, maintainer-only přes `RequireMaintainer`):** **jen
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
  stránkováním `?limit=`(≤500)/`?offset=`; neplatný čas/číslo → 400. Navíc **filtry pro admin
  přehled rozhodnutí jednoho uživatele v review hře**: `?via=review` (jen review rozhodnutí —
  `details.via='review'`, tj. akce `face.assign`/`label.attach`/`face.reject`/`label.reject`;
  literál sedí na partial index z migrace 0037) a `?decision=yes|no` (kbelík Ano = assign+attach /
  Ne = reject); jiná hodnota `via`/`decision` → 400. Audit záznamy se **nezapisují
  přes HTTP** — vznikají uvnitř mutačních transakcí (in-tx `audit.Write`, viz `internal/audit`
  konvence). Mountuje se vždy (`buildAuditAPI` v `cmd/kukatko/audit.go`).
- **Maintenance API (`/api/v1`, `internal/maintenanceapi`, maintainer-only přes `RequireMaintainer`):**
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
- **System status API (`/api/v1`, `internal/systemapi` + `internal/system`, maintainer-only přes
  `RequireMaintainer`):** `GET /system/status` → jeden agregovaný snapshot provozního zdraví:
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
- **Capabilities API (`/api/v1`, `internal/capabilitiesapi`, přihlášený přes `RequireAuth`):**
  `GET /capabilities` → `{semantic_search:bool}` — malý objekt instančních feature-flagů, který smí
  číst **každý přihlášený** (na rozdíl od maintainer-only `/system/status`). `semantic_search` je
  **cache-ovaný** stav dosažitelnosti embeddings sidecaru (ne živý probe): plní ho background loop
  `internal/reachability` (probe po 60 s, `cmd/kukatko/capabilities.go`); když `embedding.url` není
  nastavené, je vždy `false`. Tvar je **záměrně otevřený** pro budoucí flagy (např. maps-configured).
  Frontend (`CapabilitiesProvider`) ho polluje a podle něj skrývá odkaz na sémantické hledání ve
  `FilterBar`, když je box offline (fulltext funguje dál). Mountuje se **vždy**.

## Vyhledávací jazyk (q=)

Parametr `q` na `GET /photos` i `GET /search` (a skrz `parseListParams` i na `/photos/timeline`,
`/photos/years` a `GET /favorites`) přijímá **vyhledávací jazyk**: volný text a `klíč:hodnota`
filtry smíchané v jednom stringu. Parsuje ho `internal/query` (čistý parser → AST), do SQL ho
kompiluje `internal/photos` (`store_query.go`) — **všechno přes pgx parametry**, žádná konkatenace
uživatelských hodnot.

```
dovolená camera:"Canon EOS R6" iso:100-400 faces:2
```

**Sémantika volného textu se nemění:** na `GET /photos` je zbylý volný text substring filtr
(ILIKE nad title/description/notes), na `GET /search` jde do fulltext/semantic/hybrid rankingu.
Filtry výsledek jen zužují (AND). Dotaz **bez volného textu** je čistý filtrový dotaz — `/search`
ho vyřídí po list cestě (`mode: "filter"`) a **nedotkne se embedding sidecaru**.

### Operátory

| Zápis | Význam |
| --- | --- |
| mezera mezi filtry | AND — `iso:100-400 faces:2` |
| `\|` uvnitř hodnoty | OR — `label:cat\|dog` |
| `!` před hodnotou | NOT — `label:!blurry`; jde kombinovat per-alternativa: `label:cat\|!dog` |
| `-` před slovem | NOT pro volný text — `-rozmazané` |
| `lo-hi` | rozsah čísel, obě strany volitelné — `iso:200-400`, `iso:800-`, `iso:-200` |
| `*` | zástupný znak v textové hodnotě — `filename:IMG_*`; bez `*` se matchuje substring |
| `"…"` | hodnota s mezerami — `camera:"Canon EOS R6"`; text v uvozovkách je doslovný |
| `\` | escapuje operátor (svislítko, `!`, `-`, `"`, `:`), takže se matchuje doslovně |

Příklad escapu: `label:a\|b` (zpětné lomítko před svislítkem) hledá štítek s doslovným
svislítkem `a|b` místo OR dvou alternativ; stejně `iso:100\-400` už není rozsah, a proto
degraduje na volný text. Klíče jsou case-insensitive (`ISO:100` = `iso:100`). **Neznámý klíč nebo nevalidní hodnota není
chyba**: celý token se hledá jako obyčejný text (takže `foo:bar` v popisku fotku pořád najde)
a odpověď ho vrátí v `unknown_tokens`, aby UI ukázalo nápovědu. Přesná fractional shoda
(`f:1.8`) se toleruje ±0.005 kvůli zaokrouhlení single-precision EXIF sloupců.

### Filtry

| Filtr | Hodnota | Matchuje |
| --- | --- | --- |
| `title:` `description:` `notes:` | text | příslušný sloupec fotky (substring, `*` wildcard) |
| `filename:` | text | název souboru |
| `keywords:` (alias `keyword:`) | text | IPTC klíčová slova |
| `album:` | text | členství v albu dle **názvu** (substring) nebo přesného UID |
| `label:` | text | štítek dle **názvu** nebo UID |
| `person:` (alias `subject:`) | text | subjekt dle **jména** nebo UID, přes ne-invalid markery |
| `favorite:` `private:` `archived:` | `yes\|no` | per-user oblíbené / soukromé / archivované; `archived:` **zruší výchozí live-only scope** |
| `rating:` | `0-5`, rozsahy | hodnocení aktuálního uživatele; bez řádku = 0, takže `rating:0` najde nehodnocené |
| `flag:` | `pick\|reject\|eye` | příznak aktuálního uživatele |
| `year:` `month:` `day:` | číslo, rozsahy | rok (1000–9999) / měsíc (1–12) / den (1–31) pořízení |
| `taken:` `added:` | `RRRR`, `RRRR-MM`, `RRRR-MM-DD` | datum pořízení / vložení do katalogu (celý den/měsíc/rok) |
| `before:` / `after:` | datum jako výše | pořízeno **před** začátkem data / **od** začátku data |
| `country:` `city:` | text | země/město z reverse geokódování (`photo_places`) |
| `geo:` | `yes\|no` | má/nemá GPS souřadnice |
| `alt:` | číslo (m), rozsahy | nadmořská výška (jen nezáporná — `-` je range operátor) |
| `near:` | UID fotky | fotky do `dist:` km od dané fotky (sférická vzdálenost; referenční fotka matchuje taky) |
| `dist:` | km | poloměr pro `near:` (default **5 km**); sám o sobě nefiltruje |
| `camera:` | text | výrobce **nebo** model fotoaparátu |
| `lens:` | text | model objektivu |
| `iso:` `f:` `mm:` `mp:` | číslo, rozsahy | ISO / clona / ohnisko / megapixely (`šířka×výška/10⁶`) |
| `type:` | `image\|video\|live` | typ média |
| `codec:` | text | image **nebo** video kodek (`hevc`, `jpeg`, …) |
| `portrait:` `landscape:` `square:` `panorama:` | `yes\|no` | orientace dle efektivních rozměrů (EXIF orientace 5–8 prohazuje strany); panorama = poměr ≥ 1.9 |
| `faces:` | `yes\|no`, číslo, rozsah | počet ne-invalid face **markerů**; holé číslo = **minimum** (`faces:3` ≥ 3), rozsah omezuje obě strany |
| `face:new` | enum | fotka má detekovanou, zatím **nepřiřazenou** tvář (`faces.subject_uid IS NULL`) |

Booleany berou `yes/no`, `true/false` i `1/0`. Per-user filtry (`favorite:`, `rating:`, `flag:`)
jsou vždy scopnuté na volajícího (`RatedBy`); bez přihlášeného uživatele jsou inertní.
Strukturované query params (`?album=`, `?label=`, `?year=`, …) **fungují dál beze změny** —
jazyk je čistě aditivní a saved searches zůstávají kompatibilní.
