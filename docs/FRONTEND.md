# Frontend

Popisný referenční přehled frontendu (`web/`). **Nejsou to pravidla** — pravidla
jsou v [`CLAUDE.md`](../CLAUDE.md). Novou komponentu, hook, stránku nebo službu
zapiš sem.

<!-- BODY BEGIN -->
- **Frontend layout:** `web/` (Vite + React 19 + TS): `web/src/` s `components/`
  (`Layout` = navbar shell s user-menu/logout + role-gated nav, **vyvážená kolem toho, jak se
  knihovna reálně prochází — po albu, po štítku, po roce**: **Knihovna** `/` (= úvodní stránka;
  `NavLink` má `end`, jinak by se rozsvítila na každé routě),
  **Alba** `/albums` a **Štítky** `/labels` jsou vždy viditelné top-level položky
  (registr `PRIMARY_ITEMS`); hned za nimi **Rozšířit** `/expand` (`EXPAND_ITEM`, gate `canWrite`) —
  sedí u alb/štítků, protože přesně ty rozšiřuje; zbylé browse cíle sdružuje dropdown **Procházet** (`nav.browse`,
  `BROWSE_GROUP`): **Oblíbené** `/favorites`, **Lidé** `/people`, **Místa** `/places`, **Mapa**
  `/map`; **Třídění** `/review` (`REVIEW_ITEM`, gate `canWrite`) je top-level, ne v „Nástrojích" —
  uklízení knihovny po jedné otázce je nejpoužívanější kurátorská smyčka a hra, kterou nikdo
  nenajde, je hra, kterou nikdo nehraje; **Nahrát** `/upload` je top-level (gate `canWrite`); editorský dropdown **Nástroje**
  (`nav.tools`, `TOOLS_GROUP`, celý gate `canWrite`) sdružuje **Najít osobu** `/faces` +
  **Rozpoznávání** `/recognition` + **Možné chyby** `/outliers` (u ostatních lidských nástrojů,
  protože je to taky práce s obličeji) + **Duplikáty** `/duplicates` + **Koš**
  `/trash`; **Import** `/import` (`IMPORT_ITEM`) je top-level s gate `canImport` (admin **nebo** ai);
  adminský dropdown **Správa** (`nav.admin`, `ADMIN_GROUP`, celý gate `isAdmin`) sdružuje
  **Údržba** `/maintenance` + **Systém** `/system` + **Uživatelé** `/users` + **Audit** `/audit`.
  **V navbaru není hledání**
  (ani odkaz, ani živé pole ani uložená hledání) — hledá se z knihovny a ze stránky `/search`.
  Každá položka i každý dropdown toggle nese **ikonu** (`Icon`) a **`title` popisující akci**, ne
  podstatné jméno („Zobrazit alba", ne „Alba"; klíče `nav.titles.*`); ikony jsou dekorativní
  (`aria-hidden`) vedle viditelného textového labelu. Dropdown se skryje celý, když má uživatel
  skryté všechny jeho položky (Tools/Admin u viewera); rodičovské menu má **active stav** (`active`
  prop), když je aktuální route některé z jeho dětí (`pathMatches` ctí i detail sub-cesty jako
  `/albums/{uid}`) — skládá se z `Dropdown`+`Dropdown.Toggle as={NavLink}` (ne `NavDropdown`, ten
  spotřebuje prop `title` na obsah toggle, takže by nezbyl na tooltip); položky v mobilním burger
  menu expandují inline s tap-targety (`kukatko-tap-target`),
  `Footer` (**globální patička** pod `<main>` na každé stránce v `Layout` — fullscreen
  `/slideshow` běží mimo shell, takže ji nemá: „Provozuje SDH Veselice“ + odkaz na zdrojový kód
  <https://github.com/panbotka/kukatko> v novém tabu s `rel="noopener noreferrer"` a dekorativní
  ikonou `github` (`aria-hidden`); texty `footer.*` (cs/en). Rendruje se v normálním toku — na
  krátké stránce prostě následuje obsah, nic nepřekrývá ani nefloatuje. Uvnitř je space-between
  flex řádek: operátor + GitHub vlevo, pravou stranu vyplňuje `children` (dnes admin badge stav
  fronty jobů); `.kukatko-footer` sdílí safe-area padding s `.kukatko-main`),
  `JobQueueBadges` (pravá strana patičky: kompaktní badge se stavem fronty jobů **jen pro adminy**;
  přes `useAuth().isAdmin` + `useJobStats` — neadmin nic nerendruje a **nedělá žádný request**.
  Jeden badge na neprázdný stav `queued`/`running`/`failed`/`dead` z `by_state` (terminální `done`
  se záměrně vynechává), `failed`/`dead` mají `bg="danger"`, aby padly do oka; když je vše nulové,
  jediný tichý badge `idle`. Selhání requestu badge tiše skryje — patička nikdy nespadne; texty
  `footer.jobs.*` (cs/en)),
  `JobStateLegend` (**sdílená legenda stavů fronty jobů**: kompaktní `dl` s tučným termem + tichým
  jednovětým vysvětlením každého stavu, aby admin rozuměl bez najetí myší; popisky i vysvětlení ze
  sdíleného i18n bloku `jobStates.labels.*`/`jobStates.descriptions.*`, takže znění je totožné na
  `MaintenancePage` i `SystemStatusPage`; prop `states` řídí pořadí a výběr — Maintenance vynechává
  `pending`, System ho přidává. Testy: `JobStateLegend.test.tsx`),
  `Icon` (**jediná ikonová sada** aplikace: bootstrap-icons glyf jako `<i class="bi bi-{name}">`,
  font se importuje globálně v `main.tsx`; union `IconName` drží slovník použitých ikon, takže překlep
  je chyba překladu; vždy `aria-hidden` vedle viditelného labelu),
  `BackLink` (**sdílená cesta zpět** ze všech detailů (album, štítek, osoba, fotka) do seznamu,
  ke kterému patří: šipka `arrow-left` přes `Icon` (dekorativní, `aria-hidden`) + **text pojmenující
  cíl** („Zpět na alba" / „Zpět na štítky" / „Zpět na lidi"), který je zároveň přístupným jménem
  odkazu — holá šipka nikomu neřekla, kam vede. Props `to` (celý href cíle **včetně query**, takže
  stav seznamu — filtry/řazení/stránka — přežije návrat a **Zpět vždy funguje**; `PhotoDetailPage`
  ho staví přes `backHref(view)`), `label` (už přeložený volajícím), `className?`. Rendruje router
  `<Link>` — fokusovatelný z klávesnice, focus-ring + podtržení na hover přes `.kk-back-link`
  (šipka se na hover nakloní k cíli, `prefers-reduced-motion` pohyb vypne), na coarse pointeru 44px
  tap target; použitý i v error alertu těchže stránek. Testy: `BackLink.test.tsx`),
  `LanguageSwitcher` (button group cs/en, `aria-pressed` na aktivní; **nesedí v navbaru** —
  bydlí v sekci Jazyk na `AccountPage`, protože tuhle instanci používají jen Češi a trvalé
  místo v liště by bylo plýtvání. Volbu persistuje i18next language detector do localStorage),
  `MultiSelect` (**sdílený vyhledávatelný multi-select** pro kolekce, které rostou bez omezení —
  alba a štítky: psaní zúží nabídku **case- i diakritika-insensitive** přes `lib/text`
  `foldedIncludes`, každá volba se **přidá** (nenahradí), vybraná položka **zmizí ze seznamu**
  a objeví se pod polem jako odebíratelný chip (`.kk-chip`), takže dlouhý seznam zůstává krátký
  a výběr čitelný bez sloupce s fajfkami. Klávesnice Up/Down/Enter (bez zvýraznění bere nejlepší
  shodu), **Backspace nad prázdným dotazem odebere poslední chip**, Esc zavře; combobox/listbox
  ARIA (`aria-multiselectable`), strop `MAX_SUGGESTIONS` (50) rendrovaných návrhů, ~44px tap
  targety. Prop `destructive` obarví label i chipy do danger klíče, aby odebrání nikdy nevypadalo
  jako přidání. Defaultně **nezakládá položky** — jen vybírá z těch, které dostane (zrcadlí
  `AddAutocomplete` a `SearchableSelect`); s volitelným `onCreate(name)` přidá na konec seznamu
  řádek **„Vytvořit «dotaz»“**, jen když neprázdný trimovaný dotaz fold-insensitive (case,
  diakritika, okrajové mezery) neodpovídá **žádné** option — vybrané včetně — takže nikdy
  nenabídne duplikát; Enter bez zvýraznění vytváří, jen když nic jiného neodpovídá. Co založení
  znamená, řeší volající (typicky zaregistruje jméno a vybere pro něj hodnotu přes
  `options`+`selected`); pro čtenáře bez práva zápisu se `onCreate` prostě nepředá),
  `photo/PlaceSearch` (**našeptávač míst podle názvu** = třetí cesta k poloze fotky vedle
  souřadnic a kliku do mapy — u naskenované fotky víš *Veselí nad Moravou*, ne čísla, a hledat
  ten bod pananím mapy je otrava. `{id,onPick,disabled?}`, `onPick(place)` dostane `Place` a
  volající si sám rozhodne, kam souřadnice zapíše: `MetadataPanel` je píše do svého pole
  souřadnic (marker i mapa se překreslí samy), `BulkEditModal` do `lat`/`lng` u `set_location`.
  Každý řádek nese **název + druh místa (`label`) + `location`** — rozlišení je celý smysl (Veselí
  je město, zámek i část obce, tři stejně vypadající řádky by byly k ničemu). Psaní jede přes
  `usePlaceSearch` (debounce + rušení in-flight); pole drží **dvě** stavové hodnoty — co je vidět
  (`query`) a co se hledá (`term`) — aby vybrání návrhu nechalo jméno v poli jako potvrzení, ale
  hned ho znovu nehledalo. Klávesnice Up/Down/Enter (bez zvýraznění bere nejlepší shodu)/Esc,
  combobox/listbox ARIA, ~44px tap targety — je to formulářový prvek a chová se tak. Nedostupné
  vyhledávání (bez klíče, poskytovatel dole) = **jeden řádek textu**, zbytek editoru polohy jede
  dál. Testy: `PlaceSearch.test.tsx`),
  `KeyboardShortcutsHelp` (v navbaru: ikonka klávesnice + **modal nápovědy zkratek** — otevře se
  `?` (Shift+/) kdekoli nebo klikem, vypíše všechny zkratky seskupené dle kontextu (Mřížka / Detail)
  ze `lib/shortcuts.ts` `SHORTCUT_GROUPS`, zavře Escapem/křížkem),
  `EmptyState` (**sdílený placeholder prázdné kolekce**: ikona v kulaté jámě, krátký titulek,
  jednořádkový hint a volitelné akční tlačítko, vycentrované v prostoru, který by kolekce zabrala.
  Props `title` (povinné), `hint?`, `icon?` (default = obrys prázdného rámečku, `aria-hidden`),
  `action?` (obvykle stejné tlačítko, které nabízí naplněný pohled), `size?` `'md' | 'sm'`
  (kompaktní varianta pro dlaždici/úzký panel), `className?`. Titulky/hinty si **překládá volající**
  (každá stránka má vlastní i18n klíč, aby copy byla konkrétní). Nahradil holý jednořádek
  „Bez náhledu" i všechny ručně skládané `text-center py-5` bloky napříč
  stránkami (`LibraryPage`, `SearchPage`, `AlbumsPage`, `AlbumDetailPage`, `LabelsPage`,
  `LabelDetailPage`, `PeoplePage`, `SubjectPage`, `PlacesPage`, `MapPage`, `FavoritesPage`,
  `SavedSearchesPage`, `ClustersPage`, `FacesPage`, `ExpandPage`, `DuplicatesPage`, `TrashPage`, `SlideshowPage` (s akcí
  „Zpět"), `ImportPage`) i v komponentách (`AlbumTile`/`SubjectTile` cover placeholder,
  `Outliers`). **Ne každá prázdnota si ho zaslouží:** v hustém panelu, kde pod sebou sedí
  několik krátkých seznamů (`OrganizePanel` — alba a štítky), by placeholder přerostl chipy,
  které zastupuje, a panel by poskakoval, jak se jeden seznam plní a druhý zůstává prázdný —
  tam zůstává tlumený jednořádkový popisek (`text-secondary small`). Bloky se objeví přes
  `.kk-appear`, které `prefers-reduced-motion` vypne. Testy: `EmptyState.test.tsx`),
  `ConfirmModal` (**jediný sdílený potvrzovací dialog** — nahradil nativní `window.confirm`
  na čtyřech místech: `AlbumDetailPage` (smazání alba), `LabelsPage` (smazání štítku),
  `SavedSearchesPage` (smazání uloženého hledání) a `ImportPage` (potvrzení prvního běhu importu).
  Podle vzoru stylovaného modálu na `TrashPage` — jediný pattern místo grey OS dialogu: **potvrzovací
  tlačítko nese samotnou akci** („Smazat album" / „Spustit import"), nikdy „OK", a čte se stejně jako
  ovládací prvek, který dialog otevřel — akce si drží jedno jméno v celém toku. Props `show`, `title`
  (krátká otázka), `children` (důsledek — co a s čím se stane), `confirmLabel`, `cancelLabel?` (default
  sdílené `confirmModal.cancel`), `variant?` `'danger' | 'primary'` (default `danger` obarví potvrzení
  červeně; nedestruktivní `primary`), `busy?` (zamkne obě tlačítka i křížek/backdrop po dobu běhu
  akce), `onConfirm`, `onCancel`. **Destruktivní tlačítko není default Enteru**: po otevření sedí fokus
  na Zrušit, takže náhodný Enter ruší, ne maže; Escape/křížek/backdrop ruší; react-bootstrap vrátí
  fokus na spouštěč. Copy překládá volající — žádné napevno psané řetězce. Testy: `ConfirmModal.test.tsx`);
  `components/upload/` = `DropZone` (drag-and-drop zóna + file input `multiple`
  `accept="image/*,video/*"` → mobilní galerie + tlačítko **Vyfotit** `capture="environment"`),
  `UploadProgressHeader` (**prominentní sticky** hlavička celé dávky: „done / total“, **jeden**
  overall progress-bar vážený i částečným `progress` běžících souborů — `barLabel` pro a11y —,
  živý rozpad počtů uploaded/duplicate/failed/remaining; po dokončení přepne na **completed
  summary** s odkazem do knihovny a jednoklikovým retry-failed), `UploadItem` (řádek fronty jako
  samostatná `kk-surface` karta: jméno+velikost, progress-bar, status badge, near-duplicate
  varování, remove/retry akce; chybný řádek má `border-danger`), `UploadList` (**virtualizovaný**
  `Virtuoso useWindowScroll` seznam řádků, mezery přes `pb-2`, aby 100+ souborů zůstalo svižné na
  mobilu), `UploadOrganize` (dva vyhledávatelné `MultiSelect` pro **alba**
  a **štítky** platné pro celou dávku, s inline vytvořením nové položky přes `onCreate`; prázdné
  by default, řízené `useUploadOrganize`); `components/library/` = `PhotoTile`
  (čtvercová lazy-load dlaždice → `/photos/{uid}`, badge soukromé, **stack badge** (počet členů
  skupiny vpravo nahoře — ikona `images` + `stack_count`, `library.tile.stackCount`, jen když
  `stack_count > 1`), **play badge + délka** u
  videa/live fotky (`▶` + `formatDuration`), placeholder bez
  layout-shiftu; volitelný **favorite heart** overlay `favoritable` → `FavoriteButton`
  (hodnocení hvězdami a pick/reject flag žijí **jen v detailu fotky**, ne na dlaždici);
  heart se v selection módu skryje; `src` bere **`photo.thumb_url` z payloadu** přes `useThumbSrc` a
  **nikdy** ho neskládá z UID),
  `PhotoGrid` (virtualizovaný **`react-virtuoso` `VirtuosoGrid`**,
  window-scroll, `endReached` → další stránka, footer spinner/retry; prop `favoritable`
  prosákne srdíčko na dlaždice; volitelný `gridRef` (imperativní `scrollToIndex`
  handle) + `onRangeChanged` (viditelný rozsah) pro časovou osu; šablonu sloupců bere z
  `useGridDensity` → `lib/gridDensity` `gridTemplateColumns`, DOM nese `data-density` pro testy.
  Změna hustoty **jen přestyluje** existující `<div>` — virtuoso doměří dlaždice, scroll i výběr
  přežijí, protože se mřížka nekeyuje ani neremountuje),
  `TimelineScrubber` (**časová osa** — tenká fixní svislá datová lišta u mřížky: fetchne měsíční
  histogram přes `useTimeline(params)` (refetch při změně filtrů), každý měsíc = klikací tick
  umístěný proporčně dle `cumulative/total`, měsíční popisky přes `lib/format` `formatMonth`;
  klik/tažení skočí na měsíc přes `onJump(bucket.cumulative)`, aktivní měsíc se zvýrazní dle
  `activeIndex` (start viditelného rozsahu); overlay `position: fixed`, takže loading/prázdný
  timeline nerendruje nic a neposouvá layout, na malých šířkách se skryje přes `styles/app.css`
  `.kukatko-timeline*`; jen pro výchozí newest řazení), `FilterBar`
  (**redesign pro klidný výchozí stav + progresivní odhalení**: v hlavičce jen prominentní
  vyhledávací pole (vizuální kotva, největší prvek), řazení (vč. **dle hodnocení**),
  `GridDensityControl` a tlačítko
  **Filtry** s odznakem počtu aktivních filtrů; pokročilé filtry (datum od/do, poloha, soukromé,
  fotoaparát, archiv, **min. hodnocení ≥1…≥5**, **flag vybrané/zamítnuté**) žijí v rozbalovacím
  panelu — na desktopu inline `Collapse`, na mobilu `Offcanvas` dle `matchMedia` (sdílený hook `useIsNarrowViewport`,
  defenzivní k jsdom, kde `matchMedia` vrací `undefined`); každý aktivní filtr = odebíratelný
  **chip** (`buildChips`, pill s křížkem, zruší jen ten filtr — dotaz `q` chip
  nemá, má vlastní pole; **album a štítek chip nesou barvu entity** — `.kk-entity-album`
  vs. `.kk-entity-tag` + vodicí ikona z `ENTITY_STYLE`, takže album a štítek jsou na první pohled
  odlišné (viz *barvy entit* v `tokens.css`); ostatní filtry zůstávají neutrální `text-bg-primary`)
  + jedno **„zrušit filtry"** + počet fotek; **beze změny chování** — vše
  jede přes `viewToParams`/`useUrlState`/`LibraryView`, dotaz replacuje historii, ostatní pushují;
  generický nad `LibraryView`+supersetem, props `showSearch`/`showSort` skryjí dotaz/řazení
  na search stránce, `showDensity` skryje hustotu v koši (kartová, ne foto-mřížka),
  **`showFavorite`** zapne v panelu přepínač **Oblíbené** (dvoustavový select „Vše"/„Jen oblíbené"
  → `view.favorite` `''`/`'true'`, backend scopuje jen na `true`; knihovna ho zapíná, aby šlo
  kombinovat „oblíbené + album + rok" v hlavní mřížce, stránka Oblíbené ne — už je scopnutá)
  (chipy/panel/zrušit fungují dál); tap-targety ~44 px přes `styles/app.css`
  `.kukatko-filter-*`;
  **čtyři facety, kterými se fotky reálně hledají** (prop `facets` z `useLibraryFacets`) jsou
  **vždy viditelné** pod hlavičkou, ne schované v panelu: **Rok** = prostý `<select>`
  („Libovolný rok" + `{{year}} ({{n}})` z `GET /photos/years`, katalog má vždy jen hrstku let),
  **Album**, **Štítek** a **Osoba** = `SearchableSelect` (všechny kolekce rostou bez omezení;
  osoby z `GET /subjects` s `marker_count`), **multi-výběr**: každá volba se **přidá** k aktuální
  sadě (AND — fotka musí být ve všech vybraných albech, nést všechny štítky a obsahovat všechny
  vybrané osoby), select je čistý „add-picker" (drží se placeholderu „libovolné", vybrané
  položky ze svých options vypustí, aby nešly přidat dvakrát), už vybraná alba/štítky/osoby visí jako
  odebratelné chipy (jeden na UID) níž.
  Inline pole **„filtrovat dle názvu/popisu"** (`q`) zůstává rychlým zúžením mřížky; vedle něj
  **zřetelný odkaz na `/search`** pro skutečný fulltext + sémantické hledání (`searchHref` nese
  aktuální `q`) — režimy hledání se tu **nezdvojují**), `SearchableSelect`
  (`components/library/`, jednovýběrový facet, do kterého se dá psát: v klidu ukazuje volbu,
  focus otevře celý seznam, psaní ho zúží **case- i diakritika-insensitive** přes `lib/text`
  `foldedIncludes` (`namesti` najde `Náměstí`, stejně jako backendový `immutable_unaccent`);
  vedoucí řádek „libovolné" facet zruší, klávesnice Up/Down/Enter/Esc, combobox/listbox ARIA,
  strop `MAX_SUGGESTIONS` (50) rendrovaných návrhů; nikdy nevytváří položky —
  zrcadlí `AddAutocomplete`), `filterChips.ts` (pure `buildChips(view, t, {facets?, includeQuery?})`
  → `FilterChip{key,label,clear,kind?}` pro každý aktivní filtr; **jeden chip na každé vybrané album,
  štítek a osobu** (`clear` odebere jen svoje UID ze seznamu, poslední chip facet vyčistí; album chip má
  `kind:'album'`, štítek `kind:'tag'`, osoba `kind:'person'` → `FilterBar` z toho vezme barvu + ikonu přes
  `ENTITY_STYLE`; **oblíbené** = neutrální chip bez `kind`); `facets`
  pojmenují album/štítek/osobu titulkem místo UID (chybějící → raw UID, chip nikdy není prázdný),
  `includeQuery` zapíná chip pro `q`
  — filter bar ho vypíná (má vlastní pole), **prázdný stav zapíná** (čtenář u nuly výsledků musí
  vidět všechny filtry, které ho tam dostaly); délka pole = počet aktivních filtrů na odznaku),
  `SimilarPhotos` (znovupoužitelný horizontálně scrollovatelný pruh
  podobných fotek nad `GET /photos/{uid}/similar` přes `fetchSimilar`, odkazy na detail,
  empty-friendly + loading/error, refetch při změně `uid`),
  `FavoriteButton` (heart toggle nad `useFavorite` — **optimistický** per-user favorite
  s rollbackem; bez role-gate, smí každý přihlášený; jako overlay na dlaždici je sibling
  linku, takže klik nenaviguje), `RatingStars` (pure controlled 0–5 hvězd; klik na aktuální
  hodnocení maže na 0; bez `onRate` read-only display) + `FlagControl` (pure controlled per-user
  **osobní označení** — tři neutrální stavy přes `Icon` bootstrap-icons: 👁 eye (`text-info`),
  👍 thumbs-up (stored `pick`, `text-success`), 👎 thumbs-down (stored `reject`, `text-danger`);
  klik na aktivní stav maže na `none`; bez `onFlag` read-only; sibling linku → klik nenaviguje),
  `GridSkeleton` (placeholder mřížka při prvním načtení; zrcadlí i zvolenou hustotu, takže po
  načtení fotek nenaskočí layout),
  `GridDensityControl` (kompaktní zoom stepper **Dlaždic na řádek**: `−` / prostřední čip / `+`;
  `−` krokuje k **jedné fotce na řádek** (méně, větší dlaždice) až na podlahu 1, `+` připne víc
  sloupců až po 8, prostřední čip ukazuje stav — `A`, nebo počet sloupců (i `1`) — a kliknutím
  resetuje na `Automaticky`; krokuje po žebříčku `stepDensity` (podlaha 1…8; z `auto` vstoupí na
  nejmenší **vícesloupcové** rozvržení 2, ne na 1 sloupec); ikony přes `Icon`
  (`dash-lg`/`grid-3x3-gap-fill`/`plus-lg`), `−` je disabled na `auto` i na 1 (jedna fotka na
  řádek), `+` na 8; čte/píše `useGridDensity`, tedy localStorage, **ne URL** — je to preference
  zařízení, ne součást sdíleného pohledu; sedí v hlavičce `FilterBar`u, mění všechny
  foto-mřížky v appce najednou); `PhotoTile`+`PhotoGrid` podporují
  volitelný **selection mód** (props `selectable`/`selected`/`onToggleSelect`, resp. `selection`;
  heart se v selection módu skryje); **Shift+klik vybere souvislý rozsah**: `onToggleSelect` nese
  `shiftKey` kliknutí, `PhotoGrid` ho s vlastním pořadím fotek přesměruje na volitelný
  `selection.onToggleRange(uid, orderedUids)` (bez něj zůstává plain toggle) — kotvu drží
  `useSelection`, takže rozsah funguje v každé mřížce bez wiringu na stránce; `PhotoTile` má
  volitelný slot **`extras`** (resp. `PhotoGrid` prop `tileExtras(photo)`) pro overlaye stránky —
  badge/akce jako **sibling** linku/tlačítka v relative wrapperu (interaktivní extra nenaviguje,
  netoggluje; badge s `pe-none` nekrade klik) — používá `/expand` pro % podobnosti a ✗;
  dlaždice **žádné datum nezobrazuje** — jediné, které nese, je
  v `alt` textu, a i tam se **odhadované** datum značí (`cca 1950`), aby ho nešlo číst jako jisté;
  řazení mřížky/timeline se nemění, dál je to `taken_at`,
  `components/organize/` = `AlbumTile` (karta alba: **efektivní obálka** `cover_uid`
  (ručně zvolená, jinak nejnovější fotka alba — počítá backend) / název / **rozsah let**
  přes `formatCaptureRange` (jen když album má datované fotky) / počet → `/albums/{uid}`;
  `EmptyState` až pro album bez fotek),
  `AlbumEditModal` (create/rename alba: název/popis/soukromé), `LabelEditModal` (create/rename
  štítku: jméno/priorita), `SelectionBar` (sticky toolbar výběru: počet +
  akce + zrušit), `BulkEditControl` (**znovupoužitelný spouštěč** hromadné úpravy: tlačítko
  (`selection.edit`) + `BulkEditModal`, řízené výhradně výsledkem `useBulkEdit`; **viewerovi se
  nevykreslí vůbec**, při nulovém výběru je disabled — stačí ho vložit do `SelectionBar`, stránka
  nedrží žádný stav dialogu; volitelný prop `prefill` protéká do modalu), `SelectionStart` (**protějšek** `BulkEditControl`: tlačítko
  `selection.enter`, které zapne režim výběru; viewerovi ani už zapnutému výběru se nevykreslí,
  `onEnter` přebije akci pro stránku, která musí nejdřív opustit jiný režim),
  `DownloadZipButton` (**stažení výběru nebo celého alba jako ZIP** originálů: volá
  `downloadPhotosZip`, po dobu streamu ukazuje spinner a při selhání chybu — 413 = přes strop
  (`download.zipTooMany`), jinak obecná (`download.zipError`); `photoUids` = aktuální výběr,
  `albumUid` (+ `name` = titul alba) = celé album; **dostupné i viewerovi** (stažení není zápis),
  disabled, když není co stáhnout. Vkládá se do `SelectionBar` knihovny a do hlavičky alba),
  `StackSelectedControl` (**NOVÝ**: tlačítko **Seskupit vybrané** (`selection.stack`) v selection baru
  knihovny (`LibraryPage`), **jen editor/admin**, disabled dokud nejsou vybrané **≥ 2** fotky; volá
  `stackPhotos`, po úspěchu vyčistí výběr a znovunačte mřížku),
  `BulkEditModal` (**hromadná úprava** výběru přes `POST /photos/bulk`, celá dávka
  jednou transakcí na backendu; formulář je rozdělený na **čtyři sekce** (`.kk-text-eyebrow`
  nadpisy): **Zařazení** (add/remove alb, add/remove štítků — čtyři `MultiSelect`y, takže jeden
  apply zvládne **víc alb i víc štítků najednou**; add pole navíc přes `onCreate` nabízejí
  **„Vytvořit «název»“** pro jméno, které fold-insensitive nic existujícího nenese — jen pro
  uživatele s právem zápisu (`useAuth().canWrite`). Nová položka se okamžitě objeví jako chip
  (hodnota `create:<název>`, `CREATE_PREFIX` — dvojtečka se v base32 UID nevyskytuje; sdílené
  helpery `pendingValue`/`pendingName`/`pendingOptions` žijí v `lib/pendingCreate` a používá je
  i `useUploadOrganize`) a **založí
  se až při Apply**: nejdřív `POST /albums`/`POST /labels` (defaulty: prázdný popis, neprivátní;
  priorita 0), čerstvé UID se vymění do formuláře i options — retry tedy nezaloží duplikát — a
  teprve pak jde dávka; zrušený dialog nezaloží nic. Neúspěch založení vypíše hlášku serveru
  (`bulkEdit.createError`) a dávku neodešle, výběr zůstává; když se dávka nepovede až po založení,
  `bulkEdit.createdButApplyFailed` řekne, že položky už existují a selhalo jen přiřazení),
  **Metadata** (set/clear popisu), **Poloha**
  (set/clear souřadnic; nad poli `lat`/`lng` sedí u `set` **tentýž `PlaceSearch`** jako v editoru
  detailu — vyplní jen ty dvě pole, takže odeslaná dávka je stejná, jako by souřadnice někdo
  napsal ručně) a **Příznaky** (soukromé, archiv, oblíbené); set/clear páry zůstávají
  samostatné módy. **Destruktivní volby** (odebrání z alba/štítku, archivace) jsou v danger klíči
  (`destructive` chipy, `text-danger` label, `border-danger` select). Pod formulářem je
  **`PendingChanges`** — `.kk-surface` panel, který větou po větě říká, co apply udělá, a **kolik
  fotek to zasáhne** (destruktivní řádky červeně + `visually-hidden` „(destruktivní)"; `aria-live`).
  Výběr **nad `LARGE_SELECTION` (50) fotek** vyžaduje **explicitní potvrzení**: první Apply jen
  otevře danger alert („Ano, použít na N fotek" / „Zpět"), a **jakákoli změna formuláře potvrzení
  odvolá**. Klientská validace souřadnic + „aspoň jedna změna" zůstává; po aplikaci
  **per-foto result summary** z odpovědi. Neúspěšný request **vypíše hlášku serveru**
  (`ApiError.message` — konfliktní operace, příliš velká dávka), jinak generický `bulkEdit.applyError`;
  výběr zůstává nedotčený, ať se dá apply zopakovat. Volitelný prop **`prefill`**
  (`BulkEditPrefill{addAlbums?,addLabels?}`, memoizovaný — nová reference by resetla formulář)
  předvyplní add pole při každém otevření (`/expand` tam dá rozšiřovanou sbírku); `onDone` dostává
  **`BulkEditOutcome{operations,result}`** — co apply skutečně poslal a per-foto výsledky — takže
  stránka může seznam upravit na místě, místo refetche),
  `pages/` (`LoginPage`, `AccountPage` = identita/role, **sekce Jazyk** (`LanguageSwitcher` +
  hint, `account.language*`) a změna vlastního hesla, **plus technický stav aplikace**
  (`GET /healthz` badge + verze, bez commit hashe) v malém ztlumeném řádku dole — status i jazyk
  sem přišly odjinud (z úvodní stránky, resp. z navbaru): patří tam, kde je uživatel hledá, ne
  před fotky ani do prime místa v liště,
  `LibraryPage` = hlavní foto-knihovna **a zároveň úvodní stránka aplikace** (routa `/`):
  `FilterBar` nad virtualizovanou nekonečně-scrollující
  mřížkou, loading/empty/error stavy, celý pohled (filtry+řazení) v URL, srdíčka
  na dlaždicích (favoritable; hodnocení a pick/reject jsou jen v detailu fotky), **`SlideshowStart`**
  (tlačítko Promítání + odhad délky, počet fotek bere z `total`),
  **dva různé prázdné stavy** — s aktivními filtry „Nenalezeny žádné fotky", jehož hint
  **vyjmenuje aktivní filtry** (`buildChips(..., {facets, includeQuery: true})` spojené ` · `,
  album/štítek titulkem, ne UID) a nabídne je jedním tlačítkem zrušit,
  bez filtrů „Zatím tu nejsou žádné fotky" s CTA na `/upload` (editor/admin; viewer dostane jen
  vysvětlující větu), rozlišené přes `hasActiveFilters(view)`,
  `LibraryRedirect` = shim pro vysloužilou routu `/library`: `<Navigate replace>` na `/` s doslova
  zachovaným `search`+`hash` (staré záložky a odkazy fungují, `replace` zabrání odskočení Zpět),
  plus **časová osa** (`TimelineScrubber`) vedle mřížky pro rychlé skoky na měsíc — mřížka
  vystaví `gridRef`+`onRangeChanged`, skok jede přes `useGridJump` (donačte stránky, když měsíc
  leží za načtenou částí), zobrazí se jen pro výchozí newest řazení a mimo režim výběru,
  plus pro editory **režim výběru** (`Vybrat`/`Vybrat vše`) → `useBulkEdit` + `BulkEditControl`
  (hromadná úprava metadat přes bulk API; po úspěchu se mřížka přenačte přes `reloadKey`),
  plus tlačítko **Uložit pohled** (`SaveSearchModal` →
  `createSavedSearch` s aktuálním view objektem jako `params`),
  `SavedSearchesPage` = `/saved` (jakýkoli přihlášený) „Moje uložená hledání": seznam uložených
  pohledů aktuálního uživatele, každý odkaz otevírá přesně obnovený pohled (`savedSearchHref`), plus
  přejmenování (`SaveSearchModal`) a **optimistické mazání** + empty state,
  `FavoritesPage` = `/favorites` oblíbené aktuálního uživatele: stejná mřížka/filtry jako knihovna
  scopnutá `favorite=true`, srdíčka pro odebrání z oblíbených na místě (favoritable),
  dlaždice nesou scope v detail odkazu (`detailQuery` s `favorite=true`) → Esc/Zpět/prev-next z fotky se vrací sem,
  pro editory **režim výběru** → `BulkEditControl`; hromadné odebrání z oblíbených fotku ze seznamu
  vyhodí (výběr se čistí **před** refetchem, takže v něm nezůstane fotka, která zmizela z mřížky),
  `AlbumsPage` = `/albums` mřížka karet alb + `Nové album` (editor/admin) — pořadí **od
  nejnovějšího alba** (dle nejnovější fotky, nedatovaná/prázdná na konci) **vynucuje backend**,
  stránka nepřeřazuje a nemá selektor řazení; po vytvoření alba **přenačte seznam**
  (`useReloadKey`) místo lokálního připojení na konec — kam nové album patří, ví jen server,
  `AlbumDetailPage` = `/albums/:uid` hlavička + tlačítko **Promítání** (všem) + editorské akce
  (upravit/smazat/vybrat) nad
  fotomřížkou scopnutou na album (`useScopedPhotos` + `FilterBar showSort={false}` + URL stav) —
  album je **vždy chronologické** (nejstarší první, vynucuje backend), takže stránka nemá selektor
  řazení ani ruční přeřazování; výběr → nastavit cover / **hromadná úprava**
  (`BulkEditControl`) / odebrat z alba (odebrání i úspěšná úprava **výběr vyprázdní**, ať v něm
  nezůstanou UID fotek, které z mřížky zmizely, a mřížku přenačtou přes `reloadKey`); dlaždice nesou
  scope alba v detail odkazu (`detailQuery` s `album=uid`) → Esc/Zpět/prev-next z fotky se vrací do alba;
  stránka buď prochází, nebo vybírá (`selection.active`),
  `LabelsPage` = `/labels` seznam štítků s počty + create/rename/delete (editor/admin),
  `LabelDetailPage` = `/labels/:uid` fotomřížka scopnutá na štítek (`useScopedPhotos` + `FilterBar` + URL);
  dlaždice nesou scope štítku v detail odkazu (`detailQuery` s `label=uid`) → Esc/Zpět/prev-next z fotky
  se vrací ke štítku; + tlačítko **Promítání** + pro editory **režim výběru** → `BulkEditControl` (po úspěchu refetch),
  `SearchPage` = sémantické/hybridní/fulltext hledání: prominentní debouncované (350 ms)
  vyhledávací pole + přepínač režimu (`q`+`mode` v URL), stejná virtualizovaná mřížka jako
  knihovna + sdílený `FilterBar` (bez dotazu/řazení), `degraded` → neblokující upozornění
  (sidecar offline), idle/loading/empty/error stavy; pole mluví **vyhledávacím jazykem**
  (`q` = volný text + `klíč:hodnota` filtry, gramatika v docs/API.md „Vyhledávací jazyk (q=)“;
  parsuje výhradně backend): vstup je `SearchQueryInput` (`components/search/`) — combobox
  s **autocomplete klíčů filtrů** (návrhy ze `lib/queryLanguage.ts` `suggestFilterKeys`/
  `applyFilterKey` + `FILTER_KEYS`; šipky + Enter/Tab přijmou `klíč:`, Esc zavře, hodnoty se
  nikdy nedoplňují), vedle labelu `SearchQueryHelp` (`?` tlačítko → modal s operátory a filtry
  s příklady, řádky z `QUERY_HELP_ROWS`/`QUERY_HELP_OPERATORS`, texty `search.help.*` cs+en),
  a `unknown_tokens` z odpovědi (`PhotoListResponse.unknown_tokens` → `usePaginatedPhotos`
  vrací `unknownTokens`) → neblokující info hint „těmto filtrům nerozumím“ nad mřížkou;
  čistě filtrový dotaz vrací `mode: "filter"` (`EffectiveSearchMode`); dlaždice nesou scope hledání v detail odkazu
  (`detailQuery` s `q`+`mode`) → Esc/Zpět z fotky se vrací k hledání (řazené výsledky, ne knihovna s `q`
  jako podstring) a prev/next pageuje stejné výsledky, plus nad mřížkou **cross-entity sekce**
  (`GlobalSearchSections`) s chipy shodných alb/lidí/štítků (grouped `GET /search/global`), aby
  textový dotaz vynesl i nefotkové entity, plus v hlavičce **`SlideshowStart`** (scope `{mode}`,
  takže promítání přehraje **výsledky hledání**, ne knihovnu filtrovanou podstringem `q`)
  a **jediný vstupní bod uložených hledání**
  (`SavedSearchesDropdown` — vypsat, otevřít, „Spravovat" → `/saved`) vedle tlačítka **Uložit pohled**
  (`SaveSearchModal` — `params` nese i `mode`, takže obnova míří na `/search`),
  plus pro editory **režim výběru** nad výsledky → `BulkEditControl` (po úspěchu se hledání
  přehraje přes `reloadKey`); změna `q`/`mode` je jiná sada výsledků, takže **opouští režim výběru**
  (filtry, které jen zužují totéž hledání, výběr nechají, stejně jako v knihovně),
  `UploadPage` = multiupload (drag-and-drop + galerie/fotoaparát na mobilu, **mobile-first**):
  `DropZone` nad **sticky** `UploadProgressHeader` (celkový průběh dávky) a virtualizovaným
  `UploadList` (`UploadItem` řádky), ovládání start/clear + přepínač **jen neúspěšné** (filtr
  `showErrorsOnly` na chybné soubory); completed summary + odkaz na nově nahrané fotky
  (`/?sort=added`, přes `LIBRARY_PATH` v hlavičce) a retry-failed jsou v `UploadProgressHeader`; nad frontou
  `UploadOrganize` — před nahráním lze vybrat **alba a štítky** pro celou dávku a po dosettlování
  všech souborů se **všechny** rozpoznané fotky (nové **i** duplicitní `resolvedUids`) přiřadí
  jedním `POST /photos/bulk` (stav „přiřazuji…“, úspěch, nebo **opakovatelná** chyba — fotky jsou
  nahrané, selhalo jen přiřazení); bez výběru se žádné volání nedělá,
  `ImportPage` = `/import` (admin **nebo** ai) konzole importu/migrace: dvě sekce (PhotoPrism,
  photo-sorter) s tlačítkem **Spustit import** (gate na `sources` flagy), živý průběh běžícího běhu
  (spinner + counts imported/updated/skipped/failed) a stav fronty na pozadí (`GET /jobs/stats`),
  plus tabulka **historie běhů** (`import_runs`: zdroj/začátek/konec/stav/počty/chyba); polluje
  `GET /import/runs` + `GET /jobs/stats` po 3 s, 409 → „už běží", confirm před prvním (velkým) během
  zdroje, sebe-gate na `canImport`. Historie ukazuje i běhy zdroje **`folder`** (`kukatko import dir`,
  čte adresář na disku serveru → **nemá tlačítko**, jen se objeví v tabulce): v `services/import.ts`
  je proto `RunSource` = `ImportSource | 'folder'` (spouštěcí sekce zůstávají `SOURCES` =
  photoprism/photosorter), popisek `import.source.folder`,
  `MaintenancePage` = `/maintenance` (jen admin) konzole údržby knihovny: tlačítko **Spustit kontrolu**
  (`GET /maintenance/scan`) → souhrn totálů + tabulka nálezů (počet + vzorky per třída, nebo „knihovna
  konzistentní"), checkboxy oprav (náhledy/embeddingy/obličeje/hashe/import osiřelých — anotované
  zbývajícím počtem z poslední kontroly) → **Spustit opravy** (`POST /maintenance/repair`) s výsledným
  souhrnem, plus stav fronty na pozadí (`GET /jobs/stats` polluje po 3 s) jako progress; **každý nález,
  souhrnný „drift" řádek i každý stav fronty nese tiché plain-language vysvětlení** (bez najetí myší) —
  `maintenance.findings.descriptions.*`, `maintenance.scan.summaryHint`, `maintenance.jobs.intro`
  a sdílená `JobStateLegend` (total/queued/running/failed/**dead**) — aby admin poznal, co počet
  znamená a zda je třeba jednat; sebe-gate na `isAdmin`,
  `SystemStatusPage` = `/system` (jen admin) **system-status dashboard**: auto-refresh (polling 5 s)
  `GET /system/status` → kartová mřížka (DB, embeddingy, fronta jobů, záloha, importy, úložiště,
  **mapy**, verze) s **rychlými akcemi** — *znovu zařadit mrtvé úlohy* (`requeueDeadLetterJobs`: list dead →
  per-job `POST /jobs/{id}/requeue`), *spustit zálohu* (`POST /backup`), odkazy na flow importu
  (`/import`) a kontroly údržby (`/maintenance`); **box offline** + čekající embeddingy → zvýrazněná
  hláška „doženou se po návratu"; **karta Mapy** (`MapsCard` nad `status.maps`) ukazuje poslední
  stav mapy.com — `key_rejected` červeně + co s tím (vyměnit klíč v konzoli mapy.com), degradace
  žlutě, bez klíče „Nenastaveno"; karta fronty jobů nese sdílenou `JobStateLegend`
  (total/queued/running/failed/**dead**/**pending** = „Čeká na box") s plain-language vysvětlením
  každého stavu (`jobStates.*` + `system.jobs.intro`); loading/error/notice stavy, sebe-gate na `isAdmin`,
  `UsersPage` = `/users` (jen admin) **správa účtů**: tabulka uživatelů (jméno, celé jméno, role,
  stav, poznámka, poslední přihlášení, vytvořen) nad `GET /admin/users`, dialogy **Nový uživatel**
  (username/heslo/role/jméno/poznámka) a **Upravit** (role/jméno/poznámka; username je `readOnly`
  `plaintext` — backend ho měnit neumí), **Změnit heslo** jinému uživateli (odhlásí ho ze všech
  zařízení; hash se nikdy nikam nevykresluje) a **Povolit/Zakázat** za potvrzovacím dialogem
  (`setUserDisabled`); **vlastní řádek má toggle `disabled`** + krátké vysvětlení proč
  (`users.selfDisableHint`), **mazání se nenabízí** — účet se vyřazuje zakázáním, aby historie
  (fotky, hodnocení, audit) zůstala celá. Validační chyby API se mapují na konkrétní pole
  (`fieldErrorFor`: 409 → username, 400 podle klíčového slova → password/role/note, jinak
  form-level alert), ne na obecný banner. Stavy: **skeleton** (`Placeholder` v tabulce) při načítání,
  error alert s **Zkusit znovu**, prázdný stav (`EmptyState`, prakticky nedosažitelný — bootstrap
  admin vždy existuje, ale nesmí spadnout); sebe-gate na `isAdmin`,
  `AuditPage` = `/audit` (jen admin) **auditní log**: read-only tabulka záznamů z `GET /audit`
  od nejnovějších (kdy/kdo/akce/cíl/IP), `details` JSON přes rozbalovací řádek (`aria-expanded`,
  ukáže i `user_agent`). Filtry (aktér = `<select>` nad rosterem přes `fetchUsers`, akce, typ+UID
  entity, rozsah dat `od`/`do`) v **draft** formuláři → **Filtrovat** je zapíše do URL a resetuje
  stránku, **Zrušit filtry** vyčistí; datumy se v `viewToParams` rozšíří na RFC 3339 hranice dne
  (UTC). Stránkování prev/next nad `offset`/`next_offset` (limit 100) s počtem `od–do z total`;
  filtry i offset žijí v URL (`useUrlState` nad `AUDIT_DEFAULTS`), takže Zpět obnoví přesný pohled.
  Jména aktérů se dotahují z rosteru **best-effort** (fallback na UID, resp. `—` u systémové akce),
  nikdy neblokují render tabulky. Loading/empty/error (retry přes `reloadKey`) stavy, sebe-gate na
  `isAdmin`,
  `PhotoDetailPage` = `/photos/:uid` **bohatý detail fotky**: velký náhled **na celou šířku
  obsahové oblasti** (`fit_1920`, still až `maxHeight:80vh`)
  reflektující uložený nedestruktivní edit (CSS) — u **videa** místo obrázku `VideoPlayer`
  (`components/photo/`, HTML5 `<video controls>` nad range endpointem `…/video`, poster `fit_1920`,
  klávesy/fullscreen/touch zdarma, fallback na stažení když codec prohlížeč neumí), u **live fotky**
  `LivePhoto` (still + „Live" badge, motion klip se přehraje při hover/podržení/focusu); **klik na
  still náhled otevře fullscreen lightbox** (`Lightbox` v `components/photo/` + `lightbox.css`):
  fotka na celou obrazovku (contain) na tmavém pozadí s uloženým editem, **velké šipky vlevo/vpravo**
  listující stejné pořadí/scope jako detail (vlastní `usePhotoNeighbors` nad `neighborParams` + `mode`
  pro hledání, stop na koncích), klávesy ←/→ + Esc, **dotyková gesta** (`usePinchZoom`): pinch a dvojklik
zoom s posunem (pan) při přiblížení a horizontální swipe pro listování, když přiblíženo není (zoom se
**resetuje při změně fotky i zavření**); close křížkem (44px tap-target) i klikem na **pozadí** — klik do
samotného obrázku už nezavírá (obrázek má `pointer-events`/`touch-action:none`, aby gesta i dvojklik
fungovaly; odpovídá to původnímu záměru komentáře „zavřít jen kliknutím na pozadí, ne na obrázek"),
  přednačtení sousedů (`new Image()` na `fit_1920`), fetch title+editu zobrazené fotky při navigaci;
  lightbox si listuje **interně bez změny URL** a při zavření předá aktuální uid zpět → detail obnoví
  URL (`navigate` replace), takže Zpět vždy funguje; video/live neotevírá image-lightbox (mají vlastní
  nativní fullscreen), a **detailové klávesové zkratky (←/→/Esc/rating hotkeys) jsou při otevřeném
  lightboxu vypnuté** (`useKeyboardShortcuts({enabled:!lightboxOpen})`), aby je ovládal lightbox;
  **prev/next navigace** respektující pořadí
  zdrojového výpisu (`usePhotoNeighbors` pageuje `GET /photos` se scope+filtry z URL — nebo `GET /search`,
  když detail vznikl z hledání, aby prev/next drželo stejné řazené výsledky); **na dotyku i horizontální
  swipe přes obrázek** (`useSwipeNavigation`) volá tutéž navigaci jako šipky/klávesy — vertikální tah
  scrolluje (nikdy nelistuje), gesto se ignoruje na interaktivních prvcích (obličejové boxy, ‹/› šipky),
  protože jen obrázkové tlačítko nese `data-swipe-surface`;
  **listování bez fullpage flickeru** — jen **první** načtení ukáže velký spinner, při skoku na souseda
  zůstane aktuální fotka namontovaná a nová se dotáhne na pozadí (`setState` nereaguje na `loading`, když
  už je `ready`), pak se **swapne na místě**; nad snímkem přitom svítí nenápadný rohový spinner
  (`photo.loadingNext`). Dokud běží načítání souseda (`loadingNext = photo.uid !== uid`), jsou obličeje/„bez
  obličejů"/toggle keyed na cílový uid **potlačené**, aby se boxy fotky B nekreslily nad fotkou A; abort na
  změnu `uid` ruší předběhnutý request (poslední cíl vyhrává), chyba načtení souseda spadne do error stavu
  (nenechá tiše starou fotku),
  deep-linkovatelný + **Zpět** na zdrojový pohled — album/štítek/oblíbené/hledání/knihovnu podle scope,
  který dlaždice nese v `detailQuery` odkazu (`lib/detailView` `backHref`/`detailToParams`/
  `detailQueryString`; hledání se pozná podle `mode` v query, oblíbené podle `favorite=true`),
  v hlavičce `RatingStars`+`FlagControl` (per-user hvězdy 0–5 + osobní označení eye/👍/👎
  nad `useRating`) a `FavoriteButton`, plus
  **rating hotkeys** `0`–`5`/`p`/`r`/`v` na document (mimo
  psaní do inputu; `p`→👍, `r`→👎, `v`→👁), tlačítka **Stáhnout originál** /
  **Stáhnout upravenou** (`downloadUrl`); **stránka nese právě JEDEN obrázek fotky** — obličeje
  jsou **přepínatelný overlay** nad ním (`FaceOverlay` nad `useFaces`), nikdy druhá kopie snímku,
  a **i panel Úprav edituje právě tenhle jeden snímek** (viz níž), takže druhý `<img>` nepřidá
  ani on.
  **Obličeje jsou defaultně VYPNUTÉ** (`FACE_OVERLAY_DEFAULT = false` v `lib/faceOverlayPref`,
  volba se pamatuje v localStorage): fotka je obsah, boxy jsou opt-in. Zapne je tlačítko
  **Zobrazit/Skrýt obličeje** (jen u stillu s aspoň jedním obličejem, `aria-pressed`) nebo klávesa
  **`m`** (v registru zkratek, takže ji ukáže i nápověda `?`). Zapnutí **zmenší fotku na `lg={8}`
  a vedle ní se nasadí `FacesPanel` (`lg={4}`)** — prostý reflow gridu, **žádná animace ani
  Offcanvas**; pod `lg` se panel stackuje pod fotku. Vedle fotky je **jeden sloupec pro jeden
  panel**: `sidePanel: 'faces' | 'edit' | null` (jediný stav, ne boolean na každý panel) dělá
  stav „dva panely vedle sebe" **nereprezentovatelný** — otevření jednoho zavře druhý a fotka má
  právě dva layouty (plná šířka / `lg={8}` s panelem), nikdy třetí. Týž `sidePanel` řídí boxy i
  panel obličejů, takže se nemůžou rozejít. **Obličejové UI stojí celé (tlačítko i `m`) jen když
  je preview identita** (`isIdentityEdit(previewEdit)` v `facesAvailable`): `clip-path`/`transform`
  živého i uloženého editu posunou vykreslené pixely pod boxy, které se pozicují v procentech
  obalu — rámečky by prostě minuly obličeje, tak se radši nekreslí (a vrátí se, jakmile je preview
  zase neutrální). **Pozor — nosná invarianta:**
  `FaceOverlay` pozicuje boxy v **procentech** obalu `position-relative d-inline-flex mw-100`, který
  se smrskává přesně na `<img>`; kdyby ho sloupec roztáhl (`w-100`, `align-items: stretch`), obrázek
  se uvnitř vycentruje s letterboxem a **rámečky se rozjedou** (jsdom to nechytí — ověřovat vizuálně).
  Boxy jsou barevné dle stavu (`lib/faceState`: zelená přiřazený / žlutá marker bez osoby / červená
  holá detekce), vybraný je primary + ring, každý nese **číslo `#N`** (dle pořadí, ne `face_index` —
  markery bez detekce mají záporný) a přiřazený i **jméno pod boxem**; hover na boxu zvýrazní řádek
  v panelu a naopak (`hovered`/`onHover` drží stránka). Klik na box i na řádek panelu = tentýž výběr;
  **mezi titulkem a fotkou** je **read-only pruh badgí** `OrganizeBadges` (`components/photo/`):
  nejdřív alba, pak štítky fotky jako pill-badge (`ENTITY_STYLE` — fialová `album` / tyrkysová
  `tag` + vodicí ikona, vzhled 1:1 s chipy v `OrganizePanel`), každá **odkazuje** na
  `/albums/:uid`, resp. `/labels/:uid`. Odpovídá na „kam je fotka zařazená" bez scrollování dolů.
  **Žádné ovládání** (bez remove/add/create) — přidávat a odebírat se dá dál **jen** v
  `OrganizePanel`; oba čtou tentýž `photo.albums`/`photo.labels`, takže se pruh po editaci
  aktualizuje **okamžitě a bez druhého fetche**. Badge se **zalamují** (`flex-wrap`, žádný
  horizontální scroll), a když fotka není v žádném albu ani nemá štítek, **nevykreslí se nic**
  (žádný nadpis, placeholder ani mezera). Viewer vidí přesně totéž co editor;
  **ovládací/informační panely jsou POD fotkou** (ne vedle ní) a jdou **přes celou šířku
  obsahové oblasti** (žádný centrovaný užší sloupec) ve **striktním edit-first pořadí** —
  pruh `SimilarPhotos` je až pod nimi. První dvě karty sdílejí **od `lg` výš jeden řádek**
  v poměru **4:8** — **Uspořádání** (užší rail) vedle **Popis a místo** (`Row`
  `align-items-start` + `Col` `lg={4}`/`lg={8}`; `align-items-start` drží obě karty v jejich přirozené výšce, aby
  se kratší nenatáhla do prázdné krabice). **Technické údaje** pod nimi jsou
  samostatná karta na plnou šířku (**Úpravy** už mezi kartami nejsou — jsou panel vedle fotky,
  viz níž). **Pod `lg` se stackují na plnou šířku ve stejném pořadí**,
  takže na mobilu platí totéž čtecí pořadí. Karty
  (`components/photo/`): **1. Uspořádání** (`sections.organize`) = **primární blok, vždy
  viditelný a přímo editovatelný** (žádný „edit mód"): `OrganizePanel` (inline add/remove alb
  a štítků přes organize API) + `PeoplePanel` (lidé/obličeje jako **person-chips** nad stejným
  `useFaces`, co drží overlay — odpovídá na „kdo je na fotce" i s vypnutými obličeji; **sám nic
  nepřiřazuje**: editorův klik na chip volá `onEditFace` → stránka zapne obličeje a vybere ten
  obličej ve `FacesPanel`, takže přiřazování žije právě na jednom místě. Viewer vidí pojmenované
  osoby read-only; pojmenované = rose chip, nepojmenované detekce = neutrální chip); alba/štítky/lidé mají
  odlišnou barvu přes `ENTITY_STYLE` (`components/entityStyle`). Přidání jede přes
  **`AddAutocomplete`** (type-to-filter combobox nad react-bootstrap primitivy,
  **case/accent-insensitive** přes `lib/text` `foldedIncludes`, klávesnice ↑/↓/Enter/Esc + klik,
  „nic neodpovídá" stav, ~44px tap-targety, ARIA combobox/listbox; volitelná prop `onCreate` přidá
  řádek „Vytvořit «dotaz»" — `createAndAttachLabel` udělá `createLabel` + `attachLabel`, shodu
  jména hledá `foldedEquals`, takže existující štítek jen připojí místo kolize na slugu; alba se
  odsud nezakládají — typ/obálka/privátnost patří na stránku Alba). **2. Popis a místo**
  (`sections.caption`) = `MetadataPanel` = title/description/ai_note/notes/taken_at/poloha
  **read-only, dokud editor neklikne na pole** — každé pole je vlastní inline edit affordance
  (`EditableField` = celý řádek je tlačítko „Upravit «pole»" s pencil ikonou a muted „Přidat…"
  placeholderem u prázdného pole), **žádné skryté globální „Upravit"** dole (to byl fix tohoto
  tasku — dohledatelnost editace title/description/AI popisu). Klik na kterékoli pole otevře jeden
  sdílený formulář (title/description/ai_note/notes/taken_at + **přibližné datum** +
  **vizuální location picker**), Save `updatePhoto` PATCH, Cancel revert.
  **Přibližné („cca") datum** — pro naskenované/zděděné fotky, kde přesné datum nikdo nezná:
  ve formuláři checkbox „Datum je odhad" (`taken_at_estimated`) a **jen když je zaškrtnutý** textové
  pole „Poznámka k datování" (`taken_at_note`, `maxLength=500` zrcadlí backendový strop) — prázdná
  poznámka u data-faktu nedává smysl, tak formulář nezaplevelí; obojí ukládá tentýž PATCH (žádné
  vlastní tlačítko). Odškrtnutí odhadu nechá poznámku ve formuláři (kdyby si to rozmyslel), ale
  posílá se jen `taken_at_estimated: false` — poznámku smaže server. Read-only se odhadované datum
  renderuje přes `CaptureDate` (v `MetadataPanel.tsx`): badge `cca` (cs) / `c.` (en) + datum +
  poznámka kurzívou, badge nese `title` s poznámkou (**ne** jen barva/glyf), takže odhad nelze
  zaměnit za jisté datum ani letmým pohledem, ani ve screen readeru; fotka **bez** `taken_at` může
  být odhad taky — pak stojí marker s poznámkou samy. `EditableField` proto bere volitelný
  `display?: ReactNode` (bohatší render hodnoty, prostý `value` dál rozhoduje o „vyplněnosti"). Location picker = **tři cesty dovnitř** v pořadí, jak po nich člověk sáhne:
  **`PlaceSearch`** (najdi místo podle názvu), jedno tolerantní pole souřadnic parsované
  `lib/coordinates` (`parseCoordinates`/`formatCoordinates`: desetinné stupně `49.1234, 16.5678`,
  DMS `49°7'24.2"N 16°34'12.5"E`, stupně-desetinné-minuty, hemisféry, axis reorder, range check)
  a **`LeafletMap` picker mód** (`picker={position,onPick}`: draggable marker + click-to-place,
  obousměrný sync text↔marker, vymazat polohu = lat/lng null). Všechny tři **zapisují totéž jedno
  pole souřadnic**, které čte save — nemají tedy jak si o poloze protiřečit. **PATCH nese jen skutečně změněná
  pole**: nezměněný `taken_at` (pole je `step=1`, drží sekundy) by přepnul `taken_at_source`
  `exif`→`manual`, nezměněné souřadnice by se zaokrouhlily na 6 desetinných míst textového pole —
  obojí by tiše přepsalo katalog. **Neplatný text souřadnic = inline chyba u pole**, ne blokace
  celého formuláře: ostatní pole se uloží, poloha zůstane beze změny a formulář zůstane otevřený
  (Save se **nedisabluje**).
  **Odhadnutá poloha** (`location_source === 'estimate'`, viz `internal/geoestimate`) se v read-only
  řádku Poloha renderuje přes `EstimatedLocation` (v `MetadataPanel.tsx`): badge `odhad` (cs) /
  `estimate` (en) s `title` „Odhad podle fotek z téhož dne, ne změřená poloha" + jednořádkové
  vysvětlení, odkud se vzala — **labelovaný badge a věta, ne jemnější odstín**: odhadnutá poloha, co
  vypadá stejně jako skutečná, je lež, kterou appka uživateli říká, a barva sama o sobě neřekne
  screen readeru nic. Editor pod tím dostane **dvě cesty ven** (viewer vidí jen marker — i on má vědět,
  že špendlík je tip): **Potvrdit odhad** pošle `{location_source:'manual'}` — jen původ, **nikdy** ne
  souřadnice zpět (ty by se zaokrouhlily na 6 desetinných míst, co vykreslil formulář, a špendlík by se
  posunul jako cena za souhlas) — a **Zahodit odhad** pošle `{lat:null,lng:null}`, což si backend
  zapíše jako rozhodnutí (`manual` bez souřadnic) a **stejný tip už znovu nenabídne** (help text to
  říká rovnou, místo aby to uživatel zjistil tím, že se to nikdy nevrátí). Obojí je vlastní one-click
  request (`resolveEstimate`, vlastní busy/failed stav) mimo formulářový Save — je to odpověď na otázku,
  kterou položila appka, ne editace, kvůli které uživatel přišel; `location_source` se čte z `photo`,
  ne z form state, protože jde o fakt o uloženém řádku. Poloha z EXIF ani ta s **neznámým** původem
  (`''`, starší řádky) se **neoznačuje** — „nevíme" není „hádali jsme".
  **IPTC/XMP kredity** (`credits` pod-sekce ve stejném formuláři, **na první render sbalená**,
  chevron toggle `aria-expanded`/`aria-controls` jako `TechnicalDetails`) — patří na naskenované/zděděné
  fotky, kde EXIF ani importy o autorovi/roce nic neví: textová pole **Předmět** (`subject`),
  **Umělec** (`artist`), **Autorská práva** (`copyright`), **Licence** (`license`), chipové pole
  **Klíčová slova** (`keywords`) a checkbox **Sken** (`scan`). Ukládá je **tentýž** `updatePhoto` PATCH
  (žádné druhé tlačítko/formulář/request); `maxLength` polí zrcadlí backendový `creditLimits`
  (subject/copyright/license 1000, artist 255). Klíčová slova = jeden comma-separated string v DB,
  editovaný jako chipy přes `KeywordsInput` (sdílený `badge rounded-pill` + `ENTITY_STYLE.tag` look,
  ale **ne** štítky — žádný link na `/labels/:uid`): Enter/čárka/vložení „a, b" přidá, click na křížek
  ubere, Backspace v prázdném poli sundá poslední, blur zapíše rozepsané slovo; helpery
  `addKeywords`/`joinKeywords`/`sameKeywords`/`splitKeywords` (`lib/photoFacts`) trimují,
  de-duplikují a hlídají 2000-run strop na spojeném stringu (rune-count = Go `utf8.RuneCountInString`).
  Kredity jdou do PATCHe **jen když se skutečně změnily** (form normalizuje: trim + rejoin, takže
  nezměněné pole by přepsalo formulaci ze zdrojového souboru); vyprázdněné pole se pošle jako `""`
  (smaže), neúspěšný PATCH **drží** rozepsané hodnoty a ukáže existující `saveError` alert.
  Odpověď PATCHe je plný detail (`albums`/`labels`/`files`), kterým
  stránka nahradí drženou fotku; read-only poloha = `PhotoLocation` (mini-mapa nad mapy.com proxy + on-demand
  `reverseGeocode`) **embedovaná** v tomto bloku. **3. Technické údaje** (`TechnicalDetails`,
  **na první render zavřený** expander `aria-expanded`/`aria-controls`): **všechno, co appka o fotce
  ví**, ve **skupinách** (`MetaGroup` = nadpis + `<dl className="row">`, dva sloupce na širokém
  viewportu, jeden na úzkém; dlouhé hodnoty se lámou, nikdy neroztáhnou stránku):
  **Fotografie** (camera/lens/clona/expozice/ohnisko/ISO, sériové číslo, software, zdroj data
  pořízení, IPTC/XMP kredity `subject`/`artist`/`copyright`/`license`, `keywords` jako **chipy**
  rozsekané na čárce, `projection` + badge řádek `private`/`scan`), **Soubor** (název, `original_name`
  jen když se liší, formát z MIME, velikost — přesný počet bajtů v `title`, rozměry, **poměr stran**
  a **Mpx** (dopočet), EXIF orientace 1–8 jako popisek, barevný profil, `image_codec`, zkrácený
  SHA256 s plnou hodnotou v `title` a **copy-to-clipboard**, přidáno/změněno), **Poloha**
  (souřadnice, `altitude`, + **cachnuté** `place` z detailu — země/region/město/místo; **žádné
  on-demand geokódování**, to dělá jen `PhotoLocation` na vyžádání), **Video** (jen `media_type`
  `video`/`live`: délka `m:ss`, kodeky, zvuk ano/ne, fps) a **Původ** (Nahrál/a
  `photo.metadata.uploadedBy` z `photo.uploader.name`, fallback `—` `uploaderUnknown`, +
  `photoprism_uid`/`photosorter_uid`). Vše **read-only** (editace patří do `MetadataPanel`);
  **pole bez hodnoty se nerenderuje vůbec** (`MetaField` vrací `null`) a **prázdná skupina se
  nerenderuje taky** — fotka s chudými metadaty není zeď pomlček. Čísla/datumy přes aktivní locale
  (`i18n.language` → čeština má desetinnou čárku). **Servisní akce zde** (jen editor/admin, `canWrite`): `RegenerateThumbnailButton`
  (`components/photo/`) uvnitř rozbaleného expanderu volá `regenerateThumbnail(uid)` (POST
  `/photos/{uid}/regenerate-thumbnail`), ukazuje **pending** (spinner + `disabled`), pak úspěch
  nebo chybu (422 = „originál chybí nebo ho nelze dekódovat", jinak obecná hláška); po úspěchu
  zavolá `onThumbnailRegenerated`, což v `PhotoDetailPage` **bumpne `thumbVersion`** a připojí
  `?v=` k `poster` (thumb URL se staví z UID, tedy stabilní → cache-bust vynutí načtení nového
  náhledu bez tvrdého reloadu). Viewer tlačítko nevidí. **Úpravy už mezi kartami pod fotkou
  nejsou** — patří k fotce, kterou upravují, takže `EditPanel` (editor/admin, jen still) je
  **panel VEDLE fotky**, přesně jako `FacesPanel`: otevře ho tlačítko **Úpravy** (`aria-pressed`)
  v řádku ovládání pod snímkem, fotka se zmenší na `lg={8}`, panel se nasadí na `lg={4}` (pod `lg`
  pod ni), hlavička nese název + zavírací **`x-lg`** (`photo.edit.closePanel`). Rotace/jas/kontrast/
  crop, `PUT /photos/{uid}/edit` přes `saveEdit` — ten posílá **jen samotný edit** (`rotation`/
  `brightness`/`contrast`/`crop_*`): typ `PhotoEdit` slouží i jako odpověď GETu a nese navíc
  `photo_uid`/`updated_at`, jenže PUT tělo dekóduje **striktně**, takže poslat vrácený objekt
  rovnou zpátky = 400 „malformed JSON body" (tohle uložení dřív shodilo; chybějící crop pole se
  prostě vynechá, což API čte jako „bez cropu"). **Vlastní `<img>` nemá** — je to **controlled
  komponenta**: rozpracovaný edit drží stránka (`editDraft`, `null` = nic neuloženého), panel ho
  hlásí přes `onChange` nahoru — a to **jako updater `(prev) => next`, ne hotovou hodnotu**: dvě
  ovládání změněná v jednom Reactím batchi čtou týž ještě nepřerenderovaný `edit` prop, takže
  poskládat next value v panelu = **tiše zahodit tu první změnu** (stránka updater aplikuje přes
  `applyEdit`, první změna staví na `state.edit`, protože draft ještě není) — a **preview je ta
  JEDNA originální fotka nahoře**
  (`editPreviewStyle(previewEdit)`, `previewEdit = editDraft ?? state.edit`) — proto zůstává celou
  dobu vidět a mění se živě pod rukama. Zavření i skok na souseda (`uid` efekt) draft zahodí
  (fotka se vrátí k uloženému stavu), úspěšný save ho vymění za `state.edit` bez bliknutí.
  Otevření Úprav navíc **sundá obličeje** (jeden sloupec = jeden panel) i výběr obličeje, ale
  **uloženou volbu overlaye nepřepíše** — skrytí je důsledek otevření Úprav, ne rozhodnutí o
  obličejích, takže přežije na další fotku. Viewer vidí vše read-only
  (žádné tlačítko Úpravy, žádné edit/add/remove akce, žádný přepínač soukromí, `FaceOverlay` readOnly
  = boxy vidí, ale neklikne);
  `StackStrip` (`components/photo/`, **NOVÝ**) = **pruh variant stacku** pod fotkou na detailu: vypíše
  každého člena (náhled, jméno, rozměry, velikost), označí **primárního** (`stack.primary`) a linkuje na
  kteroukoli variantu (`stack.viewVariant`); editorovi per-člen tlačítka **Nastavit jako hlavní**
  (`stack.setPrimary` → `setStackPrimary`) / **Vyjmout ze skupiny** (`stack.unstack` → `unstackMember`)
  a **Zrušit skupinu** (`stack.unstackAll` → `unstackAll`). Renderuje ho `PhotoDetailPage` **pod řádkem
  s médiem**, jen když `stack_members` má **≥ 2** položky; jeho akce znovunačtou zobrazenou fotku;
  `components/photo/` dál nese `MetaField` (jeden read-only labelled řádek `<dt>`/`<dd>` uvnitř
  `<dl className="row">` skupiny, prázdná hodnota = nic; volitelný `title` = tooltip nad zkrácenou
  hodnotou a `children` = bohatá hodnota (chipy/badge/copy tlačítko), řádek s `children` se renderuje
  vždy — o prázdnotě rozhoduje volající); `lib/photoFacts` = pure odvozená fakta o souboru
  (`aspectRatio` — zlomek zkrácený přes gcd, decimal fallback `1,50 : 1` když se nezkrátí na čitelné
  členy; `megapixels`; `formatMime` → `JPEG`/`MOV`; `orientation`/`takenAtSource` = zúžení na
  literal union, aby `t()` klíč zůstal typovaný; `splitKeywords`; `shortHash`), `lib/format`
  `formatBytes(bytes, locale?)` (locale = desetinná čárka) a `formatByteCount` (přesný počet bajtů
  do tooltipu); `lib/photoEdit` = pure helpery
  edit→CSS (`editPreviewStyle`/`editFilter`/`editTransform`/`cropClipPath`/`isIdentityEdit`/
  `rotateRight`/`hasCrop`/`NEUTRAL_EDIT`),
  `PeoplePage` = `/people` index osob: responzivní mřížka `SubjectTile` (obrázek/jméno/počet
  fotek), editorům odkaz na review shluků; dlaždice ukazuje **obličej té osoby** — co přesně,
  rozhoduje pure `lib/subjectTile.ts` `subjectTileImage` → `{kind:'cover'|'face'|'none'}`:
  explicitní `cover_photo_uid` vyhrává vždy (rozhodnutí přebíjí odhad), jinak `cover_face` z API
  (výběr markeru viz `listSubjectsSQL`) `padBbox(0.3)` + `squareCrop` → `FaceCrop`, a bez
  použitelného obličeje zůstává placeholder (`people.noCover`) — appka si obličej nevymýšlí,
  `SubjectPage` = `/people/:uid` stránka osoby: hlavička (jméno/typ + edit přes
  `SubjectEditModal`), paginovaná galerie (`useSubjectPhotos` + `SubjectPhotoTile` se
  „set as cover" akcí editorům), a sekce `Outliers` (jen editor/admin); editoři můžou v galerii
  **vybírat** → `BulkEditControl` (po úspěchu refetch galerie) — v režimu výběru je dlaždice jeden
  selection target, takže „set as cover" ustoupí, jako srdíčko/hvězdy na dlaždici knihovny,
  `ClustersPage` = `/people/clusters` (editor/admin) review fronta nepojmenovaných shluků:
  `ClusterCard` (reprezentant + ukázky + odebrání zatoulaného obličeje + jednorázové pojmenování
  celého shluku) v `Row`/`Col` mřížce, optimistické odebrání po pojmenování,
  `FacesPage` = `/faces` (editor/admin, odkaz v „Nástrojích") „najdi osobu mezi neotagovanými
  fotkami": config panel `CandidateSearchForm` (výběr osoby přes `AddAutocomplete` s počtem fotek
  v `hint`, práh v **procentech** 20–80 % s bookendy „Více výsledků"↔„Lepší shody", limit, tlačítko
  Hledat — hledání je **explicitní**, ne live-on-drag), volá `searchCandidates()` (převod procent→
  vzdálenost přes `percentToDistance` z `lib/faceThreshold`), `CandidateStats` ukáže zdrojové fotky/
  obličeje, nalezené shody, hotovo i **spočítaný `min_match_count`** s vysvětlením; `CandidateFilterTabs`
  (Vše/Nové/Přiřadit/Hotovo s počty, scopne i „Potvrdit vše"), `CandidateLegend` + `CandidateCard`
  (`CandidateFaceImage` = **plný `fit_720` náhled** s obličejem jako **barevný obdélník** přes
  `faceBoxStyle`, ne oříznutý čip; barva/badge/obdélník sdílí jeden kód přes bucket `new`/`assign`/
  `done` v `lib/candidateReview`); ✓ potvrdí (`assignFace`, `create_marker` vs `assign_person` dle
  `marker_uid` kandidáta) **optimisticky na místě** (karta se překlopí, mřížka se nereloadne), ✗
  **trvale zamítne** přes `rejectFace` (`services/feedback`) a kartu odebere; **klávesnice** (šipky/
  `jkhl` posun, `y`/`Enter` potvrdit, `n` zamítnout, fokus skáče na další akční kartu — registrováno
  v `?` nápovědě přes `shortcuts.groups.faceSearch`), „Potvrdit vše (n)" projde akční karty aktivní
  záložky sekvenčně s live `current/total`, zrušitelně, **částečné selhání neroluje zpět** a nahlásí
  co selhalo — stav review drží `useCandidateReview`; config (osoba/práh/limit/záložka) v URL,
  stavy prázdno/bez-obličejů/bez-embeddingů/nula-shod/loading,
  `RecognitionPage` = `/recognition` (editor/admin, odkaz v „Nástrojích") **recognition sweep**
  „projdi všechny a najdi shody mezi neoznačenými obličeji": config panel (posuvník **jistoty** v
  procentech 50–95 %, step 1, **default 75 %** — těsný, tahle stránka je pro snadné výhry; limit na
  osobu; tlačítko Prohledat) volá **stream** `streamSweep()` (`services/recognition`, NDJSON přes
  `fetch`+`ReadableStream`); během scanu **živý pruh** `current/total` + jméno právě prohledávaného
  a **zrušení** (`cancel`→`AbortController`), karty se objevují **osobu po osobě** jak přicházejí, ne
  až na konci; jedna `PersonSweepCard` na osobu = hlavička (jméno + počet k vyřízení + **„Potvrdit vše
  (n)"**) nad **stejnou** bbox mřížkou co `/faces` (**reuse `CandidateCard`**, žádný fork); ✓ potvrdí
  (`assignFace`), ✗ **trvale zamítne** (`rejectFace`); **když se vyřídí poslední kandidát osoby, celá
  karta zmizí** (list se zmenšuje = odměna) — stav řídí `useSweepReview` (`people` filtruje na ty s
  akčními kartami přes `hasActionable`); **klávesnice** stejná jako `/faces` (šipky/`jkhl` posun přes
  plochou `focusSequence` napříč osobami, `y`/`Enter` potvrdit, `n` zamítnout — reuse
  `useKeyboardShortcuts` + `shortcuts.groups.faceSearch`); globální statistiky (k vyřízení / už
  přiřazené / lidé se shodami) ze `summary`, `capped` upozornění, **čistý prázdný stav** po scanu bez
  shod („všechny obličeje jsou přiřazené"); config (jistota/limit) v URL; **nikdy neautoconfirmuje**,
  `ExpandPage` = `/expand` (editor/admin, top-level odkaz **Rozšířit** u alb/štítků) „rozšiř album
  nebo štítek o vizuálně podobné fotky": config panel `ExpandSearchForm` (přepínač **Album|Štítek**
  (`ToggleButtonGroup`), výběr sbírky přes `AddAutocomplete` — options z `lib/expandSearch`
  `expandSources` **seřazené dle počtu fotek sestupně, prázdné sbírky vynechané**, počet v `hint` —,
  práh v **procentech** 20–80 % step 5 **default 70 %** s bookendy „Více výsledků"↔„Lepší shody"
  (rozsah/konverze sdílené s `lib/faceThreshold`, `expandThresholdDistance` řeže float šum pro URL),
  limit 1–200 default 50 (`clampExpandLimit`), tlačítko Hledat — hledání **explicitní**, ne
  live-on-drag); volá `searchSimilar()` (`services/expand`); výsledky = `ExpandResults`: summary
  řádek (zdrojové fotky / s embeddingem / min. shod / nalezeno) + **vysvětlení vote rule**
  („Fotka musí odpovídat alespoň {{n}} zdrojovým fotkám" + „Řazeno podle počtu shod, pak podle
  podobnosti", u `source_capped` i vzorek) nad **standardní `PhotoGrid`** (žádný fork mřížky);
  dlaždice nese přes `tileExtras` **% podobnosti** a při `match_count > 1` badge **počtu shod**,
  klik otevírá detail fotky jako v knihovně; **výběr = knihovní model** (`useBulkEdit` +
  `SelectionStart`/`SelectionBar`/„Vybrat vše"/Shift+klik rozsah/Esc), `BulkEditControl`
  s **`prefill` = rozšiřovaná sbírka**, takže Apply rovnou přidá; po úspěchu přes
  `BulkEditOutcome` **přidané fotky opustí mřížku na místě** (bez refetche a skoku scrollu,
  errored zůstávají; jiná bulk operace mřížku nemění) a summary počty se aktualizují; ✗ na dlaždici
  (jen **štítky** — alba rejection model nemají, tak se nenabízí) **trvale zamítne** přes
  `rejectLabel` (`services/feedback`) optimisticky s rollbackem + alertem při selhání; **klávesnice**
  jako knihovna (`useGridKeyboardNavigation`: šipky/`hjkl`, Enter otevře, `x` vybere, Esc čistí
  výběr); config (typ/sbírka/práh/limit) v URL (Back/refresh obnoví hledání); stavy
  idle/loading/error/**bez-embeddingů** (vlastní hláška — embeddingy se počítají, až je box online;
  odlišená od nula-shod)/prázdná-sbírka/nula-shod (poraď snížit práh)/vše-vyřízeno,
  `MapPage` = `/map` mapový pohled: geotagované fotky jako shlukované markery nad mapy.com
  dlaždicemi (Leaflet), přepínač podkladu + filtry (datum/archiv/soukromé) v `MapFilterBar`,
  stav (mapset/viewport/filtry) v URL — posun/zoom zapisuje viewport bez refetche, změna filtru
  dotáhne GeoJSON; klik na marker → detail fotky; loading/empty/error stavy; **selhání dlaždic**
  (`onTileError` z `LeafletMap`) diagnostikuje `probeTileFailure` a vysvětlí **zavíratelným
  varováním** (`map.tiles.*`, typicky „mapový klíč byl odmítnut") místo nevysvětlené šedé mřížky —
  mapa zůstává použitelná, markery/shluky se kreslí dál nad prázdným podkladem; probe je
  **debouncovaný** (celá dávka `tileerror` = jeden dotaz) a přepnutí mapsetu varování resetuje;
  fotky s **odhadnutou polohou** (`location_estimated` na feature) jsou v mapě **defaultně** — od toho
  odhad je — ale kreslí se **jiným tvarem** špendlíku (`estimatedMarkerIcon` v `LeafletMap`: dutý
  čárkovaný kroužek, **ne** jen jiná barva — ta nepřežije barvoslepý pohled ani černobílý tisk) plus
  `title` z `estimatedTitle` propu, který totéž řekne slovy screen readeru; špendlík, co vypadá stejně
  jako změřený, by mapu nechal tvrdit přesnost, kterou nemá,
  `PlacesPage` = `/places` procházení knihovny dle lokality: jedním fetchem `fetchPlaces()` natáhne
  hierarchii zemí→měst s počty; **drill v URL** (`?country=&city=` přes `useUrlState` nad
  `PlacesView` = `LibraryView`+`country`/`city`, takže Zpět prochází úrovně) — úroveň 1 seznam zemí
  (`ListGroup`), úroveň 2 města vybrané země (z nested dat, bez refetche), úroveň 3 fotomřížka
  scopnutá na `{country,city}` přes `useScopedPhotos` (enabled až po výběru města) + sdílený
  `FilterBar` + breadcrumb Místa/země/město; loading/empty/error stavy, pro editory **režim výběru**
  nad mřížkou → `BulkEditControl` (po úspěchu refetch, edit může fotku z místa odstěhovat); průchod
  drillem **opouští režim výběru**, každé místo je vlastní seznam,
  `SlideshowPage` = `/slideshow` fullscreen promítání (mimo `Layout`, bez navbaru): čte scope
  (`?album=`/`?label=`/`?mode=` pro hledání/žádné) + filtry/řazení z URL (stejný stav jako mřížka),
  pageuje přes `usePaginatedPhotos` (velké sady se nenačítají najednou) — fetcher je `fetchPhotos`,
  nebo **`searchPhotos`, když URL nese `mode`** (jinak by se `q` jen podstringově filtrovalo a hrála
  by se jiná sada), řídí `useSlideshow` +
  `useSlideshowSettings`, `total` ze serveru posílá do `Slideshow` (odpočet počítá celou show, ne jen
  načtené stránky), renderuje loading/empty/error stavy nebo `Slideshow`; **vlastní přednačítání
  snímků**: `preloadWindow(index,length)` → URL v `SLIDESHOW_PREVIEW_SIZE` → `useImagePreloader`
  (`prime` v efektu), jehož `statusOf` jde zpátky do `useSlideshow` jako `readiness`, takže
  auto-advance počká, než je další snímek dekódovaný; exit → `navigate(-1)`
  (fallback na zdrojový pohled — album/štítek/`searchHref`/knihovna), takže Zpět funguje,
  `TrashPage` = `/trash` (editor/admin) koš: archivované fotky (`useScopedPhotos`-style listing přes
  `usePaginatedPhotos` scopnutý `archived=only`) v mřížce `TrashCard` s `FilterBar`, **obnova**
  (`unarchivePhoto`) a **trvalé mazání** (`purgePhoto`) jednotlivě i hromadně (`useSelection`
  `SelectionBar`), **Vyprázdnit koš** (`emptyTrash`), každá trvalá akce přes potvrzovací `Modal`;
  `fetchTrashInfo` dotáhne retenci pro odpočet na kartách,
  `DuplicatesPage` = `/duplicates` (editor/admin) kontrola a **řešení** duplikátů: stránkovaný seznam
  skupin (`fetchDuplicates`, „načíst další" přes `next_offset`) v `DuplicateGroupCard`; per skupina
  uživatel vybere keeper a **„Ponechat nejlepší a sloučit"** → `mergeDuplicates(dry_run:true)` spočítá
  náhled, který se ukáže v `MergeConfirmModal` („+3 alba, +2 štítky, +1 osoba · 2 kopie budou
  archivovány"); po potvrzení `mergeDuplicates()` sloučí (keeper zdědí alba/štítky/osoby + doplní gapy,
  kopie do koše — vratné) → skupina zmizí + success alert (`duplicates.merged`), nebo skupinu **odmítne**
  („není duplikát", jen lokálně skryje); chyby přes `duplicates.actionError`/503 „nedostupné", loading
  přes `GridSkeleton`, error s retry; každá karta nabízí **„Porovnat vedle sebe"** → `DupComparePage`,
  protože 224px dlaždice stačí skupinu poznat, ne se v ní rozhodnout,
  `DupComparePage` = `/duplicates/compare?pair=<levá>|<pravá>` (editor/admin, **fullscreen mimo
  `Layout`** jako `/review` — dvě fotky s navbarem okolo jsou dvě moc malé fotky) rozhodnutí „kterou
  z těch dvou": z `fetchDuplicates` (jedna stránka skupin) postaví `buildPairQueue` **frontu dvojic** —
  vícečlenná skupina se porovnává **po dvojicích proti doporučenému keeperovi** (`[K,A,B]` → `(K,A)`,
  `(K,B)`, nikdy `(A,B)`), stránka to říká v `duplicates.compare.groupNote` („Dvojice 1 z 2 v této
  skupině"), žádný člen se nezamlčí; `useComparePair` načte pro aktuální dvojici `fetchPhoto` ×2 +
  `fetchFaces` ×2 (osoby nejsou na fotce, ale na faces endpointu — a „která kopie nese tvou kurátorskou
  práci" je přesně ta otázka, kvůli které stránka existuje); `CompareStage` ukáže obě fotky vedle sebe
  (pod `md` pod sebou) s **jedním sdíleným zoomem** (`useSyncZoom` + `lib/compareZoom`): jeden
  `ZoomView`, obě `<img>` ho renderují, takže se nemůžou rozejít — kolečko zoomuje k kurzoru, tažení
  posouvá, dvojklik přepíná fit ↔ 3×, `?pair=` drží pozici přes reload; `DiffTable` (`buildDiffRows`)
  porovná rozměry+Mpx, velikost, formát, datum, fotoaparát, objektiv, název, místo, alba, štítky, osoby
  a **odliší jen řádky, které se liší** (rámeček + tučně + `visually-hidden` „liší se" — nikdy jen
  barvou), přepínač `duplicates.compare.diff.onlyDifferences` schová shodné; tři akce —
  **Nechat levou/pravou** → `mergeDuplicates(dry_run:true)` → `MergeConfirmModal` s `note`
  (`duplicates.compare.archiveNote`: archivuje se, nemaže) → `mergeDuplicates()` **jen nad tou
  dvojicí** (`member_uids:[keeper,loser]`, ne nad celou skupinou — třetí člen nebyl na obrazovce),
  **Nechat obě** → `dismissDuplicate()` (persistentní, `POST /feedback/duplicate-dismissals`);
  po rozhodnutí se **jde na další dvojici**, ne zpět na seznam (dvojice archivované fotky vypadnou
  přes `dropPairsTouching`), na konci `EmptyState` `duplicates.compare.done`; klávesy `←`/`→`/`b`/`Esc`
  (v `SHORTCUT_GROUPS` jako `shortcuts.groups.compare`), `KeyboardShortcutsHelp` si mountuje sama,
  `OutliersPage` = `/outliers` (editor/admin, odkaz **Možné chyby** v „Nástrojích") „které obličeje
  téhle osoby nejspíš nejsou ona": **protějšek panelu na stránce osoby, který zůstává** — panel je
  správný, když si osobu zrovna prohlížíš, tahle stránka, když chceš cíleně lovit (a panel na ni
  odkazuje přes `/outliers?subject={uid}`, takže dorazí s předvybraným člověkem); `OutlierControls`
  (picker osoby přes `AddAutocomplete` s počtem obličejů v hintu + **procentní** posuvník prahu
  0–100 % step 5 **default 0 = zobrazit vše**, bookendy „Zobrazit vše"↔„Pouze extrémní"; **bez
  tlačítka Hledat** — dotaz je levné indexované čtení, tak výběr osoby prostě ukáže) → `fetchOutliers`
  s `{threshold: outlierThresholdDistance(percent), limit: OUTLIER_LIMIT}`; posuvník je **živý**
  (vidíš seznam se zužovat), ale query se **debouncuje** (`THRESHOLD_DEBOUNCE_MS = 250`) + běží přes
  `AbortController`, jinak by jeden tah vystřelil dotaz na každý krok; config (osoba/práh) v URL,
  do historie se píše až **commitnutá** hodnota (tah v ní neskončí); `OutlierStats` (oskórovaných
  celkem / průměrná vzdálenost / zobrazeno + jednořádkové vysvětlení řazení, **`no_embedding`
  hláška** (obličej rozpoznaný při offline boxu zkontrolovat nejde a v seznamu **není** — říct to
  nahlas, jinak prázdný seznam čte jako „čisto"), capped hláška při `OUTLIER_LIMIT`,
  `meaningful:false` hláška); mřížka **velkých** `OutlierCard` (`minmax(20rem, 1fr)`): **kontextový
  výřez** = bbox zvětšený o 30 % na každou stranu přes `padBbox` + `cropImageStyle`, uvnitř něj
  rámeček obličeje přes `boxWithinCrop` (vše `lib/faceGeometry`, `aspect-ratio` nese geometrii →
  žádné měření pixelů), badge vzdálenosti v **%**, otázka „Je to chyba?" a na ni dvě **opačné**
  odpovědi: **✓ „Ano, odebrat"** → `assignFace` `unassign_person`, **✗ „Ne, je to {{name}}"** →
  `confirmFace` (`services/feedback`) — **pozor na polaritu, není to `rejectFace`**; obě flipnou
  kartu **na místě** (karta nemizí → mřížka se pod kurzorem nepřeskládá); **výběr** přes
  `useSelection` (Shift+klik rozsah, **Ctrl/Cmd+A** bindnuté zvlášť — sdílený hook modifikátory
  ignoruje — a jen když mřížka vlastní stránku, ať nesebere prohlížeči select-all v poli) +
  `SelectionBar` s **hromadným odebráním**, které jde sekvenčně a **přizná částečné selhání**
  (progress + počet chyb, hotové zůstanou hotové); **klávesnice** (`shortcuts.groups.outliers`):
  šipky/`hjkl` posun, `y`/Enter odebrat, `n` potvrdit, `x` vybrat, Esc čistí výběr→zaměření —
  a **zaměření se po verdiktu posune na další nerozhodnutou kartu** (`nextActionableIndex`; reset
  zaměření proto visí na **odpovědi**, ne na pracovním seznamu, který se mění každým verdiktem —
  jinak by se posun po každém rozhodnutí zahodil); stavy idle („vyber osobu")/loading/error/
  prázdno („nic podezřelého, sniž práh"); testy `OutliersPage.test.tsx` + `lib/outlierReview.test.ts`,
  `ReviewPage` = `/review` (editor/admin, top-level odkaz **Třídění** hned vedle Nahrát) **hra na
  třídění**: jedna otázka („Je na fotce **Tomáš Kozák**?" / „Sedí k fotce štítek **Ostatky**?")
  přes **celou obrazovku** — stránka je **mimo `Layout`** (bez navbaru, jako `/slideshow`), protože
  o pozornost nemá soupeřit nic než fotka; pořadí je **otázka nad fotkou** (header/progress →
  otázka + hint + jistota → fotka → akce) a celé se to **vždy vejde do viewportu**: nescrolluje se
  na výšku ani na šířku, na krátkém displeji (ležící telefon) se zmenší **fotka** — text a tlačítka
  vyhrávají, k Ne/Nevím/Ano se nikdy nemusí scrollovat; stav řídí `useReviewGame`, fotku kreslí `ReviewPhoto`
  (`REVIEW_PREVIEW_SIZE = fit_1280`, tedy **celý snímek**, ne čtvercová dlaždice — bbox je relativní
  k plnému rámu; rámeček obličeje přes `padBbox`+`faceBoxStyle` z `lib/faceGeometry` s **~30 %
  polstrováním**, protože z těsného výřezu obličej nepoznáš, + jemné ztmavení okolí), otázku
  `QuestionText` (`Trans` s `<strong>` kolem jména/štítku — i18n **šablona**, ne skládání řetězců)
  a jistotu `ConfidenceHint` (tlumené % + proužek: kontext, ne odpověď); tři akce **Ano · Ne ·
  Nevím** jsou skutečná tlačítka (velká, dole, palcem dosažitelná na dotyku), **klávesnice je ale
  primární rozhraní**: `→`/`y` ano, `←`/`n` ne, **mezerník**/`↓` nevím, `z` i **Ctrl/Cmd+Z** undo
  (chord se váže mimo `useKeyboardShortcuts`, ten modifikátory ignoruje záměrně), `Esc` konec (nechá
  `Esc` otevřenému modalu nápovědy) — vše registrované v `?` overlayi přes
  `shortcuts.groups.review`; odpovědi jsou **optimistické** (UI jede dál, request doběhne vzadu) a
  další karta je **vždy už v paměti** (`useReviewGame` refilluje na pozadí, `useImagePreloader`
  dekóduje `PRELOAD_AHEAD = 4` fotky dopředu), takže mezi kartami **nikdy nebliká spinner**;
  neuložená odpověď se neztratí — sedí v alertu s **Uložit znovu**/**Zahodit**, undo má vlastní
  alert s retry; sezení ukazuje **počítadlo zodpovězených + zbývajících** a tenký progress proužek
  (žádné skóre, streaky ani konfety — odměna je uklizená knihovna); stavy: **prázdná knihovna**
  (`no_people_no_labels` → „nejdřív pojmenuj lidi / založ štítky" s odkazy na `/people` a
  `/labels`) je **odlišená od prázdné fronty** (`no_candidates` → „vše posouzeno" + Zkusit znovu),
  plus loading prvního batche a **offline/chyba** s retry; testy `ReviewPage.test.tsx` (polstrovaný
  bbox, jméno/štítek v otázce, →/←/mezerník posílají správný verdikt a posouvají, **žádný fetch
  mezi kartami uvnitř batche**, undo přes správný inverzní endpoint, selhaná odpověď neztratí
  místo, oba prázdné stavy odlišně),
  `NotFoundPage`),
  `components/savedsearch/` = `SaveSearchModal` (modal pro pojmenování při uložení nového pohledu
  i přejmenování existujícího uloženého hledání) + `SavedSearchesDropdown` (dropdown v hlavičce
  `SearchPage` — **ne v navbaru**; lazy fetch při otevření, položky otevírají uložený pohled přes
  `savedSearchHref`, „Spravovat" → `/saved`, loading/empty/error stavy uvnitř menu);
  `components/search/` = `GlobalSearchSections` (kompaktní cross-entity sekce nad photo mřížkou
  search stránky: přes `useGlobalSearch(query)` natáhne grouped `GET /search/global` a vyrenderuje
  chipy shodných **alb/lidí/štítků** odkazující na entitu; nezávislé na photo fulltext/semantic
  hledání pod ním, nerendruje nic dokud nepřijde aspoň jedna nefotková shoda — prázdný dotaz /
  probíhající hledání / jen-fotky shoda nepřidá žádné chrome);
  `components/trash/` = `TrashCard` (dlaždice archivované fotky: náhled + odpočet do auto-purge přes
  `trashCountdown` + restore/delete akce + výběr v selection módu);
  `components/duplicates/` = `DuplicateGroupCard` (karta skupiny: členové vedle sebe s náhledem/
  rozměry/velikostí/`taken_at`/vzdálenostmi, radio výběr keepera (default navržený), badge `reason`,
  akce **Ponechat nejlepší a sloučit** (`onResolve` → náhled) / **Není duplikát**, busy stav) +
  `MergeConfirmModal` (potvrzovací dialog: shrnutí co se přesune na keepera + kolik kopií se archivuje,
  volitelný `note` pod tím — `DupComparePage` jím říká, že se kopie archivuje a nemaže, Potvrdit/Zrušit,
  busy spinner) + `CompareStage` (dvě fotky vedle sebe, pod `md` pod sebou; obě renderují **týž**
  `SyncZoom.view`, takže zoom je synchronní z konstrukce; kurzor `zoom-in`/`grab`/`grabbing` říká,
  co gesto udělá; viewport klipuje, `object-fit: contain` nikdy neořízne) + `DiffTable` (rozdílová
  tabulka: řádek, který se liší, je označený **rámečkem + tučně + `visually-hidden` „liší se"** —
  nikdy jen barvou; `onlyDifferences` schová shodné, prázdná hodnota je „—", vše shodné → hláška
  místo tabulky) + `compare.css`;
  `components/expand/` = `ExpandSearchForm` (config panel `/expand`: přepínač Album|Štítek,
  `AddAutocomplete` picker sbírky s počtem fotek v hintu, procentní posuvník prahu s bookendy,
  limit, submit tlačítko Hledat — čistě controlled, stav drží stránka) + `ExpandResults`
  (summary řádek s vote-rule vysvětlením nad `PhotoGrid`; per-dlaždicové overlaye přes `tileExtras`:
  badge % podobnosti (`pe-none`), badge počtu shod při `match_count > 1`, ✗ tlačítko jen když
  volající dodá `onReject`; po vyprázdnění mřížky uživatelem hlášky „vše zpracováno");
  `components/review/` = `ReviewPhoto` (stage hry na třídění: **celý rám** fotky v
  `REVIEW_PREVIEW_SIZE` (`fit_1280`, **exportováno** — stránka přednačítá přesně tuhle URL) tak
  velký, jak dovolí **místo zbylé pod otázkou**; rám je **width-driven** přes `aspectRatio` +
  `maxWidth: min(100%, calc(100cqh * ratio))`, kde `100cqh` je **skutečná** výška stage (je to
  `container-type: size` kontejner) — rám se tedy stropí o reálný zbytek sloupce, **ne o odhad**,
  drží proto přesný poměr a normalizovaný bbox sedí **bez měření pixelů**; `displayAspect` počítá
  poměr v **display** (EXIF-orientovaném) prostoru — orientace 5–8 prohazují šířku/výšku —,
  fallback 3:2, ať stage nikdy nezkolabuje; rámeček obličeje = `padBbox` (~30 %) → `faceBoxStyle`,
  `pointer-events: none` + `aria-hidden`, okolí jemný dim; rozbitý náhled degraduje na ikonu, nová
  fotka flag resetuje) + `review.css` (fullscreen **flex sloupec** `review-game`: top bar /
  progress / **otázka** / stage / akce — text **nad** fotkou; stage je `flex: 1 1 0` +
  `container-type: size` + `overflow: hidden`, takže jeho výška **je** zbytek po chrome (basis 0 →
  fotka uvnitř nemůže nic vytlačit) a přetečení fotky na text je **strukturálně nemožné**, ať
  chrome naroste čímkoli — alert, zalomené dlouhé jméno, `pointer: coarse` tlačítka; `@media
  (max-height: 500px)` utahuje paddingy na **ležícím telefonu** (široký → žádný width dotaz ho
  nechytí, a přitom má nejmíň místa) a `clamp(…, min(3.5vw, 5dvh), …)` u otázky drží totéž pro
  písmo; `review-photo__box` rámeček, progress proužek, `kbd` odznaky, dotyková varianta akcí);
  `components/slideshow/` = `Slideshow` (prezentační fullscreen stage: aktuální fotka v preview
  velikosti `SLIDESHOW_PREVIEW_SIZE` (`fit_1920`, **exportováno** — stránka musí přednačítat přesně
  tuhle URL), ovládání předchozí/play-pause/další/fullscreen/nastavení/zavřít + titulek +
  **postup** (`slideshow.progress` → „snímek 7 ze 40"; počítá se proti `total` ze serveru, ne proti
  načteným stránkám — zbývající čas už tady není); klávesy ←/→ / mezerník / Esc / F
  a dotykový swipe; Fullscreen API feature-detected;
  panel nastavení = výběr efektu + rychlosti a **vedle rychlosti odhad zbývajícího času**
  (`slideshow.remaining` → „zbývá 2 min 45 s"; `slideshowRemainingMs(index, total, intervalMs)` — sleduje
  index i zvolenou rychlost, takže odpočítává a hned reaguje na změnu rychlosti, drží se `total` ze
  serveru (bez blikání při stránkování) a při pauze zamrzne; mizí s koncem promítání);
  efekt **`kenburns`** navíc zapisuje na `<img>` inline
  `--kb-*` custom properties z `lib/kenBurns` (endpointy transformu + `--kb-duration` = interval) —
  aktivuje se **jen pro obrázky**, video snímek a uživatel s `prefers-reduced-motion`
  (`usePrefersReducedMotion`) dostanou statický snímek bez animace) + `slideshow.css` (keyframes
  `slideshow-fade`/`slideshow-slide`/`slideshow-kenburns` (`object-fit: cover`, `var()` se dosadí
  před interpolací, takže se oba transformy interpolují jako shodný `translate() scale()` seznam),
  `@media (prefers-reduced-motion: reduce)` jako druhá pojistka, fullscreen layout)
  + `SlideshowStart` (**sdílené** tlačítko Promítání pro knihovnu / album / štítek / hledání:
  jen `slideshowHref(scope,view)`. **Žádný odhad délky před spuštěním** — přesunul se do přehrávače
  vedle rychlosti, kde sleduje průběh; `count` prop grid pořád posílá (má ho z `total`), ale
  komponenta ho nerenderuje);
  `components/map/` = `LeafletMap` (imperativní Leaflet most: dlaždicová vrstva na **backend
  proxy** `/api/v1/map/tiles/{mapset}/{z}/{x}/{y}{r}` (klíč server-side, `{r}`→`@2x` na retině),
  **povinné mapy.com prvky** — attribution „© Seznam.cz a.s. a další" → `/copyright` a klikatelné
  **logo** vlevo dole → `mapy.com`; `leaflet.markercluster` shluky (klik přibližuje), markery
  z GeoJSON, popup s náhledem → detail fotky; jednorázový setup, výměna URL dlaždic při změně
  mapsetu, přestavba markerů při změně fotek, fit-bounds na markery; volitelný **`onTileError`**
  prop dostane URL dlaždice, kterou se nepodařilo načíst (Leaflet `tileerror`), aby rodič mohl
  zjistit **proč** — fire per dlaždici, rodič debouncuje), `MapFilterBar` (přepínač
  podkladu basic/outdoor/aerial + datum od/do, archiv, soukromé, počet, zrušit filtry);
  `components/people/` = `SubjectTile`/`SubjectPhotoTile`/`SubjectEditModal`,
  `FaceCrop` (**preferovaný** výřez obličeje: `<img>` s `fit_*` zdrojem z `lib/faceSource.ts`
  `faceSourceSize` (celý rám — `tile_*` je centrovaný čtverec, na kterém by výřez minul obličej;
  velikost se **škáluje podle toho, jak malý obličej je**: pevná by dala 13px šmouhu místo
  člověka u obličeje přes 2 % rámu, žebřík 720/1280/1920 se zastavuje u 1920, protože dál už ty
  pixely v originále nejsou) v `overflow:hidden` kontejneru,
  `cropImageStyle` v %, `aspect-ratio` ze skutečných pixelových proporcí výřezu → **nic se
  nedeformuje**; `size` = pevná šířka v px, jinak vyplní rodiče (`w-100 h-100`); `label=""` =
  dekorativní, když jméno stojí vedle. Potřebuje rozměry rámu),
  `FaceThumb` (**legacy** čtvercový výřez přes `faceCropStyle` — deformuje a čte `tile_*`; zůstává
  jen pro cluster preview, jejichž payload rám nenese),
  `FaceOverlay`+`FacesPanel`+`FaceAssignPanel` (`FaceOverlay` = **čistě prezentační** průhledná vrstva
  klikatelných boxů z normalized bbox přes `faceBoxStyle`, **žádný vlastní obrázek ani fetch** —
  mountuje se jako poslední dítě `position-relative` obalu těsně kolem `<img>`; vrstva je
  click-through, pointer events chytají jen boxy (a při `readOnly` ani ty; číslo a jmenovka boxu mají
  `pointer-events:none`, jinak by ukradly klik a rozbily swipe). Data + stavový automat pojmenování
  drží hook `useFaces`. **`FacesPanel`** = pravý panel vedle fotky, jediné místo, kde se přiřazuje:
  **textové řádky** `Obličej #N` + barevný chip stavu (žádné výřezy — jeden obrázek na stránku),
  klik vybere/odvybere, hover se zrcadlí s boxem; pod vybraným řádkem se rozbalí `FaceAssignPanel`
  (`key={face_index}` → reset stavu při změně výběru). **`FaceAssignPanel`** = top-3 návrhy
  (`{jméno} · {confidence}%`, one-tap) + typeahead nad `useSubjects` (`AddAutocomplete` s `autoFocus`
  a `hint` = počet fotek osoby); u přiřazeného obličeje **Přeřadit** (návrhy, které backend dodává
  i pro přiřazené — vlastní osoba je z nich vyloučená) a **Odebrat**; Esc vyskočí nejdřív z přeřazení,
  pak z výběru), `ClusterCard`, `Outliers` (žebříček podezřelých obličejů s one-tap unassign na
  stránce osoby + odkaz **Projít všechny** na `/outliers?subject={uid}`, kde je plná sweep verze),
  `OutlierCard`/`OutlierControls`/`OutlierStats` (stavební bloky `/outliers`: karta s **kontextovým
  výřezem** (30 % kolem bboxu, `padBbox`+`cropImageStyle`+`boxWithinCrop`), otázkou „Je to chyba?"
  a dvěma opačnými verdikty (✓ odebrat / ✗ potvrdit), výběrovým checkboxem a focus ringem; config
  strip s pickerem osoby a procentním prahem; statistiky včetně **`no_embedding`** hlášky);
  `auth/` (`AuthContext`/`useAuth` + `AuthProvider` = boot `GET /auth/me`,
  vystavuje `user`/`role`/`login`/`logout`/`refresh`/`canWrite`/`isAdmin`/`canImport`; `ProtectedRoute` =
  `RequireAuth` + `RequireRole` + `RequireImport` route guardy), `hooks/` (`usePaginatedPhotos` = sdílený
  paginovaný infinite-scroll loader nad libovolným `PageFetcher`: akumuluje stránky,
  `loadMore`/`retry`, reset+refetch při změně dotazu/`key`/`enabled`, ruší in-flight requesty
  a ignoruje stale odpovědi, vystavuje i `mode`/`degraded`; `enabled:false` → `idle` stav bez
  requestu; `usePhotoLibrary(params,{reloadKey?})` = tenká obálka nad ním nad `fetchPhotos`
  (`reloadKey` refetchne mřížku po mutaci, stejně jako u `useScopedPhotos`); `usePhotoSearch(params,mode,{reloadKey?})` =
  obálka nad `searchPhotos` s injektovaným `mode`, vypnutá při prázdném `q` (idle), `reloadKey`
  přehraje hledání po mutaci;
  `useUploadQueue` = fronta uploadu: `addFiles` (dedup jméno+velikost+mtime)/`removeItem`/
  `start`/`retry`/`retryFailed`/`clear`, konkurenční strop `MAX_CONCURRENT_UPLOADS` (3),
  per-file status+progress, souhrn počtů + `progress` (**celková** frakce dávky 0–1 vážená
  částečným progressem běžících souborů, terminální soubory = hotové → plynulý overall bar),
  `createdUids` (jen nové) pro odkaz do knihovny
  a `resolvedUids` (nové **i** duplicitní fotky) pro pouploadové přiřazení; auto-drainuje
  frontu efektem po `start`/retry, ruší běžící uploady při unmountu;
  `useUploadOrganize` = výběr alb/štítků pro celou dávku uploadu + jejich přiřazení: načte katalogy
  alb a štítků (`fetchAlbums`/`fetchLabels`), drží výběr (inline vytvoření jako `create:` marker
  jako v `BulkEditModal`, sdílené helpery `lib/pendingCreate`), `runAssign(uids)` nejdřív založí
  čekající alba/štítky a pak jedním `POST /photos/bulk` (`add_to_albums`+`add_labels`) přiřadí;
  stav `idle`/`assigning`/`done`/`error`, `retryAssign` re-poslání téže dávky, `resetAssign`;
  `useSubjectPhotos(uid,{reloadKey?})` = obálka nad `usePaginatedPhotos` nad
  `GET /subjects/{uid}/photos` (galerie osoby, reset+reload při změně `uid` nebo `reloadKey`); `useScopedPhotos` = obálka nad `usePaginatedPhotos`
  nad `GET /photos` scopnutým na album/štítek/**lokalitu** (`PhotoScope` `{album?,label?,country?,city?}`
  + filtry/sort z URL, options `{reloadKey?,enabled?}` — `reloadKey` pro refetch po mutaci, `enabled:false`
  → idle bez fetche, např. Places před výběrem města); `useMapPhotos` = jednorázový (nestránkovaný) loader
  GeoJSON feedu geotagovaných fotek nad `fetchMapPhotos` (`status` loading/ready/error, `retry`,
  ruší in-flight + ignoruje stale při změně filtrů);
  `useJobStats(enabled)` = poller stavu fronty jobů nad `fetchJobStats` (`GET /jobs/stats`) pro badge
  v patičce: fetchuje **jen když `enabled`** (admin), refetch po ~30 s, **pauzuje při skryté záložce**
  (`visibilitychange`/`document.hidden`) a při návratu hned refreshne; selhání spolkne a vrátí `null`
  (badge se skryje), na unmountu/`enabled→false` ruší timer i in-flight request — nic ho nepřežije;

  `useLibraryFacets(params)` = loader nabídek facetů knihovny → `LibraryFacets{years,albums,labels,subjects}`:
  roky přes `fetchPhotoYears` **refetchuje při změně filtrů** (rok drží méně fotek, jakmile přibude
  štítek), ale **`year` z requestu strhává** (backend ho stejně ignoruje — facet nesmí zúžit vlastní
  nabídku — a bez něj zůstane request identický, takže přepínání let nerefetchuje); alba, štítky a
  subjekty (osoby, přes `fetchSubjects`) jsou katalogové, načtou se **jednou**. Neúspěch nechá ten seznam **prázdný** místo chyby (facet,
  který nemá co nabídnout, je degradovaný bar, ne rozbitá stránka — chyby načtení hlásí mřížka);
  in-flight requesty ruší `AbortController` při změně `params`/unmountu, takže pomalá odpověď
  nepřepíše novější (`params` si volající memoizuje z URL stavu); `useTimeline(params)` = jednorázový loader
  měsíčního date-histogramu nad `fetchTimeline` (`buckets`/`total`/`status`, refetch při změně
  filtrů, ruší in-flight + ignoruje stale — podklad `TimelineScrubber`); `useGlobalSearch(query,
  debounceMs?)` = debouncovaný (default 250 ms) grouped global-search loader nad `globalSearch`
  (`status` idle/loading/ready/error + `result`, prázdný dotaz → idle bez requestu, ruší in-flight +
  ignoruje stale — podklad `GlobalSearchSections`); `usePlaceSearch(query,debounceMs?)` =
  debouncovaný (default 300 ms) loader našeptávače míst nad `searchPlaces` (`status`
  idle/loading/ready/**error**/**unavailable** + `places`, ruší in-flight + ignoruje stale —
  podklad `PlaceSearch`); zrcadlí `useGlobalSearch` s dvěma rozdíly, které plynou z toho, že
  lookup **stojí kredit**: dotaz kratší než 2 znaky je `idle` **bez requestu** (jedno písmeno není
  název místa, jen klávesa na cestě k němu) a statusy 424/502/503 dostanou vlastní stav
  `unavailable` (rozbitá je strana poskytovatele, opakovat nemá smysl) proti `error` (zbytek,
  vč. 429 — zkusit znovu dává smysl); `useGridJump({gridRef,
  loadedCount,hasMore,loadingMore,loadMore})` = vrátí `jumpTo(index)`, který skočí mřížkou na foto
  index přes `VirtuosoGridHandle.scrollToIndex` a **nejdřív donačte stránky**, když cíl leží za
  infinite-scroll kurzorem (nebo clampne na poslední načtené, když už další stránky nejsou) —
  podklad skoku časové osy na měsíc před načtenou částí; `useSelection` = multi-výběr fotek v mřížce
  (`active`/`selected`/`count`/`enable`/`disable`/`toggle`/`selectMany` (select-all-in-view)/`clear`);
  poslední `toggle` drží **kotvu** a `toggleRange(uid, orderedUids)` (Shift+klik) vybere souvislý
  rozsah mezi kotvou a `uid` — jen **přidává**, bez kotvy nebo s kotvou mimo pořadí degraduje na
  `toggle`, `clear`/`disable` kotvu shodí;
  `useBulkEdit({onEdited?})` = **znovupoužitelná hromadná úprava** libovolného foto-seznamu:
  `useSelection` + role gate (`canBulkEdit` = `canWrite`) + stav dialogu
  (`editing`/`open`/`close`/`finish`), k tomu `photoUids` (**přesně vybrané**, nikdy celý filtrovaný
  výsledek) a `gridSelection` rovnou do `PhotoGrid` (vč. `onToggleRange` → Shift+klik rozsah zdarma
  v každé mřížce). `finish(outcome?)` = zavřít dialog → `selection.clear()`
  → `onEdited(outcome?)` (refetch; `outcome` = `BulkEditOutcome` pro stránky, které umí seznam
  upravit na místě — `/expand`); režim výběru přežije, takže po úspěchu jde hned vybírat dál a žádné
  zastaralé UID v něm nezůstane. Neúspěšný apply výběr **nechá být**. Stránka wiruje jen
  `gridSelection` a `SelectionStart`, zbytek obstará `BulkEditControl`;
  `useReloadKey()` = `[key, reload]`, string čítač do `reloadKey` foto-seznamu — jedno `reload()`
  resetne seznam a natáhne ho od první stránky; `reload` je stabilní, jde rovnou do
  `useBulkEdit({onEdited})`;
  `useKeyboardShortcuts(handlers,{enabled?})` = sdílené plumbing všech klávesových zkratek: jeden
  document-level `keydown` listener dispatchuje dle normalizovaného `shortcutToken(event.key)` na
  `handlers` (přes refy, bind jednou a vždy vidí aktuální closury), matched key `preventDefault`;
  **nikdy nevystřelí** při držení Ctrl/Meta/Alt, při psaní (`isTypingElement`) ani při otevřeném
  form-modalu (`isFormModalOpen`); `useGridKeyboardNavigation({count,enabled,resetKey,getColumns,
  scrollToIndex,onOpen,onToggleSelect,onToggleFavorite,hasSelection,onClearSelection})` = navigace
  mřížky nad `useKeyboardShortcuts`: drží `focusedIndex` (zvýraznění), šipky + `j`/`k`/`h`/`l` posouvají
  (vlevo/vpravo o 1, nahoru/dolů o řádek dle živého počtu sloupců) a dorolují dlaždici do view, `Enter`
  otevře, `x` vybere (zapne selection mód), `f` přepne oblíbenou, `Escape` zruší nejdřív výběr, pak
  fokus; fokus se resetuje na `resetKey` (nová filtr/sort/scope);
  `useSwipeNavigation({onSwipe,enabled?,threshold?})` → `{onTouchStart,onTouchMove,onTouchEnd}` =
  horizontální **swipe na dotyku → prev/next** na obrázku detailu; čte jen start/konec doteku a
  **nikdy nedělá `preventDefault`**, takže mostly-vertikální tah propadne nativnímu scrollu (rozhoduje
  `lib/gestures` `swipeAction`: práh + dominantní vodorovná složka). Gesto se zahodí při druhém prstu
  (pinch) a když **začne na interaktivním prvku** (`button`/`a`/form) bez `data-swipe-surface` — takže
  ťuknutí na obličejový box/šipku nelistuje, jen samotný obrázek (jeho tlačítko ten atribut nese). Myš
  na desktopu sem nechodí, gesto je čistě aditivní pro dotyk;
  `useSyncZoom({resetKey})` → `{view,zoomed,dragging,handlers,zoomIn,zoomOut,reset}` = **jeden**
  zoom/pan stav pro **obě** fotky v `DupComparePage`: obě `<img>` renderují týž `ZoomView`, takže
  jsou synchronní **z konstrukce** — není co kopírovat mezi panely, není kde se rozejít. Kolečko
  zoomuje k kurzoru, tažení posouvá (jen když je přiblíženo), dvojklik přepíná fit ↔ 3×, změna
  `resetKey` (id dvojice) vrátí fit, takže další dvojice nezdědí přiblížení. **Není to
  `usePinchZoom`:** ten je touch-only a měří proti `window` (obrázek vyplňuje viewport), tady jde
  o myš ve dvou půlkách obrazovky, takže box se předává dovnitř; čistá matematika je v
  `lib/compareZoom`,
  `useComparePair(pair)` → `{data,loading,error}` = načte obě strany porovnání (`fetchPhoto` ×2 +
  `fetchFaces` ×2, paralelně, `AbortController`); selže-li kterákoli, selže celá dvojice — půlka
  diff tabulky by lhala mlčením,
  `usePinchZoom({onSwipe,resetKey,enabled?})` →
  `{scale,translateX,translateY,isZoomed,gesturing,handlers,reset}` = **pinch/dvojklik zoom** fullscreen
  lightboxu s **pan** při přiblížení a swipe listováním v klidu: dva prsty škálují (`pinchScale`, clamp
  `[1,4]`), **dvojklik** přepíná fit ↔ `DOUBLE_TAP_SCALE` (zoom k místu ťuknutí), tah přiblíženého
  obrázku panuje (clamp `clampPan`, aby nevyjel z obrazovky), tah v klidu rozhodne swipe (`swipeAction`);
  **zoom se resetuje při změně `resetKey`** (zobrazená fotka) a zavřením (lightbox se odmountuje). Povrch
  má `touch-action:none`, takže `preventDefault` není potřeba a prohlížeč gesto nepřebíjí;
  `useFaces(photoUid)` = načte obličeje fotky (`fetchFaces`) a drží stavový automat pojmenování
  (výběr boxu, optimistické přiřazení, refetch smiřující se serverem, `busy`/`actionError`);
  vytažen z `FaceOverlay`, aby detail mohl kreslit boxy nad svým jediným obrázkem a panel
  pojmenování renderovat jinde na stránce. **Po načtení vybere první nepojmenovaný obličej**
  a **po přiřazení posune výběr na další nepojmenovaný** (`firstUnnamed`/`nextUnnamed`, řadí dle
  **pořadí v poli**, ne `face_index`; `facesRef` proti stale closure) — skupinovou fotku tak projedeš
  bez sahání po myši. `unassign` výběr **nechá** (obličej se právě uvolnil a typicky ho hned
  přejmenováváš). Smiřovací refetch po mutaci auto-výběr **nespouští** (`reload(signal, autoSelect)`),
  jinak by pojmenování posledního obličeje odskočilo zpátky nahoru;
  `useSubjects()` = líný seznam všech subjektů pro typeahead (mountuje se až s `FacesPanel`,
  takže prohlížení fotky ho nikdy nezaplatí; chyba = prázdný seznam, pole pak jen zakládá nové);
  `useCandidateReview(subjectUid,candidates)` = stavový stroj review mřížky `/faces`: naseeduje
  pracovní seznam z čerstvého hledání a aplikuje ✓/✗ **optimisticky** (mřížka se nereloadne);
  `confirm` překlopí kartu na `done` a zavolá `assignFace` (chyba → `error` k retry, sousedů se
  nedotkne), `reject` kartu odebere + `rejectFace` (při chybě vrátí zpět), `confirmAll(tab)` projde
  akční karty jedné záložky sekvenčně s `confirmAllState` `{running,current,total,failed}`,
  zrušitelně (`cancelConfirmAll`), částečné selhání neroluje zpět a nahlásí přes `actionError`;
  `useSweepReview()` = orchestrátor `/recognition` sweepu (multi-osoba varianta review): streamuje
  přes `streamSweep`, sbírá jednu `PersonState` na osobu s matchi jak přicházejí (`progress`/`person`/
  `summary`), `confirm`/`reject`/`confirmAllForPerson` aplikuje **optimisticky** stejnými pravidly
  (`buildAssignRequest`/`buildRejection` z `candidateReview`), `people` vrací jen osoby s akčními
  kartami (osoba zmizí, když se vyřídí poslední); `cancel`→`AbortController`, jeden `confirmAll` běží
  naráz; nikdy neautoconfirmuje;
  `useOutlierReview(subjectUid,faces)` = stavový stroj mřížky `/outliers`: naseeduje pracovní seznam
  z čerstvého dotazu a aplikuje oba verdikty **optimisticky a na místě** — karta flipne, kde stojí,
  mřížka se nereloadne a scroll neuteče kurátorovi uprostřed dlouhého seznamu. Verdikty jsou
  **opačné a míří na opačné endpointy**: ✓ `unassign` odpojí osobu přes běžný assign automat,
  ✗ `confirm` zapíše **trvalé potvrzení** (`confirmFace`), které backend z dalších outlier dotazů
  vyloučí — seznam, co dokola nabízí tytéž plané poplachy, je přesně ten problém, co tahle stránka
  řeší. Selhaný zápis označí **vlastní** kartu `error` a sousedů se nedotkne. `unassignMany` jde
  výběrem **sekvenčně** a **přizná částečné selhání** (`bulkState{running,current,total,failed}`,
  cancelovatelné): už odebrané zůstanou odebrané, chyby se spočítají a řeknou, nerollbackují se
  ani nespolknou. Nové `faces` (jiná osoba/práh) resetují vše a opustí běžící run;
  `useReviewGame()` = engine hry na třídění (`/review`): lokální fronta otázek plněná **na pozadí**
  (`fetchReviewQueue`; refill jakmile klesne na `REFILL_AT = 3`, deduplikace proti **všem** už
  viděným id, takže hranice batche je neviditelná), **optimistické** odpovědi (`answer` posune UI
  hned a request doběhne vzadu; selhání spadne do `failed` k explicitnímu retry — nikdy neblokuje
  rytmus ani tiše neztratí verdikt) a **jednokrokové undo**. Fronta má **zdroj pravdy v refu**, ne
  ve stavu: dvě odpovědi se vejdou do jednoho renderu (šipky v rychlosti) a čtení hlavy ze stavu by
  tutéž kartu zodpovědělo dvakrát. `undo` jde přes **inverzní** zápisové cesty (`unassign_person`,
  DELETE feedback-rejection, detach štítku), protože `POST /review/answer` je **idempotentní per
  otázka** — a ze stejného důvodu se **znovu**-odpověď na vrácenou otázku posílá přímými cestami
  (`sendDirect`), jinak by no-opla jako `already_answered`; undo nejdřív **počká na in-flight**
  request, aby inverze nepředběhla odpověď, kterou vrací, a `create_marker`-ano dohledá vzniklý
  marker přes `fetchFaces`, takže případné pozdější re-ano je `assign_person` na **týž** marker,
  ne duplikát;
  `useFavorite(uid,initial)` = **optimistický** per-user favorite toggle nad `favoritePhoto`
  (`PUT`/`DELETE …/favorite`), rollback při chybě, ignoruje souběžný toggle, resync na změnu
  `uid`/server stavu; `useRating(uid,initialRating,initialFlag)` = **optimistické** per-user
  hodnocení (hvězdy) + pick/reject flag nad `ratePhoto` (`PUT …/rating` jen s měněným polem),
  `setRating`/`setFlag` s per-poli rollbackem při chybě, no-op na shodnou hodnotu, `pending` přes
  in-flight counter, resync na změnu `uid`/server stavu (mirror `useFavorite`);
  `useThumbSrc(uid,thumbUrl)` → `{src,failed,onError}` = **odolnost vůči expirované podepsané URL**:
  `thumb_url` v payloadu může být krátkodobě podepsaná adresa media Workeru (default 1 h), takže
  payload držený ve virtualizovaném seznamu nebo přečkaný přes delší nečinnost dá `<img>` adresu,
  kterou Worker odmítne. První `onError` proto **jednou** refetchne fotku (`fetchPhoto`) a zkusí to
  s čerstvě podepsanou URL; druhý pád, selhaný refetch, prázdná nebo **nezměněná** adresa (to dělá
  filesystem backend — jeho URL jsou routy a nestárnou, takže pád = fakt chybějící náhled) → `failed`
  a volající vykreslí placeholder. Nová `thumbUrl` prop (nová stránka výsledků) resetuje retry budget.
  Řeší se to takhle, **ne dlouhým TTL** — krátká životnost je celý smysl podpisu. Používá
  `PhotoTile` a `TrashCard`;
  `useSlideshow({length,hasMore,intervalMs,autoPlay?,onLoadMore?,readiness?,maxHoldMs?})` = řízení
  promítání: vlastní `index`+`playing`+`holding`, `next`/`prev`/`play`/`pause`/`toggle`/`goTo`,
  wrap-around, prefetch `PRELOAD_AHEAD` stránek dopředu
  přes `onLoadMore` (na konci s další stránkou počká místo zacyklení), prázdná sada = no-op, clamp
  indexu při zmenšení sady. **Auto-advance je hlídaný `readiness(index)`**: uplynulý interval
  nepřepne slide, ale spustí *hold* — přepne se v okamžiku, kdy je další snímek `ready` (dekódovaný),
  po `maxHoldMs` (default `MAX_HOLD_MS` = 10 s) přepne tak jako tak, a slide s `error` **přeskočí**
  (rozbitý snímek show neblokuje). Manuální nav a pauza hold zruší (manuál nikdy nečeká, resume
  začne čerstvý interval), interval se dá měnit **během holdu** bez restartu/zdvojení timeru
  (timer se během holdu nearmuje, deadline holdu nezávisí na `intervalMs` ani na `readiness`).
  Sada < 2 snímků ani nedrží, ani nepřepíná. `preloadWindow(index,length)` = indexy k přednačtení
  (`PRELOAD_AHEAD` dopředu, `PRELOAD_BEHIND` dozadu, aktuální první, offsety **wrapují** →
  na konci show jsou první snímky připravené na wrap-around, u malé sady se dedupuje);
  `useImagePreloader()` → `{statusOf(url),prime(urls)}` = přednačítá okno obrázků a hlásí
  `pending`/`ready`/`error`. `prime(urls)` je **celé okno** — cokoli mimo se hned uvolní
  (`removeAttribute('src')` = abort in-flight fetche), poslední okno se uvolní na unmountu, takže
  dlouhá show nekumuluje dekódované bitmapy. Readiness měří **`img.decode()`**, ne `onload`: onload
  znamená „bajty dorazily", dekódování by teprve proběhlo při prvním paintu (přesně ten záblesk
  prázdné plochy, kvůli kterému to celé je); `decode()` je feature-detected (jsdom ho nemá →
  fallback na `onload`/`onerror`). Pozdní `decode()` už uvolněného obrázku se ignoruje. Statusy žijí
  ve stavu → `statusOf` mění identitu při každém dosednutí, takže na něm jde záviset efektem;
  `useSlideshowSettings` = persistentní efekt+rychlost přes
  `lib/slideshowSettings` (read once on mount, setteri zapisují do localStorage, sanitizace);
  `useGridDensity()` → `{density,setDensity}` = hustota foto-mřížky (1–8 sloupců nebo `'auto'`)
  přes `useSyncExternalStore` nad `lib/gridDensity`. localStorage je **jediný zdroj pravdy** (žádná
  in-memory kopie): snapshot je primitivum (počet sloupců, `'auto'`, nebo `null` = nic uloženo),
  takže Reactí `Object.is` porovnání nikdy nezacyklí. **Dokud si uživatel hustotu nezvolí**, je
  efektivní hodnota viewport-aware — `defaultDensityForViewport` přes sdílený `useIsNarrowViewport`:
  na telefonu **jedna fotka na řádek** (1 sloupec), na širším displeji `auto`; jakmile je volba
  uložena, vyhrává na **všech** viewportech (per-device preference vždy přežije).
  `subscribe` poslouchá i `storage` event → všechny záložky na zařízení drží stejný počet sloupců;
  `setGridDensity` sanitizuje, zapíše a překreslí **všechny** mřížky naráz, bez contextu a bez
  providera (takže i testy stránek fungují bez wrapperu);
  `useIsNarrowViewport()` = sdílený hook nad `matchMedia` (`(max-width: 767.98px)`, Bootstrap `md`;
  odebírá `change`, chybějící/rozbité `matchMedia` → „široký"; jeden zdroj pravdy pro offcanvas
  filtrů i výchozí hustotu mřížky);
  `usePrefersReducedMotion()` = sleduje `(prefers-reduced-motion: reduce)` přes `matchMedia`
  (odebírá `change`, chybějící/rozbité `matchMedia` → `false`) — volající dekorativní animaci
  **vynechá**, ne zkrátí)),
  `lib/` (`gestures.ts` = **pure, DOM-free rozhodovací helpery dotykových gest** sdílené
  `useSwipeNavigation`/`usePinchZoom` (a proto **přímo unit-testovatelné** bez jsdom touch sekvencí):
  `swipeAction(dx,dy,{threshold,ratio})` → `'prev'|'next'|null` (vlevo = next, vpravo = prev, práh +
  dominantní vodorovná složka), `touchDistance`/`touchMidpoint`, `pinchScale`/`clampScale`
  (clamp `[MIN_SCALE=1,MAX_SCALE=4]`, `DOUBLE_TAP_SCALE`), `isDoubleTap(dt,dist)` a `clampPan`;
  `compareZoom.ts` = **pure zoom/pan matematika** synchronního plátna v `DupComparePage` (a proto
  unit-testovatelná bez DOM): `ZoomView{scale,x,y}`, `IDENTITY_VIEW`, `MIN_SCALE=1`/`MAX_SCALE=8`/
  `ZOOM_STEP`, `zoomAt(view,factor,px,py,box)` (bod pod kurzorem zůstane pod kurzorem), `zoomCentre`,
  `panBy`, `clampView` (pan se drží v `(scale-1)*box/2`, takže obrázek nejde vytáhnout z panelu),
  `isZoomed`, `viewTransform`; oddělené od `gestures.ts` schválně — ten je touch-only a měří proti
  viewportu;
  `duplicateCompare.ts` = **pure logika porovnání dvojic**: `buildPairQueue(groups)` → `ComparePair[]`
  (vícečlenná skupina **po dvojicích proti keeperovi**, nikdy člen-člen; skupina s keeperem mimo
  members se přeskočí, ne uhodne), `pairId(a,b)` (neuspořádané, jako backend), `pairsInGroup`/
  `pairIndexInGroup` (popisek „dvojice i z n"), `dropPairsTouching(pairs,uid)` (po merge zmizí
  dvojice archivované fotky), `buildDiffRows(left,right,fmt)` → `DiffRow{key,left,right,differs}` —
  `differs` se počítá z **porovnávacího klíče, ne z formátovaného textu** (dva časy ve stejné minutě
  se pořád liší), jména se porovnávají jako **množina** (pořadí z API nic neznamená); `fmt` se
  injektuje, takže testy nezávisí na locale; `countDiffering(rows)`;
  `urlState.ts` = hook `useUrlState` +
  pure `readUrlState`/`writeUrlState`: stav pohledu ↔ URL query přes History API, „Zpět vždy
  funguje"; `libraryView.ts` = typ `LibraryView` (vč. `min_rating`/`flag`, přepínače `favorite` a facetů
  `year`/`album`/`label`/`person`) + `LIBRARY_DEFAULTS` +
  `LIBRARY_PATH` (= `/`, kanonická routa knihovny — **knihovna je úvodní stránka**; všechny odkazy
  v appce míří sem, `/library` je jen redirect pro staré odkazy) +
  **multi-výběr facetů `album`/`label`/`person`**: každý klíč nese **čárkou spojený seznam UID** (urlState
  ukládá každý klíč jako jeden string, čárka se v UID nevyskytuje) — helpery `parseFilterList`/
  `joinFilterList`/`addToFilterList`/`removeFilterList` (sic `removeFromFilterList`) seznam kódují;
  fotka musí být ve **všech** vybraných albech, nést **všechny** štítky a obsahovat **všechny** vybrané
  osoby (AND). Celý výběr round-tripuje URL query, takže Zpět ho obnoví;
  `viewToParams` (sanitizuje sort/archived/**year** — `toYear` propustí jen čtyřciferný rok, ručně
  psaná/zastaralá URL spadne na „bez filtru" místo backendové 400 —, prosákne `min_rating`/`flag`,
  přepínač `favorite` a čárkou spojené UID facetů `album`/`label`/`person` beze změny — `buildPhotoQuery`
  je rozloží na opakované parametry `?album=a&album=b`, které backend ANDuje; neznámé UID prostě nic
  nenamatchuje; `sort` union navíc `rating`) + `hasActiveFilters` (`{ignoreQuery}` na search stránce,
  neprázdný seznam album/label/person nebo `favorite` = aktivní filtr, zahrnuje rating/flag i facety) —
  mapování URL stavu na API params; `ratingHotkeys.ts` = pure `ratingHotkey(key)` (`0`–`5` →
  rating, `p`/`r`/`v` → osobní označení 👍/👎/👁 (stored pick/reject/eye), jinak null) + `isTypingElement(target)` (input/textarea/select/
  contenteditable → hotkey se přeskočí) — sdíleno detailem fotky i fokusnutou dlaždicí;
  `shortcuts.ts` = registr klávesových zkratek + pure helpery: `shortcutToken(key)` (normalizace
  `KeyboardEvent.key` — single-char lower-case, named keys passthrough, `?` zůstává), `isFormModalOpen`
  (je otevřený `.modal.show` s form controlem? → suppress zkratek za dialogem), `HELP_SHORTCUT_KEY`
  (`?`) a `SHORTCUT_GROUPS` (grouped Grid/Detail zdroj pravdy pro nápovědu, `titleKey`/`descriptionKey`
  typované jako i18next `ParseKeys`, takže neexistující klíč je compile error);
  `searchView.ts` = typ `SearchView` (= `LibraryView` + `mode`)
  + `SEARCH_DEFAULTS` (mode `hybrid`) + `toMode` sanitizér;
  `auditView.ts` = typ `AuditView` (filtry + `offset`, string-only pro URL) + `AUDIT_DEFAULTS`
  + `AUDIT_PAGE_SIZE` (100) + `pickFilters` (view bez offsetu) + `viewToParams` (mapuje na
  `AuditListParams`, `since`/`until` z `YYYY-MM-DD` rozšíří na RFC 3339 hranice dne v UTC) — podklad
  `AuditPage`;
  `savedSearchView.ts` = pure `isSearchParams(params)` (přítomnost `mode` rozlišuje search od library
  pohledu) + `savedSearchHref(params)` (složí `pathname?query` na `LIBRARY_PATH` nebo `/search`, minimálně
  zakóduje uložené params proti defaultům přes `writeUrlState`, ignoruje neznámé/zastaralé klíče) —
  obnova uloženého hledání na přesnou URL;
  `mapView.ts` = typ `MapView` (mapset + viewport `lat`/`lng`/`z` + filtry) + `MAP_DEFAULTS` +
  `mapViewToParams` (sanitizuje archived) + `viewportFromView`/`mapsetFromView`/`hasActiveMapFilters`
  — mapování URL stavu mapy na feed params; `mapPopup.ts` = pure `buildPopupElement` (náhled +
  odkaz na detail fotky jako popup element, plain klik → SPA navigace, modifikovaný klik projde);
  `faceState.ts` = pure `faceState(face)` (`assigned`/`unassigned`/`unmatched` — čte přiřazení, ne
  `face.action`, aby optimistický update držel box i řádek v syncu s právě provedeným klikem)
  + `isNamed`; jeden zdroj pravdy pro barvy v overlayi, `FacesPanel` i `PeoplePanel`;
  `faceGeometry.ts` = pure `faceBoxStyle` (normalized bbox → absolutní `left/top/width/height`
  v %, pro overlay) + `padBbox`/`boxWithinCrop`/`cropImageStyle` + `displayFrame` (uložené
  rozměry + EXIF orientace → **zobrazený** rám; orientace 5–8 prohazuje strany, protože bbox je v
  display space) + `squareCrop` (bbox → výřez **čtvercový v pixelech**, ne v normalized
  jednotkách — to je to, co brání deformaci: „čtverec" v normalized rámu 4000×3000 je v pixelech
  obdélník a ve čtvercové dlaždici by obličej rozmáčkl; roste kratší pixelovou stranu ze středu a
  zasune výřez zpátky do rámu) + `faceCropStyle` (**legacy**, škáluje osy nezávisle → deformuje, a
  čte `tile_*`, což je centrovaný čtverec, ne celý rám; jen pro `FaceThumb`);
  `faceThreshold.ts` = pure převod prahu hledání osoby mezi **procenty** (UI) a **kosinovou
  vzdáleností** (backend): `percentToDistance` (`1 - p/100`)/`distanceToPercent` (inverzní,
  zaokrouhlený — i „match %" na kartě)/`clampThresholdPercent` + konstanty rozsahu (20–80, krok 5,
  default 50); `candidateReview.ts` = pure model review mřížky `/faces`: `ReviewItem`/`CandidateStatus`
  (`pending`/`done`/`error`), bucket `new`/`assign`/`done` (`bucketOf`, sdílený barevný kód přes
  `BUCKET_VARIANT`), `FilterTab`/`FILTER_TABS`/`matchesTab`/`tabCounts`, `isActionable`,
  `buildAssignRequest` (zrcadlí `useFaces`: existující `marker_uid` → `assign_person`, jinak
  `create_marker` s bboxem — nikdy nevyrobí duplicitní marker) a `buildRejection`;
  `recognitionSweep.ts` = pure helpery `/recognition` sweepu: konstanty posuvníku jistoty (50–95,
  krok 1, default 75) + `clampConfidencePercent`, `PersonState`, `personActionableCount`/`hasActionable`
  (karta osoby zmizí, když `hasActionable` je false), a **plochá klávesová fokus sekvence** napříč
  osobami (`FocusEntry`, `focusKey`, `focusSequence` = jen akční karty, `nextFocusKey`);
  `expandSearch.ts` = pure logika `/expand`: default prahu **70 %** (`EXPAND_THRESHOLD_DEFAULT_PERCENT`,
  rozsah/krok sdílí `faceThreshold`) + `clampExpandThresholdPercent`, `expandThresholdDistance`
  (procenta → vzdálenost, `toFixed(4)` řeže float šum pro URL), limit 1–200 default 50
  (`clampExpandLimit`), `ExpandSource` + `expandSources` (picker: bez prázdných sbírek, řazený dle
  počtu fotek sestupně, tiebreak jménem) a `similarityPercent` (podobnost kandidáta → celá %);
  `outlierReview.ts` = pure model `/outliers`: lifecycle `pending`→`removed`/`confirmed`/`error`
  (`OutlierItem`, `outlierKey` = `photo_uid:face_index`, `toOutlierItems`, `isActionable` — errored
  karta se **počítá**, její zápis selhal, takže je pořád nerozhodnutá —, `canUnassign` = má marker,
  jinak není co odpojit) + aritmetika prahu: **UI mluví v procentech, endpoint v kosinové
  vzdálenosti**, `outlierThresholdDistance` (0 % → 0 = „vrať vše", 100 % → `OUTLIER_MAX_DISTANCE`=1,
  protože dva **různí** lidé sedí kolem 1.0 a dál není co schovávat; `toFixed(4)` řeže float šum pro
  URL), `clampOutlierThresholdPercent` (default **0 = zobrazit vše**; nenulový default by tiše
  schovával obličeje), `distancePercent` (schválně **ne** podobnost — na téhle stránce větší číslo
  znamená „dál od člověka", což je ta souzená veličina) a `OUTLIER_LIMIT`=200;
  `coordinates.ts` = pure tolerantní parser souřadnic pro location picker: `parseCoordinates(input)`
  → `{ok:true,value:{lat,lng}}` | `{ok:false,error:'empty'|'format'|'range'}` (desetinné stupně /
  DMS / stupně-desetinné-minuty, komma/mezera oddělovač, ±/hemisféry N/S/E/W, unicode primy/`''`,
  axis reorder dle hemisfér, range check ±90/±180) + `formatCoordinates({lat,lng},precision=6)` →
  kanonický `"49.123400, 16.567800"` (round-tripuje parserem, ale je to **zobrazovací, ztrátový**
  formát — `16.7083583333333` → `16.708358`, proto se nezměněná souřadnice do PATCHe vůbec
  neposílá) — sdílí `MetadataPanel` picker;
  `kenBurns.ts` = pure `kenBurnsMotion(uid,intervalMs)` → endpointy pomalého zoom+pan přes celý
  snímek (`durationMs` = interval, takže animace trvá přesně jeden slide) + `kenBurnsStyle(…)` →
  `--kb-*` custom properties pro `slideshow.css` + `panLimit(scale)`. Parametry (8 směrů × zoom
  in/out × 5 hloubek) se derivují **deterministicky** z FNV-1a hashe `uid`, takže stejné album
  vypadá při každém přehrání stejně. Oba endpointy drží offset do `panLimit` svého scale a scale
  i offset se interpolují lineárně → **obraz nikdy neodkryje okraj** scény;
  `gridDensity.ts` = typ `GridDensity` (`'auto'` | 1…8) + `GRID_COLUMNS_MIN` (**1** = jedna fotka
  na řádek) / `MAX` / `GRID_COLUMN_CHOICES` (1…8) / `GRID_TILE_MIN_PX` (140) / `GRID_GAP_PX` (6) /
  `GRID_DENSITY_DEFAULT` (`'auto'` = dnešní width-driven mřížka) + pure
  `readStoredDensity`/`writeDensity`/`sanitizeDensity`/`stepDensity` (localStorage
  `kukatko.grid.density`, holý skalár v JSON; číslo se zaokrouhlí a **oklampuje do 1…8**;
  `readStoredDensity` vrací `null`, když **nic není uloženo** — prázdné/nedostupné úložiště nebo
  rozbitý JSON —, aby volající mohl zvolit viewport-aware default) +
  `defaultDensityForViewport(isNarrow)` (úzký → 1 sloupec, jinak `'auto'`) +
  pure `gridTemplateColumns(density, gapPx?)`. Pro `1` vrací `minmax(0, 1fr)` = **jeden sloupec
  přes celou šířku** (žádná podlaha dlaždice, není kam ustoupit). Pro N≥2 trik „přesně N sloupců na
  desktopu, míň (nikdy víc) na úzkém displeji": `repeat(auto-fill, minmax(max(140px, calc((100% -
  Gpx) / N)), 1fr))` — dokud se ideální šířka sloupce vejde nad práh 140 px, `auto-fill` naskládá
  přesně N; pod prahem se stopy přestanou zmenšovat a vejde se jich méně. `G` = `(N-1)*gap + 1`, ten
  pixel navíc je rezerva proti sub-pixel zaokrouhlení, které by jinak poslední sloupec shodilo na
  nový řádek. Ověřeno v headless Chromiu pro N=2…8 na šířkách 300–1600 px;
  `slideshowSettings.ts` = typ `SlideshowSettings{effect,intervalMs}` + `SlideshowEffect`
  (`fade`/`slide`/`kenburns`/`none`) + nabídky `SLIDESHOW_EFFECTS`/`SLIDESHOW_INTERVALS_MS` (1/2/3/5/10/15/30 s)
  + `SLIDESHOW_DEFAULTS` (`fade`, 5 s)
  + pure `readSettings`/`writeSettings`/`sanitizeSettings` (localStorage `kukatko.slideshow.settings`,
  sanitizace efektu + interval **snapnutý na nejbližší nabízenou hodnotu** — dřív uložený interval,
  který už v nabídce není (7 s), tak nespadne pod stůl ani nevyrenderuje prázdnou položku; při shodné
  vzdálenosti vyhrává kratší; fallback na defaulty při chybě/nedostupném storage);
  `slideshowView.ts` = pure `slideshowHref(scope,view)` (staví `/slideshow?…` z `LibraryView` přes
  `writeUrlState` + scope `album`/`label`/`mode`, default filtry vynechá — launch link promítání;
  `mode` se zapíše i když je roven defaultu, protože `SlideshowPage` čte jeho **přítomnost** jako
  „tohle přišlo z hledání");
  `duration.ts` = pure `splitDuration(ms)` → `{hours,minutes,seconds}` (zaokrouhlí na sekundy,
  záporné/nekonečné → nula) + `formatDuration(ms,t)` → kompaktní jednořádkový zápis přes i18next
  (`45 s` / `3 min 20 s` / `1 h 5 min`; nulová část se vynechá, u hodin se sekundy zahodí)
  + `slideshowDurationMs(count,intervalMs)` (celá show = interval na fotku)
  + `slideshowRemainingMs(index,total,intervalMs)` (fotky, které teprve přijdou — aktuální snímek
  se nepočítá, poslední slide hlásí nulu, index za koncem taky);
  `trashCountdown.ts` = pure `purgeCountdown(archivedAt,retentionDays,now?)` (zbývající dny do
  auto-purge z `archived_at` + retence → `{daysLeft,due}` nebo `null` když odpočet neplatí
  (nearchivovaná / retence ≤ 0 / neparsovatelné), odpočet na kartách koše);
  `format.ts` = pure `formatBytes(bytes)` (byte count → human-readable binární jednotky, např.
  `1536`→`"1.5 KB"`, neplatné→`"0 B"`) pro velikost souboru na duplicate-group kartách +
  `formatDuration(ms)` (ms → `M:SS`/`H:MM:SS`, neplatné→`"0:00"`) pro délku videa na dlaždicích +
  `formatMonth(year,month,locale)` (1-based rok/měsíc → locale-aware krátký měsíc + rok, např.
  `2026,1,'en'`→`"Jan 2026"`, mimo 1–12 → `""`) pro popisky ticků časové osy +
  `formatCaptureRange(from?,to?)` (rozsah `taken_at` alba → nejužší tvar: jeden měsíc
  `"6/2007"`, jeden rok `"2006"`, jinak `"1998–1999"` s en-dash; chybějící/neplatná mez →
  `""`, tj. album bez datovaných fotek nekreslí řádek) pro `AlbumTile` +
  **locale-aware** `formatDate(value,locale)`/`formatDateTime(value,locale)` (ISO/epoch/`Date` →
  `toLocaleDateString`/`toLocaleString` s **aktivním jazykem UI** `i18n.language`, ne výchozím
  jazykem prohlížeče; neparseovatelný vstup → původní string; používá PhotoTile/DuplicateGroupCard/
  MetadataPanel/Import/System pro datumy v cs/en formátu))),
  `services/` (`health.ts`, `auth.ts` = login/logout/me/changePassword, typy
  `User`/`Role` (admin/editor/viewer/ai)/`AuthSession`, `ApiError` se statusem, `canWrite`/`canImport`/`roleAtLeast`,
  `MIN_PASSWORD_LENGTH`; `photos.ts` = `fetchPhotos(params,signal)` nad `GET /api/v1/photos`
  (filtry/řazení/stránkování → `PhotoListResponse{photos,total,limit,offset,next_offset}`),
  `searchPhotos(params,mode?,signal)` nad `GET /api/v1/search` (mód
  `fulltext`/`semantic`/`hybrid`, odpověď navíc `mode`+`degraded`),
  `fetchSimilar(uid,limit?,signal)` nad `GET /api/v1/photos/{uid}/similar` → `SimilarPhoto[]`
  (`Photo`+`distance`; empty-friendly), typy `SimilarPhoto`/`SimilarResponse`,
  `fetchTimeline(params,signal)` nad `GET /api/v1/photos/timeline` → `Timeline{buckets,total}`
  (měsíční date-histogram, stejné filtry jako list; sort/stránkování backend ignoruje), typy
  `Timeline`/`TimelineBucket{year,month,count,cumulative}` — podklad `TimelineScrubber`,
  `fetchPhotoYears(params,signal)` nad `GET /api/v1/photos/years` → `YearsResponse{years,total}`
  (rok-histogram, stejné filtry jako list; backend ignoruje `year` sám, sort/stránkování taky),
  typy `YearsResponse`/`YearBucket{year,count}` — podklad year facetu (`useLibraryFacets`);
  `PhotoListParams` navíc `year?: string` (čtyřciferný rok), `buildPhotoQuery` ho serializuje,
  `favoritePhoto(uid,favorite,signal)` nad `PUT`/`DELETE /api/v1/photos/{uid}/favorite` (per-user
  toggle, 204, podklad optimistického `useFavorite`),
  `ratePhoto(uid,{rating?,flag?},signal)` nad `PUT /api/v1/photos/{uid}/rating` +
  `clearRating(uid,signal)` nad `DELETE …/rating` (per-user hvězdy 0–5 + osobní označení
  none|pick|reject|eye, 204, podklad `useRating`), typy `RatingUpdate`/`RatingFlag`,
  `regenerateThumbnail(uid,signal)` nad `POST /api/v1/photos/{uid}/regenerate-thumbnail`
  (editor/admin servisní akce, synchronní, `RegenerateThumbnailResult{status,sizes}`, 422 =
  originál nedekódovatelný; podklad `RegenerateThumbnailButton`),
  **koš** `unarchivePhoto(uid)` (`POST …/unarchive` obnova), `purgePhoto(uid)` (`POST …/purge?confirm=true`
  trvalé mazání), `emptyTrash()` (`POST /trash/empty?confirm=true` → `PurgeResult{purged,failed}`),
  `fetchTrashInfo()` (`GET /trash/info` → `TrashInfo{retention_days}`),
  `buildPhotoQuery`, `thumbUrl(uid,size,token?)`, `videoUrl(uid,token?)` (range stream pro
  `<video>`; při R2 backendu routa **302** redirectne na Workera, `<video>` redirect následuje
  při každém requestu, takže seek jede vždy proti čerstvému podpisu), `GRID_THUMB_SIZE`,
  typy `Photo` (vč. `is_favorite` + per-user `rating`/`flag` + video pole
  `duration_ms`/`video_codec`/`audio_codec`/`has_audio`/`fps` + **`thumb_url`/`download_url`** +
  **`stack_uid`/`stack_count`**)/`PhotoListParams`
  (vč. `album`/`label` scope + **`person` scope** (čárkou spojené UID subjektů → opakované `?person=`, AND)
  + **`country`/`city` place scope** + `favorite` filtr + `min_rating`/`flag` filtry)/`PhotoSort`
  (vč. `rating`)/`RatingFlag`/`ArchivedFilter`/`SearchMode`, `ApiError`.
  **Adresy médií se neskládají z UID.** Grid dlaždice i download odkaz berou `photo.thumb_url` /
  `photo.download_url` z payloadu — jen server umí URL podepsat. `thumbUrl(uid,size)` zůstává pro
  velikost, kterou payload nenese (lightbox, canvas editoru, cover podle UID) a `downloadUrl(uid,…)`
  pro **rendering nedestruktivního editu**, který umí jen aplikace;
  `organize.ts` = Albums/Labels klient: alba `fetchAlbums`/`fetchAlbum`/`createAlbum`/`updateAlbum`/
  `deleteAlbum`/`addAlbumPhotos`/`removeAlbumPhotos`, štítky `fetchLabels`/
  `fetchLabel`/`createLabel`/`updateLabel`/`deleteLabel`/`attachLabel`/`detachLabel`; typy
  `Album`/`AlbumCount`/`AlbumInput`/`AlbumType`/`Label`/`LabelCount`/`LabelInput`;
  `savedSearches.ts` = uložená hledání klient: `fetchSavedSearches`/`createSavedSearch(name,params)`/
  `updateSavedSearch(uid,{name?,params?})`/`deleteSavedSearch(uid)` nad `/api/v1/saved-searches`, typy
  `SavedSearch`/`SavedSearchParams` (= verbatim URL view-state `Record<string,string>`)/
  `SavedSearchUpdate`; `search.ts` = grouped **global search** klient: `globalSearch(q,signal)` nad
  `GET /api/v1/search/global` → `GlobalSearchResult{query,albums,labels,people,photos}` (top-N per
  skupina, každá vždy pole) + pure helpery `hasEntityMatches`/`isEmptyResult`, typy
  `GlobalSearchAlbum`/`GlobalSearchLabel`/`GlobalSearchPerson`/`GlobalSearchResult`; oddělené od
  photo `searchPhotos` (fulltext/semantic/hybrid), podklad `GlobalSearchSections`; `bulk.ts` =
  `bulkUpdatePhotos(uids,ops)` nad `POST /photos/bulk` (hromadná úprava výběru), typy
  `BulkOperations` (add/remove alba+štítku, set/clear caption+popisu+polohy,
  archive/unarchive, set_favorite per-user)/`BulkLocation`/`BulkResult`; `duplicates.ts` =
  `fetchDuplicates(params,signal)` nad `GET /api/v1/duplicates` (skupiny duplikátů →
  `DuplicatesResponse{groups,total,limit,offset,next_offset}`) + `mergeDuplicates(input,signal)` nad
  `POST /api/v1/duplicates/merge` (řešení skupiny → `MergeResult{albums_added,labels_added,people_added,
  metadata_filled[],archived,dry_run}`; `dry_run:true` = náhled), typy `DuplicateReason`/
  `DuplicateMember`/`DuplicateGroup`/`DuplicatesParams`/`MergeInput`/`MergeResult`; `upload.ts` =
  `uploadFile(file,{onProgress,signal})`
  nad **`XMLHttpRequest`** (jeden soubor/request kvůli upload-progress eventům, FormData se
  streamuje), `isAbortError`, typy `UploadFileResult`/`UploadResponse`/`UploadWarning`/
  `UploadOutcome`; `photos.ts` navíc `fetchPhoto(uid)` (detail `GET /photos/{uid}` →
  `PhotoDetail` = `Photo`+`files`+`albums`+`labels` inline chipy `+ uploader?` `{uid,name}`),
  `updatePhoto(uid,patch)`
  (`PATCH …` částečná editace metadat → `PhotoMetadataUpdate`, null maže nullable),
  `fetchEdit(uid)`/`saveEdit(uid,edit)` (`GET`/`PUT …/edit` nedestruktivní edit → `PhotoEdit`
  crop/rotation/brightness/contrast), `downloadUrl(uid,{original?,token?})` (URL downloadu,
  default honoruje edit, `original:true` pro originál),
  `downloadPhotosZip({photoUids?,albumUid?,name?})` (**hromadné stažení ZIP**: `POST
  …/download-zip`, přečte odpověď jako `Blob` a stáhne ji přes dočasnou object URL — jméno
  archivu skládá klient (`name`.zip nebo `kukatko-photos-<date>.zip`, `date` počítá klient a
  posílá i serveru), hází `ApiError` (413 = přes strop); typ `ZipDownloadRequest`),
  **stacky** `stackPhotos(photoUids,signal)` (`POST …/photos/stack` — ruční seskupení výběru → `PhotoDetail`
  nového primárního), `setStackPrimary(uid,signal)` (`POST …/{uid}/stack/primary`),
  `unstackMember(uid,signal)` (`POST …/{uid}/unstack`) a `unstackAll(uid,signal)`
  (`POST …/{uid}/unstack-all`) — všechny vracejí refreshnutý `PhotoDetail`; typy `PhotoDetail` (navíc
  `stack_members?: StackMember[]` — pruh variant, primary první)/`StackMember`
  `{uid,file_name,media_type,file_mime,file_width,file_height,file_size,is_primary,thumb_url?,download_url?}`/`PhotoAlbumRef`/
  `PhotoLabelRef`/`PhotoUploaderRef`/`PhotoMetadataUpdate`/`PhotoEdit`; `people.ts` = People/face klient: subjekty
  `fetchSubjects`/`fetchSubject`/`createSubject`/`updateSubject`/`deleteSubject`/
  `fetchSubjectPhotos`, obličeje `fetchFaces`/`assignFace`, shluky `fetchClusters`/
  `assignCluster`/`removeClusterFace`, outlier `fetchOutliers`; typy `Subject`/`SubjectCount`/
  `SubjectInput`/`SubjectType`/`Bbox`/`FaceView`/`FacesResponse`/`AssignRequest`/`Suggestion`/
  `ClusterView`/`ExampleFace`/`ClusterAssignRequest`/`RemoveFaceRequest`/`OutlierResult`/
  `OutlierFace`; sdílí `ApiError`+`buildPhotoQuery` z `auth.ts`/`photos.ts`);
  `faces.ts` = klient hledání „najdi osobu mezi neotagovanými fotkami":
  `searchCandidates(subjectUid,{threshold,limit},signal)` nad `POST /subjects/{uid}/candidates`; typy
  `CandidateSearchRequest`/`CandidateResult`/`Candidate`/`FaceBox`/`CandidateCounts`/`CandidateAction`
  (`create_marker`/`assign_person`/`already_done`)/`CandidateReason`; potvrzení jde přes `assignFace`
  z `people.ts`, zamítnutí přes `feedback.ts`; `feedback.ts` = perzistentní zpětná vazba (nemutuje,
  jen drží zamítnutý obličej/fotku mimo příští hledání): `rejectFace(req,signal)`/`unrejectFace(req,signal)`
  nad `POST`/`DELETE /feedback/face-rejections`, typ `FaceRejection` `{photo_uid,face_index,subject_uid}`,
  a `rejectLabel(req,signal)`/`unrejectLabel(req,signal)` nad `POST`/`DELETE /feedback/label-rejections`,
  typ `LabelRejection` `{photo_uid,label_uid}`; **`confirmFace(req,signal)`/`unconfirmFace(req,signal)`**
  nad `POST`/`DELETE /feedback/face-confirmations`, typ `FaceConfirmation`
  `{photo_uid,face_index,subject_uid}` — **opačná polarita než `rejectFace`**: zapisuje „tenhle
  obličej **JE** tahle osoba" (✗ v outlier review = „ne, fakt je to on"), backend pak potvrzený
  obličej z dalších outlier výsledků vyloučí; zaměnit ji za `rejectFace` znamená uložit pravý opak
  toho, co uživatel řekl; **`dismissDuplicate(req,signal)`/`undismissDuplicate(req,signal)`** nad
  `POST`/`DELETE /feedback/duplicate-dismissals`, typ `DuplicateDismissal` `{photo_uid,other_uid}` —
  „tyhle dvě fotky NEJSOU duplikáty" z `DupComparePage` („Nechat obě"); dvojice je **neuspořádaná**
  (backend normalizuje), nic se nearchivuje ani neslučuje, jen se zapíše názor a `GET /duplicates`
  pak tu hranu na každém dalším scanu zahodí (vše idempotentní → jde volat optimisticky);
  `expand.ts` = klient rozšiřování sbírky: `searchSimilar(kind,uid,{threshold,limit},signal)` nad
  `GET /albums/{uid}/similar` / `GET /labels/{uid}/similar` (`threshold` = **kosinová vzdálenost**,
  převod z procent dělá volající přes `lib/expandSearch`), typy `ExpandKind`/`ExpandCandidate`
  (`photo` má `thumb_url` už oražené)/`ExpandResult` (summary počty + `min_match_count` +
  `reason?` `empty_collection`/`no_source_embeddings`)/`ExpandReason`/`ExpandSearchRequest`;
  přidávání jde přes `bulk.ts` (`POST /photos/bulk`), zamítnutí přes `feedback.ts`;
  `recognition.ts` = klient recognition sweepu: `streamSweep(params,onMessage,signal)` nad
  `GET /faces/sweep` **streamuje NDJSON** (`fetch`+`ReadableStream`, řádkuje ručně, `onMessage` dostane
  jen kompletní řádky), typy `SweepParams` `{confidence,limit}` (`confidence` = **procenta**, backend
  si je přeloží na vzdálenost) a `SweepMessage` = `progress`|`person`|`summary` (`SweepPerson` nese
  `candidates`/`counts`/`actionable` ve stejném tvaru jako `faces.ts`); abort přes `signal` = `AbortError`
  (volající ignoruje); potvrzení jde přes `assignFace`, zamítnutí přes `rejectFace`;
  `map.ts` = mapový klient: `fetchMapPhotos(params,signal)` nad `GET /api/v1/map/photos`
  (GeoJSON FeatureCollection geotagovaných fotek + `buildMapQuery`), `tileLayerUrl(mapset)` (Leaflet
  URL template na backend proxy, **bez API klíče**), `reverseGeocode(lat,lng,signal?)` nad
  `GET /api/v1/map/rgeocode` (on-demand reverse geocode pro detail fotky → `GeocodeResult`),
  `searchPlaces(query,limit?,signal?)` nad `GET /api/v1/map/geocode` (**forward** geocode pro
  editor polohy → `Place[]` = `{name,label,type,location,lat,lng}` od nejlepší shody; žádná shoda
  = **prázdné pole**, ne chyba; volající **musí debouncovat** — backend sice cachuje a
  rate-limituje, ale request na klávesu je jak vypálit měsíční kredit za odpoledne),
  **`probeTileFailure(tileUrl,signal?)`** (`<img>` status v JS nevidíš → dlaždice, kterou Leaflet
  nenačetl, se přefetchne a status proxy se přeloží na `TileFailure`: **424 → `key_rejected`**
  (mapy.com odmítá **náš** klíč), 429 → `rate_limited`, 503 → `unavailable`, jinak `error`;
  200/404 → `null`, protože chybějící dlaždice mimo pokrytí je normální odpověď; síťová chyba →
  `'error'`, abort probublá), `toMapset`/`MAPSETS`; typy
  `MapFeature`/`MapFeatureCollection`/`MapFeatureProperties`/`MapPhotoParams`/`Mapset`/
  `TileFailure`/`GeocodeResult`/`RegionalItem`/`Place`);
  `places.ts` = klient hierarchie míst: `fetchPlaces(country?,signal)` nad `GET /api/v1/places`
  → `PlaceCountry[]` (země s počty + nested `cities`, volitelné `country` drillne do měst jedné
  země); typy `PlaceCountry`/`PlaceCity`; procházení fotek lokality jde přes sdílené
  `fetchPhotos({country,city})`;
  `import.ts` = admin import klient: `fetchImportRuns(signal)` nad `GET /api/v1/import/runs`
  (`{runs,limit,offset,sources}`), `fetchJobStats(signal)` nad `GET /api/v1/jobs/stats`,
  `startImport(source,signal)` nad `POST /api/v1/import/{photoprism|photosorter}` (409 → ApiError);
  typy `ImportSource`/`RunStatus`/`ImportCounts`/`ImportRun`/`ImportSources`/`ImportRunsResponse`/
  `StartImportResult`/`JobStats`),
  `maintenance.ts` = admin maintenance klient: `fetchMaintenanceScan(signal)` nad
  `GET /api/v1/maintenance/scan` → `ScanReport`, `runMaintenanceRepair(options,signal)` nad
  `POST /api/v1/maintenance/repair` → `RepairResult`; typy `Finding`/`ScanReport`/`RepairOptions`/
  `RepairResult`; sdílí `ApiError` z `auth.ts` a `fetchJobStats` z `import.ts` pro progress,
  `system.ts` = admin system-status klient: `fetchSystemStatus(signal)` nad `GET /api/v1/system/status`
  → `SystemStatus`, `triggerBackup(signal)` nad `POST /api/v1/backup` (409/503 → ApiError),
  `requeueDeadLetterJobs(signal)` (vylistuje `GET /jobs?state=dead` → per-job `POST /jobs/{id}/requeue`,
  vrací počet, 404/409 skip); typy `SystemStatus`/`DatabaseStatus`/`EmbeddingsStatus`/`JobsStatus`/
  `BackupStatus`/`ImportsStatus`/`StorageStatus`/`MapsStatus`/`MapsState`/`VersionInfo`; sdílí
  `ApiError` z `auth.ts` a `ImportRun` z `import.ts`,
  `users.ts` = admin klient správy účtů nad `/api/v1/admin/users`: `fetchUsers(signal)` → `AdminUser[]`
  (= `User` + `note`), `createUser(body,signal)` (`POST`, 409 = obsazený username, 400 = slabé heslo /
  neplatná role / dlouhá poznámka), `updateUser(uid,body,signal)` (`PATCH`, **replace** celého
  mutovatelného profilu → posílej i pole, která dialog nenabízí), `setUserDisabled(user,disabled,signal)`
  (zakázat → dedikovaný `POST /{uid}/disable`, který nepotřebuje profil a nepřepíše souběžnou editaci;
  povolit → `PATCH` s `disabled:false`, vlastní endpoint neexistuje) a `resetUserPassword(uid,pwd,signal)`
  (`POST /{uid}/password`, 204, odhlásí všechny session cíle); konstanty `ROLES`/`MAX_NOTE_LENGTH`,
  typy `AdminUser`/`CreateUserBody`/`UpdateUserBody`; hash hesla nemá kam uniknout — backend ho
  neserializuje a žádný typ pro něj nemá pole,
  `audit.ts` = admin auditní klient nad `GET /api/v1/audit`: `fetchAuditLog(params,signal)` →
  `AuditListResponse{entries,total,limit,offset,next_offset}`, `buildAuditQuery` serializuje filtry
  (prázdné/nulový offset vynechá); typy `AuditRecord` (nullable `actor_uid`/`target_uid`/`ip`/
  `user_agent`/`details`)/`AuditListParams`; sdílí `ApiError` z `auth.ts`. Pozor na názvosloví:
  query params používají jména endpointu (`user`/`entity_type`/`entity_uid`), záznamy sloupce
  (`actor_uid`/`target_type`/`target_uid`),
  `i18n/` (i18next init — options jsou exportované jako `initOptions`, ať si je test může nabootit
  do vlastní instance — + `locales/{cs,en}/common.json`;
  typované klíče přes `types/i18next.d.ts` — nové stringy přidávej do **obou** locale souborů;
  **čeština default**, žádné natvrdo zapsané UI texty — vše přes `t()`. Jediný detektor je
  `localStorage` (kam píše `LanguageSwitcher` z `AccountPage`); `navigator`/`htmlTag` **záměrně
  nejsou** v `detection.order`, jinak by anglicky nastavený prohlížeč dostal při první návštěvě
  anglické UI — bez uložené volby rozhoduje `fallbackLng: 'cs'`. **Pluralizace** přes
  i18next CLDR plural sufixy: count-vázané řetězce kde se podstatné jméno shoduje s číslem mají
  formy `key_one/_few/_many/_other` (čeština) a `key_one/_other` (angličtina) — caller jen předá
  `{ count }` (např. `albums.photoCount`, `clusters.size`, `bulkEdit.title`, `duplicates.memberCount`/
  `archived`, `trash.confirm.bulk`); label-tvary s dvojtečkou/závorkou (`library.count`, `selection.count`)
  zůstávají bez plurálu. **Datumy/čísla respektují jazyk** přes `lib/format` `formatDate`/`formatDateTime`
  (`i18n.language`). **Drift-guard testy** `i18n.test.ts` (cs/en mají identické *logické* klíče po
  odstranění plural sufixu, žádné prázdné hodnoty, každý jazyk má všechny své CLDR plural kategorie,
  interpolační `{{var}}` proměnné se shodují napříč jazyky; navíc **default-language testy** nad
  čerstvou instancí z `initOptions`: prázdný localStorage → `cs` i pod anglickým prohlížečem,
  uložená volba vyhrává, změna jazyka se uloží) + `screens.test.tsx` (reprezentativní
  obrazovky — navbar + dlaždice — se vykreslí bez missing-key warningů v cs i en přes
  `cloneInstance({saveMissing})`, plural rendering 1/3/5, language-switch přepíše viditelný text)),
  `styles/tokens.css` (**design token vrstva** — jediný zdroj pravdy pro odstupy, rádiusy, elevaci,
  motion a typografickou škálu; importovaná **jednou** v `main.tsx` hned za Bootswatch CSS a **před**
  `app.css`, které tokeny konzumuje. Bootswatch Superhero zůstává základní téma — tohle je vrstva
  **nad** ním, nepřepisuje `--bs-*` proměnné globálně (jediná výjimka je cílený **theme root**).
  **Theme root:** aplikace běží s `data-bs-theme="dark"` na `<html>` (v `index.html`) — bez něj
  Superhero nechává `--bs-tertiary-bg`, `--bs-secondary-bg(-subtle)` a `--bs-emphasis-color` na
  **světlých** hodnotách na `:root` a do tmy je překlápí až uvnitř `[data-bs-theme=dark]`, takže
  `.bg-body-tertiary` panely (advanced-filtr knihovny, `SelectionBar`, detail řádek auditu) i
  skeletony (`.bg-secondary-subtle`) se malovaly skoro bílé pod skoro bílým `--bs-body-color` =
  neviditelné popisky (white-on-white). Protože Superhero v dark bloku zároveň mění navy pozadí
  `#0f2537` na neutrální charcoal `#212529`, `:root[data-bs-theme='dark']` v `tokens.css` **re-pinuje
  `--bs-body-bg`/`--bs-body-color` zpět na navy** — tmavé *povrchy* bez ztráty navy *identity*
  (a `--kk-surface-*` color-mixy z `--bs-body-bg` zůstanou beze změny). Obsah: **spacing** `--kk-space-1..7` (4px
  škála), **rádiusy** `--kk-radius-sm/md/lg/pill`, **elevace** `--kk-shadow-0..3` (na tmavém tématu
  vždy dvojice: drop shadow + `inset 0 1px 0` horní highlight, jinak by stín na navy pozadí zanikl),
  **povrchy** `--kk-surface-raised` (odvozený z `--bs-body-bg`; **záměrně není** Superhero
  `--bs-card-bg` `#4e5d6c` — ta barva je zároveň `secondary`, takže `outline-secondary` tlačítko
  na ní zmizí) a `--kk-surface-sunken` (jáma pod náhledem), **motion** `--kk-duration-fast/
  base/slow` + `--kk-ease-standard`, **focus ring** `--kk-focus-ring-*`, **typografie**
  `--kk-font-size-*`/`--kk-line-height-*`/`--kk-tracking-*`.
  Sémantické třídy: **typografická škála** `.kk-page-title` (jedna na route, na `<h1>`),
  `.kk-section-title` (nadpis panelu/sekce, `<h2>`/`<h3>`), `.kk-text-body`, `.kk-text-caption`,
  `.kk-text-eyebrow` — komponenty **nenastavují vlastní `font-size`** (žádné `h3`/`h5`/`fs-5`
  utility na nadpisech, žádné inline `fontSize`); **povrchy** `.card` (ztrácí těžký okraj přes
  `--bs-card-border-color: transparent`, dostává `--kk-shadow-1`; `.border-primary` apod. stále
  fungují) a `.kk-surface`; **dlaždice** `.kk-tile` + `.kk-tile__media` (bez okraje, elevace,
  hover/focus lift na `--kk-shadow-3` — používají `AlbumTile`, `SubjectTile`, `PhotoTile`;
  `:focus-within` pokrývá `PhotoTile`, kde je fokusovatelný až vnitřní odkaz) a `.kk-tile-row`
  (řádková varianta pro seznam štítků — místo liftu se zvýrazní pozadím, protože řádek v sloupci
  nemá kam vyskočit); `.kk-tile__placeholder`; **chip** `.kk-chip` (odebíratelný token nad
  Bootstrap `.badge` — jen to, co badge nemá: box kolem koncového `.btn-close` a strop šířky,
  aby se dlouhý název alba zkrátil místo roztažení řádku; používá `MultiSelect`);
  **barvy entit** — album/tag/osoba dostávají každý svůj odstín, aby se rozlišily na první pohled
  (dřív byly album i štítek stejná primární oranžová = nešly rozeznat). Tokeny
  `--kk-entity-album-bg` (fialová) / `--kk-entity-tag-bg` (tyrkysová) / `--kk-entity-person-bg`
  (růžová) + `--kk-entity-fg` (bílá); modifikátory `.kk-entity-album/-tag/-person` na `.badge`
  (barva má `!important`, aby přebila Bootstrap `.bg-*`/`.text-bg-*`, které jsou taky `!important`,
  takže třída sedí na plain `.badge` i na `<Badge>` i na odkaz-pill). Mapování kind→třída+ikona je
  **jednou** v `components/entityStyle.ts` (`ENTITY_STYLE`) a čte ho každé místo, kde se entita
  zobrazí jako chip: aktivní filtr-chipy knihovny (`FilterBar`), organize panel fota
  (`OrganizePanel`), pruh badgí nad fotkou (`OrganizeBadges`) a `GlobalSearchSections` — barevný
  jazyk je tak konzistentní, ne jednorázový.
  Barva je **jen doplněk**: chip vždy nese i textový popisek a vodicí ikonu (album `collection` /
  tag `tags` / osoba `person-circle`), aby rozlišení přežilo pro barvoslepé; bílý text má na navy
  kontrast ≥ 5:1. Neutrální filtry (rok, hodnocení, flag…) zůstávají `text-bg-primary`;
  **appear** `.kk-appear` (jednorázový fade-up).
  **Focus outline se nikdy neodstraňuje** — `.kk-tile:focus-visible`/`.kk-tile__media:focus-visible`
  kreslí `outline` (přežije `overflow: hidden` náhledu). **`prefers-reduced-motion`**: token
  durations spadnou na `1ms`, lift (`transform`) a `.kk-appear` se vypnou úplně; spinnery
  a progress bary animují dál, protože nesou význam),
  `styles/app.css` (**global responzivní polish vrstva** importovaná v `main.tsx` hned za
  `tokens.css` — jen cross-cutting mobil/touch věci, které Bootstrap utility neumí: **safe-area
  insety** přes `env(safe-area-inset-*)` (fungují díky `viewport-fit=cover` v `index.html`) na
  navbaru (`.kukatko-navbar`) a hlavním kontejneru (`.kukatko-main`); guard proti vodorovnému
  scrollu/overscroll bounce (`body overflow-x:hidden`, `html overscroll-behavior-y:none`); sdílený
  **sticky-toolbar offset** `.kukatko-sticky-toolbar` (`top: navbar height + safe-area-inset-top`,
  z-index pod navbarem — in-page sticky bary jako `SelectionBar` dosednou pod navbar, ne pod něj);
  **min. tap-target** `.kukatko-tap-target` (2.75rem/44px) pro icon-only ovládání jako
  `FavoriteButton`; **app-wide touch-target floor** — `@media (pointer: coarse)` blok, který na
  dotykových zařízeních (telefon/tablet) vynutí min. 44px na `.btn`/`.form-control`/`.form-select`/
  `.nav-link`/`.dropdown-item`/`.list-group-item-action`/`.page-link` + větší `.form-check-input`,
  bez zásahu do desktop (fine-pointer) layoutu a bez per-komponentových změn (systémová oprava
  všudypřítomných `size="sm"` ovládání);
  **native form chrome** `.form-control`/`.form-select { color-scheme: light }` — Superhero maluje
  form-controly bíle bez ohledu na téma, takže pod `color-scheme: dark` stránky by prohlížeč kreslil
  jejich nativní části (placeholder a glyf kalendáře u `type=date`, dropdown selectu) světle =
  neviditelné na bílém poli; připnutí ovládacích prvků na světlé schéma je vykreslí tmavě-na-bílém;
  **časová osa** `.kukatko-timeline*` (fixní svislá datová lišta u pravého
  okraje pod navbarem, absolutně umístěné ticky, floating popisek aktivního měsíce, `touch-action:
  none` pro tažení, na šířkách ≤ 575.98px skrytá); **filtr-bar** `.kukatko-filter-*`
  (`.kukatko-filter-search` = search pole roste a plní řádek hlavičky, `.kukatko-filter-sort`
  min. šířka, `.kukatko-filter-panel` = 44px tap-targety na prvcích panelu, `.kukatko-filter-chip`
  = tappable pill chip s křížkem); CSS proměnná `--kukatko-navbar-height`),
  `test/setup.ts` (jsdom **`window.matchMedia` stub** — non-matching default, jednotlivé testy ho
  můžou přepsat pro simulaci telefonu).
  Routing v `App.tsx`: tabulka rout žije v exportované `AppRoutes` (aby ji šlo v testech mountnout
  do `MemoryRouter` a ověřit samotné drátování — `App.test.tsx`), `App` ji jen obalí
  `BrowserRouter`+`AuthProvider`. `/login` veřejné, zbytek pod `RequireAuth`; `/slideshow` je pod
  `RequireAuth` ale **mimo `Layout`** (fullscreen bez navbaru), zbytek pod `Layout`
  (**`/` = `LibraryPage`** — knihovna je úvodní stránka; `/library` → `LibraryRedirect`
  (`replace` redirect na `/` se zachovaným query stringem),
  `/favorites`, `/albums`, `/albums/:uid`, `/labels`, `/labels/:uid`, `/search`, `/saved`, `/map`,
  `/places`, `/photos/:uid`, `/people`,
  `/people/:uid`, `/account`; `/upload`, `/people/clusters`, `/faces`, `/recognition`, `/trash` a
  `/duplicates` navíc pod `RequireRole role="editor"` = write-only (a `/duplicates/compare` tamtéž,
  ale **mimo `Layout`** — fullscreen jako `/review`), `/import` pod `RequireImport`
  (admin **nebo** ai — mimo žebříček rolí, řídí `canImport`), `/maintenance`, `/system`,
  `/users` a `/audit` pod `RequireRole role="admin"` = admin-only). Konfig:
  `vite.config.ts` (build → `../internal/web/static/dist`, vitest jsdom, dev proxy
  `/healthz`+`/api` → `:8080`), `eslint.config.js` (strict typed), `.prettierrc.json`,
  `tsconfig*.json`.
