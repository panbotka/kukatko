# Stránka: jeden člověk přiřazený na fotce vícekrát

Uživatel: *"občas se stává, že jedna osoba je na jedné fotce přiřazená vícekrát —
chci stránku, kde tohle bude možné řešit — je to vždy chyba."*

Jde o **kurátorskou stránku**, ne o automatickou opravu. Nic se nesmí slučovat ani
mazat bez toho, aby to člověk odklikl.

## Data (ověřeno v produkci, 2026-08-04)

```sql
SELECT m.photo_uid, s.name, count(*) n
  FROM markers m JOIN subjects s ON s.uid = m.subject_uid
 WHERE s.name <> '' AND m.invalid = false
 GROUP BY 1,2 HAVING count(*) > 1;
```

**23 fotek, 10 osob, 27 přebytečných markerů, nejhorší případ 3 na fotku.**
Nejde o šum — u čtyř fotek má "Marie Němcová" tři markery vedle sebe
(x 0.59 / 0.70 / 0.79), což je typický otisk špatného přiřazení skupinové fotky.

Pozor na dvě různé příčiny, které v UI vypadají stejně:

| příčina | počet fotek | řeší |
| --- | --- | --- |
| jeden marker, víc obličejů na něj napárovaných | 29 | samostatný task na výlučné párování v `internal/facematch` |
| **skutečně víc markerů téže osoby** | 15 (resp. 23 včetně fotek bez obličejů) | **tenhle task** |

Tenhle task řeší **druhý řádek**: markery. Neopravuj párování, to dělá druhý task —
ale stránka musí být napsaná tak, aby po jeho nasazení počty klesly a nic se
nerozbilo (tedy počítat nad `markers`, ne nad `faces`).

## Stránka

Nová položka v maintainer/editor části (stejná úroveň jako `/duplicates`,
`/outliers` — podívej se, jak jsou udělané, a drž se jejich vzoru).

- **Seznam skupin.** Jedna skupina = (fotka, osoba) s víc než jedním platným
  `face` markerem. Řadit tak, aby nejhorší (nejvíc markerů) byly nahoře, pak
  podle jména osoby, ať se to dá projít systematicky.
- **Vždy viditelný náhled s markery.** Bez výřezu obličeje se nedá rozhodnout,
  který marker je ten správný. Ukaž **náhled celé fotky s vykreslenými rámečky**
  všech markerů té osoby, každý očíslovaný, plus výřez u každého markeru.
  Pro výřez použij `fit_*`, **ne `tile_*`** — `tile_*` je čtvercově ořízlý
  na střed, takže bbox sedne vedle (viz `internal/thumb` a jak to řeší
  `/outliers`).
- **Akce na skupině** — všechny už existují jako write cesty, nové psát nemusíš:
  - *nechat tenhle* → ostatní markery skupiny **odpojit od osoby**
    (`subject_uid` → NULL, `reviewed` → false), ne smazat. Odpojený marker
    zůstane jako region k přiřazení někomu jinému — na skupinovce je za tím
    zpravidla jiný člověk.
  - *neplatný marker* → `invalid = true` u konkrétního markeru (pro rámečky,
    kde vůbec není obličej).
  - *nechat být* → skupina se schová z výpisu (falešný poplach: dvojexpozice,
    zrcadlo, fotka fotky). Musí to být **trvalé rozhodnutí v DB**, ne stav
    v prohlížeči, jinak to člověk bude odklikávat pořád dokola. Podívej se, jestli
    na to jde použít `internal/feedback` (ten už umí "tohle nejsou duplicity"),
    a jestli ne, řekni proč a udělej vlastní minimální tabulku.
- Každá akce projde `internal/audit` ve stejné transakci jako zápis, jako všude
  jinde.
- Rozhodnutá skupina zmizí ze seznamu a jde hned další — stejná smyčka jako
  `/review`. Ať se to dá projet za pár minut, těch skupin jsou desítky.

## Backend

Nový read-only balíček (vzor `internal/duplicates` / `internal/outliers`) +
jeho `*api`. Čtení pod `RequireAuth`, zápisy pod `RequireWrite`. Endpoint pro
výpis skupin a endpointy pro tři akce výše (nebo zapoj existující marker write
cesty z `internal/peopleapi`, pokud stačí — nezdvojuj logiku).

Dotaz drž nad `markers` + `subjects`, `invalid = false`, `type = 'face'`, a
vylučuj bezejmenný subjekt (`name = ''`) — ten se řeší jinde a zatlačil by
sem 6766 fotek.

## Testy (povinné)

- Unit na seskupování: fotka se 3 markery jedné osoby a 1 markerem jiné →
  jedna skupina o třech.
- Unit: `invalid = true` marker se do skupiny nepočítá; skupina, která tím
  spadne na 1, zmizí.
- Integrační na každou ze tří akcí, včetně kontroly, že marker **existuje dál**
  a jen ztratil `subject_uid`, a že vznikl audit záznam.
- Integrační: rozhodnutá skupina se v dalším výpisu neobjeví.
- Frontend Vitest na stránku. Virtualizovaný seznam testuj podle
  zavedeného vzoru v repu (mock `react-virtuoso`, viz jak to dělají existující
  testy gridů) — jinak se v jsdom nevykreslí žádná položka.

## Hotovo

Platí `docs/` + `make check` z `CLAUDE.md`: nový balíček → `docs/PACKAGES.md`
+ řádek do `## Package map` v `CLAUDE.md`, endpointy → `docs/API.md`,
stránka a komponenty → `docs/FRONTEND.md`, uživatelsky viditelná funkce →
`README.md`. Texty přes i18n, cs i en.