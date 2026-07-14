# Kukátko — návrh architektury

**Verze:** 0.1 (návrh) · **Datum:** 2026-06-25 · **Stav:** implementováno, v aktivním vývoji (M0–M7)

Tento dokument je závazný návrh systému Kukátko. Vychází z design docu (feature list),
z analýzy referenčního projektu **photo-sorter** (autor je tentýž) a z ověřené rešerše
reálných rozhraní (PhotoPrism API, mapy.com REST API, pgvector na ARM, inference sidecar).
Citované zdroje jsou v sekci [§17 Reference](#17-reference).

---

## 1. Účel a rozsah

Kukátko je samostatná aplikace pro správu osobní/rodinné knihovny fotografií. Má nahradit
PhotoPrism a zároveň přinést „chytré" funkce z photo-sorteru (embeddings, obličeje,
sémantické hledání, podobné fotky) — ale s **lepší použitelností a robustností**, protože
photo-sorter se obtížně používá.

**Co je v rozsahu (z design docu):**

- Jednoduché ukládání: originály + zmenšeniny na disk, pgvector jako relační DB.
- Plná metadata jako v PhotoPrismu: GPS, štítky, alba, lidé.
- Import z PhotoPrismu + **inkrementální** doimport.
- Embeddings obrázků a obličejů (jako photo-sorter).
- Design dle [Bootswatch Superhero](https://bootswatch.com/superhero/), důraz na použitelnost.
- Slideshow na štítcích/albech — nastavitelný efekt přechodu a rychlost.
- Spolehlivé „zpět" (i na filtr).
- Uživatelé admin/editor/viewer, bcrypt hesla.
- Mapy přes [mapy.com](https://mapy.com).
- Hromadná editace metadat (alba, štítky, popisky, lokalita).
- Per-user oblíbené fotky.
- Vše jako jeden spustitelný binár včetně frontendu.
- Zálohování na S3 (originály + dump DB) jako součást běžícího procesu.
- Konfigurace přes YAML + env proměnné.
- Hledání textem (sémantické i fulltext) jako photo-sorter.
- Rozpoznávání lidí + podobné fotky (jako sorter, lepší UX).
- Funkční multiupload včetně nahrání z mobilní galerie.
- Dvojjazyčnost: čeština (default) + angličtina.
- Plná podpora telefon/tablet.
- Filtry a řazení všude (knihovna, alba, štítky).
- Detail fotky = kombinace PhotoPrism + photo-sorter (metadata/editace, obličeje, podobné).
- **Videa** (mp4/mov/live photos jako v PhotoPrismu) — ukládání, poster + náhledy přes `ffmpeg`,
  přehrávání/streamování (range requests), import videí z PhotoPrismu. Embedding na poster snímku
  (videa jsou tím i vyhledatelná).
- **Správa duplikátů** — review podobných/duplicitních fotek (pHash + embedding) a hromadný úklid.

**Co je mimo rozsah:**

- **Tvorba fotoknihy** (z photo-sorteru se vědomě nepřebírá — LaTeX stack, komplexita).
- Veřejné sdílení / share-linky nejsou prioritou (lze přidat později; PhotoPrism je nemá jako cíl).

---

## 2. Vodící principy

1. **Inspirace, ne kopie.** Z photo-sorteru přebíráme osvědčené kontrakty a datové schéma,
   ale opravujeme jeho bolesti (viz [§15](#15-co-delame-jinak-nez-photo-sorter)).
2. **PhotoPrism zůstává primární** až do ostrého cutoveru. Import je read-only a opakovatelný;
   Kukátko běží paralelně a PhotoPrism nenarušuje.
3. **Pi-first, box jako akcelerátor.** Aplikace běží na Raspberry Pi (ARM64, omezená RAM).
   Výpočetně náročná inference (CLIP, obličeje) běží na výkonném stroji (box s NVIDIA GPU,
   na Tailscale), který **není pořád zapnutý**. Vše musí fungovat i když je box offline.
4. **Brzy viditelné.** Milníky jsou seřazené tak, aby co nejdřív vznikla použitelná UI,
   na které se dál iteruje.
5. **Robustnost > featur navíc.** Persistentní stav, idempotence, graceful degradace,
   žádné ztráty dat při restartu/výpadku boxu.
6. **YAGNI.** Žádné spekulativní featury. Jednoduché, testovatelné, ohraničené moduly.
7. **Testovatelnost a kvalita od začátku.** Každá změna má unit a (kde dává smysl) integrační
   testy. Přísný `golangci-lint` a testy jsou **tvrdá brána** (`make check`). Nic se nemerguje
   s červeným lintem nebo testy. Cíl: rozšiřitelná aplikace, kterou další iterace nerozbije.
   Detail viz [§19 Kvalita, testování a linting](#19-kvalita-testovani-a-linting).

---

## 3. Architektura — přehled

### 3.1 Topologie nasazení

```
┌──────────────────────────── Raspberry Pi (ARM64) ────────────────────────────┐
│                                                                                │
│   kukatko (jeden Go binár)                                                      │
│   ├─ HTTP server (chi) + embedded SPA (React/Bootstrap)                         │
│   ├─ REST API /api/v1/...                                                       │
│   ├─ Worker (job runner) ── čte persistentní frontu z Postgresu                │
│   ├─ Thumbnailer (pure-Go + shell-out heif-convert/exiftool)                   │
│   ├─ Scheduler (záloha S3, čištění koše, expirace sessions)                    │
│   └─ Mapy.com proxy (skrývá API klíč)                                          │
│            │                          │                         │              │
│   ┌────────▼─────────┐      ┌─────────▼─────────┐     ┌─────────▼──────────┐  │
│   │ Disk: originály   │      │ shared-postgres   │     │ (lokální cache      │  │
│   │ + cache náhledů   │      │ + pgvector (HNSW) │     │  náhledů na disku)  │  │
│   └───────────────────┘      └───────────────────┘     └────────────────────┘  │
└───────────────────────────────────┬────────────────────────────────────────────┘
                                     │ Tailscale (HTTP), jen když je box zapnutý
                          ┌──────────▼───────────┐
                          │ box (x86, RTX GPU)    │
                          │ embeddings sidecar    │  /embed/image  (CLIP 768)
                          │ (FastAPI + ONNX)      │  /embed/text   (CLIP 768)
                          │                       │  /embed/face   (InsightFace 512)
                          └───────────────────────┘

       Import (read-only, paralelní provoz, jednorázově/inkrementálně):
       PhotoPrism (MariaDB, :2342)  ──API──▶  kukatko import
       photo-sorter (Postgres)      ──přímé čtení DB──▶  kukatko migrace
```

### 3.2 Komponenty (subsystémy)

Každý subsystém má jeden účel, jasné rozhraní a jde testovat samostatně.

| # | Subsystém | Odpovědnost |
|---|-----------|-------------|
| S1 | **Storage** | Layout originálů + odvozenin na disku; hashování; integrita. |
| S2 | **Ingest/upload** | Multiupload (vč. mobilu), dedup (SHA256 + pHash), EXIF/GPS, zápis fotky, zařazení jobů. |
| S3 | **Thumbnailer** | Generování náhledů na Pi (pure-Go + shell-out pro HEIC/RAW). |
| S4 | **Job queue** | Persistentní fronta v Postgresu; retry; přežije restart; graceful při offline boxu. |
| S5 | **Embeddings client** | HTTP klient k sidecaru (image/text/face); detekce dostupnosti; backoff. |
| S6 | **Search** | Fulltext (tsvector+unaccent) + sémantické (CLIP) hybrid; filtry/řazení; podobné fotky. |
| S7 | **People** | Detekce/embedding obličejů, IoU matching markerů, subjekty, návrhy, auto-clustering, outliers. |
| S8 | **Organization** | Alba, štítky, hromadná editace metadat, per-user oblíbené. |
| S9 | **Maps** | mapy.com proxy (tile + reverse geocode), GeoJSON pro mapu, clustering na klientu. |
| S10 | **Auth** | Uživatelé admin/editor/viewer, bcrypt, sliding sessions, rate-limit, audit. |
| S11 | **Import (PhotoPrism)** | API import + stažení originálů + inkrementální re-import; mapování PP UID. |
| S12 | **Migration (photo-sorter)** | Přímé čtení DB photo-sorteru; 1:1 přenos embeddingů/obličejů; mapování PS UID. |
| S13 | **Backup** | S3-kompatibilní záloha originálů + `pg_dump`, plánovaná, v procesu. |
| S14 | **Frontend (SPA)** | React/Bootstrap Superhero, i18n, mobil/tablet, back/history, slideshow, detail. |
| S15 | **Config & ops** | YAML+env konfigurace, Prometheus metriky, audit log, CLI (Cobra). |

---

## 4. Tech stack

Volby vycházejí z photo-sorteru (osvědčené) a z rešerše ([§17](#17-reference)).

### Backend
- **Go**, jeden statický binár, **`CGO_ENABLED=0`** (jako photo-sorter — zachová jednoduchý
  deploy, shell-out na CLI nástroje pro HEIC/RAW místo CGO knihoven).
- HTTP router **chi/v5**; CLI **Cobra**; konfigurace **Viper** (YAML + env — photo-sorter má
  jen env, Kukátko přidává YAML dle požadavku).
- DB přístup: `pgx` (pool) + `pgvector-go`.

### Databáze
- **PostgreSQL + pgvector.** Použít sdílený **`shared-postgres`** (vlastní DB + uživatel dle
  konvence v CLAUDE.md, nepouštět vlastní kontejner). **Ověřit dostupnost extension `vector`**
  v shared-postgresu — pokud chybí, je to první úkol M0 (`CREATE EXTENSION vector`).
- **Vektory: `halfvec` (float16) + HNSW + `vector_cosine_ops`.** Half-precision půlí paměť
  indexu při <1 % ztrátě recall na normalizovaných embeddinzích — zásadní na Pi.
- Migrace: SQL soubory v `embed.FS`, auto-apply na startu v lexikografickém pořadí
  (převzato z photo-sorteru).

### Frontend
- **React 19 + TypeScript + Vite**, embedováno do binárky přes `//go:embed all:dist/*`,
  SPA fallback na `index.html` (jako photo-sorter).
- **react-bootstrap + Bootswatch Superhero** téma (dark). Bohaté interakce (slideshow, crop,
  infinite scroll) → React je oproti vanilla Bootstrapu nutný.
- **i18next** (cz default, en). **Leaflet** + `Leaflet.markercluster` pro mapu.
  `react-virtuoso` pro virtualizovaný grid.

### Zpracování obrázků (na Pi, bez CGO)
- JPEG/PNG/WebP/**BMP/GIF/TIFF**: pure-Go (`disintegration/imaging` + `golang.org/x/image/{bmp,tiff}`
  + stdlib `image/gif`), paralelně přes goroutines. EXIF orientace `imaging.AutoOrientation`;
  animovaný GIF se náhleduje z prvního snímku.
- **HEIC:** shell-out `heif-convert` (apt `libheif-examples`) → JPEG → resize v Go.
- **RAW** (cr2/cr3/nef/nrw/arw/srf/dng/raf/orf/rw2/pef/srw/3fr/iiq/x3f/kdc/mrw/mef): vytáhnout
  embedded JPEG preview (`exiftool -b -PreviewImage` / `dcraw -e`), ne plný demosaic. TIFF magic
  neunese RAW — RAW přípona má přednost, protože RAW kontejnery jsou většinou TIFF-based.
- EXIF metadata: `exiftool` (subprocess) + pure-Go fallback.
- (Volitelně později: shell-out na `vipsthumbnail` pro velké soubory kvůli paměti — ~200 MB
  vs GB díky shrink-on-load. Default je pure-Go.)

### Inference sidecar (na boxu)
- **Reuse existující služby na boxu.** Kukátko nestaví nový sidecar — volá **existující
  embeddings službu běžící na boxu** (stejné modely jako photo-sorter → 1:1 kompatibilita).
  Adresa je v konfiguraci (`embedding.url`); když je box offline, joby čekají ve frontě
  (viz [§8](#8-asynchronni-joby--box-offline)). Kontrakt viz [§6.1](#61-kontrakt-sidecaru).
- Modely (stejné jako photo-sorter): **CLIP ViT-L/14** (image+text, 768-dim),
  **InsightFace `buffalo_l`** (ArcFace, 512-dim). Pozn.: pretrained packy jsou typicky
  *non-commercial/research* — pro osobní použití OK.

### Úložiště originálů (`storage.backend`)
- **Dva backendy za jedním rozhraním `storage.Storage`**, přepínané jediným konfiguračním
  klíčem `storage.backend`; nad rozhraním žádný balíček rozdíl nepozná.
  - **`fs`** (default) — originály na lokálním disku, publikace atomickým hard-linkem.
    Existující nasazení i celá testovací sada tím zůstávají nedotčené.
  - **`r2`** — **privátní** Cloudflare R2 bucket (S3-kompatibilní, klient je **stejné
    `minio-go/v7`** jako u záloh; žádná nová závislost). Pro VPS, jehož disk knihovnu
    (~120 GB) neunese.
- **Object key = `photos.file_path` / `photo_files.file_path` doslova.** Ta hodnota už v Postgresu
  je a neodvozuje se; klíč tedy *je* ona → **žádný nový sloupec a žádná migrace klíčů**. Náhledy si
  nechávají svůj hash-derived cache layout, který se rovnou stává klíčem. Klíč **není tajemství**:
  request bez platného podpisu odmítne Worker dřív, než se k objektu dostane.
- **Podepsané URL:** `https://<media_base_url>/<key>?exp=<unix>&sig=<hex HMAC-SHA256>`, podpis kryje
  klíč i expiraci. Ověřují se **dvě tajemství naráz** (současné + předchozí), takže rotace nemá okno
  rozbitých URL. Default TTL 1 h; každá API odpověď nese čerstvě podepsané URL.
- **Worker není součástí téhle repo.** Bucket, zdroják Workeru, jeho bindingy i hostname
  (`kukatko-media.panbotka.cz`) žijí v **infra repu** (`/home/pi/projects/infra`, root modul
  `cloudflare-r2/`) a nasazuje je tam Terraform. Kukátko URL **razí** (`internal/storage/sign.go`),
  Worker je **ověřuje** — dvě implementace ve dvou repech a dvou jazycích, jejichž rozejití by
  žádný build nezachytil (jeden bajt navíc v podepisované zprávě = 403 na každou fotku).
- **Kontrakt drží golden vektory:** `internal/storage/testdata/url_signature_vectors.json` (secret,
  klíč, expirace → očekávaný podpis; včetně předchozího tajemství a záměrně špatného podpisu).
  Testuje se proti nim **obojí** — Go signer i Worker v infra repu. Změna algoritmu = regenerace
  souboru, což změnu zviditelní v review a vynutí souběžnou úpravu Workeru.
- **Hard-link nemá v object storage ekvivalent a není potřeba:** `PutObject` je atomický a
  katalogový dedup drží unique constraint na `photos.file_hash`. Uploady i stahování streamují —
  soubor nikdy nedrží celý v RAM; `r2` je stageuje přes `storage.temp_path`, protože klíč závisí na
  obsahu (bez SHA256 nelze odlišit re-upload od stejnojmenného jiného souboru) a protože
  `Materialize` musí externím nástrojům podat **reálný lokální soubor**. Temp soubor se maže vždy,
  i na error path.
- Detaily (metadata `x-amz-meta-sha256`, konfigurační klíče, rotace tajemství) v
  [`docs/PACKAGES.md`](PACKAGES.md) a [`docs/OPERATIONS.md`](OPERATIONS.md); rozhodnutí a cenové
  srovnání proti DO Spaces v `docs/superpowers/specs/2026-07-09-s3-storage-design.md`.

### Zálohování
- **`minio-go/v7`** (generický S3 endpoint, path-style, stream `objectSize=-1`).

---

## 5. Datový model

Schéma navazuje na photo-sorter (kompatibilita pro migraci) s úpravami pro Kukátko.
UID = `VARCHAR(32)`, generované aplikací (prefix + náhodný suffix). `file_hash` = SHA256 hex.
Originály v layoutu `YYYY/MM/<filename>` — na disku cesta pod rootem, v R2 rovnou object key.

### 5.1 Klíčové tabulky (převzaté z photo-sorteru, upravené)

- **`photos`** — `uid PK`, `file_hash UNIQUE` (SHA256), `file_path`, `file_name/size/mime`,
  `file_width/height/orientation`, `taken_at` + `taken_at_source`, `title/description/notes`,
  `ai_note` (volný text z externí AI klasifikace, `NOT NULL DEFAULT ''`, editovatelný, ve fulltextu),
  `lat/lng/altitude`, `camera_make/model`, `lens_model`, `iso/aperture/exposure/focal_length`,
  `exif JSONB`, `private` (**legacy** — píše ho už jen import z PhotoPrismu/photo-sorteru,
  aplikace ho nefiltruje ani needituje), `archived_at`, `uploaded_by`, časy.
  - **IPTC/XMP + technická metadata souboru** (migrace `0027_photos_iptc_metadata.sql`, všechny
    `NOT NULL DEFAULT ''`, resp. `false`): **editovatelné** `subject` (IPTC headline — o čem fotka
    je; ve fulltextu váha B), `keywords` (IPTC klíčová slova **verbatim**, comma-separated dle tvaru
    PhotoPrismu; ve fulltextu váha C — **nejsou to labely**, `internal/organize` zůstává beze změny),
    `artist`, `copyright`, `license`, `scan` (`BOOLEAN` — sken papírové fotky, ne snímek fotoaparátem);
    **strojově odvozené** (uloží se a servírují, ale needitují) `software` (firmware/Lightroom/skener),
    `color_profile` (ICC), `image_codec` (komprese **stillu**: jpeg/heic/avif — `video_codec`/
    `audio_codec` jsou samostatné), `camera_serial`, `original_name` (jméno souboru před importem;
    `file_name` je jméno ve storage layoutu), `projection` (`equirectangular` u panoramat).
    Plnění z EXIF a mapování z PhotoPrism importu jsou samostatné úkoly — stávající řádky mají
    defaulty.
  - **Přibližné („cca") datum** (migrace `0029_photos_taken_at_estimate.sql`): `taken_at_estimated`
    (`BOOLEAN NOT NULL DEFAULT false` — datum je **odhad**, ne fakt) + `taken_at_note`
    (`TEXT NOT NULL DEFAULT ''` — volný text vlastními slovy: „kolem roku 1950", „za války").
    Pro naskenované a zděděné fotky, kde přesné datum nikdo nezná. `taken_at` **zůstává jediná
    kotva** pro řazení, timeline, grouping i datumové filtry — příznak je jen prezentace a
    pravdivost (UI značí datum `cca`/`c.`), **není to druhá datumová osa** a nemění žádné pořadí
    (proto ani nový index). `taken_at` NULL + příznak `true` je povolený stav: fotka se všude chová
    jako každá jiná bez data a význam nese poznámka. Poznámka žije jen s příznakem — shodí-li se
    příznak, `internal/photoapi` poznámku maže (nikdy nezůstane u data prezentovaného jako fakt).
    Do fulltextu `photos.fts` **nepadá** (je to poznámka k datování, ne titulek).
  **Nové sloupce pro Kukátko:**
  - `photoprism_uid VARCHAR(32)` — PhotoUID z PhotoPrismu (dedup + inkrement).
  - `photoprism_file_hash VARCHAR(40)` — SHA1 souboru z PhotoPrismu (download mapping).
  - `photosorter_uid VARCHAR(32)` — UID z photo-sorteru (migrace).
  - **Video** (migrace `0004_video.sql`): `media_type IN (image|video|live)` (default `image`,
    partial index pro „jen videa"), `duration_ms`, `video_codec`, `audio_codec`, `has_audio`,
    `fps`. Naplněné u videí přes `internal/video.Probe` (ffprobe → exiftool fallback);
    poster frame (`internal/video.ExtractPoster`, ffmpeg) jde do thumbnaileru/pHash i embed/face
    jobů. Live foto = still jako primární image + motion klip jako další `photo_files` řádek.
  - generovaný `fts tsvector` sloupec (GIN index) — viz [§6.2](#62-hledani).
  - `favorite` se **přesouvá** do per-user tabulky (viz níže).
- **`photo_files`** — originály + odvozeniny, `role IN (original|sidecar|edited)`, `is_primary`.
- **`photo_phashes`** — `phash/dhash BIGINT` (near-duplicate detekce).
- **`photo_edits`** — nedestruktivní úpravy (crop/rotation/brightness/contrast), 0..1 souřadnice.
- **`embeddings`** — `photo_uid PK`, `embedding halfvec(768)`, `model`, `pretrained`, `dim`;
  HNSW `halfvec_cosine_ops` (m=16, ef_construction=200).
- **`faces`** — `id BIGSERIAL`, `photo_uid`, `face_index`, `embedding halfvec(512)`,
  `bbox float8[4]` (normalizované [x,y,w,h] 0..1), `det_score`, cache `marker_uid/subject_uid/
  subject_name/photo_width/height/orientation`; HNSW `halfvec_cosine_ops`.
- **`subjects`** — osoby/zvířata (`type IN (person|pet|other)`), `name`, `slug`, `cover_photo_uid`.
- **`markers`** — `type IN (face|label)`, normalizovaný bbox (x,y,w,h 0..1), `subject_uid`,
  `score`, `invalid`, `reviewed`.
- **`albums`** + **`album_photos`** — `type IN (album|folder|moment|state|month)`; album je vždy
  chronologické (ruční `sort_order` i volbu řazení `order_by` odstranila migrace 0022).
- **`labels`** + **`photo_labels`** — `source IN (manual|ai|import)`, `uncertainty`.
- **`users`** — `role IN (admin|editor|viewer)`, `password_hash` (bcrypt cost 12), `disabled`.
- **`sessions`** — viz [§11](#11-auth-a-bezpečnost) (přidáno sliding expiry).
- **`audit_log`** — durable, zapisuje se **ve stejné transakci** jako mutace (migrace
  `0012_audit_log.sql` + `0014_audit_request.sql` přidává `ip`/`user_agent` a index
  `(target_type, target_uid)`, balík `internal/audit`: `Write(ctx, exec, Entry)` přes pool **i**
  `pgx.Tx`; handler konvence `FromRequest`→`Meta`→`Entry`). Konzumenti: hromadná editace metadat
  (`POST /api/v1/photos/bulk`) a foto PATCH/archive/unarchive (audited varianty `photos.Store`).
  Admin čtení: `GET /api/v1/audit` (`internal/auditapi`, filtry user/entity/action/datum +
  stránkování, admin-only). Další mutační domény přebírají in-tx audit konvenci postupně.

### 5.2 Nové tabulky v Kukátku

- **`user_favorites`** — per-user oblíbené: `(user_uid, photo_uid) PK`, `added_at`.
  Nahrazuje globální `photos.favorite`.
- **`jobs`** — persistentní fronta (viz [§8](#8-asynchronni-joby--box-offline)):
  ```
  jobs(
    id BIGSERIAL PK,
    type        TEXT,          -- image_embed | face_detect | thumbnail | pp_import | ...
    state       TEXT,          -- queued | running | done | failed | dead
    priority    INT DEFAULT 0,
    payload     JSONB,         -- např. {"photo_uid": "..."}
    attempts    INT DEFAULT 0,
    max_attempts INT DEFAULT 5,
    last_error  TEXT,
    run_after   TIMESTAMPTZ,   -- backoff / odložení
    locked_by   TEXT,          -- worker id (pro SELECT … FOR UPDATE SKIP LOCKED)
    locked_at   TIMESTAMPTZ,
    created_at, updated_at TIMESTAMPTZ
  )
  -- index na (state, run_after, priority); dedup unique na (type, payload->>'photo_uid') WHERE state IN (queued,running)
  ```
- **`import_runs`** — historie importů: zdroj (`photoprism`/`photosorter`/**`folder`** =
  `kukatko import dir`, migrace `0026`), high-watermark
  (`updated:` timestamp pro inkrement; `folder` běh ho nemá — složka nemá zdrojový čas, idempotenci
  dělá SHA256 obsahu), počty, čas. Idempotence inkrementálního importu.

### 5.3 Mapování identit (kvůli importu/migraci)

| Zdroj | Klíč ve zdroji | Uložení v Kukátku | Účel |
|-------|----------------|-------------------|------|
| PhotoPrism | PhotoUID (16 znaků) | `photos.photoprism_uid` | dedup, inkrement |
| PhotoPrism | Files[].Hash (SHA1) | `photos.photoprism_file_hash` | stažení originálu `/dl/:hash` |
| photo-sorter | `photos.uid` | `photos.photosorter_uid` | 1:1 migrace embeddingů/obličejů |

> **Pozor:** PhotoPrism používá pro hash souboru **SHA1**, Kukátko **SHA256**. Po stažení
> originálu z PhotoPrismu si Kukátko spočítá vlastní SHA256 (dedup) a uloží PP SHA1 jen pro
> dohledání. Migrace z photo-sorteru naopak SHA256 sdílí, takže dedup je přímý.

---

## 6. Embeddings a vektorové hledání

### 6.1 Kontrakt sidecaru

Stejný jako photo-sorter (`EMBEDDING_URL`, default offline-aware). HTTP:

- **`POST /embed/image`** — multipart, pole `file`. Odpověď:
  `{ "dim": 768, "embedding": [float32×768], "model": "...", "pretrained": "ViT-L-14" }`
- **`POST /embed/text`** — JSON `{ "text": "..." }`. Odpověď jako výše (768-dim, sdílený prostor).
- **`POST /embed/face`** — multipart, pole `file`. Odpověď:
  ```
  { "faces_count": N, "model": "...",
    "faces": [ { "face_index": 0, "dim": 512, "embedding": [float32×512],
                 "bbox": [x1,y1,x2,y2] /*px*/, "det_score": 0.0..1.0 } ] }
  ```
  Pixelové `[x1,y1,x2,y2]` se při zápisu převedou na normalizované `[x,y,w,h]` (0..1) dle
  rozměrů a EXIF orientace (logika převzatá z photo-sorteru).

### 6.2 Hledání

- **Implementace:** jediný endpoint `GET /api/v1/search?q=…&mode=…` (`internal/photoapi`),
  parametr `mode` = `fulltext` | `semantic` | `hybrid` (**default `hybrid`**). Všechny módy
  ctí standardní list filtry (datum/GPS/…) i stránkování; odpověď má stejný tvar jako
  list + pole `mode` (efektivní mód) a `degraded` (viz níže).
- **Fulltext:** PostgreSQL `tsvector` (dictionary `simple`, `unaccent` pro češtinu) nad
  title(A) > description(B) = subject(B) > notes(C) = ai_note(C) = keywords(C) > file_name(D).
  Diakritika necitlivá („deti" = „Děti"). Řazení dle `ts_rank` (`photos.Store.Search`).
  Generovaný sloupec se přepisuje `ALTER COLUMN fts SET EXPRESSION` (naposledy
  `0027_photos_iptc_metadata.sql`) — Postgres přepočítá vektor všem řádkům a přestaví GIN index sám.
- **Sémantické (text→fotka):** text → sidecar `/embed/text` (768-dim CLIP) → HNSW cosine nad
  `embeddings` (`vectors.Store.FindSimilar`). Kandidáti se pak profiltrují list filtry přes
  `photos.Store.FilterUIDs` (strukturální filtry, ignoruje fulltext) a seřadí dle vzdálenosti.
- **Hybrid:** fulltextový a sémantický ranking se sloučí **Reciprocal Rank Fusion (RRF)** —
  skóre položky = Σ 1/(k + rank) přes oba seznamy, konstanta **k = 60** (původní RRF paper,
  Cormack et al. 2009). Výsledek je deduplikovaný (sjednocení obou seznamů), seřazený dle
  fúzního skóre (tie-break dle uid sestupně).
- **Box offline → graceful degradace:** když sidecar nedostane embedding dotazu
  (`embedding.IsUnavailable`, nebo embedder/vector store nezapojen), `semantic`/`hybrid`
  spadnou na čistý fulltext a odpověď nastaví `degraded: true` (UI o tom informuje uživatele).
  Upload i prohlížení fungují bez boxu dál.
- **Podobné fotky:** HNSW nad `embeddings` (`embedding <=> $vec`), práh vzdálenosti pro
  „duplikáty" (~0.05) i „podobné" (vyšší práh).
- **Parametry HNSW:** `m=16`, `ef_construction=200`, dotaz `SET LOCAL hnsw.ef_search=100`
  (nikdy ≥400 — planner spadne na seq scan). Metrika cosine (embeddingy jsou L2-normalizované).

---

## 7. Lidé a obličeje

Workflow (vylepšené UX oproti photo-sorteru):

1. Po importu/uploadu job `face_detect` → sidecar `/embed/face` → uložení do `faces`.
2. **Auto-clustering:** podobné obličeje se vektorově seskupí (HNSW + práh / souvislé komponenty),
   uživateli se nabízejí celé shluky k jednorázovému pojmenování — méně klikání než per-face.
3. **IoU matching** (práh 0.1) propojí detekovaný obličej s existujícím markerem.
4. **Návrhy:** pro nepojmenovaný obličej se hledají podobné už pojmenované (HNSW), s filtrem
   (min. velikost obličeje, vyloučit jiné osoby), limit ~5.
5. **Přiřazení:** stavy `create_marker` / `assign_person` / `unassign_person` / `already_done`.
6. **Outlier detekce:** pro každou osobu spočítat centroid a seřadit obličeje dle vzdálenosti
   → odhalí špatně přiřazené. Implementace: `internal/outliers` (`Outliers(subjectUID)` nad
   `vectors.ListFacesBySubject` + sdílené `vectors.Centroid`/`CosineDistance`) za endpointem
   `GET /api/v1/subjects/{uid}/outliers` (editor/admin, `internal/outlierapi`); malé množiny
   (< 3 obličeje) se vrátí s `meaningful:false`. Wrong obličej se odpojí přes existující assign
   API — outlier vrstva nemutuje.
7. **Stránky osob:** přehled, cover, počty, výskyty.

Souřadnice: `faces.bbox` normalizované [x,y,w,h] (0..1, display space, EXIF-aware);
`markers` taktéž 0..1. Převod z pixelů sidecaru řeší helper (`facejob.normalizeBBox`) se swapem
stran pro orientaci 5–8 (sidecar/InsightFace rotuje obraz dle EXIF, takže `face_detect` posílá
**originál v plném rozlišení**, ne náhled, aby měřítko bboxu odpovídalo uloženým rozměrům).
Job `face_detect` (`internal/facejob`) je **idempotentní** přes tabulku `face_detections`
(migrace 0009): jeden řádek na zpracovanou fotku odliší fotku bez obličejů od dosud
nezpracované (`faces` může mít nula řádků). Slabé detekce filtruje práh `faces.min_det_score`.
Admin backfill: `POST /api/v1/process/faces`.

---

## 8. Asynchronní joby a „box offline"

Toto je hlavní robustnostní vylepšení proti photo-sorteru (ten má in-memory joby + SSE,
které se ztratí při restartu).

- **Persistentní fronta v Postgresu** (`jobs`). Worker bere práci přes
  `SELECT … FOR UPDATE SKIP LOCKED`, ať více workerů/instancí nekoliduje.
- **Worker runtime** (`internal/worker`) běží **v procesu `kukatko serve`**: konfigurovatelný
  počet goroutin pollujících `Claim` s omezenou souběžností, dispatch na handler z **registru**
  (`Register(type, HandlerFunc)`) podle `job.Type`, `Complete`/`Fail` dle výsledku, plus
  stale-lock recovery. **Graceful shutdown** (SIGINT/SIGTERM) zastaví claiming a opuštěné
  in-flight joby nechá frontě k recovery. Stav fronty se čte přes **admin Jobs API**
  (`internal/jobsapi`: `GET /jobs/stats`, `GET /jobs`, `POST /jobs/{id}/requeue`); UI ho polluje.
- **Typy jobů:** `thumbnail`, `places`, `metadata` (běží lokálně na Pi, hned), `image_embed`,
  `face_detect` (vyžadují box), `pp_import`, `ps_migrate`, `backup`.
- **Box offline:** embeddings client před zpracováním ověří dostupnost sidecaru (health check).
  Když je box offline, joby `image_embed`/`face_detect` zůstanou `queued` s `run_after`
  posunutým (backoff), upload a prohlížení fungují bez omezení. Po naběhnutí boxu se fronta
  sama dožene.
- **Idempotence:** dedup na `(type, photo_uid)` v aktivních stavech; `filterUnprocessedPhotos`
  přeskočí už hotové.
- **Retry & dead-letter:** `attempts < max_attempts`, exponenciální backoff přes `run_after`,
  pak `state=dead` + `last_error` (viditelné v adminu).
- **Progress:** UI dostává stav z DB (polling / SSE jen jako tenká vrstva nad DB stavem).
- **Auto-wake boxu (volitelné, `internal/wake`):** balík `wake` periodicky kontroluje frontu
  (každou minutu, `wakeCheckInterval`) a když je nakonfigurovaně **zapnuto** a počet čekajících
  `image_embed`/`face_detect` jobů dosáhne `embedding.wake.min_queue` **a zároveň** health check
  sidecaru hlásí offline, pošle Wake-on-LAN magic packet na lokální LAN (knihovna `mdlayher/wol`).
  **Cooldown** (`embedding.wake.cooldown`) brání spamování spícího boxu; po **grace period** se
  zdraví překontroluje a zaloguje, jestli box naběhl, jinak se backoffne do dalšího cooldownu.
  Smyčka běží ve vlastní goroutině v `serve` — **nikdy neblokuje zpracování jobů**. WoL
  **nefunguje přímo přes Tailscale** (L3 overlay bez L2 broadcastu) — host musí být na stejné
  fyzické síti jako box; defaultní cesta je UDP broadcast na `embedding.wake.broadcast_addr`,
  volitelně raw Ethernet rámec na `embedding.wake.interface` (vyžaduje CAP_NET_RAW). **Defaultně
  vypnuto** (`embedding.wake.enabled=false`), plně inertní; ruční zapnutí boxu stačí. Uspávání
  boxu je mimo rozsah.

---

## 9. Import z PhotoPrismu (S11)

PhotoPrism běží paralelně a zůstává primární. Import je **read-only, opakovatelný, inkrementální**.

- **Autentizace:** dlouhožijící **app password / access token** (ne login na každý request —
  login je nejtvrději rate-limited). Vytvoření na straně PP:
  `photoprism auth add -n Kukatko -s "photos albums"`. Token v `Authorization: Bearer`.
- **Výpis fotek:** `GET /api/v1/photos?count=1000&offset=N&merged=true&order=updated&q=updated:"<RFC3339>"`.
  Stránkování `count`≤1000 + `offset`. Pole: UID, TakenAt, Lat/Lng/Altitude, Title/Description,
  Type, Width/Height, OriginalName, Camera/Lens/EXIF, `Files[]` (UID, Hash=SHA1, Primary, Mime,
  Video/Codec, Markers[]).
- **Videa & live photos:** PP `Type` video/animated → stáhne se **samotný video soubor**
  (`Files[]` s `Video=true`), uloží s `media_type=video` a **probnutými** video metadaty
  (`duration_ms`/`video_codec`/`audio_codec`/`has_audio`/`fps` přes `internal/video.Probe`), poster +
  náhledy přes ffmpeg, embedding běží na posteru. PP `Type` live → **still** jako primární originál +
  **motion klip** jako `sidecar` photo_file (`media_type=live`), video metadata z motion klipu. Vše
  ostatní jako u obrázků (dedup, externí ID, alba/labely/lidé, inkrement).
- **Inkrement:** ukládat high-watermark `max(UpdatedAt)` do `import_runs`; další běh táhne jen
  `updated:` ≥ watermark. (Empiricky ověřit, zda `updated:` zachytí i změny metadat; jinak
  fallback na `added:` + watermark.)
- **Stažení originálu:** `GET /api/v1/dl/<Files[].Hash>?t=<download_token>` (download token
  z create-session; může rotovat — číst `X-Download-Token` z odpovědí). Po stažení spočítat
  SHA256 → dedup proti `photos.file_hash`; uložit `photoprism_uid` + `photoprism_file_hash`.
- **Metadata navíc:** alba `GET /api/v1/albums` (+ `s=<albumUID>` pro obsah), labely
  `GET /api/v1/labels`, osoby `GET /api/v1/subjects`, obličeje `GET /api/v1/faces`, markery
  z `Files[].Markers[]`, GPS přímo na fotce (případně `GET /api/v1/geo`).
- **Embeddings/obličeje:** PhotoPrism je nevystavuje použitelně → po importu se v Kukátku
  **dopočítají** jobem (na boxu). (Pro fotky, které jsou i v photo-sorteru, je převezme migrace —
  viz §10, ušetří přepočet.)
- **Úskalí (ošetřit):** API nemá deprecation policy (zafixovat verzi PP, otestovat po upgradu);
  rate-limit 429 → backoff; `Content-Type: application/json` u JSON endpointů.

## 10. Migrace z photo-sorteru (S12)

Jednorázová (případně opakovatelná) migrace z běžící DB photo-sorteru. Protože **modely
i dimenze jsou stejné** (CLIP 768 + InsightFace 512), embeddingy i obličeje se přenášejí 1:1
bez přepočtu.

- **Přímé čtení Postgresu** photo-sorteru (read-only credentials).
- **Mapování:** `photos.uid` (PS) → nová fotka v Kukátku; ukládá se `photosorter_uid`.
  Match přes `file_hash` (SHA256 sdílené) — pokud fotka už je z PP importu, jen se doplní
  embeddingy/obličeje a `photosorter_uid`.
- **Přenášené entity:** `photos` (metadata), `embeddings` (768), `faces` (512 + bbox + cache),
  `subjects`, `markers`, `albums`/`album_photos`, `labels`/`photo_labels`, `photo_edits`,
  `photo_phashes`. **Nepřenáší se:** fotokniha (`photo_books`, …), share-linky.
- Originály: pokud nejsou na stejném disku, zkopírovat dle `file_path`.

**Stav: implementováno.** Read-only klient `internal/photosorter` (vlastní pgx pool, volitelný
`search_path` scope pro testy) + migrátor `internal/psimport` (`Service.Migrate`). Spouští se CLI
`kukatko migrate photosorter` (synchronně) nebo admin triggerem `POST /api/v1/import/photosorter`,
který zařadí singleton `ps_migrate` job na background worker. Běh je **inkrementální a idempotentní**:
resume přes `import_runs` watermark (viz [§9](#9-import-z-photoprismu-s11)), match dle
`photosorter_uid`/`file_hash`, embeddingy/obličeje 1:1, satelity find-or-create; per-fotka chyby se
tallyují a běh pokračuje. Konfigurace `import.photosorter.{dsn,page_size}`
(`KUKATKO_IMPORT_PHOTOSORTER_DSN`). Pokrytí: unit testy s faky + integrační testy proti
naseedovanému fake photo-sorter schématu.

---

## 11. Auth a bezpečnost

- **Uživatelé:** role admin/editor/viewer; `editor`+`admin` mají write. Bcrypt cost 12.
  Bootstrap admina přes env (`BOOTSTRAP_ADMIN_*`) na čistou instalaci.
- **Sessions:** opaque token v HttpOnly + SameSite=Strict cookie; oddělený `download_token`.
  **Vylepšení proti photo-sorteru:**
  - **Sliding expiry** — prodloužení při aktivitě (aktivní uživatel nevypadne po 30 dnech).
  - **Změna hesla zruší ostatní sessions** uživatele.
  - **Rate-limit na `/auth/login`** (brute-force ochrana; photo-sorter ji má jen na share-link).
  - **Rate-limit náročných endpointů** (`internal/ratelimit`) — per-client-IP token-bucket
    (`ratelimit.*` config) na `POST /upload`, `POST /photos/bulk`, `POST /import/*` a
    `GET /map/tiles/...`, aby jeden klient nezahltil server; prázdný bucket → 429. Limiter běží
    před auth checkem a je vypnutelný (`rate_per_sec ≤ 0`).
- **Audit log durable** — zápis do `audit_log` ve **stejné transakci** jako mutace (photo-sorter
  zapisuje až po commitu → při crashi ztráta).
- **Mapy.com klíč** se nikdy neposílá do prohlížeče — tile/geocode requesty jdou přes
  **backend proxy** (viz §12 Mapy).

---

## 12. Mapy (S9)

- **Dlaždice:** Kukátko proxy endpoint → `https://api.mapy.com/v1/maptiles/{mapset}/256/{z}/{x}/{y}`
  (klíč přidá backend přes `X-Mapy-Api-Key`, nikdy se neobjeví v klientovi). Mapsety
  `basic|outdoor|aerial|winter`, retina `256@2x` pro basic/outdoor.
- **Povinné (NEPORUŠIT):** attribution `© Seznam.cz a.s. a další` (link na `/copyright`)
  **a** klikací logo mapy.com nad mapou (Leaflet control s `logo.svg` → `mapy.com`).
- **Markery/clustering:** `Leaflet.markercluster` na klientu; data fotek s GPS z Kukátko API.
- **Reverse geocode (lokalita fotky):** proxy na `GET /v1/rgeocode?lon=&lat=&lang=cs`
  → `location` string (např. „Praha 1 - Staré Město, Česko"). Volá se na vyžádání / dávkově,
  ne pro každou fotku (geocode = 4 kredity vs 1 dlaždice).
- **Limity:** free 250k kreditů/měsíc; rate 500 dlaždic/s, 200 rgeocode/s. Hlídat.

---

## 13. Frontend (S14)

- **Stack:** React 19 + TS + Vite + react-bootstrap + Bootswatch Superhero (dark), embedováno do binárky.
- **i18n:** i18next, **čeština default** + angličtina; přepínač jazyka, persistence volby.
- **Mobil/tablet:** plně responzivní; multiupload **z mobilní galerie** (`<input capture>` /
  file picker); touch-friendly slideshow a detail.
- **„Zpět vždy funguje (i na filtr)":** veškerý stav pohledu (filtry, řazení, hledání, stránka)
  je v **URL query params** + History API. Browser back obnoví předchozí filtr; server je
  vůči view-stavu bezstavový. Sdílení URL = sdílení pohledu.
- **Knihovna:** virtualizovaný grid (`react-virtuoso`), infinite scroll, filtry+řazení.
- **Detail fotky** (kombinace PP + photo-sorter): náhled + metadata (zobrazení/editace),
  EXIF, GPS/mini-mapa, **obličeje** (boxy, přiřazení osob), **podobné fotky**, štítky, alba,
  oblíbené, nedestruktivní úpravy (crop/rotate/jas/kontrast).
- **Hromadná editace:** výběr více fotek → alba, štítky, popisky, lokalita, oblíbené.
- **Slideshow:** na albech/štítcích; nastavitelný **efekt přechodu** (fade/slide/…) a **rychlost**;
  fullscreen, touch/klávesy.

---

## 14. Konfigurace, build a provoz (S15)

### Konfigurace
- **YAML + env override** (Viper). Klíče (vychází z photo-sorteru, rozšířeno): `database.url`,
  `storage.originals_path`, `storage.cache_path`, `embedding.url`/`dim`, `web.port`/`host`/
  `session_secret`, `auth.bootstrap_admin_*`, `maps.mapy_api_key`, `backup.s3.{endpoint,
  region,bucket,access_key,secret_key,path_style}`, `backup.schedule`, `duplicate.*`,
  `trash.retention_days`. Tajemství primárně přes env.

### Build & deploy
- **goreleaser**, `CGO_ENABLED=0`, **arch arm64** (+ amd64 pro vývoj), `.deb` balík se
  systemd unitem a env-file (conffile, zachová se při upgradu).
- Runtime závislosti (apt): `exiftool`, `libheif-examples` (heif-convert), `dcraw`/LibRaw,
  `postgresql-client` (pg_dump **i** pg_restore). (Bez `texlive` — fotokniha vynechána.)
- DB migrace auto-apply na startu. Frontend `npm ci && npm run build` → `embed.FS`.

### Zálohování (S13)
- V procesu, plánovaně (cron/scheduler): `pg_dump` + sync originálů na **S3-kompatibilní**
  endpoint (`minio-go`, path-style, stream `objectSize=-1`). Konfigurovatelný endpoint
  (AWS/MinIO/Backblaze/Wasabi). Retence/verze konfigurovatelná.

### Obnova / disaster recovery (S13)
- Protějšek zálohy, aby byla **použitelná**. CLI strom `kukatko restore` (sdílí `backup.s3.*`
  konfiguraci, `internal/backup`):
  - `restore list` — vypíše dumpy v bucketu (`db/kukatko-*.dump`), nejnovější první.
  - `restore db [--dump KEY] [--yes] [--verify]` — **destruktivní**: stáhne dump z S3 a streamuje
    ho do `pg_restore` (`--clean --if-exists --single-transaction`, heslo přes `PGPASSWORD` env,
    nikdy v argv), pak idempotentně re-aplikuje migrace. Vyžaduje `--yes` (přepisuje data).
    Bez `--dump` obnoví nejnovější dump.
  - `restore originals` — stáhne chybějící originály z bucketu do `storage.originals_path`,
    přeskočí už existující dle **klíče + velikosti**; atomický zápis přes `.tmp` + rename →
    **resumovatelné** (přerušený běh se bezpečně zopakuje).
  - `restore verify` — integritní report: počet fotek v DB vs. originálů na disku + nesoulady
    (`photo_files.file_path` chybějící na disku / soubory na disku bez záznamu).
- **Admin API** (`internal/restoreapi`, admin-only, jen **read-only** operace): `GET /restore/dumps`
  a `POST /restore/verify`. Destruktivní obnova DB se přes HTTP **záměrně neexponuje** (obnova pod
  běžícím serverem by mu podtrhla tabulky) — patří do CLI při zastaveném serveru.
- Náhledy (cache) se po obnově regenerují **líně** on-demand; embeddingy/faces jsou součástí dumpu.
- Runbook (fresh machine → install → restore → verify): [`docs/RESTORE.md`](RESTORE.md).

### Observability
- **Prometheus** metriky (jako photo-sorter), `audit_log`, strukturované logy.

---

## 15. Co děláme jinak než photo-sorter

| Bolest photo-sorteru | Řešení v Kukátku |
|----------------------|------------------|
| In-memory joby, ztráta při restartu | Persistentní fronta v Postgresu (`jobs`, SKIP LOCKED, retry, dead-letter) |
| Žádný rate-limit na login | Rate-limit na `/auth/login` |
| 30denní absolutní expiry session | Sliding expiry (prodloužení při aktivitě) |
| Změna hesla nezruší ostatní sessions | Zruší je |
| Editovaný download drží celý obrázek v RAM | Streamování výstupu |
| Chybí FK na embeddings/faces | FK s `ON DELETE CASCADE` |
| Audit log mimo transakci (riziko ztráty) | Audit ve stejné transakci jako mutace |
| Manuální per-face pojmenování (pracné) | Auto-clustering obličejů + hromadné pojmenování shluku |
| Globální „favorite" | Per-user oblíbené (`user_favorites`) |
| Jen env konfigurace | YAML + env |
| Fotokniha (LaTeX, komplexní) | Vynecháno |

---

## 16. Otevřené otázky a rizika k ověření

1. **pgvector v `shared-postgres`** — je `CREATE EXTENSION vector` dostupné? (blokující pro M0)
2. **`halfvec`** vyžaduje pgvector ≥ 0.7 — ověřit verzi; jinak fallback na `vector` (float32).
3. **PhotoPrism `updated:` filtr** — zachytí i změny pouhých metadat? (ověřit empiricky proti
   reálné instanci; fallback `added:` + watermark.)
4. **PhotoPrism token bug** (#4665) — ověřit, že access token funguje na `/api/v1/photos`
   se správným scope a `Content-Type`.
5. **Mapy.com klíč** — vázanost na doménu/referrer není doložena; držet klíč server-side.
6. **HW Pi** — reálná rychlost pure-Go náhledů a HEIC na cílovém Pi; případně zapnout
   `vipsthumbnail` shell-out. Změřit build HNSW indexu (maintenance_work_mem) na Pi vs build
   na boxu/shared serveru.
7. **Inference modely** — potvrdit přesný CLIP checkpoint photo-sorteru (`pretrained` pole),
   aby migrace embeddingů seděla 1:1 (stejný prostor).

---

## 17. Reference

**photo-sorter (lokální):** `internal/fingerprint/embedding.go` (sidecar kontrakt),
`internal/database/postgres/migrations/032_native_photo_management.sql` (schéma),
`internal/config/config.go`, `internal/thumb/thumb.go`, `internal/web/handlers/process.go`,
`.goreleaser.yaml`, `deb/photo-sorter.service`.

**PhotoPrism API:** [REST API intro](https://docs.photoprism.app/developer-guide/api/) ·
[Client Authentication](https://docs.photoprism.app/developer-guide/api/auth/) ·
[Search Filters](https://docs.photoprism.app/user-guide/search/filters/) ·
[internal/api routy](https://pkg.go.dev/github.com/photoprism/photoprism/internal/api) ·
[uid.go](https://github.com/photoprism/photoprism/blob/develop/pkg/rnd/uid.go).

**mapy.com:** [REST API](https://developer.mapy.com/rest-api-mapy-cz/) ·
[Map tiles](https://github.com/mapycom/developer/blob/master/docs/rest-api/map-tiles.md) ·
[Reverse geocoding](https://github.com/mapycom/developer/blob/master/docs/rest-api/reverse-geocoding.md) ·
[Pricing](https://developer.mapy.com/pricing/).

**pgvector / ARM / inference:** [pgvector](https://github.com/pgvector/pgvector) ·
[HNSW vs IVFFlat](https://bigdataboutique.com/blog/hnsw-vs-ivfflat-how-to-choose-the-right-vector-index) ·
[disintegration/imaging](https://github.com/disintegration/imaging) ·
[libvips speed/memory](https://github.com/libvips/libvips/wiki/Speed-and-memory-use) ·
[Immich machine-learning](https://github.com/immich-app/immich/blob/main/machine-learning/README.md) ·
[open_clip pretrained](https://github.com/mlfoundations/open_clip/blob/main/docs/PRETRAINED.md) ·
[mdlayher/wol](https://github.com/mdlayher/wol) · [minio-go](https://pkg.go.dev/github.com/minio/minio-go/v7).

---

## 18. Rozpad do milníků (epiců)

Detailní tasky se zakládají v systému **botka**. Milníky jsou seřazené pro brzy viditelnou UI.

- **M0 — Základy:** repo scaffolding, Go modul, config (YAML+env), DB+migrace, pgvector/halfvec
  (ověřit v shared-postgres), CI/build (goreleaser arm64 .deb), kostra embedded frontendu
  (react-bootstrap+Superhero+i18n), auth/users+sliding sessions, layout + back/history.
- **M1 — Storage & ingest:** layout úložiště, upload (multiupload+mobil), dedup (SHA256+pHash),
  EXIF/GPS, thumbnailer na Pi, CRUD fotek, knihovna s filtry/řazením/stránkováním (viditelná UI).
- **M2 — Joby & embeddings:** persistentní fronta, sidecar client + health/offline, image
  embeddings, podobné fotky, sémantické + fulltext (hybrid) hledání.
- **M3 — Lidé:** face joby, markery/subjekty, IoU matching, auto-clustering, návrhy, přiřazování UX, outliers, stránky osob.
- **M4 — Organizace:** alba, štítky, hromadná editace metadat, per-user oblíbené, mapa (mapy.com proxy), slideshow.
- **M5 — Import/migrace:** PhotoPrism API import + originály + inkrement (PP UID); migrace z photo-sorteru (PS UID, 1:1 embeddingy).
- **M6 — Backup & ops:** S3 backup (originály + dump), durable audit, rate-limiting, metriky, volitelný WoL auto-wake (`internal/wake`), hardening.
- **M7 — Polish:** detail fotky (PP+PS kombo), mobil/tablet, i18n úplnost, slideshow efekty, výkon, nedestruktivní úpravy.

---

## 19. Kvalita, testování a linting

Robustnost a rozšiřitelnost jsou prvotřídní cíl. Každý task (i autonomní v botce) musí
dodržet tato pravidla; **task není hotový s červeným lintem nebo testy.**

### 19.1 Linting (Go)
- **golangci-lint v2**, konfigurace **`.golangci.yml` převzatá a adaptovaná z photo-sorteru**
  (přísná sada ~40+ linterů: `revive`, `gosec`, `errcheck`, `errorlint`, `wrapcheck`,
  `cyclop`, `gocognit`, `funlen`, `dupl`, `goconst`, `gocritic`, `prealloc`, `sqlclosecheck`,
  `bodyclose`, `noctx`, `testifylint`, `thelper`, `usetesting`, `nilerr`, `lll` (120),
  `misspell`, `godot`, `nakedret`, `unparam`, `wastedassign`, …).
- Nastavení mj.: `funlen` 60/40, `gocognit` 15, `goconst` 3, `lll` 120. Exported symboly
  dokumentované (`revive: exported`). `//nolint` jen s odůvodněním (`nolintlint`).
- `gofmt`/`gofumpt` čistý kód.

### 19.2 Testy (Go)
- **Unit testy** pro veškerou business logiku (table-driven, `testify`). Čisté funkce bez I/O
  preferovat → snadno testovatelné.
- **Integrační testy** pro DB repozitáře a HTTP handlery proti **reálnému pgvector Postgresu**
  (test DB `kukatko_test`, DSN v `KUKATKO_TEST_DATABASE_URL`). Harness: aplikuje migrace,
  poskytne čistý stav per test (truncate/transakce + rollback). Když env chybí, integrační
  testy se `t.Skip` (aby rychlá brána `make check` nevyžadovala DB).
- **R2 backend** má integrační testy proti **reálnému S3-kompatibilnímu endpointu**
  (`KUKATKO_TEST_S3_ENDPOINT`, stačí MinIO; volitelně `_BUCKET`/`_REGION`/`_ACCESS_KEY`/`_SECRET_KEY`):
  store/open/stat/delete, materialize + úklid temp souboru (i na error path) a klíč s UTF-8 jménem.
  Bez proměnné se skipnou, stejně jako DB testy. Podepisování URL je čistá funkce → unit testy.
- Externí závislosti (embeddings sidecar, PhotoPrism API, mapy.com, S3) za **rozhraním**
  (interface) → v testech mockované/fake; kontrakt sidecaru ověřit i contract testem proti
  fake serveru.
- Smysluplné pokrytí logiky (ne vanity %). Nové chování = nové/aktualizované testy.

### 19.3 Frontend testy
- **ESLint** (strict) + **Prettier**. **Vitest + React Testing Library** pro komponenty a hooky
  (zejména stav filtrů v URL, i18n, auth flow). Kritické toky (login, upload, hledání) pokrýt.
- (Volitelně M7) **Playwright** E2E pro pár klíčových scénářů.

### 19.4 Make targety a brána
```
make fmt              # gofmt/gofumpt + prettier (jediný cíl, který přepisuje soubory)
make fmt-check        # ověření formátu bez zápisu (golangci-lint fmt --diff + prettier --check)
make vet              # go vet (samostatně; v bráně ho pokrývá govet uvnitř golangci-lint)
make lint             # golangci-lint run + eslint
make lint-fix         # golangci-lint run --fix
make typecheck        # tsc -b --noEmit (frontend)
make test             # unit testy (bez DB, CGO_ENABLED=0, bez -race)
make test-race        # unit testy s race detektorem (CGO_ENABLED=1) — v CI, ne v bráně
make test-integration # integrační testy (vyžaduje KUKATKO_TEST_DATABASE_URL)
make check            # docs-budget + fmt-check + lint + typecheck + test   ← brána (nic nemění)
make build            # frontend build + go build (embed)
```

### 19.5 CI a brána v botce
- **GitHub Actions:** na push/PR spustit `make check` + `make test-race` + `make test-integration`
  se service kontejnerem `pgvector/pgvector:pg17` (env `KUKATKO_TEST_DATABASE_URL`) + frontend
  lint/test. Race detektor je záměrně mimo `make check`, aby brána před commitem zůstala rychlá.
- **Botka verification command projektu = `make check`** → pokud task zanechá červený lint
  nebo testy, dostane stav `needs_review` místo `done`.
- Autonomní agenti pro Go kód používají skill **golang-developer** (přísný lint, dokumentace,
  testy).
