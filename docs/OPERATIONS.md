# Operations: CLI, configuration, build, and CI

A descriptive reference overview of commands, configuration keys, `make` targets, and packaging.
**These are not rules** — the rules live in [`CLAUDE.md`](../CLAUDE.md). Write a new
configuration key both here **and** into `config.example.yaml`.

## CLI

<!-- BODY CLI -->
- **CLI:** `kukatko serve` (loads the config, **runs migrations**, **bootstraps the admin**, starts
  the hourly cleanup of expired sessions, the **background worker** (`internal/worker`) that
  processes the job queue, and the **scheduled trash cleanup** (`internal/trash` `RunPurge`, every 6 h —
  permanently deletes photos archived longer than `trash.retention_days`, default **365 days (1 year)**;
  retention ≤ 0 disables it),
  the **scheduled S3 backup** (`internal/backup` `RunSchedule` on `backup.schedule`; only if
  `backup.s3.{endpoint,bucket}` is configured), and the **optional Wake-on-LAN auto-wake of the box**
  (`internal/wake` `Run`, every minute; only if `embedding.wake.enabled`, otherwise fully inert),
  then listens on `web.host:web.port`, default
  `0.0.0.0:8080`; `GET /healthz` → 200 JSON `{"status":"ok","version":{…}}`, **`GET /metrics`**
  Prometheus (outside `/api/v1`, unauthenticated; only when `metrics.enabled`), the auth/admin API
  under `/api/v1` — see below, and all other paths are served by the **embedded SPA** with a fallback to
  `index.html`; `serve` additionally sets up **structured logging** (`obs.Setup`, JSON slog to
  stderr, level `log.level`) and — when `metrics.enabled` — builds the `metrics.Registry`, registers
  the DB-pool + job-queue-depth collectors, and inserts the request-metrics + access-log middleware via
  `server.WithMiddleware`/`WithMetricsHandler`), `kukatko migrate` (runs pending migrations on their own and exits),
  `kukatko migrate photosorter` (synchronous read-only incremental **data migration from photo-sorter** —
  `psimport`; applies DB migrations, then `Service.Migrate`; needs `import.photosorter.dsn`, otherwise
  `errPSMigrateNotConfigured`; for ops/cron without a running server),
  `kukatko import photoprism` (synchronous read-only incremental import from PhotoPrism — `ppimport`;
  needs `import.photoprism.base_url`, otherwise an error; for ops/cron without a running server;
  a **scoped run** = the library can be migrated in slices: `--album <photoprism-uid>` (an album's photos),
  `--label <slug>` (photos with that label, e.g. `sdh`), `--person <jméno>` (photos the given
  subject appears in, e.g. `"Aleš Kozák"`), `--year <YYYY>` (photos taken in that year). Flags **combine
  and narrow the run** (the album goes into `s=`, the rest into `q=` as ANDed terms, verified against production:
  `--album X --year 1985`). A scoped run pulls its slice in **full, regardless of photo age**
  (it ignores the watermark) and **transfers each photo complete**: it creates and attaches **all** the albums the
  photo is in, plus **all** its labels (with `source`/`uncertainty` from the source) — including the ones the
  scope did not name, so a photo from three albums imported via `--album` into one ends up in all three
  (this costs 1 extra photo-detail request; a full run does not do this and maps the structure by walking the
  album/label catalog). People seed the face markers of imported photos. The run **does not advance the watermark**, so
  a later full import still sees all photos. An unknown album uid → `ErrAlbumNotFound`, an unknown label
  slug → `ErrLabelNotFound` (verified **before** downloading), a nonsensical year → `ErrInvalidYear`, no
  flag → a full incremental run. It is idempotent — a re-run does not create a second album, label, or membership.
  Used to verify the import against production and to pre-pull part of the library),
  **`kukatko import dir <path>`** (uploads a **directory from disk** — `internal/dirimport`; see below),
  `kukatko backup` (synchronous one-off **S3 backup** — `internal/backup`; pg_dump + sync of
  originals + retention; needs `backup.s3.{endpoint,bucket}`, otherwise `errBackupNotConfigured`;
  under `storage.backend: r2` the originals are **copied bucket→bucket server-side** and a backup into
  the **same** bucket the library lives in fails with `errBackupSameBucket`;
  for ops/cron without a running server),
  **`kukatko restore`** (the restore/disaster-recovery tree — `internal/backup`; shares `backup.s3.*`,
  otherwise `errRestoreNotConfigured`; for ops/cron without a running server): `restore list` (dumps in
  the bucket), `restore db [--dump KEY] [--yes] [--verify]` (**destructive** DB restore via
  `pg_restore` streamed from S3 + idempotent re-migration; without `--yes` → `errRestoreNotConfirmed`),
  `restore originals` (downloads missing originals, skips by key+size, resumable),
  `restore verify` (integrity report of photos in the DB vs originals on disk); runbook
  [`docs/RESTORE.md`](RESTORE.md),
  **`kukatko maintenance`** (library integrity check & repair — `internal/maintenance`; for
  ops/cron without a running server, applies migrations and builds a service shared with the admin API):
  `maintenance scan` (read-only integrity report — disk↔DB drift + missing derived data) and
  `maintenance repair` with the flags `--thumbnails`/`--embeddings`/`--faces`/`--phashes`/`--import-orphans`
  (each opt-in; thumbnails/phashes enqueue `thumbnail` jobs drained by a running server's worker,
  embeddings/faces backfill, orphan import synchronously via the upload pipeline; a no-op without any flag;
  the **retention purge of old audit logs** is separate, only via HTTP/UI, not the CLI — the maintainer calls
  `POST /api/v1/maintenance/audit/purge` `{older_than_days}` (`internal/maintenanceapi`), which deletes audit
  entries older than `now − older_than_days` and **audits itself** (`audit.purge`, so that deleting the trail
  stays traceable); the admin UI has a „Vymazat audit log" card on the Údržba page with presets
  (3/6 months, 1/2 years) or a custom number of days plus a confirmation),
  **`kukatko sidecar`** (metadata sidecars — `internal/sidecarjob`; the terminal entry point into the export
  that makes curation data independent of the database): `sidecar backfill` enqueues a `sidecar` job for
  every photo with a **missing or stale** sidecar, `--all` forces a full re-run over every
  unarchived photo (this catches up changes outside the photo's own row — album membership, a label).
  It only **enqueues**; the files are written by a running server's worker (the same queue, same handler, same
  dedup as for live edits, so the backfill cannot race the user), which is why it prints the number of
  scheduled jobs. Idempotent — over a library with up-to-date sidecars it schedules zero — so it
  can be run from cron and, above all, **before any risky operation** (a migration, upgrade, restore drill),
  which is exactly the moment a person is at the terminal. When `sidecar.enabled: false`, the command
  **fails** instead of a silent “0 scheduled”. The full format is in [`docs/RESTORE.md`](RESTORE.md),
  the HTTP counterpart is `POST /api/v1/process/sidecars`,
  **`kukatko storage`** (operations over the storage of originals — `internal/storagemigrate`):
  `storage migrate-to-r2` (a one-off **resumable** move of the library to R2, see below),
  **`kukatko ctl`** (a remote client over the HTTP API of a running instance — `internal/ctl`; the only subcommand
  that **touches neither the DB nor disk**, see below),
  `kukatko version` (version + commit). The persistent `--config <path>` flag selects the YAML config.
  `server.New(addr, server.WithAPI(register))` mounts the route groups under `/api/v1`.

### `kukatko import dir <path>`

Walks a directory on disk (recursively) and uploads every media file into the library **through the same
pipeline as a browser upload** (`internal/ingest`): stream + SHA256, metadata, the original into
`YYYY/MM`, thumbnails, `image_embed`/`face_detect` jobs onto the queue. The source directory is **read only** —
originals are copied, never moved or modified. For ops/cron without a running server (it applies
migrations and opens the DB itself); the run is recorded in `import_runs` as source `folder`, so it is visible
in `/import` and in `GET /import/runs` alongside PhotoPrism and photo-sorter runs.

**It is always safe to run again.** Identity is the SHA256 of the content: anything already in the library is reported
as a duplicate (even under a different name — the listing shows both paths) and nothing is written. The run is also
resumable — each file is committed separately, so a crash or Ctrl-C leaves the already-imported photos
in the library and the next run finishes the rest (an interrupted run is closed as `failed`). An error on a single file
is logged and **processing continues**; the command exits with a **nonzero exit code** when at least one file
failed, so a script can tell.

#### Sidecars: Google Takeout (`.json`) and Apple (`.xmp`)

A Google Photos (Takeout) export carries metadata **next to** the photo, not inside it: the exported JPEG
usually has its EXIF stripped on re-encode, so the real capture date, caption, and GPS live only in the `.json`
file beside it. Importing such a folder naively = losing everything; that is why the import **reads** sidecars
(disable with `--no-sidecars`).

- **Co se přenáší.** Takeout: `photoTakenTime` → `taken_at`, `description` → popis,
  `geoData`/`geoDataExif` → `lat`/`lng`/`altitude` (**přesná 0/0 = neznámo**, ne bod v Guinejském
  zálivu), `favorited` → oblíbená u **importujícího uživatele** (oblíbené jsou v Kukátku per-user),
  `people[].name` → jen do metadat (Google nemá face boxy, **subjekt ani marker z nich nevznikne**).
  Apple `.xmp` (přes `exiftool`): datum, GPS, titulek/popis, klíčová slova, hodnocení (per-user),
  autor. `.AAE` je popis **editace**, ne metadata → ignoruje se.
- **Precedence.** EXIF samotného souboru je primární a sidecar **doplňuje mezery** — s výjimkou, kvůli
  které to celé existuje: když EXIF datum leží **víc než 24 h za** datem ze sidecaru, je to datum
  *exportu* (Takeout ho při re-encode zapíše do `DateTimeOriginal`) a **vyhrává sidecar**. Sidecar
  vyhrává i nad datem hádaným z názvu souboru. Zdroj se zapíše do `taken_at_source` jako `sidecar`.
  **Co už uživatel v Kukátku upravil, se nikdy nepřepíše** — import doplňuje díry.
- **Alba se z exportu nezakládají.** Struktura složek a `metadata.json` alba jsou plné
  automaticky generovaného balastu z telefonu; členství v albu se řeší přes `--album`.
- **Re-run opraví starý import.** Složku, která se naimportovala dřív, než se sidecary četly, stačí
  naimportovat znovu: soubory se nahlásí jako duplicity, nic se nezaloží, ale **doplní se chybějící**
  datum, popis a GPS. Třetí běh už nezapíše nic.
- **Co se nespárovalo, se pojmenuje** — na konci běhu:
  `sidecars: matched=… applied=… unreadable=… unmatched=… media-without-sidecar=…` a pod tím výpis
  konkrétních cest (max 10, pak `… and N more`): sidecar, který nenašel fotku; fotka bez sidecaru
  (hlásí se **jen v adresářích, kde nějaké sidecary jsou** — u složky z foťáku by to byl šum);
  a sidecar, který nešel přečíst (fotka se **stejně** naimportuje, přijde jen o metadata).
  Tichý nesoulad je způsob, jak přijít o desetiletí dat — proto se hlásí, nehádá se.
  Párování jmen přežije všechny varianty Takeoutu (`IMG.jpg.json`,
  `IMG.jpg.supplemental-metadata.json` i její useknuté formy, `IMG_1234.jp.json`,
  `IMG_1234.jpg(1).json` ↔ `IMG_1234(1).jpg`); **nejednoznačná** useknutá shoda raději nespáruje nic.

Přeskakuje (a počítá po důvodech, nikdy kvůli tomu neselže): tečkové soubory a adresáře, `@eaDir`,
`__MACOSX`, `Thumbs.db`, `.DS_Store`, `desktop.ini`, sidecary (`.xmp`, `.json`, `.aae`, `.thm` — jako
**média** se neimportují; metadata se z `.xmp`/`.json` čtou, viz výše),
prázdné soubory a formáty, které nejsou podporovaný obrázek ani video (HEIC/RAW/video **podporované
jsou**). **Symlinky se přeskakují, nenásledují se** (walk se tak nemůže zacyklit); rozbalí se jen
samotný `<path>`, takže mířit příkazem na symlinkovaný adresář funguje. Soubor bez EXIF a bez data
v názvu se naimportuje s `taken_at` = NULL — datum se **nikdy nedovozuje z mtime** (špatné datum je
horší než žádné).

Že je embedding sidecar (box) offline, je v pořádku a čekané: joby zůstanou ve frontě v Postgresu
a doberou se, až bude box zase dostupný — souhrn to tak i napíše.

| Flag | Default | Význam |
| --- | --- | --- |
| `--album <uid\|název>` | – | zařadí všechny fotky do alba; uid se použije, název se dohledá a **nenajde-li se, album se založí** (platí i pro duplicity → tak se opraví zapomenutý `--album`) |
| `--labels <a,b,c>` | – | připojí štítky (podle názvu; co neexistuje, se založí) všem fotkám běhu |
| `--recursive`, `-r` | `true` | projde i podadresáře |
| `--no-recursive` | `false` | jen plochý adresář (s `--recursive` se **vylučuje**) |
| `--dry-run` | `false` | jen ohlásí, co by udělal (nový / duplicita / přeskočeno + důvod, včetně **celého sidecar reportu**) — **nezapíše nic**, ani `import_runs` |
| `--no-sidecars` | `false` | ignoruje metadata vedle médií (Takeout export pak přijde **bez dat a popisků**) |
| `--concurrency N` | `3` | kolik souborů se nahrává paralelně; **strop 8** (thumbnailing velkých fotek je paměťově drahý a box má 16 GB sdílených se vším ostatním) |
| `--uploader <user>` | bootstrap admin | uživatelské jméno vlastníka naimportovaných fotek; bez něj `auth.bootstrap_admin_username`, jinak první admin |

Výpis: řádek na soubor (`[12/2000] imported 2014/IMG_0001.JPG (sidecar: IMG_0001.JPG.json)`) a na
konci souhrn `imported=… duplicates=… skipped=… failed=…` + rozpad přeskočených po důvodech,
**sidecar report** (viz výše) a doba běhu.

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
`no photos found` / `no albums found` / … **bez hlavičky**. `-o json` tiskne **JSON serveru beze
změny** (žádný re-marshal) pro strojové zpracování; `-o yaml` neexistuje.

**Výjimka pro `204 No Content`.** Kde API nevrací tělo (attach/detach štítku, oblíbené,
hodnocení), není co propustit beze změny — `-o table` vypíše jednu větu a `-o json` jediný
payload, který si CLI vyrábí samo: `{"status":"ok","message":"photo pht01 favorited"}`. Pipeline
tak pozná úspěch od chyby.

Exit `0` při úspěchu, nenulový při HTTP i transportní chybě. **`401`** dá krátkou akční
hlášku (token chybí / expiroval / byl revokován + jak vyrobit nový). **`403`** (viewer sáhl na
mutaci) řekne **rovnou, že nestačí role** — mutace chtějí `editor`/`admin`/`ai`, viewer jen čte.
Role **`ai`** je automat na API token: má zápis jako editor **plus** import (`POST /import/*`),
ale ostatní admin akce (uživatelé, zálohy, jobs, údržba, procesy, audit, systém) vrací `403`.
Ani jedna nevypisuje stacktrace, tělo odpovědi, ani token.

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

#### `ctl albums`

Alba a jejich členství (`internal/organizeapi`). Výpis smí kdokoli přihlášený; **create a členství
chtějí `editor`/`admin`**.

| Příkaz | Význam |
| --- | --- |
| `ctl albums list` | `GET /albums` — **holé `{"albums":[…]}`, bez stránkování**, každé album s počtem fotek |
| `ctl albums get <uid>` | `GET /albums/{uid}`; detail **neposílá** `photo_count`, sloupec proto chybí |
| `ctl albums create <title>` | `POST /albums`; `--description`, `--type`, `--order-by`, `--cover`, `--private` |
| `ctl albums add-photos <album-uid> [<photo-uid>…]` | `POST /albums/{uid}/photos` — přidá **za** stávající |
| `ctl albums remove-photos <album-uid> [<photo-uid>…]` | `DELETE /albums/{uid}/photos`; nečlen = no-op |

`--type` je `album` (default), `folder`, `moment`, `state` nebo `month`; ručně dává smysl jen
`album`, zbytek generuje server. `add-photos`/`remove-photos` čtou uidy z argumentů, nebo **ze
stdin**, když žádné nejsou (viz *Velké dávky* níže), a posílají je v **jednom** požadavku.
V tabulce vypíšou jeden řádek (`album alb1a2b3 now holds 12 photos`), `-o json` celé nové pořadí.

#### `ctl labels`

Štítky a jejich navěšení na fotky (`internal/organizeapi`). Výpis kdokoli; zbytek `editor`/`admin`.

| Příkaz | Význam |
| --- | --- |
| `ctl labels list` | `GET /labels` — **holé `{"labels":[…]}`**, řazeno dle priority |
| `ctl labels get <uid>` | `GET /labels/{uid}` |
| `ctl labels create <name>` | `POST /labels`; `--priority` |
| `ctl labels attach <label-uid> <photo-uid>` | `POST /labels/{uid}/photos`; `--source`, `--uncertainty` |
| `ctl labels detach <label-uid> <photo-uid>` | `DELETE /labels/{uid}/photos`; nenavěšený = no-op |

`--source` je `manual` (default), `ai` nebo `import`. Vynechaný se do těla **neposílá**, ať si
server dosadí vlastní default.

#### `ctl subjects`

Osoby, zvířata a další subjekty face pipeline (`internal/peopleapi`). **Celý strom je read-only** —
zakládat a editovat subjekty patří do UI, kde je vidět galerie obličejů a rozhodnutí jde ověřit.

| Příkaz | Význam |
| --- | --- |
| `ctl subjects list` | `GET /subjects` — **holé `{"subjects":[…]}`**, s počtem markerů |
| `ctl subjects get <uid>` | `GET /subjects/{uid}` |
| `ctl subjects photos <uid>` | `GET /subjects/{uid}/photos`; `--limit`/`--offset` |

Galerie subjektu je jediný stránkovaný subject endpoint a vrací **obálku `/photos`**, takže se
tiskne jako fotolist. Filtry katalogu nečte, tak je `ctl` ani nenabízí.

#### `ctl favorites` a `ctl rating`

Oblíbené i hodnocení jsou **per-user**, ne globální: scopne je token, ne parametr. Smí je proto
měnit **i viewer** — na svoje.

| Příkaz | Význam |
| --- | --- |
| `ctl favorites list` | `GET /favorites`; obálka `/photos` + filtry jako `photos list` (bez `--favorite`) |
| `ctl favorites add <uid>` | `PUT /photos/{uid}/favorite` (idempotentní, `204`) |
| `ctl favorites remove <uid>` | `DELETE /photos/{uid}/favorite` (idempotentní, `204`) |
| `ctl rating set <uid> [<0-5>]` | `PUT /photos/{uid}/rating`; `--flag none\|pick\|reject` |
| `ctl rating clear <uid>` | `DELETE /photos/{uid}/rating` (idempotentní) |

Hvězdy a flag jsou **nezávislé**: co u `rating set` vynecháš, to server nechá být — musíš ale zadat
aspoň jedno. `ctl favorites list` parametr `favorite` neposílá; endpoint se scopne sám.

#### `ctl bulk`

Jedna metadatová editace na mnoho fotek (`POST /photos/bulk`, `editor`/`admin`).

```
ctl bulk [<photo-uid>…] [operace…] [--yes]
```

**Celá dávka jde v jednom požadavku**, protože ji server aplikuje v **jedné transakci** — smyčka
po fotkách by atomicitu vyměnila za N transakcí a N audit řádků. Server stropuje dávku na 1000 fotek
(`413`). Uidy se berou z argumentů, nebo **ze stdin**, když žádné nejsou; ze stdin se čtou čtyři
tvary: obálka `{"photos":[…]}` (přesně to, co tiskne `ctl photos list -o json`), holé JSON pole uidů,
holé pole objektů s `uid`, nebo prostý seznam oddělený bílými znaky. Uidy se trimují a **deduplikují**.

| Flag | Význam |
| --- | --- |
| `--add-album` / `--remove-album` | uid alba; opakovatelné |
| `--add-label` / `--remove-label` | uid štítku; opakovatelné |
| `--set-caption` / `--clear-caption` | titulek fotky |
| `--set-description` / `--clear-description` | popis |
| `--location "lat,lng"` / `--clear-location` | GPS pozice |
| `--favorite[=false]` | oblíbená (per-user) |
| `--archive` / `--unarchive` | přesun do koše / zpět |
| `--rating 0..5` | hvězdy (per-user) |
| `--flag none\|pick\|reject` | cull flag (per-user) |

Flagy, jejichž „nezadáno" je taky platná hodnota (`--favorite`, `--rating`, `--flag`),
se posílají **jen když je vážně napíšeš** — jinak by `ctl bulk --add-label x` tiše odoblíbil všechno,
čeho se dotkne, a shodil hodnocení na nulu. Vzájemně se vylučující dvojice (`--set-caption`
+ `--clear-caption`, `--archive` + `--unarchive`, …), rozsah hvězd, flag i souřadnice se ověřují
**lokálně**, aby překlep nestál round trip ani rollbacknutou transakci.

Výstup je souhrn (`120 photos · 118 updated · 0 skipped · 2 errored`); **vypíšou se jen fotky, které
selhaly**. Celý per-fotkový rozpad je v `-o json`.

#### Velké dávky: potvrzení nad 50 fotkami

Příkaz, který by sáhl na **víc než 50 fotek** (`ctl bulk`, `ctl albums add-photos`/`remove-photos`),
se nejdřív zeptá:

```
About to apply this edit to 120 photos, more than the 50-photo threshold. Continue? [y/N]
```

`--yes` / `-y` dotaz přeskočí. Když uidy přišly **ze stdin**, dotaz položit nejde — ten stream už
spolkl seznam uidů a v pipeline není terminál, ze kterého odpovědět. Příkaz proto **skončí chybou,
která si řekne o `--yes`**, místo aby na nezodpověditelnou otázku tiše pokračoval.

```bash
kukatkoctl albums create "Léto 2024" --description "prázdniny"
kukatkoctl labels attach lbl1a2b3 pht01h2j3
kukatkoctl subjects photos sub1a2b3 --limit 20
kukatkoctl favorites add pht01h2j3
kukatkoctl rating set pht01h2j3 5 --flag pick

# celá dávka v jedné transakci, uidy rovnou z výpisu:
kukatkoctl photos search "jezero" --limit 200 -o json | kukatkoctl bulk --add-label lbl1a2b3 --yes
kukatkoctl photos list --year 2019 -o json | kukatkoctl bulk --archive --yes
```

#### Co `ctl` schválně neumí

Zálohy, obnovu, migrace, údržbu, import a frontu jobů **po síti nenabízí**. Jsou destruktivní nebo
dlouhoběžící a patří na stroj, kde instance běží — zůstávají tedy jen jako lokální subkomandy
(`kukatko backup`, `restore`, `migrate`, `maintenance`, `import`, …).

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
  vrací 503), `user_agent` (viz níže), `base_url` (default `https://api.mapy.com`), a throttle
  reverse-geokódu pro background **`places` job** (cachuje lokalitu fotky): `geocode_rate_per_sec`
  (default 5, ≤ 0 vypne) + `geocode_burst` (default 10) — chrání měsíční mapy.com kreditní budget,
  zpracovat pomalu je OK. `KUKATKO_MAPS_GEOCODE_RATE_PER_SEC`/`_GEOCODE_BURST`.
- **Server-side cache dlaždic (`maps.tile_cache_bytes`, `maps.tile_cache_ttl`):** default **64 MiB**
  (`67108864`) a **24h**; ≤ 0 u kteréhokoli z nich cache vypne. Free tier mapy.com účtuje **1 kredit
  za dlaždici** (250k/měsíc), takže bez cache stojí každé opětovné projetí už viděné oblasti znovu.
  Cachují se **jen úspěšné** dlaždice (chyba nikdy — jinak by odmítnutý klíč zamrzl v mapě na celé
  TTL); hit/miss hlásí hlavička `X-Tile-Cache`. `KUKATKO_MAPS_TILE_CACHE_BYTES`/`_TILE_CACHE_TTL`.
- **Mapa je šedá?** Podívej se na `GET /system/status` → `maps.state`: `key_rejected` znamená, že
  mapy.com odmítá **náš** API klíč (vypršel / zrušen / došly kredity) — proxy to loguje WARN
  (`mapy: tile request failed`, se statusem) a vrací **424**; frontend nad tím ukáže varování místo
  šedé mřížky. Oprava je manuální: nový klíč v konzoli mapy.com → `MAPY_API_KEY`.
- **Stacks klíče (`stacks.*`, `internal/config` + `internal/stacks`):** seskupování více souborů
  jednoho snímku (RAW+JPEG, exportovaná úprava, kopie) pod jednu viditelnou fotku. `enabled` (bool,
  **default true**) je **master switch celé funkce** — automatické detekce **i** ručního stackování;
  při `false` vrací detekční endpoint i ruční stack endpointy **503**. `rules.*` zapíná jednotlivá
  detekční pravidla nezávisle (mají velmi různou míru falešných shod): `base_name` (**default true** —
  stejné jméno, jiná přípona; nejbezpečnější), `sequential_copy` (**default true** — jména kopií/
  sekvencí/úprav `IMG_1234 (2).jpg` / `copy` / `-edited` složená na originál), `unique_id`
  (**default true** — stejné EXIF `ImageUniqueID` / XMP `InstanceID`; velmi spolehlivé, kde existuje)
  a `time_gps` (**default false** — stejná vteřina pořízení A stejné GPS; nejvolnější, mylně slévá
  sériové snímky). Env: `KUKATKO_STACKS_ENABLED`, `KUKATKO_STACKS_RULES_BASE_NAME`,
  `_RULES_SEQUENTIAL_COPY`, `_RULES_UNIQUE_ID`, `_RULES_TIME_GPS`. **Admin backfill** `POST
  /process/stacks` (jako ostatní `/process/*`) proběhne detekci nad celou knihovnou přes
  `stacks.Service.DetectStacks` a vrátí `{created}`; kandidáty jsou jen dosud nestacknuté nearchivované
  fotky, takže re-run je idempotentní. Při `stacks.enabled: false` odpovídá 503.
- **Sidecar klíče (`sidecar.*`, `internal/config` + `internal/sidecarexport`/`internal/sidecarjob`):**
  **Metadatové sidecary** — YAML soubor na fotku vedle originálů ve storage (`sidecars/<klíč
  originálu>.yml`) s jejími metadaty a kurátorskými daty (titulek, popis, kdo je na fotce i s
  rámečkem obličeje, alba, štítky, per-user oblíbené a hodnocení, nedestruktivní úprava). Existuje,
  aby knihovna **přežila ztrátu databáze**: kurátorská data jinak žijí na jediném místě, v Postgresu,
  a S3 záloha je jediný mechanismus — zálohu, která tři měsíce tiše padá, objevíš v den, kdy ji
  potřebuješ. `enabled` (bool, **default true**) je master switch a je **schválně zapnutý**:
  mechanismus obnovy, který nikdo nezapnul, žádný mechanismus není. Při `false` se nic nezapisuje ani
  nemaže, nezařadí se žádný `sidecar` job (handler není registrovaný, takže by job navěky visel ve
  frontě) a `POST /process/sidecars` odpovídá 503; **sidecary už ve storage zůstanou přesně jak
  jsou** — vypnout export není žádost o zničení toho, co už zapsal, a zastaralý sidecar má větší
  cenu než žádný. Vypni to, když ti to I/O nestojí za to: je to jeden malý zápis na fotku na edit,
  proti úložišti, které si možná účtuje za request. Env: `KUKATKO_SIDECAR_ENABLED`. Nesouvisí s
  `internal/sidecar`, které čte *cizí* sidecary (Google Takeout, Apple XMP) při importu. **Formát
  celý v [`docs/RESTORE.md`](RESTORE.md)**; backfill `kukatko sidecar backfill [--all]` nebo
  admin-only `POST /process/sidecars`.
- **MCP klíče (`mcp.*`, `internal/config` + `internal/mcpapi`):** **MCP server** — knihovna vystavená
  AI agentovi (Model Context Protocol) na `POST /api/v1/mcp`, aby v ní uměl hledat, číst a organizovat
  („najdi všechny fotky babičky ze šedesátých a dej je do alba"). `enabled` (bool, **default false**) je
  **master switch a je záměrně vypnutý**: endpoint je nový útočný povrch, takže je **opt-in** — a při
  `false` se route **vůbec nemountuje** (`RegisterRoutes` neregistruje nic), takže cesta **neexistuje**,
  ne že by vracela 403; 403 by pořád prozradilo, že tam endpoint je. `page_size` (**default 25**) —
  kolik řádků vrátí list tool, když agent limit neřekne; `max_page_size` (**default 100**) — tvrdý strop
  na argument `limit` (větší požadavek se **ořízne**, neodmítne). Obojí je schválně malé: vzácný zdroj
  je **kontextové okno agenta**, ne databáze. Nekladná hodnota u obou spadne na default. Env:
  `KUKATKO_MCP_ENABLED`, `KUKATKO_MCP_PAGE_SIZE`, `KUKATKO_MCP_MAX_PAGE_SIZE`.
  **Auth:** žádný nový mechanismus — sedí za stejným `RequireAuth` a stejným RBAC jako zbytek `/api/v1`;
  agent se autentizuje **API tokenem** (`Authorization: Bearer kkt_…`) a rozhoduje **role jeho
  vlastníka** (token vlastní roli nemá): `viewer` = jen čtení, `editor`/`admin`/**`ai`** = i zápis.
  **Token pro agenta** si uživatel razí **sám pro sebe** — admin založí uživatele s rolí `ai`
  (`POST /api/v1/admin/users`), ten se přihlásí (`POST /api/v1/auth/login`) a vyrazí si token
  (`POST /api/v1/auth/tokens`); plaintext `kkt_…` se ukáže **jednou**. **Nic destruktivního není
  vystavené** (žádné mazání, purge, koš, archivace, restore, backup, správa uživatelů) a **každá mutace
  píše audit řádek ve své transakci**, s `"via": "mcp"` v details. Celý seznam toolů, auth model
  a co záměrně chybí: [`docs/MCP.md`](MCP.md).
- **Location estimate klíče (`location_estimate.*`, `internal/config` + `internal/geoestimate`):**
  odhad polohy fotek bez GPS z fotek pořízených blízko v čase. `enabled` (bool, **default true** — plná
  mapa a použitelná hierarchie míst je to, co většina knihoven chce; vypnutí je jeden klíč daleko,
  protože domýšlení dat je přesně ten druh ochoty, o který někdo nestojí): při `false` se **nikdy**
  neodhaduje nic a `POST /process/locations` vrací **503**; už odhadnuté polohy zůstávají, označené, aby
  je uživatel přijal nebo smazal. `window` (duration, **default 6h**) je **poloviční šířka** okna
  sousedů — fotka se odhaduje z fotek pořízených ±window od ní; stejný kalendářní den je nasnadě, pár
  hodin je lepší (den, co začne v Brně a skončí ve Vídni, je přesně ten případ, kdy je same-day odhad
  špatně). `radius_meters` (float, **default 5000**) je **radius soudržnosti**: sousedům se věří, jen
  když **každý** z nich leží do téhle vzdálenosti od jejich těžiště — jinak fotka zůstane bez polohy.
  Obě páky **chybují směrem k odmítnutí** a je to tak správně: špatná poloha tiše otráví mapu,
  hierarchii míst i každé `near:` hledání nad nimi, a rozšiřování radiusu za velikost jednoho výletu je
  špatný obchod (neexistuje hodnota, při které se den mezi Prahou a Vídní stane poctivým). Zapnutý
  odhadovač s nekladným `window`/`radius_meters` **neprojde startem** (`ErrInvalidLocationEstimate`) —
  lepší odmítnout naběhnout než vypadat zapnutě a nikdy nic nevyprodukovat; u vypnutého se hodnoty
  nekontrolují. Env: `KUKATKO_LOCATION_ESTIMATE_ENABLED`, `_WINDOW`, `_RADIUS_METERS`. **Admin
  backfill** `POST /process/locations` → `{estimated}` je jediná cesta, jak odhad vzniká (při uploadu se
  neodhaduje — čerstvá fotka ještě žádné sousedy z téhož dne nemá). Každý nový odhad dostane `places`
  job, takže se propíše do hierarchie míst; **geokód je metrovaný**, jede přes stávající
  `maps.geocode_rate_per_sec` limiter, takže velký backfill geokodér krmí po kapkách místo aby ho
  zavalil — počítej s **1 kreditem mapy.com na odhadnutou fotku**. Re-run je idempotentní a
  **uživatelem smazaný odhad se nikdy nevrátí**.
- **Candidates klíče (`candidates.*`, `internal/config` + `internal/candidates`):** ladí hledání
  „osoba mezi neotagovanými fotkami" (`POST /subjects/{uid}/candidates`). `max_distance` (**default
  0.5**) — výchozí max kosinová vzdálenost kandidáta od exempláře, když ji request nepošle, **a**
  baseline, proti němuž se škáluje vote rule; `search_limit` (**default 1000**) — kolik nejbližších
  nepřiřazených obličejů vrátí kNN každého exempláře před hlasováním (omezuje fan-out na exemplár);
  `min_face_px` (**default 32**) — minimální šířka obličeje v **display pixelech**, aby byl
  recenzovatelný (drobný obličej v davu nejde posoudit; doplňuje relativní floor převzatý z
  `faces.min_face_size`); `concurrency` (**default 8**) — kolik kNN exemplárů běží naráz, aby hledání
  osoby se stovkami fotek nefanoutovalo neomezeně. Nekladná hodnota u kteréhokoli klíče spadne na
  default (u `min_face_px` vypne absolutní floor). Env: `KUKATKO_CANDIDATES_MAX_DISTANCE`,
  `_SEARCH_LIMIT`, `_MIN_FACE_PX`, `_CONCURRENCY`.
- **Sweep klíče (`sweep.*`, `internal/config` + `internal/sweep`):** ladí **recognition sweep**
  (`GET /faces/sweep`), který skládá candidates hledání přes všechny osoby najednou. `concurrency`
  (**default 4**) — kolik subjektů se skenuje **naráz**; **stohuje se** na `candidates.concurrency`
  (kNN exemplárů na subjekt), takže na RAM-limitovaném boxu se drží malé. `max_subjects` (**default
  500**) — strop kolik subjektů jeden sweep proskenuje; při přesahu skenuje prvních `max_subjects`
  (dle jména) a výsledek označí `capped` místo tichého ořezu. Nekladná hodnota → default. Sweep
  **nikdy neautoconfirmuje** — jistota jen zužuje seznam. Env: `KUKATKO_SWEEP_CONCURRENCY`,
  `_MAX_SUBJECTS`.
- **Expand klíče (`expand.*`, `internal/config` + `internal/expand`):** ladí **expanzi sbírky**
  „najdi fotky podobné albu / štítku" (`GET /albums/{uid}/similar`, `GET /labels/{uid}/similar`).
  `max_distance` (**default 0.30**, UI ukazuje jako 70 % podobnost) — výchozí max kosinová vzdálenost
  kandidáta od zdrojové fotky, když ji request nepošle, **a** baseline pro vote rule; `limit` (**default
  50**) — výchozí počet vrácených kandidátů; `max_limit` (**default 200**) — strop na `?limit` requestu;
  `search_limit` (**default 200**) — kolik nejbližších fotek vrátí kNN každé zdrojové fotky před
  hlasováním (over-fetch, aby pozdější filtry nehladověly); `source_cap` (**default 500**) — strop kolik
  členů se použije jako dotazové vektory, obří album se **navzorkuje** (deterministicky, rovnoměrně přes
  členy) a cap se **hlásí** (`source_capped`) místo tichého ořezu; `concurrency` (**default 8**) — kolik
  kNN na zdroj běží naráz. Nekladná hodnota u kteréhokoli klíče spadne na default. Expanze je
  **read-only** — přidání nalezených fotek jde přes `POST /photos/bulk`. Env:
  `KUKATKO_EXPAND_MAX_DISTANCE`, `_LIMIT`, `_MAX_LIMIT`, `_SEARCH_LIMIT`, `_SOURCE_CAP`, `_CONCURRENCY`.
- **Review klíče (`review.*`, `internal/config` + `internal/review`):** ladí **review hru**
  (`GET /review/queue`, `POST /review/answer`) — otázky jedna po druhé nad kandidáty, kterými si
  systém není jistý. `band_min` / `band_max` (**default 0.45 / 0.75**) — **pásmo nejistoty**:
  otázkou se stane jen kandidát s confidence (= 1 − kosinová vzdálenost) v `[band_min, band_max)`;
  pod pásmem je odhad šum, od `band_max` výš se potvrzuje hromadně na `/recognition` / přes expand.
  Nevalidní pásmo (mimo (0,1), min ≥ max) spadne na default **pár**. `queue_size` (**default 20**) —
  výchozí velikost batchu, UI si přednačítá; request smí poslat vlastní `?limit` (strop 100).
  `cache_ttl` (**default 60s**) — jak dlouho se postavená fronta servíruje z per-user cache, než se
  drahá vektorová hledání spustí znova (odpovědi frontu upravují in-place, čítač session je levný).
  `max_labels` (**default 200**) — strop kolik štítků jeden rebuild proskenuje. `label_concurrency`
  (**default 2**) — kolik label-similarity hledání běží naráz (každé už interně fan-outuje; na
  RAM-limitovaném boxu držet nízko). Face stranu si review nebere vlastními klíči — jede přes
  sweep/candidates a jejich `sweep.*`/`candidates.*` limity. Nekladná hodnota u kteréhokoli klíče
  spadne na default. Env: `KUKATKO_REVIEW_BAND_MIN`, `_BAND_MAX`, `_QUEUE_SIZE`, `_CACHE_TTL`,
  `_MAX_LABELS`, `_LABEL_CONCURRENCY`.

### `maps.user_agent` — restrikce mapy.com klíče na User-Agent

`maps.user_agent` (env **`KUKATKO_MAPS_USER_AGENT`**, default **prázdný**) je přesný `User-Agent`,
který klient `internal/mapy` posílá na **každý** požadavek nahoru — dlaždice i (r)geokód. Prázdná
hodnota = neposílá se žádná explicitní hlavička (platí Go default `Go-http-client/2.0`), takže
instance, která klíč nenastaví, funguje beze změny.

Konzole mapy.com umí klíč omezit **buď** na zdrojové IP, **nebo** na User-Agent — vždy jen jeden typ
restrikce naráz. IP restrikce je tady křehká (veřejná IPv4 i ISP-delegovaný IPv6 prefix se mění a
klíč pak vrací `403` → šedé dlaždice), a protože je klíč čistě server-side, používáme **restrikci na
User-Agent**. mapy.com vyžaduje **přesnou shodu** (žádné wildcardy).

**Hodnota je druhé tajemství, ne kosmetika:** obsahuje náhodný token, takže samotný uniklý API klíč
je bez správného User-Agentu k ničemu. Proto ji **nikdy** nelogujeme, necommitujeme a nedáváme do
`config.example.yaml` (tam je jen placeholder) — stejný režim jako `mapy_api_key`. Reálná hodnota
žije v gitignorovaném `.secrets/db.env`.

Postup přepnutí (pořadí je důležité — restrikce se v konzoli přepíná atomicky):

1. Nasadit build, který hlavičku posílá, a nastavit `KUKATKO_MAPS_USER_AGENT` v prostředí instance.
2. Restartovat instanci (hodnota se čte při startu).
3. Teprve pak v konzoli mapy.com přepnout klíč z IP restrikce na User-Agent restrikci se stejnou
   hodnotou.

Nepřidáváme `Referer` — mapy.com u něj ověřuje jen host+port hlavičky, kterou si sami posíláme; bez
prohlížeče je to sebeprohlášení bez hodnoty.

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
- **Make cíle:** `fmt` (golangci-lint fmt + Prettier `--write` — **jediný cíl, který mění
  soubory**), `fmt-check` (`golangci-lint fmt --diff` + Prettier `--check`, read-only),
  `vet` (samostatně; `check` ho nepouští, protože `.golangci.yml` má `default: standard`,
  takže `golangci-lint run` už `govet` obsahuje), `lint` (golangci-lint + ESLint),
  `lint-fix`, `typecheck` (`tsc -b --noEmit`), `test` (Go unit `CGO_ENABLED=0` bez `-race`
  + Vitest — sdílí build cache s `build`), `test-race` (`CGO_ENABLED=1 go test -race ./...`,
  vyžaduje cgo/gcc; běží v CI, ne v bráně), `test-integration` (tag `integration` +
  `KUKATKO_TEST_DATABASE_URL`, `-p 1` — integrační balíky sdílí jednu test DB, takže běží
  sériově; testy R2 backendu navíc chtějí `KUKATKO_TEST_S3_ENDPOINT` — bez ní se skipnou,
  viz `docs/DEVELOPMENT.md`), `check` (brána = `docs-budget` + `fmt-check` + `lint` +
  `web-typecheck` + `test`; **nic nepřepisuje**, po úspěšném běhu je `git status --short`
  prázdný), `build` (frontend build + `CGO_ENABLED=0` → `bin/kukatko`), `dev` (smart rebuild + běh na
  `:6480` přes `scripts/dev.sh`, `DEV_ARGS=--force` pro plný rebuild), `clean`, `help`.
  Frontend-only cíle: `web-deps` (`npm ci`, hlídaný stamp souborem
  `web/node_modules/.kukatko-npm-ci-stamp` závislým na `web/package-lock.json`, takže se
  reinstaluje jen při změně lockfilu), `web-build`, `web-fmt`, `web-fmt-check`, `web-lint`,
  `web-typecheck`, `web-test`.
  Verzi injectuješ `make build VERSION=x.y.z`. Frontend potřebuje **Node.js 22+**.
- **CI/CD a balíčkování:** `.github/workflows/ci.yml` (push/PR → job `check` = `make check`
  + `make test-race` na Go 1.26 + Node 22 + golangci-lint v2.11.4; job `integration` = `make test-integration`
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

## Docker image — kontejnerový build a publikace na GHCR

<!-- BODY DOCKER -->
Vedle `.deb` (goreleaser) se Kukátko balí i jako **kontejnerový image** pro běh na amd64 VPS.
Zdroje: `Dockerfile` + `.dockerignore` v rootu, workflow `.github/workflows/docker-publish.yml`
a ukázkový `.env.example`.

- **`Dockerfile` (root, multi-stage → malý statický image):**
  1. **frontend** (`node:22-alpine`): `npm ci` + `npm run build` ve `web/` → zápis do
     `internal/web/static/dist` (dané `vite.config.ts`).
  2. **backend** (`golang:1.26-alpine`, `CGO_ENABLED=0`): `go mod download`, pak se **před**
     `go build` nakopíruje hotový `dist/` z frontend stage (jinak `//go:embed all:dist/*`
     v `internal/web/static` neprojde). Build je jedna statická binárka `./cmd/kukatko`;
     `-ldflags "-s -w -X …/internal/version.Version=$VERSION -X …/internal/version.Commit=$COMMIT_SHA"`
     razí verzi z build-args `VERSION`/`COMMIT_SHA`.
  3. **runtime** (`alpine:3`): jen nástroje, na které se pipeline **reálně** shell-outuje —
     `ffmpeg` (ffprobe + ffmpeg pro video metadata/poster/transcode), `exiftool` (EXIF/XMP
     **a** RAW = extrakce zabudovaného JPEG preview přes `exiftool -b`, žádný demosaic → `dcraw`/
     `libraw` netřeba) a `libheif-tools` (heif-convert pro HEIC/HEIF), plus `ca-certificates`
     a `tzdata`. **Bez `libvips`** — `thumb.engine` je defaultně pure-Go. Běží jako **nonroot**
     (`nobody`), `EXPOSE 8080` (= `web.port` default), `STOPSIGNAL SIGTERM` (graceful drain),
     `ENTRYPOINT` binárka + `CMD ["serve"]`. Knihovnu/cache/tmp montuj jako volumes
     (`/var/lib/kukatko/{originals,cache,tmp}`).
- **Publikace (`docker-publish.yml`) na `ghcr.io/panbotka/kukatko`** (image = `${{ github.repository }}`),
  autentizace přes vestavěný `GITHUB_TOKEN` (permission `packages: write`), **žádné další secrety**.
  Triggery: push do `main`, push tagu `v*.*.*` a `pull_request` do `main` (**PR jen buildí, nikdy
  nepushuje** — `push` je true jen když `github.event_name != 'pull_request'`).
  - **Testovací brána:** job `test` pouští **`make test` + `make test-integration`** (zrcadlí setup
    `integration` jobu z `ci.yml`: Go 1.26, Node 22, service container `pgvector/pgvector:pg17`,
    extensions `vector`/`unaccent`, apt `ffmpeg`/`exiftool`, `KUKATKO_TEST_DATABASE_URL`). Job
    `build` má **`needs: test`** → když testy spadnou, **žádný image se nepushne**.
  - **Tagy** (přes `docker/metadata-action@v5`, `flavor: latest=false` + explicitní řízení):
    push do `main` → **`latest`** (jen na default větvi, `enable={{is_default_branch}}`; na tazích
    `latest` **ne**); tag `vMAJOR.MINOR.PATCH` → **`{{version}}`** a **`{{major}}.{{minor}}`**; k tomu
    vždy immutable **`sha`** tag. Build přes `docker/build-push-action@v6` s build-args
    `VERSION` (tag bez úvodního `v`, jinak `dev`) a `COMMIT_SHA` (short SHA).
- **`.env.example` (root):** dokumentovaný, secret-free příklad env proměnných pro běh kontejneru
  (`docker run --env-file .env …`). Odvozené z `config.example.yaml`: konvence `KUKATKO_` (tečka →
  podtržítko) + výjimka `MAPY_API_KEY`. Pokrývá `KUKATKO_DATABASE_URL` (povinné), embedding URL,
  storage/R2 klíče, backup S3 klíče a `MAPY_API_KEY`. Reálný **`.env` je gitignored**
  (`.env`/`.env.*`), `.env.example` je výjimka a commituje se.

