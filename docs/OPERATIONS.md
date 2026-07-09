# Provoz: CLI, konfigurace, build a CI

Popisný referenční přehled příkazů, konfiguračních klíčů, `make` cílů a balíčkování.
**Nejsou to pravidla** — pravidla jsou v [`CLAUDE.md`](../CLAUDE.md). Nový
konfigurační klíč zapiš sem **a** do `config.example.yaml`.

## CLI

<!-- BODY CLI -->
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

## Konfigurační klíče

<!-- BODY CONFIG -->
- **Observability klíče:** `log.level` (debug/info/warn/error, default info, neplatný → chyba při
  startu; `KUKATKO_LOG_LEVEL`) a `metrics.enabled` (bool, default true; vypnuté → `/metrics` se
  nemountuje, request-metriky middleware se neinstaluje, access-log běží dál; `KUKATKO_METRICS_ENABLED`).
- **Storage klíče (`storage.*`, `internal/storage`):** `backend` (`fs` **default** = lokální disk /
  `r2` = privátní Cloudflare R2 bucket; neznámá hodnota → `ErrInvalidStorageBackend` při startu),
  `originals_path` (root originálů, jen pro `fs`), `cache_path` (odvozené artefakty — náhledy,
  video postery), `temp_path` (default `/var/lib/kukatko/tmp`; `r2` přes něj stageuje uploady
  a materializuje objekty pro nástroje, co umí jen jméno souboru — musí se tam vejít **největší
  jeden soubor**, ne knihovna). `KUKATKO_STORAGE_BACKEND`/`_ORIGINALS_PATH`/`_CACHE_PATH`/
  `_TEMP_PATH`.
- **Cloudflare R2 klíče (`storage.r2.*`, čtou se jen když `storage.backend: r2`):** `endpoint`
  (`https://<accountid>.r2.cloudflarestorage.com`), `region` (R2 bere jen `auto`, default),
  `bucket` (**drž ho privátní** — objekty servíruje edge Worker, který ověřuje podpis URL),
  `access_key`/`secret_key` (R2 API token), `media_base_url` (doména Workeru, pod kterou se razí
  podepsané URL — `https://kukatko-media.panbotka.cz`), `url_signing_secret`
  (+ `url_signing_secret_previous`) a `url_ttl` (default `1h`, musí být kladné). Env:
  `KUKATKO_STORAGE_R2_ENDPOINT`/`_REGION`/`_BUCKET`/`_ACCESS_KEY`/
  `_SECRET_KEY`/`_MEDIA_BASE_URL`/`_URL_SIGNING_SECRET`/`_URL_SIGNING_SECRET_PREVIOUS`/`_URL_TTL`.
  Validace `ErrIncompleteR2Config` **při startu**: backend `r2` vyžaduje všechny klíče kromě
  `url_signing_secret_previous` (chybějící jsou vyjmenované v chybě — jen jména, nikdy hodnoty)
  a kladné `url_ttl`. Tajemství ani access key se nikdy nelogují a neobjeví se v chybě.
  **Worker sám v téhle repo není** — bucket, jeho zdroják, bindingy i hostname definuje a nasazuje
  Terraform v infra repu (root modul `cloudflare-r2/`). Rotace podpisového tajemství proto sahá do
  **dvou repozitářů** — postup níž.
- **⚠️ Náhledy do bucketu zatím nikdo nenahrává.** Při `storage.backend: r2` razí API `thumb_url`
  (a routa `/photos/{uid}/thumb/{size}` redirectuje) na objektový klíč
  `thumb/aa/bb/cc/<hash>_<size>.jpg`, jenže `thumb.Thumbnailer` píše všechny velikosti **lokálně**
  do `storage.cache_path` — pro oba backendy stejně. `storage.Storage.Store` neumí zapsat objekt na
  **zvolený** klíč (odvozuje si ho z `taken_at` + jména souboru), takže publikaci cache musí přinést
  nová metoda rozhraní. Než vznikne, musí nasazení na R2 zrcadlit náhledy do bucketu **mimo aplikaci**
  (např. `rclone sync` z `cache_path`), jinak Worker odpoví na každou dlaždici 404. Originálů a videa
  se to netýká — ty do bucketu zapisuje `Store` při importu.
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

### Rotace `url_signing_secret` (procedura přes dva repozitáře)

Kukátko media URL **podepisuje**, edge Worker je **ověřuje**. Worker žije v **infra repu**
(root modul `cloudflare-r2/`, nasazuje ho Terraform), takže **stejná hodnota** musí být
nakonfigurovaná na obou stranách: tady jako `storage.r2.url_signing_secret`, tam jako binding typu
`secret_text` na Workeru. Ověřují se **obě** tajemství naráz (`url_signing_secret` +
`url_signing_secret_previous`), podepisuje se vždy tím současným — rotace proto nemá okno
rozbitých URL, **když se dodrží pořadí**:

1. Stávající hodnotu `url_signing_secret` přesuň do `url_signing_secret_previous` — na obou
   stranách (Kukátko i Worker v infra repu).
2. Novou hodnotu dej do `url_signing_secret` — opět na obou stranách.
3. Nasaď **obě** strany (`terraform apply` v `cloudflare-r2/`, restart Kukátka). Na jejich pořadí
   nezáleží: dokud obě znají staré i nové tajemství, ověří se URL podepsané kterýmkoli z nich.
4. Počkej, až vyprší **poslední už vydaná URL** — tedy aspoň `url_ttl` (default **1 h**) od chvíle,
   kdy Kukátko přestalo podepisovat starým tajemstvím.
5. Teprve pak starou hodnotu zahoď: vyprázdni `url_signing_secret_previous` na obou stranách
   a znovu nasaď.

Zkratka přes kroky 1–2 (přepsat `url_signing_secret` bez uložení staré hodnoty do `_previous`)
**403ne každou fotku**, na kterou už prohlížeč nebo API odpověď drží podepsané URL. Samotný kontrakt
podpisu (zpráva `"<key>\n<expiry>"`, HMAC-SHA256, hex) je zamrzlý v golden vektorech
`internal/storage/testdata/url_signature_vectors.json`; testují se proti nim obě strany, takže se
algoritmus nedá změnit jen v jedné z nich.

## Make cíle a CI/CD

<!-- BODY MAKE -->
- **Make cíle:** `fmt` (golangci-lint fmt + Prettier `--write`), `vet`, `lint` (golangci-lint
  + ESLint + Prettier `--check`), `lint-fix`, `test` (Go unit `-race` + Vitest; Go vyžaduje
  cgo/gcc), `test-integration` (tag `integration` + `KUKATKO_TEST_DATABASE_URL`, `-p 1` —
  integrační balíky sdílí jednu test DB, takže běží sériově; testy R2 backendu navíc chtějí
  `KUKATKO_TEST_S3_ENDPOINT` — bez ní se skipnou, viz `docs/DEVELOPMENT.md`), `check`
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

