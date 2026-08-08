# UX výzkum — Kukátko z pohledu uživatele

Tenhle dokument nevznikl čtením kódu. Vznikl tak, že jsem se přihlásil do **živé
produkce**, proklikal ji na počítači i na telefonu a zkusil v ní udělat věci, kvůli
kterým lidé fotogalerii otevírají. Nálezy níže jsou to, co se přitom rozbilo, zdrželo
nebo zmátlo — každý s návrhem, odhadem a místem v kódu, aby se z něj dal udělat úkol.

Starší [`UX_AUDIT.md`](UX_AUDIT.md) vznikl procházením obrazovek v kódu a řeší jiné
věci (velikost dotykových cílů, žargon, konzistence tlačítek). Zůstává v platnosti,
neduplikuji ho — kde se potkáváme, odkazuji.

---

## Metoda

- **Kdy:** 5. srpna 2026. **Verze aplikace:** 0.5.1.
- **Kde:** produkce `kukatko.kotrzina.cz`, ne testovací databáze.
- **Účet:** `ux-research`, role **Prohlížeč** (viewer). Na zápis vrací RBAC 403; nic
  jsem neobcházel a nepřihlašoval se pod jiným účtem.
- **Prohlížeč:** Chromium přes `agent-browser`, jedna relace.
  - **Desktop:** 1280 × 633 a 1440 × 900, myš.
  - **Mobil:** emulace iPhone 16 — 393 × 852 CSS px, DPR 3, mobilní user-agent, nikoli
    jen zúžené okno. **Jedno omezení:** emulace nehlásila `pointer: coarse`, takže
    dotyková gesta a velikost dotykových cílů jsem neměřil — nálezy o mobilu jsou
    o rozvržení a informační architektuře. (Že pravidla `@media (pointer: coarse)`
    v nasazeném CSS opravdu jsou, jsem ověřil zvlášť — je jich sedm.)
- **Data, na kterých se to lámalo:** 20 906 fotek (20 890 v knihovně, 16 v koši),
  115 461 nalezených obličejů, z toho **16 585 nepojmenovaných** a 4 602 pojmenovaných,
  **105 osob**, **438 alb**, 113 štítků, 2 378 fotek s polohou (11 % knihovny), fotky
  od roku 1905 do 2026. Embeddingová služba (box) byla po celou dobu **offline** —
  `/api/v1/capabilities` vracelo `semantic_search: false`.
- **Ochrana soukromí:** v dokumentu nejsou snímky obrazovky s fotkami ani jména osob
  z knihovny. Kde je potřeba ilustrace, popisuji rozvržení, ne obsah.

### Co jsem projít nemohl

Účet prohlížeče dostane na tyhle stránky 403 a aplikace ho mlčky přesměruje na
knihovnu (což je samo o sobě nález — viz [N13](#n13)):

| Stránka | Stav |
| --- | --- |
| `/review` (řadicí hra) | `GET /api/v1/review/queue` → **403** |
| `/people/clusters` (skupiny obličejů) | `GET /api/v1/faces/clusters` → **403** |
| `/outliers` (možné chyby) | přesměrování na `/` |
| `/duplicates` (duplicity) | `GET /api/v1/duplicates` → **403** |
| `/upload`, `/trash` | přesměrování na `/` |

**Řadicí hru, shluky obličejů, možné chyby a úklid duplicit jsem tedy neviděl** a
nemám k nim nálezy. Totéž platí pro úkoly *„vytvoř album z dovolené"* a *„pojmenuj
lidi na téhle fotce"* — do bodu, kde se zapisuje, jsem se dostal (a popisuji, co je
tam vidět), ale samotný zápis ne. Pokud má být tahle část pokrytá, je potřeba účet
s rolí editor; nechal jsem to na rozhodnutí, protože zadání výslovně říká účet neměnit.

### Jak číst nálezy

Stupnice je stejná jako v `UX_AUDIT.md`: **🔴 vysoká · 🟡 střední · ⚪ nízká**.
První značka je **dopad**, druhá **pracnost**. Nálezy jsou seřazené podle poměru
dopad/pracnost — nahoře je to, co se vyplatí udělat první.

---

## Pořadí

| # | Nález | Dopad | Pracnost | Kde |
| --- | --- | :---: | :---: | --- |
| [N1](#n1) ✅ | Hledání čeká 30 s na AI službu, o které aplikace ví, že je offline | 🔴 | ⚪ | `SearchPage`, `usePhotoSearch` |
| [N2](#n2) | Knihovna dotazovací jazyk umí, ale tvrdí opak (a mlčí u překlepu) | 🔴 | ⚪ | `FilterBar` |
| [N3](#n3) ✅ | Na `/search` a `/saved` nevede z menu žádný odkaz, `Žebříček` má top-level slot | 🔴 | ⚪ | `navItems.ts` |
| [N4](#n4) | Hustota mřížky je jedna hodnota pro notebook i telefon | 🔴 | ⚪ | `lib/gridDensity.ts` |
| [N5](#n5) ✅ | Zpět z fotky ztratí pozici v knihovně | 🔴 | 🟡 | `PhotoGrid`, `usePaginatedPhotos` |
| [N6](#n6) ✅ | Na telefonu panel obličejů i informací fotku úplně zakryje | 🔴 | 🟡 | `photo/viewer.css` |
| [N7](#n7) | 438 alb v jedné hromadě, i když API už rozlišuje jejich typ | 🔴 | 🟡 | `AlbumsPage` |
| [N8](#n8) | `/people`: 105 osob bez hledání, a 125 Mpx stažených na 72 čtverečků | 🔴 | 🟡 | `PeoplePage`, `SubjectTile` |
| [N9](#n9) | Seznam obličejů nemá náhledy, jmenovky na fotce se překrývají | 🟡 | 🟡 | `FacesPanel`, `FaceOverlay` |
| [N10](#n10) | `/labels`: 113 štítků jako svislý seznam bez hledání a řazení | 🟡 | ⚪ | `LabelsPage` |
| [N11](#n11) ✅ | Příznaky se jmenují podle tvaru ikony: „Oko", „Palec nahoru" | 🟡 | ⚪ | `FlagControl`, `FilterBar` |
| [N12](#n12) ✅ | Do textu pro uživatele prosakuje `AI_MODEL:`, `Unknown`, anglické názvy | 🟡 | ⚪ | `MetadataPanel`, import |
| [N13](#n13) ✅ | Stránka, na kterou nemám právo, mlčky přesměruje na knihovnu | 🟡 | ⚪ | routing, `LeaderboardPage` |
| [N14](#n14) ✅ | Zašedlá tlačítka bez vysvětlení, proč nejdou zmáčknout | 🟡 | ⚪ | `ReasonedButton`, `FacesPanel` |
| [N15](#n15) ✅ | Karta prohlížeče se vždy jmenuje „Kukátko" | 🟡 | ⚪ | `useDocumentTitle`, stránky |
| [N16](#n16) | Mobilní zásuvka filtrů nemá patičku s počtem výsledků ani „Použít" | 🟡 | ⚪ | `FilterBar` |
| [N17](#n17) ✅ | Nápověda o dotazovacím jazyce mlčí a slibuje zapamatovanou pozici | 🟡 | ⚪ | `HelpPage` |
| [N18](#n18) | `Rok` (109 položek) a `Pořízeno od/do` jsou dva soupeřící filtry data | 🟡 | 🟡 | `FilterBar` |
| [N19](#n19) | Mobil: 350 z 852 px je ovládání, a časová osa je skrytá | 🟡 | 🟡 | `LibraryPage`, `TimelineScrubber` |
| [N20](#n20) ✅ | Ovládání prohlížeče fotky se schová i s jedinou cestou zpět | 🟡 | ⚪ | `useAutoHideChrome` |
| [N21](#n21) ✅ | Obálky alb se opakují, alba jdou od sebe rozeznat jen podle názvu | 🟡 | 🟡 | `AlbumsPage`, výběr obálky |
| [N22](#n22) ✅ | `/stats` mluví o „Embeddingách" a nenabízí žádnou akci | 🟡 | ⚪ | `StatsPage` |
| [N23](#n23) | Mapa je světlá v tmavé aplikaci a `Místa` jsou jeden řádek | 🟡 | 🟡 | `MapPage`, `PlacesPage` |
| [N24](#n24) | Detail alba nemá časovou osu ani popis | ⚪ | ⚪ | `AlbumDetailPage` |
| [N25](#n25) | Prázdné hledání hlásí „Počet fotek: 0" | ⚪ | ⚪ | `SearchPage` |
| [N26](#n26) | Technické údaje ukazují SHA256, PhotoPrism UID a souřadnice | ⚪ | ⚪ | `TechnicalDetails` |

---

## Nálezy

<a id="n1"></a>
### N1 — Hledání čeká 30 sekund na AI službu, o které aplikace ví, že je offline 🔴 ⚪

**Co se stalo.** Na `/search` jsem zadal `svatba`. Stránka 30 sekund ukazovala jen
„Načítání fotek…". Pak se objevilo 158 výsledků a k tomu hláška „Vyhledávání podle
obsahu je teď dočasně nedostupné. Zobrazujeme výsledky podle textu." Změřeno
v `performance`:

```
GET /api/v1/capabilities                            →    27 ms  {"semantic_search": false}
GET /api/v1/search?…&q=svatba&mode=hybrid           → 30 179 ms
GET /api/v1/search?…&q=osoba:Jarmila&mode=hybrid       → 30 039 ms
```

Aplikace se tedy hned na startu zeptá, jestli sémantické hledání funguje, dostane
`false` — a přesto pošle dotaz v režimu `hybrid` a čeká plný třicetisekundový timeout
na box. Reprodukovatelné, dvakrát po sobě, na dvou různých dotazech. Nabídka režimů
zároveň dál nabízí „Sémantické" jako plnohodnotnou volbu (`option` není `disabled`).

**Proč to vadí.** Podle `CLAUDE.md` je box offline běžný stav, ne výjimka. Znamená to,
že **v běžném stavu trvá každé hledání půl minuty**, po kterou uživatel nevidí nic než
spinner. Půl minuty je za hranicí, kdy člověk považuje aplikaci za rozbitou; většina
lidí odejde dřív, než se výsledky objeví — přestože textové výsledky byly k dispozici
celou dobu.

**Návrh.** Frontend už má `CapabilitiesProvider`, jen ho `SearchPage` a
`usePhotoSearch` nečtou. Když je `semantic_search: false`:
1. posílat rovnou `mode=fulltext`,
2. volbu „Sémantické" v nabídce zakázat (`disabled` + vysvětlující `title`),
3. hlášku o nedostupnosti ukázat **před** hledáním, ne po něm.

Pojistkou navíc: zkrátit timeout na sidecar na jednotky sekund, aby ani nesprávný
odhad dostupnosti nikdy nestál 30 s.

**Kde to je.** `web/src/pages/SearchPage.tsx` (nabídka režimů, řádek ~186),
`web/src/hooks/usePhotoSearch.ts`, `web/src/capabilities/CapabilitiesProvider.tsx`
(dnes ani jeden ze zmíněných souborů `Capabilities` nepoužívá). Serverová strana:
`internal/photoapi` + `internal/embedding` (timeout).

**✅ Vyřešeno (7. 8. 2026).** Rozhodování o režimu je teď v jednom háčku
`useSearchMode`: při `semantic_search: false` odchází dotaz rovnou jako `fulltext`
(z `usePhotoSearch`, `usePhotoNeighbors` i promítání, ne jen ze `SearchPage`), volba
„Sémantické" je `disabled` s vysvětlením a hláška o nedostupnosti visí u nabídky
režimů **dřív, než hledání proběhne**. Zvolený režim zůstává v URL, takže se uplatní,
jakmile se box vrátí. Pojistka na serveru: klient sidecaru má vlastní transport
s 3s dial timeoutem a text embedding (jediné interaktivní volání) 5s strop — oboje
konfigurovatelné (`embedding.dial_timeout`/`text_timeout`), takže ani chybný odhad
dostupnosti nestojí 30 s.

---

<a id="n2"></a>
### N2 — Knihovna dotazovací jazyk umí, ale tvrdí opak (a u překlepu mlčí) 🔴 ⚪

**Co se stalo.** Vyhledávací pole nad knihovnou má placeholder
*„Filtrovat podle názvu a popisu…"* a pod ním nápovědu *„Filtruje název a popis."*
Zkusil jsem do něj napsat `year:1960-1969` — a knihovna se skutečně profiltrovala
na **227 fotek** ze šedesátých let, přesně jako `/search`. Celý dotazovací jazyk tam
funguje; jen o tom pole lže.

Druhá půlka: napsal jsem `osoba:Jarmila` (česká varianta klíče `person:`). Knihovna
ukázala „Nenalezeny žádné fotky" a **žádné upozornění**. Tentýž dotaz na `/search`
hlásí *„Těmto filtrům nerozumím (hledám je jako obyčejný text): osoba:Jarmila"*.

Navíc: když dotaz obsahuje `year:1960-1969`, rozbalovací filtr **Rok** vedle něj dál
říká „Libovolný rok". Viditelný stav filtrů odporuje zobrazeným výsledkům.

**Proč to vadí.** Knihovna je vstupní stránka aplikace a její vyhledávací pole je
první věc, kterou člověk zkusí. Nejsilnější funkce Kukátka — `person:`, `year:`,
`label:`, `faces:` — je tak schovaná za textem, který ji popírá. A když uživatel
klíč zkomolí, dostane „nic nenalezeno", což vypadá jako *„v knihovně to není"*, ne
jako *„napsal jsi to špatně"*.

**Návrh.**
1. Přepsat placeholder i nápovědu tak, aby říkaly pravdu, např.
   *„Hledat — text, nebo filtr jako `year:1965` či `person:Jarmila`"*.
2. Přidat k poli stejnou ikonu **?**, která na `/search` otevírá `SearchQueryHelp`.
3. Ukazovat v knihovně **stejné upozornění na neznámý filtr**, jaké už `/search` má
   (parser ho vrací, jen se nevykreslí).
4. Když dotaz obsahuje `year:`/`album:`/`person:`, promítnout to do odpovídajícího
   ovládacího prvku, nebo ten prvek aspoň označit jako přebitý dotazem.

**Kde to je.** `web/src/components/library/FilterBar.tsx` (placeholder a `hint`),
`web/src/components/search/SearchQueryHelp.tsx` (existující nápověda k znovupoužití),
varování o neznámých filtrech vrací `internal/query`.

---

<a id="n3"></a>
### N3 — Na `/search` a `/saved` nevede z menu žádný odkaz, zato `Žebříček` má top-level slot 🔴 ⚪

**Co se stalo.** Horní lišta pro prohlížeče obsahuje: **Knihovna · Alba · Štítky ·
Procházet ▾ (Oblíbené, Lidé, Místa, Mapa) · Žebříček**, plus kolečko s lupou, ikonu
klávesových zkratek a účet. Mobilní zásuvka má totéž plus Můj účet, Statistiky,
Nápověda, Klávesové zkratky, Odhlásit.

**Ani v jednom z nich není položka „Hledání" ani „Uložená hledání."** Stránka
`/search` — jediné místo, kde je nápověda k dotazovacímu jazyku a přepínač režimů —
je dosažitelná pouze přes nepopsané kolečko s lupou (jeho `title` prozradí zkratku
`/` nebo Ctrl+K, ale to je vidět až po najetí myší, takže na telefonu nikdy).
`/saved` je pak schované ještě o úroveň hlouběji, v rozbalovátku uvnitř `/search`.

Naproti tomu **Žebříček** má vlastní položku nejvyšší úrovně, vedle Knihovny a Alb.
Na produkci obsahuje jednoho hráče a **38 odpovědí za celou dobu**. Jeho jediné
tlačítko („Začněte třídit" → `/review`) prohlížeče vrátí zpátky do knihovny.

**Proč to vadí.** Nejsilnější funkce aplikace nemá v navigaci žádnou stopu, zatímco
nejméně používaná má prominentní místo. Uživatel, který se nedozví o `/`-zkratce,
nemá jak zjistit, že Kukátko umí hledat podle obsahu, ukládat pohledy nebo skládat
dotazy — a to jsou přesně věci, kvůli kterým dává smysl mít 20 000 fotek v aplikaci
místo ve složkách.

**Návrh.**
1. Přidat **Hledání** jako položku nejvyšší úrovně (mobil i desktop), s ikonou lupy
   a textem — kolečko bez popisku může zůstat jako zkratka.
2. **Uložená hledání** dát do „Procházet" — jsou to chytrá alba, patří k Oblíbeným.
3. **Žebříček** přesunout pod „Procházet", nebo ještě lépe na `/review` jako postranní
   panel; sám o sobě to není destinace.
4. Na mobilu má spodní lišta tři sloty: **Knihovna · Alba · Štítky**. Vyměnit
   `Štítky` (viz [N10](#n10)) za `Hledat` nebo `Lidé` — obojí se používá častěji.

**Kde to je.** `web/src/components/navItems.ts`, `web/src/components/Layout.tsx`,
`web/src/components/MobileTabBar.tsx`, `web/src/components/MobileNavDrawer.tsx`.

**✅ Vyřešeno (7. 8. 2026).** **Hledání** je čtvrtá položka nejvyšší úrovně (ikona
lupy + text), hned za Štítky — na desktopu i v mobilní zásuvce, protože obě čtou
`PRIMARY_ITEMS`. Kolečko s lupou zůstalo jako zkratka na paletu. **Uložená hledání**
jsou v „Procházet" hned za Oblíbenými, **Žebříček** je na konci téže skupiny (varianta
„postranní panel v `/review`" by znamenala sáhnout na stránku, což zadání vylučovalo).
Spodní lišta na telefonu je teď **Knihovna · Alba · Hledat · Nahrát** — `Štítky` z ní
odešly do zásuvky, protože prohlížení podle štítku je z těch dvou ta vzácnější pochůzka.
Šířku lišty to nezhoršilo: v Chromiu s českými popisky (`probe` nad skutečným `Layout`em)
je položka „Hledání" **95 px** proti 102 px „Žebříčku", takže řádek správce je o **7 px
užší** než předtím — při 1200/1280 px přetéká kontejner o 111 px místo 118 px, od 1400 px
se vejde tak jako dřív.

---

<a id="n4"></a>
### N4 — Hustota mřížky je jedna hodnota pro notebook i telefon 🔴 ⚪

**Co se stalo.** Na desktopu jsem nechal ovladač „Dlaždic na řádek" na výchozí
hodnotě 8. Pak jsem přepnul na mobilní zařízení (393 px) — a knihovna zůstala
osmisloupcová. Na dlaždici tak zbylo necelých 50 px šířky a **srdíčko oblíbených
bylo větší než samotná fotka**: celá obrazovka byla mříž bílých srdcí, pod nimiž nebylo poznat
nic. Hodnota se drží v `localStorage` pod klíčem `kukatko.grid.density` a je společná
pro všechna zařízení i šířky okna. (Čerstvá návštěva z telefonu dostane výchozí
hodnotu 2, což vypadá dobře — problém nastane až v okamžiku, kdy si člověk jednou
zvětší hustotu na počítači.)

**Proč to vadí.** Fotky se podle zadání prohlížejí hlavně na mobilu. Stačí jednou
na notebooku zmáčknout „+" a telefon se stane nepoužitelným — a uživatel netuší
proč, protože ovladač hustoty je na mobilu schovaný ve stejné liště, kterou musí
nejdřív najít.

**Návrh.** Držet hustotu odděleně podle šířky výřezu (dvě hodnoty: „úzký" a
„široký"), nebo výslednou hodnotu tvrdě omezit — na `< 576 px` maximálně 3 sloupce,
na `< 768 px` maximálně 4. Druhá varianta je pár řádků a řeší to celé.

**Kde to je.** `web/src/lib/gridDensity.ts` (klíč `kukatko.grid.density`, řádek ~79),
`web/src/hooks/useGridDensity.ts`, `web/src/components/library/GridDensityControl.tsx`.

---

<a id="n5"></a>
### N5 — Zpět z fotky ztratí pozici v knihovně 🔴 🟡

**Co se stalo.** Odscrolloval jsem knihovnu na `scrollY = 4000`, otevřel fotku a
vrátil se zpět. Skončil jsem na `scrollY = 195` — prakticky nahoře. Vyzkoušeno oběma
cestami, které aplikace nabízí:

| Cesta zpět | Pozice před | Pozice po |
| --- | ---: | ---: |
| tlačítko Zpět v prohlížeči | 4 000 px | **195 px** |
| tlačítko „Zpět na seznam" v prohlížeči fotky | 6 000 px | **192 px** |

Celý dokument má **369 018 px**. Šest tisíc pixelů je tedy necelá dvě procenta
knihovny — byl jsem pořád ještě v roce 2025 — a i tu trochu jsem ztratil.

**Proč to vadí.** Prohlížení knihovny je střídání „mřížka → fotka → mřížka".
Když každý takový krok resetuje pozici, nedá se procházet starší část knihovny
vůbec: člověk se ke třinácti stům fotek z roku 2013 doscrolluje, otevře jednu,
a je zpátky u roku 2026. `/help` přitom výslovně slibuje opak: *„Stav prohlížení —
filtry, řazení i **pozici** — si aplikace pamatuje v adrese stránky, takže tlačítko
Zpět vždy funguje."* Tenhle slib aplikace nedodržuje (viz [N17](#n17)).

**Návrh.** `react-virtuoso` má na to `restoreStateFrom` / `stateChanged`: uložit
stav mřížky do `history.state` (nebo do `sessionStorage` pod klíč odvozený od URL)
při odchodu a obnovit ho při návratu. Alternativa s menším dosahem: do URL přidat
index první viditelné dlaždice a při načtení zavolat `scrollToIndex` — `PhotoGrid`
už `scrollToIndex` vystavuje ven.

**Kde to je.** `web/src/components/library/PhotoGrid.tsx` (imperativní handle kolem
řádku 169), `web/src/hooks/usePaginatedPhotos.ts`, `web/src/pages/LibraryPage.tsx`.
Týká se stejně tak detailu alba, štítku i osoby.

**✅ Vyřešeno (8. 8. 2026).** Pozici si pamatuje nový háček `useGridScrollMemory`
nad `lib/gridScroll`: jeden záznam v `sessionStorage` na *pohled* (cesta + filtry,
bez `at`/`info` — skok po časové ose je tentýž pohled, jiný filtr už ne), takže se
nikdy neobnoví pozice z nesouvisejícího výsledku. Mřížka ji hlásí přes
`stateChanged` a dostává zpět přes `restoreStateFrom`, což jsou nástroje, které
`react-virtuoso` na tohle má. U mřížek, které rostou přidáváním stránek (album,
štítek, osoba, oblíbené, hledání, místa), se navíc pamatuje **délka** seznamu:
`usePaginatedPhotos` má nový `initialCount` a při návratu dojde na tolik stránek,
kolik jich čtenář měl (strop `RESTORE_MAX_PAGES` = 12), protože do dokumentu
vysokého jednu stránku se hluboká pozice nedá obnovit. Galerie osoby není
virtualizovaná, takže se u ní vrací rovnou `window.scrollY`.

Změřeno v prohlížeči na 3 000 fotkách (vlastní instance nad testovací databází):
knihovna `scrollY = 20 000` → Zpět → **20 000**, „Zpět na seznam" z 35 000 →
**35 000**, detail alba 8 000 → **8 000** (znovu načteno 700 fotek), štítek 5 000 →
**5 000**, osoba 6 000 → **6 000** (500 dlaždic). `/?sort=oldest` startuje na **0**,
takže pozice z jiného řazení se nepřenáší.

---

<a id="n6"></a>
### N6 — Na telefonu panel obličejů i informací fotku úplně zakryje 🔴 🟡

**Co se stalo.** Na telefonu jsem otevřel fotku a zapnul zobrazení obličejů.
Panel „Obličejů: 5" se roztáhl přes **celý výřez** — `.kk-viewer__panel.is-open` má
393 × 852 px, neprůhledné pozadí `rgb(31,27,22)` a nad sebou ještě `panel-scrim`
s 55 % černou. Fotka je pořád v DOMu (`<img>` na `y = 300`, načtená, 1920 px zdroj),
ale **není z ní vidět ani pixel**. Stejně se chová panel „Informace".

Na téže obrazovce se navíc **zavírací křížek prohlížeče fotky (vlevo nahoře) překrývá
s nadpisem panelu** — písmena „In" ve slově „Informace" leží pod křížkem. A vedle
sebe jsou pak dva vizuálně shodné kulaté křížky ve stejné výšce: levý zavírá fotku,
pravý zavírá panel.

**Proč to vadí.** Panel obličejů dává smysl jen ve spojení s obrázkem — jeho řádky
odkazují na očíslované rámečky **na fotce**. Když fotku zakryje, není podle čeho se
orientovat a panel ztrácí funkci. Na počítači, kde panel sedí vedle fotky v prázdném
černém prostoru, to funguje dobře; na mobilu je to zkopírované rozvržení bez úpravy.
A dva stejné křížky vedle sebe u obrázku rodinné fotky znamenají, že člověk omylem
zavře celou fotku, když chtěl zavřít panel.

**Návrh.**
1. Na úzkých výřezech udělat z panelu **spodní zásuvku (bottom sheet)** přes zhruba
   40–50 % výšky, aby fotka nad ní zůstala vidět a dala se posouvat.
2. Zavírací křížek prohlížeče při otevřeném panelu buď schovat, nebo posunout mimo
   hlavičku panelu; a odlišit ho ikonou (šipka zpět vs. křížek).

**Kde to je.** `web/src/components/photo/viewer.css` (`.kk-viewer__panel`,
`.kk-viewer__panel-scrim`), `web/src/components/people/FacesPanel.tsx`,
`web/src/components/photo/MetadataPanel.tsx`.

**✅ Vyřešeno (8. 8. 2026).** Pod `md` je z bočního panelu **spodní zásuvka**: tentýž
prvek se překotví ke spodní hraně přes `--kk-viewer-sheet-h` (46 dvh — `dvh`, ne
`vh`, kvůli sjíždějícímu adresnímu řádku), scéna o přesně tutéž výšku ustoupí
(`bottom: var(--kk-viewer-sheet-h)`), takže fotka nad zásuvkou zůstane vidět
i s vlastními dotykovými gesty. **Stínítko zmizelo úplně** — i průhledné by
z fotky udělalo zavírací tlačítko, a tím pádem nepohyblivou fotku; zavírá se
křížkem zásuvky, přepínačem, který ji otevřel, nebo Esc. Zásuvka dostala úchyt
(pseudoprvek) a menší odsazení, seznam obličejů (i úprav) uvnitř ní přestal být
druhým posuvným oknem (`.kk-viewer__panel-scroll`, strop výšky se v zásuvce ruší).
**Dva křížky jsou pryč:** trvalé tlačítko vlevo nahoře je teď **šipka zpět**
(`kk-viewer__back`, `arrow-left`), křížek zůstal jen tomu, co je nad fotkou.
Křížek panelu je při 393 × 852 na `y = 487`, tlačítko zpět na `y = 10` — nemají se
kde potkat. Ověřeno v Chromiu na 393 × 852 (zásuvka 393 × 392, scéna 393 × 460,
fotka celá uvnitř scény) i na 1280 × 800, kde je rozvržení beze změny.

---

<a id="n7"></a>
### N7 — 438 alb v jedné hromadě, i když API už rozlišuje jejich typ 🔴 🟡

**Co se stalo.** `/albums` vykreslí jeden plochý, neroztříděný, nefiltrovatelný rošt
karet. Podle `GET /api/v1/albums?limit=1000` jich je **438** a mají pole `type`:

| `type` | Počet | Co to je |
| --- | ---: | --- |
| `folder` | **246** | automatická alba podle měsíce a importu |
| `album` | 166 | alba, která někdo skutečně vytvořil |
| `moment` | 23 | automatické „momenty" |
| `state` | 3 | kraje |

Z toho **239 alb** se jmenuje anglicky ve tvaru `January 2026`, `May 2026`, `April
2026`. Šest je prázdných a taky anglických (`Pets`, `Nature & Landscape`,
`Bays, Capes & Beaches` — zbytky kategorií z PhotoPrismu). Dalších 34 má méně než
tři fotky.

Nejnázornější příklad: album **„January 2026" obsahuje 2 305 fotek s rozsahem
1905–2026.** Leden 2026 je měsíc, ve kterém běžela migrace — takže album pojmenované
podle ledna 2026 patří k největším v knihovně a je plné skenů z první republiky.
Na detailu fotky z roku 1965 pak svítí štítek alba „January 2026".

**Proč to vadí.** Alba jsou v aplikaci hned druhá položka menu. Uživatel je otevře,
aby našel „Dovolená 2019", a dostane 438 dlaždic, kde 55 % tvoří anglicky
pojmenované strojové skupiny. Ta jeho jsou někde mezi nimi. Stránka nemá ani
hledání, ani řazení, ani stránkování (rošt je virtualizovaný a scrolluje 16 232 px).

**Proč je to levné.** Rozlišení už je v datech — UI ho jen ignoruje.

**Návrh.**
1. Rozdělit stránku na sekce nebo záložky podle `type`: **Moje alba** (`album`,
   výchozí) · **Podle měsíce** (`folder`) · **Momenty** · **Místa**. Uživatel pak
   ve výchozím pohledu vidí 166 alb, ne 438.
2. Přidat nad rošt **pole pro hledání v názvech** a přepínač řazení (název / počet
   fotek / datum).
3. Automatická alba pojmenovat česky (`Leden 2026`) — název skládá import, není to
   uživatelský text; totéž pro `Czech Republic 2026` → `Česko 2026`.
4. Prázdná alba (`photo_count = 0`) ve výchozím pohledu skrýt.
5. Zvlášť: automatické „album podle měsíce importu" u skenů starých fotek je věcně
   zavádějící — mělo by se odvozovat od `taken_at`, ne od data přidání.

**Kde to je.** `web/src/pages/AlbumsPage.tsx` (nepoužívá `type` z odpovědi API);
pojmenování vzniklo v importerech migrace, které byly 2026-08-06 odstraněny — data
zůstávají, kód, který je vyrobil, ne.

---

<a id="n8"></a>
### N8 — `/people`: 105 osob bez hledání, a 125 Mpx stažených na 72 čtverečků 🔴 🟡

**Co se stalo.** Dvě věci najednou.

*Za prvé:* stránka Lidé vykreslí **105 osob** jako abecední rošt a nemá **žádné
ovládání** — po nadpisu „Lidé" následuje rovnou 105 odkazů. Žádné hledání podle
jména, žádné řazení podle počtu fotek, žádné rozdělení na lidi a zvířata. Na
telefonu jsou to dvě osoby na řádek, tedy zhruba 53 řádků scrollování. Řadí se
podle křestního jména, takže „najdi babičku" znamená vzpomenout si na její jméno a
projet abecedu.

*Za druhé:* dlaždice osoby je čtverec **152 × 152 px**, do kterého se výřezem
promítne obličej z originální fotky. Zdrojem toho výřezu je ale plná varianta
náhledu — naměřeno na první obrazovce:

```
60 stažených náhledů:  28× fit_1920,  21× fit_1280,  11× fit_720
72 načtených obrázků:  125,5 megapixelu  →  vykresleno do 72 × (152×152) ≈ 1,7 Mpx
medián doby stažení:   2,8 s      maximum: 4,2 s
```

Tedy **zhruba 75× víc pixelů, než se použije**. Knihovní mřížka to dělá správně
(`tile_500`, 0,5 Mpx na obrazovku) — problém je specifický pro výřezy obličejů.
Prakticky to vypadá tak, že stránka Lidé se plní přes zhruba osm vteřin a mezitím je
plná prázdných tmavých čtverců. Detail osoby (`/people/:uid`) na tom byl stejně —
po třech vteřinách byly vykreslené tři dlaždice z dvaceti.

**Proč to vadí.** Přes stránku Lidé se chodí na „ukaž mi fotky babičky", což je jedna
z hlavních věcí, kvůli kterým se v rodinné galerii hledá. Dnes to znamená
osmisekundové čekání a ruční projíždění stovky jmen. Na mobilních datech je to horší
o řád.

**Návrh.**
1. **Hledání a řazení** nad rošt: pole pro filtrování podle jména (stejné, jaké už
   je ve filtru „Osoba" v knihovně) a přepínač *abecedně / nejvíc fotek*.
2. Zdroj výřezu vybírat podle **velikosti rámečku obličeje**, ne paušálně nejvyšší
   variantu: pro čtverec 152 px při DPR 2 stačí ~304 px výsledného výřezu, takže
   z žebříku `fit_*` má stačit nejmenší varianta, u které rámeček obličeje ještě
   vyjde na 304 px. (`tile_*` na to použít nejde — je to čtvercový ořez ze středu,
   rámeček by v něm seděl jinde.) Žebřík variant existuje, jen se z něj vybírá špatně.
3. Ještě lépe: **předgenerovat výřez obličeje** jako vlastní miniaturu (analogicky
   k `tile_500`), takže dlaždice osoby stahuje ~30 kB místo ~600 kB.
4. Do prázdných dlaždic dát kostru (`Skeleton`), aby stránka během načítání
   nevypadala rozbitě.

**Kde to je.** `web/src/pages/PeoplePage.tsx` (bez ovládání),
`web/src/components/people/SubjectTile.tsx` a `FaceCrop.tsx` (výběr varianty
náhledu), `web/src/pages/SubjectPage.tsx`. Serverová strana: `internal/thumb`
(případná nová varianta výřezu).

---

<a id="n9"></a>
### N9 — Seznam obličejů nemá náhledy, jmenovky na fotce se překrývají 🟡 🟡

**Co se stalo.** Na skupinové fotce s pěti lidmi jsem zapnul obličeje. Panel vypsal:

```
Obličej #1   [Bez jména]
Obličej #2   [jméno]
Obličej #3   [jméno]
Obličej #4   [jméno]
Obličej #5   [Bez jména]
```

Žádné náhledy obličejů, jen pořadová čísla. Ta čísla odpovídají malým odznáčkům
u rámečků na fotce, jenže **nejsou seřazená zleva doprava** — jednička byla uprostřed,
trojka vlevo od ní, pětka úplně vlevo. Ke spárování „Obličej #4" s konkrétním
člověkem tedy musím na fotce najít drobný číselný odznáček.

Zároveň se na fotce vykreslují **jmenovky pod rámečky a ty se navzájem překrývají** —
u dvou sousedních lidí ležela jedna jmenovka přes druhou a přes rámeček třetího.
U pěti obličejů je to nepříjemné; u skupinové fotky s patnácti je to nečitelná
hromada — a takových je v knihovně dost: dotaz `faces:3 face:new` (tři a víc
obličejů, aspoň jeden nepojmenovaný) vrací **2 937 fotek**.

Zajímavé je, že panel **Informace** na téže fotce zobrazuje osoby jako štítky
**s malým kulatým výřezem obličeje** — přesně to, co panel obličejů potřebuje a nemá.

**Proč to vadí.** Tohle je obrazovka, na které se lidé pojmenovávají — tedy hlavní
způsob, jak se ve fotkách 20 000 snímků dá později hledat. Když nejde spojit řádek
seznamu s obličejem, práce se zpomalí a roste riziko, že někdo pojmenuje špatného
člověka.

**Návrh.**
1. Do každého řádku panelu dát **výřez obličeje** (komponenta `FaceThumb` už
   existuje a používá se v `PeoplePanel`); slovo „Obličej #N" pak může zmizet.
2. Obličeje **seřadit zleva doprava, shora dolů** podle rámečku, aby čísla
   odpovídala tomu, jak fotku čte oko.
3. Při najetí/dotyku na řádek zvýraznit odpovídající rámeček na fotce (a naopak).
4. Jmenovky na fotce buď zobrazovat jen u najetého rámečku, nebo je při překryvu
   sbalit na číslo.

**Kde to je.** `web/src/components/people/FacesPanel.tsx` (řádky ~74–95),
`web/src/components/people/FaceOverlay.tsx`, `FaceThumb.tsx`.

---

<a id="n10"></a>
### N10 — `/labels`: 113 štítků jako svislý seznam bez hledání a řazení 🟡 ⚪

**Co se stalo.** Stránka Štítky vypíše **113 štítků** jako jeden sloupec plných
řádků: každý řádek zabírá celou šířku 1 106 px, aby nesl jedno slovo a číslo.
Dokument má **5 730 px**. Stránka nemá **žádné ovládání** — jen nadpis a 113 odkazů.

Řadí se abecedně, takže začátek vypadá takhle:

```
Dolňák 36 · Dum11 8 · Dum12 5 · Dum20 3 · Dum32 2 · Dum33 3 · Dum38 7 · Dum4 7 ·
Dum41 4 · Dum47 9 · Dum51 5 · Dum56 3 · Dum59 7 · Dum68 6 …
```

Desítky štítků `DumNN` (čísla popisná) obsadí celý začátek seznamu a vytlačí věcné
štítky mimo první obrazovky. Na telefonu se vejde 14 řádků na obrazovku, takže než
se seznam přehoupne za písmeno „D", je za sebou několik obrazovek samých „Dum".

**Proč to vadí.** `Štítky` jsou jedna ze **tří** položek spodní lišty na mobilu —
tedy jedna ze tří věcí, které aplikace na telefonu považuje za nejdůležitější. To,
co za tím tlačítkem je, tomu neodpovídá.

**Návrh.**
1. Zobrazit štítky jako **mrak pilulek** (`badge`/`chip`) v obtékajícím layoutu, ne
   jako sloupec plných řádků — 113 štítků se pak vejde na jednu až dvě obrazovky.
2. Přidat **pole pro hledání** a řazení *podle počtu fotek* (výchozí) / *abecedně*.
3. Volitelně: štítky s číselným vzorem (`Dum\d+`) seskupit pod jednu rozbalovací
   položku, aby nezaplavovaly seznam.
4. Zvážit výměnu slotu ve spodní liště — viz [N3](#n3).

`UX_AUDIT.md` už u tohohle konstatuje nekonzistenci „Alba = rošt karet vs. Štítky =
seznam"; při 113 položkách to ale není kosmetika, ale skutečná cena za nalezení
štítku.

**Kde to je.** `web/src/pages/LabelsPage.tsx`.

---

<a id="n11"></a>
### N11 — Příznaky se jmenují podle tvaru ikony: „Oko", „Palec nahoru" 🟡 ⚪

**Co se stalo.** V hlavičce prohlížeče fotky jsou vedle hvězdiček tři ikony. Jejich
přístupné popisky (a tedy i bublinové nápovědy a to, co přečte odečítač obrazovky)
znějí:

```
button "Oko"
button "Palec nahoru"
button "Palec dolů"
```

Tedy popis **tvaru ikony**, ne toho, co dělá. Ve filtru knihovny se táž vlastnost
jmenuje „Označení" a nabízí hodnoty „Vše / Vybrané / Zamítnuté / **Označené okem**".

**Proč to vadí.** Nikdo se z „Palec nahoru" nedozví, že si tím fotku označuje jako
vybranou pro další zpracování. „Označené okem" je pak jazykově nesmysl — jméno
hodnoty se opisuje ikonou. Uživatel tuhle trojici buď nepoužije vůbec, nebo jí
označí něco jiného, než myslel.

**Návrh.** Přejmenovat na to, co příznaky znamenají — např. **„Vybrat" /
„Zamítnout" / „Prohlédnout později"** (nebo cokoli, co odpovídá zamýšlené
sémantice `pick` / `reject` / `eye`) a stejné pojmenování použít v hodnotách filtru.
K ikonám doplnit textovou bublinu s jednou větou vysvětlení. Klíče jsou v i18n, jde
o překlady + `aria-label`.

**Kde to je.** `web/src/components/library/FlagControl.tsx`,
`web/src/components/library/FilterBar.tsx` (hodnoty „Označení"), `web/src/i18n`.

**✅ Vyřešeno (8. 8. 2026).** Tlačítka se jmenují podle toho, co dělají:
**„Vybrat" / „Zamítnout" / „Prohlédnout později"** (en „Pick" / „Reject" /
„Look at later"). Krátké jméno nese `aria-label`, `title` k němu přidal jednu
větu, co příznak znamená (`rating.pickHint` / `rejectHint` / `eyeHint`) — dřív
tam byla jen kopie jména. Táž jména dostaly hodnoty filtru „Označení"
(„Vše / Vybrané / Zamítnuté / **K prohlédnutí později**"), takže filtr
a prohlížeč mluví stejně, a nápověda dotazovacího jazyka u `flag:` teď ke
každé hodnotě dopisuje její jméno. Uložené hodnoty `pick`/`reject`/`eye`,
API ani klíč `flag:` se nezměnily — je to změna wordingu v UI.

---

<a id="n12"></a>
### N12 — Do textu pro uživatele prosakuje `AI_MODEL:`, `Unknown` a anglické názvy 🟡 ⚪

**Co se stalo.** Několik různých úniků na jedné obrazovce (detail fotky, panel
Informace):

1. Pod nadpisem **„Automatický popis"** stojí hezky česky napsaný odstavec a hned
   za ním na samostatném řádku:
   ```
   AI_MODEL: gemini-2.5-flash
   ```
   V API je to součást pole `ai_note`, kde je model připojený k textu dvěma
   odřádkováními při importu z photo-sorteru. UI to vypisuje 1 : 1.

2. V **Technických údajích** stojí „Fotoaparát: **Unknown**", „Objektiv:
   **Unknown**" — v API je opravdu uložený řetězec `Unknown` (ne prázdno), takže se
   řádek vykreslí i u naskenované fotky, u které se fotoaparát nikdy zjistit nedal,
   a to anglicky uprostřed české tabulky.

3. **Automaticky složené názvy** míchají jazyky: fotka se jmenuje
   `<Jméno> / Czech Republic / 2026`. Anglický název země je i v názvech alb
   (`Czech Republic 2026`, `Czech Republic 2025`) vedle českých (`Jihovýchod`).

4. Anglické názvy měsíčních alb — viz [N7](#n7).

**Proč to vadí.** „Automatický popis" je jediné místo, kde aplikace uživateli sama od
sebe něco říká o obsahu fotky. Když ta věta končí řetězcem `AI_MODEL: gemini-2.5-flash`,
vypadá to jako chyba a shazuje důvěru ve zbytek. `Unknown` a `Czech Republic` mají
stejný účinek v malém.

**Návrh.**
1. `AI_MODEL: …` z `ai_note` odstranit — buď jednorázovou migrací dat, nebo
   ořezáním při zobrazení (řádek začínající `AI_MODEL:` nevypisovat). Pokud má
   informace o modelu zůstat, patří do Technických údajů, ne do popisu.
2. Hodnoty `Unknown` / prázdné neuvádět vůbec, nebo lokalizovat na „Neuvedeno".
3. Názvy zemí lokalizovat (reverse geocoding vrací `Česko` na `/places`, ale do
   názvů se dostává `Czech Republic` — sjednotit zdroj).

**Kde to je.** `web/src/components/photo/MetadataPanel.tsx` (vykreslení `ai_note`),
`web/src/components/photo/TechnicalDetails.tsx`, skládání názvu fotky/alba
v `internal/places` a v odstraněných importerech migrace.

**✅ Vyřešeno (8. 8. 2026).** Všechno třemi kroky **při zobrazení**; v databázi se
nepřepsalo nic.

1. `splitAiNote` (`lib/photoFacts`) rozdělí `ai_note` na `{ text, model }` podle
   **koncového** řádku `AI_MODEL:`. „Automatický popis" ukazuje `text`, model se
   přesunul do Technických údajů jako řádek **„Model AI"**. Značka uprostřed věty
   je něčí text a zůstává; editační formulář drží dál **uloženou** hodnotu i se
   značkou, aby uložení kvůli něčemu jinému nemohlo model tiše zahodit.
2. `metaValue` (`lib/photoFacts`) považuje uložené `Unknown` za totéž co prázdno.
   Pravidlo je v `MetaField`, takže platí pro **každý** řádek tabulky: řádek se
   nevykreslí vůbec (skupina taky ne, když v ní nic jiného nezbylo) a fotoaparát
   spadne z `camera_model` na `camera_make` až po vyřazení zástupné hodnoty.
3. `localizeCountryNames` (`i18n/countryNames`, sdílený slovník s `albumNames`)
   přeloží anglický název země, když stojí jako **celý** segment složeného jména
   oddělený `/` nebo `,` — případně s koncovým čtyřmístným rokem. Tím se z
   `Jan / Czech Republic / 2026` stane `Jan / Česko / 2026` a z alba
   `Czech Republic 2026` `Česko 2026`, ale `New Zealand trip` zůstane. Používá to
   nadpis detailu fotky, popisky dlaždic (`photoLabel`), nadpis alba, odznaky alb
   na fotce a výsledky v paletě příkazů. Neznámý název i anglické UI dostanou
   uložený řetězec beze změny.

---

<a id="n13"></a>
### N13 — Stránka, na kterou nemám právo, mlčky přesměruje na knihovnu 🟡 ⚪

**Co se stalo.** Zadal jsem přímo adresu `/review`. Skončil jsem na `/`, bez jediného
slova vysvětlení. Totéž u `/duplicates`, `/people/clusters`, `/outliers`, `/upload`,
`/trash`. API k nim odpovídá poctivě `403`, jen se to nikde neprojeví.

Nejlépe je to vidět na `Žebříčku`: má tlačítko „**Začněte třídit**" mířící na
`/review`. Prohlížeč ho zmáčkne a ocitne se v knihovně. Vypadá to, že tlačítko
nefunguje.

**Proč to vadí.** Rodinná galerie je typicky sdílená a odkazy mezi lidmi kolují.
Když někdo pošle *„mrkni na tyhle duplicity"*, příjemce s právem jen ke čtení
skončí na jiné stránce, než na kterou klikl, a nedozví se proč. Interpretace, která
se nabízí, je „aplikace je rozbitá".

**Návrh.** Místo tichého přesměrování ukázat na chráněné cestě **stránku 403** ve
stylu `NotFoundPage`: jedna věta („Na tuhle část potřebuješ roli editora — poproste
o ni správce.") a odkaz zpět do knihovny. Zároveň skrýt nebo zašednout odkazy, které
pro danou roli nikam nevedou (tlačítko na `Žebříčku`).

**Kde to je.** Ochrany cest v `web/src/App.tsx` / `web/src/auth`,
`web/src/pages/LeaderboardPage.tsx` (tlačítko), `web/src/pages/NotFoundPage.tsx`
(vzor pro novou stránku).

**✅ Vyřešeno (8. 8. 2026).** `RequireRole` a `RequireImport` už nepřesměrovávají.
Místo `<Navigate to="/">` vykreslí **na té samé adrese** novou `ForbiddenPage` ve
stylu `NotFoundPage`: nadpis, jedna věta a odkaz zpět do knihovny. Věta pojmenuje
roli, která chybí, a každá role má vlastní znění (`forbidden.message.{editor,admin,
maintainer}`) — čeština by ji jinak musela skloňovat a cesta k roli se liší.
Protože se nikam nenaviguje, **adresa zůstane** na chráněné cestě: obnovení stránky
ukáže totéž vysvětlení a `Zpět` vede tam, odkud uživatel přišel. Přihlášení je
výjimka — `RequireAuth` dál posílá na `/login`, protože tam se chybějící krok
opravdu udělá. Na `Žebříčku` zmizely obě pozvánky do třídění (tlačítko „Začněte
třídit" i řádek „Zatím nejste na žebříčku") pro role bez práva zápisu; prohlížeč
místo nich dostane jiný text prázdného stavu. Ostatní odkazy na cesty jen pro
editory (`/upload` v knihovně, `/people/clusters` u osob, celé menu) už `canWrite`
hlídaly.

---

<a id="n14"></a>
### N14 — Zašedlá tlačítka bez vysvětlení, proč nejdou zmáčknout 🟡 ⚪

**Co se stalo.** Jako prohlížeč vidím řadu ovládacích prvků, které jsou přítomné,
ale `disabled`, a nikde není napsáno proč:

- řádky se jmény v panelu obličejů (`button … [disabled]`),
- tlačítko „**Zjistit místo**" pod mapkou v panelu Informace,
- „**Uložit pohled**" v hlavičce knihovny a hledání.

Zároveň některé zápisy prohlížeči **projdou** — srdíčko oblíbených fungovalo
(oblíbené jsou osobní, to dává smysl). Z pohledu uživatele je tedy stav
„některá tlačítka jdou, jiná ne, a nevím podle čeho".

**Proč to vadí.** Zašedlé tlačítko bez důvodu čte člověk jako poruchu, ne jako
oprávnění. Zvlášť když vedle něj funguje jiné tlačítko na téže liště.

**Návrh.** Ke každému prvku zakázanému kvůli roli přidat **bublinovou nápovědu
jedním textem** („Na tohle potřebuješ roli editora."), nebo takové prvky pro roli
prohlížeče vůbec nevykreslovat. Konzistentně jedno nebo druhé; dnes je to obojí.

**Kde to je.** `web/src/components/people/FacesPanel.tsx`,
`web/src/components/photo/PhotoLocation.tsx`,
`web/src/components/savedsearch/*`, `web/src/pages/LibraryPage.tsx`.

**✅ Vyřešeno (8. 8. 2026).** Pravidlo je jedno a zní: **prvek, na který nemáte
roli, se nevykreslí vůbec** — zašedlé tlačítko v Kukátku tedy nikdy neznamená
„ne vy", vždycky jen „teď ne". Obojí se tak dá rozeznat, aniž by se na cokoli
muselo klikat. K tomu druhá půlka pravidla: **co je zašedlé, musí říct proč.**
Nese ji nová sdílená komponenta `ReasonedButton`, která **nemá `disabled`** —
jediný způsob, jak ji vypnout, je `disabledReason`, jedna hotová věta („Nejdřív
vyberte fotky, na které se má úprava použít."). Vypíná se přes `aria-disabled`,
ne přes nativní atribut: `<button disabled>` vypadne z pořadí tabulátoru (a s ním
i vysvětlení) a Bootstrap mu navíc dává `pointer-events: none`, takže se nikdy
neukáže ani `title` — nápověda na nativně zakázaném tlačítku je tedy neviditelná
pro myš i pro klávesnici. Věta se čte třemi cestami: `title` pro myš,
`aria-describedby` na skrytou poznámku pro klávesnici a odečítač, a tam, kde už
věta na obrazovce je (řádky v `UsersPage`), `reasonId` na ten viditelný řádek —
telefon, který `title` nikdy nedostane, si ji přečte očima.

Ke třem konkrétním místům z hlášení: řádky v panelu obličejů nejsou tlačítka,
prohlížeč u nich má napsáno proč („Obličeje si můžete prohlédnout, ale na jejich
pojmenování potřebujete roli editora…") místo mlčenlivého seznamu, který
nereaguje na klik. „**Zjistit místo**" je čtení, ne zápis (mapy.com stojí kredity,
ne oprávnění) — pro prohlížeče zůstává živé a zhasíná jen po dobu vlastního
dotazu, tehdy s větou „Místo se právě zjišťuje". „**Uložit pohled**" je osobní
jako srdíčko oblíbených, žádná role ho nehlídá; v knihovně teď má stejné
jednořádkové vysvětlení jako v hledání, aby se nepletlo se zašedlými prvky vedle.
Stejné ošetření dostaly „Hromadná úprava" a „Seskupit vybrané" (řeknou, že chybí
výběr, ne že jsou rozbité) a tři akce na řádku uživatele.

---

<a id="n15"></a>
### N15 — Karta prohlížeče se vždy jmenuje „Kukátko" 🟡 ⚪

**Co se stalo.** `document.title` je na knihovně, na `/people` i na detailu fotky
shodně `"Kukátko"`. Nikde se nemění.

**Proč to vadí.** Zadání zmiňuje úkol *„najdi tu fotku, co jsem viděl minulý
týden"*. Nejpřirozenější cesta k němu je historie prohlížeče — jenže ta obsahuje
padesát položek „Kukátko" bez rozlišení. Stejně tak se nedají rozeznat dvě otevřené
karty (v galerii se běžně otevírá fotka do nové karty) a záložka na uložený pohled
se jmenuje „Kukátko".

**Návrh.** Nastavovat titulek podle stránky: `Knihovna · Kukátko`,
`<název fotky> · Kukátko`, `<jméno osoby> · Kukátko`, `Hledání „svatba" · Kukátko`.
Data pro to už každá stránka má.

**Kde to je.** `web/src/components/Layout.tsx` nebo jednotlivé stránky
(`useEffect` nad `document.title`, případně sdílený hook).

**✅ Vyřešeno (8. 8. 2026).** Titulek karty se teď jmenuje podle stránky:
`Knihovna · Kukátko`, `Svatba 1965 · Kukátko`, `Jarmila · Kukátko`,
`Hledání „svatba" · Kukátko`, `Album Dovolená · Kukátko` — a tak dál pro každou
stránku aplikace, včetně přihlášení, nápovědy i provozních obrazovek.

Nese to **jeden sdílený hook** `useDocumentTitle(title)`
(`web/src/hooks/useDocumentTitle.ts`), který si každá stránka zavolá se svým
jménem; není to tabulka u routeru, protože zajímavé titulky — jméno fotky, osoby,
alba — zná jen ta stránka, která ta data drží. Formátování, i18n i úklid jsou
proto na jednom místě: `documentTitle.page` = `{{title}} · Kukátko` (oddělovač
i pozice značky jsou tím pádem přeložitelné), přepnutí jazyka titulek přepíše,
a **odchod ze stránky vrací holé „Kukátko"** — to je to, co drží jméno fotky
mimo knihovnu. Když stránka jméno ještě nezná (detail se načítá), předá `null`
a karta se jmenuje „Kukátko" místo cizího nebo vymyšleného jména.

Statické stránky předávají svůj vlastní nadpis (`t('albums.title')` a spol.),
takže se titulek karty a `<h1>` nemůžou rozejít; dynamické předávají data —
album pod stejným `albumDisplayTitle`, jaký ukazuje nadpis, štítek, osobu jejím
jménem, fotku tím jedním řetězcem, co viewer píše do nadpisu (název, jinak kdy
a kde), a hledání dotazem **z URL**, ne z rozepsaného pole. Žádný dotaz navíc
nikam neposílá: data už na stránce jsou.

---

<a id="n16"></a>
### N16 — Mobilní zásuvka filtrů nemá patičku s počtem výsledků ani „Použít" 🟡 ⚪

**Co se stalo.** Na telefonu se filtry otevřou jako celoobrazovková zásuvka — sama
o sobě je udělaná dobře, ovládací prvky jsou velké a čitelné. Ale:

- **nemá patičku** (`.offcanvas-footer` neexistuje) a **neobsahuje jediné tlačítko**
  (`[...d.querySelectorAll('button')]` → prázdné pole),
- nikde v ní **není počet výsledků** — údaj „Počet fotek: N" zůstává na stránce
  pod zásuvkou, tedy neviditelný,
- jediná cesta ven je křížek úplně nahoře, ke kterému se musí doscrollovat zpátky
  přes deset polí.

**Proč to vadí.** Filtruje se naslepo: nastavím rok, osobu a hodnocení, zavřu
zásuvku, a teprve pak zjistím, že výsledek je nula. A pak celou cestu znovu.

**Návrh.** Přidat do zásuvky přilepenou patičku se dvěma věcmi: **živý počet
výsledků** („Zobrazit 227 fotek") jako hlavní tlačítko, které zásuvku zavře, a
vedle něj **„Zrušit filtry"**. Počet už stránka zná, jen se v zásuvce nevykresluje.

**Kde to je.** `web/src/components/library/FilterBar.tsx` (varianta pro úzký výřez).

---

<a id="n17"></a>
### N17 — Nápověda o dotazovacím jazyce mlčí a slibuje zapamatovanou pozici 🟡 ⚪

**Co se stalo.** `/help` je dobře napsaná stránka s obsahem o třinácti kapitolách
(Procházení fotek, Hledání, Alba, Štítky, Oblíbené a hodnocení, Lidé a obličeje,
Duplikáty, Varianty jednoho snímku, Mapa a místa, Mazání a koš, Import knihovny,
Uživatelské role, Váš účet). Dva problémy:

1. **Kapitola „Hledání" nezmiňuje dotazovací jazyk ani jedním slovem.** Mluví o
   rychlém filtru a o hledání podle obsahu. To, že aplikace umí `person:`, `year:`,
   `label:`, `rating:`, `faces:` a rozsahy, se uživatel z nápovědy nedozví — jen
   z vyskakovacího okna za ikonou **?** na stránce, na kterou z menu nevede odkaz
   ([N3](#n3)).
2. **Kapitola „Procházení fotek" slibuje něco, co neplatí:** *„Stav prohlížení —
   filtry, řazení i pozici — si aplikace pamatuje v adrese stránky, takže tlačítko
   Zpět vždy funguje."* Filtry a řazení v adrese skutečně jsou, **pozice ne**
   ([N5](#n5)). *(Vyřešeno 8. 8. 2026 spolu s [N5](#n5): pozici si aplikace pamatuje
   po dobu návštěvy — ne v adrese — a věta v nápovědě to teď říká takhle.)*

**Proč to vadí.** Nápověda je jediné místo, kde se dá funkce objevit bez toho, aby
na ni člověk narazil. Když v ní nejsilnější funkce chybí, prakticky neexistuje.
A nepravdivá věta v nápovědě je horší než žádná — uživatel se domnívá, že dělá
něco špatně.

**Návrh.** Doplnit do kapitoly „Hledání" krátkou sekci o `klíč:hodnota` se třemi
příklady a odkazem na plnou tabulku (nebo tam rovnou vložit tutéž komponentu
`SearchQueryHelp`). Větu o zapamatované pozici buď opravit, nebo — lépe — splnit
podle [N5](#n5).

**Kde to je.** `web/src/pages/HelpPage.tsx`, texty v `web/src/i18n`.

**✅ Vyřešeno (8. 8. 2026).** Kapitola „Hledání" dotazovací jazyk **učí**, ne jen
zmiňuje. Nejdřív tři hotové dotazy, které stačí zkopírovat do pole — `year:1965`,
`person:Jarmila rating:4-5`, `album:"Léto 2024" faces:2`, u každého jedna věta, co
udělá — a hned pod nimi **celá tabulka operátorů a filtrů**.

Ta tabulka není opsaná: nápověda vykresluje **tutéž komponentu**, kterou otevírá
`?` u vyhledávacího pole. Reference se proto vystěhovala z modálu do
`components/search/SearchQueryReference.tsx` a `SearchQueryHelp` je od té chvíle
jen `?` a modál kolem ní. Syntaxe má jeden zdroj pravdy — nový filtr
v `QUERY_HELP_ROWS` se objeví i v nápovědě, místo aby ji tiše nechal zastarat.

Druhá půlka nálezu byla vyřešená už s [N5](#n5); tady jen ověřená v běžící
aplikaci: knihovna doscrollovaná na 20 000 px → otevřít fotku → Zpět → 20 000 px,
a `Zpět na seznam` z 35 000 px → 35 000 px (nasazené sestavení, které
`useGridScrollMemory` ještě nemá, přistane v obou případech na ~190 px). Věta
v nápovědě tedy slibuje přesně to, co sestavení, se kterým jde ven, umí.

---

<a id="n18"></a>
### N18 — `Rok` (109 položek) a `Pořízeno od/do` jsou dva soupeřící filtry data 🟡 🟡

**Co se stalo.** Knihovna má v základní řadě rozbalovátko **Rok** — nativní `<select>`
se **109 položkami** (2026 až 1905, s dírami: 1964 tam není, 1992 taky ne). Vybrat
jde právě jeden rok. O jedno kliknutí hlouběji, v panelu **Filtry**, je pak druhá
dvojice polí **Pořízeno od / Pořízeno do**, která umí rozsah.

Praktický důsledek pro úkol *„najdi fotky babičky z šedesátých let"*: viditelný
ovladač na to nestačí — desetiletí se z něj poskládat nedá — a ovladač, který na to
stačí, je schovaný. Nejrychlejší cesta se ukázala být napsat `year:1960-1969` do
vyhledávacího pole, tedy funkce, o které pole tvrdí, že ji nemá ([N2](#n2)).

Filtry se navíc nesynchronizují: při dotazu `year:1960-1969` ukazuje `Rok` dál
„Libovolný rok".

**Proč to vadí.** Knihovna sahá do roku 1905. Časové rozpětí je u téhle konkrétní
sbírky **hlavní** osa vyhledávání a základní ovladač na ní neumí to, co lidé chtějí
(desetiletí, „před rokem 1950", „léto 2019").

**Návrh.**
1. Nahradit `Rok` **filtrem období**: nabídka desetiletí (`1960–1969`) rozbalitelná
   na roky, nebo dvojice „od roku / do roku". Časová osa vpravo už rozsahy zná —
   `timelineRail.ts` počítá segmenty typu „čvc 1965 – led 1967".
2. Sjednotit to s `Pořízeno od/do`, ať nejsou dva filtry téhož.
3. Stav filtrů odvozovat z dotazu, aby si neodporovaly.

**Kde to je.** `web/src/components/library/FilterBar.tsx`,
`web/src/hooks/useLibraryFacets.ts`, `web/src/components/library/timelineRail.ts`.

---

<a id="n19"></a>
### N19 — Mobil: 350 z 852 px je ovládání, a časová osa je skrytá 🟡 🟡

**Co se stalo.** Na telefonu (393 × 852) začíná první fotka v knihovně na
**`y = 350 px`**. Nad ní je: horní lišta (49 px) → nadpis „Knihovna" + tlačítka
Promítání a Uložit pohled → vyhledávací pole → řazení → ovladač hustoty + Filtry →
nápověda „Filtruje název a popis." → „Počet fotek: 20637". Se spodní lištou (54 px)
zbývá na fotky **448 px, tedy 53 % obrazovky**, na hlavní obrazovce galerie.

Zároveň má `.kukatko-timeline` na mobilu `display: none`. Časová osa — jediné
ovládání, kterým se dá 369 018 px dlouhý seznam přeskočit na rok 1965 — na telefonu
neexistuje. Zbývá jen scrollování.

**Proč to vadí.** Fotky se prohlížejí hlavně na telefonu. Tam má aplikace nejmíň
místa a utrácí ho za ovládání, které se používá občas, a naopak odebírá jediný
nástroj na rychlý pohyb ve dvacetitisícové knihovně.

**Návrh.**
1. Na úzkých výřezech sbalit řádek s řazením a hustotou **do tlačítka Filtry**
   (zásuvka na ně má místo dost), nechat nahoře jen vyhledávací pole.
2. Nadpis „Knihovna" na mobilu vypustit — spodní lišta už říká, kde jsem.
3. Hlášku „Filtruje název a popis." nahradit (viz [N2](#n2)) a počet fotek přesunout
   k filtrům.
4. Doplnit **mobilní podobu časové osy**: buď úzký proužek na pravé hraně, nebo
   plovoucí bublina s rokem, která se objeví při scrollování a jde do ní ťuknout.

**Kde to je.** `web/src/pages/LibraryPage.tsx`,
`web/src/components/library/FilterBar.tsx`,
`web/src/components/library/TimelineScrubber.tsx`, `web/src/styles/app.css`.

---

<a id="n20"></a>
### N20 — Ovládání prohlížeče fotky se schová i s jedinou cestou zpět 🟡 ⚪

**Co se stalo.** Otevřel jsem adresu fotky přímo. Po několika vteřinách nečinnosti
zůstal na obrazovce **jen obrázek a jeden malý křížek vlevo nahoře** — bez názvu,
bez hvězdiček, bez šipek na další fotku. Na počítači se všechno vrátí, jakmile
pohnu myší. Na telefonu tenhle „pohyb myší" neexistuje a nic uživateli nenaznačuje,
čím ovládání vrátit (skutečné dotykové chování jsem neověřoval — emulace nehlásila
`pointer: coarse`, viz Metoda).

Křížek je navíc **vlevo nahoře**, kde se zavírací tlačítka běžně nečekají, a při
otevřeném panelu se překrývá s jeho nadpisem ([N6](#n6)).

**Proč to vadí.** Kdo přijde na fotku z odkazu, vidí jen obrázek a netuší, že je
v aplikaci, ve které se dá jít dál. Skrytí ovládání je u promítání správné; u
prohlížeče jedné fotky, kde je to zároveň jediná cesta ven, je to riskantní.

**Návrh.** Nechat viditelné aspoň **zavírací/zpětné tlačítko a název fotky** trvale
(nebo je zeslabit místo úplného skrytí). Skrývat zbytek. Křížek nahradit šipkou
zpět, když se z prohlížeče vrací do seznamu.

**Kde to je.** `web/src/hooks/useAutoHideChrome.ts`,
`web/src/components/photo/viewer.css`.

**✅ Vyřešeno (8. 8. 2026).** Nečinnost už nebere celou horní lištu. Odejdou její
**ovládací prvky**, šipky a spodní lišta; zůstane **cesta ven** (trvalá šipka zpět,
která neblednutím nikdy neprošla) a **název fotky**, zeslabený na `opacity: 0.72`
místo úplného zmizení. Ztmavovací závoj lišty se přesunul na pseudoprvek
`.kk-viewer__chrome::before` a v klidu ztenčí na `0.55` — přechod přes průhlednost,
protože přechod přes přechod (gradient) neexistuje —, takže název drží čitelnost
i nad přepáleným nebem (ověřeno v prohlížeči; navíc mu v klidovém stavu zesílí
vlastní stín). Rozhoduje o tom `viewer.css`, ne háček: ten dál drží jen jeden
příznak `data-chrome` na kořeni prohlížeče. Křížek nahradila šipka zpět spolu
s [N6](#n6).

---

<a id="n21"></a>
### N21 — Obálky alb se opakují, alba jdou od sebe rozeznat jen podle názvu 🟡 🟡

**Co se stalo.** Na první obrazovce `/albums` měla **čtyři** alba naprosto stejnou
obálku (sken titulní strany fotoknihy) a další **dvě** tutéž fotku z ohně. Na
telefonu, kde jsou dvě alba na řádek, jsou to dvě obrazovky identických dlaždic.
Obálka se zjevně bere jako „nejnovější fotka v albu", takže překrývající se alba
vypadají stejně.

**Proč to vadí.** Rošt karet má smysl jen tehdy, když se karty od sebe liší. Když
se neliší, je to horší než seznam — zabírá víc místa a nepomáhá.

**Návrh.**
1. Obálku vybírat s ohledem na to, aby se **v rámci stránky neopakovala** (při
   kolizi sáhnout po další fotce v albu).
2. Nebo z obálky udělat **koláž 2 × 2** ze čtyř různých fotek alba — čtyři obrázky
   se opakují mnohem hůř než jeden.
3. Umožnit ruční nastavení obálky u alba (u osoby už to existuje).

**Kde to je.** `web/src/pages/AlbumsPage.tsx`, `cover_uid` z `internal/organize`.

**✅ Vyřešeno (8. 8. 2026).** Obálkou alba jsou **čtyři fotky místo jedné**:
`GET /albums` vrací vedle `cover_uid` i `cover_uids` — osm nejnovějších viditelných
fotek alba, řez toho samého pole, ze kterého se obálka brala doteď, takže dotaz
nestojí ani o milisekundu víc (210 ms / 1 024 bloků proti 208 ms / 1 013 předtím).
Z nich `lib/albumCovers` poskládá **koláž 2 × 2**; album, které na ni nemá dost
fotek, spadne zpátky na jeden obrázek — koláž vycpaná opakováním je horší než
poctivá jedna fotka. A obojí přitom **přeskakuje fotky, které si vzala dřívější
dlaždice**, takže dvě alba postavená z těch samých fotek ukážou osm různých.
Plánuje se celý seznam najednou (`useMemo` nad `visible`), ne dlaždice po
dlaždici: rošt je virtualizovaný a plán počítaný podle právě viditelného okna by
dlaždici při každém návratu přidělil jinou obálku. Ručně vybraná obálka
(`cover_photo_uid`) všechno přebíjí a zobrazí se samostatně — někdo odpověděl na
otázku „jak album vypadá" a odhad nesmí přebít rozhodnutí. Bod 3 návrhu (ruční
nastavení obálky z rozhraní) je samostatná zapisovací funkce a součástí téhle
změny není.

---

<a id="n22"></a>
### N22 — `/stats` mluví o „Embeddingách" a nenabízí žádnou akci 🟡 ⚪

**Co se stalo.** Stránka Statistiky (dostupná **každému uživateli**, ne jen adminům)
má pět karet. Jedna z nich se jmenuje **„EMBEDDINGY"** a její řádky znějí
„Embeddingů celkem 20 664", „Fotek s embeddingem 20 664", „Fotek bez embeddingu 242".
Karta „Lidé a zvířata" ukazuje „Nepojmenovaných obličejů **16 585**" žlutě
zvýrazněné — a **nikam neodkazuje**. Na celé stránce není jediný odkaz do aplikace.

**Proč to vadí.** „Embedding" nemá v rozhraní pro rodinu co dělat; `UX_AUDIT.md` ho
uvádí v inventáři žargonu, ale jen mezi administrátorskými stránkami — tady je na
stránce pro všechny. A žluté číslo „16 585 nepojmenovaných obličejů" je vlastně
výzva k akci, po které nenásleduje žádná akce.

**Návrh.**
1. Kartu přejmenovat na něco jako **„Vyhledávání podle obsahu"** a řádky přeložit
   („Připraveno k hledání podle obsahu 20 664 fotek · Zbývá zpracovat 242").
2. Ze zvýrazněných čísel udělat **odkazy**: nepojmenované obličeje → `/review`,
   fotky bez obličeje → knihovna s `faces:0`, koš → `/trash`.
3. Rolím bez práva zápisu odkaz na `/review` nezobrazovat (viz [N13](#n13)).

**Kde to je.** `web/src/pages/StatsPage.tsx`,
`web/src/components/LibraryStatsCards.tsx`, texty v `web/src/i18n`.

**✅ Vyřešeno (8. 8. 2026).** Slovo „embedding" ze stránky zmizelo — karta se
jmenuje **„Vyhledávání podle obsahu"** a říká, co z toho uživatel má:
v čele **„Fotek připravených k hledání podle obsahu 20 664"**, pod tím jediný
řádek **„Zbývá zpracovat 242"**. Zrušený řádek nic neubral: `embeddings` (počet
řádků v tabulce) a `photos_with_embedding` jsou v `internal/system/store.go`
totéž číslo, `embeddings` má `photo_uid` jako primární klíč. Čísla se počítají
úplně stejně jako předtím.

Zvýrazněná čísla teď někam vedou: **v koši** → `/trash`, **fotky bez obličeje** →
knihovna s filtrem `/?q=faces%3A0` (vlastní filtr dotazovacího jazyka, takže cíl
je běžný pohled knihovny, který jde dál filtrovat i uložit), **nepojmenované
obličeje** → `/review`. Odkaz je `Link` s třídami `text-reset
text-decoration-underline` — podědí barvu řádku (zvýrazněná mezera zůstane
žlutá) a přibere podtržení, aby byl poznat; přístupné jméno je cíl, ne číslo
(„16 585" samo o sobě neříká nic). Obě zapisovací cesty jsou podmíněné
`useAuth().canWrite`: prohlížející uživatel vidí prosté číslo, ne odkaz, který by
ho odmítl ([N13](#n13)). Odkaz do knihovny zůstává všem.

---

<a id="n23"></a>
### N23 — Mapa je světlá v tmavé aplikaci a `Místa` jsou jeden řádek 🟡 🟡

**Co se stalo.** Tři věci na dvou stránkách:

1. **Mapa** používá světlé podkladové dlaždice (`Základní`, `Turistická`) uvnitř
   tmavého rozhraní. Přechod z tmavé stránky na svítící mapu je na velké ploše
   nepříjemný, na mobilu obzvlášť. Přepínače „Turistická" a „Letecká" mají navíc
   v neaktivním stavu nízký kontrast (tmavě modrá na tmavé).
2. **Pokrytí:** „Fotek na mapě: 2 378" z 20 906, tedy **11 %**. Stránka to nijak
   nevysvětluje ani nenabízí, co s tím. (Doplnění polohy přitom v projektu existuje
   — `internal/geoestimate`.)
3. **`/places`** má pro tuhle knihovnu **jediný řádek**: „Česko — 2 351 fotek".
   Teprve po kliknutí se objeví seznam obcí (setříděný podle počtu, breadcrumb
   funguje). Řádky nemají žádné náhledy — místa v galerii bez jediné fotky.

**Proč to vadí.** Mapa a Místa zabírají dvě ze čtyř položek v „Procházet". Za jednou
je hlavně prázdno (89 % knihovny na mapě není), za druhou je jeden řádek. Pro
uživatele je to dvakrát zklamání a zároveň dvě položky menu, které mu nic nedávají.

**Návrh.**
1. Použít **tmavou variantu podkladu** mapy, pokud ji poskytovatel dlaždic nabízí;
   jinak na světlé dlaždice položit jemný tmavý filtr. A zvýšit kontrast
   neaktivních přepínačů stylu.
2. Na `/places` dát ke každé zemi/obci **náhled** (nejlepší fotka místa) — je to
   fotogalerie.
3. Když má zem jen jednu položku, přeskočit úroveň a rovnou ukázat obce.
4. Na mapě zmínit pokrytí lidsky („Na mapě je 2 378 z 20 906 fotek — u ostatních
   není uložená poloha.") a nabídnout odkaz na doplnění polohy (pro role, které to
   smí).

**Kde to je.** `web/src/pages/MapPage.tsx`, `web/src/components/map/*`,
`web/src/pages/PlacesPage.tsx`, `internal/mapsapi` (volba stylu dlaždic).

---

<a id="n24"></a>
### N24 — Detail alba nemá časovou osu ani popis ⚪ ⚪

**Co se stalo.** Otevřel jsem album se **781 fotkami** a rozsahem **1910–2026**.
Stránka má nadpis, tlačítka Promítání a Stáhnout ZIP, filtrovací pole a mřížku.
Nemá **časovou osu** (knihovna ji má) a nemá ani **přepínač řazení**. Pole
`description` v odpovědi API existuje, ale na stránce alba se nikde nevypisuje —
ani na přehledu alb, kde je pod názvem jen rozsah let a počet fotek.

**Proč to vadí.** Album se 116letým rozsahem se prochází stejně těžko jako celá
knihovna, jen bez nástroje, který na to knihovna má. A popis alba — často jediné
místo, kde je napsáno, co album vlastně je — se uživateli nikdy neukáže.

**Návrh.** Ukázat `description` pod nadpisem. Použít stejnou `TimelineScrubber`,
kterou má knihovna, když album pokrývá víc než řekněme dva roky. Přidat přepínač
řazení (nejstarší/nejnovější).

**Kde to je.** `web/src/pages/AlbumDetailPage.tsx`.

---

<a id="n25"></a>
### N25 — Prázdné hledání hlásí „Počet fotek: 0" ⚪ ⚪

**Co se stalo.** Po otevření `/search` bez dotazu je uprostřed stránky správná
výzva „Zadejte hledaný výraz." — ale nad ní stojí **„Počet fotek: 0"**.

**Proč to vadí.** Dvě protichůdná sdělení vedle sebe. „Počet fotek: 0" na první
návštěvě hledání čte část lidí jako „v knihovně nic není".

**Návrh.** Počet nevykreslovat, dokud dotaz není zadaný.

**Kde to je.** `web/src/pages/SearchPage.tsx`.

---

<a id="n26"></a>
### N26 — Technické údaje ukazují SHA256, PhotoPrism UID a souřadnice ⚪ ⚪

**Co se stalo.** Rozbalené „Technické údaje" u fotky obsahují vedle užitečných věcí
(rozměry, velikost, formát, datum) také:

- „Formát: JPEG" **a zároveň** „Kodek obrazu: jpeg" — dvakrát totéž,
- „Otisk (SHA256): 414a4fc8c2b2…",
- sekci **PŮVOD** s řádkem „PhotoPrism UID: pt8qxy9y3nuwqef2",
- v panelu Informace pod mapkou surové souřadnice „49.39322, 16.70869".

**Proč to vadí.** Nic z toho uživatel nepoužije a všechno to zvyšuje pocit, že je
aplikace pro techniky. `UX_AUDIT.md` má „raw file paths / UIDs" jako nález na
administrátorských stránkách; tady je to na detailu každé fotky.

**Návrh.** „Kodek obrazu" sloučit s „Formát". Otisk a PhotoPrism UID schovat
za rozbalovací „Pro vývojáře" (nebo je odstranit spolu s odstraněním importu —
ten úkol je už ve frontě). Souřadnice nahradit názvem místa a číslo nechat jen
jako titulek při najetí.

**Kde to je.** `web/src/components/photo/TechnicalDetails.tsx`,
`web/src/components/photo/PhotoLocation.tsx`.

---

## Co naopak funguje dobře

Aby byl obrázek úplný — tohle jsem při používání ocenil a nemá smysl na to sahat:

- **Časová osa vpravo** v knihovně. Segmenty mají pojmenované rozsahy
  („Přejít na čvc 1965 – led 1967"), reagují na filtr, a ve sbírce sahající
  do roku 1905 je to jediný použitelný způsob pohybu. Škoda, že chybí na
  mobilu ([N19](#n19)).
- **Dotazovací jazyk sám o sobě.** Nápověda je přehledná, syntaxe rozumná
  (rozsahy, negace, `|`, uvozovky, hvězdička), diakritika se ignoruje správně.
  Chyba je jen v tom, že se o něm nikdo nedozví ([N2](#n2), [N3](#n3)).
- **Přehledové hledání pod lupou (`/` nebo Ctrl+K).** Seskupené výsledky s náhledy
  a daty, nápověda ke klávesám dole, „Hledat X mezi všemi fotkami" jako první řádek.
- **Mobilní zásuvka menu.** Rozdělená do sekcí HLAVNÍ / PROCHÁZET / ÚČET, velké
  cíle, ikony. Jediná výhrada je „Klávesové zkratky" na telefonu.
- **Prázdné stavy.** „Nenalezeny žádné fotky" vypíše aktivní filtry a nabídne
  „Zrušit všechny filtry"; „Zatím žádná uložená hledání" vysvětlí, k čemu jsou.
- **`/help`** je věcná, česká a dobře členěná (výhrady v [N17](#n17)).
- **Formátování čísel a jednotek** — „20 906", „531,4 KB", „1,7 Mpx", „1,65 : 1",
  odznáček „cca" u nejistého data.
- **Dotykové cíle.** V nasazeném CSS je sedm pravidel `@media (pointer: coarse)`;
  globální podlaha ze `UX_AUDIT.md` je opravdu v produkci.
- **Osobní oblíbené a hodnocení** fungují i pro roli prohlížeče, což je správně.

---

## Vztah k `UX_AUDIT.md`

Nálezy se nepřekrývají, ale na třech místech se doplňují:

- **Žargon.** `UX_AUDIT.md` má inventář žargonu a bod #4 o vysvětlivkách na
  administrátorských stránkách. [N22](#n22) ukazuje, že „Embeddingy" prosakují i na
  `/stats`, což je stránka pro všechny; [N12](#n12) přidává `AI_MODEL:` a `Unknown`
  na detail fotky.
- **Režimy hledání.** Bod #1 v jeho zásobníku (přejmenovat „Hybridní/Fulltext/
  Sémantické") pořád platí. [N1](#n1) ukazuje, že u téhož přepínače je ještě
  závažnější věc než pojmenování.
- **Dotykové cíle.** Globální podlaha `pointer: coarse` je nasazená a funguje;
  mobilní nálezy tady ([N4](#n4), [N6](#n6), [N16](#n16), [N19](#n19)) jsou o
  rozvržení a informační architektuře, ne o velikosti tlačítek.
