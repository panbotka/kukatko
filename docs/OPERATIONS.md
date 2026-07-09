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
  při `storage.backend: r2` se originály **kopírují bucket→bucket server-side** a záloha do
  **téhož** bucketu, ve kterém leží knihovna, skončí `errBackupSameBucket`;
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
  **`kukatko storage`** (operace nad úložištěm originálů — `internal/storagemigrate`):
  `storage migrate-to-r2` (jednorázový **resumovatelný** přesun knihovny do R2, viz níže),
  **`kukatko ctl`** (vzdálený klient nad HTTP API běžící instance — `internal/ctl`; jediný subkomand,
  který **nesahá na DB ani disk**, viz níže),
  `kukatko version` (verze + commit). Persistentní flag `--config <path>` určuje YAML config.
  `server.New(addr, server.WithAPI(register))` mountuje route-skupiny pod `/api/v1`.

### `kukatko storage migrate-to-r2`

Jednorázový přesun ~120 GB originálů (a už nacachovaných náhledů) z lokálního disku do R2
bucketu. Běží hodiny, smí být kdykoli zabitý a znovu spuštěný. Klíče objektů = `file_path`
z Postgresu, takže se nic nepřeklíčovává — bucket dostane stejný layout jako disk.

Potřebuje `storage.r2.{endpoint,bucket,access_key,secret_key}` a `storage.temp_path`, jinak
skončí na `errStorageR2NotConfigured` (zpráva jmenuje klíče, nikdy jejich hodnoty).
`storage.r2.media_base_url` ani podpisový secret nepotřebuje — příkaz jen zapisuje objekty,
nerazí URL. Pouští se **ještě před** přepnutím `storage.backend` na `r2`.

| Flag | Default | Význam |
| --- | --- | --- |
| `--dry-run` | `false` | jen spočítá, kolik fotek/objektů/bajtů by se přesunulo; nesahá na bucket, DB ani disk |
| `--delete-local` | `false` | smaže lokální originál — až **po** commitu řádku, nikdy u fotky, která neprošla verifikací |
| `--concurrency` | `2` | kolik fotek se nahrává paralelně (schválně nízko: malá VPS, FD a paměť) |
| `--batch-size` | `200` | kolik čekajících fotek se načte z katalogu najednou |

**Pořadí kroků na fotku je závazné:** nahraj objekty → přečti si je zpátky (velikost + SHA256) →
commitni řádek (`photos.storage_migrated_at`) → teprve pak smaž lokální originál. Náhledy se
nemažou nikdy (jsou regenerovatelné z originálu). Fotka, která selhala, zůstane bez razítka
i s originálem na disku a příští běh ji zkusí znovu.

**Resume:** kurzorem je `photos.storage_migrated_at` (migrace `0019`) — stejné high-watermark
pravidlo jako `internal/importer`, jen per řádek, protože při paralelismu doběhne fotka N+1
běžně dřív než N. Hotová fotka se přeskočí; objekt, který už v bucketu leží se správnou
velikostí i digestem, se znovu nenahrává.

**Chyby:** per-fotkové selhání se sbírá a vypíše až na konci (běh pokračuje), systémové selhání
(špatné klíče, chybějící bucket → `storage.IsSystemic`) běh **okamžitě** zastaví. Exit ≠ 0, když
běh spadl nebo některá fotka selhala.

**Průběh** se tiskne každých 15 s: hotové fotky, nahrané objekty a bajty, přeskočené, selhané
a odhad zbytku — hodiny běžící job, který mlčí, je rozbitý job.

**Billing:** R2 účtuje Class A operaci za každý zápis a milion měsíčně je zdarma → plná migrace
~100 000 objektů vyjde na nulu. **Opakované plné nahrání už ne** — proto se příkaz nejdřív ptá
bucketu, co už má (`HEAD` = Class B, 10 M/měsíc zdarma), a zapisuje jen chybějící.

```bash
kukatko storage migrate-to-r2 --dry-run                      # kolik toho zbývá
kukatko storage migrate-to-r2 --concurrency 4                # nahraj, originály nech na disku
kukatko storage migrate-to-r2 --delete-local                 # nahraj a uklízej za sebou
```

### `kukatko ctl` — vzdálený klient API

Ostatní subkomandy sahají přímo na databázi a filesystem. `ctl` je opak: mluví s **běžící**
instancí přes její `/api/v1`, autentizuje se **API tokenem** (`Authorization: Bearer kkt_…`,
viz [`docs/API.md`](API.md)) a nepotřebuje ani `database.url`, ani přístup k originálům.
Slouží k ovládání produkce z terminálu — a skrz ten terminál i agentem. Je to levnější
v tokenech než MCP server: do kontextu modelu se nenačítá žádné tool schema, jen krátký
příkaz a úzký výsledek. **Proto je výstup kompaktní** — to je celý smysl.

**Jeden binár, dvě jména.** Přes symlink pojmenovaný `kukatkoctl` se úroveň `ctl` implikuje
(detekce z `os.Args[0]`), takže `kukatkoctl photos list` == `kukatko ctl photos list`:

```bash
ln -s /usr/local/bin/kukatko /usr/local/bin/kukatkoctl
```

#### Konfigurace klienta

Kontexty ve stylu `kubectl` žijí v **`~/.config/kukatko/ctl.yaml`** (ctí `XDG_CONFIG_HOME`).
Se serverovou konfigurací (`internal/config`, `config.yaml`) **nemají nic společného** — ta
popisuje server a o vzdáleném endpointu nic neví.

```yaml
current-context: prod
contexts:
  - name: prod
    server: https://kukatko.example.com   # kořen webu, BEZ /api/v1 (klient si ho doplní)
    token: kkt_ab12_…                     # plaintext tokenu; soubor je vždy 0600
```

Soubor se zapisuje **atomicky a vždy s módem `0600`** (adresář `0700`); existující
world-readable soubor se před zápisem utáhne. **Token se nikdy nikam nevypisuje** — ani do
logu, ani do chybové hlášky, ani do `ctl config list`.

| Příkaz | Význam |
| --- | --- |
| `ctl config set-context <name> --server <url> [--token <t> \| --token-stdin] [--current]` | vytvoří/aktualizuje kontext; první vytvořený se stane aktuálním. Vynechané pole se zachová (změna URL nesmaže token). |
| `ctl config list` (alias `get-contexts`) | vypíše kontexty; u tokenu jen `stored`/`not set` |
| `ctl config use-context <name>` (alias `use`) | přepne aktuální kontext |

`--token` je vidět v `ps` celému stroji — **preferuj `--token-stdin`**:
`printf '%s' "$TOKEN" | kukatkoctl config set-context prod --server https://… --token-stdin`.

**Env přebíjí aktivní kontext, po jednotlivých polích:** `KUKATKO_SERVER` a `KUKATKO_TOKEN`.
Samotné `KUKATKO_TOKEN` tedy přecredentialuje uložený kontext, aniž bys sahal na soubor.
Bez souboru i bez kontextu stačí obě proměnné. Flag `--context <name>` vybere jiný než
aktuální kontext, `--ctl-config <path>` jiný soubor.

#### Výstup a exit kódy

`-o table` (default) je kompaktní tabulka + jeden souhrnný řádek (`3 of 42 photos · offset 0 ·
next offset 3`, u hledání navíc `mode`/`degraded`). Prázdný výsledek vypíše jediný řádek
`no photos found` **bez hlavičky**. `-o json` tiskne **JSON serveru beze změny** (žádný
re-marshal) pro strojové zpracování; `-o yaml` neexistuje.

Exit `0` při úspěchu, nenulový při HTTP i transportní chybě. **`401`** dá krátkou akční
hlášku (token chybí / expiroval / byl revokován + jak vyrobit nový), ne stacktrace a ne výpis
těla odpovědi.

#### `ctl photos`

| Příkaz | Význam |
| --- | --- |
| `ctl photos list` | stránka `GET /photos` |
| `ctl photos get <uid>` | detail `GET /photos/{uid}` (+ soubory, alba, štítky) |
| `ctl photos search <query>` | `GET /search?q=…&mode=…` |

Filtry sdílí `list` i `search`, až na ty označené „jen `list`" / „jen `search`".
`search` řadí dle relevance, takže `--sort`/`--order` nenabízí; `--favorite` nenabízí proto,
že `GET /search` ten parametr vůbec nečte — nabízet ho by tiše vracelo nefiltrovaný výsledek.

| Flag | Default | Význam |
| --- | --- | --- |
| `--limit` | `0` (= server default 100) | fotek na stránku, server stropuje na 500 |
| `--offset` | `0` | kolik přeskočit; další offset říká souhrnný řádek |
| `--sort` (jen `list`) | server default | `newest`/`oldest`/`taken_at`/`added`/`title`/`size`/`rating` |
| `--order` (jen `list`) | dle `--sort` | `asc`/`desc` |
| `--year` | `0` (bez filtru) | kalendářní rok. **API rok nezná** — klient ho překládá na `taken_after`/`taken_before` |
| `--album` / `--label` | — | scope na uid alba/štítku |
| `--favorite` (jen `list`) | `false` | jen vlastní oblíbené |
| `--archived` | server default (`false`) | `true` = včetně archivu, `only` = jen koš |
| `--mode` (jen `search`) | `hybrid` | `fulltext`/`semantic`/`hybrid` |

Je-li box (embeddings sidecar) offline, `semantic`/`hybrid` spadne na fulltext a souhrnný
řádek to řekne (`degraded`).

```bash
kukatkoctl photos list --year 2024 --limit 5
kukatkoctl photos list --album alb1a2b3 --sort title -o json | jq '.photos[].uid'
kukatkoctl photos get pht01h2j3
kukatkoctl photos search "západ slunce nad jezerem" --mode semantic
KUKATKO_SERVER=http://localhost:8080 KUKATKO_TOKEN=kkt_… kukatkoctl photos list
```

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
- **Backup klíče (`backup.*`, `internal/backup`):** `backup.s3.*` popisuje **druhý, nezávislý
  bucket** — `endpoint`, `region`, `bucket`, `access_key`/`secret_key` a `path_style` (bool,
  default false; MinIO a většina self-hosted S3 ho chce). Nesdílí **nic** se `storage.r2.*`, takže
  záloha může žít v jiném účtu i u jiného poskytovatele; **nepředpokládej, že oba jsou R2.** Dál
  `backup.schedule` (5-pole cron / `@daily`/`@every`; prázdné vypne plánovač) a `backup.retention`
  (kolik posledních **dumpů** nechat, ≤ 0 = nechat vše). Env: `KUKATKO_BACKUP_S3_ENDPOINT`/
  `_REGION`/`_BUCKET`/`_ACCESS_KEY`/`_SECRET_KEY`/`_PATH_STYLE`, `KUKATKO_BACKUP_SCHEDULE`,
  `KUKATKO_BACKUP_RETENTION`.
  **Odkud se berou originály, určuje `storage.backend`:** `fs` → `backup.DiskOriginals` prochází
  `storage.originals_path` a streamuje soubory nahoru; `r2` → `backup.BucketOriginals` vylistuje
  primární bucket a nechá **backup endpoint objekt zkopírovat server-side** (`CopyObject` přes
  `ComposeObject`, takže i objekt > 5 GiB projde multipart copy) — payload **nikdy neteče přes
  aplikaci**, což je celý smysl na VPS, jehož disk knihovnu neunese.
  **Důsledek pro oprávnění:** server-side copy se posílá na `backup.s3.endpoint` s primárním
  bucketem jako zdrojem, takže `backup.s3.access_key` musí umět **číst `storage.r2.bucket`**
  (typicky tentýž S3 service / účet, nebo cross-account grant). `retention` prořezává **jen
  prefix `db/`** — originály se **nikdy neexpirují** a smazání v primárním bucketu se do zálohy
  **nepropaguje**; kopie je čistě aditivní. **Raději hlasitě spadnout než potichu zazálohovat
  nic:** chybějící `backup.s3.{endpoint,bucket}` → `errBackupNotConfigured`, míření zálohy na
  primární bucket → `errBackupSameBucket` (obojí ve wiringu, `cmd/kukatko/backup.go`). Chybějící
  `storage.r2.bucket` zachytí už `config.Load` (`ErrIncompleteR2Config`) při startu; sentinely
  `backup.ErrNoSourceStore`/`ErrNoSourceBucket` proto hlídají jen wiring bug uvnitř balíčku.
  Verzování objektů **neexistuje**, druhý bucket je jediná ochrana — viz [`RESTORE.md`](RESTORE.md).
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

