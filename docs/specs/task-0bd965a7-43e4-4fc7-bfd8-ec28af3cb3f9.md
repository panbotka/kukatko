# Odstranit dokončenou migraci z PhotoPrismu a photo-sorteru

Uživatel: *"Jelikož máme import hotový, klidně bych odstranil import z photoprism
a ze sorteru. Ten kód je stejně mrtvý a už se nikdy nepoužije. To stejné na
frontendu."*

Migrace je od 2026-08-05 uzavřená — `kukatko import verify` hlásí **COMPLETE**,
fotky/obličeje/alba/štítky/lidi sedí bez mezer a další import už se pouštět nebude.

## Co smazat (~20 050 řádků)

| balíček | řádků | co to je |
| --- | --- | --- |
| `internal/ppimport` | 8 804 | inkrementální import z PhotoPrismu |
| `internal/psimport` | 3 359 | přímá migrace z photo-sorteru |
| `internal/importverify` | 2 835 | kontrola úplnosti proti zdrojům |
| `internal/photoprism` | 2 102 | HTTP klient PhotoPrismu |
| `internal/psfeedsimport` | 1 629 | import vektorů z ps feedů |
| `internal/photosorter` | 721 | klient photo-sorter DB |
| `internal/psfeeds` | 600 | HTTP klient ps feedů |

Dál: `cmd/kukatko` podpříkazy `import photoprism` a `import photosorter-feeds`
(+ psimport wiring), konfigurační sekce `photosorter` a `photoprism`
(`internal/config`, `config.example.yaml`), a na frontendu `ImportPage`
+ `services/import.ts` v rozsahu, který se týká těchto zdrojů.

## Co NESMÍŠ smazat — tady se to dá snadno pokazit

**1. Sloupce `photoprism_uid`, `photoprism_file_hash`, `photosorter_uid`
v tabulce `photos`. Nepiš migraci, která je zahazuje.** Nejsou to zbytky importu,
jsou to pořád živá data:

- `internal/photos/store.go:160` na `photoprism_uid` vyhledává — je to
  **vydaná funkce z v0.5.0**: vložíš do hledání PhotoPrism id `pt…` a appka tě
  na tu fotku pošle. Bez sloupce přestane fungovat.
- `internal/sidecarexport` je zapisuje do **každého sidecar souboru** jako
  `external.photoprism_uid` / `photoprism_file_hash`. Jsou v 25 791 souborech
  vedle originálů v R2.
- Je to provenience 20 647 fotek — jediná stopa, odkud se vzaly.

**2. `internal/dirimport` zůstává.** Není to migrace, je to `kukatko import dir`,
kterým se do knihovny nahrává složka z disku. Používá se dál.

**3. `internal/importer` zůstává.** Není jen pro migraci — používá ho
`internal/dirimport` (eviduje své běhy) a `internal/system` + `internal/systemapi`
(admin dashboard). Zkontroluj, jestli v něm nezůstane kód specifický jen pro
zdroje, které mizí, a ten odstraň — ale balíček ne.

**4. `internal/importapi` se zmenší, nesmaže.** Ověř aktuální stav, dnes to je:

```
GET  /import/runs        historie běhů      ← zůstává, dirimport ji plní
GET  /import/failures    selhání po fotkách ← zůstává, ze stejného důvodu
GET  /import/verify      kontrola úplnosti  ← pryč, nemá co s čím porovnávat
POST /import/photoprism                     ← pryč
POST /import/photosorter                    ← pryč
POST /import/photosorter-feeds              ← pryč
```

**5. Historii proběhlých migrací nezahazuj.** V `import_runs` je záznam, že
migrace proběhla a s jakým výsledkem. Rozhodni, jestli řádky nechat (a jen
přestat vyrábět nové) — a rozhodnutí zdůvodni. Kdyby ses přece jen rozhodl
tabulku měnit, projdi `internal/reset/tables.go`, jinak spadne
`make test-integration` až v `internal/reset`.

## Jak postupovat

1. **Nejdřív si ověř, že těch sedm balíčků opravdu nikdo jiný nepoužívá.** Grep
   přes celé repo, ne jen přes to, co čekáš. Dnešní stav závislostí:
   `photoprism` ← `ppimport`, `importverify`, `cmd`; `psfeeds` ← `psfeedsimport`,
   `importverify`, `cmd`; a tak dál — mažou se jako celý uzavřený shluk, ale ověř
   si to sám, mohlo se to změnit.
2. Smaž balíčky, jejich testy a wiring v `cmd/kukatko` (`buildImportAPI` a spol.).
3. Vyčisti konfiguraci: struct, `setDefaults`, `config.example.yaml`, testy
   konfigurace, a **`docs/OPERATIONS.md`** — všechno naráz, jak říká `CLAUDE.md`.
4. Frontend: stránku a její routu, položku v navigaci, i18n klíče v **cs i en**,
   testy.
5. Nakonec **grep na osiřelé zmínky** v celém repu — `photoprism`, `photosorter`,
   `psfeeds`, `ppimport`, `psimport` — v dokumentaci, README, `CLAUDE.md` package
   mapě, i18n souborech a v `docs/`. Zůstat smí jen ty, které mluví o **sloupcích
   a provenienci**, ne o importu.

## Testy (povinné)

- `make check` i `make test-integration` zelené.
- **Regresní test na hledání podle PhotoPrism id** — vlož `pt…` uid a musíš
  dostat tu fotku. Tohle je ta nejpravděpodobnější škoda a musí být přikryté.
- **Regresní test na sidecar**: vygenerovaný soubor pořád nese
  `external.photoprism_uid` a `photoprism_file_hash`.
- Integrační test, že `kukatko import dir` funguje beze změny a zapisuje běh.
- Integrační test, že `GET /import/runs` a `/import/failures` odpovídají dál.
- Ověř, že server nastartuje s konfigurací, která sekce `photoprism`/`photosorter`
  **ještě obsahuje** (staré config soubory v provozu existují) — buď je Viper
  tiše ignoruje, nebo to musí být vědomé chování. Napiš, jak to dopadlo.

## Hotovo

Platí `docs/` + `make check` z `CLAUDE.md`. Odstraněné balíčky vyhoď z
`## Package map` v `CLAUDE.md` i z `docs/PACKAGES.md`, endpointy z `docs/API.md`,
stránku z `docs/FRONTEND.md`, konfiguraci a CLI z `docs/OPERATIONS.md`.
`docs/MIGRATION_AUDIT.md` a `docs/MIGRATION_PLAN.md` popisují dokončenou migraci —
**nemaž je**, jsou to historický záznam; jen v nich vyznač, že migrace je
uzavřená a kód odstraněn.